package uscita

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/db"
)

// Le risposte del cliente a una notifica (CLI-49), l'offset vecchio (D-85), la rilettura un aggiornamento
// alla volta sopra gli 8 MiB e la pulizia oraria delle iscrizioni (CLI-50) della slice 075, come
// telegram_inbound_reply_test.exs, telegram_poller_test.exs e telegram_subscriptions_test.exs («purge/1»)
// dello standalone a go-baseline. Bot API finto in processo, iscrizioni finte (quelle vere:
// internal/buste/telegram_risposte_test.go). Chat inventate sopra 2^52.

const (
	// notificaA è il message_id della notifica a chatA che il cliente risponde.
	notificaA      int64 = 501
	bustaA         int64 = 12
	testoOK              = "✅ Messaggio inviato al negozio."
	testoOra             = "⏳ Hai già inviato molti messaggi nell'ultima ora: riprova più tardi."
	testoMinuto          = "⏳ Hai inviato troppi messaggi: attendi qualche minuto e riprova."
	testoSoloTesti       = "❌ Solo risposte di testo per ora."
)

// offsetSalvato scrive l'offset del lettore com'era stato scritto all'istante il.
func offsetSalvato(t *testing.T, d *db.DB, valore, il string) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, chiaveOffset, valore, il); err != nil {
		t.Fatal(err)
	}
}

// rispostaA è il messaggio del cliente che risponde con «Rispondi» al messaggio del bot risponde; campi
// sostituisce o aggiunge campi del messaggio (text compreso).
func rispostaA(id, chat, risponde int64, campi map[string]any) map[string]any {
	m := map[string]any{
		"message_id": 9000 + id,
		"from":       map[string]any{"id": chat, "is_bot": false, "first_name": "Esempio"},
		"chat":       map[string]any{"id": chat, "type": "private", "first_name": "Esempio"},
		"reply_to_message": map[string]any{
			"message_id": risponde,
			"from":       map[string]any{"id": 123456, "is_bot": true, "first_name": "Bottega"},
			"chat":       map[string]any{"id": chat, "type": "private"},
			"text":       "Ciao",
		},
	}
	for k, v := range campi {
		m[k] = v
	}
	return map[string]any{"update_id": id, "message": m}
}

func rispostaTesto(id, chat int64, testo string) map[string]any {
	return rispostaA(id, chat, notificaA, map[string]any{"text": testo})
}

// conNotifica è la prova del lettore con la notifica notificaA di chatA mappata a bustaA.
func conNotifica(t *testing.T) *provaLettore {
	t.Helper()
	p := nuovaProvaLettore(t)
	p.isc.notifiche[[2]int64{chatA, notificaA}] = bustaA
	return p
}

func testi(r []map[string]any) []string {
	var out []string
	for _, m := range r {
		out = append(out, fmt.Sprint(m["text"]))
	}
	return out
}

// inventario: CLI-49
// Il cliente risponde con «Rispondi» a una notifica: il testo (com'è: lo ripulisce la conversazione) va
// alla busta della notifica, l'offset si scrive nella transazione della riga (dopo = id + 1, prima che il
// lettore lo salvi per conto suo) e il bot conferma citando il messaggio (reply_parameters con
// allow_sending_without_reply), senza parse_mode né tastiera.
func TestLettoreRispostaAUnaNotifica(t *testing.T) {
	p := conNotifica(t)
	var offsetAllaConferma string
	p.b.risposta(func(r richiestaBot) (int, string) {
		if strings.HasSuffix(r.Percorso, "/getUpdates") {
			j, _ := json.Marshal(map[string]any{"ok": true, "result": []any{rispostaTesto(41, chatA, "  Va bene, passo sabato  ")}})
			return 200, string(j)
		}
		offsetAllaConferma = imp(t, p.d, chiaveOffset)
		return 200, `{"ok":true,"result":{"message_id":78}}`
	})
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
		t.Fatalf("esito %+v", e)
	}
	if fmt.Sprint(p.isc.cercate) != fmt.Sprintf("[%d %d]", chatA, notificaA) || fmt.Sprint(p.isc.scritte) != "[12|  Va bene, passo sabato  ]" {
		t.Errorf("cercate %v, scritte %v", p.isc.cercate, p.isc.scritte)
	}
	if offsetAllaConferma != "42" || imp(t, p.d, chiaveOffset) != "42" {
		t.Errorf("offset alla conferma %s, dopo %s", offsetAllaConferma, imp(t, p.d, chiaveOffset))
	}
	c := p.chiamate(t)
	if len(c) != 1 || c[0].Metodo != "sendMessage" {
		t.Fatalf("chiamate %+v", c)
	}
	if c[0].Grezzo != fmt.Sprintf(`{"chat_id":%d,"text":"%s","reply_parameters":{"message_id":9041,"allow_sending_without_reply":true}}`, chatA, testoOK) {
		t.Errorf("conferma %s", c[0].Grezzo)
	}
	if strings.Contains(p.log.String(), strconv.FormatInt(chatA, 10)) || strings.Contains(p.log.String(), "sabato") {
		t.Errorf("chat o testo nel registro: %s", p.log.String())
	}
}

