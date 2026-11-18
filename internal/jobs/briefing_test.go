package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Briefing del mattino» (slice 097, JOB-10, JOB-11), come briefing_test.exs dello standalone
// (go-baseline): database vero, consegne vere, coda vera con i provider veri (non vanno in rete), orologio finto
// (martedì 17/11/2026, 10:00 in negozio). Nomi, numeri e indirizzi inventati.

type provaBriefing struct {
	*provaSolleciti
	lb *LavoroBriefing
	pb *Pianificatore
}

// nuovaProvaBriefing è il pianificatore con il solo briefing, acceso, sulla coda di serve con Telegram acceso.
// posta, se c'è, sta fra il lavoro e la coda.
func nuovaProvaBriefing(t *testing.T, posta func(*uscita.Coda) Accodatore) *provaBriefing {
	t.Helper()
	ps := nuovaProvaSolleciti(t, nil)
	var acc Accodatore = ps.coda
	if posta != nil {
		acc = posta(ps.coda)
	}
	imposta(t, ps.d, "jobs.briefing_enabled", "1")
	lb := NuovoBriefing(ps.d, acc)
	return &provaBriefing{provaSolleciti: ps, lb: lb, pb: Nuovo(ps.d, ps.o, ps.coda, map[string]Lavoro{Briefing: lb})}
}

// nuova crea la busta numero (codice «QR-<numero>») del cliente con la descrizione e la data promessa
// (aaaa-mm-gg o "").
func (pb *provaBriefing) nuova(t *testing.T, numero, cliente, descrizione, prevista string) int64 {
	t.Helper()
	return pb.nuovaConCodice(t, numero, "QR-"+strings.ReplaceAll(numero, "/", "-"), cliente, descrizione, prevista)
}

func (pb *provaBriefing) nuovaConCodice(t *testing.T, numero, qr, cliente, descrizione, prevista string) int64 {
	t.Helper()
	a := buste.Accettazione{NomeCliente: cliente, TelefonoCliente: "333 000 0004", Numero: numero,
		DataAccettazione: "2026-09-01", DataPrevista: prevista, Descrizione: descrizione, CodiceQR: qr}
	a.GemelleConfermate = buste.EtichettaScritta(qr, buste.AnnoDiAccettazione(a.DataAccettazione, pb.o.Adesso()))
	c, err := pb.b.Crea(context.Background(), a, titolare)
	if err != nil {
		t.Fatalf("busta %s: %v", numero, err)
	}
	pb.qr[c.ID] = qr
	return c.ID
}

// pronta porta la riparazione in «pronta» con la data di pronta (aaaa-mm-gg o "" per NULL).
func (pb *provaBriefing) pronta(t *testing.T, id int64, quando string) {
	t.Helper()
	var d any
	if quando != "" {
		d = quando
	}
	pb.esegui(t, `UPDATE buste SET stato = 'pronta', data_pronta = ? WHERE id = ?`, d, id)
}

// valutata fa della busta un compro oro valutato (il suo stato «pronto»).
func (pb *provaBriefing) valutata(t *testing.T, id int64) {
	t.Helper()
	pb.esegui(t, `UPDATE buste SET compro_oro = 1, stato = 'valutata' WHERE id = ?`, id)
}

// fuoriDa manda la busta dall'artigiano giorni giorni fa (blocchi di 86 400 s dall'orologio).
func (pb *provaBriefing) fuoriDa(t *testing.T, artigiano string, id int64, giorni int) {
	t.Helper()
	pb.manda(t, artigiano, id, pb.o.Adesso().Add(-time.Duration(giorni)*24*time.Hour))
}

func (pb *provaBriefing) cestino(t *testing.T, id int64) {
	t.Helper()
	pb.esegui(t, `UPDATE buste SET cancellata_il = '2026-11-16T08:00:00.000000Z' WHERE id = ?`, id)
}

func (pb *provaBriefing) aManoB(t *testing.T) Esecuzione {
	t.Helper()
	e, err := pb.pb.EseguiAMano(context.Background(), Briefing, titolare)
	if err != nil {
		t.Fatalf("a mano: %v", err)
	}
	return e
}

// testi sono i testi dei messaggi in coda, nell'ordine in cui sono stati accodati.
func (pb *provaBriefing) testi(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range pb.messaggi(t) {
		out = append(out, m.testo)
	}
	return out
}

// unita è il conto di Telegram, scritto qui con la libreria standard come controllo indipendente.
func unita(s string) int { return len(utf16.Encode([]rune(s))) }

// righeDi sono i numeri delle buste della sezione che comincia con marca, in ordine.
func righeDi(corpo, marca string) []string {
	var out []string
	for _, blocco := range strings.Split(corpo, "\n\n") {
		if !strings.HasPrefix(blocco, marca) {
			continue
		}
		for _, r := range strings.Split(blocco, "\n") {
			if strings.HasPrefix(r, "• #") {
				out = append(out, strings.Fields(r)[1])
			}
		}
	}
	return out
}

const vuoto = "🌅 Briefing 17/11 — nulla all'orizzonte ☕"

