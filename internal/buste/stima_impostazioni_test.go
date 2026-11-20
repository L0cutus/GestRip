package buste

import (
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/db"
)

// Le impostazioni della stima dei tempi (ADM-04, RIP-H-55 parte negozio), come Settings.Estimate dello standalone
// (go-baseline, settings/estimate.ex): l'interruttore e il supplemento dell'artigiano. Spenta finché il titolare non la
// accende; i giorni riportati in 0-90 e mai rifiutati; il massimo mai sotto il minimo; un solo audit con i nomi delle
// chiavi cambiate e nessun valore. Il modello (campione, percentili) è quello della pagina del cliente.

const (
	chiaveAttiva = "stima_tempi_attiva"
	chiaveMin    = "stima_tempi_artigiano_min"
	chiaveMax    = "stima_tempi_artigiano_max"
	azioneStima  = "settings.estimate_updated"
)

// moduloStima è il modulo estimate[<campo>]: i campi dati, come testo.
func moduloStima(kv ...string) ModuloStima {
	m := ModuloStima{}
	for i := 0; i+1 < len(kv); i += 2 {
		v := kv[i+1]
		m[kv[i]] = &v
	}
	return m
}

func salvaStima(t *testing.T, b *Buste, m ModuloStima) {
	t.Helper()
	if err := b.SalvaStima(ctx, m, chi); err != nil {
		t.Fatalf("SalvaStima: %v", err)
	}
}

func paginaStima(t *testing.T, b *Buste) PaginaStima {
	t.Helper()
	p, err := b.PaginaStima(ctx)
	if err != nil {
		t.Fatalf("PaginaStima: %v", err)
	}
	return p
}

// salvate dà le tre chiavi come sono nella tabella: «1|7|8», «∅» per una riga che non c'è.
func salvate(t *testing.T, d *db.DB) string {
	t.Helper()
	var out []string
	for _, k := range []string{chiaveAttiva, chiaveMin, chiaveMax} {
		var v string
		if err := d.Lettura.QueryRow(`SELECT valore FROM impostazioni WHERE chiave = ?`, k).Scan(&v); err != nil {
			v = "∅"
		}
		out = append(out, v)
	}
	return strings.Join(out, "|")
}

// inventario: ADM-04
// Senza niente di salvato la stima è spenta e il supplemento è 7-8 (i valori di partenza di Stefano del 16/10/2026);
// leggere non scrive: né impostazioni né audit.
func TestStimaImpostazioniPartenza(t *testing.T) {
	b, d, _ := prova(t)
	p := paginaStima(t, b)
	if p.Attiva || p.Min != 7 || p.Max != 8 {
		t.Fatalf("partenza %+v", p)
	}
	if p.Oggi != nil || p.Bottega != 0 || p.Artigiano != 0 || p.Minimo != 20 || p.Supplemento != (Stima{7, 8}) || p.Misurato {
		t.Fatalf("senza buste pronte %+v", p)
	}
	if got := salvate(t, d); got != "∅|∅|∅" {
		t.Fatalf("la lettura ha scritto: %s", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneStima); n != 0 {
		t.Fatalf("la lettura ha scritto l'audit: %d", n)
	}
}

// inventario: ADM-04
// Salvare scrive le tre chiavi («1» per acceso) e una riga «settings.estimate_updated» con l'attore e i nomi delle chiavi
// cambiate, mai i valori; la prima volta sono tutte e tre (la riga non c'era), una conferma identica scrive l'audit con
// l'elenco vuoto, un solo campo cambiato ne nomina uno.
func TestStimaImpostazioniSalvaEAudit(t *testing.T) {
	b, d, _ := prova(t)
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "10", "artisan_max", "12"))
	if got := salvate(t, d); got != "1|10|12" {
		t.Fatalf("salvate %s", got)
	}
	if got := ultimoAudit(t, d, azioneStima); got != azioneStima+`|settings|estimate|ok|{"changed":["stima_tempi_attiva","stima_tempi_artigiano_min","stima_tempi_artigiano_max"]}` {
		t.Fatalf("audit %q", got)
	}
	if got := valore(t, d, `SELECT attore FROM eventi_audit WHERE azione = ?`, azioneStima); got != chi.Attore {
		t.Fatalf("attore %v", got)
	}
	if p := paginaStima(t, b); !p.Attiva || p.Min != 10 || p.Max != 12 || p.Supplemento != (Stima{10, 12}) {
		t.Fatalf("dopo %+v", p)
	}
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "10", "artisan_max", "12"))
	if got := ultimoAudit(t, d, azioneStima); got != azioneStima+`|settings|estimate|ok|{"changed":[]}` {
		t.Fatalf("conferma identica %q", got)
	}
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "10", "artisan_max", "15"))
	if got := ultimoAudit(t, d, azioneStima); got != azioneStima+`|settings|estimate|ok|{"changed":["stima_tempi_artigiano_max"]}` {
		t.Fatalf("un campo %q", got)
	}
	salvaStima(t, b, moduloStima("enabled", "false", "artisan_min", "10", "artisan_max", "15"))
	if got := salvate(t, d); got != "0|10|15" {
		t.Fatalf("spenta %s", got)
	}
	if got := ultimoAudit(t, d, azioneStima); got != azioneStima+`|settings|estimate|ok|{"changed":["stima_tempi_attiva"]}` {
		t.Fatalf("spegnere %q", got)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ?`, azioneStima); n != 4 {
		t.Fatalf("righe d'audit %d", n)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = ? AND (dati LIKE '%10%' OR dati LIKE '%15%')`, azioneStima); n != 0 {
		t.Fatal("l'audit porta un valore")
	}
}