// inventario: CLI-49
// I rifiuti della conversazione con le parole dello standalone (TelegramReplies.answer/2): vuoto, troppo
// lungo con il conto in code point del testo ripulito, dieci all'ora per busta, busta chiusa, busta non
// trovata (conto terzi, cestino). Un rifiuto non scrive l'offset nella transazione: lo salva il lettore dopo,
// come per ogni aggiornamento. Un altro errore fa fallire l'aggiornamento, che si riprova.
func TestLettoreRispostaRifiuti(t *testing.T) {
	lungo := " e" + strings.Repeat("́", 1000) + "\n"
	casi := []struct {
		err   error
		testo string
		detto string
	}{
		{ErrRispostaVuota, "x", "❌ Testo vuoto."},
		{ErrRispostaTroppoLunga, lungo, "❌ Risposta troppo lunga (1001/1000), accorcia."},
		{ErrRispostaTroppoLunga, strings.Repeat("é", 1200), "❌ Risposta troppo lunga (1200/1000), accorcia."},
		{ErrRispostaTroppe, "x", testoOra},
		{ErrRispostaChiusa, "x", "Questa riparazione non riceve più messaggi."},
		{ErrRispostaNonTrovata, "x", "❌ Riparazione non trovata."},
	}
	for _, k := range casi {
		p := conNotifica(t)
		p.isc.errRisposta = k.err
		p.arrivano(rispostaTesto(7, chatA, k.testo))
		if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) {
			t.Errorf("%v: esito %+v", k.err, e)
		}
		r := p.risposte(t)
		citato := map[string]any{}
		if len(r) == 1 {
			citato, _ = r[0]["reply_parameters"].(map[string]any)
		}
		if fmt.Sprint(testi(r)) != "["+k.detto+"]" || citato["message_id"] != json.Number("9007") {
			t.Errorf("%v: risposte %v", k.err, r)
		}
		if imp(t, p.d, chiaveOffset) != "8" {
			t.Errorf("%v: offset %s", k.err, imp(t, p.d, chiaveOffset))
		}
	}
	p := conNotifica(t)
	p.isc.errRisposta = errors.New("database chiuso")
	p.arrivano(rispostaTesto(7, chatA, "ciao"))
	if e := p.leggi(t); e.Errore != "telegram:update 7 non gestito" || len(p.risposte(t)) != 0 || imp(t, p.d, chiaveOffset) != "<assente>" {
		t.Errorf("errore: %+v, offset %s", e, imp(t, p.d, chiaveOffset))
	}
	p = conNotifica(t)
	p.isc.errCerca = errors.New("database chiuso")
	p.arrivano(rispostaTesto(7, chatA, "ciao"))
	if e := p.leggi(t); e.Errore != "telegram:update 7 non gestito" || len(p.risposte(t)) != 0 {
		t.Errorf("ricerca che fallisce: %+v", e)
	}
}

