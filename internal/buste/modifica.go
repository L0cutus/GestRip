package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La modifica della busta (RIP-S-04…11), come Workshop.update_repair/4 dello standalone
// (go-baseline) con Repair.update_changeset/2, kind_change/2, clear_expected_on/2,
// revoke_on_counterparty_change/4 e resolve_artisan_id/2. Tutto in una transazione, nell'ordine del
// Multi: la busta letta adesso (mai la copia di chi chiama), il cambio di tipo, l'oreficeria,
// l'artigiano, i campi, la revoca del link se cambia la controparte, la riga repair.updated. Un
// rifiuto o una riga d'audit che non si scrive non lasciano niente.
//
// I campi si confrontano con la busta come il cast di Ecto: un campo che non arriva non cambia; un
// testo vuoto o di soli spazi vale NULL; un valore uguale a quello salvato non è un cambio. Se non
// cambia niente la busta non si riscrive (aggiornato_il resta), ma la riga d'audit sì. Si scrivono
// solo le colonne cambiate, così i trigger delle iscrizioni Telegram non scattano per un valore
// rimasto uguale (come la UPDATE di Ecto, che nomina solo i cambi).

// ErrOreficeriaNonValida: l'oreficeria del modulo non è un'oreficeria esterna che esiste
// (resolve_external_oreficeria_id/2: :invalid_external_oreficeria). La pagina mostra il messaggio
// generico, come lo standalone.
var ErrOreficeriaNonValida = errors.New("buste: oreficeria non valida")

// I rifiuti del cambio di tipo, con i testi dello standalone (kind_refusal/1).
const (
	ErrTipoFuoriBottega Rifiuto = "Impossibile marcare come compro oro: la busta è fuori bottega presso un artigiano. Registrare il rientro prima."
	ErrTipoChiusa       Rifiuto = "Per cambiare il tipo di una busta chiusa riaprila prima: riportarla al lavoro richiede un motivo."
)

// I messaggi dei campi della modifica: quelli di Repair.update_changeset/2 e i predefiniti di Ecto.
const (
	MsgGiorniInPiu   = "Giorni in più: da 0 a 90."
	MsgNomeLungo     = "should be at most 160 character(s)"
	MsgTelefonoLungo = "should be at most 40 character(s)"
	MsgCentesimi     = "must be greater than or equal to 0"
)

const giorniInPiuMassimi = 90

// Modifica sono i campi del modulo «Modifica riparazione» (@edit_fields di RepairLive) come li
// manda il modulo; nil vuol dire campo non arrivato, che non cambia. Stato, numero, date di pronta e
// consegna, cliente, tipo e gettone non ci sono: non si impostano da qui. I centesimi li scrive
// solo il codice, mai il modulo (edit_attrs/1 li scarta): una scrittura dei soli centesimi toglie
// il testo del prezzo.
type Modifica struct {
	NomeCliente, TelefonoCliente         *string // le copie sulla busta
	DataAccettazione                     *string // AAAA-MM-GG
	Descrizione                          *string
	NotePubbliche, NoteInterne           *string
	Preventivo, PrezzoFinale             *string // testo libero
	DataPrevista                         *string // AAAA-MM-GG o vuota
	DataPrevistaTesto                    *string // il testo importato, che il modulo rimanda nascosto
	GiorniInPiu                          *string // da 0 a 90
	CodiceQR                             *string
	OreficeriaID, ArtigianoID            *string // vuoto: nessuna (diretta), nessuno
	PreventivoCentesimi, PrezzoCentesimi *int64
}

// Modificata è l'esito di una modifica riuscita: se il tipo è cambiato, e lo stato prima e dopo.
type Modificata struct {
	TipoCambiato          bool
	StatoPrima, StatoDopo string
}

