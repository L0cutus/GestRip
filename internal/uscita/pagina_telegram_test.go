package uscita

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// La pagina «Provider di invio», parte Telegram (slice 071, ADM-10, CLI-52 parte Telegram), come
// Outgoing.Config.put(:telegram, …), Telegram.changeset/2, TelegramInbound.set_enabled/2, status/0 e
// refresh_bot_username/0 dello standalone a go-baseline (config_test.exs, providers_live_test.exs), più
// l'interruttore dell'invio Telegram (D-03) con la revisione dei messaggi rimasti in coda (D-27), solo del
// Go. Il Bot API è quello finto. Dati inventati.

func tokenSalvato(t *testing.T, c *Coda) string {
	t.Helper()
	var v string
	if err := c.d.Lettura.QueryRow(`SELECT valore FROM segreti_provider WHERE nome = 'outgoing.telegram.bot_token'`).Scan(&v); err != nil {
		return "(assente)"
	}
	return v
}

// codaTelegramVuota è la coda col provider Telegram sul Bot API finto e niente configurato.
func codaTelegramVuota(t *testing.T) (*Coda, *botFinto) {
	t.Helper()
	d, o := apriDB(t)
	b := nuovoBotFinto(t)
	return Nuova(d, o, map[string]Provider{CanaleTelegram: NuovoTelegram(d, b.srv.URL)}), b
}

// inventario: ADM-10, CLI-52
// Config.put(:telegram): il token in segreti_provider (mai tra le impostazioni), la chat in impostazioni, e
// outgoing.provider_configured (risorsa outgoing_provider, id telegram) con il canale, i nomi delle chiavi
// cambiate e se è configurato; mai un valore. Casella del token vuota = lo tiene; «Cancella il token
// salvato» toglie solo il token. Un token nuovo dimentica lo stato del bot (nome, offset, ultimo errore,
// aggiornamento in errore: forget_bot_state/2), le chat sconosciute no.
func TestSalvaTelegram(t *testing.T) {
	c, _ := codaTelegramVuota(t)
	ctx := context.Background()
	rif, err := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": tokenFinto, "chat_id": chatFinta}, contesto)
	if err != nil || rif != nil {
		t.Fatalf("%+v %v", rif, err)
	}
	if tokenSalvato(t, c) != tokenFinto || valoreImpostazione(t, c, "outgoing.telegram.chat_id") != chatFinta {
		t.Error("non salvato")
	}
	if valoreImpostazione(t, c, "outgoing.telegram.bot_token") != "(assente)" {
		t.Error("token tra le impostazioni")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.provider_configured"); id != "telegram" || dati != `{"changed":["bot_token","chat_id"],"channel":"telegram","configured":true}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%AAH%'`); n != 0 {
		t.Error("token nell'audit")
	}
	// Lo stato del bot del token di prima.
	for _, k := range []string{ChiaveNomeBot, "telegram_inbound.offset", ChiaveErroreLettore, ChiaveErroreLettoreIl, "telegram_inbound.failing_update"} {
		impostazione(t, c.d, k, "vecchio")
	}
	impostazione(t, c.d, ChiaveChatSconosciute, "3")
	// Casella vuota: tiene il token e non dimentica niente.
	if rif, err := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": "  ", "chat_id": "@negozio_prova"}, contesto); err != nil || rif != nil {
		t.Fatalf("%+v %v", rif, err)
	}
	if tokenSalvato(t, c) != tokenFinto || valoreImpostazione(t, c, ChiaveNomeBot) != "vecchio" {
		t.Error("token o stato del bot persi senza un token nuovo")
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["chat_id"],"channel":"telegram","configured":true}` {
		t.Errorf("audit %s", dati)
	}
	// Token nuovo: lo stato del bot va via.
	if rif, err := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": "654321:BBB_altro-token"}, contesto); err != nil || rif != nil {
		t.Fatalf("%+v %v", rif, err)
	}
	for _, k := range []string{ChiaveNomeBot, "telegram_inbound.offset", ChiaveErroreLettore, ChiaveErroreLettoreIl, "telegram_inbound.failing_update"} {
		if v := valoreImpostazione(t, c, k); v != "" {
			t.Errorf("%s = %q dopo il token nuovo", k, v)
		}
	}
	if valoreImpostazione(t, c, ChiaveChatSconosciute) != "3" {
		t.Error("chat sconosciute dimenticate")
	}
	// Cancella: solo il token.
	if rif, err := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": "", "chat_id": "@negozio_prova", "clear_secret": "1"}, contesto); err != nil || rif != nil {
		t.Fatalf("%+v %v", rif, err)
	}
	if tokenSalvato(t, c) != "(assente)" || valoreImpostazione(t, c, "outgoing.telegram.chat_id") != "@negozio_prova" {
		t.Error("cancella")
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["bot_token"],"channel":"telegram","configured":false}` {
		t.Errorf("audit %s", dati)
	}
}

