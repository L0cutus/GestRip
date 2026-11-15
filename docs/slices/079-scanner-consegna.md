# 079 — Scanner continuo con la fotocamera nella sessione di consegna (T4.8, quarta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/handover_session_live.ex`
  (pulsante `#handover-scan-button` con `data-event="add_scanned"` e `data-count-label="in consegna"`, evento
  `add_scanned`, `add_code/3` con le letture `~w(qr numero_busta)`, `reply/3`, `qr_outcome/2`), `assets/js/qr_scanner.js`
  (modo continuo: `scanned/1`, `REPEAT_MS`, `REPLY_WAIT_MS`, risposta tardiva scartata, errore di rete),
  `assets/js/qr_sound.js`; test «the camera scanner (slice 186…)» di `handover_session_live_test.exs`; la ricevuta
  della slice 186 dello standalone (`docs/plans/2026-10-27-gestrip-banco-telefono/slices/186-consegna-artigiano-scanner.md`).
- **Voci:** ART-09 (portata; si chiude con l'esito verde del banco). Nessuna migrazione, nessuna D nuova.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco (Docker) non è stato lanciato:
  lo fa la sessione principale (sotto, «Banco»).

## Cosa c'è

- **Pagina** (`internal/web/pagine/consegne.html`, CSS in `app.css`): nel modulo del campo, dopo «Aggiungi», il
  pulsante `#handover-scan-button` «📷 Scansiona» (`type="button"`, a contorno come `btn-outline`), con
  `data-scansione="/handovers/{id}/scansione"`, `data-ricarica="/handovers/{id}"` e `data-count-label="in consegna"`;
  niente `data-target` né `data-submit` (lo scanner continuo non riempie il campo e non manda il modulo). Solo a
  sessione aperta: una sessione confermata non ha il modulo. Senza JavaScript non fa niente.
- **Rotta** `POST /handovers/{id}/scansione` (`internal/web/consegne_scansione.go`, `f.rotta`, `personale`, CSRF del
  modulo), l'evento `add_scanned`: il codice è `qr` senza spazi ai bordi (un `qr[…]` che Plug farebbe mappa o lista
  vale vuoto), letto **prima come QR e poi come numero** (`buste.LetturaScansione` della 077, l'ordine opposto al
  campo; solo «non trovata» passa alla seconda lettura). Risponde in JSON (`jsonCoda`, `no-store`) la reply dello
  standalone `{ok, message, count}` più `info`:
  - busta nuova: `ok` vero, «✓ Aggiunta busta #0045/26» (numero visibile D-64), `info` vero;
  - busta già della consegna (anche aggiunta nel frattempo da un altro telefono: la consegna si rilegge nella
    transazione): `ok` falso, «↻ Busta #0045/26 già in lista», `info` vero;
  - ogni rifiuto (`RifiutoAggiunta`, gli stessi testi del campo, con il codice): `ok` e `info` falsi;
  - `count`: le buste della consegna rilette dopo l'aggiunta (anche dopo un rifiuto, come `load_bags`);
  - `info` dice se il messaggio è d'informazione (`put_flash :info`) o un errore: alla chiusura la pagina mostra
    solo l'ultimo errore, e senza `info` «già in lista» (`ok` falso) non si distinguerebbe da un rifiuto.
  Un id che non è un numero positivo è 404 (la pagina non lo manda mai); una consegna chiusa o che non c'è è il
  rifiuto del dominio «Consegna non disponibile («X»)», come lo standalone, con il conto delle buste rilette: 0 per
  la consegna che non c'è, le buste ancora collegate per quella chiusa. Niente messaggio nella
  sessione, nessuna riga d'audit (come l'aggiunta dal campo); un guasto è il 500 delle altre pagine.
