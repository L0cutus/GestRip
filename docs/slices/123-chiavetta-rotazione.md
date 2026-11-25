# 123 — Chiavetta USB, rotazione 7 + 4, pulizia dei `.pending` (T5.2, quinta slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 123, risposte D (7 + 4, «se manca spazio non cancella altro», chiavetta da 128 GB), F (sblocca solo la
  chiavetta), «Incongruenze», «Rischi» (chiavetta tolta a metà; la T5.4 decide come si monta); D-11, D-113, **D-114**;
  ripresa `docs/handoff/2026-11-25-fine-122.md`, «Prossimo passo»; note 6c, 7b, 8c di `docs/domande-aperte.md`.
- **Voci:** OPS-16 (parte: destinazione chiavetta), OPS-28 (parte: conservazione 7 + 4). Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** nessuno (nessuna pagina cambia).

## Cosa c'è

- `internal/backup/chiavetta.go`: `controllaChiavetta` (strato `sistemaMontaggi` iniettabile: stat, mountinfo,
  permessi), `leggiMontaggi`, `Destinazione` con il lucchetto (`BloccaDestinazione`, `ApriChiavetta`, `Chiudi`),
  `ConfermaChiavetta`, `DestinazioneDaRegistrare`, `PulisciPending`; errori `ErrNonMontata`, `ErrStessoDisco`,
  `ErrNonChiavetta`, `ErrChiavettaSolaLettura`, `ErrChiavettaCambiata`, `ErrDestinazioneOccupata`,
  `ErrRinominaNonSupportata`.
- `internal/backup/rotazione.go`: `daTenere` (scelta 7 + 4), `Destinazione.Ruota`, `ErrRotazione`.
- `internal/backup/sistema_linux.go` / `sistema_altri.go`: ripiego `link`+`unlink` della rinomina (6c),
  `scaricaDallaCache` (7b), flock della cartella, `fstatat`/`unlinkat`/`openat` sulla cartella bloccata; fuori da
  Linux tutto rifiuta.
- `internal/backup/cifra.go`: fadvise dopo l'fsync.
- `internal/db/registro_backup.go`: `ArchiviNellaCartella`, `UltimoVerificatoSullaChiavetta`, attesa dopo un
  ricalcolo fallito (8c: `AttesaDopoFallimento` 10 min → `MaxAttesaDopoFallimento` 6 h); `dopoimport.go` commento.
- `cmd/gestrip/backup.go`, `main.go`: `gestrip backup -chiavetta DIR`; lucchetto anche con `-cartella`.
- Documenti: SVILUPPO (sezione nuova «Backup: chiavetta, rotazione e pulizia», 120 e 122 aggiornate), PIANO (§1.2,
  §2.5 destinazioni e calendario), `docs/domande-aperte.md` (6c in parte, 7b e 8c chiusi).

### Scelte

- **CLI `-chiavetta DIR`** accanto a `-cartella` (una sola delle due), non un'impostazione: oggi non c'è una
  configurazione del backup (arriva con la pagina, 126), e il lavoro notturno (124) chiamerà la stessa sequenza con la
  cartella letta dalla configurazione. `-cartella` resta «una cartella qualsiasi»: non pulisce, non ruota, registra
  `cartella`. La sequenza (apri → pulisci → Cifra → verifica → destinazione → registro → ruota) oggi è in
  `cmd/gestrip/faiBackup`; la 124 la sposterà in una funzione comune (Dubbio 4). Le guardie stanno nel pacchetto
  `backup` (Ruota rifiuta se il registro non ha già il nuovo archivio come ultimo `ok` sulla chiavetta), quindi un
  ordine sbagliato nel chiamante non cancella.
- **Controllo del dispositivo**: tutti insieme, stat **e** mountinfo (punto esatto, maggiore:minore, sorgente),
  sorgente `/dev/…`. Solo stat non basta (un sottovolume btrfs ha uno `st_dev` diverso senza essere un altro disco;
  un tmpfs è «montato»); solo mountinfo non basta (un bind mount). Ogni dubbio è un rifiuto: una chiavetta btrfs con
  st_dev uguale alla cartella sopra sarebbe rifiutata (accettato).
- **Registro:** `chiavetta` solo se i controlli passano all'inizio **e** alla fine (`ConfermaChiavetta`: stesso
  percorso, dispositivo, sorgente, cartella bloccata e archivio dentro); altrimenti `cartella`, «BACKUP NON RIUSCITO
  sulla chiavetta», niente rotazione.
