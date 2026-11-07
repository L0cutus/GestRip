package buste

import (
	"errors"
	"reflect"
	"testing"
)

// Slice 055d (Stefano 06/11/2026, risposte 1 e 2 alla 053): «rip-» minuscolo e gli spazi accanto ai
// trattini si tolgono quando il risultato è un'etichetta; il resto tiene solo gli spazi ai bordi tolti.

// inventario: RIP-H-39, RIP-H-22
func TestNormalizzaCodice(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{"RIP-2026-0045", "RIP-2026-0045"},
		{"rip-2026-0045", "RIP-2026-0045"},
		{"rIp-2026-0045", "RIP-2026-0045"},
		{" \trip-2026-0045\n", "RIP-2026-0045"},
		{"RIP - 2026 - 0045", "RIP-2026-0045"},
		{"RIP-2026- 0045", "RIP-2026-0045"},
		{"RIP-2026-10000", "RIP-2026-10000"},
		{"RIP 2026 0045", "RIP 2026 0045"},
		{"RIP-2026-00 45", "RIP-2026-00 45"},
		{"RIP-20 26-0045", "RIP-20 26-0045"},
		{"rip-2026-45", "rip-2026-45"},
		{"rip-2026-0000", "rip-2026-0000"},
		{" ABC 12 ", "ABC 12"},
		{"abc", "abc"},
		{"0045 / 26", "0045 / 26"}, // il numero scritto lo risolve EtichettaScritta, non qui
		{"   ", ""},
		{"", ""},
	} {
		if got := NormalizzaCodice(c.in); got != c.out {
			t.Errorf("%q: %q, atteso %q", c.in, got, c.out)
		}
	}
}

// inventario: RIP-H-39
// Le forme che somigliano a un'etichetta senza esserlo avvisano; le etichette, i numeri scritti come
// la ricerca, i codici di altro formato e il vuoto no.
func TestSembraEtichetta(t *testing.T) {
	for _, c := range []struct {
		codice string
		sembra bool
	}{
		{"45/2026", true}, {"0045-26", true}, {"0045-2026", true}, {"45.26", true}, {"#45", true},
		{"00045", true}, {"0", true}, {"0000/26", true}, {"4 5", true}, {"123456", true},
		{"RIP-2026-45", true}, {"RIP-26-0045", true}, {"RIP 2026 0045", true}, {"rip2026", true},
		{"RIP-2026-0000", true}, {"RIP-2026-00045", true},
		{"RIP-2026-0045", false}, {"rip-2026-0045", false}, {"45", false}, {"0045/26", false},
		{"ABC-12", false}, {"SINT-QR-0005", false}, {"8001234567890", false}, {"", false}, {"  ", false},
	} {
		if got := SembraEtichetta(c.codice); got != c.sembra {
			t.Errorf("%q: %v, atteso %v", c.codice, got, c.sembra)
		}
	}
}

// inventario: RIP-H-39, RIP-H-30
// La creazione avvisa una volta per un codice simile a un'etichetta («45/2026»), senza salvare né
// far avanzare il contatore; con lo stesso codice confermato salva com'è. L'avviso viene dopo gli
// errori dei campi e prima delle gemelle; «rip-» minuscolo non avvisa e si salva in maiuscolo.
func TestCreaFormaNonRiconosciuta(t *testing.T) {
	b, d, _ := prova(t)
	prima := scatta(t, d)
	for _, conferma := range []string{"", "0045-26", "45"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.FormaConfermata = " 45/2026 ", conferma }), chi)
		var f *FormaNonRiconosciuta
		if !errors.As(err, &f) || f.Codice != "45/2026" {
			t.Fatalf("conferma %q: %v %+v", conferma, err, f)
		}
		uguali(t, "conferma "+conferma, prima, scatta(t, d))
	}
	// Prima gli errori dei campi.
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.Descrizione = "45/2026", "" }), chi); !errors.As(err, new(*CampiRifiutati)) {
		t.Errorf("campi: %v", err)
	}
	c, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.FormaConfermata = "45/2026", " 45/2026" }), chi)
	if err != nil || valore(t, d, `SELECT codice_qr FROM buste WHERE id = ?`, c.ID) != "45/2026" || c.CodiceQR != "45/2026" {
		t.Fatalf("confermata: %v %+v", err, c)
	}
	// «rip-» minuscolo: nessun avviso, salvato in maiuscolo.
	c, err = b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "rip-2026- 0050" }), chi)
	if err != nil || valore(t, d, `SELECT codice_qr FROM buste WHERE id = ?`, c.ID) != "RIP-2026-0050" {
		t.Fatalf("minuscolo: %v", err)
	}
	// Prima la forma, poi le gemelle: «45/2026» è già sulla busta aperta salvata qui sopra.
	_, err = b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "45/2026" }), chi)
	if !errors.As(err, new(*FormaNonRiconosciuta)) {
		t.Fatalf("prima la forma: %v", err)
	}
	_, err = b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.FormaConfermata = "45/2026", "45/2026" }), chi)
	var g *GemelleAperte
	if !errors.As(err, &g) || g.Codice != "45/2026" {
		t.Fatalf("poi le gemelle: %v", err)
	}
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) {
		a.CodiceQR, a.FormaConfermata, a.GemelleConfermate = "45/2026", "45/2026", "45/2026"
	}), chi); err != nil {
		t.Fatalf("tutte e due confermate: %v", err)
	}
	// Le gemelle riconoscono il salvato minuscolo o con gli spazi (dato importato).
	id := gemella(t, b, "Importata", "RIP-2026-0060", "in_lavorazione", false)
	esegui(t, d, `UPDATE buste SET codice_qr = ' rip-2026 -0060' WHERE id = ?`, id)
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "60" }), chi); !errors.As(err, &g) ||
		g.Codice != "RIP-2026-0060" || len(g.Righe) != 1 || g.Righe[0].ID != id {
		t.Errorf("gemella importata: %v", err)
	}
}

