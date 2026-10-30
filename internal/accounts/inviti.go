package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Gli inviti degli account (ACC-07, ACC-08, ACC-09), come invite_user/2, reissue_invitation/2,
// accept_invitation/2 e invitation_recipient/1 dello standalone.
//
// Nessuno sceglie la password di un altro: il titolare aggiunge un indirizzo e il sistema crea un
// link monouso; chi lo apre sceglie la propria password, e fino ad allora l'account non entra
// (hash_password è nullo). Il token si dà in chiaro una volta sola, a chi l'ha chiesto: nel
// database c'è solo il suo SHA-256. Vale 15 minuti (MAGIC_LINK_TTL_MIN della sorgente); uno
// nuovo annulla quelli aperti dello stesso account.

// DurataInvito è la validità di un link d'invito.
const DurataInvito = 15 * time.Minute

const (
	emailMassima = 254 // caratteri (grafemi, String.length)
	nomeMassimo  = 120
)

// Rifiuti degli inviti. Per un account che non c'è o è fuori vale ErrNonTrovato.
var (
	ErrEmailNonValida  = errors.New("accounts: email non valida")
	ErrNomeNonValido   = errors.New("accounts: nome troppo lungo")
	ErrEmailOccupata   = errors.New("accounts: email già registrata su un account attivo")
	ErrGiaConPassword  = errors.New("accounts: l'account ha già una password")
	ErrInvitoNonValido = errors.New("accounts: invito non valido")
)

// reEmail è la forma di app_users_bp.py:15, `^[^@\s]+@[^@\s]+\.[^@\s]+$`: una sola chiocciola e un
// punto nel dominio. \s è quello di PCRE senza Unicode (spazio, \t, \n, \v, \f, \r); il \s di Go
// non ha \v.
var reEmail = regexp.MustCompile(`^[^@\t\n\v\f\r ]+@[^@\t\n\v\f\r ]+\.[^@\t\n\v\f\r ]+$`)

// Invito è un link appena creato: il token in chiaro esiste solo qui.
type Invito struct {
	UtenteID int64
	Email    string
	Token    string
	Scade    time.Time
}

// Inviti crea e consuma i link d'invito.
type Inviti struct {
	d   *db.DB
	o   clock.Orologio
	reg *audit.Registro
}

// NuoviInviti dà gli inviti del database d.
func NuoviInviti(d *db.DB, o clock.Orologio) *Inviti {
	return &Inviti{d: d, o: o, reg: audit.Nuovo(d, o)}
}

func (i *Inviti) adesso() (time.Time, string) {
	t := i.o.Adesso().UTC()
	return t, t.Format(db.FormatoIstante)
}

func nuovoToken() (token string, hash []byte) {
	b := make([]byte, byteToken)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce
	token = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:]
}

// controllaInvito è invitation_changeset/2: email normalizzata, ruolo, nome senza spazi ai lati e
// nullo se vuoto. Il ruolo sbagliato viene prima (invite_error/1 lo guarda per primo); un nome
// troppo lungo è un suo errore, che la pagina dice come «Email non valida.» come lo standalone.
func controllaInvito(email, nome, ruolo string) (string, any, error) {
	if ruolo != Titolare && ruolo != Dipendente {
		return "", nil, ErrRuoloNonValido
	}
	email = NormalizzaEmail(email)
	if !reEmail.MatchString(email) || uniseg.GraphemeClusterCount(email) > emailMassima {
		return "", nil, ErrEmailNonValida
	}
	nome = strings.TrimSpace(nome)
	if uniseg.GraphemeClusterCount(nome) > nomeMassimo {
		return "", nil, ErrNomeNonValido
	}
	if nome == "" {
		return email, nil, nil
	}
	return email, nome, nil
}

