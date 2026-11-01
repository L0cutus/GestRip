package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il link pubblico della busta sulla scheda (RIP-S-24, CLI-23, CLI-24, CLI-25), come
// Workshop.public_link_status/1, legacy_link_status/1, revoke_public_access/2 e
// regenerate_public_link/2 dello standalone (go-baseline), con TelegramSubscriptions.cut_off/2 ed
// EmailNotice.cut_off/2 che quei due comandi chiamano nella stessa transazione.

// StatoLink è public_link_state/2: che cosa dice la scheda del link /r/ della busta.
type StatoLink int

const (
	LinkMancante     StatoLink = iota // :missing, nessuna impronta (con hash NOT NULL non capita)
	LinkAttivo                        // :usable, il testo del token c'è
	LinkSoloImpronta                  // :legacy_hash_only, solo l'impronta (link importato)
	LinkScaduto                       // :expired
	LinkRevocato                      // :revoked
)

// StoricoLink è legacy_link_status/1: il vecchio link /stato con codice della busta.
type StoricoLink int

const (
	StoricoNessuno  StoricoLink = iota // :none, mai avuto (busta nata qui)
	StoricoVivo                        // :live
	StoricoRevocato                    // :revoked
)

// Link è lo stato del link come lo legge la scheda: mai il token.
type Link struct {
	Stato     StatoLink
	Scade     *time.Time // solo per un link vivo (attivo o solo impronta)
	Storico   StoricoLink
	InviatoIl *time.Time // link_inviato_at: quando il link è stato dato al cliente
}

// Vivo è link_alive?/1: «Revoca link» c'è solo per un link attivo o con la sola impronta.
func (l Link) Vivo() bool { return l.Stato == LinkAttivo || l.Stato == LinkSoloImpronta }

// LinkNuovo è il link appena emesso da «Genera nuovo link». Token è in chiaro, per mostrarlo una
// volta: non va mai in registri, audit o errori.
type LinkNuovo struct {
	Oreficeria bool // busta di un'oreficeria esterna: il link è dell'oreficeria
	Token      string
}

// String nasconde il token: %v e %+v.
func (l LinkNuovo) String() string {
	return fmt.Sprintf("{Oreficeria:%t Token:**redacted**}", l.Oreficeria)
}

// GoString nasconde il token anche con %#v.
func (l LinkNuovo) GoString() string { return "buste.LinkNuovo" + l.String() }

// LogValue nasconde il token nel registro slog.
func (l LinkNuovo) LogValue() slog.Value {
	return slog.GroupValue(slog.Bool("oreficeria", l.Oreficeria), slog.String("token", "**redacted**"))
}

// colonneLink sono le colonne del link lette con la busta (alias b): mai il testo del token, solo
// se c'è. Il vecchio link con codice c'è con il codice importato o con la riga dell'import Flask
// della busta (legacy_credential?/2).
const colonneLink = `b.token_pubblico IS NOT NULL, b.hash_token_pubblico IS NOT NULL, b.token_pubblico_scade_il,
		b.token_pubblico_revocato_il, b.codice_flask_revocato_il,
		b.hash_codice_flask IS NOT NULL OR EXISTS (SELECT 1 FROM corrispondenze_import_flask m
			WHERE m.tabella_flask = 'riparazioni' AND m.tabella_standalone = 'repairs' AND m.id_standalone = b.id),
		b.link_inviato_il`

// rigaLink sono le colonneLink come le dà il database.
type rigaLink struct {
	testo, impronta, codice bool
	scade                   string
	revocato                *string
	codiceRevocato          *string
	inviato                 *string
}

// stato è public_link_state/2: revocato prima di tutto, poi scaduto (la scadenza non è dopo
// adesso), poi il testo del token, poi la sola impronta.
func (r rigaLink) stato(scade, adesso time.Time) StatoLink {
	switch {
	case r.revocato != nil:
		return LinkRevocato
	case !scade.After(adesso):
		return LinkScaduto
	case r.testo:
		return LinkAttivo
	case r.impronta:
		return LinkSoloImpronta
	}
	return LinkMancante
}

// storico è legacy_link_status/2.
func (r rigaLink) storico() StoricoLink {
	switch {
	case !r.codice:
		return StoricoNessuno
	case r.codiceRevocato != nil:
		return StoricoRevocato
	}
	return StoricoVivo
}

// link è public_link_status/1 con legacy_link_status/1 e link_inviato_at: la scadenza solo per un
// link vivo.
func (r rigaLink) link(adesso time.Time) (Link, error) {
	scade, err := time.Parse(db.FormatoIstante, r.scade)
	if err != nil {
		return Link{}, err
	}
	l := Link{Stato: r.stato(scade, adesso), Storico: r.storico()}
	if l.Vivo() {
		l.Scade = &scade
	}
	if l.InviatoIl, err = istante(r.inviato); err != nil {
		return Link{}, err
	}
	return l, nil
}

