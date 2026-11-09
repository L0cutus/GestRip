package buste

import (
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La conversazione (CLI-10…15, RIP-S-19, RIP-S-20), come repair_messages_test.exs,
// public_repair_thread_test.exs, public_repair_message_test.exs e notifications_test.exs dello
// standalone (go-baseline).

// rigaConv scrive una riga della conversazione all'istante dato.
func rigaConv(t *testing.T, b *Buste, id int64, autore string, canale any, testo, il string) {
	t.Helper()
	esegui(t, b.d, `INSERT INTO messaggi_buste (busta_id, autore, canale, testo, inviato_il, creato_il, aggiornato_il)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, id, autore, canale, testo, il, il, il)
}

func testi(ms []Messaggio) string {
	var s []string
	for _, m := range ms {
		s = append(s, m.Parte+":"+m.Lato()+":"+m.Testo)
	}
	return strings.Join(s, "|")
}

// inventario: CLI-10
func TestConversazionePubblica(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	altra := crea(t, b, modulo())
	// Stesso istante: conta l'id (l'ordine di scrittura).
	rigaConv(t, b, c.ID, "shop", nil, "Due", "2026-10-31T10:00:00.000000Z")
	rigaConv(t, b, c.ID, "customer", nil, "Uno", "2026-10-31T09:00:00.000000Z")
	rigaConv(t, b, c.ID, "customer", "whatsapp", "Tre", "2026-10-31T10:00:00.000000Z")
	rigaConv(t, b, altra.ID, "customer", nil, "Di un'altra busta", "2026-10-31T09:30:00.000000Z")
	p := pagina(t, b, c.Token)
	if !p.ConConversazione || testi(p.Conversazione) != "customer:cliente:Uno|shop:negozio:Due|customer:wa:Tre" {
		t.Errorf("conversazione: %v %q", p.ConConversazione, testi(p.Conversazione))
	}
	if p.Conversazione[0].Inviato != "2026-10-31T09:00:00.000000Z" || p.Conversazione[0].InviatoIl.Hour() != 9 {
		t.Errorf("istante: %+v", p.Conversazione[0])
	}
	// Dopo un cambio di controparte solo le righe da quel momento (compreso), e il lato è la nuova.
	esegui(t, d, `UPDATE buste SET messaggi_visibili_dal = '2026-10-31T10:00:00.000000Z', oreficeria_id = 2 WHERE id = ?`, c.ID)
	if p = pagina(t, b, c.Token); testi(p.Conversazione) != "shop:negozio:Due|oreficeria:wa:Tre" {
		t.Errorf("dal cambio: %q", testi(p.Conversazione))
	}
	// Vuota ma visibile; nascosta sulle buste chiuse.
	if p = pagina(t, b, altra.Token); !p.ConConversazione || len(p.Conversazione) != 1 {
		t.Errorf("altra: %+v", p.Conversazione)
	}
	vuota := crea(t, b, modulo())
	if p = pagina(t, b, vuota.Token); !p.ConConversazione || p.Conversazione != nil {
		t.Errorf("vuota: %+v", p)
	}
	for _, stato := range []string{"consegnata", "annullata", "chiusa"} {
		esegui(t, d, `UPDATE buste SET stato = ?, compro_oro = ? WHERE id = ?`, stato, stato == "chiusa", altra.ID)
		if p = pagina(t, b, altra.Token); p.ConConversazione || p.Conversazione != nil {
			t.Errorf("%s: %+v", stato, p)
		}
		if _, err := b.ConversazioneCliente(ctx, altra.Token); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("poll %s: %v", stato, err)
		}
	}
	for _, stato := range []string{"pronta", "preventivo", "in_attesa_materiali"} {
		esegui(t, d, `UPDATE buste SET stato = ?, compro_oro = 0 WHERE id = ?`, stato, altra.ID)
		if p = pagina(t, b, altra.Token); !p.ConConversazione {
			t.Errorf("%s nascosta", stato)
		}
	}
}

// inventario: CLI-11
func TestConversazioneDelPoll(t *testing.T) {
	b, d, o := prova(t)
	c := crea(t, b, modulo())
	rigaConv(t, b, c.ID, "shop", nil, "Pronta domani", "2026-10-31T10:00:00.000000Z")
	ms, err := b.ConversazioneCliente(ctx, c.Token)
	if err != nil || testi(ms) != "shop:negozio:Pronta domani" {
		t.Fatalf("poll: %q %v", testi(ms), err)
	}
	scaduta := crea(t, b, modulo())
	cestino := crea(t, b, modulo())
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	for nome, token := range map[string]string{"vuoto": "", "inventato": "x" + c.Token} {
		if _, err := b.ConversazioneCliente(ctx, token); !errors.Is(err, ErrBustaNonTrovata) {
			t.Errorf("%s: %v", nome, err)
		}
	}
	esegui(t, d, `UPDATE buste SET token_pubblico_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	if _, err := b.ConversazioneCliente(ctx, c.Token); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("revocato: %v", err)
	}
	esegui(t, d, `UPDATE buste SET token_pubblico_scade_il = ? WHERE id = ?`, o.Adesso().Format("2006-01-02T15:04:05.000000Z"), scaduta.ID)
	if _, err := b.ConversazioneCliente(ctx, scaduta.Token); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("scaduto alla scadenza esatta: %v", err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, cestino.ID)
	if _, err := b.ConversazioneCliente(ctx, cestino.Token); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("il poll ha scritto")
	}
}

