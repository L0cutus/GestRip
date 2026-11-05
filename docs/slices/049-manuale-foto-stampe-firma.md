# 049 — Manuale d'uso: foto, stampe ed etichette, firma (prima slice del manuale)

- **Fonte:** PIANO, «Manuale d'uso» (Stefano, 03/11/2026): una slice di manuale alla fine di ogni
  tappa; la prima alla fine della T4.4 copre T4.3 e T4.4. Righe di partenza: `docs/manuale/da-scrivere.md`
  (sezioni «Foto (T4.3)» e «Stampe») e le sezioni «Per il manuale» delle ricevute 045, 045b, 046 e 047,
  più le ricevute 042, 043 e 044 per ricevuta, scheda PDF, etichetta busta e report.
- **Voci:** nessuna voce dell'inventario; nessun comportamento del programma cambiato.
- **Lavoro:** un sottoagente; ogni frase dei capitoli ricontrollata sul codice di adesso (template di
  `internal/web/pagine`, handler di `internal/web`, `internal/buste`, `internal/stampe`, `app.js`).

## Cosa c'è

- **Capitoli** in `docs/manuale/`, Markdown semplice (un solo `#` per file, niente HTML, immagini con
  percorsi relativi in `img/`, una regola per paragrafo, testi dei pulsanti e dei messaggi copiati dal
  codice, limiti scritti in numeri, chi può fare cosa detto esplicitamente):
  - `00-sommario.md`: i dodici capitoli previsti nell'ordine deciso, quelli non scritti segnati «(da
    scrivere)» senza link, e la regola dei due ruoli;
  - `03-foto.md`: regole comuni (formati, 25 MiB, 4 per volta, pulizia, Ultra HDR, foto della busta),
    foto in «Nuova riparazione», coda in background con la barretta, tentativi e «Riprova», foto
    rifiutate e ✕, pulizia della coda nel telefono, Safari privato, foto nella scheda, canali,
    visualizzatore, eliminazione;
  - `04-stampe-ed-etichette.md`: pulsanti della scheda, ricevuta, scheda PDF, etichetta busta
    76×127, regole comuni dei PDF (D-52, D-53, D-57), report (limite 50, senza foto, oggi senza
    pulsante), etichette QR 57×32 (modulo, limiti, numero «0045/26»), contatore e azzeramento, nuovo
    anno (D-66), scheda «Stampanti», stampa diretta delle etichette QR e dell'etichetta busta;
  - `05-firma-di-consegna.md`: quando si firma (stati), pagina di firma, conferma e consegna,
    «Firma mancante.», la firma nella scheda, «↺ Rifai firma» solo del titolare;
  - `99-glossario.md`: i termini usati sempre nello stesso senso, CUPS compreso («il sistema di stampa
    del computer»).
- **`docs/manuale/da-scrivere.md`:** tolte le sezioni «Foto» e «Stampe», passate nei capitoli;
  restano le due sezioni *(da fare)* della T4.2 e «Non per il manuale»; nuova sezione «Accesso (T1, da
  recuperare)» con le righe «Per il manuale» della 048.
- **Schermate automatiche:** 15 PNG in `docs/manuale/img/`, fatti da `banco manuale` (nuovo
  sottocomando, `tools/banco/manuale.go`) con lo script `tools/banco/browser/manuale.mjs` e l'elenco
  dichiarativo `bench/manuale/schermate.json`. Solo il Go, dati sintetici, le due viste del livello
  browser, nella stessa immagine `grgo-banco-browser` (lo script si monta al posto di `banco.mjs`:
  impronta invariata). Regole in `docs/SVILUPPO.md`, «Schermate del manuale». Ogni immagine nei
  capitoli ha il testo alternativo e una didascalia in una riga sotto.

Per rifarle tutte, su pc-fisso, dalla radice del repo:

    sg docker -c "$B/banco su -nome man -immagine gestrip-standalone:fab-222 -gestrip $B/gestrip -dati sintetici-import"
    sg docker -c "$B/banco manuale -nome man"
    sg docker -c "$B/banco giu -nome man"

## Prove

- `go test ./tools/banco`: `TestLeggiSchermate`, `TestLeggiSchermateRifiuta` (13 elenchi guasti),
  `TestSchermateDelRepo` (l'elenco del repo si legge, ogni ricetta c'è nello script),
  `TestComandoManuale` (protezioni del container, solo il Go, due soli segreti, `banco giu` toglie
  `-manuale`), `TestManualeOrchestrazione` (giro buono e secondo giro rifiutato, dopo il livello
  browser, senza dati sintetici, PNG guasto senza copie, container fallito, immagine vecchia),
  `TestRaccogliSchermateMancante`.
- Giri veri sul banco (istanza `s049`, `fab-222`, `-dati sintetici-import`): cinque giri durante la
  scrittura; due giri consecutivi su istanze nuove con lo stesso codice danno gli stessi file (vedi
  sotto). Totale circa 1 MB (1016 KB), il più grande 161 KB.
- `scripts/check` verde; `scripts/parole-vietate --file` pulito su ogni file nuovo.

## Review indipendente

Una review di un altro sottoagente ha trovato questi punti, tutti chiusi in questa slice:

- **Medi.** (1) La barra del visualizzatore è in basso (`app.css`, `.gs-viewer-bar`), non in alto:
  corretto. (2) Nella figura 4.6 il messaggio «Inviate N etichette…» non si vedeva, perché sta fuori
  da `main`: `banco manuale` ora ritaglia il riquadro che contiene **tutti** gli elementi del
  selettore, e la voce usa `.flash-info, main`; schermate rifatte con un giro nuovo, il messaggio si
  vede. (3) Le note interne della scheda PDF sono su sfondo giallo chiaro (#fffbe6), non grigio. (4)
  Tolta la frase sulle etichette a specchio (il lettore QR del Go arriva con la T4.2). (5) Le foto
  scelte in «Nuova riparazione» si tolgono quando si ricarica la pagina o si esce e si torna prima di
  «Genera busta» (chiave nuova a ogni pagina), non «dopo una riconnessione». (6) Dopo «accesso
  scaduto» le foto ripartono col login, ma se la coda si è fermata dopo 5 tentativi serve «Riprova».
- **Bassi.** Regola dei 4 detta chiara (4 per volta nella scheda, 4 per pulsante in «Nuova
  riparazione»); niente «cestino»; motivi della riga rossa completi («foto non leggibile adesso», «foto
  non più disponibile», i tre rifiuti); ✕ solo sulle righe rosse; PNG/WebP animati con i due testi
  veri; intestazione del report solo sulla prima pagina; l'avviso del formato piccolo guarda l'ultima
  misura usata; contatore unico, 0 dopo l'azzeramento con la proposta da 1, e quando la proposta del
  nuovo anno compare nel modulo (anno di oggi) e quando nella pagina intermedia (anno scritto più
  nuovo); «Azzera contatore» solo con il contatore sopra 0; il messaggio «Stampa diretta non
  disponibile…»; «canale» e «coda di stampa» usati nei capitoli, «Annullata» fra gli stati Compro Oro
  del glossario; «🏠 Lista» spiegato (capitolo 5 e glossario); «Firma mancante.» è una finestra del
  browser con «OK»; commenti di `manuale.mjs` sui tocchi alla pagina unificati; una riga del capitolo 3
  dice che «Choose Files» è il browser delle schermate e che sul telefono vero il campo è in italiano.

## Per il manuale

Il manuale stesso; nessun comportamento del programma cambiato.
