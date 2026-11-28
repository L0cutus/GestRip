# 137 — Pagina del cestino e ripristino

- **Fonte:** piano T4.11 approvato da Stefano il 27/11/2026 (`piano-T4.11-bozza.md`, riga 137 della tabella; domande
  **B** (ripristino di titolare e dipendente) e **K** (struttura pronta per le vendite); rischio «una pagina che
  dimentica il cestino»). Decisioni **D-125** (lo stato con le etichette del Go) e **D-126** («Busta non ripristinata.»),
  Stefano 28/11/2026, sui due dubbi della slice. Per il resto chi decide: **lo standalone** a `go-baseline` (`TrashLive`, `Trash.list/1`, `Trash.restore/4`,
  `Trash.owner_only?/1`, `Trash.names/0`, la voce «🗑 Cestino» di `layouts.ex:290`, `trash_live_test.exs`).
- **Voci:** RIP-S-51, RIP-S-52 (parte «buste»; le vendite sono della T4.12, VEN-18). **Dipende da:** 136.
  **Dipendenze nuove:** nessuna. **Migrazioni nuove:** nessuna (`buste.cancellata_il`, il suo indice parziale e
  `vendite.cancellata_il` ci sono già).
- **Modelli:** implementatore Opus (~280k token, ~25 minuti di orologio). Revisore Opus indipendente (~110k token, ~10 minuti):
  1 ALTA, 3 BASSE, tutte corrette (vedi «Revisione»).

## Cosa c'è

- **`buste.Cestino`** (`internal/buste/cestino_pagina.go`): `Trash.list/1` con la finestra di `load/1`. La finestra si
  legge **una volta** (`GiorniCestino`, la lettura della 136: assente, rovinata o fuori da 1–365 → 30), poi le voci di
  ogni famiglia, ordinate per `cancellata_il` decrescente (stabile; a pari istante l'id crescente, come la lettura per
  indice dello standalone). Ogni voce: famiglia, id, istante, numero visibile (D-64: «0045/26» o «N. 45/26»),
  descrizione, stato com'è salvato (la pagina lo scrive con l'etichetta, D-125), giorni rimasti. La lettura non prende il
  token del link del cliente.
- **Giorni rimasti** (`giorniRimasti`, `countdown/3`): `trascorsi = max(oggi − giorno del cestinamento, 0)` e
  `rimasti = max(finestra − trascorsi, 0)`, con le **date del negozio** (`clock.DataDi`, fuso di Roma) e `Data.Sub`, mai
  periodi di 24 ore. Quindi mai negativi (anche un anno dopo) e mai oltre la finestra (un istante nel futuro, un
  orologio tornato indietro); l'ora legale non conta (si contano date). La finestra cambiata vale alla lettura dopo.
- **`buste.RipristinaDalCestino`** (`Trash.restore/4`): la sola scrittura `UPDATE buste SET cancellata_il = NULL,
  aggiornato_il = adesso WHERE id = ? AND cancellata_il IS NOT NULL` decide; tocca solo quelle due colonne. Famiglia
  sconosciuta, id non positivo, busta che non c'è o fuori dal cestino: lo stesso `ErrNonNelCestino`, niente scritto.
  Nella stessa transazione `repair.restored` senza dati; un errore qualsiasi (attore vuoto, audit guasto, gancio prima
  del commit) non ripristina. Nessun limite di tempo (anche dopo la finestra). Il dettato e le iscrizioni Telegram spente
  al cestinamento non tornano, come nello standalone.
- **Pagina** (`internal/web/cestino_pagina.go`, `pagine/cestino.html`, `app.css`): `GET /cestino` con il guscio, per
  titolare e dipendente (`personale`); titolo «Cestino · Laboratorio». La voce «🗑 Cestino» del menu ☰ compare da sola
  con la rotta (`vociMenu`, ultima, come lo standalone). Una riga per voce: cartellino «Busta», numero, «descrizione ·
  stato · cestinato il gg/mm/aaaa hh:mm» (ora del negozio; lo stato con `etichettaStato` come la scheda, «In
  lavorazione», **D-125**), i giorni con «giorni», «Finestra scaduta» a zero, «↩ Ripristina».
  Nessun link alla scheda (lo standalone non ne ha; la scheda di una busta nel cestino non si apre). Gli id dello
  standalone con la famiglia (`trash-repair-7`, `trash-label-…`, `trash-days-…`, `restore-…`, `trash-empty`, `trash-list`).
