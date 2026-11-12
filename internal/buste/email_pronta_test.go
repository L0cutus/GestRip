package buste

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'email «pronta per il ritiro» (CLI-42) e lo stato dell'avviso email nella scheda, come
// EmailNotice.notify_ready/2 e EmailNotice.sheet/1 dello standalone (go-baseline), con i casi di
// email_ready_notice_test.exs; in più la D-16 (solo con l'email disponibile) e l'accodamento nella
// transazione del cambio di stato (solo Go). Dati inventati, indirizzi .invalid.

const indirizzoPronta = "ada.rossi@example.invalid"

// provaPronta sono le buste con l'email accesa, l'indirizzo pubblico e il nome del negozio.
func provaPronta(t *testing.T) (*Buste, *db.DB, *providerEmail) {
	t.Helper()
	b, d, p := provaEmail(t)
	b.ConIndirizzoPubblico(baseEmail)
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega Prova', '', ?, ?)`, istanteProva, istanteProva)
	return b, d, p
}

// confermata chiede l'avviso dalla pagina del link e lo conferma con il pulsante del link dell'email.
func confermata(t *testing.T, b *Buste, d *db.DB, c Creata, indirizzo string) {
	t.Helper()
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi(indirizzo), baseEmail, pubblico); err != nil {
		t.Fatalf("richiesta: %v", err)
	}
	tok := linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
	if _, err := b.ConfermaEmail(ctx, tok, pubblico); err != nil {
		t.Fatalf("conferma: %v", err)
	}
}

// emailPronta è una riga della coda dell'email di pronta.
type emailPronta struct {
	id                                      int64
	busta                                   int64
	parte, indirizzo, oggetto, testo, stato string
}

