package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il recupero della password del titolare (ACC-13, ACC-14), come PasswordRecovery dello
// standalone (slice 188 e 208).
//
// Di chi: il titolare è l'account attivo più vecchio con ruolo titolare e una password; un
// dipendente che dimentica la password si fa aiutare dal titolare. Il link non va mai dove dice
// chi lo chiede: alla pagina non si scrive niente. Limiti della pagina: 3 richieste l'ora per
// indirizzo e 5 al giorno in tutto, contate sulle righe (anche quelle che non hanno raggiunto
// nessuno). Il link vale 30 minuti, una volta; uno nuovo annulla quelli aperti.
//
// Canali: il Go non ha ancora Telegram né email (T4.6, T4.7), quindi dalla pagina il link non
// arriva a nessuno, come nello standalone senza canali, e si annulla. Il comando locale
// (D-04) lo stampa sul terminale: non conta nei limiti della pagina.

const (
	// DurataReset è la validità di un link (RESET_TOKEN_TTL_MIN).
	DurataReset       = 30 * time.Minute
	resetPerIndirizzo = 3 // l'ora
	resetAlGiorno     = 5
	byteToken         = 32
	tokenMassimo      = 64 // un token è di 43 caratteri: uno molto più lungo non si calcola nemmeno
)

var (
	ErrNessunTitolare  = errors.New("accounts: nessun titolare attivo con una password")
	ErrPasswordDiverse = errors.New("accounts: le due password non coincidono")
	ErrPasswordCorta   = errors.New("accounts: password troppo corta")
	ErrLinkNonValido   = errors.New("accounts: link non valido")
)

// Recupero gestisce i link di reimpostazione.
type Recupero struct {
	d   *db.DB
	o   clock.Orologio
	reg *audit.Registro
}

// NuovoRecupero dà il recupero del database d.
func NuovoRecupero(d *db.DB, o clock.Orologio) *Recupero {
	return &Recupero{d: d, o: o, reg: audit.Nuovo(d, o)}
}

func (r *Recupero) adesso() (time.Time, string) {
	t := r.o.Adesso().UTC()
	return t, t.Format(db.FormatoIstante)
}

// RichiestaDallaPagina è perform_request/2: crea un link per il titolare se i limiti lo permettono
// e lo consegna. Dà quello che è successo, per il registro e i test: «no_channel» (senza canali,
// oggi sempre), «rate_limited» o «no_account». Chi chiede non lo vede mai: la pagina risponde
// uguale e prima.
func (r *Recupero) RichiestaDallaPagina(ctx context.Context, ip string) (string, error) {
	_, limite, utente, riga, _, err := r.crea(ctx, "pagina", ip)
	switch {
	case err != nil:
		return "", err
	case limite != "":
		return "rate_limited", r.limitato(ctx, ip, limite)
	case utente == 0:
		return "no_account", r.reg.Registra(ctx, audit.Contesto{Attore: "public", IP: ip}, "password_reset.requested",
			"installation", "password", "no_account", nil)
	}
	// Nessun canale: il link si annulla e il registro dice perché (deliver/3 senza canali).
	_, ora := r.adesso()
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if _, err := tx.ExecContext(ctx, `UPDATE reset_password SET revocato_il = ?, aggiornato_il = ? WHERE id = ?`, ora, ora, riga); err != nil {
		return "", err
	}
	if err := r.reg.Scrivi(ctx, tx, audit.Contesto{Attore: "public", IP: ip}, "password_reset.requested", "user", utente, "failed",
		map[string]any{"telegram": "not_configured", "email": "not_configured", "channel": "none"}); err != nil {
		return "", err
	}
	return "no_channel", tx.Commit()
}

