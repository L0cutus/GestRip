package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"modernc.org/sqlite"
)

// L'anagrafica degli artigiani (slice 076, ART-01…04), come Workshop.list_artisans/0,
// create_artisan/2, update_artisan/3 e delete_artisan/2 dello standalone (workshop.ex:115-207),
// con lo schema Artisan (nome ripulito, telefono normalizzato da Workshop.Phone, nome unico). Stanno
// nelle buste perché le consegne, i rientri e i giri della T4.8 lavorano nelle stesse transazioni
// delle buste (il rientro chiama avvisaPronta, email_pronta.go). Ogni scrittura e la sua riga
// d'audit (senza dati, come insert_audit/5 con i metadati vuoti) stanno in una transazione: se
// l'audit non si scrive, niente cambia (Ecto.Multi).

// Artigiano è una riga dell'anagrafica. Telefono "" è NULL.
type Artigiano struct {
	ID       int64
	Nome     string
	Telefono string
}

// DatiArtigiano sono i campi del modulo (artisan[name], artisan[phone]) come arrivano; nil è un
// campo che manca, e un campo che manca vale NULL come in Artisan.normalize_attrs/1
// (Map.update/4 con nil): il nome è obbligatorio, il telefono si cancella.
type DatiArtigiano struct {
	Nome, Telefono *string
}

// I rifiuti dell'anagrafica, con i testi di OreficerieLive.
const (
	ErrNomeArtigiano       Rifiuto = "Il nome è obbligatorio."
	ErrArtigianoNonTrovato Rifiuto = "Artigiano non trovato."
)

// ArtigianoDoppio è il nome già usato (unique_constraint alla creazione, LOWER alla modifica): il
// messaggio nomina il nome ripulito, come get_field(changeset, :name).
type ArtigianoDoppio struct{ Nome string }

func (e *ArtigianoDoppio) Error() string {
	return `Esiste già un artigiano con il nome "` + e.Nome + `".`
}

// ArtigianoInUso è {:in_use, count}: l'artigiano ha delle buste, anche nel cestino.
type ArtigianoInUso struct{ Buste int }

func (e *ArtigianoInUso) Error() string {
	if e.Buste == 1 {
		return "Impossibile eliminare: questo artigiano ha 1 riparazione associata."
	}
	return fmt.Sprintf("Impossibile eliminare: questo artigiano ha %d riparazioni associate.", e.Buste)
}

// ArtigianoConConsegne è {:has_handovers, count}: l'artigiano ha delle consegne, anche vuote.
type ArtigianoConConsegne struct{ Consegne int }

func (e *ArtigianoConConsegne) Error() string {
	if e.Consegne == 1 {
		return "Impossibile eliminare: questo artigiano ha 1 sessione di consegna."
	}
	return fmt.Sprintf("Impossibile eliminare: questo artigiano ha %d sessioni di consegna.", e.Consegne)
}

// cifrePython sono i caratteri che str.isdigit di Python accetta oltre a \p{Nd}
// (@python_digit_ranges di Workshop.Phone): apici, pedici, cifre cerchiate e affini.
var cifrePython = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x00B2, Hi: 0x00B3, Stride: 1}, {Lo: 0x00B9, Hi: 0x00B9, Stride: 1},
		{Lo: 0x1369, Hi: 0x1371, Stride: 1}, {Lo: 0x19DA, Hi: 0x19DA, Stride: 1},
		{Lo: 0x2070, Hi: 0x2070, Stride: 1}, {Lo: 0x2074, Hi: 0x2079, Stride: 1},
		{Lo: 0x2080, Hi: 0x2089, Stride: 1}, {Lo: 0x2460, Hi: 0x2468, Stride: 1},
		{Lo: 0x2474, Hi: 0x247C, Stride: 1}, {Lo: 0x2488, Hi: 0x2490, Stride: 1},
		{Lo: 0x24EA, Hi: 0x24EA, Stride: 1}, {Lo: 0x24F5, Hi: 0x24FD, Stride: 1},
		{Lo: 0x24FF, Hi: 0x24FF, Stride: 1}, {Lo: 0x2776, Hi: 0x277E, Stride: 1},
		{Lo: 0x2780, Hi: 0x2788, Stride: 1}, {Lo: 0x278A, Hi: 0x2792, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x10A40, Hi: 0x10A43, Stride: 1}, {Lo: 0x10E60, Hi: 0x10E68, Stride: 1},
		{Lo: 0x11052, Hi: 0x1105A, Stride: 1}, {Lo: 0x1F100, Hi: 0x1F10A, Stride: 1},
	},
}

// NormalizzaTelefono è Workshop.Phone.normalize/1 (normalizza_telefono del Flask): tolti gli spazi
// ai bordi, un «+» in testa resta e poi solo le cifre di Python; "" se non resta nessuna cifra.
func NormalizzaTelefono(s string) string {
	s = strings.TrimSpace(s)
	var cifre strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) || unicode.Is(cifrePython, r) {
			cifre.WriteRune(r)
		}
	}
	if cifre.Len() == 0 {
		return ""
	}
	if strings.HasPrefix(s, "+") {
		return "+" + cifre.String()
	}
	return cifre.String()
}

