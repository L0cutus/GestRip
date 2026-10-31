package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"reflect"
	"strings"
	"testing"

	"github.com/L0cutus/gestrip/internal/importer"
)

// Il passo della vendita gira come il legacy import: immagine dello standalone, senza rete, con
// le protezioni e i dati dell'istanza, più la foto sintetica in sola lettura.
func TestComandoVendita(t *testing.T) {
	n, p := istanzaDiProva(t)
	c := comandoVendita(n, "img:1", []string{"--env", "DATABASE_PATH=/data/gestrip.db"}, p)
	s := strings.Join(c, " ")
	if valoreDopo(c, "--name") != n.base+"-"+passoVendita || valoreDopo(c, "--network") != "none" {
		t.Errorf("nome o rete: %s", s)
	}
	for _, voluto := range protezioniStandalone(n) {
		if indice(c, voluto) < 0 {
			t.Errorf("manca la protezione %q: %s", voluto, s)
		}
	}
	if !strings.Contains(s, strings.Join(protezioniStandalone(n), " ")) {
		t.Errorf("protezioni diverse da quelle dei passi vicini: %s", s)
	}
	if !strings.Contains(s, "--env DATABASE_PATH=/data/gestrip.db") {
		t.Errorf("ambiente dell'app perso: %s", s)
	}
	for _, vietato := range []string{"GESTRIP_IMPORT", "/import/", "PHX_SERVER", "-d "} {
		if strings.Contains(s, vietato) {
			t.Errorf("contiene %q: %s", vietato, s)
		}
	}
	for dst, voluto := range map[string]struct {
		src string
		ro  bool
	}{
		"/data":            {n.dir + "/data", false},
		"/data/media":      {n.dir + "/media", false},
		"/run/secrets":     {n.dir + "/secrets", true},
		"/catena/foto.jpg": {p.foto, true},
	} {
		if src, ro := montaggio(c, dst); src != voluto.src || ro != voluto.ro {
			t.Errorf("%s montato da %q ro=%v, voluto %q ro=%v", dst, src, ro, voluto.src, voluto.ro)
		}
	}
	i := indice(c, "img:1")
	if i < 0 || len(c) != i+4 || c[i+1] != "bin/gestrip_standalone" || c[i+2] != "eval" || c[i+3] != venditaStandalone {
		t.Fatalf("comando = %v", c)
	}
}

// Lo script chiama le stesse funzioni della pagina delle vendite, con il contesto d'audit della
// pagina (solo actor), e avvia l'applicazione come il legacy import (senza HTTP né lavori).
func TestScriptVenditaComeLaPagina(t *testing.T) {
	for _, voluto := range []string{
		"GestripStandalone.LegacyImport.Release.quiet!()",
		"Application.ensure_all_started(:gestrip_standalone)",
		"\naudit = %{actor: \"/run/secrets/admin_email\" |> File.read!() |> String.trim_trailing()}\n",
		"SalePhotos.attach(bozza, \"/catena/foto.jpg\", audit)",
		"OnlineSales.issue_sale(bozza, audit)",
		"~w(sales sale_revisions sale_photos sale_revision_photos sale_revision_seals)",
		"\"GR_VENDITA:\"",
		"System.halt(1)",
	} {
		if !strings.Contains(venditaStandalone, voluto) {
			t.Errorf("lo script della vendita non contiene %q", voluto)
		}
	}
	// Il contesto d'audit della pagina va a tutte e tre le funzioni, e a nessun'altra.
	if n := strings.Count(venditaStandalone, ", audit)"); n != 3 {
		t.Errorf("%d chiamate con il contesto d'audit, ne servono 3", n)
	}
	// Le tre funzioni della pagina, nell'ordine della pagina.
	bozza := strings.Index(venditaStandalone, "OnlineSales.create_draft(%{")
	foto := strings.Index(venditaStandalone, "SalePhotos.attach(")
	emissione := strings.Index(venditaStandalone, "OnlineSales.issue_sale(")
	if bozza < 0 || !(bozza < foto && foto < emissione) {
		t.Errorf("ordine: create_draft %d, attach %d, issue_sale %d", bozza, foto, emissione)
	}
	// Mai una scrittura a mano, mai un contesto vuoto, niente profilo scritto dal banco: il profilo
	// del negozio viene dal legacy import, e se manca issue_sale fallisce.
	for _, vietato := range []string{"INSERT", "UPDATE ", "DELETE", "%{})", ", %{}", "SHOP_PROFILE"} {
		if strings.Contains(strings.ToUpper(venditaStandalone), strings.ToUpper(vietato)) {
			t.Errorf("lo script della vendita contiene %q", vietato)
		}
	}
}

func TestFotoSintetica(t *testing.T) {
	b, err := fotoSintetica()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}) || len(b) > 64<<10 {
		t.Fatalf("non è un piccolo JPEG: %d byte, %x", len(b), b[:min(4, len(b))])
	}
	cfg, formato, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || formato != "jpeg" || cfg.Width < 16 || cfg.Height < 16 {
		t.Errorf("config = %+v %q %v", cfg, formato, err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("JPEG non decodificabile: %v", err)
	}
}

