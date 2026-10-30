# Sviluppo

## Strumenti a versione fissata

| Strumento | Versione | Verifica |
|---|---|---|
| Go | 1.27.3 (linux/amd64), in `~/sdk/go1.27.3` | sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40` da go.dev/dl |
| gitleaks | 8.30.1 (linux_x64), in `~/.local/bin/gitleaks-8.30.1` | sha256 `551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb` dal file checksums della release |
| staticcheck | 2026.2.2 (`honnef.co/go/tools` v0.8.2) | dichiarato come `tool` in `go.mod`: `go tool staticcheck` |
| errcheck | v1.30.0 (`github.com/kisielk/errcheck`) | dichiarato come `tool` in `go.mod` (hash in `go.sum`): `go tool errcheck` |

Il binario si compila senza CGO: `CGO_ENABLED=0 go build ./...`.

## Dopo il clone

    git config core.hooksPath scripts/githooks

Serve anche l'elenco delle parole vietate, **fuori dal repo**:
`~/.config/gestrip-go/parole-vietate.txt` (permessi 0600). Una parola o frase per riga, senza
maiuscole: nomi veri di negozi, clienti, persone, indirizzi email o percorsi privati. Senza
l'elenco i commit si fermano. Si può indicare un altro percorso con `GESTRIP_PAROLE_VIETATE`.

Per il banco serve anche `~/.config/gestrip-go/percorsi-produzione.txt` (0600): i percorsi
dell'installazione di produzione sulla macchina, uno per riga, che `tools/banco` non tocca mai
(`/opt/gestrip` è già compreso). Sta fuori dal repo perché contiene il nome del negozio
(`GESTRIP_PERCORSI_PRODUZIONE` per un altro percorso).

## Controlli

- **Ogni commit** (`scripts/githooks/pre-commit` e `commit-msg`): gitleaks sulle modifiche in
  staging, parole vietate nelle righe aggiunte e nel messaggio.
- **Ogni slice** (`scripts/check`, l'equivalente di `mix precommit`): gofmt, vet, staticcheck,
  errcheck, build senza CGO, test con la sola CPU, copertura dell'inventario, gitleaks e parole vietate su
  tutto il repo e la sua history.
- Tutto passa da `scripts/limiti.sh` (`systemd-run --user --scope`, CPU 800%, RAM 16G,
  `nice 19`), perché pc-fisso fa girare il dettato del negozio. I limiti si cambiano con
  `GESTRIP_CPU_QUOTA` e `GESTRIP_MEMORY_MAX`.

## Copertura dell'inventario

Ogni test che chiude una voce dell'inventario porta il segno nel commento di documentazione:

```go
// inventario: RIP-S-48, RIP-S-47
// Ristampa 12–12 a contatore 300: il contatore resta 300.
func TestEtichetteRistampaNonAbbassaIlContatore(t *testing.T) { … }
```

Il segno vale solo sopra un `func TestXxx`; altrove è un errore. Comandi:

    go run ./tools/copertura                       # riepilogo
    go run ./tools/copertura -tabella              # una riga per voce
    go test -json ./... > /tmp/r.json && go run ./tools/copertura -risultati /tmp/r.json -verifica
    go run ./tools/copertura -risultati /tmp/r.json -rilascio   # blocca se una voce v1 non è chiusa

L'inventario completo si legge da `~/Condivisi/GestRip-Go/inventario-funzioni.md` (o
`GESTRIP_INVENTARIO`, o `-inventario`) ed è privato. Nel repo c'è solo `docs/inventario-elenco.tsv`
(ID, titolo, destino), che la CI legge al posto dell'inventario (PIANO §8.3, A2). Dove l'inventario
c'è, la copertura controlla che l'elenco sia allineato; dopo una modifica all'inventario:

    go run ./tools/copertura -scrivi-elenco

Lo scrive solo se nessun titolo contiene una parola di `~/.config/gestrip-go/parole-vietate.txt`
(o `GESTRIP_PAROLE_VIETATE`); senza quell'elenco rifiuta.

## Errori ignorati

`errcheck` blocca ogni errore non guardato, anche nei test. Un errore che non serve si scarta con
`_ =` esplicito e una parola sul perché, così si vede in review. Le uniche eccezioni globali sono
in `scripts/errcheck-escluse.txt`, ognuna motivata (`Rows.Close`, `Tx.Rollback` in defer).

## Rotte

- Le rotte si registrano nel mux della faccia (`internal/web`) e passano da `faccia`, che legge il
  percorso come Phoenix e risponde il 404 dello standalone a tutto il resto.
- **Niente pattern a sottoalbero** (che finiscono con `/`, come `GET /r/`): darebbero redirect;
  `faccia` li tratta come 404. Si usa `GET /r/{token}`.
- I gestori leggono i segmenti con `r.PathValue(...)`, **mai** `r.URL.Path`: dopo la riscrittura
  contiene i `%2F` decodificati.
- Sulla faccia interna ogni rotta si registra con `f.rotta(mux, "METODO /percorso", permesso, h)`
  (`internal/web/ruoli.go`): `aperta`, `personale` o `soloTitolare`. La catena (pagina, sessione,
  CSRF per i POST, accesso, ruolo) la mette `rotta`, nell'ordine dello standalone; `f.speciale`
  solo per le rotte con una catena loro (`/healthz`, `POST /login`).
- Ogni rotta nuova va nella **tabella dei permessi** (`tabellaPermessi` in
  `internal/web/ruoli_test.go`) con il permesso dello standalone, altrimenti il test fallisce.
  Un'azione «solo titolare» dentro una pagina comune (EME-12/13/14, CLI-24/25, vendite, cestino)
  ha già la sua riga con il messaggio dello standalone: la slice che la porta scrive la rotta Go e
  un test con il POST forgiato dal dipendente.
- Sulla faccia pubblica una rotta rifiuta **solo** con `RifiutoPubblico(w, r, testo, robots)`
  (`internal/web/limiti.go`), con il testo della sua famiglia: così il 404 di un indirizzo
  bloccato è identico a quello normale (D-05). Le famiglie che contano i 404 sono in
  `famigliaDi`, copiate da `PublicAttempts.family/1`; le foto non contano.

## Moduli

- Una rotta che riceve un modulo è `POST` e passa da `csrf.Modulo` (`internal/web/modulo.go`),
  con il `*web.CSRF` della sua faccia; la pagina mette `csrf.Token(w, r)` nel campo nascosto
  `_csrf_token` e nel meta `csrf-token`, **prima** di scrivere la risposta.
- Ordine con le intestazioni delle pagine: `csrf.Modulo(web.Pagina(h))`: il 403 dello
  standalone non le ha.
- Il modulo ha sempre un `action` esplicito; i campi si leggono da `r.PostForm` (la query non
  conta).
- Solo `application/x-www-form-urlencoded`. Una pagina con foto (multipart) manda il token
  nell'intestazione `x-csrf-token`, non nel corpo.

## Regole del repository pubblico

- Nessun segreto, chiave o token, nemmeno di prova se sembra vero.
- Nessun dato reale: clienti, buste, foto, dettati, nomi di persone o negozi.
  Nei test e negli esempi solo dati inventati.
- Nei messaggi di commit nessun riferimento a dati veri.
- Autore dei commit: `Stefano Zamprogno <90012+L0cutus@users.noreply.github.com>`.
