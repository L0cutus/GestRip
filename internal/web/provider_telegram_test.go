package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La pagina «Provider di invio», parte Telegram (provider.go, slice 071, ADM-10, CLI-52 parte Telegram),
// come il riquadro Telegram di ProvidersLive dello standalone a go-baseline (providers_live_test.exs)
// senza LiveView, più l'interruttore dell'invio Telegram con la revisione dei messaggi in coda (D-03,
// D-27), solo del Go. Il provider Telegram è quello vero, puntato a un Bot API finto in processo: nessuna
// richiesta esce mai verso Telegram. Token e chat inventati.

const tokenWeb = "123456:AAH-token-finto-della-pagina"

// botWeb è il Bot API finto: annota i percorsi (mai il token nel test che lo legge) e risponde rispondi.
type botWeb struct {
	mu       sync.Mutex
	percorsi []string
	rispondi func(percorso string) (int, string)
}

func (b *botWeb) quante() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.percorsi)
}

// facciaConTelegram è la faccia interna con la coda in uscita sul provider email finto e sul provider
// Telegram vero puntato al Bot API finto.
func facciaConTelegram(t *testing.T, reg *bytes.Buffer) (http.Handler, *db.DB, *botWeb) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	bot := &botWeb{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		bot.mu.Lock()
		bot.percorsi = append(bot.percorsi, r.URL.Path)
		f := bot.rispondi
		bot.mu.Unlock()
		stato, corpo := 200, `{"ok":true,"result":{"message_id":91}}`
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			corpo = `{"ok":true,"result":{"username":"negozio_prova_bot"}}`
		}
		if f != nil {
			stato, corpo = f(r.URL.Path)
		}
		w.WriteHeader(stato)
		_, _ = io.WriteString(w, corpo)
	}))
	t.Cleanup(srv.Close)
	l := nessunRegistro
	if reg != nil {
		l = slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, l, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	dd.Uscita = uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: &providerWeb{d: d}, uscita.CanaleTelegram: uscita.NuovoTelegram(d, srv.URL)})
	return mustInterna(t, dd), d, bot
}

func moduloTelegram(b *browser, kv ...string) url.Values {
	v := url.Values{"_csrf_token": {b.token}, "telegram[bot_token]": {tokenWeb}, "telegram[chat_id]": {"-1001"}}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

// inviaETorna manda il modulo, pretende il ritorno alla pagina e dà la pagina dopo.
func inviaETorna(t *testing.T, b *browser, percorso string, v url.Values) string {
	t.Helper()
	if r := b.vai("POST", percorso, v, nil); !redirectA(r, "/settings/providers") {
		t.Fatalf("%s: %d %q", percorso, r.codice, r.location)
	}
	return b.vai("GET", "/settings/providers", nil, nil).corpo
}

// inventario: ADM-10, CLI-52
// Il riquadro Telegram senza niente configurato, fra la posta e la coda: «Non configurato», il modulo (il
// token come password vuota senza segnaposto, la chat con l'aiuto dello standalone, «Cancella il token
// salvato», «Salva provider Telegram»), «✈ Invia messaggio di prova», «Nome del bot non ancora letto», le
// notifiche ai clienti «Spente» con «Accendi», e l'invio Telegram (D-03) «Spento». Mai l'indirizzo del Bot
// API.
func TestPaginaProviderTelegram(t *testing.T) {
	h, _, _ := facciaConTelegram(t, nil)
	b := titolareDentro(t, h)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "pagina", c,
		`<section id="telegram-card" class="provider-riquadro">`,
		`<h2>Telegram</h2> <span id="telegram-state" class="badge">Non configurato</span>`,
		`<form id="telegram-form" action="/settings/providers/telegram" method="post" class="provider-modulo">`,
		`<label for="telegram_bot_token">Token del bot</label> <input type="password" id="telegram_bot_token" name="telegram[bot_token]" value="" autocomplete="new-password" placeholder="">`,
		`<label for="telegram_chat_id">Chat di destinazione</label> <input type="text" id="telegram_chat_id" name="telegram[chat_id]">`,
		`<p id="telegram-chat-help" class="sotto">Usa un gruppo o una chat diversa da quella con cui provi il bot da cliente: i messaggi da questa chat sono ignorati.</p>`,
		`<input type="checkbox" name="telegram[clear_secret]" value="1"> Cancella il token salvato</label>`,
		`<button type="submit">Salva provider Telegram</button>`,
		`<form id="telegram-test-form" action="/settings/providers/prova-telegram" method="post">`,
		`<button id="telegram-test" type="submit" class="pulsante-contorno">✈ Invia messaggio di prova</button>`,
		`<p id="telegram-bot-username" class="sotto">Nome del bot non ancora letto: lo legge «Invia messaggio di prova».</p>`,
		`<h3>Notifiche Telegram ai clienti</h3> <span id="telegram-customer-state" class="badge">Spente</span>`,
		"Accese, l&#39;installazione legge i messaggi che arrivano al bot. Per ora risponde a chiunque scriva che le notifiche si attivano dal link della riparazione: l&#39;iscrizione dei clienti non c&#39;è ancora.",
		`<input type="hidden" name="enabled" value="true"><button id="telegram-customer-toggle" type="submit" class="pulsante-contorno">Accendi</button>`,
		`<h3>Invio Telegram</h3> <span id="telegram-switch-state" class="badge">Spento</span>`,
		`<input type="hidden" name="acceso" value="1"><button id="telegram-switch-toggle" type="submit">Accendi l'invio Telegram</button>`)
	nonContiene(t, "pagina", c, "telegram-inbound-error", "Il bot non è configurato", "Risposte fisse", "api.telegram.org", "127.0.0.1", "salvato — lascia vuoto")
	if i, j, k := strings.Index(c, `id="smtp-card"`), strings.Index(c, `id="telegram-card"`), strings.Index(c, `id="outgoing-queue"`); !(i < j && j < k) {
		t.Errorf("ordine dei riquadri %d %d %d", i, j, k)
	}
}

