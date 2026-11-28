package buste

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina del cestino e il ripristino (slice 137, Trash.list/1 e Trash.restore/4 dello standalone): l'ordine, i giorni
// rimasti sul calendario del negozio, il ripristino che tocca solo il segno del cestino. Dati inventati.

// nelCestinoIl mette la busta nel cestino all'istante dato (scrittura diretta: i test dei giorni scelgono l'istante).
func nelCestinoIl(t *testing.T, d *db.DB, id int64, il time.Time) {
	t.Helper()
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, il.UTC().Format(db.FormatoIstante), id)
}

// leggiCestino è b.Cestino o fallisce.
func leggiCestino(t *testing.T, b *Buste) Cestino {
	t.Helper()
	c, err := b.Cestino(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rigaIntera è la riga intera della busta, colonna per colonna.
func rigaIntera(t *testing.T, d *db.DB, id int64) map[string]any {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT * FROM buste WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	cc, _ := rr.Columns()
	if !rr.Next() {
		t.Fatalf("busta %d assente", id)
	}
	vv := make([]any, len(cc))
	pp := make([]any, len(cc))
	for i := range vv {
		pp[i] = &vv[i]
	}
	if err := rr.Scan(pp...); err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	for i, c := range cc {
		m[c] = vv[i]
	}
	return m
}

// inventario: RIP-S-51
// Cestino vuoto: nessuna voce, la finestra di base. Poi le buste nel cestino, dalla più recente; a pari istante l'id
// crescente; quelle fuori dal cestino no. Ogni voce porta famiglia, numero visibile (D-64: «N. 1/26» senza etichetta,
// «0045/26» con), descrizione, stato salvato e istante.
func TestCestinoOrdine(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC))
	if c := leggiCestino(t, b); c.Giorni != 30 || len(c.Voci) != 0 {
		t.Fatalf("vuoto: %+v", c)
	}
	prima := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR, a.Descrizione = "QR-PRIMA", "Collana da saldare" }))
	etichetta := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "RIP-2026-0045" }))
	pari := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-PARI" }))
	fuori := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-FUORI" }))
	esegui(t, d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-11-27' WHERE id = ?`, etichetta.ID)
	ieri := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	nelCestinoIl(t, d, prima.ID, ieri.Add(-48*time.Hour))
	nelCestinoIl(t, d, pari.ID, ieri)
	nelCestinoIl(t, d, etichetta.ID, ieri)
	c := leggiCestino(t, b)
	var ids []int64
	for _, v := range c.Voci {
		ids = append(ids, v.ID)
		if v.Famiglia != FamigliaBuste {
			t.Errorf("famiglia %q", v.Famiglia)
		}
	}
	if want := []int64{etichetta.ID, pari.ID, prima.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ordine %v, atteso %v (fuori dal cestino: %d)", ids, want, fuori.ID)
	}
	if v := c.Voci[0]; v.Etichetta != "0045/26" || v.Stato != "pronta" || !v.CancellataIl.Equal(ieri) || v.Dettaglio != "Anello da stringere" {
		t.Errorf("etichetta: %+v", v)
	}
	if v := c.Voci[2]; v.Etichetta != "N. "+prima.Numero || v.Stato != "in_lavorazione" || v.Dettaglio != "Collana da saldare" {
		t.Errorf("prima: %+v", v)
	}
	// La più recente, buttata adesso con SpostaNelCestino, va in cima.
	if _, err := b.SpostaNelCestino(ctx, fuori.ID, chi); err != nil {
		t.Fatal(err)
	}
	if c := leggiCestino(t, b); len(c.Voci) != 4 || c.Voci[0].ID != fuori.ID {
		t.Errorf("dopo lo spostamento: %+v", c.Voci)
	}
}

