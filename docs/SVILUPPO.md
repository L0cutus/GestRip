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
  dello standalone. Le email-optin/optout del link storico sono della 070 (sezione «Avviso email della
  pagina del cliente»), le telegram-optin/optout della 072 (sezione «Iscrizione Telegram della pagina del
  cliente»). La pagina riusa `pagina_cliente.html`: gli indirizzi delle foto, del poll e del modulo li
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
- **Popup «📲 Avvisa cliente» (slice 066):** sulla faccia interna `GET /repairs/{id}/avviso` e
  `POST /repairs/{id}/avviso` (`rottaConGuscio`, `personale`: il POST ridisegna il popup dopo un testo
  vuoto) e `POST /repairs/{id}/azzera-avviso` (`f.rotta`, `personale`), in `internal/web/avvisi.go`, con
  il dominio in `internal/buste/avvisi.go` (bozza, invio, link, avviso di ritiro), `modelli.go` (le nove
  famiglie `wa_*`, i predefiniti, `ErroreModello` = `format_error/2`) e `whatsapp.go` (`NumeroWhatsApp`,
  `IndirizzoWaMe`). Regole:
  - la GET **non scrive niente** (nemmeno il link: `{link}` resta il segnaposto) ed è la pagina del popup
    (`pagine/avviso.html`, `#notice-popup`); da dove si parte lo dice la query: dalla scheda il segno
    della scheda in `mostra` (non si consuma: lo usano «Annulla», che torna alla scheda senza
    `repair.viewed`, e il modulo), dalla lista `da=lista` con i filtri in `lista` (riscritti come
    `listaDelModulo`), dopo «Rispondi» `scopo=risposta`. Dalla lista una busta chiusa torna alla lista
    con «La busta è chiusa: niente è stato inviato.»;
  - il modulo rimanda nei campi nascosti ciò per cui il popup era disegnato (`buste.Disegnata`: scopo,
    tipo, canale, numero, controparte); `InviaAvviso` rilegge la busta e rifiuta senza scrivere se è
    cambiato qualcosa (i motivi e i testi di `NoticePopup.refusal/1`), poi il link in una transazione sua
    (`garantisciLink` = `ensure_public_link/2`), poi la riga WhatsApp (`registraConsegna`, con il timbro
    `link_inviato_il`) o, per l'avviso di ritiro, il blocco `avviso_pronta_il` con la riga, l'evento e
    `ready_notice.sent` in una transazione (`avvisaRitiro`). Senza `indirizzo_pubblico` un testo con
    `{link}` non parte (`ErrLinkNonSalvato`, prima di emettere il link);
  - riuscito: senza JavaScript il redirect a `wa.me` (la pagina stessa va a WhatsApp); con
    `Accept: application/json` il JSON `{"url": …}` con `no-store`. **D-74 (fatta per il popup):**
    `app.js` apre la scheda vuota al tocco, manda il modulo con `fetch`, e solo con il JSON (cioè dopo le
    scritture) manda la scheda vuota a wa.me e questa pagina alla lista ricordata (`gestrip:list-return`
    di `#list-return`); un rifiuto chiude la scheda vuota e mostra la pagina della risposta. Il link del
    pannello QR è della 068 (sotto); il lotto inviato della 091 e il reinvio del lotto della 093 (sezione dei lotti); gli
    altri link wa.me della D-74 (preventivo) arrivano con le loro slice; i solleciti agli artigiani con la 083 (sezione
    «Solleciti all'artigiano»);
  - il canale è sempre WhatsApp: `sceglieTelegram` è il punto che la 074 completa. Dalla 072 l'avviso del
    popup sul link Telegram che non porta a niente (`TelegramSpento`) segue `TelegramDisponibile`. Il numero dei
    testi è quello visibile (D-64);
  - con JavaScript il 📲 e «📲 Avvisa cliente» (`data-avviso`) chiedono la pagina del popup e ne mettono il
    riquadro sopra la pagina (`avviso-sopra`); «Annulla», Esc e un tocco fuori lo tolgono senza richieste.
  - **differenze dichiarate dallo standalone:** dopo l'invio la lista non mostra «Messaggio registrato in
    lista: WhatsApp si apre con il testo.» (come lo standalone dalla sua slice 225, dove il messaggio si
    perde con la navigazione); dopo «Rispondi» il popup è una pagina intera (`/repairs/{id}/avviso?scopo=
    risposta`), non un riquadro sopra la scheda: Esc e un tocco fuori non lo chiudono, «Annulla» sì (torna
    alla scheda con il segno);
  - il tipo rimandato dal modulo è controllato: uno di `ready`, `update`, `reply`, e `reply` solo con
    `scopo=risposta` (altrimenti il rifiuto di «tipo cambiato»).
- **«📲 Salva e avverti cliente» (slice 068, RIP-S-04, CLI-29):** nessuna rotta nuova. Il modulo di modifica
  (`POST /repairs/{id}/modifica`) ha il pulsante `#save-and-notify` (`name="then" value="notify"`) dopo
  «Salva modifiche», solo su una **riparazione** `pronta` con un numero WhatsApp della controparte
  (`datiModulo.SalvaEAvverti` = `@repair.status == "pronta" && @send_address`; la compro oro valutata no, come lo
  standalone). Salvato, `dopoSalvaEAvverti` (= `then_notify/2`) rilegge la bozza con `ApriAvviso` (non scrive) e,
  se la busta è ancora pronta e ha un numero, va al popup `/repairs/{id}/avviso?mostra=<segno>` con «Riparazione
  aggiornata.»; altrimenti (un `then` forgiato, una busta lasciata senza numero) è un salvataggio semplice. Un
  rifiuto ridisegna il modulo, mai il popup. Il popup è la pagina intera della 066, come dopo «Rispondi» (lo
  standalone lo apre sopra la scheda: differenza già dichiarata per la risposta, Dubbio della 068). Con
  JavaScript `data-invio-unico` spegne i pulsanti al primo invio e un pulsante spento non manda il suo valore:
  `app.js` copia nome e valore del pulsante premuto in un campo nascosto (`data-copia-pulsante`, tolto da
  `pageshow`) prima di spegnerlo.
- **Avviso di ritiro dal pannello del QR (slice 068, RIP-H-26, D-74):** dopo «✅ Valutata» (`DopoAvviso`,
  `POST /repairs/{id}/qr-avanza` con la conferma) `avvisaDopoValutazione` = `qr_announce/2`: lo stato è già
  avanzato; `buste.AnnunciaRitiro` = `ReadyNotice.announce/3` (numero → `ErrNessunRecapito`, testo della famiglia
  d'avviso → `ErrModelloRitiro`, link con `mettiLink` → D-46 estesa senza `indirizzo_pubblico`, poi il blocco
  `avvisaRitiro` di «Invia»). **Differenza dichiarata, senza D:** `AnnunciaRitiro` controlla subito anche il
  numero presente ma non componibile («n/d»), prima del testo e del link; lo standalone lo scopre dopo, dentro
  `notify/4`, quando ha già emesso il link della busta. Il messaggio è lo stesso («(no WA, telefono
  mancante)»), il Go non emette un link inutile. Registrato: la pagina della scansione (200, `no-store`, `data-qr-esito="ok"`) con
  «✓ valutata» e il riquadro `#qr-opening` («· avviso registrato», `#qr-send-opening` «↗ Apri WhatsApp»
  `target="_blank"` `data-qr-apertura`, `#qr-opening-consulta`), senza il pannello delle azioni. Non registrato:
  la lista di «Chiudi» con il messaggio dello standalone («✓ valutata (no WA, telefono mancante)» come info; «—
  già stata avvisata: …», «— è in un lotto ancora aperto: …», «— testo «…» non valido: …. Avviso non
  registrato.», «— avviso non registrato.» come errore) e, senza `indirizzo_pubblico`, «✓ valutata — avviso non
  registrato: manca «indirizzo_pubblico = …» …» (D-46 estesa, Dubbio della 068). Il secondo invio dello stesso
  modulo (busta già valutata) non avvisa di nuovo: «✓ valutata» e la lista. **D-74:** con JavaScript il tocco su
  «↗ Apri WhatsApp» apre wa.me nella scheda nuova e porta la pagina alla lista di «Chiudi»; «Chiudi» ed Esc su
  un riquadro con `#qr-opening` vanno alla lista vera (ricaricata) invece di togliere il riquadro (lo
  standalone ricarica la lista sotto il popup). Senza JavaScript il link resta com'è (scheda nuova).
- **«📨 Invii registrati» e «Togli dalla lista» (slice 067, CLI-37, RIP-S-21):** sulla faccia interna
  `POST /repairs/{id}/togli-invio` (`f.rotta`, `personale`, campo `invio`), in `internal/web/invii.go`, con
  il dominio in `internal/buste/invii.go` (`Scheda.Invii`, `TogliInvio`). La lista è il `<details
  id="repair-send-history">` chiuso sotto «Rispondi al cliente», solo con degli invii (anche senza più il
  numero): conteggio, una didascalia per canale presente (WhatsApp, email, Telegram, in quest'ordine), le righe
  dalla più recente per id con canale (« · all'artigiano»), stato e ora (`accettato_il`, o `accodato_il`),
  indirizzo (mai la chat di Telegram cliente; «contatto scelto sul telefono»), testo (link dell'email
  mascherati come `mask_links/1`, Telegram senza HTML come `plain_text/1`). «Togli dalla lista» solo su una riga
  WhatsApp senza avviso di ritiro; il dominio rifiuta in transazione una riga di un altro canale o di un'altra
  busta (`ErrInvioNonInLista`) e la riga di un avviso di ritiro (`ErrInvioAvvisoPronta`), poi `DELETE` e
  `outgoing.handoff_discarded` (canale, parte, busta; mai il testo). Il timbro «link inviato» resta. Messaggi
  dello standalone: «Messaggio tolto dalla lista.», «Questo messaggio non è più in lista.», «Questo è l'avviso
  di pronta: si annulla con «azzera avviso», non da qui.».
- **«Testi dei messaggi» (slice 067, CLI-38, ADM-13):** sulla faccia interna `GET /settings/templates`,
  `POST /settings/templates` («Salva il testo») e `POST /settings/templates/ripristina` («Ripristina
  predefinito», `formaction` dello stesso modulo, con `data-confirm`), tutte `rottaConGuscio` e
  `soloTitolare` (`internal/web/testi.go`, `pagine/testi.html`; scheda «Messaggi WhatsApp» delle
  Impostazioni, che dalla 067 è la prima scheda del Go: `/settings` va lì). Il dominio è in
  `internal/buste/modelli.go`: le **quindici** famiglie della pagina dello standalone (nove `wa_*`, due `tg_*`,
  quattro `email_*`) con etichette, campi, predefiniti ed esempi dell'anteprima; `ErroreModello` =
  `format_error/2` con le due regole delle email (`{link_conferma}` obbligatorio, oggetto su una riga);
  `SalvaModello` = `put/3` (casella vuota = predefinito, più di 4000 grafemi = `ErrModelloTroppoLungo`, testo non
  valido = `*ModelloRifiutato`, poi impostazione e `outgoing.template_saved` con `family`, `default`, `valid`
  in una transazione, mai il testo). I testi Telegram ed email si salvano già ma li compongono la T4.7 e la 069.
  Il modulo manda `\r\n`: il gestore lo fa `\n` prima di tutto (LiveView manda allo standalone il valore della
  casella con `\n`). **Differenza dichiarata:** l'anteprima è quella del testo disegnato (salvato, o rifiutato),
  non si aggiorna mentre si scrive (lo standalone la rifà a ogni tasto, `phx-change`): dubbio della 067.
- **«Provider di invio» (slice 069b, ADM-09, CLI-52 parte email, D-03, D-27):** `GET /settings/providers`, `POST
  /settings/providers/smtp` (`rottaConGuscio`), `POST /settings/providers/prova`, `POST /settings/providers/accensione`,
  `POST /settings/providers/sospese/{id}/scarta` e `…/manda` (`f.rotta`), tutte `soloTitolare`; scheda «Provider»
  delle Impostazioni (`schedaDelModello["provider"]`). Regole nella sezione dell'email, sotto. **Parte Telegram
  (slice 071, ADM-10):** `POST /settings/providers/telegram` (`rottaConGuscio`), `POST /settings/providers/prova-telegram`,
  `POST /settings/providers/notifiche-clienti`, `POST /settings/providers/accensione-telegram` (`f.rotta`), tutte
  `soloTitolare`; scarta e manda dei messaggi Telegram in coda sono le due rotte `sospese/{id}` di sopra. Regole
  nella sezione Telegram, sotto.
- **Popup senza `indirizzo_pubblico` (D-46 estesa, slice 067):** il popup ha `#notice-link-missing` («{link} e
  {telegram_optin} non si possono usare: manca «indirizzo_pubblico = …» in <file>, poi riavvia gestrip serve
  con «…». Togli {link} dal testo per mandarlo senza link.», `f.linkMancante`; l'ultima frase dalla 069c, e dalla
  071 nomina quello che il testo contiene: «Togli {telegram_optin} …» o «Togli {link} e {telegram_optin} …»); un testo con `{link}` o `{telegram_optin}` è `buste.ErrSenzaIndirizzoPubblico`
  (avvolge `ErrLinkNonSalvato`, prima di emettere il link) e il popup si ridisegna con il testo e «Messaggio
  non inviato: manca …», come dopo un testo vuoto. Niente si scrive.
- **Link Telegram dei messaggi (slice 066, completato con la 072):** sulla faccia pubblica
  `GET /r/{token}/telegram` (`rottaPubblica`, famiglia `/r/` del limite), quello che `{telegram_optin}`
  diventa all'invio, come `telegram_link/2`: link che non apre niente o busta dell'oreficeria (anche chiusa)
  il 404 «Riparazione non disponibile»; busta chiusa la sua pagina con «Questa riparazione non riceve più
  avvisi.»; canale non disponibile 503 `text/plain; charset=utf-8` «Servizio notifiche temporaneamente non
  disponibile.» con le intestazioni pubbliche, senza scrivere niente; oltre il limite 429 in testo semplice;
  altrimenti un token nuovo di 60 minuti e il 302 a t.me (sezione «Iscrizione Telegram della pagina del
  cliente», sotto).
- **Popup «📲 Notifica Telegram al cliente» (slice 074, CLI-47, RIP-H-18):** sulla faccia interna
  `POST /repairs/{id}/notifica-telegram` (`f.rotta`, `personale`; campi `notifica`, il gettone del popup, e
  `azione`, `invia` o `annulla`), in `internal/web/telegram_avvisi.go`. Il popup non ha una rotta sua: lo
  disegna `mostraConInfo` sulla pagina dopo il cambio di stato (sezione «Avvisi Telegram», sotto).
- **Anagrafica degli artigiani (slice 076, ART-01…04):** sulla faccia interna `GET /settings/anagrafiche`,
  `POST /settings/anagrafiche/artigiani` e `POST /settings/anagrafiche/artigiani/{id}` (`rottaConGuscio`: ridisegnano la
  pagina dopo un rifiuto, con i valori scritti) e `POST /settings/anagrafiche/artigiani/{id}/elimina` (`f.rotta`, sempre
  un redirect alla pagina), tutte `personale` come la live_session dello standalone (`internal/web/anagrafiche.go`,
  dominio in `internal/buste/artigiani.go`). «Profilo negozio» (ADM-02) compare solo al titolare quando
  `GET /settings/shop` è registrata (dalla 087 lo è sempre: sotto); le oreficerie clienti sono della 086 (sotto). Il
  telefono si normalizza con `telefono.Normalizza` (`internal/telefono`, dalla slice 104; prima `buste.NormalizzaTelefono`) (= `normalizza_telefono`, cifre di Python);
  il doppione è a maiuscole esatte alla creazione e con `LOWER` (solo ASCII) alla modifica, come lo standalone.
- **Oreficerie clienti nella stessa pagina (slice 086, ORE-01…03):** `POST /settings/anagrafiche/oreficerie` e
  `POST /settings/anagrafiche/oreficerie/{id}` (`rottaConGuscio`, `personale`, `internal/web/anagrafiche.go`, dominio in
  `internal/buste/oreficerie.go`: `Oreficerie`, `CreaOreficeria`, `ModificaOreficeria`, tipo `OreficeriaCliente` perché
  `Oreficeria` è già quella della scheda). Il riquadro `#oreficerie` sta **sopra** quello degli artigiani, come nello
  standalone; i campi sono `oreficeria[name]` e `oreficeria[phone]` (stessi ID dello standalone: `oreficerie-<id>`,
  `edit-oreficeria-<id>`, `oreficeria-name-<id>`, `new-oreficeria-form`, `oreficerie-empty`). La riga 1 («il negozio
  stesso», portata dall'import) non si elenca e non si modifica: per la modifica è un id che non c'è, «Oreficeria non
  trovata o non modificabile.». Il doppione in creazione è l'indice unico (a maiuscole esatte, vede anche la riga 1);
  in modifica è `LOWER` (solo ASCII) fra le sole esterne (id > 1), poi l'indice unico per il nome esatto, negozio
  compreso. **Non c'è nessuna rotta di eliminazione** (ORE-03: quella voluta dopo la v1 è un'altra funzione) e le
  oreficerie create dal banco restano nei menu delle altre pagine fino alla fine del giro: lo scenario browser
  `anagrafica-oreficerie` usa nomi propri per vista che cadono in fondo («Zz Banco 086 desktop» e «… telefono»).
  La riga 1 («Negozio», senza telefono) la semina la migrazione `0020_negozio_oreficeria.sql` con
  `INSERT OR IGNORE`, come quella dello standalone: su un database nuovo la prima oreficeria creata prende l'id 2; su un
  database che ha già l'id 1 (un import, o un'altra riga) non cambia niente. L'import (`importer.Importa`) toglie la
  riga seminata prima di copiare quella dello standalone (nome e telefono suoi), altrimenti l'INSERT dell'id 1
  andrebbe in conflitto. Nei test la riga 1 con un nome proprio si scrive con `INSERT OR REPLACE`.
- **Profilo del negozio (slice 087, ADM-02):** sulla faccia interna `GET /settings/shop` e `POST /settings/shop`
  (`rottaConGuscio`, `soloTitolare`, `internal/web/negozio.go`, `pagine/negozio.html`; dominio in `internal/buste/profilo.go`:
  `ProfiloPerModulo`, `SalvaProfilo`). La POST è l'evento `save` di ShopLive: campi `shop_profile[<campo>]` con i nomi dello
  standalone (`shop_name`, `subtitle`, `legal_name`, `fiscal_id`, `tax_code`, `address`, `postal_code`, `city`, `province`,
  `phone`, `email`, `sales_email`, `opening_hours`, `instagram`, `facebook`, `website`, `warranty_terms`; id `shop_profile_<campo>`,
  form `#shop-form`). Riuscito: 302 alla pagina con «Impostazioni salvate. I documenti emessi restano invariati.»; rifiutato: pagina
  ridisegnata (200) con l'errore sotto il campo (`Campo obbligatorio.`, `Troppo lungo (massimo N caratteri).`, «codice fiscale non
  valido: 16 caratteri, o 11 cifre per una società») e i valori del changeset; un modulo senza `shop_profile[…]` è «Richiesta non
  riconosciuta: nessuna impostazione salvata.»; un guasto del database «Impostazioni non salvate: riprova.» (e una riga nel
  registro senza il testo del profilo). `SalvaProfilo` porta il changeset di Ecto com'è (`ShopProfile.changeset/2`): `cast` (campo che
  non arriva = non cambia; vuoto o di soli spazi = NULL; è un cambio solo se diverso dal salvato), `trim_strings` solo sui cambi,
  `validate_required` di nome e garanzia, `validate_length` in grafemi solo sui cambi (255 per nome, ragione sociale, indirizzo,
  email, sottotitolo, social e sito; 20 CAP e provincia; 120 città; 80 telefono; 40 identificativo fiscale; 500 orari; la garanzia
  senza limite), codice fiscale senza spazi e in maiuscole (`String.upcase`: ß → SS). Crea la riga `default` la prima volta, poi la
  riscrive solo nei campi cambiati (senza cambi niente UPDATE e `aggiornato_il` resta); la riga d'audit `shop_profile.updated` (risorsa
  `shop_profile`) c'è a ogni salvataggio, anche alla creazione, nella stessa transazione. Le righe della garanzia arrivano con «\r\n»
  e si salvano con «\n» (`aCapoUnix`, come le altre caselle di LiveView). **Chi legge il profilo** lo legge dalla tabella
  `profilo_negozio` e vede subito il salvato: `LeggiProfiloNegozio` (`/m/`, `/privacy`), `nomeNegozio` (intestazione della home, della
  nuova busta, delle scelte), `leggiProfiloAvviso` (`{negozio}`, `{citta}`, `{orari}`, `{social}` dei messaggi), ricevuta, scheda PDF e
  report. **Il manifest e il titolo sotto l'icona (`apple-mobile-web-app-title`) dicono ancora «Negozio»**: il nome dal profilo lo porta
  ADM-07 (T4.15). La pagina ha il profilo e, dalla slice 102, la stima dei tempi: operatività, logo, colori della barra e accessi
  (ADM-03, 05…07 e gli accessi all'installazione) sono di altre slice. `/settings` va alla prima scheda, che ora è «Negozio». Banco: HTTP
  `negozio` e `negozio-dipendente`; browser `profilo-negozio`, che cambia il profilo e **alla fine lo rimette com'era** (i 17 valori
  riscritti dal modulo e riletti), perché gli altri scenari lo leggono; sul database vuoto guarda solo la pagina.
- **Consegne all'artigiano (slice 077, ART-05…08, 10, 11):** sulla faccia interna `GET /handovers` (pulisce le
  consegne vuote dei giorni prima, poi la lista), `POST /handovers` («Apri sessione»), `GET /handovers/{id}` e
  `POST /handovers/{id}/aggiungi` (`rottaConGuscio`: la lista e la pagina si ridisegnano dopo un rifiuto, con la scelta
  o il codice scritto), `POST /handovers/{id}/buste/{busta}/togli` e `POST /handovers/{id}/conferma` (`f.rotta`, sempre
  un redirect), tutte `personale` (`internal/web/consegne.go`, dominio in `internal/buste/consegne.go`). «Oggi» è il
  giorno del negozio dalla sua mezzanotte (`clock.Data.Inizio`). Il numero scritto è quello dell'etichetta (D-64,
  `LeggiRicercaEtichetta` e `RicercaEtichetta.Trova`, il numero interno solo per le buste senza etichetta), poi il QR
  (`righeConCodice`, fra più buste la più recente). Fra più buste con il numero scritto contano solo quelle che si
  possono dare all'artigiano; con più e senza anno, quella dell'anno in corso; se ne restano più d'una, il rifiuto
  solo del Go «Più buste corrispondono a «X»: …» (`ErrPiuBuste`; Dubbio 1 della ricevuta 077). Le stampe della consegna (078), lo scanner (079) e i link a
  storico e solleciti (082, 083, compaiono quando le loro rotte ci sono) arrivano dopo. Banco: HTTP `consegne`,
  browser `sessioni-consegna` (gira dopo `pannello-qr`: la busta confermata resta fuori bottega).
- **Scanner continuo della consegna (slice 079, ART-09):** «📷 Scansiona» (`#handover-scan-button`, solo a sessione
  aperta) usa il lettore QR di `app.js` della home e di «Nuova riparazione» nel **modo continuo** (`data-scansione`):
  nessuna seconda finestra, nessun secondo caricamento di jsQR. Ogni codice letto va a `POST /handovers/{id}/scansione`
  (`f.rotta`, `personale`, `internal/web/consegne_scansione.go`), che risponde in JSON `{ok, message, count, info}` (la
  reply di `add_scanned`, più `info` per il tipo del messaggio): prima il QR e poi il numero (`buste.LetturaScansione`),
  `ok` solo per una busta appena aggiunta (il suono), nessun messaggio nella sessione e nessuna riga d'audit. Le
  regole del modo continuo (stesso codice non ripetuto finché inquadrato, un codice alla volta, rete che non risponde)
  sono quelle di `qr_scanner.js` della 186 dello standalone. Alla chiusura, dopo almeno una scansione, `app.js`
  aspetta la scansione ancora senza risposta (al più 5 s), rilegge la pagina di `data-ricarica` e la mette al posto
  di questa (`mostraPagina`) con sopra solo l'ultimo errore (o quello che la pagina aveva già) e i campi di testo
  com'erano: in LiveView la pagina sotto la finestra si ridisegna da sé, e il flash verde si chiude al tocco su
  «Chiudi» (`FlashAutoClose`), il rosso resta. Login o 403 durante le scansioni: una navigazione alla pagina della
  sessione. Una risposta tardiva suona comunque (`qr-outcome` dello standalone). Lo stesso modo lo usa lo scanner del lotto (ORE-07, sotto, slice 090). Banco: la prova `scanner` di `lettore-qr` con la fotocamera finta `consegna.y4m`.
- **Rientro dall'artigiano dalla scheda (slice 080, ART-13, RIP-S-16):** `buste.RientraDallArtigiano`
  (`internal/buste/rientro.go`, = `return_from_artisan/4`) fa l'atto intero in una transazione: esito (`fatto`,
  `da_rifare`, ""; un'altra parola è «Esito lavoro non valido.» prima di ogni scrittura), timbro con la condizione di
  fuori bottega nel `WHERE` (fuori dal cestino; due rientri insieme: uno timbra, l'altro «La riparazione non risulta
  fuori bottega.»), nota senza spazi ai bordi (vuota: cancellata), esito controllato sul grafo («Da «X» non si passa a
  «Y»: scegli un altro esito lavoro.», `*EsitoNonConsentito`, tutto torna indietro; lo stesso stato scrive solo la data
  di pronta vuota), chiusura della consegna con l'ultima busta fuori (`chiudiConsegnaSeCompleta`, la stessa del
  rientro automatico dal QR della 054), una riga `repair.returned_from_artisan` (`handover_id`, `handover_closed`,
  `outcome`, `status`, `artisan_notes` vero/falso, mai la nota) e nessuna `repair.status_changed`. **L'email di pronta
  parte nella stessa transazione** (`avvisaPronta`, sezione 070b) solo se la busta non era già «pronta» prima del
  rientro; lo standalone la accoda dopo il commit. Una busta nel cestino non tiene aperta la consegna, né nel rientro
  manuale né in quello automatico (come `Repo.prepare_query/3` dello standalone; prima della 080 il QR la contava).
  Scheda: `#repair-handed-over` dopo i dati (`internal/web/rientro.go`, `scheda.html`), il modulo in un `<details>`,
  l'avviso `return_warning/1` da `buste.EsitiApplicabili`, «🖨 Ristampa» e «📤 Condividi» (`data-share-pdf`)
  dell'etichetta; dopo il rientro `#repair-artisan-notes`. `POST /repairs/{id}/rientro` (`personale`,
  `return[stato_lavoro]`, `return[note_artigiano]` con `aCapoUnix`): un rifiuto o un errore ridisegnano la scheda con
  il messaggio e il modulo aperto con l'esito e la nota mandati (`rientroRifiutato`). Nessun popup Telegram dopo il
  rientro, come lo standalone. Banco: HTTP `rientro-scheda` (la 4/26 fuori e la 1/26 in bottega, solo lettura: nello
  standalone il rientro è un evento LiveView), browser `sessioni-rientro` (dati propri: una busta nuova per vista, del
  2025, una consegna di «Incastonatore Sintetico», rientro con «Fatto», la consegna chiusa; alla fine la busta senza
  artigiano, così il conto delle buste dell'artigiano che `anagrafica-artigiani` guarda sul telefono non cambia).
- **Lotti d'avviso alle oreficerie (slice 088, ORE-04, ORE-05, D-94):** sulla faccia interna `GET /ready-lots` (pulisce
  i lotti aperti vuoti dei giorni prima, poi la lista) e `POST /ready-lots` («Apri lotto», campo `lot[oreficeria_id]`),
  tutte e due `rottaConGuscio` e `personale` (`internal/web/lotti.go`, dominio in `internal/buste/lotti.go`). «Oggi» è il
  giorno del negozio dalla sua mezzanotte (`inizioDiOggi`, come le consegne); aperto = né inviato né chiuso. La pulizia
  risparmia ogni lotto con una busta, anche nel cestino (`NOT EXISTS`, non `NOT IN`); il conto delle buste nella lista
  esclude il cestino. Apertura: id ≤ 1 «…oreficeria principale.», id che non c'è «Oreficeria non trovata.», poi il lotto
  aperto di oggi più recente o uno nuovo (gettone di 24 byte casuali, base64 url-safe senza «=», 32 caratteri; riga
  `ready_lot.opened` con `oreficeria_id`) **nella stessa transazione di scrittura** (D-94: due aperture insieme danno un
  lotto solo). Riuscita: redirect a `/ready-lots/{id}` (la pagina del lotto, dalla 089); rifiuto: la lista
  ridisegnata (il valore rifiutato non è mai nel menu); guasto: la pagina d'errore con la riga nel registro. Il link «Storico dei lotti
  inviati» compare con la sua rotta (093). Banco: HTTP `lotti` e `lotti-dipendente`, browser `lotti-apertura` (oreficeria
  propria per vista, lotto lasciato aperto e vuoto; dalla 089 l'apertura arriva alla pagina vera del lotto).
- **Pagina del lotto (slice 089, ORE-06, ORE-08, ORE-09):** sulla faccia interna `GET /ready-lots/{id}`,
  `POST /ready-lots/{id}/aggiungi` («Numero busta», `bag[numero_busta]`), `POST /ready-lots/{id}/qr` (il campo `qr`),
  `POST /ready-lots/{id}/buste/{busta}/aggiungi` («Aggiungi» di una candidata), tutte `rottaConGuscio` (un rifiuto
  ridisegna la pagina con il numero scritto), e `POST /ready-lots/{id}/buste/{busta}/togli` («Rimuovi», `f.rotta`, sempre
  un redirect); tutte `personale` (`internal/web/lotto.go`, dominio in `internal/buste/lotto_buste.go`). Un lotto che non
  c'è torna alla lista con «Lotto non trovato.». L'aggiunta legge il QR (codice normalizzato, la più recente), poi il
  numero (D-64, come la consegna: fra più buste contano quelle che possono entrare nel lotto, poi l'anno in corso, poi
  il rifiuto solo del Go «Più buste corrispondono a «X»: …»), poi l'id; **una sola scrittura decide**: l'UPDATE
  condizionato chiede insieme busta da avvisare (`condizioneDaAvvisare`, la stessa dell'avviso singolo), in nessun
  lotto, di questa oreficeria, non compro oro, fuori dal cestino e lotto ancora aperto; i rifiuti si chiedono solo
  dopo, nell'ordine dello standalone («già avvisata» prima dei due del lotto). Riga `ready_lot.bag_added` /
  `ready_lot.bag_removed` (`repair_id`) nella stessa transazione. «Busta già in questo lotto.» è un esito, non un
  errore. Un lotto inviato o chiuso è in sola lettura (niente aggiunte, «Rimuovi», candidate) con «Questo lotto è
  partito il …» e il link pubblico `indirizzo_pubblico + /lotto/<gettone>` (la pagina pubblica è della 092), o «Link
  storico non riattivato durante l'importazione.». Dalla scheda, «Apri il lotto» nel riquadro dell'avviso di una busta
  in un lotto aperto. Lo scanner continuo è sotto (090), «📤 Chiudi e invia» sotto (091). Banco: HTTP `lotto-pagina`
  (lotto che non c'è, id non valido, il lotto inviato e chiuso della catena degli stati), browser `lotti-buste`.
- **Scanner continuo del lotto (slice 090, ORE-07):** «📷 Scansiona» (`#ready-lot-scan-button`, solo a lotto aperto, fra il
  campo del codice e «Aggiungi dal QR») usa lo stesso lettore di `app.js` della consegna nel modo continuo
  (`data-scansione="/ready-lots/{id}/scansione"`, `data-ricarica="/ready-lots/{id}"`, `data-count-label="nel lotto"`): nessun
  JavaScript nuovo. Ogni codice letto va a `POST /ready-lots/{id}/scansione` (`f.rotta`, `personale`,
  `internal/web/lotto_scansione.go`), l'evento `scan_continuous` di ReadyLotLive, che risponde in JSON `{ok, message, count,
  info}` come la consegna (`rispondiEsito`). L'aggiunta è quella del campo (`AggiungiAlLotto`, con il rifiuto «già avvisata»
  che vince, la D-95 e la riga `ready_lot.bag_added` con l'utente): prima il QR e poi il numero (solo «Riparazione non
  trovata.» passa alla seconda lettura). `ok` solo per una busta appena aggiunta («✓ Aggiunta busta #0045/26», `info`
  vero, D-64); «↻ Busta già in questo lotto» ha `ok` e **`info` falsi** (nello standalone il flash di `scan_continuous` è
  sempre rosso, a differenza del campo: alla chiusura la pagina lo mostra come ultimo errore); un codice di nessuna busta
  «Riparazione non trovata. («X»)»; gli altri rifiuti il testo del campo senza il codice; `count` le buste del lotto
  rilette (anche dopo un rifiuto; 0 per il lotto che non c'è, «Lotto non disponibile.»). Un id non valido è 404. La lista e il conto sotto la
  finestra si rileggono solo alla chiusura (D-98, gemella della D-90); la gemella della D-91 non serve (nel lotto dello
  standalone `add` e `add_scanned` non mandano `qr-outcome`: il campo non suona in nessuna delle due app). Banco: la
  prova `scansione` di `lettore-qr` con `lotto.y4m`, che ha la sua oreficeria e la sua busta («Zz Banco 090», QR
  `SINT-LOTTO-090`, `CODICE_LOTTO` in `banco.mjs`).
- **«📤 Chiudi e invia» del lotto (slice 091, ORE-10):** il riquadro `#ready-lot-send` (solo a lotto aperto con almeno
  una busta fuori dal cestino, dopo «Buste nel lotto») ha un modulo `POST /ready-lots/{id}/invia` (`rottaConGuscio`,
  `personale`, `internal/web/lotto_invio.go`, dominio in `internal/buste/lotto_invio.go`) con la domanda dello
  standalone in `data-confirm` e, senza un numero WhatsApp dell'oreficeria, `#ready-lot-send-no-number`.
  `buste.InviaLotto` = `send_lot/2`, **tutto in una transazione di scrittura**: prima l'UPDATE condizionato che segna il
  lotto inviato (aperto, con una busta visibile, nessuna busta di un'altra oreficeria o di nessuna, **cestino
  compreso**; riaccende `accesso_pubblico_spento_il`), poi il testo `wa_oreficeria_lotto` (`{oref_nome}`, `{n_buste}`,
  `{link}` = `indirizzo_pubblico + /lotto/<gettone>`, `{negozio}`), poi ogni busta fuori dal cestino, dalla più
  recente, con la porta dell'avviso singolo (`avvisaRitiroIn`, la parte in transazione di `avvisaRitiro`): testo
  «Avviso oreficeria» con il link **del lotto** (nessun link della busta, niente `repair.public_link_issued`), riga
  WhatsApp all'oreficeria con il suo numero o `contatto-scelto`, evento «notified», `ready_notice.sent`; infine
  `ready_lot.sent` (`{"bags": N}`). Il lotto si segna prima delle buste (l'avviso singolo rifiuta una busta di un
  lotto aperto); un rifiuto o un errore, anche a metà (gancio di prova `dopoAvvisoNelLotto`), annulla tutto. Rifiuti
  (i testi di `refusal_message/1` e `member_refusal_message/1`): «Lotto vuoto: aggiungi almeno una busta.», «Lotto
  già inviato.» (anche chiuso), «Busta #0045/26: non è più di questa oreficeria / non è più pronta / è già stata avvisata.
  Niente è partito.» (il numero visibile della busta, D-99; lo standalone scrive l'id), «Il testo «…» non è valido: …. Niente è partito.»
  (`ErrModelloLotto`, prima il testo del lotto), e senza `indirizzo_pubblico` un testo con `{link}` «Lotto non
  inviato: manca … Niente è partito.» (D-46 estesa; senza `{link}` il lotto parte e il link è il solo percorso). Un
  lotto che non c'è torna alla lista con «Lotto non trovato.». Riuscito: la pagina (200, non un redirect, come la
  LiveView con `@sent`) con «Lotto inviato: apri WhatsApp per mandarlo all'oreficeria.» e `#ready-lot-sent` (il testo
  in `#ready-lot-sent-message`, «Apri WhatsApp» `#ready-lot-sent-whatsapp` solo con un numero, «👥 Scegli contatto»
  `#ready-lot-sent-choose` sempre verso `wa.me/?text=`, `#ready-lot-sent-no-number`, il link). **D-74:** i due link
  hanno `target="_blank"` e `data-wa-lista`: con JavaScript il tocco apre WhatsApp e porta la pagina alla lista
  ricordata (`gestrip:list-return`), come `wa_home.js` dello standalone; le scritture sono già fatte quando i link si
  vedono, quindi niente coda in uscita al tocco. Riaperta, la pagina dice «Questo lotto è partito il …». Guasto: la
  pagina d'errore con «lotto non inviato». Banco: browser `lotti-invio` (dati propri «Zz Banco 091 <vista>», i due
  testi senza `{link}` per tutta la durata, rimessi predefiniti alla fine) e i due campi dell'invio nel passo del
  lotto inviato di `lotto-pagina`.