// inventario: ADM-10, CLI-52
// «Salva provider Telegram»: «Provider salvato.», «Configurato», la chat nel modulo, il token mai (casella
// vuota con «salvato — lascia vuoto per non cambiarlo»), nemmeno nel registro del server; un rifiuto
// ridisegna la pagina con gli errori sotto i campi e i valori mandati, mai il token; «Cancella il token
// salvato» lo toglie.
func TestSalvaProviderTelegram(t *testing.T) {
	var reg bytes.Buffer
	h, d, _ := facciaConTelegram(t, &reg)
	b := titolareNeiProvider(t, h)
	c := inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b))
	contiene(t, "salvato", c, `<div role="status" class="flash flash-info">Provider salvato.</div>`,
		`<span id="telegram-state" class="badge badge-ok">Configurato</span>`,
		`<input type="password" id="telegram_bot_token" name="telegram[bot_token]" value="" autocomplete="new-password" placeholder="salvato — lascia vuoto per non cambiarlo">`,
		`<input type="text" id="telegram_chat_id" name="telegram[chat_id]" value="-1001">`)
	r := b.vai("POST", "/settings/providers/telegram", moduloTelegram(b, "telegram[bot_token]", "AAH-senza-numero", "telegram[chat_id]", "chat sbagliata"), nil)
	if r.codice != 200 {
		t.Fatalf("rifiuto %d", r.codice)
	}
	contiene(t, "rifiuto", r.corpo, `<input type="password" id="telegram_bot_token" name="telegram[bot_token]" value=""`,
		`id="telegram_chat_id" name="telegram[chat_id]" value="chat sbagliata"`, `<p class="field-error">Formato non valido.</p>`)
	nonContiene(t, "rifiuto", r.corpo, "AAH-senza-numero", "Provider salvato.")
	c = inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b, "telegram[bot_token]", "", "telegram[clear_secret]", "1"))
	contiene(t, "cancellato", c, `<span id="telegram-state" class="badge">Non configurato</span>`,
		`name="telegram[bot_token]" value="" autocomplete="new-password" placeholder="">`)
	if n := contaSQL(t, d, `SELECT count(*) FROM segreti_provider`); n != 0 {
		t.Errorf("token rimasto: %d", n)
	}
	if strings.Contains(reg.String(), "AAH") {
		t.Errorf("token nel registro %q", reg.String())
	}
}

