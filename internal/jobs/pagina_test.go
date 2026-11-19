package jobs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/audit"
	"github.com/L0cutus/gestrip/internal/buste"
	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
)

// La pagina «Promemoria e job» dal lato dei lavori (slice 098, JOB-05, JOB-06): i valori del modulo come
// Config.get/0 e Config.change/1 dello standalone (go-baseline, jobs/config.ex), il salvataggio come Config.put/2
// (cast, validazioni, solo le chiavi cambiate, jobs.configured con i soli nomi), i giorni di apertura solo Go
// (D-102) e lo stato dei tre lavori con la resa della D-16. Database vero, dati inventati.

var titolarePagina = audit.Contesto{Attore: "titolare@esempio.test"}

func nuovoPianificatoreCompleto(t *testing.T) (*db.DB, *clock.Finto, *Pianificatore) {
	t.Helper()
	d, o := apriDB(t)
	coda := canaliVeri(d, o)
	return d, o, Nuovo(d, o, coda, map[string]Lavoro{Ritardi: NuoviRitardi(d, coda), Solleciti: NuoviSolleciti(d, coda),
		Briefing: NuovoBriefing(d, coda)})
}

func testo(s string) *string { return &s }

// modulo è un modulo con i campi dati (a coppie).
func modulo(kv ...string) ModuloConfig {
	m := ModuloConfig{Campi: map[string]*string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Campi[kv[i]] = testo(kv[i+1])
	}
	return m
}

func valoreImpostazione(t *testing.T, d *db.DB, chiave string) (string, bool) {
	t.Helper()
	v, ok, err := impostazione(context.Background(), d.Lettura, chiave)
	if err != nil {
		t.Fatal(err)
	}
	return v, ok
}

