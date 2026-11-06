package printing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// I test non lanciano mai i comandi veri di CUPS (su pc-fisso ci sono code vere): ogni CUPS dei test
// punta a programmi finti scritti in una cartella temporanea, che registrano argomenti, ambiente e
// standard input e rispondono come CUPS (uscite di lp.c, lpstat.c e lpoptions.c in inglese).

// comandiFinti scrive i programmi finti (sh) nella cartella del test e dà il CUPS che li usa. Ogni
// script dato è il corpo dopo la riga #!/bin/sh; quelli vuoti non si scrivono (il comando manca).
func comandiFinti(t *testing.T, lp, lpstat, lpoptions string) (*CUPS, string) {
	t.Helper()
	dir := t.TempDir()
	for nome, corpo := range map[string]string{"lp": lp, "lpstat": lpstat, "lpoptions": lpoptions} {
		if corpo == "" {
			continue
		}
		testo := "#!/bin/sh\n" +
			`printf '%s\n' "$@" > "` + dir + `/` + nome + `.argomenti"` + "\n" +
			`env > "` + dir + `/` + nome + `.ambiente"` + "\n" + corpo + "\n"
		if err := os.WriteFile(filepath.Join(dir, nome), []byte(testo), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := NuovoCUPS(dir)
	for _, p := range []string{c.Lp, c.Lpstat, c.Lpoptions} {
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			t.Fatalf("comando fuori dalla cartella finta: %s", p)
		}
	}
	return c, dir
}

func letto(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Il percorso dei comandi veri è assoluto e fisso: il PATH non lo sceglie. Nessun test lo lancia.
func TestComandiVeriAssoluti(t *testing.T) {
	c := NuovoCUPS("")
	if c.Lp != "/usr/bin/lp" || c.Lpstat != "/usr/bin/lpstat" || c.Lpoptions != "/usr/bin/lpoptions" ||
		c.TempoStampa != 30*time.Second || c.TempoElenco != 10*time.Second {
		t.Errorf("%+v", c)
	}
}

// Un lavoro: un solo lp, gli argomenti separati (coda, titolo, PageSize dai mm, opzioni), il PDF sullo
// standard input, LC_ALL=C e nient'altro nell'ambiente; l'id dalla risposta in inglese.
func TestStampaConLp(t *testing.T) {
	c2, dir2 := comandiFinti(t, `cat > "$(dirname "$0")/stdin.pdf"; echo "request id is Etichette_QR-42 (0 file(s))"`, "", "")
	t.Setenv("LANG", "it_IT.UTF-8")
	t.Setenv("HOME", "/home/qualcuno")
	pdf := []byte("%PDF-1.4 finto\n\x00\xff")
	id, err := c2.Print(context.Background(), "Etichette_QR", pdf, Options{LarghezzaMM: 57, AltezzaMM: 32,
		Extra: []Opzione{{"MediaTracking", "Gap"}, {"Darkness", "8"}}, Titolo: "etichette-qr-2026-0001-0050.pdf"})
	if err != nil || id != "Etichette_QR-42" {
		t.Fatalf("%q %v", id, err)
	}
	argomenti := strings.Split(strings.TrimSuffix(letto(t, filepath.Join(dir2, "lp.argomenti")), "\n"), "\n")
	atteso := []string{"-d", "Etichette_QR", "-t", "etichette-qr-2026-0001-0050.pdf", "-o", "PageSize=Custom.57x32mm",
		"-o", "MediaTracking=Gap", "-o", "Darkness=8"}
	if !reflect.DeepEqual(argomenti, atteso) {
		t.Errorf("argomenti %q", argomenti)
	}
	if letto(t, filepath.Join(dir2, "stdin.pdf")) != string(pdf) {
		t.Error("il PDF non è arrivato intero sullo standard input")
	}
	amb := letto(t, filepath.Join(dir2, "lp.ambiente"))
	for _, v := range []string{"LC_ALL=C\n", "LANG=C\n", "PATH=/usr/bin:/bin\n"} {
		if !strings.Contains(amb, v) {
			t.Errorf("ambiente senza %q: %s", v, amb)
		}
	}
	if strings.Contains(amb, "HOME=") || strings.Contains(amb, "it_IT") {
		t.Errorf("ambiente del processo passato a lp: %s", amb)
	}
}

// Rifiuti, risposte strane e tempi: niente id, mai un lavoro «a buon fine» senza la riga di lp.
func TestStampaRifiutiETempi(t *testing.T) {
	casi := []struct {
		nome, lp string
		verifica func(error) bool
		motivo   string
	}{
		{"coda che non c'è", `echo "lp: The printer or class does not exist." >&2; exit 1`,
			func(e error) bool { return errors.Is(e, ErrRifiutato) }, "lp: The printer or class does not exist."},
		{"scheduler fermo", `echo "lp: Scheduler is not running." >&2; exit 1`,
			func(e error) bool { return errors.Is(e, ErrRifiutato) }, "lp: Scheduler is not running."},
		{"uscita 0 senza id", `echo "qualcosa d'altro"`, func(e error) bool { return errors.Is(e, ErrRifiutato) }, ""},
		{"id in italiano", `echo "l'ID della richiesta è Coda-3 (0 file)"`, func(e error) bool { return errors.Is(e, ErrRifiutato) }, ""},
		{"id con spazi", `echo "request id is Coda 3 (0 file(s))"`, func(e error) bool { return errors.Is(e, ErrRifiutato) }, ""},
		{"id solo sullo standard error", `echo "request id is Coda-3 (0 file(s))" >&2`, func(e error) bool { return errors.Is(e, ErrRifiutato) }, ""},
		{"messaggio con controlli", `printf '\033[31mlp: Unable to\tprint\r\n' >&2; exit 2`,
			func(e error) bool { return errors.Is(e, ErrRifiutato) }, "[31mlp: Unable toprint"},
	}
	for _, k := range casi {
		c, _ := comandiFinti(t, "cat > /dev/null; "+k.lp, "", "")
		id, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 76, AltezzaMM: 127})
		if id != "" || !k.verifica(err) {
			t.Errorf("%s: %q %v", k.nome, id, err)
		}
		var r Rifiuto
		if k.motivo != "" && (!errors.As(err, &r) || r.Motivo != k.motivo) {
			t.Errorf("%s: motivo %q", k.nome, r.Motivo)
		}
	}
	// Tempo scaduto: lp fermo, si ferma al tempo e niente id.
	c, _ := comandiFinti(t, `exec sleep 5`, "", "")
	c.TempoStampa = 200 * time.Millisecond
	inizio := time.Now()
	if id, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32}); id != "" || !errors.Is(err, ErrTempoScaduto) {
		t.Errorf("tempo: %q %v", id, err)
	}
	if d := time.Since(inizio); d > 5*time.Second {
		t.Errorf("tempo scaduto dopo %v", d)
	}
	// Il comando non c'è.
	c, _ = comandiFinti(t, "", "", "")
	if _, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32}); !errors.Is(err, ErrNonDisponibile) {
		t.Errorf("senza lp: %v", err)
	}
	// Documento vuoto e coda non valida: lp non parte.
	c, dir := comandiFinti(t, `echo "request id is Coda-1 (0 file(s))"`, "", "")
	if _, err := c.Print(context.Background(), "Coda", nil, Options{LarghezzaMM: 57, AltezzaMM: 32}); err == nil {
		t.Error("documento vuoto accettato")
	}
	for _, coda := range []string{"", "-o", "-dAltra", "Coda Due", "Coda/istanza", "Coda\n", "Coda'", `Co"da`, "Codà", strings.Repeat("a", 128)} {
		if _, err := c.Print(context.Background(), coda, []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32}); !errors.Is(err, ErrCodaNonValida) {
			t.Errorf("coda %q: %v", coda, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "lp.argomenti")); err == nil {
		t.Error("lp lanciato con un documento vuoto o una coda non valida")
	}
}

