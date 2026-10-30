package accounts

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"

	"github.com/L0cutus/gestrip/internal/db"
)

// chi è il contesto d'audit delle modifiche: il titolare che le fa dalla pagina utenti.
var chi = audit.Contesto{Attore: "titolare@esempio.it"}

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
	if err := u.Disattiva(ctx, id, chi); err != nil {
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
	if err := u.CambiaRuolo(ctx, id, Dipendente, chi); err != nil {
		t.Errorf("dipendente di nuovo dipendente: %v", err)
	}
	if err := u.Disattiva(ctx, id, chi); err != nil {
		t.Errorf("disattivare un dipendente: %v", err)
	}
}

// inventario: ACC-10
// L'ultimo titolare attivo non si disattiva, e il rifiuto dice perché; un titolare se ne va se
// ne resta un altro attivo con la password scelta. Un titolare solo invitato o disattivato non
// conta (other_active_owner/1).
func TestDisattivaUltimoTitolare(t *testing.T) {
	u, d := utentiProva(t)
	if err := u.Disattiva(ctx, 1, chi); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("ultimo titolare: %v", err)
	}
	if s := leggiAccount(t, d, 1); s.attivo != 1 || s.disattivatoIl != nil || s.aggiornatoIl != istante {
		t.Errorf("il rifiuto ha scritto: %+v", s)
	}
	invitato := aggiungi(t, d, "socio@esempio.it", "titolare", nil, 1)
	aggiungi(t, d, "vecchio@esempio.it", "titolare", vettoriStandalone[0].hash, 0)
	if err := u.Disattiva(ctx, 1, chi); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("con un titolare solo invitato e uno disattivato: %v", err)
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET hash_password = ? WHERE id = ?`, vettoriStandalone[0].hash, invitato); err != nil {
		t.Fatal(err)
	}
	if err := u.Disattiva(ctx, 1, chi); err != nil {
		t.Fatalf("con un altro titolare che ha scelto la password: %v", err)
	}
	if err := u.Disattiva(ctx, invitato, chi); !errors.Is(err, ErrUltimoTitolare) {
		t.Errorf("ora l'altro è l'ultimo: %v", err)
	}
}

// inventario: ACC-10
// Un account già fuori o inesistente: «non trovato», nessuna scrittura.
func TestDisattivaNonTrovato(t *testing.T) {
	u, d := utentiProva(t)
	for _, id := range []int64{3, 99} {
		if err := u.Disattiva(ctx, id, chi); !errors.Is(err, ErrNonTrovato) {
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
	if err := u.CambiaRuolo(ctx, 1, Dipendente, chi); !errors.Is(err, ErrUltimoTitolare) {
		t.Fatalf("declassare l'ultimo titolare: %v", err)
	}
	if err := u.CambiaRuolo(ctx, 4, Titolare, chi); err != nil {
		t.Fatal(err)
	}
	// L'account 4 ha un hash (rotto ma presente): per la regola conta come «con password».
	if err := u.CambiaRuolo(ctx, 1, Dipendente, chi); err != nil {
		t.Fatalf("declassare con un altro titolare: %v", err)
	}
	if s := leggiAccount(t, d, 1); s.ruolo != Dipendente {
		t.Errorf("ruolo = %q", s.ruolo)
	}
	if err := u.CambiaRuolo(ctx, 4, Titolare, chi); err != nil {
		t.Errorf("titolare di nuovo titolare: %v", err)
	}
	if err := u.CambiaRuolo(ctx, 2, Titolare, chi); err != nil {
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
		if err := u.CambiaRuolo(ctx, c.id, c.ruolo, chi); !errors.Is(err, c.err) {
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
	go func() { errs <- u.Disattiva(ctx, 1, chi) }()
	go func() { errs <- u.CambiaRuolo(ctx, altro, Dipendente, chi) }()
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

// inventario: ACC-10, ACC-11, ADM-17
// La riga d'audit sta nella stessa transazione (deactivate_user/2, set_role/3): user.deactivated
// e user.role_changed con il ruolo, attore dal contesto; un rifiuto non scrive nessuna riga; se la
// riga non si può scrivere, la modifica si annulla.
func TestDisattivaECambiaRuoloScrivonoAudit(t *testing.T) {
	u, d := utentiProva(t)
	id := aggiungi(t, d, "banco@esempio.it", "dipendente", vettoriStandalone[0].hash, 1)
	if err := u.CambiaRuolo(ctx, id, Titolare, chi); err != nil {
		t.Fatal(err)
	}
	if err := u.Disattiva(ctx, id, chi); err != nil {
		t.Fatal(err)
	}
	_ = u.Disattiva(ctx, 1, chi)               // ultimo titolare: rifiuto
	_ = u.CambiaRuolo(ctx, 1, Dipendente, chi) // idem
	_ = u.CambiaRuolo(ctx, 99, Titolare, chi)  // non trovato
	righe := eventi(t, d)
	attese := []string{
		fmt.Sprintf("titolare@esempio.it|user.role_changed|user|%d|ok|{\"role\":\"titolare\"}", id),
		fmt.Sprintf("titolare@esempio.it|user.deactivated|user|%d|ok|{}", id),
	}
	if strings.Join(righe, "\n") != strings.Join(attese, "\n") {
		t.Errorf("righe:\n%s\nattese:\n%s", strings.Join(righe, "\n"), strings.Join(attese, "\n"))
	}
	// Una riga che non si può scrivere (attore vuoto) annulla la modifica.
	altro := aggiungi(t, d, "banco2@esempio.it", "dipendente", vettoriStandalone[0].hash, 1)
	if err := u.Disattiva(ctx, altro, audit.Contesto{}); err == nil {
		t.Fatal("disattivato senza riga d'audit")
	}
	if s := leggiAccount(t, d, altro); s.attivo != 1 {
		t.Error("la disattivazione è rimasta senza la sua riga d'audit")
	}
	if err := u.CambiaRuolo(ctx, altro, Titolare, audit.Contesto{}); err == nil {
		t.Fatal("ruolo cambiato senza riga d'audit")
	}
	if s := leggiAccount(t, d, altro); s.ruolo != Dipendente {
		t.Error("il cambio di ruolo è rimasto senza la sua riga d'audit")
	}
}

func eventi(t *testing.T, d *db.DB) []string {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT attore, azione, risorsa, id_risorsa, esito, dati FROM eventi_audit ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []string
	for rr.Next() {
		var a, b, c, e, f, g string
		if err := rr.Scan(&a, &b, &c, &e, &f, &g); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{a, b, c, e, f, g}, "|"))
	}
	return out
}

// inventario: ACC-06
// L'elenco del titolare (list_users/0): attivi prima, poi i dipendenti prima dei titolari, poi
// email e id; «in attesa» se non c'è password, che non esce mai dal pacchetto; l'ultimo accesso.
func TestElencoUtenti(t *testing.T) {
	u, d := utentiProva(t)
	aggiungi(t, d, "aaa@esempio.it", "titolare", vettoriStandalone[0].hash, 1)
	aggiungi(t, d, "zzz@esempio.it", "dipendente", vettoriStandalone[0].hash, 0)
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET ultimo_accesso_il = ?, nome_visibile = 'Anna' WHERE id = 1`, istante); err != nil {
		t.Fatal(err)
	}
	ee, err := u.ElencoUtenti(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ee {
		ultimo := "-"
		if e.UltimoAccesso != nil {
			ultimo = e.UltimoAccesso.Format(db.FormatoIstante)
		}
		got = append(got, fmt.Sprintf("%s %s %s %v %v %s", e.Email, e.Nome, e.Ruolo, e.Attivo, e.InAttesa, ultimo))
	}
	attesi := []string{
		"invitato@esempio.it  dipendente true true -",
		"rotto@esempio.it  dipendente true false -",
		"aaa@esempio.it  titolare true false -",
		"titolare@esempio.it Anna titolare true false " + istante,
		"uscito@esempio.it  dipendente false false -",
		"zzz@esempio.it  dipendente false false -",
	}
	if strings.Join(got, "\n") != strings.Join(attesi, "\n") {
		t.Errorf("elenco:\n%s\natteso:\n%s", strings.Join(got, "\n"), strings.Join(attesi, "\n"))
	}
}