// inventario: RIP-S-51
// countdown/3: giorni di calendario del negozio (Roma), mai negativi, mai oltre la finestra.
func TestGiorniRimasti(t *testing.T) {
	roma := func(a int, m time.Month, g, h, mi int) time.Time { return time.Date(a, m, g, h, mi, 0, 0, clock.Roma) }
	for _, c := range []struct {
		nome            string
		cestino, oggi   time.Time
		finestra, resta int64
	}{
		{"il giorno stesso", roma(2026, 11, 28, 8, 0), roma(2026, 11, 28, 23, 59), 30, 30},
		{"un minuto dopo mezzanotte", roma(2026, 11, 28, 23, 59), roma(2026, 11, 29, 0, 1), 30, 29},
		// In UTC tutte e due le date sono il 20 gennaio: in negozio il cestino è del 21, ieri sera del 20.
		{"fuso: dopo mezzanotte a Roma", time.Date(2026, 1, 20, 23, 30, 0, 0, time.UTC), time.Date(2026, 1, 21, 8, 0, 0, 0, time.UTC), 30, 30},
		{"fuso: prima di mezzanotte a Roma", time.Date(2026, 1, 20, 22, 30, 0, 0, time.UTC), time.Date(2026, 1, 21, 8, 0, 0, 0, time.UTC), 30, 29},
		// Ora legale: il 29 marzo ha 23 ore, il 25 ottobre 25; contano come un giorno.
		{"ora legale in marzo", roma(2026, 3, 28, 12, 0), roma(2026, 3, 30, 0, 30), 30, 28},
		{"ora solare in ottobre", roma(2026, 10, 24, 12, 0), roma(2026, 10, 26, 0, 30), 30, 28},
		{"ora legale, 23 ore", roma(2026, 3, 29, 0, 30), roma(2026, 3, 29, 23, 30), 30, 30},
		{"ultimo giorno", roma(2026, 10, 1, 9, 0), roma(2026, 10, 30, 9, 0), 30, 1},
		{"finestra appena passata", roma(2026, 10, 1, 9, 0), roma(2026, 10, 31, 0, 0), 30, 0},
		{"un giorno oltre", roma(2026, 10, 1, 9, 0), roma(2026, 11, 1, 9, 0), 30, 0},
		{"un anno oltre", roma(2025, 1, 1, 9, 0), roma(2026, 11, 28, 9, 0), 30, 0},
		{"nel futuro", roma(2026, 12, 5, 9, 0), roma(2026, 11, 28, 9, 0), 30, 30},
		{"finestra di un giorno", roma(2026, 11, 27, 9, 0), roma(2026, 11, 28, 9, 0), 1, 0},
		{"finestra lunga", roma(2026, 11, 27, 9, 0), roma(2026, 11, 28, 9, 0), 365, 364},
	} {
		if got := giorniRimasti(clock.DataDi(c.oggi), c.cestino, c.finestra); got != c.resta {
			t.Errorf("%s: %d, attesi %d", c.nome, got, c.resta)
		}
	}
}

// inventario: RIP-S-51
// Gli stessi giorni dalla pagina, con l'orologio e il database: il fuso, la finestra che cambia (anche rovinata: 30),
// mai negativi.
func TestCestinoGiorniDallaPagina(t *testing.T) {
	b, d, o := prova(t)
	busta := crea(t, b, modulo())
	nelCestinoIl(t, d, busta.ID, time.Date(2026, 1, 20, 22, 30, 0, 0, time.UTC)) // 23:30 a Roma, il 20
	giorni := func() (int64, int64) {
		t.Helper()
		c := leggiCestino(t, b)
		if len(c.Voci) != 1 {
			t.Fatalf("voci %+v", c.Voci)
		}
		return c.Giorni, c.Voci[0].GiorniRimasti
	}
	o.Imposta(time.Date(2026, 1, 20, 22, 59, 0, 0, time.UTC))
	if f, g := giorni(); f != 30 || g != 30 {
		t.Errorf("la sera stessa: %d %d", f, g)
	}
	o.Imposta(time.Date(2026, 1, 20, 23, 1, 0, 0, time.UTC)) // mezzanotte passata a Roma, ancora il 20 in UTC
	if _, g := giorni(); g != 29 {
		t.Errorf("dopo mezzanotte: %d", g)
	}
	o.Imposta(time.Date(2026, 1, 23, 9, 0, 0, 0, time.UTC))
	for _, c := range []struct {
		valore      string
		finestra, g int64
	}{{"5", 5, 2}, {"3", 3, 0}, {"2", 2, 0}, {"1", 1, 0}, {"365", 365, 362}, {"0", 30, 27}, {"abc", 30, 27}} {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('trash.retention_days', ?, ?)
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, c.valore, istanteProva)
		if f, g := giorni(); f != c.finestra || g != c.g {
			t.Errorf("finestra %q: %d %d, attesi %d %d", c.valore, f, g, c.finestra, c.g)
		}
	}
}

