package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Gli avvisi Telegram al cliente e al negozio (slice 074), come telegram_notices_test.exs,
// telegram_status_notice_test.exs, quote_decision_test.exs («an answer with no Telegram account…») e
// public_repair_message_test.exs (describe «the shop's notice») dello standalone (go-baseline). Chat fuori
// da ogni intervallo di Telegram, nomi e indirizzi inventati.

const (
	chatUno  = int64(4503599627370497)
	chatDue  = int64(4503599627370498)
	chatShop = "-1009999999999"
)

// provaAvvisi sono le buste con la coda di Telegram accesa, le notifiche ai clienti accese, l'indirizzo
// pubblico, l'indirizzo interno, il nome del negozio con dei segni da proteggere e la chat del negozio.
func provaAvvisi(t *testing.T) (*Buste, *db.DB, *providerTelegram) {
	t.Helper()
	b, d, _, p := provaTelegram(t)
	b.ConIndirizzoPubblico(baseEmail).ConIndirizzoInterno("https://interno.example.invalid:8443")
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('Bottega <Oro> & Co', '', ?, ?)`, istanteProva, istanteProva)
	esegui(t, d, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '123456:AAH-token-di-prova', ?)`, istanteProva)
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('outgoing.telegram.chat_id', ?, ?)`, chatShop, istanteProva)
	return b, d, p
}

// bustaTG è una busta del cliente diretto con un nome e una descrizione da proteggere.
func bustaTG(t *testing.T, b *Buste) Creata {
	t.Helper()
	return crea(t, b, con(modulo(), func(a *Accettazione) {
		a.NomeCliente, a.Descrizione = "Ada Maria <Rossi> & figli", "Ridurre <anello> & lucidare\nseconda riga"
	}))
}

// rigaTG è una riga Telegram della coda.
type rigaTG struct {
	busta                                    sql.NullInt64
	parte, indirizzo, testo, stato, tastiera string
	formato                                  sql.NullString
}

func righeTG(t *testing.T, d *db.DB) []rigaTG {
	t.Helper()
	rr, err := d.Lettura.Query(`SELECT busta_id, destinatario, indirizzo, testo, stato, formato, coalesce(tastiera, '') FROM messaggi_in_uscita
		WHERE canale = 'telegram' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Close()
	var out []rigaTG
	for rr.Next() {
		var r rigaTG
		if err := rr.Scan(&r.busta, &r.parte, &r.indirizzo, &r.testo, &r.stato, &r.formato, &r.tastiera); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

var reTastiera = regexp.MustCompile(`\A\{"inline_keyboard":\[\[\{"text":"📂 Apri scheda riparazione","url":"` + regexp.QuoteMeta(baseEmail) + `/r/([A-Za-z0-9_-]{43})"\}\]\]\}\z`)

// linkDelTasto è il token del link del pulsante, o il test si ferma.
func linkDelTasto(t *testing.T, r rigaTG) string {
	t.Helper()
	m := reTastiera.FindStringSubmatch(r.tastiera)
	if m == nil {
		t.Fatalf("tastiera inattesa: %q", r.tastiera)
	}
	return m[1]
}

func salvaTesto(t *testing.T, d *db.DB, f Famiglia, testo string) {
	t.Helper()
	esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore`, f.Chiave(), testo, istanteProva)
}

const corpoPronta = "Ciao <b>Ada</b>! 👋\n\n✨ La tua riparazione è ora <b>Pronta per il ritiro</b>\n\n📦 Busta: <b>1/26</b>\n" +
	"💍 <i>Ridurre &lt;anello&gt; &amp; lucidare</i>\n\n— Bottega &lt;Oro&gt; &amp; Co"

// inventario: CLI-47
// Un cambio dalla scheda prepara l'avviso per le chat iscritte adesso con il consenso (non per quella senza
// consenso né per quella fermata), con il testo dello standalone protetto e il pulsante, e non accoda niente.
func TestNotificaDelCambioDiStato(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	iscrivi(t, d, c.ID, chatDue, 0, nil)
	iscrivi(t, d, c.ID, 7, 1, istanteProva)
	cambio, err := b.CambiaStato(ctx, c.ID, "pronta", chi)
	if err != nil || cambio.TelegramNonPreparata != nil {
		t.Fatalf("cambio: %v %v", err, cambio.TelegramNonPreparata)
	}
	n := cambio.Telegram
	if n == nil {
		t.Fatal("nessun avviso preparato")
	}
	if n.BustaID != c.ID || n.Stato != "pronta" || n.Versione != 1 || n.Numero != "N. 1/26" || n.Pulsante != "📂 Apri scheda riparazione" ||
		n.Anteprima != corpoPronta || fmt.Sprint(n.chat) != fmt.Sprint([]int64{chatUno}) {
		t.Errorf("avviso %+v chat %v anteprima %q", *n, n.chat, n.Anteprima)
	}
	if len(righeTG(t, d)) != 0 {
		t.Error("il cambio ha accodato")
	}
}

// inventario: CLI-47
// Le chat si congelano prima del cambio: la consegna spegne l'iscrizione (trigger della 0014), ma
// l'avviso «Consegnata» arriva lo stesso alla sua chat.
func TestNotificaCongelaPrimaDellaConsegna(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	passa(t, b, c.ID, "pronta")
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	cambio, err := b.CambiaStato(ctx, c.ID, "consegnata", chi)
	if err != nil || cambio.Telegram == nil {
		t.Fatalf("cambio: %v %+v", err, cambio)
	}
	if attive(t, b, c.ID) != 0 {
		t.Fatal("la consegna non ha spento l'iscrizione")
	}
	if n, err := b.AutorizzaNotificaTelegram(ctx, cambio.Telegram, chi); err != nil || n != 1 {
		t.Fatalf("autorizza: %d %v", n, err)
	}
	rr := righeTG(t, d)
	if len(rr) != 1 || rr[0].indirizzo != strconv.FormatInt(chatUno, 10) || !strings.Contains(rr[0].testo, "🤝 La tua riparazione è ora <b>Consegnata</b>") ||
		rr[0].parte != "customer" || rr[0].stato != "queued" || rr[0].formato.String != "HTML" || rr[0].busta.Int64 != c.ID {
		t.Errorf("righe %+v", rr)
	}
	tok := linkDelTasto(t, rr[0])
	if valore(t, d, `SELECT token_pubblico FROM buste WHERE id = ?`, c.ID) != tok {
		t.Error("il pulsante non porta il link della busta")
	}
}

// inventario: CLI-47
// Niente avviso senza iscritti con il consenso, su una busta conto terzi, con le notifiche ai clienti spente,
// con il canale della coda spento o senza indirizzo pubblico; il cambio resta.
func TestNotificaNienteSenzaIscrittiOCanale(t *testing.T) {
	b, d, p := provaAvvisi(t)
	c := bustaTG(t, b)
	if cambio, err := b.CambiaStato(ctx, c.ID, "pronta", chi); err != nil || cambio.Telegram != nil || cambio.TelegramNonPreparata != nil {
		t.Fatalf("senza iscritti: %v %+v", err, cambio)
	}
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	casi := []struct {
		nome        string
		prima, dopo func()
	}{
		{"notifiche spente", func() {
			esegui(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
		},
			func() {
				esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
			}},
		{"canale spento", func() { p.spento = true }, func() { p.spento = false }},
		{"senza indirizzo pubblico", func() { b.ConIndirizzoPubblico("") }, func() { b.ConIndirizzoPubblico(baseEmail) }},
	}
	stati := []string{"in_lavorazione", "pronta", "in_lavorazione"}
	for i, k := range casi {
		k.prima()
		cambio, err := b.CambiaStato(ctx, c.ID, stati[i], chi)
		if err != nil || cambio.Telegram != nil || cambio.TelegramNonPreparata != nil {
			t.Errorf("%s: %v %+v", k.nome, err, cambio)
		}
		k.dopo()
	}
	oref := crea(t, b, modulo())
	iscrivi(t, d, oref.ID, chatDue, 1, nil)
	// La busta passa a un'oreficeria dopo l'iscrizione: il trigger la spegne, quindi la si riaccende a mano.
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, oref.ID)
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso = 1, fermata_il = NULL, motivo_fermata = NULL WHERE busta_id = ?`, oref.ID)
	if cambio, err := b.CambiaStato(ctx, oref.ID, "pronta", chi); err != nil || cambio.Telegram != nil {
		t.Errorf("conto terzi: %v %+v", err, cambio)
	}
	if len(righeTG(t, d)) != 0 {
		t.Error("righe accodate")
	}
}

