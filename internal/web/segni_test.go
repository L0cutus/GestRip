package web

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La scheda mostrata senza una riga repair.viewed (D-44 estesa, RIP-S-01; Stefano 01/11/2026):
// solo dopo il redirect di un'azione della scheda (rifiuti compresi) e per «Modifica» e «Annulla»,
// con un segno monouso legato a utente, sessione e busta. Ricaricare, riusare un segno, un segno di
// un'altra sessione o un segno inventato sono aperture vere, con la riga.

// reSchedaMostrata è il redirect di un'azione alla scheda: il segno, e prima l'esito del nuovo link.
var reSchedaMostrata = regexp.MustCompile(`^/repairs/(\d+)\?(?:esito=[A-Za-z0-9_-]{22}&)?mostra=([A-Za-z0-9_-]{22})$`)

// redirectAllaScheda dice se r è il redirect alla scheda della busta id con il segno, con il corpo
// del redirect di Phoenix.
func redirectAllaScheda(r risposta, id int64) bool {
	m := reSchedaMostrata.FindStringSubmatch(r.location)
	return m != nil && m[1] == strconv.FormatInt(id, 10) && redirectA(r, r.location)
}

// viste conta le righe repair.viewed.
func viste(t *testing.T, d *db.DB) int {
	t.Helper()
	return contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.viewed'`)
}

// hrefDi è l'href dell'elemento con quell'id, con le entità tolte.
func hrefDi(t *testing.T, corpo, id string) string {
	t.Helper()
	m := regexp.MustCompile(`<a id="` + regexp.QuoteMeta(id) + `" href="([^"]*)"`).FindStringSubmatch(corpo)
	if m == nil {
		t.Fatalf("manca il link %s", id)
	}
	return strings.ReplaceAll(m[1], "&amp;", "&")
}

// inventario: RIP-S-01, RIP-S-04, RIP-S-06, RIP-S-13, RIP-S-14, RIP-S-15, RIP-S-24
// Ogni azione della scheda, riuscita o rifiutata, torna alla scheda con il segno: la scheda si
// mostra con il messaggio e senza una riga repair.viewed, e senza segnare letta la conversazione.
// Ricaricare lo stesso indirizzo, o aprire la scheda senza segno, scrive la riga (e segna letto).
func TestSchedaDopoUnAzioneSenzaRiga(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	chiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, chiusa)
	tit, dip := titolareDentro(t, h), dipendenteDentro(t, h)
	for _, c := range []struct {
		nome      string
		b         *browser
		busta     int64
		percorso  string
		form      func(*browser) url.Values
		messaggio string
	}{
		{"modifica", dip, id, azioneModifica(id), func(b *browser) url.Values {
			return url.Values{"_csrf_token": {b.token}, "repair[description]": {"Anello rifatto"}}
		}, "Riparazione aggiornata."},
		{"togli data", dip, id, percorsoScheda(id) + "/togli-data", nil, "Data di consegna tolta."},
		{"stato", dip, id, azioneStato(id), func(b *browser) url.Values { return formStato(b, "preventivo") }, "Stato aggiornato."},
		{"stato rifiutato", dip, id, azioneStato(id), func(b *browser) url.Values { return formStato(b, "chiusa") }, "Transizione non consentita."},
		{"stato uguale", dip, id, azioneStato(id), func(b *browser) url.Values { return formStato(b, "preventivo") }, ""},
		{"attesa", dip, id, azioneAttesa(id), func(b *browser) url.Values { return formAttesa(b, "on", "Colore?") }, msgInAttesa},
		{"attesa tolta", dip, id, azioneAttesa(id), func(b *browser) url.Values { return formAttesa(b, "off", "") }, msgAttesaRimossa},
		{"attesa rifiutata", dip, id, azioneAttesa(id), func(b *browser) url.Values { return formAttesa(b, "boh", "") }, "Azione non valida."},
		{"riapertura", dip, chiusa, azioneRiapri(chiusa), func(b *browser) url.Values { return formRiapri(b, "in_lavorazione", "Pietra") }, msgBustaRiaperta},
		{"revoca del dipendente", dip, id, azioneRevoca(id), nil, msgSoloTitolareRevo},
		{"nuovo link del dipendente", dip, id, azioneNuovo(id), nil, msgSoloTitolareNuov},
		{"revoca", tit, id, azioneRevoca(id), nil, "Link cliente revocato."},
		{"nuovo link", tit, id, azioneNuovo(id), nil, "Nuovo link cliente generato: il precedente non funziona più."},
	} {
		c.b.vai("GET", percorsoScheda(c.busta), nil, nil)
		// Un messaggio nuovo da leggere: la scheda mostrata senza riga non lo segna letto.
		eseguiSQL(t, d, `UPDATE buste SET ultimo_messaggio_il = ? WHERE id = ?`, o.Adesso().Format("2006-01-02T15:04:05.000000Z"), c.busta)
		o.Sposta(time.Second)
		form := url.Values{"_csrf_token": {c.b.token}}
		if c.form != nil {
			form = c.form(c.b)
		}
		prima, letti := viste(t, d), colonna(t, d, c.busta, "messaggi_letti_il")
		r := c.b.vai("POST", c.percorso, form, nil)
		if !redirectAllaScheda(r, c.busta) {
			t.Fatalf("%s: %d %q", c.nome, r.codice, r.location)
		}
		mostrata := c.b.vai("GET", r.location, nil, nil)
		if mostrata.codice != 200 {
			t.Fatalf("%s: %d %q", c.nome, mostrata.codice, mostrata.location)
		}
		if c.messaggio != "" {
			contiene(t, c.nome, mostrata.corpo, escapeHTMLTest(c.messaggio))
		} else {
			nonContiene(t, c.nome, mostrata.corpo, "flash-errore", "flash-info")
		}
		if n := viste(t, d); n != prima {
			t.Errorf("%s: %d righe repair.viewed dopo l'azione", c.nome, n-prima)
		}
		if l := colonna(t, d, c.busta, "messaggi_letti_il"); l != letti {
			t.Errorf("%s: la scheda mostrata ha segnato letto (%s)", c.nome, l)
		}
		// Ricaricare è un'apertura vera.
		if r := c.b.vai("GET", r.location, nil, nil); r.codice != 200 || viste(t, d) != prima+1 {
			t.Errorf("%s: ricaricata %d, righe %d", c.nome, r.codice, viste(t, d)-prima)
		}
		if l := colonna(t, d, c.busta, "messaggi_letti_il"); l == letti {
			t.Errorf("%s: l'apertura vera non ha segnato letto", c.nome)
		}
	}
}

