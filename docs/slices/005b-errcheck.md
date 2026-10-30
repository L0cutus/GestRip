# 005b — errcheck nel controllo completo

- **Fonte:** richiesta di Stefano del 30/10/2026, dopo la review della 005 (un errore di
  `tx.Commit()` ignorato era rimasto non provato).
- **Confine:** `scripts/check` e la CI falliscono su ogni errore non guardato.

## Cosa c'è

- `errcheck` v1.30.0 come `tool` in `go.mod`, in `scripts/check` dopo staticcheck e nella CI.
- `scripts/errcheck-escluse.txt`: due sole eccezioni motivate (`Rows.Close`, l'errore arriva da
  `rows.Err()`; `Tx.Rollback` in defer, che dopo un commit dà `ErrTxDone`).
- I 54 errori ignorati trovati alla prima passata sono stati sistemati, senza eccezioni globali:
  - prodotto: la chiusura del database in `serve` ora riporta il suo errore (checkpoint del WAL);
    `Apri` unisce gli errori di chiusura a quello di apertura; il backup prima della migrazione
    controlla la chiusura del file riservato e dice se non riesce a togliere una copia fallita;
    `/healthz` scarta in modo esplicito l'errore di scrittura verso un client che se n'è andato;
  - strumenti: le letture di file passano da `os.ReadFile`, niente più file aperti da chiudere;
  - test: gli errori di preparazione (`WriteFile`, `Mkdir`, `Symlink`, `Scan`, chiusure) fermano
    il test.

## Prove

- Prova negativa: un file con `os.Remove("x")` senza controllo fa fallire errcheck (rc 1); tolto.
- `scripts/check` verde.
