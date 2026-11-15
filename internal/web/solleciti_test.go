package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// I solleciti all'artigiano (solleciti.go, slice 083, ART-20, ART-21, ART-22, ART-07 contatore), come
// ArtisanRemindersLive, il contatore di HandoverLive e #repair-artisan-reminders di RepairLive (302439d): i casi
// di artisan_reminders_live_test.exs senza LiveView, con la D-64 per il numero. Nomi e numeri inventati;
// l'orologio è fermo al 31/10/2026, le 10:00 in negozio.

const rottaSollecitiProva = "/handovers/reminders"

type sollecitiDiProva struct {
	anello, collana, bracciale, vicina int64
	numCollana, numBracciale           string
}

// scriviSolleciti: «Bottega Due» (con telefono) ha l'anello (etichetta 0045, 21 giorni) e la collana (15 giorni),
// «Orafo <Uno>» (senza telefono) il bracciale (30 giorni) e una busta uscita da 6 giorni.
func scriviSolleciti(t *testing.T, h *provaRientro) sollecitiDiProva {
	t.Helper()
	eseguiSQL(t, h.d, `INSERT INTO artigiani (id, nome, telefono) VALUES (1, 'Orafo <Uno>', NULL), (2, 'Bottega Due', '+39 333 1234567')`)
	uno := consegnaProva(t, h.d, 1, "2026-09-20T08:00:00.000000Z", "")
	due := consegnaProva(t, h.d, 2, "2026-10-01T08:00:00.000000Z", "")
	var s sollecitiDiProva
	s.anello, _ = bustaConsegna(t, h.d, h.o, "RIP-2026-0045", "Anello con una descrizione lunga che supera i cinquanta caratteri di sicuro")
	s.collana, s.numCollana = bustaConsegna(t, h.d, h.o, "", "Collana")
	s.bracciale, s.numBracciale = bustaConsegna(t, h.d, h.o, "", "Bracciale")
	s.vicina, _ = bustaConsegna(t, h.d, h.o, "", "Spilla")
	fuori := func(id, consegna int64, il string) {
		eseguiSQL(t, h.d, `UPDATE buste SET consegna_artigiano_id = ?, consegnata_artigiano_il = ? WHERE id = ?`, consegna, il, id)
	}
	fuori(s.anello, due, "2026-10-10T09:00:00.000000Z")
	fuori(s.collana, due, "2026-10-16T08:00:00.000000Z")
	fuori(s.bracciale, uno, "2026-10-01T09:00:00.000000Z")
	fuori(s.vicina, uno, "2026-10-25T09:00:00.000000Z")
	return s
}

func moduliSollecito(token, artigiano, giorni string) string {
	return `<form id="reminder-choose-form-` + artigiano + `" action="/handovers/reminders" method="post" data-channel="whatsapp" data-avviso-invio><input name="_csrf_token" type="hidden" value="` + token +
		`"><input type="hidden" name="artigiano" value="` + artigiano + `"><input type="hidden" name="giorni" value="` + giorni + `"><input type="hidden" name="wa" value="scegli"><button id="reminder-choose-` + artigiano +
		`" type="submit" class="pulsante-contorno"><span aria-hidden="true">👥</span> Scegli contatto</button></form>` + "\n" +
		`<form id="record-reminder-form-` + artigiano + `" action="/handovers/reminders" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="` + token +
		`"><input type="hidden" name="artigiano" value="` + artigiano + `"><input type="hidden" name="giorni" value="` + giorni + `"><button id="record-reminder-` + artigiano +
		`" type="submit" data-confirm="Registrare il sollecito per queste buste?">⏰ Registra sollecito</button></form>`
}

