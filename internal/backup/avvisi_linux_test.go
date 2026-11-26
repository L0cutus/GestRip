package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Gli avvisi dell'allarme dei backup al negozio (avvisi.go, slice 127, tema E, risposta H, D-16). Database inventato,
// coda in uscita vera con i provider veri (nessuno va in rete: si accoda soltanto), uno Stato scritto a mano con
// l'orologio finto dentro (Stato.Adesso).
//
// Voce OPS-28, in parte: niente segno «inventario:» (la voce chiude con la copia fuori sede della 128).

const (
	mittenteAvvisi = "negozio@esempio.it"
	chatAvvisi     = "-1004242"
)

// alleRoma è un istante dell'ora del negozio.
func alleRoma(anno int, mese time.Month, giorno, ora, minuto, secondo int) time.Time {
	return time.Date(anno, mese, giorno, ora, minuto, secondo, 0, clock.Roma).UTC()
}

type provaAvvisi struct {
	d       *db.DB
	coda    *uscita.Coda
	a       *Avvisi
	mu      sync.Mutex
	stato   Stato
	errore  error
	letture int
}

func impostaAvvisi(t *testing.T, d *db.DB, chiave, valore string) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-01T08:00:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, chiave, valore); err != nil {
		t.Fatal(err)
	}
}

func accendiEmailAvvisi(t *testing.T, d *db.DB) {
	t.Helper()
	impostaAvvisi(t, d, "outgoing.smtp.host", "smtp.esempio.it")
	impostaAvvisi(t, d, "outgoing.smtp.from_address", mittenteAvvisi)
	impostaAvvisi(t, d, uscita.ChiaveEmailAcceso, "1")
}

func accendiTelegramAvvisi(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '4242:finto-SINTETICO', '2026-11-01T08:00:00.000000Z')
		ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore`); err != nil {
		t.Fatal(err)
	}
	impostaAvvisi(t, d, "outgoing.telegram.chat_id", chatAvvisi)
	impostaAvvisi(t, d, uscita.ChiaveTelegramAcceso, "1")
}

// nuoviAvvisiProva sono gli avvisi su un database inventato, con email e Telegram accesi.
func nuoviAvvisiProva(t *testing.T) *provaAvvisi {
	t.Helper()
	p := nuovaProva(t)
	d := p.s.DB
	accendiEmailAvvisi(t, d)
	accendiTelegramAvvisi(t, d)
	pa := &provaAvvisi{d: d}
	pa.coda = uscita.Nuova(d, p.s.Orologio, map[string]uscita.Provider{uscita.CanaleEmail: uscita.NuovoSMTP(d, p.s.Orologio),
		uscita.CanaleTelegram: uscita.NuovoTelegram(d, "http://bot.invalid")})
	pa.a = pa.nuovi(pa.coda)
	return pa
}

// nuovi sono altri Avvisi sullo stesso database e sullo stesso Stato finto (un riavvio di serve).
func (pa *provaAvvisi) nuovi(posta PostaDelNegozio) *Avvisi {
	a := NuoviAvvisi(NuovoNotturno(pa.d, nil, "", clock.NuovoFinto(adessoStato)), posta)
	a.stato = func(context.Context) (Stato, error) {
		pa.mu.Lock()
		defer pa.mu.Unlock()
		pa.letture++
		return pa.stato, pa.errore
	}
	return a
}

// con imposta lo stato dei backup del prossimo giro: adesso e i codici dell'allarme (statoBuono più i problemi).
func (pa *provaAvvisi) con(adesso time.Time, cambia ...func(*Stato)) {
	s := statoBuono()
	sposta := adesso.Sub(s.Adesso)
	s.Adesso = adesso
	s.Esecuzioni[0].IniziatoIl = s.Esecuzioni[0].IniziatoIl.Add(sposta)
	s.UltimoBackup.CatturatoIl = s.UltimoBackup.CatturatoIl.Add(sposta)
	s.PrimoBackup = s.PrimoBackup.Add(sposta)
	s.PiuAvanti = s.PiuAvanti.Add(sposta)
	s.Prova.FinitoIl = s.Prova.FinitoIl.Add(sposta)
	for _, c := range cambia {
		c(&s)
	}
	pa.mu.Lock()
	pa.stato, pa.errore = s, nil
	pa.mu.Unlock()
}

// I problemi di prova, come cambi di uno Stato sano.
var (
	conChiavettaAssente = func(s *Stato) { s.Chiavetta = fmt.Errorf("%w: /media/chiavetta-segreta", ErrNonMontata) }
	conChiavettaRO      = func(s *Stato) { s.Chiavetta = fmt.Errorf("%w: /media/chiavetta-segreta", ErrChiavettaSolaLettura) }
	conFallito          = func(s *Stato) {
		s.Esecuzioni[0].Esito, s.Esecuzioni[0].Errore = EsitoFallito, "scrittura su /media/chiavetta-segreta/gestrip-x.age: I/O"
	}
	conVecchio = func(s *Stato) { s.UltimoBackup.CatturatoIl = s.Adesso.Add(-27 * time.Hour) }
)

// giro fa un giro e dice il messaggio accodato ("" se nessuno).
func (pa *provaAvvisi) giro(t *testing.T, a *Avvisi) string {
	t.Helper()
	m, ok, err := a.Giro(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ok != (m.Testo != "") {
		t.Fatalf("accodato %v con il testo %q", ok, m.Testo)
	}
	return m.Testo
}

type rigaCoda struct {
	canale, destinatario, indirizzo, testo string
	oggetto                                sql.NullString
}

func (pa *provaAvvisi) righe(t *testing.T) []rigaCoda {
	t.Helper()
	rs, err := pa.d.Lettura.Query(`SELECT canale, destinatario, indirizzo, oggetto, testo FROM messaggi_in_uscita ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var v []rigaCoda
	for rs.Next() {
		var r rigaCoda
		if err := rs.Scan(&r.canale, &r.destinatario, &r.indirizzo, &r.oggetto, &r.testo); err != nil {
			t.Fatal(err)
		}
		v = append(v, r)
	}
	return v
}

