package imaging

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// impostaLimiti mette i limiti duri e morbidi uguali (come resource.setrlimit degli script): il
// figlio non può alzarli. Niente core dump. La memoria si conta sopra i dati che il processo ha già
// all'avvio (datiIniziali): il runtime di Go e il libwebp tradotto in Go ne tengono un centinaio di
// MiB prima di leggere la foto, mentre l'interprete Python dello standalone ne teneva pochi; così
// alla conversione resta tutta LimitiDelFiglio.Memoria.
func impostaLimiti(l Limiti) error {
	base, err := datiIniziali()
	if err != nil {
		return err
	}
	for _, r := range []struct {
		quale int
		v     uint64
	}{
		{unix.RLIMIT_DATA, base + l.Memoria},
		{unix.RLIMIT_CPU, l.CPU},
		{unix.RLIMIT_CORE, 0},
	} {
		if err := unix.Setrlimit(r.quale, &unix.Rlimit{Cur: r.v, Max: r.v}); err != nil {
			return err
		}
	}
	return nil
}

// datiIniziali è VmData di /proc/self/status, in byte: quello che RLIMIT_DATA conta.
func datiIniziali() (uint64, error) {
	s, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(s))
	for sc.Scan() {
		if v, ok := bytes.CutPrefix(sc.Bytes(), []byte("VmData:")); ok {
			c := bytes.Fields(v)
			if len(c) == 2 && string(c[1]) == "kB" {
				n, err := strconv.ParseUint(string(c[0]), 10, 64)
				return n * 1024, err
			}
		}
	}
	return 0, errors.New("imaging: VmData non trovato")
}

// attributiFiglio: il figlio riceve SIGKILL se il padre muore.
func attributiFiglio() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// causaDelSegnale dice se il figlio è morto per un segnale, quale, e perché: CausaCPU per SIGKILL
// dopo almeno LimitiDelFiglio.CPU secondi di CPU (con i limiti morbido e duro uguali il kernel, al
// limite, manda subito SIGKILL e non SIGXCPU, che il runtime di Go comunque ignora: misurato in
// TestFiglioOltreLaCPU), CausaSegnale per ogni altro (anche SIGKILL del killer della memoria del
// kernel). Il SIGKILL del tempo del padre lo ha già visto esitoDelFiglio.
// Il tempo di CPU che il padre legge (rusage) può stare un po' sotto quello che il kernel ha contato
// per il limite (misurato: 999,458 ms con un limite di 1 s): si accettano 100 ms in meno.
func causaDelSegnale(ps *os.ProcessState) (Causa, string, bool) {
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		return "", "", false
	}
	s := ws.Signal()
	limite := time.Duration(LimitiDelFiglio.CPU)*time.Second - 100*time.Millisecond
	if s == syscall.SIGKILL && ps.UserTime()+ps.SystemTime() >= limite {
		return CausaCPU, s.String(), true
	}
	return CausaSegnale, s.String(), true
}
