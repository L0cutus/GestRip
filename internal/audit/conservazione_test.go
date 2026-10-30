package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// adessoGiro è venerdì 30/10/2026 10:15 in negozio (CET, UTC+1).
var adessoGiro = time.Date(2026, 10, 30, 9, 15, 7, 123456000, time.UTC)

// conservazioneProva dà il lavoro su un database nuovo, migrato, con l'orologio finto fermo ad
// adessoGiro; il percorso serve per aprire un secondo collegamento (un altro processo).
func conservazioneProva(t *testing.T) (*Conservazione, *db.DB, *clock.Finto, string) {
	t.Helper()
	percorso := filepath.Join(t.TempDir(), "gestrip.db")
	d := apriProva(t, percorso)
	o := clock.NuovoFinto(adessoGiro)
	if _, err := d.Migra(ctx, o); err != nil {
		t.Fatal(err)
	}
	// Un evento di adesso: il negozio lavora, l'orologio torna con i dati (MargineOrologio).
	evento(t, d, adessoGiro, "riferimento")
	return NuovaConservazione(d, o), d, o, percorso
}

func apriProva(t *testing.T, percorso string) *db.DB {
	t.Helper()
	d, err := db.Apri(percorso)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	return d
}

// evento scrive una riga d'audit con l'istante dato, come farebbe l'import di una riga vecchia.
func evento(t *testing.T, d *db.DB, quando time.Time, azione string) {
	t.Helper()
	if _, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il) VALUES ('public', ?, 'r', '1', 'ok', ?)`,
		azione, quando.UTC().Format(db.FormatoIstante)); err != nil {
		t.Fatal(err)
	}
}

func contaRighe(t *testing.T, d *db.DB, q string, arg ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, arg...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// righeDelGiro sono le righe audit.retention_applied: attore, dati e istante.
func righeDelGiro(t *testing.T, d *db.DB) []riga {
	t.Helper()
	righe, err := d.Lettura.Query(`SELECT attore, azione, risorsa, id_risorsa, esito, dati, ip, user_agent, creato_il FROM eventi_audit WHERE azione = ? ORDER BY id`, AzioneConservazione)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = righe.Close() }()
	var rr []riga
	for righe.Next() {
		var r riga
		if err := righe.Scan(&r.attore, &r.azione, &r.risorsa, &r.id, &r.esito, &r.dati, &r.ip, &r.ua, &r.creato); err != nil {
			t.Fatal(err)
		}
		rr = append(rr, r)
	}
	if err := righe.Err(); err != nil {
		t.Fatal(err)
	}
	return rr
}

func datiDelGiro(t *testing.T, r riga) (cancellati int64, limite string) {
	t.Helper()
	var v struct {
		Deleted *int64  `json:"deleted"`
		Cutoff  *string `json:"cutoff"`
	}
	if err := json.Unmarshal([]byte(r.dati), &v); err != nil || v.Deleted == nil || v.Cutoff == nil {
		t.Fatalf("dati della riga del giro: %s (%v)", r.dati, err)
	}
	return *v.Deleted, *v.Cutoff
}

// inventario: ADM-17
// «Più vecchi di 5 anni» sono 5 anni del calendario del negozio (Europe/Rome), non 5×365
// giorni: lo stesso giorno e la stessa ora di 5 anni fa, che può essere un'altra ora UTC se il
// cambio dell'ora sta in mezzo. Un 29 febbraio che non c'è diventa il 1° marzo (AddDate).
func TestLimiteConservazione(t *testing.T) {
	casi := []struct{ adesso, limite string }{
		// 30/10/2026 10:15 CET → 30/10/2021 10:15 CEST: un'ora prima in UTC.
		{"2026-10-30T09:15:07.123456Z", "2021-10-30T08:15:07.123456Z"},
		// Estate su estate: la stessa ora UTC.
		{"2026-07-15T08:00:00Z", "2021-07-15T08:00:00Z"},
		// Capodanno del negozio: 01/01/2027 00:30 → 01/01/2022 00:30.
		{"2026-12-31T23:30:00Z", "2021-12-31T23:30:00Z"},
		// 29/02/2028 → 29/02/2023 non c'è: 01/03/2023.
		{"2028-02-29T11:00:00Z", "2023-03-01T11:00:00Z"},
	}
	for _, c := range casi {
		a, _ := time.Parse(time.RFC3339Nano, c.adesso)
		atteso, _ := time.Parse(time.RFC3339Nano, c.limite)
		got := LimiteConservazione(a)
		if !got.Equal(atteso) {
			t.Errorf("%s: limite %s, atteso %s", c.adesso, got.UTC().Format(time.RFC3339Nano), c.limite)
		}
		if got.Location() != time.UTC {
			t.Errorf("%s: limite non in UTC: %v", c.adesso, got.Location())
		}
	}
	if d := LimiteConservazione(adessoGiro).Sub(adessoGiro.AddDate(0, 0, -5*365)); d > 0 {
		t.Errorf("il limite è dopo 5×365 giorni fa: i giorni dei bisestili non contano (%v)", d)
	}
}

// inventario: ADM-17
// Il confine: un evento di esattamente 5 anni fa resta, uno di un microsecondo prima se ne va. La
// riga che chiude il giro dice quante righe e il limite, con l'attore «retention» dei lavori di
// pulizia dello standalone, e non viene cancellata dal suo stesso giro.
func TestConservazioneConfine(t *testing.T) {
	c, d, _, _ := conservazioneProva(t)
	limite := LimiteConservazione(adessoGiro)
	evento(t, d, limite.Add(-time.Microsecond), "va.via")
	evento(t, d, time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC), "va.via")
	evento(t, d, limite, "resta.confine")
	evento(t, d, limite.Add(time.Microsecond), "resta")
	// Con 5 anni in UTC (30/10/2021 09:15 UTC) questa se ne andrebbe: con il calendario del
	// negozio resta.
	evento(t, d, time.Date(2021, 10, 30, 9, 0, 0, 0, time.UTC), "resta.utc")
	evento(t, d, adessoGiro.Add(-time.Hour), "resta")

	e, err := c.Applica(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !e.Girata || e.Cancellati != 2 || !e.Limite.Equal(limite) {
		t.Errorf("esito %+v", e)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 0 {
		t.Errorf("restano %d eventi più vecchi di 5 anni", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'resta%'`); n != 4 {
		t.Errorf("restano %d eventi di 5 anni o meno, attesi 4", n)
	}
	rr := righeDelGiro(t, d)
	if len(rr) != 1 {
		t.Fatalf("righe del giro: %d", len(rr))
	}
	r := rr[0]
	if r.attore != "retention" || r.risorsa != "audit" || r.id != "events" || r.esito != "ok" || r.ip != nil || r.ua != nil ||
		r.creato != adessoGiro.Format(db.FormatoIstante) {
		t.Errorf("riga del giro %+v", r)
	}
	n, lim := datiDelGiro(t, r)
	if n != 2 || lim != limite.Format(db.FormatoIstante) {
		t.Errorf("dati del giro %s", r.dati)
	}
}

