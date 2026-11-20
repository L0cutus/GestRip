package assist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/L0cutus/gestrip/internal/clock"
	"github.com/L0cutus/gestrip/internal/db"
	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La configurazione dell'assistente e la pagina (slice 107; ADM-11, RIP-H-46 parte impostazioni), come
// Assist.Config, Assist.Glossary e il «test» di AssistLive dello standalone a go-baseline (config_test.exs,
// glossary_test.exs, assist_live_test.exs). Indirizzi .invalid o di questa macchina, chiavi inventate.

const indirizzoLocale = "http://127.0.0.1:11434"

// impostazioniDiProva è la pagina dell'assistente su un database nuovo.
func impostazioniDiProva(t *testing.T) (*Impostazioni, *db.DB) {
	t.Helper()
	d, err := db.Apri(filepath.Join(t.TempDir(), "gestrip.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Chiudi() })
	o := clock.NuovoFinto(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	if _, err := d.Migra(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	i := NuoveImpostazioni(d, o)
	i.Attesa = 2 * time.Second
	return i, d
}

func ai(kv ...string) Modulo {
	m := Modulo{"endpoint": indirizzoLocale, "model": "qwen2.5:7b"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func stt(kv ...string) Modulo {
	m := Modulo{"endpoint": "http://127.0.0.1:8900"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func salva(t *testing.T, i *Impostazioni, famiglia string, m Modulo) *Rifiuto {
	t.Helper()
	rif, err := i.Salva(context.Background(), famiglia, m, chi)
	if err != nil {
		t.Fatal(err)
	}
	return rif
}

func salvaBene(t *testing.T, i *Impostazioni, famiglia string, m Modulo) {
	t.Helper()
	if rif := salva(t, i, famiglia, m); rif != nil {
		t.Fatalf("rifiutato: %v", rif.Errori)
	}
}

func riga(t *testing.T, d *db.DB, dove, chiave string) string {
	t.Helper()
	var v string
	if err := d.Lettura.QueryRow(`SELECT valore FROM `+dove, chiave).Scan(&v); err != nil {
		return "(assente)"
	}
	return v
}

func impostazione(t *testing.T, d *db.DB, chiave string) string {
	return riga(t, d, `impostazioni WHERE chiave = ?`, chiave)
}

func segreto(t *testing.T, d *db.DB, nome string) string {
	return riga(t, d, `segreti_provider WHERE nome = ?`, nome)
}

func conta(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Lettura.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

type eventoAudit struct {
	Attore, Risorsa, ID, Esito string
	Dati                       map[string]any
}

func ultimoEvento(t *testing.T, d *db.DB, azione string) eventoAudit {
	t.Helper()
	var e eventoAudit
	var dati string
	err := d.Lettura.QueryRow(`SELECT attore, risorsa, id_risorsa, esito, dati FROM eventi_audit WHERE azione = ? ORDER BY id DESC LIMIT 1`, azione).
		Scan(&e.Attore, &e.Risorsa, &e.ID, &e.Esito, &dati)
	if err != nil {
		t.Fatalf("%s: %v", azione, err)
	}
	if err := json.Unmarshal([]byte(dati), &e.Dati); err != nil {
		t.Fatal(err)
	}
	return e
}

func errori(rif *Rifiuto) map[string][]string {
	if rif == nil {
		return nil
	}
	return rif.Errori
}

// «an installation with nothing typed»: nessuna famiglia configurata, niente di predefinito.
func TestNienteDiPartenza(t *testing.T) {
	i, d := impostazioniDiProva(t)
	p, err := i.Pagina(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p != (PaginaAssistente{}) {
		t.Errorf("pagina %+v", p)
	}
	if c, err := LeggiEstrazione(context.Background(), d.Lettura); err != nil || c != (Configurazione{}) || c.Usabile() {
		t.Errorf("estrazione %v %v", c, err)
	}
	if c, err := LeggiTrascrizione(context.Background(), d.Lettura); err != nil || c != (ConfigTrascrizione{}) || c.Usabile() {
		t.Errorf("trascrizione %v %v", c, err)
	}
}

// «what the owner types»: indirizzo e modello rendono usabile l'estrazione, il solo indirizzo la trascrizione;
// salvati nelle chiavi dello standalone, la chiave fra i segreti dei provider; il client li legge.
func TestSalvaEdLeggi(t *testing.T) {
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", "Poloni, Bottari"))
	for k, v := range map[string]string{"assist.ai.endpoint": indirizzoLocale, "assist.ai.model": "qwen2.5:7b", "assist.stt.endpoint": "http://127.0.0.1:8900",
		"assist.stt.glossary": "Poloni\nBottari", "assist.ai.api_key": "(assente)", "assist.stt.api_key": "(assente)", "assist.stt.model": "(assente)"} {
		if g := impostazione(t, d, k); g != v {
			t.Errorf("impostazione %s = %q, attesa %q", k, g, v)
		}
	}
	if g := segreto(t, d, "assist.ai.api_key"); g != chiaveFinta {
		t.Errorf("chiave salvata %q", g)
	}
	if g := segreto(t, d, "assist.stt.api_key"); g != "(assente)" {
		t.Errorf("chiave della trascrizione %q", g)
	}
	c, err := LeggiEstrazione(context.Background(), d.Lettura)
	if err != nil || c != (Configurazione{Indirizzo: indirizzoLocale, Modello: "qwen2.5:7b", Chiave: chiaveFinta}) || !c.Usabile() {
		t.Errorf("estrazione %#v %v", c, err)
	}
	s, err := LeggiTrascrizione(context.Background(), d.Lettura)
	if err != nil || s != (ConfigTrascrizione{Indirizzo: "http://127.0.0.1:8900", Glossario: "Poloni\nBottari"}) || !s.Usabile() {
		t.Errorf("trascrizione %#v %v", s, err)
	}
	p, err := i.Pagina(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	attesa := PaginaAssistente{
		Estrazione:   Famiglia{Indirizzo: indirizzoLocale, Modello: "qwen2.5:7b", ChiaveSalvata: true, Configurata: true},
		Trascrizione: Famiglia{Indirizzo: "http://127.0.0.1:8900", Glossario: "Poloni\nBottari", Configurata: true},
	}
	if p != attesa {
		t.Errorf("pagina %+v", p)
	}
}

// I rifiuti del changeset, uno per campo e con i messaggi dello standalone; niente scritto, nessuna riga di audit.
func TestRifiuti(t *testing.T) {
	lungo := func(n int) string { return strings.Repeat("è", n) }
	casi := []struct {
		nome     string
		famiglia string
		m        Modulo
		attesi   map[string][]string
	}{
		{"senza modello", FamigliaEstrazione, Modulo{"endpoint": indirizzoLocale}, map[string][]string{"model": {msgVuoto}}},
		{"modello di spazi", FamigliaEstrazione, ai("model", "  \t"), map[string][]string{"model": {msgVuoto}}},
		{"indirizzo di spazi", FamigliaTrascrizione, stt("endpoint", " \u2003 "), map[string][]string{"endpoint": {msgVuoto}}},
		{"niente", FamigliaEstrazione, Modulo{}, map[string][]string{"endpoint": {msgVuoto}, "model": {msgVuoto}}},
		{"trascrizione senza indirizzo", FamigliaTrascrizione, Modulo{"glossary": "Poloni"}, map[string][]string{"endpoint": {msgVuoto}}},
		{"non un indirizzo", FamigliaEstrazione, ai("endpoint", "ollama:11434"), map[string][]string{"endpoint": {msgSchema}}},
		// Senza schema http la chiave non è «in chiaro» (remote_plaintext?/1 guarda solo http): solo l'errore dello schema.
		{"non un indirizzo, con la chiave", FamigliaEstrazione, ai("endpoint", "ollama:11434", "api_key", chiaveFinta), map[string][]string{"endpoint": {msgSchema}}},
		{"senza schema, con la chiave", FamigliaEstrazione, ai("endpoint", "//modelli.example.invalid", "api_key", chiaveFinta), map[string][]string{"endpoint": {msgSchema}}},
		{"ftp", FamigliaEstrazione, ai("endpoint", "ftp://host/x"), map[string][]string{"endpoint": {msgSchema}}},
		{"senza schema", FamigliaTrascrizione, stt("endpoint", "127.0.0.1:8900"), map[string][]string{"endpoint": {msgSchema}}},
		{"schema senza server", FamigliaEstrazione, ai("endpoint", "http://"), map[string][]string{"endpoint": {msgSenzaHost}}},
		{"solo la porta", FamigliaEstrazione, ai("endpoint", "https://:443"), map[string][]string{"endpoint": {msgSenzaHost}}},
		{"chiave in chiaro altrove", FamigliaEstrazione, ai("endpoint", "http://modelli.example.invalid", "api_key", chiaveFinta),
			map[string][]string{"api_key": {msgChiaveInChiaro}}},
		{"chiave in chiaro altrove, trascrizione", FamigliaTrascrizione, stt("endpoint", "HTTP://LOCALHOST:8900", "api_key", chiaveFinta),
			map[string][]string{"api_key": {msgChiaveInChiaro}}},
		{"chiave verso un indirizzo senza host", FamigliaEstrazione, ai("endpoint", "http://", "api_key", chiaveFinta),
			map[string][]string{"endpoint": {msgSenzaHost}, "api_key": {msgChiaveInChiaro}}},
		{"indirizzo lungo", FamigliaEstrazione, ai("endpoint", "http://"+lungo(249)), map[string][]string{"endpoint": {msgTroppoLungo(255)}}},
		{"modello lungo", FamigliaEstrazione, ai("model", lungo(121)), map[string][]string{"model": {msgTroppoLungo(120)}}},
		{"chiave lunga", FamigliaEstrazione, ai("api_key", lungo(256)), map[string][]string{"api_key": {msgTroppoLungo(255)}}},
		{"chiave lunga e in chiaro", FamigliaEstrazione, ai("endpoint", "http://x.invalid", "api_key", lungo(256)),
			map[string][]string{"api_key": {msgTroppoLungo(255), msgChiaveInChiaro}}},
		{"glossario oltre il limite di uno", FamigliaTrascrizione, stt("glossary", strings.Repeat("a", 2001)),
			map[string][]string{"glossary": {"1 carattere di troppo sul limite di 2000"}}},
		{"glossario oltre di molto", FamigliaTrascrizione, stt("glossary", strings.Repeat("a", 4001)),
			map[string][]string{"glossary": {msgTroppoLungo(4000), "2001 caratteri di troppo sul limite di 2000"}}},
		{"glossario contato a una riga", FamigliaTrascrizione, stt("glossary", strings.Repeat("a\n", 668)),
			map[string][]string{"glossary": {"2 caratteri di troppo sul limite di 2000"}}},
		{"cancella non booleano", FamigliaEstrazione, ai("clear_secret", "on"), map[string][]string{"clear_secret": {msgNonValido}}},
		{"cancella maiuscolo", FamigliaEstrazione, ai("clear_secret", "TRUE"), map[string][]string{"clear_secret": {msgNonValido}}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			i, d := impostazioniDiProva(t)
			rif := salva(t, i, c.famiglia, c.m)
			if fmt.Sprint(errori(rif)) != fmt.Sprint(c.attesi) {
				t.Errorf("errori %v, attesi %v", errori(rif), c.attesi)
			}
			if n := conta(t, d, `SELECT (SELECT count(*) FROM impostazioni WHERE chiave LIKE 'assist.%') + (SELECT count(*) FROM segreti_provider) + (SELECT count(*) FROM eventi_audit)`); n != 0 {
				t.Errorf("%d righe scritte da un rifiuto", n)
			}
		})
	}
}

// Il glossario di 1999 caratteri nella forma a una riga (667 voci) ci sta; quello salvato è la forma a righe.
func TestGlossarioAlLimite(t *testing.T) {
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", strings.Repeat("a\n", 667)))
	if g := impostazione(t, d, "assist.stt.glossary"); g != strings.Repeat("a\n", 666)+"a" {
		t.Errorf("glossario %d caratteri", len(g))
	}
}

// Assist.Glossary: forma salvata, forma a una riga, eccesso.
func TestGlossario(t *testing.T) {
	for _, c := range []struct{ dentro, salvato, rete string }{
		{"Poloni, Bottari", "Poloni\nBottari", "Poloni, Bottari"},
		{"  Poloni  \r\n\r\n Gioielli   Bottari ,,Rossi\n", "Poloni\nGioielli Bottari\nRossi", "Poloni, Gioielli Bottari, Rossi"},
		{"", "", ""},
		{" ,\n , ", "", ""},
		{"a b c", "a b c", "a b c"},
	} {
		if g := GlossarioSalvato(c.dentro); g != c.salvato {
			t.Errorf("salvato(%q) = %q, atteso %q", c.dentro, g, c.salvato)
		}
		if g := GlossarioPerLaRete(c.dentro); g != c.rete {
			t.Errorf("rete(%q) = %q, attesa %q", c.dentro, g, c.rete)
		}
		if GlossarioPerLaRete(GlossarioSalvato(c.dentro)) != c.rete {
			t.Errorf("la forma salvata di %q non torna alla stessa riga", c.dentro)
		}
	}
	for _, c := range []struct {
		dentro string
		n      int
	}{{strings.Repeat("a", 2000), 0}, {strings.Repeat("a", 2001), 1}, {strings.Repeat("è", 2003), 3}, {strings.Repeat("a\n", 667), 0}, {strings.Repeat("a\n", 668), 2}} {
		if g := EccessoGlossario(c.dentro); g != c.n {
			t.Errorf("eccesso di %d byte = %d, atteso %d", len(c.dentro), g, c.n)
		}
	}
}

// L'indirizzo come URI.parse/1 di Elixir 1.18 (misurato su 17023 indirizzi a caso: nessuna differenza).
func TestPartiIndirizzo(t *testing.T) {
	for _, c := range []struct{ dentro, schema, host string }{
		{"http://127.0.0.1:11434", "http", "127.0.0.1"},
		{"HTTP://Host:1", "http", "Host"},
		{"https://x", "https", "x"},
		{"http://", "http", ""},
		{"http:///x", "http", ""},
		{"http:x", "http", ""},
		{"ftp://x", "ftp", "x"},
		{"x", "", ""},
		{"", "", ""},
		{"http://[::1]:8080", "http", "::1"},
		{"http://[::1", "http", ""},
		{"http://user:pw@host:80/p", "http", "host"},
		{"http://LOCALHOST", "http", "LOCALHOST"},
		{"http://0:0:0:0:0:0:0:1", "http", "0"},
		{"http://[0:0:0:0:0:0:0:1]", "http", "0:0:0:0:0:0:0:1"},
		{"http://:80", "http", ""},
		{"http://@", "http", ""},
		{"http://a b", "http", "a b"},
		{"//host", "", "host"},
		{"http://host?x", "http", "host"},
		{"http://ho\nst", "http", "ho\nst"},
		{"http://a:b:c", "http", "a"},
		{"http://[::1]x", "http", "::1"},
		{"1http://x", "", ""},
		{"ht+tp://x", "ht+tp", "x"},
		{"ſttp://x", "", ""},
		{"http://a@b@c", "http", "c"},
		{"http://[[::1]]", "http", ""},
		{"http://localhost#x", "http", "localhost"},
	} {
		if s, h := partiIndirizzo(c.dentro); s != c.schema || h != c.host {
			t.Errorf("%q: %q %q, attesi %q %q", c.dentro, s, h, c.schema, c.host)
		}
	}
}

// «a key never travels in the clear to somewhere else»: in chiaro solo verso questa macchina, https ovunque.
func TestChiaveInChiaroSoloQui(t *testing.T) {
	for _, e := range []string{"http://127.0.0.1:11434", "http://localhost:8900", "http://[::1]:8080", "http://[0:0:0:0:0:0:0:1]:1",
		" http://localhost ", "https://modelli.example.invalid", "HTTPS://modelli.example.invalid:8443/v1"} {
		i, d := impostazioniDiProva(t)
		salvaBene(t, i, FamigliaEstrazione, ai("endpoint", e, "api_key", chiaveFinta))
		if segreto(t, d, "assist.ai.api_key") != chiaveFinta {
			t.Errorf("%q: chiave non salvata", e)
		}
	}
	for _, e := range []string{"http://modelli.example.invalid", "http://127.0.0.2", "http://LOCALHOST", "http://localhost.", "http://0:0:0:0:0:0:0:1",
		"http://user@127.0.0.1x",
		" http://modelli.example.invalid", "\thttp://127.0.0.2"} {
		i, _ := impostazioniDiProva(t)
		if rif := salva(t, i, FamigliaEstrazione, ai("endpoint", e, "api_key", chiaveFinta)); rif == nil || !slices.Equal(rif.Errori["api_key"], []string{msgChiaveInChiaro}) {
			t.Errorf("%q: %v", e, errori(rif))
		}
	}
	// La regola guarda i valori finali: una chiave salvata non resta se l'indirizzo passa in chiaro altrove.
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
	if rif := salva(t, i, FamigliaEstrazione, Modulo{"endpoint": "http://modelli.example.invalid"}); rif == nil ||
		!slices.Equal(rif.Errori["api_key"], []string{msgChiaveInChiaro}) {
		t.Errorf("chiave tenuta verso un altro host: %v", errori(rif))
	}
	if impostazione(t, d, "assist.ai.endpoint") != indirizzoLocale {
		t.Error("indirizzo cambiato da un rifiuto")
	}
	// Con la chiave cancellata nello stesso salvataggio, si può.
	salvaBene(t, i, FamigliaEstrazione, Modulo{"endpoint": "http://modelli.example.invalid", "clear_secret": "true"})
}

// «the secret»: casella vuota o di spazi = tiene; «Cancella la chiave salvata» vince su una chiave scritta; mai
// nel rifiuto, nemmeno quella tenuta.
func TestLaChiave(t *testing.T) {
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
	for _, vuota := range []string{"", "   "} {
		salvaBene(t, i, FamigliaEstrazione, ai("api_key", vuota))
		if segreto(t, d, "assist.ai.api_key") != chiaveFinta {
			t.Errorf("la casella %q ha cancellato la chiave", vuota)
		}
	}
	salvaBene(t, i, FamigliaEstrazione, Modulo{"model": "gemma3:12b"}) // senza la casella
	if segreto(t, d, "assist.ai.api_key") != chiaveFinta {
		t.Error("un modulo senza la casella ha cancellato la chiave")
	}
	rif := salva(t, i, FamigliaEstrazione, ai("model", "", "api_key", ""))
	if rif == nil || slices.Contains(mappaValori(rif), chiaveFinta) {
		t.Fatalf("rifiuto con la chiave salvata: %+v", rif)
	}
	if _, c := rif.Valori["api_key"]; c {
		t.Error("la chiave sta fra i valori del rifiuto")
	}
	rif = salva(t, i, FamigliaEstrazione, ai("model", "", "api_key", "chiave-finta-nuova"))
	if rif == nil || slices.Contains(mappaValori(rif), "chiave-finta-nuova") {
		t.Fatalf("rifiuto con la chiave scritta: %+v", rif)
	}
	for _, v := range []string{"1", "true", "on"} {
		salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
		m := ai("api_key", "chiave-finta-altra", "clear_secret", v)
		rif, err := i.Salva(context.Background(), FamigliaEstrazione, m, chi)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case v == "on" && (rif == nil || !slices.Equal(rif.Errori["clear_secret"], []string{msgNonValido})):
			t.Errorf("on: %+v", rif)
		case v != "on" && (rif != nil || segreto(t, d, "assist.ai.api_key") != "(assente)"):
			t.Errorf("%s: chiave ancora %q, %+v", v, segreto(t, d, "assist.ai.api_key"), rif)
		}
	}
	for _, no := range []string{"false", "0", ""} {
		salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta, "clear_secret", no))
		if segreto(t, d, "assist.ai.api_key") != chiaveFinta {
			t.Errorf("clear_secret %q ha cancellato", no)
		}
	}
}

func mappaValori(r *Rifiuto) []string {
	var out []string
	for _, v := range r.Valori {
		out = append(out, v)
	}
	return out
}

// Il rifiuto rimette nel modulo i valori mandati così come sono (anche vuoti), quelli salvati per i campi non
// mandati, mai la chiave.
func TestValoriDelRifiuto(t *testing.T) {
	i, _ := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", "Poloni", "api_key", chiaveFinta))
	rif := salva(t, i, FamigliaTrascrizione, Modulo{"endpoint": " ftp://x ", "glossary": "Rossi,  Bianchi"})
	if rif == nil {
		t.Fatal("non rifiutato")
	}
	if fmt.Sprint(rif.Valori) != fmt.Sprint(map[string]string{"endpoint": " ftp://x ", "glossary": "Rossi,  Bianchi"}) {
		t.Errorf("valori %q", rif.Valori)
	}
	rif = salva(t, i, FamigliaTrascrizione, Modulo{"endpoint": ""})
	if rif == nil || fmt.Sprint(rif.Valori) != fmt.Sprint(map[string]string{"endpoint": "", "glossary": "Poloni"}) {
		t.Errorf("valori %+v", rif)
	}
}

// Le validazioni delle lunghezze guardano solo i campi cambiati (validate_change): un modello importato più
// lungo di 120 si risalva com'è; l'indirizzo e la chiave in chiaro si controllano sempre (get_field).
func TestSoloICambiati(t *testing.T) {
	i, d := impostazioniDiProva(t)
	lungo := strings.Repeat("m", 130)
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.ai.endpoint', ?, 'x'), ('assist.ai.model', ?, 'x')`,
		indirizzoLocale, lungo); err != nil {
		t.Fatal(err)
	}
	salvaBene(t, i, FamigliaEstrazione, Modulo{"endpoint": indirizzoLocale, "model": lungo})
	if impostazione(t, d, "assist.ai.model") != lungo {
		t.Error("modello cambiato")
	}
	if _, err := d.Scrittura.Exec(`UPDATE impostazioni SET valore = 'ftp://importato' WHERE chiave = 'assist.ai.endpoint'`); err != nil {
		t.Fatal(err)
	}
	if rif := salva(t, i, FamigliaEstrazione, Modulo{}); rif == nil || !slices.Equal(rif.Errori["endpoint"], []string{msgSchema}) {
		t.Errorf("indirizzo importato non valido: %v", errori(rif))
	}
}

// «the audit line»: assist.provider_configured con la famiglia, i soli nomi dei campi cambiati (nell'ordine di
// keys/1) e se è configurata; mai un valore; nella stessa transazione.
func TestAuditDelSalvataggio(t *testing.T) {
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
	e := ultimoEvento(t, d, "assist.provider_configured")
	if e.Attore != chi.Attore || e.Risorsa != "assist_provider" || e.ID != "ai" || e.Esito != "ok" ||
		fmt.Sprint(e.Dati) != fmt.Sprint(map[string]any{"family": "ai", "changed": []any{"endpoint", "model", "api_key"}, "configured": true}) {
		t.Errorf("audit %+v", e)
	}
	var tutto string
	if err := d.Lettura.QueryRow(`SELECT group_concat(dati) FROM eventi_audit`).Scan(&tutto); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{chiaveFinta, "qwen", "127.0.0.1"} {
		if strings.Contains(tutto, v) {
			t.Errorf("l'audit contiene %q", v)
		}
	}
	salvaBene(t, i, FamigliaEstrazione, ai("model", "gemma3:12b"))
	if e := ultimoEvento(t, d, "assist.provider_configured"); fmt.Sprint(e.Dati["changed"]) != "[model]" {
		t.Errorf("changed %v", e.Dati["changed"])
	}
	salvaBene(t, i, FamigliaEstrazione, ai("model", "gemma3:12b"))
	if e := ultimoEvento(t, d, "assist.provider_configured"); fmt.Sprint(e.Dati["changed"]) != "[]" {
		t.Errorf("conferma identica: changed %v", e.Dati["changed"])
	}
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", "Poloni,Bottari"))
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", "Poloni\nBottari"))
	if e := ultimoEvento(t, d, "assist.provider_configured"); e.ID != "stt" || fmt.Sprint(e.Dati) != fmt.Sprint(map[string]any{"family": "stt", "changed": []any{}, "configured": true}) {
		t.Errorf("stesso glossario in un'altra forma: %+v", e)
	}
	// Un glossario di sole virgole è un testo vuoto, diverso da «niente»: cambiato, come put_change/3.
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", ""))
	salvaBene(t, i, FamigliaTrascrizione, stt("glossary", ",,"))
	if e := ultimoEvento(t, d, "assist.provider_configured"); fmt.Sprint(e.Dati["changed"]) != "[glossary]" || impostazione(t, d, "assist.stt.glossary") != "" {
		t.Errorf("glossario di virgole: %+v", e)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.provider_configured'`); n != 7 {
		t.Errorf("%d righe", n)
	}
	salvaBene(t, i, FamigliaEstrazione, ai("api_key", chiaveFinta))
	salvaBene(t, i, FamigliaEstrazione, ai("clear_secret", "1"))
	if e := ultimoEvento(t, d, "assist.provider_configured"); fmt.Sprint(e.Dati["changed"]) != "[api_key]" {
		t.Errorf("cancella: %v", e.Dati["changed"])
	}
}

// Il glossario si tocca solo se cambia: uno importato in un'altra forma (o oltre il limite) resta com'è quando si
// salva solo l'indirizzo. E usable?/1 della trascrizione guarda l'indirizzo senza spazi.
func TestGlossarioImportatoNonCambiato(t *testing.T) {
	i, d := impostazioniDiProva(t)
	importato := "Poloni, Bottari  Rossi"
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.stt.glossary', ?, 'x')`, importato); err != nil {
		t.Fatal(err)
	}
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": "http://127.0.0.1:8900"})
	if g := impostazione(t, d, "assist.stt.glossary"); g != importato {
		t.Errorf("glossario riscritto: %q", g)
	}
	if _, err := d.Scrittura.Exec(`UPDATE impostazioni SET valore = ? WHERE chiave = 'assist.stt.glossary'`, strings.Repeat("a", 2100)); err != nil {
		t.Fatal(err)
	}
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"endpoint": "http://127.0.0.1:8901"})
	if (ConfigTrascrizione{Indirizzo: " \t"}).Usabile() || !(ConfigTrascrizione{Indirizzo: "x"}).Usabile() {
		t.Error("Usabile della trascrizione")
	}
}

// Una famiglia che non c'è non scrive niente.
func TestFamigliaSconosciuta(t *testing.T) {
	i, d := impostazioniDiProva(t)
	if _, err := i.Salva(context.Background(), "tts", ai(), chi); err != ErrFamiglia {
		t.Errorf("err %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit`) != 0 {
		t.Error("scritto")
	}
}

// Un valore di soli spazi salvato vale come assente (setting/1): la famiglia non è configurata.
func TestLetturaSpazi(t *testing.T) {
	_, d := impostazioniDiProva(t)
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.ai.endpoint', ' ', 'x'), ('assist.ai.model', 'm', 'x'),
		('assist.stt.endpoint', ' http://127.0.0.1:1 ', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Scrittura.Exec(`INSERT INTO segreti_provider (nome, valore, aggiornato_il) VALUES ('assist.stt.api_key', ' ', '2026-11-20T09:00:00.000000Z')`); err != nil {
		t.Fatal(err)
	}
	if c, _ := LeggiEstrazione(context.Background(), d.Lettura); c != (Configurazione{Modello: "m"}) || c.Usabile() {
		t.Errorf("estrazione %#v", c)
	}
	if c, _ := LeggiTrascrizione(context.Background(), d.Lettura); c != (ConfigTrascrizione{Indirizzo: " http://127.0.0.1:1 "}) || !c.Usabile() {
		t.Errorf("trascrizione %#v", c)
	}
}

// La chiave (e l'indirizzo, che può portare utente e password) non si stampano: né con fmt, né con slog.
func TestConfigurazioneNonSiStampa(t *testing.T) {
	c := Configurazione{Indirizzo: "http://utente:parola@127.0.0.1:1", Modello: "qwen2.5:7b", Chiave: chiaveFinta}
	s := ConfigTrascrizione{Indirizzo: "http://utente:parola@127.0.0.1:2", Chiave: chiaveFinta, Glossario: "Poloni"}
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	log.Info("x", "c", c, "s", s)
	log.Info("y", "c", &c)
	for _, v := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		buf.WriteString(fmt.Sprintf(v, c) + fmt.Sprintf(v, s) + fmt.Sprintf(v, &c))
	}
	buf.WriteString(fmt.Sprint(c, s) + fmt.Sprintln([]Configurazione{c}))
	out := buf.String()
	for _, vietato := range []string{chiaveFinta, "parola", "utente"} {
		if strings.Contains(out, vietato) {
			t.Errorf("stampato %q: %s", vietato, out)
		}
	}
	for _, atteso := range []string{"qwen2.5:7b", "Poloni", "***"} {
		if !strings.Contains(out, atteso) {
			t.Errorf("manca %q: %s", atteso, out)
		}
	}
	if fmt.Sprint(Configurazione{Modello: "m"}) != `assist.Configurazione{Indirizzo: Modello:"m" Chiave:}` {
		t.Errorf("vuota: %v", Configurazione{Modello: "m"})
	}
}

// modelloFintoPerLaPagina è il modello finto della 106 con l'estrazione salvata che punta lì (127.0.0.1: la
// chiave può viaggiare in chiaro).
func modelloFintoPerLaPagina(t *testing.T, i *Impostazioni) *modellofinto.Server {
	t.Helper()
	f := modellofinto.Nuovo()
	s := httptest.NewServer(f)
	t.Cleanup(s.Close)
	salvaBene(t, i, FamigliaEstrazione, Modulo{"endpoint": s.URL, "model": "gestrip-prova", "api_key": chiaveFinta})
	return f
}

// La prova: la frase fissa senza clienti, con il client della 106 sulla configurazione salvata (chiave come
// Bearer, un solo messaggio dell'utente nel formato corto), audit assist.extract con soli conteggi.
func TestProva(t *testing.T) {
	i, d := impostazioniDiProva(t)
	if _, err := i.Prova(context.Background(), chi); err != ErrNonConfigurato {
		t.Errorf("senza configurazione: %v", err)
	}
	if conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract'`) != 0 {
		t.Error("audit senza configurazione")
	}
	f := modelloFintoPerLaPagina(t, i)
	e, err := i.Prova(context.Background(), chi)
	if err != nil || e.Proposta.Descrizione != "anello da lucidare" || e.Modello != modellofinto.NomeModello {
		t.Fatalf("prova %+v %v", e, err)
	}
	r := f.Richieste()
	if len(r) != 1 || r[0].Autorizzazione != "Bearer "+chiaveFinta || r[0].Corpo["model"] != "gestrip-prova" {
		t.Fatalf("richieste %+v", r)
	}
	msg, _ := json.Marshal(r[0].Corpo["messages"])
	if want, _ := json.Marshal([]map[string]string{{"role": "user", "content": "Oggi: venerdì 20/11/2026\nDettato: " + FraseDiProva}}); string(msg) != string(want) {
		t.Errorf("messaggi %s", msg)
	}
	ev := ultimoEvento(t, d, "assist.extract")
	if ev.Esito != "ok" || ev.Attore != chi.Attore || fmt.Sprint(ev.Dati) != fmt.Sprint(map[string]any{"characters": float64(len([]rune(FraseDiProva)))}) {
		t.Errorf("audit %+v", ev)
	}
	f.Programma(modellofinto.Risposta{Stato: 500}, modellofinto.Risposta{Stato: 500})
	if _, err := i.Prova(context.Background(), chi); fmt.Sprint(err) != fmt.Sprint(ErrModello{"ai:http:500"}) {
		t.Errorf("500: %v", err)
	}
	f.Programma(modellofinto.Buona("niente"), modellofinto.Buona("ancora niente"))
	if _, err := i.Prova(context.Background(), chi); err != ErrRispostaIllegibile {
		t.Errorf("illeggibile: %v", err)
	}
	if n := conta(t, d, `SELECT count(*) FROM eventi_audit WHERE azione = 'assist.extract'`); n != 3 {
		t.Errorf("%d righe assist.extract", n)
	}
}

// Una lettura che fallisce è l'errore del database, non «non configurato».
func TestProvaDatabaseChiuso(t *testing.T) {
	i, d := impostazioniDiProva(t)
	_ = d.Chiudi()
	if _, err := i.Prova(context.Background(), chi); err == nil || err == ErrNonConfigurato {
		t.Errorf("err %v", err)
	}
	if _, err := i.Pagina(context.Background()); err == nil {
		t.Error("pagina senza database")
	}
	if _, err := i.Salva(context.Background(), FamigliaEstrazione, ai(), chi); err == nil {
		t.Error("salvataggio senza database")
	}
}

// I limiti delle lunghezze sono compresi: 255, 120 e 255 caratteri esatti si salvano.
func TestLunghezzeAlLimite(t *testing.T) {
	i, d := impostazioniDiProva(t)
	indirizzo := "https://" + strings.Repeat("è", 247)
	modello, chiave := strings.Repeat("m", 120), strings.Repeat("k", 255)
	salvaBene(t, i, FamigliaEstrazione, Modulo{"endpoint": indirizzo, "model": modello, "api_key": chiave})
	if impostazione(t, d, "assist.ai.endpoint") != indirizzo || impostazione(t, d, "assist.ai.model") != modello || segreto(t, d, "assist.ai.api_key") != chiave {
		t.Error("valori al limite non salvati")
	}
}

// changed della trascrizione segue keys(:stt): indirizzo, chiave, glossario.
func TestAuditTrascrizioneInOrdine(t *testing.T) {
	i, d := impostazioniDiProva(t)
	salvaBene(t, i, FamigliaTrascrizione, Modulo{"glossary": "Poloni", "api_key": chiaveFinta, "endpoint": "http://localhost:8900"})
	if e := ultimoEvento(t, d, "assist.provider_configured"); fmt.Sprint(e.Dati["changed"]) != "[endpoint api_key glossary]" || e.Dati["configured"] != true {
		t.Errorf("audit %+v", e)
	}
}

// Un'estrazione con l'indirizzo e senza modello (scritta da un import) non è configurata.
func TestPaginaSenzaModello(t *testing.T) {
	i, d := impostazioniDiProva(t)
	if _, err := d.Scrittura.Exec(`INSERT INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('assist.ai.endpoint', ?, 'x')`, indirizzoLocale); err != nil {
		t.Fatal(err)
	}
	p, err := i.Pagina(context.Background())
	if err != nil || p.Estrazione.Configurata || p.Estrazione.Indirizzo != indirizzoLocale {
		t.Errorf("pagina %+v %v", p, err)
	}
}

// La frase della prova è @self_test dello standalone, parola per parola.
func TestFraseDiProva(t *testing.T) {
	if FraseDiProva != "Busta 1, anello in oro da stringere di una misura, preventivo 40 euro." {
		t.Errorf("frase %q", FraseDiProva)
	}
}
