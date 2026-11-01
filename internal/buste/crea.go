package buste

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La creazione della busta (RIP-H-30, 33, 34, 35, 38, 39), come
// Workshop.create_repair_with_customer/4 con create_repair/4: cliente, busta, gettone pubblico e
// righe d'audit in una transazione sola. Un rifiuto non lascia niente.

// DurataGettone è la validità del link cliente di una busta nuova (@public_token_lifetime_seconds).
const DurataGettone = 90 * 24 * time.Hour

const byteGettone = 32

// sqliteUnico è SQLITE_CONSTRAINT_UNIQUE.
const sqliteUnico = 2067

// Accettazione sono i campi del modulo della nuova busta (@intake_fields di WorkshopLive), come li
// manda il modulo. Non c'è altro: stato, prezzo finale, testo di consegna, copie del cliente e
// gettone non si impostano da fuori.
type Accettazione struct {
	NomeCliente, TelefonoCliente string
	OreficeriaID                 string // vuoto: diretta del negozio; mai 1 (il negozio)
	ArtigianoID                  string // vuoto: nessuno
	Numero                       string // vuoto: il prossimo dell'anno della data di accettazione
	DataAccettazione             string // AAAA-MM-GG
	DataPrevista                 string // AAAA-MM-GG o vuota
	Descrizione                  string // la lavorazione
	NotePubbliche, NoteInterne   string
	Preventivo                   string // testo libero
	CompraOro                    string // "1", "on" o "true" per una scheda compro oro
	CodiceQR                     string
}

// Creata è la busta appena salvata. Token è il link cliente in chiaro (per «Link cliente» o «Link
// dell'oreficeria», mostrato una volta): non va mai in registri, audit o errori.
type Creata struct {
	ID, ClienteID int64
	Numero        string
	OreficeriaID  *int64 // non nil: busta di un'oreficeria esterna, il link è dell'oreficeria
	Token         string
}

