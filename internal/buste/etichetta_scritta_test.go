package buste

import (
	"errors"
	"reflect"
	"testing"
)

// Il campo «Etichetta (0045/26)» (D-64, slice 053): il codice letto resta, un numero scritto a mano
// diventa il codice dell'etichetta con l'anno scritto o quello della data di accettazione, il resto
// resta com'è; l'avviso delle gemelle aperte alla creazione; le gemelle della scheda.

// inventario: RIP-H-39
func TestEtichettaScritta(t *testing.T) {
	for _, c := range []struct {
		scritto string
		anno    int
		codice  string
	}{
		{"45", 2026, "RIP-2026-0045"},
		{"45", 2025, "RIP-2025-0045"}, // l'anno è quello della data di accettazione, non di oggi
		{"0045", 2026, "RIP-2026-0045"},
		{"045", 2026, "RIP-2026-0045"},
		{"1", 2026, "RIP-2026-0001"},
		{"9999", 2026, "RIP-2026-9999"},
		{"0045/26", 2026, "RIP-2026-0045"},
		{"45/26", 2026, "RIP-2026-0045"},
		{"0045/25", 2026, "RIP-2025-0045"}, // etichetta dell'anno prima: l'anno scritto vince
		{"0045/27", 2026, "RIP-2027-0045"},
		{"0045/26", 2025, "RIP-2026-0045"},
		{"45/99", 2026, "RIP-2099-0045"}, // lo stesso secolo della data di accettazione
		{" 45 ", 2026, "RIP-2026-0045"},
		{" 0045/26 ", 2026, "RIP-2026-0045"},
		{"RIP-2026-0045", 2025, "RIP-2026-0045"}, // il codice letto resta quello
		{" RIP-2025-0045 ", 2026, "RIP-2025-0045"},
		{"RIP-2026-10000", 2026, "RIP-2026-10000"},
		// Slice 055d: «rip-» minuscolo e gli spazi accanto ai separatori.
		{"rip-2026-0045", 2026, "RIP-2026-0045"},
		{"Rip-2026-0045", 2026, "RIP-2026-0045"},
		{"rip-2026- 0045", 2026, "RIP-2026-0045"},
		{"RIP - 2026 - 0045", 2026, "RIP-2026-0045"},
		{"RIP-\u00a02026-0045", 2026, "RIP-2026-0045"},
		{"0045 / 26", 2026, "RIP-2026-0045"},
		{"45 /26", 2026, "RIP-2026-0045"},
		// Il resto resta com'è, tolti solo gli spazi ai bordi.
		{"RIP 2026 0045", 2026, "RIP 2026 0045"},
		{"RIP-2026-00 45", 2026, "RIP-2026-00 45"},
		{"rip-2026-45", 2026, "rip-2026-45"},
		{"RIP-2026-45", 2026, "RIP-2026-45"},
		{"RIP-2026-0000", 2026, "RIP-2026-0000"},
		{"00045", 2026, "00045"},
		{"0", 2026, "0"},
		{"0000", 2026, "0000"},
		{"0/26", 2026, "0/26"},
		{"45/2026", 2026, "45/2026"},
		{"#45", 2026, "#45"},
		{"4 5", 2026, "4 5"},
		{"١٢", 2026, "١٢"},
		{" QR-2 ", 2026, "QR-2"},
		{"Q R-2", 2026, "Q R-2"},
		{"SINT-QR-0005", 2026, "SINT-QR-0005"},
		{"   ", 2026, ""},
		{"", 2026, ""},
	} {
		if got := EtichettaScritta(c.scritto, c.anno); got != c.codice {
			t.Errorf("%q (%d): %q, atteso %q", c.scritto, c.anno, got, c.codice)
		}
	}
}

// inventario: RIP-H-39, RIP-H-30
// La creazione salva il codice risolto, con l'anno della data di accettazione scritta (anche di un
// altro anno) e lo dà in Creata.CodiceQR; un codice di altro formato com'è; vuoto è NULL.
func TestCreaEtichettaScritta(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct {
		scritto, data string
		salvato       any
	}{
		{"rip-2026-0044", "2026-10-31", "RIP-2026-0044"},
		{"45", "2026-10-31", "RIP-2026-0045"},
		{"45", "2025-03-01", "RIP-2025-0045"},
		{"46/24", "2026-10-31", "RIP-2024-0046"},
		{"0047/26", "2025-12-31", "RIP-2026-0047"},
		{" RIP-2026-0048\t", "2026-10-31", "RIP-2026-0048"},
		{"RIP-2026-49", "2026-10-31", "RIP-2026-49"},
		{" QR-2 ", "2026-10-31", "QR-2"},
		{"  ", "2026-10-31", nil},
	} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) {
			a.CodiceQR, a.DataAccettazione, a.FormaConfermata = c.scritto, c.data, EtichettaScritta(c.scritto, 2026)
		}))
		if v := valore(t, d, `SELECT codice_qr FROM buste WHERE id = ?`, got.ID); v != c.salvato {
			t.Errorf("%q del %s: salvato %v, atteso %v", c.scritto, c.data, v, c.salvato)
		}
		if c.salvato == nil && got.CodiceQR != "" || c.salvato != nil && got.CodiceQR != c.salvato {
			t.Errorf("%q: Creata.CodiceQR %q", c.scritto, got.CodiceQR)
		}
	}
}