// inventario: ADM-10, CLI-52
// Telegram.changeset/2: token «^\d+:[A-Za-z0-9_\-]+$» e chat «^-?\d+$|^@[A-Za-z0-9_]+$» (regex senza «u»:
// «$» lascia passare un solo a capo finale, come lo standalone), al più 200 e 64 caratteri, una spunta
// che non è un booleano; solo sui campi cambiati. Rifiutato: niente scritto, gli errori per campo dal più
// recente e i valori mandati, mai il token.
func TestSalvaTelegramRifiuti(t *testing.T) {
	c, _ := codaTelegramVuota(t)
	ctx := context.Background()
	casi := []struct {
		modulo ModuloTelegram
		errori map[string][]string
	}{
		{ModuloTelegram{"bot_token": "abc", "chat_id": "chat"}, map[string][]string{"bot_token": {"Formato non valido."}, "chat_id": {"Formato non valido."}}},
		{ModuloTelegram{"bot_token": "123:abc def"}, map[string][]string{"bot_token": {"Formato non valido."}}},
		{ModuloTelegram{"bot_token": "123:" + strings.Repeat("a", 197)}, map[string][]string{"bot_token": {"Troppo lungo (massimo 200 caratteri)."}}},
		{ModuloTelegram{"bot_token": "1:a", "chat_id": "@" + strings.Repeat("a", 64)}, map[string][]string{"chat_id": {"Troppo lungo (massimo 64 caratteri)."}}},
		{ModuloTelegram{"bot_token": "1:a", "chat_id": "@" + strings.Repeat("è", 64)}, map[string][]string{"chat_id": {"Troppo lungo (massimo 64 caratteri).", "Formato non valido."}}},
		{ModuloTelegram{"bot_token": "1:a", "chat_id": "--1"}, map[string][]string{"chat_id": {"Formato non valido."}}},
		{ModuloTelegram{"bot_token": "1:a", "chat_id": "4242", "clear_secret": "forse"}, map[string][]string{"clear_secret": {"Valore non valido."}}},
	}
	for i, k := range casi {
		rif, err := c.SalvaTelegram(ctx, k.modulo, contesto)
		if err != nil || rif == nil {
			t.Fatalf("%d: %+v %v", i, rif, err)
		}
		for campo, atteso := range k.errori {
			if !slices.Equal(rif.Errori[campo], atteso) {
				t.Errorf("%d %s: %q", i, campo, rif.Errori[campo])
			}
		}
		if len(rif.Errori) != len(k.errori) {
			t.Errorf("%d: errori %v", i, rif.Errori)
		}
		if rif.Valori["bot_token"] != "" || rif.Valori["chat_id"] != k.modulo["chat_id"] {
			t.Errorf("%d: valori %q", i, rif.Valori)
		}
	}
	if n := conta(t, c.d, `SELECT (SELECT count(*) FROM eventi_audit) + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM impostazioni)`); n != 0 {
		t.Errorf("scritto qualcosa: %d", n)
	}
	// Il «$» senza «u»: un a capo finale passa (come lo standalone), due no.
	if rif, _ := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": "1:a\n", "chat_id": "4242\n"}, contesto); rif != nil {
		t.Errorf("a capo finale rifiutato: %v", rif.Errori)
	}
	if rif, _ := c.SalvaTelegram(ctx, ModuloTelegram{"chat_id": "4242\n\n"}, contesto); rif == nil {
		t.Error("due a capo accettati")
	}
	// Solo i cambiati: una chat importata storta si risalva com'è cambiando il token.
	impostazione(t, c.d, "outgoing.telegram.chat_id", "chat importata")
	if rif, _ := c.SalvaTelegram(ctx, ModuloTelegram{"bot_token": tokenFinto, "chat_id": "chat importata"}, contesto); rif != nil {
		t.Errorf("chat non cambiata validata: %v", rif.Errori)
	}
}

