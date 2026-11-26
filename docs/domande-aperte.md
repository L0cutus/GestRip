# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

Slice 037c (CI instabile, errori di risorse del processo delle immagini). Nessuna blocca il lavoro.

6. **Memoria e CPU danno anch'esse «riprova»,** anche se per la stessa foto si ripeterebbero.
   Misurato: con le foto che si accettano il tetto ha almeno 125 MiB di margine e la CPU usata è
   circa 2,5 s su 8. **Raccomandazione:** tenerlo così.
   Va rivista alla T5 con la misura della CPU sull'apparecchio (Stefano, 01/11/2026).

Slice 107 (pagina /settings/assistente). Nessuna blocca il lavoro (coordinatore, 20/11/2026).

7. **Testi dell'estrazione dopo la D-103.** La pagina tiene i testi dello standalone: «Un server compatibile con le API di
   chat: Ollama, llama.cpp, vLLM» e il segnaposto «qwen2.5:7b» del modello. Con il formato corto della D-103 un modello
   generico non basta: il server deve servire un modello addestrato per GestRip (o un adattatore). **Raccomandazione:** il
   coordinatore porta a Stefano una proposta di D per la T5.1 (testi e segnaposto riscritti insieme alla scelta del modello).
8. **Seconda serratura nel client del modello.** La regola «la chiave non viaggia in chiaro verso un'altra macchina» vale
   solo al salvataggio della pagina (come nello standalone): `Collegamento.Chiedi` (slice 106) manda la chiave a qualunque
   indirizzo letto. Verificato: l'import copia le `assist.ai.*`/`assist.stt.*` così come sono (indirizzi e glossario in
   `impostazioni`, le due chiavi in `segreti_provider`, `importer.righeDi`), senza la validazione del salvataggio; nello
   standalone erano già passate dalla sua, ma un database sorgente ritoccato a mano porterebbe nel Go una chiave con un
   indirizzo `http` remoto, e il client la manderebbe. **Raccomandazione:** come la seconda serratura dell'SMTP (069), il
   client tratta come non configurata un'estrazione con chiave e indirizzo `http` fuori da questa macchina. In lista, non
   fatta.

Slice 108 (revisione della nuova busta con il dettato). Nessuna blocca il lavoro (coordinatore, 20/11/2026).

9. **Test instabili sotto carico: `TestPaginaClienteMostra` e `TestStimaNegozioGuasto` (`internal/web`).** Visti cadere dal
   revisore della 108 con la macchina carica (mutazioni in parallelo), verdi da soli e in `scripts/check`. Non toccati dalla
   108. **Raccomandazione:** guardare dove aspettano tempi fissi o l'orologio vero; in lista, non corretti.

Slice 115 (raccolta dei dettati: scheda e durata). Nessuna blocca il lavoro (coordinatore, 24/11/2026).

11. **Una busta nel cestino tiene il suo dettato raccolto.** Nello standalone il dettato sparisce subito quando la busta va
   nel cestino: `Trash.discard/4` chiama `Training.forget_repair/2` nella stessa transazione
   (`lib/gestrip_standalone/trash.ex`, riga 243 a `go-baseline`). In più il giro giornaliero `Training.expire/1`
   (`training.ex`, righe 352-363) cancella anche i dettati delle buste già nel cestino. Nel Go il cestino arriva con la
   T4.11: oggi nessuna azione mette una busta nel cestino, e la scadenza della 115 cancella solo i dettati oltre 12 mesi.
   Quindi una busta importata già nel cestino con un dettato lo tiene fino ai 12 mesi. Il testo della pagina dice invece
   «Il dettato sparisce quando la busta va nel cestino». **Raccomandazione:** nella T4.11, con il cestino, fare tutte e due
   le parti dello standalone (al cestino e nel giro giornaliero), così anche le buste importate già nel cestino perdono il
   dettato al primo giro.
12. **La busta «N. 45/26» senza etichetta non si esclude dalla pagina se esiste l'etichetta «0045/26».** Misurato con un
   test (`TestRaccoltaEscludiClienteDallaSchedaSenzaEtichetta`): nella pagina «Assistente e dettatura» «45/26», «0045/26» e
   «45» trovano tutte e due le buste, quindi la D-109 rifiuta con «Più buste corrispondono a…» e non cancella niente;
   «N. 45/26» non è un numero che la pagina legge («Nessuna busta con questo numero.»). Il blocco vale per tutte e due le
   buste, anche per quella con l'etichetta. Dalla scheda (115) il cliente di ognuna si esclude, ma l'esclusione di una sola
   busta (senza il cliente) resta impossibile. **Raccomandazione:** se serve escludere una busta sola, aggiungere alla
   pagina il numero interno scritto come lo mostra la scheda («N. 45/26»), che indica una sola busta; altrimenti basta la
   scheda.

