# 124 — Backup notturno, lucchetto dei backup, «Esegui adesso», pulizia delle cartelle temporanee (T5.2, sesta slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 124, risposta **C** (ogni notte, anche nei giorni di chiusura, alle 02:30, ora modificabile, recupero 10
  minuti dopo la riaccensione), D (7 + 4, già nella 123), G (prova settimanale: resta alla 125), «Rischi» (un backup
  alle 02:30 non deve mai fermare l'app; il banco senza allarme); D-15, **D-115**, D-113, D-114; ripresa
  `docs/handoff/2026-11-25-fine-122.md`, «Prossimo passo»; note **6b** e **7(e)** di `docs/domande-aperte.md`;
  pianificatore della 094 (fuso del negozio, salto dell'ora, `-senza-lavori-automatici`).
- **Voci:** OPS-28 (parte: calendario), OPS-16 (parte: il backup notturno sulla chiavetta). Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** caduto due volte sulla nota 8 (s124a, s124b), solo lato standalone; pubblicata per decisione di
  Stefano come la 109 e la 115 (dettagli in fondo).

## Cosa c'è

- `internal/backup/lucchetto.go` (+ `bloccaFile` in `sistema_linux.go`, rifiuto in `sistema_altri.go`):
  `PrendiLucchetto`, `Lucchetto.Rilascia/Preso/PulisciResti`, `LucchettoBackup`, `ErrBackupInCorso`.
- `internal/backup/giro.go`: `Giro.Esegui`, la sequenza di `faiBackup` spostata qui (Dubbio 4 della 123), con `Dire`
  per le righe del comando; `EsitoGiro`; `ErrSenzaLucchetto`; la riga della verifica fallita si scrive anche dopo lo
  stop.
- `internal/backup/notturno.go`: `Notturno` (`Dovuto`, `Giro`, `EseguiAdesso`, `Lavoro`, `Storico`, `Ora`), `Turno`,
  `PulisciAllAvvio`, costanti del calendario, `ErrNonConfigurato`, `ErrFermato`.
- `internal/db/migrazioni/0022_esecuzioni_backup.sql`: lo storico delle esecuzioni.
- `internal/apparecchio/apparecchio.go`: riga `backup_chiavetta` (`Configurazione.ChiavettaBackup`).
- `internal/web/backup.go`, `salute.go`: `POST /settings/backup/esegui` (`Dipendenze.Backup`, `BackupAMano`).
- `cmd/gestrip/main.go`: pulizia all'avvio, `Notturno` nei lavori in sottofondo (spento con
  `-senza-lavori-automatici`), contesto dei lavori creato prima delle facce; `cmd/gestrip/backup.go`: `faiBackup`
  su `backup.Giro` con il lucchetto, `verifica-backup` con il lucchetto.
- `internal/importer/mappa.go`: `esecuzioni_backup` in `TabelleSoloGo`, il suo indice in `IndiciSoloGo`, `backup.ora`
  in `ChiaviSoloGo`.
- Documenti: SVILUPPO (sezione nuova «Backup: lavoro notturno, lucchetto e pulizia», 120, 122, 123, rotte,
  configurazione della macchina, `-senza-lavori-automatici`), PIANO (§1.2, §1.9, §2.5 calendario),
  `docs/domande-aperte.md` (6b e 7(e) chiuse).

### Scelte

- **Destinazione: riga `backup_chiavetta` del file dell'apparecchio** (`/etc/gestrip/apparecchio.conf`), non
  un'impostazione nel database né un'opzione di `serve`. Perché: il piano dice che la T5.4 decide come si monta la
  chiavetta (l'installer scrive il punto di montaggio, come scrive già gli indirizzi delle facce), e la pagina della
  126 è in sola lettura; un'impostazione nel database viaggerebbe col backup, e una copia ripristinata su un'altra
  macchina (risposta I) scriverebbe e ruoterebbe sul percorso della macchina vecchia; un'opzione di `serve` sta
  nell'unità systemd, dove il titolare non la vede e non la scrive. Senza la riga: non configurato (nessun lavoro,
  nessuna riga, «Esegui adesso» lo dice; il banner lo dirà nella 126). La 123 diceva «`DIR` dalla configurazione
  della pagina, 126»: corretto in SVILUPPO. L'**ora** invece sta nel database (`backup.ora`, solo Go, 02:30 senza
  riga), perché la risposta C la vuole modificabile dal titolare nella pagina; oggi nessuno la scrive.
