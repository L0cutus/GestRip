# 022c — Mutazioni complete sull'import e due bug dei percorsi

- **Fonte:** domanda di Stefano (31/10/2026): la 022 era stata scritta prima dei test e le 65
  mutazioni di allora erano sul codice prima delle correzioni della review. Giro mirato su tutti i
  rami prima di andare avanti. Il giro ha trovato due bug del Go, corretti qui.
- **Voci:** nessuna nuova (rinforza OPS-01, OPS-04, OPS-07, OPS-10 e OPS-12).
- **Lavoro:** mutazioni e test di un sottoagente, correzioni di un secondo sottoagente, review di un
  terzo; correzioni della review fatte dalla sessione principale, che ha rilanciato `scripts/check`
  e il banco.

## Mutazioni sulla 022

- 385 mutazioni, tutte compilano (33 riscritte perché non compilavano), su `importa.go`,
  `sorgente.go`, `report.go`, la parte d'import di `conservazione.go`, il comando `import` di
  `cmd/gestrip/main.go`, la migrazione `0013`.
- 213 uccise dai test che c'erano; delle 172 vive, **91 uccise da 31 test nuovi** (prova rossa con la
  mutazione, verde senza): `internal/importer/rami_test.go`, `cmd/gestrip/import_rami_test.go`,
  `cmd/gestrip/import_rami_linux_test.go`. Tra le lacune chiuse: opzioni e rifiuti, permessi dei
  temporanei, istante in UTC, verifica finale delle chiavi esterne, traduzioni rifiutate, vincoli
  della 0013 (14 su 15 erano vive), forma del report, `sqlite_sequence` di tutte le tabelle, SIGINT
  al processo.
- 81 vive alla fine, nessuna lacuna: equivalenti (stesso comportamento osservabile, una riga di
  motivo per ognuna) o non osservabili senza agganci nuovi (fsync, errori di I/O, una corsa fra
  `Lstat` e `Open`, un secondo file system), più quella del bug 1, superata dalla correzione.
- Un dubbio diventato decisione: trigger e viste della sorgente si ignoravano; Stefano ha deciso
  che bloccano con `schema/oggetto_diverso` (nota in OPS-01), da fare nella 025. Una cartella della
  destinazione che è un link a una cartella è accettata: coerente con D-34 (si controlla la
  cartella reale).

## Bug corretti

1. **Resti non visti con i metacaratteri del glob.** Il controllo dei resti `.gestrip-import-*`
   (import e `serve`) usava `filepath.Glob` sul nome della cartella: con `negozio[` l'import
   rifiutava con «syntax error in pattern»; con `negozio[1]/` i resti non si vedevano e l'import si
   attivava accanto (e `serve` sarebbe partito creando un database vuoto). Ora `cartelleDiLavoro`
   legge la cartella (`os.ReadDir` + prefisso), una sola strada per import e `serve`. Una cartella
   che non si può elencare ora dà errore invece di «nessun resto» (più prudente).
2. **Percorsi con caratteri dell'URI.** `db.Apri`, il backup prima delle migrazioni e l'import
   costruivano `file:<percorso>` senza codifica: con `%41` «unable to open database file», con `?`
   o `#` SQLite apriva un altro file (`…/con`) accanto alla cartella. Ora `db.URIDi` codifica `%`,
   `?`, `#` e dà l'autorità vuota ai percorsi assoluti (anche `//` in testa); `Apri` rende il percorso
   assoluto (dalla review: un relativo `file:…` o `:memory:` rompeva il backup o apriva un database
   in memoria).

## Prove

- **TDD:** rosso misurato prima di ogni correzione (resti con `[`, `[1]`, `*`, `?`, `\` per import e
  `serve`; percorsi con `%41`, `%`, `?`, `#`, spazio, non ASCII, `//`; backup; relativi `file:rel.db`,
  `:memory:`, `file:con?domanda.db`).
- **Mutazioni sulle correzioni:** 15, tutte compilano, tutte uccise (2 vive al primo giro, uccise da
  un test aggiunto); la tolta di `filepath.Abs` è uccisa dal test dei relativi.
- `scripts/check` verde; con l'esito del banco **30 voci chiuse**, 3 parziali (ACC-13, ACC-14,
  CLI-21), come prima. **Banco verde** con `fab-219` (13 scenari, 156 passi), spento, nessun avanzo.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** `VACUUM INTO` del backup riceveva il percorso nudo: con un `-db` relativo che comincia
  con `file:` nasceva un file estraneo e la migrazione era rifiutata. Ora `Apri` rende assoluto il
  percorso.
- **BASSA:** `:memory:` relativo apriva un database in memoria (stessa correzione); un test con
  `Chmod 0` falliva da root (ora salta); l'errore della ricerca dei resti in `serve` ora dice che
  cosa stava cercando; «Illeggibile» nei nomi dei test.
- **Dichiarato, non corretto:** alcuni aiuti dei test aprono ancora i database con `"file:"+p`; con
  i nomi di `t.TempDir()` oggi va bene.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** vedi sotto.
