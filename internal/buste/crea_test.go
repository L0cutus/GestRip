package buste

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// La creazione della busta (RIP-H-30, 33, 34, 35, 38, 39), come Workshop.create_repair/4,
// create_repair_with_customer/4 e resolve_intake_customer/2 di workshop_live.ex; i casi da
// workshop_test.exs, workshop_live_test.exs e workshop_intake_atomic_test.exs.

// inventario: RIP-H-30
// Una busta completa: cliente nuovo, copia di nome e telefono, numero, date, testi, QR, stato
// iniziale, gettone (impronta SHA-256 del token, token in chiaro come lo standalone, scadenza a
// 90 giorni), istanti e le due righe d'audit.
func TestCreaBusta(t *testing.T) {
	b, d, _ := prova(t)
	got := crea(t, b, Accettazione{
		NomeCliente: "  Ada Esempio ", TelefonoCliente: " 333 111 2222 ", OreficeriaID: "2", ArtigianoID: "1",
		DataAccettazione: "2026-10-30", DataPrevista: "2026-11-20", Descrizione: "Saldare una catenina",
		NotePubbliche: "Ritiro di sabato", NoteInterne: "Oro 750 del cliente", Preventivo: "35",
		CodiceQR: "QR-0001",
	})
	if got.Numero != "1/26" || got.OreficeriaID == nil || *got.OreficeriaID != 2 {
		t.Errorf("risultato %+v", got)
	}
	cliente := valore(t, d, `SELECT id || '|' || nome || '|' || telefono || '|' || creato_il || '|' || aggiornato_il FROM clienti`)
	if cliente != itoa(got.ClienteID)+"|Ada Esempio|333 111 2222|"+istanteProva+"|"+istanteProva {
		t.Errorf("cliente %v", cliente)
	}
	riga := valore(t, d, `SELECT cliente_id || '|' || nome_cliente || '|' || telefono_cliente || '|' || numero || '|' ||
		data_accettazione || '|' || data_prevista || '|' || coalesce(data_prevista_testo, '-') || '|' || descrizione || '|' ||
		note_pubbliche || '|' || note_interne || '|' || stato || '|' || compro_oro || '|' || preventivo_testo || '|' ||
		preventivo_centesimi || '|' || oreficeria_id || '|' || artigiano_id || '|' || codice_qr || '|' ||
		token_pubblico_scade_il || '|' || coalesce(token_pubblico_revocato_il, '-') || '|' || creato_il || '|' || aggiornato_il || '|' ||
		coalesce(data_pronta, data_consegna, conservazione_iniziata_il, cancellata_il, '-') || '|' || giorni_in_piu || '|' || attesa_cliente
		FROM buste WHERE id = ?`, got.ID)
	atteso := itoa(got.ClienteID) + "|Ada Esempio|333 111 2222|1/26|2026-10-30|2026-11-20|-|Saldare una catenina|" +
		"Ritiro di sabato|Oro 750 del cliente|in_lavorazione|0|35|3500|2|1|QR-0001|" +
		"2027-01-29T09:00:00.000000Z|-|" + istanteProva + "|" + istanteProva + "|-|0|0"
	if riga != atteso {
		t.Errorf("busta\n%v\natteso\n%v", riga, atteso)
	}
	if len(got.Token) != 43 {
		t.Errorf("token di %d caratteri", len(got.Token))
	}
	if _, err := base64.RawURLEncoding.DecodeString(got.Token); err != nil {
		t.Errorf("token non base64url: %v", err)
	}
	h := sha256.Sum256([]byte(got.Token))
	if v, _ := valore(t, d, `SELECT hash_token_pubblico FROM buste WHERE id = ?`, got.ID).([]byte); string(v) != string(h[:]) {
		t.Error("l'impronta non è lo SHA-256 del token")
	}
	if valore(t, d, `SELECT token_pubblico FROM buste WHERE id = ?`, got.ID) != got.Token {
		t.Error("il token in chiaro non è quello restituito")
	}
	if s := ultimoAudit(t, d, "customer.created"); s != "customer.created|customer|"+itoa(got.ClienteID)+"|ok|{}" {
		t.Errorf("audit cliente %q", s)
	}
	if s := ultimoAudit(t, d, "repair.created"); s != "repair.created|repair|"+itoa(got.ID)+
		`|ok|{"artisan_id":1,"bag_number":"1/26","compro_oro":false,"oreficeria_id":2,"status":"in_lavorazione"}` {
		t.Errorf("audit busta %q", s)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE attore = 'titolare@esempio.test' AND ip = '203.0.113.7'`); n != 2 {
		t.Errorf("righe d'audit con l'attore: %d", n)
	}
	var token string
	_ = d.Lettura.QueryRow(`SELECT group_concat(dati) FROM eventi_audit`).Scan(&token)
	if strings.Contains(token, got.Token) {
		t.Error("il token è finito nell'audit")
	}
}

// inventario: RIP-H-30
// Ogni busta ha un gettone suo; senza oreficeria e artigiano l'audit li dice null.
func TestGettoniDiversi(t *testing.T) {
	b, d, _ := prova(t)
	uno, due := crea(t, b, modulo()), crea(t, b, modulo())
	if uno.Token == due.Token || conta(t, d, `SELECT count(DISTINCT hash_token_pubblico) FROM buste`) != 2 {
		t.Error("due buste con lo stesso gettone")
	}
	if uno.OreficeriaID != nil {
		t.Error("oreficeria senza sceglierla")
	}
	if s := ultimoAudit(t, d, "repair.created"); !strings.HasSuffix(s, `|ok|{"artisan_id":null,"bag_number":"2/26","compro_oro":false,"oreficeria_id":null,"status":"in_lavorazione"}`) {
		t.Errorf("audit %q", s)
	}
}

// inventario: RIP-H-30
// I campi facoltativi vuoti o fatti di soli spazi restano NULL; un testo scritto si tiene com'è,
// spazi ai bordi compresi.
func TestCampiFacoltativiVuoti(t *testing.T) {
	b, d, _ := prova(t)
	for _, vuoto := range []string{"", "   ", "\n"} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) {
			a.NotePubbliche, a.NoteInterne, a.Preventivo, a.CodiceQR, a.DataPrevista, a.OreficeriaID, a.ArtigianoID =
				vuoto, vuoto, vuoto, vuoto, vuoto, vuoto, vuoto
		}))
		if v := valore(t, d, `SELECT coalesce(note_pubbliche, note_interne, preventivo_testo, preventivo_centesimi, codice_qr,
			data_prevista, data_prevista_testo, oreficeria_id, artigiano_id, 'tutti NULL') FROM buste WHERE id = ?`, got.ID); v != "tutti NULL" {
			t.Errorf("%q: %v", vuoto, v)
		}
	}
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NotePubbliche, a.Descrizione = " nota ", " lavoro\n" }))
	if v := valore(t, d, `SELECT note_pubbliche || '|' || descrizione FROM buste WHERE id = ?`, got.ID); v != " nota | lavoro\n" {
		t.Errorf("testi %q", v)
	}
}

// inventario: RIP-H-30
// Lavorazione e data di accettazione sono obbligatorie («can't be blank», il messaggio di Ecto che
// lo standalone mostra); una data che non è AAAA-MM-GG valida è «is invalid». Gli errori del
// modulo arrivano tutti insieme e non resta niente.
func TestCampiObbligatori(t *testing.T) {
	b, d, _ := prova(t)
	prima := scatta(t, d)
	for _, c := range []struct {
		a      Accettazione
		errori string
	}{
		{con(modulo(), func(a *Accettazione) { a.Descrizione = "" }), "map[descrizione:[can't be blank]]"},
		{con(modulo(), func(a *Accettazione) { a.Descrizione = "  \t" }), "map[descrizione:[can't be blank]]"},
		{con(modulo(), func(a *Accettazione) { a.DataAccettazione = "" }), "map[data_accettazione:[can't be blank]]"},
		{con(modulo(), func(a *Accettazione) { a.DataAccettazione = "31/10/2026" }), "map[data_accettazione:[is invalid]]"},
		{con(modulo(), func(a *Accettazione) { a.DataAccettazione = "2026-02-29" }), "map[data_accettazione:[is invalid]]"},
		{con(modulo(), func(a *Accettazione) {
			a.Descrizione, a.DataAccettazione, a.Numero, a.Preventivo, a.DataPrevista = "", "ieri", "1-26", strings.Repeat("1", 300), "domani"
		}), "map[data_accettazione:[is invalid] data_prevista:[is invalid] descrizione:[can't be blank] numero:[formato richiesto N/AA (es. 12/26)] preventivo:[Importo troppo lungo: massimo 255 caratteri.]]"},
	} {
		_, err := b.Crea(ctx, c.a, chi)
		if got := fmt.Sprint(map[string][]string(erroriCampi(t, err))); got != c.errori {
			t.Errorf("errori %s, attesi %s", got, c.errori)
		}
	}
	uguali(t, "campi obbligatori", prima, scatta(t, d))
}

// inventario: RIP-H-33
// Nessun cliente con quel nome: se ne crea uno, con nome e telefono senza gli spazi ai bordi e
// il telefono non normalizzato; vuoto il telefono resta vuoto.
func TestClienteNuovo(t *testing.T) {
	b, d, _ := prova(t)
	uno := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = " Bruno Esempio ", " +39 (333) 12-34 " }))
	due := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Carla Esempio", "" }))
	if v := valore(t, d, `SELECT group_concat(nome || '|' || telefono, ',') FROM (SELECT * FROM clienti ORDER BY id)`); v != "Bruno Esempio|+39 (333) 12-34,Carla Esempio|" {
		t.Errorf("clienti %v", v)
	}
	if v := valore(t, d, `SELECT group_concat(nome_cliente || '|' || telefono_cliente, ',') FROM (SELECT * FROM buste ORDER BY id)`); v != "Bruno Esempio|+39 (333) 12-34,Carla Esempio|" {
		t.Errorf("copie %v", v)
	}
	if uno.ClienteID == due.ClienteID {
		t.Error("stesso cliente")
	}
}

// inventario: RIP-H-33
// Un solo cliente con quel nome (senza maiuscole e spazi ai bordi) si riusa; un telefono scritto
// diverso aggiorna l'anagrafica con audit customer.updated, anche se prima era vuoto; uno uguale
// o vuoto non scrive niente sul cliente.
func TestClienteUnicoRiusato(t *testing.T) {
	b, d, o := prova(t)
	esegui(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (5, 'Ada Esempio', '', ?, ?)`, istanteProva, istanteProva)
	o.Sposta(time.Minute)
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")

	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "  ada ESEMPIO ", " 333 999 " }))
	if got.ClienteID != 5 {
		t.Fatalf("cliente %d", got.ClienteID)
	}
	if v := valore(t, d, `SELECT nome || '|' || telefono || '|' || aggiornato_il FROM clienti WHERE id = 5`); v != "Ada Esempio|333 999|"+ora {
		t.Errorf("cliente %v", v)
	}
	if v := valore(t, d, `SELECT nome_cliente || '|' || telefono_cliente FROM buste WHERE id = ?`, got.ID); v != "Ada Esempio|333 999" {
		t.Errorf("copia %v", v)
	}
	if s := ultimoAudit(t, d, "customer.updated"); s != "customer.updated|customer|5|ok|{}" {
		t.Errorf("audit %q", s)
	}
	if conta(t, d, `SELECT count(*) FROM clienti`) != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'customer.created'`) != 0 {
		t.Error("creato un cliente in più")
	}

	o.Sposta(time.Minute)
	for _, tel := range []string{"", "  ", "333 999", " 333 999 "} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Ada Esempio", tel }))
		if v := valore(t, d, `SELECT telefono_cliente FROM buste WHERE id = ?`, got.ID); v != "333 999" {
			t.Errorf("%q: copia %v", tel, v)
		}
	}
	if v := valore(t, d, `SELECT telefono || '|' || aggiornato_il FROM clienti WHERE id = 5`); v != "333 999|"+ora {
		t.Errorf("cliente toccato senza motivo: %v", v)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'customer.updated'`); n != 1 {
		t.Errorf("righe customer.updated %d", n)
	}
}

