package buste

import "testing"

// Le parti delle voci che stanno nelle pagine e non ci sono ancora (T4.2, il livello browser del
// banco). Finché questi test sono saltati, le voci restano parziali. Le pagine che ci sono hanno i
// loro test in internal/web (nuova_test.go, scheda_test.go, modifica_test.go, stati_test.go).

// inventario: RIP-H-37
// Manca il segno della consegna a testo nella lista (T4.2). La scheda, il modulo di modifica e
// «Togli data» ci sono dalla slice 033 (internal/web/modifica_test.go).
func TestPagineConsegnaNellaLista(t *testing.T) {
	t.Skip("lista delle buste: T4.2")
}

// inventario: RIP-S-14
// La finestra «Riaprire questa busta chiusa? Il motivo resta nel registro.» la apre app.js
// (data-confirm, come phoenix_html dello standalone), e senza JavaScript il pulsante riapre senza
// chiedere. Il modulo e lo script li provano internal/web/stati_test.go; la finestra vera la prova
// il livello browser del banco.
func TestPaginaRiaperturaConfermaNelBrowser(t *testing.T) {
	t.Skip("livello browser del banco")
}

// inventario: RIP-S-13, RIP-S-14
// Dopo il passaggio a «pronta» da un altro stato, e dopo una riapertura in «pronta», lo standalone
// accoda l'email di pronta (announce_ready_by_email/4, EmailNotice.notify_ready/2) in una
// transazione sua, solo con un indirizzo confermato. Nel Go l'aggancio arriva con l'area email.
func TestEmailDiProntaDopoIlCambio(t *testing.T) {
	t.Skip("email di pronta dopo il cambio di stato e la riapertura: area email (T4.7)")
}

// inventario: RIP-S-33
// La rotta delle foto del personale c'è dalla slice 037 (internal/web/foto_test.go), con il
// gettone della galleria verificato. Manca chi lo firma: la galleria della home, che scrive una
// riga repair.staff_gallery_viewed per tutte le foto che mostra (RepairPhotos.audit_gallery/2) e
// mette il gettone nell'indirizzo di ogni miniatura (T4.2).
func TestGalleriaDellaHomeFirmaIlGettone(t *testing.T) {
	t.Skip("galleria della home: T4.2")
}

// inventario: RIP-S-28
// La galleria e il visualizzatore senza JavaScript ci sono dalla slice 038 (internal/web/
// foto_scheda_test.go), con lo scorrimento, le frecce, Esc e la voce della cronologia sostituita in
// app.js (photo_viewer.js). Che quei gesti girino in un browser vero lo prova il livello browser del
// banco (slice 040).
func TestVisualizzatoreFotoNelBrowser(t *testing.T) {
	t.Skip("gesti del visualizzatore nel browser: livello browser del banco (slice 040)")
}

// inventario: RIP-S-29
// Il caricamento dalla scheda c'è dalla slice 038 (internal/web/foto_scheda_test.go), anche senza
// JavaScript. Mancano l'anteprima della foto scelta e la barra di avanzamento
// (live_img_preview e entry.progress di LiveView), che vogliono JavaScript, e la prova nel browser
// vero che il modulo parta da solo e che «Foto caricata.» sparisca dopo 3 s: livello browser del
// banco (slice 040).
func TestCaricamentoFotoAnteprimaEAvanzamento(t *testing.T) {
	t.Skip("anteprima e barra di avanzamento del caricamento: JavaScript, livello browser del banco (slice 040)")
}

// inventario: RIP-S-32
// La finestra «Eliminare la foto?» la apre app.js (data-confirm, come phoenix_html dello
// standalone), e senza JavaScript «Elimina foto» toglie la foto senza chiedere, come la riapertura
// (RIP-S-14). Il modulo e il testo li prova internal/web/foto_scheda_test.go; la finestra vera la
// prova il livello browser del banco.
func TestEliminaFotoConfermaNelBrowser(t *testing.T) {
	t.Skip("livello browser del banco")
}
