package buste

import (
	"encoding/json"
	"fmt"
	"testing"
)

// I test del contatore rimasto indietro (slice 055a): la busta nuova si salva con il primo numero
// dopo il massimo dell'anno, con la riga bag_sequence.realigned nella stessa transazione.

// conNumeri salva, accanto alla busta di partenza, buste con i numeri dati (da SQL, come un
// import), copiando la prima riga: numero, codice e impronta del gettone diversi.
func conNumeri(t *testing.T, b *Buste, numeri ...string) {
	t.Helper()
	crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "999/99" })) // la riga da copiare
	for _, n := range numeri {
		esegui(t, b.d, `INSERT INTO buste (cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione,
			hash_token_pubblico, token_pubblico_scade_il, creato_il, aggiornato_il)
			SELECT cliente_id, nome_cliente, telefono_cliente, ?, data_accettazione, descrizione, randomblob(32),
			token_pubblico_scade_il, creato_il, aggiornato_il FROM buste WHERE numero = '999/99'`, n)
	}
}

// riallineamenti sono i dati delle righe bag_sequence.realigned, in ordine.
func riallineamenti(t *testing.T, b *Buste) []map[string]any {
	t.Helper()
	righe, err := b.d.Lettura.Query(`SELECT risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = 'bag_sequence.realigned' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = righe.Close() }()
	var tutte []map[string]any
	for righe.Next() {
		var risorsa, id, esito, dati string
		if err := righe.Scan(&risorsa, &id, &esito, &dati); err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(dati), &m); err != nil {
			t.Fatal(err)
		}
		m["_risorsa"], m["_id"], m["_esito"] = risorsa, id, esito
		tutte = append(tutte, m)
	}
	return tutte
}

func contatore(t *testing.T, b *Buste, anno int) any {
	t.Helper()
	var v any
	err := b.d.Lettura.QueryRow(`SELECT ultimo_numero FROM sequenze_buste WHERE anno = ?`, anno).Scan(&v)
	if err != nil {
		return nil
	}
	return v
}

// inventario: RIP-H-31
// Il contatore dietro ai numeri usati: la busta si salva con il primo numero dopo il massimo
// dell'anno, il contatore arriva lì e c'è una riga d'audit con anno, valore di prima e nuovo. La
// busta si salva davvero (cliente, audit repair.created): il vincolo fallito non ha annullato la
// transazione.
func TestContatoreIndietroRiallineato(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "1/26", "2/26", "3/26", "50/26", "7/26", "60/25")
	esegui(t, d, `INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2026, 2) ON CONFLICT (anno) DO UPDATE SET ultimo_numero = 2`)
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente = "Cliente Riallineato" }))
	if got.Numero != "51/26" {
		t.Fatalf("numero %q, atteso 51/26", got.Numero)
	}
	if v := contatore(t, b, 2026); v != int64(51) {
		t.Errorf("contatore %v", v)
	}
	if v := contatore(t, b, 2025); v != nil {
		t.Errorf("contatore 2025 %v: non va toccato", v)
	}
	r := riallineamenti(t, b)
	if len(r) != 1 {
		t.Fatalf("righe d'audit %v", r)
	}
	atteso := map[string]any{"_risorsa": "bag_sequence", "_id": "2026", "_esito": "ok", "year": 2026.0, "previous": 2.0, "new": 50.0, "bag_number_taken": "3/26"}
	if fmt.Sprint(r[0]) != fmt.Sprint(atteso) {
		t.Errorf("audit %v, atteso %v", r[0], atteso)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.created' AND id_risorsa = ?`, fmt.Sprint(got.ID)) != 1 {
		t.Error("manca repair.created")
	}
	if conta(t, d, `SELECT count(*) FROM clienti WHERE nome = 'Cliente Riallineato'`) != 1 {
		t.Error("il cliente nuovo non c'è")
	}
	// Il numero dopo: dal contatore, senza altri riallineamenti.
	if got := crea(t, b, modulo()); got.Numero != "52/26" || len(riallineamenti(t, b)) != 1 {
		t.Errorf("dopo: %q, righe %d", got.Numero, len(riallineamenti(t, b)))
	}
}

