package db

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// La riga 1 di oreficerie, «il negozio stesso» (slice 086): la migrazione 0020 la semina come la migrazione
// 20260906213000 dello standalone (`INSERT OR IGNORE INTO oreficerie(id, name) VALUES (1, 'Negozio')`), perché
// su un database senza import la prima oreficeria creata non prenda l'id 1 (le esterne sono id > 1).

// migrazioniFinoA19 sono le migrazioni del prodotto senza la 0020 e le successive: il database com'era prima.
func migrazioniFinoA19(t *testing.T) fstest.MapFS {
	t.Helper()
	sub, err := fs.Sub(migrazioniProdotto, "migrazioni")
	if err != nil {
		t.Fatal(err)
	}
	voci, err := fs.ReadDir(sub, ".")
	if err != nil {
		t.Fatal(err)
	}
	f := fstest.MapFS{}
	for _, v := range voci {
		if strings.Compare(v.Name(), "0020") >= 0 {
			continue
		}
		b, err := fs.ReadFile(sub, v.Name())
		if err != nil {
			t.Fatal(err)
		}
		f[v.Name()] = &fstest.MapFile{Data: b}
	}
	return f
}

// inventario: ORE-01
// Su un database nuovo la riga 1 c'è («Negozio», senza telefono) e la prima oreficeria creata prende l'id 2.
func TestMigrazioneSeminaIlNegozio(t *testing.T) {
	d, _ := apriProva(t)
	if _, err := d.Migra(ctx, orologio()); err != nil {
		t.Fatal(err)
	}
	var nome string
	var tel *string
	if err := d.Lettura.QueryRow(`SELECT nome, telefono FROM oreficerie WHERE id = 1`).Scan(&nome, &tel); err != nil {
		t.Fatalf("riga 1: %v", err)
	}
	if nome != "Negozio" || tel != nil {
		t.Fatalf("riga 1: %q %v", nome, tel)
	}
	res, err := d.Scrittura.Exec(`INSERT INTO oreficerie (nome) VALUES ('Prima esterna')`)
	if err != nil {
		t.Fatal(err)
	}
	if id, _ := res.LastInsertId(); id != 2 {
		t.Fatalf("la prima oreficeria nuova ha id %d, atteso 2", id)
	}
}

// inventario: ORE-01
// Su un database che ha già la riga 1 (un import, o una copia di prima della 0020) la migrazione non la tocca: né il
// nome né il telefono; e se l'id 1 è occupato da un'altra riga (o il nome «Negozio» da un'altra) `INSERT OR IGNORE`
// la lascia com'è, senza errore, e non ne aggiunge una.
func TestMigrazioneNegozioNonTocca(t *testing.T) {
	for _, c := range []struct {
		nome, sql string
		atteso    string
	}{
		{"riga 1 già presente", `INSERT INTO oreficerie (id, nome, telefono) VALUES (1, 'Il negozio di prova', '049')`, "1:Il negozio di prova:049"},
		{"id 1 di un'altra riga", `INSERT INTO oreficerie (id, nome) VALUES (1, 'Oreficeria Altra')`, "1:Oreficeria Altra:-"},
		{"nome già di un'altra riga", `INSERT INTO oreficerie (id, nome) VALUES (7, 'Negozio')`, "7:Negozio:-"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			d, _ := apriProva(t)
			if _, err := d.migraDa(ctx, migrazioniFinoA19(t), orologio()); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Scrittura.Exec(c.sql); err != nil {
				t.Fatal(err)
			}
			if app, err := d.Migra(ctx, orologio()); err != nil || len(app) == 0 {
				t.Fatalf("applicate %v, %v", app, err)
			}
			var s string
			if err := d.Lettura.QueryRow(`SELECT group_concat(id || ':' || nome || ':' || coalesce(telefono, '-'), ',') FROM oreficerie`).Scan(&s); err != nil {
				t.Fatal(err)
			}
			if s != c.atteso {
				t.Fatalf("oreficerie %q, attese %q", s, c.atteso)
			}
		})
	}
}