// inventario: ADM-04
// I giorni si riportano in 0-90 e non si rifiutano mai; un testo da cui non si legge un intero (con spazi, con la virgola,
// vuoto) vale il valore di partenza 7 o 8; il segno e gli zeri davanti vanno bene (Integer.parse/1 senza resto); il
// massimo non è mai sotto il minimo (lo alza). Quello che si salva è già riportato, anche per un intero oltre i 64 bit.
func TestStimaImpostazioniGiorniRiportati(t *testing.T) {
	b, d, _ := prova(t)
	casi := []struct{ min, max, salvate string }{
		{"-5", "120", "0|90"},
		{"0", "0", "0|0"},
		{"90", "90", "90|90"},
		{"91", "500", "90|90"},
		{"30", "10", "30|30"}, // massimo sotto il minimo: alzato
		{"20", "25", "20|25"},
		{"abc", "", "7|8"},
		{" 5", "5 ", "7|8"}, // gli spazi non si tagliano: Integer.parse/1 dà un resto
		{"5,5", "5.5", "7|8"},
		{"+5", "+9", "5|9"},
		{"007", "-0", "7|7"},
		{"99999999999999999999999", "99999999999999999999999", "90|90"},
		{"-99999999999999999999", "x", "0|8"},
		{"80", "abc", "80|80"}, // il massimo illeggibile vale 8, che è sotto 80: alzato a 80
		{"abc", "3", "7|7"},    // il minimo di partenza 7 e il massimo 3: alzato a 7
		{"٣", "٤", "7|8"},      // cifre non ASCII: nessun intero
	}
	for _, c := range casi {
		salvaStima(t, b, moduloStima("enabled", "false", "artisan_min", c.min, "artisan_max", c.max))
		got := valore(t, d, `SELECT valore FROM impostazioni WHERE chiave = ?`, chiaveMin).(string) + "|" + valore(t, d, `SELECT valore FROM impostazioni WHERE chiave = ?`, chiaveMax).(string)
		if got != c.salvate {
			t.Errorf("%q, %q: salvate %s, attese %s", c.min, c.max, got, c.salvate)
		}
		if p := paginaStima(t, b); p.Min != atoi(t, strings.Split(c.salvate, "|")[0]) || p.Max != atoi(t, strings.Split(c.salvate, "|")[1]) {
			t.Errorf("%q, %q: la pagina dà %d-%d", c.min, c.max, p.Min, p.Max)
		}
	}
}