// inventario: RIP-S-04, RIP-H-39
// La modifica avvisa allo stesso modo, solo per un testo cambiato: un codice importato rimandato
// com'era non avvisa. Confermato, salva com'è.
func TestModificaFormaNonRiconosciuta(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil)
	modifica(t, b, id, Modifica{CodiceQR: p("45")})
	_, err := b.Modifica(ctx, id, Modifica{CodiceQR: p("0045-26"), NoteInterne: p("nota")}, nil, chi)
	var f *FormaNonRiconosciuta
	if !errors.As(err, &f) || f.Codice != "0045-26" {
		t.Fatalf("avviso: %v", err)
	}
	if got := colonne(t, b, id, "codice_qr", "note_interne"); got != "RIP-2026-0045|∅" {
		t.Errorf("dopo l'avviso: %s", got)
	}
	// Prima i rifiuti dei campi.
	if _, err := b.Modifica(ctx, id, Modifica{CodiceQR: p("0045-26"), GiorniInPiu: p("91")}, nil, chi); !errors.As(err, new(Campi)) {
		t.Errorf("campi: %v", err)
	}
	modifica(t, b, id, Modifica{CodiceQR: p("0045-26"), FormaConfermata: "0045-26"})
	if got := colonne(t, b, id, "codice_qr"); got != "0045-26" {
		t.Errorf("confermata: %s", got)
	}
	// Rimandato uguale: niente avviso.
	modifica(t, b, id, Modifica{CodiceQR: p("0045-26"), NoteInterne: p("nota")})
	if got := colonne(t, b, id, "codice_qr", "note_interne"); got != "0045-26|nota" {
		t.Errorf("rimandato uguale: %s", got)
	}
	// Un codice importato con gli spazi, rimandato com'era: si normalizza senza avviso.
	esegui(t, b.d, `UPDATE buste SET codice_qr = ' 45/2026 ' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{CodiceQR: p(" 45/2026 ")})
	if got := colonne(t, b, id, "codice_qr"); got != "45/2026" {
		t.Errorf("importato rimandato: %q", got)
	}
	// «rip-» minuscolo scritto nella modifica: in maiuscolo, senza avviso.
	modifica(t, b, id, Modifica{CodiceQR: p("rip-2026-0046")})
	if got := colonne(t, b, id, "codice_qr"); got != "RIP-2026-0046" {
		t.Errorf("minuscolo: %s", got)
	}
}

// inventario: RIP-H-22, RIP-H-03
// Review della 055d: un salvato importato con uno spazio Unicode o un tab accanto a un trattino è la
// stessa etichetta per NormalizzaCodice, quindi la scansione, le gemelle e la ricerca per numero lo
// trovano (le candidate SQL non devono perderlo).
func TestCandidateConSpaziUnicode(t *testing.T) {
	b, _, _ := prova(t)
	nbsp := bustaLista(t, b, "2/26", "Q", "codice_qr = ?", "RIP-2026- 0045")
	tab := bustaLista(t, b, "3/26", "Q", "codice_qr = ?", "rip-2026 -\t0045")
	altro := bustaLista(t, b, "4/26", "Q", "codice_qr = ?", "\tABC 12 ")
	ids := func(r []RigaQR) []int64 {
		var out []int64
		for _, x := range r {
			out = append(out, x.ID)
		}
		return out
	}
	if r, err := b.CercaPerQR(ctx, "RIP-2026-0045", false, chi); err != nil || !reflect.DeepEqual(ids(r), []int64{tab, nbsp}) {
		t.Errorf("scansione: %v %v", ids(r), err)
	}
	if r, err := b.CercaPerQR(ctx, "ABC 12", false, chi); err != nil || !reflect.DeepEqual(ids(r), []int64{altro}) {
		t.Errorf("altro formato: %v %v", ids(r), err)
	}
	if r, err := b.CercaPerQR(ctx, "abc 12", false, chi); err != nil || len(r) != 0 {
		t.Errorf("altro formato minuscolo: %v %v", ids(r), err)
	}
	got, err := b.busteDellEtichetta(ctx, RicercaEtichetta{Numero: 45, Anno: "26"})
	if err != nil || !reflect.DeepEqual(got, []int64{nbsp, tab}) {
		t.Errorf("ricerca per numero: %v %v", got, err)
	}
}
