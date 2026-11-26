# 130 — Ripristino isolato `gestrip restore` (T5.2, quattordicesima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 130, risposta **I** e «Rischi»; inventario OPS-18, OPS-19, D-12; `docs/domande-aperte.md` nota 13(a)(c);
  ripresa `docs/handoff/2026-11-26-fine-129c.md`. Decisione tecnica del capo-slice: lo stato «isolato» sta nei dati
  ripristinati (riga `impostazioni`), così sopravvive ai riavvii fino alla conferma della 131.
- **Voci:** OPS-18 (**chiude**, vedi «Segno di copertura»), OPS-19 (parte: la conferma è la 131). Nessuna D nuova.
- **Dipendenze nuove:** nessuna (`golang.org/x/sys/unix` c'era già). **Migrazioni nuove:** nessuna.
- **Modelli:** implementatore Opus (~340k token in due giri, ~35 minuti di orologio), revisore Opus indipendente
  (~142k token, ~6 minuti). **Banco:** non serve (vedi in fondo).

## Cosa c'è

- **`gestrip restore -archivio FILE -cartella CARTELLA-NUOVA [-chiave FILE]`** (`cmd/gestrip/ripristino.go`): senza
  `-chiave` chiede di incollare la riga `AGE-SECRET-KEY-…`, con l'eco del terminale spento (rimesso sempre, anche con
  Ctrl-C; `terminale_linux.go`). Esce 2 per un errore d'uso, 1 per un errore, messaggio «RIPRISTINO NON RIUSCITO».
  Segnala su stderr le cartelle `.ripristino-*` rimaste da un ripristino interrotto (non le tocca). Con una chiave dai
  permessi larghi (chiavetta FAT) dice di copiarla in una cartella propria con `chmod 600`.
- **`backup.Ripristina`** (`internal/backup/ripristino.go`): la destinazione non deve esistere o deve essere vuota
  (anche senza file nascosti), rifiutata **prima di decifrare**; collegamenti, file e cartella padre mancante
  rifiutati; spazio = archivio + 10 GiB. Lavora in `.ripristino-XXXX` accanto, fa la **verifica completa della 121**
  (ora `verificaFile(…, tutto=true)` estrae anche media e segreti, 0600 con fsync), scrive `secrets/chiave_backup`,
  migra, isola, fsync, e alla fine `syscall.Rename` (che sostituisce solo una cartella vuota; `os.Rename` la
  rifiuta sempre, misurato). Un errore o uno stop tolgono la temporanea e lasciano la destinazione com'era.
