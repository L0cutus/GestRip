package web

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il riquadro del link pubblico sulla scheda (RIP-S-24, CLI-23, CLI-24, CLI-25), come la sezione
// #repair-public-link e gli eventi revoke-link e regenerate-link di RepairLive dello standalone
// (go-baseline), su account e buste veri. I casi da repair_public_link_live_test.exs e
// repair_public_link_owner_test.exs.

const (
	confermaRevoca       = "Revocare il link? Il cliente non potrà più aprire la pagina con questo link. La cronologia della busta resta."
	confermaRevocaCodice = "Revocare il link? Il cliente non potrà più aprire la pagina, né con questo link né con il vecchio link con codice. La cronologia della busta resta."
	confermaNuovo        = "Generare un nuovo link? Il link attuale smette di funzionare."
)

func azioneRevoca(id int64) string { return percorsoScheda(id) + "/revoca-link" }
func azioneNuovo(id int64) string  { return percorsoScheda(id) + "/nuovo-link" }

var reEsitoScheda = regexp.MustCompile(`^/repairs/(\d+)\?esito=([A-Za-z0-9_-]{22})&mostra=[A-Za-z0-9_-]{22}$`)

// statoLink è la riga #repair-public-link-state della pagina.
func statoLink(t *testing.T, corpo string) string {
	t.Helper()
	m := regexp.MustCompile(`<p id="repair-public-link-state"[^>]*>(.*?)</p>`).FindStringSubmatch(corpo)
	if m == nil {
		t.Fatal("manca #repair-public-link-state")
	}
	return m[1]
}

func tokenDi(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var s string
	if err := d.Lettura.QueryRow(`SELECT coalesce(token_pubblico, '') FROM buste WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func azioniLink(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.public_link_revoked', 'repair.public_link_issued')`)
}

func postLink(b *browser, percorso string) risposta {
	return b.vai("POST", percorso, url.Values{"_csrf_token": {b.token}}, nil)
}

// inventario: RIP-S-24, CLI-23
// Il titolare vede la riga «🔗 Link cliente attivo fino al <giorno>» (90 giorni dalla busta) e il
// menu «⋯» con «Revoca link» e «Genera nuovo link», ognuno con la sua conferma; il token salvato non
// compare mai, nemmeno con il modulo della modifica aperto, e niente «mostrato una volta».
func TestPaginaRiquadroLink(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	sid := strconv.FormatInt(id, 10)
	for _, p := range []string{percorsoScheda(id), percorsoScheda(id) + "?modifica=1"} {
		r := b.vai("GET", p, nil, nil)
		contiene(t, p, r.corpo,
			`<section id="repair-public-link" class="riquadro riquadro-link">`,
			`<p id="repair-public-link-state" class="stato-link">🔗 Link cliente attivo fino al 29/01/2027</p>`,
			`<details id="repair-public-link-menu" class="menu-link" data-close-outside>`+"\n"+`<summary aria-label="Azioni sul link cliente">⋯</summary>`,
			`<form action="/repairs/`+sid+`/revoca-link" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><input name="mostra" type="hidden" value="`+b.segno+`">`+
				`<button id="repair-public-link-revoke" type="submit" data-confirm="`+confermaRevoca+`">Revoca link</button></form>`,
			`<form action="/repairs/`+sid+`/nuovo-link" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><input name="mostra" type="hidden" value="`+b.segno+`">`+
				`<button id="repair-public-link-regenerate" type="submit" data-confirm="`+confermaNuovo+`">Genera nuovo link</button></form>`)
		nonContiene(t, p, r.corpo, tokenDi(t, d, id), `id="repair-public-link-new"`, "mostrato una volta")
	}
	// Il riquadro viene dopo i dati, come nello standalone.
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	if strings.Index(r.corpo, `id="repair-public-link"`) < strings.Index(r.corpo, `id="repair-detail"`) {
		t.Error("il riquadro del link viene prima dei dati")
	}
}

