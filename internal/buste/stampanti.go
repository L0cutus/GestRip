package buste

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/printing"
)

// La stampa diretta (slice 046, D-08 e D-09; approvata da Stefano il 04/11/2026): per le
// etichette QR (57×32 o il formato del foglio) e per l'etichetta busta 76×127 il titolare sceglie una
// coda di CUPS, o nessuna («solo PDF», il valore iniziale), e le opzioni della stampante. Le scelte
// stanno nelle impostazioni, con chiavi solo del Go (importer.ChiaviSoloGo). Qui:
//   - ScelteStampa e SalvaScelteStampa, con la riga printer_settings.updated;
//   - StampaEtichetteAccettata: dopo che CUPS ha accettato il lavoro del foglio, nella stessa
//     transazione il contatore come il PDF (D-08: max(ultimo, a) e l'anno del D-66), il formato
//     come il PDF e la riga label_sheet.printed con la coda e l'id del lavoro;
//   - EtichettaDaStampare e StampaEtichettaAccettata per l'etichetta busta (D-09): il PDF senza riga
//     d'audit, poi, solo dopo che CUPS ha accettato, la riga repair.label_printed.
//
// Il lavoro di stampa non sta nella transazione (non si può disfare): si manda prima, e solo un
// lavoro accettato scrive qualcosa.

// Le chiavi delle scelte (solo Go).
const (
	ChiaveCodaQR       = "stampa_coda_etichette_qr"
	ChiaveOpzioniQR    = "stampa_opzioni_etichette_qr"
	ChiaveCodaBusta    = "stampa_coda_etichetta_busta"
	ChiaveOpzioniBusta = "stampa_opzioni_etichetta_busta"
)

// SceltaStampa è la stampante di un documento: la coda ("" = nessuna, solo PDF) e le opzioni nella
// forma salvata («MediaTracking=Gap Darkness=8»).
type SceltaStampa struct {
	Coda, Opzioni string
}

// Attiva dice se il documento si stampa direttamente.
func (s SceltaStampa) Attiva() bool { return s.Coda != "" }

// ScelteStampa sono le stampanti dei due documenti.
type ScelteStampa struct {
	QR, Busta SceltaStampa
}

// ErrSceltaStampa è una scelta che non si salva (coda o opzioni non valide).
var ErrSceltaStampa = errors.New("scelta della stampante non valida")

// ScelteStampa legge le scelte; quelle mancanti sono «nessuna». Una coda salvata che non è più un
// nome valido (dati guasti) vale nessuna: non si manda a lp.
func (b *Buste) ScelteStampa(ctx context.Context) (ScelteStampa, error) {
	var s ScelteStampa
	for _, x := range []struct {
		chiave string
		dove   *string
	}{{ChiaveCodaQR, &s.QR.Coda}, {ChiaveOpzioniQR, &s.QR.Opzioni}, {ChiaveCodaBusta, &s.Busta.Coda}, {ChiaveOpzioniBusta, &s.Busta.Opzioni}} {
		v, _, err := b.impostazione(ctx, b.d.Lettura, x.chiave)
		if err != nil {
			return ScelteStampa{}, err
		}
		*x.dove = v
	}
	for _, c := range []*SceltaStampa{&s.QR, &s.Busta} {
		if c.Coda != "" && !printing.CodaValida(c.Coda) {
			c.Coda = ""
		}
		if _, err := printing.LeggiOpzioni(c.Opzioni); err != nil {
			c.Opzioni = ""
		}
	}
	return s, nil
}

// normalizza controlla una scelta e dà le opzioni nella forma salvata.
func normalizza(s SceltaStampa) (SceltaStampa, error) {
	if s.Coda != "" && !printing.CodaValida(s.Coda) {
		return SceltaStampa{}, fmt.Errorf("%w: %w", ErrSceltaStampa, printing.ErrCodaNonValida)
	}
	oo, err := printing.LeggiOpzioni(s.Opzioni)
	if err != nil {
		return SceltaStampa{}, fmt.Errorf("%w: %w", ErrSceltaStampa, err)
	}
	return SceltaStampa{Coda: s.Coda, Opzioni: printing.ScriviOpzioni(oo)}, nil
}

// SalvaScelteStampa salva le due scelte (coda valida o vuota, opzioni valide) e scrive
// printer_settings.updated con le scelte di prima e quelle nuove, in una transazione. Che la coda ci
// sia in CUPS lo controlla chi chiama (la pagina, con l'elenco di lpstat).
func (b *Buste) SalvaScelteStampa(ctx context.Context, s ScelteStampa, c audit.Contesto) error {
	qr, err := normalizza(s.QR)
	if err != nil {
		return err
	}
	busta, err := normalizza(s.Busta)
	if err != nil {
		return err
	}
	prima, err := b.ScelteStampa(ctx)
	if err != nil {
		return err
	}
	return b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		for k, v := range map[string]string{ChiaveCodaQR: qr.Coda, ChiaveOpzioniQR: qr.Opzioni, ChiaveCodaBusta: busta.Coda, ChiaveOpzioniBusta: busta.Opzioni} {
			if err := b.salvaImpostazione(ctx, tx, k, v, ora); err != nil {
				return err
			}
		}
		d := map[string]any{
			"label_sheet": map[string]any{"queue": qr.Coda, "options": qr.Opzioni, "previous_queue": prima.QR.Coda},
			"bag_label":   map[string]any{"queue": busta.Coda, "options": busta.Opzioni, "previous_queue": prima.Busta.Coda},
		}
		return b.reg.Scrivi(ctx, tx, c, "printer_settings.updated", "settings", "printers", "ok", d)
	})
}