// Le opzioni passano una per argomento, dopo la validazione: un valore non diventa mai un'altra
// opzione di lp, e PageSize non si sceglie a mano.
func TestOpzioniNonIniettabili(t *testing.T) {
	c, dir := comandiFinti(t, `cat > /dev/null; echo "request id is Coda-1 (0 file(s))"`, "", "")
	for _, o := range []Opzione{{"Darkness", "8 -d Altra"}, {"Darkness", "8\n-dAltra"}, {"-d", "Altra"}, {"PageSize", "A4"},
		{"media", "A4"}, {"Darkness", ""}, {"Dark ness", "8"}, {"Darkness", "8;rm"}, {"Darkness", "$(id)"}} {
		if _, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32, Extra: []Opzione{o}}); !errors.Is(err, ErrOpzioneVietata) {
			t.Errorf("%+v: %v", o, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "lp.argomenti")); err == nil {
		t.Error("lp lanciato con un'opzione non valida")
	}
	// Il titolo si ripulisce: niente spazi, niente «-» in testa, al più 64 caratteri.
	if _, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32, Titolo: "--Ada Rossi è/qui.pdf"}); err != nil {
		t.Fatal(err)
	}
	if a := letto(t, filepath.Join(dir, "lp.argomenti")); !strings.HasPrefix(a, "-d\nCoda\n-t\nAdaRossiqui.pdf\n-o\nPageSize=") {
		t.Errorf("titolo: %q", a)
	}
}