// emailPronte sono le email di pronta in coda (quelle che la riga repair.email_ready_queued nomina),
// dalla prima.
func emailPronte(t *testing.T, d *db.DB) []emailPronta {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT id, busta_id, destinatario, indirizzo, coalesce(oggetto, ''), testo, stato FROM messaggi_in_uscita
		WHERE id IN (SELECT json_extract(dati, '$.message_id') FROM eventi_audit WHERE azione = 'repair.email_ready_queued') ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []emailPronta
	for rr.Next() {
		var e emailPronta
		if err := rr.Scan(&e.id, &e.busta, &e.parte, &e.indirizzo, &e.oggetto, &e.testo, &e.stato); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

// traccePronta conta tutto quello che l'email di pronta scrive: le email con il suo oggetto, le sue righe
// d'audit e il segno sulla busta.
func traccePronta(t *testing.T, d *db.DB) int {
	t.Helper()
	return conta(t, d, `SELECT (SELECT count(*) FROM messaggi_in_uscita WHERE oggetto LIKE 'La tua riparazione%') +
		(SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.email_ready_queued', 'repair.email_ready_not_queued')) +
		(SELECT count(*) FROM buste WHERE email_pronta_inviata_il IS NOT NULL)`)
}

func passa(t *testing.T, b *Buste, id int64, stati ...string) {
	t.Helper()
	for _, s := range stati {
		if _, err := b.CambiaStato(ctx, id, s, chi); err != nil {
			t.Fatalf("a %s: %v", s, err)
		}
	}
}

func giro(t *testing.T, b *Buste) {
	t.Helper()
	if _, err := b.posta.(*uscita.Coda).Giro(ctx, 50); err != nil {
		t.Fatal(err)
	}
}

// inventario: CLI-42
// Il primo passaggio a «pronta» di un indirizzo confermato: un'email in coda a quell'indirizzo con i testi
// dell'editor, {numero} come la D-82, {link_stato} il link stabile della busta e il piè con il link per
// disattivare; il segno sulla busta e repair.email_ready_queued con la riga della coda, senza l'indirizzo.
func TestEmailPronta(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "RIP-2026-0045" }))
	confermata(t, b, d, c, indirizzoPronta)
	cambio, err := b.CambiaStato(ctx, c.ID, "pronta", chi)
	if err != nil || cambio.Dopo != "pronta" {
		t.Fatalf("cambio: %+v %v", cambio, err)
	}
	pronte := emailPronte(t, d)
	if len(pronte) != 1 {
		t.Fatalf("email di pronta: %+v", pronte)
	}
	e := pronte[0]
	if e.busta != c.ID || e.parte != "customer" || e.indirizzo != indirizzoPronta || e.stato != "queued" ||
		e.oggetto != "La tua riparazione è pronta — busta 0045/26" {
		t.Errorf("email: %+v", e)
	}
	stop := linkDi(t, reLinkStop, e.testo)
	atteso := "Ciao Cliente Prova,\n\nla tua riparazione (busta 0045/26) è pronta per il ritiro!\n\nAnello da stringere\n\n" +
		"Dettagli e foto: " + baseEmail + "/r/" + c.Token + "\n\nTi aspettiamo in negozio.\n\n— Bottega Prova" +
		"\n\n---\nRicevi questa email perché hai attivato l'avviso dalla pagina stato della tua riparazione presso Bottega Prova.\n" +
		"Per disattivare gli avvisi: " + baseEmail + "/stato/email-disattiva?t=" + stop
	if e.testo != atteso {
		t.Errorf("testo\n%q\natteso\n%q", e.testo, atteso)
	}
	if err := b.ControllaStop(stop); err != nil {
		t.Errorf("link del piè: %v", err)
	}
	if got := colonneEmail(t, d, c.ID); got != indirizzoPronta+"|x|x" {
		t.Errorf("colonne: %s", got)
	}
	if got := ultimoAudit(t, d, "repair.email_ready_queued"); got != `repair.email_ready_queued|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+
		`","message_id":`+itoa(e.id)+`,"party":"customer"}` {
		t.Errorf("audit: %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND id_risorsa = ?`, itoa(e.id)) != 1 {
		t.Error("outgoing.queued della riga")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%ada.rossi%'`) != 0 {
		t.Error("indirizzo nel registro")
	}
	// Il segno ha l'istante del cambio di stato: la stessa transazione.
	if conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND email_pronta_inviata_il = aggiornato_il`, c.ID) != 1 {
		t.Error("istante del segno")
	}
}

// inventario: CLI-42
// Una volta sola: un secondo «pronta» (dopo un ritorno in lavorazione) non accoda un'altra email, né con
// la prima accettata, né con la prima ancora in coda; nemmeno se il titolare l'ha scartata (D-27).
func TestEmailProntaUnaVolta(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	passa(t, b, c.ID, "pronta")
	giro(t, b)
	passa(t, b, c.ID, "in_lavorazione", "pronta")
	giro(t, b)
	if p := emailPronte(t, d); len(p) != 1 || p[0].stato != "accepted" {
		t.Errorf("accettata: %+v", p)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.email_ready_queued'`); n != 1 {
		t.Errorf("righe: %d", n)
	}
	// Ancora in coda.
	altra := crea(t, b, modulo())
	confermata(t, b, d, altra, "altra@example.invalid")
	passa(t, b, altra.ID, "pronta", "in_lavorazione", "pronta")
	p := emailPronte(t, d)
	if len(p) != 2 || p[1].stato != "queued" {
		t.Fatalf("in coda: %+v", p)
	}
	// Scartata dal titolare: la riga non c'è più, il segno resta (come lo standalone: solo «failed» riapre).
	esegui(t, d, `DELETE FROM messaggi_in_uscita WHERE id = ?`, p[1].id)
	prima := traccePronta(t, d)
	passa(t, b, altra.ID, "in_lavorazione", "pronta")
	if dopo := traccePronta(t, d); dopo != prima {
		t.Errorf("scartata: %d → %d", prima, dopo)
	}
}

