package buste

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'iscrizione Telegram dalla pagina del cliente (CLI-44) e dal link dei messaggi WhatsApp (CLI-45), anche
// dalla pagina storica (CLI-19), come telegram_subscriptions_test.exs, public_repair_telegram_test.exs e
// public_repair_telegram_link_test.exs dello standalone (go-baseline), con la regola del canale
// disponibile del Go (D-16 come per l'email).

const nomeBotProva = "bottega_prova_bot"

// providerTelegram è il provider finto di Telegram: il canale (invio acceso e bot configurato) è acceso
// finché spento è falso.
type providerTelegram struct{ spento bool }

func (p *providerTelegram) Consegna(context.Context, uscita.Messaggio) (string, error) {
	return "1", nil
}
func (p *providerTelegram) Attivo(context.Context) (bool, error) { return !p.spento, nil }

// provaTelegram sono le buste con la coda di Telegram accesa, le notifiche ai clienti accese e il nome del
// bot noto: il canale è disponibile con l'indirizzo pubblico baseEmail.
func provaTelegram(t *testing.T) (*Buste, *db.DB, *clock.Finto, *providerTelegram) {
	t.Helper()
	b, d, o := prova(t)
	p := &providerTelegram{}
	b.ConPosta(uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleTelegram: p}), []byte("chiave-email-di-prova-di-32-byte"))
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?), (?, ?, ?)`,
		uscita.ChiaveNotificheClienti, istanteProva, uscita.ChiaveNomeBot, nomeBotProva, istanteProva)
	return b, d, o, p
}

var reLinkTMe = regexp.MustCompile(`\Ahttps://t\.me/` + nomeBotProva + `\?start=([A-Za-z0-9_-]{32})\z`)

// startDi è il token del link t.me, o il test si ferma.
func startDi(t *testing.T, link string) string {
	t.Helper()
	m := reLinkTMe.FindStringSubmatch(link)
	if m == nil {
		t.Fatalf("link t.me inatteso: %q", link)
	}
	return m[1]
}

// iscrivi mette un'iscrizione della chat alla busta: consenso e fermata come dati.
func iscrivi(t *testing.T, d *db.DB, busta, chat int64, consenso int, fermata any) int64 {
	t.Helper()
	r, err := d.Scrittura.Exec(`INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, fermata_il, creato_il, aggiornato_il)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, busta, chat, consenso, istanteProva, fermata, istanteProva, istanteProva)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

// inventario: CLI-44
// Il canale è disponibile (regola D-16 del Go) solo con l'invio Telegram acceso e il bot configurato (il
// canale della coda), le notifiche ai clienti accese, il nome del bot noto e di forma valida e
// l'indirizzo pubblico; senza la coda mai.
func TestTelegramDisponibile(t *testing.T) {
	b, d, _, p := provaTelegram(t)
	si := func(nome, base string, atteso bool) {
		t.Helper()
		got, err := b.TelegramDisponibile(ctx, base)
		if err != nil || got != atteso {
			t.Errorf("%s: %v %v, atteso %v", nome, got, err, atteso)
		}
	}
	si("tutto acceso", baseEmail, true)
	si("senza indirizzo pubblico", "", false)
	p.spento = true
	si("invio spento", baseEmail, false)
	p.spento = false
	esegui(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	si("notifiche spente", baseEmail, false)
	esegui(t, d, `UPDATE impostazioni SET valore = 'true' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	si("notifiche «true»", baseEmail, false)
	esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	for _, nome := range []string{"", "   ", "bot/altro", "bot?start=x", "bot nome", "bot\n"} {
		esegui(t, d, `UPDATE impostazioni SET valore = ? WHERE chiave = ?`, nome, uscita.ChiaveNomeBot)
		si("nome del bot "+url.QueryEscape(nome), baseEmail, false)
	}
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, uscita.ChiaveNomeBot)
	si("nome del bot assente", baseEmail, false)
	senza, _, _ := prova(t)
	if ok, err := senza.TelegramDisponibile(ctx, baseEmail); ok || err != nil {
		t.Errorf("senza coda: %v %v", ok, err)
	}
}

