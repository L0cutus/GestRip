package buste

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/imaging"
	"github.com/L0cutus/gestrip/internal/media"
)

// Le foto della busta (T4.3), senza pagine: allegare (RIP-S-29, RIP-S-30), visibilità per canale
// (RIP-S-31), eliminazione (RIP-S-32). Copia RepairPhotos dello standalone a go-baseline
// (lib/gestrip_standalone/repair_photos.ex).

// Pubblico è chi guarda una foto: lo nomina la rotta, che sa chi c'è dall'altra parte.
type Pubblico string

// I pubblici dello standalone (field_for/2).
const (
	PubblicoPersonale  Pubblico = "staff"
	PubblicoCliente    Pubblico = "customer"
	PubblicoArtigiano  Pubblico = "artisan"
	PubblicoOreficeria Pubblico = "orefice"
)

// Classe è la classificazione della busta per le foto (visibility_classification/1).
type Classe int

// Le due classi: diretta (cliente; anche l'oreficeria 1, il negozio) ed esterna (un'oreficeria).
const (
	ClasseDiretta Classe = iota + 1
	ClasseEsterna
)

// Visibilita sono i tre segni di una foto.
type Visibilita struct {
	Cliente, Artigiano, Oreficeria bool
}

// Foto è una riga di foto_buste.
type Foto struct {
	ID, BustaID int64
	Posizione   int64
	Chiave      string
	NomeFile    string
	TipoMime    string
	Byte        int64
	Sha256      []byte
	FotoBusta   bool
	Visibilita
}

// BustaDellaFoto è quello che la decisione sulla visibilità guarda della busta.
type BustaDellaFoto struct {
	ID           int64
	OreficeriaID *int64
}

// Caricamento è una foto arrivata: il file temporaneo del caricamento e il nome che le ha dato chi
// l'ha scelta.
type Caricamento struct {
	Percorso, NomeOriginale string
}

// Sessione è l'account di chi carica, da ricontrollare dopo la normalizzazione (session_user).
type Sessione struct {
	ID, Versione int64
}

// Normalizzatore fa la foto da salvare (imaging.Lavoratore, o la funzione nel processo nei test).
type Normalizzatore interface {
	Normalizza(ctx context.Context, in []byte) ([]byte, error)
}

// Gli errori delle foto, con i nomi dello standalone.
var (
	ErrTroppeFoto        = errors.New("buste: troppe foto")            // :too_many_photos
	ErrCaricamento       = errors.New("buste: caricamento non valido") // :invalid_upload
	ErrFotoNonTrovata    = errors.New("buste: foto non trovata")       // :not_found
	ErrPubblicoNonValido = errors.New("buste: pubblico non valido")    // :unsupported_audience
	ErrSessioneRevocata  = errors.New("buste: sessione revocata")      // :session_revoked
	ErrSenzaArchivio     = errors.New("buste: archivio delle foto non configurato")
)

// ConFoto dà alle buste l'archivio dei media e il normalizzatore delle foto.
func (b *Buste) ConFoto(a *media.Archivio, n Normalizzatore) *Buste {
	b.archivio, b.normalizzatore = a, n
	return b
}

// Classificazione è visibility_classification/1: esterna solo con un'oreficeria oltre la 1
// (Oreficeria.is_b2b/1).
func Classificazione(oreficeriaID *int64) Classe {
	if oreficeriaID != nil && *oreficeriaID > 1 {
		return ClasseEsterna
	}
	return ClasseDiretta
}

// VisibilitaIniziale è default_visibility/2: busta diretta, cliente sì e artigiano sì; esterna,
// oreficeria sì e artigiano sì; la foto della busta mai all'artigiano; il cliente di una busta
// esterna non vede mai foto.
func VisibilitaIniziale(c Classe, fotoBusta bool) Visibilita {
	v := Visibilita{Artigiano: !fotoBusta}
	if c == ClasseEsterna {
		v.Oreficeria = true
	} else {
		v.Cliente = true
	}
	return v
}

