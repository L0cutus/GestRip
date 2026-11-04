package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Le etichette QR prenumerate (RIP-S-47, RIP-S-48), come LabelSheet e LabelSheets dello standalone
// (slice 210: al più 100 etichette per volta, contatore max), con le deviazioni approvate da Stefano:
//   - D-08 (04/11/2026): il PDF non salva il contatore. Salva solo il formato e la riga
//     label_sheet.generated, come lo standalone; il contatore etichette_qr_ultimo = max(ultimo, a)
//     si salva con la conferma «Sì, stampate» (ConfermaEtichette), con la sua riga
//     label_sheet.confirmed, nella stessa transazione. La conferma rifà le stesse regole del PDF,
//     limite di 100 compreso;
//   - D-22 (04/11/2026): il titolare azzera il contatore a mano (AzzeraEtichette), con la riga
//     label_sheet.counter_reset;
//   - D-66 (04/11/2026): l'anno dell'ultimo foglio confermato si ricorda in etichette_qr_anno (il più
//     nuovo fra quello salvato e quello del foglio, nella transazione della conferma); il modulo
//     propone l'azzeramento quando l'anno proposto è più nuovo e il contatore non è zero. Senza
//     quell'anno (dati importati: lo standalone non lo ha) non propone niente. Dopo l'azzeramento il
//     contatore è 0 e l'anno salvato è quello per cui si azzera: la proposta non ricompare per
//     quell'anno. La pagina del PDF pronto la propone per l'anno del foglio (NuovoAnnoPer); la conferma
//     accetta solo gli anni del modulo, 2024…2099 (AnnoDelModulo);
//   - D-21 e D-64: il numero si stampa sotto il QR come «0045/26» (stampe.EtichetteQR).
//
// La lettura della richiesta copia LabelSheet.parse/2: «da» alzato a 1, «a» mancante = da+49, anno
// e 57×32 mm come valori mancanti, 16…200 mm compresi; un valore che non è un testo, un numero che
// Integer.parse/Float.parse non leggono per intero (dopo String.trim/1), più di 18 cifre o più di 26
// caratteri, da > a o mm fuori: ErrRichiestaEtichette. Oltre 100 etichette: TroppeEtichette.

// Le chiavi delle impostazioni: le prime tre sono quelle dello standalone, l'anno è del Go (D-66).
const (
	ChiaveUltimaEtichetta = "etichette_qr_ultimo"
	ChiaveLarghezzaQR     = "etichette_qr_w"
	ChiaveAltezzaQR       = "etichette_qr_h"
	ChiaveAnnoEtichette   = "etichette_qr_anno"
)

// MaxEtichette è LabelSheet.max_labels/0.
const MaxEtichette = 100

const (
	larghezzaEtichetteQR = 57.0
	altezzaEtichetteQR   = 32.0
	minMMEtichette       = 16.0
	maxMMEtichette       = 200.0
	maxCifreEtichette    = 18
)

// ErrRichiestaEtichette è il 400 «Richiesta non valida» dello standalone.
var ErrRichiestaEtichette = errors.New("buste: richiesta delle etichette non valida")

// MsgRichiestaEtichette è il corpo di quel 400.
const MsgRichiestaEtichette = "Richiesta non valida"

// TroppeEtichette è {:too_many, n, sheet}: la richiesta com'era e quante etichette chiedeva.
type TroppeEtichette struct {
	N      int64
	Foglio FoglioEtichette
}

func (e TroppeEtichette) Error() string {
	return fmt.Sprintf("Massimo %d etichette per volta: hai chiesto %d.", MaxEtichette, e.N)
}

// FoglioEtichette è un foglio di etichette letto e valido: da, a, anno e formato in mm.
type FoglioEtichette struct {
	Da, A, Anno int64
	L, H        float64
}

// Parametro è un parametro della richiesta come lo vede Plug: Presente se c'è, Testo se è un testo
// (non una lista né una mappa), Valore il testo.
type Parametro struct {
	Valore          string
	Presente, Testo bool
}

