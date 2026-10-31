package importer

import (
	"bytes"
	"database/sql"
	"reflect"
	"testing"
)

// Gli istanti salvati senza zona dallo standalone (decisione del 31/10/2026): issue_sale scrive
// sale_revision_photos.inserted_at con una scrittura SQL a mano, senza la Z (è UTC per
// costruzione), e così altre colonne del registro d'emergenza e delle buste. Solo nelle colonne
// decise (istantiSenzaZona) la forma AAAA-MM-GGThh:mm:ss con 0–6 decimali e senza zona si accetta
// come UTC, si scrive nel Go nella forma normale e il report lo dice con un avviso per colonna e
// il numero di righe; ogni altra forma o colonna blocca come prima.

const codiceSenzaZona = "traduzione/istante_senza_zona"

// istantiFotoRevisioniGo dà creato_il delle foto delle revisioni nel Go, in ordine di id.
func istantiFotoRevisioniGo(t *testing.T, p string) []string {
	t.Helper()
	rows, err := apriLettura(t, p).Query(`SELECT creato_il FROM foto_revisioni_vendite ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	chiudiRighe(t, rows)
	return out
}

// inventario: OPS-01
// Due foto di revisione senza zona (con e senza decimali) passano, riconciliate, con l'avviso su
// sale_revision_photos.inserted_at per 2 righe e nel Go lo stesso istante con la Z; con una riga
// con la Z e una senza l'avviso conta 1; senza righe senza zona non c'è avviso. Il report non
// contiene mai il valore.
func TestIstantiSenzaZonaAccettati(t *testing.T) {
	for _, c := range []struct {
		nome         string
		prima, dopo  string // inserted_at delle righe 1 e 2 nella sorgente
		righe        int64  // 0: nessun avviso
		primaGo, dGo string
	}{
		{"due senza zona", "2025-03-07T08:09:41.034145", "2025-03-07T08:09:42",
			2, "2025-03-07T08:09:41.034145Z", "2025-03-07T08:09:42.000000Z"},
		{"un decimale", "2025-03-07T08:09:41.5", "2025-03-07T08:09:42.12",
			2, "2025-03-07T08:09:41.500000Z", "2025-03-07T08:09:42.120000Z"},
		{"mista", "2025-03-07T08:09:41.034145Z", "2025-03-07T08:09:42.000001",
			1, "2025-03-07T08:09:41.034145Z", "2025-03-07T08:09:42.000001Z"},
		{"nessuna senza zona", "2025-03-07T08:09:41.034145Z", "2025-03-07T08:09:42.000000Z",
			0, "2025-03-07T08:09:41.034145Z", "2025-03-07T08:09:42.000000Z"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			_, o := preparaImport(t, func(db *sql.DB) {
				tabelleImpronta[2].seconda(db, 1) // seconda foto di revisione, con il file del logo
				esegui(db, `UPDATE sale_revision_photos SET inserted_at = ? WHERE id = 1`, c.prima)
				esegui(db, `UPDATE sale_revision_photos SET inserted_at = ? WHERE id = 2`, c.dopo)
			})
			es := importa(t, o)
			r, b := leggiReport(t, o.Report)
			if es.Verdetto != Riconciliato {
				t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
			}
			var want []Voce
			if c.righe > 0 {
				want = []Voce{{Codice: codiceSenzaZona, Tabella: "sale_revision_photos", Colonna: "inserted_at", Righe: ptr(c.righe)}}
			}
			if len(r.Avvisi)+len(want) > 0 && !reflect.DeepEqual(r.Avvisi, want) {
				t.Errorf("avvisi %+v, attesi %+v", r.Avvisi, want)
			}
			if got := istantiFotoRevisioniGo(t, o.Destinazione); !reflect.DeepEqual(got, []string{c.primaGo, c.dGo}) {
				t.Errorf("creato_il nel Go %v, attesi %v", got, []string{c.primaGo, c.dGo})
			}
			if bytes.Contains(b, []byte("2025-03-07")) {
				t.Error("il report contiene un istante della sorgente")
			}
		})
	}
}

// inventario: OPS-01
// Ogni altra forma blocca con traduzione/istante su tabella, id e colonna, senza l'avviso: nella
// colonna decisa un fuso, 7 decimali, un giorno o un'ora che non esistono, lo spazio al posto
// della T, senza secondi, i decimali vuoti, un numero (e qualcuna nelle altre colonne decise); la
// forma senza zona in ogni colonna non decisa, anche delle vendite.
func TestIstantiSenzaZonaAltroveBloccano(t *testing.T) {
	type caso struct {
		nome, tab, col string
		valore         any
	}
	var casi []caso
	for _, v := range []struct {
		nome   string
		valore any
	}{
		{"fuso +02:00", "2025-03-07T08:09:41+02:00"},
		{"fuso +02:00 con decimali", "2025-03-07T08:09:41.034145+02:00"},
		{"7 decimali", "2025-03-07T08:09:41.0341451"},
		{"7 decimali con la Z", "2025-03-07T08:09:41.0341451Z"},
		{"giorno inesistente", "2025-02-30T08:09:41"},
		{"ora inesistente", "2025-03-07T24:09:41"},
		{"spazio al posto della T", "2025-03-07 08:09:41"},
		{"senza secondi", "2025-03-07T08:09"},
		{"decimali vuoti", "2025-03-07T08:09:41."},
		{"solo la data", "2025-03-07"},
		{"intero", int64(1741334981)},
		{"reale", 1741334981.5},
	} {
		casi = append(casi, caso{"sale_revision_photos.inserted_at " + v.nome, "sale_revision_photos", "inserted_at", v.valore})
	}
	// Anche nelle altre colonne decise solo la forma senza zona passa.
	casi = append(casi, caso{"repairs.updated_at fuso +02:00", "repairs", "updated_at", "2025-03-07T08:09:41+02:00"},
		caso{"emergency_events.closed_at spazio al posto della T", "emergency_events", "closed_at", "2025-03-07 08:09:41"},
		caso{"emergency_entries.inserted_at 7 decimali", "emergency_entries", "inserted_at", "2025-03-07T08:09:41.0341451"})
	for _, c := range []struct{ tab, col string }{
		{"sale_photos", "inserted_at"},
		{"sale_revision_seals", "sealed_at"},
		{"sale_revision_revocations", "revoked_at"},
		{"historical_documents", "inserted_at"},
		{"customers", "inserted_at"},
		{"customers", "updated_at"},
	} {
		casi = append(casi, caso{c.tab + "." + c.col + " senza zona", c.tab, c.col, "2025-03-07T08:09:41.034145"})
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			dir, o := preparaImport(t, func(db *sql.DB) {
				esegui(db, `UPDATE `+c.tab+` SET `+c.col+` = ? WHERE rowid = 1`, c.valore)
			})
			r := controllaBloccato(t, dir, o, importa(t, o))
			if !ha(r.Differenze, "traduzione/istante", c.tab, 1, c.col) {
				t.Errorf("differenze %s, manca traduzione/istante su %s.%s", differenzeDi(r), c.tab, c.col)
			}
			for _, a := range r.Avvisi {
				if a.Codice == codiceSenzaZona {
					t.Errorf("avviso %+v per una forma che blocca", a)
				}
			}
			if _, b := leggiReport(t, o.Report); bytes.Contains(b, []byte("2025-")) {
				t.Error("il report contiene il valore")
			}
		})
	}
}

// valoriGo dà la prima colonna di q nel Go, come testo, riga per riga.
func valoriGo(t *testing.T, p, q string) []string {
	t.Helper()
	rows, err := apriLettura(t, p).Query(q)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	chiudiRighe(t, rows)
	return out
}

// avvisoSenzaZona è l'avviso atteso per una colonna.
func avvisoSenzaZona(tab, col string, righe int64) Voce {
	return Voce{Codice: codiceSenzaZona, Tabella: tab, Colonna: col, Righe: ptr(righe)}
}

// inventario: OPS-01
// Le altre colonne decise: una colonna può avere righe con la Z e righe senza (l'avviso conta solo
// quelle senza), e due colonne della stessa tabella danno due avvisi, in ordine di tabella e
// colonna. Nel Go lo stesso istante con la Z; il report non contiene il valore.
func TestIstantiSenzaZonaAltreColonneDecise(t *testing.T) {
	for _, c := range []struct {
		nome     string
		ritocca  func(*sql.DB)
		want     []Voce
		q        string // nel Go, i valori da confrontare con goWant
		goWant   []string
		conLaZed string // nel Go, un valore che resta quello della sorgente con la Z (vuoto: niente)
	}{
		{"repairs", func(db *sql.DB) {
			esegui(db, `UPDATE repairs SET updated_at = '2025-03-07T08:09:41' WHERE id = 2`)
			esegui(db, `UPDATE repairs SET retention_started_at = '2025-03-07T08:09:43.5' WHERE id = 1`)
		}, []Voce{avvisoSenzaZona("repairs", "retention_started_at", 1), avvisoSenzaZona("repairs", "updated_at", 1)},
			`SELECT conservazione_iniziata_il FROM buste WHERE id = 1 UNION ALL SELECT aggiornato_il FROM buste WHERE id = 2`,
			[]string{"2025-03-07T08:09:43.500000Z", "2025-03-07T08:09:41.000000Z"},
			`SELECT aggiornato_il FROM buste WHERE id = 1`},
		{"emergency_entries", func(db *sql.DB) {
			esegui(db, `UPDATE emergency_entries SET inserted_at = '2025-03-07T08:09:4' || id`)
		}, []Voce{avvisoSenzaZona("emergency_entries", "inserted_at", 2)},
			`SELECT creato_il FROM righe_emergenza ORDER BY id`,
			[]string{"2025-03-07T08:09:41.000000Z", "2025-03-07T08:09:42.000000Z"}, ""},
		{"emergency_events", func(db *sql.DB) {
			esegui(db, `UPDATE emergency_events SET closed_at = '2025-03-07T08:09:41.000001', updated_at = '2025-03-07T08:09:42.123456'`)
		}, []Voce{avvisoSenzaZona("emergency_events", "closed_at", 1), avvisoSenzaZona("emergency_events", "updated_at", 1)},
			`SELECT chiuso_il FROM eventi_emergenza UNION ALL SELECT aggiornato_il FROM eventi_emergenza`,
			[]string{"2025-03-07T08:09:41.000001Z", "2025-03-07T08:09:42.123456Z"}, ""},
	} {
		t.Run(c.nome, func(t *testing.T) {
			_, o := preparaImport(t, c.ritocca)
			es := importa(t, o)
			r, b := leggiReport(t, o.Report)
			if es.Verdetto != Riconciliato {
				t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
			}
			if !reflect.DeepEqual(r.Avvisi, c.want) {
				t.Errorf("avvisi %+v, attesi %+v", r.Avvisi, c.want)
			}
			if got := valoriGo(t, o.Destinazione, c.q); !reflect.DeepEqual(got, c.goWant) {
				t.Errorf("nel Go %v, attesi %v", got, c.goWant)
			}
			if c.conLaZed != "" {
				if got := valoriGo(t, o.Destinazione, c.conLaZed); len(got) != 1 || !bytes.HasPrefix([]byte(got[0]), []byte("2024-")) {
					t.Errorf("la riga con la Z nel Go: %v", got)
				}
			}
			if bytes.Contains(b, []byte("2025-03-07")) {
				t.Error("il report contiene un istante della sorgente")
			}
		})
	}
}

// inventario: OPS-01
// Il conteggio è per colonna della sorgente su tutto l'import: settings va in due tabelle del Go
// (D-03), e una sua colonna tra quelle decise darebbe un solo avviso con la somma delle righe.
// Il test allarga l'elenco solo per sé.
func TestIstantiSenzaZonaUnAvvisoPerColonnaSorgente(t *testing.T) {
	k := colonnaSorgente{"settings", "updated_at"}
	istantiSenzaZona[k] = true
	t.Cleanup(func() { delete(istantiSenzaZona, k) })
	_, o := preparaImport(t, func(db *sql.DB) {
		esegui(db, `UPDATE settings SET updated_at = '2025-03-07T08:09:41'`)
	})
	es := importa(t, o)
	r, _ := leggiReport(t, o.Report)
	if es.Verdetto != Riconciliato {
		t.Fatalf("verdetto %s, differenze %s", es.Verdetto, differenzeDi(r))
	}
	var n int64
	if err := apriLettura(t, o.Sorgente).QueryRow(`SELECT count(*) FROM settings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	var segreti, impostazioni int64
	dst := apriLettura(t, o.Destinazione)
	if err := dst.QueryRow(`SELECT (SELECT count(*) FROM segreti_provider), (SELECT count(*) FROM impostazioni)`).Scan(&segreti, &impostazioni); err != nil {
		t.Fatal(err)
	}
	if segreti < 1 || impostazioni < 1 {
		t.Fatalf("il caso vuole righe in tutte e due le tabelle: segreti %d, impostazioni %d", segreti, impostazioni)
	}
	if want := []Voce{avvisoSenzaZona("settings", "updated_at", n)}; !reflect.DeepEqual(r.Avvisi, want) {
		t.Errorf("avvisi %+v, attesi %+v", r.Avvisi, want)
	}
}

// inventario: OPS-01
// Una riga senza zona che il Go rifiuta (qui la chiave che non è l'indirizzo del contenuto) è una
// differenza e non si conta nell'avviso.
func TestIstantiSenzaZonaRigaScartataNonContata(t *testing.T) {
	dir, o := preparaImport(t, func(db *sql.DB) {
		tabelleImpronta[2].seconda(db, 1)
		esegui(db, `UPDATE sale_revision_photos SET inserted_at = '2025-03-07T08:09:41'`)
		esegui(db, `UPDATE sale_revision_photos SET storage_key = upper(storage_key) WHERE id = 2`)
	})
	r := controllaBloccato(t, dir, o, importa(t, o))
	if !ha(r.Differenze, "vincolo/check", "sale_revision_photos", 2, "storage_key") {
		t.Errorf("differenze %s, manca vincolo/check su id 2", differenzeDi(r))
	}
	var trovati []Voce
	for _, a := range r.Avvisi {
		if a.Codice == codiceSenzaZona {
			trovati = append(trovati, a)
		}
	}
	if want := []Voce{avvisoSenzaZona("sale_revision_photos", "inserted_at", 1)}; !reflect.DeepEqual(trovati, want) {
		t.Errorf("avvisi %+v, attesi %+v", trovati, want)
	}
}

// Gli elenchi delle colonne speciali sono coerenti con la mappa: ogni istante senza zona è una
// colonna con la traduzione Istante, ogni impronta come testo una colonna BLOB nel Go, in ogni
// voce della mappa della sua tabella. L'elenco degli istanti è proprio quello deciso il 31/10/2026.
func TestColonneSpecialiNellaMappa(t *testing.T) {
	_, o := preparaImport(t, nil)
	if es := importa(t, o); es.Verdetto != Riconciliato {
		t.Fatalf("verdetto %s", es.Verdetto)
	}
	g, err := leggiGo(t.Context(), apriLettura(t, o.Destinazione))
	if err != nil {
		t.Fatal(err)
	}
	trova := func(k colonnaSorgente, ok func(Tabella, Colonna) bool) {
		t.Helper()
		voci := 0
		for _, tab := range Mappa {
			if tab.Standalone != k.tabella || tab.Go == "" {
				continue
			}
			voci++
			c, trovata := Colonna{}, false
			for _, col := range tab.Colonne {
				if col.Standalone == k.colonna {
					c, trovata = col, true
				}
			}
			if !trovata || !ok(tab, c) {
				t.Errorf("%s.%s nella voce %s della mappa: %+v (trovata %v)", k.tabella, k.colonna, tab.Go, c, trovata)
			}
		}
		if voci == 0 {
			t.Errorf("%s non è nella mappa", k.tabella)
		}
	}
	for k := range istantiSenzaZona {
		trova(k, func(_ Tabella, c Colonna) bool { return c.Traduzione == Istante })
	}
	for k := range impronteTesto {
		trova(k, func(tab Tabella, c Colonna) bool { return g.colonne[tab.Go][c.Go].tipo == "BLOB" })
	}
	decise := map[colonnaSorgente]bool{
		{"sale_revision_photos", "inserted_at"}: true,
		{"emergency_entries", "inserted_at"}:    true,
		{"emergency_events", "closed_at"}:       true,
		{"emergency_events", "updated_at"}:      true,
		{"repairs", "retention_started_at"}:     true,
		{"repairs", "updated_at"}:               true,
	}
	if !reflect.DeepEqual(istantiSenzaZona, decise) {
		t.Errorf("istantiSenzaZona %v, decise %v", istantiSenzaZona, decise)
	}
}