// pubblicoDiClasse è @audience_fields: i pubblici che ogni classe ha, con la loro colonna.
func pubblicoDiClasse(c Classe, p Pubblico) (string, bool) {
	switch {
	case p == PubblicoArtigiano:
		return "visibile_artigiano", true
	case c == ClasseDiretta && p == PubblicoCliente:
		return "visibile_cliente", true
	case c == ClasseEsterna && p == PubblicoOreficeria:
		return "visibile_oreficeria", true
	}
	return "", false
}

// Visibile è visible?/3, l'unico punto che decide per tutte le rotte: il personale vede ogni foto
// della busta; un pubblico che la classe della busta non ha, o che nessuno ha dichiarato, non vede
// niente (mai un'altra colonna al suo posto); una foto di un'altra busta non si vede mai.
func Visibile(b BustaDellaFoto, f Foto, p Pubblico) bool {
	if f.BustaID != b.ID {
		return false
	}
	if p == PubblicoPersonale {
		return true
	}
	colonna, ok := pubblicoDiClasse(Classificazione(b.OreficeriaID), p)
	if !ok {
		return false
	}
	switch colonna {
	case "visibile_cliente":
		return f.Cliente
	case "visibile_oreficeria":
		return f.Oreficeria
	}
	return f.Artigiano
}

const colonneFoto = `id, busta_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, foto_busta,
	visibile_cliente, visibile_artigiano, visibile_oreficeria`

type scansione interface{ Scan(...any) error }

func leggiFoto(r scansione) (Foto, error) {
	var f Foto
	err := r.Scan(&f.ID, &f.BustaID, &f.Posizione, &f.Chiave, &f.NomeFile, &f.TipoMime, &f.Byte, &f.Sha256,
		&f.FotoBusta, &f.Cliente, &f.Artigiano, &f.Oreficeria)
	return f, err
}

type interroga interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// ElencoFoto è list/1: le foto della busta nell'ordine di caricamento (posizione, poi id).
func (b *Buste) ElencoFoto(ctx context.Context, bustaID int64) ([]Foto, error) {
	return elencoFoto(ctx, b.d.Lettura, bustaID)
}

func elencoFoto(ctx context.Context, q interroga, bustaID int64) ([]Foto, error) {
	righe, err := q.QueryContext(ctx, `SELECT `+colonneFoto+` FROM foto_buste WHERE busta_id = ? ORDER BY posizione, id`, bustaID)
	if err != nil {
		return nil, err
	}
	defer righe.Close()
	var v []Foto
	for righe.Next() {
		f, err := leggiFoto(righe)
		if err != nil {
			return nil, err
		}
		v = append(v, f)
	}
	return v, righe.Err()
}

// FotoDi è get/2: la foto fotoID solo se è della busta bustaID.
func (b *Buste) FotoDi(ctx context.Context, bustaID, fotoID int64) (Foto, bool, error) {
	return fotoDi(ctx, b.d.Lettura, bustaID, fotoID)
}

func fotoDi(ctx context.Context, q interroga, bustaID, fotoID int64) (Foto, bool, error) {
	f, err := leggiFoto(q.QueryRowContext(ctx, `SELECT `+colonneFoto+` FROM foto_buste WHERE busta_id = ? AND id = ?`, bustaID, fotoID))
	if errors.Is(err, sql.ErrNoRows) {
		return Foto{}, false, nil
	}
	return f, err == nil, err
}

// VisibiliPer è visible_for/2: le foto della busta come le vede quel pubblico, nell'ordine. La
// lista che una pagina disegna e i byte che una rotta serve vengono dalla stessa risposta.
func (b *Buste) VisibiliPer(ctx context.Context, busta BustaDellaFoto, p Pubblico) ([]Foto, error) {
	tutte, err := b.ElencoFoto(ctx, busta.ID)
	if err != nil {
		return nil, err
	}
	var v []Foto
	for _, f := range tutte {
		if Visibile(busta, f, p) {
			v = append(v, f)
		}
	}
	return v, nil
}

// estensioniFoto sono @allowed_extensions.
var estensioniFoto = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}

