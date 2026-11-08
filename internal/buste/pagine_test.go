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
// transazione sua, solo con un indirizzo confermato: anche dopo «✅ Pronta» del pannello del QR
// (qr_advance_repair/3 passa da transact_transition/5, slice 054). Nel Go l'aggancio arriva con
// l'area email, per i tre chiamanti di passaStato che portano a pronta: CambiaStato, Riapri e
// AvanzaDaQR.
func TestEmailDiProntaDopoIlCambio(t *testing.T) {
	t.Skip("email di pronta dopo il cambio di stato, la riapertura e «✅ Pronta» del pannello QR: area email (T4.7)")
}
