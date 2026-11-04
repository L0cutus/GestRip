package printing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// CartellaComandi è dove il pacchetto cups-client di Debian mette lp, lpstat e lpoptions. Il
// percorso è assoluto: il PATH del processo non sceglie mai quale lp si lancia.
const CartellaComandi = "/usr/bin"

// I tempi massimi dei comandi. Un lp che non risponde in tempo si ferma (SIGKILL): il lavoro
// potrebbe essere già nella coda, e la pagina lo dice.
const (
	TempoElenco = 10 * time.Second
	TempoStampa = 30 * time.Second
	// MaxUscita è quanto si legge dell'uscita di un comando: un elenco di code o la riga dell'id.
	MaxUscita = 256 << 10
)

// ambiente è tutto l'ambiente dei comandi: l'uscita in inglese fisso (LC_ALL=C) e un PATH minimo
// per gli eventuali programmi che lp lancia. Niente HOME: le opzioni personali di ~/.cups/lpoptions
// non contano.
var ambiente = []string{"LC_ALL=C", "LANG=C", "PATH=/usr/bin:/bin"}

// CUPS è la Printer con i comandi di CUPS. Lp, Lpstat e Lpoptions sono i percorsi assoluti dei
// comandi (NuovoCUPS); i test e il banco li fanno puntare a programmi finti.
type CUPS struct {
	Lp, Lpstat, Lpoptions    string
	TempoElenco, TempoStampa time.Duration
}

// NuovoCUPS usa i comandi della cartella data (vuota: CartellaComandi).
func NuovoCUPS(cartella string) *CUPS {
	if cartella == "" {
		cartella = CartellaComandi
	}
	return &CUPS{Lp: filepath.Join(cartella, "lp"), Lpstat: filepath.Join(cartella, "lpstat"),
		Lpoptions: filepath.Join(cartella, "lpoptions"), TempoElenco: TempoElenco, TempoStampa: TempoStampa}
}

// uscitaLimitata tiene al più MaxUscita byte e scarta il resto.
type uscitaLimitata struct{ bytes.Buffer }

func (u *uscitaLimitata) Write(p []byte) (int, error) {
	if resta := MaxUscita - u.Len(); resta > 0 {
		u.Buffer.Write(p[:min(len(p), resta)])
	}
	return len(p), nil
}

// esegui lancia il comando senza shell, con l'ambiente fisso, lo standard input dato e il tempo
// massimo. Gli errori: ErrNonDisponibile se il comando non c'è o non parte, ErrTempoScaduto se il
// tempo finisce; un'uscita diversa da 0 dà *exec.ExitError (con lo standard error in errore).
func esegui(ctx context.Context, tempo time.Duration, percorso string, stdin []byte, args ...string) (uscita, errore string, err error) {
	ctx, ferma := context.WithTimeout(ctx, tempo)
	defer ferma()
	c := exec.CommandContext(ctx, percorso, args...)
	c.Env, c.Dir = ambiente, "/"
	inGruppo(c)
	c.WaitDelay = 2 * time.Second // un figlio che tiene aperta l'uscita non blocca
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	var o, e uscitaLimitata
	c.Stdout, c.Stderr = &o, &e
	err = c.Run()
	uscita, errore = o.String(), e.String()
	var fine *exec.ExitError
	switch {
	case err == nil:
		return uscita, errore, nil
	case ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		return uscita, errore, ErrTempoScaduto
	case ctx.Err() != nil:
		return uscita, errore, ctx.Err()
	case errors.As(err, &fine):
		return uscita, errore, err
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return uscita, errore, fmt.Errorf("%w: %s non si avvia", ErrNonDisponibile, filepath.Base(percorso))
	}
	return uscita, errore, fmt.Errorf("%w: %w", ErrNonDisponibile, err)
}

// MotivoPulito è la prima riga non vuota del messaggio di CUPS, senza caratteri di controllo, al più
// 200 caratteri: si mostra sulla pagina e va nel registro.
func MotivoPulito(s string) string {
	for _, riga := range strings.Split(s, "\n") {
		riga = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || r == unicode.ReplacementChar {
				return -1
			}
			return r
		}, riga)
		if riga = strings.TrimSpace(riga); riga != "" {
			if r := []rune(riga); len(r) > 200 {
				riga = string(r[:200]) + "…"
			}
			return riga
		}
	}
	return ""
}