// inventario: RIP-H-33
// Il confronto del telefono per decidere se aggiornarlo è con quello salvato com'è: un telefono
// salvato con gli spazi ai bordi si riscrive senza (sync_customer_phone/3).
func TestTelefonoSalvatoConGliSpazi(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (5, ' Ada Esempio ', ' 333 ', ?, ?)`, istanteProva, istanteProva)
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Ada Esempio", "333" }))
	if v := valore(t, d, `SELECT nome || '|' || telefono FROM clienti WHERE id = 5`); v != " Ada Esempio |333" {
		t.Errorf("cliente %q", v)
	}
	if v := valore(t, d, `SELECT nome_cliente FROM buste WHERE id = ?`, got.ID); v != " Ada Esempio " {
		t.Errorf("copia del nome %q", v)
	}
}

// inventario: RIP-H-33
// Più omonimi: vince quello con lo stesso telefono (il primo per nome e id); se nessuno coincide,
// o il telefono è vuoto, nasce un cliente nuovo e nessun omonimo viene sovrascritto.
func TestOmonimi(t *testing.T) {
	b, d, _ := prova(t)
	for _, q := range []string{
		`INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (3, 'Maria Esempio', '111', ?1, ?1)`,
		`INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (4, 'maria esempio', ' 222 ', ?1, ?1)`,
		`INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (2, 'Maria Esempio', '222', ?1, ?1)`,
	} {
		esegui(t, d, q, istanteProva)
	}
	for _, c := range []struct {
		tel     string
		cliente int64
	}{{"111", 3}, {"222", 2}} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Maria Esempio", c.tel }))
		if got.ClienteID != c.cliente {
			t.Errorf("telefono %s: cliente %d, atteso %d", c.tel, got.ClienteID, c.cliente)
		}
	}
	for _, tel := range []string{"333", ""} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Maria Esempio", tel }))
		if got.ClienteID <= 4 {
			t.Errorf("telefono %q: riusato il cliente %d", tel, got.ClienteID)
		}
	}
	if v := valore(t, d, `SELECT group_concat(id || ':' || telefono, ',') FROM (SELECT * FROM clienti WHERE id < 5 ORDER BY id)`); v != "2:222,3:111,4: 222 " {
		t.Errorf("omonimi toccati: %v", v)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'customer.updated'`); n != 0 {
		t.Errorf("aggiornamenti %d", n)
	}
}