// inventario: CLI-47
// Un testo salvato che non si compone: il cambio resta, nessun avviso, e il motivo dice famiglia e avviso.
func TestNotificaTestoCheNonSiCompone(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	salvaTesto(t, d, FamTGCambioStato, "Ciao {ignoto}")
	cambio, err := b.CambiaStato(ctx, c.ID, "pronta", chi)
	if err != nil || cambio.Telegram != nil {
		t.Fatalf("cambio: %v %+v", err, cambio)
	}
	var m *ModelloNonValido
	if !errors.As(cambio.TelegramNonPreparata, &m) || m.Famiglia != FamTGCambioStato || !strings.Contains(m.Avviso, "{ignoto}") {
		t.Errorf("motivo %v", cambio.TelegramNonPreparata)
	}
	if valore(t, d, `SELECT stato FROM buste WHERE id = ?`, c.ID) != "pronta" || len(righeTG(t, d)) != 0 {
		t.Error("il cambio non è rimasto o qualcosa è accodato")
	}
}

// inventario: RIP-H-18, CLI-47
// I cambi con il popup sono quelli della scheda e della lista (menu e «✓ Pronta»); il pannello del QR non
// prepara niente, e un cambio rifiutato nemmeno.
func TestNotificaSoloDaiCambiConIlPopup(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	lista, rapido, qr := bustaTG(t, b), bustaTG(t, b), bustaTG(t, b)
	for _, c := range []Creata{lista, rapido, qr} {
		iscrivi(t, d, c.ID, chatUno, 1, nil)
	}
	if cambio, err := b.CambiaStatoDallaLista(ctx, lista.ID, "preventivo", chi); err != nil || cambio.Telegram == nil ||
		!strings.Contains(cambio.Telegram.Anteprima, "📋 La tua riparazione è ora <b>Preventivo proposto</b>") {
		t.Errorf("lista: %v %+v", err, cambio)
	}
	if cambio, err := b.PassoRapido(ctx, rapido.ID, "pronta", chi); err != nil || cambio.Telegram == nil {
		t.Errorf("rapido: %v %+v", err, cambio)
	}
	if cambio, err := b.AvanzaDaQR(ctx, qr.ID, "in_lavorazione", chi); err != nil || cambio.Telegram != nil {
		t.Errorf("QR: %v %+v", err, cambio)
	}
	if cambio, err := b.CambiaStato(ctx, lista.ID, "consegnata", chi); !errors.Is(err, ErrTransizione) || cambio.Telegram != nil {
		t.Errorf("rifiutato: %v %+v", err, cambio)
	}
}

