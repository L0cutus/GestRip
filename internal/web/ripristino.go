package web

// La conferma «questa è ora la macchina vera» dopo un ripristino isolato (slice 131, OPS-19, D-12, D-118), solo Go:
//   - GET /settings/backup/ripristino, solo il titolare: sulla copia isolata dice prima di tutto di spegnere la
//     macchina vecchia, poi da quale archivio viene la copia, cosa si riaccenderà (email, Telegram, copia remota:
//     solo quelli accesi nel backup) e i messaggi rimasti in coda, ognuno con «Scarta» (già scelto, la proposta della
//     D-118 (b)) o «Manda»; in fondo la parola da scrivere (backup.ParolaDiConferma). Su una macchina normale dice
//     che non c'è niente da confermare; dopo la conferma, finché serve non riparte, che bisogna riavviare;
//   - POST /settings/backup/ripristino, solo il titolare, con il CSRF della catena: la parola, gli id dei messaggi
//     che la pagina ha mostrato (riga) e la scelta di ognuno (scelta-<id>: «manda», altrimenti scarta). La conferma
//     (backup.ConfermaRipristino) è tutta in una transazione; un messaggio arrivato dopo che la pagina è stata
//     disegnata è un rifiuto (la pagina si ridisegna con la lista nuova), come la parola sbagliata: niente cambia;
//   - il serve che gira resta isolato fino al riavvio (la via più semplice e sicura: nessun canale si accende a
//     caldo, e le righe che il titolare ha scartato non ci sono più quando il serve nuovo fa il primo giro della
//     coda). La pagina e il banner lo dicono;
//   - il banner «Questa è una copia isolata» (D-118 (d)) su ogni pagina interna, per tutto il personale, finché serve
//     non riparte: al titolare con il link alla pagina, agli altri con «chiama il titolare». Non sulla firma (il
//     tablet passa al cliente) né sulla pagina della conferma.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/backup"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// RipristinoDelNegozio è la conferma del ripristino: *backup.Conferma.
type RipristinoDelNegozio interface {
	Stato(ctx context.Context) (backup.StatoRipristino, error)
	Isolata(ctx context.Context) (bool, error)
	Conferma(ctx context.Context, s backup.SceltaConferma, chi audit.Contesto) (backup.EsitoConferma, error)
}

const rottaRipristino = rottaPaginaBackup + "/ripristino"

const (
	msgRipristinoNienteDaConfermare = "Questa non è una copia ripristinata: non c'è niente da confermare."
	msgParolaSbagliata              = "Conferma non data: per confermare scrivi " + backup.ParolaDiConferma + " nella casella. Niente è cambiato."
	msgCodaCambiata                 = "Nel frattempo i messaggi in coda sono cambiati: ricontrolla la lista qui sotto e conferma di nuovo. Niente è cambiato."
	msgConfermaNonRiuscita          = "La conferma non è riuscita: niente è cambiato. Riprova; se il problema resta, chiedi assistenza."
	msgStatoNonLetto                = "Lo stato del ripristino non si riesce a leggere. Riprova fra poco; se il problema resta, chiedi assistenza."
	msgRipristinoConfermato         = "Ripristino confermato: questa è ora la macchina vera."
	msgRipristinoGiaConfermato      = "Ripristino già confermato: riavvia GestRip."
)

// datiRipristino è la pagina della conferma.
type datiRipristino struct {
	Illeggibile bool
	// Isolata: la copia è ancora da confermare; Confermata: confermata, ma serve è partito isolato e va riavviato.
	Isolata, Confermata                   bool
	Archivio, RipristinatoIl, CatturatoIl string
	DaRiaccendere                         string
	Coda                                  []rigaRipristino
	Parola                                string
}

// rigaRipristino è un messaggio in coda come la pagina lo mostra; Manda è la scelta da ridisegnare.
type rigaRipristino struct {
	ID                                     int64
	Canale, Destinatario, Indirizzo, Busta string
	Oggetto, Testo, Quando                 string
	Manda                                  bool
}

var paroleCanale = map[string]string{"email": "Email", "telegram": "Telegram"}
var paroleDestinatario = map[string]string{"shop": "al negozio", "customer": "al cliente", "oreficeria": "all'oreficeria", "artisan": "all'artigiano"}

// elencoRiaccesi è l'elenco in parole di quello che la conferma riaccende.
func elencoRiaccesi(p []string) string {
	if len(p) == 0 {
		return "niente: nel backup email, Telegram e copia remota erano spenti"
	}
	return strings.Join(p, ", ")
}

