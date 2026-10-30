package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
)

// L'elenco pubblico dell'inventario (PIANO §8.3, risposta A2): una riga per voce con ID, titolo
// e destino, separati da tabulazioni. È l'unica parte dell'inventario che va nel repo; la CI
// legge questo, l'inventario completo resta privato.
const intestazioneElenco = "# Generato da `go run ./tools/copertura -scrivi-elenco` dall'inventario privato. Non modificare a mano.\n"

// scriviElenco scrive l'elenco in ordine di ID. Se un titolo contiene una parola vietata non
// scrive niente: senza parole vietate da controllare rifiuta (fail-closed).
func scriviElenco(w io.Writer, voci map[string]voce, vietate []string) error {
	if len(vietate) == 0 {
		return fmt.Errorf("elenco delle parole vietate vuoto o mancante: non scrivo l'elenco senza controllarlo")
	}
	ids := make([]string, 0, len(voci))
	for id := range voci {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var b bytes.Buffer
	b.WriteString(intestazioneElenco)
	for _, id := range ids {
		v := voci[id]
		riga := v.ID + "\t" + v.Titolo + "\t" + v.Destino
		if strings.ContainsAny(v.Titolo, "\t\r\n") {
			return fmt.Errorf("voce %s: il titolo contiene tabulazioni o a capo", id)
		}
		basso := strings.ToLower(riga)
		for _, p := range vietate {
			if strings.Contains(basso, p) {
				return fmt.Errorf("voce %s: il titolo contiene una parola vietata", id)
			}
		}
		b.WriteString(riga + "\n")
	}
	_, err := w.Write(b.Bytes())
	return err
}

// leggiElenco legge l'elenco scritto da scriviElenco; ogni riga sbagliata è un errore.
func leggiElenco(r io.Reader) (map[string]voce, error) {
	voci := map[string]voce{}
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		l := sc.Text()
		if strings.HasPrefix(l, "#") {
			continue
		}
		c := strings.Split(l, "\t")
		if len(c) != 3 || c[1] == "" {
			return nil, fmt.Errorf("elenco, riga %d: attesi ID, titolo e destino", n)
		}
		if !reID.MatchString(c[0]) {
			return nil, fmt.Errorf("elenco, riga %d: ID non valido %q", n, c[0])
		}
		if !slices.Contains(destini, c[2]) {
			return nil, fmt.Errorf("elenco, riga %d: destino non riconosciuto %q", n, c[2])
		}
		if _, dup := voci[c[0]]; dup {
			return nil, fmt.Errorf("elenco, riga %d: voce %s duplicata", n, c[0])
		}
		voci[c[0]] = voce{ID: c[0], Titolo: c[1], Destino: c[2]}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(voci) == 0 {
		return nil, fmt.Errorf("elenco vuoto")
	}
	return voci, nil
}

// leggiParoleVietate legge l'elenco locale delle parole vietate (fuori dal repo), una per riga,
// in minuscolo. Un elenco vuoto è un errore.
func leggiParoleVietate(r io.Reader) ([]string, error) {
	var p []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if s := strings.ToLower(strings.TrimSpace(sc.Text())); s != "" {
			p = append(p, s)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(p) == 0 {
		return nil, fmt.Errorf("elenco delle parole vietate vuoto")
	}
	return p, nil
}
