package buste

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina pubblica del lotto /lotto/{gettone} e le sue foto (slice 092, ORE-14, ORE-15, ORE-16), come
// PublicLotController.show/2 e photo/2 dello standalone (go-baseline) con ReadyLots.get_public_lot/1,
// public_members/1, close_if_complete/2, member/2 e RepairPhotos.first_visible_by_repair/2 e read_public_media/5.
//
// Il gettone è tutta l'autorizzazione: il lotto si cerca per **uguaglianza esatta** del gettone, solo inviato e con
// l'accesso pubblico non spento (domanda G, Stefano 17/11/2026). Nessun controllo di lunghezza o di alfabeto oltre
// «non vuoto»: i gettoni del Flask (secrets.token_urlsafe(24), 32 caratteri) arrivano intatti con l'import, e i
// «disabled-import-…» hanno l'accesso spento. Un gettone sbagliato, un lotto non inviato o spento sono lo stesso
// rifiuto (ErrLottoNonDisponibile), deciso qui prima che la pagina lo veda.
//
// Leggere la pagina chiude il lotto completo (ORE-15): la stessa scrittura condizionata della consegna
// (chiudiLottoCompleto, slice 212), una volta sola, con le buste nel cestino che contano. È una scrittura su una GET,
// come nello standalone e nel Flask; non porta niente della richiesta, quindi un'anteprima di un'app di messaggi
// arriva solo allo stato a cui arriverebbe la prossima visita vera. Nessuna riga d'audit, né per la pagina né per la
// chiusura. Il riquadro «Lotto completato» segue chiuso_il riletto dopo la scrittura, mai le sole buste visibili.

// BustaLottoPubblico è una riga della pagina pubblica del lotto.
type BustaLottoPubblico struct {
	ID          int64
	Numero      string // il numero visibile della D-64 («N. 45/26», «0045/26»)
	Descrizione string
	Stato       string
	DataPronta  *string // AAAA-MM-GG
	DataRitiro  *string // AAAA-MM-GG (data_consegna)
	Prezzo      Prezzo  // il prezzo finale
	Preventivo  Prezzo
	FotoID      *int64 // la prima foto che l'oreficeria può vedere, se c'è
}

// Ritirata è collected?/1: la busta è consegnata.
func (x BustaLottoPubblico) Ritirata() bool { return x.Stato == "consegnata" }

// prezzoMostrato è shown_price/1: il prezzo finale se ne ha uno (testo non vuoto o centesimi), altrimenti il
// preventivo. Dà se c'è un prezzo da mostrare e i centesimi esatti dello stesso campo (nil: niente da sommare).
func (x BustaLottoPubblico) prezzoMostrato() (Prezzo, bool, bool) {
	if haPrezzo(x.Prezzo) {
		return x.Prezzo, false, true
	}
	return x.Preventivo, true, haPrezzo(x.Preventivo)
}

// haPrezzo è price_source/2 diverso da :none.
func haPrezzo(p Prezzo) bool { return (p.Testo != nil && *p.Testo != "") || p.Centesimi != nil }

// EtichettaPrezzo è price_label/1: il prezzo finale con Money.label/1 («€ 35», «€ 400/500», «€ 12,50»); il
// preventivo mostrato al suo posto com'è scritto, o «€ 12,50» se ha solo i centesimi (slice 215); "" senza prezzo.
func (x BustaLottoPubblico) EtichettaPrezzo() string {
	p, preventivo, ok := x.prezzoMostrato()
	switch {
	case !ok:
		return ""
	case !preventivo:
		return EtichettaPrezzo(p)
	case p.Testo != nil && *p.Testo != "":
		return *p.Testo
	}
	return EtichettaCentesimi(*p.Centesimi)
}

// PaginaLottoPubblico è quello che la pagina pubblica del lotto mostra.
type PaginaLottoPubblico struct {
	ID         int64
	Oreficeria string
	Aperto     time.Time // inserted_at: «lotto del …»
	Buste      []BustaLottoPubblico
	Completato bool // chiuso_il riletto dopo la chiusura alla lettura
	Profilo    ProfiloNegozio
}

