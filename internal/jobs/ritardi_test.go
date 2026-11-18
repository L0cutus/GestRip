package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Buste in ritardo» (slice 095, JOB-07, JOB-08), come «the overdue bags job» di jobs_test.exs e
// overdue_reopen_test.exs dello standalone (go-baseline): database vero, coda vera con i provider veri (non vanno
// in rete: le righe restano in coda), orologio finto. Nomi, numeri e indirizzi inventati.

var titolare = audit.Contesto{Attore: "titolare@esempio.test", IP: "203.0.113.7"}

type provaRitardi struct {
	d    *db.DB
	o    *clock.Finto
	b    *buste.Buste
	coda *uscita.Coda
	l    *LavoroRitardi
	p    *Pianificatore
}

// nuovaProvaRitardi è il pianificatore con il solo lavoro dei ritardi, sulla coda di serve. posta, se c'è,
// sta fra il lavoro e la coda (per i rifiuti).
func nuovaProvaRitardi(t *testing.T, posta func(*uscita.Coda) Accodatore) *provaRitardi {
	t.Helper()
	d, o := apriDB(t)
	coda := canaliVeri(d, o)
	var acc Accodatore = coda
	if posta != nil {
		acc = posta(coda)
	}
	l := NuoviRitardi(d, acc)
	return &provaRitardi{d: d, o: o, b: buste.Nuove(d, o), coda: coda, l: l,
		p: Nuovo(d, o, coda, map[string]Lavoro{Ritardi: l})}
}

// oggi è la data del negozio adesso, spostata di n giorni, in aaaa-mm-gg.
func (pr *provaRitardi) giorno(n int) string {
	return clock.Locale(pr.o.Adesso()).AddDate(0, 0, n).Format(time.DateOnly)
}

// busta crea la busta numero con la data promessa ritardo giorni fa (negativo: nel futuro).
func (pr *provaRitardi) busta(t *testing.T, numero string, ritardo int) int64 {
	t.Helper()
	qr := "QR-" + strings.ReplaceAll(numero, "/", "-")
	a := buste.Accettazione{NomeCliente: "Cliente " + numero, TelefonoCliente: "333 000 0001", Numero: numero,
		DataAccettazione: "2026-09-01", DataPrevista: pr.giorno(-ritardo), Descrizione: "Anello da stringere", CodiceQR: qr}
	a.GemelleConfermate = buste.EtichettaScritta(qr, buste.AnnoDiAccettazione(a.DataAccettazione, pr.o.Adesso()))
	c, err := pr.b.Crea(context.Background(), a, titolare)
	if err != nil {
		t.Fatalf("busta %s: %v", numero, err)
	}
	return c.ID
}