// inventario: CLI-44
// Il pulsante della pagina: un token di 24 byte in base64url (32 caratteri) nel link t.me del bot; nel
// database solo il suo SHA-256, con la busta e la scadenza a 24 ore esatte; ogni pressione un token
// nuovo; la riga repair.telegram_optin con busta, link e via, mai il token.
func TestTokenTelegramPagina(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	primo, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico)
	if err != nil {
		t.Fatal(err)
	}
	tok := startDi(t, primo)
	var hash []byte
	var busta int64
	var scade string
	if err := d.Lettura.QueryRow(`SELECT busta_id, hash_token, scade_il FROM token_telegram`).Scan(&busta, &hash, &scade); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(tok))
	if busta != c.ID || string(hash) != string(h[:]) {
		t.Errorf("riga del token: busta %d, hash %x", busta, hash)
	}
	if atteso := o.Adesso().UTC().Add(24 * time.Hour).Format(db.FormatoIstante); scade != atteso {
		t.Errorf("scadenza %s, attesa %s", scade, atteso)
	}
	secondo, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico)
	if err != nil || startDi(t, secondo) == tok {
		t.Fatalf("seconda pressione: %v, stesso token", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id = ?`, c.ID); n != 2 {
		t.Errorf("token: %d", n)
	}
	var dati, ip string
	if err := d.Lettura.QueryRow(`SELECT dati, coalesce(ip, '') FROM eventi_audit WHERE azione = 'repair.telegram_optin' ORDER BY id LIMIT 1`).Scan(&dati, &ip); err != nil {
		t.Fatal(err)
	}
	if dati != `{"bag_number":"`+c.Numero+`","link":"token","via":"page"}` || ip != pubblico.IP {
		t.Errorf("audit: %s %s", dati, ip)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(dati, ?) > 0 OR instr(dati, ?) > 0`, tok, startDi(t, secondo)) != 0 {
		t.Error("il token è nell'audit")
	}
	if conta(t, d, `SELECT count(*) FROM iscrizioni_telegram`) != 0 {
		t.Error("il pulsante ha iscritto qualcuno")
	}
}

