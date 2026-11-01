package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/importer"
)

// dockerFinto sta al posto di docker, git e del controllo del disco in cmdSu: fa ciò che
// farebbero i container (file nella cartella dell'istanza) e registra i container avviati.
type dockerFinto struct {
	n            nomi
	outImportSA  string
	outDecifra   string
	outVendita   string // in suDiProva: la riga di una vendita riuscita, con la chiave della foto
	fotoAssente  bool   // il «container» della vendita non salva la foto nei media
	nomeFoto     string // vuoto: la foto si salva con il nome della sua impronta
	errVendita   error
	outEmergenza string // in suDiProva: la riga di un registro d'emergenza riuscito
	errEmergenza error
	outModifiche string // in suDiProva: la riga del passo delle modifiche riuscito
	errModifiche error
	outStati     string // in suDiProva: la riga del passo degli stati riuscito
	errStati     error
	fotoDoppia   bool // il «container» della vendita salva anche un secondo file nei media
	chiaroLink   bool // il «container» della decifratura piazza un collegamento al posto del file
	errImportGo  error
	guastaReport func(*importer.Report) // nil: report riconciliato e coerente
	senzaReport  bool
	passi        []string // suffissi dei container, in ordine
	staccati     []string // container avviati con -d (restano accesi)
}

// fotoFinta è ciò che il «container» della vendita salva nei media, con il suo nome per impronta.
const (
	fotoFinta       = "foto normalizzata"
	chiaveFotoFinta = "5979b8b780d3cae6e50100529998c9e1cf0f630ea724b873a12829bc8b84b7f1" // sha256 di fotoFinta
)

const okImportSA = "GR_IMPORT:{\"mode\":\"synthetic\",\"verdict\":\"reconciled\",\"sha256\":\"ab\"}\n"

