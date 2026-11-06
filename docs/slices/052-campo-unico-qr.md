# 052 — Campo unico, ricerca per etichetta, scansione QR e gemelle (T4.2, terza)

- **Fonte:** riga 052 del piano della T4.2 (`050-home-lista.md`); le decisioni D-63 e D-64
  (Stefano, 03/11/2026, righe dell'inventario e `docs/studio-D64-numero-etichetta.md`), le risposte
  del 05/11/2026 alle domande 1-7 della T4.2, D-68 e D-69 (approvate il 05/11/2026), D-70 da sapere.
  Lo standalone a `go-baseline`: `WorkshopLive` (`handle_event("qr_lookup")`, `qr_select`,
  `qr_select/2`, il riquadro `#qr-lookup` con `#qr-lookup-form`, `#qr-lookup-scan`,
  `#qr-lookup-error`, `#qr-matches`, `#qr-action`, `#qr-btn-consulta`, `#qr-matches-count`,
  `#qr-match-list`, `qr-match-<id>`; `#repair-search-form`), `Workshop.lookup_repairs_by_qr/2` e
  `qr_query/1`, `RepairIndex` (`search/2`), la migrazione `20260909090000_add_repair_qr_code` (senza
  indice); il test «qr lookup opens the exact repair for staff and mirrors the source errors» di
  `workshop_live_test.exs`.
- **Voci:** RIP-H-03 (dopo il D-63: il campo unico e la ricerca per etichetta), RIP-H-22 (dopo D-63
  e D-64: la scansione, le gemelle, l'unica aperta), RIP-H-06 (la ricerca per etichetta toglie gli
  altri filtri), RIP-H-07 (pillola D-69), RIP-H-21 (D-68 nello smistamento).
- **Deviazioni:** D-63, D-64, D-68, D-69 (sotto). D-70 non ancora usata: le rotte la permettono.
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Cosa c'è

- **Dominio** (`internal/buste/ricerca.go`, nuovo; `indice.go`):
  - `LeggiRicercaEtichetta`: un testo cercato di 1-4 cifre ASCII, con o senza «/AA», da 1 in su, è
    un numero d'etichetta («0045/26», «045/26», «45», «0012»); «0», «00045», «45/2026», «#45» no
    (risposta 3).
  - `RicercaEtichetta.Trova`: la busta **mostra** quel numero, con la regola di `NumeroVisibile`:
    l'etichetta `RIP-AAAA-NNNN` col suo anno, o il numero interno (senza gli zeri davanti) **solo**
    per le buste senza etichetta. Senza «/AA» tutti gli anni (risposta 2); «0012» trova la 12/26
    senza etichetta e la 0012/25.
  - `Filtri.Effettivi`: con un numero d'etichetta restano solo la ricerca e la pagina (risposta 4,
    anche il nascondimento di pronte e consegnate); `Lista` e `Selezione` li usano, così pillole,
    campi nascosti, cassetto, paginazione e «⧉ Schede PDF» dicono solo la ricerca. Le buste le sceglie
    `busteDellEtichetta` (candidate in SQL, regola esatta in Go), la lista resta nell'ordine di
    sempre (risposta 5). Ogni altro testo resta il «contiene» della 051.
  - `CercaPerQR` è `lookup_repairs_by_qr/2`: codice senza spazi ai bordi (`CodiceLetto`, come
    `String.trim/1`), uguale esatto col salvato, fuori dal cestino, dalla più recente; la riga
    `repair.qr_lookup` (`repair_index`, `qr`, `matches` e `repair_ids`, mai il codice) se ha trovato
    qualcosa o se è una scansione col 📷; vuoto `ErrQRVuoto` senza riga; se la riga non si scrive,
    errore e nessuna busta.
  - `UnicaDaAprire`: l'unica busta, o l'unica aperta fra le gemelle (D-64, cambia RIP-H-22).
- **Migrazione 0016** `buste_codice_qr` (indice non unico su `codice_qr`), in `IndiciSoloGo`: lo
  standalone non ha l'indice.
