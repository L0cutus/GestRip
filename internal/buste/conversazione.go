package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La conversazione della busta (CLI-10, CLI-11, CLI-12, CLI-14, RIP-S-19), come RepairMessages,
// PublicMessage e RepairMessage dello standalone (go-baseline):
//   - la conversazione pubblica (public_thread/2 con il link :token) è quella della busta dal più
//     vecchio (istante, poi id), con i soli messaggi scritti da messaggi_visibili_dal in poi; una
//     busta consegnata, annullata o chiusa non ne ha (nascosta);
//   - il lato di un messaggio (Recipient.message_party/2) si legge alla lettura: «shop» per il
//     negozio, altrimenti la controparte della busta («oreficeria» su una busta B2B, «customer»);
//   - il messaggio del cliente (PublicMessage.post/5): spazi Unicode ai bordi tolti, vuoto o oltre
//     1000 caratteri (code point) rifiutato; nella stessa transazione la busta riletta (chiusa =
//     ErrConversazioneChiusa), il limite di 10 messaggi web della controparte in un'ora (dal cambio
//     di controparte, se è dopo), la riga, i due istanti della busta (il segno «letto» tolto: è una
//     notifica per il negozio) e l'audit repair.message_appended senza il testo;
//   - l'avviso Telegram al negozio (CLI-13) non c'è ancora (T4.6): come lo standalone con Telegram
//     non configurato, il mancato avviso è una riga repair.message_notice_not_queued dopo il
//     commit, e il messaggio resta valido;
//   - la risposta del negozio dalla scheda (RepairMessages.reply/3): stesse regole del testo, la
//     riga è scritta già letta, anche su una busta chiusa (la pagina pubblica non la mostra).

// Messaggio è una riga della conversazione.
type Messaggio struct {
	ID        int64
	Autore    string  // "customer" o "shop"
	Canale    *string // "whatsapp" (trascritto dal negozio) o nil
	Testo     string
	InviatoIl time.Time
	Inviato   string // l'istante com'è salvato (db.FormatoIstante): il ts del poll
	Parte     string // "shop", "customer" o "oreficeria" (Recipient.message_party/2)
}

// Lato è thread_side/1: «negozio», «wa» o «cliente», la parola che sceglie l'icona.
func (m Messaggio) Lato() string {
	switch {
	case m.Autore == "shop":
		return "negozio"
	case m.Canale != nil && *m.Canale == "whatsapp":
		return "wa"
	}
	return "cliente"
}

// Segno è message_marker/1 della scheda: «🏪 », «📲 » o niente.
func (m Messaggio) Segno() string {
	switch m.Lato() {
	case "negozio":
		return "🏪 "
	case "wa":
		return "📲 "
	}
	return ""
}

var (
	ErrMessaggioVuoto       = errors.New("buste: messaggio vuoto")
	ErrMessaggioTroppoLungo = errors.New("buste: messaggio oltre i 1000 caratteri")
	ErrConversazioneChiusa  = errors.New("buste: la busta non riceve più messaggi")
	ErrTroppiMessaggi       = errors.New("buste: troppi messaggi nell'ultima ora")
)

const (
	// MessaggioMassimo è @max_body di RepairMessage, in code point.
	MessaggioMassimo = 1000
	// limiteMessaggi e finestraMessaggi sono @limit e @window_seconds di PublicMessage (Stefano,
	// 06/10/2026 nello standalone: «limite semplice», dieci all'ora per busta).
	limiteMessaggi   = 10
	finestraMessaggi = time.Hour
)

// statiSenzaConversazione sono @closed_statuses di RepairMessages.
var statiSenzaConversazione = []string{"consegnata", "annullata", "chiusa"}

// ConversazioneChiusa è RepairMessages.closed?/1.
func ConversazioneChiusa(stato string) bool { return slices.Contains(statiSenzaConversazione, stato) }