- **Rotazione e registro:** sì, la rotazione rispetta il registro, e più di quanto chiesto: (1) parte solo se l'ultima
  riga `ok` sulla chiavetta (quella che sblocca, `UltimoVerificatoSullaChiavetta`) è il nuovo archivio, quindi
  l'archivio che sblocca non si tocca mai; (2) cancella solo archivi con una riga del registro con lo stesso percorso e
  gli stessi byte: il nome da solo non basta per dire «nostro» (una copia a mano, un'altra macchina). Prezzo: dopo un
  ripristino (130 svuota il registro) gli archivi vecchi della chiavetta restano finché qualcuno non li toglie (Dubbio 1).
- **Quali si tengono:** fra i verificati, l'ultimo di ognuno dei 7 giorni più recenti con un archivio, poi l'ultimo
  di ognuna delle 4 settimane ISO **prima** di quella del più vecchio giornaliero (fino a 11, come la risposta D:
  «per le 4 settimane prima», 11 archivi). Giorni e settimane nel fuso del negozio (`clock.Roma`), non in UTC del nome.
  Dopo la revisione: gli archivi registrati solo come falliti restano finché sono nella finestra dei giornalieri (non
  contano fra i 7 + 4), e un nome nel futuro ferma la rotazione solo se il registro lo conosce.
- **Lucchetto:** flock esclusivo sulla cartella di destinazione, per tutto il giro, anche con `-cartella`: è quello che
  rende sicura la pulizia dei `.pending` (ogni backup del Go che scrive lì lo tiene). Non è «mai due backup insieme»
  (6b resta alla 124: due backup in due cartelle diverse passano).
- **6c:** ripiego `link`+`unlink`, che non sovrascrive mai (EEXIST); un blackout fra i due lascia due nomi dello stesso
  file. Il `.pending` lo toglie la pulizia, ma il nome finale di un backup interrotto dopo il link non ha una riga nel
  registro: la rotazione non lo tocca mai (l'uscita lo dice «lasciato»), va tolto a mano. Senza link (FAT sotto FUSE)
  messaggio chiaro. Un ripiego «Lstat e poi rename» l'ho scartato: rename
  sovrascrive, e la finestra esiste.
- **7b:** fadvise DONTNEED sul `.pending` dopo l'fsync, prima della rinomina e della verifica; errore ignorato.
- **8c:** nella memoria del ricontrollo, per riga e file; un ricalcolo interrotto dal ctx non si ricorda.

## Prove (tutte su dati inventati, cartelle temporanee, nessun montaggio vero)

- `internal/backup/chiavetta_linux_test.go`: `TestControllaChiavetta` (15 casi: buona, non montata per stat, non nella
  tabella, altra cartella nella tabella, stesso dispositivo dei dati, bind mount, stessa sorgente, tmpfs, CIFS,
  montaggio sopra un altro, `ro`, non scrivibile, tabella illeggibile, stat che fallisce, dentro i dati),
  `TestControllaChiavettaPercorsi` (spazio `\040`, collegamento, radice, tabelle malformate), `TestDestinazioneOccupata`,
  `TestDestinazioneDaRegistrare` (7 casi + `-cartella`), `TestPulisciPending` (collegamenti, cartelle, nomi quasi
  uguali, date impossibili, errore di cancellazione), `TestRinominaSenzaRename2` (EINVAL, ENOSYS, nome che c'è già,
  senza link, EACCES che non ripiega), `TestCifraSenzaRename2`, `TestCifraScaricaLaCache`.
- `internal/backup/rotazione_test.go`: `TestRotazioneScelta` (tabella: settembre-novembre con il cambio d'ora del
  25/10, anno 2026-W53 a cavallo, due backup nella notte del cambio d'ora, inizio dell'ora legale con 23:30Z, nome del
  giorno prima in UTC, pochi backup, giorni saltati, verifiche fallite, nessun verificato, mai più di 11).
- `internal/backup/rotazione_linux_test.go`: `TestRuotaChiavetta` (tre mesi; restano gli 11, i file estranei, i
  `.pending`, collegamenti e cartelle col nostro nome, un archivio senza riga, uno con altri byte; la verifica fallita
  si toglie; secondo giro senza effetti), `TestRuotaNonCancellaConUnDubbio` (11 casi, più il controllo positivo),
  `TestRuotaSiFerma` (errore di cancellazione; altri byte, altro inode, collegamento al suo posto),
  `TestGiroCompletoSullaChiavetta` (dieci notti con Cifra, verifica, registro e rotazione veri).
- `internal/db/registro_backup_test.go`: `TestPreparaBloccoImportAttesaDopoFallimento` (attesa, raddoppio fino al
  massimo, orologio indietro, ctx finito durante il ricalcolo, file sistemato, riga nuova).
- `cmd/gestrip/backup_linux_test.go`: opzioni (`-chiavetta`, tutte e due, nessuna), `TestBackupChiavettaNonMontata`
  (sistema vero: cartella temporanea → `ErrNonMontata`, niente scritto, niente righe), `TestBackupCartellaLucchettoSenzaPulizia`.
- Test scritti insieme al codice, non prima: quasi tutti verdi al primo giro (un helper di prova sbagliato, corretto);
  le guardie si sono viste rosse con le mutazioni.
- **Mutazioni a mano** (`to-be-deleted-after-use/_slice-123/muta.py`, `muta.log`), 20, tutte compilano (4 riscritte
  con `&& false` dopo un primo giro che non compilava), **uccise 20:** M1 `.pending` anche collegamenti; M2 nome
  `.pending` senza fine esatta; M3 rotazione anche senza riga del registro; M4 registro non confrontato col nuovo; M5
  nomi nel futuro ammessi; M6 niente controllo prima di togliere; M7 giorni in UTC; M8 settimana del più vecchio
  giornaliero fra le settimanali; M9 verifiche fallite contate; M10 sorgente `/dev` non controllata; M11 stesso disco
  nella tabella non controllato; M12 punto di montaggio solo con stat; M13 `chiavetta` anche con la conferma fallita;
  M14 conferma senza dispositivo; M15 ripiego con `rename` che sovrascrive; M16 niente fadvise; M17 8c senza attesa;
  M18 8c sbloccato durante l'attesa; M19 attesa che non raddoppia; M20 fallimento ricordato anche per il ctx.
  **Equivalenti noti (non provati):** `tieni[nuovo] = true` in Ruota (daTenere lo sceglie già come giornaliero).

## Segno di copertura

**Nessun `// inventario:`.** OPS-16 nel destino Go è «ogni notte, cifrato, verificato, su chiavetta e SFTP, 7 + 4»:
mancano il lavoro notturno (124) e l'SFTP (128). OPS-28 (notturni fuori sede, conservazione, allarme, prova
settimanale) chiude con 124-127. I test portano «in parte».

## Per il manuale

- Un backup sulla chiavetta si fa con `gestrip backup -chiavetta /media/chiavetta` (il punto dove è montata).
- Il programma controlla che lì ci sia davvero una chiavetta montata, diversa dal disco dei dati e scrivibile; se no
  rifiuta e dice perché («non è montata», «sta sullo stesso disco dei dati», «non è scrivibile»…).
- Solo un backup verificato **sulla chiavetta** conta per ripartire con le cancellazioni dopo il passaggio; se la
  chiavetta viene tolta o cambiata durante il backup, il comando dice «BACKUP NON RIUSCITO sulla chiavetta».
- Dopo ogni backup riuscito sulla chiavetta il programma tiene gli ultimi 7 giorni e, per le 4 settimane prima, un
  backup a settimana: gli altri archivi suoi li cancella e lo dice. File messi lì da altri, o archivi che non conosce,
  non li tocca mai.
- Se sulla chiavetta manca spazio il backup non parte e non cancella niente per farne.
- Pezzi rimasti da un backup interrotto (blackout, chiavetta tolta) si tolgono da soli al backup dopo.
- Formattare la chiavetta in **exFAT** o **ext4**: con NTFS montato da ntfs-3g il backup potrebbe rifiutare con un
  messaggio che lo dice.
- Due backup nella stessa cartella insieme: il secondo rifiuta («un altro backup sta scrivendo»).

## Da provare sulla chiavetta vera (Stefano, T5.4, pc-fiacco)

Qui non si può (niente sudo, niente montaggi). Sulla macchina di prova, con una chiavetta vuota da almeno 16 GB e dati
inventati:
1. exFAT montata dal kernel (`mount -t exfat`, o dal desktop): `gestrip backup -chiavetta <punto>` riesce, nel
   registro la riga è `chiavetta` (atteso: `RENAME_NOREPLACE` va, nessun ripiego).
2. NTFS con ntfs-3g (`fuseblk`): il backup riesce con il ripiego `link` (atteso) oppure rifiuta con
   `ErrRinominaNonSupportata`; in nessun caso un archivio sovrascritto.
3. NTFS con il driver del kernel `ntfs3`: come exFAT.
4. Una cartella normale (`-chiavetta /home/x`), la chiavetta montata `ro`, un tmpfs: rifiuto con il messaggio giusto.
5. Chiavetta tolta a metà backup: errore, nessun archivio nuovo contato; rimessa: il backup dopo toglie il `.pending`.
6. Il punto di montaggio che usa la T5.4 (per esempio `/media/gestrip/…` o un'unità systemd) ha un nome che il
   programma accetta (spazi compresi).
7. La chiavetta è scrivibile dall'utente `gestrip`: con exFAT/vfat servono le opzioni `uid=`/`gid=` del montaggio;
   udisks la monta per l'utente del desktop e toglie la cartella allo smontaggio (non va bene per il backup notturno).
8. Un `x-systemd.automount` non ancora scattato (sorgente `systemd-1`) deve dare `ErrNonChiavetta`.
9. flock della cartella e fadvise funzionano su `fuseblk` (ntfs-3g).
10. La chiavetta tolta e rimessa che torna come `sdc` (non più `sdb`) a metà giro deve dare «cambiata».
11. Nessuna partizione interna di pc-fiacco passa i controlli (vedi nota 9a di `docs/domande-aperte.md`).

## Differenze dallo standalone

- Lo standalone non controlla dove scrive e non cancella mai un archivio (la rotazione la fa restic su otto-vps,
  fuori dal pacchetto, con 7 giornalieri e 4 settimanali); il Go ruota da solo sulla chiavetta (D-114), con le regole
  sopra. restic tiene le settimanali anche dentro i giornalieri (fino a 10); il Go tiene 4 settimane **prima** (fino a
  11, risposta D).
- Lucchetto della cartella di destinazione: lo standalone ferma i container, quindi non ne ha bisogno.
- Il ripiego `link` della rinomina non c'è nello standalone (`os.replace`, che sovrascrive).

## Dubbi

1. **Archivi senza riga del registro non si cancellano mai** (regola tenuta dopo la revisione, coordinatore). Dopo un ripristino (130 svuota il registro) o una
   chiavetta usata da due macchine restano finché qualcuno non li toglie; la chiavetta si riempie e il backup fallisce
   con l'allarme (126). Alternativa: cancellare per solo nome (quello che chiedeva il brief). Ho scelto il registro
   perché è più chiuso; da confermare con il coordinatore, e da ricordare nella 130 e nel manuale.
2. **Lo SHA-256 dei vecchi archivi non si ricalcola** per scegliere i 7 + 4 (11 × 3,6 GB ogni notte): conta riga `ok`
   più byte uguali. Un vecchio archivio rovinato può restare fra i settimanali; la prova settimanale (125) guarda solo
   l'ultimo.
3. **Una partizione dello stesso disco fisico** (sda1 dati, sda2 «chiavetta») passa: sorgenti diverse. Riconoscerla
   vorrebbe leggere `/sys/block`; non fatto.
4. **Sequenza nel comando:** la 124 deve spostare la sequenza di `faiBackup` in una funzione comune invece di
   copiarla. Le guardie che cancellano sono già nel pacchetto `backup`.
5. **Rotazione fallita dopo un backup riuscito:** il comando stampa «backup riuscito» e poi «ROTAZIONE NON RIUSCITA»,
   uscita 1. Per la 126 è un avviso, non un backup fallito: da decidere lì.
6. **Attesa 8c (10 min → 6 h):** numeri miei. Durante l'attesa la pulizia scrive comunque l'errore a ogni giro (come
   prima: la nota 8b resta alla 126).
7. LSP (gopls): `findReferences` su `rinominaSenzaSovrascrivere` (chiamanti `CreaChiave` e `Cifra`: il ripiego vale
   anche per la chiave in `secrets/`, dove non serve mai, innocuo). Su `cmd/gestrip/backup.go` gopls aveva una vista
   vecchia del file («line out of range»): lì grep e `go vet`.

## Revisione (Opus indipendente, ~110k token, ~30 minuti, 15 mutazioni)

Nessun difetto ALTA.

- **MEDIA, corretto:** la cartella bloccata non si controllava contro il dispositivo verificato. `blocca` non
  guardava il dispositivo del descrittore e `ConfermaChiavetta` confrontava `identitaDi(d.d)` con `d.id`, preso dallo
  stesso descrittore (sempre vero, R11 sopravvissuta). Con la chiavetta smontata fra controllo e apertura il flock
  finiva sulla cartella vuota del punto di montaggio, sul disco dei dati, Cifra scriveva lì e, con la chiavetta
  rimontata con lo stesso maggiore:minore, la riga diceva `chiavetta`. Ora `suDispositivo` (fstat del descrittore
  attraverso lo strato iniettabile `sistemaMontaggi.identitaAperta`) vuole il dispositivo verificato in `blocca` e in
  `ConfermaChiavetta` (`TestChiavettaSmontataPrimaDelLucchetto`).
- **BASSA, corretto:** un nome nel futuro senza riga del registro fermava la rotazione per sempre: ora ferma solo se
  registrato, altrimenti si lascia (`TestRuotaNonCancellaConUnDubbio`, «nel futuro senza riga»).
- **BASSA, corretto:** un archivio con la sola verifica fallita si toglieva al giro dopo anche se recente: ora resta
  nella finestra dei giornalieri, senza contare fra i 7 + 4 (`TestRotazioneScelta`, `TestRuotaChiavetta`).
- **Test aggiunti:** R7 (rotazione, file su un altro dispositivo lasciato), R13 (l'attesa 8c è legata alla riga: una
  riga nuova sullo stesso file si rilegge subito), R14 (`.pending` su un altro dispositivo non toccato), con
  `guardaVoce` iniettabile. Tutti economici.
- **Documenti:** il secondo nome lasciato da un blackout dopo il link del ripiego 6c (senza riga, mai ruotato, da
  togliere a mano) in SVILUPPO e qui; la lista delle prove sulla chiavetta vera completata (punti 7-11).
- Mutazioni del revisore: **uccise** R1, R2, R3, R4, R6, R8, R12, R14b. **Sopravvissute:** R5, R7, R9, R10, R11, R13,
  R14, R15. R9 e R10 equivalenti; R15 la ferma la guardia del registro in `Ruota`. **Ora coperte:** R7
  (`altro dispositivo`, la mia N7), R11 (N2), R13 (N9), R14 (N8). R5: vedi il log del revisore (non rifatta qui).
- Mutazioni dell'implementatore sulle correzioni (`muta.log`), 9, **uccise 9:** N1 `blocca` senza dispositivo; N2
  conferma senza dispositivo (R11); N3 futuro senza riga che ferma tutto; N4 futuro registrato lasciato; N5 fallite
  recenti non tenute; N6 fallite tenute sempre (prima non compilava, rifatta con `true ||`); N7 rotazione su un altro
  dispositivo (R7); N8 `.pending` su un altro dispositivo (R14); N9 attesa 8c non legata alla riga (R13). Rigirate e
  ancora uccise: M7, M8, M9, M12, M14.
- **In lista** (`docs/domande-aperte.md`, nota 9): (a) seconda partizione, LV dello stesso disco, `/dev/loop*`,
  `/dev/zram*` passano (leggere `/sys/dev/block/M:m`); (b) `-cartella` ora vuole il flock (NFS senza lockd fallirebbe);
  (c) proposta del revisore per la 130: declassare le righe invece di svuotare il registro (Dubbio 1).
- File del revisore lasciati dove sono.

## Difetti trovati per strada (solo in lista)

- Nessuno nuovo nel codice. Il nome dell'helper di prova `voceDi` in `cifra_test.go` («voce» per un elenco di nomi) mi
  ha fatto scontrare con un nome mio: rinominato il mio (`guardaNome`), il test non toccato.

## Check

- `scripts/check` senza banco (implementatore): **verde** al primo giro; copertura «voci 353: chiuse 53, rosse 0,
  parziali 192, aperte 97, fuori v1 11» (invariata: nessun segno nuovo). Dopo le correzioni della revisione:
  `scripts/check` **verde** al primo giro, copertura invariata («voci 353: chiuse 53, rosse 0, parziali 192, aperte 97,
  fuori v1 11»); parole vietate cercate a parte su ricevuta, SVILUPPO, PIANO, domande-aperte e file Go toccati:
  nessuna.