// inventario: CLI-45
// Il link di un messaggio WhatsApp ({telegram_optin}): ogni apertura un token nuovo di 60 minuti esatti,
// via «whatsapp»; aprirlo non iscrive nessuno.
func TestTokenTelegramWhatsApp(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	uno, err := b.LinkTelegram(ctx, c.Token, baseEmail, pubblico)
	if err != nil {
		t.Fatal(err)
	}
	due, err := b.LinkTelegram(ctx, c.Token, baseEmail, pubblico)
	if err != nil || startDi(t, uno) == startDi(t, due) {
		t.Fatalf("seconda apertura: %v", err)
	}
	atteso := o.Adesso().UTC().Add(time.Hour).Format(db.FormatoIstante)
	if n := conta(t, d, `SELECT count(*) FROM token_telegram WHERE busta_id = ? AND scade_il = ?`, c.ID, atteso); n != 2 {
		t.Errorf("token di 60 minuti: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_optin' AND dati = ?`,
		`{"bag_number":"`+c.Numero+`","link":"token","via":"whatsapp"}`); n != 2 {
		t.Errorf("audit whatsapp: %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM iscrizioni_telegram`) != 0 {
		t.Error("il link ha iscritto qualcuno")
	}
}

// inventario: CLI-44, CLI-45
// Sei all'ora per busta, contati a parte per il pulsante e per il link WhatsApp (via); una riga di prima
// senza via conta come della pagina; dopo un'ora si riparte. Oltre il limite niente si scrive.
func TestTelegramLimiti(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	for i := range 6 {
		if _, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico); err != nil {
			t.Fatalf("pressione %d: %v", i+1, err)
		}
	}
	if _, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico); !errors.Is(err, ErrTroppeRichiesteTelegram) {
		t.Fatalf("settima pressione: %v", err)
	}
	for i := range 6 {
		if _, err := b.LinkTelegram(ctx, c.Token, baseEmail, pubblico); err != nil {
			t.Fatalf("apertura %d: %v", i+1, err)
		}
	}
	if _, err := b.LinkTelegram(ctx, c.Token, baseEmail, pubblico); !errors.Is(err, ErrTroppeRichiesteTelegram) {
		t.Fatalf("settima apertura: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM token_telegram`); n != 12 {
		t.Errorf("token: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_optin'`); n != 12 {
		t.Errorf("righe: %d", n)
	}
	// Il limite è della busta: un'altra busta, nella stessa ora, ha i suoi sei per via (review della 072).
	seconda := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-SECONDA" }))
	if _, err := b.IscriviTelegramCliente(ctx, seconda.Token, baseEmail, pubblico); err != nil {
		t.Errorf("altra busta, pulsante: %v", err)
	}
	if _, err := b.LinkTelegram(ctx, seconda.Token, baseEmail, pubblico); err != nil {
		t.Errorf("altra busta, link WhatsApp: %v", err)
	}
	o.Sposta(time.Hour + time.Second)
	if _, err := b.IscriviTelegramCliente(ctx, c.Token, baseEmail, pubblico); err != nil {
		t.Errorf("dopo un'ora: %v", err)
	}
	// Le righe di prima della via (slice 136 dello standalone) contano come della pagina.
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" }))
	for range 6 {
		esegui(t, d, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES ('public', 'repair.telegram_optin', 'repair', ?, 'ok', '{"link":"token"}', ?)`,
			altra.ID, o.Adesso().UTC().Format(db.FormatoIstante))
	}
	if _, err := b.IscriviTelegramCliente(ctx, altra.Token, baseEmail, pubblico); !errors.Is(err, ErrTroppeRichiesteTelegram) {
		t.Errorf("righe senza via: %v", err)
	}
	if _, err := b.LinkTelegram(ctx, altra.Token, baseEmail, pubblico); err != nil {
		t.Errorf("righe senza via, link WhatsApp: %v", err)
	}
}

// inventario: CLI-44, CLI-45
// I rifiuti nell'ordine dello standalone (issue_link/4): link morto, busta nel cestino, busta conto
// terzi (anche chiusa), busta chiusa, poi il canale spento. Nessuno scrive niente.
func TestTelegramRifiuti(t *testing.T) {
	b, d, _, p := provaTelegram(t)
	diretta := crea(t, b, modulo())
	oref := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "QR-OREF" }))
	orefChiusa := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "QR-OREF-CHIUSA" }))
	chiusa := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-CHIUSA" }))
	cestino := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-CESTINO" }))
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id IN (?, ?)`, chiusa.ID, orefChiusa.ID)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for _, caso := range []struct {
		nome, token string
		atteso      error
	}{
		{"link morto", "QUESTO-TOKEN-NON-ESISTE", ErrBustaNonTrovata},
		{"vuoto", "", ErrBustaNonTrovata},
		{"cestino", cestino.Token, ErrBustaNonTrovata},
		{"oreficeria", oref.Token, ErrTelegramNascosto},
		{"oreficeria chiusa", orefChiusa.Token, ErrTelegramNascosto},
		{"chiusa", chiusa.Token, ErrTelegramChiusa},
	} {
		if _, err := b.IscriviTelegramCliente(ctx, caso.token, baseEmail, pubblico); !errors.Is(err, caso.atteso) {
			t.Errorf("pagina, %s: %v", caso.nome, err)
		}
		if _, err := b.LinkTelegram(ctx, caso.token, baseEmail, pubblico); !errors.Is(err, caso.atteso) {
			t.Errorf("WhatsApp, %s: %v", caso.nome, err)
		}
	}
	p.spento = true
	if _, err := b.IscriviTelegramCliente(ctx, diretta.Token, baseEmail, pubblico); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("spento: %v", err)
	}
	if _, err := b.LinkTelegram(ctx, diretta.Token, baseEmail, pubblico); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("spento (WhatsApp): %v", err)
	}
	p.spento = false
	if _, err := b.LinkTelegram(ctx, diretta.Token, "", pubblico); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("senza indirizzo pubblico: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 0 || conta(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("un rifiuto ha scritto")
	}
}

// inventario: CLI-44
// Il riquadro (box/2): nil sulla busta chiusa, su quella conto terzi, nel cestino e sul link storico
// revocato; «attive» solo con un'iscrizione con il consenso e non fermata.
func TestRiquadroTelegram(t *testing.T) {
	b, d, _, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	stato := func(nome string, storico bool, atteso string) {
		t.Helper()
		r, err := b.RiquadroTelegram(ctx, c.ID, storico)
		got := "nil"
		switch {
		case r != nil && r.Attive:
			got = "attive"
		case r != nil:
			got = "proposta"
		}
		if err != nil || got != atteso {
			t.Errorf("%s: %s %v, atteso %s", nome, got, err, atteso)
		}
	}
	stato("senza iscrizioni", false, "proposta")
	stato("senza iscrizioni, storico", true, "proposta")
	iscrivi(t, d, c.ID, 1001, 0, nil)
	stato("consenso spento", false, "proposta")
	iscrivi(t, d, c.ID, 1002, 1, istanteProva)
	stato("con consenso ma fermata", false, "proposta")
	iscrivi(t, d, c.ID, 1003, 1, nil)
	stato("attiva", false, "attive")
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	stato("storico revocato", true, "nil")
	stato("link token di una busta con lo storico revocato", false, "attive")
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = NULL, oreficeria_id = 2 WHERE id = ?`, c.ID)
	stato("conto terzi", false, "nil")
	stato("conto terzi, storico", true, "nil")
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, stato = 'annullata' WHERE id = ?`, c.ID)
	stato("chiusa", false, "nil")
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione', cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	stato("cestino", false, "nil")
}

