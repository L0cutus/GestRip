package imaging

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// figliFinti sostituisce l'avvio del figlio: ogni figlio aspetta su via, e intanto si contano quanti
// girano insieme. Il risultato è un byte, così il lavoratore lo prende per buono.
type figliFinti struct {
	via             chan struct{}
	dentro, massimo atomic.Int32
	partiti         atomic.Int32
}

func conFigliFinti(t *testing.T) *figliFinti {
	t.Helper()
	f := &figliFinti{via: make(chan struct{})}
	prima := eseguiFiglio
	eseguiFiglio = func(cmd *exec.Cmd) error {
		f.partiti.Add(1)
		n := f.dentro.Add(1)
		for {
			m := f.massimo.Load()
			if n <= m || f.massimo.CompareAndSwap(m, n) {
				break
			}
		}
		<-f.via
		f.dentro.Add(-1)
		_, err := cmd.Stdout.Write([]byte{1})
		return err
	}
	t.Cleanup(func() { eseguiFiglio = prima })
	return f
}

// aspettaDentro aspetta che n figli girino insieme.
func (f *figliFinti) aspettaDentro(t *testing.T, n int32) {
	t.Helper()
	fine := time.Now().Add(5 * time.Second)
	for f.dentro.Load() != n {
		if time.Now().After(fine) {
			t.Fatalf("figli dentro %d, voluti %d", f.dentro.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// inventario: RIP-S-30
// Al più MaxFigli (2) processi delle immagini insieme in tutto il processo (domanda 13, Stefano
// 01/11/2026), normalizzazione e miniature insieme e anche da lavoratori diversi: gli altri
// aspettano il loro turno e poi partono tutti.
func TestAlPiuDueFigliInsieme(t *testing.T) {
	if MaxFigli != 2 {
		t.Fatalf("MaxFigli %d", MaxFigli)
	}
	f := conFigliFinti(t)
	var wg sync.WaitGroup
	errori := make(chan error, 6)
	for i := range 6 {
		l := &Lavoratore{Eseguibile: "/nessun/binario"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			if i%2 == 0 {
				_, err = l.Normalizza(context.Background(), []byte{1})
			} else {
				_, err = l.Miniatura(context.Background(), []byte{1})
			}
			errori <- err
		}()
	}
	f.aspettaDentro(t, 2)
	time.Sleep(50 * time.Millisecond) // un terzo che entrasse adesso si vedrebbe
	if n := f.partiti.Load(); n != 2 {
		t.Fatalf("partiti %d con due posti occupati", n)
	}
	close(f.via)
	finiti := make(chan struct{})
	go func() { wg.Wait(); close(finiti) }()
	select {
	case <-finiti:
	case <-time.After(10 * time.Second):
		t.Fatal("i figli in attesa non partono: posti non resi")
	}
	close(errori)
	for err := range errori {
		if err != nil {
			t.Error(err)
		}
	}
	if f.massimo.Load() != 2 || f.partiti.Load() != 6 {
		t.Errorf("massimo %d, partiti %d", f.massimo.Load(), f.partiti.Load())
	}
	if len(posti) != 0 {
		t.Errorf("posti rimasti occupati: %d", len(posti))
	}
}

// inventario: RIP-S-30
// Chi aspetta il posto rispetta il contesto: se il client se ne va smette di aspettare, dà l'errore
// del contesto e il suo figlio non parte; il posto liberato va a chi aspetta ancora. Anche un
// figlio che fallisce libera il posto.
func TestAttesaDelPostoRispettaIlContesto(t *testing.T) {
	f := conFigliFinti(t)
	l := &Lavoratore{Eseguibile: "/nessun/binario"}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = l.Normalizza(context.Background(), []byte{1}) // occupano i posti
		}()
	}
	f.aspettaDentro(t, 2)
	ctx, annulla := context.WithCancel(context.Background())
	fatto := make(chan error, 1)
	go func() {
		_, err := l.Miniatura(ctx, []byte{1})
		fatto <- err
	}()
	time.Sleep(20 * time.Millisecond)
	annulla()
	select {
	case err := <-fatto:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("errore %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("l'attesa non ha guardato il contesto")
	}
	if f.partiti.Load() != 2 {
		t.Errorf("partiti %d", f.partiti.Load())
	}
	// Un contesto già scaduto non prende il posto nemmeno se c'è.
	close(f.via)
	wg.Wait()
	scaduto, ferma := context.WithCancel(context.Background())
	ferma()
	for range 30 { // select sceglie a caso fra due casi pronti: più giri
		if _, err := l.Normalizza(scaduto, []byte{1}); !errors.Is(err, context.Canceled) {
			t.Fatalf("contesto scaduto: %v", err)
		}
	}
	if f.partiti.Load() != 2 {
		t.Errorf("partiti con il contesto scaduto: %d", f.partiti.Load()-2)
	}
	// Un figlio che fallisce libera il posto.
	eseguiFiglio = func(*exec.Cmd) error { return errors.New("caduto") }
	for range 3 {
		if _, err := l.Normalizza(context.Background(), []byte{1}); !errors.Is(err, ErrImmagine) {
			t.Errorf("figlio caduto: %v", err)
		}
	}
	if len(posti) != 0 {
		t.Errorf("posti rimasti occupati: %d", len(posti))
	}
}
