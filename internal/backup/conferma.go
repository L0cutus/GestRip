package backup

// La conferma «questa è ora la macchina vera» dopo un ripristino isolato (slice 131, OPS-19, D-12, D-118). Lo
// standalone non ha una conferma: per un disastro rifà il ripristino con --live e i messaggi in coda partono al
// riavvio senza chiedere. Il Go la dà al titolare (pagina Impostazioni → Backup → ripristino, o gestrip
// conferma-ripristino dalla console): prima mostra i messaggi rimasti in coda, ognuno con «manda» o «scarta»
// (proposta: scarta, D-118 (b)), poi, in una transazione sola, scarta quelli scelti, rimette email, Telegram e copia
// remota come li aveva il backup (Isolamento.Spento), toglie lo stato isolato e scrive backup.restore_confirmed
// (D-118 (e)); segna anche come già fatti oggi i lavori del pianificatore già dovuti a quell'ora (D-119,
// jobs.SegnaDovutiComeFatti), così il primo giro del serve riavviato non rifà quello che la macchina vecchia ha fatto
// dopo il backup. Tutto o niente: una conferma a metà non lascia canali accesi con la copia ancora isolata, né il
// contrario. Il serve che gira resta isolato fino al riavvio (lo dice chi chiama): solo un serve nuovo rilegge lo
// stato e accende coda, lettore Telegram, lavori automatici e backup.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/jobs"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// ParolaDiConferma è la parola che il titolare scrive per confermare (maiuscole o minuscole, senza spazi ai bordi).
const ParolaDiConferma = "CONFERMO"

// Rifiuti della conferma: niente è cambiato.
var (
	// ErrNonIsolata: il database non è una copia ripristinata ancora da confermare.
	ErrNonIsolata = errors.New("questa non è una copia ripristinata da confermare")
	// ErrParolaSbagliata: la parola scritta non è ParolaDiConferma.
	ErrParolaSbagliata = errors.New("conferma non data: per confermare va scritta la parola " + ParolaDiConferma)
	// ErrCodaCambiata: le scelte non sono esattamente una per ogni messaggio in coda adesso (ne è arrivato uno dopo
	// che la pagina li ha mostrati, uno è sparito, uno è scelto due volte).
	ErrCodaCambiata = errors.New("i messaggi in coda sono cambiati: ricontrollali e conferma di nuovo")
)

// MessaggioInCoda è un messaggio rimasto in coda (stato queued) come la conferma lo mostra al titolare: dati del
// cliente, solo per la pagina del titolare e per la console, mai per un registro.
type MessaggioInCoda struct {
	ID                   int64
	Canale, Destinatario string // email o telegram; shop, customer, oreficeria, artisan
	Indirizzo, Oggetto   string
	Testo                string
	AccodatoIl           time.Time
	BustaID              *int64 // nil se il messaggio non è di una busta
	NumeroBusta          string // "" se il messaggio non è di una busta
}

// StatoRipristino è quello che la conferma mostra prima di chiedere la parola.
type StatoRipristino struct {
	Isolata    bool
	Isolamento db.Isolamento
	// DaRiaccendere sono, in parole, i canali che il backup aveva accesi («email», «Telegram», «copia remota»).
	DaRiaccendere []string
	Coda          []MessaggioInCoda
}

// SceltaConferma è la conferma del titolare: la parola e, per ogni messaggio in coda, «manda» (resta in coda e parte
// al primo giro del serve riavviato, se il suo canale è acceso) o «scarta» (va via dalla coda).
type SceltaConferma struct {
	Parola        string
	Manda, Scarta []int64
}

// EsitoConferma è quello che la conferma ha fatto.
type EsitoConferma struct {
	Isolamento db.Isolamento
	// Riaccesi sono, in parole, i canali rimessi com'erano nel backup.
	Riaccesi          []string
	Mandati, Scartati int
	// LavoriSegnati sono i lavori del pianificatore segnati come già fatti oggi (D-119), nell'ordine di jobs.Nomi.
	LavoriSegnati []string
}

// ParoleRiaccese dice in parole quali delle impostazioni messe da parte dal ripristino sono «accese»: email e
// Telegram se valevano «1», la copia remota se era configurata.
func ParoleRiaccese(spento map[string]string) []string {
	var p []string
	if spento[uscita.ChiaveEmailAcceso] == "1" {
		p = append(p, "email")
	}
	if spento[uscita.ChiaveTelegramAcceso] == "1" {
		p = append(p, "Telegram")
	}
	if _, ok := spento[ChiaveRemoto]; ok {
		p = append(p, "copia remota")
	}
	return p
}