func atoi(t *testing.T, s string) int64 {
	t.Helper()
	var n int64
	for _, c := range s {
		n = n*10 + int64(c-'0')
	}
	return n
}

// inventario: ADM-04
// Un campo dei giorni che non arriva lascia quello che c'è (un invio a mano senza il campo non riporta a 7-8); un campo
// che arriva ma non è un testo (una lista, una mappa) vale il valore di partenza. L'interruttore invece spento se non
// arriva o non è un testo: solo «true», «1» e «on» lo accendono (tutto il resto, anche «TRUE» o «yes», spegne).
func TestStimaImpostazioniCampiMancantiENonTesto(t *testing.T) {
	b, d, _ := prova(t)
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "20", "artisan_max", "30"))
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "21"))
	if got := salvate(t, d); got != "1|21|30" {
		t.Fatalf("massimo mancante: %s", got)
	}
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_max", "40"))
	if got := salvate(t, d); got != "1|21|40" {
		t.Fatalf("minimo mancante: %s", got)
	}
	salvaStima(t, b, moduloStima("artisan_min", "21", "artisan_max", "40"))
	if got := salvate(t, d); got != "0|21|40" {
		t.Fatalf("interruttore mancante: %s", got)
	}
	salvaStima(t, b, ModuloStima{"enabled": nil, "artisan_min": nil, "artisan_max": nil})
	if got := salvate(t, d); got != "0|7|8" {
		t.Fatalf("campi che non sono testi: %s", got)
	}
	for v, acceso := range map[string]bool{"true": true, "1": true, "on": true, "false": false, "0": false, "": false, "TRUE": false, "yes": false, "On": false, " true": false} {
		salvaStima(t, b, moduloStima("enabled", v))
		if p := paginaStima(t, b); p.Attiva != acceso {
			t.Errorf("enabled=%q: acceso %v, atteso %v", v, p.Attiva, acceso)
		}
	}
	// Un modulo vuoto è un modulo valido: spegne e lascia i giorni.
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "12", "artisan_max", "14"))
	salvaStima(t, b, ModuloStima{})
	if got := salvate(t, d); got != "0|12|14" {
		t.Fatalf("modulo vuoto: %s", got)
	}
}

// inventario: ADM-04
// Una riga scritta da altro che dalla pagina si legge riparata: un valore diverso da «1» è spenta, i giorni illeggibili o
// fuori campo come quando si salvano, il massimo sotto il minimo alzato. La pagina e la stima del cliente leggono lo
// stesso.
func TestStimaImpostazioniLetturaRiparata(t *testing.T) {
	b, d, _ := prova(t)
	for k, v := range map[string]string{chiaveAttiva: "true", chiaveMin: "250", chiaveMax: "2"} {
		esegui(t, d, `INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES (?, ?, ?)`, k, v, istanteProva)
	}
	if p := paginaStima(t, b); p.Attiva || p.Min != 90 || p.Max != 90 {
		t.Fatalf("riparata %+v", p)
	}
	esegui(t, d, `UPDATE impostazioni SET valore = '1' WHERE chiave = ?`, chiaveAttiva)
	esegui(t, d, `UPDATE impostazioni SET valore = 'dieci' WHERE chiave = ?`, chiaveMin)
	esegui(t, d, `UPDATE impostazioni SET valore = '3' WHERE chiave = ?`, chiaveMax)
	if p := paginaStima(t, b); !p.Attiva || p.Min != 7 || p.Max != 7 {
		t.Fatalf("acceso, minimo illeggibile %+v", p)
	}
}