func (pr *provaRitardi) esegui(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := pr.d.Scrittura.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// timbro è ritardo_avvisato_il della busta ("" se NULL).
func (pr *provaRitardi) timbro(t *testing.T, id int64) string {
	t.Helper()
	var v sql.NullString
	if err := pr.d.Lettura.QueryRow(`SELECT ritardo_avvisato_il FROM buste WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v.String
}

func (pr *provaRitardi) silenziato(t *testing.T) (string, bool) {
	t.Helper()
	var v string
	err := pr.d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, ChiaveStoricoSilenziato).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return v, true
}

// rigaCoda è una riga di messaggi_in_uscita.
type rigaCoda struct {
	busta                                    sql.NullInt64
	canale, parte, indirizzo, oggetto, testo string
}

func (pr *provaRitardi) messaggi(t *testing.T) []rigaCoda {
	t.Helper()
	rs, err := pr.d.Lettura.Query(`SELECT busta_id, canale, destinatario, indirizzo, coalesce(oggetto, ''), testo FROM messaggi_in_uscita ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []rigaCoda
	for rs.Next() {
		var r rigaCoda
		if err := rs.Scan(&r.busta, &r.canale, &r.parte, &r.indirizzo, &r.oggetto, &r.testo); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// aMano è un'esecuzione a mano dei ritardi, che deve lasciare la riga.
func (pr *provaRitardi) aMano(t *testing.T) Esecuzione {
	t.Helper()
	e, err := pr.p.EseguiAMano(context.Background(), Ritardi, titolare)
	if err != nil {
		t.Fatalf("a mano: %v", err)
	}
	return e
}

// giro è un giro del pianificatore che deve eseguire i ritardi.
func (pr *provaRitardi) giro(t *testing.T) Esecuzione {
	t.Helper()
	es, err := pr.p.Giro(context.Background())
	if err != nil || len(es) != 1 {
		t.Fatalf("giro: %v %+v", err, es)
	}
	return es[0]
}

func (pr *provaRitardi) adessoIstante() string { return pr.o.Adesso().UTC().Format(db.FormatoIstante) }

// inventario: JOB-07
// Una busta con la data promessa passata da un giorno si segnala una volta, al negozio, con il testo dello
// standalone senza formattazione; il timbro è l'istante dell'esecuzione e la busta non cambia altro. Una busta
// promessa per oggi o per domani non è in ritardo. La seconda esecuzione non trova niente.
func TestRitardiBustaAppenaInRitardo(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	late := pr.busta(t, "1/26", 1)
	oggi := pr.busta(t, "2/26", 0)
	domani := pr.busta(t, "3/26", -1)
	var aggiornata string
	if err := pr.d.Lettura.QueryRow(`SELECT aggiornato_il FROM buste WHERE id = ?`, late).Scan(&aggiornata); err != nil {
		t.Fatal(err)
	}
	pr.o.Sposta(time.Minute)

	e := pr.giro(t)
	if e.Lavoro != Ritardi || e.Esito != EsitoOK || e.Avvisi != 1 || e.Messaggi != 1 || e.Dettaglio != "1 busta in ritardo." {
		t.Errorf("esecuzione %+v", e)
	}
	m := pr.messaggi(t)
	if len(m) != 1 {
		t.Fatalf("messaggi %+v", m)
	}
	atteso := "🕒 Busta in ritardo\nBusta 1/26 — Cliente 1/26\nConsegna prevista: 16/11/2026 (1gg fa)\nAnello da stringere"
	if m[0].busta.Valid || m[0].canale != uscita.CanaleTelegram || m[0].parte != "shop" || m[0].indirizzo != chatFinta ||
		m[0].oggetto != "Busta in ritardo 1/26" || m[0].testo != atteso {
		t.Errorf("messaggio %+v", m[0])
	}
	if got := pr.timbro(t, late); got != pr.adessoIstante() {
		t.Errorf("timbro %q, atteso %q", got, pr.adessoIstante())
	}
	if pr.timbro(t, oggi) != "" || pr.timbro(t, domani) != "" {
		t.Error("timbrata una busta non in ritardo")
	}
	var dopo string
	if err := pr.d.Lettura.QueryRow(`SELECT aggiornato_il FROM buste WHERE id = ?`, late).Scan(&dopo); err != nil {
		t.Fatal(err)
	}
	if dopo != aggiornata {
		t.Errorf("aggiornato_il cambiato: %q → %q", aggiornata, dopo)
	}
	var attore string
	if err := pr.d.Lettura.QueryRow(`SELECT attore FROM eventi_audit WHERE azione = 'outgoing.queued' ORDER BY id LIMIT 1`).Scan(&attore); err != nil || attore != AttoreScheduler {
		t.Errorf("outgoing.queued: %q %v", attore, err)
	}

	e = pr.aMano(t)
	if e.Esito != EsitoOK || e.Avvisi != 0 || e.Messaggi != 0 || e.Dettaglio != "Nessuna busta in ritardo." {
		t.Errorf("seconda esecuzione %+v", e)
	}
	if len(pr.messaggi(t)) != 1 {
		t.Error("la busta è stata segnalata due volte")
	}
	pr.o.Sposta(48 * time.Hour)
	if e := pr.aMano(t); e.Avvisi != 2 || e.Dettaglio != "2 buste in ritardo." {
		t.Errorf("due giorni dopo %+v", e)
	}
}

// inventario: JOB-07
// La soglia del titolare (jobs.overdue_days) sposta il confine: in ritardo se la data promessa è al più oggi
// meno la soglia. Un valore che non è una soglia vale 1.
func TestRitardiSoglia(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	pr.busta(t, "5/26", 3)
	imposta(t, pr.d, ChiaveGiorniRitardi, "5")
	if e := pr.aMano(t); e.Avvisi != 0 {
		t.Errorf("soglia 5: %+v", e)
	}
	imposta(t, pr.d, ChiaveGiorniRitardi, " 3 ")
	if e := pr.aMano(t); e.Avvisi != 1 {
		t.Errorf("soglia 3: %+v", e)
	}
	for _, c := range []struct {
		v string
		n int
	}{{"1", 1}, {"+4", 4}, {"365", 365}, {"007", 7}, {"\t2\n", 2}, {"0", 1}, {"-3", 1}, {"366", 1}, {"", 1}, {"2.5", 1},
		{"2 giorni", 1}, {"+-2", 1}, {"+", 1}, {"99999999999999999999", 1}, {"٣", 1}} {
		imposta(t, pr.d, ChiaveGiorniRitardi, c.v)
		conf, err := LeggiConfig(context.Background(), pr.d.Lettura)
		if err != nil || conf.GiorniRitardi() != c.n {
			t.Errorf("%q: %d %v, attesi %d", c.v, conf.GiorniRitardi(), err, c.n)
		}
	}
	if (Config{}).GiorniRitardi() != 1 {
		t.Error("Config vuota")
	}
}

// inventario: JOB-07
// Gli stati chiusi (consegnata, annullata, chiusa del compro oro) e il cestino non si segnalano e non si
// timbrano; una busta pronta o in attesa di materiali sì (non è chiusa).
func TestRitardiStatiChiusiECestino(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	chiuse := map[string]int64{}
	for i, s := range []string{"consegnata", "annullata", "chiusa", "cestino"} {
		id := pr.busta(t, fmt.Sprintf("%d/26", 10+i), 3)
		switch s {
		case "chiusa":
			pr.esegui(t, `UPDATE buste SET compro_oro = 1, stato = 'chiusa' WHERE id = ?`, id)
		case "cestino":
			pr.esegui(t, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, pr.adessoIstante(), id)
		default:
			pr.esegui(t, `UPDATE buste SET stato = ? WHERE id = ?`, s, id)
		}
		chiuse[s] = id
	}
	pronta := pr.busta(t, "20/26", 3)
	pr.esegui(t, `UPDATE buste SET stato = 'pronta' WHERE id = ?`, pronta)
	attesa := pr.busta(t, "21/26", 3)
	pr.esegui(t, `UPDATE buste SET stato = 'in_attesa_materiali' WHERE id = ?`, attesa)

	if e := pr.aMano(t); e.Avvisi != 2 || e.Messaggi != 2 {
		t.Errorf("esecuzione %+v", e)
	}
	for s, id := range chiuse {
		if pr.timbro(t, id) != "" {
			t.Errorf("%s timbrata", s)
		}
	}
	for _, m := range pr.messaggi(t) {
		if !strings.Contains(m.oggetto, "20/26") && !strings.Contains(m.oggetto, "21/26") {
			t.Errorf("segnalata %q", m.oggetto)
		}
	}
	if len(buste.StatiChiusi()) != 3 {
		t.Errorf("stati chiusi %v", buste.StatiChiusi())
	}
}

// inventario: JOB-07
// Con email e Telegram attivi la stessa busta è un messaggio per canale (email al mittente del negozio con
// l'oggetto, Telegram alla chat): un avviso, due messaggi.
func TestRitardiDueCanali(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiEmail(t, pr.d)
	accendiTelegram(t, pr.d)
	pr.busta(t, "6/26", 2)
	e := pr.aMano(t)
	if e.Avvisi != 1 || e.Messaggi != 2 || e.Dettaglio != "1 busta in ritardo." {
		t.Errorf("esecuzione %+v", e)
	}
	m := pr.messaggi(t)
	if len(m) != 2 || m[0].canale != uscita.CanaleEmail || m[0].indirizzo != mittenteFinto || m[1].canale != uscita.CanaleTelegram ||
		m[1].indirizzo != chatFinta || m[0].testo != m[1].testo || m[0].oggetto != "Busta in ritardo 6/26" || m[1].oggetto != m[0].oggetto {
		t.Errorf("messaggi %+v", m)
	}
}

// inventario: JOB-07
// La descrizione si taglia a 200 grafemi con «…»; vuota o di soli spazi, la riga non c'è. Il nome è quello
// copiato sulla busta.
func TestRitardiTesto(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	lunga := pr.busta(t, "7/26", 1)
	pr.esegui(t, `UPDATE buste SET descrizione = ?, nome_cliente = 'Ada <Rossi>' WHERE id = ?`, "  "+strings.Repeat("👍🏽", 199)+"ab  ", lunga)
	giusta := pr.busta(t, "8/26", 1)
	pr.esegui(t, `UPDATE buste SET descrizione = ? WHERE id = ?`, strings.Repeat("x", 200), giusta)
	accenti := pr.busta(t, "10/26", 1)
	pr.esegui(t, `UPDATE buste SET descrizione = ? WHERE id = ?`, strings.Repeat("è", 150), accenti)
	vuota := pr.busta(t, "9/26", 1)
	pr.esegui(t, `UPDATE buste SET descrizione = ? WHERE id = ?`, " \n ", vuota)
	pr.aMano(t)
	m := pr.messaggi(t)
	if len(m) != 4 {
		t.Fatalf("messaggi %+v", m)
	}
	if !strings.HasSuffix(m[2].testo, "\n"+strings.Repeat("è", 150)) {
		t.Errorf("150 grafemi in 300 byte %q", m[2].testo)
	}
	if want := "🕒 Busta in ritardo\nBusta 7/26 — Ada <Rossi>\nConsegna prevista: 16/11/2026 (1gg fa)\n" + strings.Repeat("👍🏽", 199) + "a…"; m[0].testo != want {
		t.Errorf("tagliata %q", m[0].testo)
	}
	if !strings.HasSuffix(m[1].testo, "\n"+strings.Repeat("x", 200)) {
		t.Errorf("200 grafemi %q", m[1].testo)
	}
	if m[3].testo != "🕒 Busta in ritardo\nBusta 9/26 — Cliente 9/26\nConsegna prevista: 16/11/2026 (1gg fa)" {
		t.Errorf("vuota %q", m[3].testo)
	}
}

// inventario: JOB-07
// Slice 211: una data promessa spostata a un altro giorno riapre l'avviso; spostata in avanti si segnala solo
// dopo il giorno nuovo, spostata indietro al giro dopo. Togliere la data tiene il timbro; una busta chiusa con la
// data spostata non si segnala.
func TestRitardiDataSpostataRiapre(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	avanti := pr.busta(t, "1/26", 2)
	indietro := pr.busta(t, "2/26", 2)
	tolta := pr.busta(t, "3/26", 2)
	chiusa := pr.busta(t, "4/26", 2)
	if e := pr.aMano(t); e.Avvisi != 4 {
		t.Fatalf("prima %+v", e)
	}
	sposta := func(id int64, giorno string) {
		t.Helper()
		if _, err := pr.b.Modifica(context.Background(), id, buste.Modifica{DataPrevista: &giorno}, nil, titolare); err != nil {
			t.Fatal(err)
		}
	}
	sposta(avanti, pr.giorno(3))
	sposta(indietro, pr.giorno(-5))
	sposta(chiusa, pr.giorno(1))
	pr.esegui(t, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, chiusa)
	if err := pr.b.TogliData(context.Background(), tolta, titolare); err != nil {
		t.Fatal(err)
	}
	if pr.timbro(t, avanti) != "" || pr.timbro(t, indietro) != "" || pr.timbro(t, tolta) == "" {
		t.Fatal("riapertura della 211")
	}
	e := pr.aMano(t)
	if e.Avvisi != 1 {
		t.Errorf("stesso giorno %+v", e)
	}
	if m := pr.messaggi(t); !strings.Contains(m[len(m)-1].testo, "Busta 2/26 — ") || !strings.Contains(m[len(m)-1].testo, "(5gg fa)") {
		t.Errorf("indietro: %q", m[len(m)-1].testo)
	}
	pr.o.Sposta(3 * 24 * time.Hour)
	if e := pr.aMano(t); e.Avvisi != 0 {
		t.Errorf("il giorno nuovo %+v", e)
	}
	pr.o.Sposta(24 * time.Hour)
	if e := pr.aMano(t); e.Avvisi != 1 {
		t.Errorf("il giorno dopo il nuovo %+v", e)
	}
	if m := pr.messaggi(t); !strings.Contains(m[len(m)-1].testo, "Busta 1/26 — ") || !strings.Contains(m[len(m)-1].testo, "(1gg fa)") {
		t.Errorf("avanti: %q", m[len(m)-1].testo)
	}
	if pr.timbro(t, chiusa) != "" {
		t.Error("chiusa timbrata")
	}
}

// inventario: JOB-07
// Slice 213: il timbro ricontrolla il predicato e il messaggio si scrive dalla riga timbrata. Una modifica fra
// l'elenco e il timbro: data in avanti → niente timbro né messaggio (si segnala dopo il giorno nuovo); data su un
// altro giorno in ritardo → timbro e messaggio col giorno nuovo; data tolta, busta consegnata, nel cestino o
// timbrata da un altro → niente.
func TestRitardiTimbroRicontrolla(t *testing.T) {
	for _, c := range []struct {
		nome     string
		cambia   func(pr *provaRitardi, id int64)
		avvisi   int
		testo    string
		timbrata bool
	}{
		{"data in avanti", func(pr *provaRitardi, id int64) {
			g := pr.giorno(3)
			if _, err := pr.b.Modifica(context.Background(), id, buste.Modifica{DataPrevista: &g}, nil, titolare); err != nil {
				t.Fatal(err)
			}
		}, 0, "", false},
		{"altro giorno in ritardo", func(pr *provaRitardi, id int64) {
			g := pr.giorno(-5)
			if _, err := pr.b.Modifica(context.Background(), id, buste.Modifica{DataPrevista: &g}, nil, titolare); err != nil {
				t.Fatal(err)
			}
		}, 1, "Consegna prevista: 12/11/2026 (5gg fa)", true},
		{"data tolta", func(pr *provaRitardi, id int64) {
			if err := pr.b.TogliData(context.Background(), id, titolare); err != nil {
				t.Fatal(err)
			}
		}, 0, "", false},
		{"consegnata", func(pr *provaRitardi, id int64) {
			pr.esegui(t, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, id)
		}, 0, "", false},
		{"cestino", func(pr *provaRitardi, id int64) {
			pr.esegui(t, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, pr.adessoIstante(), id)
		}, 0, "", false},
		{"timbrata da un altro", func(pr *provaRitardi, id int64) {
			pr.esegui(t, `UPDATE buste SET ritardo_avvisato_il = '2026-11-17T08:00:00.000000Z' WHERE id = ?`, id)
		}, 0, "", true},
	} {
		t.Run(c.nome, func(t *testing.T) {
			pr := nuovaProvaRitardi(t, nil)
			accendiTelegram(t, pr.d)
			id := pr.busta(t, "20/26", 2)
			chiamate := 0
			pr.l.primaDelTimbro = func(b int64) {
				chiamate++
				if b != id {
					t.Errorf("busta %d", b)
				}
				c.cambia(pr, id)
			}
			e := pr.aMano(t)
			pr.l.primaDelTimbro = nil
			if chiamate != 1 || e.Esito != EsitoOK || e.Avvisi != c.avvisi || e.Messaggi != c.avvisi {
				t.Errorf("esecuzione %+v, chiamate %d", e, chiamate)
			}
			m := pr.messaggi(t)
			if c.avvisi == 0 && len(m) != 0 || c.avvisi == 1 && (len(m) != 1 || !strings.Contains(m[0].testo, c.testo)) {
				t.Errorf("messaggi %+v", m)
			}
			if (pr.timbro(t, id) != "") != c.timbrata {
				t.Errorf("timbro %q", pr.timbro(t, id))
			}
			if c.nome == "data in avanti" {
				pr.o.Sposta(4 * 24 * time.Hour)
				if e := pr.aMano(t); e.Avvisi != 1 {
					t.Errorf("dopo il giorno nuovo %+v", e)
				}
			}
		})
	}
}

// postaCheRifiuta rifiuta con err le righe il cui oggetto contiene numero (e, se canale non è vuoto, solo su
// quel canale); le altre vanno alla coda vera.
type postaCheRifiuta struct {
	*uscita.Coda
	numero, canale string
	err            error
}

func (p postaCheRifiuta) AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error) {
	if cont.Oggetto != nil && (p.numero == "*" || strings.HasSuffix(*cont.Oggetto, " "+p.numero)) && (p.canale == "" || p.canale == dest.Canale) {
		return 0, p.err
	}
	return p.Coda.AccodaIn(ctx, tx, dest, cont, a)
}

// inventario: JOB-07
// Un rifiuto della coda per una busta non ferma le altre: quella busta non si timbra e non ha messaggi (anche
// quello già accodato su un altro canale si annulla), le altre sì; l'esecuzione è «Fallito» con il conto e il
// primo motivo, e al giro dopo la busta rifiutata si segnala.
func TestRitardiUnErroreNonFermaLeAltre(t *testing.T) {
	for _, c := range []struct {
		err    error
		canale string
		motivo string
	}{
		{uscita.ErrMessaggioNonValido, uscita.CanaleTelegram, "la coda ha rifiutato il messaggio della busta 2/26"},
		{uscita.ErrSoloAMano, "", "la coda ha rifiutato il messaggio della busta 2/26"},
		{fmt.Errorf("avvolto: %w", uscita.ErrNonConfigurato), uscita.CanaleTelegram, "il canale telegram non è più configurato"},
	} {
		rifiuta := &postaCheRifiuta{numero: "2/26", canale: c.canale, err: c.err}
		pr := nuovaProvaRitardi(t, func(q *uscita.Coda) Accodatore { rifiuta.Coda = q; return rifiuta })
		accendiEmail(t, pr.d)
		accendiTelegram(t, pr.d)
		var registro bytes.Buffer
		pr.l.ConRegistro(slog.New(slog.NewTextHandler(&registro, nil)))
		uno, due, tre := pr.busta(t, "1/26", 2), pr.busta(t, "2/26", 2), pr.busta(t, "3/26", 2)
		e := pr.giro(t)
		if r := registro.String(); !strings.Contains(r, "ritardi: avviso di una busta non riuscito") || !strings.Contains(r, c.motivo) ||
			strings.Contains(r, chatFinta) || strings.Contains(r, mittenteFinto) || strings.Contains(r, "Cliente") {
			t.Errorf("%v: registro %q", c.err, r)
		}
		if e.Esito != EsitoFallito || e.Avvisi != 2 || e.Messaggi != 4 || e.Dettaglio != "2 buste in ritardo. 1 non riuscite: "+c.motivo {
			t.Errorf("%v: esecuzione %+v", c.err, e)
		}
		if pr.timbro(t, uno) == "" || pr.timbro(t, due) != "" || pr.timbro(t, tre) == "" {
			t.Errorf("%v: timbri", c.err)
		}
		for _, m := range pr.messaggi(t) {
			if strings.Contains(m.oggetto, "2/26") {
				t.Errorf("%v: messaggio della busta rifiutata su %s", c.err, m.canale)
			}
		}
		rifiuta.numero = "nessuna"
		pr.o.Sposta(time.Minute)
		if e := pr.giro(t); e.Esito != EsitoOK || e.Avvisi != 1 || e.Messaggi != 2 {
			t.Errorf("%v: giro dopo %+v", c.err, e)
		}
	}
}

// inventario: JOB-07
// Più buste rifiutate: il conto è di tutte, il motivo è il primo. Un guasto che non è un rifiuto della coda
// (il database) ferma il lavoro come l'eccezione dello standalone: «Il job si è interrotto», le buste già
// timbrate restano timbrate con i loro messaggi, le altre aspettano il giro dopo.
func TestRitardiRifiutiEGuasti(t *testing.T) {
	rifiuta := &postaCheRifiuta{numero: "", err: uscita.ErrMessaggioNonValido}
	pr := nuovaProvaRitardi(t, func(q *uscita.Coda) Accodatore { rifiuta.Coda = q; return rifiuta })
	accendiTelegram(t, pr.d)
	pr.busta(t, "1/26", 2)
	pr.busta(t, "2/26", 2)
	rifiuta.numero = "*" // tutte
	rifiuta.err = uscita.ErrMessaggioNonValido
	e := pr.aMano(t)
	if e.Esito != EsitoFallito || e.Avvisi != 0 || e.Dettaglio != "Nessuna busta in ritardo. 2 non riuscite: la coda ha rifiutato il messaggio della busta 1/26" {
		t.Errorf("tutte rifiutate %+v", e)
	}

	rifiuta.numero, rifiuta.err = "2/26", errors.New("disco pieno: indirizzo segreto@esempio.it")
	tre := pr.busta(t, "3/26", 2)
	e = pr.aMano(t)
	if e.Esito != EsitoFallito || e.Avvisi != 0 || e.Dettaglio != DettaglioInterrotto {
		t.Errorf("guasto %+v", e)
	}
	if len(pr.messaggi(t)) != 1 || pr.timbro(t, tre) != "" {
		t.Errorf("dopo il guasto: messaggi %+v, timbro della 3/26 %q", pr.messaggi(t), pr.timbro(t, tre))
	}
	rifiuta.numero = "nessuna"
	if e := pr.aMano(t); e.Esito != EsitoOK || e.Avvisi != 2 {
		t.Errorf("dopo %+v", e)
	}
}

// inventario: JOB-08
// La prima esecuzione vera timbra senza messaggi le buste in ritardo da 7 giorni o più (non chiuse, non nel
// cestino), segnala le altre e scrive la chiave «1»; la frase lo dice. Il giorno dopo il silenziamento non si
// rifà: una busta in ritardo da 40 giorni si segnala.
func TestRitardiSilenziamento(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	trenta := pr.busta(t, "1/26", 30)
	sette := pr.busta(t, "2/26", 7)
	sei := pr.busta(t, "3/26", 6)
	vecchiaChiusa := pr.busta(t, "4/26", 30)
	pr.esegui(t, `UPDATE buste SET stato = 'annullata' WHERE id = ?`, vecchiaChiusa)
	vecchiaNelCestino := pr.busta(t, "5/26", 30)
	pr.esegui(t, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, pr.adessoIstante(), vecchiaNelCestino)
	futura := pr.busta(t, "6/26", -10)

	e := pr.giro(t)
	if e.Esito != EsitoOK || e.Avvisi != 1 || e.Messaggi != 1 || e.Dettaglio != "1 busta in ritardo. 2 buste storiche silenziate alla prima esecuzione." {
		t.Errorf("prima esecuzione %+v", e)
	}
	m := pr.messaggi(t)
	if len(m) != 1 || m[0].oggetto != "Busta in ritardo 3/26" {
		t.Errorf("messaggi %+v", m)
	}
	for _, id := range []int64{trenta, sette, sei} {
		if pr.timbro(t, id) != pr.adessoIstante() {
			t.Errorf("busta %d non timbrata", id)
		}
	}
	for _, id := range []int64{vecchiaChiusa, vecchiaNelCestino, futura} {
		if pr.timbro(t, id) != "" {
			t.Errorf("busta %d timbrata", id)
		}
	}
	if v, ok := pr.silenziato(t); !ok || v != "1" {
		t.Errorf("chiave %q %v", v, ok)
	}

	pr.o.Sposta(24 * time.Hour)
	pr.busta(t, "7/26", 40)
	e = pr.giro(t)
	if e.Avvisi != 1 || strings.Contains(e.Dettaglio, "silenziate") {
		t.Errorf("seconda esecuzione %+v", e)
	}
}

// inventario: JOB-08
// Silenziamento senza buste da silenziare: la chiave si scrive lo stesso e la frase non dice niente; con una
// sola busta silenziata la frase è quella dello standalone («1 busta storiche»).
func TestRitardiSilenziamentoFrasi(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	if e := pr.aMano(t); e.Dettaglio != "Nessuna busta in ritardo." {
		t.Errorf("vuoto %+v", e)
	}
	if _, ok := pr.silenziato(t); !ok {
		t.Error("chiave non scritta")
	}

	pr2 := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr2.d)
	pr2.busta(t, "1/26", 9)
	if e := pr2.aMano(t); e.Avvisi != 0 || e.Dettaglio != "Nessuna busta in ritardo. 1 busta storiche silenziate alla prima esecuzione." {
		t.Errorf("una %+v", e)
	}
}

// inventario: JOB-08
// Il silenziamento avviene alla prima esecuzione vera, cioè con almeno un canale e il lavoro acceso, anche a
// mano (lo standalone non distingue): non senza canali (D-16, niente riga), non con il lavoro spento («Saltato»),
// non in un giorno chiuso (D-17). Con la chiave già presente (importata, qualunque valore) non si rifà e la busta
// vecchia si segnala.
func TestRitardiSilenziamentoSoloAllaPrimaVera(t *testing.T) {
	ctx := context.Background()
	pr := nuovaProvaRitardi(t, nil)
	vecchia := pr.busta(t, "1/26", 30)

	if es, err := pr.p.Giro(ctx); err != nil || len(es) != 0 {
		t.Fatalf("giro senza canali: %v %+v", err, es)
	}
	if _, err := pr.p.EseguiAMano(ctx, Ritardi, titolare); !errors.Is(err, ErrSenzaCanali) {
		t.Fatalf("a mano senza canali: %v", err)
	}
	accendiTelegram(t, pr.d)
	imposta(t, pr.d, "jobs.overdue_enabled", "false")
	if e := pr.aMano(t); e.Esito != EsitoSaltato {
		t.Fatalf("spento %+v", e)
	}
	imposta(t, pr.d, "jobs.overdue_enabled", "true")
	pr.o.Sposta(-24 * time.Hour) // lunedì, chiuso
	if es, err := pr.p.Giro(ctx); err != nil || len(es) != 0 {
		t.Fatalf("giro di lunedì: %v %+v", err, es)
	}
	if _, ok := pr.silenziato(t); ok || pr.timbro(t, vecchia) != "" || len(pr.messaggi(t)) != 0 {
		t.Fatal("silenziamento senza un'esecuzione vera")
	}
	if e := pr.aMano(t); e.Avvisi != 0 || !strings.Contains(e.Dettaglio, "1 busta storiche silenziate") {
		t.Errorf("a mano di lunedì %+v", e)
	}
	if _, ok := pr.silenziato(t); !ok || pr.timbro(t, vecchia) == "" {
		t.Error("l'esecuzione a mano non ha silenziato")
	}

	imp := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, imp.d)
	imposta(t, imp.d, ChiaveStoricoSilenziato, "2026-02-03T04:05:06.000000Z")
	imp.busta(t, "1/26", 30)
	if e := imp.giro(t); e.Avvisi != 1 || strings.Contains(e.Dettaglio, "silenziate") {
		t.Errorf("chiave importata %+v", e)
	}
	if v, _ := imp.silenziato(t); v != "2026-02-03T04:05:06.000000Z" {
		t.Errorf("chiave riscritta: %q", v)
	}
}

// inventario: JOB-08
// La chiave e i timbri del silenziamento sono una transazione: se la chiave non si scrive, nessuna busta resta
// timbrata, il lavoro è interrotto e il giro dopo rifà tutto.
func TestRitardiSilenziamentoTuttoONiente(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	vecchia := pr.busta(t, "1/26", 30)
	pr.busta(t, "2/26", 2)
	pr.esegui(t, `CREATE TRIGGER niente_chiave BEFORE INSERT ON impostazioni WHEN NEW.chiave = '`+ChiaveStoricoSilenziato+`'
		BEGIN SELECT RAISE(ABORT, 'no'); END`)
	e := pr.giro(t)
	if e.Esito != EsitoFallito || e.Dettaglio != DettaglioInterrotto {
		t.Errorf("esecuzione %+v", e)
	}
	if pr.timbro(t, vecchia) != "" || len(pr.messaggi(t)) != 0 {
		t.Error("timbri senza la chiave")
	}
	pr.esegui(t, `DROP TRIGGER niente_chiave`)
	pr.o.Sposta(time.Minute)
	if e := pr.giro(t); e.Avvisi != 1 || !strings.Contains(e.Dettaglio, "1 busta storiche silenziate") || pr.timbro(t, vecchia) == "" {
		t.Errorf("giro dopo %+v", e)
	}
}

// inventario: JOB-07
// Uno stop a metà (il contesto finito dopo la prima busta): il lavoro torna con l'errore dello stop, niente riga;
// la busta timbrata resta timbrata con il suo messaggio. Dopo il riavvio (pianificatore e lavoro nuovi sullo stesso
// database) il giro dello stesso giorno è ancora dovuto e segnala solo le altre: nessun messaggio doppio.
func TestRitardiStopERiavvio(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	uno, due := pr.busta(t, "1/26", 2), pr.busta(t, "2/26", 2)
	ctx, ferma := context.WithCancel(context.Background())
	pr.l.primaDelTimbro = func(b int64) {
		if b == due {
			ferma()
		}
	}
	es, err := pr.p.Giro(ctx)
	if len(es) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("giro fermato: %v %+v", err, es)
	}
	if n := len(pr.messaggi(t)); n != 1 || pr.timbro(t, uno) == "" || pr.timbro(t, due) != "" {
		t.Fatalf("dopo lo stop: %d messaggi", n)
	}
	if st, _ := pr.p.Storico(context.Background(), 10); len(st) != 0 {
		t.Errorf("righe dopo lo stop: %+v", st)
	}

	l := NuoviRitardi(pr.d, pr.coda)
	p := Nuovo(pr.d, pr.o, pr.coda, map[string]Lavoro{Ritardi: l})
	pr.o.Sposta(time.Minute)
	es, err = p.Giro(context.Background())
	if err != nil || len(es) != 1 || es[0].Avvisi != 1 || es[0].Esito != EsitoOK {
		t.Fatalf("dopo il riavvio: %v %+v", err, es)
	}
	m := pr.messaggi(t)
	if len(m) != 2 || m[0].oggetto != "Busta in ritardo 1/26" || m[1].oggetto != "Busta in ritardo 2/26" {
		t.Errorf("messaggi %+v", m)
	}
	pr.o.Sposta(time.Minute)
	if es, err := p.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Errorf("giro dopo l'ok: %v %+v", err, es)
	}
	// Uno stop prima di ogni busta: il silenziamento è già fatto, nessuna busta, nessuna riga.
	pr2 := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr2.d)
	pr2.busta(t, "1/26", 30)
	ctx2, ferma2 := context.WithCancel(context.Background())
	ferma2()
	if _, err := pr2.l.Esegui(ctx2, Config{}, pr2.o.Adesso(), []uscita.Destinazione{{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: chatFinta}}, titolare); !errors.Is(err, context.Canceled) {
		t.Errorf("stop prima: %v", err)
	}
}

