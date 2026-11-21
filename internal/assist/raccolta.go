package assist

import (
	"encoding/json"
	"regexp"
)

// La cattura di un'estrazione per la raccolta dei dettati (slice 113; RIP-H-48), la parte di Training.capture/3 dello
// standalone a go-baseline che sa com'è fatta la proposta. Il testo è Estrazione.Testo (il dettato mandato al modello,
// senza spazi ai bordi), il modello Estrazione.Modello (quello che ha risposto, o il configurato: with_model/2); la riga
// la scrive internal/buste nella transazione della busta.
//
// La proposta tiene le chiavi della riga dello standalone (@kept di Training: data, nome, telefono, descrizione, note,
// preventivo, data_consegna, oreficeria, compro_oro, oreficeria_id, nome_corretto_da; mai il numero di busta), così le
// righe importate e quelle nuove si leggono allo stesso modo nel conteggio delle correzioni e nell'esportazione (115-116).
// I valori sono quelli della proposta del Go (formato corto D-103, date D-18, note D-20, telefono D-104 e D-106), scritti
// come li legge lo standalone: date in GG/MM/AAAA, compro_oro 0 o 1, oreficeria_id intero o "".

// reDataISO è una data AAAA-MM-GG del modulo.
var reDataISO = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})$`)

// dataPerLaRaccolta è una data del modulo in GG/MM/AAAA (Clock.format(date, "%d/%m/%Y")); un altro testo resta com'è.
func dataPerLaRaccolta(s string) string {
	if m := reDataISO.FindStringSubmatch(s); m != nil {
		return m[3] + "/" + m[2] + "/" + m[1]
	}
	return s
}

// PropostaPerLaRaccolta è la proposta p come la tiene la riga della raccolta, in JSON.
func PropostaPerLaRaccolta(p Proposta) []byte {
	m := map[string]any{
		"data": dataPerLaRaccolta(p.DataAccettazione), "nome": p.NomeCliente, "telefono": p.TelefonoCliente,
		"descrizione": p.Descrizione, "note": p.NotePubbliche, "preventivo": p.Preventivo,
		"data_consegna": dataPerLaRaccolta(p.DataPrevista), "oreficeria": p.Oreficeria, "compro_oro": 0, "oreficeria_id": "",
	}
	if p.CompraOro {
		m["compro_oro"] = 1
	}
	if p.OreficeriaID != 0 {
		m["oreficeria_id"] = p.OreficeriaID
	}
	if p.NomeCorretto {
		m["nome_corretto_da"] = p.NomeCorrettoDa
	}
	j, _ := json.Marshal(m) // testi e numeri: non fallisce
	return j
}
