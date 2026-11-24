package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// «🎙️ Escludi dalla raccolta dettati» sulla scheda (slice 115, RIP-S-26), come la sezione #repair-training e l'evento
// training-exclude-customer di RepairLive e repair_sheet_ux_live_test.exs («Escludi dalla raccolta dettati» on the sheet)
// dello standalone a go-baseline, senza LiveView: un modulo POST con CSRF e la conferma di app.js (data-confirm).
// Dettati, nomi e telefoni inventati.

const confermaEscludiScheda = "Escludere questo cliente dalla raccolta dei dettati? I dettati di tutte le sue buste vengono cancellati e le sue buste future non saranno raccolte."

func azioneEscludiScheda(id int64) string { return percorsoScheda(id) + "/escludi-raccolta" }

// raccoltaAccesaUnaVolta scrive la chiave del lotto (la prima accensione), con la raccolta accesa o spenta.
func (p provaRaccolta) raccoltaAccesaUnaVolta(t *testing.T, accesa string) {
	t.Helper()
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', ?, '2026-11-20T09:00:00.000000Z'),
		('assist.training.remaining', '500', '2026-11-20T09:00:00.000000Z')`, accesa)
}

func postEscludiScheda(b *browser, id int64) risposta {
	return b.vai("POST", azioneEscludiScheda(id), url.Values{"_csrf_token": {b.token}}, nil)
}

// inventario: RIP-S-26
// Con la raccolta mai accesa la sezione non c'è, nemmeno per il titolare.
func TestSchedaRaccoltaMaiAccesa(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	c := p.busta(t, "Ofelia Pagina", "QR-115-A", false)
	b := titolareDentro(t, p.h)
	nonContiene(t, "mai accesa", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo, `id="repair-training"`, "Escludi dalla raccolta")
}

// inventario: RIP-S-26
// Il titolare, con la raccolta accesa almeno una volta (anche se adesso è spenta), vede il pulsante con la conferma; il
// POST torna alla scheda con il messaggio dello standalone e il numero dei dettati cancellati; poi la scheda dice
// «Cliente escluso…» senza pulsante, anche sulle altre buste del cliente, non su quelle di un altro cliente.
func TestSchedaRaccoltaEscludiTitolare(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	p.raccoltaAccesaUnaVolta(t, "1")
	c := p.busta(t, "Ofelia Pagina", "QR-115-B", true)
	sorella := p.busta(t, "Ofelia Pagina", "QR-115-C", true)
	altro := p.busta(t, "Altro Pagina", "QR-115-D", true)
	eseguiSQL(t, p.d, `UPDATE impostazioni SET valore = '0' WHERE chiave = 'assist.training.collect'`)
	b := titolareDentro(t, p.h)
	sid := strconv.FormatInt(c.ID, 10)
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "pulsante", r.corpo,
		`<section id="repair-training" class="riquadro riquadro-link">`+"\n"+
			`<form action="/repairs/`+sid+`/escludi-raccolta" method="post" data-invio-unico><input name="_csrf_token" type="hidden" value="`+b.token+`"><input name="mostra" type="hidden" value="`+b.segno+`">`+
			`<button id="repair-training-exclude" type="submit" class="pulsante-contorno" data-confirm="`+confermaEscludiScheda+`">🎙️ Escludi dalla raccolta dettati</button></form>`+"\n"+
			`</section>`)
	nonContiene(t, "pulsante", r.corpo, `id="repair-training-excluded"`, dettatoPagina)
	// Come nello standalone, il riquadro viene subito prima di quello del link pubblico.
	if i, j := strings.Index(r.corpo, `id="repair-training"`), strings.Index(r.corpo, `id="repair-public-link"`); i < 0 || j < 0 || i > j {
		t.Errorf("ordine dei riquadri: %d %d", i, j)
	}

	if r := postEscludiScheda(b, c.ID); !redirectAllaScheda(r, c.ID) {
		t.Fatalf("esclusione: %d %q", r.codice, r.location)
	}
	r = b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "escluso", r.corpo,
		`<div role="status" class="flash flash-info">Cliente escluso dalla raccolta dettati: cancellati 2 dettati; le sue buste future non saranno raccolte.</div>`,
		`<section id="repair-training" class="riquadro riquadro-link">`+"\n"+
			`<p id="repair-training-excluded" class="stato-link">🎙️ Cliente escluso dalla raccolta dettati (opposizione).</p>`+"\n"+`</section>`)
	nonContiene(t, "escluso", r.corpo, `id="repair-training-exclude"`)
	contiene(t, "sorella", b.vai("GET", percorsoScheda(sorella.ID), nil, nil).corpo, `id="repair-training-excluded"`)
	r = b.vai("GET", percorsoScheda(altro.ID), nil, nil)
	contiene(t, "altro cliente", r.corpo, `id="repair-training-exclude"`)
	nonContiene(t, "altro cliente", r.corpo, `id="repair-training-excluded"`)
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`) != 1 || contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento WHERE busta_id = `+strconv.FormatInt(altro.ID, 10)) != 1 {
		t.Error("righe rimaste")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_customer_excluded' AND attore = 'titolare@esempio.it'
		AND risorsa = 'customer' AND id_risorsa = '`+strconv.FormatInt(c.ClienteID, 10)+`' AND dati = '{"cancellati":2}'`); n != 1 {
		t.Errorf("audit %d", n)
	}
}