// inventario: CLI-44
// «Disattiva» (revoke/3): ogni iscrizione ancora attiva della busta si spegne (consenso 0, fermata
// adesso, motivo «page») con una riga repair.telegram_stop per ognuna (busta, iscrizione, via «page»,
// mai la chat); quelle già fermate e quelle di altre buste restano; anche su una busta chiusa; la busta
// conto terzi è nascosta.
func TestDisattivaTelegram(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	altra := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "QR-ALTRA" }))
	uno := iscrivi(t, d, c.ID, 7770001, 1, nil)
	due := iscrivi(t, d, c.ID, 7770002, 0, nil)
	ferma := iscrivi(t, d, c.ID, 7770003, 0, "2026-10-30T09:00:00.000000Z")
	esegui(t, d, `UPDATE iscrizioni_telegram SET motivo_fermata = 'stop' WHERE id = ?`, ferma)
	fuori := iscrivi(t, d, altra.ID, 7770001, 1, nil)
	o.Sposta(time.Minute)
	if err := b.DisattivaTelegramCliente(ctx, c.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().UTC().Format(db.FormatoIstante)
	for _, id := range []int64{uno, due} {
		if n := conta(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND consenso = 0 AND fermata_il = ? AND motivo_fermata = 'page'
			AND aggiornato_il = ?`, id, ora, ora); n != 1 {
			t.Errorf("iscrizione %d non spenta", id)
		}
		dati := `{"bag_number":"` + c.Numero + `","subscription_id":` + itoa(id) + `,"via":"page"}`
		if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop' AND id_risorsa = ? AND dati = ? AND attore = 'public'`,
			itoa(c.ID), dati); n != 1 {
			t.Errorf("audit dell'iscrizione %d: %d", id, n)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND fermata_il = '2026-10-30T09:00:00.000000Z' AND motivo_fermata = 'stop'`, ferma); n != 1 {
		t.Error("l'iscrizione già fermata è cambiata")
	}
	if n := conta(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND consenso = 1 AND fermata_il IS NULL`, fuori); n != 1 {
		t.Error("spenta l'iscrizione di un'altra busta")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop'`); n != 2 {
		t.Errorf("righe: %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE instr(dati, '777000') > 0`) != 0 {
		t.Error("la chat è nell'audit")
	}
	// Di nuovo: niente da spegnere, nessuna riga, nessun errore.
	if err := b.DisattivaTelegramCliente(ctx, c.Token, pubblico); err != nil || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop'`) != 2 {
		t.Errorf("seconda volta: %v", err)
	}
	// Una busta chiusa si disattiva lo stesso (il trigger l'ha già spenta: niente da fare).
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, altra.ID)
	if err := b.DisattivaTelegramCliente(ctx, altra.Token, pubblico); err != nil {
		t.Errorf("chiusa: %v", err)
	}
	oref := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2"; a.CodiceQR = "QR-OREF" }))
	if err := b.DisattivaTelegramCliente(ctx, oref.Token, pubblico); !errors.Is(err, ErrTelegramNascosto) {
		t.Errorf("conto terzi: %v", err)
	}
	if err := b.DisattivaTelegramCliente(ctx, "NON-ESISTE", pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("link morto: %v", err)
	}
}