// inventario: RIP-H-33
// Più clienti possono avere lo stesso telefono.
func TestStessoTelefono(t *testing.T) {
	b, d, _ := prova(t)
	crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Ada Esempio", "333 1111111" }))
	crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Bruno Esempio", "333 1111111" }))
	if n := conta(t, d, `SELECT count(DISTINCT id) FROM clienti WHERE telefono = '333 1111111'`); n != 2 {
		t.Errorf("clienti %d", n)
	}
}

// inventario: RIP-H-33, RIP-H-30
// Nome vuoto o di soli spazi: «Inserisci il nome del cliente.» prima di ogni altra cosa, niente
// scritto.
func TestNomeClienteVuoto(t *testing.T) {
	b, d, _ := prova(t)
	prima := scatta(t, d)
	for _, nome := range []string{"", "   ", "\t\n"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.Descrizione, a.OreficeriaID = nome, "", "1" }), chi)
		if !errors.Is(err, ErrNomeCliente) || err.Error() != "Inserisci il nome del cliente." {
			t.Errorf("%q: %v", nome, err)
		}
	}
	uguali(t, "nome vuoto", prima, scatta(t, d))
}

// inventario: RIP-H-33
// Limiti del cliente: nome al più 160 grafemi, telefono al più 40 (contati come Ecto, dopo gli
// spazi ai bordi); oltre, «Controlla nome e telefono del cliente.» e niente salvato, nemmeno il
// telefono di un cliente che c'è già.
func TestLimitiDelCliente(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (5, 'Ada Esempio', '333111', ?, ?)`, istanteProva, istanteProva)
	prima := scatta(t, d)
	for _, c := range []struct{ nome, tel string }{
		{strings.Repeat("a", 161), ""},
		{"Nome Nuovo", strings.Repeat("3", 41)},
		{"Ada Esempio", strings.Repeat("3", 45)},
	} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente, a.ArtigianoID = c.nome, c.tel, "999" }), chi)
		if !errors.Is(err, ErrCliente) || err.Error() != "Controlla nome e telefono del cliente." {
			t.Errorf("%d/%d: %v", len(c.nome), len(c.tel), err)
		}
	}
	uguali(t, "cliente oltre i limiti", prima, scatta(t, d))
	// Al limite si salva; «é» scritta come e + accento combinante è un grafema solo.
	for _, c := range []struct{ nome, tel string }{
		{" " + strings.Repeat("a", 160) + " ", " " + strings.Repeat("3", 40) + " "},
		{strings.Repeat("é", 160), strings.Repeat("é", 40)},
	} {
		if _, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = c.nome, c.tel }), chi); err != nil {
			t.Errorf("%d byte: %v", len(c.nome), err)
		}
	}
}

// inventario: RIP-H-33
// Cliente e busta stanno nella stessa transazione: una busta rifiutata dopo il passo del cliente
// non lascia un cliente nuovo, un telefono cambiato, un omonimo in più, un contatore mosso né
// righe d'audit.
func TestBustaRifiutataNonLasciaNiente(t *testing.T) {
	b, d, _ := prova(t)
	crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.Numero = "Cliente Esistente", "77/26" }))
	esegui(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (10, 'Ada Esempio', '333 111 2222', ?1, ?1),
		(11, 'Mario Esempio', '333 1', ?1, ?1), (12, 'Mario Esempio', '333 2', ?1, ?1)`, istanteProva)
	prima := scatta(t, d)
	for _, c := range []struct {
		a       Accettazione
		rifiuto string
	}{
		{con(modulo(), func(a *Accettazione) {
			a.NomeCliente, a.TelefonoCliente, a.Numero = "Cliente Nuovo", "333 000 1111", "77/26"
		}), "Numero busta già esistente."},
		{con(modulo(), func(a *Accettazione) {
			a.NomeCliente, a.TelefonoCliente, a.Numero = "Ada Esempio", "333 999 8888", "77/26"
		}), "Numero busta già esistente."},
		{con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente, a.Numero = "Mario Esempio", "333 3", "77/26" }), "Numero busta già esistente."},
		{con(modulo(), func(a *Accettazione) { a.NomeCliente, a.ArtigianoID = "Cliente Nuovo", "999999" }), "Seleziona un artigiano valido."},
		{con(modulo(), func(a *Accettazione) { a.NomeCliente, a.OreficeriaID = "Ada Esempio", "1" }), "Seleziona un'oreficeria valida."},
		{con(modulo(), func(a *Accettazione) { a.NomeCliente, a.Descrizione = "Cliente Nuovo", "" }), "descrizione"},
	} {
		_, err := b.Crea(ctx, c.a, chi)
		if err == nil || !strings.Contains(fmt.Sprint(err), c.rifiuto) {
			t.Errorf("%s: %v", c.a.NomeCliente, err)
		}
	}
	uguali(t, "busta rifiutata", prima, scatta(t, d))
	// Con il numero corretto si salvano cliente e busta.
	got := crea(t, b, con(modulo(), func(a *Accettazione) {
		a.NomeCliente, a.TelefonoCliente, a.Numero = "Cliente Nuovo", "333 000 1111", "78/26"
	}))
	if valore(t, d, `SELECT nome FROM clienti WHERE id = ?`, got.ClienteID) != "Cliente Nuovo" {
		t.Error("cliente della busta corretta")
	}
}