// inventario: CLI-49
// Solo testo: una foto, un testo di soli spazi (anche Unicode) o un testo che non è una stringa non vanno
// nella conversazione e non si contano negli otto al minuto; il bot dice «solo risposte di testo».
func TestLettoreRispostaSoloTesto(t *testing.T) {
	p := conNotifica(t)
	var agg []any
	agg = append(agg, rispostaA(1, chatA, notificaA, map[string]any{"photo": []any{map[string]any{"file_id": "esempio"}}}))
	for n := int64(2); n <= 9; n++ {
		agg = append(agg, rispostaTesto(n, chatA, "   \n "))
	}
	agg = append(agg, rispostaA(10, chatA, notificaA, map[string]any{"text": 42}))
	agg = append(agg, rispostaTesto(11, chatA, "Ecco"))
	p.arrivano(agg...)
	if e := p.leggi(t); e.Gestiti != 11 {
		t.Fatalf("esito %+v", e)
	}
	if fmt.Sprint(p.isc.scritte) != "[12|Ecco]" {
		t.Errorf("scritte %v", p.isc.scritte)
	}
	// Le conferme sono risposte del bot: otto al minuto per chat (reply/4), poi tacciono.
	r := testi(p.risposte(t))
	if len(r) != 8 || r[0] != testoSoloTesti || r[7] != testoSoloTesti {
		t.Errorf("risposte %q", r)
	}
}

// inventario: CLI-49
// Otto messaggi al minuto per chat (ReplyLimit :incoming): il nono non va nella conversazione e il bot
// dice «troppi messaggi»; dopo un minuto si riprende. Il conto è per chat.
func TestLettoreRispostaOttoAlMinuto(t *testing.T) {
	p := conNotifica(t)
	p.isc.notifiche[[2]int64{chatB, notificaA}] = 13
	adesso := p.o.Adesso().Unix()
	for range 8 {
		p.l.limiti.conta(chatA, rispostaIngresso, adesso)
	}
	p.arrivano(rispostaTesto(1, chatA, "Il nono"), rispostaTesto(2, chatB, "Altra chat"))
	p.leggi(t)
	if fmt.Sprint(p.isc.scritte) != "[13|Altra chat]" || fmt.Sprint(testi(p.risposte(t))) != "["+testoMinuto+" "+testoOK+"]" {
		t.Errorf("scritte %v, risposte %v", p.isc.scritte, testi(p.risposte(t)))
	}
	p.o.Sposta(59 * time.Second)
	p.arrivano(rispostaTesto(3, chatA, "Ancora"))
	p.leggi(t)
	if len(p.isc.scritte) != 1 {
		t.Errorf("dopo 59 s: %v", p.isc.scritte)
	}
	p.o.Sposta(time.Second)
	p.arrivano(rispostaTesto(4, chatA, "Dopo un minuto"))
	p.leggi(t)
	if fmt.Sprint(p.isc.scritte) != "[13|Altra chat 12|Dopo un minuto]" {
		t.Errorf("dopo un minuto: %v", p.isc.scritte)
	}
	// Nove di fila: otto scritte, il nono rifiutato (anche il rifiuto conta).
	q := conNotifica(t)
	var agg []any
	for n := int64(1); n <= 9; n++ {
		agg = append(agg, rispostaTesto(n, chatA, "Messaggio "+strconv.FormatInt(n, 10)))
	}
	q.arrivano(agg...)
	q.leggi(t)
	if len(q.isc.scritte) != 8 || q.isc.scritte[7] != "12|Messaggio 8" {
		t.Errorf("nove di fila: %v", q.isc.scritte)
	}
}