// inventario: ADM-10, CLI-52
// Quello che la pagina mostra (load/1 e TelegramInbound.status/0): la chat, se c'è un token (mai il
// token), configurato, l'invio acceso (D-03), le notifiche ai clienti, il nome del bot, le chat
// sconosciute, l'ultimo errore del lettore con la sua ora e il conflitto (un errore che comincia con
// «409»); con l'invio spento i messaggi Telegram in coda da guardare (D-27), dal più vecchio.
func TestPaginaTelegram(t *testing.T) {
	c, _ := codaTelegramVuota(t)
	ctx := context.Background()
	p, err := c.PaginaTelegram(ctx)
	if err != nil || p.Configurata || p.TokenSalvato || p.Acceso || p.NotificheClienti || p.NomeBot != "" || p.UltimoErrore != "" || p.Conflitto ||
		p.UltimoErroreIl != nil || p.ChatSconosciute != 0 || len(p.InSospeso) != 0 {
		t.Fatalf("vuota %+v %v", p, err)
	}
	impostaTelegram(t, c.d, tokenFinto, chatFinta, false)
	impostazione(t, c.d, ChiaveNotificheClienti, "1")
	impostazione(t, c.d, ChiaveNomeBot, "negozio_prova_bot")
	impostazione(t, c.d, ChiaveChatSconosciute, "4")
	impostazione(t, c.d, ChiaveErroreLettore, "409: Conflict: can't use getUpdates method while webhook is active")
	impostazione(t, c.d, ChiaveErroreLettoreIl, "2026-10-12T08:00:00Z")
	if _, err := c.d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (7, 'telegram', 'customer', '4242', 'Pronta', 'queued', 1, ?1, ?1), (5, 'telegram', 'shop', ?2, 'Prova', 'queued', 0, ?1, ?1),
		(6, 'telegram', 'shop', ?2, 'Fatta', 'accepted', 1, ?1, ?1), (4, 'email', 'shop', 'negozio@example.invalid', 'Email', 'queued', 0, ?1, ?1)`,
		fra(-time.Hour), chatFinta); err != nil {
		t.Fatal(err)
	}
	p, err = c.PaginaTelegram(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Configurata || !p.TokenSalvato || p.Acceso || !p.NotificheClienti || p.Chat != chatFinta || p.NomeBot != "negozio_prova_bot" ||
		p.ChatSconosciute != 4 || !p.Conflitto || !strings.HasPrefix(p.UltimoErrore, "409: Conflict") {
		t.Errorf("pagina %+v", p)
	}
	if p.UltimoErroreIl == nil || !p.UltimoErroreIl.Equal(time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("ora dell'errore %v", p.UltimoErroreIl)
	}
	if len(p.InSospeso) != 2 || p.InSospeso[0].ID != 5 || p.InSospeso[1].ID != 7 {
		t.Errorf("in sospeso %+v", p.InSospeso)
	}
	impostazione(t, c.d, ChiaveErroreLettore, "telegram:http:502")
	impostazione(t, c.d, ChiaveErroreLettoreIl, "non è una data")
	impostazione(t, c.d, ChiaveTelegramAcceso, "1")
	if p, _ = c.PaginaTelegram(ctx); p.Conflitto || p.UltimoErroreIl != nil || !p.Acceso || len(p.InSospeso) != 0 {
		t.Errorf("acceso %+v", p)
	}
}

// inventario: OPS-02
// L'accensione dell'invio Telegram (D-03, solo Go): spento all'inizio; non si accende senza una
// configurazione usabile né con dei messaggi Telegram in coda (D-27); acceso scrive
// canale_telegram_acceso = 1 e outgoing.channel_enabled (id telegram); la seconda volta niente; spento 0 e
// outgoing.channel_disabled. I messaggi in coda si scartano o si mandano uno per uno solo da spento; un
// messaggio Telegram «Manda adesso» parte con ConsegnaAMano e l'esito si scrive sulla riga come in un
// giro (un 400 la fa fallire).
func TestAccensioneTelegramConRevisione(t *testing.T) {
	c, b := codaTelegramVuota(t)
	ctx := context.Background()
	if err := c.ImpostaTelegram(ctx, true, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Fatalf("senza configurazione: %v", err)
	}
	impostaTelegram(t, c.d, tokenFinto, chatFinta, false)
	if _, err := c.d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (5, 'telegram', 'shop', ?2, 'Prova', 'queued', 0, ?1, ?1), (7, 'telegram', 'shop', ?2, 'Seconda', 'queued', 4, ?1, ?1),
		(8, 'email', 'shop', 'negozio@example.invalid', 'Email', 'queued', 0, ?1, ?1)`, fra(-time.Hour), chatFinta); err != nil {
		t.Fatal(err)
	}
	if err := c.ImpostaTelegram(ctx, true, contesto); !errors.Is(err, ErrTelegramInSospeso) {
		t.Fatalf("con i messaggi in coda: %v", err)
	}
	// La riga email non è di questo provider (la coda ha solo Telegram).
	if _, err := c.ScartaInSospeso(ctx, 8, contesto); !errors.Is(err, ErrNonInSospeso) {
		t.Errorf("scarta l'email: %v", err)
	}
	canale, err := c.ScartaInSospeso(ctx, 5, contesto)
	if err != nil || canale != CanaleTelegram {
		t.Fatalf("scarta %q %v", canale, err)
	}
	if id, dati := ultimoAudit(t, c, "outgoing.queued_discarded"); id != "5" || dati != `{"channel":"telegram","party":"shop","repair_id":null}` {
		t.Errorf("audit dello scarto %s %s", id, dati)
	}
	b.risposta(rifiutoBot(400, "Bad Request: chat not found"))
	e, err := c.MandaInSospeso(ctx, 7, contesto)
	if err != nil || e != (EsitoAMano{Canale: CanaleTelegram, Stato: "failed", Errore: "telegram:http:400:Bad Request: chat not found", Tentativi: 5}) {
		t.Fatalf("manda %+v %v", e, err)
	}
	if got := riga(t, c.d, 7); got != "failed|5|"+fra(-time.Hour)+"|-|"+fra(0)+"|-|telegram:http:400:Bad Request: chat not found" {
		t.Errorf("riga %s", got)
	}
	if err := c.ImpostaTelegram(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	if valoreImpostazione(t, c, ChiaveTelegramAcceso) != "1" {
		t.Error("interruttore")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.channel_enabled"); id != "telegram" || dati != `{"channel":"telegram"}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if err := c.ImpostaTelegram(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.channel_enabled'`); n != 1 {
		t.Errorf("acceso due volte: %d", n)
	}
	if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'queued', fallito_il = NULL, tentativi = 0 WHERE id = 7`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ScartaInSospeso(ctx, 7, contesto); !errors.Is(err, ErrTelegramAcceso) {
		t.Errorf("scarta da acceso: %v", err)
	}
	if _, err := c.MandaInSospeso(ctx, 7, contesto); !errors.Is(err, ErrTelegramAcceso) {
		t.Errorf("manda da acceso: %v", err)
	}
	if err := c.ImpostaTelegram(ctx, false, contesto); err != nil {
		t.Fatal(err)
	}
	if valoreImpostazione(t, c, ChiaveTelegramAcceso) != "0" {
		t.Error("spento")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.channel_disabled"); id != "telegram" || dati != `{"channel":"telegram"}` {
		t.Errorf("audit %s %s", id, dati)
	}
	b.risposta(nil)
	if e, err := c.MandaInSospeso(ctx, 7, contesto); err != nil || e.Stato != "accepted" || e.Canale != CanaleTelegram {
		t.Errorf("manda %+v %v", e, err)
	}
	// Senza configurazione niente invio.
	if _, err := c.d.Scrittura.Exec(`DELETE FROM segreti_provider`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'queued', accettato_il = NULL WHERE id = 7`); err != nil {
		t.Fatal(err)
	}
	n := b.quante()
	if _, err := c.MandaInSospeso(ctx, 7, contesto); !errors.Is(err, ErrNonConfigurato) || b.quante() != n {
		t.Errorf("manda senza configurazione: %v", err)
	}
}