// inventario: CLI-12
func TestMessaggioDelCliente(t *testing.T) {
	b, d, o := prova(t)
	c := crea(t, b, modulo())
	esegui(t, d, `UPDATE buste SET messaggi_letti_il = '2026-10-01T00:00:00.000000Z' WHERE id = ?`, c.ID)
	versione0, _ := b.Versione(ctx, c.ID)
	for nome, x := range map[string]error{"": ErrMessaggioVuoto, "  \n\t　": ErrMessaggioVuoto,
		strings.Repeat("é", 1001): ErrMessaggioTroppoLungo, " " + strings.Repeat("a", 1001) + " ": ErrMessaggioTroppoLungo} {
		if err := b.ScriviMessaggioCliente(ctx, c.Token, nome, pubblicoProva); !errors.Is(err, x) {
			t.Errorf("%.10q: %v", nome, err)
		}
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_buste`) != 0 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione LIKE 'repair.message%'`) != 0 {
		t.Fatal("scritto un rifiuto")
	}
	o.Sposta(time.Minute)
	testo := "  " + strings.Repeat("é", 999) + "<b>\n "
	if err := b.ScriviMessaggioCliente(ctx, c.Token, testo, pubblicoProva); err == nil || !errors.Is(err, ErrMessaggioTroppoLungo) {
		t.Fatalf("1002 caratteri: %v", err)
	}
	testo = " " + strings.Repeat("é", 997) + "<b>\n "
	if err := b.ScriviMessaggioCliente(ctx, c.Token, testo, pubblicoProva); err != nil {
		t.Fatalf("1000 caratteri esatti: %v", err)
	}
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	riga := valore(t, d, `SELECT autore || '|' || coalesce(canale, '-') || '|' || testo || '|' || inviato_il FROM messaggi_buste`).(string)
	if riga != "customer|-|"+strings.Repeat("é", 997)+"<b>|"+ora {
		t.Errorf("riga %.40q", riga)
	}
	// Non letto per il negozio: il segno tolto, l'ultimo messaggio adesso, la busta non cambia versione.
	if n := conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND ultimo_messaggio_il = ? AND messaggi_letti_il IS NULL`, c.ID, ora); n != 1 {
		t.Error("segni della busta")
	}
	if v, _ := b.Versione(ctx, c.ID); v != versione0 {
		t.Errorf("versione %d → %d", versione0, v)
	}
	dati := valore(t, d, `SELECT attore || '|' || ip || '|' || dati FROM eventi_audit WHERE azione = 'repair.message_appended'`).(string)
	mid := conta(t, d, `SELECT id FROM messaggi_buste`)
	if dati != `public|192.0.2.10|{"author":"customer","bag_number":"`+c.Numero+`","channel":null,"message_id":`+strconv.Itoa(mid)+`}` {
		t.Errorf("audit %q", dati)
	}
	// CLI-13 non c'è ancora: il mancato avviso, come lo standalone senza Telegram.
	if dati := valore(t, d, `SELECT dati FROM eventi_audit WHERE azione = 'repair.message_notice_not_queued'`).(string); dati != `{"bag_number":"`+c.Numero+`","reason":"not_configured"}` {
		t.Errorf("avviso mancato %q", dati)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE dati LIKE '%é%'`) != 0 {
		t.Error("il testo nel registro")
	}
	// Il negozio vede il non letto (filtro «notifiche» e campanella).
	if cc, err := b.LeggiCampanella(ctx); err != nil || cc.Messaggi != 1 || cc.Recenti[0].BustaID != c.ID {
		t.Errorf("campanella %+v %v", cc, err)
	}
	// Link morto o busta chiusa: niente scritto.
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "ciao", pubblicoProva); !errors.Is(err, ErrConversazioneChiusa) {
		t.Errorf("consegnata: %v", err)
	}
	esegui(t, d, `UPDATE buste SET stato = 'in_lavorazione', token_pubblico_revocato_il = ? WHERE id = ?`, istanteProva, c.ID)
	for _, tok := range []string{c.Token, "", "inventato"} {
		// Il link prima del testo (with_public_repair/3): anche vuoto o troppo lungo è non trovata.
		for _, testo := range []string{"ciao", "  ", strings.Repeat("a", MessaggioMassimo+1)} {
			if err := b.ScriviMessaggioCliente(ctx, tok, testo, pubblicoProva); !errors.Is(err, ErrBustaNonTrovata) {
				t.Errorf("%q, %d caratteri: %v", tok, len(testo), err)
			}
		}
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_buste`) != 1 {
		t.Error("scritto su un link morto o una busta chiusa")
	}
}

// inventario: CLI-12
func TestMessaggioDelClienteLimite(t *testing.T) {
	b, d, o := prova(t)
	c := crea(t, b, modulo())
	ins := func(autore string, canale any, fa time.Duration) {
		rigaConv(t, b, c.ID, autore, canale, "x", o.Adesso().Add(-fa).Format("2006-01-02T15:04:05.000000Z"))
	}
	// Fuori dalla finestra (un'ora e un microsecondo fa), del negozio, trascritte da WhatsApp: non contano.
	for i := 0; i < 10; i++ {
		ins("customer", nil, time.Hour+time.Microsecond)
		ins("shop", nil, time.Minute)
		ins("customer", "whatsapp", time.Minute)
	}
	// Nove dentro (una esattamente a un'ora fa) e la decima passa.
	ins("customer", nil, time.Hour)
	for i := 0; i < 8; i++ {
		ins("customer", nil, time.Minute)
	}
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "decimo", pubblicoProva); err != nil {
		t.Fatalf("decimo: %v", err)
	}
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "undicesimo", pubblicoProva); !errors.Is(err, ErrTroppiMessaggi) {
		t.Fatalf("undicesimo: %v", err)
	}
	// Il cambio di controparte fa ripartire il conteggio.
	esegui(t, d, `UPDATE buste SET messaggi_visibili_dal = ? WHERE id = ?`, o.Adesso().Format("2006-01-02T15:04:05.000000Z"), c.ID)
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "dopo il cambio", pubblicoProva); err != nil {
		t.Fatalf("dopo il cambio: %v", err)
	}
	// Dodici insieme su una busta nuova: ne passano dieci.
	n := crea(t, b, modulo())
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, troppi := 0, 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.ScriviMessaggioCliente(ctx, n.Token, "insieme", pubblicoProva)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrTroppiMessaggi):
				troppi++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if ok != 10 || troppi != 2 || conta(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ?`, n.ID) != 10 {
		t.Errorf("insieme: %d passati, %d rifiutati", ok, troppi)
	}
}

