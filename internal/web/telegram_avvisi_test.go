package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il popup «📲 Notifica Telegram al cliente» dopo un cambio di stato dalla scheda e dalla lista (CLI-47,
// RIP-H-18) e la notifica Telegram dopo la risposta della scheda (CLI-14, CLI-48), come
// status_telegram_popup_live_test.exs e repair_reply_telegram_live_test.exs dello standalone
// (go-baseline), senza LiveView. Chat fuori da ogni intervallo di Telegram, dati inventati.

// facciaAvvisiTelegram è la faccia interna con le buste vere, la coda di Telegram accesa (provider finto), le
// notifiche ai clienti accese e l'indirizzo pubblico.
func facciaAvvisiTelegram(t *testing.T) (http.Handler, *db.DB, *clock.Finto, *bottinoFinto) {
	t.Helper()
	return facciaAvvisiTelegramCon(t, func(p buste.Posta) buste.Posta { return p })
}

// facciaAvvisiTelegramCon è facciaAvvisiTelegram con la coda avvolta da avvolgi.
func facciaAvvisiTelegramCon(t *testing.T, avvolgi func(buste.Posta) buste.Posta) (http.Handler, *db.DB, *clock.Finto, *bottinoFinto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio'), (2, 'Oreficeria Esempio')`)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega di Prova', '', ?, ?)`,
		istanteHome, istanteHome)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '1', ?)`, uscita.ChiaveNotificheClienti, istanteHome)
	bot := &bottinoFinto{}
	coda := uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleTelegram: bot})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o).ConPosta(avvolgi(coda), []byte("chiave-email-di-prova-di-32-byte")).ConIndirizzoPubblico(indirizzoPubblicoProva)
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	return mustInterna(t, dd), d, o, bot
}

const msgAccodata = `<div role="status" class="flash flash-info">📲 Notifica Telegram accodata per il cliente</div>`

var reGettoneTelegram = regexp.MustCompile(`<input name="notifica" type="hidden" value="([A-Za-z0-9_-]{22})">`)

// popupDi è il popup della pagina e il suo gettone; "" e "" se non c'è.
func popupDi(corpo string) (string, string) {
	i := strings.Index(corpo, `<div id="telegram-status-popup"`)
	if i < 0 {
		return "", ""
	}
	p := corpo[i:]
	p = p[:strings.Index(p, "</form>")+len("</form>")]
	m := reGettoneTelegram.FindStringSubmatch(p)
	if m == nil {
		return p, ""
	}
	return p, m[1]
}

func rispondiPopup(b *browser, id int64, gettone, azione string) risposta {
	return b.vai("POST", percorsoScheda(id)+"/notifica-telegram", url.Values{"_csrf_token": {b.token}, "notifica": {gettone}, "azione": {azione}}, nil)
}