func TestFormatoPagina(t *testing.T) {
	for _, k := range []struct {
		l, h   float64
		atteso string
	}{{57, 32, "Custom.57x32mm"}, {76, 127, "Custom.76x127mm"}, {57.5, 20.25, "Custom.57.5x20.25mm"}, {57.03125, 20.5, "Custom.57.03x20.5mm"}, {16, 16, "Custom.16x16mm"}} {
		if s, err := FormatoPagina(k.l, k.h); err != nil || s != k.atteso {
			t.Errorf("%v×%v: %q %v", k.l, k.h, s, err)
		}
	}
	for _, k := range [][2]float64{{0, 32}, {57, -1}, {1001, 32}} {
		if _, err := FormatoPagina(k[0], k[1]); err == nil {
			t.Errorf("%v accettato", k)
		}
	}
}

// lpstat -e: un nome per riga, anche quelli che il Go non accetta (la pagina li mostra non
// sceglibili); senza code l'elenco è vuoto; lo scheduler fermo è un errore.
func TestElencoCode(t *testing.T) {
	c, dir := comandiFinti(t, "", `printf 'Etichette_QR\nMUNBYN-ITPP130\nUfficio@server\nCoda strana\n\n'`, "")
	qq, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(qq, []Queue{{"Etichette_QR"}, {"MUNBYN-ITPP130"}, {"Ufficio@server"}, {"Coda strana"}}) {
		t.Errorf("%+v", qq)
	}
	if a := letto(t, filepath.Join(dir, "lpstat.argomenti")); a != "-e\n" {
		t.Errorf("argomenti %q", a)
	}
	c, _ = comandiFinti(t, "", `echo "lpstat: No destinations added." >&2; exit 1`, "")
	if qq, err := c.List(context.Background()); err != nil || len(qq) != 0 {
		t.Errorf("senza code: %+v %v", qq, err)
	}
	c, _ = comandiFinti(t, "", `echo "lpstat: Scheduler is not running." >&2; exit 1`, "")
	if _, err := c.List(context.Background()); !errors.Is(err, ErrNonDisponibile) || !strings.Contains(err.Error(), "Scheduler is not running") {
		t.Errorf("scheduler fermo: %v", err)
	}
	c, _ = comandiFinti(t, "", `sleep 5 # il figlio tiene aperta l uscita`, "")
	c.TempoElenco = 200 * time.Millisecond
	inizio := time.Now()
	if _, err := c.List(context.Background()); !errors.Is(err, ErrTempoScaduto) {
		t.Errorf("tempo: %v", err)
	}
	if d := time.Since(inizio); d > 5*time.Second {
		t.Errorf("tempo scaduto dopo %v con un figlio che tiene aperta l'uscita", d)
	}
	c, _ = comandiFinti(t, "", "", "")
	if _, err := c.List(context.Background()); !errors.Is(err, ErrNonDisponibile) {
		t.Errorf("senza lpstat: %v", err)
	}
}

