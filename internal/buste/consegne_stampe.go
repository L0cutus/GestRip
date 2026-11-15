package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Le etichette della consegna all'artigiano (slice 078, ART-12), come Workshop.list_handover_label_bags/1,
// HandoverLabels e Workshop.record_handover_labels/3 dello standalone, e la stampa diretta con la conferma
// (D-09, Stefano 29/10/2026), solo del Go:
//   - le buste sono quelle della consegna lette adesso (una aggiunta dopo una stampa c'è, una tolta no, una
//     rientrata resta: il rientro non scollega), fuori dal cestino, per numero: l'ordine del testo di
//     numero, come order_by bag_number dello standalone («10/26» prima di «2/26»), poi l'id;
//   - ogni etichetta è quella della busta (leggiEtichetta) con «→» l'artigiano della consegna, uguale su
//     tutte le pagine, non quello di ogni busta;
//   - una consegna che non c'è o senza buste è ErrConsegnaVuota (il 404 dello standalone, uguale per i due
//     casi); il PDF non cambia né la consegna né le buste;
//   - EtichetteDellaConsegna scrive handover.labels_generated {"bags": N} solo a PDF fatto, prima che i
//     byte escano; EtichetteDaStampare non scrive niente: dopo il lavoro accettato, StampaConsegnaAccettata
//     conferma come ConfermaConsegna e scrive handover.labels_printed (le buste stampate, coda, lavoro) e
//     handover.confirmed nella stessa transazione, solo se c'è almeno una busta da timbrare. Il lavoro sta fuori dalla transazione (non si disfa).

// ErrConsegnaVuota è la consegna che non c'è o senza buste fuori dal cestino.
const ErrConsegnaVuota Rifiuto = "Consegna vuota o non trovata"

// ErrNienteDaConfermare (solo Go, review della 078): la stampa diretta su una consegna senza buste ancora da
// timbrare (già confermata). Non si scrive una seconda handover.confirmed da 0.
const ErrNienteDaConfermare Rifiuto = "Nessuna busta da confermare"

// NomeFileEtichetteConsegna è filename/1 di HandoverLabels: «etichette_consegna_<id>.pdf».
func NomeFileEtichetteConsegna(consegna int64) string {
	return "etichette_consegna_" + strconv.FormatInt(consegna, 10) + ".pdf"
}

// etichetteDellaConsegna legge le etichette della consegna, con l'artigiano della consegna.
func (b *Buste) etichetteDellaConsegna(ctx context.Context, consegna int64) ([]DatiEtichetta, error) {
	var artigiano string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT a.nome FROM consegne_artigiani c JOIN artigiani a ON a.id = c.artigiano_id
		WHERE c.id = ?`, consegna).Scan(&artigiano)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConsegnaVuota
	}
	if err != nil {
		return nil, err
	}
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.data_prevista_testo, b.data_prevista, b.stato,
		b.descrizione FROM buste b WHERE b.consegna_artigiano_id = ? AND b.cancellata_il IS NULL ORDER BY b.numero, b.id`, consegna)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []DatiEtichetta
	for rr.Next() {
		var e DatiEtichetta
		var qr, consTesto, consData sql.NullString
		var stato string
		if err := rr.Scan(&e.ID, &e.Numero, &qr, &consTesto, &consData, &stato, &e.Descrizione); err != nil {
			return nil, err
		}
		b.completaEtichetta(&e, qr, consTesto, consData, stato, artigiano)
		out = append(out, e)
	}
	if err := rr.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrConsegnaVuota
	}
	return out, nil
}

// EtichetteDellaConsegna fa con fai il PDF delle etichette della consegna e, solo se è fatto, scrive
// handover.labels_generated con il numero delle buste per chi lo chiede (c). Errori: ErrConsegnaVuota,
// ErrPDFNonFatto (anche se la riga non si scrive: il PDF non esce).
func (b *Buste) EtichetteDellaConsegna(ctx context.Context, consegna int64, c audit.Contesto, fai func([]DatiEtichetta) ([]byte, error)) ([]byte, error) {
	ee, err := b.etichetteDellaConsegna(ctx, consegna)
	if err != nil {
		return nil, err
	}
	pdf, err := fai(ee)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	if err := b.reg.Registra(ctx, c, "handover.labels_generated", "handover", strconv.FormatInt(consegna, 10), "ok",
		map[string]any{"bags": len(ee)}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	return pdf, nil
}

// EtichetteDaStampare fa il PDF delle etichette della consegna per la stampa diretta, senza riga d'audit, e
// dà gli id delle buste stampate, nell'ordine delle pagine. Errori come EtichetteDellaConsegna.
func (b *Buste) EtichetteDaStampare(ctx context.Context, consegna int64, fai func([]DatiEtichetta) ([]byte, error)) ([]byte, []int64, error) {
	ee, err := b.etichetteDellaConsegna(ctx, consegna)
	if err != nil {
		return nil, nil, err
	}
	pdf, err := fai(ee)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	ids := make([]int64, len(ee))
	for i, e := range ee {
		ids[i] = e.ID
	}
	return pdf, ids, nil
}

// datiStampaConsegna sono i dati di handover.labels_printed: le buste stampate (quante e quali: quelle del
// PDF, non quelle che la conferma timbra, che possono cambiare mentre lp lavora), la coda e il lavoro.
func datiStampaConsegna(stampate []int64, coda, lavoro string) map[string]any {
	return map[string]any{"bags": len(stampate), "repair_ids": stampate, "queue": coda, "job": lavoro}
}

// StampaConsegnaAccettata è la conferma dopo che la stampante ha accettato il lavoro (coda, lavoro) con le
// buste stampate: in una transazione handover.labels_printed e la conferma di ConfermaConsegna (le uscite e
// handover.confirmed). Dà quante buste ha timbrato. Una consegna chiusa o che non c'è è
// ErrConsegnaNonDisponibile, una senza buste ancora da timbrare (confermata da un altro mentre lp lavorava)
// ErrNienteDaConfermare: niente si scrive; se una riga non si scrive non si salva niente.
func (b *Buste) StampaConsegnaAccettata(ctx context.Context, consegna int64, stampate []int64, coda, lavoro string, c audit.Contesto) (int, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if _, err := consegnaAperta(ctx, tx, consegna); err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "handover.labels_printed", "handover", strconv.FormatInt(consegna, 10), "ok",
		datiStampaConsegna(stampate, coda, lavoro)); err != nil {
		return 0, err
	}
	n, err := b.confermaNellaTransazione(ctx, tx, consegna, c)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNienteDaConfermare // il rollback toglie le due righe
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// StampaConsegnaNonConfermata è la traccia di un lavoro accettato quando StampaConsegnaAccettata non si è
// chiusa: handover.labels_printed con esito not_confirmed, fuori da ogni transazione e senza garanzia (chi
// chiama, se fallisce anche questa, lo scrive nel registro).
func (b *Buste) StampaConsegnaNonConfermata(ctx context.Context, consegna int64, stampate []int64, coda, lavoro string, c audit.Contesto) error {
	return b.reg.Registra(ctx, c, "handover.labels_printed", "handover", strconv.FormatInt(consegna, 10), "not_confirmed",
		datiStampaConsegna(stampate, coda, lavoro))
}