// LettoreRipristino è dove la conferma legge: il collegamento di lettura o la sua transazione.
type LettoreRipristino interface {
	db.Interrogatore
	db.Elencatore
}

// LeggiStatoRipristino legge lo stato della copia e i messaggi in coda. Uno stato isolato che non si legge è
// l'errore di db.LeggiIsolamento.
func LeggiStatoRipristino(ctx context.Context, q LettoreRipristino) (StatoRipristino, error) {
	iso, isolata, err := db.LeggiIsolamento(ctx, q)
	if err != nil || !isolata {
		return StatoRipristino{}, err
	}
	coda, err := messaggiInCoda(ctx, q)
	if err != nil {
		return StatoRipristino{}, err
	}
	return StatoRipristino{Isolata: true, Isolamento: iso, DaRiaccendere: ParoleRiaccese(iso.Spento), Coda: coda}, nil
}

func messaggiInCoda(ctx context.Context, q LettoreRipristino) ([]MessaggioInCoda, error) {
	rr, err := q.QueryContext(ctx, `SELECT m.id, m.busta_id, m.canale, m.destinatario, m.indirizzo, coalesce(m.oggetto, ''), m.testo, m.accodato_il,
		coalesce((SELECT b.numero FROM buste b WHERE b.id = m.busta_id), '')
		FROM messaggi_in_uscita m WHERE m.stato = 'queued' ORDER BY m.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rr.Close() }()
	var out []MessaggioInCoda
	for rr.Next() {
		var m MessaggioInCoda
		var accodato string
		if err := rr.Scan(&m.ID, &m.BustaID, &m.Canale, &m.Destinatario, &m.Indirizzo, &m.Oggetto, &m.Testo, &accodato, &m.NumeroBusta); err != nil {
			return nil, err
		}
		if m.AccodatoIl, err = time.Parse(db.FormatoIstante, accodato); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rr.Err()
}

// scelteGiuste dice se manda e scarta sono, insieme, esattamente gli id in coda, ognuno una volta sola.
func scelteGiuste(coda []MessaggioInCoda, manda, scarta []int64) bool {
	scelti := slices.Concat(manda, scarta)
	if len(scelti) != len(coda) {
		return false
	}
	slices.Sort(scelti)
	for i, m := range coda { // coda è in ordine di id
		if scelti[i] != m.ID {
			return false
		}
	}
	return true
}

// ConfermaRipristino è la conferma del titolare, in una transazione sola (lo scrittore è IMMEDIATE: nessun altro
// scrive fra la lettura della coda e il commit):
//   - la copia deve essere isolata (ErrNonIsolata), la parola giusta (ErrParolaSbagliata) e le scelte una per ogni
//     messaggio in coda adesso (ErrCodaCambiata), prima di toccare qualcosa;
//   - i messaggi da scartare vanno via dalla coda, ognuno con outgoing.queued_discarded (canale, parte, busta, il
//     motivo; mai testo né indirizzo), come lo scarto a mano della D-27; quelli da mandare restano in coda;
//   - le impostazioni messe da parte dal ripristino tornano con il valore del backup, ma solo se mancano: un
//     interruttore che il titolare ha toccato durante l'isolamento resta com'è (la scelta più prudente);
//   - la riga dello stato isolato si toglie;
//   - i lavori del pianificatore già dovuti adesso (ritardi, solleciti, briefing) si segnano come già fatti oggi
//     (jobs.SegnaDovutiComeFatti, D-119): uno non ancora dovuto parte alla sua ora dopo il riavvio;
//   - backup.restore_confirmed con archivio, impronta, ora di cattura e del ripristino, cosa si è riacceso, i
//     conteggi delle scelte e dei lavori segnati.
//
// Un errore in uno di questi passi annulla tutto.
func ConfermaRipristino(ctx context.Context, d *db.DB, o clock.Orologio, s SceltaConferma, chi audit.Contesto) (EsitoConferma, error) {
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return EsitoConferma{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	iso, isolata, err := db.LeggiIsolamento(ctx, tx)
	switch {
	case err != nil:
		return EsitoConferma{}, fmt.Errorf("stato del ripristino: %w", err)
	case !isolata:
		return EsitoConferma{}, ErrNonIsolata
	case !strings.EqualFold(strings.TrimSpace(s.Parola), ParolaDiConferma):
		return EsitoConferma{}, ErrParolaSbagliata
	}
	coda, err := messaggiInCoda(ctx, tx)
	if err != nil {
		return EsitoConferma{}, err
	}
	if !scelteGiuste(coda, s.Manda, s.Scarta) {
		return EsitoConferma{}, ErrCodaCambiata
	}
	reg := audit.Nuovo(d, o)
	for _, m := range coda {
		if !slices.Contains(s.Scarta, m.ID) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM messaggi_in_uscita WHERE id = ? AND stato = 'queued'`, m.ID); err != nil {
			return EsitoConferma{}, err
		}
		var busta any
		if m.BustaID != nil {
			busta = *m.BustaID
		}
		if err := reg.Scrivi(ctx, tx, chi, "outgoing.queued_discarded", "outgoing_message", m.ID, "ok",
			map[string]any{"channel": m.Canale, "party": m.Destinatario, "repair_id": busta, "reason": "restore_confirmed"}); err != nil {
			return EsitoConferma{}, err
		}
	}
	ora := o.Adesso()
	adesso := ora.UTC().Format(db.FormatoIstante)
	rimesse := map[string]string{}
	var chiavi []string
	for _, k := range ImpostazioniSpenteDalRipristino { // in un ordine fisso
		v, ok := iso.Spento[k]
		if !ok {
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
			ON CONFLICT (chiave) DO NOTHING`, k, v, adesso)
		if err != nil {
			return EsitoConferma{}, fmt.Errorf("impostazione %s: %w", k, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return EsitoConferma{}, err
		} else if n == 1 {
			rimesse[k] = v
			chiavi = append(chiavi, k)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM impostazioni WHERE chiave = ?`, db.ChiaveIsolamento); err != nil {
		return EsitoConferma{}, err
	}
	segnati, err := jobs.SegnaDovutiComeFatti(ctx, tx, ora)
	if err != nil {
		return EsitoConferma{}, err
	}
	es := EsitoConferma{Isolamento: iso, Riaccesi: ParoleRiaccese(rimesse), Mandati: len(s.Manda), Scartati: len(s.Scarta),
		LavoriSegnati: segnati}
	if err := reg.Scrivi(ctx, tx, chi, "backup.restore_confirmed", "backup", iso.Archivio, "ok", map[string]any{
		"archive": iso.Archivio, "sha256": iso.SHA256, "captured_at": iso.CatturatoIl, "restored_at": iso.RipristinatoIl,
		"reenabled": chiavi, "queued_kept": es.Mandati, "queued_discarded": es.Scartati,
		"jobs_marked_done": len(segnati)}); err != nil {
		return EsitoConferma{}, err
	}
	if err := tx.Commit(); err != nil {
		return EsitoConferma{}, err
	}
	return es, nil
}

// Conferma è la conferma del ripristino per la pagina del titolare: lo stato (dal collegamento di lettura) e la
// conferma (ConfermaRipristino) sul database di serve.
type Conferma struct {
	d *db.DB
	o clock.Orologio
}

// NuovaConferma dà la conferma del ripristino sul database d.
func NuovaConferma(d *db.DB, o clock.Orologio) *Conferma { return &Conferma{d: d, o: o} }

// Stato è LeggiStatoRipristino sul collegamento di lettura.
func (c *Conferma) Stato(ctx context.Context) (StatoRipristino, error) {
	return LeggiStatoRipristino(ctx, c.d.Lettura)
}

// Isolata dice solo se la copia è ancora isolata (db.LeggiIsolamento), senza leggere la coda: è quello che il
// banner rilegge a ogni pagina.
func (c *Conferma) Isolata(ctx context.Context) (bool, error) {
	_, isolata, err := db.LeggiIsolamento(ctx, c.d.Lettura)
	return isolata, err
}

// Conferma è ConfermaRipristino.
func (c *Conferma) Conferma(ctx context.Context, s SceltaConferma, chi audit.Contesto) (EsitoConferma, error) {
	return ConfermaRipristino(ctx, c.d, c.o, s, chi)
}
