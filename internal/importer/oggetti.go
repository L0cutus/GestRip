package importer

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// I trigger e le viste dello standalone a go-baseline come li tiene sqlite_schema di un database
// vero (quello decifrato della catena del banco, rigenerato con tools/schema-standalone/oggetti.py):
// la sorgente li deve avere uguali, testo compreso (OPS-01, Stefano 31/10/2026). La fixture .sql ha
// gli stessi oggetti con un'altra impaginazione (TestOggettiVeriComeLaFixture). Tabelle e colonne si
// confrontano con la mappa.
//
//go:embed testdata/standalone-go-baseline-oggetti.json
var oggettiRiferimento []byte

// oggettoSchema è un trigger o una vista di sqlite_schema: tipo, tabella (tbl_name) e testo.
type oggettoSchema struct {
	tipo, tabella, sql string
}

// chiaveOggetto: trigger e viste hanno nomi separati in SQLite (una vista può chiamarsi come un
// trigger), quindi la chiave è tipo e nome insieme.
type chiaveOggetto struct {
	tipo, nome string
}

// leggiOggetti dà trigger e viste di un database, per tipo e nome.
func leggiOggetti(ctx context.Context, q *sql.DB) (map[chiaveOggetto]oggettoSchema, error) {
	rows, err := q.QueryContext(ctx, `SELECT type, name, tbl_name, coalesce(sql, '') FROM sqlite_schema WHERE type IN ('trigger', 'view')`)
	if err != nil {
		return nil, err
	}
	m := map[chiaveOggetto]oggettoSchema{}
	for rows.Next() {
		var nome string
		var o oggettoSchema
		if err := rows.Scan(&o.tipo, &nome, &o.tabella, &o.sql); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m[chiaveOggetto{o.tipo, nome}] = o
	}
	return m, errors.Join(rows.Err(), rows.Close())
}

// oggettiDelRiferimento legge trigger e viste del riferimento, per tipo e nome.
func oggettiDelRiferimento() (map[chiaveOggetto]oggettoSchema, error) {
	return riferimentoDa(oggettiRiferimento)
}

// riferimentoDa legge un elenco di trigger e viste in JSON. Un nome ripetuto è un riferimento
// rovinato.
func riferimentoDa(b []byte) (map[chiaveOggetto]oggettoSchema, error) {
	var voci []struct {
		Tipo    string `json:"tipo"`
		Nome    string `json:"nome"`
		Tabella string `json:"tabella"`
		SQL     string `json:"sql"`
	}
	if err := json.Unmarshal(b, &voci); err != nil {
		return nil, fmt.Errorf("riferimento di trigger e viste: %w", err)
	}
	m := map[chiaveOggetto]oggettoSchema{}
	for _, v := range voci {
		k := chiaveOggetto{v.Tipo, v.Nome}
		if _, c := m[k]; c {
			return nil, fmt.Errorf("riferimento di trigger e viste: %s ripetuto", v.Nome)
		}
		m[k] = oggettoSchema{tipo: v.Tipo, tabella: v.Tabella, sql: v.SQL}
	}
	return m, nil
}

// controllaOggetti confronta trigger e viste della sorgente con il riferimento: un oggetto in più,
// in meno, di un altro tipo o con un altro testo è bloccante (schema/oggetto_diverso), con il nome
// e la tabella, mai il testo. ok è false se c'è una differenza.
func controllaOggetti(ctx context.Context, src *sql.DB, rep *Report) (ok bool, err error) {
	rif, err := oggettiDelRiferimento()
	if err != nil {
		return false, err
	}
	sor, err := leggiOggetti(ctx, src)
	if err != nil {
		return false, err
	}
	var nomi []chiaveOggetto
	for k := range rif {
		nomi = append(nomi, k)
	}
	for k := range sor {
		if _, c := rif[k]; !c {
			nomi = append(nomi, k)
		}
	}
	slices.SortFunc(nomi, func(a, b chiaveOggetto) int {
		return cmp.Or(strings.Compare(a.nome, b.nome), strings.Compare(a.tipo, b.tipo))
	})
	ok = true
	for _, n := range nomi {
		r, inRif := rif[n]
		s, inSor := sor[n]
		if inRif && inSor && r == s {
			continue
		}
		tab := s.tabella
		if !inSor {
			tab = r.tabella
		}
		rep.blocca(Voce{Codice: "schema/oggetto_diverso", Tabella: tab, Oggetto: n.nome})
		ok = false
	}
	return ok, nil
}
