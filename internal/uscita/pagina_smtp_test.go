package uscita

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// La pagina «Provider di invio», parte email (slice 069b, ADM-09, CLI-52 parte email), come
// Outgoing.Config.put/3 e Smtp.changeset/3 dello standalone a go-baseline (config_test.exs,
// providers_live_test.exs), più l'accensione del canale (D-03) con la revisione delle email rimaste in
// coda (D-27), solo del Go. Nessun server vero: il server SMTP finto della 069. Dati inventati.

// moduloValido è il modulo come lo manda il browser, con tutti i campi.
func moduloValido() ModuloSMTP {
	return ModuloSMTP{"host": "smtp.example.invalid", "port": "587", "security": "starttls", "username": "negozio-prova",
		"password": parolaDiProva, "from_address": "negozio@example.invalid", "from_name": "Negozio di Prova"}
}

func con(m ModuloSMTP, kv ...string) ModuloSMTP {
	n := ModuloSMTP{}
	for k, v := range m {
		n[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		n[kv[i]] = kv[i+1]
	}
	return n
}

func valoreImpostazione(t *testing.T, c *Coda, chiave string) string {
	t.Helper()
	var v string
	err := c.d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, chiave).Scan(&v)
	if err != nil {
		return "(assente)"
	}
	return v
}

func passwordSalvata(t *testing.T, c *Coda) string {
	t.Helper()
	var v string
	if err := c.d.Lettura.QueryRow(`SELECT valore FROM segreti_provider WHERE nome = 'outgoing.smtp.password'`).Scan(&v); err != nil {
		return "(assente)"
	}
	return v
}

func ultimoAudit(t *testing.T, c *Coda, azione string) (id, dati string) {
	t.Helper()
	if err := c.d.Lettura.QueryRow(`SELECT id_risorsa, dati FROM eventi_audit WHERE azione = ? ORDER BY id DESC LIMIT 1`, azione).Scan(&id, &dati); err != nil {
		t.Fatalf("%s: %v", azione, err)
	}
	return id, dati
}

func codaSMTP(t *testing.T) (*Coda, *SMTP) {
	t.Helper()
	d, o := apriDB(t)
	p := NuovoSMTP(d, o)
	p.tempo = 3 * time.Second
	return Nuova(d, o, map[string]Provider{CanaleEmail: p}), p
}