// bustaDelLink è la riga che i due comandi leggono dentro la loro transazione (la riga salvata, mai
// quella di una pagina): numero, controparte e se il vecchio link con codice è vivo.
type bustaDelLink struct {
	numero      string
	oreficeria  *int64
	storicoVivo bool
}

func leggiBustaDelLink(ctx context.Context, tx *sql.Tx, id int64) (bustaDelLink, error) {
	var bl bustaDelLink
	var rl rigaLink
	var oreficeria sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT b.numero, b.oreficeria_id, `+colonneLink+`
		FROM buste b WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(&bl.numero, &oreficeria, &rl.testo, &rl.impronta,
		&rl.scade, &rl.revocato, &rl.codiceRevocato, &rl.codice, &rl.inviato)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaDelLink{}, ErrBustaNonTrovata
	}
	if err != nil {
		return bustaDelLink{}, err
	}
	bl.oreficeria = nullaIntero(oreficeria)
	bl.storicoVivo = rl.storico() == StoricoVivo
	return bl, nil
}

// conLegacy è legacy_mark/2: legacy: true solo se il vecchio link con codice era vivo.
func conLegacy(dati map[string]any, vivo bool) map[string]any {
	if vivo {
		dati["legacy"] = true
	}
	return dati
}

// RevocaLink è «Revoca link» (revoke_public_access/2), in una transazione: sulla riga salvata segna
// la revoca adesso (anche se c'era già: un POST forgiato la riscrive), toglie il testo del token
// (l'impronta resta, così il vecchio link dà «non trovato»), revoca per sempre il vecchio link con
// codice (il primo istante resta) e non emette niente; repair.public_link_revoked senza dati, o con
// legacy: true se il link con codice era vivo; poi spegne le iscrizioni Telegram e toglie l'indirizzo
// email (cut_off/2 dei due moduli). La conversazione pubblica non cambia. Dice se la busta è di
// un'oreficeria esterna (il link è «dell'oreficeria»). Una busta che non c'è o nel cestino è
// ErrBustaNonTrovata, e niente si scrive.
func (b *Buste) RevocaLink(ctx context.Context, id int64, c audit.Contesto) (bool, error) {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	bl, err := leggiBustaDelLink(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET token_pubblico_revocato_il = ?1, token_pubblico = NULL,
		codice_flask_revocato_il = coalesce(codice_flask_revocato_il, ?1), aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
		return false, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.public_link_revoked", "repair", id, "ok", conLegacy(map[string]any{}, bl.storicoVivo)); err != nil {
		return false, err
	}
	if err := b.tagliaCanali(ctx, tx, id, bl.numero, c, ora); err != nil {
		return false, err
	}
	return b2b(valoreDi(bl.oreficeria)), tx.Commit()
}