// rigaBusta sono le colonne della busta che la modifica legge e confronta.
type rigaBusta struct {
	numero, stato                  string
	oro, fuori                     bool
	accettazione, descrizione      string
	nome, telefono                 string
	prevista, previstaTesto        *string
	notePubbliche, noteInterne, qr *string
	preventivoTesto, prezzoTesto   *string
	preventivoCent, prezzoCent     *int64
	giorni                         int64
	oreficeria, artigiano          *int64
	ritardo, revocato              *string
}

func (b *Buste) leggiPerModifica(ctx context.Context, tx *sql.Tx, id int64) (rigaBusta, error) {
	var r rigaBusta
	var prevista, previstaTesto, notePub, noteInt, qr, prevTesto, prezzoTesto, ritardo, revocato sql.NullString
	var prevCent, prezzoCent, oreficeria, artigiano sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT numero, stato, compro_oro, consegnata_artigiano_il IS NOT NULL AND rientrata_artigiano_il IS NULL,
		data_accettazione, descrizione, nome_cliente, telefono_cliente, data_prevista, data_prevista_testo, note_pubbliche,
		note_interne, codice_qr, preventivo_testo, prezzo_testo, preventivo_centesimi, prezzo_centesimi, giorni_in_piu,
		oreficeria_id, artigiano_id, ritardo_avvisato_il, token_pubblico_revocato_il
		FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&r.numero, &r.stato, &r.oro, &r.fuori, &r.accettazione,
		&r.descrizione, &r.nome, &r.telefono, &prevista, &previstaTesto, &notePub, &noteInt, &qr, &prevTesto, &prezzoTesto,
		&prevCent, &prezzoCent, &r.giorni, &oreficeria, &artigiano, &ritardo, &revocato)
	if errors.Is(err, sql.ErrNoRows) {
		return rigaBusta{}, ErrBustaNonTrovata
	}
	if err != nil {
		return rigaBusta{}, err
	}
	r.prevista, r.previstaTesto = nullaTesto(prevista), nullaTesto(previstaTesto)
	r.notePubbliche, r.noteInterne, r.qr = nullaTesto(notePub), nullaTesto(noteInt), nullaTesto(qr)
	r.preventivoTesto, r.prezzoTesto = nullaTesto(prevTesto), nullaTesto(prezzoTesto)
	r.preventivoCent, r.prezzoCent = nullaIntero(prevCent), nullaIntero(prezzoCent)
	r.oreficeria, r.artigiano = nullaIntero(oreficeria), nullaIntero(artigiano)
	r.ritardo, r.revocato = nullaTesto(ritardo), nullaTesto(revocato)
	return r, nil
}

// cambi sono le colonne da scrivere, con il valore nuovo (nil per NULL).
type cambi map[string]any

// metti segna la colonna solo se il valore nuovo è diverso da quello salvato (put_change/3).
func (c cambi) metti(colonna string, nuovo, salvato any) {
	if !stessoValore(nuovo, salvato) {
		c[colonna] = nuovo
	}
}

func stessoValore(a, b any) bool { return valoreDi(a) == valoreDi(b) }

// valoreDi dà il valore di un *string, *int64, string o int64 come any confrontabile (nil per NULL).
func valoreDi(v any) any {
	switch x := v.(type) {
	case *string:
		if x == nil {
			return nil
		}
		return *x
	case *int64:
		if x == nil {
			return nil
		}
		return *x
	}
	return v
}

// testoNullo è il testo del modulo come lo vede il cast di Ecto: nil se vuoto o di soli spazi.
func testoNullo(s string) *string {
	if vuoto(s) {
		return nil
	}
	return &s
}