- **Ripristino** `POST /cestino/repair/{id}/ripristina` (`personale`, CSRF di ogni modulo, `data-confirm`,
  `data-invio-unico`): riuscita → `/cestino` con «Ripristinato.»; niente da ripristinare (id malformato, busta che non
  c'è o non più nel cestino) o guasto → `/cestino` con «Busta non ripristinata.» nello stesso flash (**D-126**; lo
  standalone ignora l'evento, `{:noreply, socket}`); il guasto anche con la riga fissa «elemento non ripristinato dal
  cestino» nel registro, senza numeri, nomi né testo dell'errore.

## Testi (dallo standalone)

| Dove | Testo | Da |
|---|---|---|
| titolo | «🗑 Cestino» (page_title «Cestino») | `trash_live.ex` render/1, load/1 |
| frase | «Quello che viene cestinato resta recuperabile per X giorni. Passata la finestra non sparisce da solo: resta qui, ancora intero, in attesa che il titolare decida.» | `trash_live.ex` `:subtitle` |
| vuoto | «Il cestino è vuoto.» | `#trash-empty` |
| riga | «Busta» · numero · «… · … · cestinato il gg/mm/aaaa hh:mm» · N «giorni» · «Finestra scaduta» | `family_label/1`, `stamp/1` (`Clock.stamp/1`) |
| pulsante | «↩ Ripristina» | `#restore-…` |
| conferma | «Riportare questo elemento fra quelli attivi?» | `data-confirm` |
| riuscita | «Ripristinato.» | `put_flash(:info, …)` |
| rifiuto | «Busta non ripristinata.» | **D-126** (Go; lo standalone non dice niente) |
| menu | «🗑 Cestino» | `layouts.ex:293` |

Differenze volute: il numero è quello visibile della D-64 (lo standalone stampa `bag_number`); lo stato con l'etichetta
della scheda («In lavorazione», D-125; lo standalone `in_lavorazione`); il messaggio del rifiuto (D-126).

## Pronto per le vendite (domanda K)

- Dominio: il cestino è l'elenco `famiglieCestino`; ogni famiglia ha il nome dello standalone (`FamigliaCestino`:
  `"repair"`, poi `"sale"`), la sua lettura (`busteNelCestino`), la sua scrittura del ripristino (`sqlRipristinoBusta`)
  e il ruolo (`SoloTitolare`, `Trash.owner_only?/1`; un nome sconosciuto è del titolare). `Cestino` fonde e ordina le
  famiglie e conta i giorni per tutte; `RipristinaDalCestino` scrive `<famiglia>.restored`.
- Web: una rotta di ripristino **per famiglia** (`registraCestino` gira su `buste.FamiglieCestino()`), con il permesso
  preso da `SoloTitolare`, che decide anche se il pulsante si disegna; il cartellino da `tipiCestino`. Così la tabella dei
  permessi ha una riga per famiglia (una rotta del titolare per le vendite non si mescola a quella di tutti).
- La T4.12 aggiunge: la famiglia `"sale"` (solo titolare, lettura da `vendite`, `UPDATE vendite …`), «Vendita» in
  `tipiCestino`, la riga `POST /cestino/sale/{id}/ripristina` (soloTitolare) in `tabellaPermessi` al posto di «evento
  restore di una vendita (TrashLive)». Ordine, giorni, pagina e test dei giorni non cambiano. L'eliminazione definitiva
  (139–140) usa le stesse famiglie.

## Il test statico

`TestFiltroDelCestinoInOgniQuery` resta verde con **due eccezioni nuove**, volute:

- `internal/buste/cestino_pagina.go:Buste.busteNelCestino` — la pagina legge proprio le buste nel cestino
  (`cancellata_il IS NOT NULL`, `discarded/1`);