// inventario: RIP-S-24, CLI-23
// I testi dello stato: la scadenza è il giorno del negozio (22:30 UTC di luglio è già il giorno
// dopo); solo impronta, con «Revoca link»; scaduto e revocato senza «Revoca link» ma con «Genera
// nuovo link»; «· inviato il <giorno>»; su una busta di un'oreficeria «Link dell'oreficeria».
func TestPaginaStatiDelLink(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria Esempio')`)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	stato := func() string { return statoLink(t, b.vai("GET", percorsoScheda(id), nil, nil).corpo) }

	eseguiSQL(t, d, `UPDATE buste SET token_pubblico_scade_il = '2027-07-15T22:30:00.000000Z' WHERE id = ?`, id)
	if s := stato(); s != "🔗 Link cliente attivo fino al 16/07/2027" {
		t.Errorf("giorno del negozio: %q", s)
	}
	eseguiSQL(t, d, `UPDATE buste SET token_pubblico = NULL WHERE id = ?`, id)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	if s := statoLink(t, r.corpo); s != "🔗 Link cliente attivo fino al 16/07/2027, emesso prima dei link stabili: al prossimo invio ne nasce uno nuovo" {
		t.Errorf("solo impronta: %q", s)
	}
	contiene(t, "solo impronta", r.corpo, `id="repair-public-link-revoke"`)

	eseguiSQL(t, d, `UPDATE buste SET token_pubblico_scade_il = '2026-10-31T09:00:00.000000Z', token_pubblico = 'gettone-finto' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	if s := statoLink(t, r.corpo); s != "🔗 Link cliente scaduto: al prossimo invio ne nasce uno nuovo" {
		t.Errorf("scaduto: %q", s)
	}
	contiene(t, "scaduto", r.corpo, `id="repair-public-link-regenerate"`)
	nonContiene(t, "scaduto", r.corpo, `id="repair-public-link-revoke"`, "gettone-finto")

	eseguiSQL(t, d, `UPDATE buste SET token_pubblico_scade_il = '2027-07-15T22:30:00.000000Z', token_pubblico_revocato_il = '2026-10-30T09:00:00.000000Z' WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	if s := statoLink(t, r.corpo); s != "🔗 Link cliente revocato: al prossimo invio ne nasce uno nuovo" {
		t.Errorf("revocato: %q", s)
	}
	nonContiene(t, "revocato", r.corpo, `id="repair-public-link-revoke"`)

	eseguiSQL(t, d, `UPDATE buste SET token_pubblico_revocato_il = NULL, link_inviato_il = '2026-10-30T23:30:00.000000Z', oreficeria_id = 2 WHERE id = ?`, id)
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	if s := statoLink(t, r.corpo); s != "🔗 Link dell&#39;oreficeria attivo fino al 16/07/2027<span> · inviato il 31/10/2026</span>" {
		t.Errorf("oreficeria e inviato: %q", s)
	}
	contiene(t, "oreficeria", r.corpo, `<summary aria-label="Azioni sul link dell&#39;oreficeria">⋯</summary>`)
}

// inventario: RIP-S-24, CLI-24, ACC-05
// Al dipendente la riga dello stato sì, il menu no (né «Revoca link» né «Genera nuovo link»); i due
// POST forgiati tornano alla scheda con il messaggio dello standalone e non scrivono niente.
func TestPaginaLinkDelDipendente(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "dipendente", r.corpo, "🔗 Link cliente attivo fino al 29/01/2027")
	nonContiene(t, "dipendente", r.corpo, `id="repair-public-link-menu"`, `id="repair-public-link-revoke"`, `id="repair-public-link-regenerate"`)

	token, audit := tokenDi(t, d, id), azioniLink(t, d)
	for percorso, msg := range map[string]string{
		azioneRevoca(id): "Solo il titolare può revocare il link.",
		azioneNuovo(id):  "Solo il titolare può generare un nuovo link.",
	} {
		b.vai("GET", percorsoScheda(id), nil, nil)
		if r := postLink(b, percorso); !redirectAllaScheda(r, id) {
			t.Fatalf("%s: %d %q", percorso, r.codice, r.location)
		}
		r := b.vai("GET", percorsoScheda(id), nil, nil)
		contiene(t, percorso, r.corpo, `<div role="alert" class="flash flash-errore">`+msg+`</div>`)
		nonContiene(t, percorso, r.corpo, "mostrato una volta")
	}
	if tokenDi(t, d, id) != token || azioniLink(t, d) != audit || colonna(t, d, id, "token_pubblico_revocato_il") != "∅" {
		t.Error("il dipendente ha cambiato il link")
	}
}