// inventario: RIP-H-33
// Le copie di nome e telefono nella busta non cambiano quando cambia l'anagrafica.
func TestCopieDelClienteFerme(t *testing.T) {
	b, d, _ := prova(t)
	got := crea(t, b, modulo())
	esegui(t, d, `UPDATE clienti SET nome = 'Altro Nome', telefono = '000' WHERE id = ?`, got.ClienteID)
	if v := valore(t, d, `SELECT nome_cliente || '|' || telefono_cliente FROM buste WHERE id = ?`, got.ID); v != "Cliente Prova|333 000 0001" {
		t.Errorf("copia %v", v)
	}
}

// inventario: RIP-H-34
// L'oreficeria: vuota è la diretta; una esterna esistente si usa (anche con spazi o lo zero
// davanti, come Id.positive/1); l'id 1 (il negozio), una che non c'è, un valore non numerico o
// fuori misura sono «Seleziona un'oreficeria valida.» e nessuna busta. Il telefono del cliente
// resta suo.
func TestOreficeria(t *testing.T) {
	b, d, _ := prova(t)
	for _, v := range []string{"2", " 2 ", "02", "+2"} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = v }))
		if got.OreficeriaID == nil || *got.OreficeriaID != 2 {
			t.Errorf("%q: %v", v, got.OreficeriaID)
		}
		if valore(t, d, `SELECT telefono_cliente FROM buste WHERE id = ?`, got.ID) != "333 000 0001" {
			t.Error("telefono del cliente cambiato dall'oreficeria")
		}
	}
	prima := scatta(t, d)
	for _, v := range []string{"1", "0", "-2", "999999", "99999999999999999999999999999", "abc", "2a", "2.0", "1_0"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.OreficeriaID = v; a.Descrizione = "" }), chi)
		if !errors.Is(err, ErrOreficeria) || err.Error() != "Seleziona un'oreficeria valida." {
			t.Errorf("%q: %v", v, err)
		}
	}
	uguali(t, "oreficeria non valida", prima, scatta(t, d))
}