// inventario: ADM-09, CLI-52
// Config.put/3 con un modulo valido: le sei chiavi outgoing.smtp.* in impostazioni (la porta come intero
// riscritto), la password in segreti_provider e mai tra le impostazioni, e outgoing.provider_configured
// con il canale, i soli nomi delle chiavi cambiate nell'ordine del modulo e se è configurato: mai un
// valore. Lo stesso modulo una seconda volta non cambia niente (changed vuoto).
func TestSalvaSMTP(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	rif, err := c.SalvaSMTP(ctx, con(moduloValido(), "port", "0025"), contesto)
	if err != nil || rif != nil {
		t.Fatalf("salva: %v %+v", err, rif)
	}
	for k, v := range map[string]string{"outgoing.smtp.host": "smtp.example.invalid", "outgoing.smtp.port": "25",
		"outgoing.smtp.security": "starttls", "outgoing.smtp.username": "negozio-prova",
		"outgoing.smtp.from_address": "negozio@example.invalid", "outgoing.smtp.from_name": "Negozio di Prova",
		"outgoing.smtp.password": "(assente)"} {
		if got := valoreImpostazione(t, c, k); got != v {
			t.Errorf("%s = %q, atteso %q", k, got, v)
		}
	}
	if got := passwordSalvata(t, c); got != parolaDiProva {
		t.Errorf("password %q", got)
	}
	id, dati := ultimoAudit(t, c, "outgoing.provider_configured")
	// «security» non è fra le cambiate: get/1 dà già «starttls» senza la chiave.
	if id != "email" || dati != `{"changed":["host","port","username","password","from_address","from_name"],"channel":"email","configured":true}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if _, err := c.SalvaSMTP(ctx, con(moduloValido(), "port", "25", "password", ""), contesto); err != nil {
		t.Fatal(err)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":[],"channel":"email","configured":true}` {
		t.Errorf("secondo salvataggio %s", dati)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE ?`, "%"+parolaDiProva+"%"); n != 0 {
		t.Error("la password è nell'audit")
	}
}

// inventario: ADM-09, CLI-52
// Le chiavi che get/1 riempie da sé (porta 587, sicurezza starttls) non sono «cambiate» se il modulo
// manda lo stesso valore su un database vuoto; una casella vuota (o di soli spazi) è nil: si salva "" e
// si rilegge come assente (la porta torna 587).
func TestSalvaSMTPPredefinitiECaselleVuote(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	m := ModuloSMTP{"host": "smtp.example.invalid", "port": "587", "security": "starttls", "username": "  ", "password": "",
		"from_address": "negozio@example.invalid", "from_name": ""}
	if rif, err := c.SalvaSMTP(ctx, m, contesto); err != nil || rif != nil {
		t.Fatalf("%v %+v", err, rif)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["host","from_address"],"channel":"email","configured":true}` {
		t.Errorf("audit %s", dati)
	}
	if got := valoreImpostazione(t, c, "outgoing.smtp.username"); got != "" {
		t.Errorf("utente %q", got)
	}
	if got := passwordSalvata(t, c); got != "(assente)" {
		t.Errorf("password %q", got)
	}
	if _, err := c.SalvaSMTP(ctx, con(m, "port", " "), contesto); err != nil {
		t.Fatal(err)
	}
	if got := valoreImpostazione(t, c, "outgoing.smtp.port"); got != "" {
		t.Errorf("porta vuota salvata come %q", got)
	}
	cfg, err := LeggiSMTP(ctx, c.d.Lettura)
	if err != nil || cfg.Porta != 587 {
		t.Errorf("porta riletta %d %v", cfg.Porta, err)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["port"],"channel":"email","configured":true}` {
		t.Errorf("porta tolta: %s", dati)
	}
}

// inventario: ADM-09, CLI-52
// Le regole di Smtp.changeset/3, con i testi del catalogo it dello standalone e i suoi due testi scritti a
// mano; un modulo rifiutato non scrive niente (né impostazioni, né segreto, né audit) e ridà i valori
// mandati, mai la password. Un campo che manca nel modulo resta quello salvato (cast/3).
func TestSalvaSMTPRifiuti(t *testing.T) {
	lungo := func(n int) string { return strings.Repeat("a", n) }
	casi := []struct {
		nome   string
		modulo ModuloSMTP
		campo  string
		errori []string
	}{
		{"porta oltre", con(moduloValido(), "port", "70000"), "port", []string{"Al massimo 65535."}},
		{"porta zero", con(moduloValido(), "port", "0"), "port", []string{"Deve essere maggiore di 0."}},
		{"porta negativa", con(moduloValido(), "port", "-5"), "port", []string{"Deve essere maggiore di 0."}},
		{"porta non intera", con(moduloValido(), "port", "25a"), "port", []string{"Valore non valido."}},
		{"porta con spazi", con(moduloValido(), "port", " 25"), "port", []string{"Valore non valido."}},
		{"porta enorme", con(moduloValido(), "port", "99999999999999999999999"), "port", []string{"Al massimo 65535."}},
		{"sicurezza", con(moduloValido(), "security", "STARTTLS"), "security", []string{"Valore non valido."}},
		{"mittente senza chiocciola", con(moduloValido(), "from_address", "negozio.example.invalid"), "from_address", []string{"Formato non valido."}},
		{"mittente con spazio", con(moduloValido(), "from_address", "neg ozio@example.invalid"), "from_address", []string{"Formato non valido."}},
		{"mittente dominio corto", con(moduloValido(), "from_address", "negozio@example.x"), "from_address", []string{"Formato non valido."}},
		{"mittente due chiocciole", con(moduloValido(), "from_address", "a@b@example.invalid"), "from_address", []string{"Formato non valido."}},
		{"nome con a capo", con(moduloValido(), "from_name", "Negozio\r\nBcc: x@example.invalid"), "from_name", []string{"Formato non valido."}},
		{"server lungo", con(moduloValido(), "host", lungo(256)), "host", []string{"Troppo lungo (massimo 255 caratteri)."}},
		{"utente lungo", con(moduloValido(), "username", lungo(256)), "username", []string{"Troppo lungo (massimo 255 caratteri)."}},
		{"password lunga", con(moduloValido(), "password", lungo(256)), "password", []string{"Troppo lungo (massimo 255 caratteri)."}},
		{"mittente lungo", con(moduloValido(), "from_address", lungo(243)+"@example.com"), "from_address", []string{"Troppo lungo (massimo 254 caratteri)."}},
		{"nome lungo", con(moduloValido(), "from_name", strings.Repeat("è", 201)), "from_name", []string{"Troppo lungo (massimo 200 caratteri)."}},
		{"utente senza password", con(moduloValido(), "password", ""), "password", []string{"serve una password per questo utente"}},
		{"password in chiaro", con(moduloValido(), "security", "none"), "password", []string{"le credenziali non viaggiano su una connessione in chiaro"}},
		// Gli errori di un campo, il più recente prima (come changeset.errors).
		{"password lunga in chiaro", con(moduloValido(), "security", "none", "password", lungo(256)), "password",
			[]string{"le credenziali non viaggiano su una connessione in chiaro", "Troppo lungo (massimo 255 caratteri)."}},
	}
	for _, k := range casi {
		t.Run(k.nome, func(t *testing.T) {
			c, _ := codaSMTP(t)
			rif, err := c.SalvaSMTP(context.Background(), k.modulo, contesto)
			if err != nil {
				t.Fatal(err)
			}
			if rif == nil {
				t.Fatal("salvato")
			}
			if !slices.Equal(rif.Errori[k.campo], k.errori) {
				t.Errorf("errori di %s: %q (tutti: %q)", k.campo, rif.Errori[k.campo], rif.Errori)
			}
			for campo, e := range rif.Errori {
				if campo != k.campo {
					t.Errorf("errore inatteso su %s: %q", campo, e)
				}
			}
			if rif.Valori["password"] != "" {
				t.Error("la password torna nel modulo")
			}
			for _, campo := range []string{"host", "port", "security", "username", "from_address", "from_name"} {
				if rif.Valori[campo] != k.modulo[campo] {
					t.Errorf("valore di %s: %q", campo, rif.Valori[campo])
				}
			}
			if n := conta(t, c.d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'outgoing.%') + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM eventi_audit)`); n != 0 {
				t.Errorf("scritto qualcosa: %d", n)
			}
		})
	}
}

