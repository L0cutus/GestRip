package buste

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/firmephoenix"
)

// Il link e la pagina pubblica dell'artigiano (ART-23…26, RIP-S-25; slice 084), come ArtisanLink,
// PublicArtisanController e Outgoing.resolve_artisan_link/2 dello standalone (go-baseline):
//   - il link è /artigiano/<id>?t=<gettone>: senza stato, 90 giorni, non revocabile, uno nuovo a ogni
//     lettura (i vecchi valgono fino alla scadenza). Il Go firma i suoi gettoni con una sua sottochiave
//     (db.SottoChiave(chiave, "artigiano")): «g1.<firmato_ms>.<HMAC-SHA256 in base64url>» sull'id e
//     sull'istante, con un prefisso di dominio, così nessun altro gettone del Go (galleria, email) si
//     scambia con questo;
//   - D-87 (Stefano 14/11/2026): la pagina accetta anche i Phoenix.Token già mandati dallo standalone,
//     verificati con il suo secret_key_base (internal/firmephoenix, sale «artisan bag projection»);
//   - il gettone nomina la busta e la rotta pure: devono coincidere (un gettone di un'altra busta è un
//     rifiuto, non un rinvio a quella busta);
//   - la proiezione legge solo id, numero, QR (per il numero visibile della D-64), descrizione, stato e
//     oreficeria_id: niente del cliente esce dal database;
//   - ogni rifiuto (gettone assente, malformato, scaduto, di un'altra busta o di un altro tipo, busta
//     che non c'è o nel cestino) è lo stesso ErrBustaNonTrovata con la riga repair.public_artisan_denied
//     senza busta (id «unknown»); l'apertura scrive repair.public_artisan_viewed. Una riga che non si
//     scrive è il rifiuto. Il cestino è una scelta del Go, come per /r/ e /stato: lo standalone non lo
//     guarda;
//   - i vecchi link del Flask /artigiano/<busta>/<anno>?t=<exp>.<16hex> (ART-26) con il segreto Flask
//     della 064, sul numero scritto nell'indirizzo (la busta trovata anche senza gli zeri, D-77), con
//     repair.legacy_artisan_viewed e repair.legacy_artisan_denied; le foto per nome file;
//   - le foto: solo quelle visibili al pubblico «artisan», nominato dalla rotta (Visibile).

// SaleArtigiano è @salt di ArtisanLink, il sale dei Phoenix.Token dello standalone.
const SaleArtigiano = "artisan bag projection"

// DurataLinkArtigiano è @max_age di ArtisanLink: 90 giorni.
const DurataLinkArtigiano = 90 * 24 * time.Hour

// prefissoGettoneArtigiano è la versione del gettone del Go.
const prefissoGettoneArtigiano = "g1."

// dominioGettoneArtigiano separa l'HMAC di questo gettone da ogni altro firmato con chiavi vicine.
const dominioGettoneArtigiano = "gestrip link artigiano\x00"

// ConLinkArtigiano dà alle buste la sottochiave dei link dell'artigiano e il secret_key_base dello
// standalone (D-87). Una chiave vuota: niente gettoni del Go (né firmati né accettati); un segreto
// assente: nessun Phoenix.Token accettato.
func (b *Buste) ConLinkArtigiano(chiave []byte, standalone firmephoenix.Segreto) *Buste {
	b.chiaveArtigiano, b.segretoStandalone = chiave, standalone
	return b
}

func (b *Buste) macArtigiano(id, firmato int64) []byte {
	m := hmac.New(sha256.New, b.chiaveArtigiano)
	m.Write([]byte(dominioGettoneArtigiano + strconv.FormatInt(id, 10) + "\x00" + strconv.FormatInt(firmato, 10)))
	return m.Sum(nil)
}

// GettoneArtigiano è ArtisanLink.sign/2 nel Go: un gettone nuovo per la busta id, firmato adesso;
// "" senza la sottochiave.
func (b *Buste) GettoneArtigiano(id int64) string {
	if len(b.chiaveArtigiano) == 0 || id <= 0 {
		return ""
	}
	firmato := b.o.Adesso().UnixMilli()
	return prefissoGettoneArtigiano + strconv.FormatInt(firmato, 10) + "." +
		base64.RawURLEncoding.EncodeToString(b.macArtigiano(id, firmato))
}

