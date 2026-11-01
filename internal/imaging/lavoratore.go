package imaging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"time"
)

// La conversione gira in un processo figlio dello stesso binario (`gestrip image-worker`), con un
// limite di memoria e di tempo, come gli script Python dello standalone (`timeout --signal=KILL
// 10s python3 -I …`, con RLIMIT_AS di 512 MiB e RLIMIT_CPU di 8 s): se una foto malevola fa
// esplodere la memoria, cade il figlio, non il negozio (tema H, PIANO §2.2).

// ComandoFiglio è il comando di gestrip che fa da figlio.
const ComandoFiglio = "image-worker"

// Le operazioni del figlio.
const (
	OpNormalizza = "normalizza"
	OpMiniatura  = "miniatura"
)

// TempoMassimo è quanto il padre aspetta il figlio prima di ucciderlo (timeout 10s dello
// standalone).
const TempoMassimo = 10 * time.Second

// Limiti sono i limiti che il figlio si dà prima di leggere la foto.
type Limiti struct {
	// Memoria in byte, oltre ai dati che il processo ha già all'avvio (RLIMIT_DATA: in Go
	// RLIMIT_AS non si può usare, il runtime riserva molto più spazio d'indirizzi di quanto usa).
	Memoria uint64
	// CPU in secondi (RLIMIT_CPU).
	CPU uint64
}

// LimitiDelFiglio: 8 s di CPU come gli script dello standalone; di memoria 1440 MiB oltre l'avvio
// invece dei 512 MiB di spazio d'indirizzi dello script, perché si accettino le stesse foto. I
// decoder di Go tengono più memoria di Pillow (il JPEG progressivo tiene i coefficienti a 32 bit di
// tutta la foto, il PNG a 16 bit 8 byte per pixel): misurato con le foto più pesanti che lo script
// accetta nell'immagine dello standalone, il JPEG progressivo CMYK 8640×6480 ne vuole 1312, il
// progressivo 4:4:4 da 60 MP 875, il PNG RGB a 16 bit da 59,5 MP 544. 1440 è quel massimo con un
// margine di circa il 10% (slice 036, review).
var LimitiDelFiglio = Limiti{Memoria: 1440 * 1024 * 1024, CPU: 8}

// MaxFigli è il massimo di processi delle immagini che girano insieme in tutto il processo di
// gestrip (domanda 13, Stefano 01/11/2026): normalizzazione e miniature, dalle pagine e dal
// riscaldatore, di qualunque Lavoratore. Con il limite di memoria di ognuno (LimitiDelFiglio) il
// caso peggiore è due volte la memoria dell'avvio più 1440 MiB; alla T5 si misura sull'apparecchio.
const MaxFigli = 2

// posti sono i posti dei figli: uno occupato per ogni figlio che gira, per tutto il processo.
var posti = make(chan struct{}, MaxFigli)

// eseguiFiglio fa partire il figlio e lo aspetta; i test lo sostituiscono per contare i figli.
var eseguiFiglio = func(cmd *exec.Cmd) error { return cmd.Run() }

// prendiPosto aspetta un posto libero finché il contesto vale: se la richiesta se ne va, smette di
// aspettare e dà l'errore del contesto. Un contesto già finito non prende il posto nemmeno se c'è.
// Il posto si rende con lasciaPosto.
func prendiPosto(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case posti <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func lasciaPosto() { <-posti }

// Lavoratore fa girare le conversioni nel processo figlio.
type Lavoratore struct {
	// Eseguibile è il binario di gestrip (os.Executable).
	Eseguibile string
	// Tempo è l'attesa massima; 0 vuol dire TempoMassimo.
	Tempo time.Duration
}

// NuovoLavoratore dà il lavoratore sul binario che gira.
func NuovoLavoratore() (*Lavoratore, error) {
	e, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("imaging: binario del lavoratore: %w", err)
	}
	return &Lavoratore{Eseguibile: e}, nil
}

// Normalizza è Normalizza nel figlio: ErrImmagine per ogni rifiuto e per un figlio caduto o fermato
// dopo il suo tempo; l'errore del contesto se la richiesta è stata annullata.
func (l *Lavoratore) Normalizza(ctx context.Context, in []byte) ([]byte, error) {
	return l.esegui(ctx, OpNormalizza, in, MaxByteIngresso, MaxByteUscita)
}

