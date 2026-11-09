package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/accounts"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/ratelimit"
)

// La pagina del cliente /r/<token> sulla faccia pubblica (CLI-01, CLI-02, CLI-21), come
// public_repair_controller_test.exs e attempt_limits_test.exs dello standalone (go-baseline).

type provaCliente struct {
	h   http.Handler
	d   *db.DB
	b   *buste.Buste
	o   *clock.Finto
	reg *bytes.Buffer
}

func paginaClienteProva(t *testing.T) provaCliente {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	eseguiSQL(t, d, `INSERT INTO oreficerie (id, nome) VALUES (1, 'Il negozio'), (2, 'Oreficeria Esempio')`)
	eseguiSQL(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, telefono, email, indirizzo, creato_il, aggiornato_il)
		VALUES ('Bottega di Prova', '', '0490001122', 'bottega@esempio.it', 'Via del Negozio 1', ?, ?)`, istanteHome, istanteHome)
	reg := &bytes.Buffer{}
	dd := dipendenzeProva(&sondaFinta{pronto: true}, slog.New(slog.NewTextHandler(reg, &slog.HandlerOptions{Level: slog.LevelDebug})), accounts.NuoviUtenti(d, o))
	dd.Orologio, dd.Limiti = o, ratelimit.Nuovo(o)
	b := buste.Nuove(d, o)
	dd.Buste = b
	h, err := Pubblica(dd)
	if err != nil {
		t.Fatal(err)
	}
	return provaCliente{h: h, d: d, b: b, o: o, reg: reg}
}

func (p provaCliente) busta(t *testing.T, f func(*buste.Accettazione)) buste.Creata {
	t.Helper()
	a := buste.Accettazione{NomeCliente: "Mario Cliente Riservato", TelefonoCliente: "333 444 5566", DataAccettazione: "2026-10-31",
		Descrizione: "Collana da saldare", CodiceQR: "RIP-2026-0045", NoteInterne: "Nota interna riservata"}
	if f != nil {
		f(&a)
	}
	a.GemelleConfermate = buste.EtichettaScritta(a.CodiceQR, buste.AnnoDiAccettazione(a.DataAccettazione, p.o.Adesso()))
	c, err := p.b.Crea(context.Background(), a, audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPaginaClienteMostra(t *testing.T) {
	p := paginaClienteProva(t)
	c := p.busta(t, func(a *buste.Accettazione) { a.NotePubbliche = "Chiusura da cambiare"; a.Preventivo = "40" })
	audit0 := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`)
	b := nuovoBrowser(t, p.h)
	r := b.vai("GET", "/r/"+c.Token, nil, nil)
	if r.codice != http.StatusOK {
		t.Fatalf("codice %d: %s", r.codice, r.corpo)
	}
	for _, x := range []string{`id="public-shop-masthead" class="cliente-negozio">Bottega di Prova<`, ">La tua riparazione<",
		">0045/26<", ">In lavorazione<", "<dt>Accettata</dt><dd>2026-10-31</dd>", ">Collana da saldare<", ">Nota del laboratorio<",
		">Chiusura da cambiare<", `<p class="cliente-etichetta-prezzo">Preventivo</p><p class="cliente-prezzo">40</p>`,
		"Per informazioni contattare Bottega di Prova.", `<a id="public-privacy-link" href="/privacy">Informativa privacy</a>`,
		`name="csrf-token"`} {
		if !strings.Contains(r.corpo, x) {
			t.Errorf("manca %q", x)
		}
	}
	// Mai i dati del cliente, i recapiti del negozio, le note interne, il token o il numero interno.
	for _, x := range []string{"Mario", "Riservato", "333", "4445566", "0490001122", "bottega@esempio.it", "Via del Negozio",
		"Nota interna", c.Token, "1/26", "Prezzo finale", "Consegna prevista", "Pronta dal", "Tempo indicativo",
		"gs-fab", "manifest", "navbar"} {
		if strings.Contains(r.corpo, x) {
			t.Errorf("la pagina mostra %q", x)
		}
	}
	for k, v := range map[string]string{"Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
		"Content-Security-Policy": "base-uri 'self'; frame-ancestors 'self';", "X-Content-Type-Options": "nosniff",
		"X-Permitted-Cross-Domain-Policies": "none", "Content-Type": "text/html; charset=utf-8"} {
		if g := r.h.Get(k); g != v {
			t.Errorf("%s: %q, atteso %q", k, g, v)
		}
	}
	// Aprire la pagina non scrive nessuna riga d'audit (show/2), e il token non va nel registro.
	if n := contaSQL(t, p.d, `SELECT count(*) FROM eventi_audit`); n != audit0 {
		t.Errorf("righe d'audit: %d, prima %d", n, audit0)
	}
	if strings.Contains(p.reg.String(), c.Token) {
		t.Error("il token è nel registro")
	}
	// Gli asset della testa si servono anche sulla faccia pubblica.
	if r := b.vai("GET", percorsoAsset("app.css"), nil, nil); r.codice != http.StatusOK || !strings.Contains(r.corpo, ".cliente-scheda") {
		t.Errorf("app.css: %d", r.codice)
	}
	if r := b.vai("GET", indirizzoIcona192, nil, nil); r.codice != http.StatusOK {
		t.Errorf("icona: %d", r.codice)
	}
}

