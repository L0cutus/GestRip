package buste

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/firmeflask"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// L'avviso email della pagina del cliente (CLI-39, CLI-40, CLI-41, CLI-19), come email_notice_test.exs,
// email_notice_stop_test.exs, public_repair_email_test.exs e public_repair_email_stop_test.exs dello
// standalone (go-baseline), più la D-16 e la pagina di ripiego della D-02 (solo Go).

const baseEmail = "https://negozio.example.invalid"

var pubblico = audit.Contesto{Attore: "public", IP: "203.0.113.9"}

// providerEmail è il provider finto dell'email: prende tutto, e il canale è acceso finché spento è falso
// (l'interruttore della D-03 e la configurazione, uscita.EmailAttiva).
type providerEmail struct{ spento bool }

func (p *providerEmail) Consegna(context.Context, uscita.Messaggio) (string, error) {
	return "250 ok", nil
}
func (p *providerEmail) Attivo(context.Context) (bool, error) { return !p.spento, nil }

// postaCheRifiuta è una coda che dice il canale acceso ma rifiuta di accodare (il canale spento fra la
// domanda e l'invio).
type postaCheRifiuta struct{ Posta }

func (postaCheRifiuta) Accoda(context.Context, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	return 0, uscita.ErrNonConfigurato
}

func provaEmail(t *testing.T) (*Buste, *db.DB, *providerEmail) {
	t.Helper()
	b, d, o := prova(t)
	p := &providerEmail{}
	b.ConPosta(uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: p}), []byte("chiave-email-di-prova-di-32-byte"))
	return b, d, p
}

func chiedi(indirizzo string) RichiestaEmail {
	return RichiestaEmail{Indirizzo: indirizzo, ConIndirizzo: true, Consenso: "1", ConConsenso: true}
}

var reinvia = RichiestaEmail{Reinvia: true}

// emailInCoda è l'ultima email accodata: destinatario, indirizzo, oggetto, testo e stato.
type emailInCoda struct{ parte, indirizzo, oggetto, testo, stato string }

func ultimaEmail(t *testing.T, d *db.DB) emailInCoda {
	t.Helper()
	var e emailInCoda
	if err := d.Lettura.QueryRow(`SELECT destinatario, indirizzo, coalesce(oggetto, ''), testo, stato FROM messaggi_in_uscita
		WHERE canale = 'email' ORDER BY id DESC LIMIT 1`).Scan(&e.parte, &e.indirizzo, &e.oggetto, &e.testo, &e.stato); err != nil {
		t.Fatalf("nessuna email in coda: %v", err)
	}
	return e
}

var (
	reLinkConferma = regexp.MustCompile(`/stato/email-conferma\?t=(\S+)`)
	reLinkStop     = regexp.MustCompile(`/stato/email-disattiva\?t=(\S+)`)
)

func linkDi(t *testing.T, re *regexp.Regexp, testo string) string {
	t.Helper()
	m := re.FindStringSubmatch(testo)
	if m == nil {
		t.Fatalf("nessun link in %q", testo)
	}
	return m[1]
}

// colonneEmail è «indirizzo|conferma|pronta» della busta, «-» per NULL.
func colonneEmail(t *testing.T, d *db.DB, id int64) string {
	t.Helper()
	var a, c, p any
	if err := d.Lettura.QueryRow(`SELECT email_cliente, email_confermata_il, email_pronta_inviata_il FROM buste WHERE id = ?`, id).Scan(&a, &c, &p); err != nil {
		t.Fatal(err)
	}
	s := make([]string, 3)
	for i, v := range []any{a, c, p} {
		s[i] = "-"
		if v != nil {
			s[i] = "x"
			if i == 0 {
				s[i] = v.(string)
			}
		}
	}
	return strings.Join(s, "|")
}

// inventario: CLI-39
// Le regole dell'indirizzo di EmailConsent.normalize_address/1.
func TestNormalizzaEmail(t *testing.T) {
	for in, atteso := range map[string]string{
		" Ada.Rossi@Example.INVALID\n":           "ada.rossi@example.invalid",
		"a@b.co":                                 "a@b.co",
		"a@b.c.d":                                "a@b.c.d",
		"a@b.c":                                  "",
		"a@.co":                                  "",
		"@b.co":                                  "",
		"a@@b.co":                                "",
		"a b@c.co":                               "",
		"a@b c.co":                               "",
		"a@b.co x":                               "",
		"senza-chiocciola.it":                    "",
		"":                                       "",
		strings.Repeat("a", 244) + "@esempio.it": "",
		strings.Repeat("a", 243) + "@esempio.it": strings.Repeat("a", 243) + "@esempio.it",
		"ÀNNA@ESEMPIO.IT":                        "ànna@esempio.it",
	} {
		got, ok := NormalizzaEmail(in)
		if ok != (atteso != "") || got != atteso {
			t.Errorf("%q: %q %v, atteso %q", in, got, ok, atteso)
		}
	}
}