// MaxFotoPerVolta è quante foto si caricano insieme.
const MaxFotoPerVolta = 2

// fotoPronta è prepare_uploads/1 per una foto: normalizzata e pubblicata nell'archivio.
type fotoPronta struct {
	chiave, nome, tipo string
	byte               int64
	impronta           []byte
}

// AllegaFoto è attach_many/3, nell'ordine dello standalone:
//   - il gruppo (validate_batch/1): più di MaxFotoPerVolta è ErrTroppeFoto, nessuna o una con
//     un'estensione fuori da .jpg .jpeg .png .webp (senza maiuscole), che non è un file regolare o
//     che pesa 0 o più di imaging.MaxByteIngresso è ErrCaricamento, prima di leggere un byte;
//   - con il lucchetto dell'archivio, ogni foto si normalizza e si pubblica, fuori dalla
//     transazione (l'errore della prima che fallisce: imaging.ErrImmagine o media.ErrSalvataggio);
//   - nella transazione: la sessione (se c'è) deve essere ancora attiva con la stessa versione
//     (ErrSessioneRevocata), la busta deve esserci fuori dal cestino (ErrBustaNonTrovata; come
//     Repo.get con il filtro del cestino di prepare_query/3), poi le righe dopo l'ultima
//     posizione, la prima di una busta senza foto è la foto della busta, con le visibilità
//     iniziali della sua classe, e per ognuna repair.photo_uploaded con l'id e la posizione;
//   - dopo il Commit le chiavi delle foto nuove vanno al riscaldatore delle miniature, se c'è.
//
// Con un rifiuto dopo la pubblicazione nessuna riga si scrive e i file restano nell'archivio,
// orfani (li raccoglie la pulizia, RIP-S-57).
func (b *Buste) AllegaFoto(ctx context.Context, bustaID int64, cs []Caricamento, s *Sessione, c audit.Contesto) ([]Foto, error) {
	if b.archivio == nil || b.normalizzatore == nil {
		return nil, ErrSenzaArchivio
	}
	if len(cs) > MaxFotoPerVolta {
		return nil, ErrTroppeFoto
	}
	if len(cs) == 0 {
		return nil, ErrCaricamento
	}
	for _, x := range cs {
		if !caricamentoValido(x) {
			return nil, ErrCaricamento
		}
	}
	var foto []Foto
	err := b.archivio.ConLucchetto(func() error {
		pronte, err := b.preparaFoto(ctx, cs)
		if err != nil {
			return err
		}
		foto, err = b.scriviFoto(ctx, bustaID, pronte, s, c)
		return err
	})
	if err != nil {
		return nil, err
	}
	// La miniatura è pronta prima che la prima pagina la chieda (warm_previews/1).
	if b.riscaldatore != nil {
		chiavi := make([]string, len(foto))
		for i, f := range foto {
			chiavi[i] = f.Chiave
		}
		b.riscaldatore.Scalda(chiavi)
	}
	return foto, nil
}

// caricamentoValido è valid_upload?/1.
func caricamentoValido(x Caricamento) bool {
	if !estensioniFoto[strings.ToLower(estensioneElixir(x.NomeOriginale))] {
		return false
	}
	fi, err := os.Lstat(x.Percorso)
	return err == nil && fi.Mode().IsRegular() && fi.Size() >= 1 && fi.Size() <= imaging.MaxByteIngresso
}

func (b *Buste) preparaFoto(ctx context.Context, cs []Caricamento) ([]fotoPronta, error) {
	var v []fotoPronta
	for _, x := range cs {
		in, err := leggiCaricamento(x.Percorso)
		if err != nil {
			return nil, imaging.ErrImmagine
		}
		out, err := b.normalizzatore.Normalizza(ctx, in)
		if err != nil {
			return nil, err
		}
		k, err := b.archivio.SalvaConLucchetto(out)
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256(out)
		v = append(v, fotoPronta{chiave: k, nome: NomeSalvato(x.NomeOriginale, out), tipo: media.TipoImmagine(out),
			byte: int64(len(out)), impronta: h[:]})
	}
	return v, nil
}

