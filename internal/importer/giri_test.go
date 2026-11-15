package importer

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// inventario: ART-19
// L'import ricostruisce i giri dall'artigiano (slice 081) dall'audit e dalle buste che ha copiato: un giro
// chiuso per il rientro con la sua uscita (la conferma che nomina la busta) e la nota presente, un giro aperto
// per la busta fuori con l'uscita della sua conferma. Sulla scheda: il giro attuale dalle colonne e quello
// passato dalla tabella, come HandoverHistory.cycles/1.
func TestImportRicostruisceIGiri(t *testing.T) {
	_, o := preparaImport(t, func(c *sql.DB) {
		for _, q := range []string{
			`UPDATE repairs SET handed_over_at = '2026-09-10T08:00:00.000000Z', returned_from_artisan_at = '2026-09-12T08:00:00.000000Z',
				artisan_notes = 'Nota del giro attuale' WHERE id = 1`,
			`UPDATE repairs SET handed_over_at = '2026-10-01T08:00:00.000000Z', returned_from_artisan_at = NULL WHERE id = 2`,
			`INSERT INTO audit_events (actor, action, resource, resource_id, outcome, metadata, inserted_at) VALUES
				('titolare', 'handover.confirmed', 'handover', '1', 'ok', '{"handed_over":1,"repair_ids":[1]}', '2026-09-01T08:00:00.000000Z'),
				('titolare', 'repair.returned_from_artisan', 'repair', '1', 'ok', '{"handover_id":1,"artisan_notes":true}', '2026-09-05T08:00:00.000000Z'),
				('titolare', 'handover.confirmed', 'handover', '1', 'ok', '{"handed_over":1,"repair_ids":[2]}', '2026-10-01T08:00:00.000000Z')`,
		} {
			if _, err := c.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	})
	importa(t, o)
	d, err := db.Apri(o.Destinazione)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Chiudi(); err != nil {
			t.Error(err)
		}
	}()
	var righe string
	if err := d.Lettura.QueryRow(`SELECT group_concat(r, ',') FROM (SELECT busta_id || '|' || coalesce(consegna_artigiano_id, '-') || '|' ||
		coalesce(uscita_il, '-') || '|' || coalesce(rientro_il, '-') || '|' || nota_presente || '|' || sganciata AS r FROM giri_artigiani ORDER BY id)`).Scan(&righe); err != nil {
		t.Fatal(err)
	}
	if righe != "1|1|2026-09-01T08:00:00.000000Z|2026-09-05T08:00:00.000000Z|1|0,2|1|2026-10-01T08:00:00.000000Z|-|0|0" {
		t.Fatalf("giri importati: %s", righe)
	}
	gg, err := buste.Nuove(d, clock.NuovoFinto(istanteImport)).GiriDellArtigiano(context.Background(), 1)
	if err != nil || len(gg) != 2 || !gg[0].Attuale || gg[1].Attuale || !gg[1].NotaPresente || gg[1].Note != nil ||
		!gg[1].Uscita.Equal(time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)) || *gg[1].Consegna != 1 {
		t.Fatalf("giri della busta 1: %+v %v", gg, err)
	}
}
