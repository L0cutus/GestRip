# 059 — Pistola che suona, 📷 di «Etichetta» senza fotocamera, D-78, D-79, capitolo 06 del manuale (T4.2, undicesima e ultima)

- **Fonte:** coordinatore, 08/11/2026, con le decisioni di Stefano del 07/11/2026 (`docs/SVILUPPO.md`,
  «Decisioni di Stefano del 07/11/2026»: D-78, D-79 e le due voci senza D) e la ripresa 058. Lo
  standalone a `go-baseline` = `302439d` (slice 225): `assets/js/qr_scanner.js` (`open`, `close`,
  `switchCamera`, `rearCameras`), `assets/js/qr_sound.js` (`play`, `unlock`), `assets/js/app.js`
  (`phx:qr-outcome`), `WorkshopLive` (`#qr-lookup-form`, `#qr-lookup-code`, `#qr-lookup-scan`,
  `#repair-qr-code-scan`, `qr_lookup`, `qr_outcome/2`), `Repair.missing_qr?/1` e `missing_qr_query/1`.
- **Voci:** RIP-H-27 (suono della pistola), RIP-H-28 (📷 di «Etichetta», linea di mira, «Cambia
  fotocamera» con due fotocamere), RIP-H-03 e RIP-H-12 (segno «⚠ senza QR»), RIP-H-22 (la scansione
  sopra la lista, nelle schermate).
- **Deviazioni:** **D-78 fatta**, **D-79 fatta** (il segno e il titolo; contatore e filtro come lo
  standalone, Dubbio 1); D-63 per i campi `solo_go` della pistola.
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

### A. Il lettore a pistola con un'etichetta sconosciuta (RIP-H-27, senza D)

- **Server** (`internal/web/cerca.go`): `esitoRicercaScritta(cerca, trovate)` dà `"ko"` quando il testo
  ha la forma esatta di un'etichetta (`buste.EtichettaDi`, quindi anche «rip-2026-0999», « RIP-2026-
  0999 », «RIP - 2026 - 0999», come `NormalizzaCodice`) e la ricerca scritta non trova nessuna busta;
  altrimenti `""`. `smista` lo passa a `disegnaLista` solo per la lista disegnata dal POST del campo
  unico (`datiHome.EsitoQR`), e `home.html` scrive `<div id="repair-search-outcome"
  data-qr-esito="ko" hidden>`. La GET della home e `GET /cerca` non lo hanno mai.
- **Prima**, nel POST: un'etichetta che è il codice di una busta apre ancora il riquadro con il suo
  esito `ok` (055); un'etichetta che la ricerca testuale trova (per esempio scritta nelle note di due
  buste) dà la lista senza esito, e con una sola busta la scheda; un nome, un numero, «RIP-2026-45»,
  «RIP-2026-0000» o «RIP 2026 0999» restano muti.
- **Script** (`app.js`, `risposta`): quando la risposta al `fetch` del campo unico è la lista con
  `#repair-search-outcome` ko, la lista prende il posto della pagina (`mostraPagina`, senza cambiare
  indirizzo, come prima) e poi `suonoQR.suona(false)`: i due bip gravi (330 Hz) e `[120, 80, 120]`,
  con le regole della 055 (l'audio sbloccato dal tocco su 📷, l'interruttore `gs-qr-sound`). Mai per le
  risposte dei moduli del pannello. Senza JavaScript la lista è la stessa e non suona niente.
- Come nello standalone, una pistola che non ha mai visto un tocco su 📷 nella pagina resta muta: il
  manuale dice di toccare 📷 e «Chiudi» dopo aver aperto GestRip.

### B. D-78, la linea di mira (RIP-H-28)

- C'era dalla 055 (`.gs-lettore-mira`). Resa un po' più visibile (3 px invece di 2, un bordo scuro
  sottile oltre al bagliore rosso, `margin-top: -1px` per restare a metà): non cambia la lettura
  (`pointer-events: none`, jsQR legge tutto il fotogramma).
