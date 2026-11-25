# 125 — Prova di ripristino settimanale e media rovinati (T5.2, settima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 125, risposta **G** («la domenica notte, dopo il backup, sull'ultimo archivio della chiavetta; fallisce anche
  se l'archivio ha più di 8 giorni»), H (allarme: banner e avvisi restano alla 126-127); D-15; **D-116** (Stefano,
  25/11, scelta A: media rovinati annotati, il backup non fallisce; «nella 126, o nella 125 se più pulito»);
  ripresa `docs/handoff/2026-11-25-fine-122.md`; ricevuta della 124 (calendario, lucchetto, tentativi).
- **Voci:** OPS-28 (parte: prova settimanale). Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** non serve (vedi in fondo).

## Cosa c'è

- `internal/backup/prova.go`: `Prova` (`Esegui`), `EsitoProva`, `SettimanaDi`, `EtaMassimaProva` (8 giorni),
  `ErrProvaSenzaBackup`, `ErrProvaVecchio`, `ErrProvaFuoriChiavetta`; sul `Notturno`: `ProvaDovuta`, `GiroProva`,
  `ProvaAMano`, `provaProtetta`.
- `internal/backup/notturno.go`: il lavoro fa la prova dopo il backup nello stesso giro; `dovuto` diviso in
  `statoTurno` + `daRifare` (stessa regola per backup e prova); `nonScritte` con una chiave per backup e prova;
  `inserisciProva` iniettabile.
- `internal/backup/verifica.go`: `Verificato.MediaRovinati` (D-116); `internal/backup/giro.go`: la riga
  «ATTENZIONE: N media…» e i media rovinati nella riga del registro.
- `internal/db/migrazioni/0023_prove_ripristino.sql`: tabella `prove_ripristino` e due colonne di
  `verifiche_backup` (`media_rovinati`, `media_rovinati_nomi`).
- `internal/db/prove_ripristino.go`: `RegistraProvaRipristino`, `TentativiProva`, `UltimaProvaRipristino`,
  **`ProvaSettimanaleFallita`** (per la 126); `internal/db/registro_backup.go`: `VerificaBackup.MediaRovinati`,
  `MaxNomiRovinati` (100), `ArchivioRegistrato.CatturatoIl` (solo da `UltimoVerificatoSullaChiavetta`),
  **`MediaRovinati`** (per la 126).
- `cmd/gestrip/backup.go` e `main.go`: comando **`gestrip prova-backup -chiavetta CARTELLA [-db FILE]`**;
  `verifica-backup` stampa anche i media rovinati.
- `internal/importer/mappa.go`: `prove_ripristino` in `TabelleSoloGo`, il suo indice in `IndiciSoloGo`.
- Documenti: SVILUPPO (sezione nuova «Backup: prova di ripristino settimanale e media rovinati»), PIANO (§2.5
  calendario), `docs/domande-aperte.md` (nota 10(c), per la 126).

### Scelte

- **Registro: tabella nuova `prove_ripristino` (migrazione 0023).** Non `verifiche_backup`: una seconda riga `ok`
  per lo stesso archivio cambierebbe chi è «l'ultimo verificato» (D-11) e farebbe righe doppie in
  `ArchiviNellaCartella` (la rotazione); e una prova fallita perché l'archivio è vecchio non è la verifica di un
  archivio. Non `esecuzioni_backup`: il CHECK di `avvio` e il conteggio dei turni sono del backup, e cambiare il CHECK
  vorrebbe ricostruire la tabella. La tabella nuova ha avvio, settimana, istanti, esito, motivo, archivio/SHA-256/
  ora di cattura (obbligatori per `ok`, CHECK) e il numero di media rovinati.
- **Quale archivio:** l'ultimo verificato sulla chiavetta **del registro** (`UltimoVerificatoSullaChiavetta`), non
  il file più nuovo della cartella: è quello da cui si ripartirebbe, e la rotazione non lo toglie mai. Deve stare
  nella chiavetta configurata, montata e controllata (`ApriChiavetta`, `ConfermaChiavetta`), e la verifica deve
  dare gli stessi byte, SHA-256 e ora di cattura del registro.