// inventario: ART-20
// La pagina per il personale: ritorno alle consegne e allo storico, la soglia (parametro, impostazione, 14; un
// parametro che non è un numero è 14; zero è 1) nel modulo GET, le buste in ritardo per artigiano della consegna
// per nome, con il telefono, i giorni fuori e il numero della D-64; «📱 WhatsApp sollecito» solo con un numero,
// altrimenti l'avviso; vuota «Nessun sollecito pendente. Tutto in regola. ✓». Ogni visita scrive
// handover.reminders_viewed con soglia e conteggio.
func TestPaginaSolleciti(t *testing.T) {
	h := nuovaProvaBuste(t)
	dip := dipendenteDentro(t, h.h)
	r := dip.vai("GET", rottaSollecitiProva, nil, nil)
	if r.codice != 200 {
		t.Fatalf("vuota: %d", r.codice)
	}
	contiene(t, "vuota", r.corpo, "<title>Solleciti pendenti",
		`<nav class="gs-back-bar" aria-label="Ritorno"><a id="artisan-reminders-back" href="/handovers" class="gs-back-btn">← Indietro</a> <a href="/handovers/history">📋 Storico consegne</a></nav>`,
		`<h1>⏰ Solleciti pendenti</h1>`+"\n"+`<p class="sotto">Buste ancora dall'artigiano da almeno 14 giorni. Il sollecito registrato resta nella storia della busta; l'invio del messaggio non passa da qui.</p>`,
		`<form id="artisan-reminders-threshold" method="get" action="/handovers/reminders" class="riquadro solleciti-soglia">`+"\n"+
			`<label><span>Fuori da almeno (giorni)</span> <input id="giorni" type="number" name="giorni" value="14" min="1" max="120"></label>`+"\n"+`<button type="submit">Aggiorna</button>`+"\n"+`</form>`,
		`<p id="artisan-reminders-empty" class="riquadro sotto solleciti-vuoto">Nessun sollecito pendente. Tutto in regola. ✓</p>`)
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.reminders_viewed|index|ok|{"count":0,"days":14}` {
		t.Fatal(got)
	}
	s := scriviSolleciti(t, h)
	r = dip.vai("GET", rottaSollecitiProva+"?giorni=abc", nil, nil)
	contiene(t, "piena", r.corpo, `da almeno 14 giorni`, `name="giorni" value="14"`,
		`<section id="artisan-reminder-group-2" class="riquadro">`+"\n"+`<div class="solleciti-testa"><div>`+"\n"+`<h2>🔧 Bottega Due</h2>`+"\n"+
			`<p class="sotto">2 buste fuori bottega <span>· &#43;39 333 1234567</span></p>`+"\n"+`</div>`+"\n"+`<div class="solleciti-azioni">`+"\n"+
			`<form id="reminder-whatsapp-form-2" action="/handovers/reminders" method="post" data-channel="whatsapp" data-avviso-invio><input name="_csrf_token" type="hidden" value="`+dip.token+
			`"><input type="hidden" name="artigiano" value="2"><input type="hidden" name="giorni" value="abc"><input type="hidden" name="wa" value="numero"><button id="reminder-whatsapp-2" type="submit">📱 WhatsApp sollecito</button></form>`+"\n"+
			moduliSollecito(dip.token, "2", "abc")+"\n"+`</div></div>`+"\n"+`<ul class="solleciti-buste">`+"\n"+
			`<li id="artisan-reminder-bag-`+itoa(s.anello)+`"><div><a href="/repairs/`+itoa(s.anello)+`" class="forte">#0045/26</a>`+"\n"+
			`<div class="sotto">Anello con una descrizione lunga che supera …</div></div> <span class="sotto-in-linea">21gg</span></li>`+"\n"+
			`<li id="artisan-reminder-bag-`+itoa(s.collana)+`"><div><a href="/repairs/`+itoa(s.collana)+`" class="forte">N. `+s.numCollana+`</a>`+"\n"+
			`<div class="sotto">Collana</div></div> <span class="sotto-in-linea">15gg</span></li>`+"\n"+`</ul>`+"\n"+`</section>`,
		`<section id="artisan-reminder-group-1" class="riquadro">`+"\n"+`<div class="solleciti-testa"><div>`+"\n"+`<h2>🔧 Orafo &lt;Uno&gt;</h2>`+"\n"+
			`<p class="sotto">1 busta fuori bottega</p>`+"\n"+
			`<p id="reminder-no-number-1" class="sotto testo-avviso">Telefono artigiano non impostato: scegli tu il contatto. Salva il numero in Anagrafiche per la prossima volta.</p>`+"\n"+
			`</div>`+"\n"+`<div class="solleciti-azioni">`+"\n"+moduliSollecito(dip.token, "1", "abc"))
	nonContiene(t, "piena", r.corpo, "artisan-reminders-empty", "reminder-whatsapp-form-1", "artisan-reminder-bag-"+itoa(s.vicina)+`"`, "artisan-reminder-last-", "Sollecitata il")
	if strings.Index(r.corpo, "artisan-reminder-group-2") > strings.Index(r.corpo, "artisan-reminder-group-1") {
		t.Fatal("i gruppi vanno per nome dell'artigiano")
	}
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.reminders_viewed|index|ok|{"count":3,"days":14}` {
		t.Fatal(got)
	}
	// Soglie diverse: 5 giorni prende anche la busta di 6; zero è 1; un'impostazione vale senza parametro, non
	// con un parametro che non è un numero.
	r = dip.vai("GET", rottaSollecitiProva+"?giorni=5", nil, nil)
	contiene(t, "5", r.corpo, `name="giorni" value="5"`, `<p class="sotto">2 buste fuori bottega</p>`, `id="artisan-reminder-bag-`+itoa(s.vicina)+`"`, `<span class="sotto-in-linea">6gg</span>`)
	if got := ultimaRiga(t, h.d); got != `banco@esempio.it|handover.reminders_viewed|index|ok|{"count":4,"days":5}` {
		t.Fatal(got)
	}
	contiene(t, "0", dip.vai("GET", rottaSollecitiProva+"?giorni=0", nil, nil).corpo, "da almeno 1 giorni", `name="giorni" value="1"`)
	eseguiSQL(t, h.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('solleciti_artigiano_giorni', '20', '2026-10-31T09:00:00.000000Z')`)
	r = dip.vai("GET", rottaSollecitiProva, nil, nil)
	contiene(t, "impostazione", r.corpo, "da almeno 20 giorni", "artisan-reminder-bag-"+itoa(s.anello)+`"`, "artisan-reminder-bag-"+itoa(s.bracciale)+`"`)
	nonContiene(t, "impostazione", r.corpo, "artisan-reminder-bag-"+itoa(s.collana)+`"`)
	contiene(t, "impostazione e testo", dip.vai("GET", rottaSollecitiProva+"?giorni=%2B", nil, nil).corpo, "da almeno 14 giorni")
	contiene(t, "impostazione e vuoto", dip.vai("GET", rottaSollecitiProva+"?giorni=", nil, nil).corpo, "da almeno 20 giorni")
	// Il titolare la vede uguale.
	if r := titolareDentro(t, h.h).vai("GET", rottaSollecitiProva, nil, nil); r.codice != 200 || !strings.Contains(r.corpo, "artisan-reminder-group-2") {
		t.Fatalf("titolare: %d", r.codice)
	}
}

