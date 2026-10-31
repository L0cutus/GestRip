package main

// Il passo della vendita (slice 026): dopo il legacy import, nello standalone ancora fermo, una
// vendita con una foto caricata e la sua revisione, fatte con le stesse funzioni che chiama la
// pagina delle vendite (sales_live.ex a go-baseline: OnlineSales.create_draft/2,
// SalePhotos.attach/3, OnlineSales.issue_sale/2, contesto d'audit %{actor: email}). Lo standalone
// salva sale_photos.sha256 come TEXT e issue_sale lo copia in sale_revision_photos (D-42):
// l'import Go deve accettarle con l'avviso tipo/impronta_testo, e la catena lo controlla.
// issue_sale scrive anche sale_revision_photos.inserted_at senza zona (decisione del 31/10/2026):
// l'import Go lo accetta come UTC con l'avviso traduzione/istante_senza_zona, controllato allo
// stesso modo.

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/L0cutus/gestrip/internal/importer"
)

// tabelleVendita sono le tabelle che il passo della vendita riempie, con la tabella Go in cui
// l'import le scrive.
var tabelleVendita = []struct{ standalone, goTab string }{
	{"sales", "vendite"},
	{"sale_revisions", "revisioni_vendite"},
	{"sale_photos", "foto_vendite"},
	{"sale_revision_photos", "foto_revisioni_vendite"},
	{"sale_revision_seals", "sigilli_revisioni_vendite"},
}

// famiglieVendita sono le famiglie media del report che nominano la foto della vendita.
var famiglieVendita = []string{"sale_photos", "sale_revision_photos"}

// tabelleImprontaTesto sono le tabelle in cui lo standalone salva l'impronta come TEXT (D-42):
// l'import Go deve avvisare su ognuna, per tutte le sue righe.
var tabelleImprontaTesto = []string{"historical_documents", "sale_photos", "sale_revision_photos"}

const codiceImprontaTesto = "tipo/impronta_testo"

// L'istante senza zona che issue_sale scrive in ogni foto di revisione: l'import Go deve avvisare
// una volta, su questa colonna, per tutte le foto di revisione dello standalone.
const (
	codiceIstanteSenzaZona = "traduzione/istante_senza_zona"
	istanteVendita         = "sale_revision_photos.inserted_at"
)

// colonneIstanteSenzaZona sono le colonne (tabella.colonna dello standalone) dove Stefano ha deciso
// il 31/10/2026 che l'import accetta l'istante senza zona: l'elenco dell'importer, ripetuto qui
// apposta come controllo indipendente. Un avviso su un'altra colonna ferma la catena.
var colonneIstanteSenzaZona = map[string]bool{
	istanteVendita:                  true,
	"emergency_entries.inserted_at": true,
	"emergency_events.closed_at":    true,
	"emergency_events.updated_at":   true,
	"repairs.retention_started_at":  true,
	"repairs.updated_at":            true,
}

// venditaStandalone avvia l'applicazione come il legacy import (Release.quiet!/0: niente HTTP,
// niente Chromium, niente lavori in sottofondo) e fa ciò che fa un titolare dalla pagina delle
// vendite, con le sole tre funzioni della pagina. Il profilo del negozio che issue_sale vuole
// viene dal legacy import (ensure_profile in legacy_import/operational.ex crea «Negozio»): se
// manca, issue_sale dà {:error, :shop_profile_missing} e il banco si ferma con il motivo.
// Tutti i dati sono inventati. L'esito è una sola riga GR_VENDITA: con i conteggi delle tabelle
// delle vendite letti nello standalone; un errore esce con stato 1.
const venditaStandalone = `GestripStandalone.LegacyImport.Release.quiet!()
{:ok, _} = Application.ensure_all_started(:gestrip_standalone)
alias GestripStandalone.{OnlineSales, Repo, SalePhotos}
audit = %{actor: "/run/secrets/admin_email" |> File.read!() |> String.trim_trailing()}
conta = fn tabella -> %{rows: [[n]]} = Repo.query!("SELECT count(*) FROM " <> tabella); n end
risultato =
  with {:ok, bozza} <- OnlineSales.create_draft(%{"sale_on" => "2026-10-15", "channel" => "eBay",
         "item_description" => "Orologio sintetico di prova", "serial_number" => "SINT-0001", "price_euros" => "120,50",
         "warranty_months" => "12", "buyer_display_name" => "Acquirente Sintetico", "buyer_email" => "acquirente@example.invalid",
         "buyer_city" => "Paese di Prova"}, audit),
       {:ok, %{storage_key: chiave}} <- SalePhotos.attach(bozza, "/catena/foto.jpg", audit),
       {:ok, emessa, revisione, _token} <- OnlineSales.issue_sale(bozza, audit) do
    %{esito: "ok", vendita: emessa.id, revisione: revisione.id, chiave: chiave,
      conteggi: Map.new(~w(sales sale_revisions sale_photos sale_revision_photos sale_revision_seals), &{&1, conta.(&1)})}
  else
    errore -> %{esito: "errore", motivo: inspect(errore)}
  end
IO.puts("GR_VENDITA:" <> Jason.encode!(risultato))
if risultato.esito != "ok", do: System.halt(1)`