// NuovoLink è «Genera nuovo link» (regenerate_public_link/2, issue_public_link/5 con il motivo
// manual), in una transazione: sempre un token nuovo come quello della busta nuova (crea.go), con
// l'impronta del testo, la scadenza a 90 giorni da adesso e la revoca tolta, così il link di prima
// smette di funzionare; il vecchio link con codice si revoca per sempre (il primo istante resta);
// repair.public_link_issued con reason manual (e legacy: true se il link con codice era vivo), mai
// il token; poi, come la revoca, Telegram ed email. Il token torna in LinkNuovo, che lo nasconde
// nelle stampe; un errore non lo porta (SQLite nomina le colonne, mai i valori).
func (b *Buste) NuovoLink(ctx context.Context, id int64, c audit.Contesto) (LinkNuovo, error) {
	t, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return LinkNuovo{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	bl, err := leggiBustaDelLink(ctx, tx, id)
	if err != nil {
		return LinkNuovo{}, err
	}
	genera := gettone
	if b.nuovoGettone != nil {
		genera = b.nuovoGettone
	}
	token, impronta := genera()
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET token_pubblico = ?1, hash_token_pubblico = ?2, token_pubblico_scade_il = ?3,
		token_pubblico_revocato_il = NULL, codice_flask_revocato_il = coalesce(codice_flask_revocato_il, ?4), aggiornato_il = ?4
		WHERE id = ?5`, token, impronta, t.Add(DurataGettone).Format(db.FormatoIstante), ora, id); err != nil {
		return LinkNuovo{}, fmt.Errorf("buste: link non salvato: %w", err)
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.public_link_issued", "repair", id, "ok",
		conLegacy(map[string]any{"reason": "manual"}, bl.storicoVivo)); err != nil {
		return LinkNuovo{}, err
	}
	if err := b.tagliaCanali(ctx, tx, id, bl.numero, c, ora); err != nil {
		return LinkNuovo{}, err
	}
	if err := tx.Commit(); err != nil {
		return LinkNuovo{}, err
	}
	return LinkNuovo{Oreficeria: b2b(valoreDi(bl.oreficeria)), Token: token}, nil
}

const (
	// erroreTelegramTagliato è @withdrawn di TelegramSubscriptions.
	erroreTelegramTagliato = "Iscrizione Telegram chiusa con il link della busta"
	// erroreEmailTagliata è @cut_off di EmailNotice.
	erroreEmailTagliata = "Avvisi email chiusi con il link della busta"
)

// tagliaCanali sono TelegramSubscriptions.cut_off/2 ed EmailNotice.cut_off/2, nella transazione del
// comando e in quest'ordine.
func (b *Buste) tagliaCanali(ctx context.Context, tx *sql.Tx, id int64, numero string, c audit.Contesto, ora string) error {
	if err := b.tagliaTelegram(ctx, tx, id, numero, c, ora); err != nil {
		return err
	}
	return b.tagliaEmail(ctx, tx, id, numero, c, ora)
}

// tagliaTelegram è TelegramSubscriptions.cut_off/2: ogni iscrizione ancora attiva si spegne
// (consenso 0, fermata adesso, motivo «link») con una riga repair.telegram_stop (busta, iscrizione,
// via «link»; mai la chat); le iscrizioni già fermate con un altro motivo prendono «link» e tengono
// il loro istante; via i token t.me della busta; i messaggi Telegram al cliente ancora in coda
// falliscono.
func (b *Buste) tagliaTelegram(ctx context.Context, tx *sql.Tx, id int64, numero string, c audit.Contesto, ora string) error {
	righe, err := tx.QueryContext(ctx, `SELECT id FROM iscrizioni_telegram WHERE busta_id = ? AND fermata_il IS NULL ORDER BY id`, id)
	if err != nil {
		return err
	}
	var attive []int64
	for righe.Next() {
		var n int64
		if err := righe.Scan(&n); err != nil {
			_ = righe.Close() // l'errore che conta è quello della lettura
			return err
		}
		attive = append(attive, n)
	}
	if err := righe.Err(); err != nil {
		return err
	}
	for _, n := range attive {
		if _, err := tx.ExecContext(ctx, `UPDATE iscrizioni_telegram SET consenso = 0, fermata_il = ?1, motivo_fermata = 'link',
			aggiornato_il = ?1 WHERE id = ?2`, ora, n); err != nil {
			return err
		}
		if err := b.reg.Scrivi(ctx, tx, c, "repair.telegram_stop", "repair", id, "ok", map[string]any{
			"bag_number": numero, "subscription_id": n, "via": "link",
		}); err != nil {
			return err
		}
	}
	for _, q := range []string{
		// stop_reason != "link": come in SQL, un motivo NULL resta NULL.
		`UPDATE iscrizioni_telegram SET motivo_fermata = 'link', aggiornato_il = ?2 WHERE busta_id = ?1 AND motivo_fermata != 'link'`,
		`DELETE FROM token_telegram WHERE busta_id = ?1`,
		`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?2, ultimo_errore = ?3
			WHERE busta_id = ?1 AND canale = 'telegram' AND destinatario = 'customer' AND stato = 'queued'`,
	} {
		if _, err := tx.ExecContext(ctx, q, id, ora, erroreTelegramTagliato); err != nil {
			return err
		}
	}
	return nil
}

// tagliaEmail è EmailNotice.cut_off/2: se la busta ha un indirizzo, le email ancora in coda per
// quell'indirizzo falliscono, indirizzo, conferma e invio della pronta si vuotano e si scrive
// repair.email_optout con la busta e via «link», mai l'indirizzo. Senza indirizzo niente.
func (b *Buste) tagliaEmail(ctx context.Context, tx *sql.Tx, id int64, numero string, c audit.Contesto, ora string) error {
	var indirizzo sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT email_cliente FROM buste WHERE id = ?`, id).Scan(&indirizzo); err != nil {
		return err
	}
	if !indirizzo.Valid {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?1, ultimo_errore = ?2
		WHERE busta_id = ?3 AND canale = 'email' AND indirizzo = ?4 AND stato = 'queued'`, ora, erroreEmailTagliata, id, indirizzo.String); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET email_cliente = NULL, email_confermata_il = NULL, email_pronta_inviata_il = NULL,
		aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
		return err
	}
	return b.reg.Scrivi(ctx, tx, c, "repair.email_optout", "repair", id, "ok", map[string]any{"bag_number": numero, "via": "link"})
}
