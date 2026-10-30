# 022 — `gestrip import`: dal database dello standalone a un database Go nuovo

- **Fonte:** T2 sintetica (PIANO T2), note per l'import nelle ricevute 018 e 020. Standalone a
  `go-baseline` = `6eb93ed`: `lib/gs/legacy_import.ex` e `lib/gs/legacy_import/*.ex` come
  precedente (lì l'import era Flask→Elixir).
- **Voci chiuse:** OPS-01, OPS-04, OPS-07, OPS-10 (sostituita da `gestrip import`). ADM-17 ha un
  test in più (conservazione ferma dopo l'import).
- **Voci parziali, senza segno:** OPS-02 e OPS-05 (mancano foto, firme, PDF e link storici: slice
  023 e VEN-31); OPS-03 (domanda 1 in `docs/domande-aperte.md`). JOB-08 e CLI-22: l'import porta i
  dati (verificato nel test completo), il comportamento arriva con le loro aree.
- **Fuori perimetro:** la cartella media (023), l'attivazione dei vecchi link (VEN-31), la prova
  sulla copia dei dati veri (serve l'ok di Stefano).
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha rilanciato `scripts/check` due volte e letto i punti critici.

## Cosa c'è

- **Comando:** `gestrip import -sintetico | -autorizzato -riferimento "…" -sorgente F -sha256 HEX
  [-dimensione N] -destinazione F -report F`. Controllo del volume cifrato come `serve` (OPS-12) e
  dei segreti nell'ambiente (OPS-13). Esce con 0 solo se riconciliato; stampa modalità, verdetto,
  report, sha256 e righe, mai il percorso della sorgente.
- **Rifiuti prima di aprire la sorgente, senza creare file:** modalità mancante o doppia,
  `-riferimento` vuoto o dato con `-sintetico`; destinazione o report già esistenti; destinazione o
  report uguali alla sorgente o a un suo file accanto; `-journal`, `-wal`, `-shm` accanto alla
  sorgente (ricontrollati dopo la copia); sorgente che non è un file regolare; **resti di un import
  interrotto** (`.gestrip-import-*`) nella cartella della destinazione.
- **Sorgente:** copia in `.gestrip-import-*` (0700, nome casuale) nella cartella della
  destinazione, confronto di SHA-256 e dimensione, apertura della copia `mode=ro&immutable=1`,
  `integrity_check`. La cartella di lavoro si toglie sempre (con fsync della cartella).
- **Controlli:** tabelle e colonne contro la mappa (una differenza di schema ferma tutto prima di
  copiare); tabella sconosciuta con nome e numero di righe; `permanent_deletion_permits` vuota;
  `typeof()` delle colonne BLOB; chiavi di `settings` che usa solo il Go (`ChiaviSoloGo`) bloccanti;
  due account attivi uguali dopo la normalizzazione dell'email bloccanti.
- **Copia:** una transazione, ordine della mappa, chiavi esterne rimandate, stessi id. Traduzioni:
  istanti da 0 a 6 decimali con la Z (altre forme bloccano, niente troncamenti), date, booleani,
  JSON, ruolo, email (normalizzata con avviso). `sqlite_sequence` = max(seq, max id).
  `reset_password.origine = 'pagina'`. Le 4 chiavi segrete in `segreti_provider`.
  `codici_primo_titolare` vuota.
- **Nessuna riga sparisce in silenzio:** ogni rifiuto (traduzione, CHECK, unico, non nullo, tipo,
  trigger, chiave esterna) è una differenza bloccante con codice, tabella, id e colonna, mai il valore.
- **Segno dell'import:** tabella solo Go `import_standalone` (migrazione 0013), una riga con
  modalità, sha256, byte e istante. La conservazione dell'audit non cancella niente finché c'è (il
  registro dei backup verificati non esiste ancora: la slice dei backup toglierà il blocco).
- **Report e attivazione:** report JSON 0600 con conteggi letti e scritti per tabella, totali delle
  colonne in centesimi, avvisi, differenze, sha256, modalità e riferimento, verdetto. Ordine:
  temporaneo del report sincronizzato, controllo dello stop, link della destinazione, link del
  report, fsync della cartella. I link non sovrascrivono mai. Dopo il commit: `journal_mode=DELETE`,
  `integrity_check`, `foreign_key_check`. Bloccato: report sì, destinazione no. Interruzione:
  niente report, niente temporanei. Un solo istante per report e segno.
- **Ripetibilità:** la destinazione è sempre nuova; due import della stessa sorgente danno lo stesso
  contenuto e lo stesso report, istante a parte.

## Prove

- **TDD: non rispettato al primo giro.** L'implementatore ha scritto il codice prima dei test; il
  rosso (16 test su 16) l'ha misurato dopo, su uno stub. Le correzioni della review sono state
  fatte test prima: 7 rosse sul codice di prima, 1 già verde (protegge un comportamento che c'era,
  provato dalle mutazioni).
- **Test:** database sintetico con lo schema esatto dello standalone e righe in tutte le tabelle
  mappate, un valore diverso in ogni colonna (andata e ritorno); secondo import identico; sorgente
  rovinata, troncata, incompleta; schema diverso; righe che il Go rifiuta; email; report senza
  valori; rifiuti senza file; import autorizzato; interruzione a metà in processo e con SIGTERM a un
  processo figlio; SIGKILL e resti; nessuna sovrascrittura; stop prima del link; verifica finale;
  conservazione ferma dopo l'import.
- **Mutazioni:** 65, tutte compilano: **59 uccise**, 6 vive (2 equivalenti, 1 quasi equivalente,
  3 su fsync o difese che non si vedono in un test).
- `scripts/check` verde; copertura: chiuse 16 (prima 12).

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** resti in chiaro dopo SIGKILL o mancanza di corrente: ora un import nuovo rifiuta finché
  non si tolgono (domanda 3); nessun test difendeva «non si sovrascrive mai»: aggiunti.
- **BASSA:** ordine report/attivazione e fsync della cartella; stop subito prima del link; due
  letture dell'orologio; chiavi solo Go nella sorgente; file accanto ricontrollati dopo la copia;
  destinazione sul nome di un file accanto alla sorgente.

Dichiarati:
- **D-03 e D-27 (canali spenti, coda ferma):** nel Go non ci sono ancora canali né coda, quindi
  oggi non parte niente. Le chiavi e le righe `queued` si importano come sono. Lo spegnimento lo
  scrive la slice dei canali (nota in PIANO T4).
- Viste, indici, trigger e `sqlite_stat*` in più nella sorgente non si confrontano.
- Nel report di un import bloccato «scritte» conta le righe della transazione annullata.
- L'import non scrive una riga d'audit (romperebbe la ripetibilità); il database importato non ha
  la chiave del server, la crea `serve` al primo avvio.
- Manca il controllo dello spazio libero (domanda 2).
- **Trovato fuori dall'import:** dopo `db.Chiudi()` il WAL resta accanto al database e il file
  principale non ha i dati; il commento di `serve` sul checkpoint alla chiusura non è vero. Nessun
  dato perso (il WAL si rilegge). Slice 022b.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `0682875`.
