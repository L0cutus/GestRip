package buste

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// La lista della home (RIP-H-02, 06 in parte, 08, 09, 10, 11, 12, 21, 56), come RepairIndex.build/2,
// counters/2, urgent?/3, hide_defaults/2 ed explicit_filter?/1 dello standalone a go-baseline, con
// Settings.Operator.read/0 per le tre impostazioni:
//   - le buste nel cestino non ci sono mai (il filtro globale di Repo.prepare_query/3);
//   - i filtri di questa slice sono quelli dei riquadri dei contatori (stato, scadenza, compro_oro,
//     attesa, fuori_bottega, senza_qr, messaggi) e la pagina; cerca, oreficeria, artigiano, date e
//     notifiche arrivano con la slice 051 (cassetto dei filtri e pillole);
//   - senza filtri la lista nasconde le pronte e/o le consegnate se l'impostazione lo dice (righe,
//     mai contatori); un filtro qualsiasi, anche il clic su un riquadro, le rimostra;
//   - ordine: fissate, con un messaggio non letto, anno del numero, progressivo, id (tutti a scendere);
//   - 30 buste per pagina; una pagina oltre l'ultima è l'ultima, una pagina non valida è la 1;
//   - i contatori non guardano né i filtri né le impostazioni della lista.
// La riga repair.index_viewed la scrive RegistraLista, una per visita (D-68 proposta: lo standalone,
// LiveView, ne scrive due).

// BustePerPagina è @page_size di RepairIndex.
const BustePerPagina = 30

// Le chiavi delle impostazioni della home (Settings.Operator).
const (
	ChiaveGiorniScadenza   = "scadenza_giorni"
	ChiaveMostraPronte     = "home_mostra_pronte"
	ChiaveMostraConsegnate = "home_mostra_consegnate"
)

// I limiti della soglia di scadenza (Operator: @default_due_days, @minimum_due_days,
// @maximum_due_days).
const (
	giorniScadenzaPredefiniti = 7
	giorniScadenzaMinimi      = 1
	giorniScadenzaMassimi     = 365
)

// Filtri sono i filtri della lista che questa slice legge, già ripuliti (normalize/1).
type Filtri struct {
	Stato                                                        string // "" nessuno
	Scadenza, CompraOro, Attesa, FuoriBottega, SenzaQR, Messaggi bool
	Pagina                                                       int64 // da 1
}

// Attivo è explicit_filter?/1 sui filtri di questa slice: c'è qualcosa che si chiede. La pagina
// da sola non è un filtro.
func (f Filtri) Attivo() bool {
	return f.Stato != "" || f.Scadenza || f.CompraOro || f.Attesa || f.FuoriBottega || f.SenzaQR || f.Messaggi
}

// LeggiFiltri legge i filtri da valore(nome), che dà il parametro della query come testo di Plug
// (false se manca o non è un testo): Stato senza spazi ai bordi, una casella c'è se non è vuota
// senza spazi (present?/1), la pagina con Id.positive/1 (altrimenti 1).
func LeggiFiltri(valore func(nome string) (string, bool)) Filtri {
	testo := func(nome string) string {
		if v, ok := valore(nome); ok {
			return strings.TrimFunc(v, unicode.IsSpace)
		}
		return ""
	}
	f := Filtri{
		Stato: testo("stato"), Scadenza: testo("scadenza") != "", CompraOro: testo("compro_oro") != "",
		Attesa: testo("attesa") != "", FuoriBottega: testo("fuori_bottega") != "", SenzaQR: testo("senza_qr") != "",
		Messaggi: testo("messaggi") != "", Pagina: 1,
	}
	if v, ok := valore("p"); ok {
		if p, ok := idPositivo(v); ok {
			f.Pagina = p
		}
	}
	return f
}

// ImpostazioniHome sono le tre impostazioni della home (Operator.read/0), già riparate.
type ImpostazioniHome struct {
	GiorniScadenza                 int
	MostraPronte, MostraConsegnate bool
}

