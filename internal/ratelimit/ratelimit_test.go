package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

func nuovo() (*Limitatore, *clock.Finto) {
	o := clock.NuovoFinto(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	return Nuovo(o), o
}

const ms = time.Millisecond

// inventario: CLI-21
// Il codice corto (/stato) si blocca al decimo fallito in 15 minuti, per 15 minuti; un altro
// indirizzo e un altro secchio non si toccano (attempt_limiter_test.exs).
func TestCortoBloccaAlDecimo(t *testing.T) {
	l, o := nuovo()
	for i := 1; i <= 9; i++ {
		o.Sposta(ms)
		if l.Fallito(Corto, "203.0.113.1") {
			t.Fatalf("bloccato al fallito %d", i)
		}
		if d := l.Bloccato(Corto, "203.0.113.1"); d != 0 {
			t.Fatalf("bloccato dopo %d falliti: %v", i, d)
		}
	}
	o.Sposta(ms)
	if !l.Fallito(Corto, "203.0.113.1") {
		t.Fatal("il decimo fallito non blocca")
	}
	if d := l.Bloccato(Corto, "203.0.113.1"); d != 15*time.Minute {
		t.Errorf("attesa = %v", d)
	}
	o.Sposta(15*time.Minute - ms)
	if d := l.Bloccato(Corto, "203.0.113.1"); d != ms {
		t.Errorf("un millisecondo prima della fine: %v", d)
	}
	if d := l.Bloccato(Corto, "203.0.113.2"); d != 0 {
		t.Errorf("altro indirizzo: %v", d)
	}
	if d := l.Bloccato(Token, "203.0.113.1"); d != 0 {
		t.Errorf("altro secchio: %v", d)
	}
	o.Sposta(ms)
	if d := l.Bloccato(Corto, "203.0.113.1"); d != 0 {
		t.Errorf("a 15 minuti esatti: %v", d)
	}
	// Dopo il blocco si riparte da zero.
	for i := 1; i <= 9; i++ {
		if l.Fallito(Corto, "203.0.113.1") {
			t.Fatalf("dopo il blocco, bloccato al fallito %d", i)
		}
	}
}

// inventario: CLI-21
// I falliti più vecchi della finestra non contano più.
func TestFinestra(t *testing.T) {
	l, o := nuovo()
	t0 := o.Adesso()
	for i := 1; i <= 9; i++ {
		o.Imposta(t0.Add(time.Duration(i) * ms))
		l.Fallito(Corto, "203.0.113.3")
	}
	o.Imposta(t0.Add(15*time.Minute + 5*ms))
	if l.Fallito(Corto, "203.0.113.3") || l.Bloccato(Corto, "203.0.113.3") != 0 {
		t.Error("contati falliti fuori dalla finestra")
	}
	// Quattro dentro (t0+6…9 ms) più questo: con altri cinque si arriva a dieci.
	for i := 1; i <= 4; i++ {
		if l.Fallito(Corto, "203.0.113.3") {
			t.Fatalf("bloccato troppo presto (%d)", i)
		}
	}
	if !l.Fallito(Corto, "203.0.113.3") {
		t.Error("i falliti dentro la finestra non contano")
	}
}

// inventario: CLI-21, ACC-20
// I limiti di ogni secchio: token 30 in 1 minuto, login 20 per indirizzo e 5 per indirizzo e
// account in 15 minuti; blocco sempre di 15 minuti.
func TestLimitiDeiSecchi(t *testing.T) {
	for _, c := range []struct {
		s        Secchio
		limite   int
		finestra time.Duration
	}{
		{Corto, 10, 15 * time.Minute},
		{Token, 30, time.Minute},
		{LoginIndirizzo, 20, 15 * time.Minute},
		{LoginAccount, 5, 15 * time.Minute},
	} {
		l, o := nuovo()
		for i := 1; i < c.limite; i++ {
			o.Sposta(ms)
			if l.Fallito(c.s, "k") {
				t.Fatalf("%v: bloccato al %d", c.s, i)
			}
		}
		o.Sposta(ms)
		if !l.Fallito(c.s, "k") || l.Bloccato(c.s, "k") != 15*time.Minute {
			t.Errorf("%v: nessun blocco al fallito %d", c.s, c.limite)
		}
		// Gli stessi falliti, ma l'ultimo appena fuori dalla finestra del primo: niente blocco.
		l, o = nuovo()
		t0 := o.Adesso()
		for i := 1; i < c.limite; i++ {
			o.Imposta(t0.Add(time.Duration(i) * ms))
			l.Fallito(c.s, "k")
		}
		o.Imposta(t0.Add(c.finestra + 2*ms))
		if l.Fallito(c.s, "k") {
			t.Errorf("%v: bloccato con il primo fallito fuori dalla finestra", c.s)
		}
	}
}

// inventario: CLI-21, ACC-20
// Una chiave azzerata riparte (login riuscito); mentre il blocco dura, un altro fallito non
// rinnova il blocco e non ne fa partire un secondo.
func TestAzzeraEFallitoDuranteIlBlocco(t *testing.T) {
	l, o := nuovo()
	for range 4 {
		l.Fallito(LoginAccount, "a")
	}
	l.Azzera(LoginAccount, "a")
	for i := 1; i <= 4; i++ {
		if l.Fallito(LoginAccount, "a") {
			t.Fatalf("dopo Azzera, bloccato al %d", i)
		}
	}
	if !l.Fallito(LoginAccount, "a") {
		t.Fatal("nessun blocco")
	}
	o.Sposta(10 * time.Minute)
	if l.Fallito(LoginAccount, "a") {
		t.Error("un fallito durante il blocco ne fa partire un altro")
	}
	if d := l.Bloccato(LoginAccount, "a"); d != 5*time.Minute {
		t.Errorf("il blocco è stato rinnovato: %v", d)
	}
	l.Azzera(LoginIndirizzo, "a") // un altro secchio non c'entra
	if l.Bloccato(LoginAccount, "a") == 0 {
		t.Error("Azzera di un altro secchio ha tolto il blocco")
	}
}

// Le chiavi scadute si tolgono (la pulizia ogni minuto dello standalone): la memoria non cresce
// con gli indirizzi che non tornano.
func TestPulizia(t *testing.T) {
	l, o := nuovo()
	for i := range 1000 {
		l.Fallito(Token, fmt.Sprint("198.51.100.", i))
	}
	o.Sposta(5 * time.Minute)
	for range 30 {
		l.Fallito(Token, "bloccato")
	}
	o.Sposta(10*time.Minute + time.Second)
	l.Fallito(Corto, "nuovo") // un'operazione qualunque fa partire la pulizia
	if n := l.quante(); n != 2 {
		t.Errorf("voci dopo la pulizia: %d (attese: il blocco ancora in corso e la nuova)", n)
	}
	o.Sposta(5 * time.Minute)
	l.Bloccato(Corto, "x")
	if n := l.quante(); n != 1 {
		t.Errorf("voci dopo la fine del blocco: %d (attesa: solo la nuova, ancora nella finestra)", n)
	}
}

// Più richieste insieme: nessun conteggio perso, un solo blocco.
func TestConcorrenza(t *testing.T) {
	l, _ := nuovo()
	var wg sync.WaitGroup
	var mu sync.Mutex
	blocchi := 0
	for range 100 {
		wg.Go(func() {
			if l.Fallito(Token, "k") {
				mu.Lock()
				blocchi++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if blocchi != 1 || l.Bloccato(Token, "k") == 0 {
		t.Errorf("blocchi = %d", blocchi)
	}
}

// Se l'orologio torna indietro (ora di sistema corretta), la pulizia non si ferma (review della
// 009): riparte dall'ora nuova.
func TestPuliziaConLOrologioIndietro(t *testing.T) {
	l, o := nuovo()
	l.Fallito(Token, "vecchia")
	o.Sposta(-time.Hour)
	l.Fallito(Token, "nuova")
	o.Sposta(16 * time.Minute)
	l.Bloccato(Corto, "x")
	if n := l.quante(); n != 1 {
		t.Errorf("voci dopo 16 minuti dall'orologio indietro: %d (attesa: la sola «vecchia», che è nel futuro)", n)
	}
}
