package web

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// «📤 Chiudi e invia» nella pagina del lotto (lotto_invio.go, slice 091, ORE-10), come l'evento send di ReadyLotLive:
// i casi di ready_lot_live_test.exs («sending the lot», «the batch send and the bag links (slice 104)»), senza
// LiveView. Nomi inventati; l'oreficeria 2 «Alfa Orafi» ha il telefono 0490000000, il suo lotto il gettone
// «gettone-lotto-091».

const (
	gettoneLotto091  = "gettone-lotto-091"
	confermaInvio091 = "Inviare il lotto? Ogni busta risulterà avvisata e il lotto non sarà più modificabile."
)

// inventario: ORE-10
// Il riquadro «Chiudi e invia» c'è solo su un lotto aperto con almeno una busta visibile: titolo, frase con
// l'oreficeria, il modulo POST con data-invio-unico e il pulsante con la domanda (data-confirm). Senza un numero
// WhatsApp dell'oreficeria (nessun telefono o niente cifre) l'avviso che il contatto si sceglierà dopo.
func TestRiquadroChiudiEInvia(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	r := b.vai("GET", percorsoLotto(l), nil, nil)
	nonContiene(t, "vuoto", r.corpo, "ready-lot-send", "Chiudi e invia")
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	r = b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "con una busta", r.corpo,
		`</section>`+"\n"+`<section id="ready-lot-send" class="riquadro">`+"\n"+`<h2>Chiudi e invia</h2>`+"\n"+
			`<p class="sotto">Registra l&#39;avviso di ogni busta del lotto e apre il link pubblico per Alfa Orafi. Dopo l&#39;invio il lotto non è più modificabile.</p>`+"\n"+
			`<form id="ready-lot-send-form" action="/ready-lots/`+itoa(l)+`/invia" method="post" data-invio-unico>`+"\n"+
			`<input name="_csrf_token" type="hidden" value="`+b.token+`">`+"\n"+
			`<button id="ready-lot-send-button" type="submit" data-confirm="`+confermaInvio091+`">📤 Chiudi e invia</button>`+"\n"+`</form>`+"\n"+`</section>`)
	nonContiene(t, "con una busta", r.corpo, "ready-lot-send-no-number", "ready-lot-sent")
	// La sezione sta fra le buste e le candidate.
	if i, j := strings.Index(r.corpo, `id="ready-lot-bags"`), strings.Index(r.corpo, `id="ready-lot-send"`); i < 0 || j < i {
		t.Fatal("ordine delle sezioni")
	}
	for _, tel := range []any{nil, "", "da chiedere"} {
		eseguiSQL(t, d, `UPDATE oreficerie SET telefono = ? WHERE id = 2`, tel)
		r = b.vai("GET", percorsoLotto(l), nil, nil)
		contiene(t, "senza numero", r.corpo, `</form>`+"\n"+`<p id="ready-lot-send-no-number" class="lotto-avviso">Alfa Orafi non ha un numero WhatsApp: dopo l&#39;invio scegli tu il contatto. Salva il numero in Anagrafiche per aprire subito la sua chat.</p>`+"\n"+`</section>`)
	}
	// Con la sola busta nel cestino il lotto è vuoto.
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`, uno)
	nonContiene(t, "nel cestino", b.vai("GET", percorsoLotto(l), nil, nil).corpo, "ready-lot-send")
	eseguiSQL(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, uno)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, l)
	nonContiene(t, "inviato", b.vai("GET", percorsoLotto(l), nil, nil).corpo, "ready-lot-send")
}

// inventario: ORE-10
// L'invio: la pagina (200, non un redirect: il testo pronto si vede una volta, come nella LiveView) con «Lotto
// inviato: apri WhatsApp per mandarlo all'oreficeria.», il riquadro «Lotto inviato» (frase, il testo del lotto,
// «Apri WhatsApp» verso il numero dell'oreficeria e «👥 Scegli contatto» verso wa.me/?text=, tutti e due in una scheda
// nuova e con il ritorno alla lista (D-74), il link pubblico con l'indirizzo configurato), il badge «Non più
// modificabile», niente aggiunte, candidate, «Rimuovi» né «Chiudi e invia». La busta è avvisata, con la riga
// all'oreficeria; riaperta la pagina c'è «Questo lotto è partito il …» e non più il testo. Senza token 403.
func TestInviaLottoDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET token = ? WHERE id = ?`, gettoneLotto091, l)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	candidata := prontaDelLotto(t, d, o, "", "Due")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	b.vai("GET", percorsoLotto(l), nil, nil)
	if r := b.vai("POST", percorsoLotto(l)+"/invia", nil, nil); r.codice != 403 {
		t.Fatalf("senza token: %d", r.codice)
	}
	r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
	if r.codice != 200 {
		t.Fatalf("invio: %d %q", r.codice, r.location)
	}
	link := indirizzoPubblicoProva + "/lotto/" + gettoneLotto091
	testo := "Buongiorno Alfa Orafi,\nho 1 riparazioni pronte da farvi ritirare.\nDettagli e foto: " + link + "\n\nNegozio"
	contiene(t, "inviato", r.corpo,
		`<div role="status" class="flash flash-info">Lotto inviato: apri WhatsApp per mandarlo all&#39;oreficeria.</div>`,
		`<span id="ready-lot-closed-badge" class="consegna-confermata">· Non più modificabile</span>`,
		`<section id="ready-lot-sent" class="riquadro lotto-inviato">`+"\n"+`<h2>Lotto inviato</h2>`+"\n"+
			`<p class="sotto">Il testo è pronto: aprilo su WhatsApp e mandalo. L&#39;avviso di ogni busta è già registrato.</p>`+"\n"+
			`<pre id="ready-lot-sent-message" class="lotto-testo">`+testo+`</pre>`+"\n"+
			`<div class="lotto-pulsanti"><a id="ready-lot-sent-whatsapp" href="`+buste.IndirizzoWaMe("+390490000000", testo)+`" target="_blank" rel="noopener" class="pulsante" data-wa-lista>Apri WhatsApp</a>`+"\n"+
			`<a id="ready-lot-sent-choose" href="`+buste.IndirizzoWaMe("", testo)+`" target="_blank" rel="noopener" class="pulsante-contorno" data-wa-lista><span aria-hidden="true">👥</span> Scegli contatto</a>`+"\n"+`</div>`+"\n"+
			`<p class="lotto-link">Link pubblico: <a id="ready-lot-sent-link" href="`+link+`" class="lotto-link">`+link+`</a></p>`+"\n"+`</section>`,
		`<li id="ready-lot-bag-`+itoa(uno)+`" class="consegna-busta"><div><strong>N. 1/26</strong> <span class="sotto-in-linea">— Uno</span></div>`+"\n"+`</li>`)
	nonContiene(t, "inviato", r.corpo, "ready-lot-add-form", "ready-lot-candidates", "Rimuovi", `id="ready-lot-send"`, "ready-lot-gone", "ready-lot-sent-no-number",
		"flash-errore", "ready-lot-candidate-"+itoa(candidata))
	if strings.Index(r.corpo, `id="ready-lot-bags"`) > strings.Index(r.corpo, `id="ready-lot-sent"`) {
		t.Fatal("il riquadro dell'invio sta sopra le buste")
	}
	if v := contaRighe(t, d, `SELECT count(*) FROM buste WHERE id = `+itoa(uno)+` AND avviso_pronta_il IS NOT NULL`); v != 1 {
		t.Fatal("busta non avvisata")
	}
	if v := contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = `+itoa(uno)+` AND destinatario = 'oreficeria' AND indirizzo = '+390490000000' AND testo LIKE '%`+link+`%'`); v != 1 {
		t.Fatal("riga dell'avviso")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'ready_lot.sent' AND attore = 'banco@esempio.it' AND id_risorsa = '`+itoa(l)+`' AND dati = '{"bags":1}'`); n != 1 {
		t.Fatalf("audit: %d", n)
	}
	r = b.vai("GET", percorsoLotto(l), nil, nil)
	contiene(t, "riaperta", r.corpo, `<section id="ready-lot-gone" class="riquadro sotto">Questo lotto è partito il 31/10/2026 10:00. Il link pubblico è <a id="ready-lot-gone-link" href="`+link+`"`)
	nonContiene(t, "riaperta", r.corpo, "ready-lot-sent", "Lotto inviato:", "ready-lot-send")
	// La scheda della busta: l'invio registrato all'oreficeria.
	s := seguiRedirect(t, b, b.vai("GET", percorsoScheda(uno), nil, nil))
	contiene(t, "scheda", s.corpo, `<div class="invio-indirizzo">&#43;390490000000</div>`)
}

