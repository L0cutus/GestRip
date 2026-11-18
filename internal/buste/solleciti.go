package buste

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// I solleciti all'artigiano (slice 083, ART-20, ART-21, ART-22, ART-07 contatore), come
// Workshop.ArtisanReminders e Workshop.record_artisan_reminder/4 dello standalone (302439d):
//   - una busta è «in ritardo» se le sue colonne dicono che è ancora fuori (Repair.out_of_shop_query/1:
//     consegnata_artigiano_il presente, rientrata_artigiano_il assente), con una consegna e il suo artigiano, e
//     l'uscita è di almeno N giorni fa (il limite compreso). Niente audit: conta quello che la riga dice oggi.
//     Le buste nel cestino si escludono: late_query/3 parte da Repair e Repo.prepare_query/3 aggiunge
//     deleted_at IS NULL (097b, 18/11/2026); vale per la pagina, il contatore, il sollecito e il lavoro (096);
//   - la soglia (threshold_days/1): il parametro giorni se non è vuoto (dopo String.trim/1), altrimenti
//     l'impostazione solleciti_artigiano_giorni; poi Integer.parse/1 per intero, max(1, n); un testo che non è
//     un numero, o nessuna impostazione, dà 14, anche se l'impostazione c'è (copiato dal Flask);
//   - i giorni fuori sono giorni interi di 86 400 secondi, troncati (DateTime.diff(…, :day)): non dipendono
//     dall'ora del negozio; il confine è adesso meno N volte 86 400 secondi;
//   - i gruppi seguono l'artigiano della consegna (non buste.artigiano_id), per nome e poi per uscita;
//   - il sollecito rilegge le buste in ritardo dell'artigiano nella transazione che scrive, all'istante della
//     scrittura: una riga solleciti_artigiani e una repair.artisan_reminded per busta; nessuna busta → il
//     rifiuto, niente scritto.

// ChiaveSollecitiGiorni è l'impostazione della soglia (@setting_key).
const ChiaveSollecitiGiorni = "solleciti_artigiano_giorni"

// GiorniSollecitiPredefiniti è @default_days.
const GiorniSollecitiPredefiniti int64 = 14

// giorniSenzaConfine: oltre questa soglia (circa 270 anni) nessuna busta può essere uscita prima del confine;
// serve solo a non far traboccare time.Duration.
const giorniSenzaConfine = 100000

// ErrNessunaBustaDaSollecitare è il rifiuto del sollecito (:no_bags_out), con il messaggio della pagina.
const ErrNessunaBustaDaSollecitare Rifiuto = "Nessuna busta da sollecitare per questo artigiano."

// giorniDaTesto è days/1: Integer.parse/1 del testo senza spazi ai bordi, per intero (segno compreso, solo
// cifre ASCII), poi max(1, n). Un numero oltre i 64 bit vale il massimo (lo standalone non ha limiti).
func giorniDaTesto(s string) (int64, bool) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	negativo := false
	switch {
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	case strings.HasPrefix(s, "-"):
		s, negativo = s[1:], true
	}
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		if n <= (1<<63-1-9)/10 {
			n = n*10 + int64(c-'0')
		} else {
			n = 1<<63 - 1
		}
	}
	if negativo || n < 1 {
		return 1, true
	}
	return n, true
}

// SogliaSolleciti è threshold_days/1: giorni è il parametro della pagina ("" se manca).
func (b *Buste) SogliaSolleciti(ctx context.Context, giorni string) (int64, error) {
	return b.soglia(ctx, b.d.Lettura, giorni)
}

func (b *Buste) soglia(ctx context.Context, q lettore, giorni string) (int64, error) {
	return sogliaSolleciti(ctx, q, giorni)
}

// sogliaSolleciti è threshold_days/1 letta con q: la pagina e il lavoro dei solleciti (096) la leggono da qui.
func sogliaSolleciti(ctx context.Context, q lettore, giorni string) (int64, error) {
	grezzo := strings.TrimFunc(giorni, unicode.IsSpace)
	if grezzo == "" {
		v, ok, err := leggiImpostazione(ctx, q, ChiaveSollecitiGiorni)
		if err != nil {
			return 0, err
		}
		if !ok {
			return GiorniSollecitiPredefiniti, nil
		}
		grezzo = v
	}
	if n, ok := giorniDaTesto(grezzo); ok {
		return n, nil
	}
	return GiorniSollecitiPredefiniti, nil
}

// BustaInRitardo è una busta dei solleciti.
type BustaInRitardo struct {
	ID              int64
	Numero          string
	CodiceQR        *string
	Cliente         string // la copia del nome sulla busta (il briefing la scrive, slice 097)
	Descrizione     string
	Uscita          time.Time
	Consegna        int64
	GiorniFuori     int64
	UltimoSollecito *time.Time
}

// Visibile è il numero da mostrare (D-64).
func (x BustaInRitardo) Visibile() Mostrato { return mostratoDi(x.CodiceQR, x.Numero) }

