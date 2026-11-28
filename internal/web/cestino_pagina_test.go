package web

import (
	"bytes"
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
)

// La pagina «🗑 Cestino» e il ripristino (slice 137, RIP-S-51, RIP-S-52), come TrashLive e trash_live_test.exs dello
// standalone a go-baseline, senza LiveView: una pagina GET e un modulo POST con CSRF e data-confirm. Nomi inventati.
// L'orologio della prova è fermo al 22/11/2026 09:00 UTC (10:00 a Roma).

func azioneRipristinoBusta(id int64) string {
	return "/cestino/repair/" + strconv.FormatInt(id, 10) + "/ripristina"
}

func postRipristino(b *browser, id int64) risposta {
	return b.vai("POST", azioneRipristinoBusta(id), url.Values{"_csrf_token": {b.token}}, nil)
}

// buttaVia mette la busta nel cestino con il dominio (lo spostamento dalla scheda lo prova la 136).
func (p provaRaccolta) buttaVia(t *testing.T, id int64) {
	t.Helper()
	if _, err := p.b.SpostaNelCestino(context.Background(), id, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
}

// rigaCestino è la riga della pagina per una busta, con il pulsante del ripristino.
func rigaCestino(b *browser, id int64, numero, stato, il string, giorni int, scaduta bool) string {
	sid := strconv.FormatInt(id, 10)
	s := `<li id="trash-repair-` + sid + `" class="cestino-voce">` + "\n" +
		`<div class="cestino-testo"><span class="cestino-tipo">Busta</span> <span id="trash-label-repair-` + sid + `" class="forte">` + numero + `</span>` + "\n" +
		`<div class="sotto">Collana da saldare · ` + stato + ` · cestinato il ` + il + `</div></div>` + "\n" +
		`<div class="cestino-giorni"><div id="trash-days-repair-` + sid + `" class="cestino-numero">` + strconv.Itoa(giorni) + `</div><div class="cestino-unita">giorni</div></div>` + "\n"
	if scaduta {
		s += `<span class="cestino-scaduta">Finestra scaduta</span>` + "\n"
	}
	return s + `<form action="/cestino/repair/` + sid + `/ripristina" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="` + b.token +
		`"><button id="restore-repair-` + sid + `" type="submit" class="pulsante-contorno" data-confirm="Riportare questo elemento fra quelli attivi?">↩ Ripristina</button></form>` + "\n" + `</li>`
}

const fraseCestino = `<h1>🗑 Cestino</h1>` + "\n" + `<p class="sotto">Quello che viene cestinato resta recuperabile per %d giorni. Passata la finestra non sparisce da solo: resta qui, ancora intero, in attesa che il titolare decida.</p>`

func frase(giorni int) string { return strings.Replace(fraseCestino, "%d", strconv.Itoa(giorni), 1) }

// inventario: RIP-S-51
// Cestino vuoto, per titolare e dipendente: il titolo «Cestino · Laboratorio», la frase con la finestra e «Il cestino è
// vuoto.», senza lista; la voce «🗑 Cestino» nel menu.
func TestPaginaCestinoVuota(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	for nome, b := range map[string]*browser{"titolare": titolareDentro(t, p.h), "dipendente": dipendenteDentro(t, p.h)} {
		r := b.vai("GET", "/cestino", nil, nil)
		if r.codice != 200 {
			t.Fatalf("%s: %d", nome, r.codice)
		}
		contiene(t, nome, r.corpo, "<title>Cestino · Laboratorio</title>", frase(30),
			`<div id="trash-empty" class="riquadro cestino-vuoto">Il cestino è vuoto.</div>`, `href="/cestino"`)
		nonContiene(t, nome, r.corpo, `id="trash-list"`)
	}
}

// inventario: RIP-S-51, RIP-S-50
// Le buste nel cestino, dalla più recente: tipo, numero visibile (D-64), descrizione, stato, ora del negozio del
// cestinamento, giorni rimasti (della finestra dell'impostazione), «Finestra scaduta» a zero e «↩ Ripristina» per
// titolare e dipendente. Nessun link alla scheda e mai il token del link del cliente (né il link /r/); la busta fuori
// dal cestino non c'è.
func TestPaginaCestinoRighe(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	vecchia := p.busta(t, "Ofelia Pagina", "QR-137-A", false)
	nuova := p.busta(t, "Altro Pagina", "RIP-2026-0045", false)
	resta := p.busta(t, "Terzo Pagina", "QR-137-C", false)
	ultimo := p.busta(t, "Quarto Pagina", "QR-137-D", false)
	p.buttaVia(t, vecchia.ID)
	p.buttaVia(t, nuova.ID)
	p.buttaVia(t, ultimo.ID)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-24T08:00:00.000000Z' WHERE id = ?`, ultimo.ID)  // 29 giorni fa: ne resta 1
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-01T22:30:00.000000Z' WHERE id = ?`, vecchia.ID) // 2 ottobre 00:30 a Roma
	eseguiSQL(t, p.d, `UPDATE buste SET stato = 'pronta', data_pronta = '2026-11-21' WHERE id = ?`, nuova.ID)
	for nome, b := range map[string]*browser{"titolare": titolareDentro(t, p.h), "dipendente": dipendenteDentro(t, p.h)} {
		r := b.vai("GET", "/cestino", nil, nil)
		uno := rigaCestino(b, nuova.ID, "0045/26", "Pronta", "22/11/2026 10:00", 30, false)
		uno2 := rigaCestino(b, ultimo.ID, "N. "+ultimo.Numero, "In lavorazione", "24/10/2026 10:00", 1, false)
		due := rigaCestino(b, vecchia.ID, "N. "+vecchia.Numero, "In lavorazione", "02/10/2026 00:30", 0, true)
		contiene(t, nome, r.corpo, frase(30), `<ul id="trash-list" class="cestino-lista">`+"\n"+uno+"\n"+uno2+"\n"+due+"\n</ul>")
		nonContiene(t, nome, r.corpo, `id="trash-empty"`, "Terzo Pagina", `href="/repairs/`, vecchia.Token, nuova.Token, resta.Token, "/r/")
	}
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('trash.retention_days', '60', '2026-11-20T09:00:00.000000Z')`)
	b := dipendenteDentro(t, p.h)
	r := b.vai("GET", "/cestino", nil, nil)
	contiene(t, "60 giorni", r.corpo, frase(60), rigaCestino(b, nuova.ID, "0045/26", "Pronta", "22/11/2026 10:00", 60, false),
		rigaCestino(b, ultimo.ID, "N. "+ultimo.Numero, "In lavorazione", "24/10/2026 10:00", 31, false),
		rigaCestino(b, vecchia.ID, "N. "+vecchia.Numero, "In lavorazione", "02/10/2026 00:30", 9, false))
	nonContiene(t, "stato salvato", r.corpo, "in_lavorazione", "· pronta ·")
}

// inventario: RIP-S-51
// Il token del link del cliente non compare mai nella pagina, nemmeno con una busta il cui link è stato inviato e
// mostrato sulla scheda prima del cestino.
func TestPaginaCestinoSenzaToken(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	c := p.busta(t, "Ofelia Pagina", "QR-137-T", false)
	if c.Token == "" {
		t.Fatal("busta senza token")
	}
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	p.buttaVia(t, c.ID)
	r := b.vai("GET", "/cestino", nil, nil)
	contiene(t, "riga", r.corpo, `id="trash-repair-`+strconv.FormatInt(c.ID, 10)+`"`)
	nonContiene(t, "token", r.corpo, c.Token, url.PathEscape(c.Token), url.QueryEscape(c.Token))
}

// inventario: RIP-S-52, RIP-S-50
// «↩ Ripristina» (titolare o dipendente): la pagina con «Ripristinato.» e il cestino vuoto, la riga repair.restored
// dell'utente, la busta di nuovo nella home (e nel suo contatore) e la scheda che si apre; un secondo POST torna alla
// pagina con «Busta non ripristinata.» (D-126) e senza scrivere.
func TestPaginaCestinoRipristina(t *testing.T) {
	for _, chi := range []string{"titolare", "dipendente"} {
		t.Run(chi, func(t *testing.T) {
			p := facciaConRaccolta(t, nil, nil)
			c := p.busta(t, "Ofelia Pagina", "QR-137-R", false)
			p.busta(t, "Altro Pagina", "QR-137-S", false)
			b, attore := titolareDentro(t, p.h), "titolare@esempio.it"
			if chi == "dipendente" {
				b, attore = dipendenteDentro(t, p.h), "banco@esempio.it"
			}
			p.buttaVia(t, c.ID)
			nonContiene(t, "home prima", b.vai("GET", "/", nil, nil).corpo, "Ofelia Pagina")
			b.vai("GET", "/cestino", nil, nil)
			if r := postRipristino(b, c.ID); !redirectA(r, "/cestino") {
				t.Fatalf("ripristino: %d %q", r.codice, r.location)
			}
			r := b.vai("GET", "/cestino", nil, nil)
			contiene(t, "pagina", r.corpo, `<div role="status" class="flash flash-info">Ripristinato.</div>`, `id="trash-empty"`)
			if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.restored' AND attore = '`+attore+`'
				AND risorsa = 'repair' AND id_risorsa = '`+strconv.FormatInt(c.ID, 10)+`' AND dati = '{}'`); n != 1 {
				t.Errorf("audit %d", n)
			}
			contiene(t, "home", b.vai("GET", "/", nil, nil).corpo, "Ofelia Pagina", "Altro Pagina")
			if r := b.vai("GET", percorsoScheda(c.ID), nil, nil); r.codice != 200 {
				t.Errorf("scheda: %d %q", r.codice, r.location)
			}
			if r := postRipristino(b, c.ID); !redirectA(r, "/cestino") {
				t.Fatalf("di nuovo: %d %q", r.codice, r.location)
			}
			r = b.vai("GET", "/cestino", nil, nil)
			contiene(t, "di nuovo", r.corpo, `<div role="status" class="flash flash-info">Busta non ripristinata.</div>`)
			nonContiene(t, "di nuovo", r.corpo, "Ripristinato.", "flash-errore")
			if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.restored'`); n != 1 {
				t.Errorf("il secondo POST ha scritto: %d righe", n)
			}
		})
	}
}

