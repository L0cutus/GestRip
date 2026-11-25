package backup

// La rotazione degli archivi sulla chiavetta (slice 123, D-114, risposta D del piano della T5.2): 7
// giornalieri + 4 settimanali, fatta dal Go. Lo standalone non cancella mai un archivio (la rotazione la fa
// restic su otto-vps, fuori dal pacchetto, con --keep-daily 7 --keep-weekly 4).
//
// Fallisce chiusa: ogni dubbio (registro che non dice che il nuovo archivio è l'ultimo verificato, un nome
// nel futuro, un archivio più nuovo di quello appena verificato, un file cambiato fra l'elenco e la
// cancellazione) vuol dire «non cancello niente» (o «non cancello altro»).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// GiornalieriDaTenere e SettimanaliDaTenere sono i numeri di D-114.
const (
	GiornalieriDaTenere = 7
	SettimanaliDaTenere = 4
)

// ErrRotazione è una rotazione che non cancella niente (o si ferma) per un dubbio.
var ErrRotazione = errors.New("backup: rotazione fermata, nessun altro archivio cancellato")

// fusoRotazione è il fuso dei giorni e delle settimane della rotazione: quello del negozio.
var fusoRotazione = clock.Roma

// EsitoRotazione dice cosa ha fatto la rotazione: gli archivi tenuti, quelli tolti e quelli con il nostro
// nome che non si toccano perché il registro non li conosce (o non sono file normali).
type EsitoRotazione struct {
	Tenuti, Tolti, Lasciati []string
}

// archivioInCartella è un archivio con il nostro nome nella destinazione.
type archivioInCartella struct {
	voce       voceCartella
	t          time.Time
	verificato bool // una riga 'ok' del registro con questo percorso e questi byte
	registrato bool // una riga qualsiasi del registro con questo percorso e questi byte
}

// daTenere sceglie gli archivi da tenere fra quelli verificati, con i giorni e le settimane nel fuso loc
// (il nome è in UTC: un backup alle 00:30 di Roma ha il nome del giorno prima in UTC, ma è del giorno
// dopo; al cambio dell'ora legale un giorno ha 23 o 25 ore, e conta come uno):
//   - giornalieri: per i GiornalieriDaTenere giorni più recenti che hanno un archivio verificato,
//     l'ultimo di ogni giorno (come --keep-daily di restic: un giorno senza backup non conta);
//   - verifiche fallite: un archivio registrato solo come fallito resta se è dal giorno del più vecchio
//     giornaliero in poi (non conta fra i 7 + 4), si toglie dopo;
//   - settimanali: per le SettimanaliDaTenere settimane ISO più recenti **prima** della settimana del più
//     vecchio giornaliero, l'ultimo di ogni settimana («per le 4 settimane prima», risposta D: fino a 11
//     archivi). La settimana ISO va a cavallo dell'anno (29/12/2026 e 2/1/2027 sono tutti e due della
//     2026-W53).
//
// aa va dal più nuovo al più vecchio.
func daTenere(aa []archivioInCartella, loc *time.Location) map[string]bool {
	tieni := map[string]bool{}
	type giorno struct {
		a, m, g int
	}
	giorni := map[giorno]bool{}
	var vecchioAnno, vecchiaSett int
	var vecchioGiorno time.Time // mezzanotte (fuso loc) del più vecchio giornaliero
	for _, a := range aa {
		if !a.verificato {
			continue
		}
		y, m, d := a.t.In(loc).Date()
		k := giorno{y, int(m), d}
		if giorni[k] {
			continue
		}
		if len(giorni) == GiornalieriDaTenere {
			break
		}
		giorni[k] = true
		tieni[a.voce.nome] = true
		vecchioAnno, vecchiaSett = a.t.In(loc).ISOWeek()
		vecchioGiorno = time.Date(y, m, d, 0, 0, 0, 0, loc)
	}
	if len(giorni) == 0 {
		return tieni
	}
	// Gli archivi con la sola verifica fallita (registrati, mai verificati) dentro la finestra dei giornalieri
	// restano: se la verifica è caduta per un errore passeggero di lettura, sono comunque un punto di ripristino.
	// Non contano fra i 7 + 4 e si tolgono quando la finestra li supera (revisione della 123).
	for _, a := range aa {
		if !a.verificato && a.registrato && !a.t.Before(vecchioGiorno) {
			tieni[a.voce.nome] = true
		}
	}
	settimane := map[[2]int]bool{}
	for _, a := range aa {
		if !a.verificato {
			continue
		}
		y, w := a.t.In(loc).ISOWeek()
		if y > vecchioAnno || (y == vecchioAnno && w >= vecchiaSett) {
			continue
		}
		k := [2]int{y, w}
		if settimane[k] {
			continue
		}
		if len(settimane) == SettimanaliDaTenere {
			break
		}
		settimane[k] = true
		tieni[a.voce.nome] = true
	}
	return tieni
}

