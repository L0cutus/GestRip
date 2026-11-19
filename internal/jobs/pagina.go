package jobs

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/internal/uscita"
)

// La pagina «Promemoria e job» dal lato dei lavori (slice 098, JOB-05, JOB-06), come Jobs.Config (get/0, change/1,
// put/2) e JobsLive dello standalone (go-baseline):
//   - i valori del modulo (LeggiValori) sono quelli di Config.get/0: attivo con le letture «spento/acceso» di on?/2,
//     l'ora com'è scritta (text/1: senza riga o vuota la predefinita), le soglie lette (days/1; i solleciti con
//     threshold_days/1 della pagina dei solleciti), più i giorni di apertura solo Go (D-102);
//   - il salvataggio (SalvaConfig) è put/2: il cast dei campi mandati sopra i valori di adesso (un campo non mandato
//     resta com'è), le validazioni del changeset con i loro testi, e in una transazione le sole chiavi cambiate più
//     jobs.configured con i soli nomi dei campi cambiati (anche nessuno); un rifiuto non scrive niente;
//   - lo stato della pagina (Pagina) dice per ogni lavoro se ha un destinatario adesso (D-16: senza, niente «Esegui
//     adesso»; il briefing solo con Telegram), l'ultima esecuzione e le ultime 20 righe dello storico.

// CampoGiorniApertura è il campo solo Go dei giorni di apertura (D-102): nel modulo jobs[giorni_apertura][], nell'audit
// il suo nome fra quelli cambiati.
const CampoGiorniApertura = "giorni_apertura"

// CampiModulo sono i campi del modulo nell'ordine di Config.keys/0 (e della pagina).
var CampiModulo = []string{"overdue_enabled", "overdue_hour", "overdue_days", "chases_enabled", "chases_hour", "chases_days",
	"briefing_enabled", "briefing_hour", "briefing_days"}

// VociPagina sono le righe dello storico che la pagina mostra (history/1, limit 20).
const VociPagina = 20

// I testi degli errori del modulo: quelli di Ecto come li traduce il catalogo it dello standalone (msgid accanto),
// quelli scritti a mano in jobs/config.ex, e quello solo Go dei giorni di apertura (D-102).
const (
	MsgObbligatorio    = "Campo obbligatorio." // can't be blank
	MsgNonValido       = "Valore non valido."  // is invalid
	MsgOraNonValida    = "Orario non valido: usare HH:MM."
	MsgSogliaRitardi   = "Soglia non valida: da 1 a 365 giorni."
	MsgSogliaSolleciti = "Soglia non valida: almeno 1 giorno."
	MsgSogliaBriefing  = "Soglia non valida: da 0 a 365 giorni (0 la disattiva)."
	MsgNessunGiorno    = "Scegli almeno un giorno di apertura."
)

// ModuloConfig è il modulo mandato: Campi per nome (un campo assente non c'è, uno che non è un testo è nil),
// Giorni i valori di jobs[giorni_apertura][] se ConGiorni (il nascosto vuoto compreso).
type ModuloConfig struct {
	Campi     map[string]*string
	Giorni    []string
	ConGiorni bool
}

// ValoriConfig sono i valori da mostrare nel modulo: i campi come testo (vero/falso «true»/«false») e i giorni di
// apertura per numero ISO (1 lunedì … 7 domenica; lo 0 non si usa).
type ValoriConfig struct {
	Campi  map[string]string
	Giorni [8]bool
}

// RifiutoConfig è il changeset rifiutato: i valori da rimostrare (quelli mandati sopra quelli salvati) e gli errori
// per campo. Niente è stato scritto.
type RifiutoConfig struct {
	ValoriConfig
	Errori map[string][]string
}

func (r *RifiutoConfig) Error() string { return "jobs: impostazioni rifiutate" }

