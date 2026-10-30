# Slice 001 — Strumenti di controllo (T0)

- **Livello:** leggero (strumenti di sviluppo, nessun comportamento del prodotto).
- **Fonte:** PIANO §5, §9; prompt di avvio (repo pubblico dal primo commit, gitleaks a versione fissata).
- **Confine:** hook dei commit (gitleaks e parole vietate), `scripts/check`, `scripts/limiti.sh`,
  staticcheck fissato, CI GitHub. Nessuna voce dell'inventario.

## Ricevuta di completamento

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Fatto:**
  - `scripts/githooks/pre-commit` (gitleaks sullo staging; `--tutto` = repo e history) e
    `commit-msg`;
  - `scripts/parole-vietate`, con l'elenco fuori dal repo in `~/.config/gestrip-go/`;
  - `scripts/check` con tutti i passi sotto `scripts/limiti.sh`;
  - staticcheck 2026.2.2 come `tool` del modulo;
  - `.github/workflows/ci.yml` con le azioni fissate per hash (checkout v7.0.1, setup-go v7.0.0).
- **Prove:**
  - `scripts/githooks/pre-commit --tutto` → rc 0 sulla history attuale;
  - un messaggio con una parola dell'elenco → rc 1 (prova negativa);
  - `scripts/check` verde in 15 s.
- **Debiti:**
  - la CI GitHub non è mai girata: il repo GitHub non esiste ancora (lo crea Stefano);
  - la copertura non gira in CI finché l'inventario non è nel repo (A2).

### Correzioni dopo la review indipendente (30/10/2026)

- **`parole-vietate --tutto` poteva passare in silenzio** quando un file tracciato era sparito dal
  worktree: `xargs` dava 123 e l'`if` risultava falso. Ora ogni ricerca decide su ciò che trova
  (`git grep` e `grep -c`), e un errore di lettura ferma il commit.
  - Prove negative: un file in staging con una parola vietata → rc 1; la stessa parola con un
    file tracciato cancellato → rc 1.
  - In più: controllati anche i nomi dei file in staging, `--no-ext-diff`, via CRLF e spazi in
    coda dall'elenco.
- **`scripts/check` chiamava `-verifica` senza esiti**, quindi non poteva mai trovare test
  rossi. Ora gira un solo `go test -json`, passato a `tools/copertura -risultati … -verifica`;
  `-verifica` senza `-risultati` è un errore. Con `GESTRIP_BANCO_ESITO` si passa anche l'esito
  del banco.
- **Debiti rimasti (bassi):**
  - `commit-msg` con `--cleanup=verbatim` non controlla le righe che iniziano con `#`;
  - in locale C, `-i` non confronta le lettere accentate;
  - il controllo di email e telefoni «non ammessi» del PIANO §4.6 non c'è ancora: arriva con i
    primi dati sintetici (T2 e T3).
