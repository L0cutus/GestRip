# 014 — Protezioni all'avvio: segreti solo da file privati (OPS-13), dati solo sul volume cifrato (OPS-12)

- **Fonte:** `prompt-via-area.md` (Stefano, 30/10/2026): fine di T1. PIANO T1 e §1.9.
  Standalone a `go-baseline` = `6eb93ed`: `config/runtime.exs`, `bootstrap.ex` (`read_secret`),
  `database.ex`, `database_gate.ex`; test `runtime_config_test.exs`, `database_test.exs`.
- **Voci:** OPS-13 (v1), OPS-12 (sostituita: niente SQLCipher; si tiene il rifiuto di partire senza
  la protezione e il database a 0600).
- **Lavoro:** implementazione e correzioni di un sottoagente; review e mutazioni di sottoagenti
  diversi; check, banco e il giro finale delle mutazioni rilanciati dalla sessione principale.

## Cosa c'è

- `internal/segreti`:
  - `LeggiFile` legge un file privato: regolare, non un link (lstat, poi `O_NOFOLLOW|O_NONBLOCK`,
    poi `SameFile`), senza permessi di gruppo né di altri, entro i limiti di dimensione, in UTF-8,
    senza gli a capo finali come `read_secret`. Oggi non la usa nessuno: il primo uso sarà il
    segreto Flask (T4).
  - `ControllaAmbiente`: `SECRET_KEY_BASE` o `LEGACY_WARRANTY_SECRET` nell'ambiente, anche vuote,
    fermano `serve`, `reset-link` e `invito`, come `runtime.exs` a ogni avvio del rilascio.
  - `NonScaricabile`: `prctl(PR_SET_DUMPABLE, 0)` all'inizio di ogni comando, quindi niente core
    dump, ptrace o `/proc/<pid>/environ` per lo stesso utente. È il corrispettivo di
    `ERL_CRASH_DUMP=/dev/null`.
- `apparecchio.ControllaVolumeCifrato`: la cartella del database deve stare su dm-crypt LUKS
  (uuid `CRYPT-LUKS*`).
  - Il montaggio si riconosce dal `mnt_id` della cartella aperta (`/proc/self/fdinfo`), cercato
    nel campo 0 di mountinfo, non dal percorso più lungo.
  - Si scende per `slaves/` (LVM, RAID md) fino a 16 livelli, e da una partizione si sale al disco.
  - Per btrfs contano tutti i dischi in `/sys/fs/btrfs/*/devices`, con la sorgente risolta.
  - `gestrip.db`, `-wal` e `-shm` già presenti non possono essere link né stare su un altro
    montaggio.
  - Se la cartella manca, il messaggio dice che manca.
- `serve -senza-volume-cifrato` spegne il controllo e stampa «ATTENZIONE … mai sull'apparecchio».
  È spento di default; lo usa solo il banco (`tools/banco/istanza.go`).
- `db.Apri`: il database nasce o torna a 0600 prima che SQLite lo apra, e così anche `-wal` e
  `-shm`. Un link, anche pendente, o un file non regolare sono rifiutati.

## Prove

- TDD: test rossi con le firme vuote, poi verdi; le uscite rosse sono state viste e salvate. Dopo
  la review: test rossi su link, montaggio sopra la cartella madre, partizione md, btrfs by-uuid e
  a più dischi, cartella mancante. Poi verdi.
- Misurato su pc-fisso, con namespace utente e senza sudo:
  - la cartella del repo (btrfs su due nvme in chiaro) e `/dev/shm` sono rifiutate;
  - un tmpfs montato sopra la cartella madre e un bind mount del solo file sono visti;
  - dopo `PR_SET_DUMPABLE=0` il processo legge ancora fdinfo e mountinfo, e
    `/proc/<pid>/environ` dà EACCES.
  - Un vero LUKS non c'è: LUKS, md e btrfs a più dischi sono provati solo su alberi `/proc` e
    `/sys` sintetici.
- Mutazioni (in una copia): **94 uccise su 121 che compilano**. Le 27 vive sono equivalenti:
  - si vedono solo con una corsa tra due controlli dello stesso file;
  - danno lo stesso rifiuto, preso da un altro controllo;
  - non hanno nessun effetto osservabile.

  Le 27 lacune del primo giro hanno avuto il loro test, e il giro di verifica l'ha rifatto la
  sessione principale su una copia nuova: 27 su 27 uccise.
- Banco **verde** con `fab-219` (9 scenari, 121 passi; il Go parte con l'opzione), spento, nessun
  avanzo. `scripts/check` verde con l'esito: chiuse **OPS-12, OPS-13** (18 voci chiuse).

## Review indipendente (due giri)

Primo giro: due MEDIA, verificati e corretti.
- Il controllo era sulla cartella e non sul file: con `gestrip.db` link verso un disco in chiaro,
  `serve` partiva.
- Un montaggio sopra la cartella madre non si vedeva.

Corrette anche le BASSA: partizione su md, btrfs by-uuid, btrfs a più dischi, `-wal`/`-shm` link,
messaggio della cartella mancante, test che non compilavano su Windows.

Secondo giro: nessuna ALTA né MEDIA; gli scenari del primo giro, riprovati sul kernel, ora vengono
rifiutati.

Restano dichiarate, BASSA:
- servono root, o lo stesso utente del servizio con permesso di scrittura sulla cartella madre:
  - corsa tra il controllo e `db.Apri` (percorso risolto contro percorso dato) se una cartella
    intermedia è un link e viene cambiata in mezzo;
  - id dei montaggi riusati tra la lettura di fdinfo e quella di mountinfo;
  - corsa tra `Lstat` e `Chmod` su `-wal`/`-shm`;
- LUKS con `cipher_null` passa: il cifrario non si vede da `/sys`;
- un loop device sopra un file su LUKS è rifiutato (prudente);
- i test sintetici impostano gli id a mano: il kernel vero l'ha provato il revisore.

## Differenze dallo standalone (in `docs/domande-aperte.md`, 3–6)

- Il Go non legge file di segreti oggi: la chiave del server sta nel DB (008b).
- `LeggiFile` toglie solo gli a capo finali, come `read_secret`. Per il segreto Flask
  `runtime.exs` usa `String.trim`: si decide in T4, quando arriva il primo uso.
- Il messaggio sulle variabili non rimanda a `*_FILE`, che nel Go non esistono.
- Un database link è rifiutato (lo standalone lo accettava), e così `-wal`/`-shm` link o su un
  altro montaggio: domanda 6.
- L'opzione `-senza-volume-cifrato` (domanda 3), btrfs e installer (domanda 4), `LimitCORE=0` e
  `UMask=0077` nel servizio (domanda 5).

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `56f762e` (013b).