Slice 116 (pseudonimi dell'esportazione dei dettati). Nessuna blocca il lavoro (coordinatore, 24/11/2026).

13. **Prezzi in fila riscritti come un telefono quando il telefono del cliente è nel testo.** Misurato (implementatore
   della 116): «prezzi 350 400 450 euro» e «preventivo 340 350 400» hanno la forma di un telefono anche per lo
   standalone (cifre unite da un solo spazio, prima cifra 3). Senza il telefono del cliente la riga si scarta
   (`telefono_dubbio`); con il telefono del cliente nel testo lo standalone, e il Go come lui, riscrivono quei numeri con
   un telefono inventato: nessun dato vero esce, ma i prezzi del file di addestramento si rovinano senza scarto.
   **Raccomandazione:** lasciare così (i prezzi in fila sono rari nei dettati); in alternativa, con il telefono del
   cliente nel testo, scartare invece di riscrivere un secondo numero fatto di gruppi di 3 cifre separati da spazi.
14. **La 117 deve leggere il JSON dei dettati con `UseNumber`.** I pseudonimi (116) leggono come testo stringhe,
   `json.Number`, interi e `float64` interi esatti; un decimale dà `valore_illeggibile`. Con `UseNumber` nessun numero
   passa da `float64`. Da applicare nella 117 (ok del coordinatore, 24/11/2026).
15. **Pseudonimi: due casi lasciati come lo standalone (116).** (a) Un segno ASCII dentro un nome («Ros-si», «Ros.si») lo
   spezza e il nome passa, come nello standalone: la pulizia della D-111 tocca solo i segni non ASCII, perché il
   trattino serve («porta-anelli») e una trascrizione così è poco realistica. (b) Nei campi, un telefono che le forme
   dello standalone riconoscono («0423 52.12.34») si sostituisce invece di scartare la riga; non esce in chiaro.
   **Raccomandazione:** lasciare così.

## Difetti in lista

Difetti vecchi trovati nelle review, rari e senza danno oggi: da correggere più avanti, nella slice
dove cadono meglio (coordinatore, 07/11/2026).

1. **Numero con zeri e numero del contatore insieme (review 055a).** Un «0046/26» importato non
   blocca il «46/26» dato dal contatore, perché l'indice unico di `buste.numero` confronta il testo
   esatto: nascono due buste che per il negozio hanno lo stesso numero. **Deciso da Stefano il
   07/11/2026 (D-77, `docs/SVILUPPO.md`):** all'import i numeri con zeri davanti si riscrivono senza
   zeri, con un avviso nel report. **Fatto nella slice 057** (`internal/importer/riscritture.go`):
   dopo l'import «0046/26» è «46/26» e il contatore che dà «46/26» trova il numero occupato e si
   riallinea (055a). Resta aperto, come domanda per Stefano, il caso del numero senza zeri già di
   un'altra busta: per ora non si riscrive e il report lo dice in un avviso a parte
   (`buste/numero_senza_zeri_occupato`), quindi lì il difetto resta finché non si decide.
2. **Esiti quando l'audit di una lettura fallisce (review 055b).** Lo standalone risponde 404 per una
   riga rifiutata e 500 per un errore SQL; il Go risponde 500 per la foto e 404 per la firma con
   qualunque errore. Non escono mai byte; ci si arriva solo con un guasto del database. Il commento
   di `internal/web/firma.go` («lo standalone risponde lo stesso 404») vale solo per la riga
   rifiutata. Lo stesso vale per le foto storiche `/foto` e `/thumb` della slice 064 (500 dove lo
   standalone risponde 404).
3. **Copertura che chiude voci ancora parziali (review 063).** Lo strumento di copertura
   (`tools/copertura`, lanciato da `scripts/check`) chiude una voce quando tutti i suoi test sono
   verdi. Per questo CLI-14 e RIP-S-20 risultano chiuse, anche se mancano gli avvisi
   WhatsApp/Telegram dopo la risposta del negozio (T4.6/T4.7), e lo stesso vale per CLI-15, a cui
   manca l'aggiornamento in tempo reale della campanella. Serve un modo per tenere una voce
   «parziale» finché manca un pezzo. Nell'inventario privato queste voci sono segnate «Parziale».

4. **Comando `esporta-dettati`: quattro difetti BASSI della 117 (revisione Opus indipendente, 24/11/2026).** Solo in lista,
   niente correzioni senza un nuovo ok di Stefano (coordinatore, ok di Stefano del 24/11).
   - `cmd/gestrip/esporta_dettati.go:51-58`: `senzaPercorso` toglie il percorso solo da un `*fs.PathError`, ma `db.Apri`
     restituisce «apertura di %s: %w»: con un database rovinato il messaggio contiene il percorso del database (che può
     contenere un nome), contro il commento.
   - `cmd/gestrip/esporta_dettati.go:70-74`: se la stampa del resoconto su stdout fallisce dopo un'esportazione riuscita,
     il comando dice «Esportazione non riuscita» ed esce con 1, ma le righe sono già cancellate e il file c'è.
   - `internal/buste/esporta_dettati.go:127`: se `os.Remove` del file a metà fallisce, l'errore è ignorato in silenzio: il
     file resta e il giro dopo è rifiutato come «file esiste già».
   - Decimali con `UseNumber`: «35.50» resta «35.50», lo standalone scrive «35.5» («1e2» diventa «100.0»); in casi rari
     cambia `campi_corretti`. Differenza da annotare, non da correggere.

5. **Backup: tre difetti BASSI della 119 (revisione Opus, 25/11/2026).** (a) Il giro della pulizia aspetta
   `cancellazioni.Lock()` senza guardare il ctx: con il backup dentro serve, lo stop resta appeso finché il backup
   non finisce; il backup guarda il ctx solo fra un file e l'altro. **Chiuso nella 120:** attese interrompibili
   (`aspettaFinche`) e ogni scrittura del tar legata al ctx. (b) Il lucchetto in `CancellaInCodaConLucchetto` non ha
   un test (oggi nessun chiamante fuori dal pacchetto). **Chiuso nella 120:** `TestAttesaDellaPuliziaSiInterrompe`
   lo prova (senza lucchetto la funzione non aspetterebbe). (c) La guardia `AGE-SECRET-KEY-` non ha un test con la chiave in minuscolo.
   Dettagli in `docs/slices/119-contenuto-archivio.md`, «Revisione».
6. **Backup: difetti BASSI latenti della 120 (revisione Opus, 25/11/2026).** (a) Ordine dei lucchetti invertito:
   `cancellaInCoda` prende RWMutex+flock e poi `mu` (senza ctx), `CancellaInCodaConLucchetto` prende `mu` e poi
   RWMutex+flock. Oggi nessun chiamante in produzione della seconda: da sistemare quando arriva il primo
   (probabilmente T4.11, cestino). (b) Due backup insieme passano tutti e due (flock condiviso, controlli dello
   spazio non sommati): **124**, escludere anche il processo da console. **Chiuso nella 124:** il lucchetto dei
   backup (`backup.PrendiLucchetto`, flock esclusivo su `<dati>/.lucchetto-backup`, senza aspettare) lo prendono il
   backup notturno, «Esegui adesso», `gestrip backup` e `gestrip verifica-backup`; il secondo rifiuta con
   `ErrBackupInCorso`, anche fra processi (`TestLucchettoFraProcessi`), quindi anche i controlli dello spazio non si
   sovrappongono. (c) `renameat2(RENAME_NOREPLACE)` dà EINVAL
   su CIFS, 9p e FUSE senza rename2 (per esempio una chiavetta con ntfs-3g): il backup fallisce sempre, chiuso ma
   senza un messaggio chiaro. **123:** prova sul file system vero della chiavetta e messaggio chiaro. **In parte
   chiuso nella 123:** con EINVAL/ENOSYS ripiego su `link` + `unlink` (non sovrascrive mai) e, se nemmeno link va, un
   messaggio chiaro (`ErrRinominaNonSupportata`: formattare in exFAT o ext4). Provato solo con errori simulati: la
   prova sulla chiavetta vera (exFAT, e NTFS con ntfs-3g) resta a Stefano (T5.4, vedi la ricevuta della 123). (d) Un
   temporaneo `.chiave_backup-*` rimasto da un'interruzione bloccava i backup con un messaggio muto: **corretto
   nella 120** (il messaggio dice di toglierlo a mano; `TestTemporaneoDellaChiaveRimasto`). (e) La chiave non
   mostrata si toglieva senza fsync della cartella: **corretto nella 120** (fsync dopo la rimozione; il test vede la
   rimozione, non l'fsync). Dettagli in `docs/slices/120-chiave-cifratura-backup.md`, «Revisione».
7. **Backup: difetti BASSI e domande della 121 (revisione Opus e implementatore, 25/11/2026).** (a) Lo **schema** del
   database estratto non si confronta: un database con le stesse migrazioni e gli stessi conteggi ma uno schema
   diverso passa, e age non autentica chi ha scritto l'archivio (chiunque abbia la chiave pubblica può cifrarne
   uno). **130:** il ripristino confronti lo schema con quello delle migrazioni. (b) `gestrip backup` rilegge
   l'archivio subito dopo l'fsync, dalla cache delle pagine: non prova che i byte siano sul supporto. **123:**
   `posix_fadvise(DONTNEED)` dopo l'fsync, prima di rileggere. **Chiuso nella 123** (solo un aiuto: il kernel può
   ignorarlo e la cache del dispositivo resta; un errore non ferma il backup). (c) `mode=ro` su un database con l'intestazione WAL
   crea `-wal`/`-shm` accanto alla copia temporanea: innocuo, si tolgono con la cartella. (d) **Domanda per
   Stefano:** la verifica non controlla che l'impronta di un media sia uguale al suo nome (le chiavi dei media sono
   lo SHA-256 del contenuto). Controllarlo troverebbe un media già rovinato nei dati, ma da quel giorno ogni backup
   fallirebbe finché qualcuno non lo toglie. Raccomandazione: sì, insieme all'allarme della 126, con un messaggio
   che dice quale foto. (e) **Domanda per Stefano:** cartelle temporanee rimaste dopo un blackout
   (`<dati>/.backup-*`, `<dati>/.verifica-backup-*`, con il database in chiaro dentro il volume cifrato): nessuno le
   toglie. Raccomandazione: le toglie il lavoro notturno (124) prima di partire, solo se nessun backup è in corso.
   **Deciso dal coordinatore, 124** (25/11/2026, con la 122): la pulizia di queste cartelle va nella 124, all'avvio di
   `serve` e prima di ogni backup, solo con il lucchetto preso e solo per le cartelle che hanno il nostro schema di
   nome (`.backup-*`, `.verifica-backup-*` nella cartella dei dati). **Chiuso nella 124:** `Lucchetto.PulisciResti`
   all'avvio di `serve` (`backup.PulisciAllAvvio`: con un backup in corso in un altro processo non tocca niente) e
   all'inizio di ogni giro (`backup.Giro`, anche del comando); solo i nomi esatti `.backup-<cifre>` e
   `.verifica-backup-<cifre>` (il suffisso di `os.MkdirTemp`), solo cartelle vere (un collegamento con quel nome
   resta), tolte con `os.Root.RemoveAll` senza seguire i collegamenti dentro (`TestPulisciResti`).
   (f) `fotografa` esclude le tabelle con `NOT LIKE 'sqlite_%'`, dove `_` è un jolly e il confronto ignora le
   maiuscole: esclude anche una tabella `sqliteX…`. Oggi nessuna; innocuo, meglio `substr(name, 1, 7) <> 'sqlite_'`.
   Dettagli in `docs/slices/121-verifica-backup.md`, «Revisione» e «Dubbi».