// inventario: CLI-42
// Una riga che la coda ha fatto fallire riapre l'avviso: il «pronta» dopo accoda un'email nuova.
func TestEmailProntaFallitaRiapre(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	passa(t, b, c.ID, "pronta")
	if len(emailPronte(t, d)) != 1 {
		t.Fatal("nessuna email di pronta")
	}
	esegui(t, d, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = ?, tentativi = 5 WHERE id = ?`, istanteProva, emailPronte(t, d)[0].id)
	// Un mancato invio in mezzo (testo rotto) non cambia niente: conta l'ultima email accodata, fallita.
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ritira {sconosciuto}', ?)`, FamEmailProntaTesto.Chiave(), istanteProva)
	passa(t, b, c.ID, "in_lavorazione", "pronta")
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, FamEmailProntaTesto.Chiave())
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.email_ready_not_queued'`); n != 1 {
		t.Fatalf("mancato invio: %d", n)
	}
	passa(t, b, c.ID, "in_lavorazione", "pronta")
	p := emailPronte(t, d)
	if len(p) != 2 || p[0].stato != "failed" || p[1].stato != "queued" {
		t.Errorf("email: %+v", p)
	}
}

// inventario: CLI-42
// Niente si scrive (né email, né righe, né segno) per un indirizzo in attesa, una busta senza indirizzo o
// con l'avviso disattivato, un segno importato dallo standalone, un altro stato, l'email non disponibile
// (D-16: spenta o senza indirizzo pubblico), e da chiunque la chiami per una busta non pronta o nel cestino.
func TestEmailProntaNiente(t *testing.T) {
	b, d, p := provaPronta(t)
	attesa := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, attesa.Token, chiedi(indirizzoPronta), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	passa(t, b, attesa.ID, "pronta")
	passa(t, b, crea(t, b, modulo()).ID, "pronta")
	spenta := crea(t, b, modulo())
	confermata(t, b, d, spenta, indirizzoPronta)
	if err := b.DisattivaEmailCliente(ctx, spenta.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	passa(t, b, spenta.ID, "pronta")
	if n := traccePronta(t, d); n != 0 {
		t.Fatalf("attesa, senza indirizzo, disattivata: %d", n)
	}
	importata := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = ?, email_confermata_il = ?, email_pronta_inviata_il = '2026-10-03T10:00:00.000000Z' WHERE id = ?`,
		indirizzoPronta, istanteProva, importata.ID)
	passa(t, b, importata.ID, "pronta")
	if v := valore(t, d, `SELECT email_pronta_inviata_il FROM buste WHERE id = ?`, importata.ID); v != "2026-10-03T10:00:00.000000Z" {
		t.Errorf("segno importato: %v", v)
	}
	altri := crea(t, b, modulo())
	confermata(t, b, d, altri, indirizzoPronta)
	passa(t, b, altri.ID, "preventivo", "in_lavorazione", "in_attesa_materiali", "annullata")
	if n := traccePronta(t, d); n != 1 { // solo il segno importato
		t.Fatalf("segno importato, altri stati: %d", n)
	}
	// D-16: l'email spenta, poi l'indirizzo pubblico che manca.
	off := crea(t, b, modulo())
	confermata(t, b, d, off, indirizzoPronta)
	p.spento = true
	passa(t, b, off.ID, "pronta")
	p.spento = false
	senza := crea(t, b, modulo())
	confermata(t, b, d, senza, indirizzoPronta)
	b.ConIndirizzoPubblico("")
	passa(t, b, senza.ID, "pronta")
	b.ConIndirizzoPubblico(baseEmail)
	if n := traccePronta(t, d); n != 1 {
		t.Fatalf("D-16: %d", n)
	}
	// La porta stessa: chiede «pronta» alla riga, e una busta nel cestino non c'è.
	nonPronta := crea(t, b, modulo())
	confermata(t, b, d, nonPronta, indirizzoPronta)
	cestino := crea(t, b, modulo())
	confermata(t, b, d, cestino, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET stato = 'pronta', cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	for id, attesoEsito := range map[int64]string{nonPronta.ID: "not_ready", cestino.ID: "not_found"} {
		tx, err := d.Scrittura.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		esito, err := b.notificaPronta(ctx, tx, id, chi)
		_ = tx.Rollback() // la prova guarda solo l'esito
		if esito != attesoEsito || err != nil {
			t.Errorf("busta %d: %q %v, atteso %q", id, esito, err, attesoEsito)
		}
	}
	if n := traccePronta(t, d); n != 1 {
		t.Errorf("porta: %d", n)
	}
}

// inventario: CLI-42, CLI-28
// Su una busta conto terzi l'email va all'oreficeria che l'ha chiesta dal suo /r/, salutata con il suo
// nome; mai a un indirizzo che nessuno può attribuire (importato su una busta conto terzi), al cliente di
// una busta passata a un'oreficeria né all'oreficeria di una busta tornata al cliente. L'indirizzo
// importato di una busta diretta è del cliente. Di chi è l'indirizzo lo dice l'ultima richiesta.
func TestEmailProntaContoTerzi(t *testing.T) {
	b, d, _ := provaPronta(t)
	oref := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	confermata(t, b, d, oref, "orefice@example.invalid")
	passa(t, b, oref.ID, "pronta")
	p := emailPronte(t, d)
	if len(p) != 1 || p[0].parte != "oreficeria" || !strings.HasPrefix(p[0].testo, "Ciao Oreficeria Esempio,\n\n") {
		t.Fatalf("oreficeria: %+v", p)
	}
	if !strings.HasSuffix(ultimoAudit(t, d, "repair.email_ready_queued"), `"party":"oreficeria"}`) {
		t.Errorf("audit: %s", ultimoAudit(t, d, "repair.email_ready_queued"))
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND id_risorsa = ?
		AND json_extract(dati, '$.party') = 'oreficeria'`, itoa(p[0].id)) != 1 {
		t.Error("outgoing.queued della riga dell'oreficeria")
	}
	prima := traccePronta(t, d)
	imp := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	esegui(t, d, `UPDATE buste SET email_cliente = ?, email_confermata_il = ? WHERE id = ?`, indirizzoPronta, istanteProva, imp.ID)
	passa(t, b, imp.ID, "pronta")
	passata := crea(t, b, modulo())
	confermata(t, b, d, passata, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, passata.ID)
	passa(t, b, passata.ID, "pronta")
	tornata := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	confermata(t, b, d, tornata, "orefice@example.invalid")
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, tornata.ID)
	passa(t, b, tornata.ID, "pronta")
	if dopo := traccePronta(t, d); dopo != prima {
		t.Fatalf("indirizzi di nessuno: %d → %d", prima, dopo)
	}
	diretta := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = ?, email_confermata_il = ? WHERE id = ?`, indirizzoPronta, istanteProva, diretta.ID)
	passa(t, b, diretta.ID, "pronta")
	if p := emailPronte(t, d); len(p) != 2 || p[1].parte != "customer" || p[1].busta != diretta.ID {
		t.Errorf("importato diretto: %+v", p)
	}
	// L'ultima richiesta, non la prima: il cliente chiede, la busta passa all'oreficeria, l'oreficeria chiede
	// e conferma.
	ultima := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, ultima.Token, chiedi("cliente@example.invalid"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, ultima.ID)
	confermata(t, b, d, ultima, "orefice@example.invalid")
	passa(t, b, ultima.ID, "pronta")
	if p := emailPronte(t, d); len(p) != 3 || p[2].parte != "oreficeria" || p[2].indirizzo != "orefice@example.invalid" {
		t.Errorf("ultima richiesta: %+v", p)
	}
}

// inventario: CLI-42
// Ogni via per «pronta»: il pannello del QR, il pulsante rapido e il menu della lista, la riapertura di una
// busta consegnata; un secondo tocco del pannello (busta già pronta) e il menu sullo stesso stato non
// scrivono niente.
func TestEmailProntaTutteLeVie(t *testing.T) {
	b, d, _ := provaPronta(t)
	vie := []func(id int64) (Cambio, error){
		func(id int64) (Cambio, error) { return b.AvanzaDaQR(ctx, id, "in_lavorazione", chi) },
		func(id int64) (Cambio, error) { return b.PassoRapido(ctx, id, "pronta", chi) },
		func(id int64) (Cambio, error) { return b.CambiaStatoDallaLista(ctx, id, "pronta", chi) },
		func(id int64) (Cambio, error) {
			esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, id)
			return b.Riapri(ctx, id, "pronta", "Il cliente torna", chi)
		},
	}
	for i, via := range vie {
		c := crea(t, b, modulo())
		confermata(t, b, d, c, indirizzoPronta)
		if cambio, err := via(c.ID); err != nil || cambio.Dopo != "pronta" {
			t.Fatalf("via %d: %+v %v", i, cambio, err)
		}
		if p := emailPronte(t, d); len(p) != i+1 || p[i].busta != c.ID {
			t.Errorf("via %d: %+v", i, p)
		}
		if i == 0 {
			if _, err := b.AvanzaDaQR(ctx, c.ID, "in_lavorazione", chi); err != nil {
				t.Errorf("secondo tocco: %v", err)
			}
			if _, err := b.CambiaStatoDallaLista(ctx, c.ID, "pronta", chi); err != nil {
				t.Errorf("menu sullo stesso stato: %v", err)
			}
			if p := emailPronte(t, d); len(p) != 1 {
				t.Errorf("secondo tocco: %+v", p)
			}
		}
	}
}

// postaCheRifiutaIn è una coda che dice il canale acceso ma non accoda nella transazione (il canale spento
// fra la domanda e l'invio).
type postaCheRifiutaIn struct{ Posta }

func (postaCheRifiutaIn) AccodaIn(context.Context, *sql.Tx, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	return 0, uscita.ErrNonConfigurato
}

// inventario: CLI-42
// Un'email che non può partire non toglie mai il nuovo stato: un testo salvato che non si compone (mai il
// predefinito), una coda che rifiuta, un errore del database nell'accodamento. Niente email, niente segno,
// niente link nuovo; la riga repair.email_ready_not_queued con il motivo e senza dati; al «pronta» dopo
// l'email riparte.
func TestEmailProntaNonAccodata(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ritira {sconosciuto} {link_stato}', ?)`,
		FamEmailProntaTesto.Chiave(), istanteProva)
	if cambio, err := b.CambiaStato(ctx, c.ID, "pronta", chi); err != nil || cambio.Dopo != "pronta" {
		t.Fatalf("testo rotto: %+v %v", cambio, err)
	}
	if got := ultimoAudit(t, d, "repair.email_ready_not_queued"); got != `repair.email_ready_not_queued|repair|`+itoa(c.ID)+
		`|ok|{"bag_number":"`+c.Numero+`","reason":"invalid_template"}` {
		t.Errorf("mancato invio: %s", got)
	}
	if len(emailPronte(t, d)) != 0 || colonneEmail(t, d, c.ID) != indirizzoPronta+"|x|-" ||
		conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE testo LIKE 'Ritira%'`) != 0 {
		t.Error("testo rotto: email o segno")
	}
	esegui(t, d, `DELETE FROM impostazioni WHERE chiave = ?`, FamEmailProntaTesto.Chiave())
	passa(t, b, c.ID, "in_lavorazione", "pronta")
	if p := emailPronte(t, d); len(p) != 1 {
		t.Errorf("dopo il testo rimesso: %+v", p)
	}
	// La coda che rifiuta.
	rifiuta := crea(t, b, modulo())
	confermata(t, b, d, rifiuta, indirizzoPronta)
	vera := b.posta
	b.posta = postaCheRifiutaIn{vera}
	passa(t, b, rifiuta.ID, "pronta")
	b.posta = vera
	if !strings.HasSuffix(ultimoAudit(t, d, "repair.email_ready_not_queued"), `"reason":"not_configured"}`) || colonneEmail(t, d, rifiuta.ID) != indirizzoPronta+"|x|-" {
		t.Errorf("coda che rifiuta: %s", ultimoAudit(t, d, "repair.email_ready_not_queued"))
	}
	// Un errore del database a metà (dopo il link nuovo): tutto quello che l'email aveva scritto torna
	// indietro, lo stato resta.
	rotta := crea(t, b, modulo())
	confermata(t, b, d, rotta, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, rotta.ID)
	link := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`)
	esegui(t, d, `CREATE TRIGGER prova_rotta BEFORE INSERT ON messaggi_in_uscita WHEN NEW.oggetto LIKE 'La tua%' BEGIN SELECT RAISE(ABORT, 'guasto di prova'); END`)
	if cambio, err := b.CambiaStato(ctx, rotta.ID, "pronta", chi); err != nil || cambio.Dopo != "pronta" {
		t.Fatalf("guasto: %+v %v", cambio, err)
	}
	esegui(t, d, `DROP TRIGGER prova_rotta`)
	if v := valore(t, d, `SELECT stato FROM buste WHERE id = ?`, rotta.ID); v != "pronta" {
		t.Errorf("stato: %v", v)
	}
	if got := ultimoAudit(t, d, "repair.email_ready_not_queued"); got != `repair.email_ready_not_queued|repair|`+itoa(rotta.ID)+
		`|ok|{"bag_number":"`+rotta.Numero+`","reason":"error"}` {
		t.Errorf("guasto: %s", got)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`) != link ||
		valore(t, d, `SELECT token_pubblico FROM buste WHERE id = ?`, rotta.ID) != rotta.Token || colonneEmail(t, d, rotta.ID) != indirizzoPronta+"|x|-" {
		t.Error("guasto: il link nuovo o il segno sono rimasti")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%guasto%' OR dati LIKE '%ada.rossi%'`) != 0 {
		t.Error("dati nel registro")
	}
}

