# 133c — Porta 23 proposta e archivi remoti riconosciuti cambiando porta (voce 19)

- **Fonte:** voce **19** di `docs/domande-aperte.md` (slice 129b), **decisa da Stefano il 27/11/2026** come
  raccomandata; ricevuta `129b-prova-vera-storage-box.md` (porta 23 OpenSSH con ED25519, cartella `/home/<cartella>`;
  porta 22 ProFTPD con sola RSA, cartella `/<cartella>`; archivi orfani cambiando porta). Ricevute 128, 129, 129c.
  Nella stessa slice il commento della D-77 in `internal/importer/riscritture.go` (scelta definitiva del 09/11/2026).
- **Voci:** nessuna voce nuova chiusa; OPS-16 (destinazione remota) e OPS-28 (rotazione e prova mensile) ritoccate.
  Nessuna D nuova. **Dipendenze e migrazioni nuove:** nessuna.
- **Modelli:** implementatore Opus (~210k token in due giri, ~21 minuti di orologio). Revisore Opus indipendente
  (~60k token, ~7 minuti): nessuna ALTA, nessuna MEDIA, 2 BASSE, corrette (vedi «Revisione»).

## Cosa c'è

- **Porta 23 proposta:** `backup.PortaRemotaPredefinita` passa da 22 a **23** (`internal/backup/remoto.go`): la usa il
  comando (`gestrip backup-remoto -porta`, aiuto «porta SSH del server (per la Storage Box: 23)») e la pagina (campo
  «Porta» del primo passo già scritto «23»). Testi della pagina (`internal/web/backup_remoto.go`,
  `pagine/backup.html`):
  - aiuto del campo: «Per la Storage Box: 23» (era «22 o 23»);
  - campo sbagliato: «La porta è un numero da 1 a 65535 (per la Storage Box: 23).»;
  - porta chiusa: «Il server ha rifiutato la connessione su quella porta (porta chiusa): per la Storage Box usa la
    porta 23.»;
  - secondo passo: «Confrontala con l'impronta pubblicata dal fornitore (per la Storage Box, sulla porta 23:
    l'impronta ED25519 nella documentazione di Hetzner).».
  Una configurazione già salvata con la 22 resta com'è: la pagina mostra la porta salvata.
- **Stessa cartella dalle due porte** (`db.StessaCartellaRemota`, `internal/db/copie_remote.go`): due cartelle remote
  sono la stessa se sono uguali, oppure se tutte e due sono percorsi assoluti e puliti e una è l'altra con **un solo**
  `/home` davanti (`/home/backup` e `/backup`; `/home/home` e `/home`). Usata in due punti, e solo lì:
  - `db.ArchiviRemotiNellaCartella`: le righe «sftp» del registro che la rotazione remota può togliere sono quelle
    della cartella configurata o della stessa vista dall'altra porta; `sessione.ruota` (`remoto.go`) poi confronta,
    per ogni file del server, il **nome** e i **byte** con quelli di queste righe (prima confrontava il percorso
    intero). Tutto il resto della 128 è uguale: solo nomi esatti e file normali, solo dentro la cartella configurata,
    mai la copia appena fatta, un nome registrato nel futuro ferma tutto, ogni file riguardato prima di `Remove`;
  - prova mensile (`ProvaRemota`, `prova_remota.go`): l'ultima copia del registro vale se sta nella cartella
    configurata o nella stessa vista dall'altra porta; `scarica` la cerca nella **cartella configurata con il nome
    del registro** (prima con il percorso del registro, che dall'altra porta non c'è). Byte, SHA-256 e ora di cattura
    si confrontano come prima; la riga di `prove_remote` tiene il percorso del registro.
- **D-77:** il commento di `internal/importer/riscritture.go` dice ora «la scelta definitiva di Stefano del
  09/11/2026 (la strada che non cambia dati, nata nella slice 057)». Solo il commento.