// Miniatura è Miniatura nel figlio, con gli stessi errori di Normalizza.
func (l *Lavoratore) Miniatura(ctx context.Context, in []byte) ([]byte, error) {
	return l.esegui(ctx, OpMiniatura, in, MaxByteUscita, MaxByteMiniatura)
}

// esegui manda in al figlio sullo standard input e ne prende lo standard output, al più maxOut
// byte, quando c'è un posto (MaxFigli). Il figlio parte con un ambiente vuoto (niente dell'ambiente del negozio, segreti compresi),
// senza standard error (un panic o un «out of memory» non finiscono nel registro) e, su Linux,
// muore con il padre.
func (l *Lavoratore) esegui(ctx context.Context, op string, in []byte, maxIn, maxOut int) ([]byte, error) {
	if len(in) == 0 || len(in) > maxIn {
		return nil, ErrImmagine
	}
	// Il tempo del figlio parte quando ha il posto: l'attesa la limita solo il contesto.
	if err := prendiPosto(ctx); err != nil {
		return nil, err
	}
	defer lasciaPosto()
	tempo := l.Tempo
	if tempo == 0 {
		tempo = TempoMassimo
	}
	c, annulla := context.WithTimeout(ctx, tempo)
	defer annulla()
	cmd := exec.CommandContext(c, l.Eseguibile, ComandoFiglio, op)
	cmd.Env = []string{}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(in)
	out := &limitato{max: maxOut}
	cmd.Stdout = out
	cmd.SysProcAttr = attributiFiglio()
	cmd.WaitDelay = time.Second
	// Pdeathsig vale per il thread che ha creato il figlio, non per il processo: se quel thread
	// finisse (il runtime di Go può chiuderne), il figlio riceverebbe SIGKILL a metà lavoro. Il
	// goroutine resta sullo stesso thread da Start a Wait.
	runtime.LockOSThread()
	err := eseguiFiglio(cmd)
	runtime.UnlockOSThread()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if err != nil || out.troppo || out.b.Len() == 0 {
		return nil, ErrImmagine
	}
	return out.b.Bytes(), nil
}

// limitato tiene al più max byte; oltre, rifiuta la scrittura e lo ricorda.
type limitato struct {
	b      bytes.Buffer
	max    int
	troppo bool
}

func (w *limitato) Write(p []byte) (int, error) {
	if w.troppo || w.b.Len()+len(p) > w.max {
		w.troppo = true
		return 0, errTroppo
	}
	return w.b.Write(p)
}

var errTroppo = errors.New("imaging: risultato oltre il massimo")

// Figlio è `gestrip image-worker <operazione>`: si dà i limiti, legge la foto da in (al più il
// massimo dell'operazione), scrive il risultato su out e dà il codice d'uscita: 0 solo se ha
// scritto un risultato, 1 per un rifiuto, 2 per un'operazione sconosciuta.
func Figlio(args []string, in io.Reader, out io.Writer) int {
	if len(args) != 1 {
		return 2
	}
	var f func([]byte) ([]byte, error)
	var maxIn int
	switch args[0] {
	case OpNormalizza:
		f, maxIn = Normalizza, MaxByteIngresso
	case OpMiniatura:
		f, maxIn = Miniatura, MaxByteUscita
	default:
		return 2
	}
	if err := preparaFiglio(); err != nil {
		return 1
	}
	dati, err := io.ReadAll(io.LimitReader(in, int64(maxIn)+1))
	if err != nil || len(dati) > maxIn {
		return 1
	}
	r, err := f(dati)
	if err != nil {
		return 1
	}
	if _, err := out.Write(r); err != nil {
		return 1
	}
	return 0
}

// preparaFiglio dà al figlio LimitiDelFiglio e un tetto morbido alla memoria del runtime un po'
// sotto, così la raccolta lavora prima di arrivare al limite vero.
func preparaFiglio() error {
	if err := impostaLimiti(LimitiDelFiglio); err != nil {
		return err
	}
	debug.SetMemoryLimit(int64(LimitiDelFiglio.Memoria) - 32<<20)
	return nil
}