8. **Backup: difetti BASSI della 122 (revisione Opus e implementatore, 25/11/2026).** (a) Una riga di
   `verifiche_backup` con un istante nel futuro (orologio andato avanti e poi corretto) blocca per sempre lo sblocco
   dopo l'import (`ErrOrologioBackup`) finché l'ora vera non la raggiunge; oggi l'unica uscita è correggere il
   database a mano. **126:** la pagina e l'allarme devono dirlo e offrire una via d'uscita (per esempio segnare la
   riga come non valida, solo titolare). **In parte nella 126:** banner e pagina lo dicono (problema `orologio`:
   «controlla data e ora; finché l'orologio non arriva a quell'ora le cancellazioni restano ferme; se manca molto,
   chiedi assistenza»). **Resta aperta la via d'uscita:** segnare la riga come non valida è una scrittura sul registro
   che sblocca D-11, fuori dalla pagina in sola lettura della 126; va in una slice sua (Opus, con revisione). (b) Con un errore del controllo la pulizia dei media scrive due righe nel
   registro: l'avviso «manca un backup verificato» (una volta per avvio) e la riga d'errore con causa
   `registro_backup`. Corretto ma ridondante: **126**, insieme all'allarme. **Non fatta nella 126** (non chiesta nel suo
   brief): resta aperta, innocua. (c) **Seconda revisione:** un archivio
   che non passa il ricalcolo (bit rot, errori di I/O) si rilegge per intero a ogni giro della pulizia (ogni 60 s, e
   due volte per giro) e a ogni controllo della conservazione: resta chiuso, ma consuma la chiavetta e rallenta il
   backup della 123. Correzione: ricordare il fallimento per riga e file, con un'attesa che cresce, nella **123 o
   124**. **Chiuso nella 123:** `PreparaBloccoImport` ricorda il fallimento per riga e file e non rilegge per 10
   minuti, poi 20, 40… fino a 6 ore; intanto dà lo stesso errore e il blocco resta. `BackupVerificatoDopo` (cestino)
   ricalcola sempre. (d) Facoltativo: confrontare anche il ctime nella memoria, che chiuderebbe la riscrittura sul posto con
   `touch -r` prima del ricalcolo delle 23-24 h. Dettagli in `docs/slices/122-registro-backup.md`, «Revisione» e
   «Seconda revisione».
9. **Backup: difetti BASSI della 123 (revisione Opus, 25/11/2026).** (a) Una seconda partizione o un LV dello stesso
   disco dei dati, `/dev/loop*` e `/dev/zram*` passano i controlli della chiavetta (sorgenti `/dev/…` diverse):
   correzione leggendo `/sys/dev/block/M:m` (disco padre, `removable`). (b) `-cartella` ora vuole il flock sulla
   cartella: su NFS senza lockd fallirebbe sempre (oggi nessuno lo usa così). (c) Proposta del revisore per la **130**:
   invece di svuotare il registro, declassare le righe a una destinazione che non sblocca, così la rotazione le
   conosce ancora (è il Dubbio 1 della 123; resta la regola «solo archivi con una riga del registro»). Dettagli in
   `docs/slices/123-chiavetta-rotazione.md`, «Revisione».
10. **Backup: difetti BASSI della 124 (revisione Opus, 25/11/2026), per la 126.** (a) Spostare `backup.ora` a
   un'ora più presto durante il giorno (per esempio alle 15:00 da 02:30 a 11:00) fa partire subito il backup del
   turno di oggi, in orario di negozio: il turno nuovo risulta da fare. La pagina che scrive l'ora dovrà dirlo o
   valere dal giorno dopo. **Resta aperta:** la pagina Backup della 126 mostra l'ora ma non la scrive (nessuna
   pagina scrive `backup.ora`); vale per chi farà quella modifica. (b) «Esegui adesso» (`POST /settings/backup/esegui`) torna con un 302 a `/settings/backup`,
   che è un 404 finché la 126 non porta la pagina, e il messaggio resta nella sessione fino alla pagina dopo.
   **Chiusa nella 126:** la pagina c'è e il messaggio si vede lì.
   Dettagli in `docs/slices/124-backup-notturno.md`, «Revisione».
   (c) **Dalla 125, per la 126:** lo stato «prova settimanale fallita» è `db.ProvaSettimanaleFallita` (l'ultima
   prova, automatica o a mano, non riuscita); i media rovinati (D-116) sono `db.MediaRovinati` (ultimo backup
   verificato sulla chiavetta). Una prova **mai fatta** (apparecchio nuovo, o lavoro notturno mai partito) oggi non
   accende niente: la 126 decida se dirlo con `db.UltimaProvaRipristino` (per esempio nessuna prova da più di 8
   giorni). Dettagli in `docs/slices/125-prova-settimanale.md`. **Chiusa nella 126:** prova settimanale fallita e
   media rovinati nel banner; «prova mancante» se l'ultima prova riuscita ha più di 8 giorni o se non c'è nessuna
   prova e il primo backup sulla chiavetta ha più di 8 giorni (un apparecchio nuovo non suona subito).