// inventario: ORE-10
// Un'oreficeria senza numero: avvisata prima (il riquadro dell'invio), il lotto parte lo stesso, niente «Apri
// WhatsApp», solo «👥 Scegli contatto» e l'avviso «Nessun numero WhatsApp …»; la scheda della busta dice «contatto
// scelto sul telefono», mai il segno salvato.
func TestInviaLottoSenzaNumeroDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	eseguiSQL(t, d, `UPDATE oreficerie SET telefono = NULL WHERE id = 2`)
	eseguiSQL(t, d, `UPDATE lotti_avviso SET token = ? WHERE id = ?`, gettoneLotto091, l)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	contiene(t, "prima", b.vai("GET", percorsoLotto(l), nil, nil).corpo, `id="ready-lot-send-no-number"`)
	r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
	testo := "Buongiorno Alfa Orafi,\nho 1 riparazioni pronte da farvi ritirare.\nDettagli e foto: " + indirizzoPubblicoProva + "/lotto/" + gettoneLotto091 + "\n\nNegozio"
	contiene(t, "dopo", r.corpo, `<div class="lotto-pulsanti"><a id="ready-lot-sent-choose" href="`+buste.IndirizzoWaMe("", testo)+`"`,
		`<span id="ready-lot-sent-no-number" class="lotto-avviso">Nessun numero WhatsApp per questa oreficeria: scegli tu il contatto. Salva il numero in Anagrafiche per la prossima volta.</span>`+"\n"+`</div>`)
	nonContiene(t, "dopo", r.corpo, "ready-lot-sent-whatsapp", "Apri WhatsApp")
	s := seguiRedirect(t, b, b.vai("GET", percorsoScheda(uno), nil, nil))
	contiene(t, "scheda", s.corpo, "contatto scelto sul telefono")
	nonContiene(t, "scheda", s.corpo, "contatto-scelto")
}

