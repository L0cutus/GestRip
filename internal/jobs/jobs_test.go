package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// martedì 17/11/2026, 10:00 in negozio (09:00 UTC): giorno aperto per il predefinito martedì–sabato.
var martedi = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

const (
	mittenteFinto = "negozio@esempio.it"
	chatFinta     = "-1004242"
)

func apriDB(t *testing.T) (*db.DB, *clock.Finto) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(martedi)
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	return d, o
}

func imposta(t *testing.T, d *db.DB, chiave, valore string) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, '2026-11-01T08:00:00.000000Z')
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, chiave, valore); err != nil {
		t.Fatal(err)
	}
}

// accendiEmail configura l'SMTP (dati inventati) e accende il canale (D-03).
func accendiEmail(t *testing.T, d *db.DB) {
	t.Helper()
	imposta(t, d, "outgoing.smtp.host", "smtp.esempio.it")
	imposta(t, d, "outgoing.smtp.from_address", mittenteFinto)
	imposta(t, d, uscita.ChiaveEmailAcceso, "1")
}

// accendiTelegram configura il bot (token inventato) e accende l'invio (D-03).
func accendiTelegram(t *testing.T, d *db.DB) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '4242:finto-SINTETICO', '2026-11-01T08:00:00.000000Z')
		ON CONFLICT (nome) DO UPDATE SET valore = excluded.valore`); err != nil {
		t.Fatal(err)
	}
	imposta(t, d, "outgoing.telegram.chat_id", chatFinta)
	imposta(t, d, uscita.ChiaveTelegramAcceso, "1")
}

// canaliVeri è la coda in uscita di serve, con i provider veri (nessuno va in rete per dire se è attivo).
func canaliVeri(d *db.DB, o clock.Orologio) *uscita.Coda {
	return uscita.Nuova(d, o, map[string]uscita.Provider{uscita.CanaleEmail: uscita.NuovoSMTP(d, o),
		uscita.CanaleTelegram: uscita.NuovoTelegram(d, "http://bot.invalid")})
}

type chiamata struct {
	conf   Config
	adesso time.Time
	dest   []uscita.Destinazione
	a      audit.Contesto
}

// lavoroFinto è un lavoro che registra le chiamate e risponde come gli si dice.
type lavoroFinto struct {
	mu        sync.Mutex
	chiamate  []chiamata
	risultato Risultato
	err       error
	panico    any
	// dentro, se c'è, riceve un segnale all'ingresso; lascia, se c'è, tiene il lavoro fermo finché si chiude.
	dentro         chan struct{}
	lascia         chan struct{}
	inCorso, picco int
}

func (l *lavoroFinto) Esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	l.mu.Lock()
	l.chiamate = append(l.chiamate, chiamata{conf, adesso, dest, a})
	l.inCorso++
	if l.inCorso > l.picco {
		l.picco = l.inCorso
	}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.inCorso--
		l.mu.Unlock()
	}()
	if l.dentro != nil {
		l.dentro <- struct{}{}
	}
	if l.lascia != nil {
		<-l.lascia
	}
	if l.panico != nil {
		panic(l.panico)
	}
	return l.risultato, l.err
}

func (l *lavoroFinto) quante() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.chiamate)
}

func (l *lavoroFinto) ultima(t *testing.T) chiamata {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.chiamate) == 0 {
		t.Fatal("lavoro mai chiamato")
	}
	return l.chiamate[len(l.chiamate)-1]
}

// ricevi aspetta un valore da c, al più 10 secondi (un test rosso non resta appeso).
func ricevi[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("niente dopo 10 secondi")
	}
	var zero T
	return zero
}

// manda manda v su c, al più 10 secondi.
func manda[T any](t *testing.T, c chan<- T, v T) {
	t.Helper()
	select {
	case c <- v:
	case <-time.After(10 * time.Second):
		t.Fatal("nessuno riceve dopo 10 secondi")
	}
}

func ok(n int) Risultato { return Risultato{Avvisi: n, Messaggi: n, Dettaglio: "Fatto."} }

// tre sono i tre lavori finti, registrati con i loro nomi.
func tre() (map[string]Lavoro, map[string]*lavoroFinto) {
	f := map[string]*lavoroFinto{}
	l := map[string]Lavoro{}
	for _, n := range Nomi {
		f[n] = &lavoroFinto{risultato: ok(1)}
		l[n] = f[n]
	}
	return l, f
}

func dovuto(t *testing.T, p *Pianificatore, nome string) bool {
	t.Helper()
	v, err := p.Dovuto(context.Background(), nome)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func conta(t *testing.T, d *db.DB, q string, arg ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, arg...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func righe(t *testing.T, d *db.DB) int { return conta(t, d, `SELECT count(*) FROM esecuzioni_lavori`) }

func giro(t *testing.T, p *Pianificatore) []Esecuzione {
	t.Helper()
	e, err := p.Giro(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// locale è l'istante delle ore h:m del giorno a/m/g in negozio.
func locale(a int, m time.Month, g, h, min int) time.Time {
	return time.Date(a, m, g, h, min, 0, 0, clock.Roma).UTC()
}

// inventario: JOB-02
// Le quattro domande di due?/2 sull'orologio del negozio: attivo (ritardi e solleciti accesi, briefing spento
// di default; spento con «0», «false», «False», «no», acceso il briefing solo con «1», «true», «True», «yes»),
// l'ora raggiunta (predefinite 09:00, 10:00, 08:00; «HH:MM» e «HH:MM:SS»; un valore illeggibile dà la
// predefinita), un canale, e le esecuzioni di oggi.
func TestDovutoAttivoEOra(t *testing.T) {
	d, o := apriDB(t)
	accendiTelegram(t, d)
	l, _ := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(locale(2026, 11, 17, 8, 59))
	if dovuto(t, p, Ritardi) || dovuto(t, p, Solleciti) || dovuto(t, p, Briefing) {
		t.Error("dovuto prima dell'ora")
	}
	o.Imposta(locale(2026, 11, 17, 9, 0))
	if !dovuto(t, p, Ritardi) || dovuto(t, p, Solleciti) {
		t.Error("alle 09:00 solo i ritardi")
	}
	o.Imposta(locale(2026, 11, 17, 10, 0))
	if !dovuto(t, p, Solleciti) {
		t.Error("alle 10:00 i solleciti")
	}
	if dovuto(t, p, Briefing) {
		t.Error("il briefing è spento di default")
	}
	for _, v := range []string{"0", "false", "False", "no", " no "} {
		imposta(t, d, "jobs.overdue_enabled", v)
		if dovuto(t, p, Ritardi) {
			t.Errorf("ritardi spenti con %q: dovuto", v)
		}
	}
	for _, v := range []string{"1", "si", "spento", "NO"} {
		imposta(t, d, "jobs.overdue_enabled", v)
		if !dovuto(t, p, Ritardi) {
			t.Errorf("ritardi con %q: non dovuto", v)
		}
	}
	for _, v := range []string{"1", "true", "True", "yes", " yes "} {
		imposta(t, d, "jobs.briefing_enabled", v)
		if !dovuto(t, p, Briefing) {
			t.Errorf("briefing acceso con %q: non dovuto", v)
		}
	}
	o.Imposta(locale(2026, 11, 17, 7, 59))
	if dovuto(t, p, Briefing) {
		t.Error("briefing alle 07:59: dovuto")
	}
	o.Imposta(locale(2026, 11, 17, 8, 0))
	if !dovuto(t, p, Briefing) {
		t.Error("briefing alle 08:00: non dovuto")
	}
	o.Imposta(locale(2026, 11, 17, 10, 0))
	for _, v := range []string{"0", "si", "TRUE", "acceso"} {
		imposta(t, d, "jobs.briefing_enabled", v)
		if dovuto(t, p, Briefing) {
			t.Errorf("briefing con %q: dovuto", v)
		}
	}
	// L'ora impostata, con i secondi o senza; illeggibile o vuota → la predefinita.
	o.Imposta(locale(2026, 11, 17, 7, 30))
	imposta(t, d, "jobs.overdue_hour", "07:30")
	if !dovuto(t, p, Ritardi) {
		t.Error("ora 07:30 alle 07:30: non dovuto")
	}
	imposta(t, d, "jobs.overdue_hour", "07:30:30")
	if dovuto(t, p, Ritardi) {
		t.Error("ora 07:30:30 alle 07:30:00: dovuto")
	}
	o.Sposta(30 * time.Second)
	if !dovuto(t, p, Ritardi) {
		t.Error("ora 07:30:30 alle 07:30:30: non dovuto")
	}
	for _, v := range []string{"25:00", "7:30", "sette", "  ", "07:30 "} {
		imposta(t, d, "jobs.overdue_hour", v)
		o.Imposta(locale(2026, 11, 17, 8, 59))
		if dovuto(t, p, Ritardi) {
			t.Errorf("ora %q alle 08:59: dovuto (la predefinita è 09:00)", v)
		}
		o.Imposta(locale(2026, 11, 17, 9, 0))
		if !dovuto(t, p, Ritardi) {
			t.Errorf("ora %q alle 09:00: non dovuto", v)
		}
	}
	// Un'ora tarda vale fino a mezzanotte, non oltre.
	imposta(t, d, "jobs.overdue_hour", "23:59")
	o.Imposta(locale(2026, 11, 18, 0, 30))
	if dovuto(t, p, Ritardi) {
		t.Error("ora 23:59 alle 00:30 del giorno dopo: dovuto")
	}
}

// inventario: JOB-02
// D-17: i lavori girano solo nei giorni di apertura (predefiniti martedì–sabato), alle stesse ore; il
// giorno chiuso non lascia righe; il primo giorno aperto riparte da solo. Un valore illeggibile dei giorni
// dà il predefinito. L'esecuzione a mano non guarda i giorni.
func TestDovutoGiorniDiApertura(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	l, f := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	giorni := map[int]bool{15: false, 16: false, 17: true, 18: true, 19: true, 20: true, 21: true} // dom 15 … sab 21
	for g, aperto := range giorni {
		o.Imposta(locale(2026, 11, g, 9, 0))
		if dovuto(t, p, Ritardi) != aperto {
			t.Errorf("giorno %d: dovuto %v", g, !aperto)
		}
	}
	o.Imposta(locale(2026, 11, 15, 12, 0)) // domenica
	if e := giro(t, p); len(e) != 0 || righe(t, d) != 0 || f[Ritardi].quante() != 0 {
		t.Errorf("domenica: %v, %d righe", e, righe(t, d))
	}
	// A mano sì, anche di domenica.
	if _, err := p.EseguiAMano(context.Background(), Ritardi, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil || f[Ritardi].quante() != 1 {
		t.Errorf("a mano di domenica: %v, %d chiamate", err, f[Ritardi].quante())
	}
	imposta(t, d, ChiaveGiorniApertura, "1,2,3,4,5,6,7")
	if !dovuto(t, p, Ritardi) {
		t.Error("tutti i giorni aperti: domenica non dovuto")
	}
	imposta(t, d, ChiaveGiorniApertura, "7")
	o.Imposta(locale(2026, 11, 17, 9, 0))
	if dovuto(t, p, Ritardi) {
		t.Error("aperto solo la domenica: martedì dovuto")
	}
	for _, v := range []string{"", "lunedì", "0,2", "0", "8", "2,,3", "2;3"} {
		imposta(t, d, ChiaveGiorniApertura, v)
		o.Imposta(locale(2026, 11, 15, 9, 0)) // domenica
		if dovuto(t, p, Ritardi) {
			t.Errorf("giorni %q: domenica dovuto (predefinito martedì–sabato)", v)
		}
		o.Imposta(locale(2026, 11, 16, 9, 0)) // lunedì
		if dovuto(t, p, Ritardi) {
			t.Errorf("giorni %q: lunedì dovuto (predefinito martedì–sabato)", v)
		}
		o.Imposta(locale(2026, 11, 17, 9, 0))
		if !dovuto(t, p, Ritardi) {
			t.Errorf("giorni %q: martedì non dovuto", v)
		}
	}
	imposta(t, d, ChiaveGiorniApertura, " 1 , 3 ")
	o.Imposta(locale(2026, 11, 16, 9, 0))
	if !dovuto(t, p, Ritardi) {
		t.Error("giorni « 1 , 3 »: lunedì non dovuto")
	}
}

// inventario: JOB-02
// Un'esecuzione saltata o fallita non consuma il giorno: si riprova al giro dopo, fino a 3 esecuzioni
// automatiche al giorno; una riuscita lo consuma. Le esecuzioni a mano non contano. Il giorno dopo si
// ricomincia.
func TestDovutoTentativi(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	l, f := tre()
	f[Ritardi].risultato = Risultato{Fallito: true, Dettaglio: "Non riuscito."}
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(locale(2026, 11, 17, 9, 0))
	for i := 1; i <= 3; i++ {
		if !dovuto(t, p, Ritardi) {
			t.Fatalf("tentativo %d non dovuto", i)
		}
		e := giro(t, p)
		if len(e) != 1 || e[0].Lavoro != Ritardi || e[0].Esito != EsitoFallito || e[0].Avvio != AvvioAutomatico {
			t.Fatalf("tentativo %d: %+v", i, e)
		}
		o.Sposta(time.Minute)
	}
	if dovuto(t, p, Ritardi) {
		t.Error("quarto tentativo dovuto")
	}
	if e := giro(t, p); len(e) != 0 {
		t.Errorf("quarto giro: %+v", e)
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE lavoro = 'overdue' AND esito = 'failed'`); n != 3 {
		t.Errorf("%d righe fallite, volute 3 (ognuna resta)", n)
	}
	// Le esecuzioni a mano non contano, né riuscite né fallite.
	o.Imposta(locale(2026, 11, 18, 8, 0))
	f[Ritardi].risultato = ok(2)
	for range 4 {
		if _, err := p.EseguiAMano(context.Background(), Ritardi, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
			t.Fatal(err)
		}
	}
	o.Imposta(locale(2026, 11, 18, 9, 0))
	if !dovuto(t, p, Ritardi) {
		t.Error("il giorno dopo, con quattro esecuzioni a mano: non dovuto")
	}
	// Uno fallito e uno riuscito: il giorno è fatto.
	f[Ritardi].risultato = Risultato{Fallito: true}
	giro(t, p)
	f[Ritardi].risultato = ok(0)
	o.Sposta(time.Minute)
	if e := giro(t, p); len(e) != 1 || e[0].Esito != EsitoOK {
		t.Fatalf("secondo tentativo: %+v", e)
	}
	o.Sposta(time.Minute)
	if dovuto(t, p, Ritardi) {
		t.Error("dopo un'esecuzione riuscita: dovuto")
	}
	// Mezzanotte del negozio (23:00 UTC d'inverno): giorno nuovo.
	o.Imposta(locale(2026, 11, 19, 9, 0))
	if !dovuto(t, p, Ritardi) {
		t.Error("giorno nuovo: non dovuto")
	}
}

