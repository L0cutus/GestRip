# 055 — Lettore QR con la fotocamera, suono, QR a specchio e pannello sopra la lista (T4.2, sesta)

- **Fonte:** le decisioni di Stefano per la 055 (via del coordinatore del 07/11/2026): RIP-H-28 come lo
  standalone, jsQR nel repo, D-52 provata con un QR trasposto, RIP-H-27 con l'esito del server, il
  resto della D-70 (Dubbio 9 della ricevuta 054). Lo standalone a `go-baseline` (`d3778df`):
  `assets/js/qr_scanner.js` (hook `QrScanner`), `assets/js/qr_sound.js` (`play`, `unlock`,
  `QrSoundToggle`), `assets/vendor/jsqr.min.js` e `docs/third-party/jsqr/`, `assets/js/app.js`
  (`phx:qr-outcome`), `WorkshopLive` (`qr_outcome/2` e dove si chiama, `#qr-lookup-scan`,
  `#repair-qr-code-scan`, `#qr-sound-toggle`, `qr_close`, `qr_escape`, il riquadro `#qr-lookup`),
  i test `qr_scanner_asset_test.exs` e `qr_sound_asset_test.exs`.
- **Voci:** RIP-H-28 (lettore con la fotocamera), RIP-H-27 (suono e vibrazione dell'esito), RIP-H-22
  (la scansione dentro la lista, le gemelle nello stesso riquadro), D-52 (QR a specchio).
- **Deviazioni:** D-70 completata (sotto); D-63 e D-64 come nella 052-054. Nessuna D-xx nuova.
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave. Niente review indipendente in
  questa slice.

## Cosa c'è

- **jsQR nel binario** (`internal/web/statici/jsqr.min.js`, `LICENSE-jsqr.txt`, `guscio.go`):
  - il file dello standalone, che è byte per byte `dist/jsQR.js` di `jsqr@1.4.0` (controllato contro
    unpkg: stessa impronta; l'1.3.1 e l'1.2.0 sono diversi). Licenza Apache-2.0: il file di jsQR non
    ha un'intestazione, quindi la licenza (il testo di `jsqr@1.4.0`, invariato) sta accanto, con
    origine, versione e sha256 `bc40c8a1…0859`, come i font in `internal/pdf/caratteri/LICENSE`. Il
    repo non ha un elenco unico delle licenze di terzi: jsQR è nella tabella «Strumenti a versione
    fissata» di `docs/SVILUPPO.md`;
  - servito come gli altri asset (`/assets/jsqr.min-<hash>.js`, cache di un anno, nessuna sessione).
    `app.js` lo carica solo al primo tocco su 📷: `caricaAsset` scrive l'indirizzo con l'hash al
    posto del segnaposto `"__JSQR__"` di `app.js` (jsQR prima di `app.js`, così il suo indirizzo entra
    nell'hash di `app.js`). Niente CDN, niente inline: la CSP del Go (`base-uri`, `frame-ancestors`)
    non vieta gli script, e lo script si carica dalla stessa origine.
- **Il lettore** (`app.js`, `lettoreQR`; `app.css`), `qr_scanner.js` nella lettura singola:
  - il tocco su `#qr-lookup-scan` (campo unico della home) o `#repair-qr-code-scan` («Etichetta» di
    «Nuova riparazione» e della modifica) fa `blur`, sblocca l'audio e apre la finestra
    `#qr-scanner-modal`: il video con la **linea di mira** (rossa, a metà altezza), la riga di stato
    `#qr-scanner-status`, «🔄 Cambia fotocamera» (`#qr-scanner-switch`) e «Chiudi»
    (`#qr-scanner-close`); Esc chiude la finestra;
  - fotocamera posteriore con la risoluzione della sorgente (1280×720 ideali), poi la principale per
    numero `camera2 N` fra le posteriori (etichetta `back|rear|environment|posterior`), la scelta in
    `gs-camera-qr` (lettura e scrittura dentro `try`), «Una sola fotocamera disponibile.»; una chiusura
    mentre la fotocamera si apre spegne il flusso arrivato;
  - contesto non sicuro prima della fotocamera assente, con i due messaggi della sorgente;
  - i fotogrammi su un canvas, jsQR con `inversionAttempts: "dontInvert"`; il codice nel campo di
    `data-target` con l'evento `input`; con `data-submit` il modulo parte con il 📷 come mittente
    (sulla home `via=qr`, la ricerca è una scansione, come senza JavaScript); senza, riempie solo il
    campo e suona il bip di successo (slice 193 dello standalone);
  - il 📷 della home ha `data-target="repair-search-code"` e `data-submit="repair-search-form"`; quello
    dell'etichetta solo `data-target`. Senza JavaScript il 📷 della home manda il modulo con `via=qr`
    (052) e quello dell'etichetta non fa niente. Il ripiego della 053 per la pistola (il 📷
    dell'etichetta che metteva il cursore nel campo) non c'è più: vedi il Dubbio 3.
- **D-52, i QR a specchio:** **jsQR 1.4.0 li legge già da sé**: in `decode` (`jsqr.min.js`), quando la
  griglia letta non si decodifica, la riprova scambiando righe e colonne («try mirroring the QR across
  the topLeft -> bottomRight line»). Una prima versione di `app.js` rifaceva la lettura sul fotogramma
  trasposto; il banco ha mostrato che **anche lo standalone** legge il QR trasposto e l'etichetta di
  Chromium, con lo stesso jsQR, quindi la trasposizione è stata tolta (raddoppiava il lavoro su ogni
  fotogramma senza codice). `TestJsQRNelBinario` fissa l'impronta di jsQR e quella riga; il banco lo
  prova con la fotocamera finta (sotto).
- **Il suono** (`app.js`, `suonoQR`; `cerca.go`, `pannello_qr.go`, `pagine/cercaqr.html`), `qr_sound.js`:
  - un bip acuto (1760 Hz, 0,12 s) per il successo, due gravi (330 Hz) per il fallimento, oscillatori
    Web Audio, `navigator.vibrate(60)` o `[120, 80, 120]`; `navigator.audioSession.type = "playback"`
    dove c'è (iPhone col silenzioso);
  - l'audio si sblocca solo nel tocco su 📷 (e sull'interruttore): un codice scritto a mano, o letto
    con la pistola senza aver mai toccato 📷, resta muto, come lo standalone;
  - l'interruttore `#qr-sound-toggle` (🔊/🔇, `gs-qr-sound`, ogni accesso allo storage dentro `try`,
    uno storage rotto lascia il suono acceso) sta nella testa del riquadro `#qr-lookup`, accanto a
    «Chiudi», come nello standalone; senza JavaScript è `hidden` e vuoto, lo disegna `app.js`;
  - **l'esito lo decide il server**, come `qr_outcome/2`: `data-qr-esito` su `#qr-lookup` della pagina
    della scansione (`datiCercaQR.Esito`): `ok` con una busta o con le gemelle, `ko` per «QR non
    fornito», «Nessuna riparazione trovata per questo QR», «Ricerca non riuscita.», una scelta non
    valida e, dalle azioni del pannello (`mostraQRKO`), busta assente, cambiata nel frattempo o azione
    rifiutata; **assente** per la scelta valida di una gemella e per la conferma disegnata dal server
    (lo standalone lì non suona). Un'azione riuscita è un redirect: `app.js` suona il successo quando
    la risposta a un modulo del pannello è un redirect a una pagina del personale.