// inventario: ADM-17
// La cancellazione va a lotti, una transazione per lotto: tra un lotto e l'altro il collegamento
// di scrittura è libero e le altre scritture passano (qui: una riga d'audit a ogni pausa). Il
// totale della riga del giro conta tutti i lotti; le righe scritte nelle pause restano.
func TestConservazioneALotti(t *testing.T) {
	c, d, o, _ := conservazioneProva(t)
	c.lotto = 7
	vecchio := LimiteConservazione(adessoGiro).Add(-time.Hour)
	for i := range 50 {
		evento(t, d, vecchio.Add(-time.Duration(i)*time.Minute), "va.via")
	}
	pause := 0
	c.traLotti = func() {
		pause++
		o.Sposta(time.Minute)
		// Con il collegamento di scrittura ancora preso questa scrittura aspetterebbe per sempre.
		fatto := make(chan error, 1)
		go func() {
			fatto <- Nuovo(d, o).Registra(ctx, Contesto{Attore: "titolare@esempio.it"}, "user.deactivated", "user", pause, "ok", nil)
		}()
		select {
		case err := <-fatto:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("scrittura bloccata tra due lotti")
		}
	}
	e, err := c.Applica(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 7 lotti pieni e l'ottavo con 1 riga, che chiude il giro nella sua transazione.
	if pause != 7 {
		t.Errorf("pause tra i lotti: %d, attese 7", pause)
	}
	// Lo stato porta l'ora vera dell'ultimo lotto, non quella d'inizio del giro.
	var aggiornata string
	if err := d.Lettura.QueryRow(`SELECT aggiornata_il FROM impostazioni WHERE chiave = 'audit.conservazione'`).Scan(&aggiornata); err != nil {
		t.Fatal(err)
	}
	if aggiornata != o.Adesso().Format(db.FormatoIstante) {
		t.Errorf("aggiornata_il %s, atteso %s", aggiornata, o.Adesso().Format(db.FormatoIstante))
	}
	if !e.Girata || e.Cancellati != 50 {
		t.Errorf("esito %+v", e)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 0 {
		t.Errorf("restano %d eventi vecchi", n)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'user.deactivated'`); n != 7 {
		t.Errorf("righe scritte nelle pause: %d", n)
	}
	if rr := righeDelGiro(t, d); len(rr) != 1 {
		t.Errorf("righe del giro: %d", len(rr))
	} else if n, _ := datiDelGiro(t, rr[0]); n != 50 {
		t.Errorf("dati del giro %s", rr[0].dati)
	}
	// Un multiplo esatto del lotto: l'ultimo lotto è vuoto e chiude il giro lo stesso.
	c2, d2, _, _ := conservazioneProva(t)
	c2.lotto = 5
	for range 10 {
		evento(t, d2, vecchio, "va.via")
	}
	if e, err := c2.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 10 {
		t.Errorf("multiplo del lotto: %+v %v", e, err)
	}
	// Un ultimo lotto di lotto−1 righe chiude il giro: 9 righe a lotti di 5, una pausa sola.
	c3, d3, _, _ := conservazioneProva(t)
	c3.lotto = 5
	for range 9 {
		evento(t, d3, vecchio, "va.via")
	}
	pause3 := 0
	c3.traLotti = func() { pause3++ }
	if e, err := c3.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 9 || pause3 != 1 {
		t.Errorf("ultimo lotto di lotto−1: %+v %v, pause %d", e, err, pause3)
	}
}

// inventario: ADM-17
// Il lotto vero è di 1000 righe (misurato: circa 3 ms a lotto, al massimo 27 ms con 200.000 righe
// da cancellare): con 1001 righe vecchie, un lotto pieno, una pausa e l'ultimo lotto di 1.
func TestConservazioneLottoVero(t *testing.T) {
	c, d, _, _ := conservazioneProva(t)
	vecchio := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC).Format(db.FormatoIstante)
	tx, err := d.Scrittura.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for range 1001 {
		if _, err := tx.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il) VALUES ('public', 'va.via', 'r', '1', 'ok', ?)`, vecchio); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	pause := 0
	c.traLotti = func() { pause++ }
	if e, err := c.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 1001 || pause != 1 {
		t.Errorf("esito %+v %v, pause %d", e, err, pause)
	}
	if OgniQuantoConservazione != time.Hour {
		t.Errorf("controllo ogni %v, atteso ogni ora", OgniQuantoConservazione)
	}
}

// inventario: ADM-17
// Il giro di un mese nuovo parte da zero: il totale del mese prima non si somma.
func TestConservazioneMeseNuovoDaCapo(t *testing.T) {
	c, d, o, _ := conservazioneProva(t)
	evento(t, d, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), "va.via")
	evento(t, d, time.Date(2019, 1, 2, 0, 0, 0, 0, time.UTC), "va.via")
	if e, err := c.Applica(ctx); err != nil || e.Cancellati != 2 {
		t.Fatalf("ottobre: %+v %v", e, err)
	}
	evento(t, d, time.Date(2019, 2, 1, 0, 0, 0, 0, time.UTC), "va.via")
	o.Imposta(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	e, err := c.Applica(ctx)
	if err != nil || !e.Girata || e.Cancellati != 1 {
		t.Errorf("novembre: %+v %v", e, err)
	}
	if rr := righeDelGiro(t, d); len(rr) != 2 {
		t.Errorf("righe del giro: %d", len(rr))
	} else if n, _ := datiDelGiro(t, rr[1]); n != 1 {
		t.Errorf("dati di novembre %s", rr[1].dati)
	}
}

// inventario: ADM-17
// Un errore dentro la transazione di un lotto (qui: uno stato con il limite rovinato) la
// annulla: Applica dà errore e il collegamento di scrittura è subito libero.
func TestConservazioneErroreNellaTransazione(t *testing.T) {
	c, d, o, _ := conservazioneProva(t)
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, '{"mese":"2026-10","limite":"rotto","finito":false}', ?)`,
		ChiaveConservazione, adessoGiro.Format(db.FormatoIstante)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Applica(ctx); err == nil || !strings.Contains(err.Error(), "limite illeggibile") {
		t.Errorf("errore %v", err)
	}
	scritto := make(chan error, 1)
	go func() { scritto <- Nuovo(d, o).Registra(ctx, Contesto{Attore: "x"}, "a.b", "r", "1", "ok", nil) }()
	select {
	case err := <-scritto:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("collegamento di scrittura ancora preso dopo l'errore")
	}
}

// inventario: ADM-17
// Lo spegnimento a metà giro (contesto finito tra due lotti) non è un errore da registrare: i
// lotti fatti restano e il giro riprende al prossimo avvio.
func TestConservazioneSpegnimentoNonEUnErrore(t *testing.T) {
	c, d, _, _ := conservazioneProva(t)
	c.lotto = 2
	for range 5 {
		evento(t, d, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), "va.via")
	}
	lavoroCtx, ferma := context.WithCancel(ctx)
	c.traLotti = ferma
	var log registroSicuro
	c.Lavoro(lavoroCtx, func(context.Context) bool { return false }, slog.New(slog.NewTextHandler(&log, nil)))
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 3 {
		t.Errorf("dopo il primo lotto restano %d, attese 3", n)
	}
	if strings.Contains(log.String(), "level=ERROR") {
		t.Errorf("spegnimento registrato come errore: %q", log.String())
	}
}

// inventario: ADM-17
// Al massimo un giro per mese del calendario del negozio, anche dopo un riavvio (lo stato sta nel
// database, in impostazioni): un evento che diventa vecchio dopo il giro aspetta il mese dopo. Il
// mese cambia alla mezzanotte di Roma, non a quella UTC. Un giro senza niente da cancellare
// scrive comunque la sua riga, con 0.
func TestConservazioneUnaVoltaAlMese(t *testing.T) {
	c, d, o, percorso := conservazioneProva(t)
	if e, err := c.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 0 {
		t.Fatalf("primo giro: %+v %v", e, err)
	}
	evento(t, d, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), "va.via.dopo")
	if e, err := c.Applica(ctx); err != nil || e.Girata {
		t.Errorf("secondo giro nello stesso mese: %+v %v", e, err)
	}
	// Il riavvio: un altro collegamento, un altro lavoro.
	d2 := apriProva(t, percorso)
	c2 := NuovaConservazione(d2, o)
	// 31/10/2026 23:59 a Roma è ancora ottobre (22:59 UTC).
	o.Imposta(time.Date(2026, 10, 31, 22, 59, 0, 0, time.UTC))
	if e, err := c2.Applica(ctx); err != nil || e.Girata {
		t.Errorf("dopo il riavvio, stesso mese: %+v %v", e, err)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via.dopo'`); n != 1 {
		t.Errorf("cancellato fuori dal giro del mese: %d", n)
	}
	if rr := righeDelGiro(t, d); len(rr) != 1 {
		t.Errorf("righe del giro in ottobre: %d", len(rr))
	} else if n, _ := datiDelGiro(t, rr[0]); n != 0 {
		t.Errorf("dati del primo giro %s", rr[0].dati)
	}
	// 01/11/2026 00:00 a Roma (23:00 UTC del 31): novembre.
	o.Imposta(time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC))
	if e, err := c2.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 1 {
		t.Errorf("giro di novembre: %+v %v", e, err)
	}
	if rr := righeDelGiro(t, d); len(rr) != 2 {
		t.Errorf("righe del giro: %d", len(rr))
	}
	var stato string
	if err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = 'audit.conservazione'`).Scan(&stato); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stato, `"mese":"2026-11"`) || !strings.Contains(stato, `"finito":true`) {
		t.Errorf("stato %s", stato)
	}
}

// inventario: ADM-17
// Recupero: con l'apparecchio spento per mesi, il giro si fa al primo controllo dopo l'avvio. Un
// giro interrotto a metà (spegnimento tra due lotti) riprende al controllo dopo, con lo stesso
// limite, e la riga del giro conta anche i lotti di prima.
func TestConservazioneRecupero(t *testing.T) {
	c, d, o, percorso := conservazioneProva(t)
	o.Imposta(time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC))
	if e, err := c.Applica(ctx); err != nil || !e.Girata {
		t.Fatalf("giro di luglio: %+v %v", e, err)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = ? AND valore LIKE '%"mese":"2026-07"%'`, ChiaveConservazione); n != 1 {
		t.Error("mese di luglio non scritto come 2026-07")
	}
	// Spento da luglio a fine ottobre.
	o.Imposta(adessoGiro)
	vecchio := LimiteConservazione(adessoGiro).Add(-time.Minute)
	for range 12 {
		evento(t, d, vecchio, "va.via")
	}
	c.lotto = 5
	interrotto, ferma := context.WithCancel(ctx)
	c.traLotti = func() { ferma() }
	if e, err := c.Applica(interrotto); err == nil || e.Girata {
		t.Fatalf("giro interrotto: %+v %v", e, err)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 7 {
		t.Fatalf("dopo il primo lotto restano %d righe, attese 7", n)
	}
	// Il riavvio, un'ora dopo: il limite resta quello del giro cominciato.
	o.Sposta(time.Hour)
	evento(t, d, vecchio.Add(30*time.Minute), "resta")
	c2 := NuovaConservazione(apriProva(t, percorso), o)
	c2.lotto = 5
	e, err := c2.Applica(ctx)
	if err != nil || !e.Girata || e.Cancellati != 12 || !e.Limite.Equal(LimiteConservazione(adessoGiro)) {
		t.Errorf("ripresa: %+v %v", e, err)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'resta'`); n != 1 {
		t.Errorf("la ripresa ha spostato il limite: %d", n)
	}
	rr := righeDelGiro(t, d)
	if len(rr) != 2 {
		t.Fatalf("righe del giro: %d", len(rr))
	}
	if n, lim := datiDelGiro(t, rr[1]); n != 12 || lim != LimiteConservazione(adessoGiro).Format(db.FormatoIstante) {
		t.Errorf("dati della ripresa %s", rr[1].dati)
	}
}

// inventario: ADM-17
// Due lavori insieme sullo stesso database (due processi, due collegamenti): un solo giro. Il
// controllo del mese sta dentro ogni transazione di scrittura, il totale nello stato condiviso:
// una sola riga del giro, con tutte le righe cancellate, e un solo esito «girata».
func TestConservazioneConcorrente(t *testing.T) {
	t.Run("intrecciati", func(t *testing.T) {
		a, d, o, percorso := conservazioneProva(t)
		b := NuovaConservazione(apriProva(t, percorso), o)
		a.lotto, b.lotto = 4, 4
		for range 10 {
			evento(t, d, time.Date(2019, 5, 1, 0, 0, 0, 0, time.UTC), "va.via")
		}
		var eb EsitoConservazione
		var errB error
		a.traLotti = func() {
			if a.traLotti == nil {
				return
			}
			a.traLotti = nil
			// B fa tutto il resto mentre A è fermo dopo il primo lotto.
			eb, errB = b.Applica(ctx)
		}
		ea, errA := a.Applica(ctx)
		if errA != nil || errB != nil {
			t.Fatal(errA, errB)
		}
		if ea.Girata || !eb.Girata || eb.Cancellati != 10 {
			t.Errorf("esiti %+v %+v", ea, eb)
		}
		if rr := righeDelGiro(t, d); len(rr) != 1 {
			t.Errorf("righe del giro: %d", len(rr))
		} else if n, _ := datiDelGiro(t, rr[0]); n != 10 {
			t.Errorf("dati del giro %s", rr[0].dati)
		}
	})
	t.Run("liberi", func(t *testing.T) {
		a, d, o, percorso := conservazioneProva(t)
		b := NuovaConservazione(apriProva(t, percorso), o)
		a.lotto, b.lotto = 3, 3
		for i := range 200 {
			evento(t, d, time.Date(2019, 5, 1, 0, 0, i, 0, time.UTC), "va.via")
		}
		var wg sync.WaitGroup
		esiti := make([]EsitoConservazione, 2)
		errs := make([]error, 2)
		for i, c := range []*Conservazione{a, b} {
			wg.Go(func() { esiti[i], errs[i] = c.Applica(ctx) })
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatal(err)
		}
		if esiti[0].Girata == esiti[1].Girata {
			t.Errorf("esiti %+v", esiti)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 0 {
			t.Errorf("restano %d", n)
		}
		if rr := righeDelGiro(t, d); len(rr) != 1 {
			t.Errorf("righe del giro: %d", len(rr))
		} else if n, _ := datiDelGiro(t, rr[0]); n != 200 {
			t.Errorf("dati del giro %s", rr[0].dati)
		}
	})
}

// inventario: ADM-17
// Il lavoro tocca solo eventi_audit (e la sua riga di stato in impostazioni): utenti,
// impostazioni e ogni altra tabella restano uguali, anche con date di più di 5 anni fa. Il
// registro di emergenza (EME) non è ancora nel Go: quando arriva sarà un'altra tabella.
func TestConservazioneAltreTabelle(t *testing.T) {
	c, d, _, _ := conservazioneProva(t)
	vecchio := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC).Format(db.FormatoIstante)
	if _, err := d.Scrittura.Exec(`INSERT INTO utenti (email, ruolo, creato_il, aggiornato_il, disattivato_il, attivo) VALUES ('vecchio@esempio.it', 'dipendente', ?, ?, ?, 0)`, vecchio, vecchio, vecchio); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('negozio_nome', 'Prova', ?)`, vecchio); err != nil {
		t.Fatal(err)
	}
	evento(t, d, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), "va.via")
	prima := fotografia(t, d)
	if e, err := c.Applica(ctx); err != nil || e.Cancellati != 1 {
		t.Fatalf("%+v %v", e, err)
	}
	dopo := fotografia(t, d)
	for tabella, righe := range prima {
		if tabella == "eventi_audit" || tabella == "sqlite_sequence" {
			continue
		}
		if dopo[tabella] != righe {
			t.Errorf("%s cambiata:\nprima %s\ndopo  %s", tabella, righe, dopo[tabella])
		}
	}
}