// inventario: CLI-49
// Un comando del bot che risponde a una notifica è il comando (command?/1: /start, /stop, /disattiva,
// /privacy, con @bot, maiuscole, spazi Unicode davanti o dopo); ogni altro testo, anche se comincia con «/»,
// è il messaggio del cliente.
func TestLettoreRispostaComandoOMessaggio(t *testing.T) {
	comandi := []string{"/stop", "  /STOP@negozio_bot", "/disattiva grazie", "/privacy", "/Start", " /stop subito", "/stop\n", "/ſtop"}
	for i, c := range comandi {
		p := conNotifica(t)
		p.arrivano(rispostaTesto(int64(i+1), chatA, c))
		p.leggi(t)
		if len(p.isc.cercate) != 0 || len(p.isc.scritte) != 0 {
			t.Errorf("%q: cercate %v, scritte %v", c, p.isc.cercate, p.isc.scritte)
		}
	}
	messaggi := []string{"/10 va bene", "/stopper", "va bene /stop", "/stop@", "/privacyx"}
	for i, m := range messaggi {
		p := conNotifica(t)
		p.arrivano(rispostaTesto(int64(i+1), chatA, m))
		p.leggi(t)
		if fmt.Sprint(p.isc.scritte) != "[12|"+m+"]" {
			t.Errorf("%q: scritte %v", m, p.isc.scritte)
		}
	}
}

// inventario: CLI-49
// Fuori dal flusso come gli altri messaggi: la chat del negozio, un gruppo, un altro bot. Un
// reply_to_message senza message_id intero non è la risposta a una notifica.
func TestLettoreRispostaFuoriDalFlusso(t *testing.T) {
	p := conNotifica(t)
	negozio, _ := strconv.ParseInt(chatFinta, 10, 64)
	p.isc.notifiche[[2]int64{negozio, notificaA}] = bustaA
	gruppo := rispostaTesto(2, chatA, "Nel gruppo")
	sotto(gruppo, "message", "chat")["type"] = "group"
	bot := rispostaTesto(3, chatA, "Da un bot")
	sotto(bot, "message", "from")["is_bot"] = true
	strano := rispostaA(4, chatA, 0, map[string]any{"text": "Ciao"})
	sotto(strano, "message", "reply_to_message")["message_id"] = "501"
	p.arrivano(rispostaTesto(1, negozio, "Dal negozio"), gruppo, bot, strano)
	if e := p.leggi(t); e.Gestiti != 4 {
		t.Fatalf("esito %+v", e)
	}
	if len(p.isc.cercate) != 0 || len(p.isc.scritte) != 0 || fmt.Sprint(testi(p.risposte(t))) != "["+testoFisso+"]" {
		t.Errorf("cercate %v, scritte %v, risposte %v", p.isc.cercate, p.isc.scritte, testi(p.risposte(t)))
	}
}

// inventario: CLI-46
// D-85 (Dubbio 1 della 073): un offset la cui riga non è stata scritta da più di sette giorni si dimentica
// (dopo una settimana senza aggiornamenti Telegram può ricominciare la numerazione più in basso): la lettura
// parte senza offset e non salta gli aggiornamenti «dietro». A sette giorni esatti l'offset vale ancora.
func TestLettoreOffsetVecchio(t *testing.T) {
	p := nuovaProvaLettore(t)
	settimana := 7 * 24 * time.Hour
	offsetSalvato(t, p.d, "900", fra(-settimana))
	p.arrivano()
	p.leggi(t)
	if r := p.b.tutte(); len(r) != 1 || r[0].Grezzo != `{"allowed_updates":["message","callback_query"],"offset":900,"timeout":25}` {
		t.Errorf("a sette giorni: %+v", r)
	}
	offsetSalvato(t, p.d, "900", fra(-settimana-time.Microsecond))
	p.arrivano(messaggio(12, chatA, "ciao"))
	if e := p.leggi(t); e.Gestiti != 1 {
		t.Errorf("esito %+v", e)
	}
	r := p.b.tutte()
	if len(r) < 1 || r[0].Grezzo != `{"allowed_updates":["message","callback_query"],"timeout":25}` {
		t.Errorf("oltre sette giorni: %+v", r)
	}
	if imp(t, p.d, chiaveOffset) != "13" || valoreDB(t, p.d, `SELECT aggiornata_il FROM impostazioni WHERE chiave = ?`, chiaveOffset) != fra(0) {
		t.Errorf("offset dopo %s", imp(t, p.d, chiaveOffset))
	}
	// Un offset che non è un intero (ma non vuoto) vale come nessun offset (integer/1 dà nil).
	offsetSalvato(t, p.d, "12a", fra(0))
	p.arrivano()
	p.leggi(t)
	if r := p.b.tutte(); len(r) != 1 || r[0].Grezzo != `{"allowed_updates":["message","callback_query"],"timeout":25}` {
		t.Errorf("offset non intero: %+v", r)
	}
	// Un istante che non si legge non decide niente: l'offset resta.
	offsetSalvato(t, p.d, "900", "ieri")
	p.arrivano()
	p.leggi(t)
	if r := p.b.tutte(); len(r) != 1 || !strings.Contains(r[0].Grezzo, `"offset":900`) {
		t.Errorf("istante illeggibile: %+v", r)
	}
}