// Ruota toglie dalla chiavetta gli archivi che D-114 non tiene. Va chiamata **solo dopo** che il nuovo
// archivio è verificato e registrato come «chiavetta», con il lucchetto della destinazione. Le regole:
//   - la destinazione dev'essere una chiavetta, e l'ultima riga 'ok' sulla chiavetta del registro (quella
//     che sblocca le cancellazioni dopo un import) dev'essere proprio nuovo: stesso percorso, SHA-256 e
//     byte; il file c'è, normale, con quei byte. Se no non si cancella niente;
//   - si guardano solo i nomi esatti di NomeArchivio (reArchivio), file normali; collegamenti, cartelle,
//     .pending e ogni altro file restano;
//   - un nome registrato nel futuro (rispetto ad adesso) o più nuovo di nuovo: non si cancella niente
//     (orologio); uno senza riga si lascia e la rotazione va avanti;
//   - si cancella solo un archivio che il registro conosce (una riga con lo stesso percorso e gli stessi
//     byte): uno senza riga (copiato a mano, di un'altra macchina, di prima di un ripristino) resta;
//   - non si cancellano mai nuovo, l'archivio dell'ultima riga 'ok' sulla chiavetta, e quelli che daTenere
//     sceglie (anche gli archivi con la sola verifica fallita dentro la finestra dei giornalieri); fuori
//     dalla finestra un archivio con la sola verifica fallita si toglie;
//   - prima di ogni cancellazione si riguarda il nome (stesso dispositivo, inode, byte, ora di modifica e di
//     cambio di stato: un inode si può riusare); un cambio o un errore ferma la rotazione: quello che è tolto è
//     tolto, il resto resta.
func (d *Destinazione) Ruota(ctx context.Context, q *db.DB, nuovo Risultato, adesso time.Time) (EsitoRotazione, error) {
	if d.chiavetta == nil {
		return EsitoRotazione{}, fmt.Errorf("%w: la destinazione non è una chiavetta", ErrRotazione)
	}
	if filepath.Dir(nuovo.File) != d.Cartella {
		return EsitoRotazione{}, fmt.Errorf("%w: il nuovo archivio non sta nella chiavetta", ErrRotazione)
	}
	ultimo, ok, err := db.UltimoVerificatoSullaChiavetta(ctx, q.Lettura)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	if !ok || ultimo.Archivio != nuovo.File || ultimo.SHA256 != nuovo.SHA256 || ultimo.Byte != nuovo.ByteCifrati {
		return EsitoRotazione{}, fmt.Errorf("%w: il registro non dice che il nuovo archivio è l'ultimo verificato sulla chiavetta", ErrRotazione)
	}
	tNuovo, ok := istanteDelNome(reArchivio, filepath.Base(nuovo.File))
	if !ok {
		return EsitoRotazione{}, fmt.Errorf("%w: nome del nuovo archivio", ErrRotazione)
	}
	righe, err := db.ArchiviNellaCartella(ctx, q.Lettura, d.Cartella)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	nomi, err := nomiDi(d.d)
	if err != nil {
		return EsitoRotazione{}, errors.Join(ErrRotazione, err)
	}
	var e EsitoRotazione
	var aa []archivioInCartella
	trovato := false
	for _, n := range nomi {
		t, ok := istanteDelNome(reArchivio, n)
		if !ok {
			continue
		}
		v, err := guardaVoce(d.d, n)
		if err != nil {
			return EsitoRotazione{}, errors.Join(ErrRotazione, err)
		}
		if !v.normale || v.id.dev != d.id.dev {
			e.Lasciati = append(e.Lasciati, n)
			continue
		}
		a := archivioInCartella{voce: v, t: t}
		p := filepath.Join(d.Cartella, n)
		for _, r := range righe {
			if r.Archivio == p && r.Byte == v.byte {
				a.registrato = true
				a.verificato = a.verificato || r.Riuscita
			}
		}
		if t.After(adesso) || t.After(tNuovo) {
			if a.registrato {
				return EsitoRotazione{}, fmt.Errorf("%w: %s è più nuovo del backup appena fatto (orologio?)", ErrRotazione, n)
			}
			// Senza riga non si cancellerebbe comunque: una copia a mano da una macchina con l'orologio avanti
			// non deve fermare la rotazione per sempre (revisione della 123).
			e.Lasciati = append(e.Lasciati, n)
			continue
		}
		if n == filepath.Base(nuovo.File) {
			if !a.verificato || v.byte != nuovo.ByteCifrati {
				return EsitoRotazione{}, fmt.Errorf("%w: il nuovo archivio non è quello registrato", ErrRotazione)
			}
			trovato = true
		}
		aa = append(aa, a)
	}
	if !trovato {
		return EsitoRotazione{}, fmt.Errorf("%w: il nuovo archivio non è nella chiavetta", ErrRotazione)
	}
	sort.Slice(aa, func(i, j int) bool { return aa[i].t.After(aa[j].t) })
	tieni := daTenere(aa, fusoRotazione)
	// Il nuovo archivio è l'ultimo verificato del registro (controllato sopra): non si toglie mai, anche se
	// daTenere lo sceglie già come giornaliero.
	tieni[filepath.Base(nuovo.File)] = true
	var togliere []archivioInCartella
	for _, a := range aa {
		switch {
		case tieni[a.voce.nome]:
			e.Tenuti = append(e.Tenuti, a.voce.nome)
		case !a.registrato:
			e.Lasciati = append(e.Lasciati, a.voce.nome)
		default:
			togliere = append(togliere, a)
		}
	}
	for _, a := range togliere {
		if err := ctx.Err(); err != nil {
			return e, errors.Join(ErrRotazione, err)
		}
		if primaDiTogliereArchivio != nil {
			primaDiTogliereArchivio(a.voce.nome)
		}
		v, err := guardaVoce(d.d, a.voce.nome)
		if err != nil || v != a.voce {
			return e, fmt.Errorf("%w: %s è cambiato mentre si ruotava", ErrRotazione, a.voce.nome)
		}
		if err := togliVoce(d.d, a.voce.nome); err != nil {
			return e, errors.Join(ErrRotazione, err)
		}
		e.Tolti = append(e.Tolti, a.voce.nome)
	}
	if len(e.Tolti) > 0 {
		if err := d.d.Sync(); err != nil {
			return e, fmt.Errorf("backup: rotazione, fsync della chiavetta: %w", err)
		}
	}
	return e, nil
}

// primaDiTogliereArchivio, solo nei test, gira prima di riguardare un archivio da togliere.
var primaDiTogliereArchivio func(nome string)
