package buste

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/firmeflask"
)

// I link storici della vecchia app Flask (CLI-16…20), come legacy/2, legacy_poll/2,
// legacy_message/2, legacy_photo/2 e legacy_thumb/2 di PublicRepairController,
// Workshop.get_legacy_public_repair/1 e get_legacy_public_repair_by_slug/1 e LegacyPublicLinks dello
// standalone (go-baseline):
//   - /stato/<busta>/<anno> si apre con il codice breve ?pw= (impronta importata in
//     hash_codice_flask) o con la firma ?t=&exp= del segreto Flask; il link non si emette mai;
//   - la busta si trova con il numero scritto nell'indirizzo, prima così com'è e poi senza gli zeri
//     davanti (NormalizzaNumero, slice 220); il cestino non conta. Prima com'è: dopo la D-77 una busta
//     che all'import è rimasta con gli zeri («0046/26», perché «46/26» era già di un'altra) deve
//     rispondere al suo link, non a quello dell'altra busta;
//   - firma e codice si verificano sul numero scritto nell'indirizzo (quello che Flask ha firmato:
//     dopo la D-77 la busta importata «0046/26» è salvata «46/26») e sul numero salvato (quello su cui
//     verifica lo standalone, così «/stato/046/26» di una busta «46/26» si apre come lì);
//   - la revoca (codice_flask_revocato_il) chiude tutte le rotte /stato prima di guardare la
//     credenziale; solo chi ha la credenziale giusta sa che il link è finito (ErrLinkStoricoRevocato,
//     la pagina 410), ogni altro rifiuto è il 404 della busta che non c'è (CLI-18);
//   - ogni rifiuto delle rotte /stato scrive repair.legacy_public_denied senza busta (id «unknown»),
//     la revoca con la busta e reason: revoked; l'apertura riuscita della pagina scrive
//     repair.legacy_public_viewed; il poll riuscito non scrive niente.

// ErrLinkStoricoRevocato è il link storico giusto di una busta revocata: la pagina 410, che nomina
// il negozio (PaginaStorica dà allora solo NomeNegozio).
var ErrLinkStoricoRevocato = errors.New("buste: link storico revocato")

// durataFotoStoriche è la validità delle firme che la pagina /stato mette sulle sue foto
// (legacy_photo_src/1: adesso più 3600 secondi).
const durataFotoStoriche = 3600

// ConSegretoFlask dà alle buste il segreto Flask (firmeflask.Leggi); senza, ogni firma dei link
// storici è rifiutata e le foto della pagina /stato hanno la firma vuota, come lo standalone senza
// LEGACY_WARRANTY_SECRET_FILE. Il codice breve ?pw= non ne ha bisogno.
func (b *Buste) ConSegretoFlask(s firmeflask.Segreto) *Buste {
	b.segretoFlask = s
	return b
}

// CredenzialeStorica è la credenziale di un link storico come arriva (param/2): ogni campo con il
// suo «c'è», falso se il parametro manca o non è un testo.
type CredenzialeStorica struct {
	Codice, Firma, Scadenza          string
	ConCodice, ConFirma, ConScadenza bool
}

// bustaStorica è quello che le rotte storiche guardano della busta trovata.
type bustaStorica struct {
	id           int64
	numero       string // il numero salvato
	impronta     []byte
	revocata     bool
	oreficeriaID *int64
	stato        string
	visibiliDal  *string
}

// cercaStorica trova la busta del numero scritto nell'indirizzo: prima il numero com'è, poi senza
// gli zeri davanti; una busta nel cestino è una busta che non c'è (e non lascia cercare l'altra
// forma: il numero è di quella busta).
func (b *Buste) cercaStorica(ctx context.Context, scritto string) (bustaStorica, bool, error) {
	s, trovata, err := b.bustaStoricaDove(ctx, `numero = ?`, scritto)
	if err != nil || trovata != nessuna {
		return s, trovata == viva, err
	}
	if n := NormalizzaNumero(scritto); n != scritto {
		s, trovata, err = b.bustaStoricaDove(ctx, `numero = ?`, n)
		return s, trovata == viva, err
	}
	return bustaStorica{}, false, nil
}

// cercaPerSlug è get_legacy_public_repair_by_slug/1: lo slug è il numero con «_» al posto di «/»,
// com'è scritto («0046_26»); come cercaStorica, prima com'è e poi senza gli zeri davanti.
//
// Lo slug com'è si cerca prima con l'indice del numero (il primo «_» torna «/») e solo se non trova
// niente, né viva né nel cestino, con replace(numero) come lo standalone (che scorre la tabella).
func (b *Buste) cercaPerSlug(ctx context.Context, slug string) (bustaStorica, bool, error) {
	scritto := strings.Replace(slug, "_", "/", 1)
	s, trovata, err := b.bustaStoricaDove(ctx, `numero = ?`, scritto)
	if err == nil && trovata == nessuna {
		s, trovata, err = b.bustaStoricaDove(ctx, `replace(numero, '/', '_') = ?`, slug)
	}
	if err != nil || trovata != nessuna {
		return s, trovata == viva, err
	}
	if n := NormalizzaNumero(scritto); n != scritto {
		s, trovata, err = b.bustaStoricaDove(ctx, `numero = ?`, n)
		return s, trovata == viva, err
	}
	return bustaStorica{}, false, nil
}