// Crea salva una busta nuova dal modulo, con il suo cliente, nell'ordine dello standalone: il
// nome del cliente (ErrNomeCliente) e i suoi limiti (ErrCliente); l'oreficeria (ErrOreficeria) e
// l'artigiano (ErrArtigiano); il numero; i campi del modulo, con gli errori tutti insieme (Campi);
// per ultimo il numero già esistente (Campi con MsgNumeroEsistente). Gli errori dei campi
// arrivano come *CampiRifiutati, con il numero risolto. Ogni altro errore è del
// database: la pagina mostra MessaggioNonSalvata.
func (b *Buste) Crea(ctx context.Context, a Accettazione, c audit.Contesto) (Creata, error) {
	t, ora := b.adesso()
	oro := CompraOroDalModulo(a.CompraOro)
	stato := StatoIniziale(oro)
	token, impronta := gettone()

	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Creata{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente

	cl, err := b.risolviCliente(ctx, tx, a.NomeCliente, a.TelefonoCliente, c, ora)
	if err != nil {
		return Creata{}, err
	}
	oreficeria, err := relazione(ctx, tx, a.OreficeriaID, `SELECT count(*) FROM oreficerie WHERE id = ? AND id > 1`, ErrOreficeria)
	if err != nil {
		return Creata{}, err
	}
	artigiano, err := relazione(ctx, tx, a.ArtigianoID, `SELECT count(*) FROM artigiani WHERE id = ?`, ErrArtigiano)
	if err != nil {
		return Creata{}, err
	}
	numero, err := risolviNumero(ctx, tx, a.Numero, annoDiAccettazione(a.DataAccettazione, t))
	if err != nil {
		return Creata{}, err
	}

	campi := Campi{}
	var accettata any
	if vuoto(a.DataAccettazione) {
		campi.aggiungi("data_accettazione", MsgObbligatorio)
	} else if v, msg := dataDalModulo(a.DataAccettazione); msg != "" {
		campi.aggiungi("data_accettazione", msg)
	} else {
		accettata = v
	}
	prevista, msg := dataDalModulo(a.DataPrevista)
	if msg != "" {
		campi.aggiungi("data_prevista", msg)
	}
	if vuoto(a.Descrizione) {
		campi.aggiungi("descrizione", MsgObbligatorio)
	}
	if !formatoNumeroScritto.MatchString(numero) {
		campi.aggiungi("numero", MsgFormatoNumero)
	}
	preventivo, err := LeggiPrezzo(a.Preventivo)
	if err != nil {
		campi.aggiungi("preventivo", err.Error())
	}
	if len(campi) > 0 {
		return Creata{}, &CampiRifiutati{Campi: campi, Numero: numero}
	}

	r, err := tx.ExecContext(ctx, `INSERT INTO buste (cliente_id, nome_cliente, telefono_cliente, numero, data_accettazione,
		data_prevista, descrizione, note_pubbliche, note_interne, stato, compro_oro, preventivo_centesimi, preventivo_testo,
		oreficeria_id, artigiano_id, hash_token_pubblico, token_pubblico, token_pubblico_scade_il, codice_qr, creato_il, aggiornato_il)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cl.id, cl.nome, cl.telefono, numero, accettata, prevista, a.Descrizione, testoONulla(a.NotePubbliche),
		testoONulla(a.NoteInterne), stato, oro, preventivo.centesimi(), preventivo.testo(), oreficeria, artigiano,
		impronta, token, t.Add(DurataGettone).Format(db.FormatoIstante), testoONulla(a.CodiceQR), ora, ora)
	if numeroGiaUsato(err) {
		return Creata{}, &CampiRifiutati{Campi: Campi{"numero": {MsgNumeroEsistente}}, Numero: numero}
	}
	if err != nil {
		return Creata{}, fmt.Errorf("buste: busta non salvata: %w", err) // SQLite nomina le colonne, mai i valori
	}
	id, err := r.LastInsertId()
	if err != nil {
		return Creata{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.created", "repair", id, "ok", map[string]any{
		"bag_number": numero, "status": stato, "compro_oro": oro, "oreficeria_id": oreficeria, "artisan_id": artigiano,
	}); err != nil {
		return Creata{}, err
	}
	if err := tx.Commit(); err != nil {
		return Creata{}, err
	}
	creata := Creata{ID: id, ClienteID: cl.id, Numero: numero, Token: token}
	if v, ok := oreficeria.(int64); ok {
		creata.OreficeriaID = &v
	}
	return creata, nil
}

// String nasconde il token, come redact: true dello standalone: %v e %+v.
func (c Creata) String() string {
	o := "nil"
	if c.OreficeriaID != nil {
		o = strconv.FormatInt(*c.OreficeriaID, 10)
	}
	return fmt.Sprintf("{ID:%d ClienteID:%d Numero:%s OreficeriaID:%s Token:**redacted**}", c.ID, c.ClienteID, c.Numero, o)
}

// GoString nasconde il token anche con %#v.
func (c Creata) GoString() string { return "buste.Creata" + c.String() }

// LogValue nasconde il token nel registro slog.
func (c Creata) LogValue() slog.Value {
	a := []slog.Attr{slog.Int64("id", c.ID), slog.Int64("cliente_id", c.ClienteID), slog.String("numero", c.Numero)}
	if c.OreficeriaID != nil {
		a = append(a, slog.Int64("oreficeria_id", *c.OreficeriaID))
	}
	return slog.GroupValue(append(a, slog.String("token", "**redacted**"))...)
}

// gettone è un token casuale da 32 byte in base64url senza «=» e la sua impronta SHA-256 (del
// testo del token, come hash_token/1): la pagina /r/ lo verificherà come quelli importati.
func gettone() (string, []byte) {
	b := make([]byte, byteGettone)
	_, _ = rand.Read(b) // crypto/rand.Read non fallisce
	token := base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	return token, h[:]
}

// annoDiAccettazione è accepted_on_year/1: l'anno della data scritta, se è una data; altrimenti
// quello di oggi in negozio (la creazione poi rifiuta la data).
func annoDiAccettazione(data string, t time.Time) int {
	if dataVera(data) {
		a, _ := strconv.Atoi(data[:4])
		return a
	}
	return clock.DataDi(t).Anno
}

// relazione è resolve_external_oreficeria_id/2 e resolve_artisan_id/2: vuoto (anche di soli
// spazi) è NULL; un id intero positivo che la domanda trova è l'id; tutto il resto è rifiuto.
func relazione(ctx context.Context, tx *sql.Tx, scritto, domanda string, rifiuto error) (any, error) {
	s := strings.TrimSpace(scritto)
	if s == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(s, 10, 64) // come Integer.parse/1: segno ammesso, niente «_»
	if err != nil || id <= 0 {
		return nil, rifiuto
	}
	var n int
	if err := tx.QueryRowContext(ctx, domanda, id).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, rifiuto
	}
	return id, nil
}

func numeroGiaUsato(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteUnico && strings.Contains(se.Error(), "buste.numero")
}