// inventario: CLI-47
// «Invia» è superato da un cambio successivo della busta, anche senza popup (il QR); un cambio che non tocca
// lo stato (il 📌) no. Un secondo popup del cambio dopo vale.
func TestAutorizzaSuperata(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	primo, _ := b.CambiaStato(ctx, c.ID, "pronta", chi)
	secondo, _ := b.CambiaStato(ctx, c.ID, "in_lavorazione", chi)
	if _, err := b.AutorizzaNotificaTelegram(ctx, primo.Telegram, chi); !errors.Is(err, ErrTelegramSuperata) {
		t.Errorf("primo: %v", err)
	}
	if len(righeTG(t, d)) != 0 {
		t.Fatal("accodato un avviso superato")
	}
	if n, err := b.AutorizzaNotificaTelegram(ctx, secondo.Telegram, chi); err != nil || n != 1 {
		t.Errorf("secondo: %d %v", n, err)
	}
	terzo, _ := b.CambiaStato(ctx, c.ID, "pronta", chi)
	if _, err := b.AvanzaDaQR(ctx, c.ID, "pronta", chi); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AutorizzaNotificaTelegram(ctx, terzo.Telegram, chi); !errors.Is(err, ErrTelegramSuperata) {
		t.Errorf("dopo il QR: %v", err)
	}
	c2 := bustaTG(t, b)
	iscrivi(t, d, c2.ID, chatUno, 1, nil)
	quarto, _ := b.CambiaStato(ctx, c2.ID, "pronta", chi)
	if _, err := b.CambiaFissata(ctx, c2.ID, chi); err != nil {
		t.Fatal(err)
	}
	if n, err := b.AutorizzaNotificaTelegram(ctx, quarto.Telegram, chi); err != nil || n != 1 {
		t.Errorf("dopo il 📌: %d %v", n, err)
	}
	if _, err := b.AutorizzaNotificaTelegram(ctx, nil, chi); !errors.Is(err, ErrTelegramSuperata) {
		t.Errorf("senza avviso: %v", err)
	}
}

// inventario: CLI-47
// «Invia» accoda il testo che il popup ha mostrato, anche se il testo salvato è cambiato dopo.
func TestAutorizzaIlTestoDelPopup(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	cambio, _ := b.CambiaStato(ctx, c.ID, "pronta", chi)
	salvaTesto(t, d, FamTGCambioStato, "Altro {numero}")
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio.Telegram, chi); err != nil {
		t.Fatal(err)
	}
	if rr := righeTG(t, d); len(rr) != 1 || rr[0].testo != corpoPronta {
		t.Errorf("righe %+v", rr)
	}
}