// inventario: ADM-04, RIP-H-55
// «Oggi»: con venti buste di bottega pronte la base 9-20 e i conti («ora 20», nessuna dall'artigiano), il supplemento
// 7-8 «dalle impostazioni»; da dieci buste dall'artigiano il supplemento misurato («misurato sui dati», come
// supplement_source). Con meno di venti la base manca.
func TestStimaImpostazioniOggi(t *testing.T) {
	b, _, _ := prova(t)
	pronteDa(t, b, 5, 8, false)
	if p := paginaStima(t, b); p.Oggi != nil || p.Bottega != 5 || p.Artigiano != 0 || p.Supplemento != (Stima{7, 8}) || p.Misurato {
		t.Fatalf("cinque buste %+v", p)
	}
	for g := 5; g < 20; g++ { // con le cinque di prima, venti: 5, 6, …, 19 e cinque da 8 giorni
		pronteDa(t, b, 1, g, false)
	}
	p := paginaStima(t, b)
	if p.Oggi == nil || p.Bottega != 20 || p.Artigiano != 0 || p.Minimo != 20 || p.Supplemento != (Stima{7, 8}) || p.Misurato {
		t.Fatalf("venti buste %+v", p)
	}
	base := *p.Oggi
	pronteDa(t, b, 10, 30, true)
	p = paginaStima(t, b)
	if p.Oggi == nil || *p.Oggi != base || p.Artigiano != 10 || !p.Misurato || p.Supplemento != (Stima{30 - base.Min, 31 - base.Max}) {
		t.Fatalf("con l'artigiano %+v (base %+v)", p, base)
	}
}

// inventario: ADM-04
// Le chiavi e l'audit stanno nella stessa transazione: se l'audit non si scrive non resta nessuna chiave
// (e la riga che c'era non cambia), e l'errore arriva a chi chiama.
func TestStimaImpostazioniTuttoONiente(t *testing.T) {
	b, d, _ := prova(t)
	salvaStima(t, b, moduloStima("enabled", "false", "artisan_min", "10", "artisan_max", "11"))
	esegui(t, d, `CREATE TRIGGER no_stima BEFORE INSERT ON eventi_audit WHEN NEW.azione = 'settings.estimate_updated' BEGIN SELECT RAISE(ABORT, 'no'); END`)
	if err := b.SalvaStima(ctx, moduloStima("enabled", "true", "artisan_min", "20", "artisan_max", "25"), chi); err == nil {
		t.Fatal("l'audit non si scrive e il salvataggio è riuscito")
	}
	if got := salvate(t, d); got != "0|10|11" {
		t.Fatalf("il salvataggio fallito ha cambiato le chiavi: %s", got)
	}
}

// inventario: ADM-04, RIP-H-55
// L'interruttore salvato è quello che legge la pagina del cliente (061): con venti buste pronte e la stima spenta il cliente
// non vede niente, accesa dalle impostazioni vede «Pronta di solito in 9-20 giorni dal ritiro», rispenta torna a non vederla; i
// giorni del supplemento salvati valgono per una busta data all'artigiano.
func TestStimaImpostazioniEPaginaDelCliente(t *testing.T) {
	b, _, _ := prova(t)
	busta := crea(t, b, modulo())
	ventiPronte(t, b)
	if s := pagina(t, b, busta.Token).Stima; s != nil {
		t.Fatalf("spenta: %+v", s)
	}
	salvaStima(t, b, moduloStima("enabled", "true", "artisan_min", "10", "artisan_max", "12"))
	if s := pagina(t, b, busta.Token).Stima; s == nil || *s != (Stima{9, 20}) {
		t.Fatalf("accesa: %+v", s)
	}
	esegui(t, b.d, `UPDATE buste SET consegnata_artigiano_il = ? WHERE id = ?`, istanteProva, busta.ID)
	if s := pagina(t, b, busta.Token).Stima; s == nil || *s != (Stima{19, 32}) {
		t.Fatalf("con l'artigiano (supplemento 10-12): %+v", s)
	}
	salvaStima(t, b, moduloStima("enabled", "false", "artisan_min", "10", "artisan_max", "12"))
	if s := pagina(t, b, busta.Token).Stima; s != nil {
		t.Fatalf("rispenta: %+v", s)
	}
}