func TestPaginaClienteRifiutoUniforme(t *testing.T) {
	p := paginaClienteProva(t)
	ctx := context.Background()
	chi := audit.Contesto{Attore: "titolare@esempio.it"}
	revocata := p.busta(t, nil)
	if _, err := p.b.RevocaLink(ctx, revocata.ID, chi); err != nil {
		t.Fatal(err)
	}
	scaduta := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET token_pubblico_scade_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, scaduta.ID)
	cestino := p.busta(t, nil)
	eseguiSQL(t, p.d, `UPDATE buste SET cancellata_il = '2026-10-31T09:00:00.000000Z' WHERE id = ?`, cestino.ID)
	controparte := p.busta(t, nil)
	if _, err := p.b.Modifica(ctx, controparte.ID, buste.Modifica{OreficeriaID: ptrTesto("2")}, nil, chi); err != nil {
		t.Fatal(err)
	}
	b := nuovoBrowser(t, p.h)
	var prima *risposta
	for nome, token := range map[string]string{"inesistente": "QUESTO-TOKEN-NON-ESISTE-0000000000000000000",
		"revocato": revocata.Token, "scaduto": scaduta.Token, "cestino": cestino.Token, "controparte": controparte.Token} {
		r := b.vai("GET", "/r/"+token, nil, nil)
		if r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
			t.Errorf("%s: %d %q", nome, r.codice, r.corpo)
		}
		r.h.Del("Date")
		r.h.Del("X-Request-Id")
		if prima != nil && (r.corpo != prima.corpo || !stesseIntestazioni(r.h, prima.h)) {
			t.Errorf("%s: risposta diversa\n%v\n%v", nome, r.h, prima.h)
		}
		prima = &r
	}
}

func stesseIntestazioni(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if strings.Join(v, "\x00") != strings.Join(b.Values(k), "\x00") {
			return false
		}
	}
	return true
}

func TestPaginaClienteLimiteTentativi(t *testing.T) {
	p := paginaClienteProva(t)
	c := p.busta(t, nil)
	b := nuovoBrowser(t, p.h)
	b.ip = "198.51.100.23"
	// I token buoni non contano: molti clienti dietro lo stesso indirizzo.
	for range 40 {
		if r := b.vai("GET", "/r/"+c.Token, nil, nil); r.codice != http.StatusOK {
			t.Fatalf("token buono: %d", r.codice)
		}
	}
	for i := range 30 {
		if r := b.vai("GET", "/r/sbagliato-"+string(rune('a'+i%26)), nil, nil); r.codice != http.StatusNotFound {
			t.Fatalf("fallito %d: %d", i, r.codice)
		}
	}
	// Al 31°, anche il token giusto riceve lo stesso 404 per 15 minuti.
	r := b.vai("GET", "/r/"+c.Token, nil, nil)
	if r.codice != http.StatusNotFound || r.corpo != "Riparazione non disponibile" {
		t.Fatalf("bloccato: %d %q", r.codice, r.corpo)
	}
	// Un altro indirizzo non è bloccato.
	b.ip = "198.51.100.24"
	if r := b.vai("GET", "/r/"+c.Token, nil, nil); r.codice != http.StatusOK {
		t.Errorf("altro indirizzo: %d", r.codice)
	}
	b.ip = "198.51.100.23"
	p.o.Sposta(15*time.Minute + time.Second)
	if r := b.vai("GET", "/r/"+c.Token, nil, nil); r.codice != http.StatusOK {
		t.Errorf("dopo il blocco: %d", r.codice)
	}
}

func contaSQL(t *testing.T, d *db.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func ptrTesto(s string) *string { return &s }
