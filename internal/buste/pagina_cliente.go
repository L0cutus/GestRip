package buste

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// La pagina del cliente /r/<token> (CLI-01, CLI-02, CLI-03, CLI-28), come show/2,
// with_public_repair/3 e token_page/1 di PublicRepairController, Workshop.get_public_repair/1 e
// RepairEstimate.for_customer/2 dello standalone (go-baseline). Il token è tutta
// l'autorizzazione: si cerca per l'impronta SHA-256 del testo arrivato (hash_token/1), con la
// scadenza dopo adesso, senza revoca e fuori dal cestino. Vale per i token emessi qui (crea.go,
// NuovoLink) e per quelli emessi dallo standalone e portati dall'import, che hanno la stessa
// impronta: un link già mandato a un cliente continua a funzionare finché non scade o non si revoca.
// Token sbagliato, scaduto, revocato o busta nel cestino sono un solo ErrBustaNonTrovata.
// Aprire la pagina non scrive niente (show/2 non scrive), e una pagina pubblica non emette mai un
// link (CLI-22).

// PaginaCliente è quello che la pagina mostra, e nient'altro: mai il nome o il telefono del
// cliente, mai i recapiti del negozio, mai il token.
type PaginaCliente struct {
	// ID e Oreficeria servono alle parti della pagina delle slice dopo (foto «cliente» o
	// «orefice», preventivo, conversazione, riquadri email e Telegram): la pagina non li stampa.
	ID         int64
	Oreficeria bool // busta di un'oreficeria esterna: chi ha il link è l'oreficeria (Recipient, CLI-28)

	NomeNegozio      string  // OnlineSales.shop_name/0
	Numero           string  // il numero visibile (D-64): «0045/26» o «N. 45/26»
	Stato            string  // status_label/1 di PublicRepairHTML
	DataAccettazione string  // AAAA-MM-GG, come la scrive la pagina dello standalone
	Consegna         string  // delivery/1: il testo scritto, o la data AAAA-MM-GG; "" senza
	Stima            *Stima  // «Tempo indicativo», nil se non si mostra (CLI-03)
	DataPronta       *string // «Pronta dal», AAAA-MM-GG
	DataConsegna     *string // «Consegnata», AAAA-MM-GG
	Descrizione      string
	NotePubbliche    *string // «Nota del laboratorio»
	Preventivo       string  // estimate/1: il testo com'è, o «€ 12,50»; "" senza
	PrezzoFinale     string  // final_price/1: Money.label/1; "" senza
	// Risposta è public_state/1 del preventivo (CLI-05): fuori da «preventivo» non applicabile.
	Risposta StatoRisposta
	// Foto sono le foto visibili a chi ha il link (visible_for/2 con il pubblico del link: «orefice»
	// su una busta B2B, «customer» altrimenti), nell'ordine (CLI-04).
	Foto []Foto
	// ConConversazione dice se la pagina mostra la conversazione (public_thread/2: no su una busta
	// consegnata, annullata o chiusa); Conversazione sono i suoi messaggi, dal più vecchio (CLI-10).
	ConConversazione bool
	Conversazione    []Messaggio
}

// etichetteStatoCliente è status_label/1 di PublicRepairHTML: le parole della pagina del cliente.
var etichetteStatoCliente = map[string]string{
	"in_lavorazione":      "In lavorazione",
	"in_attesa_materiali": "In attesa materiali",
	"preventivo":          "Preventivo in attesa",
	"pronta":              "Pronta per il ritiro",
	"consegnata":          "Consegnata",
	"annullata":           "Annullata",
	"in_valutazione":      "In valutazione",
	"valutata":            "Valutata",
	"perfezionata":        "Perfezionata",
	"chiusa":              "Chiusa",
}