- `internal/buste/cestino_pagina.go:const sqlRipristinoBusta` — il ripristino ha la guardia `cancellata_il IS NOT NULL`:
  la busta deve essere nel cestino.

## Prove

`CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -tags nodynamic` su `internal/buste`, `internal/web`, `internal/db`,
`tools/banco`: verdi.

- `internal/buste/cestino_pagina_test.go`: `TestCestinoOrdine` (vuoto: nessuna voce e 30; poi dal più recente, a pari
  istante l'id crescente, la busta fuori dal cestino assente, numero «0045/26» e «N. …», descrizione, stato salvato,
  istante; quella buttata adesso in cima), `TestGiorniRimasti` (14 casi: il giorno stesso, un minuto dopo mezzanotte,
  il fuso nei due sensi, l'ora legale di marzo e ottobre e il giorno di 23 ore, l'ultimo giorno, la finestra appena
  passata, un giorno e un anno oltre, nel futuro, finestra di 1 e di 365), `TestCestinoGiorniDallaPagina` (orologio e
  database: la sera stessa, dopo la mezzanotte di Roma ancora il 20 in UTC, la finestra cambiata a 5, 3, 2, 1, 365 e
  rovinata), `TestRipristinaDalCestino` (dopo la finestra; riga intera uguale a prima tranne `aggiornato_il`; audit
  dell'autore con IP e `{}`; lista, «Pronte», ricerca, cestino, scheda e link del cliente di nuovo; le colonne facoltative piene (ritardo, sollecito, link inviato, email…) restano; la seconda volta, una
  busta fuori dal cestino, 0, −1, inesistente, «sale», «», «REPAIR»: `ErrNonNelCestino` e database identico),
  `TestRipristinaDalCestinoTuttoONiente`, `TestFamiglieCestino`.
- `internal/web/cestino_pagina_test.go`: `TestPaginaCestinoVuota` (titolare e dipendente, titolo, frase, «Il cestino è
  vuoto.», voce del menu), `TestPaginaCestinoRighe` (HTML esatto di tre righe in ordine, ora di Roma, stato con l'etichetta (D-125), a 1 giorno senza e a 0 con «Finestra
  scaduta», finestra a 60; niente link alla scheda, niente token né `/r/`, la busta fuori dal cestino assente),
  `TestPaginaCestinoSenzaToken` (il token in chiaro e codificato, dopo aver aperto la scheda), `TestPaginaCestinoRipristina`
  (titolare e dipendente: «Ripristinato.», cestino vuoto, audit dell'utente, home, scheda; secondo POST con «Busta non ripristinata.»
  e senza righe), `TestPaginaCestinoRifiuti` (id malformati, inesistente, fuori dal cestino, tutti con «Busta non ripristinata.», D-126; senza CSRF e CSRF falso 403;
  `/cestino/sale/…` senza rotta; guasto con `RAISE(ABORT, 'guasto-prova-137')`: «Busta non ripristinata.», niente ripristinato, registro senza
  numero, nome né errore), `TestTipiCestinoPerOgniFamiglia` (ogni famiglia ha il suo cartellino),
  `TestPaginaCestinoEtichettaStato` (D-125).
- Ritoccati: `ruoli_test.go` (righe «live /cestino (TrashLive)» e «evento restore di una busta (TrashLive)», `personale`),
  `guscio_test.go` (la voce «🗑 Cestino» nei menu di titolare e dipendente, `GET /cestino` fra le rotte con il guscio, non
  più fra le rotte finte di `TestMenuCresceConLeRotte`), `lotti_storico_test.go` (cercava «Cestino» nella pagina, ora c'è
  nel menu: cerca la riga della busta), `nuova_test.go` (`busteFinte`), `filtro_cestino_test.go` (le due eccezioni),
  `tools/banco/browser_test.go` (voci e attese dello scenario).

`PATH=$HOME/sdk/go1.27.3/bin:$PATH scripts/check`: **verde** (voci 353: chiuse 57, rosse 0, parziali 196, aperte 89,
fuori v1 11; RIP-S-51 e RIP-S-52 passano da aperte a parziali; segreti e dati veri OK; senza `GESTRIP_BANCO_ESITO`).

## Mutazioni