// StatoLavoro è un lavoro nella pagina.
type StatoLavoro struct {
	Nome, Etichetta string
	// Raggiungibile dice se il lavoro ha un destinatario adesso (D-16): senza, niente «Esegui adesso».
	Raggiungibile bool
	Ultima        *Esecuzione // nil: mai eseguito
}

// Pagina è quello che mostra la pagina «Promemoria e job».
type Pagina struct {
	ValoriConfig
	Lavori  []StatoLavoro // nell'ordine di Nomi
	Storico []Esecuzione  // le ultime VociPagina, la più recente prima
	// ConCanali dice se almeno un canale è attivo (email o Telegram): senza, la riga della D-16.
	ConCanali bool
}

var (
	etichette      = map[string]string{Ritardi: "Buste in ritardo", Solleciti: "Solleciti artigiano", Briefing: "Briefing del mattino"}
	etichetteEsito = map[string]string{EsitoOK: "Eseguito", EsitoSaltato: "Saltato", EsitoFallito: "Fallito"}
	etichetteAvvio = map[string]string{AvvioAutomatico: "Automatico", AvvioAMano: "A mano"}
)

// Etichetta è il nome italiano del lavoro (label/1).
func Etichetta(nome string) string { return etichette[nome] }

// EtichettaEsito è la parola dell'esito (outcome_label/1).
func EtichettaEsito(e string) string { return etichetteEsito[e] }

// EtichettaAvvio è la parola dell'avvio (trigger_label/1).
func EtichettaAvvio(a string) string { return etichetteAvvio[a] }

// campo è la descrizione di un campo del modulo.
type campo struct {
	lavoro, genere string // genere: enabled, hour, days
}

func campoDi(nome string) campo {
	i := strings.LastIndexByte(nome, '_')
	return campo{lavoro: nome[:i], genere: nome[i+1:]}
}

// chiaveDi è setting_key/1: le soglie dei solleciti e del briefing sono le righe delle loro pagine, le altre jobs.<campo>.
func chiaveDi(nome string) string {
	switch nome {
	case "chases_days":
		return buste.ChiaveSollecitiGiorni
	case "briefing_days":
		return ChiaveGiorniBriefing
	}
	return "jobs." + nome
}

// orePredefinite sono le ore di @defaults come le scrive il modulo.
var orePredefinite = map[string]string{Ritardi: "09:00", Solleciti: "10:00", Briefing: "08:00"}

// LeggiValori è Config.get/0 come testo per il modulo, più i giorni di apertura (D-102).
func LeggiValori(ctx context.Context, q db.Interrogatore) (ValoriConfig, error) {
	conf, err := LeggiConfig(ctx, q)
	if err != nil {
		return ValoriConfig{}, err
	}
	v := ValoriConfig{Campi: map[string]string{}}
	for _, n := range Nomi {
		v.Campi[n+"_enabled"] = strconv.FormatBool(conf.Attivo(n))
		ora, ok, err := impostazione(ctx, q, chiaveDi(n+"_hour"))
		if err != nil {
			return ValoriConfig{}, err
		}
		if !ok || strings.TrimSpace(ora) == "" {
			ora = orePredefinite[n]
		}
		v.Campi[n+"_hour"] = ora
	}
	v.Campi["overdue_days"] = strconv.Itoa(conf.GiorniRitardi())
	v.Campi["briefing_days"] = strconv.Itoa(conf.GiorniBriefing())
	s, err := buste.SogliaSollecitiCon(ctx, q)
	if err != nil {
		return ValoriConfig{}, err
	}
	v.Campi["chases_days"] = strconv.FormatInt(s, 10)
	for g := 1; g <= 7; g++ {
		v.Giorni[g] = conf.Aperto(time.Weekday(g % 7))
	}
	return v, nil
}

var reIntero = regexp.MustCompile(`\A[+-]?[0-9]+\z`)

// testoVuoto è Ecto.Type.empty_trimmed_string?/1: un testo di soli spazi vale come non dato.
func testoVuoto(s string) bool { return strings.TrimFunc(s, unicode.IsSpace) == "" }