// contaFoglio conta un foglio generato o stampato (D-08, D-66), dentro una transazione: con l'ultimo letto dentro,
// etichette_qr_ultimo = max(ultimo, a) e l'anno del D-66 = max(anno salvato, anno del foglio). Dà
// l'ultimo di prima e quello nuovo.
func (b *Buste) contaFoglio(ctx context.Context, tx esecutoreLettore, f FoglioEtichette, ora string) (prima, ultimo int64, err error) {
	v, _, err := b.impostazione(ctx, tx, ChiaveUltimaEtichetta)
	if err != nil {
		return 0, 0, err
	}
	prima = interoSalvato(v, 0)
	ultimo = max(prima, f.A)
	va, okAnno, err := b.impostazione(ctx, tx, ChiaveAnnoEtichette)
	if err != nil {
		return 0, 0, err
	}
	// Solo un anno del modulo (2024…2099) diventa l'anno salvato: /labels.pdf accetta ogni anno come lo
	// standalone, e un anno sbagliato (20267) fermerebbe per sempre la proposta del D-66.
	anno := f.Anno
	if !AnnoDelModulo(anno) {
		anno = math.MinInt64
	}
	if okAnno {
		if a := interoSalvato(va, math.MinInt64); a > anno {
			anno = a
		}
	}
	if err := b.salvaImpostazione(ctx, tx, ChiaveUltimaEtichetta, strconv.FormatInt(ultimo, 10), ora); err != nil {
		return 0, 0, err
	}
	if anno != math.MinInt64 {
		if err := b.salvaImpostazione(ctx, tx, ChiaveAnnoEtichette, strconv.FormatInt(anno, 10), ora); err != nil {
			return 0, 0, err
		}
	}
	return prima, ultimo, nil
}

// StampaEtichetteAccettata è il foglio che CUPS ha accettato (coda, lavoro): in una transazione il
// formato (format_g, come il PDF), il contatore e l'anno come il PDF (D-08) e label_sheet.printed
// con i dati del foglio, previous, last, queue e job. Dà il nuovo ultimo numero. Se la riga non si
// scrive non si salva niente.
func (b *Buste) StampaEtichetteAccettata(ctx context.Context, f FoglioEtichette, coda, lavoro string, c audit.Contesto) (int64, error) {
	var ultimo int64
	err := b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		if err := b.salvaImpostazione(ctx, tx, ChiaveLarghezzaQR, FormatoG(f.L), ora); err != nil {
			return err
		}
		if err := b.salvaImpostazione(ctx, tx, ChiaveAltezzaQR, FormatoG(f.H), ora); err != nil {
			return err
		}
		prima, u, err := b.contaFoglio(ctx, tx, f, ora)
		if err != nil {
			return err
		}
		ultimo = u
		d := datiFoglio(f)
		d["previous"], d["last"], d["queue"], d["job"] = prima, ultimo, coda, lavoro
		return b.reg.Scrivi(ctx, tx, c, "label_sheet.printed", "label_sheet", IdFoglio(f), "ok", d)
	})
	if err != nil {
		return 0, err
	}
	return ultimo, nil
}

// EtichettaDaStampare legge la busta id e fa il PDF dell'etichetta con fai, senza riga d'audit: la
// riga è repair.label_printed, solo se CUPS accetta (StampaEtichettaAccettata). Errori come Etichetta.
func (b *Buste) EtichettaDaStampare(ctx context.Context, id string, fai func(DatiEtichetta) ([]byte, error)) ([]byte, DatiEtichetta, error) {
	n, err := idDellaRicevuta(id)
	if err != nil {
		return nil, DatiEtichetta{}, err
	}
	e, err := b.leggiEtichetta(ctx, n)
	if err != nil {
		return nil, DatiEtichetta{}, err
	}
	pdf, err := fai(e)
	if err != nil {
		return nil, e, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	return pdf, e, nil
}

// StampaEtichettaAccettata scrive repair.label_printed per l'etichetta della busta che CUPS ha
// accettato, con il numero, la coda e l'id del lavoro.
func (b *Buste) StampaEtichettaAccettata(ctx context.Context, e DatiEtichetta, coda, lavoro string, c audit.Contesto) error {
	return b.reg.Registra(ctx, c, "repair.label_printed", "repair", e.ID, "ok",
		map[string]any{"bag_number": e.Numero, "queue": coda, "job": lavoro})
}

// StampaEtichetteNonSalvata è la traccia di un foglio che CUPS ha accettato quando la transazione di
// StampaEtichetteAccettata non si è chiusa: label_sheet.printed con esito counter_not_saved, fuori
// da ogni transazione e senza garanzia (chi chiama, se fallisce anche questa, lo scrive nel registro).
func (b *Buste) StampaEtichetteNonSalvata(ctx context.Context, f FoglioEtichette, coda, lavoro string, c audit.Contesto) error {
	d := datiFoglio(f)
	d["queue"], d["job"] = coda, lavoro
	return b.reg.Registra(ctx, c, "label_sheet.printed", "label_sheet", IdFoglio(f), "counter_not_saved", d)
}

// StampaEtichettaNonSalvata è la stessa traccia per l'etichetta busta, quando la riga di
// StampaEtichettaAccettata non si è scritta: repair.label_printed con esito counter_not_saved.
func (b *Buste) StampaEtichettaNonSalvata(ctx context.Context, e DatiEtichetta, coda, lavoro string, c audit.Contesto) error {
	return b.reg.Registra(ctx, c, "repair.label_printed", "repair", e.ID, "counter_not_saved",
		map[string]any{"bag_number": e.Numero, "queue": coda, "job": lavoro})
}
