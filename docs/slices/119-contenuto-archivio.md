# 119 — Contenuto dell'archivio del backup, senza cifratura (T5.2, prima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 119, risposta B, «Incongruenze» 1 e 4, «Rischi» (media cancellati durante il backup); D-10, D-55, D-113.
- **Voci:** OPS-16 (parte: contenuto, inventario, fotografia, spazio), OPS-27 (parte backup: segreto Flask
  nell'archivio). Nessuna D nuova (ultima usata D-115).
- **Modelli:** implementatore Opus (~110k token di contesto, ~45 minuti).
- **Banco:** nessuno (la slice non cambia pagine; piano: «nessuno scenario»).

## Cosa c'è

- Pacchetto nuovo `internal/backup`: `Scrivi(ctx, Sorgente, w)` scrive in `w` il tar con `data/gestrip.db`,
  `media/<chiave>`, `secrets/segreto_flask`, `secrets/segreto_standalone`, `inventory.json` (SHA-256 di ogni
  file) e `snapshot.json` (formato 1, versione del binario, ora della copia, migrazioni, conteggi). Restituisce
  fotografia, inventario e byte scritti. Dettagli e ordine dei passi in SVILUPPO, «Backup: contenuto
  dell'archivio».
- Copia a caldo: `db.CopiaACaldo` (nuova, esportata) è il pezzo «riserva il nome + `VACUUM INTO`» tolto da
  `backupPrima`, che ora la usa; comportamento di `backupPrima` invariato (test di `internal/db` verdi).
  `db.DB.Percorso()` nuovo.
- La copia sta in `<cartella dei dati>/.backup-XXXX/` (0700) e si toglie sempre; conteggi sulla copia, senza
  `eventi_audit` e senza `sqlite_*` (come `snapshot_repo!`).
- Spazio: database + WAL + 10 GiB liberi nel volume dei dati, altrimenti `ErrSpazio` prima di qualsiasi
  scrittura; statfs solo su Linux (fuori da Linux rifiuta, come l'import).
- Pulizia dei media: `media.Archivio.SenzaCancellazioni` (RWMutex nuovo, separato dal lucchetto dei
  salvataggi); `Pulizia.CancellaInCoda` e `CancellaInCodaConLucchetto` aspettano mentre il backup legge.
- Chiave dei backup (D-113): elenco chiuso dei segreti che entrano (`segretiNellArchivio`) e di quelli fuori
  (`segretiFuori`: `chiave_backup`); file sconosciuto in `secrets/` → backup fermo; `AGE-SECRET-KEY-` in un
  segreto ammesso → `ErrChiaveBackupNellArchivio`, prima di scrivere. Regole per la 120 in SVILUPPO.
- PIANO §1.9 corretto: in `secrets/` anche la chiave **privata** dei backup (D-113), mai nell'archivio.

## Prove (tutte su dati inventati, in cartelle temporanee dei test)

- `TestArchivioContenuto`, `TestArchivioInventario`, `TestArchivioFotografia`, `TestArchivioConteggiSullaCopia`
  (una riga scritta dopo la copia non entra nei conteggi), `TestArchivioSpazioInsufficiente` (al byte: un byte
  in meno rifiuta, lo spazio esatto parte; statfs illeggibile rifiuta), `TestArchivioCopiaNelVolumeDeiDati`,
  `TestArchivioErroreDelloScrittore`, `TestArchivioLaPuliziaAspetta` (la foto in coda resta durante il backup ed
  entra nel tar, un salvataggio intanto passa, a backup finito la pulizia la toglie), `TestNomeSicuro`,
  `TestArchivioMediaInattesi` (collegamento, sottocartella, nome strano), `TestArchivioChiaveDeiBackupFuori`.
- Rosso prima: il pacchetto di prova scritto prima del codice non compilava. Lucchetto della pulizia scritto
  prima del suo test: il test è stato visto rosso togliendo il lucchetto (sotto).
- Mutazioni a mano dell'implementatore (compilano): lucchetto della pulizia tolto → uccisa; `SenzaCancellazioni`
  saltato → uccisa; confine dello spazio `< serve-1` e `<= serve` → uccise; controllo `AGE-SECRET-KEY-`
  indebolito → uccisa; file sconosciuto ammesso → uccisa; temporanei `.upload-` non saltati → uccisa; audit nei
  conteggi → uccisa. **Sopravvissuta:** togliere `nomeSicuro` dall'intestazione del tar (tutti i nomi arrivano già
  da fonti controllate: è una difesa in più, provata da sola con `TestNomeSicuro`).
- `scripts/check` verde (implementatore, senza banco e senza `GESTRIP_BANCO_ESITO`, che è facoltativa): voci 353, chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11.

## Per il manuale

- Nessun comportamento visibile nuovo: il backup non si lancia ancora (comando con la 120, lavoro notturno con la
  124). Da scrivere nel capitolo 11, quando ci saranno:
- Il backup si fa con il programma acceso: il negozio può lavorare mentre gira.
- Il backup contiene il database, tutte le foto e i documenti, il segreto delle vecchie garanzie e il segreto del
  vecchio programma. **Non** contiene la chiave dei backup: quella sta sull'apparecchio e sul foglio di carta.
- Se nel disco dei dati restano meno di 10 GB liberi oltre al database, il backup non parte.
- Mentre il backup legge le foto, le foto da cancellare aspettano la fine del backup; caricare foto nuove si può.

## Differenze dallo standalone

- A caldo invece che con l'app ferma (D-10, già decisa); `secrets/` nell'archivio (D-10, OPS-27).
- `snapshot.json` ha chiavi italiane e le migrazioni al posto di `image` e dell'impronta della prima riparazione
  (`repair_digest`): l'archivio del Go non si ripristina nello standalone e viceversa.
- Più severo: un file inatteso in media o in `secrets/` ferma il backup (lo standalone mette nel tar tutto quello
  che trova sotto `data/` e `media/`, rifiutando solo collegamenti e file speciali).

## Dubbi

1. **Cartelle `.backup-XXXX` rimaste dopo un blackout** (copia del database in chiaro, nel volume cifrato):
   nessuno le toglie. Da decidere con la 123 (pulizia dei `.pending`) o alla 124 all'avvio.
2. **Oggetto media con contenuto diverso dal nome** (guasto): entra com'è, con l'impronta vera nell'inventario;
   la 121 lo vedrà uguale all'inventario. Non fermo il backup per un oggetto guasto; da decidere se segnalarlo.
3. **Un file sconosciuto in `secrets/` ferma il backup** (scelta mia, prudente): il backup fallisce finché qualcuno
   non decide. L'allarme della 126 lo mostrerà; un'alternativa è saltarlo con un avviso.
4. **Margine di 10 GiB** come lo standalone, senza la sua regola «disco al 90%»; su un apparecchio con disco
   piccolo potrebbe essere tanto. Da rivedere con la T5.4 (dimensione del disco).
5. **Chiavi del json in italiano** (`formato`, `versione`, `catturato_il`, `migrazioni`, `conteggi`); i nomi dei
   file restano quelli dello standalone.
6. `versione` viene da `debug.ReadBuildInfo` (versione del modulo e commit): nei binari costruiti senza
   informazioni VCS sarà `(devel)`. Il programma non ha ancora un numero di versione suo (T5.3).
7. **Segno di copertura:** i test non portano `// inventario: OPS-16` né `OPS-27`, perché `tools/copertura`
   chiuderebbe le voci (non accetta «parte»); il segno lo mette la slice che le chiude (121/130). Da confermare.
8. LSP (gopls): usato poco, per i riferimenti di `CancellaInCodaConLucchetto` e `backupPrima`; il resto con grep e
   letture mirate.

## Difetti trovati per strada (solo in lista)

- Nessuno nuovo. Il commento di `db.BloccatoDopoImport` resta valido (la condizione sul backup verificato arriva
  con la 122).

## Revisione (Opus, ~45k token, ~12 minuti)

Nessun difetto ALTA né MEDIA. Tre BASSA:

1. **Stop di serve appeso durante un backup** (`internal/media/pulizia.go`, `giro`): `cancellazioni.Lock()` non
   guarda il ctx, e `lavori.Wait()` aspetta la pulizia; il backup guarda il ctx solo fra un file e l'altro, non dentro
   `io.Copy` verso una destinazione lenta. Oggi non succede (il backup non si lancia ancora): **da tenere nel brief
   della 120 e della 124** (backup legato al ctx, o attesa interrompibile).
2. Precisione dei documenti: le scritture nel database aspettano `VACUUM INTO` (connessione di scrittura unica).
   **Corretto** in SVILUPPO e nel commento di `SenzaCancellazioni`.
3. Il lucchetto in `CancellaInCodaConLucchetto` non ha un test (mutazione sopravvissuta); oggi la funzione non ha
   chiamanti fuori dal pacchetto. In lista.

Mutazioni del revisore: 13 provate, 11 uccise; sopravvissute il punto 3 e `ToUpper` tolto nella guardia della
chiave age (innocua: age scrive le chiavi in maiuscolo; manca solo un test in minuscolo). In lista.