// inventario: RIP-H-35
// L'artigiano: vuoto è nessuno; uno dell'anagrafica si usa; uno che non c'è o un valore non
// numerico è «Seleziona un artigiano valido.» e nessuna busta. L'oreficeria si controlla prima.
func TestArtigiano(t *testing.T) {
	b, d, _ := prova(t)
	got := crea(t, b, con(modulo(), func(a *Accettazione) { a.ArtigianoID = " 1 " }))
	if valore(t, d, `SELECT artigiano_id FROM buste WHERE id = ?`, got.ID) != int64(1) {
		t.Error("artigiano non assegnato")
	}
	prima := scatta(t, d)
	for _, v := range []string{"0", "999999", "99999999999999999999999999999", "x", "-1"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.ArtigianoID = v; a.Numero = "abc" }), chi)
		if !errors.Is(err, ErrArtigiano) || err.Error() != "Seleziona un artigiano valido." {
			t.Errorf("%q: %v", v, err)
		}
	}
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.ArtigianoID, a.OreficeriaID = "999", "999" }), chi)
	if !errors.Is(err, ErrOreficeria) {
		t.Errorf("oreficeria e artigiano sbagliati: %v", err)
	}
	uguali(t, "artigiano non valido", prima, scatta(t, d))
}

// inventario: RIP-H-39
// Il QR è testo facoltativo e non unico: vuoto o di soli spazi è nessun codice, uno scritto si
// tiene com'è.
func TestCodiceQR(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct {
		scritto string
		salvato any
	}{{"QR-1", "QR-1"}, {"QR-1", "QR-1"}, {" QR-2 ", " QR-2 "}, {"   ", nil}, {"", nil}} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = c.scritto }))
		if v := valore(t, d, `SELECT codice_qr FROM buste WHERE id = ?`, got.ID); v != c.salvato {
			t.Errorf("%q: %v", c.scritto, v)
		}
	}
}