// inventario: ADM-10, CLI-52
// «✈ Invia messaggio di prova»: senza configurazione il rifiuto dello standalone; con un bot che non
// risponde a getMe «Il bot non risponde a getMe (telegram:http:401): niente è stato accodato.»; con il
// bot che risponde il nome sulla pagina e l'esito del messaggio, subito e anche con l'invio spento, mai
// in coda.
func TestProvaProviderTelegram(t *testing.T) {
	h, d, bot := facciaConTelegram(t, nil)
	b := titolareNeiProvider(t, h)
	c := inviaETorna(t, b, "/settings/providers/prova-telegram", soloToken(b))
	contiene(t, "senza configurazione", c, `<div role="alert" class="flash flash-errore">Telegram non è configurato: niente è stato accodato.</div>`)
	if bot.quante() != 0 {
		t.Fatal("richieste senza configurazione")
	}
	inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b))
	bot.rispondi = func(string) (int, string) { return 401, `{"ok":false,"description":"Unauthorized"}` }
	c = inviaETorna(t, b, "/settings/providers/prova-telegram", soloToken(b))
	contiene(t, "getMe", c, `<div role="alert" class="flash flash-errore">Il bot non risponde a getMe (telegram:http:401): niente è stato accodato.</div>`)
	bot.rispondi = nil
	c = inviaETorna(t, b, "/settings/providers/prova-telegram", soloToken(b))
	contiene(t, "accettata", c, `<div role="status" class="flash flash-info">Messaggio di prova accettato da Telegram: guarda se è arrivato nella chat del negozio.</div>`,
		`<p id="telegram-bot-username" class="sotto">Bot: <span class="forte">@negozio_prova_bot</span></p>`,
		`<span id="telegram-switch-state" class="badge">Spento</span>`, "<p>La coda è vuota.</p>")
	bot.rispondi = func(p string) (int, string) {
		if strings.HasSuffix(p, "/getMe") {
			return 200, `{"ok":true,"result":{"username":"negozio_prova_bot"}}`
		}
		return 400, `{"ok":false,"description":"Bad Request: chat not found"}`
	}
	c = inviaETorna(t, b, "/settings/providers/prova-telegram", soloToken(b))
	contiene(t, "rifiutata", c, `<div role="alert" class="flash flash-errore">Messaggio di prova non partito (telegram:http:400:Bad Request: chat not found): controlla il token e la chat e riprova.</div>`)
	if n := contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Error("prova in coda")
	}
}

// inventario: ADM-10, CLI-52
// «Notifiche Telegram ai clienti»: «Accendi» porta enabled=true, la pagina dice «Notifiche Telegram ai
// clienti accese.», «Accese» e, senza bot configurato, «Il bot non è configurato: niente viene letto
// finché non lo è.»; «Spegni» le spegne. L'ultimo errore del lettore (settings telegram_inbound.*) con la
// sua ora e, per un 409, cosa fa l'installazione; le chat sconosciute.
func TestNotificheClientiEErroreLettore(t *testing.T) {
	h, d, _ := facciaConTelegram(t, nil)
	b := titolareNeiProvider(t, h)
	c := inviaETorna(t, b, "/settings/providers/notifiche-clienti", soloToken(b, "enabled", "true"))
	contiene(t, "accese", c, `<div role="status" class="flash flash-info">Notifiche Telegram ai clienti accese.</div>`,
		`<span id="telegram-customer-state" class="badge badge-ok">Accese</span>`,
		"Il bot non è configurato: niente viene letto finché non lo è.",
		`<input type="hidden" name="enabled" value="false"><button id="telegram-customer-toggle" type="submit" class="pulsante-contorno">Spegni</button>`)
	c = inviaETorna(t, b, "/settings/providers/notifiche-clienti", soloToken(b, "enabled", "false"))
	contiene(t, "spente", c, `<div role="status" class="flash flash-info">Notifiche Telegram ai clienti spente.</div>`,
		`<span id="telegram-customer-state" class="badge">Spente</span>`)
	for k, v := range map[string]string{"telegram_inbound.last_error": "409: Conflict: can't use getUpdates method while webhook is active",
		"telegram_inbound.last_error_at": "2026-10-12T08:00:00Z", "telegram_inbound.unknown_chats": "2"} {
		eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-10-12T08:00:00.000000Z')`, k, v)
	}
	c = b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "errore", c, `<div id="telegram-inbound-error" class="avviso-attenzione">`,
		"Lettura dei messaggi non riuscita (12/10/2026 10:00): 409: Conflict: can&#39;t use getUpdates method while webhook is active",
		"Telegram rifiuta la lettura perché sullo stesso token è attivo un webhook o un&#39;altra installazione sta leggendo. L&#39;installazione non rimuove il webhook da sola e riprova con un&#39;attesa crescente: va tolto il webhook o fermato l&#39;altro lettore.",
		"Risposte fisse date a chat sconosciute: 2.")
}

