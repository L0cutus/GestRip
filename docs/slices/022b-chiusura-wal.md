# 022b — La chiusura del database porta il WAL nel file principale

- **Fonte:** trovato nella 022: dopo `db.Chiudi()` restavano `-wal` e `-shm` pieni accanto al
  database e il file principale non aveva i dati. Il commento di `serve` («la chiusura dello
  scrittore fa il checkpoint») non era vero. Nessun dato perso: il WAL si rilegge all'apertura.
- **Voci:** nessuna; riguarda `internal/db` (`serve`, i comandi, l'import, i backup futuri).
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha rilanciato `scripts/check` e il banco.

## Cosa c'è

- **Causa:** si chiudeva prima lo scrittore e poi i lettori, che sono in sola lettura: l'ultima
  connessione a chiudersi non faceva il checkpoint.
- **`Chiudi`** (`internal/db/db.go`): chiude prima i lettori, poi fa `wal_checkpoint(PASSIVE)`
  sullo scrittore con una scadenza di 2 s (`TempoMassimoCheckpoint`), poi chiude lo scrittore, che
  per ultimo toglie `-wal` e `-shm`. È idempotente (`sync.Once`, la seconda chiamata restituisce lo
  stesso esito).
- **Non sono errori**, perché i dati restano nel WAL e si rileggono: un checkpoint occupato o
  parziale (un altro processo sul file, o un nostro lettore ancora in uso); lo scrittore che non si
  libera entro la scadenza (una transazione aperta dopo uno Shutdown scaduto: senza scadenza lo
  spegnimento resterebbe fermo fino al kill). Gli errori veri del checkpoint risalgono a `serve`.
- `Chiudi` va chiamata senza query in corso. Chi ha bisogno del file principale completo e da solo
  (import, backup) lo controlla per conto suo: l'import passa già a `journal_mode=DELETE`.

## Prove

- **TDD:** rosso misurato prima della correzione (dopo `Chiudi` `-wal` di 12392 byte e `-shm` di
  32768, file principale senza la tabella); le correzioni della review sono state fatte test prima
  (4 test rossi sul codice di prima: chiusura bloccata oltre 4 s da una transazione aperta, 5 s di
  attesa con una lettura aperta, errore del checkpoint taciuto, seconda chiusura in errore).
- **Test** (`internal/db/chiusura_test.go`): WAL nel file principale; un altro lettore aperto; una
  scrittura in corso (torna in 2 s, il commit dopo non perde dati); una lettura aperta (1,8 ms, dati
  riletti); errore vero del checkpoint (aggancio `istruzioneCheckpoint`); doppia chiusura.
- **Mutazioni:** 10 su 10 uccise, tutte compilano (ordine, checkpoint tolto o ignorato, return
  prima dello scrittore, PASSIVE→TRUNCATE, scadenza tolta o trattata come errore, Once tolto).
- `scripts/check` verde. **Banco verde** con `fab-219` (13 scenari, 156 passi), due volte; dopo lo
  stop del container Go nessun `-wal` o `-shm` nella sua cartella dati. Spento, nessun avanzo.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** la chiusura si bloccava senza limite con una transazione aperta sullo scrittore; il
  commento sui «lettori già chiusi» era falso (`sql.DB.Close` chiude solo le connessioni libere) e
  un nostro lettore in uso faceva aspettare 5 s e lasciava il WAL.
- **BASSA:** fino a 5 s di attesa con il file occupato da altri (ora PASSIVE); doppia chiusura che
  diventava un errore (ora idempotente).

Dichiarato: che i 2 s stiano dentro il tempo di stop di systemd si verifica con l'unità vera (T5).

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** `7efc5e2`, dopo `349607e`.