// fotografia è il contenuto di ogni tabella, come testo; per impostazioni senza la riga di stato
// del lavoro.
func fotografia(t *testing.T, d *db.DB) map[string]string {
	t.Helper()
	nomi, err := d.Lettura.Query(`SELECT name FROM sqlite_schema WHERE type = 'table'`)
	if err != nil {
		t.Fatal(err)
	}
	var tabelle []string
	for nomi.Next() {
		var n string
		if err := nomi.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tabelle = append(tabelle, n)
	}
	_ = nomi.Close()
	f := map[string]string{}
	for _, tab := range tabelle {
		q := `SELECT * FROM "` + tab + `"`
		if tab == "impostazioni" {
			q += ` WHERE chiave <> '` + ChiaveConservazione + `'`
		}
		righe, err := d.Lettura.Query(q + ` ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		col, _ := righe.Columns()
		var b strings.Builder
		for righe.Next() {
			v := make([]any, len(col))
			p := make([]any, len(col))
			for i := range v {
				p[i] = &v[i]
			}
			if err := righe.Scan(p...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%v\n", v)
		}
		_ = righe.Close()
		f[tab] = b.String()
	}
	return f
}

// orologioCheSalta è un orologio che, a richiesta, va in panico: subito (salta) o alla n-esima
// lettura da adesso (saltaFra).
type orologioCheSalta struct {
	*clock.Finto
	mu       sync.Mutex
	salta    bool
	saltaFra int
}

func (o *orologioCheSalta) Adesso() time.Time {
	o.mu.Lock()
	s := o.salta
	if o.saltaFra > 0 {
		o.saltaFra--
		s = s || o.saltaFra == 0
	}
	o.mu.Unlock()
	if s {
		panic("orologio rotto")
	}
	return o.Finto.Adesso()
}

func (o *orologioCheSalta) imposta(s bool) {
	o.mu.Lock()
	o.salta = s
	o.mu.Unlock()
}

func (o *orologioCheSalta) saltaAlla(n int) {
	o.mu.Lock()
	o.saltaFra = n
	o.mu.Unlock()
}

// registroSicuro è un buffer per slog che si può leggere mentre il lavoro scrive.
type registroSicuro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (r *registroSicuro) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.Write(p)
}

func (r *registroSicuro) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.b.String()
}

// inventario: ADM-17
// Il lavoro di serve: un controllo subito all'avvio (il recupero), poi uno dopo ogni attesa. Un
// errore o un panico finiscono nel registro del processo e il controllo dopo riprova; il lavoro
// non si ferma. Alla fine del contesto il lavoro esce. Nessuna attesa vera: l'attesa è finta.
func TestConservazioneLavoro(t *testing.T) {
	_, d, finto, _ := conservazioneProva(t)
	o := &orologioCheSalta{Finto: finto}
	c := NuovaConservazione(d, o)
	evento(t, d, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), "va.via")
	// Uno stato illeggibile: il giro dà errore e non cancella niente.
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, 'rotto', ?)`,
		ChiaveConservazione, adessoGiro.Format(db.FormatoIstante)); err != nil {
		t.Fatal(err)
	}
	var log registroSicuro
	lavoroCtx, ferma := context.WithCancel(ctx)
	giri := make(chan struct{}) // il test dà il via al controllo dopo
	attese := make(chan struct{})
	finito := make(chan struct{})
	go func() {
		defer close(finito)
		c.Lavoro(lavoroCtx, func(ctx context.Context) bool {
			attese <- struct{}{}
			select {
			case <-giri:
				return true
			case <-ctx.Done():
				return false
			}
		}, slog.New(slog.NewTextHandler(&log, nil)))
	}()
	attendi := func() {
		t.Helper()
		select {
		case <-attese:
		case <-time.After(10 * time.Second):
			t.Fatal("il lavoro non arriva all'attesa")
		}
	}
	attendi() // il controllo dell'avvio è fatto
	if !strings.Contains(log.String(), "level=ERROR") || !strings.Contains(log.String(), "conservazione") {
		t.Errorf("errore non registrato: %q", log.String())
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 1 {
		t.Errorf("cancellato con lo stato illeggibile: %d", n)
	}
	// Un panico nel giro: registrato, il lavoro continua.
	o.imposta(true)
	giri <- struct{}{}
	attendi()
	if !regexp.MustCompile(`level=ERROR [^\n]*orologio rotto`).MatchString(log.String()) {
		t.Errorf("panico non registrato come errore: %q", log.String())
	}
	o.imposta(false)
	if _, err := d.Scrittura.Exec(`DELETE FROM impostazioni WHERE chiave = ?`, ChiaveConservazione); err != nil {
		t.Fatal(err)
	}
	giri <- struct{}{}
	attendi()
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 0 {
		t.Errorf("il controllo dopo l'errore non ha riprovato: %d", n)
	}
	if !strings.Contains(log.String(), "level=INFO") {
		t.Errorf("giro non registrato: %q", log.String())
	}
	// Un altro controllo nello stesso mese non scrive niente.
	giri <- struct{}{}
	attendi()
	if rr := righeDelGiro(t, d); len(rr) != 1 {
		t.Errorf("righe del giro: %d", len(rr))
	}
	ferma()
	select {
	case <-finito:
	case <-time.After(10 * time.Second):
		t.Fatal("il lavoro non esce alla fine del contesto")
	}
}

// inventario: ADM-17
// L'orologio del sistema avanti per errore (apparecchio senza RTC, NTP non ancora sincronizzato
// all'avvio): se adesso è oltre l'evento più recente di più di MargineOrologio, il giro si rimanda
// senza cancellare niente e senza consumare il mese (avviso nel registro del processo). Le righe
// del giro non contano come evento più recente. Entro il margine il limite è il minimo tra
// adesso e l'evento più recente, meno 5 anni: non si cancella niente che i dati dicono più
// giovane di 5 anni.
func TestConservazioneOrologioAvanti(t *testing.T) {
	t.Run("2040", func(t *testing.T) {
		c, d, o, _ := conservazioneProva(t)
		evento(t, d, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), "vecchio")
		evento(t, d, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "recente")
		if _, err := d.Scrittura.Exec(`INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, creato_il) VALUES ('retention', ?, 'audit', 'events', 'ok', '2039-12-01T00:00:00.000000Z')`,
			AzioneConservazione); err != nil {
			t.Fatal(err)
		}
		prima := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`)
		o.Imposta(time.Date(2040, 1, 1, 9, 0, 0, 0, time.UTC))
		e, err := c.Applica(ctx)
		if err != nil || e.Girata || !e.Rimandata {
			t.Errorf("esito %+v %v", e, err)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit`); n != prima {
			t.Errorf("righe %d, prima %d", n, prima)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = ?`, ChiaveConservazione); n != 0 {
			t.Errorf("il giro rimandato ha scritto lo stato")
		}
		var log registroSicuro
		c.Lavoro(ctx, func(context.Context) bool { return false }, slog.New(slog.NewTextHandler(&log, nil)))
		if !strings.Contains(log.String(), "level=WARN") || !strings.Contains(log.String(), "rimandato") {
			t.Errorf("avviso mancante: %q", log.String())
		}
		// Con l'orologio di nuovo giusto il giro si fa.
		o.Imposta(adessoGiro)
		if e, err := c.Applica(ctx); err != nil || !e.Girata || e.Cancellati != 1 {
			t.Errorf("con l'orologio giusto: %+v %v", e, err)
		}
	})
	t.Run("entro il margine", func(t *testing.T) {
		c, d, o, _ := conservazioneProva(t)
		limite := LimiteConservazione(adessoGiro)
		evento(t, d, limite.Add(-time.Microsecond), "va.via")
		// Con adesso − 5 anni (19/11/2021) se ne andrebbe; l'evento più recente (30/10/2026) dice
		// che non ha ancora 5 anni.
		evento(t, d, limite.Add(24*time.Hour), "resta")
		o.Imposta(adessoGiro.Add(20 * 24 * time.Hour))
		e, err := c.Applica(ctx)
		if err != nil || !e.Girata || e.Cancellati != 1 || !e.Limite.Equal(limite) {
			t.Errorf("esito %+v %v", e, err)
		}
		if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'resta'`); n != 1 {
			t.Errorf("cancellato un evento di meno di 5 anni rispetto ai dati")
		}
	})
}

