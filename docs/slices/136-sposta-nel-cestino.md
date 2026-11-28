# 136 — Sposta nel cestino (e filtro del cestino controllato)

- **Fonte:** piano T4.11 approvato da Stefano il 27/11/2026 (`piano-T4.11-bozza.md`, riga 136 della tabella; domande
  **B** e **L**, nota 11 di `docs/domande-aperte.md`; rischio «una pagina che dimentica il cestino»; **incongruenza 1**).
  Chi decide: **lo standalone** a `go-baseline` (`Trash.discard/4`, `Trash.Config.retention_days/0`,
  `Training.forget_repair/2` ed `expire/1`, l'evento `trash` e il pulsante `#trash-repair` di `RepairLive`).
  Decisione **D-110** per il giro giornaliero.
- **Voci:** RIP-S-49, RIP-S-50, RIP-H-51 (parte «cestino»). **Dipende da:** niente. **Dipendenze nuove:** nessuna.
  **Migrazioni nuove:** nessuna (`buste.cancellata_il` e il suo indice ci sono già; il trigger delle iscrizioni Telegram è
  della 0014).
- **Modelli:** implementatore Opus (~450k token, ~34 minuti di orologio). Revisore Opus indipendente (~150k token, ~10 minuti):
  nessuna ALTA, 1 MEDIA, 3 BASSE, tutte corrette (vedi «Revisione»).

## Cosa c'è

- **`buste.SpostaNelCestino`** (`internal/buste/cestino.go`): la sola scrittura `UPDATE buste SET cancellata_il = adesso,
  aggiornato_il = adesso WHERE id = ? AND cancellata_il IS NULL` decide; una busta che non c'è, già nel cestino o un id
  non positivo è lo stesso `ErrBustaNonTrovata`. Nella stessa transazione: `DELETE FROM dettati_addestramento WHERE
  busta_id = ?` (D-110, domanda L) e `repair.trashed` senza dati (come lo standalone, che non ha metadati). Un errore
  qualsiasi (l'audit, un attore vuoto, il gancio prima del commit) non sposta niente. Le iscrizioni Telegram le spegne il
  trigger `iscrizioni_telegram_spente_nel_cestino`. Niente ruolo: la rotta è `personale`.
- **`buste.GiorniCestino`**: `trash.retention_days` come `retention_days/0` (assente, non un intero per Integer.parse/1,
  fuori da 1–365 → 30). La scheda lo legge (`Scheda.GiorniCestino`). La pagina dell'impostazione è della 138.
- **Scheda** (`internal/web/cestino.go`, `scheda.html`, `app.css`): la sezione `#repair-trash` in fondo, dopo il riquadro
  del link (come lo standalone), per titolare e dipendente, con un modulo POST a `/repairs/{id}/cestino` (CSRF, segno
  della scheda, `data-invio-unico`) e `data-confirm`.
- **Campanella:** nel Go si legge a ogni pagina con il filtro (`LeggiCampanella`), quindi la pagina dopo non conta più la
  busta: niente da annunciare (lo standalone chiama `Notifications.announce/0` dopo il commit).
- **Giro giornaliero** (`ScadenzaDettati`): `DELETE … WHERE creato_il < ? OR busta_id IN (SELECT id FROM buste WHERE
  cancellata_il IS NOT NULL)`, come `expire/1`, dentro la stessa transazione e **sotto gli stessi blocchi** di prima.
- **Test statico del filtro** (`internal/db/filtro_cestino_test.go`), vedi sotto. Regola nuova in `docs/SVILUPPO.md`
  («Cestino: spostamento e filtro»).

## Testi (dallo standalone)

| Dove | Testo | Da |
|---|---|---|
| pulsante | «🗑 Sposta nel cestino» | `repair_live.ex:1831` |
| conferma | «Spostare la busta N nel cestino? Resta recuperabile per X giorni.» | `repair_live.ex:1829` (N è il numero visibile della D-64: «#0045/26» o «N. 45/26»; lo standalone scrive `bag_number`) |
| riuscita | «Busta spostata nel cestino.» (home) | `repair_live.ex:442` |
| rifiuto | «Busta non spostata nel cestino.» | `repair_live.ex:446` |

Differenze volute: una busta che non c'è o già nel cestino porta alla **home** con il rifiuto (lo standalone lo dà sulla
scheda LiveView, che nel Go non si ridisegna per una busta nel cestino); un guasto torna alla scheda con lo stesso testo e
una riga fissa nel registro, senza numeri.

## Dettati e blocco dopo l'import (D-110, domanda L)

- **Spostamento a mano:** il dettato se ne va subito, **anche dopo un import senza backup verificato**. Il blocco della
  D-110 e di `db.BloccatoDopoImport` vale per i lavori automatici (conservazione dell'audit, coda dei media, scadenza dei
  dettati: `findReferences` su `PreparaBloccoImport`), non per le azioni delle persone: anche l'esclusione di un cliente
  (114, 115) cancella i dettati senza blocco. Il dettato non torna con il ripristino (come lo standalone).
- **Giro giornaliero:** i dettati delle buste già nel cestino (le importate così) se ne vanno al giro, ma solo quando il
  giro può cancellare: dopo un import niente finché non c'è un backup verificato sulla chiavetta dopo `importato_il`, e
  niente con l'orologio andato avanti oltre il margine («non cancella niente», D-110 punti 1 e 2). Il limite dei 12 mesi
  (punto 3) vale solo per l'età. Una riga `assist.training_expired` con il numero di tutti e due insieme.

## Il test statico (incongruenza 1)

`TestFiltroDelCestinoInOgniQuery` legge il sorgente Go di `internal/` e `cmd/` (non i test), prende ogni stringa o
concatenazione con `+` e conta i riferimenti a `buste` (FROM, JOIN, UPDATE, DELETE FROM; non INSERT INTO) e i filtri
`cancellata_il IS NULL`. Risolve variabili locali (l'assegnazione con meno filtri: un `cestino := "…IS NULL"` che un ramo
cambia in `"1"` vale `"1"`), parametri (la chiamata con meno filtri), costanti del pacchetto, e le funzioni del pacchetto
che costruiscono il WHERE dal loro risultato (`return`, `strings.Join`, `append`: `Buste.dove` → `condizioni`). Con meno filtri che riferimenti il pezzo va in
`eccezioniFiltroCestino` con il numero esatto per funzione e il motivo; un'eccezione in più o in meno fa fallire.
Le funzioni che ricevono il WHERE da chi chiama (`funzioniConWhere`: `leggiBustaEmail`, `paginaDove`, `bustaStoricaDove`,
`righeDellEtichettaCon`, `bustaPerNumeroScelta`) hanno la query fra le eccezioni, quindi il test controlla ogni loro
chiamata (dalla revisione): un argomento senza filtro, o `true` per la scelta del cestino, va nell'elenco con la chiave
«file:Chiamante→funzione»; oggi 7 chiamanti (3 di `leggiBustaEmail` su 9, 2 di `bustaStoricaDove`, uno per la D-109 in
`bustaPerNumeroScelta` e in `righeDellEtichettaCon`). Una funzione dell'elenco senza chiamate fa fallire.
`TestFiltroDelCestinoRiconosce` prova lo strumento su un sorgente inventato. Limiti scritti in SVILUPPO: `Sprintf`, il
filtro dentro un commento SQL, le query sulle sole tabelle figlie.

**Esito sul codice di oggi:** 69 funzioni, 78 pezzi senza filtro, **tutti voluti** (letti uno per uno); nessuno in `internal/web`,
`internal/assist`, `internal/jobs` (i ritardi passano da `predicatoRitardo`, risolto). Le eccezioni, per motivo:

- **busta già letta fuori dal cestino nella stessa transazione** (o appena prima, per id): 30 funzioni (attesa, avvisi,
  firma, fissa, link, modifica, preventivo, QR, rientro, riapertura, stati, email, scheda, consegna, lotto…);
- **RIP-S-50, genitori per il ripristino:** `EliminaArtigiano`, `PulisciConsegneVuote`, `PulisciLottiVuoti`,
  `condInRaccolta` (con `ApriConsegna`, `ConsegneInRaccolta`), `InviaLotto`, `rifiutoInvio`, `chiudiAllaLettura`,
  `chiudiLottoCompleto`, `EliminaDalloStorico`;
- **cestino compreso apposta:** D-109 (`bustaPerNumeroScelta` con `conCestino`, `righeDellEtichettaCon`,
  `bustaDellaRaccolta`), l'opposizione (`escludiCliente`), D-99 (`rifiutoMembro`), D-02 (`MandaLinkDisattivazione`,
  `leggiBustaEmail` per le disattivazioni), l'esportazione dei dettati (scarta le buste nel cestino), il giro dei dettati
  (`ScadenzaDettati`, nuovo);
- **link storici:** `bustaStoricaDove` legge `cancellata_il` e dà il 404 (064, 135);
- **import:** `riscriviBuste`, `numeriSenzaZeri`, `codiciNormalizzati`; **numerazione:** `massimoDellAnno`;
- **ricostruzione dei giri** (migrazione): `sqlGiriAperti`, `sqlRientriNeiGiri`, `sqlRicostruzioneRientri`,
  `rientroNeiGiri`; **frammenti:** `colonneBustaEmail`;
- **Telegram:** `ConsensoTelegram` (il trigger ferma le iscrizioni al cestino), `FermaChatTelegram`, `Coda.bloccaChat`
  (il numero solo per l'audit);
- **numero accanto a un messaggio già in coda** (solo titolare): `Coda.righe` (pagina dei provider) e `messaggiInCoda`
  (conferma del ripristino) — D-123.

## Prove

`CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -tags nodynamic` su `internal/buste`, `internal/web`, `internal/db`,
`internal/jobs`, `internal/media`, `tools/banco`: verdi.

- `internal/buste/cestino_test.go`: `TestSpostaNelCestino` (data del cestino e `aggiornato_il` adesso, dettato della busta
  via e quello dell'altra no, la riga `repair.trashed` con attore, risorsa, id, `{}` e IP, la scheda «non trovata»; la
  seconda volta, id 0, −1 e inesistente: rifiuto e database identico; l'orologio avanza di un minuto prima, così
  `aggiornato_il` deve cambiare), `…SenzaDettato`, `…TuttoONiente` (attore vuoto,
  audit guasto, errore prima del commit), `…SparisceDalleListe` (lista, totale, «Pronte», «Messaggi», ricerca,
  campanella, briefing, `/r/` della busta: tutto uno in meno; la pagina `/r/` dell'altra si apre), `TestGiorniCestino`
  (15 valori, la scheda), `TestRaccoltaScadenzaBusteNelCestino`, `…BusteNelCestinoBloccate` (import senza backup,
  orologio avanti, poi il backup verificato), `TestSpostaNelCestinoDopoImport`.
- `internal/buste/raccolta_scheda_test.go`: `TestRaccoltaScadenzaTuttoONiente` ritoccato (diceva che il dettato giovane
  della busta nel cestino restava «per la T4.11»): ora se ne va con il giro, e con l'audit guasto restano tutti e due.
- `internal/web/cestino_test.go`: `TestSchedaCestinoPulsante` (HTML esatto per titolare e dipendente, dopo il riquadro
  del link, numero visibile «N. 1/26» e «#0045/26», 30 e 7 giorni), `TestSchedaCestinoSposta` (titolare e dipendente:
  home con il messaggio, campanella da 2 a 1, la busta fuori dalla lista, audit dell'utente, dettati, scheda «Riparazione
  non trovata.», il secondo POST rifiutato senza righe), `TestSchedaCestinoRifiuti` (id malformati e inesistente, senza
  CSRF e con CSRF falso 403, guasto dell'audit con `RAISE(ABORT, 'guasto-prova-136')`: scheda con il messaggio, niente spostato, registro senza
  numero, nome né testo dell'errore).
- Ritoccati: `ruoli_test.go` (la riga «evento trash (RepairLive)», `personale`, RIP-S-49), `segni_test.go` (il modulo
  `cestino` porta il segno: 11 moduli), `nuova_test.go` (`busteFinte.SpostaNelCestino`).
- `internal/db/filtro_cestino_test.go`: i due test statici.

`PATH=$HOME/sdk/go1.27.3/bin:$PATH scripts/check`: **verde** (voci 353: chiuse 57, rosse 0, parziali 194, aperte 91,
fuori v1 11; RIP-S-49 e RIP-S-50 passano da aperte a parziali; segreti e dati veri OK; senza `GESTRIP_BANCO_ESITO`).

## Mutazioni

Fatte dall'implementatore con uno script (testo cambiato, test mirati di `internal/buste`, `internal/web` o
`internal/db`, file rimesso e confrontato byte per byte con la copia; alla fine le impronte di tutti i file della slice
confrontate con quelle di prima): **41 valide, 41 uccise** (32 prima della revisione, 9 sulle correzioni). Tre non compilavano (M10b, M23, M33: non contano; M10b e M23
rifatte come M10c e M23b). Una equivalente nel primo giro (l'espressione regolare davanti a `strconv.ParseInt` non serviva:
tolta dal codice, con due valori in più nel test). Script e log in `to-be-deleted-after-use/_slice-136/`.

| # | Mutazione | Uccisa da (fra gli altri) |
|---|---|---|
| M1 | UPDATE senza `AND cancellata_il IS NULL` | `TestSpostaNelCestino` |
| M2 | righe toccate non controllate | `TestSpostaNelCestino` |
| M3 | il dettato della busta non si cancella | `TestSpostaNelCestino`, `…DopoImport` |
| M4 | si cancellano i dettati di tutte le buste | `TestSpostaNelCestino` |
| M5 | niente audit | `TestSpostaNelCestino`, `TestSchedaCestinoSposta` |
| M6 | audit `repair.discarded` | `TestSpostaNelCestino`, `TestSchedaCestinoSposta` |
| M7 | senza il gancio prima del commit | `TestSpostaNelCestinoTuttoONiente` |
| M8, M9 | limiti 1–365 allargati | `TestGiorniCestino` |
| M10c | `ParseInt` in base 0 | `TestGiorniCestino` |
| M12 | la scheda non legge la finestra | `TestGiorniCestino`, `TestSchedaCestinoPulsante` |
| M13 | il giro non cancella i dettati delle buste nel cestino | `TestRaccoltaScadenzaBusteNelCestino`, `TestRaccoltaScadenzaTuttoONiente` |
| M14 | il giro cancella quelli delle buste fuori dal cestino | `TestRaccoltaScadenzaBusteNelCestino` |
| M15 | i dettati del cestino cancellati prima dei blocchi | `…BusteNelCestinoBloccate` |
| M30 | la sottoquery del cestino vuota | `TestRaccoltaScadenzaBusteNelCestino` |
| M16 | rotta solo del titolare | `TestSchedaCestinoSposta/dipendente`, `TestOgniRottaHaIlSuoPermesso` |
| M17 | riuscita alla scheda invece che alla home | `TestSchedaCestinoSposta` |
| M18 | «non trovata» alla scheda invece che alla home | `TestSchedaCestinoSposta`, `TestSchedaCestinoRifiuti` |
| M19 | testo della riuscita | `TestSchedaCestinoSposta` |
| M20 | pulsante senza `data-confirm` | `TestSchedaCestinoPulsante` |
| M21 | numero interno al posto di quello visibile | `TestSchedaCestinoPulsante` |
| M22 | pulsante solo al titolare | `TestSchedaCestinoPulsante` |
| M23b | guasto alla home invece che alla scheda | `TestSchedaCestinoRifiuti` |
| M23c | guasto senza riga nel registro | `TestSchedaCestinoRifiuti` |
| M24 | test statico: un filtro in meno tollerato | i due test statici |
| M25 | variabili locali: il ramo con **più** filtri | i due test statici |
| M26 | JOIN non contato | i due test statici |
| M31b | lista che cresce trattata come rami | `TestFiltroDelCestinoInOgniQuery` |
| M32 | i return d'errore contano | i due test statici |
| M27 | **codice:** `condizioni` della lista senza il filtro | `TestFiltroDelCestinoInOgniQuery` (`Buste.Lista`, `Buste.Selezione`) |
| M28 | **codice:** la query della scheda senza il filtro | `TestFiltroDelCestinoInOgniQuery` |
| M29 | **codice:** una query nuova senza filtro in `internal/web` | `TestFiltroDelCestinoInOgniQuery` |
| F5 | **codice:** una chiamata nuova `leggiBustaEmail(ctx, q, "id = ?", id)` (la F5 del revisore) | `TestFiltroDelCestinoInOgniQuery` |
| F5b–F5d | **codice:** `righeDellEtichetta` con «1», la consegna con `conCestino` true, l'anteprima con `paginaDove` senza filtro | `TestFiltroDelCestinoInOgniQuery` |
| F5e–F5g | test: chiamate non controllate, `true` ignorato, un nome sbagliato in `funzioniConWhere` | `TestFiltroDelCestinoInOgniQuery` |
| N1 | `aggiornato_il` tolto dall'UPDATE (del revisore) | `TestSpostaNelCestino` |
| N10 | l'errore scritto nel registro (del revisore) | `TestSchedaCestinoRifiuti` |

M27 nel primo giro **viveva**: il test seguiva tutte le stringhe del corpo dei costruttori del WHERE, e una query con il
filtro in `busteDellEtichetta` bastava. Corretto: ora segue il risultato (i `return`, `strings.Join`, `append`, le liste
che crescono), con un caso in più in `TestFiltroDelCestinoRiconosce`; rifatta, uccisa.

## Banco

**Scenario nuovo `cestino`** (livello **browser**: lo spostamento dello standalone è un evento LiveView, che il livello
HTTP non può fare), `bench/browser/cestino.json` e `cestino` in `tools/banco/browser/banco.mjs`, fra gli scenari normali
(non `in_fondo`): usa solo una busta sua per vista («Cliente cestino 136 <vista>», QR «SINT-CESTINO-136-<vista>», del
2025: 8136/25 e 8137/25 nello standalone), che finisce nel cestino prima che giri lo scenario dopo.

- **Confrontati** (tutte e due le app, PC e telefono): la busta creata, la ricerca per nome (1 → 0, «Nessuna riparazione
  trovata.»), il contatore «Lavor.» (uno in meno), il pulsante, la domanda (con `<numero>`, D-64), «Busta spostata nel
  cestino.» sulla home, la scheda dopo («Riparazione non trovata.»), il secondo tocco da una scheda aperta prima («Busta
  non spostata nel cestino.»), una riga `repair.trashed`; sul database vuoto la busta 1 «Riparazione non trovata.».
- **`solo_go`:** `di-nuovo.percorso` (il secondo tocco: lo standalone resta sulla scheda LiveView, il Go torna alla home).
- **Non nel banco:** il link del cliente `/r/` della busta (nel banco il Go non ha `indirizzo_pubblico`, D-46: il link di
  una busta nuova non si vede in nessuna pagina), la campanella (servirebbe un messaggio del cliente sulla busta), il
  dettato (servirebbe la raccolta accesa: la accende solo `raccolta-esclusioni`, in fondo). Li provano i test Go.
- Controllato con `node --check` e i test di `tools/banco` (`TestScenariBrowserDelRepo` con le attese nuove).

Esito: **un giro, verde in tutti e due i livelli** (s136a, 28/11). Configurazione: `gestrip-standalone:fab-225`,
`sintetici-import`, binari `gestrip` e `banco` compilati dal working tree (`CGO_ENABLED=0 -tags nodynamic`), «memoria
libera» del coordinatore; guardia sotto 10 GB mai scattata, minimo 18 646 792 kB (17,8 GiB) di `MemAvailable`.

- **HTTP:** 91 scenari, 1373 passi, 0 inattese, 0 errori, «BANCO VERDE».
- **Browser:** 40 scenari (con `cestino`), 80 viste, 49 differenze attese, **0 inattese**, 0 da decidere, 0 errori,
  «BANCO BROWSER VERDE». Nessun 500 «Database busy» dello standalone: la nota 8 non si è vista, niente rigiri.
- **`cestino` verde** sulle due viste: tutti i campi confrontati sono uguali nelle due app e alle attese (busta salvata,
  ricerca 1 → 0, «Nessuna riparazione trovata.», contatore, pulsante, domanda con `<numero>`, «Busta spostata nel
  cestino.», scheda dopo «Riparazione non trovata.», secondo tocco «Busta non spostata nel cestino.», una riga
  `repair.trashed`, nessun errore JS né risposta d'errore). L'unica differenza è il `solo_go` `di-nuovo.percorso`
  (standalone `/repairs/N`, Go `/`). Ricetta e scenario non sono stati corretti.
- **`scripts/check` con `GESTRIP_BANCO_ESITO` (esito s136a, salvato fuori dall'istanza prima di `banco giu`):** verde;
  voci 353, chiuse 235, **rosse 0**, parziali 16 (aperte 91, fuori v1 11).
- **`banco giu`** senza avanzi: nessun container, rete o volume `s136a`, cartella dell'istanza tolta.
- Nessuna PNG del manuale toccata (impronte uguali prima e dopo).

## Per il manuale

Una riga in `docs/manuale/da-scrivere.md` per il capitolo 12 (146): il pulsante, la domanda, dove sparisce la busta, il
dettato che non torna con il ripristino e quelli delle buste importate già nel cestino.

## LSP

3 chiamate: `workspaceSymbol` «Campanella» (la campanella si legge a ogni pagina, `LeggiCampanella` con il filtro: niente
da annunciare), `findReferences` su `bustaPerNumeroScelta` (3: l'unico `conCestino = true` è la raccolta, il motivo
dell'eccezione D-109) e su `PreparaBloccoImport` (i chiamanti del blocco dopo l'import sono solo i tre lavori automatici:
le azioni a mano non hanno blocco). Il resto (le 79 query, il banco) con grep e lettura.

## Revisione (Opus indipendente, ~150k token, ~10 minuti; 31 mutazioni, 25 uccise; 0 ALTA, 1 MEDIA, 3 BASSE)

Mutazioni del revisore: 31, 25 uccise; N17 equivalente; F6 e F7 sono limiti del test statico, ora scritti; N1, N10 e
F5, sopravvissute, ora uccise dalle correzioni. Pareri positivi sul trattamento dei dettati (blocco solo per il giro
automatico) e sullo scenario del banco.

1. **MEDIA, test statico:** le eccezioni contavano i pezzi dove sono scritti, non dove si chiamano: una chiamata nuova
   `leggiBustaEmail(ctx, q, "id = ?", id)` passava (F5). **Corretta:** `funzioniConWhere` e il controllo di ogni
   chiamata, con l'elenco nominativo dei chiamanti senza filtro e il motivo (sopra). F5 rifatta: uccisa.
2. **BASSA, `TestSpostaNelCestino`:** il controllo di `aggiornato_il` non provava niente (stesso istante della
   creazione). **Corretta:** l'orologio avanza di un minuto prima dello spostamento; N1 uccisa.
3. **BASSA, `TestSchedaCestinoRifiuti`:** il registro non era controllato contro il testo dell'errore. **Corretta:**
   `RAISE(ABORT, 'guasto-prova-136')` e quel testo assente dal registro; N10 uccisa.
4. **BASSA, limiti del test statico non scritti** (`Sprintf`, filtro in un commento SQL, query sulle sole tabelle figlie).
   **Corretta:** scritti in SVILUPPO, sezione «Cestino».

Dopo la revisione: la nota 11 di `docs/domande-aperte.md` è fra le risposte registrate, con quello che ha fatto la 136.

## Dubbi (decisi da Stefano il 28/11/2026: si lascia com'è)

- **D-123:** i messaggi già in coda di una busta nel cestino mostrano ancora il numero, senza segni (`Coda.righe` della
  pagina dei provider e `messaggiInCoda` della conferma del ripristino, solo titolare).
- **D-124:** con l'orologio avanti il giro giornaliero non cancella niente, nemmeno i dettati delle buste nel cestino.