// inventario: CLI-19, CLI-44
// Dalla pagina storica con la credenziale: il pulsante (link «legacy») e «Disattiva»; una credenziale
// sbagliata, una busta conto terzi e uno storico revocato sono il link che non apre niente, con la riga
// repair.legacy_public_denied; una revoca arrivata dopo la verifica della rotta non emette token.
func TestTelegramDallaPaginaStorica(t *testing.T) {
	b, d, o, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	giusta := CredenzialeStorica{Codice: "segreto1", ConCodice: true}
	sbagliata := CredenzialeStorica{Codice: "no", ConCodice: true}
	link, err := b.IscriviTelegramStorica(ctx, c.Numero, giusta, baseEmail, pubblico)
	if err != nil {
		t.Fatal(err)
	}
	startDi(t, link)
	if atteso := o.Adesso().UTC().Add(24 * time.Hour).Format(db.FormatoIstante); conta(t, d, `SELECT count(*) FROM token_telegram WHERE scade_il = ?`, atteso) != 1 {
		t.Error("storico: il token non vale 24 ore esatte")
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_optin' AND dati = ?`,
		`{"bag_number":"`+c.Numero+`","link":"legacy","via":"page"}`); n != 1 {
		t.Errorf("audit storico: %d", n)
	}
	sub := iscrivi(t, d, c.ID, 4242, 1, nil)
	if err := b.DisattivaTelegramStorica(ctx, c.Numero, giusta, pubblico); err != nil {
		t.Fatal(err)
	}
	if conta(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND motivo_fermata = 'page'`, sub) != 1 {
		t.Error("storico: iscrizione non spenta")
	}
	negati := func() int {
		return conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.legacy_public_denied'`)
	}
	n := negati()
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 1 {
		t.Error("storico: nessun token")
	}
	if _, err := b.IscriviTelegramStorica(ctx, c.Numero, sbagliata, baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("sbagliata: %v", err)
	}
	if err := b.DisattivaTelegramStorica(ctx, c.Numero, sbagliata, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("sbagliata (disattiva): %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 1 {
		t.Error("una credenziale sbagliata ha scritto un token")
	}
	// Passata a un'oreficeria: il trigger della 0014 toglie i token della busta; da qui nessuno se ne scrive.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if _, err := b.IscriviTelegramStorica(ctx, c.Numero, giusta, baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("conto terzi: %v", err)
	}
	if err := b.DisattivaTelegramStorica(ctx, c.Numero, giusta, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("conto terzi (disattiva): %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if _, err := b.IscriviTelegramStorica(ctx, c.Numero, giusta, baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("revocata: %v", err)
	}
	if negati() != n+5 {
		t.Errorf("righe del rifiuto: %d", negati()-n)
	}
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = NULL, stato = 'consegnata' WHERE id = ?`, c.ID)
	if _, err := b.IscriviTelegramStorica(ctx, c.Numero, giusta, baseEmail, pubblico); !errors.Is(err, ErrTelegramChiusa) {
		t.Errorf("chiusa: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 0 {
		t.Error("un rifiuto ha scritto un token")
	}
}

// inventario: CLI-19, CLI-44
// La revoca dello storico che arriva fra la verifica della rotta e la scrittura (link_alive/2): la
// transazione la vede e non emette il token.
func TestTelegramStoricaRevocataNelFrattempo(t *testing.T) {
	b, d, _, _ := provaTelegram(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	b.primaDelToken = func() {
		esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	}
	if _, err := b.IscriviTelegramStorica(ctx, c.Numero, CredenzialeStorica{Codice: "segreto1", ConCodice: true}, baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("revocata nel frattempo: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM token_telegram`) != 0 {
		t.Error("token emesso dopo la revoca")
	}
}

// inventario: CLI-32, CLI-45
// Il popup avvisa del link Telegram che non porta a niente solo quando il canale non è disponibile.
func TestPopupTelegramSpentoSoloSenzaCanale(t *testing.T) {
	b, _, _, p := provaTelegram(t)
	b.ConIndirizzoPubblico(baseEmail)
	id := bustaAvviso(t, b, false, "")
	if z := apriAvviso(t, b, id, ScopoControparte); z.TelegramSpento {
		t.Error("canale disponibile: il popup lo dice spento")
	}
	p.spento = true
	if z := apriAvviso(t, b, id, ScopoControparte); !z.TelegramSpento {
		t.Error("canale spento: il popup non lo dice")
	}
	// Senza indirizzo pubblico il link t.me non serve: gli avvisi della 074 non avrebbero il pulsante.
	p.spento = false
	b.ConIndirizzoPubblico("")
	if z := apriAvviso(t, b, id, ScopoControparte); !z.TelegramSpento {
		t.Error("senza indirizzo pubblico: il popup non lo dice")
	}
}

// codaRotta è una coda che non sa dire se il canale Telegram è acceso.
type codaRotta struct{ Posta }

func (codaRotta) CanaleAttivo(context.Context, string) (bool, error) {
	return false, errors.New("database guasto")
}

// inventario: CLI-32, CLI-45
// Un errore nel leggere lo stato di Telegram non toglie il popup WhatsApp: il canale vale spento e la bozza
// lo dice (TelegramNonLetto), così la pagina lo scrive nel registro (review della 072).
func TestPopupTelegramStatoNonLetto(t *testing.T) {
	b, _, _, _ := provaTelegram(t)
	b.ConIndirizzoPubblico(baseEmail)
	b.ConPosta(codaRotta{b.posta}, b.chiaveEmail)
	id := bustaAvviso(t, b, false, "")
	z, err := b.ApriAvviso(ctx, id, ScopoControparte)
	if err != nil {
		t.Fatalf("il popup non si apre: %v", err)
	}
	if !z.TelegramSpento || !z.TelegramNonLetto {
		t.Errorf("bozza: spento %v, non letto %v", z.TelegramSpento, z.TelegramNonLetto)
	}
}
