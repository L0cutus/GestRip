package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
)

type istanza struct {
	Nome   string
	Base   string
	Client *http.Client
}

type rapporto struct {
	Scenari, Passi   int
	Attese, Inattese int
	Righe            []string
	Errori           []string
	Verdi            map[string]bool // scenario → nessuna differenza inattesa e nessun errore
}

func (r rapporto) ok() bool { return r.Inattese == 0 && len(r.Errori) == 0 && r.Passi > 0 }

// credenziali sono quelle del titolare e del dipendente sintetici, uguali sulle due istanze.
type credenziali struct {
	Email, Password, EmailDipendente, PasswordDipendente string
	// IPCliente è {ip_cliente}: nuovo per ogni scenario, uguale sulle due istanze.
	IPCliente string
}

// ipCasuale è un indirizzo IPv6 di documentazione (2001:db8::/32) con un /64 casuale: i limiti
// ai tentativi di uno scenario non toccano gli altri scenari né i giri dopo.
func ipCasuale() string {
	var b [4]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read non fallisce
	return fmt.Sprintf("2001:db8:%x:%x::1", uint16(b[0])<<8|uint16(b[1]), uint16(b[2])<<8|uint16(b[3]))
}

var reTokenPagina = regexp.MustCompile(`name="csrf-token" content="([^"]+)"`)

// chiedi esegue un passo su un'istanza, senza seguire i redirect (si confronta il 30x). token è
// il token CSRF dell'ultima pagina di quell'istanza in questo scenario.
func chiedi(i istanza, p passo) (risposta, error) {
	return chiediCon(i, p, "", credenziali{})
}

func chiediCon(i istanza, p passo, token string, cred credenziali) (risposta, error) {
	var corpo io.Reader
	if p.Corpo != "" {
		if strings.Contains(p.Corpo, "{csrf}") && token == "" {
			return risposta{}, fmt.Errorf("{csrf} senza una pagina che dia il token")
		}
		corpo = strings.NewReader(strings.NewReplacer("{csrf}", url.QueryEscape(token),
			"{email}", url.QueryEscape(cred.Email), "{password}", url.QueryEscape(cred.Password),
			"{email_dipendente}", url.QueryEscape(cred.EmailDipendente),
			"{password_dipendente}", url.QueryEscape(cred.PasswordDipendente),
			"{ip_cliente}", url.QueryEscape(cred.IPCliente)).Replace(p.Corpo))
	}
	// Il percorso si attacca alla base: anche «//altro/x» resta un percorso sull'istanza (test).
	req, err := http.NewRequest(p.Metodo, strings.TrimRight(i.Base, "/")+p.Percorso, corpo)
	if err != nil {
		return risposta{}, err
	}
	if p.Corpo != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range p.Intestazioni {
		req.Header.Set(k, strings.ReplaceAll(v, "{ip_cliente}", cred.IPCliente))
	}
	c := *i.Client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		return risposta{}, err
	}
	defer func() { _ = res.Body.Close() }() // in lettura: l'errore utile è quello di ReadAll
	letto, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return risposta{}, err
	}
	return risposta{Codice: res.StatusCode, Corpo: string(letto), Intestazioni: res.Header}, nil
}

// passoSoloGo esegue un passo SoloGo sul Go, con i segnaposto presi dalla risposta prima del Go.
func passoSoloGo(gox istanza, p passo, token string, cred credenziali, prima risposta) (risposta, error) {
	percorso, err := percorsoDopo(p.Percorso, prima)
	if err != nil {
		return risposta{}, err
	}
	p.Percorso = percorso
	// Senza segno nella pagina di prima (database vuoto: la scheda non c'è) {mostra} è vuoto, come un
	// modulo senza il campo: le attese dei passi dopo dicono che cosa deve succedere.
	segno := ""
	if m := reSegnoPagina.FindStringSubmatch(prima.Corpo); m != nil {
		segno = m[1]
	}
	p.Corpo = strings.ReplaceAll(p.Corpo, "{mostra}", url.QueryEscape(segno))
	return chiediCon(gox, p, token, cred)
}

// eseguiScenari fa girare ogni passo su standalone e Go e classifica le differenze.
func eseguiScenari(ss []scenario, devs deviazioni, standalone, gox istanza) rapporto {
	return eseguiScenariCon(ss, devs, standalone, gox, credenziali{}, "")
}

