package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

type istanza struct {
	Nome   string
	Base   string
	Client *http.Client
}

type rapporto struct {
	Scenari, Passi   int
	Attese, Inattese int
	// DaDecidere sono le differenze del livello browser che aspettano una decisione (browser.go):
	// non fanno rosso il banco, ma lo scenario non è verde per la copertura.
	DaDecidere int
	Righe      []string
	Errori     []string
	Verdi      map[string]bool // scenario → nessuna differenza inattesa e nessun errore
	// NonProvati sono gli scenari che con questi dati non provano niente (prova_con): non hanno un
	// esito in Verdi, ma le loro differenze contano lo stesso.
	NonProvati []string
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
	if p.CorpoBase64 != "" {
		b, err := base64.StdEncoding.Strict().DecodeString(p.CorpoBase64)
		if err != nil {
			return risposta{}, err
		}
		corpo = bytes.NewReader(b)
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
		// {csrf} in un'intestazione (x-csrf-token della coda delle foto, slice 039) è il token così
		// com'è, non codificato per un modulo.
		if strings.Contains(v, "{csrf}") && token == "" {
			return risposta{}, fmt.Errorf("{csrf} senza una pagina che dia il token")
		}
		req.Header.Set(k, strings.NewReplacer("{ip_cliente}", cred.IPCliente, "{csrf}", token).Replace(v))
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
	var errore error
	p.Corpo = reCampoNascosto.ReplaceAllStringFunc(p.Corpo, func(x string) string {
		nome := reCampoNascosto.FindStringSubmatch(x)[1]
		v, ok := campoNascosto(prima.Corpo, nome)
		if !ok && errore == nil {
			errore = fmt.Errorf("%s senza il campo nascosto nella pagina di prima", x)
		}
		return url.QueryEscape(v)
	})
	if errore != nil {
		return risposta{}, errore
	}
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

// istanzeTelegram sono il Go con il canale Telegram acceso e il bot finto (slice 074b, gotelegram.go):
// vuote se il banco non li ha.
type istanzeTelegram struct{ Go, Bot istanza }

func (t istanzeTelegram) presenti() bool { return t.Go.Base != "" && t.Bot.Base != "" }

// intervalloAttendi è quanto aspetta un passo con attendi fra una richiesta e l'altra.
var intervalloAttendi = 500 * time.Millisecond

// reCampoNascosto è {campo:<nome>} nel corpo di un passo solo_go: il valore del campo nascosto con quel
// nome nella risposta di prima del Go (il gettone del popup Telegram, slice 074b).
var reCampoNascosto = regexp.MustCompile(`\{campo:([a-z_][a-z0-9_]*)\}`)

// campoNascosto è il valore del primo <input name="nome" type="hidden" value="…"> del corpo, come lo
// scrivono le pagine del Go, con le entità decodificate.
func campoNascosto(corpo, nome string) (string, bool) {
	m := regexp.MustCompile(`<input name="` + regexp.QuoteMeta(nome) + `" type="hidden" value="([^"]*)">`).FindStringSubmatch(corpo)
	if m == nil {
		return "", false
	}
	return html.UnescapeString(m[1]), true
}

// tokenStart è {start}: il parametro start del redirect a t.me della risposta di prima del Go («Attiva su
// Telegram»), con la forma che il bot finto accetta.
func tokenStart(prima risposta) (string, error) {
	loc := strings.Join(http.Header(prima.Intestazioni).Values("Location"), ", ")
	u, err := url.Parse(loc)
	if err != nil || u.Scheme != "https" || u.Host != "t.me" {
		return "", fmt.Errorf("{start} senza un redirect a https://t.me/…: %q", loc)
	}
	t := u.Query().Get("start")
	if !reTokenStart.MatchString(t) {
		return "", fmt.Errorf("{start}: token del redirect non valido")
	}
	return t, nil
}

// passoBot esegue un passo «bot» sul bot finto, con {start} dalla risposta di prima del Go; con attendi
// rifà la richiesta finché le attese tornano o finisce il tempo, e dà l'ultima risposta.
func passoBot(bot istanza, p passo, prima risposta, dati string) (risposta, error) {
	if strings.Contains(p.Corpo, "{start}") {
		t, err := tokenStart(prima)
		if err != nil {
			return risposta{}, err
		}
		p.Corpo = strings.ReplaceAll(p.Corpo, "{start}", t)
	}
	fine := time.Now().Add(time.Duration(p.Attendi) * time.Second)
	for {
		r, err := chiedi(bot, p)
		if err != nil || p.Attendi == 0 || len(differenzeSoloGo(p, dati, r)) == 0 || !time.Now().Before(fine) {
			return r, err
		}
		time.Sleep(intervalloAttendi)
	}
}

// differenzeSoloGo sono le attese che una risposta di un passo di una sola istanza non rispetta (campo,
// valore atteso, valore dato), in ordine di campo.
func differenzeSoloGo(p passo, dati string, r risposta) [][3]string {
	var out [][3]string
	for c, v := range attesePer(p, dati) {
		if vr := r.valore(c); vr != v {
			out = append(out, [3]string{c, v, vr})
		}
	}
	slices.SortFunc(out, func(a, b [3]string) int { return strings.Compare(a[0], b[0]) })
	return out
}

// eseguiScenariCon è eseguiScenariTutti senza il Go con Telegram.
func eseguiScenariCon(ss []scenario, devs deviazioni, standalone0, gox0 istanza, cred credenziali, dati string) rapporto {
	return eseguiScenariTutti(ss, devs, standalone0, gox0, istanzeTelegram{}, cred, dati)
}

// eseguiScenariTutti è eseguiScenari con le credenziali sintetiche, la modalità dei dati da cui è
// partito il banco ("" = account seminati, oppure datiSinteticiImport), che sceglie le attese, e il Go con
// Telegram con il bot finto per gli scenari con "istanza": "go-telegram" (slice 074b): quelli girano solo
// lì, solo con i dati che li provano (altrimenti non provati, senza richieste) e sono un errore se il
// banco non ha le due istanze.
func eseguiScenariTutti(ss []scenario, devs deviazioni, standalone0, gox0 istanza, tg istanzeTelegram, cred credenziali, dati string) rapporto {
	rap := rapporto{Verdi: map[string]bool{}}
	devs = devs.delLivello("")
	// Una modalità sconosciuta non ripiega sulle attese di base: nessuna richiesta, banco rosso.
	if _, err := modalitaDati(dati); err != nil {
		rap.Errori = append(rap.Errori, err.Error())
		return rap
	}
	for _, s := range ss {
		rap.Scenari++
		prima := rap.Inattese + len(rap.Errori)
		standalone, gox := conCookie(standalone0), conCookie(gox0)
		if s.Istanza == istanzaGoTelegram {
			if !provato(s.ProvaCon, dati) {
				rap.NonProvati = append(rap.NonProvati, s.Nome)
				continue
			}
			if !tg.presenti() {
				rap.Errori = append(rap.Errori, fmt.Sprintf("%s: manca l'istanza go-telegram (Go con Telegram e bot finto, banco su -dati %s)", s.Nome, datiSinteticiImport))
				continue
			}
			gox = conCookie(tg.Go)
		}
		cred := cred
		cred.IPCliente = ipCasuale()
		var tokA, tokB string
		var primaB risposta
		for n, p := range s.Passi {
			if p.Bot {
				// Il passo del bot non è una pagina del Go: token e risposta di prima restano quelli del Go.
				b, err := passoBot(tg.Bot, p, primaB, dati)
				if err != nil {
					rap.Errori = append(rap.Errori, fmt.Sprintf("%s[%d] %s %s (bot, %s): %v", s.Nome, n, p.Metodo, p.Percorso, p.SoloGo, err))
					continue
				}
				rap.Passi++
				for _, d := range differenzeSoloGo(p, dati, b) {
					rap.Inattese++
					rap.Righe = append(rap.Righe, "INATTESA "+differenza{Scenario: s.Nome, Passo: n,
						Campo: "atteso " + d[0] + "=" + d[1] + " (bot, " + p.SoloGo + ")", Standalone: "(non eseguito)", Go: d[2]}.String())
				}
				continue
			}
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
				if d.Normalizzata != "" {
					rap.Attese++
					rap.Righe = append(rap.Righe, "ATTESA ("+d.Normalizzata+", normalizzata) "+d.String())
				} else if id, ok := devs.copre(d); ok {
					rap.Attese++
					rap.Righe = append(rap.Righe, "ATTESA ("+id+") "+d.String())
				} else {
					rap.Inattese++
					rap.Righe = append(rap.Righe, "INATTESA "+d.String())
				}
			}
		}
		if provato(s.ProvaCon, dati) {
			rap.Verdi[s.Nome] = rap.Inattese+len(rap.Errori) == prima
		} else {
			rap.NonProvati = append(rap.NonProvati, s.Nome)
		}
	}
	return rap
}