// inventario: RIP-H-30
// L'ordine dei rifiuti è quello dello standalone: il nome del cliente, poi i limiti del cliente,
// poi oreficeria, artigiano e infine gli errori del modulo.
func TestOrdineDeiRifiuti(t *testing.T) {
	b, _, _ := prova(t)
	tutto := Accettazione{NomeCliente: " ", TelefonoCliente: strings.Repeat("3", 41), OreficeriaID: "1", ArtigianoID: "0", Numero: "x", Descrizione: ""}
	for _, c := range []struct {
		f      func(*Accettazione)
		atteso string
	}{
		{func(*Accettazione) {}, "Inserisci il nome del cliente."},
		{func(a *Accettazione) { a.NomeCliente = "Nome" }, "Controlla nome e telefono del cliente."},
		{func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = "Nome", "" }, "Seleziona un'oreficeria valida."},
		{func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente, a.OreficeriaID = "Nome", "", "" }, "Seleziona un artigiano valido."},
		{func(a *Accettazione) {
			a.NomeCliente, a.TelefonoCliente, a.OreficeriaID, a.ArtigianoID = "Nome", "", "", ""
		}, "can't be blank"},
	} {
		_, err := b.Crea(ctx, con(tutto, c.f), chi)
		if err == nil || !strings.Contains(err.Error(), c.atteso) {
			t.Errorf("atteso %q, avuto %v", c.atteso, err)
		}
	}
}

