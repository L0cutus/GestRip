package jobs

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// segna è SegnaDovutiComeFatti in una transazione sua, confermata.
func segna(t *testing.T, p *Pianificatore, adesso time.Time) []string {
	t.Helper()
	tx, err := p.d.Scrittura.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	n, err := SegnaDovutiComeFatti(context.Background(), tx, adesso)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return n
}

// rigaDelBackup scrive un'esecuzione automatica di oggi con l'esito dato (le righe del backup).
func rigaDelBackup(t *testing.T, p *Pianificatore, nome, esito string, il time.Time) {
	t.Helper()
	if _, err := p.d.Scrittura.Exec(`INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno) VALUES (?, ?, ?, ?, ?)`,
		nome, AvvioAutomatico, esito, il.UTC().Format("2006-01-02T15:04:05.000000Z"), clock.DataDi(il).String()); err != nil {
		t.Fatal(err)
	}
}

// inventario: JOB-02
// D-119 (slice 133): alla conferma del ripristino si segnano come già fatti oggi i lavori già dovuti a quell'ora, con
// le regole del giro (giorno d'apertura, lavoro attivo, ora del negozio raggiunta, nessuna esecuzione automatica
// riuscita oggi, meno di tre tentativi) ma senza i canali, che nella copia isolata sono spenti: una riga automatica
// «ok» per lavoro, 0 avvisi e 0 messaggi, DettaglioDopoRipristino, niente jobs.ran. Il giro dopo non li rifà; un
// lavoro non ancora dovuto parte alla sua ora.
func TestSegnaDovutiComeFatti(t *testing.T) {
	d, o := apriDB(t)
	imposta(t, d, "jobs.briefing_enabled", "1") // 08:00
	l, f := tre()
	accendiTelegram(t, d)
	p := Nuovo(d, o, canaliVeri(d, o), l)
	// Senza canali (la copia isolata) i lavori si segnano lo stesso.
	imposta(t, d, uscita.ChiaveTelegramAcceso, "0")
	adesso := locale(2026, 11, 17, 9, 30) // martedì: briefing (08:00) e ritardi (09:00) dovuti, solleciti (10:00) no
	rigaDelBackup(t, p, Ritardi, EsitoFallito, locale(2026, 11, 17, 9, 0))
	if got := segna(t, p, adesso); !reflect.DeepEqual(got, []string{Ritardi, Briefing}) {
		t.Fatalf("segnati %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE avvio = 'scheduled' AND esito = 'ok' AND avvisi = 0 AND messaggi = 0
		AND giorno = '2026-11-17' AND eseguito_il = '2026-11-17T08:30:00.000000Z' AND dettaglio = ?`, DettaglioDopoRipristino); n != 2 {
		t.Fatalf("righe segnate %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'jobs.ran'`); n != 0 {
		t.Fatalf("jobs.ran %d", n)
	}
	// Una seconda volta (o con il giro già fatto) niente: sono già fatti.
	if got := segna(t, p, adesso); len(got) != 0 {
		t.Fatalf("di nuovo: %v", got)
	}
	// Il serve riavviato, con il canale acceso: alle 09:30 niente, alle 10:00 i solleciti, una volta sola.
	imposta(t, d, uscita.ChiaveTelegramAcceso, "1")
	o.Imposta(adesso)
	if e := giro(t, p); len(e) != 0 {
		t.Fatalf("giro dopo la conferma: %+v", e)
	}
	o.Imposta(locale(2026, 11, 17, 10, 0))
	if e := giro(t, p); len(e) != 1 || e[0].Lavoro != Solleciti {
		t.Fatalf("giro delle 10:00: %+v", e)
	}
	if f[Ritardi].quante() != 0 || f[Briefing].quante() != 0 || f[Solleciti].quante() != 1 {
		t.Fatalf("esecuzioni: ritardi %d, briefing %d, solleciti %d", f[Ritardi].quante(), f[Briefing].quante(), f[Solleciti].quante())
	}
}

// inventario: JOB-02
// Quello che il giro non farebbe non si segna: un giorno di chiusura, un lavoro spento, un lavoro già riuscito oggi
// (la riga del backup basta), tre tentativi già fatti; il giorno è quello del negozio, con l'ora legale.
func TestSegnaDovutiComeFattiNonDovuti(t *testing.T) {
	d, o := apriDB(t)
	l, _ := tre()
	p := Nuovo(d, o, canaliVeri(d, o), l)
	if got := segna(t, p, locale(2026, 11, 16, 12, 0)); len(got) != 0 { // lunedì, chiuso di default
		t.Fatalf("lunedì: %v", got)
	}
	imposta(t, d, "jobs.chases_enabled", "0")
	rigaDelBackup(t, p, Ritardi, EsitoOK, locale(2026, 11, 18, 9, 1))
	if got := segna(t, p, locale(2026, 11, 18, 12, 0)); len(got) != 0 { // ritardi già fatti, solleciti spenti, briefing spento
		t.Fatalf("mercoledì: %v", got)
	}
	for range TentativiAlGiorno {
		rigaDelBackup(t, p, Ritardi, EsitoFallito, locale(2026, 11, 19, 9, 0))
	}
	imposta(t, d, "jobs.chases_enabled", "1")
	if got := segna(t, p, locale(2026, 11, 19, 12, 0)); !reflect.DeepEqual(got, []string{Solleciti}) {
		t.Fatalf("giovedì, ritardi senza tentativi: %v", got)
	}
	// Ora legale: alle 02:30 del 29/03, ogni giorno aperto. 01:59 CET non è ancora dovuto, 03:00 CEST sì.
	imposta(t, d, ChiaveGiorniApertura, "1,2,3,4,5,6,7")
	imposta(t, d, "jobs.overdue_hour", "02:30")
	imposta(t, d, "jobs.chases_enabled", "0")
	if got := segna(t, p, time.Date(2026, 3, 29, 0, 59, 0, 0, time.UTC)); len(got) != 0 {
		t.Fatalf("01:59 CET: %v", got)
	}
	if got := segna(t, p, time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC)); !reflect.DeepEqual(got, []string{Ritardi}) {
		t.Fatalf("03:00 CEST: %v", got)
	}
	// La sera tardi in UTC è già il giorno dopo in negozio.
	imposta(t, d, "jobs.overdue_hour", "00:15")
	if got := segna(t, p, time.Date(2026, 7, 14, 22, 30, 0, 0, time.UTC)); !reflect.DeepEqual(got, []string{Ritardi}) {
		t.Fatalf("sera tardi UTC: %v", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM esecuzioni_lavori WHERE giorno = '2026-07-15' AND dettaglio = ?`, DettaglioDopoRipristino); n != 1 {
		t.Fatal("giorno del negozio sbagliato")
	}
}

// inventario: JOB-02
// La frase della conferma (console e pagina) dice i lavori segnati con i nomi della pagina «Promemoria e job»,
// nell'ordine dato, e «Esegui adesso» per quelli che la macchina vecchia non aveva fatto; niente se nessuno.
func TestFraseSegnati(t *testing.T) {
	if f := FraseSegnati(nil); f != "" {
		t.Errorf("nessuno: %q", f)
	}
	if f := FraseSegnati([]string{Ritardi, Briefing}); f != "Lavori di oggi segnati come già fatti: Buste in ritardo, Briefing del mattino. "+
		"Se la macchina vecchia non li aveva fatti, usa «Esegui adesso» in Promemoria e job dopo il riavvio." {
		t.Errorf("due: %q", f)
	}
	if f := FraseSegnati([]string{Solleciti}); f != "Lavori di oggi segnati come già fatti: Solleciti artigiano. "+
		"Se la macchina vecchia non li aveva fatti, usa «Esegui adesso» in Promemoria e job dopo il riavvio." {
		t.Errorf("uno: %q", f)
	}
}
