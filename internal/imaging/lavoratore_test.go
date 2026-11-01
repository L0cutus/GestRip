package imaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// TestMain: con «image-worker …» come argomenti il binario dei test fa da gestrip image-worker,
// così il lavoratore lo avvia come figlio. Le operazioni «prova-…» ci sono solo qui: provano i
// limiti e l'ambiente del figlio.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == ComandoFiglio && strings.HasPrefix(os.Args[2], "prova-") {
		os.Exit(figlioDiProva(os.Args[2]))
	}
	if len(os.Args) >= 2 && os.Args[1] == ComandoFiglio {
		os.Exit(Figlio(os.Args[2:], os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

var tieni []byte

func figlioDiProva(op string) int {
	// prova-figlio-N: il Figlio vero, con un limite di memoria di N MiB al posto di quello di
	// LimitiDelFiglio (misura del limite e prova che Figlio si dà i limiti).
	if n, ok := strings.CutPrefix(op, "prova-figlio-"); ok {
		m, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			return 3
		}
		LimitiDelFiglio.Memoria = m << 20
		return Figlio([]string{OpNormalizza}, os.Stdin, os.Stdout)
	}
	switch op {
	case "prova-limiti":
		if err := preparaFiglio(); err != nil {
			return 1
		}
		l, err := os.ReadFile("/proc/self/limits")
		if err != nil {
			return 1
		}
		_, _ = os.Stdout.Write(l) // un errore si vede dal padre: niente uscita
		return 0
	case "prova-memoria":
		if err := preparaFiglio(); err != nil {
			return 1
		}
		tieni = make([]byte, LimitiDelFiglio.Memoria+128<<20)
		for i := 0; i < len(tieni); i += 4096 {
			tieni[i] = 1
		}
		fmt.Print("sopravvissuto")
		return 0
	case "prova-cpu":
		if err := impostaLimiti(Limiti{Memoria: LimitiDelFiglio.Memoria, CPU: 1}); err != nil {
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
	}
	return 2
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
func TestFiglioConILimiti(t *testing.T) {
	soloLinux(t)
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
// Oltre la memoria il figlio cade e il padre dice solo ErrImmagine; il padre resta su.
func TestFiglioOltreLaMemoria(t *testing.T) {
	soloLinux(t)
	out, err := lavoratore(t).esegui(ctx, "prova-memoria", []byte("x"), 1, 1<<20)
	if !errors.Is(err, ErrImmagine) || len(out) != 0 {
		t.Fatalf("%q, %v; atteso ErrImmagine", out, err)
	}
}

// inventario: RIP-S-30
// Oltre il tempo di CPU il figlio cade (qui con un limite di 1 s, per fare in fretta).
func TestFiglioOltreLaCPU(t *testing.T) {
	soloLinux(t)
	inizio := time.Now()
	out, err := lavoratore(t).esegui(ctx, "prova-cpu", []byte("x"), 1, 1<<20)
	if !errors.Is(err, ErrImmagine) || len(out) != 0 {
		t.Fatalf("%q, %v; atteso ErrImmagine", out, err)
	}
	if d := time.Since(inizio); d > 8*time.Second {
		t.Errorf("il figlio è caduto dopo %v", d)
	}
}

// inventario: RIP-S-30
// Il padre uccide il figlio dopo il suo tempo (10 s, qui 300 ms).
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
	if !errors.Is(err, ErrImmagine) || len(out) != 0 {
		t.Fatalf("%q, %v; atteso ErrImmagine", out, err)
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
// Il figlio vero: un'operazione sconosciuta o mancante, o più argomenti, non fanno niente.
func TestFiglioOperazioneSconosciuta(t *testing.T) {
	for _, args := range [][]string{nil, {"altro"}, {OpNormalizza, "di più"}} {
		var out bytes.Buffer
		if c := Figlio(args, bytes.NewReader(sintetiche.PNG(8, 8, 1)), &out); c == 0 || out.Len() != 0 {
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
	l := lavoratore(t)
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
			t.Errorf("%s (%d byte): %v dopo %v", c.cosa, len(c.in), err, time.Since(inizio))
			continue
		}
		i, _ := sintetiche.LeggiJPEG(out)
		if i.Larghezza != c.l || i.Altezza != c.a {
			t.Errorf("%s: %d×%d, attese %d×%d", c.cosa, i.Larghezza, i.Altezza, c.l, c.a)
		}
		t.Logf("%s: %v", c.cosa, time.Since(inizio))
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
	l := lavoratore(t)
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
			t.Errorf("%s: %v dopo %v", c.cosa, err, time.Since(inizio))
		}
	}
}

// inventario: RIP-S-30
// Il Figlio vero si dà i limiti: con 256 MiB al posto di quelli di LimitiDelFiglio il JPEG
// progressivo 4:4:4 da 60 MP (che ne vuole circa 875) è rifiutato, e una foto piccola passa.
func TestFiglioVeroSiDaILimiti(t *testing.T) {
	soloLinux(t)
	l := lavoratore(t)
	grande := sintetiche.JPEGPiatto(8944, 6708, sintetiche.OpzioniJPEGPiatto{Progressivo: true})
	if out, err := l.esegui(ctx, "prova-figlio-256", grande, MaxByteIngresso, MaxByteUscita); !errors.Is(err, ErrImmagine) || out != nil {
		t.Errorf("grande con 256 MiB: %d byte, %v", len(out), err)
	}
	if out, err := l.esegui(ctx, "prova-figlio-256", sintetiche.PNG(64, 64, 1), MaxByteIngresso, MaxByteUscita); err != nil || len(out) == 0 {
		t.Errorf("piccola con 256 MiB: %v", err)
	}
}

// inventario: RIP-S-30
// Su Linux il figlio muore con il padre (Pdeathsig SIGKILL). Che il segnale arrivi davvero alla
// morte del padre non si prova qui senza fragilità (servirebbe un processo nonno): lo dichiara la
// ricevuta.
func TestFiglioMuoreConIlPadre(t *testing.T) {
	soloLinux(t)
	if a := attributiFiglio(); a == nil || a.Pdeathsig != syscall.SIGKILL {
		t.Errorf("attributi del figlio: %+v", a)
	}
}
