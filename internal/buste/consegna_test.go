package buste

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// La consegna prevista (RIP-H-37, RIP-S-06), come Repair.cast_expected_on/1; i casi da
// workshop_test.exs («an imported delivery date is recognised in both source formats…»).

// inventario: RIP-H-37, RIP-S-06
// Dal testo la data si ricava solo se il testo comincia con AAAA-MM-GG o GG/MM/AAAA e la data
// esiste.
func TestDataDalTesto(t *testing.T) {
	for _, c := range []struct{ testo, data string }{
		{"12/05/2026", "2026-05-12"}, {"2026-05-12", "2026-05-12"},
		{"12/05/2026 pomeriggio", "2026-05-12"}, {"2026-05-12 mattina", "2026-05-12"},
		{"2026-05-12T10:00", "2026-05-12"}, {"29/02/2028", "2028-02-29"},
		{"31/02/2026", ""}, {"29/02/2026", ""}, {"2026-13-01", ""}, {"2026-00-10", ""},
		{"12/5/2026", ""}, {"fine mese", ""}, {" 12/05/2026", ""}, {"entro il 12/05/2026", ""},
		{"2026-5-12", ""}, {"١٢/05/2026", ""}, {"", ""},
	} {
		got, ok := DataDalTesto(c.testo)
		if got != c.data || ok != (c.data != "") {
			t.Errorf("DataDalTesto(%q) = %q %v, attesa %q", c.testo, got, ok, c.data)
		}
	}
}

// inventario: RIP-H-37, RIP-S-06
// Il testo importato si tiene com'è, con la data ricavata quando c'è; vuoto o di soli spazi
// toglie testo e data; oltre 255 byte «Consegna troppo lunga: massimo 255 caratteri.».
func TestConsegnaDalTesto(t *testing.T) {
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	for _, c := range []struct{ in, testo, data string }{
		{"fine mese", "fine mese", "<nil>"},
		{"12/05/2026 pomeriggio", "12/05/2026 pomeriggio", "2026-05-12"},
		{"", "<nil>", "<nil>"}, {"   ", "<nil>", "<nil>"},
		{strings.Repeat("x", 255), strings.Repeat("x", 255), "<nil>"},
	} {
		got, err := LeggiConsegna(c.in)
		if err != nil || str(got.Testo) != c.testo || str(got.Data) != c.data {
			t.Errorf("LeggiConsegna(%q) = %q %q %v", c.in, str(got.Testo), str(got.Data), err)
		}
	}
	for _, lungo := range []string{strings.Repeat("x", 256), strings.Repeat("è", 128)} {
		if _, err := LeggiConsegna(lungo); !errors.Is(err, ErrConsegnaLunga) || err.Error() != "Consegna troppo lunga: massimo 255 caratteri." {
			t.Errorf("%d byte: %v", len(lungo), err)
		}
	}
}

// inventario: RIP-H-37
// Dal modulo della nuova busta arriva solo una data vera: si salva la data e il testo resta
// NULL; vuota, nessuna consegna; una data che non è AAAA-MM-GG valida è «Valore non valido.» (il
// messaggio di Ecto che lo standalone mostra) e non salva niente.
func TestConsegnaDalModulo(t *testing.T) {
	b, d, _ := prova(t)
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.DataPrevista = "2026-11-15" }))
	if v := valore(t, d, `SELECT data_prevista || '|' || coalesce(data_prevista_testo, 'NULL') FROM buste WHERE id = ?`, got.ID); v != "2026-11-15|NULL" {
		t.Errorf("consegna %v", v)
	}
	for _, vuota := range []string{"", "  "} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.DataPrevista = vuota }))
		if v := valore(t, d, `SELECT coalesce(data_prevista, data_prevista_testo, 'NULL') FROM buste WHERE id = ?`, got.ID); v != "NULL" {
			t.Errorf("%q: consegna %v", vuota, v)
		}
	}
	prima := scatta(t, d)
	for _, falsa := range []string{"15/11/2026", "2026-02-30", "fine mese", " 2026-11-15"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.DataPrevista = falsa }), chi)
		if c := erroriCampi(t, err); fmt.Sprint(c["data_prevista"]) != "[Valore non valido.]" {
			t.Errorf("%q: errori %v", falsa, c)
		}
	}
	uguali(t, "consegna non valida", prima, scatta(t, d))
}
