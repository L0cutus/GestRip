package buste

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Il numero di busta scritto con zeri davanti e i testi degli errori in italiano (slice 037d,
// allineamento alla slice 220 dello standalone: Repair.normalize_bag_number/1,
// @typed_bag_number_format, resolve_bag_number/3 e priv/gettext/it/LC_MESSAGES/errors.po); i casi
// da bag_number_zeros_test.exs e italian_errors_test.exs.

// inventario: RIP-H-31
// NormalizzaNumero toglie gli zeri davanti al numero e ne tiene una cifra; tutto quello che non è
// N/AA torna com'era. Come la regex dello standalone (PCRE, `$` anche prima di un a capo finale).
func TestNormalizzaNumero(t *testing.T) {
	for _, c := range []struct{ scritto, atteso string }{
		{"012/26", "12/26"},
		{"0007/26", "7/26"},
		{"000012/26", "12/26"},
		{"00/26", "0/26"},
		{"0/26", "0/26"},
		{"00000/26", "0/26"},
		{"12/26", "12/26"},
		{"100/26", "100/26"},
		{"012", "012"},
		{"012/026", "012/026"},
		{"012/2", "012/2"},
		{" 012/26", " 012/26"},
		{"012/26 ", "012/26 "},
		{"012/26\n", "12/26\n"},
		{"012/26\n\n", "012/26\n\n"},
		{"0١2/26", "0١2/26"},
		{"", ""},
		{"0", "0"},
		{"/26", "/26"},
	} {
		if got := NormalizzaNumero(c.scritto); got != c.atteso {
			t.Errorf("NormalizzaNumero(%q) = %q, atteso %q", c.scritto, got, c.atteso)
		}
	}
}

// inventario: RIP-H-31, RIP-H-30
// Un numero scritto con zeri davanti si salva senza: «012/26» è «12/26», « 0007/26 » è «7/26», e
// così lo porta la riga d'audit.
func TestNumeroConZeriDavantiSalvatoSenza(t *testing.T) {
	b, d, _ := prova(t)
	if got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "012/26" })); got.Numero != "12/26" {
		t.Errorf("012/26 salvato come %q", got.Numero)
	}
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = " 0007/26 " }))
	if got.Numero != "7/26" || valore(t, d, `SELECT numero FROM buste WHERE id = ?`, got.ID) != "7/26" {
		t.Errorf("« 0007/26 » salvato come %q", got.Numero)
	}
	if s := ultimoAudit(t, d, "repair.created"); !strings.Contains(s, `"bag_number":"7/26"`) {
		t.Errorf("audit %q", s)
	}
}

// inventario: RIP-H-31
// Lo stesso numero con zeri davanti è la stessa busta: «Numero busta già esistente.», il modulo
// rimostra il numero senza zeri, e non resta niente.
func TestNumeroConZeriGiaEsistente(t *testing.T) {
	b, d, _ := prova(t)
	crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "12/26" }))
	prima := scatta(t, d)
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = "012/26"; a.NomeCliente = "Cliente Nuovo" }), chi)
	c := erroriCampi(t, err)
	if fmt.Sprint(c["numero"]) != "[Numero busta già esistente.]" || len(c) != 1 {
		t.Errorf("errori %v", c)
	}
	var r *CampiRifiutati
	if !errors.As(err, &r) || r.Numero != "12/26" {
		t.Errorf("numero rimostrato %+v", r)
	}
	uguali(t, "numero con zeri già esistente", prima, scatta(t, d))
}

// inventario: RIP-H-31
// Zero non è un numero di busta, con quanti zeri si vuole: il messaggio del formato, nessuna
// busta, contatore fermo. Il modulo rimostra il numero com'è dopo gli zeri tolti («0/26»).
func TestZeroNonENumeroDiBusta(t *testing.T) {
	b, d, _ := prova(t)
	prima := scatta(t, d)
	for _, n := range []string{"0/26", "00/26", "00000/26", " 000000/26 "} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = n }), chi)
		c := erroriCampi(t, err)
		if fmt.Sprint(c["numero"]) != "[formato richiesto N/AA (es. 12/26)]" || len(c) != 1 {
			t.Errorf("%q: errori %v", n, c)
		}
		var r *CampiRifiutati
		if !errors.As(err, &r) || r.Numero != "0/26" {
			t.Errorf("%q: numero rimostrato %+v", n, r)
		}
	}
	uguali(t, "zero", prima, scatta(t, d))
}

