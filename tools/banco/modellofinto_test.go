package main

import "testing"

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
