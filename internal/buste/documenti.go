package buste

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// L'etichetta busta (RIP-S-43) e la scheda PDF (RIP-S-45), come BagLabel, BagLabelController,
// RepairSheet e RepairSheetController dello standalone a go-baseline:
//   - la busta si cerca come la ricevuta (get_repair!/1: cast di :id, filtro del cestino), con
//     artigiano e oreficeria letti nella stessa query (preload_repair/1);
//   - il PDF lo fa il chiamante (fai); solo se è fatto si scrive la riga della sua azione
//     (repair.label_generated, repair.sheet_generated) con il numero della busta
//     (Workshop.record_repair_event/3). Senza la riga il PDF non esce. Non sono aperture della scheda:
//     niente repair.viewed;
//   - la firma della scheda si legge dall'archivio con la verifica dell'impronta
//     (MediaStore.read_object/1), senza la riga repair.signature_viewed (la scheda dello standalone
//     non la scrive); una firma che non si legge non dà il blocco.

// ErrPDFNonFatto: il PDF non si è fatto o la sua riga d'audit non si è scritta: il documento non si
// consegna (503 «PDF non disponibile»).
var ErrPDFNonFatto = errors.New("buste: PDF non fatto")

// DichiarazioneFirma è la frase che il cliente legge sulla pagina di firma e che la scheda stampa
// sopra la firma (@declaration di DeliverySignature, app_firma_bp.py:22-23).
const DichiarazioneFirma = "Dichiaro di aver ritirato l'oggetto sopra indicato e di accettarne lo stato."

// DatiEtichetta è ciò che l'etichetta busta stampa, già pronto.
type DatiEtichetta struct {
	ID          int64
	Numero      string   // il numero interno: nome del file, registro, titolo del lavoro di stampa
	Visibile    Mostrato // il numero grande (D-64, slice 058): «#0045/26», «N. 45/26»
	Artigiano   string   // il nome dell'artigiano della busta; "" = nessuna riga «→»
	QR          string   // il contenuto del QR (ValoreQR): non cambia con la D-64
	Oggi        string   // «All'artigiano:» (D-73), la data di oggi in negozio, gg/mm/aaaa
	Scadenza    string   // «Scad.:», il testo scritto o la data AAAA-MM-GG; "" = nessuna
	Stato       string   // lo stato con gli spazi al posto di «_»; "" = nessun riquadro
	Descrizione string   // DescrizioneEtichetta
}

// ValoreQR è qr_value/1 (app.py:1677, `qr_code or numero_busta`): il codice salvato così com'è, spazi
// compresi; solo NULL e "" lasciano il posto al numero busta.
func ValoreQR(codice *string, numero string) string {
	if codice == nil || *codice == "" {
		return numero
	}
	return *codice
}

// La descrizione dell'etichetta: truncate(180, True, '…') di Jinja con la tolleranza di 5.
const (
	taglioDescrizione     = 180
	tolleranzaDescrizione = 5
)

// DescrizioneEtichetta è description/1: «—» se vuota; fino a 185 grafemi intera, oltre i primi 179 e
// «…» (String.length/1 e String.slice/3 contano i grafemi).
func DescrizioneEtichetta(s string) string {
	if s == "" {
		return "—"
	}
	if uniseg.GraphemeClusterCount(s) <= taglioDescrizione+tolleranzaDescrizione {
		return s
	}
	var b strings.Builder
	g := uniseg.NewGraphemes(s)
	for i := 0; i < taglioDescrizione-1 && g.Next(); i++ {
		b.WriteString(g.Str())
	}
	return b.String() + "…"
}

// NomeFileEtichetta è filename/1: «etichetta_busta_<numero con / → _>.pdf».
func NomeFileEtichetta(numero string) string {
	return "etichetta_busta_" + strings.ReplaceAll(numero, "/", "_") + ".pdf"
}

// NomeFileSchedaPDF è filename/1 della scheda: «busta_<numero con / → _>.pdf».
func NomeFileSchedaPDF(numero string) string {
	return "busta_" + strings.ReplaceAll(numero, "/", "_") + ".pdf"
}

