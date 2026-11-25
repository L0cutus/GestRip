# 121 — Verifica completa del backup, comando `gestrip verifica-backup` (T5.2, terza slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 121, risposte A e B, «Cosa fa lo standalone oggi», «Incongruenze trovate» (n. 2), «Rischi»; D-113; regole
  delle 119 e 120 in SVILUPPO.
- **Voci:** OPS-16 (parte: «riuscito solo dopo la verifica completa»), OPS-18 (parte: controlli dell'archivio).
  Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** nessuno (nessuna pagina cambia).

## Cosa c'è

- `internal/backup/verifica.go`: `VerificaArchivio(ctx, dati, file) (Verificato, error)`, errori `ErrVoce`,
  `ErrInventario`, `ErrFotografia`, `ErrIntegrita`, `ErrFineArchivio`, `ErrImpronta`. Regole in SVILUPPO, «Backup:
  verifica completa».
- `internal/backup/archivio.go`: `fotografa` divisa in `fotografa` + `fotografaCon`, così la verifica conta le righe
  con la stessa funzione del backup su un collegamento aperto a modo suo (`trusted_schema` spento).
- `cmd/gestrip/backup.go`, `main.go`: `gestrip backup` verifica l'archivio appena scritto prima di dire «backup
  riuscito»; comando nuovo `gestrip verifica-backup [-db FILE] ARCHIVIO`.
- `docs/SVILUPPO.md` (sezione nuova, sezione 120 aggiornata), `docs/PIANO.md` (§1.2 comandi, §2.5 riga «Verifica»).

### Come fallisce chiusa

- Un solo esito buono: errore nil. Qualunque errore, anche di lettura, di age, di SQLite o del ctx, è «non
  verificato»; non ci sono avvisi che lasciano passare.
- **Lista bianca, non lista nera.** Si controlla esattamente questo (dopo la revisione):
  - intestazioni: tipo `'0'` nel blocco grezzo (il vecchio `'\x00'` il lettore di Go lo trasforma in `TypeReg`: lo
    vede solo il blocco), `Linkname` vuoto, formato USTAR, permessi 0600, uid e gid 0 senza nomi, nessun dispositivo,
    nessun campo PAX tranne `size` per il solo `data/gestrip.db` oltre 8 GiB (uguale alla dimensione), la stessa ora
    in tutte le voci e uguale all'ora di cattura;
  - nomi (lista bianca, anche per `secrets/`), ordine di Scrivi, JSON byte per byte uguali a `json.Marshal`;
  - un solo destinatario age, X25519 (`identitaSola`).
  Non si controllano `AccessTime`/`ChangeTime` (USTAR non li ha). Il contenuto si controlla sempre con lo SHA-256.
  Un archivio fatto con un altro strumento, anche corretto, non passa: va bene, gli archivi li scrive solo il Go.
- La fine del tar e del file si controllano a parte, perché il lettore tar di Go si ferma senza errori anche con un
  solo blocco vuoto, nessuno o con dati dopo.
- `gestrip backup` vuole che il file verificato abbia lo stesso SHA-256 e gli stessi byte di quello scritto.

### Limiti contro un archivio ostile o rovinato

age limita l'intestazione (2 MiB, 1024 destinatari); il lettore tar di Go le intestazioni PAX e i nomi lunghi
(1 MiB); qui: al massimo 200 000 voci, `inventory.json` 32 MiB, `snapshot.json` 1 MiB, media `media.MaxByte`,
segreti `maxSegreto`, database solo se nel volume dei dati c'è la sua dimensione più 10 GiB. In memoria restano solo
nomi e impronte (limitati dalle voci) e i due json. Lo standalone non ha limiti propri (usa `budget` sullo spazio e
il `tarfile` di Python); l'importer del Go non legge archivi tar, quindi niente da riusare.

## Prove (tutte su dati inventati, chiavi age generate nel test)

- `internal/backup/verifica_test.go`: gli archivi ostili si costruiscono da uno buono (Scrivi), scomposto,
  cambiato in un punto, ricomposto e cifrato con la chiave di prova; dove serve l'inventario si rifà apposta, così
  si prova proprio la guardia del caso. Ogni prova controlla che `.verifica-backup-*` non resti.
  - `TestVerificaGiroCompleto`: Cifra → VerificaArchivio, stessi SHA-256, byte, fotografia e inventario; database
    estratto in `<dati>/.verifica-backup-*/` 0700, file 0600.
  - `TestVerificaRifiutaVoci` (26 casi): `..`, assoluti, sottocartelle, nomi non puliti, simbolico, fisico, cartella,
    fifo e dispositivo al loro posto nell'ordine, duplicati, voce dopo snapshot, ordine sbagliato, nomi sconosciuti,
    segreto con una chiave age, permessi, campo PAX, attributo esteso, database/inventario/fotografia mancanti, tar
    vuoto.
  - Dopo la revisione: `TestVerificaSegretiFuoriLista` (chiave dei backup e segreto sconosciuto **al loro posto
    nell'ordine**, prima di `segreto_flask`: li ferma la lista bianca), `TestVerificaIntestazioni` (tipo `'\x00'` con
    il blocco ritoccato a mano, `Linkname` su un file normale, utente/gruppo con nome o numero, formato GNU, un'ora
    diversa, tutte le ore diverse dalla cattura), `TestVerificaDatabaseGrande` (intestazione di 9 GiB scritta da
    `tar.Writer` e riletta senza dati: passa per il database; no per un media, con `size` diverso, con un campo in
    più, con il formato sbagliato), `TestVerificaAltriDestinatari` (archivio cifrato anche per un'altra chiave),
    `TestVerificaAperturaDelDatabase` (`trusted_schema` 0 e sola lettura).
  - `TestVerificaInventario` (16): file in più, mancante, voce tolta, impronta sbagliata di media/database/segreto,
    impronte malformate, non JSON, lista, null, `{}`, numero, spazio, JSON in coda, chiave doppia.
  - `TestVerificaFotografia` (18): conteggi diversi, tabelle in più o in meno, negativi, senza conteggi, migrazioni
    mancanti o fuori sequenza, formato, versione, ora, campo sconosciuto o doppio, spazio, `1.0`, non JSON.
  - `TestVerificaIntegrita`: pagina di un indice rovinata (le tabelle si contano, `integrity_check` no), file che
    non è un database, intestazione rovinata; database vuoto (passa `integrity_check`, non la fotografia).
  - `TestVerificaCifratura`: troncato di un byte, a un pezzo intero, alla sola intestazione, vuoto; byte toccato
    nel mezzo, in fondo, nell'intestazione; byte o pezzo in più dopo la fine; chiave sbagliata; non age;
    collegamento simbolico all'archivio buono, cartella, file assente.
  - `TestVerificaFineDelTar`: spazzatura, blocco o byte zero in più, un solo blocco vuoto, nessuno, mezzo, un tar
    dopo il tar.
  - `TestVerificaLimiti`: voci oltre e al limite, inventario e fotografia oltre il limite, segreto e media troppo
    grandi, PAX di 900 KiB, intestazione «x» di 2 MiB costruita a mano (`ErrFieldTooLong`), nome di 4096 caratteri.
  - `TestVerificaSpazio` (al byte; spazio illeggibile), `TestVerificaContestoEChiave` (ctx finito prima e durante,
    senza chiave).
- `cmd/gestrip/backup_linux_test.go`: `TestBackupComando` (backup → «backup riuscito», poi `verifica-backup` dice lo
  stesso SHA-256), `TestBackupComandoVerificaFallita` (verifica che fallisce o che verifica un altro file: «BACKUP NON
  RIUSCITO», archivio rimasto, errore), `TestVerificaBackupComando` (file non age, senza database, opzioni).
- Test scritti insieme al codice: la prima corsa ha avuto 4 rossi veri (tar di prova che non si scriveva) poi verdi;
  le guardie si sono viste rosse con le mutazioni.
- **Mutazioni a mano** (tutte compilano; `to-be-deleted-after-use/_slice-121/muta.log`, `muta2.log`), 14: **uccise
  14.** M1 tipo della voce non controllato (al primo giro **sopravvissuta**: fifo e collegamenti li fermavano altre
  guardie; uccisa dopo aver messo fifo e dispositivo al loro posto nell'ordine); M2 campi PAX ammessi; M3 ordine non
  controllato; M4 byte dopo il tar ammessi; M5 blocchi vuoti finali non controllati; M6 JSON non canonico ammesso
  (al primo giro non compilava, rifatta come `&& false`); M7 voci dell'inventario mancanti nel tar ammesse; M8
  `integrity_check` ignorato; M9 conteggi non confrontati; M10 limite delle voci tolto; M11 spazio con un byte di
  tolleranza; M12 chiave age nei segreti ammessa; M13 SHA-256 verificato non confrontato con quello scritto; M14
  tabelle del database in più ammesse.
- `scripts/check` senza banco (implementatore): verde; copertura «voci 353: chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11» (invariata: nessun segno nuovo).

- **Mutazioni sulle guardie nuove** (`muta3.log`), 11, **uccise 11:** M15 tipo grezzo non controllato, M16
  `Linkname` ammesso, M17 `size` PAX non confrontato, M18 `size` PAX su ogni voce, M19 formato non controllato, M20
  nomi utente ammessi, M21 ore delle voci non confrontate, M22 ora di cattura non confrontata, M23 più destinatari
  ammessi, M24 `trusted_schema` tolto, M25 uid/gid ammessi.

### `trusted_schema` (R7 della revisione)

Un test «vero» (uno schema che fa girare una funzione bloccata da `trusted_schema`) non è ragionevole: l'ho provato
con CHECK, colonne generate e indici su `random()`, `changes()`, `total_changes()`, `last_insert_rowid()`,
`sqlite_version()`. SQLite rifiuta le funzioni non deterministiche nelle colonne generate e negli indici già alla
creazione, e le funzioni del driver (modernc, senza `load_extension`) sono tutte innocue: `integrity_check` e
`count(*)` danno lo stesso esito con e senza. È una difesa in più per un driver futuro. Il test controlla quindi
l'apertura stessa (`dsnVerifica`): `PRAGMA trusted_schema` vale 0 e una scrittura fallisce.

## Segno di copertura

**Nessun `// inventario:`** né per OPS-16 né per OPS-18. OPS-16 nel destino Go è «backup ogni notte, cifrato,
verificato, su chiavetta USB e SFTP, 7 + 4»: mancano chiavetta (123), lavoro notturno (124) e SFTP (128). OPS-18 è il
ripristino su una macchina nuova: la 121 fa solo i controlli dell'archivio che il ripristino userà; la voce chiude con
`gestrip restore` (130). I test portano il commento «in parte», come nelle 119 e 120.

## Per il manuale

- Dopo aver scritto l'archivio, `gestrip backup` lo **riapre e lo controlla tutto**: lo decifra, controlla ogni
  file, controlla che il database sia integro e che abbia le stesse righe del momento del backup. Solo allora dice
  «backup riuscito».
- Se il controllo non passa il comando scrive **BACKUP NON RIUSCITO**, dice dove è rimasto l'archivio (non lo
  cancella) ed esce con un errore. Quell'archivio non conta come backup.
- Un archivio già scritto (per esempio sulla chiavetta) si controlla con `gestrip verifica-backup ARCHIVIO`: dice
  quando è stato fatto, quanti file contiene, e «verificato»; altrimenti «NON verificato» e il motivo.
- Il controllo serve la chiave dei backup sull'apparecchio e, per un momento, spazio nel disco dei dati per una copia
  del database (più 10 GB di margine); la copia si toglie subito dopo.
- Un archivio toccato, troncato, aperto con un'altra chiave o con file in più o in meno non passa mai.

## Differenze dallo standalone

- Integrità completa e conteggi a **ogni** backup (D-113); lo standalone li fa solo nella prova settimanale e al
  ripristino.
- Decifratura in flusso: il tar non tocca il disco (lo standalone lo decifra in un file temporaneo anonimo); solo il
  database si estrae, nel volume dei dati.
- Più severa: ordine delle voci, permessi, niente campi PAX, JSON canonici, fine del tar esatta, nessuna cartella
  ammessa (lo standalone ammette `data`, `media` come cartelle e qualsiasi ordine).
- Segreti nell'archivio (D-10) controllati per nome; una chiave age dentro è un rifiuto.

## Dubbi

1. **Impronta dei media uguale al nome:** le chiavi dei media sono lo SHA-256 del contenuto; la verifica controlla
   solo che l'impronta sia quella dell'inventario, non che sia uguale al nome. Controllarlo troverebbe un media già
   rovinato nell'archivio dei dati, ma da quel giorno **ogni** backup fallirebbe finché qualcuno non lo toglie.
   Decisione di prodotto: lasciato fuori, da decidere (raccomando di sì, insieme all'allarme della 126).
2. **Ordine obbligatorio delle voci:** lega la verifica a Scrivi. Un cambio dell'ordine in Scrivi senza cambiare la
   verifica fa fallire tutti i backup (lo prende subito `TestVerificaGiroCompleto`). Scritto in SVILUPPO.
3. **Spazio per la verifica = dimensione del database + 10 GiB**, lo stesso margine del backup. Su un disco quasi
   pieno il backup passa il suo controllo e la verifica no: va bene (fallisce chiusa), ma l'allarme (126) lo dirà
   come «backup fallito».
4. **Cartelle temporanee rimaste dopo un blackout** (`.backup-*`, `.verifica-backup-*`): nessuno le toglie all'avvio.
   Contengono il database in chiaro nel volume cifrato. Da decidere con la 124 (pulizia all'avvio del lavoro).
5. **Due verifiche insieme** sommano lo spazio senza saperlo (come la nota 6b sui backup).
6. LSP (gopls): usato per i riferimenti di `spazioLibero` (tutti i chiamanti e le sostituzioni nei test) prima di
   usarlo nella verifica; su `fotografaCon` gopls non ha trovato l'identificatore (file appena cambiato), lì grep.

## Revisione (Opus, ~75k token, ~15 minuti, 10 mutazioni)

Nessun difetto ALTA.

- **MEDIA, corretto:** un database oltre 8 GiB fa scrivere a `tar.Writer` un campo PAX `size`
  (`map[size:9663676416]` per 9 GiB): la verifica rifiutava ogni campo PAX, quindi **ogni** backup sarebbe fallito
  per sempre. Ora `size` è ammesso da solo, uguale alla dimensione, solo per `data/gestrip.db`
  (`TestVerificaDatabaseGrande`, senza scrivere 8 GiB).
- **BASSA, corretto:** un archivio cifrato per la nostra chiave e per un altro destinatario passava: ora
  `identitaSola` vuole un solo destinatario X25519 (`TestVerificaAltriDestinatari`).
- **BASSA, corretto:** i testi esageravano «passa solo quello che Scrivi scrive»: passavano formato GNU, utente e
  gruppo con nome, tipo `'\x00'`, `Linkname` su un file normale, uid/gid/ora mai controllati. Ora sono rifiutati
  (sopra) e i testi dicono esattamente cosa si controlla.
- Mutazioni del revisore: **uccise** R1, R3, R6, R8. **Sopravvissute:** R2 (`rango` che ammette ogni nome in
  `secrets/`: la fermava l'ordine) → ora uccisa da `TestVerificaSegretiFuoriLista`; R7 (`trusted_schema` tolto) → ora
  uccisa da `TestVerificaAperturaDelDatabase` (vedi sopra perché non con uno schema); R9 (`Linkname` ammesso) → ora
  uccisa da `TestVerificaIntestazioni` (M16); R4, R5, R10 equivalenti o quasi.
- **In lista** (`docs/domande-aperte.md`, nota 7): (a) schema del database non confrontato (130); (b) rilettura
  dalla cache delle pagine subito dopo l'fsync (123, `posix_fadvise(DONTNEED)`); (c) `-wal`/`-shm` accanto alla copia
  temporanea, innocui; più le domande per Stefano dei Dubbi 1 e 4 e il difetto `NOT LIKE`.

## Difetti trovati per strada (solo in lista)

- (In `docs/domande-aperte.md`, nota 7f.) `fotografa` esclude le tabelle con `NOT LIKE 'sqlite_%'`: in LIKE `_` è un jolly e non distingue maiuscole, quindi
  esclude anche una tabella chiamata per esempio `sqliteX…`. Oggi nessuna tabella nostra ha quel nome; innocuo, ma
  meglio `substr(name,1,7) <> 'sqlite_'`. Non toccato.
- Nessun altro.