// LinkDallaConsole è il comando locale (D-04): un link per il titolare, da stampare sul terminale
// e mai nel registro del processo, con l'email del titolare letta nella stessa transazione. Riga
// nel registro delle azioni, senza il token.
func (r *Recupero) LinkDallaConsole(ctx context.Context) (token, email string, scade time.Time, err error) {
	t, _ := r.adesso()
	var utente int64
	token, _, utente, _, email, err = r.crea(ctx, "console", "")
	if err != nil {
		return "", "", time.Time{}, err
	}
	if utente == 0 {
		return "", "", time.Time{}, ErrNessunTitolare
	}
	return token, email, t.Add(DurataReset), nil
}

// crea è mint/2: in una transazione di scrittura (immediata) conta i limiti della pagina, trova il
// titolare, annulla i link aperti e scrive il nuovo. Dà il limite superato («ip» o «global»), o
// l'utente 0 se non c'è un titolare; riga è l'id del link nuovo, email quella del titolare.
// Prima del lucchetto di scrittura i limiti si contano con letture semplici (precheck/1): un'ondata
// di richieste già oltre il limite non si mette in coda dietro le scritture; nella transazione si
// contano di nuovo, così due richieste insieme non passano.
func (r *Recupero) crea(ctx context.Context, origine, ip string) (token, limite string, utente, riga int64, email string, err error) {
	if origine == "pagina" {
		if limite, err := r.limiteSuperato(ctx, r.d.Lettura, ip); err != nil || limite != "" {
			return "", limite, 0, 0, "", err
		}
	}
	t, ora := r.adesso()
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return "", "", 0, 0, "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if origine == "pagina" {
		if limite, err := r.limiteSuperato(ctx, tx, ip); err != nil || limite != "" {
			return "", limite, 0, 0, "", err
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT id, email FROM utenti WHERE attivo = 1 AND ruolo = 'titolare' AND hash_password IS NOT NULL ORDER BY id LIMIT 1`).Scan(&utente, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, 0, "", nil
	}
	if err != nil {
		return "", "", 0, 0, "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reset_password SET revocato_il = ?, aggiornato_il = ? WHERE usato_il IS NULL AND revocato_il IS NULL`, ora, ora); err != nil {
		return "", "", 0, 0, "", err
	}
	b := make([]byte, byteToken)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce
	token = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	var canale, indirizzo any
	if origine == "console" {
		canale = "console"
	} else {
		indirizzo = ip
	}
	ins, err := tx.ExecContext(ctx, `INSERT INTO reset_password (utente_id, hash_token, scade_il, ip, canale, origine, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		utente, h[:], t.Add(DurataReset).Format(db.FormatoIstante), indirizzo, canale, origine, ora, ora)
	if err != nil {
		return "", "", 0, 0, "", err
	}
	if riga, err = ins.LastInsertId(); err != nil {
		return "", "", 0, 0, "", err
	}
	if origine == "console" {
		if err := r.reg.Scrivi(ctx, tx, audit.Contesto{Attore: "console"}, "password_reset.requested", "user", utente, "ok",
			map[string]any{"channel": "console"}); err != nil {
			return "", "", 0, 0, "", err
		}
	}
	return token, "", utente, riga, email, tx.Commit()
}

// interrogabile: la transazione o il collegamento di lettura.
type interrogabile interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// limiteSuperato conta le righe della pagina: «ip» oltre 3 nell'ora per l'indirizzo, «global»
// oltre 5 nelle 24 ore, "" se si può.
func (r *Recupero) limiteSuperato(ctx context.Context, q interrogabile, ip string) (string, error) {
	t, _ := r.adesso()
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM reset_password WHERE origine = 'pagina' AND creato_il > ? AND ip = ?`,
		t.Add(-time.Hour).Format(db.FormatoIstante), ip).Scan(&n); err != nil {
		return "", err
	}
	if n >= resetPerIndirizzo {
		return "ip", nil
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM reset_password WHERE origine = 'pagina' AND creato_il > ?`,
		t.Add(-24*time.Hour).Format(db.FormatoIstante)).Scan(&n); err != nil {
		return "", err
	}
	if n >= resetAlGiorno {
		return "global", nil
	}
	return "", nil
}

// limitato scrive il rifiuto per limite una volta l'ora per indirizzo (limited/2).
func (r *Recupero) limitato(ctx context.Context, ip, quale string) error {
	t, _ := r.adesso()
	var gia bool
	if err := r.d.Lettura.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM eventi_audit WHERE azione = 'password_reset.requested'
		AND esito = 'rate_limited' AND ip = ? AND creato_il > ?)`, ip, t.Add(-time.Hour).Format(db.FormatoIstante)).Scan(&gia); err != nil {
		return err
	}
	if gia {
		return nil
	}
	return r.reg.Registra(ctx, audit.Contesto{Attore: "public", IP: ip}, "password_reset.requested", "installation", "password",
		"rate_limited", map[string]any{"limit": quale})
}