// inventario: CLI-14, RIP-S-20
func TestRispostaDelNegozio(t *testing.T) {
	b, d, o := prova(t)
	c := crea(t, b, modulo())
	chi := audit.Contesto{Attore: "titolare@esempio.it"}
	for testo, x := range map[string]error{"  \n": ErrMessaggioVuoto, strings.Repeat("a", 1001): ErrMessaggioTroppoLungo} {
		if err := b.RispondiAlCliente(ctx, c.ID, testo, chi); !errors.Is(err, x) {
			t.Errorf("%.10q: %v", testo, err)
		}
	}
	if err := b.RispondiAlCliente(ctx, 999, "ciao", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("busta che non c'è: %v", err)
	}
	o.Sposta(time.Minute)
	// Anche su una busta chiusa: resta nella conversazione del negozio, la pagina non la mostra.
	esegui(t, d, `UPDATE buste SET stato = 'consegnata' WHERE id = ?`, c.ID)
	if err := b.RispondiAlCliente(ctx, c.ID, " Pronta da lunedì ", chi); err != nil {
		t.Fatal(err)
	}
	ora := o.Adesso().Format("2006-01-02T15:04:05.000000Z")
	if n := conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND ultimo_messaggio_il = ? AND messaggi_letti_il = ?`, c.ID, ora, ora); n != 1 {
		t.Error("la risposta del negozio non è già letta")
	}
	if dati := valore(t, d, `SELECT attore || '|' || dati FROM eventi_audit WHERE azione = 'repair.message_appended'`).(string); !strings.HasPrefix(dati, `titolare@esempio.it|{"author":"shop","bag_number":"`+c.Numero+`","channel":null,"message_id":`) {
		t.Errorf("audit %q", dati)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.message_notice_not_queued'`) != 0 {
		t.Error("avviso mancato su una risposta del negozio")
	}
	s, err := b.Scheda(ctx, c.ID)
	if err != nil || testi(s.Messaggi) != "shop:negozio:Pronta da lunedì" {
		t.Errorf("scheda: %q %v", testi(s.Messaggi), err)
	}
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c.ID)
	if err := b.RispondiAlCliente(ctx, c.ID, "ciao", chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("cestino: %v", err)
	}
}