11. **Backup: da ricordare dopo la 126 (revisione Opus, 25/11/2026).** (a) **Guida della T5.4:** l'apparecchio non
   usa mai `-senza-lavori-automatici` (con l'opzione il banner non segnala backup mancanti o vecchi né la
   configurazione); dalla 127 con l'opzione non partono nemmeno gli avvisi dei backup al negozio. (b) **127:** il
   «Motivo» della pagina Backup è il testo grezzo dell'errore (può avere percorsi): gli avvisi Telegram/email non lo
   riusano, usano `Problema.Testo`. **Chiusa nella 127** (`backup.Avvisi`: solo `Problema.Testo` e titoli fissi per
   codice; `TestAvvisiTestiSenzaPercorsi` con il punto di montaggio nell'errore e nel motivo). (c) `promemoria.html` non chiude la
   `<section id="settings-panel">` aperta da `schede` (le altre pagine delle impostazioni sì): innocuo per il
   browser, trovato contando le section della pagina Backup.

12. **Foto interna con 500 nel banco (s126a e s126b, 25/11/2026, trovata nella 126).** In tutti e due i giri, anche in
   quello verde, il registro del Go ha una riga `level=ERROR msg="foto non letta" faccia=interna` con 500 su
   `GET /repairs/{id}/photos/{foto}` (`internal/web/foto.go:57`), subito dopo una pagina di firma e una scheda. Non è
   la D-100: quella riguarda solo le foto delle facce pubbliche (`/r/`, `/artigiano/`, `/lotto/`). Il banco non la
   conta come differenza, quindi la pagina confrontata non dipende da quella foto. Il registro non dice il motivo
   (file mancante nei dati sintetici? variante non generata?). Da capire: scenario che la chiede, e se il registro
   deve dire la causa. Non toccata dalla 126.

13. **Backup: dalla 128 (copia remota SFTP, 26/11/2026), per le slice dopo.** (a) **130 (ripristino):** la
   configurazione `backup.remoto` e la chiave `secrets/chiave_sftp` viaggiano con il backup: il ripristino deve
   spegnere la copia remota (togliere o mettere da parte `backup.remoto`, risposta I) e svuotare `copie_remote` con
   il registro, altrimenti la copia di prova caricherebbe sulla Storage Box vera e ne ruoterebbe gli archivi
   (le righe `sftp` svuotate fanno lasciare gli archivi remoti, ma le copie nuove li affiancherebbero). La 131 la
   riaccende. **Chiusa nella 130:** `gestrip restore` toglie `backup.remoto` mettendolo da parte nello stato isolato
   (`ripristino.isolato`, per la 131), svuota `copie_remote` e gli altri registri dei backup, e `serve` sulla copia
   isolata non ha la sezione della copia remota né una chiavetta. `secrets/chiave_sftp` resta nella copia (serve alla
   131 per riaccendere la copia remota): senza `backup.remoto` non si usa. (b) **129b (prova vera, con Stefano):** misurare la durata del caricamento (il tetto è 6 ore,
   `TempoMassimoCopiaRemota`), controllare che la Storage Box dia `statvfs@openssh.com` e che il `RealPath` della
   cartella del sottoaccount sia assoluto; la porta predefinita di comando e pagina è 22 (la Storage Box risponde
   anche sulla 23): si sceglie dopo la prova. Passi ed esito atteso nella ricevuta 129. La 129 ha dato il tempo
   massimo a `ChiaveDelServer` e `Prova` (`TempoMassimoProvaRemota`, 60 s), che prima ne erano senza. (c) **Risposta
   E, download mensile:** «una volta al mese un download completo di notte con la verifica intera»: assegnato alla
   **129c**. **Chiusa nella 129c:** la prima domenica del mese, dopo la prova settimanale, il lavoro notturno scarica
   l'ultima copia remota e la verifica per intera (un mese non riuscito si riprova ogni notte) (registro `prove_remote`, problemi `prova_remota_fallita` e
   `prova_remota_mancante`, comando `gestrip prova-backup -remota`). Per la **130**: il ripristino svuoti anche
   `prove_remote` (insieme a `copie_remote` e al registro, punto (a)). **Fatto nella 130.** (d) Un nome finale caricato e rinominato la cui riga `sftp` non si scrive (codice `registro`)
   resta sul server per sempre (la rotazione remota tocca solo archivi registrati), come il caso della chiavetta
   nella nota 6c: va tolto a mano. (e) Il rischio «chiavetta appesa» del lavoro notturno (124) resta: la copia remota
   legge la chiavetta in una goroutine a parte e non lo peggiora (vedi la ricevuta 128).

14. **Ripristino isolato: da decidere per la 131 (slice 130, 26/11/2026).**
   (a) **Chiave del server vecchia e link già mandati.** Il ripristino rinnova `chiave_server` (piano, riga 130), ma da
   lei vengono anche le firme dei link email di conferma e disiscrizione (sottochiave «email») e dei link artigiano
   `g1.` (sottochiave «artigiano»): sulla copia i link mandati dalla macchina vecchia non valgono più (le garanzie
   `/g/` sì: segreto Flask, identico). La vecchia è tenuta in `segreti` come `chiave_server_del_backup`, mai usata.
   **Raccomandazione:** alla conferma separare le sottochiavi: sessione, CSRF e galleria dalla chiave nuova, email e
   artigiano dalla vecchia; se Stefano accetta la rottura, la 131 cancella la vecchia.
   (b) **Righe in coda e «Esegui adesso» durante l'isolamento.** La coda in uscita non gira, ma le righe accodate
   (già nel backup, o fatte dalla faccia interna: «Esegui adesso» dei promemoria, avvisi a mano, email ai clienti)
   restano `queued` e partirebbero tutte alla conferma: avvisi doppi o vecchi ai clienti. **Raccomandazione:** la 131
   prima di riaccendere mostra le righe in coda e chiede «manda» o «scarta» (scarta come proposta); in più, durante
   l'isolamento, «Esegui adesso» dei promemoria rifiuta.
   (c) **IA e sidecar non spenti.** Partono solo da azioni del personale, ma con un fornitore remoto la copia
   userebbe la chiave del negozio (lo standalone isolato non aveva rete, quindi nemmeno l'IA). **Raccomandazione:**
   nella copia isolata spegnere il client del modello remoto (i sidecar locali possono restare).
   **Deciso (coordinatore, 26/11/2026):** nella copia isolata si spegne il client del modello remoto; i sidecar locali possono restare. Da fare nella 131.
   (d) **Nessun banner «copia isolata» nella faccia interna:** solo l'avviso sullo stderr di `serve`.
   **Raccomandazione:** la 131 aggiunge il banner per tutti (non solo il titolare) con il link alla pagina di
   conferma.
   (e) **Nessuna riga di audit del ripristino** (lo standalone non ne scriveva; la verifica aggiungeva righe
   escluse dai conteggi). **Raccomandazione:** alla conferma una riga `backup.restore_confirmed` con archivio e ora del
   ripristino (dallo stato isolato); al ripristino niente, il database è appena verificato.
   **Deciso (coordinatore, 26/11/2026):** alla conferma una riga `backup.restore_confirmed` con l'archivio e l'ora del ripristino; al ripristino niente.
   (f) **Nome del comando:** `restore` come nel piano, mentre gli altri comandi del backup sono in italiano
   (`verifica-backup`, `prova-backup`). **Raccomandazione:** tenere `restore` e aggiungere l'alias `ripristina` (costo
   minimo), da decidere con il capitolo 11 del manuale (132).
   **Deciso (coordinatore, 26/11/2026):** resta `restore`, si aggiunge l'alias `ripristina` (nella 131).