// inventario: JOB-10
// Un negozio sintetico con una busta per ognuna delle cinque domande e una appena fuori da ognuna: un messaggio
// solo, al bot Telegram del negozio, con le cinque sezioni dello standalone nel suo ordine e i suoi testi. Fuori:
// la pronta già avvisata, quella da 10 giorni dall'artigiano, la promessa fra quattro giorni, la consegnata e le
// buste nel cestino (anche fuori bottega da 20 giorni). Nessun timbro, nessun parse_mode.
func TestBriefingCinqueSezioni(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	avvisata := pb.nuova(t, "2/26", "Beatrice", "Non da avvisare", "")
	pb.pronta(t, avvisata, "2026-11-10")
	pb.esegui(t, `UPDATE buste SET avviso_pronta_il = '2026-11-16T08:00:00.000000Z' WHERE id = ?`, avvisata)
	pb.valutata(t, pb.nuova(t, "3/26", "Bruno", "Oro rotto", ""))
	pb.nuova(t, "4/26", "Carla", "Catena", "2026-11-10")
	pb.nuova(t, "5/26", "Dino", "Orologio", "2026-11-19")
	pb.nuova(t, "6/26", "Egle", "Troppo lontana", "2026-11-21")
	pb.fuoriDa(t, "Ernesto", pb.nuova(t, "7/26", "Elena", "Spilla", ""), 15)
	pb.fuoriDa(t, "Fabio", pb.nuova(t, "8/26", "Fiora", "Troppo recente", ""), 10)
	consegnata := pb.nuova(t, "9/26", "Gina", "Già consegnata", "2026-11-01")
	pb.esegui(t, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, consegnata)
	pb.cestino(t, pb.nuova(t, "10/26", "Hugo", "Nel cestino", "2026-11-01"))
	fuoriNelCestino := pb.nuova(t, "11/26", "Ivo", "Nel cestino e fuori", "")
	pb.fuoriDa(t, "Ernesto", fuoriNelCestino, 20)
	pb.cestino(t, fuoriNelCestino)
	prontaNelCestino := pb.nuova(t, "12/26", "Lia", "Pronta nel cestino", "")
	pb.pronta(t, prontaNelCestino, "2026-11-10")
	pb.cestino(t, prontaNelCestino)
	oroNelCestino := pb.nuova(t, "13/26", "Mia", "Oro nel cestino", "")
	pb.valutata(t, oroNelCestino)
	pb.cestino(t, oroNelCestino)
	pb.cestino(t, pb.nuova(t, "14/26", "Nino", "In scadenza nel cestino", "2026-11-18"))

	e := pb.aManoB(t)
	if e.Esito != EsitoOK || e.Avvisi != 5 || e.Messaggi != 1 || e.Dettaglio != "5 voci in 1 messaggio." {
		t.Errorf("esecuzione %+v", e)
	}
	m := pb.messaggi(t)
	if len(m) != 1 {
		t.Fatalf("messaggi %+v", m)
	}
	if m[0].canale != uscita.CanaleTelegram || m[0].parte != "shop" || m[0].indirizzo != chatFinta || m[0].oggetto != "Briefing 17/11" || m[0].busta.Valid {
		t.Errorf("riga %+v", m[0])
	}
	atteso := strings.Join([]string{
		"🌅 Briefing 17/11",
		"✅ Pronte da avvisare (1)\n• #1/26 Anna — Anello",
		"🪙 Compro oro da avvisare (1)\n• #3/26 Bruno — Oro rotto",
		"⚠ In ritardo (1)\n• #4/26 Carla — Catena (10/11/2026)",
		"📅 In scadenza 3gg (1)\n• #5/26 Dino — Orologio (19/11/2026)",
		"🛠 Fuori bottega da ≥14gg (1)\n• #7/26 Elena da Ernesto · 15gg",
	}, "\n\n")
	if m[0].testo != atteso {
		t.Errorf("testo\n%s\natteso\n%s", m[0].testo, atteso)
	}
	var formato sql.NullString
	if err := pb.d.Lettura.QueryRow(`SELECT formato FROM messaggi_in_uscita`).Scan(&formato); err != nil || formato.Valid {
		t.Errorf("formato %v %v", formato, err)
	}
}

// inventario: JOB-10
// Il numero delle righe è quello dei testi (D-64, D-82): «0045/26» per una busta con l'etichetta RIP-…, il numero
// interno per le altre, in tutte e cinque le sezioni.
func TestBriefingNumeroDeiTesti(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuovaConCodice(t, "1/26", "RIP-2026-0045", "Anna", "Anello", ""), "2026-11-10")
	pb.valutata(t, pb.nuovaConCodice(t, "2/26", "RIP-2026-0046", "Bruno", "Oro", ""))
	pb.nuovaConCodice(t, "3/26", "RIP-2026-0047", "Carla", "Catena", "2026-11-10")
	pb.nuovaConCodice(t, "4/26", "RIP-2026-0048", "Dino", "Orologio", "2026-11-18")
	pb.fuoriDa(t, "Ernesto", pb.nuovaConCodice(t, "5/26", "RIP-2026-0049", "Elena", "Spilla", ""), 20)
	pb.nuova(t, "6/26", "Fiora", "Senza etichetta", "2026-11-10")
	if e := pb.aManoB(t); e.Avvisi != 6 {
		t.Fatalf("%+v", e)
	}
	c := pb.testi(t)[0]
	for marca, attese := range map[string][]string{"✅": {"#0045/26"}, "🪙": {"#0046/26"}, "⚠": {"#0047/26", "#6/26"},
		"📅": {"#0048/26"}, "🛠": {"#0049/26"}} {
		if got := righeDi(c, marca); strings.Join(got, ",") != strings.Join(attese, ",") {
			t.Errorf("%s: %v", marca, got)
		}
	}
	if strings.Contains(c, "#1/26") || strings.Contains(c, "#5/26") {
		t.Errorf("numero interno nel testo:\n%s", c)
	}
}

// inventario: JOB-10
// Un negozio senza niente da dire riceve la riga tranquilla dello standalone, mandata lo stesso: «Nessuna voce:
// inviato il briefing vuoto in 1 messaggio.», esito ok. Anche buste che non sono in nessuna sezione non cambiano.
func TestBriefingNullaAllOrizzonte(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	e := pb.aManoB(t)
	if e.Esito != EsitoOK || e.Avvisi != 0 || e.Messaggi != 1 || e.Dettaglio != "Nessuna voce: inviato il briefing vuoto in 1 messaggio." {
		t.Errorf("esecuzione %+v", e)
	}
	if tt := pb.testi(t); len(tt) != 1 || tt[0] != vuoto {
		t.Errorf("testi %q", tt)
	}
	pb2 := nuovaProvaBriefing(t, nil)
	pb2.nuova(t, "1/26", "Anna", "Senza data", "")
	pb2.nuova(t, "2/26", "Bea", "Lontana", "2026-12-01")
	pb2.fuoriDa(t, "Ernesto", pb2.nuova(t, "3/26", "Cora", "Appena uscita", ""), 13)
	if r, err := pb2.lb.Esegui(context.Background(), Config{}, pb2.o.Adesso(), soloTelegram, titolare); err != nil || r.Avvisi != 0 || r.Messaggi != 1 {
		t.Errorf("%+v %v", r, err)
	}
	if tt := pb2.testi(t); len(tt) != 1 || tt[0] != vuoto {
		t.Errorf("testi %q", tt)
	}
}

// inventario: JOB-10
// Il giorno dell'intestazione e dell'oggetto è quello del negozio: le 23:30 UTC del 17 sono già il 18 a Roma.
func TestBriefingGiornoDelNegozio(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.nuova(t, "1/26", "Anna", "Scade il 21", "2026-11-21")
	pb.o.Imposta(time.Date(2026, 11, 17, 23, 30, 0, 0, time.UTC))
	if _, err := pb.lb.Esegui(context.Background(), Config{}, pb.o.Adesso(), soloTelegram, titolare); err != nil {
		t.Fatal(err)
	}
	m := pb.messaggi(t)
	// il 21 è «fra tre giorni» dal 18, non dal 17
	if len(m) != 1 || m[0].oggetto != "Briefing 18/11" || m[0].testo != "🌅 Briefing 18/11\n\n📅 In scadenza 3gg (1)\n• #1/26 Anna — Scade il 21 (21/11/2026)" {
		t.Errorf("messaggi %+v", m)
	}
}