// converti è cast/3 di un campo mandato: il valore per il salvataggio (to_setting/1) o l'errore. Un campo
// vuoto è il nil di cast, che validate_required rifiuta.
func converti(c campo, v *string) (string, string) {
	if v == nil {
		return "", MsgNonValido
	}
	if testoVuoto(*v) {
		return "", MsgObbligatorio
	}
	switch c.genere {
	case "enabled":
		switch *v {
		case "true", "1":
			return "true", ""
		case "false", "0":
			return "false", ""
		}
		return "", MsgNonValido
	case "days":
		if !reIntero.MatchString(*v) {
			return "", MsgNonValido
		}
		n, _ := new(big.Int).SetString(*v, 10) // la regex garantisce un intero
		return n.String(), ""
	}
	return *v, ""
}

// erroreSoglia è validate_days/2 sul valore cambiato: i limiti e il messaggio del lavoro.
func erroreSoglia(lavoro, v string) string {
	n, _ := new(big.Int).SetString(v, 10)
	switch lavoro {
	case Ritardi:
		if n.Cmp(big.NewInt(1)) < 0 || n.Cmp(big.NewInt(MassimoGiorni)) > 0 {
			return MsgSogliaRitardi
		}
	case Solleciti:
		if n.Sign() < 1 {
			return MsgSogliaSolleciti
		}
	case Briefing:
		if n.Sign() < 0 || n.Cmp(big.NewInt(MassimoGiorni)) > 0 {
			return MsgSogliaBriefing
		}
	}
	return ""
}

// giorniDelModulo legge jobs[giorni_apertura][]: i numeri ISO spuntati (il nascosto vuoto non conta); un valore che
// non è un giorno da 1 a 7 non è valido.
func giorniDelModulo(valori []string) ([8]bool, string) {
	var g [8]bool
	nessuno := true
	for _, v := range valori {
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 7 || strconv.Itoa(n) != v {
			return g, MsgNonValido
		}
		g[n], nessuno = true, false
	}
	if nessuno {
		return g, MsgNessunGiorno
	}
	return g, ""
}

// testoGiorni scrive i giorni come li legge leggiGiorni: «1,3,7», in ordine.
func testoGiorni(g [8]bool) string {
	var parti []string
	for n := 1; n <= 7; n++ {
		if g[n] {
			parti = append(parti, strconv.Itoa(n))
		}
	}
	return strings.Join(parti, ",")
}

// SalvaConfig è Config.put/2: il cast dei campi mandati sopra i valori di adesso, le validazioni (obbligatori, l'ora
// anche se è quella salvata, le soglie cambiate), poi in una transazione le sole chiavi cambiate e jobs.configured con i
// nomi dei campi cambiati. Un rifiuto è *RifiutoConfig (niente scritto); un guasto è un errore.
func (p *Pianificatore) SalvaConfig(ctx context.Context, m ModuloConfig, a audit.Contesto) (*RifiutoConfig, error) {
	attuali, err := LeggiValori(ctx, p.d.Lettura)
	if err != nil {
		return nil, err
	}
	mostra := ValoriConfig{Campi: map[string]string{}, Giorni: attuali.Giorni}
	nuovi := map[string]string{}
	errori := map[string][]string{}
	for _, nome := range CampiModulo {
		c := campoDi(nome)
		mostra.Campi[nome], nuovi[nome] = attuali.Campi[nome], attuali.Campi[nome]
		v, mandato := m.Campi[nome]
		if mandato {
			if v != nil {
				mostra.Campi[nome] = *v
			} else {
				mostra.Campi[nome] = ""
			}
			convertito, e := converti(c, v)
			if e != "" {
				errori[nome] = append(errori[nome], e)
				continue
			}
			if c.genere == "days" && convertito != attuali.Campi[nome] {
				if e := erroreSoglia(c.lavoro, convertito); e != "" {
					errori[nome] = append(errori[nome], e)
				}
			}
			nuovi[nome] = convertito
		}
		// validate_hour guarda il valore del changeset: quello mandato o, senza, quello salvato.
		if c.genere == "hour" {
			if _, letta := leggiOra(nuovi[nome]); !letta && len(errori[nome]) == 0 {
				errori[nome] = append(errori[nome], MsgOraNonValida)
			}
		}
	}
	giorni := attuali.Giorni
	if m.ConGiorni {
		g, e := giorniDelModulo(m.Giorni)
		mostra.Giorni = g
		if e != "" {
			errori[CampoGiorniApertura] = []string{e}
		}
		giorni = g
	}
	if len(errori) > 0 {
		return &RifiutoConfig{ValoriConfig: mostra, Errori: errori}, nil
	}
	cambiati := []string{}
	for _, nome := range CampiModulo {
		if nuovi[nome] != attuali.Campi[nome] {
			cambiati = append(cambiati, nome)
		}
	}
	if giorni != attuali.Giorni {
		cambiati = append(cambiati, CampoGiorniApertura)
	}
	return nil, p.scriviConfig(ctx, cambiati, nuovi, giorni, a)
}

