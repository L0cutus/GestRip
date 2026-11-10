# Sviluppo

## Strumenti a versione fissata

| Strumento | Versione | Verifica |
|---|---|---|
| Go | 1.27.3 (linux/amd64), in `~/sdk/go1.27.3` | sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40` da go.dev/dl |
| gitleaks | 8.30.1 (linux_x64), in `~/.local/bin/gitleaks-8.30.1` | sha256 `551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb` dal file checksums della release |
| staticcheck | 2026.2.2 (`honnef.co/go/tools` v0.8.2) | dichiarato come `tool` in `go.mod`: `go tool staticcheck` |
| errcheck | v1.30.0 (`github.com/kisielk/errcheck`) | dichiarato come `tool` in `go.mod` (hash in `go.sum`): `go tool errcheck` |
| Playwright (livello browser del banco) | 1.58.2, con il Chromium della base | immagine `grgo-banco-browser:1.58.2` da `tools/banco/browser/Dockerfile`, sotto |
| jsQR (nel binario, lettore QR della fotocamera) | 1.4.0, Apache-2.0 | `internal/web/statici/jsqr.min.js`, sha256 `bc40c8a15196236b2314db0856f72ca0b49980cd5413b8c852a7349f5fee0859`: `dist/jsQR.js` di `jsqr@1.4.0` (https://github.com/cozmo/jsQR), lo stesso file dello standalone a `go-baseline` (`assets/vendor/jsqr.min.js`); licenza e origine in `internal/web/statici/LICENSE-jsqr.txt`; `TestJsQRNelBinario` fissa l'impronta |

**Immagine del livello browser del banco** (slice 040a). Base
`mcr.microsoft.com/playwright:v1.58.2-noble@sha256:65cefd09a5e943921ecd3a6e5414c603db2eb161e9eb48f2e2ccc63486dc7dc0`
(linux/amd64, Ubuntu 24.04, Node 24, Chromium di Playwright 1.58.2: la stessa dello standalone,
`browser/Dockerfile` a `go-baseline`). Il pacchetto npm `playwright` 1.58.2 (con `playwright-core`
1.58.2) si installa con `npm ci --ignore-scripts` dal `package-lock.json`, con le impronte
`sha512` del registro; nessun browser scaricato. `libnss3-tools` (per `certutil`) è fissato a
`2:3.98-1build1`, la versione del pocket della release di noble, che non sparisce dall'archivio ed è
la stessa di `libnss3` nella base (senza la versione apt prenderebbe `-updates` e aggiornerebbe
anche `libnss3`). Si costruisce da pc-fisso, nel repo, scaricando prima la base per digest:

    sg docker -c "docker pull mcr.microsoft.com/playwright:v1.58.2-noble@sha256:65cefd09a5e943921ecd3a6e5414c603db2eb161e9eb48f2e2ccc63486dc7dc0"
    sg docker -c "<banco> immagine-browser"      # docker build --pull=false con l'etichetta dell'impronta

`banco immagine-browser` mette sull'immagine l'etichetta `grgo.banco.impronta`, lo SHA-256 di
`Dockerfile`, `package.json`, `package-lock.json` ed `entrypoint.sh`; `banco esegui` la confronta
con i file di adesso e, se l'immagine manca o è stata costruita da altri file, si ferma e dice di
ricostruirla. Il container parte con `--pull never`. L'immagine contiene solo le dipendenze e
`entrypoint.sh` (CA dell'istanza nel database NSS di Chromium, `DEBUG` e affini tolti: l'uscita di
debug di Playwright contiene indirizzi e valori dei moduli; `node --check` dello script prima del
giro). Lo script degli scenari, `tools/banco/browser/banco.mjs`, si monta in sola lettura a ogni
giro, quindi cambiarlo non vuole una nuova immagine.

Il binario si compila senza CGO e con il tag `nodynamic`:
`CGO_ENABLED=0 go build -tags nodynamic ./...` (anche `go test`, `go vet` e il banco; basta
`GOFLAGS=-tags=nodynamic`). Senza il tag `gen2brain/webp` (miniature WebP, tema H) porta il
caricamento di libwebp di sistema e il binario diventa dinamico: `internal/imaging` allora non
compila (`serveIlTagNodynamic`), apposta.

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
    go test -tags nodynamic -json ./... > /tmp/r.json && go run ./tools/copertura -risultati /tmp/r.json -verifica
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
  un test con il POST forgiato dal dipendente. La rotta si registra con
  `f.azioneDelTitolare(mux, "POST /…", rifiuto, torna, h)` (slice 035): il dipendente torna alla
  pagina (`torna`) con il messaggio della riga, non a `/` con «Operazione riservata al titolare.»
  (`TestAzioniDelTitolareConIlLoroRifiuto`).
- **Scheda della busta: aperture e mostre** (slice 037b, D-44 come applicata). Ogni risposta che
  mostra i dati della scheda scrive la riga della propria azione o `repair.viewed`, a meno che la
  richiesta porti un segno monouso valido del server (`internal/web/segni.go`, legato a utente,
  nonce e busta). Regole:
  - la GET legge con `f.mostraOApri` (segno nel parametro `mostra`); ogni disegno della scheda
    passa da `f.disegnaScheda`, che mette il segno della pagina nei link «Modifica» e «Annulla» e,
    come campo nascosto `mostra`, in **ogni modulo** della scheda;
  - ogni azione della scheda chiama una volta `f.segnoDelModulo(r, id)` e consuma il segno della
    pagina;
  - un'azione riuscita, che ha scritto la sua riga, torna con `f.allaScheda(r, id, esito)`;
  - un rifiuto, un'azione che non cambia niente o un guasto (nessuna riga sua) torna con
    `f.dopoRifiuto(r, id, conSegno)` e ridisegna leggendo con `f.leggiDopoRifiuto`: senza il segno
    della pagina la scheda mostrata è un'apertura vera, con la riga e il segno «letto»;
  - un'azione del titolare della scheda torna con `f.tornaAllaScheda`;
  - la catena dei segni dura al più un'ora (`durataCatena`) dall'ultima riga scritta da quella
    sessione per quella busta (apertura vera con `f.apri`, o azione riuscita con `f.allaScheda`): un
    segno emesso da una pagina mostrata senza riga ne eredita l'inizio. Al più 64 segni per
    sessione, nessuno per una HEAD;
  - mai `"/repairs/<id>"` scritto a mano in un gestore della scheda. La lettura senza audit
    `buste.Scheda` ha i chiamanti fissati da `TestLetturaSenzaAuditSoloDaiChiamantiNoti`: un
    chiamante nuovo si aggiunge lì, con il perché nel commento di `buste.Scheda`.
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
- **Link storici della vecchia app Flask (slice 064):** sulla faccia pubblica `GET /stato/{busta}/{anno}`,
  `GET /stato/{busta}/{anno}/poll`, `POST /stato/{busta}/{anno}/messaggio`, `GET /foto/{slug}/{file}` e
  `GET /thumb/{slug}/{file}` (`internal/web/pagina_storica.go`, regole in `internal/buste/storica.go`).
  La credenziale è `pw`, oppure `t` ed `exp`, lette come `param/2` (solo un testo; nel poll dalla query,
  nel messaggio dai campi del modulo: la query del POST non conta) e rimandate nell'ordine `pw`, `t`,
  `exp` nel poll, nei campi nascosti del modulo e nel redirect (`legacy_credential/1`). Gli indirizzi
  si scrivono come Phoenix (`segmentoPhoenix`: tutto tranne lettere, cifre e `-_.~` diventa `%XX`) e
  Plug (`queryPlug`: le coppie nell'ordine dato, `URI.encode_www_form/1`), misurati nell'immagine
  dello standalone. Le email-optin/optout e telegram-optin/optout del link storico arrivano con la
  T4.6/T4.7. La pagina riusa `pagina_cliente.html`: gli indirizzi delle foto, del poll e del modulo li
  prepara il gestore (`FotoPagina[i].Src`, `IndirizzoPoll`, `IndirizzoMessaggio`, `Credenziale`).
- **Portale ritirato e informativa (slice 065):** sulla faccia pubblica `GET /m/{codice}` e
  `GET /m/{codice}/{resto...}` (410 «Portale non più attivo», `rottaPubblica`), `GET /m/{codice}/sw.js`
  (`f.speciale`: fuori dal pipeline delle pagine, niente sessione, `text/javascript`, `no-cache`) e
  `GET /privacy` (`internal/web/portale_privacy.go`). Nessuna conta nel limite (`famigliaDi` non le ha);
  `{codice}` e `{resto}` non si leggono mai. Il QR dell'informativa è `qrSVG` sull'indirizzo pubblico
  configurato (mai l'`Host`); senza `indirizzo_pubblico` non c'è. Il profilo del negozio si legge con
  `buste.LeggiProfiloNegozio`. Una pagina pubblica i cui dati hanno `titoloPagina()` ha il titolo
  «… · Laboratorio» anche fuori dal guscio (`mostraPubblica`).
- **Anteprima del cliente (slice 065):** sulla faccia interna `GET /repairs/{id}/anteprima-cliente`
  (`f.rotta`, `personale`, senza guscio: la pagina è del cliente), `internal/web/anteprima_cliente.go`
  e `buste.AnteprimaCliente` (`paginaDove` per id, fuori dal cestino, più la riga
  `repair.customer_preview_viewed`). Disegna `pagina_cliente.html` con `Anteprima`: barra
  `#customer-preview-banner`, `fieldset` spento, nessun modulo con `action` né token CSRF, niente
  `data-poll-url`, foto dalla rotta del personale (`/repairs/<id>/photos/<foto>`, che scrive la sua riga),
  🏠 e ← di `public_fabs/1`, l'informativa sull'indirizzo pubblico (la faccia interna non ha
  `/privacy`). Il messaggio del personale nella sessione si toglie (`clear_flash/1`).
- Sulla faccia pubblica una rotta rifiuta **solo** con `RifiutoPubblico(w, r, testo, robots)`
  (`internal/web/limiti.go`), con il testo della sua famiglia: così il 404 di un indirizzo
  bloccato è identico a quello normale (D-05). Le famiglie che contano i 404 sono in
  `famigliaDi`, copiate da `PublicAttempts.family/1`; le foto non contano.

## Segreto Flask (slice 064)

- **Dove:** `secrets/segreto_flask` nella cartella del database (PIANO §1.9): con `-db
  /var/lib/gestrip/gestrip.db` è `/var/lib/gestrip/secrets/segreto_flask`. Lo legge `gestrip serve`
  all'avvio, prima di aprire il database (`firmeflask.Leggi`, `percorsoSegretoFlask` in
  `cmd/gestrip/main.go`).
- **Regole:** quelle di `segreti.LeggiFile` (file regolare, non un link, 0600 o 0400, al più 4096 byte,
  UTF-8), poi senza gli spazi ai bordi come `String.trim/1` di `config/runtime.exs` dello standalone,
  e almeno 16 byte. **File assente** = nessun segreto: ogni firma della vecchia app (`/stato?t=`,
  `/foto`, `/thumb`) è rifiutata, i codici brevi `?pw=` funzionano lo stesso (come lo standalone senza
  `LEGACY_WARRANTY_SECRET_FILE`). **File presente ma sbagliato** = l'avvio si ferma con «segreto Flask:
  …», un errore che non riporta il contenuto.
- **Nel codice:** `internal/firmeflask` (riusabile da `/artigiano`, `/g/` e foto delle vendite nelle
  tappe dopo: `firma`/`verifica` con le parti della famiglia). Il segreto sta in `firmeflask.Segreto`,
  che non si stampa: `String`, `Format` (ogni verbo), `LogValue` e `MarshalText` danno «[segreto
  Flask]», e i byte stanno dentro una funzione, così anche un `fmt.Printf("%+v")` di una struttura che
  lo contiene non li mostra. Mai nei log, mai nell'audit. Le buste lo ricevono con `ConSegretoFlask`.