// inventario: JOB-10
// L'ordine dentro le sezioni è quello dello standalone: pronte dalla data di pronta più vecchia, quelle senza data
// in fondo, a parità per id; compro oro dal più nuovo; ritardo e scadenza per data promessa e poi id; fuori
// bottega dall'uscita più vecchia, qualunque sia l'artigiano.
func TestBriefingOrdine(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuova(t, "1/26", "Prima", "Senza data", ""), "")
	pb.pronta(t, pb.nuova(t, "2/26", "Seconda", "Più vecchia", ""), "2026-11-01")
	pb.pronta(t, pb.nuova(t, "3/26", "Terza", "Più recente", ""), "2026-11-12")
	pb.pronta(t, pb.nuova(t, "4/26", "Quarta", "Stessa data, id dopo", ""), "2026-11-01")
	pb.valutata(t, pb.nuova(t, "5/26", "Quinta", "Valutata uno", ""))
	pb.valutata(t, pb.nuova(t, "6/26", "Sesta", "Valutata due", ""))
	pb.nuova(t, "7/26", "R1", "Ritardo recente", "2026-11-16")
	pb.nuova(t, "8/26", "R2", "Ritardo vecchio", "2026-11-02")
	pb.nuova(t, "9/26", "R3", "Ritardo vecchio, id dopo", "2026-11-02")
	pb.nuova(t, "10/26", "S1", "Scade dopo", "2026-11-20")
	pb.nuova(t, "11/26", "S2", "Scade oggi", "2026-11-17")
	pb.fuoriDa(t, "Alfa", pb.nuova(t, "12/26", "F1", "Uscita dopo", ""), 15)
	pb.fuoriDa(t, "Zeta", pb.nuova(t, "13/26", "F2", "Uscita prima", ""), 30)
	pb.fuoriDa(t, "Alfa", pb.nuova(t, "14/26", "F3", "In mezzo", ""), 20)
	pb.aManoB(t)
	c := pb.testi(t)[0]
	for marca, attese := range map[string]string{
		"✅": "#2/26,#4/26,#3/26,#1/26", "🪙": "#6/26,#5/26", "⚠": "#8/26,#9/26,#7/26", "📅": "#11/26,#10/26", "🛠": "#13/26,#14/26,#12/26",
	} {
		if got := strings.Join(righeDi(c, marca), ","); got != attese {
			t.Errorf("%s: %s, attese %s", marca, got, attese)
		}
	}
	if !strings.Contains(c, "• #13/26 F2 da Zeta · 30gg\n• #14/26 F3 da Alfa · 20gg") {
		t.Errorf("fuori bottega:\n%s", c)
	}
}

// inventario: JOB-10
// Le finestre e i filtri delle sezioni: oggi e fra tre giorni sono «in scadenza», fra quattro no, ieri è «in
// ritardo»; una pronta (o una valutata) con la data passata è solo fra le pronte; senza data mai in ritardo; una
// data di consegna toglie la busta da pronte e ritardi; gli stati chiusi mai in ritardo; in attesa di materiali e
// preventivo sì.
func TestBriefingFinestreEFiltri(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.nuova(t, "1/26", "Oggi", "Scade oggi", "2026-11-17")
	pb.nuova(t, "2/26", "Tre", "Fra tre giorni", "2026-11-20")
	pb.nuova(t, "3/26", "Quattro", "Fra quattro giorni", "2026-11-21")
	pb.nuova(t, "4/26", "Ieri", "Scaduta ieri", "2026-11-16")
	pb.pronta(t, pb.nuova(t, "5/26", "Pronta", "Pronta in ritardo", "2026-11-01"), "2026-11-10")
	pb.valutata(t, pb.nuova(t, "6/26", "Oro", "Valutata in ritardo", "2026-11-01"))
	pb.nuova(t, "7/26", "Senza", "Senza data", "")
	ritirata := pb.nuova(t, "8/26", "Ritirata", "Pronta ritirata", "")
	pb.pronta(t, ritirata, "2026-11-10")
	pb.esegui(t, `UPDATE buste SET data_consegna = '2026-11-12' WHERE id = ?`, ritirata)
	ritirataTardi := pb.nuova(t, "9/26", "Ritirata", "In ritardo ritirata", "2026-11-01")
	pb.esegui(t, `UPDATE buste SET data_consegna = '2026-11-12' WHERE id = ?`, ritirataTardi)
	oroRitirato := pb.nuova(t, "10/26", "Oro", "Valutata ritirata", "")
	pb.valutata(t, oroRitirato)
	pb.esegui(t, `UPDATE buste SET data_consegna = '2026-11-12' WHERE id = ?`, oroRitirato)
	for i, s := range []string{"annullata", "consegnata"} {
		id := pb.nuova(t, fmt.Sprintf("%d/26", 11+i), "Chiusa", s, "2026-11-01")
		pb.esegui(t, `UPDATE buste SET stato = ? WHERE id = ?`, s, id)
	}
	chiusa := pb.nuova(t, "13/26", "Chiusa", "Compro oro chiuso", "2026-11-01")
	pb.esegui(t, `UPDATE buste SET compro_oro = 1, stato = 'chiusa' WHERE id = ?`, chiusa)
	attesa := pb.nuova(t, "14/26", "Attesa", "In attesa di materiali", "2026-11-01")
	pb.esegui(t, `UPDATE buste SET stato = 'in_attesa_materiali' WHERE id = ?`, attesa)
	prev := pb.nuova(t, "15/26", "Preventivo", "Preventivo", "2026-11-01")
	pb.esegui(t, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, prev)
	pb.aManoB(t)
	c := pb.testi(t)[0]
	for marca, attese := range map[string]string{
		"✅": "#5/26", "🪙": "#6/26", "⚠": "#14/26,#15/26,#4/26", "📅": "#1/26,#2/26",
	} {
		if got := strings.Join(righeDi(c, marca), ","); got != attese {
			t.Errorf("%s: %s, attese %s", marca, got, attese)
		}
	}
	for _, n := range []string{"#3/26", "#7/26", "#8/26", "#9/26", "#10/26", "#11/26", "#12/26", "#13/26"} {
		if strings.Contains(c, n+" ") {
			t.Errorf("%s nel briefing:\n%s", n, c)
		}
	}
}

// inventario: JOB-10
// Le righe: descrizione tagliata a 60 unità UTF-16 con «…» dopo 57 grafemi (il conto e il taglio dello
// standalone non usano la stessa misura: 31 emoji sono 62 unità, si «tagliano» a 57 grafemi e restano tutte con
// «…»), 60 unità esatte restano intere, una descrizione vuota o di spazi non ha il trattino, un cliente vuoto è
// «—», gli spazi ai bordi del nome si tolgono.
func TestBriefingRighe(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	for i, d := range []string{strings.Repeat("a", 70), strings.Repeat("è", 61), strings.Repeat("😀", 31), strings.Repeat("😀", 30), "Anello"} {
		pb.pronta(t, pb.nuova(t, fmt.Sprintf("%d/26", i+1), "Cliente", d, ""), fmt.Sprintf("2026-11-%02d", i+1))
	}
	pb.esegui(t, `UPDATE buste SET descrizione = '   ' WHERE numero = '5/26'`)
	pb.pronta(t, pb.nuova(t, "6/26", "Anna", "Anello", ""), "2026-11-07")
	pb.esegui(t, `UPDATE buste SET nome_cliente = '' WHERE numero = '6/26'`)
	pb.pronta(t, pb.nuova(t, "7/26", "Bea", "Spilla", ""), "2026-11-08")
	pb.esegui(t, `UPDATE buste SET nome_cliente = '  Bea  ' WHERE numero = '7/26'`)
	pb.aManoB(t)
	righe := strings.Split(pb.testi(t)[0], "\n")
	attese := []string{
		"• #1/26 Cliente — " + strings.Repeat("a", 57) + "…",
		"• #2/26 Cliente — " + strings.Repeat("è", 57) + "…",
		"• #3/26 Cliente — " + strings.Repeat("😀", 31) + "…",
		"• #4/26 Cliente — " + strings.Repeat("😀", 30),
		"• #5/26 Cliente",
		"• #6/26 — — Anello",
		"• #7/26 Bea — Spilla",
	}
	if len(righe) != 10 || strings.Join(righe[3:], "\n") != strings.Join(attese, "\n") {
		t.Errorf("righe\n%s", strings.Join(righe, "\n"))
	}
}