// leggiCaricamento legge il file del caricamento, al più imaging.MaxByteIngresso + 1 byte (lo
// script dello standalone rifiuta un file cresciuto oltre il limite dopo il controllo).
func leggiCaricamento(p string) ([]byte, error) {
	f, err := media.ApriInLettura(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // in sola lettura: niente da perdere
	return io.ReadAll(io.LimitReader(f, imaging.MaxByteIngresso+1))
}

func (b *Buste) scriviFoto(ctx context.Context, bustaID int64, pronte []fotoPronta, s *Sessione, c audit.Contesto) ([]Foto, error) {
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if s != nil {
		var uno int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM utenti WHERE id = ? AND attivo = 1 AND versione_sessione = ?`, s.ID, s.Versione).Scan(&uno)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSessioneRevocata
		}
		if err != nil {
			return nil, err
		}
	}
	var oreficeria sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT oreficeria_id FROM buste WHERE id = ? AND cancellata_il IS NULL`, bustaID).Scan(&oreficeria)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBustaNonTrovata
	}
	if err != nil {
		return nil, err
	}
	var quante, ultima int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(max(posizione), 0) FROM foto_buste WHERE busta_id = ?`, bustaID).Scan(&quante, &ultima); err != nil {
		return nil, err
	}
	classe := Classificazione(nullaIntero(oreficeria))
	var v []Foto
	for i, p := range pronte {
		f := Foto{BustaID: bustaID, Posizione: ultima + 1 + int64(i), Chiave: p.chiave, NomeFile: p.nome, TipoMime: p.tipo,
			Byte: p.byte, Sha256: p.impronta, FotoBusta: quante == 0 && i == 0}
		f.Visibilita = VisibilitaIniziale(classe, f.FotoBusta)
		r, err := tx.ExecContext(ctx, `INSERT INTO foto_buste (busta_id, posizione, chiave, nome_file, tipo_mime, byte, sha256,
			foto_busta, visibile_cliente, visibile_artigiano, visibile_oreficeria, creato_il, aggiornato_il)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.BustaID, f.Posizione, f.Chiave, f.NomeFile, f.TipoMime, f.Byte,
			f.Sha256, f.FotoBusta, f.Cliente, f.Artigiano, f.Oreficeria, ora, ora)
		if err != nil {
			return nil, err
		}
		if f.ID, err = r.LastInsertId(); err != nil {
			return nil, err
		}
		if err := b.reg.Scrivi(ctx, tx, c, "repair.photo_uploaded", "repair", bustaID, "ok",
			map[string]any{"photo_id": f.ID, "position": f.Posizione}); err != nil {
			return nil, err
		}
		v = append(v, f)
	}
	return v, tx.Commit()
}

// ImpostaVisibilita è set_visibility/5, in una transazione: un id di foto non positivo è
// ErrPubblicoNonValido (Id.positive); la busta deve esserci fuori dal cestino (ErrBustaNonTrovata,
// come Repo.get) e la foto essere sua (ErrFotoNonTrovata); poi il pubblico deve esistere per la
// classe della busta adesso (ErrPubblicoNonValido: «cliente» su una esterna forzato dal browser). La riga cambia solo se il valore è diverso (Ecto non scrive un cambio vuoto);
// repair.photo_visibility_changed con id, pubblico e valore si scrive sempre.
func (b *Buste) ImpostaVisibilita(ctx context.Context, bustaID, fotoID int64, pubblico string, visibile bool, c audit.Contesto) (Foto, error) {
	if fotoID <= 0 {
		return Foto{}, ErrPubblicoNonValido
	}
	_, ora := b.adesso()
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return Foto{}, err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	oreficeria, err := oreficeriaDellaBusta(ctx, tx, bustaID)
	if err != nil {
		return Foto{}, err
	}
	f, ok, err := fotoDi(ctx, tx, bustaID, fotoID)
	if err != nil {
		return Foto{}, err
	}
	if !ok {
		return Foto{}, ErrFotoNonTrovata
	}
	colonna, ok := pubblicoDiClasse(Classificazione(oreficeria), Pubblico(pubblico))
	if !ok {
		return Foto{}, ErrPubblicoNonValido
	}
	campo := map[string]*bool{"visibile_cliente": &f.Cliente, "visibile_artigiano": &f.Artigiano, "visibile_oreficeria": &f.Oreficeria}[colonna]
	if *campo != visibile {
		*campo = visibile
		if _, err := tx.ExecContext(ctx, `UPDATE foto_buste SET `+colonna+` = ?, aggiornato_il = ? WHERE id = ?`, visibile, ora, f.ID); err != nil {
			return Foto{}, err
		}
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.photo_visibility_changed", "repair", bustaID, "ok",
		map[string]any{"photo_id": f.ID, "audience": pubblico, "visible": visibile}); err != nil {
		return Foto{}, err
	}
	return f, tx.Commit()
}