// inventario: JOB-02
// Riavvio: lo stato sta nel database, non nel processo. Un pianificatore nuovo sullo stesso database non
// rifà il lavoro riuscito oggi e dopo due tentativi falliti ne fa uno solo. Partito dopo l'ora impostata,
// recupera subito; un giorno con l'apparecchio spento non si recupera dopo.
func TestDovutoDopoUnRiavvio(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	l, f := tre()
	f[Solleciti].risultato = Risultato{Fallito: true}
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(locale(2026, 11, 17, 10, 0))
	giro(t, p) // ritardi ok, solleciti fallito
	o.Sposta(time.Minute)
	giro(t, p) // solleciti fallito di nuovo
	// «Riavvio»: un pianificatore nuovo, con lavori nuovi.
	l2, f2 := tre()
	f2[Solleciti].risultato = Risultato{Fallito: true}
	p2 := Nuovo(d, o, canaliVeri(d, o), l2)
	o.Sposta(time.Minute)
	e := giro(t, p2)
	if len(e) != 1 || e[0].Lavoro != Solleciti || f2[Ritardi].quante() != 0 {
		t.Fatalf("dopo il riavvio: %+v, ritardi chiamati %d volte", e, f2[Ritardi].quante())
	}
	o.Sposta(time.Minute)
	if e := giro(t, p2); len(e) != 0 {
		t.Errorf("quarto tentativo dopo il riavvio: %+v", e)
	}
	// Spento mercoledì, riacceso giovedì alle 15: recupera giovedì, mercoledì non c'è.
	o.Imposta(locale(2026, 11, 19, 15, 0))
	f2[Solleciti].risultato = ok(1)
	e = giro(t, p2)
	if len(e) != 2 || e[0].Lavoro != Ritardi || e[1].Lavoro != Solleciti {
		t.Fatalf("recupero: %+v", e)
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE giorno = '2026-11-18'`); n != 0 {
		t.Errorf("%d righe del giorno spento", n)
	}
	if f[Ritardi].quante() != 1 {
		t.Errorf("ritardi del primo processo chiamati %d volte", f[Ritardi].quante())
	}
}

// inventario: JOB-01, JOB-02
// Il cambio d'ora e la mezzanotte si leggono sull'orologio del negozio, non su UTC. Il 29/03/2026 alle 02:00
// si salta alle 03:00: un lavoro alle 02:30 diventa dovuto alle 03:00 CEST (01:00 UTC). Il 25/10/2026 le
// 02:30 tornano due volte: il lavoro gira la prima e la seconda volta il giorno è già fatto. La sera tardi
// UTC è già domani in negozio. Domeniche: con tutti i giorni aperti.
func TestDovutoCambioDOra(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	imposta(t, d, ChiaveGiorniApertura, "1,2,3,4,5,6,7")
	imposta(t, d, "jobs.overdue_hour", "02:30")
	l, _ := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC)) // 01:59 CET
	if dovuto(t, p, Ritardi) {
		t.Error("01:59 CET: dovuto")
	}
	o.Imposta(time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)) // 03:00 CEST
	e := giro(t, p)
	if len(e) != 1 || e[0].Giorno != (clock.Data{Anno: 2026, Mese: 3, Giorno: 29}) {
		t.Fatalf("03:00 CEST: %+v", e)
	}
	o.Imposta(time.Date(2026, 10, 25, 0, 29, 0, 0, time.UTC)) // 02:29 CEST
	if dovuto(t, p, Ritardi) {
		t.Error("02:29 CEST: dovuto")
	}
	o.Imposta(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) // 02:30 CEST
	if e := giro(t, p); len(e) != 1 || e[0].Giorno.String() != "2026-10-25" {
		t.Fatalf("02:30 CEST: %+v", e)
	}
	o.Imposta(time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) // 02:30 CET, la seconda volta
	if dovuto(t, p, Ritardi) {
		t.Error("02:30 CET, la seconda volta: dovuto")
	}
	// 14/07 22:30 UTC = 15/07 00:30 in negozio; ora 00:15.
	imposta(t, d, "jobs.overdue_hour", "00:15")
	o.Imposta(time.Date(2026, 7, 14, 22, 30, 0, 0, time.UTC))
	if e := giro(t, p); len(e) != 1 || e[0].Giorno.String() != "2026-07-15" {
		t.Fatalf("sera tardi UTC: %+v", e)
	}
	o.Sposta(time.Minute) // ancora il 15/07 in negozio, il 14/07 in UTC: il giorno è già fatto
	if dovuto(t, p, Ritardi) {
		t.Error("sera tardi UTC, un minuto dopo: dovuto (tentativi contati sul giorno UTC)")
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE giorno = '2026-07-15' AND eseguito_il = '2026-07-14T22:30:00.000000Z'`); n != 1 {
		t.Error("giorno o istante salvati male")
	}
}