- **Pagina pubblica del lotto (slice 092, ORE-14, ORE-15, ORE-16):** sulla faccia pubblica `GET /lotto/{token}` e
  `GET /lotto/{token}/buste/{busta}/foto/{foto}` (`rottaPubblica`, `internal/web/lotto_pubblico.go`,
  `pagine/lotto_pubblico.html`, `app.css` `.lotto-pubblico*`; dominio in `internal/buste/lotto_pubblico.go`). Il lotto
  si cerca per **uguaglianza esatta** del gettone, solo inviato e con `accesso_pubblico_spento_il IS NULL`
  (`get_public_lot/1`; domanda G, 17/11/2026): nessun controllo di lunghezza o alfabeto oltre «non vuoto», così i
  gettoni di 32 caratteri del Flask importati funzionano e i `disabled-import-…` (accesso spento) danno 404. Ogni
  rifiuto della pagina è il 404 «Lotto non disponibile» di `RifiutoPubblico` (famiglia `lotto` del limite: 30 falliti
  in un minuto da un indirizzo, 15 minuti di blocco); la pagina ha `no-store` e `no-referrer`, niente X-Robots-Tag,
  niente audit. Mostra il nome del negozio, «Buongiorno <oreficeria>», «N riparazioni pronte al ritiro · lotto del
  <giorno del negozio>», «Totale lotto» con la somma dei soli importi esatti (se almeno una busta ne ha uno, anche
  zero) e «(1 senza importo)» / «(N senza importo esatto)», lo stato («✓ Lotto completato…», «N ancora da ritirare
  su M», «Il lotto non è ancora completato.», «Nessuna busta in questo lotto.»), le buste ancora dell'oreficeria del
  lotto e fuori dal cestino dalla più recente (numero D-64, «pronta <data>», prima foto per l'oreficeria come
  miniatura `?size=thumb`, descrizione oltre 100 grafemi tagliata, «✓ Pronta» / «✓ Ritirata <data>», prezzo finale
  con `Money.label` o preventivo com'è scritto) e il piè col profilo del negozio (087: nome, indirizzo CAP città,
  telefono `tel:`). **Leggere la pagina chiude il lotto completo** (ORE-15): `chiudiLottoCompleto` della slice 212 in
  una transazione di scrittura, una volta sola, con le buste nel cestino che contano; il riquadro segue `chiuso_il`
  riletto. È una scrittura su una GET (anche HEAD), come lo standalone; solo del Go, senza effetti visibili, una
  lettura prima dice se il lotto si chiuderebbe, e solo allora si prende la connessione di scrittura. Le foto: la busta dev'essere del lotto, ancora
  della sua oreficeria e fuori dal cestino (`member/2`), la foto visibile alla colonna dell'oreficeria (mai quella del
  cliente); `?size=thumb` esatto dà la miniatura; ogni rifiuto il 404 «Foto non disponibile» (non contato dal limite);
  la riga `repair.public_photo_viewed` (`audience` «orefice») solo quando la busta è del lotto. Banco: HTTP
  `lotto-pubblico` (`in_fondo`, gettone `lottoBanco` del passo `token`).
- **Storico dei lotti e reinvio (slice 093, ORE-11, ORE-12, ORE-13):** sulla faccia interna `GET /ready-lots/history`,
  `GET /ready-lots/history/{id}`, `POST /ready-lots/history/{id}/prepara` e `POST /ready-lots/history/{id}/reinvia`, tutte
  `rottaConGuscio` e `personale` (`internal/web/lotti_storico.go`, `pagine/lotti_storico.html`; dominio in
  `internal/buste/lotti_storico.go`). La lista (`StoricoLotti` = `list_sent_lots/0`): gli ultimi 100 lotti inviati, chiusi
  compresi, **per id decrescente** (non per data di invio), con «lotto #N · N buste · inviato …» (buste fuori dal
  cestino, «buste» anche per una), «Completato il …» o «N da ritirare» (fuori dal cestino, non consegnate), «Apri»;
  «Nessun lotto inviato.»; nessuna eliminazione. Il link «Storico dei lotti inviati» di `/ready-lots` segue `haRotta`.
  Il dettaglio (`LottoDelloStorico` = `fetch_sent_lot/1`): «Lotto #N · inviato … · completato …», il link pubblico
  (`indirizzo_pubblico + /lotto/<gettone>`, lo stesso dell'invio e della pagina del lotto; senza indirizzo il solo
  percorso) o «Link storico non riattivato durante l'importazione.», le buste (`BusteDelLotto`, che dalla 093 legge anche
  `Ritirata`) con il numero D-64, la descrizione oltre 60 grafemi tagliata, «Ritirata», «Apri busta». Un lotto aperto,
  che non c'è o un id non valido: redirect allo storico con «Lotto non trovato nello storico.». Il reinvio (riquadro
  `#ready-lot-resend` solo con `Reinviabile`: inviato, non chiuso, link acceso): «Prepara di nuovo il messaggio»
  (`PreparaReinvio` = `resend_message/1`, **non scrive**) rifiuta con «Lotto non disponibile.», «Tutte le buste del lotto
  sono state ritirate.» (nessuna busta fuori dal cestino da consegnare, comprese quelle passate a un'altra oreficeria),
  «Nessuna busta del lotto è ancora di questa oreficeria: niente da reinviare.», «Il testo «…» non è valido: … Niente è
  partito.» e senza `indirizzo_pubblico` con `{link}` «Messaggio non inviato: manca …» (D-46 estesa, la frase del
  popup); riuscito ridisegna il dettaglio (200) con «Messaggio pronto: …», il testo `wa_oreficeria_lotto` (buste di
  `public_members/1`), «↗ Apri WhatsApp» (solo con un numero) e «👥 Scegli contatto», moduli `data-avviso-invio` con
  `reinvio` (la chiave) e `via`. Il testo preparato sta nel **deposito in memoria** `depositoReinvii` (il
  `resend_pending` del socket): chiave casuale legata a utente, accesso (nonce) e lotto, 30 minuti, al più 500. Il tocco
  (`reinviaLotto`): la prima volta `RegistraReinvio` = `record_resend/4` in una transazione che rilegge il lotto (gli
  stessi rifiuti), la riga WhatsApp «accepted» all'oreficeria **senza busta** (`busta_id` NULL, `registraConsegna` con
  `repair_id: null` in `outgoing.handed_off`) col testo preparato e il numero, o `contatto-scelto` da «Scegli contatto» o
  senza numero, e `ready_lot.resent` (`{"bags","message_id"}`); non tocca lotto, gettone, data di invio né avvisi. Poi
  wa.me: con JavaScript il JSON `{"url"}` (D-74, la pagina va alla lista), senza il redirect. Un secondo tocco sulla
  stessa chiave (anche insieme: decide il deposito) riapre WhatsApp e non registra; un rifiuto al tocco consuma la
  chiave e ridisegna il dettaglio; una chiave che il deposito non ha (inventata, di un'altra sessione, persa con un
  riavvio, scaduta) ridisegna il dettaglio con «Prepara di nuovo il messaggio: niente è stato registrato.» (D-101,
  ricevuta 093). Banco: HTTP `lotti-storico` (lotto 1 della catena, prima di `lotto-pubblico`), browser
  `lotti-reinvio` (i lotti della 091).
- **Promemoria e job (slice 098, JOB-05, JOB-06):** sulla faccia interna `GET /settings/promemoria` e `POST
  /settings/promemoria` (evento `save`, `rottaConGuscio`: un rifiuto ridisegna) e `POST /settings/promemoria/esegui` (evento
  `run`, `f.rotta`, sempre redirect alla pagina), tutte `soloTitolare` (`internal/web/promemoria.go`,
  `pagine/promemoria.html`; dominio in `internal/jobs/pagina.go`). Scheda «Promemoria». Dettagli nella sezione «Pagina
  Promemoria e job».
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

## Segreto dello standalone e link dell'artigiano (slice 084, D-87)