- Ora si vede **solo sopra un video acceso**: nascosta all'apertura, accesa da `mostra`, spenta da
  `chiudiLettore` e quando un cambio di fotocamera non riuscito spegne il video. Senza fotocamera la
  riga restava sospesa nel vuoto sopra la pagina (visto nella schermata `06-senza-fotocamera`).
- Nel manuale (capitolo 06, «Leggere un'etichetta con la fotocamera») e nella schermata
  `06-fotocamera-telefono.png`. `docs/SVILUPPO.md`: D-78 «fatta con la 059».

### C. Il 📷 di «Etichetta» (RIP-H-28, senza D)

- Apriva già la fotocamera dalla 055 (`#repair-qr-code-scan`, solo `data-target`): il codice letto va
  nel campo, la pagina resta dov'è, il bip acuto. Non cambia.
- **Nuovo, quando la fotocamera non si apre** (`senzaFotocamera` in `app.js`): contesto non sicuro,
  browser senza `mediaDevices`, `getUserMedia` rifiutato (nessuna fotocamera, «Requested device not
  found»; permesso negato, «Permission denied»; fotocamera occupata). La finestra scrive il messaggio
  come il lettore della home e segna il campo di `data-target` come ripiego, solo per un 📷 **senza**
  `data-submit` (quindi non quello della home, dove il campo unico è già quello della pistola).
  Chiusa la finestra con «Chiudi» o Esc (`chiudiDaUtente`, che è anche il `chiudi` usato da Esc), il
  cursore va nel campo «Etichetta». La chiusura per una lettura, per un cambio di pagina o per una
  nuova apertura (`chiudiLettore`) azzera il ripiego e non sposta niente.
- Vale anche nella modifica della busta (stesso pulsante in `scheda.html`).
- Senza JavaScript il 📷 di «Etichetta» non fa niente, come prima.

### D. D-79, «⚠ senza QR» sulle buste con un QR di altro formato (RIP-H-03, RIP-H-12)

- **Regola** (`internal/buste/visibile.go`): `QRAltroFormato(stato, codice)` è vero per una busta
  **aperta** con un codice non vuoto che non è un'etichetta (`EtichettaDi`): «SINT-QR-0045», «RIP
  2026 0045», «RIP-2026-45», «RIP-2026-0000». Mai insieme a `SenzaQR` (che resta
  `Repair.missing_qr?/1`).
- **Lista** (`rigaDellaLista`, `home.html`): il riquadro porta «⚠ senza QR» anche per quelle
  buste, con il titolo «Busta senza etichetta RIP: il suo QR è di un altro formato»
  (`rigaHome.AltroFormato`); la busta senza codice tiene «Busta senza codice QR: aggiungilo dalla
  scheda». La galleria non ha il segno (non lo ha nemmeno lo standalone).
- **Ricerca per numero d'etichetta (055d)**: invariata. Ogni busta trovata per il numero interno ha il
  segno, anche chiusa e anche con un QR di altro formato, con «Busta senza etichetta: trovata con il
  numero interno» (vince sugli altri due titoli). Lo **storico** di un'etichetta dalla scheda
  (`/cerca?cerca=0045/26`) è questa ricerca, quindi vale lo stesso.
- **Non cambiati, come lo standalone** (Dubbio 1): il riquadro «⚠ Senza QR» in alto, il filtro
  `senza_qr` («⚠ Solo buste aperte senza QR») e la sua pillola contano solo le buste senza codice
  (`missing_qr_query/1`). Con i dati del banco il riquadro dice «⚠ Senza QR: 5» mentre i riquadri
  segnati sono di più (quelli con i codici SINT-…). Il manuale lo dice.
- **Scheda** («⚠ senza QR» accanto al titolo): invariata, solo le buste senza codice (Dubbio 3).
- `docs/SVILUPPO.md`: D-79 «fatta con la 059», con quello che resta come lo standalone.

### E. Il capitolo 06 del manuale

`docs/manuale/06-ricerca-e-qr.md`, ora «La lista, la ricerca e il QR» (anche nel sommario), riscritto per
tutta la T4.2:

- il numero delle buste (etichetta, «N. 45/26», l'anno dell'etichetta, «⚠ senza QR» con i due titoli,
  il contatore e il filtro che contano solo le buste senza codice);
- la lista: contatori, i segni del riquadro, l'ordine, le pagine; i pulsanti «✓ Pronta», «✓
  Consegnata», il menu dello stato, il 📌, il messaggio in cima, «← Indietro»;
- i filtri del cassetto, le pillole, «× Azzera filtro», «⧉ Schede PDF»; la galleria;
- il campo unico: ricerca scritta, ricerca per numero d'etichetta, fotocamera (linea di mira, «Cambia
  fotocamera», permesso, connessione sicura, QR a specchio), pistola (con i due bip per un'etichetta
  sconosciuta), riquadro della busta, gemelle, suono (anche per la pistola, con il tocco su 📷 per
  sbloccarlo);
