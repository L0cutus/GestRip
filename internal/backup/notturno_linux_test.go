package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// Il backup notturno e «Esegui adesso» (slice 124, risposta C, D-115). Orologio finto, database e media
// inventati della prova, chiavetta finta della scena della 123 (niente montaggi veri); il backup, la verifica,
// il registro e il lucchetto sono veri dove non si dice altro.

// provaNotturno è un backup notturno configurato sulla chiavetta della scena, con la chiave dei backup vera.
type provaNotturno struct {
	p *prova
	s *scena
	o *clock.Finto
	n *Notturno
}

func nuovaProvaNotturno(t *testing.T, adesso time.Time) *provaNotturno {
	t.Helper()
	p := nuovaProva(t)
	conChiave(t, p)
	s := nuovaScena(t, "chiavetta")
	dati, err := percorsoVero(p.dir)
	if err != nil {
		t.Fatal(err)
	}
	s.sis.dev[dati], s.sis.dev[p.dir] = 1, 1
	o := clock.NuovoFinto(adesso)
	p.s.Orologio = o
	return &provaNotturno{p: p, s: s, o: o, n: NuovoNotturno(p.s.DB, p.s.Archivio, s.chiavetta, o)}
}

// righeStorico dà le righe dello storico, dalla prima: avvio, turno, esito.
func (pn *provaNotturno) righeStorico(t *testing.T) []string {
	t.Helper()
	es, err := pn.n.Storico(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var v []string
	for i := len(es) - 1; i >= 0; i-- {
		v = append(v, es[i].Avvio+" "+es[i].Turno.String()+" "+es[i].Esito)
	}
	return v
}

// riga scrive a mano una riga dello storico (per le prove del calendario, senza backup veri).
func riga(t *testing.T, d *db.DB, avvio, turno, esito string, fine time.Time) {
	t.Helper()
	var errore any
	if esito != EsitoOK {
		errore = "motivo inventato"
	}
	if _, err := d.Scrittura.ExecContext(context.Background(), `INSERT INTO esecuzioni_backup
		(avvio, turno, iniziato_il, finito_il, esito, errore) VALUES (?, ?, ?, ?, ?, ?)`, avvio, turno,
		fine.Add(-time.Minute).UTC().Format(db.FormatoIstante), fine.UTC().Format(db.FormatoIstante), esito, errore); err != nil {
		t.Fatal(err)
	}
}

func dovutoAlle(t *testing.T, n *Notturno, o *clock.Finto, at time.Time) bool {
	t.Helper()
	o.Imposta(at)
	v, err := n.Dovuto(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Il turno è il giorno del negozio dell'ultima ora del backup passata, sull'ora del muro di Roma: alle 02:29 è
// ancora ieri, alle 02:30 oggi; con il salto delle 02:00 (29/03/2026) si passa alle 03:00; le 02:30 che vengono
// due volte (25/10/2026) sono lo stesso turno; il giorno UTC diverso da quello di Roma non conta.
func TestTurno(t *testing.T) {
	casi := []struct {
		nome   string
		at     time.Time
		ora    time.Duration
		atteso string
	}{
		{"02:29", alle(2026, 11, 24, 2, 29), OraPredefinita, "2026-11-23"},
		{"02:30", alle(2026, 11, 24, 2, 30), OraPredefinita, "2026-11-24"},
		{"23:59", alle(2026, 11, 24, 23, 59), OraPredefinita, "2026-11-24"},
		{"00:00", alle(2026, 11, 25, 0, 0), OraPredefinita, "2026-11-24"},
		{"primo dell'anno 01:00", alle(2027, 1, 1, 1, 0), OraPredefinita, "2026-12-31"},
		{"01:59 CET prima del salto", time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC), OraPredefinita, "2026-03-28"},
		{"03:00 CEST dopo il salto", time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC), OraPredefinita, "2026-03-29"},
		{"02:29 CEST", time.Date(2026, 10, 25, 0, 29, 0, 0, time.UTC), OraPredefinita, "2026-10-24"},
		{"02:30 CEST", time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), OraPredefinita, "2026-10-25"},
		{"02:30 CET, la seconda volta", time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC), OraPredefinita, "2026-10-25"},
		// Nell'ora ripetuta, prima della seconda 02:30, l'ora del muro torna sotto le 02:30 e il turno torna quello di
		// ieri (come il confronto della 094): ieri è già fatto o ha i suoi tentativi, quindi niente backup doppio.
		{"02:29 CET, la seconda volta", time.Date(2026, 10, 25, 1, 29, 0, 0, time.UTC), OraPredefinita, "2026-10-24"},
		{"sera tardi UTC", time.Date(2026, 7, 14, 22, 30, 0, 0, time.UTC), 15 * time.Minute, "2026-07-15"},
		{"ora 00:00", alle(2026, 11, 24, 0, 0), 0, "2026-11-24"},
	}
	for _, c := range casi {
		if g := Turno(c.at, c.ora).String(); g != c.atteso {
			t.Errorf("%s: %s, atteso %s", c.nome, g, c.atteso)
		}
	}
}