// inventario: JOB-07
// Il recupero della D-17: il lunedì (chiuso) la busta promessa per domenica è già in ritardo ma il giro non fa
// niente; il martedì la trova da solo e la segnala col ritardo vero.
func TestRitardiRecuperoD17(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	pr.o.Imposta(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)) // lunedì 16/11, 10:00 in negozio
	id := pr.busta(t, "1/26", 1)                                // domenica 15/11
	if es, err := pr.p.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Fatalf("lunedì: %v %+v", err, es)
	}
	if pr.timbro(t, id) != "" || len(pr.messaggi(t)) != 0 {
		t.Fatal("lunedì ha scritto")
	}
	pr.o.Imposta(martedi)
	e := pr.giro(t)
	if e.Avvisi != 1 || e.Esito != EsitoOK {
		t.Errorf("martedì %+v", e)
	}
	if m := pr.messaggi(t); len(m) != 1 || !strings.Contains(m[0].testo, "Consegna prevista: 15/11/2026 (2gg fa)") {
		t.Errorf("messaggi %+v", m)
	}
}

// inventario: JOB-07
// Il timbro ricontrolla anche la soglia: con soglia 3, una data spostata fra l'elenco e il timbro a 2 giorni fa non
// è più in ritardo e la busta non si timbra.
func TestRitardiTimbroRicontrollaLaSoglia(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	imposta(t, pr.d, ChiaveGiorniRitardi, "3")
	id := pr.busta(t, "1/26", 5)
	pr.l.primaDelTimbro = func(int64) {
		pr.esegui(t, `UPDATE buste SET data_prevista = ? WHERE id = ?`, pr.giorno(-2), id)
	}
	if e := pr.aMano(t); e.Avvisi != 0 || pr.timbro(t, id) != "" || len(pr.messaggi(t)) != 0 {
		t.Errorf("%+v timbro %q", e, pr.timbro(t, id))
	}
}

