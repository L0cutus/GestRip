package buste

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
)

// L'anagrafica delle oreficerie clienti (slice 086, ORE-01…03), come Workshop.list_external_oreficerie/0,
// create_oreficeria/2 e update_oreficeria/3 dello standalone: i casi di oreficerie_test.exs, con nomi e
// numeri inventati. La prova() ha già la riga 1 («Il negozio di prova», il negozio stesso) e la 2
// («Oreficeria Esempio»).

// oreficerieScritte è «id:nome:telefono» di ogni oreficeria (anche la riga 1), per id.
func oreficerieScritte(t *testing.T, b *Buste) string {
	t.Helper()
	var s sql.NullString
	_ = b.d.Lettura.QueryRow(`SELECT group_concat(id || ':' || nome || ':' || coalesce(telefono, '-'), ',') FROM (SELECT * FROM oreficerie ORDER BY id)`).Scan(&s)
	return s.String
}

func nomiOreficerie(t *testing.T, b *Buste) []string {
	t.Helper()
	oo, err := b.Oreficerie(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var nomi []string
	for _, o := range oo {
		nomi = append(nomi, o.Nome)
	}
	return nomi
}

func creaOreficeria(t *testing.T, b *Buste, nome, telefono string) OreficeriaCliente {
	t.Helper()
	o, err := b.CreaOreficeria(ctx, DatiOreficeria{Nome: puntatore(nome), Telefono: puntatore(telefono)}, chi)
	if err != nil {
		t.Fatalf("crea %q: %v", nome, err)
	}
	return o
}

// inventario: ORE-01
// L'elenco sono le sole oreficerie esterne (id > 1), per nome (BINARY: le maiuscole prima) e poi per id,
// con il telefono vuoto per chi non ce l'ha; la riga 1, il negozio stesso, non c'è mai. Senza oreficerie
// esterne l'elenco è vuoto.
func TestElencoOreficerie(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `DELETE FROM oreficerie WHERE id = 2`)
	if oo, err := b.Oreficerie(ctx); err != nil || len(oo) != 0 {
		t.Fatalf("elenco senza esterne: %v %v", oo, err)
	}
	esegui(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (2, 'Oreficeria Esempio', '0490000000')`)
	creaOreficeria(t, b, "Zeta", "")
	creaOreficeria(t, b, "alfa", "0490000001")
	creaOreficeria(t, b, "Beta", "")
	if got, want := strings.Join(nomiOreficerie(t, b), "|"), "Beta|Oreficeria Esempio|Zeta|alfa"; got != want {
		t.Fatalf("ordine %q, atteso %q", got, want)
	}
	oo, _ := b.Oreficerie(ctx)
	if oo[0].ID <= 1 || oo[0].Telefono != "" || oo[1].ID != 2 || oo[1].Telefono != "0490000000" || oo[3].Telefono != "0490000001" {
		t.Fatalf("righe %+v", oo)
	}
	// Due oreficerie con lo stesso nome non esistono (indice unico): l'id decide solo a parità impossibile.
	for _, o := range oo {
		if o.Nome == "Il negozio di prova" {
			t.Fatal("la riga 1 è nell'elenco")
		}
	}
}

// inventario: ORE-01, ORE-03
// La riga 1 è riservata: non si modifica, e per la modifica è come se non ci fosse («Oreficeria non
// trovata o non modificabile.»); niente cambia e non si scrive l'audit.
func TestOreficeriaRiservata(t *testing.T) {
	b, d, _ := prova(t)
	prima := oreficerieScritte(t, b)
	_, err := b.ModificaOreficeria(ctx, 1, DatiOreficeria{Nome: puntatore("Alterato"), Telefono: puntatore("333")}, chi)
	if !errors.Is(err, ErrOreficeriaNonTrovata) || err.Error() != "Oreficeria non trovata o non modificabile." {
		t.Fatalf("riga 1: %v", err)
	}
	if oreficerieScritte(t, b) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != 0 {
		t.Fatal("la riga 1 è stata scritta")
	}
}

// inventario: ORE-02
// Il nome si ripulisce dagli spazi ai bordi ed è obbligatorio (anche se manca); il telefono si
// normalizza e, senza cifre o senza il campo, è NULL. Riga d'audit oreficeria.created senza dati.
func TestCreaOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	prima := oreficerieScritte(t, b)
	for _, nome := range []*string{puntatore("   "), puntatore(""), puntatore(" 　"), nil} {
		_, err := b.CreaOreficeria(ctx, DatiOreficeria{Nome: nome, Telefono: puntatore("333")}, chi)
		if !errors.Is(err, ErrNomeOreficeria) || err.Error() != "Il nome è obbligatorio." {
			t.Fatalf("nome %v: %v", nome, err)
		}
	}
	if oreficerieScritte(t, b) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != 0 {
		t.Fatal("un rifiuto ha scritto")
	}
	o := creaOreficeria(t, b, "  Gioielleria Rossi  ", " +39 (333) 12-34/56. ")
	if o.Nome != "Gioielleria Rossi" || o.Telefono != "+39333123456" || o.ID <= 2 {
		t.Fatalf("creata %+v", o)
	}
	if got := ultimoAudit(t, d, "oreficeria.created"); got != "oreficeria.created|oreficeria|"+itoa(o.ID)+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = 'oreficeria.created'`); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
	senza := creaOreficeria(t, b, "Senza telefono", " interno ")
	mancante, err := b.CreaOreficeria(ctx, DatiOreficeria{Nome: puntatore("Senza campo")}, chi)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{senza.ID, mancante.ID} {
		if v := valore(t, d, `SELECT telefono FROM oreficerie WHERE id = ?`, id); v != nil {
			t.Fatalf("telefono %v, atteso NULL", v)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'oreficeria.created'`); n != 3 {
		t.Fatalf("righe d'audit %d", n)
	}
}

// inventario: ORE-02, ORE-03
// Il doppione: alla creazione a maiuscole esatte (l'indice unico, che vede anche la riga 1), alla
// modifica senza distinguerle (LOWER, solo ASCII come SQLite) ma solo fra le oreficerie esterne; il
// messaggio nomina il nome ripulito. Un rifiuto non scrive niente, nemmeno il telefono.
func TestNomiDoppiOreficerie(t *testing.T) {
	b, d, _ := prova(t)
	creaOreficeria(t, b, "Rossi", "")
	for _, nome := range []string{"Rossi", "  Rossi  ", "Il negozio di prova"} {
		_, err := b.CreaOreficeria(ctx, DatiOreficeria{Nome: puntatore(nome)}, chi)
		var doppio *OreficeriaDoppia
		if !errors.As(err, &doppio) || err.Error() != `Esiste già un'oreficeria con il nome "`+strings.TrimSpace(nome)+`".` {
			t.Fatalf("%q: %v", nome, err)
		}
	}
	// Con altre maiuscole alla creazione va bene.
	creaOreficeria(t, b, "ROSSI", "")
	secondo := creaOreficeria(t, b, "Bianchi", "")
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	_, err := b.ModificaOreficeria(ctx, secondo.ID, DatiOreficeria{Nome: puntatore(" rOsSi "), Telefono: puntatore("999")}, chi)
	var doppio *OreficeriaDoppia
	if !errors.As(err, &doppio) || err.Error() != `Esiste già un'oreficeria con il nome "rOsSi".` {
		t.Fatalf("modifica: %v", err)
	}
	if v := valore(t, d, `SELECT nome || ':' || coalesce(telefono, '-') FROM oreficerie WHERE id = ?`, secondo.ID); v != "Bianchi:-" {
		t.Fatalf("dopo il rifiuto %v", v)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Fatal("il rifiuto ha scritto l'audit")
	}
	// La riga 1 non conta per il confronto senza maiuscole (external_oreficerie): «IL NEGOZIO DI PROVA» passa;
	// il suo nome esatto no, lo ferma l'indice unico, che la vede.
	if _, err := b.ModificaOreficeria(ctx, secondo.ID, DatiOreficeria{Nome: puntatore("IL NEGOZIO DI PROVA")}, chi); err != nil {
		t.Fatalf("maiuscole del negozio: %v", err)
	}
	terza := creaOreficeria(t, b, "Neri", "")
	if _, err := b.ModificaOreficeria(ctx, terza.ID, DatiOreficeria{Nome: puntatore("Il negozio di prova")}, chi); !errors.As(err, &doppio) ||
		err.Error() != `Esiste già un'oreficeria con il nome "Il negozio di prova".` {
		t.Fatalf("nome esatto del negozio: %v", err)
	}
	if v := valore(t, d, `SELECT nome FROM oreficerie WHERE id = ?`, terza.ID); v != "Neri" {
		t.Fatalf("dopo l'indice unico %v", v)
	}
	// Alla creazione anche il nome del negozio con altre maiuscole va bene (l'indice è esatto).
	creaOreficeria(t, b, "il negozio di prova", "")
	// Le maiuscole Unicode non contano per LOWER di SQLite: «Èrre» e «èrre» sono due nomi.
	creaOreficeria(t, b, "Èrre", "")
	if _, err := b.ModificaOreficeria(ctx, secondo.ID, DatiOreficeria{Nome: puntatore("èrre")}, chi); err != nil {
		t.Fatalf("èrre: %v", err)
	}
	// Il proprio nome con altre maiuscole non è un doppione; «ÈRRE» sì: LOWER lo fa «Èrre».
	if _, err := b.ModificaOreficeria(ctx, secondo.ID, DatiOreficeria{Nome: puntatore("èRRE")}, chi); err != nil {
		t.Fatalf("èRRE: %v", err)
	}
	if _, err := b.ModificaOreficeria(ctx, secondo.ID, DatiOreficeria{Nome: puntatore("ÈRRE")}, chi); err == nil || err.Error() != `Esiste già un'oreficeria con il nome "ÈRRE".` {
		t.Fatalf("ÈRRE: %v", err)
	}
	verdi := creaOreficeria(t, b, "Verdi", "")
	if _, err := b.ModificaOreficeria(ctx, verdi.ID, DatiOreficeria{Nome: puntatore("VERDI")}, chi); err != nil {
		t.Fatalf("VERDI: %v", err)
	}
}

