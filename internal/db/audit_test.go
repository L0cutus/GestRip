package db

import (
	"strings"
	"testing"
)

func inserisciEvento(d *DB, attore, azione, risorsa, id, esito, dati string) error {
	_, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		attore, azione, risorsa, id, esito, dati, istante)
	return err
}

// inventario: ADM-17
// Le regole di audit_events dello standalone (migrazione 20260905070000 e AuditEvent.changeset):
// attore, azione, risorsa, id della risorsa ed esito obbligatori e non vuoti, con le lunghezze
// massime; dati un oggetto JSON ({} di default); ip e user agent facoltativi con il loro massimo.
// In più: istante nella forma fissa, id mai riusati.
func TestEventiAuditRegole(t *testing.T) {
	d := utentiMigrati(t)
	if err := inserisciEvento(d, "titolare@esempio.it", "user.deactivated", "user", "7", "ok", "{}"); err != nil {
		t.Fatal(err)
	}
	for nome, args := range map[string][6]string{
		"attore vuoto":        {"", "a", "r", "1", "ok", "{}"},
		"azione vuota":        {"x", "", "r", "1", "ok", "{}"},
		"risorsa vuota":       {"x", "a", "", "1", "ok", "{}"},
		"id vuoto":            {"x", "a", "r", "", "ok", "{}"},
		"esito vuoto":         {"x", "a", "r", "1", "", "{}"},
		"attore lungo":        {strings.Repeat("x", 256), "a", "r", "1", "ok", "{}"},
		"azione lunga":        {"x", strings.Repeat("a", 101), "r", "1", "ok", "{}"},
		"risorsa lunga":       {"x", "a", strings.Repeat("r", 101), "1", "ok", "{}"},
		"id lungo":            {"x", "a", "r", strings.Repeat("1", 101), "ok", "{}"},
		"esito lungo":         {"x", "a", "r", "1", strings.Repeat("o", 41), "{}"},
		"dati non JSON":       {"x", "a", "r", "1", "ok", "{"},
		"dati non un oggetto": {"x", "a", "r", "1", "ok", "[1]"},
	} {
		if err := inserisciEvento(d, args[0], args[1], args[2], args[3], args[4], args[5]); err == nil {
			t.Errorf("%s accettato", nome)
		}
	}
	for _, ok := range [][6]string{
		{strings.Repeat("è", 255), strings.Repeat("a", 100), strings.Repeat("r", 100), strings.Repeat("1", 100), strings.Repeat("o", 40), `{"role":"titolare"}`},
	} {
		if err := inserisciEvento(d, ok[0], ok[1], ok[2], ok[3], ok[4], ok[5]); err != nil {
			t.Errorf("ai limiti: %v", err)
		}
	}
	for nome, q := range map[string]string{
		"ip lungo":         `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, ip, creato_il) VALUES ('x','a','r','1','ok','` + strings.Repeat("1", 65) + `', '` + istante + `')`,
		"user agent lungo": `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, user_agent, creato_il) VALUES ('x','a','r','1','ok','` + strings.Repeat("u", 513) + `', '` + istante + `')`,
		"istante libero":   `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il) VALUES ('x','a','r','1','ok','30/10/2026')`,
	} {
		if _, err := d.Scrittura.Exec(q); err == nil {
			t.Errorf("%s accettato", nome)
		}
	}
	var dati string
	if _, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il) VALUES ('x','a','r','1','ok', ?)`, istante); err != nil {
		t.Fatal(err)
	}
	if err := d.Lettura.QueryRow(`SELECT dati FROM eventi_audit ORDER BY id DESC LIMIT 1`).Scan(&dati); err != nil || dati != "{}" {
		t.Errorf("dati di default %q %v", dati, err)
	}
	// Gli id non si riusano: l'ultimo cancellato non torna.
	var ultimo, nuovo int64
	_ = d.Lettura.QueryRow(`SELECT max(id) FROM eventi_audit`).Scan(&ultimo)
	if _, err := d.Scrittura.Exec(`DELETE FROM eventi_audit WHERE id = ?`, ultimo); err != nil {
		t.Fatal(err)
	}
	_ = inserisciEvento(d, "x", "a", "r", "1", "ok", "{}")
	_ = d.Lettura.QueryRow(`SELECT max(id) FROM eventi_audit`).Scan(&nuovo)
	if nuovo <= ultimo {
		t.Errorf("id riusato: %d dopo %d", nuovo, ultimo)
	}
}