// DatiSchedaPDF è ciò che la scheda PDF stampa, già pronto. "" vuol dire «niente».
type DatiSchedaPDF struct {
	ID                           int64
	Numero                       string   // il numero interno: nome del file e registro
	Visibile                     Mostrato // sotto «N° BUSTA» (D-64, slice 058): «0045/26», «N. 45/26»
	NomeNegozio                  string   // shop_name/0
	Oreficeria                   string   // il badge: il nome dell'oreficeria della busta
	DataAccettazione             string   // gg/mm/aaaa
	Consegna                     string   // il testo scritto, la data gg/mm/aaaa, o ""
	NomeCliente, TelefonoCliente string   // le copie salvate sulla busta
	Artigiano                    string
	Descrizione                  string
	NotePubbliche, NoteInterne   string
	Preventivo                   string // «testo €», «€ 12,50» dai soli centesimi, o "" (&nbsp;)
	CompraOro                    bool
	Firma                        *FirmaScheda // nil: nessun blocco della firma
}

// FirmaScheda è il blocco «Consegna — Firma del cliente» (signature_block/1): la riga «Firmata …» e i
// byte della firma, letti e verificati.
type FirmaScheda struct {
	Riga string
	PNG  []byte
}

// Etichetta legge la busta id, fa il PDF dell'etichetta con fai e, solo se è fatto, scrive
// repair.label_generated per chi la chiede (c). Errori come Ricevuta, con ErrPDFNonFatto.
func (b *Buste) Etichetta(ctx context.Context, id string, c audit.Contesto, fai func(DatiEtichetta) ([]byte, error)) ([]byte, DatiEtichetta, error) {
	n, err := idDellaRicevuta(id)
	if err != nil {
		return nil, DatiEtichetta{}, err
	}
	e, err := b.leggiEtichetta(ctx, n)
	if err != nil {
		return nil, DatiEtichetta{}, err
	}
	pdf, err := b.consegna(ctx, c, "repair.label_generated", e.ID, e.Numero, func() ([]byte, error) { return fai(e) })
	return pdf, e, err
}

// SchedaPDF legge la busta id (con la firma), fa il PDF della scheda con fai e, solo se è fatto,
// scrive repair.sheet_generated. Errori come Ricevuta, con ErrPDFNonFatto.
func (b *Buste) SchedaPDF(ctx context.Context, id string, c audit.Contesto, fai func(DatiSchedaPDF) ([]byte, error)) ([]byte, DatiSchedaPDF, error) {
	n, err := idDellaRicevuta(id)
	if err != nil {
		return nil, DatiSchedaPDF{}, err
	}
	s, err := b.leggiSchedaPDF(ctx, n)
	if err != nil {
		return nil, DatiSchedaPDF{}, err
	}
	pdf, err := b.consegna(ctx, c, "repair.sheet_generated", s.ID, s.Numero, func() ([]byte, error) { return fai(s) })
	return pdf, s, err
}

// consegna fa il PDF e poi scrive la sua riga d'audit: il with del controller dello standalone.
func (b *Buste) consegna(ctx context.Context, c audit.Contesto, azione string, id int64, numero string, fai func() ([]byte, error)) ([]byte, error) {
	pdf, err := fai()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	if err := b.reg.Registra(ctx, c, azione, "repair", id, "ok", map[string]any{"bag_number": numero}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPDFNonFatto, err)
	}
	return pdf, nil
}

// prevista è Repair.date_source/2: il testo scritto se non è vuoto, altrimenti la data (nel formato di
// chi chiama), altrimenti "".
func prevista(testo, data sql.NullString, formato func(string) string) string {
	switch {
	case testo.Valid && testo.String != "":
		return testo.String
	case data.Valid:
		return formato(data.String)
	}
	return ""
}

// dataGiornoMeseAnno scrive gg/mm/aaaa una data AAAA-MM-GG (Clock.format(date, "%d/%m/%Y")).
func dataGiornoMeseAnno(s string) string {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return s // lo schema vuole AAAA-MM-GG: non succede
	}
	return t.Format("02/01/2006")
}