// inventario: CLI-39
// La richiesta: l'indirizzo in attesa, la riga d'audit senza l'indirizzo, l'email di conferma in coda
// con i testi dell'editor, {numero} come la D-82, il link firmato e il piè con il link per disattivare.
func TestRichiestaEmail(t *testing.T) {
	b, d, _ := provaEmail(t)
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega Prova', '', ?, ?)`, istanteProva, istanteProva)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.CodiceQR = "RIP-2026-0045" }))
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi(" Ada.Rossi@Example.INVALID "), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if got := colonneEmail(t, d, c.ID); got != "ada.rossi@example.invalid|-|-" {
		t.Errorf("colonne: %s", got)
	}
	if got := ultimoAudit(t, d, azioneRichiesta); got != azioneRichiesta+`|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+`","party":"customer","resend":false}` {
		t.Errorf("audit: %s", got)
	}
	e := ultimaEmail(t, d)
	if e.parte != "customer" || e.indirizzo != "ada.rossi@example.invalid" || e.stato != "queued" ||
		e.oggetto != "Conferma avviso email — busta 0045/26" {
		t.Errorf("email: %+v", e)
	}
	tok := linkDi(t, reLinkConferma, e.testo)
	stop := linkDi(t, reLinkStop, e.testo)
	attesoTesto := "Ciao Cliente Prova,\n\nper ricevere l'avviso via email quando la tua riparazione (busta 0045/26) sarà pronta, " +
		"conferma il tuo indirizzo cliccando questo link:\n\n" + baseEmail + "/stato/email-conferma?t=" + tok +
		"\n\nIl link vale 48 ore. Se non hai richiesto tu questo avviso, ignora questa email.\n\n— Bottega Prova" +
		"\n\n---\nRicevi questa email perché hai attivato l'avviso dalla pagina stato della tua riparazione presso Bottega Prova.\n" +
		"Per disattivare gli avvisi: " + baseEmail + "/stato/email-disattiva?t=" + stop
	if e.testo != attesoTesto {
		t.Errorf("testo\n%q\natteso\n%q", e.testo, attesoTesto)
	}
	// Nessun indirizzo in chiaro nei link, né nel registro.
	if strings.Contains(tok, "ada") || strings.Contains(stop, "ada") || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%ada.rossi%'`) != 0 {
		t.Error("indirizzo in chiaro")
	}
	// Una richiesta nuova toglie la conferma e il segno della pronta di prima.
	esegui(t, d, `UPDATE buste SET email_confermata_il = ?1, email_pronta_inviata_il = ?1 WHERE id = ?2`, istanteProva, c.ID)
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("ada.rossi@example.invalid"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if got := colonneEmail(t, d, c.ID); got != "ada.rossi@example.invalid|-|-" {
		t.Errorf("dopo la seconda: %s", got)
	}
}

// inventario: CLI-39
// I rifiuti che non scrivono niente: consenso, indirizzo, busta chiusa o nel cestino, link morto, D-16.
func TestRichiestaEmailRifiuti(t *testing.T) {
	b, d, p := provaEmail(t)
	c := crea(t, b, modulo())
	audit0 := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for nome, x := range map[string]struct {
		r   RichiestaEmail
		err error
	}{
		"senza consenso":     {RichiestaEmail{Indirizzo: "a@b.co", ConIndirizzo: true}, ErrConsensoEmail},
		"consenso sbagliato": {RichiestaEmail{Indirizzo: "a@b.co", ConIndirizzo: true, Consenso: "si", ConConsenso: true}, ErrConsensoEmail},
		"senza indirizzo":    {RichiestaEmail{Consenso: " on ", ConConsenso: true}, ErrIndirizzoEmail},
		"indirizzo":          {RichiestaEmail{Indirizzo: "a@b", ConIndirizzo: true, Consenso: "true", ConConsenso: true}, ErrIndirizzoEmail},
	} {
		if err := b.ChiediEmailCliente(ctx, c.Token, x.r, baseEmail, pubblico); !errors.Is(err, x.err) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if err := b.ChiediEmailCliente(ctx, "inventato", chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("link morto: %v", err)
	}
	// D-16: l'email spenta, o senza indirizzo pubblico, non scrive niente.
	p.spento = true
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrEmailNonDisponibile) {
		t.Errorf("spenta: %v", err)
	}
	// Spenta, la risposta è la stessa anche per un modulo che con l'email accesa sarebbe sbagliato, e
	// per «Reinvia» (la pagina non aveva il riquadro).
	for nome, r := range map[string]RichiestaEmail{"senza consenso": {Indirizzo: "a@b", ConIndirizzo: true}, "reinvia": reinvia} {
		if err := b.ChiediEmailCliente(ctx, c.Token, r, baseEmail, pubblico); !errors.Is(err, ErrEmailNonDisponibile) {
			t.Errorf("spenta, %s: %v", nome, err)
		}
	}
	p.spento = false
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), "", pubblico); !errors.Is(err, ErrEmailNonDisponibile) {
		t.Errorf("senza indirizzo pubblico: %v", err)
	}
	for _, stato := range []string{"consegnata", "annullata"} {
		esegui(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, c.ID)
		if err := b.chiediEmail(ctx, c.ID, false, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrEmailChiusa) {
			t.Errorf("%s: %v", stato, err)
		}
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione', cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.chiediEmail(ctx, c.ID, false, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, c.ID)
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit0 || conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 ||
		colonneEmail(t, d, c.ID) != "-|-|-" {
		t.Error("scritto qualcosa")
	}
}

// inventario: CLI-39
// Sei richieste all'ora per busta, la settima rifiutata; mezz'ora fa conta ancora, oltre l'ora no; un
// «Reinvia» conta; un'altra busta no.
func TestRichiestaEmailLimite(t *testing.T) {
	b, d, _ := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	c := crea(t, b, modulo())
	altra := crea(t, b, modulo())
	for i := range 5 {
		if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
	}
	o.Sposta(30 * time.Minute)
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); err != nil {
		t.Fatalf("sesta (reinvia): %v", err)
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrTroppeRichiesteMail) {
		t.Fatalf("settima: %v", err)
	}
	if err := b.ChiediEmailCliente(ctx, altra.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatalf("altra busta: %v", err)
	}
	o.Sposta(30*time.Minute + time.Second)
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatalf("dopo l'ora: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ? AND id_risorsa = ?`, azioneRichiesta, itoa(c.ID)); n != 7 {
		t.Errorf("righe: %d", n)
	}
}

// inventario: CLI-39
// «Reinvia conferma»: all'indirizzo in attesa, riga con resend vero, il link di prima resta buono; senza
// indirizzo in attesa (nessuno, o confermato) ErrNessunaAttesa.
func TestReinviaEmail(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); !errors.Is(err, ErrNessunaAttesa) {
		t.Fatalf("senza indirizzo: %v", err)
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	primo := linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(ultimoAudit(t, d, azioneRichiesta), `"resend":true}`) || conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 2 {
		t.Error("reinvia: riga o email")
	}
	if e, err := b.ConfermaEmail(ctx, primo, pubblico); err != nil || e.Gia {
		t.Fatalf("il link di prima dopo un reinvia: %+v %v", e, err)
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); !errors.Is(err, ErrNessunaAttesa) {
		t.Errorf("confermato: %v", err)
	}
}