- **Pagine** (`internal/web/cerca.go`, nuovo; `pagine/cercaqr.html`, nuova; `home.html`, `home.go`,
  `app.css`):
  - il campo unico `#repair-search-form` al posto della ricerca della 051: `POST /cerca-qr` con gli
    altri filtri come campi nascosti, il `_csrf_token` fuori dal modulo legato con `form=` (così i
    campi del modulo sono quelli dello standalone, review punto 4), «🔍» (`#repair-search-submit`, il primo
    pulsante: quello dell'Invio e del lettore a pistola), il campo `cerca`
    (`#repair-search-code`, «Cerca nome, n° busta o QR…») e «📷» (`#qr-lookup-scan`, `via=qr`,
    l'id del pulsante dello standalone), tutti e due dentro il campo; il suggerimento «Cerca in: …»
    resta. Quando il campo prende il fuoco `app.js` ne seleziona il testo (`data-seleziona-tutto`),
    così la pistola lo sostituisce (review punto 2). Non c'è su `/nuova`, niente «📷 Scan»
    nell'intestazione (risposta 6); il cassetto dei filtri fa GET a `/cerca` (review punto 3);
  - `POST /cerca-qr` (personale, con il guscio): un valore uguale esatto al codice di una busta è una
    scansione (risposta 1): il pannello `#qr-action` («#0045/26 — Cliente», «Stato: …», 🪙, e fino
    alla 054 solo `#qr-btn-consulta` «👁 Consultazione»), o la scelta `#qr-matches-count` «N
    riparazioni con questo QR — scegli:» con i `qr-match-<id>` dello standalone (data, stato, 🪙, 80
    caratteri di descrizione, consegnate e annullate attenuate); ogni gemella è un modulo POST con il
    codice nel corpo e `scelta=<id>` (`qr_select`; un id che non è fra le trovate dà «Riparazione non
    trovata.»). Con `via=qr` un codice sconosciuto dà «⚠️ Nessuna riparazione trovata per questo QR»,
    vuoto «⚠️ QR non fornito»; il guasto del registro «⚠️ Ricerca non riuscita.» (500). La pagina è
    `no-store`, ha di nuovo il campo unico con i filtri e «Chiudi» porta alla lista con i filtri.
    Ogni altro testo è una ricerca scritta;
  - `GET /cerca` (personale): la ricerca scritta da un indirizzo, mai una scansione;
  - lo smistamento (`smista`, per tutti e due): una busta trovata → la scheda; più buste o nessuna →
    dal POST la lista **disegnata nella risposta** (`no-store`, nessun redirect, con la sua
    `repair.index_viewed`: review punto 1), da `GET /cerca` un redirect a `/?cerca=…`; con i filtri,
    senza per un numero d'etichetta; ricerca vuota → la lista coi filtri. Nei link della lista
    (paginazione, «⧉ Schede PDF») e nel campo del cassetto un `cerca` con la forma esatta di un codice
    d'etichetta non c'è (`parametriLink`); per un testo libero tutto come nella 051.
    Lo smistamento non scrive niente (D-68): la scheda scrive la sua `repair.viewed`, la lista la sua
    `repair.index_viewed`;
  - la pillola «⏰ In scadenza (N giorni)» con la soglia della lista (D-69), «(1 giorno)» al singolare.
- **Banco:**
  - `home-cerca` (20 passi, gira prima degli altri scenari della home): i campi del campo unico (con e
    senza filtri), la ricerca per etichetta nella lista («0045» con stato e compro oro, «45»,
    «0045/26» con la scadenza, «045/25», «0012», «0046» con lo stato consegnata, «27» con i messaggi,
    «0048»), `GET /cerca` (una busta, più buste, etichetta con i filtri tolti, testo con i filtri, un
    codice che resta un «contiene», vuoto). Tutti `solo_go` D-63 o D-64.
  - `cerca-qr` (26 passi, prima di tutti gli scenari della home): l'etichetta riusata 0045/26 (dritta
    alla 22/26 aperta), le gemelle 0046/26 (24 e 23), la scelta della 23 e di una busta non fra le
    trovate, un codice da solo chiuso (37/26), il 📷 con un codice sconosciuto e vuoto, lo stesso
    codice dal 🔍 con lo stato (la lista vuota nella risposta, `no-store`, il codice in nessun link),
    una ricerca scritta dal modulo (0045/25 → scheda della 25), una con più buste (la lista nella
    risposta, il testo nel link del report), una ricerca vuota con i filtri, il 403 senza token, il registro `repair.qr_lookup` senza nessun «RIP-20…».
    Sul database vuoto le stesse richieste hanno le loro attese (nessuna busta: le liste disegnate
    nella risposta e i messaggi).
  - `home-filtri`: di `#repair-search-form` confronta ancora i campi `input@N` (tipo, nome, valore:
    i nascosti e `cerca`, uguali allo standalone perché il token è fuori dal modulo), non il metodo,
    l'azione e il testo (D-63, li fissa `home-cerca`), e non l'azione del cassetto; il passo 52 cerca il telefono della 27/26 invece di «27», che ora è un numero
    d'etichetta (lo prova `home-cerca`); i passi 34 e 53 non hanno più la pillola fra le attese,
    coperta da due righe D-69 in `bench/deviazioni.json`.
  - `TestOrdineScenariHome`, `provacon_test.go` e `dati_test.go` sanno dei due scenari nuovi;
    `docs/SVILUPPO.md` ha l'ordine nuovo, la regola del campo unico nel banco e quella della ricerca
    per etichetta.

## Deviazioni

- **D-63** (approvata): campo unico e due rotte che lo standalone non ha; passi `solo_go`, e i campi
  del modulo dello standalone non si confrontano più in `home-filtri`.
- **D-64** (approvata): ricerca per numero d'etichetta che ignora i filtri, scansione dritta all'unica
  gemella aperta; passi `solo_go`.
- **D-68** (approvata): lo smistamento non scrive righe; nessuno scenario confronta il totale delle
  `repair.index_viewed` (lo dice il commento di `tools/banco/stati.go`), quindi nessuna riga in
  `bench/deviazioni.json`.
- **D-69** (approvata): due righe in `bench/deviazioni.json` (`home-filtri` passi 34 e 53, «7 giorni»
  nello standalone, «12 giorni» nel Go).

## Prove

- **Test Go** (database vero, orologio al 31/10/2026):
  - `internal/buste/ricerca_test.go`: `TestLeggiRicercaEtichetta` (29 testi), `TestRicercaEtichettaTrova`
    (20 casi), `TestListaRicercaEtichetta` (12 ricerche più il caso con tutti i filtri, ognuna con
    lista, totale, filtri effettivi e selezione; anche un numero interno «045/23» e un codice salvato
    con lo spazio in fondo; i «contiene» che restano), `TestFiltriEffettivi`, `TestCercaPerQR`
    (spazi, cestino, ordine, righe con e senza scansione, mai il codice, guasto del registro),
    `TestUnicaDaAprire` (8 casi).
  - `internal/web/cerca_test.go`: `TestCercaQRScansione` (titolare e dipendente: pannello, unica
    aperta, gemelle con l'ordine e i filtri, chiuse attenuate, scelta, scelte non valide, nessuna
    `repair.viewed`, `no-store`, il token in ogni modulo delle gemelle, «Chiudi» con i filtri),
    `TestCercaQRMessaggi` (📷 sconosciuto e vuoto, pistola con la lista vuota nella risposta e il
    codice in nessun link, vuoto con i filtri, 403, guasto del registro senza il codice nelle righe
    del server), `TestCercaQRCodiceMaiFuori` (righe dell'audit, righe del server, `Location`),
    `TestCercaSmista` (D-68 con le righe contate, la lista disegnata dal POST, 21 indirizzi di
    `GET /cerca`, quattro dal cassetto), `TestHomeRicercaEtichetta`, `TestRigaDellaScansione`.
  - Ritoccati: `TestHomePillole` (D-69, anche con le soglie riparate e il singolare),
    `TestHomeRicercaECassetto` (il campo unico col token fuori dal modulo, assente su `/nuova`; il
    cassetto a `/cerca`), `TestRotteConGuscio`, la tabella dei permessi, il conteggio
    delle migrazioni dell'import.
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-052/mutazioni/muta.py`; prima della review 28
  punti, con la review 11 in più, sotto; 28 punti, test mirati
  di `internal/buste` e `internal/web`, in una copia del repo fuori dal modulo): **27 uccise, 1
  sopravvissuta equivalente**. Uccise: forma dell'etichetta (5 cifre, anno di 4 cifre, lo 0); anno
  ignorato; senza anno solo l'anno di oggi; numero interno anche per le buste con l'etichetta; filtri
  tenuti con l'etichetta; solo lo stato tolto; etichetta come «contiene»; codice nel registro;
  codice nell'indirizzo (redirect col codice); codice nelle righe del server sul guasto (uccisa dopo
  aver aggiunto il controllo a `TestCercaQRMessaggi`); modulo in GET; più buste alla scheda; una
  busta alla lista; niente unica aperta fra le gemelle; l'unica aperta anche con più aperte; `via=qr`
  ignorato; ogni codice sconosciuto come scansione; riga `repair.qr_lookup` anche per la ricerca
  scritta; lo smistamento che scrive la lista (D-68); pillola a 7 (D-69); codice senza togliere gli
  spazi Unicode; cestino nella scansione; gemelle dalla più vecchia; scelta fuori dalle trovate;
  ricerca vuota che perde i filtri. Sopravvissuta: il cestino tolto dalle **candidate** della ricerca
  per etichetta, equivalente (la query della lista ha sempre `cancellata_il IS NULL`).

- `scripts/check` verde, prima e dopo la review (gofmt, vet, staticcheck, errcheck, build, test,
  copertura: 353 voci, 26 chiuse, 0 rosse, 77 parziali, 239 aperte, 11 fuori v1; segreti e dati veri
  OK).

## Banco

Istanze nuove `s052a`…`s052g`, `gestrip-standalone:fab-222`.

- Giro mirato (i due scenari nuovi, `home-filtri`, `home-contatori`, `report-filtri`, dati importati,
  senza browser): 139 passi, 5 inattese, tutte attese giuste da correggere (un attributo assente si
  scrive «(senza attributo)»; la pillola D-69 dei passi 34 e 53 di `home-filtri` era fra le attese).
  I passi di «0045», «012/26» e del report «0045» e «012/26» danno le stesse buste nelle due app:
  restano confrontati.
- **Giro completo, dati importati** (`s052b`): HTTP **45 scenari, 797 passi, 1045 differenze attese**
  (D-64 normalizzate, D-01, D-56, D-69), **0 inattese**, 0 errori; browser 8 scenari, 16 viste, 22
  attese, **0 inattese**, 0 da decidere: **verde**.
- **Giro completo, database vuoto** (`s052c`, col binario finale): HTTP 45 scenari, 797 passi, 17
  attese, **0 inattese**, 0 errori (27 non provati con questi dati, come previsto); browser 8 scenari,
  **0 inattese**: verde.
- **Secondo giro completo, dati importati** (`s052d`, col binario finale): HTTP uguale al primo (45,
  797, 1045, **0 inattese**); browser **2 inattese**, il solito 500 dello **standalone** su due foto
  della 6/26 in `firma-riquadro[1]` (`/repairs/6/photos/27` e `/29`; il Go nessun errore): il guasto
  noto di fab-222 della 040a, 050 e 051, che questa slice non tocca (nel giro `s052b` non c'era).
- **Dopo la review** (codice finale): giro mirato `s052e` (i due scenari nuovi, `home-filtri` con i
  campi `input@N` di nuovo confrontati, `home-contatori`, `home-dipendente`): 128 passi, 1 inattesa,
  un'attesa da misurare, fissata. **Giro completo, dati importati** (`s052f`): HTTP **45 scenari, 799
  passi, 1045 attese, 0 inattese**, 0 errori; browser 8 scenari, 16 viste, 22 attese, **0
  inattese**: verde. **Giro completo, database vuoto** (`s052g`): HTTP 45 scenari, 799 passi, 17
  attese, **0 inattese** (27 non provati); browser 8 scenari, **0 inattese**: verde.
- `banco giu` dopo ogni giro e `banco controlla`: «AVANZI: nessuno».

## Review indipendente

Nessun difetto alto; quattro medi e sei bassi, tutti chiusi in questa slice (le scelte dei punti 1,
2 e 3 sono della sessione principale, da confermare con Stefano: vedi i Dubbi).

1. **Medio, il codice della pistola nell'indirizzo.** Un valore che non era il codice di nessuna busta
   faceva redirect a `/?cerca=RIP-2026-0999`. Ora dal POST, quando non è una scansione e non si apre
   una sola scheda, la lista si disegna nella risposta (`no-store`, nessun redirect, una riga
   `repair.index_viewed` con le chiavi del modulo); paginazione, «⧉ Schede PDF» e campo del cassetto
   non portano un `cerca` con la forma esatta di un'etichetta (`parametriLink`, `buste.EtichettaDi`);
   un testo libero li porta come nella 051. Il codice resta solo nella pillola e nel campo (corpo,
   `no-store`). `TestCercaQRMessaggi`, `TestCercaSmista` e i passi di `cerca-qr` aggiornati.
2. **Medio, la pistola accodava al testo del campo.** `app.js` seleziona il testo del campo
   (`data-seleziona-tutto`) quando prende il fuoco, così la pistola lo sostituisce; senza JavaScript
   il campo è come prima. Non c'è un test del comportamento nel browser (il banco non lo prova).
3. **Medio, il «Cerca» del cassetto non smistava.** `#repair-filter-form` fa GET a `/cerca`: con un
   risultato la scheda, con la ricerca vuota («Applica» senza testo) la lista con i filtri, «Reset» a
   `/` come prima. Quattro indirizzi del cassetto in `TestCercaSmista`; in `home-filtri` l'azione del
   cassetto non si confronta più, la fissa `home-cerca`.
4. **Medio, il banco non confrontava più i nascosti.** Il token del campo unico sta fuori dal modulo,
   legato con `form="repair-search-form"`: i campi del modulo (`input@N`) sono quelli dello standalone
   e `home-filtri` li confronta di nuovo su tutti i passi (il giro mirato: tutti uguali).
5. **Basso:** «Chiudi» della pagina della scansione porta alla lista con i filtri del modulo.
6. **Basso:** `TestCercaQRScansione` controlla il token in ogni modulo delle gemelle.
7. **Basso:** in `TestListaRicercaEtichetta` una busta con il numero interno «045/23» e una con il
   codice salvato «RIP-2026-0045 »: le due mutazioni delle candidate ora muoiono.
8. **Basso:** la pillola dice «(1 giorno)» al singolare, con il test; nessuna riga D-69 in più (nel
   banco la soglia è 12).
9. **Basso:** `docs/SVILUPPO.md` dice che i passi di quella forma che coincidono (`home-filtri` 9 e
   12, `report-filtri` 7 e 9) restano confrontati finché i risultati coincidono.
10. **Basso:** in questa ricevuta le risposte 1-7, gli indirizzi di `TestCercaSmista` e le ricerche di
    `TestListaRicercaEtichetta` contati giusti.

**Mutazioni della review** (11): candidate senza il «%» davanti al numero interno e senza il «%» in
fondo al codice; link col codice; cassetto col codice; POST con il redirect alla lista; lista del POST
senza `no-store`; cassetto a `/`; «Chiudi» senza filtri; singolare mai; gemelle senza token; token
dentro il modulo: **11 uccise**. Rifatte poi tutte e 39 sul codice finale: **38 uccise, 1 sopravvissuta
equivalente** (il cestino nelle candidate, sopra).

## Per il manuale

- In alto nella lista c'è un solo campo, «Cerca nome, n° busta o QR…», con 🔍 a sinistra e 📷 a
  destra; i filtri già scelti restano.
- Con il lettore a pistola (o scrivendo il codice intero) si apre il riquadro della busta con quel
  codice; se l'etichetta è su più buste si va dritti all'unica aperta, altrimenti si sceglie dall'elenco.
- Per ora il riquadro ha solo «👁 Consultazione»; i pulsanti per avanzare lo stato arrivano dopo.
- Scrivendo «0045/26», «0045» o «45» escono tutte le buste con quell'etichetta, di tutti gli anni se
  non si scrive l'anno, anche chiuse e consegnate, qualunque filtro ci sia.
- Le altre ricerche (nome, telefono, descrizione, note) tengono i filtri; se trovano una sola busta si
  apre la sua scheda, altrimenti la lista.
- Con il lettore a pistola il testo già nel campo si sostituisce: basta toccare il campo e scansionare.
- Il «Cerca» del cassetto dei filtri fa lo stesso: con una sola busta trovata si apre la sua scheda.
- La pillola «⏰ In scadenza» dice i giorni impostati, non sempre 7.

## Dubbi

1. **Il pannello fino alla 054.** Con una busta (o l'unica aperta fra le gemelle) si vede il pannello
   `#qr-action` con il numero, il cliente, lo stato e solo «👁 Consultazione»: è la variante dello
   standalone per una busta che non si può avanzare. Per le buste chiuse lo standalone aggiunge
   «Stato X — non avanzabile.»: arriva con le azioni nella 054.
2. **`GET /cerca` non fa mai la scansione.** Il modulo della home è un POST, così il codice letto non
   va nell'indirizzo; `GET /cerca` (per un link o un indirizzo scritto, per esempio il futuro «storico
   di questa etichetta» della 053) è solo la ricerca scritta: un codice intero lì è un «contiene».
3. **La riga `repair.qr_lookup`** si scrive per ogni scansione (un codice trovato, o qualunque codice
   col 📷), anche per la scelta di una gemella, che nel Go è una nuova richiesta e rilegge le buste per
   codice (lo standalone sceglie dal risultato in memoria e non scrive). Un testo scritto che non è il
   codice di nessuna busta non scrive la riga: è una ricerca della lista.
4. **302 e non 303.** Lo smistamento usa `vaiA`, il redirect di tutto il Go (302 con il corpo di
   Plug); il browser fa comunque una GET dopo il POST. Il piano diceva 303.
5. **Il 📷 senza la fotocamera.** Fino alla 055 il 📷 manda solo il modulo con `via=qr`: col campo
   vuoto dice «QR non fornito».
6. **Il cassetto tiene il suo campo «Cerca»**, come lo standalone, ma fa GET a `/cerca` (review punto
   3): con una sola busta la scheda. Essendo un GET, quello che si scrive lì va nell'indirizzo; un
   codice d'etichetta già cercato non ci viene rimesso.
7. **«#45» e «N. 45/26»** (come si leggono in lista) non sono riconosciuti come numero d'etichetta:
   ho tenuto la forma stretta della risposta 3 (1-4 cifre, con o senza «/AA»).
8. **Le gemelle attenuate** sono le consegnate e le annullate, come lo standalone; una «chiusa» (compro
   oro) no.
9. **La ricerca per etichetta fa sparire i filtri** dalla pagina (pillole, cassetto, campi nascosti):
   la lista mostra solo quello che applica davvero. Dal campo unico il redirect li toglie già.
10. **Scelte della sessione principale, da confermare con Stefano** (review punti 1, 2 e 3): la lista
    disegnata nella risposta al POST senza redirect (e un codice d'etichetta mai nei link); il testo
    selezionato al fuoco del campo (resta da decidere se il campo debba essere vuoto dopo la ricerca);
    il cassetto che smista con `GET /cerca`. Con la lista nella risposta al POST, ricaricare la pagina
    chiede di rimandare il modulo, e la pagina 2 di una ricerca per codice perde il codice.

Domande aperte dalla review, non applicate:

- (a) il 📷 si vede già prima della 055, quando fa solo l'invio con `via=qr`;
- (b) «N. 45/26», come si legge in lista, non si trova scrivendo «45/26» se la busta ha l'etichetta
  (D-64: il numero interno solo per le buste senza etichetta), e «N. 45/26» non è un numero cercato;
- (c) un numero da 1 a 4 cifre («2026», «3331») non cerca più nel telefono né nelle note, ma il
  suggerimento dice ancora «Cerca in: … telefono»;
- (d) un codice QR fatto di sole cifre («45») uguale al valore scritto è una scansione e vince sulla
  lista D-64;
- (e) la riga `repair.qr_lookup` alla scelta di una gemella va dichiarata come parte della D-63;
- (f) il piano dice 303, il Go fa 302 (Dubbio 4).
