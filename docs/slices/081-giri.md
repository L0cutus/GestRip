# 081 — Giri dall'artigiano in una tabella, con la prova di equivalenza (T4.8, sesta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/workshop/handover_history.ex`
  (`cycles/1`, `past_events/1`, `outbound/3`, `confirmations/1`, `stamped_ids/1`, `current_cycle/2`, `past_cycle/3`,
  `notes_present?/1`, `event_handover_id/1`, `newest_first/1`, `detached_by_hand/1`), `lib/gestrip_standalone/workshop.ex`
  (`artisan_cycles/1` alla riga 801, `confirm_handover/2`, `delete_handover_from_history/2`,
  `detach_bag_from_history/3`), `repair_live.ex` (riquadro `#repair-artisan-cycles`, solo per sapere quali campi si
  vedono: la consegna «Consegna #N» si vede, quindi resta anche dopo l'eliminazione della consegna).
- **Voci:** ART-19 (dominio; il riquadro sulla scheda è della 082). **Migrazione 0019** (`0019_giri_artigiani.sql`).
  Nessuna D nuova: comportamento visibile identico (la tabella è una scelta tecnica, tema H, decisa il 30/10/2026).
- **Con questo commit (amministrativo):** le risposte di Stefano ai dubbi 077-080 nelle ricevute (riga «Deciso da
  Stefano il 15/11/2026» sotto ogni dubbio) e le **D-89, D-90, D-91** nelle ricevute 077 e 079 e in `SVILUPPO.md`
  (sezione «Decisioni di Stefano del 15/11/2026»). **Nessuna riga in `bench/deviazioni.json`:** nessuno scenario le
  vede (il banco evita apposta un numero che vale per più buste, `banco.mjs` riga ~2726; la lista sotto lo scanner si
  confronta dopo la chiusura; il suono non si confronta).
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco non è stato lanciato: lo fa
  la sessione principale.

## Cosa c'è

- **Migrazione 0019** (`internal/db/migrazioni/0019_giri_artigiani.sql`): tabella `giri_artigiani` (busta con
  `ON DELETE CASCADE`; consegna come numero **senza chiave esterna**, come nell'audit: «Consegna #N» resta dopo
  «Elimina dallo storico»; uscita e rientro nella forma fissa degli istanti; `nota_presente`, `sganciata`; CHECK
  uscita ≤ rientro e nessuna nota o sgancio su un giro aperto), indici per busta e per consegna, poi la
  ricostruzione da vuota. **Rieseguibile** (`IF NOT EXISTS`, `DELETE` prima): su un database nuovo non trova niente,
  su uno in uso o già importato legge l'audit.
- **Le regole** (`internal/buste/giri.go`, le stesse istruzioni della migrazione, `TestMigrazioneGiriComeIlCodice`):
  - un **giro chiuso** per ogni `repair.returned_from_artisan` o `repair.returned_from_artisan_auto` con
    `risorsa = 'repair'` e l'id della busta scritto com'è (`resource_id == to_string(id)`); consegna da
    `handover_id` solo se è un intero JSON; nota presente solo con `artisan_notes: true`;
  - la sua **uscita**: prima quella del giro aperto della stessa busta e della stessa consegna (`IS`, anche vuota) non
    dopo il rientro, poi (ricostruzione) l'ultima `handover.confirmed` della stessa consegna con `repair_ids` elenco
    che nomina la busta come intero e non è dopo il rientro; nessuna: sconosciuta (`outbound/3`);
  - **sganciata**: una `handover.bag_detached` della stessa consegna con `repair_id` intero uguale
    (`detached_by_hand/1`), per le «riaperte» della 082;
  - un **giro aperto** per ogni busta fuori adesso (ricostruzione) o timbrata da una conferma (dal vivo), con
    l'uscita della conferma: è l'uscita che il rientro copierà, così un rientro dopo la pulizia dell'audit (D-14)
    non la perde.
- **Chi scrive** (nella transazione della riga d'audit, a partire da quella riga: `audit.ScriviConID`):
  `confermaNellaTransazione` (anche «Stampa e conferma» della 078) → `usciteNeiGiri`; `RientraDallArtigiano` (080) e
  `rientroAutomatico` dal QR (054) → `rientroNeiGiri` (il giro chiuso, poi via i giri aperti della busta).
- **Chi ricostruisce:** la migrazione; l'import, dopo aver copiato audit e buste e riscritto i numeri
  (`buste.RicostruisciGiri` nella sua transazione: la migrazione gira sul database ancora vuoto). Tabella, indici e
  chiave esterna in `importer.TabelleSoloGo`, `IndiciSoloGo`, `EsterneSoloGo`.
- **Chi legge:** `Buste.GiriDellArtigiano(ctx, id) ([]Giro, error)` = `artisan_cycles/1`: il giro attuale dalle
  colonne quando la busta è uscita (consegna, artigiano della consegna, uscita, rientro, testo della nota), poi i giri
  chiusi con il rientro prima della sua uscita (tutti se non è uscita), dal più recente, a pari istante nell'ordine
  dell'audit (`Enum.sort_by` stabile). L'artigiano è quello della consegna se c'è ancora. I giri aperti non si
  mostrano mai (una busta riaperta prima del rientro non ha giro, come nello standalone). Busta che non c'è:
  `ErrBustaNonTrovata`.

## Scelte

- **Righe per rientro, non per conferma.** Lo standalone fa un giro passato per ogni riga di rientro; la tabella fa
  lo stesso, così l'equivalenza è per costruzione. I giri aperti servono solo a portare l'uscita oltre la pulizia
  dell'audit e non si vedono.
- **La stessa SQL per ricostruzione e vivo.** Il rientro dal vivo usa `sqlRientriNeiGiri` ristretta alla riga appena
  scritta: le regole stanno in un posto solo (più la copia nella migrazione, controllata dal test).
- **Il giro attuale resta dalle colonne**, come nello standalone (anche il testo della nota, che l'audit non ha).
- **Dati strani portati come sono:** un rientro dopo l'uscita attuale (non è un giro passato), una consegna scritta
  come testo o con la virgola (nessuna consegna), un id con lo zero davanti (ignorato), `repair_ids` non elenco o con
  testi (uscita sconosciuta).

## Prove

`internal/buste/giri_test.go`: `cicliDallAudit` è `cycles/1` riscritta in Go **senza SQL in comune** con `giri.go`
(Jason: intero = numero senza parte decimale né esponente). `stessiGiri` a ogni passo: per ogni busta i giri della
tabella sono quelli dell'audit, e la tabella scritta dal vivo è uguale (come insieme di righe) a quella che
`RicostruisciGiri` e il file della migrazione rifarebbero in una transazione annullata.
- `TestGiriDalVivo`: conferma, conferma con la stampa (078), rientro con nota, riapertura con un altro artigiano,
  rientro automatico dal QR, chiusure, la stessa busta due volte nella stessa consegna (riaperta «già in lista» e
  riconfermata), con i giri attesi scritti per esteso.
- `TestGiriInventati`: i casi limite (uscita sconosciuta in quattro forme, conferma dopo il rientro, conferma di
  un'altra consegna, nota `"true"` come testo, rientri a pari istante, rientro dopo l'uscita attuale, id con lo zero,
  risorsa sbagliata, busta che non c'è, consegna uscita dallo storico, sgancio con id intero e come testo, buste fuori
  con conferma, senza e senza consegna, consegna tolta mentre la busta è fuori).
- `TestGiriSopravvivonoAllAudit` (D-14): tolto tutto l'audit, i giri sono quelli di prima; un rientro dopo la
  pulizia prende l'uscita dal giro aperto.
- `TestMigrazioneGiriComeIlCodice`, `TestMigrazioneGiriSuUnDatabaseInUso` (database tornato alla 0018 con storia e
  un giro dal vivo: `Migra` applica la 0019 e dà la tabella del vivo e della ricostruzione; il file rieseguito dà la
  stessa tabella).
- `internal/importer/giri_test.go`, `TestImportRicostruisceIGiri`: import del database sintetico dello standalone con
  le righe d'audit di una consegna (la busta 1 rientrata con nota, la 2 fuori): giro chiuso e giro aperto, e
  `GiriDellArtigiano` sul database importato. **La cattura vera** (Flask sintetico → standalone → Go) nasce solo nella
  catena del banco: il suo import standalone non scrive righe di consegna o rientro nell'audit, quindi lì la tabella
  ha solo i giri aperti delle buste fuori (la 4/26 nella consegna 1, uscita sconosciuta) e i giri attuali restano
  dalle colonne: la scheda non cambia. Il banco della sessione principale importa la cattura con la 0019.
- `TestMigrazioniSuDatabaseVuotoEDopoIlRiavvio`: 19 migrazioni, tabella vuota su un database nuovo.

## Mutazioni

24 mutazioni a mano, una alla volta su una copia del repo (`to-be-deleted-after-use/081-giri/mutazioni/`: `muta.py`,
`giro1.log`, `giro2.log`, `esiti/<id>.log`; copia cancellata a fine giro). Le mutazioni della SQL dei giri sono fatte
**uguali in `giri.go` e nella migrazione**, così non le prende solo il confronto dei testi. Primo giro: **22 prese su
24**; M20 non compilava (l'import di `buste` restava inutilizzato: riscritta), M23 sopravvissuta (l'uscita presa
dalla colonna della busta invece che dal giro aperto: nei dati di prova i due istanti erano uguali); messa la
conferma della b4 un microsecondo dopo la colonna, come nello standalone. Secondo giro su M20 e M23: **prese tutte e
due**. Totale **24 su 24**. Tolto anche, prima del giro, un `DELETE` dei giri aperti nella conferma che nessun caso
poteva raggiungere (una busta si timbra solo in bottega, e il rientro toglie già i giri aperti).

| Id | Mutazione | Presa da |
|---|---|---|
| M01 | `repair_ids` non elenco accettato | TestGiriInventati, TestGiriSopravvivonoAllAudit, TestMigrazioneGiriSuUnDatabaseInUso |
| M02 | id in `repair_ids` anche non interi | gli stessi |
| M03 | conferma dopo il rientro | TestGiriDalVivo, TestGiriInventati, … |
| M04 | la prima conferma invece dell'ultima | TestGiriDalVivo |
| M05 | conferma di un'altra consegna | TestGiriInventati, … |
| M06 | nota con qualunque valore | TestGiriDalVivo, TestGiriInventati, … |
| M07 | consegna non intera presa | TestGiriInventati, … |
| M08 | id con lo zero davanti | TestGiriInventati, … |
| M09 | risorsa non `repair` | TestGiriInventati, … |
| M10 | mai sganciata | TestGiriInventati |
| M11 | sgancio con l'id come testo | TestGiriInventati |
| M12 | giri aperti anche per le rientrate | TestGiriDalVivo, TestGiriInventati, TestMigrazioneGiriSuUnDatabaseInUso |
| M13 | lettura senza il filtro dell'uscita attuale | tutti e quattro |
| M14 | pari istante in ordine inverso | TestGiriInventati, … |
| M15 | il rientro lascia i giri aperti | tutti e quattro |
| M16 | giro aperto di un'altra consegna | TestGiriInventati |
| M17 | rientro automatico senza giro | TestGiriDalVivo |
| M18 | conferma senza giri aperti | TestGiriDalVivo |
| M19 | rientro senza giro | tutti e quattro |
| M20 | import senza ricostruzione | TestImportRicostruisceIGiri |
| M21 | nota presente del giro attuale sempre falsa | tutti e quattro |
| M22 | migrazione senza giri aperti | TestGiriDalVivo, TestGiriInventati, TestMigrazioneGiriComeIlCodice, TestMigrazioneGiriSuUnDatabaseInUso |
| M23 | uscita dalla colonna invece del giro aperto | TestGiriSopravvivonoAllAudit, TestMigrazioneGiriSuUnDatabaseInUso |
| M24 | migrazione non da vuota | TestGiriDalVivo, TestGiriInventati, TestMigrazioneGiriComeIlCodice, TestMigrazioneGiriSuUnDatabaseInUso |

## Controlli

`scripts/check` (gofmt, vet, staticcheck, errcheck, build senza CGO, tutti i test, copertura dell'inventario,
segreti) verde due volte, l'ultima dopo le mutazioni e l'ultima modifica del codice
(`to-be-deleted-after-use/081-giri/prove/check2.log`, «uscita 0»): copertura «voci 353: chiuse 41, rosse 0,
parziali 148, aperte 153, fuori v1 11».

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Nessuno scenario nuovo (i giri si vedono con la 082). Da
guardare: che l'import della cattura passi con la 0019 (`giri_artigiani` è solo Go, l'import la ricostruisce) e che
il banco resti verde come dopo la 080 (HTTP 72/1207, browser 20/40): conferme e rientri ora scrivono anche la tabella
nella stessa transazione.

## Dubbi

1. **Giri aperti senza uscita conosciuta.** Le buste fuori al momento dell'import (nella cattura la 4/26) hanno un
   giro aperto con l'uscita sconosciuta, perché l'import dello standalone non ha scritto la conferma nell'audit:
   quando rientrano, il loro giro passato dirà «Uscita —», esattamente come lo standalone. L'uscita delle colonne
   sarebbe nota, ma usarla sarebbe una differenza. **Raccomandazione:** tenere come lo standalone.

## Revisione

Revisore indipendente: nessuna ALTA né MEDIA; prova di equivalenza indipendente dal codice confermata. Due BASSE
corrette dalla sessione principale: (1) la 0019 e `RicostruisciGiri` svuotano e rifanno la tabella dall'audit, quindi
rieseguirle è sicuro solo prima di qualsiasi pulizia dell'audit (D-14): ora lo dicono il commento della migrazione e
quello di `RicostruisciGiri` («solo per l'import, su un database appena creato»); (2) il titolo del Dubbio 1 della 077
non dice più «da decidere» (D-89).

## Banco finale (sessione principale)

`s081p`, 15/11/2026: HTTP 72 scenari, 1207 passi, 0 inattese; browser 20 scenari, 40 viste, 0 inattese. `scripts/check`
rifatto dalla sessione principale: verde.
