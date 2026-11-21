package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"

	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La raccolta dei dettati dalla pagina «Nuova riparazione» (raccolta.go, slice 113; RIP-H-48), come «the intake» di
// training_collection_live_test.exs dello standalone a go-baseline: l'estrazione riuscita lascia una cattura legata a
// questa pagina (sessione e chiave del modulo), «Genera busta» la passa al salvataggio e la consuma. Il modello è quello
// finto; dettati, nomi e telefoni inventati.

const dettatoRaccolta = "Cliente Ofelia Inventata 333 000 0113, anello da stringere di una misura, preventivo 40"

// accendiRaccolta accende la raccolta con rimaste buste nel lotto (in attesa della pagina della 114).
func (p *dettatoProva) accendiRaccolta(t *testing.T, rimaste string) {
	t.Helper()
	eseguiSQL(t, p.d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.training.collect', '1', '2026-03-01T09:00:00.000000Z'),
		('assist.training.remaining', ?, '2026-03-01T09:00:00.000000Z')`, rimaste)
}

func (p *dettatoProva) impostazione(t *testing.T, chiave string) string {
	t.Helper()
	var v string
	if err := p.d.Lettura.QueryRow(`SELECT coalesce((SELECT valore FROM impostazioni WHERE chiave = ?), '(assente)')`, chiave).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// estraiPagina manda il dettato con la sola chiave del modulo (come app.js porta il modulo della busta), o senza niente.
func (p *dettatoProva) estraiPagina(t *testing.T, testo, chiave string) risposta {
	t.Helper()
	m := url.Values{}
	if chiave != "" {
		m.Set("intake_key", chiave)
	}
	r := p.estrai(testo, m)
	if r.codice != 200 {
		t.Fatalf("estrai %d", r.codice)
	}
	return r
}

// salvaPagina manda «Genera busta» con i valori che la pagina mostra, la sua chiave e un invio nuovo; qr è l'etichetta.
func (p *dettatoProva) salvaPagina(t *testing.T, pagina, qr string, cambia func(url.Values)) risposta {
	t.Helper()
	return p.b.vai("POST", "/nuova", p.moduloPagina(pagina, qr, cambia), nil)
}

// moduloPagina è il modulo di salvaPagina.
func (p *dettatoProva) moduloPagina(pagina, qr string, cambia func(url.Values)) url.Values {
	v := url.Values{"_csrf_token": {p.b.token}, "invio": {casuale()}, "qr_avvisato": {"1"}}
	for k, x := range valoriModulo(pagina) {
		v.Set("repair["+k+"]", x)
	}
	if m := reChiaveCampo.FindStringSubmatch(pagina); m != nil {
		v.Set("intake_key", m[1])
	}
	v.Set("repair[qr_code]", qr)
	v.Set("gemelle_avvisate", qr)
	if cambia != nil {
		cambia(v)
	}
	return v
}

type rigaRaccolta struct {
	busta                    int64
	testo, proposta, modello string
}

func (p *dettatoProva) righeRaccolta(t *testing.T) []rigaRaccolta {
	t.Helper()
	rows, err := p.d.Lettura.Query(`SELECT busta_id, testo, proposta, coalesce(modello, '(nulla)') FROM dettati_addestramento ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var rr []rigaRaccolta
	for rows.Next() {
		var r rigaRaccolta
		if err := rows.Scan(&r.busta, &r.testo, &r.proposta, &r.modello); err != nil {
			t.Fatal(err)
		}
		rr = append(rr, r)
	}
	return rr
}

func ultimaBusta(t *testing.T, p *dettatoProva) int64 {
	t.Helper()
	var id int64
	if err := p.d.Lettura.QueryRow(`SELECT max(id) FROM buste`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// inventario: RIP-H-48
// Un'estrazione e poi il salvataggio lasciano una riga: il dettato senza spazi ai bordi, la proposta del modello (anche se
// la persona ha corretto il modulo), il modello che ha risposto; il lotto scende di uno. Dopo, la prossima busta della
// stessa pagina, senza un'estrazione nuova, non ne scrive un'altra (il salvataggio consuma la cattura).
func TestRaccoltaDallaPagina(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "500")
	p.m.Programma(modellofinto.Risposta{Contenuto: modellofinto.Testo(`{"n": "ofelia inventata", "d": "anello da stringere di una misura", "p": "40"}`),
		Modello: modellofinto.Testo("gestrip-inventato-v3")})
	r := p.estraiPagina(t, "  "+dettatoRaccolta+"\n", "")
	chiave := chiaveDi(t, r.corpo)
	if s := p.salvaPagina(t, r.corpo, "QR-R113", func(v url.Values) { v.Set("repair[description]", "corretta a mano") }); s.codice != 302 {
		t.Fatalf("salvataggio %d", s.codice)
	}
	rr := p.righeRaccolta(t)
	if len(rr) != 1 {
		t.Fatalf("righe %d", len(rr))
	}
	if rr[0].busta != ultimaBusta(t, p) || rr[0].testo != dettatoRaccolta || rr[0].modello != "gestrip-inventato-v3" {
		t.Errorf("riga %+v", rr[0])
	}
	var prop map[string]any
	if err := json.Unmarshal([]byte(rr[0].proposta), &prop); err != nil {
		t.Fatal(err)
	}
	if prop["nome"] != "Ofelia Inventata" || prop["telefono"] != "3330000113" || prop["descrizione"] != "anello da stringere di una misura" ||
		prop["preventivo"] != "40" || prop["data"] != "13/03/2026" || prop["compro_oro"] != float64(0) {
		t.Errorf("proposta %v", prop)
	}
	if v := p.impostazione(t, "assist.training.remaining"); v != "499" {
		t.Errorf("rimaste %s", v)
	}
	// Lo stesso modulo (stessa chiave) mandato di nuovo con un invio nuovo: la cattura è già consumata.
	if s := p.salvaPagina(t, r.corpo, "QR-R113B", func(v url.Values) { v.Set("intake_key", chiave) }); s.codice != 302 {
		t.Fatalf("secondo salvataggio %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 1 || contaBuste(t, p.d) != 2 {
		t.Errorf("righe %d, buste %d", n, contaBuste(t, p.d))
	}
	// La pagina nuova dopo il salvataggio ha un'altra chiave e nessuna cattura.
	nuova := p.b.vai("GET", "/nuova", nil, nil)
	if chiaveDi(t, nuova.corpo) == chiave {
		t.Error("la pagina nuova ha la chiave di prima")
	}
}

// inventario: RIP-H-48
// Busta scritta a mano (nessuna estrazione) e raccolta spenta (com'è all'inizio): nessuna riga, lotto intero. Con la
// raccolta spenta anche il salvataggio consuma la cattura: riaccesa, la stessa pagina non scrive.
func TestRaccoltaAManoESpenta(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "500")
	pagina := p.b.vai("GET", "/nuova", nil, nil)
	if s := p.salvaPagina(t, pagina.corpo, "QR-MANO", func(v url.Values) {
		v.Set("repair[customer_name]", "Ugo Inventato")
		v.Set("repair[description]", "catenina da saldare")
	}); s.codice != 302 {
		t.Fatalf("a mano %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 0 || p.impostazione(t, "assist.training.remaining") != "500" {
		t.Errorf("a mano: righe %d", n)
	}

	p2 := nuovoDettato(t, true)
	r := p2.estraiPagina(t, dettatoRaccolta, "")
	chiave := chiaveDi(t, r.corpo)
	if s := p2.salvaPagina(t, r.corpo, "QR-SPENTA", nil); s.codice != 302 {
		t.Fatalf("spenta %d", s.codice)
	}
	if n := len(p2.righeRaccolta(t)); n != 0 || p2.impostazione(t, "assist.training.collect") != "(assente)" {
		t.Errorf("spenta: righe %d", n)
	}
	p2.accendiRaccolta(t, "500")
	if s := p2.salvaPagina(t, r.corpo, "QR-SPENTA2", func(v url.Values) { v.Set("intake_key", chiave) }); s.codice != 302 {
		t.Fatalf("riaccesa %d", s.codice)
	}
	if n := len(p2.righeRaccolta(t)); n != 0 {
		t.Errorf("riaccesa: righe %d", n)
	}
}

// inventario: RIP-H-48
// Una nuova estrazione della stessa pagina sostituisce la cattura; un'estrazione rifiutata la lascia com'era (come lo
// standalone, che cambia assist_capture solo a una riuscita); un salvataggio rifiutato non la consuma.
func TestRaccoltaNuovaEstrazioneSostituisce(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "500")
	r := p.estraiPagina(t, "Prima versione inventata del dettato, anello", "")
	chiave := chiaveDi(t, r.corpo)
	p.m.Programma(modellofinto.Buona(`{"n": "ofelia inventata", "d": "anello da stringere di una misura", "p": "40"}`))
	r = p.estraiPagina(t, dettatoRaccolta, chiave)
	if chiaveDi(t, r.corpo) != chiave {
		t.Fatal("la chiave del modulo è cambiata")
	}
	// Un rifiuto (testo corto) non tocca la cattura.
	if rif := p.estraiPagina(t, "corto", chiave); !strings.Contains(rif.corpo, "Testo troppo corto") {
		t.Fatal("rifiuto atteso")
	}
	// Un salvataggio rifiutato (lavorazione vuota) non la consuma.
	if s := p.salvaPagina(t, r.corpo, "QR-SOST", func(v url.Values) { v.Set("repair[description]", " ") }); s.codice != 200 {
		t.Fatalf("salvataggio rifiutato %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 0 || p.impostazione(t, "assist.training.remaining") != "500" {
		t.Errorf("dopo il rifiuto: righe %d", n)
	}
	if s := p.salvaPagina(t, r.corpo, "QR-SOST", nil); s.codice != 302 {
		t.Fatalf("salvataggio %d", s.codice)
	}
	rr := p.righeRaccolta(t)
	if len(rr) != 1 || rr[0].testo != dettatoRaccolta || !strings.Contains(rr[0].proposta, `"descrizione":"anello da stringere di una misura"`) {
		t.Errorf("righe %+v", rr)
	}
}

// inventario: RIP-H-48
// La cattura è della pagina che l'ha chiesta: un'altra sessione (anche dello stesso titolare) o un'altra chiave non la
// usano, e un client non può mandare testo o proposta suoi (non ci sono campi per farlo). Dopo durataCattura non vale più.
func TestRaccoltaCatturaDellaPagina(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "500")
	r := p.estraiPagina(t, dettatoRaccolta, "")
	chiave := chiaveDi(t, r.corpo)
	// Un'altra sessione con la stessa chiave.
	mio := p.b
	p.b = titolareDentro(t, p.h)
	_ = p.b.vai("GET", "/nuova", nil, nil)
	if s := p.salvaPagina(t, r.corpo, "QR-ALTRA", func(v url.Values) {
		v.Set("intake_key", chiave)
		v.Set("dettato", "testo inventato dal client")
		v.Set("assist[text]", "testo inventato dal client")
	}); s.codice != 302 {
		t.Fatalf("altra sessione %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 0 {
		t.Errorf("altra sessione: righe %d", n)
	}
	// La stessa sessione con un'altra chiave.
	p.b = mio
	if s := p.salvaPagina(t, r.corpo, "QR-ALTRACHIAVE", func(v url.Values) { v.Set("intake_key", casuale()) }); s.codice != 302 {
		t.Fatalf("altra chiave %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 0 {
		t.Errorf("altra chiave: righe %d", n)
	}
	// Scaduta.
	p.o.Sposta(durataCattura)
	if s := p.salvaPagina(t, r.corpo, "QR-SCADUTA", nil); s.codice != 302 {
		t.Fatalf("scaduta %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 0 {
		t.Errorf("scaduta: righe %d", n)
	}
	// Appena prima della scadenza vale.
	r = p.estraiPagina(t, dettatoRaccolta, "")
	p.o.Sposta(durataCattura - 1)
	if s := p.salvaPagina(t, r.corpo, "QR-INTEMPO", nil); s.codice != 302 {
		t.Fatalf("in tempo %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 1 {
		t.Errorf("in tempo: righe %d", n)
	}
}

// inventario: RIP-H-48
// L'ultima unità del lotto dalla pagina: la riga, la raccolta spenta, la riga d'audit «system»; la busta dopo, estratta,
// non scrive.
func TestRaccoltaUltimaUnitaDallaPagina(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "1")
	r := p.estraiPagina(t, dettatoRaccolta, "")
	if s := p.salvaPagina(t, r.corpo, "QR-ULTIMA", nil); s.codice != 302 {
		t.Fatalf("salvataggio %d", s.codice)
	}
	if p.impostazione(t, "assist.training.collect") != "0" || p.impostazione(t, "assist.training.remaining") != "0" || len(p.righeRaccolta(t)) != 1 {
		t.Error("ultima unità")
	}
	if n := contaRighe(t, p.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.training_collect' AND attore = 'system' AND dati = '{"on":false,"remaining":0}'`); n != 1 {
		t.Errorf("audit system %d", n)
	}
	r = p.estraiPagina(t, dettatoRaccolta, "")
	if s := p.salvaPagina(t, r.corpo, "QR-DOPO", nil); s.codice != 302 {
		t.Fatalf("dopo %d", s.codice)
	}
	if n := len(p.righeRaccolta(t)); n != 1 {
		t.Errorf("righe %d", n)
	}
}

// inventario: RIP-H-48
// Un errore nel salvare la riga non blocca la busta: la busta si salva e si va all'esito come sempre; nel registro solo una
// frase fissa, mai il dettato, il nome o il modello.
func TestRaccoltaErroreDallaPagina(t *testing.T) {
	p := nuovoDettato(t, true)
	p.accendiRaccolta(t, "500")
	eseguiSQL(t, p.d, `CREATE TRIGGER rifiuta_dettato BEFORE INSERT ON dettati_addestramento BEGIN SELECT RAISE(ABORT, 'no'); END`)
	p.m.Programma(modellofinto.Risposta{Contenuto: modellofinto.Testo(`{"n": "ofelia inventata", "d": "anello da stringere di una misura", "p": "40"}`),
		Modello: modellofinto.Testo("gestrip-inventato-v3")})
	r := p.estraiPagina(t, dettatoRaccolta, "")
	s := p.salvaPagina(t, r.corpo, "QR-ERRORE", nil)
	if s.codice != 302 || !strings.HasPrefix(s.location, "/?esito=") {
		t.Fatalf("salvataggio %d %q", s.codice, s.location)
	}
	if contaBuste(t, p.d) != 1 || len(p.righeRaccolta(t)) != 0 || p.impostazione(t, "assist.training.remaining") != "500" {
		t.Errorf("buste %d righe %d", contaBuste(t, p.d), len(p.righeRaccolta(t)))
	}
	registro := p.reg.String()
	if !strings.Contains(registro, "dettato: riga della raccolta non scritta") {
		t.Errorf("registro senza la frase: %q", registro)
	}
	for _, vietato := range []string{"Ofelia", "ofelia", "0113", "anello da stringere", "gestrip-inventato-v3"} {
		if strings.Contains(registro, vietato) {
			t.Errorf("%q nel registro", vietato)
		}
	}
	var audit string
	_ = p.d.Lettura.QueryRow(`SELECT group_concat(dati, ' ') FROM eventi_audit`).Scan(&audit)
	for _, vietato := range []string{"Ofelia", "0113", "anello", "gestrip-inventato-v3"} {
		if strings.Contains(audit, vietato) {
			t.Errorf("%q nell'audit", vietato)
		}
	}
}

// inventario: RIP-H-48
// Il deposito delle catture: prendi la toglie; rimetti la ridà solo se nel frattempo non ne è arrivata una nuova e se non è
// scaduta; sessioni e chiavi diverse non si vedono.
func TestDepositoCatture(t *testing.T) {
	o := clock.NuovoFinto(giornoDettato)
	c := nuovoDepositoCatture(o)
	a, b := buste.Dettato{Testo: "prima inventata"}, buste.Dettato{Testo: "seconda inventata"}
	c.metti(1, "n1", "k", a)
	if d, _ := c.prendi(2, "n1", "k"); d != nil {
		t.Error("altro utente")
	}
	if d, _ := c.prendi(1, "n2", "k"); d != nil {
		t.Error("altro accesso")
	}
	if d, _ := c.prendi(1, "n1", "k2"); d != nil {
		t.Error("altra chiave")
	}
	d, scade := c.prendi(1, "n1", "k")
	if d == nil || d.Testo != a.Testo || !scade.Equal(giornoDettato.Add(durataCattura)) {
		t.Fatalf("presa %v %v", d, scade)
	}
	if d2, _ := c.prendi(1, "n1", "k"); d2 != nil {
		t.Error("presa due volte")
	}
	// Rimessa: torna con la sua scadenza.
	c.rimetti(1, "n1", "k", d, scade)
	o.Sposta(time.Hour)
	if d2, s2 := c.prendi(1, "n1", "k"); d2 == nil || d2.Testo != a.Testo || !s2.Equal(scade) {
		t.Fatalf("rimessa %v %v", d2, s2)
	}
	// Una nuova estrazione arrivata nel frattempo vince.
	c.metti(1, "n1", "k", b)
	c.rimetti(1, "n1", "k", d, scade)
	if d2, _ := c.prendi(1, "n1", "k"); d2 == nil || d2.Testo != b.Testo {
		t.Errorf("dopo la nuova: %v", d2)
	}
	// Scaduta nel frattempo: non si rimette. Niente da rimettere: niente.
	o.Sposta(time.Hour)
	c.rimetti(1, "n1", "k", d, scade)
	c.rimetti(1, "n1", "k3", nil, scade.Add(time.Hour))
	if d2, _ := c.prendi(1, "n1", "k"); d2 != nil {
		t.Error("rimessa scaduta")
	}
	if d2, _ := c.prendi(1, "n1", "k3"); d2 != nil {
		t.Error("rimessa vuota")
	}
	// Le scadute si tolgono dalla memoria a ogni uso.
	c.metti(1, "n1", "vecchia", a)
	o.Sposta(durataCattura)
	c.metti(1, "n1", "nuova", b)
	if len(c.voci) != 1 {
		t.Errorf("voci in memoria %d", len(c.voci))
	}
}

// inventario: RIP-H-48
// Il tetto del deposito delle catture: una sessione ne tiene al più maxCattureSessione (oltre, si toglie la sua più vecchia,
// non quelle degli altri); a deposito pieno, anche dopo aver tolto le scadute, una cattura nuova non si tiene, mentre
// quella di una pagina che ne ha già una la sostituisce.
func TestDepositoCattureTetto(t *testing.T) {
	o := clock.NuovoFinto(giornoDettato)
	c := nuovoDepositoCatture(o)
	c.metti(9, "altro", "k-altro", buste.Dettato{Testo: "altro"})
	c.metti(1, "n2", "k-n2", buste.Dettato{Testo: "stesso utente, altro accesso"}) // la più vecchia di tutte
	for i := range maxCattureSessione + 1 {
		o.Sposta(time.Second)
		c.metti(1, "n1", strconv.Itoa(i), buste.Dettato{Testo: strconv.Itoa(i)})
	}
	if d, _ := c.prendi(1, "n1", "0"); d != nil {
		t.Error("la più vecchia della sessione è rimasta")
	}
	if d, _ := c.prendi(1, "n1", strconv.Itoa(maxCattureSessione)); d == nil {
		t.Error("la più nuova manca")
	}
	if d, _ := c.prendi(1, "n1", "1"); d == nil {
		t.Error("la seconda più vecchia manca")
	}
	if d, _ := c.prendi(9, "altro", "k-altro"); d == nil {
		t.Error("tolta la cattura di un'altra sessione")
	}
	if d, _ := c.prendi(1, "n2", "k-n2"); d == nil {
		t.Error("tolta la cattura di un altro accesso dello stesso utente")
	}
	// Deposito pieno di sessioni diverse.
	c = nuovoDepositoCatture(o)
	for i := range maxCatture {
		c.metti(int64(i+1), "n", "k", buste.Dettato{Testo: "x"})
	}
	c.metti(int64(maxCatture+1), "n", "k", buste.Dettato{Testo: "nuova"})
	if d, _ := c.prendi(int64(maxCatture+1), "n", "k"); d != nil || len(c.voci) != maxCatture {
		t.Errorf("deposito pieno: tenuta %v, voci %d", d, len(c.voci))
	}
	c.metti(1, "n", "k", buste.Dettato{Testo: "sostituita"})
	if d, _ := c.prendi(1, "n", "k"); d == nil || d.Testo != "sostituita" {
		t.Errorf("sostituzione a deposito pieno: %v", d)
	}
	// Una rimessa a deposito pieno (il posto liberato l'ha preso un altro) non si tiene. Dopo la scadenza c'è di nuovo posto.
	c.metti(int64(maxCatture+2), "n", "k", buste.Dettato{Testo: "x"})
	c.rimetti(1, "n", "k", &buste.Dettato{Testo: "rimessa"}, o.Adesso().Add(time.Hour))
	if d, _ := c.prendi(1, "n", "k"); d != nil || len(c.voci) != maxCatture {
		t.Errorf("rimessa a deposito pieno: %v, voci %d", d, len(c.voci))
	}
	o.Sposta(durataCattura)
	c.metti(int64(maxCatture+1), "n", "k", buste.Dettato{Testo: "dopo"})
	if d, _ := c.prendi(int64(maxCatture+1), "n", "k"); d == nil {
		t.Error("dopo la scadenza nessun posto")
	}
}

// busteCheScoppiano vanno in panic al primo Crea con una cattura.
type busteCheScoppiano struct {
	Buste
	scoppia bool
}

func (b *busteCheScoppiano) Crea(ctx context.Context, a buste.Accettazione, c audit.Contesto) (buste.Creata, error) {
	if b.scoppia && a.Dettato != nil {
		b.scoppia = false
		panic("guasto provocato")
	}
	return b.Buste.Crea(ctx, a, c)
}

// inventario: RIP-H-48
// Un panic durante il salvataggio non perde la cattura: la busta rimandata dopo scrive la riga.
func TestRaccoltaPanicRimetteLaCattura(t *testing.T) {
	var sc *busteCheScoppiano
	p := nuovoDettatoCon(t, true, func(b Buste) Buste { sc = &busteCheScoppiano{Buste: b, scoppia: true}; return sc })
	p.accendiRaccolta(t, "500")
	r := p.estraiPagina(t, dettatoRaccolta, "")
	// Il server chiude la connessione dopo il panic: la richiesta si manda a mano.
	req, _ := http.NewRequest("POST", p.b.srv.URL+"/nuova", strings.NewReader(p.moduloPagina(r.corpo, "QR-PANIC", nil).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if risp, err := p.b.c.Do(req); err == nil {
		_ = risp.Body.Close()
		t.Fatalf("nessun panic: %d", risp.StatusCode)
	}
	if sc.scoppia {
		t.Fatal("Crea non è stata chiamata")
	}
	if contaBuste(t, p.d) != 0 {
		t.Fatal("busta salvata nel panic")
	}
	if s := p.salvaPagina(t, r.corpo, "QR-PANIC", nil); s.codice != 302 {
		t.Fatalf("dopo il panic %d", s.codice)
	}
	if rr := p.righeRaccolta(t); len(rr) != 1 || rr[0].testo != dettatoRaccolta {
		t.Errorf("righe %+v", rr)
	}
}
