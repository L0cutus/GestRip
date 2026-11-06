# 053 — D-64 su scheda e «Nuova riparazione» (T4.2, quarta)

- **Fonte:** riga 053 del piano della T4.2 (`050-home-lista.md`); D-64 (Stefano, 03/11/2026, terza
  versione) con lo studio `docs/studio-D64-numero-etichetta.md` (punti (b), (c), (d) e le risposte
  del 03/11/2026) e la risposta 8 del 05/11/2026 (il `bag_number` di un invio vecchio o forgiato si
  ignora); le righe «Numero della busta ed etichette» di `docs/manuale/da-scrivere.md`. Lo standalone
  a `go-baseline`: `WorkshopLive` (`render(%{live_action: :new})` con «N° Busta», «Codice QR busta» e
  `#repair-qr-code-scan`, `save_repair/3`, `create_intake/3`, il flash «Riparazione N creata.»),
  `RepairLive` (`page_title` «Busta #N», il titolo, `#repair-qr-code`, il modulo di modifica con
  `#repair-qr-code-scan`), `Repair` (`qr_code` testo libero non unico, `blank_qr?/1`, nessuna
  validazione del QR: nessuna da tenere).
- **Voci:** RIP-H-29, RIP-H-30, RIP-H-31 (il numero solo dal contatore), RIP-H-39 (il campo
  «Etichetta» e l'avviso delle gemelle), RIP-H-56 (il segno «⚠ senza QR» anche nella scheda),
  RIP-S-01, RIP-S-02, RIP-S-04 (il campo nella modifica).
- **Deviazioni:** D-64 (approvata).
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Cosa c'è

- **Dominio** (`internal/buste/etichetta_scritta.go`, nuovo; `crea.go`, `modifica.go`, `scheda.go`,
  `ricerca.go`):
  - `EtichettaScritta(testo, anno)`: il codice intero letto dal 📷 o dalla pistola
    (`RIP-2026-0045`, la forma esatta di `CodiceEtichetta`) resta, senza spazi ai bordi; un numero
    nelle forme della ricerca (`LeggiRicercaEtichetta`: «0045/26», «45/26», «0045», «45») diventa
    `CodiceEtichetta` con l'anno scritto (stesso secolo della data) o, senza, l'anno della **data di
    accettazione** (`AnnoDiAccettazione`, la stessa del numero interno); ogni altro testo resta com'è,
    spazi compresi, come oggi.
  - `Crea` salva in `codice_qr` il codice risolto e lo rimette in `Creata.CodiceQR`. Dopo tutti gli
    altri errori (anche il numero già usato: il controllo è dopo l'INSERT, nella stessa transazione,
    che l'avviso annulla) cerca le altre buste fuori dal cestino con lo stesso codice; se ce n'è di
    **aperte** dà `*GemelleAperte` (codice e buste, dalla più recente), a meno che
    `Accettazione.GemelleConfermate` sia lo stesso codice. Le chiuse (consegnata, annullata, chiusa)
    non contano. Vale per ogni codice, non solo per le etichette.
  - `righeConCodice` è la query della scansione (`CercaPerQR` ora la usa): uguale esatto, fuori dal
    cestino, dalla più recente, con una busta esclusa. Per le gemelle (creazione e scheda) il salvato
    si confronta senza spazi ai bordi da tutte e due le parti (`CodiceLetto`, candidate con `instr`
    in SQL); la scansione resta com'era (il salvato com'è, come `lookup_repairs_by_qr/2`).
  - `Scheda.Gemelle` (le altre buste con il codice della busta, senza spazi ai bordi), `GemelleAperte`,
    `GemelleChiuse`, `Scheda.NumeroVisibile`.
  - La modifica: un testo del campo diverso dal codice salvato passa da `EtichettaScritta`, con l'anno
    della data scritta nello stesso modulo (se è una data) o di quella salvata; uguale al salvato resta
    com'è (un codice importato con gli spazi non cambia rimandando il modulo); con una data salvata
    illeggibile (solo da import) il testo resta com'è, mai «RIP-1-0045». `EtichettaNelModulo` dà il
    valore del campo: «0045/26» solo se, rimandato così, ridà esattamente il codice salvato.
  - Il numero interno: `Accettazione.Numero` resta nel dominio (i test della 037d), ma la pagina non lo
    passa più: lo assegna sempre il contatore dell'anno della data di accettazione.
- **«Nuova riparazione»** (`internal/web/nuova.go`, `pagine/nuova.html`, `app.js`, `app.css`):
  - niente «N° Busta» e niente «busta #N» nel titolo; `bag_number` non è più fra i campi letti, quindi
    quello di un modulo vecchio o forgiato si ignora; `GET /nuova` non chiede più il prossimo numero;
  - il campo del QR si chiama «Etichetta (0045/26)» (nome e id `repair[qr_code]`/`repair_qr_code`,
    segnaposto dello standalone) con «📷 Scansiona» (`#repair-qr-code-scan`, l'id dello standalone,
    `data-target`). Fino alla 055 il pulsante porta solo il cursore nel campo e ne seleziona il testo
    (per la pistola); senza JavaScript non fa niente, come «Togli data». Il comportamento del 📷 non
    ha un test (è JavaScript, il banco non lo prova): è affidato alla 055;
  - l'avviso `#intake-qr-twins`: «⚠ Etichetta già usata dalla busta aperta di Cliente (0045/26):
    controlla l'etichetta, oppure premi di nuovo «Genera busta» per salvarla lo stesso.» (al plurale
    «dalle buste aperte di A (…) e B (…)»), con il riquadro arancione dell'avviso del QR mancante e il
    campo nascosto `gemelle_avvisate` con il codice risolto: premendo di nuovo si salva. Un altro
    numero dopo l'avviso si controlla da capo; un errore dei campi dopo l'avviso rimanda il codice
    avvisato (nessun secondo avviso). Insieme all'avviso delle gemelle non si vede «⚠ Manca il codice
    QR» (resta solo il campo nascosto `qr_avvisato`);
  - «Numero busta già esistente.» (il contatore dietro a un numero già usato) va in cima alla pagina,
    perché il campo non c'è più;
  - dopo il salvataggio la home dice **«Busta 0045/26 salvata»**, il testo esatto dello studio, punto
    (d) (lo standalone: «Riparazione 45/26 creata.»); anche il numero della striscia delle foto
    (`data-bag-number`) è il numero visibile (`NumeroVisibile`): «Busta N. 4/26 salvata» senza
    etichetta.
