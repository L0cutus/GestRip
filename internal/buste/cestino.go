package buste

import (
	"context"
	"strconv"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il cestino delle buste, prima parte (slice 136, T4.11; RIP-S-49, RIP-S-50, RIP-H-51 parte «cestino»), come
// Trash.discard/4, Trash.Config.retention_days/0 e Training.forget_repair/2 dello standalone a go-baseline:
//   - SpostaNelCestino: la sola scrittura condizionata `WHERE id = ? AND cancellata_il IS NULL` decide; una busta già
//     nel cestino è lo stesso ErrBustaNonTrovata di una che non c'è (chi chiama non deve sapere quale dei due). Nella
//     stessa transazione: cancellata_il e aggiornato_il adesso, il dettato raccolto della busta cancellato (D-110,
//     domanda L del piano: «Il dettato sparisce quando la busta va nel cestino»; non torna con il ripristino, come lo
//     standalone), la riga repair.trashed senza dati. Un errore qualsiasi (anche dell'audit: un contesto senza
//     attore non si scrive) lascia la busta dov'era. Le iscrizioni Telegram della busta le spegne il trigger
//     iscrizioni_telegram_spente_nel_cestino (migrazione 0014), nella stessa scrittura. Niente ruolo: per le buste
//     sposta titolare e dipendente (Trash.owner_only?(:repair) è false; domanda B del piano); il ruolo lo dice la rotta;
//   - la campanella: nello standalone Notifications.announce/0 dopo il commit; nel Go la campanella si legge a ogni
//     pagina (LeggiCampanella) e ha già il filtro del cestino, quindi la pagina dopo non conta più la busta;
//   - GiorniCestino: la finestra del cestino in giorni, per la frase della conferma («Resta recuperabile per X
//     giorni.»). La chiave è quella dello standalone (l'import la copia); assente, vuota, non un intero, o fuori da
//     1–365 vale 30, come retention_days/0. La pagina che la cambia è della slice 138.
//
// Il filtro del cestino è scritto a mano in ogni query sulle buste (cancellata_il IS NULL): lo controlla il test statico
// TestFiltroDelCestinoInOgniQuery di internal/db, con le eccezioni volute elencate lì.

// ChiaveGiorniCestino è la chiave della finestra del cestino (Trash.Config, @key).
const ChiaveGiorniCestino = "trash.retention_days"

// I giorni del cestino: il valore di base e i limiti (@default_days, @min_days, @max_days).
const (
	GiorniCestinoBase = 30
	GiorniCestinoMin  = 1
	GiorniCestinoMax  = 365
)

// azioneCestinata è la riga d'audit dello spostamento nel cestino ("#{family}.#{verb}").
const azioneCestinata = "repair.trashed"

// GiorniCestino è Trash.Config.retention_days/0.
func (b *Buste) GiorniCestino(ctx context.Context) (int64, error) {
	v, ok, err := b.impostazione(ctx, b.d.Lettura, ChiaveGiorniCestino)
	if err != nil {
		return 0, err
	}
	return giorniCestinoDa(v, ok), nil
}

// giorniCestinoDa è la lettura del valore salvato: fuori formato o fuori dai limiti vale la base. strconv.ParseInt in
// base 10 è Integer.parse/1 preso per intero: un segno facoltativo e solo cifre, niente spazi né «_».
func giorniCestinoDa(v string, ok bool) int64 {
	if !ok {
		return GiorniCestinoBase
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < GiorniCestinoMin || n > GiorniCestinoMax {
		return GiorniCestinoBase
	}
	return n
}

// SpostaNelCestino è Trash.discard(:repair, id, contesto): dà quanti dettati raccolti della busta ha cancellato (0 o 1).
// ErrBustaNonTrovata per un id non positivo, una busta che non c'è o già nel cestino; ogni errore non scrive niente.
func (b *Buste) SpostaNelCestino(ctx context.Context, id int64, c audit.Contesto) (int64, error) {
	if id < 1 {
		return 0, ErrBustaNonTrovata
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	res, err := tx.ExecContext(ctx, `UPDATE buste SET cancellata_il = ?1, aggiornato_il = ?1 WHERE id = ?2 AND cancellata_il IS NULL`, ora, id)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n != 1 {
		return 0, ErrBustaNonTrovata
	}
	// Training.forget_repair/2: il dettato va via adesso, non quando il cestino si svuota.
	res, err = tx.ExecContext(ctx, `DELETE FROM dettati_addestramento WHERE busta_id = ?`, id)
	if err != nil {
		return 0, err
	}
	dettati, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := b.reg.Scrivi(ctx, tx, c, azioneCestinata, "repair", id, "ok", nil); err != nil {
		return 0, err
	}
	if err := b.provaPrimaDelCommit(tx); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return dettati, nil
}