- **Dove:** `secrets/segreto_standalone` accanto al database (`/var/lib/gestrip/secrets/segreto_standalone`
  sull'apparecchio): il `secret_key_base` dello standalone, letto da `gestrip serve` all'avvio
  (`firmephoenix.Leggi`, `percorsoSegretoStandalone`). Regole di `segreti.LeggiFile`, poi senza gli spazi ai
  bordi e almeno 64 byte (come `SECRET_KEY_BASE_FILE` di `runtime.exs`). **Assente** = ogni Phoenix.Token è
  rifiutato (i link `/artigiano/<id>?t=` già mandati dallo standalone danno il 404); **presente ma sbagliato** =
  l'avvio si ferma con «segreto dello standalone: …», senza il contenuto; assente, all'avvio un avviso «segreto dello
  standalone assente (<percorso>): i link Phoenix degli artigiani saranno rifiutati». Serve **solo** ai link degli
  artigiani (D-87): per conferma e disiscrizione email la D-02 resta.
- **Nel codice:** `internal/firmephoenix` (Plug.Crypto 2.2.0: PBKDF2-SHA256 1000 giri 32 byte, `SFMyNTY.<payload>.<firma>`,
  `{dato, firmato_ms, max_age}` in External Term Format letto in modo rigoroso; rifiutato se firmato dopo adesso o
  oltre i 90 giorni); `firmephoenix.Segreto` non si stampa, come `firmeflask.Segreto`. I vettori dei test sono veri,
  generati con Plug.Crypto dell'immagine dello standalone (ricevuta della 084).
- **Gettoni del Go:** `buste.GettoneArtigiano` firma «g1.<firmato_ms>.<HMAC>» con `db.SottoChiave(chiave,
  "artigiano")` e un prefisso di dominio; 90 giorni, non revocabile, uno nuovo a ogni lettura della scheda.
- **Cutover (T5, da fare):** copiare il `secret_key_base` dello standalone (quello di `SECRET_KEY_BASE_FILE`) in
  `secrets/segreto_standalone`, 0600, proprietario l'utente del servizio; nel backup cifrato come il segreto
  Flask. Dopo 90 giorni dal passaggio nessun link dello standalone è più valido e il file si può togliere.
  Al cutover si apre con il Go un link `/artigiano/<id>?t=` vero già mandato dallo standalone.
- **404 pubblici senza tipo:** `RifiutoPubblico` toglie il `Content-Type` e fa `Flush` prima del corpo (dalla 084,
  come `rispostaSenzaTipo` della 078): se intestazioni e corpo partono insieme il Caddy davanti mette `text/plain`.

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
  perché il `ReadTimeout` del server vale per tutto il corpo; letto il corpo fino in fondo la
  scadenza si toglie (`togliScadenzaLettura`, slice 118b, nota 10), perché la lettura di fondo del
  server non cancelli la richiesta durante la normalizzazione delle foto. Se la lettura si ferma al
  limite per una violazione (`arrivo.interrotto`) la scadenza resta: un client fermo ha la risposta
  dopo il tempo del pezzo (`TestCaricamentoOltreIlLimiteClienteFermo`). I file in arrivo vanno nella cartella
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
  `allungaLettura` per la scadenza per pezzo; il file va nei caricamenti come sopra. Finito il corpo
  la scadenza si toglie (`togliScadenzaLettura`, slice 118b, nota 10): se la lettura che arriva alla
  fine chiudeva anche un MiB, la scadenza tornava dopo che il server l'aveva tolta per la sua lettura
  di fondo, e l'attesa del figlio o la normalizzazione oltre il tempo del pezzo cancellavano la
  richiesta (`TestCodaFotoNormalizzazioneOltreIlPezzo`, `TestCaricamentoNormalizzazioneOltreIlPezzo`).

- **Righe delle caselle (slice 068, Dubbio 3 della 067):** senza JavaScript il browser manda una `<textarea>` con
  `\r\n`, LiveView manda allo standalone `\n`. Le caselle che nello standalone sono di LiveView si leggono con
  `aCapoUnix` (`internal/web/a_capo.go`, solo `\r\n` → `\n`): `reply[body]`, descrizione e note della modifica
  e di «Nuova riparazione» (`campiConRighe`), `notice[body]` e `template[text]`. **Non** `reason` e `testo` della
  pagina pubblica (`/r/`, `/stato/`): lì lo standalone ha un modulo vero (anche con `public_thread.js`, che
  manda `FormData`) e salva `\r\n` come il Go (Dubbio della 068). Sono tutte le `<textarea>` delle pagine
  (controllo della 069c): `TestCaselleDiPiuRigheNormalizzate` legge `pagine/*.html` e cade per una casella di
  più righe nuova che non sia fra le normalizzate o fra le due pubbliche.

## Coda in uscita (slice 068, CLI-51)

- **Dove:** `internal/uscita` (`Coda`, `Accoda`, `Giro`, `Lavoro`, l'interfaccia `Provider`), sulla tabella
  `messaggi_in_uscita` della 0008 (quella dello standalone, `outgoing_messages`). Stati `queued` → `accepted` o
  `failed` (nessun «consegnato»: il CHECK del database lo impedisce); WhatsApp non passa di qui (nasce `accepted`,
  `buste.registraConsegna`).
- **Accoda** = `Outgoing.enqueue/3`: solo `email` e `telegram` (WhatsApp `ErrSoloAMano`), solo un canale con un
  provider (`ErrNonConfigurato`), indirizzo `\A\S+\z` e al più 320 caratteri, parte fra shop, customer,
  oreficeria, artisan, testo senza spazi ai bordi e non vuoto (`ErrMessaggioNonValido`); la riga con zero
  tentativi e il prossimo tentativo adesso, e `outgoing.queued` (canale, parte, busta; mai testo né indirizzo)
  nella stessa transazione.
- **Giro** = `dispatch_once/1`: le righe `queued` col momento venuto **dei canali che hanno un provider**
  (`canale IN (…)`, review della 068), per id, al più 50. Le righe di un canale senza provider restano `queued`
  senza tentativi né scritture (dalla 071 ogni canale ha il suo provider in `serve`: restano ferme le righe di un
  canale spento, D-03); una riga uscita dalla
  coda nel frattempo non parte (`still_queued?`); l'esito si scrive solo su una riga ancora `queued`. Accettata:
  `accepted`, l'ora, il riferimento, nessun errore. Rifiutata: un tentativo, l'errore e l'attesa 10 s, 60 s,
  5 min, 30 min; al quinto `failed`. Un provider che manca al momento del tentativo (non succede in `serve`)
  dà `not_configured` e costa il tentativo, come lo standalone. Anche il riferimento del provider passa dalla
  pulizia dei segreti.
- **Errore senza dati:** si salva solo il motivo che il provider dichiara con `uscita.Rifiuto` (senza i suoi
  `Segreti()`, al più 500 caratteri); ogni altro errore è `provider_error`, un panico `raised` (lo standalone
  aggiunge il messaggio dell'eccezione: qui no). Nel registro del server solo canale, id e tipo.
- **Lavoro** = `Outgoing.Dispatcher`: un giro all'avvio (riprende le righe rimaste in coda prima del riavvio, con
  i tentativi già fatti) e poi ogni 30 s; un panico o un errore di un giro non lo ferma. `gestrip serve` lo
  avvia **solo con almeno un provider** (`opzioniServe.provider`; nil = `providerDiServe`): dalla 069 c'è sempre
  SMTP, quindi la coda gira, ma legge solo i canali accesi (sezione sotto): le righe `queued` importate dallo
  standalone restano com'erano finché il loro canale non è acceso (D-03). Le regole di Telegram (429, 400, 403,
  notifiche ai clienti spente) sono della 071 (sezione Telegram, sotto).
- **AccodaIn (slice 070b):** `Coda.AccodaIn(ctx, tx, …)` è `Accoda` nella transazione di chi chiama: stessi
  rifiuti (prima di scrivere: WhatsApp, messaggio non valido, canale senza provider o spento), poi la riga e
  `outgoing.queued` in `tx`, che si scrivono con il commit di chi chiama o mai. Il canale si chiede al provider
  (SMTP lo legge con il collegamento di **lettura**): dentro `tx` niente deve usare il collegamento di
  scrittura, che è uno solo (`SetMaxOpenConns(1)`) e la tiene già, altrimenti si blocca. `Accoda` resta con la
  sua transazione. L'interfaccia `buste.Posta` ha anche `AccodaIn`.
- **Formato e tastiera (slice 071, era il vincolo per la T4.7):** `uscita.Messaggio` porta `Formato` e `Tastiera`
  (le colonne `formato` e `tastiera` della 0008, `parse_mode` e `reply_markup` dello standalone, che l'import già
  copia); `Contenuto` porta `Formato` (`FormatoHTML` o "") e `Tastiera` (`*PulsanteURL`: un pulsante con un link,
  scritto come `{"inline_keyboard":[[{"text":…,"url":…}]]}`). Solo su una riga Telegram, e il solo formato è
  HTML, altrimenti `ErrMessaggioNonValido` (`Message.changeset/2`).
- **Provider nei test:** finti (`providerFinto` in `internal/uscita`, `providerServe` in `cmd/gestrip`) o SMTP contro un server finto (sezione sotto),
  mai la rete.
- **Migrazione 0018** (`0018_indice_invii_busta.sql`): l'indice `messaggi_in_uscita_busta` su `busta_id` (la
  lista degli invii della scheda e l'avviso di ritiro; Dubbio 5 della 067). Solo del Go: `IndiciSoloGo`.

## Email: provider SMTP e configurazione (slice 069, CLI-43, CLI-52 parte email, ADM-09, D-03, D-16)

- **Provider** (`internal/uscita/smtp.go`, `uscita.SMTP`) = `Outgoing.Providers.Smtp` e `Smtp.Tcp` dello
  standalone: una conversazione per riga della coda, solo libreria standard (`net`, `crypto/tls`). Saluto 220,
  `EHLO <dominio del mittente>` (o `localhost`), con `starttls` `STARTTLS` → 220 → TLS → di nuovo `EHLO`, con
  `tls` il TLS dal primo byte (465); `AUTH LOGIN` (utente e password in base64) **solo su una sessione cifrata e
  solo con utente e password**; una password su una sessione mai cifrata è `smtp:auth_requires_tls` senza
  mandare niente; `MAIL FROM`, `RCPT TO`, `DATA`, il messaggio, il 250 (la sua riga è il riferimento), `QUIT`
  senza leggere la risposta. Se il server rifiuta STARTTLS il tentativo finisce lì: mai in chiaro. TLS: le
  radici del sistema, il nome del server come SNI e come nome del certificato, almeno TLS 1.2; dopo STARTTLS
  il lettore riparte da zero (niente di quello che il server aveva mandato in chiaro). Tempi (review della 069):
  15 s per collegarsi e per ogni passo (TLS, ogni risposta, ogni scrittura), mai oltre la **scadenza
  complessiva di 2 minuti** della conversazione; una risposta con più di 512 righe di continuazione o una riga
  oltre i 64 KiB è `:protocol`; il contesto annullato (serve che si ferma) chiude subito la connessione.
- **Messaggio** = `payload/2`: `From` («nome <indirizzo>», solo l'indirizzo senza nome; un nome che non è
  `^[A-Za-z0-9 .'\-]+$` è sempre `=?utf-8?B?…?=`, D-83), `To`, `Subject` (vuoto senza oggetto), `Date` (RFC 2822 in
  UTC, dall'orologio), `MIME-Version`, `Content-Type: text/plain; charset=utf-8`,
  `Content-Transfer-Encoding: base64`; **nessun `Message-ID`** (lo standalone non lo mette); il testo in base64
  a righe di 76 (nessun punto da raddoppiare). `encode_word/1`: un testo di soli caratteri 32-126 resta com'è,
  ogni altro diventa `=?utf-8?B?…?=` (un a capo non può aprire un'intestazione): vale per l'oggetto. **D-83
  (slice 069c, differenza voluta):** il nome del mittente non semplice si codifica sempre
  (`parolaSempreCodificata`), anche tutto ASCII; lo standalone lo passa com'è e «Rossi, Mario» diventerebbe due
  indirizzi.
- **Rifiuti:** ogni codice inatteso è `smtp:<codice>` e basta (mai il testo del server, che può ripetere
  l'indirizzo o la credenziale); ogni errore del trasporto `smtp:transport:<genere>` con un genere fisso
  (`:timeout`, `:econnrefused`, `:econnreset`, `:nxdomain`, `:closed`, `:protocol`, `:tls`, `:error`; lo
  standalone scrive `inspect/1` del motivo di `:inet`/`:ssl`, qui per ogni errore TLS `:tls`). **Come lo
  standalone, 4xx e 5xx non si distinguono:** sono tutti un tentativo della coda, fino al quinto. Il riferimento
  e il motivo passano dalla pulizia della password (`***`) anche nel provider.
- **Configurazione** (`internal/uscita/config_smtp.go`) = `Outgoing.Config` dello standalone, **nel database**
  come lì: le chiavi `outgoing.smtp.host`, `port`, `security`, `username`, `from_address`, `from_name` in
  `impostazioni` (l'import le porta come sono) e la password `outgoing.smtp.password` in `segreti_provider`
  (D-03, migrazione 0006: mai tra le impostazioni, mai in un export). `LeggiSMTP` = `Config.get(:email)`: una
  chiave che manca o di soli spazi è assente, un valore presente resta com'è; porta 587 se manca o non è un
  intero, sicurezza `starttls` se manca. `Usabile` = `usable?/1`: server e mittente (la password no: relay
  locale con `none`). Si rilegge **a ogni tentativo** (`attempt/1`): niente da riavviare dopo un cambio.
  Una configurazione sbagliata non ferma l'avvio (lo standalone la rifiuta al salvataggio e alla consegna
  fallisce): una password con una sessione in chiaro è `smtp:auth_requires_tls`.
- **Seconda serratura (solo Go, review della 069, differenza senza D):** `Usabile` è falso anche con una sicurezza
  fuori da `starttls`, `tls`, `none` (anche «STARTTLS» maiuscolo: lo standalone non la può salvare, il suo
  changeset la rifiuta, e alla consegna parlerebbe in chiaro) e con un server, un mittente o un utente che
  contengono spazi, a capo, `<` o `>` (finirebbero in `EHLO`, `MAIL FROM`, `AUTH`): il canale resta non
  configurato, `not_configured`, invece di parlare col server.
- **Password:** `uscita.Password`, che non si stampa (`String`, `GoString`, `Format` con ogni verbo, `LogValue`,
  `MarshalText` danno `***`; il valore sta in una funzione). Il provider non scrive niente nel registro; la coda
  scrive solo canale, id e tipo.
- **Canale acceso (D-03, solo Go):** l'email è attiva solo con la configurazione usabile **e**
  `canale_email_acceso = 1` in `impostazioni` (`uscita.ChiaveEmailAcceso`, in `importer.ChiaviSoloGo`: una riga
  così nella sorgente blocca l'import). Senza, il canale è spento come senza configurazione: `Consegna` dà
  `not_configured` senza aprire niente, `Accoda` `ErrNonConfigurato`, il giro non legge le righe email. Così
  la configurazione e le righe email importate dallo standalone restano ferme (D-03, D-27) finché il titolare
  non accende il canale. Lo accende (e lo spegne) la pagina dei provider (slice 069b, sotto).
- **D-16:** `uscita.EmailAttiva(ctx, q)` è la domanda «email attiva?» (configurazione usabile e canale acceso):
  la pagina del cliente la fa con `Coda.CanaleAttivo` (`buste.EmailDisponibile`, slice 070, sotto).
- **Coda:** un provider può dire se il suo canale è acceso (`Attivo`, interfaccia `conAccensione` di
  `coda.go`); `Coda.CanaleAttivo` = `configured?/1`. Il giro lo richiede anche riga per riga, prima del
  provider: una riga di un canale che si è spento durante il giro resta com'è, senza tentativo (review della
  069). `gestrip serve` registra sempre il provider SMTP
  (`providerDiServe`), quindi la coda gira sempre (un giro ogni 30 s); con l'email spenta un giro non tocca
  niente. Le righe Telegram restano `queued` senza tentativi finché l'invio Telegram è spento (slice 071).
- **Prove:** server SMTP finto in processo su 127.0.0.1 e porta casuale (`smtp_finto_test.go`), certificato di
  prova generato nel test; relay finto in `cmd/gestrip/coda_uscita_test.go`. Mai un server vero, mai la rete.
- **Pagina «Provider di invio» (slice 069b, ADM-09, CLI-52 parte email):** `internal/web/provider.go`,
  `pagine/provider.html`, dominio in `internal/uscita/pagina_smtp.go` (metodi di `Coda`, interfaccia
  `web.ProviderInvio`, `Dipendenze.Uscita`: in `serve` è la **stessa** coda del lavoro in sottofondo). Rotte, tutte
  `soloTitolare`: `GET /settings/providers` e `POST /settings/providers/smtp` (`rottaConGuscio`: un rifiuto
  ridisegna), `POST /settings/providers/prova`, `POST /settings/providers/accensione`,
  `POST /settings/providers/sospese/{id}/scarta` e `…/manda` (`f.rotta`, poi redirect alla pagina col messaggio).
  - **Modulo** = `Smtp.changeset/3` e `Config.put/3`: `SalvaSMTP` legge i campi `smtp[…]` mandati (un campo che
    manca resta il salvato, come `cast/3`); casella della password vuota o di spazi = tiene quella salvata
    (`keep_secret/3`); `smtp[clear_secret]` («true», «1», «on») toglie password **e** utente; una casella vuota è
    nil (salvata ""; la porta vuota torna 587). Le validazioni guardano **solo i campi cambiati** (come
    `validate_*` di Ecto: un mittente importato non valido si risalva com'è): porta intera (`Integer.parse`
    intero: segno facoltativo, cifre ASCII) «Deve essere maggiore di 0.» / «Al massimo 65535.» / «Valore non
    valido.», sicurezza fra `starttls`, `tls`, `none`, mittente `^[^@\s]+@[^@\s]+\.[^@\s]{2,}$` e nome
    `^[^\r\n]*$` letti come PCRE senza «u» (sui byte, `$` passa un solo «\n» finale: `mittenteValido`,
    `nomeValido`), lunghezze in grafemi (255 server, utente e password; 254 mittente; 200 nome); poi, sui valori
    finali, «serve una password per questo utente» e «le credenziali non viaggiano su una connessione in chiaro».
    Più errori di un campo dal più recente, come `changeset.errors`. Salvato: in una transazione le sei
    impostazioni `outgoing.smtp.*`, la password in `segreti_provider` (nil la cancella) e
    `outgoing.provider_configured` (`outgoing_provider`, `email`) con `channel`, `changed` (i soli nomi, nell'ordine
    di `keys(:email)`) e `configured` (`Usabile`, con la seconda serratura). Rifiutato: niente scritto, la pagina
    con i valori mandati e gli errori sotto i campi (`p.field-error`).
  - **Password mai nella pagina:** l'input è sempre `value=""`, con il segnaposto «salvata — lascia vuoto per non
    cambiarla» se ce n'è una (vuoto altrimenti, come lo standalone); `PaginaSMTP.Valori` e `RifiutoSMTP.Valori` non
    la portano mai. Come `core_components`, un campo vuoto non ha l'attributo `value`.
  - **Messaggio di prova «subito» (slice 069c, decisione dell'11/11; differenza voluta da
    `enqueue_test_message(:email)`, che lo accoda):** `MandaProva` manda al mittente stesso (parte `shop`, nessuna
    busta) «GestRip — prova del provider di posta» con il testo dello standalone **dentro la richiesta, senza riga
    nella coda, anche con l'invio delle email spento** (D-03): `SMTP.ConsegnaAMano` con i suoi tempi massimi e il
    contesto della richiesta (niente riga, niente doppio invio), passando da `provaCon` (panico, pulizia dei
    segreti). Senza configurazione usabile «Posta SMTP non è configurato: niente è stato accodato.» (come lo
    standalone; il banco `provider-posta` lo confronta). Esito: «Messaggio di prova accettato da Posta SMTP: guarda
    se è arrivato nella casella del mittente.» o «Messaggio di prova non partito (smtp:535): controlla i dati del
    server e riprova.» (solo il genere, come la coda lo salverebbe). Audit `outgoing.test_sent` (`outgoing_provider`,
    `email`), esito `ok`/`error`, dati `channel`, `party` («shop»), `status` (`accepted`/`failed`): mai indirizzo,
    testo o errore, come `outgoing.queued_sent_by_hand`; scritto con `context.WithoutCancel` (una richiesta chiusa
    a metà lo lascia lo stesso), se non si scrive va nel registro (solo il tipo). Lo standalone scrive invece
    `outgoing.queued` della riga.
  - **Email da guardare (069c):** ogni riga con una busta mostra «Busta 0045/26» o «Busta N. 7/26»
    (`buste.NumeroVisibile`, D-64; `RigaCoda.NumeroBusta` e `CodiceQRBusta`); senza busta niente.
  - **Acceso ma non configurato (069c):** con `canale_email_acceso` = 1 e la configurazione non `Usabile`,
    `#smtp-switch-unusable` dice «L'invio delle email è acceso ma Posta SMTP non è configurato: nessuna email parte
    finché non completi i dati del server qui sopra.».
  - **Acceso senza indirizzo pubblico (070b, Dubbio 1 della 070):** con `canale_email_acceso` = 1 e
    `indirizzo_pubblico` che manca, `#smtp-switch-no-public` dice che il riquadro «avvisami via email» della
    pagina del cliente non compare e l'email di pronta non parte, con la riga da scrivere nel file e il riavvio
    (`comeConfigurare`, la frase della D-46). Testo solo del Go.
  - **Coda uscente** = `list_recent(20)` e `counts_by_status/0`: solo email e Telegram, dalla più recente; ora del
    negozio (`clock.Timbro`); «↻ Aggiorna» è un link alla pagina. Niente scarta/riprova: lo standalone non li ha.
  - **Telegram (ADM-10):** il riquadro dello standalone sta fra la posta e la coda (slice 071, sezione Telegram
    sotto).
- **D-03 fatta (email; Telegram con la 071) e D-27 fatta (email; Telegram con la 071), slice 069b, solo Go:** nel riquadro della posta
  `#smtp-switch` «Invio delle email» «Spento»/«Acceso». `ImpostaEmail` (= POST `accensione`, campo `acceso`: «1»
  accende, ogni altro valore spegne; il pulsante porta lo stato che chiede, così un doppio clic o una seconda
  scheda non tornano indietro; lo stesso stato non scrive niente) scrive `canale_email_acceso` «1» o «0» e
  `outgoing.channel_enabled` / `outgoing.channel_disabled` (`outgoing_provider`, `email`, `{"channel":"email"}`).
  **Non accende** senza configurazione usabile né con **una qualunque email `queued`** (importata o rimasta da un
  periodo acceso): «Prima di accendere guarda le email rimaste in coda: mandale o scartale una per una.». Con
  l'invio spento la pagina elenca le email in coda dalla più vecchia (`#smtp-pending`: destinatario, oggetto,
  testo con i link firmati mascherati come `mask_links/1`, accodato, tentativi, errore) con:
  - «Manda adesso» = `MandaInSospeso`: **una** email, subito, nella richiesta: `SMTP.ConsegnaAMano` (Consegna
    senza l'interruttore, la configurazione deve essere usabile), l'esito sulla riga come un giro (`salva`:
    accettata, o tentativo, errore senza dati e attesa, al quinto fallita) e `outgoing.queued_sent_by_hand`
    (canale, parte, busta e lo stato dopo; mai testo, indirizzo né errore), esito `ok`/`error`. Messaggi: «Email
    accettata dal provider.», «Email non partita (smtp:550): resta in coda, tentativi 1 di 5.», «… segnata come
    fallita.»;
  - «Scarta» (con `data-confirm` «Scartare questa email? Non partirà più.») = `ScartaInSospeso`: `DELETE` della
    riga e `outgoing.queued_discarded` (canale, parte, busta) in una transazione; «Email scartata: non partirà.».
  Con l'invio acceso le due azioni rispondono «L'invio delle email è acceso: le email in coda le manda la
  coda.»; una riga che non è un'email in coda «Questa email non è più in coda.». Il lucchetto `Coda.aMano` mette
  in fila nel processo ogni riga del giro (`unaRiga`: rilettura, canale acceso, tentativo, esito), accensione,
  scarta e manda (review della 069b): un'email non parte due volte (giro e «Manda adesso» dopo uno «Spegni» a
  metà invio, o un doppio clic) e l'email non si accende né si spegne a metà di un invio. «Manda adesso» usa
  `context.WithoutCancel`: una richiesta chiusa dal browser non ferma l'invio né la scrittura dell'esito
  (restano i tempi massimi dell'SMTP); un audit che non si scrive dopo l'invio va nel registro del server
  (solo id e tipo) e la pagina mostra l'esito vero.
  **Perché «qualunque email in coda» e non solo le importate (D-27):** l'import non lascia un segno per riga
  (`import_standalone` ha solo `importato_il`), gli id sono quelli dello standalone e nessuno scrive il massimo
  al momento dell'import; confrontare `accodato_il` con `importato_il` dipende dall'orologio della macchina dello
  standalone, e un errore in quella direzione farebbe partire le importate tutte insieme. Le email nate nel Go
  che restano in coda con l'invio spento sono solo quelle accodate mentre era acceso: guardarle prima di
  riaccendere è la stessa regola, senza migrazioni (Dubbio della 069b per Stefano). Tutti i testi dell'interruttore e della revisione sono del Go (Dubbi della 069b).

## Telegram: provider, configurazione e riquadro della pagina (slice 071, ADM-10, CLI-51 e CLI-52 parte Telegram, D-03, D-27)

- **Provider** (`internal/uscita/telegram.go`, `uscita.Telegram`) = `Outgoing.Providers.Telegram`: per ogni riga
  della coda un `POST <endpoint>/bot<token>/sendMessage` con `chat_id` (l'indirizzo della riga, com'è), `text` e, solo
  se la riga li porta, `parse_mode` e `reply_markup` (il JSON salvato, com'è); `ok: true` è accettato e il riferimento
  è il `message_id`. Rifiuti: `telegram:http:<stato>:<descrizione>` con le parole di Telegram (le legge la coda),
  `telegram:http:<stato>` senza descrizione, `telegram:<descrizione>` per un 200 con `ok: false`,
  `telegram:risposta non riconosciuta`. `ChiediNome` = `get_me/1` (`GET …/getMe`): l'`username`, o
  `telegram:http:<stato>` / `telegram:getMe senza username`. Il token sta nel percorso: **mai** l'errore di
  `net/http` (porta l'indirizzo), solo `telegram:transport:<genere>` con i generi dell'SMTP
  (`genereTrasporto`: `::timeout`, `::econnrefused`, …), e ogni testo passa dalla pulizia del token (`***`).
  Tempi: 15 s per richiesta tutta compresa; contesto annullato = richiesta chiusa; nessun redirect seguito (come
  hackney); al più 1 MiB di risposta letta (solo Go); trasporto proprio senza proxy dell'ambiente (review della 071:
  un proxy vedrebbe il token); `Segreti()` dà il token, così lo toglie anche `pulisci` della coda. `Consegna` legge la configurazione a ogni tentativo; con
  l'invio spento o la configurazione non usabile `not_configured` senza richiesta; la riga di un **cliente** con
  «Notifiche Telegram ai clienti» spente `telegram:notifiche ai clienti spente`, senza richiesta (`attempt/1`,
  slice 137). `ConsegnaAMano` salta solo l'interruttore della D-03 (revisione D-27 e prova).
- **Indirizzo del Bot API:** solo dall'ambiente, `TELEGRAM_API_ENDPOINT` (come `config/runtime.exs`), letto da
  `gestrip serve` prima del database (`uscita.EndpointDaAmbiente`): assente è `https://api.telegram.org`; presente
  (anche vuoto) deve essere `http(s)://host…`, senza «/» finale; solo Go, niente utente, query o frammento.
  Sbagliato = l'avvio si ferma con un messaggio che nomina la variabile, mai il valore. Nessuna pagina lo mostra.
- **Configurazione** (`internal/uscita/config_telegram.go`) = `Config.get(:telegram)`: la chat in `impostazioni`
  (`outgoing.telegram.chat_id`), il token in `segreti_provider` (`outgoing.telegram.bot_token`, D-03, migrazione
  0006), dove l'import già li mette; il token è una `Password` (non si stampa). `Usabile` = `usable?/1` (token e
  chat presenti) più una **seconda serratura** solo Go: token esattamente `\A[0-9]+:[A-Za-z0-9_-]+\z` e chat
  esattamente un numero o `@nome` (un token importato con «/», «?» o un a capo cambierebbe l'indirizzo).
- **Interruttore dell'invio Telegram (D-03, solo Go):** `canale_telegram_acceso` = 1 (`uscita.ChiaveTelegramAcceso`,
  in `importer.ChiaviSoloGo`). Spento come senza configurazione: `Accoda` `ErrNonConfigurato`, il giro non legge le
  righe Telegram (anche quelle importate restano com'erano), `Consegna` `not_configured`. **Scelta della 071:** la
  D-03 dice «email e Telegram spenti finché il titolare non li accende», ma lo standalone ha solo «Notifiche
  Telegram ai clienti» (ferma le righe dei clienti e il lettore; le righe del **negozio** partono con il bot
  configurato): per tenere fermo anche il negozio serve un interruttore del canale, come «Invio delle email».
  Restano due interruttori nel riquadro (Dubbio della 071).
- **Regole della coda per Telegram** (`coda.go`, `salva` e `Giro`) = `record/2` e `dispatch_once/1` (slice 137 e
  160 dello standalone):
  - **429** (`telegram:http:429`, lo stato intero: «4291» no): la riga prende il tentativo e aspetta il più lungo
    fra il suo gradino e «retry after N» (al più un giorno; senza numero la prima attesa, 10 s); le **altre righe
    Telegram dello stesso giro** non vanno al provider, non prendono tentativi, non si contano e aspettano fino a
    adesso + N (`trattieni`, guardia `queued`); le email del giro partono. Anche un quinto rifiuto trattiene;
  - **400** (`telegram:http:400` o `…400:…`): fallita subito, del negozio come del cliente, niente spento;
  - **403 sulla riga di un cliente** (`telegram:http:403…`, ha bloccato il bot): fallita subito e, nella stessa
    transazione, spente tutte le iscrizioni attive della chat (`iscrizioni_telegram`: consenso 0, `fermata_il`,
    motivo `blocked`) con `repair.telegram_stop` (attore `telegram`, `bag_number`, `subscription_id`, via
    `blocked`; mai la chat) per ognuna = `TelegramSubscriptions.block_chat/1`. Sul negozio un 403 è un rifiuto come
    gli altri;
  - **notifiche ai clienti spente:** la riga del cliente fallisce al primo tentativo.
- **Pagina** (`internal/uscita/pagina_telegram.go`, `internal/web/provider.go`, `pagine/provider.html`, riquadro
  `#telegram-card` fra la posta e la coda):
  - **Modulo** = `Config.put(:telegram)` e `Telegram.changeset/2`: `telegram[bot_token]` (password, sempre vuota; il
    segnaposto «salvato — lascia vuoto per non cambiarlo» se c'è), `telegram[chat_id]` con l'aiuto
    `#telegram-chat-help`, «Cancella il token salvato» (`telegram[clear_secret]`: solo il token). Casella vuota =
    tiene il token. Validazioni solo dei campi cambiati: token `^\d+:[A-Za-z0-9_\-]+$`, chat
    `^-?\d+$|^@[A-Za-z0-9_]+$` (PCRE senza «u»: un solo a capo finale passa, come lo standalone; la seconda
    serratura lo rende poi non usabile), 200 e 64 grafemi, «Formato non valido.», «Troppo lungo (massimo N
    caratteri).». Salvato in una transazione: chat, token, **stato del bot dimenticato** se il token è cambiato
    (`forget_bot_state/2`: `outgoing.telegram.bot_username`, `telegram_inbound.offset`, `last_error`,
    `last_error_at`, `failing_update` a ""), `outgoing.provider_configured` (`telegram`, `changed`, `configured`).
  - **Prova** (`MandaProvaTelegram`, `POST …/prova-telegram`): senza configurazione «Telegram non è configurato:
    niente è stato accodato.»; poi `getMe` e, se il bot non risponde, «Il bot non risponde a getMe (<motivo>):
    niente è stato accodato.» (frasi dello standalone); se risponde il nome si salva (`outgoing.telegram.bot_username`,
    mostrato «Bot: @nome») e il messaggio di prova dello standalone va alla chat del negozio **subito, senza coda,
    anche con l'invio spento** (come la posta della 069c; lo standalone lo accoda). Esiti «Messaggio di prova
    accettato da Telegram: guarda se è arrivato nella chat del negozio.» o «Messaggio di prova non partito (<motivo>):
    controlla il token e la chat e riprova.». Audit `outgoing.test_sent` (`telegram`, parte `shop`, `accepted` o
    `failed`).
  - **Notifiche Telegram ai clienti** (`POST …/notifiche-clienti`, campo `enabled`, «true» accende) =
    `TelegramInbound.set_enabled/2`: `outgoing.telegram.customer_notifications` «1» o "", audit
    `outgoing.telegram_customer_notifications` con `enabled` (scrive anche se lo stato è già quello, come lo
    standalone). Il riquadro (testi dello standalone): «Accese»/«Spente», «Il bot non è configurato: niente viene
    letto finché non lo è.», «Risposte fisse date a chat sconosciute: N.», e l'**ultimo errore del lettore**
    (`#telegram-inbound-error`, da `telegram_inbound.last_error` e `last_error_at`; per un errore che comincia con
    «409» la frase sul webhook). Li scrive il lettore della 073 (sezione «Bot Telegram», sotto).
  - **Invio Telegram (D-03, D-27, solo Go):** `#telegram-switch` con «Accendi l'invio Telegram»/«Spegni …»
    (`POST …/accensione-telegram`, campo `acceso`); non si accende senza bot configurato né con **un qualunque
    messaggio Telegram in coda**; da spento `#telegram-pending` elenca i messaggi in coda (testo senza i tag HTML,
    come gli invii della scheda) con «Manda adesso» e «Scarta» (le rotte `sospese/{id}` della 069b, ora per i due
    canali: `ScartaInSospeso` dà il canale, `EsitoAMano.Canale`; una riga è «in sospeso» solo se il suo canale ha un
    provider nella coda). Con l'invio acceso e il bot non più configurato `#telegram-switch-unusable` (come
    la posta della 069c). Testi solo del Go.
- **Prove:** Bot API finto in processo (`httptest`, `telegram_finto_test.go`; `botWeb` nei test della pagina;
  `TestServeTelegram` con `TELEGRAM_API_ENDPOINT` puntato a lui). Mai Telegram, mai un token vero.
- **Fuori dalla 071:** iscrizioni e riquadro della pagina del cliente (fatti con la 072, sezione sotto),
  lettore `getUpdates` e comandi del bot (073), avvisi di cambio stato e nuovo messaggio (fatti con la 074,
  sezione «Avvisi Telegram»), risposte e pulizia (fatte con la 075, sezione «Risposte del cliente e pulizia
  Telegram»), il reset della password via Telegram (ACC-13: `ConsegnaAMano` alla chat del negozio è già pronta).

## Iscrizione Telegram della pagina del cliente (slice 072, CLI-44, CLI-45, CLI-19, D-16)

- **Dove:** dominio in `internal/buste/telegram_cliente.go` (= `TelegramSubscriptions.box/2`, `issue_link/4`,
  `revoke/3`), pagine in `internal/web/telegram_cliente.go` e `pagine/pagina_cliente.html` (riquadro
  `#public-telegram`, **sopra** `#public-email`, anche nella pagina storica e nell'anteprima). Nessuna migrazione:
  `token_telegram` e `iscrizioni_telegram` ci sono dalla 0008 (i trigger della 0014 le spengono e tolgono i token alla
  chiusura, nel cestino e al passaggio a un'oreficeria) e l'import le riempie da `telegram_link_tokens` e
  `telegram_subscriptions`.
- **Rotte** (faccia pubblica, `rottaPubblica`): `POST /r/{token}/telegram-optin` e `…/telegram-optout`,
  `POST /stato/{busta}/{anno}/telegram-optin` e `…/telegram-optout` (credenziale `pw` o `t`+`exp` nei campi del
  modulo, redirect con la credenziale come `legacy_page/3`), e `GET /r/{token}/telegram` della 066 (sopra, Rotte).
- **Canale disponibile (D-16 come per l'email, solo Go):** `TelegramDisponibile(ctx, base)` = coda con il canale
  Telegram attivo (`CanaleAttivo("telegram")` = `uscita.TelegramAttivo`: «Invio Telegram» della D-03 acceso e bot
  usabile), «Notifiche Telegram ai clienti» accese (`uscita.NotificheClientiAccese`), il nome del bot noto
  (`outgoing.telegram.bot_username`, che salva la prova della 071 e che un token nuovo dimentica) e
  `indirizzo_pubblico` (gli avvisi Telegram della 074 hanno il pulsante con il link della pagina). Lo standalone
  chiede solo notifiche, bot configurato e nome (`available?/0`): i due interruttori sono quelli che la 073 chiederà
  per il lettore. **Seconda serratura del nome (solo Go):** un nome che non è `\A[A-Za-z0-9_]+\z` è ignoto (finisce
  nell'indirizzo t.me). Senza canale: nessun riquadro (anche nell'anteprima), nessun token; un vecchio modulo torna
  alla pagina con «⚠️ Servizio non disponibile. Riprova più tardi.» in cima, come lo standalone.
- **Token** (= `issue_link/4`): 24 byte di `crypto/rand` in base64url senza «=» (32 caratteri), nel database solo
  `sha256(token)` con la busta e `scade_il`: 24 ore dal pulsante (via `page`), 60 minuti dal link WhatsApp (via
  `whatsapp`). Nella transazione: busta riletta fuori dal cestino, prima la controparte (conto terzi =
  `ErrTelegramNascosto`, anche chiusa), poi chiusa (`ErrTelegramChiusa`), lo storico revocato dopo la verifica della
  rotta (`link_alive/2`), il canale, il limite, il token e `repair.telegram_optin` (`bag_number`, `link` «token» o
  «legacy», `via`; mai il token). **Limite:** 6 righe all'ora per busta **per via** (una riga senza via, di prima della
  slice 136 dello standalone, è della pagina): un'anteprima di WhatsApp non consuma il pulsante. Il redirect è
  `https://t.me/<bot>?start=<token>`. Aprirlo non iscrive nessuno: il token lo consuma `/start` nel bot (073).
- **Riquadro** (= `box/2`): stato A «Vuoi essere avvisato in tempo reale?» con «Attiva su Telegram →»; stato B
  «Notifiche Telegram attive» (un'iscrizione con consenso 1 e non fermata) con «⚙️ Modifica preferenze» (lo stesso
  modulo, un token nuovo) e «Disattiva» (`data-confirm`). Solo buste del cliente diretto, non chiuse, fuori dal
  cestino, e sul link storico non revocato. Esiti nella riga `#public-telegram-feedback` (sessione `InfoTelegram`,
  `ErroreTelegram`), una volta. Nell'anteprima i moduli non hanno `action` né token.
- **Risposte** (`telegram_linked/3`, `telegram_followed/3`, `telegram_revoked/3`): riuscito il 302 a t.me
  (`no-store`, `no-referrer`); link morto e conto terzi il 404 della pagina (il link storico con
  `repair.legacy_public_denied`); chiusa la pagina con «Questa riparazione non riceve più avvisi.» in cima; limite
  del pulsante «⚠️ Troppe richieste nell'ultima ora: riprova più tardi o contatta il negozio.» nel riquadro
  (`#public-telegram`); dal link WhatsApp, senza riquadro, 503 e 429 in testo semplice.
- **Disattiva** (= `revoke/3`): solo il cliente diretto (conto terzi = 404), anche su una busta chiusa, senza
  limite (come lo standalone): ogni iscrizione ancora attiva della busta prende consenso 0, `fermata_il`, motivo
  `page` e una riga `repair.telegram_stop` (`bag_number`, `subscription_id`, `via` «page», attore `public`; mai la
  chat); poi «Notifiche Telegram disattivate.» nel riquadro. `spegniIscrizioni` è anche il ciclo di `tagliaTelegram`
  (motivo e via «link»).
- **Popup (066):** `ApriAvviso` dice `TelegramSpento` quando `TelegramDisponibile` (con l'indirizzo pubblico delle
  buste) è falso; un errore nel leggerlo vale «spento» e mette `TelegramNonLetto`, che `f.apriAvviso` scrive nel
  registro (review della 072): il popup WhatsApp si apre lo stesso.
- **Dopo l'import (cutover, Dubbio 6 della 072):** «Invio Telegram» è spento (D-03), quindi i link
  `/r/<token>/telegram` già mandati via WhatsApp rispondono 503 e il riquadro non compare finché il titolare non lo
  accende (dopo la revisione D-27). `HEAD /r/{token}/telegram` crea un token come la GET (come `Plug.Head`): un'anteprima
  che fa HEAD e GET consuma 2 dei 6 posti.
- **Banco:** `bench/scenari/telegram-cliente.json` (pagina, link WhatsApp, pulsante, Disattiva, link storico, rifiuti):
  nel banco il Go ha i canali spenti (D-16) e lo standalone non ha il bot, quindi si confrontano le risposte a canale
  spento; il riquadro, il token e lo spegnimento riusciti li provano i test Go.

## Bot Telegram: lettore `getUpdates` e comandi del cliente (slice 073, CLI-46)

- **Dove:** il lettore in `internal/uscita/lettore_telegram.go` (= `TelegramInbound.poll_once/0`, `TelegramPoller`,
  `TelegramInbound.ReplyLimit`), le iscrizioni in `internal/buste/telegram_bot.go` (= `TelegramSubscriptions.start/2`,
  `consent/3`, `stop_chat/1`, `OnlineSales.shop_name/0`), che soddisfano `uscita.IscrizioniTelegram`. `gestrip serve`
  lo avvia con gli altri lavori quando il provider Telegram è quello vero (`*uscita.Telegram`: i test con i provider
  finti non lo avviano) e lo aspetta all'arresto. Nessuna migrazione.
- **Quando legge (vincolo di Stefano, Dubbio 1 della 071):** solo con «Invio Telegram» (D-03) **e** «Notifiche
  Telegram ai clienti» accese e il bot `Usabile`, riletti a ogni giro. Altrimenti la lettura è «inattiva»: nessuna
  richiesta, e il lavoro riguarda dopo 30 s (accendere non vuole un riavvio). Lo standalone chiede solo le notifiche e
  il bot. Prima lettura subito all'avvio (lo standalone aspetta prima i 30 s).
- **Lettura:** `POST <endpoint>/bot<token>/getUpdates` con `allowed_updates` `message` e `callback_query`, `timeout` 25
  e l'`offset` salvato se c'è; endpoint, trasporto senza proxy e niente redirect della 071 (`richiestaPer`), 35 s di
  tempo e al più 8 MiB letti (solo Go). Attese (`ProssimaAttesa` = `next_delay/3`): con aggiornamenti subito, lettura
  vuota 1 s, inattivo 30 s, errore o 409 5 s → 15 s → 1 min → 5 min. All'arresto la lettura lunga si chiude e niente
  si salva come errore; un aggiornamento iniziato si finisce (contesto senza annullamento), fra uno e l'altro si
  guarda l'arresto.
- **Offset e fallimenti:** `telegram_inbound.offset` = id + 1 dopo **ogni** aggiornamento; uno dietro l'offset o senza
  `update_id` intero non si gestisce. **Solo Go (D-85, slice 075):** un offset la cui riga non è stata scritta da più
  di sei giorni (`aggiornata_il`; sette fino alla slice 077) si dimentica e la lettura parte senza. Un aggiornamento che fallisce (errore delle iscrizioni o panico) ferma la lettura
  con l'offset su di lui, `telegram_inbound.failing_update` = `id:volte`, motivo `telegram:update <id> non gestito`; al
  terzo si salta. Nel registro il numero dell'aggiornamento e il genere (`%T`), mai il messaggio.
- **Errore e 409:** `telegram_inbound.last_error` (al più 500 caratteri) e `last_error_at` (RFC 3339); un 409 è
  `409: <descrizione>` (o `Conflict`), che la pagina della 071 mostra come conflitto. Il bot non chiama mai
  `deleteWebhook`. Una lettura riuscita pulisce l'errore, e scrive solo se c'era.
- **Comandi** (solo chat private; fuori dal flusso gruppi, canali, altri bot e la chat del negozio per numero o
  `@nome`): `/start <token>` (anche `/START@bot`, spazi ASCII come `\s+`) → `AvviaTelegram`: token di al più 64 byte,
  SHA-256 non scaduto (`scade_il > adesso`), cancellato nella stessa transazione; busta riletta fuori dal cestino, del
  cliente diretto e non chiusa, altrimenti rifiuto (il token si consuma lo stesso, come lo standalone); iscrizione
  nuova o riaperta con consenso 0, `consenso_il`, `fermata_il` e `motivo_fermata` a NULL; `repair.telegram_linked`.
  Risposta: il benvenuto HTML dello standalone (nome del negozio con `Plug.HTML.html_escape`) e la tastiera
  `cli:tx:1:<id>` / `cli:tx:0:<id>`. Il pulsante → `answerCallbackQuery` e `ConsensoTelegram` (solo la chat
  dell'iscrizione, solo non fermata; stessa risposta già data = niente; `repair.telegram_consent`). `/stop` e
  `/disattiva` → `FermaChatTelegram` (motivo `stop`, `repair.telegram_stop` via `bot`). `/privacy` → indirizzo
  pubblico + `/privacy` (**solo Go:** senza indirizzo pubblico la risposta fissa). Il resto → la risposta fissa, che
  conta `telegram_inbound.unknown_chats`. Audit con attore `telegram`, mai chat, token o testo.
- **Risposte subito, non dalla coda** (come `send_text/4` dello standalone): `sendMessage` dentro la lettura, nessuna
  riga in `messaggi_in_uscita`; una risposta rifiutata va nel registro (solo il motivo) e si lascia.
- **Limiti in memoria** (`limiteRisposte` = `ReplyLimit`, sull'orologio del lettore): risposta fissa 1 ogni 10 minuti
  per chat e 20 al minuto in tutto; le altre 8 al minuto per chat e 30 al minuto in tutto; un pulsante oltre il limite
  non riceve nemmeno `answerCallbackQuery`. Niente oltre 10 minuti, niente nel database.
- **Risposte a una notifica e pulizia oraria:** fatte con la 075 (sezione «Risposte del cliente e pulizia Telegram»,
  sotto).
- **Import:** `telegram_inbound.*` arrivano con le altre impostazioni, così come sono (provato in
  `TestImportCompletoRiconciliato`): stesso token, stesso bot, quindi l'offset importato è giusto e il Go riprende da
  dove lo standalone si era fermato. Con lo standalone ancora acceso sullo stesso bot, il Go vede un 409.
- **Prove:** Bot API finto in processo (`lettore_telegram_test.go` con iscrizioni finte, `telegram_bot_test.go` di
  `buste` con il giro completo pagina → `/start` → «Sì» → `/stop`, `TestServeLettoreTelegram*`). Mai Telegram.
- **Banco:** nel Go del confronto il Telegram è spento (D-03), quindi il lettore resta inattivo e non fa richieste;
  nel Go con Telegram della 074b legge il bot finto (sezione «Banco con il canale Telegram acceso»).

## Avvisi Telegram al cliente e al negozio (slice 074, CLI-47, CLI-48, RIP-H-18, CLI-14, CLI-08, CLI-13)

- **Dove:** dominio in `internal/buste/telegram_avvisi.go` (= `TelegramNotices`, `TelegramNotices.Pending`,
  `QuoteDecision.notify_shop/3`, `PublicMessage.notify_shop/3`), il congelamento in `passaStato` (`stati.go`,
  campo `congela` di `passaggio`), le pagine in `internal/web/telegram_avvisi.go` (= `TelegramStatusPopup`),
  `lista_azioni.go`, `stati.go`, `conversazione.go` e il modello `popupTelegram` di `pagine/base.html`. Nessuna
  migrazione: `versioni_stato_buste` e il suo trigger ci sono dalla 0007/0014, le iscrizioni dalla 0008.
- **Canale degli avvisi al cliente** (`telegramClienti`, solo Go): la coda con Telegram attivo (D-03 e bot
  usabile), «Notifiche Telegram ai clienti» accese e `indirizzo_pubblico` delle buste (il pulsante porta il link
  `/r/`). Lo standalone chiede solo notifiche e bot (`TelegramInbound.active?/0`). Il nome del bot non serve.
- **Cambio di stato con il popup (CLI-47, RIP-H-18):** solo `CambiaStato` (scheda), `CambiaStatoDallaLista` (menu
  della lista) e `PassoRapido` («✓ Pronta», «✓ Consegnata») congelano: nella transazione del cambio, **prima**
  dell'UPDATE, le chat iscritte con il consenso e non fermate (il trigger della 0014 spegne le iscrizioni nella
  stessa scrittura che consegna); dopo il commit `preparaNotifica` (= `prepare/3`) dà `Cambio.Telegram`
  (`*NotificaTelegram`: busta, numero D-64, stato, versione, anteprima HTML, pulsante; le chat restano in un
  campo non esportato, e `String`/`GoString` non le scrivono) o `Cambio.TelegramNonPreparata` (un
  `*ModelloNonValido`, o un errore: il cambio resta). Nessun avviso senza chat, su una busta conto terzi o con il
  canale spento. Pannello del QR, lotti, import, lavori e riapertura non congelano niente.
- **«Invia»** = `AutorizzaNotificaTelegram` (= `authorize/2`), una transazione: la versione
  (`ErrTelegramSuperata` se un cambio dopo, da qualunque scrittura, l'ha superata; il 📌 no), la busta fuori dal
  cestino, il canale (`ErrTelegramSpento`), il cliente diretto (`ErrTelegramNonCliente`), poi `accodaTelegram`.
  Il testo è quello del popup, parola per parola, anche se il testo salvato è cambiato dopo.
- **Righe** (`accodaTelegram` = `queue_messages/4`): le chat tagliate con il link (`motivo_fermata = 'link'`, un
  `/start` nuovo lo toglie) restano fuori, senza nessuna `ErrTelegramLinkRevocato`; il link stabile
  (`garantisciLinkIn`); una riga per chat con `Posta.AccodaIn` (`customer`, la chat come indirizzo, formato HTML,
  il pulsante «📂 Apri scheda riparazione» su `indirizzo_pubblico + /r/<token>`). Tutte o nessuna.
- **Testi** (`componiCambioStato`, `componiNuovoMessaggio` = `compose_status/2`, `compose_message/2`): `{nome}` la
  prima parola del nome, `{numero}` come la D-82, `{negozio}` = `NomeNegozio`, `{emoji}`/`{titolo_stato}` da
  `visivoStati` (🔔 e il nome con la maiuscola per uno stato sconosciuto), `{stato}` con gli spazi, `{descrizione}`
  la prima riga tagliata a 100 code point con «…», `{testo}` tagliato a 400; ogni valore con `&`, `<`, `>` come
  entità. Un testo salvato che non si compone è il suo `*ModelloNonValido` (mai il predefinito).
- **Popup nella pagina (solo Go, senza LiveView):** `dopoCambio` mette l'avviso nel deposito del server
  (`depositoNotifiche`: in memoria, un'ora, al più 256, legato a utente e nonce della sessione) e nella sessione
  solo il gettone (`datiSessione.Telegram`, 16 byte casuali): le chat non vanno mai nel cookie né nella pagina. La
  prima pagina del personale dopo il redirect lo disegna (`popupTelegram`, in `mostraConInfo`) e toglie il
  gettone dalla sessione: ricaricarla è «Annulla». Un cambio dopo toglie il popup di prima. Anteprima senza tag
  (`testoInvio`), `#telegram-status-popup`, `#telegram-status-bag`, `#telegram-status-preview`,
  `#telegram-status-button`, `#telegram-status-send`, `#telegram-status-cancel`. `app.js`: fuoco su «Invia», Esc e
  un tocco sullo sfondo mandano «Annulla».
- **Risposta del popup:** il modulo è `data-invio-unico`; il deposito dà l'avviso una volta sola e lo tiene
  `risposto` per 5 minuti: un secondo invio dello stesso popup (il doppio tocco) torna dove torna il primo, senza
  errore e senza accodare (review della 074); un gettone che non c'è va alla lista con «Notifica Telegram non
  inviata: il popup non è più valido.»; «Invia» riuscito «📲 Notifica Telegram accodata per il
  cliente», rifiutato le frasi di `TelegramStatusPopup.refusal/1`; «Annulla» tace. Dopo: la scheda con un segno
  nuovo (il seguito di un'azione della scheda), la lista, o il prezzo finale dopo «✓ Pronta» (`?edit=prezzo`, che
  con il popup aspetta la risposta; senza popup ci si va subito). Un testo che non si compone: «Notifica Telegram
  non preparata: il testo «…» non è valido (…).».
- **Nuovo messaggio del negozio (CLI-48, CLI-14):** dopo `RispondiAlCliente` la scheda chiama
  `NotificaNuovoMessaggio` (= `notify_new_message/3`): con delle righe «Risposta inviata. 📲 Notifica Telegram
  accodata per il cliente» e la scheda, senza popup WhatsApp; con zero righe (conto terzi, canale spento, nessun
  iscritto con il consenso) il popup WhatsApp della 066 come prima; un testo che non si compone «Notifica Telegram
  non accodata: il testo «…» non è valido (…).» e il popup WhatsApp. Busta chiusa: le iscrizioni sono già spente
  e il popup WhatsApp rimanda alla scheda (066), quindi nessun avviso.
- **Avvisi alla chat del negozio (CLI-08, CLI-13):** `avvisaNegozio` = `destination_for_shop(:telegram)` ed
  `enqueue/3`: testo semplice, parte `shop`, la chat di `outgoing.telegram.chat_id`, nessuna busta, in una
  transazione sua dopo il commit; senza coda o configurazione `uscita.ErrNonConfigurato`, un panico è un errore.
  Preventivo (`rispondi`): «🤝 Preventivo APPROVATO — busta N» / «🚫 Preventivo RIFIUTATO — busta N», «Importo: …»
  (il testo scritto com'è o i centesimi «€ 12,50»), «La busta è passata in lavorazione.» o «Motivo: …» / «Nessun
  motivo indicato.»; un mancato avviso non lascia righe, come lo standalone. Messaggio del cliente o
  dell'oreficeria (`scriviMessaggioPubblico`, anche dalla pagina storica): «💬 Messaggio dal cliente|dall'orefice —
  busta N», i primi 400 grafemi con «…», «Apri la scheda: <indirizzo_interno>/repairs/<id>» (la scheda del Go;
  senza `indirizzo_interno` il solo percorso); un mancato avviso è `repair.message_notice_not_queued` con
  `not_configured` (senza coda, configurazione, canale spento) o `error`. Il numero nei due testi come la D-82; mai
  il nome del cliente. `gestrip serve` dà alle buste `ConIndirizzoInterno(conf.IndirizzoInterno)`. **Dalla 075
  (D-86, solo Go)** anche il mancato avviso del preventivo lascia una riga, `repair.quote_notice_not_queued`, con
  lo stesso motivo (`registraAvvisoMancato`).
- **Convivenza:** come lo standalone, il cambio di stato non apre il popup WhatsApp (solo «Rispondi», «Salva e
  avverti» e il QR lo fanno) e l'email di pronta (070b) parte da sola nella transazione del cambio: popup Telegram
  ed email di pronta sono indipendenti. Il popup «📲 Avvisa cliente» resta solo WhatsApp: l'invio via Telegram
  da lì (`queue_text/4`, slice 145 dello standalone) non è della 074.
- **Prove:** test di dominio con la coda vera e il provider finto (`telegram_avvisi_test.go` di `buste`), pagine
  con la faccia interna vera (`telegram_avvisi_test.go` di `web`). Mai la rete.
- **Banco:** nel Go del confronto Telegram è spento (D-03), quindi nessun popup, nessun avviso e, nel Go, il mancato
  avviso dei messaggi `not_configured` come prima. Il flusso con il canale acceso lo prova, solo sul Go, lo scenario
  `telegram-acceso` della 074b (sotto, «Banco con il canale Telegram acceso»).

## Risposte del cliente e pulizia Telegram (slice 075, CLI-49, CLI-50, D-85, D-86)

- **Dove:** il lettore in `internal/uscita/lettore_telegram.go` (`messaggio`, `bustaRisposta`, `rispostaDelCliente`,
  `scriviRisposta`, `Pulisci`, `puliziaProtetta`), la busta e la conversazione in `internal/buste/telegram_risposte.go`
  (= `TelegramReplies.bag/2` e `post/3`) e `conversazione.go` (`scriviMessaggio`, il `write/6` di `PublicMessage` con
  il link `:telegram` e l'`also`). `uscita.IscrizioniTelegram` ha due metodi in più (`BustaDellaRisposta`,
  `RispostaTelegram`) e i rifiuti `uscita.ErrRisposta…`. In `gestrip serve` il lettore ha le buste di serve (`ms.buste`,
  con la coda e gli indirizzi pubblico e interno), non un `buste.Nuove` nudo: senza la coda l'avviso al negozio della
  risposta sarebbe sempre `not_configured` (banco della 075, giro s075p; `TestServeRispostaTelegramAvvisaIlNegozio`). **Nessuna migrazione:** l'indice
  `messaggi_in_uscita_risposte_telegram` (= `20261013160000`) e i trigger dello spegnimento (0014) c'erano già.
- **Risposta a una notifica (CLI-49):** un messaggio di una chat privata (non un bot, non la chat del negozio) con
  `reply_to_message.message_id` intero e che non è un comando del bot (`reComandoBot` = `command?/1`:
  `^\s*/(start|stop|disattiva|privacy)(@\S+)?(\s|$)` con `iu`, quindi spazi Unicode) cerca la busta: la riga Telegram
  del cliente accettata con quel riferimento, a quella chat, accodata da non più di 90 giorni (`accodato_il >=`), con
  la busta, la più recente; niente se la chat è tagliata con il link di quella busta (`chatTagliate`). Un `/stop` non
  conta. Senza busta il messaggio è un comando o testo libero, come prima.
- **Il messaggio** (`rispostaDelCliente` = `answer/5`): senza testo o di soli spazi → «❌ Solo risposte di testo per
  ora.» senza contarlo; oltre 8 al minuto per chat (`limiteRisposte`, genere `rispostaIngresso`, in memoria) → «⏳ Hai
  inviato troppi messaggi: attendi qualche minuto e riprova.»; il resto si conta e va a `RispostaTelegram`: la busta
  fuori dal cestino prima del testo (`Repo.get/2`), poi `testoMessaggio` (vuoto, 1000 code point), poi nella
  transazione della pagina chiusa → «Questa riparazione non riceve più messaggi.», di un'oreficeria → «❌ Riparazione
  non trovata.» (anche cestino e busta sparita), dieci all'ora per busta con i messaggi della pagina → «⏳ Hai già
  inviato molti messaggi nell'ultima ora: riprova più tardi.»; troppo lungo «❌ Risposta troppo lunga (N/1000),
  accorcia.» (N in code point del testo ripulito); riuscito «✅ Messaggio inviato al negozio.». La riga è del cliente,
  senza canale, la busta non letta, `repair.message_appended` e l'avviso alla chat del negozio (CLI-13) con l'attore
  `telegram`. **L'offset (id + 1) si scrive nella transazione della riga** (`dopo`); un rifiuto non scrive niente e
  l'offset lo salva il lettore come sempre. La risposta del bot cita il messaggio (`reply_parameters` con
  `message_id` e `allow_sending_without_reply: true`) e consuma i limiti delle risposte come le altre. Un errore del
  database fa fallire l'aggiornamento (si riprova, al terzo si salta).
- **Pulizia (CLI-50)** (`Pulisci` = `purge/1`, in una transazione): via le iscrizioni fermate da più di 90 giorni e i
  token scaduti (`scade_il <= adesso`); le righe Telegram dei clienti non più in coda e accodate da più di 90 giorni
  prendono l'indirizzo `cancellato`. Il lavoro del lettore la fa **prima di ogni lettura se è passata un'ora**
  (la prima subito), anche con il canale spento (`purge_due?/2`); un errore o un panico vanno nel registro col solo
  genere. Lo spegnimento automatico (trigger della 0014) e il 403 sulle iscrizioni della chat (071) c'erano già; una
  busta riaperta, tolta dal cestino o tornata al cliente non riaccende niente (`TestTelegramNonSiRiaccende`).
- **Offset vecchio (D-85, solo Go, Dubbio 1 della 073):** vedi «Bot Telegram» sopra. A sei giorni esatti vale ancora;
  un `aggiornata_il` che non si legge non decide niente; un offset non intero vale come nessuno. Senza offset Telegram
  ridà solo i non confermati. Telegram conta la settimana dall'arrivo dell'ultimo aggiornamento, il Go da quando ha
  scritto l'offset (fino a circa 24 ore di differenza): per questo sei giorni e non sette (Stefano, 14/11/2026,
  Dubbio 6 della ricevuta 075; fatto con la slice 077).
- **Risposta di `getUpdates` oltre 8 MiB (solo Go, Dubbio 7 della 073):** `richiestaPer` dà
  `errRispostaTroppoGrande` (per le altre richieste resta «non riconosciuta», come prima); il lettore rilegge subito con
  `limit: 1` e continua così (al più 100 letture) finché una lettura torna vuota, poi di nuovo senza limite. Un
  aggiornamento solo oltre 8 MiB resta l'errore «telegram:risposta non riconosciuta» di prima.
- **Mancato avviso del preventivo (D-86, solo Go):** `repair.quote_notice_not_queued` (`bag_number` salvato, `reason`
  `not_configured` o `error`) con il contesto di chi ha risposto, come `repair.message_notice_not_queued`.
- **Banco:** lo scenario `telegram-acceso` (sezione «Banco con il canale Telegram acceso») ha in più la risposta del
  negozio dalla scheda con l'avviso CLI-48, la risposta del cliente con «Rispondi» (comando `rispondi` del bot
  finto), la conferma, l'avviso alla chat del negozio (CLI-13) e il messaggio nella scheda.

## Avviso email della pagina del cliente (slice 070, CLI-39, CLI-40, CLI-41, CLI-19, D-02, D-16)

- **Dove:** dominio in `internal/buste/email_cliente.go` (= `EmailNotice` e `Workshop.EmailConsent`), pagine in
  `internal/web/email_cliente.go`, `pagine/pagina_cliente.html` (riquadro `#public-email`), `pagine/email_azione.html`
  (= `email_action`, anche la pagina di ripiego) ed `esito_pubblico.html` (= `email_outcome`, con «Torna alla tua
  riparazione»). Le buste ricevono la coda e la chiave con `ConPosta(posta, chiave)` (`buste.Posta`: `Accoda` e
  `CanaleAttivo`); in `serve` è la stessa `uscita.Coda` del lavoro in sottofondo e della pagina dei provider, e la
  chiave è `db.SottoChiave(chiave, "email")` (`mediaServe.posta`, `mediaServe.chiaveEmail` in `cmd/gestrip`).
  Nessuna migrazione: le colonne `email_cliente`, `email_confermata_il`, `email_pronta_inviata_il` ci sono dalla 0007.
- **Rotte** (faccia pubblica, `rottaPubblica`): `POST /r/{token}/email-optin` e `…/email-optout`,
  `POST /stato/{busta}/{anno}/email-optin` e `…/email-optout` (credenziale `pw` o `t`+`exp` nei campi del modulo,
  redirect con la credenziale come `legacy_page/3`), `GET`/`POST /stato/email-conferma`, `GET`/`POST
  /stato/email-disattiva` (il `t` dalla query nella GET, dal modulo nel POST) e, solo Go, `POST /stato/email-ripiego`.
  Le `/stato/email-…` hanno due segmenti: il limite ai tentativi non le conta (`famigliaDi`), come lo standalone.
- **D-16 (solo Go):** il riquadro e la richiesta solo con `EmailDisponibile(ctx, base)`: coda con l'email accesa e
  configurata (`CanaleAttivo("email")` = `uscita.EmailAttiva` per lo SMTP), chiave dei link e `indirizzo_pubblico`
  (senza non si scrivono i link dell'email, D-46). Senza: la pagina non ha il riquadro (anche nell'anteprima) e una
  richiesta (anche «Reinvia», anche senza consenso) è `ErrEmailNonDisponibile` prima di scrivere, con la frase
  «⚠️ L'invio dell'email non è al momento disponibile. Riprova più tardi.» **in cima** alla pagina (non c'è il
  riquadro dove metterla). «Disattiva», la conferma e i link nel piè funzionano anche con l'email spenta.
- **Richiesta** (= `request/5`, `resend/3`): con l'email disponibile consenso (`1`, `on`, `true` senza spazi ai bordi)
  e indirizzo (`NormalizzaEmail` = `normalize_address/1`: senza spazi ai bordi, minuscolo, al più 254 caratteri,
  `\A[^@\s]+@[^@\s]+\.[^@\s]{2,}\z` con lo spazio Unicode), poi in una transazione: busta riletta fuori dal
  cestino, `visibile` (link storico di una busta revocata = 404, busta chiusa = «Questa riparazione non riceve più
  avvisi.» in cima, link storico di una busta conto terzi = 404 con `repair.legacy_public_denied`), «Reinvia» solo
  per l'indirizzo **proprio** in attesa, il limite di 6 righe `repair.email_optin_requested` all'ora per busta (anche
  i «Reinvia»), le email in coda dell'indirizzo sostituito `failed` («Avvisi email disattivati dal destinatario»),
  le tre colonne e la riga (`party`, `oreficeria_id` su una busta conto terzi, `bag_number`, `resend`; mai
  l'indirizzo; `audit.ScriviConID` dà l'id della riga, che va nel link). Dopo il commit l'email di conferma va in
  coda: testi dell'editor `email_conferma_oggetto`/`_testo` (067), `{nome}` di chi ha il link («cliente» senza),
  `{numero}` come la D-82 (`NumeroNeiTesti`), oggetto su una riga, il piè dello standalone con il link per
  disattivare. Una coda che rifiuta o un testo salvato che non si compone (mai il predefinito): l'indirizzo resta
  in attesa, riga `repair.email_optin_not_queued` con `reason` (`not_configured`, `invalid_template`, `error`).
- **Di chi è l'indirizzo** (`richiedente` = `address_party/1`): della parte dell'ultima richiesta che non è un
  «Reinvia» (`json_extract(dati,'$.resend') = 0`), il cliente per un indirizzo importato; un'oreficeria con il suo id
  (una riga di oreficeria senza id è di nessuno). L'indirizzo dell'altra parte si disegna come nessuno e non si
  rimanda né si spegne dalla pagina (`proprio`).
- **Riquadro** (= `box/2`): `none`, `pending` con la consegna dell'email di conferma dell'ultima richiesta
  (`queued`, `accepted`, `failed`, `not_queued`; `unknown` senza richieste), `confirmed`. La riga della coda si
  trova per id: dopo `Accoda` `mandaConferma` scrive `repair.email_optin_queued` (solo Go: `bag_number`,
  `message_id`); una richiesta senza quella riga (importata) si legge come `delivery/1` ma solo fra le email con il
  link di conferma, così l'email di ripiego non cambia il riquadro (review della 070). Moduli veri con il
  CSRF della faccia pubblica, esiti nella riga `#public-email-feedback` (sessione: `InfoEmail`, `ErroreEmail`), una
  volta; redirect a `…#public-email`. Nell'anteprima i moduli non hanno `action` né token, dentro il fieldset spento.
- **Link firmati dal Go** (`firmaLink`, `leggiLink`): `base64url(dati).base64url(HMAC-SHA256)` con `dati` = scopo
  (`conf` o `stop`), busta, impronta dell'indirizzo (HMAC con la stessa chiave e il prefisso `repair email
  address:`, mai l'indirizzo) e, per la conferma, l'id della riga della richiesta, più l'istante della firma in
  millisecondi; conferma 48 ore, disattivazione 10 anni, valido fino all'istante compreso. Rinnovare la chiave del
  server (`RinnovaChiaveServer`) fa scadere tutti i link delle email. **D-02:** un `Phoenix.Token` dello standalone
  (`FormaPhoenix`: `SFMyNTY.` più due parti base64url) non si verifica mai: `ErrLinkEmailVecchio`.
- **Conferma** (= `check_confirmation/1`, `confirm/2`): la GET non scrive; il POST, una volta, `email_confermata_il` e
  `repair.email_optin_confirmed`; poi «Email confermata ✅» con il link della busta solo se ha il testo, non è revocato
  né scaduto (`linkStabile`: mai un link emesso qui). Rifiuti 410: «Link non valido o scaduto» (anche il vecchio
  Phoenix.Token: dura 48 ore e la frase dice di chiedere «Reinvia conferma»), «Link non più valido» con
  «L'indirizzo email di questa riparazione è cambiato.» (indirizzo diverso o tolto, busta nel cestino o sparita) o con
  «Per questa riparazione è stata fatta una richiesta più recente…» (una richiesta non «Reinvia» o una disattivazione
  dopo la riga del link).
- **Disattivazione:** dalla pagina (= `revoke/3`) solo l'indirizzo proprio, anche su una busta chiusa, 6 all'ora con
  `via` `page`; dal piè (= `stop/2`) la busta anche nel cestino, solo se porta ancora l'indirizzo del link (altrimenti
  lo stesso successo senza cambiare niente), `via` `footer`, 503 «Disattivazione non riuscita» se la scrittura
  fallisce. Le due svuotano le tre colonne, fanno `failed` le email in coda per l'indirizzo e scrivono
  `repair.email_optout` senza l'indirizzo (`togliEmail` = `clear/4`; «Revoca link» e «Genera nuovo link» lo fanno già
  con `via` `link`, `tagliaEmail` della 061).
- **Pagina di ripiego (D-02, solo Go):** un link di disattivazione non valido (GET o POST: un vecchio Phoenix.Token e,
  dalla 070c, ogni altro link che non si verifica) dà 200 e
  `email_azione` con il modulo «scrivi la tua email» (`#email-fallback-address`) verso `POST /stato/email-ripiego`.
  Quel POST risponde **sempre la stessa pagina** («Controlla la tua email»; con l'email spenta «Avvisi email», senza
  promettere un'email), e il lavoro parte dopo la risposta
  (`Dipendenze.Sottofondo`; in `serve` legato al `WaitGroup` dei lavori, che all'arresto lo aspetta, e un lavoro
  chiesto dopo l'inizio dell'arresto non parte), con `context.WithoutCancel` e al più 2 minuti, così né il testo né
  il tempo dicono se l'indirizzo è iscritto. Un panico del lavoro resta nel lavoro (`recover`): nel registro solo
  «lavoro del ripiego andato in panico» con il tipo del valore. Limiti: `ratelimit.RipiegoEmail`, ogni invio conta,
  5 in 15 minuti per indirizzo IP, poi 15 minuti di blocco; al più 4 lavori in corso o in attesa
  (`postiLavoriRipiego`, un semaforo): oltre, il lavoro si scarta; sempre con la stessa risposta. Il lavoro
  (`MandaLinkDisattivazione`): solo con l'email disponibile, le ultime 10 buste con `lower(trim(email_cliente))`
  uguale all'indirizzo (anche in attesa e nel cestino; un indirizzo importato con maiuscole o spazi si ricontrolla
  sull'impronta dell'indirizzo salvato e normalizzato, e il link si firma per l'indirizzo com'è salvato). Per ogni
  busta, in una transazione IMMEDIATE e **prima** dell'accodamento, i due tetti (3 all'ora per busta, 3 al giorno
  per indirizzo, contati sull'impronta con chiave) e la riga `repair.email_stop_link_sent` (`bag_number`, `party`,
  `address_digest`: l'impronta con chiave, mai l'indirizzo); poi un'email con un testo del Go (non nell'editor) e un
  link di disattivazione nuovo. Se l'indirizzo non è di chi ha il link adesso, l'email va alla parte che l'ha
  chiesto con `{nome}` «cliente» (mai il nome dell'oreficeria al cliente finale, CLI-28). Un errore va nel registro
  del server senza dati.
- **Banco:** `bench/scenari/email-cliente.json` con le righe D-16 (riquadro e redirect delle richieste) e D-02
  (pagina di ripiego) in `bench/deviazioni.json`: nel banco il Go non ha `indirizzo_pubblico` né l'email accesa,
  quindi il riquadro, la conferma e lo spegnimento riusciti li provano i test Go.
- **L'email «pronta per il ritiro» e lo stato dell'avviso nella scheda:** sezione sotto (slice 070b).

## Email «pronta per il ritiro» e avviso email nella scheda (slice 070b, CLI-42, D-16)

- **Dove:** `internal/buste/email_pronta.go` (= `EmailNotice.notify_ready/2`, `sheet/1`), chiamata da `passaStato`
  (`internal/buste/stati.go`: scheda, lista con «✓ Pronta» e il menu, pannello del QR) e da `Riapri`
  (`riapertura.go`) quando la busta arriva a «pronta» da un altro stato. **Il rientro dall'artigiano (T4.8, RIP-S-16)
  chiama `avvisaPronta` nella sua transazione** quando l'esito porta a «pronta» da un altro stato (lo standalone lo
  fa in `return_from_artisan/4`; fatto con la slice 080, `rientro.go`). Nessuna migrazione: `email_pronta_inviata_il` c'è dalla 0007, l'indice
  `messaggi_in_uscita_busta` dalla 0018.
- **Nella transazione del cambio di stato (solo Go):** lo standalone accoda dopo il commit, in una transazione sua.
  Qui `avvisaPronta(ctx, tx, …)` lavora nella transazione del cambio, dentro `SAVEPOINT email_pronta`:
  `notificaPronta` rilegge la busta in `tx`, poi link stabile (`garantisciLinkIn`, la parte di
  `garantisciLink` che lavora in una `tx` data), riga della coda (`Posta.AccodaIn`), segno
  `email_pronta_inviata_il` (con `aggiornato_il`) e `repair.email_ready_queued` (`bag_number`, `party`,
  `message_id`). Il nuovo stato e l'email si scrivono insieme o niente; un'email che non parte (testo salvato che
  non si compone, coda che rifiuta, un errore del database a metà) fa `ROLLBACK TO` il SAVEPOINT (via anche il
  link nuovo) e scrive `repair.email_ready_not_queued` (`bag_number`, `reason`: `invalid_template`,
  `not_configured`, `error`), senza togliere il nuovo stato. Solo un SAVEPOINT che non si può disfare annulla
  tutto il cambio (database rotto). Dentro la transazione niente usa il collegamento di scrittura (le letture di
  testi, nome del negozio, oreficeria e canale vanno sul collegamento di lettura).
- **Condizioni** (`ready_bag/1`, rilette in `tx`, nell'ordine dello standalone): busta fuori dal cestino, stato
  «pronta», indirizzo confermato, indirizzo della controparte (`richiedente` = `address_party/1` uguale a
  `chiTieneIlLink`: un indirizzo importato su una busta conto terzi, il cliente di una busta passata a
  un'oreficeria, l'oreficeria di una busta tornata al cliente non ricevono niente), non già detta (`giaDetto` =
  `told?/1`: il segno, a meno che l'ultima `repair.email_ready_queued` dopo l'ultima richiesta non «Reinvia» o
  disattivazione nomini una riga `failed`; un segno importato senza righe del Go, o una riga scartata dal titolare
  con D-27, è «detta»). Poi la **D-16 (solo Go):** solo con `PostaDisponibile` (canale acceso e configurato,
  chiave dei link, `indirizzo_pubblico` delle buste); senza, niente si scrive (lo standalone scrive il mancato
  invio `not_configured`) e il «pronta» dopo riprova, come lì.
- **Testi:** `email_pronta_oggetto`/`email_pronta_testo` dell'editor (067), un testo salvato rotto è un mancato invio
  (mai il predefinito), `{nome}` di chi ha il link («cliente» senza), `{numero}` come la D-82, `{link_stato}` =
  `indirizzo_pubblico + /r/<token>` del link stabile (`ensure_public_link/2`: un link scaduto o revocato si
  rinnova con `repair.public_link_issued`), oggetto su una riga, il piè con il link per disattivare.
- **Indirizzo pubblico nelle buste:** `Buste.ConIndirizzoPubblico(base)`, in `serve` da `conf.IndirizzoPubblico`
  (`dipendenzeWeb`), lo stesso della pagina del cliente; `PostaDisponibile(ctx)` = `EmailDisponibile(ctx, base)`.
- **Disattivazione e indirizzo nuovo:** come la 070 (`togliEmail`, `ritiraInCoda`): una disattivazione (pagina, piè,
  «Revoca link») o un indirizzo nuovo fanno `failed` anche l'email di pronta ancora in coda per l'indirizzo di
  prima, e le righe dell'email di pronta di prima non contano più (`ultimaPronta`).
- **Scheda** (`Scheda.Email`, `buste.EmailDellaScheda` = `sheet/1`; `internal/web/email_pronta.go`, riquadro
  `#repair-email-notice` in `scheda.html` dopo `#repair-notice`): solo con un indirizzo; indirizzo, «In attesa di
  conferma»/«Confermato»; per un indirizzo confermato della controparte, prima di ogni email di pronta e fuori da
  «pronta», «Email di pronta: partirà da sola al passaggio a «pronta», una volta.» (`#repair-email-ready-pending`)
  o, **solo Go (D-16)** con l'email non disponibile, «Email di pronta: l'email non è disponibile (Impostazioni › Provider): al passaggio a «pronta» non partirà.» (`#repair-email-ready-off`); poi `#repair-email-ready` con
  `email_ready_text/1`: «In coda dal», «Accettato dal provider il», «Fallito il … Riparte al prossimo passaggio a
  «pronta».», «non accodata: <motivo>. Riparte…», «inviata dal gestionale precedente il» (il segno importato). Solo Go: una riga tolta dalla coda dal titolare (D-27) è «Email di pronta: tolta dalla coda dal titolare.» e niente promessa (`discarded`; per `giaDetto` è detta).
  Ore del negozio (`clock.Timbro`). Lo vedono titolare e dipendenti.
- **Prove:** test di dominio con la coda vera e un provider finto, mai la rete; la transazione con il gancio di prova
  `Buste.primaDelCommit` (nil fuori dai test), un errore del database con un trigger `RAISE(ABORT)`.
- **Banco:** il Go del banco non ha l'email accesa né indirizzi confermati (D-16), quindi l'email di pronta non si
  confronta; il giro browser `whatsapp` (la 39/26 portata a «pronta» dalla scheda nelle due app) conta anche
  `repair.email_ready_queued` e `repair.email_ready_not_queued` (0 nelle due) e guarda che la scheda non abbia
  `#repair-email-notice` (`pronta.email`).

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
  la pagina della firma (058); i testi per il cliente e la pagina del cliente usano invece
  `buste.NumeroNeiTesti`, senza «N.» (D-82, estesa alla pagina del cliente con la 074c). Il
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
  **Fatta con la 066 per il popup «📲 Avvisa cliente»** (scheda, lista, dopo «Rispondi»; vedi «Rotte») e con
  la **068 per «↗ Apri WhatsApp» del pannello del QR** dopo «✅ Valutata»: con la **083 per i solleciti agli
  artigiani** («📱 WhatsApp sollecito», «👥 Scegli contatto») e con la **091 per «Apri WhatsApp» e «👥 Scegli
  contatto» del lotto inviato** (`data-wa-lista`) e con la **093 per il reinvio del lotto** (moduli
  `data-avviso-invio`: la riga si registra al tocco); gli altri punti wa.me non ci sono ancora nel Go e la prendono con
  le loro slice.

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

## Decisioni di Stefano del 10/11/2026 (registrate con la slice 067)

Le risposte ai Dubbi delle slice 062, 064, 065 e 066 («ok a tutto» alle raccomandazioni, tramite il
coordinatore).

- **D-81 (CLI-09, RIP-S-22: revoca del rifiuto; slice 067, fatta).** La revoca del rifiuto, che riporta una
  busta `annullata` a `preventivo`, non chiede il motivo (come lo standalone), ma scrive nello storico
  della busta la riga del cambio di stato `repair.status_changed`. Lo standalone non la scrive: lo
  stato è solo nei dati di `quote.*` (Dubbio 2 della 062). L'approvazione resta com'è. Nel Go:
  `RevocaRisposta` legge lo stato di prima nella sua transazione e, se l'UPDATE lo ha cambiato, scrive dopo
  `quote.rejection_revoked` anche `repair.status_changed` con `{"status": "preventivo"}` (i dati dei cambi
  di `passaStato`), nella stessa transazione. Una revoca su una busta già in preventivo e la revoca
  dell'approvazione non la scrivono. Il banco non passa di lì (nessuno scenario revoca un rifiuto): nessuna
  riga di deviazione.
- **D-82 (CLI-33: `{numero}` nei testi per il cliente; slice 067, fatta).** Nei testi dei messaggi per il
  cliente o per l'oreficeria (WhatsApp, e poi email e Telegram) il segnaposto `{numero}` è il numero
  visibile della D-64 **senza** l'«N.» davanti: una busta senza etichetta è «45/26», non «N. 45/26»;
  con l'etichetta resta «0045/26». Le pagine del negozio (la scheda, le stampe) non cambiano; dal 13/11/2026
  (Stefano) **anche la pagina del cliente** (`/r/`, `/stato`, l'anteprima del negozio) mostra il numero senza
  «N.», nel riquadro scuro come ovunque (estensione fatta con la 074c: `PaginaCliente.Numero` da
  `NumeroNeiTesti`; lo standalone scrive il numero interno, quindi su una busta senza etichetta la pagina del Go
  è uguale e restano le righe D-64 delle buste con l'etichetta). Nel Go:
  `buste.NumeroNeiTesti` (accanto a `NumeroVisibile`), usato da `ApriAvviso` per i segnaposto; il popup
  («Busta N. 45/26») resta D-64. Nel banco il livello HTTP non vede i testi (il popup dello standalone è un
  evento LiveView), quindi nessun normalizzatore `D-82` in `confronto.go`; nel livello browser
  `leggiPopup` scrive «N» al posto del numero anche senza «N.» (come fa per la D-64).
- **D-46 estesa ai testi con `{link}` (slice 066-067): fatta con la 067.** Senza `indirizzo_pubblico` un
  testo che contiene `{link}` (o `{telegram_optin}`) non parte, e la pagina dice cosa configurare, con la
  stessa frase della D-46. Nel Go: il popup ha la riga `#notice-link-missing` e, all'«Invia», si ridisegna con
  il testo e «Messaggio non inviato: manca «indirizzo_pubblico = https://host[:porta]» in <file>, poi riavvia
  gestrip serve con «…».» (`ErrSenzaIndirizzoPubblico`, prima di emettere il link: niente scritto). Nel banco
  il Go non ha `indirizzo_pubblico`: gli scenari mandano testi senza link e la riga non si confronta.
  **Porta del lotto (slice 091):** «📤 Chiudi e invia» senza `indirizzo_pubblico`, con `{link}` nel testo del lotto
  («Avviso lotto pronte oreficeria») o in quello delle buste («Avviso oreficeria»), ridisegna la pagina del lotto con
  «Lotto non inviato: manca «indirizzo_pubblico = https://host[:porta]» in <file>, poi riavvia gestrip serve con «…».
  Niente è partito.» (prima di ogni scrittura: il lotto resta aperto). Con i due testi senza `{link}` il lotto parte, e
  «Link pubblico» del riquadro (come «Questo lotto è partito il …» riaprendo la pagina) mostra il solo percorso
  relativo `/lotto/<gettone>`. Lo scenario browser `lotti-invio` salva per questo i due testi senza `{link}`.
- **Numero del link storico (slice 064, si tiene):** la credenziale di `/stato` vale sul numero
  scritto nel link o su quello salvato.
- **Numero di prima della D-77:** va salvato all'import prima della T4.11 (cancellazioni
  definitive), così un vecchio link «0046/26» non apre mai la busta di un'altra. È un prerequisito
  della T4.11 nel PIANO.
- **Segreto Flask di almeno 16 byte:** lo controlla il coordinatore al cutover.
- **Informativa privacy:** resta uguale allo standalone fino al cutover; prima del cutover si riscrive
  tutta con un testo deciso da Stefano (nota della T5 nel PIANO).
- **Senza JavaScript «Invia» va diretto a `wa.me`** (Dubbio 1 della 066): confermato.
- **`{telegram_optin}` è nei testi WhatsApp del negozio:** il segnaposto va gestito bene già nella
  T4.6 (il link porta a `/r/<token>/telegram`, che a canale spento risponde come lo standalone, mai un
  404 su un link valido) e si completa con la T4.7.
- **Inventario, CLI-17 e CLI-28:** testo corretto, sulla pagina storica di una busta conto terzi
  nessuna foto (come il codice dello standalone e il Go).

## Decisioni di Stefano dell'11/11/2026 (risposte ai Dubbi di 067, 068, 069, 069b)

«Ok a tutto» alle raccomandazioni, tramite il coordinatore. Le cose da fare entrano nelle prossime slice
dentro il tetto: la **069c** (rifiniture) e la **070** (email lato cliente, chiude la T4.6).

- **D-83 (CLI-43: nome del mittente delle email; fatta con la 069c).** Il nome del mittente si codifica
  sempre (RFC 2047, «=?utf-8?B?…?=») quando non è «semplice» (`@plain_name` dello standalone: lettere ASCII,
  cifre, spazio, punto, apostrofo, trattino): una virgola, delle virgolette, `<`, `>` o altri segni non
  devono mai cambiare l'indirizzo o il nome mostrato. **Differenza voluta** dallo
  standalone, che passa senza codifica un nome tutto ASCII (Dubbio 4 della 069). Nel Go: `intestazioneMittente`
  usa `parolaSempreCodificata` per il nome non semplice; l'oggetto resta su `parolaCodificata`.
- **Prova «subito» a email spenta (Dubbio 2 della 069b; fatta con la 069c).** «Invia prova» manda il
  messaggio al mittente stesso dentro la richiesta, senza passare dalla coda, anche con l'invio delle email
  spento: serve a controllare i dati del server prima di accendere. Nel Go: `Coda.MandaProva` (al posto di
  `AccodaProva`), audit `outgoing.test_sent` senza dati sensibili.
- **Lista delle email da guardare (Dubbi 3 e 4 della 069b; fatta con la 069c):** mostra il numero di busta (D-64, come
  nelle altre pagine, se la riga ne ha una); con l'email accesa ma la configurazione non più usabile la pagina dice «acceso ma non
  configurato». Nel Go: «Busta …» con `buste.NumeroVisibile` e la riga `#smtp-switch-unusable`.
- **Popup senza `indirizzo_pubblico` (Dubbio 2 della 067; fatta con la 069c):** la riga del popup aggiunge «Togli {link}
  dal testo per mandarlo senza link.». Nel Go: in fondo a `f.linkMancante`.
- **A capo `\r\n` (Dubbio 3 della 067; fatta con la 069c):** si completa la normalizzazione a `\n` in tutti i moduli
  interni senza JavaScript dove manca; la pagina pubblica resta com'è (come lo standalone, Dubbio 1 della 068).
  Nel Go: le sei caselle interne avevano già `aCapoUnix` dalla 067/068; la 069c aggiunge il controllo
  `TestCaselleDiPiuRigheNormalizzate` su tutte le `<textarea>` delle pagine.
- **Si tengono come sono:** configurazione SMTP nel database; accensione solo con la revisione (D-27); coda
  sempre attiva; oggetto senza limite di lunghezza; blocco dell'accensione con qualunque email in coda;
  «Scarta» che cancella la riga; «Ripristina predefinito» senza conferma; popup a pagina intera; frase
  «avviso non registrato: manca …»; doppio «Valutata» senza secondo avviso.
- **Più avanti:** l'anteprima dal vivo dell'editor dei testi in una slice di rifinitura dopo la T4.6; i
  testi solo del Go (pannello, popup, accensione e revisione delle email) li rivede il coordinatore con le
  schermate del manuale: per ora restano.

## Decisioni di Stefano del 12/11/2026 (risposte ai Dubbi di 069c, 070, 070b)

«Ok a tutto» alle raccomandazioni, tramite il coordinatore; le cose da fare sono nella **070c**.

- **D-02 estesa: ripiego per ogni link di disattivazione non valido (Dubbio 9 della 070; fatta con la 070c).**
  `GET` e `POST /stato/email-disattiva` con un link che non si verifica (inventato, senza `t`, della vecchia app
  Flask con `?e=`, firmato con una chiave di prima del rinnovo, oltre al vecchio `Phoenix.Token`) aprono la pagina
  di ripiego «scrivi la tua email»; lo standalone risponde 410 «Link non valido». Il link di conferma vecchio o non
  valido resta il 410 (Dubbio 3 della 070). Nel Go: il ramo `default` di `disattivaEmail` e `disattivaEmailPost`
  è `mostraRipiego`; nel banco le righe D-02 dei passi 10 e 11 di `email-cliente`.
- **Ripiego a email spenta (Dubbio 10 della 070; fatta con la 070c):** le frasi non promettono un'email; testo
  neutro provvisorio («…per chiedere di disattivarli.», pulsante «Invia richiesta», risposta «Al momento il negozio
  non manda email…», perché a email spenta il lavoro del ripiego non manda niente). Quello definitivo, e gli altri
  testi solo Go, li prepara il coordinatore con il manuale. Nel Go: `msgRipiegoSpenta`, `titoloRipiegoSpentaOK`,
  `msgRipiegoSpentaOK`, scelti con `emailAccesa` (stato di tutto il negozio, non dell'indirizzo).
- **Compro oro (Dubbio 4 della 070b; fatta con la 070c):** la scheda di una busta compro oro non promette «partirà da
  sola» né dice «non partirà», perché quelle buste a «pronta» non arrivano. Lo standalone la promette.
- **Si tengono come sono:** lo scarto dalla coda (D-27) conta come email di pronta «già detta» (Dubbio 3 della 070b);
  le raccomandazioni 1–5 della 069c; `{telegram_optin}` nella frase del popup va con la T4.7.
- **Banco con l'email accesa:** una seconda istanza Go con l'email accesa e un SMTP `.invalid`, se costa poco;
  dove metterla lo decide la sessione del porting.
- **Via:** T4.7 (Telegram) → T4.8 (Artigiani); finita la T4.8 ci si ferma.

## Decisioni di Stefano del 13/11/2026 (risposte ai Dubbi di 072, 073, 074)

Via del coordinatore del 13/11: «ok a tutto» alle raccomandazioni delle ricevute 072, 073 e 074. In particolare:
- **074b prima della 075:** banco con il canale Telegram acceso (Bot API finto nella rete interna), con il piano
  della ricevuta 074. **Fatta con la 074b** (sezione «Banco con il canale Telegram acceso»).
- **«📲 Avvisa cliente» anche su Telegram** (come lo standalone, slice 145): una slice piccola dopo la 075.
- **Mancato avviso del preventivo alla chat del negozio:** sì alla riga d'audit `repair.quote_notice_not_queued`,
  solo nel Go (una D nuova, numerata dalla slice che la fa), con lo stesso motivo di `repair.message_notice_not_queued`.
- **Con la 075:** l'offset vecchio dopo una settimana di silenzio e la rilettura con `limit: 1` quando la risposta di
  `getUpdates` supera gli 8 MiB (Dubbi 1 e 7 della 073).
- **Testi del popup** («Notifiche Telegram ai clienti spente…» e gli altri solo Go): si rivedono con il manuale.
- **Si tengono come sono:** canale disponibile con i due interruttori e l'indirizzo pubblico (072, 074), seconda
  serratura del nome del bot, «Disattiva» senza limite, `/privacy` senza indirizzo pubblico con la risposta fissa,
  limiti sull'ora del sistema, numero D-82 nei testi Telegram, popup di un'ora nella memoria del server.
- **Cutover** (le note le tiene il coordinatore nel promemoria): spegnere lo standalone prima di accendere «Invio
  Telegram» nel Go; accenderlo subito dopo la revisione D-27.

Risposte del 13/11 ai Dubbi ancora aperti di 055–063 (via del coordinatore): ok alle raccomandazioni mandate il 12/11,
con queste scelte, da fare in una slice piccola dentro il tetto:
- **Date della pagina del cliente** (`/r/<token>` e `/stato`): gg/mm/aaaa come i PDF della D-80, non AAAA-MM-GG dello
  standalone (Dubbio 1 della 061; D nuova, numerata dalla slice che la fa). **Fatta con la 074c: D-84** (sotto).
- **Numero della busta senza «N.» anche sulla pagina del cliente:** il cliente legge «45/26» ovunque, come nei
  messaggi della D-82 (Dubbio 2 della 061; D nuova o estensione della D-82, da scegliere nella slice). **Fatta con
  la 074c, come estensione della D-82** (vedi la riga D-82).
- **D-84 (CLI-01, CLI-16, CLI-27: date della pagina del cliente; fatta con la 074c).** Sulla pagina del cliente
  (`/r/<token>`, la pagina storica `/stato/<busta>/<anno>` e l'anteprima del negozio) «Accettata», «Consegna
  prevista», «Pronta dal» e «Consegnata» sono gg/mm/aaaa («05/11/2026»), come i PDF della D-80; lo standalone le
  scrive AAAA-MM-GG, com'è nel database. Un testo scritto al posto della data di consegna («fine mese») resta
  com'è. Le altre date che il cliente vede (risposta al preventivo, ore della conversazione) erano già
  gg/mm/aaaa hh:mm (`Clock.stamp/1`, come lo standalone). Nel Go: `paginaDove` (`internal/buste/pagina_cliente.go`)
  con `dataGiornoMeseAnno`. Nel banco: normalizzatore `D-84` (`tools/banco/confronto.go`, solo una data
  AAAA-MM-GG da sola nel `<dd>` di quelle quattro righe) sui passi di `pagina-cliente` (0-2), `anteprima-cliente`
  e `anteprima-cliente-dipendente` (3-8) che confrontano `.space-y-7@1/dl`.
- **Banner d'emergenza** sulle pagine pubbliche (Dubbio 5 della 061): si decide al cutover; la nota la tiene il
  coordinatore nel promemoria.

Risposte del 13/11 ai Dubbi della 071 (via del coordinatore): ok a tutte le raccomandazioni. Si tengono i due
interruttori Telegram (il lettore legge solo con tutti e due accesi), la prova subito senza coda, la descrizione di
Telegram nell'errore salvato (senza token, tagliata a 500), l'audit di «Notifiche Telegram ai clienti» anche a stato
invariato; il banco con i canali accesi è la 074b; i testi solo Go si rivedono con il manuale. **Reset della password
via Telegram (ACC-13):** una slice piccola dopo la T4.7, insieme all'email del reset.

**Ordine al prossimo via:** slice piccola della pagina del cliente (date e numero, sopra; fatta con la 074c), poi 074b, poi 075.

## Decisioni di Stefano del 14/11/2026 (risposte ai Dubbi di 075 e 076)

Via del coordinatore del 14/11, «ok a tutto» di Stefano:
- **075, Dubbio 6:** il lettore del bot dimentica l'offset dopo **6 giorni** senza scriverlo, non 7 (Telegram conta la
  settimana dall'arrivo dell'ultimo aggiornamento, il Go da quando ha scritto l'offset). Aggiornare la D-85 e i testi
  collegati nella prima slice utile. **Fatto con la slice 077** (`offsetDimenticato`, test e testi).
- **075, Dubbio 7:** una busta tornata da un'oreficeria a un cliente diretto riapre le risposte Telegram della vecchia
  chat, come lo standalone. Nessuna modifica.
- **076, Dubbio 3: eccezione alla D-02 solo per gli artigiani.** Il Go accetta anche i link Phoenix.Token
  `/artigiano/:repair_id?t=` dello standalone, verificati con il suo segreto, oltre ai vecchi link Flask (ART-26): i
  link già mandati agli artigiani non si rompono. D nuova (**D-87**) nell'inventario; si fa con la slice dei link
  pubblici dell'artigiano (084 nel piano della 076). Per conferma e disiscrizione email la D-02 resta com'è.
- **076, Dubbio 5:** con l'eccezione l'elenco delle buste da rimandare agli artigiani non serve; niente promemoria.
- **076, Dubbio 6 (banco s076p, `crea.campo-nome`): D-88.** Dopo un «+ Aggiungi» riuscito nell'anagrafica degli
  artigiani il Go svuota il modulo, come vuole il codice dello standalone (`assign` di un modulo nuovo); nello
  standalone il nome scritto resta nel campo per un effetto di LiveView (il valore del server resta `nil`, nessuna
  differenza mandata), salvo subito dopo un rifiuto. Così un secondo tocco non dà il falso «Esiste già un artigiano…».
  Fatta con la slice 076 (il Go già svuotava); nel banco due righe D-88 di livello browser in `bench/deviazioni.json`.

## Decisioni di Stefano del 15/11/2026 (risposte ai Dubbi di 077, 078, 079 e 080)

Via del coordinatore del 15/11, «ok a tutto» di Stefano: tutto resta com'è, come raccomandato nelle ricevute (la
riga «Deciso da Stefano il 15/11/2026» sotto ogni dubbio, registrata con la slice 081). I punti della 078 e della 080
non sono deviazioni. Tre deviazioni nuove, numerate dal coordinatore nell'inventario privato:
- **D-89 (ART-08, ART-09; slice 077 e 079).** Il numero scritto o la scansione nella consegna all'artigiano che vale per
  più buste (con la D-64 il numero scritto è quello dell'etichetta): il Go tiene solo le buste aggiungibili, poi l'anno
  in corso, e se ne resta più d'una rifiuta con «Più buste corrispondono a «X»: scrivi il numero con l'anno o usa il
  QR.» senza aggiungere niente, anche con la pistola nel caso «numero interno contro etichetta»; lo standalone cerca il
  numero interno, che è unico. L'alternativa (la busta senza etichetta per la forma «N/AA») solo se il caso capita in
  negozio. Nessuna riga nel banco: nessuno scenario scrive un numero che vale per più buste.
- **D-90 (ART-09; slice 079).** Mentre lo scanner della consegna è aperto il Go mostra esito e conto nella finestra e
  rilegge la lista sotto solo alla chiusura; lo standalone (LiveView) la ridisegna a ogni scansione. Nessuna riga nel
  banco: `sessioni-consegna` confronta la lista dopo la chiusura, dove le due app sono uguali.
- **D-91 (ART-08; slice 079).** Nel Go «Aggiungi» del campo «Numero busta» ricarica la pagina e la pistola resta muta;
  nello standalone, dopo un tocco su 📷, anche l'aggiunta dal campo suona. Nessuna riga nel banco: il suono non si
  confronta.
- **D-92 (RIP-H-30, RIP-H-31; slice 082; Stefano 15/11/2026).** Una busta nuova prende il numero dell'anno della sua
  data di accettazione. Lo standalone precompila «N° Busta» con il prossimo numero dell'anno in corso
  (`peek_next_bag_number/0`) e usa l'anno della data solo a campo vuoto (`accepted_on_year/1`); il Go, che per la
  D-64 non ha il campo, usa sempre l'anno della data. Nessuna riga nel banco: gli scenari scrivono nello standalone un
  numero del 2025 (`numeroDel2025`).

## Decisioni di Stefano del 17/11/2026 (risposte ai Dubbi della 091)

Via del coordinatore del 17/11, tutto approvato:
- **D-99 (ORE-10; slice 091).** Nel rifiuto dell'invio del lotto per una busta il Go nomina il numero visibile della busta
  (D-64): «Busta #0045/26: non è più pronta. Niente è partito.» («N. 45/26» senza etichetta), anche per una busta letta
  dal cestino; lo standalone scrive l'id interno («Busta #137: …»). `RifiutoMembro.Numero`, `rifiutoMembro` in
  `internal/buste/lotto_invio.go`. Nel banco `lotti-invio` scrive «#ID» per l'id e «#N» per il numero: due righe D-99 in
  `bench/deviazioni.json` (campo `rifiuto.errore`, PC e telefono).
- **D-46 estesa anche all'invio del lotto (ORE-10):** decisa; vedi il punto della D-46 estesa (porta del lotto).
- **Conferma senza JavaScript prima dell'invio del lotto:** non per la v1, fra le idee per dopo (PIANO, «Idee per
  l'interfaccia»).
- **D-100 (ORE-16, CLI foto, ART foto; slice 092, Stefano 17/11/2026).** Se la riga `repair.public_photo_viewed` non si
  scrive, le foto di `/r/`, `/artigiano/` e `/lotto/` rispondono la pagina d'errore 500 senza byte, invece del 404 «Foto
  non disponibile» dello standalone: nessuna foto esce senza la sua riga. Per `/r/` (062) e `/artigiano/` (084) il Go
  faceva già così senza una D: omissione sanata. Nessuna riga nel banco: il caso non si provoca da HTTP.
- **D-101 (ORE-13, slice 093, Stefano 17/11/2026).** Il messaggio del reinvio preparato con «Prepara di nuovo il
  messaggio» vale 30 minuti e per l'accesso che l'ha preparato; dopo un riavvio, la scadenza, da un'altra sessione, dopo
  un rifiuto al tocco o dopo un nuovo accesso, il tocco su «Apri WhatsApp» / «Scegli contatto» non registra e mostra
  «Prepara di nuovo il messaggio: niente è stato registrato.». Lo standalone tiene il testo nella pagina aperta.
- **D-46 estesa al reinvio del lotto (ORE-13) e D-74 estesa ai lotti** (senza JavaScript il tocco registra e porta a
  wa.me): approvate da Stefano il 17/11/2026 (slice 093).
- **D-102 (JOB-02, JOB-05; slice 094, Stefano 15/11 e 18/11/2026).** Precisa la D-17: sette caselle dei giorni di apertura
  nella pagina Promemoria (chiave solo Go `jobs.giorni_apertura`, giorni ISO separati da virgole; se manca o non si legge
  vale martedì–sabato). I lavori automatici partono solo nei giorni spuntati, «Esegui adesso» sempre; i festivi
  infrasettimanali contano come aperti; la 098 rifiuta il modulo senza nessun giorno.

## Giri dall'artigiano (slice 081, ART-19, tema H)

- **Tabella `giri_artigiani`** (migrazione 0019) al posto della ricostruzione dall'audit di `HandoverHistory.cycles/1`:
  con la conservazione di 5 anni (D-14) la ricostruzione perderebbe i giri vecchi. Le regole sono quelle dello
  standalone, scritte una volta in `internal/buste/giri.go` (`sqlRientriNeiGiri`, `sqlGiriAperti`) e copiate uguali nella
  migrazione (`TestMigrazioneGiriComeIlCodice`): un giro chiuso per ogni `repair.returned_from_artisan*` (consegna di
  `handover_id` solo se intero, nota presente solo con `artisan_notes: true`), uscita = l'ultima `handover.confirmed`
  della stessa consegna che nomina la busta in `repair_ids` e non è dopo il rientro (nessuna: sconosciuta),
  `sganciata` da `handover.bag_detached` (per le «riaperte» della 082). Un giro aperto per ogni busta timbrata da una
  conferma porta l'uscita che il rientro copierà: dopo la pulizia dell'audit il giro non perde l'uscita.
- **Chi scrive:** la conferma (`confermaNellaTransazione`, anche la stampa e conferma della 078: `usciteNeiGiri`), il
  rientro (080) e il rientro automatico dal QR (054) (`rientroNeiGiri`), nella transazione della riga d'audit e a
  partire da quella riga (`audit.ScriviConID`). Dalla 082 anche lo sgancio (`SganciaDalloStorico`); l'eliminazione dallo storico non tocca i giri (la
  consegna resta un numero senza chiave esterna, come nell'audit).
- **Chi ricostruisce:** la migrazione stessa (rieseguibile: tabella e indici solo se mancano, poi da vuota), su un
  database già in uso o già importato; l'import dopo aver copiato audit e buste (`buste.RicostruisciGiri`, nella sua
  transazione), perché la migrazione gira sul database ancora vuoto. Tabella, indici e chiave esterna solo Go in
  `importer.TabelleSoloGo`, `IndiciSoloGo`, `EsterneSoloGo`.
- **Chi legge:** `buste.GiriDellArtigiano` (= `Workshop.artisan_cycles/1`): il giro attuale dalle colonne della busta,
  poi i giri chiusi con il rientro prima della sua uscita, dal più recente (a pari istante nell'ordine dell'audit). I
  giri aperti non si mostrano mai. Il riquadro sulla scheda è della 082.
- **Prova di equivalenza** (`internal/buste/giri_test.go`): `cicliDallAudit` è `cycles/1` riscritta in Go senza SQL
  in comune; a ogni passo dei test dal vivo e sui casi inventati i giri della tabella sono quelli dell'audit, e la
  tabella scritta dal vivo è quella che `RicostruisciGiri` e il file della migrazione rifarebbero.

## Storico delle consegne (slice 082, ART-15…18, ART-19 riquadro)

- **Pagine** (`internal/web/storico.go`, `pagine/storico.html`; dominio `internal/buste/storico.go`): l'elenco
  `GET /handovers/history` (personale; filtri GET artigiano, stato, `data_da`, `data_a`, `p` come `normalize/1`; 30
  per pagina, una pagina oltre la fine è l'ultima; `handover.history_viewed` con i soli nomi dei filtri, scritta prima
  della lettura: se non si scrive, niente pagina) e il dettaglio (`handover.history_detail_viewed` per ogni id
  leggibile, anche uno che non c'è; poi «Consegna non trovata.» all'elenco).
- **Rotta del dettaglio:** `GET /handovers/{storico}/{id}` (registrata con `rottaConGuscioSe`: un primo segmento che
  non è «history» è il 404 prima della catena, anche per chi non è entrato; `percorsoDi` dei test mette «history» al
  posto di `{storico}`), non `/handovers/history/{id}`: il mux di Go rifiuta quel
  modello accanto a `GET /handovers/{id}/labels.pdf` (tutti e due prendono `/handovers/history/labels.pdf`, nessuno è
  più preciso). Anche il gestore vuole `storico = "history"`;
  `/handovers/history/labels.pdf` resta il PDF della consegna «history» (404), come nello standalone dove
  `HandoverLabelController` viene prima nel router. Una rotta nuova a tre segmenti sotto `/handovers` deve tenerne conto.
- **Correzioni del titolare** (`POST /handovers/history/{id}/delete` e `…/detach`): rotte `soloTitolare` come
  `:require_owner_user` dello standalone (il dipendente va a `/` con «Operazione riservata al titolare.»; la tabella dei
  permessi le aveva già così), **non** `f.azioneDelTitolare`: lo standalone non le tratta come eventi di una pagina
  comune. I pulsanti si disegnano solo al titolare e solo su una consegna chiusa, con la domanda dello standalone.
- **Elimina dallo storico:** nella transazione sgancia le buste (anche nel cestino), poi i solleciti
  (`solleciti_artigiani.consegna_artigiano_id` è `ON DELETE RESTRICT`: restano, con busta e artigiano, ART-22), poi
  cancella la consegna e scrive `handover.deleted` (`bags_detached`, `discarded_bags_detached`, `reminders_unlinked`).
  **I giri non cambiano** (la consegna vi è un numero senza chiave, come nell'audit). Nessuna delle due correzioni
  tocca `aggiornato_il` (come `update_all/3`).
- **Sgancia:** la busta fuori dal cestino lascia la consegna chiusa, `handover.bag_detached` con `repair_id`, e i giri
  chiusi della coppia consegna-busta diventano `sganciata = 1` nella stessa transazione: è quello che la ricostruzione
  della 081 legge dalla stessa riga d'audit (`stessiGiri` lo prova).
- **Riaperte** dalla tabella dei giri, non dall'audit: giri chiusi della consegna, non sganciati, di buste fuori dal
  cestino ora in un'altra consegna o in nessuna, dal rientro più recente. `riaperteDallAudit` (test) è `reopened/1`
  riscritta in Go: uguale a ogni passo, tranne un rientro con `handover_id` decimale (7.0), che `reopened/1` prende
  (SQLite: 7.0 = 7) e `cycles/1` no; la tabella segue `cycles/1` (ricevuta 082, Dubbio 2).
- **Riquadro dei giri sulla scheda** (`#repair-artisan-cycles`, dopo la nota dell'ultimo rientro): solo con un giro
  passato; letto in `disegnaScheda` con `GiriDellArtigiano`.
- **Banco:** HTTP `storico-consegne` e `storico-consegne-dipendente` (consegna 1 della cattura, aperta: le correzioni
  sono rifiutate, nessun dato cambia); browser `storico-correzioni` con dati suoi (due buste del 2025 per vista, una
  consegna di «Incastonatore Sintetico» chiusa dai rientri «Nessun cambio di stato», poi sganciata ed eliminata; alla
  fine l'artigiano tolto alle buste). Le letture dello storico non si contano nel browser: la LiveView le scrive due
  volte (primo disegno e collegamento).

## Solleciti all'artigiano (slice 083, ART-20…22, ART-07 contatore)

- **Dominio** `internal/buste/solleciti.go` (= `Workshop.ArtisanReminders` e `record_artisan_reminder/4`): una busta è in
  ritardo se le sue colonne la dicono fuori (`consegnata_artigiano_il` senza `rientrata_artigiano_il`), con consegna e
  artigiano, uscita da almeno N giorni (limite compreso, confine = adesso − N·86 400 s; le buste nel cestino si escludono, come lo
  standalone con `Repo.prepare_query/3`: 097b). Giorni fuori interi di 86 400 s, troncati: **non** dipendono dall'ora del negozio. Gruppi per
  artigiano **della consegna**. La soglia (`SogliaSolleciti`): parametro `giorni` non vuoto, altrimenti
  `solleciti_artigiano_giorni`, poi `Integer.parse/1` per intero e `max(1, n)`; un testo illeggibile è 14 anche con
  l'impostazione. È la soglia che servirà a JOB-09 (T4.9): `ContaSollecitiPendenti` e `sqlRitardo` sono il punto da
  riusare.
- **Pagina** `GET /handovers/reminders` (personale) con `handover.reminders_viewed`; `POST /handovers/reminders`
  (`artigiano`, `giorni`, `wa`) registra il sollecito (una riga `solleciti_artigiani` e `repair.artisan_reminded` per
  busta, buste rilette nella transazione). Dopo il sollecito (o il rifiuto) la pagina torna con il segno di sessione
  `DopoSollecito`: niente riga di lettura, come l'evento LiveView che non rifà `handle_params`.
- **WhatsApp (D-74):** «📱 WhatsApp sollecito» e «👥 Scegli contatto» sono moduli `data-avviso-invio`
  `data-channel="whatsapp"` (lo stesso gestore di «Invia» del popup): con JavaScript il JSON `{"url": …}` dopo la
  scrittura, la scheda aperta al tocco a wa.me, la pagina alla lista; senza, il redirect a wa.me. Niente coda in uscita.
- Contatore `#artisan-reminders-count` nella lista consegne (solo sopra zero); riquadro `#repair-artisan-reminders`
  sulla scheda dopo i giri. Il link «⏰ Solleciti pendenti» della lista, dello storico e del menu segue `haRotta`.
- **Banco:** HTTP `solleciti` (legge e basta); browser `consegne-solleciti` sulla 4/26 della cattura, solo sul PC e
  prima di `pannello-qr`: **eccezione ai dati propri** (una busta nuova non può essere in ritardo), motivata nel
  commento dello scenario in `banco.mjs` e nella ricevuta 083 (Dubbio 1).

## Lavori automatici: il pianificatore (slice 094, JOB-01…04, D-16, D-17)

- **Dove:** `internal/jobs` (`Pianificatore`, `Config`, `Lavoro`); lo fa partire `gestrip serve` (`cmd/gestrip/main.go`)
  fra i lavori in sottofondo, sulla stessa coda in uscita. I lavori veri si registrano in `lavoriAutomatici()`
  (`main.go`): ritardi (095), solleciti (096) e briefing (097), ognuno con il suo registro `lavoro=<nome>`. Un nome senza lavoro registrato non è mai
  dovuto e non lascia righe.
- **Giro al minuto** (`jobs.OgniQuanto`, `Pianificatore.Lavoro`): prima l'attesa, poi il giro, come lo standalone (il
  primo giro un minuto dopo l'avvio). Un giro che va in panico o non riesce si scrive nel registro (solo il tipo) e
  non ferma niente; allo stop `serve` lo ferma e lo aspetta prima di chiudere il database.
- **Dovuto** (`Dovuto`, `due?/2`): lavoro registrato, giorno aperto (D-17), attivo, ora del negozio raggiunta
  (confronto sull'ora del muro di Roma: con il salto delle 02:00 un lavoro delle 02:30 parte alle 03:00), almeno un
  destinatario (D-16), nessuna esecuzione automatica `ok` nel giorno del negozio e meno di 3 in tutto. Lo stato è nel
  database: un riavvio non rifà un lavoro riuscito oggi; un giorno con l'apparecchio spento non si recupera.
- **Impostazioni** (`LeggiConfig`): le chiavi dello standalone `jobs.<lavoro>_enabled` e `jobs.<lavoro>_hour`
  (predefiniti 09:00 e 10:00 accesi, briefing 08:00 spento; letture «spento/acceso» di JOB-05; ora `HH:MM` o
  `HH:MM:SS`, altrimenti la predefinita) e la chiave **solo Go** `jobs.giorni_apertura` (D-17, D-102): giorni ISO separati
  da virgole, «1» lunedì … «7» domenica; senza riga, vuota o illeggibile → martedì–sabato. La scrive la pagina della 098.
- **Destinatari** (`Destinazioni`): il negozio stesso (`Destinatario: "shop"`) su ogni canale attivo per la coda
  (`CanaleAttivo`: configurato e acceso, D-03), email al mittente SMTP e Telegram alla chat del negozio, in
  quest'ordine; il briefing solo Telegram. **D-16:** senza destinatari il lavoro non è dovuto, il giro non lo esegue
  e `EseguiAMano` dà `ErrSenzaCanali` senza scrivere niente (nessuna riga «Saltato»).
- **Storico** (`esecuzioni_lavori`, migrazione 0011): una riga per esecuzione, mai modificata, con `jobs.ran`
  (`risorsa` `job`, `id_risorsa` il lavoro, esito, dati `job`, `trigger`, `notices`, `messages`) nella stessa
  transazione; firmata `scheduler` se automatica. Un lavoro che va in panico o torna con un errore è `failed` con
  «Il job si è interrotto. Il dettaglio è nel log dell'applicazione.» e zero conteggi; nel registro `nome` e tipo,
  più lo stack per un panico (ACC-19), mai il valore né il testo dell'errore. Dettaglio tagliato a 500 grafemi,
  vuoto → NULL; conteggi negativi → 0. Uno stop mentre il lavoro gira: se il lavoro torna con l'errore dello stop
  niente riga (il giorno resta da fare); se torna con un risultato, anche fallito, la riga si scrive con i conteggi.
- **Mai due esecuzioni insieme:** giro, `Dovuto` ed `EseguiAMano` passano dallo stesso lucchetto del pianificatore
  (uno per processo); un'esecuzione a mano aspetta il giro in corso. Fra due processi sullo stesso database non c'è
  blocco, come nello standalone: sull'apparecchio `serve` è uno solo.
- **Banco:** i due Go del banco partono con `-senza-lavori-automatici` (il pianificatore non parte), anche quello
  con Telegram acceso (074b), dove ritardi e solleciti sarebbero dovuti. Lo scheduler dello standalone del banco
  parte con l'applicazione; dalla slice 100 i tre lavori sono **spenti nei suoi dati** e non scrive più righe «Saltato» né audit
  (sezione «Banco: lavori automatici spenti nello standalone»).

## Lavoro «Buste in ritardo» (slice 095, JOB-07, JOB-08)

- **Dove:** `internal/jobs/ritardi.go` (`LavoroRitardi`, `NuoviRitardi(d, coda)`), registrato come `jobs.Ritardi` in
  `lavoriAutomatici` di `serve`, sulla stessa coda (`AccodaIn`). La soglia è `Config.GiorniRitardi()` (chiave dello
  standalone `jobs.overdue_days`, da 1 a 365, altrimenti 1); la chiave del silenziamento `jobs.overdue.silenced_history`
  la porta l'import. Gli stati chiusi vengono da `buste.StatiChiusi()`.
- **In ritardo:** `data_prevista` presente e al più oggi (negozio) meno la soglia, stato non chiuso (consegnata,
  annullata, chiusa), non nel cestino, `ritardo_avvisato_il` vuoto. La riapertura dopo una data nuova (slice 211) è
  in `buste.Modifica`, non qui.
- **Timbro e messaggi:** una transazione per busta: `UPDATE … SET ritardo_avvisato_il WHERE id AND <tutto il
  predicato> RETURNING` (slice 213: il messaggio si compone dalla riga timbrata; numero dei testi, D-64/D-82, dalla
  096 anche nei motivi del dettaglio), poi un messaggio per destinatario
  (negozio, email poi Telegram, `busta_id` NULL) nella stessa transazione. `aggiornato_il` non cambia (come
  `update_all`). Un rifiuto della coda (`ErrMessaggioNonValido`, `ErrSoloAMano`: «la coda ha rifiutato il messaggio
  della busta N»; `ErrNonConfigurato`: «il canale X non è più configurato») annulla timbro e messaggi di quella busta
  e il lavoro va avanti: esito `failed` con «<frase> N non riuscite: <primo motivo>»; nel registro id della busta e
  motivo. Anche una data promessa illeggibile (`2026-02-31`, che la colonna accetta) è un rifiuto della busta («data
  promessa non valida della busta N»). Ogni altro errore (database) ferma il lavoro come l'eccezione dello standalone
  (`DettaglioInterrotto`). Chiamato senza destinatari il lavoro dà `ErrSenzaCanali` e non timbra niente.
- **Silenziamento (JOB-08):** senza la chiave, in una transazione, le buste in ritardo da 7 giorni o più si timbrano
  senza messaggi e la chiave si scrive «1»; la frase aggiunge «N buste storiche silenziate alla prima esecuzione.»
  (con 1: «1 busta storiche…», come lo standalone); dopo il commit una riga Info nel registro con il numero. Succede alla prima esecuzione che arriva al lavoro: con un canale
  e il lavoro acceso, anche «Esegui adesso»; non senza canali (D-16), non con il lavoro spento, non nei giorni chiusi.
- **Stop:** con il contesto finito il lavoro torna con l'errore del contesto (niente riga, il giorno resta da fare);
  le buste già timbrate non si ripetono dopo il riavvio. Recupero D-17: il primo giorno aperto trova da solo le buste
  diventate in ritardo nei giorni chiusi.

## Lavoro «Solleciti artigiano» (slice 096, JOB-09)

- **Dove:** `internal/jobs/solleciti.go` (`LavoroSolleciti`, `NuoviSolleciti(d, coda)`), registrato come
  `jobs.Solleciti` in `lavoriAutomatici` di `serve` (registro con `lavoro=solleciti`), sulla stessa coda (`AccodaIn`).
  Il lato del dominio è `buste.TimbraSollecitiDelGiro` (`internal/buste/solleciti_giro.go`), che riusa la soglia e
  la query della pagina dei solleciti (083: `sogliaSolleciti`, `busteInRitardoDove` con `sqlRitardo`): **una sola
  definizione** di «busta da sollecitare» per pagina, contatore e lavoro.
- **Buste e soglia:** quelle della pagina (fuori bottega con consegna e artigiano, uscite da almeno N·86 400 s,
  fuori dal cestino: 097b), soglia `solleciti_artigiano_giorni` letta come `threshold_days/1` (14 di default, nessun
  tetto). I giorni sono blocchi di 86 400 s dall'uscita, **non** giorni del calendario del negozio, come la pagina.
- **Per giro:** `sollecito_avvisato_il` vale solo se è uguale o dopo `consegnata_artigiano_il` (l'uscita del giro in
  corso). Nessun percorso azzera il timbro: una nuova consegna lo rende vecchio da sola. Una busta nominata e ancora
  fuori non si nomina più; rientrata e mandata di nuovo, si nomina quando passa la soglia del giro nuovo.
- **Tutto o niente:** una transazione per esecuzione: elenco (dentro la transazione), timbri con la guardia del giro,
  un riepilogo per destinatario (negozio, email poi Telegram, `busta_id` NULL, oggetto «Solleciti artigiano»,
  testo «🧰 Solleciti artigiano» / «N buste fuori bottega da oltre <soglia> giorni.» / «<artigiano>: <numero>
  (Ngg), …»; numero dei testi, D-64/D-82). Un rifiuto della coda annulla timbri e riepiloghi già accodati: esito
  `failed`, conteggi zero, «La coda ha rifiutato il riepilogo dei solleciti.» o «Il canale X non è più
  configurato.»; nel registro solo il motivo. Un altro errore (database) annulla lo stesso e la riga è
  `DettaglioInterrotto`. Niente da nominare: «Nessuna busta oltre soglia.» (`ok`, nessun messaggio).
- **Non registra un sollecito:** nessuna riga `solleciti_artigiani` né `repair.artisan_reminded`; la pagina continua a
  dire «mai sollecitata». Senza destinatari `ErrSenzaCanali` senza timbri; con lo stop l'errore del contesto, niente
  riga, tutto annullato. Recupero D-17: il primo giorno aperto trova da solo le buste passate oltre soglia nei giorni
  chiusi.

## Lavoro «Briefing del mattino» (slice 097, JOB-10, JOB-11)

- **Dove:** `internal/jobs/briefing.go` (`LavoroBriefing`, `NuovoBriefing(d, coda)`), registrato come `jobs.Briefing` in
  `lavoriAutomatici` (registro `lavoro=briefing`), sulla stessa coda (`AccodaIn`). Le cinque query sono in
  `internal/buste/briefing.go` (`LeggiBriefing`), che riusa `condizionePronta` (Repair.ready_query, ora anche dentro
  `condizioneDaAvvisare`), `statiChiusiSQL` e, per fuori bottega, `busteInRitardo` della pagina dei solleciti (il cestino è già fuori in `sqlRitardo`, 097b).
  Spento di default alle 08:00 (094); destinatario solo Telegram (D-16: senza Telegram non parte e non scrive righe).
- **Sezioni** (al più 20 righe, se ne legge una in più: «(21+)» e «…e altre ≥1»): pronte da avvisare, compro oro da
  avvisare, in ritardo, in scadenza 3gg (oggi … oggi+3), fuori bottega da ≥N giorni (`Config.GiorniBriefing()`, chiave
  del Flask `briefing_alert_fuori_bottega_giorni`, 0–365, 0 toglie la sezione, altrimenti 14). **Senza le buste del
  cestino**, in tutte e cinque (nello standalone le filtra `Repo.prepare_query/3`). Numero dei testi (D-64/D-82).
  Niente da dire: «🌅 Briefing gg/mm — nulla all'orizzonte ☕», mandato lo stesso. Nessun timbro sulle buste.
- **Divisione (JOB-11):** misura in unità UTF-16 (`unitaUTF16`), parti fino a 4000 (`MassimoMessaggio`). `dividi` =
  `pack/1`: blocchi uniti con «\n\n» finché stanno; un blocco oltre il limite si divide fra le righe (almeno una per
  parte), le parti dopo la prima con «<titolo> (segue)»; dentro un blocco troppo lungo una riga oltre 3900 unità si
  taglia fra due grafemi con «…» (`accorcia`, uniseg: mai una coppia surrogata o un'emoji composta a metà). Le parti
  di un blocco diviso si uniscono ai blocchi vicini come blocchi qualunque.
- **Tutto o niente:** lettura e accodamento di tutte le parti, nell'ordine, in una transazione; un rifiuto della coda
  annulla anche le parti già accodate: `failed`, zero conteggi, «La coda ha rifiutato il briefing.» (anche
  `ErrSoloAMano`) o «Il bot Telegram non è più configurato.»; nel registro solo il motivo. Un altro errore annulla lo
  stesso (`DettaglioInterrotto`). Riuscito: «N voci in M messaggi.» o «Nessuna voce: inviato il briefing vuoto in 1
  messaggio.». Stop: errore del contesto, niente riga, niente in coda. Le parti sono righe separate: dopo un errore
  temporaneo dell'invio possono arrivare fuori ordine (come lo standalone).

## Pagina Promemoria e job (slice 098, JOB-05, JOB-06)

- **Dove:** `internal/web/promemoria.go` e `pagine/promemoria.html` (come JobsLive dello standalone, senza LiveView); il
  dominio in `internal/jobs/pagina.go` (`LeggiValori`, `Pianificatore.SalvaConfig`, `Pianificatore.Pagina`). La faccia lo
  riceve in `web.Dipendenze.Lavori`: `serve` crea il pianificatore **sempre** e lo passa alla pagina; con
  `-senza-lavori-automatici` (sviluppo e banco) parte solo il giro al minuto in meno, «Esegui adesso» resta.
- **Valori del modulo** (`Config.get/0`): «Attivo» con le letture «spento/acceso» della 094, l'ora com'è scritta (senza
  riga o di soli spazi la predefinita; un'ora salvata illeggibile si mostra com'è), le soglie lette (ritardi 1–365,
  briefing 0–365, altrimenti le predefinite; solleciti con `buste.SogliaSollecitiCon`, la lettura della pagina dei
  solleciti). Campi `jobs[<lavoro>_enabled|hour|days]`, id `jobs_<campo>`, con il nascosto «false» prima della casella.
- **Salvataggio** (`Config.put/2`): un campo non mandato resta com'è; vuoto o di soli spazi «Campo obbligatorio.»; un
  vero/falso diverso da `true/1/false/0` o una soglia che non è un intero (`Integer.parse` per intero) «Valore non
  valido.»; l'ora `HH:MM` o `HH:MM:SS` (Dubbio 3 della 094: anche al salvataggio più stretta dello standalone, che con
  `Time.from_iso8601` prende anche `07:45:00.5`; il Go lo rifiuta), altrimenti «Orario non valido: usare HH:MM.», controllata
  **anche quella salvata** (`validate_hour` guarda il campo del changeset: un'ora salvata illeggibile ferma ogni
  salvataggio finché non la si riscrive, come lo standalone); le soglie cambiate con i messaggi dello standalone
  (ritardi 1–365, solleciti almeno 1 senza massimo, anche oltre i 64 bit, briefing 0–365). Poi, in una transazione, le
  **sole chiavi cambiate** (vero «1», falso «0», l'ora com'è, la soglia come intero; solleciti in
  `solleciti_artigiano_giorni`, briefing in `briefing_alert_fuori_bottega_giorni`) e `jobs.configured` (risorsa `jobs`,
  id `config`, esito `ok`) con `{"changed": [...]}`, i soli nomi dei campi, anche vuoto. Un rifiuto non scrive niente e
  ridisegna con i valori mandati; un modulo senza la mappa `jobs[…]` «Richiesta non riconosciuta: nessuna impostazione
  salvata.»; un guasto «Impostazioni non salvate: riprova.» (nel registro solo il tipo).
- **Giorni di apertura (D-102, solo Go):** sette caselle `jobs[giorni_apertura][]` (valori 1…7) con un nascosto vuoto,
  così il campo c'è anche senza caselle spuntate: senza nessun giorno «Scegli almeno un giorno di apertura.», un valore
  che non è un giorno «Valore non valido.», campo assente = giorni invariati. Si scrive `jobs.giorni_apertura` in
  ordine («1,3,7») solo se cambia; nel `changed` dell'audit il nome `giorni_apertura`.
- **«▶ Esegui adesso»** (`run`): solo i tre nomi (`jobs.Nomi`); un altro nome torna alla pagina senza fare niente.
  `EseguiAMano` con il contesto della richiesta senza la sua cancellazione (il lavoro finisce anche se il browser chiude).
  Riuscito «Job eseguito.»; l'esito è nello storico (un lavoro spento è «Saltato» «Job disattivato dal titolare.», JOB-06,
  e non conta per il giorno).
- **Resa della D-16** (risposta B del piano T4.9): la pagina c'è e si salva anche senza canali, con la riga «I promemoria
  partono solo con email o Telegram attivi…» (`#jobs-no-channels`) e **nessun** «▶ Esegui adesso»; il pulsante c'è solo
  per un lavoro con un destinatario adesso (`Pianificatore.Destinazioni`): con la sola email il briefing non lo ha e ha
  la riga «Il briefing parte solo con Telegram attivo.». Un «Esegui adesso» mandato lo stesso dà `ErrSenzaCanali`: nessuna
  riga e «Niente è stato eseguito: …». L'ordine dei controlli è quello di `EseguiAMano` (prima i canali, poi
  l'interruttore, Dubbio 6 della 094): senza canali un lavoro spento non scrive «Saltato» (lo standalone sì). Le righe
  importate (anche «Saltato») restano nello storico. **Testi solo Go approvati da Stefano il 19/11/2026** (Dubbio 1 della
  ricevuta 098; nessuna D nuova): la riga senza canali, «Il briefing parte solo con Telegram attivo.», «Niente è stato
  eseguito: …», «Scegli almeno un giorno di apertura.», i nomi dei giorni da «Lunedì» a «Domenica» e, con un solo
  cambio chiesto da lui, «Non è stato possibile avviare il promemoria: riprova.» al posto di «…eseguire il job: riprova.»
  (`msgEseguiNonRiuscito`, slice 100).
- **Storico:** le ultime 20 righe (`jobs.VociPagina`), la più recente prima, con l'ora del negozio (`clock.Timbro`),
  «Automatico»/«A mano», «buste N · messaggi N» e il dettaglio; sotto ogni lavoro «Ultima esecuzione: <ora> · <esito>» o
  «mai».
- **Banco:** HTTP `promemoria` (due app: pagina, campi e valori; D-16 «▶ Esegui adesso» e la riga senza canali, D-102 i
  giorni in `bench/deviazioni.json`; passi `solo_go` per «Esegui adesso» senza canali, l'ora non valida, nessun giorno,
  un salvataggio e il ripristino), `promemoria-dipendente` (302 a /), e `telegram-promemoria` sul Go con Telegram (074b,
  dopo `telegram-acceso`): «Esegui adesso» dei tre lavori, i testi in `/_prova/mandati` della chat del negozio solo per
  presenza (il numero delle buste in ritardo dipende dalla data del giro), il briefing acceso e rispento. Lo storico dello
  standalone non si confronta (scelta della 098, quando il suo scheduler scriveva «Saltato» dalle 09:00/10:00): dalla
  slice 100 lo scheduler del banco non scrive più, le tre caselle «Attivo» sono spente nei dati delle due app e
  `promemoria` lo fissa nel primo passo, solo con `-dati sintetici-import` (`atteso_dati`: `jobs_overdue_enabled:checked` e
  `jobs_chases_enabled:checked` «senza attributo»). Lo storico e «Ultima esecuzione» di ritardi e solleciti sarebbero ora stabili; confrontarli è una
  decisione del coordinatore.

## Stima dei tempi sulla scheda (slice 101, RIP-S-03, RIP-H-55 parte negozio)

- **Un solo modello.** La scheda usa lo stesso modello della pagina del cliente (slice 061, `internal/buste/pagina_cliente.go`:
  `modelloStima`, campione di 365 giorni, durata 0-90, almeno 20 buste di bottega, supplemento artigiano misurato da 10 buste
  oppure 7-8 dalle impostazioni). Il dominio è `stimaDellaScheda` (`internal/buste/stima_scheda.go`, `RepairEstimate.for_shop/2`),
  chiamata da `leggiScheda`: sta in `Scheda.Stima`, `nil` per una busta che non è una riparazione aperta (in lavorazione,
  preventivo, in attesa materiali; mai un compro oro). Leggere la scheda non scrive altro che `repair.viewed`, come prima.
- **Il negozio la vede sempre**, anche con l'interruttore `stima_tempi_attiva` spento o con una consegna scritta (data o testo non
  vuoto, `Consegna.esiste` = `date_source/2 != :none`); il cliente no (`stimaPerIlCliente`, 061). Per questo `StimaScheda.Nascosta` dice
  `spenta` (guardata per prima, come `hidden_reason/2`), `consegna` o niente.
- **Testi dello standalone**, nel riquadro `#repair-time-estimate` (dopo «Consegna entro», prima di «Pronta dal»):
  «Pronta di solito in N-M giorni dal ritiro · su X buste» (X = buste di bottega più quelle passate dall'artigiano); senza base «Dati
  insufficienti: servono almeno 20 buste pronte (ora N)» (N = solo quelle di bottega); «Giorni in più: N» solo se N > 0, con o senza
  stima; «non mostrata ai clienti: stima spenta nelle impostazioni» o «… c'è una data di consegna» solo accanto a un intervallo.
  L'apostrofo esce come `&#39;`, come in Phoenix.
- **Banco:** scenario `scheda-stima` (RIP-S-03; RIP-H-55 si chiude con la 102; buste 1, 5, 10, 24 aperte, 2 chiusa, 34 compro oro; titolare e dipendente).
  La catena ha probabilmente meno di 20 buste pronte nel campione (ragionato, non misurato: dipende anche dalle date del giro, perché
  molte buste del banco sono accettate il 02/03/2026 e diventano pronte il giorno del giro, e oltre 90 giorni non contano): le due app
  devono dire lo stesso «Dati insufficienti (ora N)», qualunque sia N. Lo
  scenario gira con l'interruttore com'è nei dati (spento: la catena non lo scrive); con meno di 20 buste l'interruttore non cambia
  niente sulla scheda, e il caso pieno (intervallo, «su X buste», i due perché) è coperto dai test Go
  (`internal/buste/stima_scheda_test.go`, `internal/web/scheda_stima_test.go`). Accendere la stima nelle due app arriva con la 102
  (`/settings/shop`, LiveView nello standalone: non è HTTP semplice).

## Stima dei tempi nelle impostazioni (slice 102, ADM-04, RIP-H-55)

- **Dove:** sezione «Stima tempi» (`#shop-estimate`) di `/settings/shop`, dopo il profilo, solo titolare. Dominio:
  `internal/buste/stima_impostazioni.go` (`PaginaStima`, `SalvaStima`, `Settings.Estimate`); pagina: `internal/web/negozio.go`,
  `pagine/negozio.html`. Il modulo `#estimate-form` manda `POST /settings/shop/stima` (nello standalone è l'evento `save_estimate`,
  senza URL: la rotta è solo del Go) con `estimate[enabled]` (il nascosto «false» e poi la casella, vale l'ultimo valore),
  `estimate[artisan_min]` e `estimate[artisan_max]`; `data-invio-unico` e il token come le altre forme.
- **Le chiavi sono quelle della 061 e della 101** (`stima_tempi_attiva`, `stima_tempi_artigiano_min`, `stima_tempi_artigiano_max`,
  `internal/buste/pagina_cliente.go`): la lettura riparata (`leggiImpostazioniStimaDa`) è una sola, la pagina del cliente, la scheda e questa
  sezione leggono lo stesso. Interruttore acceso solo con «1»; niente riga vale spenta.
- **Regole di `Settings.Estimate.put/2`, copiate:** accesa solo se `enabled` è «true», «1» o «on» (senza il campo o con un campo che non è un
  testo: spenta); i giorni **non si rifiutano**, si riportano in 0-90 (un testo che non dà un intero, con spazi, con la virgola o vuoto, vale
  il valore di partenza 7 o 8; il segno e gli zeri davanti vanno bene), e **il massimo sotto il minimo si alza al minimo** (non si scambiano, non
  si rifiuta). Un campo dei giorni **assente** dal modulo lascia quello che c'è; uno che c'è ma non è un testo (lista o mappa) vale il valore di
  partenza. Un modulo senza la mappa `estimate` è «Richiesta non riconosciuta: nessuna impostazione salvata.» (come il catch-all di
  `handle_event`).
- **Esito:** riuscito, ritorno alla pagina (302, come `push_navigate`) con «Stima tempi salvata.»; la pagina mostra i valori tenuti (già riportati), non
  quelli scritti. Un guasto del database ridisegna la pagina (200) con «Stima tempi non salvata: riprova.» e i valori letti di nuovo, una riga nel
  registro senza i valori. Una lettura che fallisce è la pagina d'errore (500).
- **Audit:** un'unica riga `settings.estimate_updated` su `settings/estimate`, esito `ok`, dati `{"changed": [<chiavi>]}` con i **nomi** delle
  chiavi il cui testo salvato è diverso (una riga che non c'è è cambiata: la prima volta sono tutte e tre; una conferma identica scrive
  `{"changed":[]}`), mai i valori; nella stessa transazione delle tre chiavi.
- **«Oggi»** (`#estimate-today`): «Oggi: Pronta di solito in N-M giorni dal ritiro» oppure «Dati insufficienti: servono almeno 20 buste pronte (ora N)»,
  poi «— X buste in bottega, Y passate dall'artigiano; supplemento artigiano A-B giorni (misurato sui dati | dalle impostazioni).» Il modello è
  quello della 061 (`modelloStima`, con il nuovo campo `misurato`); leggere non scrive.
- **Spenta anche dopo un import:** l'import copia le chiavi `impostazioni` dello standalone com'erano e non ne scrive di sue
  (`internal/importer/stima_test.go`): senza le righe la stima è spenta e il supplemento 7-8.
- **Banco:** HTTP `negozio-stima` (la sezione letta dalle due app: testi, etichette, attributi, valori di partenza; il salvataggio **non si
  può fare in HTTP nello standalone**, è un evento LiveView, come per le altre pagine di impostazioni); browser `stima-negozio` (cinque salvataggi con giorni
  fuori campo, massimo sotto il minimo, testo non intero, campi vuoti e stima spenta; poi la stima **accesa** con la scheda delle buste 10 e 5; alla
  fine **interruttore e giorni rimessi com'erano**, sette righe `settings.estimate_updated` per vista). Il browser rifiuta `min` e `max`
  del campo: lo scenario spegne la convalida del modulo (`noValidate`) e per il testo cambia il tipo del campo in `text`. Il rifiuto del dipendente
  è `negozio-dipendente` (la pagina del titolare lo manda alla home, nelle due app) e `TestStimaNegozioSoloTitolare`. Con meno di 20 buste pronte
  la scheda dice «Dati insufficienti» sia accesa sia spenta: il banco non può distinguere l'interruttore, che lo provano i test Go
  (`TestStimaNegozioInterruttoreSullaScheda`, `TestStimaImpostazioniEPaginaDelCliente`).

## Date di consegna dal dettato (slice 103, D-18)

- **Dove:** `internal/assist/consegna.go`, `DataConsegna(frase, oggi) (time.Time, bool)`: porting parola per parola di
  `consegna.py`, il traduttore di produzione di pc-fisso (Stefano, 19/11/2026, domanda D del piano T4.10: «uguale a
  `consegna.py`; ogni ritocco va fra le idee»). Riceve la **frase di consegna** che il modello ha già separato dal dettato
  (formato corto, D-103), non tutto il dettato: le date da incidere restano fuori perché il modello non le mette in «c»,
  `DataConsegna` non ne sa niente (come `consegna.py`). Il collegamento alla scheda arriva con la 104.
- **«Oggi» è un parametro:** conta solo il giorno di `oggi` nel suo fuso (il chiamante passa l'ora del negozio,
  `clock.Roma`); la data esce a mezzanotte dello stesso fuso. `false` e la data zero = frase non riconosciuta, data vuota.
- **Cosa riconosce, nell'ordine di `consegna.py`** (vince la prima regola che scatta): data con il mese («il 5 novembre»,
  «15 dicembre 2026»; senza anno e già passata → l'anno dopo); «dopodomani»; «domani»/«domattina»; «oggi»/«stasera»;
  «Natale» (il 24/12, dell'anno dopo se passato); «N giorni/settimane/mesi» con N in cifre o in parole (solo la prima
  coppia della frase; «e mezzo» aggiunge 3 giorni alle settimane e 15 ai mesi); «fine» + «settimana» (il sabato, della
  settimana dopo con «prossima»); «fine» + «mese» (l'ultimo del mese corrente); un giorno della settimana (il primo
  nell'ordine lunedì…domenica, non nella frase; detto lo stesso giorno è fra 7 giorni; con un numero dopo, «lunedì 2»,
  vale il giorno del mese); «il N» (questo mese se non è passato, altrimenti il primo dei due mesi dopo che lo ha).
  Orari e parole in più si ignorano; minuscole come Python, solo à è é ì ò ù perdono l'accento (con «í» o «ó» la parola
  non si riconosce), ogni altro segno è uno spazio.
- **Unica differenza voluta:** dove `consegna.py` solleva un'eccezione («lunedì 0», «il 00», «il 29 febbraio» detto
  dopo il 29 di un bisestile, numeri che portano oltre l'anno 9999) il Go dà la data vuota.
- **Test:** `internal/assist/consegna_test.go`, 135 frasi inventate con l'atteso preso da `consegna.py` (fine mese, anno
  nuovo, bisestile, «venerdì» detto di venerdì, orari, maiuscole e accenti, casi negativi). Confronto una tantum con una
  copia di `consegna.py` (slice 103): le 135 frasi e 50 000 frasi a caso (più 1,3 milioni nella revisione), nessuna differenza oltre alle eccezioni.
  RIP-H-42 e RIP-H-46 restano parziali: il segno `// inventario:` si mette quando si chiudono (104-108), perché lo
  strumento di copertura conta chiusa una voce che ha il segno su un test verde.

## Dalla risposta del modello alla scheda (slice 104, D-19, D-20, D-103, D-104)

- **Dove:** `internal/assist/scheda.go` e `telefono.go`. Con il formato corto (D-103) il Go fa il lavoro che in
  produzione fa il traduttore di pc-fisso (`traduttore.py`) prima dello standalone. Dove decide il traduttore il
  comportamento è il suo; dove decide lo standalone (lettura della risposta, campi come testo, telefono normalizzato,
  compro oro) è quello dello standalone.
- **`LeggiRisposta(risposta) (Risposta, bool)`** dice «valida / non valida» (D-19), come `Assist.decode/1`: il testo
  intero, altrimenti il pezzo dalla prima «{» all'ultima «}» (blocco ```json o testo intorno). Non valida: niente
  oggetto, JSON rotto, una chiave `n o b d c p a x` che è un oggetto o una lista (anche vuota), un numero fuori dal
  float64, «c» o «x» vere che non sono testo (lì il traduttore si ferma con un errore). Le altre chiavi si ignorano.
- **`PropostaDa(r, dettato, oggi, negozio) Proposta`**: i campi del modulo della busta nuova (`negozio` dalla
  slice 105). Data di accettazione = oggi
  (il giorno nel fuso di `oggi`); consegna = `DataConsegna` della sola frase «c» (D-18); note = «Busta N — Acconto N € —
  x» (D-20, valori «falsi» per Python esclusi, numeri scritti come Python: lì scrive il traduttore); gli altri campi
  copiati come `to_string/1` dello standalone (null vuoto, `true`/`false`, decimali come `Float.to_string/1` di Elixir:
  la forma più corta fra «1000.0» e «1.0e3», a parità senza esponente, da 2^53 sempre con l'esponente; misurato con
  Elixir 1.18/OTP 27 su 40 386 numeri); telefono = `TelefonoDalDettato` (D-104, `trova_telefono`, con `\d`, `\s`,
  `\b` e `re.I` di Python resi a mano) normalizzato con `telefono.Normalizza` (`internal/telefono`, pacchetto foglia
  condiviso con le anagrafiche);
  compro oro solo da «oro vecchio» / «scheda compro oro» nel dettato (la regola dello standalone, con lo spazio Unicode
  di PCRE). Nome e oreficeria: vedi la sezione della 105.
- **Date da incidere:** la consegna si calcola solo da «c», mai dal dettato o dalla descrizione, come nel traduttore:
  «fede da incidere con la data 12 dicembre 2026» non diventa la consegna (test espliciti). Se il modello mette la data
  da incidere proprio in «c», il Go si comporta come oggi in produzione (la prende per consegna): la regola che la
  escluderebbe è fra le idee (coordinatore, 20/11/2026).
- **`Estrai(chiedi, dettato, oggi, negozio)`**: un solo nuovo tentativo se la risposta non è valida (D-19, domanda F), poi
  `ErrRispostaIllegibile`; dalla 106 anche dopo un errore di `chiedi`, e vale l'esito del secondo. `Domanda(dettato,
  oggi)` scrive il messaggio per il modello («Oggi: venerdì 23/10/2026\nDettato: …», dalla 106 con il dettato senza
  spazi ai bordi). Rete, attesa di 90 s e rifiuti: sezione della 106.
- **Differenze volute dal traduttore:** risposta non JSON → nuovo tentativo e poi errore (il traduttore dava una scheda
  vuota); busta o acconto annidati o infiniti → non valida (il traduttore scriveva «Busta [12]» o «Busta inf»); anche
  `[]` e `{}` vuoti in «b», «a», «c», «x» (il traduttore li ignorava come falsi) e `NaN` → non valida.
- **Test:** `internal/assist/scheda_test.go`, con risposte finte dai banchi `bench/modello/`. Confronto una tantum con
  una copia del traduttore (slice 104): i 1720 casi dei banchi, 20 000 risposte a caso, 100 000 dettati a caso per il
  telefono e 30 000 per il compro oro, nessuna differenza oltre a quelle dette sopra e allo spazio di PCRE per il
  compro oro (U+180E sì, U+001C-U+001F no). RIP-H-42 e RIP-H-43 restano parziali (105-108), senza segno `// inventario:`.

## Nome del cliente e oreficeria dal dettato (slice 105, RIP-H-43)

- **Dove:** `internal/assist/nome.go`, chiamato da `PropostaDa`. Chi decide: **lo standalone**. Il traduttore di
  produzione passa «n» e «o» com'erano (verificato su una copia di `traduttore.py`); le regole sono `read_answer/3`,
  `shape/3`, `person_name/3`, `opening_name/2`, `recognise/2`, `match_oreficeria/2` di `Assist` e
  `assign_customer_pair/3`, `assist_summary/1` di `WorkshopLive`, nello stesso ordine.
- **`Negozio{Clienti []Cliente; Oreficerie []Oreficeria}`**: quello che il negozio conosce, letto dal chiamante (la
  pagina della 108: tutti i clienti come `list_customers/0`, le oreficerie esterne `id > 1`), così `assist` non importa
  `buste`. `Cliente{ID, Nome, Telefono}`, `Oreficeria{ID, Nome}`. L'ordine lo rifà `assist`: per nome e id, confronto
  dei byte come l'`ORDER BY nome, id` di SQLite.
- **Ordine delle regole:** (1) nome dall'apertura del dettato («Cliente/Signora/Signor/Sig.ra/Sig.» + fino alla prima
  punteggiatura o cifra, 1–4 parole con una lettera, nessuna parola come «tel», «anello», «busta»…, nessuna oreficeria
  del negozio dentro), che vince sul nome del modello se questo è vuoto, ha cifre o, senza articoli e particelle davanti,
  è la coda dell'apertura («Lana» di «dalla lana»); (2) maiuscole come `title_case/1` su nome e oreficeria;
  (3) il nome deve essere una persona: vuoto se segnaposto («Cliente», «n/d»…), se comincia con la parola
  «Oreficeria»/«Gioielleria», o se è **uguale** (a parte accenti, spazi, apostrofi e maiuscole) all'oreficeria
  estratta o a una del negozio; (4) `OreficeriaID`: la prima oreficeria per nome e id il cui nome in minuscolo contiene
  quello detto o vi è contenuto, 0 nessuna; (5) riconoscimento solo nei casi certi: un solo nome fra i clienti con
  quel telefono (almeno 7 cifre dopo aver tolto «0039», o «39» davanti a più di 10 cifre), altrimenti un solo nome
  fra i clienti con lo stesso nome confrontabile; più schede con lo stesso nome scritto uguale contano come uno;
  nessuna somiglianza approssimata; (6) coppia del modulo: vince il telefono dettato, se manca quello dei
  clienti con lo stesso nome confrontabile, solo se hanno tutti lo stesso telefono (D-106, dalla 108; lo standalone prende
  il primo per nome e id con lo stesso nome a parte maiuscole e spazi, anche fra omonimi).
- **Correzione:** `NomeCorretto` e `NomeCorrettoDa` (anche vuoto: nome svuotato dalla regola della persona e poi trovato
  dal telefono). `Proposta.Messaggio()` dà «✓ Nome cliente corretto da «X» a «Y». Verifica i dati prima di salvare.» solo
  con `NomeCorrettoDa` non vuoto, altrimenti «✓ Dati estratti. Verifica e correggi prima di salvare.». Lo mostra la 108.
- **Come Elixir:** `String.trim/1` (spazi Unicode), `String.split/1` (non divide su U+00A0, U+2007, U+202F: misurato su
  tutti i caratteri), `String.downcase/1` («İ» → «i̇»), `String.upcase/1` completo («ß» → «SS», con
  `golang.org/x/text/cases`), NFKD con `golang.org/x/text/unicode/norm` (dipendenza nuova della 105, già nella cache dei
  moduli), `\s`, `\d` e `\b` del flag `u` di PCRE scritti per esteso.
- **Test:** `internal/assist/nome_test.go`, tabelle da `assist_test.exs` (go-baseline) con il telefono nel dettato
  (D-104), più i casi di sicurezza (omonimi: dalla 108 telefono proposto solo se è lo stesso per tutti, D-106; telefono corto, «0039»/«39»,
  nomi uguali a parte accenti e spazi, nome che non è una persona, cliente senza telefono o senza nome). Confronto una tantum con una copia delle funzioni di
  `assist.ex` in Elixir 1.18/OTP 27 (docker, senza rete): 200 000 casi a caso senza differenze; un carattere per volta
  (1 112 064) differiscono solo i caratteri che le tabelle Unicode di PCRE di OTP 27 non conoscono come lettere o cifre
  (scritture aggiunte dopo, da U+0560 in su) e quelli non assegnati in Unicode 15: nessuno usato in un nome italiano.

## Collegamento al modello e modello finto (slice 106, D-19, D-103)

- **Dove:** `internal/assist/modello.go`. Chi decide: **lo standalone** (`LocalChat.exchange/3`,
  `Assist.extract_with_model/3`, `assist_refusal/2` di `WorkshopLive`), con il formato corto del traduttore (D-103) e
  l'attesa unica della domanda F.
- **`Collegamento{Config, Attesa}`**, `Config` = `Configurazione{Indirizzo, Modello, Chiave}` (le
  impostazioni `assist.ai.*` della 107). `Usabile()`: indirizzo e modello non vuoti a parte gli spazi.
- **`Chiedi(ctx, istruzioni, testo)`**: POST `<indirizzo senza spazi e senza «/» finali>/v1/chat/completions`, JSON
  `{model, temperature: 0, stream: false, messages}`; le istruzioni come messaggio `system` **solo se non sono vuote**,
  il testo come messaggio `user`; `Authorization: Bearer <chiave senza spazi>` solo se la chiave non è vuota; niente
  proxy dall'ambiente, niente redirect (la chiave va solo all'indirizzo configurato), niente keep-alive (vedi
  Riservatezza), al più 1 MiB di risposta. Solo il 200 vale; il contenuto è `choices[0].message.content` (testo; le
  chiavi con le loro maiuscole, come Jason), il nome del modello è il campo `model` se, senza spazi
  ai bordi, non è vuoto, ha al più 120 caratteri e nessun `\p{C}`, altrimenti quello configurato (`with_model/2`).
- **`(*Collegamento).Estrai(ctx, dettato, oggi, negozio, reg, chi)`** → `Estrazione{Proposta, Modello}`. Nell'ordine:
  `ErrNonConfigurato`; dettato senza spazi ai bordi (`String.trim/1`) contato in grafemi, `ErrTestoCorto` sotto 10 e
  `ErrTestoLungo` sopra 5000 (nessuna richiesta, nessun audit, come lo standalone); poi la sola `Domanda` (formato
  corto, «Oggi: …\nDettato: …») come messaggio dell'utente, **senza messaggio di sistema** (come il traduttore manda al
  modello addestrato; correzione del piano del coordinatore, 20/11/2026); `assist.Estrai` fa al più **un nuovo tentativo**, dopo una risposta illeggibile **o dopo un
  errore** (rete, HTTP, risposta non riconosciuta), e vale l'esito del secondo; tutto dentro **una sola scadenza di
  90 s** (`AttesaModello`; `Attesa` la accorcia nei test): a tempo finito il secondo tentativo non parte.
- **Rifiuti:** `ErrNonConfigurato`, `ErrTestoCorto`, `ErrTestoLungo`, `ErrRispostaIllegibile`, `ErrModello{Motivo}`
  con un motivo fisso: `ai:http:<stato>`, `ai:transport:<genere>` (`:timeout`, `:econnrefused`, `:econnreset`,
  `:ehostunreach`, `:enetunreach`, `:nxdomain`, `:closed`, `:error`, come `inspect/1` di hackney) o
  `ai:risposta non riconosciuta`. `MessaggioDiRifiuto(err)` dà le frasi dello standalone: «Nessun modello è
  configurato: non è stato chiesto niente a nessuno.», «⚠ Testo troppo corto: servono almeno 10 caratteri.», «⚠ Testo
  troppo lungo: al massimo 5000 caratteri.», «⚠ Il modello non ha risposto con una scheda leggibile, riprova.», «⚠ AI
  temporaneamente non disponibile, riprova fra qualche minuto.».
- **Audit `assist.extract`** (risorsa `assist`, id `ai`, attore dal chiamante) con **soli conteggi**, come lo
  standalone: `ok` con `{"characters": N}`, `error` con `{"characters": N, "reason": <motivo>}` (`unreadable_answer`
  per la risposta illeggibile). L'esito della scrittura non si guarda (come `audit/5`). `Registratore` è l'interfaccia
  di `*audit.Registro`.
- **Riservatezza (RIP-H-47):** il collegamento non scrive log, e il trasporto è senza keep-alive perché net/http, con
  una connessione tenuta aperta, scrive nel log standard i byte che un server manda oltre il Content-Length
  («Unsolicited response…»), che possono ripetere il dettato (revisione della 106). Errori, audit e messaggi non
  contengono mai il dettato, la risposta del modello, la chiave o l'indirizzo: niente testo di un errore di rete,
  niente corpo di una risposta. Chiamante andato via = `::closed`, scadenza passata = `::timeout`.
  Lo prova `TestPrivacyDettatoEChiave` (nome, telefono e chiave inventati; 500 che ripete dettato e chiave, attesa
  scaduta, risposta illeggibile che li ripete, busta rotta, rete giù, server spento, byte oltre il Content-Length; log
  `slog` e `log` catturati).
- **Spazi del dettato (correzione portata dalla 105):** `Domanda` toglie gli spazi ai bordi come `.strip()` di Python
  (spazi Unicode più U+001C-U+001F, misurato con Python 3.12 su tutti i caratteri).
- **Modello finto:** `tools/banco/modellofinto` (pacchetto, usabile dai test Go con `httptest`) e
  `banco modello-finto [-ascolta :8082]` (`tools/banco/modellofinto.go`). Risposte in coda con `Programma(...)` o
  `POST /_prova/risposte` (`stato`, `contenuto`, `modello`, `grezzo`, `ritardo_ms`, `chiudi`); a coda vuota una scheda
  buona inventata; uno `stato` fuori da 200-599 si rifiuta con 400; `Richieste()` / `GET /_prova/richieste` per i
  controlli (anche Authorization, Content-Type e Accept); `GET /_prova/salute`. Dalla 107 è un
  container dell'istanza del banco e lo usa lo scenario browser `assistente-salva` (sezione «Assistente: configurazione»).
- **Test:** `internal/assist/modello_test.go`; RIP-H-45, RIP-H-46 e RIP-H-47 restano parziali (pagine e trascrizione
  nelle 107-108), senza segno `// inventario:`.

## Assistente: configurazione (slice 107, ADM-11)

- **Dove:** `internal/assist/config.go` (chiavi, lettura, glossario, indirizzo), `internal/assist/impostazioni.go` (`Impostazioni`:
  pagina, salvataggio, prova), `internal/web/assistente.go`, `pagine/assistente.html`. Chi decide: **lo standalone**
  (`AssistLive`, `Assist.Config`, `Assist.Glossary`); la raccolta dei dettati della stessa pagina è della 114 (sezione
  «Raccolta dei dettati dalla pagina dell'assistente»).
- **Dati:** `assist.ai.endpoint`, `assist.ai.model`, `assist.stt.endpoint`, `assist.stt.glossary` in `impostazioni` (le chiavi
  dello standalone, l'import le porta); `assist.ai.api_key`, `assist.stt.api_key` in `segreti_provider` (D-03, migrazione 0006).
  `LeggiEstrazione` è il punto da cui il client della 106 (`Collegamento`) prende la configurazione salvata (prova; busta nuova
  dalla 108); `LeggiTrascrizione` per la 109. Si rileggono a ogni uso. Un valore che manca o di soli spazi vale assente.
- **Rotte** (tutte `soloTitolare`, con il guscio): `GET /settings/assistente`; `POST /settings/assistente/estrazione` (`save_ai`)
  e `…/trascrizione` (`save_stt`): salvato → 302 con «Impostazione salvata.», rifiutato → la pagina (200) con gli errori sotto i
  campi e i valori mandati (mai la chiave); `POST /settings/assistente/prova` (`test`) → la pagina (200) con l'esito in
  `#ai-probe` (`text-error` sugli errori). Un guasto del database è la pagina d'errore, con una riga nel registro senza valori.
- **Regole di `Config.put/3`, copiate:** campi del modulo `ai[…]`/`stt[…]`, uno che manca resta il salvato; casella della chiave
  vuota o di spazi = tiene; `clear_secret` `true`/`1` la toglie e vince su una chiave scritta; `on` e ogni altro valore
  fuori da `true/1/false/0/vuoto` è «Valore non valido.» (salvataggio rifiutato); obbligatori sui valori finali indirizzo e modello (estrazione) e indirizzo
  (trascrizione) «non può essere vuoto»; indirizzo come `URI.parse/1` di Elixir (`partiIndirizzo`, misurato): schema
  `http`/`https` («deve cominciare con http:// oppure https://») e host non vuoto («manca l'indirizzo del server»); una chiave
  presente con un indirizzo `http` verso un host che non è `127.0.0.1`, `localhost`, `::1`, `[::1]`, `0:0:0:0:0:0:0:1`
  (confronto esatto) è «una chiave non viaggia in chiaro verso un host remoto» (sui valori finali: anche la chiave tenuta);
  glossario cambiato → una voce per riga (virgola e a capo separano, spazi interni a uno), rifiutato se la forma a una riga
  («, ») supera 2000 caratteri; lunghezze 255/120/255/4000 («Troppo lungo (massimo N caratteri).») solo sui campi cambiati.
  Più errori di un campo dal più recente. Salvato: in una transazione i campi della famiglia (nil = ""), la chiave (nil toglie
  la riga) e `assist.provider_configured` (`assist_provider`, `ai`/`stt`, `ok`) con `family`, `changed` (solo i nomi) e
  `configured`.
- **Prova:** `assist.FraseDiProva` (quella dello standalone, senza clienti) con `Collegamento.Estrai` della 106 (attesa,
  nuovo tentativo D-19, audit `assist.extract` con soli conteggi) e un negozio vuoto. «Il modello ha risposto: «<80 grafemi
  della descrizione>».», «Il modello non è configurato: non è stato chiesto niente.», «Il modello ha rifiutato: <motivo>.»,
  «Il modello ha risposto qualcosa che non è un oggetto JSON.».
- **Chiave e indirizzo non si stampano:** `Configurazione` e `ConfigTrascrizione` hanno `Format` (ogni verbo di fmt) e
  `LogValue` con «***» al posto di chiave e indirizzo (che può portare utente e password).
- **Banco:** con `-dati sintetici-import` `banco su` avvia anche il **modello finto** (`grgo-<nome>-modello`, `banco modello-finto
  -ascolta :8082` dal binario del banco, alias `modello.invalid`, niente porte sull'host; `runtime.json` `modello`; `banco giu`
  lo toglie) e il browser riceve `MODELLO_URL`. HTTP `assistente` (la pagina nelle due app) e `assistente-dipendente`; browser
  `assistente-salva` (rifiuti, prova senza configurazione, e con i dati salvataggio sul modello finto, prova con una risposta
  programmata e con due 500, glossario, chiave verso localhost poi cancellata). **Gira in fondo** (`IN_FONDO` in `banco.mjs`, dopo
  `lettore-qr`): lo standalone non lascia togliere indirizzo e modello, quindi alla fine le due app restano configurate sul
  modello finto (chiavi e glossario sì, tolti nel `finally`; scelta approvata dal coordinatore il 20/11/2026). Per questo
  **un secondo `banco esegui` sulla stessa istanza non è confrontabile:** prima di ripetere il giro l'istanza va rifatta
  (`banco giu` + `banco su`), come già vuole il segno `browser_girato`. `banco giu` toglie `grgo-<nome>-modello` anche dopo un
  giro fermato a metà o un `su` caduto dopo averlo creato (toglie tutti i suffissi di `suffissiContainer`, ignorando quelli
  che non ci sono); la guardia della memoria (sotto 10 GB, sull'host) lo conta come ogni altro container (al più 256 MB).

## Nuova busta con il «Dettato AI» (slice 108, RIP-H-42, RIP-H-43, D-106)

- **Dove:** `internal/web/dettato.go` (pagina), `internal/web/pagine/nuova.html` (riquadro), `internal/assist/dettato.go`
  (`Impostazioni.EstrazioneConfigurata`, `EstraiDettato`), `internal/buste/scelte.go` (`ClientiNoti`), `statici/app.js` e
  `app.css`. Chi decide: **lo standalone** (`#assist-card`, `assist_extract`, `repair_form/1`, `assign_customer_pair/3`,
  `assist_summary/1`, `assist_refusal/2` di `WorkshopLive`), con le D-18, D-20, D-103, D-104 delle 103-106 e la D-106.
- **Riquadro:** con l'estrazione configurata (`LeggiEstrazione(...).Usabile()`, riletta a ogni pagina come il mount dello
  standalone) «1 · Incolla il testo dettato» (`#assist-card`) con «Descrizione libera» (`assist[text]`), il segnaposto dello
  standalone e «✦ Estrai con AI» (`#assist-estrai`), il messaggio in `#assist-status` (`text-error` sui rifiuti); il modulo
  diventa «Inserimento manuale senza AI», chiuso. Senza estrazione (o senza `Dipendenze.Assistente`) «🔒 Dettato AI» come
  prima. Un guasto nella lettura della configurazione è la pagina d'errore anche per `GET /nuova`. Il microfono arriva con le
  109-110.
- **`POST /nuova/estrai`** (personale, con il guscio): un modulo a parte (`#assist-form`, `data-dettato`); **app.js ci copia
  prima dell'invio i campi del modulo della busta** com'è (repair[…], invio, chiave delle foto, avvisi) e `intake_aperto=1`
  se è aperto; al contrario «Genera busta» porta con sé la casella del dettato, che un salvataggio rifiutato rimostra. Con
  delle foto che si stanno ancora salvando nel browser l'estrazione le aspetta come «Genera busta» (`WRITE_WAIT_MS`), e nel
  «giro» con le foto solo in memoria va con fetch (la cronologia resta su `/nuova`). Il negozio (tutti i clienti, oreficerie
  esterne) lo legge la pagina da `buste`, ma solo dopo i rifiuti che non costano: `assist.EstraiDettato` riceve una funzione
  e la chiama dopo «non configurata», «troppo corto», «troppo lungo» (client della 106: D-19, 90 s, audit `assist.extract`
  con soli conteggi; un guasto del negozio è `assist.ErrNegozio`). Il dettato passa per `aCapoUnix` come la casella di
  LiveView; la domanda al modello usa il contesto della richiesta: un browser che se ne va la ferma.
  - **Riuscita:** la pagina torna con il modulo della proposta (`repair_form/1`: nome, telefono, oreficeria segnata se c'è,
    data di oggi, consegna D-18, descrizione, «Busta N — Acconto N € — x» nelle note D-20, preventivo, compro oro; artigiano,
    note interne e codice QR vuoti come in un modulo nuovo; l'avviso del QR azzerato), aperto, con `Proposta.Messaggio()`.
    Invio e chiave delle foto restano: le foto scelte prima restano della busta. Non si salva niente.
  - **Rifiuto:** il messaggio dello standalone e il modulo **com'è arrivato** (valori, invio, chiave), aperto se lo era;
    gli avvisi delle etichette con la regola di «Genera busta»: il codice simile a un'etichetta se è ancora nel campo, le
    buste aperte con il codice di `gemelle_avvisate` (rilette, `buste.GemelleAperteDi`), l'avviso del QR mancante solo
    senza di loro. Senza JavaScript non arriva nessun campo e torna il modulo nuovo con la data di oggi. Con l'estrazione
    tolta dopo aver aperto la pagina la pagina resta com'era all'apertura (riquadro, «Inserimento manuale senza AI»,
    chiuso se non era aperto) con «Nessun modello è configurato: …».
  - Un errore che non è un rifiuto (database) è la pagina d'errore con «dettato: estrazione non riuscita» o «dettato:
    clienti e oreficerie non letti» nel registro: mai dettato, nomi, telefoni o l'errore.
- **D-106** (Stefano, 20/11/2026, aggiornata lo stesso giorno; `assist/nome.go`, `telefonoDellUnico`): senza telefono
  dettato contano i clienti con lo stesso **nome confrontabile** (la chiave del riconoscimento: a parte accenti, spazi,
  apostrofi e maiuscole); se hanno **tutti lo stesso telefono** (`telefono.Normalizza` e poi senza «0039»/«39», così
  «333 1234567» e «+393331234567» sono uguali) si propone quello del primo per nome e id com'è salvato, e il salvataggio
  trova la scheda con quel telefono; con telefoni diversi il campo resta vuoto. Il riconoscimento del nome (105) non cambia.
- **Banco:** browser `dettato-nuova` (`IN_FONDO` in `banco.mjs`, dopo `assistente-salva`, che lascia le due app sul modello
  finto; nell'ordine dei nomi). Senza dati guarda solo «🔒 Dettato AI»; con i dati il riquadro nelle due viste e, sul PC, un
  rifiuto che non tocca il modulo scritto a mano, un'estrazione con una risposta programmata con le chiavi dei due formati
  (corto del Go e lungo dello standalone) e il salvataggio di una busta propria del 2025 (8108/25 nello standalone,
  `SINT-DETTATO-108-desktop`). Differenze attese: D-18 `estratto.consegna` e D-20 `estratto.note` (`bench/deviazioni.json`).
  Il microfono dello standalone (trascrizione configurata da `assistente-salva`) non si guarda. Il livello HTTP non vede
  il riquadro: gira prima del browser, quando le app non sono ancora configurate; `nuova.json` resta com'è (deviazione dal
  piano approvata dal coordinatore il 20/11/2026).

## Trascrizione, parte server (slice 109, RIP-H-44, RIP-H-46, RIP-H-47)

- **Dove:** `internal/assist/trascrizione.go` (interfaccia, motori, rifiuti, `Impostazioni.Trascrivi`),
  `internal/assist/silenzio.go` (`ConSilenzio`), `internal/web/trascrizione.go` (`POST /nuova/trascrivi`),
  `tools/banco/modellofinto` (trascrittore finto su `/stt`). Chi decide: **lo standalone** (`Assist.transcribe/3`,
  `Assist.Transcriber`, `Assist.Audio`, `Providers.LocalWhisper`, l'upload `:dictation` e `handle_dictation/3` di
  `WorkshopLive`). Il microfono e la pagina arrivano con la 110.
- **`Transcriber`** (`Trascrivi(ctx, audio, tipo, cfg) (string, error)`): il testo come l'ha scritto il motore, oppure
  `ErrTrascrittore{Motivo}`. Due motori: **`ServerEsterno`** (`LocalWhisper`: POST `<indirizzo senza spazi e «/» finali>/stt`
  con la clip nuda nel corpo, `Content-Type` il tipo normalizzato, `Accept: application/json`, **`X-Gestrip-Glossario`
  sempre, anche vuota**, più `Accept-Encoding: gzip` e `User-Agent` che Go aggiunge da sé, con `GlossarioPerLaRete` (la forma a una riga; i 2000 caratteri li fa rispettare il salvataggio
  della 107, l'invio non taglia, come lo standalone), `Authorization: Bearer` solo con una chiave; niente proxy, redirect o
  keep-alive (`trasportoModello` della 106); **attesa 35 s** (`AttesaTrascrizione`, scadenza di tutta la richiesta; lo
  standalone la dà a hackney come `recv_timeout`); solo il 200 con `{"text": <stringa>}` vale, al più 1 MiB) e
  **`TrascrittoreFinto`** (il motore finto dei test: testo, errore o panic, conta le chiamate).
- **`Impostazioni.Trascrivi(ctx, audio, tipo, chi)`** → `Trascrizione{Testo, NienteVoce}`. Nell'ordine, senza chiedere niente e
  senza audit: `ErrTrascrizioneNonConfigurata` (`LeggiTrascrizione(...).Usabile()`, riletta a ogni clip), `ErrClipCorta` sotto
  **2000 byte** (`ClipMinima`), `ErrFormatoAudio` se il tipo, senza parametri, spazi e maiuscole (`TipoAudio`), non è
  `audio/webm`, `audio/ogg`, `audio/mp4`, `audio/mpeg`, `audio/wav`, `audio/x-wav`. Poi il motore (`i.Trascrittore`, il server
  esterno se nil; un panic o un errore che non è `ErrTrascrittore` è `stt:provider non raggiungibile`, il rescue di `ask/3`), il
  testo senza spazi ai bordi, vuoto = **niente voce**, e la riga **`assist.transcribe`** (risorsa `assist`, id `stt`):
  `ok` con `{"bytes", "no_speech"}`, `error` con `{"bytes", "reason"}`. Motivi: `stt:http:<stato>`, `stt:transport:<genere>`
  (come la 106: `::timeout`, `::closed`, `::econnrefused`…), `stt:risposta non riconosciuta`, `stt:provider non raggiungibile`.
- **Messaggi** (`MessaggioDiTrascrizione`, `Trascrizione.Messaggio`): «Nessuna trascrizione è configurata: scrivi a mano e premi
  Estrai.», «⚠ Registrazione troppo breve.», «⚠ Questo browser ha registrato in un formato che non so inoltrare.», «⚠ Trascrizione
  non disponibile, scrivi a mano e premi Estrai.», «🔇 Non ho sentito nulla, riprova.», «✓ Trascritto. Controlla e premi «✦ Estrai
  con AI».».
- **`POST /nuova/trascrivi`** (personale come `/nuova`, `rottaConFile` con `csrf.ModuloConFile`: token in testa al corpo o in
  `x-csrf-token`, prima di un byte della clip; corpo al più 25 MiB + 1 MiB): una sola parte con un file, il campo **`dictation`**
  (il suo `Content-Type` è il tipo della clip, `application/octet-stream` se manca); un file di un altro campo o altre parti dopo
  la clip → 400; **oltre 25 MiB** (`ClipMassima`) la lettura si ferma → **413** (lo standalone non dice niente nella pagina); un
  modulo senza file è una clip vuota («troppo breve»). Tempo per pezzo come le foto; **finita la clip la scadenza di lettura
  si toglie** (`togliScadenzaLettura`, dalla 118b comune con le foto), altrimenti scadeva durante l'attesa del trascrittore e cancellava la
  richiesta dopo 10 s invece di 35 (revisione della 109). **Al più 2 clip per istanza e 1 per account** (`clipInCorso`, dalla
  ricezione alla fine della trascrizione); oltre, subito «⚠ Trascrizione non disponibile…» senza leggere la clip. Arrivata la
  clip, **la sessione si ricontrolla** (revocata → `/login`, la clip non parte; non riletta → pagina d'errore). Risposta **JSON, sempre 200, `Cache-Control: no-store`**:
  `{"tipo":"ok","messaggio":"✓ Trascritto…","testo":…}` o `{"tipo":"error","messaggio":…}` (anche «🔇 Non ho sentito nulla…»).
  Il testo lo aggiunge in fondo alla casella la pagina della 110 (`append_dictated/2`: casella senza spazi ai bordi, uno spazio,
  il testo). Un guasto del database è la pagina d'errore con «dettatura: trascrizione non riuscita» nel registro.
- **Riservatezza (RIP-H-47):** la clip sta solo in memoria (letta dalla parte multipart, mai `ParseMultipartForm`, quindi niente
  file temporanei; il test lo prova con un `TMPDIR` vuoto), in un buffer che raddoppia azzerando quelli lasciati indietro
  (`leggiClip`), e si azzera dopo l'uso (restano i pochi KiB dei buffer di lettura di net/http e mime/multipart); un motore
  in panic lascia nel registro solo famiglia e tipo («assist: il motore della trascrizione è andato in panic», `famiglia=stt
  tipo=…`, `Impostazioni.Registro`); testo, glossario, chiave e byte mai in log,
  audit, errori o messaggi (`TestPrivacyTrascrizione`: 500 e 200 che ripetono testo, glossario e chiave, attesa scaduta, rete giù,
  motore in panic o con un errore che li porta, byte oltre il Content-Length; `slog` e `log` catturati).
- **Silenzio ai bordi (`ConSilenzio`):** mezzo secondo di campioni a zero prima e dopo un WAV PCM a 16 bit (qualunque frequenza e
  canali; gli altri pezzi del RIFF non si copiano; un `data` più lungo del file vale per i byte che ci sono). Accetta al più 48 kHz,
  8 canali e `ClipMassima` byte di campioni: alloca dalla dimensione vera, al più `ClipMassima` più i due silenzi. Serve al motore
  locale della T5.1 con il WAV della 110 (D-25); **il server esterno riceve la clip com'è**, come lo standalone (domanda G).
- **Trascrittore finto:** lo stesso server del modello finto (stesso container del banco, `modello.invalid:8082`): `POST /stt`
  (`{"text": …}`, a coda vuota `TestoTrascritto`), `POST /_prova/risposte-stt` (stessi campi delle risposte del modello;
  `contenuto` è il testo), `GET /_prova/richieste-stt` (intestazioni, `con_glossario`, `byte`, `multipart`; mai la clip);
  `ProgrammaSTT` e `RichiesteSTT` per i test Go. Dopo `assistente-salva` le due app hanno la trascrizione puntata lì.
- **Banco:** nessuno scenario in questa slice (deviazione dal piano approvata dal coordinatore il 20/11/2026): lo standalone
  riceve la clip solo con l'upload LiveView (niente HTTP da confrontare) e al livello HTTP le app non hanno ancora la
  trascrizione configurata. Il confronto lo fa uno scenario browser della 110, dopo `assistente-salva`.
- **Test:** `internal/assist/trascrizione_test.go`, `internal/web/trascrizione_test.go`, `tools/banco/modellofinto`
  (`TestTrascrittoreFinto`). RIP-H-44, RIP-H-46, RIP-H-47 restano parziali (microfono e pagina nella 110), senza segno.

## Microfono nella nuova busta (slice 110, RIP-H-44, D-25)

- **Dove:** `internal/web/statici/app.js` (il blocco «🎤 Detta a voce»), `pagine/nuova.html`, `internal/web/dettato.go`
  (`microfonoConfigurato`), `internal/assist/dettato.go` (`Impostazioni.TrascrizioneConfigurata`), `app.css`. Chi decide: **lo
  standalone** (`#assist-mic` e l'upload `:dictation` di `WorkshopLive`, il hook `assets/js/dictation.js`) con la **D-25**.
- **Pagina:** con l'estrazione **e** la trascrizione configurate (`assist_stt?`, `LeggiTrascrizione(...).Usabile()` riletta a ogni
  pagina, letta solo se c'è il riquadro) `#assist-form` ha `<button id="assist-mic" type="button" aria-label="Detta a voce">🎤 Detta
  a voce</button>` prima di «✦ Estrai con AI» e `<input id="assist-clip" type="file" hidden data-trascrivi="/nuova/trascrivi">`
  (senza nome: non va a `/nuova/estrai`; è il campo dell'upload LiveView dello standalone). Un guasto nella lettura è la pagina
  d'errore, come per l'estrazione. Senza JavaScript il pulsante non fa niente.
- **Registrazione (app.js), regole di `dictation.js`:** contesto non sicuro → «⚠ Microfono non disponibile: contesto non sicuro
  (HTTP o certificato non valido), il browser non espone il microfono.»; senza `navigator.mediaDevices.getUserMedia` (e, nel Go,
  senza AudioWorklet) → «⚠ Microfono non disponibile su questo dispositivo o browser.»; un secondo tocco mentre si chiede il
  permesso non fa niente; `getUserMedia({audio: true})` rifiutato → «⚠ Microfono negato: <messaggio del browser> — scrivi a mano.»;
  in ascolto «🔴 Sto ascoltando… (tocca per fermare)»; sotto **600 ms** «⚠ Troppo breve, riprova.» e niente invio. I messaggi
  vanno in `#assist-status` (al più 200 grafemi, `text-error` sui rifiuti).
- **D-25:** niente MediaRecorder. Un **AudioWorklet** (codice in app.js, caricato da un indirizzo `blob:`) copia il canale (il nodo
  mescola gli altri: `channelCount: 1`, `explicit`) a pezzi di 4096 campioni; la pagina li **ricampiona a 16 kHz mentre
  arrivano** (`Ricampionatore`: media dei campioni che cadono nell'intervallo di ogni campione d'uscita, un filtro a scatola; con un
  ingresso più lento il campione si ripete) in PCM a 16 bit, così in memoria restano 32 KB al secondo. Al secondo tocco il flusso
  si ferma, il processore manda l'ultimo pezzo (o si va avanti dopo 1 s) e `wav()` scrive **RIFF/WAVE, fmt di 16 byte (PCM, 1
  canale, 16000 Hz, 32000 byte/s, blocco 2, 16 bit), data**. Arrivata a 25 MiB (`assist.ClipMassima`, circa 13 minuti) la
  registrazione si ferma da sola. La clip («dettato», `audio/wav`) va in `#assist-clip` con un DataTransfer e un `change`.
- **Invio:** il `change` di `#assist-clip` (registrata o scelta come file) manda un FormData con **`_csrf_token` per primo** e poi
  `dictation` a `POST /nuova/trascrivi` (slice 109, risposta invariata), mostra «⏳ Trascrivo…», svuota il campo. Una fetch finita
  su `/login` (sessione revocata, redirect seguito) porta a `/login`; una risposta che non è il JSON della 109 (413 oltre 25 MiB,
  pagina d'errore, rete giù) è «⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai.» (lo standalone non dice niente per
  il 413). Con `tipo: "ok"` il testo va in fondo alla casella come `append_dictated/2` (casella vuota: il testo; altrimenti la
  casella senza spazi ai bordi, uno spazio, il testo), poi il messaggio.
- **Riservatezza:** niente audio in IndexedDB, localStorage o sul server (109); i campioni restano in memoria fino alla clip, il
  campo si svuota appena la clip è nel modulo da mandare.
- **Revisione:** `gs:pagina` butta la registrazione di un pulsante che non c'è più (`scarta`, senza invio) e libera l'avvio;
  una traccia che finisce da sé (`ended`) ferma e manda la clip; mentre una clip è al server (`trascrivendo`) pulsante e campo
  non fanno niente.
- **Banco:** due scenari browser in fondo (`IN_FONDO`, dopo `assistente-salva`, che lascia la trascrizione sul trascrittore
  finto), con `RIP-H-44`: **`dettato-clip`** (il confronto del caricamento di una clip spostato dalla 109: un file sintetico sul
  campo file di `#assist-form` delle due app; troppo corta, formato di testo, niente voce, trascrittore a 500, riuscita in una
  casella con gli spazi ai bordi; messaggio, rosso, casella, richieste e tipo al finto, righe `assist.transcribe`) e
  **`dettato-microfono`**, con il **microfono finto di Chromium** (`--use-fake-device-for-media-stream`,
  `--use-fake-ui-for-media-stream`, `--use-file-for-fake-audio-capture=/banco/fotocamere/microfono.wav`, `ARGOMENTI_IN_FONDO`;
  provato con l'immagine `grgo-banco-browser:1.58.2` il 21/11/2026): `microfono.wav` lo scrive `scriviFotocamere` (`microfono.go`:
  3 s di un tono a 440 Hz, mono, 16 kHz). Uno script prima di quelli della pagina tiene flussi e contesti audio e dopo ogni caso
  si guarda che siano spenti (`*.rilascio`). Tocco breve, registrazione di 2 s (testo in fondo alla casella) e seconda ripresa
  sulla stessa pagina, un tocco dopo il microfono negato, microfono negato,
  contesto non sicuro e browser senza microfono (questi tre cambiando la pagina prima del tocco). Il trascrittore finto descrive
  la clip WAV che riceve (`modellofinto.DescriviWAV`: formato e **tono** stimato dai passaggi per lo zero con isteresi; `wav` e
  `tono_hz` in `/_prova/richieste-stt`, mai i byte): differenza attesa **D-25** su `detta.clip` (standalone `audio/webm`, Go
  `audio/wav, PCM 16000 Hz, 1 canale, 16 bit, tono di 440 Hz circa`), che prova anche il ricampionamento (un WAV a 16 kHz con i
  campioni di un'altra frequenza darebbe un altro tono).
- **Test:** `internal/web/microfono_test.go` (pulsante e campo, guasto della lettura, righe di app.js), `internal/assist/dettato_test.go`
  (`TestTrascrizioneConfigurata`), `tools/banco/modellofinto/wav_test.go`, `tools/banco/fotocamera_test.go` (`TestMicrofonoFinto`).
  Il comportamento di app.js nel browser lo provano i due scenari (il repo non fa girare JavaScript nei test Go).

## Sidecar dell'IA (slice 111, PIANO §1.8)

- **Dove:** `internal/sidecar` (`sidecar.go`: `Comando`, `Tempi`, `Supervisore`; `figlio.go`, `figlio_linux.go`:
  avvio e arresto del processo), `internal/apparecchio` (le righe `sidecar_*`), `cmd/gestrip/main.go` (`serve`). Chi
  decide: **PIANO §1.8** e il piano della T4.10 (riga 111, risposte A e G, nota 8): lo standalone non avvia processi
  del modello (parla solo con un server già acceso, `LocalChat`).
- **Supervisore generico:** programma assoluto, argomenti (`{porta}` diventa la porta), porta da 1024 a 65535 su
  `127.0.0.1`, percorso di salute (di default `/health`, controllato anche con `url.Parse`); `{porta}` deve stare in
  almeno un argomento. Il sidecar dell'estrazione può non essere `llama-server`
  (risposta A). Il figlio parte con **solo l'ambiente del `Comando`** (di default vuoto: l'ambiente di GestRip non passa),
  cartella `/`, senza standard input né output (`/dev/null`), in un **gruppo di processi suo** e con **Pdeathsig
  SIGKILL** (la goroutine che fa `Start` resta sul suo thread fino a `Wait`, come in `imaging` e `figlitest`).
- **Ciclo (`Lavoro`):** porta già occupata → il figlio non parte (la salute risponderebbe per un altro server); avvio
  → GET di salute ogni `PassoAvvio` finché dà **200** (llama-server dà 503 mentre carica) **e** la porta è del figlio
  (`ascolto_linux.go`: in `/proc/net/tcp` e `tcp6` sulla porta ascoltano solo socket su 127.0.0.1, ognuno aperto da un
  processo del gruppo del figlio secondo `/proc/<pid>/fd`; revisione della 111, M2), al più `Avvio`; un 302 non vale; pronto →
  GET ogni `Controllo` (con lo stesso controllo della porta), `Malato` fallite di fila lo riavviano; figlio uscito (anche durante l'avvio) → si riprova.
  Attese prima di ogni nuovo avvio: `AttesaMinima`, poi il doppio, fino a `AttesaMassima`; un figlio rimasto pronto
  almeno `Stabile` riporta l'attesa al minimo. Non si arrende mai e non torna errori: **GestRip resta su**. Default
  (`TempiDiDefault`): avvio 3 min, passo 0,5 s, controllo 10 s, risposta 5 s, malato 3, attese da 1 s a 5 min,
  stabile 10 min, arresto 10 s.
- **Arresto (fine del contesto):** SIGTERM al gruppo e al figlio, SIGKILL dopo `Arresto`, e a figlio raccolto SIGKILL al
  gruppo per i nipoti rimasti (anche quando il figlio cade da solo). `Lavoro` torna solo a figlio raccolto. Un nipote
  che si fa un gruppo suo (Setsid) sfugge, come in `figlitest`; sull'apparecchio lo prende il cgroup del servizio
  systemd. Se GestRip muore di colpo (SIGKILL) il figlio muore per Pdeathsig, i nipoti no (stesso limite).
- **Registro (RIP-H-47):** solo `sidecar=<nome>`, pid, esito (`codice N`, `segnale killed`), attese, porta occupata,
  genere dell'errore d'avvio (`non trovato`, `permesso negato`, `errore`); mai argomenti, ambiente, percorsi o quello
  che il figlio scrive (`TestRegistroSenzaDati`).
- **Configurazione, spenta di default:** nel file dell'apparecchio (sezione «Configurazione della macchina»), per le
  famiglie `estrazione` e `trascrizione`: `sidecar_<famiglia> = <programma assoluto> <argomenti…>` (separati da spazi,
  niente virgolette), `sidecar_<famiglia>_porta = N`, `sidecar_<famiglia>_salute = /percorso` (facoltativa). Le due
  prime righe vanno insieme, la stessa porta per le due famiglie è rifiutata; una riga sbagliata ferma l'avvio come
  ogni altra del file. Senza righe `serve` non avvia
  niente. `serve` fa girare un supervisore per sidecar fra i lavori in sottofondo e lo aspetta all'uscita, prima di
  chiudere il database. **Non collegato all'assistente:** l'estrazione e la trascrizione parlano ancora solo con gli
  indirizzi delle impostazioni (107); collegarle al sidecar è una scelta aperta (ricevuta 111, Dubbi).
- **Test:** `internal/sidecar/sidecar_linux_test.go` con il **figlio finto** (il binario dei test con
  `sidecar-finto <modo> <porta>`: sano, lento, malato, singhiozzo, muto, mai, sordo, nonno, ambiente, chiacchierone,
  appeso, rinvia, ovunque, cede) e
  il **supervisore finto** (`sidecar-supervisore`, per Pdeathsig); `cmd/gestrip/sidecar_linux_test.go` (serve avvia,
  ferma, sopravvive a un sidecar che non parte e lo ferma quando cade una faccia); `internal/apparecchio` (`TestSidecar`). Attese di prova di 10-40 ms
  vere; i test aspettano condizioni (al più 20 s), mai una durata fissa come prova. **Porte dei test a caso fra 20000 e
  32000** (provate con Listen), mai con la porta 0: una porta effimera sotto carico può andare a una connessione in
  uscita prima che il figlio la apra (revisione della 111, M1). Le asserzioni esatte sulle attese contano solo le cadute
  volute; uno zombie conta come morto.
- **Corsa vera:** `TestSidecarVero` (`vero_linux_test.go`) parte solo con `GESTRIP_SIDECAR_VERO` e
  `GESTRIP_SIDECAR_VERO_PORTA` (sopra 47000, mai le porte di gestrip-switch e llm-switch): mai in `scripts/check` né in
  CI. Il comando, sotto `systemd-run --user --scope` con limiti, è nella ricevuta 111.

## Banco del modello (slice 112, RIP-H-54, OPS-08)

- **Dove:** `internal/verifica` (casi, punteggio, riassunto, corsa), `cmd/gestrip/verifica.go` (il comando), casi in
  `bench/modello/` (README). Chi decide: **lo standalone** (`mix assist.bench`, `Assist.Bench`) per il punteggio, il Go
  (D-103, domanda F) per la richiesta.
- **Comando:** `gestrip verifica-modello -indirizzo URL -modello NOME [-casi FILE,…] [-oreficerie NOMI] [-per-categoria N]`.
  Senza `-casi` legge i quattro banchi di `bench/modello/` dalla cartella corrente (la radice del repo): B, C, D, D2
  (1720 casi). Gli 84 casi dello standalone **non sono nel repo** finché Stefano non dà l'ok: si rigenerano in locale
  (README di `bench/modello/`) e si aggiungono con `-casi`. `-per-categoria N` ne tiene al più N per categoria di
  ogni file, distribuiti uniformi (10 → 300 casi dei banchi, 330 con gli 84); `-oreficerie` sono le oreficerie che il negozio conosce (default
  «Bottari,Bianchi Gioielli», come lo standalone). Uscita 0 dopo la corsa (qualunque punteggio), 2 per un'opzione
  sbagliata (anche un indirizzo con utente o password), 1 per un file di casi sbagliato o una corsa interrotta (Ctrl-C, `timeout`: stampa quello che ha fatto).
- **Come la nuova busta:** ogni caso passa per `assist.Collegamento.Estrai` della 106 (un solo messaggio utente
  «Oggi: <giorno> GG/MM/AAAA\nDettato: …», `temperature 0`, un solo nuovo tentativo dentro i 90 s) e per la lettura delle
  104-105 (proposta con nome dall'apertura, oreficerie note, telefono dal dettato, consegna dalla frase «c», note
  «Busta N — Acconto N €»). Nessuna chiave (il comando non ne prende), nessun registro dell'audit, nessun database,
  nessun cliente noto. Un caso alla volta, nell'ordine.
- **Punteggio** (sulla proposta, cioè quello che vedrebbe il negozio): casi dello standalone su data, nome, telefono,
  preventivo, data_consegna, oreficeria, compro_oro (dal testo, non dal modello), più busta e parola se il caso le dice;
  banchi su nome, oreficeria, busta, descrizione, data_consegna, preventivo, acconto, altre_note (le chiavi `n o b d c p a
  x`; una chiave assente vuol dire «vuoto», e un valore inventato è un errore). Busta, acconto e altre note si rileggono
  dalle note (nei banchi il primo pezzo «Busta N»; nei casi dello standalone `\bbusta\s+N\b` in qualsiasi punto, come
  `right?("busta")`). Acconti e buste inventati (attesi vuoti, proposta piena) contati a parte. Si perdonano solo maiuscole e spazi; nel preventivo «400-500» = «400/500»; il telefono atteso passa per
  `telefono.Normalizza`. Senza proposta (illeggibile, rifiuto) ogni campo del caso è sbagliato.
- **Resoconto:** schede tutte giuste (in tutto e per file), per campo, risposte illeggibili, rifiuti per motivo fisso,
  casi con un nuovo tentativo, tempo per scheda (dalla domanda alla proposta, nuovo tentativo compreso) mediana/p90/max
  al rango più vicino; poi una riga per caso sbagliato con l'id e i nomi dei campi. Su stderr l'avanzamento ogni 25 casi.
  **Mai** il dettato, la risposta, il nome che il server dice di sé, l'indirizzo: un file di casi sbagliato si rifiuta
  tutto con il solo numero della riga.
- **Test:** `internal/verifica/verifica_test.go` (modello finto, cronometro finto) e `cmd/gestrip/verifica_test.go`,
  `verifica_linux_test.go` (processo vero, codici d'uscita); `TestVerificaModelloNonStampaIlDettato` cattura anche `log` e `slog`.
- **Corsa vera:** solo CPU e solo con il «memoria libera»; comando e stime nella ricevuta `docs/slices/112-verifica-modello.md`.
  **Parametri del server:** le misure sono con llama-server `--reasoning off -n 256 --cache-ram 0` (la richiesta resta quella della
  busta); il sidecar della T5.1 deve usare gli stessi perché valgano (scelta aperta per la T5.1).

## Raccolta dei dettati al salvataggio (slice 113, RIP-H-48)

- **Dove:** `internal/buste/raccolta.go` (la riga, dentro `Crea`), `internal/assist/raccolta.go` (la proposta della riga),
  `internal/web/raccolta.go` (la cattura della pagina). Chi decide: **lo standalone** (`Training.capture/3` e
  `record/3`, `Workshop.create_repair/4`). Interruttore, nuovo lotto ed esclusioni dalla pagina sono della 114.
- **Quando nasce una riga:** solo nel salvataggio di una busta (`buste.Accettazione.Dettato` non nil), nella sua
  transazione; con `assist.training.collect` = «1» (spenta di default), cliente non in `dettati_esclusioni` (niente riga,
  lotto intero) e un'unità di `assist.training.remaining` presa con `UPDATE … WHERE CAST(valore AS INTEGER) > 0` (due
  salvataggi sull'ultima unità: una riga). L'ultima unità spegne subito; un interruttore acceso su un lotto finito (o senza
  lotto) si spegne al salvataggio dopo. Lo spegnimento scrive `assist.training_collect` con attore `system` e
  `{"on":false,"remaining":0}`. `Creata.Raccolta` dice che cosa è successo.
- **Un errore non blocca mai la busta (D-108):** tutto quello che la raccolta legge e scrive sta nel `SAVEPOINT
  raccolta_dettato` dentro la transazione della busta; un errore torna al savepoint (riga, unità e spegnimento annullati:
  l'unità non si consuma) e la busta si salva (`RaccoltaNonScritta`, nel registro «dettato: riga della raccolta non
  scritta»). Lo standalone tiene l'unità consumata e, con un guasto nelle letture o nel lotto, perde la busta.
- **Import (D-105):** l'import copia `assist.training.collect` con il valore «0» (`importer.ValoriAllImport`) e
  `assist.training.remaining` com'è. La raccolta si accende solo dalla pagina (114); nel binario `gestrip` nessun altro
  percorso la scrive. Il banco la accende nelle due app (`tools/banco/raccolta.go`) dopo l'import, solo sulle sue copie.
- **La cattura:** l'estrazione riuscita la mette in memoria (`depositoCatture`) per sessione (utente e nonce) e chiave del
  modulo (`intake_key`), al posto di quella di prima; un rifiuto non la tocca. «Genera busta» la prende prima di `Crea` e la
  rimette se la busta non si salva (anche con un panic di `Crea`). Vale `durataCattura` (2 ore); un riavvio la perde. Al
  più `maxCatture` in tutto e `maxCattureSessione` per sessione (oltre, si toglie la più vecchia della sessione; a deposito
  pieno la cattura nuova non si tiene e la busta si salverà senza riga). Il browser manda solo la
  chiave: testo, proposta e modello non passano mai dal client.
- **La riga:** `testo` è il dettato senza spazi ai bordi (`Estrazione.Testo`), `modello` quello che ha risposto (NULL se
  vuoto), `proposta` con le chiavi dello standalone (`@kept`: data, nome, telefono, descrizione, note, preventivo,
  data_consegna, oreficeria, compro_oro, oreficeria_id, nome_corretto_da solo se corretto; date GG/MM/AAAA, compro_oro
  0/1, oreficeria_id intero o ""), così import, correzioni ed esportazione leggono le righe nuove come le importate.
- **Riservatezza:** `buste.Dettato` si nasconde in `%v`, `%+v`, `%#v` e slog; mai testo, proposta o modello in registro,
  audit o errori.
- **Test:** `internal/buste/raccolta_test.go`, `internal/web/raccolta_test.go`, `internal/assist/raccolta_test.go`.

## Raccolta dei dettati dalla pagina dell'assistente (slice 114, RIP-H-49, RIP-H-50, ADM-12)

- **Dove:** `internal/buste/raccolta_pagina.go` (stato, interruttore, nuovo lotto, esclusioni), `internal/web/raccolta_pagina.go`
  e la sezione `#training` di `pagine/assistente.html`. Chi decide: **lo standalone** (`Training.status/0`, `set_collect/2`,
  `refill/1`, `exclude/2`, `exclude_customer/2`, gli eventi `training_*` di `AssistLive`), salvo la D-07.
- **Pagina** (solo titolare, `/settings/assistente`): «Dettati per l'addestramento» con «Accesa»/«Spenta», il testo dello
  standalone e, in un paragrafo a parte (`#training-privacy`), le tre frasi della **D-07**; il conteggio
  `#training-remaining` (buste rimaste su 500, dettati raccolti, clienti esclusi: solo numeri, mai un dettato); «Accendi la
  raccolta» (spenta e lotto non finito), «Spegni la raccolta» (accesa), a lotto finito «Budget esaurito: la raccolta è
  finita.» e «Avvia un nuovo lotto di 500 buste»; i moduli «Escludi» (`exclude[bag_number]`) ed «Escludi il cliente»
  (`exclude_customer[bag_number]`).
- **Rotte** (`soloTitolare`, CSRF): `POST /settings/assistente/raccolta/accendi`, `…/spegni`, `…/lotto`, `…/escludi`,
  `…/escludi-cliente`; ognuna torna alla pagina (302) con il messaggio dello standalone. Un modulo senza il campo del numero
  non fa niente (come l'evento senza parametri). Un guasto è la pagina d'errore con una riga fissa nel registro, senza il
  numero scritto.
- **Regole copiate:** la prima accensione scrive `assist.training.remaining` = 500; spegnere e riaccendere non lo ricarica;
  l'accensione con il lotto a 0 (o illeggibile, o negativo: `remaining/0`) è rifiutata («Budget esaurito: la raccolta non si
  riaccende.»); spegnere si può sempre. Il nuovo lotto solo se il lotto salvato è esattamente «0» (anche prima del primo
  lotto è rifiutato: «Il lotto in corso ha ancora buste: niente da ricaricare.»): 500 e raccolta accesa. L'esclusione di una
  busta cerca, fra **tutte** le buste (cestino compreso), il numero scritto senza spazi ai bordi e cancella il suo dettato (la
  busta resta). L'esclusione del cliente lo trova dal numero di una sua busta, lo mette in `dettati_esclusioni` (una volta) e
  cancella i dettati di tutte le sue buste; le buste future le salta la raccolta della 113.
- **D-109 (Stefano, 22/11/2026), il numero scritto:** è quello dell'**etichetta** («0045/26», «0045», «45»); il numero interno
  vale solo per le buste senza etichetta `RIP-…`. Stessa regola della consegna e del lotto (D-89, D-95): il codice è
  `bustaPerNumeroScelta` di `consegne.go` (`bustaPerNumeroCon` con il cestino), con tutte le buste trovate buone, qualunque
  sia lo stato. **Più buste sono sempre un rifiuto** (D-109 aggiornata, Stefano 22/11/2026), anche senza l'anno scritto e con
  una sola dell'anno in corso: niente scelta dell'anno (parametro `sceltaAnno` falso; consegna e lotto lo tengono vero).
  Il rifiuto è «Più buste corrispondono a «X»: scrivi il numero con l'anno o usa il QR.» (`RifiutoPiuBuste`) e niente si
  cancella. Gli esiti nominano il numero visibile
  (`NumeroVisibile`): «Busta 0045/26 esclusa: il suo dettato è stato cancellato.», «La busta 0045/26 non ha un dettato
  raccolto.», «Cliente della busta 0045/26 escluso: cancellati N dettati; le sue buste future non saranno raccolte.» (senza
  etichetta «N. 45/26»); «Nessuna busta con questo numero.» resta. Lo standalone cerca il numero interno e dice «Busta
  esclusa», «Questa busta», «Cliente escluso».
- **Audit** (dal dominio, soli numeri): `assist.training_collect` `{"on","remaining"}`, `assist.training_refill`
  `{"on":true,"remaining":500}`, `assist.training_excluded` sulla busta con `{}`, `assist.training_customer_excluded` sul
  cliente con `{"cancellati":N}`.
- **Tutto o niente:** ogni azione è una transazione, ricerca della busta compresa (lo standalone la cerca prima): un errore,
  anche dell'audit, non lascia una cancellazione a metà.
- **Conferme:** nuovo lotto ed esclusione del cliente hanno il `data-confirm` dello standalone; `app.js` mette `conferma=1`
  (campo `data-conferma-js`). Senza JavaScript il server risponde con la pagina e la conferma nella sezione
  (`#training-confirm`: «Sì» rimanda il modulo con `conferma=1`, «Annulla» torna alla pagina) e non scrive niente, come la
  conferma di «✓ Consegnata» (D-70).
- **D-105** resta: dopo un import la raccolta è spenta e il lotto conservato; la pagina mostra le buste rimaste.
- **Banco:** HTTP `assistente-raccolta` (`in_fondo`, la sezione nelle due app; con i dati accesa con una busta nel lotto, dal
  comando `banco`), con la D-07 in `bench/deviazioni.json` solo per `#training-privacy`. Le azioni nello standalone sono
  eventi LiveView, quindi stanno nel browser: `raccolta-interruttore` (tutte e due le viste, tocca l'interruttore due volte
  e rimette stato e conteggio) e `raccolta-esclusioni` (`IN_FONDO`, dopo `dettato-lotto`, solo con i dati e sul PC: pagina a
  lotto finito, esclusioni per numero e del cliente con i rifiuti, nuovo lotto con la conferma, raccolta rispenta). Le buste
  di `dettato-lotto` non hanno un'etichetta `RIP-…`: nel Go si cercano con il numero interno, letto dalla scheda; gli esiti
  con il numero (mascherato in «<numero>») sono quattro righe D-109 in `bench/deviazioni.json`. Gemelle e numero
  d'etichetta uguale al numero interno di un'altra busta stanno nei test Go (i dati del banco non li hanno).
  `dettato-lotto` guarda anche stato e conteggio della pagina prima e dopo le due buste.
- **Test:** `internal/buste/raccolta_pagina_test.go`, `internal/web/raccolta_pagina_test.go`.

## Messaggio del cliente dalla pagina pubblica (slice 114, CLI-12)

- `app.js` mandava il modulo `#public-thread-form` con `new FormData(form)` (multipart, come `public_thread.js` dello
  standalone, dove Plug legge il token anche dal multipart); `CSRF.Modulo` legge `_csrf_token` solo da un modulo
  urlencoded, quindi `POST /r/{token}/messaggio` era il 403 e il cliente leggeva «Errore durante l'invio.». Ora manda
  `new URLSearchParams(new FormData(form))`, come gli altri fetch. `TestScriptMessaggioClienteURLEncoded` vuole che nessun
  fetch dello script mandi un modulo multipart diretto: le foto della scheda (`corpoInvio`) e la clip del dettato (`dati`)
  hanno un file e vanno a rotte con `ModuloConFile` o che leggono il multipart, quindi restano così.
- Banco: browser `messaggio-cliente` (`IN_FONDO`, con i dati, tutte e due le viste) manda un messaggio dalla pagina della
  28/26 e guarda esito, riga e riga dopo la ricarica.

## CI: i test caduti nel primo giro (slice 114)

- Se il primo `go test -json` della CI fallisce, `go run ./tools/testfalliti go-test.json` stampa i pacchetti non
  compilati, i test e i pacchetti caduti con le loro righe di output, prima del secondo giro leggibile; il passo
  «go-test.json del giro caduto» (`if: failure()`) carica `go-test.json` come artefatto per 7 giorni. Il resto della CI non
  cambia.

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
- **Dati propri (regola dalla slice 080):** uno scenario browser **nuovo** che cambia dati (stati, consegne, artigiani
  delle buste) lavora su dati che crea lui, con nomi e codici univoci per vista (es. la busta «SINT-RIENTRO-080-desktop»
  / «-telefono» di `sessioni-rientro`), non su buste della catena scelte a mano: con l'ordine «prima tutte le viste
  del PC, poi tutte quelle del telefono» una busta «libera» per il PC può essere già cambiata (o consegnata) da una
  vista del telefono che gira prima (banco s080q: la 6/26, consegnata da `firma-riquadro` sul telefono). Le buste nuove
  si accettano nel **2025**, così stanno dopo tutte quelle del 2026 nella lista (gli altri scenari leggono la prima
  pagina) e non toccano la numerazione del 2026. **Il «trucco del 2025» vale da solo solo per il Go**, che numera con
  l'anno della data di accettazione (e non ha «N° Busta», D-64): lo standalone precompila «N° Busta» con il numero
  dell'anno in corso e lo usa com'è, quindi lo scenario scrive anche un numero del 2025 in quel campo
  (`numeroDel2025` in `banco.mjs`: 8080/25 e 8081/25 per `sessioni-rientro`, 8082/25…8085/25 per
  `storico-correzioni`; un numero nuovo per ogni scenario nuovo). Senza, nel banco s082p le buste dello standalone
  presero 52/26…69/26 e spostarono la prima pagina che `riquadro-azioni` legge sul telefono. Se lo scenario deve usare dati condivisi (un artigiano della catena),
  alla fine li rimette come li ha trovati e lo scrive nel commento d'ordine in `banco.mjs` (chi guarda quel dato e
  quando); prima di sceglierli si cerca chi li tocca anche nelle attese (`bench/browser/*.json`, `/repairs/N`), non
  solo nei numeri scritti in `banco.mjs`, dove molte buste sono variabili per vista.
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
  sono per `pannello-qr` e gli altri. **Tranne la prova `scanner` (slice 079)**, l'ultima, che cambia i dati
  apposta: con `consegna.y4m` (2,5 s di bianco, poi la 26/26, `RIP-2026-0047`, e `SINT-NIENTE-079` di nessuna
  busta, 320×240, 160 fotogrammi) apre una sessione dell'artigiano 1 sul PC, aggiunge la 26/26 e la toglie; sul telefono riprende la
  stessa sessione, aggiunge la 26/26 e conferma. Dopo non gira niente sulla stessa app. **La prova `scansione` (slice 090, ORE-07)** cambia i dati con `lotto.y4m` (la
  stessa sequenza, con il QR di `SINT-LOTTO-090`): crea l'oreficeria «Zz Banco 090» e una sua busta pronta del 2025 con quel QR,
  apre il suo lotto e la scansiona; sul PC «Rimuovi» la rimette fra le candidate, sul telefono resta nel lotto. Gli script che `banco.mjs`
  mette nelle pagine sono stringhe template: ogni barra rovescia va scritta doppia (`\\s`), altrimenti sparisce
  (banco s079p: le righe di stato arrivavano senza le «s»); lo controlla `TestScriptDellePagineSenzaEscapePersi`.
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

- **«Salva e avverti» e avviso dal pannello (slice 068):** lo scenario browser `salva-avverti`, nelle due app,
  sulla 40/26 (10000/25, di nessun altro scenario): pronta dal menu, il modulo con i due pulsanti, un rifiuto
  (descrizione vuota, `noValidate`) senza popup, poi il prezzo con «📲 Salva e avverti cliente»: sul PC il popup e
  «Annulla», sul telefono «Invia» con un testo senza link e «Azzera l'avviso»; alla fine la busta torna in
  lavorazione e le righe del registro si contano. In `pannello-qr` (telefono) «✅ Valutata» della 34/26 ora
  registra l'avviso: lo scenario salva prima, nelle due app, un testo «Pronta — Compro oro» senza `{link}` (il Go
  del banco non ha `indirizzo_pubblico`) e lo ripristina alla fine; legge `#qr-opening` e il link wa.me, lo
  tocca (catturato, mai aperto) e conta `ready_notice.sent`.
- **Testi e invii registrati (slice 067):** gli scenari HTTP `testi` (titolare, confronta tutta la pagina:
  sezioni, titoli, segnaposto, caselle con le righe, anteprime, conferma) e `testi-dipendente` (302 a `/`)
  leggono e basta. Lo scenario browser `testi-invii`, nelle due app: sul desktop un testo di `wa_pronta`
  rifiutato, uno salvato e «Ripristina predefinito» (il testo torna quello di prima per `whatsapp`, che gira
  dopo); sulle due viste un messaggio senza link dal popup della 24/26 (0046/26, di nessun altro scenario),
  la lista «📨 Invii registrati» e «Togli dalla lista» con la conferma, contando le righe del registro.
- **Provider di invio (slice 069b):** gli scenari HTTP `provider` (titolare: titolo, frase, riquadro della posta,
  «Non configurato», etichette, nomi, valori e segnaposto dei campi, le tre sicurezze, «✉ Invia messaggio di prova»,
  la coda vuota con i conteggi) e `provider-dipendente` (302 a `/`) leggono e basta. Lo scenario browser
  `provider-posta`, nelle due app, sul desktop: due salvataggi rifiutati (gli errori del changeset sotto i campi,
  letti con `p.text-error, p.field-error` del `div` che contiene il campo, i valori mandati, la password vuota),
  uno valido («Provider salvato.», «Configurato», il segnaposto della password, la password mai nella pagina),
  «Cancella le credenziali salvate», poi la configurazione **rimessa vuota** (come all'inizio: «Non configurato»,
  porta 587) e «✉ Invia messaggio di prova» senza configurazione (niente accodato); conta
  `outgoing.provider_configured` (3) e `outgoing.queued` (0). Solo server `.invalid` e mai una prova con la
  posta configurata: nessuna connessione verso fuori, in nessuna delle due app. La password finta è
  `PAROLA_BANCO`, scritta a pezzi (gitleaks). L'interruttore e la revisione delle email in coda (D-03, D-27) sono
  solo del Go e li provano i test Go. Dalla 069c la prova con la posta configurata parte subito (nel Go) e
  aprirebbe una connessione: il banco continua a non farla mai.
- **Popup WhatsApp (slice 066):** lo scenario HTTP `avvisi-whatsapp` (`in_fondo`, prima di `firma`, legge e basta) confronta il 📲
  della lista (`aria-label` con il numero, che il normalizzatore D-64 riconosce anche in «Avvisa per la
  busta N/AA»), «📲 Avvisa cliente|oreficeria» e il riquadro `#repair-notice` della scheda. Lo scenario
  browser `whatsapp` fa il resto nelle due app: popup dalla lista (25/26) e dalla scheda (2/26
  dell'oreficeria, 39/26), «Annulla», Esc, «Invia» con il ritorno alla lista con i filtri (D-74), dopo
  «Rispondi», avviso di ritiro e «Azzera l'avviso»; il contesto annota gli indirizzi `wa.me` e li
  interrompe (`ctx.waMe`), mai raggiunti. Nei testi il numero della busta si scrive «N» (D-64), il link
  «<link>», le date «<data>»; le righe del registro si contano prima e dopo.
- **Link storici /stato (slice 064):** il banco dà alle due app lo stesso segreto Flask **sintetico**
  (`segretoFlaskBanco` in `tools/banco/storica.go`, chiaramente finto): allo standalone con
  `LEGACY_WARRANTY_SECRET_FILE` (file `legacy_warranty_secret` dei segreti dell'istanza), al Go in
  `go-data/secrets/segreto_flask`. Il Caddy manda alla faccia pubblica del Go `/r/*`, `/stato/*`,
  `/foto/*` e `/thumb/*` (dalla 065 anche `/m/*` e `/privacy`, dalla 084 `/artigiano/*`, dalla 092 `/lotto/*`). Dalla 084 il
  `secret_key_base` dello standalone del banco è fisso e sintetico (`segretoStandaloneBanco`), dato anche al Go in
  `go-data/secrets/segreto_standalone`; `{phoenix_artigiano:<id>:<secondi fa>}` nel percorso di un passo è il
  Phoenix.Token dell'artigiano firmato a ogni richiesta (scenario `pagina-artigiano`). Il passo `token` dà il codice breve `SINT-4` alla 4/26 (aperta, diretta) e
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

## Banco: lavori automatici spenti nello standalone (slice 100)

Il banco confronta le due app a ogni giro, e lo scheduler dei job dello standalone (`GestripStandalone.Jobs.Scheduler`,
al tag `go-baseline`) gira per conto suo ogni minuto. Senza canali, ogni lavoro acceso che ha raggiunto la sua ora
(ritardi 09:00, solleciti 10:00; il briefing è spento di suo) scrive una riga «Saltato» in `job_runs` e una riga di audit
(`Jobs.run/3`, fino a tre tentativi al giorno, solo dopo l'ora del lavoro). **Scopo dello spegnimento: togliere le righe
«Saltato» e l'audit che dipendono dall'ora del giro, per un confronto più stabile. Non è il rimedio della nota 8** di
`docs/domande-aperte.md` (i «Database busy» e «database is locked» a caso): quattro comparse su nove cadono prima delle
09:00, quando lo scheduler non scrive; la causa resta aperta.

- **Come si spegne, e perché così.** Spegnendo i **lavori** nei dati: con `jobs.<lavoro>_enabled` a «0» `Jobs.due?/2`
  risponde no alla prima domanda (`Config.enabled?/2`) e il giro non scrive niente; restano le letture del tick, ogni
  minuto. È lo stesso gesto della casella «Attivo» della pagina e non tocca file generati della release. Esiste anche
  una via per fermare l'intero scheduler (misurata sulla fab-225): montare in sola lettura una copia di
  `releases/0.1.0/sys.config` con `tick_interval_ms` a `nil` e `RELEASE_SYS_CONFIG=/run/syscfg/sys`.
- **Il passo** `lavori` (`tools/banco/lavori.go`, `lavoriSpentiStandalone`): ultimo della catena dello standalone, prima
  della copia del database, con le funzioni dello standalone (`Settings.put/3` per `jobs.overdue_enabled`,
  `jobs.chases_enabled`, `jobs.briefing_enabled` a «0») e un controllo (`Config.get/0` dice spento per i tre,
  `Jobs.due?/2` falso); senza rete come gli altri passi, riga `GR_LAVORI:ok`, container `grgo-<nome>-lavori` tolto da
  `banco giu`. Cambia solo `banco su` con `-dati sintetici-import`.
- **Il Go riceve le stesse impostazioni con l'import** (le `jobs.*_enabled` non sono tra le chiavi solo Go): le tre
  caselle «Attivo» di `/settings/promemoria` sono spente nelle due app e il confronto non ha differenze. Il **Go con
  Telegram** invece, nella sua copia, **riaccende ritardi e solleciti** (`seminaTelegramGo`): `telegram-promemoria`
  li esegue a mano e aspetta «Job eseguito.»; il briefing resta spento come prima.
- **Non toglie** gli altri processi periodici dello standalone: `Outgoing.Dispatcher` (30 s), `MediaCleanup` (60 s;
  alla partenza e una volta al giorno la ritenzione delle foto) e `TelegramPoller`. Se la nota 8 si ripresenta con i
  lavori spenti, il sospetto passa a loro; le comparse vanno confrontate con l'ora di quei giri.

## Banco con il canale Telegram acceso (slice 074b)

Lo standalone del banco non ha il bot e la sua immagine non cambia: il flusso Telegram con il canale acceso si prova
**solo sul Go**, su un secondo Go dell'istanza. Codice in `tools/banco/gotelegram.go` (istanza), `botfinto.go` (bot
finto), `esegui.go` e `confronto.go` (scenari).

- **Cosa crea `banco su -dati sintetici-import`** (senza `-dati` niente di questo):
  - `grgo-<nome>-bot`: il **bot finto**, `banco bot-finto -ascolta :8081` dal binario del banco che fa `su`
    (`os.Executable`, montato in sola lettura; deve essere statico come gli altri), alias `bot.invalid` sulla rete
    interna. Nessuna porta sull'host, nessuna rete esterna. **`banco su` mai con `go run`:** il binario di
    `go run` è temporaneo e sparisce, e il bot finto gira proprio da lì; si usa sempre il `banco` compilato;
  - `grgo-<nome>-gotg`: il **Go con Telegram**, sullo stesso binario del Go del confronto, con i dati in
    `go-telegram-data/` dell'istanza: una copia di `go-data/` fatta subito dopo l'import, prima di avviare qualunque
    container. Nella copia `su` accende Telegram con le funzioni della pagina dei provider (`SalvaTelegram`,
    `ImpostaNotificheClienti`, `ImpostaTelegram`, attore `console`): token `4242:banco-finto-SINTETICO`, chat del
    negozio `-4242000`, «Notifiche Telegram ai clienti» e «Invio Telegram» accesi (rifiutato con un messaggio Telegram
    in coda: D-27), più il nome del bot `BancoSinteticoBot`, che la prova di getMe salverebbe, e (dalla slice 100) ritardi
    e solleciti riaccesi, perché i dati del banco hanno i lavori spenti. Ambiente
    `TELEGRAM_API_ENDPOINT=http://bot.invalid:8081`, configurazione `go-telegram.conf` dell'istanza con
    `indirizzo_pubblico = https://go-telegram.invalid:8443`, lo stesso segreto Flask sintetico;
  - nel Caddy il sito `https://go-telegram.invalid:8443` (stesse facce del Go del confronto) e il suo alias.
  `runtime.json` porta `go_telegram` e `bot`; `banco esegui` li legge, li aspetta con `attendi` e li passa a
  `confronta` con `-go-telegram` e `-bot`. `banco giu` toglie anche `-bot` e `-gotg`; i dati sono nella cartella
  dell'istanza. Il Go del confronto non cambia (Telegram spento) e gli scenari di sempre nemmeno.
- **Bot finto** (`botfinto.go`): `getMe`, `sendMessage` e `answerCallbackQuery` (registrati: solo il corpo, mai il
  token), `getUpdates` con la coda come Telegram (un aggiornamento resta finché una richiesta porta un offset dopo di
  lui; nessuna lettura lunga). Un token diverso da quello seminato è 401. Rotte di prova: `POST /_prova/aggiornamenti`
  (`{"comando":"start","token":…}`, `{"comando":"si"|"no"}` = il pulsante del consenso dell'ultimo benvenuto
  mandato, `{"comando":"stop"}`, dalla 075 `{"comando":"rispondi","testo":…}` = «Rispondi» all'ultima notifica con un
  link mandata al cliente), `GET /_prova/mandati[?metodo=…][&chat=…][&indice=N]` (`in_attesa`, `totale`, `ultimo`),
  `GET /_prova/salute`. Il cliente sintetico è la chat privata `4242001`.
- **Scenari** con `"istanza": "go-telegram"` (`bench/scenari/telegram-acceso.json`): girano tutti sul Go con
  Telegram (il loro cookie jar) e mai sulle due app; vogliono `prova_con` e solo passi `solo_go`; con dati che non
  li provano non girano (non provati), e senza `-go-telegram`/`-bot` sono un errore. Due cose nuove dei passi:
  - **passo «bot»** (`"bot": true`, solo lì, con `solo_go` e le attese): va al bot finto su `/_prova/…`; nel corpo
    `{start}` è il token del redirect a `https://t.me/…?start=` della risposta di prima del Go; non cambia né la
    pagina di prima né il token CSRF del Go. `"attendi": N` (solo su una GET «bot», al più 120 s) rifà la richiesta
    ogni mezzo secondo finché le attese tornano: il Go legge il bot e gira la coda (30 s) per conto suo;
  - nel corpo di un passo `solo_go`, `{campo:<nome>}` è il campo nascosto `<input name="<nome>" type="hidden" …>`
    della pagina di prima del Go (il gettone `notifica` del popup Telegram); se manca è un errore;
  - il campo **`json:<percorso>`** (in ogni passo) legge un valore di un corpo JSON: chiavi separate da «.», un
    numero è l'indice di una lista (`json:ultimo.reply_markup.inline_keyboard.0.0.url`).
- **Il flusso** di `telegram-acceso` (busta 23/26, del cliente diretto, in lavorazione, link `/r/` del banco):
  riquadro «Vuoi essere avvisato…», «Attiva su Telegram» (302 a t.me), `/start` nel bot, benvenuto con i due
  pulsanti, ancora «Vuoi essere avvisato…» (consenso non dato), «Sì», «✅ Notifiche attive…» e
  `answerCallbackQuery`, riquadro «Notifiche Telegram attive», login, scheda, «pronta», popup (titolo, numero,
  anteprima, pulsante), «Invia», «📲 Notifica Telegram accodata per il cliente», `sendMessage` della coda alla chat
  del cliente con testo HTML, `parse_mode` e il pulsante su `https://go-telegram.invalid:8443/r/<token>`; dalla 075 la
  risposta del negozio dalla scheda («Risposta inviata. 📲 Notifica Telegram accodata per il cliente», l'avviso CLI-48),
  «Rispondi» del cliente a quell'avviso, «✅ Messaggio inviato al negozio.» con `reply_parameters`, l'avviso alla chat
  del negozio `-4242000` (CLI-13) e le due righe nella scheda; poi `/stop`,
  «🔕 Notifiche disattivate…», riquadro di nuovo «Vuoi essere avvisato…». Il D dei passi è la D-03 (il canale
  acceso è l'interruttore solo del Go).

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
- `sidecar_estrazione`, `sidecar_estrazione_porta`, `sidecar_estrazione_salute` (e le stesse per
  `trascrizione`): i sidecar dell'IA che `serve` avvia (slice 111, sezione «Sidecar dell'IA»); senza
  righe nessuno. Le regole di forma dei due indirizzi qui sotto non valgono per queste righe.

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

- `-senza-lavori-automatici` (slice 094): il pianificatore dei lavori automatici (ritardi, solleciti, briefing) non
  parte, così nessuna riga di `esecuzioni_lavori` né messaggio nasce da solo a seconda dell'ora del giro. Il banco la
  passa ai due Go (`tools/banco/istanza.go`, `gotelegram.go`). «Esegui adesso» (098) resta. Avviso «ATTENZIONE …
  mai sull'apparecchio» a ogni avvio.

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
- **Le etichette della consegna all'artigiano (slice 078, D-09)** vanno alla stampante dell'etichetta busta, con
  le sue opzioni: sono la stessa 76×127, una pagina per busta, in un lavoro solo. «🖨 Stampa e conferma» conferma la
  consegna solo se il lavoro è accettato (`handover.labels_printed` con gli id stampati e `handover.confirmed` in una
  transazione; mai una conferma da 0: una consegna senza buste da timbrare non stampa); senza stampante scelta il
  pulsante è il link al PDF e conferma, come lo standalone. Il modulo porta il modo con cui la pagina è stata
  disegnata: se la stampante è cambiata intanto, né stampa né conferma. Un lavoro accettato e una conferma non
  scritta lasciano la traccia `handover.labels_printed` con esito `not_confirmed`.
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
- **Banco e revisione (coordinatore, 15/11/2026, dopo il banco s085p):** il banco non si lancia mentre il revisore fa
  girare le mutazioni o altri `go test` (CPU occupata, tempi falsati, giro da rifare: in s085p una corsa dello script
  in `anagrafica-artigiani`); in parallelo alla revisione va bene solo quando il revisore legge soltanto. Il banco si
  lancia solo con il «memoria libera» del coordinatore e la guardia sotto 10 GB, e a giro finito si scrive «banco
  finito» prima del commit.
- **Esito del banco e copertura (coordinatore, 15/11/2026):** l'esito del giro finale del banco si salva fuori
  dall'istanza e si passa a `scripts/check` (`GESTRIP_BANCO_ESITO`) **prima** di `banco giu`, così la copertura del
  resoconto è quella vera.
- **Slice senza banco (coordinatore, 16/11/2026):** se all'ora del banco il coordinatore non ha dato il «memoria
  libera», la slice si committa solo in locale con «banco da fare» nella ricevuta, senza push; push, copertura e riga
  di avanzamento dopo il giro verde (una correzione è un commit in più prima del push). Al massimo una slice in questo
  stato: alla seconda ci si ferma e lo si dice al coordinatore.
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