// inventario: JOB-10
// Fuori bottega: la soglia è compresa (14 giorni esatti sì, 13 no), il numero è il pavimento dei giorni, segue la
// soglia del titolare (briefing_alert_fuori_bottega_giorni), 0 toglie la sezione, una busta rientrata non è fuori.
func TestBriefingFuoriBottega(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.fuoriDa(t, "Ernesto", pb.nuova(t, "1/26", "Anna", "A", ""), 14)
	pb.fuoriDa(t, "Fabio", pb.nuova(t, "2/26", "Bea", "B", ""), 13)
	pb.aManoB(t)
	c := pb.testi(t)[0]
	if !strings.Contains(c, "🛠 Fuori bottega da ≥14gg (1)\n• #1/26 Anna da Ernesto · 14gg") || strings.Contains(c, "#2/26") {
		t.Errorf("14 e 13:\n%s", c)
	}

	pb2 := nuovaProvaBriefing(t, nil)
	pb2.manda(t, "Ernesto", pb2.nuova(t, "1/26", "Anna", "A", ""), pb2.o.Adesso().Add(-(5*24*time.Hour + 23*time.Hour)))
	imposta(t, pb2.d, "briefing_alert_fuori_bottega_giorni", "3")
	pb2.aManoB(t)
	if c := pb2.testi(t)[0]; !strings.Contains(c, "🛠 Fuori bottega da ≥3gg (1)\n• #1/26 Anna da Ernesto · 5gg") {
		t.Errorf("soglia 3:\n%s", c)
	}

	pb3 := nuovaProvaBriefing(t, nil)
	pb3.fuoriDa(t, "Ernesto", pb3.nuova(t, "1/26", "Anna", "A", ""), 40)
	imposta(t, pb3.d, ChiaveGiorniBriefing, "0")
	if e := pb3.aManoB(t); e.Avvisi != 0 || pb3.testi(t)[0] != vuoto {
		t.Errorf("soglia 0: %+v %q", e, pb3.testi(t))
	}

	// soglia 1: una busta fuori da un giorno esatto c'è (b19)
	pb1 := nuovaProvaBriefing(t, nil)
	pb1.fuoriDa(t, "Ernesto", pb1.nuova(t, "1/26", "Anna", "A", ""), 1)
	imposta(t, pb1.d, "briefing_alert_fuori_bottega_giorni", "1")
	pb1.aManoB(t)
	if c := pb1.testi(t)[0]; c != "🌅 Briefing 17/11\n\n🛠 Fuori bottega da ≥1gg (1)\n• #1/26 Anna da Ernesto · 1gg" {
		t.Errorf("soglia 1:\n%s", c)
	}

	// 22 buste fuori: se ne mostrano 20, «(21+)» e «…e altre ≥1» (b15)
	pb5 := nuovaProvaBriefing(t, nil)
	for i := 1; i <= 22; i++ {
		pb5.fuoriDa(t, "Ernesto", pb5.nuova(t, fmt.Sprintf("%d/26", i), fmt.Sprintf("Cliente %d", i), "A", ""), 20)
	}
	e5 := pb5.aManoB(t)
	c5 := pb5.testi(t)[0]
	if e5.Avvisi != 20 || len(righeDi(c5, "🛠")) != 20 || !strings.Contains(c5, "🛠 Fuori bottega da ≥14gg (21+)\n") ||
		!strings.HasSuffix(c5, "\n• #20/26 Cliente 20 da Ernesto · 20gg\n…e altre ≥1") {
		t.Errorf("22 fuori: %+v\n%s", e5, c5)
	}

	pb4 := nuovaProvaBriefing(t, nil)
	id := pb4.nuova(t, "1/26", "Anna", "A", "")
	pb4.fuoriDa(t, "Ernesto", id, 40)
	pb4.esegui(t, `UPDATE buste SET rientrata_artigiano_il = ? WHERE id = ?`, pb4.adessoIstante(), id)
	pb4.aManoB(t)
	if c := pb4.testi(t)[0]; c != vuoto {
		t.Errorf("rientrata:\n%s", c)
	}
}

func (ps *provaSolleciti) adessoIstante() string {
	return ps.o.Adesso().UTC().Format("2006-01-02T15:04:05.000000Z")
}

// inventario: JOB-10
// La soglia del briefing (briefing_alert_fuori_bottega_giorni, la chiave del Flask che l'import porta): senza riga
// 14; un intero da 0 a 365 com'è (Integer.parse del valore senza spazi ai bordi); ogni altra cosa 14.
func TestBriefingSoglia(t *testing.T) {
	d, _ := apriDB(t)
	if c, err := LeggiConfig(context.Background(), d.Lettura); err != nil || c.GiorniBriefing() != 14 {
		t.Errorf("senza riga %d %v", c.GiorniBriefing(), err)
	}
	if (Config{}).GiorniBriefing() != 14 {
		t.Error("Config vuota")
	}
	for v, atteso := range map[string]int{"0": 0, "1": 1, "7": 7, " 21 ": 21, "+5": 5, "365": 365, "366": 14, "-1": 14, "boh": 14, "": 14,
		"3.5": 14, "1e2": 14, "99999999999999999999": 14, "-0": 0} {
		imposta(t, d, "briefing_alert_fuori_bottega_giorni", v)
		c, err := LeggiConfig(context.Background(), d.Lettura)
		if err != nil || c.GiorniBriefing() != atteso {
			t.Errorf("%q: %d %v, atteso %d", v, c.GiorniBriefing(), err, atteso)
		}
	}
}