// Invita aggiunge un account senza password e crea il suo link (invite_user/2): account, invito e
// riga user.invited con il ruolo nella stessa transazione. L'email è unica solo tra gli account
// attivi: chi è uscito si reinvita e la riga vecchia resta.
func (i *Inviti) Invita(ctx context.Context, email, nome, ruolo string, c audit.Contesto) (Invito, error) {
	email, nomeDB, err := controllaInvito(email, nome, ruolo)
	if err != nil {
		return Invito{}, err
	}
	t, ora := i.adesso()
	tx, err := i.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Invito{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var presa bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM utenti WHERE email = ? AND attivo = 1)`, email).Scan(&presa); err != nil {
		return Invito{}, err
	}
	if presa {
		return Invito{}, ErrEmailOccupata
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO utenti (email, ruolo, nome_visibile, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?)`,
		email, ruolo, nomeDB, ora, ora)
	if err != nil {
		return Invito{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return Invito{}, err
	}
	token, h := nuovoToken()
	scade := t.Add(DurataInvito)
	if _, err := tx.ExecContext(ctx, `INSERT INTO inviti_utenti (utente_id, hash_token, scade_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?)`,
		id, h, scade.Format(db.FormatoIstante), ora, ora); err != nil {
		return Invito{}, err
	}
	if err := i.reg.Scrivi(ctx, tx, c, "user.invited", "user", id, "ok", map[string]any{"role": ruolo}); err != nil {
		return Invito{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invito{}, err
	}
	return Invito{UtenteID: id, Email: email, Token: token, Scade: scade}, nil
}

// NuovoLink è reissue_invitation/2: annulla i link aperti dell'account e ne crea uno nuovo, solo
// se l'account è attivo e non ha ancora una password. Il controllo sta nella scrittura stessa (un
// INSERT … SELECT sulle condizioni): se nel frattempo l'account ha scelto la password, o è uscito,
// non si scrive niente e l'annullamento torna indietro. Vale anche per un account senza inviti
// (gli utenti importati senza password).
func (i *Inviti) NuovoLink(ctx context.Context, id int64, c audit.Contesto) (Invito, error) {
	t, ora := i.adesso()
	tx, err := i.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Invito{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if _, err := tx.ExecContext(ctx, `UPDATE inviti_utenti SET revocato_il = ?, aggiornato_il = ?
		WHERE utente_id = ? AND accettato_il IS NULL AND revocato_il IS NULL`, ora, ora, id); err != nil {
		return Invito{}, err
	}
	token, h := nuovoToken()
	scade := t.Add(DurataInvito)
	r, err := tx.ExecContext(ctx, `INSERT INTO inviti_utenti (utente_id, hash_token, scade_il, creato_il, aggiornato_il)
		SELECT id, ?, ?, ?, ? FROM utenti WHERE id = ? AND attivo = 1 AND hash_password IS NULL`,
		h, scade.Format(db.FormatoIstante), ora, ora, id)
	if err != nil {
		return Invito{}, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return Invito{}, err
	}
	if n != 1 {
		if err := tx.Rollback(); err != nil {
			return Invito{}, err
		}
		// Il motivo si cerca dopo, fuori dalla decisione (reissue_refusal/1).
		var attivo bool
		if err := i.d.Lettura.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM utenti WHERE id = ? AND attivo = 1)`, id).Scan(&attivo); err != nil {
			return Invito{}, err
		}
		if attivo {
			return Invito{}, ErrGiaConPassword
		}
		return Invito{}, ErrNonTrovato
	}
	var email string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM utenti WHERE id = ?`, id).Scan(&email); err != nil {
		return Invito{}, err
	}
	if err := i.reg.Scrivi(ctx, tx, c, "user.invitation_reissued", "user", id, "ok", nil); err != nil {
		return Invito{}, err
	}
	if err := tx.Commit(); err != nil {
		return Invito{}, err
	}
	return Invito{UtenteID: id, Email: email, Token: token, Scade: scade}, nil
}

// aperto: un invito non accettato, non revocato e non scaduto, di un account attivo.
const aperto = `FROM inviti_utenti AS i JOIN utenti AS u ON u.id = i.utente_id
	WHERE i.hash_token = ? AND i.accettato_il IS NULL AND i.revocato_il IS NULL AND i.scade_il > ? AND u.attivo = 1`

// Destinatario è invitation_recipient/1: l'email dell'account di un invito aperto, perché la pagina
// dica di chi è l'accesso. Una lettura e basta: non spende niente e l'accettazione non la usa
// per decidere. ok falso per ogni invito che non va, qualunque il motivo.
func (i *Inviti) Destinatario(ctx context.Context, token string) (string, bool, error) {
	h, ok := hashToken(token)
	if !ok {
		return "", false, nil
	}
	_, ora := i.adesso()
	var email string
	err := i.d.Lettura.QueryRowContext(ctx, `SELECT u.email `+aperto, h, ora).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}

// Accetta è accept_invitation/2: prima la lunghezza della password (una password corta costa solo
// un altro tentativo e non spende il link), poi due scritture protette nella stessa transazione:
// la prima spende il link, la seconda scrive la password sull'account del link se è ancora attivo.
// Se la seconda non trova niente, la prima torna indietro. Riga user.invitation_accepted con
// l'email dell'invitato come attore.
func (i *Inviti) Accetta(ctx context.Context, token, password string) error {
	if !PasswordAbbastanzaLunga(password) {
		return ErrPasswordCorta
	}
	// Un link morto si rifiuta prima della derivazione: non costa niente (come Reimposta).
	if _, ok, err := i.Destinatario(ctx, token); err != nil || !ok {
		if err != nil {
			return err
		}
		return ErrInvitoNonValido
	}
	h, _ := hashToken(token)
	hash := Hash(password)
	_, ora := i.adesso()
	tx, err := i.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`UPDATE inviti_utenti SET accettato_il = ?, aggiornato_il = ?
			WHERE hash_token = ? AND accettato_il IS NULL AND revocato_il IS NULL AND scade_il > ?`, []any{ora, ora, h, ora}},
		{`UPDATE utenti SET hash_password = ?, aggiornato_il = ?
			WHERE id = (SELECT utente_id FROM inviti_utenti WHERE hash_token = ?) AND attivo = 1`, []any{hash, ora, h}},
	} {
		r, err := tx.ExecContext(ctx, q.sql, q.args...)
		if err != nil {
			return err
		}
		if n, err := r.RowsAffected(); err != nil || n != 1 {
			if err != nil {
				return err
			}
			return ErrInvitoNonValido
		}
	}
	var id int64
	var email string
	if err := tx.QueryRowContext(ctx, `SELECT u.id, u.email FROM inviti_utenti AS i JOIN utenti AS u ON u.id = i.utente_id WHERE i.hash_token = ?`, h).
		Scan(&id, &email); err != nil {
		return err
	}
	if err := i.reg.Scrivi(ctx, tx, audit.Contesto{Attore: email}, "user.invitation_accepted", "user", id, "ok", nil); err != nil {
		return err
	}
	return tx.Commit()
}

// AccountAttivo dà l'account attivo con quell'email (normalizzata) e se è ancora in attesa della
// password: serve al comando locale per scegliere tra un invito e un nuovo link.
func (i *Inviti) AccountAttivo(ctx context.Context, email string) (id int64, inAttesa, trovato bool, err error) {
	err = i.d.Lettura.QueryRowContext(ctx, `SELECT id, hash_password IS NULL FROM utenti WHERE email = ? AND attivo = 1`,
		NormalizzaEmail(email)).Scan(&id, &inAttesa)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, err
	}
	return id, inAttesa, true, nil
}
