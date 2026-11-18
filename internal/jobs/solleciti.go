package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Solleciti artigiano» (Jobs.ArtisanChases dello standalone a go-baseline, JOB-09; slice 096):
//
//   - **Le buste** sono quelle della pagina dei solleciti (083: fuori bottega con una consegna e il suo artigiano,
//     uscite da almeno N·86 400 s, fuori dal cestino: 097b) con la stessa soglia (solleciti_artigiano_giorni,
//     threshold_days/1), non ancora nominate **in questo giro** dall'artigiano (buste.TimbraSollecitiDelGiro).
//   - **Un riepilogo per canale** al negozio (i destinatari del pianificatore), non all'artigiano: oggetto
//     «Solleciti artigiano», righe «🧰 Solleciti artigiano», «N buste fuori bottega da oltre <soglia> giorni.» e
//     una riga per artigiano «<nome>: <numero> (Ngg), …»; il numero è quello dei testi (D-64, D-82).
//   - **Tutto o niente:** elenco, timbri e riepiloghi sono una transazione sola. Un rifiuto della coda su un
//     canale annulla i timbri di tutte le buste e i riepiloghi già accodati: «Fallito» con il motivo e zero
//     conteggi. Un guasto che non è un rifiuto (il database) annulla lo stesso e la riga dirà DettaglioInterrotto.
//   - **Non registra un sollecito:** nessuna riga solleciti_artigiani né repair.artisan_reminded; il sollecito
//     vero all'artigiano resta il gesto a mano della pagina.
//   - **Stop:** con il contesto finito il lavoro torna con l'errore del contesto (niente riga, il giorno resta da
//     fare) e la transazione si annulla: nessuna busta resta timbrata senza riepilogo.

// LavoroSolleciti è il lavoro dei solleciti di un database, da registrare nel pianificatore come Solleciti.
type LavoroSolleciti struct {
	d     *db.DB
	posta Accodatore
	log   *slog.Logger
}

// NuoviSolleciti è il lavoro dei solleciti sul database d, che accoda con posta (la coda di serve).
func NuoviSolleciti(d *db.DB, posta Accodatore) *LavoroSolleciti {
	return &LavoroSolleciti{d: d, posta: posta, log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà al lavoro le righe del server: per un riepilogo rifiutato il motivo (testi fissi, mai indirizzi,
// artigiani, numeri o testi dei messaggi).
func (s *LavoroSolleciti) ConRegistro(l *slog.Logger) *LavoroSolleciti {
	if l != nil {
		s.log = l
	}
	return s
}

// Esegui è run/4.
func (s *LavoroSolleciti) Esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	if len(dest) == 0 {
		return Risultato{}, ErrSenzaCanali // il pianificatore non lo fa mai (D-16): niente timbri senza riepilogo
	}
	r, err := s.esegui(ctx, adesso, dest, a)
	if err != nil && ctx.Err() != nil {
		return Risultato{}, ctx.Err() // uno stop, anche se il driver lo dice con un altro errore
	}
	return r, err
}

func (s *LavoroSolleciti) esegui(ctx context.Context, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	tx, err := s.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Risultato{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente; senza Commit annulla tutto il giro
	giorni, gruppi, n, err := buste.TimbraSollecitiDelGiro(ctx, tx, adesso)
	if err != nil {
		return Risultato{}, err
	}
	if n == 0 {
		return Risultato{Dettaglio: "Nessuna busta oltre soglia."}, nil // niente timbrato: niente da confermare
	}
	cont := testoSolleciti(giorni, gruppi, n)
	for _, d := range dest {
		if _, err := s.posta.AccodaIn(ctx, tx, d, cont, a); err != nil {
			var motivo string
			switch {
			case errors.Is(err, uscita.ErrNonConfigurato):
				motivo = "Il canale " + d.Canale + " non è più configurato."
			case errors.Is(err, uscita.ErrMessaggioNonValido), errors.Is(err, uscita.ErrSoloAMano):
				motivo = "La coda ha rifiutato il riepilogo dei solleciti."
			default:
				return Risultato{}, err
			}
			s.log.Warn("solleciti: riepilogo non accodato", "motivo", motivo)
			return Risultato{Fallito: true, Dettaglio: motivo}, nil // Rollback: nessun timbro, nessun riepilogo
		}
	}
	if err := tx.Commit(); err != nil {
		return Risultato{}, err
	}
	return Risultato{Avvisi: n, Messaggi: len(dest), Dettaglio: quanteBuste(n) + " oltre soglia."}, nil
}

// testoSolleciti è content/2: testo semplice (nessun parse_mode), una riga per artigiano.
func testoSolleciti(giorni int64, gruppi []buste.GruppoSolleciti, n int) uscita.Contenuto {
	righe := []string{
		"🧰 Solleciti artigiano",
		fmt.Sprintf("%s fuori bottega da oltre %d giorni.", quanteBuste(n), giorni),
	}
	for _, g := range gruppi {
		elenco := make([]string, len(g.Buste))
		for i, x := range g.Buste {
			qr := ""
			if x.CodiceQR != nil {
				qr = *x.CodiceQR
			}
			elenco[i] = fmt.Sprintf("%s (%dgg)", buste.NumeroNeiTesti(qr, x.Numero), x.GiorniFuori)
		}
		righe = append(righe, g.Artigiano+": "+strings.Join(elenco, ", "))
	}
	oggetto := "Solleciti artigiano"
	return uscita.Contenuto{Oggetto: &oggetto, Testo: strings.Join(righe, "\n")}
}