// List è lpstat -e: un nome di coda per riga. Le righe con un nome che il Go non accetta
// (CodaValida) si tengono lo stesso, così la pagina le può mostrare come non sceglibili. Senza
// code («No destinations added», anche con un'uscita diversa da 0) l'elenco è vuoto.
func (c *CUPS) List(ctx context.Context) ([]Queue, error) {
	out, errore, err := esegui(ctx, c.TempoElenco, c.Lpstat, nil, "-e")
	var fine *exec.ExitError
	if errors.As(err, &fine) {
		if strings.Contains(errore+out, "No destinations added") {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrNonDisponibile, MotivoPulito(errore))
	}
	if err != nil {
		return nil, err
	}
	var qq []Queue
	for _, riga := range strings.Split(out, "\n") {
		if riga = strings.TrimSpace(riga); riga != "" {
			qq = append(qq, Queue{Nome: riga})
		}
	}
	return qq, nil
}

// reRichiesta è la risposta di lp a un lavoro accettato: «request id is Coda-12 (0 file(s))»
// (lp.c di CUPS, in inglese con LC_ALL=C). L'id è il nome della coda, «-» e il numero.
var reRichiesta = regexp.MustCompile(`(?m)^request id is ([A-Za-z0-9][A-Za-z0-9_.@-]{0,126}-[0-9]{1,10}) \([0-9]+ file\(s\)\)\s*$`)

// Print è un lavoro di lp con il PDF sullo standard input. A buon fine solo se lp esce con 0 e
// risponde con l'id; un'uscita diversa da 0 è un Rifiuto con il messaggio di CUPS; un'uscita 0
// senza la riga dell'id è un Rifiuto (non si sa se il lavoro c'è: meglio non contarlo).
func (c *CUPS) Print(ctx context.Context, queue string, pdf []byte, opt Options) (JobID, error) {
	args, err := argomentiLp(queue, opt)
	if err != nil {
		return "", err
	}
	if len(pdf) == 0 {
		return "", errors.New("documento vuoto")
	}
	out, errore, err := esegui(ctx, c.TempoStampa, c.Lp, pdf, args...)
	var fine *exec.ExitError
	if errors.As(err, &fine) {
		return "", Rifiuto{Motivo: MotivoPulito(errore + "\n" + out)}
	}
	if err != nil {
		return "", err
	}
	m := reRichiesta.FindStringSubmatch(out)
	if m == nil {
		return "", Rifiuto{Motivo: "risposta di lp senza id del lavoro: " + MotivoPulito(out+"\n"+errore)}
	}
	return JobID(m[1]), nil
}

// Opzioni è lpoptions -p <coda> -l: una riga per opzione, «Chiave/Nome: scelta *predefinita scelta».
func (c *CUPS) Opzioni(ctx context.Context, queue string) ([]OpzioneCoda, error) {
	if !CodaValida(queue) {
		return nil, ErrCodaNonValida
	}
	out, errore, err := esegui(ctx, c.TempoElenco, c.Lpoptions, nil, "-p", queue, "-l")
	var fine *exec.ExitError
	if errors.As(err, &fine) {
		return nil, fmt.Errorf("%w: %s", ErrNonDisponibile, MotivoPulito(errore))
	}
	if err != nil {
		return nil, err
	}
	return LeggiLpoptions(out), nil
}

// LeggiLpoptions legge l'uscita di lpoptions -l. Una riga senza «:» o con una chiave non valida si
// salta.
func LeggiLpoptions(s string) []OpzioneCoda {
	var out []OpzioneCoda
	for _, riga := range strings.Split(s, "\n") {
		testa, scelte, ok := strings.Cut(riga, ":")
		if !ok {
			continue
		}
		k, nome, _ := strings.Cut(strings.TrimSpace(testa), "/")
		if !reChiave.MatchString(k) {
			continue
		}
		o := OpzioneCoda{Chiave: k, Nome: nome}
		for _, x := range strings.Fields(scelte) {
			if v, ok := strings.CutPrefix(x, "*"); ok {
				o.Predefinita, x = v, v
			}
			o.Scelte = append(o.Scelte, x)
		}
		out = append(out, o)
	}
	return out
}