func (pa *provaAvvisi) statoScritto(t *testing.T) (statoAvvisi, bool) {
	t.Helper()
	var v string
	err := pa.d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveAvvisi).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return statoAvvisi{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var s statoAvvisi
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		t.Fatal(err)
	}
	return s, true
}

func codiciAperti(s statoAvvisi) []string {
	var v []string
	for c := range s.Aperti {
		v = append(v, c)
	}
	slices.Sort(v)
	return v
}

// Nascita, uno al giorno alle 9 del negozio (non prima, non due volte, anche il giorno dopo), risoluzione; su email
// e Telegram, al negozio, l'oggetto solo sull'email; lo stato scritto con le righe.
func TestAvvisiNascitaGiornoRisoluzione(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	passi := []struct {
		nome    string
		adesso  time.Time
		cambia  []func(*Stato)
		inizio  string // "" nessun messaggio
		oggetto string
	}{
		{"sano", alleRoma(2026, 11, 25, 1, 0, 0), nil, "", ""},
		{"nasce di notte", alleRoma(2026, 11, 25, 2, 35, 0), []func(*Stato){conFallito}, "⚠ Backup del negozio: c'è un problema nuovo.", "GestRip: problema con i backup"},
		{"stesso giro", alleRoma(2026, 11, 25, 2, 40, 0), []func(*Stato){conFallito}, "", ""},
		{"stesso giorno dopo le 9", alleRoma(2026, 11, 25, 9, 0, 0), []func(*Stato){conFallito}, "", ""},
		{"stesso giorno sera", alleRoma(2026, 11, 25, 23, 59, 59), []func(*Stato){conFallito}, "", ""},
		{"giorno dopo prima delle 9", alleRoma(2026, 11, 26, 8, 59, 59), []func(*Stato){conFallito}, "", ""},
		{"giorno dopo alle 9", alleRoma(2026, 11, 26, 9, 0, 0), []func(*Stato){conFallito}, "⚠ Backup del negozio: promemoria", "GestRip: i backup hanno ancora un problema"},
		{"giorno dopo 9:05", alleRoma(2026, 11, 26, 9, 5, 0), []func(*Stato){conFallito}, "", ""},
		{"terzo giorno", alleRoma(2026, 11, 27, 9, 3, 0), []func(*Stato){conFallito}, "⚠ Backup del negozio: promemoria", "GestRip: i backup hanno ancora un problema"},
		{"sparito, si aspetta", alleRoma(2026, 11, 27, 12, 0, 0), nil, "", ""},
		{"sparito da 29 minuti", alleRoma(2026, 11, 27, 12, 29, 59), nil, "", ""},
		{"risolto dopo 30 minuti", alleRoma(2026, 11, 27, 12, 30, 0), nil, "✅ Backup del negozio: di nuovo tutto a posto.", "GestRip: backup di nuovo a posto"},
		{"dopo il risolto", alleRoma(2026, 11, 28, 9, 0, 0), nil, "", ""},
	}
	attese := 0
	for _, p := range passi {
		pa.con(p.adesso, p.cambia...)
		testo := pa.giro(t, pa.a)
		if !strings.HasPrefix(testo, p.inizio) || (p.inizio == "") != (testo == "") {
			t.Fatalf("%s: messaggio %q, atteso che inizi con %q", p.nome, testo, p.inizio)
		}
		if p.inizio != "" {
			attese += 2
		}
		rr := pa.righe(t)
		if len(rr) != attese {
			t.Fatalf("%s: %d righe in coda, attese %d", p.nome, len(rr), attese)
		}
		if p.inizio == "" {
			continue
		}
		email, tg := rr[len(rr)-2], rr[len(rr)-1]
		if email.canale != uscita.CanaleEmail || email.destinatario != "shop" || email.indirizzo != mittenteAvvisi ||
			email.oggetto.String != p.oggetto || email.testo != testo {
			t.Errorf("%s: email %+v", p.nome, email)
		}
		if tg.canale != uscita.CanaleTelegram || tg.destinatario != "shop" || tg.indirizzo != chatAvvisi || tg.oggetto.Valid || tg.testo != testo {
			t.Errorf("%s: telegram %+v", p.nome, tg)
		}
	}
	// Il promemoria ha l'ora in cui il problema è nato e il testo di adesso.
	rr := pa.righe(t)
	for _, i := range []int{2, 4} {
		if !strings.Contains(rr[i].testo, "(dal 25/11/2026 02:35)") || !strings.Contains(rr[i].testo, "L'ultimo backup (") {
			t.Errorf("promemoria %d: %q", i, rr[i].testo)
		}
	}
	if !strings.Contains(rr[6].testo, "• ultimo backup non riuscito") || !strings.Contains(rr[6].testo, "Non ci sono altri problemi") {
		t.Errorf("risolto: %q", rr[6].testo)
	}
	s, ok := pa.statoScritto(t)
	if !ok || len(s.Aperti) != 0 {
		t.Errorf("stato dopo il risolto: %+v %v", s, ok)
	}
	// Le righe d'audit della coda sono del pianificatore, come i lavori automatici.
	var attori int
	if err := pa.d.Lettura.QueryRow(`SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND attore = 'scheduler'`).Scan(&attori); err != nil || attori != attese {
		t.Errorf("audit: %d righe, errore %v", attori, err)
	}
}

