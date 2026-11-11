package web

import (
	"regexp"
	"strings"
	"testing"
)

// «📲 Salva e avverti cliente» del modulo di modifica (slice 068), come then_notify/2 di RepairLive e i
// casi di repair_notice_live_test.exs (««📲 Salva e avverti cliente» (slice 187…)») dello standalone a
// go-baseline: salva come «Salva modifiche», poi apre il popup dell'avviso (slice 066) della busta
// appena salvata. Dati inventati.

// pulsanteSalvaAvverti è il pulsante del modulo, subito dopo «Salva modifiche».
const pulsanteSalvaAvverti = `<button id="save-repair" type="submit">Salva modifiche</button>` + "\n" +
	`<button id="save-and-notify" type="submit" name="then" value="notify" class="pulsante-salva-avverti">📲 Salva e avverti cliente</button>` + "\n" +
	`<a id="cancel-repair" href="`

var reVersoPopup = regexp.MustCompile(`^/repairs/(\d+)/avviso\?mostra=([A-Za-z0-9_-]{22})$`)

// salvaEAvverti apre il modulo, cambia i campi e lo manda con «📲 Salva e avverti cliente».
func salvaEAvverti(t *testing.T, b *browser, id int64, campi map[string]string) risposta {
	t.Helper()
	v := moduloDi(t, b.vai("GET", percorsoModifica(id), nil, nil).corpo)
	for k, val := range campi {
		v.Set(k, val)
	}
	v.Set("then", "notify")
	return b.vai("POST", azioneModifica(id), v, nil)
}

// inventario: RIP-S-04, CLI-29
// Una busta pronta con un numero: il modulo ha «📲 Salva e avverti cliente» dopo «Salva modifiche»;
// mandato, salva (prezzo, audit) e porta al popup della busta salvata, con il segno della scheda e
// «Riparazione aggiornata.»; aprire il popup non scrive niente (nessun messaggio, nessun evento
// dell'avviso), «Invia» sì. Vale per il dipendente e il titolare.
func TestSalvaEAvvertiApreIlPopup(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	for i, b := range []*browser{dipendenteDentro(t, h), titolareDentro(t, h)} {
		id := bustaHome(t, d, o, idTesto(int64(1+i))+"/26", "", "stato = 'pronta'")
		contiene(t, "pulsante", b.vai("GET", percorsoModifica(id), nil, nil).corpo, pulsanteSalvaAvverti)
		r := salvaEAvverti(t, b, id, map[string]string{"repair[final_price_euros]": "45,50"})
		m := reVersoPopup.FindStringSubmatch(r.location)
		if r.codice != 302 || m == nil || m[1] != idTesto(id) {
			t.Fatalf("salva e avverti: %d %q", r.codice, r.location)
		}
		if colonna(t, d, id, "prezzo_centesimi") != "4550" || colonna(t, d, id, "prezzo_testo") != "45,50" {
			t.Errorf("prezzo non salvato: %s %s", colonna(t, d, id, "prezzo_centesimi"), colonna(t, d, id, "prezzo_testo"))
		}
		if contaSQL(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.updated' AND id_risorsa = ?`, idTesto(id)) != 1 {
			t.Error("audit della modifica")
		}
		p := b.vai("GET", r.location, nil, nil)
		if p.codice != 200 {
			t.Fatalf("popup %d", p.codice)
		}
		contiene(t, "popup", p.corpo, `<div role="status" class="flash flash-info">Riparazione aggiornata.</div>`,
			`<h2 id="notice-title">📲 Avviso di ritiro</h2>`, `<input name="tipo" type="hidden" value="ready">`,
			`<input name="mostra" type="hidden" value="`+m[2]+`">`, "è pronta per il ritiro",
			`<a id="notice-cancel" href="/repairs/`+idTesto(id)+`?mostra=`+m[2]+`" class="pulsante-contorno" data-avviso-chiudi>Annulla</a>`)
		if n := contaSQL(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?1) + (SELECT count(*) FROM eventi_avviso_pronta WHERE busta_id = ?1)`, id); n != 0 {
			t.Fatalf("aprire il popup ha scritto: %d", n)
		}
		f := moduloAvviso(t, p.corpo)
		f.Set("_csrf_token", b.token)
		f.Set("notice[body]", "Pronta, 45,50 €")
		if x := b.vai("POST", percorsoScheda(id)+"/avviso", f, nil); x.codice != 302 || !strings.HasPrefix(x.location, "https://wa.me/") {
			t.Fatalf("invia: %d %q", x.codice, x.location)
		}
		if contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 1 {
			t.Error("«Invia» non ha registrato")
		}
	}
}

// inventario: RIP-S-04, CLI-29
// Una busta pronta conto terzi: il popup è per l'oreficeria, con il suo numero.
func TestSalvaEAvvertiOreficeria(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome, telefono) VALUES (1, 'Il negozio', NULL), (2, 'Oreficeria Finta', '049 000')`)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta', oreficeria_id = 2")
	b := dipendenteDentro(t, h)
	r := salvaEAvverti(t, b, id, map[string]string{"repair[final_price_euros]": "30"})
	if r.codice != 302 || !reVersoPopup.MatchString(r.location) || colonna(t, d, id, "prezzo_centesimi") != "3000" {
		t.Fatalf("%d %q", r.codice, r.location)
	}
	contiene(t, "oreficeria", b.vai("GET", r.location, nil, nil).corpo, `<span>a &#43;39049000 (Oreficeria Finta)</span>`,
		`<input name="controparte" type="hidden" value="2">`)
	if contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ?`, id) != 0 {
		t.Error("ha scritto")
	}
}

// inventario: RIP-S-04, CLI-29
// «Salva modifiche» salva e torna alla scheda senza popup; un salvataggio rifiutato (descrizione vuota)
// con «then=notify» ridisegna il modulo con l'errore, senza popup e senza salvare; un «then» forgiato su
// una busta non pronta salva e torna alla scheda; una busta pronta che il salvataggio lascia senza
// numero torna alla scheda.
func TestSalvaEAvvertiSoloDopoUnSalvataggioRiuscito(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	pronta := bustaHome(t, d, o, "1/26", "", "stato = 'pronta'")
	lav := bustaHome(t, d, o, "2/26", "", "")
	b := dipendenteDentro(t, h)
	if r := salva(t, b, pronta, map[string]string{"repair[final_price_euros]": "12"}); !redirectAllaScheda(r, pronta) {
		t.Fatalf("salva modifiche: %d %q", r.codice, r.location)
	}
	if colonna(t, d, pronta, "prezzo_centesimi") != "1200" {
		t.Error("non salvato")
	}
	r := salvaEAvverti(t, b, pronta, map[string]string{"repair[description]": "", "repair[final_price_euros]": "99"})
	if r.codice != 200 || colonna(t, d, pronta, "prezzo_centesimi") != "1200" {
		t.Fatalf("rifiuto: %d %q %s", r.codice, r.location, colonna(t, d, pronta, "prezzo_centesimi"))
	}
	contiene(t, "rifiuto", r.corpo, `id="repair-form"`, pulsanteSalvaAvverti)
	nonContiene(t, "rifiuto", r.corpo, `id="notice-popup"`, `id="notice-form"`)
	r = salvaEAvverti(t, b, lav, map[string]string{"repair[final_price_euros]": "9"})
	if !redirectAllaScheda(r, lav) || colonna(t, d, lav, "prezzo_centesimi") != "900" {
		t.Fatalf("forgiato: %d %q", r.codice, r.location)
	}
	r = salvaEAvverti(t, b, pronta, map[string]string{"repair[customer_phone_snapshot]": ""})
	if !redirectAllaScheda(r, pronta) || colonna(t, d, pronta, "telefono_cliente") != "" {
		t.Fatalf("senza numero dopo il salvataggio: %d %q", r.codice, r.location)
	}
	if contaRighe(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita) + (SELECT count(*) FROM eventi_avviso_pronta)`) != 0 {
		t.Error("ha scritto un avviso")
	}
}