func oreficeriaDellaBusta(ctx context.Context, tx *sql.Tx, bustaID int64) (*int64, error) {
	var o sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT oreficeria_id FROM buste WHERE id = ? AND cancellata_il IS NULL`, bustaID).Scan(&o)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBustaNonTrovata
	}
	return nullaIntero(o), err
}

// RimuoviFoto è remove/3, in una transazione: un id non positivo è ErrFotoNonTrovata; la busta
// deve esserci fuori dal cestino (ErrBustaNonTrovata, come Repo.get) e la foto essere sua (ErrFotoNonTrovata); si toglie solo la
// riga, con repair.photo_removed (id e posizione). Il file resta: può essere di altre righe, e lo
// raccoglie la pulizia degli orfani dopo un backup verificato (RIP-S-55).
func (b *Buste) RimuoviFoto(ctx context.Context, bustaID, fotoID int64, c audit.Contesto) error {
	if fotoID <= 0 {
		return ErrFotoNonTrovata
	}
	tx, err := b.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	if _, err := oreficeriaDellaBusta(ctx, tx, bustaID); err != nil {
		return err
	}
	f, ok, err := fotoDi(ctx, tx, bustaID, fotoID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrFotoNonTrovata
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM foto_buste WHERE id = ?`, f.ID); err != nil {
		return err
	}
	if err := b.reg.Scrivi(ctx, tx, c, "repair.photo_removed", "repair", bustaID, "ok",
		map[string]any{"photo_id": f.ID, "position": f.Posizione}); err != nil {
		return err
	}
	return tx.Commit()
}

// NomeSalvato è stored_filename/2: il nome che ha dato chi ha scelto la foto, con l'estensione di
// quello che si salva (IMG_0001.png scelta dalla galleria si tiene come IMG_0001.jpg, perché ora è
// un JPEG): Path.basename, Path.rootname, al più 255 grafemi in tutto.
func NomeSalvato(nome string, b []byte) string {
	est := media.Estensione(b)
	radice := radiceElixir(baseElixir(nome))
	resto := 255 - uniseg.GraphemeClusterCount(est)
	var out strings.Builder
	g := uniseg.NewGraphemes(radice)
	for n := 0; n < resto && g.Next(); n++ {
		out.WriteString(g.Str())
	}
	return out.String() + est
}

// baseElixir è Path.basename/1: l'ultimo pezzo dopo «/», tolte le barre in fondo (misurato
// sull'immagine dello standalone: «dir/» → «dir»).
func baseElixir(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

// estensioneElixir è Path.extname/1 (misurato): dall'ultimo punto dell'ultimo pezzo (senza
// togliere le barre in fondo: «a.jpg/» non ha estensione); un nome con un solo punto all'inizio
// («.jpg») non ne ha.
func estensioneElixir(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]
	i := strings.LastIndex(base, ".")
	if i <= 0 && strings.Count(base, ".") <= 1 {
		return ""
	}
	return base[i:]
}

// radiceElixir è Path.rootname/1 su un nome già senza cartelle (misurato): via dall'ultimo punto in
// poi, anche per «.jpg» (che diventa vuoto).
func radiceElixir(base string) string {
	if i := strings.LastIndex(base, "."); i >= 0 {
		return base[:i]
	}
	return base
}