// inventario: JOB-07
// «Oggi» è quello del negozio: alle 23:30 UTC del 17/11 in negozio è già il 18, e la busta promessa per il 17 è in
// ritardo di un giorno.
func TestRitardiOggiDelNegozio(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	pr.o.Imposta(time.Date(2026, 11, 17, 23, 30, 0, 0, time.UTC))
	imposta(t, pr.d, ChiaveStoricoSilenziato, "1")
	id := pr.busta(t, "1/26", 1)
	e := pr.aMano(t)
	if e.Avvisi != 1 || pr.timbro(t, id) == "" {
		t.Errorf("%+v", e)
	}
	if m := pr.messaggi(t); len(m) != 1 || !strings.Contains(m[0].testo, "Consegna prevista: 17/11/2026 (1gg fa)") {
		t.Errorf("messaggi %+v", m)
	}
}

// postaCheFerma ferma il contesto e risponde con un errore che non è quello del contesto (come un driver che dice
// «interrupted»).
type postaCheFerma struct {
	*uscita.Coda
	ferma func()
}

func (p postaCheFerma) AccodaIn(context.Context, *sql.Tx, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	p.ferma()
	return 0, errors.New("sqlite: interrupted")
}

// inventario: JOB-07
// Lo stop che arriva durante l'accodamento con un errore che non è quello del contesto è comunque uno stop: niente
// riga «interrotto», niente timbro, il giorno resta da fare.
func TestRitardiStopConErroreNonDelContesto(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	pr := nuovaProvaRitardi(t, func(q *uscita.Coda) Accodatore { return postaCheFerma{q, ferma} })
	accendiTelegram(t, pr.d)
	id := pr.busta(t, "1/26", 2)
	es, err := pr.p.Giro(ctx)
	if !errors.Is(err, context.Canceled) || len(es) != 0 {
		t.Errorf("%v %+v", err, es)
	}
	if st, _ := pr.p.Storico(context.Background(), 10); len(st) != 0 {
		t.Errorf("righe %+v", st)
	}
	if pr.timbro(t, id) != "" {
		t.Error("timbrata")
	}
}

