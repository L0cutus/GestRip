package buste

import (
	"errors"
	"testing"
)

// La stampa diretta (slice 046, D-08 e D-09, Stefano 04/11/2026): le scelte delle
// stampanti e le transazioni dopo un lavoro accettato da CUPS.

// inventario: RIP-S-48, RIP-S-43
// Le scelte: «nessuna» all'inizio; si salvano normalizzate con la loro riga; coda o opzioni non valide
// non si salvano; valori guasti nelle impostazioni valgono «nessuna» e nessuna opzione.
func TestScelteStampa(t *testing.T) {
	b, d, _ := prova(t)
	if s, err := b.ScelteStampa(ctx); err != nil || s != (ScelteStampa{}) || s.QR.Attiva() {
		t.Fatalf("%+v %v", s, err)
	}
	nuove := ScelteStampa{QR: SceltaStampa{Coda: "Etichette_QR", Opzioni: " MediaTracking=Gap\tDarkness=8 "}, Busta: SceltaStampa{Coda: "Buste"}}
	if err := b.SalvaScelteStampa(ctx, nuove, chi); err != nil {
		t.Fatal(err)
	}
	s, _ := b.ScelteStampa(ctx)
	if s.QR != (SceltaStampa{"Etichette_QR", "MediaTracking=Gap Darkness=8"}) || s.Busta != (SceltaStampa{"Buste", ""}) || !s.Busta.Attiva() {
		t.Errorf("%+v", s)
	}
	if a := ultimoAudit(t, d, "printer_settings.updated"); a != `printer_settings.updated|settings|printers|ok|{"bag_label":{"options":"","previous_queue":"","queue":"Buste"},`+
		`"label_sheet":{"options":"MediaTracking=Gap Darkness=8","previous_queue":"","queue":"Etichette_QR"}}` {
		t.Errorf("audit %s", a)
	}
	prima := scatta(t, d)
	for _, x := range []ScelteStampa{
		{QR: SceltaStampa{Coda: "-d"}}, {QR: SceltaStampa{Coda: "Due parole"}}, {Busta: SceltaStampa{Opzioni: "PageSize=A4"}},
		{Busta: SceltaStampa{Coda: "B", Opzioni: "Darkness"}},
	} {
		if err := b.SalvaScelteStampa(ctx, x, chi); !errors.Is(err, ErrSceltaStampa) {
			t.Errorf("%+v: %v", x, err)
		}
	}
	uguali(t, "scelte non valide", prima, scatta(t, d))
	esegui(t, d, `CREATE TRIGGER no_scelte BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'printer_settings.updated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.SalvaScelteStampa(ctx, ScelteStampa{}, chi); err == nil {
		t.Error("salvate senza riga")
	}
	uguali(t, "senza riga", prima, scatta(t, d))
	metti(t, b, ChiaveCodaQR, "-dAltra")
	metti(t, b, ChiaveOpzioniQR, "PageSize=A4")
	if s, _ := b.ScelteStampa(ctx); s.QR != (SceltaStampa{}) {
		t.Errorf("valori guasti: %+v", s.QR)
	}
}

// inventario: RIP-S-48
// Il foglio accettato da CUPS (D-08): formato, contatore max(ultimo, a), anno e label_sheet.printed con
// coda e lavoro in una transazione; senza la riga non si salva niente.
func TestStampaEtichetteAccettata(t *testing.T) {
	b, d, _ := prova(t)
	metti(t, b, ChiaveUltimaEtichetta, "80")
	u, err := b.StampaEtichetteAccettata(ctx, FoglioEtichette{Da: 51, A: 100, Anno: 2026, L: 57.5, H: 32}, "Etichette_QR", "Etichette_QR-7", chi)
	if err != nil || u != 100 {
		t.Fatalf("%d %v", u, err)
	}
	if a := ultimoAudit(t, d, "label_sheet.printed"); a != `label_sheet.printed|label_sheet|2026-0051-0100|ok|{"from":51,"h":"32","job":"Etichette_QR-7","last":100,"previous":80,"queue":"Etichette_QR","to":100,"w":"57.5","year":2026}` {
		t.Errorf("audit %s", a)
	}
	for k, v := range map[string]string{ChiaveAnnoEtichette: "2026", ChiaveLarghezzaQR: "57.5", ChiaveAltezzaQR: "32"} {
		if x, _ := impostazioneDi(t, b, k); x != v {
			t.Errorf("%s: %q", k, x)
		}
	}
	esegui(t, d, `CREATE TRIGGER no_stampa BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'label_sheet.printed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	prima := scatta(t, d)
	if _, err := b.StampaEtichetteAccettata(ctx, FoglioEtichette{Da: 500, A: 550, Anno: 2030, L: 40, H: 20}, "Etichette_QR", "Etichette_QR-8", chi); err == nil {
		t.Error("salvata senza riga")
	}
	uguali(t, "senza riga", prima, scatta(t, d))
}

// inventario: RIP-S-43
// L'etichetta busta da stampare (D-09): il PDF senza riga; la riga repair.label_printed solo dopo.
func TestEtichettaDaStampare(t *testing.T) {
	b, d, _ := prova(t)
	id := itoa(crea(t, b, modulo()).ID)
	pdf, e, err := b.EtichettaDaStampare(ctx, id, func(e DatiEtichetta) ([]byte, error) { return []byte("%PDF " + e.Numero), nil })
	if err != nil || string(pdf) != "%PDF "+e.Numero {
		t.Fatalf("%q %v", pdf, err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'repair.label%'`); n != 0 {
		t.Errorf("righe prima della stampa: %d", n)
	}
	if err := b.StampaEtichettaAccettata(ctx, e, "Buste", "Buste-3", chi); err != nil {
		t.Fatal(err)
	}
	if a := ultimoAudit(t, d, "repair.label_printed"); a != `repair.label_printed|repair|`+id+`|ok|{"bag_number":"`+e.Numero+`","job":"Buste-3","queue":"Buste"}` {
		t.Errorf("audit %s", a)
	}
	if _, _, err := b.EtichettaDaStampare(ctx, "999", func(DatiEtichetta) ([]byte, error) { return nil, nil }); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta che non c'è: %v", err)
	}
	if _, _, err := b.EtichettaDaStampare(ctx, id, func(DatiEtichetta) ([]byte, error) { return nil, errors.New("no") }); !errors.Is(err, ErrPDFNonFatto) {
		t.Errorf("PDF non fatto: %v", err)
	}
}