// inventario: ADM-09, CLI-52
// Le validazioni guardano solo i campi cambiati (validate_*/3 sulle changes): un mittente importato che
// non passerebbe il formato si risalva com'è; cambiato, no. Un campo che il modulo non manda resta
// quello salvato. La regola delle credenziali guarda sempre i valori finali (get_field/2).
func TestSalvaSMTPSoloICambiati(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	scriviConfigSMTP(t, c.d, map[string]string{"outgoing.smtp.host": "smtp.example.invalid", "outgoing.smtp.from_address": "vecchio-mittente"}, "")
	if rif, err := c.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "from_address": "vecchio-mittente", "from_name": "Nuovo"}, contesto); err != nil || rif != nil {
		t.Fatalf("mittente salvato invariato: %v %+v", err, rif)
	}
	if got := valoreImpostazione(t, c, "outgoing.smtp.from_name"); got != "Nuovo" {
		t.Errorf("nome %q", got)
	}
	rif, err := c.SalvaSMTP(ctx, ModuloSMTP{"from_address": "altro-mittente"}, contesto)
	if err != nil || rif == nil || !slices.Equal(rif.Errori["from_address"], []string{"Formato non valido."}) {
		t.Fatalf("mittente cambiato: %v %+v", err, rif)
	}
	// Il campo non mandato si mostra con il valore salvato.
	if rif.Valori["host"] != "smtp.example.invalid" || rif.Valori["port"] != "587" || rif.Valori["security"] != "starttls" {
		t.Errorf("valori mostrati %q", rif.Valori)
	}
	// Una password salvata e la sicurezza portata a «none» senza ribatterla: la regola la vede lo stesso.
	scriviConfigSMTP(t, c.d, map[string]string{"outgoing.smtp.from_address": "negozio@example.invalid"}, parolaDiProva)
	rif, err = c.SalvaSMTP(ctx, ModuloSMTP{"security": "none", "password": ""}, contesto)
	if err != nil || rif == nil || !slices.Equal(rif.Errori["password"], []string{"le credenziali non viaggiano su una connessione in chiaro"}) {
		t.Fatalf("password salvata in chiaro: %v %+v", err, rif)
	}
}

