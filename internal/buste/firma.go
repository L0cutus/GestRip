package buste

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La firma di consegna (RIP-S-36…40), come DeliverySignature e Workshop.collection_changes/2 dello
// standalone a go-baseline (lib/gestrip_standalone/delivery_signature.ex, workshop.ex):
//   - una busta ha al più una firma; il salvataggio è la consegna: nella stessa transazione la busta
//     passa allo stato «ritirata» della sua famiglia (consegnata, o chiusa per il compro oro) con la
//     data di consegna se manca e l'orologio della conservazione delle foto (CatturaFirma);
//   - il blocco è la scrittura, mai una lettura prima: UPDATE … WHERE firma_chiave IS NULL. Due
//     conferme quasi contemporanee danno una firma e un rifiuto; la firma già presente vince sul
//     controllo dello stato;
//   - i byte non si toccano mai (è una prova): il file va nell'archivio media con il nome del suo
//     SHA-256, e ogni lettura lo ricontrolla (media.Archivio.Leggi). Il controllo all'ingresso è
//     quello dello standalone: i byte magici del PNG e al più 500 KiB, niente decodifica;
//   - l'azzeramento (solo il titolare, lo decide la rotta) toglie la firma dalla busta e lascia stato
//     e data di consegna; il file resta nell'archivio e il registro eventi_firma_consegna, solo in
//     aggiunta, ha una riga «reset» che lo nomina;
//   - la firma la vede solo il personale (visible?/2 con :staff); ogni lettura scrive
//     repair.signature_viewed, «ok» o «denied»;
//   - nessuna notifica (DEBITI.md dello standalone).
// La busta si cerca come Workshop.get_repair/1: un id che non è un intero positivo e una busta nel
// cestino (Repo.prepare_query/3) sono «non trovata».

// MaxByteFirma è il limite della firma (@max_bytes, app_firma_bp.py:20-21).
const MaxByteFirma = 500 * 1024

// maxRitirataDa sono i grafemi di «Ritirato da» (@max_collected_by).
const maxRitirataDa = 120

// prefissoFirma è il data URL che il riquadro produce e lo standalone accetta, alla lettera.
const prefissoFirma = "data:image/png;base64,"

// magicoPNG sono gli otto byte con cui comincia ogni PNG.
var magicoPNG = []byte("\x89PNG\r\n\x1a\n")

// I rifiuti della firma, con i testi del controller dello standalone.
var (
	// ErrFirmaNonValida: il campo non è il data URL di un PNG di al più 500 KiB (:invalid_signature).
	ErrFirmaNonValida Rifiuto = "Firma mancante o non valida."
	// ErrFirmaGiaPresente: la busta ha già una firma (:already_signed).
	ErrFirmaGiaPresente Rifiuto = "La firma è già stata registrata su questa busta."
	// ErrNonConsegnabile: il grafo non porta la busta al suo stato «ritirata» (:status_not_deliverable).
	ErrNonConsegnabile Rifiuto = "Questa busta non può essere consegnata da dove si trova: firma non disponibile."
	// ErrNessunaFirma: l'azzeramento di una busta senza firma (:not_signed).
	ErrNessunaFirma Rifiuto = "Su questa busta non c'è nessuna firma da azzerare."
)

// FirmaBusta è la firma che la busta porta (DeliverySignature.state/1 con {:signed, …}). Il è nil per
// una firma importata senza istante.
type FirmaBusta struct {
	Chiave     string
	Il         *time.Time
	RitirataDa *string
}

// Timbro è signature_stamp/1 della scheda: «Firmata il <gg/mm/aaaa hh:mm>» nel fuso del negozio, o
// «Firmata (data non registrata)», e « — ritirata da <nome>» se c'è.
func (f FirmaBusta) Timbro() string {
	s := "Firmata"
	if f.Il != nil {
		s += " il " + clock.Timbro(*f.Il)
	} else {
		s += " (data non registrata)"
	}
	if f.RitirataDa != nil {
		s += " — ritirata da " + *f.RitirataDa
	}
	return s
}

