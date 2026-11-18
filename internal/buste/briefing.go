package buste

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Il lato del dominio del briefing del mattino (Jobs.Briefing dello standalone, JOB-10; slice 097): le cinque
// domande, ognuna con al più quante righe, nell'ordine dello standalone. Solo letture, nessun timbro.
//
// Le buste nel cestino non ci sono, in nessuna sezione: nello standalone le cinque query passano da
// Repo.prepare_query/3, che aggiunge deleted_at IS NULL alla busta, anche a late_query/3 della quinta.
//
//  1. Pronte da avvisare: riparazioni pronte (condizionePronta), mai avvisate, senza data di consegna; dalla data di
//     pronta più vecchia, quelle senza data in fondo, poi per id.
//  2. Compro oro da avvisare: compro oro valutati, mai avvisati, senza data di consegna; dal più nuovo (id).
//  3. In ritardo: data promessa prima di oggi, stato non chiuso e non pronta o valutata, senza data di consegna.
//  4. In scadenza 3gg: le stesse, con la data promessa da oggi a oggi più tre, compresi.
//  5. Fuori bottega: late_query/3 della pagina dei solleciti (busteInRitardoDove) con la soglia del briefing, dalla
//     uscita più vecchia, qualunque sia l'artigiano.

// VoceBriefing è una busta di una sezione del briefing.
type VoceBriefing struct {
	ID          int64
	Numero      string
	CodiceQR    *string
	Cliente     string
	Descrizione string
	Prevista    string // aaaa-mm-gg, "" se manca (ritardo e scadenza)
	Artigiano   string // fuori bottega
	GiorniFuori int64  // fuori bottega: blocchi interi di 86 400 s dall'uscita
}

// LetturaBriefing sono le cinque sezioni del briefing, ognuna con al più quante righe (lo standalone ne chiede una
// più di quelle che mostra, per sapere se ce ne sono altre). FuoriBottega è nil con la soglia 0.
type LetturaBriefing struct {
	Pronte, CompraOro, InRitardo, InScadenza, FuoriBottega []VoceBriefing
}

const (
	// colonneBriefing sono le colonne di select_bag/1.
	colonneBriefing = `SELECT b.id, b.numero, b.codice_qr, b.nome_cliente, b.descrizione, coalesce(b.data_prevista, '') FROM buste b WHERE b.cancellata_il IS NULL AND `
	// daAvvisareBriefing è ready/1 senza il compro_oro (il secondo argomento).
	daAvvisareBriefing = condizionePronta + ` AND b.compro_oro = ? AND b.avviso_pronta_il IS NULL AND b.data_consegna IS NULL`
	// apertaConDataSQL è open_with_date/0: data promessa, stato non chiuso e non nel suo «pronto» (@not_late),
	// non ritirata.
	apertaConDataSQL = `b.data_prevista IS NOT NULL AND b.stato NOT IN ` + statiChiusiSQL + ` AND b.stato NOT IN ('pronta', 'valutata')
		AND b.data_consegna IS NULL`
)

// LeggiBriefing legge le cinque sezioni con q (la transazione del lavoro): «oggi» è il giorno del negozio di adesso,
// giorniFuori la soglia di fuori bottega (0: la sezione non c'è), quante le righe al più per sezione.
func LeggiBriefing(ctx context.Context, q interrogatore, adesso time.Time, giorniFuori int64, quante int) (LetturaBriefing, error) {
	var (
		l   LetturaBriefing
		err error
	)
	oggi := clock.DataDi(adesso)
	t3 := time.Date(oggi.Anno, oggi.Mese, oggi.Giorno+3, 12, 0, 0, 0, time.UTC) // Date.add(today, 3)
	fra3 := clock.Data{Anno: t3.Year(), Mese: t3.Month(), Giorno: t3.Day()}
	if l.Pronte, err = vociBriefing(ctx, q, daAvvisareBriefing+` ORDER BY b.data_pronta IS NULL, b.data_pronta, b.id LIMIT ?`, 0, quante); err != nil {
		return LetturaBriefing{}, err
	}
	if l.CompraOro, err = vociBriefing(ctx, q, daAvvisareBriefing+` ORDER BY b.id DESC LIMIT ?`, 1, quante); err != nil {
		return LetturaBriefing{}, err
	}
	if l.InRitardo, err = vociBriefing(ctx, q, apertaConDataSQL+` AND b.data_prevista < ? ORDER BY b.data_prevista, b.id LIMIT ?`,
		oggi.String(), quante); err != nil {
		return LetturaBriefing{}, err
	}
	if l.InScadenza, err = vociBriefing(ctx, q, apertaConDataSQL+` AND b.data_prevista >= ? AND b.data_prevista <= ? ORDER BY b.data_prevista, b.id LIMIT ?`,
		oggi.String(), fra3.String(), quante); err != nil {
		return LetturaBriefing{}, err
	}
	if giorniFuori > 0 {
		if l.FuoriBottega, err = fuoriBottegaBriefing(ctx, q, adesso, giorniFuori, quante); err != nil {
			return LetturaBriefing{}, err
		}
	}
	return l, nil
}

func vociBriefing(ctx context.Context, q interrogatore, dove string, args ...any) ([]VoceBriefing, error) {
	rr, err := q.QueryContext(ctx, colonneBriefing+dove, args...)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []VoceBriefing
	for rr.Next() {
		var v VoceBriefing
		if err := rr.Scan(&v.ID, &v.Numero, &v.CodiceQR, &v.Cliente, &v.Descrizione, &v.Prevista); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rr.Err()
}

// fuoriBottegaBriefing è out_of_shop/2: le buste della pagina dei solleciti con la soglia del briefing, fuori dal
// cestino, ordinate per uscita e id (exclude(:order_by)), le prime quante.
func fuoriBottegaBriefing(ctx context.Context, q interrogatore, adesso time.Time, giorni int64, quante int) ([]VoceBriefing, error) {
	gg, _, err := busteInRitardoDove(ctx, q, adesso, giorni, 0, ` AND b.cancellata_il IS NULL`)
	if err != nil {
		return nil, err
	}
	var out []VoceBriefing
	tempi := map[int64]time.Time{}
	for _, g := range gg {
		for _, x := range g.Buste {
			out = append(out, VoceBriefing{ID: x.ID, Numero: x.Numero, CodiceQR: x.CodiceQR, Cliente: x.Cliente,
				Descrizione: x.Descrizione, Artigiano: g.Artigiano, GiorniFuori: x.GiorniFuori})
			tempi[x.ID] = x.Uscita
		}
	}
	slices.SortFunc(out, func(a, b VoceBriefing) int {
		return cmp.Or(tempi[a.ID].Compare(tempi[b.ID]), cmp.Compare(a.ID, b.ID))
	})
	if len(out) > quante {
		out = out[:quante]
	}
	return out, nil
}
