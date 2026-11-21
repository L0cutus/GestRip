// Package apparecchio legge la configurazione della macchina, quella che scrive l'installer e che
// servono più comandi: gli indirizzi delle due facce, su cui si costruiscono i link (interna:
// reset, inviti; pubblica: il link cliente /r/ di una busta nuova). È l'equivalente di PHX_HOST e PHX_URL_PORT dello standalone: sta sulla
// macchina, non nel database, quindi non viaggia con il backup (su un'altra macchina l'indirizzo è
// un altro) e un link non si costruisce mai sull'Host della richiesta (ACC-13).
package apparecchio

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/L0cutus/gestrip/internal/sidecar"
)

// PercorsoConfigurazione è il file di configurazione dell'apparecchio.
const PercorsoConfigurazione = "/etc/gestrip/apparecchio.conf"

// ComandoRiavvio riavvia gestrip serve sull'apparecchio, che legge il file solo all'avvio. Il
// servizio systemd si chiama gestrip (lo installa l'installer, T5).
const ComandoRiavvio = "sudo systemctl restart gestrip"

const (
	chiaveIndirizzoInterno  = "indirizzo_interno"
	chiaveIndirizzoPubblico = "indirizzo_pubblico"
)

// Famiglie dei sidecar dell'IA (PIANO §1.8, slice 111): per ognuna una riga con il programma e
// gli argomenti, una con la porta e una facoltativa con il percorso di salute.
var famiglieSidecar = []string{"estrazione", "trascrizione"}

// massimoFile: la configurazione è di poche righe; un file più grande è un errore. Sta sotto il
// limite di una riga di bufio.Scanner, quindi nessuna riga si tronca.
const massimoFile = 16 << 10

// ErrNonConfigurato: l'apparecchio non ha ancora l'indirizzo della faccia interna.
var ErrNonConfigurato = errors.New("indirizzo della faccia interna non configurato")

// ComeConfigurare dice quale riga scrivere e in quale file, per chi trova ErrNonConfigurato.
// Senza percorso (-config vuoto) non c'è un file da indicare.
func ComeConfigurare(percorso string) string {
	if percorso == "" {
		return "scrivi «indirizzo_interno = https://host[:porta]» in un file e daglielo con -config"
	}
	return "scrivi «indirizzo_interno = https://host[:porta]» in " + percorso
}

// Configurazione è il contenuto del file dell'apparecchio. Un campo vuoto non è configurato.
type Configurazione struct {
	IndirizzoInterno string
	// IndirizzoPubblico è la faccia pubblica (Funnel, porta 443: D-26), su cui si costruisce il
	// link cliente di una busta (Endpoint.url() dello standalone). Decisione del 31/10/2026.
	IndirizzoPubblico string
	// Sidecar sono i sidecar dell'IA da avviare (slice 111), nell'ordine estrazione, trascrizione;
	// vuoto (il default) vuol dire nessun sidecar: GestRip parla solo con i server indicati nelle
	// impostazioni dell'assistente.
	//
	//	sidecar_estrazione = /percorso/assoluto/llama-server -m /percorso/modello.gguf --host 127.0.0.1 --port {porta} -ngl 0
	//	sidecar_estrazione_porta = 47301
	//	sidecar_estrazione_salute = /health   (facoltativa, di default /health)
	//
	// Il programma e gli argomenti sono separati da spazi (niente virgolette: un percorso con uno
	// spazio non si può scrivere); {porta} diventa la porta. Il figlio non riceve l'ambiente di
	// GestRip.
	Sidecar []sidecar.Comando
}

