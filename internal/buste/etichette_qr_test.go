package buste

import (
	"errors"
	"strings"
	"testing"
)

// Le etichette QR prenumerate (RIP-S-47, RIP-S-48): i casi vengono da label_sheet_test.exs e
// label_sheets_test.exs dello standalone (slice 210), più le deviazioni D-08, D-22, D-64 e D-66.

func parametri(m map[string]string) func(string) Parametro {
	return func(n string) Parametro {
		v, ok := m[n]
		return Parametro{Valore: v, Presente: ok, Testo: ok}
	}
}

func foglio(t *testing.T, m map[string]string) FoglioEtichette {
	t.Helper()
	f, err := LeggiFoglio(parametri(m), 2026)
	if err != nil {
		t.Fatalf("%v: %v", m, err)
	}
	return f
}

// inventario: RIP-S-48
func TestLeggiFoglioComeLabelSheetParse(t *testing.T) {
	for _, c := range []struct {
		in   map[string]string
		vuoi FoglioEtichette
	}{
		{map[string]string{}, FoglioEtichette{1, 50, 2026, 57, 32}},
		{map[string]string{"da": "0"}, FoglioEtichette{1, 50, 2026, 57, 32}},
		{map[string]string{"da": "-4"}, FoglioEtichette{1, 50, 2026, 57, 32}},
		{map[string]string{"da": " 12 "}, FoglioEtichette{12, 61, 2026, 57, 32}},
		{map[string]string{"da": "\u00a012\u2003"}, FoglioEtichette{12, 61, 2026, 57, 32}},
		{map[string]string{"da": "+12", "a": "12"}, FoglioEtichette{12, 12, 2026, 57, 32}},
		{map[string]string{"da": "1", "a": "100"}, FoglioEtichette{1, 100, 2026, 57, 32}},
		{map[string]string{"da": "51", "a": "150"}, FoglioEtichette{51, 150, 2026, 57, 32}},
		{map[string]string{"da": "7"}, FoglioEtichette{7, 56, 2026, 57, 32}},
		{map[string]string{"w": "16", "h": "200"}, FoglioEtichette{1, 50, 2026, 16, 200}},
		{map[string]string{"w": "1e2", "h": "57.5"}, FoglioEtichette{1, 50, 2026, 100, 57.5}},
		{map[string]string{"w": "5.7E1", "h": ".5e2"}, FoglioEtichette{1, 50, 2026, 57, 50}},
		{map[string]string{"w": "57.", "h": "+32"}, FoglioEtichette{1, 50, 2026, 57, 32}},
		{map[string]string{"anno": "2027"}, FoglioEtichette{1, 50, 2027, 57, 32}},
		{map[string]string{"anno": "1"}, FoglioEtichette{1, 50, 1, 57, 32}},
		{map[string]string{"da": "9999", "a": "10000"}, FoglioEtichette{9999, 10000, 2026, 57, 32}},
		{map[string]string{"da": "999999999999999999", "a": "999999999999999999"}, FoglioEtichette{999999999999999999, 999999999999999999, 2026, 57, 32}},
	} {
		if f := foglio(t, c.in); f != c.vuoi {
			t.Errorf("%v: %+v, atteso %+v", c.in, f, c.vuoi)
		}
	}
}

// inventario: RIP-S-48
func TestLeggiFoglioRifiuti(t *testing.T) {
	for _, m := range []map[string]string{
		{"da": "5", "a": "2"}, {"da": "5", "a": "0"}, {"w": "15"}, {"w": "15.9"}, {"w": "200.5"}, {"h": "15"}, {"h": "201"},
		{"h": "inf"}, {"h": "nan"}, {"w": "1e400"}, {"h": "1e-400"}, {"h": "-1e400"}, {"w": "1.e5"}, {"w": "57,5"},
		{"da": "abc"}, {"da": "1.5"}, {"a": ""}, {"anno": ""}, {"anno": "x"}, {"w": "abc"}, {"w": ""}, {"w": "0x10"},
		{"da": strings.Repeat("9", 19)}, {"da": "1_0"}, {"da": "٥٧"}, {"da": "\u200b5"},
		{"da": "1", "a": "500", "w": "15"},
		{"w": "+" + strings.Repeat("0", 26) + "57"},
	} {
		if _, err := LeggiFoglio(parametri(m), 2026); !errors.Is(err, ErrRichiestaEtichette) {
			t.Errorf("%v: %v", m, err)
		}
	}
	// Un valore che non è un testo (Plug: lista o mappa) non è valido.
	if _, err := LeggiFoglio(func(n string) Parametro { return Parametro{Presente: n == "da"} }, 2026); !errors.Is(err, ErrRichiestaEtichette) {
		t.Errorf("lista: %v", err)
	}
}

