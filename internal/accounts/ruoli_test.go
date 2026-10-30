package accounts

import (
	"errors"
	"testing"

	"github.com/L0cutus/gestrip/internal/db"
)

// Gli account di utentiProva: 1 titolare con password, 2 invitato (dipendente senza password),
// 3 uscito, 4 dipendente con hash rotto.

func aggiungi(t *testing.T, d *db.DB, email, ruolo string, hash any, attivo int) int64 {
	t.Helper()
	r, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, hash_password, attivo, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?)`,
		email, ruolo, hash, attivo, istante, istante)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

type statoAccount struct {
	attivo        int
	ruolo         string
	disattivatoIl any
	aggiornatoIl  string
}

func leggiAccount(t *testing.T, d *db.DB, id int64) statoAccount {
	t.Helper()
	var s statoAccount
	if err := d.Lettura.QueryRow(`SELECT attivo, ruolo, disattivato_il, aggiornato_il FROM utenti WHERE id = ?`, id).
		Scan(&s.attivo, &s.ruolo, &s.disattivatoIl, &s.aggiornatoIl); err != nil {
		t.Fatal(err)
	}
	return s
}

// inventario: ACC-10
// La disattivazione mette l'account fuori e segna quando (deactivate_user/2).
func TestDisattivaUnDipendente(t *testing.T) {
	u, d := utentiProva(t)
	id := aggiungi(t, d, "banco@esempio.it", "dipendente", vettoriStandalone[0].hash, 1)
	o := orologio()
	o.Sposta(90)
	u.o = o
	if err := u.Disattiva(ctx, id); err != nil {
		t.Fatal(err)
	}
	s := leggiAccount(t, d, id)
	adesso := o.Adesso().Format(db.FormatoIstante)
	if s.attivo != 0 || s.disattivatoIl != adesso || s.aggiornatoIl != adesso {
		t.Errorf("dopo Disattiva: %+v", s)
	}
	if _, ok, _ := u.DiSessione(ctx, id, 0); ok {
		t.Error("la sessione di un account disattivato vale ancora")
	}
}

// inventario: ACC-10, ACC-11
// La regola dell'ultimo titolare vale solo per i titolari: un dipendente si disattiva o resta
// dipendente anche se nessun titolare ha ancora scelto la password (dati importati).
func TestDipendenteSenzaTitolariConPassword(t *testing.T) {
	u, d := utentiProva(t)
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET hash_password = NULL WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	id := aggiungi(t, d, "banco@esempio.it", "dipendente", vettoriStandalone[0].hash, 1)
	if err := u.CambiaRuolo(ctx, id, Dipendente); err != nil {
		t.Errorf("dipendente di nuovo dipendente: %v", err)
	}
	if err := u.Disattiva(ctx, id); err != nil {
		t.Errorf("disattivare un dipendente: %v", err)
	}
}

// inventario: ACC-10
// L'ultimo titolare attivo non si disattiva, e il rifiuto dice perché; un titolare se ne va se
// ne resta un altro attivo con la password scelta. Un titolare solo invitato o disattivato non
// conta (other_active_owner/1).
func TestDisattivaUltimoTitolare(t *testing.T) {
	u, d := utentiProva(t)
	if err := u.Disattiva(ctx, 1); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("ultimo titolare: %v", err)
	}
	if s := leggiAccount(t, d, 1); s.attivo != 1 || s.disattivatoIl != nil || s.aggiornatoIl != istante {
		t.Errorf("il rifiuto ha scritto: %+v", s)
	}
	invitato := aggiungi(t, d, "socio@esempio.it", "titolare", nil, 1)
	aggiungi(t, d, "vecchio@esempio.it", "titolare", vettoriStandalone[0].hash, 0)
	if err := u.Disattiva(ctx, 1); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("con un titolare solo invitato e uno disattivato: %v", err)
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET hash_password = ? WHERE id = ?`, vettoriStandalone[0].hash, invitato); err != nil {
		t.Fatal(err)
	}
	if err := u.Disattiva(ctx, 1); err != nil {
		t.Fatalf("con un altro titolare che ha scelto la password: %v", err)
	}
	if err := u.Disattiva(ctx, invitato); !errors.Is(err, ErrUltimoTitolare) {
		t.Errorf("ora l'altro è l'ultimo: %v", err)
	}
}

