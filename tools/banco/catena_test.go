package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/L0cutus/gestrip/internal/importer"
)

func TestModalitaDati(t *testing.T) {
	for _, c := range []struct {
		dati    string
		catena  bool
		rifiuta bool
	}{{"", false, false}, {datiSinteticiImport, true, false}, {"sintetici", false, true}, {"veri", false, true}, {" sintetici-import", false, true}} {
		catena, err := modalitaDati(c.dati)
		if (err != nil) != c.rifiuta || catena != c.catena {
			t.Errorf("modalitaDati(%q) = %v, %v", c.dati, catena, err)
		}
	}
}

func istanzaDiProva(t *testing.T) (nomi, percorsiCatena) {
	t.Helper()
	t.Setenv("GESTRIP_BANCO_RUN", t.TempDir())
	n, err := nomiPer("t3")
	if err != nil {
		t.Fatal(err)
	}
	return n, catenaPer(n)
}

// Ogni passo della catena gira in un container senza rete, con le protezioni degli altri
// container del banco, e monta solo cartelle dell'istanza (più il binario gestrip in sola lettura).
func TestComandiCatenaIsolati(t *testing.T) {
	n, p := istanzaDiProva(t)
	bin := "/x/gestrip"
	cmds := map[string][]string{
		"cattura":           comandoCattura(n, "img:1", p),
		"import-standalone": comandoImportStandalone(n, "img:1", []string{"--env", "A=B"}, p),
		"vendita":           comandoVendita(n, "img:1", []string{"--env", "A=B"}, p),
		"emergenza":         comandoEmergenza(n, "img:1", []string{"--env", "A=B"}),
		"decifra":           comandoDecifra(n, "img:1", p),
		"import-go":         comandoImportGo(n, bin, p, strings.Repeat("a", 64), 4096, true),
	}
	for nome, c := range cmds {
		s := strings.Join(c, " ")
		if len(c) == 0 || c[0] != "run" || indice(c, "--rm") < 0 {
			t.Errorf("%s: non è un docker run --rm: %s", nome, s)
		}
		for _, voluto := range []string{"--network none", "--read-only", "--cap-drop ALL", "--user 1000:1000",
			"no-new-privileges:true", "--name " + n.base + "-"} {
			if !strings.Contains(s, voluto) {
				t.Errorf("%s: manca %q in %s", nome, voluto, s)
			}
		}
		for _, vietato := range []string{" -p ", "--publish", "sh -c", "--privileged", n.rete} {
			if strings.Contains(s, vietato) {
				t.Errorf("%s: contiene %q: %s", nome, vietato, s)
			}
		}
		for _, a := range c {
			if !strings.HasPrefix(a, "type=bind,src=") {
				continue
			}
			src := strings.SplitN(strings.TrimPrefix(a, "type=bind,src="), ",", 2)[0]
			if src != bin && !strings.HasPrefix(src, n.dir+string(filepath.Separator)) {
				t.Errorf("%s: monta %s fuori dall'istanza", nome, src)
			}
		}
	}
}

func valoreDopo(c []string, k string) string {
	if i := indice(c, k); i >= 0 && i+1 < len(c) {
		return c[i+1]
	}
	return ""
}

func montaggio(c []string, dst string) (string, bool) {
	for _, a := range c {
		if strings.HasPrefix(a, "type=bind,src=") && strings.Contains(a, ",dst="+dst) {
			resto := strings.SplitN(a, ",dst="+dst, 2)[1]
			if resto != "" && resto != ",readonly" {
				continue
			}
			return strings.SplitN(strings.TrimPrefix(a, "type=bind,src="), ",", 2)[0], resto == ",readonly"
		}
	}
	return "", false
}

func TestComandoCatturaConPythonIsolato(t *testing.T) {
	n, p := istanzaDiProva(t)
	c := comandoCattura(n, "img:1", p)
	i := indice(c, "img:1")
	if i < 0 || strings.Join(c[i+1:], " ") != "python3 -I /cattura/synthetic_capture.py /out/"+filepath.Base(p.flask) {
		t.Fatalf("cattura = %v", c)
	}
	if valoreDopo(c, "--entrypoint") != "" {
		// l'immagine non ha entrypoint: il comando arriva così com'è
		t.Errorf("entrypoint inatteso: %v", c)
	}
	if src, ro := montaggio(c, "/cattura/synthetic_capture.py"); src != p.script || !ro {
		t.Errorf("script montato %q ro=%v", src, ro)
	}
	if src, ro := montaggio(c, "/out"); src != filepath.Dir(p.flask) || ro {
		t.Errorf("uscita montata %q ro=%v", src, ro)
	}
}