// Il confine delle 9 con l'ora legale e l'ora solare: le 9 del negozio, non le 9 UTC.
func TestAvvisiNoveDelNegozio(t *testing.T) {
	prima := statoAvvisi{Aperti: map[string]time.Time{ProblemaChiavetta: adessoStato.Add(-48 * time.Hour)}, Giorno: "2026-03-28"}
	allarme := []Problema{{ProblemaChiavetta, "la chiavetta"}}
	for _, c := range []struct {
		adesso time.Time
		manda  bool
	}{
		{time.Date(2026, 3, 29, 6, 59, 59, 0, time.UTC), false}, // 08:59:59 CEST, il giorno del cambio
		{time.Date(2026, 3, 29, 7, 0, 0, 0, time.UTC), true},    // 09:00 CEST
		{time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC), true},    // 10:00 CEST
		{time.Date(2026, 3, 28, 23, 0, 0, 0, time.UTC), false},  // mezzanotte a Roma del 29: prima delle 9
	} {
		_, ok, dopo, _ := decidiAvviso(prima, allarme, false, c.adesso)
		if ok != c.manda || ok && dopo.Giorno != "2026-03-29" {
			t.Errorf("%v: manda %v giorno %q", c.adesso, ok, dopo.Giorno)
		}
	}
	// Un giorno scritto «nel futuro» (l'orologio tornato indietro) non ferma il promemoria.
	prima.Giorno = "2026-12-31"
	if _, ok, dopo, _ := decidiAvviso(prima, allarme, false, alleRoma(2026, 11, 26, 9, 0, 0)); !ok || dopo.Giorno != "2026-11-26" {
		t.Errorf("giorno nel futuro: %v %q", ok, dopo.Giorno)
	}
}

