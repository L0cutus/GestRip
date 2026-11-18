package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Solleciti artigiano» (slice 096, JOB-09), come «the artisan chase job» di jobs_test.exs dello
// standalone (go-baseline): database vero, consegne vere (ApriConsegna, AggiungiAllaConsegna, ConfermaConsegna,
// RientraDallArtigiano), coda vera con i provider veri (non vanno in rete), orologio finto. Nomi, numeri e
// indirizzi inventati.

type provaSolleciti struct {
	d    *db.DB
	o    *clock.Finto
	b    *buste.Buste
	coda *uscita.Coda
	l    *LavoroSolleciti
	p    *Pianificatore
	art  map[string]int64
	qr   map[int64]string
}

// nuovaProvaSolleciti è il pianificatore con il solo lavoro dei solleciti, sulla coda di serve. posta, se c'è,
// sta fra il lavoro e la coda (per i rifiuti). Telegram è acceso.
func nuovaProvaSolleciti(t *testing.T, posta func(*uscita.Coda) Accodatore) *provaSolleciti {
	t.Helper()
	d, o := apriDB(t)
	accendiTelegram(t, d)
	coda := canaliVeri(d, o)
	var acc Accodatore = coda
	if posta != nil {
		acc = posta(coda)
	}
	l := NuoviSolleciti(d, acc)
	return &provaSolleciti{d: d, o: o, b: buste.Nuove(d, o), coda: coda, l: l,
		p: Nuovo(d, o, coda, map[string]Lavoro{Solleciti: l}), art: map[string]int64{}, qr: map[int64]string{}}
}

// artigiano è l'id dell'artigiano nome, creato la prima volta.
func (ps *provaSolleciti) artigiano(t *testing.T, nome string) int64 {
	t.Helper()
	if id, ok := ps.art[nome]; ok {
		return id
	}
	a, err := ps.b.CreaArtigiano(context.Background(), buste.DatiArtigiano{Nome: &nome}, titolare)
	if err != nil {
		t.Fatalf("artigiano %s: %v", nome, err)
	}
	ps.art[nome] = a.ID
	return a.ID
}

// busta crea la busta numero con il codice qr (un'etichetta RIP-AAAA-NNNN o un codice qualunque).
func (ps *provaSolleciti) busta(t *testing.T, numero, qr string) int64 {
	t.Helper()
	a := buste.Accettazione{NomeCliente: "Cliente " + numero, TelefonoCliente: "333 000 0002", Numero: numero,
		DataAccettazione: "2026-09-01", Descrizione: "Collana da saldare", CodiceQR: qr}
	a.GemelleConfermate = buste.EtichettaScritta(qr, buste.AnnoDiAccettazione(a.DataAccettazione, ps.o.Adesso()))
	c, err := ps.b.Crea(context.Background(), a, titolare)
	if err != nil {
		t.Fatalf("busta %s: %v", numero, err)
	}
	ps.qr[c.ID] = qr
	return c.ID
}

// manda porta la busta dall'artigiano con una consegna vera confermata all'istante quando (l'orologio torna dov'era).
func (ps *provaSolleciti) manda(t *testing.T, artigiano string, busta int64, quando time.Time) {
	t.Helper()
	ctx := context.Background()
	prima := ps.o.Adesso()
	ps.o.Imposta(quando)
	defer ps.o.Imposta(prima)
	c, err := ps.b.ApriConsegna(ctx, ps.artigiano(t, artigiano))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ps.b.AggiungiAllaConsegna(ctx, c, ps.qr[busta], buste.LetturaScansione); err != nil {
		t.Fatalf("aggiunta della busta %d: %v", busta, err)
	}
	if n, err := ps.b.ConfermaConsegna(ctx, c, titolare); err != nil || n != 1 {
		t.Fatalf("conferma: %d %v", n, err)
	}
}