// inventario: JOB-10
// Una sezione si ferma a 20 e lo dice: con 21 buste il titolo ha «(21+)», 20 righe e «…e altre ≥1» (l'aritmetica
// del Flask); con 20 esatte «(20)» e niente altro. Le voci contano le righe mostrate. Vale anche per fuori bottega.
func TestBriefingVentiEVentuno(t *testing.T) {
	for _, n := range []int{20, 21} {
		pb := nuovaProvaBriefing(t, nil)
		for i := 1; i <= n; i++ {
			pb.pronta(t, pb.nuova(t, fmt.Sprintf("%d/26", i), fmt.Sprintf("Cliente %d", i), "Anello", ""), "2026-11-10")
			pb.fuoriDa(t, "Ernesto", pb.nuova(t, fmt.Sprintf("%d/26", i+100), fmt.Sprintf("Cliente %d", i), "Anello", ""), 20)
		}
		e := pb.aManoB(t)
		c := pb.testi(t)[0]
		if e.Avvisi != 40 || len(righeDi(c, "✅")) != 20 || len(righeDi(c, "🛠")) != 20 {
			t.Errorf("%d: %+v\n%s", n, e, c)
		}
		if n == 20 {
			if !strings.Contains(c, "✅ Pronte da avvisare (20)\n") || !strings.Contains(c, "🛠 Fuori bottega da ≥14gg (20)\n") || strings.Contains(c, "…e altre") {
				t.Errorf("20:\n%s", c)
			}
			continue
		}
		if !strings.Contains(c, "✅ Pronte da avvisare (21+)\n") || !strings.Contains(c, "🛠 Fuori bottega da ≥14gg (21+)\n") ||
			strings.Count(c, "\n…e altre ≥1\n\n") != 1 || !strings.HasSuffix(c, "\n…e altre ≥1") || strings.Contains(c, "#21/26") || strings.Contains(c, "#121/26") {
			t.Errorf("21:\n%s", c)
		}
	}
}

// inventario: JOB-11
// Il conto di Telegram: unità UTF-16, un'emoji fuori dal BMP vale 2, una lettera accentata 1, come
// utf16.Encode; mai i byte né le rune.
func TestBriefingUnitaUTF16(t *testing.T) {
	for _, s := range []string{"", "abc", "è", "😀", "👨‍👩‍👧", "🌅 Briefing 17/11", "☕", "\xff"} {
		if unitaUTF16(s) != unita(s) {
			t.Errorf("%q: %d, attese %d", s, unitaUTF16(s), unita(s))
		}
	}
	if unitaUTF16("è😀") != 3 {
		t.Error("è😀")
	}
}

// riempi è prefisso più emoji e «è» fino a esattamente n unità UTF-16.
func riempi(t *testing.T, prefisso string, n int) string {
	t.Helper()
	resto := n - unita(prefisso)
	if resto < 0 {
		t.Fatalf("prefisso troppo lungo")
	}
	s := prefisso + strings.Repeat("😀", resto/2) + strings.Repeat("è", resto%2)
	if unita(s) != n {
		t.Fatalf("riempi %d", unita(s))
	}
	return s
}

// inventario: JOB-11
// La divisione fra sezioni è esatta: intestazione e blocco che fanno 4000 unità UTF-16 con «\n\n» stanno in un
// messaggio solo; una «è» in più e il blocco va in un secondo messaggio, intero. I blocchi che stanno non si
// tagliano, e nessuna parte supera 4000.
func TestBriefingDivisioneAlLimite(t *testing.T) {
	testa := "🌅 Briefing 17/11"
	titolo := "✅ Pronte da avvisare (1)"
	blocco := riempi(t, titolo+"\n• #1/26 ", MassimoMessaggio-unita(testa)-2)
	parti := dividi([]string{testa, blocco})
	if len(parti) != 1 || parti[0] != testa+"\n\n"+blocco || unita(parti[0]) != MassimoMessaggio {
		t.Errorf("4000: %d parti, %d unità", len(parti), unita(parti[0]))
	}
	piu := blocco + "è"
	parti = dividi([]string{testa, piu})
	if len(parti) != 2 || parti[0] != testa || parti[1] != piu {
		t.Errorf("4001: %d parti", len(parti))
	}
	// tre blocchi: i primi due stanno insieme, il terzo no
	a := riempi(t, "A\n", 1500)
	b := riempi(t, "B\n", 2498)
	c := "C\nx"
	parti = dividi([]string{a, b, c})
	if len(parti) != 2 || parti[0] != a+"\n\n"+b || parti[1] != c {
		t.Errorf("tre blocchi: %q", parti)
	}
	for _, p := range parti {
		if !utf8.ValidString(p) || unita(p) > MassimoMessaggio {
			t.Errorf("parte non valida (%d unità)", unita(p))
		}
	}
}

// inventario: JOB-11
// Una sezione che da sola supera il limite si divide fra le righe, mai dentro una: la prima parte arriva
// esattamente a 4000 unità quando le righe lo permettono, le parti dopo hanno il titolo con « (segue)», ogni riga
// compare una volta e nell'ordine.
func TestBriefingSezioneTroppoLunga(t *testing.T) {
	titolo := "⚠ In ritardo (3)"
	r1 := riempi(t, "• #1/26 ", 2000)
	r2 := riempi(t, "• #2/26 ", MassimoMessaggio-unita(titolo)-1-2000-1)
	r3 := "• #3/26 Anna — Anello (01/11/2026)"
	parti := dividi([]string{titolo + "\n" + r1 + "\n" + r2 + "\n" + r3})
	if len(parti) != 2 || parti[0] != titolo+"\n"+r1+"\n"+r2 || unita(parti[0]) != MassimoMessaggio ||
		parti[1] != titolo+" (segue)\n"+r3 {
		t.Errorf("al limite: %d parti %d unità", len(parti), unita(parti[0]))
	}
	r2b := r2 + "è"
	parti = dividi([]string{titolo + "\n" + r1 + "\n" + r2b + "\n" + r3})
	if len(parti) != 2 || parti[0] != titolo+"\n"+r1 || parti[1] != titolo+" (segue)\n"+r2b+"\n"+r3 {
		t.Errorf("oltre il limite: %q", parti)
	}
	// cinque righe da 1800 unità: tre parti; l'intestazione sta con la prima e la sezione dopo con l'ultima (pack/1
	// unisce i pezzi di fit/1 come blocchi qualunque)
	lunghe := []string{titolo}
	for i := 1; i <= 5; i++ {
		lunghe = append(lunghe, riempi(t, fmt.Sprintf("• #%d/26 ", i), 1800))
	}
	parti = dividi([]string{"🌅 Briefing 17/11", strings.Join(lunghe, "\n"), "📅 In scadenza 3gg (1)\n• #9/26 Dino"})
	if len(parti) != 3 || parti[0] != "🌅 Briefing 17/11\n\n"+titolo+"\n"+lunghe[1]+"\n"+lunghe[2] ||
		parti[1] != titolo+" (segue)\n"+lunghe[3]+"\n"+lunghe[4] ||
		parti[2] != titolo+" (segue)\n"+lunghe[5]+"\n\n📅 In scadenza 3gg (1)\n• #9/26 Dino" {
		t.Errorf("tre parti: %d", len(parti))
		for _, p := range parti {
			t.Logf("%d %.60q", unita(p), p)
		}
	}
	for _, p := range parti {
		if unita(p) > MassimoMessaggio {
			t.Errorf("parte di %d unità", unita(p))
		}
	}
}

