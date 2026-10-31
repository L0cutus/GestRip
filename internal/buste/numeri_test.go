package buste

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// La numerazione N/AA per anno (RIP-H-31), come peek_next_bag_number/1, resolve_bag_number/3,
// advance_bag_sequence/2 e next_bag_number/2 di workshop.ex; i casi da workshop_test.exs.

// inventario: RIP-H-31
// L'anteprima è il prossimo numero dell'anno di oggi e non scrive niente; dopo una busta è il
// numero successivo. Un anno chiesto a parte ha il suo contatore.
func TestAnteprimaDelNumero(t *testing.T) {
	b, d, _ := prova(t)
	n, err := b.ProssimoNumero(ctx)
	if err != nil || n != "1/26" {
		t.Fatalf("anteprima %q %v, attesa 1/26", n, err)
	}
	if conta(t, d, `SELECT count(*) FROM sequenze_buste`) != 0 {
		t.Error("l'anteprima ha scritto il contatore")
	}
	crea(t, b, modulo())
	if n, _ := b.ProssimoNumero(ctx); n != "2/26" {
		t.Errorf("anteprima dopo una busta %q, attesa 2/26", n)
	}
	if n, _ := b.ProssimoNumeroDellAnno(ctx, 2025); n != "1/25" {
		t.Errorf("anteprima del 2025 %q, attesa 1/25", n)
	}
	if n, _ := b.ProssimoNumeroDellAnno(ctx, 2007); n != "1/07" {
		t.Errorf("anno con lo zero davanti %q, atteso 1/07", n)
	}
}

// inventario: RIP-H-31
// L'anteprima usa l'anno di oggi in negozio (Europe/Rome): alle 23:30 UTC del 31/12 è già
// l'anno nuovo.
func TestAnteprimaSulGiornoDelNegozio(t *testing.T) {
	b, _, o := prova(t)
	o.Imposta(time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC))
	if n, _ := b.ProssimoNumero(ctx); n != "1/27" {
		t.Errorf("anteprima %q, attesa 1/27", n)
	}
}

// inventario: RIP-H-31
// Un numero manuale si usa com'è e alza il contatore del suo anno almeno a quel valore; uno più
// basso non lo abbassa. La riga d'audit porta il numero assegnato.
func TestNumeroManualeAlzaIlContatore(t *testing.T) {
	b, d, _ := prova(t)
	for _, c := range []struct{ scritto, atteso string }{
		{"7/26", "7/26"}, {"", "8/26"}, {"3/26", "3/26"}, {"", "9/26"},
	} {
		got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = c.scritto }))
		if got.Numero != c.atteso {
			t.Fatalf("scritto %q: numero %q, atteso %q", c.scritto, got.Numero, c.atteso)
		}
	}
	if s := ultimoAudit(t, d, "repair.created"); !strings.Contains(s, `"bag_number":"9/26"`) {
		t.Errorf("audit %q", s)
	}
}

// inventario: RIP-H-31
// Un numero manuale di un altro anno alza solo quel contatore; il numero automatico usa l'anno
// della data di accettazione, non quello di oggi.
func TestNumeroManualeDiUnAltroAnno(t *testing.T) {
	b, _, _ := prova(t)
	if got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "3/25" })); got.Numero != "3/25" {
		t.Fatalf("manuale %q", got.Numero)
	}
	if got := crea(t, b, modulo()); got.Numero != "1/26" {
		t.Errorf("automatico di quest'anno %q, atteso 1/26", got.Numero)
	}
	if got := crea(t, b, con(modulo(), func(a *Accettazione) { a.DataAccettazione = "2025-12-01" })); got.Numero != "4/25" {
		t.Errorf("automatico del 2025 %q, atteso 4/25", got.Numero)
	}
}

// inventario: RIP-H-31
// Fino a cinque cifre (divergenza voluta dal Flask): 9999/26, poi 10000/26 automatico, 99999/26
// manuale.
func TestNumeroOltreLeQuattroCifre(t *testing.T) {
	b, _, _ := prova(t)
	crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "9999/26" }))
	if n, _ := b.ProssimoNumeroDellAnno(ctx, 2026); n != "10000/26" {
		t.Errorf("anteprima %q", n)
	}
	if got := crea(t, b, modulo()); got.Numero != "10000/26" {
		t.Errorf("automatico %q", got.Numero)
	}
	if got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "99999/26" })); got.Numero != "99999/26" {
		t.Errorf("manuale %q", got.Numero)
	}
}

