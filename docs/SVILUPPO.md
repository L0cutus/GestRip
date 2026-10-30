# Sviluppo

## Strumenti a versione fissata

| Strumento | Versione | Verifica |
|---|---|---|
| Go | 1.27.3 (linux/amd64) | sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40` da go.dev/dl |
| gitleaks | 8.30.1 (linux_x64) | sha256 `551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb` dal file checksums della release |

Il binario si compila senza CGO: `CGO_ENABLED=0 go build ./...`.

## Hook dei segreti

Dopo il clone:

    git config core.hooksPath scripts/githooks

Ogni commit passa da `gitleaks git --pre-commit --staged`. Se gitleaks manca, il
commit si ferma.

## Regole del repository pubblico

- Nessun segreto, chiave o token, nemmeno di prova se sembra vero.
- Nessun dato reale: clienti, buste, foto, dettati, nomi di persone o negozi.
  Nei test e negli esempi solo dati inventati.
- Nei messaggi di commit nessun riferimento a dati veri.
