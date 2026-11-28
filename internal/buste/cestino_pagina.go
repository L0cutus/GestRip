package buste

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina del cestino e il ripristino (slice 137, T4.11; RIP-S-51, RIP-S-52), come Trash.list/1, Trash.restore/4 e
// Trash.owner_only?/1 dello standalone a go-baseline:
//   - Cestino: tutto quello che è nel cestino, dal più recente (cancellata_il decrescente; a pari istante l'id
//     crescente, l'ordine stabile di Enum.sort_by/3 sulla lettura per indice), con i giorni rimasti. La finestra si
//     legge una volta sola per tutta la pagina (load/1 di TrashLive), con la stessa lettura della 136 (GiorniCestino);
//   - i giorni rimasti sono countdown/3: giorni di calendario del negozio (clock.DataDi, fuso di Roma), non periodi di
//     24 ore; trascorsi = oggi − giorno del cestinamento, mai sotto zero (un istante nel futuro, un orologio tornato
//     indietro); rimasti = finestra − trascorsi, mai sotto zero. Quindi mai negativi e mai oltre la finestra. Il cambio
//     dell'ora legale non conta: si contano date, non ore;
//   - RipristinaDalCestino: la sola scrittura condizionata `WHERE id = ? AND cancellata_il IS NOT NULL` decide, e tocca
//     solo cancellata_il (a NULL) e aggiornato_il (adesso). Una busta che non c'è, fuori dal cestino, un id non
//     positivo o una famiglia sconosciuta sono lo stesso ErrNonNelCestino e non scrivono niente. Nella stessa
//     transazione la riga «<famiglia>.restored» senza dati; un errore qualsiasi (anche dell'audit) non ripristina.
//     Nessun limite di tempo: anche dopo la finestra. Il dettato cancellato al cestinamento non torna (domanda L), le
//     iscrizioni Telegram spente dal trigger della 0014 restano spente, come nello standalone.
//
// Pronto per le vendite (domanda K del piano T4.11, VEN-18 nella T4.12): il cestino è un elenco di famiglie
// (famiglieCestino). Ogni famiglia porta il suo nome (quello dello standalone: "repair", "sale"), la sua lettura,
// la sua scrittura del ripristino e la regola del ruolo (SoloTitolare, Trash.owner_only?/1). La T4.12 aggiunge la
// famiglia "sale" (vendite.cancellata_il c'è già dalla 0009) con la sua riga qui, la sua rotta di ripristino del
// titolare e l'etichetta «Vendita» nella pagina; né l'ordine, né i giorni, né la pagina cambiano.

// FamigliaCestino è una famiglia del cestino, con il nome dello standalone (Trash.names/0, l'id dei pulsanti
// «restore-<famiglia>-<id>» e la risorsa dell'audit).
type FamigliaCestino string

// FamigliaBuste sono le buste (:repair). Le vendite (:sale) arrivano con la T4.12.
const FamigliaBuste FamigliaCestino = "repair"

// ErrNonNelCestino è il rifiuto del ripristino: niente da ripristinare (famiglia sconosciuta, id non positivo, riga che
// non c'è o non è nel cestino). Lo standalone lo ignora in silenzio ({:noreply, socket}).
var ErrNonNelCestino = errors.New("buste: niente da ripristinare nel cestino")

// VoceCestino è una riga della pagina (la mappa di discarded/1 con days_left).
type VoceCestino struct {
	Famiglia      FamigliaCestino
	ID            int64
	CancellataIl  time.Time
	Etichetta     string // label: il numero visibile della busta (D-64)
	Dettaglio     string // detail: la descrizione della busta
	Stato         string // status, com'è salvato (lo standalone lo stampa così)
	GiorniRimasti int64  // days_left
}

// Cestino è la pagina: la finestra letta una volta e le voci, dal più recente.
type Cestino struct {
	Giorni int64
	Voci   []VoceCestino
}

// famigliaCestino è una famiglia del cestino (una voce di @schemas dello standalone).
type famigliaCestino struct {
	nome         FamigliaCestino
	soloTitolare bool // Trash.owner_only?/1
	elenca       func(b *Buste, ctx context.Context) ([]VoceCestino, error)
	ripristino   string // UPDATE condizionato: ?1 adesso, ?2 id
}

// sqlRipristinoBusta è la scrittura del ripristino di una busta (Trash.restore/4 con la guardia not is_nil(deleted_at)).
const sqlRipristinoBusta = `UPDATE buste SET cancellata_il = NULL, aggiornato_il = ?1 WHERE id = ?2 AND cancellata_il IS NOT NULL`