// ImprontaToken è hash_token/1: lo SHA-256 del testo del token così com'è arrivato.
func ImprontaToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// PaginaCliente legge la pagina del link token (get_public_repair/1 e token_page/1), senza
// scrivere niente.
func (b *Buste) PaginaCliente(ctx context.Context, token string) (PaginaCliente, error) {
	if token == "" {
		return PaginaCliente{}, ErrBustaNonTrovata
	}
	_, ora := b.adesso()
	var p PaginaCliente
	var oro int
	var qr, consegnaTesto, consegnaData, preventivoTesto, prezzoTesto, consegnataArtigiano sql.NullString
	var preventivoCent, prezzoCent, oreficeria sql.NullInt64
	var approvato, rifiutato, motivo, visibiliDal sql.NullString
	var stato string
	var giorni int64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, numero, codice_qr, compro_oro, stato, data_accettazione,
		data_prevista_testo, data_prevista, data_pronta, data_consegna, descrizione, note_pubbliche,
		preventivo_testo, preventivo_centesimi, prezzo_testo, prezzo_centesimi, oreficeria_id, giorni_in_piu,
		consegnata_artigiano_il, preventivo_accettato_il, preventivo_rifiutato_il, preventivo_rifiuto_motivo,
		messaggi_visibili_dal
		FROM buste WHERE hash_token_pubblico = ? AND token_pubblico_revocato_il IS NULL
			AND token_pubblico_scade_il > ? AND cancellata_il IS NULL`, ImprontaToken(token), ora).Scan(
		&p.ID, &p.Numero, &qr, &oro, &stato, &p.DataAccettazione, &consegnaTesto, &consegnaData, &p.DataPronta,
		&p.DataConsegna, &p.Descrizione, &p.NotePubbliche, &preventivoTesto, &preventivoCent, &prezzoTesto, &prezzoCent,
		&oreficeria, &giorni, &consegnataArtigiano, &approvato, &rifiutato, &motivo, &visibiliDal)
	if errors.Is(err, sql.ErrNoRows) {
		return PaginaCliente{}, ErrBustaNonTrovata
	}
	if err != nil {
		return PaginaCliente{}, err
	}
	p.Numero, _ = NumeroVisibile(qr.String, p.Numero)
	p.Stato = etichetteStatoCliente[stato]
	if p.Stato == "" {
		p.Stato = stato // uno stato che la pagina dello standalone non conosce (lì sarebbe un 500)
	}
	consegna := Consegna{Testo: nullaTesto(consegnaTesto), Data: nullaTesto(consegnaData)}
	switch {
	case consegna.Testo != nil && *consegna.Testo != "":
		p.Consegna = *consegna.Testo
	case consegna.Data != nil:
		p.Consegna = *consegna.Data
	}
	preventivo := Prezzo{Testo: nullaTesto(preventivoTesto), Centesimi: nullaIntero(preventivoCent)}
	if preventivo.Testo != nil && *preventivo.Testo != "" {
		p.Preventivo = *preventivo.Testo // estimate/1: il testo com'è, senza «€»
	} else {
		p.Preventivo = EtichettaPrezzo(preventivo)
	}
	p.PrezzoFinale = EtichettaPrezzo(Prezzo{Testo: nullaTesto(prezzoTesto), Centesimi: nullaIntero(prezzoCent)})
	p.Oreficeria = oreficeria.Valid && oreficeria.Int64 > 1
	ia, err := istante(nullaTesto(approvato))
	if err != nil {
		return PaginaCliente{}, err
	}
	ir, err := istante(nullaTesto(rifiutato))
	if err != nil {
		return PaginaCliente{}, err
	}
	p.Risposta = statoRispostaPubblico(stato, ia, ir, nullaTesto(motivo), p.Preventivo != "")
	t := bustaDelToken{id: p.ID, oreficeriaID: nullaIntero(oreficeria)}
	if p.Foto, err = b.VisibiliPer(ctx, BustaDellaFoto{ID: t.id, OreficeriaID: t.oreficeriaID}, t.pubblico()); err != nil {
		return PaginaCliente{}, err
	}
	if p.Conversazione, p.ConConversazione, err = b.conversazionePubblica(ctx, p.ID, stato, t.oreficeriaID, nullaTesto(visibiliDal)); err != nil {
		return PaginaCliente{}, err
	}
	if p.Stima, err = b.stimaPerIlCliente(ctx, oro == 1, stato, p.Consegna != "", consegnataArtigiano.Valid, giorni); err != nil {
		return PaginaCliente{}, err
	}
	if p.NomeNegozio, err = b.nomeNegozio(ctx); err != nil {
		return PaginaCliente{}, err
	}
	return p, nil
}

// Stima è l'intervallo di giorni della stima dei tempi (RepairEstimate, slice 158 dello standalone).
type Stima struct{ Min, Max int64 }

// Frase è RepairEstimate.phrase/1.
func (s Stima) Frase() string {
	return fmt.Sprintf("Pronta di solito in %d-%d giorni dal ritiro", s.Min, s.Max)
}

const (
	chiaveStimaAttiva      = "stima_tempi_attiva"
	chiaveStimaArtigianoMn = "stima_tempi_artigiano_min"
	chiaveStimaArtigianoMx = "stima_tempi_artigiano_max"
	stimaFinestraGiorni    = 365
	stimaGiorniMassimi     = 90
	stimaMinimoBottega     = 20
	stimaMinimoArtigiano   = 10
)

// statiConStima sono @shown_statuses: le parole di una riparazione aperta.
var statiConStima = []string{"in_lavorazione", "preventivo", "in_attesa_materiali"}

// impostazioniStima è Settings.Estimate.read/0.
type impostazioniStima struct {
	attiva         bool
	artMin, artMax int64
}

// giorniStima è days/2 di Settings.Estimate: Integer.parse/1 senza resto (segno facoltativo, solo
// cifre ASCII), stretto fra 0 e 90; altrimenti il valore di partenza.
func giorniStima(v *string, partenza int64) int64 {
	if v == nil || !reInteroElixir.MatchString(*v) {
		return partenza
	}
	n, err := strconv.ParseInt(*v, 10, 64)
	if err != nil { // oltre i 64 bit: Elixir non ha limiti, e lo stringe a 0 o a 90
		if (*v)[0] == '-' {
			return 0
		}
		return stimaGiorniMassimi
	}
	return min(max(n, 0), stimaGiorniMassimi)
}

// reInteroElixir è un intero che Integer.parse/1 legge senza resto.
var reInteroElixir = regexp.MustCompile(`^[+-]?[0-9]+$`)

func (b *Buste) leggiImpostazioniStima(ctx context.Context) (impostazioniStima, error) {
	leggi := func(k string) (*string, error) {
		var v string
		switch err := b.d.Lettura.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v); {
		case errors.Is(err, sql.ErrNoRows):
			return nil, nil
		case err != nil:
			return nil, err
		}
		return &v, nil
	}
	attiva, err := leggi(chiaveStimaAttiva)
	if err != nil {
		return impostazioniStima{}, err
	}
	mn, err := leggi(chiaveStimaArtigianoMn)
	if err != nil {
		return impostazioniStima{}, err
	}
	mx, err := leggi(chiaveStimaArtigianoMx)
	if err != nil {
		return impostazioniStima{}, err
	}
	s := impostazioniStima{attiva: attiva != nil && *attiva == "1", artMin: giorniStima(mn, 7), artMax: giorniStima(mx, 8)}
	s.artMax = max(s.artMax, s.artMin)
	return s, nil
}

// stimaPerIlCliente è RepairEstimate.for_customer/2: niente per una busta che non è una riparazione
// aperta, con l'interruttore spento o con una data di consegna scritta dal negozio (la promessa
// del negozio vince sulla media); altrimenti for_repair/2 sul modello di oggi.
func (b *Buste) stimaPerIlCliente(ctx context.Context, compraOro bool, stato string, conConsegna, artigiano bool, giorni int64) (*Stima, error) {
	if compraOro || !slices.Contains(statiConStima, stato) {
		return nil, nil
	}
	imp, err := b.leggiImpostazioniStima(ctx)
	if err != nil || !imp.attiva || conConsegna {
		return nil, err
	}
	m, err := b.modelloStima(ctx, imp)
	if err != nil || m.base == nil {
		return nil, err
	}
	add := [2]int64{}
	if artigiano {
		add = m.supplemento
	}
	return &Stima{Min: m.base[0] + add[0] + giorni, Max: m.base[1] + add[1] + giorni}, nil
}

// modelloStima è RepairEstimate.model/1, solo quello che serve al cliente.
type modelloStima struct {
	base        *[2]int64
	supplemento [2]int64
}

// modelloStima legge le buste (non compro oro, fuori dal cestino) pronte negli ultimi 365 giorni
// con una durata da 0 a 90 giorni, divise fra quelle restate in bottega e quelle passate
// dall'artigiano (handed_over_at).
func (b *Buste) modelloStima(ctx context.Context, imp impostazioniStima) (modelloStima, error) {
	oggi := clock.Oggi(b.o)
	da := time.Date(oggi.Anno, time.Month(oggi.Mese), oggi.Giorno, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -stimaFinestraGiorni)
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT data_accettazione, data_pronta, consegnata_artigiano_il IS NOT NULL
		FROM buste WHERE compro_oro = 0 AND cancellata_il IS NULL AND data_pronta IS NOT NULL
			AND data_pronta >= ? AND data_pronta <= ?`, da.Format(time.DateOnly), oggi.String())
	if err != nil {
		return modelloStima{}, err
	}
	defer func() { _ = righe.Close() }()
	var bottega, artigiano []int64
	for righe.Next() {
		var acc, pronta string
		var art bool
		if err := righe.Scan(&acc, &pronta, &art); err != nil {
			return modelloStima{}, err
		}
		a, err1 := time.Parse(time.DateOnly, acc)
		p, err2 := time.Parse(time.DateOnly, pronta)
		if err1 != nil || err2 != nil {
			continue
		}
		g := int64(p.Sub(a).Hours() / 24)
		if g < 0 || g > stimaGiorniMassimi {
			continue
		}
		if art {
			artigiano = append(artigiano, g)
		} else {
			bottega = append(bottega, g)
		}
	}
	if err := righe.Err(); err != nil {
		return modelloStima{}, err
	}
	m := modelloStima{supplemento: [2]int64{imp.artMin, imp.artMax}}
	if len(bottega) >= stimaMinimoBottega {
		base := intervalloStima(bottega)
		m.base = &base
		if len(artigiano) >= stimaMinimoArtigiano {
			a := intervalloStima(artigiano)
			m.supplemento = [2]int64{max(a[0]-base[0], 0), max(a[1]-base[1], 0)}
		}
	}
	return m, nil
}

// intervalloStima è range/1: dal 25° percentile in giù al 75° in su, per interpolazione lineare fra
// i ranghi, largo almeno un giorno.
func intervalloStima(durate []int64) [2]int64 {
	s := slices.Clone(durate)
	slices.Sort(s)
	basso := int64(math.Floor(percentileStima(s, 0.25)))
	alto := int64(math.Ceil(percentileStima(s, 0.75)))
	return [2]int64{basso, max(alto, basso+1)}
}

func percentileStima(s []int64, frazione float64) float64 {
	rango := frazione * float64(len(s)-1)
	sotto := int(math.Floor(rango))
	inf := float64(s[sotto])
	sup := float64(s[min(sotto+1, len(s)-1)])
	return inf + (rango-float64(sotto))*(sup-inf)
}