func (f *dockerFinto) scrivi(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *dockerFinto) installa(t *testing.T) {
	t.Helper()
	vecchi := []any{dockerFa, dockerLegge, dockerProva, dockerTiene, daTag, spazioDisco}
	t.Cleanup(func() {
		dockerFa = vecchi[0].(func(...string) error)
		dockerLegge = vecchi[1].(func(...string) (string, error))
		dockerProva = vecchi[2].(func(...string) error)
		dockerTiene = vecchi[3].(func(...string) (string, error))
		daTag = vecchi[4].(func(string) ([]byte, error))
		spazioDisco = vecchi[5].(func(string) error)
	})
	p := catenaPer(f.n)
	esegui := func(a ...string) (string, error) {
		if len(a) == 0 || a[0] != "run" {
			return "", nil // network create
		}
		nome := strings.TrimPrefix(valoreDopo(a, "--name"), f.n.base+"-")
		f.passi = append(f.passi, nome)
		if indice(a, "-d") >= 0 {
			f.staccati = append(f.staccati, nome)
		}
		switch nome {
		case "boot":
			f.scrivi(t, filepath.Join(f.n.dir, "data", "gestrip.db"), strings.Repeat("\x9a", 64))
			f.scrivi(t, filepath.Join(f.n.dir, "media", "f1"), "aaa")
			f.scrivi(t, filepath.Join(f.n.dir, "media", "f2"), "bbbbbbb")
		case "cattura":
			f.scrivi(t, filepath.Join(p.flask, "riparazioni.db"), "SQLite format 3\x00flask")
		case "importsa":
			return f.outImportSA, nil
		case passoVendita:
			if src, ro := montaggio(a, "/catena/foto.jpg"); src != p.foto || !ro {
				t.Errorf("foto montata da %q ro=%v", src, ro)
			} else if b, err := os.ReadFile(src); err != nil || len(b) == 0 {
				t.Errorf("foto sintetica non scritta: %v", err)
			}
			if nome := f.nomeFoto; !f.fotoAssente {
				if nome == "" {
					nome = chiaveFotoFinta
				}
				f.scrivi(t, filepath.Join(f.n.dir, "media", nome), fotoFinta)
			}
			if f.fotoDoppia {
				f.scrivi(t, filepath.Join(f.n.dir, "media", "altro"), "x")
			}
			return f.outVendita, f.errVendita
		case passoEmergenza:
			if src, _ := montaggio(a, "/catena/foto.jpg"); src != "" {
				t.Errorf("il registro d'emergenza monta la foto della vendita: %v", a)
			}
			return f.outEmergenza, f.errEmergenza
		case passoModifiche:
			if src, _ := montaggio(a, "/catena/foto.jpg"); src != "" {
				t.Errorf("il passo delle modifiche monta la foto della vendita: %v", a)
			}
			return f.outModifiche, f.errModifiche
		case passoStati:
			if src, _ := montaggio(a, "/catena/foto.jpg"); src != "" {
				t.Errorf("il passo degli stati monta la foto della vendita: %v", a)
			}
			return f.outStati, f.errStati
		case "decifra":
			dst := filepath.Join(p.chiaro, nomeChiaro)
			if f.chiaroLink {
				vero := filepath.Join(f.n.dir, "altrove.db")
				f.scrivi(t, vero, "SQLite format 3\x00altrove")
				if err := os.Symlink(vero, dst); err != nil {
					t.Fatal(err)
				}
			} else {
				// Come sqlite nel container (umask 022): 0644, il banco lo rende privato.
				f.scrivi(t, dst, "SQLite format 3\x00decifrato")
				if err := os.Chmod(dst, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			return f.outDecifra, nil
		case "importgo":
			if !f.senzaReport {
				sha, dim, err := impronta(filepath.Join(p.chiaro, nomeChiaro))
				if err != nil {
					t.Fatal(err)
				}
				r := reportDiProva(sha)
				r.Sorgente.Byte = dim
				r.Tabelle[0].Lette, r.Tabelle[0].Scritte["buste"] = 15, 15 // le buste della cattura e dei passi delle modifiche e degli stati
				r.Tabelle = append(r.Tabelle, importer.TabellaReport{Standalone: "ready_lots", Lette: 1, Scritte: map[string]int64{"lotti_avviso": 1}})
				r.Media.FileCopiati, r.Media.ByteCopiati = 3, int64(10+len(fotoFinta))
				if f.guastaReport != nil {
					f.guastaReport(&r)
				}
				b, _ := json.Marshal(r)
				f.scrivi(t, filepath.Join(f.n.dir, "esito", nomeReportGo), string(b))
			}
			return "", f.errImportGo
		case "caddy":
			f.scrivi(t, filepath.Join(f.n.dir, "caddy-data/caddy/pki/authorities/local/root.crt"), "ca")
		}
		return "", nil
	}
	dockerFa = func(a ...string) error { _, err := esegui(a...); return err }
	dockerTiene = esegui
	dockerLegge = func(...string) (string, error) { return "sha256:finto", nil }
	dockerProva = func(a ...string) error {
		if len(a) > 0 && a[0] == "network" {
			return errors.New("no such network")
		}
		return nil
	}
	daTag = func(string) ([]byte, error) { return []byte("{}\n"), nil }
	spazioDisco = func(string) error { return nil }
}

func suDiProva(t *testing.T) (*dockerFinto, string) {
	t.Helper()
	run := t.TempDir()
	t.Setenv("GESTRIP_BANCO_RUN", run)
	n, err := nomiPer("t9")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "gestrip")
	if err := os.WriteFile(bin, []byte("binario"), 0o700); err != nil {
		t.Fatal(err)
	}
	f := &dockerFinto{n: n, outImportSA: okImportSA, outDecifra: "GR_DECIFRATO\n", outVendita: rigaVendita(chiaveFotoFinta, conteggiUno),
		outEmergenza: rigaEmergenza(esitoEmergenzaBuono), outModifiche: rigaModifiche(esitoModificheBuono),
		outStati: rigaStati(esitoStatiBuono)}
	f.installa(t)
	return f, bin
}

func argSu(bin string, catena bool) []string {
	a := []string{"-nome", "t9", "-immagine", "img:1", "-gestrip", bin}
	if catena {
		a = append(a, "-dati", datiSinteticiImport)
	}
	return a
}

func esiste(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestSuConLaCatena(t *testing.T) {
	f, bin := suDiProva(t)
	if err := cmdSu(argSu(bin, true)); err != nil {
		t.Fatal(err)
	}
	// La catena finisce tutta (anche l'import Go) prima che parta un container che resta acceso.
	voluti := []string{"boot", "dipendente", "cattura", "importsa", "vendita", "emergenza", "modifiche", "stati", "decifra", "importgo", "app", "caddy", "go"}
	if strings.Join(f.passi, " ") != strings.Join(voluti, " ") {
		t.Errorf("passi = %v, voluti %v", f.passi, voluti)
	}
	if strings.Join(f.staccati, " ") != "app caddy go" {
		t.Errorf("staccati = %v", f.staccati)
	}
	// Niente semina: il database del Go viene solo dall'import (qui finto, quindi assente).
	if esiste(filepath.Join(f.n.dir, "go-data", "gestrip.db")) {
		t.Error("con la catena il Go è stato seminato")
	}
	// Riconciliato: le copie in chiaro e cifrate della catena non restano.
	p := catenaPer(f.n)
	for _, d := range []string{p.cifrato, p.chiaro, p.media, p.miniature} {
		if esiste(d) {
			t.Errorf("copia rimasta dopo un import riconciliato: %s", d)
		}
	}
	b, err := os.ReadFile(filepath.Join(f.n.dir, "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rt struct {
		Dati     string           `json:"dati"`
		ImportGo *riassuntoImport `json:"import_go"`
	}
	if err := json.Unmarshal(b, &rt); err != nil || rt.Dati != datiSinteticiImport || rt.ImportGo == nil ||
		rt.ImportGo.Verdetto != importer.Riconciliato || rt.ImportGo.FileMedia != 3 || rt.ImportGo.MediaVendita != 1 ||
		rt.ImportGo.ImpronteTesto["sale_revision_photos"] != 1 || rt.ImportGo.IstantiSenzaZona["emergency_entries.inserted_at"] != 2 ||
		rt.ImportGo.Emergenza != (riassuntoEmergenza{Righe: 2, EventiChiusi: 1, BusteScansionate: 1}) {
		t.Errorf("runtime.json = %s (%v)", b, err)
	}
}

func TestSuSenzaCatena(t *testing.T) {
	f, bin := suDiProva(t)
	if err := cmdSu(argSu(bin, false)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.passi, " ") != "boot dipendente app caddy go" {
		t.Errorf("passi = %v", f.passi)
	}
	if !esiste(filepath.Join(f.n.dir, "go-data", "gestrip.db")) {
		t.Error("senza la catena il Go non è stato seminato")
	}
	if esiste(catenaPer(f.n).dir) {
		t.Error("senza la catena c'è la cartella della catena")
	}
}

// Ogni controllo che ferma la catena la ferma davvero: cmdSu dà errore e nessun container resta
// acceso (né lo standalone, né Caddy, né il Go). Le copie restano per la diagnosi.
func TestSuLaCatenaSiFerma(t *testing.T) {
	casi := map[string]struct {
		guasta     func(*dockerFinto)
		ultimo     string
		tieneCopie bool
	}{
		"legacy import senza GR_IMPORT": {func(f *dockerFinto) { f.outImportSA = "fatto\n" }, "importsa", false},
		"legacy import bloccato": {func(f *dockerFinto) {
			f.outImportSA = "GR_IMPORT:{\"mode\":\"synthetic\",\"verdict\":\"blocked\"}\n"
		}, "importsa", false},
		"vendita senza GR_VENDITA":              {func(f *dockerFinto) { f.outVendita = "fatto\n" }, "vendita", false},
		"vendita con due file in più nei media": {func(f *dockerFinto) { f.fotoDoppia = true }, "vendita", false},
		"vendita con errore": {func(f *dockerFinto) {
			f.outVendita = "GR_VENDITA:{\"esito\":\"errore\",\"motivo\":\"invalid_image\"}\n"
			f.errVendita = errors.New("exit status 1")
		}, "vendita", false},
		"foto della vendita non nei media": {func(f *dockerFinto) { f.fotoAssente = true }, "vendita", false},
		"vendita riuscita ma container con errore": {func(f *dockerFinto) {
			f.errVendita = errors.New("exit status 1")
		}, "vendita", false},
		"foto della vendita con un'altra impronta": {func(f *dockerFinto) {
			f.outVendita = rigaVendita(strings.Repeat("ab", 32), conteggiUno)
			f.nomeFoto = strings.Repeat("ab", 32)
		}, "vendita", false},
		"registro senza GR_EMERGENZA": {func(f *dockerFinto) { f.outEmergenza = "fatto\n" }, "emergenza", false},
		"registro con errore": {func(f *dockerFinto) {
			f.outEmergenza = rigaEmergenza(`{"esito":"errore","motivo":"{:error, :not_found}"}`)
			f.errEmergenza = errors.New("exit status 1")
		}, "emergenza", false},
		"registro riuscito ma container con errore": {func(f *dockerFinto) {
			f.errEmergenza = errors.New("exit status 1")
		}, "emergenza", false},
		"registro con l'evento aperto": {func(f *dockerFinto) {
			f.outEmergenza = rigaEmergenza(strings.Replace(esitoEmergenzaBuono, `"chiuso":true`, `"chiuso":false`, 1))
		}, "emergenza", false},
		"import Go senza l'avviso delle righe del registro": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Avvisi = r.Avvisi[:5] }
		}, "importgo", true},
		"import Go con la busta scansionata due volte": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { *r.Avvisi[8].Righe = 2 }
		}, "importgo", true},
		"modifiche senza GR_MODIFICHE": {func(f *dockerFinto) { f.outModifiche = "fatto\n" }, "modifiche", false},
		"modifiche con errore": {func(f *dockerFinto) {
			f.outModifiche = rigaModifiche(`{"esito":"errore","motivo":"{:error, :invalid_artisan}"}`)
			f.errModifiche = errors.New("exit status 1")
		}, "modifiche", false},
		"modifiche riuscite ma container con errore": {func(f *dockerFinto) {
			f.errModifiche = errors.New("exit status 1")
		}, "modifiche", false},
		"import Go senza le buste modificate": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Tabelle[0].Lette, r.Tabelle[0].Scritte["buste"] = 4, 4 }
		}, "importgo", true},
		"stati senza GR_STATI": {func(f *dockerFinto) { f.outStati = "fatto\n" }, "stati", false},
		"stati con errore": {func(f *dockerFinto) {
			f.outStati = rigaStati(`{"esito":"errore","motivo":"{:error, :not_ready}"}`)
			f.errStati = errors.New("exit status 1")
		}, "stati", false},
		"stati riusciti ma container con errore": {func(f *dockerFinto) {
			f.errStati = errors.New("exit status 1")
		}, "stati", false},
		"import Go senza le buste degli stati": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Tabelle[0].Lette, r.Tabelle[0].Scritte["buste"] = 10, 10 }
		}, "importgo", true},
		"import Go senza il lotto": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Tabelle[len(r.Tabelle)-1].Scritte["lotti_avviso"] = 0 }
		}, "importgo", true},
		"manca GR_DECIFRATO": {func(f *dockerFinto) { f.outDecifra = "fatto\n" }, "decifra", false},
		"import Go senza l'avviso delle foto di revisione": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Avvisi = r.Avvisi[:2] }
		}, "importgo", true},
		"import Go con una vendita in meno": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Tabelle[3].Scritte["vendite"] = 0 }
		}, "importgo", true},
		"decifrato è un collegamento": {func(f *dockerFinto) { f.chiaroLink = true }, "decifra", false},
		"import Go rc≠0 con report riconciliato": {func(f *dockerFinto) {
			f.errImportGo = errors.New("exit status 1")
		}, "importgo", true},
		"import Go bloccato": {func(f *dockerFinto) {
			f.errImportGo = errors.New("exit status 1")
			f.guastaReport = func(r *importer.Report) {
				r.Verdetto = importer.Bloccato
				r.Differenze = []importer.Voce{{Codice: "tipo/blob_atteso", Tabella: "historical_documents"}}
			}
		}, "importgo", true},
		"import Go senza report": {func(f *dockerFinto) { f.senzaReport = true }, "importgo", true},
		"riconciliato ma nessun file media": {func(f *dockerFinto) {
			f.guastaReport = func(r *importer.Report) { r.Media.FileCopiati, r.Media.ByteCopiati = 0, 0 }
		}, "importgo", true},
	}
	// Dove conta, l'errore dice perché.
	messaggi := map[string]string{"vendita senza GR_VENDITA": "0 righe GR_VENDITA",
		"vendita con due file in più nei media": "2 file in più", "vendita con errore": "invalid_image",
		"registro senza GR_EMERGENZA": "0 righe GR_EMERGENZA", "registro con errore": ":not_found",
		"import Go senza l'avviso delle righe del registro": "emergency_entries.inserted_at",
		"modifiche senza GR_MODIFICHE":                      "0 righe GR_MODIFICHE", "modifiche con errore": ":invalid_artisan",
		"import Go senza le buste modificate": "repairs 4 righe lette",
		"stati senza GR_STATI":                "0 righe GR_STATI", "stati con errore": ":not_ready",
		"import Go senza le buste degli stati": "repairs 10 righe lette", "import Go senza il lotto": "lotti_avviso 0 scritte"}
	for nome, c := range casi {
		t.Run(nome, func(t *testing.T) {
			f, bin := suDiProva(t)
			c.guasta(f)
			err := cmdSu(argSu(bin, true))
			if err == nil {
				t.Fatal("cmdSu senza errore")
			}
			if len(f.staccati) > 0 {
				t.Errorf("container rimasti accesi: %v", f.staccati)
			}
			if len(f.passi) == 0 || f.passi[len(f.passi)-1] != c.ultimo {
				t.Errorf("passi = %v, l'ultimo doveva essere %s", f.passi, c.ultimo)
			}
			if m := messaggi[nome]; !strings.Contains(err.Error(), m) {
				t.Errorf("errore %q senza %q", err, m)
			}
			if esiste(filepath.Join(f.n.dir, "runtime.json")) {
				t.Error("runtime.json scritto dopo un errore")
			}
			if c.tieneCopie {
				fi, err := os.Lstat(filepath.Join(catenaPer(f.n).chiaro, nomeChiaro))
				if err != nil {
					t.Error("la copia in chiaro non è rimasta per la diagnosi")
				} else if fi.Mode().Perm() != 0o600 {
					t.Errorf("copia in chiaro con permessi %v", fi.Mode().Perm())
				}
			}
		})
	}
}

