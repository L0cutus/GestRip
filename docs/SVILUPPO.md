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
    pannello QR è della 068 (sotto); gli altri link wa.me della D-74 (lotti, artigiani, preventivo) arrivano
    con le loro slice;
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
  dominio in `internal/buste/artigiani.go`). La pagina ha per ora la sola parte degli artigiani: le oreficerie clienti
  (ORE-01…03) e «Profilo negozio» (ADM-02, compare solo al titolare quando `GET /settings/shop` è registrata) arrivano
  con la T4.9. Il telefono si normalizza con `buste.NormalizzaTelefono` (= `normalizza_telefono`, cifre di Python);
  il doppione è a maiuscole esatte alla creazione e con `LOWER` (solo ASCII) alla modifica, come lo standalone.
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
  di sette giorni (`aggiornata_il`) si dimentica e la lettura parte senza. Un aggiornamento che fallisce (errore delle iscrizioni o panico) ferma la lettura
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
- **Offset vecchio (D-85, solo Go, Dubbio 1 della 073):** vedi «Bot Telegram» sopra. A sette giorni esatti vale ancora;
  un `aggiornata_il` che non si legge non decide niente; un offset non intero vale come nessuno. Senza offset Telegram
  ridà solo i non confermati. Telegram conta la settimana dall'arrivo dell'ultimo aggiornamento, il Go da quando ha
  scritto l'offset (fino a circa 24 ore di differenza): Dubbio 6 della ricevuta 075.
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
  deve chiamare `avvisaPronta` nella sua transazione** quando l'esito porta a «pronta» (lo standalone lo fa in
  `return_from_artisan/4`). Nessuna migrazione: `email_pronta_inviata_il` c'è dalla 0007, l'indice
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
  la **068 per «↗ Apri WhatsApp» del pannello del QR** dopo «✅ Valutata»: gli altri punti wa.me non ci sono
  ancora nel Go e la prendono con le loro slice.

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
  collegati nella prima slice utile.
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
    in coda: D-27), più il nome del bot `BancoSinteticoBot`, che la prova di getMe salverebbe. Ambiente
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