- il 📷 di «Etichetta» (anche senza fotocamera), il campo scritto a mano, «Senza JavaScript».

Nove schermate nuove, sul telefono (390 px, 2 pixel per px), dal banco su dati sintetici (sotto,
«Schermate»). Il glossario ha sette voci nuove (Galleria, Lettore a pistola, Linea di mira, Numero
interno, Pillola, QR di altro formato, Senza QR).

Nel capitolo 03 gli esempi della barretta della coda hanno il numero visibile (D-64): «📷 Busta 0045/26:
0/2 foto, 0%» (con «N. 45/26» senza etichetta), «❌ Busta 0046/26: …», e la didascalia della figura 3.2
non dice più «21/26».

`docs/manuale/da-scrivere.md`: tolte la sezione «Lista delle buste» (056 e 057) e le righe «N. 45/26» e
«0045/25» del numero, passate nel capitolo 06; restano la grandezza della galleria nelle impostazioni
(da fare) e l'import (per il capitolo dell'amministrazione), messe sotto due titoli loro.

### F. Banco

- **Scenario browser `lettore-qr`, tre prove nuove** (`banco.mjs`, `argomentiFotocamera`; attese in
  `bench/browser/lettore-qr.json`), ognuna nel suo Chromium:
  - `vuota` (desktop, home, **pistola**): la fotocamera finta di Chromium senza file (un disegno senza
    QR); il tocco su 📷 e «Chiudi» sbloccano l'audio, poi «rip-2026-0999» scritto con Invio nel campo
    unico del Go o in «Cerca per QR busta» dello standalone: **«330 330» e `[120,80,120]` nelle due
    app**, indirizzo `/`. Poi «Cliente inesistente»: muto nel Go, «330 330» nello standalone (campo
    `solo_go`, D-63: lì il campo legge ogni testo come un QR);
  - `nessuna` (telefono, «Nuova riparazione», **senza fotocamera**): nessuna opzione, `getUserMedia`
    dà «Requested device not found»; il messaggio «⚠ Requested device not found» nelle due app; dopo
    «Chiudi» e dopo Esc il cursore è in `repair_qr_code` nel Go (`solo_go`; lo standalone lo lascia sul
    `body`), e nel Go Esc chiude la finestra anche qui (`solo_go`: nello standalone resta aperta);
  - `due` (telefono, «Nuova riparazione», **due fotocamere**, `--use-fake-device-for-media-stream=
    device-count=2`): `fake_device_0`, «🔄 Cambia fotocamera» → `fake_device_1` con «Inquadra il codice
    QR…» e la scelta salvata in `gs-camera-qr`, di nuovo → `fake_device_0`, la fotocamera spenta alla
    chiusura: **uguale nelle due app**. È il «Cambia fotocamera» che la 055 non poteva confrontare.
- **D-79 nel livello HTTP:** 108 righe D-79 in `bench/deviazioni.json`, una per campo, nelle pagine
  della home dei dati importati (`home-filtri` 61, `home` 19, `home-contatori` 14, `home-dipendente` 14): il testo
  del riquadro (`visibile:.rep-card@N/div@1`) con « ⚠ senza QR» in più, il segno
  (`visibile:repair-noqr-N`) e il suo titolo. Le ha scritte uno script usa e getta
  (`to-be-deleted-after-use/slice-059/d79/righe.py`) dalle 108 inattese del primo giro, accettando solo
  le differenze che sono **esattamente** l'aggiunta di « ⚠ senza QR» o del segno con il titolo della
  D-79 (nessuno scarto). Sul database vuoto nessuna di queste pagine ha buste con codici SINT-….