// Ogni container della catena ha un nome che giu toglie.
func TestGiuToglieIContainerDellaCatena(t *testing.T) {
	n, p := istanzaDiProva(t)
	da := map[string]bool{}
	for _, s := range suffissiContainer() {
		da[s] = true
	}
	for _, c := range [][]string{comandoCattura(n, "img:1", p), comandoImportStandalone(n, "img:1", nil, p), comandoVendita(n, "img:1", nil, p),
		comandoEmergenza(n, "img:1", nil), comandoDecifra(n, "img:1", p), comandoImportGo(n, "/x/gestrip", p, "", 0, false)} {
		nome := valoreDopo(c, "--name")
		if !strings.HasPrefix(nome, n.base) || !da[strings.TrimPrefix(nome, n.base)] {
			t.Errorf("giu non toglie %s", nome)
		}
	}
	for _, s := range []string{"-boot", "-dipendente", "-app", "-caddy", "-go", "-banco", "-attendi"} {
		if !da[s] {
			t.Errorf("giu non toglie %s", s)
		}
	}
}

// I file scritti dai container si aprono senza seguire collegamenti, e il chmod va sul file.
func TestFileDeiContainerSenzaCollegamenti(t *testing.T) {
	dir := t.TempDir()
	vero := filepath.Join(dir, "vero.db")
	if err := os.WriteFile(vero, []byte("SQLite format 3\x00dati"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.db")
	if err := os.Symlink(vero, link); err != nil {
		t.Fatal(err)
	}
	if _, err := inChiaro(link); err == nil {
		t.Error("inChiaro segue un collegamento")
	}
	if _, _, err := impronta(link); err == nil {
		t.Error("impronta segue un collegamento")
	}
	if err := rendiPrivato(link); err == nil {
		t.Error("rendiPrivato segue un collegamento")
	}
	if fi, err := os.Stat(vero); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("il bersaglio del collegamento è cambiato: %v %v", fi, err)
	}
	if err := rendiPrivato(vero); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(vero); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("permessi dopo rendiPrivato: %v %v", fi, err)
	}
	if ok, err := inChiaro(vero); err != nil || !ok {
		t.Errorf("inChiaro sul file vero: %v %v", ok, err)
	}
	if _, err := leggiPiccolo(link, 1<<20); err == nil {
		t.Error("leggiPiccolo segue un collegamento")
	}
	if _, err := leggiPiccolo(vero, 4); err == nil {
		t.Error("leggiPiccolo oltre il limite")
	}
}