// inventario: RIP-S-48
// Slice 210: oltre 100 etichette il rifiuto dice quante, con la richiesta com'era; mai tagliata.
func TestLeggiFoglioTroppe(t *testing.T) {
	for _, c := range []struct {
		in map[string]string
		n  int64
		f  FoglioEtichette
	}{
		{map[string]string{"da": "1", "a": "101"}, 101, FoglioEtichette{1, 101, 2026, 57, 32}},
		{map[string]string{"da": "51", "a": "9999"}, 9949, FoglioEtichette{51, 9999, 2026, 57, 32}},
		{map[string]string{"da": "301", "a": "401", "anno": "2027", "w": "40", "h": "20"}, 101, FoglioEtichette{301, 401, 2027, 40, 20}},
	} {
		_, err := LeggiFoglio(parametri(c.in), 2026)
		var tr TroppeEtichette
		if !errors.As(err, &tr) || tr.N != c.n || tr.Foglio != c.f {
			t.Errorf("%v: %v %+v", c.in, err, tr)
		}
	}
	if s := (TroppeEtichette{N: 101}).Error(); s != "Massimo 100 etichette per volta: hai chiesto 101." {
		t.Errorf("messaggio %q", s)
	}
}

// inventario: RIP-S-48
// codes/1, filename/1 e il numero sotto il QR (D-21, D-64).
func TestCodiciENomi(t *testing.T) {
	for _, c := range []struct {
		anno, n        int64
		codice, numero string
	}{
		{2026, 1, "RIP-2026-0001", "0001/26"},
		{2026, 45, "RIP-2026-0045", "0045/26"},
		{2027, 9999, "RIP-2027-9999", "9999/27"},
		{2026, 10000, "RIP-2026-10000", "10000/26"},
		{2100, 7, "RIP-2100-0007", "0007/00"},
		{5, 7, "RIP-5-0007", "0007/05"},
	} {
		if g := CodiceEtichetta(c.anno, c.n); g != c.codice {
			t.Errorf("codice %d/%d: %q", c.n, c.anno, g)
		}
		if g := NumeroEtichetta(c.anno, c.n); g != c.numero {
			t.Errorf("numero %d/%d: %q", c.n, c.anno, g)
		}
	}
	for _, c := range []struct {
		f    FoglioEtichette
		nome string
	}{
		{FoglioEtichette{Da: 1, A: 50, Anno: 2026}, "etichette-qr-2026-0001-0050.pdf"},
		{FoglioEtichette{Da: 42, A: 42, Anno: 2027}, "etichette-qr-2027-0042-0042.pdf"},
		{FoglioEtichette{Da: 9999, A: 10000, Anno: 2026}, "etichette-qr-2026-9999-10000.pdf"},
	} {
		if g := NomeFileEtichetteQR(c.f); g != c.nome {
			t.Errorf("%+v: %q", c.f, g)
		}
	}
}

// inventario: RIP-S-47
// format_g/1, con i valori misurati in fab-220 (Float.round/2 a metà per eccesso, Float.to_string/1).
func TestFormatoG(t *testing.T) {
	for v, s := range map[float64]string{57: "57", 200: "200", 57.5: "57.5", 16.25: "16.25", 57.123456: "57.1235", 0: "0", -5: "-5",
		1e-12: "1.0e-12", 57.03125: "57.0313", 1234.125: "1234.13", 123456.5: "123457", -2.5: "-2.5", 0.001: "0.001",
		0.0001: "0.0001", 1e-5: "1.0e-5", 16.000001: "16", 1e30: "1000000000000000019884624838656"} {
		if g := FormatoG(v); g != s {
			t.Errorf("FormatoG(%v) = %q, atteso %q", v, g, s)
		}
	}
}

func impostazioneDi(t *testing.T, b *Buste, chiave string) (string, bool) {
	t.Helper()
	v, ok, err := b.impostazione(ctx, b.d.Lettura, chiave)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

func metti(t *testing.T, b *Buste, chiave, valore string) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, chiave, valore, istanteProva)
}

