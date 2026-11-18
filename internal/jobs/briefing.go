package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// Il lavoro «Briefing del mattino» (Jobs.Briefing dello standalone a go-baseline, JOB-10, JOB-11; slice 097):
//
//   - **Cinque sezioni** (buste.LeggiBriefing): pronte da avvisare, compro oro da avvisare, in ritardo, in
//     scadenza fra tre giorni, fuori bottega da almeno la soglia del briefing (0 la toglie). Ognuna elenca al più
//     VociPerSezione buste e ne legge una in più: con 21 il titolo dice «(21+)» e l'ultima riga «…e altre ≥1»
//     (l'aritmetica del Flask). Il numero è quello dei testi (D-64, D-82). Niente da dire: «🌅 Briefing gg/mm —
//     nulla all'orizzonte ☕», mandato lo stesso.
//   - **Solo Telegram:** i destinatari li sceglie il pianificatore (il briefing ha solo Telegram, D-16).
//   - **Nessun timbro:** legge e basta; la stessa busta torna ogni mattina finché non cambia.
//   - **Divisione** (dividi, JOB-11): Telegram conta in unità UTF-16 (unitaUTF16) e rifiuta un testo oltre 4096; il
//     briefing sta in MassimoMessaggio unità per parte. Si divide fra le sezioni; una sezione che da sola non sta si
//     divide fra le righe, con il titolo seguito da « (segue)» nelle parti dopo; una riga oltre 3900 unità si taglia
//     fra due grafemi con «…» (mai una coppia surrogata o un'emoji composta a metà).
//   - **Tutto o niente:** tutte le parti si accodano in una transazione, nell'ordine; un rifiuto della coda annulla
//     anche le parti già accodate: «Fallito» con il motivo e zero conteggi. Un guasto che non è un rifiuto annulla lo
//     stesso e la riga dirà DettaglioInterrotto. Le parti sono righe separate della coda: dopo un errore temporaneo
//     dell'invio possono arrivare fuori ordine, come nello standalone.
//   - **Stop:** con il contesto finito il lavoro torna con l'errore del contesto (niente riga, niente in coda).

// MassimoMessaggio è la parte più lunga che il briefing scrive, in unità UTF-16 (@max_message: 4096 di Telegram
// meno un margine).
const MassimoMessaggio = 4000

// VociPerSezione sono le buste che una sezione elenca prima di dire che ce ne sono altre (@cap).
const VociPerSezione = 20

// MassimoDescrizioneBriefing è la descrizione più lunga di una riga, in unità UTF-16 (@max_description).
const MassimoDescrizioneBriefing = 60

// margineTitolo è lo spazio lasciato al titolo di una parte quando si taglia una riga (clip/1).
const margineTitolo = 100

// LavoroBriefing è il lavoro del briefing di un database, da registrare nel pianificatore come Briefing.
type LavoroBriefing struct {
	d     *db.DB
	posta Accodatore
	log   *slog.Logger
}

// NuovoBriefing è il lavoro del briefing sul database d, che accoda con posta (la coda di serve).
func NuovoBriefing(d *db.DB, posta Accodatore) *LavoroBriefing {
	return &LavoroBriefing{d: d, posta: posta, log: slog.New(slog.DiscardHandler)}
}

// ConRegistro dà al lavoro le righe del server: per un briefing rifiutato il motivo (testi fissi, mai indirizzi,
// nomi, numeri o testi dei messaggi).
func (b *LavoroBriefing) ConRegistro(l *slog.Logger) *LavoroBriefing {
	if l != nil {
		b.log = l
	}
	return b
}

// sezioneBriefing è una sezione letta: le voci da mostrare e quante se ne sono lette (al più una in più).
type sezioneBriefing struct {
	titolo string
	voci   []buste.VoceBriefing
	lette  int
	data   bool // la riga ha la data promessa
	fuori  bool // la riga è di fuori bottega
}

// Esegui è run/4.
func (b *LavoroBriefing) Esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	if len(dest) == 0 {
		return Risultato{}, ErrSenzaCanali // il pianificatore non lo fa mai (D-16)
	}
	r, err := b.esegui(ctx, conf, adesso, dest, a)
	if err != nil && ctx.Err() != nil {
		return Risultato{}, ctx.Err() // uno stop, anche se il driver lo dice con un altro errore
	}
	return r, err
}

