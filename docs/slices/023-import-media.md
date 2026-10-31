# 023 — I file media nell'import: foto, firme, documenti, logo

- **Fonte:** T2 sintetica (PIANO T2, §1.9), OPS-02 e OPS-05 (media con hash confrontati), ricevuta
  della 022. Standalone a `go-baseline` = `6eb93ed`: `lib/gestrip_standalone/media_store.ex`
  (`referenced?`, `read_object` con il limite di 16 MiB), `lib/gs/legacy_import/media.ex` come
  precedente.
- **Voci:** nessuna nuova chiusa. OPS-02 resta parziale: ora porta anche i media, ma lo
  spegnimento dei canali (D-03) e il fermo della coda (D-27) arrivano con la slice dei canali, come
  per JOB-08 e CLI-22. OPS-05 resta parziale (link storici con VEN-31). OPS-01 e OPS-07 hanno test
  in più.
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha tolto i segni di OPS-02 messi dall'implementatore, rilanciato
  `scripts/check` e il banco.

## Cosa c'è

- **Opzioni:** `-media CARTELLA` (la cartella media dello standalone, solo letta), oppure
  `-senza-media` (per ora **solo con `-sintetico`**: domanda 5), e `-miniature CARTELLA`
  facoltativa (si contano file e byte, non si copia niente). Se il database nomina file e non c'è
  né l'una né l'altra, l'import rifiuta.
- **Colonne guardate** (`importer.ColonneMedia`): quelle di `MediaStore.referenced?` dello
  standalone (logo e icone, firma della busta e degli eventi di consegna, foto delle buste, delle
  vendite e delle revisioni, documenti storici) più la coda delle cancellazioni, facoltativa. Il test
  le confronta con lo schema dello standalone, con le esclusioni motivate.
- **Per ogni file:** chiave `^[0-9a-f]{64}$` (niente percorsi), file regolare (Lstat, apertura
  `O_NOFOLLOW|O_NONBLOCK`, stesso file dopo l'apertura), dimensione in 1..16 MiB e uguale a quella
  della riga **prima di copiare un byte**, SHA-256 dei byte copiati uguale alla chiave e allo
  `sha256` della riga. Ogni anomalia è bloccante con tabella, id, colonna e codice (`media/mancante`,
  `non_regolare`, `troppo_grande`, `vuoto`, `byte_diversi`, `hash_diverso`, …), mai chiave o
  contenuto. Un file nominato da più righe si copia una volta.
- **Copia:** in `.gestrip-import-*/media` (0700), file 0400 con fsync, fsync delle cartelle.
- **Report** (`media`): per famiglia attesi, copiati, mancanti, hash diversi, altre differenze e
  byte; file e byte copiati; file in più (avviso, non copiati: domanda 4); file della coda già
  cancellati; miniature.
- **Attivazione:** report temporaneo → rename senza sovrascrivere (`RENAME_NOREPLACE`) di `media/`
  accanto alla destinazione → fsync → link del database → link del report → fsync. Se il database
  o il report non prendono il nome, i media si tolgono. I media vanno per primi: mai un database
  senza i suoi file.
- **Resti:** dopo un arresto di colpo fra i media e il database resta `media/` senza database.
  L'import nuovo lo riconosce come resto e chiede di toglierlo a mano insieme a `.gestrip-import-*`;
  **`serve` rifiuta di partire** se il database manca e accanto ci sono `media/` o
  `.gestrip-import-*`, invece di creare un database vuoto vicino alle foto (domanda 3).
- Gli errori dei media non nominano chiavi né percorsi dentro la cartella di lavoro.

## Prove

- **TDD rispettato** (prova salvata durante il lavoro): test scritti prima, poi stub con soli nomi
  e tipi: 11 test e 29 sottotest rossi; correzioni della review: 8 test e 9 sottotest rossi sul
  codice di prima. Alcuni rinforzi scritti dopo il codice per uccidere mutazioni vive, dichiarati.
- **Test:** import completo con i media (andata e ritorno, cartella media confrontata nel secondo
  import identico), 12 casi di media che non tornano, file in più, senza cartella, rifiuti senza
  file, miniature, attivazione, resti, errori senza chiavi, dimensione prima di copiare (file sparso
  di 16 MiB + 1: niente scritto), file scambiato durante l'apertura, `-senza-media` solo sintetico,
  `serve` accanto ai resti.
- **Mutazioni:** secondo giro 58, tutte compilano: **53 uccise**, 5 vive (3 fsync, 2 difese
  equivalenti).
- `scripts/check` verde; `GOOS=windows go vet` di `./internal/importer` e `./cmd/...` verde.
  **Banco verde** con `fab-219` (13 scenari, 156 passi), spento, nessun avanzo.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** media senza database dopo un SIGKILL, con `serve` che avrebbe creato un database vuoto
  accanto; chiavi dei file nei messaggi d'errore (con un disco pieno, per esempio); nessun limite di
  dimensione prima della copia (un file sbagliato da 256 MiB si copiava per intero).
- **BASSA:** apertura che seguiva un link a una FIFO comparso dopo il controllo; controllo «stesso
  file» senza test; commento sbagliato; file della coda già cancellati non contati; completezza delle
  colonne controllata solo sui nomi; commento del rename.

Dichiarati: il proprietario dei file (0400, dell'utente che lancia l'import) si fissa con
l'installer (nota in PIANO T5); `RENAME_NOREPLACE` misurato su btrfs e tmpfs, ext4 su LUKS solo
ragionato; i tempi con circa 4 GB di foto si misurano nella prova sui dati veri; il controllo dello
spazio libero aspetta la domanda 2.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `ebf6eb7`.
