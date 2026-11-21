package verifica

import (
	"context"
	"time"

	"github.com/L0cutus/gestrip/internal/assist"
	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Estrattore è l'estrazione della nuova busta per un dettato detto il giorno oggi.
type Estrattore func(ctx context.Context, dettato string, oggi time.Time, negozio assist.Negozio) (assist.Estrazione, error)

// DallaBusta è l'estrazione della nuova busta con il collegamento dato (slice 106): stessa richiesta,
// stesso messaggio corto, stesso nuovo tentativo dentro i 90 s, stessa lettura (104-105). Senza
// registro: il banco non scrive audit.
func DallaBusta(c *assist.Collegamento) Estrattore {
	return func(ctx context.Context, dettato string, oggi time.Time, negozio assist.Negozio) (assist.Estrazione, error) {
		return c.Estrai(ctx, dettato, oggi, negozio, nil, audit.Contesto{})
	}
}

// Cronometro comincia una misura e dà la funzione che dice il tempo passato.
type Cronometro func() func() time.Duration

// CronometroVero misura sulla lettura monotona del sistema (clock.Parti).
func CronometroVero() func() time.Duration { return clock.Parti().Trascorso }

// Corsa sono gli ingredienti di un giro del banco.
type Corsa struct {
	Estrai     Estrattore
	Negozio    assist.Negozio // le oreficerie che il negozio conosce; nessun cliente (nessun database)
	Cronometro Cronometro     // nil: CronometroVero
	// Avanzamento, se c'è, riceve quanti casi sono fatti su quanti, dopo ogni caso: solo numeri.
	Avanzamento func(fatti, totale int)
}

// Gira manda i casi uno alla volta, nell'ordine, e li conta. Se ctx finisce (Ctrl-C, timeout esterno)
// si ferma prima del caso dopo e dà gli esiti fatti fin lì con l'errore di ctx; il caso a metà non si
// conta.
func (c Corsa) Gira(ctx context.Context, casi []Caso) ([]Esito, error) {
	crono := c.Cronometro
	if crono == nil {
		crono = CronometroVero
	}
	esiti := make([]Esito, 0, len(casi))
	for _, caso := range casi {
		if err := ctx.Err(); err != nil {
			return esiti, err
		}
		trascorso := crono()
		e, err := c.Estrai(ctx, caso.Dettato, caso.Oggi, c.Negozio)
		durata := trascorso()
		if ctx.Err() != nil {
			return esiti, ctx.Err()
		}
		esiti = append(esiti, Valuta(caso, e, err, durata))
		if c.Avanzamento != nil {
			c.Avanzamento(len(esiti), len(casi))
		}
	}
	return esiti, nil
}
