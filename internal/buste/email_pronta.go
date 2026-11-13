package buste

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'email «pronta per il ritiro» (CLI-42) e lo stato dell'avviso email nella scheda, come
// EmailNotice.notify_ready/2 e EmailNotice.sheet/1 dello standalone (go-baseline, slice 126 di lì):
//   - parte da sola a ogni passaggio a «pronta» da un altro stato (passaStato: scheda, lista, pannello
//     del QR; Riapri; il rientro dall'artigiano arriva con la T4.8 e deve chiamare avvisaPronta), solo
//     all'indirizzo confermato della controparte (address_party/1 = richiedente), una volta: il segno
//     email_pronta_inviata_il, e un «pronta» dopo non accoda niente, a meno che la riga dell'ultima email
//     di pronta (dopo l'ultima richiesta o disattivazione) sia «failed» (told?/1);
//   - solo Go, D-16: solo con l'email disponibile (canale acceso e configurato, chiave dei link e
//     indirizzo_pubblico, PostaDisponibile); senza, niente si scrive (lo standalone scrive il mancato
//     invio «not_configured») e il «pronta» dopo riprova, come lì;
//   - solo Go, la transazione: lo standalone accoda dopo il commit del cambio di stato, in una
//     transazione sua. Qui l'email (link stabile, riga della coda con AccodaIn, segno, riga d'audit) si
//     scrive nella transazione del cambio di stato, dentro un SAVEPOINT: il nuovo stato e l'email si
//     scrivono insieme o niente; un'email che non può partire (testo che non si compone, coda che
//     rifiuta, errore del database) torna indietro fino al SAVEPOINT, lascia la riga
//     repair.email_ready_not_queued con il motivo e non toglie mai il nuovo stato;
//   - testi dell'editor email_pronta_oggetto/testo (067) con {numero} della D-82, {link_stato} il link
//     stabile della busta (ensure_public_link/2, mai la password del vecchio link), il piè con il link
//     per disattivare; nessun indirizzo nell'audit.

// Le righe d'audit dell'email di pronta, quelle dello standalone.
const (
	azioneProntaAccodata    = "repair.email_ready_queued"
	azioneProntaNonAccodata = "repair.email_ready_not_queued"
)

// ConIndirizzoPubblico dà alle buste indirizzo_pubblico, su cui l'email di pronta scrive i suoi link
// (in gestrip serve lo stesso della pagina del cliente). "": l'email di pronta non parte (D-16).
func (b *Buste) ConIndirizzoPubblico(base string) *Buste {
	b.pubblico = base
	return b
}

// ConIndirizzoInterno dà alle buste indirizzo_interno, la faccia del personale: il link «Apri la scheda»
// dell'avviso Telegram al negozio (slice 074).
func (b *Buste) ConIndirizzoInterno(base string) *Buste {
	b.interno = base
	return b
}

// PostaDisponibile è EmailDisponibile con l'indirizzo pubblico delle buste: la domanda della D-16 per
// l'email di pronta e per la scheda.
func (b *Buste) PostaDisponibile(ctx context.Context) (bool, error) {
	return b.EmailDisponibile(ctx, b.pubblico)
}

// errPronta è un'email di pronta che non si è accodata per un motivo da scrivere nella riga del mancato
// invio (miss_reason/1): «not_configured» o «invalid_template».
type errPronta struct{ motivo string }

func (e errPronta) Error() string { return "buste: email di pronta non accodata (" + e.motivo + ")" }

// motivoPronta è il motivo della riga del mancato invio: quello di errPronta, «error» per ogni altro.
func motivoPronta(err error) string {
	var e errPronta
	if errors.As(err, &e) {
		return e.motivo
	}
	return "error"
}

// avvisaPronta è announce_ready_by_email/4 nella transazione tx del cambio di stato che ha portato la
// busta id a «pronta» da un altro stato: notificaPronta dentro un SAVEPOINT. Un'email che non parte
// torna indietro fino al SAVEPOINT e lascia la riga del mancato invio; il nuovo stato resta. Solo un
// SAVEPOINT che non si può disfare (il database rotto) è un errore, che annulla tutto il cambio.
func (b *Buste) avvisaPronta(ctx context.Context, tx *sql.Tx, id int64, c audit.Contesto) error {
	if _, err := tx.ExecContext(ctx, `SAVEPOINT email_pronta`); err != nil {
		b.mancataPronta(ctx, tx, id, "error", c)
		return nil
	}
	_, err := b.notificaPronta(ctx, tx, id, c)
	if err == nil {
		if _, err = tx.ExecContext(ctx, `RELEASE email_pronta`); err == nil {
			return nil
		}
	}
	if _, e := tx.ExecContext(ctx, `ROLLBACK TO email_pronta`); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `RELEASE email_pronta`); e != nil {
		return e
	}
	b.mancataPronta(ctx, tx, id, motivoPronta(err), c)
	return nil
}