// gemella crea la busta e la porta allo stato, nel cestino se cestino.
func gemella(t *testing.T, b *Buste, nome, codice, stato string, cestino bool) int64 {
	t.Helper()
	id := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.CodiceQR = nome, codice })).ID
	esegui(t, b.d, `UPDATE buste SET stato = ?, compro_oro = ? WHERE id = ?`, stato, stato == "chiusa", id)
	if cestino {
		esegui(t, b.d, `UPDATE buste SET cancellata_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	}
	return id
}

// inventario: RIP-H-39
// D-64 (busta di carta riusata): un'etichetta già su una busta **aperta** si avvisa (GemelleAperte,
// con le aperte dalla più recente), senza salvare né far avanzare il contatore; con lo stesso codice
// confermato si salva. Le gemelle chiuse (consegnata, annullata, chiusa) e quelle nel cestino non
// contano. L'avviso arriva dopo tutti gli altri errori, anche il numero già usato.
func TestCreaGemelleAperte(t *testing.T) {
	b, d, _ := prova(t)
	gemella(t, b, "Chiusa uno", "RIP-2026-0045", "consegnata", false)
	gemella(t, b, "Chiusa due", "RIP-2026-0045", "annullata", false)
	gemella(t, b, "Chiusa tre", "RIP-2026-0045", "chiusa", false)
	gemella(t, b, "Cestino", "RIP-2026-0045", "in_lavorazione", true)
	gemella(t, b, "Altro anno", "RIP-2025-0045", "in_lavorazione", false)
	// Solo chiuse, nel cestino o di un altro anno: si salva senza avviso.
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "45" }), chi); err != nil {
		t.Fatalf("solo chiuse: %v", err)
	}
	aperta := gemella(t, b, "Aperta", "RIP-2026-0046", "pronta", false)
	prima := scatta(t, d)
	for _, conferma := range []string{"", "RIP-2026-0047", "46", "RIP-2025-0046"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.GemelleConfermate = "0046/26", conferma }), chi)
		var g *GemelleAperte
		if !errors.As(err, &g) || g.Codice != "RIP-2026-0046" || len(g.Righe) != 1 || g.Righe[0].ID != aperta ||
			g.Righe[0].NomeCliente != "Aperta" || g.Righe[0].Stato != "pronta" {
			t.Fatalf("conferma %q: %v %+v", conferma, err, g)
		}
		uguali(t, "conferma "+conferma, prima, scatta(t, d))
	}
	seconda := gemella(t, b, "Seconda", "RIP-2026-0046", "in_attesa_materiali", false)
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = " RIP-2026-0046 " }), chi)
	var g *GemelleAperte
	if !errors.As(err, &g) || len(g.Righe) != 2 || g.Righe[0].ID != seconda || g.Righe[1].ID != aperta {
		t.Fatalf("due aperte: %v %+v", err, g)
	}
	// Confermato (anche scritto in un'altra forma che dà lo stesso codice): si salva.
	c, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.GemelleConfermate = "46", " RIP-2026-0046 " }), chi)
	if err != nil || valore(t, d, `SELECT codice_qr FROM buste WHERE id = ?`, c.ID) != "RIP-2026-0046" {
		t.Fatalf("confermata: %v", err)
	}
	// Ogni codice, non solo le etichette.
	gemella(t, b, "Codice libero", "QR-LIBERO", "in_lavorazione", false)
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-LIBERO" }), chi); !errors.As(err, &g) || g.Codice != "QR-LIBERO" {
		t.Errorf("codice libero: %v", err)
	}
	// Un codice libero salvato con gli spazi ai bordi: gemello dello stesso scritto con gli spazi
	// (che si salva com'è) e di quello senza (review della 053).
	gemella(t, b, "Spazi", "ABC ", "in_lavorazione", false)
	for _, scritto := range []string{"ABC ", "ABC", "\u00a0ABC\t"} {
		if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = scritto }), chi); !errors.As(err, &g) || g.Codice != "ABC" || len(g.Righe) != 1 {
			t.Errorf("%q: %v", scritto, err)
		}
	}
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR = "AB" }), chi); err != nil {
		t.Errorf("AB, contenuto in ABC: %v", err)
	}
	// Prima gli errori dei campi e il numero già usato.
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.Descrizione = "46", "" }), chi); !errors.As(err, new(*CampiRifiutati)) {
		t.Errorf("campi: %v", err)
	}
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.Numero = "46", "1/26" }), chi); !errors.As(err, new(*CampiRifiutati)) {
		t.Errorf("numero già usato: %v", err)
	}
	if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.NomeCliente = "46", " " }), chi); !errors.Is(err, ErrNomeCliente) {
		t.Errorf("nome: %v", err)
	}
}

// inventario: RIP-S-01, RIP-S-02
// Le gemelle della scheda: le altre buste fuori dal cestino con lo stesso codice (il salvato senza
// spazi ai bordi), dalla più recente, senza la busta stessa; GemelleAperte e GemelleChiuse.
func TestSchedaGemelle(t *testing.T) {
	b, _, _ := prova(t)
	chiusa := gemella(t, b, "Chiusa", "RIP-2026-0045", "consegnata", false)
	gemella(t, b, "Cestino", "RIP-2026-0045", "in_lavorazione", true)
	gemella(t, b, "Altro anno", "RIP-2025-0045", "in_lavorazione", false)
	aperta := gemella(t, b, "Aperta", "RIP-2026-0045", "in_lavorazione", false)
	esegui(t, b.d, `UPDATE buste SET codice_qr = ' RIP-2026-0045 ' WHERE id = ?`, aperta)
	sola := gemella(t, b, "Sola", "RIP-2026-0099", "in_lavorazione", false)
	senza := gemella(t, b, "Senza", "", "in_lavorazione", false)
	ids := func(rr []RigaQR) []int64 {
		out := []int64{}
		for _, r := range rr {
			out = append(out, r.ID)
		}
		return out
	}
	for _, c := range []struct {
		id              int64
		gemelle, aperte []int64
		chiuse          bool
	}{
		{aperta, []int64{chiusa}, []int64{}, true},
		{sola, []int64{}, []int64{}, false},
		{senza, []int64{}, []int64{}, false},
	} {
		s, err := b.Scheda(ctx, c.id)
		if err != nil {
			t.Fatal(err)
		}
		if g := ids(s.Gemelle); !reflect.DeepEqual(g, c.gemelle) {
			t.Errorf("%d: gemelle %v, attese %v", c.id, g, c.gemelle)
		}
		if g := ids(s.GemelleAperte()); !reflect.DeepEqual(g, c.aperte) || s.GemelleChiuse() != c.chiuse {
			t.Errorf("%d: aperte %v chiuse %v", c.id, g, s.GemelleChiuse())
		}
	}
	// Dalla chiusa: l'aperta salvata con gli spazi è la stessa (il salvato si confronta normalizzato da
	// tutte e due le parti, review della 053); dalla slice 055d anche la scansione.
	s, err := b.Scheda(ctx, chiusa)
	if err != nil || !reflect.DeepEqual(ids(s.Gemelle), []int64{aperta}) {
		t.Errorf("dalla chiusa: %v %v", err, ids(s.Gemelle))
	}
	if rr, err := b.CercaPerQR(ctx, "RIP-2026-0045", false, chi); err != nil || !reflect.DeepEqual(ids(rr), []int64{aperta, chiusa}) {
		t.Errorf("scansione: %v %v", err, ids(rr))
	}
	esegui(t, b.d, `UPDATE buste SET codice_qr = 'RIP-2026-0045' WHERE id = ?`, aperta)
	nuova := gemella(t, b, "Nuova", "RIP-2026-0045", "pronta", false)
	if s, err = b.Scheda(ctx, chiusa); err != nil || !reflect.DeepEqual(ids(s.Gemelle), []int64{nuova, aperta}) ||
		!reflect.DeepEqual(ids(s.GemelleAperte()), []int64{nuova, aperta}) || s.GemelleChiuse() {
		t.Errorf("dalla chiusa: %v %v", err, ids(s.Gemelle))
	}
	if s, err = b.ApriScheda(ctx, itoa(aperta), chi); err != nil || !reflect.DeepEqual(ids(s.GemelleAperte()), []int64{nuova}) || !s.GemelleChiuse() {
		t.Errorf("apertura: %v %v", err, ids(s.Gemelle))
	}
}

// inventario: RIP-S-04, RIP-H-39
// La modifica: un testo diverso dal codice salvato passa da EtichettaScritta, con l'anno della data
// scritta nello stesso modulo (se è una data) o di quella salvata; uguale al salvato resta com'è.
func TestModificaEtichettaScritta(t *testing.T) {
	b, _, _ := prova(t)
	id := bustaDaModificare(t, b, nil) // accettata il 05/09/2026
	modifica(t, b, id, Modifica{CodiceQR: p("45")})
	if got := colonne(t, b, id, "codice_qr"); got != "RIP-2026-0045" {
		t.Errorf("45: %s", got)
	}
	modifica(t, b, id, Modifica{CodiceQR: p("46"), DataAccettazione: p("2025-01-10")})
	if got := colonne(t, b, id, "codice_qr", "data_accettazione"); got != "RIP-2025-0046|2025-01-10" {
		t.Errorf("46 con la data: %s", got)
	}
	if _, err := b.Modifica(ctx, id, Modifica{CodiceQR: p("47"), DataAccettazione: p("2025-02-30")}, nil, chi); err == nil {
		t.Error("data non valida accettata")
	}
	if got := colonne(t, b, id, "codice_qr"); got != "RIP-2025-0046" {
		t.Errorf("data rifiutata: %s", got)
	}
	modifica(t, b, id, Modifica{CodiceQR: p("0048/24")})
	if got := colonne(t, b, id, "codice_qr"); got != "RIP-2024-0048" {
		t.Errorf("0048/24: %s", got)
	}
	// Rimandato uguale al salvato: dalla slice 055d si normalizza (spazi, «rip-»), ma non si legge
	// come un numero scritto.
	esegui(t, b.d, `UPDATE buste SET codice_qr = ' rip-2024-0048 ' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{CodiceQR: p(" rip-2024-0048 "), NoteInterne: p("nota")})
	if got := colonne(t, b, id, "codice_qr"); got != "RIP-2024-0048" {
		t.Errorf("uguale al salvato: %q", got)
	}
	esegui(t, b.d, `UPDATE buste SET codice_qr = '45' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{CodiceQR: p("45")})
	if got := colonne(t, b, id, "codice_qr"); got != "45" {
		t.Errorf("45 salvato e rimandato: %q", got)
	}
	modifica(t, b, id, Modifica{CodiceQR: p("ALTRO 1 ")})
	if got := colonne(t, b, id, "codice_qr"); got != "ALTRO 1" {
		t.Errorf("altro formato: %q", got)
	}
	// Una data salvata illeggibile (solo da import): il testo com'è, mai «RIP-1-0045».
	esegui(t, b.d, `UPDATE buste SET data_accettazione = '2026-02-30' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{CodiceQR: p("45")})
	if got := colonne(t, b, id, "codice_qr"); got != "45" {
		t.Errorf("data illeggibile: %q", got)
	}
	esegui(t, b.d, `UPDATE buste SET data_accettazione = '2026-09-05' WHERE id = ?`, id)
	modifica(t, b, id, Modifica{CodiceQR: p("  ")})
	if got := colonne(t, b, id, "codice_qr"); got != "∅" {
		t.Errorf("vuoto: %q", got)
	}
}