// inventario: CLI-42
// Il nuovo stato e l'email si scrivono nella stessa transazione: prima del commit la transazione ha il
// nuovo stato, la riga della coda, il segno e le due righe d'audit; se il commit non arriva non resta
// niente di tutto questo.
func TestEmailProntaStessaTransazione(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	traccia0 := traccePronta(t, d)
	audit0 := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	guasto := errors.New("commit mancato di prova")
	var visto string
	b.primaDelCommit = func(tx *sql.Tx) error {
		var n int
		var stato string
		if err := tx.QueryRow(`SELECT (SELECT count(*) FROM messaggi_in_uscita WHERE oggetto LIKE 'La tua%') +
			(SELECT count(*) FROM eventi_audit WHERE azione IN ('repair.email_ready_queued', 'repair.status_changed', 'outgoing.queued')) +
			(SELECT count(*) FROM buste WHERE email_pronta_inviata_il IS NOT NULL), (SELECT stato FROM buste WHERE id = ?)`,
			c.ID).Scan(&n, &stato); err != nil {
			return err
		}
		visto = stato + ":" + itoa(int64(n))
		return guasto
	}
	if _, err := b.CambiaStato(ctx, c.ID, "pronta", chi); !errors.Is(err, guasto) {
		t.Fatalf("errore: %v", err)
	}
	b.primaDelCommit = nil
	// Nella transazione: lo stato, la riga, il segno, repair.status_changed, outgoing.queued (anche quello
	// della conferma, prima) e repair.email_ready_queued.
	if visto != "pronta:6" {
		t.Errorf("nella transazione: %s", visto)
	}
	if traccePronta(t, d) != traccia0 || conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit0 ||
		valore(t, d, `SELECT stato FROM buste WHERE id = ?`, c.ID) != "in_lavorazione" {
		t.Error("è rimasto qualcosa")
	}
}