// datiDelRipristino legge lo stato e prepara la pagina; manda sono gli id da ridisegnare come «Manda».
func (f *facciaWeb) datiDelRipristino(ctx context.Context, manda map[int64]bool) datiRipristino {
	st, err := f.ripristino.Stato(ctx)
	if err != nil {
		f.registro.Error("stato del ripristino non letto", "tipo", fmt.Sprintf("%T", err))
		return datiRipristino{Illeggibile: true}
	}
	if !st.Isolata {
		return datiRipristino{Confermata: f.copiaIsolata}
	}
	d := datiRipristino{Isolata: true, Archivio: st.Isolamento.Archivio, DaRiaccendere: elencoRiaccesi(st.DaRiaccendere),
		Parola: backup.ParolaDiConferma}
	for _, x := range []struct {
		testo string
		dst   *string
	}{{st.Isolamento.RipristinatoIl, &d.RipristinatoIl}, {st.Isolamento.CatturatoIl, &d.CatturatoIl}} {
		*x.dst = x.testo
		if t, err := time.Parse(db.FormatoIstante, x.testo); err == nil {
			*x.dst = clock.Timbro(t)
		}
	}
	for _, m := range st.Coda {
		canale := paroleCanale[m.Canale]
		if canale == "" {
			canale = m.Canale
		}
		d.Coda = append(d.Coda, rigaRipristino{ID: m.ID, Canale: canale, Destinatario: paroleDestinatario[m.Destinatario], Indirizzo: m.Indirizzo,
			Busta: m.NumeroBusta, Oggetto: m.Oggetto, Testo: m.Testo, Quando: clock.Timbro(m.AccodatoIl), Manda: manda[m.ID]})
	}
	return d
}

// paginaRipristino è GET /settings/backup/ripristino.
func (f *facciaWeb) paginaRipristino(w http.ResponseWriter, r *http.Request) {
	if f.ripristino == nil {
		Errore(w, r)
		return
	}
	f.mostraRipristino(w, r, "", nil)
}

func (f *facciaWeb) mostraRipristino(w http.ResponseWriter, r *http.Request, errore string, manda map[int64]bool) {
	d := f.datiDelRipristino(r.Context(), manda)
	if d.Illeggibile && errore == "" {
		errore = msgStatoNonLetto
	}
	f.mostraConInfo(w, r, "ripristino", http.StatusOK, errore, "", d)
}

// confermaRipristino è POST /settings/backup/ripristino.
func (f *facciaWeb) confermaRipristino(w http.ResponseWriter, r *http.Request) {
	if f.ripristino == nil {
		Errore(w, r)
		return
	}
	parola, _ := ultimo(r, "parola")
	s := backup.SceltaConferma{Parola: parola}
	manda := map[int64]bool{}
	valide := true
	for _, v := range r.PostForm["riga"] {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			valide = false
			continue
		}
		if scelta, _ := ultimo(r, "scelta-"+v); scelta == "manda" {
			s.Manda, manda[id] = append(s.Manda, id), true
		} else {
			s.Scarta = append(s.Scarta, id) // la proposta della D-118 (b), anche senza una scelta
		}
	}
	var err error
	if !valide {
		err = backup.ErrCodaCambiata
	} else {
		// Una richiesta chiusa dal browser a metà non lascia la conferma a metà: è una transazione, ma non la si
		// interrompe nemmeno.
		_, err = f.ripristino.Conferma(context.WithoutCancel(r.Context()), s, contestoDi(r))
	}
	switch {
	case err == nil:
		st := statoDi(r)
		st.dati.Info, st.cambiata = msgRipristinoConfermato, true
		f.vaiA(w, r, rottaRipristino)
	case errors.Is(err, backup.ErrNonIsolata) && f.copiaIsolata:
		// Confermata nel frattempo (dalla console o da un'altra scheda): il serve che gira è ancora isolato.
		f.mostraRipristino(w, r, msgRipristinoGiaConfermato, nil)
	case errors.Is(err, backup.ErrNonIsolata):
		f.mostraRipristino(w, r, msgRipristinoNienteDaConfermare, nil)
	case errors.Is(err, backup.ErrParolaSbagliata):
		f.mostraRipristino(w, r, msgParolaSbagliata, manda)
	case errors.Is(err, backup.ErrCodaCambiata):
		f.mostraRipristino(w, r, msgCodaCambiata, manda)
	default:
		f.registro.Error("conferma del ripristino non riuscita", "tipo", fmt.Sprintf("%T", err))
		f.mostraRipristino(w, r, msgConfermaNonRiuscita, manda)
	}
}

// copiaIsolataVista è il banner della copia isolata.
type copiaIsolataVista struct {
	Titolare, Confermata bool
	Rotta                string
}

// senzaBannerIsolata sono le pagine interne senza il banner della copia isolata.
var senzaBannerIsolata = map[string]bool{"firma": true, "ripristino": true}

// bannerCopiaIsolata è il banner di questa pagina, nil fuori da una copia isolata. Uno stato che non si legge
// mostra il banner della copia ancora isolata (il caso più prudente).
func (f *facciaWeb) bannerCopiaIsolata(r *http.Request, nome string, titolare bool) *copiaIsolataVista {
	if !f.copiaIsolata || senzaBannerIsolata[nome] {
		return nil
	}
	v := &copiaIsolataVista{Titolare: titolare, Rotta: rottaRipristino}
	if f.ripristino != nil {
		// Solo lo stato (db.LeggiIsolamento), mai la coda con testi e indirizzi dei clienti.
		if isolata, err := f.ripristino.Isolata(r.Context()); err == nil && !isolata {
			v.Confermata = true
		}
	}
	return v
}

// registraRipristino registra la pagina e la conferma (solo il titolare).
func (f *facciaWeb) registraRipristino(mux *http.ServeMux) {
	f.rottaConGuscio(mux, "GET "+rottaRipristino, soloTitolare, f.paginaRipristino)
	f.rottaConGuscio(mux, "POST "+rottaRipristino, soloTitolare, f.confermaRipristino)
}