- **Firme:** HMAC-SHA256 esadecimale minuscolo troncato a 32 (16 per l'artigiano), messaggio = parti
  unite da «:» più la scadenza riscritta senza zeri davanti; scadenza solo cifre ASCII, valida se
  `exp ≥ adesso` (un numero oltre int64 è sempre dopo); confronto a tempo costante. Vettori sintetici
  dello standalone in `internal/firmeflask/firmeflask_test.go`.
- **Installer e cutover (T5, da fare):** l'installer crea `secrets/` (0700) e il cutover copia il file
  del segreto dello standalone (quello di `LEGACY_WARRANTY_SECRET_FILE`) in `secrets/segreto_flask`,
  0600, proprietario l'utente del servizio; poi controlla che `gestrip serve` parta. Il file va nel
  backup cifrato (PIANO §2.4). Prima del cutover va controllato che il segreto vero abbia almeno 16
  byte (lo standalone ne vuole solo uno): altrimenti serve parte solo senza il file.

## Moduli

- Una rotta che riceve un modulo è `POST` e passa da `csrf.Modulo` (`internal/web/modulo.go`),
  con il `*web.CSRF` della sua faccia; la pagina mette `csrf.Token(w, r)` nel campo nascosto
  `_csrf_token` e nel meta `csrf-token`, **prima** di scrivere la risposta.
- Ordine con le intestazioni delle pagine: `csrf.Modulo(web.Pagina(h))`: il 403 dello
  standalone non le ha.
- Il modulo ha sempre un `action` esplicito; i campi si leggono da `r.PostForm` (la query non
  conta).
- Solo `application/x-www-form-urlencoded`, tranne i moduli con dei file (slice 038): la rotta si
  registra con `f.rottaConFile(mux, "POST /…", permesso, limite, h)`, che usa
  `csrf.ModuloConFile`. Il modulo `multipart/form-data` mette `_csrf_token` (e `mostra`) **prima**
  dei campi file; il server legge solo i campi di testa, controlla il token (il campo se c'è,
  altrimenti `x-csrf-token`, come Plug) e solo dopo il gestore riceve i file con
  `fileDelModulo(r)`. Un `_csrf_token` dopo un file non conta. Prima del token il corpo si legge al
  più fino a `LimiteModulo` (intestazioni delle parti comprese); dopo, fino a `limite`, e il
  `Content-Length` dichiarato non decide niente: decide il gestore leggendo. Dopo token e sessione
  il gestore sposta la scadenza di lettura a ogni pezzo di 64 000 byte (chunk_timeout di LiveView),
  perché il `ReadTimeout` del server vale per tutto il corpo. I file in arrivo vanno nella cartella
  dei caricamenti (`Dipendenze.Caricamenti`, accanto al database, mai `/tmp`, 0700 controllata
  all'avvio), in una cartella della richiesta 0700, a 0600, e si tolgono sempre. Nessun gestore usa
  `ParseMultipartForm`, `FormValue`, `FormFile` o `MultipartReader`
  (`TestNessunaLetturaDelModuloFuoriDalCSRF`). Il nome del file dal browser passa da
  `buste.PulisciNomeFile` (`clean_name/1`).
- Un POST il cui corpo **non è un modulo** (la coda delle foto, `POST /repairs/{id}/photos`, slice
  039: il corpo è la foto) si registra con `f.rotta` come gli altri: `csrf.Modulo` non legge un
  corpo che non è `application/x-www-form-urlencoded` e prende il token da `x-csrf-token`. Il
  gestore legge `r.Body` solo dopo token, sessione e permesso, e dopo i suoi controlli che non
  leggono (busta, tipo, lunghezza dichiarata), con `lettoreLimitato` (limite contato leggendo) e
  `allungaLettura` per la scadenza per pezzo; il file va nei caricamenti come sopra.

## Coda delle foto in background (slice 044b, D-58, D-59)

- **La coda vive nel browser, in IndexedDB** (base `gestrip-coda-foto`, magazzini `voci` e `file`:
  i file a parte, così un cambio di stato non riscrive la foto). Ogni foto scelta in «Nuova
  riparazione» è subito una voce, con l'utente della pagina, la scheda del browser (`sessionStorage`)
  e la chiave del modulo. «Genera busta» annota in `sessionStorage` la chiave del modulo mandato e
  aspetta al più 5 s le scritture e le cancellazioni non finite (poi quelle foto restano in memoria);
  l'avviso della busta salvata si annota in `sessionStorage` (sincrono) e poi, in una transazione, le
  voci del modulo mandato passano in attesa (o si tolgono se la chiave dell'avviso è un'altra): rifatto
  non trova più niente, quindi vale una volta. Un modulo con un'altra chiave toglie le foto scelte della
  stessa scheda, ma prima chiede sul canale se una pagina aperta tiene ancora quella chiave (una scheda
  duplicata ha lo stesso `sessionStorage`). Ogni pagina interna (quella con `#photo-queue`) manda le
  voci in attesa dell'utente della pagina, una alla volta; con più schede ne manda una sola (Web Locks
  `gestrip-coda-foto`, o un affitto in `localStorage`), le altre ridisegnano la striscia
  (BroadcastChannel). Quando una foto entra in coda si chiede `navigator.storage.persist()`. Le voci
  in attesa o rifiutate scadono dopo 14 giorni (`QUEUE_MAX_MS`, da confermare con Stefano), le scelte
  mai salvate dopo un giorno. Niente rotte nuove e niente nella cache del worker; mai `innerHTML`.
- **Utente della pagina:** il guscio interno mette `<meta name="gestrip-utente" content="<id>">`
  (`guscio.Utente`). È l'id, non un segreto; il login e le pagine pubbliche non lo hanno. Le voci di
  un altro utente non partono; con «Esci» restano.
- **Identificativo degli invii:** ogni voce ha 16 byte casuali in base64url (22 caratteri) e li
  manda in `x-upload-id` (conta la prima intestazione). `POST /repairs/{id}/photos` usa
  `buste.AllegaFotoConInvio`: un invio riuscito scrive la traccia in `invii_foto` (migrazione 0015)
  nella stessa transazione della foto (se la traccia non si scrive, niente foto: 500); un nuovo invio
  con lo stesso identificativo per la stessa busta risponde 201 con la foto già salvata, senza righe
  né audit. Se quella foto è stata poi tolta, la traccia resta senza foto (`ON DELETE SET NULL`) e la
  risposta è **201 `{"ok":true,"photo_id":null}`**: già fatto, la foto non torna, e la coda la toglie
  come salita. Ricerca e scrittura stanno sotto il lucchetto dell'archivio (un `AllegaFoto` alla
  volta), e la chiave primaria (busta, identificativo) tiene comunque. La traccia se ne va con la busta;
  la tabella è in `TabelleSoloGo` (chiavi esterne in `EsterneSoloGo`) e l'import la lascia vuota. Un
  valore malformato si ignora, come lo standalone ignora un'intestazione che non conosce, con una
  riga nel registro senza il valore.
- **Errori:** 413, 415, 422 sono definitivi come nello standalone (la foto resta nella striscia finché
  la si chiude); gli altri, 404 compreso, si riprovano. Tentativi e momento del prossimo stanno nella
  voce (valgono per ogni pagina e scheda): 5 s, 15 s, 30 s, 1 min, e dopo 5 errori di fila la coda si
  ferma fino a «Riprova». Un invio interrotto perché la pagina se ne va non conta come errore.
- **Nessuna conferma all'uscita**, tranne per le voci rimaste in memoria quando IndexedDB non c'è o
  non scrive: solo per quelle restano `beforeunload` e il giro lista ↔ «Nuova riparazione» senza
  ricaricare della 040b (`attiva()`).
- **D-59:** al più `buste.MaxFotoPerVolta` (4) foto per fonte, «Massimo 4 foto per volta.»; il tetto
  del caricamento dalla scheda è `MaxFotoPerVolta × 25 MiB + 1 MiB` (Fotocamera e Galleria sono due
  moduli con un campo ciascuno).
- **Banco:** scenari browser `sfondo` (4 foto e le pagine che cambiano a metà invio, la ricarica a
  metà invio, due buste di fila, l'altro utente, le scadenze), `schede` (scheda duplicata, due schede
  con una sola che manda) e `tentativi` (503 fisso: 5 tentativi alle loro attese, fermo anche
  cambiando pagina, «Riprova»); le differenze dallo standalone sono deviazioni D-58 e D-59 o campi
  `solo_go`. Il container del browser ha anche le credenziali sintetiche del dipendente.
- **Conto alla rovescia (slice 055d, D-58):** finché i tentativi automatici continuano, la riga rossa
  ha accanto a «Riprova» «nuovo tentativo tra N s…» (`.attesa-coda`, su una riga sua), aggiornato ogni
  secondo; dopo il quinto errore di fila resta solo «Riprova». Nel banco `banco.mjs` scrive «N» al
  posto dei secondi in `salva-2.striscia` (righe D-58 di `bench/deviazioni.json`).
- **«Massimo 4 foto per pulsante»** è il testo di «Nuova riparazione» (frase e errore, slice 055d); la
  scheda resta «Massimo 4 foto per volta.».

## Testi degli errori dei campi

- Un errore di un campo si scrive con il testo che lo standalone mostra **in italiano**: i suoi
  testi scritti a mano, o quelli di Ecto come li traduce il catalogo `it` dello standalone
  (`priv/gettext/it/LC_MESSAGES/errors.po` a `go-baseline`, slice 220), parola per parola e con il
  plurale del conteggio («Troppo lungo (massimo 160 caratteri).»). Mai un msgid inglese («can't be
  blank»), mai un testo inventato. Le costanti stanno nel pacchetto del dominio
  (`buste.MsgObbligatorio`, `buste.MsgNonValido`, …) con il msgid accanto, e un test le fissa
  (`TestTestiDegliErroriDelCatalogo`).

## Numero di busta (slice 037d)

- Ogni numero di busta **scritto a mano o letto da un link** passa da `buste.NormalizzaNumero`
  (`Repair.normalize_bag_number/1` della slice 220) prima di cercare o salvare: «012/26» è «12/26».
  Chi lo legge da un modulo o da un percorso toglie prima gli spazi ai bordi, se lo standalone lo
  fa. Oggi la usano la creazione (numero e contatore) e, dalla 064, `/stato/<busta>/<anno>` con il
  poll e il messaggio (dopo il numero com'è scritto, vedi sotto); dovranno usarla, quando arrivano, la
  ricerca della home (`RepairIndex`, anche dentro il `LIKE`), la consegna all'artigiano e il lotto
  (numero scritto) e l'esclusione dell'assistente. Una busta nuova vuole un numero che, senza zeri, comincia con una cifra diversa da
  zero.
- **Non** si normalizza: i link firmati dal server sul numero com'è scritto (`/artigiano/<busta>/<anno>`,
  foto storiche `/foto` e `/thumb`). **L'import sì, una volta (D-77, slice 057):** un numero con zeri
  davanti si riscrive senza (`internal/importer/riscritture.go`), tranne quando il numero senza zeri
  è già di un'altra busta o lo darebbero due buste importate (avviso `buste/numero_senza_zeri_occupato`,
  scelta provvisoria). **Per chi porta `/stato/<busta>/<anno>`, `/artigiano/…` e le foto storiche
  `/foto/<slug>/…` e `/thumb/<slug>/…` (slug «0046_26», `get_legacy_public_repair_by_slug/1`):** la
  firma Flask (`verify_status`, `verify_artisan`, `verify_repair_photo` sullo slug) e l'impronta del
  codice (`repair_code_digest`) sono sul numero com'era nello standalone («0046/26»); dopo la D-77 la
  busta ha «46/26». La rotta trova la busta con `NormalizzaNumero` e deve verificare la firma e il
  codice con il numero **scritto nell'indirizzo**, non con quello salvato (vedi i Dubbi della 057).
  **Fatto nella 064 (`internal/buste/storica.go`):** la busta si cerca prima con il numero (o lo slug)
  **com'è scritto**, poi senza gli zeri; una busta nel cestino trovata così ferma la ricerca. Prima
  com'è: una busta rimasta «0046/26» all'import (numero senza zeri già occupato) risponde al suo link,
  mai a quello dell'altra. Firma e codice si verificano sul numero scritto **e** su quello salvato (lo
  standalone verifica su quello salvato, quindi «/stato/046/26» di una busta «46/26» si apre come lì);
  le foto della pagina si firmano con lo slug del numero scritto.
