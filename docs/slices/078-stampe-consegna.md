# 078 — Stampe della consegna all'artigiano: etichette, Anteprima, Ristampa, Condividi, Stampa e conferma (T4.8, terza)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/controllers/handover_label_controller.ex`
  (`show/2`, `load/1`, `deliver/4`: 404 «Consegna vuota o non trovata», 503 «PDF non disponibile», audit prima dei byte,
  `?download=`), `lib/gestrip_standalone/handover_labels.ex` (`render_html/3`, `artisan_name/1`, `filename/1`),
  `workshop.ex` (`list_handover_label_bags/1`, `record_handover_labels/3`), `handover_session_live.ex` (`render/1`: i
  link «👁 Anteprima», «🖨 Ristampa», «📤 Condividi», «🖨 Stampa e conferma» e la frase sotto).
- **Voci:** ART-12 (sostituita: PDF in puro Go, e la D-09 per la stampa diretta). Portata; si chiude con l'esito verde
  del banco. Nessuna migrazione.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco (Docker) non è stato lanciato:
  lo fa la sessione principale (sotto, «Banco»).

## Cosa c'è

- **PDF** (`internal/stampe/etichetta.go`, `EtichetteConsegna`): una pagina 76×127 per busta, nell'ordine dato, ognuna
  disegnata da `PaginaEtichetta` (la stessa dell'etichetta della busta, con «All'artigiano:» della D-73 e il numero
  visibile della D-64). Senza buste errore.