Fatte dall'implementatore con uno script (testo cambiato, test mirati di `internal/buste` o `internal/web`, file rimesso
e impronta SHA-256 confrontata): **32 valide, 31 uccise, 1 equivalente** prima della revisione, più **9 su 9 uccise**
sulle correzioni e su D-125/D-126 (da D1 nella tabella) (M6: `id < 1` → `id < 0`; l'id 0 non trova righe
e dà lo stesso rifiuto). Cinque al primo giro non si applicavano o non compilavano (M1, M2, M7, W7, T2): rifatte, uccise.
Script e log in `to-be-deleted-after-use/_slice-137/`.

| # | Mutazione | Uccisa da |
|---|---|---|
| M1 | ripristino senza `AND cancellata_il IS NOT NULL` | `TestRipristinaDalCestino` |
| M2 | `aggiornato_il` con un istante fisso | `TestRipristinaDalCestino` |
| M3 | righe toccate non controllate | `TestRipristinaDalCestino` |
| M4 | audit `repair.restore` | `TestRipristinaDalCestino`, `…TuttoONiente` |
| M5 | senza il gancio prima del commit | `…TuttoONiente` |
| M7 | famiglia sconosciuta accettata | `TestRipristinaDalCestino` |
| M8, M9 | `SoloTitolare` sbagliato (sconosciuta di tutti, buste del titolare) | `TestFamiglieCestino` |
| M10 | ordine crescente | `TestCestinoOrdine` |
| M11 | trascorsi negativi ammessi | `TestGiorniRimasti` |
| M12 | rimasti negativi ammessi | `TestGiorniRimasti`, `TestCestinoGiorniDallaPagina`, `TestRipristinaDalCestino` |
| M13 | giorno del cestinamento in UTC | `TestGiorniRimasti` |
| M14 | oggi in UTC | `TestCestinoGiorniDallaPagina` |
| M15 | finestra fissa a 30 | `TestCestinoGiorniDallaPagina` |
| M16 | a pari istante l'id decrescente | `TestCestinoOrdine` |
| M17 | numero interno invece di quello visibile | `TestCestinoOrdine` |
| M18 | il token al posto della descrizione | `TestCestinoOrdine` |
| M19 | la lettura senza `cancellata_il IS NOT NULL` | `TestCestinoOrdine`, `TestRipristinaDalCestino` |
| W1 | «Ripristinata.» | `TestPaginaCestinoRipristina` |
| W2 | dopo il ripristino alla home | `TestPaginaCestinoRipristina`, `…Rifiuti` |
| W3 | guasto senza riga nel registro | `TestPaginaCestinoRifiuti` |
| W4 | pulsante solo al titolare | `TestPaginaCestinoRighe`, `…Rifiuti` |
| W5 | rotta del ripristino solo del titolare | `TestOgniRottaHaIlSuoPermesso`, `TestPaginaCestinoRipristina` |
| W6 | «Finestra scaduta» mai | `TestPaginaCestinoRighe` |
| W7 | «cestinato il» in UTC | `TestPaginaCestinoRighe` |
| W8 | un guasto dice «Ripristinato.» | `TestPaginaCestinoRifiuti` |
| W9 | la pagina senza voci | `TestPaginaCestinoRighe`, `…SenzaToken`, `…Rifiuti` |
| T1 | pulsante senza `data-confirm` | `TestPaginaCestinoRighe` |
| T2 | testo del cestino vuoto | `TestPaginaCestinoVuota` |
| T3 | frase con 30 fisso | `TestPaginaCestinoRighe` |
| T4 | pulsante mai disegnato | `TestPaginaCestinoRighe`, `…Rifiuti` |
| D1 | stato com'è salvato (D-125) | `TestPaginaCestinoRighe` |
| D2, D3, D6 | rifiuto o guasto senza «Busta non ripristinata.», o come errore invece che nel flash di «Ripristinato.» | `TestPaginaCestinoRifiuti`, `…Ripristina` |
| D4 | testo del rifiuto | `TestPaginaCestinoRifiuti`, `…Ripristina` |
| D5 | id malformato dato per riuscito | `TestPaginaCestinoRifiuti` |
| R5 | il ripristino svuota `ritardo_avvisato_il` e `sollecito_avvisato_il` (del revisore) | `TestRipristinaDalCestino` |
| R8 | «Finestra scaduta» con `<= 1` (del revisore) | `TestPaginaCestinoRighe` |
| R9 | `tipiCestino` senza le buste | `TestTipiCestinoPerOgniFamiglia`, `TestPaginaCestinoRighe` |