// inventario: RIP-S-04
// Il campo «Etichetta» della modifica mostra «0045/26» solo se, rimandato così, dà esattamente il
// codice salvato (D-64, studio punto (d)); altrimenti il codice com'è.
func TestEtichettaNelModulo(t *testing.T) {
	for _, c := range []struct{ codice, data, campo string }{
		{"RIP-2026-0045", "2026-10-31", "0045/26"},
		{"RIP-2025-0045", "2026-10-31", "0045/25"},
		{"RIP-2026-0001", "2025-01-01", "0001/26"},
		{"RIP-2026-10000", "2026-10-31", "RIP-2026-10000"},
		{" RIP-2026-0045 ", "2026-10-31", " RIP-2026-0045 "},
		{"RIP-1999-0045", "2026-10-31", "RIP-1999-0045"}, // «0045/99» darebbe RIP-2099-0045
		{"RIP-2026-0045", "2026-02-30", "RIP-2026-0045"},
		{"RIP-2026-45", "2026-10-31", "RIP-2026-45"},
		{"QR-PROVA", "2026-10-31", "QR-PROVA"},
		{"", "2026-10-31", ""},
	} {
		if got := EtichettaNelModulo(c.codice, c.data); got != c.campo {
			t.Errorf("%q del %s: %q, atteso %q", c.codice, c.data, got, c.campo)
		}
		if c.campo != c.codice && EtichettaScritta(c.campo, AnnoDiAccettazione(c.data, orologio().Adesso())) != c.codice {
			t.Errorf("%q: %q non torna al codice", c.codice, c.campo)
		}
	}
}