// gettoneDelGoValido dice se g è un gettone del Go per la busta id, non firmato dopo adesso e non
// scaduto (fino a 90 giorni compresi). L'istante è solo cifre, senza zeri davanti; la firma è
// confrontata a tempo costante.
func (b *Buste) gettoneDelGoValido(id int64, g string, adesso time.Time) bool {
	if len(b.chiaveArtigiano) == 0 {
		return false
	}
	resto, ok := strings.CutPrefix(g, prefissoGettoneArtigiano)
	if !ok {
		return false
	}
	istante, firma64, ok := strings.Cut(resto, ".")
	if !ok || istante == "" || len(istante) > 19 {
		return false
	}
	for i := 0; i < len(istante); i++ {
		if istante[i] < '0' || istante[i] > '9' {
			return false
		}
	}
	firmato, err := strconv.ParseInt(istante, 10, 64)
	if err != nil || strconv.FormatInt(firmato, 10) != istante {
		return false
	}
	firma, err := base64.RawURLEncoding.Strict().DecodeString(firma64)
	if err != nil || !hmac.Equal(firma, b.macArtigiano(id, firmato)) {
		return false
	}
	ora := adesso.UnixMilli()
	return firmato <= ora && ora-firmato <= DurataLinkArtigiano.Milliseconds()
}

// gettoneArtigianoValido è verify/2 di ArtisanLink: il gettone del Go o (D-87) il Phoenix.Token dello
// standalone, per la busta id.
func (b *Buste) gettoneArtigianoValido(id int64, g string) bool {
	adesso := b.o.Adesso()
	if strings.HasPrefix(g, prefissoGettoneArtigiano) {
		return b.gettoneDelGoValido(id, g, adesso)
	}
	dato, ok := b.segretoStandalone.VerificaIntero(SaleArtigiano, g, DurataLinkArtigiano, adesso)
	return ok && dato == id
}

// LinkArtigiano è ArtisanLink.public_link/1: indirizzo_pubblico + /artigiano/<id>?t=<gettone>; ""
// senza indirizzo pubblico o senza la sottochiave. La sottochiave c'è sempre in gestrip serve
// (cmd/gestrip la deriva dalla chiave del database): senza, RisolviLinkArtigiano direbbe
// ErrSenzaIndirizzoPubblico per una causa diversa, cosa che succede solo nei test.
func (b *Buste) LinkArtigiano(id int64) string {
	g := b.GettoneArtigiano(id)
	if b.pubblico == "" || g == "" {
		return ""
	}
	return strings.TrimRight(b.pubblico, "/") + "/artigiano/" + strconv.FormatInt(id, 10) + "?t=" + g
}

// RisolviLinkArtigiano è Outgoing.resolve_artisan_link/2: {link} del testo wa_artigiano diventa il link
// dell'artigiano della busta id (uno nuovo, niente si scrive); un testo senza {link} torna com'è. Senza
// indirizzo pubblico il testo con {link} non parte (ErrSenzaIndirizzoPubblico, D-46 estesa).
func (b *Buste) RisolviLinkArtigiano(id int64, testo string) (string, error) {
	if !strings.Contains(testo, segnapostoLink) {
		return testo, nil
	}
	l := b.LinkArtigiano(id)
	if l == "" {
		return "", ErrSenzaIndirizzoPubblico
	}
	return strings.ReplaceAll(testo, segnapostoLink, l), nil
}

// PaginaArtigiano è quello che la pagina dell'artigiano mostra (show.html.heex): il numero visibile
// senza «N.» (D-64 e D-82, NumeroNeiTesti), lo stato con le parole della pagina del cliente, la lavorazione e le foto per l'artigiano.
// Niente del cliente.
type PaginaArtigiano struct {
	ID          int64
	Numero      string
	Stato       string
	Descrizione string
	Foto        []Foto
	// numeroInterno è il numero salvato, per gli indirizzi delle foto della pagina storica.
	numeroInterno string
}

