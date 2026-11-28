package web

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// «🗑 Sposta nel cestino» sulla scheda (slice 136, RIP-S-49), come il pulsante #trash-repair e l'evento trash di
// RepairLive e trash_live_test.exs («a bag goes to the trash from its own page, and the confirmation tells the truth»)
// dello standalone a go-baseline, senza LiveView: un modulo POST con CSRF e la conferma di app.js (data-confirm). Nomi e
// dettati inventati.

func azioneCestino(id int64) string { return percorsoScheda(id) + "/cestino" }

func postCestino(b *browser, id int64) risposta {
	return b.vai("POST", azioneCestino(id), url.Values{"_csrf_token": {b.token}}, nil)
}

// sezioneCestino è la sezione in fondo alla scheda, con il segno della scheda mostrata.
func sezioneCestino(b *browser, id int64, numero string, giorni int) string {
	sid := strconv.FormatInt(id, 10)
	return `<section id="repair-trash" class="riquadro-cestino">` + "\n" +
		`<form action="/repairs/` + sid + `/cestino" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="` + b.token +
		`"><input name="mostra" type="hidden" value="` + b.segno + `"><button id="trash-repair" type="submit" class="pulsante-cestino" data-confirm="Spostare la busta ` +
		numero + ` nel cestino? Resta recuperabile per ` + strconv.Itoa(giorni) + ` giorni.">🗑 Sposta nel cestino</button></form>` + "\n" + `</section>`
}

// inventario: RIP-S-49
// Titolare e dipendente vedono «🗑 Sposta nel cestino» in fondo alla scheda, dopo il riquadro del link, con la conferma
// dello standalone: il numero visibile (D-64: «N. 1/26» senza etichetta, «#0045/26» con) e i giorni della finestra
// (30 di base, quelli dell'impostazione se c'è).
func TestSchedaCestinoPulsante(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	c := p.busta(t, "Ofelia Pagina", "QR-136-A", false)
	etichetta := p.busta(t, "Altro Pagina", "RIP-2026-0045", false)
	for nome, b := range map[string]*browser{"titolare": titolareDentro(t, p.h), "dipendente": dipendenteDentro(t, p.h)} {
		r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
		contiene(t, nome, r.corpo, sezioneCestino(b, c.ID, "N. "+c.Numero, 30))
		if i, j := strings.Index(r.corpo, `id="repair-public-link"`), strings.Index(r.corpo, `id="repair-trash"`); i < 0 || j < 0 || i > j {
			t.Errorf("%s: ordine dei riquadri: %d %d", nome, i, j)
		}
		r = b.vai("GET", percorsoScheda(etichetta.ID), nil, nil)
		contiene(t, nome+" etichetta", r.corpo, sezioneCestino(b, etichetta.ID, "#0045/26", 30))
	}
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('trash.retention_days', '7', '2026-11-20T09:00:00.000000Z')`)
	b := dipendenteDentro(t, p.h)
	contiene(t, "sette giorni", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, sezioneCestino(b, c.ID, "N. "+c.Numero, 7))
}

// inventario: RIP-S-49, RIP-S-50, RIP-H-51
// Il POST (titolare o dipendente) sposta la busta: la home con «Busta spostata nel cestino.», la riga repair.trashed
// dell'utente, il dettato raccolto cancellato (quello di un'altra busta resta), la campanella che non la conta più, la
// scheda che non si apre («Riparazione non trovata.») e la busta fuori dalla lista; un secondo POST è «Busta non spostata
// nel cestino.» senza scrivere niente.
func TestSchedaCestinoSposta(t *testing.T) {
	for _, chi := range []string{"titolare", "dipendente"} {
		t.Run(chi, func(t *testing.T) {
			p := facciaConRaccolta(t, nil, nil)
			p.raccoltaAccesaUnaVolta(t, "1")
			c := p.busta(t, "Ofelia Pagina", "QR-136-B", true)
			altra := p.busta(t, "Altro Pagina", "QR-136-C", true)
			eseguiSQL(t, p.d, `UPDATE buste SET ultimo_messaggio_il = '2026-11-21T08:00:00.000000Z' WHERE id IN (?, ?)`, c.ID, altra.ID)
			b, attore := titolareDentro(t, p.h), "titolare@esempio.it"
			if chi == "dipendente" {
				b, attore = dipendenteDentro(t, p.h), "banco@esempio.it"
			}
			contiene(t, "prima", b.vai("GET", "/", nil, nil).corpo, `<div id="nav-bell-wrap" data-count="2">`, "Ofelia Pagina")
			b.vai("GET", percorsoScheda(c.ID), nil, nil)
			if r := postCestino(b, c.ID); !redirectA(r, "/") {
				t.Fatalf("cestino: %d %q", r.codice, r.location)
			}
			r := b.vai("GET", "/", nil, nil)
			contiene(t, "home", r.corpo, `<div role="status" class="flash flash-info">Busta spostata nel cestino.</div>`,
				`<div id="nav-bell-wrap" data-count="1">`, "Altro Pagina")
			nonContiene(t, "home", r.corpo, "Ofelia Pagina")
			if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.trashed' AND attore = '`+attore+`'
				AND risorsa = 'repair' AND id_risorsa = '`+strconv.FormatInt(c.ID, 10)+`' AND dati = '{}'`); n != 1 {
				t.Errorf("audit %d", n)
			}
			if contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = `+strconv.FormatInt(c.ID, 10)) != 0 ||
				contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = `+strconv.FormatInt(altra.ID, 10)) != 1 {
				t.Error("dettati")
			}
			if r := b.vai("GET", percorsoScheda(c.ID), nil, nil); !redirectA(r, "/") {
				t.Errorf("scheda nel cestino: %d %q", r.codice, r.location)
			}
			contiene(t, "scheda", b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
			prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.trashed'`)
			if r := postCestino(b, c.ID); !redirectA(r, "/") {
				t.Fatalf("di nuovo: %d %q", r.codice, r.location)
			}
			contiene(t, "di nuovo", b.vai("GET", "/", nil, nil).corpo, `<div role="alert" class="flash flash-errore">Busta non spostata nel cestino.</div>`)
			if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.trashed'`); n != prima {
				t.Errorf("il secondo POST ha scritto %d righe", n-prima)
			}
		})
	}
}

