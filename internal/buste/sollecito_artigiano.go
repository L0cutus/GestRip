package buste

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
)

// Il popup «🔧 Sollecita artigiano» della scheda (slice 085; ART-23, CLI-29 parte artigiano), come il
// purpose :artisan di CustomerNotice e Outgoing.destination_for_artisan/1, compose_artisan/1 e
// resolve_artisan_link/2 dello standalone (go-baseline):
//   - il destinatario è sempre l'artigiano della busta, qualunque sia la controparte (una busta B2B non
//     raggiunge così né l'oreficeria né il cliente finale); una busta senza artigiano, o con un
//     artigiano senza un numero con delle cifre, ha il problema «nessun recapito»; una busta chiusa
//     (consegnata, annullata, chiusa) il problema «chiusa», anche se l'artigiano ha il numero;
//   - il testo è wa_artigiano: il nome dell'artigiano (o «Collega»), il numero dei testi senza «N.»
//     (D-64 e D-82), la prima riga della lavorazione, lo stato in maiuscolo, la consegna e le note
//     pubbliche come righe «Etichetta: valore», {link} letterale fino all'invio, il negozio. Niente del
//     cliente e nessun importo;
//   - l'«Invia» rilegge la busta e rifiuta senza scrivere se dal popup sono cambiati l'artigiano
//     (ErrArtigianoCambiato), il suo numero (ErrRecapitoCambiato) o se la busta si è chiusa
//     (ErrBustaChiusa); poi {link} diventa il link dell'artigiano (RisolviLinkArtigiano: firmato, non
//     salvato, nessuna riga del registro; senza indirizzo pubblico ErrSenzaIndirizzoPubblico), poi la
//     riga WhatsApp «accepted» verso «artisan» con outgoing.handed_off. Nessun timbro sulla busta.

// ScopoArtigiano è il purpose :artisan: «🔧 Sollecita artigiano» della scheda.
const ScopoArtigiano ScopoAvviso = "artigiano"

// TipoSollecito è il kind :artisan della bozza.
const TipoSollecito TipoAvviso = "artisan"

// ParteArtigiano è la parte :artisan: l'artigiano della busta, solo per il sollecito.
const ParteArtigiano Parte = 2

// ProblemaChiusa è :closed: la busta è chiusa e l'artigiano non si sollecita.
const ProblemaChiusa ProblemaAvviso = "closed"

var (
	ErrArtigianoCambiato = errors.New("buste: artigiano cambiato") // :artisan_changed
	ErrBustaChiusa       = errors.New("buste: busta chiusa")       // :closed
)

// rigaSollecito è la busta come la legge il sollecito: quella dell'avviso più il suo artigiano.
type rigaSollecito struct {
	rigaAvviso
	artigianoID                      *int64
	artigianoNome, artigianoTelefono *string
}

// leggiSollecito legge la busta (fuori dal cestino) con il suo artigiano.
func leggiSollecito(ctx context.Context, q lettore, id int64) (rigaSollecito, error) {
	r, err := leggiAvviso(ctx, q, id)
	if err != nil {
		return rigaSollecito{}, err
	}
	s := rigaSollecito{rigaAvviso: r}
	err = q.QueryRowContext(ctx, `SELECT b.artigiano_id, a.nome, a.telefono FROM buste b LEFT JOIN artigiani a ON a.id = b.artigiano_id
		WHERE b.id = ?`, id).Scan(&s.artigianoID, &s.artigianoNome, &s.artigianoTelefono)
	if errors.Is(err, sql.ErrNoRows) {
		return rigaSollecito{}, ErrBustaNonTrovata
	}
	if err != nil {
		return rigaSollecito{}, err
	}
	if s.artigianoID != nil && s.artigianoNome == nil {
		// L'artigiano non precaricato fa sollevare lo standalone: la chiave esterna lo impedisce.
		return rigaSollecito{}, errors.New("buste: la busta punta a un artigiano che non c'è")
	}
	return s, nil
}

// destinazioneArtigiano è destination_for_artisan/1: il numero per wa.me dell'artigiano, o il motivo
// per cui non c'è (ErrBustaChiusa, ErrNessunRecapito anche senza artigiano).
func (r rigaSollecito) destinazioneArtigiano() (string, error) {
	switch {
	case r.artigianoID == nil:
		return "", ErrNessunRecapito
	case slices.Contains(statiChiusi, r.stato):
		return "", ErrBustaChiusa
	}
	n := NumeroWhatsApp(valoreTesto(r.artigianoTelefono))
	if n == "" {
		return "", ErrNessunRecapito
	}
	return n, nil
}