// inventario: RIP-H-31
// Un numero fuori formato è rifiutato con «formato richiesto N/AA (es. 12/26)» e non lascia
// niente: né busta, né cliente, né contatore, né audit.
func TestNumeroManualeFormatoNonValido(t *testing.T) {
	b, d, _ := prova(t)
	prima := scatta(t, d)
	for _, n := range []string{"123456/26", "1/266", "1/2", "1-26", "abc", "1/", "/26", "1 /26", "1/ 26", "١/26", "1/26\n1/26"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = n }), chi)
		c := erroriCampi(t, err)
		if fmt.Sprint(c["numero"]) != "[formato richiesto N/AA (es. 12/26)]" {
			t.Errorf("%q: errori %v", n, c)
		}
	}
	uguali(t, "numero fuori formato", prima, scatta(t, d))
}

// inventario: RIP-H-31
// Un numero già usato, anche da una busta nel cestino, è rifiutato con «Numero busta già
// esistente.» e mai rinumerato; il contatore non si muove e il numero automatico dopo è il
// successivo.
func TestNumeroManualeGiaEsistente(t *testing.T) {
	b, d, _ := prova(t)
	primo := crea(t, b, modulo())
	nel := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = "40/26" }))
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, nel.ID)
	prima := scatta(t, d)
	for _, n := range []string{"1/26", " 1/26 ", "40/26"} {
		_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = n; a.NomeCliente = "Cliente Nuovo" }), chi)
		c := erroriCampi(t, err)
		if fmt.Sprint(c["numero"]) != "[Numero busta già esistente.]" || len(c) != 1 {
			t.Errorf("%q: errori %v", n, c)
		}
	}
	uguali(t, "numero già esistente", prima, scatta(t, d))
	if got := crea(t, b, modulo()); got.Numero != "41/26" {
		t.Errorf("automatico dopo %q, atteso 41/26", got.Numero)
	}
	if valore(t, d, `SELECT numero FROM buste WHERE id = ?`, primo.ID) != "1/26" {
		t.Error("la prima busta è stata rinumerata")
	}
}

// inventario: RIP-H-31
// Il numero già esistente si dice solo quando il resto del modulo è giusto (il vincolo unico
// arriva dopo le altre regole, come nello standalone).
func TestNumeroEsistenteDopoGliAltriErrori(t *testing.T) {
	b, _, _ := prova(t)
	crea(t, b, modulo())
	_, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.Numero = "1/26"; a.Descrizione = "" }), chi)
	c := erroriCampi(t, err)
	if len(c) != 1 || fmt.Sprint(c["descrizione"]) != "[can't be blank]" {
		t.Errorf("errori %v", c)
	}
}

// inventario: RIP-H-31
// Vuoto o fatto di spazi vuol dire numero automatico; uno manuale si salva senza gli spazi ai
// bordi.
func TestNumeroVuotoAssegnaAutomatico(t *testing.T) {
	b, _, _ := prova(t)
	for _, c := range []struct{ scritto, atteso string }{{"", "1/26"}, {"   ", "2/26"}, {" 5/26 ", "5/26"}, {"\t", "6/26"}} {
		if got := crea(t, b, con(modulo(), func(a *Accettazione) { a.Numero = c.scritto })); got.Numero != c.atteso {
			t.Errorf("scritto %q: %q, atteso %q", c.scritto, got.Numero, c.atteso)
		}
	}
}

// inventario: RIP-H-31
// L'assegnazione automatica è atomica: otto accettazioni insieme hanno otto numeri diversi e
// consecutivi.
func TestNumeroAutomaticoAtomico(t *testing.T) {
	b, _, _ := prova(t)
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		numeri []string
	)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := b.Crea(ctx, con(modulo(), func(a *Accettazione) { a.NomeCliente = fmt.Sprintf("Cliente %d", i) }), chi)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			numeri = append(numeri, c.Numero)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(numeri)
	if strings.Join(numeri, ",") != "1/26,2/26,3/26,4/26,5/26,6/26,7/26,8/26" {
		t.Errorf("numeri %v", numeri)
	}
}