// inventario: ADM-09, CLI-52
// La casella della password vuota (o di soli spazi) tiene quella salvata (keep_secret/3); «Cancella le
// credenziali salvate» toglie password e utente, e lascia il resto; senza la casella la password non si
// cancella mai.
func TestSalvaSMTPPasswordVuotaECancella(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	if _, err := c.SalvaSMTP(ctx, moduloValido(), contesto); err != nil {
		t.Fatal(err)
	}
	for _, vuota := range []string{"", "   "} {
		if rif, err := c.SalvaSMTP(ctx, con(moduloValido(), "password", vuota, "username", "altro"), contesto); err != nil || rif != nil {
			t.Fatalf("%v %+v", err, rif)
		}
		if got := passwordSalvata(t, c); got != parolaDiProva {
			t.Errorf("casella %q: password %q", vuota, got)
		}
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":[],"channel":"email","configured":true}` {
		t.Errorf("audit %s", dati)
	}
	if rif, err := c.SalvaSMTP(ctx, con(moduloValido(), "password", "", "clear_secret", "1"), contesto); err != nil || rif != nil {
		t.Fatalf("cancella: %v %+v", err, rif)
	}
	if got := passwordSalvata(t, c); got != "(assente)" {
		t.Errorf("password dopo la cancellazione %q", got)
	}
	if got := valoreImpostazione(t, c, "outgoing.smtp.username"); got != "" {
		t.Errorf("utente dopo la cancellazione %q", got)
	}
	if got := valoreImpostazione(t, c, "outgoing.smtp.host"); got != "smtp.example.invalid" {
		t.Errorf("server dopo la cancellazione %q", got)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["username","password"],"channel":"email","configured":true}` {
		t.Errorf("audit della cancellazione %s", dati)
	}
	// Con la spunta, anche una password scritta va via.
	if _, err := c.SalvaSMTP(ctx, moduloValido(), contesto); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SalvaSMTP(ctx, con(moduloValido(), "clear_secret", "true"), contesto); err != nil {
		t.Fatal(err)
	}
	if got := passwordSalvata(t, c); got != "(assente)" {
		t.Errorf("password con la spunta %q", got)
	}
}

// inventario: ADM-09, CLI-52
// La pagina: valori salvati (la porta 587 e starttls se mancano), se c'è una password (mai il valore),
// «Configurato» (Usabile), il canale acceso o no, i conteggi della coda (email e Telegram, mai WhatsApp)
// e le ultime 20 righe dalla più recente, e le email in coda da guardare (D-27) dalla più vecchia.
func TestPaginaSMTP(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	p, err := c.PaginaSMTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Configurata || p.Accesa || p.PasswordSalvata || p.Valori["port"] != "587" || p.Valori["security"] != "starttls" || p.Valori["host"] != "" {
		t.Errorf("vuota: %+v", p)
	}
	if _, err := c.SalvaSMTP(ctx, moduloValido(), contesto); err != nil {
		t.Fatal(err)
	}
	inserisciRighe(t, c)
	p, err = c.PaginaSMTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Configurata || p.Accesa || !p.PasswordSalvata || p.Valori["host"] != "smtp.example.invalid" || p.Valori["password"] != "" {
		t.Errorf("configurata: %+v", p)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", p, p), parolaDiProva) {
		t.Error("la password è nei dati della pagina")
	}
	if p.InCoda != 3 || p.Accettati != 1 || p.Falliti != 1 {
		t.Errorf("conteggi %d %d %d", p.InCoda, p.Accettati, p.Falliti)
	}
	var ids []int64
	for _, r := range p.Recenti {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []int64{6, 5, 4, 3, 2}) {
		t.Errorf("recenti %v", ids)
	}
	ids = nil
	for _, r := range p.InSospeso {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []int64{2, 5}) {
		t.Errorf("in sospeso %v", ids)
	}
	if r := p.Recenti[2]; r.Stato != "failed" || r.UltimoErrore != "smtp:550" || r.FallitoIl == nil || r.Tentativi != 5 {
		t.Errorf("riga fallita %+v", r)
	}
	if r := p.Recenti[3]; r.Riferimento != "250 ok" || r.AccettatoIl == nil || !r.AccodatoIl.Equal(time.Date(2026, 1, 15, 8, 5, 0, 0, time.UTC)) {
		t.Errorf("riga accettata %+v", r)
	}
	accendiEmail(t, c.d)
	if p, _ = c.PaginaSMTP(ctx); !p.Accesa || p.InSospeso != nil {
		t.Errorf("accesa: %v %v", p.Accesa, p.InSospeso)
	}
}

// inserisciRighe mette nella coda, come le porta l'import: 1 WhatsApp (mai nella pagina), 2 email in coda,
// 3 email accettata, 4 email fallita, 5 email in coda, 6 Telegram in coda.
func inserisciRighe(t *testing.T, c *Coda) {
	t.Helper()
	q := `INSERT INTO messaggi_in_uscita (id, canale, destinatario, indirizzo, oggetto, testo, stato, tentativi, prossimo_tentativo_il, accodato_il,
		accettato_il, fallito_il, riferimento_provider, ultimo_errore) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '2026-01-15T08:05:00.000000Z', '2026-01-15T08:05:00.000000Z', ?, ?, ?, ?)`
	righe := [][]any{
		{1, "whatsapp", "customer", "+390000000001", nil, "Ciao", "accepted", 0, "2026-01-15T08:05:00.000000Z", nil, nil, nil},
		{2, "email", "customer", "cliente-due@example.com", "Conferma", "Conferma: https://negozio.example.invalid/stato/email-conferma?t=SEGNO", "queued", 1, nil, nil, nil, "smtp:451"},
		{3, "email", "shop", "negozio@example.invalid", "Prova", "Prova", "accepted", 1, "2026-07-15T08:05:00.000000Z", nil, "250 ok", nil},
		{4, "email", "customer", "cliente-quattro@example.com", "Pronta", "Pronta", "failed", 5, nil, "2026-07-15T22:30:00.000000Z", nil, "smtp:550"},
		{5, "email", "customer", "cliente-cinque@example.com", "Pronta", "<b>Pronta</b>", "queued", 0, nil, nil, nil, nil},
		{6, "telegram", "customer", "12345", nil, "Pronta", "queued", 0, nil, nil, nil, nil},
	}
	for _, r := range righe {
		if _, err := c.d.Scrittura.Exec(q, r...); err != nil {
			t.Fatal(err)
		}
	}
}

// inventario: OPS-02
// L'accensione dell'email (solo Go): spenta all'inizio; non si accende senza una configurazione usabile
// né con delle email ancora in coda (D-27: prima si guardano, una per una). Scartata una e mandata
// l'altra, si accende: canale_email_acceso = 1 e outgoing.channel_enabled; la seconda volta non scrive
// niente. Spegnere scrive 0 e outgoing.channel_disabled. Con l'email accesa né scarta né manda.
func TestAccensioneEmailConRevisione(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	if err := c.ImpostaEmail(ctx, true, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Fatalf("senza configurazione: %v", err)
	}
	if _, err := c.SalvaSMTP(ctx, moduloValido(), contesto); err != nil {
		t.Fatal(err)
	}
	inserisciRighe(t, c)
	if err := c.ImpostaEmail(ctx, true, contesto); !errors.Is(err, ErrEmailInSospeso) {
		t.Fatalf("con le email in coda: %v", err)
	}
	if acceso, _ := c.CanaleAttivo(ctx, CanaleEmail); acceso {
		t.Fatal("acceso con le email in coda")
	}
	// Una riga che non è un'email in coda non si scarta.
	for _, id := range []int64{1, 3, 4, 6, 99} {
		if _, err := c.ScartaInSospeso(ctx, id, contesto); !errors.Is(err, ErrNonInSospeso) {
			t.Errorf("scarta %d: %v", id, err)
		}
	}
	if _, err := c.ScartaInSospeso(ctx, 2, contesto); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita WHERE id = 2`); n != 0 {
		t.Error("la riga scartata c'è ancora")
	}
	if id, dati := ultimoAudit(t, c, "outgoing.queued_discarded"); id != "2" || dati != `{"channel":"email","party":"customer","repair_id":null}` {
		t.Errorf("audit dello scarto %s %s", id, dati)
	}
	if err := c.ImpostaEmail(ctx, true, contesto); !errors.Is(err, ErrEmailInSospeso) {
		t.Fatalf("con una email in coda: %v", err)
	}
	// La 5 la toglie un altro (qui: chiusa a mano), poi si accende.
	if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'failed', fallito_il = '2026-01-15T08:05:00.000000Z' WHERE id = 5`); err != nil {
		t.Fatal(err)
	}
	if err := c.ImpostaEmail(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	if got := valoreImpostazione(t, c, ChiaveEmailAcceso); got != "1" {
		t.Errorf("interruttore %q", got)
	}
	if id, dati := ultimoAudit(t, c, "outgoing.channel_enabled"); id != "email" || dati != `{"channel":"email"}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if err := c.ImpostaEmail(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.channel_enabled'`); n != 1 {
		t.Errorf("accesa due volte: %d righe", n)
	}
	// Accesa: la coda manda da sé, quindi né scarta né manda a mano.
	if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET stato = 'queued', fallito_il = NULL WHERE id = 5`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ScartaInSospeso(ctx, 5, contesto); !errors.Is(err, ErrEmailAccesa) {
		t.Errorf("scarta con l'email accesa: %v", err)
	}
	if _, err := c.MandaInSospeso(ctx, 5, contesto); !errors.Is(err, ErrEmailAccesa) {
		t.Errorf("manda con l'email accesa: %v", err)
	}
	if err := c.ImpostaEmail(ctx, false, contesto); err != nil {
		t.Fatal(err)
	}
	if got := valoreImpostazione(t, c, ChiaveEmailAcceso); got != "0" {
		t.Errorf("interruttore spento %q", got)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.channel_disabled"); dati != `{"channel":"email"}` {
		t.Errorf("audit spento %s", dati)
	}
	if acceso, _ := c.CanaleAttivo(ctx, CanaleEmail); acceso {
		t.Error("ancora accesa")
	}
}

// inventario: OPS-02, CLI-43
// «Manda» di una email rimasta in coda, con l'email spenta: un tentativo vero, adesso, con il provider
// SMTP contro il server finto (ConsegnaAMano non guarda l'interruttore), l'esito sulla riga come un giro
// della coda (accettata) e outgoing.queued_sent_by_hand con l'esito, mai testo né indirizzo. Il giro
// della coda, intanto, non la tocca (l'email resta spenta) e Consegna la rifiuta.
func TestMandaInSospesoConIlServerFinto(t *testing.T) {
	c, p := codaSMTP(t)
	ctx := context.Background()
	s := avviaServerFinto(t, &serverFinto{})
	p.radici = s.Radici
	m := ModuloSMTP{"host": "127.0.0.1", "port": fmt.Sprint(s.porta()), "security": "none", "from_address": "negozio@example.invalid"}
	if rif, err := c.SalvaSMTP(ctx, m, contesto); err != nil || rif != nil {
		t.Fatalf("%v %+v", err, rif)
	}
	inserisciRighe(t, c)
	if e, err := c.Giro(ctx, PerGiro); err != nil || e != (Esito{}) {
		t.Fatalf("giro con l'email spenta: %+v %v", e, err)
	}
	if _, err := p.Consegna(ctx, Messaggio{ID: 5, Canale: CanaleEmail, Indirizzo: "cliente-cinque@example.com", Testo: "x"}); err == nil {
		t.Fatal("Consegna con l'email spenta")
	}
	e, err := c.MandaInSospeso(ctx, 5, contesto)
	if err != nil {
		t.Fatal(err)
	}
	if e.Stato != "accepted" || e.Errore != "" || e.Tentativi != 1 {
		t.Errorf("esito %+v", e)
	}
	s.fine(t)
	if !s.haRicevuto("RCPT TO:<cliente-cinque@example.com>") {
		t.Errorf("comandi %q", s.ricevuti())
	}
	if got := riga(t, c.d, 5); !strings.HasPrefix(got, "accepted|1|") {
		t.Errorf("riga %s", got)
	}
	if id, dati := ultimoAudit(t, c, "outgoing.queued_sent_by_hand"); id != "5" || dati != `{"channel":"email","party":"customer","repair_id":null,"status":"accepted"}` {
		t.Errorf("audit %s %s", id, dati)
	}
	if _, err := c.MandaInSospeso(ctx, 5, contesto); !errors.Is(err, ErrNonInSospeso) {
		t.Errorf("seconda volta: %v", err)
	}
	if _, err := c.MandaInSospeso(ctx, 6, contesto); !errors.Is(err, ErrNonInSospeso) {
		t.Errorf("riga Telegram: %v", err)
	}
}

// inventario: OPS-02, CLI-51
// «Manda» rifiutato dal server: costa il tentativo come un giro (attesa, errore senza dati), la riga resta
// in coda e quindi da guardare; al quinto è «failed». Senza configurazione usabile non si prova niente e
// non si consuma il tentativo.
func TestMandaInSospesoRifiutato(t *testing.T) {
	c, p := codaSMTP(t)
	ctx := context.Background()
	inserisciRighe(t, c)
	if _, err := c.MandaInSospeso(ctx, 5, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Fatalf("senza configurazione: %v", err)
	}
	if got := riga(t, c.d, 5); !strings.HasPrefix(got, "queued|0|") {
		t.Errorf("riga dopo il rifiuto senza configurazione %s", got)
	}
	s := avviaServerFinto(t, &serverFinto{Risposte: map[string]string{"RCPT": "550 5.1.1 cliente-cinque@example.com sconosciuto"}})
	p.radici = s.Radici
	m := ModuloSMTP{"host": "127.0.0.1", "port": fmt.Sprint(s.porta()), "security": "none", "from_address": "negozio@example.invalid"}
	if _, err := c.SalvaSMTP(ctx, m, contesto); err != nil {
		t.Fatal(err)
	}
	e, err := c.MandaInSospeso(ctx, 5, contesto)
	if err != nil {
		t.Fatal(err)
	}
	if e.Stato != "queued" || e.Errore != "smtp:550" || e.Tentativi != 1 {
		t.Errorf("esito %+v", e)
	}
	if got := riga(t, c.d, 5); got != "queued|1|2026-11-11T09:00:10.000000Z|-|-|-|smtp:550" {
		t.Errorf("riga %s", got)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.queued_sent_by_hand"); dati != `{"channel":"email","party":"customer","repair_id":null,"status":"queued"}` {
		t.Errorf("audit %s", dati)
	}
	if _, err := c.d.Scrittura.Exec(`UPDATE messaggi_in_uscita SET tentativi = 4 WHERE id = 5`); err != nil {
		t.Fatal(err)
	}
	s2 := avviaServerFinto(t, &serverFinto{Risposte: map[string]string{"RCPT": "550 5.1.1 sconosciuto"}})
	if _, err := c.SalvaSMTP(ctx, con(m, "port", fmt.Sprint(s2.porta())), contesto); err != nil {
		t.Fatal(err)
	}
	if e, err = c.MandaInSospeso(ctx, 5, contesto); err != nil || e.Stato != "failed" || e.Tentativi != 5 {
		t.Errorf("quinto: %+v %v", e, err)
	}
}

// inventario: ADM-09, CLI-52, CLI-43
// Il messaggio di prova «subito» (069c, decisione dell'11/11): al mittente stesso, con l'oggetto e il
// testo di enqueue_test_message/2 dello standalone, ma dentro la richiesta e senza riga nella coda, anche
// con l'invio delle email spento (D-03). Senza configurazione ErrNonConfigurato, niente aperto e niente
// scritto. Spenta o accesa: il server finto lo riceve, nessuna riga, audit outgoing.test_sent con canale,
// parte e stato (mai indirizzo, testo o errore). Poi la configurazione cambia (un altro mittente) e la
// prova dopo va con quella nuova, senza riavviare niente.
func TestMessaggioDiProvaEConfigurazioneNuova(t *testing.T) {
	c, p := codaSMTP(t)
	ctx := context.Background()
	if _, err := c.MandaProva(ctx, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Fatalf("senza configurazione: %v", err)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent'`); n != 0 {
		t.Fatalf("audit senza configurazione: %d", n)
	}
	// Configurazione a metà (server sì, mittente no): lo stesso rifiuto, niente aperto, nessun audit.
	if _, err := c.d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.smtp.host', 'smtp.example.invalid', '2026-11-12T09:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MandaProva(ctx, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Fatalf("configurazione a metà: %v", err)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent'`); n != 0 {
		t.Fatalf("audit con la configurazione a metà: %d", n)
	}
	s := avviaServerFinto(t, &serverFinto{})
	p.radici = s.Radici
	m := ModuloSMTP{"host": "127.0.0.1", "port": fmt.Sprint(s.porta()), "security": "none", "from_address": "negozio@example.invalid", "from_name": "Negozio"}
	if _, err := c.SalvaSMTP(ctx, m, contesto); err != nil {
		t.Fatal(err)
	}
	// Spenta (D-03): parte lo stesso, subito.
	if errore, err := c.MandaProva(ctx, contesto); err != nil || errore != "" {
		t.Fatalf("spenta: %q %v", errore, err)
	}
	s.fine(t)
	if !s.haRicevuto("MAIL FROM:<negozio@example.invalid>") || !s.haRicevuto("RCPT TO:<negozio@example.invalid>") ||
		!strings.Contains(s.corpo(), "Subject: =?utf-8?B?"+base64.StdEncoding.EncodeToString([]byte("GestRip — prova del provider di posta"))+"?=") ||
		!strings.Contains(s.corpo(), base64.StdEncoding.EncodeToString([]byte("Messaggio di prova inviato dalla tua installazione GestRip.\n\nSe lo stai leggendo, l'account SMTP configurato funziona.\nNessun cliente e nessuna oreficeria ha ricevuto questo messaggio."))[:76]) {
		t.Errorf("prova: %q %q", s.ricevuti(), s.corpo())
	}
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatalf("righe %d", n)
	}
	if v := valoreImpostazione(t, c, ChiaveEmailAcceso); v == "1" {
		t.Fatalf("la prova ha acceso l'email")
	}
	id, dati := ultimoAudit(t, c, "outgoing.test_sent")
	var risultato string
	if err := c.d.Lettura.QueryRow(`SELECT esito FROM eventi_audit WHERE azione = 'outgoing.test_sent'`).Scan(&risultato); err != nil {
		t.Fatal(err)
	}
	if id != "email" || dati != `{"channel":"email","party":"shop","status":"accepted"}` || risultato != "ok" {
		t.Errorf("audit %q %s %q", id, dati, risultato)
	}
	// Accesa: lo stesso, sempre senza riga; la configurazione nuova vale subito.
	if err := c.ImpostaEmail(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	s2 := avviaServerFinto(t, &serverFinto{})
	if _, err := c.SalvaSMTP(ctx, con(m, "port", fmt.Sprint(s2.porta()), "from_address", "altro@example.invalid"), contesto); err != nil {
		t.Fatal(err)
	}
	if errore, err := c.MandaProva(ctx, contesto); err != nil || errore != "" {
		t.Fatalf("accesa: %q %v", errore, err)
	}
	s2.fine(t)
	if !s2.haRicevuto("MAIL FROM:<altro@example.invalid>") || !s2.haRicevuto("RCPT TO:<altro@example.invalid>") {
		t.Errorf("configurazione nuova: %q", s2.ricevuti())
	}
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatalf("righe accesa %d", n)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent'`); n != 2 {
		t.Fatalf("audit %d", n)
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued'`); n != 0 {
		t.Fatalf("outgoing.queued %d", n)
	}
}

