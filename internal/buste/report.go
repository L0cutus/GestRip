package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
)

// Il report multi-scheda A4 (RIP-S-46, GET /repairs/report.pdf), come RepairReportController,
// RepairIndex.selection_by_ids/1 e RepairReport dello standalone a go-baseline:
//   - ids=3,1,2 (named_ids/1): le voci divise sulle virgole, senza spazi ai bordi (String.trim/1), le
//     vuote tolte, ognuna con Id.positive/1, i doppioni tolti tenendo il primo; la prima voce che non è
//     un id ferma tutto (SelezioneNonValida), anche oltre i 64 bit (Id.positive/1, misurato: qui lo
//     standalone non dà 500, quindi D-56 non vale); nessuna voce è la variante dei filtri della home
//     (ErrSelezioneDaiFiltri, poi IdsDaiFiltri: from_filters/1, slice 051);
//   - al più MaxSchedeReport buste diverse, controllato prima di cercarle (TroppeSchede);
//   - le buste si cercano tutte (il cestino è filtrato come in Repo.prepare_query/3) con oreficeria e
//     artigiano; se ne manca una, SchedeNonTrovate con gli id mancanti nell'ordine chiesto: niente PDF
//     e niente righe;
//   - il PDF lo fa il chiamante (fai); poi una riga repair.report_generated per busta, nell'ordine,
//     con il numero (record_repair_event/3). La prima riga che non si scrive ferma il documento
//     (ErrPDFNonFatto) e le righe già scritte restano, come audit/2 con reduce_while.
// Non è un'apertura della scheda: niente repair.viewed.

// MaxSchedeReport è max_sheets/0: quante buste può avere un report.
const MaxSchedeReport = 50

// ErrSelezioneDaiFiltri: nessun id nella query (ids assente, vuoto o di sole virgole): il report è la
// selezione della home con i suoi filtri (from_filters/1), che dà IdsDaiFiltri.
var ErrSelezioneDaiFiltri = errors.New("buste: report dai filtri della home")

// ErrSelezioneVuota è il 422 di from_filters/1 quando i filtri non trovano nessuna busta, con il testo
// MsgSelezioneVuota.
var ErrSelezioneVuota = errors.New("buste: i filtri non trovano nessuna busta")

// MsgSelezioneVuota è il testo del 422 di ErrSelezioneVuota.
const MsgSelezioneVuota = "Nessuna riparazione da stampare: il filtro selezionato non trova nessuna busta."

// SelezioneNonValida è il 422 di una selezione che non si può usare: una voce di ids che non è un id,
// o un ids che per Plug non è un testo (Valore è allora inspect/1 del valore).
type SelezioneNonValida struct{ Valore string }

func (e SelezioneNonValida) Error() string {
	return "Selezione non valida: «" + e.Valore + "» non è un numero di riparazione."
}

// TroppeSchede è il 422 di too_many/2: N buste diverse, più di MaxSchedeReport.
type TroppeSchede struct{ N int }

func (e TroppeSchede) Error() string {
	return fmt.Sprintf("Troppe riparazioni per un solo report: %d selezionate, il massimo è %d. Restringi la selezione.",
		e.N, MaxSchedeReport)
}

// SchedeNonTrovate è il 404 di from_ids/1: le buste chieste che non ci sono, nell'ordine chiesto.
type SchedeNonTrovate struct{ ID []int64 }

func (e SchedeNonTrovate) Error() string {
	parti := make([]string, len(e.ID))
	for i, id := range e.ID {
		parti[i] = strconv.FormatInt(id, 10)
	}
	return "Riparazione non trovata: " + strings.Join(parti, ", ") + ". Nessun report generato."
}

// DatiReport è ciò che il report stampa: l'intestazione e una scheda per busta, nell'ordine.
type DatiReport struct {
	NomeNegozio string // shop_name/0
	Oggi        string // «Generato il», la data di oggi in negozio, gg/mm/aaaa
	Schede      []SchedaReport
}