- **Scheda** (`internal/web/scheda.go`, `pagine/scheda.html`, `app.css`):
  - titolo della pagina e `<h1>` con il numero visibile, con la regola della lista: «Busta #0045/26»
    con il numero interno «N. 45/26» in piccolo (`#repair-internal-number`), «Busta #0045/25» per
    un'etichetta riusata dell'anno prima, «Busta N. 45/26» senza etichetta o con un codice di altro
    formato; «⚠ senza QR» (`#repair-noqr`) per una busta aperta senza codice. Anche con il modulo
    della modifica aperto;
  - sotto il titolo `#repair-label-twins`: «⚠ Un'altra busta ha la stessa etichetta:» (o «Altre N
    buste hanno…») con il link a ogni gemella **aperta** («Cliente · data · stato»); se una gemella è
    **chiusa** e il codice è un'etichetta, il link «storico di questa etichetta» a
    `GET /cerca?cerca=0045%2F26` (052: con più buste la lista di tutte, chiuse comprese);
  - il riquadro «Dati riparazione» tiene «Codice QR busta» con il codice salvato, come lo standalone.
- **Modifica (033):** il campo si chiama «Etichetta (0045/26)» (nello standalone «Codice QR busta»)
  con «📷 Scansiona» (che lo standalone aveva già) e accetta le stesse forme di «Nuova riparazione».
  Il valore mostrato è «0045/26» (studio, punto (d): «mostrata come 0045/26») quando, rimandato così,
  ridà esattamente il codice salvato; altrimenti il codice com'è («RIP-2026-10000», un codice con gli
  spazi, uno di altro formato). Nessun avviso delle gemelle (vedi i Dubbi).