// Modifica salva la modifica m della busta id per chi la fa (c). compraOro è il gesto sulla casella
// «🪙 Scheda Compro Oro»: nil se la casella non è stata toccata (una pagina rimasta aperta non
// annulla un cambio fatto altrove). I rifiuti, nell'ordine dello standalone: ErrBustaNonTrovata
// (anche nel cestino); ErrTipoFuoriBottega, ErrTipoChiusa; ErrOreficeriaNonValida; ErrArtigiano;
// Campi, tutti insieme. Ogni altro errore è del database.
func (b *Buste) Modifica(ctx context.Context, id int64, m Modifica, compraOro *bool, c audit.Contesto) (Modificata, error) {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Modificata{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	prima, err := b.leggiPerModifica(ctx, tx, id)
	if err != nil {
		return Modificata{}, err
	}
	esito := Modificata{StatoPrima: prima.stato, StatoDopo: prima.stato}
	if compraOro != nil {
		if esito, err = b.cambiaTipo(ctx, tx, id, prima, *compraOro, c, ora); err != nil {
			return Modificata{}, err
		}
	}
	oreficeria := any(valoreDi(prima.oreficeria))
	if m.OreficeriaID != nil {
		if oreficeria, err = relazione(ctx, tx, *m.OreficeriaID, `SELECT count(*) FROM oreficerie WHERE id = ? AND id > 1`, ErrOreficeriaNonValida); err != nil {
			return Modificata{}, err
		}
	}
	artigiano := any(valoreDi(prima.artigiano))
	if m.ArtigianoID != nil {
		if artigiano, err = relazione(ctx, tx, *m.ArtigianoID, `SELECT count(*) FROM artigiani WHERE id = ?`, ErrArtigiano); err != nil {
			return Modificata{}, err
		}
	}
	cc, campi := campiDellaModifica(prima, m, b2b(oreficeria))
	if len(campi) > 0 {
		return Modificata{}, campi
	}
	cc.metti("oreficeria_id", oreficeria, prima.oreficeria)
	cc.metti("artigiano_id", artigiano, prima.artigiano)
	if len(cc) > 0 {
		cc["aggiornato_il"] = ora
		if err := aggiorna(ctx, tx, id, cc); err != nil {
			return Modificata{}, err
		}
	}
	if controparte(oreficeria) != controparte(valoreDi(prima.oreficeria)) {
		if err := b.revocaPerControparte(ctx, tx, id, prima.revocato != nil, c, ora); err != nil {
			return Modificata{}, err
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.updated", "repair", id, "ok", datiModifica(prima, cc, oreficeria, artigiano)); err != nil {
		return Modificata{}, err
	}
	if err := tx.Commit(); err != nil {
		return Modificata{}, err
	}
	return esito, nil
}

// TogliData è clear_expected_on/2 («Togli data»): una modifica che svuota data e testo della
// consegna prevista, con la stessa transazione e la stessa riga d'audit, che dice quale scadenza
// era.
func (b *Buste) TogliData(ctx context.Context, id int64, c audit.Contesto) error {
	vuota := ""
	_, err := b.Modifica(ctx, id, Modifica{DataPrevista: &vuota, DataPrevistaTesto: &vuota}, nil, c)
	return err
}

// Scheda legge la busta id come la scheda, senza registrare l'apertura né segnare niente letto.
// ErrBustaNonTrovata come ApriScheda. È la sola lettura della scheda senza repair.viewed, e va
// tenuta stretta (D-44 estesa, slice 037b): la chiamano solo i gestori della scheda di internal/web
// che mostrano la busta con il segno monouso del server: dopo un'azione (mostraOApri) o ridisegnata
// dopo un rifiuto mandato con il segno della pagina (leggiDopoRifiuto); o che ne leggono un campo
// prima di un'azione (cambiaStato, proprietarioDellaBusta). Ogni altra pagina che mostra una busta passa da
// ApriScheda. L'elenco lo controlla TestLetturaSenzaAuditSoloDaiChiamantiNoti.
func (b *Buste) Scheda(ctx context.Context, id int64) (Scheda, error) {
	if id < 1 {
		return Scheda{}, ErrBustaNonTrovata
	}
	return b.leggiScheda(ctx, id)
}

// campiDellaModifica confronta i campi del modulo con la busta come Repair.update_changeset/2: dà le
// colonne cambiate e gli errori. Nome e telefono come cast/3 più update_change/3 (misurato con
// l'Ecto 3.14.2 dello standalone): il valore scritto uguale al salvato, spazi compresi, non cambia;
// diverso, si toglie dagli spazi e cambia solo se è ancora diverso dal salvato. bloccato è la relazione con cui la modifica finisce: su una busta
// conto terzi il telefono del cliente finale non si cambia (locked_fields/1).
func campiDellaModifica(r rigaBusta, m Modifica, bloccato bool) (cambi, Campi) {
	cc, campi := cambi{}, Campi{}
	if v := m.DataAccettazione; v != nil {
		switch {
		case vuoto(*v):
			campi.aggiungi("data_accettazione", MsgObbligatorio)
		case !dataVera(*v):
			campi.aggiungi("data_accettazione", MsgNonValido)
		default:
			cc.metti("data_accettazione", *v, r.accettazione)
		}
	}
	if v := m.Descrizione; v != nil {
		if vuoto(*v) {
			campi.aggiungi("descrizione", MsgObbligatorio)
		} else {
			cc.metti("descrizione", *v, r.descrizione)
		}
	}
	for _, t := range []struct {
		colonna string
		v       *string
		salvato *string
	}{{"note_pubbliche", m.NotePubbliche, r.notePubbliche}, {"note_interne", m.NoteInterne, r.noteInterne}, {"codice_qr", m.CodiceQR, r.qr}} {
		if t.v != nil {
			cc.metti(t.colonna, testoNullo(*t.v), t.salvato)
		}
	}
	prezzo(cc, campi, m.Preventivo, m.PreventivoCentesimi, r.preventivoTesto, r.preventivoCent, "preventivo")
	prezzo(cc, campi, m.PrezzoFinale, m.PrezzoCentesimi, r.prezzoTesto, r.prezzoCent, "prezzo")
	consegna(cc, campi, m, r)
	if v := m.NomeCliente; v != nil {
		if vuoto(*v) {
			campi.aggiungi("nome_cliente", MsgObbligatorio)
		} else if nome := strings.TrimSpace(*v); *v != r.nome && nome != r.nome {
			if uniseg.GraphemeClusterCount(nome) > nomeMassimo {
				campi.aggiungi("nome_cliente", MsgNomeLungo)
			} else {
				cc["nome_cliente"] = nome
			}
		}
	}
	if v := m.TelefonoCliente; v != nil && !bloccato {
		// Vuoto o di soli spazi è nil per il cast, quindi diverso da ogni telefono salvato (anche di
		// soli spazi), e diventa "".
		if tel := strings.TrimSpace(*v); (vuoto(*v) || *v != r.telefono) && tel != r.telefono {
			if uniseg.GraphemeClusterCount(tel) > telefonoMassimo {
				campi.aggiungi("telefono_cliente", MsgTelefonoLungo)
			} else {
				cc["telefono_cliente"] = tel
			}
		}
	}
	if v := m.GiorniInPiu; v != nil {
		if vuoto(*v) {
			campi.aggiungi("giorni_in_piu", MsgObbligatorio)
		} else if n, err := strconv.ParseInt(*v, 10, 64); errors.Is(err, strconv.ErrRange) {
			campi.aggiungi("giorni_in_piu", MsgGiorniInPiu) // Integer.parse/1 non ha limiti: è fuori da 0–90
		} else if err != nil {
			campi.aggiungi("giorni_in_piu", MsgNonValido)
		} else if n != r.giorni {
			if n < 0 || n > giorniInPiuMassimi {
				campi.aggiungi("giorni_in_piu", MsgGiorniInPiu)
			} else {
				cc["giorni_in_piu"] = n
			}
		}
	}
	return cc, campi
}

// prezzo è cast_price/4 per la modifica: il testo si confronta con quello che il modulo mostra
// (price_input/2: il testo, o i centesimi come «123.45»); un testo nuovo si salva com'è, con i
// centesimi se è un importo; svuotato toglie testo e centesimi; i soli centesimi tolgono il testo.
// Poi validate_number sui centesimi cambiati.
func prezzo(cc cambi, campi Campi, scritto *string, centesimi *int64, testo *string, cent *int64, nome string) {
	colTesto, colCent := nome+"_testo", nome+"_centesimi"
	campo, campoCent := nome, nome+"_centesimi"
	if nome == "prezzo" {
		campo = "prezzo_finale"
	}
	mostrato := testoMostrato(testo, cent)
	cambiaTesto := scritto != nil && !stessoValore(testoNullo(*scritto), mostrato)
	cambiaCent := centesimi != nil && !stessoValore(centesimi, cent)
	nuoviCent, hannoCent := any(nil), false
	switch {
	case cambiaTesto:
		p, err := LeggiPrezzo(*scritto)
		if err != nil {
			campi.aggiungi(campo, err.Error())
			break
		}
		cc.metti(colTesto, valoreDi(p.Testo), testo)
		nuoviCent, hannoCent = valoreDi(p.Centesimi), true
	case cambiaCent:
		cc.metti(colTesto, nil, testo)
		nuoviCent, hannoCent = *centesimi, true
	}
	if !hannoCent && cambiaCent {
		nuoviCent, hannoCent = *centesimi, true // il testo è sbagliato: i centesimi si validano lo stesso
	}
	if hannoCent {
		if n, ok := nuoviCent.(int64); ok && n < 0 {
			campi.aggiungi(campoCent, MsgCentesimi)
			return
		}
		cc.metti(colCent, nuoviCent, cent)
	}
}

// testoMostrato è price_input/2: il testo se c'è e non è vuoto, poi i centesimi come «123.45»
// (Money.euros/1), poi niente.
func testoMostrato(testo *string, cent *int64) *string {
	switch {
	case testo != nil && *testo != "":
		return testo
	case cent != nil:
		s := Euro(*cent)
		return &s
	}
	return nil
}

// Euro è Money.euros/1: i centesimi in euro con il punto e due decimali («123.45»), come li mostra
// il modulo di modifica.
func Euro(c int64) string {
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}

// consegna è cast_expected_on/1 per la modifica: la data scritta (il selettore) toglie il testo;
// il testo svuotato toglie anche la data; un testo nuovo (import, o quello che il modulo rimanda)
// dà la data se comincia con una data vera; oltre 255 byte ErrConsegnaLunga.
func consegna(cc cambi, campi Campi, m Modifica, r rigaBusta) {
	var data *string
	cambiaData := false
	if v := m.DataPrevista; v != nil {
		switch {
		case vuoto(*v):
			cambiaData = r.prevista != nil
		case !dataVera(*v):
			campi.aggiungi("data_prevista", MsgNonValido)
		default:
			data = v
			cambiaData = !stessoValore(v, r.prevista)
		}
	}
	cambiaTesto := m.DataPrevistaTesto != nil && !stessoValore(testoNullo(*m.DataPrevistaTesto), r.previstaTesto)
	switch {
	case cambiaTesto:
		c, err := LeggiConsegna(*m.DataPrevistaTesto)
		if err != nil {
			campi.aggiungi("data_prevista_testo", err.Error())
			return
		}
		cc.metti("data_prevista_testo", valoreDi(c.Testo), r.previstaTesto)
		cc.metti("data_prevista", valoreDi(c.Data), r.prevista)
	case cambiaData:
		cc.metti("data_prevista_testo", nil, r.previstaTesto)
		cc.metti("data_prevista", valoreDi(data), r.prevista)
	default:
		return
	}
	// reopen_overdue_notice/2 (slice 211): un giorno promesso diverso riapre l'avviso di ritardo.
	finale := valoreDi(r.prevista)
	if v, ok := cc["data_prevista"]; ok {
		finale = v
	}
	if finale != nil && !stessoValore(finale, r.prevista) {
		cc.metti("ritardo_avvisato_il", nil, r.ritardo)
	}
}

// aggiorna scrive le colonne cambiate, in ordine fisso.
func aggiorna(ctx context.Context, tx *sql.Tx, id int64, cc cambi) error {
	colonne := make([]string, 0, len(cc))
	for k := range cc {
		colonne = append(colonne, k)
	}
	sort.Strings(colonne)
	parti := make([]string, len(colonne))
	args := make([]any, 0, len(colonne)+1)
	for i, k := range colonne {
		parti[i] = k + " = ?"
		args = append(args, cc[k])
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET `+strings.Join(parti, ", ")+` WHERE id = ?`, append(args, id)...); err != nil {
		return fmt.Errorf("buste: busta non modificata: %w", err) // SQLite nomina le colonne, mai i valori
	}
	return nil
}

// b2b è Oreficeria.is_b2b/1 su un id già risolto (nil o int64).
func b2b(oreficeria any) bool {
	n, ok := oreficeria.(int64)
	return ok && n > 1
}

// controparte è counterparty/1: l'oreficeria esterna, o 0 per il cliente (nessuna oreficeria o la
// riga 1, il negozio).
func controparte(oreficeria any) int64 {
	if b2b(oreficeria) {
		return oreficeria.(int64)
	}
	return 0
}

// revocaPerControparte è revoke_on_counterparty_change/4 quando la controparte cambia: la
// conversazione pubblica riparte da adesso; se il link non era già revocato, si revoca (il testo
// del token va via, l'impronta resta) con repair.public_link_revoked e il solo motivo.
func (b *Buste) revocaPerControparte(ctx context.Context, tx *sql.Tx, id int64, giaRevocato bool, c audit.Contesto, ora string) error {
	if giaRevocato {
		_, err := tx.ExecContext(ctx, `UPDATE buste SET messaggi_visibili_dal = ?1, aggiornato_il = ?1 WHERE id = ?2`, ora, id)
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE buste SET token_pubblico = NULL, token_pubblico_revocato_il = ?1, messaggi_visibili_dal = ?1,
		aggiornato_il = ?1 WHERE id = ?2`, ora, id); err != nil {
		return err
	}
	return b.reg.Scrivi(ctx, tx, c, "repair.public_link_revoked", "repair", id, "ok", map[string]any{"reason": "counterparty_changed"})
}

// datiModifica sono i metadati di repair.updated: relazione e artigiano da/a, quali campi del
// cliente (mai il contenuto), giorni in più da/a, la scadenza tolta con quella che era, l'avviso di
// ritardo riaperto. Niente per i campi rimasti uguali.
func datiModifica(prima rigaBusta, cc cambi, oreficeria, artigiano any) map[string]any {
	dati := map[string]any{}
	if !stessoValore(oreficeria, prima.oreficeria) {
		dati["oreficeria_id_from"], dati["oreficeria_id_to"] = valoreDi(prima.oreficeria), oreficeria
	}
	if !stessoValore(artigiano, prima.artigiano) {
		dati["artisan_id_from"], dati["artisan_id_to"] = valoreDi(prima.artigiano), artigiano
	}
	var cliente []string
	if _, ok := cc["nome_cliente"]; ok {
		cliente = append(cliente, "name")
	}
	if _, ok := cc["telefono_cliente"]; ok {
		cliente = append(cliente, "phone")
	}
	if len(cliente) > 0 {
		dati["customer_fields"] = cliente
	}
	if g, ok := cc["giorni_in_piu"]; ok {
		dati["extra_days"] = map[string]any{"from": prima.giorni, "to": g}
	}
	dopo := prima
	if v, ok := cc["data_prevista_testo"]; ok {
		dopo.previstaTesto = puntatore(v)
	}
	if v, ok := cc["data_prevista"]; ok {
		dopo.prevista = puntatore(v)
	}
	if da := scadenza(prima); da != nil && scadenza(dopo) == nil {
		dati["expected_on"] = map[string]any{"from": *da, "to": nil}
	}
	if v, ok := cc["ritardo_avvisato_il"]; ok && v == nil && prima.ritardo != nil {
		dati["overdue_reopened"] = true
	}
	return dati
}

func puntatore(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// scadenza è deadline/1: il testo se c'è e non è vuoto, poi la data, poi niente.
func scadenza(r rigaBusta) *string {
	switch {
	case r.previstaTesto != nil && *r.previstaTesto != "":
		return r.previstaTesto
	case r.prevista != nil:
		return r.prevista
	}
	return nil
}

// EsitoTipo è cosa farebbe il cambio di tipo (kind_change/2).
type EsitoTipo int

// Gli esiti del cambio di tipo.
const (
	TipoUguale    EsitoTipo = iota // :same, lo stesso tipo
	TipoResta                      // {:keep, stato}: lo stato esiste anche nel tipo nuovo
	TipoRiallinea                  // {:realign, da, a}
	TipoRifiutato                  // {:error, motivo}
)

// CambioTipo è la risposta di kind_change/2: con TipoResta e TipoRiallinea lo stato da e a, con
// TipoRifiutato il rifiuto.
type CambioTipo struct {
	Esito   EsitoTipo
	Da, A   string
	Rifiuto Rifiuto
}

// CambioDiTipo è kind_change/2, l'unica regola del cambio di tipo, per la pagina (che avvisa) e per
// la modifica (che decide): lo stesso tipo non fa niente; una riparazione fuori bottega non
// diventa compro oro; uno stato che esiste nel tipo nuovo resta; una busta chiusa che il
// riallineamento riaprirebbe va riaperta prima; altrimenti lo stato va all'iniziale del tipo nuovo.
func CambioDiTipo(s Scheda, oro bool) CambioTipo {
	switch {
	case s.CompraOro == oro:
		return CambioTipo{Esito: TipoUguale}
	case oro && s.FuoriBottega:
		return CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoFuoriBottega}
	case slices.Contains(Stati(oro), s.Stato):
		return CambioTipo{Esito: TipoResta, Da: s.Stato, A: s.Stato}
	case Chiuso(s.Stato):
		return CambioTipo{Esito: TipoRifiutato, Rifiuto: ErrTipoChiusa}
	}
	return CambioTipo{Esito: TipoRiallinea, Da: s.Stato, A: StatoIniziale(oro)}
}

// cambiaTipo è apply_kind_change/4 con write_kind_change/5: solo tipo e stato (nessuna data, nessun
// campo, nessun artigiano), con repair.kind_changed.
func (b *Buste) cambiaTipo(ctx context.Context, tx *sql.Tx, id int64, r rigaBusta, oro bool, c audit.Contesto, ora string) (Modificata, error) {
	ct := CambioDiTipo(Scheda{Stato: r.stato, CompraOro: r.oro, FuoriBottega: r.fuori}, oro)
	switch ct.Esito {
	case TipoUguale:
		return Modificata{StatoPrima: r.stato, StatoDopo: r.stato}, nil
	case TipoRifiutato:
		return Modificata{}, ct.Rifiuto
	}
	cc := cambi{"compro_oro": oro, "aggiornato_il": ora}
	cc.metti("stato", ct.A, r.stato)
	if err := aggiorna(ctx, tx, id, cc); err != nil {
		return Modificata{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.kind_changed", "repair", id, "ok", map[string]any{
		"compro_oro": oro, "status_from": r.stato, "status_to": ct.A,
	}); err != nil {
		return Modificata{}, err
	}
	return Modificata{TipoCambiato: true, StatoPrima: r.stato, StatoDopo: ct.A}, nil
}