// Un problema che cambia: ogni codice nasce e si risolve per conto suo; lo stesso codice con un testo diverso resta
// lo stesso problema. Un messaggio di risoluzione che elenca problemi ancora aperti vale come quello del giorno.
func TestAvvisiProblemaCheCambia(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	g := func(adesso time.Time, cambia ...func(*Stato)) string {
		pa.con(adesso, cambia...)
		return pa.giro(t, pa.a)
	}
	if m := g(alleRoma(2026, 11, 25, 10, 0, 0), conChiavettaAssente); !strings.Contains(m, "Nuovo:\n• La chiavetta dei backup non c'è") {
		t.Fatalf("chiavetta: %q", m)
	}
	// Lo stesso codice, un altro testo: niente.
	if m := g(alleRoma(2026, 11, 25, 11, 0, 0), conChiavettaRO); m != "" {
		t.Fatalf("stesso codice: %q", m)
	}
	// Dopo 26 ore nasce anche «backup vecchio»: nuovo, con la chiavetta fra quelli ancora aperti.
	m := g(alleRoma(2026, 11, 26, 8, 0, 0), conChiavettaAssente, conVecchio)
	if !strings.Contains(m, "Nuovo:\n• L'ultimo backup buono è del") || !strings.Contains(m, "Ancora da sistemare:\n• (dal 25/11/2026 10:00) La chiavetta") ||
		strings.Contains(m, "Risolto:") {
		t.Fatalf("vecchio: %q", m)
	}
	// Rimessa la chiavetta, il backup è ancora vecchio: dopo 30 minuti risolto uno, l'altro resta.
	if m := g(alleRoma(2026, 11, 27, 8, 0, 0), conVecchio); m != "" {
		t.Fatalf("chiavetta rimessa, prima dei 30 minuti: %q", m)
	}
	m = g(alleRoma(2026, 11, 27, 8, 30, 0), conVecchio)
	if !strings.HasPrefix(m, "✅ Backup del negozio: un problema è risolto, ma non tutti.") || !strings.Contains(m, "Risolto:\n• chiavetta dei backup") ||
		!strings.Contains(m, "Ancora da sistemare:\n• (dal 26/11/2026 08:00) L'ultimo backup buono") {
		t.Fatalf("risolto uno: %q", m)
	}
	// Quel messaggio elencava «backup vecchio»: niente promemoria lo stesso giorno.
	if m := g(alleRoma(2026, 11, 27, 9, 30, 0), conVecchio); m != "" {
		t.Fatalf("promemoria dopo un risolto parziale: %q", m)
	}
	// Nello stesso giro uno nasce e uno sparisce: la nascita dice che l'altro sembra sistemato; 30 minuti dopo il
	// risolto.
	m = g(alleRoma(2026, 11, 27, 10, 0, 0), conFallito)
	if !strings.Contains(m, "Nuovo:\n• L'ultimo backup (") || strings.Contains(m, "Risolto:") ||
		!strings.Contains(m, "Ancora da sistemare:\n• (dal 26/11/2026 08:00) backup mancante o troppo vecchio: sembra sistemato") {
		t.Fatalf("scambio: %q", m)
	}
	m = g(alleRoma(2026, 11, 27, 10, 30, 0), conFallito)
	if !strings.Contains(m, "Risolto:\n• backup mancante o troppo vecchio") || !strings.Contains(m, "Ancora da sistemare:\n• (dal 27/11/2026 10:00) L'ultimo backup (") {
		t.Fatalf("scambio, risolto: %q", m)
	}
	s, _ := pa.statoScritto(t)
	if got := codiciAperti(s); !slices.Equal(got, []string{ProblemaBackupFallito}) || s.Giorno != "2026-11-27" {
		t.Errorf("stato: %v %q", got, s.Giorno)
	}
}

// Un riavvio (Avvisi nuovi sullo stesso database) non rimanda la nascita né il promemoria del giorno e non li perde.
// Uno stato illeggibile riparte da niente avvisato: il problema si ridice.
func TestAvvisiRiavvio(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 9, 30, 0), conFallito)
	if pa.giro(t, pa.a) == "" {
		t.Fatal("nessuna nascita")
	}
	dopo := pa.nuovi(pa.coda)
	if m := pa.giro(t, dopo); m != "" {
		t.Fatalf("dopo il riavvio, rimandato: %q", m)
	}
	pa.con(alleRoma(2026, 11, 26, 9, 0, 0), conFallito)
	if m := pa.giro(t, pa.nuovi(pa.coda)); !strings.Contains(m, "promemoria") {
		t.Fatalf("promemoria perso dopo il riavvio: %q", m)
	}
	if m := pa.giro(t, pa.nuovi(pa.coda)); m != "" {
		t.Fatalf("promemoria rimandato dopo il riavvio: %q", m)
	}
	// Riavvio durante i 30 minuti: l'inizio dell'assenza è scritto, il risolto arriva 30 minuti dopo l'inizio.
	pa.con(alleRoma(2026, 11, 26, 10, 0, 0))
	if m := pa.giro(t, pa.nuovi(pa.coda)); m != "" {
		t.Fatalf("risolto prima dei 30 minuti: %q", m)
	}
	if s, _ := pa.statoScritto(t); !s.Assenti[ProblemaBackupFallito].Equal(alleRoma(2026, 11, 26, 10, 0, 0)) {
		t.Fatalf("assenza non scritta: %+v", s)
	}
	pa.con(alleRoma(2026, 11, 26, 10, 20, 0))
	if m := pa.giro(t, pa.nuovi(pa.coda)); m != "" {
		t.Fatalf("risolto dopo 20 minuti: %q", m)
	}
	pa.con(alleRoma(2026, 11, 26, 10, 30, 0))
	if m := pa.giro(t, pa.nuovi(pa.coda)); !strings.Contains(m, "tutto a posto") {
		t.Fatalf("risolto perso dopo il riavvio: %q", m)
	}
	if n := len(pa.righe(t)); n != 6 {
		t.Errorf("%d righe, attese 6", n)
	}
	// Stato illeggibile: il problema di adesso si dice come nuovo.
	impostaAvvisi(t, pa.d, ChiaveAvvisi, "{rotto")
	pa.con(alleRoma(2026, 11, 26, 11, 0, 0), conChiavettaAssente)
	if m := pa.giro(t, pa.a); !strings.Contains(m, "problema nuovo") {
		t.Fatalf("stato rotto: %q", m)
	}
}