type trovata int

const (
	nessuna trovata = iota
	viva
	nelCestino
)

func (b *Buste) bustaStoricaDove(ctx context.Context, dove string, arg string) (bustaStorica, trovata, error) {
	var s bustaStorica
	var oref sql.NullInt64
	var revocata, cestino, dal sql.NullString
	err := b.d.Lettura.QueryRowContext(ctx, `SELECT id, numero, hash_codice_flask, codice_flask_revocato_il, oreficeria_id,
		stato, messaggi_visibili_dal, cancellata_il FROM buste WHERE `+dove+` ORDER BY id LIMIT 1`, arg).Scan(
		&s.id, &s.numero, &s.impronta, &revocata, &oref, &s.stato, &dal, &cestino)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return bustaStorica{}, nessuna, nil
	case err != nil:
		return bustaStorica{}, nessuna, err
	case cestino.Valid:
		return bustaStorica{}, nelCestino, nil
	}
	s.revocata, s.oreficeriaID, s.visibiliDal = revocata.Valid, nullaIntero(oref), nullaTesto(dal)
	return s, viva, nil
}

// credenzialeValida è legacy_credential_valid?/2: il codice o la firma, sul numero scritto o su
// quello salvato.
func (b *Buste) credenzialeValida(s bustaStorica, scritto string, c CredenzialeStorica) bool {
	adesso := b.o.Adesso().Unix()
	numeri := []string{scritto}
	if s.numero != scritto {
		numeri = append(numeri, s.numero)
	}
	for _, n := range numeri {
		if c.ConCodice && firmeflask.VerificaCodice(n, c.Codice, s.impronta) {
			return true
		}
		if b.segretoFlask.VerificaStato(n, c.Firma, c.Scadenza, adesso) {
			return true
		}
	}
	return false
}

// autorizzata è legacy_repair_authorized?/2: la busta non è revocata e la credenziale è giusta.
func (b *Buste) autorizzata(s bustaStorica, scritto string, c CredenzialeStorica) bool {
	return !s.revocata && b.credenzialeValida(s, scritto, c)
}

// b2b è Recipient.party/1 == :oreficeria.
func (s bustaStorica) b2b() bool { return Classificazione(s.oreficeriaID) == ClasseEsterna }

// rifiutoStorico scrive repair.legacy_public_denied senza busta (record_public_repair_read(nil, …));
// come lo standalone, una riga che non si scrive non cambia la risposta.
func (b *Buste) rifiutoStorico(ctx context.Context, c audit.Contesto) {
	_ = b.reg.Registra(ctx, c, "repair.legacy_public_denied", "repair", "unknown", "ok", nil)
}

// PaginaStorica è legacy/2: la pagina di /stato/<busta>/<anno> (scritto è «<busta>/<anno>» come
// arriva dall'indirizzo) con la credenziale c. ErrBustaNonTrovata per ogni rifiuto (con la riga
// repair.legacy_public_denied senza busta), ErrLinkStoricoRevocato per la credenziale giusta di una
// busta revocata (con la riga che nomina la busta e reason: revoked); riuscita, una riga
// repair.legacy_public_viewed e la pagina ridotta (CLI-17) con le foto firmate per un'ora.
func (b *Buste) PaginaStorica(ctx context.Context, scritto string, cred CredenzialeStorica, c audit.Contesto) (PaginaCliente, error) {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return PaginaCliente{}, err
	}
	if ok && s.revocata && b.credenzialeValida(s, scritto, cred) {
		_ = b.reg.Registra(ctx, c, "repair.legacy_public_denied", "repair", s.id, "ok", map[string]any{"reason": "revoked"})
		negozio, err := b.nomeNegozio(ctx)
		if err != nil {
			return PaginaCliente{}, err
		}
		return PaginaCliente{NomeNegozio: negozio}, ErrLinkStoricoRevocato
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return PaginaCliente{}, ErrBustaNonTrovata
	}
	// Una riga che non si scrive è il rifiuto (il with di legacy/2 cade nel ramo del 404).
	if err := b.reg.Registra(ctx, c, "repair.legacy_public_viewed", "repair", s.id, "ok", nil); err != nil {
		b.rifiutoStorico(ctx, c)
		return PaginaCliente{}, ErrBustaNonTrovata
	}
	p, err := b.paginaDove(ctx, `id = ? AND cancellata_il IS NULL`, []any{s.id}, true)
	if err != nil {
		return PaginaCliente{}, err
	}
	p.SlugFoto = strings.ReplaceAll(scritto, "/", "_")
	scad := b.o.Adesso().Unix() + durataFotoStoriche
	p.ScadenzaFoto = strconv.FormatInt(scad, 10)
	p.FirmeFoto = make([]string, len(p.Foto))
	for i, f := range p.Foto {
		p.FirmeFoto[i] = b.segretoFlask.FirmaFotoBusta(p.SlugFoto, f.NomeFile, scad)
	}
	return p, nil
}