func hashToken(token string) ([]byte, bool) {
	if len(token) < 1 || len(token) > tokenMassimo {
		return nil, false
	}
	h := sha256.Sum256([]byte(token))
	return h[:], true
}

// apertoTitolare: un link aperto (non usato, non revocato, non scaduto) di un titolare attivo.
const apertoTitolare = `FROM reset_password AS r JOIN utenti AS u ON u.id = r.utente_id
	WHERE r.hash_token = ? AND r.usato_il IS NULL AND r.revocato_il IS NULL AND r.scade_il > ?
	AND u.attivo = 1 AND u.ruolo = 'titolare'`

// AccountDel è account_for/1: l'email dell'account di un link aperto, per dire nella pagina di
// chi è la password. Non spende niente; ok falso per ogni link che non va, qualunque il motivo.
func (r *Recupero) AccountDel(ctx context.Context, token string) (string, bool, error) {
	h, ok := hashToken(token)
	if !ok {
		return "", false, nil
	}
	_, ora := r.adesso()
	var email string
	err := r.d.Lettura.QueryRowContext(ctx, `SELECT u.email `+apertoTitolare, h, ora).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}

// Reimposta è reset/4: prima le due password (un errore costa solo un altro tentativo), poi il
// link; in una transazione si spende il link, si scrive la password sull'account solo se è ancora
// titolare attivo, si chiudono tutte le sue sessioni, si annullano i suoi altri link, e si scrive
// la riga password_reset.completed.
func (r *Recupero) Reimposta(ctx context.Context, token, password, conferma, ip string) (err error) {
	switch {
	case password != conferma:
		return ErrPasswordDiverse
	case !PasswordAbbastanzaLunga(password):
		return ErrPasswordCorta
	}
	// Un link morto si rifiuta prima della derivazione: non costa niente.
	if _, ok, err := r.AccountDel(ctx, token); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrLinkNonValido
	}
	h, _ := hashToken(token)
	hash := Hash(password)
	_, ora := r.adesso()
	tx, err := r.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var id, utente int64
	var email string
	err = tx.QueryRowContext(ctx, `SELECT r.id, u.id, u.email `+apertoTitolare, h, ora).Scan(&id, &utente, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLinkNonValido
	}
	if err != nil {
		return err
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE reset_password SET usato_il = ?, aggiornato_il = ? WHERE id = ?`, []any{ora, ora, id}},
		{`UPDATE utenti SET hash_password = ?, aggiornato_il = ?, versione_sessione = versione_sessione + 1 WHERE id = ?`, []any{hash, ora, utente}},
		{`UPDATE reset_password SET revocato_il = ?, aggiornato_il = ? WHERE utente_id = ? AND id <> ? AND usato_il IS NULL AND revocato_il IS NULL`, []any{ora, ora, utente, id}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return err
		}
	}
	if err := r.reg.Scrivi(ctx, tx, audit.Contesto{Attore: email, IP: ip}, "password_reset.completed", "user", utente, "ok", nil); err != nil {
		return err
	}
	return tx.Commit()
}
