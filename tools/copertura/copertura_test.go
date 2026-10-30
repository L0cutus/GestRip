package main

import (
	"strings"
	"testing"
)

const inventarioFinto = `# Inventario

| D-01 | RIP-S-46 | tabella delle deviazioni, non è una voce |

## Parte uno

### RIP-H-01 — Accesso alla home
- **Destino Go:** v1 — «home».

### RIP-H-56 — Segno «⚠ Senza QR»
- **Destino Go:** v1.

### STA-01 — Statistiche
- **Destino Go:** dopo-v1 insieme alle statistiche.

### OPS-11 — Strumento ritirato
- **Destino Go:** mai.

### OPS-16 — Backup
- **Destino Go:** sostituita — backup age.

### Da verificare
- ### RIP-H-99 — non è un titolo
`

func TestLeggiInventario(t *testing.T) {
	voci, err := leggiInventario(strings.NewReader(inventarioFinto))
	if err != nil {
		t.Fatal(err)
	}
	attese := map[string]string{
		"RIP-H-01": "v1", "RIP-H-56": "v1", "STA-01": "dopo-v1", "OPS-11": "mai", "OPS-16": "sostituita",
	}
	if len(voci) != len(attese) {
		t.Fatalf("voci = %d, attese %d: %v", len(voci), len(attese), voci)
	}
	for id, destino := range attese {
		v, ok := voci[id]
		if !ok {
			t.Fatalf("manca %s", id)
		}
		if v.Destino != destino {
			t.Errorf("%s destino = %q, atteso %q", id, v.Destino, destino)
		}
	}
	if voci["RIP-H-56"].Titolo != "Segno «⚠ Senza QR»" {
		t.Errorf("titolo = %q", voci["RIP-H-56"].Titolo)
	}
}

func TestLeggiInventarioRifiutaDuplicatiEDestiniIgnoti(t *testing.T) {
	dup := "### RIP-H-01 — a\n- **Destino Go:** v1\n### RIP-H-01 — b\n- **Destino Go:** v1\n"
	if _, err := leggiInventario(strings.NewReader(dup)); err == nil {
		t.Error("ID duplicato accettato")
	}
	ignoto := "### RIP-H-01 — a\n- **Destino Go:** Da decidere\n"
	if _, err := leggiInventario(strings.NewReader(ignoto)); err == nil || !strings.Contains(err.Error(), "non riconosciuto") {
		t.Errorf("destino «Da decidere»: err = %v, atteso «non riconosciuto» (l'inventario va riallineato)", err)
	}
	senza := "### RIP-H-01 — a\n- **Regole:** niente\n"
	if _, err := leggiInventario(strings.NewReader(senza)); err == nil {
		t.Error("voce senza destino accettata")
	}
}

const modulo = "github.com/L0cutus/gestrip"

const testFinto = `package esempio

import "testing"

// inventario: RIP-H-01, RIP-H-56
// La home mostra i contatori.
func TestHomeContatori(t *testing.T) {}

// inventario: OPS-16
func TestBackup(t *testing.T) {
	t.Run("cifrato", func(t *testing.T) {})
}

func TestSenzaSegno(t *testing.T) {}

func helper() {}
`

func TestLeggiSegni(t *testing.T) {
	segni, err := leggiSegni("esempio/a_test.go", strings.NewReader(testFinto))
	if err != nil {
		t.Fatal(err)
	}
	want := []segno{
		{File: "esempio/a_test.go", Riga: 5, Test: "TestHomeContatori", Voci: []string{"RIP-H-01", "RIP-H-56"}},
		{File: "esempio/a_test.go", Riga: 9, Test: "TestBackup", Voci: []string{"OPS-16"}},
	}
	if len(segni) != len(want) {
		t.Fatalf("segni = %+v", segni)
	}
	for i := range want {
		g, w := segni[i], want[i]
		if g.Test != w.Test || g.Riga != w.Riga || strings.Join(g.Voci, ",") != strings.Join(w.Voci, ",") {
			t.Errorf("segno %d = %+v, atteso %+v", i, g, w)
		}
	}
}