// fuori crea la busta e la manda dall'artigiano da giorni giorni (dall'istante dell'orologio).
func (ps *provaSolleciti) fuori(t *testing.T, artigiano, numero, qr string, giorni int) int64 {
	t.Helper()
	id := ps.busta(t, numero, qr)
	ps.manda(t, artigiano, id, ps.o.Adesso().Add(-time.Duration(giorni)*24*time.Hour))
	return id
}

func (ps *provaSolleciti) esegui(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := ps.d.Scrittura.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// timbro è sollecito_avvisato_il della busta ("" se NULL).
func (ps *provaSolleciti) timbro(t *testing.T, id int64) string {
	t.Helper()
	var v *string
	if err := ps.d.Lettura.QueryRow(`SELECT sollecito_avvisato_il FROM buste WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (ps *provaSolleciti) messaggi(t *testing.T) []rigaCoda {
	t.Helper()
	return (&provaRitardi{d: ps.d}).messaggi(t)
}

// soloTelegram sono i destinatari del pianificatore con il solo Telegram acceso.
var soloTelegram = []uscita.Destinazione{{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta}}

// esegui chiama il lavoro all'istante dell'orologio con il solo Telegram (senza pianificatore).
func (ps *provaSolleciti) adesso(t *testing.T) Risultato {
	t.Helper()
	r, err := ps.l.Esegui(context.Background(), Config{}, ps.o.Adesso(), soloTelegram, titolare)
	if err != nil {
		t.Fatalf("esecuzione: %v", err)
	}
	return r
}

func (ps *provaSolleciti) aMano(t *testing.T) Esecuzione {
	t.Helper()
	e, err := ps.p.EseguiAMano(context.Background(), Solleciti, titolare)
	if err != nil {
		t.Fatalf("a mano: %v", err)
	}
	return e
}

func (ps *provaSolleciti) giro(t *testing.T) Esecuzione {
	t.Helper()
	es, err := ps.p.Giro(context.Background())
	if err != nil || len(es) != 1 {
		t.Fatalf("giro: %v %+v", err, es)
	}
	return es[0]
}

// pagina è quante buste conta la pagina dei solleciti (083) adesso, con la soglia dell'impostazione.
func (ps *provaSolleciti) pagina(t *testing.T) int {
	t.Helper()
	s, err := ps.b.SollecitiPendenti(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	return s.Totale
}

// inventario: JOB-09
// Una busta dall'artigiano da 20 giorni (soglia predefinita 14) si nomina in un riepilogo solo al negozio, con il
// testo dello standalone; la busta fuori da 3 giorni no. Il timbro è l'istante dell'esecuzione, il messaggio non ha
// busta, l'esecuzione è «ok» con un avviso e un messaggio, firmata scheduler. Il giro dopo (stesso giorno, a mano) e
// il giorno dopo non nominano di nuovo la busta: «Nessuna busta oltre soglia.», nessun messaggio.
func TestSollecitiBustaOltreSoglia(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	tardi := ps.fuori(t, "Bulino", "20/26", "QR-20", 20)
	fresca := ps.fuori(t, "Bulino", "21/26", "QR-21", 3)
	e := ps.giro(t)
	if e.Lavoro != Solleciti || e.Avvio != AvvioAutomatico || e.Esito != EsitoOK || e.Avvisi != 1 || e.Messaggi != 1 ||
		e.Dettaglio != "1 busta oltre soglia." {
		t.Errorf("esecuzione %+v", e)
	}
	m := ps.messaggi(t)
	if len(m) != 1 || m[0].busta.Valid || m[0].canale != uscita.CanaleTelegram || m[0].parte != "shop" || m[0].indirizzo != chatFinta ||
		m[0].oggetto != "Solleciti artigiano" ||
		m[0].testo != "🧰 Solleciti artigiano\n1 busta fuori bottega da oltre 14 giorni.\nBulino: 20/26 (20gg)" {
		t.Errorf("messaggi %+v", m)
	}
	if ps.timbro(t, tardi) != ps.o.Adesso().UTC().Format(db.FormatoIstante) || ps.timbro(t, fresca) != "" {
		t.Errorf("timbri %q %q", ps.timbro(t, tardi), ps.timbro(t, fresca))
	}
	if n := conta(t, ps.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued' AND attore = ?`, AttoreScheduler); n != 1 {
		t.Errorf("outgoing.queued firmati scheduler: %d", n)
	}
	ps.o.Sposta(time.Minute)
	if e := ps.aMano(t); e.Esito != EsitoOK || e.Avvisi != 0 || e.Messaggi != 0 || e.Dettaglio != "Nessuna busta oltre soglia." {
		t.Errorf("seconda %+v", e)
	}
	ps.o.Sposta(24 * time.Hour)
	if e := ps.giro(t); e.Avvisi != 0 || e.Dettaglio != "Nessuna busta oltre soglia." {
		t.Errorf("giorno dopo %+v", e)
	}
	if len(ps.messaggi(t)) != 1 {
		t.Errorf("messaggi dopo: %d", len(ps.messaggi(t)))
	}
}

// inventario: JOB-09
// Un solo riepilogo per tutti gli artigiani: per artigiano (per nome) una riga, le buste dalla più vecchia uscita,
// i giorni fuori interi; il numero è quello dei testi (D-64/D-82: «0020/26» con l'etichetta, «21/26» senza). Con
// email e Telegram accesi lo stesso riepilogo su ogni canale, email prima: due messaggi, avvisi = buste nominate.
func TestSollecitiUnRiepilogoPerCanale(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	accendiEmail(t, ps.d)
	ps.fuori(t, "Incastonatore", "24/26", "QR-24", 16)
	ps.fuori(t, "Bulino", "21/26", "QR-21", 15)
	ps.fuori(t, "Bulino", "20/26", "RIP-2026-0020", 20)
	e := ps.giro(t)
	if e.Esito != EsitoOK || e.Avvisi != 3 || e.Messaggi != 2 || e.Dettaglio != "3 buste oltre soglia." {
		t.Errorf("esecuzione %+v", e)
	}
	atteso := "🧰 Solleciti artigiano\n3 buste fuori bottega da oltre 14 giorni.\nBulino: 0020/26 (20gg), 21/26 (15gg)\nIncastonatore: 24/26 (16gg)"
	m := ps.messaggi(t)
	if len(m) != 2 || m[0].canale != uscita.CanaleEmail || m[0].indirizzo != mittenteFinto || m[1].canale != uscita.CanaleTelegram ||
		m[0].testo != atteso || m[1].testo != atteso || m[0].oggetto != "Solleciti artigiano" || m[1].oggetto != m[0].oggetto {
		t.Errorf("messaggi %+v", m)
	}
}

// inventario: JOB-09
// La soglia è quella della pagina dei solleciti (083, solleciti_artigiano_giorni, letta come threshold_days/1):
// con 14 una busta fuori da 10 giorni non c'è; con «7» sì e il testo dice 7; con un testo che non è un numero 14;
// con «0» un giorno. Il limite è compreso: fuori da esattamente 7·86 400 s è oltre soglia, un secondo prima no.
func TestSollecitiSoglia(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	dieci := ps.fuori(t, "Bulino", "25/26", "QR-25", 10)
	if r := ps.adesso(t); r.Avvisi != 0 || ps.pagina(t) != 0 {
		t.Errorf("con 14: %+v", r)
	}
	imposta(t, ps.d, buste.ChiaveSollecitiGiorni, "abc")
	if r := ps.adesso(t); r.Avvisi != 0 {
		t.Errorf("con abc: %+v", r)
	}
	imposta(t, ps.d, buste.ChiaveSollecitiGiorni, " 7 ")
	r := ps.adesso(t)
	if r.Avvisi != 1 || ps.timbro(t, dieci) == "" {
		t.Errorf("con 7: %+v", r)
	}
	if m := ps.messaggi(t); len(m) != 1 || !strings.Contains(m[0].testo, "\n1 busta fuori bottega da oltre 7 giorni.\n") {
		t.Errorf("messaggi %+v", m)
	}

	ps2 := nuovaProvaSolleciti(t, nil)
	imposta(t, ps2.d, buste.ChiaveSollecitiGiorni, "7")
	limite := ps2.busta(t, "26/26", "QR-26")
	ps2.manda(t, "Bulino", limite, ps2.o.Adesso().Add(-7*24*time.Hour+time.Second))
	if r := ps2.adesso(t); r.Avvisi != 0 {
		t.Errorf("un secondo prima: %+v", r)
	}
	ps2.o.Sposta(time.Second)
	if r := ps2.adesso(t); r.Avvisi != 1 || r.Dettaglio != "1 busta oltre soglia." {
		t.Errorf("al limite: %+v", r)
	}
	ps3 := nuovaProvaSolleciti(t, nil)
	imposta(t, ps3.d, buste.ChiaveSollecitiGiorni, "0")
	ps3.fuori(t, "Bulino", "27/26", "QR-27", 1)
	if r := ps3.adesso(t); r.Avvisi != 1 || !strings.Contains(ps3.messaggi(t)[0].testo, "da oltre 1 giorni.") {
		t.Errorf("con 0: %+v", r)
	}
	ps4 := nuovaProvaSolleciti(t, nil)
	imposta(t, ps4.d, buste.ChiaveSollecitiGiorni, "99999999999999999999")
	ps4.fuori(t, "Bulino", "28/26", "QR-28", 4000)
	if r := ps4.adesso(t); r.Avvisi != 0 || r.Dettaglio != "Nessuna busta oltre soglia." {
		t.Errorf("soglia enorme: %+v", r)
	}
}

// inventario: JOB-09
// Il timbro vale per un giro dall'artigiano: la busta nominata e ancora fuori non si nomina più (nemmeno dopo 30
// giorni); rientrata non è più fuori; mandata di nuovo il giorno dopo, il timbro del giro vecchio non conta e dopo
// 20 giorni del giro nuovo si nomina di nuovo. Una nuova consegna che non ha ancora passato la soglia non si nomina.
func TestSollecitiTimbroPerGiro(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	id := ps.fuori(t, "Bulino", "22/26", "QR-22", 20)
	if r := ps.adesso(t); r.Avvisi != 1 {
		t.Fatalf("primo giro %+v", r)
	}
	primo := ps.timbro(t, id)
	ps.o.Sposta(30 * 24 * time.Hour)
	if r := ps.adesso(t); r.Avvisi != 0 || ps.pagina(t) != 1 {
		t.Errorf("ancora fuori, stesso giro: %+v (pagina %d)", r, ps.pagina(t))
	}
	if _, err := ps.b.RientraDallArtigiano(context.Background(), id, "", "", titolare); err != nil {
		t.Fatal(err)
	}
	if r := ps.adesso(t); r.Avvisi != 0 {
		t.Errorf("rientrata: %+v", r)
	}
	ps.o.Sposta(24 * time.Hour)
	ps.manda(t, "Incastonatore", id, ps.o.Adesso())
	ps.o.Sposta(10 * 24 * time.Hour)
	if r := ps.adesso(t); r.Avvisi != 0 || ps.timbro(t, id) != primo {
		t.Errorf("giro nuovo sotto soglia: %+v", r)
	}
	ps.o.Sposta(10 * 24 * time.Hour)
	if r := ps.adesso(t); r.Avvisi != 1 || ps.timbro(t, id) == primo {
		t.Errorf("giro nuovo oltre soglia: %+v", r)
	}
	m := ps.messaggi(t)
	if len(m) != 2 || !strings.HasSuffix(m[1].testo, "\nIncastonatore: 22/26 (20gg)") {
		t.Errorf("messaggi %+v", m)
	}
	ps.o.Sposta(24 * time.Hour)
	if r := ps.adesso(t); r.Avvisi != 0 {
		t.Errorf("giro nuovo, di nuovo: %+v", r)
	}
	// Un timbro uguale all'uscita (stesso istante) è di questo giro: non si nomina.
	ps2 := nuovaProvaSolleciti(t, nil)
	stesso := ps2.fuori(t, "Bulino", "23/26", "QR-23", 20)
	ps2.esegui(t, `UPDATE buste SET sollecito_avvisato_il = consegnata_artigiano_il WHERE id = ?`, stesso)
	if r := ps2.adesso(t); r.Avvisi != 0 {
		t.Errorf("timbro all'istante dell'uscita: %+v", r)
	}
	ps2.esegui(t, `UPDATE buste SET sollecito_avvisato_il = '2026-01-01T00:00:00.000000Z' WHERE id = ?`, stesso)
	if r := ps2.adesso(t); r.Avvisi != 1 {
		t.Errorf("timbro di prima dell'uscita: %+v", r)
	}
}

// inventario: JOB-09
// Le buste della pagina e solo quelle: senza consegna (consegna_artigiano_id NULL con l'uscita) no, rientrate no,
// nel cestino sì (deciso da Stefano il 15/11/2026 per la 083: come lo standalone); il gruppo segue l'artigiano
// della consegna, non quello della busta.
func TestSollecitiLeBusteDellaPagina(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	cestino := ps.fuori(t, "Bulino", "30/26", "QR-30", 20)
	ps.esegui(t, `UPDATE buste SET cancellata_il = '2026-11-10T08:00:00.000000Z' WHERE id = ?`, cestino)
	senza := ps.fuori(t, "Bulino", "31/26", "QR-31", 20)
	ps.esegui(t, `UPDATE buste SET consegna_artigiano_id = NULL WHERE id = ?`, senza)
	altro := ps.fuori(t, "Bulino", "32/26", "QR-32", 18)
	ps.esegui(t, `UPDATE buste SET artigiano_id = ? WHERE id = ?`, ps.artigiano(t, "Zecca"), altro)
	if p := ps.pagina(t); p != 2 {
		t.Fatalf("pagina %d", p)
	}
	r := ps.adesso(t)
	if r.Avvisi != 2 || ps.timbro(t, senza) != "" || ps.timbro(t, cestino) == "" {
		t.Errorf("%+v", r)
	}
	if m := ps.messaggi(t); len(m) != 1 || !strings.HasSuffix(m[0].testo, "\nBulino: 30/26 (20gg), 32/26 (18gg)") {
		t.Errorf("messaggi %+v", m)
	}
}

// inventario: JOB-09
// Il lavoro non registra un sollecito vero: nessuna riga solleciti_artigiani, nessuna repair.artisan_reminded, e la
// pagina dei solleciti continua a dire «mai sollecitata» e a contare la busta.
func TestSollecitiNessunSollecitoVero(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	ps.fuori(t, "Bulino", "20/26", "QR-20", 20)
	if r := ps.adesso(t); r.Avvisi != 1 {
		t.Fatalf("%+v", r)
	}
	if n := conta(t, ps.d, `SELECT count(*) FROM solleciti_artigiani`); n != 0 {
		t.Errorf("solleciti %d", n)
	}
	if n := conta(t, ps.d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.artisan_reminded'`); n != 0 {
		t.Errorf("audit %d", n)
	}
	s, err := ps.b.SollecitiPendenti(context.Background(), "")
	if err != nil || s.Totale != 1 || s.Gruppi[0].UltimoSollecito != nil || s.Gruppi[0].Buste[0].UltimoSollecito != nil {
		t.Errorf("pagina %+v %v", s, err)
	}
}

// inventario: JOB-09
// Tutto o niente: se la coda rifiuta il riepilogo su un canale, si annullano i timbri di tutte le buste e anche il
// riepilogo già accodato sull'altro canale; «Fallito» con il motivo dello standalone e zero conteggi. Nel registro il
// motivo, mai indirizzi, artigiani o numeri. Il giro dopo, con la coda a posto, nomina tutte le buste.
func TestSollecitiTuttoONiente(t *testing.T) {
	for _, c := range []struct {
		err    error
		motivo string
	}{
		{uscita.ErrMessaggioNonValido, "La coda ha rifiutato il riepilogo dei solleciti."},
		{uscita.ErrSoloAMano, "La coda ha rifiutato il riepilogo dei solleciti."},
		{fmt.Errorf("avvolto: %w", uscita.ErrNonConfigurato), "Il canale telegram non è più configurato."},
	} {
		rifiuta := &postaCheRifiuta{numero: "*", canale: uscita.CanaleTelegram, err: c.err}
		ps := nuovaProvaSolleciti(t, func(q *uscita.Coda) Accodatore { rifiuta.Coda = q; return rifiuta })
		accendiEmail(t, ps.d)
		var registro bytes.Buffer
		ps.l.ConRegistro(slog.New(slog.NewTextHandler(&registro, nil)))
		uno := ps.fuori(t, "Bulino", "26/26", "QR-26", 20)
		due := ps.fuori(t, "Incastonatore", "27/26", "QR-27", 18)
		e := ps.giro(t)
		if e.Esito != EsitoFallito || e.Avvisi != 0 || e.Messaggi != 0 || e.Dettaglio != c.motivo {
			t.Errorf("%v: esecuzione %+v", c.err, e)
		}
		if len(ps.messaggi(t)) != 0 || ps.timbro(t, uno) != "" || ps.timbro(t, due) != "" {
			t.Errorf("%v: scritto qualcosa", c.err)
		}
		if r := registro.String(); !strings.Contains(r, "solleciti: riepilogo non accodato") || !strings.Contains(r, c.motivo) ||
			strings.Contains(r, chatFinta) || strings.Contains(r, mittenteFinto) || strings.Contains(r, "Bulino") || strings.Contains(r, "26/26") {
			t.Errorf("%v: registro %q", c.err, r)
		}
		rifiuta.numero = "nessuna"
		ps.o.Sposta(time.Minute)
		if e := ps.giro(t); e.Esito != EsitoOK || e.Avvisi != 2 || e.Messaggi != 2 {
			t.Errorf("%v: giro dopo %+v", c.err, e)
		}
	}
}

// inventario: JOB-09
// Un guasto che non è un rifiuto della coda (il database) ferma il lavoro: «Il job si è interrotto», e la
// transazione unica annulla tutto, timbri e messaggi (nello standalone l'eccezione esce da Repo.transaction).
func TestSollecitiGuasto(t *testing.T) {
	rifiuta := &postaCheRifiuta{numero: "*", canale: uscita.CanaleTelegram, err: errors.New("disco pieno: indirizzo segreto@esempio.it")}
	ps := nuovaProvaSolleciti(t, func(q *uscita.Coda) Accodatore { rifiuta.Coda = q; return rifiuta })
	accendiEmail(t, ps.d)
	id := ps.fuori(t, "Bulino", "26/26", "QR-26", 20)
	if e := ps.aMano(t); e.Esito != EsitoFallito || e.Avvisi != 0 || e.Dettaglio != DettaglioInterrotto {
		t.Errorf("guasto %+v", e)
	}
	if len(ps.messaggi(t)) != 0 || ps.timbro(t, id) != "" {
		t.Error("scritto dopo il guasto")
	}
}

// inventario: JOB-09
// I giorni sono quelli della pagina (083): blocchi interi di 86 400 secondi dall'uscita, non giorni del calendario
// del negozio. Una busta uscita alle 23:30 del negozio, dopo la mezzanotte di 14 giorni dopo, è fuori da 13 giorni
// e 1 ora: non si nomina finché non passano 14·86 400 s. Col cambio d'ora (25/10/2026) il confine resta a 14·86 400
// s dall'uscita: un'ora prima, sull'orologio del muro, di quanto direbbe il calendario. In ogni istante lavoro e
// pagina contano le stesse buste.
func TestSollecitiGiorniEOrologio(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	notte := ps.busta(t, "40/26", "QR-40")
	uscita := locale(2026, 11, 1, 23, 30) // domenica 01/11, 23:30 in negozio (22:30 UTC)
	ps.manda(t, "Bulino", notte, uscita)
	ps.o.Imposta(locale(2026, 11, 16, 0, 30)) // il calendario dice 15 giorni, i secondi 14 giorni e 1 ora
	if r := ps.adesso(t); r.Avvisi != 1 || ps.pagina(t) != 1 {
		t.Errorf("dopo 14 giorni e un'ora: %+v", r)
	}
	ps2 := nuovaProvaSolleciti(t, nil)
	notte2 := ps2.busta(t, "41/26", "QR-41")
	ps2.manda(t, "Bulino", notte2, uscita)
	ps2.o.Imposta(locale(2026, 11, 15, 23, 29)) // calendario: 14 giorni dopo; secondi: un minuto meno di 14 giorni
	if r := ps2.adesso(t); r.Avvisi != 0 || ps2.pagina(t) != 0 {
		t.Errorf("un minuto prima: %+v", r)
	}
	ps2.o.Imposta(locale(2026, 11, 15, 23, 30))
	if r := ps2.adesso(t); r.Avvisi != 1 {
		t.Errorf("al minuto: %+v", r)
	}
	if m := ps2.messaggi(t); len(m) != 1 || !strings.HasSuffix(m[0].testo, "Bulino: 41/26 (14gg)") {
		t.Errorf("messaggi %+v", m)
	}

	// Cambio d'ora: uscita lunedì 12/10 alle 10:00 (CEST, 08:00 UTC); 14·86 400 s dopo è il 26/10 alle 09:00 (CET).
	ps3 := nuovaProvaSolleciti(t, nil)
	ottobre := ps3.busta(t, "42/26", "QR-42")
	ps3.manda(t, "Bulino", ottobre, locale(2026, 10, 12, 10, 0))
	ps3.o.Imposta(locale(2026, 10, 26, 8, 59))
	if r := ps3.adesso(t); r.Avvisi != 0 || ps3.pagina(t) != 0 {
		t.Errorf("08:59 del 26/10: %+v", r)
	}
	ps3.o.Imposta(locale(2026, 10, 26, 9, 0))
	if p := ps3.pagina(t); p != 1 {
		t.Errorf("pagina alle 09:00: %d", p)
	}
	if r := ps3.adesso(t); r.Avvisi != 1 || !strings.HasSuffix(ps3.messaggi(t)[0].testo, "(14gg)") {
		t.Errorf("09:00 del 26/10: %+v", r)
	}
}

// inventario: JOB-09
// Nulla da sollecitare: nessun messaggio e nessun timbro, «Nessuna busta oltre soglia.» con esito «ok» (come lo
// standalone), anche con buste fuori sotto soglia.
func TestSollecitiNullaDaSollecitare(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	e := ps.giro(t)
	if e.Esito != EsitoOK || e.Avvisi != 0 || e.Messaggi != 0 || e.Dettaglio != "Nessuna busta oltre soglia." {
		t.Errorf("vuoto %+v", e)
	}
	id := ps.fuori(t, "Bulino", "50/26", "QR-50", 13)
	if r := ps.adesso(t); r.Fallito || r.Avvisi != 0 || r.Messaggi != 0 || r.Dettaglio != "Nessuna busta oltre soglia." || ps.timbro(t, id) != "" {
		t.Errorf("sotto soglia %+v", r)
	}
	if len(ps.messaggi(t)) != 0 {
		t.Error("messaggi")
	}
}

// inventario: JOB-09
// Senza destinatari il lavoro non timbra niente e torna subito con ErrSenzaCanali: il pianificatore non lo chiama
// mai così (D-16), ma un timbro senza riepilogo sarebbe un avviso perso.
func TestSollecitiSenzaDestinatari(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	id := ps.fuori(t, "Bulino", "20/26", "QR-20", 20)
	if _, err := ps.l.Esegui(context.Background(), Config{}, ps.o.Adesso(), nil, titolare); !errors.Is(err, ErrSenzaCanali) {
		t.Errorf("errore %v", err)
	}
	if ps.timbro(t, id) != "" {
		t.Error("timbrata senza destinatari")
	}
}

// inventario: JOB-09
// Uno stop durante l'accodamento (anche con un errore che non è quello del contesto) o prima di cominciare: il
// lavoro torna con l'errore dello stop, niente riga, niente timbri né messaggi (tutto o niente); dopo il riavvio il
// giro dello stesso giorno è ancora dovuto e nomina le buste una volta.
func TestSollecitiStop(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	ps := nuovaProvaSolleciti(t, func(q *uscita.Coda) Accodatore { return postaCheFerma{q, ferma} })
	uno, due := ps.fuori(t, "Bulino", "20/26", "QR-20", 20), ps.fuori(t, "Bulino", "21/26", "QR-21", 16)
	es, err := ps.p.Giro(ctx)
	if !errors.Is(err, context.Canceled) || len(es) != 0 {
		t.Errorf("%v %+v", err, es)
	}
	if st, _ := ps.p.Storico(context.Background(), 10); len(st) != 0 {
		t.Errorf("righe %+v", st)
	}
	if ps.timbro(t, uno) != "" || ps.timbro(t, due) != "" || len(ps.messaggi(t)) != 0 {
		t.Error("scritto dopo lo stop")
	}
	l := NuoviSolleciti(ps.d, ps.coda)
	p := Nuovo(ps.d, ps.o, ps.coda, map[string]Lavoro{Solleciti: l})
	ps.o.Sposta(time.Minute)
	es, err = p.Giro(context.Background())
	if err != nil || len(es) != 1 || es[0].Avvisi != 2 || es[0].Messaggi != 1 || es[0].Esito != EsitoOK {
		t.Fatalf("dopo il riavvio: %v %+v", err, es)
	}
	ctx2, ferma2 := context.WithCancel(context.Background())
	ferma2()
	ps2 := nuovaProvaSolleciti(t, nil)
	id := ps2.fuori(t, "Bulino", "20/26", "QR-20", 20)
	if _, err := ps2.l.Esegui(ctx2, Config{}, ps2.o.Adesso(), soloTelegram, titolare); !errors.Is(err, context.Canceled) {
		t.Errorf("stop prima: %v", err)
	}
	if ps2.timbro(t, id) != "" {
		t.Error("timbrata dopo lo stop prima")
	}
}

// inventario: JOB-09
// Il recupero della D-17: il lunedì (chiuso) la busta passa la soglia ma il giro non fa niente; il martedì il lavoro
// la trova da solo e la nomina con i giorni veri.
func TestSollecitiRecuperoD17(t *testing.T) {
	ps := nuovaProvaSolleciti(t, nil)
	lunedi := time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC) // lunedì 16/11, 10:30 in negozio
	id := ps.busta(t, "60/26", "QR-60")
	ps.manda(t, "Bulino", id, lunedi.Add(-14*24*time.Hour)) // passa la soglia lunedì alle 10:30
	ps.o.Imposta(lunedi)
	if es, err := ps.p.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Fatalf("lunedì: %v %+v", err, es)
	}
	if ps.timbro(t, id) != "" || len(ps.messaggi(t)) != 0 {
		t.Fatal("lunedì ha scritto")
	}
	ps.o.Imposta(martedi)
	if e := ps.giro(t); e.Avvisi != 1 || e.Esito != EsitoOK {
		t.Errorf("martedì %+v", e)
	}
	if m := ps.messaggi(t); len(m) != 1 || !strings.HasSuffix(m[0].testo, "Bulino: 60/26 (14gg)") {
		t.Errorf("messaggi %+v", m)
	}
}