// inventario: CLI-42
// {link_stato}: un link scaduto si rinnova (repair.public_link_issued) e l'email porta quello nuovo; senza
// nome «cliente»; i testi salvati nell'editor, con tutti i loro campi.
func TestEmailProntaTesti(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = '2026-10-01T00:00:00.000000Z', nome_cliente = '  ' WHERE id = ?`, c.ID)
	passa(t, b, c.ID, "pronta")
	nuovo, _ := valore(t, d, `SELECT token_pubblico FROM buste WHERE id = ?`, c.ID).(string)
	p := emailPronte(t, d)
	if nuovo == "" || nuovo == c.Token || len(p) != 1 || !strings.Contains(p[0].testo, "Dettagli e foto: "+baseEmail+"/r/"+nuovo+"\n") ||
		!strings.HasPrefix(p[0].testo, "Ciao cliente,\n\n") {
		t.Errorf("link o nome: %+v", p)
	}
	if !strings.HasSuffix(ultimoAudit(t, d, "repair.public_link_issued"), `{"reason":"expired"}`) {
		t.Errorf("link nuovo: %s", ultimoAudit(t, d, "repair.public_link_issued"))
	}
	for f, testo := range map[Famiglia]string{FamEmailProntaOggetto: "{nome}|{numero}|{negozio}", FamEmailProntaTesto: "{nome}|{numero}|{descrizione}|{negozio}|{link_stato}"} {
		if _, err := b.SalvaModello(ctx, f, testo, chi); err != nil {
			t.Fatal(err)
		}
	}
	tutti := crea(t, b, modulo())
	confermata(t, b, d, tutti, indirizzoPronta)
	passa(t, b, tutti.ID, "pronta")
	p = emailPronte(t, d)
	if len(p) != 2 || p[1].oggetto != "Cliente Prova|"+tutti.Numero+"|Bottega Prova" ||
		!strings.HasPrefix(p[1].testo, "Cliente Prova|"+tutti.Numero+"|Anello da stringere|Bottega Prova|"+baseEmail+"/r/"+tutti.Token+"\n\n---\n") {
		t.Errorf("testi salvati: %+v", p)
	}
}

// inventario: CLI-42
// Disattivare l'avviso (dalla pagina o con il link del piè dell'email di pronta) toglie dalla coda l'email
// di pronta ancora da mandare, e solo quella; dopo una disattivazione e un indirizzo nuovo confermato
// l'avviso di prima non conta più: il «pronta» dopo avvisa il nuovo indirizzo. Lo stesso indirizzo chiesto
// di nuovo lascia stare la coda.
func TestEmailProntaDopoDisattivazione(t *testing.T) {
	b, d, _ := provaPronta(t)
	c := crea(t, b, modulo())
	confermata(t, b, d, c, indirizzoPronta)
	altra := crea(t, b, modulo())
	confermata(t, b, d, altra, indirizzoPronta)
	passa(t, b, c.ID, "pronta")
	passa(t, b, altra.ID, "pronta")
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	p := emailPronte(t, d)
	if len(p) != 2 || p[0].stato != "failed" || p[1].stato != "queued" ||
		conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = ? AND ultimo_errore = 'Avvisi email disattivati dal destinatario'`, p[0].id) != 1 {
		t.Fatalf("disattiva: %+v", p)
	}
	// Il link del piè dell'email di pronta.
	if err := b.DisattivaConLink(ctx, linkDi(t, reLinkStop, p[1].testo), pubblico); err != nil {
		t.Fatal(err)
	}
	if p := emailPronte(t, d); len(p) != 2 || p[1].stato != "failed" {
		t.Errorf("link del piè: %+v", p[1])
	}
	// Un indirizzo nuovo confermato.
	confermata(t, b, d, c, "ada.nuova@example.invalid")
	if e, err := b.emailDellaScheda(ctx, c.ID); err != nil || e == nil || e.Consenso != "confirmed" || e.Pronta != nil {
		t.Errorf("scheda dopo l'indirizzo nuovo: %+v %v", e, err)
	}
	passa(t, b, c.ID, "in_lavorazione", "pronta")
	p = emailPronte(t, d)
	if len(p) != 3 || p[2].indirizzo != "ada.nuova@example.invalid" || p[2].stato != "queued" {
		t.Errorf("indirizzo nuovo: %+v", p)
	}
	// Lo stesso indirizzo chiesto di nuovo: la coda resta.
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("ada.nuova@example.invalid"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if p := emailPronte(t, d); len(p) != 3 || p[2].stato != "queued" {
		t.Errorf("stesso indirizzo: %+v", p[2])
	}
}