// inventario: RIP-S-52, RIP-S-50
// Il ripristino tocca solo cancellata_il (NULL) e aggiornato_il (adesso), scrive repair.restored dell'autore senza dati,
// e la busta torna in lista, contatori, ricerca, scheda e link del cliente; anche dopo la finestra. Una seconda volta,
// una busta fuori dal cestino, un id che non c'è o non positivo, una famiglia sconosciuta: ErrNonNelCestino, niente
// scritto.
func TestRipristinaDalCestino(t *testing.T) {
	b, d, o := prova(t)
	o.Imposta(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	busta := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.CodiceQR = "Cliente Ripristino", "QR-RIPR" }))
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" }))
	esegui(t, d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-10-31' WHERE id = ?`, busta.ID)
	// Colonne facoltative piene: il ripristino non deve toccarle.
	esegui(t, d, `UPDATE buste SET ritardo_avvisato_il = ?1, sollecito_avvisato_il = ?1, link_inviato_il = ?1, fissata_il = ?1,
		ultimo_messaggio_il = ?1, email_cliente = 'cliente.ripristino@esempio.test', note_artigiano = 'Nota di prova' WHERE id = ?2`,
		istanteProva, busta.ID)
	prima := rigaIntera(t, d, busta.ID)
	o.Imposta(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	if _, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	conti := func() [4]int64 {
		t.Helper()
		l, err := b.Lista(ctx, Filtri{Pagina: 1})
		if err != nil {
			t.Fatal(err)
		}
		n, _, err := b.Selezione(ctx, Filtri{Cerca: "Cliente Ripristino"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		return [4]int64{l.Totale, l.Contatori.Pronte, n, int64(len(leggiCestino(t, b).Voci))}
	}
	if c := conti(); c != [4]int64{1, 0, 0, 1} {
		t.Fatalf("nel cestino: %v", c)
	}
	// Ben oltre la finestra: si ripristina lo stesso (nessun limite di tempo).
	adesso := time.Date(2026, 12, 10, 9, 30, 0, 654321000, time.UTC)
	o.Imposta(adesso)
	if g := leggiCestino(t, b).Voci[0].GiorniRimasti; g != 0 {
		t.Errorf("giorni %d", g)
	}
	pc := chi
	pc.Attore = "banco@esempio.test"
	if err := b.RipristinaDalCestino(ctx, FamigliaBuste, busta.ID, pc); err != nil {
		t.Fatalf("ripristino: %v", err)
	}
	dopo := rigaIntera(t, d, busta.ID)
	if dopo["cancellata_il"] != nil || dopo["aggiornato_il"] != adesso.Format(db.FormatoIstante) {
		t.Errorf("segni: %v %v", dopo["cancellata_il"], dopo["aggiornato_il"])
	}
	for _, c := range []string{"ritardo_avvisato_il", "sollecito_avvisato_il", "link_inviato_il", "email_cliente"} {
		if prima[c] == nil {
			t.Errorf("%s vuota prima del cestino: la prova non prova niente", c)
		}
	}
	delete(prima, "aggiornato_il")
	delete(dopo, "aggiornato_il")
	if !reflect.DeepEqual(prima, dopo) {
		t.Errorf("la busta non è tornata com'era:\nprima %v\ndopo  %v", prima, dopo)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.restored' AND attore = ? AND risorsa = 'repair'
		AND id_risorsa = ? AND esito = 'ok' AND dati = '{}' AND ip = ?`, pc.Attore, itoa(busta.ID), pc.IP); n != 1 {
		t.Errorf("repair.restored: %d righe", n)
	}
	if c := conti(); c != [4]int64{2, 1, 1, 0} {
		t.Errorf("ripristinata: %v", c)
	}
	if _, err := b.Scheda(ctx, busta.ID); err != nil {
		t.Errorf("scheda: %v", err)
	}
	if _, err := b.PaginaCliente(ctx, busta.Token); err != nil {
		t.Errorf("link del cliente: %v", err)
	}
	f := scatta(t, d)
	riga := rigaIntera(t, d, busta.ID)
	o.Imposta(adesso.Add(time.Hour))
	for _, c := range []struct {
		fam FamigliaCestino
		id  int64
	}{{FamigliaBuste, busta.ID}, {FamigliaBuste, altra.ID}, {FamigliaBuste, 0}, {FamigliaBuste, -1}, {FamigliaBuste, altra.ID + 100},
		{"sale", busta.ID}, {"", busta.ID}, {"REPAIR", busta.ID}} {
		if err := b.RipristinaDalCestino(ctx, c.fam, c.id, chi); !errors.Is(err, ErrNonNelCestino) {
			t.Errorf("%q %d: %v", c.fam, c.id, err)
		}
	}
	uguali(t, "rifiuti", f, scatta(t, d))
	if !reflect.DeepEqual(riga, rigaIntera(t, d, busta.ID)) {
		t.Error("un rifiuto ha cambiato la busta")
	}
}

// inventario: RIP-S-52
// Tutto o niente: senza autore, con l'audit che non si scrive o un errore prima del commit la busta resta nel cestino
// con la sua data, senza righe repair.restored.
func TestRipristinaDalCestinoTuttoONiente(t *testing.T) {
	b, d, _ := prova(t)
	busta := crea(t, b, modulo())
	if _, err := b.SpostaNelCestino(ctx, busta.ID, chi); err != nil {
		t.Fatal(err)
	}
	quando := valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID)
	resta := func(cosa string) {
		t.Helper()
		if valore(t, d, `SELECT cancellata_il FROM buste WHERE id = ?`, busta.ID) != quando ||
			conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.restored'`) != 0 {
			t.Errorf("%s: ripristinata a metà", cosa)
		}
	}
	if err := b.RipristinaDalCestino(ctx, FamigliaBuste, busta.ID, audit.Contesto{}); err == nil || errors.Is(err, ErrNonNelCestino) {
		t.Errorf("senza autore: %v", err)
	}
	resta("senza autore")
	esegui(t, d, `CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.restored' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.RipristinaDalCestino(ctx, FamigliaBuste, busta.ID, chi); err == nil {
		t.Error("audit guasto senza errore")
	}
	resta("audit guasto")
	esegui(t, d, `DROP TRIGGER niente_audit`)
	guasto := errors.New("guasto prima del commit")
	b.primaDelCommit = func(*sql.Tx) error { return guasto }
	if err := b.RipristinaDalCestino(ctx, FamigliaBuste, busta.ID, chi); !errors.Is(err, guasto) {
		t.Errorf("prima del commit: %v", err)
	}
	resta("prima del commit")
	b.primaDelCommit = nil
	if err := b.RipristinaDalCestino(ctx, FamigliaBuste, busta.ID, chi); err != nil {
		t.Errorf("dopo: %v", err)
	}
}

// inventario: RIP-S-52
// Trash.owner_only?/1 e Trash.names/0: le buste sono di tutti; un nome che il cestino non conosce è del titolare.
func TestFamiglieCestino(t *testing.T) {
	if got := FamiglieCestino(); !reflect.DeepEqual(got, []FamigliaCestino{FamigliaBuste}) {
		t.Errorf("famiglie %v", got)
	}
	if FamigliaBuste.SoloTitolare() {
		t.Error("le buste sono solo del titolare")
	}
	for _, f := range []FamigliaCestino{"sale", "", "user"} {
		if !f.SoloTitolare() {
			t.Errorf("%q non è del titolare", f)
		}
	}
}