// inventario: RIP-H-31
// Il contatore a posto: nessun riallineamento, nessuna riga d'audit, anche con numeri dell'anno più
// alti scritti fuori dal contatore (non sono in conflitto: il contatore non si tocca).
func TestContatoreAPostoNonRiallinea(t *testing.T) {
	b, _, _ := prova(t)
	for i := range 3 {
		if got := crea(t, b, modulo()); got.Numero != fmt.Sprintf("%d/26", i+1) {
			t.Errorf("numero %q", got.Numero)
		}
	}
	conNumeri(t, b, "90/26")
	if got := crea(t, b, modulo()); got.Numero != "4/26" {
		t.Errorf("numero %q, atteso 4/26", got.Numero)
	}
	if r := riallineamenti(t, b); len(r) != 0 {
		t.Errorf("righe d'audit %v", r)
	}
}

// inventario: RIP-H-31
// Un anno senza riga del contatore ma con numeri già usati (dati vecchi): la riga nasce al massimo
// e il valore di prima è 0.
func TestContatoreAnnoSenzaRiga(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "1/26", "12/26")
	esegui(t, d, `DELETE FROM sequenze_buste`)
	if got := crea(t, b, modulo()); got.Numero != "13/26" {
		t.Fatalf("numero %q, atteso 13/26", got.Numero)
	}
	r := riallineamenti(t, b)
	if len(r) != 1 || r[0]["previous"] != 0.0 || r[0]["new"] != 12.0 || r[0]["bag_number_taken"] != "1/26" {
		t.Errorf("audit %v", r)
	}
	if v := contatore(t, b, 2026); v != int64(13) {
		t.Errorf("contatore %v", v)
	}
}

