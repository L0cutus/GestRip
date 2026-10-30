package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Utente è quello che serve alla sessione di un account.
type Utente struct {
	ID       int64
	Ruolo    string // «titolare» o «dipendente»
	Versione int64  // versione_sessione
}

// Utenti legge e aggiorna gli account per l'accesso (ACC-01, ACC-04).
type Utenti struct {
	d *db.DB
	o clock.Orologio
}

// NuoviUtenti dà gli account del database d.
func NuoviUtenti(d *db.DB, o clock.Orologio) *Utenti { return &Utenti{d: d, o: o} }

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
		`SELECT id, ruolo, versione_sessione, hash_password FROM utenti WHERE email = ? AND attivo = 1`,
		NormalizzaEmail(email)).Scan(&ut.ID, &ut.Ruolo, &ut.Versione, &hash)
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
		`SELECT ruolo FROM utenti WHERE id = ? AND attivo = 1 AND versione_sessione = ?`, id, versione).Scan(&ut.Ruolo)
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
// Manca la riga d'audit `user.deactivated` nella stessa transazione: arriva con l'audit (ADM-16).
func (u *Utenti) Disattiva(ctx context.Context, id int64) error {
	adesso := u.o.Adesso().UTC().Format(db.FormatoIstante)
	r, err := u.d.Scrittura.ExecContext(ctx,
		`UPDATE utenti SET attivo = 0, disattivato_il = ?, aggiornato_il = ? WHERE id = ? AND attivo = 1 AND `+restaUnTitolare,
		adesso, adesso, id, id)
	return u.esitoProtetto(ctx, r, err, id)
}

// CambiaRuolo sposta un account attivo tra titolare e dipendente (ACC-11). Promuovere vale sempre
// (anche un titolare di nuovo titolare: la scrittura c'è, come set_role/3); declassare rispetta la
// regola dell'ultimo titolare. Il ruolo nuovo conta dalla richiesta dopo, perché la sessione lo
// rilegge ogni volta.
//
// Manca la riga d'audit `user.role_changed` nella stessa transazione: arriva con l'audit (ADM-16).
func (u *Utenti) CambiaRuolo(ctx context.Context, id int64, ruolo string) error {
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
	r, err := u.d.Scrittura.ExecContext(ctx, q, args...)
	return u.esitoProtetto(ctx, r, err, id)
}

// esitoProtetto legge l'esito di una scrittura protetta: una riga è fatta; nessuna riga è un
// rifiuto, e il motivo si cerca dopo, fuori dalla decisione (refusal/1 dello standalone).
func (u *Utenti) esitoProtetto(ctx context.Context, r sql.Result, err error, id int64) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
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