// inventario: RIP-S-24, CLI-24
// «Revoca link» dal titolare: torna alla scheda con «Link cliente revocato.», la riga dice
// «revocato…», «Revoca link» non c'è più e «Genera nuovo link» sì; il token salvato è vuoto e la
// riga d'audit è del titolare. Una busta che non c'è porta alla home.
func TestPaginaRevocaLink(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria Esempio')`)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := postLink(b, azioneRevoca(id)); !redirectAllaScheda(r, id) {
		t.Fatalf("revoca: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "revocato", r.corpo, `<div role="status" class="flash flash-info">Link cliente revocato.</div>`,
		"🔗 Link cliente revocato: al prossimo invio ne nasce uno nuovo", `id="repair-public-link-regenerate"`)
	nonContiene(t, "revocato", r.corpo, `id="repair-public-link-revoke"`)
	if tokenDi(t, d, id) != "" {
		t.Error("token rimasto")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_revoked' AND attore = 'titolare@esempio.it'`); n != 1 {
		t.Errorf("audit %d", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`); n != 0 {
		t.Errorf("la revoca ha emesso %d link", n)
	}

	altra := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	b.vai("GET", percorsoScheda(altra), nil, nil)
	postLink(b, azioneRevoca(altra))
	contiene(t, "oreficeria", b.vai("GET", percorsoScheda(altra), nil, nil).corpo,
		`<div role="status" class="flash flash-info">Link dell&#39;oreficeria revocato.</div>`)

	for _, p := range []string{azioneRevoca(altra + 100), azioneNuovo(altra + 100), "/repairs/0/revoca-link", "/repairs/x/nuovo-link"} {
		b.vai("GET", percorsoScheda(id), nil, nil)
		if r := postLink(b, p); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", p, r.codice, r.location)
		}
		contiene(t, p, b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}

	// Un guasto del database: «Link cliente non revocato.» sulla scheda.
	eseguiSQL(t, d, `CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.public_link_revoked' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b.vai("GET", percorsoScheda(altra), nil, nil)
	if r := postLink(b, azioneRevoca(altra)); !redirectAllaScheda(r, altra) {
		t.Fatalf("guasto: %d %q", r.codice, r.location)
	}
	contiene(t, "guasto", b.vai("GET", percorsoScheda(altra), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Link dell&#39;oreficeria non revocato.</div>`)
	eseguiSQL(t, d, `CREATE TRIGGER blocca2 BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.public_link_issued' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := postLink(b, azioneNuovo(id)); !redirectAllaScheda(r, id) {
		t.Fatalf("guasto del nuovo link: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(id), nil, nil)
	contiene(t, "guasto del nuovo link", r.corpo, `<div role="alert" class="flash flash-errore">Nuovo link cliente non generato.</div>`)
	nonContiene(t, "guasto del nuovo link", r.corpo, "mostrato una volta")
}

// inventario: RIP-S-24, CLI-25
// «Genera nuovo link»: il redirect porta alla scheda con un identificativo opaco; la prima GET
// mostra «Nuovo link cliente generato: il precedente non funziona più.» e il riquadro «Nuovo link
// cliente, mostrato una volta» con <indirizzo_pubblico>/r/<token>, senza cache. Il token è nuovo,
// salvato, e il vecchio non c'è più. Una HEAD non consuma, la seconda GET non lo mostra più, la
// scheda senza identificativo nemmeno; un altro utente, un'altra sessione, la scheda di un'altra
// busta e la home con lo stesso identificativo non vedono niente e non lo consumano.
func TestPaginaNuovoLinkMostratoUnaVolta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	altraBusta := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	altroUtente := dipendenteDentro(t, h)
	altraSessione := titolareDentro(t, h)
	vecchio := tokenDi(t, d, id)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := postLink(b, azioneNuovo(id))
	m := reEsitoScheda.FindStringSubmatch(r.location)
	if r.codice != 302 || m == nil || m[1] != strconv.FormatInt(id, 10) || !redirectA(r, r.location) {
		t.Fatalf("nuovo link: %d %q", r.codice, r.location)
	}
	nuovo := tokenDi(t, d, id)
	if nuovo == vecchio || len(nuovo) != 43 {
		t.Fatalf("token %q", nuovo)
	}
	esito := r.location
	conAltroID := percorsoScheda(altraBusta) + "?esito=" + m[2]
	for nome, c := range map[string]struct {
		b *browser
		p string
	}{
		"un altro utente": {altroUtente, esito}, "un'altra sessione": {altraSessione, esito},
		"un'altra busta": {b, conAltroID}, "la home": {b, "/?esito=" + m[2]},
	} {
		if r := c.b.vai("GET", c.p, nil, nil); r.codice != 200 || strings.Contains(r.corpo, nuovo) || strings.Contains(r.corpo, "mostrato una volta") {
			t.Errorf("%s vede il link: %d", nome, r.codice)
		}
	}
	if r := b.vai("HEAD", esito, nil, nil); r.codice != 200 {
		t.Errorf("HEAD: %d", r.codice)
	}
	r = b.vai("GET", esito, nil, nil)
	contiene(t, "link", r.corpo, `<div role="status" class="flash flash-info">Nuovo link cliente generato: il precedente non funziona più.</div>`,
		`<div id="repair-public-link-box" class="link-nuovo">`+"\n"+`<p class="titoletto">Nuovo link cliente, mostrato una volta</p>`+"\n"+
			`<p id="repair-public-link-new" class="mono">`+indirizzoPubblicoProva+"/r/"+nuovo+`</p>`,
		"🔗 Link cliente attivo fino al 29/01/2027")
	if strings.Count(r.corpo, nuovo) != 1 {
		t.Errorf("il token compare %d volte", strings.Count(r.corpo, nuovo))
	}
	if r.h.Get("Cache-Control") != "no-store" {
		t.Errorf("cache: %q", r.h.Get("Cache-Control"))
	}
	for _, p := range []string{esito, percorsoScheda(id)} {
		if r := b.vai("GET", p, nil, nil); strings.Contains(r.corpo, nuovo) || strings.Contains(r.corpo, "mostrato una volta") {
			t.Errorf("%s: il link si vede ancora", p)
		} else if r.h.Get("Cache-Control") == "no-store" && p == percorsoScheda(id) {
			t.Error("la scheda normale è no-store")
		}
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued' AND attore = 'titolare@esempio.it' AND dati = '{"reason":"manual"}'`); n != 1 {
		t.Errorf("audit %d", n)
	}

	// Un esito scaduto (5 minuti) non si vede.
	b.vai("GET", percorsoScheda(id), nil, nil)
	r = postLink(b, azioneNuovo(id))
	o.Sposta(5 * time.Minute)
	if r := b.vai("GET", r.location, nil, nil); strings.Contains(r.corpo, "mostrato una volta") {
		t.Error("esito scaduto mostrato")
	}
}

// inventario: RIP-S-24, CLI-25
// Su una busta di un'oreficeria i testi dicono «dell'oreficeria». Senza indirizzo_pubblico il
// nuovo link si rifiuta (D-46): TestNuovoLinkSenzaIndirizzoPubblico.
func TestPaginaNuovoLinkOreficeria(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria Esempio')`)
	id := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	b := titolareDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := b.vai("GET", postLink(b, azioneNuovo(id)).location, nil, nil)
	contiene(t, "oreficeria", r.corpo, "Nuovo link dell&#39;oreficeria generato: il precedente non funziona più.",
		"Nuovo link dell&#39;oreficeria, mostrato una volta", indirizzoPubblicoProva+"/r/"+tokenDi(t, d, id))

}

// inventario: RIP-S-24, CLI-24, CLI-25
// Il vecchio link con codice: la conferma della revoca lo nomina solo se è vivo; dopo la revoca la
// riga dice «· link storico revocato», anche riaprendo la scheda. «Genera nuovo link» su una busta
// con il codice lascia il link attivo con «· link storico revocato», e la conferma torna quella
// semplice. Una busta nata qui non ha mai la riga.
func TestPaginaLinkStorico(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	nata := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	contiene(t, "nata qui", b.vai("GET", percorsoScheda(nata), nil, nil).corpo, `data-confirm="`+confermaRevoca+`">Revoca link`)
	postLink(b, azioneRevoca(nata))
	nonContiene(t, "nata qui", b.vai("GET", percorsoScheda(nata), nil, nil).corpo, "link storico revocato")

	conCodice := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, bytes.Repeat([]byte{7}, 32), conCodice)
	r := b.vai("GET", percorsoScheda(conCodice), nil, nil)
	contiene(t, "con il codice", r.corpo, `data-confirm="`+confermaRevocaCodice+`">Revoca link`)
	nonContiene(t, "con il codice", r.corpo, "link storico revocato")
	postLink(b, azioneRevoca(conCodice))
	if s := statoLink(t, b.vai("GET", percorsoScheda(conCodice), nil, nil).corpo); s != "🔗 Link cliente revocato: al prossimo invio ne nasce uno nuovo<span> · link storico revocato</span>" {
		t.Errorf("dopo la revoca: %q", s)
	}

	nuova := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `INSERT INTO corrispondenze_import_flask (tabella_flask, id_flask, tabella_standalone, id_standalone, creato_il)
		VALUES ('riparazioni', '900', 'repairs', ?, '2026-10-31T09:00:00.000000Z')`, nuova)
	b.vai("GET", percorsoScheda(nuova), nil, nil)
	postLink(b, azioneNuovo(nuova))
	r = b.vai("GET", percorsoScheda(nuova), nil, nil)
	if s := statoLink(t, r.corpo); s != "🔗 Link cliente attivo fino al 29/01/2027<span> · link storico revocato</span>" {
		t.Errorf("dopo il nuovo link: %q", s)
	}
	contiene(t, "dopo il nuovo link", r.corpo, `data-confirm="`+confermaRevoca+`">Revoca link`)
}

