# 077 — Consegne all'artigiano: lista, sessione, aggiunta, togli e conferma (T4.8, seconda)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/handover_live.ex` (mount con la
  pulizia, evento `open`, `render/1`), `handover_session_live.ex` (mount, eventi `add`, `remove`, `confirm`, `add_code/3`,
  `refusal/2`, `render/1`), `lib/gestrip_standalone/workshop.ex` (`purge_stale_empty_handovers/1`,
  `handover_in_collection/2`, `open_handover/2`, `get_handover/1`, `list_handover_bags/1`, `add_bag_to_handover/2`,
  `remove_bag_from_handover/2`, `confirm_handover/2`, `list_artisans_with_open_handovers/0`,
  `list_handovers_in_collection/1`, `handover_error_message/1`, `find_bag/3`, `addable/2`, `reopen_cycle/3`,
  `attach_bag/3`, `linked_handover_ids/0`, `confirmed_handover_ids/0`, `start_of_local_day/1`), `repo.ex`
  (`prepare_query/3`: il cestino), `workshop/repair.ex` (`out_of_shop?/1`, `normalize_bag_number/1`), `jobs/clock.ex`.
- **Voci:** ART-05, ART-06, ART-07 (senza il contatore dei solleciti, della 083), ART-08, ART-10, ART-11 (portate; si
  chiudono con l'esito verde del banco). In più la **D-85 a 6 giorni** (Stefano, 14/11/2026, Dubbio 6 della 075).
  Nessuna migrazione: `consegne_artigiani` e le colonne delle buste ci sono dalla 0007.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco (Docker) non è stato lanciato:
  lo fa la sessione principale (sotto, «Banco»).

## Cosa c'è

- **Dominio** (`internal/buste/consegne.go`; «oggi» con `clock.Data.Inizio`, la mezzanotte del negozio come istante):
  - `PulisciConsegneVuote` = `purge_stale_empty_handovers/1`: via le consegne ancora aperte create prima della
    mezzanotte di oggi senza nessuna busta, **anche nel cestino** (`linked_handover_ids/0` non ha il filtro del cestino);
    nessuna riga d'audit.
  - `ApriConsegna` = `open_handover/2`: «Artigiano non trovato.», poi la consegna di oggi dell'artigiano in raccolta
    (aperta, senza buste uscite, **anche nel cestino**, la più recente) o una nuova (`creato_il` = `aggiornato_il` =
    adesso); nessuna riga d'audit.
  - `ArtigianiDelMenu` = `list_artisans_with_open_handovers/0`: per nome e id, con le consegne non chiuse di qualunque
    giorno. `ConsegneInRaccolta` = `list_handovers_in_collection/1`: oggi, aperte, senza buste uscite, dalla più
    recente, con le buste fuori dal cestino.
  - `ConsegnaArtigiano` = `get_handover/1` («Consegna non trovata.»), `BusteDellaConsegna` = `list_handover_bags/1`
    (fuori dal cestino, dalla più recente).
  - `AggiungiAllaConsegna(…, LetturaScritta|LetturaScansione)` = `add_bag_to_handover/2` con le due letture di
    `add_code/3`, in una transazione: consegna chiusa o che non c'è, codice vuoto, non trovata, compro oro, consegnata o
    annullata, già fuori bottega in un'altra consegna (anche senza consegna), in raccolta in un'altra consegna, corsa
    persa; i testi della pagina (`*RifiutoAggiunta`: «Riparazione non trovata: nessuna busta con numero o QR «X»»,
    «Numero busta o QR richiesto», gli altri con «(«X»)»). Una busta rientrata riparte pulita (`reopen_cycle/3`: uscita,
    rientro, nota dell'artigiano e consegna vecchia a NULL); poi consegna e artigiano della sessione. «Già in lista» se
    la busta era già della consegna. Nessuna riga d'audit.
  - **Le due letture** (D-64): il numero scritto è quello dell'etichetta (`LeggiRicercaEtichetta`: «0045/26»,
    «045/26», «0045», «45»; `RicercaEtichetta.Trova`: le etichette RIP-AAAA-NNNN con quel numero, di quell'anno o di tutti
    senza «/AA», e il numero interno solo per le buste senza etichetta); un testo che non ha quella forma (per esempio
    «10000/25») vale come numero interno senza zeri davanti (`NormalizzaNumero`) delle sole buste senza etichetta. Il QR
    è il codice normalizzato (`righeConCodice`, D-64 della 055d): fra più buste vince la più recente (deciso il
    29/10/2026 per il QR non unico). **Fra più buste con il numero scritto** (dopo la review, Dubbio 1): contano solo
    quelle che si possono dare all'artigiano (non compro oro, né consegnate né annullate; il cestino è già fuori); con
    una sola, quella; con più e l'anno non scritto («45»), quella dell'anno in corso in negozio, come «45» in «Nuova
    riparazione»; se ne restano più d'una, il rifiuto solo del Go «Più buste corrispondono a «X»: scrivi il numero con
    l'anno o usa il QR.» (`ErrPiuBuste`), senza scrivere niente e senza passare al QR; se nessuna si può dare, la più
    recente, con il suo rifiuto («Stato 'consegnata' non adatto…»). Il campo prova prima il numero, la scansione (079)
    prima il QR; si passa alla seconda lettura solo con «non trovata».
  - `TogliDallaConsegna` = `remove_bag_from_handover/2`: solo una busta della consegna non uscita e fuori dal cestino;
    l'artigiano resta; ogni altro caso non cambia niente e non è un errore.
  - `ConfermaConsegna` = `confirm_handover/2`: timbra le buste non uscite (fuori dal cestino), non chiude la consegna,
    `handover.confirmed` con `handed_over` e `repair_ids` (gli id timbrati, in ordine; `[]` se nessuno) nella stessa
    transazione; «Consegna non disponibile» per una chiusa o che non c'è.
- **Pagine** (`internal/web/consegne.go`, `pagine/consegne.html`, CSS in `app.css`, il fuoco in `app.js`):
  - `GET /handovers` (titolo «Consegna ad artigiano»): pulizia, poi «📤 Consegna ad artigiano», la frase, «Sessioni
    aperte oggi» (`#handovers-open-today`, solo se ce ne sono: «<artigiano> — 1 busta / N buste» e l'ora del negozio,
    link alla sessione), «Nuova consegna» con il menu (`handover[artisan_id]`, «— scegli —», «(sessione aperta)»), la
    frase della ripresa, «Apri sessione», «Nessun artigiano in anagrafica. Aggiungine uno». I link «📋 Storico consegne»
    e «⏰ Solleciti pendenti» compaiono quando le loro rotte ci sono (082, 083), come le voci del menu.
  - `POST /handovers` (evento `open`): «Seleziona un artigiano.», «Artigiano non valido.» (`Id.positive/1`), «Artigiano
    non trovato.», «Impossibile aprire la sessione.» (guasto), con la lista ridisegnata e la scelta tenuta (senza la
    pulizia, che è solo del mount); riuscita → 302 a `/handovers/{id}`. Un modulo senza la mappa `handover` è «nessuna
    scelta».
  - `GET /handovers/{id}` (titolo «Consegna a <artigiano>»): id non valido o consegna che non c'è → 302 a `/handovers`
    con «Consegna non trovata.». «← Indietro» (`#handover-back`), «📤 Consegna a …», «Sessione #N aperta il gg/mm/aaaa
    hh:mm» e «· Confermata» quando una busta è uscita; il campo «Numero busta» (`bag[numero_busta]`, «es. 0045/26»,
    D-64) con «Aggiungi»; «Buste in consegna» con il conto, una riga per busta (numero visibile D-64 con «#» solo davanti
    a un'etichetta, descrizione troncata come `truncate/1`: fino a 65 grafemi, altrimenti 59 e «…»), ✕ con «Rimuovere la
    busta dalla consegna?»; «Nessuna busta. Aggiungi il numero busta.»; «✓ Conferma senza stampare» con «Confermare la
    consegna di N busta/e senza stampare etichette?». Confermata: niente campo, niente ✕, niente conferma.
  - `POST /handovers/{id}/aggiungi` (evento `add`): riuscita → 302 alla pagina con «✓ Aggiunta busta #0045/26» (o
    «N. 45/26» senza etichetta) o «↻ Busta #0045/26 già in lista», il campo vuoto; rifiuto → la pagina ridisegnata (200)
    con il messaggio e il codice scritto, senza spazi ai bordi, nel campo.
  - `POST /handovers/{id}/buste/{busta}/togli` (evento `remove`): sempre «Busta rimossa dalla consegna.»; un id di busta
    non valido torna alla pagina senza messaggio.
  - `POST /handovers/{id}/conferma` (evento `confirm`): senza buste «Aggiungi almeno una busta.»; riuscita → 302 a
    `/?fuori_bottega=1` con «Consegna confermata: 1 busta / N buste fuori bottega.»; un rifiuto del dominio torna alla
    pagina con il suo testo.
  - Tutte del personale (righe nuove nella tabella dei permessi), moduli con `data-invio-unico`, CSRF e sessione
    rilette a ogni richiesta; la voce «📤 Consegne» del menu ☰ ora compare.
- **D-85 a 6 giorni** (`internal/uscita/lettore_telegram.go`): `offsetDimenticato` = 6 giorni;
  `TestLettoreOffsetVecchio` a sei giorni esatti e un microsecondo dopo; testi in `docs/SVILUPPO.md` (due punti e la
  decisione del 14/11 segnata «fatto») e nel manuale da scrivere. La riga D-85 dell'inventario dice già 6 giorni.
- **Banco:** `bench/scenari/consegne.json`, `bench/browser/sessioni-consegna.json`, la funzione `sessioniConsegna` in
  `tools/banco/browser/banco.mjs`; i test del banco (`TestScenariBrowserDelRepo`, `TestScenarioUtentiSuiDueDati`) li
  conoscono.

## Scelte

1. **Moduli POST al posto degli eventi.** Come 076: un rifiuto ridisegna la pagina (i valori scritti restano, come
   LiveView che non tocca i campi), una riuscita torna con il redirect e il campo è vuoto (`assign_form("")`).
2. **Il cursore dopo «Aggiungi».** In LiveView la pagina non si ricarica e il cursore resta nel campo
   (`phx-patch-focused`), comodo con il lettore a pistola. Nel Go la pagina si ricarica: `app.js` annota l'invio di
   `#handover-add-form` nella scheda (`sessionStorage`) e alla pagina dopo rimette il fuoco su `#handover-bag-number`,
   una volta. Aprendo la pagina in altro modo il cursore non ci va (niente `autofocus`: sul telefono aprirebbe la
   tastiera), come lo standalone.
3. **Pulizia solo alla GET della lista.** Lo standalone la fa nel mount connesso, una volta per caricamento; il
   ridisegno dopo un rifiuto di «Apri sessione» (un evento nello standalone) non la rifà. Un guasto della pulizia (per
   esempio il vincolo di un sollecito su una consegna vuota) è la pagina d'errore con «consegne vuote non pulite» nel
   registro: lo standalone lì solleva un'eccezione (come la Scelta 6 della 076).
4. **Il cestino come lo standalone.** Ogni lettura e scrittura delle buste esclude il cestino (`prepare_query/3`), tranne
   le due domande «ha buste» (pulizia) e «ha buste uscite» (in raccolta), che le contano.
5. **Una transazione per l'aggiunta.** Lo standalone fa una transazione per lettura; il Go fa le due letture e la
   scrittura in una. Stessi esiti (la prima lettura che non trova non scrive niente).
6. **Le stampe non ci sono ancora.** «👁 Anteprima», «🖨 Stampa e conferma», «🖨 Ristampa», «📤 Condividi» e la frase
   sotto i pulsanti arrivano con la 078 (niente link verso un 404); «📷 Scansiona» con la 079 (il dominio ha già
   `LetturaScansione`). Il banco non li confronta.
7. **Il segnaposto «es. 0045/26»** (lo standalone «es. 12/26") viene dalla D-64 (il numero scritto è quello
   dell'etichetta). Il banco non lo confronta (nel livello HTTP la consegna della cattura è confermata e non ha il
   campo; nel browser non si legge).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/077-consegne/prove/`): `rosso-clock.log` (`Data.Inizio` che non
  c'è), `rosso-buste.log` con uno scheletro che non fa niente (tutti e nove i test di dominio rossi sull'asserzione),
  `rosso-web.log` senza le rotte (404).
- **Dominio** (`internal/buste/consegne_test.go`): pulizia (ieri vuota, un microsecondo prima della mezzanotte, la
  mezzanotte esatta, oggi, chiusa, con una busta, con una busta nel cestino), apertura (artigiano che non c'è, ripresa,
  la più recente, confermata anche nel cestino, ieri, nessun audit), menu e in raccolta, aggiunta (etichetta «0045/26»,
  «45», «045/26», «0045», gemella più recente, numero interno con gli zeri di una busta senza etichetta, numero interno
  non cercato per una busta con etichetta, QR esatto e normalizzato, scansione prima QR), rifiuti nell'ordine con i
  testi, più buste con il numero scritto (`TestNumeroConPiuBuste`: anno in corso, «0045/25», gemella chiusa, due aperte,
  nessuna aggiungibile, numero interno contro etichetta), corsa persa (gancio `primaDiCollegare`), già uscita con questa consegna, riapertura pulita, togli, conferma (due
  volte, cestino, vuota, chiusa, audit), audit nella transazione (`RAISE(ABORT)`), consegna e buste.
- **Pagine** (`internal/web/consegne_test.go`): lista vuota e piena (markup, ordine, «(sessione aperta)», pulizia,
  link di storico e solleciti con le loro rotte), «Apri sessione» con i moduli forgiati e il guasto (scelta tenuta),
  pagina della sessione (vuota, piena, troncatura, confermata, id che non c'è), aggiunta, togli, conferma, POST senza
  token (403), lo script del fuoco.
- **`Data.Inizio`** (`internal/clock/clock_test.go`): inverno, estate, i due giorni del cambio dell'ora, capodanno.
- **Tabelle aggiornate:** permessi (`ruoli_test.go`), rotte con il guscio e menu (`guscio_test.go`: ora c'è «📤
  Consegne»), `busteFinte`.

## Mutazioni

42 mutazioni a mano sul codice nuovo (dominio, pagine, script, `Data.Inizio`, D-85), una alla volta su una copia del repo
(`to-be-deleted-after-use/077-consegne/mutazioni/`: `muta.py`, `giro.log`, `esiti.json`, `esiti/<id>.log`; copia
cancellata a fine giro). Tutte compilano. Primo giro: **40 prese su 42**; le due vive (B14, W12) hanno avuto un caso di
test in più (una busta rientrata senza uscita legata a un'altra consegna; «· Confermata» con la busta uscita prima nella
lista) e al secondo giro sono **prese**: **42 su 42**, tutte da un'asserzione.

| Id | Mutazione | Presa da |
|---|---|---|
| B01 | pulizia anche della consegna della mezzanotte esatta (`<=`) | `TestPuliziaConsegneVuote` |
| B02 | pulizia: le buste nel cestino non contano | `TestPuliziaConsegneVuote` |
| B03 | in raccolta: le buste uscite nel cestino non contano | `TestApriConsegna` |
| B04 | ripresa della consegna più vecchia | `TestApriConsegna` |
| B05 | menu: contate anche le consegne chiuse | `TestElenchiDelleConsegne` |
| B06 | in raccolta: contate le buste nel cestino | `TestElenchiDelleConsegne` |
| B07 | numero: vince la prima letta, non la più recente | `TestAggiungiAllaConsegna` |
| B08 | numero interno cercato anche per le buste con etichetta | `TestAggiungiAllaConsegna` |
| B09 | campo scritto: prima il QR | `TestAggiungiAllaConsegna` |
| B10 | una lettura sola | `TestAggiungiAllaConsegna`, `TestAggiuntaRiapreUnGiroFinito` |
| B11 | compro oro accettato | `TestRifiutiDellAggiunta` |
| B12 | annullata accettata | `TestRifiutiDellAggiunta` |
| B13 | busta già uscita con questa consegna rifiutata | `TestRifiutiDellAggiunta` |
| B14 | busta rientrata (senza uscita) di un'altra consegna «in raccolta altrove» | `TestAggiuntaRiapreUnGiroFinito` (secondo giro) |
| B15 | riapertura senza togliere la nota dell'artigiano | `TestAggiuntaRiapreUnGiroFinito` |
| B16 | scrittura senza la guardia della corsa | `TestRifiutiDellAggiunta` |
| B17 | artigiano della sessione non assegnato | `TestAggiungiAllaConsegna` e altri |
| B18 | mai «già in lista» | `TestAggiungiAllaConsegna` e altri |
| B19 | «togli» anche di una busta uscita | `TestTogliDallaConsegna` |
| B20 | «togli» toglie anche l'artigiano | `TestTogliDallaConsegna` |
| B21 | id dell'audit con le buste nel cestino | `TestConfermaConsegna` |
| B22 | la conferma chiude la consegna | `TestConfermaConsegna` |
| B23 | campo vuoto con «(«»)» | `TestRifiutiDellAggiunta` |
| B24 | testo «Consegna non disponibile.» | `TestRifiutiDellAggiunta`, `TestConfermaConsegna` |
| C01 | mezzanotte UTC invece di Roma | `TestDataInizio`, `TestPuliziaConsegneVuote`, `TestApriConsegna` |
| D01 | D-85 ancora a sette giorni | `TestLettoreOffsetVecchio` |
| W01 | `handover` testo vale come mappa | `TestApriConsegnaDallaPagina` |
| W02 | scelta non ripulita dagli spazi | `TestApriConsegnaDallaPagina` |
| W03 | guasto detto «Artigiano non trovato.» | `TestApriConsegnaDallaPagina` |
| W04 | scelta non tenuta dopo un rifiuto | `TestApriConsegnaDallaPagina` |
| W05 | nessuna pulizia alla GET | `TestPaginaConsegne` |
| W06 | troncatura a 64 grafemi | `TestPaginaSessione` |
| W07 | «1 buste» | `TestPaginaConsegne`, `TestConfermaDallaPagina` |
| W08 | messaggi di aggiunta scambiati | `TestAggiungiDallaPagina` |
| W09 | conferma a consegna vuota | `TestConfermaDallaPagina` |
| W10 | ritorno alla home senza `fuori_bottega=1` | `TestConfermaDallaPagina` |
| W11 | id di busta non valido con il messaggio | `TestTogliDallaPagina` |
| W12 | «· Confermata» solo se l'ultima busta della lista è uscita | `TestPaginaSessione` (secondo giro) |
| W13 | link allo storico senza la sua pagina | `TestPaginaConsegne` |
| W14 | consegna che non c'è: 404 invece del ritorno alla lista | `TestPaginaSessione`, `TestAggiungiDallaPagina`, `TestTogliDallaPagina` |
| W15 | codice ridisegnato con gli spazi | `TestAggiungiDallaPagina` |
| J01 | `app.js` senza il fuoco nel campo | `TestScriptDelFuocoDellaConsegna` |

**Mutazioni dopo la review** (regola del numero scritto, stessa cartella, `giro.log` giri 3 e 4): **6 prese su 6**. Al
giro 3 quattro risultavano vive perché il filtro dei test dello script non comprendeva `TestNumeroConPiuBuste`;
corretto il filtro, al giro 4 tutte prese.

| Id | Mutazione | Presa da |
|---|---|---|
| R01 | senza la preferenza per l'anno in corso | `TestNumeroConPiuBuste` |
| R02 | candidate consegnate o annullate contate come buone | `TestAggiungiAllaConsegna`, `TestNumeroConPiuBuste` |
| R03 | più candidate: la più recente invece del rifiuto | `TestNumeroConPiuBuste` |
| R04 | nessuna aggiungibile: «non trovata» invece del rifiuto della più recente | `TestRifiutiDellAggiunta`, `TestNumeroConPiuBuste` |
| R05 | testo del rifiuto delle più buste | `TestNumeroConPiuBuste` |
| R06 | più buste: si passa al QR invece di rifiutare | `TestNumeroConPiuBuste` |

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Nessuna D nuova: la D-64 copre i numeri.

- **HTTP**, uno scenario nuovo, `consegne` (ART-05…08), 5 passi: login del titolare; `GET /handovers` (titolo, frase,
  «Nuova consegna», il menu: «— scegli —» sul database vuoto, «— scegli — Incastonatore Sintetico Laboratorio
  Sintetico (sessione aperta)» sui dati della cattura; «Nessun artigiano in anagrafica. Aggiungine uno» e il suo link
  solo sul vuoto; niente «Sessioni aperte oggi»; le intestazioni); `GET /handovers/1` con `normalizza: D-64` (sul vuoto
  302 a `/handovers`; sui dati la consegna 1 dell'artigiano 1: «📤 Consegna a Laboratorio Sintetico», «aperta il
  06/01/2026», «← Indietro», «· Confermata», niente campo né ✕ né conferma, «Buste in consegna 1», la riga della 4/26
  «N. 4/26 — …» confrontata dopo la D-64); di nuovo `GET /handovers` con «Consegna non trovata.» solo sul vuoto. Lo
  standalone sulla GET (render senza socket) non pulisce: sulla cattura non c'è niente da pulire.
  Ci si aspetta: **HTTP 69 scenari, 1175 passi** (68 e 1170 della 076, più 1 e 5), 0 inattese, 0 errori.
- **Browser**, uno scenario nuovo, `sessioni-consegna` (ART-05…08, 10, 11), sulle due viste. Sul vuoto solo la lista.
  Sui dati: «Apri sessione» senza scelta, poi l'artigiano 1; «✓ Conferma senza stampare» a sessione vuota (rifiutata,
  con la finestra); l'aggiunta per etichetta con Invio (Go «0048/26», standalone «27/26» sul PC; «0045/25» / «25/26»
  sul telefono), il cursore nel campo (`etichetta.fuoco`), la stessa di nuovo, la riga in «Sessioni aperte oggi» e il
  ritorno con il suo link, l'aggiunta per QR («RIP-2026-0051», la 30/26), un codice sconosciuto (il campo lo tiene), ✕
  sulla 30/26 (torna libera per la vista dopo), la conferma (la busta dell'etichetta resta fuori bottega),
  `/?fuori_bottega=1` con il messaggio, la sessione confermata, `handover.confirmed` +1. Nei messaggi il numero è «#N»;
  `etichetta.numero` è `solo_go` (D-64: «#0048/26» e «#0045/25»). Lo scenario si chiama così per girare, sul PC,
  **dopo `pannello-qr`** (e dopo `schede`), che sul PC vuole la home senza «📤 Fuori» dopo il rientro della 4/26. Il
  banco fa per ogni app prima tutti gli scenari sul PC e poi tutti sul telefono: dopo questo scenario sul PC girano
  `sfondo`, `tentativi`, `testi-invii`, `visualizzatore`, `whatsapp` e poi **tutte le viste del telefono**
  (`anagrafica-artigiani`, `caricamento`, `coda`, `conferme`, `firma-riquadro`, `galleria-home`, `pannello-qr`,
  `provider-posta`, `provider-telegram`, `riquadro-azioni`, `salva-avverti`, `schede`, `sessioni-consegna`, `sfondo`,
  `tentativi`, `testi-invii`, `visualizzatore`, `whatsapp`; `lettore-qr` per ultimo) con la 27/26 già fuori bottega:
  nessuna guarda «📤 Fuori» sul telefono né la 25/26, la 27/26 o la 30/26 (`whatsapp` sul telefono usa la 39/26; la
  25/26 la usa `whatsapp` sul PC, che gira prima della vista del telefono di questo scenario).
  Ci si aspetta: **browser 19 scenari, 38 viste**, 0 inattese.
- Se qualcosa è rosso, da guardare per primo:
  - `etichetta.fuoco`: nello standalone il fuoco resta per LiveView, nel Go lo rimette `app.js` dopo il redirect.
  - `oggi.righe`: il testo della riga senza l'ora (tolta con gli spazi davanti, `\s*[0-9]{2}:[0-9]{2}$`), «Laboratorio
    Sintetico — 1 busta»: non dipende da come `innerText` separa i due `span`.
  - `qr.righe` e `confermata.righe` non hanno attese: confrontano il testo delle righe (numero «#N», descrizione e ✕).
  - `testo:aperta il 06/01/2026` (HTTP): l'istante della consegna 1 importato dalla cattura («2026-01-06 10:00:00»).
  - Il 302 dello standalone per `/handovers/1` sul vuoto (`push_navigate` nel mount senza socket) e il messaggio dopo.

## Controlli

`scripts/check` verde (`to-be-deleted-after-use/077-consegne/prove/check1.log` e, con i casi di test aggiunti dopo le
mutazioni, `check2.log`; dopo la review `check3.log`, «uscita 0»): gofmt, vet, staticcheck, errcheck, build senza CGO, test, copertura **voci 353:
chiuse 38, rosse 0, parziali 145, aperte 159, fuori v1 11**, segreti e dati veri OK. ART-05…08, 10, 11 passano da aperte
a parziali; si chiudono con l'esito verde del banco. `tools/banco` compilato (`bin/banco`) e i suoi test verdi.

## Review

Review indipendente del coordinatore: una MEDIA e due BASSE, corrette nella slice (test rosso prima:
`prove/rosso-review.log`; `scripts/check` di nuovo verde: `prove/check3.log`).

- **MEDIA — numero scritto con più buste.** Prima vinceva in silenzio l'id più alto: «45» prendeva l'etichetta
  dell'anno prima, una gemella chiusa più recente dava «Stato … non adatto» anche con quella aperta buona, «123/26» del
  QR della 76×127 si scontrava con RIP-2026-0123. Ora la regola di `bustaPerNumero` (sopra, «Cosa c'è», e Dubbio 1, da
  decidere); `TestNumeroConPiuBuste`; mutazioni R01…R06.
- **BASSA — commento dell'ordine** in `banco.mjs` e in questa ricevuta («Banco»): `schede` gira prima di
  `sessioni-consegna`, e tutte le viste del telefono girano dopo la vista del PC; ora sono nominate.
- **BASSA — `oggi.righe`:** l'ora si toglie con gli spazi davanti (`\s*[0-9]{2}:[0-9]{2}$`) e l'attesa è «Laboratorio
  Sintetico — 1 busta», senza dipendere da come `innerText` separa gli `span`.

## Banco s077p (sessione principale) e correzioni

HTTP 69 scenari, 1175 passi, **1 inattesa**; browser 19 scenari, 38 viste, **52 inattese e 1 errore**, tutte di
`sessioni-consegna`. Nessun difetto del Go; tre attese o scelte del banco sbagliate, corrette:

1. **HTTP `consegne[3]`, `visibile:handover-bag-4`:** le due app mostrano «N. 4/26 — Anello sintetico» (standalone
   normalizzato D-64). L'«(assente)» era l'attesa del database vuoto (302) nel blocco `atteso`, che vale anche con i dati
   se `atteso_dati` non la riscrive: mancava la riga dei dati. Aggiunta.
2. **Browser, PC, `pagina.menu`:** le due app danno «Laboratorio Sintetico» senza «(sessione aperta)». Sul PC lo
   scenario gira dopo `pannello-qr`, che fa rientrare la 4/26, l'ultima busta fuori della consegna 1 della cattura:
   la consegna si chiude e l'artigiano non ha più sessioni aperte. Sul telefono «(sessione aperta)» c'è (la consegna
   confermata della vista del PC resta aperta). Attesa del PC corretta.
3. **Browser, telefono, solo Go: `TimeoutError` nella fase `etichetta`** (e le 50 inattese «(assente)» che ne seguono).
   **Causa:** la regola del numero scritto (review, Dubbio 1, **caso B**) sui dati del banco. Il Go scriveva «0047/26»
   (la 26/26, RIP-2026-0047); ma gli scenari prima (HTTP, e nel browser `coda`, `sfondo`, `schede`, `tentativi`… che
   creano buste con codici `QR-040B-…`, non etichette) avevano già creato nel Go buste nuove con i numeri interni
   39/26, 40/26, …, fra cui la **47/26**, aperta e senza etichetta. «0047/26» valeva quindi per due buste che si
   possono dare: «Più buste corrispondono a «0047/26»…», nessuna busta aggiunta, e lo scenario aspettava il conto «1».
   Lo standalone scriveva «26/26» (numero interno, unico). Sul PC «0048/26» è andato bene: a quel punto la 48/26
   non c'era ancora. **Dedotto, non misurato:** i dati dell'istanza s077p sono già cancellati e il giro non l'ho
   rifatto (niente banco); le altre cause possibili sono escluse (la 26/26 è in lavorazione, fuori dal cestino, di
   nessun altro scenario, e nessun altro scenario dà l'etichetta 0047). Il meccanismo è quello provato da `TestNumeroConPiuBuste` (numero interno contro etichetta).
   **Correzione:** sul telefono l'etichetta del 2025 della 25/26 («0045/25», standalone «25/26»): nel 2025 i soli numeri
   interni sono 9999/25 e 10000/25, quindi è una busta sola. `etichetta.numero` sul telefono ora «#0045/25».
   **Fragile:** «0048/26» sul PC regge finché prima dello scenario si creano meno di dieci buste nuove del 2026; se un
   giorno no, va cambiata allo stesso modo (l'unica altra etichetta del 2025 è la 39/26, usata da `whatsapp`).
   **Osservazione per il Dubbio 1:** il caso B succede non appena una busta senza etichetta ha il numero interno uguale
   al numero di un'etichetta; nel negozio vero le buste senza etichetta sono rare e quasi sempre chiuse (quindi
   escluse), ma è un'altra ragione per l'alternativa (a).



- In ☰ c'è «📤 Consegne»: la lista delle consegne agli artigiani, con le sessioni aperte oggi e «Nuova consegna».
- Si sceglie l'artigiano e «Apri sessione»: se per oggi c'è già una sessione in raccolta per lui, si riapre quella.
- Nella sessione si scrive il numero dell'etichetta («0045/26» o «45») o si legge il QR con il lettore a pistola, poi
  Invio: la busta entra nella lista e il cursore resta nel campo per la busta dopo.
- Se il numero scritto vale per più buste che si possono consegnare (per esempio «45» con due etichette 0045 di
  quest'anno), GestRip non ne sceglie una: chiede di scrivere il numero con l'anno o di usare il QR. Senza anno vale
  l'anno in corso.
- La stessa busta due volte dice «già in lista»; una busta consegnata, annullata, compro oro o già da un altro artigiano
  non entra, con il motivo.
- ✕ toglie una busta non ancora confermata.
- «✓ Conferma senza stampare» segna le buste fuori bottega e porta alla lista delle buste fuori bottega; la sessione
  resta aperta finché tutte non rientrano.
- Aprendo la lista spariscono da sole le sessioni dei giorni prima rimaste vuote.
- Le stampe delle etichette della consegna arrivano dopo.

## Dubbi

1. **Più buste con lo stesso numero scritto (da decidere, Stefano).** Lo standalone non ha il caso: cerca il numero
   interno, che è unico. Con la D-64 il numero scritto è quello dell'etichetta e può valere per più buste. Due casi veri:
   - **A — stesso numero d'etichetta.** «0045/26» vale per la gemella chiusa e per quella aperta (busta di carta
     riusata); «45» senza anno vale anche per l'etichetta dell'anno prima (RIP-2025-0045, mostrata «0045/25»), che può
     essere ancora aperta. Nei dati del banco: la 22/26 (RIP-2026-0045, aperta) e la 25/26 (RIP-2025-0045, aperta).
   - **B — numero interno contro etichetta.** Il QR dell'etichetta 76×127 di una busta senza RIP contiene il suo
     numero interno («123/26»), che il lettore a pistola scrive nel campo; «123/26» è anche il numero dell'etichetta
     RIP-2026-0123 di un'altra busta.

   **Regola scelta** (prima della review vinceva in silenzio la più recente, che nel caso A prendeva la 25/26
   dell'anno prima e dava «Stato … non adatto» anche con una gemella aperta buona): fra le candidate contano solo quelle
   che si possono dare all'artigiano; con una sola, quella; con più e l'anno non scritto, quella dell'anno in corso; se
   ne restano più d'una, il rifiuto «Più buste corrispondono a «X»: scrivi il numero con l'anno o usa il QR.» (testo
   solo del Go); se nessuna si può dare, il rifiuto della più recente. Così nel caso A «45» prende la 22/26, «0045/25»
   la 25/26, e una gemella chiusa non disturba. **Nel caso B il rifiuto arriva anche leggendo il QR con la pistola**: il
   QR della 76×127 senza RIP è proprio «123/26», quindi «usa il QR» lì non basta (serve la fotocamera della 079, che
   legge prima il QR, o il codice della busta). `TestNumeroConPiuBuste` prova i casi.

   **Alternative:** (a) nel caso B preferire la busta senza etichetta quando il testo ha la forma «N/AA» (il formato del
   suo QR), e lasciare il numero d'etichetta a chi scrive «0123/26»; (b) tornare alla più recente in silenzio (prima
   della review). **Raccomandazione:** la regola scelta, con l'alternativa (a) se il caso B succede davvero in negozio
   (servono buste senza RIP con l'etichetta 76×127 e un numero interno uguale a un'etichetta usata).
2. **Numero interno di una busta senza etichetta scritto in una forma che non è un'etichetta** («10000/25», cinque
   cifre): il Go lo cerca come numero interno (senza zeri davanti) fra le sole buste senza etichetta, perché il QR della
   loro etichetta 76×127 è proprio il numero interno e il lettore a pistola lo scrive nel campo. Lo standalone cerca ogni
   numero interno. **Raccomandazione:** tenere così (è la regola della D-64: il numero interno resta cercabile solo per
   le buste senza etichetta).

## Banco finale (sessione principale)

`s077q`, 15/11/2026, istanza nuova dopo le correzioni del giro `s077p`: HTTP 69 scenari, 1175 passi, 0 inattese,
0 errori; browser 19 scenari, 38 viste, 0 inattese, 0 errori. `scripts/check` rifatto dalla sessione principale: verde.