func (b *LavoroBriefing) esegui(ctx context.Context, conf Config, adesso time.Time, dest []uscita.Destinazione, a audit.Contesto) (Risultato, error) {
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Risultato{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente; senza Commit annulla tutte le parti
	giorni := conf.GiorniBriefing()
	l, err := buste.LeggiBriefing(ctx, tx, adesso, int64(giorni), VociPerSezione+1)
	if err != nil {
		return Risultato{}, err
	}
	sezioni := []sezioneBriefing{
		sezione("✅ Pronte da avvisare", l.Pronte, false),
		sezione("🪙 Compro oro da avvisare", l.CompraOro, false),
		sezione("⚠ In ritardo", l.InRitardo, true),
		sezione("📅 In scadenza 3gg", l.InScadenza, true),
	}
	if giorni > 0 {
		s := sezione(fmt.Sprintf("🛠 Fuori bottega da ≥%dgg", giorni), l.FuoriBottega, false)
		s.fuori = true
		sezioni = append(sezioni, s)
	}
	oggi := clock.DataDi(adesso)
	parti := dividi(componi(sezioni, oggi))
	oggetto := "Briefing " + oggi.Formatta("02/01")
	scritti := 0
	for _, testo := range parti {
		for _, d := range dest {
			if _, err := b.posta.AccodaIn(ctx, tx, d, uscita.Contenuto{Oggetto: &oggetto, Testo: testo}, a); err != nil {
				var motivo string
				switch {
				case errors.Is(err, uscita.ErrNonConfigurato):
					motivo = "Il bot Telegram non è più configurato."
				case errors.Is(err, uscita.ErrMessaggioNonValido), errors.Is(err, uscita.ErrSoloAMano):
					motivo = "La coda ha rifiutato il briefing."
				default:
					return Risultato{}, err
				}
				b.log.Warn("briefing: briefing non accodato", "motivo", motivo)
				return Risultato{Fallito: true, Dettaglio: motivo}, nil // Rollback: nessuna parte
			}
			scritti++
		}
	}
	if err := tx.Commit(); err != nil {
		return Risultato{}, err
	}
	voci := 0
	for _, s := range sezioni {
		voci += len(s.voci)
	}
	return Risultato{Avvisi: voci, Messaggi: scritti, Dettaglio: fraseBriefing(voci, scritti)}, nil
}

// sezione è section/3: le prime VociPerSezione delle lette.
func sezione(titolo string, lette []buste.VoceBriefing, data bool) sezioneBriefing {
	s := sezioneBriefing{titolo: titolo, voci: lette, lette: len(lette), data: data}
	if len(lette) > VociPerSezione {
		s.voci = lette[:VociPerSezione]
	}
	return s
}

// fraseBriefing è detail/2.
func fraseBriefing(voci, messaggi int) string {
	m := fmt.Sprintf("%d messaggi", messaggi)
	if messaggi == 1 {
		m = "1 messaggio"
	}
	switch voci {
	case 0:
		return "Nessuna voce: inviato il briefing vuoto in " + m + "."
	case 1:
		return "1 voce in " + m + "."
	}
	return fmt.Sprintf("%d voci in %s.", voci, m)
}

// componi è render/2: l'intestazione e un blocco per ogni sezione che ha qualcosa da dire; nessuna: la riga
// tranquilla sull'intestazione.
func componi(sezioni []sezioneBriefing, oggi clock.Data) []string {
	testa := "🌅 Briefing " + oggi.Formatta("02/01")
	blocchi := []string{testa}
	for _, s := range sezioni {
		if len(s.voci) > 0 {
			blocchi = append(blocchi, blocco(s))
		}
	}
	if len(blocchi) == 1 {
		return []string{testa + " — nulla all'orizzonte ☕"}
	}
	return blocchi
}

// blocco è block/1: il titolo con quante lette («+» se troncata), le righe e, se troncata, «…e altre ≥N».
func blocco(s sezioneBriefing) string {
	troncata := s.lette > len(s.voci)
	titolo := fmt.Sprintf("%s (%d", s.titolo, s.lette)
	if troncata {
		titolo += "+"
	}
	righe := []string{titolo + ")"}
	for _, v := range s.voci {
		righe = append(righe, riga(v, s))
	}
	if troncata {
		righe = append(righe, fmt.Sprintf("…e altre ≥%d", s.lette-len(s.voci)))
	}
	return strings.Join(righe, "\n")
}

// riga è line/2; il numero è quello dei testi (D-64, D-82), lo standalone scrive bag_number.
func riga(v buste.VoceBriefing, s sezioneBriefing) string {
	qr := ""
	if v.CodiceQR != nil {
		qr = *v.CodiceQR
	}
	r := "• #" + buste.NumeroNeiTesti(qr, v.Numero) + " " + nome(v.Cliente)
	if s.fuori {
		return fmt.Sprintf("%s da %s · %dgg", r, v.Artigiano, v.GiorniFuori)
	}
	if d := strings.TrimFunc(v.Descrizione, unicode.IsSpace); d != "" {
		r += " — " + tagliaDescrizioneBriefing(d)
	}
	if s.data {
		r += " (" + dataItaliana(v.Prevista) + ")"
	}
	return r
}

// nome è name/1: il nome senza spazi ai bordi, «—» se vuoto.
func nome(cliente string) string {
	if t := strings.TrimFunc(cliente, unicode.IsSpace); t != "" {
		return t
	}
	return "—"
}

// tagliaDescrizioneBriefing è cut/1: oltre 60 unità UTF-16, i primi 57 grafemi e «…». Lo standalone misura in unità
// e taglia in grafemi: 31 emoji (62 unità) restano tutte, con «…».
func tagliaDescrizioneBriefing(s string) string {
	if unitaUTF16(s) <= MassimoDescrizioneBriefing {
		return s
	}
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < MassimoDescrizioneBriefing-3 && g.Next(); i++ {
		_, fine = g.Positions()
	}
	return s[:fine] + "…"
}

// dataItaliana è date/1 (%d/%m/%Y) di una data aaaa-mm-gg, rigirata senza controllarla: la colonna accetta con il
// suo GLOB anche una data che il calendario non ha, e il briefing la scrive com'è invece di fermarsi.
func dataItaliana(d string) string {
	if len(d) != len("2006-01-02") {
		return d
	}
	return d[8:10] + "/" + d[5:7] + "/" + d[0:4]
}

// unitaUTF16 è size/1: il testo in unità UTF-16, come lo conta Telegram (una runa fuori dal BMP vale 2).
func unitaUTF16(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

// dividi è pack/1: i blocchi (intestazione e sezioni) uniti con una riga vuota finché la parte sta in
// MassimoMessaggio unità; un blocco che da solo non sta si divide fra le righe (adatta).
func dividi(blocchi []string) []string {
	var parti []string
	for _, b := range blocchi {
		for _, pezzo := range adatta(b) {
			if n := len(parti); n > 0 && unitaUTF16(parti[n-1])+2+unitaUTF16(pezzo) <= MassimoMessaggio {
				parti[n-1] += "\n\n" + pezzo
				continue
			}
			parti = append(parti, pezzo)
		}
	}
	return parti
}

// adatta è fit/1 e split/3: un blocco che sta resta intero; uno che non sta si taglia fra le righe (ognuna
// accorciata da accorcia), almeno una riga per parte, con il titolo e « (segue)» dalla seconda parte.
func adatta(b string) []string {
	if unitaUTF16(b) <= MassimoMessaggio {
		return []string{b}
	}
	righe := strings.Split(b, "\n")
	titolo, righe := righe[0], righe[1:]
	for i := range righe {
		righe[i] = accorcia(righe[i])
	}
	var parti []string
	corrente := titolo
	for len(righe) > 0 {
		presa := []string{corrente, righe[0]}
		misura := unitaUTF16(corrente) + 1 + unitaUTF16(righe[0])
		righe = righe[1:]
		for len(righe) > 0 && misura+1+unitaUTF16(righe[0]) <= MassimoMessaggio {
			misura += 1 + unitaUTF16(righe[0])
			presa = append(presa, righe[0])
			righe = righe[1:]
		}
		parti = append(parti, strings.Join(presa, "\n"))
		corrente = titolo + " (segue)"
	}
	return parti
}

// accorcia è clip/1: una riga oltre MassimoMessaggio-100 unità tiene i grafemi interi che stanno in una unità meno
// del limite e prende «…».
func accorcia(r string) string {
	limite := MassimoMessaggio - margineTitolo
	if unitaUTF16(r) <= limite {
		return r
	}
	g := uniseg.NewGraphemes(r)
	usate, fine := 0, 0
	for g.Next() {
		usate += unitaUTF16(g.Str())
		if usate > limite-1 {
			break
		}
		_, fine = g.Positions()
	}
	return r[:fine] + "…"
}