// Canali spenti (D-16): niente messaggi, lo stato dei backup non si legge e quello degli avvisi non si tocca; un
// canale solo: solo quello. Telegram configurato ma spento, o acceso senza chat: non c'è.
func TestAvvisiCanaliSpenti(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	impostaAvvisi(t, pa.d, uscita.ChiaveEmailAcceso, "0")
	impostaAvvisi(t, pa.d, uscita.ChiaveTelegramAcceso, "0")
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conFallito)
	if m := pa.giro(t, pa.a); m != "" || pa.letture != 0 {
		t.Fatalf("spenti: %q, %d letture dello stato", m, pa.letture)
	}
	if _, ok := pa.statoScritto(t); ok || len(pa.righe(t)) != 0 {
		t.Fatal("spenti: scritto qualcosa")
	}
	// Senza provider (la coda senza canali): lo stesso.
	vuota := pa.nuovi(uscita.Nuova(pa.d, clock.NuovoFinto(adessoStato), nil))
	if m := pa.giro(t, vuota); m != "" {
		t.Fatalf("senza provider: %q", m)
	}
	// Solo l'email: una riga sola, all'email; Telegram acceso ma senza chat non conta.
	impostaAvvisi(t, pa.d, uscita.ChiaveEmailAcceso, "1")
	impostaAvvisi(t, pa.d, uscita.ChiaveTelegramAcceso, "1")
	impostaAvvisi(t, pa.d, "outgoing.telegram.chat_id", " ")
	if m := pa.giro(t, pa.a); m == "" {
		t.Fatal("solo email: niente")
	}
	if rr := pa.righe(t); len(rr) != 1 || rr[0].canale != uscita.CanaleEmail {
		t.Fatalf("solo email: %+v", rr)
	}
	// Un provider che non sa dire se è acceso (attivo vuol dire «c'è»): una chat che non è una chat non è un
	// destinatario, la seconda serratura di uscita.ConfigTelegram.Usabile.
	impostaAvvisi(t, pa.d, "outgoing.telegram.chat_id", "chat-non-valida")
	nudi := pa.nuovi(uscita.Nuova(pa.d, clock.NuovoFinto(adessoStato), map[string]uscita.Provider{uscita.CanaleTelegram: providerNudo{}}))
	pa.con(alleRoma(2026, 11, 25, 10, 30, 0), conFallito, conChiavettaAssente) // un problema nuovo: ci sarebbe da dire
	if m := pa.giro(t, nudi); m != "" {
		t.Fatalf("chat non valida: %q", m)
	}
	pa.con(alleRoma(2026, 11, 25, 10, 30, 0), conFallito)
	// Spenti di nuovo mentre il problema si risolve: niente; riaccesi, il risolto arriva.
	impostaAvvisi(t, pa.d, uscita.ChiaveEmailAcceso, "0")
	pa.con(alleRoma(2026, 11, 25, 11, 0, 0))
	if m := pa.giro(t, pa.a); m != "" {
		t.Fatalf("spenti al risolto: %q", m)
	}
	impostaAvvisi(t, pa.d, uscita.ChiaveEmailAcceso, "1")
	if m := pa.giro(t, pa.a); m != "" {
		t.Fatalf("riaccesi, prima dei 30 minuti: %q", m)
	}
	pa.con(alleRoma(2026, 11, 25, 11, 30, 0))
	if m := pa.giro(t, pa.a); !strings.Contains(m, "tutto a posto") {
		t.Fatalf("riaccesi: %q", m)
	}
}

// Con i lavori automatici spenti tacciono gli stessi problemi del banner (non configurato, backup vecchio, prova
// mancante); gli altri si avvisano. Anche con il Notturno vero non configurato.
func TestAvvisiLavoriSpenti(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conVecchio, func(s *Stato) {
		s.LavoroSpento = true
		s.Prova.FinitoIl = s.Adesso.Add(-9 * 24 * time.Hour)
	})
	if m := pa.giro(t, pa.a); m != "" {
		t.Fatalf("lavori spenti: %q", m)
	}
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conVecchio, conChiavettaAssente, func(s *Stato) { s.LavoroSpento = true })
	m := pa.giro(t, pa.a)
	if !strings.Contains(m, "La chiavetta dei backup non c'è") || strings.Contains(m, "L'ultimo backup buono") {
		t.Fatalf("lavori spenti con la chiavetta: %q", m)
	}
	// Il Notturno vero, senza chiavetta configurata: con il lavoro spento niente, acceso «non configurato».
	for _, spento := range []bool{true, false} {
		p := nuovaProva(t)
		accendiTelegramAvvisi(t, p.s.DB)
		n := NuovoNotturno(p.s.DB, p.s.Archivio, "", clock.NuovoFinto(alleRoma(2026, 11, 25, 10, 0, 0)))
		if spento {
			n.ConLavoroSpento()
		}
		a := NuoviAvvisi(n, uscita.Nuova(p.s.DB, p.s.Orologio, map[string]uscita.Provider{uscita.CanaleTelegram: uscita.NuovoTelegram(p.s.DB, "http://bot.invalid")}))
		m, ok, err := a.Giro(context.Background())
		if err != nil || ok == spento || !spento && !strings.Contains(m.Testo, "Il backup automatico non è configurato") {
			t.Errorf("spento=%v: %q %v %v", spento, m.Testo, ok, err)
		}
	}
}