func righeTelegram(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE canale = 'telegram' AND destinatario = 'customer'`)
}

// inventario: CLI-47, RIP-H-18
// Dalla scheda: il cambio di stato con un iscritto apre il popup con l'anteprima in sola lettura (le
// parole del cliente, non i tag), il pulsante, «Invia» e «Annulla»; niente in coda finché non si preme
// «Invia», che accoda e lo dice; un secondo «Invia» non manda niente; ricaricare la pagina toglie il popup.
func TestPopupTelegramScheda(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "descrizione = 'Anello <oro> & perle'")
	iscrittaWeb(t, d, id)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("POST", azioneStato(id), formStato(b, "pronta"), nil)
	if !redirectAllaScheda(r, id) {
		t.Fatalf("cambio: %d %q", r.codice, r.location)
	}
	pagina := b.vai("GET", r.location, nil, nil).corpo
	popup, gettone := popupDi(pagina)
	if gettone == "" {
		t.Fatalf("nessun popup: %s", popup)
	}
	contiene(t, "popup", popup,
		`<div id="telegram-status-popup" class="avviso-sfondo avviso-sopra" role="dialog" aria-modal="true" aria-labelledby="telegram-status-title" data-popup-telegram>`,
		`<h2 id="telegram-status-title">📲 Notifica Telegram al cliente</h2>`,
		`Busta <span id="telegram-status-bag">N. 1/26</span>: il cliente è iscritto alle notifiche Telegram.`,
		`<div id="telegram-status-preview" class="avviso-anteprima">Ciao Cliente! 👋`+"\n\n"+`✨ La tua riparazione è ora Pronta per il ritiro`+"\n\n"+
			`📦 Busta: 1/26`+"\n"+`💍 Anello &lt;oro&gt; &amp; perle`+"\n\n"+`— Bottega di Prova</div>`,
		`<p id="telegram-status-button" class="avviso-tasto">📂 Apri scheda riparazione</p>`,
		`<form action="/repairs/`+idTesto(id)+`/notifica-telegram" method="post" data-invio-unico>`,
		`<input name="_csrf_token" type="hidden" value="`+b.token+`"><input name="notifica" type="hidden" value="`+gettone+`">`,
		`<button id="telegram-status-send" type="submit" name="azione" value="invia">Invia</button>`,
		`<button id="telegram-status-cancel" type="submit" name="azione" value="annulla" class="pulsante-contorno" data-telegram-annulla>Annulla</button>`)
	nonContiene(t, "popup", popup, "&lt;b&gt;", "<textarea", "4503599627370497")
	contiene(t, "esito", pagina, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`)
	if righeTelegram(t, d) != 0 {
		t.Fatal("accodato prima di «Invia»")
	}
	if p, _ := popupDi(b.vai("GET", percorsoScheda(id), nil, nil).corpo); p != "" {
		t.Error("il popup resta alla pagina ricaricata")
	}
	x := rispondiPopup(b, id, gettone, "invia")
	if !redirectAllaScheda(x, id) {
		t.Fatalf("invia: %d %q", x.codice, x.location)
	}
	contiene(t, "invia", b.vai("GET", x.location, nil, nil).corpo, msgAccodata, "Telegram cliente")
	if righeTelegram(t, d) != 1 || contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = '4503599627370497' AND testo LIKE '%Pronta per il ritiro%'`) != 1 {
		t.Error("riga non accodata")
	}
	// Il doppio tocco su «Invia»: il secondo invio torna dove torna il primo, senza errore né un'altra riga.
	x = rispondiPopup(b, id, gettone, "invia")
	if !redirectAllaScheda(x, id) || righeTelegram(t, d) != 1 {
		t.Errorf("secondo invia: %q %d", x.location, righeTelegram(t, d))
	}
	nonContiene(t, "secondo invia", b.vai("GET", x.location, nil, nil).corpo, "Notifica Telegram non inviata", "telegram-status-popup")
	// Un gettone che il deposito non ha: la lista con la frase.
	if x := rispondiPopup(b, id, "AAAAAAAAAAAAAAAAAAAAAA", "invia"); !redirectA(x, "/") {
		t.Errorf("gettone sconosciuto: %q", x.location)
	}
	contiene(t, "gettone sconosciuto", b.vai("GET", "/", nil, nil).corpo, msgTelegramNonValida)
}

// inventario: RIP-H-18, CLI-47
// Il doppio tocco su «Invia» dopo «✓ Pronta»: le due risposte vanno al prezzo, senza errore e con una riga sola.
func TestPopupTelegramDoppioInvio(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	b.vai("GET", "/", nil, nil)
	b.vai("POST", "/repairs/"+idTesto(id)+"/rapido", formLista(b, "", "status", "pronta"), nil)
	_, g := popupDi(b.vai("GET", "/", nil, nil).corpo)
	prezzo := "/repairs/" + idTesto(id) + "?edit=prezzo"
	for i := range 2 {
		if x := rispondiPopup(b, id, g, "invia"); !redirectA(x, prezzo) {
			t.Fatalf("invio %d: %q", i+1, x.location)
		}
	}
	nonContiene(t, "prezzo", b.vai("GET", prezzo, nil, nil).corpo, "Notifica Telegram non inviata")
	if righeTelegram(t, d) != 1 {
		t.Errorf("righe: %d", righeTelegram(t, d))
	}
}

// inventario: CLI-47
// Un popup di più di un'ora non si disegna e «Invia» dice che non vale più; il gettone di una sessione non
// vale in un'altra dello stesso account; il deposito tiene al più 256 popup e perde il più vecchio.
func TestPopupTelegramScadenzaESessioni(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	cambia(t, b, id, "pronta")
	_, g := popupDi(b.vai("GET", percorsoScheda(id), nil, nil).corpo)
	altra := dipendenteDentro(t, h) // lo stesso account, un'altra sessione
	altra.vai("GET", percorsoScheda(id), nil, nil)
	if x := rispondiPopup(altra, id, g, "invia"); !redirectA(x, "/") || righeTelegram(t, d) != 0 {
		t.Errorf("altra sessione: %q %d", x.location, righeTelegram(t, d))
	}
	o.Sposta(61 * time.Minute)
	if x := rispondiPopup(b, id, g, "invia"); !redirectA(x, "/") || righeTelegram(t, d) != 0 {
		t.Errorf("scaduto: %q %d", x.location, righeTelegram(t, d))
	}
	contiene(t, "scaduto", b.vai("GET", "/", nil, nil).corpo, msgTelegramNonValida)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("POST", azioneStato(id), formStato(b, "in_lavorazione"), nil)
	o.Sposta(61 * time.Minute)
	if p, _ := popupDi(b.vai("GET", r.location, nil, nil).corpo); p != "" {
		t.Error("popup scaduto disegnato")
	}

	dep := nuovoDepositoNotifiche(o)
	n := &buste.NotificaTelegram{BustaID: 1}
	primo := dep.metti(&attesaTelegram{utente: 1, nonce: "n", n: n})
	for range maxNotificheInAttesa - 1 {
		dep.metti(&attesaTelegram{utente: 1, nonce: "n", n: n})
	}
	if dep.guarda(primo, 1, "n") == nil {
		t.Fatal("perso prima del limite")
	}
	dep.metti(&attesaTelegram{utente: 1, nonce: "n", n: n})
	if dep.guarda(primo, 1, "n") != nil || len(dep.voci) != maxNotificheInAttesa {
		t.Errorf("oltre il limite: %d voci", len(dep.voci))
	}
}

// inventario: CLI-47, RIP-H-18
// «Annulla» non manda niente e non dice niente; un popup superato da un cambio successivo (anche senza
// popup) lo dice e non manda niente; un cambio dopo toglie il popup di prima anche senza avviso suo; senza
// iscritti nessun popup; un testo che non si compone non apre il popup e dice perché; un altro account non
// usa il gettone.
func TestPopupTelegramAnnullaESuperato(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	cambia(t, b, id, "pronta")
	_, g := popupDi(b.vai("GET", percorsoScheda(id), nil, nil).corpo)
	x := rispondiPopup(b, id, g, "annulla")
	if !redirectAllaScheda(x, id) {
		t.Fatalf("annulla: %d %q", x.codice, x.location)
	}
	nonContiene(t, "annulla", b.vai("GET", x.location, nil, nil).corpo, "Notifica Telegram", "telegram-status-popup")
	if righeTelegram(t, d) != 0 {
		t.Fatal("annulla ha accodato")
	}

	cambia(t, b, id, "in_lavorazione")
	_, g = popupDi(b.vai("GET", percorsoScheda(id), nil, nil).corpo)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, id) // un cambio senza popup (il QR)
	x = rispondiPopup(b, id, g, "invia")
	contiene(t, "superato", b.vai("GET", x.location, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Notifica Telegram non inviata: superata da un cambio di stato successivo della busta.</div>`)

	// Il cambio dopo, senza iscritti, toglie il popup del cambio prima (mai visto).
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := b.vai("POST", azioneStato(id), formStato(b, "in_lavorazione"), nil); !redirectAllaScheda(r, id) {
		t.Fatalf("cambio con il popup: %q", r.location)
	}
	eseguiSQL(t, d, `UPDATE iscrizioni_telegram SET fermata_il = ?, motivo_fermata = 'stop' WHERE busta_id = ?`, istanteHome, id)
	r := b.vai("POST", azioneStato(id), formStato(b, "pronta"), nil) // senza vedere la pagina del popup di prima
	if !redirectAllaScheda(r, id) {
		t.Fatalf("cambio senza popup: %q", r.location)
	}
	if p, _ := popupDi(b.vai("GET", r.location, nil, nil).corpo); p != "" {
		t.Error("il popup del cambio prima è rimasto")
	}

	altra := bustaHome(t, d, o, "2/26", "", "")
	iscrittaWeb(t, d, altra)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ciao {ignoto}', ?)`, buste.FamTGCambioStato.Chiave(), istanteHome)
	b.vai("GET", percorsoScheda(altra), nil, nil)
	r = b.vai("POST", azioneStato(altra), formStato(b, "pronta"), nil)
	pagina := b.vai("GET", r.location, nil, nil).corpo
	contiene(t, "testo rotto", pagina, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`,
		`<div role="alert" class="flash flash-errore">Notifica Telegram non preparata: il testo «Cambio stato riparazione» non è valido (Segnaposto non supportato: {ignoto}).</div>`)
	if p, _ := popupDi(pagina); p != "" {
		t.Error("popup con il testo rotto")
	}
	eseguiSQL(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, buste.FamTGCambioStato.Chiave())

	terza := bustaHome(t, d, o, "3/26", "", "")
	iscrittaWeb(t, d, terza)
	cambia(t, b, terza, "pronta")
	_, g = popupDi(b.vai("GET", percorsoScheda(terza), nil, nil).corpo)
	tit := titolareDentro(t, h)
	tit.vai("GET", percorsoScheda(terza), nil, nil)
	if x := rispondiPopup(tit, terza, g, "invia"); !redirectA(x, "/") || righeTelegram(t, d) != 0 {
		t.Errorf("gettone di un altro account: %q %d", x.location, righeTelegram(t, d))
	}
	if x := rispondiPopup(b, altra, g, "invia"); !redirectA(x, "/") || righeTelegram(t, d) != 0 {
		t.Errorf("gettone su un'altra busta: %q %d", x.location, righeTelegram(t, d))
	}
	if x := rispondiPopup(b, terza, g, "invia"); !redirectAllaScheda(x, terza) || righeTelegram(t, d) != 1 {
		t.Errorf("gettone giusto: %q %d", x.location, righeTelegram(t, d))
	}
}

// inventario: RIP-H-18, CLI-47
// Dalla lista: il menu dello stato apre il popup sulla lista e «Invia» torna alla lista; «✓ Pronta» con un
// iscritto resta sulla lista con il popup e va al prezzo dopo la risposta, «Invia» o «Annulla»; senza
// iscritti va subito al prezzo.
func TestPopupTelegramLista(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	b.vai("GET", "/", nil, nil)
	r := b.vai("POST", "/repairs/"+idTesto(id)+"/stato-lista", formLista(b, "stato=in_lavorazione", "status", "pronta"), nil)
	if !redirectA(r, "/?stato=in_lavorazione") {
		t.Fatalf("menu: %d %q", r.codice, r.location)
	}
	pagina := b.vai("GET", r.location, nil, nil).corpo
	popup, g := popupDi(pagina)
	contiene(t, "lista", popup, `<span id="telegram-status-bag">N. 1/26</span>`, "Pronta per il ritiro")
	x := rispondiPopup(b, id, g, "invia")
	if !redirectA(x, "/?stato=in_lavorazione") {
		t.Fatalf("invia dalla lista: %q", x.location)
	}
	contiene(t, "invia dalla lista", b.vai("GET", x.location, nil, nil).corpo, msgAccodata)

	for i, azione := range []string{"invia", "annulla"} {
		rapida := bustaHome(t, d, o, idTesto(int64(10+i))+"/26", "", "")
		iscrittaWeb(t, d, rapida)
		b.vai("GET", "/", nil, nil)
		r := b.vai("POST", "/repairs/"+idTesto(rapida)+"/rapido", formLista(b, "", "status", "pronta"), nil)
		if !redirectA(r, "/") {
			t.Fatalf("✓ Pronta: %q", r.location)
		}
		pagina := b.vai("GET", "/", nil, nil).corpo
		contiene(t, "✓ Pronta", pagina, `<div role="status" class="flash flash-info">Stato aggiornato.</div>`)
		_, g := popupDi(pagina)
		prima := righeTelegram(t, d)
		x := rispondiPopup(b, rapida, g, azione)
		if !redirectA(x, "/repairs/"+idTesto(rapida)+"?edit=prezzo") {
			t.Errorf("%s: %q", azione, x.location)
		}
		if dopo := righeTelegram(t, d) - prima; (azione == "invia") != (dopo == 1) {
			t.Errorf("%s: %d righe", azione, dopo)
		}
	}
	// «✓ Consegnata» con un iscritto: il popup sulla lista, «Invia» accoda e torna alla lista.
	cons := bustaHome(t, d, o, "15/26", "", "stato = 'pronta'")
	iscrittaWeb(t, d, cons)
	b.vai("GET", "/", nil, nil)
	r = b.vai("POST", "/repairs/"+idTesto(cons)+"/rapido", formLista(b, "stato=pronta", "status", "consegnata", "conferma", "1"), nil)
	if !redirectA(r, "/?stato=pronta") {
		t.Fatalf("✓ Consegnata: %q", r.location)
	}
	popup, g = popupDi(b.vai("GET", r.location, nil, nil).corpo)
	contiene(t, "✓ Consegnata", popup, "La tua riparazione è ora Consegnata")
	prima := righeTelegram(t, d)
	if x := rispondiPopup(b, cons, g, "invia"); !redirectA(x, "/?stato=pronta") || righeTelegram(t, d) != prima+1 {
		t.Errorf("invia dopo ✓ Consegnata: %q %d", x.location, righeTelegram(t, d)-prima)
	}

	senza := bustaHome(t, d, o, "20/26", "", "")
	b.vai("GET", "/", nil, nil)
	if r := b.vai("POST", "/repairs/"+idTesto(senza)+"/rapido", formLista(b, "", "status", "pronta"), nil); !redirectA(r, "/repairs/"+idTesto(senza)+"?edit=prezzo") {
		t.Errorf("senza iscritti: %q", r.location)
	}
}

// inventario: CLI-14, CLI-48
// La risposta della scheda: con il cliente diretto iscritto con il consenso la notifica Telegram parte
// subito, lo dice e torna alla scheda, senza il popup WhatsApp; senza iscritti il popup WhatsApp come
// prima; un testo Telegram che non si compone non accoda niente, lo dice e apre il popup WhatsApp; su una
// busta conto terzi niente Telegram.
func TestRispostaConTelegram(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegram(t)
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	rispondi := func(busta int64, testo string) risposta {
		t.Helper()
		b.vai("GET", percorsoScheda(busta), nil, nil)
		return b.vai("POST", percorsoScheda(busta)+"/risposta", url.Values{"_csrf_token": {b.token}, "reply[body]": {testo}}, nil)
	}
	x := rispondi(id, "Pronta <lunedì>")
	if !redirectAllaScheda(x, id) {
		t.Fatalf("risposta: %d %q", x.codice, x.location)
	}
	pagina := b.vai("GET", x.location, nil, nil).corpo
	contiene(t, "telegram", pagina, `<div role="status" class="flash flash-info">Risposta inviata. 📲 Notifica Telegram accodata per il cliente</div>`)
	nonContiene(t, "telegram", pagina, "notice-popup", "4503599627370497")
	if contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE canale = 'telegram' AND testo LIKE '%<blockquote>Pronta &lt;lunedì&gt;</blockquote>%'`) != 1 {
		t.Error("notifica non accodata")
	}

	senza := bustaHome(t, d, o, "2/26", "", "")
	if x := rispondi(senza, "Ciao"); !strings.HasPrefix(x.location, percorsoScheda(senza)+"/avviso?scopo=risposta") {
		t.Errorf("senza iscritti: %q", x.location)
	}

	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ciao {sconosciuto}', ?)`, buste.FamTGNuovoMessaggio.Chiave(), istanteHome)
	x = rispondi(id, "Ancora")
	if !strings.HasPrefix(x.location, percorsoScheda(id)+"/avviso?scopo=risposta") {
		t.Fatalf("testo rotto: %q", x.location)
	}
	contiene(t, "testo rotto", b.vai("GET", x.location, nil, nil).corpo, `<div role="status" class="flash flash-info">Risposta inviata.</div>`,
		`<div role="alert" class="flash flash-errore">Notifica Telegram non accodata: il testo «Nuovo messaggio dal negozio» non è valido (Segnaposto non supportato: {sconosciuto}).</div>`,
		`<h2 id="notice-title">📲 Avvisa il cliente</h2>`)
	eseguiSQL(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, buste.FamTGNuovoMessaggio.Chiave())

	oref := bustaHome(t, d, o, "3/26", "", "")
	iscrittaWeb(t, d, oref)
	eseguiSQL(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, oref)
	eseguiSQL(t, d, `UPDATE iscrizioni_telegram SET consenso = 1, fermata_il = NULL WHERE busta_id = ?`, oref)
	if x := rispondi(oref, "Ciao"); !strings.HasPrefix(x.location, percorsoScheda(oref)+"/avviso?scopo=risposta") {
		t.Errorf("conto terzi: %q", x.location)
	}
	if righeTelegram(t, d) != 1 {
		t.Errorf("righe Telegram: %d", righeTelegram(t, d))
	}
}

