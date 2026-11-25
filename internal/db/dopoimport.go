package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Interrogatore è quello che serve a BloccatoDopoImport e BackupVerificatoDopo: una transazione o un
// *sql.DB.
type Interrogatore interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// BloccatoDopoImport dice se il database viene da un import dallo standalone e non c'è ancora un
// backup verificato catturato dopo l'import: allora non si cancella niente di quello che l'import ha
// portato (PIANO T2, ADM-17, decisione di Stefano del 30/10/2026; slice 122, D-11). Lo usano la
// conservazione dell'audit, la coda delle cancellazioni dei media (media.Pulizia, D-55) e la scadenza dei
// dettati (buste.ScadenzaDettati, D-110): dal primo backup verificato sulla chiavetta dopo importato_il
// ripartono tutte e tre.
//
// È fatta per stare dentro una transazione o un lucchetto: **non rilegge mai l'archivio**. Si fida solo
// della memoria riempita da PreparaBloccoImport (stessa riga del registro, stesso file non cambiato, meno di
// ValiditaRicontrollo); senza memoria fresca dice bloccato. I chiamanti chiamano PreparaBloccoImport
// prima di aprire la transazione o di prendere il lucchetto.
//
// Fallisce chiusa: con un errore (segno dell'import illeggibile, registro illeggibile, orologio che non
// torna, archivio cambiato o illeggibile) dice bloccato, e l'errore.
func BloccatoDopoImport(ctx context.Context, q Interrogatore) (bool, error) {
	ok, err := verificatoDopoImport(ctx, q, soloMemoria)
	if err != nil {
		return true, err
	}
	return !ok, nil
}

// PreparaBloccoImport, se il database viene da un import, ricontrolla l'ultimo backup verificato dopo
// l'import e ne riempie la memoria (memoriaImport) per BloccatoDopoImport: rilegge tutto l'archivio (qualche
// GB) solo se la memoria non è fresca (meno di ValiditaRicontrollo-AnticipoRicontrollo, stessa riga, stesso
// file), e un solo ricalcolo alla volta per processo: chi arriva mentre un altro ricalcola aspetta e usa il
// suo esito. Va chiamata fuori da ogni transazione e lucchetto. Un ricalcolo fallito (archivio cambiato o
// illeggibile) svuota la memoria. Un errore che arriva prima del ricalcolo (registro, orologio, Lstat, ctx)
// lascia la memoria com'era, anche fresca: non sblocca niente di sbagliato, perché BloccatoDopoImport rifà
// gli stessi controlli (registro, orologio, Lstat, byte, stessa riga e stesso file) prima di fidarsi.
func PreparaBloccoImport(ctx context.Context, q Interrogatore) error {
	_, err := verificatoDopoImport(ctx, q, riempiMemoria)
	return err
}

// verificatoDopoImport dice se non c'è niente da bloccare: nessun import, o un backup verificato dopo l'import.
func verificatoDopoImport(ctx context.Context, q Interrogatore, modo modoRicontrollo) (bool, error) {
	var il string
	err := q.QueryRowContext(ctx, `SELECT importato_il FROM import_standalone`).Scan(&il)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	t, err := time.Parse(FormatoIstante, il)
	if err != nil {
		return false, fmt.Errorf("segno dell'import: istante illeggibile: %w", err)
	}
	return backupVerificatoDopo(ctx, q, t, &memoriaImport, modo)
}