// Lo stato dei backup illeggibile: niente messaggi e niente «risolto»; lo stato degli avvisi resta com'era.
func TestAvvisiStatoIlleggibile(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conFallito)
	if pa.giro(t, pa.a) == "" {
		t.Fatal("nessuna nascita")
	}
	prima, _ := pa.statoScritto(t)
	pa.mu.Lock()
	pa.stato, pa.errore = Stato{Adesso: alleRoma(2026, 11, 26, 10, 0, 0)}, errors.New("database occupato")
	pa.mu.Unlock()
	m, ok, err := pa.a.Giro(context.Background())
	if err == nil || ok || m.Testo != "" {
		t.Fatalf("illeggibile: %q %v %v", m.Testo, ok, err)
	}
	dopo, _ := pa.statoScritto(t)
	if !slices.Equal(codiciAperti(prima), codiciAperti(dopo)) || prima.Giorno != dopo.Giorno || len(pa.righe(t)) != 2 {
		t.Errorf("illeggibile: stato %+v → %+v, %d righe", prima, dopo, len(pa.righe(t)))
	}
}

// providerNudo è un provider senza accensione: per la coda il suo canale è attivo se c'è.
type providerNudo struct{}

func (providerNudo) Consegna(context.Context, uscita.Messaggio) (string, error) {
	return "", errors.New("mai")
}

// postaCheCade è la coda vera che rifiuta la riga di un canale.
type postaCheCade struct {
	*uscita.Coda
	cade string
}

func (p postaCheCade) AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error) {
	if dest.Canale == p.cade {
		return 0, errors.New("rifiutata")
	}
	return p.Coda.AccodaIn(ctx, tx, dest, cont, a)
}

// Righe e stato insieme o niente: una riga rifiutata (Telegram, la seconda) non lascia l'email accodata né lo stato
// scritto, e il giro dopo riprova.
func TestAvvisiTuttoONiente(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conFallito)
	if _, ok, err := pa.nuovi(postaCheCade{pa.coda, uscita.CanaleTelegram}).Giro(context.Background()); err == nil || ok {
		t.Fatalf("rifiutata: %v %v", ok, err)
	}
	if _, ok := pa.statoScritto(t); ok || len(pa.righe(t)) != 0 {
		t.Fatalf("rifiutata: scritto qualcosa (%d righe)", len(pa.righe(t)))
	}
	if pa.giro(t, pa.a) == "" || len(pa.righe(t)) != 2 {
		t.Fatal("il giro dopo non riprova")
	}
}

// I testi: mai percorsi (il punto di montaggio negli errori della chiavetta e nel motivo dell'esecuzione), mai il
// motivo grezzo; le sole barre sono quelle delle date; stanno in un messaggio Telegram.
func TestAvvisiTestiSenzaPercorsi(t *testing.T) {
	tutti := []func(*Stato){conChiavettaAssente, conFallito, conVecchio, func(s *Stato) {
		s.PiuAvanti = s.Adesso.Add(time.Hour)
		s.Prova.Riuscita = false
		s.Rovinati, s.NomiRovinati = 3, []string{strings.Repeat("ab", 32)}
	}}
	pa := nuoviAvvisiProva(t)
	var messaggi []string
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), tutti...)
	messaggi = append(messaggi, pa.giro(t, pa.a))
	pa.con(alleRoma(2026, 11, 26, 9, 0, 0), tutti...)
	messaggi = append(messaggi, pa.giro(t, pa.a))
	pa.con(alleRoma(2026, 11, 26, 10, 0, 0), conChiavettaAssente)
	pa.giro(t, pa.a) // gli altri spariscono: si aspettano 30 minuti
	pa.con(alleRoma(2026, 11, 26, 10, 30, 0), conChiavettaAssente)
	messaggi = append(messaggi, pa.giro(t, pa.a))
	pa.con(alleRoma(2026, 11, 26, 11, 0, 0))
	pa.giro(t, pa.a)
	pa.con(alleRoma(2026, 11, 26, 11, 30, 0))
	messaggi = append(messaggi, pa.giro(t, pa.a))
	// Senza configurazione.
	pa.con(alleRoma(2026, 11, 26, 12, 0, 0), func(s *Stato) { s.Configurato = false })
	messaggi = append(messaggi, pa.giro(t, pa.a))
	s, _ := pa.statoScritto(t)
	if len(s.Aperti) != 1 {
		t.Fatalf("non configurato: %+v", s)
	}
	reBarra := regexp.MustCompile(`/`)
	for i, m := range messaggi {
		if m == "" {
			t.Fatalf("messaggio %d vuoto", i)
		}
		senzaDate := reData.ReplaceAllString(m, "")
		if strings.Contains(m, "/media") || strings.Contains(m, "chiavetta-segreta") || strings.Contains(m, "gestrip-x.age") ||
			strings.Contains(m, "I/O") || strings.Contains(m, strings.Repeat("ab", 32)) || reBarra.MatchString(senzaDate) {
			t.Errorf("messaggio %d con un percorso o il motivo: %q", i, m)
		}
		if n := len([]rune(m)); n > 4096 {
			t.Errorf("messaggio %d troppo lungo per Telegram: %d", i, n)
		}
	}
	if !strings.Contains(messaggi[0], "L'ultimo backup ha trovato 3 foto o file rovinati") || !strings.Contains(messaggi[0], "un'ora nel futuro") {
		t.Errorf("tutti: %q", messaggi[0])
	}
	// Ogni codice ha il suo titolo per il «risolto»; uno sconosciuto ne ha uno generico.
	for _, c := range []string{ProblemaNonConfigurato, ProblemaChiavetta, ProblemaBackupFallito, ProblemaBackupVecchio, ProblemaOrologio,
		ProblemaProvaFallita, ProblemaProvaMancante, ProblemaMediaRovinati} {
		if titoliAvviso[c] == "" {
			t.Errorf("titolo mancante per %s", c)
		}
	}
	if titoloAvviso("sconosciuto") != "un problema dei backup" {
		t.Error("titolo dello sconosciuto")
	}
}