// inventario: ORE-03
// La modifica riscrive nome e telefono e tiene l'id; un telefono che manca diventa NULL (come
// Map.update/4 di normalize_attrs); il nome vuoto o un id che non c'è sono rifiutati senza scrivere.
// Riga oreficeria.updated senza dati. Le buste restano collegate per id.
func TestModificaAnagraficaOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	o := creaOreficeria(t, b, "Originale", "111")
	busta := crea(t, b, con(modulo(), func(m *Accettazione) { m.OreficeriaID = itoa(o.ID) }))
	m, err := b.ModificaOreficeria(ctx, o.ID, DatiOreficeria{Nome: puntatore("  Rinominata  "), Telefono: puntatore("(02) 12.34")}, chi)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != o.ID || m.Nome != "Rinominata" || m.Telefono != "021234" {
		t.Fatalf("modificata %+v", m)
	}
	if got := ultimoAudit(t, d, "oreficeria.updated"); got != "oreficeria.updated|oreficeria|"+itoa(o.ID)+"|ok|{}" {
		t.Fatalf("audit %q", got)
	}
	if v := valore(t, d, `SELECT o.nome FROM buste b JOIN oreficerie o ON o.id = b.oreficeria_id WHERE b.id = ?`, busta.ID); v != "Rinominata" {
		t.Fatalf("busta %v", v)
	}
	prima := oreficerieScritte(t, b)
	audit := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, nome := range []*string{puntatore("  "), nil} {
		if _, err := b.ModificaOreficeria(ctx, o.ID, DatiOreficeria{Nome: nome, Telefono: puntatore("5")}, chi); !errors.Is(err, ErrNomeOreficeria) {
			t.Fatalf("nome %v: %v", nome, err)
		}
	}
	for _, id := range []int64{0, -1, 999} {
		_, err := b.ModificaOreficeria(ctx, id, DatiOreficeria{Nome: puntatore("Alterata"), Telefono: puntatore("222")}, chi)
		if !errors.Is(err, ErrOreficeriaNonTrovata) || err.Error() != "Oreficeria non trovata o non modificabile." {
			t.Fatalf("id %d: %v", id, err)
		}
	}
	// Un id che non c'è, o la riga 1, con il nome vuoto: prima «non trovata», come update_oreficeria.
	for _, id := range []int64{999, 1} {
		if _, err := b.ModificaOreficeria(ctx, id, DatiOreficeria{Nome: puntatore("  ")}, chi); !errors.Is(err, ErrOreficeriaNonTrovata) {
			t.Fatalf("id %d con nome vuoto: %v", id, err)
		}
	}
	if oreficerieScritte(t, b) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit {
		t.Fatal("un rifiuto ha scritto")
	}
	if _, err := b.ModificaOreficeria(ctx, o.ID, DatiOreficeria{Nome: puntatore("Rinominata")}, chi); err != nil {
		t.Fatal(err)
	}
	if v := valore(t, d, `SELECT telefono FROM oreficerie WHERE id = ?`, o.ID); v != nil {
		t.Fatalf("telefono %v, atteso NULL", v)
	}
}