- **Ricerca per numero d'etichetta (D-64, slice 052):** un testo cercato di 1-4 cifre, con o senza
  «/AA», è un numero d'etichetta (`buste.LeggiRicercaEtichetta`): trova le buste che **mostrano** quel
  numero (`RicercaEtichetta.Trova`, la stessa regola di `NumeroVisibile`: l'etichetta, o il numero
  interno solo per le buste senza etichetta), di tutti gli anni senza «/AA», e toglie ogni altro filtro
  (`Filtri.Effettivi`). Chi cerca o seleziona buste dai filtri della home passa da `Lista` o
  `Selezione`, che lo fanno già. Dalla slice 055d (Stefano 06/11/2026, risposta b alla 052) nella
  lista di una ricerca per numero una busta trovata **per il numero interno** porta sempre «⚠ senza
  QR», anche chiusa o con un codice di altro formato, con il titolo «Busta senza etichetta: trovata con
  il numero interno» (`rigaDellaLista`, `NumeroInterno`); lo «storico di questa etichetta» della scheda
  è la stessa lista. Il suggerimento sotto il campo non dice «telefono» quando il testo è un numero
  d'etichetta (`campoRicerca.Suggerimento`, e `app.js` mentre si scrive): D-63.
- **Numero visibile (D-64, slice 050):** ogni pagina, PDF o testo che **mostra** il numero di una
  busta lo prende da `buste.NumeroVisibile(codiceQR, numero)`: «0045/26» con un'etichetta
  `RIP-AAAA-NNNN` (esattamente la forma di `CodiceEtichetta` dopo `NormalizzaCodice`, vedi sotto), altrimenti
  «N. 45/26». Il numero interno resta il dato di registro, link, ordine e ricerca tecnica. Oggi la
  usano la lista della home, la scansione (052), la scheda e «Busta 0045/26 salvata» (053), i PDF e
  la pagina della firma (058); i testi per il cliente la useranno nelle loro slice (T4.5–T4.8). Il
  «#» va davanti solo nei titoli (della scheda, «Busta #0045/26»; della pagina della firma, «Firma
  busta #0045/26» e «✍️ Consegna busta #0045/26»), nella lista («#0045/26») e nei PDF dove lo
  standalone scrive «#N/AA» (il numero grande dell'etichetta 76×127 e di ogni scheda del report), e
  solo con un'etichetta: mai davanti a «N. 45/26», e non nei messaggi («Busta 0045/26 salvata»),
  negli avvisi, nei campi («0045/26»), sulla ricevuta («Busta 0045/26») e sotto «N° BUSTA» della
  scheda PDF («0045/26»), che nello standalone non l'hanno.
- **Numero visibile nei PDF e nella firma (D-64, slice 058):** i dati dei documenti
  (`DatiRicevuta`, `DatiEtichetta`, `DatiSchedaPDF`, `SchedaReport`, `PaginaFirma`) portano
  `Visibile` (`buste.Mostrato`, da `NumeroMostrato` sul codice salvato, letto nella stessa query);
  `Mostrato.Titolo()` mette il «#» solo davanti a un'etichetta. `Numero` resta il numero interno: nome
  del file (`ricevuta-45-26.pdf`, `etichetta_busta_45_26.pdf`, `busta_45_26.pdf`), titolo del
  lavoro di stampa CUPS e `bag_number` delle righe d'audit (`repair.receipt_generated`,
  `label_generated`, `label_printed`, `sheet_generated`, `report_generated`), come lo standalone. Il
  QR dell'etichetta 76×127 non cambia (`ValoreQR`: il codice salvato com'è, o il numero interno senza
  codice). Le etichette QR 57×32 scrivono già «0045/26» dalla 045 (è il numero dell'etichetta, non
  di una busta). Sull'etichetta 76×127 «N. 1000/26» e oltre non sta nei 46 mm accanto al QR e va a
  capo dopo «N.», come farebbe Chromium; le due righe stanno nell'altezza del QR.
- **Etichetta scritta (D-64, slice 053):** il campo «Etichetta (0045/26)» di «Nuova riparazione» e
  della modifica passa da `buste.EtichettaScritta(testo, anno)`, con `anno` quello della data di
  accettazione (`AnnoDiAccettazione`): il codice intero letto resta (spazi ai bordi tolti), un numero
  nelle forme di `LeggiRicercaEtichetta` diventa `CodiceEtichetta` (l'anno scritto vince), ogni altro
  testo resta com'è (dalla 055d con gli spazi ai bordi tolti). Il numero interno di una busta nuova lo assegna sempre il contatore: nessun
  modulo manda più `bag_number`, e quello di un invio vecchio o forgiato si ignora (risposta 8).
  Le gemelle (stesso codice, fuori dal cestino) le legge `righeConCodice`, la stessa query della
  scansione: la creazione avvisa una volta solo per le **aperte** (`GemelleAperte`), dopo tutti gli
  altri errori; la scheda le mostra in `Scheda.Gemelle`. Gemelle e scansione confrontano i codici
  normalizzati da tutte e due le parti (`NormalizzaCodice`, slice 055d; lo standalone toglie gli
  spazi solo al codice letto). Nella modifica il campo mostra «0045/26» (`EtichettaNelModulo`) solo
  se, rimandato così, ridà esattamente il codice salvato.
- **Codice normalizzato (D-64, slice 055d, Stefano 06/11/2026, risposte 1 e 2 alla 053):**
  `buste.NormalizzaCodice` è il codice come si salva e si confronta in ogni lettura (modulo,
  scansione, ricerca, gemelle, numero visibile): spazi ai bordi sempre tolti; «rip-» minuscolo e gli
  spazi **accanto ai trattini** tolti solo se così il codice diventa esattamente un'etichetta
  («rip-2026- 0045» → «RIP-2026-0045»); ogni altro codice resta com'è dentro («ABC 12», «RIP 2026
  0045», «4 5»). `EtichettaScritta` toglie anche gli spazi accanto alla barra di un numero scritto
  («0045 / 26»). La modifica normalizza anche un codice rimandato uguale al salvato, ma senza leggerlo
  come numero scritto («45» importato resta «45»). I dati già salvati non si riscrivono: la
  normalizzazione vale alla lettura, e alla prossima modifica della busta. **Tranne all'import (D-76,
  slice 057):** un codice che `NormalizzaCodice` cambia (spazi ai bordi, «rip-» minuscolo, spazi
  accanto ai trattini di un'etichetta) si riscrive normalizzato, con l'avviso
  `buste/codice_qr_normalizzato`; un codice di soli spazi resta com'è.
- **Forma non riconosciuta (D-64, slice 055d):** un codice da salvare che somiglia a un'etichetta
  senza esserlo (`SembraEtichetta`: comincia con «RIP», o è un numero di al più 6 cifre con o senza
  anno in un'altra forma: «45/2026», «0045-26», «#45», «00045», «4 5») avvisa una volta in «Nuova
  riparazione» e nella modifica (`*FormaNonRiconosciuta`, campo nascosto `forma_avvisata` come
  `gemelle_avvisate`); rimandato uguale salva com'è. Viene dopo gli errori dei campi e prima delle
  gemelle; nella modifica solo per un testo cambiato.
- **Contatore rimasto indietro (rischio della 053, rimedio della 055a, Stefano 06/11/2026):** senza
  «N° Busta» il numero di una busta nuova lo dà solo `sequenze_buste`. Se il contatore di un anno è
  dietro a un numero già usato (un dato vecchio o scritto a mano nel database), l'INSERT della busta
  trova il numero occupato. Allora `Crea`, nella stessa transazione (un UNIQUE fallito in SQLite
  annulla solo l'istruzione: `TestVincoloUnicoAnnullaSoloLIstruzione`), porta il contatore al massimo
  dei numeri dell'anno (`riallineaContatore` in `internal/buste/contatore.go`) e riprova **una
  volta** con il numero dopo. Il massimo guarda tutte le buste dell'anno, **cestino compreso** (l'indice
  unico lo copre), e legge i numeri in modo largo (`numeroDellAnno`): spazi ai bordi tolti, zeri
  davanti ignorati («0045/26» vale 45), al più cinque cifre e le due cifre dell'anno; ogni altra forma
  («45/2026», «A45/26», «123456/26») non conta. Il contatore non scende mai (`MAX`). Riga d'audit
  `bag_sequence.realigned`, risorsa `bag_sequence`, id l'anno, dati `year`, `previous` (il valore di
  prima della creazione, 0 per un anno senza riga), `new` (il massimo) e `bag_number_taken` (il numero
  trovato occupato); solo del Go. Se anche il secondo tentativo trova il numero occupato (o il numero
  dopo il massimo supera le cinque cifre) resta «Numero busta già esistente.» e la transazione annulla
  tutto, riallineamento compreso. Un numero scritto (`Accettazione.Numero`, solo dal dominio) già usato
  resta rifiutato senza riallineamento.

## Date della busta (slice 055a, D-71, D-72)

- **D-71:** la data di accettazione (`data_accettazione`, `repair[accepted_on]`) si chiama **«Data
  ricezione»** dappertutto la veda l'utente: scheda («Dati riparazione» e modulo della modifica),
  «Nuova riparazione» (era «Data accettazione»), scheda PDF (era «Data accettazione») e report (era
  «Data riparazione»). Resta precompilata con oggi e modificabile; i dati e i nomi dei campi non
  cambiano. **Dalla slice 056 (Stefano, 07/11/2026) vale anche per il filtro e la pillola della
  home:** nel cassetto la scelta «Filtra il periodo per data di» dice «Ricezione» (era
  «Accettazione»; il valore resta `accettazione`) e la pillola «📅 Ricezione: … → …» (era «📅
  Accettazione: …»). Non cambiano: le etichette «Data da» e «Data a», il 📅 senza etichetta del
  pannello QR, la ricevuta per il cliente («ACCETTATA»).
- **D-72:** nella scheda «Registrata il» (il `creato_il` della busta) si vede solo quando il suo giorno
  **in negozio** (`clock.DataDi`, Roma) è diverso dalla data di ricezione; altrimenti la riga non c'è.
- **Date nei PDF (verificate con la slice 058):** la D-71 vale nella scheda PDF («DATA RICEZIONE») e
  nel report («DATA RICEZIONE»); la ricevuta tiene «ACCETTATA» (Stefano, 07/11/2026) e l'etichetta
  76×127 non ha la data di ricezione. Nessun PDF dello standalone stampa «Registrata il», quindi la
  D-72 nei PDF non ha niente da togliere. I formati sono quelli dello standalone: ricevuta
  AAAA-MM-GG (ACCETTATA e PREVISTA, `escape(date)`), scheda PDF e report gg/mm/aaaa
  (`Clock.format`), etichetta 76×127 gg/mm/aaaa per la data di stampa e AAAA-MM-GG per «Scad.:» (vedi
  i Dubbi della 058).
- **Nel banco:** due normalizzatori in `tools/banco/confronto.go`. `D-71` cambia nello standalone la
  riga `<dt>Data</dt>`, l'etichetta del campo `repair_accepted_on` («Data», «Data accettazione») e,
  nel testo dei PDF, «DATA ACCETTAZIONE» e «DATA RIPARAZIONE» in «Data ricezione»/«DATA RICEZIONE».
  `D-72` toglie dalla scheda dello standalone la riga «Registrata il» dello stesso giorno della data.
  Li usano `scheda` (`D-64+D-71+D-72`, con l'attesa «05/01/2026 10:00» ora assente), `nuova`,
  `scheda-pdf`, `etichetta-scheda-dipendente`, `report`, `report-dipendente` e `report-filtri`
  (`D-71`); le attese dei PDF dicono «DATA RICEZIONE». Dalla slice 056 `D-71` cambia anche, nella
  home dello standalone, la scelta `<option value="accettazione">Accettazione</option>` e la pillola
  «📅 Accettazione: »: tutti i passi di `home-filtri` hanno `D-64+D-71` (il testo intero del menu
  del periodo si confronta a ogni passo) e le attese dicono «📅 Ricezione:» (anche la riga D-69 del
  passo 53 in `bench/deviazioni.json`).

## Deviazioni annotate (06/11/2026; D-75 fatta con la 055d)

- **D-73 (RIP-S-43): fatta con la 058.** Sull'etichetta 76×127 «Consegna:» diventa «All'artigiano:»
  (`stampe.EtichettaDataArtigiano`); la data resta quella di stampa e «Scad.:» resta. Nel banco il
  normalizzatore `D-73` cambia «Consegna: gg/mm/aaaa» del testo del PDF dello standalone (i passi
  delle etichette hanno `D-64+D-73`), e `lettura.PosizioniEtichetta` riconosce la riga della data
  nelle due forme.
- **D-75 (RIP-S-38, slice 055d, Stefano 06/11/2026, punto 4 della 049): fatta.** Dopo la firma di
  una scheda Compro Oro il messaggio è «Firma registrata: pratica chiusa.» al posto di «Firma
  registrata: riparazione consegnata.» dello standalone (che resta per le riparazioni). Lo sceglie
  `messaggioFirma` rileggendo la busta appena firmata. Il banco non la vede: gli scenari `firma` e
  `firma-riquadro` firmano solo riparazioni.
- **D-74 (T4.6):** dopo ogni invio WhatsApp la pagina torna alla home da sola, per tutti i link e
  pulsanti wa.me (avviso di pronta, preventivo, link al cliente, pannello QR, oreficerie, artigiani).
  Con JavaScript GestRip apre WhatsApp e porta la propria scheda alla home con i filtri di prima;
  l'audit e «già avvisata» si salvano prima di lasciare la pagina. Senza JavaScript resta come oggi.

## Decisioni di Stefano del 07/11/2026 (registrate con la slice 056)

Le risposte ai Dubbi delle slice 053, 055 e 055d. Prima di ognuna è stato guardato lo standalone a
`go-baseline` (`302439d`): una D-xx solo dove il Go farà diverso da lui.

- **D-76 (RIP-H-22, import; slice 057, fatta).** All'import dallo standalone, una volta sola, i codici QR
  salvati con spazi ai bordi o con «rip-» minuscolo si riscrivono nella forma normalizzata
  (`buste.NormalizzaCodice`), con un avviso nel report d'import. Lo standalone salva il codice
  come è scritto (`qr_code` è testo libero, `Repair.blank_qr?/1` toglie gli spazi solo per dire
  «vuoto») e l'import di oggi lo copia uguale. Va con la lettura del QR che già confronta anche il
  codice salvato normalizzato (slice 055d, versione (a) confermata da Stefano il 07/11/2026).
- **D-77 (RIP-H-30, numero di busta, import; slice 057, fatta).** All'import i numeri interni vecchi con
  zeri davanti («0046/26») si riscrivono senza zeri («46/26»), con un avviso nel report d'import. Lo
  standalone tiene il numero importato com'era (`@bag_number_format` accetta gli zeri, solo il
  numero scritto a mano li perde con `normalize_bag_number/1`) e il suo indice unico confronta il
  testo esatto, quindi lì «0046/26» e «46/26» possono stare insieme. È il difetto 1 di
  `docs/domande-aperte.md` («Difetti in lista»). Cosa fare quando il numero senza zeri è già di
  un'altra busta: la 057 non lo riscrive e lo dice in un avviso a parte del report
  (`buste/numero_senza_zeri_occupato`). **Scelta definitiva** (Stefano, 09/11/2026; registrata con
  la 060).
- **Come si fanno D-76 e D-77 (slice 057):** `internal/importer/riscritture.go`, dopo la copia di tutte
  le tabelle e nella stessa transazione, prima i numeri e poi i codici. Il report ha un avviso per
  riscrittura (`buste/numero_senza_zeri`, `buste/numero_senza_zeri_occupato`,
  `buste/codice_qr_normalizzato`) con quante buste (`righe`) e quali (`buste`: id, numero interno,
  il numero di prima o quello occupato), mai il codice né un dato del cliente; `aggiornato_il` non si
  tocca.
- **D-78 (RIP-H-28, lettore con la fotocamera): fatta con la 059.** La finestra della fotocamera ha una
  linea di mira. Lo standalone non la disegna (`qr_scanner.js`: video, riga di stato, «🔄 Cambia
  fotocamera», «Chiudi»). Il Go ne ha una dalla slice 055 (`.gs-lettore-mira`, riga rossa a metà
  del video, Dubbio 2 della 055); la 059 la rende un po' più visibile (3 px con un bordo scuro, stesso
  comportamento: non cambia la lettura), la mostra solo sopra un video acceso (`s.mira`, nascosta senza
  fotocamera) e la mette nel manuale (capitolo 06) e nella schermata
  `06-fotocamera-telefono.png`. Il banco non la confronta: è dentro la finestra.