// giorniScadenza è due_days/1: Integer.parse/1 per intero (segno ammesso, niente spazi), portato fra
// 1 e 365; un testo che non è un intero, o nessun valore, vale 7. Un intero oltre i 64 bit (lo
// standalone non ha limiti) va al suo estremo.
func giorniScadenza(v string, c bool) int {
	if !c {
		return giorniScadenzaPredefiniti
	}
	cifre := strings.TrimLeft(v, "+-")
	if len(v)-len(cifre) > 1 || cifre == "" || strings.Trim(cifre, "0123456789") != "" {
		return giorniScadenzaPredefiniti
	}
	n, err := strconv.ParseInt(v, 10, 64)
	switch {
	case err != nil && strings.HasPrefix(v, "-"), err == nil && n < giorniScadenzaMinimi:
		return giorniScadenzaMinimi
	case err != nil, n > giorniScadenzaMassimi:
		return giorniScadenzaMassimi
	}
	return int(n)
}

// interruttore è switch/1: spento solo per esattamente «0»; nessun valore è acceso.
func interruttore(v string, c bool) bool { return !c || v != "0" }

// LeggiImpostazioniHome legge le tre impostazioni, una volta per lista (normalize/1).
func (b *Buste) LeggiImpostazioniHome(ctx context.Context) (ImpostazioniHome, error) {
	var i ImpostazioniHome
	for _, x := range []struct {
		chiave string
		metti  func(string, bool)
	}{
		{ChiaveGiorniScadenza, func(v string, c bool) { i.GiorniScadenza = giorniScadenza(v, c) }},
		{ChiaveMostraPronte, func(v string, c bool) { i.MostraPronte = interruttore(v, c) }},
		{ChiaveMostraConsegnate, func(v string, c bool) { i.MostraConsegnate = interruttore(v, c) }},
	} {
		v, c, err := b.impostazione(ctx, b.d.Lettura, x.chiave)
		if err != nil {
			return ImpostazioniHome{}, err
		}
		x.metti(v, c)
	}
	return i, nil
}

// Contatori sono i riquadri della home (counters/2).
type Contatori struct {
	Pronte, Lavorazione, Preventivo, AttesaMateriali, ConsegnateOggi, Scadenza int64
	CompraOro, AttesaCliente, FuoriBottega, SenzaQR, Messaggi                  int64
}

// RigaLista è una busta della lista, con quello che il suo riquadro mostra.
type RigaLista struct {
	ID               int64
	Numero           string  // il numero interno N/AA
	CodiceQR         *string // nil è NULL
	NomeCliente      string  // la copia salvata sulla busta
	DataAccettazione string  // AAAA-MM-GG
	Descrizione      string
	Oreficeria       *string // il nome, anche della riga 1 (il negozio); nil senza oreficeria
	Stato            string
	CompraOro        bool
	AttesaCliente    bool
	AttesaDal        *string // come sta nel database: un testo
	AttesaMotivo     *string
	Consegna         Consegna
	Fissata          bool
	FuoriBottega     bool
	Urgente          bool // urgent?/3 con la soglia di questa lista
	ultimoMessaggio  *string
	messaggiLetti    *string
	accettato        *string
	accettatoLetto   *string
	rifiutato        *string
	rifiutatoLetto   *string
}

// Conversazione dice se la busta ha una conversazione (last_message_at c'è).
func (r RigaLista) Conversazione() bool { return r.ultimoMessaggio != nil }

// MessaggioNonLetto è Repair.unread_messages?/1.
func (r RigaLista) MessaggioNonLetto() bool { return nonLetto(r.ultimoMessaggio, r.messaggiLetti) }

// AccettazioneNonLetta è Repair.unread_approval?/1.
func (r RigaLista) AccettazioneNonLetta() bool { return nonLetto(r.accettato, r.accettatoLetto) }

// RifiutoNonLetto è Repair.unread_rejection?/1.
func (r RigaLista) RifiutoNonLetto() bool { return nonLetto(r.rifiutato, r.rifiutatoLetto) }

// SenzaQR è Repair.missing_qr?/1.
func (r RigaLista) SenzaQR() bool { return SenzaQR(r.Stato, r.CodiceQR) }

// Chiusa dice se la busta è chiusa (Repair.closed_statuses/0): il riquadro è attenuato.
func (r RigaLista) Chiusa() bool { return Chiuso(r.Stato) }

// NumeroVisibile è il numero da mostrare (D-64) e se è quello di un'etichetta.
func (r RigaLista) NumeroVisibile() (string, bool) {
	qr := ""
	if r.CodiceQR != nil {
		qr = *r.CodiceQR
	}
	return NumeroVisibile(qr, r.Numero)
}