// proiezioneArtigiano è la SELECT difensiva di ArtisanLink: la busta id fuori dal cestino.
func (b *Buste) proiezioneArtigiano(ctx context.Context, id int64) (PaginaArtigiano, *int64, bool, error) {
	var p PaginaArtigiano
	var qr sql.NullString
	var oref sql.NullInt64
	var stato string
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, numero, codice_qr, descrizione, stato, oreficeria_id
		FROM buste WHERE id = ? AND cancellata_il IS NULL`, id).Scan(&p.ID, &p.numeroInterno, &qr, &p.Descrizione, &stato, &oref)
	if errors.Is(err, sql.ErrNoRows) {
		return PaginaArtigiano{}, nil, false, nil
	}
	if err != nil {
		return PaginaArtigiano{}, nil, false, err
	}
	// D-82 estesa alla pagina dell'artigiano (Stefano, 15/11/2026, slice 085): il numero dei testi, senza «N.».
	p.Numero = NumeroNeiTesti(qr.String, p.numeroInterno)
	p.Stato = etichetteStatoCliente[stato]
	return p, nullaIntero(oref), true, nil
}

// conFotoArtigiano mette nella pagina le foto visibili all'artigiano.
func (b *Buste) conFotoArtigiano(ctx context.Context, p *PaginaArtigiano, oref *int64) error {
	var err error
	p.Foto, err = b.VisibiliPer(ctx, BustaDellaFoto{ID: p.ID, OreficeriaID: oref}, PubblicoArtigiano)
	return err
}

// apriArtigiano è projection/2: la busta della rotta con il gettone, o niente.
func (b *Buste) apriArtigiano(ctx context.Context, idTesto, gettone string) (PaginaArtigiano, *int64, bool, error) {
	id, ok := idPositivo(idTesto)
	if !ok || !b.gettoneArtigianoValido(id, gettone) {
		return PaginaArtigiano{}, nil, false, nil
	}
	return b.proiezioneArtigiano(ctx, id)
}

// PaginaArtigiano è show/2: la pagina di /artigiano/<id>?t=. ErrBustaNonTrovata per ogni rifiuto, con la
// riga repair.public_artisan_denied senza busta; riuscita, repair.public_artisan_viewed.
func (b *Buste) PaginaArtigiano(ctx context.Context, idTesto, gettone string, c audit.Contesto) (PaginaArtigiano, error) {
	p, oref, ok, err := b.apriArtigiano(ctx, idTesto, gettone)
	if err != nil {
		return PaginaArtigiano{}, err
	}
	return b.paginaArtigianoAperta(ctx, p, oref, ok, "repair.public_artisan_viewed", "repair.public_artisan_denied", c)
}

func (b *Buste) paginaArtigianoAperta(ctx context.Context, p PaginaArtigiano, oref *int64, ok bool, vista, rifiuto string, c audit.Contesto) (PaginaArtigiano, error) {
	if ok {
		// Una riga che non si scrive è il rifiuto (il ramo _error di show/2).
		if err := b.reg.Registra(ctx, c, vista, "repair", p.ID, "ok", nil); err == nil {
			if err := b.conFotoArtigiano(ctx, &p, oref); err != nil {
				return PaginaArtigiano{}, err
			}
			return p, nil
		}
	}
	_ = b.reg.Registra(ctx, c, rifiuto, "repair", "unknown", "ok", nil)
	return PaginaArtigiano{}, ErrBustaNonTrovata
}

// apriArtigianoStorica è legacy_projection/1 più verify_artisan/4: la busta del numero scritto (prima
// com'è, poi senza gli zeri davanti, fuori dal cestino: D-77, la busta «0046/26» importata «46/26») con
// la firma Flask sul numero scritto, quello che il Flask ha firmato. Solo su quello, come lo standalone
// (che cerca il numero esatto): a differenza di /stato, «/artigiano/046/26» con la firma di «46/26» è un
// rifiuto.
func (b *Buste) apriArtigianoStorica(ctx context.Context, scritto, gettone string) (PaginaArtigiano, *int64, bool, error) {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil || !ok {
		return PaginaArtigiano{}, nil, false, err
	}
	if !b.segretoFlask.VerificaArtigiano(scritto, gettone, b.o.Adesso().Unix()) {
		return PaginaArtigiano{}, nil, false, nil
	}
	return b.proiezioneArtigiano(ctx, s.id)
}

// PaginaArtigianoStorica è legacy/2: la pagina di /artigiano/<busta>/<anno>?t= (scritto è
// «<busta>/<anno>» come arriva). ErrBustaNonTrovata per ogni rifiuto, con repair.legacy_artisan_denied
// senza busta; riuscita, repair.legacy_artisan_viewed.
func (b *Buste) PaginaArtigianoStorica(ctx context.Context, scritto, gettone string, c audit.Contesto) (PaginaArtigiano, error) {
	p, oref, ok, err := b.apriArtigianoStorica(ctx, scritto, gettone)
	if err != nil {
		return PaginaArtigiano{}, err
	}
	return b.paginaArtigianoAperta(ctx, p, oref, ok, "repair.legacy_artisan_viewed", "repair.legacy_artisan_denied", c)
}

// LeggiFotoArtigiano è photo/2: la foto foto (l'id come arriva) della busta della rotta, con il
// gettone della pagina, nella variante v. nil senza righe se il gettone non apre la busta; nil con la
// riga denied se la foto non c'è, è di un'altra busta o non è visibile all'artigiano.
func (b *Buste) LeggiFotoArtigiano(ctx context.Context, idTesto, gettone, foto string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	p, oref, ok, err := b.apriArtigiano(ctx, idTesto, gettone)
	if err != nil || !ok {
		return nil, err
	}
	var f Foto
	trovata := false
	if fid, ok := idPositivo(foto); ok {
		if f, trovata, err = fotoDi(ctx, b.d.Lettura, p.ID, fid); err != nil {
			return nil, err
		}
	}
	return b.fotoPubblicaLetta(ctx, p.ID, oref, f, trovata, idRipulito(foto), PubblicoArtigiano, v, c)
}

// LeggiFotoArtigianoStorica è legacy_photo/2: la foto della busta del vecchio link per nome file
// (read_public_filename/5, photo_id «filename»).
func (b *Buste) LeggiFotoArtigianoStorica(ctx context.Context, scritto, gettone, file string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	p, oref, ok, err := b.apriArtigianoStorica(ctx, scritto, gettone)
	if err != nil || !ok {
		return nil, err
	}
	f, trovata, err := fotoPerNome(ctx, b.d.Lettura, p.ID, file)
	if err != nil {
		return nil, err
	}
	return b.fotoPubblicaLetta(ctx, p.ID, oref, f, trovata, "filename", PubblicoArtigiano, v, c)
}

// fotoPubblicaLetta dà i byte della foto se è visibile al pubblico p (l'artigiano, o l'oreficeria del lotto dalla
// 092), poi scrive la riga repair.public_photo_viewed (audit_public_read/6); una riga che non si scrive è un errore:
// niente byte.
func (b *Buste) fotoPubblicaLetta(ctx context.Context, busta int64, oref *int64, f Foto, trovata bool, fotoID any, p Pubblico, v Variante, c audit.Contesto) (*MediaFoto, error) {
	var m *MediaFoto
	if trovata && Visibile(BustaDellaFoto{ID: busta, OreficeriaID: oref}, f, p) {
		m = b.mediaDi(ctx, f, v)
	}
	esito := "denied"
	if m != nil {
		esito = "ok"
	}
	dati := map[string]any{"photo_id": fotoID, "audience": string(p)}
	if v == VarianteMiniatura {
		dati["variant"] = string(VarianteMiniatura)
		if m != nil {
			dati["variant"] = string(m.Variante)
		}
	}
	if err := b.reg.Registra(ctx, c, "repair.public_photo_viewed", "repair", busta, esito, dati); err != nil {
		return nil, err
	}
	return m, nil
}