// inventario: ADM-09, CLI-52, CLI-43
// La prova rifiutata dal server: l'errore è solo il genere («smtp:535», mai il testo del server, che
// ripete utente e password), nessuna riga, e l'audit outgoing.test_sent con risultato «error» e stato
// «failed», senza l'errore né l'indirizzo. Un provider senza invio a mano è come senza configurazione.
func TestMessaggioDiProvaRifiutato(t *testing.T) {
	c, p := codaSMTP(t)
	ctx := context.Background()
	s := avviaServerFinto(t, &serverFinto{Utente: "negozio-prova", Password: "altra-parola-finta"})
	p.radici = s.Radici
	m := ModuloSMTP{"host": "127.0.0.1", "port": fmt.Sprint(s.porta()), "security": "starttls", "username": "negozio-prova",
		"password": parolaDiProva, "from_address": "negozio@example.invalid"}
	if _, err := c.SalvaSMTP(ctx, m, contesto); err != nil {
		t.Fatal(err)
	}
	errore, err := c.MandaProva(ctx, contesto)
	if err != nil || errore != "smtp:535" {
		t.Fatalf("rifiuto %q %v", errore, err)
	}
	s.fine(t)
	if n := conta(t, c.d, `SELECT count(*) FROM messaggi_in_uscita`); n != 0 {
		t.Fatalf("righe %d", n)
	}
	var dati, risultato string
	if err := c.d.Lettura.QueryRow(`SELECT dati, esito FROM eventi_audit WHERE azione = 'outgoing.test_sent'`).Scan(&dati, &risultato); err != nil {
		t.Fatal(err)
	}
	if dati != `{"channel":"email","party":"shop","status":"failed"}` || risultato != "error" {
		t.Errorf("audit %s %q", dati, risultato)
	}
	for _, vietato := range []string{parolaDiProva, "negozio@example.invalid", "negozio-prova"} {
		if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent' AND dati LIKE ?`, "%"+vietato+"%"); n != 0 {
			t.Errorf("audit con %q", vietato)
		}
	}
	// Un provider email che non sa mandare a mano: niente prova.
	d, o := apriDB(t)
	c2 := Nuova(d, o, map[string]Provider{CanaleEmail: providerSoloCoda{}})
	if _, err := c2.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "security": "none", "from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.MandaProva(ctx, contesto); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("senza invio a mano: %v", err)
	}
}

