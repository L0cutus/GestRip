package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Utente è quello che serve alla sessione di un account.
type Utente struct {
	ID       int64
	Email    string // l'attore delle sue azioni nel registro
	Ruolo    string // «titolare» o «dipendente»
	Versione int64  // versione_sessione
}

// Utenti legge e aggiorna gli account per l'accesso (ACC-01, ACC-04).
type Utenti struct {
	d   *db.DB
	o   clock.Orologio
	reg *audit.Registro
}

// NuoviUtenti dà gli account del database d.
func NuoviUtenti(d *db.DB, o clock.Orologio) *Utenti {
	return &Utenti{d: d, o: o, reg: audit.Nuovo(d, o)}
}

// hashFittizio è l'hash di una password che non è di nessuno: costa una derivazione vera quando
// l'account non c'è, non ha password o ha un hash malformato, e il risultato si butta sempre
// (come @dummy_hash dello standalone).
var hashFittizio = Hash("non-una-password-vera")

// NormalizzaEmail toglie gli spazi ai lati e porta in minuscolo, come String.trim e
// String.downcase di Elixir (misurati il 30/10/2026): l'unica differenza di Go è «İ», che
// Elixir porta a «i» più il punto combinante (U+0307) e Go a «i».
func NormalizzaEmail(email string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(email), "İ", "i\u0307"))
}

// Autentica controlla email e password: entra solo un account attivo con la password giusta.
// Email inesistente, account in attesa (senza password), hash malformato e password sbagliata
// costano la stessa derivazione e danno lo stesso no.
func (u *Utenti) Autentica(ctx context.Context, email, password string) (Utente, bool, error) {
	var ut Utente
	var hash sql.NullString
	err := u.d.Lettura.QueryRowContext(ctx,
		`SELECT id, email, ruolo, versione_sessione, hash_password FROM utenti WHERE email = ? AND attivo = 1`,
		NormalizzaEmail(email)).Scan(&ut.ID, &ut.Email, &ut.Ruolo, &ut.Versione, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Utente{}, false, err
	}
	if err != nil || !hash.Valid || !formaValida(hash.String) {
		Verifica(password, hashFittizio) // stesso costo; il risultato non conta mai
		return Utente{}, false, nil
	}
	if !Verifica(password, hash.String) {
		return Utente{}, false, nil
	}
	return ut, true, nil
}

// DiSessione dà l'account di una sessione solo se è ancora attivo e ha la stessa versione di
// sessione del cookie (ACC-04: disattivazione e «chiudi tutte» valgono al primo clic).
func (u *Utenti) DiSessione(ctx context.Context, id, versione int64) (Utente, bool, error) {
	ut := Utente{ID: id, Versione: versione}
	err := u.d.Lettura.QueryRowContext(ctx,
		`SELECT email, ruolo FROM utenti WHERE id = ? AND attivo = 1 AND versione_sessione = ?`, id, versione).Scan(&ut.Email, &ut.Ruolo)
	if errors.Is(err, sql.ErrNoRows) {
		return Utente{}, false, nil
	}
	if err != nil {
		return Utente{}, false, err
	}
	return ut, true, nil
}

// RegistraAccesso segna l'ultimo accesso, che la pagina utenti mostra al titolare.
func (u *Utenti) RegistraAccesso(ctx context.Context, id int64) error {
	adesso := u.o.Adesso().UTC().Format(db.FormatoIstante)
	_, err := u.d.Scrittura.ExecContext(ctx, `UPDATE utenti SET ultimo_accesso_il = ?, aggiornato_il = ? WHERE id = ?`, adesso, adesso, id)
	return err
}

// I due ruoli: owner e staff nello standalone (l'import li traduce).
const (
	Titolare   = "titolare"
	Dipendente = "dipendente"
)

// Rifiuti di Disattiva e CambiaRuolo (last_owner, not_found e invalid_role dello standalone).
var (
	ErrUltimoTitolare = errors.New("accounts: è l'ultimo titolare attivo con una password")
	ErrNonTrovato     = errors.New("accounts: account inesistente o non attivo")
	ErrRuoloNonValido = errors.New("accounts: ruolo non valido")
)

// restaUnTitolare è la regola dell'ultimo titolare (other_active_owner/1 dello standalone): un
// titolare può uscire o diventare dipendente solo se resta un **altro** titolare attivo che ha già
// scelto la password, cioè che può entrare adesso. Un titolare solo invitato non conta. Sta dentro
// la scrittura, così due titolari che si tolgono a vicenda non vincono entrambi. Il parametro è
// l'id dell'account che si cambia.
const restaUnTitolare = `(ruolo <> 'titolare' OR EXISTS (SELECT 1 FROM utenti AS altro
	WHERE altro.ruolo = 'titolare' AND altro.attivo = 1 AND altro.id <> ? AND altro.hash_password IS NOT NULL))`