- **Lucchetto globale:** flock esclusivo su `<dati>/.lucchetto-backup`, senza aspettare, preso da backup notturno,
  «Esegui adesso», `gestrip backup` e `gestrip verifica-backup` (la verifica estrae il database nel volume dei dati:
  così anche i controlli dello spazio non si sovrappongono, e la pulizia non toglie mai la sua cartella). Il flock è
  per descrizione di file aperto: vale fra processi e anche fra due goroutine di `serve`, quindi non serve un mutex
  in più. **Ordine:** lucchetto dei backup → cartella della destinazione (123) → cancellazioni dei media (dentro
  `Scrivi`); i primi due non aspettano mai, la pulizia dei media prende solo il terzo: nessun giro di attese.
- **Storico: tabella nuova `esecuzioni_backup`** (migrazione 0022). `verifiche_backup` non basta: ha righe solo per
  archivi scritti e verificati, quindi un backup che non parte (chiavetta tolta, chiave mancante, spazio) non
  lascerebbe traccia e il lavoro riproverebbe ogni minuto; `esecuzioni_lavori` della 094 non va: viene dallo
  standalone (`job_runs`), il suo CHECK ha solo i tre lavori e la pagina «Promemoria e job», confrontata dal banco,
  mostra le sue righe. Il comando `gestrip backup` non scrive nello storico (scrive il registro come prima).
- **Calendario a turni:** il turno è il giorno del negozio dell'ultima ora del backup passata (sull'ora del muro di
  Roma, come la 094). Un turno è da fare finché non ha un'esecuzione automatica `ok`/`avviso`; al più 3 tentativi,
  a 30 minuti l'uno dall'altro. Così il recupero copre anche «spento da ieri sera alle 01:00» (alle 01:10 si fa il
  turno di ieri, alle 02:30 quello di oggi). Tentativi e attesa sono numeri miei (Dubbio 2).
- **Recupero 10 minuti dopo l'avvio** per ogni avvio di `serve` (risposta C), non solo quando l'apparecchio era
  spento: più semplice e innocuo (un riavvio alle 02:25 sposta il backup alle 02:35).
- **«Esegui adesso»:** solo l'azione (`POST /settings/backup/esegui`, `soloTitolare`, CSRF dalla catena di
  `f.rotta`), come dice il piano (pulsante e pagina nella 126). Prende il lucchetto **prima** di rispondere, così
  «già in corso» si dice subito; poi il backup gira in sottofondo con il contesto dei lavori di `serve` (lo stop lo
  ferma, `serve` lo aspetta). Non conta per il turno notturno, come «Esegui adesso» dei promemoria. Nessuna riga
  d'audit: l'esecuzione è nello storico (Dubbio 5).
- **Pulizia delle cartelle temporanee:** all'avvio di `serve` (prima delle facce, anche con
  `-senza-lavori-automatici`) e all'inizio di ogni giro, comando compreso; solo con il lucchetto; nomi esatti
  `.backup-<1-10 cifre>`/`.verifica-backup-<1-10 cifre>` (il suffisso di `os.MkdirTemp`); solo cartelle vere,
  `Lstat` e `os.Root.RemoveAll` (un collegamento con il nostro nome resta, quelli dentro si tolgono senza seguirli).
  Un errore della pulizia all'inizio di un giro ferma il giro (lo scrive nello storico come fallito); all'avvio è solo
  una riga nel registro.
- **Stop durante il backup:** nessuna riga nello storico (il turno resta da fare), il lucchetto si rilascia. Trovato
  facendo il test: la verifica interrotta scriveva la sua riga «fallita» con il ctx già finito, quindi non la
  scriveva, e l'archivio rimasto sulla chiavetta non aveva riga: la rotazione non l'avrebbe mai tolto (ogni stop alle
  02:3x = 3,6 GB persi sulla chiavetta). Ora `Giro.registra` scrive con `context.WithoutCancel` (vale anche per
  Ctrl-C del comando).