// providerSoloCoda è un provider email senza ConsegnaAMano.
type providerSoloCoda struct{}

func (providerSoloCoda) Consegna(context.Context, Messaggio) (string, error) { return "250 ok", nil }

// providerLento è un provider email che ci mette un po' a rispondere e conta gli invii a mano.
type providerLento struct {
	mu    sync.Mutex
	invii int
}

func (p *providerLento) Consegna(ctx context.Context, m Messaggio) (string, error) {
	return p.ConsegnaAMano(ctx, m)
}

func (p *providerLento) ConsegnaAMano(context.Context, Messaggio) (string, error) {
	p.mu.Lock()
	p.invii++
	p.mu.Unlock()
	time.Sleep(200 * time.Millisecond)
	return "250 ok", nil
}

// inventario: OPS-02
// Due «Manda adesso» della stessa email insieme (un doppio clic, due schede): parte una volta sola; il
// secondo trova la riga già accettata (ErrNonInSospeso).
func TestMandaInSospesoUnaVolta(t *testing.T) {
	d, o := apriDB(t)
	p := &providerLento{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	ctx := context.Background()
	if _, err := c.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "security": "none", "from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	inserisciRighe(t, c)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() { _, errs[i] = c.MandaInSospeso(ctx, 5, contesto) })
	}
	wg.Wait()
	if p.invii != 1 {
		t.Errorf("invii %d", p.invii)
	}
	if (errs[0] == nil) == (errs[1] == nil) || !errors.Is(errors.Join(errs[0], errs[1]), ErrNonInSospeso) {
		t.Errorf("esiti %v %v", errs[0], errs[1])
	}
}