// LeggiFoglio è LabelSheet.parse/2 con l'anno del negozio annoLocale.
func LeggiFoglio(p func(nome string) Parametro, annoLocale int) (FoglioEtichette, error) {
	da, ok := interoEtichette(p("da"), 1)
	if !ok {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	da = max(1, da)
	a, ok := interoEtichette(p("a"), da+49)
	if !ok {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	anno, ok := interoEtichette(p("anno"), int64(annoLocale))
	if !ok {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	l, ok := decimaleEtichette(p("w"), larghezzaEtichetteQR)
	if !ok {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	h, ok := decimaleEtichette(p("h"), altezzaEtichetteQR)
	if !ok {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	if da > a || !mmValido(l) || !mmValido(h) {
		return FoglioEtichette{}, ErrRichiestaEtichette
	}
	f := FoglioEtichette{Da: da, A: a, Anno: anno, L: l, H: h}
	if n := a - da + 1; n > MaxEtichette {
		return FoglioEtichette{}, TroppeEtichette{N: n, Foglio: f}
	}
	return f, nil
}

func mmValido(v float64) bool { return v >= minMMEtichette && v <= maxMMEtichette }

var (
	reInteroEtichette   = regexp.MustCompile(`^[+-]?[0-9]+$`)
	reDecimaleEtichette = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	reSegnoPunto        = regexp.MustCompile(`^([+-]?)\.`)
	reCifra             = regexp.MustCompile(`[0-9]`)
)

// cifreBuone è digits_ok?/1: non vuoto, al più 26 caratteri (grafemi) e al più 18 cifre.
func cifreBuone(s string) bool {
	return s != "" && uniseg.GraphemeClusterCount(s) <= maxCifreEtichette+8 &&
		len(reCifra.FindAllStringIndex(s, -1)) <= maxCifreEtichette
}

func interoEtichette(p Parametro, mancante int64) (int64, bool) {
	if !p.Presente {
		return mancante, true
	}
	if !p.Testo {
		return 0, false
	}
	s := strings.TrimFunc(p.Valore, unicode.IsSpace)
	if !cifreBuone(s) || !reInteroEtichette.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64) // al più 18 cifre: sta sempre nei 64 bit
	return n, err == nil
}

func decimaleEtichette(p Parametro, mancante float64) (float64, bool) {
	if !p.Presente {
		return mancante, true
	}
	if !p.Testo {
		return 0, false
	}
	s := strings.TrimFunc(p.Valore, unicode.IsSpace)
	s = reSegnoPunto.ReplaceAllString(s, "${1}0.")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	if !cifreBuone(s) || !reDecimaleEtichette.MatchString(s) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	// 1e400: Float.parse/1 solleva ArgumentError, cioè :error. Un valore troppo piccolo (1e-400, che
	// lo standalone rifiuta e Go legge come 0) cade comunque fuori dai 16…200 mm.
	if err != nil || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// Quattro è pad/1: il numero con gli zeri davanti fino a 4 cifre, mai tagliato (10000 resta 10000).
func Quattro(n int64) string {
	s := strconv.FormatInt(n, 10) // String.pad_leading/3 sul testo, segno compreso (non succede: da ≥ 1)
	return strings.Repeat("0", max(0, 4-len(s))) + s
}

// CodiceEtichetta è il contenuto del QR: «RIP-<anno>-NNNN» (codes/1).
func CodiceEtichetta(anno, n int64) string {
	return "RIP-" + strconv.FormatInt(anno, 10) + "-" + Quattro(n)
}

// NumeroEtichetta è il numero stampato sotto il QR (D-21, D-64): «0045/26», le 4 cifre di pad/1, la
// barra e l'anno a 2 cifre (le ultime due dell'anno).
func NumeroEtichetta(anno, n int64) string {
	aa := anno % 100
	if aa < 0 {
		aa = -aa
	}
	return fmt.Sprintf("%s/%02d", Quattro(n), aa)
}

// IdFoglio è range_id/1: «<anno>-<da>-<a>» con pad/1.
func IdFoglio(f FoglioEtichette) string {
	return strconv.FormatInt(f.Anno, 10) + "-" + Quattro(f.Da) + "-" + Quattro(f.A)
}

// NomeFileEtichetteQR è filename/1: «etichette-qr-<anno>-<da>-<a>.pdf».
func NomeFileEtichetteQR(f FoglioEtichette) string { return "etichette-qr-" + IdFoglio(f) + ".pdf" }

// FormatoG è format_g/1 dello standalone (Python f'{v:g}'): sei cifre significative, arrotondate a
// metà per eccesso sul valore esatto (Float.round/2), senza zeri in coda; un intero senza punto;
// altrimenti Float.to_string/1 (misurato in fab-220: 57.03125 → «57.0313», 0.0001 → «0.0001»,
// 1.0e-5 → «1.0e-5»).
func FormatoG(v float64) string {
	if v == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	m := int(math.Floor(math.Log10(math.Abs(v))))
	r := arrotondaMetaSu(v, max(0, min(15, 5-m)))
	if r == math.Trunc(r) {
		return new(big.Float).SetFloat64(r).Text('f', 0)
	}
	return floatComeElixir(r)
}

// arrotondaMetaSu è Float.round(v, cifre): sul valore esatto del float, la metà lontano da zero.
func arrotondaMetaSu(v float64, cifre int) float64 {
	esatto := new(big.Float).SetPrec(2000).SetFloat64(v)
	scala := new(big.Float).SetPrec(2000).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(cifre)), nil))
	x := new(big.Float).SetPrec(2000).Mul(esatto, scala)
	mezzo := big.NewFloat(0.5)
	if x.Sign() < 0 {
		x.Sub(x, mezzo)
	} else {
		x.Add(x, mezzo)
	}
	i, _ := x.Int(nil) // verso lo zero: con il mezzo aggiunto è la metà lontano da zero
	q := new(big.Float).SetPrec(2000).SetInt(i)
	q.Quo(q, scala)
	r, _ := q.Float64()
	return r
}

// floatComeElixir è Float.to_string/1 (:erlang.float_to_binary(v, [:short])) per un valore non intero
// sotto 10^15: la forma fissa più corta, o «1.0e-5» sotto 10^-4.
func floatComeElixir(v float64) string {
	if math.Abs(v) >= 1e-4 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	s := strconv.FormatFloat(v, 'e', -1, 64) // «1e-05», «1.5e-07»
	mant, esp, _ := strings.Cut(s, "e")
	if !strings.Contains(mant, ".") {
		mant += ".0"
	}
	n, _ := strconv.Atoi(esp)
	return mant + "e" + strconv.Itoa(n)
}

// ModuloEtichette sono i valori del modulo (form_defaults/1) e, per D-66, se proporre l'azzeramento
// del nuovo anno.
type ModuloEtichette struct {
	Ultimo, Da, A, Anno int64
	L, H                float64
	NuovoAnno           bool // l'anno proposto (Anno) è più nuovo dell'ultimo foglio confermato (D-66)
	// AnnoConfermato è l'anno dell'ultimo foglio confermato (etichette_qr_anno), se AnnoNoto.
	AnnoConfermato int64
	AnnoNoto       bool
}

// ConAnno è il modulo con l'anno proposto dato (dopo un azzeramento dalla pagina del PDF pronto) e la
// proposta del D-66 per quell'anno.
func (m ModuloEtichette) ConAnno(anno int64) ModuloEtichette {
	m.Anno, m.NuovoAnno = anno, m.NuovoAnnoPer(anno)
	return m
}

// NuovoAnnoPer dice se, per un foglio dell'anno dato, proporre l'azzeramento del nuovo anno (D-66):
// l'anno dell'ultimo foglio confermato è noto e più vecchio, e il contatore non è 0.
func (m ModuloEtichette) NuovoAnnoPer(anno int64) bool {
	return m.AnnoNoto && m.Ultimo > 0 && anno > m.AnnoConfermato
}

// L'intervallo dell'anno del modulo (min e max dell'input): la conferma, che c'è solo nel Go, non
// accetta altro, così un anno forgiato (20267) non blocca la D-66 per sempre. /labels.pdf resta
// come lo standalone.
const (
	MinAnnoEtichette = 2024
	MaxAnnoEtichette = 2099
)

// AnnoDelModulo dice se l'anno sta nell'intervallo del modulo.
func AnnoDelModulo(anno int64) bool { return anno >= MinAnnoEtichette && anno <= MaxAnnoEtichette }

// Piccolo è l'avviso del formato piccolo: il lato minore sotto i 20 mm.
func (m ModuloEtichette) Piccolo() bool { return min(m.L, m.H) < 20 }

// ConFoglio è il modulo con la richiesta rifiutata com'era (Map.merge(form_defaults, sheet)).
func (m ModuloEtichette) ConFoglio(f FoglioEtichette) ModuloEtichette {
	m.Da, m.A, m.Anno, m.L, m.H = f.Da, f.A, f.Anno, f.L, f.H
	return m
}

func (b *Buste) impostazione(ctx context.Context, q lettore, chiave string) (string, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT valore FROM impostazioni WHERE chiave = ?`, chiave).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// interoSalvato è stored_integer/2: Integer.parse/1 del testo senza spazi ai bordi, per intero;
// altrimenti il ripiego. Oltre i 64 bit (lo standalone ha gli interi senza limite) vale il ripiego.
func interoSalvato(s string, ripiego int64) int64 {
	s = strings.TrimFunc(s, unicode.IsSpace)
	if !reInteroEtichette.MatchString(s) {
		return ripiego
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return ripiego
	}
	return n
}

// decimaleSalvato è stored_float/2: Float.parse/1 per intero, senza le correzioni del modulo.
func decimaleSalvato(s string, ripiego float64) float64 {
	s = strings.TrimFunc(s, unicode.IsSpace)
	if !reDecimaleEtichette.MatchString(s) {
		return ripiego
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return ripiego
	}
	return v
}

// ModuloEtichette dà i valori del modulo: da = ultimo+1, a = ultimo+50, l'anno di oggi, l'ultimo
// formato salvato o 57×32 mm (form_defaults/1); NuovoAnno per D-66.
func (b *Buste) ModuloEtichette(ctx context.Context) (ModuloEtichette, error) {
	anno := clock.Oggi(b.o).Anno
	m := ModuloEtichette{Anno: int64(anno), L: larghezzaEtichetteQR, H: altezzaEtichetteQR}
	v, ok, err := b.impostazione(ctx, b.d.Lettura, ChiaveUltimaEtichetta)
	if err != nil {
		return ModuloEtichette{}, err
	}
	if ok {
		m.Ultimo = interoSalvato(v, 0)
	}
	m.Da, m.A = m.Ultimo+1, m.Ultimo+50
	if v, ok, err = b.impostazione(ctx, b.d.Lettura, ChiaveLarghezzaQR); err != nil {
		return ModuloEtichette{}, err
	} else if ok {
		m.L = decimaleSalvato(v, larghezzaEtichetteQR)
	}
	if v, ok, err = b.impostazione(ctx, b.d.Lettura, ChiaveAltezzaQR); err != nil {
		return ModuloEtichette{}, err
	} else if ok {
		m.H = decimaleSalvato(v, altezzaEtichetteQR)
	}
	if v, ok, err = b.impostazione(ctx, b.d.Lettura, ChiaveAnnoEtichette); err != nil {
		return ModuloEtichette{}, err
	} else if ok {
		if a := interoSalvato(v, math.MaxInt64); a != math.MaxInt64 {
			m.AnnoConfermato, m.AnnoNoto = a, true
		}
	}
	m.NuovoAnno = m.NuovoAnnoPer(m.Anno)
	return m, nil
}

type esecutoreLettore interface {
	audit.Esecutore
	lettore
}

func (b *Buste) salvaImpostazione(ctx context.Context, tx audit.Esecutore, chiave, valore, ora string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
		ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, chiave, valore, ora)
	return err
}

// GeneraEtichette fa il PDF del foglio con fai e, solo se è fatto, salva il formato (format_g) e
// scrive label_sheet.generated in una transazione (D-08: il contatore no). Un PDF che non si fa o
// una transazione che non si chiude: ErrPDFNonFatto, e niente salvato.
func (b *Buste) GeneraEtichette(ctx context.Context, f FoglioEtichette, c audit.Contesto, fai func(FoglioEtichette) ([]byte, error)) ([]byte, error) {
	pdf, err := fai(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	if err := b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		l, h := FormatoG(f.L), FormatoG(f.H)
		if err := b.salvaImpostazione(ctx, tx, ChiaveLarghezzaQR, l, ora); err != nil {
			return err
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveAltezzaQR, h, ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "label_sheet.generated", "label_sheet", IdFoglio(f), "ok", datiFoglio(f))
	}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	return pdf, nil
}

func datiFoglio(f FoglioEtichette) map[string]any {
	return map[string]any{"from": f.Da, "to": f.A, "year": f.Anno, "w": FormatoG(f.L), "h": FormatoG(f.H)}
}

func (b *Buste) inTransazione(ctx context.Context, fai func(tx esecutoreLettore, ora string) error) error {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if err := fai(tx, ora); err != nil {
		return err
	}
	return tx.Commit()
}

// ConfermaEtichette è «Sì, stampate» (D-08): in una transazione, con l'ultimo numero letto dentro
// (il lucchetto di scrittura lo tiene fermo), etichette_qr_ultimo = max(ultimo, a), l'anno del D-66 =
// max(anno salvato, anno del foglio), e label_sheet.confirmed. Dà il nuovo ultimo numero. Se la riga
// non si scrive non si salva niente.
func (b *Buste) ConfermaEtichette(ctx context.Context, f FoglioEtichette, c audit.Contesto) (int64, error) {
	var ultimo int64
	err := b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		v, _, err := b.impostazione(ctx, tx, ChiaveUltimaEtichetta)
		if err != nil {
			return err
		}
		prima := interoSalvato(v, 0)
		ultimo = max(prima, f.A)
		va, okAnno, err := b.impostazione(ctx, tx, ChiaveAnnoEtichette)
		if err != nil {
			return err
		}
		anno := f.Anno
		if okAnno {
			if a := interoSalvato(va, math.MinInt64); a > anno {
				anno = a
			}
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveUltimaEtichetta, strconv.FormatInt(ultimo, 10), ora); err != nil {
			return err
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveAnnoEtichette, strconv.FormatInt(anno, 10), ora); err != nil {
			return err
		}
		d := datiFoglio(f)
		d["previous"], d["last"] = prima, ultimo
		return b.reg.Scrivi(ctx, tx, c, "label_sheet.confirmed", "label_sheet", IdFoglio(f), "ok", d)
	})
	if err != nil {
		return 0, err
	}
	return ultimo, nil
}

// AzzeraEtichette è l'azzeramento del contatore del titolare (D-22; nuovoAnno se viene dalla proposta
// del D-66) per l'anno dato (quello del foglio o del modulo, o l'anno di oggi): in una transazione
// etichette_qr_ultimo = 0, etichette_qr_anno = anno (così la proposta non torna per quell'anno, anche
// dopo la ristampa confermata di un anno prima) e label_sheet.counter_reset con il numero di prima
// (un numero, 0 se illeggibile o mancante) e l'anno. Il permesso lo controlla la rotta.
func (b *Buste) AzzeraEtichette(ctx context.Context, nuovoAnno bool, anno int64, c audit.Contesto) error {
	return b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		v, _, err := b.impostazione(ctx, tx, ChiaveUltimaEtichetta)
		if err != nil {
			return err
		}
		d := map[string]any{"previous": interoSalvato(v, 0), "new_year": nuovoAnno, "year": anno}
		if err := b.salvaImpostazione(ctx, tx, ChiaveUltimaEtichetta, "0", ora); err != nil {
			return err
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveAnnoEtichette, strconv.FormatInt(anno, 10), ora); err != nil {
			return err
		}
		return b.reg.Scrivi(ctx, tx, c, "label_sheet.counter_reset", "label_sheet", "counter", "ok", d)
	})
}