// inventario: CLI-47
// I rifiuti di «Invia», senza accodare niente: canale spento nel frattempo, busta passata a un'oreficeria,
// tutte le chat tagliate con il link; una chat tagliata resta fuori e una iscritta di nuovo rientra.
func TestAutorizzaRifiuti(t *testing.T) {
	b, d, p := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	cambio, _ := b.CambiaStato(ctx, c.ID, "pronta", chi)
	p.spento = true
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio.Telegram, chi); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("canale spento: %v", err)
	}
	p.spento = false
	esegui(t, d, `UPDATE impostazioni SET valore = '0' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio.Telegram, chi); !errors.Is(err, ErrTelegramSpento) {
		t.Errorf("notifiche spente: %v", err)
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio.Telegram, chi); !errors.Is(err, ErrTelegramNonCliente) {
		t.Errorf("conto terzi: %v", err)
	}
	if len(righeTG(t, d)) != 0 {
		t.Fatal("accodato dopo un rifiuto")
	}

	c2 := bustaTG(t, b)
	iscrivi(t, d, c2.ID, chatUno, 1, nil)
	iscrivi(t, d, c2.ID, chatDue, 1, nil)
	cambio2, _ := b.CambiaStato(ctx, c2.ID, "pronta", chi)
	if _, err := b.RevocaLink(ctx, c2.ID, chi); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso = 1, fermata_il = NULL, motivo_fermata = NULL WHERE busta_id = ? AND chat_id = ?`, c2.ID, chatDue)
	if n, err := b.AutorizzaNotificaTelegram(ctx, cambio2.Telegram, chi); err != nil || n != 1 {
		t.Fatalf("tagliata una: %d %v", n, err)
	}
	if rr := righeTG(t, d); len(rr) != 1 || rr[0].indirizzo != strconv.FormatInt(chatDue, 10) {
		t.Errorf("righe %+v", rr)
	}

	c3 := bustaTG(t, b)
	iscrivi(t, d, c3.ID, chatUno, 1, nil)
	cambio3, _ := b.CambiaStato(ctx, c3.ID, "pronta", chi)
	if _, err := b.NuovoLink(ctx, c3.ID, chi); err != nil {
		t.Fatal(err)
	}
	prima := conta(t, d, `SELECT count(*) FROM eventi_audit`)
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio3.Telegram, chi); !errors.Is(err, ErrTelegramLinkRevocato) {
		t.Errorf("tutte tagliate: %v", err)
	}
	if len(righeTG(t, d)) != 1 || conta(t, d, `SELECT count(*) FROM eventi_audit`) != prima {
		t.Error("il rifiuto ha scritto qualcosa")
	}

	c4 := bustaTG(t, b)
	iscrivi(t, d, c4.ID, chatUno, 1, nil)
	cambio4, _ := b.CambiaStato(ctx, c4.ID, "pronta", chi)
	esegui(t, d, `UPDATE buste SET cancellata_il = ? WHERE id = ?`, istanteProva, c4.ID)
	if _, err := b.AutorizzaNotificaTelegram(ctx, cambio4.Telegram, chi); !errors.Is(err, ErrBustaNonTrovata) {
		t.Errorf("nel cestino: %v", err)
	}
	if len(righeTG(t, d)) != 1 {
		t.Error("accodato su una busta nel cestino")
	}
}

// inventario: CLI-47
// Le chat congelate non escono dalla notifica: né %v, né %+v, né %#v le scrivono.
func TestNotificaNonScriveLeChat(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	cambio, _ := b.CambiaStato(ctx, c.ID, "pronta", chi)
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		s := fmt.Sprintf(f, cambio.Telegram)
		if strings.Contains(s, strconv.FormatInt(chatUno, 10)) || !strings.Contains(s, "pronta") {
			t.Errorf("%s: %q", f, s)
		}
	}
}

