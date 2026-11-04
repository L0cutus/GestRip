# 046 — Stampa diretta CUPS e «Condividi / Stampa» (T4.4, quinta)

- **Fonte:** PIANO §2.3 «Stampa CUPS dietro l'interfaccia `Printer`» (opzione A: `lp`, `lpstat -e`,
  `lpoptions -p X -l` con `LC_ALL=C`); riga 046 del piano della T4.4 nella ricevuta della 042;
  `assets/js/pdf_share.js` dello standalone a `go-baseline` (slice 199).
- **Voci:** RIP-S-48 e RIP-S-43 (la stampa diretta della D-08 e della D-09, seconda metà), RIP-S-44
  («Condividi / Stampa» dal telefono).
- **Deviazioni:** D-08 e D-09 (approvate il 27/10 e il 29/10/2026: stampa diretta del 57×32 e del
  76×127, conferma solo se CUPS accetta), D-66 (la pagina del nuovo anno). Le proposte di questa
  slice (pagina «Stampanti», «🖨 Stampa etichetta», «Condividi / Stampa») sono **approvate da Stefano
  il 04/11/2026**, insieme a un cambio della D-08 (sotto, «D-08 cambiata»).
- **D-08 cambiata (Stefano, 04/11/2026):** niente più domanda «Etichette stampate correttamente?».
  Con il solo PDF `GET /labels.pdf` salva subito `max(ultimo, a)` con formato e audit, come lo
  standalone dopo la sua 210; con la stampa diretta il contatore avanza appena CUPS accetta il lavoro.
  Motivo: un buco nella numerazione non fa danni, un doppione sì; l'intervallo lo si cambia a mano.
  Tolti dalla 045 «Sì, stampate», «No», `POST /labels/conferma`, `GET /labels/genera` (la pagina «PDF
  pronto»), `label_sheet.confirmed`, i loro test, passi del banco e le 49 righe D-08 di
  `bench/deviazioni.json`.
- **Lavoro:** implementazione di un sottoagente. Review, mutazioni e giro finale alla sessione
  principale.

## Cosa c'è

- **`internal/printing`** (nuovo):
  - `Printer` con `List` e `Print`, come nel PIANO; `ConOpzioni` (`lpoptions -p <coda> -l`) a parte.
  - `CUPS`: comandi con il **percorso assoluto** (`/usr/bin`, o la cartella di `-comandi-stampa`),
    lanciati senza shell, con gli argomenti separati, l'ambiente fisso `LC_ALL=C`, `LANG=C`,
    `PATH=/usr/bin:/bin` (niente `HOME`: le opzioni personali di `~/.cups` non contano) e un tempo
    massimo (10 s per `lpstat`/`lpoptions`, 30 s per `lp`). Il comando ha un gruppo di processi
    suo (`Setpgid`) e il tempo scaduto uccide tutto il gruppo, figli e nipoti; `WaitDelay` di 2 s
    resta come rete. Uscita letta al più per 256 KiB. **Senza `-comandi-stampa` `serve` usa i comandi
    veri di `/usr/bin`**: su una macchina di sviluppo con stampanti vere va sempre passato un finto.
  - **Il PDF va a `lp` sullo standard input**, non in un file temporaneo: nessun file con i dati della
    busta sul disco, niente da togliere dopo un errore o un arresto a metà, nessuna cartella da
    proteggere; `lp` lo copia nello spool di CUPS prima di rispondere.
  - «A buon fine» = `lp` esce con 0 **e** risponde `request id is <coda>-<n> (<k> file(s))`
    (`lp.c`); l'id del lavoro si legge da lì. Un'uscita diversa da 0 è un `Rifiuto` con la prima riga
    del messaggio di CUPS (ripulita dai caratteri di controllo, al più 200 caratteri); un'uscita 0
    senza quella riga è un `Rifiuto` anche lei (non si conta un lavoro di cui non si sa niente). Il
    tempo scaduto è `ErrTempoScaduto`; un comando che manca `ErrNonDisponibile`.
  - **Niente iniezione**: la coda passa solo se `CodaValida` (lettere e cifre ASCII, poi `_ . - @`,
    al più 127, mai `-` in testa, niente spazi né `/`); le opzioni solo da `LeggiOpzioni` (chiave che
    comincia con una lettera, valore `[A-Za-z0-9_.+-]`, al più 16, niente ripetute) e vanno una per
    `-o`. Vietate a mano: `PageSize`, `PageRegion`, `media`, `media-col`, `fit-to-page`,
    `print-scaling`, `scaling`, `number-up`, `page-ranges`, `document-format`. Il titolo del lavoro
    è il nome del file, ridotto a `[A-Za-z0-9_.-]`.
  - `PageSize=Custom.<L>x<H>mm` da `FormatoPagina`, con i millimetri del documento (al più due
    decimali: `57x32`, `76x127`, `57.5x20.25`).
  - `lpstat -e`: un nome per riga; senza code («No destinations added») l'elenco è vuoto. I nomi che
    `CodaValida` non accetta si mostrano come non sceglibili.
  - `Finta`: code fisse, lavori in memoria, errori scelti dal test, stesse validazioni di `lp`.
