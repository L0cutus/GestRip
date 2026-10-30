package clock

import (
	"sync"
	"testing"
	"time"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// inventario: JOB-01
// Roma è UTC+1 d'inverno e UTC+2 d'estate; ogni ora stampata è quella del negozio, mai quella
// del sistema. Vettori copiati da clock_test.exs dello standalone.
func TestTimbro(t *testing.T) {
	casi := []struct{ utc, atteso string }{
		{"2026-01-15T08:05:00Z", "15/01/2026 09:05"}, // inverno
		{"2026-07-15T08:05:00Z", "15/07/2026 10:05"}, // estate
		{"2026-07-15T22:30:00Z", "16/07/2026 00:30"}, // sera tardi UTC, domani in negozio
		{"2026-12-31T23:30:00Z", "01/01/2027 00:30"},
		{"2026-03-29T00:59:00Z", "29/03/2026 01:59"}, // cambi dell'ora
		{"2026-03-29T01:00:00Z", "29/03/2026 03:00"},
		{"2026-10-25T00:30:00Z", "25/10/2026 02:30"},
		{"2026-10-25T01:30:00Z", "25/10/2026 02:30"},
		{"2026-07-15T08:05:59Z", "15/07/2026 10:05"}, // niente secondi né sigla del fuso
	}
	for _, c := range casi {
		if got := Timbro(utc(c.utc)); got != c.atteso {
			t.Errorf("Timbro(%s) = %q, atteso %q", c.utc, got, c.atteso)
		}
	}
}

// inventario: JOB-01
func TestFormatta(t *testing.T) {
	casi := []struct{ utc, layout, atteso string }{
		{"2026-07-15T21:59:07Z", "15:04:05", "23:59:07"},
		{"2026-07-15T22:00:00Z", "02/01/2006", "16/07/2026"},
		{"2026-01-15T22:59:00Z", "2006-01-02 15:04:05", "2026-01-15 23:59:00"},
		{"2026-01-15T23:00:00Z", "2006-01-02", "2026-01-16"},
	}
	for _, c := range casi {
		if got := Formatta(utc(c.utc), c.layout); got != c.atteso {
			t.Errorf("Formatta(%s, %q) = %q, atteso %q", c.utc, c.layout, got, c.atteso)
		}
	}
	// Un istante in un altro fuso si legge comunque sull'orologio del negozio.
	ny := utc("2026-07-15T08:05:00Z").In(time.FixedZone("NY", -4*3600))
	if got := Timbro(ny); got != "15/07/2026 10:05" {
		t.Errorf("Timbro da un altro fuso = %q", got)
	}
}

// inventario: JOB-01
// Una data senza ora si stampa così com'è: un giorno non ha fuso.
func TestDataSiStampaComE(t *testing.T) {
	if got := (Data{2026, time.July, 15}).Formatta("02/01/2006"); got != "15/07/2026" {
		t.Errorf("= %q", got)
	}
	if got := (Data{2026, time.January, 1}).Formatta("02/01"); got != "01/01" {
		t.Errorf("= %q", got)
	}
	if got := (Data{2026, time.January, 1}).String(); got != "2026-01-01" {
		t.Errorf("String = %q", got)
	}
}

// inventario: JOB-01
func TestDataDi(t *testing.T) {
	casi := []struct {
		utc    string
		attesa Data
	}{
		{"2026-01-15T22:59:59Z", Data{2026, time.January, 15}},
		{"2026-01-15T23:00:00Z", Data{2026, time.January, 16}},
		{"2026-07-15T21:59:59Z", Data{2026, time.July, 15}},
		{"2026-07-15T22:00:00Z", Data{2026, time.July, 16}},
		{"2026-12-31T23:00:00Z", Data{2027, time.January, 1}}, // l'anno nuovo inizia a mezzanotte in negozio
	}
	for _, c := range casi {
		if got := DataDi(utc(c.utc)); got != c.attesa {
			t.Errorf("DataDi(%s) = %v, attesa %v", c.utc, got, c.attesa)
		}
	}
}

// inventario: JOB-01
// Il fuso del sistema non conta: con TZ diverso il risultato non cambia.
func TestNonDipendeDalFusoDelSistema(t *testing.T) {
	vecchio := time.Local
	defer func() { time.Local = vecchio }()
	time.Local = time.FixedZone("Altrove", -10*3600)
	if got := Timbro(utc("2026-01-15T08:05:00Z")); got != "15/01/2026 09:05" {
		t.Errorf("con un altro fuso di sistema Timbro = %q", got)
	}
	if Roma.String() != "Europe/Rome" {
		t.Errorf("fuso = %q", Roma)
	}
}

// inventario: JOB-01
func TestOrologi(t *testing.T) {
	prima := time.Now().UTC()
	s := Sistema{}.Adesso()
	if s.Location() != time.UTC || s.Before(prima) || s.Sub(prima) > time.Minute {
		t.Errorf("Sistema.Adesso = %v (fuso %v), atteso UTC vicino a %v", s, s.Location(), prima)
	}
	ua, um, ug := prima.Date() // la data UTC: Roma è avanti, quindi oggi in negozio è quella o la successiva
	if d := Oggi(Sistema{}).Sub(Data{ua, um, ug}); d != 0 && d != 1 {
		t.Errorf("Oggi dista %d giorni dalla data UTC di adesso", d)
	}

	f := NuovoFinto(utc("2026-10-24T23:30:00Z"))
	if got := Oggi(f); got != (Data{2026, time.October, 25}) {
		t.Errorf("Oggi = %v", got)
	}
	f.Sposta(2 * time.Hour) // attraverso il ritorno all'ora solare
	if got := Timbro(f.Adesso()); got != "25/10/2026 02:30" {
		t.Errorf("dopo Sposta: %q", got)
	}
	f.Imposta(utc("2026-12-31T22:59:59Z"))
	if got := Oggi(f); got != (Data{2026, time.December, 31}) {
		t.Errorf("dopo Imposta: %v", got)
	}
	f.Imposta(utc("2026-12-31T19:00:00Z").In(time.FixedZone("NY", -4*3600)))
	if f.Adesso().Location() != time.UTC {
		t.Error("il finto deve dare istanti UTC")
	}
	f.Imposta(utc("2026-12-31T22:59:59Z"))
	var wg sync.WaitGroup // il finto si usa da più goroutine (job e richieste)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); f.Sposta(time.Second); _ = f.Adesso() }()
	}
	wg.Wait()
	if got := f.Adesso(); !got.Equal(utc("2026-12-31T23:00:07Z")) {
		t.Errorf("dopo 8 spostamenti concorrenti: %v", got)
	}
}

// inventario: JOB-01
func TestDataSub(t *testing.T) {
	a, b := Data{2026, time.March, 31}, Data{2026, time.March, 28} // attraverso il cambio dell'ora
	if a.Sub(b) != 3 || b.Sub(a) != -3 {
		t.Errorf("Sub = %d, %d", a.Sub(b), b.Sub(a))
	}
	if n := (Data{2027, time.January, 1}).Sub(Data{2026, time.January, 1}); n != 365 {
		t.Errorf("un anno = %d giorni", n)
	}
	if n := (Data{9999, time.December, 31}).Sub(Data{1, time.January, 1}); n != 3652058 {
		t.Errorf("da 1 a 9999 = %d giorni, attesi 3652058 (nessuna saturazione)", n)
	}
	if (Data{2027, time.January, 1}).Sub(Data{2026, time.December, 31}) != 1 {
		t.Error("fine anno")
	}
}
