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

// inventario: RIP-S-29, RIP-S-30
// «Fotocamera» e «Galleria» sulla scheda, il caricamento (multipart, token in x-csrf-token) con i
// messaggi dello standalone e la normalizzazione nel figlio (imaging.Lavoratore): slice 038.
func TestPaginaCaricamentoFotoDallaScheda(t *testing.T) {
	t.Skip("caricamento delle foto dalla scheda: slice 038")
}

// inventario: RIP-S-31, RIP-S-32
// I pulsanti «Cliente/Orefice/Artigiano ✓/⛔» sotto ogni foto con «Visibilità non aggiornata.», e
// «Elimina foto» con «Eliminare la foto?» e «Foto non disponibile.»: slice 038.
func TestPaginaVisibilitaEdEliminazioneFoto(t *testing.T) {
	t.Skip("visibilità ed eliminazione dalla scheda: slice 038")
}

// inventario: RIP-S-33
// La rotta delle foto del personale c'è dalla slice 037 (internal/web/foto_test.go), con il
// gettone della galleria verificato. Manca chi lo firma: la galleria della home, che scrive una
// riga repair.staff_gallery_viewed per tutte le foto che mostra (RepairPhotos.audit_gallery/2) e
// mette il gettone nell'indirizzo di ogni miniatura (T4.2).
func TestGalleriaDellaHomeFirmaIlGettone(t *testing.T) {
	t.Skip("galleria della home: T4.2")
}
