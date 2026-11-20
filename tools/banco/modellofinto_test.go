package main

import (
	"slices"
	"strings"
	"testing"
)

// banco modello-finto rifiuta argomenti in più e un indirizzo che non si apre (il servizio vero lo
// prova TestServi del pacchetto modellofinto).
func TestCmdModelloFinto(t *testing.T) {
	if err := cmdModelloFinto([]string{"-ascolta", "127.0.0.1:0", "avanzo"}); err == nil {
		t.Error("argomento in più accettato")
	}
	if err := cmdModelloFinto([]string{"-sconosciuto"}); err == nil {
		t.Error("opzione sconosciuta accettata")
	}
	if err := cmdModelloFinto([]string{"-ascolta", "indirizzo-sbagliato"}); err == nil {
		t.Error("indirizzo sbagliato accettato")
	}
}

// Il container del modello finto (slice 107): rete interna con l'alias modello.invalid, dal binario del banco in sola
// lettura, nessuna porta sull'host; banco giu lo toglie.
func TestComandoModelloFinto(t *testing.T) {
	n, _ := nomiPer("t9")
	c := comandoModelloFinto(n, "/banco/bin/banco")
	s := strings.Join(c, " ")
	for _, x := range []string{"run -d ", "--name grgo-t9-modello", "--network grgo-t9-net", "--network-alias modello.invalid", "--label grgo=1",
		"--user 1000:1000", "--cap-drop ALL", "--read-only", "no-new-privileges:true",
		"type=bind,src=/banco/bin/banco,dst=/banco/banco,readonly", immagineBase + " /banco/banco modello-finto -ascolta :8082"} {
		if !strings.Contains(s, x) {
			t.Errorf("comando del modello finto senza %q: %s", x, s)
		}
	}
	if c[0] != "run" || pubblicaPorte(c) {
		t.Errorf("comando del modello finto: %s", s)
	}
	if !slices.Contains(suffissiContainer(), "-modello") {
		t.Error("giu non toglie il modello finto")
	}
	if !strings.HasSuffix(aliasModelloFinto, ".invalid") || indirizzoModelloFinto != "http://modello.invalid:8082" {
		t.Errorf("indirizzo del modello finto %s", indirizzoModelloFinto)
	}
}