// mancataPronta è ready_miss/3: la riga repair.email_ready_not_queued con il numero e il motivo, mai
// altro (né l'indirizzo né il testo dell'errore). Una riga che non si scrive non ferma il cambio di stato.
func (b *Buste) mancataPronta(ctx context.Context, tx *sql.Tx, id int64, motivo string, c audit.Contesto) {
	var numero string
	if err := tx.QueryRowContext(ctx, `SELECT numero FROM buste WHERE id = ?`, id).Scan(&numero); err != nil {
		return
	}
	_ = b.reg.Scrivi(ctx, tx, c, azioneProntaNonAccodata, "repair", id, "ok", map[string]any{"bag_number": numero, "reason": motivo})
}

// notificaPronta è notify_ready/2 nella transazione tx: le condizioni di ready_bag/1 rilette in tx, poi
// l'accodamento. L'esito: «queued», o il motivo per cui niente si è scritto («not_found»: la busta non c'è
// o è nel cestino; «not_ready»; «no_consent»: nessun indirizzo confermato; «other_party»: l'indirizzo
// non è della controparte; «already_sent»; «unavailable»: la D-16). Un errore vuol dire che qualcosa può
// essere già scritto in tx: chi chiama lo disfa (avvisaPronta).
func (b *Buste) notificaPronta(ctx context.Context, tx *sql.Tx, id int64, c audit.Contesto) (string, error) {
	be, err := leggiBustaEmail(ctx, tx, `id = ? AND cancellata_il IS NULL`, id)
	switch {
	case errors.Is(err, ErrBustaNonTrovata):
		return "not_found", nil
	case err != nil:
		return "", err
	case be.stato != "pronta":
		return "not_ready", nil
	case be.statoConsenso() != "confirmed":
		return "no_consent", nil
	}
	chi, err := richiedente(ctx, tx, id)
	if err != nil {
		return "", err
	}
	if chi != be.chiTieneIlLink() {
		return "other_party", nil
	}
	detto, err := giaDetto(ctx, tx, id)
	switch {
	case err != nil:
		return "", err
	case detto:
		return "already_sent", nil
	}
	disponibile, err := b.PostaDisponibile(ctx)
	switch {
	case err != nil:
		return "", err
	case !disponibile:
		return "unavailable", nil
	}
	token, err := b.garantisciLinkIn(ctx, tx, id, be.oreficeriaID, c)
	if err != nil {
		return "", err
	}
	valori, err := b.valoriEmail(ctx, be)
	if err != nil {
		return "", err
	}
	valori["link_stato"] = b.pubblico + "/r/" + token
	cont, modello, err := b.contenutoEmail(ctx, FamEmailProntaOggetto, FamEmailProntaTesto, valori, be, b.pubblico)
	switch {
	case err != nil:
		return "", err
	case modello:
		return "", errPronta{"invalid_template"}
	}
	messaggio, err := b.posta.AccodaIn(ctx, tx, uscita.Destinazione{BustaID: &be.id, Canale: uscita.CanaleEmail, Destinatario: be.parte(),
		Indirizzo: *be.indirizzo}, cont, c)
	switch {
	case errors.Is(err, uscita.ErrNonConfigurato):
		return "", errPronta{"not_configured"}
	case err != nil:
		return "", err
	}
	_, ora := b.adesso()
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET email_pronta_inviata_il = ?1, aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
		return "", err
	}
	if err := b.reg.Scrivi(ctx, tx, c, azioneProntaAccodata, "repair", id, "ok",
		map[string]any{"bag_number": be.numero, "party": be.parte(), "message_id": messaggio}); err != nil {
		return "", err
	}
	return "queued", nil
}