// inventario: RIP-S-47
// form_defaults/1: da = ultimo+1, a = ultimo+50, l'anno di oggi, il formato salvato o 57×32; i valori
// salvati illeggibili valgono i default.
func TestModuloEtichette(t *testing.T) {
	b, _, _ := prova(t)
	m, err := b.ModuloEtichette(ctx)
	if err != nil || m != (ModuloEtichette{Ultimo: 0, Da: 1, A: 50, Anno: 2026, L: 57, H: 32}) || m.Piccolo() {
		t.Fatalf("vuoto: %+v %v", m, err)
	}
	metti(t, b, ChiaveUltimaEtichetta, "120")
	metti(t, b, ChiaveLarghezzaQR, "18")
	metti(t, b, ChiaveAltezzaQR, " 32.5 ")
	if m, _ = b.ModuloEtichette(ctx); m != (ModuloEtichette{Ultimo: 120, Da: 121, A: 170, Anno: 2026, L: 18, H: 32.5}) || !m.Piccolo() {
		t.Errorf("salvati: %+v", m)
	}
	metti(t, b, ChiaveLarghezzaQR, "0")
	if m, _ = b.ModuloEtichette(ctx); m.L != 0 || !m.Piccolo() {
		t.Errorf("zero: %+v", m)
	}
	for _, v := range []string{"", "abc", "1.5", "12x", "99999999999999999999999"} {
		metti(t, b, ChiaveUltimaEtichetta, v)
		if m, _ = b.ModuloEtichette(ctx); m.Ultimo != 0 || m.Da != 1 || m.A != 50 {
			t.Errorf("ultimo illeggibile %q: %+v", v, m)
		}
	}
	for _, v := range []string{"", "abc", "12x", "57.", ".5", "1.e5"} {
		metti(t, b, ChiaveLarghezzaQR, v)
		metti(t, b, ChiaveAltezzaQR, v)
		if m, _ = b.ModuloEtichette(ctx); m.L != 57 || m.H != 32 {
			t.Errorf("formato illeggibile %q: %+v", v, m)
		}
	}
	metti(t, b, ChiaveUltimaEtichetta, "+7")
	if m, _ = b.ModuloEtichette(ctx); m.Ultimo != 7 || m.Da != 8 {
		t.Errorf("+7: %+v", m)
	}
}

func finto(pdf []byte, err error) func(FoglioEtichette) ([]byte, error) {
	return func(FoglioEtichette) ([]byte, error) { return pdf, err }
}

// generaFoglio fa il PDF del foglio (finto) e dà il contatore dopo.
func generaFoglio(t *testing.T, b *Buste, f FoglioEtichette) int64 {
	t.Helper()
	if _, err := b.GeneraEtichette(ctx, f, chi, finto([]byte("%PDF-x"), nil)); err != nil {
		t.Fatal(err)
	}
	v, _ := impostazioneDi(t, b, ChiaveUltimaEtichetta)
	return interoSalvato(v, -1)
}