// Totale è total/1: la somma dei centesimi esatti delle buste che li hanno e quante non li hanno (senza prezzo o
// con un testo che non è un importo, come «400/500»: contate, mai lette come zero).
func (p PaginaLottoPubblico) Totale() (centesimi int64, senzaImporto int) {
	for _, x := range p.Buste {
		pr, _, ok := x.prezzoMostrato()
		if !ok || pr.Centesimi == nil {
			senzaImporto++
			continue
		}
		centesimi += *pr.Centesimi
	}
	return centesimi, senzaImporto
}

// ConTotale è total?/2: almeno una busta ha un importo esatto (anche zero).
func (p PaginaLottoPubblico) ConTotale() bool {
	_, senza := p.Totale()
	return len(p.Buste) > senza
}

// DaRitirare è to_collect/1: le buste non ancora consegnate.
func (p PaginaLottoPubblico) DaRitirare() int {
	n := 0
	for _, x := range p.Buste {
		if !x.Ritirata() {
			n++
		}
	}
	return n
}

// lottoPubblico è get_public_lot/1: id e oreficeria del lotto inviato e acceso con esattamente quel gettone; ok
// falso per ogni altro caso, anche un gettone vuoto.
func (b *Buste) lottoPubblico(ctx context.Context, gettone string) (id, oreficeria int64, nome string, aperto time.Time, ok bool, err error) {
	if gettone == "" {
		return 0, 0, "", time.Time{}, false, nil
	}
	var a string
	err = b.d.Lettura.QueryRowContext(ctx, `SELECT l.id, l.oreficeria_id, o.nome, l.creato_il FROM lotti_avviso l
		JOIN oreficerie o ON o.id = l.oreficeria_id
		WHERE l.token = ? AND l.inviato_il IS NOT NULL AND l.accesso_pubblico_spento_il IS NULL`, gettone).Scan(&id, &oreficeria, &nome, &a)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, "", time.Time{}, false, nil
	}
	if err != nil {
		return 0, 0, "", time.Time{}, false, err
	}
	if aperto, err = time.Parse(db.FormatoIstante, a); err != nil {
		return 0, 0, "", time.Time{}, false, err
	}
	return id, oreficeria, nome, aperto, true, nil
}

// PaginaLottoPubblica è show/2: il lotto del gettone con le sue buste (public_members/1: ancora di quell'oreficeria,
// fuori dal cestino, dalla più recente), la chiusura alla lettura, la prima foto per l'oreficeria di ogni busta e il
// profilo del negozio. ErrLottoNonDisponibile per ogni rifiuto.
func (b *Buste) PaginaLottoPubblica(ctx context.Context, gettone string) (PaginaLottoPubblico, error) {
	id, oref, nome, aperto, ok, err := b.lottoPubblico(ctx, gettone)
	if err != nil {
		return PaginaLottoPubblico{}, err
	}
	if !ok {
		return PaginaLottoPubblico{}, ErrLottoNonDisponibile
	}
	p := PaginaLottoPubblico{ID: id, Oreficeria: nome, Aperto: aperto}
	if p.Buste, err = b.membriPubblici(ctx, id, oref); err != nil {
		return PaginaLottoPubblico{}, err
	}
	if err := b.chiudiAllaLettura(ctx, id); err != nil {
		return PaginaLottoPubblico{}, err
	}
	var chiuso sql.NullString
	if err := b.d.Lettura.QueryRowContext(ctx, `SELECT chiuso_il FROM lotti_avviso WHERE id = ?`, id).Scan(&chiuso); err != nil {
		return PaginaLottoPubblico{}, err
	}
	p.Completato = chiuso.Valid
	for i := range p.Buste {
		x := &p.Buste[i]
		v, err := b.VisibiliPer(ctx, BustaDellaFoto{ID: x.ID, OreficeriaID: &oref}, PubblicoOreficeria)
		if err != nil {
			return PaginaLottoPubblico{}, err
		}
		if len(v) > 0 {
			x.FotoID = &v[0].ID
		}
	}
	if p.Profilo, err = b.LeggiProfiloNegozio(ctx); err != nil {
		return PaginaLottoPubblico{}, err
	}
	return p, nil
}

