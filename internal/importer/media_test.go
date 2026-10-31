package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/clock"
)

// fileMedia è un file di una cartella media visto da un test: byte e permessi.
type fileMedia struct {
	Byte string
	Perm fs.FileMode
}

// alberoMedia legge una cartella media: per ogni nome i byte e i permessi; una voce che non è
// un file regolare ha Byte vuoto e il tipo nei permessi.
func alberoMedia(t *testing.T, dir string) map[string]fileMedia {
	t.Helper()
	voci, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]fileMedia{}
	for _, v := range voci {
		fi, err := os.Lstat(filepath.Join(dir, v.Name()))
		if err != nil {
			t.Fatal(err)
		}
		f := fileMedia{Perm: fi.Mode()}
		if fi.Mode().IsRegular() {
			b, err := os.ReadFile(filepath.Join(dir, v.Name()))
			if err != nil {
				t.Fatal(err)
			}
			f.Byte = string(b)
		}
		m[v.Name()] = f
	}
	return m
}

func famiglia(r Report, tab string) FamigliaMedia {
	if r.Media == nil {
		return FamigliaMedia{}
	}
	for _, f := range r.Media.Famiglie {
		if f.Tabella == tab {
			return f
		}
	}
	return FamigliaMedia{}
}

func lunghezza(ns ...int) int64 {
	var n int64
	for _, i := range ns {
		n += int64(len(contenutoMedia(i)))
	}
	return n
}

// Import completo con la cartella media: ogni file nominato da una riga (logo, firme della busta
// e degli eventi, foto delle buste, delle vendite e delle revisioni, documenti storici, file in
// coda di cancellazione) è nella cartella media accanto alla destinazione, con gli stessi byte,
// permessi 0400 e la cartella 0700; un file nominato da più righe c'è una volta sola. Il report
// dice per famiglia attesi, copiati, mancanti e hash diversi, e i file e i byte copiati; la
// cartella della sorgente non cambia.
func TestImportMediaCompleto(t *testing.T) {
	dir, o := preparaImport(t, nil)
	prima := alberoMedia(t, o.Media)
	es := importa(t, o)
	r, b := leggiReport(t, o.Report)
	if es.Verdetto != Riconciliato || len(r.Avvisi) != 0 {
		t.Fatalf("verdetto %s, differenze %v, avvisi %v\n%s", es.Verdetto, r.Differenze, r.Avvisi, b)
	}
	md := CartellaMediaDi(o.Destinazione)
	if md != filepath.Join(dir, "media") {
		t.Errorf("cartella media della destinazione %s", md)
	}
	fi, err := os.Lstat(md)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("cartella media: %v %v", fi, err)
	}
	atteso := map[string]fileMedia{}
	for _, n := range mediaSintetici {
		atteso[chiaveMedia(n)] = fileMedia{Byte: string(contenutoMedia(n)), Perm: 0o400}
	}
	if got := alberoMedia(t, md); !reflect.DeepEqual(got, atteso) {
		t.Errorf("media della destinazione:\n%v\natteso\n%v", got, atteso)
	}
	if dopo := alberoMedia(t, o.Media); !reflect.DeepEqual(prima, dopo) {
		t.Error("la cartella media della sorgente è cambiata")
	}
	attese := []FamigliaMedia{
		{Tabella: "branding_assets", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(1)},
		{Tabella: "repairs", Colonna: "signature_storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(6)},
		{Tabella: "repair_photos", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(2)},
		{Tabella: "delivery_signature_events", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(6)},
		{Tabella: "sale_photos", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(3)},
		{Tabella: "sale_revision_photos", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(4)},
		{Tabella: "historical_documents", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(5)},
		{Tabella: "media_deletion_jobs", Colonna: "storage_key", Attesi: 2, Copiati: 1, GiaCancellati: 1, Byte: lunghezza(7)},
	}
	want := &MediaReport{Importati: true, Famiglie: attese, FileCopiati: 7, ByteCopiati: lunghezza(mediaSintetici...)}
	if !reflect.DeepEqual(r.Media, want) {
		g, _ := json.Marshal(r.Media)
		w, _ := json.Marshal(want)
		t.Errorf("media nel report:\n%s\natteso\n%s", g, w)
	}
	if bytes.Contains(b, []byte(o.Media)) {
		t.Error("il report contiene il percorso della cartella media della sorgente")
	}
}

// rimpiazza toglie il file media n dalla cartella e ci mette quello che fa f (niente: mancante).
func rimpiazza(t *testing.T, dir string, n int, f func(p string)) {
	t.Helper()
	p := filepath.Join(dir, chiaveMedia(n))
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if f != nil {
		f(p)
	}
}