- **Dominio** (`internal/buste/stampanti.go`): le scelte nelle impostazioni con quattro chiavi **solo
  Go** (`stampa_coda_etichette_qr`, `stampa_opzioni_etichette_qr`, `stampa_coda_etichetta_busta`,
  `stampa_opzioni_etichetta_busta`, aggiunte a `importer.ChiaviSoloGo`); `SalvaScelteStampa` con
  `printer_settings.updated` (coda, opzioni e coda di prima per i due documenti);
  `StampaEtichetteAccettata` (dopo che CUPS ha accettato: in **una** transazione formato come il PDF,
  contatore `max(ultimo, a)` e anno come il PDF, `label_sheet.printed` con i dati del foglio,
  `previous`, `last`, `queue`, `job`); `EtichettaDaStampare` (PDF senza riga) e
  `StampaEtichettaAccettata` (`repair.label_printed` con `bag_number`, `queue`, `job`). Il PDF
  (`GeneraEtichette`) e la stampa usano lo stesso cuore (`contaFoglio`): contatore `max(ultimo, a)` e
  anno del D-66 = il più nuovo fra quello salvato e quello del foglio, ma **solo per un anno del
  modulo (2024…2099)**: `/labels.pdf` accetta ogni anno come lo standalone, e un anno sbagliato
  (20267) salvato fermerebbe per sempre la proposta del D-66 (lo stesso caso della review della 045).