func valoreDB(t *testing.T, d *db.DB, q string, args ...any) string {
	t.Helper()
	var v string
	if err := d.Lettura.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// inventario: CLI-46
// Dubbio 7 della 073: una risposta di getUpdates oltre gli 8 MiB si rilegge subito con limit 1 (un
// aggiornamento alla volta) e si va avanti così finché una lettura torna vuota; poi di nuovo senza limite.
// Se anche un aggiornamento solo è troppo grande, la lettura è un errore come prima.
func TestLettoreRispostaTroppoGrande(t *testing.T) {
	p := nuovaProvaLettore(t)
	// Due aggiornamenti da 5 MiB: insieme oltre gli 8 MiB, uno per volta no.
	grande := strings.Repeat("x", 5<<20)
	coda := []any{messaggio(1, chatA, grande), messaggio(2, chatB, grande)}
	p.b.risposta(func(r richiestaBot) (int, string) {
		if !strings.HasSuffix(r.Percorso, "/getUpdates") {
			return 200, `{"ok":true,"result":{"message_id":1}}`
		}
		var c struct {
			Offset int64 `json:"offset"`
			Limit  int   `json:"limit"`
		}
		_ = json.Unmarshal([]byte(r.Grezzo), &c)
		var resto []any
		for _, a := range coda {
			if id := a.(map[string]any)["update_id"].(int64); id >= c.Offset {
				resto = append(resto, a)
			}
		}
		if c.Limit == 1 && len(resto) > 1 {
			resto = resto[:1]
		}
		j, _ := json.Marshal(map[string]any{"ok": true, "result": append([]any{}, resto...)})
		return 200, string(j)
	})
	letture := func() []string {
		var out []string
		for _, r := range p.b.tutte() {
			if strings.HasSuffix(r.Percorso, "/getUpdates") {
				out = append(out, r.Grezzo)
			}
		}
		return out
	}
	senza := `{"allowed_updates":["message","callback_query"],"timeout":25}`
	uno := `{"allowed_updates":["message","callback_query"],"limit":1,"timeout":25}`
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) || fmt.Sprint(letture()) != "["+senza+" "+uno+"]" {
		t.Fatalf("prima lettura %+v: %v", e, letture())
	}
	p.arrivano()
	if e := p.leggi(t); e != (EsitoLettura{Gestiti: 1}) || fmt.Sprint(letture()) != `[{"allowed_updates":["message","callback_query"],"offset":2,"limit":1,"timeout":25}]` {
		t.Fatalf("seconda lettura %+v: %v", e, letture())
	}
	p.arrivano()
	if e := p.leggi(t); e != (EsitoLettura{}) || fmt.Sprint(letture()) != `[{"allowed_updates":["message","callback_query"],"offset":3,"limit":1,"timeout":25}]` {
		t.Fatalf("lettura vuota %+v: %v", e, letture())
	}
	p.arrivano()
	if e := p.leggi(t); e != (EsitoLettura{}) || fmt.Sprint(letture()) != `[{"allowed_updates":["message","callback_query"],"offset":3,"timeout":25}]` {
		t.Fatalf("di nuovo senza limite %+v: %v", e, letture())
	}
	coda = []any{messaggio(3, chatA, strings.Repeat("x", massimoAggiornamenti))}
	p.arrivano()
	if e := p.leggi(t); e.Errore != "telegram:risposta non riconosciuta" || len(letture()) != 2 {
		t.Errorf("un aggiornamento troppo grande: %+v, %d letture", e, len(letture()))
	}
}

