# 080 — Rientro dall'artigiano dalla scheda, con l'email di pronta nella transazione (T4.8, quinta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/workshop.ex` (`return_from_artisan/4`,
  `work_outcome/1`, `artisan_notes/1`, `apply_work_outcome/4`, `stamp_outcome/4`, `applicable_outcomes/1`,
  `return_error_message/1`, `stamp_return/4`, `return_write/4`, `return_refusal/2`, `close_handover_if_complete/3`,
  `announce_ready_by_email/4` alle righe 509-510 e 1832-1841), `lib/gestrip_standalone/repo.ex` (`prepare_query/3`),
  `lib/gestrip_standalone_web/live/repair_live.ex` (evento `return-from-artisan`, `#repair-handed-over`,
  `#repair-artisan-notes`, `return_warning/1`, `return_attrs/1`); test «returning a bag from the artisan» di
  `test/gestrip_standalone/handovers_test.exs`.
- **Voci:** ART-13, RIP-S-16 (portate; si chiudono con l'esito verde del banco); ART-14 e RIP-H-24 (054) solo
  ricontrollate contro le regole di ART-13: **una differenza trovata e corretta** (sotto, «Verifica di ART-14»).
  Nessuna migrazione, nessuna D nuova.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco (Docker) non è stato lanciato:
  lo fa la sessione principale (sotto, «Banco»).

## Cosa c'è

- **Dominio** (`internal/buste/rientro.go`): `RientraDallArtigiano(ctx, id, esito, nota, chi) (Rientro, error)`, una
  transazione:
  1. l'esito senza spazi ai bordi: `fatto`, `da_rifare` o "" (un campo assente è ""); un'altra parola è
     `ErrEsitoNonValido` «Esito lavoro non valido.» prima di ogni scrittura (anche su una busta che non c'è, come
     `work_outcome/1` nel `with` prima della `Multi`);
  2. il timbro (`rientrata_artigiano_il` e `aggiornato_il` con l'istante dell'orologio, la nota nella stessa
     istruzione) con la condizione di fuori bottega nel `WHERE` (fuori dal cestino, consegnata, non rientrata): è
     l'unica autorità, come `return_write/4`. Se non timbra: `ErrBustaNonTrovata` per una busta che non c'è o nel
     cestino, `ErrNonFuoriBottega` «La riparazione non risulta fuori bottega.» per una mai uscita, aggiunta e mai
     confermata o già rientrata (`return_refusal/2`);
  3. la nota senza spazi ai bordi sostituisce quella del giro; una vuota (o di soli spazi) la **cancella**;
  4. l'esito: `fatto` → «pronta» con la data di pronta (oggi in negozio) solo se vuota; `da_rifare` → «in
     lavorazione», la data di pronta resta; lo stesso stato non è un passaggio (un `fatto` su una busta già pronta
     scrive solo la data vuota); uno stato che il grafo della famiglia non permette è `*EsitoNonConsentito`
     («Da «preventivo» non si passa a «pronta»: scegli un altro esito lavoro.», le parole dello stato con gli spazi)
     e **tutto** torna indietro, timbro e nota compresi. Il contatore dei cambi di stato conta solo i cambi veri (il
     trigger della 0014);
  5. la consegna si chiude se nessuna sua busta è più fuori bottega (`chiudiConsegnaSeCompleta`, sotto); una busta
     importata uscita senza consegna rientra senza chiudere niente;
  6. una riga `repair.returned_from_artisan` con `handover_id` (nullo senza consegna), `handover_closed`, `outcome`,
     `status` (lo stato dopo) e `artisan_notes` (vero/falso, mai il testo). Nessuna `repair.status_changed`, come lo
     standalone (una riga sola, come `ARTIG_RIENTRO`);
  7. **l'email di pronta nella stessa transazione** (`avvisaPronta`, SAVEPOINT `email_pronta`, sezione 070b) quando
     l'esito porta la busta a «pronta» **da un altro stato**; niente email se era già «pronta» prima del rientro,
     né con `da_rifare` o l'esito vuoto. Lo standalone la accoda dopo il commit (`announce_ready_by_email(:returned,
     …)`); nel Go è la scelta della 070b: il nuovo stato, il rientro e l'email si scrivono insieme, un'email che non
     parte (`repair.email_ready_not_queued`) non toglie il rientro. La riga del rientro viene prima di quella
     dell'email, come nello standalone;
  8. il gancio di prova `primaDelCommit`, poi il commit.
  `Rientro` dice stato prima e dopo, consegna e chiusura. `EsitiApplicabili(stato, oro)` è `applicable_outcomes/1`
  (sempre con "" in fondo). `ErroreDelRientro(err)` dice se l'errore è un rifiuto da mostrare com'è.
- **Chiusura della consegna** (`chiudiConsegnaSeCompleta`, = `close_handover_if_complete/3`): un `UPDATE` con
  `chiusa_il IS NULL` (una consegna già chiusa tiene la prima chiusura) e `NOT EXISTS` di una busta della consegna
  **fuori dal cestino** e fuori bottega (una busta aggiunta e mai confermata non la tiene aperta). La usano il rientro
  dalla scheda e il rientro automatico dal QR (`rientroAutomatico`, 054).
- **Scheda** (`internal/buste/scheda.go`: `ConsegnataArtigianoIl`, `RientrataArtigiano`, `NoteArtigiano`;
  `internal/web/rientro.go`; `scheda.html` dopo i dati e prima della conversazione, come lo standalone; CSS in
  `app.css`, anche il tema scuro):
  - `#repair-handed-over` solo per una busta fuori bottega: «🔧 Fuori bottega presso <artigiano>» (l'artigiano
    assegnato, «artigiano» senza), «Consegnata il gg/mm/aaaa hh:mm» (ora del negozio), «✓ Segna rientrata» che apre
    un `<details>` con il modulo (`#repair-return-form`, `data-invio-unico`): «Rientro da <artigiano>», l'avviso
    `#repair-return-outcome-warning` di `return_warning/1` (da preventivo «l'esito «Fatto» non è applicabile…», da
    consegnata, annullata e dai compro oro «nessun esito è applicabile…»), «Stato lavoro» con i tre esiti dello
    standalone e **«fatto» primo e scelto** (il predefinito di «Conferma rientro»), la nota con l'etichetta e il
    segnaposto dello standalone, «Conferma rientro» con la conferma «Confermare il rientro della busta
    dall'artigiano?» (`data-confirm`, `app.js`); accanto «🖨 Ristampa» (`/repairs/{id}/label.pdf` in una nuova scheda)
    e «📤 Condividi» (`?download=1` con `data-share-pdf`, il contratto di `app.js` della 046);
  - `#repair-artisan-notes` «📝 Note ultimo rientro da artigiano:» con la nota com'è scritta (a capo compresi) per una
    busta rientrata con una nota.