// GruppoSolleciti è un artigiano con le sue buste in ritardo; UltimoSollecito è il più recente delle sue buste.
type GruppoSolleciti struct {
	ArtigianoID     int64
	Artigiano       string
	Telefono        *string
	Buste           []BustaInRitardo
	UltimoSollecito *time.Time
}

// Solleciti è la pagina: la soglia usata, i gruppi e quante buste in tutto.
type Solleciti struct {
	Giorni int64
	Gruppi []GruppoSolleciti
	Totale int
}

// confineSolleciti è il cutoff di late_query/3 come istante salvato; "" vuol dire che nessuna busta lo passa.
func confineSolleciti(adesso time.Time, giorni int64) string {
	if giorni > giorniSenzaConfine {
		return ""
	}
	return adesso.UTC().Add(-time.Duration(giorni) * 24 * time.Hour).Format(db.FormatoIstante)
}

// sqlRitardo è late_query/3: ?1 il confine, ?2 l'artigiano (0: tutti). Fuori il cestino (prepare_query/3).
const sqlRitardo = `FROM buste b JOIN consegne_artigiani c ON c.id = b.consegna_artigiano_id JOIN artigiani a ON a.id = c.artigiano_id
	WHERE b.cancellata_il IS NULL AND b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL AND b.consegnata_artigiano_il <= ?1
	  AND (?2 = 0 OR c.artigiano_id = ?2)`

// busteInRitardo è late_bags/3, con i gruppi già fatti (chunk_by sull'artigiano, nell'ordine della query).
func busteInRitardo(ctx context.Context, q interrogatore, adesso time.Time, giorni, artigiano int64) ([]GruppoSolleciti, int, error) {
	return busteInRitardoDove(ctx, q, adesso, giorni, artigiano, "")
}

