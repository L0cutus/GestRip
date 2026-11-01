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
	"strconv"
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
// standalone). È tempo di orologio, quindi dipende dal carico della macchina: misurato (slice 037c)
// il JPEG progressivo CMYK 8640×6480, la foto accettata più pesante, vuole 2,5 s di CPU e 2,4 s di
// orologio da solo, 10,1 s con un quarto di CPU. Un figlio fermato dopo il tempo è ErrRisorse
// (CausaTempo), non un rifiuto.
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
// Rimisurato nella slice 037c per bisezione con il Figlio vero: il CMYK passa con 1309–1315 MiB,
// uguale con 24 o 4 processori e con un quarto di CPU; sotto, il figlio esce con 2 (memoria).
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

// Normalizza è Normalizza nel figlio: ErrImmagine per ogni rifiuto (anche un risultato vuoto o
// oltre il massimo); ErrRisorse, con la causa, per un figlio fermato dopo il suo tempo, ucciso, senza
// memoria o che non parte (slice 037c); l'errore del contesto se la richiesta è stata annullata.
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
	if out.troppo {
		return nil, ErrImmagine
	}
	// Il tempo è passato se il contesto del figlio è scaduto mentre quello del chiamante vale ancora
	// (un annullamento del chiamante è già uscito sopra, con il suo errore).
	scaduto := errors.Is(c.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	if err := esitoDelFiglio(err, cmd.ProcessState, scaduto, tempo); err != nil {
		return nil, err
	}
	if out.b.Len() == 0 {
		return nil, ErrImmagine
	}
	return out.b.Bytes(), nil
}

// ErrRisorse è una foto che il figlio non ha elaborato adesso senza averla rifiutata (slice 037c):
// fermato dopo il suo tempo, ucciso da un segnale (anche al limite di CPU), memoria negata, figlio
// che non parte o che esce fuori dal protocollo. Riprovare può riuscire. L'errore vero è un
// *ErroreRisorse con la causa per il registro (CausaDi). È una differenza dichiarata: nello
// standalone il timeout e i limiti dello script escono come un rifiuto della foto.
var ErrRisorse = errors.New("imaging: elaborazione non riuscita adesso")

// Causa è perché il figlio non ha dato un risultato quando non è un rifiuto della foto.
type Causa string

// Le cause di ErrRisorse. Nessuna dice niente della foto.
const (
	// CausaTempo: il padre ha fermato il figlio dopo il suo tempo (Lavoratore.Tempo, TempoMassimo).
	CausaTempo Causa = "tempo"
	// CausaCPU: il kernel ha ucciso il figlio al limite di CPU (RLIMIT_CPU di LimitiDelFiglio).
	CausaCPU Causa = "cpu"
	// CausaSegnale: il figlio è morto per un altro segnale (per esempio il killer della memoria del
	// kernel).
	CausaSegnale Causa = "segnale"
	// CausaMemoria: il figlio è uscito con 2, l'uscita del runtime di Go per un errore fatale. Di
	// solito è l'allocazione negata dal limite dei dati («fatal error: out of memory»): i panic
	// della conversione sono rifiuti (converti). Ma non sempre: anche un altro errore fatale del
	// runtime (un thread che non si crea, un panic in un'altra goroutine) esce con 2.
	CausaMemoria Causa = "memoria"
	// CausaAvvio: il figlio non è partito, o non si è dato i limiti (UscitaSenzaLimiti).
	CausaAvvio Causa = "avvio"
	// CausaUscita: un codice d'uscita fuori dal protocollo, o un errore dopo un'uscita 0.
	CausaUscita Causa = "uscita"
)

// ErroreRisorse è ErrRisorse con la sua causa. Dettaglio è il segnale, il codice d'uscita o il
// tempo, mai niente della foto né un percorso: l'errore si può scrivere nel registro.
type ErroreRisorse struct {
	Causa     Causa
	Dettaglio string
}

func (e *ErroreRisorse) Error() string {
	if e.Dettaglio == "" {
		return ErrRisorse.Error() + " (" + string(e.Causa) + ")"
	}
	return ErrRisorse.Error() + " (" + string(e.Causa) + ", " + e.Dettaglio + ")"
}

// Is fa valere errors.Is(err, ErrRisorse).
func (e *ErroreRisorse) Is(t error) bool { return t == ErrRisorse }

// CausaDi è la causa di un ErrRisorse dentro err, o "" se err non lo è (un rifiuto, il contesto).
func CausaDi(err error) Causa {
	var e *ErroreRisorse
	if errors.As(err, &e) {
		return e.Causa
	}
	return ""
}