- **Pagine e rotte** (`internal/web/stampanti.go`, `pagine/stampanti.html`):
  - `GET`/`POST /settings/stampanti`, **solo titolare**, scheda «Stampanti» dopo quelle dello
    standalone (`schedeSoloGo`). Per «Etichette QR» e «Etichetta busta» un menu con «Nessuna (solo
    PDF)» (il valore iniziale) e le code di `lpstat -e`, e il campo «Opzioni». Se la coda scelta ha
    le sue opzioni (`lpoptions`), la pagina le mostra in «Opzioni di questa stampante» e il
    salvataggio le controlla (chiave che esiste, valore fra le scelte); se `lpoptions` non risponde
    bastano le regole della forma. Errori sotto il campo, 422, niente salvato. Se CUPS non risponde la
    pagina mostra il motivo («CUPS non risponde: …» o «CUPS non ha risposto in tempo»), tiene le
    scelte salvate e permette solo di tenerle o di scegliere «Nessuna». La pagina sa dire anche
    «Stampa diretta non disponibile», ma solo quando la faccia è costruita senza `Stampanti` (i
    test): `serve` la costruisce sempre, con CUPS.
  - **Modulo `/labels`** come lo standalone, **anche con la coda scelta**: `GET /labels.pdf` con
    «⬇ Scarica PDF» (il testo dello standalone) e nessun campo in più. Con la coda scelta, accanto,
    «🖨 Stampa su <coda>»: un secondo pulsante dello stesso modulo GET con
    `formaction="/labels/stampa"`, senza token, così porta i valori scritti anche senza JavaScript.
  - `GET /labels/stampa` (personale, solo Go): la **pagina intermedia**. Rifà le regole del PDF (400,
    422) e vuole l'anno del modulo 2024…2099 (400); senza coda torna al modulo con «Nessuna stampante
    scelta…». Chiede «Stampa N etichette (0045/26–0094/26) su <coda>?» con il modulo POST (CSRF,
    `invio` monouso, `continua=1`), «🖨 Stampa» e «Annulla» (link a `/labels`). Se vale la D-66,
    prima della domanda c'è la proposta: per il titolare l'azzeramento e il pulsante «Continua senza
    azzerare: 🖨 Stampa», per il dipendente l'avviso e «Continua: 🖨 Stampa». Un solo passaggio
    intermedio. La pagina non stampa e non salva niente.
  - `GET /labels.pdf`: salva come lo standalone (sopra). **D-66:** se l'anno del foglio (2024…2099) è
    più nuovo dell'anno salvato e il contatore non è 0, al posto del PDF una pagina (200, HTML)
    «Nuovo anno: ripartire da 1?» con i numeri del foglio e l'ultimo stampato; il titolare ha
    «Azzera contatore» (la pagina di conferma della D-22, poi `/labels?anno=N`) e «Continua senza
    azzerare: scarica il PDF», il dipendente l'avviso «chiedi al titolare» e «Continua: scarica il
    PDF». Continuare è la stessa richiesta con `continua=1`. Senza l'anno salvato (dati importati)
    niente pagina. È la sola deviazione del PDF rispetto allo standalone, oltre a «0045/26».
  - `POST /labels/stampa` (personale): dalla pagina intermedia. Stesse regole del PDF (limite di 100
    → 422, valori non validi → 400, CSRF) e in più l'anno 2024…2099 (400). Un POST senza
    `continua=1` (forgiato) con la condizione della D-66 torna alla pagina intermedia senza stampare.
    Un solo `lp` per il foglio.
    Se CUPS accetta: la transazione qui sopra e `/labels` con «Inviate N etichette a <coda> (lavoro
    <id>). Contatore aggiornato: ultimo numero stampato M.». Se CUPS rifiuta, non risponde o va in
    tempo scaduto: niente salvato, nessuna riga, e il modulo con i valori del foglio e «Stampa non
    riuscita: … Il contatore non è cambiato: puoi riprovare, oppure scaricare il PDF.». Per il tempo
    scaduto il messaggio dice che il lavoro potrebbe essere partito lo stesso.
  - `POST /repairs/{id}/stampa-etichetta` (personale): nella barra della scheda, dopo «📤 Scarica
    etichetta», «🖨 Stampa etichetta» solo con la coda scelta. Lavoro 76×127 con le opzioni della coda;
    poi `repair.label_printed` e la scheda con «Etichetta inviata a <coda> (lavoro <id>).». Un rifiuto
    torna alla scheda con «Etichetta non stampata: … Puoi aprire il PDF con «🖨 Etichetta busta».» e
    niente riga. Le regole della scheda (segno `mostra`, `dopoRifiuto`, `allaScheda`) come le altre
    azioni.
  - **Doppio clic e doppio invio:** i due moduli portano un identificativo monouso (`invio`, lo stesso
    meccanismo della nuova busta: il secondo invio aspetta il primo e ne mostra l'esito, senza un
    secondo lavoro); in più un blocco in memoria per foglio (numeri e formato) e per busta tiene fuori
    un secondo lavoro finché il primo non ha risposto (due schede aperte) con «Stampa già in corso:
    aspetta l'esito prima di riprovare.». Il modulo della scheda e quello della pagina intermedia
    hanno `data-invio-unico` (il pulsante si spegne); il modulo `/labels` no, perché scaricare il PDF
    non cambia pagina e il pulsante resterebbe spento.
  - Il lavoro e la transazione girano con `context.WithoutCancel`: un browser che se ne va non ferma
    `lp` a metà.
  - **Esito del doppio invio con il tipo:** l'esito ricordato per l'invio porta il tipo (riuscito,
    errore, avviso) e il secondo invio lo mostra con il colore del primo. Con il **tempo scaduto**
    l'invio si chiude con il suo errore (il lavoro potrebbe essere già nella coda): lo stesso modulo
    rimandato non chiama di nuovo `lp`. Un rifiuto di CUPS invece lascia rimandare il modulo.
  - Se CUPS accetta ma la transazione non si chiude: «Etichette inviate a <coda> (lavoro <id>), ma il
    contatore non è stato aggiornato: controlla i numeri proposti prima del prossimo foglio.» (per la
    busta: «…, ma la stampa non è stata registrata.»), come errore; il modulo rimandato non rimanda il
    lavoro. Fuori dalla transazione, senza garanzia, si scrive la traccia del lavoro partito:
    `label_sheet.printed` (o `repair.label_printed`) con esito `counter_not_saved`, coda e id del
    lavoro; se non si scrive nemmeno quella, resta la riga `Error` del registro con coda e lavoro.