// inventario: ORE-10
// I rifiuti ridisegnano la pagina (200) con il lotto riletto e il messaggio d'errore, senza scrivere: lotto vuoto,
// già inviato (con «Questo lotto è partito il …» e senza il pulsante), una busta non più pronta («Busta N. 1/26: non è
// più pronta. Niente è partito.»: il numero visibile, D-99), un testo non valido, l'indirizzo pubblico che manca
// (D-46 estesa). Un lotto che non c'è (o un id non valido) torna alla lista con «Lotto non trovato.».
func TestRifiutiDellInvioDelLotto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	b.vai("GET", percorsoLotto(l), nil, nil)
	invia := func(cosa string, errore string, presenti ...string) string {
		t.Helper()
		r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", cosa, r.codice, r.location)
		}
		contiene(t, cosa, r.corpo, append([]string{`<div role="alert" class="flash flash-errore">` + errore + `</div>`, `<h1>📦 Lotto Alfa Orafi</h1>`}, presenti...)...)
		nonContiene(t, cosa, r.corpo, "ready-lot-sent", "flash-info")
		return r.corpo
	}
	invia("vuoto", "Lotto vuoto: aggiungi almeno una busta.")
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ?, stato = 'in_lavorazione' WHERE id = ?`, l, uno)
	invia("non pronta", "Busta N. 1/26: non è più pronta. Niente è partito.", `id="ready-lot-send-button"`, `id="ready-lot-add-form"`)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, uno)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.template.wa_oreficeria_lotto', 'Lotto {password}', '2026-10-31T09:00:00.000000Z')`)
	invia("testo", "Il testo «Avviso lotto pronte oreficeria» non è valido: Segnaposto non supportato: {password}. Niente è partito.")
	eseguiSQL(t, d, `DELETE FROM impostazioni WHERE chiave = 'outgoing.template.wa_oreficeria_lotto'`)
	if n := contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE inviato_il IS NOT NULL`) + contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita`) +
		contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'ready_%'`); n != 0 {
		t.Fatalf("un rifiuto ha scritto: %d", n)
	}
	eseguiSQL(t, d, `UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:30:00.000000Z' WHERE id = ?`, l)
	invia("già inviato", "Lotto già inviato.", `id="ready-lot-gone"`, "Questo lotto è partito il 31/10/2026 09:30")
	for _, p := range []string{"/ready-lots/9999/invia", "/ready-lots/abc/invia"} {
		r := b.vai("POST", p, modulo(b), nil)
		if !redirectA(r, rottaLotti) {
			t.Fatalf("%s: %d", p, r.codice)
		}
		contiene(t, p, seguiRedirect(t, b, r).corpo, `<div role="alert" class="flash flash-errore">Lotto non trovato.</div>`)
	}
}