// giu toglie ogni container che un'istanza può avere, catena compresa, poi la rete e la cartella.
func TestGiuToglieTutto(t *testing.T) {
	n, p := istanzaDiProva(t)
	if err := os.MkdirAll(p.chiaro, 0o700); err != nil {
		t.Fatal(err)
	}
	vecchi, vecchiL := dockerCombina, dockerLegge
	t.Cleanup(func() { dockerCombina, dockerLegge = vecchi, vecchiL })
	var tolti []string
	dockerCombina = func(a ...string) ([]byte, error) {
		if len(a) > 3 && a[0] == "rm" {
			tolti = append(tolti, strings.TrimPrefix(a[3], n.base))
		}
		if len(a) > 2 && a[0] == "network" {
			tolti = append(tolti, "rete "+a[2])
		}
		return nil, nil
	}
	dockerLegge = func(...string) (string, error) { return "", nil }
	if err := cmdGiu([]string{"-nome", "t3"}); err != nil {
		t.Fatal(err)
	}
	voluti := append(suffissiContainer(), "rete "+n.rete)
	if strings.Join(tolti, " ") != strings.Join(voluti, " ") {
		t.Errorf("tolti %v, voluti %v", tolti, voluti)
	}
	for _, s := range []string{"-" + passoCattura, "-" + passoImportSA, "-" + passoVendita, "-" + passoEmergenza, "-" + passoDecifra, "-" + passoImportGo} {
		if indice(tolti, s) < 0 {
			t.Errorf("giu non toglie %s", s)
		}
	}
	if esiste(n.dir) {
		t.Error("cartella dell'istanza rimasta")
	}
}