// Disattiva mette fuori un account attivo (ACC-10): attivo a 0 e l'ora; le sue sessioni cadono
// alla richiesta dopo (DiSessione). Rifiuta l'ultimo titolare (ErrUltimoTitolare) e un account
// inesistente o già fuori (ErrNonTrovato), senza scrivere.
//
// La riga d'audit `user.deactivated` sta nella stessa transazione: se non si scrive, l'account
// resta attivo.
func (u *Utenti) Disattiva(ctx context.Context, id int64, c audit.Contesto) error {
	adesso := u.o.Adesso().UTC().Format(db.FormatoIstante)
	return u.protetta(ctx, id, c, "user.deactivated", nil,
		`UPDATE utenti SET attivo = 0, disattivato_il = ?, aggiornato_il = ? WHERE id = ? AND attivo = 1 AND `+restaUnTitolare,
		adesso, adesso, id, id)
}

// CambiaRuolo sposta un account attivo tra titolare e dipendente (ACC-11). Promuovere vale sempre
// (anche un titolare di nuovo titolare: la scrittura c'è, come set_role/3); declassare rispetta la
// regola dell'ultimo titolare. Il ruolo nuovo conta dalla richiesta dopo, perché la sessione lo
// rilegge ogni volta.
//
// La riga d'audit `user.role_changed`, con il ruolo, sta nella stessa transazione.
func (u *Utenti) CambiaRuolo(ctx context.Context, id int64, ruolo string, c audit.Contesto) error {
	adesso := u.o.Adesso().UTC().Format(db.FormatoIstante)
	q := `UPDATE utenti SET ruolo = ?, aggiornato_il = ? WHERE id = ? AND attivo = 1`
	args := []any{ruolo, adesso, id}
	switch ruolo {
	case Titolare:
	case Dipendente:
		q += ` AND ` + restaUnTitolare
		args = append(args, id)
	default:
		return ErrRuoloNonValido
	}
	return u.protetta(ctx, id, c, "user.role_changed", map[string]any{"role": ruolo}, q, args...)
}

// protetta fa una scrittura protetta e la sua riga d'audit in una transazione: una riga cambiata
// è fatta; nessuna riga è un rifiuto, senza audit, e il motivo si cerca dopo, fuori dalla
// decisione (refusal/1 dello standalone).
func (u *Utenti) protetta(ctx context.Context, id int64, c audit.Contesto, azione string, dati map[string]any, q string, args ...any) (err error) {
	tx, err := u.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback() // l'errore utile è quello della scrittura
		}
	}()
	r, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		if err = u.reg.Scrivi(ctx, tx, c, azione, "user", id, "ok", dati); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err = tx.Rollback(); err != nil {
		return err
	}
	var attivo bool
	err = u.d.Lettura.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM utenti WHERE id = ? AND attivo = 1)`, id).Scan(&attivo)
	if err != nil {
		return err
	}
	if attivo {
		return ErrUltimoTitolare
	}
	return ErrNonTrovato
}

// VoceUtente è una riga della pagina utenti: mai l'hash della password, solo se manca.
type VoceUtente struct {
	ID            int64
	Email, Nome   string
	Ruolo         string
	Attivo        bool
	InAttesa      bool // l'invito non è ancora stato accettato (nessuna password)
	UltimoAccesso *time.Time
}

// ElencoUtenti è list_users/0: attivi prima, poi i dipendenti prima dei titolari (desc role dello
// standalone, «staff» dopo «owner» nell'alfabeto), poi email e id. «In attesa» si calcola nella
// query: l'hash non esce mai.
func (u *Utenti) ElencoUtenti(ctx context.Context) ([]VoceUtente, error) {
	rr, err := u.d.Lettura.QueryContext(ctx, `SELECT id, email, coalesce(nome_visibile, ''), ruolo, attivo, hash_password IS NULL, ultimo_accesso_il
		FROM utenti ORDER BY attivo DESC, ruolo = 'titolare', email, id`)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []VoceUtente
	for rr.Next() {
		var v VoceUtente
		var ultimo sql.NullString
		if err := rr.Scan(&v.ID, &v.Email, &v.Nome, &v.Ruolo, &v.Attivo, &v.InAttesa, &ultimo); err != nil {
			return nil, err
		}
		if ultimo.Valid {
			t, err := time.Parse(db.FormatoIstante, ultimo.String)
			if err != nil {
				return nil, err
			}
			v.UltimoAccesso = &t
		}
		out = append(out, v)
	}
	return out, rr.Err()
}