// scriviConfig è write/3: le chiavi cambiate (to_setting/1) e jobs.configured, tutto o niente.
func (p *Pianificatore) scriviConfig(ctx context.Context, cambiati []string, nuovi map[string]string, giorni [8]bool, a audit.Contesto) error {
	tx, err := p.d.Scrittura.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // dopo il Commit non fa niente
	adesso := p.o.Adesso().UTC().Format(db.FormatoIstante)
	for _, nome := range cambiati {
		chiave, valore := ChiaveGiorniApertura, testoGiorni(giorni)
		if nome != CampoGiorniApertura {
			chiave, valore = chiaveDi(nome), nuovi[nome]
			switch valore {
			case "true":
				valore = "1"
			case "false":
				valore = "0"
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)
			ON CONFLICT (chiave) DO UPDATE SET valore = excluded.valore, aggiornata_il = excluded.aggiornata_il`, chiave, valore, adesso); err != nil {
			return fmt.Errorf("jobs: impostazione %s: %w", nome, err)
		}
	}
	if err := p.reg.Scrivi(ctx, tx, a, "jobs.configured", "jobs", "config", "ok", map[string]any{"changed": cambiati}); err != nil {
		return err
	}
	return tx.Commit()
}

// Pagina è lo stato della pagina: i valori del modulo, per ogni lavoro se ha un destinatario adesso e l'ultima
// esecuzione, lo storico delle ultime VociPagina righe.
func (p *Pianificatore) Pagina(ctx context.Context) (Pagina, error) {
	v, err := LeggiValori(ctx, p.d.Lettura)
	if err != nil {
		return Pagina{}, err
	}
	ultime, err := p.Ultime(ctx)
	if err != nil {
		return Pagina{}, err
	}
	storico, err := p.Storico(ctx, VociPagina)
	if err != nil {
		return Pagina{}, err
	}
	pg := Pagina{ValoriConfig: v, Storico: storico}
	for _, n := range Nomi {
		s := StatoLavoro{Nome: n, Etichetta: Etichetta(n)}
		if _, ok := p.lavori[n]; ok {
			dest, err := p.Destinazioni(ctx, n)
			if err != nil {
				return Pagina{}, err
			}
			s.Raggiungibile = len(dest) > 0
		}
		if e, ok := ultime[n]; ok {
			s.Ultima = &e
		}
		pg.Lavori = append(pg.Lavori, s)
	}
	// La riga della D-16 dice dei canali, non dei lavori: con un canale attivo non c'è.
	for _, c := range []string{uscita.CanaleEmail, uscita.CanaleTelegram} {
		attivo, err := p.canali.CanaleAttivo(ctx, c)
		if err != nil {
			return Pagina{}, err
		}
		pg.ConCanali = pg.ConCanali || attivo
	}
	return pg, nil
}