func TestLeggiSegniRifiutaSegnoSenzaTest(t *testing.T) {
	src := "package x\n\n// inventario: RIP-H-01\nfunc helper() {}\n"
	if _, err := leggiSegni("x_test.go", strings.NewReader(src)); err == nil {
		t.Error("segno sopra una funzione che non è un test accettato")
	}
	malformato := "package x\n\n// inventario: rip-h-1\nfunc TestX(t *testing.T) {}\n"
	if _, err := leggiSegni("x_test.go", strings.NewReader(malformato)); err == nil {
		t.Error("ID malformato accettato")
	}
}

const risultatiFinti = `{"Action":"run","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestHomeContatori"}
{"Action":"pass","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestHomeContatori","Elapsed":0}
{"Action":"run","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestBackup"}
{"Action":"run","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestBackup/cifrato"}
{"Action":"fail","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestBackup/cifrato","Elapsed":0}
{"Action":"fail","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestBackup","Elapsed":0}
{"Action":"pass","Package":"github.com/L0cutus/gestrip/esempio","Elapsed":0}
`

func TestCalcolaStato(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	segni, _ := leggiSegni("esempio/a_test.go", strings.NewReader(testFinto))
	esiti, err := leggiRisultati(strings.NewReader(risultatiFinti))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := calcola(voci, segni, esiti, modulo)
	if err != nil {
		t.Fatal(err)
	}
	stati := map[string]string{}
	for _, r := range rep.Righe {
		stati[r.ID] = r.Stato
	}
	attesi := map[string]string{
		"RIP-H-01": statoChiusa,  // test verde
		"RIP-H-56": statoChiusa,  // stesso test
		"OPS-16":   statoRossa,   // test rosso
		"STA-01":   statoFuoriV1, // dopo-v1
		"OPS-11":   statoFuoriV1, // mai
	}
	for id, s := range attesi {
		if stati[id] != s {
			t.Errorf("%s stato = %q, atteso %q", id, stati[id], s)
		}
	}
	if rep.Conteggi[statoChiusa] != 2 || rep.Conteggi[statoRossa] != 1 || rep.Conteggi[statoFuoriV1] != 2 {
		t.Errorf("conteggi = %v", rep.Conteggi)
	}
}

func TestCalcolaSenzaRisultati(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	segni, _ := leggiSegni("esempio/a_test.go", strings.NewReader(testFinto))
	rep, err := calcola(voci, segni, nil, modulo)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Righe {
		if r.ID == "RIP-H-01" && r.Stato != statoParziale {
			t.Errorf("senza esiti RIP-H-01 = %q, atteso %q", r.Stato, statoParziale)
		}
	}
}

func TestCalcolaRifiutaSegnoVersoVoceInesistente(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	segni := []segno{{File: "x_test.go", Riga: 3, Test: "TestX", Voci: []string{"RIP-H-77"}}}
	if _, err := calcola(voci, segni, nil, modulo); err == nil {
		t.Error("segno verso una voce inesistente accettato")
	}
}

func TestVoceSenzaTestEAperta(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader("### RIP-H-02 — x\n- **Destino Go:** v1.\n"))
	rep, err := calcola(voci, nil, nil, modulo)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Righe[0].Stato != statoAperta {
		t.Errorf("stato = %q, atteso %q", rep.Righe[0].Stato, statoAperta)
	}
}

func TestLeggiSegniIgnoraLeStringheERifiutaISegniAppesi(t *testing.T) {
	inStringa := "package x\n\nconst s = `\n// inventario: RIP-H-01\n`\n"
	segni, err := leggiSegni("x_test.go", strings.NewReader(inStringa))
	if err != nil || len(segni) != 0 {
		t.Errorf("segno dentro una stringa: segni=%v err=%v", segni, err)
	}
	appeso := "package x\n\nfunc TestX(t *testing.T) {\n\t// inventario: RIP-H-01\n}\n"
	if _, err := leggiSegni("x_test.go", strings.NewReader(appeso)); err == nil {
		t.Error("segno dentro il corpo di un test accettato")
	}
	metodo := "package x\n\n// inventario: RIP-H-01\nfunc (s *S) TestX(t *testing.T) {}\n"
	if _, err := leggiSegni("x_test.go", strings.NewReader(metodo)); err == nil {
		t.Error("segno sopra un metodo accettato")
	}
}