- **D-79 (RIP-H-03, RIP-H-12, segno «⚠ senza QR»): fatta con la 059, estesa con la 060.** «⚠ senza QR» anche sulle buste
  con un QR di altro formato (per esempio «SINT-QR-0045»), cioè senza un'etichetta `RIP-…`. Lo
  standalone marca solo le buste aperte senza nessun codice (`Repair.missing_qr?/1`). Nel Go, dalla
  059, il riquadro della lista porta il segno anche sulla busta **aperta** con un codice che non è
  un'etichetta (`buste.QRAltroFormato`: non vuoto e non `EtichettaDi`, quindi anche «RIP-2026-45»),
  con il titolo «Busta senza etichetta RIP: il suo QR è di un altro formato» (`rigaHome.AltroFormato`);
  la busta senza codice tiene «Busta senza codice QR: aggiungilo dalla scheda». Nella ricerca per
  numero d'etichetta resta la regola della 055d: ogni busta trovata per il numero interno ha il segno,
  anche chiusa, con «Busta senza etichetta: trovata con il numero interno» (vince sulle altre due).
  **Estesa con la 060** (Stefano, 09/11/2026, Dubbi 1 e 3 della 059): anche il riquadro «⚠ Senza QR»
  in alto, il filtro `senza_qr` e la sua pillola contano le buste aperte con un QR di altro formato,
  oltre a quelle senza codice (lo standalone: solo queste, `missing_qr_query/1`). In SQL la forma
  dell'etichetta non si riscrive: `senzaQRSQL` chiama la funzione Go `gestrip_senza_etichetta`
  (`internal/buste/senza_qr_sql.go`, registrata nel driver SQLite: `EtichettaDi` sul codice). Anche
  la scheda porta «⚠ senza QR» accanto al titolo su quelle buste, con il titolo «Busta senza etichetta
  RIP: il suo QR è di un altro formato» (`datiScheda.AltroFormato`). Nel banco: 291 righe D-79 in
  `bench/deviazioni.json` (108 della 059; 183 della 060: il riquadro «⚠ Senza QR» e le liste dei
  filtri `senza_qr` dei dati importati), generate dalle inattese di un giro e controllate una per una;
  le attese di quei campi sono uscite dagli scenari (il banco non ammette righe sulle attese).
- **Senza D (come lo standalone), fatte con la slice 059:**
  - il lettore a pistola con un codice che non è di nessuna busta suona i due bip gravi del
    fallimento (RIP-H-27). Nello standalone la pistola scrive nel campo «Cerca per QR busta» e lo
    scanner suona il fallimento; nel Go, con il campo unico (D-63), era una ricerca scritta senza
    suono. La regola (Dubbio 1 della 055): dal POST del campo unico un testo con la forma esatta di
    un'etichetta (`buste.EtichettaDi`: anche minuscolo o con gli spazi che `NormalizzaCodice` toglie)
    per cui la ricerca scritta non trova nessuna busta disegna la lista con `#repair-search-outcome`
    (`data-qr-esito="ko"`, `esitoRicercaScritta` in `cerca.go`); `app.js` la mette al posto della
    pagina e suona `suonoQR.suona(false)`, quindi con lo sblocco nel tocco su 📷, l'interruttore
    `gs-qr-sound` e la vibrazione della 055. Un nome, un numero, un «RIP-2026-45» o un'etichetta che
    la ricerca testuale trova restano muti; senza JavaScript non suona niente;
  - il 📷 accanto a «Etichetta» («Nuova riparazione» e modifica) apre la fotocamera, come lo
    standalone (`#repair-qr-code-scan`, slice 193 dello standalone), senza il ripiego della 053 che
    metteva il cursore nel campo al tocco (Dubbio 3 della 055). Dalla 059, solo quando la fotocamera
    non si apre (contesto non sicuro, browser senza fotocamera, `getUserMedia` rifiutato: nessuna
    fotocamera o permesso negato) la finestra dice il messaggio come nella home e, chiusa con «Chiudi» o
    Esc (`chiudiDaUtente`), rimette il cursore nel campo per la pistola (`senzaFotocamera`,
    `s.ripiego`). Lo standalone non sposta il cursore: è un'estensione della D-63 (Stefano,
    09/11/2026; Dubbio 2 della 059).
- **Nota, senza D:** i segnaposto delle opzioni di stampa (slice 055d) si rivedono alla T5.5, con la
  stampante MUNBYN vera. L'indirizzo della pagina non cambia con una ricerca scritta (slice 055,
  Dubbio 4), come oggi.

## Decisioni di Stefano del 09/11/2026 (registrate con la slice 060)

Le risposte ai Dubbi delle slice 056–059.

- **D-80 (RIP-S-42, RIP-S-43: ricevuta ed etichetta 76×127; slice 060, fatta).** Sulla ricevuta le
  date di «ACCETTATA» e di «PREVISTA», e sull'etichetta 76×127 quella di «Scad.:», sono gg/mm/aaaa
  («05/09/2026»). Lo standalone le scrive AAAA-MM-GG (`receipt.ex`, `bag_label.ex`: la data com'è nel
  database). Un testo scritto al posto della data («fine mese», «Da concordare») resta com'è. Nel Go:
  `DatiRicevuta.Accettata` e `DatiRicevuta.Prevista`, `DatiEtichetta.Scadenza` con
  `dataGiornoMeseAnno` (`internal/buste`). Nel banco: normalizzatore `D-80` (`tools/banco/confronto.go`,
  solo una data AAAA-MM-GG subito dopo «ACCETTATA», «PREVISTA» o «Scad.:») sui passi PDF di
  `ricevuta`, `ricevuta-dipendente` (`D-64+D-80`), `etichetta` e delle etichette di
  `etichetta-scheda-dipendente` (`D-64+D-73+D-80`).
- **«✓ Consegnata» senza JavaScript chiede conferma (slice 060, fatta; riserva della D-70).** Senza
  JavaScript il pulsante manda il modulo senza `conferma=1` e il server risponde con la pagina
  `conferma_consegna` (`#quick-delivered-confirm`: la frase della conferma col numero visibile, «Sì»
  che rimanda il modulo con `conferma=1`, «Annulla» verso la lista), come il pannello del QR; niente
  si scrive. Con JavaScript resta `data-confirm`: dopo «OK» `app.js` mette `conferma=1` nel campo
  `data-conferma-js` e il server non chiede di nuovo. Su una busta che non è pronta o non c'è, la lista
  con «Transizione non consentita.». Lo standalone non ha il caso (è un evento LiveView).