- **Schermate del manuale** (`manuale.mjs`): otto ricette nuove (`lista`, `filtri`, `galleria`,
  `cerca-etichetta`, `pannello-qr`, `gemelle-qr`, `fotocamera`, `senza-fotocamera`); `fotocamera`
  gira in un secondo Chromium con due fotocamere finte e il permesso dato (`CON_FOTOCAMERE`), le altre
  nel Chromium di sempre, che non ha fotocamere. `galleria` carica prima tre foto inventate sulla 26/26
  e sulla 27/26 (la prima pagina della lista non ha foto nei dati sintetici).

## Deviazioni

- **D-78** (Stefano 07/11/2026): fatta (sopra, B). Nel banco non si confronta (è dentro la finestra).
- **D-79** (Stefano 07/11/2026): fatta per il segno e il titolo nella lista (sopra, D); 108 righe in
  `bench/deviazioni.json`.
- **D-63**: due campi `solo_go` della prova `pistola` (un nome scritto nella pistola).
- **Senza D:** il suono della pistola (come lo standalone); il cursore nel campo «Etichetta» dopo una
  fotocamera che non si apre è un'aggiunta del Go (Dubbio 2), campo `solo_go` con il motivo.

## Prove

- **Test Go nuovi:**
  - `internal/web/pistola_qr_test.go`: `TestPistolaEtichettaSconosciuta` (titolare e dipendente; cinque
    forme d'etichetta sconosciute con l'esito ko e la lista vuota, sette testi senza esito, l'etichetta
    nelle note di due buste senza esito, l'etichetta di una busta col riquadro ok, una ricerca con una
    busta che apre la scheda, tre GET senza esito), `TestEsitoRicercaScritta` (undici casi),
    `TestScriptDellaPistola`, `TestScriptDelRipiegoEtichetta` (i tre casi della fotocamera che non si
    apre, il ripiego solo senza `data-submit`, `chiudiDaUtente` per «Chiudi» ed Esc),
    `TestScriptDellaMira` (D-78: nascosta, accesa, spenta, CSS).
  - `internal/buste/altro_formato_test.go`: `TestQRAltroFormato` (dodici casi; mai insieme a `SenzaQR`).
- **Test cambiati:** `TestCercaQRMessaggi` (la pistola con «RIP-2026-0999» ha ora l'esito ko della
  lista), `TestHomeRicercaNumeroInterno` (fuori dalla ricerca per numero la busta aperta con
  «QR-ALTRO» ha il segno col titolo della D-79), `TestHomeRiquadroBusta` (la busta pronta con
  «SINT-QR-25» ha il segno).
- `tools/banco`: `TestScenariBrowserDelRepo` e `TestSchermateDelRepo` leggono le voci nuove.

## Mutazioni

`to-be-deleted-after-use/slice-059/mutazioni/muta.py`, 25 punti, test mirati di `internal/web` e
`internal/buste` in una copia del repo nello scratchpad (cancellata dopo): **24 uccise, 1 equivalente**.

Uccise: l'esito anche con buste trovate, solo per «RIP-» maiuscolo esatto, mai passato alla lista, ko per
ogni testo sconosciuto; il `div` dell'esito tolto; l'esito anche per le risposte del pannello, col bip
di successo, ignorato; il ripiego anche per il 📷 della home, la chiusura senza cursore nel campo, il
ripiego non azzerato, «Chiudi» ed Esc senza ripiego, il permesso negato e il contesto non sicuro senza
ripiego; la mira visibile senza video, mai accesa, rimasta dopo un cambio fallito; il QR di altro
formato anche per una busta chiusa, senza codice, con un'etichetta; la lista senza la D-79, il titolo
della D-79 uguale a quello senza codice; il contatore che conta anche le buste della D-79.

Equivalente: nella ricerca per numero d'etichetta non rimettere `AltroFormato` a falso. Il modello
guarda `NumeroInterno` prima di `AltroFormato`, quindi il titolo resta «trovata con il numero
interno» (lo prova `TestHomeRicercaNumeroInterno`, caso «0014»); il campo si azzera lo stesso per
chiarezza.