func TestComandoImportStandalone(t *testing.T) {
	n, p := istanzaDiProva(t)
	c := comandoImportStandalone(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"}, p)
	s := strings.Join(c, " ")
	if !strings.Contains(s, "--env GESTRIP_IMPORT_MODE=synthetic") || strings.Contains(s, "authorized") {
		t.Errorf("modalità: %s", s)
	}
	if !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("ambiente dell'app perso: %s", s)
	}
	i := indice(c, "img:1")
	if i < 0 || strings.Join(c[i+1:], " ") != "bin/gestrip_standalone eval GestripStandalone.LegacyImport.Release.main()" {
		t.Fatalf("comando = %v", c[i+1:])
	}
	for dst, voluto := range map[string]struct {
		src string
		ro  bool
	}{
		"/import/capture":         {p.flask, true},
		"/import/output":          {p.uscitaStandalone, false},
		"/run/import/archive_key": {p.chiaveArchivio, true},
		"/data":                   {filepath.Join(n.dir, "data"), false},
		"/data/media":             {filepath.Join(n.dir, "media"), false},
		"/run/secrets":            {filepath.Join(n.dir, "secrets"), true},
	} {
		if src, ro := montaggio(c, dst); src != voluto.src || ro != voluto.ro {
			t.Errorf("%s montato da %q ro=%v, voluto %q ro=%v", dst, src, ro, voluto.src, voluto.ro)
		}
	}
}