// inventario: RIP-S-52
// Rifiuti: id malformati, che non c'è, di una busta fuori dal cestino: la pagina con «Busta non ripristinata.» (D-126) e niente scritto; senza
// CSRF o con un CSRF falso 403; una famiglia che il Go non ha (vendite: T4.12) non ha rotta; un guasto (l'audit che non si
// scrive) torna alla pagina con «Busta non ripristinata.», con una riga fissa nel registro senza numero, nome né testo dell'errore, e la
// busta resta nel cestino.
func TestPaginaCestinoRifiuti(t *testing.T) {
	var reg bytes.Buffer
	p := facciaConRaccolta(t, &reg, nil)
	c := p.busta(t, "Ofelia Pagina", "QR-137-X", false)
	fuori := p.busta(t, "Altro Pagina", "QR-137-Y", false)
	p.buttaVia(t, c.ID)
	b := dipendenteDentro(t, p.h)
	b.vai("GET", "/cestino", nil, nil)
	nelCestino := func(cosa string) {
		t.Helper()
		if contaRighe(t, p.d, `SELECT count(*) FROM buste WHERE cancellata_il IS NOT NULL AND id = `+strconv.FormatInt(c.ID, 10)) != 1 ||
			contaRighe(t, p.d, `SELECT count(*) FROM buste WHERE cancellata_il IS NOT NULL`) != 1 ||
			contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.restored'`) != 0 {
			t.Errorf("%s: qualcosa è cambiato", cosa)
		}
	}
	for _, percorso := range []string{azioneRipristinoBusta(c.ID + 100), azioneRipristinoBusta(fuori.ID), "/cestino/repair/0/ripristina",
		"/cestino/repair/x/ripristina", "/cestino/repair/-1/ripristina", "/cestino/repair/1;drop/ripristina"} {
		if r := b.vai("POST", percorso, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/cestino") {
			t.Errorf("%s: %d %q", percorso, r.codice, r.location)
		}
		r := b.vai("GET", "/cestino", nil, nil)
		contiene(t, percorso, r.corpo, `<div role="status" class="flash flash-info">Busta non ripristinata.</div>`)
		nonContiene(t, percorso, r.corpo, "Ripristinato.", "flash-errore")
	}
	nelCestino("id")
	if r := b.vai("POST", azioneRipristinoBusta(c.ID), url.Values{}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if r := b.vai("POST", azioneRipristinoBusta(c.ID), url.Values{"_csrf_token": {"falso"}}, nil); r.codice != 403 {
		t.Errorf("CSRF falso: %d", r.codice)
	}
	if r := b.vai("POST", "/cestino/sale/"+strconv.FormatInt(c.ID, 10)+"/ripristina", url.Values{"_csrf_token": {b.token}}, nil); r.codice == 302 {
		t.Errorf("famiglia sconosciuta: %d %q", r.codice, r.location)
	}
	nelCestino("CSRF e famiglia")
	eseguiSQL(t, p.d, `CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.restored' BEGIN SELECT RAISE(ABORT, 'guasto-prova-137'); END`)
	if r := postRipristino(b, c.ID); !redirectA(r, "/cestino") {
		t.Fatalf("guasto: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", "/cestino", nil, nil)
	contiene(t, "guasto", r.corpo, `id="restore-repair-`+strconv.FormatInt(c.ID, 10)+`"`)
	contiene(t, "guasto", r.corpo, `<div role="status" class="flash flash-info">Busta non ripristinata.</div>`)
	nonContiene(t, "guasto", r.corpo, "Ripristinato.")
	nelCestino("guasto")
	contiene(t, "registro", reg.String(), "elemento non ripristinato dal cestino")
	nonContiene(t, "registro", reg.String(), c.Numero, "Ofelia", "guasto-prova-137")
}

// inventario: RIP-S-51
// Ogni famiglia del cestino ha il suo cartellino (family_label/1): una famiglia nuova senza la sua voce in tipiCestino
// fa fallire il test, invece di disegnare una riga senza tipo.
func TestTipiCestinoPerOgniFamiglia(t *testing.T) {
	for _, f := range buste.FamiglieCestino() {
		if strings.TrimSpace(tipiCestino[f]) == "" {
			t.Errorf("famiglia %q senza cartellino", f)
		}
	}
	if len(tipiCestino) != len(buste.FamiglieCestino()) {
		t.Errorf("tipiCestino ha %d voci, le famiglie sono %d", len(tipiCestino), len(buste.FamiglieCestino()))
	}
}

// inventario: RIP-S-51
// D-125: lo stato con l'etichetta delle altre pagine (etichettaStato), anche per gli stati del compro oro.
func TestPaginaCestinoEtichettaStato(t *testing.T) {
	for stato, atteso := range map[string]string{"in_lavorazione": "In lavorazione", "in_attesa_materiali": "In attesa materiali",
		"pronta": "Pronta", "in_valutazione": "In valutazione", "consegnata": "Consegnata"} {
		if got := etichettaStato(stato); got != atteso {
			t.Errorf("%s: %q, atteso %q", stato, got, atteso)
		}
	}
}