- **`serve`:** `Dipendenze.Stampanti = printing.NuovoCUPS(-comandi-stampa)`; l'opzione
  `-comandi-stampa` vuole una cartella assoluta (documentata in `docs/SVILUPPO.md` con le opzioni di
  sviluppo e banco, più una sezione «Stampa diretta CUPS»).
- **«Condividi / Stampa» (RIP-S-44):** `pdf_share.js` portato in `app.js` con lo stesso contratto:
  un link al download (`?download=1`) segnato `data-share-pdf` con l'indirizzo del PDF; il tocco
  scarica il PDF e lo passa come file a `navigator.share` con il nome di `content-disposition`; dove
  non si può (o qualcosa va storto) scarica; annullare (`AbortError`) non fa niente. **Nessuna pagina
  portata lo usa ancora:** lo standalone lo mette solo nel riquadro «fuori bottega» della scheda
  («📤 Condividi», artigiani T4.8), nello storico e nella sessione delle consegne e nel registro
  d'emergenza. Sulla scheda i pulsanti delle etichette non lo usano, come nello standalone. Pronto
  per quelle slice; il contratto è fissato da `TestCondividiPDFNelloScript`, come
  `pdf_share_asset_test.exs`. **RIP-S-44 resta parziale:** `TestCondividiPDFSullePagine` (marcato
  RIP-S-44) si salta finché nessuna pagina di `internal/web/pagine` contiene `data-share-pdf`; quando
  una pagina lo usa, controlla che il controllo sia un link al download.
- **Banco:** il Go del banco parte con `-comandi-stampa /stampa-finta`, una cartella in sola lettura
  con `lp`, `lpstat` e `lpoptions` finti (`tools/banco/stampa.go`): code `Etichette_QR`,
  `Buste-76x127` e `Rifiutata` (elencata, rifiutata da `lp` con il messaggio di CUPS). Il container
  del Go non ha CUPS (controllato: `/usr/bin/lp` assente). Scenari nuovi, con i passi della stampa
  `solo_go`: `stampa-diretta` (D-08, 16 passi: pagina, opzioni rifiutate, salvataggio, stampa
  accettata, coda rifiutata con il modulo che resta, ritorno a «Nessuna») e
  `stampa-etichetta-busta` (D-09, 8 passi, `prova_con` sintetici-import, con le attese del database
  vuoto; l'apertura della scheda è un passo confrontato sulle due app, così le righe `repair.viewed`
  restano uguali). **Scenari della 045 rifatti:** tolti i passi della conferma; nel titolare, prima
  del PDF del 2026 dopo un foglio del 2025, due passi `solo_go` D-66 (la pagina e «Continua»), così il
  passo confrontato dopo trova le due app allineate; il passo della D-66 del 2027 ora è
  `/labels.pdf`, e l'allineamento dopo l'azzeramento è un PDF solo Go invece di una conferma. In
  `bench/deviazioni.json` le righe delle etichette passano da 63 a 11: 6 D-64 e 1 D-21 (testo del
  PDF), 4 D-66 (proposta e avviso del nuovo anno nel modulo); nessuna D-08.

## Per il manuale

- In «🏷️ Etichette QR» «⬇ Scarica PDF» scarica il foglio e aggiorna subito il contatore, come prima
  del Go: non c'è più la domanda «Etichette stampate correttamente?». Se un foglio non è uscito bene,
  si ristampano gli stessi numeri cambiando «Da» e «A» a mano.
- Se si chiede un foglio di un anno nuovo e il contatore non è a zero, prima compare «Nuovo anno:
  ripartire da 1?»: il titolare può azzerare o continuare; il dipendente vede che deve chiedere al
  titolare, e può continuare.
- In Impostazioni → «Stampanti» il titolare sceglie dove stampare le etichette QR e l'etichetta busta:
  una stampante oppure «Nessuna (solo PDF)», che è la scelta iniziale.
- Per ogni stampante si possono scrivere delle opzioni, per esempio «MediaTracking=Gap Darkness=8».
  Il formato della pagina lo mette GestRip. Se la stampante (CUPS) non risponde, la pagina lo dice.