// famiglieCestino sono le famiglie, nell'ordine in cui si leggono (discarded(:repair) ++ discarded(:sale)).
var famiglieCestino = []famigliaCestino{
	{nome: FamigliaBuste, soloTitolare: false, elenca: (*Buste).busteNelCestino, ripristino: sqlRipristinoBusta},
}

// famigliaDi è la famiglia con quel nome; ok è false per un nome che il cestino non conosce.
func famigliaDi(nome FamigliaCestino) (famigliaCestino, bool) {
	for _, f := range famiglieCestino {
		if f.nome == nome {
			return f, true
		}
	}
	return famigliaCestino{}, false
}

// FamiglieCestino sono i nomi delle famiglie del cestino (Trash.names/0): la faccia web ne fa le rotte.
func FamiglieCestino() []FamigliaCestino {
	out := make([]FamigliaCestino, 0, len(famiglieCestino))
	for _, f := range famiglieCestino {
		out = append(out, f.nome)
	}
	return out
}

// SoloTitolare è Trash.owner_only?/1: se ripristinare (e buttare) una riga di questa famiglia è solo del titolare. Una
// sola risposta per il pulsante della pagina e per la rotta. Una famiglia sconosciuta è del titolare (la più stretta).
func (f FamigliaCestino) SoloTitolare() bool {
	g, ok := famigliaDi(f)
	return !ok || g.soloTitolare
}

// Cestino è Trash.list/1 con la finestra di load/1: le voci di tutte le famiglie, dalla più recente, con i giorni
// rimasti contati sul calendario del negozio di oggi.
func (b *Buste) Cestino(ctx context.Context) (Cestino, error) {
	finestra, err := b.GiorniCestino(ctx)
	if err != nil {
		return Cestino{}, err
	}
	adesso, _ := b.adesso()
	oggi := clock.DataDi(adesso)
	var voci []VoceCestino
	for _, f := range famiglieCestino {
		vv, err := f.elenca(b, ctx)
		if err != nil {
			return Cestino{}, err
		}
		voci = append(voci, vv...)
	}
	sort.SliceStable(voci, func(i, j int) bool { return voci[i].CancellataIl.After(voci[j].CancellataIl) })
	for i := range voci {
		voci[i].GiorniRimasti = giorniRimasti(oggi, voci[i].CancellataIl, finestra)
	}
	return Cestino{Giorni: finestra, Voci: voci}, nil
}

// giorniRimasti è countdown/3: max(finestra − max(oggi − giorno del cestinamento, 0), 0), in giorni di calendario del
// negozio.
func giorniRimasti(oggi clock.Data, cancellata time.Time, finestra int64) int64 {
	trascorsi := max(int64(oggi.Sub(clock.DataDi(cancellata))), 0)
	return max(finestra-trascorsi, 0)
}

// busteNelCestino è discarded(:repair): le buste nel cestino, con il numero visibile (D-64), la descrizione e lo
// stato. Mai il token del link del cliente.
func (b *Buste) busteNelCestino(ctx context.Context) ([]VoceCestino, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT id, numero, codice_qr, descrizione, stato, cancellata_il FROM buste
		WHERE cancellata_il IS NOT NULL ORDER BY cancellata_il DESC, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rr.Close() }()
	var out []VoceCestino
	for rr.Next() {
		var (
			v              VoceCestino
			numero, quando string
			qr             *string
		)
		if err := rr.Scan(&v.ID, &numero, &qr, &v.Dettaglio, &v.Stato, &quando); err != nil {
			return nil, err
		}
		if v.CancellataIl, err = time.Parse(db.FormatoIstante, quando); err != nil {
			return nil, err
		}
		codice := ""
		if qr != nil {
			codice = *qr
		}
		v.Famiglia = FamigliaBuste
		v.Etichetta, _ = NumeroVisibile(codice, numero)
		out = append(out, v)
	}
	return out, rr.Err()
}

// RipristinaDalCestino è Trash.restore(famiglia, id, contesto): ErrNonNelCestino se non c'è niente da ripristinare;
// ogni errore non scrive niente. Il ruolo lo decide la rotta (SoloTitolare).
func (b *Buste) RipristinaDalCestino(ctx context.Context, famiglia FamigliaCestino, id int64, c audit.Contesto) error {
	f, ok := famigliaDi(famiglia)
	if !ok || id < 1 {
		return ErrNonNelCestino
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	res, err := tx.ExecContext(ctx, f.ripristino, ora, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNonNelCestino
	}
	if err := b.reg.Scrivi(ctx, tx, c, string(f.nome)+".restored", string(f.nome), id, "ok", nil); err != nil {
		return err
	}
	if err := b.provaPrimaDelCommit(tx); err != nil {
		return err
	}
	return tx.Commit()
}
