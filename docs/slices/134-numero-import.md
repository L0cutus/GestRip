# 134 — Numero di prima della D-77 nel database (`numero_import`)

- **Fonte:** piano T4.11 approvato da Stefano il 27/11/2026 (`piano-T4.11-bozza.md`, riga 134 della tabella; riga 135
  per chi la userà; domanda **F**; rischi «Vecchi link «0046/26»» e prerequisito «Numero di prima della D-77»).
  Decisione **D-120** (risposta F, slice 134–135) nell'inventario; **D-77** (`internal/importer/riscritture.go`,
  `buste.NormalizzaNumero`). Chiude l'incongruenza 5 del piano (il numero di prima stava solo nel report).
- **Voci:** nessuna voce chiusa; prerequisito della riga T4.11 (Stefano, 10/11/2026) e prima metà della D-120 (la
  seconda, i link pubblici, è la 135). **Dipendenze nuove:** nessuna. **Migrazioni nuove:** `0026_numero_import.sql`.
- **Modelli:** implementatore Opus (~135k token, ~13 minuti di orologio). Revisore Opus indipendente (~60k token, ~10
  minuti): nessuna ALTA, nessuna MEDIA, 2 BASSE, corrette (vedi «Revisione»).

## Cosa c'è

- **Migrazione 0026** (`internal/db/migrazioni/0026_numero_import.sql`): `ALTER TABLE buste ADD COLUMN numero_import
  TEXT`, senza DEFAULT, senza CHECK, senza indice.
  - **NULL e non ''** per le buste nate nel Go: è lo stile delle colonne di testo facoltative delle buste
    (`codice_qr`, `note_pubbliche`, `token_pubblico`, … sono `TEXT` nullabili, mai `NOT NULL DEFAULT ''`), e tiene
    distinti «non viene dall'import» (NULL) e un numero vuoto dello standalone ('').
  - **Nessun CHECK sulla forma:** il numero dello standalone non ne ha (`numero` nel Go nemmeno) e l'import lo porta
    com'è; un CHECK potrebbe bloccare un import vero.
  - **Nessun indice:** se serve lo aggiunge la 135, che sa come cerca.
- **Import** (`internal/importer/riscritture.go`, `riscriviBuste`): prima delle riscritture, nella stessa transazione
  dopo la copia di tutte le tabelle, `UPDATE buste SET numero_import = numero` su **tutte** le buste (cestino
  compreso); poi la D-77 riscrive `numero` come prima. Quindi:
  - «0046/26» riscritta → `numero` «46/26», `numero_import` «0046/26»;
  - numero senza zeri, o con zeri ma occupato («00/26» accanto a «0/26») → `numero_import` = `numero`.
  Nessun trigger sulle buste guarda `numero` o `numero_import`; `aggiornato_il` non si tocca.
- **Report dell'import:** com'era (stessi avvisi `buste/numero_senza_zeri…`, nessuna voce nuova, la colonna non è
  nominata).
- **Struttura dell'importer** (`internal/importer/mappa.go`): `buste.numero_import` in `ColonneSoloGo` con il perché
  (`TestLeColonneMappateEsistonoNelGo` lo vuole per ogni colonna Go senza sorgente). Non va in `ValoriSoloGo`: il
  valore non è fisso, lo scrive `riscriviBuste`.
- **Nessun uso nei link pubblici** (`/stato`, `/artigiano`, `/foto`, `/thumb`, `internal/buste/storica.go` non
  cambiano): è la 135.

### Coerenza con backup, ripristino, esportazioni ed elenchi

- **Backup** (`backup.Scrivi`): copia il database intero con `VACUUM INTO`; `snapshot.json` ha le migrazioni
  applicate (ora 1…26) e i **conteggi delle righe** per tabella, non le colonne: niente da cambiare.
- **Verifica e ripristino:** la verifica confronta migrazioni e conteggi del database estratto con la sua fotografia
  (uguali per costruzione); il ripristino applica le migrazioni del programma, quindi un archivio di prima della 134
  prende la 0026 con `numero_import` NULL anche sulle buste importate (vedi «Dubbi»).
- **Esportazioni** (`assist/esportazione.go`, `buste/esporta_dettati.go`, liste e PDF): tutte con colonne scritte a
  mano, nessun `SELECT *` sulle buste: la colonna **non esce** in niente di visibile o esportato.
- **Unico INSERT del Go** sulle buste (`buste/crea.go`) con colonne esplicite: la colonna resta NULL.
- **Elenchi espliciti:** `ChiaviSoloGo` (impostazioni) non c'entra; `TabelleSoloGo`, `IndiciSoloGo`, `EsterneSoloGo`
  invariati; `ColonneSoloGo` aggiornato. Due test con il numero delle migrazioni ritoccati (sotto).

## Prove

`CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -tags nodynamic` su `internal/importer`, `internal/db`, `internal/buste`,
`internal/backup`: verdi.