// inventario: JOB-11
// Una riga più lunga di un messaggio (oltre 3900 unità, il margine per il titolo) si taglia con «…» fra due
// grafemi: mai una coppia surrogata spezzata né un'emoji composta divisa; il risultato sta in 3900 unità.
func TestBriefingRigaTagliata(t *testing.T) {
	titolo := "✅ Pronte da avvisare (1)"
	for _, c := range []struct {
		riga   string
		attesa string
	}{
		{"x" + strings.Repeat("😀", 3000), "x" + strings.Repeat("😀", 1949) + "…"},
		{strings.Repeat("😀", 3000), strings.Repeat("😀", 1949) + "…"},
		{strings.Repeat("è", 5000), strings.Repeat("è", 3899) + "…"},
		{strings.Repeat("👨‍👩‍👧", 700), strings.Repeat("👨‍👩‍👧", 487) + "…"},
	} {
		parti := dividi([]string{titolo + "\n" + c.riga})
		if len(parti) != 1 || parti[0] != titolo+"\n"+c.attesa {
			t.Errorf("%.20q: %d parti, %d unità", c.riga, len(parti), unita(parti[0]))
		}
		if !utf8.ValidString(parti[0]) || unita(c.attesa) > MassimoMessaggio-100 {
			t.Errorf("%.20q: non valida o lunga %d", c.riga, unita(c.attesa))
		}
	}
	// 3900 unità esatte non si tagliano (in un blocco che da solo supera il limite)
	riga := riempi(t, "• #1/26 ", 3900)
	altra := riempi(t, "• #2/26 ", 200)
	parti := dividi([]string{titolo + "\n" + riga + "\n" + altra})
	if len(parti) != 2 || parti[0] != titolo+"\n"+riga || parti[1] != titolo+" (segue)\n"+altra {
		t.Errorf("3900: %d parti", len(parti))
	}
	if parti := dividi([]string{titolo + "\n" + riga + "è\n" + altra}); len(parti) != 2 || !strings.HasSuffix(strings.Split(parti[0], "\n")[1], "…") {
		t.Errorf("3901: %q", parti)
	}
	// un blocco di 4000 unità esatte sta in un messaggio e non si tocca, anche con una riga oltre 3900 (clip/1 vale
	// solo per i blocchi che non stanno)
	lunga := riempi(t, "• #1/26 ", MassimoMessaggio-unita(titolo)-1)
	if parti := dividi([]string{titolo + "\n" + lunga}); len(parti) != 1 || parti[0] != titolo+"\n"+lunga {
		t.Errorf("blocco di 4000 con una riga lunga: %d parti", len(parti))
	}
}

// inventario: JOB-11
// Un nome di 5000 caratteri si taglia e il briefing parte lo stesso, con le altre righe intere; un nome di 3000
// emoji (6000 unità, 3000 grafemi) è lungo per Telegram: venti buste così fanno almeno venti messaggi, ognuno entro
// 4000 unità e con UTF-8 valido.
func TestBriefingNomiLunghi(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	pb.esegui(t, `UPDATE buste SET nome_cliente = ? WHERE numero = '1/26'`, strings.Repeat("x", 5000))
	pb.pronta(t, pb.nuova(t, "2/26", "Bea", "Anello", ""), "2026-11-11")
	e := pb.aManoB(t)
	if e.Esito != EsitoOK {
		t.Fatalf("%+v", e)
	}
	var righe []string
	for _, s := range pb.testi(t) {
		if unita(s) > MassimoMessaggio {
			t.Errorf("messaggio di %d unità", unita(s))
		}
		righe = append(righe, strings.Split(s, "\n")...)
	}
	var lunga []string
	for _, r := range righe {
		if strings.HasPrefix(r, "• #1/26 ") {
			lunga = append(lunga, r)
		}
	}
	if len(lunga) != 1 || !strings.HasSuffix(lunga[0], "…") || !strings.Contains(strings.Join(righe, "\n"), "\n• #2/26 Bea — Anello") {
		t.Errorf("righe %d %v", len(lunga), len(righe))
	}

	pb2 := nuovaProvaBriefing(t, nil)
	for i := 1; i <= 20; i++ {
		id := pb2.nuova(t, fmt.Sprintf("%d/26", i), "Cliente", "Anello", "")
		pb2.pronta(t, id, "2026-11-10")
		pb2.esegui(t, `UPDATE buste SET nome_cliente = ? WHERE id = ?`, strings.Repeat("😀", 3000), id)
	}
	e = pb2.aManoB(t)
	tt := pb2.testi(t)
	if e.Esito != EsitoOK || len(tt) < 20 || e.Messaggi != len(tt) || e.Avvisi != 20 {
		t.Errorf("%+v, %d messaggi", e, len(tt))
	}
	for _, s := range tt {
		if unita(s) > MassimoMessaggio || !utf8.ValidString(s) {
			t.Errorf("messaggio di %d unità", unita(s))
		}
	}
}

// inventario: JOB-11
// Tre sezioni piene (60 buste con descrizioni di 60 caratteri) sono più di un messaggio: si dividono fra le
// sezioni, l'intestazione è solo nel primo, ogni busta compare una volta, ogni titolo una volta, le parti sono
// accodate nell'ordine (id crescenti: pronte, compro oro, ritardo) e la frase conta i messaggi.
func TestBriefingPiuMessaggiInOrdine(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	lunga := strings.Repeat("d", 60)
	for n := 1; n <= 20; n++ {
		pb.pronta(t, pb.nuova(t, fmt.Sprintf("%d/26", n), fmt.Sprintf("Cliente %d", n), lunga, ""), "2026-11-10")
		pb.valutata(t, pb.nuova(t, fmt.Sprintf("%d/26", n+20), fmt.Sprintf("Cliente %d", n+20), lunga, ""))
		pb.nuova(t, fmt.Sprintf("%d/26", n+40), fmt.Sprintf("Cliente %d", n+40), lunga, "2026-11-10")
	}
	e := pb.aManoB(t)
	tt := pb.testi(t)
	if e.Esito != EsitoOK || e.Avvisi != 60 || len(tt) < 2 || e.Messaggi != len(tt) || e.Dettaglio != fmt.Sprintf("60 voci in %d messaggi.", len(tt)) {
		t.Fatalf("%+v, %d messaggi", e, len(tt))
	}
	if !strings.HasPrefix(tt[0], "🌅 Briefing 17/11\n\n✅ Pronte da avvisare (20)\n") {
		t.Errorf("primo %.80q", tt[0])
	}
	tutto := strings.Join(tt, "\n")
	if strings.Count(tutto, "🌅") != 1 {
		t.Error("intestazione ripetuta")
	}
	for n := 1; n <= 60; n++ {
		if strings.Count(tutto, fmt.Sprintf("• #%d/26 ", n)) != 1 {
			t.Errorf("busta %d", n)
		}
	}
	ultimo := -1
	for _, titolo := range []string{"✅ Pronte", "🪙 Compro oro", "⚠ In ritardo"} {
		if strings.Count(tutto, titolo) != 1 {
			t.Errorf("titolo %s", titolo)
		}
		if i := strings.Index(tutto, titolo); i <= ultimo {
			t.Errorf("%s fuori ordine", titolo)
		} else {
			ultimo = i
		}
	}
	for _, s := range tt {
		if unita(s) > MassimoMessaggio {
			t.Errorf("messaggio di %d unità", unita(s))
		}
	}
	for i, m := range pb.messaggi(t) {
		if m.oggetto != "Briefing 17/11" || m.canale != uscita.CanaleTelegram || m.testo != tt[i] {
			t.Errorf("riga %d %+v", i, m)
		}
	}
}