// inventario: RIP-S-49
// Una busta che non c'è o un id malformato: la home con «Busta non spostata nel cestino.»; senza CSRF 403 e niente si
// sposta; un guasto (l'audit che non si scrive) torna alla scheda con lo stesso messaggio, una riga fissa nel registro
// senza numeri né il testo dell'errore, e la busta resta fuori dal cestino con il suo dettato.
func TestSchedaCestinoRifiuti(t *testing.T) {
	var reg bytes.Buffer
	p := facciaConRaccolta(t, &reg, nil)
	p.raccoltaAccesaUnaVolta(t, "1")
	c := p.busta(t, "Ofelia Pagina", "QR-136-D", true)
	b := dipendenteDentro(t, p.h)
	for _, percorso := range []string{azioneCestino(c.ID + 100), "/repairs/0/cestino", "/repairs/x/cestino", "/repairs/-1/cestino"} {
		b.vai("GET", percorsoScheda(c.ID), nil, nil)
		if r := b.vai("POST", percorso, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", percorso, r.codice, r.location)
		}
		contiene(t, percorso, b.vai("GET", "/", nil, nil).corpo, "Busta non spostata nel cestino.")
	}
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := b.vai("POST", azioneCestino(c.ID), url.Values{}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	if r := b.vai("POST", azioneCestino(c.ID), url.Values{"_csrf_token": {"falso"}}, nil); r.codice != 403 {
		t.Errorf("CSRF falso: %d", r.codice)
	}
	eseguiSQL(t, p.d, `CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'repair.trashed' BEGIN SELECT RAISE(ABORT, 'guasto-prova-136'); END`)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := postCestino(b, c.ID); !redirectAllaScheda(r, c.ID) {
		t.Fatalf("guasto: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "guasto", r.corpo, `<div role="alert" class="flash flash-errore">Busta non spostata nel cestino.</div>`, `id="trash-repair"`)
	if contaRighe(t, p.d, `SELECT count(*) FROM buste WHERE cancellata_il IS NOT NULL`) != 0 ||
		contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`) != 1 {
		t.Error("spostata o cancellata con il guasto")
	}
	contiene(t, "registro", reg.String(), "busta non spostata nel cestino")
	nonContiene(t, "registro", reg.String(), c.Numero, "Ofelia", "guasto-prova-136")
}