// testoMessaggio è il testo ripulito (String.trim/1) con il suo rifiuto: vuoto o troppo lungo
// (too_long?/1, code point).
func testoMessaggio(s string) (string, error) {
	s = strings.TrimFunc(s, unicode.IsSpace)
	switch {
	case s == "":
		return "", ErrMessaggioVuoto
	case utf8.RuneCountInString(s) > MessaggioMassimo:
		return "", ErrMessaggioTroppoLungo
	}
	return s, nil
}

// messaggiDella è list/1 con il lato di ogni riga: la conversazione della busta dal più vecchio,
// dal istante dal in poi (since?/2, compreso) se non è nil. parte è la controparte della busta.
func messaggiDella(ctx context.Context, q interrogatore, id int64, parte string, dal *string) ([]Messaggio, error) {
	query := `SELECT id, autore, canale, testo, inviato_il FROM messaggi_buste WHERE busta_id = ?`
	args := []any{id}
	if dal != nil {
		query += ` AND inviato_il >= ?`
		args = append(args, *dal)
	}
	righe, err := q.QueryContext(ctx, query+` ORDER BY inviato_il, id`, args...)
	if err != nil {
		return nil, err
	}
	defer righe.Close()
	var ms []Messaggio
	for righe.Next() {
		var m Messaggio
		var canale sql.NullString
		if err := righe.Scan(&m.ID, &m.Autore, &canale, &m.Testo, &m.Inviato); err != nil {
			return nil, err
		}
		m.Canale = nullaTesto(canale)
		if m.InviatoIl, err = time.Parse(db.FormatoIstante, m.Inviato); err != nil {
			return nil, err
		}
		m.Parte = parte
		if m.Autore == "shop" {
			m.Parte = "shop"
		}
		ms = append(ms, m)
	}
	return ms, righe.Err()
}

// parteDella è Recipient.party/1 dall'oreficeria della busta.
func parteDella(oreficeriaID *int64) string {
	return bustaDelToken{oreficeriaID: oreficeriaID}.parte()
}

// conversazionePubblica è public_thread/2 con il link :token: nil e false se la pagina non ha
// conversazione (busta chiusa).
func (b *Buste) conversazionePubblica(ctx context.Context, id int64, stato string, oreficeriaID *int64, dal *string) ([]Messaggio, bool, error) {
	if ConversazioneChiusa(stato) {
		return nil, false, nil
	}
	ms, err := messaggiDella(ctx, b.d.Lettura, id, parteDella(oreficeriaID), dal)
	if err != nil {
		return nil, false, err
	}
	return ms, true, nil
}