// comandoVendita gira come il legacy import: stessa immagine, senza rete, stesse protezioni e
// stessi dati dell'istanza, più la foto sintetica in sola lettura (al posto del file temporaneo
// del caricamento della pagina).
func comandoVendita(n nomi, img string, env []string, p percorsiCatena) []string {
	a := append(passoSenzaRete(n, passoVendita), protezioniStandalone(n)...)
	a = append(a, montaggiStandalone(n)...)
	a = append(a, monta(p.foto, "/catena/foto.jpg", true)...)
	a = append(a, env...)
	return append(a, img, "bin/gestrip_standalone", "eval", venditaStandalone)
}

// righeMarcatore sono le righe che cominciano con il marcatore, senza il marcatore.
func righeMarcatore(out, marcatore string) []string {
	var righe []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, marcatore) {
			righe = append(righe, strings.TrimPrefix(l, marcatore))
		}
	}
	return righe
}

// esitoVendita è la riga GR_VENDITA: del passo della vendita.
type esitoVendita struct {
	Esito    string           `json:"esito"`
	Motivo   string           `json:"motivo"`
	Chiave   string           `json:"chiave"`
	Conteggi map[string]int64 `json:"conteggi"`
}

// leggiEsitoVendita vuole una sola riga GR_VENDITA: riuscita, con la chiave della foto (l'impronta
// esadecimale minuscola, come la scrive MediaStore) e almeno una riga in ogni tabella delle
// vendite.
func leggiEsitoVendita(out string) (esitoVendita, error) {
	righe := righeMarcatore(out, "GR_VENDITA:")
	if len(righe) != 1 {
		return esitoVendita{}, fmt.Errorf("vendita nello standalone: %d righe GR_VENDITA, ne serve una", len(righe))
	}
	var e esitoVendita
	dec := json.NewDecoder(strings.NewReader(righe[0]))
	if err := dec.Decode(&e); err != nil {
		return esitoVendita{}, fmt.Errorf("vendita nello standalone: riga GR_VENDITA non valida: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return esitoVendita{}, errors.New("vendita nello standalone: dati dopo la riga GR_VENDITA")
	}
	if e.Esito != "ok" {
		return esitoVendita{}, fmt.Errorf("vendita nello standalone: esito %q: %s", e.Esito, e.Motivo)
	}
	if b, err := hex.DecodeString(e.Chiave); err != nil || len(b) != 32 || strings.ToLower(e.Chiave) != e.Chiave {
		return esitoVendita{}, fmt.Errorf("vendita nello standalone: chiave della foto non valida: %q", e.Chiave)
	}
	for _, t := range tabelleVendita {
		if e.Conteggi[t.standalone] < 1 {
			return esitoVendita{}, fmt.Errorf("vendita nello standalone: %s ha %d righe", t.standalone, e.Conteggi[t.standalone])
		}
	}
	return e, nil
}

// fotoSintetica è un piccolo JPEG inventato (una sfumatura), generato a ogni giro: nessun file
// binario nel repo.
func fotoSintetica() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 128, A: 255})
		}
	}
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 85})
	return b.Bytes(), err
}

// contaFile conta i file regolari sotto una cartella.
func contaFile(dir string) (int64, error) {
	var n int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			n++
		}
		return err
	})
	return n, err
}

// venditaConFoto fa il passo della vendita e controlla che la foto sia nei media dello standalone
// con la sua impronta: un solo file in più. Dà l'esito e i file in più.
func venditaConFoto(n nomi, img string, env []string, p percorsiCatena) (esitoVendita, int64, error) {
	foto, err := fotoSintetica()
	if err != nil {
		return esitoVendita{}, 0, fmt.Errorf("foto sintetica: %w", err)
	}
	if err := scriviPrivato(p.foto, string(foto)); err != nil {
		return esitoVendita{}, 0, err
	}
	media := filepath.Join(n.dir, "media")
	prima, err := contaFile(media)
	if err != nil {
		return esitoVendita{}, 0, err
	}
	fmt.Println("catena: vendita con foto nello standalone (bozza, foto, emissione)…")
	out, errDocker := dockerTiene(comandoVendita(n, img, env, p)...)
	e, err := leggiEsitoVendita(out)
	if err != nil {
		return esitoVendita{}, 0, errors.Join(err, errDocker)
	}
	if errDocker != nil {
		return esitoVendita{}, 0, fmt.Errorf("vendita nello standalone: %w", errDocker)
	}
	if sha, _, err := impronta(filepath.Join(media, e.Chiave)); err != nil || sha != e.Chiave {
		return esitoVendita{}, 0, fmt.Errorf("vendita nello standalone: la foto %s non è nei media con la sua impronta (%v)", e.Chiave, err)
	}
	dopo, err := contaFile(media)
	if err != nil {
		return esitoVendita{}, 0, err
	}
	if dopo-prima != 1 {
		return esitoVendita{}, 0, fmt.Errorf("vendita nello standalone: %d file in più nei media, atteso uno", dopo-prima)
	}
	return e, dopo - prima, nil
}