// inventario: RIP-H-31
// Il contatore va avanti dal numero che gli zeri nascondevano: dopo «012/26» il prossimo è 13;
// «000012/26» (sei caratteri) dopo «5/26» porta il contatore a 12, non lo lascia a 5.
func TestContatoreDopoNumeroConZeri(t *testing.T) {
	b, _, _ := prova(t)
	crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "012/26" }))
	if got := crea(t, b, modulo()); got.Numero != "13/26" {
		t.Errorf("dopo 012/26 il prossimo è %q, atteso 13/26", got.Numero)
	}

	b2, _, _ := prova(t)
	crea(t, b2, con(modulo(), func(a *Accettazione) { a.Numero = "5/26" }))
	if got := crea(t, b2, con(modulo(), func(a *Accettazione) { a.Numero = "000012/26" })); got.Numero != "12/26" {
		t.Fatalf("000012/26 salvato come %q", got.Numero)
	}
	if n, _ := b2.ProssimoNumeroDellAnno(ctx, 2026); n != "13/26" {
		t.Errorf("anteprima %q, attesa 13/26", n)
	}
	if got := crea(t, b2, modulo()); got.Numero != "13/26" {
		t.Errorf("dopo 000012/26 il prossimo è %q, atteso 13/26", got.Numero)
	}
}

// inventario: RIP-S-04
// La modifica non tocca il numero: una busta salvata con zeri davanti da un import si modifica
// lo stesso e tiene il suo numero; una nata con «012/26» resta «12/26».
func TestModificaDiBustaConZeri(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "012/26" }))
	if _, err := b.Modifica(ctx, c.ID, Modifica{Descrizione: p("Anello e catenina")}, nil, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT numero || '|' || descrizione FROM buste WHERE id = ?`, c.ID); v != "12/26|Anello e catenina" {
		t.Errorf("dopo la modifica %v", v)
	}
	esegui(t, d, `UPDATE buste SET numero = '012/26' WHERE id = ?`, c.ID)
	if _, err := b.Modifica(ctx, c.ID, Modifica{Descrizione: p("Catenina")}, nil, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT numero || '|' || descrizione FROM buste WHERE id = ?`, c.ID); v != "012/26|Catenina" {
		t.Errorf("busta importata con zeri: %v", v)
	}
}

// inventario: RIP-H-30, RIP-S-04
// I testi degli errori dei campi sono quelli del catalogo italiano dello standalone
// (priv/gettext/it/LC_MESSAGES/errors.po a go-baseline), parola per parola.
func TestTestiDegliErroriDelCatalogo(t *testing.T) {
	for _, c := range []struct{ nome, got, atteso string }{
		{"MsgObbligatorio", MsgObbligatorio, "Campo obbligatorio."},             // can't be blank
		{"MsgNonValido", MsgNonValido, "Valore non valido."},                    // is invalid
		{"MsgNomeLungo", MsgNomeLungo, "Troppo lungo (massimo 160 caratteri)."}, // should be at most %{count} character(s)
		{"MsgTelefonoLungo", MsgTelefonoLungo, "Troppo lungo (massimo 40 caratteri)."},
		{"MsgCentesimi", MsgCentesimi, "Almeno 0."}, // must be greater than or equal to %{number}
		{"MsgFormatoNumero", MsgFormatoNumero, "formato richiesto N/AA (es. 12/26)"},
		{"MsgNumeroEsistente", MsgNumeroEsistente, "Numero busta già esistente."},
	} {
		if c.got != c.atteso {
			t.Errorf("%s = %q, atteso %q", c.nome, c.got, c.atteso)
		}
	}
}