// inventario: RIP-S-48
// Dalla 046 (D-08 cambiata, Stefano 04/11/2026) il PDF salva come LabelSheets.generate/3 della 210:
// max(ultimo, a), formato e label_sheet.generated con i dati dello standalone, più l'anno del D-66;
// senza PDF o senza la riga niente.
func TestGeneraEtichetteSalvaIlContatore(t *testing.T) {
	b, d, _ := prova(t)
	f := FoglioEtichette{Da: 1, A: 3, Anno: 2026, L: 57.03125, H: 32}
	pdf, err := b.GeneraEtichette(ctx, f, chi, finto([]byte("%PDF-x"), nil))
	if err != nil || string(pdf) != "%PDF-x" {
		t.Fatalf("%q %v", pdf, err)
	}
	for k, v := range map[string]string{ChiaveUltimaEtichetta: "3", ChiaveAnnoEtichette: "2026", ChiaveLarghezzaQR: "57.0313", ChiaveAltezzaQR: "32"} {
		if x, _ := impostazioneDi(t, b, k); x != v {
			t.Errorf("%s: %q", k, x)
		}
	}
	if a := ultimoAudit(t, d, "label_sheet.generated"); a != `label_sheet.generated|label_sheet|2026-0001-0003|ok|{"from":1,"h":"32","to":3,"w":"57.0313","year":2026}` {
		t.Errorf("audit %s", a)
	}
	prima := scatta(t, d)
	if _, err := b.GeneraEtichette(ctx, FoglioEtichette{Da: 5, A: 5, Anno: 2026, L: 40, H: 20}, chi, finto(nil, errors.New("no"))); !errors.Is(err, ErrPDFNonFatto) {
		t.Errorf("senza PDF: %v", err)
	}
	esegui(t, d, `CREATE TRIGGER no_foglio BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.generated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if pdf, err := b.GeneraEtichette(ctx, FoglioEtichette{Da: 5, A: 9, Anno: 2030, L: 40, H: 20}, chi, finto([]byte("%PDF-x"), nil)); !errors.Is(err, ErrPDFNonFatto) || pdf != nil {
		t.Errorf("senza riga: %q %v", pdf, err)
	}
	uguali(t, "PDF non consegnato", prima, scatta(t, d))
}

// inventario: RIP-S-48
// Il contatore del PDF: max(ultimo, a) e l'anno più nuovo (D-66); una ristampa non torna indietro; un
// ultimo illeggibile vale 0.
func TestContatoreDelPDF(t *testing.T) {
	b, _, _ := prova(t)
	if u := generaFoglio(t, b, FoglioEtichette{Da: 1, A: 50, Anno: 2026, L: 57, H: 32}); u != 50 {
		t.Fatalf("%d", u)
	}
	if u := generaFoglio(t, b, FoglioEtichette{Da: 12, A: 12, Anno: 2025, L: 57, H: 32}); u != 50 {
		t.Errorf("ristampa: %d", u)
	}
	if v, _ := impostazioneDi(t, b, ChiaveAnnoEtichette); v != "2026" {
		t.Errorf("anno dopo una ristampa vecchia: %q", v)
	}
	if u := generaFoglio(t, b, FoglioEtichette{Da: 51, A: 100, Anno: 2027, L: 57, H: 32}); u != 100 {
		t.Errorf("avanti: %d", u)
	}
	if v, _ := impostazioneDi(t, b, ChiaveAnnoEtichette); v != "2027" {
		t.Errorf("anno: %q", v)
	}
	// Un anno fuori dal modulo conta il numero ma non diventa l'anno salvato (D-66).
	if u := generaFoglio(t, b, FoglioEtichette{Da: 101, A: 101, Anno: 20267, L: 57, H: 32}); u != 101 {
		t.Errorf("20267: %d", u)
	}
	if v, _ := impostazioneDi(t, b, ChiaveAnnoEtichette); v != "2027" {
		t.Errorf("anno dopo il 20267: %q", v)
	}
	metti(t, b, ChiaveUltimaEtichetta, "abc")
	if u := generaFoglio(t, b, FoglioEtichette{Da: 3, A: 4, Anno: 2027, L: 57, H: 32}); u != 4 {
		t.Errorf("illeggibile: %d", u)
	}
}

// inventario: RIP-S-47
// D-22 e D-66: l'azzeramento porta il contatore a 0 con la sua riga; senza la riga niente. La
// proposta del nuovo anno c'è solo con l'anno dell'ultimo foglio generato noto e più vecchio di
// oggi e il contatore non a zero, e sparisce dopo l'azzeramento.
func TestAzzeraEtichetteENuovoAnno(t *testing.T) {
	b, d, o := prova(t)
	metti(t, b, ChiaveUltimaEtichetta, "300") // dati importati: l'anno non c'è
	if m, _ := b.ModuloEtichette(ctx); m.NuovoAnno {
		t.Error("proposta senza l'anno dell'ultimo foglio")
	}
	generaFoglio(t, b, FoglioEtichette{Da: 301, A: 350, Anno: 2026, L: 57, H: 32})
	if m, _ := b.ModuloEtichette(ctx); m.NuovoAnno {
		t.Error("proposta nello stesso anno")
	}
	o.Imposta(o.Adesso().AddDate(0, 3, 0)) // gennaio 2027
	m, _ := b.ModuloEtichette(ctx)
	if !m.NuovoAnno || m.Anno != 2027 || m.Ultimo != 350 {
		t.Errorf("gennaio: %+v", m)
	}
	esegui(t, d, `CREATE TRIGGER no_azzera BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.counter_reset' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.AzzeraEtichette(ctx, true, 2027, chi); err == nil {
		t.Error("azzeramento senza riga riuscito")
	}
	if v, _ := impostazioneDi(t, b, ChiaveUltimaEtichetta); v != "350" {
		t.Errorf("contatore senza riga: %q", v)
	}
	if v, _ := impostazioneDi(t, b, ChiaveAnnoEtichette); v != "2026" {
		t.Errorf("anno senza riga: %q", v)
	}
	esegui(t, d, `DROP TRIGGER no_azzera`)
	if err := b.AzzeraEtichette(ctx, true, 2027, chi); err != nil {
		t.Fatal(err)
	}
	if a := ultimoAudit(t, d, "label_sheet.counter_reset"); a != `label_sheet.counter_reset|label_sheet|counter|ok|{"new_year":true,"previous":350,"year":2027}` {
		t.Errorf("audit %s", a)
	}
	m, _ = b.ModuloEtichette(ctx)
	if m.NuovoAnno || m.Ultimo != 0 || m.Da != 1 || m.A != 50 {
		t.Errorf("dopo l'azzeramento: %+v", m)
	}
	generaFoglio(t, b, FoglioEtichette{Da: 1, A: 50, Anno: 2027, L: 57, H: 32})
	if m, _ = b.ModuloEtichette(ctx); m.NuovoAnno {
		t.Error("proposta dopo il primo foglio del nuovo anno")
	}
	if err := b.AzzeraEtichette(ctx, false, 2027, chi); err != nil {
		t.Fatal(err)
	}
	if a := ultimoAudit(t, d, "label_sheet.counter_reset"); a != `label_sheet.counter_reset|label_sheet|counter|ok|{"new_year":false,"previous":50,"year":2027}` {
		t.Errorf("audit %s", a)
	}
	metti(t, b, ChiaveUltimaEtichetta, "5")
	metti(t, b, ChiaveAnnoEtichette, "abc")
	if m, _ = b.ModuloEtichette(ctx); m.NuovoAnno {
		t.Error("proposta con un anno illeggibile")
	}
}