// controllaVendita confronta il report dell'import Go con la vendita: le righe delle vendite
// lette e scritte come le ha contate lo standalone, e un solo avviso tipo/impronta_testo su
// sha256 per ognuna delle tabelle di D-42, per tutte le sue righe lette, e le famiglie media delle
// foto di vendita complete (attesi = copiati = righe, nessun mancante né impronta diversa). Dà le
// righe degli avvisi per tabella e quelle degli avvisi sugli istanti senza zona per colonna
// (istantiSenzaZona).
func controllaVendita(r importer.Report, att sorgenteGo) (map[string]int64, map[string]int64, error) {
	tabelle := map[string]importer.TabellaReport{}
	for _, t := range r.Tabelle {
		tabelle[t.Standalone] = t
	}
	for _, tv := range tabelleVendita {
		t, voluto := tabelle[tv.standalone], att.conteggiVendita[tv.standalone]
		if voluto < 1 || t.Lette != voluto || t.Scritte[tv.goTab] != voluto {
			return nil, nil, fmt.Errorf("report dell'import Go: %s %d righe lette, %s %d scritte; nello standalone %d",
				tv.standalone, t.Lette, tv.goTab, t.Scritte[tv.goTab], voluto)
		}
	}
	avvisi := map[string]int64{}
	for _, v := range r.Avvisi {
		if v.Codice != codiceImprontaTesto {
			continue
		}
		if _, doppio := avvisi[v.Tabella]; doppio || v.Colonna != "sha256" || v.Righe == nil {
			return nil, nil, fmt.Errorf("report dell'import Go: avviso %s inatteso su %s.%s", codiceImprontaTesto, v.Tabella, v.Colonna)
		}
		avvisi[v.Tabella] = *v.Righe
	}
	for _, tab := range tabelleImprontaTesto {
		if n, ok := avvisi[tab]; !ok || n < 1 || n != tabelle[tab].Lette {
			return nil, nil, fmt.Errorf("report dell'import Go: manca l'avviso %s su %s per %d righe (avviso: %d righe)",
				codiceImprontaTesto, tab, tabelle[tab].Lette, n)
		}
	}
	if len(avvisi) != len(tabelleImprontaTesto) {
		return nil, nil, fmt.Errorf("report dell'import Go: avvisi %s su %d tabelle, attese %d", codiceImprontaTesto, len(avvisi), len(tabelleImprontaTesto))
	}
	senzaZona, err := istantiSenzaZona(r, att)
	if err != nil {
		return nil, nil, err
	}
	famiglie := map[string]int{}
	for _, f := range r.Media.Famiglie {
		famiglie[f.Tabella]++
	}
	for _, tab := range famiglieVendita {
		voluto, trovata := att.conteggiVendita[tab], false
		for _, f := range r.Media.Famiglie {
			if f.Tabella == tab && famiglie[tab] == 1 && f.Attesi == voluto && f.Copiati == voluto && f.Mancanti == 0 && f.HashDiversi == 0 {
				trovata = true
			}
		}
		if !trovata {
			return nil, nil, fmt.Errorf("report dell'import Go: la famiglia media %s non ha %d file attesi e copiati senza mancanti né impronte diverse", tab, voluto)
		}
	}
	return avvisi, senzaZona, nil
}

// istantiSenzaZona guarda gli avvisi traduzione/istante_senza_zona: ognuno su una colonna decisa,
// una volta sola, senza id e con almeno una riga; quello di sale_revision_photos.inserted_at c'è
// sempre, per tante righe quante le foto di revisione che il passo della vendita ha contato nello
// standalone (tutte scritte da issue_sale). Gli altri si accettano e si riportano: oggi la catena
// non scrive il registro d'emergenza (slice 028). Dà le righe per tabella.colonna.
func istantiSenzaZona(r importer.Report, att sorgenteGo) (map[string]int64, error) {
	righe := map[string]int64{}
	for _, v := range r.Avvisi {
		if v.Codice != codiceIstanteSenzaZona {
			continue
		}
		k := v.Tabella + "." + v.Colonna
		if _, doppio := righe[k]; doppio || !colonneIstanteSenzaZona[k] || v.Id != nil || v.Righe == nil || *v.Righe < 1 {
			return nil, fmt.Errorf("report dell'import Go: avviso %s inatteso su %s (colonna decisa %v, doppio %v, con id %v, senza righe %v)",
				codiceIstanteSenzaZona, k, colonneIstanteSenzaZona[k], doppio, v.Id != nil, v.Righe == nil || *v.Righe < 1)
		}
		righe[k] = *v.Righe
	}
	if n, voluto := righe[istanteVendita], att.conteggiVendita["sale_revision_photos"]; n < 1 || n != voluto {
		return nil, fmt.Errorf("report dell'import Go: avviso %s su %s per %d righe, volute %d",
			codiceIstanteSenzaZona, istanteVendita, n, voluto)
	}
	return righe, nil
}
