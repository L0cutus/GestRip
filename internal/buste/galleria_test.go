package buste

import (
	"testing"
)

// La galleria della home (galleria.go) e la vista nei filtri, come RepairPhotos.gallery/1,
// audit_gallery/2, refresh/1 e Operator.read/0 dello standalone a go-baseline.

// fotoRiga scrive una riga di foto_buste con la posizione data. Dà l'id.
func fotoRiga(t *testing.T, b *Buste, busta, posizione int64) int64 {
	t.Helper()
	r, err := b.d.Scrittura.Exec(`INSERT INTO foto_buste (busta_id, posizione, chiave, nome_file, tipo_mime, byte, sha256, foto_busta,
		visibile_cliente, visibile_artigiano, visibile_oreficeria, creato_il, aggiornato_il)
		SELECT ?, ?, lower(hex(h)), 'f.jpg', 'image/jpeg', 10, h, 0, 0, 0, 0, ?, ? FROM (SELECT randomblob(32) AS h)`,
		busta, posizione, istanteProva, istanteProva)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// inventario: RIP-H-13
// gallery/1: per le buste date che hanno foto, la prima per posizione (unica nella busta) e quante sono;
// le buste senza foto e quelle non chieste non ci sono; nessuna busta, nessuna voce.
func TestGalleria(t *testing.T) {
	b, _, _ := prova(t)
	tre := bustaLista(t, b, "1/26", "", "")
	una := bustaLista(t, b, "2/26", "", "")
	nuda := bustaLista(t, b, "3/26", "", "")
	fuori := bustaLista(t, b, "4/26", "", "")
	fotoRiga(t, b, tre, 3)
	prima := fotoRiga(t, b, tre, 1) // la posizione più bassa, non l'id più basso
	fotoRiga(t, b, tre, 2)
	sola := fotoRiga(t, b, una, 7)
	fotoRiga(t, b, fuori, 1)
	g, err := b.Galleria(ctx, []int64{nuda, una, tre})
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 2 || g[tre].ID != prima || g[tre].Quante != 3 || g[tre].BustaID != tre || g[una].ID != sola || g[una].Quante != 1 {
		t.Errorf("%+v", g)
	}
	if _, c := g[nuda]; c {
		t.Error("una busta senza foto")
	}
	if g, err := b.Galleria(ctx, nil); err != nil || len(g) != 0 {
		t.Errorf("nessuna busta: %v %v", g, err)
	}
}

// inventario: RIP-H-13
// audit_gallery/2: una riga repair.staff_gallery_viewed, risorsa repair, id «gallery», esito ok,
// con gli id delle foto nell'ordine dato.
func TestRegistraGalleria(t *testing.T) {
	b, d, _ := prova(t)
	if err := b.RegistraGalleria(ctx, []int64{9, 3, 5}, chi); err != nil {
		t.Fatal(err)
	}
	var riga string
	if err := d.Lettura.QueryRow(`SELECT attore || '|' || risorsa || '|' || id_risorsa || '|' || esito || '|' || dati FROM eventi_audit
		WHERE azione = 'repair.staff_gallery_viewed'`).Scan(&riga); err != nil {
		t.Fatal(err)
	}
	if riga != `titolare@esempio.test|repair|gallery|ok|{"photo_ids":[9,3,5]}` {
		t.Errorf("%s", riga)
	}
}

// inventario: RIP-H-13
// vista=galleria: solo il testo esatto, senza togliere spazi (Map.get(params, "vista") ==
// "galleria"); non è un filtro (Attivo no) e una ricerca per numero d'etichetta la tiene.
func TestFiltriVista(t *testing.T) {
	for v, voluto := range map[string]bool{"galleria": true, "Galleria": false, " galleria": false, "galleria ": false, "lista": false, "": false} {
		f := LeggiFiltri(func(nome string) (string, bool) {
			if nome == "vista" {
				return v, true
			}
			return "", false
		})
		if f.Galleria != voluto || f.Attivo() {
			t.Errorf("%q: %+v", v, f)
		}
	}
	if f := LeggiFiltri(func(string) (string, bool) { return "", false }); f.Galleria {
		t.Error("vista mancante")
	}
	f := Filtri{Cerca: "0045", Stato: "pronta", Galleria: true, Pagina: 2}
	if e := f.Effettivi(); !e.Galleria || e.Stato != "" || e.Pagina != 2 {
		t.Errorf("effettivi: %+v", e)
	}
}

// inventario: RIP-H-13
// gallery_size/1: galleria_dim «S», «M» o «L» esatti, ogni altro valore o nessuno «M».
func TestImpostazioneGalleria(t *testing.T) {
	b, d, _ := prova(t)
	for v, voluto := range map[string]string{"S": "S", "M": "M", "L": "L", "s": "M", " L": "M", "XL": "M", "": "M"} {
		esegui(t, d, `INSERT OR REPLACE INTO impostazioni (chiave, valore, aggiornata_il) VALUES ('galleria_dim', ?, ?)`, v, istanteProva)
		i, err := b.LeggiImpostazioniHome(ctx)
		if err != nil || i.GalleriaDim != voluto {
			t.Errorf("%q: %q %v", v, i.GalleriaDim, err)
		}
		if l, err := b.Lista(ctx, Filtri{Pagina: 1}); err != nil || l.GalleriaDim != voluto {
			t.Errorf("lista %q: %q %v", v, l.GalleriaDim, err)
		}
	}
}
