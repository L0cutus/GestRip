# 018 — Schema Go di tutte le tabelle dello standalone (inizio di T2)

- **Fonte:** `prompt-via-area.md` (Stefano, 30/10/2026), T2 sintetica. PIANO T2: «Lo schema Go di
  tutte le tabelle si scrive subito, perché l'import deve rifiutare una tabella sconosciuta».
  Standalone a `go-baseline` = `6eb93ed`: le 60 migrazioni di `priv/repo/migrations/`, gli schemi
  Ecto, `docs/import-*.md`.
- **Voci:** nessuna chiusa. OPS-01, OPS-02 e OPS-03 si chiudono con l'import (slice dopo); i segni
  sono stati tolti dai test dello schema, così la copertura non le chiude in anticipo.
- **Lavoro:** implementazione e correzioni di un sottoagente; review e mutazioni di sottoagenti
  diversi; la sessione principale ha rigenerato il riferimento, rifatto le mutazioni e rilanciato
  check e banco.

## Cosa c'è

- **Migrazioni `0006`–`0011`:**
  - 36 tabelle nuove, STRICT, con i nomi italiani;
  - istanti nella forma fissa, date `AAAA-MM-GG`, booleani `IN (0, 1)`, JSON con `json_valid`;
  - soldi in centesimi, come lo standalone;
  - gli elenchi di valori sono quelli dello standalone, tranne il ruolo;
  - 13 trigger.
- **`internal/importer/mappa.go`:** la mappa unica dello standalone verso il Go, tabella per tabella
  e colonna per colonna, con la traduzione di ogni colonna. Contiene anche:
  - le 3 tabelle scartate, con il motivo: `permanent_deletion_permits` è sempre vuota a riposo,
    `schema_migrations` e `sqlite_sequence` non portano dati;
  - le tabelle e le colonne che ha solo il Go;
  - le 4 chiavi segrete di `settings`, che vanno in `segreti_provider` (D-03);
  - per ogni trigger dello standalone, cosa diventa nel Go: portato, CHECK o rinviato all'area.
- **Riferimento ricavato dallo standalone vero:**
  - `tools/schema-standalone/` traduce le migrazioni Ecto in SQL; lo strumento è del revisore, con
    11 trascrizioni a mano per le migrazioni con codice;
  - `rigenera.sh` legge lo standalone solo con `git show`;
  - le fixture stanno in `internal/importer/testdata/` (138 oggetti di schema, 4 chiavi segrete);
  - la sessione principale le ha rigenerate: sono identiche.
- **Test** (`schema_test.go`, `struttura_test.go`, `standalone_test.go`) sulla fixture:
  - **mappa:** ogni tabella e colonna mappata o scartata con un motivo;
  - **colonne:** tipi, NOT NULL o chiave primaria, chiavi primarie, DEFAULT;
  - **chiavi e indici:** chiavi esterne con ON DELETE; tutti gli indici, anche parziali e non unici;
  - **CHECK:** tutti tradotti, compresi i BETWEEN e le regole tra colonne; gli elenchi di valori
    vengono dallo standalone;
  - **trigger:** ogni trigger ha una destinazione;
  - **nomi:** una parola dello standalone deve tornare nel nome Go, così uno scambio di colonne
    simili si vede;
  - **migrazioni:** passano su un database vuoto, e al riavvio non se ne applica nessuna;
  - **forme sbagliate:** rifiutate per il motivo giusto, oltre 50 casi;
  - **vincoli tra più righe:** una sola foto della busta, una sola emergenza aperta, addebito e
    storno, foto rifiutata su una revisione sigillata.

## Prove

- **TDD:** rossi con la mappa vuota (8 su 8), poi con la mappa senza migrazioni (36 tabelle
  mancanti).
- **Mutazioni:**
  - primo giro: **91 uccise su 139**;
  - dopo le correzioni: **133 su 136**, rifatte dalla sessione principale su una copia nuova con
    ripristino verificato;
  - vive: il controllo che cambia un commento, un trigger da BEFORE ad AFTER DELETE (equivalente),
    e lo smistamento di `settings`, che lo prenderà il test dell'import;
  - non applicate: 4, sugli elenchi che ora vengono dalla fixture.
- **Banco verde** con `fab-219` (13 scenari, 156 passi): le migrazioni nuove non cambiano niente di
  visibile. Spento, nessun avanzo.
- `scripts/check` verde; 25 voci chiuse, come prima.

## Review indipendente

Nessuna ALTA o MEDIA. Il revisore ha ricavato lo schema dalle 60 migrazioni con un traduttore suo,
indipendente, e ha trovato tutto identico:
- tabelle, colonne, chiavi esterne, indici e trigger;
- default, CHECK e AUTOINCREMENT.

Corretto:
- **BASSA:** il commento sugli istanti con tre decimali; la regola «NOT NULL o chiave primaria»;
  il riferimento scritto a mano, sostituito con quello ricavato dalle migrazioni vere; i test che
  non coglievano default, CHECK non a elenco, scambio di colonne, chiavi segrete ripetute, numero
  delle migrazioni.

Dichiarato:
- `db.TabelleSegrete` non ha ancora un consumatore (lo avrà l'export);
- in tabelle non segrete restano credenziali come nello standalone (`token_pubblico`, token dei
  lotti, hash del codice Flask), da ricordare quando arriva l'export.

## Note per l'import (slice dopo)

- **Lettura e controllo:**
  - leggere `pragma_table_info` del database sorgente e confrontarlo con la fixture;
  - bloccare tabelle e colonne sconosciute;
  - controllare `typeof()` sui BLOB, perché le tabelle STRICT rifiutano un hash salvato come TEXT;
  - controllare che `permanent_deletion_permits` sia vuota.
- **Traduzioni:**
  - istanti, anche quelli con tre decimali;
  - ruolo;
  - email minuscola, segnalata nel report;
  - `sqlite_sequence`;
  - `reset_password.origine = 'pagina'`.
- **Smistamento e ordine:**
  - smistare `settings`: le 4 chiavi segrete in `segreti_provider`, con email e Telegram spenti
    (D-03);
  - scrivere le foto delle revisioni prima dei sigilli;
  - lasciare ferma la coda dei messaggi (D-27).
- **Riconciliazione:** senza acconti.
- **Conservazione dell'audit:** non cancella niente dopo un import finché non c'è un backup
  verificato.
- **Test di andata e ritorno:** con un valore diverso in ogni colonna.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `f7e437d`.