// lpoptions -p <coda> -l, come lo scrive CUPS per un PPD di una termica.
func TestOpzioniDellaCoda(t *testing.T) {
	uscita := `PageSize/Media Size: w162h90 *w288h432 Custom.WIDTHxHEIGHT
MediaTracking/Media Tracking: Continuous *Gap Mark
Darkness/Darkness: 1 2 3 4 5 6 7 *8 9 10 11 12 13 14 15
riga senza due punti
`
	c, dir := comandiFinti(t, "", "", `printf '%s' '`+uscita+`'`)
	oo, err := c.Opzioni(context.Background(), "MUNBYN")
	if err != nil || len(oo) != 3 {
		t.Fatalf("%+v %v", oo, err)
	}
	if oo[1].Chiave != "MediaTracking" || oo[1].Nome != "Media Tracking" || oo[1].Predefinita != "Gap" ||
		!reflect.DeepEqual(oo[1].Scelte, []string{"Continuous", "Gap", "Mark"}) {
		t.Errorf("%+v", oo[1])
	}
	if a := letto(t, filepath.Join(dir, "lpoptions.argomenti")); a != "-p\nMUNBYN\n-l\n" {
		t.Errorf("argomenti %q", a)
	}
	if _, err := c.Opzioni(context.Background(), "-l"); !errors.Is(err, ErrCodaNonValida) {
		t.Errorf("coda non valida: %v", err)
	}
	ok, _ := LeggiOpzioni("MediaTracking=Gap darkness=8")
	if err := ControllaOpzioni(ok, oo); err != nil {
		t.Errorf("valide: %v", err)
	}
	for _, s := range []string{"MediaTracking=Nastro", "Velocita=3", "Darkness=16"} {
		x, _ := LeggiOpzioni(s)
		if err := ControllaOpzioni(x, oo); !errors.Is(err, ErrOpzioneVietata) {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// Le opzioni scritte nelle impostazioni.
func TestLeggiOpzioni(t *testing.T) {
	oo, err := LeggiOpzioni("  MediaTracking=Gap\tDarkness=8  ")
	if err != nil || !reflect.DeepEqual(oo, []Opzione{{"MediaTracking", "Gap"}, {"Darkness", "8"}}) || ScriviOpzioni(oo) != "MediaTracking=Gap Darkness=8" {
		t.Errorf("%+v %v", oo, err)
	}
	if oo, err := LeggiOpzioni(""); err != nil || oo != nil {
		t.Errorf("vuoto: %+v %v", oo, err)
	}
	for _, s := range []string{"Darkness", "=8", "Darkness=", "PageSize=w288h432", "pagesize=A4", "media=A4", "fit-to-page=true",
		"Darkness=8 darkness=9", "Dark$ness=8", "Darkness=8'", `Darkness="8"`, "1Darkness=8", "Darkness=a=b",
		strings.Repeat("A=1 ", 17)} {
		if _, err := LeggiOpzioni(s); !errors.Is(err, ErrOpzioneVietata) {
			t.Errorf("%q accettato: %v", s, err)
		}
	}
}

// La finta fa le stesse validazioni di lp e non lancia niente.
func TestFinta(t *testing.T) {
	f := &Finta{Code: []string{"A"}}
	if id, err := f.Print(context.Background(), "A", []byte("x"), Options{LarghezzaMM: 57, AltezzaMM: 32}); err != nil || id != "A-1" {
		t.Errorf("%q %v", id, err)
	}
	if _, err := f.Print(context.Background(), "B", []byte("x"), Options{LarghezzaMM: 57, AltezzaMM: 32}); !errors.Is(err, ErrRifiutato) {
		t.Errorf("coda assente: %v", err)
	}
	if _, err := f.Print(context.Background(), "-A", []byte("x"), Options{LarghezzaMM: 57, AltezzaMM: 32}); !errors.Is(err, ErrCodaNonValida) {
		t.Errorf("coda non valida: %v", err)
	}
	if len(f.Lavori()) != 1 {
		t.Errorf("%d lavori", len(f.Lavori()))
	}
}

// vivo dice se il processo pid c'è ancora (uno zombie, già morto e in attesa del padre, non conta).
func vivo(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	f := strings.Fields(string(b))
	return len(f) > 2 && f[2] != "Z"
}

// Il tempo scaduto uccide tutto il gruppo del comando: anche un nipote (un processo lanciato dal
// comando, che resterebbe vivo uccidendo solo il primo). Gli sleep dei comandi finti durano 5 s,
// più dei tempi massimi dei test (200-300 ms) e dei 3 s di attesa qui sotto: se un test cade, un
// orfano vive al più quei 5 s (esegui di produzione non dà Pdeathsig, slice 051b).
func TestTempoScadutoUccideIlGruppo(t *testing.T) {
	c, dir := comandiFinti(t, `sleep 5 & echo $! > "$(dirname "$0")/nipote.pid"; wait`, "", "")
	c.TempoStampa = 300 * time.Millisecond
	if _, err := c.Print(context.Background(), "Coda", []byte("%PDF"), Options{LarghezzaMM: 57, AltezzaMM: 32}); !errors.Is(err, ErrTempoScaduto) {
		t.Fatalf("tempo: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(letto(t, filepath.Join(dir, "nipote.pid"))))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; vivo(pid); i++ {
		if i > 30 {
			_ = syscall.Kill(pid, syscall.SIGKILL) // non lasciarlo in giro
			t.Fatalf("il nipote %d è vivo dopo il tempo scaduto", pid)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