// apriSollecito è CustomerNotice.open(repair, :artisan): compone la bozza e non scrive niente.
func (b *Buste) apriSollecito(ctx context.Context, id int64) (BozzaAvviso, error) {
	r, err := leggiSollecito(ctx, b.d.Lettura, id)
	if err != nil {
		return BozzaAvviso{}, err
	}
	z := BozzaAvviso{BustaID: r.id, Scopo: ScopoArtigiano, Tipo: TipoSollecito, Canale: CanaleWhatsApp, Parte: ParteArtigiano,
		OreficeriaID: r.oreficeriaID, ArtigianoID: r.artigianoID, Chiusa: slices.Contains(statiChiusi, r.stato)}
	z.Numero, _ = NumeroVisibile(r.codiceQR, r.numero)
	indirizzo, err := r.destinazioneArtigiano()
	switch {
	case errors.Is(err, ErrBustaChiusa):
		z.Problema = ProblemaChiusa
		return z, nil
	case err != nil:
		z.Problema = ProblemaNessunRecapito
		return z, nil
	}
	z.Indirizzo, z.Nome = indirizzo, valoreTesto(r.artigianoNome)
	p, err := b.leggiProfiloAvviso(ctx)
	if err != nil {
		return BozzaAvviso{}, err
	}
	nome := presente(r.artigianoNome)
	if nome == "" {
		nome = "Collega"
	}
	// compose_artisan/1; il numero dei testi senza «N.» (D-82).
	valori := map[string]string{"artigiano_nome": nome, "numero": NumeroNeiTesti(r.codiceQR, r.numero),
		"lavorazione": riga("Lavorazione", primaRiga(r.descrizione)),
		"stato":       strings.ToUpper(strings.ReplaceAll(r.stato, "_", " ")),
		"consegna":    riga("Consegna prevista", consegnaAvviso(r.rigaAvviso)), "note_riga": riga("Note", presente(r.notePubbliche)),
		"link": segnapostoLink, "negozio": p.negozio()}
	if z.Testo, z.Modello, err = b.componiFamiglia(ctx, FamArtigiano, valori); err != nil {
		return BozzaAvviso{}, err
	}
	return z, nil
}

// inviaSollecito è send_to/4 per il kind :artisan (con deliver/3 per una busta senza artigiano): rilegge la busta, rifiuta senza scrivere se dal
// popup è cambiato qualcosa, mette il link dell'artigiano al posto di {link} e registra la riga.
func (b *Buste) inviaSollecito(ctx context.Context, id int64, dis Disegnata, testo string, c audit.Contesto) (AvvisoInviato, error) {
	if dis.Tipo != TipoSollecito {
		return AvvisoInviato{}, ErrTipoCambiato
	}
	r, err := leggiSollecito(ctx, b.d.Lettura, id)
	if err != nil {
		return AvvisoInviato{}, err
	}
	// same(repair.artisan_id, draft.artisan_id, :artisan_changed): anche da nessuno a qualcuno e viceversa.
	if (r.artigianoID == nil) != (dis.ArtigianoID == nil) || (r.artigianoID != nil && *r.artigianoID != *dis.ArtigianoID) {
		return AvvisoInviato{}, ErrArtigianoCambiato
	}
	if r.artigianoID == nil {
		// Solo un modulo forzato (il popup senza artigiano non ha il modulo): la bozza dello standalone ha il
		// problema :no_address e deliver/3 rifiuta con refusal(:no_address).
		return AvvisoInviato{}, ErrNessunRecapito
	}
	indirizzo, err := r.destinazioneArtigiano()
	switch {
	case errors.Is(err, ErrBustaChiusa):
		return AvvisoInviato{}, ErrBustaChiusa
	case err != nil:
		return AvvisoInviato{}, ErrRecapitoCambiato
	}
	if dis.Canale != CanaleWhatsApp {
		return AvvisoInviato{}, ErrCanaleCambiato
	}
	if indirizzo != dis.Indirizzo {
		return AvvisoInviato{}, ErrRecapitoCambiato
	}
	if testo, err = b.RisolviLinkArtigiano(r.id, testo); err != nil {
		return AvvisoInviato{}, err
	}
	testo = strings.TrimFunc(testo, unicode.IsSpace)
	m := consegnaAMano{bustaID: r.id, parte: ParteArtigiano, indirizzo: indirizzo, testo: testo}
	if err := b.inTx(ctx, func(tx *sql.Tx, ora string) error {
		_, err := b.registraConsegna(ctx, tx, m, ora, c)
		return err
	}); err != nil {
		return AvvisoInviato{}, err
	}
	return AvvisoInviato{URL: IndirizzoWaMe(indirizzo, testo), Tipo: TipoSollecito}, nil
}
