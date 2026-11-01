package imaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// TestMain: con «image-worker …» come argomenti il binario dei test fa da gestrip image-worker,
// così il lavoratore lo avvia come figlio. Le operazioni «prova-…» ci sono solo qui: provano i
// limiti e l'ambiente del figlio; dopo l'operazione arriva il limite di CPU che il padre manda.
func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == ComandoFiglio && strings.HasPrefix(os.Args[2], "prova-") {
		os.Exit(figlioDiProva(os.Args[2], os.Args[3:]))
	}
	if len(os.Args) >= 2 && os.Args[1] == ComandoFiglio {
		os.Exit(Figlio(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

var tieni []byte

// figlioDiProva fa l'operazione di prova op; resto sono gli argomenti dopo l'operazione (il limite
// di CPU del padre).
func figlioDiProva(op string, resto []string) int {
	// prova-figlio-N: il Figlio vero, con un limite di memoria di N MiB al posto di quello di
	// LimitiDelFiglio (misura del limite e prova che Figlio si dà i limiti) e gli argomenti del padre.
	if n, ok := strings.CutPrefix(op, "prova-figlio-"); ok {
		m, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			return 3
		}
		LimitiDelFiglio.Memoria = m << 20
		return Figlio(append([]string{OpNormalizza}, resto...), os.Stdin, os.Stdout)
	}
	// I limiti che il Figlio vero si darebbe con gli argomenti del padre.
	lim, ok := limitiDagliArgomenti(resto)
	if !ok {
		return UscitaOperazione
	}
	switch op {
	case "prova-limiti":
		if err := preparaFiglio(lim); err != nil {
			return 1
		}
		l, err := os.ReadFile("/proc/self/limits")
		if err != nil {
			return 1
		}
		_, _ = os.Stdout.Write(l) // un errore si vede dal padre: niente uscita
		return 0
	case "prova-memoria":
		if err := preparaFiglio(lim); err != nil {
			return 1
		}
		tieni = make([]byte, LimitiDelFiglio.Memoria+128<<20)
		for i := 0; i < len(tieni); i += 4096 {
			tieni[i] = 1
		}
		fmt.Print("sopravvissuto")
		return 0
	case "prova-cpu":
		if err := impostaLimiti(lim); err != nil {
			return 1
		}
		for x := uint64(1); ; x = x*6364136223846793005 + 1 {
			if x == 0 {
				fmt.Print(x)
			}
		}
	case "prova-dormi":
		time.Sleep(time.Minute)
		fmt.Print("sveglio")
		return 0
	case "prova-ambiente":
		fmt.Print(strings.Join(os.Environ(), "\n"))
		return 0
	case "prova-troppo":
		_, _ = os.Stdout.Write(make([]byte, MaxByteUscita+1)) // il padre lo rifiuta comunque
		return 0
	case "prova-niente":
		return 0
	case "prova-senza-limiti":
		// Il limite duro di CPU sotto quello di LimitiDelFiglio: il Figlio vero non può darsi i suoi.
		if abbassaLimiteCPU == nil || abbassaLimiteCPU() != nil {
			return 98
		}
		return Figlio([]string{OpNormalizza}, os.Stdin, os.Stdout)
	case "prova-panico":
		// Il Figlio vero con una conversione che va in panic.
		normalizzaNelFiglio = func([]byte) ([]byte, error) { panic("decoder") }
		return Figlio([]string{OpNormalizza}, os.Stdin, os.Stdout)
	case "prova-uccidi":
		// Un segnale che non viene né dal tempo del padre né dal limite di CPU (come il killer della
		// memoria del kernel).
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Kill() // SIGKILL a sé stesso; se non arriva, l'uscita qui sotto lo dice
		}
		time.Sleep(time.Minute)
		return 0
	}
	if n, ok := strings.CutPrefix(op, "prova-uscita-"); ok {
		c, err := strconv.Atoi(n)
		if err != nil {
			return 99
		}
		return c
	}
	return 99
}

// abbassaLimiteCPU mette a 1 s il limite duro di CPU del processo (solo Linux, nel figlio).
var abbassaLimiteCPU func() error

// tempoFotoGrandi è il tempo del figlio nei test delle foto grandi. Il tempo vero (TempoMassimo,
// 10 s di orologio) lo prova TestFiglioUccisoDopoIlTempo; qui si provano la memoria e la CPU, che
// non dipendono dal carico della macchina, mentre l'orologio sì: misurato (slice 037c) il JPEG
// progressivo CMYK 8640×6480 vuole 2,5 s di CPU e, con un quarto di CPU, 10,1 s di orologio, oltre
// i 10 s. Due minuti bastano anche con un ventesimo di CPU.
const tempoFotoGrandi = 2 * time.Minute

// lavoratoreFotoGrandi è il lavoratore dei test delle foto grandi, con tempoFotoGrandi e, se
// GESTRIP_TEST_CPU_FIGLIO c'è, il suo limite di CPU (cpuFiglioDeiTest).
func lavoratoreFotoGrandi(t *testing.T) *Lavoratore {
	t.Helper()
	l := lavoratore(t)
	l.Tempo = tempoFotoGrandi
	l.CPU = cpuFiglioDeiTest(t)
	return l
}

// cpuFiglioDeiTest è GESTRIP_TEST_CPU_FIGLIO, in secondi, o 0 se non c'è (slice 037e): il limite
// di CPU del figlio nei test delle foto grandi su una macchina più lenta di pc-fisso. La CI la mette
// (.github/workflows/ci.yml): sul runner di GitHub, con i pacchetti dei test insieme su 4 CPU, il
// JPEG progressivo CMYK 8640×6480 ha usato 7,92 s di CPU (run 41305328853) e una volta è stato
// ucciso al limite di 8 s. La legge solo il codice dei test: in produzione il limite è sempre
// LimitiDelFiglio.CPU (8 s) e nessuna variabile d'ambiente lo cambia. Un valore fuori da
// MinCPUFiglio…MaxCPUFiglio ferma il test.
func cpuFiglioDeiTest(t *testing.T) uint64 {
	t.Helper()
	v, ok := os.LookupEnv("GESTRIP_TEST_CPU_FIGLIO")
	if !ok {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n < MinCPUFiglio || n > MaxCPUFiglio {
		t.Fatalf("GESTRIP_TEST_CPU_FIGLIO=%q: attesi secondi interi fra %d e %d", v, MinCPUFiglio, MaxCPUFiglio)
	}
	return n
}

// cpuMassimaFotoGrande è la CPU che una foto grande accettata può usare nel figlio: il margine verso
// i 10 s di TempoMassimo, che il tempo largo dei test non guarda più. Misurato nella slice 037c
// (review) su pc-fisso, 16 giri per foto fra CPU piena, 4 processori e un quarto di CPU: al più
// 2,58 s (JPEG progressivo CMYK 8640×6480 e CMYK di base 8944×6708), 2,36 s a CPU piena. 5 s sono
// circa il doppio, e lasciano metà dei 10 s a un figlio che ha una CPU intera; il limite di CPU del
// figlio è 8 s.
// La CPU non dipende dal carico ma dipende dalla macchina: sul runner di GitHub le stesse foto hanno
// usato fino a 7,92 s (slice 037e). Il controllo vale quindi solo dove la soglia è tarata
// (pc-fisso, scripts/check): con GESTRIP_TEST_CPU_FIGLIO (la CI) la CPU si scrive nel log e basta.
// La misura vera, sull'apparecchio, è della T5.
const cpuMassimaFotoGrande = 5 * time.Second

// controllaCPU scrive nel log la CPU u del figlio per la foto cosa e, fuori dalla CI
// (GESTRIP_TEST_CPU_FIGLIO assente), vuole che stia entro cpuMassimaFotoGrande. Una CPU negativa
// (nessuno stato del figlio) è sempre un errore.
func controllaCPU(t *testing.T, cosa string, u time.Duration) {
	t.Helper()
	t.Logf("%s: CPU del figlio %v", cosa, u)
	switch {
	case u < 0:
		t.Errorf("%s: CPU del figlio non letta", cosa)
	case cpuFiglioDeiTest(t) != 0:
		// La CI: la soglia è di pc-fisso, la misura resta nel log.
	case u > cpuMassimaFotoGrande:
		t.Errorf("%s: %v di CPU nel figlio, al più %v", cosa, u, cpuMassimaFotoGrande)
	}
}

// cpuDelFiglio fa misurare la CPU di ogni figlio: dopo ogni foto, ultima() dà la CPU dell'ultimo.
func cpuDelFiglio(t *testing.T) (ultima func() time.Duration) {
	t.Helper()
	prima := eseguiFiglio
	var cpu time.Duration
	eseguiFiglio = func(cmd *exec.Cmd) error {
		err := prima(cmd)
		cpu = -1
		if ps := cmd.ProcessState; ps != nil {
			cpu = ps.UserTime() + ps.SystemTime()
		}
		return err
	}
	t.Cleanup(func() { eseguiFiglio = prima })
	return func() time.Duration { return cpu }
}

// risorse dice se err è un ErrRisorse con la causa c, e non un rifiuto.
func risorse(err error, c Causa) bool {
	return errors.Is(err, ErrRisorse) && !errors.Is(err, ErrImmagine) && CausaDi(err) == c
}

func lavoratore(t *testing.T) *Lavoratore {
	t.Helper()
	l, err := NuovoLavoratore()
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// inventario: RIP-S-30
// Il figlio dà gli stessi byte della conversione nel processo: foto normalizzata e miniatura.
func TestLavoratoreComeNelProcesso(t *testing.T) {
	l := lavoratore(t)
	in := sintetiche.JPEG(sintetiche.Gradiente(800, 600, 1), sintetiche.OpzioniJPEG{Orientamento: 6})
	atteso, err := Normalizza(in)
	if err != nil {
		t.Fatal(err)
	}
	avuto, err := l.Normalizza(ctx, in)
	if err != nil || !bytes.Equal(avuto, atteso) {
		t.Fatalf("normalizza nel figlio: %d byte, %v; attesi %d", len(avuto), err, len(atteso))
	}
	m, err := Miniatura(atteso)
	if err != nil {
		t.Fatal(err)
	}
	if avuta, err := l.Miniatura(ctx, atteso); err != nil || !bytes.Equal(avuta, m) {
		t.Fatalf("miniatura nel figlio: %d byte, %v", len(avuta), err)
	}
	for _, c := range []struct {
		cosa string
		f    func(context.Context, []byte) ([]byte, error)
		in   []byte
	}{
		{"normalizza di byte a caso", l.Normalizza, []byte("not an image")},
		{"miniatura di byte a caso", l.Miniatura, []byte("not an image")},
		{"normalizza di niente", l.Normalizza, nil},
		{"normalizza oltre 25 MiB", l.Normalizza, make([]byte, MaxByteIngresso+1)},
		{"miniatura oltre 16 MiB", l.Miniatura, make([]byte, MaxByteUscita+1)},
	} {
		if out, err := c.f(ctx, c.in); !errors.Is(err, ErrImmagine) || out != nil {
			t.Errorf("%s: %d byte, %v", c.cosa, len(out), err)
		}
	}
}

var ctx = context.Background()

func soloLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("limiti del figlio: solo Linux")
	}
}

// inventario: RIP-S-30
// Il figlio si dà 1440 MiB di memoria (oltre ai dati che ha già all'avvio; vedi LimitiDelFiglio) e 8 s di CPU prima di
// leggere la foto, come gli script dello standalone (in Go con RLIMIT_DATA: vedi Limiti).
// Il limite di CPU arriva dal padre (Lavoratore.CPU, slice 037e): il lavoratore di produzione
// (NuovoLavoratore) manda sempre 8 s, anche con GESTRIP_TEST_CPU_FIGLIO nell'ambiente; un
// lavoratore dei test può mandare da MinCPUFiglio a MaxCPUFiglio.
func TestFiglioConILimiti(t *testing.T) {
	soloLinux(t)
	t.Setenv("GESTRIP_TEST_CPU_FIGLIO", "30") // non tocca il lavoratore di produzione
	for _, c := range []struct {
		cpu  uint64 // Lavoratore.CPU
		vuoi string // il limite nel figlio
	}{{0, "8"}, {MinCPUFiglio, "1"}, {30, "30"}, {MaxCPUFiglio, "60"}} {
		l := lavoratore(t)
		if l.CPU != 0 {
			t.Fatalf("NuovoLavoratore: CPU %d, atteso 0 (LimitiDelFiglio)", l.CPU)
		}
		l.CPU = c.cpu
		out, err := l.esegui(ctx, "prova-limiti", []byte("x"), 1, 1<<20)
		if err != nil {
			t.Fatalf("CPU %d: %v", c.cpu, err)
		}
		i := strings.Index(string(out), "Max cpu time")
		if i < 0 {
			t.Fatalf("manca il limite di CPU in\n%s", out)
		}
		if v := strings.Fields(string(out[i+len("Max cpu time"):]))[:2]; v[0] != c.vuoi || v[1] != c.vuoi {
			t.Errorf("Lavoratore.CPU %d: limite %v, attesi %s %s", c.cpu, v, c.vuoi, c.vuoi)
		}
	}
	out, err := lavoratore(t).esegui(ctx, "prova-limiti", []byte("x"), 1, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	valori := func(riga string) []string {
		i := strings.Index(string(out), riga)
		if i < 0 {
			t.Fatalf("manca %q in\n%s", riga, out)
		}
		return strings.Fields(string(out[i+len(riga):]))[:2]
	}
	if c := valori("Max cpu time"); c[0] != "8" || c[1] != "8" {
		t.Errorf("CPU: %v, attesi 8 8", c)
	}
	if c := valori("Max core file size"); c[0] != "0" || c[1] != "0" {
		t.Errorf("core: %v, attesi 0 0", c)
	}
	d := valori("Max data size")
	dati, err := strconv.ParseUint(d[0], 10, 64)
	if err != nil || d[1] != d[0] {
		t.Fatalf("dati: %v", d)
	}
	// I dati all'avvio del figlio (VmData) stanno sotto i 256 MiB: il limite è 1440 MiB più quelli.
	if dati <= 1440<<20 || dati > (1440+256)<<20 {
		t.Errorf("dati: %d, attesi 1440 MiB più quelli dell'avvio", dati)
	}
	t.Logf("limite dei dati del figlio: %d MiB", dati>>20)
}

// inventario: RIP-S-30
// Oltre la memoria il figlio cade (il runtime di Go esce con 2) e il padre resta su: non è un
// rifiuto della foto ma ErrRisorse, causa «memoria» (slice 037c).
func TestFiglioOltreLaMemoria(t *testing.T) {
	soloLinux(t)
	out, err := lavoratoreFotoGrandi(t).esegui(ctx, "prova-memoria", []byte("x"), 1, 1<<20)
	if !risorse(err, CausaMemoria) || len(out) != 0 {
		t.Fatalf("%q, %v (causa %q); atteso ErrRisorse per la memoria", out, err, CausaDi(err))
	}
}

// inventario: RIP-S-30
// Oltre il tempo di CPU il figlio cade (qui con un limite di 1 s, per fare in fretta): il kernel lo
// uccide con SIGKILL e il padre, dal tempo di CPU che il figlio ha usato, dice ErrRisorse con la
// causa «cpu», non un rifiuto (slice 037c).
func TestFiglioOltreLaCPU(t *testing.T) {
	soloLinux(t)
	l := lavoratore(t)
	l.CPU = 1 // il padre lo manda a prova-cpu e lo usa per la causa (slice 037e)
	inizio := time.Now()
	out, err := l.esegui(ctx, "prova-cpu", []byte("x"), 1, 1<<20)
	if !risorse(err, CausaCPU) || len(out) != 0 {
		t.Fatalf("%q, %v (causa %q); atteso ErrRisorse per la CPU", out, err, CausaDi(err))
	}
	if d := time.Since(inizio); d > 8*time.Second {
		t.Errorf("il figlio è caduto dopo %v", d)
	}
}

// inventario: RIP-S-30
// Il padre uccide il figlio dopo il suo tempo (10 s, qui 300 ms): ErrRisorse con la causa «tempo»,
// non un rifiuto della foto (slice 037c; nello standalone il timeout esce come rifiuto).
func TestFiglioUccisoDopoIlTempo(t *testing.T) {
	l := lavoratore(t)
	if l.Tempo != 0 {
		t.Fatalf("tempo di base %v, atteso 0 (TempoMassimo)", l.Tempo)
	}
	if TempoMassimo != 10*time.Second {
		t.Errorf("TempoMassimo = %v", TempoMassimo)
	}
	l.Tempo = 300 * time.Millisecond
	inizio := time.Now()
	out, err := l.esegui(ctx, "prova-dormi", []byte("x"), 1, 1<<20)
	if !risorse(err, CausaTempo) || len(out) != 0 {
		t.Fatalf("%q, %v (causa %q); atteso ErrRisorse per il tempo", out, err, CausaDi(err))
	}
	if e := err.Error(); e != "imaging: elaborazione non riuscita adesso (tempo, oltre 300ms)" {
		t.Errorf("testo: %q", e)
	}
	if d := time.Since(inizio); d > 5*time.Second {
		t.Errorf("il padre ha aspettato %v", d)
	}
}

// inventario: RIP-S-30
// Il figlio parte con un ambiente vuoto: niente dell'ambiente del negozio (segreti compresi)
// arriva al processo che legge le foto.
func TestFiglioSenzaAmbiente(t *testing.T) {
	t.Setenv("GESTRIP_SEGRETO_DI_PROVA", "non-deve-passare")
	out, err := lavoratore(t).esegui(ctx, "prova-ambiente", []byte("x"), 1, 1<<20)
	if err != nil && !errors.Is(err, ErrImmagine) {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "GESTRIP_SEGRETO_DI_PROVA") || len(out) != 0 {
		t.Errorf("ambiente del figlio: %q", out)
	}
}

// inventario: RIP-S-30
// Un risultato oltre il massimo o vuoto è un rifiuto, anche con l'uscita 0.
func TestFiglioRisultatoTroppoGrandeOVuoto(t *testing.T) {
	l := lavoratore(t)
	for _, op := range []string{"prova-troppo", "prova-niente"} {
		if out, err := l.esegui(ctx, op, []byte("x"), 1, MaxByteUscita); !errors.Is(err, ErrImmagine) || out != nil {
			t.Errorf("%s: %d byte, %v", op, len(out), err)
		}
	}
}

// inventario: RIP-S-30
// Il figlio vero: un'operazione sconosciuta o mancante, o più argomenti, non fanno niente ed escono
// con UscitaOperazione.
func TestFiglioOperazioneSconosciuta(t *testing.T) {
	for _, args := range [][]string{nil, {"altro"}, {OpNormalizza, "di più"}} {
		var out bytes.Buffer
		if c := Figlio(args, bytes.NewReader(sintetiche.PNG(8, 8, 1)), &out); c != UscitaOperazione || out.Len() != 0 {
			t.Errorf("%q: uscita %d, %d byte", args, c, out.Len())
		}
	}
	cmd := exec.Command(os.Args[0], ComandoFiglio, OpNormalizza)
	cmd.Stdin = bytes.NewReader(sintetiche.PNG(8, 8, 1))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := sintetiche.LeggiJPEG(out); !ok || i.Larghezza != 8 {
		t.Errorf("figlio vero: %+v %v", i, ok)
	}
}

// inventario: RIP-S-30
// Una richiesta annullata ferma il figlio e dà l'errore del contesto, non un rifiuto della foto.
func TestFiglioRichiestaAnnullata(t *testing.T) {
	c, annulla := context.WithCancel(ctx)
	annulla()
	if out, err := lavoratore(t).esegui(c, "prova-dormi", []byte("x"), 1, 1<<20); !errors.Is(err, context.Canceled) || out != nil {
		t.Errorf("%d byte, %v; atteso context.Canceled", len(out), err)
	}
}

// inventario: RIP-S-30
// Le foto più grandi che si accettano stanno nei limiti del figlio: un JPEG appena sotto i 60 MP,
// un PNG a 8 bit con trasparenza da 42 MP (Pillow 9.4 rifiuta già 7000×7000, misurato) e uno in
// scala di grigi da 60 MP.
func TestLavoratoreFotoGrandiNeiLimiti(t *testing.T) {
	soloLinux(t)
	if testing.Short() {
		t.Skip("immagini da 60 MP")
	}
	l := lavoratoreFotoGrandi(t)
	cpu := cpuDelFiglio(t)
	grigio := image.NewGray(image.Rect(0, 0, 7740, 7740))
	for _, c := range []struct {
		cosa string
		in   []byte
		l, a int
	}{
		{"JPEG 8944×6708", sintetiche.JPEG(sintetiche.Liscio(8944, 6708), sintetiche.OpzioniJPEG{}), 4096, 3072},
		{"PNG grigio 7740²", sintetiche.CodificaPNG(grigio), 4096, 4096},
		{"PNG RGBA 7000×6000", sintetiche.PNGRighe(7000, 6000, 6, 8, sintetiche.Sfumata), 4096, 3511},
	} {
		inizio := time.Now()
		out, err := l.Normalizza(ctx, c.in)
		if err != nil {
			t.Errorf("%s (%d byte): %v (causa %q) dopo %v", c.cosa, len(c.in), err, CausaDi(err), time.Since(inizio))
			continue
		}
		i, _ := sintetiche.LeggiJPEG(out)
		if i.Larghezza != c.l || i.Altezza != c.a {
			t.Errorf("%s: %d×%d, attese %d×%d", c.cosa, i.Larghezza, i.Altezza, c.l, c.a)
		}
		controllaCPU(t, c.cosa, cpu())
		t.Logf("%s: %v di orologio", c.cosa, time.Since(inizio))
	}
}

// inventario: RIP-S-30
// Le foto più pesanti da decodificare che Pillow 9.4 accetta nei suoi limiti (misurate con lo
// script dello standalone nella sua immagine) passano dal figlio: JPEG progressivo CMYK 8640×6480
// (il più grande che Pillow accetta: 8680×6510 no), progressivo 4:4:4 da 60 MP, CMYK di base da
// 60 MP, PNG RGB a 16 bit da 59,5 MP e RGBA a 16 bit da 42 MP.
func TestLavoratoreFotoCheLoStandaloneAccetta(t *testing.T) {
	soloLinux(t)
	if testing.Short() {
		t.Skip("foto fino a 60 MP")
	}
	l := lavoratoreFotoGrandi(t)
	cpu := cpuDelFiglio(t)
	for _, c := range []struct {
		cosa string
		in   func() []byte
	}{
		{"progressivo CMYK 8640×6480", func() []byte {
			return sintetiche.JPEGPiatto(8640, 6480, sintetiche.OpzioniJPEGPiatto{Progressivo: true, Componenti: 4})
		}},
		{"progressivo 4:4:4 8944×6708", func() []byte {
			return sintetiche.JPEGPiatto(8944, 6708, sintetiche.OpzioniJPEGPiatto{Progressivo: true})
		}},
		{"CMYK di base 8944×6708", func() []byte {
			return sintetiche.JPEGPiatto(8944, 6708, sintetiche.OpzioniJPEGPiatto{Componenti: 4})
		}},
		{"PNG RGB 16 bit 7000×8500", func() []byte { return sintetiche.PNGRighe(7000, 8500, 2, 16, sintetiche.Sfumata) }},
		{"PNG RGBA 16 bit 7000×6000", func() []byte { return sintetiche.PNGRighe(7000, 6000, 6, 16, sintetiche.Sfumata) }},
	} {
		inizio := time.Now()
		if _, err := l.Normalizza(ctx, c.in()); err != nil {
			t.Errorf("%s: %v (causa %q) dopo %v", c.cosa, err, CausaDi(err), time.Since(inizio))
		}
		controllaCPU(t, c.cosa, cpu())
		t.Logf("%s: %v di orologio", c.cosa, time.Since(inizio))
	}
}

// inventario: RIP-S-30
// Il Figlio vero si dà i limiti: con 256 MiB al posto di quelli di LimitiDelFiglio il JPEG
// progressivo 4:4:4 da 60 MP (che ne vuole circa 875) non si fa, per la memoria negata (ErrRisorse,
// causa «memoria»: non è un rifiuto della foto), e una foto piccola passa. Con il limite di CPU di
// 1 s mandato dal padre (slice 037e) la stessa foto, che vuole almeno 1,6 s di CPU (misurato nella
// 037c), cade per la CPU.
func TestFiglioVeroSiDaILimiti(t *testing.T) {
	soloLinux(t)
	l := lavoratoreFotoGrandi(t)
	grande := sintetiche.JPEGPiatto(8944, 6708, sintetiche.OpzioniJPEGPiatto{Progressivo: true})
	uno := lavoratoreFotoGrandi(t)
	uno.CPU = 1
	if out, err := uno.Normalizza(ctx, grande); !risorse(err, CausaCPU) || out != nil {
		t.Errorf("grande con 1 s di CPU: %d byte, %v (causa %q)", len(out), err, CausaDi(err))
	}
	if out, err := l.esegui(ctx, "prova-figlio-256", grande, MaxByteIngresso, MaxByteUscita); !risorse(err, CausaMemoria) || out != nil {
		t.Errorf("grande con 256 MiB: %d byte, %v (causa %q)", len(out), err, CausaDi(err))
	}
	if out, err := l.esegui(ctx, "prova-figlio-256", sintetiche.PNG(64, 64, 1), MaxByteIngresso, MaxByteUscita); err != nil || len(out) == 0 {
		t.Errorf("piccola con 256 MiB: %v", err)
	}
}

// inventario: RIP-S-30
// Il protocollo d'uscita del figlio non è ambiguo (slice 037c): codici propri per il rifiuto, i
// limiti che non si mettono e l'operazione sconosciuta, diversi da 0, da 1 e da 2 (l'uscita del
// runtime di Go per un errore fatale, come «out of memory», e per un panic).
func TestCodiciDUscitaDelFiglio(t *testing.T) {
	codici := map[int]string{}
	for c, cosa := range map[string]int{"riuscita": UscitaRiuscita, "rifiuto": UscitaRifiuto, "senza limiti": UscitaSenzaLimiti, "operazione": UscitaOperazione} {
		codici[cosa] = c
	}
	if len(codici) != 4 || UscitaRiuscita != 0 {
		t.Errorf("codici non distinti: %v", codici)
	}
	for c, cosa := range codici {
		if c != UscitaRiuscita && (c == 1 || c == 2 || c < 0 || c > 125) {
			t.Errorf("%s: codice %d ambiguo", cosa, c)
		}
	}
	var out bytes.Buffer
	if c := Figlio([]string{"altro"}, bytes.NewReader(nil), &out); c != UscitaOperazione {
		t.Errorf("operazione sconosciuta: %d", c)
	}
	// Il rifiuto del Figlio vero, nel processo figlio (i limiti non toccano il processo dei test).
	cmd := exec.Command(os.Args[0], ComandoFiglio, OpNormalizza)
	cmd.Stdin = strings.NewReader("not an image")
	var ee *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &ee) || ee.ExitCode() != UscitaRifiuto {
		t.Errorf("rifiuto del figlio vero: %v", err)
	}
}

// inventario: RIP-S-30
// Un panic della conversione (un decoder che sbaglia su byte strani) è un rifiuto della foto, come
// un'eccezione di Pillow nello standalone: non si confonde con un errore fatale del runtime.
func TestPanicDellaConversioneERifiuto(t *testing.T) {
	out, err := converti(func([]byte) ([]byte, error) { panic("decoder") }, []byte("x"))
	if !errors.Is(err, ErrImmagine) || out != nil {
		t.Errorf("panic: %d byte, %v", len(out), err)
	}
	out, err = converti(func(b []byte) ([]byte, error) { return append(b, 'y'), nil }, []byte("x"))
	if err != nil || string(out) != "xy" {
		t.Errorf("riuscita: %q, %v", out, err)
	}
}

// inventario: RIP-S-30
// Ogni uscita del figlio che non è un risultato né un rifiuto è ErrRisorse con la sua causa, per il
// registro: l'uscita 2 del runtime (errore fatale: la memoria negata), i limiti che non si mettono
// (avvio), un segnale che non viene dal tempo né dalla CPU, un codice fuori dal protocollo. Il
// rifiuto resta ErrImmagine. La causa non porta niente della foto: solo la parola e, nel testo
// dell'errore, il segnale o il codice.
func TestUsciteDelFiglioComeErrori(t *testing.T) {
	soloLinux(t)
	l := lavoratore(t)
	for _, c := range []struct {
		op    string
		causa Causa // "" per un rifiuto
		testo string
	}{
		{"prova-uscita-" + strconv.Itoa(UscitaRifiuto), "", "imaging: immagine non valida"},
		{"prova-uscita-2", CausaMemoria, "imaging: elaborazione non riuscita adesso (memoria, uscita 2)"},
		{"prova-uscita-" + strconv.Itoa(UscitaSenzaLimiti), CausaAvvio, "imaging: elaborazione non riuscita adesso (avvio, limiti non messi)"},
		{"prova-uscita-" + strconv.Itoa(UscitaOperazione), CausaUscita, "imaging: elaborazione non riuscita adesso (uscita, uscita " + strconv.Itoa(UscitaOperazione) + ")"},
		{"prova-uscita-1", CausaUscita, "imaging: elaborazione non riuscita adesso (uscita, uscita 1)"},
		{"prova-uscita-77", CausaUscita, "imaging: elaborazione non riuscita adesso (uscita, uscita 77)"},
		{"prova-uccidi", CausaSegnale, "imaging: elaborazione non riuscita adesso (segnale, killed)"},
		{"prova-panico", "", "imaging: immagine non valida"},
		{"prova-senza-limiti", CausaAvvio, "imaging: elaborazione non riuscita adesso (avvio, limiti non messi)"},
	} {
		if c.op == "prova-senza-limiti" && os.Geteuid() == 0 {
			t.Log("prova-senza-limiti saltata: da root il figlio può rialzare il limite di CPU")
			continue
		}
		out, err := l.esegui(ctx, c.op, []byte("x"), 1, 1<<20)
		if out != nil || err == nil || err.Error() != c.testo {
			t.Errorf("%s: %d byte, %v; atteso %q", c.op, len(out), err, c.testo)
			continue
		}
		if c.causa == "" {
			if !errors.Is(err, ErrImmagine) || errors.Is(err, ErrRisorse) || CausaDi(err) != "" {
				t.Errorf("%s: %v non è un rifiuto", c.op, err)
			}
		} else if !risorse(err, c.causa) {
			t.Errorf("%s: %v, causa %q; attesa %q", c.op, err, CausaDi(err), c.causa)
		}
	}
	for _, err := range []error{nil, ErrImmagine, context.Canceled, errors.New("altro")} {
		if c := CausaDi(err); c != "" {
			t.Errorf("CausaDi(%v) = %q", err, c)
		}
	}
	if c := CausaDi(fmt.Errorf("avvolto: %w", &ErroreRisorse{Causa: CausaTempo})); c != CausaTempo {
		t.Errorf("causa avvolta: %q", c)
	}
	if e := (&ErroreRisorse{Causa: CausaTempo}).Error(); e != "imaging: elaborazione non riuscita adesso (tempo)" {
		t.Errorf("senza dettaglio: %q", e)
	}
}

// inventario: RIP-S-30
// Un figlio che non parte (binario che non c'è, o un percorso relativo: il figlio parte da «/») è
// ErrRisorse con la causa «avvio», non un rifiuto della foto (slice 037c).
func TestFiglioCheNonParte(t *testing.T) {
	for _, e := range []string{"/nessun/binario/gestrip", "gestrip.test", "./gestrip.test", ""} {
		l := &Lavoratore{Eseguibile: e}
		out, err := l.Normalizza(ctx, sintetiche.PNG(8, 8, 1))
		if !risorse(err, CausaAvvio) || out != nil {
			t.Errorf("%q: %d byte, %v (causa %q)", e, len(out), err, CausaDi(err))
		}
		if strings.Contains(fmt.Sprint(err), "nessun") || strings.Contains(fmt.Sprint(err), "gestrip.test") {
			t.Errorf("%q: il percorso nell'errore: %v", e, err)
		}
	}
}

// inventario: RIP-S-30
// Una richiesta annullata mentre il figlio lavora dà l'errore del contesto, non ErrRisorse: il
// figlio è stato fermato per l'annullamento, non per il suo tempo (review della 037c).
func TestAnnullataDuranteIlFiglio(t *testing.T) {
	c, annulla := context.WithCancel(ctx)
	time.AfterFunc(200*time.Millisecond, annulla)
	out, err := lavoratore(t).esegui(c, "prova-dormi", []byte("x"), 1, 1<<20)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrRisorse) || out != nil {
		t.Errorf("%d byte, %v (causa %q)", len(out), err, CausaDi(err))
	}
}