func TestLeggiScenarioDelBanco(t *testing.T) {
	ids, err := leggiScenarioBanco("bench/scenari/healthz.json", strings.NewReader(`{"nome":"healthz","inventario":["ADM-20","RIP-H-01"],"passi":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "ADM-20,RIP-H-01" {
		t.Errorf("voci = %v", ids)
	}
	if _, err := leggiScenarioBanco("x.json", strings.NewReader(`{"inventario":["adm-20"]}`)); err == nil {
		t.Error("ID malformato accettato")
	}
}

func TestCalcolaRiportaIlBanco(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	rep, err := calcolaConBanco(voci, nil, nil, modulo, map[string][]string{"RIP-H-01": {"healthz"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Righe {
		if r.ID == "RIP-H-01" && (len(r.Banco) != 1 || r.Banco[0] != "healthz") {
			t.Errorf("banco = %v", r.Banco)
		}
	}
	if _, err := calcolaConBanco(voci, nil, nil, modulo, map[string][]string{"RIP-H-77": {"x"}}, nil); err == nil {
		t.Error("scenario verso una voce inesistente accettato")
	}
}

func TestUnFallimentoNonSiCopreESkipNonERosso(t *testing.T) {
	r := `{"Action":"fail","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestHomeContatori"}
{"Action":"pass","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestHomeContatori"}
{"Action":"skip","Package":"github.com/L0cutus/gestrip/esempio","Test":"TestBackup"}
`
	e, err := leggiRisultati(strings.NewReader(r))
	if err != nil {
		t.Fatal(err)
	}
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	segni, _ := leggiSegni("esempio/a_test.go", strings.NewReader(testFinto))
	rep, _ := calcola(voci, segni, e, modulo)
	for _, x := range rep.Righe {
		if x.ID == "RIP-H-01" && x.Stato != statoRossa {
			t.Errorf("fail seguito da pass: %q, atteso rossa", x.Stato)
		}
		if x.ID == "OPS-16" && x.Stato != statoParziale {
			t.Errorf("test saltato: %q, atteso parziale", x.Stato)
		}
	}
}

func TestPackageEsatto(t *testing.T) {
	r := `{"Action":"pass","Package":"github.com/L0cutus/gestrip/altro/esempio","Test":"TestHomeContatori"}
{"Action":"fail","Package":"github.com/L0cutus/gestrip","Test":"TestRadice"}
`
	e, _ := leggiRisultati(strings.NewReader(r))
	if _, trovato := e.esito(segno{File: "esempio/a_test.go", Test: "TestHomeContatori"}, modulo); trovato {
		t.Error("esito preso da un package che finisce solo con lo stesso nome")
	}
	if v, trovato := e.esito(segno{File: "a_test.go", Test: "TestRadice"}, modulo); !trovato || v {
		t.Error("esito della radice del modulo non trovato")
	}
}

func TestBancoDecideLaChiusura(t *testing.T) {
	voci, _ := leggiInventario(strings.NewReader(inventarioFinto))
	segni, _ := leggiSegni("esempio/a_test.go", strings.NewReader(testFinto))
	e, _ := leggiRisultati(strings.NewReader(risultatiFinti))
	banco := map[string][]string{"RIP-H-01": {"healthz"}}
	stato := func(esitoBanco map[string]bool) string {
		rep, err := calcolaConBanco(voci, segni, e, modulo, banco, esitoBanco)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range rep.Righe {
			if x.ID == "RIP-H-01" {
				return x.Stato
			}
		}
		return ""
	}
	if s := stato(nil); s != statoParziale {
		t.Errorf("scenario senza esito del banco: %q, atteso parziale", s)
	}
	if s := stato(map[string]bool{"healthz": false}); s != statoParziale {
		t.Errorf("banco rosso: %q, atteso parziale", s)
	}
	if s := stato(map[string]bool{"healthz": true}); s != statoChiusa {
		t.Errorf("banco verde: %q, atteso chiusa", s)
	}
}

func TestSegniScrittiMaleSonoErrori(t *testing.T) {
	for _, src := range []string{
		"package x\n\n//inventario: RIP-H-01\nfunc TestX(t *testing.T) {}\n",
		"package x\n\n// Inventario: RIP-H-01\nfunc TestX(t *testing.T) {}\n",
		"package x\n\n/* inventario: RIP-H-01 */\nfunc TestX(t *testing.T) {}\n",
		"package x\n\n// inventario: RIP-H-01\nfunc Testx(t *testing.T) {}\n",
	} {
		if _, err := leggiSegni("x_test.go", strings.NewReader(src)); err == nil {
			t.Errorf("accettato: %q", src)
		}
	}
}