// inventario: ADM-09, CLI-52
// La richiesta chiusa durante la prova: il tentativo finisce lì (un errore senza dati), ma l'audit
// outgoing.test_sent si scrive lo stesso, con stato «failed».
func TestMessaggioDiProvaRichiestaChiusa(t *testing.T) {
	c, p := codaSMTP(t)
	s := avviaServerFinto(t, &serverFinto{Muto: true})
	p.radici = s.Radici
	if _, err := c.SalvaSMTP(context.Background(), ModuloSMTP{"host": "127.0.0.1", "port": fmt.Sprint(s.porta()), "security": "none",
		"from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	ctx, annulla := context.WithCancel(context.Background())
	defer annulla()
	time.AfterFunc(200*time.Millisecond, annulla)
	inizio := time.Now()
	errore, err := c.MandaProva(ctx, contesto)
	// Il contesto della richiesta chiude la connessione subito (non il tempo massimo dell'SMTP).
	if err != nil || errore != "smtp:transport::closed" || time.Since(inizio) > 10*time.Second {
		t.Fatalf("esito %q %v dopo %v", errore, err, time.Since(inizio))
	}
	if n := conta(t, c.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.test_sent' AND esito = 'error'
		AND dati = '{"channel":"email","party":"shop","status":"failed"}'`); n != 1 {
		t.Errorf("audit %d", n)
	}
}