- **Il pannello sopra la lista** (D-70, `app.js`):
  - con JavaScript, dove la pagina ha la lista (`#repairs`: la home, o la lista disegnata dal POST di
    una ricerca), il modulo del campo unico (🔍, Invio, lettore a pistola, e il 📷 dopo la lettura) e i
    moduli dentro `#qr-lookup` (azioni, «Sì», «💰 Preventivo» con il suo `formaction`, le gemelle)
    partono con `fetch` (POST con il CSRF e il valore del pulsante premuto);
  - la risposta con `#qr-lookup` (la pagina della scansione): quel riquadro va sopra la lista, subito
    dopo il campo unico, **senza** il campo unico della risposta (la lista ha il suo), con i messaggi
    della risposta («Stato cambiato nel frattempo: ricontrolla.»); la lista e i filtri restano. Le
    gemelle si scelgono nello stesso riquadro: la scelta è un altro `fetch` e il riquadro si sostituisce;
  - un redirect (la scheda di una ricerca con un risultato, la lista con «✓ consegnata» o la scheda
    sul prezzo dopo un'azione): la pagina del redirect prende il posto di questa con `mostraPagina`
    (040b) e il suo indirizzo nella cronologia, come senza JavaScript; la lista disegnata nella
    risposta (ricerca scritta con più risultati o nessuno) prende il posto di questa **senza** cambiare
    indirizzo (il testo può essere un codice letto dalla pistola); il login fa ricaricare la pagina;
    una risposta che non è del personale (il 403 del CSRF) si mostra com'è;
  - **il codice non va mai nell'indirizzo**: nel pezzo nuovo non c'è nessuna `pushState`,
    `replaceState` o `location` (`TestScriptDelPannelloSopraLaLista`); la cronologia la cambia solo
    `mostraPagina`, con l'indirizzo del redirect;
  - «Chiudi» (`#qr-lookup-close`, un link alla lista con i filtri) e Esc tolgono il riquadro (e i suoi
    messaggi) e rimettono il fuoco nel campo; la lista è ancora lì;
  - il doppio invio della 054 resta: l'ascoltatore che ferma il secondo invio di `#qr-action-form` viene
    prima di quello del `fetch` (che guarda `defaultPrevented`); un secondo invio della ricerca mentre
    la prima risposta non è arrivata si ignora; un errore di rete toglie il segno del doppio invio e
    dice «Connessione assente»;
  - senza JavaScript resta tutto come nella 052 e 054: `POST /cerca-qr` disegna la pagina della
    scansione e le azioni sono POST con il redirect.
- **Banco** (`tools/banco/fotocamera.go`, nuovo; `browser.go`, `istanza.go`, `browser/banco.mjs`,
  `bench/browser/lettore-qr.json`, nuovo):
  - `banco esegui` scrive tre video Y4M (640×480, 4:2:0, tre fotogrammi, 10 pixel per modulo) in
    `fotocamere/` dell'istanza, montata in sola lettura nel container del browser: il QR di
    `RIP-2026-0056` (la 37/26 consegnata dei dati importati) diritto e trasposto, e il QR
    dell'etichetta 5 di Chromium dello standalone (`internal/pdf/lettura/testdata/chromium-etichetta-5.pdf`,
    la fixture della 043, trasposta, `SINT-QR-0005`), ridisegnato modulo per modulo dai rettangoli del
    PDF (opzione `-etichetta-qr`). `scriviFotocamere` rifiuta un'etichetta che non sia trasposta;
  - scenario browser **`lettore-qr`** (RIP-H-22, RIP-H-27, RIP-H-28), nelle due app, con un Chromium
    per fotocamera (`--use-fake-device-for-media-stream`, `--use-fake-ui-for-media-stream`:
    senza, il Chromium senza finestra risponde «Not supported»; `--use-file-for-fake-video-capture`),
    dopo gli altri scenari; il suono si osserva dagli oscillatori Web Audio e da `navigator.vibrate`
    con uno script messo prima di quelli della pagina:
    - *diritto, PC, home*: 📷, la lettura, il riquadro sopra la lista (stato, pulsanti, avviso), la
      finestra chiusa, l'indirizzo `/`, la lista presente, il suono `1760` e la vibrazione `60`, poi
      «Chiudi»: riquadro via, indirizzo `/`, lista presente. Sul database vuoto «⚠️ Nessuna
      riparazione trovata per questo QR», `330 330` e `[120,80,120]`;
    - *trasposto, PC, home*: lo stesso con il QR a specchio (D-52);
    - *etichetta, PC, home*: le gemelle 5/26 e 10/26 (SINT-QR-0005, aperte), «2 riparazioni con
      questo QR — scegli:» con due scelte, il bip, poi la prima scelta: il pannello nello stesso
      riquadro sopra la lista, l'elenco delle scelte via, indirizzo `/`, nessun suono (come
      `qr_select`);
    - *diritto* e *etichetta*, telefono, «Nuova riparazione»: il codice nel campo «Etichetta»
      (`RIP-2026-0056`, `SINT-QR-0005`), la finestra chiusa, l'indirizzo `/nuova`, il bip `1760`.
    Nessuna attesa `solo_go`: le due app danno lo stesso in ogni campo. Lo scenario legge soltanto.
  - `TestComandoBrowser` (il montaggio in sola lettura), `TestScenariBrowserDelRepo` (le voci),
    `TestEseguiOrchestrazione` (la fixture), `TestFotocamereFinte` (nuovo, sotto).