// inventario: CLI-46
// Review della 075 (R17): una risposta troppo grande con un codice diverso da 200 è l'errore del codice e
// non fa entrare nel modo «un aggiornamento alla volta»: niente rilettura, e la lettura dopo è senza limit.
func TestLettoreTroppoGrandeNon200(t *testing.T) {
	p := nuovaProvaLettore(t)
	grande := `{"ok":false,"description":"` + strings.Repeat("x", massimoAggiornamenti) + `"}`
	p.b.risposta(func(richiestaBot) (int, string) { return 502, grande })
	if e := p.leggi(t); e.Errore != "telegram:http:502" || p.b.quante() != 1 {
		t.Fatalf("esito %+v, richieste %d", e, p.b.quante())
	}
	p.arrivano()
	p.b.risposta(nil)
	p.leggi(t)
	if r := p.b.tutte(); len(r) != 1 || strings.Contains(r[0].Grezzo, "limit") {
		t.Errorf("lettura dopo: %+v", r)
	}
}

// righePulizia sono le righe per la prova della pulizia, con gli istanti relativi a inizio.
func righePulizia(t *testing.T, d *db.DB) {
	t.Helper()
	novanta := ConservazioneTelegram
	bustaProva(t, &Coda{d: d}, "1/26")
	iscr := func(chat int64, fermata any) {
		if _, err := d.Scrittura.Exec(`INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, fermata_il, creato_il, aggiornato_il)
			VALUES (1, ?1, 0, ?2, ?3, ?2, ?2)`, chat, fra(-200*24*time.Hour), fermata); err != nil {
			t.Fatal(err)
		}
	}
	iscr(101, fra(-novanta-time.Microsecond)) // via
	iscr(102, fra(-novanta))                  // resta: novanta giorni esatti
	iscr(103, nil)                            // resta: mai fermata
	iscr(104, fra(-200*24*time.Hour))         // via
	tok := func(h string, scade string) {
		if _, err := d.Scrittura.Exec(`INSERT INTO token_telegram (busta_id, hash_token, scade_il, creato_il) VALUES (1, ?, ?, ?)`, []byte(h), scade, fra(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	tok("scaduto", fra(-time.Second))
	tok("alla scadenza", fra(0))
	tok("valido", fra(time.Microsecond))
	msg := func(canale, parte, indirizzo, stato string, accodato time.Duration) {
		if _, err := d.Scrittura.Exec(`INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi,
			prossimo_tentativo_il, accodato_il) VALUES (1, ?, ?, ?, 'Ciao', ?, 0, ?5, ?5)`, canale, parte, indirizzo, stato, fra(accodato)); err != nil {
			t.Fatal(err)
		}
	}
	vecchio := -novanta - time.Microsecond
	msg("telegram", "customer", "4242", "accepted", vecchio)  // cancellato
	msg("telegram", "customer", "4243", "failed", vecchio)    // cancellato
	msg("telegram", "customer", "4244", "queued", vecchio)    // resta: ancora in coda
	msg("telegram", "customer", "4245", "accepted", -novanta) // resta: novanta giorni esatti
	msg("telegram", "shop", "-1001", "accepted", vecchio)     // resta: il negozio
	msg("email", "customer", "cliente@esempio.invalid", "accepted", vecchio)
	msg("whatsapp", "customer", "+390000000001", "accepted", vecchio)
	msg("telegram", "customer", "cancellato", "accepted", vecchio) // già cancellato: non si conta
}

// inventario: CLI-50
// La pulizia (TelegramSubscriptions.purge/1): via le iscrizioni fermate da più di 90 giorni e i token
// scaduti (alla scadenza esatta compresa); le notifiche Telegram dei clienti accodate da più di 90 giorni e
// non più in coda perdono la chat («cancellato»). Il resto resta com'è. Dà i tre conti.
func TestPuliziaTelegram(t *testing.T) {
	p := nuovaProvaLettore(t)
	righePulizia(t, p.d)
	n, err := p.l.Pulisci(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != (PuliziaTelegram{Iscrizioni: 2, Token: 2, Messaggi: 2}) {
		t.Errorf("conti %+v", n)
	}
	if v := valoreDB(t, p.d, `SELECT group_concat(chat_id) FROM (SELECT chat_id FROM iscrizioni_telegram ORDER BY id)`); v != "102,103" {
		t.Errorf("iscrizioni rimaste %s", v)
	}
	if v := valoreDB(t, p.d, `SELECT group_concat(CAST(hash_token AS TEXT)) FROM token_telegram`); v != "valido" {
		t.Errorf("token rimasti %s", v)
	}
	if v := valoreDB(t, p.d, `SELECT group_concat(indirizzo, ',') FROM (SELECT indirizzo FROM messaggi_in_uscita ORDER BY id)`); v !=
		"cancellato,cancellato,4244,4245,-1001,cliente@esempio.invalid,+390000000001,cancellato" {
		t.Errorf("indirizzi %s", v)
	}
	if v := valoreDB(t, p.d, `SELECT group_concat(stato || testo, ',') FROM (SELECT stato, testo FROM messaggi_in_uscita ORDER BY id)`); v !=
		"acceptedCiao,failedCiao,queuedCiao,acceptedCiao,acceptedCiao,acceptedCiao,acceptedCiao,acceptedCiao" {
		t.Errorf("righe cambiate %s", v)
	}
	if n, err := p.l.Pulisci(context.Background()); err != nil || n != (PuliziaTelegram{}) {
		t.Errorf("di nuovo: %+v %v", n, err)
	}
}

// inventario: CLI-50
// La pulizia gira dal lavoro del lettore una volta all'ora (TelegramPoller: purge_due?/2), anche con il
// canale spento, prima della lettura; la prima subito. Un errore della pulizia non ferma la lettura e
// nel registro c'è solo il genere.
func TestLavoroPuliziaOraria(t *testing.T) {
	p := nuovaProvaLettore(t)
	impostazione(t, p.d, ChiaveNotificheClienti, "")
	righePulizia(t, p.d)
	conta := func() int {
		return contaRighe(t, p.d, `SELECT count(*) FROM iscrizioni_telegram`)
	}
	passi := []time.Duration{59*time.Minute + 59*time.Second, time.Second, 30 * time.Minute}
	var dopo []int
	p.l.Lavoro(context.Background(), func(_ context.Context, d time.Duration) bool {
		dopo = append(dopo, conta())
		if len(dopo) > len(passi) {
			return false
		}
		// Fra un giro e l'altro una nuova iscrizione fermata da 91 giorni.
		if _, err := p.d.Scrittura.Exec(`INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, fermata_il, creato_il, aggiornato_il)
			VALUES (1, ?1, 0, ?2, ?2, ?2, ?2)`, 500+len(dopo), p.o.Adesso().UTC().Add(-91*24*time.Hour).Format(db.FormatoIstante)); err != nil {
			t.Fatal(err)
		}
		p.o.Sposta(passi[len(dopo)-1])
		return true
	})
	// Subito: via le due vecchie (restano 2); dopo 59:59 nessuna pulizia (3); a un'ora sì, e anche quella
	// fermata da novanta giorni esatti ormai è oltre (1); mezz'ora dopo no (2).
	if fmt.Sprint(dopo) != "[2 3 1 2]" {
		t.Errorf("iscrizioni dopo ogni giro %v", dopo)
	}
	if p.b.quante() != 0 {
		t.Errorf("richieste a canale spento: %d", p.b.quante())
	}
	q := nuovaProvaLettore(t)
	impostazione(t, q.d, ChiaveNotificheClienti, "")
	if _, err := q.d.Scrittura.Exec(`DROP TABLE token_telegram`); err != nil {
		t.Fatal(err)
	}
	giri := 0
	q.l.Lavoro(context.Background(), func(context.Context, time.Duration) bool {
		giri++
		return giri < 2
	})
	// Una sola pulizia non riuscita in due giri senza che passi un'ora: l'ora si tiene anche dopo un errore.
	if giri != 2 || strings.Count(q.log.String(), "pulizia non riuscita") != 1 {
		t.Errorf("giri %d, registro %s", giri, q.log.String())
	}
}

func contaRighe(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