// inventario: RIP-S-19
func TestConversazioneDellaScheda(t *testing.T) {
	b, d, _ := prova(t)
	c := crea(t, b, modulo())
	rigaConv(t, b, c.ID, "customer", nil, "Prima del cambio", "2026-10-31T09:00:00.000000Z")
	rigaConv(t, b, c.ID, "shop", nil, "Dopo", "2026-10-31T11:00:00.000000Z")
	esegui(t, d, `UPDATE buste SET messaggi_visibili_dal = '2026-10-31T10:00:00.000000Z', oreficeria_id = 2,
		ultimo_messaggio_il = '2026-10-31T11:00:00.000000Z' WHERE id = ?`, c.ID)
	// La scheda vede tutto, anche dopo il cambio di controparte; il lato è quello di adesso.
	s, err := b.ApriScheda(ctx, strconv.FormatInt(c.ID, 10), audit.Contesto{Attore: "titolare@esempio.it"})
	if err != nil || testi(s.Messaggi) != "oreficeria:cliente:Prima del cambio|shop:negozio:Dopo" {
		t.Fatalf("scheda: %q %v", testi(s.Messaggi), err)
	}
	if s.Messaggi[0].Segno() != "" || s.Messaggi[1].Segno() != "🏪 " {
		t.Error("segni")
	}
	if conta(t, d, `SELECT count(*) FROM buste WHERE id = ? AND messaggi_letti_il = ultimo_messaggio_il`, c.ID) != 1 ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.messages_read'`) != 1 {
		t.Error("aprire la scheda non segna letto")
	}
	w := Messaggio{Autore: "customer", Canale: p("whatsapp")}
	if w.Lato() != "wa" || w.Segno() != "📲 " {
		t.Error("whatsapp")
	}
}

// inventario: CLI-15
func TestCampanella(t *testing.T) {
	b, d, _ := prova(t)
	vuota, err := b.LeggiCampanella(ctx)
	if err != nil || vuota.Totale != 0 || vuota.Recenti != nil {
		t.Fatalf("vuota: %+v %v", vuota, err)
	}
	var ids []int64
	for i := 0; i < 7; i++ {
		ids = append(ids, crea(t, b, modulo()).ID)
	}
	at := func(m int) string { return "2026-10-31T10:0" + strconv.Itoa(m) + ":00.000000Z" }
	// 0: messaggio non letto e approvazione non letta (conta due volte), consegnata (nessuna esclusione).
	esegui(t, d, `UPDATE buste SET stato = 'consegnata', ultimo_messaggio_il = ?, preventivo_accettato_il = ? WHERE id = ?`, at(1), at(2), ids[0])
	// 1: rifiuto non letto; 2: messaggio letto (non conta); 3: approvazione letta con un altro istante (conta).
	esegui(t, d, `UPDATE buste SET preventivo_rifiutato_il = ? WHERE id = ?`, at(3), ids[1])
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ?, messaggi_letti_il = ? WHERE id = ?`, at(4), at(4), ids[2])
	esegui(t, d, `UPDATE buste SET preventivo_accettato_il = ?, preventivo_accettato_letto_il = ? WHERE id = ?`, at(5), at(1), ids[3])
	// 4 e 5: messaggi non letti allo stesso istante (vince l'id più alto); 6: nel cestino.
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ? WHERE id IN (?, ?)`, at(6), ids[4], ids[5])
	esegui(t, d, `UPDATE buste SET ultimo_messaggio_il = ?, cancellata_il = ? WHERE id = ?`, at(9), istanteProva, ids[6])
	c, err := b.LeggiCampanella(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Messaggi != 3 || c.Approvati != 2 || c.Rifiutati != 1 || c.Totale != 6 {
		t.Errorf("conti %+v", c)
	}
	var visti []string
	for _, n := range c.Recenti {
		visti = append(visti, n.Tipo+strconv.Itoa(int(n.BustaID)))
	}
	atteso := []string{"message" + strconv.Itoa(int(ids[5])), "message" + strconv.Itoa(int(ids[4])), "approved" + strconv.Itoa(int(ids[3])),
		"rejected" + strconv.Itoa(int(ids[1])), "approved" + strconv.Itoa(int(ids[0]))}
	if strings.Join(visti, ",") != strings.Join(atteso, ",") {
		t.Errorf("recenti %v, attesi %v", visti, atteso)
	}
	if n := c.Recenti[0]; n.Nome == "" || !strings.HasPrefix(n.Numero, "N. ") && !strings.HasPrefix(n.Numero, "#") {
		t.Errorf("voce %+v", n)
	}
}