// statoRitirata è collection_status/1: lo stato in cui una busta è ritirata dal cliente.
func statoRitirata(compraOro bool) string {
	if compraOro {
		return "chiusa"
	}
	return "consegnata"
}

// Ritirabile è Workshop.collection_changes/2 senza i cambi: ok se la busta può arrivare allo stato
// «ritirata» della sua famiglia da dove si trova; cambia se ci arriva adesso (false: c'è già, il caso
// «Aggiungi firma»).
func Ritirabile(stato string, compraOro bool) (cambia, ok bool) {
	meta := statoRitirata(compraOro)
	switch {
	case stato == meta:
		return false, true
	case slices.Contains(Consentiti(stato, compraOro), meta):
		return true, true
	}
	return false, false
}

// DecodificaFirma è DeliverySignature.decode/1: il campo firma_png dev'essere il data URL del riquadro,
// con il prefisso alla lettera e il base64 come Base.decode64/1 (padding obbligatorio, niente spazi né
// a capo: il decodificatore di Go salterebbe \r e \n; i bit di riempimento non si guardano, come là),
// e i byte devono passare controllaFirma. Un campo assente è come uno vuoto: ErrFirmaNonValida.
func DecodificaFirma(valore string) ([]byte, error) {
	corpo, ok := strings.CutPrefix(valore, prefissoFirma)
	if !ok || strings.ContainsAny(corpo, "\r\n") {
		return nil, ErrFirmaNonValida
	}
	png, err := base64.StdEncoding.DecodeString(corpo)
	if err != nil || controllaFirma(png) != nil {
		return nil, ErrFirmaNonValida
	}
	return png, nil
}

// controllaFirma è validate/1: i byte magici del PNG e al più MaxByteFirma; niente che riscriva un byte.
func controllaFirma(png []byte) error {
	if len(png) > MaxByteFirma || !bytes.HasPrefix(png, magicoPNG) {
		return ErrFirmaNonValida
	}
	return nil
}

// ritirataDa è normalize_collected_by/1: spazi Unicode ai bordi tolti (String.trim/1), i primi 120
// grafemi; vuoto è nessuno (NULL).
func ritirataDa(s string) any {
	s = strings.TrimFunc(s, unicode.IsSpace)
	g := uniseg.NewGraphemes(s)
	fine := 0
	for i := 0; i < maxRitirataDa && g.Next(); i++ {
		_, fine = g.Positions()
	}
	if s = s[:fine]; s == "" {
		return nil
	}
	return s
}

// PaginaFirma è quello che la pagina di firma mostra (new.html.heex).
type PaginaFirma struct {
	ID                       int64
	Numero                   string
	NomeCliente, Descrizione string
}

// bustaDellaFirma legge la busta come la cerca il controller (fetch/1): fuori dal cestino.
type bustaDellaFirma struct {
	id                  int64
	numero, nome, descr string
	stato               string
	compraOro           bool
	chiave              sql.NullString
}