// Un file nominato che non torna è una differenza bloccante con tabella, id e colonna, mai il
// contenuto o l'indirizzo: mancante, contenuto diverso dall'indirizzo, sha256 della riga diverso,
// byte_size diverso, un link (anche verso il file giusto), una cartella, una chiave che non è un
// indirizzo (fuori dalla cartella con «..», assoluta, maiuscola). Niente si attiva: né il database
// né la cartella media, e nessun temporaneo resta. Un file in coda di cancellazione che non c'è
// più non è una differenza.
func TestMediaCheNonTornano(t *testing.T) {
	casi := []struct {
		nome    string
		ritocca func(*sql.DB)
		file    func(t *testing.T, media string)
		voci    []Voce
	}{
		{"foto mancante", nil, func(t *testing.T, m string) { rimpiazza(t, m, 2, nil) },
			[]Voce{{Codice: "media/mancante", Tabella: "repair_photos", Id: ptr(1), Colonna: "storage_key"}}},
		{"firma mancante: due righe", nil, func(t *testing.T, m string) { rimpiazza(t, m, 6, nil) },
			[]Voce{{Codice: "media/mancante", Tabella: "repairs", Id: ptr(1), Colonna: "signature_storage_key"},
				{Codice: "media/mancante", Tabella: "delivery_signature_events", Id: ptr(1), Colonna: "storage_key"}}},
		{"contenuto diverso", nil, func(t *testing.T, m string) {
			rimpiazza(t, m, 5, func(p string) { scriviByte(t, p, stessaLunghezza(5)) })
		}, []Voce{{Codice: "media/hash_diverso", Tabella: "historical_documents", Id: ptr(1), Colonna: "storage_key"}}},
		// Nello standalone il CHECK di branding_assets e i trigger delle foto lo impediscono; i
		// documenti storici hanno solo il trigger, creato qui dopo le righe.
		{"sha256 della riga diverso", func(c *sql.DB) {
			esegui(c, `UPDATE historical_documents SET sha256 = ?`, impronta(77))
		}, nil, []Voce{{Codice: "media/hash_diverso", Tabella: "historical_documents", Id: ptr(1), Colonna: "sha256"}}},
		{"byte_size diverso", func(c *sql.DB) { esegui(c, `UPDATE sale_photos SET byte_size = byte_size + 1`) }, nil,
			[]Voce{{Codice: "media/byte_diversi", Tabella: "sale_photos", Id: ptr(1), Colonna: "byte_size"}}},
		{"byte della firma diversi", func(c *sql.DB) { esegui(c, `UPDATE repairs SET signature_byte_size = 3 WHERE id = 1`) }, nil,
			[]Voce{{Codice: "media/byte_diversi", Tabella: "repairs", Id: ptr(1), Colonna: "signature_byte_size"}}},
		// Il file l'ha già copiato una riga senza dimensione (l'evento della firma viene prima delle
		// foto delle vendite): la dimensione della foto si confronta con il file copiato.
		{"byte diversi su un file già copiato", func(c *sql.DB) {
			esegui(c, `UPDATE delivery_signature_events SET storage_key = ?`, chiaveMedia(3))
			esegui(c, `UPDATE sale_photos SET byte_size = byte_size + 1`)
		}, nil, []Voce{{Codice: "media/byte_diversi", Tabella: "sale_photos", Id: ptr(1), Colonna: "byte_size"}}},
		{"link al file giusto", nil, func(t *testing.T, m string) {
			altrove := filepath.Join(t.TempDir(), "vero")
			if err := os.WriteFile(altrove, contenutoMedia(3), 0o400); err != nil {
				t.Fatal(err)
			}
			rimpiazza(t, m, 3, func(p string) {
				if err := os.Symlink(altrove, p); err != nil {
					t.Fatal(err)
				}
			})
		}, []Voce{{Codice: "media/non_regolare", Tabella: "sale_photos", Id: ptr(1), Colonna: "storage_key"}}},
		{"cartella", nil, func(t *testing.T, m string) {
			rimpiazza(t, m, 4, func(p string) {
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			})
		}, []Voce{{Codice: "media/non_regolare", Tabella: "sale_revision_photos", Id: ptr(1), Colonna: "storage_key"}}},
		{"chiave fuori dalla cartella", func(c *sql.DB) {
			esegui(c, `UPDATE repairs SET signature_storage_key = '../fuori' WHERE id = 1`)
		}, func(t *testing.T, m string) {
			if err := os.WriteFile(filepath.Join(filepath.Dir(m), "fuori"), contenutoMedia(6), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []Voce{{Codice: "media/chiave_non_valida", Tabella: "repairs", Id: ptr(1), Colonna: "signature_storage_key"}}},
		{"chiave assoluta", func(c *sql.DB) {
			esegui(c, `UPDATE repairs SET signature_storage_key = ? WHERE id = 1`, "/"+chiaveMedia(6))
		}, nil, []Voce{{Codice: "media/chiave_non_valida", Tabella: "repairs", Id: ptr(1), Colonna: "signature_storage_key"}}},
		{"chiave maiuscola", func(c *sql.DB) {
			esegui(c, `UPDATE delivery_signature_events SET storage_key = ?`, strings.ToUpper(chiaveMedia(6)))
		}, nil, []Voce{{Codice: "media/chiave_non_valida", Tabella: "delivery_signature_events", Id: ptr(1), Colonna: "storage_key"}}},
		{"chiave in coda non valida", func(c *sql.DB) {
			esegui(c, `UPDATE media_deletion_jobs SET storage_key = 'non-un-indirizzo' WHERE storage_key = ?`, chiaveMedia(8))
		}, nil, []Voce{{Codice: "media/chiave_non_valida", Tabella: "media_deletion_jobs", Id: ptr(2), Colonna: "storage_key"}}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			if c.file != nil {
				c.file(t, o.Media)
			}
			prima := alberoMedia(t, o.Media)
			r := controllaBloccato(t, dir, o, importa(t, o))
			for _, v := range c.voci {
				if !slices.ContainsFunc(r.Differenze, func(d Voce) bool { return reflect.DeepEqual(d, v) }) {
					b, _ := json.Marshal(r.Differenze)
					t.Errorf("differenze %s, manca %+v", b, v)
				}
			}
			_, b := leggiReport(t, o.Report)
			for _, n := range append(mediaSintetici, 8) {
				if bytes.Contains(b, []byte(chiaveMedia(n))) {
					t.Errorf("il report contiene l'indirizzo del file %d", n)
				}
			}
			if bytes.Contains(b, []byte("byte inventati")) || bytes.Contains(b, []byte("fuori")) {
				t.Error("il report contiene un contenuto o una chiave")
			}
			if dopo := alberoMedia(t, o.Media); !reflect.DeepEqual(prima, dopo) {
				t.Error("la cartella media della sorgente è cambiata")
			}
			// Una differenza solo dove c'è: lo stesso file, giusto per un'altra riga, non si
			// segna anche lì (la firma della busta ha i byte sbagliati, l'evento no).
			if c.nome == "byte della firma diversi" && ha(r.Differenze, "media/byte_diversi", "delivery_signature_events", 0, "") {
				t.Errorf("differenza anche sull'evento: %+v", r.Differenze)
			}
		})
	}
	t.Run("in coda e già cancellato", func(t *testing.T) {
		_, o := preparaImport(t, nil)
		rimpiazza(t, o.Media, 7, nil)
		if es := importa(t, o); es.Verdetto != Riconciliato {
			r, _ := leggiReport(t, o.Report)
			t.Fatalf("verdetto %s, differenze %+v", es.Verdetto, r.Differenze)
		}
		r, _ := leggiReport(t, o.Report)
		if f := famiglia(r, "media_deletion_jobs"); f.Attesi != 2 || f.Copiati != 0 || f.Mancanti != 0 || f.GiaCancellati != 2 {
			t.Errorf("coda: %+v", f)
		}
	})
	t.Run("conteggi per famiglia", func(t *testing.T) {
		dir, o := preparaImport(t, func(c *sql.DB) { esegui(c, `UPDATE sale_photos SET byte_size = byte_size + 1`) })
		rimpiazza(t, o.Media, 2, nil)
		rimpiazza(t, o.Media, 5, func(p string) { scriviByte(t, p, stessaLunghezza(5)) })
		r := controllaBloccato(t, dir, o, importa(t, o))
		casi := map[string]FamigliaMedia{
			"repair_photos":        {Tabella: "repair_photos", Colonna: "storage_key", Attesi: 1, Mancanti: 1},
			"historical_documents": {Tabella: "historical_documents", Colonna: "storage_key", Attesi: 1, HashDiversi: 1},
			"sale_photos":          {Tabella: "sale_photos", Colonna: "storage_key", Attesi: 1, AltreDifferenze: 1},
			"branding_assets":      {Tabella: "branding_assets", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: lunghezza(1)},
		}
		for tab, want := range casi {
			if got := famiglia(r, tab); got != want {
				t.Errorf("%s: %+v, atteso %+v", tab, got, want)
			}
		}
	})
}

// I file della cartella media che nessuna riga nomina (foto tolte da una busta, che lo standalone
// tiene finché la pulizia non li raccoglie, e i temporanei di un caricamento interrotto) non si
// copiano: il report li conta come avviso, senza nomi, e il verdetto resta riconciliato.
func TestMediaInPiu(t *testing.T) {
	_, o := preparaImport(t, nil)
	orfano := []byte("byte inventati di una foto tolta")
	h := sha256.Sum256(orfano)
	if err := os.WriteFile(filepath.Join(o.Media, hex.EncodeToString(h[:])), orfano, 0o400); err != nil {
		t.Fatal(err)
	}
	scriviFile(t, filepath.Join(o.Media, ".upload-temporaneo"))
	if err := os.Mkdir(filepath.Join(o.Media, "sottocartella"), 0o700); err != nil {
		t.Fatal(err)
	}
	es := importa(t, o)
	r, b := leggiReport(t, o.Report)
	if es.Verdetto != Riconciliato {
		t.Fatalf("verdetto %s, differenze %+v", es.Verdetto, r.Differenze)
	}
	if r.Media == nil {
		t.Fatal("il report non ha i media")
	}
	if !reflect.DeepEqual(r.Avvisi, []Voce{{Codice: "media/in_piu", Righe: ptr(3)}}) || r.Media.InPiu != 3 {
		t.Errorf("avvisi %+v, in più %d", r.Avvisi, r.Media.InPiu)
	}
	if got := alberoMedia(t, CartellaMediaDi(o.Destinazione)); len(got) != len(mediaSintetici) {
		t.Errorf("copiati %d file, attesi %d", len(got), len(mediaSintetici))
	}
	if bytes.Contains(b, []byte(hex.EncodeToString(h[:]))) || bytes.Contains(b, []byte("upload")) || bytes.Contains(b, []byte("sottocartella")) {
		t.Error("il report nomina un file in più")
	}
}

// Senza la cartella media: se il database nomina file (righe in coda di cancellazione escluse)
// l'import si blocca e il report dice quante righe; con SenzaMedia (scelta esplicita) importa il database, non crea la cartella media e lo
// segnala come avviso; un database che non nomina file si importa senza cartella.
func TestMediaSenzaCartella(t *testing.T) {
	t.Run("rifiuto", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		o.Media = ""
		r := controllaBloccato(t, dir, o, importa(t, o))
		if !slices.ContainsFunc(r.Differenze, func(v Voce) bool {
			return reflect.DeepEqual(v, Voce{Codice: "media/cartella_non_data", Righe: ptr(7)})
		}) {
			t.Errorf("differenze %+v", r.Differenze)
		}
	})
	t.Run("senza media esplicito", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		o.Media, o.SenzaMedia = "", true
		if es := importa(t, o); es.Verdetto != Riconciliato {
			t.Fatalf("verdetto %s", es.Verdetto)
		}
		r, _ := leggiReport(t, o.Report)
		if !reflect.DeepEqual(r.Avvisi, []Voce{{Codice: "media/non_importati", Righe: ptr(7)}}) || r.Media == nil || r.Media.Importati {
			t.Errorf("avvisi %+v, media %+v", r.Avvisi, r.Media)
		}
		if f := famiglia(r, "repair_photos"); f.Attesi != 1 || f.Copiati != 0 {
			t.Errorf("foto: %+v", f)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "report.json", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
	})
	t.Run("database senza file", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "standalone.db")
		creaSorgente(t, src, true, nil)
		o := Opzioni{Sorgente: src, Destinazione: filepath.Join(dir, "gestrip.db"), Report: filepath.Join(dir, "report.json"),
			Sha256: shaDi(t, src), Sintetico: true, Orologio: clock.NuovoFinto(istanteImport)}
		if es := importa(t, o); es.Verdetto != Riconciliato {
			t.Fatalf("verdetto %s", es.Verdetto)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "report.json", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
	})
}

