package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// spazioServe è lo spazio che il test si aspetta, calcolato qui a parte: due volte la sorgente,
// più i file della cartella media (se c'è), più 1 GiB.
func spazioServe(t *testing.T, o Opzioni) uint64 {
	t.Helper()
	fi, err := os.Stat(o.Sorgente)
	if err != nil {
		t.Fatal(err)
	}
	n := 2*fi.Size() + 1<<30
	if o.Media != "" {
		for _, k := range mediaSintetici {
			n += int64(len(contenutoMedia(k)))
		}
	}
	return uint64(n)
}

// conSpazio fa leggere all'import lo spazio libero dato, e ricorda la cartella guardata.
func conSpazio(t *testing.T, libero uint64, errLettura error) *string {
	t.Helper()
	guardata := new(string)
	prima := spazioLibero
	spazioLibero = func(dir string) (uint64, error) { *guardata = dir; return libero, errLettura }
	t.Cleanup(func() { spazioLibero = prima })
	return guardata
}

// inventario: OPS-07
// Prima di copiare la sorgente l'import guarda lo spazio libero nella cartella della destinazione:
// servono almeno 2 × la sorgente + i file della cartella media (con -media) + 1 GiB. Lo spazio
// appena sufficiente passa; un byte in meno rifiuta prima della copia, senza cartella di lavoro e
// senza nessun file nuovo, con quanto serve e quanto c'è. La cartella media entra nel conto; con
// -senza-media no; sottocartelle e link della cartella media non contano. Uno spazio che non si
// legge rifiuta anche lui.
func TestSpazioLiberoPrimaDiCopiare(t *testing.T) {
	rifiuta := func(t *testing.T, dir string, o Opzioni, serve, libero uint64) {
		t.Helper()
		copiata := false
		dopoLaCopia = func() { copiata = true }
		t.Cleanup(func() { dopoLaCopia = nil })
		prima := fileDi(t, dir)
		_, err := Importa(context.Background(), o)
		if err == nil {
			t.Fatal("import partito senza spazio")
		}
		for _, s := range []string{"spazio", fmt.Sprint(serve), fmt.Sprint(libero)} {
			if !strings.Contains(err.Error(), s) {
				t.Errorf("errore %q senza %q", err, s)
			}
		}
		if copiata {
			t.Error("la sorgente è stata copiata prima del rifiuto")
		}
		if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
			t.Errorf("file prima %v, dopo %v", prima, dopo)
		}
		if resti, err := cartelleDiLavoro(dir); err != nil || len(resti) != 0 {
			t.Errorf("cartelle di lavoro %v %v", resti, err)
		}
	}
	t.Run("appena sufficiente", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		// Il report altrove: lo spazio si guarda nella cartella della destinazione.
		o.Report = filepath.Join(t.TempDir(), "report.json")
		// Una sottocartella e un link nella cartella media non contano: non sono file media.
		if err := os.Mkdir(filepath.Join(o.Media, "sottocartella"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(o.Media, "sottocartella", "dentro"), make([]byte, 4096), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(o.Sorgente, filepath.Join(o.Media, "link")); err != nil {
			t.Fatal(err)
		}
		guardata := conSpazio(t, spazioServe(t, o), nil)
		if es := importa(t, o); es.Verdetto != Riconciliato {
			r, _ := leggiReport(t, o.Report)
			t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
		}
		if *guardata != dir {
			t.Errorf("spazio guardato in %q, non nella cartella della destinazione %q", *guardata, dir)
		}
	})
	t.Run("un byte in meno", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		serve := spazioServe(t, o)
		conSpazio(t, serve-1, nil)
		rifiuta(t, dir, o, serve, serve-1)
	})
	t.Run("la cartella media entra nel conto", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		serve := spazioServe(t, o)
		senzaMedia := serve - uint64(lunghezza(mediaSintetici...))
		conSpazio(t, senzaMedia, nil)
		rifiuta(t, dir, o, serve, senzaMedia)
	})
	t.Run("un file grande nella cartella media", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		grande := make([]byte, 1<<20)
		if err := os.WriteFile(filepath.Join(o.Media, "file-in-piu"), grande, 0o400); err != nil {
			t.Fatal(err)
		}
		serve := spazioServe(t, o) + 1<<20
		conSpazio(t, serve-1, nil)
		rifiuta(t, dir, o, serve, serve-1)
	})
	t.Run("senza media non entra", func(t *testing.T) {
		_, o := preparaImport(t, nil)
		o.Media, o.SenzaMedia = "", true
		conSpazio(t, spazioServe(t, o), nil)
		if es := importa(t, o); es.Verdetto != Riconciliato {
			r, _ := leggiReport(t, o.Report)
			t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
		}
	})
	t.Run("senza media, un byte in meno", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		o.Media, o.SenzaMedia = "", true
		serve := spazioServe(t, o)
		conSpazio(t, serve-1, nil)
		rifiuta(t, dir, o, serve, serve-1)
	})
	t.Run("spazio che non si legge", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		conSpazio(t, 1<<62, errors.New("statfs finto"))
		prima := fileDi(t, dir)
		_, err := Importa(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "spazio") || !strings.Contains(err.Error(), "statfs finto") {
			t.Errorf("errore %v", err)
		}
		if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
			t.Errorf("file prima %v, dopo %v", prima, dopo)
		}
	})
}

// Lo spazio libero vero della cartella: un numero positivo, e una cartella che non c'è è un errore.
func TestSpazioLiberoVero(t *testing.T) {
	n, err := spazioLiberoDi(t.TempDir())
	if err != nil || n == 0 {
		t.Errorf("spazio libero %d %v", n, err)
	}
	if _, err := spazioLiberoDi(filepath.Join(t.TempDir(), "non-c-e")); err == nil {
		t.Error("nessun errore per una cartella che non c'è")
	}
}