// ConversazioneStorica è legacy_poll/2: la conversazione della busta per lo script della pagina
// storica, con la credenziale con cui la pagina è stata aperta. Busta che non c'è, revocata,
// credenziale sbagliata, conversazione nascosta (chiusa, o B2B sul link storico): ErrBustaNonTrovata
// con la riga repair.legacy_public_denied. Riuscita non scrive niente.
func (b *Buste) ConversazioneStorica(ctx context.Context, scritto string, cred CredenzialeStorica, c audit.Contesto) ([]Messaggio, error) {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return nil, err
	}
	if !ok || !b.autorizzata(s, scritto, cred) || s.b2b() || ConversazioneChiusa(s.stato) {
		b.rifiutoStorico(ctx, c)
		return nil, ErrBustaNonTrovata
	}
	ms, _, err := b.conversazionePubblica(ctx, s.id, s.stato, s.oreficeriaID, s.visibiliDal)
	return ms, err
}

// ScriviMessaggioStorico è legacy_message/2: il messaggio dalla pagina storica, con la credenziale.
// Busta che non c'è, revocata o credenziale sbagliata: ErrBustaNonTrovata con la riga
// repair.legacy_public_denied, prima di guardare il testo. Poi le regole di PublicMessage.post/5 con
// il link :legacy: testo vuoto o troppo lungo, busta chiusa, limite; una busta B2B (nessuna
// conversazione sul link storico) è ErrBustaNonTrovata con la riga del rifiuto, come il codice
// sbagliato.
func (b *Buste) ScriviMessaggioStorico(ctx context.Context, scritto string, cred CredenzialeStorica, testo string, c audit.Contesto) error {
	s, ok, err := b.cercaStorica(ctx, scritto)
	if err != nil {
		return err
	}
	if !ok || !b.autorizzata(s, scritto, cred) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	testo, err = testoMessaggio(testo)
	if err != nil {
		return err
	}
	err = b.scriviMessaggioPubblico(ctx, s.id, true, testo, c)
	if errors.Is(err, errConversazioneNascosta) {
		b.rifiutoStorico(ctx, c)
		return ErrBustaNonTrovata
	}
	return err
}

// LeggiFotoStorica è legacy_photo/2 (e legacy_thumb/2, con la miniatura): la foto file della busta
// dello slug con la firma ?sig=&exp=, nella variante v. nil (il 404 «Foto non disponibile», senza
// righe) per la busta che non c'è o è revocata e per la firma sbagliata o scaduta; con la firma
// giusta, una riga repair.public_photo_viewed («ok» o «denied», photo_id «filename», audience
// «customer») e i byte solo per una foto della busta visibile al cliente. Un errore vuol dire che la
// riga non si è scritta: niente byte.
func (b *Buste) LeggiFotoStorica(ctx context.Context, slug, file, firma, scad string, v Variante, c audit.Contesto) (*MediaFoto, error) {
	if b.archivio == nil {
		return nil, ErrSenzaArchivio
	}
	s, ok, err := b.cercaPerSlug(ctx, slug)
	if err != nil || !ok || s.revocata {
		return nil, err
	}
	if !b.segretoFlask.VerificaFotoBusta(slug, file, firma, scad, b.o.Adesso().Unix()) {
		return nil, nil
	}
	busta := BustaDellaFoto{ID: s.id, OreficeriaID: s.oreficeriaID}
	var m *MediaFoto
	f, trovata, err := fotoPerNome(ctx, b.d.Lettura, s.id, file)
	if err != nil {
		return nil, err
	}
	if trovata && Visibile(busta, f, PubblicoCliente) {
		m = b.mediaDi(ctx, f, v)
	}
	esito := "denied"
	if m != nil {
		esito = "ok"
	}
	dati := map[string]any{"photo_id": "filename", "audience": string(PubblicoCliente)}
	if v == VarianteMiniatura {
		dati["variant"] = string(VarianteMiniatura)
		if m != nil {
			dati["variant"] = string(m.Variante)
		}
	}
	if err := b.reg.Registra(ctx, c, "repair.public_photo_viewed", "repair", s.id, esito, dati); err != nil {
		return nil, err
	}
	return m, nil
}

// fotoPerNome è Repo.get_by(RepairPhoto, repair_id:, original_filename:): la foto della busta con
// quel nome di file. Lo standalone va in errore (500) se due foto della busta hanno lo stesso nome;
// qui vale la prima per id (vedi i Dubbi della slice 064).
func fotoPerNome(ctx context.Context, q interroga, bustaID int64, nome string) (Foto, bool, error) {
	f, err := leggiFoto(q.QueryRowContext(ctx, `SELECT `+colonneFoto+` FROM foto_buste WHERE busta_id = ? AND nome_file = ?
		ORDER BY id LIMIT 1`, bustaID, nome))
	if errors.Is(err, sql.ErrNoRows) {
		return Foto{}, false, nil
	}
	return f, err == nil, err
}
