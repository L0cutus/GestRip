package buste

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"modernc.org/sqlite"
)

// L'anagrafica delle oreficerie clienti (slice 086, ORE-01…03), come Workshop.list_external_oreficerie/0,
// create_oreficeria/2 e update_oreficeria/3 dello standalone (workshop.ex:56-105), con lo schema
// Oreficeria (nome ripulito, telefono normalizzato da Workshop.Phone, nome unico): le stesse regole degli
// artigiani (artigiani.go), con una riga in più da proteggere. La riga 1, «il negozio» stesso, la porta
// l'import: non si elenca e non si modifica (`id > 1` è «esterna», come Oreficeria.is_b2b/1). Non esiste
// un'eliminazione (ORE-03; quella voluta dopo la v1 è un'altra funzione). Ogni scrittura e la sua riga
// d'audit (senza dati) stanno in una transazione: se l'audit non si scrive, niente cambia (Ecto.Multi).

// OreficeriaCliente è una riga dell'anagrafica (la scheda ha la sua Oreficeria, quella della busta). Telefono "" è NULL.
type OreficeriaCliente struct {
	ID       int64
	Nome     string
	Telefono string
}

// DatiOreficeria sono i campi del modulo (oreficeria[name], oreficeria[phone]) come arrivano; nil è un
// campo che manca e vale NULL come in Oreficeria.normalize_attrs/1: il nome è obbligatorio, il
// telefono si cancella.
type DatiOreficeria struct {
	Nome, Telefono *string
}

// I rifiuti dell'anagrafica, con i testi di OreficerieLive.
const (
	ErrNomeOreficeria       Rifiuto = "Il nome è obbligatorio."
	ErrOreficeriaNonTrovata Rifiuto = "Oreficeria non trovata o non modificabile."
)

// OreficeriaDoppia è il nome già usato (unique_constraint alla creazione, LOWER alla modifica): il
// messaggio nomina il nome ripulito, come get_field(changeset, :name).
type OreficeriaDoppia struct{ Nome string }

func (e *OreficeriaDoppia) Error() string {
	return `Esiste già un'oreficeria con il nome "` + e.Nome + `".`
}

// campiOreficeria sono nome e telefono come li scrive il changeset: gli stessi dell'artigiano (nome
// ripulito e obbligatorio, telefono normalizzato o NULL), con il rifiuto dell'oreficeria.
func campiOreficeria(d DatiOreficeria) (string, any, error) {
	nome, telefono, err := campiArtigiano(DatiArtigiano(d))
	if err != nil {
		return "", nil, ErrNomeOreficeria
	}
	return nome, telefono, nil
}

// Oreficerie è list_external_oreficerie/0: le oreficerie clienti (id > 1), per nome e id.
func (b *Buste) Oreficerie(ctx context.Context) ([]OreficeriaCliente, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT id, nome, telefono FROM oreficerie WHERE id > 1 ORDER BY nome, id`)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var oo []OreficeriaCliente
	for rr.Next() {
		var o OreficeriaCliente
		var tel sql.NullString
		if err := rr.Scan(&o.ID, &o.Nome, &tel); err != nil {
			return nil, err
		}
		o.Telefono = tel.String
		oo = append(oo, o)
	}
	return oo, rr.Err()
}

// nomeOreficeriaUsato è il doppione dell'indice unico (oreficerie.nome), che vede anche la riga 1.
func nomeOreficeriaUsato(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqliteUnico && strings.Contains(se.Error(), "oreficerie.nome")
}

func clienteOreficeriaDi(id int64, nome string, telefono any) OreficeriaCliente {
	o := OreficeriaCliente{ID: id, Nome: nome}
	if t, ok := telefono.(string); ok {
		o.Telefono = t
	}
	return o
}

// CreaOreficeria è create_oreficeria/2: il doppione lo dice l'indice unico, a maiuscole esatte.
func (b *Buste) CreaOreficeria(ctx context.Context, d DatiOreficeria, c audit.Contesto) (OreficeriaCliente, error) {
	nome, telefono, err := campiOreficeria(d)
	if err != nil {
		return OreficeriaCliente{}, err
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return OreficeriaCliente{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	res, err := tx.ExecContext(ctx, `INSERT INTO oreficerie (nome, telefono) VALUES (?, ?)`, nome, telefono)
	if nomeOreficeriaUsato(err) {
		return OreficeriaCliente{}, &OreficeriaDoppia{Nome: nome}
	}
	if err != nil {
		return OreficeriaCliente{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return OreficeriaCliente{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "oreficeria.created", "oreficeria", id, "ok", nil); err != nil {
		return OreficeriaCliente{}, err
	}
	if err := tx.Commit(); err != nil {
		return OreficeriaCliente{}, err
	}
	return clienteOreficeriaDi(id, nome, telefono), nil
}

// ModificaOreficeria è update_oreficeria/3: l'oreficeria riletta nella transazione (la riga 1 e un id
// che non c'è sono lo stesso «non trovata o non modificabile»), il doppione cercato senza distinguere
// le maiuscole fra le altre oreficerie esterne (case_insensitive_duplicate?/4 con external_oreficerie:
// LOWER di SQLite, solo ASCII, e la riga 1 non conta), poi la riscrittura di nome e telefono; il nome
// uguale a un altro a maiuscole esatte, compreso quello del negozio, lo ferma l'indice unico.
func (b *Buste) ModificaOreficeria(ctx context.Context, id int64, d DatiOreficeria, c audit.Contesto) (OreficeriaCliente, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return OreficeriaCliente{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM oreficerie WHERE id = ? AND id > 1`, id).Scan(&n); err != nil {
		return OreficeriaCliente{}, err
	}
	if n == 0 {
		return OreficeriaCliente{}, ErrOreficeriaNonTrovata
	}
	nome, telefono, err := campiOreficeria(d)
	if err != nil {
		return OreficeriaCliente{}, err
	}
	var doppi int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM oreficerie WHERE id > 1 AND id != ? AND LOWER(nome) = LOWER(?)`, id, nome).Scan(&doppi); err != nil {
		return OreficeriaCliente{}, err
	}
	if doppi > 0 {
		return OreficeriaCliente{}, &OreficeriaDoppia{Nome: nome}
	}
	_, err = tx.ExecContext(ctx, `UPDATE oreficerie SET nome = ?, telefono = ? WHERE id = ?`, nome, telefono, id)
	if nomeOreficeriaUsato(err) {
		return OreficeriaCliente{}, &OreficeriaDoppia{Nome: nome}
	}
	if err != nil {
		return OreficeriaCliente{}, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "oreficeria.updated", "oreficeria", id, "ok", nil); err != nil {
		return OreficeriaCliente{}, err
	}
	if err := tx.Commit(); err != nil {
		return OreficeriaCliente{}, err
	}
	return clienteOreficeriaDi(id, nome, telefono), nil
}