## Prove (dati inventati, cartelle temporanee, orologio finto, chiavetta finta della 123)

- `internal/backup/lucchetto_linux_test.go`: `TestLucchettoMaiDueInsieme` (stesso processo, rilascio doppio, 0600,
  collegamento al posto del file), `TestLucchettoFraProcessi` (il binario dei test rilanciato come figlio, nei due
  versi), `TestPulisciResti` (nomi esatti, quasi uguali, 11 cifre, file e collegamento col nostro nome, collegamento
  dentro verso un file fuori, senza lucchetto), `TestPulisciRestiConUnBackupInCorso` (cartella in uso di un altro
  processo, errore di cancellazione).
- `internal/backup/notturno_linux_test.go`: `TestTurno` (02:29/02:30, mezzanotte, capodanno, 29/03 e 25/10, giorno UTC
  diverso), `TestOraBackup`, `TestNotturnoDovuto` (02:30, giorni di chiusura, a mano non conta, tentativi e attesa,
  orologio indietro, avviso, senza chiavetta), `TestNotturnoCambioDOra`, `TestNotturnoGiroVero` (backup vero,
  registro «chiavetta», cartella temporanea tolta, giorno di chiusura), `TestNotturnoGiroFallito` (chiavetta non
  montata, tre tentativi), `TestNotturnoRecupero` (acceso alle 15:00 dopo un giorno spento: prima attesa 10 minuti),
  `TestEseguiAdesso` (durante il backup a mano: secondo «Esegui adesso», giro automatico e lucchetto rifiutano;
  `ErrNonConfigurato`, `ErrFermato`), `TestNotturnoConIlComandoInCorso` (lucchetto di un altro processo),
  `TestNotturnoStopDuranteIlBackup` (a mano e automatico: niente righe, lucchetto libero, niente cartelle
  temporanee, riga «fallita» nel registro), `TestNotturnoPanico`, `TestGiroSenzaLucchetto`.
- `internal/web/backup_test.go`: `TestEseguiBackupTitolare` (sei esiti), `TestEseguiBackupRuoloECSRF` (dipendente,
  senza token, token sbagliato, senza sessione, senza dipendenza); riga nuova in `tabellaPermessi`.
- `cmd/gestrip`: `TestServeBackupNotturno` (con `backup_chiavetta` la prima attesa è 10 minuti, con
  `-senza-lavori-automatici` nessuna; la faccia ha lo stesso `Notturno`; pulizia all'avvio; con il lucchetto preso
  la cartella resta), `TestBackupComandoMaiDueInsieme` (backup e verifica-backup rifiutano, poi «tolta …»).
- `internal/apparecchio`: `TestChiavettaBackup`.
- Aggiornati: `internal/buste/giri_test.go` (migrazioni 19…22), `internal/importer/schema_test.go` (22 migrazioni),
  `correzioni_test.go` (il file nuovo che legge `impostazioni`).
- Test prima del codice per il calendario e il lucchetto solo in parte: il codice del giro è nato spostando
  `faiBackup` (i test della 123 restati verdi senza cambiarli provano che le righe del comando sono le stesse).
  Rossi visti al primo giro: il caso del 25/10 fra le 02:00 e le 02:29 CET (atteso sbagliato nel test, il turno torna
  quello di ieri: documentato), due archivi con lo stesso nome al secondo con l'orologio fermo (test), e la riga
  della verifica non scritta dopo lo stop (difetto vero, corretto).
- **Mutazioni a mano** (`to-be-deleted-after-use/_slice-124/muta.py`, `muta.log`), 15, tutte compilano (M11 riscritta
  con `false && fi != nil`), **uccise 15:** M1 lucchetto condiviso; M2 verifica-backup senza lucchetto; M3 giro senza
  controllo del lucchetto; M4 turno con `>`; M5 «a mano» contato per il turno; M6 niente attesa fra i tentativi; M7
  orologio indietro non riprova; M8 tentativi con `>`; M9 niente attesa dopo l'avvio; M10 pulizia di ogni nome col
  prefisso; M11 pulizia di file e collegamenti; M12 pulizia senza lucchetto; M13 riga nello storico dopo lo stop; M14
  registro della verifica con il ctx finito; M15 backup notturno anche con `-senza-lavori-automatici`.