func (b *Buste) leggiEtichetta(ctx context.Context, id int64) (DatiEtichetta, error) {
	var e DatiEtichetta
	var qr, consTesto, consData, artigiano sql.NullString
	var stato string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.data_prevista_testo, b.data_prevista,
		b.stato, b.descrizione, a.nome
		FROM buste b LEFT JOIN artigiani a ON a.id = b.artigiano_id
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(&e.ID, &e.Numero, &qr, &consTesto, &consData, &stato, &e.Descrizione, &artigiano)
	if errors.Is(err, sql.ErrNoRows) {
		return DatiEtichetta{}, ErrBustaNonTrovata
	}
	if err != nil {
		return DatiEtichetta{}, err
	}
	e.QR = ValoreQR(nullaTesto(qr), e.Numero)
	e.Visibile = NumeroMostrato(qr.String, e.Numero)
	e.Artigiano = artigiano.String
	e.Oggi = clock.Oggi(b.o).Formatta("02/01/2006")
	// Il testo scritto com'è; la data come to_string/1 di un Date, cioè AAAA-MM-GG.
	e.Scadenza = prevista(consTesto, consData, func(s string) string { return s })
	e.Stato = strings.ReplaceAll(stato, "_", " ")
	e.Descrizione = DescrizioneEtichetta(e.Descrizione)
	return e, nil
}

func (b *Buste) leggiSchedaPDF(ctx context.Context, id int64) (DatiSchedaPDF, error) {
	var s DatiSchedaPDF
	var accettata string
	var qr, consTesto, consData, note, interne, prevTesto, oreficeria, artigiano, negozio, chiave, firmaIl, ritirataDa sql.NullString
	var prevCent sql.NullInt64
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT b.id, b.numero, b.codice_qr, b.data_accettazione, b.data_prevista_testo, b.data_prevista,
		b.nome_cliente, b.telefono_cliente, b.descrizione, b.note_pubbliche, b.note_interne, b.preventivo_testo,
		b.preventivo_centesimi, b.compro_oro, o.nome, a.nome, p.nome, b.firma_chiave, b.firma_il, b.firma_ritirata_da
		FROM buste b LEFT JOIN oreficerie o ON o.id = b.oreficeria_id LEFT JOIN artigiani a ON a.id = b.artigiano_id
		LEFT JOIN profilo_negozio p ON p.ambito = 'default'
		WHERE b.id = ? AND b.cancellata_il IS NULL`, id).Scan(&s.ID, &s.Numero, &qr, &accettata, &consTesto, &consData,
		&s.NomeCliente, &s.TelefonoCliente, &s.Descrizione, &note, &interne, &prevTesto, &prevCent, &s.CompraOro,
		&oreficeria, &artigiano, &negozio, &chiave, &firmaIl, &ritirataDa)
	if errors.Is(err, sql.ErrNoRows) {
		return DatiSchedaPDF{}, ErrBustaNonTrovata
	}
	if err != nil {
		return DatiSchedaPDF{}, err
	}
	s.Visibile = NumeroMostrato(qr.String, s.Numero)
	s.DataAccettazione = dataGiornoMeseAnno(accettata)
	s.Consegna = prevista(consTesto, consData, dataGiornoMeseAnno)
	s.NotePubbliche, s.NoteInterne = note.String, interne.String
	s.Oreficeria, s.Artigiano = oreficeria.String, artigiano.String
	s.NomeNegozio = nomeNegozioDiRipiego
	if negozio.Valid && negozio.String != "" {
		s.NomeNegozio = negozio.String
	}
	// estimate/1: il testo scritto con « €», o i soli centesimi con Money.label/1, o niente.
	switch {
	case prevTesto.Valid && prevTesto.String != "":
		s.Preventivo = prevTesto.String + " €"
	case prevCent.Valid:
		s.Preventivo = EtichettaCentesimi(prevCent.Int64)
	}
	if chiave.Valid {
		s.Firma = b.firmaDellaScheda(chiave.String, firmaIl, ritirataDa)
	}
	return s, nil
}

// firmaDellaScheda è signature_block/1: i byte dall'archivio (impronta verificata), o nil se non si
// leggono o se non c'è l'archivio.
func (b *Buste) firmaDellaScheda(chiave string, il, ritirataDa sql.NullString) *FirmaScheda {
	if b.archivio == nil {
		return nil
	}
	png, err := b.archivio.Leggi(chiave)
	if err != nil {
		return nil
	}
	riga := "Firmata"
	// format_stamp/1: il timbro sull'orologio del negozio; senza istante (un import storico) lo dice.
	if t, err := time.Parse(db.FormatoIstante, il.String); il.Valid && err == nil {
		riga += " il " + clock.Timbro(t)
	} else {
		riga += " (data non registrata)"
	}
	if ritirataDa.Valid {
		riga += " — ritirata da " + ritirataDa.String
	}
	return &FirmaScheda{Riga: riga, PNG: png}
}
