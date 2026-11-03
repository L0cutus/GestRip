package media_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/importer"
	"github.com/L0cutus/gestrip/internal/media"
)

// nonOggetti sono le colonne dello schema che per il nome sembrano una chiave dell'archivio e non
// lo sono, con il perché.
var nonOggetti = map[media.Riferimento]string{
	{Tabella: "impostazioni", Colonna: "chiave"}:        "il nome di un'impostazione",
	{Tabella: "cancellazioni_media", Colonna: "chiave"}: "la coda stessa: una chiave in coda è da cancellare, non usata",
}

// inventario: RIP-S-57
// media.Riferimenti è l'elenco unico delle colonne che nominano un oggetto dell'archivio, e la
// pulizia non cancella un oggetto che una di loro nomina. Questo test fallisce se una tabella
// nuova (di una slice futura: firme, vendite, logo…) ha una colonna con «chiave» nel nome che non
// sta né in Riferimenti né in nonOggetti, o se Riferimenti nomina una colonna che non c'è; e se
// Riferimenti non copre ogni colonna di referenced?/1 dello standalone (importer.ColonneMedia
// tradotte con importer.Mappa, tranne la coda).
func TestRiferimentiCopronoLoSchema(t *testing.T) {
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	if _, err := d.Migra(context.Background(), clock.NuovoFinto(time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	r, err := d.Lettura.Query(`SELECT m.name, c.name FROM sqlite_schema m, pragma_table_info(m.name) c
		WHERE m.type = 'table' ORDER BY m.name, c.cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var colonne []media.Riferimento
	for r.Next() {
		var c media.Riferimento
		if err := r.Scan(&c.Tabella, &c.Colonna); err != nil {
			t.Fatal(err)
		}
		colonne = append(colonne, c)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	for _, c := range colonne {
		if !strings.Contains(c.Colonna, "chiave") {
			continue
		}
		if _, ok := nonOggetti[c]; !ok && !slices.Contains(media.Riferimenti, c) {
			t.Errorf("%s.%s sembra una chiave dell'archivio: va in media.Riferimenti (la pulizia deve saperlo) o in nonOggetti con il perché",
				c.Tabella, c.Colonna)
		}
	}
	for _, c := range append(slices.Clone(media.Riferimenti), keys(nonOggetti)...) {
		if !slices.Contains(colonne, c) {
			t.Errorf("%s.%s non c'è nello schema", c.Tabella, c.Colonna)
		}
	}
	var dallImport []media.Riferimento
	for _, cm := range importer.ColonneMedia {
		if cm.Facoltativo {
			continue // media_deletion_jobs: la coda
		}
		g, ok := traduci(cm.Tabella, cm.Chiave)
		if !ok {
			t.Errorf("%s.%s senza traduzione in importer.Mappa", cm.Tabella, cm.Chiave)
			continue
		}
		dallImport = append(dallImport, g)
	}
	a, b := slices.Clone(media.Riferimenti), slices.Clone(dallImport)
	ordina(a)
	ordina(b)
	if !slices.Equal(a, b) {
		t.Errorf("media.Riferimenti %v,\nreferenced?/1 dello standalone %v", a, b)
	}
}

func keys(m map[media.Riferimento]string) []media.Riferimento {
	var v []media.Riferimento
	for k := range m {
		v = append(v, k)
	}
	return v
}

func ordina(v []media.Riferimento) {
	slices.SortFunc(v, func(x, y media.Riferimento) int {
		return strings.Compare(x.Tabella+"."+x.Colonna, y.Tabella+"."+y.Colonna)
	})
}

func traduci(tabella, colonna string) (media.Riferimento, bool) {
	for _, t := range importer.Mappa {
		if t.Standalone != tabella {
			continue
		}
		for _, c := range t.Colonne {
			if c.Standalone == colonna {
				return media.Riferimento{Tabella: t.Go, Colonna: c.Go}, true
			}
		}
	}
	return media.Riferimento{}, false
}