// inventario: RIP-S-26
// Il secondo tocco (un POST quando il cliente è già escluso, per esempio da una pagina aperta prima) torna alla scheda
// senza messaggio e non scrive niente; lo stesso con la raccolta mai accesa.
func TestSchedaRaccoltaSecondoTocco(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	mai := p.busta(t, "Altro Pagina", "QR-115-E", false)
	b := titolareDentro(t, p.h)
	b.vai("GET", percorsoScheda(mai.ID), nil, nil)
	if r := postEscludiScheda(b, mai.ID); !redirectAllaScheda(r, mai.ID) {
		t.Fatalf("mai accesa: %d %q", r.codice, r.location)
	}
	nonContiene(t, "mai accesa", b.vai("GET", percorsoScheda(mai.ID), nil, nil).corpo, "flash-info", "flash-errore", `id="repair-training"`)
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Error("escluso con la raccolta mai accesa")
	}

	p.raccoltaAccesaUnaVolta(t, "1")
	c := p.busta(t, "Ofelia Pagina", "QR-115-F", true)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	postEscludiScheda(b, c.ID)
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := postEscludiScheda(b, c.ID); !redirectAllaScheda(r, c.ID) {
		t.Fatalf("secondo tocco: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	nonContiene(t, "secondo tocco", r.corpo, "flash-info", "flash-errore")
	contiene(t, "secondo tocco", r.corpo, `id="repair-training-excluded"`)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != prima {
		t.Errorf("il secondo tocco ha scritto %d righe d'audit", n-prima)
	}
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("esclusioni")
	}
}

// inventario: RIP-S-26
// Il dipendente non vede la sezione (né il pulsante né «Cliente escluso…»); il POST forgiato torna alla scheda con «Solo
// il titolare può escludere un cliente.» e non scrive niente.
func TestSchedaRaccoltaDipendente(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	p.raccoltaAccesaUnaVolta(t, "1")
	c := p.busta(t, "Ofelia Pagina", "QR-115-G", true)
	escluso := p.busta(t, "Altro Pagina", "QR-115-H", false)
	eseguiSQL(t, p.d, `INSERT INTO dettati_esclusioni (cliente_id, creato_il) VALUES (?, '2026-11-20T09:00:00.000000Z')`, escluso.ClienteID)
	b := dipendenteDentro(t, p.h)
	for _, id := range []int64{c.ID, escluso.ID} {
		nonContiene(t, "dipendente", b.vai("GET", percorsoScheda(id), nil, nil).corpo, `id="repair-training`, "Escludi dalla raccolta", "Cliente escluso")
	}
	prima := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := postEscludiScheda(b, c.ID); !redirectAllaScheda(r, c.ID) {
		t.Fatalf("dipendente: %d %q", r.codice, r.location)
	}
	contiene(t, "dipendente", b.vai("GET", percorsoScheda(c.ID), nil, nil).corpo,
		`<div role="alert" class="flash flash-errore">Solo il titolare può escludere un cliente.</div>`)
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'assist.%'`); n != prima {
		t.Errorf("il dipendente ha scritto %d righe d'audit", n-prima)
	}
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`) != 1 || contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`) != 1 {
		t.Error("il dipendente ha cancellato o escluso")
	}
}

// inventario: RIP-S-26
// Una busta che non c'è, nel cestino o con un id malformato porta alla home con «Riparazione non trovata.»; senza CSRF il
// POST è rifiutato; un guasto torna alla scheda con «Esclusione non riuscita.» e niente si cancella.
func TestSchedaRaccoltaRifiuti(t *testing.T) {
	p := facciaConRaccolta(t, nil, nil)
	p.raccoltaAccesaUnaVolta(t, "1")
	c := p.busta(t, "Ofelia Pagina", "QR-115-I", true)
	cestino := p.busta(t, "Altro Pagina", "QR-115-J", true)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-11-21T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	b := titolareDentro(t, p.h)
	for _, percorso := range []string{azioneEscludiScheda(cestino.ID), azioneEscludiScheda(c.ID + 100), "/repairs/0/escludi-raccolta", "/repairs/x/escludi-raccolta"} {
		b.vai("GET", percorsoScheda(c.ID), nil, nil)
		if r := b.vai("POST", percorso, url.Values{"_csrf_token": {b.token}}, nil); !redirectA(r, "/") {
			t.Errorf("%s: %d %q", percorso, r.codice, r.location)
		}
		contiene(t, percorso, b.vai("GET", "/", nil, nil).corpo, "Riparazione non trovata.")
	}
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := b.vai("POST", azioneEscludiScheda(c.ID), url.Values{}, nil); r.codice != 403 {
		t.Errorf("senza CSRF: %d", r.codice)
	}
	eseguiSQL(t, p.d, `CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'assist.training_customer_excluded' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	b.vai("GET", percorsoScheda(c.ID), nil, nil)
	if r := postEscludiScheda(b, c.ID); !redirectAllaScheda(r, c.ID) {
		t.Fatalf("guasto: %d %q", r.codice, r.location)
	}
	r := b.vai("GET", percorsoScheda(c.ID), nil, nil)
	contiene(t, "guasto", r.corpo, `<div role="alert" class="flash flash-errore">Esclusione non riuscita.</div>`, `id="repair-training-exclude"`)
	if contaRighe(t, p.d, `SELECT count(*) FROM dettati_addestramento`) != 2 || contaRighe(t, p.d, `SELECT count(*) FROM dettati_esclusioni`) != 0 {
		t.Error("cancellato con il guasto")
	}
}