// inventario: CLI-47
// Tutti gli stati con l'emoji e il titolo dello standalone, uno sconosciuto con 🔔 e il nome con la
// maiuscola; la descrizione è la prima riga tagliata a 100 code point con «…».
func TestComponiCambioStato(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	x, err := leggiBustaNotifica(ctx, b.d.Lettura, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s, err := b.componiCambioStato(ctx, x, "pronta"); err != nil || s != corpoPronta {
		t.Errorf("pronta: %q %v", s, err)
	}
	salvaTesto(t, d, FamTGCambioStato, "{emoji}|{titolo_stato}|{stato}")
	attesi := map[string]string{
		"in_lavorazione": "🔧|In lavorazione|in lavorazione", "preventivo": "📋|Preventivo proposto|preventivo",
		"pronta": "✨|Pronta per il ritiro|pronta", "consegnata": "🤝|Consegnata|consegnata",
		"in_attesa_materiali": "⏳|In attesa materiali|in attesa materiali", "annullata": "❌|Annullata|annullata",
		"valutata": "⚖️|Valutazione completata|valutata", "in_valutazione": "🔎|In valutazione|in valutazione",
		"perfezionata": "💰|Pagata|perfezionata", "chiusa": "🔒|Chiusa|chiusa", "sconosciuto_DUE": "🔔|Sconosciuto due|sconosciuto DUE",
	}
	for stato, atteso := range attesi {
		if s, err := b.componiCambioStato(ctx, x, stato); err != nil || s != atteso {
			t.Errorf("%s: %q %v", stato, s, err)
		}
	}
	salvaTesto(t, d, FamTGCambioStato, "[{descrizione}]")
	x.descrizione = "  " + strings.Repeat("è", 101) + "\naltro"
	if s, _ := b.componiCambioStato(ctx, x, "pronta"); s != "["+strings.Repeat("è", 100)+"…]" {
		t.Errorf("descrizione lunga: %q", s)
	}
	x.descrizione = strings.Repeat("è", 60) // 60 code point di due byte: non si taglia
	if s, _ := b.componiCambioStato(ctx, x, "pronta"); s != "["+strings.Repeat("è", 60)+"]" {
		t.Errorf("descrizione di 60 «è»: %q", s)
	}
	x.descrizione = strings.Repeat("b", 100)
	if s, _ := b.componiCambioStato(ctx, x, "pronta"); s != "["+strings.Repeat("b", 100)+"]" {
		t.Errorf("descrizione di 100: %q", s)
	}
}

// inventario: CLI-47, CLI-48
// Un testo che nomina tutti i campi dichiarati delle due famiglie si compone, con ogni valore protetto.
func TestCampiTelegramDichiarati(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	for _, f := range FamiglieTelegram {
		var campi []string
		for _, k := range f.Campi() {
			campi = append(campi, "{"+k+"}")
		}
		salvaTesto(t, d, f, strings.Join(campi, "|"))
	}
	x, _ := leggiBustaNotifica(ctx, b.d.Lettura, c.ID)
	if s, err := b.componiCambioStato(ctx, x, "pronta"); err != nil ||
		s != "Ada|✨|Pronta per il ritiro|1/26|Ridurre &lt;anello&gt; &amp; lucidare|pronta|Bottega &lt;Oro&gt; &amp; Co" {
		t.Errorf("cambio stato: %q %v", s, err)
	}
	if s, err := b.componiNuovoMessaggio(ctx, x, " Ciao "); err != nil || s != "Ada|1/26|Ciao|Bottega &lt;Oro&gt; &amp; Co" {
		t.Errorf("nuovo messaggio: %q %v", s, err)
	}
}

// inventario: CLI-48
// Il nuovo messaggio del negozio: una riga HTML per chat iscritta con il consenso, valori protetti, lo
// stesso pulsante per tutte, il testo tagliato a 400 code point; il testo del titolare com'è.
func TestNuovoMessaggioTelegram(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	iscrivi(t, d, c.ID, chatDue, 1, nil)
	iscrivi(t, d, c.ID, 9, 0, nil)
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Pronto <domani> & dopodomani", chi); err != nil || n != 2 {
		t.Fatalf("accodate %d %v", n, err)
	}
	rr := righeTG(t, d)
	atteso := "Ciao <b>Ada</b>! 👋\n\n💬 Hai un nuovo messaggio sulla busta <b>1/26</b>:\n\n" +
		"<blockquote>Pronto &lt;domani&gt; &amp; dopodomani</blockquote>\n\n— Bottega &lt;Oro&gt; &amp; Co"
	if len(rr) != 2 || rr[0].indirizzo != strconv.FormatInt(chatUno, 10) || rr[1].indirizzo != strconv.FormatInt(chatDue, 10) {
		t.Fatalf("righe %+v", rr)
	}
	for _, r := range rr {
		if r.testo != atteso || r.parte != "customer" || r.stato != "queued" || r.formato.String != "HTML" || r.busta.Int64 != c.ID {
			t.Errorf("riga %+v", r)
		}
	}
	if linkDelTasto(t, rr[0]) != linkDelTasto(t, rr[1]) {
		t.Error("due link per lo stesso avviso")
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'outgoing.queued'`) != 2 {
		t.Error("audit outgoing.queued")
	}
	esegui(t, d, `DELETE FROM messaggi_in_uscita`)
	if _, err := b.NotificaNuovoMessaggio(ctx, c.ID, strings.Repeat("a", 401), chi); err != nil {
		t.Fatal(err)
	}
	if rr := righeTG(t, d); !strings.Contains(rr[0].testo, "<blockquote>"+strings.Repeat("a", 400)+"…</blockquote>") {
		t.Errorf("testo lungo %q", rr[0].testo)
	}
	esegui(t, d, `DELETE FROM messaggi_in_uscita`)
	if _, err := b.NotificaNuovoMessaggio(ctx, c.ID, strings.Repeat("a", 400), chi); err != nil {
		t.Fatal(err)
	}
	if rr := righeTG(t, d); !strings.Contains(rr[0].testo, "<blockquote>"+strings.Repeat("a", 400)+"</blockquote>") {
		t.Errorf("testo di 400 %q", rr[0].testo)
	}
	esegui(t, d, `DELETE FROM messaggi_in_uscita`)
	salvaTesto(t, d, FamTGNuovoMessaggio, "<i>{numero}</i>: {testo}")
	if _, err := b.NotificaNuovoMessaggio(ctx, c.ID, "a<b", chi); err != nil {
		t.Fatal(err)
	}
	if rr := righeTG(t, d); rr[0].testo != "<i>1/26</i>: a&lt;b" {
		t.Errorf("testo del titolare %q", rr[0].testo)
	}
}

// inventario: CLI-48
// Niente senza iscritti con il consenso, su una busta conto terzi, con il canale o le notifiche spente;
// un testo salvato che non si compone non accoda niente e lo dice.
func TestNuovoMessaggioNiente(t *testing.T) {
	b, d, p := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 0, nil)
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi); err != nil || n != 0 {
		t.Errorf("senza consenso: %d %v", n, err)
	}
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso = 1`)
	p.spento = true
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi); err != nil || n != 0 {
		t.Errorf("canale spento: %d %v", n, err)
	}
	p.spento = false
	esegui(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi); err != nil || n != 0 {
		t.Errorf("notifiche spente: %d %v", n, err)
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, uscita.ChiaveNotificheClienti)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso = 1, fermata_il = NULL WHERE busta_id = ?`, c.ID)
	if n, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi); err != nil || n != 0 {
		t.Errorf("conto terzi: %d %v", n, err)
	}
	esegui(t, d, `UPDATE buste SET oreficeria_id = NULL WHERE id = ?`, c.ID)
	esegui(t, d, `UPDATE iscrizioni_telegram SET consenso = 1, fermata_il = NULL WHERE busta_id = ?`, c.ID)
	salvaTesto(t, d, FamTGNuovoMessaggio, "Ciao {sconosciuto}")
	_, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi)
	var m *ModelloNonValido
	if !errors.As(err, &m) || m.Famiglia != FamTGNuovoMessaggio || !strings.Contains(m.Avviso, "{sconosciuto}") {
		t.Errorf("testo rotto: %v", err)
	}
	if len(righeTG(t, d)) != 0 {
		t.Error("righe accodate")
	}
}

// postaRifiutaTutto è una coda che dice il canale acceso ma rifiuta ogni riga, in tx o no.
type postaRifiutaTutto struct{ Posta }

func (postaRifiutaTutto) Accoda(context.Context, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	return 0, errors.New("coda rotta")
}
func (postaRifiutaTutto) AccodaIn(context.Context, *sql.Tx, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	return 0, errors.New("coda rotta")
}

// inventario: CLI-48
// Una riga che non si accoda lascia fuori tutto: nessuna riga, nessun link nuovo.
func TestNuovoMessaggioTuttoONiente(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	iscrivi(t, d, c.ID, chatUno, 1, nil)
	esegui(t, d, `UPDATE buste SET token_pubblico = NULL WHERE id = ?`, c.ID)
	b.ConPosta(postaRifiutaTutto{b.posta}, b.chiaveEmail)
	if _, err := b.NotificaNuovoMessaggio(ctx, c.ID, "Ciao", chi); err == nil {
		t.Fatal("accodato con la coda rotta")
	}
	if len(righeTG(t, d)) != 0 || valore(t, d, `SELECT token_pubblico FROM buste WHERE id = ?`, c.ID) != nil ||
		conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'repair.public_link_issued'`) != 0 {
		t.Error("rimasto qualcosa")
	}
}