- Con una stampante scelta, nel modulo delle etichette QR c'è anche «🖨 Stampa su <stampante>»: si
  apre la domanda «Stampa N etichette (…) su <stampante>?» con «🖨 Stampa» e «Annulla». Se la
  stampante accetta il lavoro, il contatore si aggiorna e compare «Inviate N etichette…». Se la
  stampa non riesce, il contatore non cambia e si può riprovare o scaricare il PDF. A un anno nuovo
  la proposta «Nuovo anno: ripartire da 1?» compare nella stessa domanda.
- Nella scheda della busta, con una stampante scelta, «🖨 Stampa etichetta» manda l'etichetta 76×127
  direttamente alla stampante. Un doppio tocco su «Stampa» non stampa due volte.

## Scelte di realizzazione da guardare (non di prodotto)

1. **Pagina del nuovo anno** prima del PDF o della stampa (forma proposta dal coordinatore, D-66
   dichiarata solo in quel caso), con «Continua senza azzerare» / «Continua».
2. **Testo del pulsante** «⬇ Scarica PDF», quello dello standalone, al posto di «Genera PDF»: senza
   coda il modulo è identico allo standalone e il banco non ha più righe per il modulo.
3. **Niente token CSRF in un indirizzo GET** (coordinatore, 04/11/2026): la prima versione metteva
   `_csrf_token` e `invio` nel modulo GET di `/labels` quando la coda era scelta, quindi con
   «⬇ Scarica PDF» finivano nella query di `/labels.pdf`. Un token in una URL GET finisce nella
   cronologia del browser, nei log dei proxy e potenzialmente nel `Referer`, e che oggi il server
   ignori la query non basta. Ora «🖨 Stampa su <coda>» porta in GET, senza token, alla pagina
   intermedia `GET /labels/stampa`, e il token sta solo nel suo modulo POST. Costa un tocco in più,
   e la stessa pagina ospita la proposta della D-66 (un solo passaggio intermedio).
4. **Testi** dei messaggi e il doppio invio: il secondo invio dello stesso modulo mostra l'esito del
   primo; da un'altra scheda «Stampa già in corso…» finché il primo non risponde, poi una ristampa è
   permessa.

## Resta per la T5.5 (prova vera sulla MUNBYN ITPP130)

- Il nome vero della coda e le opzioni vere del suo driver (`lpoptions -p <coda> -l`):
  `MediaTracking`, `Darkness` e gli altri nomi possono cambiare con il PPD.
- Che `PageSize=Custom.57x32mm` e `Custom.76x127mm` siano accettati dal driver (con un PPD CUPS li
  legge con le unità; con una coda IPP «driverless» potrebbe servire `media=Custom.…`, oggi vietata
  a mano) e che la pagina esca alla misura giusta, senza scala né margini.
- Il QR a 16×16 mm (moduli di circa 0,32 mm) su una termica a 203 dpi (nota della 045).
- Il messaggio vero di `lp` con la stampante spenta, senza carta o con la coda in pausa: CUPS di
  solito **accetta** il lavoro anche così (il lavoro resta in coda). Il contatore si aggiorna lo
  stesso; il PIANO dice «a buon fine = lp accetta». Da vedere se basta o se serve guardare lo stato
  della coda (`lpstat -p`) prima di stampare.
- Il tempo di `lp` con un PDF di 100 etichette (oggi 30 s di massimo).

## Misurato (sul banco, 04/11/2026)

- Il Go nel container del banco: `/usr/bin/lp` e `/usr/bin/lpstat` assenti, `/stampa-finta` in
  sola lettura con i tre comandi finti.
- Le uscite dei comandi finti sono quelle documentate di CUPS 2.x (`request id is …`, «lp: The printer
  or class does not exist.», «lpstat: No destinations added.», `lpoptions -l`): scritte a mano dalla
  documentazione e dal sorgente, **non** misurate su un CUPS vero (regola della slice: niente comandi
  CUPS veri su pc-fisso).

## Solo ragionato

- La forma `PageSize=Custom.<L>x<H>mm` per una coda con PPD (`ppdPageSize` accetta le unità `mm`).
- Che `lp` legga lo standard input quando non ha file (comportamento documentato di `lp`).
- `context.WithoutCancel`: il browser che chiude la connessione durante `lp` non lo ferma (nessun
  test che chiuda davvero la connessione a metà).