## Deviazioni

- **D-70** (approvata il 05/11/2026), **completata:** con JavaScript la scansione apre il pannello
  dentro la lista, senza passare da una pagina nuova (la 054 aveva le conferme); senza JavaScript la
  riserva `POST /cerca-qr` resta. Le azioni sono ancora POST del Go (con `fetch`): i campi `solo_go`
  `consegna.invii` e `lavorazione.invii` di `pannello-qr` restano.
- **D-52** (approvata il 03/11/2026): il lettore del Go legge i QR a specchio. Non è una differenza dal
  lettore dello standalone, che li legge anche lui (stesso jsQR): nessuna riga in `bench/deviazioni.json`.
- **D-63, conseguenza nota:** il lettore a pistola scrive nel campo unico; un codice che non è di
  nessuna busta è una ricerca scritta (lista vuota, risposta 1 del 05/11/2026) e quindi non suona.
  Nello standalone la pistola nel campo «Cerca per QR busta» dava i due bip gravi. Vedi il Dubbio 1.
- **Non confrontato con lo standalone** (solo nei test Go): l'esito della conferma disegnata dal
  server e del CSRF; «🔄 Cambia fotocamera» con più fotocamere (la fotocamera finta è una sola);
  l'interruttore 🔊/🔇; il contesto non sicuro (il banco è in HTTPS); la modifica della busta (stesso
  codice di «Nuova riparazione»); Esc; l'errore di rete.