// righeNegozio sono i testi delle righe Telegram della chat del negozio.
func righeNegozio(t *testing.T, d *db.DB) []string {
	t.Helper()
	var out []string
	for _, r := range righeTG(t, d) {
		if r.parte != "shop" || r.indirizzo != chatShop || r.busta.Valid || r.formato.Valid || r.tastiera != "" {
			t.Errorf("riga del negozio %+v", r)
		}
		out = append(out, r.testo)
	}
	return out
}

// inventario: CLI-08
// Dopo la risposta al preventivo un testo semplice alla chat del negozio: approvato con l'importo e il
// passaggio in lavorazione, rifiutato con il motivo o «Nessun motivo indicato.».
func TestAvvisoNegozioPreventivo(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	uno, due, tre := bustaTG(t, b), bustaTG(t, b), bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET stato = 'preventivo', preventivo_centesimi = 1250 WHERE id IN (?, ?)`, uno.ID, tre.ID)
	esegui(t, d, `UPDATE buste SET stato = 'preventivo', preventivo_testo = '70-90', preventivo_centesimi = 1 WHERE id = ?`, due.ID)
	if err := b.ApprovaPreventivo(ctx, uno.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	if err := b.RifiutaPreventivo(ctx, due.Token, "  troppo caro ", pubblico); err != nil {
		t.Fatal(err)
	}
	esegui(t, d, `UPDATE buste SET preventivo_centesimi = NULL WHERE id = ?`, tre.ID)
	quattro := bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET stato = 'preventivo', preventivo_testo = '', preventivo_centesimi = 9900 WHERE id = ?`, quattro.ID)
	if err := b.ApprovaPreventivo(ctx, quattro.Token, pubblico); err != nil {
		t.Fatal(err)
	}
	if err := b.RifiutaPreventivo(ctx, tre.Token, "", pubblico); err != nil {
		t.Fatal(err)
	}
	atteso := []string{
		"🤝 Preventivo APPROVATO — busta 1/26\nImporto: € 12,50\nLa busta è passata in lavorazione.",
		"🚫 Preventivo RIFIUTATO — busta 2/26\nImporto: 70-90\nMotivo: troppo caro",
		"🤝 Preventivo APPROVATO — busta 4/26\nImporto: € 99,00\nLa busta è passata in lavorazione.",
		"🚫 Preventivo RIFIUTATO — busta 3/26\nNessun motivo indicato.",
	}
	if got := righeNegozio(t, d); fmt.Sprint(got) != fmt.Sprint(atteso) {
		t.Errorf("avvisi %q", got)
	}
}

