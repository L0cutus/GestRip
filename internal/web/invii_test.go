package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/db"
)

// «📨 Invii registrati» della scheda e «Togli dalla lista» (invii.go, slice 067), come
// #repair-send-history e l'evento discard-send di RepairLive dello standalone: i casi di
// repair_send_live_test.exs e repair_send_history_email_live_test.exs, senza LiveView. Dati inventati.

func rigaInviataWeb(t *testing.T, d *db.DB, busta int64, canale, parte, indirizzo, testo, stato string, accettato any) int64 {
	t.Helper()
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi,
		prossimo_tentativo_il, accodato_il, accettato_il) VALUES (?, ?, ?, ?, ?, ?, 1, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z', ?)`,
		busta, canale, parte, indirizzo, testo, stato, accettato)
	return int64(contaSQL(t, d, `SELECT max(id) FROM messaggi_in_uscita`))
}

func idInvio(n int64) string { return idTesto(n) }

// inventario: CLI-37, RIP-S-21
// La lista, chiusa, sotto «Rispondi al cliente»: il conteggio, una didascalia per canale presente
// (WhatsApp, email, Telegram, in quest'ordine), le righe dalla più recente con canale (· all'artigiano),
// stato («Consegnato a WhatsApp», «In coda»…) e ora, l'indirizzo (non per Telegram cliente, «contatto
// scelto sul telefono» per il contatto scelto), il testo (i link dell'email mascherati, l'HTML di Telegram
// tolto, le entità rilette come plain_text/1: «&amp;lt;» resta «&lt;»; la maschera prende tutta la parola, anche «Stop:» attaccato al link, come mask_links/1) e «Togli dalla lista» solo sulle righe WhatsApp senza avviso di ritiro. Una busta senza invii
// non ha la lista.
func TestInviiRegistratiNellaScheda(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	vuota := bustaHome(t, d, o, "2/26", "", "")
	wa := rigaInviataWeb(t, d, id, "whatsapp", "customer", "+39333111", "Primo <messaggio>\nseconda riga", "accepted", "2026-10-30T08:00:00.000000Z")
	ritiro := rigaInviataWeb(t, d, id, "whatsapp", "customer", "+39333111", "Ritiro", "accepted", "2026-10-30T08:00:00.000000Z")
	eseguiSQL(t, d, `INSERT INTO eventi_avviso_pronta (busta_id, evento, canale, messaggio_in_uscita_id, avvenuto_il, creato_il, aggiornato_il)
		VALUES (?, 'notified', 'whatsapp', ?, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z')`, id, ritiro)
	mail := rigaInviataWeb(t, d, id, "email", "customer", "ada@esempio.test",
		"Conferma: https://h.esempio.test/stato/email-conferma?t=abc&x=1 fine\nStop:https://h.esempio.test/stato/email-disattiva?t=zz.", "queued", nil)
	tg := rigaInviataWeb(t, d, id, "telegram", "customer", "123456789", "<b>Pronta</b> &amp; ok &lt;3 &amp;lt;3", "accepted", "2026-10-30T09:30:00.000000Z")
	art := rigaInviataWeb(t, d, id, "whatsapp", "artisan", "contatto-scelto", "Sollecito", "accepted", "2026-10-30T08:00:00.000000Z")
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	segno := b.segno
	riga := func(n int64, testa, indirizzo, testo string, togli bool) string {
		s := `<li id="repair-send-message-` + idInvio(n) + `" class="invio">` + "\n" + `<div class="invio-testa">` + testa + `</div>` + "\n"
		if indirizzo != "" {
			s += `<div class="invio-indirizzo">` + indirizzo + `</div>` + "\n"
		}
		s += `<div class="invio-testo">` + testo + `</div>` + "\n"
		if togli {
			s += `<form action="/repairs/` + idTesto(id) + `/togli-invio" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="` + b.token +
				`"><input name="mostra" type="hidden" value="` + segno + `"><input type="hidden" name="invio" value="` + idInvio(n) +
				`"><button id="repair-send-discard-` + idInvio(n) + `" type="submit" class="pulsante-piccolo" data-confirm="Togliere questo messaggio dalla lista? Non cambia ciò che hai già inviato su WhatsApp.">Togli dalla lista</button></form>` + "\n"
		}
		return s + `</li>` + "\n"
	}
	atteso := `<details id="repair-send-history" class="riquadro-invii">` + "\n" + `<summary>📨 Invii registrati (5)</summary>` + "\n" +
		`<p id="repair-send-caption-whatsapp" class="sotto">WhatsApp: registrano che hai aperto WhatsApp con questo messaggio, mai che sia stato recapitato o letto.</p>` + "\n" +
		`<p id="repair-send-caption-email" class="sotto">Posta SMTP: registrano che l&#39;email è in coda o che il server di posta l&#39;ha accettata, mai che sia stata recapitata o letta. I link di conferma e di disattivazione restano solo nell&#39;email del destinatario.</p>` + "\n" +
		`<p id="repair-send-caption-telegram" class="sotto">Telegram cliente: registrano che la notifica è in coda o che Telegram l&#39;ha accettata, mai che sia stata recapitata o letta. Il tasto della notifica apre la pagina della busta.</p>` + "\n" +
		`<ul>` + "\n" +
		riga(art, `<span class="grassetto">WhatsApp<span> · all'artigiano</span></span> <span class="sotto">Consegnato a WhatsApp · 30/10/2026 09:00</span>`, "contatto scelto sul telefono", "Sollecito", true) +
		riga(tg, `<span class="grassetto">Telegram cliente</span> <span class="sotto">Accettato dal provider · 30/10/2026 10:30</span>`, "", "Pronta &amp; ok &lt;3 &amp;lt;3", false) +
		riga(mail, `<span class="grassetto">Posta SMTP</span> <span class="sotto">In coda · 30/10/2026 09:00</span>`, "ada@esempio.test",
			"Conferma: [link di conferma: solo nell&#39;email] fine\n[link per disattivare gli avvisi: solo nell&#39;email]", false) +
		riga(ritiro, `<span class="grassetto">WhatsApp</span> <span class="sotto">Consegnato a WhatsApp · 30/10/2026 09:00</span>`, "&#43;39333111", "Ritiro", false) +
		riga(wa, `<span class="grassetto">WhatsApp</span> <span class="sotto">Consegnato a WhatsApp · 30/10/2026 09:00</span>`, "&#43;39333111", "Primo &lt;messaggio&gt;\nseconda riga", true) +
		`</ul>` + "\n" + `</details>`
	contiene(t, "lista", corpo, atteso)
	if strings.Index(corpo, `id="repair-reply"`) > strings.Index(corpo, `id="repair-send-history"`) {
		t.Error("la lista non sta sotto «Rispondi al cliente»")
	}
	nonContiene(t, "lista", corpo, "123456789", "t=abc", "t=zz")
	nonContiene(t, "vuota", b.vai("GET", percorsoScheda(vuota), nil, nil).corpo, "repair-send-history")
	// Senza numero la lista resta.
	eseguiSQL(t, d, `UPDATE buste SET telefono_cliente = '' WHERE id = ?`, id)
	contiene(t, "senza numero", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `<summary>📨 Invii registrati (5)</summary>`)
}

