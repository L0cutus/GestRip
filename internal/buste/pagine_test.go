package buste

import "testing"

// Le parti delle voci che stanno nelle pagine (slice 031–034). Finché questi test sono saltati,
// le voci restano parziali: il nucleo di dominio non basta a chiuderle.

// inventario: RIP-H-30, RIP-H-31, RIP-H-34, RIP-H-35, RIP-H-38
// Manca la pagina «Nuova busta»: campi e pulsante «Genera busta», solo i campi del modulo inoltrati
// (un prezzo finale o un testo di consegna forzati si scartano), errori che restano sul modulo con
// i valori, numero proposto nel campo, menu «Oreficeria cliente» solo con un'oreficeria esterna,
// menu «Artigiano», spunta «🪙 Scheda Compro Oro», «Riparazione N creata.», «Link cliente» o
// «Link dell'oreficeria» mostrato una volta, «Non è stato possibile salvare la riparazione.».
func TestPaginaNuovaBusta(t *testing.T) {
	t.Skip("pagina della nuova busta: slice 031")
}

// inventario: RIP-H-39
// Manca l'avviso del QR mancante sul modulo: il primo «Genera busta» non salva, «Genera busta
// senza QR», l'avviso che vale per una busta sola.
func TestPaginaAvvisoQR(t *testing.T) {
	t.Skip("pagina della nuova busta: slice 031")
}

// inventario: RIP-H-36, RIP-S-05
// Manca la parte delle pagine: il preventivo e il prezzo finale nella scheda e nel modulo di
// modifica (il testo vince sui centesimi), la scrittura dei soli centesimi che toglie il testo.
func TestPaginePrezzi(t *testing.T) {
	t.Skip("scheda e modifica della busta: slice 032 e 033")
}

// inventario: RIP-H-37, RIP-S-06
// Manca la parte delle pagine: «Togli data» nel modulo e nella scheda, il testo importato che
// viaggia nascosto («Scritta a mano: …»), «Data di consegna tolta.» / «Data non tolta. Nessuna
// modifica applicata.» e l'audit della scadenza tolta.
func TestPagineConsegna(t *testing.T) {
	t.Skip("nuova busta, scheda e modifica: slice 031–033")
}

// inventario: RIP-S-13
// Manca la parte delle pagine: solo gli stati raggiungibili nel menu, «Stato aggiornato.», il
// modulo generale che non cambia lo stato; e fuori area l'email di pronta e il popup Telegram.
func TestPaginaCambioStato(t *testing.T) {
	t.Skip("stati della busta: slice 034")
}
