# 021 — Controllo vero del terminale per i link della console

- **Fonte:** BASSA dichiarata nella review della 020: il controllo «solo sul terminale» (D-04)
  accettava ogni device a caratteri, quindi anche `/dev/null`.
- **Voci:** nessuna nuova; rinforza ACC-07, ACC-13 e ACC-16 (`reset-link`, `invito`,
  `primo-titolare`).
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha rilanciato `scripts/check` e i test del terminale.

## Cosa c'è

- `cmd/gestrip/terminale_linux.go`: `terminale(f)` è vero solo se il descrittore risponde a
  `TCGETS` (isatty, `golang.org/x/sys/unix`, ora dipendenza diretta; nessun modulo nuovo). Il
  descrittore si usa con `SyscallConn`, che non lo rimette in modalità bloccante come `Fd`.
- `cmd/gestrip/terminale_altri.go`: fuori da Linux rifiuta sempre. L'apparecchio è Linux e un
  controllo mai provato non decide dove finisce un link segreto.
- Messaggio di rifiuto, uscita 1 e posizione del controllo (prima di aprire il database) non
  cambiano.
- **Prima della correzione, con stdout su `/dev/null`, i tre comandi creavano davvero il codice e
  la riga d'audit e buttavano il link** (misurato dai test rossi).

## Prove

- **TDD:** rossi 2 test su 3 (4 casi con i sottotest): `/dev/null` in
  `TestResetLinkSoloSulTerminale` e i tre comandi con stdout su `/dev/null`.
- **Test** (`terminale_linux_test.go`, helper `figlioCon` in `figlio_linux_test.go`):
  - pseudo-terminale vero (`/dev/ptmx`) accettato; `/dev/null`, file, pipe e descrittore
    inutilizzabile rifiutati;
  - per ognuno dei tre comandi, in un processo figlio: stdout su `/dev/null` → uscita 1 e database
    invariato; stdin e stderr sul terminale ma stdout su `/dev/null` → rifiuto (il controllo è su
    stdout); stdout sul terminale → uscita 0, link letto dal terminale, riga creata.
  - `-count=30` stabile.
- **Mutazioni:** 16 su 16 uccise, tutte compilano (tra cui stdin o stderr al posto di stdout in
  ognuno dei tre rami, controllo saltato, vecchio controllo sul device, errori ignorati).
- `scripts/check` verde (rilanciato dalla sessione principale); vet e build di `./cmd/...` e
  `./internal/...` per windows e darwin verdi.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** i test non fissavano su quale descrittore si fa il controllo (stdin o stderr al posto di
  stdout passavano): aggiunti i due casi con il pseudo-terminale.
- **BASSA:** `Fd()` rimetteva il descrittore in modalità bloccante; i test del pseudo-terminale ora
  si saltano, invece di fallire, dove devpts è incompleto.

Dichiarati:
- il messaggio di rifiuto è ripetuto nei tre rami di `main()`, come gli altri blocchi del file;
- **limite di isatty:** un terminale «avvolto» passa (`script … | tee`, registrazione della
  sessione ssh, `tmux pipe-pane`): il controllo vede solo il descrittore del processo. Il revisore
  l'ha misurato con `script -qec … | sed`. È un limite noto, non un bug: da ricordare nella guida
  dell'apparecchio;
- `GOOS=windows go vet ./...` fallisce in `tools/banco/istanza.go` (`syscall.Statfs_t`): c'era già
  prima della slice ed è fuori dal binario.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `b38e6e9`.
- Copertura invariata (`scripts/check`: chiuse 12, parziali 17, rosse 0).