// inventario: ORE-03
// Non esiste un'eliminazione delle oreficerie: l'API non ne ha una, e il database tiene le buste
// e i lotti (ON DELETE RESTRICT) anche se qualcuno ci provasse.
func TestOreficerieNonSiEliminano(t *testing.T) {
	b, d, _ := prova(t)
	o := creaOreficeria(t, b, "Con busta", "")
	crea(t, b, con(modulo(), func(m *Accettazione) { m.OreficeriaID = itoa(o.ID) }))
	if _, err := d.Scrittura.Exec(`DELETE FROM oreficerie WHERE id = ?`, o.ID); err == nil {
		t.Fatal("una oreficeria con delle buste si è cancellata")
	}
	if conta(t, d, `SELECT count(*) FROM oreficerie WHERE id = ?`, o.ID) != 1 {
		t.Fatal("oreficeria sparita")
	}
}

// inventario: ORE-02, ORE-03
// Riga d'audit e scrittura stanno in una transazione: se l'audit non si scrive, niente cambia (Multi di
// Ecto).
func TestOreficerieAuditNellaTransazione(t *testing.T) {
	b, d, _ := prova(t)
	o := creaOreficeria(t, b, "Ferma", "1")
	esegui(t, d, `CREATE TRIGGER no_audit BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`)
	prima := oreficerieScritte(t, b)
	if _, err := b.CreaOreficeria(ctx, DatiOreficeria{Nome: puntatore("Nuova")}, chi); err == nil {
		t.Fatal("creata senza audit")
	}
	if _, err := b.ModificaOreficeria(ctx, o.ID, DatiOreficeria{Nome: puntatore("Cambiata"), Telefono: puntatore("2")}, chi); err == nil {
		t.Fatal("modificata senza audit")
	}
	if oreficerieScritte(t, b) != prima {
		t.Fatalf("scritto senza audit: %s", oreficerieScritte(t, b))
	}
}