- **Età:** ora di cattura del registro contro l'inizio della prova; **> 8 giorni** fallisce (otto giorni esatti
  passano), una cattura nel futuro fallisce (`ErrOrologioBackup`). L'età si controlla **prima** della verifica: un
  archivio vecchio fallisce subito, senza leggere 3,6 GB (il motivo dice l'età).
- **«Domenica notte, dopo il backup»:** la prova ha una *settimana*, la domenica del turno del backup. È dovuta
  appena il turno del backup di adesso è **chiuso** (un'esecuzione automatica `ok`/`avviso` o i 3 tentativi
  finiti). Quindi la domenica alle 02:30 la prova parte nello stesso giro, subito dopo il backup, sull'archivio
  appena scritto (`TestProvaDopoIlBackupDellaDomenica`). Vedi Dubbio 1 sul significato di «domenica notte».
- **Recupero:** se l'apparecchio era spento la domenica notte, la settimana resta da fare fino alla domenica dopo:
  la prova parte al primo giro utile, dopo il backup di recupero (10 minuti dall'avvio). Tentativi come il backup
  (`daRifare`, una sola regola): al più 3 per settimana, a 30 minuti, fino alla prima riuscita; le prove a mano non
  contano. Una riga che non si scrive si conta in memoria (come la revisione della 124: niente verifica intera a
  ogni minuto).
- **Mai fermare l'app:** la prova gira dentro `giroProtetto`/`provaProtetta` (panico recuperato, prova fallita con
  `DettaglioPanico`), con il contesto dei lavori di `serve` (lo stop la ferma senza righe). **Lucchetti:** il
  lucchetto dei backup (occupato: niente, riprova al minuto), poi quello della chiavetta: lo stesso ordine del giro
  della 124. La pulizia delle cartelle temporanee rimaste si fa anche qui, con il lucchetto preso.
- **Allarme:** nessun banner né Telegram. `db.ProvaSettimanaleFallita` dice se l'ultima prova (automatica o a mano)
  è fallita; una prova a mano riuscita dopo lo spegne (così il titolare, sistemata la chiavetta, non aspetta la
  domenica dopo).
- **Comando `gestrip prova-backup`:** stessa prova, lucchetto dei backup, riga `a_mano`, esce con 1 se non riesce.
  `-chiavetta` obbligatorio come `gestrip backup -chiavetta` (il file dell'apparecchio non lo legge, come il
  comando del backup).
- **D-116 fatto qui** (lato verifica e registro, niente banner). È più pulito che nella 126: la verifica calcola
  già lo SHA-256 di ogni `media/<chiave>` per confrontarlo con l'inventario, quindi basta confrontarlo anche con il
  nome (un media è a indirizzo di contenuto); il risultato va nella riga che il giro scrive comunque. Nella 126
  avrebbe voluto riaprire la verifica. **Costo: nessuno SHA-256 in più**, un confronto di 64 byte per media (per
  una foto da ~1 MB lo SHA-256 costa dell'ordine del millisecondo, il confronto nanosecondi: sotto lo 0,001%). Non
  misurato a parte: non c'è niente di nuovo da misurare. Un media rovinato entra nell'archivio con la sua impronta
  vera, l'inventario torna, quindi non è un rifiuto: riga `ok` (D-11 e rotazione invariati), con il numero e i
  primi 100 nomi. `db.MediaRovinati` legge l'ultimo backup verificato sulla chiavetta: lo stato resta finché un
  backup nuovo non li trova più.

## Prove (dati inventati, cartelle temporanee, orologio finto, chiavetta finta della 123)

- `internal/backup/prova_linux_test.go`: `TestSettimanaDi` (domenica, lunedì, sabato, capodanno, cambi d'ora),
  `TestProvaDovuta` (02:29/02:30 della domenica, prima e dopo il backup, a mano non conta, 30 minuti e 3 tentativi,
  settimana fatta, spento la domenica e recupero il martedì dopo il backup, backup fallito 3 volte chiude il turno,
  senza chiavetta), `TestProvaDopoIlBackupDellaDomenica` (lavoro vero: backup e prova nello stesso giro, stesso
  archivio, niente cartelle temporanee, lucchetto libero, allarme spento), `TestProvaArchivioVecchio` (8 giorni e 1
  ora fallisce e accende l'allarme; 8 giorni esatti a mano passano e lo spengono; 8 giorni e 1 secondo; cattura nel
  futuro; righe stampate), `TestProvaFallisce` (senza backup, chiavetta non montata, archivio tolto, archivio
  cambiato, SHA-256 o ora di cattura diversi dal registro, chiave diversa, archivio fuori dalla chiavetta, non
  configurata), `TestProvaStopPanicoLucchetto`, `TestProvaRigaNonScritta`, `TestMediaRovinatoNonFaFallire` (giro
  riuscito con il media rovinato, riga del registro, prova riuscita con 1 media rovinato, a zero dopo il media
  sistemato).
- `internal/db/prove_ripristino_test.go`: `TestRegistraProvaRipristino` (12 righe rifiutate, rilettura, motivo
  lungo, CHECK), `TestProvaSettimanaleFallita` (acceso/spento con prove automatiche e a mano, `TentativiProva`),
  `TestMediaRovinatiNelRegistro` (nome malformato, 150 rovinati → 100 nomi in ordine, fallite e cartelle non
  contano, righe di prima a 0, CHECK).
- `cmd/gestrip`: `TestProvaBackupComando` (opzioni, senza database, lucchetto preso senza righe, chiavetta non
  montata con il sistema vero: messaggio, uscita con errore, riga `a_mano` fallita).
- Aggiornati: `internal/buste/giri_test.go` (migrazioni 19…23), `internal/importer/schema_test.go` (23 migrazioni).
- Rossi visti al primo giro: solo il test «chiave diversa» (il mio test toglieva la chiave due volte); il codice è
  passato al primo giro.
- **Mutazioni a mano** (`to-be-deleted-after-use/_slice-125/muta.py`, `muta.log`), 16, tutte compilano, **uccise
  16:** M1 età 9 giorni; M2 età con `>=`; M3 cattura nel futuro accettata; M4 settimana dal lunedì; M5 prova senza
  aspettare il backup; M6 calendario con le prove a mano; M7 prova fallita non registrata; M8 lavoro senza prova;
  M9 media rovinato che fa fallire la verifica; M10 media rovinato non annotato; M11 giro che non passa i media
  rovinati al registro; M12 «ultima prova» = la prima; M13 niente confronto SHA-256 con il registro; M14 niente
  `ConfermaChiavetta`; M15 prove non scritte ignorate; M16 media rovinati letti anche da una verifica fallita.

## Segno di copertura

**Nessun `// inventario:`.** OPS-28 (notturni fuori sede, conservazione, allarme, prova settimanale) chiude con
126-127 (allarme) e 128 (fuori sede): la 125 ne fa solo la prova. I test portano «in parte».

## Per il manuale

- Ogni domenica notte, subito dopo il backup, il programma prova a riaprire l'ultimo backup della chiavetta come
  in un ripristino vero (lo decifra con la chiave dei backup e controlla database e foto), senza toccare i dati del
  negozio.
- La prova non riesce anche se l'ultimo backup buono ha più di 8 giorni, se la chiavetta non c'è o se l'archivio è
  rovinato: allora parte l'allarme (dalla pagina Backup).
- Se l'apparecchio era spento la domenica notte, la prova si fa appena si riaccende, dopo il backup.
- Dopo aver sistemato il problema si può rifare la prova subito: `gestrip prova-backup -chiavetta /punto/di/montaggio`.
- Se il backup trova una foto rovinata sul disco (il contenuto non è più quello salvato), il backup vale lo stesso,
  ma il programma lo segnala.

## Differenze dallo standalone

- Lo standalone non ha la prova nel pacchetto: la fa otto-vps (stessa regola degli 8 giorni). Il Go la fa dentro
  `serve`, con il recupero, il registro delle prove e il comando a mano.
- Lo standalone non controlla il contenuto dei media contro il nome nel backup (D-116 è solo Go).

## Dubbi

1. **«Domenica notte»:** l'ho presa come il backup del turno della domenica (le 02:30 di domenica, la notte fra
   sabato e domenica). Se Stefano intende la notte fra domenica e lunedì basta spostare la settimana di un giorno in
   `SettimanaDi` (una riga e i test). Il negozio è chiuso la domenica in tutti e due i casi.
2. **Due prove in poche ore:** acceso la domenica alle 01:00 dopo una settimana spento, si fa alle 01:10 la prova
   della settimana prima (dopo il backup di recupero del sabato) e alle 02:30 quella della domenica. Innocuo.
3. **Primo avvio dopo l'aggiornamento a metà settimana** (revisione): la tabella delle prove è vuota, quindi la
   settimana in corso (quella della domenica passata) è da fare e la prova parte subito dopo il primo backup
   notturno o di recupero, non la domenica dopo. Innocuo: una prova in più, sull'archivio appena scritto.
4. **Prova mai fatta:** oggi non accende niente; per la 126 (nota 10(c)).
5. **Il comando a mano scrive nel registro delle prove** (a differenza di `verifica-backup`): prova solo l'archivio
   del registro di questo database, non sblocca niente, e serve per spegnere l'allarme.
6. LSP (gopls): `workspaceSymbol` per controllare i nomi nuovi (`ProvaSettimanaleFallita`); `findReferences` su
   `RegistraVerificaBackup` e `UltimoVerificatoSullaChiavetta` ha dato risposte fuori posto (indici vecchi, mentre
   lo script delle mutazioni cambiava i file): i chiamanti (un solo scrittore del registro, `Giro.registra`; due
   lettori, rotazione e prova) li ho controllati con grep e `go vet`. Il resto con grep e lettura.

## Revisione (Opus indipendente, ~90k token, ~35 minuti, 14 mutazioni, 12 uccise; `-race` ok)

Nessun difetto ALTA né MEDIA. Corretti i BASSI:

- **Comando dopo uno stop o una riga non scritta:** diceva «il motivo… è nel registro delle prove» anche senza riga.
  Ora `provaBackup` guarda se la riga è scritta: senza dice «PROVA DI RIPRISTINO NON FINITA O NON REGISTRATA: …
  nel registro delle prove non c'è una riga nuova» (`TestProvaBackupComandoRigaNonScritta`, tabella tolta).
- **Panico:** la prova si registrava con `DettaglioPanico` («il backup si è interrotto…»); ora
  `DettaglioPanicoProva` («la prova di ripristino si è interrotta…»).
- **Uscita chiusa (`| head`):** un errore di `Dire` faceva fallire la prova (riga «broken pipe», allarme acceso per
  niente). Ora `Prova.dire` ignora l'errore; il comando lo ricorda e lo dice alla fine, ma la riga ha l'esito vero
  (`TestProvaLucchettoRestiEUscita`).
- **Primo avvio a metà settimana:** solo scritto (Dubbio 3).
- **Commento della migrazione 0023** su `media_rovinati`: una prova con la verifica passata e poi fallita (SHA-256
  o ora di cattura diversi dal registro) porta il numero vero; corretto il commento.
- **Mutazioni sopravvissute del revisore:** M13 (niente `PulisciResti` nella prova) e M14 (niente controllo
  `diDati`): ora uccise da `TestProvaLucchettoRestiEUscita` (lucchetto nullo e di un'altra cartella dei dati
  rifiutati senza toccare `.verifica-backup-77`; con il lucchetto giusto tolta e detta). Rifatte nel `muta.py` come
  R13 e R14: **uccise 2 su 2** (`_slice-125/muta-revisione.log`).
- **Test in più:** `TestProvaCambioDOra` (Europe/Rome): 29/03/2026, nessuna prova alle 01:59 CET, backup alle 03:00
  CEST e prova vera della settimana del 29/03; 25/10/2026, backup e prova alla prima 02:30 CEST, niente nell'ora
  ripetuta (02:29 CET torna al turno del sabato, settimana del 18/10 fatta) né alla seconda 02:30.

## Difetti trovati per strada (solo in lista)

- Nessuno nel codice toccato.

## Check

- `scripts/check` senza banco (implementatore): **verde** al primo giro; copertura «voci 353: chiuse 53, rosse 0,
  parziali 192, aperte 97, fuori v1 11» (invariata: nessun segno nuovo). Log in
  `to-be-deleted-after-use/_slice-125/check.log`.
- Dopo le correzioni della revisione: **verde** al primo giro, stessa copertura (`_slice-125/check2.log`).

## Banco

Non serve: nessuna pagina cambia (niente rotte, niente template), la migrazione aggiunge solo una tabella e due
colonne di tabelle solo Go, e il Go del banco parte con `-senza-lavori-automatici` e senza `backup_chiavetta`
(né backup notturno né prova).