// Il lavoro: la prima attesa è AttesaAvvisiDopoAvvio, poi OgniQuantoAvvisi; un giro in panico non lo ferma.
func TestAvvisiLavoro(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conFallito)
	giri := 0
	a := pa.nuovi(pa.coda)
	vero := a.stato
	a.stato = func(ctx context.Context) (Stato, error) {
		giri++
		if giri == 1 {
			panic("giro rotto")
		}
		return vero(ctx)
	}
	var attese []time.Duration
	a.Lavoro(context.Background(), func(_ context.Context, d time.Duration) bool {
		attese = append(attese, d)
		return len(attese) < 3
	})
	if !slices.Equal(attese, []time.Duration{AttesaAvvisiDopoAvvio, OgniQuantoAvvisi, OgniQuantoAvvisi}) || giri != 2 || len(pa.righe(t)) != 2 {
		t.Errorf("attese %v, giri %d, righe %d", attese, giri, len(pa.righe(t)))
	}
	// Il contesto finito: nessun giro.
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	giri = 0
	a.Lavoro(ctx, func(context.Context, time.Duration) bool { return true })
	if giri != 0 {
		t.Errorf("contesto finito: %d giri", giri)
	}
}

// Revisione della 127, MEDIA 1: una chiavetta con un contatto lento che va e viene ogni 5 minuti per due ore fa un
// solo messaggio (la nascita); il problema resta aperto con la sua data di nascita; il risolto arriva solo dopo 30
// minuti di fila senza il codice.
func TestAvvisiContattoBallerino(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	inizio := alleRoma(2026, 11, 25, 22, 0, 0)
	for i := 0; i <= 24; i++ {
		adesso := inizio.Add(time.Duration(i) * OgniQuantoAvvisi)
		if i%2 == 0 {
			pa.con(adesso, conChiavettaAssente)
		} else {
			pa.con(adesso)
		}
		m := pa.giro(t, pa.a)
		if i == 0 && !strings.Contains(m, "problema nuovo") || i > 0 && m != "" {
			t.Fatalf("giro %d: %q", i, m)
		}
	}
	// Assente per 25 minuti e poi di nuovo presente: ancora niente, l'assenza ricomincia da capo.
	ultimo := inizio.Add(24 * OgniQuantoAvvisi)
	for _, d := range []time.Duration{5, 10, 15, 20, 25} {
		pa.con(ultimo.Add(d * time.Minute))
		if m := pa.giro(t, pa.a); m != "" {
			t.Fatalf("assente da %v: %q", d, m)
		}
	}
	pa.con(ultimo.Add(30*time.Minute), conChiavettaAssente)
	if m := pa.giro(t, pa.a); m != "" {
		t.Fatalf("tornato: %q", m)
	}
	s, _ := pa.statoScritto(t)
	if !s.Aperti[ProblemaChiavetta].Equal(inizio) || len(s.Assenti) != 0 {
		t.Fatalf("stato dopo il ritorno: %+v", s)
	}
	fine := ultimo.Add(35 * time.Minute)
	for _, d := range []time.Duration{0, 29 * time.Minute} {
		pa.con(fine.Add(d))
		if m := pa.giro(t, pa.a); m != "" {
			t.Fatalf("sparito da %v: %q", d, m)
		}
	}
	pa.con(fine.Add(30 * time.Minute))
	if m := pa.giro(t, pa.a); !strings.Contains(m, "tutto a posto") || !strings.Contains(m, "• chiavetta dei backup") {
		t.Fatalf("risolto: %q", m)
	}
	if n := len(pa.righe(t)); n != 4 {
		t.Errorf("%d righe, attese 4 (nascita e risolto su due canali)", n)
	}
}

