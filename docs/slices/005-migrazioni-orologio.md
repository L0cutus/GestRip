# 005 — Migrazioni e orologio del negozio (T1)

- **Fonte:** PIANO §1.4 e §1.7; inventario JOB-01; `go-baseline:lib/gestrip_standalone/jobs/clock.ex`
  e `clock_test.exs`.
- **Confine:** migrazioni proprie solo in avanti, con backup prima di migrare un database con dati
  (segnaposto fino a T5.2); orologio unico Europe/Rome indipendente dal fuso del sistema; nessun
  pacchetto del prodotto legge l'orologio del sistema fuori da `internal/clock`.
- **Voci:** JOB-01 (chiusa). Le migrazioni non chiudono voci: OPS-20 (backup prima
  dell'aggiornamento) si chiude con T5.2.
- **Livello:** completo (schema e dati).

## Cosa c'è

- `internal/clock`:
  - `Roma` dai dati `time/tzdata` incorporati;
  - `Orologio`, con `Sistema` (UTC) e `Finto` (fermo, `Sposta`, `Imposta`, sicuro tra goroutine);
  - `Locale`, `Formatta`, `Timbro` («gg/mm/aaaa hh:mm»);
  - `Data` (giorno senza fuso), `DataDi`, `Oggi`, `Sub`.
- `guardia_test.go`: nessun `time.Now/Since/Until/Local/LoadLocation` né `.Local()` in `cmd/` e
  `internal/` fuori da `internal/clock` (file non di test), anche con l'import rinominato o `.`.
- `internal/db/migra.go`:
  - file `migrazioni/NNNN_nome.sql` incorporati, numerati da 0001 senza buchi;
  - `schema_migrations` (versione, nome, sha256 del file, applicata_il);
  - ogni migrazione nella sua transazione insieme alla sua riga;
  - rifiuta un database più nuovo del programma, una storia diversa (nome o buco) e un database
    con tabelle ma senza `schema_migrations`;
  - prima di migrare un database che ha già migrazioni: `VACUUM INTO` in
    `<db>.prima-di-NNNN-<aaaammggThhmmssZ>`, con `quick_check`; se non riesce non migra.
- `0001_impostazioni.sql`: chiave, valore, aggiornata_il, `STRICT` (la regola di `settings`).
- `gestrip serve` migra prima di aprire le facce; un errore di migrazione ferma l'avvio.
- `db.FormatoIstante`: istanti come testo UTC con microsecondi.

## Prove

- TDD: test dell'orologio, della guardia, delle migrazioni e di `serve` scritti prima, rossi
  (non compilavano; il test di `serve` falliva su un database estraneo accettato).
- Vettori dell'orologio copiati da `clock_test.exs`: inverno, estate, sera tardi UTC, capodanno,
  i due cambi dell'ora del 2026.
- **Scoperta:** con modernc, `VACUUM INTO` su un file che esiste già e non è vuoto lo
  **sovrascrive senza errore** (prova in `to-be-deleted-after-use/prova-vacuum`). Il nome del
  backup si riserva quindi con una creazione esclusiva (`O_EXCL`) del file vuoto; un test prova che
  un file esistente resta intatto e che la migrazione non parte.
- Mutazioni (rifatte dopo la review, su suite verde): 43 diverse sul codice finale, comprese quelle
  per le correzioni. Vive solo due, dichiarate:
  - il controllo `quick_check` del backup: non so produrre in un test una copia corrotta da
    `VACUUM INTO` (il conteggio delle migrazioni nella copia invece è provato);
  - l'errore di `tx.Commit()` ignorato: non so farlo fallire in un test.

  Tolti quattro controlli ridondanti, che le mutazioni hanno mostrato equivalenti: `IsDir`, il
  doppio «database estraneo», il caso `.` della guardia, l'escape `''` nelle stringhe (per trovare
  la fine del testo due stringhe attaccate valgono come una).