- **«Cambia stato» (slice 060, fatta, senza D):** il pulsante del menu dello stato senza JavaScript si
  chiama «Cambia stato» (era «Cambia»). Lo standalone non ha il pulsante.
- **D-77, numero senza zeri già di un'altra busta:** non si riscrive e c'è un avviso a parte nel
  report; la scelta è definitiva (vedi la riga D-77).
- **Galleria della home, una riga d'audit per visita** (`repair.staff_gallery_viewed`, slice 057):
  è un'estensione della D-68, non una D nuova.
- **Il cursore che torna nel campo «Etichetta»** quando la fotocamera non si apre (slice 059): è
  un'estensione della D-63.
- **I messaggi della fotocamera** (quelli inglesi del browser, come lo standalone) si tradurranno più
  avanti, in una slice di rifinitura.
- **I nomi dei file** dei PDF e il titolo del lavoro di stampa restano sul numero interno
  (`ricevuta-45-26.pdf`, con dentro «Busta 0045/26»), come lo standalone.

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

## Processo figlio delle foto (slice 037c)

- `imaging.ErrImmagine` è **solo** un rifiuto della foto. Un figlio fermato dopo il suo tempo,
  ucciso da un segnale (anche al limite di CPU), senza memoria, che non parte o che esce fuori dal
  protocollo è `imaging.ErrRisorse`, con la causa (`imaging.CausaDi`): per chi carica
  `buste.MsgFotoNonElaborata`, nel registro la causa e mai la foto, la chiave o un percorso. Per la
  miniatura si ripiega sulla foto intera come per un rifiuto, ma non è una foto guasta.
- Il figlio esce con i codici di `imaging.Uscita…`; mai 1 o 2 (2 è il runtime di Go: errore fatale o
  panic). Un panic della conversione si recupera ed è un rifiuto.
- I test che fanno girare foto grandi nel figlio vero danno al `Lavoratore` un `Tempo` largo
  (`tempoFotoGrandi`, 2 minuti): provano la memoria e la CPU, che non dipendono dal carico, mentre i
  10 s di `TempoMassimo` sono di orologio e li prova solo `TestFiglioUccisoDopoIlTempo`. Un errore
  in quei test scrive la causa.
- Limite di CPU del figlio (slice 037e): il padre lo manda come argomento
  (`gestrip image-worker <operazione> <secondi>`); il figlio accetta solo da `MinCPUFiglio` (1 s) a
  `MaxCPUFiglio` (60 s), senza argomento usa `LimitiDelFiglio` (8 s), altrimenti esce con
  `UscitaOperazione`. In produzione è sempre 8 s: `Lavoratore.CPU` lo cambiano solo i test,
  `NuovoLavoratore` (quindi `serve`) lo lascia a 0 e il codice del pacchetto non legge l'ambiente
  (`TestLimitiSenzaAmbiente`).
- `GESTRIP_TEST_CPU_FIGLIO` (secondi, letta solo dal codice dei test) alza il limite di CPU del
  figlio nei test delle foto grandi. La CI la mette a 30 (`.github/workflows/ci.yml`): sul runner
  di GitHub la foto più pesante ha usato fino a 7,92 s di CPU. Con la variabile la soglia di 5 s di
  CPU per foto (`cpuMassimaFotoGrande`, tarata su pc-fisso) non si controlla: la CPU di ogni foto
  si scrive comunque nel log. `scripts/check` non la mette: su pc-fisso la soglia vale.

## Archivio media e pulizia (slice 041)

- Ogni colonna che nomina un oggetto dell'archivio sta in `media.Riferimenti` (`referenced?/1`):
  la pulizia non cancella un oggetto che una di quelle righe usa. Una tabella nuova con una colonna
  che ha «chiave» nel nome va lì o tra le escluse di `TestRiferimentiCopronoLoSchema`, con il perché.
- Un oggetto si mette in coda (`media.Accoda`, nella transazione che toglie le righe) **solo** dove
  lo standalone chiama `queue_deletions/1`: cestino definitivo, sostituzione del logo, conservazione
  delle foto. Togliere una foto da una busta non lo fa: quel file lo raccoglie la pulizia degli
  orfani dopo un backup verificato (RIP-S-55).
- Dopo un import dallo standalone, finché non c'è un backup verificato (`db.BloccatoDopoImport`,
  lo stesso controllo della conservazione dell'audit), la pulizia non tocca la coda e lo dice una
  volta per avvio nel registro; i temporanei li toglie comunque (via prudente in attesa della
  domanda aperta 20).
- Chi scrive o cancella file nell'archivio tiene il lucchetto dell'archivio (`ConLucchetto`, uno
  per processo): la pulizia, che gira ogni 60 s in `serve`, toglie i temporanei `.upload-` che
  trova con il lucchetto preso.

## Documenti PDF (slice 042)

- Ogni PDF si fa con il motore `internal/pdf` (sopra `github.com/signintech/gopdf` v0.38.3, MIT; QR
  con `github.com/boombuler/barcode` v1.1.0, MIT), mai con Chromium né con un altro motore. I
  documenti stanno in `internal/stampe` (uno per file, come la ricevuta), il dominio dà i dati già
  pronti (testi, importi con `Money.label`, date) e la rotta solo i byte.
- **Misure:** millimetri, origine in alto a sinistra; il corpo dei caratteri in pt, come il CSS dello
  standalone. Il formato della pagina è quello del documento dello standalone (`paperWidth`,
  `paperHeight` di ChromicPDF); i margini sono quelli che Chromium applica davvero: se il foglio dice
  `@page { margin: 0 }` vince quello (la ricevuta parte dal bordo, misurato).
- **Font:** solo DejaVu Sans normale e grassetto incorporati (`internal/pdf/caratteri/`, file
  dell'immagine dello standalone, licenza in `caratteri/LICENSE`, impronte fissate da
  `TestFontIncorporati`). Un carattere che il font non ha esce come «�» (anche fuori dal piano di
  base: gopdf scrive solo codici a 16 bit); i caratteri di controllo non si stampano.
- **Testo:** `pdf.Casella` copia il CSS: interlinea `normal` = ascesa + discesa (0 nel campo
  `Interlinea`), `Spazi: Collassati` (HTML) o `Conservati` (pre-wrap), `SpezzaParole` per
  `overflow-wrap: anywhere`, `MaxRighe` per il taglio. `text-transform: uppercase` si scrive già in
  maiuscolo nel Go (il testo estratto di Chromium è in maiuscolo).
- **Consegna:** `Documento.Byte` passa sempre da `pdf.Controlla`, che rilegge il PDF; un errore vuol
  dire niente PDF e niente riga d'audit. La riga d'audit del documento si scrive solo dopo il PDF
  fatto (`buste.Ricevuta` riceve la funzione che lo fa). Un PDF non è un'apertura della scheda: scrive
  solo la riga della sua azione, come lo standalone (`repair.receipt_generated`), non `repair.viewed`.
- **Risposta:** `Content-Type: application/pdf` senza charset, `Cache-Control: private, no-store`,
  `Content-Disposition` con `disposizioneAllegato` (codifica di `send_download/3`, misurata),
  `Content-Length`. Una rotta di un PDF si registra con `f.rotta` (niente guscio) e va nella tabella
  dei permessi.
- **Come si prova un PDF:** con `internal/pdf/lettura` (nostro, senza dipendenze), che legge i PDF del
  motore e quelli di Chromium dello standalone: formato delle pagine (`Formato`, mm interi), righe di
  testo con posizione (mm) e corpo (pt), rettangoli pieni (per i QR), `TestoNormalizzato` (spazi
  ridotti, legature sciolte). Non legge stream xref, filtri diversi da FlateDecode né font senza
  ToUnicode: allora dà errore, non indovina. I test di dominio e di pagina confrontano le righe
  esatte; il confronto con lo standalone usa il testo normalizzato (D-24). Un PDF di Chromium dello
  standalone sui dati sintetici si può tenere come fixture in `internal/pdf/lettura/testdata/`.
- **Font e documenti con Helvetica (slice 043):** la scheda PDF dello standalone chiede Helvetica
  Neue/Helvetica/Arial e Chromium la stampa in Liberation Sans (misurato nel PDF di fab-220); il Go la
  stampa in DejaVu come gli altri documenti. Il testo estratto è lo stesso (D-24), cambiano dove va a
  capo e l'altezza delle righe: una scheda lunghissima può passare alla seconda pagina prima di quella
  di Chromium (domanda nella ricevuta della 043).
