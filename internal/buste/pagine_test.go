package buste

import "testing"

// Le parti delle voci che stanno nelle pagine (slice 034 e T4.2). Finché questi test sono saltati,
// le voci restano parziali: il nucleo di dominio non basta a chiuderle. La pagina della nuova
// busta (slice 031) ha i suoi test in internal/web/nuova_test.go.

// inventario: RIP-H-37
// Manca il segno della consegna a testo nella lista (T4.2). La scheda, il modulo di modifica e
// «Togli data» ci sono dalla slice 033 (internal/web/modifica_test.go).
func TestPagineConsegnaNellaLista(t *testing.T) {
	t.Skip("lista delle buste: T4.2")
}

// inventario: RIP-S-13
// Manca la parte delle pagine: solo gli stati raggiungibili nel menu, «Stato aggiornato.», il
// modulo generale che non cambia lo stato; e fuori area l'email di pronta e il popup Telegram.
func TestPaginaCambioStato(t *testing.T) {
	t.Skip("stati della busta: slice 034")
}
