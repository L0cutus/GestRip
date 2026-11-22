package main

import (
	"strings"
	"testing"
)

// Un'uscita di go test -json con un test che passa, uno che fallisce con un sottotest, un pacchetto che non si compila e un
// pacchetto che cade senza un test (panic in TestMain): si stampano solo i caduti, ognuno con le sue righe, nell'ordine.
const uscitaProva = `{"Action":"start","Package":"es/a"}
{"Action":"run","Package":"es/a","Test":"TestBuono"}
{"Action":"output","Package":"es/a","Test":"TestBuono","Output":"=== RUN   TestBuono\n"}
{"Action":"output","Package":"es/a","Test":"TestBuono","Output":"--- PASS: TestBuono (0.00s)\n"}
{"Action":"pass","Package":"es/a","Test":"TestBuono","Elapsed":0}
{"Action":"run","Package":"es/a","Test":"TestInstabile"}
{"Action":"output","Package":"es/a","Test":"TestInstabile","Output":"=== RUN   TestInstabile\n"}
{"Action":"run","Package":"es/a","Test":"TestInstabile/caso"}
{"Action":"output","Package":"es/a","Test":"TestInstabile/caso","Output":"    a_test.go:12: atteso 1, avuto 2\n"}
{"Action":"output","Package":"es/a","Test":"TestInstabile/caso","Output":"    --- FAIL: TestInstabile/caso (0.01s)\n"}
{"Action":"fail","Package":"es/a","Test":"TestInstabile/caso","Elapsed":0.01}
{"Action":"output","Package":"es/a","Test":"TestInstabile","Output":"--- FAIL: TestInstabile (0.01s)\n"}
{"Action":"fail","Package":"es/a","Test":"TestInstabile","Elapsed":0.01}
{"Action":"output","Package":"es/a","Output":"FAIL\n"}
{"Action":"fail","Package":"es/a","Elapsed":0.02}
{"ImportPath":"es/b","Action":"build-output","Output":"# es/b\n"}
{"ImportPath":"es/b","Action":"build-output","Output":"b.go:3:1: syntax error\n"}
{"ImportPath":"es/b","Action":"build-fail"}
{"Action":"start","Package":"es/b"}
{"Action":"output","Package":"es/b","Output":"FAIL\tes/b [build failed]\n"}
{"Action":"fail","Package":"es/b","Elapsed":0,"FailedBuild":"es/b"}
{"Action":"start","Package":"es/c"}
{"Action":"output","Package":"es/c","Output":"panic: caduto in TestMain\n"}
{"Action":"fail","Package":"es/c","Elapsed":0.1}
{"Action":"pass","Package":"es/d","Elapsed":0.1}
riga che non è JSON
`

func TestStampaFalliti(t *testing.T) {
	var out strings.Builder
	if err := stampaFalliti(strings.NewReader(uscitaProva), &out); err != nil {
		t.Fatal(err)
	}
	atteso := `== non compilato: es/b
# es/b
b.go:3:1: syntax error
== FAIL es/a TestInstabile/caso
    a_test.go:12: atteso 1, avuto 2
    --- FAIL: TestInstabile/caso (0.01s)
== FAIL es/a TestInstabile
=== RUN   TestInstabile
--- FAIL: TestInstabile (0.01s)
== FAIL es/a (pacchetto)
FAIL
== FAIL es/b (pacchetto)
FAIL	es/b [build failed]
== FAIL es/c (pacchetto)
panic: caduto in TestMain
== righe non JSON: 1
riga che non è JSON
== falliti: 2 test, 3 pacchetti
`
	if out.String() != atteso {
		t.Errorf("uscita:\n%s\nattesa:\n%s", out.String(), atteso)
	}
	// Niente caduti: una riga sola.
	out.Reset()
	if err := stampaFalliti(strings.NewReader(`{"Action":"pass","Package":"es/d"}`+"\n"), &out); err != nil || out.String() != "== falliti: 0 test, 0 pacchetti\n" {
		t.Errorf("senza caduti: %q %v", out.String(), err)
	}
}
