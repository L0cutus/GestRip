package buste

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// L'anagrafica degli artigiani (slice 076, ART-01…04), come Workshop.list_artisans/0,
// create_artisan/2, update_artisan/3 e delete_artisan/2 dello standalone: i casi di
// artisans_test.exs, con nomi e numeri inventati.

// artigianiScritti è «id:nome:telefono» di ogni artigiano, per id.
func artigianiScritti(t *testing.T, b *Buste) string {
	t.Helper()
	var s sql.NullString
	_ = b.d.Lettura.QueryRow(`SELECT group_concat(id || ':' || nome || ':' || coalesce(telefono, '-'), ',') FROM (SELECT * FROM artigiani ORDER BY id)`).Scan(&s)
	return s.String
}

func nomiArtigiani(t *testing.T, b *Buste) []string {
	t.Helper()
	aa, err := b.Artigiani(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, a := range aa {
		nomi = append(nomi, a.Nome)
	}
	return nomi
}

func creaArtigiano(t *testing.T, b *Buste, nome, telefono string) Artigiano {
	t.Helper()
	a, err := b.CreaArtigiano(ctx, DatiArtigiano{Nome: puntatore(nome), Telefono: puntatore(telefono)}, chi)
	if err != nil {
		t.Fatalf("crea %q: %v", nome, err)
	}
	return a
}

// inventario: ART-01
// L'elenco è tutta l'anagrafica, per nome (BINARY: le maiuscole prima) e poi per id, con il
// telefono vuoto per un artigiano senza; nessun artigiano è riservato.
func TestElencoArtigiani(t *testing.T) {
	b, _, _ := prova(t)
	creaArtigiano(t, b, "Zeta", "")
	creaArtigiano(t, b, "alfa", "0490000001")
	creaArtigiano(t, b, "Beta", "")
	if got, want := strings.Join(nomiArtigiani(t, b), "|"), "Artigiano Esempio|Beta|Zeta|alfa"; got != want {
		t.Fatalf("ordine %q, atteso %q", got, want)
	}
	aa, _ := b.Artigiani(ctx)
	if aa[0].ID != 1 || aa[0].Telefono != "" || aa[3].Telefono != "0490000001" {
		t.Fatalf("righe %+v", aa)
	}
}

// inventario: ART-02
// Il nome si ripulisce dagli spazi ai bordi ed è obbligatorio (anche se manca); il telefono si
// normalizza e, senza cifre o senza il campo, è NULL. Riga d'audit artisan.created senza dati.
func TestCreaArtigiano(t *testing.T) {
	b, d, _ := prova(t)
	prima := artigianiScritti(t, b)
	for _, nome := range []*string{puntatore("   "), puntatore(""), puntatore(" 　"), nil} {
		_, err := b.CreaArtigiano(ctx, DatiArtigiano{Nome: nome, Telefono: puntatore("333")}, chi)
		if !errors.Is(err, ErrNomeArtigiano) || err.Error() != "Il nome è obbligatorio." {
			t.Fatalf("nome %v: %v", nome, err)
		}
	}
	if artigianiScritti(t, b) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	a := creaArtigiano(t, b, "  Mario Esempio  ", " +39 (333) 12-34/56. ")
	if a.Nome != "Mario Esempio" || a.Telefono != "+39333123456" || a.ID == 0 {
		t.Fatalf("creato %+v", a)
	}
	if got := ultimoAudit(t, d, "artisan.created"); got != "artisan.created|artisan|"+itoa(a.ID)+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'artisan.created'`); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
	senza := creaArtigiano(t, b, "Senza telefono", " interno ")
	mancante, err := b.CreaArtigiano(ctx, DatiArtigiano{Nome: puntatore("Senza campo")}, chi)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{senza.ID, mancante.ID} {
		if v := valore(t, d, `SELECT telefono FROM artigiani WHERE id = ?`, id); v != nil {
			t.Fatalf("telefono %v, atteso NULL", v)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'artisan.created'`); n != 3 {
		t.Fatalf("righe d'audit %d", n)
	}
}

