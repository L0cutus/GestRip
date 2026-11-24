package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il comando esporta-dettati (slice 117; OPS-09), come mix gestrip.export_dettati dello standalone a go-baseline
// (training_export_test.exs, «mix gestrip.export_dettati»). Nomi, telefoni (333 000 …) e dettati inventati.

// dbConDettati è un database di prova con due dettati raccolti: uno che si esporta e uno senza il nome nel testo.
func dbConDettati(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gestrip.db")
	d, err := db.Apri(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Chiudi() }()
	ctx := context.Background()
	if _, err := d.Migra(ctx, clock.Sistema{}); err != nil {
		t.Fatal(err)
	}
	b := buste.Nuove(d, clock.Sistema{})
	for i, r := range []struct{ nome, tel, testo, nomeProposto string }{
		{"Mario Rossi", "333 000 1234", "Cliente Mario Rossi 333 000 1234 anello", "Mario Rossi"},
		{"Giacomo Ferretti", "333 000 5678", "Busta 45 anello da stringere", ""},
	} {
		qr := "QR-C" + string(rune('1'+i))
		c, err := b.Crea(ctx, buste.Accettazione{NomeCliente: r.nome, TelefonoCliente: r.tel, DataAccettazione: "2026-10-31",
			Descrizione: "Anello da stringere", CodiceQR: qr, GemelleConfermate: buste.EtichettaScritta(qr, 2026)}, audit.Contesto{Attore: "prova"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Scrittura.Exec(`INSERT INTO dettati_addestramento (busta_id, testo, proposta, modello, creato_il) VALUES (?, ?, ?, NULL, '2026-10-31T09:00:00.000000Z')`,
			c.ID, r.testo, `{"nome":"`+r.nomeProposto+`","telefono":"","descrizione":"Anello da stringere"}`); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// inventario: OPS-09
// Stampa numeri e nomi di motivi, nient'altro: né nomi, né telefoni, né testi.
func TestEsportaDettatiComando(t *testing.T) {
	dbp := dbConDettati(t)
	file := filepath.Join(t.TempDir(), "dettati.jsonl")
	var out bytes.Buffer
	if err := esportaDettati(context.Background(), opzioniEsporta{DB: dbp, File: file}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "totali=2\nesportati=1\nscartati=1\ncancellati=2\nscartati_nome_non_trovato=1\n" {
		t.Errorf("uscita:\n%s", got)
	}
	for _, s := range []string{"Rossi", "Ferretti", "333", "anello", "Anello"} {
		if strings.Contains(out.String(), s) {
			t.Errorf("l'uscita porta %q", s)
		}
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file %v %v", fi, err)
	}
	// Di nuovo sullo stesso file: rifiutato, niente stampato.
	out.Reset()
	if err := esportaDettati(context.Background(), opzioniEsporta{DB: dbp, File: file}, &out); !errors.Is(err, buste.ErrFileEsistente) || out.Len() != 0 {
		t.Errorf("secondo giro: %v %q", err, out.String())
	}
	if got := messaggioEsportazione(buste.ErrFileEsistente); got != "Il file esiste già: l'esportazione non sovrascrive." {
		t.Errorf("messaggio %q", got)
	}
}

// inventario: OPS-09
// Il percorso è uno e uno solo; un database che non c'è non si crea.
func TestEsportaDettatiArgomenti(t *testing.T) {
	for _, a := range [][]string{{}, {"a.jsonl", "b.jsonl"}, {"-db"}, {"-sconosciuta", "a.jsonl"}, {""}} {
		if _, err := opzioniDiEsporta(a); err == nil {
			t.Errorf("%q accettato", a)
		}
	}
	o, err := opzioniDiEsporta([]string{"-db", "/tmp/x.db", "out.jsonl"})
	if err != nil || o.DB != "/tmp/x.db" || o.File != "out.jsonl" {
		t.Errorf("%+v %v", o, err)
	}
	if o, err := opzioniDiEsporta([]string{"out.jsonl"}); err != nil || o.DB != "/var/lib/gestrip/gestrip.db" {
		t.Errorf("%+v %v", o, err)
	}
	manca := filepath.Join(t.TempDir(), "manca.db")
	file := filepath.Join(t.TempDir(), "dettati.jsonl")
	if err := esportaDettati(context.Background(), opzioniEsporta{DB: manca, File: file}, &bytes.Buffer{}); err == nil {
		t.Error("database inesistente accettato")
	}
	for _, p := range []string{manca, file} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s creato", filepath.Base(p))
		}
	}
}

// inventario: OPS-09
// Nessun percorso nei messaggi: il nome del file (o della cartella, o del database) potrebbe essere quello di un cliente.
// Una cartella che non esiste, una senza permesso di scrittura, un database che non c'è.
func TestEsportaDettatiMessaggiSenzaPercorso(t *testing.T) {
	dbp := dbConDettati(t)
	chiusa := filepath.Join(t.TempDir(), "Cartella Rossi")
	if err := os.Mkdir(chiusa, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(chiusa, 0o700) })
	casi := []struct{ nome, db, file, atteso string }{
		{"cartella che non esiste", dbp, filepath.Join(t.TempDir(), "Manca Bianchi", "Mario Rossi.jsonl"),
			"Esportazione non riuscita: apertura del file: no such file or directory."},
		{"permesso", dbp, filepath.Join(chiusa, "Mario Rossi.jsonl"),
			"Esportazione non riuscita: apertura del file: permission denied."},
		{"database", filepath.Join(t.TempDir(), "Rossi.db"), filepath.Join(t.TempDir(), "Mario Rossi.jsonl"),
			"Esportazione non riuscita: database non trovato: lo crea «gestrip serve»."},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			err := esportaDettati(context.Background(), opzioniEsporta{DB: c.db, File: c.file}, &bytes.Buffer{})
			if err == nil {
				t.Fatal("nessun errore")
			}
			m := messaggioEsportazione(err)
			if m != c.atteso || strings.Contains(m, "Rossi") || strings.Contains(m, "Bianchi") || strings.Contains(m, "/") {
				t.Errorf("messaggio %q", m)
			}
		})
	}
	if got := messaggioEsportazione(&buste.ErrFileTenuto{Mancanti: 2, Lette: 3}); got != "Esportazione non completata: la cancellazione dei dettati ha dato errore, ma 2 righe su 3 non ci sono più: il file resta." {
		t.Errorf("messaggio D-112 %q", got)
	}
}