// Leggi legge il file di configurazione: righe «chiave = valore», righe vuote e commenti con #.
// Un file che manca è una configurazione vuota; una chiave sconosciuta o ripetuta, una riga
// senza «=» o un indirizzo sbagliato sono errori, mai un link.
func Leggi(percorso string) (Configurazione, error) {
	var c Configurazione
	b, err := os.ReadFile(percorso)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if len(b) > massimoFile {
		return c, fmt.Errorf("%s: file troppo grande", percorso)
	}
	viste := map[string]bool{}
	sidecarLetti := map[string]*sidecar.Comando{}
	perFamiglia := func(f string) *sidecar.Comando {
		if sidecarLetti[f] == nil {
			sidecarLetti[f] = &sidecar.Comando{Nome: f}
		}
		return sidecarLetti[f]
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		riga := strings.TrimSpace(sc.Text())
		if riga == "" || strings.HasPrefix(riga, "#") {
			continue
		}
		k, v, ok := strings.Cut(riga, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || viste[k] {
			return Configurazione{}, fmt.Errorf("%s, riga %d: serve «chiave = valore», una volta per chiave", percorso, n)
		}
		viste[k] = true
		switch k {
		case chiaveIndirizzoInterno:
			s, err := ControllaIndirizzo(v)
			if err != nil {
				return Configurazione{}, fmt.Errorf("%s, riga %d: %w", percorso, n, err)
			}
			c.IndirizzoInterno = s
		case chiaveIndirizzoPubblico:
			s, err := controlla(v, "pubblica")
			if err != nil {
				return Configurazione{}, fmt.Errorf("%s, riga %d: %w", percorso, n, err)
			}
			c.IndirizzoPubblico = s
		default:
			f, campo, ok := chiaveSidecar(k)
			if !ok {
				return Configurazione{}, fmt.Errorf("%s, riga %d: chiave sconosciuta %q", percorso, n, k)
			}
			if err := leggiSidecar(perFamiglia(f), campo, v); err != nil {
				return Configurazione{}, fmt.Errorf("%s, riga %d: %s: %w", percorso, n, k, err)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	for _, f := range famiglieSidecar {
		s := sidecarLetti[f]
		if s == nil {
			continue
		}
		if s.Programma == "" || s.Porta == 0 {
			return Configurazione{}, fmt.Errorf("%s: il sidecar %s vuole sia «sidecar_%s» sia «sidecar_%s_porta»", percorso, f, f, f)
		}
		if err := s.Controlla(); err != nil {
			return Configurazione{}, fmt.Errorf("%s: %w", percorso, err)
		}
		for _, altro := range c.Sidecar {
			if altro.Porta == s.Porta {
				return Configurazione{}, fmt.Errorf("%s: i sidecar %s e %s non possono avere la stessa porta", percorso, altro.Nome, s.Nome)
			}
		}
		c.Sidecar = append(c.Sidecar, *s)
	}
	return c, nil
}

// chiaveSidecar riconosce sidecar_<famiglia>[_porta|_salute].
func chiaveSidecar(k string) (famiglia, campo string, ok bool) {
	for _, f := range famiglieSidecar {
		switch k {
		case "sidecar_" + f:
			return f, "comando", true
		case "sidecar_" + f + "_porta":
			return f, "porta", true
		case "sidecar_" + f + "_salute":
			return f, "salute", true
		}
	}
	return "", "", false
}

// leggiSidecar mette il valore v nel campo del sidecar s.
func leggiSidecar(s *sidecar.Comando, campo, v string) error {
	switch campo {
	case "comando":
		parti := strings.Fields(v)
		if len(parti) == 0 || !filepath.IsAbs(parti[0]) {
			return errors.New("serve il percorso assoluto del programma, poi gli argomenti")
		}
		s.Programma, s.Argomenti = parti[0], parti[1:]
	case "porta":
		n, err := strconv.Atoi(v)
		if err != nil || n < sidecar.PortaMinima || n > 65535 {
			return fmt.Errorf("la porta deve essere un numero fra %d e 65535", sidecar.PortaMinima)
		}
		s.Porta = n
	case "salute":
		if v == "" {
			return errors.New("il percorso di salute non può essere vuoto")
		}
		s.Salute = v
	}
	return nil
}

// IndirizzoInterno dà l'indirizzo della faccia interna dal file; ErrNonConfigurato se manca.
func IndirizzoInterno(percorso string) (string, error) {
	c, err := Leggi(percorso)
	if err != nil {
		return "", err
	}
	if c.IndirizzoInterno == "" {
		return "", ErrNonConfigurato
	}
	return c.IndirizzoInterno, nil
}

var reNomeHost = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// ControllaIndirizzo controlla l'indirizzo della faccia interna e lo dà nella forma dei link:
// https, un nome di host valido (o un indirizzo IP), al più una porta da 1 a 65535, niente altro;
// senza barra finale e senza la porta 443, come lo standalone. Dove punti lo decide il titolare,
// che configura l'apparecchio.
func ControllaIndirizzo(s string) (string, error) { return controlla(s, "interna") }

// controlla è ControllaIndirizzo per una faccia: l'errore la nomina.
func controlla(s, faccia string) (string, error) {
	sbagliato := errors.New("l'indirizzo della faccia " + faccia + " deve essere https://host[:porta]")
	u, err := url.Parse(strings.TrimSuffix(s, "/"))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.Fragment != "" || strings.ContainsAny(s, "?#") {
		return "", sbagliato
	}
	host, porta := u.Hostname(), u.Port()
	if _, e := netip.ParseAddr(host); e != nil && !reNomeHost.MatchString(strings.ToLower(host)) {
		return "", sbagliato
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", sbagliato
	}
	if porta == "" {
		return "https://" + u.Host, nil
	}
	n, err := strconv.Atoi(porta)
	if err != nil || n < 1 || n > 65535 {
		return "", sbagliato
	}
	if n == 443 {
		if strings.Contains(host, ":") {
			return "https://[" + host + "]", nil
		}
		return "https://" + host, nil
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(n)), nil
}
