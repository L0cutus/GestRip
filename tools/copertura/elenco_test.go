package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestElencoAndataERitorno(t *testing.T) {
	voci, err := leggiInventario(strings.NewReader(inventarioFinto))
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := scriviElenco(&b, voci, []string{"negozio-vero"}); err != nil {
		t.Fatal(err)
	}
	righe := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	if !strings.HasPrefix(righe[0], "# ") || len(righe) != 1+len(voci) {
		t.Fatalf("elenco inatteso:\n%s", b.String())
	}
	if righe[1] != "OPS-11\tStrumento ritirato\tmai" {
		t.Errorf("prima riga = %q (attese in ordine di ID)", righe[1])
	}
	letto, err := leggiElenco(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(letto) != len(voci) {
		t.Fatalf("letto %d voci, attese %d", len(letto), len(voci))
	}
	for id, v := range voci {
		if letto[id] != v {
			t.Errorf("%s: letto %+v, atteso %+v", id, letto[id], v)
		}
	}
}

func TestElencoRifiutaParoleVietate(t *testing.T) {
	voci := map[string]voce{"RIP-H-01": {ID: "RIP-H-01", Titolo: "Etichetta di Negozio-Vero", Destino: "v1"}}
	var b bytes.Buffer
	err := scriviElenco(&b, voci, []string{"negozio-vero"})
	if err == nil || !strings.Contains(err.Error(), "RIP-H-01") {
		t.Fatalf("err = %v, atteso rifiuto su RIP-H-01", err)
	}
	if b.Len() != 0 {
		t.Errorf("con una parola vietata non deve scrivere niente, scritto %q", b.String())
	}
	if err := scriviElenco(&b, voci, nil); err == nil {
		t.Error("senza elenco di parole vietate deve rifiutare (fail-closed)")
	}
	voci["RIP-H-01"] = voce{ID: "RIP-H-01", Titolo: "a\tb", Destino: "v1"}
	if err := scriviElenco(&b, voci, []string{"x"}); err == nil {
		t.Error("un titolo con tabulazione deve essere rifiutato")
	}
}

func TestLeggiElencoRifiutaRigheSbagliate(t *testing.T) {
	casi := map[string]string{
		"destino ignoto": "RIP-H-01\tA\tforse\n",
		"duplicato":      "RIP-H-01\tA\tv1\nRIP-H-01\tB\tv1\n",
		"ID non valido":  "rip-01\tA\tv1\n",
		"campi mancanti": "RIP-H-01\tA\n",
		"titolo vuoto":   "RIP-H-01\t\tv1\n",
		"vuoto":          "# solo intestazione\n",
	}
	for nome, testo := range casi {
		if _, err := leggiElenco(strings.NewReader(testo)); err == nil {
			t.Errorf("%s: atteso errore", nome)
		}
	}
}

func TestLeggiParoleVietate(t *testing.T) {
	p, err := leggiParoleVietate(strings.NewReader("Uno\n\n  due \r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(p, ",") != "uno,due" {
		t.Errorf("parole = %q", p)
	}
	if _, err := leggiParoleVietate(strings.NewReader("\n \n")); err == nil {
		t.Error("un elenco vuoto deve essere un errore")
	}
}