// giaDetto è told?/1: il segno, a meno che l'ultima email di pronta accodata dal Go dopo l'ultima
// richiesta o disattivazione sia una riga «failed». Il segno senza righe del Go (importato) è detto; una
// riga che non c'è più (scartata dal titolare, D-27) anche.
func giaDetto(ctx context.Context, q lettore, id int64) (bool, error) {
	var segno sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT email_pronta_inviata_il FROM buste WHERE id = ?`, id).Scan(&segno); err != nil {
		return false, err
	}
	if !segno.Valid {
		return false, nil
	}
	u, err := ultimaPronta(ctx, q, id, true)
	if err != nil || u == nil || !u.messaggio.Valid {
		return true, err
	}
	var stato string
	err = q.QueryRowContext(ctx, `SELECT stato FROM messaggi_in_uscita WHERE id = ?`, u.messaggio.Int64).Scan(&stato)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return stato != "failed", err
}

// rigaPronta è l'ultima riga dell'email di pronta: accodata (con la riga della coda) o no (con il motivo).
type rigaPronta struct {
	accodata  bool
	messaggio sql.NullInt64
	motivo    sql.NullString
}

// ultimaPronta è last_ready_event/2: l'ultima riga dell'email di pronta (solo quelle accodate, o anche i
// mancati invii) scritta dopo l'ultima richiesta che non è un «Reinvia» o l'ultima disattivazione: quello
// che c'era prima parlava di un indirizzo che non c'è più. nil se non ce n'è.
func ultimaPronta(ctx context.Context, q lettore, id int64, soloAccodate bool) (*rigaPronta, error) {
	azioni := `azione = '` + azioneProntaAccodata + `'`
	if !soloAccodate {
		azioni = `azione IN ('` + azioneProntaAccodata + `', '` + azioneProntaNonAccodata + `')`
	}
	var r rigaPronta
	var azione string
	err := q.QueryRowContext(ctx, `SELECT azione, json_extract(dati, '$.message_id'), json_extract(dati, '$.reason') FROM eventi_audit
		WHERE risorsa = 'repair' AND id_risorsa = ?1 AND `+azioni+` AND id > coalesce((SELECT max(id) FROM eventi_audit
			WHERE risorsa = 'repair' AND id_risorsa = ?1 AND (azione = ?2 OR (azione = ?3 AND json_extract(dati, '$.resend') = 0))), 0)
		ORDER BY id DESC LIMIT 1`, strconv.FormatInt(id, 10), azioneDisattivata, azioneRichiesta).Scan(&azione, &r.messaggio, &r.motivo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.accodata = azione == azioneProntaAccodata
	return &r, nil
}

// EmailDellaScheda è EmailNotice.sheet/1: l'avviso email come lo vede il negozio nella scheda.
type EmailDellaScheda struct {
	Consenso    string       // «pending» o «confirmed»
	Indirizzo   string       // l'indirizzo della busta
	Controparte bool         // l'indirizzo è della controparte (counterparty?): solo allora l'email di pronta parte
	Disponibile bool         // l'email è disponibile (D-16, solo Go): senza, l'email di pronta non parte
	Pronta      *EmailPronta // l'email di pronta dall'ultima richiesta o disattivazione; nil prima di ogni avviso
}

// EmailPronta è ready_state/1: Stato «queued», «accepted», «failed» con l'istante della riga della coda
// (accodata, accettata, fallita), «not_queued» con il Motivo del mancato invio, «sent» con il segno
// importato dallo standalone senza righe del Go; solo Go, «discarded» per una riga tolta dalla coda dal
// titolare (lo standalone, che non ha «Scarta», dà nil).
type EmailPronta struct {
	Stato  string
	Il     time.Time
	Motivo string
}

// emailDellaScheda è sheet/1 della busta id: nil senza indirizzo (una disattivazione nasconde gli avvisi
// di prima). Legge con il collegamento di lettura.
func (b *Buste) emailDellaScheda(ctx context.Context, id int64) (*EmailDellaScheda, error) {
	q := b.d.Lettura
	be, err := leggiBustaEmail(ctx, q, `id = ?`, id)
	if err != nil {
		return nil, err
	}
	if be.indirizzo == nil {
		return nil, nil
	}
	e := &EmailDellaScheda{Consenso: be.statoConsenso(), Indirizzo: *be.indirizzo}
	chi, err := richiedente(ctx, q, id)
	if err != nil {
		return nil, err
	}
	e.Controparte = chi == be.chiTieneIlLink()
	if e.Disponibile, err = b.PostaDisponibile(ctx); err != nil {
		return nil, err
	}
	if e.Pronta, err = statoPronta(ctx, q, id); err != nil {
		return nil, err
	}
	return e, nil
}

// statoPronta è ready_state/1 e ready_row/1.
func statoPronta(ctx context.Context, q lettore, id int64) (*EmailPronta, error) {
	u, err := ultimaPronta(ctx, q, id, false)
	if err != nil {
		return nil, err
	}
	if u == nil {
		var segno sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT email_pronta_inviata_il FROM buste WHERE id = ?`, id).Scan(&segno); err != nil || !segno.Valid {
			return nil, err
		}
		il, err := time.Parse(db.FormatoIstante, segno.String)
		if err != nil {
			return nil, err
		}
		return &EmailPronta{Stato: "sent", Il: il}, nil
	}
	if !u.accodata {
		return &EmailPronta{Stato: "not_queued", Motivo: u.motivo.String}, nil
	}
	var stato string
	var il sql.NullString
	err = q.QueryRowContext(ctx, `SELECT stato, CASE stato WHEN 'queued' THEN accodato_il WHEN 'accepted' THEN accettato_il
		WHEN 'failed' THEN fallito_il END FROM messaggi_in_uscita WHERE id = ?`, u.messaggio.Int64).Scan(&stato, &il)
	if errors.Is(err, sql.ErrNoRows) {
		// Tolta dalla coda dal titolare (D-27, «Scarta»): giaDetto la conta come detta, quindi la scheda
		// non deve promettere un'email che non partirà (review della 070b, M1).
		return &EmailPronta{Stato: "discarded"}, nil
	}
	if err == nil && !il.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(db.FormatoIstante, il.String)
	if err != nil {
		return nil, err
	}
	return &EmailPronta{Stato: stato, Il: t}, nil
}