// inventario: JOB-03
// Un solo punto sceglie i destinatari: il negozio stesso su ogni canale attivo (email al mittente
// dell'SMTP, Telegram alla chat del negozio), nell'ordine email, Telegram; il briefing solo su Telegram.
// Un canale configurato ma spento (D-03) non è un destinatario. I lavori ricevono i destinatari già scelti.
func TestDestinazioni(t *testing.T) {
	d, o := apriDB(t)
	l, f := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	dest := func(nome string) []uscita.Destinazione {
		t.Helper()
		v, err := p.Destinazioni(context.Background(), nome)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	email := uscita.Destinazione{Canale: uscita.CanaleEmail, Destinatario: "shop", Indirizzo: mittenteFinto}
	tg := uscita.Destinazione{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta}
	for _, n := range Nomi {
		if v := dest(n); len(v) != 0 {
			t.Errorf("%s senza canali: %+v", n, v)
		}
	}
	accendiEmail(t, d)
	imposta(t, d, uscita.ChiaveEmailAcceso, "0")
	if v := dest(Ritardi); len(v) != 0 {
		t.Errorf("email spenta: %+v", v)
	}
	imposta(t, d, uscita.ChiaveEmailAcceso, "1")
	if v := dest(Ritardi); !reflect.DeepEqual(v, []uscita.Destinazione{email}) {
		t.Errorf("ritardi con l'email: %+v", v)
	}
	if v := dest(Solleciti); !reflect.DeepEqual(v, []uscita.Destinazione{email}) {
		t.Errorf("solleciti con l'email: %+v", v)
	}
	if v := dest(Briefing); len(v) != 0 {
		t.Errorf("briefing con la sola email: %+v", v)
	}
	accendiTelegram(t, d)
	if v := dest(Ritardi); !reflect.DeepEqual(v, []uscita.Destinazione{email, tg}) {
		t.Errorf("ritardi con i due canali: %+v", v)
	}
	if v := dest(Briefing); !reflect.DeepEqual(v, []uscita.Destinazione{tg}) {
		t.Errorf("briefing con i due canali: %+v", v)
	}
	imposta(t, d, uscita.ChiaveEmailAcceso, "0")
	if v := dest(Solleciti); !reflect.DeepEqual(v, []uscita.Destinazione{tg}) {
		t.Errorf("solleciti con il solo Telegram: %+v", v)
	}
	// Il lavoro riceve i destinatari, le impostazioni e l'istante del giro.
	imposta(t, d, uscita.ChiaveEmailAcceso, "1")
	imposta(t, d, "jobs.briefing_enabled", "1")
	o.Imposta(locale(2026, 11, 17, 10, 0))
	giro(t, p)
	if c := f[Ritardi].ultima(t); !reflect.DeepEqual(c.dest, []uscita.Destinazione{email, tg}) || !c.adesso.Equal(o.Adesso()) ||
		!c.conf.Attivo(Ritardi) || c.conf.Ora(Solleciti) != 10*time.Hour {
		t.Errorf("chiamata dei ritardi: %+v", c)
	}
	if c := f[Briefing].ultima(t); !reflect.DeepEqual(c.dest, []uscita.Destinazione{tg}) {
		t.Errorf("chiamata del briefing: %+v", c.dest)
	}
}

// inventario: JOB-03
// D-16 (solo Go): senza canali i lavori sono spenti: non sono dovuti, il giro non li esegue e non scrive
// righe «Saltato» né audit, e nemmeno a mano (ErrSenzaCanali, niente scritto). Il briefing con la sola
// email è spento allo stesso modo.
func TestCanaliSpentiNienteRighe(t *testing.T) {
	d, o := apriDB(t)
	imposta(t, d, "jobs.briefing_enabled", "1")
	l, f := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(locale(2026, 11, 17, 12, 0))
	for _, n := range Nomi {
		if dovuto(t, p, n) {
			t.Errorf("%s dovuto senza canali", n)
		}
	}
	if e := giro(t, p); len(e) != 0 {
		t.Errorf("giro senza canali: %+v", e)
	}
	for _, n := range Nomi {
		if _, err := p.EseguiAMano(context.Background(), n, audit.Contesto{Attore: "titolare@esempio.it"}); !errors.Is(err, ErrSenzaCanali) {
			t.Errorf("%s a mano senza canali: %v", n, err)
		}
		if f[n].quante() != 0 {
			t.Errorf("%s eseguito senza canali", n)
		}
	}
	accendiEmail(t, d)
	if dovuto(t, p, Briefing) {
		t.Error("briefing dovuto con la sola email")
	}
	if _, err := p.EseguiAMano(context.Background(), Briefing, audit.Contesto{Attore: "titolare@esempio.it"}); !errors.Is(err, ErrSenzaCanali) {
		t.Errorf("briefing a mano con la sola email: %v", err)
	}
	if e := giro(t, p); len(e) != 2 || f[Briefing].quante() != 0 {
		t.Errorf("giro con la sola email: %+v", e)
	}
	if n := righe(t, d); n != 2 {
		t.Errorf("%d righe, volute 2 (ritardi e solleciti)", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE esito = 'skipped' OR lavoro = 'briefing'`); n != 0 {
		t.Errorf("%d righe «Saltato» o del briefing", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.ran'`); n != 2 {
		t.Errorf("%d righe jobs.ran, volute 2", n)
	}
}

type rigaLavoro struct {
	lavoro, avvio, esito, il, giorno string
	avvisi, messaggi                 int
	dettaglio                        sql.NullString
}

func ultimaRiga(t *testing.T, d *db.DB) rigaLavoro {
	t.Helper()
	var r rigaLavoro
	if err := d.Lettura.QueryRow(`SELECT lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio FROM esecuzioni_lavori ORDER BY id DESC LIMIT 1`).
		Scan(&r.lavoro, &r.avvio, &r.esito, &r.il, &r.giorno, &r.avvisi, &r.messaggi, &r.dettaglio); err != nil {
		t.Fatal(err)
	}
	return r
}

type rigaAudit struct{ attore, risorsa, id, esito, dati string }

func ultimoAudit(t *testing.T, d *db.DB) rigaAudit {
	t.Helper()
	var r rigaAudit
	if err := d.Lettura.QueryRow(`SELECT attore, risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = 'jobs.ran' ORDER BY id DESC LIMIT 1`).
		Scan(&r.attore, &r.risorsa, &r.id, &r.esito, &r.dati); err != nil {
		t.Fatal(err)
	}
	return r
}

// inventario: JOB-04
// Ogni esecuzione scrive una riga (lavoro, avvio, esito, istante, giorno del negozio, avvisi, messaggi,
// dettaglio tagliato a 500 grafemi) e jobs.ran con esito e conteggi, firmata «scheduler» se automatica; a
// mano con chi l'ha chiesta. Il lavoro riceve lo stesso contesto. Un dettaglio vuoto è NULL. Un nuovo
// tentativo è una riga nuova.
func TestStoricoEAudit(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	accendiTelegram(t, d)
	l, f := tre()
	f[Ritardi].risultato = Risultato{Avvisi: 3, Messaggi: 6, Dettaglio: "3 buste in ritardo."}
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(time.Date(2026, 11, 17, 8, 0, 0, 123456000, time.UTC)) // 09:00 in negozio
	giro(t, p)
	r := ultimaRiga(t, d)
	if r != (rigaLavoro{Ritardi, AvvioAutomatico, EsitoOK, "2026-11-17T08:00:00.123456Z", "2026-11-17", 3, 6, sql.NullString{String: "3 buste in ritardo.", Valid: true}}) {
		t.Errorf("riga: %+v", r)
	}
	if a := ultimoAudit(t, d); a != (rigaAudit{AttoreScheduler, "job", Ritardi, EsitoOK, `{"job":"overdue","messages":6,"notices":3,"trigger":"scheduled"}`}) {
		t.Errorf("audit: %+v", a)
	}
	if c := f[Ritardi].ultima(t); c.a.Attore != AttoreScheduler {
		t.Errorf("il lavoro firma come %q", c.a.Attore)
	}
	// A mano: con il titolare, fallito, dettaglio lungo tagliato a 500 grafemi (le emoji contano una).
	f[Solleciti].risultato = Risultato{Fallito: true, Avvisi: 1, Dettaglio: strings.Repeat("🕒", 499) + "é" + "oltre"}
	titolare := audit.Contesto{Attore: "titolare@esempio.it", IP: "127.0.0.1"}
	e, err := p.EseguiAMano(context.Background(), Solleciti, titolare)
	if err != nil {
		t.Fatal(err)
	}
	if e.Avvio != AvvioAMano || e.Esito != EsitoFallito || e.Dettaglio != strings.Repeat("🕒", 499)+"é" || e.ID == 0 {
		t.Errorf("esecuzione a mano: %+v", e)
	}
	if a := ultimoAudit(t, d); a.attore != "titolare@esempio.it" || a.esito != EsitoFallito || a.id != Solleciti {
		t.Errorf("audit a mano: %+v", a)
	}
	if c := f[Solleciti].ultima(t); c.a != titolare {
		t.Errorf("il lavoro a mano firma come %+v", c.a)
	}
	f[Briefing].risultato = Risultato{Avvisi: -1, Messaggi: -3}
	imposta(t, d, "jobs.briefing_enabled", "1")
	if _, err := p.EseguiAMano(context.Background(), Briefing, titolare); err != nil {
		t.Fatal(err)
	}
	if r := ultimaRiga(t, d); r.dettaglio.Valid || r.avvisi != 0 || r.messaggi != 0 || r.esito != EsitoOK {
		t.Errorf("riga senza dettaglio e con conteggi negativi: %+v", r)
	}
	if a := ultimoAudit(t, d); a.dati != `{"job":"briefing","messages":0,"notices":0,"trigger":"manual"}` {
		t.Errorf("audit con conteggi negativi: %+v", a)
	}
}

// inventario: JOB-04
// Un lavoro spento dal titolare ed eseguito a mano è «Saltato» con «Job disattivato dal titolare.», senza
// chiamare il lavoro, e non conta come esecuzione del giorno. Un nome che non è un lavoro registrato non si
// esegue: né a mano (ErrLavoroSconosciuto) né dal giro, e non lascia righe.
func TestSpentoASconosciuto(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	imposta(t, d, "jobs.overdue_enabled", "no")
	f := &lavoroFinto{risultato: ok(1)}
	p := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Ritardi: f, "pulizia": f})
	o.Imposta(locale(2026, 11, 17, 11, 0))
	e, err := p.EseguiAMano(context.Background(), Ritardi, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || e.Esito != EsitoSaltato || e.Dettaglio != DettaglioSpento || e.Avvio != AvvioAMano || f.quante() != 0 {
		t.Errorf("spento a mano: %+v %v, %d chiamate", e, err, f.quante())
	}
	if a := ultimoAudit(t, d); a.esito != EsitoSaltato || a.dati != `{"job":"overdue","messages":0,"notices":0,"trigger":"manual"}` {
		t.Errorf("audit dello spento: %+v", a)
	}
	imposta(t, d, "jobs.overdue_enabled", "1")
	if !dovuto(t, p, Ritardi) {
		t.Error("dopo uno «Saltato» a mano: non dovuto")
	}
	for _, n := range []string{"pulizia", Solleciti, Briefing, ""} {
		if _, err := p.EseguiAMano(context.Background(), n, audit.Contesto{Attore: "titolare@esempio.it"}); !errors.Is(err, ErrLavoroSconosciuto) {
			t.Errorf("%q a mano: %v", n, err)
		}
		if dovuto(t, p, n) {
			t.Errorf("%q dovuto", n)
		}
	}
	if es := giro(t, p); len(es) != 1 || es[0].Lavoro != Ritardi {
		t.Errorf("giro: %+v", es)
	}
	if n := righe(t, d); n != 2 {
		t.Errorf("%d righe, volute 2", n)
	}
}

// inventario: JOB-04
// Un lavoro che va in panico o dà un errore è «Fallito» con «Il job si è interrotto. Il dettaglio è nel log
// dell'applicazione.», zero avvisi e messaggi: mai il testo dell'errore, né nella riga né nel registro (solo il
// tipo). Il giro continua con gli altri lavori.
func TestLavoroInterrotto(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	l, f := tre()
	segreto := "mario.rossi@esempio.it"
	f[Ritardi].panico = errors.New("busta di " + segreto)
	f[Ritardi].risultato = ok(5)
	f[Solleciti].err = errors.New("cliente " + segreto)
	f[Solleciti].risultato = ok(4)
	var buf bytes.Buffer
	p := Nuovo(d, o, canaliVeri(d, o), l).ConRegistro(slog.New(slog.NewTextHandler(&buf, nil)))
	o.Imposta(locale(2026, 11, 17, 10, 0))
	e := giro(t, p)
	if len(e) != 2 {
		t.Fatalf("giro: %+v", e)
	}
	for _, x := range e {
		if x.Esito != EsitoFallito || x.Dettaglio != DettaglioInterrotto || x.Avvisi != 0 || x.Messaggi != 0 {
			t.Errorf("%s: %+v", x.Lavoro, x)
		}
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE dettaglio = ? AND esito = 'failed'`, DettaglioInterrotto); n != 2 {
		t.Errorf("%d righe interrotte", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%esempio%'`); n != 0 {
		t.Error("il testo dell'errore è nell'audit")
	}
	if strings.Contains(buf.String(), segreto) || !strings.Contains(buf.String(), "overdue") || !strings.Contains(buf.String(), "chases") {
		t.Errorf("registro: %s", buf.String())
	}
	// Il panico con lo stack (ACC-19), l'errore con il solo tipo.
	for _, riga := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		switch {
		case strings.Contains(riga, "nome=overdue") && !strings.Contains(riga, "stack="):
			t.Errorf("panico senza lo stack: %s", riga)
		case strings.Contains(riga, "nome=chases") && strings.Contains(riga, "stack="):
			t.Errorf("errore con lo stack: %s", riga)
		}
	}
	// Un panico che non è un errore (un valore qualunque) è lo stesso.
	f[Ritardi].panico = segreto
	o.Sposta(time.Minute)
	if e := giro(t, p); len(e) != 2 || e[0].Dettaglio != DettaglioInterrotto {
		t.Errorf("secondo giro: %+v", e)
	}
	if strings.Contains(buf.String(), segreto) {
		t.Errorf("registro: %s", buf.String())
	}
}