// inventario: RIP-S-47
// D-66 sull'anno scelto: la proposta vale anche per l'anno del foglio (il 28/12 si prepara il foglio
// dell'anno dopo); dopo l'azzeramento per il 2027 una ristampa del 2026 non la fa tornare;
// l'azzeramento senza ultimo numero salvato dà previous 0.
func TestNuovoAnnoSulFoglio(t *testing.T) {
	b, d, _ := prova(t)
	if err := b.AzzeraEtichette(ctx, false, 2026, chi); err != nil {
		t.Fatal(err)
	}
	if a := ultimoAudit(t, d, "label_sheet.counter_reset"); a != `label_sheet.counter_reset|label_sheet|counter|ok|{"new_year":false,"previous":0,"year":2026}` {
		t.Errorf("audit %s", a)
	}
	generaFoglio(t, b, FoglioEtichette{Da: 1, A: 350, Anno: 2026, L: 57, H: 32})
	m, _ := b.ModuloEtichette(ctx) // 31/10/2026
	if m.NuovoAnno || m.NuovoAnnoPer(2026) || !m.NuovoAnnoPer(2027) {
		t.Fatalf("28/12: %+v", m)
	}
	if c := m.ConAnno(2027); c.Anno != 2027 || !c.NuovoAnno {
		t.Errorf("ConAnno: %+v", c)
	}
	if err := b.AzzeraEtichette(ctx, true, 2027, chi); err != nil {
		t.Fatal(err)
	}
	if v, _ := impostazioneDi(t, b, ChiaveAnnoEtichette); v != "2027" {
		t.Errorf("anno dopo l'azzeramento: %q", v)
	}
	generaFoglio(t, b, FoglioEtichette{Da: 12, A: 12, Anno: 2026, L: 57, H: 32})
	m, _ = b.ModuloEtichette(ctx)
	if m.Ultimo != 12 || m.NuovoAnnoPer(2027) || m.NuovoAnno {
		t.Errorf("dopo la ristampa del 2026: %+v", m)
	}
	for a, ok := range map[int64]bool{2023: false, 2024: true, 2099: true, 2100: false, 20267: false} {
		if AnnoDelModulo(a) != ok {
			t.Errorf("AnnoDelModulo(%d)", a)
		}
	}
}