- **Lettore** (`internal/web/statici/app.js`): **lo stesso** lettore QR della home e di «Nuova riparazione»
  (`lettoreQR`: una finestra, un caricamento di jsQR, un ciclo dei fotogrammi), con il **modo continuo** quando il
  pulsante ha `data-scansione` (il modo continuo di `qr_scanner.js` della 186 dello standalone):
  - il tocco su 📷 sblocca l'audio e apre la finestra (il selettore del tocco ora comprende `[data-scansione]`);
  - la finestra resta aperta: ogni codice nuovo va con `fetch` (POST, `qr` e il token di `meta[name=csrf-token]`,
    `Accept: application/json`) e la riga di stato dice «Lettura…», poi il messaggio con «· N in consegna»;
  - il suono è l'esito del server: successo solo con `ok` (busta appena aggiunta), fallimento per ripetizione,
    codice sconosciuto e ogni rifiuto (`suonoQR.suona`, la vibrazione dove c'è);
  - lo stesso codice non riparte finché resta inquadrato (2 s senza vederlo lo rendono nuovo), un codice alla volta
    (il prossimo aspetta la risposta, o 5 s), un codice visto durante l'attesa non conta come visto; una risposta
    arrivata dopo la chiusura (o a un'altra apertura) non tocca più la riga di stato, ma suona (come
    `push_event("qr-outcome")` dello standalone) e il suo errore conta per la pagina riletta;
  - una richiesta che non arriva o una risposta che non è l'esito: «⚠ Errore di rete: la busta non è stata aggiunta,
    riprovo…», il suono di fallimento e una pausa di 2 s; una sessione scaduta (la risposta è il login) o un token
    scaduto (403, un'uscita e un nuovo accesso in un'altra scheda) portano alla pagina della sessione con una
    navigazione, senza ripetere l'errore all'infinito;
  - **alla chiusura** («Chiudi» o Esc), dopo almeno una scansione, si aspetta la scansione ancora senza risposta (al
    più 5 s), poi la pagina di `data-ricarica` si rilegge e prende il posto di questa (`mostraPagina`, l'indirizzo
    sostituito: anche dopo un rifiuto del campo, la cui pagina è quella del POST), con sopra **solo l'ultimo errore**
    (nello standalone il flash verde si chiude da sé al tocco su «Chiudi», `FlashAutoClose`; il rosso resta) o, senza
    un errore nuovo, quello che la pagina aveva già, e i campi di testo come li si era lasciati (il testo in corso di
    scrittura nel campo resta). Se nel frattempo la pagina è cambiata, non si sostituisce niente. Una risposta che non è la pagina della sessione (login,
    la lista con «Consegna non trovata.») o una rete che non risponde: una navigazione normale a quell'indirizzo.
- **Banco** (`tools/banco/fotocamera.go`, `tools/banco/browser/banco.mjs`, `bench/browser/lettore-qr.json`): una
  fotocamera finta in più, `consegna.y4m` (320×240, 10 fotogrammi al secondo, 160 fotogrammi): 2,5 s di bianco
  (la fotocamera parte prima della busta), il QR di `RIP-2026-0047` (la 26/26) per 1,5 s, il bianco per 3 s, il QR di `SINT-NIENTE-079` (di nessuna busta) per 1,5 s,
  il bianco, di nuovo la 26/26 e il bianco. La prova `scanner` di `lettore-qr` (`letturaConsegna`), sul PC e sul
  telefono, per ultima. `lettore-qr` ha ART-09 fra le voci (`browser_test.go` lo sa).
- **SVILUPPO:** un punto «Scanner continuo della consegna» dopo quello delle consegne, e nel punto della fotocamera
  finta la prova `scanner` che cambia i dati.

## Scelte

1. **Il lettore è uno.** Il modo continuo sta dentro `lettoreQR` (stato, finestra, fotogrammi, chiusura, Esc e
   `pagehide` comuni): un 📷 con `data-scansione` lo usa, gli altri restano come prima. ORE-07 (il lotto
   dell'oreficeria, T4.9) può riusarlo con il suo indirizzo e `data-count-label="nel lotto"`.
2. **Una rotta JSON al posto dell'evento.** Senza LiveView il `pushEvent` con la reply diventa un `fetch` con il
   JSON della reply; `info` in più perché la pagina sappia quale messaggio è un errore.
3. **La pagina si rilegge alla chiusura, non a ogni scansione.** In LiveView la lista sotto la finestra si ridisegna
   a ogni scansione e il flash resta; qui la finestra copre la pagina e la si rilegge una volta, chiudendo
   (Dubbio 1). Il flash dell'errore lo mette `app.js`: nella sessione del server non resta niente, così un messaggio
   non compare su un'altra pagina se si esce senza chiudere.
4. **«Già in lista» dal dominio.** Lo standalone confronta con le buste lette prima dell'aggiunta; il Go usa
   `Aggiunta.GiaInLista` (la busta era già di questa consegna, letta nella transazione): stesso esito, anche per la
   busta aggiunta da un altro telefono (provato).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/079-scanner-consegna/prove/`): `rosso-web.log` (pulsante,
  rotta, script, tabella dei permessi: «la tabella nomina "POST /handovers/{id}/scansione", che la faccia interna non
  registra»), `rosso-banco.log` (la fotocamera della consegna che non c'è).
- **Pagine** (`internal/web/consegne_scansione_test.go`): `TestPulsanteScansiona` (markup esatto accanto ad
  «Aggiungi», niente `data-target`/`data-submit`, assente a sessione confermata), `TestScansioneDellaConsegna` (busta
  nuova per QR con gli spazi, seconda busta, «già in lista», prima il QR anche quando il testo è il numero di
  un'altra busta, un numero letto, la busta aggiunta da un altro telefono, i rifiuti con i testi e il conto, un
  rifiuto della prima lettura che non prova la seconda, `qr` mancante o mappa o lista, nessun flash nella sessione,
  nessuna riga `handover.*`, consegna chiusa e che non c'è, id non valido 404, senza token 403),
  `TestScansioneDelTitolare`, `TestScriptDelloScannerContinuo` (le righe del modo continuo in `app.js`; un solo
  lettore: una finestra, un caricamento di jsQR, un ciclo dei fotogrammi; la chiusura che azzera attesa ed esiti).
- **Ritoccati:** `TestPaginaSessione` (il «Scansiona» assente era il segnaposto della 077), `TestScriptDelLettoreQR`
  (il selettore del tocco), `TestScriptDellaPistola` (`suonoQR.suona(false)` ora in due punti: la pistola e la rete
  dello scanner), la tabella dei permessi (`ruoli_test.go`, `ART-09`).
- **Banco** (`tools/banco/fotocamera_test.go`): `TestFotocameraDellaConsegna` (intestazione, 160 fotogrammi, ogni
  QR riletto dal fotogramma e decodificato con il suo testo, il bianco dove serve). Il comportamento nel browser lo
  prova il banco (sotto).

## Mutazioni

25 mutazioni a mano sul codice nuovo (rotta, pagina, `app.js`, fotocamera finta del banco), una alla volta su una copia
del repo (`to-be-deleted-after-use/079-scanner-consegna/mutazioni/`: `muta.py`, `giro.log`, `esiti.json`,
`esiti/<id>.log`; copia cancellata a fine giro). Tutte compilano. **25 prese su 25** al primo giro, tutte da
un'asserzione. Le mutazioni di `app.js` le prendono test sulle righe dello script (come le altre del lettore): il
comportamento nel browser lo prova il banco.

| Id | Mutazione | Presa da |
|---|---|---|
| W01 | campo scritto: prima il numero | TestScansioneDellaConsegna |
| W02 | «già in lista» con ok vero (suono di successo) | TestScansioneDellaConsegna |
| W03 | «già in lista» come errore | TestScansioneDellaConsegna |
| W04 | rifiuto come informazione | TestScansioneDellaConsegna |
| W05 | conto non riletto | TestScansioneDellaConsegna |
| W06 | id non valido trattato come 0 | TestScansioneDellaConsegna |
| W07 | testo dell'aggiunta | TestScansioneDellaConsegna |
| W08 | rotta solo del titolare | TestOgniRottaHaIlSuoPermesso, TestScansioneDellaConsegna |
| W09 | qr ignorato (sempre vuoto) | TestScansioneDelTitolare, TestScansioneDellaConsegna |
| T01 | pulsante senza data-count-label | TestPulsanteScansiona |
| T02 | pulsante che riempie il campo | TestPulsanteScansiona |
| T03 | campo e pulsante anche a sessione confermata | TestPaginaSessione, TestPulsanteScansiona |
| J01 | selettore senza lo scanner continuo | TestScriptDelLettoreQR, TestScriptDelloScannerContinuo |
| J02 | finestra di ripetizione a 1 s | TestScriptDelloScannerContinuo |
| J03 | suono di successo per ogni risposta | TestScriptDelloScannerContinuo |
| J04 | chiusura senza rileggere la pagina | TestScriptDelloScannerContinuo |
| J05 | chiusura che lascia l'attesa | TestScriptDelloScannerContinuo |
| J06 | stesso codice ripetuto subito | TestScriptDelloScannerContinuo |
| J07 | risposta tardiva non scartata | TestScriptDelloScannerContinuo |
| J08 | conto senza etichetta | TestScriptDelloScannerContinuo |
| J09 | esito d'errore come informazione | TestScriptDelloScannerContinuo |
| J10 | errore di rete senza pausa | TestScriptDelloScannerContinuo |
| B01 | sequenza senza il ritorno della busta | TestFotocameraDellaConsegna |
| B02 | bianco più corto della finestra di ripetizione | TestFotocameraDellaConsegna |
| B03 | codice sconosciuto uguale alla busta | TestFotocameraDellaConsegna |

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Nessuno scenario nuovo: la prova `scanner` è dentro
`lettore-qr` (ultimo scenario, con i suoi Chromium). Ci si aspetta **HTTP 71 scenari, 1203 passi** e **browser 19
scenari, 38 viste**, come la 078, 0 inattese, 0 errori.

- **Database vuoto:** nessun artigiano, si guarda solo il menu (`consegna.scanner.artigiani` = «0»), sulle due viste.
- **Dati** (`atteso_dati`), sul PC e sul telefono, nelle due app:
  - apertura della sessione dell'artigiano 1: sul PC una nuova (le consegne di `sessioni-consegna` sono confermate),
    sul telefono la stessa del PC, vuota dopo ✕ (`prima.conteggio` «0»); `pulsante` «📷 Scansiona»;
  - nel campo «0099/26» non mandato, poi il 📷; le righe di stato della finestra e i toni annotati con l'istante:
    le tre fasi in ordine, ognuna la prima riga dopo quella della fase prima: `aggiunta.messaggio` «✓ Aggiunta busta #N · 1 in consegna» con `1760` e vibrazione `60`;
    `sconosciuto.messaggio` «Riparazione non trovata: nessuna busta con numero o QR «SINT-NIENTE-079» · 1 in consegna»
    con `330 330` e `[120,80,120]`; `ripetuta.messaggio` «↻ Busta #N già in lista · 1 in consegna» con `330 330`;
    `finestra` «aperta» (resta aperta dopo tre letture);
  - «Chiudi»: `chiusa.percorso` «la sessione», `chiusa.conteggio` «1», `chiusa.messaggio` «(nessuno)»,
    `chiusa.errore` «Riparazione non trovata: … «SINT-NIENTE-079»», `chiusa.campo` `"0099/26"`, `chiusa.righe` senza
    attesa (confronto fra le app);
  - PC: ✕ sulla 26/26, `togli.conteggio` «0»; telefono: «✓ Conferma senza stampare», poi la sessione confermata senza
    pulsante (`confermata.pulsante` «0»). La 26/26 resta fuori bottega: dopo non gira niente sulla stessa app.
  - Il numero della busta è «#N» nei messaggi (D-64: «#26/26» nello standalone, «#0047/26» nel Go).
- **Se qualcosa è rosso, da guardare per primo:**
  - I toni entro un secondo dalla riga di stato: nel Go il suono parte subito dopo la riga; nello standalone dal
    `qr-outcome` della stessa risposta. Una finestra più larga se lo standalone è più lento.
  - Il conteggio prima della scansione sul telefono: se la prova del PC si è fermata prima di ✕, la sessione ripresa
    ha già la 26/26 e tutto il resto segue.
  - La fotocamera finta 320×240: se Chromium la scala o non la legge, nessuna riga di stato (`aggiunta.messaggio`
    «(nessuno)» nelle due app) dopo 45 s.
  - `consegna.scanner.artigiani` sul database vuoto: «0» se `anagrafica-artigiani` lascia l'anagrafica come l'ha
    trovata (lo dice la sua `elimina.come-prima`).

## Banco s079p (sessione principale) e revisione: correzioni

Banco `s079p`: HTTP verde; browser **14 inattese**, tutte della prova `scanner` di `lettore-qr`, sul PC e sul
telefono. Con la revisione del coordinatore (una ALTA, due MEDIE, cinque BASSE), corrette tutte; test rossi prima dove
tocca il Go (`prove/rosso-review.log`, `prove/rosso-banco-s079p.log`).

- **A — righe di stato senza le «s»** (uguali nelle due app: «✓ Aggiunta bu ta #N · 1 in con egna»). Difetto del
  banco: in `STATI_LETTORE` (uno script di `banco.mjs` dentro una stringa template) la regex era scritta `\s+` con
  una barra sola, che la stringa template mangia prima che lo script arrivi alla pagina: restava `/s+/g`, che toglie
  le «s». Ora `\\s+`. Gli altri script delle pagine (`OSSERVATORE`, `SUONI`, e `manuale.mjs`) non hanno il problema:
  li controlla il test nuovo `TestScriptDellePagineSenzaEscapePersi` (`tools/banco/script_pagina_test.go`: in ogni
  script `const X = (stringa template)` di `banco.mjs` e `manuale.mjs` una barra rovescia va scritta doppia),
  rosso prima della correzione.
- **B — `ripetuta.*` «(nessuno)» nelle due app.** Conseguenza di A: senza le «s» la riga «già in li ta» non
  somigliava più a `/già in lista/`, quindi l'attesa arrivava ai 45 s e la fase non si trovava. In più (revisione
  punto 2): le fasi si annotano in ordine, ognuna è la prima riga di stato che le somiglia **dopo** quella della fase
  prima; il video comincia con 2,5 s di bianco (160 fotogrammi), così la fotocamera e jsQR partono prima della busta
  (`TestFotocameraDellaConsegna` aggiornato).
- **C / ALTA 1 — dopo «Chiudi» lo standalone non mostra il flash verde.** `FlashAutoClose` (app.js dello standalone,
  righe 106-113, e `layouts.ex:513` a `302439d`): il tocco fuori dal flash verde lo chiude, e «Chiudi» è un tocco;
  il rosso resta finché non lo si tocca. Ora `rileggiPagina` rimette solo l'ultimo errore; `chiusa.messaggio` atteso
  «(nessuno)» nelle due viste. `info` resta nella risposta: serve a distinguere «già in lista» (`ok` falso, non un
  errore) da un rifiuto.
- **MEDIA 3 — «Chiudi» con una scansione in corso** rileggeva la pagina prima che l'aggiunta fosse scritta: ora
  `chiudiDaUtente` aspetta la risposta in corso (al più 5 s) e poi rilegge; se nel frattempo la pagina è cambiata non
  la sostituisce.
- **BASSA 4 — 403** (token scaduto dopo un'uscita e un nuovo accesso in un'altra scheda): prima «⚠ Errore di rete»
  all'infinito; ora, come il login, una navigazione alla pagina della sessione.
- **BASSA 5 — l'errore che la pagina aveva già** (un rifiuto dal campo) si perdeva: se le scansioni non ne hanno uno
  nuovo, resta quello di prima, come il flash rosso dello standalone.
- **BASSA 6 — il suono di una risposta tardiva** (dopo «Chiudi») ora suona, come `push_event("qr-outcome")`; la riga
  di stato non si tocca più.
- **BASSA 7 — testo della ricevuta** sul conto della consegna chiusa (sopra, «Rotta»).
- **BASSA 8 — rami senza test:** `TestScriptDelloScannerContinuo` ora fissa il login e il 403, il controllo di
  `r.ok` e del `content-type`, la navigazione normale di `rileggiPagina`, il `setAttribute("value", …)`, l'attesa della
  risposta, l'errore di prima, la pagina cambiata, e che non ci sia più il flash verde; `TestScansioneDellaConsegna`
  ha il caso `ErrPiuBuste` (due gemelle `RIP-2026-0047` aperte, «0047/26» letto: nessun QR con quel testo, poi il
  numero che vale per due buste → il rifiuto solo del Go, niente aggiunto; verde subito: il comportamento c'era).

**Mutazioni dopo il banco s079p e la revisione** (stessa cartella, `muta2.py`, `giro2.log`, `esiti/R*.log`; copia
cancellata a fine giro): **9 prese su 9**. Al primo giro R06 era viva perché il controllo della pagina cambiata c'era
due volte (togliendone uno restava l'altro): ora c'è una volta sola, dopo la risposta, e R06 è presa.

| Id | Mutazione | Presa da |
|---|---|---|
| R01 | dopo la chiusura anche «già in lista» come errore | TestScriptDelloScannerContinuo |
| R02 | «Chiudi» non aspetta la scansione in corso | TestScriptDelloScannerContinuo |
| R03 | 403 come errore di rete | TestScriptDelloScannerContinuo |
| R04 | l'errore di prima non resta | TestScriptDelloScannerContinuo |
| R05 | risposta tardiva muta | TestScriptDelloScannerContinuo |
| R06 | pagina cambiata nel frattempo sostituita | TestScriptDelloScannerContinuo (secondo giro) |
| R07 | di nuovo anche il flash verde | TestScriptDelloScannerContinuo |
| R08 | video senza il bianco iniziale | TestFotocameraDellaConsegna |
| R09 | barra rovescia sola in `STATI_LETTORE` | TestScriptDellePagineSenzaEscapePersi |

## Controlli

`scripts/check` verde (`to-be-deleted-after-use/079-scanner-consegna/prove/check1.log`, «uscita 0»): gofmt, vet,
staticcheck, errcheck, build senza CGO, test, copertura **voci 353: chiuse 39, rosse 0, parziali 146, aperte 157,
fuori v1 11**, segreti e dati veri OK. ART-09 passa da aperta a parziale; si chiude con l'esito verde del banco.
`tools/banco` compilato con i suoi test verdi (dentro `scripts/check`). Dopo il banco s079p e la revisione di nuovo verde: `prove/check2.log` e,
dopo l'ultima modifica di `app.js` (R06), `prove/check3.log`, «uscita 0», stessi numeri della copertura.

## Per il manuale

- Nella sessione di consegna c'è «📷 Scansiona» accanto ad «Aggiungi»: la fotocamera resta accesa e ogni busta
  inquadrata entra subito nella lista. Sotto il video si legge l'esito («✓ Aggiunta busta #0045/26 · 3 in consegna») e
  si sente un bip acuto; una busta già in lista, un codice sconosciuto o una busta che non si può dare danno due bip
  gravi e il motivo.
- La stessa busta tenuta ferma davanti alla fotocamera non si ripete: per rileggerla la si toglie dall'inquadratura
  per un paio di secondi.
- «Chiudi» torna alla sessione con la lista aggiornata e, se c'è stato, l'ultimo errore; quello che si stava scrivendo nel campo
  resta.
- Il suono si accende con il primo tocco su 📷 (🔊/🔇 come sulla home).

## Dubbi

1. **La lista sotto la finestra si aggiorna alla chiusura, non a ogni scansione.** Nello standalone la lista e il
   flash sotto la finestra (semitrasparente) si ridisegnano a ogni scansione; nel Go la finestra copre una pagina che
   si rilegge quando la si chiude. Quello che si vede nella finestra (esito e conto) è uguale. Differenza voluta senza
   una D. **Raccomandazione:** tenere così (rileggere la pagina a ogni scansione costa una richiesta in più per busta
   e non si vede sotto la finestra).
2. **Il suono del campo scritto.** Nello standalone la 186 spinge `qr-outcome` anche per il campo «Numero busta»:
   dopo un tocco su 📷 (che sblocca l'audio) anche la pistola suona l'esito, perché la pagina LiveView non si
   ricarica. Nel Go «Aggiungi» ricarica la pagina e l'audio si risblocca solo con un tocco: la pistola resta muta. È
   ART-08 più che ART-09. **Raccomandazione:** lasciare così per ora (la pistola ha il cursore che resta nel campo);
   se Stefano lo vuole, il campo può partire con `fetch` come il campo unico della home (slice 059).

## Banco finale (sessione principale)

`s079q`, 15/11/2026, istanza nuova dopo le correzioni di `s079p` e della revisione: HTTP 71 scenari, 1203 passi,
0 inattese, 0 errori; browser 19 scenari, 38 viste, 0 inattese, 0 errori. `scripts/check` rifatto dalla sessione
principale: verde. Il messaggio dopo «Chiudi» è allineato allo standalone (il coordinatore, 15/11): nessuna D nuova.