// inventario: ADM-10, CLI-52
// «Notifiche Telegram ai clienti» (set_enabled/2): «1» accese, "" spente, e
// outgoing.telegram_customer_notifications con «enabled»; il modulo porta lo stato che chiede.
func TestNotificheClienti(t *testing.T) {
	c, _ := codaTelegramVuota(t)
	ctx := context.Background()
	if err := c.ImpostaNotificheClienti(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	if valoreImpostazione(t, c, ChiaveNotificheClienti) != "1" {
		t.Error("accese")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.telegram_customer_notifications"); id != "telegram" || dati != `{"enabled":true}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if err := c.ImpostaNotificheClienti(ctx, false, contesto); err != nil {
		t.Fatal(err)
	}
	if valoreImpostazione(t, c, ChiaveNotificheClienti) != "" {
		t.Error("spente")
	}
	if _, dati := ultimoAudit(t, c, "outgoing.telegram_customer_notifications"); dati != `{"enabled":false}` {
		t.Errorf("audit %s", dati)
	}
}

// inventario: ADM-10, CLI-52
// La prova del bot (test di «telegram»): senza configurazione il rifiuto, nessuna richiesta; poi getMe, e
// se il bot non risponde niente parte (il motivo senza token); se risponde il nome si salva
// (outgoing.telegram.bot_username) e il messaggio di prova dello standalone va alla chat del negozio
// subito, senza coda (come la 069c per la posta), anche con l'invio Telegram spento; audit
// outgoing.test_sent (canale, parte, stato), mai chat o testo.
func TestProvaTelegram(t *testing.T) {
	c, b := codaTelegramVuota(t)
	ctx := context.Background()
	if _, err := c.MandaProvaTelegram(ctx, contesto); !errors.Is(err, ErrNonConfigurato) || b.quante() != 0 {
		t.Fatalf("senza configurazione: %v, %d richieste", err, b.quante())
	}
	impostaTelegram(t, c.d, tokenFinto, chatFinta, false)
	b.risposta(rifiutoBot(401, "Unauthorized"))
	e, err := c.MandaProvaTelegram(ctx, contesto)
	if err != nil || e.GetMe != "telegram:http:401" || b.quante() != 1 {
		t.Fatalf("getMe rifiutato %+v %v, %d richieste", e, err, b.quante())
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent'`); n != 0 {
		t.Error("audit senza invio")
	}
	b.risposta(nil)
	e, err = c.MandaProvaTelegram(ctx, contesto)
	if err != nil || e != (EsitoProvaTelegram{Nome: "negozio_prova_bot"}) {
		t.Fatalf("prova %+v %v", e, err)
	}
	if valoreImpostazione(t, c, ChiaveNomeBot) != "negozio_prova_bot" {
		t.Error("nome del bot non salvato")
	}
	r := b.tutte()[2]
	if r.Percorso != "/bot"+tokenFinto+"/sendMessage" ||
		r.Grezzo != `{"chat_id":"`+chatFinta+`","text":"GestRip — prova del provider Telegram.\nSe stai leggendo questo messaggio, il bot configurato funziona."}` {
		t.Errorf("prova %s %s", r.Percorso, r.Grezzo)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Error("la prova è passata dalla coda")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.test_sent"); id != "telegram" || dati != `{"channel":"telegram","party":"shop","status":"accepted"}` {
		t.Errorf("audit %s %s", id, dati)
	}
	b.risposta(func(r richiestaBot) (int, string) {
		if strings.HasSuffix(r.Percorso, "/getMe") {
			return 200, `{"ok":true,"result":{"username":"negozio_prova_bot"}}`
		}
		return rifiutoBot(400, "Bad Request: chat not found")(r)
	})
	e, err = c.MandaProvaTelegram(ctx, contesto)
	if err != nil || e.Errore != "telegram:http:400:Bad Request: chat not found" {
		t.Fatalf("prova rifiutata %+v %v", e, err)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.test_sent"); dati != `{"channel":"telegram","party":"shop","status":"failed"}` {
		t.Errorf("audit %s", dati)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%' || ? || '%' OR dati LIKE '%AAH%'`, chatFinta); n != 0 {
		t.Error("chat o token nell'audit")
	}
}
