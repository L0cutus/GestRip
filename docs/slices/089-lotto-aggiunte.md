# 089 — Pagina del lotto `/ready-lots/{id}`: aggiunte, candidate, «Rimuovi» (T4.9, quarta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/ready_lots.ex` (`fetch_lot/1`, `members/1`,
  `candidates/1`, `candidate_refusal/1`, `add_bag/3`, `remove_bag/3`, `lot_row/2`, `still_open?/2`, `find_bag/4`,
  `attach_bag/3`, `add_refusal/3`, `bag_refusal/3`, `held_by/2`, `detach_bag/3`, `remove_refusal/2`, `candidate_query/1`,
  `lot_param/2`, `refusal_message/1`), `ready_notice.ex` (`pending_query/1`), `workshop/ready_lot.ex` (`open?/1`,
  `open_query/1`), `workshop/repair.ex` (`ready_query/1`), `ready_lot_live.ex` (mount, eventi `validate_bag`, `add`,
  `add_scanned`, `add_candidate`, `remove`, `refuse/2`, `truncate/1`, `render/1` senza le parti di 090 e 091),
  `repair_live.ex` (`#repair-notice-in-lot` con «Apri il lotto»); test `ready_lots_test.exs` («adding a bag», «removing a
  bag», «the candidate list») e `ready_lot_live_test.exs` («the lot page», «the interlock seen from the bag's own page»).
- **Voci:** ORE-06, ORE-08, ORE-09 (portate; si chiudono con l'esito verde del banco). Nessuna migrazione (colonne della
  0007). Deviazioni: D-64 (numero scritto e mostrato); due proposte nuove nei Dubbi (D-95, D-96), non ancora in
  `bench/deviazioni.json`.
- **Lavoro:** sottoagente d'implementazione (Opus), test rossi prima; mutazioni e revisione di un revisore Opus; banco
  della sessione principale (s089a, s089b da fare). Commit solo locale, push con 087 e 088.

## Cosa c'è

- **Dominio** (`internal/buste/lotto_buste.go`, nuovo):
  - `LottoAvviso` (= `fetch_lot/1`): il lotto in qualunque stato con il nome dell'oreficeria, il gettone, gli istanti e
    il link storico spento (`accesso_pubblico_spento_il`); `ErrLottoNonTrovato` («Lotto non trovato.»).
    `Modificabile()` è `open?/1`.
  - `BusteDelLotto` (= `members/1`): fuori dal cestino, dalla più recente. `CandidateDelLotto` (= `candidates/1`): pronte
    dell'oreficeria, in nessun lotto, non compro oro, fuori dal cestino, **comprese le già avvisate** (`GiaAvvisata`,
    `candidate_refusal/1`), per data di pronta e id decrescenti.
  - `AggiungiAlLotto` (= `add_bag/3`): lotto che non c'è → «Lotto non disponibile.»; poi la busta (QR, poi numero, poi id:
    il primo campo non vuoto decide e, se non trova la busta, è «Riparazione non trovata.» senza provare il campo dopo;
    tutti vuoti «Numero busta, QR o id richiesto.»); poi **una sola scrittura condizionata**: `UPDATE buste … WHERE` busta da avvisare
    (`condizioneDaAvvisare`, la stessa dell'avviso singolo della 066) **e** in nessun lotto **e** di questa oreficeria
    **e** non compro oro **e** fuori dal cestino **e** `EXISTS` lotto ancora aperto. Se non prende niente, il motivo si
    chiede dopo, nell'ordine di `bag_refusal/3`: lotto non più aperto, «Busta già in questo lotto.», «Busta già avvisata
    singolarmente: …» (prima dei due del lotto), `*InAltroLotto` («Busta già nel lotto #N: toglila da lì, poi riprova.» o
    «Busta già uscita col lotto #N: resta sua. …»), «Busta di altra oreficeria.» (anche una diretta del negozio), «Le
    schede compro oro non entrano in un lotto.», «Busta non pronta.»; una busta cestinata nel frattempo «Riparazione non
    trovata.» (`repo.get/2` non vede il cestino). Riga `ready_lot.bag_added` (`repair_id`) nella stessa transazione.
  - `TogliDalLotto` (= `remove_bag/3`): una scrittura condizionata (busta nel lotto, fuori dal cestino, lotto aperto),
    poi «Busta non in questo lotto.» o «Lotto non disponibile.»; riga `ready_lot.bag_removed` nella transazione.
  - `bustaPerNumero` della consegna diventa `bustaPerNumeroCon` con il controllo delle buste «buone» come parametro: per
    il lotto sono, per il loro stato, quelle che un lotto dell'oreficeria prende (`lottoBuono`: pronte, non compro oro,
    dell'oreficeria, fuori dal cestino; come `aggiungibile` della consegna non guarda avviso né lotto, review della 089).
    Gancio di prova `primaDelLotto` fra la lettura e la scrittura.
- **Pagina** (`internal/web/lotto.go`, `pagine/lotti.html` «lotto», `statici/app.css` `.lotto-*`): `GET /ready-lots/{id}`,
  `POST …/aggiungi` (`bag[numero_busta]`), `POST …/qr` (`qr`), `POST …/buste/{busta}/aggiungi` (candidata), con il guscio
  (un rifiuto ridisegna); `POST …/buste/{busta}/togli` (sempre un redirect). Titolo «Lotto <oreficeria>», «← Indietro»,
  «📦 Lotto <oreficeria>», «Lotto #N aperto il …» con «· Non più modificabile», «Numero busta» (`es. 0045/26`), il campo
  del QR con «Aggiungi dal QR», «Buste nel lotto» col conto e «Nessuna busta. Scansiona o aggiungi a mano.», le righe
  (numero visibile D-64, descrizione tagliata a 50 grafemi, «Rimuovi»), «Pronte di <oreficeria> non ancora in lotto» con
  «Aggiungi» o la segnalazione «Già avvisata singolarmente — …» e «Apri busta». Lotto uscito: niente aggiunte, «Rimuovi»,
  candidate; «Questo lotto è partito il … Il link pubblico è <indirizzo pubblico>/lotto/<gettone>.» oppure «Link storico
  non riattivato durante l'importazione.».
- **Scheda:** nel riquadro dell'avviso di una busta in un lotto aperto, «Apri il lotto» verso `/ready-lots/<id>`
  (`#repair-notice-lot-link`, solo del Go come id).
- **Tabelle aggiornate:** permessi (`ruoli_test.go`, ORE-06/08/09, `personale`), rotte con il guscio (`guscio_test.go`),
  interfaccia delle buste (`nuova.go`) e `busteFinte`, rotte (`registraLotti` chiama `registraLotto`).
- **Documenti:** `docs/SVILUPPO.md` («Rotte», paragrafo della 088 aggiornato e uno nuovo per la 089).
- **Banco** (non lanciato): HTTP `lotto-pagina`, browser `lotti-buste`; da `lotti-apertura` tolti la pagina finta di
  `/ready-lots/L` e il giro del POST con il meta refresh (ora guarda anche il titolo della pagina vera, `apri.titolo`);
  `anagrafica-oreficerie` telefono `pagina.righe` 7. Sotto, «Banco».

## Scelte

1. **Stesso schema della 077 (consegne):** senza LiveView gli eventi sono POST. Riuscita → redirect alla pagina con
   «✓ Aggiunta busta <numero visibile>» e i campi vuoti (`assign_form("")`); «Busta già in questo lotto.» è un esito
   (`refuse/2`, `:info`) e, come ogni rifiuto, ridisegna la pagina col numero scritto (`assign_form(typed)`); dalla
   candidata il campo è vuoto (`add/3` con `""`); «Rimuovi» è sempre un redirect. Dopo un rifiuto la pagina rilegge il
   lotto: uno inviato nel frattempo smette di offrire i moduli (come `reload_lot/1` dell'invio).
2. **Una scrittura sola che decide.** Con una connessione di scrittura e `BEGIN IMMEDIATE` due aggiunte non si
   intrecciano davvero, ma la regola è quella dello standalone: niente si decide su una riga letta prima. Il test con il
   gancio `primaDelLotto` scrive in mezzo (avviso singolo, stato, oreficeria, cestino, lotto inviato o chiuso, compro oro,
   un altro lotto) e la scrittura rifiuta con il motivo giusto.
3. **«Rimuovi» senza domanda**, come `phx-click="remove"` dello standalone (la consegna del Go ha ✕ con la domanda perché
   ce l'ha lo standalone lì). Testo «Rimuovi», non ✕.
4. **Segnaposto «es. 0045/26»** del numero, come la consegna del Go (D-64: il numero scritto è quello dell'etichetta);
   lo standalone scrive «es. 12/26». Il banco non lo confronta.
5. **Link pubblico del lotto uscito:** `indirizzo_pubblico` + `/lotto/<gettone>` (il link dell'endpoint configurato,
   `public_link/1`); senza indirizzo pubblico resta il solo percorso. La pagina pubblica è della 092: fino ad allora il
   link porta a un 404. Nel Go un lotto uscito c'è solo dall'import fino alla 091.
6. **Un lotto che sparisce sotto la pagina** (la pulizia dei vuoti dei giorni prima): un POST torna alla lista con «Lotto
   non trovato.»; lo standalone resta sulla pagina con «Lotto non disponibile.». Caso limite (un lotto vuoto di ieri
   aperto in una scheda del browser mentre un collega apre la lista); nessuna D.
7. **Guasto del database** (lettura del lotto, buste, candidate, aggiunta, togli): pagina d'errore (500) con la riga nel
   registro («lotto non letto», «buste del lotto non lette», «candidate del lotto non lette», «busta non aggiunta al
   lotto», «busta non tolta dal lotto»), come la 088.
8. **Fuori dalla 089:** «📷 Scansiona» (090), «📤 Chiudi e invia» e il riquadro dell'invio (091), la pagina pubblica (092).

## Per il manuale

- Dalla lista «Avviso oreficeria», «Apri» (o «Apri lotto») porta alla pagina del lotto: in alto l'oreficeria, il numero
  del lotto e quando è stato aperto.
- Una busta si aggiunge scrivendo il numero dell'etichetta in «Numero busta» e toccando «Aggiungi», oppure leggendo il QR
  nel campo sotto e toccando «Aggiungi dal QR», oppure con «Aggiungi» accanto a una busta dell'elenco «Pronte di
  <oreficeria> non ancora in lotto».
- Entrano solo le buste pronte di quell'oreficeria, non ancora avvisate e in nessun altro lotto. Una busta già avvisata
  singolarmente resta nell'elenco con la scritta «Già avvisata singolarmente» e «Apri busta»: per metterla nel lotto bisogna
  prima azzerare l'avviso dal dettaglio.
- «Rimuovi» toglie la busta dal lotto e la rimette fra le pronte.
- Un lotto già inviato non si può più cambiare: la pagina dice quando è partito e mostra il link pubblico.
- Dalla scheda di una busta che è in un lotto aperto, «Apri il lotto» porta alla pagina del lotto.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-089/prove/`): `rosso-dominio.log` (17 test su 17 rossi, con
  le cinque funzioni del dominio in stub), `rosso-web.log` (8 su 8 rossi, con le rotte spente e il link della scheda tolto).
- **Dominio** (`internal/buste/lotto_buste_test.go`): per id, numero (con gli zeri, spazi) e QR, precedenze QR > numero >
  id, istante, audit con attore e `repair_id`; rifiuti senza busta (vuoti, che non c'è, id non numerico, negativo, cestino,
  QR che non ripiega sul numero, lotto che non c'è prima di tutto) senza scritture; già avvisata da ogni porta, altra
  oreficeria, diretta, non pronta, compro oro valutata, con i testi; due volte; in un altro lotto aperto e uscito (e quel
  lotto non la lascia togliere); avvisata e in lotto uscito: vince «già avvisata», azzerato l'avviso il lotto uscito;
  lotto inviato o chiuso; **la scrittura decide** (gancio in mezzo, 8 casi); **gare**: 12 aggiunte della stessa busta (un
  membro, un audit, 11 «già in questo lotto»), 12 su due lotti (un solo vincitore, gli altri il suo lotto), aggiunta e
  avviso singolo insieme su 8 buste (per ognuna vince uno solo, mai tutti e due, chi perde ha il suo rifiuto). Le gare
  girano sull'unica connessione di scrittura, quindi provano l'invariante finale (un solo vincitore, un solo audit); la
  finestra fra lettura e scrittura la prova il gancio; avviso
  singolo rifiutato per una busta del lotto aperto; audit nella transazione (trigger) per aggiunta e togli; numero di più
  buste (D-64/D-95: anno in corso, con l'anno, «45» scritto due volte con una 45/25 pronta → «già in questo lotto»,
  gemelle → rifiuto senza scritture anche con una avvisata, l'avvisata sola → «già avvisata», nessuna pronta → la più
  recente col suo rifiuto); togli (istante, audit, non nel lotto, che non c'è, cestino, di nuovo, lotto inviato e
  chiuso); candidate (ordine, segno, esclusioni); lotto e membri (stati, link spento, «Lotto non trovato.», cestino).
- **Pagina** (`internal/web/lotto_test.go`): markup vuoto e pieno (escape, taglio a 50 grafemi, ordine delle candidate,
  avvisata con «Apri busta» senza «Aggiungi»); lotto che non c'è e id non validi; le tre porte con il messaggio e i campi
  vuoti, audit con l'utente, 403 senza token; 12 rifiuti (moduli forgiati compresi) con il numero o il QR rimessi, «già in
  questo lotto» come esito, id di candidata non valido; «Rimuovi» e i suoi rifiuti; lotto inviato in sola lettura (orologio
  d'inverno e d'estate, link pubblico, aggiunta e togli da una pagina vecchia, link storico spento); «Apri il lotto» dalla
  scheda; i cinque guasti.
- **Banco (solo controlli senza istanza):** `go test ./tools/banco` verde; `node --check` su `banco.mjs`.

## Mutazioni

Revisore Opus (`to-be-deleted-after-use/review-089/mutazioni.py`): 36 mutazioni, 35 compilate (la 29, `web-altro-500`, non
compila), **28 uccise**. Sopravvissute:

- **Equivalenti (2).** La 14 (`id-cestino`, la lettura per id vede anche il cestino): l'UPDATE ha `cancellata_il IS NULL` e
  `lottoRifiuto` legge solo fuori dal cestino, quindi la busta cestinata è comunque «Riparazione non trovata.». La 26
  (`Modificabile` guarda solo `inviato_il`): il CHECK della 0007 vuole `inviato_il` per ogni lotto chiuso, come la 04 della 088.
- **Chiuse dai test nuovi (5)**, ognuna rimessa a mano e vista rossa, poi ripristinata (`prove/mutazioni-review.log`): la 09
  (`rif-gia-dopo-aperto`: una busta in un lotto inviato, riaggiunta da una pagina vecchia, è «Lotto non disponibile.»), la 11
  (`rif-oreficeria-prima-compro`: una compro oro valutata di un'altra oreficeria è «altra oreficeria») e la 17
  (`buono-senza-oreficeria`: il numero che mostra anche una busta pronta di un'altra oreficeria aggiunge quella giusta),
  tutte in `TestAggiungiAlLottoCasiDellaReview`; la 23 (`candidate-ordine-id`) in `TestCandidatePerDataDiPronta` (id alto,
  data di pronta vecchia, in fondo); la 32 (`web-sottomappa`: `bag[numero_busta][x]` insieme a `bag[numero_busta]`) in
  `TestRifiutiDellaPaginaDelLotto`.

## Revisione

Revisore Opus indipendente, due fasi. Nessun rilievo ALTO.

- **MEDIA** (`lottoBuono`): fra più buste con lo stesso numero le «buone» escludevano quella già in questo lotto, quindi «45»
  riscritto con una 45/25 pronta aggiungeva in silenzio la 45/25. Chiusa: `lottoBuono` sceglie per stato (pronta, non
  compro oro, dell'oreficeria, fuori dal cestino), come `aggiungibile` della consegna; test «45 scritto due volte».
- **BASSE (4)**, chiuse: test web dei rifiuti «in un altro lotto» e «più buste»; tolto il ramo ridondante `&piu` in
  `lotto.go`; in ricevuta le gare provano l'invariante finale e la finestra la prova il gancio; chiarita la frase su
  «Riparazione non trovata.».
- **Giudizio:** sì alla D-95 e alla D-96. Nota del revisore: il suggerimento «scrivi il numero con l'anno» del rifiuto «Più
  buste corrispondono…» non aiuta con due buste dello stesso anno (gemelle), come per la D-89: lì serve il QR.
- Seconda fase senza rilievi sulle correzioni.

## Controlli

`scripts/check` verde (implementatore, `prove/check-2.log` e, dopo le correzioni di s089a, della review e delle mutazioni, `prove/check-3.log` e `prove/check-4.log`; il primo giro, `prove/check.log`, si era fermato su gitleaks per un
gettone di prova in `lotto_test.go` che sembrava una chiave: rinominato). Copertura senza l'esito del banco: voci 353, chiuse
42, rosse 0, parziali 169, aperte 131, fuori v1 11.

## Banco

- **`s089a`** (16/11/2026, sessione principale): HTTP verde. Browser: le 4 inattese di `profilo-negozio` (la D-93 nota);
  `lotti-buste` `vuoto.errore` e `vuoto.percorso` «(assente)» nelle due app e nelle due viste (con i dati lo scenario non
  passava dal lotto che non c'è, ma le attese di `atteso` valgono anche con i dati): corretto, il passo gira sempre;
  `whatsapp` PC, solo standalone, 500 su `GET /audit` alle 05:44:25 con `database is locked` (`AuditLive.handle_params/3`)
  e poi timeout: è il caso della nota 8 di `docs/domande-aperte.md` (aggiunto lì), non uno scenario della 089 (le pagine
  dei lotti di quella vista erano finite alle 05:38). Dopo s089a la review ha cambiato `lottoBuono` (sopra): da rifare.
- **`s089b`** (16/11/2026, dalle 06:48, codice definitivo): livello HTTP finito e verde (81 scenari, 1293 passi, 0 inattese,
  0 errori). Il livello browser l'ha fermato la guardia alle 07:08:32 (4 GB disponibili: durante il giro era partito
  `strata-host`, circa 24 GB). **Il giro browser sul codice definitivo resta da fare** ed entra nel giro finale unico con
  087 e 088.

Scenari:

- **HTTP `lotto-pagina`** (ORE-06, ORE-09): `/ready-lots/999999` senza accesso (302 a `/login`), l'accesso (302 alla
  pagina chiesta), il lotto che non c'è e `abc` (302 a `/ready-lots`), poi `/ready-lots/1`: sul vuoto 302 alla lista; con i
  dati il lotto della catena degli stati (inviato e chiuso alla consegna della 14/26): conto 1, «· Non più modificabile»,
  niente moduli né candidate, «← Indietro», il link pubblico (solo l'id: l'indirizzo è diverso fra le app), «Questo lotto
  è partito il». `dati_test.go` ammette il suo `atteso_dati`.
- **Browser `lotti-buste`** (ORE-06, ORE-08, ORE-09; sotto, `banco.mjs`): oreficeria propria «Zz Banco 089 <vista>» con
  telefono, cinque buste proprie del 2025 (A-D pronte, E in lavorazione), D avvisata dalla scheda; poi la pagina del lotto,
  A per numero, B per QR, C dalla candidata, A di nuovo, D ed E per QR (rifiuti), numero che non c'è, campo vuoto,
  «Rimuovi» su A, la scheda di B con «Apri il lotto»; registro +3 / +1.
- **Da guardare al primo giro:** (a) che il lotto 1 della cattura sia quello della catena degli stati in tutte e due le
  app (se no, `lotto-pagina[5]` cambia id); (b) in `lotti-buste` la lettura del numero dal messaggio «… creata.» /
  «Busta … salvata» e la ricerca per numero nel Go (`buste.numeri` 5); (c) il popup dell'avviso di D (deve tornare a «/»,
  `avvisa.aperture` 1); (d) nello standalone i flash restano finché uno dello stesso tipo non li sostituisce: lo scenario
  legge i messaggi solo quando sono gli ultimi del loro tipo; (e) che le buste pronte del 2025 lasciate dal PC non
  spostino attese degli scenari dopo (come quella di `sessioni-rientro`).
- **Nessuna riga nuova** in `bench/deviazioni.json` per D-95 e D-96, approvate poi da Stefano il 16/11/2026 (lo scenario non le tocca:
  nessun numero vale per più buste; il QR dopo una riuscita non si guarda).

## Misura

Implementatore Opus: circa 30 minuti più due giri di correzioni (banco s089a e review, mutazioni), circa 410k token del
sottoagente. Revisione Opus: circa 190k token. Giri del banco: 2 (s089a; s089b solo HTTP, il browser fermato dalla guardia). **Browser da rifare** (guardia scattata alle 07:08, strata acceso da un'altra sessione): entra nel giro finale unico (decisione del coordinatore, 16/11).

## Dubbi

1. **Numero scritto che vale per più buste (D-95).** Con la D-64 il numero scritto è quello dell'etichetta, e può
   valere per più buste (anni diversi, gemelle, etichetta e numero interno); lo standalone cerca il numero interno, che è
   unico. Il Go fa come la consegna (D-89). **Raccomandazione:** approvarla. Testo proposto:
   > **D-95 (ORE-06; slice 089).** Il numero scritto nell'aggiunta al lotto d'avviso che vale per più buste (con la D-64
   > il numero scritto è quello dell'etichetta): il Go tiene solo le buste che per il loro stato un lotto di quell'oreficeria
   > prende (pronte, dell'oreficeria, non compro oro; avviso e lotto non contano), poi l'anno in corso, e se ne resta più
   > d'una rifiuta con «Più buste corrispondono a «X»: scrivi il numero con l'anno o usa il QR.» senza aggiungere niente; se
   > nessuna è pronta, la più recente. La busta scelta dice il suo rifiuto («Busta già in questo lotto.», «già avvisata»):
   > un'altra busta con lo stesso numero non entra mai al suo posto. Lo standalone cerca il numero interno, che è unico. Gemella della D-89.
   > Nessuna riga nel banco: nessuno scenario scrive un numero che vale per più buste.

   **Deciso da Stefano il 16/11/2026:** D-95 approvata come proposta; nessuna riga nel banco.
2. **I campi dopo un'aggiunta dal QR (D-96).** Nello standalone, dopo «Aggiungi dal QR», il codice resta scritto
   nel suo campo (anche dopo una riuscita) e il campo «Numero busta» tiene quello che c'era scritto senza inviarlo; nel Go,
   senza LiveView, dopo una riuscita i due campi sono vuoti e dopo un rifiuto il QR resta ma il numero è vuoto (il modulo
   del QR non porta l'altro campo). **Raccomandazione:** tenerlo così (il campo vuoto è pronto per il codice dopo, come
   fa la pistola) e registrarlo come D. Testo proposto:
   > **D-96 (ORE-06; slice 089).** Nella pagina del lotto, dopo «Aggiungi dal QR», il Go svuota il campo del QR quando la
   > busta entra e il campo «Numero busta» sempre (il modulo del QR non lo porta); lo standalone (LiveView) lascia i due
   > campi come li aveva scritti l'operatore. Nessuna riga nel banco: lo scenario non legge i campi dopo un'aggiunta dal QR.

   **Deciso da Stefano il 16/11/2026:** D-96 approvata come proposta; nessuna riga nel banco.
3. **Link pubblico prima della 092** (Scelta 5): fedele allo standalone, ma fino alla 092 nel Go è un 404 e non c'è un
   lotto uscito se non dall'import. **Raccomandazione:** tenerlo (il link compare solo per un lotto già inviato, che nel Go fino alla 091 viene solo dall'import, e la 092 arriva tre slice dopo); niente D.