// inventario: OPS-02, ADM-10
// L'invio Telegram (D-03, solo Go): non si accende senza configurazione; con dei messaggi Telegram in coda
// la pagina li elenca (D-27) e l'accensione dice di guardarli prima; «Scarta» e «Manda adesso» uno per
// uno con le frasi del Telegram; poi si accende e si spegne.
func TestAccensioneTelegram(t *testing.T) {
	h, d, bot := facciaConTelegram(t, nil)
	b := titolareNeiProvider(t, h)
	c := inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "1"))
	contiene(t, "senza configurazione", c, `<div role="alert" class="flash flash-errore">Telegram non è configurato: l&#39;invio Telegram non si accende.</div>`)
	inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b))
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il, formato)
		VALUES (11, 'telegram', 'shop', '-1001', '<b>Ciao</b> negozio', 'queued', 0, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z', 'HTML'),
		(12, 'telegram', 'shop', '-1001', 'Seconda', 'queued', 0, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z', NULL)`)
	c = inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "1"))
	contiene(t, "in sospeso", c, `<div role="alert" class="flash flash-errore">Prima di accendere guarda i messaggi Telegram rimasti in coda: mandali o scartali uno per uno.</div>`,
		`<h3>Messaggi Telegram rimasti in coda (2)</h3>`, `<ul id="telegram-pending" class="provider-coda">`, `<li id="telegram-pending-11">`,
		"<pre>Ciao negozio</pre>", `<button id="telegram-pending-send-11" type="submit">Manda adesso</button>`,
		`<button id="telegram-pending-discard-12" type="submit" class="pulsante-contorno" data-confirm="Scartare questo messaggio? Non partirà più.">Scarta</button>`)
	c = inviaETorna(t, b, "/settings/providers/sospese/12/scarta", soloToken(b))
	contiene(t, "scartato", c, `<div role="status" class="flash flash-info">Messaggio Telegram scartato: non partirà.</div>`)
	c = inviaETorna(t, b, "/settings/providers/sospese/11/manda", soloToken(b))
	contiene(t, "mandato", c, `<div role="status" class="flash flash-info">Messaggio Telegram accettato dal provider.</div>`)
	if bot.quante() != 1 {
		t.Errorf("richieste %d", bot.quante())
	}
	c = inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "1"))
	contiene(t, "acceso", c, `<div role="status" class="flash flash-info">Invio Telegram acceso: da adesso la coda manda i messaggi da sola.</div>`,
		`<span id="telegram-switch-state" class="badge badge-ok">Acceso</span>`,
		`<input type="hidden" name="acceso" value="0"><button id="telegram-switch-toggle" type="submit" class="pulsante-contorno">Spegni l'invio Telegram</button>`)
	nonContiene(t, "acceso", c, `id="telegram-pending"`)
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (13, 'telegram', 'shop', '-1001', 'Terza', 'queued', 0, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z')`)
	c = inviaETorna(t, b, "/settings/providers/sospese/13/scarta", soloToken(b))
	contiene(t, "da acceso", c, `<div role="alert" class="flash flash-errore">L&#39;invio Telegram è acceso: i messaggi in coda li manda la coda.</div>`)
	c = inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "0"))
	contiene(t, "spento", c, `<div role="status" class="flash flash-info">Invio Telegram spento: i messaggi restano in coda finché non lo riaccendi.</div>`)
}