- **Documenti:** `docs/SVILUPPO.md` (numero visibile ora anche nella scheda, il «#» solo nei titoli e
  nella lista, la regola dell'etichetta scritta e delle gemelle, il rischio del contatore indietro, lo
  scenario `busta-etichetta` in fondo), il commento di `visibile.go`.

## Deviazioni

- **D-64** (approvata), dichiarata così nel banco:
  - normalizzatore D-64 sui passi delle schede di `scheda` (5 passi), `numeri` (2), `home` (passo 11,
    la 27/26: «Busta #0048/26 · Laboratorio») e del nuovo `busta-etichetta`;
  - **20 righe** in `bench/deviazioni.json`. 14 per i due `GET /nuova` di `nuova` (passi 3 e 8):
    «busta #», «N° Busta», «Codice QR busta», `id="repair_bag_number"`, `name="repair[bag_number]"`,
    `inputmode="numeric"` (presenti nello standalone, non nel Go) ed «Etichetta (0045/26)» (il
    contrario). `#repair-qr-code-scan` e «📷 Scansiona» ora sono uguali nelle due app. 2 per la
    modifica (`modifica` passo 8: «Etichetta (0045/26)» e «Codice QR busta»; il 📷 è confrontato e
    uguale). 4 del livello browser (`coda`, desktop e telefono: `salva-1.messaggio` e
    `chiave.messaggio`, «Riparazione B1 creata.» nello standalone, «Busta B1 salvata» nel Go; tolte
    le due attese di quel messaggio da `bench/browser/coda.json`);
  - passi `solo_go` D-64 in `busta-etichetta` per quello che lo standalone non ha (gemelle, storico,
    numero interno, «⚠ senza QR» nella scheda, l'avviso in «Nuova riparazione»).

## Prove

- **Test Go** (database vero, orologio al 31/10/2026):
  - `internal/buste/etichetta_scritta_test.go`: `TestEtichettaScritta` (32 testi), `TestCreaEtichettaScritta`
    (8 casi, anno della data anche di un altro anno, `Creata.CodiceQR`), `TestCreaGemelleAperte`
    (chiuse, cestino e altro anno senza avviso; l'aperta con quattro conferme sbagliate e il database
    uguale; due aperte in ordine; conferma in un'altra forma; codice libero; prima campi, numero già
    usato e nome), `TestSchedaGemelle`, `TestModificaEtichettaScritta`.
  - `internal/web/etichetta_test.go`: `TestNuovaEtichetta` (5 forme, i messaggi «0045/26»,
    «0046/25», «N. 4/26»), `TestNuovaGemelleAperte` (solo chiusa, avviso con il modulo com'è e senza
    salvare, plurale, salva lo stesso, errore dopo l'avviso, QR mancante), `TestSchedaNumeroVisibile`
    (5 casi, anche col modulo della modifica), `TestSchedaGemelleWeb` (aperta, chiusa, tre, lo storico
    che porta alla lista senza il cestino, codice libero, busta sola).
  - Riscritti: `TestNuovaNumero` (dieci `bag_number` diversi e uno assente danno sempre il prossimo
    del contatore; la data del 2025 il contatore del 2025; il contatore dietro dà l'errore in cima),
    `TestPaginaNuova`, `TestModuloNuovaCampi` (ordine con «Etichetta» e «📷 Scansiona», niente
    `bag_number`), `TestNuovaCreaBusta`; tolto `TestNuovaNumeroConZeri` (coperto da `TestNuovaNumero`
    per la pagina, e dai test del dominio della 037d per `NormalizzaNumero`). Ritoccati per il titolo
    e il messaggio: scheda, modifica, coda delle foto (una con l'etichetta «45»). Gli aiuti dei test
    (`crea`, `bustaScheda`, `bustaProva`) danno l'avviso delle gemelle già confermato: i test creano
    tante buste con lo stesso codice. `TestDatiDellEtichetta` mette il codice con gli spazi da SQL (la
    creazione ora toglie gli spazi a un'etichetta).
  - Dopo la review: `TestEtichettaNelModulo` (10 codici, e che il valore mostrato torni al codice),
    `TestModificaEtichettaMostrata` (il campo «0045/26», «0046/25», «RIP-2026-10000», un codice
    libero, uno con gli spazi; rimandato com'è il codice non cambia); in `TestCreaGemelleAperte` il
    codice libero «ABC » salvato con gli spazi (gemello di «ABC », «ABC» e «\u00a0ABC\t», non di
    «AB»); in `TestSchedaGemelle` la gemella con gli spazi trovata dalla scheda e non dalla scansione;
    in `TestModificaEtichettaScritta` la data salvata illeggibile («45» resta «45»); in
    `TestNuovaGemelleAperte` l'avviso delle gemelle dopo `qr_avvisato` (senza «Manca il codice QR»);
    in `TestSchedaGemelleWeb` la gemella con la data illeggibile (la data com'è, niente 500).
  - Banco: `TestScenarioSchedaSuiDueDati`, `TestScenarioNumeriSuiDueDati` (titolo «Busta N. …»,
    normalizzati), `TestScenarioUtentiSuiDueDati` e `TestProvaConDegliScenariDelRepo` con lo scenario
    nuovo.
- **Mutazioni a mano:** sotto, «Mutazioni».
- `scripts/check` verde, anche dopo la review (gofmt, vet, staticcheck, errcheck, build, test, copertura: 353 voci, 26
  chiuse, 0 rosse, 77 parziali, 239 aperte, 11 fuori v1; segreti e dati veri OK). Il primo giro aveva
  trovato quattro file da formattare: `gofmt -w`, solo spazi, dopo i giri del banco.

## Banco

Istanze nuove, `gestrip-standalone:fab-222`.

- Scenario nuovo `busta-etichetta` (18 passi, `in_fondo`, prova con i dati importati): le schede di
  22/26 (0045/26 aperta, gemella consegnata: solo lo storico), 21/26 (la consegnata: il link alla
  22/26), 24/26 (0046/26, gemella aperta 23/26), 25/26 (0045/25, nessuna gemella), 35/26 (senza QR) e
  36/26 (codice non RIP), ognuna confrontata con lo standalone normalizzato e poi, solo nel Go, con le
  sue attese; `GET /nuova` e tre POST solo Go con l'artigiano 1 e un `bag_number` forgiato: «45»
  (gemella aperta 22/26), «0046/26» (due aperte), «0045/25» (25/26). Sul database vuoto lo stesso
  POST si ferma a «Seleziona un artigiano valido.»: nessuno dei due dati crea una busta.
- `tools/banco/browser/banco.mjs`: riconosce il messaggio dopo il salvataggio in tutte e due le forme
  (`SALVATA`, `NUMERO_SALVATA`: «Riparazione 45/26 creata.» e «Busta N. 45/26 salvata»), e il numero
  e la striscia «Busta N. 51/26:» si anonimizzano come «B1» anche con lo spazio (prima `\S+`);
  `manuale.mjs` aspetta «salvata».
- **Primo giro, dati importati** (`s053a`): HTTP 46 scenari, 817 passi, 1078 attese, 2 inattese
  (`home[11]`, la scheda della 27/26 senza normalizzatore: sistemata), 0 errori; browser 4 inattese:
  due in `tentativi` (la striscia «Busta N. 51/26:» non anonimizzata: sistemata) e il **500 dello
  standalone** in `firma-riquadro[1]` (`/repairs/6/photos/29` e `/30`, «database is locked» noto di
  fab-222; il Go nessun errore), non accettato in `deviazioni.json`.
- **Giro completo, dati importati** (`s053b`, binario finale): HTTP **46 scenari, 817 passi, 1079
  differenze attese** (D-64 normalizzate e righe, D-01, D-56, D-69), **0 inattese**, 0 errori; browser
  8 scenari, 16 viste, 22 attese, **0 inattese**, 0 da decidere: **verde**. Il 500 dello standalone in
  `firma-riquadro` in questo giro non c'era.
- **Giro completo, database vuoto** (`s053c`): HTTP 46 scenari, 817 passi, 31 attese, **0 inattese**,
  0 errori (28 non provati con questi dati, come previsto); browser 8 scenari, **0 inattese**: verde.
- **Dopo la review** (codice finale):
  - giro mirato `s053d` (`modifica`, `busta-etichetta`, `nuova`, solo HTTP): 47 passi, 2 inattese,
    il campo della modifica misurato e dichiarato con due righe D-64;
  - giro completo, dati importati, `s053e`: HTTP 46 scenari, 817 passi, 1081 attese, 0 inattese; il
    browser rosso in `sfondo` (il Go fermo in «naviga»: `genera` dello script restituiva solo il
    numero della forma dello standalone) e, di conseguenza, in `tentativi` (le foto rimaste in coda).
    Corretto `banco.mjs` (`m[1] || m[2]`);
  - **giro completo, dati importati, `s053g`**: HTTP **46 scenari, 817 passi, 1081 attese, 0
    inattese**, 0 errori; browser 8 scenari, 16 viste, 26 attese, **2 inattese**: tutte e due il 500
    noto dello **standalone** in `firma-riquadro[1]` (`/repairs/6/photos/28` e `/29`, «database is
    locked» di fab-222; il Go nessun errore). Non accettate in `deviazioni.json`: il Go è verde;
  - **giro completo, database vuoto, `s053f`**: HTTP 46 scenari, 817 passi, 31 attese, **0
    inattese**, 0 errori (28 non provati); browser 8 scenari, **0 inattese**.
- `banco giu` dopo ogni giro e `banco controlla`: «AVANZI: nessuno».

## Mutazioni

`to-be-deleted-after-use/slice-053/mutazioni/muta.py`, 24 punti, test mirati di `internal/buste` e
`internal/web` in una copia del repo nello scratchpad (cancellata dopo): **24 uccise, 0
sopravvissute**. Anno di oggi invece di quello della data di accettazione; anno scritto ignorato
(«0045/25»); «45» che non diventa un'etichetta; codice intero letto non ripulito; altro formato
ripulito degli spazi; `bag_number` usato; avviso anche con le gemelle chiuse; conferma ignorata;
nessun avviso; modulo che non rimanda il codice avvisato; avviso con il codice scritto invece del
risolto; scheda con le gemelle chiuse fra le aperte; storico anche senza gemelle chiuse; storico per
un codice non RIP; gemelle dal cestino; la busta stessa fra le gemelle; anno riusato preso dal numero
interno; titolo col numero interno; numero interno in piccolo mancante; «⚠ senza QR» anche sulle
chiuse; messaggio col numero interno; modifica con l'anno sempre della data salvata; modifica che
riscrive un codice uguale al salvato; «Numero busta già esistente.» perso.

## Per il manuale

- In «Nuova riparazione» non c'è più «N° Busta»: il numero interno lo dà il programma.
- Il campo «Etichetta (0045/26)» si riempie con il lettore (📷 Scansiona porta il cursore nel campo,
  poi la pistola) o a mano: «45» vuol dire l'etichetta 0045 dell'anno della data di accettazione,
  «0045/25» quella del 2025.
- Se l'etichetta è già su una busta ancora aperta, la pagina lo dice con il nome del cliente; premendo
  di nuovo «Genera busta» si salva lo stesso. Se l'altra busta è già consegnata non dice niente.
- Dopo il salvataggio compare «Busta 0045/26 salvata».
- Nella scheda il titolo è «Busta #0045/26», con il numero interno piccolo accanto; una busta senza
  etichetta è «Busta N. 45/26», con «⚠ senza QR» se è ancora aperta.
- Sotto il titolo, «Un'altra busta ha la stessa etichetta» con il link, se l'altra è aperta; «storico
  di questa etichetta» apre tutte le buste che hanno avuto quell'etichetta.
- Anche in «Modifica» il campo è «Etichetta (0045/26)» e accetta «45».

Righe di `docs/manuale/da-scrivere.md` («Numero della busta ed etichette») che ora sono vere: la
seconda («N° Busta» tolto, «Etichetta (0045/26)» con il 📷, «45» con l'anno della data di
accettazione), la terza e la quarta («N. 45/26» con «⚠ senza QR», «0045/25»: lista dalla 050, scheda
da qui), la quinta (busta di carta riusata). La prima è vera per lista e scheda, non ancora per PDF,
etichette, messaggi e pagina di stato (058, T4.5–T4.7). Lasciate tutte: il capitolo è della 059.

## Review indipendente

Due difetti medi e otto bassi, tutti trattati in questa slice:

1. **Medio, il campo della modifica mostrava `RIP-2026-0045`** (lo studio, punto (d): «mostrata
   come 0045/26»). Ora `EtichettaNelModulo`: «0045/26» quando `EtichettaScritta` di quel valore, con
   l'anno della busta, ridà esattamente il codice salvato; altrimenti il codice com'è. Test
   `TestEtichettaNelModulo` e `TestModificaEtichettaMostrata`.
2. **Medio, il messaggio dopo il salvataggio.** Ora è «Busta 0045/26 salvata», controllato nello
   studio (senza punto finale, fra le virgolette: «Dopo il salvataggio: «Busta 0045/26 salvata», e il
   numero interno solo nel registro.»). `banco.mjs` e `manuale.mjs` lo riconoscono, la striscia delle
   foto non dipendeva dal testo; 4 righe D-64 del livello browser per `coda` e tolte le due attese.
   Tolto il dubbio di prima.
3. **Basso, due avvisi insieme.** Con `qr_avvisato` e un'etichetta già usata comparivano «⚠
   Etichetta già usata…» e «⚠ Manca il codice QR». Ora con l'avviso delle gemelle quello del QR
   mancante non si vede (e il pulsante dice «Genera busta»); il campo nascosto `qr_avvisato` resta.
   Test in `TestNuovaGemelleAperte`.
4. **Basso, codice libero con gli spazi.** Le gemelle si confrontano con il codice ripulito da tutte
   e due le parti; cosa si salva non cambia. La scansione della home fa diversamente (confronta il
   salvato com'è, come lo standalone) e l'ho lasciata così: scritto in `docs/SVILUPPO.md` e nel Dubbio
   5. Test in `TestCreaGemelleAperte` e `TestSchedaGemelle`.
5. **Basso, contatore indietro.** Codice invariato; rischio noto in `docs/SVILUPPO.md` e nel Dubbio 3,
   con il rimedio proposto.
6. **Basso, forme non riconosciute.** Codice invariato; domanda nel Dubbio 11.
7. **Basso, la modifica nel banco.** `modifica` passo 8 confronta «Etichetta (0045/26)», «Codice QR
   busta», `#repair-qr-code-scan` e «📷 Scansiona»; le prime due sono righe D-64 (giro mirato
   `s053d`). Corretta la frase della ricevuta: nello standalone l'etichetta era «Codice QR busta».
8. **Basso, il 📷 senza test.** Nota in «Cosa c'è»: affidato alla 055.
9. **Basso, il «#».** `docs/SVILUPPO.md` dice che va solo nei titoli e nella lista, solo con
   un'etichetta.
10. **Robustezza.** La modifica con una data salvata illeggibile salva il testo com'è (mai
    «RIP-1-0045»); la scheda con una gemella dalla data illeggibile mostra la data com'è invece di un
    500. Tutte e due con un test.
11. **Ricevuta.** `busta-etichetta` ha 18 passi; il Dubbio 8 (la pagina della firma) è una domanda;
    questa sezione; le domande nuove nei Dubbi 12 e 13.

## Dubbi

1. **Il titolo di «Nuova riparazione»** non mostra più nessun numero (prima «busta #41/26», il
   prossimo interno): il numero visibile si conosce solo dopo aver letto l'etichetta.
2. **«Busta N. 45/26 salvata»**: senza etichetta il messaggio dice il numero interno nella forma «N.»,
   la regola di `NumeroVisibile`.
3. **Rischio noto, contatore indietro.** «Numero busta già esistente.» ora può venire solo da un
   `sequenze_buste` rimasto dietro a un numero già usato (un dato vecchio). La creazione annulla anche
   l'avanzamento, quindi ogni «Genera busta» di quell'anno si blocca, senza rimedio dall'interfaccia
   (prima si poteva scrivere un altro numero). Rimedio proposto, non fatto: con quell'errore portare il
   contatore al massimo dei numeri già usati dell'anno e riprovare una volta. Da decidere con Stefano.
4. **Le gemelle sono per ogni codice**, non solo per le etichette RIP: un codice di altro formato già
   su una busta aperta avvisa allo stesso modo. Lo storico invece c'è solo per le etichette (il link
   cerca il numero visibile «0045/26»; un codice libero non va in un indirizzo, regola della 052).
5. **Spazi ai bordi del codice.** Le gemelle (creazione e scheda) confrontano il codice senza spazi
   ai bordi da tutte e due le parti; la scansione della home confronta il salvato com'è, come lo
   standalone: un « RIP-2026-0045 » importato con gli spazi è gemello per la scheda ma non si trova
   con la pistola. La creazione toglie gli spazi solo a un'etichetta. **Domanda per Stefano:**
   togliere gli spazi ai bordi a ogni codice salvato (alla creazione, alla modifica e una volta sui
   dati importati)?
6. **L'avviso arriva dopo tutti gli altri errori**, anche dopo il numero già usato (lo standalone non
   ha avvisi qui): così la seconda pressione salva davvero. «⚠ Manca il codice QR» resta prima di
   tutto, come lo standalone.
7. **La modifica non avvisa delle gemelle**: cambiare l'etichetta di una busta in una già usata da una
   aperta passa senza avviso (la scheda poi mostra la riga). Se serve, è un pezzo della modifica da
   fare a parte.
8. **La pagina della firma** (`/repairs/{id}/firma`: «✍️ Consegna busta #15/26» in `firma.html:8` e
   il titolo «Firma busta #15/26» in `firma.go:47`) mostra ancora il numero interno, e la vede il
   cliente. La 058 copre i PDF, non questa pagina. **Domanda:** allargare la 058 alla pagina della
   firma? È lo stesso lavoro della scheda (titolo e intestazione con `NumeroVisibile`, il banco
   browser di `firma-riquadro` da aggiornare).
9. **Il 📷 senza fotocamera** porta solo il cursore nel campo (con la pistola basta); senza
   JavaScript non fa niente. Il lettore vero è della 055.
10. **`Accettazione.Numero`** resta nel dominio (e i suoi test della 037d): la pagina non lo usa più.
    Toglierlo vorrebbe dire togliere anche il ramo del numero scritto da `risolviNumero`.
11. **Forme non riconosciute** come etichetta, che restano scritte com'erano: `rip-2026-0045` in
    minuscolo, «45/2026», «0045-26», «RIP-2026-45». **Domanda:** riconoscerne qualcuna?
12. **Lo storico** («storico di questa etichetta», cioè `GET /cerca?cerca=0045/26`) mostra anche una
    busta **senza** etichetta con il numero interno 45/26 (la regola di ricerca della 052).
    **Domanda:** va bene così?
13. **Il 📷 prima della 055.** In «Nuova riparazione», nella modifica e nella home il 📷 c'è già ma
    non apre la fotocamera. **Domanda:** nasconderlo fino alla 055?