var reTogli = regexp.MustCompile(`<input name="mostra" type="hidden" value="([A-Za-z0-9_-]{22})"><input type="hidden" name="invio" value="(\d+)">`)

// inventario: CLI-37, RIP-S-21
// «Togli dalla lista» dal dipendente: la riga se ne va, la scheda torna con «Messaggio tolto dalla
// lista.» (e senza la lista, se era l'ultima); l'audit outgoing.handoff_discarded. Una riga già tolta da
// un collega: «Questo messaggio non è più in lista.», niente scritto; la riga dell'avviso di ritiro
// forzata: «Questo è l'avviso di pronta: si annulla con «azzera avviso», non da qui.»; un invio non
// numerico come una riga che non c'è; una busta che non c'è: la home con «Riparazione non trovata.».
// Senza CSRF 403, senza accesso il login.
func TestTogliDallaLista(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "")
	wa := rigaInviataWeb(t, d, id, "whatsapp", "customer", "+39333111", "Da togliere", "accepted", "2026-10-30T08:00:00.000000Z")
	ritiro := rigaInviataWeb(t, d, id, "whatsapp", "customer", "+39333111", "Ritiro", "accepted", "2026-10-30T08:00:00.000000Z")
	eseguiSQL(t, d, `INSERT INTO eventi_avviso_pronta (busta_id, evento, canale, messaggio_in_uscita_id, avvenuto_il, creato_il, aggiornato_il)
		VALUES (?, 'notified', 'whatsapp', ?, '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z', '2026-10-30T08:00:00.000000Z')`, id, ritiro)
	b := dipendenteDentro(t, h)
	corpo := b.vai("GET", percorsoScheda(id), nil, nil).corpo
	m := reTogli.FindStringSubmatch(corpo)
	if m == nil || m[2] != idInvio(wa) {
		t.Fatalf("manca il modulo:\n%s", corpo)
	}
	stato := func() int {
		return contaRighe(t, d, `SELECT (SELECT count(*) FROM eventi_audit) * 1000 + (SELECT count(*) FROM messaggi_in_uscita)`)
	}
	// Senza CSRF.
	if r := b.vai("POST", percorsoScheda(id)+"/togli-invio", url.Values{"mostra": {m[1]}, "invio": {m[2]}}, nil); r.codice != 403 {
		t.Errorf("senza CSRF %d", r.codice)
	}
	prima := stato()
	r := b.vai("POST", percorsoScheda(id)+"/togli-invio", url.Values{"_csrf_token": {b.token}, "mostra": {m[1]}, "invio": {m[2]}}, nil)
	if !redirectAllaScheda(r, id) {
		t.Fatalf("togli %d %q", r.codice, r.location)
	}
	corpo = b.vai("GET", r.location, nil, nil).corpo
	contiene(t, "tolto", corpo, `<div role="status" class="flash flash-info">Messaggio tolto dalla lista.</div>`, `<summary>📨 Invii registrati (1)</summary>`)
	nonContiene(t, "tolto", corpo, "Da togliere")
	if contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ?`, wa) != 0 {
		t.Error("riga rimasta")
	}
	if n := contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.handoff_discarded' AND id_risorsa = ? AND attore = 'banco@esempio.it'`, idInvio(wa)); n != 1 {
		t.Errorf("audit %d", n)
	}
	if stato() != prima-1+1000 {
		t.Errorf("scritture %d → %d", prima, stato())
	}
	// Già tolta (una pagina vecchia), l'avviso di ritiro, un invio malformato: niente si scrive.
	for _, c := range []struct{ invio, msg string }{
		{idInvio(wa), "Questo messaggio non è più in lista."},
		{idInvio(ritiro), "Questo è l&#39;avviso di pronta: si annulla con «azzera avviso», non da qui."},
		{"x1", "Questo messaggio non è più in lista."},
	} {
		b.vai("GET", percorsoScheda(id), nil, nil)
		prima := stato()
		r := b.vai("POST", percorsoScheda(id)+"/togli-invio", url.Values{"_csrf_token": {b.token}, "mostra": {b.segno}, "invio": {c.invio}}, nil)
		if !redirectAllaScheda(r, id) {
			t.Fatalf("%s: %d %q", c.invio, r.codice, r.location)
		}
		contiene(t, c.invio, b.vai("GET", r.location, nil, nil).corpo, `<div role="alert" class="flash flash-errore">`+c.msg+`</div>`)
		if stato() != prima {
			t.Errorf("%s: il rifiuto ha scritto", c.invio)
		}
	}
	if r := b.vai("POST", "/repairs/999/togli-invio", url.Values{"_csrf_token": {b.token}, "invio": {idInvio(ritiro)}}, nil); r.codice != 302 || r.location != "/" {
		t.Errorf("busta assente %d %q", r.codice, r.location)
	}
	fuori := nuovoBrowser(t, h)
	if r := fuori.vai("POST", percorsoScheda(id)+"/togli-invio", url.Values{"invio": {idInvio(ritiro)}}, nil); r.codice != 403 && (r.codice != 302 || r.location != "/login") {
		t.Errorf("senza accesso %d %q", r.codice, r.location)
	}
}
