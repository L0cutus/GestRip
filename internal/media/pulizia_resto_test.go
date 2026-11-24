package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
)

// Le parti di MediaCleanup (RIP-S-57) che non stanno nella slice 041. Finché questi test sono
// saltati, la voce resta parziale; ognuna va nel giro (Pulizia.giro, al suo punto) con la sua slice.

// inventario: RIP-S-57
// cleanup_stale_exports/0: a ogni giro, prima del resto, le esportazioni vecchie
// (InstanceExport.cleanup_stale/1) nella cartella di lavoro delle esportazioni.
func TestPuliziaEsportazioniVecchie(t *testing.T) {
	t.Skip("esportazioni vecchie: con l'esportazione dell'istanza")
}

// inventario: RIP-S-57
// run_retention/3: una volta al giorno del negozio la conservazione delle foto
// (PhotoRetention.run/0), riprovata al giro dopo se fallisce; è legata al registro dei backup
// verificati.
func TestPuliziaConservazioneFotoUnaVoltaAlGiorno(t *testing.T) {
	t.Skip("conservazione delle foto: T4.11")
}

// inventario: RIP-S-57, RIP-H-51
// run_training_expiry/3 (slice 115): una volta al giorno del negozio (Europe/Rome) la scadenza dei dettati di
// addestramento oltre dodici mesi (Assist.Training.expire/0), riprovata al giro dopo se fallisce; nel registro una riga
// fissa con il solo tipo dell'errore, mai il messaggio. Un errore o un panico della scadenza non fermano la coda dello
// stesso giro.
func TestPuliziaScadenzaDeiDettati(t *testing.T) {
	b := bancoPulizia(t)
	o := clock.NuovoFinto(time.Date(2026, 11, 24, 22, 30, 0, 0, time.UTC)) // 23:30 del 24/11 in negozio
	chiamate := 0
	var esito func() (int64, error)
	b.p.ConScadenzaDettati(o, func(ctx context.Context) (int64, error) {
		chiamate++
		return esito()
	})
	var reg registroFinto
	giro := func() { t.Helper(); b.p.giro(context.Background(), reg.logger()) }

	esito = func() (int64, error) { return 3, nil }
	giro()
	giro()
	if chiamate != 1 {
		t.Fatalf("stesso giorno: %d chiamate", chiamate)
	}
	// Mezzanotte e mezza in negozio è già il 25/11 (in UTC è ancora il 24): un giorno nuovo, con la scadenza guasta.
	o.Imposta(time.Date(2026, 11, 24, 23, 30, 0, 0, time.UTC))
	k := b.oggetto(t, "foto della scadenza")
	b.accoda(t, k)
	esito = func() (int64, error) { return 0, errors.New("guasto con Ofelia Inventata") }
	giro()
	if chiamate != 2 {
		t.Fatalf("giorno nuovo: %d chiamate", chiamate)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v: la scadenza guasta ha fermato la coda", c)
	}
	esito = func() (int64, error) { panic("valore segreto Ofelia") }
	b.accoda(t, b.oggetto(t, "foto del panico della scadenza"))
	giro()
	if chiamate != 3 {
		t.Fatalf("riprova dopo il guasto: %d chiamate", chiamate)
	}
	if c := b.coda(t); len(c) != 0 {
		t.Errorf("coda %v: il panico della scadenza ha fermato la coda", c)
	}
	esito = func() (int64, error) { return 0, nil }
	giro()
	giro()
	if chiamate != 4 {
		t.Errorf("dopo la riuscita: %d chiamate", chiamate)
	}
	s := reg.righe.String()
	if strings.Count(s, "scadenza dei dettati non riuscita") != 2 || !strings.Contains(s, "tipo=string") {
		t.Errorf("registro %q", s)
	}
	if strings.Contains(s, "Ofelia") || strings.Contains(s, "segreto") {
		t.Errorf("messaggio dell'errore nel registro: %q", s)
	}
	if strings.Count(s, "\n") != 2 {
		t.Errorf("righe del registro: %q", s)
	}
}

// inventario: RIP-S-57, RIP-H-51
// Allo stop (contesto finito) un errore della scadenza non è un avviso e il giorno resta da rifare.
func TestPuliziaScadenzaDeiDettatiFermata(t *testing.T) {
	b := bancoPulizia(t)
	o := clock.NuovoFinto(time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC))
	chiamate := 0
	b.p.ConScadenzaDettati(o, func(ctx context.Context) (int64, error) {
		chiamate++
		return 0, ctx.Err()
	})
	ctx, ferma := context.WithCancel(context.Background())
	ferma()
	var reg registroFinto
	b.p.giro(ctx, reg.logger())
	b.p.giro(context.Background(), reg.logger())
	if chiamate != 2 {
		t.Errorf("%d chiamate", chiamate)
	}
	if s := reg.righe.String(); s != "" {
		t.Errorf("registro %q", s)
	}
}
