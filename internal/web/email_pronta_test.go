package web

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/apparecchio"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Lo stato dell'avviso email nella scheda (#repair-email-notice di RepairLive, EmailNotice.sheet/1) e
// l'email di pronta che parte dal cambio di stato della scheda (CLI-42), come
// repair_email_notice_live_test.exs dello standalone (go-baseline), senza LiveView; più la D-16 (solo Go)
// e la riga della pagina dei provider senza indirizzo pubblico. Dati inventati, indirizzi .invalid.

// facciaConEmail è la faccia interna con le buste vere, la coda dell'email accesa (provider finto) e
// l'indirizzo pubblico.
func facciaConEmail(t *testing.T) (http.Handler, *db.DB, *clock.Finto, *postinoFinto, *uscita.Coda) {
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
	p := &postinoFinto{}
	coda := uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: p})
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o).ConPosta(coda, []byte("chiave-email-di-prova-di-32-byte")).ConIndirizzoPubblico(indirizzoPubblicoProva)
	dd.IndirizzoPubblico, dd.Configurazione = indirizzoPubblicoProva, configProva
	return mustInterna(t, dd), d, o, p, coda
}

const indirizzoScheda = "ada@esempio.invalid"

// riquadroEmailScheda è il riquadro #repair-email-notice della pagina, "" se non c'è.
func riquadroEmailScheda(corpo string) string {
	i := strings.Index(corpo, `<div id="repair-email-notice"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(corpo[i:], "</div>")
	return corpo[i : i+j+len("</div>")]
}

// inventario: CLI-42
// La scheda dice dove sta l'avviso email: niente senza indirizzo; l'indirizzo in attesa o confermato; per
// un indirizzo confermato della controparte «partirà da sola» prima di «pronta»; il cambio di stato della
// scheda accoda l'email e il riquadro segue la riga della coda (in coda, accettata, fallita), dice un
// mancato invio e il perché, il segno importato dallo standalone; nessuna promessa per l'indirizzo
// dell'altra parte, né con l'email non disponibile (D-16, con la sua riga).
func TestAvvisoEmailNellaScheda(t *testing.T) {
	h, d, o, p, coda := facciaConEmail(t)
	b := dipendenteDentro(t, h)
	scheda := func(id int64) string {
		t.Helper()
		return b.vai("GET", percorsoScheda(id), nil, nil).corpo
	}
	conferma := `email_cliente = '` + indirizzoScheda + `', email_confermata_il = '` + istanteHome + `'`
	senza := bustaHome(t, d, o, "1/26", "", "")
	nonContiene(t, "senza indirizzo", scheda(senza), "repair-email-notice")
	id := bustaHome(t, d, o, "2/26", "", conferma)
	contiene(t, "confermato", riquadroEmailScheda(scheda(id)), `<div id="repair-email-notice" class="riquadro-avviso">`+"\n"+
		`<strong>📧 Avviso email</strong>`+"\n"+
		`<p class="sotto"><span class="mono">`+indirizzoScheda+`</span> · Confermato</p>`+"\n"+
		`<p id="repair-email-ready-pending" class="sotto">Email di pronta: partirà da sola al passaggio a «pronta», una volta.</p>`+"\n"+
		`</div>`)
	cambia(t, b, id, "pronta")
	c := scheda(id)
	riq := riquadroEmailScheda(c)
	contiene(t, "in coda", riq, `<p id="repair-email-ready">Email di pronta: In coda dal 31/10/2026 10:00</p>`)
	nonContiene(t, "in coda", riq, "repair-email-ready-pending")
	if i, j := strings.Index(c, `id="repair-notice"`), strings.Index(c, `id="repair-email-notice"`); i < 0 || j < i {
		t.Error("il riquadro non sta dopo l'avviso di ritiro")
	}
	if conta := contaSQL(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = ? AND oggetto LIKE 'La tua riparazione%'`, indirizzoScheda); conta != 1 {
		t.Errorf("email: %d", conta)
	}
	if _, err := coda.Giro(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	contiene(t, "accettata", scheda(id), `<p id="repair-email-ready">Email di pronta: Accettato dal provider il 31/10/2026 10:00</p>`)
	// Tornata in lavorazione dopo l'email: nessuna nuova promessa.
	cambia(t, b, id, "in_lavorazione")
	riq = riquadroEmailScheda(scheda(id))
	contiene(t, "tornata", riq, "Email di pronta: Accettato dal provider")
	nonContiene(t, "tornata", riq, "repair-email-ready-pending")
	eseguiSQL(t, d, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = '2026-10-05T08:00:00.000000Z'`)
	contiene(t, "fallita", scheda(id), `<p id="repair-email-ready">Email di pronta: Fallito il 05/10/2026 10:00. Riparte al prossimo passaggio a «pronta».</p>`)
	// Tolta dalla coda dal titolare (D-27): conta come detta, la scheda lo dice e non promette niente.
	eseguiSQL(t, d, `DELETE FROM messaggi_in_uscita WHERE oggetto LIKE 'La tua riparazione%'`)
	riq = riquadroEmailScheda(scheda(id))
	contiene(t, "tolta", riq, `<p id="repair-email-ready">Email di pronta: tolta dalla coda dal titolare.</p>`)
	nonContiene(t, "tolta", riq, "repair-email-ready-pending", "repair-email-ready-off")
	// Un testo salvato che non si compone.
	rotta := bustaHome(t, d, o, "3/26", "", conferma)
	eseguiSQL(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ritira {sconosciuto}', ?)`, buste.FamEmailProntaTesto.Chiave(), istanteHome)
	cambia(t, b, rotta, "pronta")
	contiene(t, "non accodata", scheda(rotta),
		`<p id="repair-email-ready">Email di pronta non accodata: testo dell&#39;email non valido nelle Impostazioni. Riparte al prossimo passaggio a «pronta».</p>`)
	// In attesa: l'indirizzo e nient'altro.
	attesa := bustaHome(t, d, o, "4/26", "", "email_cliente = ?", indirizzoScheda)
	riq = riquadroEmailScheda(scheda(attesa))
	contiene(t, "in attesa", riq, `<p class="sotto"><span class="mono">`+indirizzoScheda+`</span> · In attesa di conferma</p>`)
	nonContiene(t, "in attesa", riq, "repair-email-ready")
	// Il segno importato.
	imp := bustaHome(t, d, o, "5/26", "", conferma+`, email_pronta_inviata_il = '2026-10-03T10:00:00.000000Z'`)
	contiene(t, "importato", scheda(imp), `<p id="repair-email-ready">Email di pronta inviata dal gestionale precedente il 03/10/2026 12:00</p>`)
	// L'indirizzo dell'altra parte: confermato, nessuna promessa.
	altra := bustaHome(t, d, o, "6/26", "", conferma+`, oreficeria_id = 2`)
	riq = riquadroEmailScheda(scheda(altra))
	contiene(t, "altra parte", riq, "· Confermato")
	nonContiene(t, "altra parte", riq, "repair-email-ready")
	// Una busta già pronta senza email di pronta (passata a «pronta» prima della conferma): nessuna promessa.
	giaPronta := bustaHome(t, d, o, "8/26", "", conferma+`, stato = 'pronta'`)
	riq = riquadroEmailScheda(scheda(giaPronta))
	contiene(t, "già pronta", riq, "· Confermato")
	nonContiene(t, "già pronta", riq, "repair-email-ready")
	// D-16: con l'email spenta la scheda non promette niente e lo dice.
	p.spento = true
	spenta := bustaHome(t, d, o, "7/26", "", conferma)
	riq = riquadroEmailScheda(scheda(spenta))
	contiene(t, "spenta", riq, `<p id="repair-email-ready-off" class="sotto">Email di pronta: l'email non è disponibile (Impostazioni › Provider): al passaggio a «pronta» non partirà.</p>`)
	nonContiene(t, "spenta", riq, "repair-email-ready-pending")
}

// inventario: CLI-42
// Le parole del mancato invio (email_miss/1): posta non configurata, testo non valido, errore.
func TestTestoEmailProntaNonAccodata(t *testing.T) {
	for motivo, atteso := range map[string]string{
		"not_configured":   "Email di pronta non accodata: posta SMTP non configurata. Riparte al prossimo passaggio a «pronta».",
		"invalid_template": "Email di pronta non accodata: testo dell'email non valido nelle Impostazioni. Riparte al prossimo passaggio a «pronta».",
		"error":            "Email di pronta non accodata: errore. Riparte al prossimo passaggio a «pronta».",
		"altro":            "Email di pronta non accodata: errore. Riparte al prossimo passaggio a «pronta».",
	} {
		if got := testoEmailPronta(buste.EmailPronta{Stato: "not_queued", Motivo: motivo}); got != atteso {
			t.Errorf("%s: %q", motivo, got)
		}
	}
}

// facciaConProviderPubblico è facciaConProvider con indirizzo_pubblico (o senza: "").
func facciaConProviderPubblico(t *testing.T, pubblico string) (http.Handler, *db.DB) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	inserisciConti(t, d, contiBase)
	dd := dipendenzeProva(&sondaFinta{pronto: true}, nessunRegistro, accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	dd.Buste = buste.Nuove(d, o)
	dd.Uscita = uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: &providerWeb{d: d}})
	dd.IndirizzoPubblico, dd.Configurazione = pubblico, configProva
	return mustInterna(t, dd), d
}