// SchedaReport è una scheda del report, già pronta (sheet/1). I campi sempre presenti hanno «—» al
// posto di un valore vuoto; per quelli facoltativi "" vuol dire che la riga non c'è.
type SchedaReport struct {
	ID          int64
	Numero      string // senza «#»
	NomeCliente string // la copia salvata sulla busta, o «—»
	Telefono    string // la copia salvata sulla busta, o «—»
	// Stato è la parola con gli spazi al posto di «_» (status_badge/1), «—» senza stato; ClasseStato
	// è la stessa con i trattini, per i colori del badge ("" senza stato).
	Stato, ClasseStato string
	DataRiparazione    string // gg/mm/aaaa, o «—»
	Consegna           string // il testo scritto, la data gg/mm/aaaa, o «—»
	Oreficeria         string // il nome dell'oreficeria della busta, se c'è
	Artigiano          string // il nome dell'artigiano della busta, se c'è
	Importi            string // «€ 400 → € 450,00» (money_field/1), o ""
	Descrizione        string // o «—»
	Note, NoteInterne  string // "" = nessuna riga
	Pronta, Ritirata   string // gg/mm/aaaa, o "" = nessuna voce
}

// NomeFileReport è filename/1: «schede_<N>.pdf».
func NomeFileReport(n int) string { return "schede_" + strconv.Itoa(n) + ".pdf" }

// IdsDelReport legge il testo di ids come named_ids/1 e from_ids/1 fino al limite.
func IdsDelReport(s string) ([]int64, error) {
	var ids []int64
	visti := map[int64]bool{}
	for _, voce := range strings.Split(s, ",") {
		voce = strings.TrimFunc(voce, unicode.IsSpace)
		if voce == "" {
			continue
		}
		id, ok := idPositivo(voce)
		if !ok {
			return nil, SelezioneNonValida{Valore: voce}
		}
		if !visti[id] {
			visti[id] = true
			ids = append(ids, id)
		}
	}
	switch {
	case len(ids) == 0:
		return nil, ErrSelezioneDaiFiltri
	case len(ids) > MaxSchedeReport:
		return nil, TroppeSchede{N: len(ids)}
	}
	return ids, nil
}

// IdsDaiFiltri è from_filters/1: le buste della selezione della home con i filtri f, nell'ordine della
// lista e senza la pagina (Selezione), caricate al più fino a una oltre il limite. Nessuna busta è
// ErrSelezioneVuota, più di MaxSchedeReport è TroppeSchede con il conteggio intero.
func (b *Buste) IdsDaiFiltri(ctx context.Context, f Filtri) ([]int64, error) {
	n, ids, err := b.Selezione(ctx, f, MaxSchedeReport+1)
	switch {
	case err != nil:
		return nil, err
	case n == 0:
		return nil, ErrSelezioneVuota
	case n > MaxSchedeReport:
		return nil, TroppeSchede{N: int(n)}
	}
	return ids, nil
}