// inventario: RIP-H-31
// Il massimo conta anche il cestino: l'indice unico lo copre.
func TestContatoreMassimoNelCestino(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "1/26", "30/26")
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE numero IN ('1/26', '30/26')`, istanteProva)
	esegui(t, d, `DELETE FROM sequenze_buste`)
	if got := crea(t, b, modulo()); got.Numero != "31/26" {
		t.Errorf("numero %q, atteso 31/26", got.Numero)
	}
}

// inventario: RIP-H-31
// I numeri importati con gli zeri davanti contano per il loro valore; quelli fuori formato (anno a
// quattro cifre, trattino, lettere, sei cifre, un altro anno) non contano e non rompono il calcolo.
func TestContatoreNumeriStrani(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "1/26", "0040/26", " 41/26 ", "45/2026", "99-26", "A99/26", "123456/26", "77/25", "", "/26", "0/26")
	esegui(t, d, `DELETE FROM sequenze_buste`)
	if got := crea(t, b, modulo()); got.Numero != "42/26" {
		t.Errorf("numero %q, atteso 42/26", got.Numero)
	}
	if r := riallineamenti(t, b); len(r) != 1 || r[0]["new"] != 41.0 {
		t.Errorf("audit %v", r)
	}
	for _, c := range []struct {
		s  string
		n  int64
		ok bool
	}{
		{"45/26", 45, true}, {"0045/26", 45, true}, {"\t45/26\n", 45, true}, {"00/26", 0, true}, {"99999/26", 99999, true},
		{"0099999/26", 99999, true}, {"100000/26", 0, false}, {"45/25", 0, false}, {"45/2026", 0, false}, {"45 /26", 0, false},
		{"４５/26", 0, false}, {"+45/26", 0, false}, {"", 0, false},
	} {
		if n, ok := numeroDellAnno(c.s, 26); n != c.n || ok != c.ok {
			t.Errorf("%q: %d %v, atteso %d %v", c.s, n, ok, c.n, c.ok)
		}
	}
}

// inventario: RIP-H-31
// Il massimo uguale al numero tentato: il contatore va al massimo (non scende) e il numero è quello
// dopo; con il contatore poi più avanti del massimo nessun altro riallineamento.
func TestContatoreNonScende(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "5/26")
	esegui(t, d, `INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2026, 4) ON CONFLICT (anno) DO UPDATE SET ultimo_numero = 4`)
	if got := crea(t, b, modulo()); got.Numero != "6/26" {
		t.Fatalf("numero %q", got.Numero)
	}
	// Il massimo 5 con il contatore già a 6: niente da fare.
	if got := crea(t, b, modulo()); got.Numero != "7/26" || len(riallineamenti(t, b)) != 1 {
		t.Errorf("numero %q, righe %d", got.Numero, len(riallineamenti(t, b)))
	}
	if v := contatore(t, b, 2026); v != int64(7) {
		t.Errorf("contatore %v", v)
	}
}

// inventario: RIP-H-31
// Il secondo tentativo fallisce anche lui (provocato con un trigger che occupa il numero dopo il
// massimo appena il contatore ci arriva): «Numero busta già esistente.» come prima, e la
// transazione annulla tutto, anche il riallineamento e la sua riga d'audit.
func TestContatoreSecondoTentativoFallito(t *testing.T) {
	b, d, _ := prova(t)
	conNumeri(t, b, "1/26", "20/26")
	esegui(t, d, `INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2026, 0) ON CONFLICT (anno) DO UPDATE SET ultimo_numero = 0`)
	esegui(t, d, `CREATE TRIGGER occupa AFTER UPDATE ON sequenze_buste WHEN NEW.ultimo_numero = 21 BEGIN
		INSERT INTO buste (cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione,
			hash_token_pubblico, token_pubblico_scade_il, creato_il, aggiornato_il)
		SELECT cliente_id, nome_cliente, telefono_cliente, '21/26', data_accettazione, descrizione, randomblob(32),
			token_pubblico_scade_il, creato_il, aggiornato_il FROM buste WHERE numero = '999/99';
	END`)
	prima := scatta(t, d)
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.NomeCliente = "Cliente Bloccato" }), chi)
	c := erroriCampi(t, err)
	if fmt.Sprint(c["numero"]) != "[Numero busta già esistente.]" || len(c) != 1 {
		t.Errorf("errori %v", c)
	}
	uguali(t, "secondo tentativo fallito", prima, scatta(t, d))
	if r := riallineamenti(t, b); len(r) != 0 {
		t.Errorf("righe d'audit rimaste %v", r)
	}
}

// inventario: RIP-H-31
// Un numero scritto (solo dal dominio: la pagina non lo manda più) già usato resta rifiutato com'era,
// senza riallineamento.
func TestContatoreNumeroScrittoNonRiallinea(t *testing.T) {
	b, _, _ := prova(t)
	crea(t, b, modulo())
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = "1/26" }), chi)
	if c := erroriCampi(t, err); fmt.Sprint(c["numero"]) != "[Numero busta già esistente.]" {
		t.Errorf("errori %v", c)
	}
	if r := riallineamenti(t, b); len(r) != 0 {
		t.Errorf("righe d'audit %v", r)
	}
}

// In SQLite un vincolo UNIQUE fallito annulla solo l'istruzione (ON CONFLICT ABORT, il
// predefinito), non la transazione: quello che c'era prima e quello che viene dopo si salvano.
// Su questo si regge il secondo tentativo nella stessa transazione.
func TestVincoloUnicoAnnullaSoloLIstruzione(t *testing.T) {
	_, d, _ := prova(t)
	tx, err := d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2030, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2031, 1), (2030, 2)`); err == nil {
		t.Fatal("il doppione è passato")
	}
	if _, err := tx.Exec(`INSERT INTO sequenze_buste (anno, ultimo_numero) VALUES (2032, 3)`); err != nil {
		t.Fatalf("la transazione non va più: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := conta(t, d, `SELECT count(*) FROM sequenze_buste WHERE anno IN (2030, 2032)`); got != 2 {
		t.Errorf("righe salvate %d, attese 2", got)
	}
	if got := conta(t, d, `SELECT count(*) FROM sequenze_buste WHERE anno = 2031`); got != 0 {
		t.Error("la riga 2031 dell'istruzione fallita è rimasta")
	}
}
