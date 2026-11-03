package buste

import "testing"

// Le parti delle voci che stanno nelle pagine e non ci sono ancora (T4.2). Finché questi test sono
// saltati, le voci restano parziali. Le pagine che ci sono hanno i loro test in internal/web
// (nuova_test.go, scheda_test.go, modifica_test.go, stati_test.go). Ciò che vuole un browser vero (la
// finestra di data-confirm di «Riapri busta» e di «Elimina foto», i gesti del visualizzatore delle
// foto, l'invio automatico del caricamento) lo provano gli scenari del livello browser del banco
// (bench/browser, slice 040a), confrontati con lo standalone.

// inventario: RIP-H-37
// Manca il segno della consegna a testo nella lista (T4.2). La scheda, il modulo di modifica e
// «Togli data» ci sono dalla slice 033 (internal/web/modifica_test.go).
func TestPagineConsegnaNellaLista(t *testing.T) {
	t.Skip("lista delle buste: T4.2")
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

// inventario: RIP-S-29
// Il caricamento dalla scheda c'è dalla slice 038 (internal/web/foto_scheda_test.go), anche senza
// JavaScript, e lo scenario browser caricamento (slice 040a) prova nel browser vero l'invio
// automatico, «Foto caricata.» che sparisce dopo 3 s, le 3 foto e la foto oltre 25 MiB. Mancano
// l'anteprima della foto scelta e la barra di avanzamento (live_img_preview e entry.progress di
// LiveView), che l'inventario prevede: lo scenario le vede nello standalone e non nel Go, e arrivano
// con la slice 040b.
func TestCaricamentoFotoAnteprimaEAvanzamento(t *testing.T) {
	t.Skip("anteprima e barra di avanzamento del caricamento: slice 040b")
}