// inventario: CLI-08
// Senza Telegram (nessuna coda, configurazione mancante, canale spento) o con la coda che rifiuta, la
// risposta resta valida e niente è accodato.
func TestAvvisoNegozioPreventivoSenzaTelegram(t *testing.T) {
	b, d, p := provaAvvisi(t)
	posta := b.posta
	casi := []struct {
		nome        string
		prima, dopo func()
	}{
		{"senza coda", func() { b.posta = nil }, func() { b.posta = posta }},
		{"senza chat", func() { esegui(t, d, `UPDATE impostazioni SET valore = '' WHERE chiave = 'outgoing.telegram.chat_id'`) },
			func() {
				esegui(t, d, `UPDATE impostazioni SET valore = ? WHERE chiave = 'outgoing.telegram.chat_id'`, chatShop)
			}},
		{"canale spento", func() { p.spento = true }, func() { p.spento = false }},
		{"coda che rifiuta", func() { b.posta = postaRifiutaTutto{posta} }, func() { b.posta = posta }},
	}
	for _, k := range casi {
		c := bustaTG(t, b)
		esegui(t, d, `UPDATE buste SET stato = 'preventivo' WHERE id = ?`, c.ID)
		k.prima()
		err := b.ApprovaPreventivo(ctx, c.Token, pubblico)
		k.dopo()
		if err != nil || valore(t, d, `SELECT stato FROM buste WHERE id = ?`, c.ID) != "in_lavorazione" {
			t.Errorf("%s: %v", k.nome, err)
		}
	}
	if len(righeTG(t, d)) != 0 || conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneAvvisoNonAccodato) != 0 {
		t.Error("accodato o scritto")
	}
}