// inventario: RIP-S-01
// Il segno non si forgia: inventato, già usato, di un'altra sessione dello stesso utente, di un
// altro utente, di un'altra busta, scaduto o con una HEAD è un'apertura vera, con la riga. Un
// segno di un'altra sessione o di un'altra busta non si consuma.
func TestSegnoDellaSchedaNonSiForgia(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	altra := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	segno := func() string {
		t.Helper()
		b.vai("GET", percorsoScheda(id), nil, nil)
		r := b.vai("POST", azioneAttesa(id), formAttesa(b, "boh", ""), nil)
		if !redirectAllaScheda(r, id) {
			t.Fatalf("%d %q", r.codice, r.location)
		}
		return reSchedaMostrata.FindStringSubmatch(r.location)[2]
	}
	apre := func(nome string, b *browser, metodo, percorso string) {
		t.Helper()
		prima := viste(t, d)
		r := b.vai(metodo, percorso, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		if n := viste(t, d) - prima; n != 1 {
			t.Errorf("%s: %d righe repair.viewed, voluta 1", nome, n)
		}
	}
	nonApre := func(nome string, b *browser, percorso string) {
		t.Helper()
		prima := viste(t, d)
		if r := b.vai("GET", percorso, nil, nil); r.codice != 200 {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		if n := viste(t, d) - prima; n != 0 {
			t.Errorf("%s: %d righe repair.viewed, voluta 0", nome, n)
		}
	}
	apre("inventato", b, "GET", percorsoScheda(id)+"?mostra="+casuale())
	apre("vuoto", b, "GET", percorsoScheda(id)+"?mostra=")
	apre("da=azione", b, "GET", percorsoScheda(id)+"?da=azione")

	s := segno()
	nonApre("primo uso", b, percorsoScheda(id)+"?mostra="+s)
	apre("secondo uso", b, "GET", percorsoScheda(id)+"?mostra="+s)

	s = segno()
	apre("altra busta", b, "GET", percorsoScheda(altra)+"?mostra="+s)
	altroUtente := titolareDentro(t, h)
	apre("altro utente", altroUtente, "GET", percorsoScheda(id)+"?mostra="+s)
	stessoUtente := dipendenteDentro(t, h) // un altro accesso: un'altra sessione dello stesso account
	apre("altra sessione", stessoUtente, "GET", percorsoScheda(id)+"?mostra="+s)
	apre("HEAD", b, "HEAD", percorsoScheda(id)+"?mostra="+s)
	nonApre("non consumato dagli altri", b, percorsoScheda(id)+"?mostra="+s)

	s = segno()
	o.Sposta(durataSegnoAzione)
	apre("scaduto", b, "GET", percorsoScheda(id)+"?mostra="+s)

	// Il segno in un'assegnazione con le parentesi non è un testo: apertura vera.
	s = segno()
	apre("parentesi", b, "GET", percorsoScheda(id)+"?mostra[]="+s)
	nonApre("ultimo valore", b, percorsoScheda(id)+"?mostra=x&mostra="+s)
}

// inventario: RIP-S-01, RIP-S-04
// «Modifica» e «Annulla» sono link con un segno monouso messo dalla pagina: aprono il modulo e
// tornano alla scheda senza la riga. Riusare il link, o scrivere ?modifica=1 a mano, è un'apertura
// vera, con la riga, e il modulo si apre lo stesso. Anche il modulo ridisegnato dopo un rifiuto e
// la scheda mostrata dopo un'azione hanno i link con il segno.
func TestModificaEAnnullaSenzaRiga(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	passo := func(nome, percorso string, righe int, modulo bool) risposta {
		t.Helper()
		prima := viste(t, d)
		r := b.vai("GET", percorso, nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		if n := viste(t, d) - prima; n != righe {
			t.Errorf("%s: %d righe repair.viewed, volute %d", nome, n, righe)
		}
		if strings.Contains(r.corpo, `id="repair-form"`) != modulo {
			t.Errorf("%s: modulo %v", nome, !modulo)
		}
		return r
	}
	r := passo("apertura", percorsoScheda(id), 1, false)
	modifica := hrefDi(t, r.corpo, "edit-repair")
	if !regexp.MustCompile(`^/repairs/` + strconv.FormatInt(id, 10) + `\?modifica=1&mostra=[A-Za-z0-9_-]{22}$`).MatchString(modifica) {
		t.Fatalf("Modifica: %q", modifica)
	}
	r = passo("Modifica", modifica, 0, true)
	annulla := hrefDi(t, r.corpo, "cancel-repair")
	if !regexp.MustCompile(`^/repairs/` + strconv.FormatInt(id, 10) + `\?mostra=[A-Za-z0-9_-]{22}$`).MatchString(annulla) {
		t.Fatalf("Annulla: %q", annulla)
	}
	r = passo("Annulla", annulla, 0, false)
	passo("Modifica riusato", modifica, 1, true)
	passo("Annulla riusato", annulla, 1, false)
	passo("modifica a mano", percorsoModifica(id), 1, true)
	passo("edit=prezzo a mano", percorsoScheda(id)+"?edit=prezzo", 1, true)

	// Dopo un rifiuto della modifica il modulo ridisegnato ha «Annulla» con il segno.
	r = b.vai("POST", azioneModifica(id), url.Values{"_csrf_token": {b.token}, "repair[description]": {""}}, nil)
	if r.codice != 200 {
		t.Fatalf("rifiuto: %d", r.codice)
	}
	passo("Annulla dopo il rifiuto", hrefDi(t, r.corpo, "cancel-repair"), 0, false)
	// Dopo un'azione la scheda mostrata ha «Modifica» con il segno.
	r = b.vai("POST", azioneAttesa(id), formAttesa(b, "boh", ""), nil)
	r = passo("dopo l'azione", r.location, 0, false)
	r = passo("Modifica dopo l'azione", hrefDi(t, r.corpo, "edit-repair"), 0, true)
	// Un segno dei link vale anche dopo la scadenza di quelli dei redirect, non oltre la sua.
	o.Sposta(durataSegnoAzione)
	r = passo("Annulla dopo due minuti", hrefDi(t, r.corpo, "cancel-repair"), 0, false)
	r = passo("Modifica di nuovo", hrefDi(t, r.corpo, "edit-repair"), 0, true)
	annulla = hrefDi(t, r.corpo, "cancel-repair")
	o.Sposta(durataSegnoLink)
	passo("Annulla scaduto", annulla, 1, false)
	// Dopo la riapertura rifiutata nei campi la scheda ridisegnata ha «Modifica» con il segno.
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, id)
	b.vai("GET", percorsoScheda(id), nil, nil)
	r = b.vai("POST", azioneRiapri(id), formRiapri(b, "in_lavorazione", ""), nil)
	if r.codice != 200 {
		t.Fatalf("riapertura rifiutata: %d %q", r.codice, r.location)
	}
	passo("Modifica dopo la riapertura rifiutata", hrefDi(t, r.corpo, "edit-repair"), 0, true)
}

// inventario: RIP-S-13
// Lo stato che la busta ha già (un modulo rimandato, una pagina tornata indietro) torna alla scheda
// in silenzio (D-45): nessun messaggio, nessun audit, niente cambia; anche su una busta chiusa.
// Gli altri rifiuti restano.
func TestPaginaStatoUgualeInSilenzio(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	chiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, chiusa)
	b := titolareDentro(t, h)
	versioni := contaRighe(t, d, `SELECT coalesce(sum(versione), 0) FROM versioni_stato_buste`)
	for _, c := range []struct {
		busta int64
		stato string
		vista string
	}{{id, "in_lavorazione", "In lavorazione"}, {chiusa, "consegnata", "Consegnata"}} {
		b.vai("GET", percorsoScheda(c.busta), nil, nil)
		aggiornata := colonna(t, d, c.busta, "aggiornato_il")
		audit := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
		r := b.vai("POST", azioneStato(c.busta), formStato(b, c.stato), nil)
		if !redirectAllaScheda(r, c.busta) {
			t.Fatalf("%s: %d %q", c.stato, r.codice, r.location)
		}
		r = b.vai("GET", r.location, nil, nil)
		contiene(t, c.stato, r.corpo, `<dd id="repair-status" class="forte">`+c.vista+`</dd>`)
		nonContiene(t, c.stato, r.corpo, "flash-errore", "flash-info", msgTransizione)
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`); n != audit {
			t.Errorf("%s: %d righe d'audit", c.stato, n-audit)
		}
		if a := colonna(t, d, c.busta, "aggiornato_il"); a != aggiornata {
			t.Errorf("%s: busta toccata", c.stato)
		}
	}
	if n := contaRighe(t, d, `SELECT coalesce(sum(versione), 0) FROM versioni_stato_buste`); n != versioni {
		t.Errorf("versioni %d", n-versioni)
	}
	// Uno stato fuori dal grafo resta un rifiuto.
	b.vai("GET", percorsoScheda(chiusa), nil, nil)
	r := b.vai("GET", b.vai("POST", azioneStato(chiusa), formStato(b, "in_lavorazione"), nil).location, nil, nil)
	contiene(t, "chiusa", r.corpo, `<div role="alert" class="flash flash-errore">Transizione non consentita.</div>`)
}

// inventario: RIP-S-24
// Senza indirizzo_pubblico «Genera nuovo link» rifiuta prima di toccare qualunque cosa (D-46): il
// vecchio link, le iscrizioni Telegram, l'email e le email in coda restano, nessun audit; la scheda
// dice quale riga scrivere, in quale file, e il riavvio. Il dipendente riceve prima il rifiuto del
// ruolo, come per ogni azione del titolare; una busta che non c'è va alla home.
func TestNuovoLinkSenzaIndirizzoPubblico(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, "")
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio di prova'), (2, 'Oreficeria Esempio')`)
	id := bustaScheda(t, d, o, nil)
	ore := bustaScheda(t, d, o, func(a *buste.Accettazione) { a.OreficeriaID = "2" })
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	eseguiSQL(t, d, `UPDATE buste SET email_cliente = 'cliente@esempio.test', email_confermata_il = ? WHERE id = ?`, ora, id)
	eseguiSQL(t, d, `INSERT INTO iscrizioni_telegram (busta_id, chat_id, consenso, collegata_il, consenso_il, creato_il, aggiornato_il)
		VALUES (?2, 77, 1, ?1, ?1, ?1, ?1)`, ora, id)
	eseguiSQL(t, d, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, prossimo_tentativo_il, accodato_il)
		VALUES (?2, 'email', 'customer', 'cliente@esempio.test', 'Testo inventato', 'queued', ?1, ?1)`, ora, id)
	foto := func() string {
		return colonna(t, d, id, "token_pubblico") + "|" + colonna(t, d, id, "hash_token_pubblico") + "|" +
			colonna(t, d, id, "token_pubblico_scade_il") + "|" + colonna(t, d, id, "token_pubblico_revocato_il") + "|" +
			colonna(t, d, id, "email_cliente") + "|" + colonna(t, d, id, "email_confermata_il") + "|" +
			strconv.Itoa(contaRighe(t, d, `SELECT count(*) FROM iscrizioni_telegram WHERE fermata_il IS NULL`)) + "|" +
			strconv.Itoa(contaRighe(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE stato = 'queued'`)) + "|" +
			strconv.Itoa(contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione != 'repair.viewed'`))
	}
	b := titolareDentro(t, h)
	for _, c := range []struct {
		busta int64
		parte string
	}{{id, "cliente"}, {ore, "dell&#39;oreficeria"}} {
		b.vai("GET", percorsoScheda(c.busta), nil, nil)
		prima := foto()
		r := postLink(b, azioneNuovo(c.busta))
		if !redirectAllaScheda(r, c.busta) || strings.Contains(r.location, "esito=") {
			t.Fatalf("%s: %d %q", c.parte, r.codice, r.location)
		}
		if dopo := foto(); dopo != prima {
			t.Errorf("%s: toccato qualcosa\nprima %s\ndopo  %s", c.parte, prima, dopo)
		}
		r = b.vai("GET", r.location, nil, nil)
		contiene(t, c.parte, r.corpo, `<div role="alert" class="flash flash-errore">Nuovo link `+c.parte+
			` non generato: manca «indirizzo_pubblico = https://host[:porta]» in `+configProva+
			`, poi riavvia gestrip serve con «sudo systemctl restart gestrip».</div>`)
		nonContiene(t, c.parte, r.corpo, "mostrato una volta", "/r/", "flash-info")
	}
	// Il dipendente: prima il ruolo.
	dip := dipendenteDentro(t, h)
	dip.vai("GET", percorsoScheda(id), nil, nil)
	prima := foto()
	r := dip.vai("GET", postLink(dip, azioneNuovo(id)).location, nil, nil)
	contiene(t, "dipendente", r.corpo, `<div role="alert" class="flash flash-errore">`+msgSoloTitolareNuov+`</div>`)
	if foto() != prima {
		t.Error("dipendente: toccato qualcosa")
	}
	// Una busta che non c'è: la home, come le altre azioni.
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := postLink(b, azioneNuovo(ore+100)); !redirectA(r, "/") {
		t.Errorf("busta che non c'è: %d %q", r.codice, r.location)
	}
	// La revoca senza indirizzo_pubblico funziona come prima.
	b.vai("GET", percorsoScheda(id), nil, nil)
	if r := postLink(b, azioneRevoca(id)); !redirectAllaScheda(r, id) || colonna(t, d, id, "token_pubblico") != "∅" {
		t.Errorf("revoca: %d %q", r.codice, r.location)
	}
}

