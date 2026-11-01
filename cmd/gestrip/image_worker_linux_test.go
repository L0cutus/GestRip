package main

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"image/jpeg"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// inventario: RIP-S-30
// gestrip image-worker è il figlio delle foto: con il binario intero (tutti i pacchetti di gestrip
// caricati, quindi i dati dell'avvio più grandi) normalizza anche le foto più grandi che si
// accettano dentro i limiti (un JPEG con le dimensioni giuste), e fa la miniatura. Byte a caso sono un rifiuto (ErrImmagine), non un
// errore di risorse. Un'operazione sconosciuta esce con imaging.UscitaOperazione.
//
// Le foto grandi provano i limiti di memoria e di CPU del figlio, non i 10 s di orologio
// (TempoMassimo, provato in internal/imaging): il loro figlio ha tempoFotoGrandi, perché
// l'orologio dipende dal carico della macchina. Misurato nella slice 037c: la CI del commit
// 8f57fa1 è caduta qui perché il JPEG progressivo CMYK, 2,5 s di CPU, con la CPU divisa fra i
// pacchetti dei test ha superato i 10 s di orologio ed è stato ucciso dal padre; il limite dei dati
// non c'entrava. Un errore dice la sua causa (rifiuto, tempo, cpu, memoria, …).
// Nella CI (GESTRIP_TEST_CPU_FIGLIO, slice 037e) il figlio delle foto grandi ha anche un limite di
// CPU più alto: sul runner di GitHub lo stesso JPEG è stato ucciso al limite di 8 s di CPU.
func TestImageWorkerConIlBinarioIntero(t *testing.T) {
	l := &imaging.Lavoratore{Eseguibile: os.Args[0]}
	ctx := context.Background()
	in := sintetiche.JPEG(sintetiche.Gradiente(800, 600, 1), sintetiche.OpzioniJPEG{Orientamento: 6})
	out, err := l.Normalizza(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := sintetiche.LeggiJPEG(out); !ok || i.Larghezza != 600 || i.Altezza != 800 || i.EXIF {
		t.Errorf("normalizzata: %+v", i)
	}
	m, err := l.Miniatura(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	if lm, am, ok := sintetiche.DimensioniWebP(m); !ok || lm != 300 || am != 400 {
		t.Errorf("miniatura %d×%d %v", lm, am, ok)
	}
	if _, err := l.Normalizza(ctx, []byte("not an image")); !errors.Is(err, imaging.ErrImmagine) || errors.Is(err, imaging.ErrRisorse) {
		t.Errorf("byte a caso: %v", err)
	}
	if !testing.Short() {
		lg := &imaging.Lavoratore{Eseguibile: os.Args[0], Tempo: tempoFotoGrandi, CPU: cpuFiglioDeiTest(t)}
		for _, g := range []struct {
			cosa string
			in   []byte
			l, a int
		}{
			{"JPEG 8944×6708", sintetiche.JPEG(sintetiche.Liscio(8944, 6708), sintetiche.OpzioniJPEG{}), 4096, 3072},
			{"progressivo CMYK 8640×6480", sintetiche.JPEGPiatto(8640, 6480, sintetiche.OpzioniJPEGPiatto{Progressivo: true, Componenti: 4}), 4096, 3072},
			{"PNG RGB 16 bit 7000×8500", sintetiche.PNGRighe(7000, 8500, 2, 16, sintetiche.Sfumata), 3373, 4096},
		} {
			inizio := time.Now()
			out, err := lg.Normalizza(ctx, g.in)
			if err != nil {
				t.Errorf("%s nel binario intero: %v (causa %q) dopo %v", g.cosa, err, imaging.CausaDi(err), time.Since(inizio))
				continue
			}
			// Decodificata tutta, non solo l'intestazione: un risultato tronco non passa.
			m, err := jpeg.Decode(bytes.NewReader(out))
			if i, ok := sintetiche.LeggiJPEG(out); err != nil || !ok || i.EXIF || m.Bounds().Dx() != g.l || m.Bounds().Dy() != g.a {
				t.Errorf("%s nel binario intero: %d byte, %v; atteso un JPEG %d×%d senza EXIF", g.cosa, len(out), err, g.l, g.a)
			}
		}
	}
	cmd := exec.Command(os.Args[0], imaging.ComandoFiglio, "altro")
	cmd.Stdin = bytes.NewReader(in)
	var ee *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &ee) || ee.ExitCode() != imaging.UscitaOperazione {
		t.Errorf("operazione sconosciuta: %v", err)
	}
}

// cpuFiglioDeiTest è GESTRIP_TEST_CPU_FIGLIO, in secondi, o 0 (il limite di produzione) se non
// c'è: il limite di CPU del figlio delle foto grandi nella CI (slice 037e; il perché è in
// internal/imaging, cpuFiglioDeiTest). La legge solo il codice dei test.
func cpuFiglioDeiTest(t *testing.T) uint64 {
	t.Helper()
	v, ok := os.LookupEnv("GESTRIP_TEST_CPU_FIGLIO")
	if !ok {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil || n < imaging.MinCPUFiglio || n > imaging.MaxCPUFiglio {
		t.Fatalf("GESTRIP_TEST_CPU_FIGLIO=%q: attesi secondi interi fra %d e %d", v, imaging.MinCPUFiglio, imaging.MaxCPUFiglio)
	}
	return n
}

// tempoFotoGrandi è il tempo del figlio per le foto grandi: due minuti bastano anche con un
// ventesimo di CPU (misurato: 10,1 s di orologio con un quarto di CPU per la foto più lenta).
const tempoFotoGrandi = 2 * time.Minute

// inventario: RIP-S-30
// Il binario è statico (tema H, PIANO §1.3): niente interprete dinamico, quindi né libc né
// libwebp di sistema, nemmeno con gen2brain/webp (che senza -tags nodynamic lo farebbe dinamico).
func TestBinarioStatico(t *testing.T) {
	f, err := elf.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }() // in sola lettura
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP || p.Type == elf.PT_DYNAMIC {
			t.Fatalf("binario dinamico (%v)", p.Type)
		}
	}
}