// ConversazioneCliente è quello che legge il poll della pagina (poll/2): la conversazione del link
// token. Link morto, busta nel cestino e conversazione nascosta sono un solo ErrBustaNonTrovata.
// Non scrive niente.
func (b *Buste) ConversazioneCliente(ctx context.Context, token string) ([]Messaggio, error) {
	if token == "" {
		return nil, ErrBustaNonTrovata
	}
	_, ora := b.adesso()
	var id int64
	var oref sql.NullInt64
	var stato string
	var dal sql.NullString
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, oreficeria_id, stato, messaggi_visibili_dal FROM buste
		WHERE hash_token_pubblico = ? AND token_pubblico_revocato_il IS NULL AND token_pubblico_scade_il > ?
		AND cancellata_il IS NULL`, ImprontaToken(token), ora).Scan(&id, &oref, &stato, &dal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBustaNonTrovata
	}
	if err != nil {
		return nil, err
	}
	ms, visibile, err := b.conversazionePubblica(ctx, id, stato, nullaIntero(oref), nullaTesto(dal))
	if err == nil && !visibile {
		return nil, ErrBustaNonTrovata
	}
	return ms, err
}

// ScriviMessaggioCliente è PublicMessage.post/5 dal link token: ErrMessaggioVuoto,
// ErrMessaggioTroppoLungo, ErrBustaNonTrovata (link morto, o busta sparita nel frattempo),
// ErrConversazioneChiusa o ErrTroppiMessaggi senza scrivere niente.
func (b *Buste) ScriviMessaggioCliente(ctx context.Context, token, testo string, c audit.Contesto) error {
	// Prima il link, poi il testo: come with_public_repair/3, un link morto è il 404 anche con un
	// testo vuoto o troppo lungo (review 063).
	t, err := b.bustaDelToken(ctx, token)
	if err != nil {
		return err
	}
	testo, err = testoMessaggio(testo)
	if err != nil {
		return err
	}
	adesso, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Riletta nella transazione: dalla pagina che ha scritto la busta può essere stata consegnata,
	// cestinata o passata a un'altra controparte.
	var numero, stato string
	var dal sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT numero, stato, messaggi_visibili_dal FROM buste WHERE id = ? AND cancellata_il IS NULL`,
		t.id).Scan(&numero, &stato, &dal)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrBustaNonTrovata
	case err != nil:
		return err
	case ConversazioneChiusa(stato):
		return ErrConversazioneChiusa
	}
	// under_limit/2: solo le righe web della controparte, dall'ora prima o dal cambio di controparte.
	dallOra := adesso.Add(-finestraMessaggi).Format(db.FormatoIstante)
	if dal.Valid && dal.String > dallOra {
		dallOra = dal.String
	}
	var recenti int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM messaggi_buste WHERE busta_id = ? AND autore = 'customer'
		AND canale IS NULL AND inviato_il >= ?`, t.id, dallOra).Scan(&recenti); err != nil {
		return err
	}
	if recenti >= limiteMessaggi {
		return ErrTroppiMessaggi
	}
	if _, err := b.aggiungiMessaggio(ctx, tx, t.id, numero, "customer", testo, ora, c); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// notify_shop/3 senza un canale (T4.6): il messaggio resta, il mancato avviso va nel registro.
	// record_miss/3: un registro che non scrive non tocca il messaggio, già salvato.
	_ = b.reg.Scrivi(ctx, b.d.Scrittura, c, "repair.message_notice_not_queued", "repair", t.id, "ok",
		map[string]any{"bag_number": numero, "reason": "not_configured"})
	return nil
}

// RispondiAlCliente è RepairMessages.reply/3 dalla scheda: la risposta del negozio, scritta già
// letta. ErrMessaggioVuoto, ErrMessaggioTroppoLungo o ErrBustaNonTrovata senza scrivere niente.
func (b *Buste) RispondiAlCliente(ctx context.Context, id int64, testo string, c audit.Contesto) error {
	testo, err := testoMessaggio(testo)
	if err != nil {
		return err
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var numero string
	err = tx.QueryRowContext(ctx, `SELECT numero FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&numero)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBustaNonTrovata
	}
	if err != nil {
		return err
	}
	if _, err := b.aggiungiMessaggio(ctx, tx, id, numero, "shop", testo, ora, c); err != nil {
		return err
	}
	return tx.Commit()
}