// inventario: RIP-S-30
// Un figlio uscito da sé con un codice ha la precedenza sul tempo scaduto (review della 037c): un
// rifiuto arrivato proprio alla scadenza resta ErrImmagine, l'uscita 2 resta la memoria. Solo un
// figlio ucciso da un segnale dopo la scadenza è CausaTempo. Gli stati sono di figli veri; la
// scadenza si dà a esitoDelFiglio, così non dipende dall'orologio.
func TestUscitaPrimaDelTempo(t *testing.T) {
	soloLinux(t)
	stato := func(op string) (*os.ProcessState, error) {
		cmd := exec.Command(os.Args[0], ComandoFiglio, op)
		err := cmd.Run()
		if cmd.ProcessState == nil {
			t.Fatalf("%s: %v", op, err)
		}
		return cmd.ProcessState, err
	}
	ps, err := stato("prova-uscita-" + strconv.Itoa(UscitaRifiuto))
	if e := esitoDelFiglio(err, ps, true, time.Second, LimitiDelFiglio.CPU); !errors.Is(e, ErrImmagine) || errors.Is(e, ErrRisorse) {
		t.Errorf("rifiuto alla scadenza: %v", e)
	}
	ps, err = stato("prova-uscita-2")
	if e := esitoDelFiglio(err, ps, true, time.Second, LimitiDelFiglio.CPU); CausaDi(e) != CausaMemoria {
		t.Errorf("uscita 2 alla scadenza: %v", e)
	}
	ps, err = stato("prova-uccidi")
	if e := esitoDelFiglio(err, ps, true, time.Second, LimitiDelFiglio.CPU); CausaDi(e) != CausaTempo || e.Error() != "imaging: elaborazione non riuscita adesso (tempo, oltre 1s)" {
		t.Errorf("ucciso alla scadenza: %v", e)
	}
	if e := esitoDelFiglio(errors.New("non partito"), nil, true, time.Second, LimitiDelFiglio.CPU); CausaDi(e) != CausaTempo {
		t.Errorf("senza stato alla scadenza: %v", e)
	}
}