- **Dominio** (`internal/buste/consegne_stampe.go`; `completaEtichetta` tolto da `leggiEtichetta` in `documenti.go`,
  `confermaNellaTransazione` tolto da `ConfermaConsegna` in `consegne.go`, stesso comportamento):
  - le etichette della consegna sono `list_handover_label_bags/1`: le buste della consegna lette adesso, fuori dal
    cestino, per numero (l'ordine del testo, come `bag_number` dello standalone: «10/26» prima di «2/26»; poi l'id), una
    rientrata compresa; «→» è l'artigiano **della consegna** su tutte le pagine (non quello della busta);
  - `EtichetteDellaConsegna`: consegna che non c'è o senza buste → `ErrConsegnaVuota`; PDF fatto → solo allora
    `handover.labels_generated` `{"bags": N}` (attore, IP e user agent come il controller), poi i byte; PDF non fatto o
    riga non scritta → `ErrPDFNonFatto`. Nessuna modifica a consegna e buste. Una consegna chiusa dà lo stesso il PDF;
  - `EtichetteDaStampare`: lo stesso PDF senza riga, con gli id delle buste stampate (nell'ordine delle pagine);
  - `StampaConsegnaAccettata` (D-09): in una transazione `handover.labels_printed` `{"bags", "repair_ids", "queue",
    "job"}` (le buste **stampate**, quelle del PDF: se mentre `lp` lavora una busta si aggiunge o si toglie, la riga
    dice cosa è uscito dalla stampante, e la conferma timbra le buste che la consegna ha in quel momento, come lo
    standalone, che conferma le buste lette alla conferma) e la conferma di «✓ Conferma senza stampare» (uscite e
    `handover.confirmed` con `handed_over` e `repair_ids`); consegna chiusa o che non c'è →
    `ErrConsegnaNonDisponibile`; **nessuna busta ancora da timbrare** (confermata da un altro mentre `lp` lavorava) →
    `ErrNienteDaConfermare` (solo Go, review): in tutti e due i casi non si scrive niente, quindi mai una seconda
    `handover.confirmed` da 0; una riga che non si scrive → niente esce;
  - `StampaConsegnaNonConfermata`: la traccia `handover.labels_printed` con esito `not_confirmed` (come
    `counter_not_saved` della 046) quando il lavoro è partito ma la conferma no.
- **Pagine** (`internal/web/consegne_stampe.go`, `pagine/consegne.html`, CSS in `app.css`, `app.js`):
  - `GET /handovers/{id}/labels.pdf`, del personale (nessun controllo di ruolo, come lo standalone): in linea
    `etichette_consegna_<id>.pdf`, allegato con `download` non vuoto (anche `0`; `comeAllegato` dell'etichetta della
    busta), `private, no-store`; id non valido, consegna che non c'è o vuota → 404 «Consegna vuota o non trovata» senza
    tipo (`send_resp`), con `max-age=0, private, must-revalidate`; 503 «PDF non disponibile» come gli altri PDF.
  - Sulla pagina della sessione, dopo «Buste in consegna»: prima della conferma «👁 Anteprima» (scheda nuova) e «📤
    Condividi» (`?download=1` con `data-share-pdf`: lo `condividiPDF` di `app.js` della 046); dopo la conferma «🖨
    Ristampa» e «📤 Condividi». Poi, prima della conferma, «✓ Conferma senza stampare» e «🖨 Stampa e conferma» (con
    «Stampare N etichetta/e e confermare la consegna?»; senza buste un pulsante semplice, senza domanda) e la frase dello
    standalone «"Conferma senza stampare" se dai le buste originali col tuo logo. "Stampa e conferma" se usi buste
    anonime e attacchi le etichette QR.».
  - Le due conferme stanno in un **gruppo d'invio** (`<div class="consegna-conferme" data-invio-gruppo>`, `app.js`):
    il primo invio di uno dei due moduli spegne anche l'altro (review); la pagina tenuta dal browser li riaccende.
  - `POST /handovers/{id}/stampa-conferma`, del personale, con `data-invio-unico`. Il modulo porta il **modo** con cui
    la pagina è stata disegnata (`modo=stampante|pdf`); se non è quello di adesso (stampante scelta o tolta dopo, o un
    modulo senza modo) né lavoro né conferma: la sessione con «La stampante delle etichette è cambiata: controlla e
    riprova.» (solo Go, review).
    - **senza la stampante dell'etichetta busta** (Impostazioni → Stampanti, o GestRip partito senza i comandi delle
      stampanti): come lo standalone, il pulsante è il **link** al PDF con `target="_blank"` e la domanda, segnato
      `data-invia-modulo`: il browser apre il PDF in una scheda nuova e `app.js` invia anche il modulo
      (`requestSubmit`); il server conferma come «✓ Conferma senza stampare» (stesso codice, `confermaLaConsegna`:
      «Aggiungi almeno una busta.», «Consegna confermata: N buste fuori bottega.» verso `/?fuori_bottega=1`, i rifiuti
      del dominio). Senza buste un pulsante, niente scheda nuova;
    - **con la stampante** (D-09): il modulo porta l'invio monouso (doppio invio: il secondo mostra l'esito del primo) e
      un blocco per consegna tiene fuori un secondo lavoro e anche «✓ Conferma senza stampare» finché il lavoro non ha
      risposto («Stampa già in corso: aspetta l'esito prima di riprovare.»); **prima** del lavoro: senza buste «Aggiungi
      almeno una busta.», consegna chiusa «Consegna non disponibile», **nessuna busta ancora da timbrare** «Nessuna busta
      da confermare: le buste di questa consegna sono già fuori bottega. Per rifare le etichette usa «🖨 Ristampa».»
      (solo Go, review); poi il PDF, un lavoro `lp` sulla coda dell'etichetta busta con le
      sue opzioni, titolo `etichette_consegna_<id>.pdf` (`context.WithoutCancel`); accettato → `StampaConsegnaAccettata`
      e `/?fuori_bottega=1` con «Consegna confermata: N buste fuori bottega. Etichette inviate a <coda> (lavoro <id>).»;
      rifiutato o senza risposta → la sessione con «Etichette non stampate: <motivo> Consegna non confermata.» (i motivi
      di `motivoStampa`, con «stampante», mai «CUPS»; il tempo scaduto è ricordato per l'invio, così lo stesso modulo
      non rimanda il lavoro); PDF non fatto → «Etichette non stampate: PDF non disponibile. Consegna non confermata.»;
      accettato ma non confermato → «Etichette inviate a <coda> (lavoro <id>), ma la consegna non è stata confermata:
      usa «✓ Conferma senza stampare».» (o «…: Consegna non disponibile.» se nel frattempo è stata chiusa; «Etichette
      inviate a <coda> (lavoro <id>), ma la consegna era già confermata: nessuna busta da timbrare.» se un altro l'ha
      confermata mentre `lp` lavorava) e la traccia `not_confirmed`.
  - Tabella dei permessi (`ruoli_test.go`): le due rotte, del personale. `busteFinte` con i quattro metodi nuovi.
- **Banco:** `bench/scenari/consegne-etichette.json`, `bench/scenari/stampa-etichette-consegna.json` (sul secondo Go,
  `istanza: go-telegram`); i test del banco li conoscono (`TestProvaConDegliScenariDelRepo`, il controllo di
  `atteso_dati` in `dati_test.go`).
- **SVILUPPO:** un punto nella sezione «Stampa diretta CUPS».

## Scelte

1. **La coda del 76×127 della consegna è quella dell'etichetta busta**, con le sue opzioni: è lo stesso foglio 76×127
   (l'etichetta della busta ripetuta per ogni busta), nessuna voce nuova in Impostazioni → Stampanti. Confermato
   leggendo la 046 (`stampaEtichettaBusta`, `ChiaveCodaBusta`) e la D-09 dell'inventario.
2. **«Stampa e conferma» senza stampante è il link al PDF, come lo standalone.** Lo standalone è un link con
   `target="_blank"` e `phx-click="confirm"`; qui lo stesso link (la scheda nuova la apre il browser con il clic, che
   il blocco dei popup non ferma) con `data-invia-modulo`: `app.js` invia anche il modulo POST (CSRF, invio unico).
   Dopo la review: prima era un modulo e `app.js` apriva il PDF con `window.open` dopo la domanda, che un blocco dei
   popup poteva fermare. Una domanda annullata ferma il clic (né scheda né invio); con il gruppo già inviato il clic
   non fa niente. Senza JavaScript il link apre solo il PDF e la conferma resta da fare (lo standalone senza
   JavaScript non fa niente).
3. **Con la stampante, i rifiuti che si sanno prima vengono prima del lavoro** (consegna vuota, consegna chiusa,
   nessuna busta da timbrare): un lavoro non parte per una conferma che il dominio rifiuterebbe. Le corse fra il
   controllo e la conferma (una chiusura, un'altra conferma) finiscono nei messaggi «… ma la consegna non è stata
   confermata» / «… era già confermata» con la traccia, senza una seconda conferma.
4. **Due righe per la stampa riuscita**: `handover.labels_printed` (solo Go, come `repair.label_printed` e
   `label_sheet.printed` della 046, con coda e lavoro) e `handover.confirmed` (come lo standalone), nella stessa
   transazione. La stampa diretta non scrive `handover.labels_generated` (che resta del PDF scaricato), come
   `repair.label_printed` al posto di `repair.label_generated`.
5. **Il modo della pagina nel modulo** (review): la stampante può essere scelta o tolta fra il disegno della pagina e
   il clic; il server non fa né l'una né l'altra cosa al posto di quella che l'operatore ha visto.
6. **Audit**: il PDF ha il contesto del controller (attore, IP, user agent: `contestoFoto`), la conferma quello della
   LiveView (solo attore: `contestoDi`), anche con la stampante.

## Per il manuale

- Nella pagina di una consegna ci sono «👁 Anteprima» (apre le etichette in una scheda nuova) e «📤 Condividi» (dal
  telefono le manda a WhatsApp, email o alla stampa; dal PC le scarica). Dopo la conferma «👁 Anteprima» diventa «🖨
  Ristampa».
- Le etichette sono una per busta, nell'ordine del numero, con il nome dell'artigiano della consegna in alto e la riga
  «All'artigiano:» con la data di stampa.
- «🖨 Stampa e conferma» senza stampante scelta apre le etichette in una scheda nuova e conferma la consegna.
- Se il titolare ha scelto la stampante dell'etichetta busta (Impostazioni → Stampanti), «🖨 Stampa e conferma» manda
  le etichette direttamente a quella stampante e conferma la consegna **solo se la stampante accetta il lavoro**; se la
  stampante rifiuta o non risponde la consegna resta da confermare e la pagina dice perché.
- Con una consegna senza buste «🖨 Stampa e conferma» dice «Aggiungi almeno una busta.»; con una consegna già
  confermata, con la stampante, dice che non c'è niente da confermare e rimanda a «🖨 Ristampa».
- Premuto uno dei due pulsanti di conferma, l'altro si spegne finché la pagina non risponde; se il titolare cambia la
  stampante mentre la pagina è aperta, «🖨 Stampa e conferma» chiede di controllare e riprovare.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/078-stampe-consegna/prove/`): `rosso-vet.log` (stampe e dominio
  che non ci sono), `rosso-web.log` (le rotte e i pulsanti che non ci sono: 6 test rossi sull'asserzione, con il dominio
  già fatto).
- **PDF** (`internal/stampe/etichette_consegna_test.go`): due pagine 76×127 con le righe e il QR di ognuna, «→ ALFA
  ORAFI» su tutte; una busta = lo stesso testo dell'etichetta della busta; senza buste errore.
- **Dominio** (`internal/buste/consegne_stampe_test.go`): vuota e che non c'è, ordine per numero diverso da quello
  degli id, cestino, rientrata compresa, artigiano della consegna contro quello della busta, dati dell'etichetta (QR,
  numero visibile, scadenza, stato, descrizione, oggi), riga dopo il PDF con l'attore, niente cambia, PDF non fatto,
  riga che non si scrive, consegna chiusa, tutte nel cestino; stampa: PDF senza riga, riga della conferma che non si
  scrive (niente esce), accettata (uscite, due righe, consegna aperta), chiusa e che non c'è, traccia `not_confirmed`.
- **Pagine** (`internal/web/consegne_stampe_test.go`): il PDF (404 per vuota, che non c'è, `abc`, `0`, oltre i 64 bit;
  200 con due pagine, testo, cestino escluso, artigiano della consegna, `download` 1/0/vuoto/ripetuto, righe, nessuna
  modifica, titolare, 503); i pulsanti (vuota, piena, con la stampante, confermata, ordine); senza stampante (vuota,
  conferma, nessun lavoro, consegna che non c'è, 403 senza token); con la stampante (vuota, rifiuto senza conferma né
  righe, accettato con coda, opzioni, titolo, PDF di due pagine, uscite e righe, doppio invio, consegna chiusa e che
  non c'è senza lavoro); accettato e non confermato (messaggio, traccia, lo stesso invio non ristampa); lo script.
  `TestCondividiPDFSullePagine` (RIP-S-44) ora non si salta più: la pagina della consegna è la prima con
  `data-share-pdf`.
- **Dopo la review** (rosso in `prove/rosso-review-vet.log`, firme nuove, e `prove/rosso-review-web.log`, 9 test web
  rossi sull'asserzione con il dominio già corretto): nel dominio la consegna già confermata
  (`ErrNienteDaConfermare`, nessuna riga) e gli id stampati in `handover.labels_printed`; nelle pagine il modo cambiato
  nei due versi e senza modo (`TestStampaEConfermaModoCambiato`), la consegna già confermata prima del lavoro, la
  consegna chiusa o confermata da un altro **durante** il lavoro (`f.Prima` della stampante finta;
  `TestStampaEConfermaConsegnaCambiataDuranteIlLavoro`: messaggi e `not_confirmed`, nessuna seconda conferma), il tempo
  scaduto (esito ricordato: lo stesso modulo non chiede un secondo lavoro) e il PDF non fatto con la stampante
  (`TestStampaEConfermaTempoScadutoEPDF`, buste avvolte da `pdfRotto`), il blocco per consegna contro un altro modulo e
  contro «✓ Conferma senza stampare» (`TestStampaEConfermaBloccoPerConsegna`), il gruppo d'invio e il link nello script
  (`TestScriptStampaEConfermaDellaConsegna`; `TestScriptSalvaEAvverti` aggiornato alla riga del gruppo).

## Mutazioni

25 mutazioni a mano sul codice nuovo (dominio, PDF, pagina, modello, script), una alla volta su una copia del repo
(`to-be-deleted-after-use/078-stampe-consegna/mutazioni/`: `muta.py`, `giro.log`, `muta2.py`, `giro2.log`,
`esiti/<id>.log`; copia cancellata a fine giro). Primo giro: 22 uccise, 3 non compilavano (M06, M09, M17: la
sostituzione lasciava una variabile inutilizzata); riscritte in una forma che compila (M06b, M09b, M17b) e **uccise**:
**25 su 25**, tutte da un'asserzione. Fra queste: ordine per id invece che per numero, cestino incluso, artigiano della
busta, riga prima del PDF, una pagina sola, 404 con tipo, sempre in linea, stampante ignorata, conferma dopo un
rifiuto, lavoro per una consegna chiusa, esito non ricordato (doppio invio), traccia mancante, titolo e opzioni del
lavoro, `data-apri-pdf` senza buste o con la stampante, Condividi senza download, script senza `defaultPrevented`.
(Le mutazioni su `data-apri-pdf` e `window.open` del primo giro valgono per il codice di prima della review, che non
c'è più.)

**Dopo la review**, 16 mutazioni nuove sul codice corretto (`muta3.py`, `giro3.log`, `muta4.py`, `giro4.log`,
`esiti-review/<id>.log`; copia cancellata): 15 uccise al primo giro, una (R10) non compilava ed è stata riscritta (R10b)
e uccisa: **16 su 16**. Coprono: il controllo «nessuna busta da timbrare» nel dominio (R01) e prima del lavoro (R03),
gli id stampati nella riga (R02) e nella traccia (R16), il modo non controllato (R04) o sempre `pdf` nella pagina
(R11), il blocco della stampa su «✓ Conferma senza stampare» (R05) e per consegna (R06), il tempo scaduto non ricordato
(R07), il PDF non fatto preso per consegna vuota (R08), i messaggi «già confermata» (R09) e del rifiuto del dominio
(R10b), il gruppo d'invio tolto dal modello (R12), il link senza `data-invia-modulo` (R13), lo script senza il gruppo
(R14) o senza `requestSubmit` (R15).

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Nessuna D nuova: D-64, D-73 e D-80 per il PDF, D-09 per la
stampa.

- **HTTP**, `consegne-etichette` (ART-12, `prova_con` i dati della cattura), 10 passi, tutti confrontati con lo
  standalone: `GET /handovers/1/labels.pdf` senza sessione (302 a `/login`), il login che torna al PDF, poi il PDF
  della consegna 1 con `normalizza: D-64+D-73+D-80` (`pdf:formato` 76x127, `pdf:testo`, `pdf:qr` «4/26»,
  `pdf:posizioni`, le intestazioni, `inline; filename="etichette_consegna_1.pdf"`), con `?download=1` e `?download=0`
  (allegato) e `?download=` (in linea); `/handovers/999`, `/handovers/abc` e `/handovers/0` (404 «Consegna vuota o non
  trovata», senza tipo). Sul database vuoto tutti i PDF sono il 404. Il nome lo fa girare **subito prima di
  `consegne`** (nell'ordine dei nomi «-» viene prima di «.»: `consegne-etichette.json` < `consegne.json`): fra i due
  nessun altro scenario, e questo non cambia niente oltre al registro (`handover.labels_generated` su tutte e due le
  app), quindi `consegne` trova la consegna 1 com'era (confermata, la 4/26).
- **HTTP**, `stampa-etichette-consegna` (ART-12, D-09), 18 passi tutti `solo_go`, **sul secondo Go
  (`"istanza": "go-telegram"`, `prova_con` i dati)**: è la copia dei dati del Go fatta subito dopo l'import, con i
  comandi di stampa finti; il Go del confronto e lo standalone non vedono niente, quindi nessuno scenario dopo (HTTP o
  browser) trova una consegna o una busta diversa. Sul secondo Go gira dopo solo `telegram-acceso` (busta 23, Bot API
  finta): nessuna delle due tocca le cose dell'altra. Il percorso vero:
  1. login, la coda dell'etichetta busta a «Rifiutata»;
  2. `/handovers` (nessuna sessione di oggi), «Apri sessione» per l'artigiano 1 (Laboratorio Sintetico) → una consegna
     nuova (la consegna 1 è del 06/01 e ha la 4/26 uscita), aggiunta della **1/26** scritta «1/26» (numero interno senza
     etichetta, nessuna RIP-2026-0001 nei dati; sul secondo Go nessuno scenario la tocca);
  3. «🖨 Stampa e conferma» (`modo=stampante`) → la sessione con «Etichette non stampate: la stampante ha rifiutato il
     lavoro (lp: The printer or class does not exist.). Consegna non confermata.», la busta «N. 1/26» ancora in lista
     (conto 1), niente «· Confermata», il modo `stampante` nel modulo;
  4. «🖨 Stampa e conferma» sulla consegna 1 (già confermata) → «Nessuna busta da confermare: le buste di questa
     consegna sono già fuori bottega. Per rifare le etichette usa «🖨 Ristampa».», nessun lavoro;
  5. la coda a «Buste-76x127»; `/handovers` con la sessione di oggi («— 1 busta»); «Apri sessione» di nuovo (ripresa
     della stessa consegna in raccolta) e «🖨 Stampa e conferma» → `/?fuori_bottega=1` con «Consegna confermata: 1 busta
     fuori bottega. Etichette inviate a Buste-76x127 (lavoro Buste-76x127-…»; poi `/handovers` senza sessioni di oggi
     (la busta è uscita); la coda tolta.
  Il percorso della consegna nuova si prende dai `{location}` dei POST (l'id non è fisso).
- Ci si aspetta: **HTTP 71 scenari, 1203 passi** (69 e 1175 della 077, più 2 e 10 + 18), 0 inattese, 0 errori.
  **Browser 19 scenari, 38 viste** come la 077 (nessuno scenario nuovo). La pagina della sessione ha i pulsanti nuovi
  e le due conferme in un gruppo d'invio: in `sessioni-consegna` «✓ Conferma senza stampare» a sessione vuota torna
  con il rifiuto (pagina nuova, gruppo riacceso) e la conferma vera va a `/?fuori_bottega=1`: nessuna attesa cambia.
- Se qualcosa è rosso, da guardare per primo:
  - `intestazione:content-type` del 404: lo standalone fa `send_resp/3` senza tipo (come il 503 dei PDF); se Phoenix lo
    mette, l'attesa `""` è da correggere.
  - `pdf:posizioni` e `pdf:testo`: la pagina è quella di `/repairs/4/label.pdf` (scenario `etichetta`, stessi
    normalizzatori) con «→ LABORATORIO SINTETICO», l'artigiano della consegna.
  - Il login che torna a `/handovers/1/labels.pdf` (come `etichetta` con `/repairs/1/label.pdf`).
  - `stampa-etichette-consegna`: se la 1/26 nella copia del secondo Go non si può dare all'artigiano (stato, compro
    oro, un'altra consegna) l'aggiunta rifiuta e il passo 8 non trova «N. 1/26»; allora scegliere un'altra busta della
    cattura senza etichetta. L'artigiano 1 è Laboratorio Sintetico (consegna 1, scenario `consegne`).

## Banco s078p (sessione principale) e correzione

Browser verde (19/38). HTTP 71 scenari, 1203 passi, **6 inattese**, tutte in `consegne-etichette[7..9]`
(`/handovers/999`, `abc`, `0` di `labels.pdf`): `intestazione:content-type` standalone `""` (send_resp senza tipo), Go
`text/plain; charset=utf-8`. Tutto il resto verde, anche `stampa-etichette-consegna` sul secondo Go con la 1/26.

- **Causa:** il Go metteva già `Content-Type` a nil (niente sniffing di net/http), ma mandava intestazioni e corpo
  insieme, con `Content-Length`: il Caddy del banco, ricevendo tutto insieme, sceglie da sé `text/plain` (lo stesso caso
  del 400 di `/labels.pdf` nella 046, `rifiutoEtichette`). Lo standalone manda le intestazioni prima del corpo.
- **Correzione:** `rispostaSenzaTipo` (`internal/web/ricevuta.go`): Cache-Control di base, `Content-Type` nil,
  `WriteHeader`, **Flush**, poi il testo. La usano il 404 «Consegna vuota o non trovata» e il 503 «PDF non disponibile»
  di `inviaPDF` (lo stesso `send_resp/3` nello standalone, per tutti i PDF: ricevuta, etichetta, scheda, etichette della
  consegna), che aveva lo stesso difetto ma il banco non lo vede (un 503 non si provoca dal banco).
- **Rosso prima:** `TestPDFEtichetteConsegna` vuole anche nessun `Content-Length` sul 404 e sul 503
  (`prove/rosso-s078p.log`: 5 404 e il 503 con `Content-Length`); poi verde.
- **Mutazione a mano** (`rispostaSenzaTipo` senza il Flush, su una copia del repo poi cancellata): compila ed è
  **presa** da `TestPDFEtichetteConsegna`, con i 6 `Content-Length` (log `prove/muta-s078p.log`).
- `scripts/check` verde dopo la correzione (`prove/check3.log`, «uscita 0», stessa copertura).
- Ci si aspetta al prossimo giro: HTTP 71/1203, 0 inattese; browser 19/38.

## Controlli

`scripts/check` verde (`to-be-deleted-after-use/078-stampe-consegna/prove/check1.log`, «uscita 0»): gofmt, vet,
staticcheck, errcheck, build senza CGO, test, copertura **voci 353: chiuse 39, rosse 0, parziali 145, aperte 158, fuori
v1 11** (077: 38/145/159), segreti e dati veri OK. Dopo il giro di `check1` un solo cambio a un test di dominio
(l'ordine di creazione delle buste, perché la mutazione M01 si vedesse), rifatto a parte: verde. `tools/banco` e i suoi
test verdi (compilati, non lanciati).

Dopo le correzioni della review: `scripts/check` di nuovo verde (`prove/check2.log`, «uscita 0»), stessa copertura
(chiuse 39, rosse 0, parziali 145, aperte 158, fuori v1 11); i test di `tools/banco` verdi con lo scenario riscritto.

## Dubbi

1. **Senza JavaScript e senza stampante** «🖨 Stampa e conferma» è il link: apre il PDF e non conferma (la conferma
   resta da fare con «✓ Conferma senza stampare»); lo standalone senza JavaScript non fa niente (LiveView).
   **Raccomandazione:** tenere così.
   **Deciso da Stefano il 15/11/2026:** tenere così: senza JavaScript il link apre solo il PDF. Non è una deviazione.
2. **Il PDF in una scheda nuova e il blocco dei popup.** Dopo la review non c'è più `window.open`: la scheda la apre il
   link con `target="_blank"`, come lo standalone, con il clic dell'operatore (dopo la domanda di `data-confirm`, che
   non toglie il gesto). Se un browser la bloccasse lo stesso, la consegna si conferma e il PDF resta in «🖨 Ristampa».
   **Raccomandazione:** tenere così; da guardare sul telefono del negozio alla prova dal vivo.
   **Deciso da Stefano il 15/11/2026:** tenere così (da guardare sul telefono del negozio alla prova dal vivo). Non è una deviazione.
3. **Id oltre i 64 bit** (`/handovers/99999999999999999999/labels.pdf`): il Go dà il 404 «Consegna vuota o non
   trovata»; lo standalone non misurato (per le altre rotte dei PDF dava 500, D-56). Non messo nel banco.
   **Raccomandazione:** se il banco lo misura diverso, estendere la D-56 a questa rotta.
   **Deciso da Stefano il 15/11/2026:** come raccomandato: se il banco lo misura diverso, si estende la D-56 a questa rotta. Non è una deviazione.
4. **La domanda di «🖨 Stampa e conferma» è quella dello standalone** («Stampare N etichetta/e e confermare la
   consegna?») anche con la stampante: non nomina la stampante. **Raccomandazione:** tenere così; il nome della coda è
   nel messaggio dopo.
   **Deciso da Stefano il 15/11/2026:** tenere così: la domanda non nomina la stampante. Non è una deviazione.
5. **Testi solo del Go** (review): «Nessuna busta da confermare: … usa «🖨 Ristampa».», «La stampante delle etichette è
   cambiata: controlla e riprova.», «… ma la consegna era già confermata: nessuna busta da timbrare.». Sono della
   stampa diretta (D-09), che lo standalone non ha. **Raccomandazione:** tenerli; Stefano può cambiarne le parole.
   **Deciso da Stefano il 15/11/2026:** tenerli (le parole si possono cambiare con il manuale). Non è una deviazione.
6. **Buste che cambiano mentre `lp` lavora** (parità con lo standalone): `handover.labels_printed` dice le buste
   stampate (`repair_ids` del PDF); la conferma timbra le buste che la consegna ha in quel momento, come
   `confirm_handover/2` dello standalone, che conferma le buste lette alla conferma e non quelle del PDF aperto prima.
   Una busta aggiunta durante il lavoro esce senza etichetta stampata (si vede dal confronto delle due righe).
   **Raccomandazione:** tenere così.
   **Deciso da Stefano il 15/11/2026:** tenere così. Non è una deviazione.

## Banco finale (sessione principale)

`s078q`, 15/11/2026, istanza nuova dopo la correzione del `Content-Type`: HTTP 71 scenari, 1203 passi, 0 inattese,
0 errori; browser 19 scenari, 38 viste, 0 inattese, 0 errori. `scripts/check` rifatto dalla sessione principale: verde.
