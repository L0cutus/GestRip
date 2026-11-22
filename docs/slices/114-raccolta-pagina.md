# 114 — Pagina Assistente, parte raccolta dei dettati (T4.10, quattordicesima)

- **Fonte:** piano della T4.10, riga 114 (approvato da Stefano il 19/11/2026). **Chi decide:** lo standalone a
  `go-baseline` (`Training.status/set_collect/refill/exclude/exclude_customer`, `assist_live.ex`; test
  `training_test.exs`, `training_collection_live_test.exs`), salvo le D approvate.
- **Voci:** RIP-H-49, RIP-H-50, ADM-12 (segno `// inventario:` sui test di `internal/buste` e `internal/web`).
  **D applicate:** D-07 (testo), D-105 (non toccata), D-108 (non toccata). **D nuova:** D-109 (22/11/2026, registrata dal
  coordinatore): numero dell'etichetta per l'esclusione, sempre rifiuto con più buste.
- **Fuori piano, approvate da Stefano il 21/11/2026:** la CI con i test falliti e `go-test.json` come artefatto; il 403
  dei messaggi del cliente da `/r/{token}`.
- **Modelli:** implementatore Opus (cancellazioni irreversibili), revisione e mutazioni Opus, diagnosi del banco Opus.
- **Prova «in ombra»:** il compito dato all'implementatore è salvato, fuori dal repo, in
  `to-be-deleted-after-use/ombra-114/brief-implementatore.md`, per il confronto del coordinatore con il modello locale.

## Cosa c'è

- `internal/buste/raccolta_pagina.go`: stato (accesa/spenta, rimaste, dettati raccolti, clienti esclusi), interruttore,
  nuovo lotto, esclusione di una busta e di un cliente. Ogni azione è una transazione, con la ricerca della busta
  dentro (lo standalone la fa prima). Audit come lo standalone, solo numeri: `assist.training_collect`,
  `assist.training_refill`, `assist.training_excluded`, `assist.training_customer_excluded`.
  - Prima accensione: lotto da 500. Spegnere e riaccendere non ricarica. Accensione rifiutata a lotto esaurito o
    illeggibile. Nuovo lotto solo se il valore salvato è esattamente «0», come lo standalone.
  - Esclusione di un cliente: cancella i dettati di tutte le sue buste, anche nel cestino, e scrive la riga in
    `dettati_esclusioni`, che la 113 già rispetta al salvataggio.