func (b *Buste) bustaDellaFirma(ctx context.Context, id string) (bustaDellaFirma, error) {
	n, ok := idPositivo(id)
	if !ok {
		return bustaDellaFirma{}, ErrBustaNonTrovata
	}
	var x bustaDellaFirma
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, numero, nome_cliente, descrizione, stato, compro_oro, firma_chiave
		FROM buste WHERE id = ? AND cancellata_il IS NULL`, n).Scan(&x.id, &x.numero, &x.nome, &x.descr, &x.stato, &x.compraOro, &x.chiave)
	if errors.Is(err, sql.ErrNoRows) {
		return bustaDellaFirma{}, ErrBustaNonTrovata
	}
	return x, err
}

// ApriFirma è DeliverySignatureController.new/2 senza la pagina: ErrBustaNonTrovata, ErrFirmaGiaPresente
// (la pagina torna alla scheda senza messaggio), ErrNonConsegnabile, o i dati della pagina. Nessuna
// scrittura.
func (b *Buste) ApriFirma(ctx context.Context, id string) (PaginaFirma, error) {
	x, err := b.bustaDellaFirma(ctx, id)
	if err != nil {
		return PaginaFirma{}, err
	}
	if x.chiave.Valid {
		return PaginaFirma{}, ErrFirmaGiaPresente
	}
	if _, ok := Ritirabile(x.stato, x.compraOro); !ok {
		return PaginaFirma{}, ErrNonConsegnabile
	}
	return PaginaFirma{ID: x.id, Numero: x.numero, NomeCliente: x.nome, Descrizione: x.descr}, nil
}

// CatturaFirma è DeliverySignatureController.create/2 con DeliverySignature.decode/1 e capture/4:
// la busta (ErrBustaNonTrovata), poi il campo (ErrFirmaNonValida, niente scritto), poi, con il
// lucchetto dell'archivio, il file salvato prima della transazione e nella transazione la busta firmata
// e consegnata con UPDATE … WHERE firma_chiave IS NULL: nessuna riga è ErrFirmaGiaPresente, che vince
// sul grafo; un grafo che non porta allo stato «ritirata» è ErrNonConsegnabile, e la transazione si
// annulla (il file salvato resta, come quello di una foto la cui riga non si scrive: lo raccoglie la
// pulizia). Poi la riga «captured» del registro, repair.signature_captured e, se la busta è stata
// consegnata adesso, la chiusura del suo lotto d'avviso.
func (b *Buste) CatturaFirma(ctx context.Context, id, campo, ritirata string, c audit.Contesto) error {
	x, err := b.bustaDellaFirma(ctx, id)
	if err != nil {
		return err
	}
	png, err := DecodificaFirma(campo)
	if err != nil {
		return err
	}
	if b.archivio == nil {
		return ErrSenzaArchivio
	}
	return b.archivio.ConLucchetto(func() error {
		chiave, err := b.archivio.SalvaConLucchetto(png)
		if err != nil {
			// store/1 dello standalone: un salvataggio che non riesce è :invalid_signature.
			return errors.Join(ErrFirmaNonValida, err)
		}
		return b.scriviFirma(ctx, x.id, chiave, int64(len(png)), ritirataDa(ritirata), c)
	})
}

func (b *Buste) scriviFirma(ctx context.Context, id int64, chiave string, n int64, ritirata any, c audit.Contesto) error {
	t, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var (
		stato string
		oro   bool
		conse sql.NullString
		lotto sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `SELECT stato, compro_oro, data_consegna, lotto_avviso_id FROM buste
		WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&stato, &oro, &conse, &lotto)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBustaNonTrovata
	}
	if err != nil {
		return err
	}
	cambia, ok := Ritirabile(stato, oro)
	q, args := `UPDATE buste SET firma_chiave = ?, firma_il = ?, firma_ritirata_da = ?, firma_byte = ?, aggiornato_il = ?`,
		[]any{chiave, ora, ritirata, n, ora}
	meta := statoRitirata(oro)
	if ok && cambia {
		// milestone_changes/4: lo stato, la data di consegna se manca (oggi in negozio, come CambiaStato),
		// e l'orologio della conservazione (consegnata e chiusa sono stati chiusi).
		q, args = q+`, stato = ?, conservazione_iniziata_il = ?`, append(args, meta, ora)
		if meta == "consegnata" && !conse.Valid {
			q, args = q+`, data_consegna = ?`, append(args, clock.DataDi(t).String())
		}
	}
	res, err := tx.ExecContext(ctx, q+` WHERE id = ? AND firma_chiave IS NULL AND cancellata_il IS NULL`, append(args, id)...)
	if err != nil {
		return err
	}
	scritte, err := res.RowsAffected()
	if err != nil {
		return err
	}
	switch {
	case scritte == 0:
		return ErrFirmaGiaPresente
	case !ok:
		return ErrNonConsegnabile
	}
	if err := b.registraFirma(ctx, tx, id, "captured", chiave, ritirata, ora, c); err != nil {
		return err
	}
	// Slice 212 dello standalone: una firma che consegna l'ultima busta di un lotto lo chiude; una
	// aggiunta a una busta già consegnata non consegna niente.
	if ok && cambia && meta == "consegnata" && lotto.Valid {
		if err := chiudiLottoCompleto(ctx, tx, lotto.Int64, ora); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// registraFirma è record/6: la riga del registro eventi_firma_consegna e la riga d'audit
// repair.signature_<evento>, che dice se qualcun altro ha ritirato, mai chi.
func (b *Buste) registraFirma(ctx context.Context, tx *sql.Tx, id int64, evento, chiave string, ritirata any, ora string, c audit.Contesto) error {
	var attore any
	if c.Attore != "" {
		attore = c.Attore
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO eventi_firma_consegna (busta_id, evento, chiave, ritirata_da, attore,
		avvenuto_il, creato_il, aggiornato_il) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, evento, chiave, ritirata, attore, ora, ora, ora); err != nil {
		return err
	}
	return b.reg.Scrivi(ctx, tx, c, "repair.signature_"+evento, "repair", id, "ok",
		map[string]any{"storage_key": chiave, "named_collector": ritirata != nil})
}

// AzzeraFirma è DeliverySignatureController.reset/2 con DeliverySignature.reset/2 (il ruolo lo
// controlla la rotta): ErrBustaNonTrovata, ErrNessunaFirma, o la firma tolta dalla busta con
// UPDATE … WHERE firma_chiave IS NOT NULL, stato e data di consegna come sono, e la riga «reset» che
// nomina il file (che resta nell'archivio) con repair.signature_reset.
func (b *Buste) AzzeraFirma(ctx context.Context, id string, c audit.Contesto) error {
	x, err := b.bustaDellaFirma(ctx, id)
	if err != nil {
		return err
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	var chiave, ritirata sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT firma_chiave, firma_ritirata_da FROM buste WHERE id = ? AND cancellata_il IS NULL`,
		x.id).Scan(&chiave, &ritirata)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrBustaNonTrovata
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE buste SET firma_chiave = NULL, firma_il = NULL, firma_ritirata_da = NULL,
		firma_byte = NULL, aggiornato_il = ? WHERE id = ? AND firma_chiave IS NOT NULL AND cancellata_il IS NULL`, ora, x.id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrNessunaFirma
	}
	var nome any
	if ritirata.Valid {
		nome = ritirata.String
	}
	if err := b.registraFirma(ctx, tx, x.id, "reset", chiave.String, nome, ora, c); err != nil {
		return err
	}
	return tx.Commit()
}

// LeggiFirma è DeliverySignatureController.show/2 con DeliverySignature.read/3 per il personale: i
// byte originali della firma, verificati con l'impronta (fuori dalla transazione), e la riga
// repair.signature_viewed «ok»; nil (e la riga «denied») per una busta senza firma o un file che
// manca o non torna. Una busta che non c'è dà nil senza riga: per chi chiede è lo stesso rifiuto.
// Un errore vuol dire che la riga non si è scritta: anche allora niente byte.
func (b *Buste) LeggiFirma(ctx context.Context, id string, c audit.Contesto) ([]byte, error) {
	x, err := b.bustaDellaFirma(ctx, id)
	if errors.Is(err, ErrBustaNonTrovata) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var png []byte
	if x.chiave.Valid && b.archivio != nil {
		if p, err := b.archivio.Leggi(x.chiave.String); err == nil {
			png = p
		}
	}
	esito := "ok"
	if png == nil {
		esito = "denied"
	}
	if err := b.reg.Registra(ctx, c, "repair.signature_viewed", "repair", x.id, esito, map[string]any{"audience": "staff"}); err != nil {
		return nil, err
	}
	return png, nil
}

// firmaDellaBusta è la firma per la scheda, dai campi letti; nil senza chiave.
func firmaDellaBusta(chiave, il, ritirata sql.NullString) (*FirmaBusta, error) {
	if !chiave.Valid {
		return nil, nil
	}
	f := &FirmaBusta{Chiave: chiave.String, RitirataDa: nullaTesto(ritirata)}
	if il.Valid {
		t, err := time.Parse(db.FormatoIstante, il.String)
		if err != nil {
			return nil, err
		}
		f.Il = &t
	}
	return f, nil
}