Il comportamento nel browser (il suono della pistola, il cursore nel campo, «Cambia fotocamera») lo
provano le prove nuove di `lettore-qr`, non le mutazioni.

## Banco

`gestrip-standalone:fab-225`, binari dell'albero di lavoro.

- **Giro mirato** (`s059m`, dati importati; HTTP `healthz`, `cerca-qr`; browser `lettore-qr`): HTTP 2
  scenari, 33 passi, 0 inattese; browser 1 scenario, 2 viste, **0 inattese** al primo colpo: le tre
  prove nuove danno nelle due app quello che le attese dicono, e le note dello standalone sono quelle
  dei campi `solo_go` («330 330» per il nome nella pistola, il cursore sul `body`, la finestra che resta
  aperta con Esc).
- **Primo giro completo, dati importati** (`s059`): HTTP 48 scenari, 886 passi, **108 inattese**, tutte
  D-79 (« ⚠ senza QR» in più nei riquadri delle buste con un codice SINT-…, il segno e il suo titolo);
  diventate le 108 righe di `bench/deviazioni.json` (sopra, F). Il livello browser non è partito.
- **Secondo giro, dati importati** (`s059`, da capo): HTTP 48 scenari, 886 passi, 1345 attese, **0
  inattese**; browser 12 scenari, 24 viste, 30 attese, **0 inattese**: verde. Il binario era quello di
  prima del ritocco della linea di mira (nascosta senza video).
- **Giro finale, dati importati, binario finale** (`s059f`): HTTP **48 scenari, 886 passi, 1345 attese,
  0 inattese**, 0 errori; browser **12 scenari, 24 viste, 30 attese, 0 inattese**, 0 da decidere, 0
  errori: **verde**.
- **Giro finale, database vuoto** (`s059v`): HTTP 48 scenari, 886 passi, 158 attese, **0 inattese** (30
  non provati con questi dati); browser 12 scenari, **0 inattese** (11 non provati; `lettore-qr` si
  prova anche sul vuoto, con «rip-2026-0999» sconosciuta in tutte e due le app): **verde**.
- **Schermate:** `m059`, `m059b`, `m059c` per le prove delle ricette (galleria senza foto nella prima
  pagina, poi con le foto della ricetta), `m059d` con il binario finale, `m059e` per la galleria
  ristretta (sotto).
- `banco giu` dopo ogni giro: nessuna istanza resta accesa.

## Schermate