// I codici d'uscita del figlio (slice 037c). Sono diversi da 1 (un'uscita generica) e da 2,
// l'uscita del runtime di Go per un errore fatale e per un panic: così il padre distingue un rifiuto
// dalla memoria negata senza leggere lo standard error.
const (
	// UscitaRiuscita: il risultato è sullo standard output.
	UscitaRiuscita = 0
	// UscitaRifiuto: la foto è rifiutata (ErrImmagine).
	UscitaRifiuto = 3
	// UscitaSenzaLimiti: il figlio non si è potuto dare i limiti e non ha letto la foto.
	UscitaSenzaLimiti = 4
	// UscitaOperazione: operazione sconosciuta o argomenti sbagliati.
	UscitaOperazione = 5
)

// uscitaRuntime è l'uscita del runtime di Go per un errore fatale (anche «out of memory»).
const uscitaRuntime = 2

// esitoDelFiglio dà l'errore del figlio finito con err e lo stato ps: nil se è uscito con 0,
// ErrImmagine per UscitaRifiuto, altrimenti un *ErroreRisorse. scaduto dice che il tempo del figlio
// è passato. Un figlio uscito da sé con un codice ha la precedenza sul tempo (un rifiuto arrivato
// alla scadenza resta un rifiuto); il tempo spiega solo un figlio ucciso o senza stato.
func esitoDelFiglio(err error, ps *os.ProcessState, scaduto bool, tempo time.Duration) error {
	switch {
	case err == nil:
		return nil
	case ps != nil && ps.Exited():
		return esitoDellUscita(ps.ExitCode())
	case scaduto:
		return &ErroreRisorse{Causa: CausaTempo, Dettaglio: "oltre " + tempo.String()}
	case ps == nil:
		// Il dettaglio è fisso: l'errore di exec nomina il percorso dell'eseguibile.
		return &ErroreRisorse{Causa: CausaAvvio, Dettaglio: "non partito"}
	}
	if causa, segnale, ok := causaDelSegnale(ps); ok {
		return &ErroreRisorse{Causa: causa, Dettaglio: segnale}
	}
	return esitoDellUscita(ps.ExitCode())
}

// esitoDellUscita è l'errore di un figlio uscito con il codice c (diverso da 0, o 0 con un errore
// dopo, per esempio nella lettura del risultato).
func esitoDellUscita(c int) error {
	switch c {
	case UscitaRifiuto:
		return ErrImmagine
	case uscitaRuntime:
		return &ErroreRisorse{Causa: CausaMemoria, Dettaglio: "uscita 2"}
	case UscitaSenzaLimiti:
		return &ErroreRisorse{Causa: CausaAvvio, Dettaglio: "limiti non messi"}
	default:
		return &ErroreRisorse{Causa: CausaUscita, Dettaglio: "uscita " + strconv.Itoa(c)}
	}
}

// converti è f sui dati, con un panic che diventa un rifiuto (come un'eccezione di Pillow nello
// standalone). Un errore fatale del runtime (la memoria negata) non è un panic: il figlio esce con 2.
func converti(f func([]byte) ([]byte, error), dati []byte) (r []byte, err error) {
	defer func() {
		if recover() != nil {
			r, err = nil, ErrImmagine
		}
	}()
	return f(dati)
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

// normalizzaNelFiglio è la conversione di OpNormalizza nel Figlio; i test la sostituiscono nel
// processo figlio per provare che un panic è un rifiuto.
var normalizzaNelFiglio = Normalizza

// Figlio è `gestrip image-worker <operazione>`: si dà i limiti, legge la foto da in (al più il
// massimo dell'operazione), scrive il risultato su out e dà il codice d'uscita: UscitaRiuscita solo
// se ha scritto un risultato, UscitaRifiuto per un rifiuto (anche un panic della conversione),
// UscitaSenzaLimiti se i limiti non si mettono, UscitaOperazione per un'operazione sconosciuta.
func Figlio(args []string, in io.Reader, out io.Writer) int {
	if len(args) != 1 {
		return UscitaOperazione
	}
	var f func([]byte) ([]byte, error)
	var maxIn int
	switch args[0] {
	case OpNormalizza:
		f, maxIn = normalizzaNelFiglio, MaxByteIngresso
	case OpMiniatura:
		f, maxIn = Miniatura, MaxByteUscita
	default:
		return UscitaOperazione
	}
	if err := preparaFiglio(); err != nil {
		return UscitaSenzaLimiti
	}
	dati, err := io.ReadAll(io.LimitReader(in, int64(maxIn)+1))
	if err != nil || len(dati) > maxIn {
		return UscitaRifiuto
	}
	r, err := converti(f, dati)
	if err != nil {
		return UscitaRifiuto
	}
	if _, err := out.Write(r); err != nil {
		return UscitaRifiuto // il padre ha smesso di leggere: un risultato oltre il massimo
	}
	return UscitaRiuscita
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