// inventario: CLI-42
// Lo stato dell'avviso email della scheda (EmailNotice.sheet/1): niente senza indirizzo; l'indirizzo in
// attesa o confermato, se è della controparte, se l'email è disponibile (D-16); l'email di pronta come la
// riga della coda la dice (in coda dall'ora dell'accodamento, accettata, fallita), non accodata con il
// motivo, o il segno importato dallo standalone. La scheda la porta.
func TestEmailDellaScheda(t *testing.T) {
	b, d, p := provaPronta(t)
	c := crea(t, b, modulo())
	sch := func(id int64) *EmailDellaScheda {
		t.Helper()
		e, err := b.emailDellaScheda(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if e := sch(c.ID); e != nil {
		t.Errorf("senza indirizzo: %+v", e)
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi(indirizzoPronta), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if e := sch(c.ID); e == nil || *e != (EmailDellaScheda{Consenso: "pending", Indirizzo: indirizzoPronta, Controparte: true, Disponibile: true}) {
		t.Errorf("in attesa: %+v", e)
	}
	tok := linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
	if _, err := b.ConfermaEmail(ctx, tok, pubblico); err != nil {
		t.Fatal(err)
	}
	if e := sch(c.ID); e == nil || e.Consenso != "confirmed" || e.Pronta != nil {
		t.Errorf("confermato: %+v", e)
	}
	p.spento = true
	if e := sch(c.ID); e == nil || e.Disponibile {
		t.Errorf("spenta: %+v", e)
	}
	p.spento = false
	passa(t, b, c.ID, "pronta")
	adesso := b.o.Adesso().UTC().Truncate(time.Microsecond)
	if e := sch(c.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "queued", Il: adesso}) {
		t.Errorf("in coda: %+v", e)
	}
	// L'ora della scheda è quella della riga: accettata un'ora dopo l'accodamento.
	b.o.(*clock.Finto).Sposta(time.Hour)
	giro(t, b)
	if e := sch(c.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "accepted", Il: adesso.Add(time.Hour)}) {
		t.Errorf("accettata: %+v", e)
	}
	if len(emailPronte(t, d)) != 1 {
		t.Fatal("nessuna email di pronta")
	}
	esegui(t, d, `UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = '2026-10-05T08:00:00.000000Z' WHERE id = ?`, emailPronte(t, d)[0].id)
	if e := sch(c.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "failed", Il: time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)}) {
		t.Errorf("fallita: %+v", e)
	}
	if s, err := b.Scheda(ctx, c.ID); err != nil || s.Email == nil || s.Email.Pronta == nil || s.Email.Pronta.Stato != "failed" {
		t.Errorf("la scheda: %+v %v", s.Email, err)
	}
	esegui(t, d, `DELETE FROM messaggi_in_uscita WHERE id = ?`, emailPronte(t, d)[0].id)
	// Tolta dalla coda dal titolare (D-27): la riga non c'è più, ed è detta (giaDetto): la scheda lo dice.
	if e := sch(c.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "discarded"}) {
		t.Errorf("riga tolta: %+v", e)
	}
	// Non accodata, con il motivo.
	rotta := crea(t, b, modulo())
	confermata(t, b, d, rotta, indirizzoPronta)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ritira {sconosciuto}', ?)`, FamEmailProntaTesto.Chiave(), istanteProva)
	passa(t, b, rotta.ID, "pronta")
	if e := sch(rotta.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "not_queued", Motivo: "invalid_template"}) {
		t.Errorf("non accodata: %+v", e)
	}
	// Il segno importato, senza righe del Go.
	imp := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = ?, email_confermata_il = ?, email_pronta_inviata_il = '2026-10-03T10:00:00.000000Z' WHERE id = ?`,
		indirizzoPronta, istanteProva, imp.ID)
	if e := sch(imp.ID); e == nil || e.Pronta == nil || *e.Pronta != (EmailPronta{Stato: "sent", Il: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)}) || !e.Controparte {
		t.Errorf("importato: %+v", e)
	}
	// L'indirizzo che non è della controparte.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, imp.ID)
	if e := sch(imp.ID); e == nil || e.Controparte || e.Consenso != "confirmed" {
		t.Errorf("altra parte: %+v", e)
	}
}
