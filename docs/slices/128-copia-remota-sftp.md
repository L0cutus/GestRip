# 128 — Copia remota dei backup su SFTP (T5.2, decima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 128, risposte **B** (la chiave SFTP dell'apparecchio entra nell'archivio), **D** (7 + 4 anche sulla Storage
  Box), **E** (sottoaccount, chiave creata dall'apparecchio, impronta controllata alla configurazione, copia dopo la
  chiavetta, controllo della dimensione), **F** (la copia remota da sola non sblocca), **I** (il ripristino spegne
  l'SFTP), «Rischi»; D-114; PIANO §2.5 e §8.2 n. 15; ricevute 123, 124, 126, 127; ripresa
  `docs/handoff/2026-11-25-fine-126.md`.
- **Voci:** OPS-16 (destinazione remota: **chiude**), OPS-28 (fuori sede: in parte, vedi «Segno di copertura»). Nessuna D nuova.
- **Dipendenze nuove:** `github.com/pkg/sftp` **v1.13.13** (diretta dopo `go mod tidy`, revisione) (BSD-2-Clause, Dave Cheney e altri) con la sua dipendenza
  `github.com/kr/fs` **v0.1.0** (BSD-3-Clause, The Go Authors). `golang.org/x/crypto/ssh` viene da
  `golang.org/x/crypto` v0.55.0, già nel go.mod (indiretta per age, ora usata direttamente; BSD-3). Approvate da
  Stefano con il piano. Nessun altro modulo aggiornato.
- **Migrazioni nuove:** **0024** `copie_remote`.
- **Modelli:** implementatore Opus.
- **Banco:** non serve (vedi in fondo).

## Cosa c'è

- `internal/backup/remoto.go`: `ConfigRemota` (+ `Controlla`, `LeggiConfigRemota`, `ScriviConfigRemota`,
  `TogliConfigRemota`, `ChiaveRemoto` = `backup.remoto`), la chiave dell'apparecchio (`CreaChiaveSFTP`,
  `LeggiChiaveSFTP`, `PubblicaSFTP`, `FileChiaveSFTP`, `NomeChiaveSFTP` = `chiave_sftp`), `Remoto` (`Copia`,
  `Prova`, `ChiaveDelServer`, `CaricaRemoto`), il sorvegliante dei tempi, la rotazione remota, i codici fissi
  (`CodiceRemoto…`, `ErrRemoto`, `CodiceRemoto`), i tempi (`TempoConnessioneRemota`, `TempoHandshakeRemoto`,
  `TempoSenzaRisposteRemoto`, `TempoMassimoCopiaRemota`).
- `internal/backup/giro.go`: `Giro.Remoto`, `Giro.Avvio`, `EsitoGiro.Remoto`, `Giro.copiaRemota` (dopo la rotazione
  della chiavetta). `notturno.go`: rilegge la configurazione a ogni backup (`CaricaRemoto`). `stato.go`: campi della
  copia remota, problemi `remoto` e `remoto_vecchio`, `testoRemoto`. `avvisi.go`: i due titoli. `archivio.go`:
  `chiave_sftp` fra i segreti dell'archivio. `sistema_linux.go`/`sistema_altri.go`: `apriNome` (openat).
- `internal/db/copie_remote.go` e `migrazioni/0024_copie_remote.sql`; `registro_backup.go`: `ArchiviNellaCartella`
  esclude le righe `sftp`; nuove `ArchiviRemotiNellaCartella`, `UltimoVerificatoRemoto`.
- `cmd/gestrip/backup_remoto.go`: comandi `gestrip chiave-sftp` e `gestrip backup-remoto`; `backup.go`: con
  `-chiavetta` anche la copia remota se configurata; `main.go`: i due comandi e l'uso.
- `internal/sftpfinto/server.go`: il server SFTP finto dei test (pacchetto a parte perché lo usano `backup` e
  `cmd/gestrip`; nessun pacchetto del programma lo importa).
- `internal/importer/mappa.go`: `backup.remoto` in `ChiaviSoloGo`, `copie_remote` in `TabelleSoloGo`; test adattati
  (`correzioni_test.go` file che toccano `impostazioni`, `schema_test.go` 24 migrazioni, `buste/giri_test.go` la
  0024 nel ritorno indietro).
