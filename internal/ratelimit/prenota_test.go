package ratelimit

import (
	"sync"
	"testing"
	"time"
)

// I casi di «reserving before the check (slice 219)» di attempt_limiter_test.exs: un posto si
// prenota prima della verifica, atomicamente; il fallito lo trasforma, la risposta buona lo
// restituisce.

// inventario: CLI-21, ACC-20
// Sessanta prenotazioni insieme: passa esattamente il limite, le altre hanno un'attesa.
func TestSessantaPrenotazioniInsieme(t *testing.T) {
	l, _ := nuovo()
	var wg sync.WaitGroup
	var mu sync.Mutex
	passate, attese := 0, 0
	for range 60 {
		wg.Go(func() {
			b, attesa := l.Prenota(LoginAccount, "ip\x00a")
			mu.Lock()
			defer mu.Unlock()
			if attesa == 0 && b != 0 {
				passate++
			} else if attesa > 0 && b == 0 {
				attese++
			}
		})
	}
	wg.Wait()
	if passate != 5 || attese != 55 {
		t.Errorf("passate %d, attese %d", passate, attese)
	}
}

// inventario: CLI-21
// Una prenotazione restituita non conta; una fallita conta una volta.
func TestPrenotazioneRestituitaOFallita(t *testing.T) {
	l, o := nuovo()
	for range 50 {
		b, attesa := l.Prenota(Corto, "k")
		if attesa != 0 {
			t.Fatal("prenotazione rifiutata")
		}
		l.Rilascia(Corto, "k", b)
	}
	for i := 1; i <= 9; i++ {
		b, _ := l.Prenota(Corto, "k")
		if l.Fallito(Corto, "k", b) {
			t.Fatalf("blocco al fallito %d", i)
		}
	}
	b, _ := l.Prenota(Corto, "k")
	if !l.Fallito(Corto, "k", b) {
		t.Fatal("il decimo fallito non blocca")
	}
	o.Sposta(ms)
	if b, attesa := l.Prenota(Corto, "k"); b != 0 || attesa != 15*time.Minute-ms {
		t.Errorf("da bloccato: %v %v", b, attesa)
	}
}

// inventario: CLI-21, ACC-20
// Le verifiche in corso tengono il loro posto: con i posti pieni l'attesa è breve (un secondo, non
// la finestra) e non è un blocco; restituito un posto, il successivo passa.
func TestPostiInCorso(t *testing.T) {
	l, _ := nuovo()
	var b [5]Biglietto
	for i := range b {
		b[i], _ = l.Prenota(LoginAccount, "k")
	}
	if x, attesa := l.Prenota(LoginAccount, "k"); x != 0 || attesa <= 0 || attesa > time.Second {
		t.Errorf("posti pieni: %v %v", x, attesa)
	}
	if d := l.Bloccato(LoginAccount, "k"); d != 0 {
		t.Errorf("aspettare non è un blocco: %v", d)
	}
	l.Rilascia(LoginAccount, "k", b[0])
	if x, attesa := l.Prenota(LoginAccount, "k"); x == 0 || attesa != 0 {
		t.Errorf("dopo un posto restituito: %v %v", x, attesa)
	}
}

// inventario: CLI-21, ACC-20
// Le verifiche in corso non fanno partire il blocco: contano solo i falliti.
func TestInCorsoNonBloccano(t *testing.T) {
	l, _ := nuovo()
	for range 4 {
		l.Prenota(LoginAccount, "k")
	}
	b, _ := l.Prenota(LoginAccount, "k")
	if l.Fallito(LoginAccount, "k", b) || l.Bloccato(LoginAccount, "k") != 0 {
		t.Error("un fallito con quattro verifiche in corso ha bloccato")
	}
}

// inventario: CLI-21, ACC-20
// Falliti e verifiche in corso riempiono i posti insieme: attesa breve, nessun blocco.
func TestFallitiEInCorsoRiempionoIPosti(t *testing.T) {
	l, _ := nuovo()
	for range 4 {
		l.Fallito(LoginIndirizzo, "k", 0)
	}
	for range 16 {
		if _, attesa := l.Prenota(LoginIndirizzo, "k"); attesa != 0 {
			t.Fatal("rifiutata prima del limite")
		}
	}
	if x, attesa := l.Prenota(LoginIndirizzo, "k"); x != 0 || attesa <= 0 || attesa > time.Second {
		t.Errorf("posti pieni: %v %v", x, attesa)
	}
	if l.Bloccato(LoginIndirizzo, "k") != 0 {
		t.Error("blocco senza venti falliti")
	}
}

// inventario: CLI-21
// Una prenotazione mai sistemata esce con la finestra.
func TestPrenotazioneOrfanaEsceConLaFinestra(t *testing.T) {
	l, o := nuovo()
	for range 10 {
		l.Prenota(Corto, "k")
	}
	if b, _ := l.Prenota(Corto, "k"); b != 0 {
		t.Fatal("undicesima prenotazione accettata")
	}
	o.Sposta(15*time.Minute + ms)
	if b, _ := l.Prenota(Corto, "k"); b == 0 {
		t.Error("le prenotazioni orfane non sono uscite con la finestra")
	}
}

// Un biglietto vale solo per la sua chiave, e due prenotazioni non hanno mai lo stesso biglietto:
// restituire il biglietto di «b» sotto «a» non libera il posto di «b».
func TestBigliettiDistinti(t *testing.T) {
	l, _ := nuovo()
	a, _ := l.Prenota(Corto, "a")
	b, _ := l.Prenota(Corto, "b")
	if a == 0 || a == b {
		t.Fatalf("biglietti %v %v", a, b)
	}
	l.Rilascia(Corto, "a", b)
	for i := 2; i <= 10; i++ {
		if x, _ := l.Prenota(Corto, "b"); x == 0 {
			t.Fatalf("prenotazione %d di b rifiutata", i)
		}
	}
	if x, attesa := l.Prenota(Corto, "b"); x != 0 || attesa == 0 {
		t.Error("il rilascio sotto un'altra chiave ha liberato un posto di b")
	}
}

// inventario: CLI-21
// Un biglietto restituito due volte, o inventato, non libera il posto di un'altra verifica in
// corso sulla stessa chiave.
func TestBigliettoGiaRestituito(t *testing.T) {
	l, _ := nuovo()
	x, _ := l.Prenota(Corto, "k")
	l.Prenota(Corto, "k") // un'altra verifica in corso
	l.Rilascia(Corto, "k", x)
	l.Rilascia(Corto, "k", x)
	l.Rilascia(Corto, "k", 999)
	for i := 2; i <= 10; i++ {
		if b, _ := l.Prenota(Corto, "k"); b == 0 {
			t.Fatalf("prenotazione %d rifiutata", i)
		}
	}
	if b, _ := l.Prenota(Corto, "k"); b != 0 {
		t.Error("il biglietto già restituito ha liberato il posto di un'altra verifica")
	}
}

// inventario: CLI-21, ACC-20
// Fallito trasforma il posto prenotato, non ne aggiunge un altro: nove prenotazioni fallite più una
// in corso lasciano libero un posto.
func TestFallitoTrasformaIlPosto(t *testing.T) {
	l, _ := nuovo()
	for range 9 {
		b, _ := l.Prenota(Corto, "k")
		l.Fallito(Corto, "k", b)
	}
	if b, attesa := l.Prenota(Corto, "k"); b == 0 {
		t.Fatalf("decima prenotazione rifiutata (%v): i falliti hanno tenuto anche il posto", attesa)
	}
}