// inventario: CLI-13
// Il messaggio del cliente e dell'oreficeria avvisa la chat del negozio: chi ha scritto, la busta, i primi
// 400 grafemi e il link della scheda del Go sull'indirizzo interno; mai il nome del cliente. Niente riga di
// mancato avviso.
func TestAvvisoNegozioMessaggio(t *testing.T) {
	b, d, _ := provaAvvisi(t)
	c := bustaTG(t, b)
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "  Quando è pronta? ", pubblico); err != nil {
		t.Fatal(err)
	}
	lungo := strings.Repeat("é", 401) // «e» più l'accento: un grafema di due code point
	lungo = strings.ReplaceAll(lungo, "é", "é")
	if err := b.ScriviMessaggioCliente(ctx, c.Token, lungo, pubblico); err != nil { // 802 code point: sotto i 1000
		t.Fatal(err)
	}
	corto := strings.Repeat("\u0065\u0301", 300) // 600 code point ma 300 grafemi: niente taglio
	if err := b.ScriviMessaggioCliente(ctx, c.Token, corto, pubblico); err != nil {
		t.Fatal(err)
	}
	oref := bustaTG(t, b)
	esegui(t, d, `UPDATE buste SET oreficeria_id = 2 WHERE id = ?`, oref.ID)
	if err := b.ScriviMessaggioCliente(ctx, oref.Token, "Ciao", pubblico); err != nil {
		t.Fatal(err)
	}
	link := "\n\nApri la scheda: https://interno.example.invalid:8443/repairs/"
	atteso := []string{
		"💬 Messaggio dal cliente — busta 1/26\n\nQuando è pronta?" + link + itoa(c.ID),
		"💬 Messaggio dal cliente — busta 1/26\n\n" + strings.Repeat("é", 400) + "…" + link + itoa(c.ID),
		"💬 Messaggio dal cliente — busta 1/26\n\n" + corto + link + itoa(c.ID),
		"💬 Messaggio dall'orefice — busta 2/26\n\nCiao" + link + itoa(oref.ID),
	}
	got := righeNegozio(t, d)
	if fmt.Sprint(got) != fmt.Sprint(atteso) {
		t.Errorf("avvisi %q", got)
	}
	for _, s := range got {
		if strings.Contains(s, "Ada") || strings.Contains(s, "Rossi") {
			t.Errorf("il nome viaggia: %q", s)
		}
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneAvvisoNonAccodato) != 0 {
		t.Error("riga di mancato avviso con l'avviso partito")
	}
	b.ConIndirizzoInterno("")
	esegui(t, d, `DELETE FROM messaggi_in_uscita`)
	if err := b.ScriviMessaggioCliente(ctx, c.Token, "Ciao", pubblico); err != nil {
		t.Fatal(err)
	}
	if got := righeNegozio(t, d); len(got) != 1 || !strings.HasSuffix(got[0], "\n\nApri la scheda: /repairs/"+itoa(c.ID)) {
		t.Errorf("senza indirizzo interno %q", got)
	}
}

// inventario: CLI-13
// Senza Telegram il messaggio resta e il mancato avviso è repair.message_notice_not_queued not_configured;
// con la coda che rifiuta (o un panico) error. Mai il testo né il nome nella riga.
func TestAvvisoNegozioMessaggioMancato(t *testing.T) {
	b, d, p := provaAvvisi(t)
	c := bustaTG(t, b)
	posta := b.posta
	casi := []struct {
		nome, motivo string
		prima, dopo  func()
	}{
		{"canale spento", "not_configured", func() { p.spento = true }, func() { p.spento = false }},
		{"senza token", "not_configured", func() { esegui(t, d, `DELETE FROM segreti_provider`) }, func() {
			esegui(t, d, `INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('outgoing.telegram.bot_token', '123456:AAH-token-di-prova', ?)`, istanteProva)
		}},
		{"senza coda", "not_configured", func() { b.posta = nil }, func() { b.posta = posta }},
		{"coda che rifiuta", "error", func() { b.posta = postaRifiutaTutto{posta} }, func() { b.posta = posta }},
		{"coda in panico", "error", func() { b.posta = postaInPanico{posta} }, func() { b.posta = posta }},
	}
	for _, k := range casi {
		k.prima()
		err := b.ScriviMessaggioCliente(ctx, c.Token, "Messaggio segreto", pubblico)
		k.dopo()
		if err != nil {
			t.Fatalf("%s: %v", k.nome, err)
		}
		if v := valore(t, d, `SELECT attore || '|' || dati FROM eventi_audit WHERE azione = ? ORDER BY id DESC LIMIT 1`, azioneAvvisoNonAccodato); v !=
			`public|{"bag_number":"1/26","reason":"`+k.motivo+`"}` {
			t.Errorf("%s: %v", k.nome, v)
		}
	}
	if conta(t, d, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ?`, c.ID) != len(casi) || len(righeTG(t, d)) != 0 {
		t.Error("messaggi persi o avvisi accodati")
	}
}

// postaInPanico va in panico all'accodamento.
type postaInPanico struct{ Posta }

func (postaInPanico) Accoda(context.Context, uscita.Destinazione, uscita.Contenuto, audit.Contesto) (int64, error) {
	panic("coda in panico")
}
