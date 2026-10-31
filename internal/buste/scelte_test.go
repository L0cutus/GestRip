package buste

import (
	"reflect"
	"testing"
)

// Le scelte del modulo della nuova busta (RIP-H-29, 34, 35), come refresh/1 di WorkshopLive per
// :new, list_external_oreficerie/0, list_artisans/0 e OnlineSales.shop_name/0.

// inventario: RIP-H-34, RIP-H-35
// Le oreficerie esterne (mai la 1, il negozio) e gli artigiani, per nome e poi per id (l'ordine
// di SQLite, BINARY: le maiuscole prima); il nome del negozio dal profilo.
func TestScelteDelModulo(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `INSERT INTO oreficerie (id, nome) VALUES (3, 'Alfa Gioielli'), (4, 'zeta minuscola'), (5, 'Beta')`)
	esegui(t, d, `INSERT INTO artigiani (id, nome) VALUES (2, 'Cesellatore'), (3, 'Argentiere')`)
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il)
		VALUES ('Bottega Esempio', 'Garanzia', ?, ?)`, istanteProva, istanteProva)
	s, err := b.Scelte(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.NomeNegozio != "Bottega Esempio" {
		t.Errorf("nome del negozio %q", s.NomeNegozio)
	}
	ore := []Voce{{3, "Alfa Gioielli"}, {5, "Beta"}, {2, "Oreficeria Esempio"}, {4, "zeta minuscola"}}
	if !reflect.DeepEqual(s.Oreficerie, ore) {
		t.Errorf("oreficerie %v", s.Oreficerie)
	}
	art := []Voce{{3, "Argentiere"}, {1, "Artigiano Esempio"}, {2, "Cesellatore"}}
	if !reflect.DeepEqual(s.Artigiani, art) {
		t.Errorf("artigiani %v", s.Artigiani)
	}
}

// inventario: RIP-H-34
// Senza profilo, o con il nome vuoto, il negozio si chiama «Negozio» (shop_name/1); senza
// oreficerie esterne l'elenco è vuoto anche se c'è la 1.
func TestScelteSenzaProfilo(t *testing.T) {
	b, d, _ := prova(t)
	esegui(t, d, `DELETE FROM oreficerie WHERE id = 2`)
	s, err := b.Scelte(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.NomeNegozio != "Negozio" || len(s.Oreficerie) != 0 {
		t.Errorf("scelte %+v", s)
	}
	esegui(t, d, `INSERT INTO profilo_negozio (nome, condizioni_garanzia, creato_il, aggiornato_il) VALUES ('', 'G', ?, ?)`, istanteProva, istanteProva)
	if s, _ := b.Scelte(ctx); s.NomeNegozio != "Negozio" {
		t.Errorf("nome vuoto: %q", s.NomeNegozio)
	}
}