- Nuovo `TestNumeroImportSalvato` (`internal/importer/regole_dati_test.go`), dati inventati: buste «0046/26» (nel
  cestino), «2/26», «0/26», «00/26» (occupato), «A-7/26» (fuori forma, dalla revisione). Vuole `numero_import`
  «0046/26», «2/26», «0/26», «00/26», «A-7/26» con `numero` «46/26», «2/26», «0/26», «00/26», «A-7/26»; il report senza «numero_import» e con l'avviso dei numeri di sempre;
  una busta creata nel Go (`buste.Crea`) sul database importato con `numero_import` NULL. Il tutto **due volte**
  sulla stessa sorgente in due destinazioni nuove (l'import non sovrascrive mai, `TestNonSovrascriveMai`): stesse
  buste e stessi avvisi.
- Ritoccati: `TestMigrazioniSuDatabaseVuotoEDopoIlRiavvio` (`internal/importer/schema_test.go`, 26 migrazioni) e
  `TestMigrazioneGiriSuUnDatabaseInUso` (`internal/buste/giri_test.go`: toglie anche la colonna della 0026 prima di
  rifare le migrazioni dalla 19, e ne aspetta 19…26).
- I test esistenti della D-77 (`TestRiscrittureDellImport`, `TestNumeroSenzaZeriGiaOccupato`,
  `TestContatoreDopoLaRiscrittura`, `TestRiscrittureNellImportBloccato`) verdi senza modifiche.

`PATH=$HOME/sdk/go1.27.3/bin:$PATH scripts/check`: **verde** (voci 353: chiuse 57, rosse 0, parziali 192, aperte 93, fuori v1 11; segreti e dati veri OK; senza `GESTRIP_BANCO_ESITO`).

## Mutazioni

Fatte dall'implementatore con uno script (riga cambiata, test mirati, file rimesso e confrontato con la copia):
**8 su 8 uccise**, tutte per l'asserzione giusta (log guardati); più M9 sulla correzione della revisione (sotto).

| # | Mutazione | Uccisa da |
|---|---|---|
| M1 | `numero_import` mai scritto | `TestNumeroImportSalvato` |
| M2 | scritto solo fuori dal cestino | `TestNumeroImportSalvato` |
| M3 | scritto solo per i numeri con lo zero davanti | `TestNumeroImportSalvato` |
| M4 | scritto dopo la D-77 (numero già riscritto) | `TestNumeroImportSalvato` |
| M5 | migrazione `NOT NULL DEFAULT ''` | `TestNumeroImportSalvato` (busta del Go) |
| M6 | migrazione `DEFAULT ''` | `TestNumeroImportSalvato` (busta del Go) |
| M7 | voce di `ColonneSoloGo` sulla tabella sbagliata | `TestLeColonneMappateEsistonoNelGo` |
| M8 | scritto per tutte tranne la busta 1 | `TestNumeroImportSalvato` |

Script e log in `to-be-deleted-after-use/_slice-134/` (da cancellare col commit).

## Revisione (Opus indipendente, ~60k token, ~10 minuti; 16 mutazioni, 14 uccise; 0 ALTA, 0 MEDIA, 2 BASSA)

Mutazioni del revisore: sopravvissute M9 (ora coperta) e N4 (equivalente). Le due BASSE, corrette
dall'implementatore:

1. **Nessun test sulla copia com'è di un numero fuori forma** (M9 del revisore sopravvissuta: un CHECK
   `GLOB '[0-9]*/[0-9][0-9]'` sulla colonna). **Corretta:** `TestNumeroImportSalvato` ha una busta «A-7/26» e vuole
   `numero_import` uguale al numero. Rifatta la M9 sulla correzione: **uccisa** (l'import fallisce sul CHECK).
2. **`buste.numero_import` anche in `ValoriSoloGo` con un valore fisso non si vedrebbe** (N4): equivalente, perché
   `riscriviBuste` sovrascrive subito con il numero. **Corretta** con una riga di commento accanto alla voce di
   `ColonneSoloGo` («Non sta in ValoriSoloGo: …»).

Nota passata alla 135: un database importato prima della 134 (o un backup di prima ripristinato dopo) ha
`numero_import` NULL anche sulle buste importate; la 135 deve trattare NULL come «nessun numero di prima» (vedi
«Dubbi»).

## Per il manuale

Niente: la colonna non si vede da nessuna parte. Il manuale parlerà dei vecchi link con la 135.

## Differenze dallo standalone

Lo standalone non ha la colonna: non riscrive i numeri, quindi il numero salvato è sempre quello di prima. Nel Go
`numero_import` è solo Go (`ColonneSoloGo`), riempita una volta all'import e mai cambiata; nessun comportamento
visibile cambia.

## Banco

**Non serve.** Il banco confronta le risposte HTTP delle due istanze, non le tabelle; il report d'import che legge
(`Tabelle`, `Lette`, `Scritte`) conta righe e non cambia. Nessuna pagina, link o testo cambia. La 135 sì (rigiro del
banco, già nel piano).

## LSP

4 chiamate. `workspaceSymbol` «ColonneSoloGo» e «ChiaviSoloGo»: utili, hanno dato subito `mappa.go` righe 588 e 603 e
i test che le controllano (`TestChiaviSoloGoComplete`, `TestChiaviSoloGoNellaSorgente`) senza leggere i file interi.
`findReferences` su `riscriviBuste` (2 tentativi): fallite («column is beyond end of line», gopls aveva ancora il
file di prima della modifica); il solo chiamante (`importa.go:390`) trovato con grep. Il resto (usi di
`ValoriSoloGo`, `INSERT INTO buste`, `SELECT *`, migrazioni contate nei test, `snapshot.json`) con grep.

## Dubbi

- **Database già importati prima della 134** (prove, banco, eventuali copie): la 0026 li lascia con `numero_import`
  NULL anche sulle buste importate, perché il numero di prima, dopo la D-77, nel database non c'è più (solo nel report
  d'import). Va bene finché l'import vero si fa con un programma che ha la 134 (il piano lo vuole); la 135 deve
  trattare NULL come «nessun numero di prima» (solo la firma sul numero salvato).
- Un archivio di backup di prima della 134 ripristinato dopo ha lo stesso effetto (NULL).