// inventario: RIP-S-24, CLI-25
// Il token del nuovo link esce solo nella pagina che lo mostra, una volta: né nella risposta al
// POST, né nell'indirizzo del redirect, né nelle intestazioni, né nel cookie di sessione, né nel
// registro (anche a livello debug), né nell'audit.
func TestPaginaNuovoLinkTokenMaiFuori(t *testing.T) {
	var reg bytes.Buffer
	h, d, o := facciaConBusteE(t, &reg, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r := postLink(b, azioneNuovo(id))
	token := tokenDi(t, d, id)
	var sessione string
	if k := setCookie(r.h, "__Host-gestrip_sessione_interna"); k != nil {
		dati, _, _ := strings.Cut(k.Value, ".")
		j, _ := base64.RawURLEncoding.DecodeString(dati)
		sessione = string(j)
	}
	pagina := b.vai("GET", r.location, nil, nil)
	if strings.Count(pagina.corpo, token) != 1 {
		t.Fatalf("il token compare %d volte nella pagina", strings.Count(pagina.corpo, token))
	}
	r2 := postLink(b, azioneRevoca(id))
	var righeAudit string
	_ = d.Lettura.QueryRow(`SELECT group_concat(attore || azione || coalesce(dati, ''), ' ') FROM eventi_audit`).Scan(&righeAudit)
	for cosa, testo := range map[string]string{
		"corpo": r.corpo, "location": r.location, "intestazioni": headerTesto(r.h), "sessione": sessione,
		"intestazioni della pagina": headerTesto(pagina.h), "revoca": r2.corpo + r2.location + headerTesto(r2.h),
		"registro": reg.String(), "audit": righeAudit,
	} {
		if strings.Contains(testo, token) {
			t.Errorf("il token è finito in %s", cosa)
		}
	}
	if !strings.Contains(reg.String(), "/repairs/{id}/nuovo-link") {
		t.Error("il registro non ha la rotta: la prova non vale")
	}
}

// inventario: ACC-05, CLI-24, CLI-25
// Un'azione del titolare dentro una pagina comune è registrata con il messaggio della sua riga della
// tabella dei permessi: il rifiuto del dipendente è quello dello standalone, non quello delle rotte
// del titolare.
func TestAzioniDelTitolareConIlLoroRifiuto(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	dichiarate := map[string]rottaDichiarata{}
	for _, r := range f.rotte {
		dichiarate[r.pattern] = r
	}
	for _, r := range tabellaPermessi {
		if r.goRotta == "" {
			continue
		}
		if got := dichiarate[r.goRotta].rifiuto; got != r.rifiuto {
			t.Errorf("%s: rifiuto %q, la tabella dice %q", r.goRotta, got, r.rifiuto)
		}
	}
}

// azioneDelTitolare è solo per un POST con il suo messaggio: un GET o un messaggio vuoto sono un
// errore di programmazione e si fermano subito.
func TestAzioneDelTitolareSoloPOSTConMessaggio(t *testing.T) {
	_, f, err := costruisciInterna(dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, &utentiFinti{attivo: true}), nil)
	if err != nil {
		t.Fatal(err)
	}
	h := func(http.ResponseWriter, *http.Request) {}
	for nome, c := range map[string][2]string{
		"GET":             {"GET /prova/azione", "Solo il titolare può provare."},
		"senza metodo":    {"/prova/azione", "Solo il titolare può provare."},
		"messaggio vuoto": {"POST /prova/azione", ""},
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("%s: nessun panic", nome)
				} else if !strings.Contains(fmt.Sprint(r), c[0]) {
					t.Errorf("%s: panic %v", nome, r)
				}
			}()
			f.azioneDelTitolare(http.NewServeMux(), c[0], c[1], f.tornaAllaScheda, h)
		}()
	}
	// Il caso buono non va in panic.
	f.azioneDelTitolare(http.NewServeMux(), "POST /prova/azione", "Solo il titolare può provare.", f.tornaAllaScheda, h)
}

