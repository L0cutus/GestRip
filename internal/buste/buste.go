// Package buste è il nucleo di dominio della busta (T4.1), senza pagine: creazione con il cliente
// (RIP-H-30, 33, 34, 35, 38, 39), numerazione N/AA (RIP-H-31), prezzi a testo (RIP-H-36,
// RIP-S-05), consegna prevista (RIP-H-37, RIP-S-06), stati con la chiusura del lotto alla consegna
// (RIP-S-13), riapertura (RIP-S-14) e attesa del cliente (RIP-S-15). Copia Workshop e
// Workshop.Repair dello standalone (go-baseline) e, per il cliente, resolve_intake_customer/2 di
// WorkshopLive.
//
// I testi arrivano come li manda il modulo. Come il cast di Ecto, un testo vuoto o fatto di soli
// spazi vale «niente» (NULL); un testo scritto si salva com'è, spazi ai bordi compresi. I messaggi
// d'errore sono quelli dello standalone, parola per parola; quelli di Ecto come li mostra il suo
// catalogo italiano (priv/gettext/it/LC_MESSAGES/errors.po, slice 220): «Campo obbligatorio.»,
// «Valore non valido.».
package buste

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/media"
)

// Buste lavora sulle buste del database.
type Buste struct {
	d   *db.DB
	o   clock.Orologio
	reg *audit.Registro
	// dopoLettura, solo nei test, gira in ApriScheda dopo la lettura della busta e prima dei segni
	// «letto»: quello che un'altra richiesta può fare in quel momento. nil fuori dai test.
	dopoLettura func()
	// nuovoGettone, solo nei test, sta al posto di gettone in NuovoLink: un token noto, per provare
	// che gli errori non lo portano. nil fuori dai test.
	nuovoGettone func() (string, []byte)
	// archivio e normalizzatore servono alle foto (ConFoto); nil: le foto non si allegano.
	archivio       *media.Archivio
	normalizzatore Normalizzatore
	// miniature fa le miniature delle foto (ConMiniature); nil: si serve sempre la foto intera.
	miniature MiniatureFoto
	// riscaldatore prepara le miniature dopo un caricamento (ConRiscaldatore); nil: niente.
	riscaldatore Riscaldatore
}

// Nuove dà le buste del database d, con l'orologio del negozio.
func Nuove(d *db.DB, o clock.Orologio) *Buste {
	return &Buste{d: d, o: o, reg: audit.Nuovo(d, o)}
}

func (b *Buste) adesso() (time.Time, string) {
	t := b.o.Adesso().UTC()
	return t, t.Format(db.FormatoIstante)
}

// Rifiuto è un no con il messaggio dello standalone, da mostrare com'è (un flash nella pagina).
type Rifiuto string

func (r Rifiuto) Error() string { return string(r) }

// I rifiuti, con i testi dello standalone.
const (
	ErrNomeCliente   Rifiuto = "Inserisci il nome del cliente."
	ErrCliente       Rifiuto = "Controlla nome e telefono del cliente."
	ErrOreficeria    Rifiuto = "Seleziona un'oreficeria valida."
	ErrArtigiano     Rifiuto = "Seleziona un artigiano valido."
	ErrTransizione   Rifiuto = "Transizione non consentita."
	ErrImportoLungo  Rifiuto = "Importo troppo lungo: massimo 255 caratteri."
	ErrConsegnaLunga Rifiuto = "Consegna troppo lunga: massimo 255 caratteri."
)

// MessaggioNonSalvata è quello che la pagina mostra per ogni altro errore della creazione.
const MessaggioNonSalvata = "Non è stato possibile salvare la riparazione."

// I messaggi sui campi del modulo. Quelli di Ecto sono i testi del catalogo it dello standalone
// (slice 220): accanto, il msgid inglese.
const (
	MsgObbligatorio    = "Campo obbligatorio." // can't be blank
	MsgNonValido       = "Valore non valido."  // is invalid
	MsgFormatoNumero   = "formato richiesto N/AA (es. 12/26)"
	MsgNumeroEsistente = "Numero busta già esistente."
)

// Campi sono gli errori del modulo, per campo, tutti insieme come nel changeset: restano sul
// modulo con i valori scritti. I campi: numero, data_accettazione, data_prevista, descrizione,
// preventivo.
type Campi map[string][]string

func (c Campi) aggiungi(campo, messaggio string) { c[campo] = append(c[campo], messaggio) }

func (c Campi) Error() string {
	nomi := make([]string, 0, len(c))
	for n := range c {
		nomi = append(nomi, n)
	}
	sort.Strings(nomi)
	parti := make([]string, 0, len(nomi))
	for _, n := range nomi {
		parti = append(parti, fmt.Sprintf("%s: %s", n, strings.Join(c[n], ", ")))
	}
	return "buste: " + strings.Join(parti, "; ")
}

// vuoto dice se un testo del modulo vale «niente» per il cast di Ecto (empty_trimmed_string?).
func vuoto(s string) bool { return strings.TrimSpace(s) == "" }

// testoONulla è il testo com'è, o NULL se vuoto.
func testoONulla(s string) any {
	if vuoto(s) {
		return nil
	}
	return s
}

// CampiRifiutati sono gli errori del modulo con il numero che la creazione aveva risolto (il
// prossimo dell'anno, o quello scritto senza spazi ai bordi): il changeset dello standalone lo
// porta nei protected_attrs, quindi il modulo lo rimostra. Il contatore non avanza: la
// transazione si annulla. errors.As trova anche Campi.
type CampiRifiutati struct {
	Campi  Campi
	Numero string
}

func (e *CampiRifiutati) Error() string { return e.Campi.Error() }

// Unwrap dà i Campi.
func (e *CampiRifiutati) Unwrap() error { return e.Campi }
