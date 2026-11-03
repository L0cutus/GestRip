package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
)

// La busta per la ricevuta PDF (RIP-S-42), come ReceiptController.show/2 e Receipt.render_html/1
// dello standalone a go-baseline:
//   - la busta si cerca come Workshop.get_repair!/1: l'id dell'indirizzo passa dal cast di :id di
//     Ecto (Integer.parse/1 senza resto), poi Repo.get! con il filtro del cestino;
//   - la busta e il profilo del negozio si leggono con una sola query, così nome e sottotitolo
//     della stessa intestazione vengono dalla stessa lettura (render_html/2);
//   - il PDF lo fa il chiamante (fai); solo se è fatto si scrive repair.receipt_generated
//     (Workshop.record_repair_event/3). Senza la riga la ricevuta non esce. È l'unica riga della
//     ricevuta: non è un'apertura della scheda, non scrive repair.viewed (come lo standalone).

var (
	// ErrIdNonValido: l'id dell'indirizzo non è un intero per Ecto (Ecto.Query.CastError: 400).
	ErrIdNonValido = errors.New("buste: id della busta non valido")
	// ErrIdFuoriScala: un intero oltre i 64 bit; lo standalone lo passa a SQLite e risponde 500
	// (misurato sul banco il 03/11/2026).
	ErrIdFuoriScala = errors.New("buste: id della busta fuori scala")
	// ErrRicevutaNonFatta: il PDF non si è fatto o la sua riga d'audit non si è scritta: la
	// ricevuta non si consegna (503 «PDF non disponibile»).
	ErrRicevutaNonFatta = errors.New("buste: ricevuta non fatta")
)

// DatiRicevuta è ciò che la ricevuta stampa. Le date sono AAAA-MM-GG; nil è NULL.
type DatiRicevuta struct {
	ID                           int64
	Numero                       string
	NomeNegozio                  string  // shop_name/1: il nome, o «Negozio» senza profilo o con il nome vuoto
	Sottotitolo                  *string // il sottotitolo del profilo, com'è
	NomeCliente, TelefonoCliente string  // le copie salvate sulla busta
	DataAccettazione             string
	Descrizione                  string
	NotePubbliche                *string
	Consegna                     Consegna
	Preventivo, PrezzoFinale     Prezzo
}

// Prevista è delivery/1: il testo scritto, la data, o «Da concordare» (Repair.date_source/2).
func (d DatiRicevuta) Prevista() string {
	switch {
	case d.Consegna.Testo != nil && *d.Consegna.Testo != "":
		return *d.Consegna.Testo
	case d.Consegna.Data != nil:
		return *d.Consegna.Data
	}
	return "Da concordare"
}

// RigaDelSottotitolo è subtitle_line/1: il sottotitolo, se non è vuoto o di soli spazi.
func (d DatiRicevuta) RigaDelSottotitolo() (string, bool) {
	if d.Sottotitolo == nil || strings.TrimFunc(*d.Sottotitolo, unicode.IsSpace) == "" {
		return "", false
	}
	return *d.Sottotitolo, true
}

// Importi sono le righe di price_section/1: «Preventivo: …» come scritto (o «€ 12,50» dai soli
// centesimi) e «Prezzo finale: …» con Money.label/1; nessuna riga per un prezzo che non c'è.
func (d DatiRicevuta) Importi() []string {
	var righe []string
	switch {
	case d.Preventivo.Testo != nil && *d.Preventivo.Testo != "":
		righe = append(righe, "Preventivo: "+*d.Preventivo.Testo)
	case d.Preventivo.Centesimi != nil:
		righe = append(righe, "Preventivo: "+EtichettaCentesimi(*d.Preventivo.Centesimi))
	}
	if e := EtichettaPrezzo(d.PrezzoFinale); e != "" {
		righe = append(righe, "Prezzo finale: "+e)
	}
	return righe
}

// formatoEtichetta è @label_format di Money: un importo o un intervallo («35», «35,50», «400/500»).
// \s di PCRE senza il flag u è [\t\n\v\f\r ].
var formatoEtichetta = regexp.MustCompile(`^[0-9]+(?:[.,][0-9]{1,2})?(?:[\t\n\v\f\r ]*[/-][\t\n\v\f\r ]*[0-9]+(?:[.,][0-9]{1,2})?)?$`)

// EtichettaPrezzo è Money.label/1 sul prezzo (price_source/2): il testo scritto, se c'è, con «€ »
// davanti quando, tolti gli spazi ai bordi (String.trim/1, spazi Unicode compresi), è un importo o
// un intervallo, altrimenti com'è; poi i centesimi «€ 12,50»; "" senza prezzo.
func EtichettaPrezzo(p Prezzo) string {
	switch {
	case p.Testo != nil && *p.Testo != "":
		if t := strings.TrimFunc(*p.Testo, unicode.IsSpace); formatoEtichetta.MatchString(t) {
			return "€ " + t
		}
		return *p.Testo
	case p.Centesimi != nil:
		return EtichettaCentesimi(*p.Centesimi)
	}
	return ""
}