## Segno di copertura

**Nessun `// inventario:`.** OPS-28 (notturni fuori sede, conservazione, allarme, prova settimanale) chiude con 125
(prova), 126-127 (allarme) e 128 (fuori sede); OPS-16 con 128 (SFTP). I test portano «in parte».

## Per il manuale

- Ogni notte, anche la domenica e il lunedì, alle 02:30 il programma fa da solo il backup sulla chiavetta, lo
  verifica e tiene gli ultimi 7 giorni più 4 settimane. Il negozio può lavorare anche mentre il backup gira.
- Se l'apparecchio era spento all'ora del backup, il backup parte 10 minuti dopo la riaccensione.
- Se il backup non riesce (per esempio la chiavetta non c'è), il programma riprova dopo mezz'ora, al massimo tre
  volte, poi aspetta la notte dopo.
- La chiavetta da usare la scrive l'installazione nel file dell'apparecchio; senza, il backup notturno non parte.
- Il titolare può far partire un backup subito («Esegui adesso», il pulsante arriva con la pagina Backup): se un
  backup è già in corso il programma lo dice e non ne fa partire un altro.
- Mai due backup insieme: anche il comando `gestrip backup` lanciato a mano rifiuta se un backup è in corso.
- Pezzi di un backup interrotto (blackout) nella cartella dei dati si tolgono da soli alla partenza del programma o
  al backup dopo.

## Differenze dallo standalone

- Lo standalone non fa il backup dentro il pacchetto: alle 02:30 lo fa un timer di otto-vps con l'app ferma (OPS-28).
  Il Go lo fa dentro `serve`, a caldo, ogni notte anche nei giorni di chiusura (D-115), con il recupero dopo la
  riaccensione, uno storico suo e «Esegui adesso».
- Lo standalone non ha un lucchetto dei backup (ferma i container); il Go esclude due backup insieme anche fra
  processi.

## Dubbi

1. **La pagina `GET /settings/backup` non c'è ancora:** «Esegui adesso» torna lì e fino alla 126 trova la pagina 404
   (l'azione non ha un pulsante, quindi la raggiunge solo un POST fatto a mano). In alternativa la 124 poteva
   rispondere altrove; ho preferito la rotta della 126.
2. **Numeri miei:** 3 tentativi per turno, 30 minuti fra i tentativi, recupero 10 minuti dopo ogni avvio di `serve`.
   Con la chiavetta tolta per un giorno: tre righe «fallito» al giorno e il banner (126).
3. **25/10, ora ripetuta:** fra le 02:00 e le 02:29 CET il turno torna quello di ieri (stesso confronto della 094).
   Se il turno di ieri non era fatto e ha ancora tentativi, lì si rifà: un backup in più, innocuo.
4. **Turno «a mano»:** la colonna c'è anche per «Esegui adesso» (stesso calcolo), ma conta solo per gli automatici.
5. **«Esegui adesso» senza riga d'audit:** lo storico ha l'esecuzione ma non chi l'ha chiesta (solo il titolare
   può). Se serve, una riga `backup.requested` nella 126.
6. **Non configurato:** con la riga mancante il lavoro esce e non scrive righe; la chiave dei backup mancante invece
   è un'esecuzione fallita (tre al giorno). Il banner della 126 deve dire «backup non configurato» per la prima.
7. **Il backup a caldo e l'app:** niente priorità di CPU o disco più bassa (una goroutine non ha un suo nice). Di notte
   non serve; il recupero di giorno pesa come un `gestrip backup` a mano. Da misurare sulla macchina di prova (T5.4).
8. LSP (gopls), usato a fine lavoro per controllare: `findReferences` su `PrendiLucchetto` (gopls vede solo
   `internal/backup`: i chiamanti in `cmd/gestrip`, `faiBackup` e `verificaBackup`, li ho controllati con grep e
   `go vet`) e su `Dipendenze.Sottofondo` (solo `nuovaFaccia` e `serve`, più i test dell'email: cambiare come nasce in
   `serve` non tocca altri). Per il resto della navigazione grep e lettura dei file.

## Revisione (Opus indipendente, ~110k token, ~25 minuti)

Nessun difetto ALTA né MEDIA. Il coordinatore ha accettato la correzione fuori dal brief (riga della verifica
fallita scritta con il contesto staccato dallo stop).

- **BASSA, corretto:** se l'INSERT nello storico fallisce (CHECK, database occupato, I/O) non c'è riga, il turno resta
  dovuto e ogni giro al minuto rifaceva un backup intero con la rotazione. Ora `registra` conta l'esecuzione
  automatica in memoria per il turno (`nonScritte`); `dovuto` la somma a quelle del database: al più 3 tentativi, e
  un backup riuscito senza riga chiude il turno (`TestNotturnoStoricoNonScritto`, con la scrittura iniettabile
  `inserisciEsecuzione`). Dopo un riavvio la memoria è vuota: al più 3 tentativi in più.
- **BASSA, corretto:** le scritture staccate dallo stop (riga dello storico, riga della verifica fallita) ora durano
  al più `TempoScritturaStorico` = 30 s (`staccato`: `context.WithTimeout(context.WithoutCancel(ctx), …)`).
- **Documenti:** SVILUPPO diceva che nell'ora ripetuta del 25/10 non c'è mai un backup doppio: falso, allineato al
  Dubbio 3. In lista (nota 10 di `docs/domande-aperte.md`, per la 126): `backup.ora` spostata prima durante il giorno
  fa partire il backup subito; il 302 a `/settings/backup` è un 404 fino alla 126.
- Mutazioni del revisore: **uccise** R1–R4, R8–R12. **Sopravvissute** R5 (taglio del motivo a 1000 byte tolto), R6
  (niente riga «avviso» con lo stop durante la rotazione), R7 (controllo di `n.ctx.Err()` in `EseguiAdesso` tolto).
  **Ora coperte:** R5 da `TestNotturnoMotivoLungo` (1500 «è»: riga scritta, ≤ 1000 byte, UTF-8 valido), R6 da
  `TestNotturnoStopDuranteLaRotazione` (lo stop subito dopo la verifica: riga «avviso», turno fatto), R7 da
  `TestEseguiAdessoServeFermato` (contesto già finito: `ErrFermato`, niente parte). Rifatte da me nel `muta.py`
  (R5, R6, R7) più N1 (tentativi in memoria ignorati) e N2 (backup riuscito senza riga che non chiude il turno):
  **uccise tutte e 5.** File del revisore lasciati dove sono.

## Difetti trovati per strada (solo in lista)

- Corretto qui perché è dentro la sequenza spostata e tocca lo stop richiesto dalla slice: la riga della verifica
  fallita non si scriveva con il ctx finito (vedi «Scelte»). Nient'altro.
- In lista: l'avviso di `-senza-lavori-automatici` nel registro del banco cambia testo (ora nomina il backup
  notturno): solo stderr del container, non confrontato.

## Check

- `scripts/check` senza banco (implementatore): **verde** al primo giro; copertura «voci 353: chiuse 53, rosse 0,
  parziali 192, aperte 97, fuori v1 11» (invariata: nessun segno nuovo). Dopo le correzioni della
  revisione: **verde** al primo giro, stessa copertura.

## Banco

Rigiro di controllo, nessuno scenario nuovo. Il Go del banco parte con `-senza-lavori-automatici` e senza
`backup_chiavetta`: il backup notturno nel banco non parte.

- **s124a:** 6 differenze inattese + 1 errore, tutte lato standalone (500 su `GET /repairs/63`, conteggio artigiani
  3 contro 1, tempo scaduto).
- **s124b** (giro identico, nota 8): 102 differenze inattese + 2 errori in `raccolta-esclusioni[0]` e
  `testi-invii[1]`; la standalone risponde 500 su `GET /audit`, poi tempi scaduti e pagine «(assente)».
- Lato Go: nessun panic e nessuna riga `level=error` nel registro dell'app; nessuno scenario riguarda il backup.
- La guardia di memoria (sotto 10 GB) non è mai scattata.
- Decisione di Stefano (25/11, tramite il coordinatore): «pubblica», come la 109 e la 115.