- **Rotta** `POST /repairs/{id}/rientro` (`f.rotta`, `personale`, CSRF): `return[stato_lavoro]`,
  `return[note_artigiano]` (con `aCapoUnix`, come le altre caselle interne). Riuscito: redirect alla scheda con
  «Rientro registrato.». Un rifiuto (il testo di `return_error_message/1`) o un errore («Rientro non riuscito.
  Nessuna modifica applicata.») **ridisegnano la scheda** (200, `rientroRifiutato`, come la riapertura rifiutata)
  con il messaggio e, se la busta è ancora fuori bottega, il modulo aperto (`<details open>`), l'esito e la nota
  mandati (un esito che il campo non può dare lascia «fatto»): nello standalone il modulo resta com'era dopo il
  flash (revisione, punto 1). Un id malformato, una busta che non c'è o nel cestino: la home con «Riparazione non
  trovata.» (come le altre azioni della scheda). Nessun popup Telegram dopo il rientro, come lo standalone.

## Scelte

- **«📤 Condividi», non «Scarica».** Il piano e l'inventario (RIP-S-16) dicono «Ristampa» e «Scarica»; la scheda dello
  standalone ha «📤 Condividi» con `data-share-pdf` (che dove non si può condividere scarica). Seguito lo standalone:
  è la prima pagina della scheda che usa `data-share-pdf` (RIP-S-44 resta parziale per lo storico e il registro
  d'emergenza; `TestCondividiPDFSullePagine` ora la controlla).
- **La busta che non c'è va alla home** con «Riparazione non trovata.», come stato, attesa e riapertura: lo standalone
  non ha questo ramo (la busta è quella montata; il suo ramo generico dice «Rientro non riuscito…»).
- **Il campo dell'esito assente vale ""** («Nessun cambio di stato»), come `Map.get(attrs, "stato_lavoro", "")`: un
  modulo che arriva senza il campo non porta la busta a «pronta».

## Verifica di ART-14 (rientro automatico dal QR, 054) contro ART-13

- **Chiusura:** il rientro automatico chiudeva la consegna contando anche le buste **nel cestino** ancora fuori
  bottega; lo standalone no (`close_handover_if_complete/3` usa `repo.exists?` su `Repair`, che
  `Repo.prepare_query/3` filtra col cestino). Corretto: ora i due rientri usano la stessa `chiudiConsegnaSeCompleta`
  con `b.cancellata_il IS NULL` (test `TestRientroChiusuraConsegna`, ramo «qr», rosso prima della correzione:
  `prove/rosso-buste-2.log`). Una busta aggiunta e mai confermata e una consegna già chiusa erano già come lo
  standalone.
- **Nessun doppio rientro:** il rientro automatico ha la condizione di fuori bottega nel suo `UPDATE` e non rientra
  una busta già rientrata a mano; il rientro a mano rifiuta una busta rientrata dal QR (`TestRientroDoppio`). Il
  rientro automatico non tocca la nota del giro (054, invariato).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/080-rientro/prove/`): `rosso-buste-1.log` (dominio che non
  c'è), `rosso-buste-2.log` (con il dominio scritto, solo il ramo «qr» della chiusura con il cestino),
  `rosso-web.log` (riquadro e rotta che non ci sono); `TestRientroRigheDelModulo` visto rosso prima della riga di
  `aCapoUnix` (non salvato in un log).
- **Dominio** (`internal/buste/rientro_test.go`): `TestRientroDallArtigiano` (timbro, istante, consegna aperta con
  un'altra busta fuori e chiusa con l'ultima, audit esatto con attore e IP, nessuna `status_changed`),
  `TestRientroEsiti` (dieci casi: data di pronta, lo stesso stato, da_rifare, esito vuoto, spazi, contatore dei
  cambi), `TestRientroRifiuti` (esito non valido anche su una busta che non c'è, mai uscita, aggiunta e mai
  confermata, già rientrata, che non c'è, nel cestino, grafo da preventivo, consegnata e compro oro, con i testi;
  niente scritto), `TestRientroNota`, `TestRientroChiusuraConsegna` (senza consegna, già chiusa, mai confermata, già
  rientrata, cestino, per la scheda e per il QR), `TestRientroDoppio` (due rientri insieme: uno timbra, l'altro
  rifiutato; QR dopo il rientro e rientro dopo il QR), `TestEsitiApplicabili`, `TestRientroEmailPronta` (l'email con
  il segno all'istante del rientro, l'ordine delle righe, niente email da pronta, con da_rifare o esito vuoto),
  `TestRientroEmailStessaTransazione` (tutto nella transazione prima del commit, niente dopo un commit mancato; una
  coda che rifiuta non toglie il rientro).
- **Pagine** (`internal/web/rientro_test.go`): `TestRiquadroFuoriBottega` (markup esatto per titolare e dipendente,
  posto nella pagina, «artigiano» senza nome, assente da rientrata e mai uscita), `TestRiquadroFuoriBottegaAvviso`,
  `TestRientroDallaScheda` (il rientro del dipendente, «Rientro registrato.», pronta con la data, la nota scappata e
  a capo, consegna chiusa, secondo invio rifiutato, campi assenti), `TestRientroRifiutatoDallaScheda` (grafo, parola
  sconosciuta con la scheda ridisegnata e il modulo aperto, 403, home per id e cestino, errore del database, utente disattivato); `TestAvvisoDelRientro` (ogni stato delle due famiglie);
  `TestRientroRigheDelModulo` (`a_capo_test.go`).
- **Ritoccati:** la tabella dei permessi (`ruoli_test.go`, RIP-S-16), l'elenco delle caselle normalizzate
  (`a_capo_test.go`), `busteFinte` (`nuova_test.go`).
- **Banco** (`tools/banco`): `TestScenariBrowserDelRepo` (lo scenario nuovo e le sue attese comuni),
  `TestProvaConDegliScenariDelRepo` e `TestScenarioUtentiSuiDueDati` (lo scenario HTTP nuovo).

## Mutazioni

36 mutazioni a mano sul codice nuovo e toccato (dominio, chiusura del QR, scheda, rotta, pagina), una alla volta su una
copia del repo (`to-be-deleted-after-use/080-rientro/mutazioni/`: `muta.py`, `giro1.log`, `giro2.log`, `esiti*.json`,
`esiti/<id>.log`; copia cancellata a fine giro). Primo giro: **34 prese su 36**; W06 (la nota mostrata anche senza
rientro) sopravvissuta: aggiunto a `TestRiquadroFuoriBottega` il caso della nota del giro in corso; W07 non compilava
(scritta male: tolto il ramo e con lui l'uso di `errors`), riscritta come `case false && …`. Secondo giro su W06 e W07:
**prese tutte e due**. Totale **36 su 36**, tutte da un'asserzione.

| Id | Mutazione | Presa da |
|---|---|---|
| D01 | esito sconosciuto accettato | TestRientroRifiuti |
| D02 | nota non ripulita | TestRientroNota |
| D03 | nota vuota che non cancella | TestRientroNota |
| D04 | timbro anche nel cestino | TestRientroRifiuti |
| D05 | timbro anche già rientrata | TestRientroDoppio, TestRientroRifiuti |
| D06 | rifiuto sempre «non fuori bottega» | TestRientroRifiuti |
| D07 | lo stesso stato chiede il grafo | TestRientroEmailPronta, TestRientroEsiti |
| D08 | grafo non controllato | TestRientroRifiuti |
| D09 | data di pronta sempre scritta | TestRientroEsiti |
| D10 | email anche da pronta | TestRientroEmailPronta |
| D11 | email mai | TestRientroEmailPronta, TestRientroEmailStessaTransazione |
| D12 | chiusura che conta il cestino | TestRientroChiusuraConsegna |
| D13 | chiusura che conta le non confermate | TestRientroChiusuraConsegna |
| D14 | audit con lo stato di prima | TestRientroChiusuraConsegna, TestRientroEsiti |
| D15 | audit senza la nota | TestRientroNota |
| D16 | gancio prima del commit saltato | TestRientroEmailStessaTransazione |
| D17 | esiti applicabili senza lo stesso stato | TestEsitiApplicabili |
| D18 | messaggio con lo stato grezzo | TestRientroRifiuti |
| D19 | consegna già chiusa richiusa | TestRientroAutomaticoDaQR, TestRientroChiusuraConsegna |
| D20 | audit senza consegna | TestRientroDallArtigiano |
| Q01 | QR con la chiusura vecchia | TestRientroAutomaticoDaQR, TestRientroChiusuraConsegna |
| S01 | scheda: rientrata sempre falsa | TestRientroDallaScheda |
| S02 | scheda: uscita non letta | TestRiquadroFuoriBottega |
| W01 | messaggio del rientro | TestRientroDallaScheda |
| W02 | nota con \r\n | TestRientroRigheDelModulo |
| W03 | rifiuto come errore generico | TestRientroDallaScheda, TestRientroRifiutatoDallaScheda |
| W04 | avviso a due esiti sbagliato | TestRiquadroFuoriBottegaAvviso |
| W05 | riquadro senza artigiano assente | TestRiquadroFuoriBottega |
| W06 | note anche senza rientro | TestRiquadroFuoriBottega |
| W07 | busta non trovata come errore generico | TestRientroRifiutatoDallaScheda |
| W08 | esito letto dal campo sbagliato | TestRientroDallaScheda, TestRientroRifiutatoDallaScheda |
| R01 | rotta solo del titolare | TestOgniRottaHaIlSuoPermesso, TestRientroDallaScheda, TestRientroRigheDelModulo |
| T01 | fatto non scelto | TestRiquadroFuoriBottega, TestRiquadroFuoriBottegaAvviso |
| T02 | condividi senza download | TestCondividiPDFSullePagine, TestRiquadroFuoriBottega |
| T03 | riquadro senza conferma | TestRiquadroFuoriBottega |
| T04 | avviso non mostrato | TestRiquadroFuoriBottegaAvviso |

Dopo la revisione e il banco s080p, 10 mutazioni nuove sul codice del rifiuto ridisegnato e dell'avviso (`giro3.log`,
`esiti-giro3.json`) e T01 rifatta sul template nuovo (`giro4.log`): **11 prese su 11**.

| Id | Mutazione | Presa da |
|---|---|---|
| N01 | modulo non riaperto dopo il rifiuto | TestRientroRifiutatoDallaScheda |
| N02 | nota persa dopo il rifiuto | TestRientroRifiutatoDallaScheda |
| N03 | esito non ripreso | TestRientroRifiutatoDallaScheda |
| N04 | esito sconosciuto ripreso | TestRientroRifiutatoDallaScheda |
| N05 | rifiuto con redirect | TestRientroDallaScheda, TestRientroRifiutatoDallaScheda |
| N06 | template senza open | TestRientroRifiutatoDallaScheda |
| N07 | template senza la nota | TestRientroRifiutatoDallaScheda |
| N08 | avvisi scambiati | TestAvvisoDelRientro, TestRiquadroFuoriBottegaAvviso |
| N09 | avviso da preventivo perso | TestAvvisoDelRientro, TestRiquadroFuoriBottegaAvviso |
| N10 | errore generico con redirect | TestRientroRifiutatoDallaScheda |
| T01 | fatto non scelto (template nuovo) | TestRientroRifiutatoDallaScheda, TestRiquadroFuoriBottega, TestRiquadroFuoriBottegaAvviso |

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Nessuna D nuova.

- **HTTP**, uno scenario nuovo, `rientro-scheda` (RIP-S-16, ART-13), 4 passi, solo lettura: login; `GET /repairs/4`
  (la 4/26 della cattura, fuori bottega nella consegna 1 di «Laboratorio Sintetico»): il riquadro, «Consegnata il
  06/01/2026» (l'ora si confronta fra le due app senza attesa), il pulsante, il titolo del modulo, il menu con i tre
  esiti e «fatto» primo e scelto, i nomi dei campi, il segnaposto, il pulsante con la conferma, «Ristampa» e
  «Condividi» con i loro indirizzi, niente avviso e niente note; `GET /repairs/1` (in bottega): tutto assente. Sul
  database vuoto 302 alla home. Il rientro vero non si fa nel livello HTTP: nello standalone è un evento LiveView
  (nessun POST da confrontare), e la 4/26 deve restare fuori per `pannello-qr` (browser, PC).
  Ci si aspetta: **HTTP 72 scenari, 1207 passi** (71 e 1203 della 079, più 1 e 4), 0 inattese, 0 errori.
- **Browser**, uno scenario nuovo, `sessioni-rientro` (ART-13, RIP-S-16), sulle due viste, su **dati propri** (dopo
  il banco s080q, sotto). Sul vuoto la lista delle consegne («Nessun artigiano in anagrafica.»). Sui dati, per vista:
  una busta nuova da «Nuova riparazione» (cliente «Cliente rientro 080 <vista>», QR `SINT-RIENTRO-080-<vista>`,
  accettata il 02/06/2025, senza artigiano: il numero del 2025 non si confronta, la busta sta in fondo alla lista e non
  tocca la numerazione del 2026); una consegna per «Incastonatore Sintetico» (scelto per nome; **non l'artigiano 1**:
  la consegna confermata di `sessioni-consegna` resta aperta e il suo menu direbbe sempre «(sessione aperta)») la
  prende per QR e la conferma; sulla scheda il riquadro (nome, «Consegnata il <data>», «fatto» scelto, i tre esiti,
  nessun avviso, «Ristampa» e «Condividi» con l'id scritto `ID`, niente note); «✓ Segna rientrata», una nota con
  spazi, «Conferma rientro» **senza toccare l'esito** (il predefinito «Fatto») con la conferma: «Rientro registrato.»,
  «Pronta», la data di pronta scritta (`rientro.pronta` = «(nessuno) → <data>»), il riquadro sparito, la nota
  ripulita; nel menu della lista «Incastonatore Sintetico» senza «(sessione aperta)» (la consegna chiusa); contate
  dopo la creazione: `repair.returned_from_artisan` +1, `_auto` +0, `handover.confirmed` +1, `repair.status_changed`
  +0; alla fine «Modifica» toglie l'artigiano alla busta (`fine.artigiano` = «—»). L'email di pronta **non** si
  confronta (D-16: il Go del banco non ha l'email accesa né indirizzi confermati, come la 070b).
  **Ordine e dati degli altri:** gira dopo `sessioni-consegna`. Tocca solo dati suoi (la busta, la consegna) e, per il
  tempo dello scenario, il conto delle buste di «Incastonatore Sintetico», che rimette com'era: `anagrafica-artigiani`
  sul telefono («ha 1 riparazione associata») lo ritrova uguale. Se si ferma a metà sul PC, sul telefono lo vedono
  `sessioni-consegna` (`pagina.menu`) e `anagrafica-artigiani` (il conto): inattese, non timeout. Le 26/26 e 27/26
  restano fuori bottega come dopo la 079.
  Ci si aspetta: **browser 20 scenari, 40 viste**, 0 inattese.
- Se qualcosa è rosso, da guardare per primo:
  - `rientro.messaggio` / `rientro.stato`: nello standalone il rientro ridisegna la scheda in LiveView, nel Go un
    redirect alla scheda con il messaggio; lo scenario aspetta «Pronta» in `#repair-status` e poi il messaggio.
  - `scheda.esiti`: i testi delle opzioni con le virgolette doppie (`Fatto → passa a "pronta"`).
  - `consegna.menu` / `sessione.menu`: se un giorno un altro scenario lascia aperta una consegna di «Incastonatore
    Sintetico», il menu avrà «(sessione aperta)» già prima.
  - HTTP `visibile:repair-handed-over-since`: l'ora dell'uscita importata dalla cattura («2026-01-06 10:00:00»),
    confrontata fra le due app senza un valore atteso.
  - `attributo:return_stato_lavoro/option@1:selected`: Phoenix scrive `selected` senza valore («»), come il Go.

## Banco s080p (sessione principale), revisione e correzioni

s080p: HTTP 72 scenari, 1207 passi, 0 inattese; browser 20 scenari, 40 viste, **5 inattese**, tutte con le due app
uguali (attese e ordine del banco, nessun difetto del Go):

1. **`anagrafica-artigiani[1]` (telefono) `elimina-rifiutato.errore`**: «ha 2 riparazioni associate» al posto di 1.
   **Causa:** `sessioni-rientro` sul PC (che gira prima di tutte le viste del telefono) aggiungeva la 30/26 a una
   consegna di «Incastonatore Sintetico», il primo artigiano per nome, quello che lo scenario prova a eliminare;
   l'aggiunta scrive l'artigiano della busta. **Correzione** (sostituita dopo s080q, sotto): lo scenario usa la **6/26**, che è già di «Incastonatore
   Sintetico» (il conto non cambia) e che nessun altro scenario browser tocca (anche il punto 3 della revisione: un
   errore a metà non lascia più la 30/26 fuori per `sessioni-consegna` del telefono).
2. **`sessioni-rientro[0]` e `[1]` `busta-30.percorso` / `busta-30.errore`**: le attese del database vuoto (blocco
   `atteso`) valgono anche con i dati se `atteso_dati` non le riscrive (lo stesso errore della 077). Con i dati lo
   scenario non apre la busta assente: ora `busta-6.*` sono «(assente)» in `atteso_dati`.

Revisione (tutte BASSE), fatte: (1) dopo un rifiuto la scheda si ridisegna con il modulo aperto, l'esito e la nota
(sopra, «Rotta»; test `TestRientroRifiutatoDallaScheda` riscritto, rosso prima: `prove/rosso-revisione-web.log`);
(2) `avvisoDelRientro` confronta gli esiti con `slices.Equal`, non il loro numero (`TestAvvisoDelRientro`, ogni stato
delle due famiglie); (3) la busta dello scenario (punto 1 sopra); (4) `rientro.pronta` ora confronta la data di
prima e di dopo (sul telefono «presente» non provava niente); (5) il commento d'ordine di `sessioniConsegna` nomina
`sessioni-rientro`.

## Banco s080q (sessione principale) e correzione: dati propri

s080q: HTTP verde (72/1207/0); browser: `sessioni-rientro` sul PC verde, sul telefono **errore in tutte e due le app**
(fase `prepara`, `TimeoutError: locator.selectOption`) con `prepara.stato` = «Consegnata». **Causa:** la 6/26 la
usa `firma-riquadro` sul telefono (`const busta = vista === 'desktop' ? 2 : 6`: la firma la consegna), che gira prima
di `sessioni-rientro` del telefono; il numero è una variabile per vista in `banco.mjs` e compare solo nelle attese
(`bench/browser/firma-riquadro.json`, `/repairs/6/firma`), e la mia ricerca della s080p guardava i numeri scritti nel
codice. Anche `caricamento` apre `/repairs/6`. **Correzione** (con l'aggiunta del coordinatore): lo scenario non usa
più buste della catena, ne crea una sua per vista (sopra, «Banco»), del 2025; l'artigiano della catena che serve
(«Incastonatore Sintetico») lo lascia com'era togliendolo alla busta alla fine. Nessun artigiano nuovo: uno con una
consegna non si può eliminare (`ArtigianoConConsegne`) e il menu e l'anagrafica del telefono lo vedrebbero. La regola
è scritta in SVILUPPO.md («Banco: scenari che cambiano i dati», «Dati propri») e nel commento d'ordine. Sul vuoto lo
scenario guarda solo la lista delle consegne (`vuoto.nessuno`), con il caso nuovo in `TestScenariBrowserDelRepo`.
Solo il banco è cambiato (nessun codice del Go): `scripts/check` rifatto verde (`prove/check4.log`).
**Rischio residuo:** il numero del 2025 dato alla busta nuova dipende dal contatore del 2025 (le buste 9999/25 e
10000/25 della semina hanno numeri scritti a mano); non si confronta, e diventerebbe un problema solo se fosse
45/25, che `sessioni-consegna` del telefono scrive come etichetta «0045/25» (una busta senza etichetta con quel numero
interno corrisponderebbe anche lei).

## Controlli

`scripts/check` verde (`to-be-deleted-after-use/080-rientro/prove/check1.log` e, dopo il test aggiunto per W06 e il
ritocco dell'attesa di `sessioni-rientro` in `banco.mjs`, `prove/check2.log`, «exit 0»): gofmt, vet, staticcheck,
errcheck, build senza CGO, test, copertura **voci 353: chiuse 40, rosse 0, parziali 148, aperte 154, fuori v1 11**,
segreti e dati veri OK. ART-13 e RIP-S-16 passano da aperte a parziali; si chiudono con l'esito verde del banco.
`tools/banco` compilato con i suoi test verdi (dentro `scripts/check`). Dopo le correzioni del banco s080p e della
revisione di nuovo verde: `prove/check3.log`, «exit 0», stessi numeri della copertura; dopo la correzione del banco
s080q `prove/check4.log` (sotto).

## Per il manuale

- Sulla scheda di una busta che è dall'artigiano c'è il riquadro blu «🔧 Fuori bottega presso …» con il giorno e
  l'ora dell'uscita, «🖨 Ristampa» (l'etichetta della busta) e «📤 Condividi».
- Quando la busta torna: «✓ Segna rientrata», si sceglie lo stato del lavoro («Fatto» è già scelto: la busta passa
  a «pronta»; «Da rifare» la rimette in lavorazione; «Nessun cambio di stato» la lascia com'è), si trascrive la nota
  che l'artigiano ha scritto sull'etichetta e «Conferma rientro».
- Se la busta era in preventivo, il riquadro avvisa prima che «Fatto» non si può scegliere.
- Con «Fatto» il cliente che ha attivato l'avviso email riceve l'email di pronta, una volta, come per ogni passaggio a
  «pronta».
- Quando rientra l'ultima busta di una consegna, la consegna si chiude da sola.
- Dopo il rientro la scheda mostra la nota dell'artigiano.

## Dubbi

1. **«Prima e dopo» nel livello HTTP.** Il piano chiede la scheda della 4/26 «prima e dopo» nel livello HTTP; ma il
   rientro nello standalone è un evento LiveView (niente da confrontare con un POST) e la 4/26 deve restare fuori per
   `pannello-qr` del browser, che la fa rientrare dal QR. Il «dopo» lo guarda il browser (`sessioni-rientro`, su una
   busta e una consegna sue); nel livello HTTP la 1/26 fa da busta in bottega. **Raccomandazione:** tenere così.
   **Deciso da Stefano il 15/11/2026:** tenere così: il «dopo» del rientro lo guarda solo il browser. Non è una deviazione.
2. **Nessun popup Telegram dopo il rientro con «Fatto».** Dalla scheda e dalla lista un passaggio a «pronta» apre il
   popup della notifica Telegram (074); il rientro con «Fatto» no, come lo standalone (`return-from-artisan` non
   chiama `transition_repair_freezing/5`). L'email di pronta invece parte. **Raccomandazione:** tenere come lo
   standalone; se Stefano vuole il popup anche qui, è una slice piccola dopo la T4.8.
   **Deciso da Stefano il 15/11/2026:** tenere come lo standalone, nessun popup Telegram dopo «Fatto». Non è una deviazione.
3. **La busta nel cestino e la chiusura della consegna** (sopra, «Verifica di ART-14»): allineata allo standalone
   anche per il rientro automatico dal QR della 054, che prima la contava. Non è una differenza voluta: è una
   correzione. Una busta cestinata e poi ripristinata ritrova la sua consegna (chiusa) e resta fuori bottega: il suo
   rientro manuale funziona e non riapre la consegna (come lo standalone). Segnalato perché cambia un
   comportamento già portato.
   **Deciso da Stefano il 15/11/2026:** tenere la correzione (allineata allo standalone). Non è una deviazione.

## Banco finale (sessione principale)

`s080r`, 15/11/2026, istanza nuova dopo le correzioni di `s080p` e `s080q`: HTTP 72 scenari, 1207 passi, 0 inattese,
0 errori; browser 20 scenari, 40 viste, 0 inattese, 0 errori. `scripts/check` rifatto dalla sessione principale: verde.