// inventario: ART-07
// Il contatore «⏰ Solleciti pendenti» della lista consegne: le buste in ritardo con la soglia dell'impostazione
// (14 senza), solo sopra zero.
func TestContatoreSolleciti(t *testing.T) {
	h := nuovaProvaBuste(t)
	dip := dipendenteDentro(t, h.h)
	link := `<a id="artisan-reminders-link" href="/handovers/reminders">⏰ Solleciti pendenti`
	r := dip.vai("GET", rottaConsegne, nil, nil)
	contiene(t, "zero", r.corpo, link+`</a>`)
	nonContiene(t, "zero", r.corpo, "artisan-reminders-count")
	scriviSolleciti(t, h)
	contiene(t, "tre", dip.vai("GET", rottaConsegne, nil, nil).corpo, link+` <span id="artisan-reminders-count" class="contatore-solleciti">3</span></a>`)
	eseguiSQL(t, h.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('solleciti_artigiano_giorni', '20', '2026-10-31T09:00:00.000000Z')`)
	contiene(t, "due", dip.vai("GET", rottaConsegne, nil, nil).corpo, link+` <span id="artisan-reminders-count" class="contatore-solleciti">2</span></a>`)
	eseguiSQL(t, h.d, `UPDATE impostazioni SET valore = '31' WHERE chiave = 'solleciti_artigiano_giorni'`)
	nonContiene(t, "nessuna", dip.vai("GET", rottaConsegne, nil, nil).corpo, "artisan-reminders-count")
}

// inventario: ART-21, ART-22, RIP-S-17
// «⏰ Registra sollecito»: con il parametro della pagina, una riga e un audit per busta, poi la stessa pagina con
// «Sollecito registrato per N buste.», l'ultimo sollecito del gruppo e di ogni busta, senza una lettura nuova
// nel registro (in LiveView l'evento non è una visita); la visita dopo sì. Nessuna busta o un artigiano illeggibile
// è «Nessuna busta da sollecitare per questo artigiano.» senza scrivere. Senza token il 403. La scheda mostra i
// solleciti della busta, dal più recente, anche dopo il rientro.
func TestRegistraSollecito(t *testing.T) {
	h := nuovaProvaBuste(t)
	dip := dipendenteDentro(t, h.h)
	s := scriviSolleciti(t, h)
	viste := func() int {
		return contaRighe(t, h.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'handover.reminders_viewed'`)
	}
	scritti := func() int { return contaRighe(t, h.d, `SELECT count(*) FROM solleciti_artigiani`) }
	dip.vai("GET", rottaSollecitiProva+"?giorni=abc", nil, nil)
	prima := viste()
	r := dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"2"}, "giorni": {"abc"}}, nil)
	if r.codice != 302 || r.location != rottaSollecitiProva+"?giorni=abc" {
		t.Fatalf("sollecito: %d %s", r.codice, r.location)
	}
	if scritti() != 2 || contaRighe(t, h.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.artisan_reminded' AND attore = 'banco@esempio.it'`) != 2 {
		t.Fatal("righe", scritti())
	}
	r = seguiRedirect(t, dip, r)
	contiene(t, "dopo", r.corpo, "Sollecito registrato per 2 buste.",
		`<p id="artisan-reminder-last-2" class="sotto">Ultimo sollecito: 31/10/2026 10:00</p>`,
		`<div class="sotto">Collana</div>`+"\n"+`<div class="sotto">Sollecitata il 31/10/2026 10:00</div></div> <span class="sotto-in-linea">15gg</span></li>`)
	nonContiene(t, "dopo", r.corpo, "artisan-reminder-last-1")
	if viste() != prima {
		t.Fatal("la pagina dopo il sollecito non è una visita")
	}
	dip.vai("GET", rottaSollecitiProva, nil, nil)
	if viste() != prima+1 {
		t.Fatal("la visita dopo sì")
	}
	// Una busta: «1 busta»; senza parametro la pagina torna senza.
	r = dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"1"}, "giorni": {""}}, nil)
	if r.location != rottaSollecitiProva {
		t.Fatal(r.location)
	}
	contiene(t, "una", seguiRedirect(t, dip, r).corpo, "Sollecito registrato per 1 busta.")
	// I rifiuti: niente scritto, la pagina con il messaggio e il parametro.
	for _, a := range []string{"", "x", "0", "99"} {
		prima := viste()
		r := dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {a}, "giorni": {"5"}}, nil)
		if r.codice != 302 || r.location != rottaSollecitiProva+"?giorni=5" || scritti() != 3 {
			t.Fatalf("rifiuto %q: %d %s %d", a, r.codice, r.location, scritti())
		}
		contiene(t, "rifiuto", seguiRedirect(t, dip, r).corpo, "Nessuna busta da sollecitare per questo artigiano.")
		if viste() != prima {
			t.Fatal("la pagina dopo il rifiuto non è una visita")
		}
	}
	if r := dip.vai("POST", rottaSollecitiProva, url.Values{"artigiano": {"2"}}, nil); r.codice != 403 || scritti() != 3 {
		t.Fatalf("senza token: %d", r.codice)
	}
	// La scheda: i solleciti della busta dal più recente, anche rientrata; una busta mai sollecitata non ha il riquadro.
	tit := titolareDentro(t, h.h)
	tit.vai("GET", rottaSollecitiProva, nil, nil)
	h.o.Sposta(26 * 60 * 60 * 1e9)
	if r := tit.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {tit.token}, "artigiano": {"2"}}, nil); r.codice != 302 {
		t.Fatal(r.codice)
	}
	eseguiSQL(t, h.d, `UPDATE buste SET rientrata_artigiano_il = '2026-11-01T12:00:00.000000Z' WHERE id = ?`, s.collana)
	r = dip.vai("GET", percorsoScheda(s.collana), nil, nil)
	contiene(t, "scheda", r.corpo, `<div id="repair-artisan-reminders" class="riquadro-giri">`+"\n"+`<strong>⏰ Solleciti all'artigiano</strong>`+"\n"+`<ul>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">01/11/2026 12:00</span> <span class="sotto-in-linea">fuori da 16gg</span></div>`+"\n"+`<div class="sotto">titolare@esempio.it</div></li>`+"\n"+
		`<li><div class="giro-testa"><span class="forte">31/10/2026 10:00</span> <span class="sotto-in-linea">fuori da 15gg</span></div>`+"\n"+`<div class="sotto">banco@esempio.it</div></li>`+"\n"+`</ul>`+"\n"+`</div>`)
	nonContiene(t, "mai sollecitata", dip.vai("GET", percorsoScheda(s.vicina), nil, nil).corpo, "repair-artisan-reminders")
	// Un attore NULL (riga importata): nessuna riga dell'attore.
	eseguiSQL(t, h.d, `UPDATE solleciti_artigiani SET attore = NULL WHERE busta_id = ?`, s.bracciale)
	contiene(t, "senza attore", dip.vai("GET", percorsoScheda(s.bracciale), nil, nil).corpo,
		`<li><div class="giro-testa"><span class="forte">31/10/2026 10:00</span> <span class="sotto-in-linea">fuori da 30gg</span></div></li>`)
}