// campiArtigiano sono nome e telefono come li scrive il changeset: nome ripulito (Phone.trim_name/1),
// obbligatorio; telefono normalizzato o NULL.
func campiArtigiano(d DatiArtigiano) (string, any, error) {
	var nome string
	if d.Nome != nil {
		nome = strings.TrimSpace(*d.Nome)
	}
	if nome == "" {
		return "", nil, ErrNomeArtigiano
	}
	var telefono any
	if d.Telefono != nil {
		if t := NormalizzaTelefono(*d.Telefono); t != "" {
			telefono = t
		}
	}
	return nome, telefono, nil
}

func artigianoDi(id int64, nome string, telefono any) Artigiano {
	a := Artigiano{ID: id, Nome: nome}
	if t, ok := telefono.(string); ok {
		a.Telefono = t
	}
	return a
}

// Artigiani è list_artisans/0: tutta l'anagrafica, per nome e id.
func (b *Buste) Artigiani(ctx context.Context) ([]Artigiano, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT id, nome, telefono FROM artigiani ORDER BY nome, id`)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var aa []Artigiano
	for rr.Next() {
		var a Artigiano
		var tel sql.NullString
		if err := rr.Scan(&a.ID, &a.Nome, &tel); err != nil {
			return nil, err
		}
		a.Telefono = tel.String
		aa = append(aa, a)
	}
	return aa, rr.Err()
}

// nomeArtigianoUsato è il doppione dell'indice unico (artigiani.nome).
func nomeArtigianoUsato(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteUnico && strings.Contains(se.Error(), "artigiani.nome")
}

// CreaArtigiano è create_artisan/2: il doppione lo dice l'indice unico, a maiuscole esatte.
func (b *Buste) CreaArtigiano(ctx context.Context, d DatiArtigiano, c audit.Contesto) (Artigiano, error) {
	nome, telefono, err := campiArtigiano(d)
	if err != nil {
		return Artigiano{}, err
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Artigiano{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	res, err := tx.ExecContext(ctx, `INSERT INTO artigiani (nome, telefono) VALUES (?, ?)`, nome, telefono)
	if nomeArtigianoUsato(err) {
		return Artigiano{}, &ArtigianoDoppio{Nome: nome}
	}
	if err != nil {
		return Artigiano{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Artigiano{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "artisan.created", "artisan", id, "ok", nil); err != nil {
		return Artigiano{}, err
	}
	if err := tx.Commit(); err != nil {
		return Artigiano{}, err
	}
	return artigianoDi(id, nome, telefono), nil
}

// ModificaArtigiano è update_artisan/3: l'artigiano riletto nella transazione, il doppione cercato
// senza distinguere le maiuscole fra gli altri (case_insensitive_duplicate?/4, LOWER di SQLite:
// solo ASCII), poi la riscrittura di nome e telefono.
func (b *Buste) ModificaArtigiano(ctx context.Context, id int64, d DatiArtigiano, c audit.Contesto) (Artigiano, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Artigiano{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM artigiani WHERE id = ?`, id).Scan(&n); err != nil {
		return Artigiano{}, err
	}
	if n == 0 {
		return Artigiano{}, ErrArtigianoNonTrovato
	}
	nome, telefono, err := campiArtigiano(d)
	if err != nil {
		return Artigiano{}, err
	}
	var doppi int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM artigiani WHERE id != ? AND LOWER(nome) = LOWER(?)`, id, nome).Scan(&doppi); err != nil {
		return Artigiano{}, err
	}
	if doppi > 0 {
		return Artigiano{}, &ArtigianoDoppio{Nome: nome}
	}
	_, err = tx.ExecContext(ctx, `UPDATE artigiani SET nome = ?, telefono = ? WHERE id = ?`, nome, telefono, id)
	if nomeArtigianoUsato(err) {
		return Artigiano{}, &ArtigianoDoppio{Nome: nome}
	}
	if err != nil {
		return Artigiano{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "artisan.updated", "artisan", id, "ok", nil); err != nil {
		return Artigiano{}, err
	}
	if err := tx.Commit(); err != nil {
		return Artigiano{}, err
	}
	return artigianoDi(id, nome, telefono), nil
}

// EliminaArtigiano è delete_artisan/2: rifiutato con il conteggio se ha delle buste (with_discarded:
// anche quelle nel cestino), poi se ha delle consegne (anche vuote); altrimenti eliminato.
func (b *Buste) EliminaArtigiano(ctx context.Context, id int64, c audit.Contesto) error {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM artigiani WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrArtigianoNonTrovato
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM buste WHERE artigiano_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ArtigianoInUso{Buste: n}
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM consegne_artigiani WHERE artigiano_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return &ArtigianoConConsegne{Consegne: n}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM artigiani WHERE id = ?`, id); err != nil {
		return err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "artisan.deleted", "artisan", id, "ok", nil); err != nil {
		return err
	}
	return tx.Commit()
}