// inventario: CLI-39
// Una coda che rifiuta: l'indirizzo resta in attesa, nessuna email, la riga del mancato invio con il
// motivo; un testo salvato che non si compone non manda niente (mai il predefinito).
func TestRichiestaEmailNonAccodata(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	vera := b.posta
	b.posta = postaCheRifiuta{vera}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrEmailNonInviata) {
		t.Fatalf("coda che rifiuta: %v", err)
	}
	if colonneEmail(t, d, c.ID) != "a@b.co|-|-" || conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Error("l'indirizzo non è in attesa o c'è un'email")
	}
	if got := ultimoAudit(t, d, azioneNonAccodata); got != azioneNonAccodata+`|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+`","reason":"not_configured"}` {
		t.Errorf("mancato invio: %s", got)
	}
	b.posta = vera
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Ciao {nome}, {sconosciuto} {link_conferma}', ?)`,
		FamEmailConfermaTesto.Chiave(), istanteProva)
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrEmailNonInviata) {
		t.Fatalf("testo rotto: %v", err)
	}
	if !strings.HasSuffix(ultimoAudit(t, d, azioneNonAccodata), `"reason":"invalid_template"}`) || conta(t, d, `SELECT count(*) FROM messaggi_in_uscita`) != 0 {
		t.Error("testo rotto: riga o email")
	}
}

