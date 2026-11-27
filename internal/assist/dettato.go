package assist

import (
	"context"
	"errors"
	"fmt"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il «Dettato AI» della busta nuova (slice 108; RIP-H-42, RIP-H-45): la pagina chiede qui se il riquadro
// si mostra (Assist.configured?(:ai) di assign_assist/1) e manda il dettato al modello (l'evento
// assist_extract di WorkshopLive, con Assist.extract_with_model/3). Clienti e oreficerie li legge il
// chiamante dal database (internal/buste), così assist non dipende da buste.

// EstrazioneConfigurata è Assist.configured?(:ai): indirizzo e modello salvati, a parte gli spazi. Si rilegge
// a ogni pagina, come lo standalone a ogni mount.
func (i *Impostazioni) EstrazioneConfigurata(ctx context.Context) (bool, error) {
	cfg, err := i.estrazione(ctx)
	if err != nil {
		return false, err
	}
	return cfg.Usabile(), nil
}

// TrascrizioneConfigurata è Assist.configured?(:stt) (slice 110): basta l'indirizzo salvato, a parte gli spazi. Il
// «🎤 Detta a voce» della busta nuova si mostra solo così; si rilegge a ogni pagina, come lo standalone a ogni mount.
func (i *Impostazioni) TrascrizioneConfigurata(ctx context.Context) (bool, error) {
	cfg, err := i.trascrizione(ctx)
	if err != nil {
		return false, err
	}
	return cfg.Usabile(), nil
}

// EstraiDettato è Assist.extract_with_model/3 per la busta nuova: la configurazione salvata (LeggiEstrazione,
// riletta a ogni richiesta), poi i rifiuti che non costano niente (estrazione non configurata, testo troppo corto o
// lungo: niente letture, niente richieste, niente audit), poi il negozio dal chiamante (negozio, letto solo adesso,
// come lo standalone legge oreficerie e clienti dopo i controlli) e il client della 106 (nuovo tentativo D-19, attesa
// unica, audit assist.extract con soli conteggi), con «oggi» nell'ora del negozio. Una lettura della configurazione o
// del negozio che fallisce è l'errore del database, non un rifiuto (ErrNegozio per il negozio).
func (i *Impostazioni) EstraiDettato(ctx context.Context, dettato string, negozio func(context.Context) (Negozio, error), a audit.Contesto) (Estrazione, error) {
	cfg, err := i.estrazione(ctx)
	if err != nil {
		return Estrazione{}, err
	}
	c := Collegamento{Config: cfg, Attesa: i.Attesa, SoloLocale: i.SoloLocale}
	if _, _, err := c.controllaPrima(dettato); err != nil {
		return Estrazione{}, err
	}
	n, err := negozio(ctx)
	if err != nil {
		return Estrazione{}, fmt.Errorf("%w: %w", ErrNegozio, err)
	}
	return c.Estrai(ctx, dettato, clock.Locale(i.o.Adesso()), n, i.reg, a)
}

// ErrNegozio: clienti e oreficerie del negozio non letti.
var ErrNegozio = errors.New("assist: clienti e oreficerie non letti")