// busteInRitardoDove è busteInRitardo ristretta da altro (" AND …" sulle colonne di b, o ""): late_query/3
// ristretta da chi la usa, senza una seconda copia della regola (il lavoro dei solleciti, 096).
func busteInRitardoDove(ctx context.Context, q interrogatore, adesso time.Time, giorni, artigiano int64, altro string) ([]GruppoSolleciti, int, error) {
	confine := confineSolleciti(adesso, giorni)
	if confine == "" {
		return nil, 0, nil
	}
	rr, err := q.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.nome_cliente, b.descrizione, b.consegnata_artigiano_il, b.consegna_artigiano_id,
		a.id, a.nome, a.telefono `+sqlRitardo+altro+` ORDER BY a.nome, b.consegnata_artigiano_il, b.id`, confine, artigiano)
	if err != nil {
		return nil, 0, err
	}
	defer rr.Close()
	var gg []GruppoSolleciti
	n := 0
	for rr.Next() {
		var (
			x        BustaInRitardo
			uscita   string
			g        GruppoSolleciti
			telefono sql.NullString
		)
		if err := rr.Scan(&x.ID, &x.Numero, &x.CodiceQR, &x.Cliente, &x.Descrizione, &uscita, &x.Consegna, &g.ArtigianoID, &g.Artigiano, &telefono); err != nil {
			return nil, 0, err
		}
		if x.Uscita, err = time.Parse(db.FormatoIstante, uscita); err != nil {
			return nil, 0, err
		}
		x.GiorniFuori = int64(adesso.Sub(x.Uscita) / (24 * time.Hour))
		if len(gg) == 0 || gg[len(gg)-1].ArtigianoID != g.ArtigianoID {
			g.Telefono = nullaOTesto(telefono)
			gg = append(gg, g)
		}
		gg[len(gg)-1].Buste = append(gg[len(gg)-1].Buste, x)
		n++
	}
	return gg, n, rr.Err()
}

// SollecitiPendenti è ArtisanReminders.build/2: giorni è il parametro della pagina ("" se manca).
func (b *Buste) SollecitiPendenti(ctx context.Context, giorni string) (Solleciti, error) {
	adesso := b.o.Adesso()
	g, err := b.soglia(ctx, b.d.Lettura, giorni)
	if err != nil {
		return Solleciti{}, err
	}
	gg, n, err := busteInRitardo(ctx, b.d.Lettura, adesso, g, 0)
	if err != nil {
		return Solleciti{}, err
	}
	// last_reminders/1: il sollecito più recente di ogni busta, in una lettura sola sulle stesse buste.
	ultimi := map[int64]*time.Time{}
	if confine := confineSolleciti(adesso, g); confine != "" && n > 0 {
		rr, err := b.d.Lettura.QueryContext(ctx, `SELECT busta_id, max(creato_il) FROM solleciti_artigiani
			WHERE busta_id IN (SELECT b.id `+sqlRitardo+`) GROUP BY busta_id`, confine, 0)
		if err != nil {
			return Solleciti{}, err
		}
		defer rr.Close()
		for rr.Next() {
			var (
				id     int64
				ultimo string
			)
			if err := rr.Scan(&id, &ultimo); err != nil {
				return Solleciti{}, err
			}
			if ultimi[id], err = istante(&ultimo); err != nil {
				return Solleciti{}, err
			}
		}
		if err := rr.Err(); err != nil {
			return Solleciti{}, err
		}
	}
	for i := range gg {
		for j := range gg[i].Buste {
			x := &gg[i].Buste[j]
			x.UltimoSollecito = ultimi[x.ID]
			if x.UltimoSollecito != nil && (gg[i].UltimoSollecito == nil || x.UltimoSollecito.After(*gg[i].UltimoSollecito)) {
				gg[i].UltimoSollecito = x.UltimoSollecito
			}
		}
	}
	return Solleciti{Giorni: g, Gruppi: gg, Totale: n}, nil
}

// ContaSollecitiPendenti è pending_count/1: le buste in ritardo con la soglia dell'impostazione, per il
// contatore della lista delle consegne.
func (b *Buste) ContaSollecitiPendenti(ctx context.Context) (int64, error) {
	g, err := b.soglia(ctx, b.d.Lettura, "")
	if err != nil {
		return 0, err
	}
	confine := confineSolleciti(b.o.Adesso(), g)
	if confine == "" {
		return 0, nil
	}
	var n int64
	err = b.d.Lettura.QueryRowContext(ctx, `SELECT count(*) `+sqlRitardo, confine, 0).Scan(&n)
	return n, err
}

// RegistraVistaSolleciti è record_artisan_reminders_view/3: la soglia e quante buste, mai un numero di busta.
func (b *Buste) RegistraVistaSolleciti(ctx context.Context, giorni int64, quante int, c audit.Contesto) error {
	return b.reg.Registra(ctx, c, "handover.reminders_viewed", "artisan_reminders", "index", "ok",
		map[string]any{"days": giorni, "count": quante})
}

// RegistraSollecito è record_artisan_reminder/4: giorni è il parametro della pagina che l'operatore guarda
// ("" se manca). Ritorna il gruppo sollecitato, con le buste rilette nella transazione (il testo WhatsApp si fa
// da queste).
func (b *Buste) RegistraSollecito(ctx context.Context, artigiano int64, giorni string, c audit.Contesto) (GruppoSolleciti, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return GruppoSolleciti{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	adesso := b.o.Adesso()
	g, err := b.soglia(ctx, tx, giorni)
	if err != nil {
		return GruppoSolleciti{}, err
	}
	if artigiano <= 0 {
		return GruppoSolleciti{}, ErrNessunaBustaDaSollecitare
	}
	gg, _, err := busteInRitardo(ctx, tx, adesso, g, artigiano)
	if err != nil {
		return GruppoSolleciti{}, err
	}
	if len(gg) == 0 {
		return GruppoSolleciti{}, ErrNessunaBustaDaSollecitare
	}
	gruppo := gg[0]
	var attore any
	if c.Attore != "" {
		attore = c.Attore
	}
	ora := adesso.UTC().Format(db.FormatoIstante)
	for _, x := range gruppo.Buste {
		if _, err := tx.ExecContext(ctx, `INSERT INTO solleciti_artigiani (busta_id, artigiano_id, consegna_artigiano_id, attore, giorni_fuori, creato_il)
			VALUES (?, ?, ?, ?, ?, ?)`, x.ID, gruppo.ArtigianoID, x.Consegna, attore, x.GiorniFuori, ora); err != nil {
			return GruppoSolleciti{}, err
		}
		if err := b.reg.Scrivi(ctx, tx, c, "repair.artisan_reminded", "repair", x.ID, "ok", map[string]any{
			"artisan_id": gruppo.ArtigianoID, "handover_id": x.Consegna, "days_out": x.GiorniFuori}); err != nil {
			return GruppoSolleciti{}, err
		}
	}
	return gruppo, tx.Commit()
}

// SollecitoDellaBusta è una riga del riquadro dei solleciti della scheda.
type SollecitoDellaBusta struct {
	Il          time.Time
	GiorniFuori int64
	Attore      *string
}

// SollecitiDellaBusta è artisan_reminder_history/1: tutti i solleciti della busta, dal più recente.
func (b *Buste) SollecitiDellaBusta(ctx context.Context, id int64) ([]SollecitoDellaBusta, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT creato_il, giorni_fuori, attore FROM solleciti_artigiani WHERE busta_id = ?
		ORDER BY creato_il DESC, id DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []SollecitoDellaBusta
	for rr.Next() {
		var (
			s      SollecitoDellaBusta
			il     string
			attore sql.NullString
		)
		if err := rr.Scan(&il, &s.GiorniFuori, &attore); err != nil {
			return nil, err
		}
		if s.Il, err = time.Parse(db.FormatoIstante, il); err != nil {
			return nil, err
		}
		s.Attore = nullaOTesto(attore)
		out = append(out, s)
	}
	return out, rr.Err()
}
