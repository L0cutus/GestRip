package main

import (
	"bytes"
	"context"
	"debug/elf"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/imaging/sintetiche"
)

// inventario: RIP-S-30
// gestrip image-worker è il figlio delle foto: con il binario intero (tutti i pacchetti di gestrip
// caricati, quindi i dati dell'avvio più grandi) normalizza anche le foto più grandi che si
// accettano dentro i limiti, e fa la miniatura. Un'operazione sconosciuta esce con 2.
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
	if _, err := l.Normalizza(ctx, []byte("not an image")); !errors.Is(err, imaging.ErrImmagine) {
		t.Errorf("byte a caso: %v", err)
	}
	if !testing.Short() {
		for _, g := range [][]byte{
			sintetiche.JPEG(sintetiche.Liscio(8944, 6708), sintetiche.OpzioniJPEG{}),
			sintetiche.JPEGPiatto(8640, 6480, sintetiche.OpzioniJPEGPiatto{Progressivo: true, Componenti: 4}),
			sintetiche.PNGRighe(7000, 8500, 2, 16, sintetiche.Sfumata),
		} {
			if _, err := l.Normalizza(ctx, g); err != nil {
				t.Errorf("foto grande nel binario intero: %v", err)
			}
		}
	}
	cmd := exec.Command(os.Args[0], imaging.ComandoFiglio, "altro")
	cmd.Stdin = bytes.NewReader(in)
	var ee *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &ee) || ee.ExitCode() != 2 {
		t.Errorf("operazione sconosciuta: %v", err)
	}
}

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