// L'ora del backup: senza riga le 02:30; «HH:MM» o «HH:MM:SS»; ogni altra forma vale la predefinita.
func TestOraBackup(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 12, 0))
	ctx := context.Background()
	if o, err := pn.n.Ora(ctx); err != nil || o != OraPredefinita {
		t.Fatalf("senza riga: %v %v", o, err)
	}
	for v, atteso := range map[string]time.Duration{
		"03:15": 3*time.Hour + 15*time.Minute, "00:00": 0, "23:59:59": 24*time.Hour - time.Second, " 04:05 ": 4*time.Hour + 5*time.Minute,
		"3:15": OraPredefinita, "24:00": OraPredefinita, "02:60": OraPredefinita, "": OraPredefinita, "02": OraPredefinita,
		"02:30:00:00": OraPredefinita, "-1:30": OraPredefinita, "ab:cd": OraPredefinita, "+2:30": OraPredefinita,
	} {
		if _, err := pn.p.s.DB.Scrittura.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il)
			VALUES (?, ?, '2026-11-01T08:00:00.000000Z') ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, ChiaveOra, v); err != nil {
			t.Fatal(err)
		}
		if o, err := pn.n.Ora(ctx); err != nil || o != atteso {
			t.Errorf("%q: %v %v, atteso %v", v, o, err, atteso)
		}
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Il calendario, con lo storico scritto a mano (nessun backup vero): alle 02:30 di Roma il turno di oggi è da
// fare, alle 02:29 no se ieri è fatto; i giorni di chiusura contano come gli altri (D-115); le esecuzioni a mano
// non contano; un turno «avviso» è fatto; tre tentativi falliti bastano, a distanza di almeno 30 minuti (con
// l'orologio tornato indietro si riprova); senza chiavetta mai.
func TestNotturnoDovuto(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 0))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	// Martedì 24/11/2026: ieri (lunedì) fatto.
	riga(t, d, AvvioAutomatico, "2026-11-23", EsitoOK, alle(2026, 11, 23, 2, 35))
	if dovutoAlle(t, n, o, alle(2026, 11, 24, 2, 29)) {
		t.Error("02:29: dovuto")
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 24, 2, 30)) {
		t.Error("02:30: non dovuto")
	}
	// Il negozio aperto solo il mercoledì: domenica e lunedì il backup si fa lo stesso (D-115).
	if _, err := d.Scrittura.ExecContext(context.Background(), `INSERT INTO impostazioni (chiave, valore, aggiornata_il)
		VALUES ('jobs.giorni_apertura', '3', '2026-11-01T08:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 22, 2, 30)) || !dovutoAlle(t, n, o, alle(2026, 11, 23, 0, 30)) {
		t.Error("giorno di chiusura: non dovuto")
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 29, 2, 30)) {
		t.Error("domenica: non dovuto")
	}
	// A mano non conta per il turno.
	riga(t, d, AvvioAMano, "2026-11-24", EsitoOK, alle(2026, 11, 24, 2, 31))
	if !dovutoAlle(t, n, o, alle(2026, 11, 24, 3, 0)) {
		t.Error("dopo un backup a mano: non dovuto")
	}
	// Tentativi falliti: dopo uno, 30 minuti di attesa; al terzo basta.
	riga(t, d, AvvioAutomatico, "2026-11-24", EsitoFallito, alle(2026, 11, 24, 3, 0))
	if dovutoAlle(t, n, o, alle(2026, 11, 24, 3, 29)) {
		t.Error("29 minuti dopo un fallimento: dovuto")
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 24, 3, 30)) {
		t.Error("30 minuti dopo un fallimento: non dovuto")
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 24, 2, 59)) {
		t.Error("orologio tornato indietro: non dovuto")
	}
	riga(t, d, AvvioAutomatico, "2026-11-24", EsitoFallito, alle(2026, 11, 24, 3, 30))
	if !dovutoAlle(t, n, o, alle(2026, 11, 24, 4, 0)) {
		t.Error("due fallimenti: non dovuto")
	}
	riga(t, d, AvvioAutomatico, "2026-11-24", EsitoFallito, alle(2026, 11, 24, 4, 0))
	if dovutoAlle(t, n, o, alle(2026, 11, 24, 12, 0)) {
		t.Error("tre fallimenti: dovuto")
	}
	if !dovutoAlle(t, n, o, alle(2026, 11, 25, 2, 30)) {
		t.Error("il turno dopo tre fallimenti: non dovuto")
	}
	// Un backup buono con la rotazione non riuscita: il turno è fatto.
	riga(t, d, AvvioAutomatico, "2026-11-25", EsitoAvviso, alle(2026, 11, 25, 2, 35))
	if dovutoAlle(t, n, o, alle(2026, 11, 25, 12, 0)) {
		t.Error("dopo un avviso: dovuto")
	}
	// Senza chiavetta mai, e senza righe nello storico.
	senza := NuovoNotturno(d, pn.p.s.Archivio, "", o)
	if dovutoAlle(t, senza, o, alle(2026, 11, 26, 2, 30)) {
		t.Error("senza chiavetta: dovuto")
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Il cambio dell'ora come la 094: il 29/03 (le 02:00 diventano 03:00) il backup delle 02:30 è dovuto alle
// 03:00 CEST e non alle 01:59 CET; il 25/10 (le 02:30 vengono due volte) è dovuto alla prima e non alla seconda.
func TestNotturnoCambioDOra(t *testing.T) {
	pn := nuovaProvaNotturno(t, time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC))
	d, n, o := pn.p.s.DB, pn.n, pn.o
	riga(t, d, AvvioAutomatico, "2026-03-28", EsitoOK, time.Date(2026, 3, 28, 1, 35, 0, 0, time.UTC))
	if dovutoAlle(t, n, o, time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC)) {
		t.Error("01:59 CET: dovuto")
	}
	if !dovutoAlle(t, n, o, time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)) {
		t.Error("03:00 CEST: non dovuto")
	}
	riga(t, d, AvvioAutomatico, "2026-10-24", EsitoOK, time.Date(2026, 10, 24, 0, 35, 0, 0, time.UTC))
	if dovutoAlle(t, n, o, time.Date(2026, 10, 25, 0, 29, 0, 0, time.UTC)) {
		t.Error("02:29 CEST: dovuto")
	}
	if !dovutoAlle(t, n, o, time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)) {
		t.Error("02:30 CEST: non dovuto")
	}
	riga(t, d, AvvioAutomatico, "2026-10-25", EsitoOK, time.Date(2026, 10, 25, 0, 35, 0, 0, time.UTC))
	if dovutoAlle(t, n, o, time.Date(2026, 10, 25, 1, 30, 0, 0, time.UTC)) {
		t.Error("02:30 CET, la seconda volta: dovuto")
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Il giro vero alle 02:30 di Roma: archivio sulla chiavetta, verifica, riga «chiavetta» nel registro e riga «ok»
// nello storico; prima del backup toglie le cartelle temporanee rimaste. Un minuto dopo niente; la notte dopo un
// altro, anche se è un giorno di chiusura.
func TestNotturnoGiroVero(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 23, 2, 30)) // lunedì, chiuso di default
	ctx := context.Background()
	resto := filepath.Join(pn.p.dir, ".backup-99")
	if err := os.Mkdir(resto, 0o700); err != nil {
		t.Fatal(err)
	}
	riga(t, pn.p.s.DB, AvvioAutomatico, "2026-11-22", EsitoOK, alle(2026, 11, 22, 2, 35))
	e, fatto, err := pn.n.Giro(ctx)
	if err != nil || !fatto || e.Esito != EsitoOK || e.Turno.String() != "2026-11-23" || filepath.Dir(e.Archivio) != pn.s.chiavetta {
		t.Fatalf("giro: %+v %v %v", e, fatto, err)
	}
	if _, err := os.Lstat(resto); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cartella temporanea rimasta: %v", err)
	}
	u, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, pn.p.s.DB.Lettura)
	if err != nil || !ok || u.Archivio != e.Archivio {
		t.Errorf("registro: %+v %v %v", u, ok, err)
	}
	pn.o.Sposta(time.Minute)
	if _, fatto, err := pn.n.Giro(ctx); fatto || err != nil {
		t.Errorf("un minuto dopo: %v %v", fatto, err)
	}
	pn.o.Imposta(alle(2026, 11, 24, 2, 30))
	if _, fatto, err := pn.n.Giro(ctx); !fatto || err != nil {
		t.Errorf("la notte dopo: %v %v", fatto, err)
	}
	if v := strings.Join(pn.righeStorico(t), ","); v != "automatico 2026-11-22 ok,automatico 2026-11-23 ok,automatico 2026-11-24 ok" {
		t.Errorf("storico %s", v)
	}
	if v := voceDi(t, pn.s.chiavetta); len(v) != 2 {
		t.Errorf("sulla chiavetta %v", v)
	}
	// Il lucchetto è libero.
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto dopo il giro: %v", err)
	}
	_ = l.Rilascia()
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Un backup che fallisce (la chiavetta non montata) lascia una riga «fallito» con il motivo e niente archivio; al
// terzo tentativo del turno il lavoro smette fino al turno dopo.
func TestNotturnoGiroFallito(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	pn.s.sis.info = rigaRadice() // la chiavetta non è più nella tabella dei montaggi
	ctx := context.Background()
	for i := range 3 {
		e, fatto, err := pn.n.Giro(ctx)
		if !fatto || !errors.Is(err, ErrNonMontata) || e.Esito != EsitoFallito || !strings.Contains(e.Errore, "punto di montaggio") || e.Archivio != "" {
			t.Fatalf("tentativo %d: %+v %v %v", i, e, fatto, err)
		}
		pn.o.Sposta(AttesaFraTentativi)
	}
	if _, fatto, err := pn.n.Giro(ctx); fatto || err != nil {
		t.Errorf("quarto tentativo: %v %v", fatto, err)
	}
	if v := voceDi(t, pn.s.chiavetta); len(v) != 0 {
		t.Errorf("sulla chiavetta %v", v)
	}
}

// aspettaFinta registra le attese chieste dal lavoro e lascia fare giri finché non ne ha fatti quanti.
type aspettaFinta struct {
	mu      sync.Mutex
	chieste []time.Duration
	giri    int
}

func (a *aspettaFinta) aspetta(ctx context.Context, d time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.chieste = append(a.chieste, d)
	return len(a.chieste) <= a.giri && ctx.Err() == nil
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Il recupero: l'apparecchio spento alle 02:30 (e per tutto il giorno prima) si riaccende alle 15:00; il lavoro
// aspetta 10 minuti dall'avvio, poi fa il backup del turno di oggi; i giri dopo ogni minuto non rifanno niente.
// Senza chiavetta il lavoro esce subito senza aspettare.
func TestNotturnoRecupero(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 15, 0))
	riga(t, pn.p.s.DB, AvvioAutomatico, "2026-11-22", EsitoOK, alle(2026, 11, 22, 2, 35))
	a := &aspettaFinta{giri: 3}
	pn.n.Lavoro(context.Background(), a.aspetta)
	if len(a.chieste) != 4 || a.chieste[0] != AttesaDopoAvvio || a.chieste[1] != OgniQuantoNotturno {
		t.Errorf("attese %v", a.chieste)
	}
	if v := strings.Join(pn.righeStorico(t), ","); v != "automatico 2026-11-22 ok,automatico 2026-11-24 ok" {
		t.Errorf("storico %s", v)
	}
	// Alle 01:00 del giorno dopo il turno è ancora quello del 24, già fatto.
	pn.o.Imposta(alle(2026, 11, 25, 1, 0))
	if ok, err := pn.n.Dovuto(context.Background()); ok || err != nil {
		t.Errorf("alle 01:00: %v %v", ok, err)
	}
	senza := NuovoNotturno(pn.p.s.DB, pn.p.s.Archivio, "", pn.o)
	b := &aspettaFinta{giri: 3}
	senza.Lavoro(context.Background(), b.aspetta)
	if len(b.chieste) != 0 {
		t.Errorf("senza chiavetta: attese %v", b.chieste)
	}
}

// sottofondoProva fa partire i lavori di «Esegui adesso» in una goroutine che il test aspetta.
type sottofondoProva struct {
	wg    sync.WaitGroup
	fermo bool
}

func (s *sottofondoProva) lancia(f func()) bool {
	if s.fermo {
		return false
	}
	s.wg.Go(f)
	return true
}

// verificaFerma è una verifica che si ferma finché il test non la lascia andare (o finché il ctx non finisce),
// poi fa la verifica vera.
type verificaFerma struct {
	partita, via chan struct{}
}

func nuovaVerificaFerma() *verificaFerma {
	return &verificaFerma{partita: make(chan struct{}), via: make(chan struct{})}
}

// aspettaPartita aspetta che il backup sia arrivato alla verifica (al più 20 secondi).
func (v *verificaFerma) aspettaPartita(t *testing.T) {
	t.Helper()
	select {
	case <-v.partita:
	case <-time.After(20 * time.Second):
		t.Fatal("il backup non è arrivato alla verifica")
	}
}

func (v *verificaFerma) verifica(ctx context.Context, dati, file string) (Verificato, error) {
	close(v.partita)
	select {
	case <-v.via:
	case <-ctx.Done():
		return Verificato{}, ctx.Err()
	}
	return VerificaArchivio(ctx, dati, file)
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// «Esegui adesso» parte in sottofondo e scrive una riga «a_mano»; mentre gira, un secondo «Esegui adesso», il giro
// automatico dovuto e il comando (il lucchetto) rifiutano o aspettano senza scrivere righe; finito, il giro
// automatico fa il suo turno (a mano non conta). Senza chiavetta ErrNonConfigurato; con serve che si ferma
// ErrFermato e il lucchetto resta libero.
func TestEseguiAdesso(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx := context.Background()
	sf := &sottofondoProva{}
	pn.n.ConSottofondo(ctx, sf.lancia)
	vf := nuovaVerificaFerma()
	pn.n.verifica = vf.verifica
	if err := pn.n.EseguiAdesso(); err != nil {
		t.Fatal(err)
	}
	vf.aspettaPartita(t)
	if err := pn.n.EseguiAdesso(); !errors.Is(err, ErrBackupInCorso) {
		t.Errorf("secondo «Esegui adesso»: %v", err)
	}
	if _, fatto, err := pn.n.Giro(ctx); fatto || err != nil {
		t.Errorf("giro automatico durante il backup a mano: %v %v", fatto, err)
	}
	if _, err := PrendiLucchetto(pn.p.dir); !errors.Is(err, ErrBackupInCorso) {
		t.Errorf("il comando durante il backup a mano: %v", err)
	}
	if v := pn.righeStorico(t); len(v) != 0 {
		t.Errorf("righe durante il backup: %v", v)
	}
	close(vf.via)
	sf.wg.Wait()
	pn.n.verifica = nil
	pn.o.Sposta(time.Minute) // un archivio con lo stesso nome al secondo non si sovrascrive
	if _, fatto, err := pn.n.Giro(ctx); !fatto || err != nil {
		t.Errorf("giro automatico dopo: %v %v", fatto, err)
	}
	if v := strings.Join(pn.righeStorico(t), ","); v != "a_mano 2026-11-24 ok,automatico 2026-11-24 ok" {
		t.Errorf("storico %s", v)
	}
	if err := NuovoNotturno(pn.p.s.DB, pn.p.s.Archivio, "", pn.o).ConSottofondo(ctx, sf.lancia).EseguiAdesso(); !errors.Is(err, ErrNonConfigurato) {
		t.Errorf("senza chiavetta: %v", err)
	}
	sf.fermo = true
	if err := pn.n.EseguiAdesso(); !errors.Is(err, ErrFermato) {
		t.Errorf("serve che si ferma: %v", err)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto dopo ErrFermato: %v", err)
	}
	_ = l.Rilascia()
	if v := pn.righeStorico(t); len(v) != 2 {
		t.Errorf("storico dopo ErrFermato: %v", v)
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Con un backup in corso in un altro processo (il comando gestrip backup) il giro automatico dovuto non fa
// niente e «Esegui adesso» rifiuta; finito l'altro, il giro fa il turno.
func TestNotturnoConIlComandoInCorso(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx := context.Background()
	sf := &sottofondoProva{}
	pn.n.ConSottofondo(ctx, sf.lancia)
	rilascia, r := figlioLucchetto(t, pn.p.dir)
	if r != "preso" {
		t.Fatalf("figlio: %q", r)
	}
	if _, fatto, err := pn.n.Giro(ctx); fatto || err != nil {
		t.Errorf("giro con il comando in corso: %v %v", fatto, err)
	}
	if err := pn.n.EseguiAdesso(); !errors.Is(err, ErrBackupInCorso) {
		t.Errorf("«Esegui adesso» con il comando in corso: %v", err)
	}
	rilascia()
	sf.wg.Wait()
	if _, fatto, err := pn.n.Giro(ctx); !fatto || err != nil {
		t.Errorf("giro dopo il comando: %v %v", fatto, err)
	}
	if v := strings.Join(pn.righeStorico(t), ","); v != "automatico 2026-11-24 ok" {
		t.Errorf("storico %s", v)
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Lo stop di serve durante un backup (qui a metà della verifica): il backup si ferma, nessuna riga nello storico
// (il turno resta da fare), il lucchetto torna libero e nella cartella dei dati non resta nessuna cartella
// temporanea. Lo stesso con il giro automatico: dopo lo stop il turno è ancora dovuto.
func TestNotturnoStopDuranteIlBackup(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx, ferma := context.WithCancel(context.Background())
	sf := &sottofondoProva{}
	pn.n.ConSottofondo(ctx, sf.lancia)
	vf := nuovaVerificaFerma()
	pn.n.verifica = vf.verifica
	if err := pn.n.EseguiAdesso(); err != nil {
		t.Fatal(err)
	}
	vf.aspettaPartita(t)
	ferma()
	sf.wg.Wait()
	if v := pn.righeStorico(t); len(v) != 0 {
		t.Errorf("righe dopo lo stop: %v", v)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto dopo lo stop: %v", err)
	}
	_ = l.Rilascia()
	if m, _ := filepath.Glob(filepath.Join(pn.p.dir, ".*backup-*")); len(m) != 0 {
		t.Errorf("cartelle temporanee rimaste: %v", m)
	}
	// L'archivio scritto prima dello stop resta sulla chiavetta, ma con la sua verifica «fallita» nel registro:
	// la rotazione lo potrà togliere (un archivio senza riga resterebbe per sempre).
	archivi, err := db.ArchiviNellaCartella(context.Background(), pn.p.s.DB.Lettura, pn.s.chiavetta)
	if err != nil || len(archivi) != 1 || archivi[0].Riuscita {
		t.Errorf("registro dopo lo stop: %+v %v", archivi, err)
	}
	// Il giro automatico fermato a metà: niente riga, il turno resta dovuto.
	pn.o.Sposta(time.Minute)
	ctx2, ferma2 := context.WithCancel(context.Background())
	vf2 := nuovaVerificaFerma()
	pn.n.verifica = vf2.verifica
	fatto := make(chan error, 1)
	go func() {
		_, _, err := pn.n.Giro(ctx2)
		fatto <- err
	}()
	vf2.aspettaPartita(t)
	ferma2()
	if err := <-fatto; !errors.Is(err, context.Canceled) {
		t.Errorf("giro fermato: %v", err)
	}
	if v := pn.righeStorico(t); len(v) != 0 {
		t.Errorf("righe dopo lo stop del giro: %v", v)
	}
	if ok, err := pn.n.Dovuto(context.Background()); !ok || err != nil {
		t.Errorf("turno dopo lo stop: %v %v", ok, err)
	}
	// Il lavoro fermato durante l'attesa dopo l'avvio esce senza giri.
	pn.n.Lavoro(ctx2, func(ctx context.Context, d time.Duration) bool { return clock.Aspetta(ctx, d) })
	if v := pn.righeStorico(t); len(v) != 0 {
		t.Errorf("righe dopo il lavoro fermato: %v", v)
	}
}

// Voce OPS-28, in parte (vedi TestLucchettoMaiDueInsieme).
// Un panico durante il backup è un'esecuzione fallita con la frase fissa (il dettaglio solo nel registro), e il
// lucchetto torna libero.
func TestNotturnoPanico(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	pn.n.verifica = func(context.Context, string, string) (Verificato, error) { panic("guasto inventato") }
	e, fatto, err := pn.n.Giro(context.Background())
	if !fatto || err == nil || e.Esito != EsitoFallito || e.Errore != DettaglioPanico {
		t.Fatalf("panico: %+v %v %v", e, fatto, err)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto dopo il panico: %v", err)
	}
	_ = l.Rilascia()
}

// Il giro non parte senza il lucchetto dei backup (nil o già rilasciato) né con il lucchetto di un'altra cartella
// dei dati: niente archivio, niente righe.
func TestGiroSenzaLucchetto(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	g := Giro{DB: pn.p.s.DB, Archivio: pn.p.s.Archivio, Chiavetta: pn.s.chiavetta}
	if _, err := g.Esegui(context.Background()); !errors.Is(err, ErrSenzaLucchetto) {
		t.Errorf("senza lucchetto: %v", err)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Rilascia()
	g.Lucchetto = l
	if _, err := g.Esegui(context.Background()); !errors.Is(err, ErrSenzaLucchetto) {
		t.Errorf("lucchetto rilasciato: %v", err)
	}
	altro, err := PrendiLucchetto(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = altro.Rilascia() }()
	g.Lucchetto = altro
	if _, err := g.Esegui(context.Background()); !errors.Is(err, ErrSenzaLucchetto) {
		t.Errorf("lucchetto di un'altra cartella: %v", err)
	}
	if v := voceDi(t, pn.s.chiavetta); len(v) != 0 {
		t.Errorf("sulla chiavetta %v", v)
	}
}

// Revisione della 124: se la riga dello storico non si scrive (qui una scrittura finta che fallisce sempre), il
// tentativo si conta in memoria. Un backup riuscito senza riga chiude il turno: i giri dopo (un minuto, mezz'ora,
// un'ora) non rifanno il backup. Con backup che falliscono, al più tre tentativi per turno.
func TestNotturnoStoricoNonScritto(t *testing.T) {
	vecchio := inserisciEsecuzione
	t.Cleanup(func() { inserisciEsecuzione = vecchio })
	scritture := 0
	inserisciEsecuzione = func(context.Context, *db.DB, string, ...any) (sql.Result, error) {
		scritture++
		return nil, errors.New("database occupato inventato")
	}
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx := context.Background()
	if _, fatto, err := pn.n.Giro(ctx); fatto || err == nil || scritture != 1 {
		t.Fatalf("primo giro: %v %v, scritture %d", fatto, err, scritture)
	}
	for _, d := range []time.Duration{time.Minute, AttesaFraTentativi, time.Hour} {
		pn.o.Sposta(d)
		if _, _, err := pn.n.Giro(ctx); err != nil || scritture != 1 {
			t.Errorf("dopo %v: %v, scritture %d", d, err, scritture)
		}
	}
	if v := voceDi(t, pn.s.chiavetta); len(v) != 1 {
		t.Errorf("sulla chiavetta %v", v)
	}
	// Il turno dopo, con la chiavetta smontata: tre tentativi e basta.
	pn.s.sis.info = rigaRadice()
	pn.o.Imposta(alle(2026, 11, 25, 2, 30))
	for range 6 {
		_, _, _ = pn.n.Giro(ctx)
		pn.o.Sposta(AttesaFraTentativi)
	}
	if scritture != 4 {
		t.Errorf("scritture %d, attese 1 + 3", scritture)
	}
}

// Revisione della 124 (R5): un motivo più lungo di 1000 byte (con lettere accentate: il taglio non spezza un
// carattere) entra nello storico tagliato, e la riga si scrive.
func TestNotturnoMotivoLungo(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	lungo := strings.Repeat("è", 1500)
	pn.n.verifica = func(context.Context, string, string) (Verificato, error) { return Verificato{}, errors.New(lungo) }
	e, fatto, err := pn.n.Giro(context.Background())
	if !fatto || err == nil || e.Esito != EsitoFallito {
		t.Fatalf("giro: %+v %v %v", e, fatto, err)
	}
	es, err := pn.n.Storico(context.Background(), 1)
	if err != nil || len(es) != 1 || len(es[0].Errore) > 1000 || len(es[0].Errore) < 990 || !utf8.ValidString(es[0].Errore) {
		t.Errorf("motivo: %d byte, %v", len(es[0].Errore), err)
	}
}

// Revisione della 124 (R6): lo stop che arriva dopo un backup verificato e registrato, durante la rotazione, lascia
// la riga «avviso» (il backup è buono) e il turno è fatto.
func TestNotturnoStopDuranteLaRotazione(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx, ferma := context.WithCancel(context.Background())
	pn.n.verifica = func(c context.Context, dati, file string) (Verificato, error) {
		v, err := VerificaArchivio(c, dati, file)
		ferma() // lo stop subito dopo la verifica: registro scritto, rotazione con il ctx finito
		return v, err
	}
	e, fatto, err := pn.n.Giro(ctx)
	if !fatto || !errors.Is(err, context.Canceled) || e.Esito != EsitoAvviso {
		t.Fatalf("giro: %+v %v %v", e, fatto, err)
	}
	if v := strings.Join(pn.righeStorico(t), ","); v != "automatico 2026-11-24 avviso" {
		t.Errorf("storico %s", v)
	}
	if ok, err := pn.n.Dovuto(context.Background()); ok || err != nil {
		t.Errorf("turno dopo l'avviso: %v %v", ok, err)
	}
}

// Revisione della 124 (R7): con serve già fermato (il contesto dei lavori finito) «Esegui adesso» dà ErrFermato
// subito, anche se il sottofondo accetterebbe ancora il lavoro: niente parte e il lucchetto resta libero.
func TestEseguiAdessoServeFermato(t *testing.T) {
	pn := nuovaProvaNotturno(t, alle(2026, 11, 24, 2, 30))
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	sf := &sottofondoProva{}
	pn.n.ConSottofondo(ctx, sf.lancia)
	if err := pn.n.EseguiAdesso(); !errors.Is(err, ErrFermato) {
		t.Errorf("serve fermato: %v", err)
	}
	sf.wg.Wait()
	if v := voceDi(t, pn.s.chiavetta); len(v) != 0 {
		t.Errorf("sulla chiavetta %v", v)
	}
	l, err := PrendiLucchetto(pn.p.dir)
	if err != nil {
		t.Fatalf("lucchetto: %v", err)
	}
	_ = l.Rilascia()
}