// chiudiAllaLettura è close_if_complete/1 chiamata da show/2, con l'istante di adesso.
//
// Solo del Go, senza effetti visibili: prima una lettura chiede se il lotto si chiuderebbe, e solo allora si prende la
// connessione di scrittura (BEGIN IMMEDIATE). Una pagina aperta tante volte (anteprime delle app di messaggi, un
// telefono che ricarica) non mette in coda le scritture del negozio; la scrittura condizionata decide comunque, quindi
// due letture insieme scrivono una data sola.
func (b *Buste) chiudiAllaLettura(ctx context.Context, lotto int64) error {
	var si int
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT 1 FROM lotti_avviso WHERE id = ?1 AND inviato_il IS NOT NULL AND chiuso_il IS NULL
		AND EXISTS (SELECT 1 FROM buste WHERE lotto_avviso_id = ?1)
		AND NOT EXISTS (SELECT 1 FROM buste WHERE lotto_avviso_id = ?1 AND stato != 'consegnata')`, lotto).Scan(&si)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, ora := b.adesso()
	if err := chiudiLottoCompleto(ctx, tx, lotto, ora); err != nil {
		return err
	}
	return tx.Commit()
}

// membriPubblici è public_members/1: le buste del lotto ancora dell'oreficeria del lotto, fuori dal cestino, dalla
// più recente.
func (b *Buste) membriPubblici(ctx context.Context, lotto, oreficeria int64) ([]BustaLottoPubblico, error) {
	rr, err := b.d.Lettura.QueryContext(ctx, `SELECT id, numero, coalesce(codice_qr, ''), descrizione, stato, data_pronta, data_consegna,
		prezzo_testo, prezzo_centesimi, preventivo_testo, preventivo_centesimi
		FROM buste WHERE lotto_avviso_id = ? AND oreficeria_id = ? AND cancellata_il IS NULL ORDER BY id DESC`, lotto, oreficeria)
	if err != nil {
		return nil, err
	}
	defer rr.Close()
	var out []BustaLottoPubblico
	for rr.Next() {
		var x BustaLottoPubblico
		var numero, qr string
		var pronta, ritiro, pt, vt sql.NullString
		var pc, vc sql.NullInt64
		if err := rr.Scan(&x.ID, &numero, &qr, &x.Descrizione, &x.Stato, &pronta, &ritiro, &pt, &pc, &vt, &vc); err != nil {
			return nil, err
		}
		x.Numero, _ = NumeroVisibile(qr, numero)
		x.DataPronta, x.DataRitiro = nullaTesto(pronta), nullaTesto(ritiro)
		x.Prezzo = Prezzo{Testo: nullaTesto(pt), Centesimi: nullaIntero(pc)}
		x.Preventivo = Prezzo{Testo: nullaTesto(vt), Centesimi: nullaIntero(vc)}
		out = append(out, x)
	}
	return out, rr.Err()
}

// LeggiFotoLotto è photo/2: la foto foto (l'id come arriva) della busta busta del lotto del gettone, nella variante
// v, se l'oreficeria la può vedere (la sua colonna, mai quella del cliente). nil senza righe d'audit se il gettone
// non apre un lotto o la busta non è del lotto e ancora della sua oreficeria (member/2: fuori dal cestino); nil con
// la riga repair.public_photo_viewed «denied» se la foto non c'è, è di un'altra busta o non è visibile; con «ok» i
// byte. Una riga che non si scrive è un errore: niente byte.
func (b *Buste) LeggiFotoLotto(ctx context.Context, gettone, busta, foto string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	lotto, _, _, _, ok, err := b.lottoPubblico(ctx, gettone)
	if err != nil || !ok {
		return nil, err
	}
	bid, ok := idPositivo(busta)
	if !ok {
		return nil, nil
	}
	var oref int64
	err = b.d.Lettura.QueryRowContext(ctx, `SELECT x.oreficeria_id FROM buste x JOIN lotti_avviso l ON l.id = x.lotto_avviso_id
		WHERE x.id = ? AND l.id = ? AND x.oreficeria_id = l.oreficeria_id AND x.cancellata_il IS NULL`, bid, lotto).Scan(&oref)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f Foto
	trovata := false
	if fid, ok := idPositivo(foto); ok {
		if f, trovata, err = fotoDi(ctx, b.d.Lettura, bid, fid); err != nil {
			return nil, err
		}
	}
	return b.fotoPubblicaLetta(ctx, bid, &oref, f, trovata, idRipulito(foto), PubblicoOreficeria, v, c)
}
