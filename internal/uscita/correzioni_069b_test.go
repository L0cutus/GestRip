package uscita

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// Le correzioni della review della 069b: il giro e l'invio a mano sotto lo stesso lucchetto, l'invio a
// mano che non si ferma con la richiesta, l'esito vero anche se l'audit non si scrive. Dati inventati.

// providerFermo: Consegna (il giro) si ferma finché il test non la libera; ConsegnaAMano conta.
type providerFermo struct {
	c                    *Coda
	dentro, libera       chan struct{}
	mu                   sync.Mutex
	inviiGiro, inviiMano int
}

func (p *providerFermo) Attivo(ctx context.Context) (bool, error) {
	return EmailAttiva(ctx, p.c.d.Lettura)
}

func (p *providerFermo) Consegna(context.Context, Messaggio) (string, error) {
	p.mu.Lock()
	p.inviiGiro++
	p.mu.Unlock()
	close(p.dentro)
	<-p.libera
	return "250 ok", nil
}

func (p *providerFermo) ConsegnaAMano(context.Context, Messaggio) (string, error) {
	p.mu.Lock()
	p.inviiMano++
	p.mu.Unlock()
	return "250 ok", nil
}

func (p *providerFermo) invii() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.inviiGiro + p.inviiMano
}

// inventario: OPS-02, CLI-51
// «Spegni» mentre il giro sta mandando un'email aspetta che il giro la chiuda: dopo, l'email non è più
// in coda, e «Manda adesso» non la manda una seconda volta.
func TestSpegniDuranteIlGiroNonMandaDueVolte(t *testing.T) {
	d, o := apriDB(t)
	p := &providerFermo{dentro: make(chan struct{}), libera: make(chan struct{})}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	p.c = c
	ctx := context.Background()
	if _, err := c.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "security": "none", "from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	if err := c.ImpostaEmail(ctx, true, contesto); err != nil {
		t.Fatal(err)
	}
	id, err := c.Accoda(ctx, Destinazione{Canale: CanaleEmail, Destinatario: "customer", Indirizzo: "cliente@example.com"}, Contenuto{Testo: "ciao"}, contesto)
	if err != nil {
		t.Fatal(err)
	}
	fatto := make(chan struct{})
	go func() { _, _ = c.Giro(ctx, PerGiro); close(fatto) }()
	<-p.dentro
	spento := make(chan error, 1)
	go func() { spento <- c.ImpostaEmail(ctx, false, contesto) }()
	select {
	case err := <-spento:
		// Spenta mentre il giro manda: «Manda adesso» trova la riga ancora in coda.
		if err != nil {
			t.Fatal(err)
		}
		_, _ = c.MandaInSospeso(ctx, id, contesto)
		close(p.libera)
	case <-time.After(300 * time.Millisecond):
		close(p.libera)
		if err := <-spento; err != nil {
			t.Fatal(err)
		}
		if _, err := c.MandaInSospeso(ctx, id, contesto); err == nil {
			t.Error("«Manda adesso» su una riga già accettata")
		}
	}
	<-fatto
	if n := p.invii(); n != 1 {
		t.Errorf("la stessa email è partita %d volte", n)
	}
}

// providerCheChiude: prende l'email, e proprio allora il browser chiude la richiesta.
type providerCheChiude struct {
	annulla context.CancelFunc
	invii   int
}

func (p *providerCheChiude) Consegna(context.Context, Messaggio) (string, error) {
	return "250 ok", nil
}

func (p *providerCheChiude) ConsegnaAMano(context.Context, Messaggio) (string, error) {
	p.invii++
	if p.annulla != nil {
		p.annulla()
		p.annulla = nil
	}
	return "250 ok", nil
}

// inventario: OPS-02
// «Manda adesso» con la richiesta annullata dopo che il provider ha preso l'email: l'esito si scrive lo
// stesso (la riga è accettata), e un secondo «Manda adesso» non la manda di nuovo.
func TestMandaInSospesoRichiestaChiusa(t *testing.T) {
	d, o := apriDB(t)
	p := &providerCheChiude{}
	c := Nuova(d, o, map[string]Provider{CanaleEmail: p})
	bg := context.Background()
	if _, err := c.SalvaSMTP(bg, ModuloSMTP{"host": "smtp.example.invalid", "security": "none", "from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	inserisciRighe(t, c)
	ctx, annulla := context.WithCancel(bg)
	p.annulla = annulla
	e, err := c.MandaInSospeso(ctx, 5, contesto)
	if err != nil || e.Stato != "accepted" {
		t.Errorf("primo: %+v %v", e, err)
	}
	if got := riga(t, d, 5); !strings.HasPrefix(got, "accepted|1|") {
		t.Errorf("riga %s", got)
	}
	_, _ = c.MandaInSospeso(bg, 5, contesto)
	if p.invii != 1 {
		t.Errorf("email mandata %d volte", p.invii)
	}
}

// inventario: OPS-02
// L'audit di «Manda adesso» che non si scrive dopo un invio riuscito: l'esito è quello vero (accettata),
// e nel registro del server una riga senza indirizzo né testo.
func TestMandaInSospesoAuditNonScritto(t *testing.T) {
	d, o := apriDB(t)
	var reg bytes.Buffer
	c := Nuova(d, o, map[string]Provider{CanaleEmail: &providerCheChiude{}}).ConRegistro(slog.New(slog.NewTextHandler(&reg, nil)))
	ctx := context.Background()
	if _, err := c.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "security": "none", "from_address": "negozio@example.invalid"}, contesto); err != nil {
		t.Fatal(err)
	}
	inserisciRighe(t, c)
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER niente_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'outgoing.queued_sent_by_hand'
		BEGIN SELECT RAISE(ABORT, 'audit rotto'); END`); err != nil {
		t.Fatal(err)
	}
	e, err := c.MandaInSospeso(ctx, 5, contesto)
	if err != nil || e.Stato != "accepted" {
		t.Fatalf("esito %+v %v", e, err)
	}
	if !strings.Contains(reg.String(), "audit") || strings.Contains(reg.String(), "cliente-cinque") || strings.Contains(reg.String(), "Pronta") {
		t.Errorf("registro %q", reg.String())
	}
}

// inventario: ADM-09, CLI-52
// configured nell'audit dice il vero anche quando è falso (senza mittente); un mittente con un solo a
// capo finale passa come nello standalone («$» di PCRE), uno con l'a capo in mezzo no.
func TestSalvaSMTPConfiguredEACapoFinale(t *testing.T) {
	c, _ := codaSMTP(t)
	ctx := context.Background()
	if rif, err := c.SalvaSMTP(ctx, ModuloSMTP{"host": "smtp.example.invalid", "from_address": ""}, contesto); err != nil || rif != nil {
		t.Fatalf("%v %+v", err, rif)
	}
	if _, dati := ultimoAudit(t, c, "outgoing.provider_configured"); dati != `{"changed":["host"],"channel":"email","configured":false}` {
		t.Errorf("audit %s", dati)
	}
	if rif, err := c.SalvaSMTP(ctx, ModuloSMTP{"from_address": "negozio@example.it\n"}, contesto); err != nil || rif != nil {
		t.Errorf("a capo finale: %v %+v", err, rif)
	}
	if rif, _ := c.SalvaSMTP(ctx, ModuloSMTP{"from_address": "negozio@exa\nmple.it"}, contesto); rif == nil {
		t.Error("a capo in mezzo accettato")
	}
}
