package accounts

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

var ctx = context.Background()

func orologio() *clock.Finto {
	return clock.NuovoFinto(time.Date(2026, 10, 30, 9, 15, 7, 0, time.UTC))
}

const istante = "2026-10-30T09:15:07.000000Z"

// utentiProva: un database migrato con quattro account inventati.
func utentiProva(t *testing.T) (*Utenti, *db.DB) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	h := vettoriStandalone[0].hash // password-sintetica-di-prova
	for _, u := range []struct {
		email, ruolo string
		hash         any
		attivo       int
	}{
		{"titolare@esempio.it", "titolare", h, 1},
		{"invitato@esempio.it", "dipendente", nil, 1},
		{"uscito@esempio.it", "dipendente", h, 0},
		{"rotto@esempio.it", "dipendente", "pbkdf2_sha256$600000$rotto", 1},
	} {
		if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, attivo, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?)`,
			u.email, u.ruolo, u.hash, u.attivo, istante, istante); err != nil {
			t.Fatal(err)
		}
	}
	return NuoviUtenti(d, orologio()), d
}

// inventario: ACC-01
// Entra solo un account attivo con la password giusta; l'email si normalizza (spazi tolti,
// minuscole), come Accounts.authenticate/2 dello standalone.
func TestAutentica(t *testing.T) {
	u, _ := utentiProva(t)
	pw := vettoriStandalone[0].password
	for email, atteso := range map[string]bool{
		"titolare@esempio.it":      true,
		"  TITOLARE@Esempio.IT \t": true,
		"invitato@esempio.it":      false, // in attesa: senza password
		"uscito@esempio.it":        false, // disattivato
		"rotto@esempio.it":         false, // hash malformato
		"nessuno@esempio.it":       false,
		"":                         false,
	} {
		ut, ok, err := u.Autentica(ctx, email, pw)
		if err != nil || ok != atteso {
			t.Errorf("Autentica(%q) = %v, %v; atteso %v", email, ok, err, atteso)
		}
		if ok && (ut.ID != 1 || ut.Ruolo != "titolare" || ut.Versione != 0) {
			t.Errorf("utente = %+v", ut)
		}
	}
	if _, ok, _ := u.Autentica(ctx, "titolare@esempio.it", pw+"x"); ok {
		t.Error("password sbagliata accettata")
	}
}

// inventario: ACC-01
// Email inesistente, account in attesa, hash malformato e password sbagliata costano lo stesso
// calcolo (hash fittizio dello standalone): i tempi non dicono quali email esistono.
func TestAutenticaACostoPieno(t *testing.T) {
	u, _ := utentiProva(t)
	misura := func(email string) time.Duration {
		inizio := time.Now()
		if _, ok, err := u.Autentica(ctx, email, "password-sbagliata"); ok || err != nil {
			t.Fatalf("%s: %v %v", email, ok, err)
		}
		return time.Since(inizio)
	}
	rif := misura("titolare@esempio.it")
	for _, e := range []string{"nessuno@esempio.it", "invitato@esempio.it", "rotto@esempio.it", "uscito@esempio.it"} {
		if d := misura(e); d < rif/2 {
			t.Errorf("%s: %v contro %v di una password sbagliata: il tempo rivela l'account", e, d, rif)
		}
	}
}

// inventario: ACC-04
// La sessione vale solo per un account ancora attivo con la stessa versione di sessione.
func TestDiSessione(t *testing.T) {
	u, d := utentiProva(t)
	if ut, ok, err := u.DiSessione(ctx, 1, 0); !ok || err != nil || ut.Ruolo != "titolare" {
		t.Errorf("sessione valida rifiutata: %v %v", ok, err)
	}
	for nome, c := range map[string][2]int64{"versione vecchia": {1, 1}, "disattivato": {3, 0}, "inesistente": {99, 0}, "id zero": {0, 0}} {
		if _, ok, err := u.DiSessione(ctx, c[0], c[1]); ok || err != nil {
			t.Errorf("%s: accettata (%v)", nome, err)
		}
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET versione_sessione = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := u.DiSessione(ctx, 1, 0); ok {
		t.Error("dopo «chiudi tutte» la sessione vecchia vale ancora")
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET attivo = 0, disattivato_il = ? WHERE id = 1`, istante); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := u.DiSessione(ctx, 1, 1); ok {
		t.Error("account disattivato: la sessione vale ancora (ACC-10, ACC-04)")
	}
}

// inventario: ACC-01
// L'accesso riuscito segna l'ora dell'ultimo accesso e dell'aggiornamento.
func TestRegistraAccesso(t *testing.T) {
	_, d := utentiProva(t)
	o := orologio()
	o.Sposta(time.Hour)
	u := NuoviUtenti(d, o)
	if err := u.RegistraAccesso(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var ultimo, agg string
	if err := d.Lettura.QueryRow(`SELECT ultimo_accesso_il, aggiornato_il FROM utenti WHERE id = 1`).Scan(&ultimo, &agg); err != nil {
		t.Fatal(err)
	}
	if ultimo != "2026-10-30T10:15:07.000000Z" || agg != ultimo {
		t.Errorf("ultimo %q, aggiornato %q", ultimo, agg)
	}
}

func TestNormalizzaEmail(t *testing.T) {
	// Misurati su Elixir (String.trim e String.downcase) il 30/10/2026: spazi Unicode tolti ai
	// lati, ZWSP e BOM no; «İ» diventa «i» più il punto combinante; niente sigma finale.
	for in, atteso := range map[string]string{
		"  Anna@Esempio.IT\n":   "anna@esempio.it",
		"\u00a0ÀNNA@x.it\u3000": "ànna@x.it",
		"\u200bA@x.it":          "\u200ba@x.it",
		"\ufeffA@x.it":          "\ufeffa@x.it",
		"\u0130@x.it":           "i\u0307@x.it",
		"ΟΔΟΣ@x.it":             "οδοσ@x.it",
	} {
		if got := NormalizzaEmail(in); got != atteso {
			t.Errorf("NormalizzaEmail(%q) = %q", in, got)
		}
	}
}