- **`db.IsolaRipristino`** (`internal/db/ripristino.go`), in una transazione: toglie `canale_email_acceso`,
  `canale_telegram_acceso`, `backup.remoto` (conservati in `spento` per la 131) e `backup.avvisi`; svuota
  `verifiche_backup`, `esecuzioni_backup`, `prove_ripristino`, `copie_remote`, `prove_remote`; rinnova `chiave_server`
  tenendo la vecchia come `chiave_server_del_backup` (mai usata da `serve`, domanda 14(a)); scrive
  `ripristino.isolato` (formato, ora, solo il nome dell'archivio, SHA-256, ora di cattura, `spento`). Un backup di una
  copia già isolata conserva i valori della macchina vera. `ripristino.isolato` è in `ChiaviSoloGo` dell'importer.
- **`serve` sulla copia isolata** (`cmd/gestrip/main.go`): legge lo stato a ogni avvio (rovinato → non parte); nessun
  provider (né coda né lettore getUpdates, anche con gli interruttori riaccesi a mano), nessuna chiavetta, niente
  sezione della copia remota, né pianificatore né notturno né avvisi dei backup; faccia pubblica
  **`web.PubblicaInManutenzione`**: 503 «GestRip è in manutenzione. Riprova più tardi.» su tutto tranne `/healthz`;
  avviso «COPIA ISOLATA …» su stderr. Il banner dei backup tace come con `-senza-lavori-automatici`.
- **Comandi da console sulla copia isolata** (`rifiutaCopiaIsolata`, `cmd/gestrip/backup.go`): `backup` (`-cartella`
  e `-chiavetta`), `backup-remoto` (anche `-togli`) e `prova-backup` (`-chiavetta` e `-remota`) rifiutano;
  restano `verifica-backup` (non scrive registri), `chiave-backup` e `chiave-sftp` (scrivono solo nella copia).

## Prove

- `internal/backup/ripristino_linux_test.go`: cartella nuova con dati, foto, segreti, permessi, chiave e registri
  (elenco fisso delle 5 tabelle, una riga ciascuna prima del backup); destinazioni rifiutate; cartella piena
  rifiutata prima di decifrare; errori che non lasciano niente (chiave di un'altra macchina, chiave non age, archivio
  toccato o mancante, database rovinato, conteggi diversi da `snapshot.json` → `ErrFotografia`, stop); spazio; file
  arrivato nella destinazione prima della rinomina non perso.
- `internal/db/ripristino_test.go`, `internal/web/manutenzione_test.go` (anche `/foto/`, `/thumb/`, `/reset/x`, `%2F`).
- `cmd/gestrip/ripristino_linux_test.go`: `TestRestoreComando`, `TestRestoreChiaveSenzaEco` (pseudo-terminale vero),
  `TestRestoreRestiEChiaveConPermessiLarghi`, `TestComandiSullaCopiaIsolata`, **`TestServeCopiaIsolata`** (`serve`
  avviato due volte, bot configurato e interruttori riaccesi a mano: il Bot API finto riceve 0 richieste; niente
  pianificatore, backup, avvisi; banner vuoto; faccia pubblica in manutenzione, interna che funziona; messaggio ancora
  `queued`), `TestServeIsolamentoRovinato`.
- `-race` (CGO=1, `-tags nodynamic`, `-run` mirato) verde sui test di serve, comando e ripristino. `scripts/check`
  verde rilanciato dal capo-slice: **chiuse 56, rosse 0**.

## Revisione (Opus indipendente, 21 mutazioni, 13 uccise; nessuna ALTA, 4 MEDIA, 6 BASSA)

- **MEDIA 1, comandi da console non isolati:** su una copia di prova `backup-remoto` + `backup -chiavetta` avrebbero
  caricato sulla Storage Box vera; `backup -chiavetta` da solo avrebbe ruotato gli archivi della chiavetta.
  Corretto (rifiuto, sopra). Scelta del capo-slice come lettura prudente della risposta I («backup automatici e SFTP
  spenti»): Stefano può volerla diversa.
- **MEDIA 2, messaggi in coda alla conferma** (avvisi doppi): non è di questa slice, domanda 14(b) per la 131.
- **MEDIA 3 e 4, test che non provavano:** tabelle svuotate controllate con la lista stessa; `controllaDatabase` nel
  ripristino mai provato. Corretti (sopra).
- **BASSA:** eco della chiave, resti `.ripristino-*`, cartella non vuota controllata solo da `rename(2)`, percorsi della
  manutenzione, banner nella copia isolata, chiave su FAT: tutti corretti. Restano solo ragionate: punto di montaggio
  come destinazione (EBUSY/EXDEV, il messaggio dice di usare una sottocartella), stop nell'istante prima della
  rinomina (finestra minima).
- **Mutazioni delle correzioni: 10 su 10 uccise** (guardie di `backup`, `backup-remoto`, `prova-backup`;
  `prove_remote` tolta; `controllaDatabase` saltato; `len(voci) > 1`; `|| isolata`; eco non rimesso; messaggio dei
  permessi; resti non segnalati). Restano sopravvissute M17 (cartella creata non tolta se la rinomina fallisce per
  EBUSY/EXDEV) e M21 (stop subito prima della rinomina), casi rari.
- La revisione ha controllato che la prova settimanale (125) e quella mensile (129c) fanno le stesse cose di prima.

## Segno di copertura

**`// inventario: OPS-18`** su `TestRipristinoInCartellaNuova`: ripristino su macchina nuova con la sola chiave dei
backup, controlli completi, chiave di sessione nuova. I vecchi QR `/g/…` usano il segreto Flask, che il ripristino
tiene identico; la parte «vecchi QR» del destino dipende anche dalla T4.13 (incongruenza 6 del piano). Se il
coordinatore preferisce chiudere OPS-18 dopo, basta togliere la riga. **OPS-19** resta in parte fino alla 131.

## Domande per Stefano (in `docs/domande-aperte.md`, voce 14, con raccomandazione)

(a) chiave del server vecchia: i link email di conferma/disiscrizione e i link artigiano `g1.` firmati con la vecchia
si rompono sulla copia; raccomandato separare le sottochiavi alla 131 (sessione, CSRF, galleria dalla nuova; email e
artigiano dalla vecchia). (b) righe in coda e «Esegui adesso» dei promemoria durante l'isolamento: alla conferma
partirebbero tutte (avvisi doppi); raccomandato che la 131 le mostri e chieda «manda» o «scarta», e che durante
l'isolamento «Esegui adesso» rifiuti. (c) IA e sidecar non spenti: raccomandato spegnere il modello remoto. (d) banner
«copia isolata» nella faccia interna (oggi solo stderr): con la 131. (e) riga di audit alla conferma. (f) `restore` con
alias `ripristina`.

## Per il manuale

- Se l'apparecchio si rompe, i dati tornano con `gestrip restore`: servono il file del backup e la chiave dei backup
  del foglio, che si può incollare quando il comando la chiede (mentre la incolli non si vede).
- Il ripristino scrive solo in una cartella nuova o vuota; se dentro c'è già qualcosa si rifiuta e non tocca niente.
- Prima di dire «riuscito» controlla tutto il backup; se qualcosa non va non lascia dati a metà.
- La copia ripristinata parte «isolata»: niente email né Telegram, niente promemoria automatici, niente backup né copia
  fuori sede, anche dai comandi da console.
- Finché è isolata, chi apre il link di una busta o di una garanzia vede «GestRip è in manutenzione. Riprova più
  tardi.». Il personale invece può entrare e guardare.
- L'isolamento resta anche se si riavvia il programma, finché il titolare non conferma che questa è la macchina vera.
- Lo storico dei backup riparte da zero: dopo la conferma serve un backup nuovo.

## Differenze dallo standalone

- Lo standalone isolava la copia con una rete Docker interna (`restore.sh`, `--live` per un disastro) e non aveva un
  comando per metterla online. Il Go isola dentro il programma e la mette online con la conferma del titolare (D-12,
  slice 131).

## Banco

Non serve: la slice aggiunge un comando e un modo di avvio di `serve` che il banco (standalone contro Go sugli stessi
dati) non esercita; le prove sono test con un archivio inventato e un `serve` di prova, come chiede il piano.

## LSP

Implementatore 3 volte (chiamanti di `RinnovaChiaveServer`, `Dipendenze.CopiaRemota`, `verificaFile`), revisore 3
(2 riuscite): utile per i chiamanti, per il resto grep.