// inventario: ART-21
// «📱 WhatsApp sollecito» e «👥 Scegli contatto» registrano lo stesso sollecito e portano a wa.me con il testo del
// Flask (il numero dei testi della D-64, le buste rilette nella scrittura): senza JavaScript il redirect, con
// Accept: application/json il JSON {"url": …} senza cache (D-74: app.js manda la scheda a wa.me e la pagina alla
// lista). Un rifiuto è la pagina con il messaggio anche con il JSON.
func TestSollecitoWhatsApp(t *testing.T) {
	h := nuovaProvaBuste(t)
	dip := dipendenteDentro(t, h.h)
	s := scriviSolleciti(t, h)
	dip.vai("GET", rottaSollecitiProva, nil, nil)
	testo := "Ciao Bottega Due, ti ricordo che hai ancora 2 buste fuori bottega. Buste: 0045/26, " + s.numCollana
	voluto := buste.IndirizzoWaMe("+393331234567", testo)
	if !strings.HasPrefix(voluto, "https://wa.me/%2B393331234567?text=Ciao%20Bottega%20Due%2C%20ti%20ricordo") {
		t.Fatal(voluto)
	}
	r := dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"2"}, "giorni": {"abc"}, "wa": {"numero"}}, nil)
	if r.codice != 302 || r.location != voluto || contaRighe(t, h.d, `SELECT count(*) FROM solleciti_artigiani`) != 2 {
		t.Fatalf("senza JavaScript: %d %s", r.codice, r.location)
	}
	r = dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"2"}, "wa": {"numero"}}, map[string]string{"Accept": "application/json"})
	var j map[string]string
	if err := json.Unmarshal([]byte(r.corpo), &j); err != nil || len(j) != 1 || j["url"] != voluto {
		t.Fatalf("JSON: %s %v", r.corpo, err)
	}
	if r.codice != 200 || r.h.Get("Cache-Control") != "no-store" || r.h.Get("Content-Type") != "application/json" {
		t.Fatalf("JSON: %d %s %v", r.codice, r.corpo, r.h)
	}
	// «Scegli contatto»: senza numero; per l'artigiano senza telefono è l'unico.
	r = dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"1"}, "wa": {"scegli"}}, nil)
	if want := buste.IndirizzoWaMe("", "Ciao Orafo <Uno>, ti ricordo che hai ancora 1 buste fuori bottega. Buste: "+s.numBracciale); r.location != want || !strings.HasPrefix(want, "https://wa.me/?text=") {
		t.Fatalf("scegli: %s", r.location)
	}
	// «📱» per l'artigiano senza telefono: nessun numero, come «Scegli contatto».
	r = dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"1"}, "wa": {"numero"}}, nil)
	if !strings.HasPrefix(r.location, "https://wa.me/?text=") {
		t.Fatalf("numero vuoto: %s", r.location)
	}
	if n := contaRighe(t, h.d, `SELECT count(*) FROM solleciti_artigiani`); n != 6 {
		t.Fatal("solleciti", n)
	}
	if n := contaRighe(t, h.d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatal("nessuna riga nella coda in uscita", n)
	}
	r = dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"7"}, "wa": {"numero"}}, map[string]string{"Accept": "application/json"})
	if r.codice != 302 || r.location != rottaSollecitiProva {
		t.Fatalf("rifiuto: %d %s", r.codice, r.location)
	}
	contiene(t, "rifiuto", seguiRedirect(t, dip, r).corpo, "Nessuna busta da sollecitare per questo artigiano.")
}