// inventario: RIP-S-01
// La lettura della busta senza audit (buste.Scheda) la usano solo i gestori della scheda che
// mostrano la busta senza un'apertura (segno valido, rifiuto con il segno della pagina) o che ne leggono un
// campo prima di un'azione: nessun altro codice del repository la chiama, così non diventa una
// strada per guardare una busta senza lasciare traccia.
func TestLetturaSenzaAuditSoloDaiChiamantiNoti(t *testing.T) {
	ammessi := []string{
		"internal/web.mostraOApri", "internal/web.cambiaStato", "internal/web.leggiDopoRifiuto",
		"internal/web.proprietarioDellaBusta",
	}
	radice := filepath.Join("..", "..")
	var trovati []string
	err := filepath.WalkDir(radice, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(radice, p)
		if e.IsDir() {
			if rel == "to-be-deleted-after-use" || strings.HasPrefix(e.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if err != nil {
			return err
		}
		pacchetto := filepath.ToSlash(filepath.Dir(rel))
		for _, dich := range f.Decls {
			fn, ok := dich.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Scheda" {
					return true
				}
				// buste.Scheda è anche il tipo: una conversione ha un solo argomento e nessun contesto.
				if len(call.Args) != 2 {
					return true
				}
				if pacchetto == "internal/buste" {
					t.Errorf("%s: %s chiama Scheda dentro il dominio", rel, fn.Name.Name)
					return true
				}
				trovati = append(trovati, pacchetto+"."+fn.Name.Name)
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range trovati {
		if !slices.Contains(ammessi, c) {
			t.Errorf("chiamante non ammesso della lettura senza audit: %s", c)
		}
	}
	for _, a := range ammessi {
		if !slices.Contains(trovati, a) {
			t.Errorf("chiamante atteso %s non trovato: il test non guarda più dove deve", a)
		}
	}
}

// inventario: RIP-S-01
// Il deposito dei segni: un segno vale una volta, per la stessa sessione e la stessa busta, fino alla
// sua scadenza, e dà l'inizio della sua catena; un segno senza sessione o senza busta non si fa;
// pieno, non ne fa altri (il link resta senza segno e aprirlo scrive la riga), e prima toglie gli
// scaduti.
func TestDepositoSegni(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	d := nuovoDepositoSegni(o)
	ora := func() time.Time { return o.Adesso() }
	prende := func(s string, u int64, n string, b int64) bool { _, ok := d.prendi(s, u, n, b); return ok }
	s := d.metti(3, "n", 7, time.Minute, ora())
	if len(s) != 22 {
		t.Fatalf("segno %q", s)
	}
	for _, c := range []struct {
		utente int64
		nonce  string
		busta  int64
	}{{4, "n", 7}, {3, "m", 7}, {3, "n", 8}, {3, "", 7}} {
		if prende(s, c.utente, c.nonce, c.busta) {
			t.Errorf("%+v ha preso il segno", c)
		}
	}
	if inizio, ok := d.prendi(s, 3, "n", 7); !ok || !inizio.Equal(ora()) || prende(s, 3, "n", 7) {
		t.Errorf("il segno non vale una volta sola, o senza il suo inizio (%v)", inizio)
	}
	if prende("", 3, "n", 7) {
		t.Error("segno vuoto")
	}
	for _, c := range []struct {
		utente int64
		nonce  string
		busta  int64
	}{{0, "n", 7}, {3, "", 7}, {3, "n", 0}} {
		if s := d.metti(c.utente, c.nonce, c.busta, time.Minute, ora()); s != "" {
			t.Errorf("%+v: segno %q", c, s)
		}
	}
	s = d.metti(3, "n", 7, time.Minute, ora())
	o.Sposta(time.Minute - time.Nanosecond)
	if !prende(s, 3, "n", 7) {
		t.Error("scaduto prima del tempo")
	}
	s = d.metti(3, "n", 7, time.Minute, ora())
	o.Sposta(time.Minute)
	if prende(s, 3, "n", 7) {
		t.Error("vale alla scadenza")
	}
	if d.quanti() != 0 {
		t.Errorf("lo scaduto è rimasto: %d", d.quanti())
	}
	// Pieno con tante sessioni (il tetto per sessione non lo riempie da solo).
	for i := range maxSegni {
		if d.metti(int64(10+i), "n", 7, time.Minute, ora()) == "" {
			t.Fatal("pieno prima del massimo")
		}
	}
	if s := d.metti(3, "n", 7, time.Minute, ora()); s != "" || d.quanti() != maxSegni {
		t.Errorf("oltre il massimo: %q, %d", s, d.quanti())
	}
	o.Sposta(time.Minute)
	if s := d.metti(3, "n", 7, time.Minute, ora()); s == "" || d.quanti() != 1 {
		t.Errorf("gli scaduti non sono stati tolti: %q, %d", s, d.quanti())
	}
}

// inventario: RIP-S-01
// La catena dei segni: la scadenza di un segno non va mai oltre un'ora (durataCatena) dall'inizio
// della sua catena, cioè dall'ultima riga scritta; un inizio già oltre l'ora non dà segni.
func TestDepositoCatena(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	d := nuovoDepositoSegni(o)
	inizio := o.Adesso()
	o.Sposta(50 * time.Minute)
	s := d.metti(3, "n", 7, durataSegnoLink, inizio)
	o.Sposta(10*time.Minute - time.Nanosecond)
	if i, ok := d.prendi(s, 3, "n", 7); !ok || !i.Equal(inizio) {
		t.Errorf("prima dell'ora: %v %v", i, ok)
	}
	s = d.metti(3, "n", 7, durataSegnoLink, inizio)
	o.Sposta(time.Nanosecond)
	if _, ok := d.prendi(s, 3, "n", 7); ok {
		t.Error("vale oltre l'ora dall'inizio della catena")
	}
	if s := d.metti(3, "n", 7, durataSegnoLink, inizio); s != "" {
		t.Errorf("segno da una catena finita: %q", s)
	}
	if s := d.metti(3, "n", 7, durataSegnoLink, time.Time{}); s != "" {
		t.Errorf("segno senza inizio: %q", s)
	}
	// Il tipo vince quando è più corto: un redirect vale 2 minuti anche a inizio della catena.
	s = d.metti(3, "n", 7, durataSegnoAzione, o.Adesso())
	o.Sposta(durataSegnoAzione)
	if _, ok := d.prendi(s, 3, "n", 7); ok {
		t.Error("il segno del redirect vale oltre la sua durata")
	}
}

// inventario: RIP-S-01
// Ogni sessione tiene al più maxSegniSessione segni: oltre, si toglie il più vecchio di quella
// sessione; gli altri e le altre sessioni restano, e una sessione sola non riempie il deposito.
func TestDepositoTettoPerSessione(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	d := nuovoDepositoSegni(o)
	altra := d.metti(2, "m", 7, durataSegnoLink, o.Adesso())
	var primi []string
	for range maxSegniSessione {
		primi = append(primi, d.metti(1, "n", 7, durataSegnoLink, o.Adesso()))
		o.Sposta(time.Millisecond)
	}
	for range maxSegni {
		if d.metti(1, "n", 7, durataSegnoLink, o.Adesso()) == "" {
			t.Fatal("una sessione ha riempito il deposito")
		}
	}
	if d.quanti() != maxSegniSessione+1 {
		t.Errorf("segni %d", d.quanti())
	}
	for i, s := range primi {
		if _, ok := d.prendi(s, 1, "n", 7); ok {
			t.Errorf("il segno vecchio %d è rimasto", i)
		}
	}
	if _, ok := d.prendi(altra, 2, "m", 7); !ok {
		t.Error("il segno dell'altra sessione è sparito")
	}
	// Uno di una sessione piena: il più vecchio va via, il più nuovo resta.
	d2 := nuovoDepositoSegni(o)
	vecchio := d2.metti(1, "n", 7, durataSegnoLink, o.Adesso())
	o.Sposta(time.Millisecond)
	for range maxSegniSessione - 1 {
		d2.metti(1, "n", 7, durataSegnoLink, o.Adesso())
		o.Sposta(time.Millisecond)
	}
	medio := d2.metti(1, "n", 7, durataSegnoLink, o.Adesso())
	if _, ok := d2.prendi(vecchio, 1, "n", 7); ok {
		t.Error("il più vecchio non è stato tolto")
	}
	if _, ok := d2.prendi(medio, 1, "n", 7); !ok {
		t.Error("il più nuovo è stato tolto")
	}
}

// inventario: RIP-S-01
// Una HEAD non riceve segni (non si vede la pagina); una GET della stessa sessione sì.
func TestNessunSegnoPerUnaHEAD(t *testing.T) {
	o := clock.NuovoFinto(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))
	f := &facciaWeb{segni: nuovoDepositoSegni(o)}
	for _, c := range []struct {
		metodo string
		vuole  bool
	}{{"HEAD", false}, {"GET", true}} {
		r := httptest.NewRequest(c.metodo, "/repairs/7", nil)
		st := &stato{dati: datiSessione{Utente: 3, Nonce: "n"}, inizioCatena: o.Adesso()}
		r = r.WithContext(context.WithValue(r.Context(), chiaveStato{}, st))
		if s := f.segnoPer(r, 7, durataSegnoLink, st.inizioCatena); (s != "") != c.vuole {
			t.Errorf("%s: segno %q", c.metodo, s)
		}
	}
	if f.segni.quanti() != 1 {
		t.Errorf("segni %d", f.segni.quanti())
	}
}

// inventario: RIP-S-01, RIP-S-04
// Seguire a turno «Modifica» e «Annulla» non rilegge la busta per sempre con una riga sola: la
// catena vale un'ora dall'ultima riga, poi il clic è un'apertura vera e la fa ripartire (review
// della 037b, TestReviewCatenaDeiSegni). Anche a clic fitti, una riga ogni ora.
func TestCatenaDeiSegniDuraUnOra(t *testing.T) {
	for _, c := range []struct {
		nome  string
		passo time.Duration
		passi int
		righe int
	}{{"ogni 50 minuti per 25 ore", 50 * time.Minute, 30, 16}, {"ogni 10 minuti per 2 ore", 10 * time.Minute, 12, 3}} {
		t.Run(c.nome, func(t *testing.T) {
			h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
			id := bustaScheda(t, d, o, nil)
			b := titolareDentro(t, h)
			r := b.vai("GET", percorsoScheda(id), nil, nil)
			for i := range c.passi {
				o.Sposta(c.passo)
				link := "edit-repair"
				if i%2 == 1 {
					link = "cancel-repair"
				}
				if r = b.vai("GET", hrefDi(t, r.corpo, link), nil, nil); r.codice != 200 {
					t.Fatalf("passo %d: %d %q", i, r.codice, r.location)
				}
			}
			if n := viste(t, d); n != c.righe {
				t.Errorf("righe repair.viewed %d, volute %d", n, c.righe)
			}
		})
	}
}

// inventario: RIP-S-01, RIP-S-15
// Un'azione riuscita scrive la sua riga e fa ripartire la catena; un rifiuto con il segno della
// pagina non scrive righe e la catena resta quella di prima.
func TestAzioneRiuscitaFaRipartireLaCatena(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := titolareDentro(t, h)
	passo := func(nome string, fa func() risposta, righe int) risposta {
		t.Helper()
		prima := viste(t, d)
		r := fa()
		if r.codice == http.StatusFound {
			r = b.vai("GET", r.location, nil, nil)
		}
		if r.codice != 200 {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		if n := viste(t, d) - prima; n != righe {
			t.Errorf("%s: %d righe, volute %d", nome, n, righe)
		}
		return r
	}
	r := passo("apertura", func() risposta { return b.vai("GET", percorsoScheda(id), nil, nil) }, 1)
	o.Sposta(50 * time.Minute)
	r = passo("attesa messa", func() risposta { return b.vai("POST", azioneAttesa(id), formAttesa(b, "on", ""), nil) }, 0)
	o.Sposta(50 * time.Minute) // 100 minuti dall'apertura, 50 dall'azione
	r = passo("Modifica dopo l'azione", func() risposta { return b.vai("GET", hrefDi(t, r.corpo, "edit-repair"), nil, nil) }, 0)
	o.Sposta(15 * time.Minute) // 65 dall'azione
	r = passo("Annulla oltre l'ora dall'azione", func() risposta { return b.vai("GET", hrefDi(t, r.corpo, "cancel-repair"), nil, nil) }, 1)
	o.Sposta(50 * time.Minute)
	r = passo("rifiuto con il segno", func() risposta { return b.vai("POST", azioneAttesa(id), formAttesa(b, "boh", ""), nil) }, 0)
	o.Sposta(15 * time.Minute) // 65 dall'ultima riga: il rifiuto non l'ha fatta ripartire
	passo("Modifica dopo il rifiuto", func() risposta { return b.vai("GET", hrefDi(t, r.corpo, "edit-repair"), nil, nil) }, 1)
}

// inventario: RIP-S-01, RIP-S-04
// Ogni modulo POST della scheda porta il campo nascosto mostra con il segno della pagina, lo stesso
// dei link: tutti gli 8 moduli (togli data, attesa, risposta arrivata, cambia stato, riapri, revoca,
// nuovo link, modifica), letti dalle pagine disegnate.
func TestOgniModuloDellaSchedaHaIlSegno(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	attesa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET attesa_cliente = 1 WHERE id = ?`, attesa)
	chiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, chiusa)
	b := titolareDentro(t, h)
	reModuloPost := regexp.MustCompile(`(?s)<form [^>]*action="(/repairs/\d+/[a-z-]+)" method="post"[^>]*>(.*?)</form>`)
	visti := map[string]bool{}
	for _, p := range []string{percorsoScheda(id), percorsoScheda(attesa), percorsoScheda(chiusa), percorsoModifica(id)} {
		r := b.vai("GET", p, nil, nil)
		segno := reSegnoPagina.FindStringSubmatch(r.corpo)
		if segno == nil {
			t.Fatalf("%s: nessun segno", p)
		}
		for _, m := range reModuloPost.FindAllStringSubmatch(r.corpo, -1) {
			azione := m[1][strings.LastIndex(m[1], "/")+1:]
			if strings.Contains(m[0], `id="repair-waiting-reply-form"`) {
				azione = "attesa-risposta"
			}
			visti[azione] = true
			if !strings.Contains(m[2], `<input name="mostra" type="hidden" value="`+segno[1]+`">`) {
				t.Errorf("%s: il modulo %s non ha il segno della pagina", p, azione)
			}
		}
	}
	for _, a := range []string{"togli-data", "attesa", "attesa-risposta", "stato", "riapri", "revoca-link", "nuovo-link", "modifica"} {
		if !visti[a] {
			t.Errorf("modulo %s non trovato", a)
		}
	}
	if len(visti) != 8 {
		t.Errorf("moduli %v", visti)
	}
}

// inventario: RIP-S-01
// Il segno del modulo si legge dal corpo, con l'ultima assegnazione: un segno buono dopo uno falso
// vale, uno falso dopo uno buono no; un segno nella query di un POST non conta.
func TestSegnoDelModuloUltimoDelCorpo(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	b := dipendenteDentro(t, h)
	for _, c := range []struct {
		nome  string
		corpo func(segno string) url.Values
		query bool
		righe int
	}{
		{"buono per ultimo", func(s string) url.Values { return url.Values{"mostra": {casuale(), s}} }, false, 0},
		{"falso per ultimo", func(s string) url.Values { return url.Values{"mostra": {s, casuale()}} }, false, 1},
		{"nella query", func(string) url.Values { return url.Values{} }, true, 1},
	} {
		b.vai("GET", percorsoScheda(id), nil, nil)
		segno := b.segno
		form := c.corpo(segno)
		form.Set("_csrf_token", b.token)
		form.Set("waiting[azione]", "boh")
		percorso := azioneAttesa(id)
		if c.query {
			percorso += "?mostra=" + segno
			form.Del("mostra")
			b.senzaSegno = true
		}
		prima := viste(t, d)
		r := b.vai("POST", percorso, form, nil)
		b.senzaSegno = false
		b.vai("GET", r.location, nil, nil)
		if n := viste(t, d) - prima; n != c.righe {
			t.Errorf("%s: %d righe, volute %d (%q)", c.nome, n, c.righe, r.location)
		}
	}
}

// rifiutoProva è un rifiuto (o un'azione che non cambia niente e non scrive una riga sua) della
// scheda: il POST e se la risposta è un redirect o la scheda ridisegnata.
type rifiutoProva struct {
	nome      string
	b         *browser
	busta     int64
	percorso  string
	form      func(*browser) url.Values
	messaggio string
	ridisegna bool
}

// inventario: RIP-S-01, RIP-S-04, RIP-S-06, RIP-S-13, RIP-S-14, RIP-S-15, RIP-S-24
// Un rifiuto non ha una riga sua: la scheda che lo mostra (redirect o ridisegno) è senza riga solo
// se il modulo porta il segno della pagina della scheda (D-44 come applicata dalla sessione
// principale, 01/11/2026). Un POST forgiato con un token CSRF valido preso da un'altra pagina, o con
// un segno di un'altra busta, già usato o scaduto, mostra la scheda come un'apertura vera: una riga
// repair.viewed e la conversazione segnata letta; il redirect allora non ha il segno.
func TestRifiutoSenzaSegnoDellaPaginaScriveLaRiga(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	hv, dv, ov := facciaConBusteE(t, nil, "")
	id := bustaScheda(t, d, o, nil)
	altra := bustaScheda(t, d, o, nil)
	chiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, chiusa)
	guasta := bustaScheda(t, d, o, nil)
	guastaChiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, guastaChiusa)
	for _, g := range []int64{guasta, guastaChiusa} {
		eseguiSQL(t, d, `CREATE TRIGGER blocca_guasta_`+strconv.FormatInt(g, 10)+` BEFORE INSERT ON eventi_audit WHEN NEW.id_risorsa = '`+strconv.FormatInt(g, 10)+`' AND NEW.azione IN ('repair.updated', 'repair.status_changed', 'repair.public_link_revoked', 'repair.public_link_issued', 'repair.waiting_started', 'repair.reopened') BEGIN SELECT RAISE(ABORT, 'no'); END`)
	}
	senza := bustaScheda(t, dv, ov, nil)
	tit, dip, titSenza := titolareDentro(t, h), dipendenteDentro(t, h), titolareDentro(t, hv)
	casi := []rifiutoProva{
		{"attesa", dip, id, azioneAttesa(id), func(b *browser) url.Values { return formAttesa(b, "boh", "") }, "Azione non valida.", false},
		{"stato uguale", dip, id, azioneStato(id), func(b *browser) url.Values { return formStato(b, "in_lavorazione") }, "", false},
		{"stato fuori grafo", dip, id, azioneStato(id), func(b *browser) url.Values { return formStato(b, "chiusa") }, msgTransizione, false},
		{"revoca del dipendente", dip, id, azioneRevoca(id), nil, msgSoloTitolareRevo, false},
		{"nuovo link del dipendente", dip, id, azioneNuovo(id), nil, msgSoloTitolareNuov, false},
		{"modifica rifiutata", dip, id, azioneModifica(id), func(b *browser) url.Values {
			return url.Values{"_csrf_token": {b.token}, "repair[description]": {""}}
		}, "", true},
		{"riapertura nei campi", dip, chiusa, azioneRiapri(chiusa), func(b *browser) url.Values { return formRiapri(b, "in_lavorazione", "") }, "", true},
		{"stato non riuscito", dip, guasta, azioneStato(guasta), func(b *browser) url.Values { return formStato(b, "preventivo") }, msgTransizione, false},
		{"togli data non riuscita", dip, guasta, percorsoScheda(guasta) + "/togli-data", nil, msgDataNonTolta, false},
		{"revoca non riuscita", tit, guasta, azioneRevoca(guasta), nil, "Link cliente non revocato.", false},
		{"nuovo link non riuscito", tit, guasta, azioneNuovo(guasta), nil, "Nuovo link cliente non generato.", false},
		{"riapertura non riuscita", dip, guastaChiusa, azioneRiapri(guastaChiusa), func(b *browser) url.Values { return formRiapri(b, "in_lavorazione", "Pietra") }, msgRiaperturaNonRiusci, false},
		{"attesa non riuscita", tit, guasta, azioneAttesa(guasta), func(b *browser) url.Values { return formAttesa(b, "on", "") }, "Azione non valida.", false},
	}
	for _, c := range casi {
		c := c
		t.Run(c.nome, func(t *testing.T) { provaRifiuto(t, d, o, c) })
	}
	t.Run("nuovo link senza indirizzo", func(t *testing.T) {
		provaRifiuto(t, dv, ov, rifiutoProva{"nuovo link senza indirizzo", titSenza, senza, azioneNuovo(senza), nil, "Nuovo link cliente non generato: manca", false})
	})
	// Un segno di un'altra busta, già usato o scaduto vale come nessun segno.
	b := dip
	for _, c := range []struct {
		nome  string
		segno func() string
	}{
		{"altra busta", func() string { b.vai("GET", percorsoScheda(altra), nil, nil); return b.segno }},
		{"già usato", func() string {
			b.vai("GET", percorsoScheda(id), nil, nil)
			s := b.segno
			b.vai("GET", b.vai("POST", azioneAttesa(id), formAttesa(b, "boh", ""), nil).location, nil, nil)
			return s
		}},
		{"scaduto", func() string {
			b.vai("GET", percorsoScheda(id), nil, nil)
			s := b.segno
			o.Sposta(durataSegnoLink)
			return s
		}},
		{"inventato", func() string { b.vai("GET", percorsoScheda(id), nil, nil); return casuale() }},
	} {
		s := c.segno()
		b.vai("GET", "/", nil, nil)
		prima := viste(t, d)
		r := b.vai("POST", azioneAttesa(id), url.Values{"_csrf_token": {b.token}, "waiting[azione]": {"boh"}, "mostra": {s}}, nil)
		if !redirectA(r, percorsoScheda(id)) {
			t.Fatalf("%s: %d %q", c.nome, r.codice, r.location)
		}
		b.vai("GET", r.location, nil, nil)
		if n := viste(t, d) - prima; n != 1 {
			t.Errorf("%s: %d righe, voluta 1", c.nome, n)
		}
	}
}

// provaRifiuto manda il rifiuto con il segno della pagina (nessuna riga, niente segnato letto) e
// forgiato senza segno, con il token di un'altra pagina (una riga e la conversazione letta).
func provaRifiuto(t *testing.T, d *db.DB, o *clock.Finto, c rifiutoProva) {
	t.Helper()
	nuovoMessaggio := func() string {
		o.Sposta(time.Second)
		eseguiSQL(t, d, `UPDATE buste SET ultimo_messaggio_il = ? WHERE id = ?`, o.Adesso().Format("2006-01-02T15:04:05.000000Z"), c.busta)
		return colonna(t, d, c.busta, "messaggi_letti_il")
	}
	form := func() url.Values {
		if c.form != nil {
			return c.form(c.b)
		}
		return url.Values{"_csrf_token": {c.b.token}}
	}
	mostra := func(r risposta) risposta {
		t.Helper()
		if c.ridisegna {
			if r.codice != 200 {
				t.Fatalf("ridisegno: %d %q", r.codice, r.location)
			}
			return r
		}
		return c.b.vai("GET", r.location, nil, nil)
	}
	for _, conSegno := range []bool{true, false} {
		if conSegno {
			c.b.vai("GET", percorsoScheda(c.busta), nil, nil)
		} else {
			c.b.vai("GET", "/", nil, nil) // un token CSRF valido da un'altra pagina, nessun segno
		}
		letti := nuovoMessaggio()
		prima := viste(t, d)
		c.b.senzaSegno = !conSegno
		r := c.b.vai("POST", c.percorso, form(), nil)
		c.b.senzaSegno = false
		switch {
		case c.ridisegna:
		case conSegno && !redirectAllaScheda(r, c.busta), !conSegno && !redirectA(r, percorsoScheda(c.busta)):
			t.Fatalf("segno %v: %d %q", conSegno, r.codice, r.location)
		}
		m := mostra(r)
		if c.messaggio != "" {
			contiene(t, c.nome, m.corpo, escapeHTMLTest(c.messaggio))
		}
		righe, letto := 0, letti
		if !conSegno {
			righe, letto = 1, colonna(t, d, c.busta, "ultimo_messaggio_il")
		}
		if n := viste(t, d) - prima; n != righe {
			t.Errorf("segno %v: %d righe repair.viewed, volute %d", conSegno, n, righe)
		}
		if l := colonna(t, d, c.busta, "messaggi_letti_il"); l != letto {
			t.Errorf("segno %v: letto %s, voluto %s", conSegno, l, letto)
		}
	}
}

// inventario: RIP-S-01, RIP-S-04, RIP-S-14
// Il ridisegno di un rifiuto senza il segno della pagina è un'apertura: se repair.viewed non si
// scrive, la busta non si mostra e si torna alla home con «Impossibile aprire la riparazione.».
func TestRifiutoSenzaSegnoConLaperturaCheNonSiScrive(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaScheda(t, d, o, nil)
	chiusa := bustaScheda(t, d, o, nil)
	eseguiSQL(t, d, `UPDATE buste SET stato = 'consegnata', data_consegna = '2026-10-30' WHERE id = ?`, chiusa)
	eseguiSQL(t, d, `CREATE TRIGGER no_visto BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.viewed' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b := dipendenteDentro(t, h)
	b.senzaSegno = true
	for nome, c := range map[string]struct {
		percorso string
		form     url.Values
	}{
		"modifica":   {azioneModifica(id), url.Values{"repair[description]": {""}}},
		"riapertura": {azioneRiapri(chiusa), url.Values{"repair[status]": {"in_lavorazione"}, "repair[reopen_reason]": {""}}},
	} {
		b.vai("GET", "/", nil, nil)
		c.form.Set("_csrf_token", b.token)
		r := b.vai("POST", c.percorso, c.form, nil)
		if !redirectA(r, "/") {
			t.Fatalf("%s: %d %q", nome, r.codice, r.location)
		}
		contiene(t, nome, b.vai("GET", "/", nil, nil).corpo, msgBustaNonAperta)
	}
}