// inventario: CLI-39, CLI-28
// Sulla busta conto terzi chiede l'oreficeria dal suo /r/, salutata con il suo nome; la riga nomina
// l'oreficeria. Un indirizzo nuovo fa fallire le email in coda per quello di prima.
func TestRichiestaEmailOreficeria(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("orefice@example.invalid"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	e := ultimaEmail(t, d)
	if e.parte != "oreficeria" || !strings.HasPrefix(e.testo, "Ciao Oreficeria Esempio,") {
		t.Errorf("email: %+v", e)
	}
	if !strings.Contains(ultimoAudit(t, d, azioneRichiesta), `"oreficeria_id":2,"party":"oreficeria"`) {
		t.Errorf("audit: %s", ultimoAudit(t, d, azioneRichiesta))
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("altro@example.invalid"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'orefice@example.invalid' AND stato = 'failed'
		AND ultimo_errore = 'Avvisi email disattivati dal destinatario'`); n != 1 {
		t.Errorf("email di prima non ritirata: %d", n)
	}
}

// inventario: CLI-39, CLI-28
// Il riquadro: lo stato dell'indirizzo, la consegna dell'ultima email di conferma, niente su una busta
// chiusa né sul link storico di una busta conto terzi, e l'indirizzo dell'altra parte come nessuno.
func TestRiquadroEmail(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	riq := func(storico bool) string {
		t.Helper()
		r, err := b.RiquadroEmail(ctx, c.ID, storico)
		if err != nil {
			t.Fatal(err)
		}
		if r == nil {
			return "nil"
		}
		return r.Stato + "|" + r.Indirizzo + "|" + r.Consegna
	}
	if got := riq(false); got != "none||" {
		t.Errorf("vuoto: %s", got)
	}
	// Un indirizzo importato in attesa: nessuna richiesta del Go, consegna sconosciuta.
	esegui(t, d, `UPDATE buste SET email_cliente = 'imp@example.invalid' WHERE id = ?`, c.ID)
	if got := riq(false); got != "pending|imp@example.invalid|unknown" {
		t.Errorf("importato: %s", got)
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if got := riq(false); got != "pending|a@b.co|queued" {
		t.Errorf("in coda: %s", got)
	}
	for _, stato := range []string{"accepted", "failed"} {
		esegui(t, d, `UPDATE messaggi_in_uscita SET stato = ?`, stato)
		if got := riq(false); got != "pending|a@b.co|"+stato {
			t.Errorf("%s: %s", stato, got)
		}
	}
	// Una richiesta dopo, che non ha accodato niente: l'email di prima non parla per lei.
	b.o.(interface{ Sposta(time.Duration) }).Sposta(time.Second)
	vera := b.posta
	b.posta = postaCheRifiuta{vera}
	_ = b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico)
	b.posta = vera
	if got := riq(false); got != "pending|a@b.co|not_queued" {
		t.Errorf("non accodata: %s", got)
	}
	// Un'email a un altro indirizzo non parla per quello in attesa.
	esegui(t, d, `INSERT INTO messaggi_in_uscita (busta_id, canale, destinatario, indirizzo, testo, stato, tentativi, prossimo_tentativo_il, accodato_il)
		VALUES (?1, 'email', 'customer', 'altro@b.co', 'x', 'accepted', 0, ?2, ?2)`, c.ID, b.o.Adesso().UTC().Format(db.FormatoIstante))
	if got := riq(false); got != "pending|a@b.co|not_queued" {
		t.Errorf("altro indirizzo: %s", got)
	}
	esegui(t, d, `UPDATE buste SET email_confermata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if got := riq(false); got != "confirmed|a@b.co|" || riq(true) != "confirmed|a@b.co|" {
		t.Errorf("confermato: %s", got)
	}
	// Passata a un'oreficeria: l'indirizzo del cliente non è suo; il link storico non ha il riquadro.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if got := riq(false); got != "none||" || riq(true) != "nil" {
		t.Errorf("oreficeria: %s %s", got, riq(true))
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if riq(true) != "nil" || riq(false) == "nil" {
		t.Error("revocato: lo storico ha il riquadro o il link no")
	}
	for _, stato := range []string{"consegnata", "annullata"} {
		esegui(t, d, `UPDATE buste SET stato = ? WHERE id = ?`, stato, c.ID)
		if riq(false) != "nil" {
			t.Errorf("%s: riquadro", stato)
		}
	}
}

// inventario: CLI-39, CLI-28
// Un indirizzo dell'altra parte: l'oreficeria non lo vede, non lo rimanda e non lo spegne; un'oreficeria
// diversa nemmeno; una riga di un'oreficeria senza id è di nessuno; la nuova controparte chiede il suo.
func TestEmailDellAltraParte(t *testing.T) {
	b, d, _ := provaEmail(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Altra Oreficeria')`)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("cliente@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); !errors.Is(err, ErrNessunaAttesa) {
		t.Errorf("reinvia dell'altra parte: %v", err)
	}
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil || colonneEmail(t, d, c.ID) != "cliente@b.co|-|-" {
		t.Errorf("disattiva dell'altra parte: %v %s", err, colonneEmail(t, d, c.ID))
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("oref@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if r, _ := b.RiquadroEmail(ctx, c.ID, false); r.Stato != "pending" {
		t.Errorf("la nuova controparte: %+v", r)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 3 WHERE id = ?`, c.ID)
	if r, _ := b.RiquadroEmail(ctx, c.ID, false); r.Stato != "none" {
		t.Errorf("un'altra oreficeria: %+v", r)
	}
	// Riga di un'oreficeria scritta prima che nominasse l'oreficeria: di nessuno.
	esegui(t, d, `UPDATE eventi_audit SET dati = json_remove(dati, '$.oreficeria_id') WHERE azione = ?`, azioneRichiesta)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if r, _ := b.RiquadroEmail(ctx, c.ID, false); r.Stato != "none" {
		t.Errorf("riga senza oreficeria: %+v", r)
	}
	// Un indirizzo importato è del cliente: non sul link di una busta conto terzi.
	imp := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	esegui(t, d, `UPDATE buste SET email_cliente = 'imp@b.co', email_confermata_il = ? WHERE id = ?`, istanteProva, imp.ID)
	if r, _ := b.RiquadroEmail(ctx, imp.ID, false); r.Stato != "none" {
		t.Errorf("importato su conto terzi: %+v", r)
	}
}

// inventario: CLI-40
// Il link di conferma: aprirlo non scrive; il pulsante conferma una volta con la riga; la seconda volta
// risponde lo stesso senza scrivere; il link stabile solo se vivo e rileggibile.
func TestConfermaEmail(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	tok := linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
	prima := scatta(t, d)
	if e, err := b.ControllaConferma(ctx, tok); err != nil || e.Gia || e.Link != "" {
		t.Fatalf("controllo: %+v %v", e, err)
	}
	uguali(t, "controllo", prima, scatta(t, d))
	e, err := b.ConfermaEmail(ctx, tok, pubblico)
	if err != nil || e.Gia || e.Link != c.Token {
		t.Fatalf("conferma: %+v %v", e, err)
	}
	if colonneEmail(t, d, c.ID) != "a@b.co|x|-" || ultimoAudit(t, d, azioneConferma) != azioneConferma+`|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+`"}` {
		t.Errorf("dopo la conferma: %s %s", colonneEmail(t, d, c.ID), ultimoAudit(t, d, azioneConferma))
	}
	audit1 := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	if e, err := b.ConfermaEmail(ctx, tok, pubblico); err != nil || !e.Gia {
		t.Errorf("seconda: %+v %v", e, err)
	}
	if e, err := b.ControllaConferma(ctx, tok); err != nil || !e.Gia || e.Link != c.Token {
		t.Errorf("controllo dopo: %+v %v", e, err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != audit1 {
		t.Error("la seconda ha scritto")
	}
	// Il link stabile solo con il testo, non revocato e non scaduto.
	for nome, q := range map[string]string{
		"solo impronta": `UPDATE buste SET token_pubblico = NULL WHERE id = ?`,
		"revocato":      `UPDATE buste SET token_pubblico_revocato_il = '2026-10-31T08:00:00.000000Z' WHERE id = ?`,
		"scaduto":       `UPDATE buste SET token_pubblico_scade_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`,
	} {
		altra := crea(t, b, modulo())
		if err := b.ChiediEmailCliente(ctx, altra.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
			t.Fatal(err)
		}
		tk := linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
		esegui(t, d, q, altra.ID)
		if e, err := b.ConfermaEmail(ctx, tk, pubblico); err != nil || e.Link != "" {
			t.Errorf("%s: %+v %v", nome, e, err)
		}
	}
}

// inventario: CLI-40
// I rifiuti del link di conferma: falso, rovinato, di un altro scopo, scaduto dopo 48 ore, indirizzo
// cambiato o tolto, busta nel cestino o sparita, sostituito da una richiesta o da una disattivazione;
// la forma dello standalone (D-02). Nessuno scrive.
func TestConfermaEmailRifiuti(t *testing.T) {
	b, d, _ := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	c := crea(t, b, modulo())
	nuovo := func() string {
		t.Helper()
		o.Sposta(11 * time.Minute) // sotto il limite di sei all'ora
		if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
			t.Fatal(err)
		}
		return linkDi(t, reLinkConferma, ultimaEmail(t, d).testo)
	}
	tok := nuovo()
	stop := linkDi(t, reLinkStop, ultimaEmail(t, d).testo)
	prova := func(nome, tk string, atteso error) {
		t.Helper()
		if _, err := b.ControllaConferma(ctx, tk); !errors.Is(err, atteso) {
			t.Errorf("%s (controllo): %v", nome, err)
		}
		if _, err := b.ConfermaEmail(ctx, tk, pubblico); !errors.Is(err, atteso) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	dati, mac, _ := strings.Cut(tok, ".")
	for nome, tk := range map[string]string{
		"vuoto": "", "inventato": "abc", "senza firma": dati, "firma rovinata": dati + "." + mac[:len(mac)-2] + "AA",
		"tre parti": tok + ".x", "link di stop": stop,
		"altra chiave": func() string {
			x := *b
			x.chiaveEmail = []byte("un'altra-chiave-di-32-byte-esatt")
			s, _ := x.firmaLink(scopoConferma, itoa(c.ID), b.improntaIndirizzo("a@b.co"), "1")
			return s
		}(),
		"forma sbagliata": func() string { s, _ := b.firmaLink(scopoConferma, itoa(c.ID), "x"); return s }(),
		"id non numero":   func() string { s, _ := b.firmaLink(scopoConferma, "uno", "x", "1"); return s }(),
		// Un link di disattivazione con tre campi non è una conferma: lo scopo è nei dati firmati.
		"stop con tre campi": func() string {
			s, _ := b.firmaLink(scopoStop, itoa(c.ID), b.improntaIndirizzo("a@b.co"), "999999")
			return s
		}(),
	} {
		prova(nome, tk, ErrLinkEmailNonValido)
	}
	prova("phoenix", "SFMyNTY.g2gDdAAAAAJ.Xj3_k-9", ErrLinkEmailVecchio)
	senzaChiave := *b
	senzaChiave.chiaveEmail = nil
	if _, err := senzaChiave.ControllaConferma(ctx, tok); !errors.Is(err, ErrLinkEmailNonValido) {
		t.Errorf("senza chiave: %v", err)
	}
	// 48 ore esatte valgono, un millesimo dopo no.
	o.Sposta(48 * time.Hour)
	if _, err := b.ControllaConferma(ctx, tok); err != nil {
		t.Errorf("48 ore: %v", err)
	}
	o.Sposta(time.Millisecond)
	prova("scaduto", tok, ErrLinkEmailNonValido)
	tok = nuovo()
	esegui(t, d, `UPDATE buste SET email_cliente = 'b@b.co' WHERE id = ?`, c.ID)
	prova("cambiato", tok, ErrIndirizzoCambiato)
	esegui(t, d, `UPDATE buste SET email_cliente = NULL WHERE id = ?`, c.ID)
	prova("tolto", tok, ErrIndirizzoCambiato)
	tok = nuovo()
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	prova("cestino", tok, ErrIndirizzoCambiato)
	esegui(t, d, `UPDATE buste SET cancellata_il = NULL WHERE id = ?`, c.ID)
	sparita, _ := b.firmaLink(scopoConferma, "999", b.improntaIndirizzo("a@b.co"), "1")
	prova("sparita", sparita, ErrIndirizzoCambiato)
	// Una disattivazione e una richiesta nuova per lo stesso indirizzo ritirano il primo link; A → B → A
	// pure.
	tok = nuovo()
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	_ = nuovo()
	prova("dopo disattiva e richiesta", tok, ErrRichiestaSostituta)
	tok = nuovo()
	o.Sposta(11 * time.Minute)
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("b@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	_ = nuovo()
	prova("A, B, A", tok, ErrRichiestaSostituta)
	// Una disattivazione che ritorna lo stesso indirizzo senza richiesta: sostituito comunque.
	tok = nuovo()
	esegui(t, d, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES ('public', ?, 'repair', ?, 'ok', '{"via":"footer"}', ?)`,
		azioneDisattivata, itoa(c.ID), istanteProva)
	prova("disattivazione", tok, ErrRichiestaSostituta)
	// Una richiesta su un'altra busta non ritira niente.
	tok = nuovo()
	altra := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, altra.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ControllaConferma(ctx, tok); err != nil {
		t.Errorf("altra busta: %v", err)
	}
	// Il link conferma solo la sua busta, anche con lo stesso indirizzo.
	if _, err := b.ConfermaEmail(ctx, tok, pubblico); err != nil || colonneEmail(t, d, altra.ID) != "a@b.co|-|-" {
		t.Errorf("la sua busta sola: %v %s", err, colonneEmail(t, d, altra.ID))
	}
}

// inventario: CLI-41
// «Disattiva» dalla pagina: le tre colonne vuote, le email in coda ritirate, la riga con via «page» e
// mai l'indirizzo; la seconda non trova niente e non scrive; anche in attesa e su una busta chiusa.
func TestDisattivaEmailPagina(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	if colonneEmail(t, d, c.ID) != "-|-|-" || ultimoAudit(t, d, azioneDisattivata) != azioneDisattivata+`|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+`","via":"page"}` {
		t.Errorf("dopo: %s %s", colonneEmail(t, d, c.ID), ultimoAudit(t, d, azioneDisattivata))
	}
	if e := ultimaEmail(t, d); e.stato != "failed" {
		t.Errorf("email in coda: %+v", e)
	}
	n := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil || conta(t, d, `SELECT count(*) FROM eventi_audit`) != n {
		t.Errorf("seconda: %v", err)
	}
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = ?, stato = 'consegnata' WHERE id = ?`, istanteProva, c.ID)
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil || colonneEmail(t, d, c.ID) != "-|-|-" {
		t.Errorf("busta chiusa: %v %s", err, colonneEmail(t, d, c.ID))
	}
	if err := b.DisattivaEmailCliente(ctx, "inventato", pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("link morto: %v", err)
	}
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	if err := b.disattivaEmail(ctx, c.ID, false, pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.disattivaEmail(ctx, c.ID, false, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
}

// inventario: CLI-41
// Sei disattivazioni all'ora dalla pagina per busta; quelle del piè e quelle di più di un'ora prima non
// contano.
func TestDisattivaEmailLimite(t *testing.T) {
	b, d, _ := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	c := crea(t, b, modulo())
	riga := func(via string) {
		esegui(t, d, `INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il) VALUES ('public', ?, 'repair', ?, 'ok', ?, ?)`,
			azioneDisattivata, itoa(c.ID), `{"via":"`+via+`"}`, b.o.Adesso().UTC().Format(db.FormatoIstante))
	}
	riga("page")
	o.Sposta(time.Hour + time.Second)
	for range 3 {
		riga("footer")
		riga("link")
	}
	for i := range 6 {
		if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		riga("page")
		if i == 4 {
			break
		}
	}
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); err != nil {
		t.Fatalf("sesta: %v", err)
	}
	riga("page")
	if err := b.DisattivaEmailCliente(ctx, c.Token, pubblico); !errors.Is(err, ErrTroppeRichiesteMail) {
		t.Errorf("settima: %v", err)
	}
}

// inventario: CLI-41
// Il link nel piè: aprirlo non scrive; il pulsante vuota le tre colonne con via «footer» (anche nel
// cestino); un indirizzo cambiato, tolto o una busta sparita danno lo stesso successo senza cambiare
// niente; dura 10 anni; una conferma non è uno stop; la forma dello standalone (D-02).
func TestDisattivaConLink(t *testing.T) {
	b, d, _ := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	e := ultimaEmail(t, d)
	stop, conf := linkDi(t, reLinkStop, e.testo), linkDi(t, reLinkConferma, e.testo)
	prima := scatta(t, d)
	if err := b.ControllaStop(stop); err != nil {
		t.Fatal(err)
	}
	uguali(t, "controllo", prima, scatta(t, d))
	for nome, tk := range map[string]string{"conferma": conf, "inventato": "abc.def", "vuoto": ""} {
		if err := b.ControllaStop(tk); !errors.Is(err, ErrLinkEmailNonValido) {
			t.Errorf("%s: %v", nome, err)
		}
		if err := b.DisattivaConLink(ctx, tk, pubblico); !errors.Is(err, ErrLinkEmailNonValido) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	if err := b.DisattivaConLink(ctx, "SFMyNTY.g2gDdAAAAAJ.Xj3_k-9", pubblico); !errors.Is(err, ErrLinkEmailVecchio) {
		t.Errorf("phoenix: %v", err)
	}
	// Un link di un indirizzo cambiato: lo stesso successo, e il nuovo resta.
	esegui(t, d, `UPDATE buste SET email_cliente = 'b@b.co' WHERE id = ?`, c.ID)
	if err := b.DisattivaConLink(ctx, stop, pubblico); err != nil || colonneEmail(t, d, c.ID) != "b@b.co|-|-" {
		t.Errorf("cambiato: %v %s", err, colonneEmail(t, d, c.ID))
	}
	// Nel cestino si spegne lo stesso; nove anni dopo vale ancora.
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = ?1, email_pronta_inviata_il = ?1, cancellata_il = ?1 WHERE id = ?2`, istanteProva, c.ID)
	o.Sposta(9 * 365 * 24 * time.Hour)
	if err := b.DisattivaConLink(ctx, stop, pubblico); err != nil || colonneEmail(t, d, c.ID) != "-|-|-" {
		t.Errorf("cestino: %v %s", err, colonneEmail(t, d, c.ID))
	}
	if ultimoAudit(t, d, azioneDisattivata) != azioneDisattivata+`|repair|`+itoa(c.ID)+`|ok|{"bag_number":"`+c.Numero+`","via":"footer"}` {
		t.Errorf("audit: %s", ultimoAudit(t, d, azioneDisattivata))
	}
	n := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	if err := b.DisattivaConLink(ctx, stop, pubblico); err != nil || conta(t, d, `SELECT count(*) FROM eventi_audit`) != n {
		t.Errorf("seconda: %v", err)
	}
	sparita, _ := b.firmaLink(scopoStop, "999", b.improntaIndirizzo("a@b.co"))
	if err := b.DisattivaConLink(ctx, sparita, pubblico); err != nil {
		t.Errorf("sparita: %v", err)
	}
	// Dieci anni esatti valgono, un millesimo dopo no.
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	o.Sposta(365 * 24 * time.Hour)
	if err := b.ControllaStop(stop); err != nil {
		t.Errorf("dieci anni: %v", err)
	}
	o.Sposta(time.Millisecond)
	if err := b.DisattivaConLink(ctx, stop, pubblico); !errors.Is(err, ErrLinkEmailNonValido) || colonneEmail(t, d, c.ID) != "a@b.co|-|-" {
		t.Errorf("scaduto: %v", err)
	}
}

// inventario: CLI-41
// Disattivare toglie solo la busta del link, mai un'altra con lo stesso indirizzo.
func TestDisattivaConLinkSoloLaSua(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	altra := crea(t, b, modulo())
	for _, x := range []Creata{c, altra} {
		if err := b.ChiediEmailCliente(ctx, x.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
			t.Fatal(err)
		}
	}
	stop := linkDi(t, reLinkStop, ultimaEmail(t, d).testo) // quello dell'altra
	if err := b.DisattivaConLink(ctx, stop, pubblico); err != nil {
		t.Fatal(err)
	}
	if colonneEmail(t, d, c.ID) != "a@b.co|-|-" || colonneEmail(t, d, altra.ID) != "-|-|-" {
		t.Errorf("%s %s", colonneEmail(t, d, c.ID), colonneEmail(t, d, altra.ID))
	}
}

// inventario: CLI-19, CLI-17, CLI-39, CLI-41
// Dalla pagina storica con la stessa credenziale: richiesta e disattivazione su una busta del cliente;
// codice sbagliato, busta revocata e busta conto terzi sono il 404 con la riga del rifiuto.
func TestEmailDallaPaginaStorica(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET hash_codice_flask = ? WHERE id = ?`, firmeflask.ImprontaCodice(c.Numero, "segreto1"), c.ID)
	giusta := CredenzialeStorica{Codice: "segreto1", ConCodice: true}
	sbagliata := CredenzialeStorica{Codice: "no", ConCodice: true}
	if err := b.ChiediEmailStorica(ctx, c.Numero, giusta, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if colonneEmail(t, d, c.ID) != "a@b.co|-|-" {
		t.Error("storica: niente indirizzo")
	}
	if err := b.DisattivaEmailStorica(ctx, c.Numero, giusta, pubblico); err != nil || colonneEmail(t, d, c.ID) != "-|-|-" {
		t.Errorf("disattiva storica: %v", err)
	}
	negati := func() int {
		return conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.legacy_public_denied' AND id_risorsa = 'unknown'`)
	}
	n := negati()
	if err := b.ChiediEmailStorica(ctx, c.Numero, sbagliata, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("sbagliata: %v", err)
	}
	if err := b.DisattivaEmailStorica(ctx, c.Numero, sbagliata, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("sbagliata (disattiva): %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2, email_cliente = 'x@b.co' WHERE id = ?`, c.ID)
	if err := b.ChiediEmailStorica(ctx, c.Numero, giusta, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("conto terzi: %v", err)
	}
	if err := b.DisattivaEmailStorica(ctx, c.Numero, giusta, pubblico); !errors.Is(err, ErrBustaNonTrovata) || colonneEmail(t, d, c.ID) != "x@b.co|-|-" {
		t.Errorf("conto terzi (disattiva): %v", err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL, codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.ChiediEmailStorica(ctx, c.Numero, giusta, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("revocata: %v", err)
	}
	if negati() != n+5 {
		t.Errorf("righe del rifiuto: %d", negati()-n)
	}
	// La revoca arrivata fra la verifica della rotta e la scrittura: la transazione la vede.
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = NULL WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE buste SET codice_flask_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.chiediEmail(ctx, c.ID, true, chiedi("a@b.co"), baseEmail, pubblico); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("revocata nella transazione: %v", err)
	}
}

// inventario: CLI-41
// La pagina di ripiego (D-02, solo Go): un'email con un link nuovo per ogni busta che porta l'indirizzo
// (anche in attesa, anche nel cestino), che disattiva davvero; nessuna per un indirizzo che non c'è,
// non valido o con l'email spenta; al più tre all'ora per busta.
func TestMandaLinkDisattivazione(t *testing.T) {
	b, d, p := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	c := crea(t, b, modulo())
	cestino := crea(t, b, con(modulo(), func(a *Accettazione) { a.OreficeriaID = "2" }))
	altra := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co', email_confermata_il = ? WHERE id = ?`, istanteProva, c.ID)
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co', cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	esegui(t, d, `UPDATE buste SET email_cliente = 'altro@b.co' WHERE id = ?`, altra.ID)
	n, err := b.MandaLinkDisattivazione(ctx, " A@B.CO ", baseEmail, pubblico)
	if err != nil || n != 2 {
		t.Fatalf("mandate %d: %v", n, err)
	}
	e := ultimaEmail(t, d) // la più recente è la prima busta (ordine per id decrescente)
	stop := linkDi(t, reLinkStop, e.testo)
	atteso := "Ciao Cliente Prova,\n\nci è arrivata una richiesta di disattivare gli avvisi email della tua riparazione (busta " + c.Numero +
		"). Per disattivarli apri questo link e premi «Disattiva»:\n\n" + baseEmail + "/stato/email-disattiva?t=" + stop +
		"\n\nSe non l'hai chiesto tu, ignora questa email: gli avvisi restano attivi.\n\n— Negozio"
	if e.testo != atteso || e.oggetto != "Disattiva gli avvisi email — busta "+c.Numero || e.parte != "customer" || e.indirizzo != "a@b.co" {
		t.Errorf("email\n%+v\natteso\n%q", e, atteso)
	}
	// Nel cestino, e passata a un'oreficeria con l'indirizzo del cliente (importato): al cliente.
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id = ? AND destinatario = 'customer' AND testo LIKE 'Ciao cliente,%'`, cestino.ID) != 1 {
		t.Error("busta nel cestino senza email")
	}
	if ultimoAudit(t, d, azioneStopInviato) != azioneStopInviato+`|repair|`+itoa(c.ID)+`|ok|{"address_digest":"`+b.improntaIndirizzo("a@b.co")+
		`","bag_number":"`+c.Numero+`","party":"customer"}` {
		t.Errorf("audit: %s", ultimoAudit(t, d, azioneStopInviato))
	}
	if err := b.DisattivaConLink(ctx, stop, pubblico); err != nil || colonneEmail(t, d, c.ID) != "-|-|-" || colonneEmail(t, d, altra.ID) != "altro@b.co|-|-" {
		t.Errorf("il link nuovo: %v %s", err, colonneEmail(t, d, c.ID))
	}
	for _, x := range []string{"nessuno@b.co", "non valido", ""} {
		if n, err := b.MandaLinkDisattivazione(ctx, x, baseEmail, pubblico); n != 0 || err != nil {
			t.Errorf("%q: %d %v", x, n, err)
		}
	}
	p.spento = true
	if n, _ := b.MandaLinkDisattivazione(ctx, "altro@b.co", baseEmail, pubblico); n != 0 {
		t.Errorf("email spenta: %d", n)
	}
	p.spento = false
	if n, _ := b.MandaLinkDisattivazione(ctx, "altro@b.co", "", pubblico); n != 0 {
		t.Errorf("senza indirizzo pubblico: %d", n)
	}
	for i := range 4 {
		n, _ := b.MandaLinkDisattivazione(ctx, "altro@b.co", baseEmail, pubblico)
		if want := map[bool]int{true: 1, false: 0}[i < 3]; n != want {
			t.Errorf("invio %d: %d", i, n)
		}
	}
	// Tre all'ora per busta anche con un altro indirizzo; tre al giorno per indirizzo.
	esegui(t, d, `UPDATE buste SET email_cliente = 'terzo@b.co' WHERE id = ?`, altra.ID)
	if n, _ := b.MandaLinkDisattivazione(ctx, "terzo@b.co", baseEmail, pubblico); n != 0 {
		t.Errorf("busta al limite: %d", n)
	}
	o.Sposta(time.Hour + time.Second)
	if n, _ := b.MandaLinkDisattivazione(ctx, "terzo@b.co", baseEmail, pubblico); n != 1 {
		t.Errorf("dopo l'ora: %d", n)
	}
	esegui(t, d, `UPDATE buste SET email_cliente = 'altro@b.co' WHERE id = ?`, altra.ID)
	if n, _ := b.MandaLinkDisattivazione(ctx, "altro@b.co", baseEmail, pubblico); n != 0 {
		t.Errorf("indirizzo al limite del giorno: %d", n)
	}
	o.Sposta(23 * time.Hour)
	if n, _ := b.MandaLinkDisattivazione(ctx, "altro@b.co", baseEmail, pubblico); n != 1 {
		t.Errorf("il giorno dopo: %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%@b.co%'`) != 0 {
		t.Error("indirizzo nel registro")
	}
}

// inventario: CLI-41
// Le buste più recenti per prime: con il tetto di tre email al giorno per indirizzo (review della
// 070) vanno le tre più recenti (il tetto di dieci buste per invio resta per la ricerca).
func TestMandaLinkDisattivazioneDieci(t *testing.T) {
	b, d, _ := provaEmail(t)
	var ultima int64
	for range 12 {
		c := crea(t, b, modulo())
		esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
		ultima = c.ID
	}
	if n, err := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 3 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE busta_id <= ?`, ultima-3) != 0 {
		t.Error("una busta vecchia")
	}
}

// inventario: CLI-40
// La forma dei Phoenix.Token dello standalone, riconosciuta e mai verificata (D-02).
func TestFormaPhoenix(t *testing.T) {
	for tok, atteso := range map[string]bool{
		"SFMyNTY.g2gDdAAAAAJtAAAAAWFtAAAA.Xj3_k-9AbC": true,
		"SFMyNTY.abc.def":  true,
		"SFMyNTY.abc":      false,
		"SFMyNTY..def":     false,
		"SFMyNTY.ab+c.def": false,
		"SFMyNTY.abc.de=f": false,
		"XFMyNTY.abc.def":  false,
		"SFMyNTY.a.b.c":    false,
		"abc.def":          false,
	} {
		if FormaPhoenix(tok) != atteso {
			t.Errorf("%q: %v", tok, !atteso)
		}
	}
}

// inventario: CLI-39
// {nome} senza nome è «cliente»; l'oggetto è su una riga e senza spazi ai bordi anche con un nome di più
// righe (one_line/1).
func TestEmailNomeEOggetto(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET nome_cliente = ' ' WHERE id = ?`, c.ID)
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if e := ultimaEmail(t, d); !strings.HasPrefix(e.testo, "Ciao cliente,\n") {
		t.Errorf("senza nome: %q", e.testo)
	}
	esegui(t, d, `UPDATE buste SET nome_cliente = ? WHERE id = ?`, "Ada\r\n Rossi", c.ID)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'Conferma {nome} ', ?)`,
		FamEmailConfermaOggetto.Chiave(), istanteProva)
	if err := b.ChiediEmailCliente(ctx, c.Token, reinvia, baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	if e := ultimaEmail(t, d); e.oggetto != "Conferma Ada  Rossi" {
		t.Errorf("oggetto: %q", e.oggetto)
	}
}

// inventario: CLI-40, CLI-41
// L'impronta dell'indirizzo ha la chiave: con un'altra chiave cambia, e nei dati del link non c'è né
// l'indirizzo (nemmeno in base64) né lo SHA-256 nudo che chiunque calcolerebbe (address_digest/1).
func TestImprontaConChiave(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	altra := *b
	altra.chiaveEmail = []byte("un'altra-chiave-di-32-byte-esatt")
	if b.improntaIndirizzo("a@b.co") == altra.improntaIndirizzo("a@b.co") {
		t.Fatal("la stessa impronta con due chiavi")
	}
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	nudo := sha256.Sum256([]byte("repair email address:a@b.co"))
	vietati := []string{"a@b.co", hex.EncodeToString(nudo[:]), b64.EncodeToString(nudo[:]),
		base64.StdEncoding.EncodeToString(nudo[:]), b64.EncodeToString([]byte("a@b.co")), base64.StdEncoding.EncodeToString([]byte("a@b.co"))}
	testo := ultimaEmail(t, d).testo
	for _, re := range []*regexp.Regexp{reLinkConferma, reLinkStop} {
		tok := linkDi(t, re, testo)
		d64, _, _ := strings.Cut(tok, ".")
		dati, err := b64.DecodeString(d64)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vietati {
			if strings.Contains(string(dati), v) || strings.Contains(tok, v) {
				t.Errorf("nel link %q c'è %q", dati, v)
			}
		}
	}
}

// inventario: CLI-39
// L'email di conferma si accoda anche se la richiesta è già stata annullata dopo la scrittura (il
// browser ha chiuso la connessione): niente indirizzo in attesa senza email né riga.
func TestMandaConfermaContestoAnnullato(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	be, err := leggiBustaEmail(ctx, b.d.Lettura, `id = ?`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	annullato, ferma := context.WithCancel(ctx)
	ferma()
	if err := b.mandaConferma(annullato, be, 1, baseEmail, pubblico); err != nil {
		t.Fatalf("contesto annullato: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_in_uscita WHERE indirizzo = 'a@b.co'`) != 1 {
		t.Error("email non accodata")
	}
}

// inventario: CLI-41
// Ripiego: la riga del link si scrive prima dell'invio (anche se la coda rifiuta conta), e al più tre
// email di ripiego al giorno per indirizzo, su tutte le buste.
func TestRipiegoTetti(t *testing.T) {
	b, d, _ := provaEmail(t)
	o := b.o.(interface{ Sposta(time.Duration) })
	for range 4 {
		c := crea(t, b, modulo())
		esegui(t, d, `UPDATE buste SET email_cliente = 'a@b.co' WHERE id = ?`, c.ID)
	}
	if n, err := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 3 || err != nil {
		t.Fatalf("primo invio: %d %v", n, err)
	}
	o.Sposta(2 * time.Hour)
	if n, _ := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 0 {
		t.Errorf("stesso giorno: %d", n)
	}
	o.Sposta(23 * time.Hour)
	vera := b.posta
	b.posta = postaCheRifiuta{vera}
	righe := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneStopInviato)
	if n, _ := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 0 {
		t.Errorf("coda che rifiuta: %d", n)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneStopInviato) != righe+3 {
		t.Error("le righe non sono scritte prima dell'invio")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%a@b.co%'`) != 0 {
		t.Error("indirizzo nel registro")
	}
}

// inventario: CLI-41, CLI-28
// Ripiego su una busta passata a un'oreficeria con l'indirizzo chiesto dal cliente: l'email va al
// cliente come «cliente», mai con il nome dell'oreficeria.
func TestRipiegoIndirizzoDellAltraParte(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if n, _ := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 1 {
		t.Fatalf("mandate %d", n)
	}
	e := ultimaEmail(t, d)
	if e.parte != "customer" || !strings.HasPrefix(e.testo, "Ciao cliente,") || strings.Contains(e.testo, "Oreficeria Esempio") {
		t.Errorf("email: %+v", e)
	}
}

// inventario: CLI-39
// La consegna del riquadro è quella dell'email di conferma accodata dalla richiesta, non dell'ultima
// email all'indirizzo (un'email di ripiego non la cambia).
func TestConsegnaSoloDellaConferma(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	if err := b.ChiediEmailCliente(ctx, c.Token, chiedi("a@b.co"), baseEmail, pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE messaggi_in_uscita SET stato = 'accepted'`)
	if n, _ := b.MandaLinkDisattivazione(ctx, "a@b.co", baseEmail, pubblico); n != 1 {
		t.Fatal("ripiego non mandato")
	}
	if r, _ := b.RiquadroEmail(ctx, c.ID, false); r.Consegna != "accepted" {
		t.Errorf("consegna: %+v", r)
	}
}

// inventario: CLI-41
// Il ripiego trova anche un indirizzo importato con maiuscole o spazi, e il link nuovo lo spegne.
func TestRipiegoIndirizzoImportato(t *testing.T) {
	b, d, _ := provaEmail(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET email_cliente = ' Ada@B.co ', email_confermata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if n, err := b.MandaLinkDisattivazione(ctx, "ada@b.co", baseEmail, pubblico); n != 1 || err != nil {
		t.Fatalf("mandate %d %v", n, err)
	}
	e := ultimaEmail(t, d)
	if e.indirizzo != "ada@b.co" {
		t.Errorf("indirizzo: %q", e.indirizzo)
	}
	if err := b.DisattivaConLink(ctx, linkDi(t, reLinkStop, e.testo), pubblico); err != nil || colonneEmail(t, d, c.ID) != "-|-|-" {
		t.Errorf("il link non spegne: %v %s", err, colonneEmail(t, d, c.ID))
	}
}
