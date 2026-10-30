package main

import (
	"fmt"
	"io"
	"net/http"
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

// chiedi esegue un passo su un'istanza, senza seguire i redirect (si confronta il 30x).
func chiedi(i istanza, p passo) (risposta, error) {
	// Il percorso si attacca alla base: anche «//altro/x» resta un percorso sull'istanza (test).
	req, err := http.NewRequest(p.Metodo, strings.TrimRight(i.Base, "/")+p.Percorso, nil)
	if err != nil {
		return risposta{}, err
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
	corpo, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return risposta{}, err
	}
	return risposta{Codice: res.StatusCode, Corpo: string(corpo), Intestazioni: res.Header}, nil
}

// eseguiScenari fa girare ogni passo su standalone e Go e classifica le differenze.
func eseguiScenari(ss []scenario, devs deviazioni, standalone, gox istanza) rapporto {
	rap := rapporto{Verdi: map[string]bool{}}
	for _, s := range ss {
		rap.Scenari++
		prima := rap.Inattese + len(rap.Errori)
		for n, p := range s.Passi {
			a, errA := chiedi(standalone, p)
			b, errB := chiedi(gox, p)
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
