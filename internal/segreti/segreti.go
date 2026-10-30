// Package segreti tiene le regole sui segreti dell'apparecchio (OPS-13): si leggono solo da file
// privati, mai dalle variabili d'ambiente, e non escono dal processo con un dump.
//
// Oggi il Go non legge nessun file di segreti: la chiave del server sta nella tabella segreti del
// database (008b). LeggiFile è pronto per quelli che arrivano (il segreto Flask, PIANO 2.4).
package segreti

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// MassimoFile è la dimensione massima di un file di segreti, come nello standalone (4096 byte).
const MassimoFile = 4096

// VariabiliRifiutate sono le variabili d'ambiente che nello standalone portavano un segreto come
// valore (config/runtime.exs): se ci sono, l'avvio si ferma, anche vuote. Il Go non le legge;
// rifiutarle vuol dire che un ambiente rimasto dallo standalone non passa in silenzio.
var VariabiliRifiutate = []string{"SECRET_KEY_BASE", "LEGACY_WARRANTY_SECRET"}

// LeggiFile legge un segreto da un file privato, con le regole di read_secret dello standalone:
//   - il percorso è il file stesso, regolare: un link, una cartella o una FIFO sono rifiutati
//     (lstat; l'apertura non segue un link e non resta appesa);
//   - nessun permesso al gruppo o agli altri (0600 o 0400);
//   - al più massimo byte nel file (al più MassimoFile), almeno minimo (almeno 1) nel valore;
//   - UTF-8 valido;
//   - il file non cambia mentre si legge.
//
// Il valore è il contenuto senza gli a capo finali («\n», poi «\r»). I messaggi d'errore non
// riportano mai il contenuto.
func LeggiFile(percorso string, minimo, massimo int) (string, error) {
	if minimo < 1 || massimo < minimo || massimo > MassimoFile {
		return "", fmt.Errorf("segreto %s: limiti %d-%d non validi", percorso, minimo, massimo)
	}
	sbagliato := func(motivo string, a ...any) error {
		return fmt.Errorf("segreto %s: %s", percorso, fmt.Sprintf(motivo, a...))
	}
	prima, err := os.Lstat(percorso)
	if err != nil {
		return "", err
	}
	switch {
	case prima.Mode()&os.ModeSymlink != 0:
		return "", sbagliato("è un link, deve essere il file regolare stesso")
	case !prima.Mode().IsRegular():
		return "", sbagliato("deve essere un file regolare")
	}
	f, err := os.OpenFile(percorso, os.O_RDONLY|aperturaSicura, 0)
	if err != nil {
		return "", sbagliato("non si apre (un link non vale): %v", errors.Unwrap(err))
	}
	defer func() { _ = f.Close() }() // in sola lettura: la chiusura non perde niente
	aperto, err := f.Stat()
	if err != nil {
		return "", err
	}
	// Tra lstat e l'apertura il file non è stato sostituito.
	if !os.SameFile(prima, aperto) || !aperto.Mode().IsRegular() {
		return "", sbagliato("deve essere un file regolare (è cambiato durante la lettura)")
	}
	if aperto.Mode().Perm()&0o077 != 0 {
		return "", sbagliato("non deve dare permessi a gruppo o altri (permessi %04o, servono 0600 o 0400)", aperto.Mode().Perm())
	}
	if aperto.Size() > int64(massimo) {
		return "", sbagliato("deve avere al più %d byte", massimo)
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(massimo)+1))
	if err != nil {
		return "", err
	}
	dopo, err := f.Stat()
	if err != nil {
		return "", err
	}
	if int64(len(b)) != aperto.Size() || dopo.Size() != aperto.Size() || !dopo.ModTime().Equal(aperto.ModTime()) {
		return "", sbagliato("è cambiato durante la lettura")
	}
	if !utf8.Valid(b) {
		return "", sbagliato("non è testo UTF-8 valido")
	}
	v := bytes.TrimRight(b, "\n")
	v = bytes.TrimRight(v, "\r")
	if len(v) < minimo {
		return "", sbagliato("deve avere almeno %d byte, senza gli a capo finali", minimo)
	}
	return string(v), nil
}

// ControllaAmbiente rifiuta l'avvio se c'è una variabile di VariabiliRifiutate, anche vuota (in
// Elixir System.get_env dà "" e "" vale come vero). cerca è os.LookupEnv. Il messaggio dice il
// nome, mai il valore.
func ControllaAmbiente(cerca func(string) (string, bool)) error {
	var trovate []string
	for _, nome := range VariabiliRifiutate {
		if _, c := cerca(nome); c {
			trovate = append(trovate, nome+" non è accettata")
		}
	}
	if len(trovate) == 0 {
		return nil
	}
	return fmt.Errorf("%s: i segreti non passano dalle variabili d'ambiente; gestrip non le legge, vanno tolte dall'ambiente del servizio",
		strings.Join(trovate, ", "))
}
