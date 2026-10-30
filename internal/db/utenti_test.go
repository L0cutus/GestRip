package db

import (
	"strings"
	"testing"
)

const istante = "2026-10-30T09:15:07.000000Z"

func inserisciUtente(d *DB, email, ruolo string, attivo int) error {
	_, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, attivo, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?)`,
		email, ruolo, attivo, istante, istante)
	return err
}

func utentiMigrati(t *testing.T) *DB {
	t.Helper()
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	return d
}

// Le regole della tabella users dello standalone (migrazioni 20260905, 20260907, 20260915,
// 20261002 a go-baseline), con i nomi del Go: ruolo titolare o dipendente (owner e staff, l'import
// traduce), dipendente di default, attivo di default, session_version da 0, hash della password
// nullo finché l'invito non è accettato.
func TestUtentiDefaultERegole(t *testing.T) {
	d := utentiMigrati(t)
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES ('anna@esempio.it', ?, ?)`, istante, istante); err != nil {
		t.Fatal(err)
	}
	var ruolo string
	var attivo, versione int
	var hash, nome, ultimo, disattivato any
	if err := d.Lettura.QueryRow(`SELECT ruolo, attivo, versione_sessione, hash_password, nome_visibile, ultimo_accesso_il, disattivato_il FROM utenti`).
		Scan(&ruolo, &attivo, &versione, &hash, &nome, &ultimo, &disattivato); err != nil {
		t.Fatal(err)
	}
	if ruolo != "dipendente" || attivo != 1 || versione != 0 || hash != nil || nome != nil || ultimo != nil || disattivato != nil {
		t.Errorf("default: ruolo %q attivo %d versione %d hash %v nome %v ultimo %v disattivato %v", ruolo, attivo, versione, hash, nome, ultimo, disattivato)
	}
	rifiuti := map[string]string{
		"ruolo sconosciuto":     `INSERT INTO utenti (email, ruolo, creato_il, aggiornato_il) VALUES ('b@esempio.it', 'owner', '` + istante + `', '` + istante + `')`,
		"attivo non booleano":   `INSERT INTO utenti (email, attivo, creato_il, aggiornato_il) VALUES ('c@esempio.it', 2, '` + istante + `', '` + istante + `')`,
		"senza email":           `INSERT INTO utenti (creato_il, aggiornato_il) VALUES ('` + istante + `', '` + istante + `')`,
		"senza creato_il":       `INSERT INTO utenti (email, aggiornato_il) VALUES ('d@esempio.it', '` + istante + `')`,
		"versione negativa":     `INSERT INTO utenti (email, versione_sessione, creato_il, aggiornato_il) VALUES ('e@esempio.it', -1, '` + istante + `', '` + istante + `')`,
		"email non testo":       `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES (x'00', '` + istante + `', '` + istante + `')`,
		"email vuota":           `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES ('', '` + istante + `', '` + istante + `')`,
		"email maiuscola":       `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES ('Anna@esempio.it', '` + istante + `', '` + istante + `')`,
		"email con spazi":       `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES (' b@esempio.it', '` + istante + `', '` + istante + `')`,
		"hash vuoto":            `INSERT INTO utenti (email, hash_password, creato_il, aggiornato_il) VALUES ('f@esempio.it', '', '` + istante + `', '` + istante + `')`,
		"istante con spazio":    `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES ('g@esempio.it', '2026-09-05 07:00:00', '` + istante + `')`,
		"istante a caso":        `INSERT INTO utenti (email, creato_il, aggiornato_il) VALUES ('h@esempio.it', '` + istante + `', 'boh')`,
		"ultimo accesso a caso": `INSERT INTO utenti (email, ultimo_accesso_il, creato_il, aggiornato_il) VALUES ('i@esempio.it', '2026', '` + istante + `', '` + istante + `')`,
		"disattivato a caso":    `INSERT INTO utenti (email, disattivato_il, creato_il, aggiornato_il) VALUES ('j@esempio.it', 'ieri', '` + istante + `', '` + istante + `')`,
	}
	for nome, sql := range rifiuti {
		if _, err := d.Scrittura.Exec(sql); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// Come lo standalone (app_users_bp.py:41, indice parziale): un indirizzo è unico solo tra gli
// account attivi; chi è uscito può essere invitato di nuovo, e la riga vecchia resta.
func TestUtentiEmailUnicaTraGliAttivi(t *testing.T) {
	d := utentiMigrati(t)
	if err := inserisciUtente(d, "anna@esempio.it", "titolare", 1); err != nil {
		t.Fatal(err)
	}
	if err := inserisciUtente(d, "anna@esempio.it", "dipendente", 1); err == nil {
		t.Error("due account attivi con lo stesso indirizzo")
	}
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET attivo = 0, disattivato_il = ? WHERE email = 'anna@esempio.it'`, istante); err != nil {
		t.Fatal(err)
	}
	if err := inserisciUtente(d, "anna@esempio.it", "dipendente", 1); err != nil {
		t.Errorf("reinvito dopo la disattivazione rifiutato: %v", err)
	}
	if err := inserisciUtente(d, "anna@esempio.it", "dipendente", 0); err != nil {
		t.Errorf("seconda riga inattiva rifiutata: %v", err)
	}
}

// Gli id non si riusano mai (AUTOINCREMENT): l'audit nomina gli utenti per numero, e un numero
// riusato farebbe di due persone una sola (lo standalone conserva sqlite_sequence apposta).
func TestUtentiIDMaiRiusati(t *testing.T) {
	d := utentiMigrati(t)
	for _, e := range []string{"a@esempio.it", "b@esempio.it"} {
		if err := inserisciUtente(d, e, "dipendente", 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Scrittura.Exec(`DELETE FROM utenti WHERE email = 'b@esempio.it'`); err != nil {
		t.Fatal(err)
	}
	if err := inserisciUtente(d, "c@esempio.it", "dipendente", 1); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := d.Lettura.QueryRow(`SELECT id FROM utenti WHERE email = 'c@esempio.it'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 3 {
		t.Errorf("id del terzo utente = %d, atteso 3 (il 2 cancellato non si riusa)", id)
	}
	var sql string
	if err := d.Lettura.QueryRow(`SELECT sql FROM sqlite_schema WHERE name = 'utenti'`).Scan(&sql); err != nil || !strings.Contains(sql, "AUTOINCREMENT") {
		t.Errorf("utenti senza AUTOINCREMENT: %q %v", sql, err)
	}
}

// La ricerca di un account attivo per email usa l'indice parziale solo scritta con `attivo = 1`
// (misurato dalla review: `AND attivo` legge tutta la tabella).
func TestUtentiRicercaUsaLIndice(t *testing.T) {
	d := utentiMigrati(t)
	rows, err := d.Lettura.Query(`EXPLAIN QUERY PLAN SELECT id FROM utenti WHERE email = ? AND attivo = 1`, "a@esempio.it")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	piano := ""
	for rows.Next() {
		var a, b, c int
		var s string
		if err := rows.Scan(&a, &b, &c, &s); err != nil {
			t.Fatal(err)
		}
		piano += s + "\n"
	}
	if !strings.Contains(piano, "utenti_email_attivi") {
		t.Errorf("piano senza l'indice: %s", piano)
	}
}
