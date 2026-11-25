# 122 — Registro dei backup verificati, sblocco dopo l'import (T5.2, quarta slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 122, risposte F e I, «Cosa fa lo standalone oggi» (`verified_backup.ex:verify_after`, letto a `go-baseline`),
  «Incongruenze», «Rischi»; D-11, D-55, D-110, D-113; regole delle 119-121 in SVILUPPO.
- **Voci:** OPS-17 (parte, vedi «Segno di copertura»); sblocca ADM-17, D-55 (RIP-S-57), D-110 (RIP-H-51) dopo
  l'import. Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** nessuno (nessuna pagina cambia).

## Cosa c'è

- `internal/db/migrazioni/0021_verifiche_backup.sql`: tabella `verifiche_backup` (nome italiano come tutte le altre;
  il PIANO diceva `backup_verifications`, corretto) e indice `(esito, catturato_il)`.
- `internal/db/registro_backup.go`: `RegistraVerificaBackup`, `BackupVerificatoDopo`, errori `ErrRegistroBackup`,
  `ErrOrologioBackup`, `ErrArchivioCambiato`; ricontrollo in tre modi (`ricalcolaSempre`, `riempiMemoria`,
  `soloMemoria`), memoria `memoriaImport` con un ricalcolo alla volta, `ValiditaRicontrollo` 24 h,
  `AnticipoRicontrollo` 1 h.
