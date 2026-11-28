package db

import "testing"

// La stessa cartella remota vista dalle due porte della Storage Box (slice 133c, voce 19): uguali, o una è l'altra
// con un solo «/home» davanti; ogni altra coppia è una cartella diversa.
func TestStessaCartellaRemota(t *testing.T) {
	for _, c := range []struct {
		a, b   string
		stessa bool
	}{
		{"/backup", "/backup", true},
		{"/home/backup", "/home/backup", true},
		{"/home/backup", "/backup", true},
		{"/backup", "/home/backup", true},
		{"/home/a/b", "/a/b", true},
		{"/home/home", "/home", true}, // la cartella «home» dalla 22 è «/home/home» dalla 23
		{"/home/home/backup", "/home/backup", true},
		{"/home/home/backup", "/backup", false}, // due prefissi
		{"/backup", "/home/home/backup", false},
		{"/home/altra", "/backup", false},
		{"/home/backup", "/altra", false},
		{"/backup/sub", "/backup", false},
		{"/home/backup/sub", "/backup", false},
		{"/backup", "/home/backup/sub", false},
		{"/homebackup", "/backup", false},
		{"/backup", "/homebackup", false},
		{"/home/backup", "/home/altra", false},
		{"/home", "/", false}, // la radice non è mai una cartella dei backup
		{"/", "/home", false},
		{"/home/", "/", false},
		{"home/backup", "backup", false}, // non assoluti
		{"/homebackup", "backup", false},
		{"backup", "/homebackup", false},
		{"/home/backup/", "/backup/", false},
		{"/home/backup", "/backup/", false},
		{"/home/backup/.", "/backup", false}, // non puliti
		{"/home//backup", "/backup", false},
		{"/home/../backup", "/backup", false},
		{"/home/backup", "/./backup", false},
		{"/HOME/backup", "/backup", false},
		{"", "", true},
		{"/home", "", false},
	} {
		if got := StessaCartellaRemota(c.a, c.b); got != c.stessa {
			t.Errorf("StessaCartellaRemota(%q, %q) = %v, atteso %v", c.a, c.b, got, c.stessa)
		}
	}
}