// inventario: ADM-10, CLI-52
// Le rotte nuove sono solo del titolare e vogliono il token CSRF: il dipendente va a / con «Operazione
// riservata al titolare.», senza token 403; niente scritto, nessuna richiesta al bot.
func TestProviderTelegramSoloTitolareECSRF(t *testing.T) {
	h, d, bot := facciaConTelegram(t, nil)
	rotte := []string{"/settings/providers/telegram", "/settings/providers/prova-telegram", "/settings/providers/notifiche-clienti",
		"/settings/providers/accensione-telegram"}
	dip := dipendenteDentro(t, h)
	dip.vai("GET", "/", nil, nil)
	for _, p := range rotte {
		if r := dip.vai("POST", p, moduloTelegram(dip, "enabled", "true", "acceso", "1"), nil); !redirectA(r, "/") {
			t.Errorf("dipendente %s: %d %q", p, r.codice, r.location)
		}
	}
	b := titolareNeiProvider(t, h)
	for _, p := range rotte {
		v := moduloTelegram(b, "enabled", "true", "acceso", "1")
		v.Del("_csrf_token")
		if r := b.vai("POST", p, v, nil); r.codice != 403 {
			t.Errorf("senza token %s: %d", p, r.codice)
		}
	}
	if n := contaSQL(t, d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.%' OR chiave LIKE 'canale_%') + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM eventi_audit WHERE azione LIKE 'outgoing.%')`); n != 0 {
		t.Errorf("scritte %d righe", n)
	}
	if bot.quante() != 0 {
		t.Error("richieste al bot")
	}
}

// inventario: ADM-09, ADM-10, CLI-52
// Review della 071: ogni modulo POST della pagina dei provider porta il token CSRF, in tutti gli stati
// della pagina (vuota; con email e messaggi Telegram in coda da guardare; con i due invii accesi e le
// notifiche accese).
func TestProviderOgniModuloConCSRF(t *testing.T) {
	h, d, _ := facciaConTelegram(t, nil)
	b := titolareNeiProvider(t, h)
	reModulo := regexp.MustCompile(`(?s)<form\b[^>]*\bmethod="post"[^>]*>.*?</form>`)
	controlla := func(stato string, almeno int) {
		t.Helper()
		c := b.vai("GET", "/settings/providers", nil, nil).corpo
		moduli := reModulo.FindAllString(c, -1)
		if len(moduli) < almeno {
			t.Errorf("%s: %d moduli, almeno %d", stato, len(moduli), almeno)
		}
		for _, m := range moduli {
			if !strings.Contains(m, `<input name="_csrf_token" type="hidden" value="`+b.token+`">`) {
				t.Errorf("%s: modulo senza token: %.120s", stato, m)
			}
		}
	}
	controlla("vuota", 8)
	inserisciCoda(t, d)
	inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b))
	controlla("in sospeso", 12)
	eseguiSQL(t, d, `DELETE FROM messaggi_in_uscita WHERE stato = 'queued'`)
	postaProva(b, "smtp[security]", "none", "smtp[username]", "", "smtp[password]", "")
	inviaETorna(t, b, "/settings/providers/accensione", soloToken(b, "acceso", "1"))
	inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "1"))
	inviaETorna(t, b, "/settings/providers/notifiche-clienti", soloToken(b, "enabled", "true"))
	controlla("accesi", 8)
}

// inventario: ADM-10, OPS-02
// Review della 071, come la riga della posta (069c): con l'invio Telegram acceso e il bot non più
// configurato, #telegram-switch-unusable dice che nessun messaggio Telegram parte.
func TestTelegramAccesoMaNonConfigurato(t *testing.T) {
	h, d, _ := facciaConTelegram(t, nil)
	b := titolareNeiProvider(t, h)
	riga := `<p id="telegram-switch-unusable" class="flash flash-errore">L&#39;invio Telegram è acceso ma Telegram non è configurato: nessun messaggio Telegram parte finché non completi token e chat qui sopra.</p>`
	nonContiene(t, "spento", b.vai("GET", "/settings/providers", nil, nil).corpo, "telegram-switch-unusable")
	inviaETorna(t, b, "/settings/providers/telegram", moduloTelegram(b))
	c := inviaETorna(t, b, "/settings/providers/accensione-telegram", soloToken(b, "acceso", "1"))
	nonContiene(t, "configurato", c, "telegram-switch-unusable")
	eseguiSQL(t, d, `DELETE FROM segreti_provider WHERE nome = 'outgoing.telegram.bot_token'`)
	c = b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "non configurato", c, riga, `<span id="telegram-switch-state" class="badge badge-ok">Acceso</span>`,
		`<span id="telegram-state" class="badge">Non configurato</span>`)
	nonContiene(t, "non configurato", c, "smtp-switch-unusable")
}