// inventario: ART-02
// normalizza_telefono: un «+» in testa e le cifre che Python chiama cifre (anche ², ④, ٣, ፩),
// niente altro; senza cifre il telefono è vuoto. Il nome solo senza spazi ai bordi.
func TestNormalizzaTelefono(t *testing.T) {
	for _, c := range [][2]string{
		{" +٣-² ④AⅫ¼ ", "+٣²④"},
		{"tel ፩/⁰.₉", "፩⁰₉"},
		{"+decorazioni", ""},
		{" 02 12 ", "0212"},
		{"", ""},
		{"   ", ""},
		{"3+33", "333"},
		{" +", ""},
		{"①⑨⑴⒐⓪⓿❶➀➊🄀", "①⑨⑴⒐⓪⓿❶➀➊🄀"},
		{"⑩⑽⒑⓫❿½", ""},
		{"𐩀𐩃𐹠𑁒𑁙፱", "𐩀𐩃𐹠𑁒𑁙፱"},
		{"௧੧", "௧੧"},
	} {
		if got := NormalizzaTelefono(c[0]); got != c[1] {
			t.Errorf("NormalizzaTelefono(%q) = %q, atteso %q", c[0], got, c[1])
		}
	}
}

// inventario: ART-02, ART-03
// Il doppione: alla creazione a maiuscole esatte (l'indice unico), alla modifica senza
// distinguerle (LOWER, solo ASCII come SQLite); il messaggio nomina il nome ripulito. Un rifiuto
// non scrive niente, nemmeno il telefono.
func TestNomiDoppiArtigiani(t *testing.T) {
	b, d, _ := prova(t)
	creaArtigiano(t, b, "Rossi", "")
	for _, nome := range []string{"Rossi", "  Rossi  "} {
		_, err := b.CreaArtigiano(ctx, DatiArtigiano{Nome: puntatore(nome)}, chi)
		var doppio *ArtigianoDoppio
		if !errors.As(err, &doppio) || err.Error() != `Esiste già un artigiano con il nome "Rossi".` {
			t.Fatalf("%q: %v", nome, err)
		}
	}
	creaArtigiano(t, b, "ROSSI", "")
	secondo := creaArtigiano(t, b, "Bianchi", "")
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	_, err := b.ModificaArtigiano(ctx, secondo.ID, DatiArtigiano{Nome: puntatore(" rOsSi "), Telefono: puntatore("999")}, chi)
	var doppio *ArtigianoDoppio
	if !errors.As(err, &doppio) || err.Error() != `Esiste già un artigiano con il nome "rOsSi".` {
		t.Fatalf("modifica: %v", err)
	}
	if v := valore(t, d, `SELECT nome || ':' || coalesce(telefono, '-') FROM artigiani WHERE id = ?`, secondo.ID); v != "Bianchi:-" {
		t.Fatalf("dopo il rifiuto %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Fatal("il rifiuto ha scritto l'audit")
	}
	// Le maiuscole Unicode non contano per LOWER di SQLite: «Èrre» e «èrre» sono due nomi.
	creaArtigiano(t, b, "Èrre", "")
	if _, err := b.ModificaArtigiano(ctx, secondo.ID, DatiArtigiano{Nome: puntatore("èrre")}, chi); err != nil {
		t.Fatalf("èrre: %v", err)
	}
	// Il proprio nome con altre maiuscole non è un doppione; «ÈRRE» sì: LOWER lo fa «Èrre».
	if _, err := b.ModificaArtigiano(ctx, secondo.ID, DatiArtigiano{Nome: puntatore("èRRE")}, chi); err != nil {
		t.Fatalf("èRRE: %v", err)
	}
	if _, err := b.ModificaArtigiano(ctx, secondo.ID, DatiArtigiano{Nome: puntatore("ÈRRE")}, chi); err == nil || err.Error() != `Esiste già un artigiano con il nome "ÈRRE".` {
		t.Fatalf("ÈRRE: %v", err)
	}
	terzo := creaArtigiano(t, b, "Verdi", "")
	if _, err := b.ModificaArtigiano(ctx, terzo.ID, DatiArtigiano{Nome: puntatore("VERDI")}, chi); err != nil {
		t.Fatalf("VERDI: %v", err)
	}
}

// inventario: ART-03
// La modifica riscrive nome e telefono e tiene l'id; un telefono che manca diventa NULL (come
// Map.update/4 di normalize_attrs); il nome vuoto o un id che non c'è sono rifiutati senza
// scrivere. Riga artisan.updated senza dati. Le buste restano collegate per id.
func TestModificaAnagraficaArtigiano(t *testing.T) {
	b, d, _ := prova(t)
	a := creaArtigiano(t, b, "Originale", "111")
	busta := crea(t, b, con(modulo(), func(m *Accettazione) { m.ArtigianoID = itoa(a.ID) }))
	m, err := b.ModificaArtigiano(ctx, a.ID, DatiArtigiano{Nome: puntatore("  Rinominato  "), Telefono: puntatore("(02) 12.34")}, chi)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != a.ID || m.Nome != "Rinominato" || m.Telefono != "021234" {
		t.Fatalf("modificato %+v", m)
	}
	if got := ultimoAudit(t, d, "artisan.updated"); got != "artisan.updated|artisan|"+itoa(a.ID)+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	if v := valore(t, d, `SELECT a.nome FROM buste b JOIN artigiani a ON a.id = b.artigiano_id WHERE b.id = ?`, busta.ID); v != "Rinominato" {
		t.Fatalf("busta %v", v)
	}
	prima := artigianiScritti(t, b)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, nome := range []*string{puntatore("  "), nil} {
		if _, err := b.ModificaArtigiano(ctx, a.ID, DatiArtigiano{Nome: nome, Telefono: puntatore("5")}, chi); !errors.Is(err, ErrNomeArtigiano) {
			t.Fatalf("nome %v: %v", nome, err)
		}
	}
	for _, id := range []int64{0, -1, 999} {
		_, err := b.ModificaArtigiano(ctx, id, DatiArtigiano{Nome: puntatore("Alterata"), Telefono: puntatore("222")}, chi)
		if !errors.Is(err, ErrArtigianoNonTrovato) || err.Error() != "Artigiano non trovato." {
			t.Fatalf("id %d: %v", id, err)
		}
	}
	// Un id che non c'è con il nome vuoto: prima «non trovato», come update_artisan (get_artisan).
	if _, err := b.ModificaArtigiano(ctx, 999, DatiArtigiano{Nome: puntatore("  ")}, chi); !errors.Is(err, ErrArtigianoNonTrovato) {
		t.Fatalf("id 999 con nome vuoto: %v", err)
	}
	if artigianiScritti(t, b) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Fatal("un rifiuto ha scritto")
	}
	if _, err := b.ModificaArtigiano(ctx, a.ID, DatiArtigiano{Nome: puntatore("Rinominato")}, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT telefono FROM artigiani WHERE id = ?`, a.ID); v != nil {
		t.Fatalf("telefono %v, atteso NULL", v)
	}
}

// inventario: ART-04
// L'eliminazione rifiuta un artigiano con delle buste (anche nel cestino) e poi uno con delle
// consegne (anche vuote), con il conteggio al singolare o al plurale; libero, si elimina con la
// riga artisan.deleted. Un id che non c'è: «Artigiano non trovato.».
func TestEliminaArtigiano(t *testing.T) {
	b, d, _ := prova(t)
	a := creaArtigiano(t, b, "Occupato", "")
	conA := func(m *Accettazione) { m.ArtigianoID = itoa(a.ID) }
	prima := crea(t, b, con(modulo(), conA))
	seconda := crea(t, b, con(modulo(), conA))
	esegui(t, d, `INSERT INTO consegne_artigiani (artigiano_id, creato_il, aggiornato_il) VALUES (?, ?, ?)`, a.ID, istanteProva, istanteProva)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	rifiuto := func(atteso string) {
		t.Helper()
		err := b.EliminaArtigiano(ctx, a.ID, chi)
		if err == nil || err.Error() != atteso {
			t.Fatalf("eliminazione: %v, atteso %q", err, atteso)
		}
		if conta(t, d, `SELECT count(*) FROM artigiani WHERE id = ?`, a.ID) != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
			t.Fatal("il rifiuto ha scritto")
		}
	}
	rifiuto("Impossibile eliminare: questo artigiano ha 2 riparazioni associate.")
	var inUso *ArtigianoInUso
	if err := b.EliminaArtigiano(ctx, a.ID, chi); !errors.As(err, &inUso) || inUso.Buste != 2 {
		t.Fatalf("errore %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, prima.ID)
	rifiuto("Impossibile eliminare: questo artigiano ha 2 riparazioni associate.")
	esegui(t, d, `UPDATE buste SET artigiano_id = NULL WHERE id = ?`, seconda.ID)
	rifiuto("Impossibile eliminare: questo artigiano ha 1 riparazione associata.")
	esegui(t, d, `UPDATE buste SET artigiano_id = NULL WHERE id = ?`, prima.ID)
	rifiuto("Impossibile eliminare: questo artigiano ha 1 sessione di consegna.")
	var conConsegne *ArtigianoConConsegne
	if err := b.EliminaArtigiano(ctx, a.ID, chi); !errors.As(err, &conConsegne) || conConsegne.Consegne != 1 {
		t.Fatalf("errore %v", err)
	}
	esegui(t, d, `INSERT INTO consegne_artigiani (artigiano_id, chiusa_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?)`, a.ID, istanteProva, istanteProva, istanteProva)
	rifiuto("Impossibile eliminare: questo artigiano ha 2 sessioni di consegna.")
	libero := creaArtigiano(t, b, "Libero", "")
	if err := b.EliminaArtigiano(ctx, libero.ID, chi); err != nil {
		t.Fatal(err)
	}
	if conta(t, d, `SELECT count(*) FROM artigiani WHERE id = ?`, libero.ID) != 0 {
		t.Fatal("non eliminato")
	}
	if got := ultimoAudit(t, d, "artisan.deleted"); got != "artisan.deleted|artisan|"+itoa(libero.ID)+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	for _, id := range []int64{libero.ID, 0, 999} {
		if err := b.EliminaArtigiano(ctx, id, chi); !errors.Is(err, ErrArtigianoNonTrovato) {
			t.Fatalf("id %d: %v", id, err)
		}
	}
}

// inventario: ART-02, ART-03, ART-04
// Riga d'audit e scrittura stanno in una transazione: se l'audit non si scrive, niente cambia
// (Multi di Ecto).
func TestArtigianiAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	a := creaArtigiano(t, b, "Fermo", "1")
	esegui(t, d, `CREATE TRIGGER no_audit BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`)
	prima := artigianiScritti(t, b)
	if _, err := b.CreaArtigiano(ctx, DatiArtigiano{Nome: puntatore("Nuovo")}, chi); err == nil {
		t.Fatal("creato senza audit")
	}
	if _, err := b.ModificaArtigiano(ctx, a.ID, DatiArtigiano{Nome: puntatore("Cambiato"), Telefono: puntatore("2")}, chi); err == nil {
		t.Fatal("modificato senza audit")
	}
	if err := b.EliminaArtigiano(ctx, a.ID, chi); err == nil {
		t.Fatal("eliminato senza audit")
	}
	if artigianiScritti(t, b) != prima {
		t.Fatalf("scritto senza audit: %s", artigianiScritti(t, b))
	}
}