// Report legge le buste ids (già lette da IdsDelReport), fa il PDF con fai e, solo se è fatto, scrive
// repair.report_generated per ogni busta, per chi lo chiede (c). Errori: SchedeNonTrovate (nessun PDF
// e nessuna riga), ErrPDFNonFatto, o un guasto del database.
func (b *Buste) Report(ctx context.Context, ids []int64, c audit.Contesto, fai func(DatiReport) ([]byte, error)) ([]byte, DatiReport, error) {
	if len(ids) == 0 || len(ids) > MaxSchedeReport {
		return nil, DatiReport{}, fmt.Errorf("buste: report con %d buste", len(ids)) // IdsDelReport non lo dà
	}
	d, err := b.leggiReport(ctx, ids)
	if err != nil {
		return nil, DatiReport{}, err
	}
	pdf, err := fai(d)
	if err != nil {
		return nil, d, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	for _, s := range d.Schede {
		if err := b.reg.Registra(ctx, c, "repair.report_generated", "repair", s.ID, "ok", map[string]any{"bag_number": s.Numero}); err != nil {
			return nil, d, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
		}
	}
	return pdf, d, nil
}

func (b *Buste) leggiReport(ctx context.Context, ids []int64) (DatiReport, error) {
	segnaposti := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	// Il profilo del negozio nella stessa lettura delle buste: nome e schede vengono dalla stessa
	// fotografia del database.
	righe, err := b.d.Lettura.QueryContext(ctx, `SELECT b.id, b.numero, b.nome_cliente, b.telefono_cliente, b.stato,
		b.data_accettazione, b.data_prevista_testo, b.data_prevista, o.nome, a.nome, b.preventivo_testo, b.preventivo_centesimi,
		b.prezzo_testo, b.prezzo_centesimi, b.descrizione, b.note_pubbliche, b.note_interne, b.data_pronta, b.data_consegna,
		p.nome
		FROM buste b LEFT JOIN oreficerie o ON o.id = b.oreficeria_id LEFT JOIN artigiani a ON a.id = b.artigiano_id
		LEFT JOIN profilo_negozio p ON p.ambito = 'default'
		WHERE b.id IN (`+segnaposti+`) AND b.cancellata_il IS NULL`, args...)
	if err != nil {
		return DatiReport{}, err
	}
	defer righe.Close()
	trovate := map[int64]SchedaReport{}
	var negozio sql.NullString
	for righe.Next() {
		var s SchedaReport
		var stato, accettata, descrizione string
		var consTesto, consData, oreficeria, artigiano, prevTesto, finTesto, note, interne, pronta, ritirata sql.NullString
		var prevCent, finCent sql.NullInt64
		if err := righe.Scan(&s.ID, &s.Numero, &s.NomeCliente, &s.Telefono, &stato, &accettata, &consTesto, &consData,
			&oreficeria, &artigiano, &prevTesto, &prevCent, &finTesto, &finCent, &descrizione, &note, &interne, &pronta,
			&ritirata, &negozio); err != nil {
			return DatiReport{}, err
		}
		s.NomeCliente, s.Telefono, s.Descrizione = trattino(s.NomeCliente), trattino(s.Telefono), trattino(descrizione)
		s.Stato, s.ClasseStato = "—", ""
		if stato != "" {
			s.Stato, s.ClasseStato = strings.ReplaceAll(stato, "_", " "), strings.ReplaceAll(stato, "_", "-")
		}
		s.DataRiparazione = trattino(dataGiornoMeseAnno(accettata))
		s.Consegna = trattino(prevista(consTesto, consData, dataGiornoMeseAnno))
		s.Oreficeria, s.Artigiano = oreficeria.String, artigiano.String
		s.Importi = importiReport(prevTesto, prevCent, finTesto, finCent)
		s.Note, s.NoteInterne = note.String, interne.String
		if pronta.Valid {
			s.Pronta = dataGiornoMeseAnno(pronta.String)
		}
		if ritirata.Valid {
			s.Ritirata = dataGiornoMeseAnno(ritirata.String)
		}
		trovate[s.ID] = s
	}
	if err := righe.Err(); err != nil {
		return DatiReport{}, err
	}
	d := DatiReport{NomeNegozio: nomeNegozioDiRipiego, Oggi: clock.Oggi(b.o).Formatta("02/01/2006")}
	if negozio.Valid && negozio.String != "" {
		d.NomeNegozio = negozio.String
	}
	var mancanti []int64
	for _, id := range ids {
		s, ok := trovate[id]
		if !ok {
			mancanti = append(mancanti, id)
			continue
		}
		d.Schede = append(d.Schede, s)
	}
	if mancanti != nil {
		return DatiReport{}, SchedeNonTrovate{ID: mancanti}
	}
	return d, nil
}

// trattino è value/1: «—» al posto di un testo vuoto.
func trattino(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// importiReport è money_field/1: preventivo e prezzo finale da price_source/2, «€ » davanti al testo
// scritto (com'è, non Money.label/1) o ai centesimi con la virgola, uniti da « → »; "" se mancano tutti
// e due.
func importiReport(prevTesto sql.NullString, prevCent sql.NullInt64, finTesto sql.NullString, finCent sql.NullInt64) string {
	var parti []string
	for _, p := range []struct {
		testo sql.NullString
		cent  sql.NullInt64
	}{{prevTesto, prevCent}, {finTesto, finCent}} {
		switch {
		case p.testo.Valid && p.testo.String != "":
			parti = append(parti, "€ "+p.testo.String)
		case p.cent.Valid:
			parti = append(parti, EtichettaCentesimi(p.cent.Int64))
		}
	}
	return strings.Join(parti, " → ")
}