// inventario: RIP-H-30
// Il token in chiaro non esce stampando la busta creata (redact: true nello standalone): né con
// %v, %+v e %#v, né nel registro slog.
func TestTokenNascostoNelleStampe(t *testing.T) {
	b, _, _ := prova(t)
	c := crea(t, b, modulo())
	var buf strings.Builder
	slog.New(slog.NewTextHandler(&buf, nil)).Info("creata", "busta", c, "puntatore", &c)
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("creata", "busta", c)
	for _, s := range []string{fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c),
		fmt.Sprintf("%v", &c), fmt.Sprintf("%+v", &c), fmt.Sprintf("%#v", &c), fmt.Sprint([]Creata{c}), buf.String()} {
		if strings.Contains(s, c.Token) {
			t.Errorf("token in %q", s)
		}
		if !strings.Contains(s, c.Numero) {
			t.Errorf("la stampa non dice più nemmeno il numero: %q", s)
		}
	}
}

// inventario: RIP-H-33
// Le minuscole del confronto dei nomi sono quelle di String.downcase/1 (modalità predefinita):
// «İ» diventa «i» + punto combinante, quindi «İlker Esempio» e «ilker esempio» sono due nomi
// diversi e nasce un cliente nuovo; la sigma non guarda la fine della parola (solo la modalità
// :greek lo fa), quindi «ΠΑΠΑΣ» è «παπασ» e non «παπας».
func TestMinuscoleComeElixir(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{"İlker", "i̇lker"}, {"İ", "i̇"}, {"ΠΑΠΑΣ", "παπασ"}, {"παπας", "παπας"},
		{"Ada ESEMPIO", "ada esempio"}, {"ÀÉÎ", "àéî"}, {"ᾈ", "ᾀ"}, {"ß", "ß"},
	} {
		if got := minuscole(c.in); got != c.out {
			t.Errorf("minuscole(%q) = %q, atteso %q", c.in, got, c.out)
		}
	}
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (5, 'İlker Esempio', '', ?1, ?1), (6, 'ΕΛΕΝΗ ΠΑΠΑΣ', '', ?1, ?1)`, istanteProva)
	for _, c := range []struct {
		nome   string
		stesso int64
	}{{"ilker esempio", 0}, {"İLKER ESEMPIO", 5}, {"i̇lker esempio", 5}, {"Ελενη Παπας", 0}, {"ελενη παπασ", 6}} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.NomeCliente, a.TelefonoCliente = c.nome, "" }))
		if (c.stesso == 0 && got.ClienteID <= 6) || (c.stesso != 0 && got.ClienteID != c.stesso) {
			t.Errorf("%q: cliente %d, atteso %d (0 = nuovo)", c.nome, got.ClienteID, c.stesso)
		}
	}
}
