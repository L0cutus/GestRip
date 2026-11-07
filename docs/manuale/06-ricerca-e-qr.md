# Ricerca e QR

In alto nella lista delle riparazioni c'è un solo campo, «Cerca nome, n° busta o QR…», con 🔍 a
sinistra e 📷 a destra. Serve per cercare una busta scrivendo e per leggere l'etichetta QR di una busta,
con la fotocamera o con il lettore a pistola. I filtri già scelti restano. Titolare e dipendente lo
usano allo stesso modo. In «Nuova riparazione» il campo non c'è: lì il 📷 sta accanto all'etichetta.

## Cercare scrivendo

- Si scrive e si preme 🔍 (o Invio).
- Un numero d'etichetta («0045/26», «0045», «45»): escono **sempre tutte** le buste con quell'etichetta,
  anche chiuse e consegnate, qualunque filtro sia attivo. Con un risultato si apre la scheda, con più
  di uno la lista.
- Insieme escono anche le buste **senza etichetta** con quel numero interno (per esempio la busta
  «N. 45/26», scrivendo «45/26» o «45»): sono segnate «⚠ senza QR», anche se sono chiuse. Senza l'anno
  («45», «0045») valgono tutti gli anni, come per le etichette.
- Con un numero da 1 a 4 cifre la ricerca guarda solo le etichette e i numeri interni, non il
  telefono: per questo la riga sotto il campo, mentre si scrive un numero così, non dice «telefono».
- Le altre ricerche (nome, telefono, descrizione, note) tengono i filtri attivi: con un risultato si
  apre la scheda, con più di uno la lista filtrata.

## Leggere un'etichetta con la fotocamera

1. Tocca 📷 nel campo di ricerca. Si apre una finestra scura con il video della fotocamera posteriore
   e una linea rossa di mira.
2. Inquadra il QR dell'etichetta. Appena il codice è letto la finestra si chiude da sola.
3. Il riquadro «📷 Cerca per QR busta» si apre **sopra la lista**, senza cambiare pagina: la lista e i
   filtri restano sotto.

Nella finestra della fotocamera:

- «🔄 Cambia fotocamera» passa alla fotocamera posteriore successiva, sui telefoni che ne hanno più
  di una. Il telefono ricorda la scelta per le volte dopo. Con una sola fotocamera la finestra dice
  «Una sola fotocamera disponibile.».
- «Chiudi» (o Esc sul computer) chiude la finestra senza leggere niente.

Il programma prova da solo la fotocamera principale: su alcuni telefoni con più obiettivi la prima
fotocamera aperta è il teleobiettivo, che mette a fuoco solo da lontano.

Il lettore legge anche i QR stampati a specchio delle etichette fatte con il programma di prima.

### Il permesso della fotocamera

