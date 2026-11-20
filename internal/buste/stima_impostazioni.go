package buste

import (
	"context"
	"strconv"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Le impostazioni della stima dei tempi (slice 102, ADM-04, RIP-H-55): Settings.Estimate dello standalone (go-baseline,
// settings/estimate.ex) e il riquadro «Stima tempi» di ShopLive. Le tre chiavi e la lettura riparata sono quelle della
// pagina del cliente e della scheda (pagina_cliente.go): qui si scrivono.
//   - Spenta finché il titolare non la accende (D9): niente riga vale spenta, e qualunque valore diverso da «1» anche. Nessun
//     import la accende (importer/stima_test.go).
//   - I giorni si riportano in 0-90 e non si rifiutano; un testo che non dà un intero vale 7 o 8; il massimo non sta mai sotto
//     il minimo. Un modulo senza un campo dei giorni tiene quello che c'è (un invio a mano); un campo che non è un testo vale
//     il valore di partenza.
//   - Un solo audit, «settings.estimate_updated» su «settings/estimate», con i nomi delle chiavi cambiate e nessun valore,
//     nella stessa transazione delle chiavi.

// Le chiavi del modulo estimate[<campo>] dello standalone.
const (
	campoStimaAttiva = "enabled"
	campoStimaMin    = "artisan_min"
	campoStimaMax    = "artisan_max"
)

// ModuloStima è il modulo estimate[<campo>]: il testo di ogni campo arrivato (l'ultimo, come Plug); nil è un campo arrivato
// che non è un testo (una lista o una mappa); un campo che non c'è non è nella mappa.
type ModuloStima map[string]*string

// PaginaStima è quello che la pagina del negozio mostra della stima: le impostazioni di oggi (riparate) e il modello di
// oggi (RepairEstimate.model/1 con quelle impostazioni).
type PaginaStima struct {
	// Attiva, Min e Max sono Settings.Estimate.read/0: l'interruttore e il supplemento dell'artigiano delle impostazioni.
	Attiva   bool
	Min, Max int64
	// Oggi è la base di oggi («Pronta di solito in N-M giorni dal ritiro»); nil senza abbastanza buste di bottega.
	Oggi *Stima
	// Bottega e Artigiano sono shop_count e artisan_count; Minimo è RepairEstimate.minimum_shop/0.
	Bottega, Artigiano, Minimo int64
	// Supplemento è quello che il modello userebbe oggi per una busta passata dall'artigiano; Misurato dice che viene dai dati
	// («misurato sui dati») e non dalle impostazioni («dalle impostazioni»).
	Supplemento Stima
	Misurato    bool
}

// PaginaStima legge le impostazioni e il modello. Leggere non scrive niente.
func (b *Buste) PaginaStima(ctx context.Context) (PaginaStima, error) {
	imp, err := b.leggiImpostazioniStima(ctx)
	if err != nil {
		return PaginaStima{}, err
	}
	m, err := b.modelloStima(ctx, imp)
	if err != nil {
		return PaginaStima{}, err
	}
	p := PaginaStima{
		Attiva: imp.attiva, Min: imp.artMin, Max: imp.artMax,
		Bottega: m.bottega, Artigiano: m.artigiano, Minimo: stimaMinimoBottega,
		Supplemento: Stima{Min: m.supplemento[0], Max: m.supplemento[1]}, Misurato: m.misurato,
	}
	if m.base != nil {
		p.Oggi = &Stima{Min: m.base[0], Max: m.base[1]}
	}
	return p, nil
}

// SalvaStima è Settings.Estimate.put/2: scrive le tre chiavi e l'audit in una transazione. Non rifiuta mai un valore: i giorni
// si riportano. Un guasto del database dà l'errore del database e non lascia niente.
func (b *Buste) SalvaStima(ctx context.Context, m ModuloStima, c audit.Contesto) error {
	return b.inTransazione(ctx, func(tx esecutoreLettore, ora string) error {
		attuale, err := leggiImpostazioniStimaDa(ctx, tx)
		if err != nil {
			return err
		}
		// Map.get(attrs, "enabled") in ["true", "1", "on"]: senza il campo, o con un campo che non è un testo, spenta.
		acceso := false
		if v := m[campoStimaAttiva]; v != nil {
			acceso = *v == "true" || *v == "1" || *v == "on"
		}
		voluta := costruisciStima(acceso,
			giorniInviati(m, campoStimaMin, stimaArtigianoMnPartenza, attuale.artMin),
			giorniInviati(m, campoStimaMax, stimaArtigianoMxPartenza, attuale.artMax))
		attivaSalvata := "0"
		if voluta.attiva {
			attivaSalvata = "1"
		}
		righe := []struct{ chiave, valore string }{
			{chiaveStimaAttiva, attivaSalvata},
			{chiaveStimaArtigianoMn, strconv.FormatInt(voluta.artMin, 10)},
			{chiaveStimaArtigianoMx, strconv.FormatInt(voluta.artMax, 10)},
		}
		// Le chiavi cambiate: quelle il cui testo salvato è diverso (una riga che non c'è è cambiata). Mai i valori.
		cambiate := make([]string, 0, len(righe))
		for _, r := range righe {
			v, esiste, err := b.impostazione(ctx, tx, r.chiave)
			if err != nil {
				return err
			}
			if !esiste || v != r.valore {
				cambiate = append(cambiate, r.chiave)
			}
		}
		for _, r := range righe {
			if err := b.salvaImpostazione(ctx, tx, r.chiave, r.valore, ora); err != nil {
				return err
			}
		}
		return b.reg.Scrivi(ctx, tx, c, "settings.estimate_updated", "settings", "estimate", "ok", map[string]any{"changed": cambiate})
	})
}

// giorniInviati è submitted_days/4: il campo che non c'è lascia quello che c'è; quello che c'è si riporta (days/2), e un
// campo che non è un testo vale il valore di partenza.
func giorniInviati(m ModuloStima, campo string, partenza, attuale int64) int64 {
	v, arrivato := m[campo]
	if !arrivato {
		return attuale
	}
	return giorniStima(v, partenza)
}