// Lista è la pagina della home (build/2).
type Lista struct {
	Righe     []RigaLista
	Contatori Contatori
	Filtri    Filtri // con la pagina portata dentro i limiti
	Pagine    int64  // almeno 1
	Totale    int64  // le buste della selezione, tutte le pagine
	// GiorniScadenza è la soglia letta per questa lista (contatore, filtro e buste urgenti).
	GiorniScadenza int
	NomeNegozio    string // OnlineSales.shop_name/0, per l'intestazione
}

// statiMaiUrgenti è @never_urgent: pronta e gli stati chiusi.
func maiUrgente(stato string) bool { return stato == "pronta" || Chiuso(stato) }

// Urgente è urgent?/3: una busta né pronta né chiusa con la data di consegna prevista (data
// AAAA-MM-GG, nil se manca: il testo libero non conta) entro oggi più giorni, scadute comprese.
func Urgente(stato string, prevista *string, oggi clock.Data, giorni int) bool {
	if maiUrgente(stato) || prevista == nil {
		return false
	}
	return *prevista <= aggiungiGiorni(oggi, giorni)
}

// aggiungiGiorni è Date.add/2, nella forma AAAA-MM-GG.
func aggiungiGiorni(d clock.Data, giorni int) string {
	return time.Date(d.Anno, d.Mese, d.Giorno+giorni, 12, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// colonneLista sono le colonne di una riga della lista, nell'ordine di scansiona.
const colonneLista = `b.id, b.numero, b.codice_qr, b.nome_cliente, b.data_accettazione, b.descrizione, o.nome, b.stato,
	b.compro_oro, b.attesa_cliente, b.attesa_dal, b.attesa_motivo, b.data_prevista_testo, b.data_prevista,
	b.fissata_il IS NOT NULL, b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL,
	b.ultimo_messaggio_il, b.messaggi_letti_il, b.preventivo_accettato_il, b.preventivo_accettato_letto_il,
	b.preventivo_rifiutato_il, b.preventivo_rifiutato_letto_il`

// ordineLista è order_rows/1: fissate, poi con un messaggio non letto (Repair.unread_messages_order/0),
// poi anno e progressivo del numero letti come interi, poi l'id; tutto a scendere.
const ordineLista = `ORDER BY CASE WHEN b.fissata_il IS NOT NULL THEN 1 ELSE 0 END DESC,
	CASE WHEN b.ultimo_messaggio_il IS NOT NULL AND (b.messaggi_letti_il IS NULL OR b.messaggi_letti_il != b.ultimo_messaggio_il) THEN 1 ELSE 0 END DESC,
	CAST(SUBSTR(b.numero, INSTR(b.numero, '/') + 1) AS INTEGER) DESC,
	CAST(SUBSTR(b.numero, 1, INSTR(b.numero, '/') - 1) AS INTEGER) DESC,
	b.id DESC`

// Le condizioni delle query di Repair, scritte una volta per i filtri e per i contatori.
const (
	// statiChiusiSQL è Repair.closed_statuses/0 dentro un NOT IN.
	statiChiusiSQL = `('consegnata', 'annullata', 'chiusa')`
	// fuoriBottegaSQL è Repair.out_of_shop_query/1.
	fuoriBottegaSQL = `b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL`
	// senzaQRSQL è Repair.missing_qr_query/1 (trim di SQLite, come lo standalone).
	senzaQRSQL = `b.stato NOT IN ` + statiChiusiSQL + ` AND (b.codice_qr IS NULL OR trim(b.codice_qr) = '')`
	// nonLettiSQL è Repair.unread_messages_query/1.
	nonLettiSQL = `b.ultimo_messaggio_il IS NOT NULL AND (b.messaggi_letti_il IS NULL OR b.messaggi_letti_il != b.ultimo_messaggio_il)`
	// scadenzaSQL è il filtro e il contatore «In scadenza»: non chiuse, prevista fra oggi e oggi più
	// la soglia (due argomenti).
	scadenzaSQL = `b.stato NOT IN ` + statiChiusiSQL + ` AND b.data_prevista >= ? AND b.data_prevista <= ?`
)

// condizioni sono il WHERE di apply_filters/3 con i suoi argomenti.
func condizioni(f Filtri, imp ImpostazioniHome, oggi clock.Data) (string, []any) {
	w := []string{"b.cancellata_il IS NULL"}
	var a []any
	// hide_defaults/2: solo senza filtri.
	if !f.Attivo() {
		var nascosti []string
		if !imp.MostraConsegnate {
			nascosti = append(nascosti, "'consegnata'")
		}
		if !imp.MostraPronte {
			nascosti = append(nascosti, "'pronta'")
		}
		if len(nascosti) > 0 {
			w = append(w, "b.stato NOT IN ("+strings.Join(nascosti, ", ")+")")
		}
	}
	if f.Stato != "" {
		w, a = append(w, "b.stato = ?"), append(a, f.Stato)
	}
	if f.CompraOro {
		w = append(w, "b.compro_oro = 1")
	}
	if f.Attesa {
		w = append(w, "b.attesa_cliente = 1")
	}
	if f.FuoriBottega {
		w = append(w, fuoriBottegaSQL)
	}
	if f.SenzaQR {
		w = append(w, senzaQRSQL)
	}
	if f.Messaggi {
		w = append(w, "b.ultimo_messaggio_il IS NOT NULL")
	}
	if f.Scadenza {
		w, a = append(w, scadenzaSQL), append(a, oggi.String(), aggiungiGiorni(oggi, imp.GiorniScadenza))
	}
	return "WHERE " + strings.Join(w, " AND "), a
}

// Lista è build/2: la pagina della lista con i filtri f, i contatori e le buste urgenti, oggi in
// negozio. Non scrive niente: la riga del registro la scrive RegistraLista.
func (b *Buste) Lista(ctx context.Context, f Filtri) (Lista, error) {
	imp, err := b.LeggiImpostazioniHome(ctx)
	if err != nil {
		return Lista{}, err
	}
	oggi := clock.Oggi(b.o)
	where, arg := condizioni(f, imp, oggi)
	l := Lista{GiorniScadenza: imp.GiorniScadenza}
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT count(*) FROM buste b `+where, arg...).Scan(&l.Totale); err != nil {
		return Lista{}, err
	}
	l.Pagine = max(1, (l.Totale+BustePerPagina-1)/BustePerPagina)
	if f.Pagina < 1 {
		f.Pagina = 1
	}
	f.Pagina = min(f.Pagina, l.Pagine)
	l.Filtri = f
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT `+colonneLista+` FROM buste b LEFT JOIN oreficerie o ON o.id = b.oreficeria_id `+
		where+` `+ordineLista+` LIMIT ? OFFSET ?`, append(arg, BustePerPagina, (f.Pagina-1)*BustePerPagina)...)
	if err != nil {
		return Lista{}, err
	}
	defer func() { _ = righe.Close() }()
	for righe.Next() {
		var r RigaLista
		var testo, data sql.NullString
		if err := righe.Scan(&r.ID, &r.Numero, &r.CodiceQR, &r.NomeCliente, &r.DataAccettazione, &r.Descrizione, &r.Oreficeria,
			&r.Stato, &r.CompraOro, &r.AttesaCliente, &r.AttesaDal, &r.AttesaMotivo, &testo, &data, &r.Fissata, &r.FuoriBottega,
			&r.ultimoMessaggio, &r.messaggiLetti, &r.accettato, &r.accettatoLetto, &r.rifiutato, &r.rifiutatoLetto); err != nil {
			return Lista{}, err
		}
		r.Consegna = Consegna{Testo: nullaTesto(testo), Data: nullaTesto(data)}
		r.Urgente = Urgente(r.Stato, r.Consegna.Data, oggi, imp.GiorniScadenza)
		l.Righe = append(l.Righe, r)
	}
	if err := righe.Err(); err != nil {
		return Lista{}, err
	}
	if l.Contatori, err = b.contatori(ctx, oggi, imp.GiorniScadenza); err != nil {
		return Lista{}, err
	}
	if l.NomeNegozio, err = b.nomeNegozio(ctx); err != nil {
		return Lista{}, err
	}
	return l, nil
}

// contatori è counters/2: ognuno con la query del filtro del suo riquadro, fuori dal cestino.
func (b *Buste) contatori(ctx context.Context, oggi clock.Data, giorni int) (Contatori, error) {
	var c Contatori
	entro := aggiungiGiorni(oggi, giorni)
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT
		count(*) FILTER (WHERE b.stato = 'pronta'),
		count(*) FILTER (WHERE b.stato = 'in_lavorazione'),
		count(*) FILTER (WHERE b.stato = 'preventivo'),
		count(*) FILTER (WHERE b.stato = 'in_attesa_materiali'),
		count(*) FILTER (WHERE b.stato = 'consegnata' AND b.data_consegna = ?),
		count(*) FILTER (WHERE `+scadenzaSQL+`),
		count(*) FILTER (WHERE b.compro_oro = 1),
		count(*) FILTER (WHERE b.attesa_cliente = 1 AND b.stato NOT IN `+statiChiusiSQL+`),
		count(*) FILTER (WHERE `+fuoriBottegaSQL+`),
		count(*) FILTER (WHERE `+senzaQRSQL+`),
		count(*) FILTER (WHERE `+nonLettiSQL+` AND b.stato NOT IN ('consegnata', 'annullata'))
		FROM buste b WHERE b.cancellata_il IS NULL`, oggi.String(), oggi.String(), entro).Scan(
		&c.Pronte, &c.Lavorazione, &c.Preventivo, &c.AttesaMateriali, &c.ConsegnateOggi, &c.Scadenza,
		&c.CompraOro, &c.AttesaCliente, &c.FuoriBottega, &c.SenzaQR, &c.Messaggi)
	return c, err
}

// ChiaviRegistroLista sono le chiavi della lista che la riga repair.index_viewed nomina
// (record_repair_index_view/2): mai i valori.
var ChiaviRegistroLista = []string{"cerca", "stato", "oreficeria", "artigiano", "scadenza", "data_da", "data_a", "data_campo", "p"}

// RegistraLista scrive repair.index_viewed per una visita della lista, con i nomi dei filtri usati
// (chiavi: audited_filter_keys/2, già filtrate e in ordine). Se non si scrive, la lista non si
// mostra.
func (b *Buste) RegistraLista(ctx context.Context, chiavi []string, c audit.Contesto) error {
	if chiavi == nil {
		chiavi = []string{}
	}
	return b.reg.Registra(ctx, c, "repair.index_viewed", "repair_index", "index", "ok", map[string]any{"filter_keys": chiavi})
}

// GiorniAttesa è Repair.waiting_days/2 con l'ora del negozio adesso: i giorni interi passati da
// attesa_dal (un testo: «AAAA-MM-GG hh:mm:ss» ora locale, o un giorno «AAAA-MM-GG» a mezzanotte),
// almeno 0; false se il testo non si legge.
func GiorniAttesa(dal string, adesso time.Time) (int64, bool) {
	g := grafemi(strings.TrimFunc(dal, unicode.IsSpace), 19)
	t, ok := time.Time{}, false
	if len(g) > 10 && g[10] == " " {
		t, ok = leggiForma(reIstanteAttesa, "2006-01-02 15:04:05", strings.Join(g, ""))
	}
	if !ok {
		if t, ok = leggiForma(reGiornoAttesa, "2006-01-02", strings.Join(g[:min(10, len(g))], "")); !ok {
			return 0, false
		}
	}
	l := clock.Locale(adesso)
	ora := time.Date(l.Year(), l.Month(), l.Day(), l.Hour(), l.Minute(), l.Second(), 0, time.UTC)
	return max(0, (ora.Unix()-t.Unix())/86400), true
}

// Le due forme che NaiveDateTime.from_iso8601/1 e Date.from_iso8601/1 leggono qui, con le cifre
// fisse (time.Parse da solo accetterebbe un'ora di una cifra).
var (
	reIstanteAttesa = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$`)
	reGiornoAttesa  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// leggiForma legge s con il layout se ha la forma re; il calendario lo controlla time.Parse.
func leggiForma(re *regexp.Regexp, layout, s string) (time.Time, bool) {
	if !re.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(layout, s)
	return t, err == nil
}

// grafemi sono i primi n grafemi di s.
func grafemi(s string, n int) []string {
	var out []string
	g := uniseg.NewGraphemes(s)
	for len(out) < n && g.Next() {
		out = append(out, g.Str())
	}
	return out
}
