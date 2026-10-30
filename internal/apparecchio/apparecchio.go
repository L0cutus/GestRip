// Package apparecchio legge la configurazione della macchina, quella che scrive l'installer e che
// servono più comandi: per ora l'indirizzo della faccia interna, su cui si costruiscono i link
// (reset, inviti). È l'equivalente di PHX_HOST e PHX_URL_PORT dello standalone: sta sulla
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
	"regexp"
	"strconv"
	"strings"
)

// PercorsoConfigurazione è il file di configurazione dell'apparecchio.
const PercorsoConfigurazione = "/etc/gestrip/apparecchio.conf"

// ComandoRiavvio riavvia gestrip serve sull'apparecchio, che legge il file solo all'avvio. Il
// servizio systemd si chiama gestrip (lo installa l'installer, T5).
const ComandoRiavvio = "sudo systemctl restart gestrip"

const chiaveIndirizzoInterno = "indirizzo_interno"

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
		default:
			return Configurazione{}, fmt.Errorf("%s, riga %d: chiave sconosciuta %q", percorso, n, k)
		}
	}
	return c, sc.Err()
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
func ControllaIndirizzo(s string) (string, error) {
	sbagliato := errors.New("l'indirizzo della faccia interna deve essere https://host[:porta]")
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
