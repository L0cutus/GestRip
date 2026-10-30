package main

import (
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

// credenziali sono quelle del titolare sintetico, uguali sulle due istanze.
type credenziali struct{ Email, Password string }

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
			"{email}", url.QueryEscape(cred.Email), "{password}", url.QueryEscape(cred.Password)).Replace(p.Corpo))
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
		req.Header.Set(k, v)
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

// eseguiScenari fa girare ogni passo su standalone e Go e classifica le differenze.
func eseguiScenari(ss []scenario, devs deviazioni, standalone, gox istanza) rapporto {
	return eseguiScenariCon(ss, devs, standalone, gox, credenziali{})
}

// conCookie dà un client con i cookie vuoti: ogni scenario parte da un browser nuovo.
func conCookie(i istanza) istanza {
	c := *i.Client
	c.Jar, _ = cookiejar.New(nil) // New(nil) non fallisce
	i.Client = &c
	return i
}

func eseguiScenariCon(ss []scenario, devs deviazioni, standalone0, gox0 istanza, cred credenziali) rapporto {
	rap := rapporto{Verdi: map[string]bool{}}
	for _, s := range ss {
		rap.Scenari++
		prima := rap.Inattese + len(rap.Errori)
		standalone, gox := conCookie(standalone0), conCookie(gox0)
		var tokA, tokB string
		for n, p := range s.Passi {
			a, errA := chiediCon(standalone, p, tokA, cred)
			b, errB := chiediCon(gox, p, tokB, cred)
			if m := reTokenPagina.FindStringSubmatch(a.Corpo); m != nil {
				tokA = m[1]
			}
			if m := reTokenPagina.FindStringSubmatch(b.Corpo); m != nil {
				tokB = m[1]
			}
			if errA != nil || errB != nil {
				rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s %s: standalone %v, go %v", s.Nome, n, p.Metodo, p.Percorso, errA, errB))
				continue
			}
			rap.Passi++
			for _, d := range confronta(p, a, b) {
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