// conCookie dà un client con i cookie vuoti: ogni scenario parte da un browser nuovo.
func conCookie(i istanza) istanza {
	c := *i.Client
	c.Jar, _ = cookiejar.New(nil) // New(nil) non fallisce
	i.Client = &c
	return i
}

// eseguiScenariCon è eseguiScenari con le credenziali sintetiche e la modalità dei dati da cui è
// partito il banco ("" = account seminati, oppure datiSinteticiImport), che sceglie le attese.
func eseguiScenariCon(ss []scenario, devs deviazioni, standalone0, gox0 istanza, cred credenziali, dati string) rapporto {
	rap := rapporto{Verdi: map[string]bool{}}
	// Una modalità sconosciuta non ripiega sulle attese di base: nessuna richiesta, banco rosso.
	if _, err := modalitaDati(dati); err != nil {
		rap.Errori = append(rap.Errori, err.Error())
		return rap
	}
	for _, s := range ss {
		rap.Scenari++
		prima := rap.Inattese + len(rap.Errori)
		standalone, gox := conCookie(standalone0), conCookie(gox0)
		cred := cred
		cred.IPCliente = ipCasuale()
		var tokA, tokB string
		var primaB risposta
		for n, p := range s.Passi {
			if p.SoloGo != "" {
				b, err := passoSoloGo(gox, p, tokB, cred, primaB)
				if err != nil {
					rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s %s (solo Go, %s): %v", s.Nome, n, p.Metodo, p.Percorso, p.SoloGo, err))
					primaB = risposta{} // i segnaposti dopo non vengono da una risposta vecchia
					continue
				}
				if m := reTokenPagina.FindStringSubmatch(b.Corpo); m != nil {
					tokB = m[1]
				}
				primaB = b
				rap.Passi++
				for c, v := range attesePer(p, dati) {
					if vb := b.valore(c); vb != v {
						rap.Inattese++
						rap.Righe = append(rap.Righe, "INATTESA "+differenza{Scenario: s.Nome, Passo: n,
							Campo: "atteso " + c + "=" + v + " (solo Go, " + p.SoloGo + ")", Standalone: "(non eseguito)", Go: vb}.String())
					}
				}
				continue
			}
			if p.SoloStandalone != "" {
				a, err := chiediCon(standalone, p, tokA, cred)
				if err != nil {
					rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s %s (solo standalone, %s): %v", s.Nome, n, p.Metodo, p.Percorso, p.SoloStandalone, err))
					continue
				}
				if m := reTokenPagina.FindStringSubmatch(a.Corpo); m != nil {
					tokA = m[1]
				}
				rap.Passi++
				for c, v := range attesePer(p, dati) {
					if va := a.valore(c); va != v {
						rap.Inattese++
						rap.Righe = append(rap.Righe, "INATTESA "+differenza{Scenario: s.Nome, Passo: n,
							Campo: "atteso " + c + "=" + v + " (solo standalone, " + p.SoloStandalone + ")", Standalone: va, Go: "(non eseguito)"}.String())
					}
				}
				continue
			}
			a, errA := chiediCon(standalone, p, tokA, cred)
			b, errB := chiediCon(gox, p, tokB, cred)
			if m := reTokenPagina.FindStringSubmatch(a.Corpo); m != nil {
				tokA = m[1]
			}
			if m := reTokenPagina.FindStringSubmatch(b.Corpo); m != nil {
				tokB = m[1]
			}
			primaB = b
			if errA != nil || errB != nil {
				rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s %s: standalone %v, go %v", s.Nome, n, p.Metodo, p.Percorso, errA, errB))
				continue
			}
			rap.Passi++
			for _, d := range confrontaCon(p, dati, a, b) {
				d.Scenario, d.Passo = s.Nome, n
				if id, ok := devs.copre(d); ok {
					rap.Attese++
					rap.Righe = append(rap.Righe, "ATTESA ("+id+") "+d.String())
				} else {
					rap.Inattese++
					rap.Righe = append(rap.Righe, "INATTESA "+d.String())
				}
			}
		}
		rap.Verdi[s.Nome] = rap.Inattese+len(rap.Errori) == prima
	}
	return rap
}