// configurati sono i dati delle righe jobs.configured, in ordine.
func configurati(t *testing.T, d *db.DB) []string {
	t.Helper()
	rs, err := d.Lettura.Query(`SELECT attore || ' ' || risorsa || ' ' || id_risorsa || ' ' || esito || ' ' || dati FROM eventi_audit
		WHERE azione = 'jobs.configured' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []string
	for rs.Next() {
		var s string
		if err := rs.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func giorniISO(g ...int) [8]bool {
	var a [8]bool
	for _, n := range g {
		a[n] = true
	}
	return a
}

// inventario: JOB-05
// I valori del modulo senza nessuna riga: i predefiniti dello standalone (@defaults) e martedì–sabato (D-102).
func TestValoriPredefiniti(t *testing.T) {
	d, _ := apriDB(t)
	v, err := LeggiValori(context.Background(), d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	voluti := map[string]string{"overdue_enabled": "true", "overdue_hour": "09:00", "overdue_days": "1", "chases_enabled": "true",
		"chases_hour": "10:00", "chases_days": "14", "briefing_enabled": "false", "briefing_hour": "08:00", "briefing_days": "14"}
	if !reflect.DeepEqual(v.Campi, voluti) {
		t.Errorf("valori = %v", v.Campi)
	}
	if v.Giorni != giorniISO(2, 3, 4, 5, 6) {
		t.Errorf("giorni = %v", v.Giorni)
	}
}

// inventario: JOB-05
// Le letture dei valori salvati, come Config.get/0: l'ora com'è scritta (text/1: vuota → predefinita, una illeggibile
// si mostra com'è), le soglie lette (days/1; i solleciti con threshold_days, max(1, n)), «spento/acceso» con le parole
// dello standalone; i giorni di apertura (illeggibili → martedì–sabato).
func TestValoriSalvati(t *testing.T) {
	d, _ := apriDB(t)
	for k, v := range map[string]string{"jobs.overdue_enabled": " no ", "jobs.overdue_hour": "alle nove", "jobs.overdue_days": "0",
		"jobs.chases_enabled": "forse", "jobs.chases_hour": "   ", buste.ChiaveSollecitiGiorni: "0", "jobs.briefing_enabled": "yes",
		"jobs.briefing_hour": "07:30:00", ChiaveGiorniBriefing: "0", ChiaveGiorniApertura: "7,1"} {
		imposta(t, d, k, v)
	}
	v, err := LeggiValori(context.Background(), d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	voluti := map[string]string{"overdue_enabled": "false", "overdue_hour": "alle nove", "overdue_days": "1", "chases_enabled": "true",
		"chases_hour": "10:00", "chases_days": "1", "briefing_enabled": "true", "briefing_hour": "07:30:00", "briefing_days": "0"}
	if !reflect.DeepEqual(v.Campi, voluti) {
		t.Errorf("valori = %v", v.Campi)
	}
	if v.Giorni != giorniISO(1, 7) {
		t.Errorf("giorni = %v", v.Giorni)
	}
	imposta(t, d, ChiaveGiorniApertura, "lunedì")
	imposta(t, d, buste.ChiaveSollecitiGiorni, "21")
	imposta(t, d, "jobs.overdue_days", "400")
	imposta(t, d, ChiaveGiorniBriefing, "abc")
	if v, _ = LeggiValori(context.Background(), d.Lettura); v.Giorni != giorniISO(2, 3, 4, 5, 6) || v.Campi["chases_days"] != "21" ||
		v.Campi["overdue_days"] != "1" || v.Campi["briefing_days"] != "14" {
		t.Errorf("illeggibili: %v %v", v.Campi, v.Giorni)
	}
}

// inventario: JOB-05
// Un salvataggio riuscito scrive solo le chiavi cambiate, con i valori di to_setting/1 (vero «1», falso «0», l'ora
// com'è scritta, la soglia come intero), le soglie nelle chiavi delle loro pagine (solleciti_artigiano_giorni,
// briefing_alert_fuori_bottega_giorni), e una riga jobs.configured con i soli nomi dei campi cambiati, anche quando
// non cambia niente. La lettura dopo dice i valori nuovi.
func TestSalvaConfig(t *testing.T) {
	d, _, p := nuovoPianificatoreCompleto(t)
	r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "07:45", "chases_days", "21", "overdue_days", "+3",
		"briefing_enabled", "true", "briefing_days", "0", "chases_enabled", "true"), titolarePagina)
	if err != nil || r != nil {
		t.Fatalf("salva: %v %v", r, err)
	}
	for k, voluto := range map[string]string{"jobs.overdue_hour": "07:45", buste.ChiaveSollecitiGiorni: "21", "jobs.overdue_days": "3",
		"jobs.briefing_enabled": "1", ChiaveGiorniBriefing: "0"} {
		if v, ok := valoreImpostazione(t, d, k); !ok || v != voluto {
			t.Errorf("%s = %q %v, voluto %q", k, v, ok, voluto)
		}
	}
	for _, k := range []string{"jobs.chases_enabled", "jobs.overdue_enabled", "jobs.chases_hour", "jobs.briefing_hour", ChiaveGiorniApertura} {
		if v, ok := valoreImpostazione(t, d, k); ok {
			t.Errorf("%s scritta (%q) senza essere cambiata", k, v)
		}
	}
	voluto := []string{`titolare@esempio.test jobs config ok {"changed":["overdue_hour","overdue_days","chases_days","briefing_enabled","briefing_days"]}`}
	if got := configurati(t, d); !reflect.DeepEqual(got, voluto) {
		t.Errorf("audit = %q", got)
	}
	conf, err := LeggiConfig(context.Background(), d.Lettura)
	if err != nil {
		t.Fatal(err)
	}
	if !conf.Attivo(Briefing) || conf.GiorniBriefing() != 0 || conf.GiorniRitardi() != 3 || conf.Ora(Ritardi).String() != "7h45m0s" {
		t.Errorf("config letta: %+v", conf)
	}
	// Spegnere scrive «0»; niente cambiato: nessuna riga toccata, ma la riga d'audit con la lista vuota.
	if r, err := p.SalvaConfig(context.Background(), modulo("overdue_enabled", "false", "briefing_enabled", "0"), titolarePagina); err != nil || r != nil {
		t.Fatalf("spegni: %v %v", r, err)
	}
	if v, _ := valoreImpostazione(t, d, "jobs.overdue_enabled"); v != "0" {
		t.Errorf("spento = %q", v)
	}
	if v, _ := valoreImpostazione(t, d, "jobs.briefing_enabled"); v != "0" {
		t.Errorf("briefing spento = %q", v)
	}
	if r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "07:45", "chases_days", "21"), titolarePagina); err != nil || r != nil {
		t.Fatalf("uguale: %v %v", r, err)
	}
	got := configurati(t, d)
	if len(got) != 3 || got[1] != `titolare@esempio.test jobs config ok {"changed":["overdue_enabled","briefing_enabled"]}` ||
		got[2] != `titolare@esempio.test jobs config ok {"changed":[]}` {
		t.Errorf("audit = %q", got)
	}
	// Le forme che il cast porta allo stesso valore di adesso non sono cambiamenti: «1» è vero, «+21» è 21.
	if r, err := p.SalvaConfig(context.Background(), modulo("chases_enabled", "1", "chases_days", "+21", "overdue_days", "03"), titolarePagina); err != nil || r != nil {
		t.Fatalf("stesse forme: %v %v", r, err)
	}
	if got := configurati(t, d); got[len(got)-1] != `titolare@esempio.test jobs config ok {"changed":[]}` {
		t.Errorf("stesse forme cambiate: %q", got[len(got)-1])
	}
	// Il limite della soglia dei ritardi è compreso.
	if r, err := p.SalvaConfig(context.Background(), modulo("overdue_days", "365"), titolarePagina); err != nil || r != nil {
		t.Fatalf("365: %v %v", r, err)
	}
	if v, _ := valoreImpostazione(t, d, "jobs.overdue_days"); v != "365" {
		t.Errorf("365 salvato %q", v)
	}
	// «09:00:00» è un'ora diversa da «09:00» come testo: si scrive.
	if r, err := p.SalvaConfig(context.Background(), modulo("chases_hour", "10:00:00"), titolarePagina); err != nil || r != nil {
		t.Fatalf("secondi: %v %v", r, err)
	}
	if v, _ := valoreImpostazione(t, d, "jobs.chases_hour"); v != "10:00:00" {
		t.Errorf("ora con i secondi = %q", v)
	}
}

// inventario: JOB-05
// I rifiuti del changeset: campo vuoto o di soli spazi («Campo obbligatorio.»), un valore che non si converte
// («Valore non valido.», anche un campo che non è un testo), l'ora («Orario non valido: usare HH:MM.»), le tre soglie
// con i loro messaggi (i solleciti senza massimo). Un rifiuto non scrive niente, nemmeno l'audit, e ridà i valori
// mandati.
func TestSalvaConfigRifiuti(t *testing.T) {
	casi := []struct {
		campo, valore, errore string
	}{
		{"overdue_hour", "alle nove", MsgOraNonValida}, {"overdue_hour", "", MsgObbligatorio}, {"chases_hour", "  ", MsgObbligatorio},
		{"briefing_hour", "24:00", MsgOraNonValida}, {"overdue_hour", "07:45:00.5", MsgOraNonValida}, {"overdue_hour", "9:00", MsgOraNonValida}, {"overdue_hour", " 09:00", MsgOraNonValida},
		{"overdue_days", "366", MsgSogliaRitardi}, {"overdue_days", "0", MsgSogliaRitardi}, {"overdue_days", "abc", MsgNonValido},
		{"overdue_days", " 3", MsgNonValido}, {"overdue_days", "3.5", MsgNonValido}, {"chases_days", "0", MsgSogliaSolleciti},
		{"chases_days", "-2", MsgSogliaSolleciti}, {"briefing_days", "-1", MsgSogliaBriefing}, {"briefing_days", "366", MsgSogliaBriefing},
		{"overdue_enabled", "forse", MsgNonValido}, {"overdue_enabled", "True", MsgNonValido}, {"briefing_enabled", "on", MsgNonValido}, {"overdue_days", "", MsgObbligatorio},
		{"chases_enabled", "", MsgObbligatorio},
	}
	for _, c := range casi {
		d, _, p := nuovoPianificatoreCompleto(t)
		r, err := p.SalvaConfig(context.Background(), modulo(c.campo, c.valore), titolarePagina)
		if err != nil || r == nil {
			t.Errorf("%s=%q: %v %v", c.campo, c.valore, r, err)
			continue
		}
		if !reflect.DeepEqual(r.Errori, map[string][]string{c.campo: {c.errore}}) {
			t.Errorf("%s=%q: errori %v", c.campo, c.valore, r.Errori)
		}
		if r.Campi[c.campo] != c.valore {
			t.Errorf("%s=%q: rimostrato %q", c.campo, c.valore, r.Campi[c.campo])
		}
		if n := contaImpostazioni(t, d); n != 0 {
			t.Errorf("%s=%q: %d impostazioni scritte", c.campo, c.valore, n)
		}
		if got := configurati(t, d); len(got) != 0 {
			t.Errorf("%s=%q: audit %q", c.campo, c.valore, got)
		}
	}
	// Un campo che non è un testo (una lista o una mappa nel modulo) non si converte.
	d, _, p := nuovoPianificatoreCompleto(t)
	r, err := p.SalvaConfig(context.Background(), ModuloConfig{Campi: map[string]*string{"overdue_days": nil}}, titolarePagina)
	if err != nil || r == nil || !reflect.DeepEqual(r.Errori, map[string][]string{"overdue_days": {MsgNonValido}}) {
		t.Errorf("non testo: %v %v", r, err)
	}
	// I solleciti non hanno un massimo (la riga è del Flask): un numero enorme si salva com'è.
	if r, err := p.SalvaConfig(context.Background(), modulo("chases_days", "99999999999999999999999"), titolarePagina); err != nil || r != nil {
		t.Fatalf("soglia enorme: %v %v", r, err)
	}
	if v, _ := valoreImpostazione(t, d, buste.ChiaveSollecitiGiorni); v != "99999999999999999999999" {
		t.Errorf("soglia enorme salvata %q", v)
	}
	// Due errori insieme, e i valori dei campi non mandati sono quelli salvati.
	r, err = p.SalvaConfig(context.Background(), modulo("overdue_hour", "x", "briefing_days", "-5"), titolarePagina)
	if err != nil || r == nil || len(r.Errori) != 2 || r.Campi["chases_hour"] != "10:00" || r.Campi["briefing_days"] != "-5" {
		t.Errorf("due errori: %+v %v", r, err)
	}
}

func contaImpostazioni(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(`SELECT count(*) FROM impostazioni WHERE chiave LIKE 'jobs.%' OR chiave IN (?, ?)`,
		buste.ChiaveSollecitiGiorni, ChiaveGiorniBriefing).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// inventario: JOB-05
// Un'ora salvata illeggibile ferma ogni salvataggio con il suo errore (validate_hour guarda anche il valore salvato),
// come lo standalone: si corregge riscrivendo quell'ora.
func TestSalvaConfigOraSalvataIllegibile(t *testing.T) {
	d, _, p := nuovoPianificatoreCompleto(t)
	imposta(t, d, "jobs.chases_hour", "alle dieci")
	r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "07:00"), titolarePagina)
	if err != nil || r == nil || !reflect.DeepEqual(r.Errori, map[string][]string{"chases_hour": {MsgOraNonValida}}) {
		t.Fatalf("ora salvata illeggibile: %+v %v", r, err)
	}
	if r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "07:00", "chases_hour", "10:30"), titolarePagina); err != nil || r != nil {
		t.Fatalf("corretta: %v %v", r, err)
	}
}

// inventario: JOB-05, JOB-02
// I giorni di apertura (D-102, solo Go): sette caselle, giorni ISO separati da virgole in ordine; senza nessun giorno
// il modulo è rifiutato; un valore che non è un giorno non è valido; il campo assente lascia i giorni come sono;
// gli stessi giorni di prima non scrivono la riga. Il nome nell'audit è giorni_apertura.
func TestSalvaGiorniApertura(t *testing.T) {
	d, _, p := nuovoPianificatoreCompleto(t)
	m := modulo()
	m.ConGiorni, m.Giorni = true, []string{""}
	r, err := p.SalvaConfig(context.Background(), m, titolarePagina)
	if err != nil || r == nil || !reflect.DeepEqual(r.Errori, map[string][]string{CampoGiorniApertura: {MsgNessunGiorno}}) {
		t.Fatalf("nessun giorno: %+v %v", r, err)
	}
	if r.Giorni != giorniISO() {
		t.Errorf("nessun giorno rimostrato: %v", r.Giorni)
	}
	for _, sbagliato := range []string{"8", "0", "03", "+3", " 3", "lun"} {
		m.Giorni = []string{"", "3", sbagliato}
		if r, err := p.SalvaConfig(context.Background(), m, titolarePagina); err != nil || r == nil ||
			!reflect.DeepEqual(r.Errori, map[string][]string{CampoGiorniApertura: {MsgNonValido}}) {
			t.Fatalf("giorno %q: %+v %v", sbagliato, r, err)
		}
	}
	if contaImpostazioni(t, d) != 0 || len(configurati(t, d)) != 0 {
		t.Fatal("rifiuto scritto")
	}
	m.Giorni = []string{"", "2", "3", "4", "5", "6"} // i predefiniti: niente da scrivere
	if r, err := p.SalvaConfig(context.Background(), m, titolarePagina); err != nil || r != nil {
		t.Fatalf("predefiniti: %v %v", r, err)
	}
	if _, ok := valoreImpostazione(t, d, ChiaveGiorniApertura); ok {
		t.Error("giorni uguali scritti")
	}
	m.Giorni = []string{"", "7", "1", "1"}
	if r, err := p.SalvaConfig(context.Background(), m, titolarePagina); err != nil || r != nil {
		t.Fatalf("lunedì e domenica: %v %v", r, err)
	}
	if v, _ := valoreImpostazione(t, d, ChiaveGiorniApertura); v != "1,7" {
		t.Errorf("giorni = %q", v)
	}
	conf, _ := LeggiConfig(context.Background(), d.Lettura)
	if !conf.Aperto(1) || !conf.Aperto(0) || conf.Aperto(2) {
		t.Error("config dei giorni non riletta")
	}
	if got := configurati(t, d); len(got) != 2 || !strings.HasSuffix(got[1], `{"changed":["giorni_apertura"]}`) {
		t.Errorf("audit = %q", got)
	}
	// Il campo assente lascia i giorni salvati.
	if r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "08:00"), titolarePagina); err != nil || r != nil {
		t.Fatal(r, err)
	}
	if v, _ := valoreImpostazione(t, d, ChiaveGiorniApertura); v != "1,7" {
		t.Errorf("giorni dopo un modulo senza il campo = %q", v)
	}
	// Un errore di un altro campo ridà i giorni mandati.
	m = modulo("overdue_hour", "x")
	m.ConGiorni, m.Giorni = true, []string{"", "4"}
	if r, err := p.SalvaConfig(context.Background(), m, titolarePagina); err != nil || r == nil || r.Giorni != giorniISO(4) {
		t.Errorf("giorni rimostrati: %+v %v", r, err)
	}
}

// inventario: JOB-05, JOB-06, JOB-04
// Lo stato della pagina (resa della D-16): senza canali nessun lavoro si può eseguire; con la sola email ritardi e
// solleciti sì e il briefing no (solo Telegram); con Telegram tutti e tre. L'ultima esecuzione di ogni lavoro, lo
// storico delle ultime 20 righe (anche quelle importate) e le etichette dello standalone.
func TestPaginaStato(t *testing.T) {
	d, o, p := nuovoPianificatoreCompleto(t)
	pg, err := p.Pagina(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pg.ConCanali || len(pg.Lavori) != 3 || len(pg.Storico) != 0 || pg.Campi["overdue_hour"] != "09:00" {
		t.Fatalf("senza canali: %+v", pg)
	}
	for i, n := range Nomi {
		if l := pg.Lavori[i]; l.Nome != n || l.Raggiungibile || l.Ultima != nil || l.Etichetta != Etichetta(n) {
			t.Errorf("lavoro %d: %+v", i, l)
		}
	}
	accendiEmail(t, d)
	if pg, _ = p.Pagina(context.Background()); !pg.ConCanali || !pg.Lavori[0].Raggiungibile || !pg.Lavori[1].Raggiungibile || pg.Lavori[2].Raggiungibile {
		t.Errorf("sola email: %+v", pg.Lavori)
	}
	accendiTelegram(t, d)
	if pg, _ = p.Pagina(context.Background()); !pg.Lavori[2].Raggiungibile {
		t.Errorf("con Telegram: %+v", pg.Lavori)
	}
	// Righe importate (anche «Saltato») e un'esecuzione vera: 22 righe, la pagina ne mostra 20, la più recente prima.
	for i := 0; i < 21; i++ {
		if _, err := d.Scrittura.Exec(`INSERT INTO esecuzioni_lavori (lavoro, avvio, esito, eseguito_il, giorno, avvisi, messaggi, dettaglio)
			VALUES ('chases', 'scheduled', 'skipped', '2026-11-10T09:00:00.000000Z', '2026-11-10', 0, 0, 'Nessun canale di invio configurato.')`); err != nil {
			t.Fatal(err)
		}
	}
	o.Imposta(martedi.Add(time.Hour))
	e, err := p.EseguiAMano(context.Background(), Briefing, titolarePagina)
	if err != nil || e.Esito != EsitoSaltato || e.Dettaglio != DettaglioSpento {
		t.Fatalf("briefing spento a mano: %+v %v", e, err)
	}
	pg, _ = p.Pagina(context.Background())
	if len(pg.Storico) != 20 || pg.Storico[0].ID != e.ID || pg.Storico[19].Lavoro != Solleciti {
		t.Errorf("storico: %d righe, prima %+v", len(pg.Storico), pg.Storico[0])
	}
	if pg.Lavori[0].Ultima != nil || pg.Lavori[1].Ultima == nil || pg.Lavori[1].Ultima.Esito != EsitoSaltato || pg.Lavori[2].Ultima.ID != e.ID {
		t.Errorf("ultime: %+v", pg.Lavori)
	}
	for k, v := range map[string]string{Etichetta(Ritardi): "Buste in ritardo", Etichetta(Solleciti): "Solleciti artigiano",
		Etichetta(Briefing): "Briefing del mattino", EtichettaEsito(EsitoOK): "Eseguito", EtichettaEsito(EsitoSaltato): "Saltato",
		EtichettaEsito(EsitoFallito): "Fallito", EtichettaAvvio(AvvioAutomatico): "Automatico", EtichettaAvvio(AvvioAMano): "A mano"} {
		if k != v {
			t.Errorf("etichetta %q, voluta %q", k, v)
		}
	}
}

// inventario: JOB-05, JOB-06
// Un lavoro non registrato nel pianificatore non ha «Esegui adesso» anche con i canali accesi.
func TestPaginaLavoroNonRegistrato(t *testing.T) {
	d, o := apriDB(t)
	coda := canaliVeri(d, o)
	accendiTelegram(t, d)
	pg, err := Nuovo(d, o, coda, map[string]Lavoro{Ritardi: NuoviRitardi(d, coda)}).Pagina(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !pg.ConCanali || !pg.Lavori[0].Raggiungibile || pg.Lavori[1].Raggiungibile || pg.Lavori[2].Raggiungibile {
		t.Errorf("lavori: %+v", pg.Lavori)
	}
}

// inventario: JOB-05
// Un guasto del database nel salvataggio non è un rifiuto: l'errore torna e niente resta scritto (una transazione).
func TestSalvaConfigGuasto(t *testing.T) {
	d, _, p := nuovoPianificatoreCompleto(t)
	if _, err := d.Scrittura.Exec(`CREATE TRIGGER no_audit BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'jobs.configured'
		BEGIN SELECT RAISE(ABORT, 'audit rifiutato'); END`); err != nil {
		t.Fatal(err)
	}
	r, err := p.SalvaConfig(context.Background(), modulo("overdue_hour", "07:00"), titolarePagina)
	if err == nil || r != nil {
		t.Fatalf("guasto: %v %v", r, err)
	}
	var rif *RifiutoConfig
	if errors.As(err, &rif) {
		t.Error("il guasto è un rifiuto")
	}
	if contaImpostazioni(t, d) != 0 {
		t.Error("ora scritta senza audit")
	}
}