- Documenti: SVILUPPO (sezione nuova «Backup: copia remota SFTP»), PIANO (§2.5 destinazioni, §8.2 n. 15),
  `docs/domande-aperte.md` (nota 13 nuova).

### Scelte

- **Dove sta la configurazione: nel database** (`backup.remoto`, solo Go), non nel file dell'apparecchio come la
  chiavetta (124). Perché: la 129 la scrive dalla pagina del titolare (serve non scrive in `/etc`), e la risposta I
  prevede proprio che il ripristino **spenga** l'SFTP e la conferma della 131 lo **riaccenda** «come era nel
  backup»: cioè la configurazione viaggia con il backup. La chiavetta resta nel file dell'apparecchio (è il
  montaggio della macchina). Rischio: una copia ripristinata ha configurazione e chiave della macchina vera → nota
  13(a) per la 130.
- **Impronta del server, mai accettata in silenzio:** il comando `gestrip backup-remoto` senza `-impronta` si collega
  solo fino a vedere la chiave del server (si ferma prima dell'autenticazione: `ChiaveDelServer`), stampa tipo e
  impronta (`SHA256:…`, come `ssh-keygen -l`) e dice di confrontarla con quella pubblicata da Hetzner; **non scrive
  niente** ed esce con 1. Con `-impronta` scrive solo se il server si presenta con quella stessa impronta (riscritta
  da chi configura: è la conferma esplicita), se la chiave dell'apparecchio viene accettata e se la cartella c'è.
  Perché un comando e non una riga nel file: la 129 farà la stessa cosa nella pagina (stesse funzioni), e il
  confronto con il fornitore è un passo umano che il comando rende obbligatorio. A ogni collegamento la chiave del
  server deve avere **lo stesso tipo e la stessa impronta** (confronto a tempo costante), e l'handshake chiede solo
  gli algoritmi di quel tipo: un'altra chiave, o un server che non ha più quel tipo, è `impronta` **prima**
  dell'autenticazione (niente known_hosts, niente «prima volta buona»).
- **Chiave dell'apparecchio:** ed25519 in formato OpenSSH (`ssh.MarshalPrivateKey`), `secrets/chiave_sftp`, 0600,
  scritta come la chiave dei backup (temporaneo 0600, fsync, rinomina che non sovrascrive), con il lucchetto dei
  backup (un backup in corso non trova il temporaneo in `secrets/`). `gestrip chiave-sftp` una seconda volta non la
  cambia (il server la conosce già): ristampa la pubblica. La pubblica non è segreta: si stampa ovunque, non solo su
  un terminale. Entra nell'archivio (risposta B).
- **Caricamento:** `.pending` con `O_EXCL` → SHA-256 dei byte **mandati** (deve essere quello della verifica: così
  sul server va proprio l'archivio verificato, anche se il file sulla chiavetta fosse cambiato con gli stessi byte) →
  dimensione del `.pending` → niente con il nome finale → rinomina → dimensione del nome finale. **Cosa è possibile
  sullo SHA-256 dal server:** SFTP non ha un comando per farlo (l'estensione `check-file` è di SSH File Transfer
  draft-ietf-secsh-filexfer-extensions, non c'è in OpenSSH né nella Storage Box); la Storage Box dovrebbe avere
  `sha256sum` come comando SSH sulla porta 23 (non SFTP; non verificato, da guardare nella 129), ma vorrebbe un canale
  di comandi a parte e dipenderebbe dal fornitore. Il controllo
  intero è il download mensile della risposta E, che nessuna riga del piano ha preso (nota 13(c)).
- **Mai sovrascrivere:** `SSH_FXP_RENAME` di OpenSSH (e del server di pkg/sftp) non sovrascrive; in più prima della
  rinomina si controlla il nome finale con `Lstat`, così anche un server che sovrascrivesse (come
  `posix-rename@openssh.com`) non schiaccia un file comparso nel frattempo (provato con il guasto
  `RinominaSovrascrive`). Fra il nostro `Lstat` e la rinomina resta una finestra minima: nella cartella del
  sottoaccount scrive solo questo apparecchio, con il lucchetto dei backup.
- **Registro (risposta F):** la copia riuscita è una riga `sftp` `ok` di `verifiche_backup` con SHA-256 e byte
  dell'archivio della chiavetta e i conteggi della verifica. Le funzioni che sbloccano (`BackupVerificatoDopo`,
  `BloccatoDopoImport`, `UltimoVerificatoSullaChiavetta`, `MediaRovinati`) guardano già solo `chiavetta`: controllato
  con un test (`TestCopiaRemotaNonSblocca`: un registro con sole righe `sftp`, file locale vero con lo SHA-256 giusto,
  non sblocca) e con una mutazione (M14). `ArchiviNellaCartella` (rotazione della chiavetta) ora **esclude** le righe
  `sftp` (un percorso remoto uguale per caso a quello della chiavetta non deve far togliere file locali, M6).
- **Rotazione remota:** stesse regole della chiavetta (`daTenere`, ultima riga `sftp` `ok` = la copia appena fatta,
  nomi esatti, file normali, solo archivi con una riga `sftp` con gli stessi byte, nome registrato nel futuro ferma
  tutto, la copia nuova mai tolta), solo nella cartella configurata; prima di ogni `Remove` si riguarda il nome con
  tipo, byte e ora di modifica (su SFTP non c'è l'inode). Un file con una riga solo `chiavetta` sul server resta.
- **Tempi e stop:** connessione 30 s; handshake, autenticazione e avvio di SFTP 30 s (scadenza sulla connessione);
  poi un **sorvegliante** (orologio solo da `internal/clock`: lettura monotona per «zitto da quanto») chiude la connessione se il server non manda un byte per 2 minuti, se la copia supera 6
  ore, o allo stop (il contesto di `serve`): ogni operazione SFTP in corso finisce con un errore. La lettura
  dell'archivio dalla chiavetta gira in una **goroutine a parte** che scrive in una pipe chiusa anche lei dal
  sorvegliante: una chiavetta appesa non tiene ferma la copia (la goroutine resta appesa con lei). Lo stop di serve
  durante la copia: il giro esce subito, nessuna riga in `copie_remote`, esecuzione `avviso`, lucchetti rilasciati
  (`TestGiroStopDuranteLaCopiaRemota`).
- **Un fallimento remoto non rovina la chiavetta:** la copia non cambia `Riuscito`; un suo errore fa `avviso` (come la
  rotazione non riuscita), il turno è chiuso (il backup non si rifà per la copia remota) e la copia si riprova la
  notte dopo. Il comando esce con 1 e dice «COPIA REMOTA NON RIUSCITA: il backup sulla chiavetta è buono, la copia
  fuori sede no.».
- **Storico `copie_remote` (0024) invece delle tabelle che c'erano:** una copia che non parte (rete, impronta) non ha
  un archivio da mettere in `verifiche_backup`; `esecuzioni_backup` non la scrive il comando e il suo esito è quello
  della chiavetta. Il codice fisso (13 parole, anche nel CHECK) è quello da cui lo stato fa il testo.
- **Stato e avvisi:** `remoto` (ultima copia `fallita` o configurazione illeggibile; testo per codice, senza host,
  utente, cartella né impronta: provato) e `remoto_vecchio` (nessuna copia riuscita da più di 26 ore, **contate dalla
  configurazione** se non ce n'è mai stata una: così configurarla di giorno non accende subito il banner; tace se c'è
  già `backup_vecchio`, la causa vera, e con `-senza-lavori-automatici` come `backup_vecchio`). Un `avviso` (rotazione
  remota) non è un problema, come per la chiavetta. Gli avvisi della 127 li prendono da `Stato.Allarme` senza altro
  codice; i due titoli del «risolto» sono in `titoliAvviso`. Il motivo grezzo (pagina) non ha l'host: gli errori di
  rete passano da `senzaIndirizzo`.
- **Chiavetta appesa (rischio in lista della 124):** il codice che la 128 apre la tocca solo leggendo l'archivio, e
  lì la lettura è in una goroutine a parte con la pipe chiusa dal sorvegliante (sopra). Il resto del rischio (Cifra,
  verifica, `ApriChiavetta` senza tempo massimo) non è codice della 128: resta in lista.

## Prove (server SFTP finto, niente rete, niente credenziali vere)

`internal/sftpfinto`: SSH (`x/crypto/ssh`) e SFTP (server a richieste di pkg/sftp) veri, nello stesso processo, su
un socket Unix in una cartella temporanea (niente porte TCP). `net.Pipe` non va: senza buffer client e server SSH
scrivono insieme la riga della versione e restano fermi (il primo giro dei test si è appeso così, 30 s di handshake).
Guasti: `SpazioFinito`, `CadeDopo`, `FermoAllaScrittura`, `RinominaSovrascrive`, `PerdeDopo` (byte persi senza
errore), `Liberi` (statvfs), `DopoLista`, `AllaPrimaScrittura`.

`internal/backup/remoto_linux_test.go` (chiavetta finta della 123, registro vero):

- `TestCopiaRemotaRiuscita`: byte uguali sul server, niente `.pending`, riga `sftp` giusta, l'ultimo sulla chiavetta
  non cambia.
- `TestCopiaRemotaNonSblocca`: risposta F (sopra); le righe `sftp` fuori dalla rotazione della chiavetta.
- `TestCopiaRemotaImprontaSbagliata`: altra impronta, altro tipo: `impronta`, niente sul server.
- `TestCopiaRemotaChiaveEConfigurazione`: chiave rifiutata (`accesso`), chiave mancante (`chiave`), cartella radice
  (`configurazione`).
- `TestCopiaRemotaCaricamentoInterrotto`: connessione che cade a 100 kB: `.pending` rimasto, niente finale; la copia
  dopo lo toglie e riesce.
- `TestCopiaRemotaNonSovrascrive`: file con il nome finale già lì (resta), file che compare durante il caricamento con
  un server che sovrascriverebbe (resta), `.pending` di altri nomi lasciato.
- `TestCopiaRemotaSpazioPieno`: spazio dichiarato insufficiente (`spazio`, niente caricato); disco che si riempie a
  metà (`caricamento`, `.pending` tolto).
- `TestCopiaRemotaServerLento`: server zitto (`rete`, `ErrTempoRemoto`, finisce in < 10 s) e durata massima.
- `TestCopiaRemotaStopDuranteIlCaricamento`: stop a metà: errore del contesto, nessun codice, nessuna riga.
- `TestCopiaRemotaControlli`: byte persi dal server (dimensione) e archivio cambiato con gli stessi byte (SHA-256):
  `controllo`, niente finale, niente riga.
- `TestRotazioneRemota`: 90 notti: restano i 7 + 4 e tutto quello che non è nostro (senza riga, riga solo
  `chiavetta`, byte diversi, altri nomi, collegamento e cartella con il nome di un archivio); tolti solo nella
  cartella.
- `TestRotazioneRemotaConUnDubbio`: nome registrato nel futuro, ultima copia del registro che non è la nuova, file
  cambiato fra l'elenco e la cancellazione: `rotazione`, la copia resta registrata.
- `TestConfigRemota`, `TestChiaveSFTP`: campi rifiutati, riga scritta/riletta/tolta, JSON rotto; chiave 0600, seconda
  volta uguale, permessi larghi rifiutati, nell'elenco dei segreti dell'archivio.
- `TestGiroConCopiaRemota` (**`// inventario: OPS-16`**, OPS-28 in parte): giro notturno vero (cifratura, verifica con la
  chiave SFTP dentro l'archivio, registro, lucchetti) con la copia; poi un server con un'altra chiave: chiavetta buona,
  esecuzione `avviso`, copia `fallita`/`impronta`, nulla mandato, banner `remoto` anche con `-senza-lavori-automatici`
  e testo senza host/utente/cartella/impronta, turno chiuso; il server giusto la notte dopo spegne il problema.
- `TestGiroStopDuranteLaCopiaRemota`, `TestProblemiRemoto` (confini delle 26 ore, `avviso`, configurazione
  illeggibile, `backup_vecchio` che fa tacere, lavoro spento, testi di tutti i codici, titoli degli avvisi).
- `cmd/gestrip/backup_remoto_linux_test.go`: `chiave-sftp` (creata, poi «già presente»); `backup-remoto` senza
  impronta (la mostra, niente scritto), impronta diversa («IMPRONTA DIVERSA», niente scritto), chiave non ancora nel
  pannello, cartella che non c'è, poi configurazione giusta (cartella risolta `/backup`) e `-togli`; opzioni.
- `-race` (`CGO_ENABLED=1 -tags nodynamic`) su `internal/backup` e `internal/sftpfinto`: pulito (70 s).

### Mutazioni a mano (`to-be-deleted-after-use/_slice-128/muta.py`, `muta.log`, `muta-2.log`)

18 mutazioni. Primo giro **14 su 18**: M7 e M13 non compilavano (variabili rimaste inutili: riscritte), **M2**
(comando che non confronta l'impronta scritta: la configurazione non si scriveva lo stesso, perché `Prova` si collega
con l'impronta scritta e il server la rifiuta; serratura doppia, aggiunto il controllo del messaggio «IMPRONTA
DIVERSA») e **M8** (nome nel futuro: il test lo registrava con l'ora di cattura nel futuro, quindi lo fermava prima
la regola «ultima copia»; ora la riga ha un'ora vecchia). Secondo giro sulle quattro: **18 su 18** (`muta-2.log`); M15 e M16 rifatte dopo il cambio del sorvegliante
(`muta-3.log`): uccise. Elenco: M1
impronta sempre buona; M2 impronta scritta non confrontata; M3 toglie archivi senza riga; M4 riga senza confronto dei
byte; M5 righe remote di ogni destinazione; M6 righe `sftp` nella rotazione della chiavetta; M7 niente controllo
dell'ultima copia; M8 nome nel futuro che non ferma; M9 niente riguardo prima del `Remove`; M10 pulizia di ogni
`.pending`; M11 niente controllo del nome finale prima della rinomina; M12 niente controllo della dimensione; M13
niente SHA-256 dei byte mandati; M14 la riga `sftp` sblocca D-11; M15 server zitto senza tempo massimo; M16 stop che
non chiude; M17 un fallimento remoto rende il backup fallito; M18 `remoto_vecchio` con il lavoro spento.

## Segno di copertura

**`// inventario: OPS-16`** su `TestGiroConCopiaRemota`; **OPS-28 resta parziale** (scelta della revisione). OPS-16
(«backup verificato e cifrato»; destino Go: a caldo ogni notte, cifrato, verificato, su chiavetta **e SFTP**, 7 + 4,
segreti dentro) ha tutte le parti nelle 119-124 e la destinazione remota qui: chiude intera. Il download mensile con
la verifica intera della copia remota (risposta E, nota 13(c)) non è in nessuna slice, e sta in **OPS-28**: è il
controllo della copia **fuori sede** (oggi su otto-vps restic copia e la prova di ripristino controlla), la stessa
famiglia della prova settimanale della 125, non una regola del backup verificato di OPS-16 (lì «riuscito solo dopo la
verifica» vale per l'archivio scritto, che la 121 verifica per intero prima della copia). Quindi OPS-28 chiude quando
il coordinatore assegna il download mensile; i test portano «Voce OPS-28, in parte».

## Per il manuale

- Ogni notte, dopo il backup sulla chiavetta, lo stesso backup (già cifrato) va anche sulla Storage Box, se è
  configurata. Sul server restano gli ultimi 7 giorni e una copia per ognuna delle 4 settimane prima; i file che non
  sono backup del programma non si toccano mai.
- Se la copia fuori sede non riesce, il backup sulla chiavetta resta buono: il riquadro rosso (e il messaggio su
  Telegram o email, se acceso) dice cosa è successo (server che non risponde, chiave rifiutata, spazio finito…). Se
  per più di un giorno non riesce nessuna copia fuori sede, lo dice.
- Configurazione (finché non c'è la pagina della 129): `gestrip chiave-sftp` stampa la chiave pubblica da incollare nel
  pannello della Storage Box; `gestrip backup-remoto -host … -porta 23 -utente … -cartella …` mostra l'impronta del
  server da confrontare con quella di Hetzner; con `-impronta SHA256:…` uguale la fissa. Un server che un giorno si
  presenta con un'impronta diversa non riceve niente.

## Differenze dallo standalone

- Lo standalone non copia fuori sede: lo fa restic da otto-vps alle 03:00 (Storage Box e B2), fuori dal pacchetto, e
  la rotazione 7 + 4 la fa restic. Il Go carica dall'apparecchio e ruota da solo (D-114). Solo Go.

## Dubbi

1. **Porta predefinita 22:** è quella SSH standard; la Storage Box risponde a SFTP sulla 22 e sulla 23. La 129
   (pagina) scelga quale proporre dopo la prova vera.
2. **Tetto di 6 ore:** con ~3,6 GB serve una banda in uscita di almeno ~1,4 Mbit/s. Da misurare nella 129 (piano,
   «Rischi»); se il negozio è più lento la copia fallirebbe ogni notte con `rete`.
3. **Download mensile (risposta E):** non è nella riga della 128 né in un'altra: da assegnare (nota 13(c)).
4. **Segno di copertura:** vedi sopra; decide il coordinatore.

## Difetti trovati per strada (solo in lista)

- Nota 13 nuova in `docs/domande-aperte.md`: (a) la 130 deve spegnere la copia remota e svuotare `copie_remote`; (b)
  cose da misurare nella 129; (c) download mensile senza slice; (d) un archivio caricato e rinominato senza riga nel
  registro resta per sempre sul server (come la nota 6c per la chiavetta); (e) la chiavetta appesa del lavoro
  notturno resta.
- `ChiaveDelServer` e `Prova` del comando non hanno il sorvegliante (solo le scadenze di connessione e handshake e il
  contesto di Ctrl-C): sono comandi a mano, una cartella che non si elenca resterebbe ferma fino a Ctrl-C. Innocuo
  per il backup notturno; la 129 (pagina, «Prova collegamento») dovrà dare un tempo massimo anche lì.

## Revisione (Opus indipendente, ~95k token, ~15 minuti, 16 mutazioni, 11 uccise; `-race` ok)

Nessun ALTA; 1 MEDIA e 8 BASSA. Corretti, con le scelte del coordinatore:

- **MEDIA 1, la pipe della lettura chiusa allo stop non aveva un test** (la M14 del revisore sopravviveva). Gancio
  `lettoreArchivioPerTest` e `TestCopiaRemotaStopConChiavettaAppesa`: la lettura dalla chiavetta si blocca durante il
  caricamento, arriva lo stop, la copia ritorna (< 10 s) con l'errore del contesto. N1 (la M14 rifatta): uccisa.
- **BASSA 2, file troncato dal server dopo la rinomina** restava per sempre: la riga `fallita` aveva i byte
  dell'archivio, il file no, quindi la rotazione non lo riconosceva (il commento diceva il contrario). Ora il nome
  finale appena rinominato che non è l'archivio si **toglie subito**, riguardandolo prima (file normale, stessa
  dimensione e ora di modifica: `togliTroncato`); se il server non lo lascia togliere, la riga `fallita` ha i byte
  **visti sul server**, così la rotazione remota lo riconosce e lo toglie quando esce dalla finestra dei giornalieri;
  codice `controllo`. Un file a 0 byte non entra nel registro (CHECK): resta da togliere a mano, lo dice il motivo.
  Commento corretto. Guasti nuovi del server finto `TroncaDopoRinomina`, `RifiutaRemove`;
  `TestCopiaRemotaTroncataDopoLaRinomina`; N2 (non tolto) e N3 (la M8 del revisore, riga con i byte dell'archivio):
  uccise.
- **BASSA 5, `-togli` faceva partire un «risolto» falso.** Scelta: i problemi `remoto` e `remoto_vecchio` aperti si
  **chiudono in silenzio** quando la copia remota non è più configurata (`chiudiRemotoSpento` in `Avvisi.Giro`, lo
  stato si scrive senza messaggio). Perché: chi la spegne lo fa apposta, con un comando o dalla pagina della 129;
  «risolto» sarebbe falso, e un messaggio «copia spenta» direbbe al negozio una cosa appena fatta da lui; banner e
  pagina non li mostrano più. Riaccesa con il problema ancora lì, nasce di nuovo. `TestAvvisiCopiaRemotaSpenta`; N6:
  uccisa.
- **BASSA 6, temporaneo `.chiave_sftp-…` rimasto da un blackout:** messaggio dedicato in `leggiSegreti`, come quello di
  `.chiave_backup-` («resto di un "gestrip chiave-sftp" interrotto: toglilo a mano»).
  `TestTemporaneoDellaChiaveSFTPRimasto`; N7: uccisa.
- **BASSA 7, go.mod:** `go mod tidy`: `github.com/pkg/sftp` e `golang.org/x/crypto` ora dirette, `github.com/kr/fs`
  indiretta, nessuna versione cambiata; in go.sum solo le impronte dei moduli di test di pkg/sftp (testify, go-spew,
  go-difflib, yaml.v3), nessun requisito nuovo.
- **BASSA 8, segno di copertura:** tolto OPS-28 (vedi «Segno di copertura»).
- **BASSA 9, test mancanti:** `TestCopiaRemotaPendingNonNostri`: un `.pending` che compare fra il controllo e
  l'apertura (gancio `AlLstat` del server finto, che agisce dopo aver risposto «non c'è») non si sovrascrive grazie a
  `O_EXCL` (N4, la M12 del revisore: uccisa); una cartella e un collegamento con il nome esatto di un `.pending`
  restano dopo la pulizia (N5, la M16 del revisore: uccisa).
- Mutazioni delle correzioni (`_slice-128/muta.py`, `muta-revisione.log`): **7 su 7**.

Solo in lista (non corretti, indicazione del coordinatore):

- **3:** rinomina riuscita sul server ma connessione caduta prima della risposta: il nome finale resta senza riga nel
  registro (come la nota 13(d)), la rotazione non lo tocca e la notte dopo il nome nuovo è un altro.
- **4:** una rotazione remota che fallisce sempre dà solo «avviso» (pagina) e nessun banner, come la rotazione della
  chiavetta: lo spazio sul server può finire prima che qualcuno se ne accorga (poi suona `spazio`).

## gopls

`gopls check` sui sei file nuovi o cambiati di più (`remoto.go`, `giro.go`, `stato.go`, `copie_remote.go`,
`backup_remoto.go`, `sftpfinto/server.go`): nessun avviso. `gopls references` su `db.ArchiviNellaCartella`: un solo
chiamante nel programma (`rotazione.go:155`) più due test, quindi escludere le righe `sftp` cambia solo la rotazione
della chiavetta (è servito a decidere di cambiare la funzione invece di aggiungerne una). Il resto con grep e lettura
mirata.

## Check

- `scripts/check` senza banco: primo giro **rosso** su gofmt (il file di prova); secondo **rosso** su
  `TestNessunOrologioFuoriDaClock` (il sorvegliante usava `time.Now`, `time.Since`, `time.NewTicker`: ora
  `clock.Parti`/`Trascorso`, `clock.Aspetta` e `clock.Sistema{}.Adesso()` per le scadenze della connessione; M15 e
  M16 rifatte sul codice nuovo, `muta-3.log`: uccise; `-race` rifatto: pulito); terzo **verde**, copertura «voci 353: chiuse 55, rosse 0, parziali
  192, aperte 95, fuori v1 11» (prima 53 e 97: OPS-16 e OPS-28 chiuse). Log in
  `to-be-deleted-after-use/_slice-128/check-1.log`, `check-2.log`, `check-3.log`.
- **Dopo la revisione:** `-race` su `internal/backup` pulito (72 s); `scripts/check` **verde** al primo giro, copertura
  «voci 353: chiuse 54, rosse 0, parziali 192, aperte 96, fuori v1 11» (solo OPS-16 chiusa; OPS-28 torna aperta senza
  segno). Log `_slice-128/check-4.log`.

## Banco

**Non serve.** Nessun template né rotta toccati: la pagina Backup mostra i problemi nuovi solo con la copia remota
configurata, e il banco non ha `backup.remoto` (né chiavetta: solo «non configurato», che tace con
`-senza-lavori-automatici`, con cui partono tutti e due i Go del banco, `tools/banco/istanza.go:354`,
`gotelegram.go:57`). La migrazione 0024 aggiunge una tabella che nessuna pagina confrontata legge. Niente SFTP né
chiavetta nel banco.