// inventario: OPS-01
// I rifiuti delle cartelle media arrivano prima di aprire la sorgente e non creano niente: la
// cartella media e SenzaMedia insieme; una cartella media della sorgente che non c'è, che è un
// file o un link; la cartella media della destinazione che c'è già (anche vuota, anche come
// link); la destinazione o il report dentro la cartella media della sorgente; una cartella delle
// miniature che non è una cartella.
func TestMediaRifiutiSenzaFile(t *testing.T) {
	casi := []struct {
		nome   string
		cambia func(t *testing.T, dir string, o *Opzioni)
		errore string
	}{
		{"media e senza media", func(t *testing.T, _ string, o *Opzioni) { o.SenzaMedia = true }, "una sola"},
		{"cartella che non c'è", func(t *testing.T, dir string, o *Opzioni) { o.Media = filepath.Join(dir, "manca") }, "cartella media della sorgente"},
		{"un file", func(t *testing.T, dir string, o *Opzioni) { o.Media = o.Sorgente }, "cartella media della sorgente"},
		{"un link", func(t *testing.T, dir string, o *Opzioni) {
			l := filepath.Join(dir, "link-media")
			if err := os.Symlink(o.Media, l); err != nil {
				t.Fatal(err)
			}
			o.Media = l
		}, "cartella media della sorgente"},
		{"media della destinazione vuota", func(t *testing.T, dir string, o *Opzioni) {
			if err := os.Mkdir(filepath.Join(dir, "media"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "esiste già"},
		{"media della destinazione link pendente", func(t *testing.T, dir string, o *Opzioni) {
			if err := os.Symlink("altrove", filepath.Join(dir, "media")); err != nil {
				t.Fatal(err)
			}
		}, "esiste già"},
		{"destinazione dentro la cartella media", func(t *testing.T, dir string, o *Opzioni) {
			o.Destinazione = filepath.Join(o.Media, "gestrip.db")
		}, "dentro la cartella media della sorgente"},
		{"destinazione in una sottocartella della cartella media", func(t *testing.T, dir string, o *Opzioni) {
			sotto := filepath.Join(o.Media, "sotto")
			if err := os.Mkdir(sotto, 0o700); err != nil {
				t.Fatal(err)
			}
			o.Destinazione = filepath.Join(sotto, "gestrip.db")
		}, "dentro la cartella media della sorgente"},
		{"report dentro la cartella media", func(t *testing.T, dir string, o *Opzioni) {
			o.Report = filepath.Join(o.Media, "report.json")
		}, "dentro la cartella media della sorgente"},
		{"miniature che non sono una cartella", func(t *testing.T, dir string, o *Opzioni) { o.Miniature = o.Sorgente }, "miniature"},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			media := o.Media
			c.cambia(t, dir, &o)
			prima, primaMedia := fileDi(t, dir), fileDi(t, media)
			es, err := Importa(context.Background(), o)
			if err == nil || !strings.Contains(err.Error(), c.errore) {
				t.Errorf("errore %v, atteso con %q", err, c.errore)
			}
			if err != nil && strings.Contains(err.Error(), media) {
				t.Errorf("l'errore nomina la cartella media della sorgente: %v", err)
			}
			if es.Report != "" {
				t.Errorf("esito %+v", es)
			}
			if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
				t.Errorf("file prima %v, dopo %v", prima, dopo)
			}
			if dopo := fileDi(t, media); !reflect.DeepEqual(primaMedia, dopo) {
				t.Errorf("cartella media prima %v, dopo %v", primaMedia, dopo)
			}
		})
	}
}

