# schema-standalone

Ricava dallo standalone vero i riferimenti con cui i test di `internal/importer` controllano la
mappa e lo schema Go (slice 018):

- `internal/importer/testdata/standalone-go-baseline.sql`: il DDL finale dello standalone al tag
  `go-baseline` (tabelle, indici, trigger; nessun dato);
- `internal/importer/testdata/chiavi-segrete-standalone.txt`: le chiavi di `settings` che
  contengono un segreto di un provider (D-03).

## Come si rigenera

    tools/schema-standalone/rigenera.sh

Serve `python3` (solo la libreria standard) e la copia dello standalone in
`~/sviluppo/gestrip-standalone` (o `GESTRIP_STANDALONE`). Il tag si sceglie con
`GESTRIP_RIFERIMENTO` (di default `go-baseline`). Lo script:

1. estrae con `git show <tag>:priv/repo/migrations/…` le migrazioni `.exs` (60 a go-baseline, senza
   `.formatter.exs`) e i file di `lib/` che tolgono dei campi da `Inspect`, in una cartella nuova
   sotto `to-be-deleted-after-use/` (lo standalone non si tocca: niente checkout);
2. con `ecto2sql.py` traduce le migrazioni in SQL SQLite e le applica in ordine a un database
   vuoto. Il traduttore copre il sottoinsieme del DSL di Ecto che lo standalone usa; le 11
   migrazioni con funzioni private o interpolazioni sono scritte a mano in `overrides/`, una per
   versione;
3. con `estrai.py` scrive i due file di `testdata/`. Il DDL è il dump di `sqlite_schema` senza
   `sqlite_sequence`, più `schema_migrations` nella forma di ecto_sqlite3 (la crea Ecto, non una
   migrazione). Una chiave è segreta se il campo che la legge (`campo: setting("chiave")`) è tra
   quelli che il modulo toglie da `Inspect` (`@derive {Inspect, except: [:campo]}`).

Alla fine dice dove ha lasciato i file di lavoro, da cancellare. Se `git diff` sui due file di
`testdata/` mostra differenze, lo schema dello standalone è cambiato: i test dicono cosa manca.

## Limiti

- Il traduttore è fatto a mano: un DEFAULT booleano esce come `1`/`0`, dove ecto_sqlite3 potrebbe
  scrivere `true`/`false` (i test li considerano uguali). Il riferimento definitivo resta un
  database vero dello standalone: l'import lo confronta con quello sintetico del banco (T3).
- Una migrazione nuova con funzioni private o interpolazioni va scritta in `overrides/`.