// Revisione della 127, BASSA: senza configurazione Problemi dà solo «non configurato»; gli altri problemi aperti
// sono nascosti, non risolti: nessun «Risolto» per loro finché la configurazione non torna.
func TestAvvisiNonConfiguratoNasconde(t *testing.T) {
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conChiavettaAssente, conFallito)
	pa.giro(t, pa.a)
	nonConf := func(s *Stato) { s.Configurato = false }
	pa.con(alleRoma(2026, 11, 25, 11, 0, 0), nonConf)
	m := pa.giro(t, pa.a)
	if !strings.Contains(m, "Il backup automatico non è configurato") || strings.Contains(m, "Risolto") || strings.Contains(m, "sembra sistemato") {
		t.Fatalf("non configurato: %q", m)
	}
	pa.con(alleRoma(2026, 11, 25, 13, 0, 0), nonConf)
	if m := pa.giro(t, pa.a); m != "" {
		t.Fatalf("non configurato, due ore dopo: %q", m)
	}
	s, _ := pa.statoScritto(t)
	if got := codiciAperti(s); !slices.Equal(got, []string{ProblemaBackupFallito, ProblemaChiavetta, ProblemaNonConfigurato}) || len(s.Assenti) != 0 {
		t.Fatalf("stato: %v %+v", got, s.Assenti)
	}
	// Configurato e sano: 30 minuti dopo tutti risolti.
	pa.con(alleRoma(2026, 11, 25, 14, 0, 0))
	pa.giro(t, pa.a)
	pa.con(alleRoma(2026, 11, 25, 14, 30, 0))
	m = pa.giro(t, pa.a)
	for _, x := range []string{"• backup automatico non configurato", "• chiavetta dei backup", "• ultimo backup non riuscito", "tutto a posto"} {
		if !strings.Contains(m, x) {
			t.Errorf("risolto senza %q: %q", x, m)
		}
	}
}

// Revisione della 127, MEDIA 2: con la chiavetta appesa (la lettura dello stato non torna) il giro scade dopo
// TempoStatoAvvisi e non manda niente; una sola lettura alla volta; lo stop del lavoro ritorna senza aspettare la
// lettura appesa.
func TestAvvisiChiavettaAppesa(t *testing.T) {
	vecchio := TempoStatoAvvisi
	TempoStatoAvvisi = 50 * time.Millisecond
	t.Cleanup(func() { TempoStatoAvvisi = vecchio })
	pa := nuoviAvvisiProva(t)
	pa.con(alleRoma(2026, 11, 25, 10, 0, 0), conFallito)
	appesa := make(chan struct{})
	var letture int
	var mu sync.Mutex
	a := pa.nuovi(pa.coda)
	a.stato = func(context.Context) (Stato, error) {
		mu.Lock()
		letture++
		mu.Unlock()
		<-appesa // come ControllaChiavetta su una chiavetta appesa: non guarda il contesto
		return pa.stato, nil
	}
	for range 2 {
		if _, ok, err := a.Giro(context.Background()); !errors.Is(err, ErrStatoAvvisiScaduto) || ok {
			t.Fatalf("appesa: %v %v", ok, err)
		}
	}
	if _, ok := pa.statoScritto(t); ok || len(pa.righe(t)) != 0 {
		t.Fatal("appesa: scritto qualcosa")
	}
	// Il lavoro: un giro (appeso), poi l'attesa finché arriva lo stop; lo stop ritorna subito.
	ctx, ferma := context.WithCancel(context.Background())
	finito := make(chan struct{})
	nelGiro := make(chan struct{}, 4)
	go func() {
		defer close(finito)
		a.Lavoro(ctx, func(ctx context.Context, d time.Duration) bool {
			nelGiro <- struct{}{}
			if d == AttesaAvvisiDopoAvvio {
				return true
			}
			<-ctx.Done()
			return false
		})
	}()
	<-nelGiro
	<-nelGiro // la seconda attesa: il giro appeso è scaduto
	ferma()
	select {
	case <-finito:
	case <-time.After(5 * time.Second):
		t.Fatal("lo stop aspetta la lettura appesa")
	}
	mu.Lock()
	n := letture
	mu.Unlock()
	if n != 1 || len(pa.righe(t)) != 0 {
		t.Errorf("%d letture (una sola alla volta), %d righe", n, len(pa.righe(t)))
	}
	// Sbloccata, la lettura finisce e il giro dopo ne fa una nuova e avvisa.
	close(appesa)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if m, ok, err := a.Giro(context.Background()); err == nil && ok && strings.Contains(m.Testo, "problema nuovo") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dopo lo sblocco nessun avviso")
		}
	}
	// Lo stop durante l'attesa della lettura: torna l'errore del contesto.
	a.stato = func(context.Context) (Stato, error) { select {} }
	c2, f2 := context.WithCancel(context.Background())
	f2()
	if _, _, err := a.Giro(c2); err == nil {
		t.Error("contesto finito: nessun errore")
	}
}