// inventario: JOB-11
// Una sezione sola più lunga del limite (20 nomi di 160 caratteri) si taglia fra le righe: la seconda parte comincia
// con «✅ Pronte da avvisare (20) (segue)», le 20 righe ci sono tutte, intere.
func TestBriefingSegue(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	nome, descr := strings.Repeat("n", 160), strings.Repeat("d", 60)
	for n := 1; n <= 20; n++ {
		id := pb.nuova(t, fmt.Sprintf("%d/26", n), "Cliente", descr, "")
		pb.pronta(t, id, "2026-11-10")
		pb.esegui(t, `UPDATE buste SET nome_cliente = ? WHERE id = ?`, nome, id)
	}
	pb.aManoB(t)
	tt := pb.testi(t)
	if len(tt) < 2 {
		t.Fatalf("%d messaggi", len(tt))
	}
	primo, secondo := strings.Split(tt[0], "\n"), strings.Split(tt[1], "\n")
	if primo[2] != "✅ Pronte da avvisare (20)" || secondo[0] != "✅ Pronte da avvisare (20) (segue)" {
		t.Errorf("titoli %q %q", primo[2], secondo[0])
	}
	n := 0
	for _, s := range tt {
		for _, r := range strings.Split(s, "\n") {
			if strings.HasPrefix(r, "• #") {
				n++
				if !strings.HasSuffix(r, descr) {
					t.Errorf("riga tagliata %.40q", r)
				}
			}
		}
	}
	if n != 20 {
		t.Errorf("%d righe", n)
	}
}

// rifiutaParte rifiuta con err la parte numero n (da 1) dell'esecuzione; le altre vanno alla coda vera.
type rifiutaParte struct {
	*uscita.Coda
	mu  sync.Mutex
	n   int
	err error
	vis int
}

func (p *rifiutaParte) AccodaIn(ctx context.Context, tx *sql.Tx, dest uscita.Destinazione, cont uscita.Contenuto, a audit.Contesto) (int64, error) {
	p.mu.Lock()
	p.vis++
	vis := p.vis
	p.mu.Unlock()
	if vis == p.n {
		return 0, p.err
	}
	return p.Coda.AccodaIn(ctx, tx, dest, cont, a)
}

// treParti sono buste che fanno un briefing di almeno tre messaggi.
func (pb *provaBriefing) treParti(t *testing.T) {
	t.Helper()
	nome := strings.Repeat("n", 300)
	for n := 1; n <= 20; n++ {
		id := pb.nuova(t, fmt.Sprintf("%d/26", n), "Cliente", "Anello", "")
		pb.pronta(t, id, "2026-11-10")
		pb.esegui(t, `UPDATE buste SET nome_cliente = ? WHERE id = ?`, nome, id)
		id = pb.nuova(t, fmt.Sprintf("%d/26", n+20), "Cliente", "Anello", "2026-11-10")
		pb.esegui(t, `UPDATE buste SET nome_cliente = ? WHERE id = ?`, nome, id)
	}
}

// inventario: JOB-11
// Tutto o niente: la coda che rifiuta la seconda parte annulla anche la prima già accodata; «Fallito» con il
// motivo dello standalone e zero conteggi; nel registro il motivo, mai la chat, i nomi o i numeri. Il giro dopo,
// con la coda a posto, manda tutte le parti. Anche il rifiuto vero della coda (una chat con uno spazio) è un
// fallimento senza righe.
func TestBriefingTuttoONiente(t *testing.T) {
	for _, c := range []struct {
		err    error
		motivo string
	}{
		{uscita.ErrMessaggioNonValido, "La coda ha rifiutato il briefing."},
		{uscita.ErrSoloAMano, "La coda ha rifiutato il briefing."},
		{fmt.Errorf("avvolto: %w", uscita.ErrNonConfigurato), "Il bot Telegram non è più configurato."},
	} {
		rif := &rifiutaParte{n: 2, err: c.err}
		pb := nuovaProvaBriefing(t, func(q *uscita.Coda) Accodatore { rif.Coda = q; return rif })
		var registro bytes.Buffer
		pb.lb.ConRegistro(slog.New(slog.NewTextHandler(&registro, nil)))
		pb.treParti(t)
		e := pb.giro(t)
		if e.Esito != EsitoFallito || e.Avvisi != 0 || e.Messaggi != 0 || e.Dettaglio != c.motivo {
			t.Errorf("%v: esecuzione %+v", c.err, e)
		}
		if len(pb.messaggi(t)) != 0 {
			t.Errorf("%v: parti rimaste in coda", c.err)
		}
		if r := registro.String(); !strings.Contains(r, "briefing: briefing non accodato") || !strings.Contains(r, c.motivo) ||
			strings.Contains(r, chatFinta) || strings.Contains(r, "nnnn") || strings.Contains(r, "1/26") || strings.Contains(r, "Anello") {
			t.Errorf("%v: registro %q", c.err, r)
		}
		rif.n = -1
		pb.o.Sposta(time.Minute)
		e = pb.giro(t)
		if tt := pb.testi(t); e.Esito != EsitoOK || e.Avvisi != 40 || len(tt) < 3 || e.Messaggi != len(tt) {
			t.Errorf("%v: giro dopo %+v (%d)", c.err, e, len(tt))
		}
	}

	// una chat con uno spazio (la pagina non la salva, un import sì; il pianificatore non la propone come destinatario,
	// quindi qui il lavoro si chiama da sé): la coda vera rifiuta la riga
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	storta := []uscita.Destinazione{{Canale: uscita.CanaleTelegram, Destinatario: "shop", Indirizzo: "-100 200"}}
	if r, err := pb.lb.Esegui(context.Background(), Config{}, pb.o.Adesso(), storta, titolare); err != nil || !r.Fallito ||
		r.Dettaglio != "La coda ha rifiutato il briefing." || r.Messaggi != 0 || r.Avvisi != 0 {
		t.Errorf("chat storta %+v %v", r, err)
	}
	if len(pb.messaggi(t)) != 0 {
		t.Error("messaggi con la chat storta")
	}
}

// inventario: JOB-10
// Un guasto che non è un rifiuto della coda (il database) annulla tutte le parti: «Il job si è interrotto».
func TestBriefingGuasto(t *testing.T) {
	rif := &rifiutaParte{n: 2, err: errors.New("disco pieno: chat -1004242")}
	pb := nuovaProvaBriefing(t, func(q *uscita.Coda) Accodatore { rif.Coda = q; return rif })
	pb.treParti(t)
	if e := pb.aManoB(t); e.Esito != EsitoFallito || e.Avvisi != 0 || e.Dettaglio != DettaglioInterrotto {
		t.Errorf("guasto %+v", e)
	}
	if len(pb.messaggi(t)) != 0 {
		t.Error("parti rimaste dopo il guasto")
	}
}