// inventario: RIP-S-04, CLI-29
// Il pulsante c'è solo su una riparazione pronta con qualcuno da avvisare (send_address): non su una
// busta in lavorazione, su una pronta senza telefono, né su una compro oro valutata.
func TestSalvaEAvvertiSoloSuUnaBustaPronta(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	b := titolareDentro(t, h)
	for i, colonne := range []string{``, `stato = 'pronta', telefono_cliente = ''`, `stato = 'pronta', telefono_cliente = 'n/d'`,
		`stato = 'valutata', compro_oro = 1`} {
		id := bustaHome(t, d, o, idTesto(int64(1+i))+"/26", "", colonne)
		corpo := b.vai("GET", percorsoModifica(id), nil, nil).corpo
		contiene(t, colonne, corpo, `id="repair-form"`)
		nonContiene(t, colonne, corpo, "save-and-notify", "Salva e avverti")
	}
}

// inventario: RIP-S-04
// Con JavaScript il modulo spegne i pulsanti al primo invio (data-invio-unico): un pulsante spento non
// manda il suo valore, quindi app.js copia nome e valore del pulsante premuto in un campo nascosto prima
// di spegnerlo, e «then=notify» arriva lo stesso.
func TestScriptSalvaEAvverti(t *testing.T) {
	h, d, o := facciaConBusteE(t, nil, indirizzoPubblicoProva)
	id := bustaHome(t, d, o, "1/26", "", "stato = 'pronta'")
	b := dipendenteDentro(t, h)
	js := assetDellaPagina(t, b, b.vai("GET", percorsoModifica(id), nil, nil).corpo, "js", "text/javascript; charset=utf-8")
	for _, s := range []string{
		"  const premuto = event.submitter\n  if (premuto && premuto.name && !premuto.disabled) {\n    const copia = document.createElement(\"input\")\n" +
			"    copia.type = \"hidden\"\n    copia.name = premuto.name\n    copia.value = premuto.value\n    copia.dataset.copiaPulsante = \"1\"\n    form.appendChild(copia)\n  }\n" +
			"  form.querySelectorAll(\"button[type=submit]\").forEach(button => { button.disabled = true })",
		"    form.querySelectorAll(\"input[data-copia-pulsante]\").forEach(copia => copia.remove())",
	} {
		if !strings.Contains(js, s) {
			t.Errorf("app.js non contiene %q", s)
		}
	}
}