Nove PNG nuovi in `docs/manuale/img/`, sul telefono, da `banco manuale` con l'elenco ridotto alle
nove voci del capitolo 06, con il binario finale (istanza nuova `m059d`; la galleria rifatta da sola su
`m059e` dopo aver ristretto il ritaglio alla prima cella, perché con due celle pesava 301 KB, sopra
l'avviso dei 250 KB), uscita nella cartella temporanea e poi copiata; le altre quindici schermate non
sono state rifatte. In tutto 1,07 MB:

| File | Cosa mostra |
|---|---|
| `06-lista-telefono.png` | contatori, campo unico, cassetto chiuso, le prime tre buste con «✓ Pronta», 📌 e il menu |
| `06-filtri-telefono.png` | pillole «⏰ In scadenza (12 giorni)», «In lavorazione», «× Azzera filtro» e il cassetto aperto |
| `06-galleria-telefono.png` | «☰ Lista / 🖼 Galleria», «2 schede con foto · 28 senza foto nascoste», la prima cella con «+1» |
| `06-senza-qr-telefono.png` | la 36/26 (SINT-ALTRO-36): «N. 36/26» con «⚠ senza QR» (D-79) |
| `06-cerca-etichetta-telefono.png` | la ricerca «0045»: le gemelle #0045/26 (una consegnata) e #0045/25 |
| `06-pannello-qr-telefono.png` | il riquadro sopra la lista per RIP-2026-0047: stato, «✅ Pronta», «💰 Preventivo», 🔊 |
| `06-gemelle-qr-telefono.png` | SINT-QR-0005: «2 riparazioni con questo QR — scegli:» |
| `06-fotocamera-telefono.png` | la finestra dopo «🔄 Cambia fotocamera» (seconda fotocamera finta) con la linea di mira |
| `06-senza-fotocamera-telefono.png` | il 📷 di «Etichetta» senza fotocamera: «⚠ Requested device not found», senza linea di mira |

- **Due fotocamere finte:** Chromium headless le dà (`--use-fake-device-for-media-stream=device-count=2`,
  provato prima nel Chromium del banco: `fake_device_0` e `fake_device_1`). Il video è il disegno di
  Chromium (un cerchio che gira e il contatore del tempo), non un QR: con un QR la finestra si
  chiuderebbe da sola. Il PNG cambia a ogni giro.
- **Il riquadro via `fetch` fuori dalla lista: il Go non lo prevede.** In `app.js` il `fetch` del campo
  unico parte solo se la pagina ha `#repairs` o `#repair-gallery` (`nellaLista`). Fuori dalla lista il
  campo unico c'è solo sulla pagina della scansione (`POST /cerca-qr`), a cui si arriva senza
  JavaScript: lì il modulo è un POST normale. Non c'è quindi una schermata «fuori dalla lista»; il
  pannello sopra la lista c'è (`06-pannello-qr-telefono.png`, `06-gemelle-qr-telefono.png`).

## Controlli

- `scripts/check` verde sul codice finale (gofmt, vet, staticcheck, errcheck, build, test, copertura:
  353 voci, 26 chiuse, 0 rosse, 88 parziali, 228 aperte, 11 fuori v1; segreti e dati veri OK), anche
  dopo aver copiato le schermate.
- `scripts/parole-vietate --staged` dopo `git add -A`: pulito; poi `git reset`.

## Per il manuale

- Il lettore a pistola con un'etichetta che non è di nessuna busta suona i due bip gravi (dopo un
  tocco su 📷 nella pagina); un testo che non è un'etichetta resta una ricerca muta.
- La finestra della fotocamera ha la linea rossa di mira, solo quando c'è il video.
- Il 📷 di «Etichetta» senza fotocamera o senza permesso: il messaggio del browser, e chiusa la finestra
  il cursore è nel campo per la pistola.
- «⚠ senza QR» anche sulle buste aperte con un QR di altro formato, con il suo titolo; il riquadro e il
  filtro «⚠ Senza QR» contano solo quelle senza codice.

Tutto scritto nel capitolo 06 (con la lista, i filtri, la galleria e i pulsanti delle slice 056-057) e
nel glossario; gli esempi del capitolo 03 corretti.

## Non portato

- **La grandezza della galleria (`galleria_dim`) nelle impostazioni:** la pagina delle impostazioni del
  Go non la ha (T4.15); resta in `da-scrivere.md` come «da fare».
- **Il numero visibile nei messaggi e nella pagina di stato** (T4.5–T4.7), come dice la D-64.
- **Il riquadro via `fetch` fuori dalla lista:** il Go non lo prevede (sopra, «Schermate»); la proposta
  della 055 (Dubbio 5) resta, senza una decisione.
- **Contatore e filtro «⚠ Senza QR» con la D-79, e il segno della D-79 nella scheda:** non decisi,
  lasciati come lo standalone (Dubbi 1 e 3).

## Dubbi