## Prove

- **Test Go nuovi:** `internal/printing/cups_test.go` (comandi finti sh in una cartella temporanea che
  registrano argomenti, ambiente e standard input: un solo `lp` con gli argomenti separati, il PDF
  intero, `LC_ALL=C` e niente `HOME`; rifiuti in inglese, uscita 0 senza id, id in italiano o con
  spazi, messaggi con caratteri di controllo, tempo scaduto anche con un figlio che tiene aperta
  l'uscita, comando assente, documento vuoto, code strane, opzioni non iniettabili, titolo ripulito,
  `FormatoPagina`, `lpstat -e` anche senza code e con lo scheduler fermo, `lpoptions -l`, controllo
  delle opzioni dichiarate, finta); `internal/buste/stampanti_test.go` (scelte, normalizzazione,
  rifiuti, riga d'audit, valori guasti, transazioni con il trigger che rifiuta la riga);
  `internal/web/stampanti_test.go` (pagina e salvataggio, ruoli, CUPS che non risponde, stampa senza
  stampa diretta, stampa del foglio con PDF riletto, contatore, anno, formato e audit, doppio invio
  dopo e durante, un'altra scheda bloccata, rifiuti e tempo scaduto con il modulo che resta, CUPS
  accetta ma la transazione no, etichetta busta per titolare e dipendente, rifiuto, busta che non c'è,
  coda tolta, il contratto di `app.js`); `cmd/gestrip/protezioni_test.go` (`-comandi-stampa`);
  `tools/banco/stampa_test.go` (i comandi finti del banco con il codice vero di `printing`).
  Aggiornati: tabella dei permessi, rotte con il guscio, schede delle impostazioni, `ChiaviSoloGo`,
  `prova_con` e `atteso_dati` del banco.
- **TDD:** i test sono stati scritti insieme al codice, non prima: nessun rosso salvato.
- **`scripts/check`** (sottoagente): verde due volte, la seconda con il codice finale e l'esito del
  banco (`prove/check-2.log`): copertura chiuse 64, parziali 18, rosse 0 (nella 045 chiuse 63).
- **Banco, primo giro** (sottoagente, `fab-222`, prima della review): due giri mirati sui due
  scenari nuovi (dati importati: il primo ha trovato un `{csrf}` senza una pagina prima del POST,
  corretto; database vuoto: verde, lo scenario della busta «non provato» come voluto). **Primo giro
  completo rosso**, 2 inattese in `viste` (`audit-total` di `repair.viewed`: 157 contro 155): i due
  `GET /repairs/1` solo Go dello scenario della busta aprivano la scheda solo nel Go. Corretto:
  l'apertura della scheda ora è un passo confrontato sulle due app, e il controllo del pulsante sta
  nella scheda mostrata dopo la stampa (con il segno, senza apertura). **Secondo giro completo**
  (istanza nuova, dati importati): HTTP verde, 37 scenari e 583 passi, 84 differenze attese (le
  stesse di prima), 0 inattese; browser verde, 7 scenari e 14 viste, 22 attese, 0 inattese, nessun
  «da decidere». Il giro completo sul database vuoto non è stato fatto. «AVANZI: nessuno».
- **Dopo la review e il cambio della D-08** (sottoagente, codice finale, binari ricompilati):
  - test nuovi o rifatti: `TestTempoScadutoUccideIlGruppo`, `request id` solo su stderr, tempo del
    figlio in `TestElencoCode`; `TestStampaTempoScadutoNonRimanda`,
    `TestStampaEtichettaAccettataNonRegistrata`, `TestStampaAccettataNonSalvata` con la traccia,
    `TestStampaNuovoAnno`, `TestCondividiPDFSullePagine` (salta: RIP-S-44 parziale); nel dominio
    `TestGeneraEtichetteSalvaIlContatore`, `TestContatoreDelPDF` (anche l'anno 20267 che non si
    salva); nel web `TestNuovoAnnoPrimaDelPDF` e i test delle etichette senza la conferma. Due
    mutanti provati a mano e uccisi (senza gruppo di processi; senza la chiusura del tempo scaduto):
    log in `prove/mutante-*.log`;
  - giri mirati sugli scenari delle etichette e della stampa (dati importati): il primo ha mostrato
    che la pagina del D-66 in un passo confrontato rompe le sue attese (corretto con i due passi
    `solo_go` prima), dal secondo vengono le 11 righe di `bench/deviazioni.json`, controllate una per
    una (solo D-64, D-21, D-66 del modulo); il terzo verde (4 scenari, 74 passi, 11 attese);
  - **giro completo** (`fab-222`, `-dati sintetici-import`, istanza nuova): HTTP verde, 37 scenari e
    573 passi, 32 differenze attese (84 − 63 righe delle etichette + 11), 0 inattese; browser verde, 7
    scenari e 14 viste, 22 attese, 0 inattese, nessun «da decidere». «AVANZI: nessuno»;
  - `scripts/check` verde con l'esito del banco (`prove/check-4.log`): chiuse 63, parziali 19, rosse 0
    (RIP-S-44 ora parziale).
- **Dopo la pagina intermedia** (sottoagente): `TestStampaEtichetteQR` (modulo senza token, pagina
  intermedia che non stampa), `TestStampaNuovoAnno` (proposta nella pagina, titolare e dipendente,
  POST forgiato senza `continua`), `TestPaginaStampaRifiuti` (400, 422, anno, senza coda); scenario
  `stampa-diretta` con i due passi `solo_go` della pagina intermedia (16 passi). `scripts/check` verde
  (`prove/check-5.log`); giro mirato sugli scenari delle etichette e della stampa (dati importati,
  istanza nuova): 4 scenari, 76 passi, 11 differenze attese, 0 inattese. Giro completo non rifatto
  (il resto non cambia).
- **Nessun lavoro di stampa è stato mandato a una coda CUPS vera:** nessun test, giro del banco o
  prova a mano ha lanciato `/usr/bin/lp` o `/usr/bin/lpstat` di pc-fisso.

## Review indipendente

Un sottoagente diverso ha letto il codice e le voci; non ha toccato il repo. Punti, tutti corretti
in questa slice dal sottoagente di implementazione:

1. **(Media) Doppio invio dopo il tempo scaduto di `lp`:** l'invio si chiudeva con un esito vuoto e
   lo stesso modulo rimandato chiamava di nuovo `lp` (con la finta, 2 chiamate a `Print`). Ora il
   tempo scaduto chiude l'invio con il suo errore, per il foglio e per la busta, e l'esito porta il
   tipo: anche «inviate ma contatore non aggiornato» al secondo invio esce come errore, non verde
   (`TestStampaTempoScadutoNonRimanda`, `TestStampaAccettataNonSalvata`,
   `TestStampaEtichettaAccettataNonRegistrata`). Mutante «senza la chiusura del tempo scaduto»
   provato: ucciso (2 e 4 chiamate a `Print`).
2. **(Bassa) `lp` accettato e transazione fallita:** ora c'è la traccia «best effort»
   `counter_not_saved` fuori dalla transazione, con il registro come ultima rete (stessi test, con
   un trigger che rifiuta solo l'esito `ok` e poi anche la traccia).
3. **(Bassa) Tempo scaduto e nipoti:** un nipote del comando restava vivo. Ora gruppo di processi e
   `kill(-pid)` (`internal/printing/gruppo_unix.go`, `TestTempoScadutoUccideIlGruppo` con un finto che
   lancia `sleep` in sottofondo). Mutante «senza il gruppo» provato: ucciso (il nipote resta vivo).
4. **Test mancanti:** `request id` solo sullo standard error → rifiuto; il figlio che tiene aperta
   l'uscita in `TestElencoCode` ora misura il tempo (< 5 s).
5. `TestStampaDoppioInvio` aspetta i canali con un tempo massimo (`ricevi`, 10 s) e non resta appeso
   se il blocco manca.
6. `stampanti.html` non ripete «CUPS non risponde»: mostra solo il motivo.
7. RIP-S-44 parziale con `TestCondividiPDFSullePagine` (sopra).
8. `docs/SVILUPPO.md` dice che `serve` senza `-comandi-stampa` usa i comandi veri; la frase su
   «Stampa diretta non disponibile» corretta (in produzione `Stampanti` non è mai nil).

**Mutazioni della review:** 22, 20 uccise; vive M06 (il gruppo di processi) e M09 (il tempo del
figlio che tiene aperta l'uscita), ora coperte dai test dei punti 3 e 4.