// NomeFileRicevuta è il nome del file allegato: «ricevuta-<numero con / → ->.pdf».
func NomeFileRicevuta(numero string) string {
	return "ricevuta-" + strings.ReplaceAll(numero, "/", "-") + ".pdf"
}

// idEcto è il cast di :id di Ecto su un testo: Integer.parse/1 senza resto, cioè un segno
// facoltativo e cifre ASCII.
var idEcto = regexp.MustCompile(`^[+-]?[0-9]+$`)

// idDellaRicevuta legge l'id come get_repair!/1: ErrIdNonValido se non è un intero, ErrIdFuoriScala
// oltre i 64 bit, ErrBustaNonTrovata se non è positivo.
func idDellaRicevuta(s string) (int64, error) {
	if !idEcto.MatchString(s) {
		return 0, ErrIdNonValido
	}
	n, ok := new(big.Int).SetString(strings.TrimPrefix(s, "+"), 10)
	if !ok {
		return 0, ErrIdNonValido
	}
	if !n.IsInt64() {
		return 0, ErrIdFuoriScala
	}
	if n.Sign() <= 0 {
		return 0, ErrBustaNonTrovata
	}
	return n.Int64(), nil
}

// Ricevuta legge la busta id, fa il PDF con fai e, solo se è fatto, scrive repair.receipt_generated
// per chi la chiede (c). Errori: ErrIdNonValido, ErrIdFuoriScala, ErrBustaNonTrovata (nessun PDF e
// nessuna riga), ErrRicevutaNonFatta (PDF o riga non riusciti), o un guasto del database.
func (b *Buste) Ricevuta(ctx context.Context, id string, c audit.Contesto, fai func(DatiRicevuta) ([]byte, error)) ([]byte, DatiRicevuta, error) {
	n, err := idDellaRicevuta(id)
	if err != nil {
		return nil, DatiRicevuta{}, err
	}
	d, err := b.leggiRicevuta(ctx, n)
	if err != nil {
		return nil, DatiRicevuta{}, err
	}
	pdf, err := fai(d)
	if err != nil {
		return nil, d, fmt.Errorf("%w: %w", ErrRicevutaNonFatta, err)
	}
	if err := b.reg.Registra(ctx, c, "repair.receipt_generated", "repair", d.ID, "ok", map[string]any{"bag_number": d.Numero}); err != nil {
		return nil, d, fmt.Errorf("%w: %w", ErrRicevutaNonFatta, err)
	}
	return pdf, d, nil
}

func (b *Buste) leggiRicevuta(ctx context.Context, id int64) (DatiRicevuta, error) {
	var d DatiRicevuta
	var note, consTesto, consData, prevTesto, finTesto, negozio, sottotitolo sql.NullString
	var prevCent, finCent sql.NullInt64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT b.id, b.numero, b.nome_cliente, b.telefono_cliente, b.data_accettazione,
		b.descrizione, b.note_pubbliche, b.data_prevista_testo, b.data_prevista, b.preventivo_testo, b.preventivo_centesimi,
		b.prezzo_testo, b.prezzo_centesimi, p.nome, p.sottotitolo
		FROM buste b LEFT JOIN profilo_negozio p ON p.ambito = 'default'
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(
		&d.ID, &d.Numero, &d.NomeCliente, &d.TelefonoCliente, &d.DataAccettazione, &d.Descrizione, &note, &consTesto,
		&consData, &prevTesto, &prevCent, &finTesto, &finCent, &negozio, &sottotitolo)
	if errors.Is(err, sql.ErrNoRows) {
		return DatiRicevuta{}, ErrBustaNonTrovata
	}
	if err != nil {
		return DatiRicevuta{}, err
	}
	d.NotePubbliche = nullaTesto(note)
	d.Consegna = Consegna{Testo: nullaTesto(consTesto), Data: nullaTesto(consData)}
	d.Preventivo = Prezzo{Testo: nullaTesto(prevTesto), Centesimi: nullaIntero(prevCent)}
	d.PrezzoFinale = Prezzo{Testo: nullaTesto(finTesto), Centesimi: nullaIntero(finCent)}
	d.NomeNegozio = nomeNegozioDiRipiego
	if negozio.Valid && negozio.String != "" {
		d.NomeNegozio = negozio.String
	}
	if negozio.Valid {
		d.Sottotitolo = nullaTesto(sottotitolo)
	}
	return d, nil
}