// inventario: ORE-10
// Senza indirizzo pubblico (D-46 estesa ai testi con {link}): il lotto non parte e la pagina dice cosa configurare.
func TestInviaLottoSenzaIndirizzoPubblicoDallaPagina(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, "")
	b := dipendenteDentro(t, h)
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	b.vai("GET", percorsoLotto(l), nil, nil)
	r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
	contiene(t, "senza indirizzo", r.corpo, `<div role="alert" class="flash flash-errore">Lotto non inviato: manca «indirizzo_pubblico = https://host[:porta]» in `,
		` Niente è partito.</div>`, `id="ready-lot-send-button"`)
	if contaRighe(t, d, `SELECT count(*) FROM lotti_avviso WHERE inviato_il IS NOT NULL`) != 0 {
		t.Fatal("inviato")
	}
}

// inventario: ORE-10
// Anche il titolare invia (personale).
func TestInviaLottoDelTitolare(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := nuovoBrowser(t, h)
	if r := b.prova("titolare@esempio.it", pwProva); !redirectA(r, "/") {
		t.Fatalf("accesso: %d", r.codice)
	}
	l := lottoConOreficeria(t, d)
	uno := prontaDelLotto(t, d, o, "", "Uno")
	eseguiSQL(t, d, `UPDATE buste SET lotto_avviso_id = ? WHERE id = ?`, l, uno)
	b.vai("GET", percorsoLotto(l), nil, nil)
	if r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil); r.codice != 200 || !strings.Contains(r.corpo, `id="ready-lot-sent"`) {
		t.Fatalf("titolare: %d", r.codice)
	}
}

// lottoNonInviabile fa fallire l'invio.
type lottoNonInviabile struct{ Buste }

func (lottoNonInviabile) InviaLotto(context.Context, int64, string, audit.Contesto) (buste.LottoInviato, error) {
	return buste.LottoInviato{}, errLottiRotti
}

// Un guasto del database nell'invio è la pagina d'errore con la riga nel registro.
func TestInviaLottoGuasto(t *testing.T) {
	var reg bytes.Buffer
	h, d := facciaConBusteCon(t, &reg, func(b Buste) Buste { return lottoNonInviabile{b} })
	l := lottoConOreficeria(t, d)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoLotto(l), nil, nil)
	r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
	if r.codice != http.StatusInternalServerError {
		t.Fatalf("guasto: %d", r.codice)
	}
	contiene(t, "guasto", reg.String(), "lotto non inviato")
}

// inventario: ORE-10
// D-74: con JavaScript il tocco su «Apri WhatsApp» o «👥 Scegli contatto» del lotto inviato apre WhatsApp nella scheda
// nuova (target=_blank, il tocco non si ferma) e porta questa pagina alla lista ricordata.
func TestScriptAperturaWhatsAppDelLotto(t *testing.T) {
	b, _ := dentroCome(t, accounts.Titolare, nil)
	js := assetDellaPagina(t, b, b.vai("GET", "/", nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		"    const apertura = event.target.closest && event.target.closest(\"a[data-wa-lista]\")\n    if (!apertura) return\n" +
			"    setTimeout(() => { window.location.href = lista() }, 0)",
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}

// lottoInviatoSotto è un invio che arriva dopo un altro: segna il lotto inviato (come un collega un attimo prima) e
// rifiuta con «Lotto già inviato.».
type lottoInviatoSotto struct {
	Buste
	d **db.DB
}

func (l lottoInviatoSotto) InviaLotto(_ context.Context, id int64, _ string, _ audit.Contesto) (buste.LottoInviato, error) {
	if _, err := (*l.d).Scrittura.Exec(`UPDATE lotti_avviso SET inviato_il = '2026-10-31T08:45:00.000000Z' WHERE id = ?`, id); err != nil {
		return buste.LottoInviato{}, err
	}
	return buste.LottoInviato{}, buste.ErrLottoGiaInviato
}

// inventario: ORE-10
// Un rifiuto ridisegna la pagina con il lotto **riletto** (reload_lot/1): un lotto inviato da un collega mentre la
// pagina era aperta smette di offrire il pulsante e dice quando è partito.
func TestRifiutoDellInvioRileggeIlLotto(t *testing.T) {
	var dd *db.DB
	h, d := facciaConBusteCon(t, nil, func(b Buste) Buste { return lottoInviatoSotto{b, &dd} })
	dd = d
	l := lottoConOreficeria(t, d)
	b := dipendenteDentro(t, h)
	b.vai("GET", percorsoLotto(l), nil, nil)
	r := b.vai("POST", percorsoLotto(l)+"/invia", modulo(b), nil)
	contiene(t, "riletto", r.corpo, `<div role="alert" class="flash flash-errore">Lotto già inviato.</div>`,
		"Questo lotto è partito il 31/10/2026 09:45", "ready-lot-closed-badge")
	nonContiene(t, "riletto", r.corpo, "ready-lot-send-button", "ready-lot-add-form")
}