// Le miniature si contano e non si importano: il report dice quanti file e quanti byte ci sono
// nella cartella delle miniature (solo i file regolari), e la destinazione non ne ha nessuna.
// Senza la cartella il report non le conta.
func TestMiniatureContateNonCopiate(t *testing.T) {
	_, o := preparaImport(t, nil)
	importa(t, o)
	r, _ := leggiReport(t, o.Report)
	if r.Media == nil || r.Media.Miniature != nil {
		t.Errorf("senza cartella: %+v", r.Media)
	}
	dir, o := preparaImport(t, nil)
	mini := t.TempDir()
	for i, b := range []string{"RIFF1", "RIFF22", "RIFF333"} {
		if err := os.WriteFile(filepath.Join(mini, chiaveMedia(i+1)+".webp"), []byte(b), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(mini, "sotto"), 0o700); err != nil {
		t.Fatal(err)
	}
	o.Miniature = mini
	if es := importa(t, o); es.Verdetto != Riconciliato {
		t.Fatalf("verdetto %s", es.Verdetto)
	}
	r, _ = leggiReport(t, o.Report)
	if r.Media == nil || !reflect.DeepEqual(r.Media.Miniature, &ConteggioFile{File: 3, Byte: 18}) {
		t.Errorf("miniature %+v", r.Media)
	}
	for n := range alberoMedia(t, CartellaMediaDi(o.Destinazione)) {
		if strings.HasSuffix(n, ".webp") {
			t.Errorf("miniatura copiata: %s", n)
		}
	}
	if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "media", "report.json", "standalone.db"}) {
		t.Errorf("nella cartella: %v", got)
	}
}