// codaCheRifiutaIn è la coda vera che rifiuta di accodare in una transazione (gli avvisi al cliente).
type codaCheRifiutaIn struct{ buste.Posta }

func (codaCheRifiutaIn) AccodaIn(context.Context, *sql.Tx, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	return 0, errors.New("coda rotta")
}

// inventario: CLI-14, CLI-48
// Un errore Telegram della risposta che non è un testo rotto (la coda che rifiuta) lo dice con la frase
// generica e apre il popup WhatsApp; niente in coda.
func TestRispostaTelegramCodaCheRifiuta(t *testing.T) {
	h, d, o, _ := facciaAvvisiTelegramCon(t, func(p buste.Posta) buste.Posta { return codaCheRifiutaIn{p} })
	b := dipendenteDentro(t, h)
	id := bustaHome(t, d, o, "1/26", "", "")
	iscrittaWeb(t, d, id)
	b.vai("GET", percorsoScheda(id), nil, nil)
	x := b.vai("POST", percorsoScheda(id)+"/risposta", url.Values{"_csrf_token": {b.token}, "reply[body]": {"Ciao"}}, nil)
	if !strings.HasPrefix(x.location, percorsoScheda(id)+"/avviso?scopo=risposta") {
		t.Fatalf("risposta: %q", x.location)
	}
	contiene(t, "coda che rifiuta", b.vai("GET", x.location, nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Notifica Telegram non accodata: non è stato possibile accodarla.</div>`,
		`<h2 id="notice-title">📲 Avvisa il cliente</h2>`)
	if righeTelegram(t, d) != 0 {
		t.Error("righe accodate")
	}
}