// inventario: JOB-02
// Mai due esecuzioni insieme: mentre un lavoro gira, un altro giro e un'esecuzione a mano aspettano il loro
// turno; finito il primo, il secondo giro trova il giorno fatto e non rifà niente.
func TestMaiDueEsecuzioniInsieme(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	f := &lavoroFinto{risultato: ok(1), dentro: make(chan struct{}, 10), lascia: make(chan struct{})}
	p := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Ritardi: f})
	o.Imposta(locale(2026, 11, 17, 9, 0))
	var wg sync.WaitGroup
	esiti := make(chan []Esecuzione, 3)
	wg.Go(func() {
		e, err := p.Giro(context.Background())
		if err != nil {
			t.Error(err)
		}
		esiti <- e
	})
	ricevi(t, f.dentro)
	for range 2 {
		wg.Go(func() {
			e, err := p.Giro(context.Background())
			if err != nil {
				t.Error(err)
			}
			esiti <- e
		})
	}
	wg.Go(func() {
		if _, err := p.EseguiAMano(context.Background(), Ritardi, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
			t.Error(err)
		}
	})
	select {
	case <-f.dentro:
		t.Fatal("due esecuzioni insieme")
	case <-time.After(200 * time.Millisecond):
	}
	close(f.lascia)
	wg.Wait()
	close(esiti)
	automatiche := 0
	for e := range esiti {
		automatiche += len(e)
	}
	if automatiche != 1 || f.picco != 1 || f.quante() != 2 {
		t.Errorf("automatiche %d, picco %d, chiamate %d", automatiche, f.picco, f.quante())
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE avvio = 'scheduled'`); n != 1 {
		t.Errorf("%d righe automatiche", n)
	}
}

// aspettaFinta è un'attesa che si sblocca a mano: ogni valore ricevuto da avanti è un giro; chiusa o con il
// contesto finito, smette.
type aspettaFinta struct {
	avanti   chan bool
	chiamate int
	mu       sync.Mutex
}

func (a *aspettaFinta) aspetta(ctx context.Context) bool {
	a.mu.Lock()
	a.chiamate++
	a.mu.Unlock()
	select {
	case v, ok := <-a.avanti:
		return ok && v
	case <-ctx.Done():
		return false
	}
}

// canaliCheCadono vanno in panico al primo giro e poi rispondono come la coda.
type canaliCheCadono struct {
	mu     sync.Mutex
	volte  int
	dentro Canali
}

func (c *canaliCheCadono) CanaleAttivo(ctx context.Context, canale string) (bool, error) {
	c.mu.Lock()
	c.volte++
	v := c.volte
	c.mu.Unlock()
	if v == 1 {
		panic("canali caduti")
	}
	return c.dentro.CanaleAttivo(ctx, canale)
}

// inventario: JOB-02
// Il lavoro di serve: prima aspetta (il primo giro dopo un minuto, come lo standalone), poi un giro dopo
// ogni attesa; un giro che va in panico non lo ferma; con il contesto finito torna (niente goroutine
// orfane) e un giro non parte più.
func TestLavoroSiFerma(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	f := &lavoroFinto{risultato: ok(1)}
	var buf bytes.Buffer
	cc := &canaliCheCadono{dentro: canaliVeri(d, o)}
	p := Nuovo(d, o, cc, map[string]Lavoro{Ritardi: f}).ConRegistro(slog.New(slog.NewTextHandler(&buf, nil)))
	o.Imposta(locale(2026, 11, 17, 9, 0))
	ctx, ferma := context.WithCancel(context.Background())
	a := &aspettaFinta{avanti: make(chan bool)}
	finito := make(chan struct{})
	go func() {
		p.Lavoro(ctx, a.aspetta)
		close(finito)
	}()
	manda(t, a.avanti, true) // primo giro: i canali cadono
	manda(t, a.avanti, true) // secondo giro: esegue
	manda(t, a.avanti, true) // terzo giro: già fatto
	if f.quante() != 1 || righe(t, d) != 1 {
		t.Errorf("dopo tre giri: %d chiamate, %d righe", f.quante(), righe(t, d))
	}
	if !strings.Contains(buf.String(), "panico") || !strings.Contains(buf.String(), "stack=") || strings.Contains(buf.String(), "canali caduti") {
		t.Errorf("registro del panico: %s", buf.String())
	}
	ferma()
	select {
	case <-finito:
	case <-time.After(5 * time.Second):
		t.Fatal("il lavoro non si ferma")
	}
	if a.chiamate != 4 {
		t.Errorf("%d attese, volute 4", a.chiamate)
	}
	// Prima l'attesa, poi il giro: un'attesa che dice di smettere subito non lascia fare niente.
	f2 := &lavoroFinto{risultato: ok(1)}
	p2 := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Solleciti: f2})
	o.Imposta(locale(2026, 11, 17, 11, 0))
	p2.Lavoro(context.Background(), func(context.Context) bool { return false })
	ctx2, ferma2 := context.WithCancel(context.Background())
	ferma2()
	p2.Lavoro(ctx2, (&aspettaFinta{avanti: make(chan bool)}).aspetta)
	if f2.quante() != 0 {
		t.Error("un giro partito prima della prima attesa o con il contesto già finito")
	}
}

// inventario: JOB-02
// Lo stop mentre un lavoro gira: se il lavoro torna con l'errore dello stop la riga non si scrive (il giorno
// resta da fare al prossimo avvio, come lo standalone fermato a metà); se torna con un risultato, riuscito o
// fallito, la riga si scrive lo stesso con i suoi conteggi (il lavoro ha già fatto qualcosa).
func TestArrestoDuranteUnLavoro(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	f := &lavoroFinto{err: context.Canceled, dentro: make(chan struct{}, 1), lascia: make(chan struct{})}
	p := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Ritardi: f})
	o.Imposta(locale(2026, 11, 17, 9, 0))
	ctx, ferma := context.WithCancel(context.Background())
	fatto := make(chan error, 1)
	go func() {
		_, err := p.Giro(ctx)
		fatto <- err
	}()
	ricevi(t, f.dentro)
	ferma()
	close(f.lascia)
	if err := ricevi(t, fatto); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n := righe(t, d); n != 0 {
		t.Errorf("%d righe dopo lo stop con errore", n)
	}
	if !dovuto(t, p, Ritardi) {
		t.Error("dopo lo stop: non più dovuto")
	}
	f2 := &lavoroFinto{risultato: ok(2), dentro: make(chan struct{}, 1), lascia: make(chan struct{})}
	p2 := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Ritardi: f2})
	ctx2, ferma2 := context.WithCancel(context.Background())
	go func() {
		_, err := p2.Giro(ctx2)
		fatto <- err
	}()
	ricevi(t, f2.dentro)
	ferma2()
	close(f2.lascia)
	if err := ricevi(t, fatto); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r := ultimaRiga(t, d); r.esito != EsitoOK || r.avvisi != 2 {
		t.Errorf("riga dopo lo stop riuscito: %+v", r)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.ran'`); n != 1 {
		t.Errorf("%d jobs.ran", n)
	}
	f3 := &lavoroFinto{risultato: Risultato{Fallito: true, Avvisi: 1, Messaggi: 1, Dettaglio: "1 busta, poi un guasto."},
		dentro: make(chan struct{}, 1), lascia: make(chan struct{})}
	p3 := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Solleciti: f3})
	o.Imposta(locale(2026, 11, 17, 10, 0))
	ctx3, ferma3 := context.WithCancel(context.Background())
	go func() {
		_, err := p3.Giro(ctx3)
		fatto <- err
	}()
	ricevi(t, f3.dentro)
	ferma3()
	close(f3.lascia)
	if err := ricevi(t, fatto); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if r := ultimaRiga(t, d); r.lavoro != Solleciti || r.esito != EsitoFallito || r.avvisi != 1 || r.messaggi != 1 ||
		r.dettaglio.String != "1 busta, poi un guasto." {
		t.Errorf("riga dopo lo stop con un risultato fallito: %+v", r)
	}
}

