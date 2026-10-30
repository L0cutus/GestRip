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
- Sulla faccia pubblica ogni rotta si registra con `f.rottaPubblica(mux, "METODO /percorso", h)`
  (intestazioni delle pagine e, per i POST, il CSRF della faccia pubblica, per costruzione), o con
  `f.speciale` se sta fuori dal pipeline `:browser` dello standalone (`/healthz`). Mai
  `mux.Handle` diretto, su nessuna delle due facce (`TestNessunaRottaFuoriDallaTabella`): i test di
  CSRF, intestazioni e registro iterano sull'elenco `f.rotte`.
- Ogni rotta nuova va nella **tabella dei permessi** (`tabellaPermessi` in
  `internal/web/ruoli_test.go`) con il permesso dello standalone, altrimenti il test fallisce.
  Un'azione «solo titolare» dentro una pagina comune (EME-12/13/14, CLI-24/25, vendite, cestino)
  ha già la sua riga con il messaggio dello standalone: la slice che la porta scrive la rotta Go e
  un test con il POST forgiato dal dipendente.
- **Guscio delle pagine** (`internal/web/guscio.go`, slice 015). La superficie la decide la rotta,
  in modo esplicito come `:staff_shell` dello standalone: il guscio interno (menu ☰, tavolozze,
  «🏠 Lista», manifest e worker) solo per le rotte registrate con
  `f.rottaConGuscio(mux, pattern, permesso, h)`, che non accetta `aperta`. Va usata per ogni rotta
  che disegna una pagina interna, anche per un POST che la ridisegna (l'elenco è fissato da
  `TestRotteConGuscio`). Le rotte aperte di login e reset (`rotteIngresso`) hanno solo manifest e
  worker. Ogni altra rotta è pubblica (niente manifest, niente tavolozza), anche se è del
  personale: è il caso dell'anteprima del cliente. Il menu e le schede delle Impostazioni
  sono tabelle con tutte le voci dello standalone (`vociMenu`, `schedeImpostazioni`): una voce
  compare da sola quando la sua rotta `GET` è registrata. Una pagina nuova delle Impostazioni
  mette `{{template "schede" .}}` dopo `<main class="gs-page">`, chiude con `</section>` e si
  aggiunge a `schedaDelModello`.
- Foglio, script e icone stanno in `internal/web/statici/`, incorporati nel binario. Le pagine li
  collegano con `{{asset "app.css"}}`: l'indirizzo ha un pezzo dell'hash del contenuto ed è in
  cache un anno, e si serve solo così com'è scritto (niente barre in più né caratteri codificati).
  Il worker mette in cache solo `/assets/…` senza query e la lista del precache: nessuna rotta
  nuova deve ricadere in quella regola (`TestNessunaRottaNellaCacheDelWorker`).
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

## Registro e panic (ACC-19)

- Nel registro mai l'indirizzo della richiesta: né `r.URL` e i suoi campi, né `RequestURI`,
  `PathValue`, `Referer`, né la richiesta intera, nemmeno in un ramo d'errore. La rotta la scrive
  `faccia`, come pattern del mux. Lo controlla `TestNessunPercorsoNeiRegistriDelCodice`, che legge i
  sorgenti di `internal/web` e `cmd/gestrip`.
- Un panic in un gestore finisce nel registro con il tipo del valore e lo stack, mai il valore.
  Nello stack gli argomenti compaiono come parole in esadecimale: di una stringa o di una slice
  solo il puntatore e la lunghezza, ma un intero o un array passato per valore si legge. **Mai
  passare un segreto per valore come intero o array** (per esempio `[32]byte`) a una funzione che
  può andare in panic: va passato come slice o stringa.

## Opzioni di `gestrip serve` solo per sviluppo e banco

Sull'apparecchio `serve` rifiuta di partire se i dati non stanno su un volume cifrato LUKS (OPS-12)
o se una faccia (`-interna`, `-pubblica`) non ascolta su loopback. I messaggi di rifiuto **non
nominano** le opzioni che spengono i controlli (decisione di Stefano, 30/10/2026): sono scritte
solo qui. Chi le usa riceve un avviso «ATTENZIONE … mai sull'apparecchio» a ogni avvio.

- `-senza-volume-cifrato`: salta il controllo del volume cifrato. Per lo sviluppo su un disco in
  chiaro, per il banco (`tools/banco/istanza.go`) e per gli import di prova in tmpfs.
- `-ascolto-non-locale`: lascia le facce ascoltare fuori da loopback. Serve al banco, dove Caddy
  raggiunge il Go da un altro container (`0.0.0.0:47101`, `0.0.0.0:47102`). Senza l'opzione si
  accettano solo un IP scritto in 127.0.0.0/8 o `::1`, oppure `localhost` (nessun nome si
  risolve; dopo l'apertura `serve` controlla anche l'indirizzo vero). La sola porta (`:47101`)
  vuol dire tutte le interfacce, quindi è rifiutata.

Le due opzioni compaiono in `gestrip serve -h` e negli errori delle opzioni (li scrive il pacchetto
`flag`), non nella riga d'uso. È voluto: il repository è pubblico, quindi nasconderle non
servirebbe; conta che i messaggi di rifiuto non le nominino.

Uno stop (SIGTERM, SIGINT) che arriva mentre `serve` si avvia, prima di «in ascolto», chiude
pulito: riga `gestrip: fermato durante l'avvio` ed esce con 0. È uno stop solo se l'errore viene
**soltanto** dal contesto: lo stop insieme a un errore vero (per esempio una copia di backup
«da togliere a mano») resta un'uscita 1 con l'errore stampato, e così ogni altro errore dell'avvio.
Nel percorso d'avvio gli errori che possono venire dal contesto si avvolgono con `%w`, mai `%v`,
altrimenti lo stop diventa un errore. I test fermano l'avvio a metà con `inAvvio` (in processo) o
con `GESTRIP_TEST_AVVIO_FERMO=1` (processo figlio, `cmd/gestrip/figlio_linux_test.go`).

Un SIGTERM che arriva nei primissimi istanti (circa i primi 70 ms, mentre si inizializzano i
pacchetti, prima di `signal.NotifyContext`) non passa da `serve`: il processo muore per il segnale
(stato 143, nessuna riga). Per systemd, che lo ha mandato, è comunque una fine pulita.

## Regole del repository pubblico

- Nessun segreto, chiave o token, nemmeno di prova se sembra vero.
- Nessun dato reale: clienti, buste, foto, dettati, nomi di persone o negozi.
  Nei test e negli esempi solo dati inventati.
- Nei messaggi di commit nessun riferimento a dati veri.
- Autore dei commit: `Stefano Zamprogno <90012+L0cutus@users.noreply.github.com>`.