## Note per lo standalone

1. **Impronte salvate come TEXT (slice 024, D-42).** Verificato dalla sessione dello standalone il
   31/10/2026: oltre a `historical_documents.sha256` anche `sale_photos.sha256` e
   `sale_revision_photos.sha256`. Domanda di allora: Lo standalone confronta o
   cerca mai `historical_documents.sha256` (controllo d'integrità, deduplica, pulizia dei file)? Se
   sì, con il valore salvato come TEXT quel confronto con un BLOB fallisce già oggi in produzione
   per ogni PDF storico arrivato da Flask. Da verificare dalla sessione dello standalone (Stefano,
   31/10/2026); il Go non ne dipende.

2. **Tipo servito per una foto che non si riconosce (slice 037, D-47).** Senza urgenza (Stefano,
   01/11/2026). La rotta delle foto del personale, se i byte non sono un'immagine riconosciuta,
   ripiega sul `tipo_mime` della riga qualunque sia: una riga con `text/html` (database manomesso,
   import strano) verrebbe servita come HTML sull'origine interna. Il Go ripiega solo su `image/*`,
   altrimenti `application/octet-stream`. Oggi non succede: nessuna scrittura mette un tipo così.

3. **Scrittura del file temporaneo senza controllo (slice 039, D-50).** Senza urgenza (Stefano,
   02/11/2026). In `RepairPhotoUploadController` l'esito di `IO.binwrite` non si guarda: con il disco
   pieno il file resta troncato e il caricamento finisce come «Foto rifiutata.» (422), come se la
   foto fosse sbagliata. Il Go risponde 500 «Foto non salvata.».

4. **500 «database is locked» sulle miniature mentre si caricano foto (slice 040a).** Misurato sul
   banco con i dati sintetici, immagine `fab-220`. `RepairPhotoController.show/2`
   (`repair_photo_controller.ex:15`) scrive la riga d'audit della lettura in un `BEGIN IMMEDIATE`:
   se la scheda chiede insieme diverse miniature mentre nella stessa busta si caricano foto, le
   richieste aspettano i 5 s del busy timeout e rispondono 500 (`Exqlite.Error database is locked`),
   e la miniatura resta rotta. Su 5 giri completi del banco è successo in 1; ripetendo lo scenario
   sulla stessa istanza, su quasi tutte le miniature delle buste coinvolte. Il Go, con le stesse
   azioni, non ha mai risposto 4xx o 5xx. Non tocca la sicurezza; nel banco il campo è solo sul Go
   (`solo_go`). Da passare alla sessione dello standalone.

5. **500 su un id di busta oltre i 64 bit (slice 042).** Misurato sul banco, immagine `fab-220`:
   `GET /repairs/9223372036854775808/receipt.pdf` (e `99999999999999999999`) risponde 500
   «Internal Server Error», mentre `9223372036854775807` risponde 404. Causa probabile (letta, non
   misurata): l'id diventa un intero che SQLite non sa confrontare. Non tocca la sicurezza: è solo un
   errore del server al posto del 404. Da passare alla sessione dello standalone (domanda 21, decisa:
   nel Go 404, D-56).

6. **QR dell'etichetta busta trasposto (slice 043).** Misurato sui PDF di `fab-220`
   (`/repairs/:id/label.pdf`): il QR è lo specchio sulla diagonale della matrice giusta (versione 1,
   livello M, maschera 0): differisce in 2 moduli dalla trasposta e in 104-168 da ogni rotazione
   diritta. Causa probabile (ragionata, non
   misurata): righe e colonne scambiate nel disegno dell'SVG. Non tocca la sicurezza. Da passare
   alla sessione dello standalone (domanda 23, decisa: D-52). Senza urgenza (Stefano, 03/11/2026).
7. **Foto JPEG «Ultra HDR» rifiutate (produzione, 03/11/2026). Risolta dallo standalone con la
   slice 222** (`9a40f1f`, immagine `fab-222`, pacchetto 2026.11.03-1). Una foto di un Oppo Find X9
   Ultra (Android) era stata rifiutata con «Foto rifiutata.» (422): il JPEG ha un APP2 MPF e una
   gain map `hdrgm` (due immagini), Pillow 9.4 lo apre come MPO con `n_frames` 2 e
   `normalize_image.py` rifiutava i file con più fotogrammi. La 222 usa il fotogramma 0 (la foto
   principale) e rifiuta ancora PNG e WebP animati. Il Go si è allineato nella mini-slice 045b
   (D-65, `docs/studio-heic.md` sezione 0). Gli HEIC danno ancora 415 «Formato non supportato.»
   (resto dello studio).

8. **500 «Database busy» all'apertura di una scheda (banco s087a, 16/11/2026, slice 087).** Misurato sul banco con i
   dati sintetici, vista telefono, scenario `sollecito-artigiano`, fase «fine»: `GET /repairs/71` (02:48:13) risponde 500
   dopo 5017 ms, cioè il busy timeout di 5 s: `Exqlite.Error Database busy` sull'INSERT in `audit_events` nel `mount/3`
   di `RepairLive` (`repair_live.ex:50`), mentre il mount LiveView di `/audit` (aperto 100 ms prima, nel conto delle righe
   del registro) non era ancora finito. Una volta sola in quel giro (le due aperture di `/repairs/71` di pochi secondi
   prima erano andate bene, e lo stesso scenario sulla vista PC); il Go, con gli stessi passi, non ha risposto 5xx. Chi
   teneva la scrittura non si vede dal log (nessuna traccia delle query). È dello stesso tipo della nota 4. Non tocca la
   sicurezza. Da passare alla sessione dello standalone, solo in lista.
   **Di nuovo nel banco s089a (16/11/2026, slice 089):** vista PC, scenario `whatsapp`, fase «registro»: il sesto `GET /audit`
   di fila (05:44:25, il conto delle righe prima dell'invio) risponde 500 dopo 5020 ms con `Exqlite.Error database is
   locked` su `BEGIN IMMEDIATE TRANSACTION` in `AuditLive.handle_params/3` (`audit_live.ex:13`), mentre le cinque aperture
   di `/audit` dei 400 ms prima erano andate bene; poi lo scenario va in timeout. Nessuno scenario della 089 in corso (le
   pagine dei lotti di quella vista erano finite alle 05:38). Il Go, con gli stessi passi, non ha risposto 5xx.
   **Terza volta nel banco s089f (17/11/2026, giro finale della 089):** vista telefono, scenario `storico-correzioni`,
   fase «fine»: `GET /repairs/82` (10:54:58) risponde 500 dopo 5018 ms con `Exqlite.Error Database busy`; poi lo scenario
   va in timeout (30 s su `locator.waitFor`). Il Go, con gli stessi passi, non ha risposto 5xx. Il giro identico s089g
   subito dopo (11:08-11:30) è verde.
   **Quarta volta nel banco s091a (17/11/2026, primo giro della 091):** vista PC, scenario `testi-invii`, fase «inizio»:
   `GET /audit` (14:24:38) risponde 500 dopo 5015 ms con `Exqlite.Error database is locked` su `BEGIN IMMEDIATE
   TRANSACTION` in `AuditLive.handle_params/3` (`audit_live.ex:13`); poi lo scenario va in timeout (30 s su
   `locator.innerText`): 1 errore e 57 differenze inattese, tutte dello standalone in quello scenario. Il Go non ha
   risposto 5xx. Il giro identico s091b subito dopo è verde.
   **Quinta volta nel banco s094a (18/11/2026, primo giro della 094):** vista telefono, scenario `whatsapp`, fase
   «risposta»: lo standalone risponde «500 GET /» con `Exqlite.Error Database busy` (08:19:32), poi lo scenario va in
   timeout (30 s su `locator.waitFor`): 1 errore e 52 differenze inattese, tutte dello standalone in quello scenario. Il
   Go non ha risposto 5xx. **Ipotesi da verificare (coordinatore, 18/11/2026):** lo scheduler dei job dello standalone è
   acceso durante il giro (nessun canale: scrive righe «Saltato» e audit) e la sua scrittura può tenere il database
   mentre una pagina scrive l'audit; da confrontare l'ora delle comparse con i giri dello scheduler. Il banco non cambia
   nella 094.
   **Sesta e settima volta nel banco s097b (18/11/2026, primo giro della 097b):** vista telefono, scenario
   `sessioni-rientro`, fase «fine» («500 GET /repairs/81») e scenario `whatsapp`, fase «registro» («500 GET /audit»);
   nel log dello standalone `Database busy` (12:56:42) e `database is locked` (13:00:02); 2 errori e 8 inattese, tutte
   dello standalone. Il Go non ha risposto 5xx. Giro identico s097c con l'ok del coordinatore.
   **Ottava e nona volta nel banco s098a (19/11/2026, primo giro della 098):** vista telefono, scenario
   `storico-correzioni`, fase «fine» (`TimeoutError`, «500 GET /repairs/88» dopo 5018 ms) e scenario `testi-invii`, fase
   «inizio» (`TimeoutError`, «500 GET /audit»); nel log dello standalone `Database busy` (01:46:56) e `database is
   locked` (01:49:44); 2 errori e 40 inattese, tutte dello standalone. HTTP verde. Il Go non ha risposto 5xx. Giro
   identico s098b con l'ok del coordinatore (una sola ripetizione per la nota 8): verde.
   **Slice 100 (19/11/2026): l'ipotesi dello scheduler non regge.** Lo scheduler dello standalone scrive solo dopo
   l'ora del lavoro (09:00 i ritardi, 10:00 i solleciti, ora di Roma) e al massimo 3 tentativi al giorno, ma 4 comparse
   su 9 cadono prima delle 09:00 italiane (le ore del log sono UTC): s087a 02:48, s089a 05:44, s098a 01:46 e 01:49 del
   log. Dal banco della 100 i tre lavori sono comunque spenti nei dati dello standalone (passo `lavori` di `banco su`,
   `Settings.put` a «0»): serve a togliere le righe «Saltato» e l'audit che dipendono dall'ora del giro, non è il
   rimedio. Restano attivi `Outgoing.Dispatcher` (30 s), `MediaCleanup` (60 s) e `TelegramPoller`. **Ipotesi nuova,
   solo ragionata:** `default_transaction_mode: :immediate` in `config/runtime.exs:187` dello standalone: anche una
   transazione di sola lettura (`AuditLive.handle_params`, il mount di `RepairLive`) prende il blocco di scrittura e,
   con un'altra scrittura in corso, aspetta il busy timeout di 5 s. **Prova proposta (coordinatore, non ancora fatta):**
   contare le comparse in giri con lo standalone in modalità `deferred`, solo nel banco, mai come modifica dello
   standalone usato per il confronto. Giro s100a: verde, nessuna comparsa.
   **Decima volta nel banco s101b (19/11/2026, secondo giro della 101), la prima con i lavori dello standalone spenti:**
   vista telefono, scenario `sessioni-rientro`, fase «registro»: «500 GET /audit» dopo 5019 ms (10:06:51 del log),
   `database is locked`; 1 errore e 10 inattese, tutte dello standalone. Il Go non ha risposto 5xx. Conferma che lo
   scheduler non è la causa; ancora una volta coinvolto `/audit`, coerente con l'ipotesi `:immediate`. Giro identico
   s101c con l'ok del coordinatore: verde.
   **Undicesima volta nel banco s107a (20/11/2026, primo giro della 107):** vista PC, scenario `sollecito-artigiano`, fase
   «scheda»: «500 GET /repairs/62» subito dopo un `GET /audit`; poi 48 inattese a catena, tutte dello standalone
   (`sollecito-artigiano`, `anagrafica-artigiani`, `sessioni-consegna`). Il Go non ha risposto 5xx. Ancora una volta
   accanto a `/audit`, coerente con l'ipotesi `:immediate`.
   **Dodicesima e tredicesima volta nei banchi s109a e s109b (20/11/2026, slice 109, due giri di fila):** in s109a vista
   telefono di `sessioni-rientro`, fase «registro»: `GET /audit` alle 20:46:42.933 → «Sent 500 in 5017ms», «database is
   locked» (log dello standalone, riga 6909), 6 inattese e 1 errore. In s109b vista PC di `storico-correzioni`, fase
   «fine»: `GET /audit` alle 21:08:41.559, poi `GET /repairs/63` → «Sent 500 in 5020ms», «Database busy» (riga 4360), 4
   inattese più 2 a catena in `anagrafica-artigiani[1]`. HTTP verde in tutti e due, nessuna inattesa lato Go. Stefano ha
   deciso di pubblicare la 109 così (eccezione per questa slice; la regola della sola ripetizione non cambia).
   **Quattro volte nello stesso giro, banco s114a (22/11/2026, primo giro della 114):** sempre un `GET /audit` con 200
   subito prima, poi un 500 dopo circa 5 s; ore UTC del log dello standalone. Vista PC di `consegne-solleciti`, fase
   «pagina»: `GET /handovers/reminders` alle 11:47:01 → «Database busy» sull'audit (37 inattese). Vista PC di
   `storico-correzioni`, fase «fine»: `GET /repairs/63` alle 11:48:59 → «Database busy» (4 inattese più 2 a catena in
   `anagrafica-artigiani[1]`). Vista telefono di `storico-correzioni`, fase «fine»: `GET /repairs/88` alle 11:53:53 (4
   inattese). Vista telefono di `whatsapp`, fase «inizio»: un secondo `GET /audit` di fila alle 11:56:48 → «database is
   locked» su `BEGIN IMMEDIATE` (52 inattese). Sono le uniche richieste dello standalone oltre 300 ms nel giro. Il Go
   non ha mai risposto 500 in questi scenari, e nessuno dei tre scenari colpiti è cambiato nella 114. Quattro comparse
   in un giro solo sono il massimo finora (prima al più due).
   **Di nuovo in due giri di fila, banchi s115a e s115b (24/11/2026, slice 115):** in s115a vista PC di `salva-avverti`,
   fase «registro»: `GET /audit` → «Sent 500 in 5015ms», «database is locked» (log dello standalone, riga 3553), 6
   inattese e 1 errore. In s115b vista telefono di `pannello-qr`, fase «inizio»: un `GET /audit` con 200 alle 00:52:38
   UTC e subito dopo un secondo `GET /audit` → «Sent 500 in 5016ms», «database is locked» (riga 6364), 93 inattese a
   catena, tutte «(assente)» dalla parte dello standalone, e 1 errore. Parte HTTP verde in tutti e due (0 inattese),
   nessuna inattesa dalla parte del Go; nessuno dei due scenari è cambiato nella 115. Decisione a Stefano.
   **Accanto, prima comparsa di un'oscillazione dello standalone (s097b):** passo HTTP `pagina-storica-rifiuti[15]`,
   `GET /foto/2_26/b.jpg?sig=…&exp=…` (foto firmata che non esiste): codice 404 e corpo «Foto non disponibile» uguali,
   ma lo standalone risponde **senza** `content-type` (il Go `text/plain; charset=utf-8`). Verde in tutti i giri
   precedenti (s094a…s097a); la 097b non tocca le foto; nel log dello standalone la richiesta non compare. Trattata dal
   coordinatore come oscillazione dello standalone; se ricapita, da guardare insieme alla nota 8.
9. **Nota del banco: minuto della firma che oscilla (banco s096a, 18/11/2026, slice 096).** Scenario HTTP `firma`, passi
   28, 31, 42 e 50, campi `repair-signature/p` e `repair-signature-stamp`: lo standalone mostra «Firmata il 18/11/2026
   10:38 — ritirata da Banco Sintetico», il Go «… 10:39 …». Le due app firmano in momenti diversi e le firme sono
   cadute a cavallo di un cambio di minuto: testo uguale, 8 differenze inattese, nessun difetto. Prima comparsa; il giro
   identico s096b è stato rilanciato con l'ok del coordinatore. **Idea (coordinatore, 18/11/2026):** se ricapita,
   normalizzare il minuto dell'ora della firma in una slice del banco; per ora il banco non cambia.

10. **Chiave dell'assistente rifiutata in silenzio (banco s107a, 20/11/2026, slice 107).** In «Impostazioni › Assistente»
   una chiave scritta con un indirizzo `http` verso un'altra macchina fa rifiutare il salvataggio, ma sotto la casella della
   chiave non compare «una chiave non viaggia in chiaro verso un host remoto»: il titolare vede il modulo tornare indietro
   senza sapere perché. Causa (standalone a `go-baseline`): `blank_secret/1` in `lib/gestrip_standalone/assist/config.ex`
   (righe 281-287) toglie `api_key` anche dai `params` del changeset rifiutato, quindi `Phoenix.Component.used_input?/1` è
   falso e `core_components.ex:193` scarta gli errori del campo. Lo stesso vale per «Troppo lungo (massimo 255
   caratteri).» sulla chiave. Il Go mostra il messaggio (differenza attesa, D in arrivo dal coordinatore). Non tocca la
   sicurezza (la chiave non è salvata). Da passare alla sessione dello standalone, solo in lista.

11. **Pseudonimi: nomi e telefoni veri che escono dall'esportazione (review 116, 24/11/2026).** Nello standalone a
   `go-baseline` (`lib/gestrip_standalone/assist/pseudonym.ex`), con test temporanei sul Go che lo copiava:
   (a) un telefono che non è di un cliente noto esce in chiaro se è scritto con separatori che la regola non ammette
   («0423/521234», spazio non separabile, spazi doppi, «+41 79 123 45 67»); (b) un nome vero esce se è unito o spezzato
   da caratteri invisibili o simili all'apostrofo («dellʼRossi» con U+02BC, ZWJ, trattino morbido). Nel Go chiusi con la
   **D-111** (più severa dello standalone: più righe scartate, nessun dato in chiaro). Tocca la privacy dei dati che
   escono dall'apparecchio: da passare alla sessione dello standalone.

## Risposte già registrate

Nota 10 della review 109 (scadenza di lettura delle foto; approvata da Stefano come mini-slice): **chiusa con la 118b**
(24/11/2026). Provato con due test su un server vero: la scadenza dell'ultimo pezzo cancellava davvero il salvataggio
dalla scheda (`POST /repairs/{id}/foto`) e, con una foto la cui ultima lettura chiude un MiB, anche dalla coda
(`POST /repairs/{id}/photos`). Finito il corpo la scadenza si toglie (`togliScadenzaLettura`, anche per la clip della 109).

Domanda 28 della slice 105 (Stefano, 20/11/2026, tramite il coordinatore: «concordo»): con clienti omonimi e nessun
telefono dettato il telefono non si propone, il campo resta vuoto → D-106 (RIP-H-43), da fare nella 108; la 105 resta
uguale allo standalone.

Le domande 1–15 (slice 013b–018) sono state approvate da Stefano il 30/10/2026 e
registrate: deviazioni D-34…D-41, note nell'inventario (ACC-16, ADM-17, ADM-20, ADM-22, OPS-12) e
nel piano (T2, T4, T5, §7.3).

La domanda 1 della slice 020 (dettagli di ACC-16) è stata approvata da Stefano il 31/10/2026,
con tutte e quattro le raccomandazioni: il link di `gestrip primo-titolare` vale 60 minuti; alla
creazione del link si scrive la riga d'audit `administrator.bootstrap_requested` con attore
`console`; `/` e `/login` su un database vuoto non dicono niente di nuovo; nessun limite ai
tentativi su `/configura` (codice da 256 bit). Registrata nell'inventario (ACC-16).

Le domande 1–7 delle slice 022–024 sono state approvate da Stefano il 31/10/2026, tutte come
raccomandate; per la 7 solo la (a), senza correggere lo standalone per ora:
- righe nel cestino importate, righe incoerenti che bloccano: nota in OPS-03, che si chiude;
- spazio libero prima di copiare (2 × sorgente + media + 1 GiB), resti di un import interrotto
  da togliere a mano (scritto in `docs/guida-import.md`), `-senza-media` solo con `-sintetico`:
  nota in OPS-07;
- file media non nominati come avviso, coda delle cancellazioni prudente: nota in OPS-02;
- impronta TEXT di 32 byte riscritta BLOB con avviso nel report: deviazione D-42. Estesa lo stesso
  giorno a `sale_photos` e `sale_revision_photos` dopo la verifica della sessione dello standalone
  (causa unica: le scritture con `Repo.query` e un binario semplice salvano TEXT); lo standalone
  non verrà corretto, il banco sintetico copre foto di vendita caricate dall'interfaccia e una
  revisione di vendita.

La domanda su trigger e viste della sorgente (giro di mutazioni della 022c) è stata approvata da
Stefano il 31/10/2026 come raccomandata: un elenco di trigger e viste diverso dal riferimento
blocca l'import con `schema/oggetto_diverso`. Nota in OPS-01; si fa nella slice 025.

Le domande 1–2 della slice 025–026 sono state approvate da Stefano il 31/10/2026 come raccomandate:
- gli indici della sorgente non si confrontano (nota in OPS-01);
- in `sale_revision_photos.inserted_at` un istante senza zona si accetta come UTC, si scrive con
  la `Z` e il report dà l'avviso `traduzione/istante_senza_zona` per tabella con il numero di
  righe; ogni altra forma o colonna blocca. Registrata nella D-42 (stessa causa: scritture a mano
  con exqlite 0.40); si fa nella slice 027. La sessione dello standalone sta cercando tutte le
  colonne scritte a mano con lo stesso problema: quando arriva l'elenco si estende in una volta.
- elenco arrivato lo stesso giorno dalla sessione dello standalone (sola lettura, database
  sintetico): la regola vale per tutte e sole `sale_revision_photos.inserted_at`,
  `emergency_entries.inserted_at`, `emergency_events.closed_at` e `updated_at`,
  `repairs.retention_started_at` e `updated_at`; una colonna può avere righe con e senza `Z`,
  l'avviso conta solo quelle senza. Registrato nella D-42. Nella catena del banco si aggiunge il
  registro d'emergenza fatto con le funzioni dello standalone (slice 028).

Domande 1–4 delle slice 030 e 032 (Stefano, 31/10/2026): 1–2 difetti dello standalone, corretti
dalla slice 220 dello standalone (`12d6e7f`, `go-baseline` dal 01/11/2026) e allineati nel Go con la
slice 037d (testi del catalogo `it`, `buste.NormalizzaNumero`; la nota per lo standalone è tolta);
3 → D-43; 4 → D-44 (inventario).

Domande 1–14 delle slice 033–037 (Stefano, 01/11/2026):
- 1, 6, 9 (righe `repair.viewed`): non come raccomandato; una riga solo per un'apertura vera della
  scheda, nessuna dopo il redirect di un'azione (rifiuti compresi), per «Modifica» e per «Annulla».
  Il segno del redirect non si può forgiare. D-44 estesa; nota in RIP-S-01.
- 2, 3, 4, 7: accettate; RIP-S-04, 11, 07, 14 restano parziali fino al livello browser del banco.
- 5: tacere anche sulla scheda (D-45, RIP-S-13).
- 8: rifiutare «Genera nuovo link» senza `indirizzo_pubblico` prima di toccare qualunque cosa, sul
  modello D-32; la creazione della busta resta com'è (D-46, RIP-S-24).
- 10 e 13: 1440 MiB e al più 2 processi delle immagini insieme; alla T5 la misura della RAM
  sull'apparecchio, e se non ci sta si abbassa con una D-xx (nota in RIP-S-30 e nel piano, T5).
- 11: accettate come scritte nei test, senza D-xx (nota in RIP-S-30).
- 14: ripiego sul tipo della riga solo se `image/*`, altrimenti `application/octet-stream` (D-47,
  RIP-S-33); nota 2 per lo standalone (era la 3 fino alla slice 037d).
Le parti di codice (1, 5, 6, 8, 9, 13, 14) le porta la slice 037b.

Domande 1–4 della slice 037b e 5, 7, 8, 9 della 037c (Stefano, 01/11/2026, tramite il revisore
della 037b), tutte come raccomandate:
- 1: un rifiuto senza il segno della pagina conta come apertura (nota in RIP-S-01, D-44);
- 2: il ripiego sul tipo della riga esclude anche `image/svg+xml` (D-47);
- 3: durate dei segni 2 minuti e 1 ora, catena al più un'ora dall'ultima riga, 10 000 in memoria;
- 4: `?edit=prezzo` e `?edit=preventivo` restano aperture con la riga;
- 5: errori di risorse del processo delle foto distinti dai rifiuti → D-48;
- 7: `POST /repairs/{id}/photos` (039) risponde 503 per un errore di risorse;
- 8: i 10 s di orologio con due figli si misurano alla T5;
- 9: la miniatura rimandata la fa la prima pagina che la chiede.
La 6 resta aperta fino alla misura della CPU alla T5. CPU sul runner della CI: slice 037e.

Domanda 15 della slice 038 (Stefano, 02/11/2026): il nome del file delle foto caricate dalla
scheda si ripulisce come nel caricamento JSON (`buste.PulisciNomeFile`, `clean_name/1`) → D-49
(RIP-S-29).

Domande 16 e 17 della slice 039 (Stefano, 02/11/2026): un guasto del server nel caricamento della
coda (disco che non scrive, errore del database) risponde 500 «Foto non salvata.»; «Foto rifiutata.»
resta per le foto non valide. La 16 come raccomandata (D-50, nota per lo standalone), la 17 diversa
dalla raccomandazione (era 422) → D-51 (RIP-S-35).

Domande 23 e 24 della slice 043 (Stefano, 03/11/2026, tramite il coordinatore), come raccomandate:
- 23: le etichette di oggi con il QR a specchio si leggono senza problemi con il telefono del negozio;
  il Go stampa il QR diritto e con la T4.2 il suo lettore QR legge anche quelli a specchio → D-52
  (RIP-S-43, nota in RIP-H-28); la nota 6 per lo standalone resta, senza urgenza;
- 24: la scheda PDF in DejaVu Sans come gli altri PDF → D-53 (RIP-S-45).

Domande 18-22 (Stefano, 03/11/2026, tramite il coordinatore):
- 18: come raccomandata, il Go tiene visibile «Massimo 2 foto per volta.» → D-54 (RIP-S-29);
- 20: come raccomandata, la coda delle cancellazioni importata aspetta il primo backup verificato →
  D-55 (RIP-S-57);
- 21: come raccomandata, 404 per un id oltre i 64 bit nelle rotte dei PDF con `{id}` → D-56 (il
  report con `ids=` risponde già 422 come lo standalone); codice nella 044;
- 22: «�» per i caratteri che il font non ha → D-57; le emoji come piccole immagini a colori
  incorporate (Twemoji o simili) vanno fra le idee future del PIANO (Stefano, 03/11/2026);
- 19: **non approvata.** La coda delle foto della nuova busta deve lavorare davvero in background:
  premuto «Genera busta» si continua a lavorare dove si vuole (schede, menu), anche una busta dietro
  l'altra; nessuna conferma all'uscita; nessuna foto persa nemmeno ricaricando, se possibile → D-58
  (RIP-H-40), slice subito dopo la 044, con il livello browser del banco.
- D-58: la barretta in basso con lo stato delle foto va bene, disegno come proposto (slice 044b).
- Nuova deviazione D-59 (Stefano, 03/11/2026): al massimo 4 foto per volta, «Massimo 4 foto per
  volta.», in «Nuova riparazione» e dalla scheda (slice 044b); D-54 aggiornata con il testo nuovo.
- Le note per lo standalone restano solo in lista: si correggono solo se gravi.

Domande 25-27 della slice 044b (Stefano, 03/11/2026, tramite il coordinatore), come raccomandate:
- 25: nuovi tentativi automatici con attesa crescente, dopo 5 errori di fila «Riprova» → D-60;
- 26: la barretta sotto il menu ☰ aperto → D-61;
- 27: foto scelte e mai salvate tolte dal telefono dopo 24 ore, in attesa o rifiutate dopo 14 giorni
  → D-62.