- `internal/db/dopoimport.go`: `PreparaBloccoImport` (fuori da transazioni e lucchetti, può rileggere l'archivio) e
  `BloccatoDopoImport` (dentro, non rilegge mai; senza memoria fresca o con un errore dice bloccato).
- Chiamanti: `internal/audit/conservazione.go` (prepara in `Applica` prima dei lotti; `bloccataDopoImport` ora
  `(true, err)`), `internal/buste/raccolta_scheda.go` (prepara prima della transazione), `internal/media/pulizia.go`
  (prepara prima dell'attesa e del lucchetto; causa `registro_backup` nel registro).
- `cmd/gestrip/backup.go`: `gestrip backup` registra ogni verifica (riuscita o fallita) con destinazione `cartella`;
  senza la riga il backup non è «riuscito». `verifica-backup` non registra.
- `internal/importer/mappa.go`: `verifiche_backup` in `TabelleSoloGo` e il suo indice in `IndiciSoloGo`.
- Test: `internal/db/registro_backup_test.go` (nuovo); `cmd/gestrip/backup_linux_test.go`; nei chiamanti
  `internal/media/pulizia_test.go`, `internal/audit/conservazione_test.go`, `internal/buste/raccolta_scadenza_test.go`;
  aggiornati per la migrazione 0021 `internal/importer/schema_test.go` (21 migrazioni) e `internal/buste/giri_test.go`.
- Documenti: SVILUPPO (sezione nuova «Backup: registro dei backup verificati», 121 e «Archivio media e pulizia»
  aggiornate), PIANO (§1.2 `internal/backup`, §2.5 riga del registro), `docs/domande-aperte.md` nota 7(e) (decisione
  del coordinatore sulle cartelle temporanee: **124**) e difetti in lista n. 8.

### Come fallisce chiusa

- `BackupVerificatoDopo(ctx, q, t)`: solo `(true, nil)` è sì. Conta **solo l'ultima** verifica `ok` (per ora di
  cattura) **sulla chiavetta** (risposta F, approvata da Stefano dopo la revisione: `cartella` e `sftp` non sbloccano;
  fino alla 123 dopo un import non si sblocca niente). Deve essere catturata **strettamente** dopo `t`. Prima la guardia
  sull'orologio su **tutte** le righe: cattura o verifica nel futuro (anche di una riga fallita, `cartella` o `sftp`),
  verifica prima della cattura, `t` zero → `ErrOrologioBackup`. `t` nel futuro → no senza errore.
- Archivio: `Lstat` (assente → no senza errore: la chiavetta tolta), file normale con gli stessi byte, aperto e
  confrontato con `os.SameFile` (un collegamento messo al suo posto no, anche verso una copia identica e della stessa
  lunghezza), SHA-256 ricalcolato su tutto il file con la lettura che guarda il ctx; ogni differenza →
  `ErrArchivioCambiato`, ogni errore di lettura → errore.
- `BloccatoDopoImport`: senza riga di import `(false, nil)`; con un errore qualsiasi (segno dell'import, istante
  illeggibile anche se passa il GLOB, registro, orologio, archivio) `(true, err)`; senza memoria fresca `(true, nil)`.
- **Regola per la 123:** scrivere `chiavetta` solo dopo i controlli del punto di montaggio e del dispositivo.

### Ricalcolo dell'archivio e costo (dopo la revisione, MEDIA)

Prima della correzione `BloccatoDopoImport` ricalcolava lo SHA-256 (~3,6 GB, 1-2 minuti da una chiavetta) **dentro** la
transazione della conservazione (che tiene l'unico collegamento di scrittura: ogni salvataggio aspettava) e, insieme,
dentro il lucchetto dell'archivio della pulizia (caricamenti fermi), a ogni avvio di `serve`, senza unire i ricalcoli.
Ora:

- `PreparaBloccoImport` è l'**unico** punto che rilegge l'archivio per il blocco, e i tre chiamanti la chiamano prima
  della transazione o del lucchetto. Rilegge solo se la memoria non è della stessa riga del registro, dello stesso file
  (inode e dispositivo, byte, ora di modifica) o ha più di 23 h (24 h meno l'anticipo di 1 h). Un ricalcolo alla
  volta per processo: chi arriva durante un ricalcolo aspetta e trova la memoria piena (provato: un solo ricalcolo).
- `BloccatoDopoImport` non rilegge mai: registro, `Lstat`, byte e memoria; scaduta o assente → bloccato.
- Il costo resta: al primo giro dopo ogni avvio di `serve` e poi circa ogni 23 ore il giro che prepara (pulizia,
  conservazione o dettati) aspetta la lettura, ma salvataggi e caricamenti no. Stima, non misurata (T5.4 con la
  chiavetta vera).
- `BackupVerificatoDopo` (cestino della T4.11) ricalcola sempre: va chiamata fuori da transazioni e lucchetti
  (scritto nel commento e in SVILUPPO).

### Chiamanti di `BloccatoDopoImport` (gopls `findReferences`)

`audit.bloccataDopoImport` (conservazione, in transazione), `buste.ScadenzaDettati` (in transazione),
`media.Pulizia.cancellaInCodaConLucchetto` (ogni 60 s, lucchetto preso). Tutti e tre con un errore non cancellano:
la conservazione rende l'errore e fa il rollback; la scadenza rende l'errore (rollback); la pulizia rende l'errore e la
coda resta. Ognuno chiama `PreparaBloccoImport` prima (`Applica`, `ScadenzaDettati`, `cancellaInCoda`).

## Prove (tutte su dati inventati, archivi finti in cartelle temporanee)

- `internal/db/registro_backup_test.go`:
  - `TestBackupVerificatoDopoIstante` (prima, stesso istante, un microsecondo dopo, in transazione);
  - `TestBackupVerificatoArchivioCambiato` (tolto, un byte diverso della stessa lunghezza, più corto, più lungo, vuoto,
    cartella, collegamento a una copia identica; ognuno anche per prepara + `BloccatoDopoImport`, dopo che la memoria
    si è riempita); `TestBackupVerificatoCollegamentoDellaStessaLunghezza`;
  - `TestBackupVerificatoSoloLUltimoRiuscito` (fallita registrata col motivo e non contata, `sftp` no, **`cartella`
    no anche se più nuova**, chiavetta sì, ultimo archivio sparito con un vecchio buono → no);
  - `TestBackupVerificatoOrologio`, `TestBackupVerificatoRegistroGuasto`, `TestBloccatoDopoImportEBackup`,
    `TestRegistraVerificaBackupRegole`;
  - dopo la revisione: `TestBloccatoDopoImportNonRicalcolaDentro` (contatore dei ricalcoli fermo dentro una
    transazione, senza memoria → bloccato; prepara una volta, poi basta la memoria),
    `TestPreparaBloccoImportUnSoloRicalcolo` (due prepara insieme, un ricalcolo),
    `TestPreparaBloccoImportRinnovaInAnticipo`, `TestBloccatoDopoImportMemoriaScaduta` (memoria scaduta dentro →
    bloccato senza ricalcolo; contenuto cambiato di nascosto visto al rinnovo; orologio indietro),
    `TestBloccatoDopoImportFileSostituito` (R5/R11: file uguale o diverso con stessa ora di modifica e inode nuovo,
    riga del registro nuova), `TestBloccatoDopoImportIstanteDellImportIlleggibile` (R1: mese 19).
- `cmd/gestrip/backup_linux_test.go`: `TestBackupComando` (riga `ok`, `verifica-backup` non ne aggiunge),
  `TestBackupComandoVerificaFallita`, `TestBackupComandoRegistroNonScritto`, `TestBackupComandoSbloccaDopoImport`
  (giro vero: import → bloccato → `gestrip backup -cartella` → **ancora bloccato** → la stessa riga segnata
  `chiavetta`, come farà la 123 → sbloccato → archivio toccato → bloccato con errore → tolto → bloccato).
- Chiamanti: `TestPuliziaCodaRipartiDopoUnBackupVerificato`, `TestConservazioneFermaDopoUnImport` (sblocca con un
  backup sulla chiavetta), `TestConservazioneBloccataConUnErrore` (il wrapper dice bloccata),
  `TestRaccoltaScadenzaDopoUnBackupVerificato`.
- Test scritti insieme al codice (non tutti rossi prima): le guardie si sono viste rosse con le mutazioni.
- **Mutazioni a mano** (`to-be-deleted-after-use/_slice-122/muta.py`, `muta.log`, `muta2.log`):
  - primo giro, 15 che compilano, uccise 14: M1 stesso istante conta; M2 nessuna guardia sul futuro; M3 verifica prima
    della cattura; M5 SHA-256 non confrontato; M6 memoria senza scadenza; M7 memoria senza ora di modifica; M8 errore
    che sblocca; M9 anche `sftp` conta; M10 anche le fallite; M11 il più vecchio; M12 archivio assente = sì; M13
    fallita registrata come riuscita; M14 registro vuoto = sì; M4b né tipo né `SameFile`. Sopravvissuta M4 (solo il
    tipo): equivalente, la ferma `SameFile`;
  - dopo la correzione, 11 nuove, **uccise 11**: M8b `BloccatoDopoImport` sblocca con un errore; N1
    `BloccatoDopoImport` ricalcola dentro; N2 ricalcoli non uniti; N3 niente anticipo (al primo giro
    **sopravvissuta**, uccisa da `TestPreparaBloccoImportRinnovaInAnticipo`); N4 memoria senza riga; N5 memoria senza
    `SameFile`; N6 wrapper dell'audit che dice non bloccata con un errore (al primo giro **sopravvissuta**, uccisa da
    `TestConservazioneBloccataConUnErrore`); N7 la cartella sblocca; N8, N9, N10 pulizia, conservazione e dettati
    senza prepara. Rigirate dopo la correzione e ancora uccise: M1, M2, M3, M4b, M5, M10-M14.

## Segno di copertura

**Nessun `// inventario:` nuovo.** OPS-17 nel destino Go è «registro interno dei backup verificati» al posto della
prova HMAC, «cestino definitivo e pulizia delle foto solo con un backup verificato successivo». La 122 fa registro e
domanda, ma la voce resta **parziale** perché: (1) chi la usa nello standalone (`Trash.purge`, conservazione delle
foto) arriva con la T4.11; (2) l'equivalente di «generazione» (il registro svuotato dal ripristino) è della 130;
(3) nessuno scrive ancora una riga `chiavetta`: arriva con la 123. I test portano «Voce OPS-17, in parte».
ADM-17, RIP-S-57 e RIP-H-51 erano già chiuse: i test nuovi dei chiamanti non aggiungono segni.

## Per il manuale

- Dopo un backup il programma se lo **segna**: quando, dove sta l'archivio, la sua impronta e quante righe aveva
  ogni tabella. Anche un backup la cui verifica non passa viene segnato, come fallito.
- Se il segno non si può scrivere, `gestrip backup` dice **BACKUP NON RIUSCITO**: l'archivio resta, ma non conta.
- Dopo il passaggio dallo standalone (import), il programma **non cancella niente** (registro delle azioni oltre 5
  anni, foto in coda da togliere, dettati oltre 12 mesi) finché non c'è un backup verificato **sulla chiavetta**
  fatto **dopo** il passaggio. Un backup a mano in una cartella non basta.
- Conta solo l'**ultimo** backup verificato sulla chiavetta, e solo se il suo archivio è ancora al suo posto e
  identico: se la chiavetta viene tolta o l'archivio cambia, le cancellazioni si fermano di nuovo.
- `gestrip verifica-backup` controlla un archivio ma non lo segna: un archivio portato da fuori non sblocca niente.

## Differenze dallo standalone

- Registro nel database al posto della prova JSON firmata HMAC accanto all'archivio (D-11): niente chiave delle
  prove; chi può scrivere nel database (volume cifrato) può già cancellare i dati.
- Niente controllo di immagine e generazione: al loro posto l'archivio al suo percorso assoluto e il registro svuotato
  al ripristino (130).
- Conta solo l'**ultima** verifica riuscita, e solo sulla chiavetta; lo standalone accetta una prova valida qualsiasi
  della sua cartella. Più severo: un ultimo archivio tolto non viene salvato da uno più vecchio.
- Guardia sull'orologio (cattura o verifica nel futuro in qualunque riga): lo standalone controlla solo che la
  verifica non sia prima della cattura.
- La memoria del ricalcolo (23-24 h, riempita fuori dalle transazioni) per il solo blocco dopo l'import: lo standalone
  non ha il blocco dopo l'import (ADM-17 è solo Go) e ricalcola a ogni `verify_after`.

## Dubbi

1. ~~Destinazione `cartella` come la chiavetta~~: **risolto** dopo la revisione (Stefano, risposta F): sblocca solo
   `chiavetta`.
2. **Orologio del registro = orologio di sistema**, non quello dei chiamanti (che nei test è finto). Un import con
   `importato_il` nel futuro dà «bloccato» senza errore.
3. ~~Ricalcolo dentro transazione o lucchetto~~: **corretto** dopo la revisione (vedi sopra). Resta l'attesa del giro
   che prepara, misurabile solo con la chiavetta vera (T5.4).
4. **Il registro entra nei backup:** `verifiche_backup` è contata nella fotografia come le altre tabelle; un ripristino
   porterebbe righe della macchina vecchia: la 130 la svuota (risposta F).
5. LSP (gopls): `findReferences` su `BloccatoDopoImport` per tutti i chiamanti; i test in
   `cmd/gestrip/*_linux_test.go` non li vede (tag di build), lì grep. Gli avvisi di gopls «undefined: sql» alla riga
   323 del test e `dimentica`/`memoriaImport` non usati erano di uno stato vecchio del file (il tipo che usava `sql`
   era già tolto): `go vet` e staticcheck (U1000) dentro `scripts/check` sono puliti, e `dimentica` e `memoriaImport`
   sono usati (prepara, test).

## Revisione (Opus, ~75k token, ~30 minuti, 12 mutazioni: 9 uccise)

Nessun percorso sbagliato di sblocco.

- **Decisione (Stefano, risposta F):** sblocca solo la chiavetta. Corretto: query su `chiavetta`, test della cartella
  che non sblocca, `TestBackupComandoSbloccaDopoImport` bloccato dopo `-cartella`.
- **MEDIA, corretto:** ricalcolo di qualche GB dentro la transazione della conservazione e dentro il lucchetto della
  pulizia, a ogni avvio, senza unire i ricalcoli; SVILUPPO diceva «al massimo una volta al giorno», falso. Ora il
  ricalcolo sta solo in `PreparaBloccoImport`, fuori, un ricalcolo alla volta; dentro solo controlli leggeri.
- **BASSA, corretto:** `bloccataDopoImport` dell'audit dava `(false, err)`: ora `(true, err)`.
- **BASSA, test aggiunti:** R1 (`importato_il` che passa il GLOB ma non si legge), R5/R11 (archivio sostituito con
  stessi byte e ora di modifica, inode diverso; riga del registro cambiata). Erano le mutazioni sopravvissute del
  revisore: ora le guardie che toccano (istante illeggibile, `SameFile` e riga nella memoria) hanno un test
  (N4, N5, M8b uccise).
- **In lista** (`docs/domande-aperte.md`, difetti n. 8): (a) una riga con un istante nel futuro blocca finché l'ora
  non la raggiunge, uscita solo a mano: la 126 deve offrire una via d'uscita; (b) doppia riga nel registro della
  pulizia con un errore del controllo (BASSA dell'implementatore).
- File del revisore in `to-be-deleted-after-use/_slice-122/revisione/`, lasciati.

### Seconda revisione (Opus, ~70k token, ~15 minuti, 6 mutazioni: 4 uccise)

Nessuno sblocco sbagliato e nessun blocco dell'app rimasto; i test con `-race` del revisore sono puliti.

- **Corretto:** chi aspettava il ricalcolo di un altro usava l'ora letta prima dell'attesa, più vecchia della
  memoria appena riempita (età negativa → secondo ricalcolo: all'avvio pulizia e conservazione potevano leggere
  l'archivio due volte). Ora l'ora si rilegge dopo il lucchetto del ricalcolo:
  `TestPreparaBloccoImportOraRilettaDopoLAttesa` (adattato da `TestRev2OraVecchiaDopoLAttesa` del revisore; uccide
  P1, ora non riletta).
- **M3 sopravvissuta** (senza `m.dimentica()` un rinnovo fallito fra 23 e 24 h lasciava sbloccare con la memoria
  vecchia fino alle 24 h): il codice era giusto, mancava il test: `TestPreparaBloccoImportRinnovoFallitoBlocca` (uccide
  P2, la stessa mutazione).
- **M5 sopravvissuta** (il prepara della pulizia spostato dentro il lucchetto passa tutti i test): non c'è un test
  economico, perché il gancio del ricalcolo (`duranteRicalcolo`) è del pacchetto `db` e il lucchetto dell'archivio del
  pacchetto `media`; servirebbe un gancio esportato solo per i test. Oggi lo garantisce **solo la lettura del codice**
  (`cancellaInCoda`: prepara prima di `conCancellazioni` e `ConLucchetto`, con il commento che dice perché).
- **Frase sbagliata corretta** (`dopoimport.go`, SVILUPPO «Blocco dopo l'import»): «con un errore la memoria resta
  vuota» era falso; ora: un ricalcolo fallito la svuota, un errore prima del ricalcolo (registro, orologio, `Lstat`,
  ctx) la lascia com'era, ma `BloccatoDopoImport` rifà gli stessi controlli, quindi niente di sbagliato si sblocca.
- **In lista** (difetti n. 8c, 8d): archivio guasto riletto a ogni giro (attesa che cresce, 123 o 124); ctime nella
  memoria, facoltativo.
- Mutazioni dell'implementatore su queste correzioni (`muta3.log`): P1, P2 uccise.

## Difetti trovati per strada (solo in lista)

- La doppia riga della pulizia (difetti in lista n. 8b). Nessun altro.

## Check

- `scripts/check` senza banco (implementatore), prima della revisione: primo giro **rosso** su
  `TestNessunOrologioFuoriDaClock` (`time.Now` in `registro_backup.go`, corretto con `clock.Sistema{}.Adesso`), secondo
  giro **verde**; copertura «voci 353: chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11».
- Dopo le correzioni della revisione: primo giro **rosso** su staticcheck (SA4006, un valore di `prima` non usato in
  `TestBloccatoDopoImportMemoriaScaduta`, corretto), secondo giro **verde**; copertura «voci 353: chiuse 53, rosse 0,
  parziali 192, aperte 97, fuori v1 11» (invariata).
- Dopo la seconda revisione: `scripts/check` **verde** al primo giro; copertura «voci 353: chiuse 53, rosse 0,
  parziali 192, aperte 97, fuori v1 11» (invariata).