// inventario: JOB-10
// Solo Telegram: con email e Telegram accesi il briefing va solo al bot; con la sola email il pianificatore non lo
// esegue e non scrive righe (D-16, nello standalone «Saltato» «Nessun bot Telegram configurato.»), e a mano dà
// ErrSenzaCanali. Spento (il predefinito) non parte. Riuscito, non si ripete nello stesso giorno.
func TestBriefingSoloTelegram(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	accendiEmail(t, pb.d)
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	e := pb.giro(t)
	m := pb.messaggi(t)
	if e.Esito != EsitoOK || e.Messaggi != 1 || len(m) != 1 || m[0].canale != uscita.CanaleTelegram {
		t.Errorf("email e telegram: %+v %+v", e, m)
	}
	pb.o.Sposta(time.Hour)
	if es, err := pb.pb.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Errorf("secondo giro %v %+v", err, es)
	}

	pb2 := nuovaProvaBriefing(t, nil)
	accendiEmail(t, pb2.d)
	imposta(t, pb2.d, uscita.ChiaveTelegramAcceso, "0")
	pb2.pronta(t, pb2.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	if es, err := pb2.pb.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Errorf("solo email: %v %+v", err, es)
	}
	if _, err := pb2.pb.EseguiAMano(context.Background(), Briefing, titolare); !errors.Is(err, ErrSenzaCanali) {
		t.Errorf("a mano solo email: %v", err)
	}
	if righe(t, pb2.d) != 0 || len(pb2.messaggi(t)) != 0 {
		t.Error("solo email ha scritto")
	}

	pb3 := nuovaProvaBriefing(t, nil)
	imposta(t, pb3.d, "jobs.briefing_enabled", "0")
	if es, err := pb3.pb.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Errorf("spento: %v %+v", err, es)
	}
}

// inventario: JOB-10
// Senza destinatari il lavoro torna subito con ErrSenzaCanali e non accoda niente (il pianificatore non lo chiama
// mai così, D-16).
func TestBriefingSenzaDestinatari(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	if _, err := pb.lb.Esegui(context.Background(), Config{}, pb.o.Adesso(), nil, titolare); !errors.Is(err, ErrSenzaCanali) {
		t.Errorf("errore %v", err)
	}
	if len(pb.messaggi(t)) != 0 {
		t.Error("accodato senza destinatari")
	}
}

// inventario: JOB-10
// Nessun timbro: il briefing legge e basta; le colonne degli avvisi e aggiornato_il non cambiano, e il giorno dopo
// la stessa busta è ancora nel briefing.
func TestBriefingNessunTimbro(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", "2026-11-01"), "2026-11-10")
	pb.fuoriDa(t, "Ernesto", pb.nuova(t, "2/26", "Bea", "Spilla", "2026-11-01"), 20)
	foto := func() string {
		var s string
		if err := pb.d.Lettura.QueryRow(`SELECT group_concat(coalesce(ritardo_avvisato_il, '-') || coalesce(avviso_pronta_il, '-') ||
			coalesce(sollecito_avvisato_il, '-') || aggiornato_il, '|') FROM buste`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	prima := foto()
	pb.giro(t)
	if foto() != prima {
		t.Error("timbri cambiati")
	}
	pb.o.Sposta(24 * time.Hour)
	pb.giro(t)
	tt := pb.testi(t)
	if len(tt) != 2 || !strings.Contains(tt[1], "🌅 Briefing 18/11") || !strings.Contains(tt[1], "#1/26") || !strings.Contains(tt[1], "#2/26") {
		t.Errorf("giorno dopo %q", tt)
	}
}

// inventario: JOB-10
// Lo stop durante l'accodamento (anche con un errore che non è quello del contesto) o prima di cominciare: il
// lavoro torna con l'errore dello stop, niente riga, nessuna parte in coda; dopo il riavvio il giro dello stesso
// giorno è ancora dovuto e manda il briefing una volta.
func TestBriefingStop(t *testing.T) {
	ctx, ferma := context.WithCancel(context.Background())
	pb := nuovaProvaBriefing(t, func(q *uscita.Coda) Accodatore { return postaCheFerma{q, ferma} })
	pb.pronta(t, pb.nuova(t, "1/26", "Anna", "Anello", ""), "2026-11-10")
	// il briefing è l'ultimo lavoro del giro: il giro finisce senza riga (con o senza l'errore dello stop)
	es, err := pb.pb.Giro(ctx)
	if (err != nil && !errors.Is(err, context.Canceled)) || len(es) != 0 {
		t.Errorf("%v %+v", err, es)
	}
	if st, _ := pb.pb.Storico(context.Background(), 10); len(st) != 0 || len(pb.messaggi(t)) != 0 {
		t.Errorf("scritto dopo lo stop %+v", st)
	}
	p := Nuovo(pb.d, pb.o, pb.coda, map[string]Lavoro{Briefing: NuovoBriefing(pb.d, pb.coda)})
	pb.o.Sposta(time.Minute)
	es, err = p.Giro(context.Background())
	if err != nil || len(es) != 1 || es[0].Esito != EsitoOK || es[0].Messaggi != 1 || len(pb.messaggi(t)) != 1 {
		t.Fatalf("dopo il riavvio: %v %+v", err, es)
	}
	ctx2, ferma2 := context.WithCancel(context.Background())
	ferma2()
	pb2 := nuovaProvaBriefing(t, nil)
	if _, err := pb2.lb.Esegui(ctx2, Config{}, pb2.o.Adesso(), soloTelegram, titolare); !errors.Is(err, context.Canceled) {
		t.Errorf("stop prima: %v", err)
	}
	if len(pb2.messaggi(t)) != 0 {
		t.Error("accodato dopo lo stop prima")
	}
}

// inventario: JOB-10
// Il recupero della D-17: il lunedì (chiuso) il giro non manda niente; il martedì il briefing parte con lo stato di
// martedì (una busta diventata in ritardo nel frattempo c'è).
func TestBriefingRecuperoD17(t *testing.T) {
	pb := nuovaProvaBriefing(t, nil)
	pb.nuova(t, "1/26", "Anna", "Anello", "2026-11-16")
	pb.o.Imposta(time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC)) // lunedì 16/11, 09:30 in negozio
	if es, err := pb.pb.Giro(context.Background()); err != nil || len(es) != 0 {
		t.Fatalf("lunedì: %v %+v", err, es)
	}
	if len(pb.messaggi(t)) != 0 {
		t.Fatal("lunedì ha scritto")
	}
	pb.o.Imposta(martedi)
	if e := pb.giro(t); e.Esito != EsitoOK || e.Avvisi != 1 || e.Dettaglio != "1 voce in 1 messaggio." {
		t.Errorf("martedì %+v", e)
	}
	if tt := pb.testi(t); len(tt) != 1 || tt[0] != "🌅 Briefing 17/11\n\n⚠ In ritardo (1)\n• #1/26 Anna — Anello (16/11/2026)" {
		t.Errorf("testi %q", tt)
	}
}

// giro è un giro del pianificatore che deve eseguire il briefing.
func (pb *provaBriefing) giro(t *testing.T) Esecuzione {
	t.Helper()
	es, err := pb.pb.Giro(context.Background())
	if err != nil || len(es) != 1 {
		t.Fatalf("giro: %v %+v", err, es)
	}
	return es[0]
}
