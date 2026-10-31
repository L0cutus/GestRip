package buste

import "testing"

// Le parti delle voci che stanno nelle pagine (slice 032–034). Finché questi test sono saltati,
// le voci restano parziali: il nucleo di dominio non basta a chiuderle. La pagina della nuova
// busta (slice 031) ha i suoi test in internal/web/nuova_test.go.

// inventario: RIP-S-05
// Manca la parte delle pagine: il preventivo e il prezzo finale nel modulo di modifica, la
// scrittura dei soli centesimi che toglie il testo. Nella scheda (il testo vince sui centesimi)
// c'è dalla slice 032 (internal/web/scheda_test.go).
func TestPaginePrezzi(t *testing.T) {
	t.Skip("modifica della busta: slice 033")
}

// inventario: RIP-H-37, RIP-S-06
// Manca la parte delle pagine: «Togli data» nella scheda e nel modulo di modifica, il testo importato che
// viaggia nascosto («Scritta a mano: …»), «Data di consegna tolta.» / «Data non tolta. Nessuna
// modifica applicata.» e l'audit della scadenza tolta.
func TestPagineConsegna(t *testing.T) {
	t.Skip("«Togli data» e modifica: slice 033")
}

// inventario: RIP-S-13
// Manca la parte delle pagine: solo gli stati raggiungibili nel menu, «Stato aggiornato.», il
// modulo generale che non cambia lo stato; e fuori area l'email di pronta e il popup Telegram.
func TestPaginaCambioStato(t *testing.T) {
	t.Skip("stati della busta: slice 034")
}