- **Documenti:** manuale cap. 11 (porta 23 già proposta, impronta **ED25519** da confrontare, le RSA ed ECDSA
  pubblicate qui non valgono; paragrafo nuovo «Se un giorno rifai la configurazione con l'altra porta…»);
  `docs/SVILUPPO.md` (sezione nuova «Backup: porta 23 e archivi riconosciuti cambiando porta», 128 e 129c ritoccate,
  `-porta 23`); voce 19 spostata fra le «Risposte già registrate» di `docs/domande-aperte.md`.

## Perché la regola è stretta e sicura

- **Solo il caso misurato:** la 129b ha visto la stessa cartella come `/home/backup` (23) e `/backup` (22). La regola
  vale per un `/home` in più davanti, una volta sola, e richiede percorsi assoluti e puliti: `/home/y` con `/x`,
  `/x/sub` con `/x`, `/homex` con `/x`, `/home/home/x` con `/x`, `/HOME/x`, percorsi relativi o con `//`, `..`, `.` o
  la barra in fondo restano cartelle diverse (`TestStessaCartellaRemota`, 32 coppie). La radice non ha un'altra forma
  (`/home/` non è pulito).
- **La rotazione non allarga cosa cancella:** lavora sempre e solo sui file della cartella configurata, con il nome
  esatto di un archivio del programma, file normali, con una riga «sftp» del registro **di questo apparecchio** con
  lo stesso nome e gli stessi byte. Un file con lo stesso nome registrato in una cartella diversa resta fra i
  «lasciati». Le regole di dubbio della 128 (futuro, ultima copia, file cambiato) non cambiano.
- **La prova mensile non scarica altro:** scarica solo dalla cartella configurata, e la copia scaricata deve avere
  byte, SHA-256 e ora di cattura del registro.

## Prove

`CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -tags nodynamic` su `internal/db`, `internal/backup`, `internal/web`,
`cmd/gestrip`: verdi. Nuovi:

- `db.TestStessaCartellaRemota` (`internal/db/copie_remote_test.go`): 32 coppie, uguali, con e senza `/home`, nei due
  versi, e le cartelle diverse.
- `TestRotazioneRemotaDopoIlCambioDiPorta` (`remoto_linux_test.go`), nei due versi (server `/home/backup` con il
  registro `/backup`, e il contrario): 90 notti di archivi registrati con l'altra forma, più cinque archivi con il
  nostro nome registrati in cartelle diverse (`/home/altra`, `<cartella>/sub`, `/homebackup`, `/home/home<cartella>`,
  `<registro>/sub`) e un `leggimi.txt`. Restano i 7 + 4 (gli stessi giorni di `TestRotazioneRemota`) e i cinque
  estranei, fra i lasciati; 80 tolti, tutti dentro la cartella configurata. Senza la correzione restano tutti e 91.
- `TestProvaRemotaDopoIlCambioDiPorta` (`prova_remota_linux_test.go`): copia vera caricata in `/backup`, cartella
  spostata sul server finto in `/home/backup` e configurazione riscritta: prova riuscita, riga con il percorso del
  registro. Con le cartelle `/home/altra`, `/home/backup/sub`, `/homebackup`, `/home/home/backup` (ognuna con dentro lo
  stesso file): «nessuna». Viceversa: copia caricata in `/home/backup`, cartella tornata `/backup`: riuscita. Senza la
  correzione: «nessuna».
- Ritoccati: `TestPaginaCopiaRemota` (`value="23"`, «Per la Storage Box: 23», la frase ED25519 del secondo passo),
  `TestPaginaCopiaRemotaErrori` (le frasi della porta), `TestOpzioniBackupRemoto` (porta 23 scritta, non la costante).

`PATH=$HOME/sdk/go1.27.3/bin:$PATH scripts/check`: **verde** (voci 353: chiuse 57, rosse 0, parziali 192, aperte 93, fuori v1 11; segreti e dati veri OK; senza `GESTRIP_BANCO_ESITO`).

## Mutazioni

Fatte dall'implementatore con uno script (riga cambiata, test mirati, file rimesso; diff confrontato prima e dopo):
**20 su 20 uccise**.

| # | Mutazione | Uccisa da |
|---|---|---|
| M1 | niente `a == b` in testa | `TestStessaCartellaRemota` |
| M2 | senza il controllo «assoluto» | `TestStessaCartellaRemota` |
| M3 | senza il controllo «pulito» | `TestStessaCartellaRemota` |
| M4 | senza nessun controllo dei percorsi | `TestStessaCartellaRemota` |
| M5 | solo `a == "/home"+b` | `TestStessaCartellaRemota` |
| M6 | solo `b == "/home"+a` | `TestStessaCartellaRemota` |
| M7 | regola allargata a «finisce con» | `TestStessaCartellaRemota` |
| M8 | prefisso `/home/` invece di `/home` | `TestStessaCartellaRemota` |
| M9 | righe del registro filtrate come prima (`==`) | `TestRotazioneRemotaDopoIlCambioDiPorta` |
| M10 | righe del registro di ogni cartella | `TestRotazioneRemotaDopoIlCambioDiPorta` |
| M11 | `ruota` confronta il percorso intero come prima | `TestRotazioneRemotaDopoIlCambioDiPorta` |
| M12 | `ruota` senza il confronto del nome | `TestRotazioneRemota` |
| M13 | prova mensile: cartella confrontata come prima | `TestProvaRemotaDopoIlCambioDiPorta` |
| M14 | prova mensile: cartella non confrontata | `TestProvaRemotaFallisce` |
| M15 | `scarica` dal percorso del registro | `TestProvaRemotaDopoIlCambioDiPorta` |
| M16 | porta proposta 22 | `TestPaginaCopiaRemota` (e `TestOpzioniBackupRemoto`) |
| M17 | aiuto «22 o 23» | `TestPaginaCopiaRemota` |
| M18 | testo del campo porta «22 o 23» | `TestPaginaCopiaRemotaErrori` |
| M19 | testo della porta chiusa «22 o 23» | `TestPaginaCopiaRemotaErrori` |
| M20 | secondo passo senza ED25519 | `TestPaginaCopiaRemota` |

Script e risultati in `to-be-deleted-after-use/_slice-133c/` (da cancellare col commit).

## Revisione (Opus indipendente, ~60k token, ~7 minuti; 16 mutazioni, 14 uccise; 0 ALTA, 0 MEDIA, 2 BASSA)

Mutazioni del revisore: sopravvissute R2 (equivalente) e R13 (l'aiuto di `-porta`, ora coperto). Le due BASSE,
corrette dall'implementatore:

1. **`StessaCartellaRemota` non sa da quale porta arriva ciascun percorso.** Sulla 22 una cartella vera `/home/backup`
   e `/backup` sono diverse, ma la regola le prende per la stessa; così, sulla 23, la cartella `/home` (la radice
   della box) e le righe di `/home/home`. In rotazione diventava toglibile solo un file con lo stesso nome e gli
   stessi byte di un archivio registrato nell'altra cartella (un duplicato); la prova mensile restava sicura.
   **Corretta:** `ConfigRemota.campoSbagliato` rifiuta con `cartellaAmbigua` la cartella vera `/home`, `/home/home` e
   tutto quello dentro `/home/home/` (non `/home/homework`, `/homehome`, `/home/backup/home`). Pagina: «Scrivi la
   cartella dei backup sul server (per esempio «backup»): non la radice «/» e non una cartella chiamata «home».»; il
   motivo di `Controlla` dice «non la radice e non «home» (né dentro «home/home»)». Una configurazione già salvata
   con una di queste cartelle diventa illeggibile e va rifatta (nessuna è ragionevole per i backup). L'ambiguità
   rimasta (una cartella `home` creata a mano e usata dalla 22) è scritta in `docs/SVILUPPO.md`, sezione 133c.
   Test: `TestConfigRemota` (3 rifiutate, 6 simili accettate), `TestPaginaCopiaRemotaErrori` («cartella home»,
   «cartella home/home», con la cartella creata sul server finto). `TestProvaRemotaDopoIlCambioDiPorta` non prova più
   `/home/home/backup` come cartella configurata (ora non si può salvare).
2. **L'aiuto di `-porta` senza test** (R13 sopravvissuta). **Corretta:** l'aiuto delle opzioni va in
   `aiutoBackupRemoto` (sempre `io.Discard` fuori dai test); `TestOpzioniBackupRemoto` legge `-h` e vuole «porta SSH
   del server (per la Storage Box: 23) (default 23)».

Mutazioni dell'implementatore sulle correzioni: **10 su 10 uccise** (`cartellaAmbigua` non usata, ciascuno dei tre
casi tolto, prefisso senza barra finale, regola allargata a `/home/…` o a «contiene home», testo della pagina, aiuto
di `-porta`, predefinita 22 nell'aiuto).

## Per il manuale

- Cap. 11, configurazione della copia remota: porta **23** già proposta; impronta **ED25519** da confrontare con
  quella di Hetzner (le RSA ed ECDSA pubblicate qui non valgono); rifacendo la configurazione con l'altra porta gli
  archivi già caricati si riconoscono e ruotano come sempre. Fatto in questa slice.
- **Schermate da rifare (serve il banco):** `11-copia-remota-desktop.png` e `11-copia-remota-chiave-desktop.png`:
  tutte e due mostrano i campi del primo passo, dove il campo «Porta» ora dice «23» e l'aiuto «Per la Storage Box:
  23». Il testo del secondo passo (ED25519) non è in nessuna schermata.
- Guida della T5.4: dire anche lì «porta 23, impronta ED25519».

## Banco

Banco manuale (giro `man`, «memoria libera» del coordinatore): verde al primo giro, 41 PNG. Restano cambiate solo
`11-copia-remota-desktop.png` e `11-copia-remota-chiave-desktop.png` (Porta 23, aiuto «Per la Storage Box: 23»),
guardate dalla sessione principale; tre PNG cambiate solo per la data o per pochi pixel rimesse com'erano. Guardia
sotto 10 GB attiva, minimo visto 17,4 GB; `banco giu` senza avanzi. `scripts/check` verde con l'istanza su: chiuse 57,
rosse 0.

## LSP

3 chiamate. `documentSymbol` su `remoto_linux_test.go`: utile, ha dato gli aiutanti dei test (`nuovaCopiaProva`,
`remotoRegistrato`, `rimastiSulServer`) senza leggere il file intero. `findReferences` su
`ArchiviRemotiNellaCartella`: utile, conferma che il solo chiamante fuori dai test è `sessione.ruota` (quindi
allargare il filtro tocca solo la rotazione remota). `findReferences` su `StessaCartellaRemota` dopo la modifica:
poco utile, l'indice di gopls non vedeva ancora il nuovo uso in `prova_remota.go` (trovato con grep). Le altre ricerche
(porta 22, testi, schermate) con grep.