// inventario: ADM-17
// Un giro interrotto e ripreso nel mese dopo: prima si chiude il giro vecchio, con il suo limite e
// il suo totale e la sua riga, poi parte quello del mese nuovo, con la riga sua.
func TestConservazioneRipresaNelMeseDopo(t *testing.T) {
	c, d, o, percorso := conservazioneProva(t)
	limOttobre := LimiteConservazione(adessoGiro)
	for range 12 {
		evento(t, d, limOttobre.Add(-time.Minute), "va.via.ottobre")
	}
	c.lotto = 5
	interrotto, ferma := context.WithCancel(ctx)
	c.traLotti = func() { ferma() }
	if _, err := c.Applica(interrotto); err == nil {
		t.Fatal("giro non interrotto")
	}
	// Riacceso il 2 novembre, con il negozio al lavoro.
	o.Imposta(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC))
	evento(t, d, o.Adesso(), "riferimento")
	limNovembre := LimiteConservazione(o.Adesso())
	for range 3 {
		evento(t, d, limNovembre.Add(-time.Minute), "va.via.novembre")
	}
	c2 := NuovaConservazione(apriProva(t, percorso), o)
	c2.lotto = 5
	e, err := c2.Applica(ctx)
	if err != nil || !e.Girata || e.Cancellati != 3 || !e.Limite.Equal(limNovembre) {
		t.Errorf("esito %+v %v", e, err)
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'va.via%'`); n != 0 {
		t.Errorf("restano %d", n)
	}
	rr := righeDelGiro(t, d)
	if len(rr) != 2 {
		t.Fatalf("righe del giro: %d", len(rr))
	}
	for i, atteso := range []struct {
		n   int64
		lim time.Time
	}{{12, limOttobre}, {3, limNovembre}} {
		if n, lim := datiDelGiro(t, rr[i]); n != atteso.n || lim != atteso.lim.Format(db.FormatoIstante) {
			t.Errorf("riga %d: %s", i, rr[i].dati)
		}
	}
}

// inventario: ADM-17
// Un panico dentro la transazione di un lotto, dopo la cancellazione: il lotto si annulla tutto
// (nessuna riga cancellata a metà, stato invariato), il collegamento di scrittura torna libero e
// il controllo dopo fa il giro.
func TestConservazionePanicoNellaTransazione(t *testing.T) {
	_, d, finto, _ := conservazioneProva(t)
	o := &orologioCheSalta{Finto: finto}
	c := NuovaConservazione(d, o)
	for range 3 {
		evento(t, d, time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC), "va.via")
	}
	// Prima lettura: Applica; seconda: la riga del giro, dopo il DELETE nella transazione.
	o.saltaAlla(2)
	var log registroSicuro
	giri := 0
	finito := make(chan struct{})
	go func() {
		defer close(finito)
		c.Lavoro(ctx, func(context.Context) bool {
			giri++
			if giri > 1 {
				return false
			}
			if !strings.Contains(log.String(), "orologio rotto") {
				t.Errorf("panico non registrato: %q", log.String())
			}
			if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 3 {
				t.Errorf("cancellate a metà: restano %d", n)
			}
			if n := contaRighe(t, d, `SELECT count(*) FROM impostazioni WHERE chiave = ?`, ChiaveConservazione); n != 0 {
				t.Error("stato scritto dal lotto annullato")
			}
			scritto := make(chan error, 1)
			go func() {
				scritto <- Nuovo(d, finto).Registra(ctx, Contesto{Attore: "x"}, "a.b", "r", "1", "ok", nil)
			}()
			select {
			case err := <-scritto:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("collegamento di scrittura ancora preso dopo il panico")
			}
			return true
		}, slog.New(slog.NewTextHandler(&log, nil)))
	}()
	select {
	case <-finito:
	case <-time.After(30 * time.Second):
		t.Fatal("il lavoro non esce")
	}
	if n := contaRighe(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'va.via'`); n != 0 {
		t.Errorf("il controllo dopo il panico non ha fatto il giro: restano %d", n)
	}
}