## Banco

**Scenario `cestino` esteso** (`bench/browser/cestino.json`, `cestino` e la nuova `paginaCestino` in
`tools/banco/browser/banco.mjs`). Dopo lo spostamento della 136, sulla stessa busta della vista:

- **Confrontati** (tutte e due le app, PC e telefono): `/cestino` (percorso, frase con 30 giorni, nessun «vuoto»), la riga
  della busta (una; numero `<numero>`, D-64; «Anello del cestino 136 <vista>»; `in_lavorazione`; «cestinato il» nella
  forma gg/mm/aaaa hh:mm, l'ora non si confronta; «Busta»; 30 giorni; niente «Finestra scaduta»; «↩ Ripristina»), il
  ripristino (la domanda, «Ripristinato.», nessun errore, resta su `/cestino`, la riga sparita, una `repair.restored`),
  la busta tornata (ricerca 1, contatore «Lavor.» com'era, la scheda con «🗑 Sposta nel cestino»), poi **di nuovo nel
  cestino** dalla scheda («Busta spostata nel cestino.»), così gli scenari dopo trovano il database come con la 136. Sul
  database vuoto: «Il cestino è vuoto.».
- **`solo_go` nuovo:** `cestino.stato` («In lavorazione» nel Go, «in_lavorazione» nello standalone: D-125). Il
  ripristino: il Go con un POST e il redirect a `/cestino`, lo standalone con un evento LiveView sulla stessa pagina,
  stesso percorso e stesso messaggio. Il rifiuto (D-126) lo scenario non lo esercita: lo provano i test Go.
- **Correzione della revisione (ALTA):** `vuoto.testo` (del database vuoto) è in `atteso` e passa anche ai dati
  sintetici, dove `cestino()` non lo legge: ora `"vuoto.testo": "(assente)"` in `atteso_dati` per le due viste.
- **Non nel banco:** le buste importate già nel cestino dei dati sintetici (si legge solo la riga di questa busta), i
  giorni ai bordi (test Go), il token (test Go; nel banco il Go non mostra il link, D-46).
- Controllato con `node --check` e i test di `tools/banco` (`TestScenariBrowserDelRepo` con le voci e le attese nuove).

Esito: **verde al secondo giro** (s137b, 28/11; il primo, s137a, è caduto solo per la nota 8). Configurazione:
`gestrip-standalone:fab-225`, `sintetici-import`, binari `gestrip` e `banco` compilati dal working tree
(`CGO_ENABLED=0 -tags nodynamic`), «memoria libera» del coordinatore; guardia sotto 10 GB mai scattata, minimo
18 332 460 kB (17,5 GiB) di `MemAvailable`.

- **s137a (primo giro):** HTTP verde (91 scenari, 1373 passi, 0 inattese, 0 errori). Browser rosso: 8 inattese e 2
  errori, tutti in due scenari del telefono, `sessioni-rientro` e `storico-correzioni`: lo standalone ha risposto 500 a
  `GET /repairs/83` e `GET /repairs/90` dopo 5 s con `(Exqlite.Error) Database busy` (registro dello standalone),
  quindi `fine.artigiano` assente e il `waitFor` della fase finale scaduto. È la nota 8: niente di Go, niente di
  `cestino`, che era già verde nelle due viste. Un rigiro su un'istanza nuova, come vuole la regola.
- **s137b, HTTP:** 91 scenari, 1373 passi, 0 inattese, 0 errori, «BANCO VERDE».
- **s137b, browser:** 40 scenari, 80 viste, 49 differenze attese, **0 inattese**, 0 da decidere, 0 errori,
  «BANCO BROWSER VERDE». Nessun «Database busy» nel registro dello standalone.
- **`cestino` verde** sulle due viste in tutti e due i giri: tutti i campi confrontati sono uguali nelle due app e alle
  attese (pagina `/cestino`, la riga della busta, il ripristino e la busta tornata, di nuovo nel cestino). Le uniche
  differenze sono i `solo_go` `cestino.stato` (standalone «in_lavorazione», D-125) e `di-nuovo.percorso` (standalone
  `/repairs/N`, Go `/`). Ricetta e scenario non sono stati corretti.
- **`scripts/check` con `GESTRIP_BANCO_ESITO` (esito s137b, salvato fuori dall'istanza prima di `banco giu`):** verde;
  voci 353, chiuse 237, **rosse 0**, parziali 16 (aperte 89, fuori v1 11).
- **`banco giu`** di s137a e s137b senza avanzi: nessun container, rete o volume, cartelle delle istanze tolte.
- Nessuna PNG del manuale toccata (impronte uguali prima e dopo).

## Per il manuale

Una riga in `docs/manuale/da-scrivere.md` per il capitolo 12 (146): la pagina, i giorni, «Finestra scaduta» che non
cancella, il ripristino e cosa non torna (dettato, notifiche Telegram).

## LSP

4 chiamate a gopls: 1 utile (`findReferences` su `vaiAConErrore`: i redirect con messaggio di tutte le azioni, per
scegliere il ritorno senza messaggi del ripristino), 3 rifiutate dal server («column is beyond end of line» su
`guscio.go` e due volte su `cestino.go`). Il resto con grep e lettura.

## Revisione

Revisore Opus indipendente (~110k token, ~10 minuti): **1 ALTA, 3 BASSE, tutte corrette.** Mutazioni del revisore: 16,
13 uccise; R1 equivalente; R5 e R8, sopravvissute, ora uccise dalle correzioni. Nota del revisore: il ripristino non
riaccende le iscrizioni Telegram né riporta il dettato, come lo standalone.

1. **ALTA, `bench/browser/cestino.json`:** con i dati sintetici lo scenario falliva in tutte e due le viste
   (`vuoto.testo` copiato da `atteso` da `attesePerBrowser`, mai letto da `cestino()` in quella modalità).
   **Corretta:** `"vuoto.testo": "(assente)"` in `atteso_dati`. Un test che se ne accorga da solo **non c'è**: la
   regola «ogni attesa di `atteso` è ripetuta in `atteso_dati`» non vale per gli altri scenari (molti ereditano apposta
   `pagina.*`), e sapere quali campi legge la ricetta richiederebbe di analizzare `banco.mjs`. Resta il giro del banco.
2. **BASSA, `TestRipristinaDalCestino`:** le colonne facoltative erano vuote, quindi un ripristino che le svuotava
   passava (R5). **Corretta:** riempite prima del cestino, con un controllo che lo siano.
3. **BASSA, `TestPaginaCestinoRighe`:** nessuna riga a 1 giorno, quindi `<= 1` per «Finestra scaduta» passava (R8).
   **Corretta:** una riga a 1 giorno senza il cartellino.
4. **BASSA:** nessun test che ogni famiglia abbia il cartellino in `tipiCestino`. **Corretta:**
   `TestTipiCestinoPerOgniFamiglia`.

Dopo la revisione, le due decisioni di Stefano sui dubbi della slice: **D-125** (lo stato con `etichettaStato`) e
**D-126** («Busta non ripristinata.» nello stesso flash di «Ripristinato.»), con test e mutazioni.

## Dubbi

Nessuno: i due della slice li ha decisi Stefano il 28/11/2026 (D-125, D-126).

## Seconda revisione (Opus, sola lettura, ~15k token, ~3 minuti)

Correzioni e D-125/D-126 a posto; nessuna ALTA né MEDIA. BASSE: commenti dei test ancora «senza messaggi» (corretti);
`TestPaginaCestinoEtichettaStato` ridondante (lasciato). **Per la T4.12:** `msgNonRipristinata` dice «Busta…» per ogni
famiglia (una vendita rifiutata direbbe «Busta non ripristinata.») e il rifiuto esce nel flash informativo, come la
riuscita: la D-126 non dice quale flash; da chiedere a Stefano con le vendite.
