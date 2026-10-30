package accounts

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il recupero della password del titolare (ACC-13, ACC-14, ACC-20), come PasswordRecovery dello
// standalone; senza canali (Telegram ed email arrivano con la T4.6 e la T4.7).

func recuperoProva(t *testing.T) (*Recupero, *Utenti, *db.DB, *clock.Finto) {
	t.Helper()
	u, d := utentiProva(t)
	o := orologio()
	u.o = o
	return NuovoRecupero(d, o), u, d, o
}

func ultimoEvento(t *testing.T, d *db.DB) string {
	t.Helper()
	var a, b, c, e, f, g string
	var ip any
	if err := d.Lettura.QueryRow(`SELECT attore, coalesce(ip, '-'), azione, risorsa, id_risorsa, esito, dati FROM eventi_audit ORDER BY id DESC LIMIT 1`).
		Scan(&a, &ip, &b, &c, &e, &f, &g); err != nil {
		return ""
	}
	return strings.Join([]string{a, ip.(string), b, c, e, f, g}, "|")
}

func contaEventi(t *testing.T, d *db.DB, esito string) int {
	t.Helper()
	var n int
	_ = d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE azione = 'password_reset.requested' AND esito = ?`, esito).Scan(&n)
	return n
}

// inventario: ACC-13
// Senza canali la richiesta trova «nessun canale», come lo standalone: il link creato si annulla
// subito e il registro dice perché, senza il token.
func TestRichiestaSenzaCanali(t *testing.T) {
	r, _, d, _ := recuperoProva(t)
	esito, err := r.RichiestaDallaPagina(ctx, "203.0.113.5")
	if err != nil || esito != "no_channel" {
		t.Fatalf("esito %q %v", esito, err)
	}
	var aperti int
	_ = d.Lettura.QueryRow(`SELECT count(*) FROM reset_password WHERE revocato_il IS NULL`).Scan(&aperti)
	if aperti != 0 {
		t.Error("il link senza canale è rimasto aperto")
	}
	if got := ultimoEvento(t, d); got != `public|203.0.113.5|password_reset.requested|user|1|failed|{"channel":"none","email":"not_configured","telegram":"not_configured"}` {
		t.Errorf("registro: %q", got)
	}
}

// inventario: ACC-20, ACC-13
// Tre richieste l'ora per indirizzo e cinque al giorno in tutto, contate sulle righe (anche quelle
// che non hanno raggiunto nessuno); il rifiuto per limite si scrive una volta l'ora per indirizzo.
// I link della console non contano.
func TestLimitiDelReset(t *testing.T) {
	r, _, d, o := recuperoProva(t)
	for range 4 {
		if _, _, _, err := r.LinkDallaConsole(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 3; i++ {
		if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.6"); e != "no_channel" {
			t.Fatalf("richiesta %d: %q", i, e)
		}
	}
	for range 3 {
		if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.6"); e != "rate_limited" {
			t.Fatalf("quarta richiesta: %q", e)
		}
	}
	if n := contaEventi(t, d, "rate_limited"); n != 1 {
		t.Errorf("righe del limite: %d, attesa 1 l'ora", n)
	}
	if got := ultimoEvento(t, d); got != `public|203.0.113.6|password_reset.requested|installation|password|rate_limited|{"limit":"ip"}` {
		t.Errorf("registro del limite: %q", got)
	}
	var righe int
	_ = d.Lettura.QueryRow(`SELECT count(*) FROM reset_password WHERE origine = 'pagina'`).Scan(&righe)
	if righe != 3 {
		t.Errorf("righe della pagina: %d", righe)
	}
	// Altri due indirizzi arrivano a cinque in tutto; il sesto è il limite del giorno.
	for _, ip := range []string{"203.0.113.7", "203.0.113.7"} {
		if e, _ := r.RichiestaDallaPagina(ctx, ip); e != "no_channel" {
			t.Fatalf("%s: %q", ip, e)
		}
	}
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.8"); e != "rate_limited" {
		t.Fatalf("sesta del giorno: %q", e)
	}
	if got := ultimoEvento(t, d); !strings.HasSuffix(got, `|rate_limited|{"limit":"global"}`) {
		t.Errorf("registro del limite del giorno: %q", got)
	}
	// Dopo un'ora l'indirizzo può di nuovo, se il giorno lo permette: no, il giorno è pieno.
	o.Sposta(time.Hour + time.Second)
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.9"); e != "rate_limited" {
		t.Errorf("il limite del giorno dura 24 ore: %q", e)
	}
	o.Sposta(23 * time.Hour)
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.6"); e != "no_channel" {
		t.Errorf("dopo un giorno: %q", e)
	}
	// Una riga per indirizzo: .6 (l'ora), .8 e .9 (il giorno).
	if n := contaEventi(t, d, "rate_limited"); n != 3 {
		t.Errorf("righe del limite: %d", n)
	}
}

// inventario: ACC-13
// Senza un titolare attivo con password: «no_account» nel registro, nessuna riga di reset.
func TestRichiestaSenzaTitolare(t *testing.T) {
	r, _, d, _ := recuperoProva(t)
	if _, err := d.Scrittura.Exec(`UPDATE utenti SET hash_password = NULL WHERE ruolo = 'titolare'`); err != nil {
		t.Fatal(err)
	}
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.10"); e != "no_account" {
		t.Errorf("esito %q", e)
	}
	if got := ultimoEvento(t, d); got != "public|203.0.113.10|password_reset.requested|installation|password|no_account|{}" {
		t.Errorf("registro: %q", got)
	}
	if _, _, _, err := r.LinkDallaConsole(ctx); !errors.Is(err, ErrNessunTitolare) {
		t.Errorf("console senza titolare: %v", err)
	}
}

// inventario: ACC-13
// D-04: il comando locale dà un link del titolare (il più vecchio attivo con password), valido 30
// minuti, monouso; un link nuovo annulla quelli aperti; riga nel registro senza il token.
func TestLinkDallaConsole(t *testing.T) {
	r, _, d, o := recuperoProva(t)
	vecchio, _, _, err := r.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok, email, scade, err := r.LinkDallaConsole(ctx)
	if err != nil || email != "titolare@esempio.it" || len(tok) != 43 || tok == vecchio || !scade.Equal(o.Adesso().Add(30*time.Minute)) {
		t.Fatalf("link %q %v %v", tok, scade, err)
	}
	if got := ultimoEvento(t, d); got != `console|-|password_reset.requested|user|1|ok|{"channel":"console"}` {
		t.Errorf("registro: %q", got)
	}
	var tutti string
	_ = d.Lettura.QueryRow(`SELECT group_concat(dati) FROM eventi_audit`).Scan(&tutti)
	if strings.Contains(tutti, tok) {
		t.Error("il token è nel registro")
	}
	if _, ok, _ := r.AccountDel(ctx, vecchio); ok {
		t.Error("il link vecchio vale ancora")
	}
	o.Sposta(30*time.Minute - time.Second)
	if email, ok, _ := r.AccountDel(ctx, tok); !ok || email != "titolare@esempio.it" {
		t.Errorf("a 29:59: %q %v", email, ok)
	}
	o.Sposta(time.Second)
	if _, ok, _ := r.AccountDel(ctx, tok); ok {
		t.Error("a 30 minuti vale ancora")
	}
	// Anche una richiesta dalla pagina annulla il link aperto della console.
	o.Sposta(-30 * time.Minute)
	tok, _, _, _ = r.LinkDallaConsole(ctx)
	_, _ = r.RichiestaDallaPagina(ctx, "203.0.113.11")
	if _, ok, _ := r.AccountDel(ctx, tok); ok {
		t.Error("la richiesta della pagina non ha annullato il link della console")
	}
	for _, x := range []string{"", strings.Repeat("a", 65), "inventato"} {
		if _, ok, _ := r.AccountDel(ctx, x); ok {
			t.Errorf("token %q accettato", x)
		}
	}
}

// inventario: ACC-14
// Il reset: password diverse e troppo corte prima di tutto; un link morto è «non valido» e non
// costa una derivazione; riuscito, la password nuova vale, tutte le sessioni dell'account sono
// chiuse, il link è speso, gli altri link aperti dell'account annullati, una riga nel registro.
func TestReimposta(t *testing.T) {
	r, u, d, _ := recuperoProva(t)
	tok, _, _, _ := r.LinkDallaConsole(ctx)
	if _, err := d.Scrittura.Exec(`INSERT INTO reset_password (utente_id, hash_token, scade_il, origine, creato_il, aggiornato_il) VALUES (1, randomblob(32), ?, 'console', ?, ?)`,
		"2026-10-30T10:00:00.000000Z", istante, istante); err != nil {
		t.Fatal(err)
	}
	nuova := "una-password-nuova-lunga"
	for _, c := range []struct {
		tok, pw, conferma string
		err               error
	}{
		{tok, nuova, nuova + "x", ErrPasswordDiverse},
		{tok, "corta", "corta", ErrPasswordCorta},
		{"inventato", nuova, nuova, ErrLinkNonValido},
	} {
		if err := r.Reimposta(ctx, c.tok, c.pw, c.conferma, "203.0.113.12"); !errors.Is(err, c.err) {
			t.Errorf("%q %q: %v, atteso %v", c.pw, c.conferma, err, c.err)
		}
	}
	if err := r.Reimposta(ctx, tok, nuova, nuova, "203.0.113.12"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := u.Autentica(ctx, "titolare@esempio.it", nuova); !ok {
		t.Error("la password nuova non vale")
	}
	if _, ok, _ := u.DiSessione(ctx, 1, 0); ok {
		t.Error("le sessioni di prima valgono ancora")
	}
	if err := r.Reimposta(ctx, tok, nuova, nuova, "203.0.113.12"); !errors.Is(err, ErrLinkNonValido) {
		t.Errorf("seconda volta: %v", err)
	}
	var aperti int
	_ = d.Lettura.QueryRow(`SELECT count(*) FROM reset_password WHERE usato_il IS NULL AND revocato_il IS NULL`).Scan(&aperti)
	if aperti != 0 {
		t.Errorf("link ancora aperti: %d", aperti)
	}
	if got := ultimoEvento(t, d); got != "titolare@esempio.it|203.0.113.12|password_reset.completed|user|1|ok|{}" {
		t.Errorf("registro: %q", got)
	}
}

// inventario: ACC-14
// Un link di un account che non è più titolare attivo vale come morto, e non cambia niente.
func TestReimpostaAccountNonPiuTitolare(t *testing.T) {
	r, u, d, _ := recuperoProva(t)
	tok, _, _, _ := r.LinkDallaConsole(ctx)
	aggiungi(t, d, "socio@esempio.it", "titolare", vettoriStandalone[0].hash, 1)
	if err := u.CambiaRuolo(ctx, 1, Dipendente, chi); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.AccountDel(ctx, tok); ok {
		t.Error("link di un dipendente accettato")
	}
	if err := r.Reimposta(ctx, tok, "una-password-nuova-lunga", "una-password-nuova-lunga", "x"); !errors.Is(err, ErrLinkNonValido) {
		t.Errorf("reset: %v", err)
	}
	if _, ok, _ := u.Autentica(ctx, "titolare@esempio.it", vettoriStandalone[0].password); !ok {
		t.Error("la password è cambiata")
	}
}

// inventario: ACC-13
// Manca l'invio del link via Telegram e poi email (deliver/3), con la riga «ok» e il canale:
// arriva con i canali (T4.6, T4.7). Finché è saltato, la voce resta parziale.
func TestInvioDelLinkSuiCanali(t *testing.T) {
	t.Skip("canali Telegram ed email non ancora nel Go")
}

// inventario: ACC-14
// Manca l'avviso «password cambiata» sul canale da cui è partito il link (notify_changed/2).
func TestAvvisoPasswordCambiata(t *testing.T) {
	t.Skip("canali Telegram ed email non ancora nel Go")
}

// inventario: ACC-13
// Il titolare è il più vecchio attivo con password (titolare/0): con due titolari il link è del
// primo, anche se l'altro è più recente.
func TestIlTitolareEIlPiuVecchio(t *testing.T) {
	r, _, d, _ := recuperoProva(t)
	aggiungi(t, d, "socio@esempio.it", "titolare", vettoriStandalone[0].hash, 1)
	tok, _, _, err := r.LinkDallaConsole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if email, ok, _ := r.AccountDel(ctx, tok); !ok || email != "titolare@esempio.it" {
		t.Errorf("link per %q", email)
	}
}

// inventario: ACC-20
// La finestra per indirizzo è di un'ora: passata, lo stesso indirizzo può di nuovo (se il giorno
// non è pieno); il rifiuto per limite si riscrive dopo un'ora.
func TestFinestraDellIndirizzo(t *testing.T) {
	r, _, d, o := recuperoProva(t)
	for range 3 {
		_, _ = r.RichiestaDallaPagina(ctx, "203.0.113.13")
	}
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.13"); e != "rate_limited" {
		t.Fatalf("quarta: %q", e)
	}
	o.Sposta(59 * time.Minute)
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.13"); e != "rate_limited" {
		t.Fatalf("a 59 minuti: %q", e)
	}
	if n := contaEventi(t, d, "rate_limited"); n != 1 {
		t.Errorf("righe del limite nella stessa ora: %d", n)
	}
	o.Sposta(time.Minute + time.Second)
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.13"); e != "no_channel" {
		t.Fatalf("dopo un'ora: %q", e)
	}
	// Ora le righe del giorno sono cinque: tre di prima e due di questa ora.
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.13"); e != "no_channel" {
		t.Fatalf("seconda dell'ora nuova: %q", e)
	}
	o.Sposta(time.Second)
	if e, _ := r.RichiestaDallaPagina(ctx, "203.0.113.14"); e != "rate_limited" {
		t.Fatalf("sesta del giorno: %q", e)
	}
	if n := contaEventi(t, d, "rate_limited"); n != 2 {
		t.Errorf("righe del limite: %d, attese 2 (indirizzi diversi)", n)
	}
	o.Sposta(time.Hour + time.Second)
	_, _ = r.RichiestaDallaPagina(ctx, "203.0.113.13")
	if n := contaEventi(t, d, "rate_limited"); n != 3 {
		t.Errorf("dopo un'ora la riga del limite non si riscrive: %d", n)
	}
}

// inventario: ACC-14
// Il link di un titolare disattivato non vale più, e il reset non tocca l'account spento.
func TestReimpostaTitolareDisattivato(t *testing.T) {
	r, u, d, _ := recuperoProva(t)
	tok, _, _, _ := r.LinkDallaConsole(ctx)
	aggiungi(t, d, "socio@esempio.it", "titolare", vettoriStandalone[0].hash, 1)
	if err := u.Disattiva(ctx, 1, chi); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.AccountDel(ctx, tok); ok {
		t.Error("link di un titolare disattivato accettato")
	}
	if err := r.Reimposta(ctx, tok, "una-password-nuova-lunga", "una-password-nuova-lunga", "x"); !errors.Is(err, ErrLinkNonValido) {
		t.Errorf("reset: %v", err)
	}
	var hash string
	_ = d.Lettura.QueryRow(`SELECT hash_password FROM utenti WHERE id = 1`).Scan(&hash)
	if hash != vettoriStandalone[0].hash {
		t.Error("la password dell'account spento è cambiata")
	}
}