// busteLetturaGuasta fa fallire SollecitiPendenti finché guasta è vero.
type busteLetturaGuasta struct {
	*buste.Buste
	guasta bool
}

func (b *busteLetturaGuasta) SollecitiPendenti(ctx context.Context, giorni string) (buste.Solleciti, error) {
	if b.guasta {
		return buste.Solleciti{}, errors.New("lettura guasta")
	}
	return b.Buste.SollecitiPendenti(ctx, giorni)
}

// inventario: ART-20
// Il segno della pagina dopo un sollecito si toglie all'inizio della pagina: se la lettura dei solleciti non
// riesce (pagina d'errore), la visita dopo scrive comunque handover.reminders_viewed.
func TestSegnoDopoSollecitoConLetturaGuasta(t *testing.T) {
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
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	g := &busteLetturaGuasta{Buste: buste.Nuove(d, o)}
	dd.Buste = g
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	h := &provaRientro{mustInterna(t, dd), d, o}
	scriviSolleciti(t, h)
	dip := dipendenteDentro(t, h.h)
	dip.vai("GET", rottaSollecitiProva, nil, nil)
	viste := func() int {
		return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'handover.reminders_viewed'`)
	}
	prima := viste()
	r := dip.vai("POST", rottaSollecitiProva, url.Values{"_csrf_token": {dip.token}, "artigiano": {"2"}}, nil)
	if r.codice != 302 {
		t.Fatal(r.codice)
	}
	g.guasta = true
	if r := dip.vai("GET", rottaSollecitiProva, nil, nil); r.codice != 500 {
		t.Fatalf("lettura guasta: %d", r.codice)
	}
	g.guasta = false
	dip.vai("GET", rottaSollecitiProva, nil, nil)
	if viste() != prima+1 {
		t.Fatalf("la visita dopo la pagina d'errore non è stata registrata: %d → %d", prima, viste())
	}
}
