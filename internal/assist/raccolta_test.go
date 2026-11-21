package assist

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/L0cutus/gestrip/tools/banco/modellofinto"
)

// La cattura di un'estrazione per la raccolta dei dettati (slice 113; RIP-H-48), come Training.capture/3 dello standalone a
// go-baseline: il dettato senza spazi ai bordi (String.trim/1), la proposta con le chiavi della riga dello standalone (@kept,
// senza il numero di busta) e il modello che ha risposto. Dati inventati.

// inventario: RIP-H-48
// Il testo della cattura è il dettato mandato al modello, senza gli spazi ai bordi; il modello è quello che ha risposto.
func TestEstrazioneTestoDellaCattura(t *testing.T) {
	f, m := modelloDiProva(t)
	f.Programma(modellofinto.Risposta{Contenuto: modellofinto.Testo(modellofinto.SchedaBuona), Modello: modellofinto.Testo("gestrip-inventato-v2")})
	e, _, err := estraiProva(t, m, "  \n"+dettatoProva+"  \t")
	if err != nil {
		t.Fatal(err)
	}
	if e.Testo != dettatoProva || e.Modello != "gestrip-inventato-v2" {
		t.Errorf("testo %q modello %q", e.Testo, e.Modello)
	}
	// Un rifiuto non porta il testo.
	if e, _, err := estraiProva(t, m, "corto"); err == nil || e.Testo != "" {
		t.Errorf("rifiuto con testo %q (%v)", e.Testo, err)
	}
}

// inventario: RIP-H-48
// La proposta della riga: le chiavi dello standalone (data, nome, telefono, descrizione, note, preventivo, data_consegna,
// oreficeria, compro_oro, oreficeria_id e nome_corretto_da solo se il nome è stato corretto), le date in GG/MM/AAAA come le
// legge il conteggio delle correzioni, compro oro 0/1, l'oreficeria riconosciuta come id o "".
func TestPropostaPerLaRaccolta(t *testing.T) {
	casi := []struct {
		nome   string
		p      Proposta
		attesa map[string]any
	}{
		{"completa", Proposta{
			DataAccettazione: "2026-03-13", NomeCliente: "Ofelia Inventata", TelefonoCliente: "3330000113", Oreficeria: "Bottari",
			Descrizione: "anello da stringere", NotePubbliche: "Busta 12 — Acconto 10 €", Preventivo: "40", DataPrevista: "2026-03-27",
			CompraOro: true, OreficeriaID: 7, NomeCorretto: true, NomeCorrettoDa: "ofelia inventata",
		}, map[string]any{
			"data": "13/03/2026", "nome": "Ofelia Inventata", "telefono": "3330000113", "descrizione": "anello da stringere",
			"note": "Busta 12 — Acconto 10 €", "preventivo": "40", "data_consegna": "27/03/2026", "oreficeria": "Bottari",
			"compro_oro": float64(1), "oreficeria_id": float64(7), "nome_corretto_da": "ofelia inventata",
		}},
		{"vuota", Proposta{DataAccettazione: "2026-03-13"}, map[string]any{
			"data": "13/03/2026", "nome": "", "telefono": "", "descrizione": "", "note": "", "preventivo": "", "data_consegna": "",
			"oreficeria": "", "compro_oro": float64(0), "oreficeria_id": "",
		}},
		{"nome corretto da vuoto", Proposta{DataAccettazione: "2026-03-13", NomeCliente: "Ofelia Inventata", NomeCorretto: true},
			map[string]any{
				"data": "13/03/2026", "nome": "Ofelia Inventata", "telefono": "", "descrizione": "", "note": "", "preventivo": "",
				"data_consegna": "", "oreficeria": "", "compro_oro": float64(0), "oreficeria_id": "", "nome_corretto_da": "",
			}},
		{"date non nella forma", Proposta{DataAccettazione: "13/03/2026", DataPrevista: "fine mese"}, map[string]any{
			"data": "13/03/2026", "nome": "", "telefono": "", "descrizione": "", "note": "", "preventivo": "", "data_consegna": "fine mese",
			"oreficeria": "", "compro_oro": float64(0), "oreficeria_id": "",
		}},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			var got map[string]any
			if err := json.Unmarshal(PropostaPerLaRaccolta(c.p), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.attesa) {
				t.Errorf("proposta\n%v\nattesa\n%v", got, c.attesa)
			}
			if _, ok := got["bag_number"]; ok {
				t.Error("il numero di busta nella proposta")
			}
		})
	}
}