La prima volta il browser chiede se GestRip può usare la fotocamera: rispondi «Consenti». Se per
sbaglio hai risposto di no, la finestra mostra l'errore del browser; il permesso si rimette dalle
impostazioni del sito nel browser (il lucchetto accanto all'indirizzo).

### Serve una connessione sicura

La fotocamera funziona solo se GestRip è aperto con un indirizzo sicuro (`https://`, con il
certificato del negozio installato sul telefono). Altrimenti la finestra dice «⚠ Fotocamera non
disponibile: contesto non sicuro (HTTP o certificato non valido), il browser non espone la
fotocamera.»: non è un guasto del programma, è il browser che non dà la fotocamera. Se il telefono o
il browser non hanno una fotocamera, il messaggio è «⚠ Fotocamera non disponibile su questo
dispositivo o browser.».

## Leggere un'etichetta con il lettore a pistola

Tocca il campo di ricerca (il testo già scritto si seleziona) e spara sull'etichetta: il lettore
scrive il codice e preme Invio da solo. Se il codice è quello di una busta, il riquadro si apre sopra
la lista come con la fotocamera. Un codice che non è di nessuna busta conta come una ricerca scritta:
esce la lista vuota.

## Il riquadro della busta

- Se l'etichetta è su una sola busta aperta, il riquadro mostra quella busta: numero, cliente, stato e
  il passo successivo.
- Se l'etichetta è su più buste aperte, o solo su buste chiuse, il riquadro dice «N riparazioni con
  questo QR — scegli:»: tocca la busta giusta e il riquadro mostra quella, sempre sopra la lista.
- Se nessuna busta ha quel codice: «⚠️ Nessuna riparazione trovata per questo QR».
- «Chiudi» (o Esc sul computer) toglie il riquadro: la lista è ancora lì.

Il riquadro propone il passo successivo della busta:

- «✅ Pronta», «▶ In lavorazione», «✅ Consegnata»; per il Compro Oro «✅ Valutata», «✅ Perfezionata»,
  «✅ Chiusa».
- «💰 Preventivo» mette la busta in preventivo e apre la scheda sul preventivo; «✅ Pronta» apre la
  scheda sul prezzo finale.
- Consegna, valutazione e perfezionamento chiedono conferma nel riquadro: «Sì» conferma, «Annulla»
  torna ai pulsanti.
- Dopo le altre azioni compare «✓ consegnata» (o lo stato nuovo) e si torna alla lista con i filtri di
  prima.
- Se la busta è ancora dall'artigiano, la conferma lo dice e la busta risulta anche rientrata.
- Una busta consegnata, annullata o chiusa ha solo «👁 Consultazione».
- Se qualcun altro ha appena cambiato la stessa busta, il riquadro dice «Stato cambiato nel frattempo:
  ricontrolla.» e mostra i pulsanti giusti per lo stato di adesso.
- Dopo «✅ Valutata» il messaggio WhatsApp al cliente non parte ancora: arriva con gli avvisi.

## Il suono

Sul telefono (e sul computer con le casse accese) il programma suona l'esito della lettura:

- **un bip acuto** quando la busta è trovata o l'azione è fatta;
- **due bip gravi** quando il codice non è di nessuna busta o l'azione è rifiutata.

Dove il telefono lo permette (Android) vibra anche. L'esito lo decide il programma dopo aver cercato la
busta, non la sola lettura: un codice letto bene ma sconosciuto suona i due bip gravi.

Il suono parte solo dopo che hai toccato 📷 almeno una volta sulla pagina: è una regola dei telefoni.
Per questo un codice scritto a mano dal computer, o letto con la pistola senza aver mai toccato 📷,
resta muto.

Nel riquadro c'è l'interruttore 🔊/🔇: spegne e riaccende i suoni del QR **su quel telefono o
computer** (gli altri non cambiano). Riaccendendolo suona un bip di prova. Su iPhone, con
l'interruttore laterale del silenzioso attivo, il suono può non sentirsi sui modelli più vecchi.

## In «Nuova riparazione» e nella modifica della busta

Accanto a «Etichetta (0045/26)» c'è «📷 Scansiona»: apre la stessa finestra della fotocamera. Il codice
letto va nel campo e suona il bip acuto; il programma non cerca niente e non salva: la busta si salva
come sempre con «Genera busta» (o «Salva modifiche» nella modifica).

Scritto a mano, il campo accetta il numero dell'etichetta («45», «0045/26») o il codice intero
(«RIP-2026-0045», anche in minuscolo: si salva in maiuscolo). Gli spazi all'inizio e alla fine si
tolgono sempre; quelli accanto ai trattini o alla barra («0045 / 26») solo se così il codice diventa
un'etichetta.

Se il testo somiglia a un'etichetta ma non ha una forma riconosciuta, per esempio «45/2026» o
«0045-26», sotto il campo compare l'avviso «⚠ «45/2026» non è un'etichetta riconosciuta: scrivila
come 0045/26 o 45, oppure premi di nuovo «Genera busta» per salvarla così com'è.» (nella modifica
dice «Salva modifiche»). Correggendo il testo si salva normalmente; premendo di nuovo senza
cambiarlo, il codice si salva com'è.

## Senza JavaScript

Se il browser non esegue JavaScript (raro: un browser molto vecchio o con gli script spenti) la
ricerca funziona lo stesso, ma il riquadro della busta si apre in una pagina a parte, il 📷 della lista
cerca il testo scritto nel campo come una lettura e non apre la fotocamera, e non c'è il suono.