func rigaVendita(chiave string, conteggi string) string {
	return "GR_VENDITA:{\"esito\":\"ok\",\"vendita\":1,\"revisione\":1,\"chiave\":\"" + chiave + "\",\"conteggi\":" + conteggi + "}\n"
}

const conteggiUno = `{"sales":1,"sale_revisions":1,"sale_photos":1,"sale_revision_photos":1,"sale_revision_seals":1}`

func TestLeggiEsitoVendita(t *testing.T) {
	chiave := strings.Repeat("ab", 32)
	ok := "avvio\n" + rigaVendita(chiave, conteggiUno)
	e, err := leggiEsitoVendita(ok)
	if err != nil {
		t.Fatalf("esito buono rifiutato: %v", err)
	}
	if e.Chiave != chiave || len(e.Conteggi) != 5 || e.Conteggi["sale_revision_photos"] != 1 {
		t.Errorf("esito = %+v", e)
	}
	for nome, out := range map[string]string{
		"senza riga":                 "fatto\n",
		"esito errore":               "GR_VENDITA:{\"esito\":\"errore\",\"motivo\":\"{:error, :shop_profile_missing}\"}\n",
		"due righe":                  ok + ok,
		"json rotto":                 "GR_VENDITA:{esito\n",
		"dati in coda":               strings.TrimSuffix(rigaVendita(chiave, conteggiUno), "\n") + "{}\n",
		"riga nel mezzo":             "x " + rigaVendita(chiave, conteggiUno),
		"chiave corta":               rigaVendita("abcd", conteggiUno),
		"chiave maiuscola":           rigaVendita(strings.Repeat("AB", 32), conteggiUno),
		"chiave non hex":             rigaVendita(strings.Repeat("zz", 32), conteggiUno),
		"senza revisioni":            rigaVendita(chiave, `{"sales":1,"sale_revisions":0,"sale_photos":1,"sale_revision_photos":1,"sale_revision_seals":1}`),
		"manca un conteggio":         rigaVendita(chiave, `{"sales":1,"sale_revisions":1,"sale_photos":1}`),
		"foto di revisione negativa": rigaVendita(chiave, `{"sales":1,"sale_revisions":1,"sale_photos":1,"sale_revision_photos":-1,"sale_revision_seals":1}`),
		"senza sigillo":              rigaVendita(chiave, `{"sales":1,"sale_revisions":1,"sale_photos":1,"sale_revision_photos":1,"sale_revision_seals":0}`),
	} {
		if _, err := leggiEsitoVendita(out); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
	// L'errore porta il motivo dello standalone, per la diagnosi.
	if _, err := leggiEsitoVendita("GR_VENDITA:{\"esito\":\"errore\",\"motivo\":\"shop_profile_missing\"}\n"); err == nil ||
		!strings.Contains(err.Error(), "shop_profile_missing") {
		t.Errorf("errore senza il motivo: %v", err)
	}
}

// Il riassunto stampa il verdetto, gli avvisi delle impronte salvate come testo, le righe delle
// vendite lette e scritte e i media in più della vendita.
func TestRigheRiassunto(t *testing.T) {
	r := riassuntoImport{Verdetto: "riconciliato", Avvisi: 4, FileMedia: 6, ByteMedia: 900, MediaVendita: 1,
		IstantiSenzaZona: map[string]int64{"sale_revision_photos.inserted_at": 5, "repairs.updated_at": 2},
		ImpronteTesto:    map[string]int64{"historical_documents": 2, "sale_photos": 3, "sale_revision_photos": 5},
		Lette:            map[string]int64{"sales": 7, "sale_revisions": 11, "sale_photos": 3, "sale_revision_photos": 5, "sale_revision_seals": 29, "repairs": 4},
		Scritte:          map[string]int64{"vendite": 13, "revisioni_vendite": 17, "foto_vendite": 19, "foto_revisioni_vendite": 23, "sigilli_revisioni_vendite": 31, "buste": 4}}
	s := strings.Join(righeRiassunto(r), "\n")
	for _, voluto := range []string{
		"import Go riconciliato, 4 avvisi, media 6 file 900 byte (1 in più dalla vendita)",
		"tipo/impronta_testo: historical_documents=2 sale_photos=3 sale_revision_photos=5",
		"traduzione/istante_senza_zona: repairs.updated_at=2 sale_revision_photos.inserted_at=5",
		"sales=7→vendite=13", "sale_revisions=11→revisioni_vendite=17", "sale_photos=3→foto_vendite=19",
		"sale_revision_photos=5→foto_revisioni_vendite=23", "sale_revision_seals=29→sigilli_revisioni_vendite=31",
		"lette: repairs=4", "scritte: buste=4",
	} {
		if !strings.Contains(s, voluto) {
			t.Errorf("manca %q in:\n%s", voluto, s)
		}
	}
}

// issue_sale scrive sale_revision_photos.inserted_at senza zona (decisione del 31/10/2026): il
// report dell'import Go deve avere un solo avviso traduzione/istante_senza_zona su quella colonna,
// senza id, per tutte le foto di revisione che il passo della vendita ha contato nello standalone.
// Un avviso su un'altra colonna decisa passa e si riporta (il registro d'emergenza arriva con la
// slice 028); su una colonna non decisa, doppio, con un id o senza righe ferma la catena.
func TestControllaVenditaIstantiSenzaZona(t *testing.T) {
	sha := strings.Repeat("c", 64)
	att := sorgenteGo{sha: sha, conteggiVendita: conteggiVendita()}
	if _, n, err := controllaVendita(reportDiProva(sha), att); err != nil || !reflect.DeepEqual(n, map[string]int64{"sale_revision_photos.inserted_at": 1}) {
		t.Fatalf("report buono: righe %v, %v", n, err)
	}
	// Più foto di revisione nello standalone: l'avviso deve contarle tutte.
	piu := reportDiProva(sha)
	piu.Tabelle[6].Lette, piu.Tabelle[6].Scritte["foto_revisioni_vendite"] = 3, 3
	piu.Media.Famiglie[2].Attesi, piu.Media.Famiglie[2].Copiati = 3, 3
	piu.Avvisi[2], piu.Avvisi[4] = avvisoTesto("sale_revision_photos", 3), avvisoSenzaZona(3)
	att3 := att
	att3.conteggiVendita = conteggiVendita()
	att3.conteggiVendita["sale_revision_photos"] = 3
	if _, n, err := controllaVendita(piu, att3); err != nil || n["sale_revision_photos.inserted_at"] != 3 {
		t.Errorf("tre foto di revisione: righe %v, %v", n, err)
	}
	piu.Avvisi[4] = avvisoSenzaZona(2)
	if _, _, err := controllaVendita(piu, att3); err == nil {
		t.Error("avviso per 2 righe su 3 foto di revisione: accettato")
	}
	// Le altre colonne decise passano e si riportano con le loro righe.
	altre := reportDiProva(sha)
	altre.Avvisi = append(altre.Avvisi, avvisoIstante("repairs", "updated_at", 4), avvisoIstante("emergency_events", "closed_at", 1))
	want := map[string]int64{"sale_revision_photos.inserted_at": 1, "repairs.updated_at": 4, "emergency_events.closed_at": 1}
	if _, n, err := controllaVendita(altre, att); err != nil || !reflect.DeepEqual(n, want) {
		t.Errorf("altre colonne decise: righe %v, %v", n, err)
	}
	uno := int64(1)
	for nome, guasta := range map[string]func(*importer.Report){
		"senza avviso":           func(r *importer.Report) { r.Avvisi = r.Avvisi[:4] },
		"avviso doppio":          func(r *importer.Report) { r.Avvisi = append(r.Avvisi, avvisoSenzaZona(1)) },
		"avviso con un id":       func(r *importer.Report) { r.Avvisi[4].Id = &uno },
		"avviso senza righe":     func(r *importer.Report) { r.Avvisi[4].Righe = nil },
		"avviso a zero righe":    func(r *importer.Report) { r.Avvisi[4] = avvisoSenzaZona(0) },
		"avviso con più righe":   func(r *importer.Report) { r.Avvisi[4] = avvisoSenzaZona(2) },
		"avviso su sale_photos":  func(r *importer.Report) { r.Avvisi[4].Tabella = "sale_photos" },
		"avviso su un'altra col": func(r *importer.Report) { r.Avvisi[4].Colonna = "sha256" },
		"in più su una colonna non decisa": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi, avvisoIstante("customers", "updated_at", 1))
		},
		"in più su repairs.delivered_on": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi, avvisoIstante("repairs", "delivered_on", 1))
		},
		"colonna decisa doppia": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi, avvisoIstante("repairs", "updated_at", 1), avvisoIstante("repairs", "updated_at", 1))
		},
		"colonna decisa senza righe": func(r *importer.Report) {
			v := avvisoIstante("repairs", "updated_at", 1)
			v.Righe = nil
			r.Avvisi = append(r.Avvisi, v)
		},
		"colonna decisa a zero righe": func(r *importer.Report) {
			r.Avvisi = append(r.Avvisi, avvisoIstante("emergency_entries", "inserted_at", 0))
		},
		"colonna decisa con un id": func(r *importer.Report) {
			v := avvisoIstante("emergency_entries", "inserted_at", 1)
			v.Id = &uno
			r.Avvisi = append(r.Avvisi, v)
		},
	} {
		rep := reportDiProva(sha)
		guasta(&rep)
		if _, _, err := controllaVendita(rep, att); err == nil {
			t.Errorf("%s: accettato", nome)
		}
	}
}

// avvisoIstante è un avviso traduzione/istante_senza_zona su una colonna qualsiasi.
func avvisoIstante(tabella, colonna string, righe int64) importer.Voce {
	return importer.Voce{Codice: "traduzione/istante_senza_zona", Tabella: tabella, Colonna: colonna, Righe: &righe}
}
