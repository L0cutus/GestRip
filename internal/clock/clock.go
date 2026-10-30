// Package clock è l'unico orologio del negozio (JOB-01, PIANO §1.7).
//
// Gli istanti si salvano in UTC; si leggono nel fuso fisso Europe/Rome (regole dalla zoneinfo
// del sistema se c'è, altrimenti dai dati incorporati; il fuso impostato sul sistema non conta): il giorno a cui
// appartiene un'esecuzione, il «oggi» di ritardi e briefing, ogni data stampata su schermo o
// PDF. Il fuso del sistema non conta. Nessun altro pacchetto del prodotto chiama time.Now() o
// legge time.Local: guardia_test.go lo vieta.
package clock

import (
	"context"
	"fmt"
	"sync"
	"time"
	_ "time/tzdata" // ripiego incorporato: funziona anche senza zoneinfo sul sistema
)

// Roma è il fuso del negozio, fisso nel codice (deciso il 29/10/2026).
var Roma = func() *time.Location {
	l, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		panic("fuso Europe/Rome mancante: " + err.Error())
	}
	return l
}()

// Orologio dà l'istante attuale, in UTC. Il prodotto riceve un Orologio invece di leggere il
// sistema, così i test possono fermarlo e spostarlo.
type Orologio interface {
	Adesso() time.Time
}

// Sistema è l'orologio vero.
type Sistema struct{}

// Adesso è l'istante attuale in UTC.
func (Sistema) Adesso() time.Time { return time.Now().UTC() }

// Finto è un orologio fermo che si sposta solo a mano; si può usare da più goroutine.
type Finto struct {
	mu sync.Mutex
	t  time.Time
}

// NuovoFinto crea un orologio fermo all'istante t.
func NuovoFinto(t time.Time) *Finto { return &Finto{t: t.UTC()} }

// Adesso è l'istante a cui è fermo, in UTC.
func (f *Finto) Adesso() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

// Sposta lo porta avanti (o indietro, con d negativo).
func (f *Finto) Sposta(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// Imposta lo ferma all'istante t.
func (f *Finto) Imposta(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = t.UTC()
}

// Aspetta aspetta d sull'orologio vero e dice true; false se ctx finisce prima. È l'attesa dei
// lavori automatici: nei test si passa un'attesa finta.
func Aspetta(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Locale è l'istante come lo legge il negozio.
func Locale(t time.Time) time.Time { return t.In(Roma) }

// Formatta stampa l'istante sull'orologio del negozio, con il layout di chi chiama.
func Formatta(t time.Time, layout string) string { return Locale(t).Format(layout) }

// Timbro è «gg/mm/aaaa hh:mm» sull'orologio del negozio, la forma della maggior parte delle
// pagine.
func Timbro(t time.Time) string { return Formatta(t, "02/01/2006 15:04") }

// Data è un giorno del calendario, senza ora e senza fuso.
type Data struct {
	Anno   int
	Mese   time.Month
	Giorno int
}

// DataDi è il giorno in cui cade l'istante, in negozio.
func DataDi(t time.Time) Data {
	a, m, g := Locale(t).Date()
	return Data{a, m, g}
}

// Oggi è la data del negozio adesso.
func Oggi(o Orologio) Data { return DataDi(o.Adesso()) }

// mezzogiorno è un istante dentro il giorno, lontano dai cambi dell'ora: serve solo per i
// conti sul calendario.
func (d Data) mezzogiorno() time.Time {
	return time.Date(d.Anno, d.Mese, d.Giorno, 12, 0, 0, 0, time.UTC)
}

// Formatta stampa la data così com'è, con il layout di chi chiama.
func (d Data) Formatta(layout string) string { return d.mezzogiorno().Format(layout) }

// String è la data nella forma aaaa-mm-gg.
func (d Data) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Anno, d.Mese, d.Giorno) }

// Sub è il numero di giorni da e a d (negativo se d viene prima). Conta sui secondi Unix e non
// su time.Duration, che satura oltre circa 292 anni (una data importata con l'anno sbagliato).
func (d Data) Sub(e Data) int {
	return int((d.mezzogiorno().Unix() - e.mezzogiorno().Unix()) / 86400)
}