// inventario: ACC-10
// Un account già fuori o inesistente: «non trovato», nessuna scrittura.
func TestDisattivaNonTrovato(t *testing.T) {
	u, d := utentiProva(t)
	for _, id := range []int64{3, 99} {
		if err := u.Disattiva(ctx, id); !errors.Is(err, ErrNonTrovato) {
			t.Errorf("Disattiva(%d) = %v", id, err)
		}
	}
	if s := leggiAccount(t, d, 3); s.aggiornatoIl != istante || s.disattivatoIl != nil {
		t.Errorf("l'account già fuori è stato riscritto: %+v", s)
	}
}

// inventario: ACC-11
// Il cambio di ruolo: un dipendente diventa titolare (anche se lo è già: la scrittura vale);
// declassare rispetta la regola dell'ultimo titolare con password; un ruolo che il modulo non
// offre, un account fuori o inesistente si rifiutano senza scrivere.
func TestCambiaRuolo(t *testing.T) {
	u, d := utentiProva(t)
	if err := u.CambiaRuolo(ctx, 1, Dipendente); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("declassare l'ultimo titolare: %v", err)
	}
	if err := u.CambiaRuolo(ctx, 4, Titolare); err != nil {
		t.Fatal(err)
	}
	// L'account 4 ha un hash (rotto ma presente): per la regola conta come «con password».
	if err := u.CambiaRuolo(ctx, 1, Dipendente); err != nil {
		t.Fatalf("declassare con un altro titolare: %v", err)
	}
	if s := leggiAccount(t, d, 1); s.ruolo != Dipendente {
		t.Errorf("ruolo = %q", s.ruolo)
	}
	if err := u.CambiaRuolo(ctx, 4, Titolare); err != nil {
		t.Errorf("titolare di nuovo titolare: %v", err)
	}
	if err := u.CambiaRuolo(ctx, 2, Titolare); err != nil {
		t.Errorf("promuovere un invitato: %v", err)
	}
	for _, c := range []struct {
		id    int64
		ruolo string
		err   error
	}{
		{1, "owner", ErrRuoloNonValido},
		{1, "", ErrRuoloNonValido},
		{3, Titolare, ErrNonTrovato},
		{99, Dipendente, ErrNonTrovato},
	} {
		prima := leggiAccountOZero(t, d, c.id)
		if err := u.CambiaRuolo(ctx, c.id, c.ruolo); !errors.Is(err, c.err) {
			t.Errorf("CambiaRuolo(%d, %q) = %v, atteso %v", c.id, c.ruolo, err, c.err)
		}
		if dopo := leggiAccountOZero(t, d, c.id); dopo != prima {
			t.Errorf("CambiaRuolo(%d, %q) ha scritto: %+v → %+v", c.id, c.ruolo, prima, dopo)
		}
	}
}

func leggiAccountOZero(t *testing.T, d *db.DB, id int64) statoAccount {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM utenti WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		return statoAccount{}
	}
	return leggiAccount(t, d, id)
}

// inventario: ACC-10, ACC-11
// Due titolari che si disattivano o si declassano a vicenda non vincono entrambi: la regola sta
// dentro la scrittura, non in una lettura prima.
func TestUltimoTitolareSenzaCorse(t *testing.T) {
	u, d := utentiProva(t)
	altro := aggiungi(t, d, "socio@esempio.it", "titolare", vettoriStandalone[0].hash, 1)
	errs := make(chan error, 2)
	go func() { errs <- u.Disattiva(ctx, 1) }()
	go func() { errs <- u.CambiaRuolo(ctx, altro, Dipendente) }()
	e1, e2 := <-errs, <-errs
	riusciti := 0
	for _, e := range []error{e1, e2} {
		if e == nil {
			riusciti++
		} else if !errors.Is(e, ErrUltimoTitolare) {
			t.Fatal(e)
		}
	}
	var titolari int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM utenti WHERE ruolo = 'titolare' AND attivo = 1 AND hash_password IS NOT NULL`).Scan(&titolari); err != nil {
		t.Fatal(err)
	}
	if riusciti != 1 || titolari != 1 {
		t.Errorf("riusciti %d, titolari rimasti %d", riusciti, titolari)
	}
}

// inventario: ACC-10, ACC-11
// Manca la riga d'audit nella stessa transazione (`user.deactivated`, `user.role_changed`): arriva
// con la tabella dell'audit (ADM-16). Finché il test è saltato, le due voci restano parziali.
func TestDisattivaECambiaRuoloScrivonoAudit(t *testing.T) {
	t.Skip("audit non ancora nel Go (ADM-16): le righe user.deactivated e user.role_changed vanno nella stessa transazione")
}