// inventario: OPS-07
// Attivazione dei media insieme al database: una cartella media della destinazione comparsa
// durante l'import (anche vuota) non si sovrascrive e niente si attiva; un'interruzione durante la
// copia dei media non lascia niente, nemmeno i media copiati fin lì.
func TestMediaAttivazione(t *testing.T) {
	t.Run("cartella media creata durante", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		md := filepath.Join(dir, "media")
		o.DopoTabella = func(_ context.Context, tab string) {
			if tab == "buste" {
				if err := os.Mkdir(md, 0o755); err != nil {
					panic(err)
				}
			}
		}
		_, err := Importa(context.Background(), o)
		if err == nil {
			t.Error("nessun errore")
		} else if strings.Contains(err.Error(), ".gestrip-import-") {
			t.Errorf("l'errore nomina la cartella di lavoro: %v", err)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"media", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
		if got := fileDi(t, md); len(got) != 0 {
			t.Errorf("nella cartella media altrui: %v", got)
		}
	})
	t.Run("interruzione durante la copia", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		ctx, ferma := context.WithCancel(context.Background())
		defer ferma()
		visti := 0
		// Il settimo file è il primo dei due della coda delle cancellazioni: lo stop deve valere
		// anche tra due file della stessa tabella, non solo alla query della tabella dopo.
		dopoUnFileMedia = func() {
			if visti++; visti == 7 {
				ferma()
			}
		}
		t.Cleanup(func() { dopoUnFileMedia = nil })
		_, err := Importa(ctx, o)
		if !errors.Is(err, ErrInterrotto) {
			t.Errorf("errore %v, attesa l'interruzione", err)
		}
		if visti != 7 {
			t.Errorf("file visti %d", visti)
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
	})
	t.Run("database non attivato: media tolti", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		prima := alberoMedia(t, o.Media)
		dopoLaCartellaMedia = func() { scriviFile(t, o.Destinazione) }
		t.Cleanup(func() { dopoLaCartellaMedia = nil })
		if _, err := Importa(context.Background(), o); err == nil {
			t.Error("nessun errore")
		}
		if got := fileDi(t, dir); !reflect.DeepEqual(got, []string{"gestrip.db", "standalone.db"}) {
			t.Errorf("nella cartella: %v", got)
		}
		if dopo := alberoMedia(t, o.Media); !reflect.DeepEqual(prima, dopo) {
			t.Error("la cartella media della sorgente è cambiata")
		}
	})
}

