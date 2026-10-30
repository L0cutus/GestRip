package accounts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il primo titolare su un database vuoto (ACC-16, «sostituita»). Lo standalone lo crea
// dall'installer, con email e password lette da due file privati (Bootstrap.admin_from_files/2):
// solo se users è vuota (already_initialized altrimenti), riga administrator.bootstrapped con attore
// installer, tutto in una transazione. Il Go lo fa dal browser (decisione di Stefano del
// 30/10/2026): «gestrip primo-titolare» stampa sul terminale un link monouso e a scadenza,
// /configura/<codice>, e la pagina chiede email e password. Restano le regole dello standalone:
// solo su un database senza nessun account (anche disattivato o in attesa), password di almeno
// 12 caratteri, email come il changeset degli utenti, riga administrator.bootstrapped con attore
// installer nella stessa transazione.
//
// Il codice è come i token di reset e invito: 32 byte casuali, in chiaro una volta sola, sul
// terminale; nel database solo il suo SHA-256. Vale 60 minuti (l'installatore deve arrivare a un
// browser nella tailnet); uno nuovo annulla quelli aperti.

// DurataPrimoTitolare è la validità di un link /configura.
const DurataPrimoTitolare = time.Hour

var (
	// ErrGiaConfigurato: nel database c'è già un account (already_initialized).
	ErrGiaConfigurato = errors.New("accounts: il database ha già degli account")
	// ErrCodiceNonValido vale per ogni codice che non va, qualunque il motivo.
	ErrCodiceNonValido = errors.New("accounts: codice di configurazione non valido")
)

// PrimoTitolare crea i codici e, con uno di essi, il primo titolare.
type PrimoTitolare struct {
	d   *db.DB
	o   clock.Orologio
	reg *audit.Registro
}

// NuovoPrimoTitolare dà il primo titolare del database d.
func NuovoPrimoTitolare(d *db.DB, o clock.Orologio) *PrimoTitolare {
	return &PrimoTitolare{d: d, o: o, reg: audit.Nuovo(d, o)}
}

func (p *PrimoTitolare) adesso() (time.Time, string) {
	t := p.o.Adesso().UTC()
	return t, t.Format(db.FormatoIstante)
}

// LinkDallaConsole è il comando locale: in una transazione di scrittura controlla che utenti sia
// vuota, annulla i codici aperti, scrive il nuovo e la riga administrator.bootstrap_requested con
// attore «console», senza il codice. Il codice va stampato solo sul terminale.
func (p *PrimoTitolare) LinkDallaConsole(ctx context.Context) (codice string, scade time.Time, err error) {
	t, ora := p.adesso()
	tx, err := p.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var vuota bool
	if err := tx.QueryRowContext(ctx, `SELECT NOT EXISTS (SELECT 1 FROM utenti)`).Scan(&vuota); err != nil {
		return "", time.Time{}, err
	}
	if !vuota {
		return "", time.Time{}, ErrGiaConfigurato
	}
	if _, err := tx.ExecContext(ctx, `UPDATE codici_primo_titolare SET revocato_il = ?, aggiornato_il = ? WHERE usato_il IS NULL AND revocato_il IS NULL`, ora, ora); err != nil {
		return "", time.Time{}, err
	}
	codice, h := nuovoToken()
	scade = t.Add(DurataPrimoTitolare)
	if _, err := tx.ExecContext(ctx, `INSERT INTO codici_primo_titolare (hash_codice, scade_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`,
		h, scade.Format(db.FormatoIstante), ora, ora); err != nil {
		return "", time.Time{}, err
	}
	if err := p.reg.Scrivi(ctx, tx, audit.Contesto{Attore: "console"}, "administrator.bootstrap_requested", "installation", "administrator", "ok",
		map[string]any{"channel": "console"}); err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, err
	}
	return codice, scade, nil
}

// codiceAperto: un codice non usato, non annullato e non scaduto, con utenti ancora vuota.
const codiceAperto = `FROM codici_primo_titolare WHERE hash_codice = ? AND usato_il IS NULL AND revocato_il IS NULL AND scade_il > ?
	AND NOT EXISTS (SELECT 1 FROM utenti)`

// CodiceValido dice se il codice apre la pagina. Una lettura e basta: non spende niente
// (anteprime e scanner non lo bruciano) e la creazione non la usa per decidere.
func (p *PrimoTitolare) CodiceValido(ctx context.Context, codice string) (bool, error) {
	h, ok := hashToken(codice)
	if !ok {
		return false, nil
	}
	_, ora := p.adesso()
	var uno int
	err := p.d.Lettura.QueryRowContext(ctx, `SELECT 1 `+codiceAperto, h, ora).Scan(&uno)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Crea è admin_from_files/2 dal browser: prima il modulo (email come il changeset degli utenti,
// due password uguali di almeno 12 caratteri: un errore costa solo un altro tentativo e non
// spende il codice), poi il codice, che si rifiuta prima della derivazione PBKDF2 se è morto. In
// una transazione: la prima scrittura spende il codice solo se è ancora aperto e utenti è vuota,
// la seconda crea il titolare solo se utenti è ancora vuota; se una non trova niente, nessuna
// resta. Gli altri codici aperti si annullano; riga administrator.bootstrapped con attore
// installer e nessun dato, come lo standalone, più l'indirizzo di chi l'ha fatto.
func (p *PrimoTitolare) Crea(ctx context.Context, codice, email, password, conferma, ip string) (id int64, normalizzata string, err error) {
	normalizzata, _, err = controllaInvito(email, "", Titolare)
	switch {
	case err != nil:
		return 0, "", err
	case password != conferma:
		return 0, "", ErrPasswordDiverse
	case !PasswordAbbastanzaLunga(password):
		return 0, "", ErrPasswordCorta
	}
	if ok, err := p.CodiceValido(ctx, codice); err != nil || !ok {
		if err != nil {
			return 0, "", err
		}
		return 0, "", ErrCodiceNonValido
	}
	h, _ := hashToken(codice)
	hash := Hash(password)
	_, ora := p.adesso()
	tx, err := p.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	r, err := tx.ExecContext(ctx, `UPDATE codici_primo_titolare SET usato_il = ?, aggiornato_il = ?
		WHERE hash_codice = ? AND usato_il IS NULL AND revocato_il IS NULL AND scade_il > ? AND NOT EXISTS (SELECT 1 FROM utenti)`, ora, ora, h, ora)
	if err != nil {
		return 0, "", err
	}
	if n, err := r.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return 0, "", err
		}
		return 0, "", ErrCodiceNonValido
	}
	r, err = tx.ExecContext(ctx, `INSERT INTO utenti (email, ruolo, hash_password, creato_il, aggiornato_il)
		SELECT ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM utenti)`, normalizzata, Titolare, hash, ora, ora)
	if err != nil {
		return 0, "", err
	}
	if n, err := r.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return 0, "", err
		}
		return 0, "", ErrCodiceNonValido
	}
	if id, err = r.LastInsertId(); err != nil {
		return 0, "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE codici_primo_titolare SET revocato_il = ?, aggiornato_il = ? WHERE usato_il IS NULL AND revocato_il IS NULL`, ora, ora); err != nil {
		return 0, "", err
	}
	if err := p.reg.Scrivi(ctx, tx, audit.Contesto{Attore: "installer", IP: ip}, "administrator.bootstrapped", "user", id, "ok", nil); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return id, normalizzata, nil
}