// inventario: ADM-09, CLI-42
// L'invio delle email acceso senza indirizzo_pubblico (Dubbio 1 della 070): la pagina dei provider dice che
// il riquadro della pagina del cliente non compare e che l'email di pronta non parte, e che cosa
// configurare. Con l'indirizzo pubblico, o con l'invio spento, la riga non c'è.
func TestProviderSenzaIndirizzoPubblico(t *testing.T) {
	riga := `<p id="smtp-switch-no-public" class="flash flash-errore">Senza indirizzo pubblico il riquadro «avvisami via email» della pagina del cliente non compare e l&#39;email di pronta non parte: manca «indirizzo_pubblico = https://host[:porta]» in ` +
		configProva + `, poi riavvia gestrip serve con «` + apparecchio.ComandoRiavvio + `».</p>`
	h, _ := facciaConProviderPubblico(t, "")
	b := titolareNeiProvider(t, h)
	postaProva(b)
	nonContiene(t, "spento", b.vai("GET", "/settings/providers", nil, nil).corpo, "smtp-switch-no-public")
	b.vai("POST", "/settings/providers/accensione", soloToken(b, "acceso", "1"), nil)
	contiene(t, "acceso", b.vai("GET", "/settings/providers", nil, nil).corpo, riga)
	h, _ = facciaConProviderPubblico(t, indirizzoPubblicoProva)
	b = titolareNeiProvider(t, h)
	postaProva(b)
	b.vai("POST", "/settings/providers/accensione", soloToken(b, "acceso", "1"), nil)
	c := b.vai("GET", "/settings/providers", nil, nil).corpo
	contiene(t, "con l'indirizzo", c, `<span id="smtp-switch-state" class="badge badge-ok">Acceso</span>`)
	nonContiene(t, "con l'indirizzo", c, "smtp-switch-no-public")
}
