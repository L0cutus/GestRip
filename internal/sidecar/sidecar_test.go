package sidecar

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestControlla(t *testing.T) {
	buono := Comando{Nome: "estrazione", Programma: "/opt/gestrip/llama-server", Argomenti: []string{"--port", "{porta}"}, Porta: 47301}
	if err := buono.Controlla(); err != nil {
		t.Fatalf("comando buono rifiutato: %v", err)
	}
	casi := map[string]func(*Comando){
		"senza nome":         func(c *Comando) { c.Nome = "  " },
		"programma relativo": func(c *Comando) { c.Programma = "llama-server" },
		"programma vuoto":    func(c *Comando) { c.Programma = "" },
		"porta bassa":        func(c *Comando) { c.Porta = 1023 },
		"porta zero":         func(c *Comando) { c.Porta = 0 },
		"porta troppo alta":  func(c *Comando) { c.Porta = 65536 },
		"salute senza barra": func(c *Comando) { c.Salute = "health" },
		"salute con query":   func(c *Comando) { c.Salute = "/health?x=1" },
		"salute con spazio":  func(c *Comando) { c.Salute = "/he alth" },
		"salute con fram":    func(c *Comando) { c.Salute = "/health#x" },
		"salute altro host":  func(c *Comando) { c.Salute = "//altro/health" },
		"salute con tab":     func(c *Comando) { c.Salute = "/he\talth" },
		"salute con NUL":     func(c *Comando) { c.Salute = "/http://altro/health" + "\x00" },
		"salute con ritorno": func(c *Comando) { c.Salute = "/health\r" },
		"senza argomenti":    func(c *Comando) { c.Argomenti = nil },
		"senza {porta}":      func(c *Comando) { c.Argomenti = []string{"--port", "47301"} },
	}
	for nome, cambia := range casi {
		c := buono
		cambia(&c)
		if err := c.Controlla(); err == nil {
			t.Errorf("%s: accettato", nome)
		}
		if _, err := Nuovo(c, Tempi{}, nil); err == nil {
			t.Errorf("%s: Nuovo accetta", nome)
		}
	}
	for _, p := range []int{1024, 65535} {
		c := buono
		c.Porta = p
		if err := c.Controlla(); err != nil {
			t.Errorf("porta %d rifiutata: %v", p, err)
		}
	}
	c := buono
	c.Salute = "/v1/models"
	if err := c.Controlla(); err != nil {
		t.Errorf("salute /v1/models rifiutata: %v", err)
	}
}

func TestArgomentiEIndirizzo(t *testing.T) {
	c := Comando{Nome: "x", Programma: "/bin/srv", Porta: 47301,
		Argomenti: []string{"--port", "{porta}", "--host=127.0.0.1:{porta}", "-m", "/m.gguf"}}
	want := []string{"/bin/srv", "--port", "47301", "--host=127.0.0.1:47301", "-m", "/m.gguf"}
	if got := c.argomenti(); !slices.Equal(got, want) {
		t.Errorf("argomenti = %q, voglio %q", got, want)
	}
	if got := c.Indirizzo(); got != "http://127.0.0.1:47301" {
		t.Errorf("Indirizzo = %q", got)
	}
	s, err := Nuovo(c, Tempi{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.c.Salute != SaluteDiDefault || s.Indirizzo() != c.Indirizzo() {
		t.Errorf("salute %q, indirizzo %q", s.c.Salute, s.Indirizzo())
	}
	// Nuovo copia argomenti: cambiarli dopo non cambia il supervisore.
	c.Argomenti[0] = "--altro"
	if s.c.Argomenti[0] != "--port" {
		t.Errorf("Nuovo non copia gli argomenti")
	}
	if st := s.Stato(); st.Fase != FaseSpento || st.Pid != 0 || st.Avvii != 0 {
		t.Errorf("stato iniziale %+v", st)
	}
}

func TestTempiDiDefault(t *testing.T) {
	got := Tempi{}.conDefault()
	if got != TempiDiDefault {
		t.Errorf("tempi a zero = %+v, voglio i default", got)
	}
	d := TempiDiDefault
	if d.Avvio != 3*time.Minute || d.AttesaMinima != time.Second || d.AttesaMassima != 5*time.Minute ||
		d.Malato != 3 || d.Arresto != 10*time.Second || d.Controllo != 10*time.Second {
		t.Errorf("default cambiati: %+v", d)
	}
	// Un tetto più basso del minimo si alza al minimo.
	if got := (Tempi{AttesaMinima: time.Minute, AttesaMassima: time.Second}).conDefault(); got.AttesaMassima != time.Minute {
		t.Errorf("tetto = %v", got.AttesaMassima)
	}
	// I campi dati restano.
	dati := Tempi{Avvio: 1, PassoAvvio: 2, Controllo: 3, Risposta: 4, Malato: 5, AttesaMinima: 6, AttesaMassima: 7, Stabile: 8, Arresto: 9}
	if got := dati.conDefault(); got != dati {
		t.Errorf("conDefault cambia campi dati: %+v", got)
	}
}

func TestGenereAvvio(t *testing.T) {
	_, err := avvia(Comando{Nome: "x", Programma: "/non/esiste/srv-inventato", Porta: 47301})
	if err == nil {
		t.Fatal("parte un programma che non c'è")
	}
	if g := genereAvvio(err); g != "non trovato" {
		t.Errorf("genere = %q", g)
	}
	if strings.Contains(genereAvvio(err), "/") {
		t.Errorf("il genere contiene un percorso")
	}
}