- **D-109**: la busta si cerca come nella consegna (D-89/D-95), riusando `bustaPerNumeroScelta` in
  `internal/buste/consegne.go`, con due parametri: `conCestino` (la raccolta cerca anche nel cestino, come
  `with_discarded`) e `sceltaAnno` (consegna e lotto scelgono ancora la gemella dell'anno in corso; la raccolta no). Con
  più buste la raccolta rifiuta sempre con «Più buste corrispondono a «X»: scrivi il numero con l'anno o usa il QR.» e
  non cancella niente. Gli esiti nominano il numero visibile («Busta 0045/26 esclusa: …», «Busta N. 45/26 …» senza
  etichetta). `inTransazione` delega ora a `inTransazioneTx`.
- `internal/web/raccolta_pagina.go` e `pagine/assistente.html` (sezione `#training`): cinque rotte
  `POST /settings/assistente/raccolta/{accendi,spegni,lotto,escludi,escludi-cliente}`, solo titolare, con CSRF. Nuovo
  lotto ed esclusione del cliente chiedono conferma; senza JavaScript la conferma la disegna il server e non si scrive
  niente (come la D-70). Un guasto lascia nel registro solo una riga fissa senza il numero.
- Le tre frasi della D-07, in un paragrafo a parte (`#training-privacy`): «I dettati raccolti restano solo su questo
  apparecchio: non vengono mandati a nessuno e si esportano solo a mano. La raccolta resta spenta finché il titolare non
  la accende. Il modello addestrato con questi testi, resi anonimi, potrà essere ceduto ad altri negozi.»
- **CI** (`.github/workflows/ci.yml`, `tools/testfalliti`): se il primo `go test -json > go-test.json` fallisce,
  `go run ./tools/testfalliti go-test.json` stampa i pacchetti che non compilano e i test falliti con le loro righe; poi il
  secondo giro com'era. Passo `actions/upload-artifact` (v7.0.2, fissato per hash) con `if: failure()`, 7 giorni. Il resto
  della CI non cambia.
- **Messaggio del cliente**: in `internal/web/statici/app.js` il gestore di `#public-thread-form` manda il modulo
  urlencoded (`new URLSearchParams(new FormData(form))`), perché `CSRF.Modulo` legge il token solo da lì: prima
  `POST /r/{token}/messaggio` dava 403 e il cliente vedeva «Errore durante l'invio.». Gli altri `new FormData(` vanno a
  rotte con file (`ModuloConFile`/`rottaConFile`), che leggono il multipart: restano così.
- Banco: scenario HTTP `assistente-raccolta` (`in_fondo`, sola lettura della pagina: le azioni nello standalone sono
  eventi LiveView); scenari browser `raccolta-interruttore` (accende, spegne, rimette com'era), `raccolta-esclusioni` (in
  fondo, dopo `dettato-lotto`: esclusioni, cliente, nuovo lotto), `messaggio-cliente` (in fondo: un messaggio vero dalla
  pagina `/r/`); `dettato-lotto` ora guarda anche il contatore. Differenze attese: una riga D-07 (HTTP) e quattro D-109
  (browser).
- `docs/SVILUPPO.md`: sezione sulla pagina della raccolta e punto D-109.

## Prove

- `internal/buste/raccolta_pagina_test.go`: interruttore, accensione a lotto esaurito (0, negativo, illeggibile, vuoto),
  nuovo lotto (anche «00», «tanti», « 0», «-0» rifiutati), esclusione di una busta (spazi e zeri, cestino, «non
  raccolta», «non trovata»), esclusione del cliente, tutto o niente con l'audit guasto, solo numeri;
  `TestRaccoltaEsclusioneNumeroEtichetta`, `TestRaccoltaGemelleDiAnniDiversi` (rifiuto con «45» e «0045», consegna e
  lotto scelgono ancora il 2026), `TestRaccoltaGemelleInStatiDiversi`, `TestRaccoltaNumeroInternoNelCestino`.
- `internal/web/raccolta_pagina_test.go`: pagina, interruttore, nuovo lotto, esclusioni, solo titolare, guasto, numero
  dell'etichetta; rifiuto senza CSRF, conferma senza JavaScript che non scrive.
- `internal/web/conversazione_test.go`: `TestScriptMessaggioClienteURLEncoded` (rosso prima della correzione) e
  `TestPaginaClienteMessaggioMultipart` (403 in multipart, passa in urlencoded).
- `tools/testfalliti`: `TestStampaFalliti`; prova in locale su un JSON con un test e un pacchetto che falliscono apposta.
- Rossi prima di ogni giro di codice (dominio, pagina, CI, messaggio, D-109, revisione).
- `scripts/check` verde, rilanciato dal capo-slice quattro volte; l'ultima con l'esito del banco.

## Mutazioni

- Implementatore, sulle correzioni della revisione: 6 su 6 uccise.
- Revisore, primo giro: 35 compilanti, 31 uccise, 4 sopravvissute (3 buchi di test, poi chiusi; 1 equivalente: la
  conferma accetta qualunque valore non vuoto). Secondo giro sulle correzioni: 26 su 26 uccise, comprese le tre
  sopravvissute di prima.

## Review (Opus, indipendente)

- **ALTA:** con le gemelle la raccolta sceglieva da sola l'anno in corso, contro la D-109 aggiornata → corretta
  (`sceltaAnno`).
- **MEDIA:** nessun test con gemelle in stati diversi → aggiunto.
- **BASSE:** nuovo lotto con «00» o illeggibile non provato; cestino nel ramo del numero interno non provato; funzione
  di transazione duplicata → tutte corrette. Una busta gemella non si può escludere da sola da questa pagina (il testo
  del rifiuto, dalla D-109, dice «usa il QR», ma qui il QR non c'è) → in Dubbi.
- Secondo giro sulle correzioni: nessun rilievo nuovo.

## Banco s114 (22/11/2026, con il «memoria libera» del coordinatore)

- **s114a, rosso.** HTTP: 4 inattese in `assistente-raccolta`, per un'attesa sbagliata. Lo standalone scrive `value=""`
  nei due campi delle esclusioni, il Go non mette l'attributo, la casella è vuota in tutte e due: tolti i due controlli
  dallo scenario. Browser: 99 inattese e 4 errori. Erano tutti la nota 8 dello standalone, quattro comparse con «Database
  busy» o «database is locked» subito dopo un `GET /audit`, in `consegne-solleciti`, `storico-correzioni` (PC e telefono)
  e `whatsapp`, più 2 a catena in `anagrafica-artigiani[1]`. Diagnosi di un sottoagente Opus sui `docker logs`: nessun
  difetto del Go, nessun effetto degli scenari nuovi su quelli dopo. Ore in `docs/domande-aperte.md`, nota 8.
- **s114b, verde** (unica ripetizione, con l'ok del coordinatore). HTTP: 91 scenari, 1373 passi, differenze attese 1571,
  inattese 0, errori 0. Browser: 38 scenari, 76 viste, attese 49, inattese 0, da decidere 0, errori 0. Nota 8 non
  comparsa (0 righe busy/locked). La guardia sotto 10 GB non è mai scattata.
- Copertura con l'esito di s114b: 353 voci, chiuse 225, rosse 0, parziali 16, aperte 101, fuori v1 11 (senza banco:
  chiuse 49, parziali 192).

## Per il manuale

- In «Assistente e dettatura» c'è la sezione «Dettati per l'addestramento». Dice se la raccolta è accesa o spenta e
  mostra solo numeri (buste rimaste nel lotto, dettati raccolti, clienti esclusi), mai un dettato.
- La raccolta è spenta finché il titolare non la accende. La prima accensione apre un lotto di 500 buste; spegnere e
  riaccendere non lo ricarica.
- A lotto finito la raccolta non si riaccende: compare «Avvia un nuovo lotto di 500 buste», che chiede conferma.
- «Escludi» con il numero dell'etichetta di una busta (per esempio «0045/26», «0045» o «45») cancella il suo dettato;
  la busta resta. Se più buste hanno quel numero, il programma chiede di scrivere anche l'anno.
- «Escludi il cliente», dal numero di una sua busta, chiede conferma, cancella i dettati di tutte le sue buste e non
  raccoglie più le sue buste future.
- Solo il titolare vede e usa questa sezione.
- I clienti riescono di nuovo a mandare un messaggio dalla loro pagina: prima ricevevano «Errore durante l'invio.».

## Dubbi

1. **Busta gemella da escludere da sola:** il rifiuto della D-109 dice «usa il QR», ma questa pagina non legge il QR né
   accetta il codice `RIP-…`. Per una gemella senza l'anno giusto oggi non c'è un modo da questa pagina; la scheda della
   115 coprirà il caso del cliente.
2. **Segnaposto «118/26»** dei due campi: è quello dello standalone (uguale nel banco), ma con la D-64 il numero
   che si scrive è quello dell'etichetta. Da decidere se cambiarlo, per esempio in «0045/26».
3. **Il testo dello standalone** promette cose che nel Go arrivano dopo (cestino e 12 mesi nella 115, esportazione con la
   OPS-09). È tenuto così.
4. **Gemelle e cestino nel banco:** i dati sintetici non hanno gemelle né buste nel cestino; questi casi sono coperti solo
   dai test Go.
5. **CI:** il secondo giro di `go test` dopo l'estrazione è tenuto, per leggibilità; si può togliere per risparmiare
   tempo.
6. **LSP:** né la sessione né i sottoagenti hanno potuto usarlo («gopls» non nel PATH del processo di Claude Code,
   partito prima della modifica del 21/11).