// inventario: RIP-S-24, CLI-24, CLI-25
// Il nuovo link mostrato una volta sparisce con il comando dopo, come clear_one_time_link/3 dello
// standalone: un esito non ancora aperto e poi una revoca, o un altro nuovo link, e la pagina del
// primo esito non mostra più niente; l'esito di un'altra busta, o di un'altra sessione (anche dello
// stesso titolare: nello standalone il link sta nel socket), resta.
func TestPaginaNuovoLinkTolto(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id, altra := bustaScheda(t, d, o, nil), bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	altro := titolareDentro(t, h)

	b.vai("GET", percorsoScheda(id), nil, nil)
	primo := postLink(b, azioneNuovo(id)).location
	b.vai("GET", percorsoScheda(altra), nil, nil)
	dellAltra := postLink(b, azioneNuovo(altra)).location
	altro.vai("GET", percorsoScheda(id), nil, nil)
	dellAltro := postLink(altro, azioneNuovo(id)).location
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := postLink(b, azioneRevoca(id)); !redirectAllaScheda(r, id) {
		t.Fatalf("revoca: %d %q", r.codice, r.location)
	}
	if r := b.vai("GET", primo, nil, nil); strings.Contains(r.corpo, "mostrato una volta") || strings.Contains(r.corpo, "/r/") {
		t.Error("dopo la revoca il primo esito mostra ancora il link")
	}
	if r := b.vai("GET", dellAltra, nil, nil); !strings.Contains(r.corpo, "mostrato una volta") {
		t.Error("la revoca ha tolto l'esito di un'altra busta")
	}
	if r := altro.vai("GET", dellAltro, nil, nil); !strings.Contains(r.corpo, "mostrato una volta") {
		t.Error("la revoca ha tolto l'esito di un'altra sessione")
	}

	// Due nuovi link di fila: si vede solo l'ultimo.
	b.vai("GET", percorsoScheda(id), nil, nil)
	uno := postLink(b, azioneNuovo(id)).location
	b.vai("GET", percorsoScheda(id), nil, nil)
	due := postLink(b, azioneNuovo(id)).location
	if r := b.vai("GET", uno, nil, nil); strings.Contains(r.corpo, "mostrato una volta") {
		t.Error("il nuovo link precedente si vede ancora")
	}
	r := b.vai("GET", due, nil, nil)
	contiene(t, "ultimo", r.corpo, "mostrato una volta", indirizzoPubblicoProva+"/r/"+tokenDi(t, d, id))
}