- `go test -race` verde su `internal/` e `cmd/`.
- `scripts/check` verde; copertura: JOB-01 chiusa (ADM-20 è parziale senza l'esito del banco).

## Review indipendente

Fatta da un'altra sessione, rileggendo dalle fonti (PIANO e `go-baseline`). Esito e correzioni:

- **ALTA, errore mio:** `TestOrologi` era **rosso** nella versione data alla review. Avevo
  aggiunto un caso al test senza rilanciare il controllo completo, quindi anche le prime mutazioni
  sull'orologio non valevano. Corretto; mutazioni rifatte su suite verde.
- **MEDIA, corrette:**
  - un `COMMIT`, `BEGIN`, `ROLLBACK`, `END`, `SAVEPOINT` o `RELEASE` dentro un file `.sql`
    rompeva l'atomicità: database mezzo migrato e poi non più avviabile. Ora `PRAGMA`, queste
    istruzioni, `VACUUM`, `ATTACH` e `DETACH` all'inizio di uno statement sono rifiutati prima di
    toccare il database. Il controllo salta commenti, stringhe e identificatori e permette il
    `BEGIN…END` del corpo di un trigger;
  - `PRAGMA foreign_keys=OFF` dentro la transazione non ha effetto e un `DROP TABLE` della
    madre cancellava in cascata le figlie (provato dal revisore). Ora il `PRAGMA` è rifiutato; la
    ricostruzione di una tabella con figlie resta un debito (sotto);
  - un database estraneo veniva modificato (creata `schema_migrations`) prima del rifiuto. Ora i
    controlli vengono prima di ogni scrittura. Resta il passaggio a WAL fatto da `Apri` (debito);
  - un database dello standalone (Ecto) dava un errore oscuro: ora dice che si importa e non si
    migra;
  - la guardia non vedeva `time.Unix/UnixMilli/UnixMicro` (istanti in `time.Local`) e
    `time.After/Tick/NewTicker/NewTimer` (ora del sistema sul canale): ora li vieta;
  - `Data.Sub` saturava oltre circa 292 anni: ora conta sui secondi Unix.
- **BASSA, corrette:**
  - impronta sha256 di ogni migrazione in `schema_migrations`: una migrazione cambiata dopo
    l'applicazione ferma l'avvio;
  - il backup fallito o non verificato viene tolto;
  - la verifica controlla anche il numero di migrazioni nella copia, perché un file vuoto passa
    `quick_check`;
  - nome del backup al millisecondo;
  - `.UTC()` esplicito prima di scrivere un istante;
  - commenti imprecisi su `VACUUM INTO` e su `time/tzdata`;
  - il test di `Oggi` confronta con la data UTC, come lo standalone.
- **BASSA, dichiarate e non corrette:**
  - un solo backup per tutte le migrazioni in sospeso: se fallisce la 0003 dopo la 0002, la
    copia è quella di prima della 0002;
  - `Data` non controlla date impossibili (`{2026, 2, 30}`): oggi `Data` nasce solo da
    `DataDi`;
  - manca l'ora del giorno in negozio (`local_time/1`): arriva con i job;
  - la guardia non vede `os.FileInfo.ModTime()` e dà falsi positivi su un campo chiamato
    `Local`, dal lato sicuro.

## Debiti

- Il backup accanto al database è un segnaposto: cifratura, conservazione e verifica completa
  arrivano con T5.2 (OPS-20).
- **Ricostruzione di una tabella con chiavi esterne:** con le chiavi esterne accese, il
  `DROP TABLE` della madre cancella le figlie in cascata. `PRAGMA foreign_keys` non si può cambiare
  dentro una transazione, e nelle migrazioni il `PRAGMA` è vietato. La prima migrazione che ne avrà
  bisogno porta una procedura sua: chiavi esterne spente fuori dalla transazione e
  `foreign_key_check` prima del commit.
- `Apri` porta in WAL anche un file che poi la migrazione rifiuta: un database estraneo aperto per
  sbaglio cambia modo del giornale, ma non i dati.