// Il decifrato lavora sulla copia: i dati dello standalone non sono nemmeno montati.
func TestComandoDecifraSoloSullaCopia(t *testing.T) {
	n, p := istanzaDiProva(t)
	c := comandoDecifra(n, "img:1", p)
	s := strings.Join(c, " ")
	for _, vietato := range []string{"src=" + filepath.Join(n.dir, "data") + ",", "src=" + filepath.Join(n.dir, "media") + ","} {
		if strings.Contains(s, vietato) {
			t.Errorf("monta i dati originali (%s): %s", vietato, s)
		}
	}
	if src, ro := montaggio(c, "/catena/cifrato"); src != p.cifrato || ro {
		t.Errorf("cifrato %q ro=%v", src, ro)
	}
	if src, ro := montaggio(c, "/catena/chiaro"); src != p.chiaro || ro {
		t.Errorf("chiaro %q ro=%v", src, ro)
	}
	// Solo i due segreti che servono (la chiave e, per runtime.exs, secret_key_base), mai le
	// credenziali degli account.
	if src, _ := montaggio(c, "/run/secrets"); src != "" {
		t.Errorf("monta tutta la cartella dei segreti: %s", src)
	}
	for _, k := range []string{"sqlcipher.key", "secret_key_base"} {
		if src, ro := montaggio(c, "/run/secrets/"+k); src != filepath.Join(n.dir, "secrets", k) || !ro {
			t.Errorf("segreto %s montato da %q ro=%v", k, src, ro)
		}
	}
	if strings.Count(s, "DATABASE_PATH=") != 1 || !strings.Contains(s, "--env DATABASE_PATH=/catena/cifrato/gestrip.db") {
		t.Errorf("DATABASE_PATH non solo sulla copia: %s", s)
	}
	for _, vietato := range []string{"ADMIN_", "admin_", "dipendente", "MEDIA_ROOT", "PHX_SERVER"} {
		if strings.Contains(s, vietato) {
			t.Errorf("la decifratura riceve %s: %s", vietato, s)
		}
	}
	// runtime.exs pretende SECRET_KEY_BASE_FILE e controlla SQLCIPHER_KEY_FILE, che il Dockerfile
	// fissa a un altro nome: tutti e due sui file montati (prova dal vivo del 31/10/2026).
	for _, voluto := range []string{"--env SECRET_KEY_BASE_FILE=/run/secrets/secret_key_base", "--env SQLCIPHER_KEY_FILE=/run/secrets/sqlcipher.key"} {
		if !strings.Contains(s, voluto) {
			t.Errorf("manca %s: %s", voluto, s)
		}
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != decifraStandalone {
		t.Fatalf("comando = %v", c)
	}
	for _, voluto := range []string{"sqlcipher_export('chiaro')", "/catena/cifrato/gestrip.db", "/catena/chiaro/" + nomeChiaro,
		"KEY ''", "Database.load_key!", "DETACH"} {
		if !strings.Contains(decifraStandalone, voluto) {
			t.Errorf("lo script di decifratura non contiene %q", voluto)
		}
	}
}

func TestComandoImportGo(t *testing.T) {
	n, p := istanzaDiProva(t)
	sha := strings.Repeat("b", 64)
	for _, conMiniature := range []bool{true, false} {
		c := comandoImportGo(n, "/x/gestrip", p, sha, 12345, conMiniature)
		i := indice(c, immagineBase)
		if i < 0 || c[i+1] != "/gestrip" || c[i+2] != "import" {
			t.Fatalf("comando = %v", c)
		}
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		sint, senzaVol, senzaMedia := fs.Bool("sintetico", false, ""), fs.Bool("senza-volume-cifrato", false, ""), fs.Bool("senza-media", false, "")
		v := map[string]*string{}
		for _, k := range []string{"sorgente", "sha256", "dimensione", "destinazione", "report", "media", "miniature"} {
			v[k] = fs.String(k, "", "")
		}
		if err := fs.Parse(c[i+3:]); err != nil || fs.NArg() != 0 {
			t.Fatalf("argomenti: %v %v", err, fs.Args())
		}
		if !*sint || !*senzaVol || *senzaMedia || *v["sha256"] != sha || *v["dimensione"] != "12345" ||
			*v["sorgente"] != "/sorgente/"+nomeChiaro || *v["destinazione"] != "/data/gestrip.db" ||
			*v["report"] != "/esito/"+nomeReportGo || *v["media"] != "/media" {
			t.Errorf("flag = %v", c[i+3:])
		}
		if (*v["miniature"] == "/miniature") != conMiniature {
			t.Errorf("miniature con %v: %v", conMiniature, c[i+3:])
		}
		if src, ro := montaggio(c, "/miniature"); (src == p.miniature && ro) != conMiniature {
			t.Errorf("miniature con %v: montate da %q ro=%v", conMiniature, src, ro)
		}
		for dst, voluto := range map[string]struct {
			src string
			ro  bool
		}{
			"/gestrip":  {"/x/gestrip", true},
			"/sorgente": {p.chiaro, true},
			"/media":    {p.media, true},
			"/data":     {filepath.Join(n.dir, "go-data"), false},
			"/esito":    {filepath.Join(n.dir, "esito"), false},
		} {
			if src, ro := montaggio(c, dst); src != voluto.src || ro != voluto.ro {
				t.Errorf("%s montato da %q ro=%v", dst, src, ro)
			}
		}
	}
}

func reportDiProva(sha string) importer.Report {
	return importer.Report{Formato: importer.FormatoReport, Modalita: importer.Sintetico,
		Sorgente: importer.SorgenteReport{Sha256: sha, Byte: 4096},
		Tabelle: []importer.TabellaReport{{Standalone: "repairs", Lette: 4, Scritte: map[string]int64{"buste": 4}},
			{Standalone: "users", Lette: 4, Scritte: map[string]int64{"utenti": 4}},
			{Standalone: "historical_documents", Lette: 2, Scritte: map[string]int64{"documenti_storici": 2}},
			{Standalone: "sales", Lette: 1, Scritte: map[string]int64{"vendite": 1}},
			{Standalone: "sale_revisions", Lette: 1, Scritte: map[string]int64{"revisioni_vendite": 1}},
			{Standalone: "sale_photos", Lette: 1, Scritte: map[string]int64{"foto_vendite": 1}},
			{Standalone: "sale_revision_photos", Lette: 1, Scritte: map[string]int64{"foto_revisioni_vendite": 1}},
			{Standalone: "sale_revision_seals", Lette: 1, Scritte: map[string]int64{"sigilli_revisioni_vendite": 1}}},
		Avvisi: append([]importer.Voce{avvisoTesto("historical_documents", 2), avvisoTesto("sale_photos", 1),
			avvisoTesto("sale_revision_photos", 1), {Codice: "altro/avviso", Tabella: "repairs"}, avvisoSenzaZona(1)},
			avvisiEmergenza(2, 1, 1)...),
		Media: &importer.MediaReport{Importati: true, FileCopiati: 5, ByteCopiati: 200, Famiglie: []importer.FamigliaMedia{
			{Tabella: "repair_photos", Colonna: "storage_key", Attesi: 3, Copiati: 3, Byte: 60},
			{Tabella: "sale_photos", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: 70},
			{Tabella: "sale_revision_photos", Colonna: "storage_key", Attesi: 1, Copiati: 1, Byte: 70}}},
		Verdetto: importer.Riconciliato, Istante: "2026-10-31T10:00:00Z"}
}

func avvisoTesto(tabella string, righe int64) importer.Voce {
	return importer.Voce{Codice: "tipo/impronta_testo", Tabella: tabella, Colonna: "sha256", Righe: &righe}
}

func avvisoSenzaZona(righe int64) importer.Voce {
	return importer.Voce{Codice: "traduzione/istante_senza_zona", Tabella: "sale_revision_photos", Colonna: "inserted_at", Righe: &righe}
}

// conteggiVendita sono i conteggi che il passo della vendita legge nello standalone.
func conteggiVendita() map[string]int64 {
	return map[string]int64{"sales": 1, "sale_revisions": 1, "sale_photos": 1, "sale_revision_photos": 1, "sale_revision_seals": 1}
}

func TestLeggiReportImport(t *testing.T) {
	sha := strings.Repeat("c", 64)
	codifica := func(r importer.Report) []byte {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	att := sorgenteGo{sha: sha, dimensione: 4096, fileMedia: 5, byteMedia: 200, mediaVendita: 1, conteggiVendita: conteggiVendita(),
		emergenza: esitoEmergenzaDiProva()}
	r, err := leggiReportImport(codifica(reportDiProva(sha)), att)
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdetto != importer.Riconciliato || r.Lette["repairs"] != 4 || r.Scritte["utenti"] != 4 || r.FileMedia != 5 || r.ByteMedia != 200 {
		t.Errorf("riassunto = %+v", r)
	}
	if r.Avvisi != 10 || r.IstantiSenzaZona["sale_revision_photos.inserted_at"] != 1 || len(r.IstantiSenzaZona) != 6 ||
		r.Emergenza != (riassuntoEmergenza{Righe: 2, EventiChiusi: 1, BusteScansionate: 1}) || r.MediaVendita != 1 || len(r.ImpronteTesto) != 3 || r.ImpronteTesto["historical_documents"] != 2 ||
		r.ImpronteTesto["sale_photos"] != 1 || r.ImpronteTesto["sale_revision_photos"] != 1 {
		t.Errorf("riassunto delle impronte = %+v", r)
	}
	// Con più vendite nella sorgente (domani nella cattura) le attese vengono dal passo della vendita.
	piu := reportDiProva(sha)
	for i := range piu.Tabelle {
		if piu.Tabelle[i].Standalone == "sale_photos" {
			piu.Tabelle[i].Lette, piu.Tabelle[i].Scritte["foto_vendite"] = 3, 3
		}
	}
	piu.Media.Famiglie[1].Attesi, piu.Media.Famiglie[1].Copiati = 3, 3
	piu.Avvisi[1] = avvisoTesto("sale_photos", 3)
	att3 := att
	att3.conteggiVendita = conteggiVendita()
	att3.conteggiVendita["sale_photos"] = 3
	if _, err := leggiReportImport(codifica(piu), att3); err != nil {
		t.Errorf("tre foto di vendita attese e trovate: %v", err)
	}
	if _, err := leggiReportImport(codifica(piu), att); err == nil {
		t.Error("tre foto di vendita lette, una attesa: accettato")
	}
	// Una tabella delle vendite che manca sia nelle attese sia nel report non passa per «zero
	// righe uguali»: ne serve almeno una.
	senza := reportDiProva(sha)
	senza.Tabelle = append(senza.Tabelle[:3], senza.Tabelle[4:]...) // via sales
	attSenza := att
	attSenza.conteggiVendita = conteggiVendita()
	delete(attSenza.conteggiVendita, "sales")
	if _, err := leggiReportImport(codifica(senza), attSenza); err == nil {
		t.Error("report senza vendite e nessuna vendita attesa: accettato")
	}
	uno := int64(7)
	casi := map[string]func(*importer.Report){
		"bloccato": func(r *importer.Report) {
			r.Verdetto = importer.Bloccato
			r.Differenze = []importer.Voce{{Codice: "tabella/sconosciuta", Tabella: "x", Righe: &uno}}
		},
		"differenze con riconciliato": func(r *importer.Report) { r.Differenze = []importer.Voce{{Codice: "x"}} },
		"altra impronta":              func(r *importer.Report) { r.Sorgente.Sha256 = strings.Repeat("d", 64) },
		"autorizzato":                 func(r *importer.Report) { r.Modalita = importer.Autorizzato },
		"senza media":                 func(r *importer.Report) { r.Media = nil },
		"media non importati":         func(r *importer.Report) { r.Media.Importati = false },
		"formato":                     func(r *importer.Report) { r.Formato = 99 },
		"nessuna tabella":             func(r *importer.Report) { r.Tabelle = nil },
		"nessun file copiato":         func(r *importer.Report) { r.Media.FileCopiati, r.Media.ByteCopiati = 0, 0 },
		"un file in meno":             func(r *importer.Report) { r.Media.FileCopiati = 4 },
		"byte dei media diversi":      func(r *importer.Report) { r.Media.ByteCopiati = 199 },
		"file in più":                 func(r *importer.Report) { r.Media.InPiu = 1 },
		"dimensione della sorgente":   func(r *importer.Report) { r.Sorgente.Byte = 4095 },
		"senza avviso sale_photos":    func(r *importer.Report) { r.Avvisi = append(r.Avvisi[:1], r.Avvisi[2:]...) },
		"senza avviso sale_revision_photos": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi[:2], r.Avvisi[3:]...)
		},
		"senza avviso historical_documents": func(r *importer.Report) { r.Avvisi = r.Avvisi[1:] },
		"senza avvisi":                      func(r *importer.Report) { r.Avvisi = nil },
		"avviso con meno righe":             func(r *importer.Report) { r.Avvisi[0] = avvisoTesto("historical_documents", 1) },
		"avviso con più righe":              func(r *importer.Report) { r.Avvisi[1] = avvisoTesto("sale_photos", 2) },
		"avviso senza righe": func(r *importer.Report) {
			r.Avvisi[2] = importer.Voce{Codice: "tipo/impronta_testo", Tabella: "sale_revision_photos", Colonna: "sha256"}
		},
		"avviso su un'altra colonna": func(r *importer.Report) {
			r.Avvisi[1].Colonna = "storage_key"
		},
		"avviso doppio": func(r *importer.Report) { r.Avvisi = append(r.Avvisi, avvisoTesto("sale_photos", 1)) },
		"altro codice":  func(r *importer.Report) { r.Avvisi[2].Codice = "tipo/altro" },
		"avviso su una tabella in più": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi, avvisoTesto("repairs", 4))
		},
		"documenti storici vuoti con avviso a zero righe": func(r *importer.Report) {
			r.Tabelle[2].Lette, r.Tabelle[2].Scritte = 0, nil
			r.Avvisi[0] = avvisoTesto("historical_documents", 0)
		},
		"vendite lette diverse": func(r *importer.Report) {
			r.Tabelle[3].Lette = 2
		},
		"revisioni scritte diverse": func(r *importer.Report) {
			r.Tabelle[4].Scritte = map[string]int64{"revisioni_vendite": 0}
		},
		"foto scritte in un'altra tabella": func(r *importer.Report) {
			r.Tabelle[5].Scritte = map[string]int64{"altro": 1}
		},
		"foto di revisione assenti": func(r *importer.Report) {
			r.Tabelle = r.Tabelle[:6]
			r.Avvisi = append(r.Avvisi[:2], r.Avvisi[3:]...)
		},
		"sigilli scritti diversi": func(r *importer.Report) { r.Tabelle[7].Scritte["sigilli_revisioni_vendite"] = 0 },
		"sigilli assenti":         func(r *importer.Report) { r.Tabelle = r.Tabelle[:7] },
		// Le famiglie media delle foto di vendita: saltate o incomplete, anche a totali invariati.
		"famiglia sale_photos saltata": func(r *importer.Report) {
			r.Media.Famiglie = append(r.Media.Famiglie[:1], r.Media.Famiglie[2:]...)
		},
		"famiglia sale_revision_photos saltata": func(r *importer.Report) { r.Media.Famiglie = r.Media.Famiglie[:2] },
		"famiglia con un attesa in meno":        func(r *importer.Report) { r.Media.Famiglie[1].Attesi = 0 },
		"famiglia con un copiato in meno":       func(r *importer.Report) { r.Media.Famiglie[2].Copiati = 0 },
		"famiglia con due attesi e due copiati": func(r *importer.Report) {
			r.Media.Famiglie[2].Attesi, r.Media.Famiglie[2].Copiati = 2, 2
		},
		"famiglia con un mancante":     func(r *importer.Report) { r.Media.Famiglie[1].Mancanti = 1 },
		"famiglia con un hash diverso": func(r *importer.Report) { r.Media.Famiglie[2].HashDiversi = 1 },
		"famiglia doppia": func(r *importer.Report) {
			r.Media.Famiglie = append(r.Media.Famiglie, r.Media.Famiglie[1])
		},
		"documenti storici assenti": func(r *importer.Report) {
			r.Tabelle = append(r.Tabelle[:2], r.Tabelle[3:]...)
			r.Avvisi = r.Avvisi[1:]
		},
	}
	for nome, guasta := range casi {
		rep := reportDiProva(sha)
		guasta(&rep)
		if _, err := leggiReportImport(codifica(rep), att); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	// Un report bloccato fa vedere i codici delle differenze, mai valori.
	rep := reportDiProva(sha)
	casi["bloccato"](&rep)
	if _, err := leggiReportImport(codifica(rep), att); err == nil || !strings.Contains(err.Error(), "tabella/sconosciuta") {
		t.Errorf("errore del bloccato senza il codice: %v", err)
	}
	if _, err := leggiReportImport([]byte("{non json"), att); err == nil {
		t.Error("JSON rotto accettato")
	}
	if _, err := leggiReportImport(append(codifica(reportDiProva(sha)), []byte(`{"x":1}`)...), att); err == nil {
		t.Error("dati in coda accettati")
	}
}