// aggiungiMessaggio è append/4 dentro la transazione del chiamante: la riga, i due istanti della
// busta (read_marker/1: il negozio scrive già letto, il cliente toglie il segno) e l'audit, senza
// il testo. L'ultima modifica della busta non cambia (update_all).
func (b *Buste) aggiungiMessaggio(ctx context.Context, tx *sql.Tx, id int64, numero, autore, testo, ora string, c audit.Contesto) (int64, error) {
	r, err := tx.ExecContext(ctx, `INSERT INTO messaggi_buste (busta_id, autore, canale, testo, inviato_il, creato_il, aggiornato_il)
		VALUES (?, ?, NULL, ?, ?, ?, ?)`, id, autore, testo, ora, ora, ora)
	if err != nil {
		return 0, err
	}
	mid, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	var letto any
	if autore == "shop" {
		letto = ora
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET ultimo_messaggio_il = ?, messaggi_letti_il = ? WHERE id = ?`, ora, letto, id); err != nil {
		return 0, err
	}
	return mid, b.reg.Scrivi(ctx, tx, c, "repair.message_appended", "repair", id, "ok", map[string]any{
		"bag_number": numero, "author": autore, "channel": nil, "message_id": mid})
}

// Notifica è una voce della campanella (Notifications.summary/0).
type Notifica struct {
	Tipo    string // "message", "approved" o "rejected"
	BustaID int64
	Nome    string
	Numero  string // il numero visibile (D-64): «#0045/26» o «N. 45/26»
	Il      time.Time
}

// Campanella è la campanella del negozio (CLI-15): i non letti per tipo, il totale (una busta può
// contare due volte, nessuna esclusione per stato, il cestino no) e le cinque notifiche più recenti.
type Campanella struct {
	Totale, Messaggi, Approvati, Rifiutati int64
	Recenti                                []Notifica
}

// Le condizioni e gli istanti dei tre tipi (unread_*_query/1).
var tipiCampanella = []struct{ tipo, condizione, istante string }{
	{"message", nonLettiSQL, "b.ultimo_messaggio_il"},
	{"approved", `b.preventivo_accettato_il IS NOT NULL AND (b.preventivo_accettato_letto_il IS NULL OR b.preventivo_accettato_letto_il != b.preventivo_accettato_il)`, "b.preventivo_accettato_il"},
	{"rejected", `b.preventivo_rifiutato_il IS NOT NULL AND (b.preventivo_rifiutato_letto_il IS NULL OR b.preventivo_rifiutato_letto_il != b.preventivo_rifiutato_il)`, "b.preventivo_rifiutato_il"},
}

const recentiCampanella = 5

// LeggiCampanella è Notifications.summary/0: solo letture.
func (b *Buste) LeggiCampanella(ctx context.Context) (Campanella, error) {
	var c Campanella
	conti := []*int64{&c.Messaggi, &c.Approvati, &c.Rifiutati}
	for i, k := range tipiCampanella {
		dove := ` FROM buste b WHERE b.cancellata_il IS NULL AND (` + k.condizione + `)`
		if err := b.d.Lettura.QueryRowContext(ctx, `SELECT count(*)`+dove).Scan(conti[i]); err != nil {
			return Campanella{}, err
		}
		righe, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.nome_cliente, b.numero, b.codice_qr, `+k.istante+dove+
			` ORDER BY `+k.istante+` DESC, b.id DESC LIMIT ?`, recentiCampanella)
		if err != nil {
			return Campanella{}, err
		}
		for righe.Next() {
			n := Notifica{Tipo: k.tipo}
			var nome, qr sql.NullString
			var numero, il string
			if err := righe.Scan(&n.BustaID, &nome, &numero, &qr, &il); err != nil {
				righe.Close()
				return Campanella{}, err
			}
			n.Nome = nome.String
			testo, etichetta := NumeroVisibile(qr.String, numero)
			if etichetta {
				testo = "#" + testo
			}
			n.Numero = testo
			if n.Il, err = time.Parse(db.FormatoIstante, il); err != nil {
				righe.Close()
				return Campanella{}, err
			}
			c.Recenti = append(c.Recenti, n)
		}
		if err := righe.Close(); err != nil {
			return Campanella{}, err
		}
		if err := righe.Err(); err != nil {
			return Campanella{}, err
		}
	}
	c.Totale = c.Messaggi + c.Approvati + c.Rifiutati
	// Dal più recente; a pari istante l'id più alto; a pari busta l'ordine dei tipi (sort_by stabile).
	sort.SliceStable(c.Recenti, func(i, j int) bool {
		a, z := c.Recenti[i], c.Recenti[j]
		if !a.Il.Equal(z.Il) {
			return a.Il.After(z.Il)
		}
		return a.BustaID > z.BustaID
	})
	if len(c.Recenti) > recentiCampanella {
		c.Recenti = c.Recenti[:recentiCampanella]
	}
	return c, nil
}
