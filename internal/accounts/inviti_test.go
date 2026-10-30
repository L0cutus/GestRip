package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Gli inviti (ACC-07, ACC-08, ACC-09), come invite_user/2, reissue_invitation/2,
// accept_invitation/2 e invitation_recipient/1 dello standalone.

func invitiProva(t *testing.T) (*Inviti, *Utenti, *db.DB, *clock.Finto) {
	t.Helper()
	u, d := utentiProva(t)
	o := orologio()
	u.o = o
	return NuoviInviti(d, o), u, d, o
}

type rigaInvito struct {
	utente              int64
	hash                []byte
	scade               string
	accettato, revocato any
	creato, aggiornato  string
}

func righeInvito(t *testing.T, d *db.DB) []rigaInvito {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT utente_id, hash_token, scade_il, accettato_il, revocato_il, creato_il, aggiornato_il FROM inviti_utenti ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []rigaInvito
	for rr.Next() {
		var r rigaInvito
		if err := rr.Scan(&r.utente, &r.hash, &r.scade, &r.accettato, &r.revocato, &r.creato, &r.aggiornato); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func conta(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: ACC-07
// L'invito crea un account senza password e un link monouso: 32 byte casuali, nel database solo
// il loro SHA-256, 15 minuti; email normalizzata, nome tenuto, il ruolo chiesto; account,
// invito e riga user.invited con il ruolo nella stessa transazione. Ogni account ha un token suo.
func TestInvita(t *testing.T) {
	i, u, d, o := invitiProva(t)
	inv, err := i.Invita(ctx, "Nuovo@Esempio.IT ", " Nuovo ", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Email != "nuovo@esempio.it" || inv.UtenteID == 0 || !inv.Scade.Equal(o.Adesso().Add(15*time.Minute)) {
		t.Errorf("invito: %+v", inv)
	}
	b, err := base64.RawURLEncoding.DecodeString(inv.Token)
	if err != nil || len(b) != 32 {
		t.Fatalf("token: %q %v", inv.Token, err)
	}
	var email, ruolo string
	var nome, hash any
	var attivo int
	if err := d.Lettura.QueryRow(`SELECT email, ruolo, nome_visibile, hash_password, attivo FROM utenti WHERE id = ?`, inv.UtenteID).
		Scan(&email, &ruolo, &nome, &hash, &attivo); err != nil {
		t.Fatal(err)
	}
	if email != "nuovo@esempio.it" || ruolo != Dipendente || nome != "Nuovo" || hash != nil || attivo != 1 {
		t.Errorf("account: %q %q %v %v %d", email, ruolo, nome, hash, attivo)
	}
	rr := righeInvito(t, d)
	h := sha256.Sum256([]byte(inv.Token))
	if len(rr) != 1 || rr[0].utente != inv.UtenteID || string(rr[0].hash) != string(h[:]) || rr[0].accettato != nil || rr[0].revocato != nil ||
		rr[0].scade != "2026-10-30T09:30:07.000000Z" || rr[0].creato != istante {
		t.Errorf("riga dell'invito: %+v", rr)
	}
	if got := ultimoEvento(t, d); got != "titolare@esempio.it|-|user.invited|user|"+itoa(inv.UtenteID)+`|ok|{"role":"dipendente"}` {
		t.Errorf("registro: %q", got)
	}
	// Un account invitato non entra con nessuna password.
	for _, pw := range []string{"", "qualunque cosa", pwProva} {
		if _, ok, _ := u.Autentica(ctx, "nuovo@esempio.it", pw); ok {
			t.Errorf("l'invitato entra con %q", pw)
		}
	}
	altro, err := i.Invita(ctx, "secondo@esempio.it", "", "titolare", chi)
	if err != nil || altro.Token == inv.Token {
		t.Fatalf("secondo invito: %v", err)
	}
	if err := d.Lettura.QueryRow(`SELECT ruolo, nome_visibile FROM utenti WHERE id = ?`, altro.UtenteID).Scan(&ruolo, &nome); err != nil ||
		ruolo != Titolare || nome != nil {
		t.Errorf("titolare invitato: %q %v %v", ruolo, nome, err)
	}
	if got := ultimoEvento(t, d); !strings.HasSuffix(got, `|ok|{"role":"titolare"}`) {
		t.Errorf("registro del titolare invitato: %q", got)
	}
}

const pwProva = "password-sintetica-di-prova"

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// inventario: ACC-07
// I rifiuti del modulo, senza scrivere niente: email di forma sbagliata (due chiocciole, niente
// punto nel dominio, spazi dentro, vuota), oltre 254 caratteri; nome oltre 120; ruolo che il modulo
// non offre. Le lunghezze contano i caratteri come String.length (grafemi).
func TestInvitaRifiuti(t *testing.T) {
	i, _, d, _ := invitiProva(t)
	lunga := strings.Repeat("a", 254-len("@esempio.it")) + "@esempio.it"
	for _, c := range []struct {
		email, nome, ruolo string
		atteso             error
	}{
		{"a@b@c.esempio", "", "dipendente", ErrEmailNonValida},
		{"root@localhost", "", "dipendente", ErrEmailNonValida},
		{"non-e-un-indirizzo", "", "dipendente", ErrEmailNonValida},
		{"a b@esempio.it", "", "dipendente", ErrEmailNonValida},
		{"a\v@esempio.it", "", "dipendente", ErrEmailNonValida},
		{"a@esem\vpio.it", "", "dipendente", ErrEmailNonValida},
		{"a@esempio.i\vt", "", "dipendente", ErrEmailNonValida},
		{"", "", "dipendente", ErrEmailNonValida},
		{"   ", "", "dipendente", ErrEmailNonValida},
		{"x" + lunga, "", "dipendente", ErrEmailNonValida},
		{"ok@esempio.it", strings.Repeat("n", 121), "dipendente", ErrNomeNonValido},
		{"ok@esempio.it", "", "admin", ErrRuoloNonValido},
		{"ok@esempio.it", "", "owner", ErrRuoloNonValido},
		{"ok@esempio.it", "", "", ErrRuoloNonValido},
		{"ok@esempio.it", "", " dipendente", ErrRuoloNonValido},
		// Il ruolo sbagliato vince su un'email sbagliata (invite_error/1 guarda prima il ruolo).
		{"non-e-un-indirizzo", "", "admin", ErrRuoloNonValido},
	} {
		if _, err := i.Invita(ctx, c.email, c.nome, c.ruolo, chi); !errors.Is(err, c.atteso) {
			t.Errorf("%q %q %q: %v, atteso %v", c.email[:min(len(c.email), 20)], c.nome[:min(len(c.nome), 5)], c.ruolo, err, c.atteso)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM utenti`); n != 4 {
		t.Errorf("account scritti dai rifiuti: %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM inviti_utenti`) + conta(t, d, `SELECT count(*) FROM eventi_audit`); n != 0 {
		t.Errorf("inviti o righe scritti dai rifiuti: %d", n)
	}
	// Al limite si accetta: 254 caratteri di email, 120 di nome, contati come grafemi
	// («é» scomposta conta uno).
	if _, err := i.Invita(ctx, lunga, strings.Repeat("é", 120), "dipendente", chi); err != nil {
		t.Errorf("al limite: %v", err)
	}
	accentata := strings.Repeat("é", 120) + "@esempio.it"
	if _, err := i.Invita(ctx, accentata, "", "dipendente", chi); err != nil {
		t.Errorf("email di 131 grafemi e 251 rune: %v", err)
	}
	// Un nome di soli spazi diventa nullo.
	inv, err := i.Invita(ctx, "spazi@esempio.it", "   ", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	var nome any
	_ = d.Lettura.QueryRow(`SELECT nome_visibile FROM utenti WHERE id = ?`, inv.UtenteID).Scan(&nome)
	if nome != nil {
		t.Errorf("nome di spazi: %v", nome)
	}
}

// inventario: ACC-07
// L'email è unica solo tra gli account attivi: un indirizzo occupato si rifiuta senza scrivere
// niente (anche quello di un account solo invitato); uno liberato da una disattivazione si può dare
// a qualcun altro, che entra con la sua password mentre la riga vecchia resta fuori.
func TestInvitaEmailUnicaTraGliAttivi(t *testing.T) {
	i, u, d, _ := invitiProva(t)
	for _, e := range []string{"titolare@esempio.it", "Invitato@Esempio.it"} {
		if _, err := i.Invita(ctx, e, "", "dipendente", chi); !errors.Is(err, ErrEmailOccupata) {
			t.Errorf("%s: %v", e, err)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM utenti`) + conta(t, d, `SELECT count(*) FROM inviti_utenti`); n != 4 {
		t.Errorf("scritto qualcosa: %d", n)
	}
	inv, err := i.Invita(ctx, "uscito@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if inv.UtenteID == 3 {
		t.Error("riusato l'account disattivato")
	}
	if err := i.Accetta(ctx, inv.Token, "una password lunga"); err != nil {
		t.Fatal(err)
	}
	ut, ok, err := u.Autentica(ctx, "uscito@esempio.it", "una password lunga")
	if !ok || err != nil || ut.ID != inv.UtenteID {
		t.Errorf("il nuovo account non entra: %v %v %d", ok, err, ut.ID)
	}
	if n := conta(t, d, `SELECT attivo FROM utenti WHERE id = 3`); n != 0 {
		t.Error("la riga vecchia è tornata attiva")
	}
}

// inventario: ACC-09
// L'accettazione scrive la password scelta, e solo dopo si entra; il link si spende una volta;
// riga user.invitation_accepted con l'email dell'invitato come attore, senza ip.
func TestAccetta(t *testing.T) {
	i, u, d, o := invitiProva(t)
	inv, err := i.Invita(ctx, "ospite@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if email, ok, err := i.Destinatario(ctx, inv.Token); !ok || err != nil || email != "ospite@esempio.it" {
		t.Errorf("destinatario: %q %v %v", email, ok, err)
	}
	// Aprire non spende.
	if rr := righeInvito(t, d); rr[0].accettato != nil {
		t.Error("il destinatario ha speso il link")
	}
	o.Sposta(time.Minute)
	if err := i.Accetta(ctx, inv.Token, "una password lunga"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := u.Autentica(ctx, "ospite@esempio.it", "una password lunga"); !ok {
		t.Error("l'invitato non entra con la sua password")
	}
	if rr := righeInvito(t, d); rr[0].accettato != "2026-10-30T09:16:07.000000Z" || rr[0].aggiornato != "2026-10-30T09:16:07.000000Z" {
		t.Errorf("link non speso: %+v", rr[0])
	}
	if got := ultimoEvento(t, d); got != "ospite@esempio.it|-|user.invitation_accepted|user|"+itoa(inv.UtenteID)+"|ok|{}" {
		t.Errorf("registro: %q", got)
	}
	if err := i.Accetta(ctx, inv.Token, "un'altra password"); !errors.Is(err, ErrInvitoNonValido) {
		t.Errorf("seconda volta: %v", err)
	}
	if _, ok, _ := i.Destinatario(ctx, inv.Token); ok {
		t.Error("un link speso mostra ancora il destinatario")
	}
	if _, ok, _ := u.Autentica(ctx, "ospite@esempio.it", "una password lunga"); !ok {
		t.Error("la seconda volta ha cambiato la password")
	}
}

// inventario: ACC-09
// Rifiuti dell'accettazione: password corta (il link resta e poi vale), link scaduto, inventato,
// lungo, revocato, o di un account disattivato nel frattempo (nessuna password scritta). Ogni
// rifiuto del link è lo stesso errore.
func TestAccettaRifiuti(t *testing.T) {
	i, _, d, o := invitiProva(t)
	inv, err := i.Invita(ctx, "ospite@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if err := i.Accetta(ctx, inv.Token, "corta"); !errors.Is(err, ErrPasswordCorta) {
		t.Errorf("corta: %v", err)
	}
	if err := i.Accetta(ctx, inv.Token, strings.Repeat("é", 11)); !errors.Is(err, ErrPasswordCorta) {
		t.Errorf("11 grafemi: %v", err)
	}
	if rr := righeInvito(t, d); rr[0].accettato != nil {
		t.Error("una password corta ha speso il link")
	}
	for _, tok := range []string{"token-inventato", "", strings.Repeat("A", 43), strings.Repeat("A", 500)} {
		if err := i.Accetta(ctx, tok, "una password lunga"); !errors.Is(err, ErrInvitoNonValido) {
			t.Errorf("token %q: %v", tok[:min(len(tok), 10)], err)
		}
		if _, ok, err := i.Destinatario(ctx, tok); ok || err != nil {
			t.Errorf("destinatario di %q: %v %v", tok[:min(len(tok), 10)], ok, err)
		}
	}
	// Scaduto: al quindicesimo minuto esatto non vale più (expires_at > now).
	o.Sposta(15 * time.Minute)
	if _, ok, _ := i.Destinatario(ctx, inv.Token); ok {
		t.Error("destinatario di un link scaduto")
	}
	if err := i.Accetta(ctx, inv.Token, "una password lunga"); !errors.Is(err, ErrInvitoNonValido) {
		t.Errorf("scaduto: %v", err)
	}
	o.Sposta(-time.Microsecond)
	if _, ok, _ := i.Destinatario(ctx, inv.Token); !ok {
		t.Error("un microsecondo prima della scadenza il link non vale")
	}
	// Account disattivato tra l'invito e la visita.
	u := NuoviUtenti(d, o)
	if err := u.Disattiva(ctx, inv.UtenteID, chi); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := i.Destinatario(ctx, inv.Token); ok {
		t.Error("destinatario di un account disattivato")
	}
	if err := i.Accetta(ctx, inv.Token, "una password lunga"); !errors.Is(err, ErrInvitoNonValido) {
		t.Errorf("disattivato: %v", err)
	}
	var hash any
	_ = d.Lettura.QueryRow(`SELECT hash_password FROM utenti WHERE id = ?`, inv.UtenteID).Scan(&hash)
	if rr := righeInvito(t, d); hash != nil || rr[0].accettato != nil {
		t.Errorf("rifiuto che ha scritto: %v %+v", hash, rr[0])
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'user.invitation_accepted'`); n != 0 {
		t.Errorf("righe di accettazione: %d", n)
	}
}

// inventario: ACC-08
// Il nuovo link: annulla quelli aperti dell'account (e solo i suoi), vale per l'account chiesto e
// per nessun altro, 15 minuti, riga user.invitation_reissued. Vale anche per un account senza
// password e senza inviti (gli utenti importati senza password). Rifiuta un account che ha già una
// password (ErrGiaConPassword) e uno che non c'è o è fuori (ErrNonTrovato), senza scrivere niente
// e senza annullare il link aperto.
func TestNuovoLink(t *testing.T) {
	i, _, d, o := invitiProva(t)
	uno, err := i.Invita(ctx, "uno@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	due, err := i.Invita(ctx, "due@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	o.Sposta(5 * time.Minute)
	nuovo, err := i.NuovoLink(ctx, uno.UtenteID, chi)
	if err != nil {
		t.Fatal(err)
	}
	if nuovo.Token == uno.Token || nuovo.UtenteID != uno.UtenteID || nuovo.Email != "uno@esempio.it" ||
		!nuovo.Scade.Equal(o.Adesso().Add(15*time.Minute)) {
		t.Errorf("nuovo link: %+v", nuovo)
	}
	if got := ultimoEvento(t, d); got != "titolare@esempio.it|-|user.invitation_reissued|user|"+itoa(uno.UtenteID)+"|ok|{}" {
		t.Errorf("registro: %q", got)
	}
	rr := righeInvito(t, d)
	if len(rr) != 3 || rr[0].revocato != "2026-10-30T09:20:07.000000Z" || rr[1].revocato != nil || rr[2].revocato != nil ||
		rr[2].scade != "2026-10-30T09:35:07.000000Z" {
		t.Errorf("righe: %+v", rr)
	}
	if _, ok, _ := i.Destinatario(ctx, uno.Token); ok {
		t.Error("il link revocato mostra ancora il destinatario")
	}
	if err := i.Accetta(ctx, uno.Token, "una password lunga"); !errors.Is(err, ErrInvitoNonValido) {
		t.Errorf("il primo link vale ancora: %v", err)
	}
	if err := i.Accetta(ctx, due.Token, "una password lunga"); err != nil {
		t.Errorf("il link dell'altro account è caduto: %v", err)
	}
	if err := i.Accetta(ctx, nuovo.Token, "una password lunga"); err != nil {
		t.Errorf("il nuovo link non vale: %v", err)
	}
	// L'account importato senza password e senza inviti.
	if inv, err := i.NuovoLink(ctx, 2, chi); err != nil || inv.Email != "invitato@esempio.it" {
		t.Errorf("account senza inviti: %+v %v", inv, err)
	}
	prima := conta(t, d, `SELECT count(*) FROM inviti_utenti`)
	eventiPrima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	aperto, err := i.Invita(ctx, "aperto@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	prima, eventiPrima = prima+1, eventiPrima+1
	for id, atteso := range map[int64]error{1: ErrGiaConPassword, uno.UtenteID: ErrGiaConPassword, 3: ErrNonTrovato, 4242: ErrNonTrovato} {
		if _, err := i.NuovoLink(ctx, id, chi); !errors.Is(err, atteso) {
			t.Errorf("account %d: %v, atteso %v", id, err, atteso)
		}
	}
	if conta(t, d, `SELECT count(*) FROM inviti_utenti`) != prima || conta(t, d, `SELECT count(*) FROM eventi_audit`) != eventiPrima ||
		conta(t, d, `SELECT count(*) FROM inviti_utenti WHERE revocato_il IS NOT NULL`) != 1 {
		t.Error("un rifiuto ha scritto o annullato")
	}
	if _, ok, _ := i.Destinatario(ctx, aperto.Token); !ok {
		t.Error("un rifiuto ha annullato il link aperto di un altro")
	}
}

// inventario: ACC-08
// Un account disattivato dopo l'invito non riceve un nuovo link; quello aperto resta morto.
func TestNuovoLinkAccountUscito(t *testing.T) {
	i, u, _, _ := invitiProva(t)
	inv, err := i.Invita(ctx, "atteso@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Disattiva(ctx, inv.UtenteID, chi); err != nil {
		t.Fatal(err)
	}
	if _, err := i.NuovoLink(ctx, inv.UtenteID, chi); !errors.Is(err, ErrNonTrovato) {
		t.Errorf("account uscito: %v", err)
	}
}

// inventario: ACC-09
// Due accettazioni simultanee dello stesso link: una sola riesce, una sola riga nel registro.
func TestAccettaSenzaCorse(t *testing.T) {
	i, _, d, _ := invitiProva(t)
	inv, err := i.Invita(ctx, "ospite@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	riuscite := 0
	for n := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i.Accetta(ctx, inv.Token, "una password lunga "+strconv.Itoa(n)) == nil {
				mu.Lock()
				riuscite++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if riuscite != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'user.invitation_accepted'`) != 1 {
		t.Errorf("riuscite %d", riuscite)
	}
}

// inventario: ACC-07, ACC-09
// Se la riga del registro non si scrive, non resta niente: né l'account invitato né il link
// speso, né il link vecchio annullato (un trigger fa fallire la scrittura dell'audit).
func TestInvitiAuditNellaTransazione(t *testing.T) {
	i, _, d, _ := invitiProva(t)
	inv, err := i.Invita(ctx, "ospite@esempio.it", "", "dipendente", chi)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER blocca BEFORE INSERT ON eventi_audit BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := i.Invita(ctx, "altro@esempio.it", "", "dipendente", chi); err == nil {
		t.Error("invito riuscito senza audit")
	}
	if _, err := i.NuovoLink(ctx, inv.UtenteID, chi); err == nil {
		t.Error("nuovo link riuscito senza audit")
	}
	if err := i.Accetta(ctx, inv.Token, "una password lunga"); err == nil {
		t.Error("accettazione riuscita senza audit")
	}
	if conta(t, d, `SELECT count(*) FROM utenti WHERE email = 'altro@esempio.it'`) != 0 ||
		conta(t, d, `SELECT count(*) FROM inviti_utenti WHERE accettato_il IS NOT NULL OR revocato_il IS NOT NULL`) != 0 ||
		conta(t, d, `SELECT count(*) FROM utenti WHERE email = 'ospite@esempio.it' AND hash_password IS NOT NULL`) != 0 {
		t.Error("è rimasto qualcosa senza la riga del registro")
	}
}

// inventario: ACC-07, ACC-08
// AccountAttivo è la ricerca del comando locale: l'account attivo con quell'email (normalizzata),
// se ha già la password.
func TestAccountAttivo(t *testing.T) {
	i, _, _, _ := invitiProva(t)
	for email, atteso := range map[string]struct {
		id      int64
		attesa  bool
		trovato bool
	}{
		" Titolare@Esempio.it": {1, false, true},
		"invitato@esempio.it":  {2, true, true},
		"uscito@esempio.it":    {0, false, false},
		"nessuno@esempio.it":   {0, false, false},
	} {
		id, attesa, trovato, err := i.AccountAttivo(ctx, email)
		if err != nil || id != atteso.id || attesa != atteso.attesa || trovato != atteso.trovato {
			t.Errorf("%s: %d %v %v %v", email, id, attesa, trovato, err)
		}
	}
}

// orologioConColpo è l'orologio finto che, alla chiamata n, fa succedere qualcosa: in Accetta il
// pre-controllo legge l'ora una volta e la scrittura una seconda, così il colpo cade tra le due.
type orologioConColpo struct {
	*clock.Finto
	mu    sync.Mutex
	n     int
	quale int
	colpo func()
}

func (o *orologioConColpo) Adesso() time.Time {
	o.mu.Lock()
	o.n++
	scatta := o.n == o.quale
	o.mu.Unlock()
	if scatta {
		o.colpo()
	}
	return o.Finto.Adesso()
}

// inventario: ACC-09
// Le condizioni stanno anche dentro la scrittura, non solo nel pre-controllo: un link che scade, è
// revocato da un «Nuovo link» o il cui account è disattivato tra il pre-controllo e la transazione
// non si spende e non scrive la password.
func TestAccettaGuardieNellaScrittura(t *testing.T) {
	for nome, colpo := range map[string]func(d *db.DB, o *clock.Finto, id int64){
		"scaduto": func(_ *db.DB, o *clock.Finto, _ int64) { o.Sposta(15 * time.Minute) },
		"revocato": func(d *db.DB, _ *clock.Finto, _ int64) {
			_, _ = d.Scrittura.Exec(`UPDATE inviti_utenti SET revocato_il = '2026-10-30T09:15:08.000000Z'`)
		},
		"disattivato": func(d *db.DB, _ *clock.Finto, id int64) {
			_, _ = d.Scrittura.Exec(`UPDATE utenti SET attivo = 0 WHERE id = ?`, id)
		},
	} {
		t.Run(nome, func(t *testing.T) {
			_, _, d, f := invitiProva(t)
			inv, err := NuoviInviti(d, f).Invita(ctx, "ospite@esempio.it", "", "dipendente", chi)
			if err != nil {
				t.Fatal(err)
			}
			o := &orologioConColpo{Finto: f, quale: 2}
			o.colpo = func() { colpo(d, f, inv.UtenteID) }
			if err := NuoviInviti(d, o).Accetta(ctx, inv.Token, "una password lunga"); !errors.Is(err, ErrInvitoNonValido) {
				t.Errorf("accettato: %v", err)
			}
			if o.n < 2 {
				t.Fatalf("il colpo non è caduto (%d letture dell'ora)", o.n)
			}
			if conta(t, d, `SELECT count(*) FROM utenti WHERE hash_password IS NOT NULL AND id = `+itoa(inv.UtenteID)) != 0 ||
				conta(t, d, `SELECT count(*) FROM inviti_utenti WHERE accettato_il IS NOT NULL`) != 0 {
				t.Error("la scrittura ha speso il link o scritto la password")
			}
		})
	}
}