1. **D-79, contatore e filtro — domanda per Stefano.** Il riquadro «⚠ Senza QR» in alto, il filtro
   «⚠ Solo buste aperte senza QR» e la sua pillola contano ancora solo le buste senza codice, come lo
   standalone. Così il riquadro dice «5» ma nella lista i segni sono di più, e toccando il riquadro le
   buste con un QR di altro formato non escono. Se Stefano le vuole contate, è una riga in
   `senzaQRSQL` e nei contatori (e un'estensione della D-79 nel banco).
2. **Il cursore nel campo «Etichetta» dopo una fotocamera che non si apre** non c'è nello standalone
   (lì il cursore resta sulla pagina): l'ho fatto come chiesto, alla **chiusura** della finestra
   («Chiudi» o Esc), lasciando il messaggio visibile finché non la si chiude. L'alternativa era
   chiudere la finestra da sola e mettere il messaggio sotto il campo, ma il messaggio non sarebbe più
   «come nel lettore della home». Non ha una D: se serve, è un'estensione della D-63.
3. **D-79 nella scheda:** la scheda di una busta con un QR di altro formato mostra «N. 36/26» senza
   «⚠ senza QR» (solo le buste senza codice lo hanno, come lo standalone). La via diceva il riquadro
   della lista; se Stefano lo vuole anche nella scheda è una riga in `numeroDellaScheda` (e il titolo
   lì dice «aggiungilo da «Modifica»»).
4. **La pistola senza un tocco su 📷 resta muta,** come nello standalone (l'audio del browser si
   sblocca solo dentro un tocco). Su un PC con la pistola vuol dire toccare 📷 e «Chiudi» ogni volta
   che la pagina si ricarica (il manuale lo dice). L'interruttore 🔊 sblocca anche lui, ma si vede
   solo nel riquadro.
5. **I messaggi della fotocamera sono quelli del browser**, in inglese su Chromium («Requested device
   not found», «Permission denied»), come nello standalone (`"⚠ " + err.message`). Tradurli sarebbe un
   testo nuovo.
6. **Inventario privato** non toccato: le note «Portato (slice 059)» di RIP-H-27, RIP-H-28, RIP-H-03,
   RIP-H-12, D-78 e D-79 fatte, le scrive il coordinatore.

## Review


Review di un altro sottoagente, 08/11/2026, su pc-fisso (`~/sviluppo/gestrip-go`, albero non
committato), contro lo standalone a `go-baseline` (`qr_scanner.js`, `qr_sound.js`, `app.js`,
`Repair.missing_qr?/1` e `missing_qr_query/1`) e le «Decisioni di Stefano del 07/11/2026».

**Guardato:**
- **Bip della pistola:** `esitoRicercaScritta` dà `ko` solo con `buste.EtichettaDi` (la stessa
  regola dell'etichetta usata ovunque: «RIP-AAAA-NNNN», minuscolo e spazi di `NormalizzaCodice`; mai
  «RIP-2026-45», «RIP-2026-0000», «RIP 2026 0999», un nome o un numero) e zero buste trovate. Arriva
  solo dal POST del campo unico che disegna la lista (`smista` con `chiavi`): la GET della home,
  `GET /cerca` (redirect) e la scheda di un risultato non lo hanno. Il riquadro di un'etichetta
  trovata viene prima (`CercaPerQR`), quindi un'etichetta esistente non può dare `ko` per colpa dei
  filtri. `data-qr-esito` porta solo «ko»: nessun dato in più (il testo cercato è già nella pillola e
  nel campo). Senza JavaScript non c'è audio. Lo script calcola l'esito prima di `mostraPagina`, solo
  per il modulo del campo unico (`!dalPannello`), e suona con `suonoQR.suona(false)` (sblocco,
  interruttore, vibrazione della 055).
- **`app.js`, lettore:** il ripiego vale solo per il 📷 senza `data-submit` (quello della home ce
  l'ha, `home.html:212`); `chiudiLettore` lo azzera per lettura, doppio tocco (`apri` chiude prima),
  cambio di pagina (`gs:pagina`: dopo un errore `s.apertura` è ancora piena, quindi chiude) e
  `pagehide`; Esc passa da `lettoreQR.chiudi` = `chiudiDaUtente`, e sulla home senza ripiego non
  sposta niente. La mira si accende solo in `mostra` e si spegne in `chiudiLettore` e nel cambio
  fallito; «Una sola fotocamera» la lascia accesa (il video c'è ancora). `node --check` pulito.
- **D-79:** `QRAltroFormato` esclude le buste chiuse (`Chiuso`: consegnata, annullata, chiusa) e
  senza codice; il cestino non arriva mai a `rigaDellaLista` (filtro di `indice.go`). Mai insieme a
  `SenzaQR`; nella ricerca per numero vince «trovata con il numero interno».
- **Banco, 108 righe D-79:** con uno script mio
  (`to-be-deleted-after-use/review-059/controlla_d79.py`) tutte e 108: 104 testi di riquadro
  (`visibile:.rep-card@N/div@1`) dove il Go è **esattamente** lo standalone con « ⚠ senza QR» inserito
  una volta; 2 `visibile:repair-noqr-N` («(assente)» → «⚠ senza QR») e 2 `attributo:…:title`
  («(assente)» → il titolo della D-79). Nessun doppione, nessuna riga `togli` o browser, nessuna busta
  consegnata o annullata nei testi; le buste sono quelle con codici SINT-… della semina (`stati.go`,
  `numeri.go`, `home.go` del banco). `copre` confronta i due valori interi, quindi le righe non possono
  nascondere un'altra differenza nello stesso campo.
- **Manuale e schermate:** le nove `06-*.png` guardate una per una: solo dati sintetici («Cliente
  Sintetico …», «Negozio», codici SINT-…, il disegno della fotocamera finta, foto inventate). Tutte le
  immagini citate nei capitoli esistono. Il testo del capitolo 06 confrontato col codice (contatori
  condizionali, 30 per pagina, ordine 📌/messaggi, «Pinna in cima»/«Togli pin», conferma della
  consegna, pillola del periodo, selezione del campo, messaggi della fotocamera): corrisponde.
- **Ripresa e piano:** la ripresa dice che la T4.2 è chiusa e che ci si ferma in attesa di Stefano;
  la riga della T4.2 in `PIANO.md` segue lo stesso schema della T4.4 («finito con la …; resta …»).
- **Differenze dallo standalone:** pistola muta per i testi non-etichetta (D-63, `solo_go`), cursore
  nel campo (Dubbio 2, `solo_go`), Esc che chiude su «Nuova riparazione» (dalla 055), contatore,
  filtro e scheda (Dubbi 1 e 3). Non ho trovato differenze non dichiarate.
- **Controlli:** `scripts/check` verde prima e dopo le correzioni; `scripts/parole-vietate --staged`
  pulito.

**Corretto (piccole cose, solo testo):**
- Capitolo 06: «Tenendo il dito (o il mouse) sul segno» → «Sul computer, tenendo il mouse sul
  segno»: sul telefono il titolo di un elemento non si vede tenendo il dito.
- Capitolo 06, figura 6.8 e `bench/manuale/schermate.json`: «Un'etichetta su due buste aperte» →
  «Un codice QR (qui di altro formato) su due buste aperte»: SINT-QR-0005 non è un'etichetta.
- Glossario, «QR di altro formato»: «La busta **aperta** che lo ha porta «⚠ senza QR»».
- `pistola_qr_test.go`: il commento diceva di provare il 📷 della modifica, che lì non si prova;
  ora rimanda a `TestModificaApreIlModulo`, che lo prova. Nessun codice cambiato, quindi nessun test
  nuovo.

**Resta:**
- **BASSA** — `TestScriptDellaPistola`, `TestScriptDelRipiegoEtichetta` e `TestScriptDellaMira`
  cercano righe di testo in `app.js` e `app.css`: fermano un cambio del codice, ma il comportamento
  vero lo provano solo le prove browser di `lettore-qr` (pistola, nessuna fotocamera, due
  fotocamere). La mira nascosta senza video e il ripiego dopo un permesso negato (non solo «nessuna
  fotocamera») non hanno una prova nel browser.
- **BASSA** — Con jsQR che non si carica la mira si accende lo stesso sopra il video, anche se non si
  legge niente (il messaggio lo dice). Solo estetica.
- **BASSA** — Sulla pagina della scansione (`/cerca-qr`, fuori dalla lista) la pistola resta muta
  anche con un'etichetta sconosciuta: lì il modulo è un POST normale, la lista arriva come pagina nuova
  e l'audio non è sbloccato. È coerente con «il suono parte solo dopo aver toccato 📷 sulla pagina»,
  ma il manuale non lo dice in modo esplicito.
- **BASSA** — Nelle schermate i campi data mostrano «mm/dd/yyyy» e «11/08/2026» (Chromium in
  inglese), come i messaggi della fotocamera (Dubbio 5).