// inventario: JOB-04
// Lo storico per la pagina: le ultime righe, la più recente prima (per id, anche con lo stesso istante), e
// l'ultima esecuzione di ogni lavoro; un lavoro mai eseguito non c'è. Le righe importate si leggono uguali.
func TestStoricoEUltime(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	accendiTelegram(t, d)
	imposta(t, d, "jobs.briefing_enabled", "1")
	if _, err := d.Scrittura.Exec(`INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
		VALUES ('briefing', 'scheduled', 'skipped', '2026-10-30T07:00:00.000000Z', '2026-10-30', 0, 0, 'Nessun bot Telegram configurato.')`); err != nil {
		t.Fatal(err)
	}
	l, _ := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	o.Imposta(locale(2026, 11, 17, 10, 0))
	giro(t, p) // ritardi, solleciti, briefing nello stesso istante
	if _, err := p.EseguiAMano(context.Background(), Ritardi, audit.Contesto{Attore: "titolare@esempio.it"}); err != nil {
		t.Fatal(err)
	}
	s, err := p.Storico(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0].Avvio != AvvioAMano || s[1].Lavoro != Briefing || s[2].Lavoro != Solleciti || !s[1].EseguitoIl.Equal(o.Adesso()) {
		t.Errorf("storico: %+v", s)
	}
	u, err := p.Ultime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 3 || u[Ritardi].Avvio != AvvioAMano || u[Briefing].ID != s[1].ID {
		t.Errorf("ultime: %+v", u)
	}
	tutto, err := p.Storico(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tutto) != 5 || tutto[4].Dettaglio != "Nessun bot Telegram configurato." || tutto[4].Giorno.String() != "2026-10-30" {
		t.Errorf("riga importata: %+v", tutto[4])
	}
}

// inventario: JOB-04
// L'audit del giro e la riga si scrivono insieme: se l'audit non si può scrivere, niente riga (e il lavoro
// resta dovuto); il giro dà l'errore.
func TestRigaEAuditInsieme(t *testing.T) {
	d, o := apriDB(t)
	accendiEmail(t, d)
	f := &lavoroFinto{risultato: ok(1)}
	p := Nuovo(d, o, canaliVeri(d, o), map[string]Lavoro{Ritardi: f})
	o.Imposta(locale(2026, 11, 17, 9, 0))
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'jobs.ran' BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Giro(context.Background()); err == nil {
		t.Error("giro senza errore con l'audit rifiutato")
	}
	if righe(t, d) != 0 || !dovuto(t, p, Ritardi) {
		t.Error("riga scritta senza audit")
	}
}
