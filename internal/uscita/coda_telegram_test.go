package uscita

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Le regole della coda per Telegram (slice 071, CLI-51), come record/2, rate_limit_wait/2,
// refused_for_good?/2, blocked?/2 e dispatch_once/1 dello standalone a go-baseline
// (telegram_queue_limits_test.exs): la coda passa dal provider Telegram vero e ogni richiesta finisce al
// Bot API finto. L'orologio della coda è finto (inizio = 2026-11-11 09:00 UTC). Dati inventati.

// righeNegozio accoda n messaggi Telegram al negozio (come enqueue_test_message(:telegram)).
func righeNegozio(t *testing.T, c *Coda, n int) []int64 {
	t.Helper()
	var ids []int64
	for range n {
		id, err := c.Accoda(context.Background(), Destinazione{Canale: CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta},
			Contenuto{Testo: "GestRip — prova del provider Telegram."}, contesto)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// fra è l'istante della coda dopo d, nella forma del database.
func fra(d time.Duration) string { return inizio.Add(d).Format("2006-01-02T15:04:05.000000Z") }

func giro(t *testing.T, c *Coda) Esito {
	t.Helper()
	e, err := c.Giro(context.Background(), PerGiro)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// inventario: CLI-51
// Un 429 «Too Many Requests: retry after 37»: la riga prende il tentativo e aspetta 37 s (più della prima
// attesa della scala), le altre righe Telegram del giro non partono, non prendono tentativi e aspettano lo
// stesso momento; un secondo giro subito non manda niente.
func TestTelegram429RetryAfter(t *testing.T) {
	c, _, b := codaTelegram(t)
	ids := righeNegozio(t, c, 3)
	b.risposta(rifiutoBot(429, "Too Many Requests: retry after 37"))
	if e := giro(t, c); e != (Esito{Riprovati: 1}) {
		t.Errorf("esito %+v", e)
	}
	if b.quante() != 1 {
		t.Fatalf("richieste %d", b.quante())
	}
	if got := riga(t, c.d, ids[0]); got != "queued|1|"+fra(37*time.Second)+"|-|-|-|telegram:http:429:Too Many Requests: retry after 37" {
		t.Errorf("riga limitata %s", got)
	}
	for _, id := range ids[1:] {
		if got := riga(t, c.d, id); got != "queued|0|"+fra(37*time.Second)+"|-|-|-|-" {
			t.Errorf("riga trattenuta %d: %s", id, got)
		}
	}
	if e := giro(t, c); e != (Esito{}) || b.quante() != 1 {
		t.Errorf("secondo giro %+v, richieste %d", e, b.quante())
	}
}

// inventario: CLI-51
// L'attesa del 429 contro la scala: la riga aspetta la più lunga delle due (3 s → 10 s; al terzo tentativo
// 37 s → 5 min), le trattenute solo quella del 429. Oltre un giorno vale un giorno (anche un numero
// enorme). Senza numero si tiene la prima attesa della scala, e il giro si ferma lo stesso. Il quinto
// rifiuto è l'ultimo e trattiene le altre. (Lo stato intero: TestTelegramStatoIntero.)
func TestTelegram429Attese(t *testing.T) {
	casi := []struct {
		nome, descrizione     string
		stato, tentativiPrima int
		prima, seconda        string
	}{
		{"corta", "Too Many Requests: retry after 3", 429, 0, "queued|1|" + fra(10*time.Second), "queued|0|" + fra(3*time.Second)},
		{"scala più lunga", "Too Many Requests: retry after 37", 429, 2, "queued|3|" + fra(5*time.Minute), "queued|0|" + fra(37*time.Second)},
		{"un giorno", "Too Many Requests: retry after 300000000000", 429, 0, "queued|1|" + fra(24*time.Hour), "queued|0|" + fra(24*time.Hour)},
		{"un giorno da 6 cifre", "Too Many Requests: retry after 100000", 429, 0, "queued|1|" + fra(24*time.Hour), "queued|0|" + fra(24*time.Hour)},
		{"senza numero", "Too Many Requests", 429, 0, "queued|1|" + fra(10*time.Second), "queued|0|" + fra(10*time.Second)},
		{"quinto", "Too Many Requests: retry after 37", 429, 4, "failed|5|" + fra(0), "queued|0|" + fra(37*time.Second)},
	}
	for _, k := range casi {
		t.Run(k.nome, func(t *testing.T) {
			c, _, b := codaTelegram(t)
			ids := righeNegozio(t, c, 2)
			if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET tentativi = ? WHERE id = ?`, k.tentativiPrima, ids[0]); err != nil {
				t.Fatal(err)
			}
			b.risposta(rifiutoBot(k.stato, k.descrizione))
			giro(t, c)
			if got := riga(t, c.d, ids[0]); got[:len(k.prima)] != k.prima {
				t.Errorf("prima %s, attesa %s", got, k.prima)
			}
			if got := riga(t, c.d, ids[1]); got[:len(k.seconda)] != k.seconda {
				t.Errorf("seconda %s, attesa %s", got, k.seconda)
			}
		})
	}
}

// inventario: CLI-51
// Il 429 trattiene solo le righe Telegram: un'email dello stesso giro parte (il limite è del bot). Una
// riga trattenuta che nel frattempo è uscita dalla coda resta com'è. Un altro rifiuto non trattiene
// niente.
func TestTelegram429SoloTelegram(t *testing.T) {
	d, o := apriDB(t)
	b := nuovoBotFinto(t)
	impostaTelegram(t, d, tokenFinto, chatFinta, true)
	posta := &providerFinto{}
	c := Nuova(d, o, map[string]Provider{CanaleTelegram: NuovoTelegram(d, b.srv.URL), CanaleEmail: posta})
	primo := righeNegozio(t, c, 1)[0]
	dest, cont := email("cliente@esempio.it", "Ciao")
	mail, err := c.Accoda(context.Background(), dest, cont, contesto)
	if err != nil {
		t.Fatal(err)
	}
	dopo := righeNegozio(t, c, 2)
	b.risposta(func(r richiestaBot) (int, string) {
		// La terza riga esce dalla coda mentre il giro lavora sulla prima.
		if _, err := d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?1 WHERE id = ?2`, fra(0), dopo[1]); err != nil {
			t.Error(err)
		}
		return rifiutoBot(429, "Too Many Requests: retry after 37")(r)
	})
	if e := giro(t, c); e != (Esito{Accettati: 1, Riprovati: 1}) {
		t.Errorf("esito %+v", e)
	}
	if b.quante() != 1 || posta.quante() != 1 {
		t.Errorf("richieste %d, email %d", b.quante(), posta.quante())
	}
	if got := riga(t, d, mail); got[:8] != "accepted" {
		t.Errorf("email %s", got)
	}
	if got := riga(t, d, dopo[0]); got != "queued|0|"+fra(37*time.Second)+"|-|-|-|-" {
		t.Errorf("trattenuta %s", got)
	}
	if got := riga(t, d, dopo[1]); got != "failed|0|"+fra(0)+"|-|"+fra(0)+"|-|-" {
		t.Errorf("uscita dalla coda %s", got)
	}
	_ = primo
	// Un 500 non trattiene.
	c2, _, b2 := codaTelegram(t)
	ids := righeNegozio(t, c2, 2)
	b2.risposta(rifiutoBot(500, "Internal Server Error"))
	if e := giro(t, c2); e != (Esito{Riprovati: 2}) || b2.quante() != 2 {
		t.Errorf("500: %+v, richieste %d", e, b2.quante())
	}
	for _, id := range ids {
		if got := riga(t, c2.d, id); got != "queued|1|"+fra(10*time.Second)+"|-|-|-|telegram:http:500:Internal Server Error" {
			t.Errorf("500 %s", got)
		}
	}
}

// inventario: CLI-51
// Un 400 (un testo che Telegram non legge, una chat che non c'è) fa fallire la riga al primo rifiuto, del
// negozio come del cliente, anche senza descrizione; non trattiene il giro e non spegne niente.
func TestTelegram400(t *testing.T) {
	c, _, b := codaTelegram(t)
	ids := righeNegozio(t, c, 2)
	b.risposta(rifiutoBot(400, "Bad Request: chat not found"))
	if e := giro(t, c); e != (Esito{Falliti: 2}) || b.quante() != 2 {
		t.Errorf("esito %+v, richieste %d", e, b.quante())
	}
	if got := riga(t, c.d, ids[0]); got != "failed|1|"+fra(0)+"|-|"+fra(0)+"|-|telegram:http:400:Bad Request: chat not found" {
		t.Errorf("riga %s", got)
	}
	ids = righeNegozio(t, c, 1)
	b.risposta(func(richiestaBot) (int, string) { return 400, "" })
	giro(t, c)
	if got := riga(t, c.d, ids[0]); got != "failed|1|"+fra(0)+"|-|"+fra(0)+"|-|telegram:http:400" {
		t.Errorf("senza descrizione %s", got)
	}
}

// inventario: CLI-51
// Le regole guardano lo stato intero, non un numero più lungo (i casi «4291» e «4001» di
// telegram_queue_limits_test.exs): un provider che dichiara «telegram:http:4291:…» o «telegram:http:4001:…»
// (un server HTTP non li può mandare, quindi qui un provider finto) è un rifiuto come gli altri, che non
// trattiene il giro e non fa fallire. Le stesse parole su una riga email non sono regole di Telegram.
func TestTelegramStatoIntero(t *testing.T) {
	d, o := apriDB(t)
	for _, motivo := range []string{"telegram:http:4291:Too Many Requests: retry after 37", "telegram:http:4001:Not a status",
		"telegram:http:4290", "telegram:http:4003"} {
		p := &providerFinto{rispondi: func(Messaggio) (string, error) { return "", Rifiuto(motivo) }}
		c := Nuova(d, o, map[string]Provider{CanaleTelegram: p})
		ids := righeNegozio(t, c, 2)
		if e := giro(t, c); e != (Esito{Riprovati: 2}) || p.quante() != 2 {
			t.Errorf("%s: %+v, richieste %d", motivo, e, p.quante())
		}
		for _, id := range ids {
			if got := riga(t, d, id); got != "queued|1|"+fra(10*time.Second)+"|-|-|-|"+motivo {
				t.Errorf("%s: %s", motivo, got)
			}
		}
		if _, err := d.Scrittura.Exec(`DELETE FROM messaggi_in_uscita`); err != nil {
			t.Fatal(err)
		}
	}
	// Su un'email «telegram:http:400» e «…429…» non sono regole.
	for _, motivo := range []string{"telegram:http:400:x", "telegram:http:429:Too Many Requests: retry after 37"} {
		p := &providerFinto{rispondi: func(Messaggio) (string, error) { return "", Rifiuto(motivo) }}
		c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
		var ids []int64
		for range 2 {
			dest, cont := email("cliente@esempio.it", "Ciao")
			id, err := c.Accoda(context.Background(), dest, cont, contesto)
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if e := giro(t, c); e != (Esito{Riprovati: 2}) {
			t.Errorf("email %s: %+v", motivo, e)
		}
		for _, id := range ids {
			if got := riga(t, d, id); got != "queued|1|"+fra(10*time.Second)+"|-|-|-|"+motivo {
				t.Errorf("email %s: %s", motivo, got)
			}
		}
		if _, err := d.Scrittura.Exec(`DELETE FROM messaggi_in_uscita`); err != nil {
			t.Fatal(err)
		}
	}
}

// iscrizione scrive un'iscrizione Telegram attiva (come TelegramSubscriptions.start + consent) della busta
// alla chat.
func iscrizione(t *testing.T, c *Coda, busta int64, chat int64) int64 {
	t.Helper()
	res, err := c.d.Scrittura.Exec(`INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, consenso_il, creato_il, aggiornato_il)
		VALUES (?1, ?2, 1, ?3, ?3, ?3, ?3)`, busta, chat, fra(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func bustaProva(t *testing.T, c *Coda, numero string) int64 {
	t.Helper()
	if _, err := c.d.Scrittura.Exec(`INSERT OR IGNORE INTO clienti (id, nome, telefono, creato_il, aggiornato_il) VALUES (1, 'Cliente Prova', '3330000000', ?1, ?1)`, fra(0)); err != nil {
		t.Fatal(err)
	}
	res, err := c.d.Scrittura.Exec(`INSERT INTO buste (cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione, descrizione,
		hash_token_pubblico, token_pubblico_scade_il, creato_il, aggiornato_il)
		VALUES (1, 'Cliente Prova', '3330000000', ?1, '2026-11-01', 'Anello di prova', randomblob(32), ?2, ?2, ?2)`, numero, fra(0))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// inventario: CLI-51
// Un 403 sulla riga di un cliente (ha bloccato il bot) la fa fallire subito e spegne tutte le iscrizioni
// ancora attive di quella chat (consenso 0, fermata, motivo «blocked»), con repair.telegram_stop (via
// «blocked», attore «telegram») per ognuna; le iscrizioni di altre chat restano. Sulla riga del negozio un
// 403 è un rifiuto come un altro: riprova.
func TestTelegram403(t *testing.T) {
	c, _, b := codaTelegram(t)
	impostazione(t, c.d, ChiaveNotificheClienti, "1")
	b1, b2 := bustaProva(t, c, "12/26"), bustaProva(t, c, "13/26")
	s1, s2 := iscrizione(t, c, b1, 4242), iscrizione(t, c, b2, 4242)
	altra := iscrizione(t, c, b1, 5151)
	// Un'iscrizione della stessa chat già fermata (con /stop, un'ora fa) resta com'era, senza audit.
	fermata := iscrizione(t, c, bustaProva(t, c, "14/26"), 4242)
	if _, err := c.d.Scrittura.Exec(`UPDATE iscrizioni_telegram SET consenso = 0, fermata_il = ?1, motivo_fermata = 'stop', aggiornato_il = ?1
		WHERE id = ?2`, fra(-time.Hour), fermata); err != nil {
		t.Fatal(err)
	}
	id, err := c.Accoda(context.Background(), Destinazione{BustaID: &b1, Canale: CanaleTelegram, Destinatario: "customer", Indirizzo: "4242"},
		Contenuto{Testo: "Ciao", Formato: FormatoHTML}, contesto)
	if err != nil {
		t.Fatal(err)
	}
	b.risposta(rifiutoBot(403, "Forbidden: bot was blocked by the user"))
	if e := giro(t, c); e != (Esito{Falliti: 1}) {
		t.Errorf("esito %+v", e)
	}
	if got := riga(t, c.d, id); got != "failed|1|"+fra(0)+"|-|"+fra(0)+"|-|telegram:http:403:Forbidden: bot was blocked by the user" {
		t.Errorf("riga %s", got)
	}
	for _, s := range []int64{s1, s2} {
		if n := conta(t, c.d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND consenso = 0 AND fermata_il = ? AND motivo_fermata = 'blocked'
			AND aggiornato_il = ?`, s, fra(0), fra(0)); n != 1 {
			t.Errorf("iscrizione %d non spenta", s)
		}
	}
	if n := conta(t, c.d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND consenso = 1 AND fermata_il IS NULL`, altra); n != 1 {
		t.Error("spenta l'iscrizione di un'altra chat")
	}
	if n := conta(t, c.d, `SELECT count(*) FROM iscrizioni_telegram WHERE id = ? AND fermata_il = ? AND motivo_fermata = 'stop' AND aggiornato_il = ?`,
		fermata, fra(-time.Hour), fra(-time.Hour)); n != 1 {
		t.Error("toccata l'iscrizione già fermata")
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop' AND json_extract(dati, '$.subscription_id') = ?`, fermata); n != 0 {
		t.Error("audit per l'iscrizione già fermata")
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.telegram_stop' AND attore = 'telegram' AND risorsa = 'repair'
		AND ((id_risorsa = ?1 AND dati = '{"bag_number":"12/26","subscription_id":'||?3||',"via":"blocked"}')
		  OR (id_risorsa = ?2 AND dati = '{"bag_number":"13/26","subscription_id":'||?4||',"via":"blocked"}'))`, b1, b2, s1, s2); n != 2 {
		t.Errorf("audit dello spegnimento: %d righe", n)
	}
	ids := righeNegozio(t, c, 1)
	b.risposta(rifiutoBot(403, "Forbidden: bot was kicked from the group chat"))
	giro(t, c)
	if got := riga(t, c.d, ids[0]); got[:8] != "queued|1" {
		t.Errorf("negozio %s", got)
	}
}

// inventario: CLI-51
// «Notifiche Telegram ai clienti» spente fermano anche quello che è già in coda per un cliente: la riga
// fallisce al primo tentativo con «telegram:notifiche ai clienti spente», senza richiesta (slice 137);
// la riga del negozio parte.
func TestTelegramClientiSpentiFallisce(t *testing.T) {
	c, _, b := codaTelegram(t)
	cliente, err := c.Accoda(context.Background(), Destinazione{Canale: CanaleTelegram, Destinatario: "customer", Indirizzo: "4242"},
		Contenuto{Testo: "Ciao"}, contesto)
	if err != nil {
		t.Fatal(err)
	}
	negozio := righeNegozio(t, c, 1)[0]
	if e := giro(t, c); e != (Esito{Accettati: 1, Falliti: 1}) || b.quante() != 1 {
		t.Errorf("esito %+v, richieste %d", e, b.quante())
	}
	if got := riga(t, c.d, cliente); got != "failed|1|"+fra(0)+"|-|"+fra(0)+"|-|telegram:notifiche ai clienti spente" {
		t.Errorf("cliente %s", got)
	}
	if got := riga(t, c.d, negozio); got[:8] != "accepted" {
		t.Errorf("negozio %s", got)
	}
}

// inventario: CLI-51
// Formato e tastiera (slice 134 dello standalone): Accoda li scrive sulla riga (formato «HTML», tastiera
// come {"inline_keyboard":[[{"text":…,"url":…}]]}) e il provider li manda; solo per Telegram, e il solo
// formato è HTML (Message.changeset/2): altrimenti messaggio non valido, niente scritto. Una riga importata
// con formato e tastiera parte con loro, una senza come testo semplice.
func TestTelegramFormatoETastiera(t *testing.T) {
	c, _, b := codaTelegram(t)
	ctx := context.Background()
	dest := Destinazione{Canale: CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta}
	id, err := c.Accoda(ctx, dest, Contenuto{Testo: "<b>Pronta</b> & bella", Formato: FormatoHTML,
		Tastiera: &PulsanteURL{Testo: "📂 Apri scheda riparazione", URL: "https://negozio.example.invalid/r/TOKEN?a=1&b=<2>"}}, contesto)
	if err != nil {
		t.Fatal(err)
	}
	tastiera := `{"inline_keyboard":[[{"text":"📂 Apri scheda riparazione","url":"https://negozio.example.invalid/r/TOKEN?a=1&b=<2>"}]]}`
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ? AND formato = 'HTML' AND tastiera = ?`, id, tastiera); n != 1 {
		var f, k any
		_ = c.d.Lettura.QueryRow(`SELECT formato, tastiera FROM messaggi_in_uscita WHERE id = ?`, id).Scan(&f, &k)
		t.Errorf("riga: formato %v tastiera %v", f, k)
	}
	oggetto := "Oggetto"
	rifiutati := []struct {
		dest Destinazione
		cont Contenuto
	}{
		{Destinazione{Canale: CanaleEmail, Destinatario: "shop", Indirizzo: "negozio@example.invalid"}, Contenuto{Oggetto: &oggetto, Testo: "x", Formato: FormatoHTML}},
		{Destinazione{Canale: CanaleEmail, Destinatario: "shop", Indirizzo: "negozio@example.invalid"}, Contenuto{Testo: "x", Tastiera: &PulsanteURL{Testo: "a", URL: "https://x.invalid"}}},
		{dest, Contenuto{Testo: "x", Formato: "Markdown"}},
		{dest, Contenuto{Testo: "x", Formato: "html"}},
		{dest, Contenuto{Testo: "x", Tastiera: &PulsanteURL{Testo: "", URL: "https://x.invalid"}}},
		{dest, Contenuto{Testo: "x", Tastiera: &PulsanteURL{Testo: "a", URL: ""}}},
	}
	c2 := Nuova(c.d, c.o, map[string]Provider{CanaleTelegram: c.provider[CanaleTelegram], CanaleEmail: &providerFinto{}})
	prima := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`)
	for i, r := range rifiutati {
		if _, err := c2.Accoda(ctx, r.dest, r.cont, contesto); !errors.Is(err, ErrMessaggioNonValido) {
			t.Errorf("%d: %v", i, err)
		}
	}
	if conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`) != prima {
		t.Error("scritto un rifiuto")
	}
	// Una riga importata con formato e tastiera, una senza.
	if _, err := c.d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il, formato, tastiera) VALUES (900, 'telegram', 'shop', ?1, 'Importata', 'queued', 2, ?2, ?2, 'HTML',
		'{"inline_keyboard":[[{"text":"Apri","url":"https://negozio.example.invalid/r/X"}]]}'),
		(901, 'telegram', 'shop', ?1, 'Semplice', 'queued', 0, ?2, ?2, NULL, NULL)`, chatFinta, fra(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e := giro(t, c); e.Accettati != 3 {
		t.Errorf("esito %+v", e)
	}
	corpi := map[string]string{}
	for _, r := range b.tutte() {
		corpi[r.Corpo["text"].(string)] = r.Grezzo
	}
	if got := corpi["<b>Pronta</b> & bella"]; got != `{"chat_id":"`+chatFinta+`","text":"<b>Pronta</b> & bella","parse_mode":"HTML","reply_markup":`+tastiera+`}` {
		t.Errorf("accodata %s", got)
	}
	if got := corpi["Importata"]; got != `{"chat_id":"`+chatFinta+`","text":"Importata","parse_mode":"HTML","reply_markup":{"inline_keyboard":[[{"text":"Apri","url":"https://negozio.example.invalid/r/X"}]]}}` {
		t.Errorf("importata %s", got)
	}
	if got := corpi["Semplice"]; got != `{"chat_id":"`+chatFinta+`","text":"Semplice"}` {
		t.Errorf("semplice %s", got)
	}
	if got := riga(t, c.d, 900); got != "accepted|3|"+fra(-time.Hour)+"|"+fra(0)+"|-|77|-" {
		t.Errorf("importata %s", got)
	}
}

// inventario: CLI-51, OPS-02
// D-03 per Telegram (solo Go): con l'invio Telegram spento Accoda rifiuta (non configurato) e il giro non
// legge le righe Telegram, anche quelle importate: restano com'erano. Acceso, partono.
func TestTelegramSpentoLeRigheAspettano(t *testing.T) {
	c, _, b := codaTelegram(t)
	impostaTelegram(t, c.d, tokenFinto, chatFinta, false)
	if _, err := c.Accoda(context.Background(), Destinazione{Canale: CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta},
		Contenuto{Testo: "x"}, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("accoda da spento: %v", err)
	}
	if _, err := c.d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il) VALUES (900, 'telegram', 'shop', ?1, 'Importata', 'queued', 1, ?2, ?2)`, chatFinta, fra(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if e := giro(t, c); e != (Esito{}) || b.quante() != 0 {
		t.Errorf("da spento %+v, richieste %d", e, b.quante())
	}
	if got := riga(t, c.d, 900); got != "queued|1|"+fra(-time.Hour)+"|-|-|-|-" {
		t.Errorf("riga %s", got)
	}
	impostaTelegram(t, c.d, tokenFinto, chatFinta, true)
	if e := giro(t, c); e != (Esito{Accettati: 1}) {
		t.Errorf("acceso %+v", e)
	}
}