- **Spaziatura e immagini (slice 043):** `Casella.Spaziatura` è la `letter-spacing` del CSS in pt
  (conta per l'a capo e per l'allineamento); `Documento.Immagine` disegna un PNG (decodificato con la
  libreria standard e riscritto in RGBA, con la trasparenza; oltre `MaxPixelPNG` o non decodificabile
  è un errore) e `DimensioniPNG` ne legge i pixel senza decodificarlo. Per scegliere fra immagine e
  ripiego si usa `LeggiPNG` (decodifica intera), mai `DimensioniPNG`: un IHDR giusto con i dati guasti
  passa la seconda e non la prima; `ImmaginePNG` disegna il PNG già letto. Le misure di un'immagine in
  linea sono quelle del CSS: un px (1/96") per pixel.
- **Banco:** i campi `pdf:formato` e `pdf:testo` confrontano i PDF delle due app; un corpo che non è
  un PDF vale «(non un PDF)».
- **QR e posizioni nel banco (slice 043, il resto di D-24):**
  - `pdf:qr` è il contenuto dei QR di ogni pagina (« | » fra le pagine; «(nessun QR)», «(QR
    illeggibile)»), decodificato da `lettura.CodiciQR` dai rettangoli pieni: Chromium disegna l'SVG
    dello standalone come un rettangolo `re … f` per modulo scuro, il motore Go un rettangolo per
    tratto di riga. Il decodificatore è nostro (ISO/IEC 18004: formato dalle due copie, maschera,
    blocchi, modi numerico, alfanumerico, byte ed ECI), controlla che ogni blocco sia una parola di
    Reed-Solomon senza correggere niente (rettangoli esatti: un modulo diverso è un codice diverso) e
    prova anche il codice trasposto, come i telefoni: **il QR dello standalone è trasposto** (QRCode
    3.2.0 mette la riga della matrice sull'asse x) e si legge solo così. Conta il testo; il Go stampa
    il QR diritto. Attenzione: `pdf:qr` confronta **solo il testo**, quindi un QR del Go che uscisse
    specchiato il banco non lo vedrebbe; lo ferma il test di `internal/stampe` che controlla
    `!Specchiato` (`TestEtichettaContenutiEOrdine`). Anche i bit di formato non si correggono: almeno
    una delle due copie deve essere esatta.
  - `pdf:posizioni` sono le posizioni chiave dell'etichetta busta (`lettura.PosizioniEtichetta`): il
    riquadro dei moduli del QR, la riga del numero, l'inizio di «Consegna:», la casella FATTO (il
    primo riquadro di solo tratto). **Dalla slice 058** della riga del numero conta solo la y: la riga
    è centrata accanto al QR e il suo inizio dipende dal testo, che con la D-64 nelle due app è diverso
    («#5/26» e «N. 5/26»); si riconosce nelle forme «#…», «N. …» e «N.» (a capo). La data di stampa si
    riconosce come «Consegna:» o «All'artigiano:» (D-73). L'inizio della riga del numero, a testo
    uguale, lo confronta `TestEtichettaComeQuellaDiChromium`. Si confrontano **entro ±0,5 mm**, fra le app e con le attese
    (`lettura.PosizioniVicine`), mai al centesimo: Chromium arrotonda il layout ai px (pagina di 76 mm
    larga 75,86, margine di 3 mm 2,91). Una pagina nuova di etichette (le prenumerate, il foglio della
    consegna) che vuole posizioni sue le aggiunge lì, con il perché.
  - Un test in `internal/stampe` confronta il documento del Go con il PDF di Chromium della stessa
    busta salvato in `internal/pdf/lettura/testdata/` (formato, testo, QR e, per le etichette,
    posizioni): il controllo D-24 gira anche senza banco.

- **Documenti di più pagine con il piè (slice 044, report A4):** un documento che numera le pagine
  («Pagina N / M») rimanda i disegni alla fine come la scheda (`opera` con pagina e strato) e scrive il
  piè di ogni pagina dopo il corpo, quando conosce il totale: nel testo estratto di Chromium il piè di
  `displayHeaderFooter` viene dopo il corpo di ogni pagina, e nel Go deve stare nello stesso punto
  (D-24 confronta il testo nell'ordine). Una busta per pagina vuol dire una pagina nuova prima di ogni
  scheda tranne la prima; una scheda più lunga continua sulla pagina dopo e il totale la conta.
- **Parti di un documento tolte da una D-xx (slice 044):** nel banco una deviazione con `"togli"` e
  `"copie"` (solo `pdf:testo`, livello HTTP, `standalone` e `go` vuoti) copre la differenza se il testo
  dello standalone contiene la frase esattamente `copie` volte (una per scheda del passo: una nota
  uguale alla frase sarebbe una copia in più) e, tolte e normalizzati gli spazi, è esattamente quello
  del Go: il resto del testo si confronta ancora tutto, anche quello che cambia ogni giorno (le date).
  La usa D-01 (la sezione delle foto del report, «📷 FOTO Nessuna foto allegata.» su ogni scheda), un
  passo per riga di `bench/deviazioni.json`. Il test di `internal/stampe` contro la fixture di Chromium
  toglie la stessa frase.

## Parametri della query come Plug (slice 044)

- Un parametro che lo standalone legge da `conn.params` e che può arrivare come lista o mappa
  (`ids[]=`, `ids[a]=`) si legge come `Plug.Conn.Query.decode/1`, misurato nell'immagine: coppie
  separate solo da «&», «+» è uno spazio, la chiave si decodifica prima, la radice fino alla prima «[»
  e le sottochiavi divise su «][» solo se la chiave finisce con «]» (altrimenti è un'altra chiave);
  vale l'ultima assegnazione, che sostituisce anche un valore di un altro tipo. Oggi lo fa
  `idsDellaQuery` (`internal/web/report.go`); un secondo parametro così lo generalizza lì.
- Un messaggio che stampa `inspect/1` di un valore di Plug usa `valorePlug.inspect`: escape e
  limiti misurati (50 elementi, uno in meno per ogni livello e per ogni elemento prima, 50 byte di un
  testo non stampabile, 4096 caratteri), chiavi delle mappe in ordine (fino a 32 chiavi: oltre
  l'ordine di Erlang è quello dell'hash e non si copia).

## Banco: scenari che cambiano i dati (slice 047)

- Gli scenari HTTP girano nell'ordine dei nomi. Uno scenario che cambia dati letti da altri scenari
  (la firma consegna la 15/26 e azzera la firma importata della 3/26) ha `"in_fondo": true` e gira
  dopo tutti gli altri (`TestScenariInFondo`). Il livello browser gira dopo tutto il livello HTTP:
  le sue buste non devono essere quelle che uno scenario HTTP cambia.
- Livello browser sul telefono: un tratto col dito si fa con `Input.dispatchTouchEvent` (CDP) e
  finisce con il dito fermo prima di alzarsi (`firma` in `banco.mjs`); senza, Chromium legge il
  tratto come una spinta e il tocco dopo serve solo a fermarla, senza clic (misurato su tutte e due
  le app).
- Il PNG che il riquadro di firma produce dipende dalla larghezza del riquadro, che nelle due app non
  è la stessa (impaginazione diversa): `firma-riquadro` ne confronta il formato, il tratto, il colore
  più frequente, l'altezza, i pixel per px CSS e che i byte serviti da `firma.png` siano quelli
  mandati, mai i byte.

- **Normalizzatori (slice 050):** un passo con `"normalizza": "D-xx"` passa il corpo dello
  standalone dal normalizzatore di quella deviazione (`normalizzatori` in `tools/banco/confronto.go`)
  prima di confrontare e di guardare le attese; un campo diverso che normalizzato è uguale al Go è
  una differenza attesa («ATTESA (D-64, normalizzata)»). Serve alle deviazioni che toccano ogni busta
  di una pagina (D-64: «#21/26» dello standalone diventa il numero dell'etichetta, dalla tabella
  scritta a mano `numeriVisibiliBanco` di `tools/banco/home.go`), al posto di centinaia di righe in
  `bench/deviazioni.json`. Un campo che il Go dà uguale al valore **grezzo** dello standalone, dove
  il normalizzato è diverso, è inatteso («D-64 non applicata dal Go»). La D-xx deve essere approvata
  nell'inventario come quelle dei passi `solo_go`; un passo `solo_go` o `solo_standalone` non
  normalizza.
  Dalla slice 055a: più normalizzatori si scrivono uniti da «+» (`"D-64+D-71+D-72"`) e si applicano
  in quell'ordine, e ogni D-xx si controlla nell'inventario; per i campi del PDF (`pdf:testo`…) il
  normalizzatore passa sul **testo letto** dal PDF, non sul corpo. Una differenza che resta anche
  normalizzata porta lo standalone normalizzato, così una riga di `bench/deviazioni.json` si scrive su
  quello (la D-01 del report, con `togli`, copre il report anche sopra la D-71).
- **Ordine degli scenari della home (slice 050, 051, 052):** `home-cerca.json`, `home-contatori.json`,
  `home-dipendente.json` e `home-filtri.json` girano prima di `home.json` (ordine dei nomi: «-» viene
  prima di «.»), e contano su un messaggio non letto della 27/26 («💬 Messaggi: 1»); l'ultimo passo di
  `home` apre la 27/26 e lo segna letto, così la campanella dello standalone torna a zero prima del
  livello browser. `TestOrdineScenariHome` lo controlla: uno scenario nuovo che legge la home dopo
  `home.json` vede «Messaggi: 0». `cerca-qr.json` gira prima di tutti gli scenari della home e non
  cambia dati: le sue scansioni non aprono la scheda (niente `repair.viewed` in più, che lo scenario
  `registro` confronta) e non toccano la 27/26.
- **Campo unico della home (slice 052, D-63):** il modulo `#repair-search-form` del Go è un POST a
  `/cerca-qr` con «🔍» e «📷», lo standalone ha un GET con la sola casella (e la scansione è un evento
  LiveView). Il token del Go sta **fuori** dal modulo (`form="repair-search-form"`), così i campi
  `input@N` (i nascosti di `search_hidden/1`, poi `cerca`) sono quelli dello standalone e
  `home-filtri` li confronta ancora; metodo, azione e testo del modulo, e l'azione del cassetto (GET a
  `/cerca` nel Go, a `/` nello standalone), non si confrontano e li fissa `home-cerca` con passi
  `solo_go` D-63. Lo stesso per `GET /cerca`, `POST /cerca-qr` (scenario `cerca-qr`) e la ricerca per
  numero d'etichetta nella lista (passi `solo_go` D-64): un passo che cerca un numero da 1 a 4 cifre,
  con o senza «/AA», nello standalone è un «contiene» e nel Go no. I passi di quella forma che danno
  le stesse buste nelle due app (`home-filtri` 9 e 12, `report-filtri` 7 e 9) restano confrontati
  finché i risultati coincidono: una differenza lì vuol dire che è cambiata la semina, non il Go.
- **Il codice letto non va in un indirizzo (slice 052, D-63):** dal POST del campo unico una ricerca
  scritta che non apre una sola scheda disegna la lista nella risposta (`no-store`, nessun redirect);
  nei link della lista (paginazione, «⧉ Schede PDF») e nel campo del cassetto, che è un GET, un
  `cerca` con la forma esatta di un'etichetta (`buste.EtichettaDi`) non c'è (`parametriLink`).

- **PDF e firma con la D-64 e la D-73 (slice 058):** il normalizzatore `D-64` riconosce anche
  «Busta N/AA» (ricevuta) e «N° BUSTA N/AA» (scheda PDF) nel testo dei PDF; «#N/AA» di etichetta,
  report e pagina della firma è la forma di sempre. I passi dei PDF di `ricevuta`,
  `ricevuta-dipendente` (`D-64`), `scheda-pdf`, `report`, `report-dipendente`, `report-filtri`,
  `etichetta-scheda-dipendente` (`D-64+D-71`) ed `etichetta` (`D-64+D-73`) sono normalizzati, e le
  attese dicono «Busta N. 1/26», «N° BUSTA N. 1/26». In fondo a `ricevuta`, `etichetta` e
  `scheda-pdf` quattro passi sulle buste della semina 22/26 (0045/26), 25/26 (0045/25), 35/26 (senza
  QR) e 36/26 (codice di altro formato), e in fondo a `report` il report `ids=22,35,25,36` (con la
  sua riga D-01). Nello scenario `firma` i passi della pagina hanno `D-64` e confrontano «Firma busta
  N. 15/26 · Laboratorio» e «✍️ Consegna busta N. 15/26»; nel livello browser il titolo di
  `firma-riquadro` (`pagina.titolo`) è una riga D-64 di `bench/deviazioni.json` per vista.
- **Scheda e «Nuova riparazione» con la D-64 (slice 053):** i passi delle schede di `scheda`, `numeri`
  e `busta-etichetta` hanno `"normalizza": "D-64"` e il titolo del Go («Busta N. 1/26 · Laboratorio»,
  «Busta #0045/26 · Laboratorio»); i campi tolti o rinominati di `GET /nuova` («N° Busta», «busta #»,
  `bag_number`, «Codice QR busta» → «Etichetta (0045/26)») sono righe D-64 in
  `bench/deviazioni.json`. `busta-etichetta` è `in_fondo`: i suoi passi `solo_go` aprono la scheda
  sul solo Go (una `repair.viewed` in più), quindi deve girare dopo `registro` e `viste`, che contano
  le righe; i POST a `/nuova` hanno l'artigiano 1, che c'è solo nei dati importati, così sul database
  vuoto il modulo si ferma prima e nessun dato crea una busta.
- **Pannello delle azioni del QR (slice 054, D-70):** lo scenario HTTP `qr-pannello` (passi `solo_go`
  D-70) è `in_fondo` e gira dopo `firma` (`TestScenariInFondo`): avanza solo nel Go la 29/26
  (pronta, poi consegnata) e la 33/26 (preventivo, poi di nuovo in lavorazione), che nessuno
  scenario dopo legge; la 30/26 e la 34/26 le legge senza cambiarle. Lo scenario browser
  `pannello-qr` usa invece la 4/26 (le dà l'etichetta RIP-2026-0058 dalla modifica della scheda,
  poi il rientro dall'artigiano), la 32/26, la 34/26 e la 31/26, nelle due app; lì il confronto è
  su quello che si vede (pulsanti, frase della conferma, avviso), su dove porta l'azione e sul
  messaggio, mai sull'indirizzo della pagina della scansione (`/cerca-qr` nel Go) né sul numero del
  titolo (D-64). Nel modulo del pannello lo stato mostrato si chiama `stato_visto`: `stato` è il
  filtro della lista, che il modulo porta con gli altri.
- **Pannello del QR, regola (slice 054):** le azioni del pannello passano da `buste.AvanzaDaQR` e
  `buste.PreventivoDaQR`, che usano lo stesso cambio di stato della scheda (`passaStato` in
  `internal/buste/stati.go`): grafo, date, conservazione, lotto e audit non si duplicano. Una
  conferma nuova si scrive con il modulo e due blocchi (`data-qr-scelte` e `data-qr-conferma`, il
  secondo `hidden`): `app.js` scambia i blocchi senza passare dal server, e il server disegna la
  conferma (solo il secondo blocco, visibile) quando il modulo arriva senza `conferma=1`.

- **Lettore QR, suono e pannello sopra la lista (slice 055, RIP-H-22, RIP-H-27, RIP-H-28, D-52,
  D-70):** il lettore è in `app.js` (`lettoreQR`): jsQR si carica solo al primo tocco su 📷,
  dall'indirizzo con l'hash che `guscio.go` scrive al posto di `"__JSQR__"` (niente CDN: il negozio
  lavora anche senza internet). jsQR 1.4.0 legge da sé i QR a specchio sulla diagonale delle etichette
  dello standalone (D-52: `decode` riprova la griglia trasposta), quindi `app.js` non trasforma i
  fotogrammi. Il suono (`suonoQR`) lo decide il server: `data-qr-esito="ok|ko"` su `#qr-lookup`
  della pagina della scansione (`datiCercaQR.Esito`, `mostraQRKO` per le azioni rifiutate; assente
  per la scelta di una gemella e per la conferma disegnata dal server, che lo standalone non suona),
  e il redirect di un'azione riuscita. Sulla lista (`#repairs` nella pagina) il campo unico e i moduli
  del pannello partono con `fetch`: la pagina della scansione dà il riquadro, che va sopra la lista
  senza il suo campo unico; un redirect passa da `mostraPagina` (con la cronologia); il codice non va
  mai nell'indirizzo (`TestScriptDelPannelloSopraLaLista`). Senza JavaScript resta il POST della 052.
  Una regola nuova del pannello va provata anche con il riquadro dentro la lista: gli ascoltatori sono
  delegati dal documento e quello del doppio invio (054) deve restare prima di quello del `fetch`.
- **Pistola, fotocamera assente e due fotocamere (slice 059):** lo scenario browser `lettore-qr` ha tre
  prove in più, ognuna nel suo Chromium (`argomentiFotocamera` in `banco.mjs`): `vuota` (la fotocamera
  finta di Chromium senza file, un disegno senza QR) per la **pistola** sulla home: il tocco su 📷 e
  «Chiudi» sbloccano l'audio, poi il codice scritto con Invio nel campo unico del Go o in «Cerca per QR
  busta» dello standalone; `nessuna` (nessuna opzione: `getUserMedia` dà «Requested device not found»)
  per il 📷 di «Etichetta» su «Nuova riparazione», con «Chiudi» e con Esc; `due`
  (`--use-fake-device-for-media-stream=device-count=2`, le fotocamere `fake_device_0` e
  `fake_device_1`) per «🔄 Cambia fotocamera» avanti e indietro, la scelta ricordata e la fotocamera
  spenta alla chiusura. I campi solo del Go (`solo_go`): il nome scritto nella pistola, che lo
  standalone suona come un QR sconosciuto (D-63), e il cursore nel campo dopo una fotocamera assente.
- **Fotocamera finta del banco (slice 055):** lo scenario browser `lettore-qr` gira dopo gli altri,
  con un Chromium per fotocamera (`--use-fake-device-for-media-stream`,
  `--use-fake-ui-for-media-stream`, senza il quale il Chromium senza finestra risponde «Not
  supported», e `--use-file-for-fake-video-capture`). I tre video Y4M (`diritto`, `trasposto`,
  `etichetta`) li scrive `banco esegui` in `fotocamere/` dell'istanza (`tools/banco/fotocamera.go`),
  montata in sola lettura in `/banco/fotocamere`: il QR di `RIP-2026-0056` (la 37/26 consegnata dei
  dati importati) diritto e trasposto, e il QR dell'etichetta 5 di Chromium dello standalone
  (`internal/pdf/lettura/testdata/chromium-etichetta-5.pdf`, opzione `-etichetta-qr`), ridisegnato
  dai suoi rettangoli. Il suono si osserva dagli oscillatori Web Audio e da `navigator.vibrate`. Lo
  scenario legge e non cambia niente: la 37/26 e le gemelle 5/26 e 10/26 (SINT-QR-0005) restano come
  sono per `pannello-qr` e gli altri.
- **Azioni del riquadro della lista (slice 056, RIP-H-14, 15, 16, 20):** nel livello HTTP la home
  (`home` passo 2, `home-contatori` passi 2 e 6, `home-filtri` passi 3 e 22) confronta il markup che
  le due app disegnano già alla prima GET: «✓ Pronta» e «✓ Consegnata» (testo, `title`, la frase di
  `data-confirm`), il 📌 (`aria-label`, `title`), il menu dello stato (`name`, `aria-label`,
  `disabled`, i valori e la scelta delle opzioni) e `#list-return` (`data-path`). La conferma e
  l'`aria-label` del menu portano il numero interpolato («Segnare la busta 32/26 come consegnata?»,
  «Stato 22/26»): il normalizzatore D-64 li riconosce come «Apri busta …». Le azioni vere le fa lo
  scenario browser `riquadro-azioni` nelle due app (nello standalone sono eventi LiveView, senza
  HTTP): menu sulla 36/26 e 28/26 (rimesse in lavorazione alla fine), 📌 sulla 35/26 e 33/26 (tolto
  alla fine), «✓ Pronta» e «✓ Consegnata» sulla 22/26 e 23/26, che restano consegnate; nessuna è
  di un altro scenario browser. Lì la frase della conferma ha il numero scritto «N» (la D-64 la
  confronta il livello HTTP). Il `data-path` di `#list-return` si confronta solo su liste con
  parametri validi: lo standalone scrive i parametri dell'indirizzo come sono (anche «p=99» o
  «stato=boh»), il Go quelli dei filtri letti, in ordine di nome come Plug.
- **Moduli del riquadro e JavaScript (slice 056):** i moduli `data-azione-lista` di `home.html`
  portano il campo `lista` (la query della lista di adesso, `ritornoLista`); il server non la segue
  com'è, la rilegge con `buste.LeggiFiltri` e ne riscrive l'indirizzo (`listaDelModulo`). Con
  JavaScript `app.js` li manda con `fetch` e mette la risposta al posto della pagina con lo stesso
  scorrimento (`mostraPagina(…, true)`), con il messaggio fisso in cima (`flash-sospesa`); il menu
  parte all'evento `change` (`requestSubmit`) e il pulsante «Cambia» (`data-solo-senza-js`) si
  nasconde. Un elemento solo per chi non ha JavaScript si segna `data-solo-senza-js`, mai con
  `<noscript>`: le pagine cambiate senza ricaricare passano da `DOMParser`, che lo leggerebbe come
  HTML normale.
- **Galleria della home (slice 057, RIP-H-13):** lo scenario HTTP `home-galleria` (prima di `home`,
  `TestOrdineScenariHome`; legge e non apre schede) confronta, con il normalizzatore D-64, le celle
  di ogni busta (link della foto e della scheda, `alt`, `loading`, «+N», i dati), il riassunto, il
  messaggio della galleria vuota, le linguette e `vista=galleria` nei link, nei campi nascosti, in
  `#list-return` e nei redirect di `GET /cerca`. Il `src` delle miniature non si confronta: il
  gettone è diverso nelle due app. D-64 riconosce anche il testo alternativo «Foto N/AA»
  (`reNumeroStandalone`). Lo scenario browser `galleria-home` apre la galleria nelle due app, guarda
  che le miniature abbiano il gettone e arrivino (nessuna risposta 4xx o 5xx), apre il visualizzatore
  dalla foto, torna con «← Indietro» e passa fra le linguette; legge e non cambia niente.
- **Gettone della galleria (slice 057):** la galleria (`internal/web/galleria.go`) scrive una riga
  `repair.staff_gallery_viewed` per visita con gli id delle foto della pagina (`buste.RegistraGalleria`,
  in autocommit come le altre letture), e solo se la riga c'è firma ogni miniatura con il gettone di
  `foto.go` (utente, busta, foto, inizio del giorno UTC). Senza riga la pagina si mostra senza gettoni.
  Lo standalone riscrive la riga solo quando cambiano le foto o il giorno durante la vita del socket
  LiveView; il Go non ha socket e ne scrive una per visita, come `repair.index_viewed` (D-68).

- **Link storici /stato (slice 064):** il banco dà alle due app lo stesso segreto Flask **sintetico**
  (`segretoFlaskBanco` in `tools/banco/storica.go`, chiaramente finto): allo standalone con
  `LEGACY_WARRANTY_SECRET_FILE` (file `legacy_warranty_secret` dei segreti dell'istanza), al Go in
  `go-data/secrets/segreto_flask`. Il Caddy manda alla faccia pubblica del Go `/r/*`, `/stato/*`,
  `/foto/*` e `/thumb/*` (dalla 065 anche `/m/*` e `/privacy`). Il passo `token` dà il codice breve `SINT-4` alla 4/26 (aperta, diretta) e
  `SINT-3` alla 3/26 (revocata dal passo del link), tutte e due della cattura (la scheda non cambia).
  Gli scenari `pagina-storica-rifiuti` (rifiuti, 410, foto, conteggi del registro) e `pagina-storica`
  (pagine, zeri nel numero, poll, messaggio, B2B, consegnata) hanno le firme calcolate con quel segreto
  e la scadenza fissa 4102444800 (`TestFirmeScenariStorici` le ricontrolla); ognuno resta sotto i 10
  404 di `/stato` del suo indirizzo. La 1/26, l'unica diretta con foto, ha il link storico revocato dal
  passo del link: la foto servita con la firma giusta la provano i test Go; il banco prova i rifiuti e,
  dal registro, che la firma giusta di una foto della 2/26 (B2B) arrivi fino alla riga `denied`. I
  messaggi vanno sulla 27/26 (già al limite di 10 all'ora dopo `pagina-cliente-conversazione`: il banco
  confronta il rifiuto del limite). Il passo `solo_go` D-77 (firma di Flask su «04/26» per la busta
  «4/26») è l'ultimo, dopo i conteggi del registro.

## Schermate del manuale (slice 049)

Le schermate di `docs/manuale/img/` non si fanno a mano: le rifà il banco, sul solo Go, con i dati
sintetici, nelle due viste del livello browser (desktop 1440×1000, telefono 390×844 a 2 pixel per px).

    B=to-be-deleted-after-use/<cartella>/bin      # gestrip e banco compilati come per il banco
    sg docker -c "$B/banco su -nome man -immagine gestrip-standalone:fab-225 -gestrip $B/gestrip -dati sintetici-import"
    sg docker -c "$B/banco manuale -nome man"     # scrive i PNG in docs/manuale/img
    sg docker -c "$B/banco giu -nome man"

- **Elenco dichiarativo:** `bench/manuale/schermate.json`, una voce per PNG: `file` (nome stabile
  `NN-nome-desktop.png` o `NN-nome-telefono.png`, con NN il capitolo), `vista`, `ricetta`, `busta`
  (se la ricetta ne vuole una), `elemento` (selettore CSS: il ritaglio contiene tutti gli elementi che trova, per esempio `.flash-info, main`; senza, la finestra intera) e
  `descrizione`. `banco` lo legge in modo stretto (`leggiSchermate`); `TestSchermateDelRepo` vuole
  ogni ricetta presente nello script.
- **Script:** `tools/banco/browser/manuale.mjs`, montato al posto di `banco.mjs` nella stessa immagine
  `grgo-banco-browser` (niente immagine nuova: l'impronta non cambia). Container `grgo-<nome>-manuale`
  con le protezioni del livello browser, solo l'indirizzo del Go, solo l'accesso del titolare
  sintetico; scrive solo in `esito/manuale` dell'istanza. Ogni voce gira in un contesto nuovo (login
  compreso), nell'ordine dell'elenco: l'ordine conta, perché alcune ricette cambiano i dati (sceglie le
  code finte in «Stampanti», crea una busta con le foto, carica tre foto sulla 5/26, manda un foglio
  di etichette alla coda finta, firma la 6/26). Le foto sono disegni generati nello script.
- **Una volta per istanza:** come il livello browser, `banco manuale` vuole un'istanza nuova con
  `-dati sintetici-import` e rifiuta se in `runtime.json` c'è `browser_girato` o `manuale_girato`.
  Così lo stesso comando dà le stesse schermate.
- **Stabilità:** l'ora del timbro della firma si scrive fissa nella pagina (15/03/2026 10:30) prima
  della foto; il cursore e le animazioni sono spenti. Restano legati al giorno del giro solo l'anno
  (etichette QR, numeri «/26») e il numero della busta nuova. Nei ritagli i pulsanti flottanti si
  nascondono e la barra «← Indietro» non resta ferma in alto (coprirebbero l'elemento); il ritaglio
  prende anche quello che esce di lato dall'elemento.
- **Misure:** `banco manuale` copia i PNG solo se ci sono tutti e sono PNG veri, e stampa misure e
  peso. Sopra 250 KB per file o 3 MB in tutto stampa un avviso (non fallisce). Un PNG in
  `docs/manuale/img` che l'elenco non nomina si segnala, non si cancella.
- Chromium della base Playwright ha solo l'interfaccia inglese: i campi file nativi della scheda
  dicono «Choose Files» anche con `--lang=it-IT`, e i messaggi della fotocamera sono quelli inglesi del
  browser («Requested device not found»).
- **Fotocamera (slice 059):** il Chromium delle ricette non ha nessuna fotocamera (la ricetta
  `senza-fotocamera`). Le ricette in `CON_FOTOCAMERE` di `manuale.mjs` (oggi `fotocamera`) girano in un
  secondo Chromium con due fotocamere finte (`device-count=2`, il disegno di Chromium, senza QR, così
  la finestra resta aperta) e il permesso già dato. Nella foto il video cambia a ogni giro (il disegno
  si muove e ha il contatore del tempo).

## Configurazione della macchina

`/etc/gestrip/apparecchio.conf` (`-config` per un altro percorso), scritto dall'installer, fuori
dal database e dal backup (`internal/apparecchio`). Righe `chiave = valore`, commenti con `#`:

- `indirizzo_interno = https://host[:porta]`: link di reset e inviti (slice 011b);
- `indirizzo_pubblico = https://host[:porta]`: link cliente `/r/<token>` di una busta nuova
  (slice 031). Se manca, `serve` parte e la home, dopo il salvataggio, dice che il link non si
  può mostrare.

Stesse regole di forma per le due righe (https, host valido, porta 1-65535, niente percorso né
query, 443 omessa). Una chiave sconosciuta o ripetuta, o un indirizzo sbagliato, ferma l'avvio.
Il file si legge solo all'avvio. I link non si costruiscono mai sull'`Host` della richiesta.

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

- `-comandi-stampa <cartella assoluta>` (slice 046): la cartella di `lp`, `lpstat` e `lpoptions`
  della stampa diretta, al posto di `/usr/bin`. Il banco la fa puntare ai comandi finti di
  `tools/banco/stampa.go`, montati in sola lettura in `/stampa-finta`; il container del Go non ha
  CUPS. Non spegne nessun controllo, quindi niente avviso.

Le due opzioni dei controlli compaiono in `gestrip serve -h` e negli errori delle opzioni (li scrive il pacchetto
`flag`), non nella riga d'uso. È voluto: il repository è pubblico, quindi nasconderle non
servirebbe; conta che i messaggi di rifiuto non le nominino.

Uno stop (SIGTERM, SIGINT) che arriva mentre `serve` si avvia, prima di «in ascolto», chiude
pulito: riga `gestrip: fermato durante l'avvio` ed esce con 0. È uno stop solo se l'errore viene
**soltanto** dal contesto: lo stop insieme a un errore vero (per esempio una copia di backup
«da togliere a mano») resta un'uscita 1 con l'errore stampato, e così ogni altro errore dell'avvio.
Nel percorso d'avvio gli errori che possono venire dal contesto si avvolgono con `%w`, mai `%v`,
altrimenti lo stop diventa un errore. I test fermano l'avvio a metà con `inAvvio` (in processo) o
con `GESTRIP_TEST_AVVIO_FERMO=1` (processo figlio, `cmd/gestrip/figlio_linux_test.go`).

I limiti di vita e le attese dei figli dei test (`internal/figlitest`) si misurano sulla lettura
monotona del sistema (`clock.Parti`, `Partenza.Trascorso`, `clock.Resta`; slice 055d, dubbio 2 della
051b): un salto dell'ora (timesyncd all'avvio) non li accorcia né li allunga. `clock.Sistema.Adesso`
resta sull'ora del muro in UTC, che è quella da salvare e mostrare.

Un SIGTERM che arriva nei primissimi istanti (circa i primi 70 ms, mentre si inizializzano i
pacchetti, prima di `signal.NotifyContext`) non passa da `serve`: il processo muore per il segnale
(stato 143, nessuna riga). Per systemd, che lo ha mandato, è comunque una fine pulita.

## Stampa diretta CUPS (slice 046)

- Tutto passa da `internal/printing`: l'interfaccia `Printer` (`List`, `Print`), `CUPS` con i comandi
  e `Finta` per i test. Nessun altro pacchetto lancia `lp`.
- I comandi si lanciano **senza shell**, con il percorso assoluto (`/usr/bin`, o `-comandi-stampa`),
  gli argomenti separati, l'ambiente fisso (`LC_ALL=C`, `LANG=C`, `PATH=/usr/bin:/bin`, niente
  `HOME`) e un tempo massimo (10 s per gli elenchi, 30 s per `lp`). Il PDF va a `lp` sullo standard
  input: nessun file temporaneo con i dati.
- Una coda si passa a `lp` solo se `printing.CodaValida` (niente `-` in testa, niente spazi né `/`);
  le opzioni solo dopo `printing.LeggiOpzioni` (una per `-o`, `PageSize` e simili vietati: il formato
  lo calcola `FormatoPagina` dai millimetri del documento).
- «A buon fine» vuol dire `lp` uscito con 0 **e** la riga `request id is <coda>-<n> (… file(s))`; ogni
  altra risposta è un `Rifiuto`. Il lavoro non sta mai dentro una transazione: si manda prima, e solo
  un lavoro accettato scrive contatore e audit (in una transazione sola).
- **Un `gestrip serve` senza `-comandi-stampa` usa i comandi CUPS veri di `/usr/bin`**: su una
  macchina di sviluppo con stampanti vere (pc-fisso) va sempre avviato con `-comandi-stampa` verso
  una cartella di comandi finti (per esempio quelli di `tools/banco/stampa.go`), altrimenti
  «Stampanti» elenca e «🖨 Stampa» manda lavori alle code vere.
- Il tempo scaduto uccide tutto il gruppo di processi del comando (`Setpgid` e `Cancel` con
  `kill(-pid)`), anche i figli e i nipoti.
- **Nei messaggi per chi usa GestRip si dice «stampante», mai «CUPS»** (slice 055d, punto 5 della 049):
  «la stampante non risponde.», «Le stampanti non rispondono», «(non trovata nel computer)». CUPS resta
  nei registri, nel codice e nel glossario del manuale. Il segnaposto del campo «Opzioni» cambia con la
  stampante (`segnapostoOpzioni`): le prime due opzioni che la stampante scelta dichiara, con il loro
  valore di adesso; senza, un esempio per tipo («MediaTracking=Gap Darkness=8» per le etichette QR,
  «Darkness=12 PrintSpeed=3» per l'etichetta busta).
- **Mai una coda CUPS vera nei test né nel banco.** I test di `internal/printing` scrivono comandi
  finti (sh) in una cartella temporanea e controllano che `CUPS` punti lì; i test web usano
  `printing.Finta`; il banco monta i comandi finti. Su pc-fisso ci sono stampanti vere: niente prove a
  mano con `/usr/bin/lp`.

## Modo di lavorare: via per tappa

Stefano dà il via a una **tappa** (o a una parte di tappa), non alla singola slice. Nel via ci sono
cosa è autorizzato e dove fermarsi. Dentro la tappa si lavora una slice dopo l'altra, senza aspettare.

### Ogni slice

- Test prima rossi, poi il codice; `scripts/check` verde prima della review.
- Review indipendente, mutazioni (contano come uccise solo quelle che compilano).
- Ricevuta in `docs/slices/`, ripresa in `docs/handoff/` già committata, nota su Jarvis, push su origin.
- Nella ricevuta una sezione **«Per il manuale»** (Stefano, 03/11/2026): 3-8 righe in frasi semplici
  con i comportamenti visibili a chi usa il programma, introdotti o cambiati dalla slice (per
  esempio «al massimo 4 foto per volta», «Riprova»). È la base della slice del manuale a fine tappa
  (PIANO, «Manuale d'uso»). Se la slice non cambia niente di visibile, lo si scrive.
- Copie dell'inventario privato: solo come rete di sicurezza durante la slice, cancellate a fine
  slice (Stefano, 03/11/2026).
- Una riga in `~/Condivisi/GestRip-Go/avanzamento.md`: data e ora, slice, commit, voci chiuse e
  parziali, mutazioni, esito della review in breve, temporanei eliminabili.
- I temporanei usa e getta della slice si cancellano dopo il commit. Restano `history-backup/` e
  tutto ciò che contiene dati o copie di documenti; il README della cartella si aggiorna sempre.

### Chi fa cosa

- La sessione principale **coordina**: scrive i brief, decide le correzioni, scrive ricevuta e
  ripresa, fa commit e push.
- I sottoagenti lavorano, uno alla volta per slice, mai in parallelo sullo stesso codice:
  - **implementazione**: brief che sta in piedi da solo (voci, file da leggere, regole del metodo e
    del repo pubblico, cosa restituire: file toccati, test, dubbi, comportamenti misurati e solo
    ragionati);
  - **review**: un agente diverso, che riceve il codice e le voci, non le conclusioni;
  - **mutazioni e banco**: si possono affidare, l'esito si controlla.
- La sessione principale **rilancia di persona** `scripts/check` e il banco, legge `git diff --stat`
  e i punti critici. Un «verde» riportato da un sottoagente non basta.
- Le decisioni di prodotto non le prende mai un sottoagente.

### Quando ci si ferma comunque

- **Decisione di prodotto**: un comportamento non scritto nell'inventario, una differenza dallo
  standalone che non è una D-xx approvata, un caso dubbio. La domanda va in
  `docs/domande-aperte.md` con la raccomandazione; se il resto non dipende dalla risposta, si va
  avanti con il resto.
- **Bug dello standalone**: il caso, con la prova se misurata, va in `docs/domande-aperte.md`; non si
  copia e non si corregge nel Go senza decisione. Se tocca la sicurezza in produzione ci si ferma
  subito.
- **Sempre prima chiedere**: dati veri, produzione, otto-vps fuori da `~/git/gestrip-go.git`,
  servizi di pc-fisso, sudo, pacchetti di sistema, repo GitHub o pubblicazione.
- Una review con un problema **ALTA** che non si chiude nella stessa slice.
- `scripts/check` o il banco rossi senza una causa capita dopo un tentativo serio di diagnosi.
- Contesto oltre circa 600k: si finisce la slice in corso, si scrive la ripresa e si chiede una
  sessione nuova.
- Fine della tappa autorizzata.

### Resoconto quando ci si ferma

Breve, in italiano semplice: cosa è chiuso (voci e commit), cosa è parziale e perché, le domande
aperte con la raccomandazione, cosa serve da Stefano, percentuale delle voci v1 chiuse.
Niente controlli programmati e niente messaggi automatici da parte della sessione del porting.

## Regole del repository pubblico

- Nessun segreto, chiave o token, nemmeno di prova se sembra vero.
- Nessun dato reale: clienti, buste, foto, dettati, nomi di persone o negozi.
  Nei test e negli esempi solo dati inventati.
- Nei messaggi di commit nessun riferimento a dati veri.
- Autore dei commit: `Stefano Zamprogno <90012+L0cutus@users.noreply.github.com>`.