// inventario: RIP-S-30
// Il limite di CPU che il figlio accetta dal padre (slice 037e): solo secondi interi da
// MinCPUFiglio a MaxCPUFiglio; nessun argomento vuol dire LimitiDelFiglio (8 s, la produzione).
// Altro, o più argomenti, è UscitaOperazione prima di darsi i limiti e di leggere la foto; per il
// padre è ErrRisorse con la causa «uscita», non un rifiuto. La memoria resta quella di
// LimitiDelFiglio.
func TestLimiteDiCPUDalPadre(t *testing.T) {
	if LimitiDelFiglio.CPU != 8 || MinCPUFiglio != 1 || MaxCPUFiglio != 60 {
		t.Errorf("limiti: produzione %d s, accettati %d…%d s", LimitiDelFiglio.CPU, MinCPUFiglio, MaxCPUFiglio)
	}
	for _, c := range []struct {
		args []string
		cpu  uint64
	}{{nil, 8}, {[]string{"1"}, 1}, {[]string{"8"}, 8}, {[]string{"30"}, 30}, {[]string{"60"}, 60}} {
		l, ok := limitiDagliArgomenti(c.args)
		if !ok || l.CPU != c.cpu || l.Memoria != LimitiDelFiglio.Memoria {
			t.Errorf("%q: %+v %v; attesi %d s", c.args, l, ok, c.cpu)
		}
	}
	for _, a := range [][]string{{"0"}, {"61"}, {"-1"}, {""}, {"1.5"}, {" 8"}, {"8s"}, {"+8"}, {"0x10"}, {"1_0"}, {"99999999999999999999"}, {"8", "8"}} {
		if l, ok := limitiDagliArgomenti(a); ok {
			t.Errorf("%q accettato: %+v", a, l)
		}
		// Nel processo dei test: esce prima di darsi i limiti.
		var out bytes.Buffer
		if c := Figlio(append([]string{OpNormalizza}, a...), bytes.NewReader(sintetiche.PNG(8, 8, 1)), &out); c != UscitaOperazione || out.Len() != 0 {
			t.Errorf("figlio con %q: uscita %d, %d byte", a, c, out.Len())
		}
	}
	for _, cpu := range []uint64{MaxCPUFiglio + 1, 1 << 40} {
		l := lavoratore(t)
		l.CPU = cpu
		if out, err := l.Normalizza(ctx, sintetiche.PNG(8, 8, 1)); !risorse(err, CausaUscita) || out != nil {
			t.Errorf("Lavoratore.CPU %d: %d byte, %v (causa %q)", cpu, len(out), err, CausaDi(err))
		}
	}
	l := lavoratore(t)
	l.CPU = MaxCPUFiglio
	if out, err := l.Normalizza(ctx, sintetiche.PNG(8, 8, 1)); err != nil || len(out) == 0 {
		t.Errorf("Lavoratore.CPU %d: %v", MaxCPUFiglio, err)
	}
}

// inventario: RIP-S-30
// Il codice di produzione del pacchetto non legge l'ambiente (slice 037e): il limite di CPU del
// figlio, il suo tempo e la sua memoria non si cambiano con una variabile. GESTRIP_TEST_CPU_FIGLIO
// la leggono solo i test.
func TestLimitiSenzaAmbiente(t *testing.T) {
	file, err := filepath.Glob("*.go")
	if err != nil || len(file) == 0 {
		t.Fatal(file, err)
	}
	for _, f := range file {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{"Getenv", "LookupEnv", "Environ", "GESTRIP_"} {
			if bytes.Contains(b, []byte(v)) {
				t.Errorf("%s contiene %q", f, v)
			}
		}
	}
}