## Prove

- **Test Go:**
  - `internal/web/lettore_qr_test.go` (nuovo): `TestJsQRNelBinario` (asset, impronta, la riga della
    lettura a specchio di jsQR, `root["jsQR"]`, licenza con origine, versione e impronta, nessun
    indirizzo esterno in `app.js`), `TestScriptDelLettoreQR` (le righe di `qr_scanner_asset_test.exs`
    in italiano, l'ordine del contesto sicuro, i due 📷), `TestScriptDelSuonoQR` (toni, chiave e
    `try`, sblocco solo nei tocchi, l'esito del server), `TestScriptDelPannelloSopraLaLista` (fetch con
    mittente e `formaction`, il riquadro senza il secondo campo unico, nessuna cronologia col codice,
    l'ordine degli ascoltatori).
  - Ritoccati: `TestCercaQRScansione` (l'interruttore, `data-qr-esito` ok per una busta e per le
    gemelle, assente per la scelta valida, ko per quella non valida), `TestCercaQRMessaggi` (ko per
    sconosciuto, vuoto e ricerca non riuscita, nessun riquadro nella lista della pistola),
    `TestPannelloQRConferma` (la conferma dal server senza esito), `TestPannelloQRAvanza` (ko per
    cambiata, chiusa, assente, cestino), `TestPannelloQRPreventivo` (ko per i rifiuti),
    `TestHomeRicercaECassetto` (il 📷 con `data-target` e `data-submit`).
  - `tools/banco/fotocamera_test.go` (nuovo): `TestFotocamereFinte` (diritto letto diritto, trasposto
    letto solo trasposto, l'etichetta di Chromium trasposta con `SINT-QR-0005`, decodificati da
    `lettura.CodiciQR`; i file Y4M riletti modulo per modulo, intestazione, fotogrammi, permessi).
- `scripts/check` verde: gofmt, vet, staticcheck, errcheck, build, test, copertura (353 voci: 26
  chiuse, 0 rosse, 83 parziali, 233 aperte, 11 fuori v1; RIP-H-27 e RIP-H-28 ora parziali), segreti e
  dati veri OK. RIP-H-27 e RIP-H-28 sono «parziali» perché hanno lo scenario `lettore-qr`, e
  `scripts/check` gira senza l'esito del banco (`GESTRIP_BANCO_ESITO`).

## Mutazioni

`to-be-deleted-after-use/slice-055/mutazioni/muta.py`, 41 punti, test mirati di `internal/web` e
`tools/banco` in una copia del repo nello scratchpad. Primo giro: 38 uccise, 2 che non compilavano
(riscritte), 1 sopravvissuta (l'audio sbloccato anche al caricamento della pagina: aggiunto il conto
di `sblocca()` a `TestScriptDelSuonoQR`). Secondo giro sulle 3: 2 uccise, 1 equivalente. **Totale: 41
punti, 40 uccise, 1 equivalente.**

Uccise: esito ok e ko scambiati; la scelta valida che suona; le gemelle senza esito; l'azione
rifiutata senza esito; la conferma del server che suona; l'esito non scritto; l'interruttore visibile
senza JavaScript; l'indirizzo di jsQR non scritto in `app.js`; jsQR da un CDN; `jsqr.min.js` cambiato;
il contesto sicuro dopo la fotocamera; il messaggio del contesto cambiato; la fotocamera salvata senza
`try`; la chiave della fotocamera; la fotocamera anteriore; la principale non per indice; jsQR con
l'inversione; il modulo senza il 📷 come mittente (via=qr perso); «Nuova riparazione» senza bip;
l'audio non sbloccato dal tocco; l'audio sbloccato al caricamento; lo storage rotto che spegne il
suono; il bip di successo grave; l'esito del server ignorato; nessun bip dopo un'azione riuscita; il
codice nell'indirizzo; il fetch senza il valore del pulsante; `formaction` ignorato; il riquadro con il
secondo campo unico; il doppio invio non fermato; l'errore di rete senza ripartenza; il redirect senza
cronologia; il 📷 della home senza modulo; il 📷 di «Nuova riparazione» che manda il modulo; la
fotocamera trasposta uguale alla diritta; un rettangolo dell'etichetta perso; un solo fotogramma Y4M;
le fotocamere non montate o montate scrivibili; lo scenario senza RIP-H-28.

Equivalente: il controllo che l'etichetta dello standalone sia trasposta (`scriviFotocamere`) tolto:
con la fixture vera, che è trasposta, non cambia niente; il test controlla la fixture per conto suo.

Il comportamento nel browser (finestra, lettura, riquadro dentro la lista, suono) lo provano solo gli
scenari `lettore-qr` e `pannello-qr` del banco, non le mutazioni.

## Banco

`gestrip-standalone:fab-224`, istanze `s055a`…`s055e`.

- **Giri mirati** (dati importati; HTTP `healthz`, `cerca-qr`, `qr-pannello`; browser `lettore-qr` e
  `pannello-qr`):
  - `s055a`: HTTP 1 inattesa (il testo visibile di `#qr-lookup` in `cerca-qr[3]` prendeva il «🔊»
    dell'interruttore nascosto: ora il pulsante è vuoto senza JavaScript); browser `lettore-qr` tutto
    «⚠ Not supported» nelle due app (il Chromium senza finestra vuole anche
    `--use-fake-ui-for-media-stream`), `pannello-qr` **verde** con il pannello dentro la lista;
  - `s055b`: HTTP 76 passi, **0 inattese**; browser 2 scenari, **0 inattese**: qui si è visto che lo
    standalone legge anche il QR trasposto (le attese `solo_go` D-52 erano sbagliate: tolte, e tolta
    la trasposizione di `app.js`);
  - `s055c` (con le gemelle): HTTP 76 passi, 0 inattese; browser 2 scenari, 4 viste, **0 inattese**.
- **Giro completo, dati importati** (`s055d`): HTTP **47 scenari, 860 passi, 1105 differenze attese,
  0 inattese**, 0 errori; browser **10 scenari, 20 viste, 26 attese** (D-58, D-59, D-64 di sempre),
  **0 inattese**, 0 errori: **verde**. Niente 500 dello standalone in `firma-riquadro` in questo giro.
- **Giro completo, database vuoto** (`s055e`): HTTP 47 scenari, 860 passi, 33 attese, **0 inattese**
  (29 non provati con questi dati); browser 10 scenari, **0 inattese** (9 non provati con questi dati; `lettore-qr`
  si prova anche sul vuoto): **verde**.
- I giri completi avevano il binario prima di un ritocco piccolo di `app.js` (il messaggio d'errore
  di jsQR che resta visibile quando il video parte). Col binario finale: giri mirati `s055f` (dati
  importati) e `s055g` (vuoto), HTTP `healthz`, `cerca-qr`, `qr-pannello` e browser `lettore-qr`,
  `pannello-qr`: tutti e due **verdi** (HTTP 76 passi, 0 inattese; browser 2 scenari, 4 viste,
  0 inattese).
- `banco giu` dopo ogni giro: «AVANZI: nessuno».

## Per il manuale

Scritto nel capitolo nuovo `docs/manuale/06-ricerca-e-qr.md` (tolta la sezione «Ricerca e QR» da
`da-scrivere.md`), con quello della 052-054:

- 📷 nel campo di ricerca apre la fotocamera posteriore con la linea di mira; il codice letto apre il
  riquadro della busta **sopra la lista**, che resta con i filtri; «Chiudi» o Esc lo tolgono.
- «🔄 Cambia fotocamera» passa alla fotocamera successiva e il telefono la ricorda; «Chiudi» chiude la
  finestra.
- Il permesso della fotocamera: la prima volta «Consenti»; se negato si rimette dalle impostazioni del
  sito.
- Serve `https://` con il certificato del negozio: altrimenti «⚠ Fotocamera non disponibile: contesto
  non sicuro…», che non è un guasto.
- Il lettore legge anche le etichette vecchie stampate a specchio.
- Il suono: un bip acuto per busta trovata o azione fatta, due gravi per codice sconosciuto o azione
  rifiutata, la vibrazione su Android; parte solo dopo un tocco su 📷; l'interruttore 🔊/🔇 vale per
  quel telefono; su iPhone col silenzioso può non sentirsi.
- In «Nuova riparazione» e nella modifica «📷 Scansiona» riempie solo il campo «Etichetta» e suona.
- Le gemelle si scelgono nello stesso riquadro.

Mancano le schermate del capitolo (vogliono una ricetta di `banco manuale`): vedi il Dubbio 5.

## Dubbi

1. **La pistola e il suono (D-63).** Con il campo unico un codice letto dalla pistola che non è di
   nessuna busta è una ricerca scritta: lista vuota e nessun suono. Nello standalone la pistola nel
   campo «Cerca per QR busta» suonava i due bip gravi. Se Stefano vuole i due bip anche qui, la regola
   potrebbe essere: un testo con la forma esatta di un'etichetta `RIP-AAAA-NNNN` che non trova niente
   suona il fallimento (decisione di prodotto, non presa).
2. **La linea di mira.** L'inventario la chiede, ma la finestra di `qr_scanner.js` dello standalone
   non la disegna (solo video, stato, due pulsanti). L'ho messa (una riga rossa a metà del video, che
   non cambia la lettura): da confermare o togliere.
3. **Il ripiego della 053 per la pistola sull'etichetta.** Fino a oggi il 📷 di «Etichetta» metteva
   il cursore nel campo, per la pistola; ora apre la fotocamera, come lo standalone. Con la pistola si
   tocca il campo e si spara. Se in negozio l'etichetta si legge di solito con la pistola, si potrebbe
   rimettere il cursore nel campo quando la finestra si chiude senza lettura.
4. **La lista di una ricerca scritta senza ricaricare.** Con JavaScript una ricerca scritta che dà più
   risultati (o nessuno) mostra la lista al posto della pagina senza cambiare indirizzo: ricaricando si
   torna alla lista di prima, senza la ricerca. Senza JavaScript il browser chiedeva di rimandare il
   modulo. Il codice non va nell'indirizzo (regola della 052), quindi non vedo un modo migliore.
5. **Proposta di una 055c** (quello che non ho fatto, per non farlo a metà): le schermate del capitolo
   06 (una ricetta di `banco manuale` con il riquadro sopra la lista; la finestra della fotocamera non
   si fotografa bene con la fotocamera finta); un `banco` che provi anche «🔄 Cambia fotocamera» con due
   fotocamere finte (Chromium ne dà una sola per file); il riquadro anche sulla pagina della scansione
   senza lista (oggi lì, con JavaScript, le azioni sono ancora POST normali, e dopo un'azione non c'è il
   suono perché la pagina nuova non ha l'audio sbloccato: ci si arriva solo senza JavaScript o
   tornando indietro).
6. **L'inventario privato** non l'ho toccato (sola lettura): le righe «Portato (slice 055)» di RIP-H-27
   e RIP-H-28 e la nota sulla D-52 (letta anche dallo standalone) sono da scrivere dal coordinatore.

## Review

Review fatta da un sottoagente diverso: nessun problema ALTO o MEDIO. `scripts/check` verde; banco completo `rev055` su fab-224 con dati importati: HTTP 860 passi, 0 differenze inattese; browser 10 scenari, 0 inattese.

Ha controllato e trovato a posto:
- il codice letto non finisce mai nell'indirizzo;
- il token CSRF del `fetch`, `no-store` sulle risposte e nessuna XSS (`DOMParser` e `adoptNode`, niente `innerHTML`);
- jsQR: sha256 e licenza uguali al pacchetto npm 1.4.0;
- localStorage sempre dentro `try`;
- la fotocamera viene fermata in ogni caso;
- doppio invio e conferme della 054;
- il funzionamento senza JavaScript;
- i video finti, che contengono davvero un QR a specchio.

Corretti due problemi BASSI:
- `cameraPosteriore`: se si aprivano male sia la fotocamera principale sia quella di `facingMode`, compariva un video nero invece del messaggio d'errore;
- `banco.mjs`: il nome di uno scenario si controlla con `Object.hasOwn` invece che con `in`.

Tre problemi BASSI segnalati e non corretti:
- un solo Esc chiude sia il menu aperto sia il riquadro QR;
- un'eccezione dentro la gestione della risposta mostra «Connessione assente»;
- una seconda lettura arrivata mentre la prima ricerca non ha ancora risposto viene ignorata senza avviso.