// ColonneMedia è esattamente l'elenco scritto qui, e ogni colonna dello schema dello standalone
// (testdata/standalone-go-baseline.sql) che per il nome può riguardare un file (chiavi, percorsi,
// nomi di file, impronte, dimensioni) è in ColonneMedia o tra le escluse con il perché.
func TestColonneMediaComplete(t *testing.T) {
	attese := []ColonnaMedia{
		{Tabella: "branding_assets", Chiave: "storage_key", Sha256: "sha256", Byte: "byte_size"},
		{Tabella: "repairs", Chiave: "signature_storage_key", Byte: "signature_byte_size"},
		{Tabella: "repair_photos", Chiave: "storage_key", Sha256: "sha256", Byte: "byte_size"},
		{Tabella: "delivery_signature_events", Chiave: "storage_key"},
		{Tabella: "sale_photos", Chiave: "storage_key", Sha256: "sha256", Byte: "byte_size"},
		{Tabella: "sale_revision_photos", Chiave: "storage_key", Sha256: "sha256", Byte: "byte_size"},
		{Tabella: "historical_documents", Chiave: "storage_key", Sha256: "sha256", Byte: "byte_size"},
		{Tabella: "media_deletion_jobs", Chiave: "storage_key", Facoltativo: true},
	}
	if !reflect.DeepEqual(ColonneMedia, attese) {
		t.Errorf("ColonneMedia %+v,\nattese %+v", ColonneMedia, attese)
	}
	escluse := map[string]string{
		"branding_assets.original_filename":      "nome mostrato, non un percorso",
		"repair_photos.original_filename":        "nome mostrato, non un percorso",
		"sale_photos.original_filename":          "nome mostrato, non un percorso",
		"sale_revision_photos.original_filename": "nome mostrato, non un percorso",
		"historical_documents.source_filename":   "nome del PDF nel Flask, storia: il file è storage_key",
		"repairs.signature_at":                   "istante della firma",
		"repairs.signature_collected_by":         "chi ha ritirato",
		"legacy_import_captures.source_size":     "la cattura del Flask, non un file della cartella",
		"legacy_import_captures.source_sha256":   "la cattura del Flask, non un file della cartella",
		"legacy_import_captures.capture_sha256":  "la cattura del Flask, non un file della cartella",
		"users.password_hash":                    "hash della password",
		"password_resets.token_hash":             "hash di un token",
		"user_invitations.token_hash":            "hash di un token",
		"telegram_link_tokens.token_hash":        "hash di un token",
		"repairs.public_token_hash":              "hash di un token",
		"repairs.legacy_public_code_hash":        "hash del codice Flask",
		"sale_revisions.public_token_hash":       "hash di un token",
		"settings.key":                           "nome di un'impostazione",
		"emergency_entries.document_year":        "numero di documento fiscale, non un file",
		"sales.document_year":                    "numero di documento fiscale, non un file",
		"sales.document_number":                  "numero di documento fiscale, non un file",
		"sale_revisions.document_year":           "numero di documento fiscale, non un file",
		"sale_revisions.document_number":         "numero di documento fiscale, non un file",
	}
	nominate := map[string]bool{}
	for _, cm := range ColonneMedia {
		for _, c := range []string{cm.Chiave, cm.Sha256, cm.Byte} {
			if c != "" {
				nominate[cm.Tabella+"."+c] = true
			}
		}
	}
	c, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }() // in memoria
	c.SetMaxOpenConns(1)
	if _, err := c.Exec(ddlStandalone); err != nil {
		t.Fatal(err)
	}
	rows, err := c.Query(`SELECT m.name, p.name FROM sqlite_schema m, pragma_table_info(m.name) p WHERE m.type = 'table' ORDER BY m.name, p.cid`)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?i)storage|key|path|file|sha256|media|photo|foto|firma|signature|logo|document|digest|hash|size`)
	nelloSchema := map[string]bool{}
	for rows.Next() {
		var tab, col string
		if err := rows.Scan(&tab, &col); err != nil {
			t.Fatal(err)
		}
		k := tab + "." + col
		nelloSchema[k] = true
		if re.MatchString(col) && !nominate[k] && escluse[k] == "" {
			t.Errorf("%s può nominare un file: né in ColonneMedia né tra le escluse", k)
		}
	}
	chiudiRighe(t, rows)
	for k := range nominate {
		if !nelloSchema[k] {
			t.Errorf("%s di ColonneMedia non è nello schema dello standalone", k)
		}
	}
	for k := range escluse {
		if !nelloSchema[k] {
			t.Errorf("esclusa %s non è nello schema dello standalone", k)
		}
	}
}

// cartellaDiLavoro trova la cartella media dentro la cartella di lavoro dell'import in dir.
func cartellaDiLavoro(t *testing.T, dir string) string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, ".gestrip-import-*", "media"))
	if err != nil || len(m) != 1 {
		t.Fatalf("cartella media di lavoro: %v %v", m, err)
	}
	return m[0]
}

// byteDi somma i byte dei file in dir (spazio occupato davvero, per i file sparsi).
func byteDi(t *testing.T, dir string) (n int64, nomi []string) {
	t.Helper()
	voci, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range voci {
		fi, err := v.Info()
		if err != nil {
			t.Fatal(err)
		}
		n += fi.Size()
		nomi = append(nomi, v.Name())
	}
	return n, nomi
}

// stessaLunghezza dà byte diversi da quelli del file media n, della stessa lunghezza.
func stessaLunghezza(n int) []byte { return bytes.Repeat([]byte{'z'}, len(contenutoMedia(n))) }

// M3: la dimensione si guarda prima di copiare. Un file vuoto, uno oltre 16 MiB (il limite di
// MediaStore.read_object dello standalone) o uno di dimensione diversa da quella della riga è
// una differenza bloccante e nella cartella di lavoro non si scrive nemmeno un byte di quel file.
func TestMediaDimensionePrimaDiCopiare(t *testing.T) {
	casi := []struct {
		nome    string
		n       int
		ritocca func(*sql.DB)
		file    func(t *testing.T, p string)
		voce    Voce
	}{
		{"oltre 16 MiB", 2, nil, func(t *testing.T, p string) {
			f, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := errors.Join(f.Truncate(16<<20+1), f.Close()); err != nil {
				t.Fatal(err)
			}
		}, Voce{Codice: "media/troppo_grande", Tabella: "repair_photos", Id: ptr(1), Colonna: "storage_key"}},
		{"vuoto", 4, nil, func(t *testing.T, p string) {
			if err := os.WriteFile(p, nil, 0o400); err != nil {
				t.Fatal(err)
			}
		}, Voce{Codice: "media/vuoto", Tabella: "sale_revision_photos", Id: ptr(1), Colonna: "storage_key"}},
		{"dimensione diversa dalla riga", 3, func(c *sql.DB) { esegui(c, `UPDATE sale_photos SET byte_size = byte_size + 1`) }, nil,
			Voce{Codice: "media/byte_diversi", Tabella: "sale_photos", Id: ptr(1), Colonna: "byte_size"}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, c.ritocca)
			if c.file != nil {
				rimpiazza(t, o.Media, c.n, func(p string) { c.file(t, p) })
			}
			var massimo int64
			dopoUnFileMedia = func() {
				n, nomi := byteDi(t, cartellaDiLavoro(t, dir))
				massimo = max(massimo, n)
				if slices.Contains(nomi, chiaveMedia(c.n)) {
					t.Errorf("il file %d è stato copiato", c.n)
				}
			}
			t.Cleanup(func() { dopoUnFileMedia = nil })
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !slices.ContainsFunc(r.Differenze, func(d Voce) bool { return reflect.DeepEqual(d, c.voce) }) {
				b, _ := json.Marshal(r.Differenze)
				t.Errorf("differenze %s, manca %+v", b, c.voce)
			}
			if massimo > lunghezza(mediaSintetici...) {
				t.Errorf("nella cartella di lavoro %d byte, al più %d", massimo, lunghezza(mediaSintetici...))
			}
		})
	}
}

// B2: un file scambiato fra Lstat e l'apertura (un altro file regolare con gli stessi byte) o
// cresciuto mentre si apre non si copia: è una differenza bloccante «non regolare» (non un hash
// diverso: il file è cambiato sotto l'import).
func TestMediaScambiatoDuranteLApertura(t *testing.T) {
	for _, c := range []struct {
		nome   string
		cambia func(p string)
	}{
		{"scambiato", func(p string) {
			nuovo := p + ".nuovo"
			if err := os.WriteFile(nuovo, contenutoMedia(2), 0o400); err != nil {
				panic(err)
			}
			if err := os.Rename(nuovo, p); err != nil {
				panic(err)
			}
		}},
		{"cresciuto", func(p string) {
			if err := os.Chmod(p, 0o600); err != nil {
				panic(err)
			}
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				panic(err)
			}
			if _, err := f.Write([]byte("in più")); err != nil {
				panic(err)
			}
			if err := f.Close(); err != nil {
				panic(err)
			}
		}},
	} {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, nil)
			traLstatEOpen = func(p string) {
				if filepath.Base(p) == chiaveMedia(2) {
					c.cambia(p)
				}
			}
			t.Cleanup(func() { traLstatEOpen = nil })
			r := controllaBloccato(t, dir, o, importa(t, o))
			v := Voce{Codice: "media/non_regolare", Tabella: "repair_photos", Id: ptr(1), Colonna: "storage_key"}
			if !slices.ContainsFunc(r.Differenze, func(d Voce) bool { return reflect.DeepEqual(d, v) }) {
				t.Errorf("differenze %+v", r.Differenze)
			}
		})
	}
}

// M2: un errore di scrittura nella cartella media di lavoro (qui non scrivibile dopo il primo
// file) ferma l'import, e né l'errore né il messaggio sulla pulizia della cartella di lavoro
// nominano una chiave media o un file dentro la cartella media di lavoro.
func TestMediaErroriSenzaChiavi(t *testing.T) {
	dir, o := preparaImport(t, nil)
	var lavoro string
	dopoUnFileMedia = func() {
		if lavoro == "" {
			lavoro = cartellaDiLavoro(t, dir)
			if err := os.Chmod(lavoro, 0o500); err != nil {
				panic(err)
			}
		}
	}
	t.Cleanup(func() {
		dopoUnFileMedia = nil
		if lavoro != "" {
			_ = os.Chmod(lavoro, 0o700) // per la pulizia di TempDir
		}
	})
	_, err := Importa(context.Background(), o)
	if err == nil {
		t.Fatal("nessun errore")
	}
	for _, n := range append(mediaSintetici, 8) {
		if strings.Contains(err.Error(), chiaveMedia(n)) {
			t.Errorf("l'errore nomina la chiave del file %d: %v", n, err)
		}
	}
	if strings.Contains(err.Error(), string(filepath.Separator)+CartellaMedia+string(filepath.Separator)) {
		t.Errorf("l'errore nomina un file dentro la cartella media di lavoro: %v", err)
	}
	if strings.Contains(err.Error(), o.Media) {
		t.Errorf("l'errore nomina la cartella media della sorgente: %v", err)
	}
}

// M1a: i resti di un import interrotto si guardano prima della cartella media; una cartella
// media senza il database accanto è detta come il probabile resto di un import interrotto.
func TestMediaRestiDiUnImportInterrotto(t *testing.T) {
	t.Run("resti e media", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		for _, d := range []string{".gestrip-import-123", "media"} {
			if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		_, err := Importa(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "resti di un import interrotto") {
			t.Errorf("errore %v", err)
		}
	})
	t.Run("solo media", func(t *testing.T) {
		dir, o := preparaImport(t, nil)
		if err := os.Mkdir(filepath.Join(dir, "media"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Importa(context.Background(), o)
		if err == nil || !strings.Contains(err.Error(), "esiste già") || !strings.Contains(err.Error(), "import interrotto") ||
			!strings.Contains(err.Error(), "a mano") {
			t.Errorf("errore %v", err)
		}
	})
	t.Run("RestiDiImport", func(t *testing.T) {
		dir := t.TempDir()
		db := filepath.Join(dir, "gestrip.db")
		if r, err := RestiDiImport(db); err != nil || len(r) != 0 {
			t.Errorf("cartella vuota: %v %v", r, err)
		}
		for _, d := range []string{".gestrip-import-9", "media"} {
			if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if r, err := RestiDiImport(db); err != nil || !reflect.DeepEqual(r, []string{filepath.Join(dir, ".gestrip-import-9"), filepath.Join(dir, "media")}) {
			t.Errorf("resti %v %v", r, err)
		}
		scriviFile(t, db)
		if r, err := RestiDiImport(db); err != nil || !reflect.DeepEqual(r, []string{filepath.Join(dir, ".gestrip-import-9")}) {
			t.Errorf("con il database: %v %v", r, err)
		}
	})
}

// 6: -senza-media per ora vale solo con -sintetico: con -autorizzato rifiuta prima di aprire la
// sorgente.
func TestSenzaMediaSoloSintetico(t *testing.T) {
	dir, o := preparaImport(t, nil)
	o.Media, o.SenzaMedia = "", true
	o.Sintetico, o.Autorizzato, o.Riferimento = false, true, "prova"
	prima := fileDi(t, dir)
	if _, err := Importa(context.Background(), o); err == nil || !strings.Contains(err.Error(), "-senza-media") || !strings.Contains(err.Error(), "-sintetico") {
		t.Errorf("errore %v", err)
	}
	if dopo := fileDi(t, dir); !reflect.DeepEqual(prima, dopo) {
		t.Errorf("file prima %v, dopo %v", prima, dopo)
	}
}

func scriviByte(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.WriteFile(p, b, 0o400); err != nil {
		t.Fatal(err)
	}
}