// inventario: JOB-07
// Una data promessa che il calendario non ha (2026-02-31: la colonna la accetta, il tipo date dello standalone no) è
// un rifiuto di quella busta: niente timbro né messaggio, le altre vanno avanti, «Fallito» con il motivo; nel
// registro l'id della busta e il motivo, niente altro.
func TestRitardiDataPromessaNonValida(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	accendiTelegram(t, pr.d)
	var registro bytes.Buffer
	pr.l.ConRegistro(slog.New(slog.NewTextHandler(&registro, nil)))
	imposta(t, pr.d, ChiaveStoricoSilenziato, "1") // la data storta viene prima di oggi − 7: si silenzierebbe
	storta := pr.busta(t, "1/26", 2)
	pr.esegui(t, `UPDATE buste SET data_prevista = '2026-02-31' WHERE id = ?`, storta)
	buona := pr.busta(t, "2/26", 2)
	e := pr.aMano(t)
	if e.Esito != EsitoFallito || e.Avvisi != 1 || e.Messaggi != 1 ||
		e.Dettaglio != "1 busta in ritardo. 1 non riuscite: data promessa non valida della busta 1/26" {
		t.Errorf("esecuzione %+v", e)
	}
	if pr.timbro(t, storta) != "" || pr.timbro(t, buona) == "" {
		t.Error("timbri")
	}
	if m := pr.messaggi(t); len(m) != 1 || m[0].oggetto != "Busta in ritardo 2/26" {
		t.Errorf("messaggi %+v", m)
	}
	r := registro.String()
	if !strings.Contains(r, fmt.Sprintf("busta=%d", storta)) || strings.Contains(r, "Cliente") || strings.Contains(r, chatFinta) {
		t.Errorf("registro %q", r)
	}
}