func TestEsitoImportStandalone(t *testing.T) {
	ok := "avvio\nGR_IMPORT:{\"verdict\":\"reconciled\",\"mode\":\"synthetic\",\"sha256\":\"ab\"}\n"
	if err := esitoImportStandalone(ok); err != nil {
		t.Errorf("riconciliato rifiutato: %v", err)
	}
	for nome, out := range map[string]string{
		"senza riga":     "niente\n",
		"bloccato":       "GR_IMPORT:{\"verdict\":\"blocked\",\"mode\":\"synthetic\"}\n",
		"autorizzato":    "GR_IMPORT:{\"verdict\":\"reconciled\",\"mode\":\"authorized\"}\n",
		"due righe":      ok + ok,
		"json rotto":     "GR_IMPORT:{verdict\n",
		"riga nel mezzo": "x GR_IMPORT:{\"verdict\":\"reconciled\",\"mode\":\"synthetic\"}\n",
	} {
		if err := esitoImportStandalone(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

func TestInChiaro(t *testing.T) {
	dir := t.TempDir()
	for nome, c := range map[string]struct {
		dati   string
		chiaro bool
	}{"chiaro": {"SQLite format 3\x00" + strings.Repeat("x", 100), true}, "cifrato": {strings.Repeat("\x9a", 116), false},
		"corto": {"SQLite format", false}, "vuoto": {"", false}} {
		p := filepath.Join(dir, nome)
		if err := os.WriteFile(p, []byte(c.dati), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := inChiaro(p)
		if err != nil || got != c.chiaro {
			t.Errorf("%s: %v, %v", nome, got, err)
		}
	}
	if _, err := inChiaro(filepath.Join(dir, "manca")); err == nil {
		t.Error("file mancante accettato")
	}
}

func TestCopiaAlbero(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	for p, d := range map[string]string{"ab/cd/f1": "uno", "f2": "due!", "vuota/.tieni": ""} {
		if err := os.MkdirAll(filepath.Join(src, filepath.Dir(p)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, p), []byte(d), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(dir, "dst")
	file, byte_, err := copiaAlbero(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if file != 3 || byte_ != 7 {
		t.Errorf("file %d byte %d", file, byte_)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "ab/cd/f1")); err != nil || string(b) != "uno" {
		t.Errorf("f1 = %q %v", b, err)
	}
	if fi, err := os.Stat(filepath.Join(dst, "f2")); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("permessi di f2: %v %v", fi, err)
	}
	if _, _, err := copiaAlbero(src, dst); err == nil {
		t.Error("destinazione esistente sovrascritta")
	}
	// Un collegamento o un file speciale nella sorgente ferma la copia.
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := copiaAlbero(src, filepath.Join(dir, "dst2")); err == nil {
		t.Error("collegamento copiato")
	}
	if err := os.Remove(filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(src, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := copiaAlbero(src, filepath.Join(dir, "dst3")); err == nil {
		t.Error("fifo copiata")
	}
	if _, _, err := copiaAlbero(filepath.Join(dir, "manca"), filepath.Join(dir, "dst4")); err == nil {
		t.Error("sorgente mancante accettata")
	}
}

func TestCopiaDatabaseCifrato(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	scrivi := func(n, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(data, n), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := copiaDatabaseCifrato(data, filepath.Join(dir, "c0")); err == nil {
		t.Error("database mancante accettato")
	}
	scrivi("gestrip.db", "principale")
	scrivi("gestrip.db-wal", "wal")
	scrivi("gestrip.db-shm", "shm")
	scrivi("altro", "x")
	dst := filepath.Join(dir, "c1")
	if err := copiaDatabaseCifrato(data, dst); err != nil {
		t.Fatal(err)
	}
	for n, voluto := range map[string]string{"gestrip.db": "principale", "gestrip.db-wal": "wal"} {
		if b, err := os.ReadFile(filepath.Join(dst, n)); err != nil || string(b) != voluto {
			t.Errorf("%s = %q %v", n, b, err)
		}
	}
	for _, n := range []string{"gestrip.db-shm", "altro"} {
		if _, err := os.Lstat(filepath.Join(dst, n)); err == nil {
			t.Errorf("%s copiato", n)
		}
	}
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("cartella della copia: %v %v", fi, err)
	}
	// Un journal accanto vuol dire una transazione a metà: niente copia.
	scrivi("gestrip.db-journal", "j")
	if err := copiaDatabaseCifrato(data, filepath.Join(dir, "c2")); err == nil {
		t.Error("journal accettato")
	}
}

func TestDentroIstanza(t *testing.T) {
	n, p := istanzaDiProva(t)
	for _, ok := range []string{p.flask, p.chiaro, filepath.Join(n.dir, "esito", "x")} {
		if err := dentroIstanza(n, ok); err != nil {
			t.Errorf("%s rifiutato: %v", ok, err)
		}
	}
	for _, no := range []string{n.dir, filepath.Dir(n.dir), filepath.Join(n.dir, "..", "altro"), n.dir + "/a/../../altro", n.dir + "/a//b",
		n.dir + "x/a", "relativo/a", "/tmp/a"} {
		if err := dentroIstanza(n, no); err == nil {
			t.Errorf("%s accettato", no)
		}
	}
	// I percorsi della catena stanno tutti dentro l'istanza.
	for _, x := range []string{p.dir, p.script, p.flask, p.chiaveArchivio, p.uscitaStandalone, p.foto, p.cifrato, p.chiaro, p.media, p.miniature} {
		if err := dentroIstanza(n, x); err != nil {
			t.Errorf("percorso della catena %s: %v", x, err)
		}
	}
}

// copiaFile da sola non segue un collegamento, nemmeno verso un file regolare.
func TestCopiaFileNonSegueICollegamenti(t *testing.T) {
	dir := t.TempDir()
	vero := filepath.Join(dir, "vero")
	if err := os.WriteFile(vero, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vero, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := copiaFile(filepath.Join(dir, "link"), filepath.Join(dir, "copia")); err == nil {
		t.Error("collegamento seguito")
	}
	if n, err := copiaFile(vero, filepath.Join(dir, "copia2")); err != nil || n != 1 {
		t.Errorf("copia del file vero: %d %v", n, err)
	}
}