// inventario: JOB-08
// Il numero delle buste silenziate va nel registro (Info) appena il silenziamento è scritto: resta anche se poi
// l'esecuzione si interrompe e la riga dice solo «interrotto».
func TestRitardiSilenziamentoNelRegistro(t *testing.T) {
	rifiuta := &postaCheRifiuta{numero: "2/26", err: errors.New("guasto")}
	pr := nuovaProvaRitardi(t, func(q *uscita.Coda) Accodatore { rifiuta.Coda = q; return rifiuta })
	accendiTelegram(t, pr.d)
	var registro bytes.Buffer
	pr.l.ConRegistro(slog.New(slog.NewTextHandler(&registro, nil)))
	pr.busta(t, "1/26", 30)
	pr.busta(t, "2/26", 2)
	if e := pr.aMano(t); e.Dettaglio != DettaglioInterrotto {
		t.Errorf("esecuzione %+v", e)
	}
	if r := registro.String(); !strings.Contains(r, "level=INFO") || !strings.Contains(r, "storico silenziato") || !strings.Contains(r, "buste=1") {
		t.Errorf("registro %q", r)
	}
}

// inventario: JOB-07
// Senza destinatari il lavoro non timbra niente (né silenziamento né avvisi) e torna subito con ErrSenzaCanali: il
// pianificatore non lo chiama mai così (D-16), ma un timbro senza messaggio sarebbe un avviso perso.
func TestRitardiSenzaDestinatari(t *testing.T) {
	pr := nuovaProvaRitardi(t, nil)
	vecchia, nuova := pr.busta(t, "1/26", 30), pr.busta(t, "2/26", 2)
	if _, err := pr.l.Esegui(context.Background(), Config{}, pr.o.Adesso(), nil, titolare); !errors.Is(err, ErrSenzaCanali) {
		t.Errorf("errore %v", err)
	}
	if _, ok := pr.silenziato(t); ok || pr.timbro(t, vecchia) != "" || pr.timbro(t, nuova) != "" {
		t.Error("timbrato senza destinatari")
	}
}
