# 074b — Banco con il canale Telegram acceso (solo Go)

- **Fonte:** piano della ricevuta 074 (sezione «Banco»), approvato da Stefano il 13/11/2026 («074b prima della 075»,
  `docs/SVILUPPO.md`, «Decisioni di Stefano del 13/11/2026»). Nessun codice dello standalone: la sua immagine e il
  suo banco non cambiano.
- **Voci:** nessuna voce nuova chiusa. Lo scenario nuovo porta CLI-44, CLI-46, CLI-47 e RIP-H-18 (già portate con
  072–074): con il banco verde le prova anche a scatola chiusa.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Nessuna migrazione, nessun codice
  del prodotto toccato: solo `tools/banco`, uno scenario e i documenti.

## Cosa c'è

- **Bot API finto** (`tools/banco/botfinto.go`, sottocomando `banco bot-finto -ascolta :8081`): `getMe` (nome
  `BancoSinteticoBot`), `sendMessage` e `answerCallbackQuery` registrati (solo il corpo: il token non è mai in una
  risposta), `getUpdates` con la coda come Telegram (un aggiornamento resta finché un `offset` dopo di lui non lo
  conferma), 401 con un altro token, 404 `ok:false` per un metodo che non c'è, 400 per un `sendMessage` senza testo
  o chat. Rotte di prova: `POST /_prova/aggiornamenti` (`start` con un token, `si`/`no` = il pulsante del consenso
  dell'ultimo benvenuto mandato, `stop`), `GET /_prova/mandati[?metodo=…][&indice=N]` (`in_attesa`, `totale`,
  `ultimo`), `GET /_prova/salute`. Il cliente sintetico è la chat privata `4242001`. Il comando si ferma con
  SIGTERM/SIGINT (lo stop del container, `--init`).
- **Seconda istanza Go** (`tools/banco/gotelegram.go`, `istanza.go`): con `-dati sintetici-import`, `banco su`
  - copia `go-data/` in `go-telegram-data/` subito dopo l'import (database e media, prima di avviare qualunque
    container) e accende Telegram nella copia con le funzioni della pagina dei provider, attore `console`
    (`SalvaTelegram` con token `4242:banco-finto-SINTETICO` e chat del negozio `-4242000`,
    `ImpostaNotificheClienti`, `ImpostaTelegram`, che rifiuta con un messaggio Telegram già in coda: D-27), più il
    nome del bot nelle impostazioni (quello che la prova di getMe salverebbe);
  - scrive `go-telegram.conf` (0600) con `indirizzo_pubblico = https://go-telegram.invalid:8443`;
  - avvia, dopo il Go del confronto, `grgo-<nome>-bot` (alias `bot.invalid`, dal binario del banco che sta facendo
    `su`, in sola lettura) e poi `grgo-<nome>-gotg` (stesso binario `gestrip`, `TELEGRAM_API_ENDPOINT=
    http://bot.invalid:8081`, `-config` sul file sopra, stesso segreto Flask sintetico, stesse protezioni del Go del
    confronto);
  - aggiunge al Caddy il sito `https://go-telegram.invalid:8443` (le due facce del Go con Telegram) e il suo alias;
  - scrive in `runtime.json` `go_telegram` e `bot`.
  `banco esegui` li legge (`telegramDiRuntime`), aggiunge a `attendi` il `/healthz` del Go con Telegram e la salute
  del bot, e passa a `confronta` `-go-telegram` e `-bot`. `banco giu` toglie anche `-bot` e `-gotg`
  (`suffissiContainer`); i dati stanno nella cartella dell'istanza, che `giu` toglie già. Senza `-dati` non cambia
  niente.
- **Scenari go-telegram** (`confronto.go`, `esegui.go`): `"istanza": "go-telegram"` fa girare tutto lo scenario sul
  Go con Telegram (cookie suoi) e sul bot finto, mai sulle due app; vuole `prova_con` e solo passi `solo_go`; con
  dati che non lo provano non gira (non provato, nessuna richiesta), e senza le due istanze è un errore del banco.
  Cose nuove dei passi:
  - **passo «bot»** (`"bot": true`, solo in uno scenario go-telegram, con `solo_go` e le attese, su `/_prova/…`):
    nel corpo `{start}` è il token del redirect a `https://t.me/…?start=` della risposta di prima del Go (host
    `t.me`, token con la forma che il bot accetta, altrimenti errore); il passo non cambia la pagina di prima né il
    token CSRF del Go;
  - **`"attendi": N`** (solo GET «bot», 1–120 s): la richiesta si rifà ogni 0,5 s finché le attese tornano, poi
    l'ultima risposta si confronta con le attese come sempre;
  - **`{campo:<nome>}`** nel corpo di un passo `solo_go`: il campo nascosto della pagina di prima del Go (il gettone
    `notifica` del popup), con le entità decodificate; se manca è un errore;
  - **campo `json:<percorso>`**: il valore a quel percorso di un corpo JSON (chiavi con «.», indici numerici; testo
    com'è, numeri come scritti, oggetti e liste in JSON compatto con le chiavi in ordine; «(assente)» e «(non
    JSON)»).
- **Scenario** `bench/scenari/telegram-acceso.json` (20 passi, tutti `solo_go` D-03, busta 23/26 col token del
  banco): riquadro A → «Attiva su Telegram» (302 a t.me) → `/start` → benvenuto con i due pulsanti → riquadro
  ancora A (consenso non dato) → «Sì» → «✅ Notifiche attive…» e `answerCallbackQuery` → riquadro B «Notifiche
  Telegram attive» → login → scheda → `pronta` → popup (titolo, «0046/26», anteprima senza tag, pulsante, «Invia»)
  → «Invia» → «📲 Notifica Telegram accodata per il cliente» e popup sparito → `sendMessage` della coda alla chat
  `4242001` con il testo HTML esatto, `parse_mode` `HTML` e il pulsante «📂 Apri scheda riparazione» su
  `https://go-telegram.invalid:8443/r/<token>` (attesa fino a 90 s: la coda gira ogni 30) → `/stop` → «🔕
  Notifiche disattivate per 1 iscrizione/i. Grazie!» → riquadro di nuovo A. Le attese dei passi del bot vogliono
  anche `in_attesa` 0 (il lettore ha confermato l'aggiornamento: l'ha finito) e il `totale` dei `sendMessage` (1, 2,
  3, 4: niente in più).
- **Documenti:** `docs/SVILUPPO.md` (sezione nuova «Banco con il canale Telegram acceso», righe «Banco» delle sezioni
  073 e 074, decisione del 13/11 «fatta con la 074b»), commento d'uso di `tools/banco/main.go`.

## Scelte

- **Popup nel livello HTTP, non nel browser.** Nel Go il popup Telegram è una pagina disegnata dal server (gettone
  nel deposito, modulo POST con `notifica` e `azione`), senza LiveView: con `{campo:notifica}` il flusso intero si fa
  in HTTP puro e prova davvero le scritture (coda, bot). Il livello browser proverebbe in più solo `app.js` (fuoco,
  Esc, tocco fuori), che i test Go della 074 già guardano; costerebbe un terzo bersaglio in `banco.mjs` e un'altra
  immagine da tenere allineata.
- **Attesa della coda con `attendi`, non con una pausa fissa:** il passo rifà la GET al bot finché le attese tornano
  (al più 90 s per la coda, 30 per il lettore). Un giro veloce non aspetta 30 s, uno lento non diventa rosso per un
  secondo. Scaduto il tempo, la differenza è inattesa come sempre.
- **`in_attesa` 0 come segno che il Go ha finito un aggiornamento:** il lettore salva l'offset dopo ogni
  aggiornamento e lo manda alla lettura dopo, quindi un aggiornamento confermato è già tutto scritto (iscrizione,
  consenso, risposta). Così il passo della pagina dopo «Sì» non corre contro il lettore.
- **Indirizzi fissi `.invalid`, non per istanza:** `bot.invalid` e `go-telegram.invalid` sono alias della rete
  interna dell'istanza (ognuna ha la sua), quindi non si scontrano fra istanze; l'indirizzo pubblico del Go con
  Telegram è proprio il suo sito nel Caddy, così il link del pulsante del messaggio è vero e lo scenario lo scrive
  per intero.
- **Il bot finto dal binario del banco che fa `su`** (`os.Executable`, collegamenti risolti, `percorsoAmmesso`):
  niente flag nuovi per `su`. Il binario deve essere statico (`CGO_ENABLED=0`), come quello che `esegui` monta già.
- **Impostazioni con le funzioni della pagina dei provider**, non con righe scritte a mano (tranne il nome del bot,
  che salva solo la prova di getMe e non è esportata): stesse regole del titolare, comprese le righe d'audit e il
  rifiuto della D-27 con messaggi Telegram in coda. Se un giorno i dati sintetici ne avranno, `su` si ferma con un
  messaggio chiaro invece di mandarli al bot finto.
- **Copia dei dati subito dopo l'import:** il Go con Telegram parte dagli stessi dati del Go del confronto ma non ne
  vede le scritture degli scenari (e viceversa): `telegram-acceso` può cambiare la 23/26 senza toccare le attese
  degli altri scenari, e gira nell'ordine dei nomi senza `in_fondo`.
- **Scenario non provato senza richieste** (diverso dagli scenari comuni, che girano lo stesso): con i soli account
  seminati il Go con Telegram non c'è e non c'è una busta da seguire.
- **Nessuna riga in `bench/deviazioni.json`** e nessun normalizzatore: ogni passo ha solo attese esatte.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-074b/prove/rosso.log` e `rosso-per-test.log`): con le
  funzioni nuove vuote, rossi sull'asserzione `TestBotFinto…` (6), `TestSeminaTelegramGo`, `…SenzaDatabase`,
  `…ConRigheInCoda`, `TestFormeSintetiche`, `TestComandoBotFinto`, `TestComandoGoTelegram`,
  `TestCaddyfileConTelegram`, `TestContainerTelegramDaTogliere`, `TestIstanzeDaFlag`,
  `TestComandiEseguiConTelegram`, `TestTelegramDiRuntime`, `TestPreparaGoTelegram`, `TestScenarioGoTelegram`,
  `…SenzaIstanze`, `TestSegnapostoTelegramRifiutati`, `TestPassoBotAttende`, `TestLeggiScenarioGoTelegram`,
  `TestValoreJSON`, `TestScenarioTelegramAccesoNelRepo`, `TestSuConLaCatena`, `TestSuSenzaDatabaseDelGo`
  (`TestCmdBotFinto` restava appeso sullo stub e si è provato dopo). Poi tutti verdi.
- **Il bot finto parla con il Go vero** (`TestBotFintoConIlLettoreVero`): su un database seminato da
  `seminaTelegramGo`, `uscita.NuovoTelegram` (getMe, `Consegna` di una riga HTML con il pulsante) e
  `uscita.NuovoLettoreTelegram` con iscrizioni finte fanno `/start` → benvenuto con `cli:tx:1:<id>` → «Sì» →
  `answerCallbackQuery` e «✅ Notifiche attive…» → `/stop`, con le conferme di `getUpdates`. È la prova più vicina al
  banco che si può fare senza Docker.
- **`su` con il Docker finto** (`TestSuConLaCatena`): ordine dei container (`… app caddy go bot gotg`), la copia
  dei dati con Telegram acceso solo lì (il Go del confronto resta spento), i comandi del bot e del Go con Telegram,
  l'alias in Caddy, il Caddyfile, `runtime.json`. `TestSuSenzaCatena`: niente di questo senza `-dati`.
  `TestSuSenzaDatabaseDelGo`: senza il database dell'import `su` si ferma prima di avviare qualunque container.
- **Scenario del repo** (`TestScenarioTelegramAccesoNelRepo`): istanza, `prova_con`, D-03 su ogni passo, i tre
  comandi del bot in ordine, almeno tre attese, il pulsante sull'indirizzo pubblico del Go con Telegram e la busta
  del token del banco.

## Mutazioni

27 mutazioni a mano sul codice nuovo (bot finto, `su`/`giu`/`esegui`, passo «bot», validazione e `json:`), una
alla volta su una copia del repo (`to-be-deleted-after-use/slice-074b/mutazioni/`: `muta.py`, `giro.log`, un log per
mutazione; copia cancellata a fine giro). **27 prese su 27**, tutte da un'asserzione. Quattro (M05, M12, M17, M22)
al primo giro non compilavano (variabile o import rimasti inutili): riscritte in modo che compilino e rifatte, prese.

| # | Mutazione | Presa da |
|---|---|---|
| M01 | token del bot: basta il prefisso «bot» | `TestBotFintoGetMeETokenSbagliato` |
| M02 | getUpdates: l'offset uguale conferma | `TestBotFintoStartEConferma`, `TestBotFintoConIlLettoreVero` |
| M03 | pulsante dal primo benvenuto | `TestBotFintoPulsante` |
| M04 | token di /start fino a 128 | `TestBotFintoRifiuti` |
| M05 | mandati senza filtro del metodo | `TestBotFintoMandati`, `TestBotFintoConIlLettoreVero` |
| M06 | sendMessage senza testo accettato | `TestBotFintoRifiuti` |
| M07 | semina senza invio Telegram | `TestBotFintoConIlLettoreVero`, `TestSeminaTelegramGo`, `TestPreparaGoTelegram`, `TestSeminaTelegramGoConRigheInCoda`, `TestSuConLaCatena` |
| M08 | semina senza il nome del bot | `TestSeminaTelegramGo` |
| M09 | configurazione vuota | `TestPreparaGoTelegram` |
| M10 | Go con Telegram senza l'endpoint del bot | `TestComandoGoTelegram` |
| M11 | Go con Telegram prima del bot | `TestSuConLaCatena` |
| M12 | Caddy senza l'alias del Go con Telegram | `TestSuConLaCatena` |
| M13 | confronta senza -bot | `TestComandiEseguiConTelegram` |
| M14 | runtime con indirizzi qualunque | `TestTelegramDiRuntime` |
| M15 | scenario go-telegram sul Go del confronto | `TestScenarioGoTelegram` |
| M16 | scenario go-telegram anche non provato | `TestScenarioGoTelegramSenzaIstanze` |
| M17 | attendi senza ripetere | `TestPassoBotAttende` |
| M18 | il passo del bot cambia la pagina di prima | `TestScenarioGoTelegram` |
| M19 | {start} da qualunque host | `TestSegnapostoTelegramRifiutati` |
| M20 | attendi su ogni passo | `TestLeggiScenarioGoTelegram` |
| M21 | json con un secondo valore | `TestValoreJSON` |
| M22 | campo nascosto senza entità | `TestScenarioGoTelegram` |
| M23 | Caddy del Go con Telegram sul Go del confronto | `TestCaddyfileConTelegram` |
| M24 | giu senza il bot | `TestContainerTelegramDaTogliere` |
| M25 | un solo flag basta | `TestIstanzeDaFlag` |
| M26 | bot in uno scenario comune | `TestLeggiScenarioGoTelegram` |
| M27 | scenario go-telegram con passi comuni | `TestLeggiScenarioGoTelegram` |

## Banco

**Giro finale valido `s074br`** (sessione principale, binari dell'albero finale, dopo `scripts/check` verde): **BANCO
VERDE**, HTTP 66 scenari, 1157 passi, 1548 differenze attese, 0 inattese, 0 errori, non provati 0; **BANCO BROWSER
VERDE**, 17 scenari, 34 viste, 30 attese, 0 inattese, 0 errori; `telegram-acceso` nell'esito; `banco giu`: «AVANZI:
nessuno». Prima due giri non validi: `s074bp` aveva l'HTTP verde e il browser rosso per il guasto saltuario dello
standalone (`500 GET /audit` in `provider-posta`, lato standalone in timeout; il Go con i valori attesi); `s074bq` è
stato fermato pulito con la memoria di pc-fisso a 9 GB (sotto i 10). `s074br` è partito con il «memoria libera» del
coordinatore (minimo 18 GB durante il giro).

**Giro preliminare `s074b1`** (sessione principale, binari di prima della review): **BANCO VERDE**, HTTP 66 scenari,
1157 passi, 0 inattese, 0 errori; **BANCO BROWSER VERDE**, 17 scenari, 0 inattese; `telegram-acceso` nell'esito;
`banco giu` senza avanzi. Il nome del negozio nelle attese («Negozio», Dubbio 2) è confermato dal giro. Le correzioni
della review toccano solo i test, un commento e il `message_id` del pulsante nel bot finto (che il Go non legge):
il giro dopo la review deve dare lo stesso esito.

### Come si lancia (sessione principale)

Non lanciato qui (regola della slice: Docker e memoria di pc-fisso li gestisce la sessione principale). Comandi,
da pc-fisso, nel repo, con i binari compilati come al solito (`B=to-be-deleted-after-use/<cartella>/bin`):

    sg docker -c "$B/banco su -nome X -immagine gestrip-standalone:fab-225 -gestrip $B/gestrip -dati sintetici-import"
    sg docker -c "$B/banco esegui -nome X -banco $B/banco"
    sg docker -c "$B/banco giu -nome X"

Niente di più: `su` crea da sé il bot e il Go con Telegram (usa come bot il binario `$B/banco` che lo sta
eseguendo, che quindi deve essere quello statico di sempre e restare lì fino a `giu`). Memoria: due container in più,
il Go con Telegram (limite 4 GB come l'altro Go, ne usa poche decine di MB) e il bot (limite 256 MB).

Cosa ci si aspetta:
- `su`: dopo il riassunto dell'import la riga «catena: copia dei dati per il Go con Telegram…», poi `PRONTA`. Se si
  ferma con «invio Telegram non acceso: … in coda», i dati sintetici hanno un messaggio Telegram in coda (D-27): da
  guardare prima di andare avanti.
- `esegui`, livello HTTP: **66 scenari** (65 + `telegram-acceso`), **1157 passi** (1137 + 20), 0 inattese, 0
  errori, non provati 0; `telegram-acceso` dura circa 30–60 s (l'attesa della coda). Browser come prima (17 scenari,
  34 viste).
- Il nome del negozio nei due testi esatti del bot e nell'anteprima del popup è «Negozio» (il ripiego del Go senza
  profilo del negozio): confermato dal giro `s074b1`.
- Altre cose da guardare se qualcosa è rosso: i nomi `.invalid` si risolvono con il DNS di Docker (il resolver Go non
  li tratta a parte; solo `.onion` sì); il primo `getUpdates` del Go con Telegram parte subito all'avvio, quindi il bot
  deve essere su prima (lo è, per ordine).
- `giu`: «AVANZI: nessuno» (anche `grgo-X-bot` e `grgo-X-gotg`).

## Controlli

Dopo la review: `scripts/check` verde (`prove/check4.log`, «uscita 0»; copertura: voci 353: chiuse 39, rosse 0, parziali 132, aperte 171, fuori v1 11).

Prima della review: `scripts/check` verde al terzo giro (`prove/check3.log`, «uscita 0»; copertura: **voci 353: chiuse 39, rosse 0,
parziali 132, aperte 171, fuori v1 11**). Primo giro rosso su staticcheck (un errore che cominciava con «Go»
maiuscolo, ST1005), secondo su errcheck (tre `fmt.Fprint` senza `_, _ =` nel bot finto e nel test): corretti.

Le chiuse scendono da 42 (074c) a 39: CLI-44, CLI-46 e CLI-47 (e RIP-H-18, già parziale) ora hanno uno scenario del
banco, e una voce con scenari è chiusa solo con l'esito verde del banco (`GESTRIP_BANCO_ESITO`, PIANO §5), che
`scripts/check` qui non ha. Tornano chiuse con l'esito del primo giro verde.

## Dubbi

1. **D dei passi: D-03.** I passi `solo_go` vogliono una D approvata; ho usato la D-03 («email e Telegram spenti
   finché il titolare non li accende»), perché il canale acceso è proprio l'interruttore solo del Go e il popup
   esiste solo lì. Non è una deviazione nuova. Alternativa: lasciare che uno scenario go-telegram non chieda una D
   per passo (il motivo è il banco, non una differenza dallo standalone). Raccomandazione: tenere la D-03, così il
   controllo delle D approvate resta uguale per tutti i passi.
2. **Nome del negozio nelle attese** («Negozio»): scritto da lettura del codice, **chiuso**: confermato dal giro
   preliminare `s074b1`.
3. **Copertura:** le tre voci tornano «chiuse» solo con l'esito del banco (sopra, Controlli).
4. **Avviso del nuovo messaggio (CLI-48) e avvisi alla chat del negozio (CLI-08, CLI-13)** non sono nello scenario:
   il piano chiedeva il cambio di stato. Il bot finto e i passi li permettono già (i `sendMessage` alla chat del
   negozio `-4242000` si leggono con `GET /_prova/mandati?metodo=sendMessage&indice=N` e `json:ultimo.chat_id`);
   raccomandazione: aggiungerli con la 075 o la slice di «📲 Avvisa cliente» su Telegram, che
   toccano le stesse cose.

## Review

Revisore: sottoagente indipendente, 14/11/2026, sul lavoro non committato. **Nessuna ALTA.** Corrette tutte:

- **MEDIA 1, protezioni del Go con Telegram.** `TestComandoGoTelegram` vuole in `comandoGoTelegram` tutte le
  protezioni e i limiti del Go del confronto (`protezioni`: `--init`, `--user 1000:1000`, `--cap-drop ALL`,
  `no-new-privileges`, `--read-only`, `--tmpfs`, log, `limitiContainer`) e nessuna porta pubblicata (anche per il
  bot); `TestSuConLaCatena` le confronta fra i due `docker run` registrati (`go` e `gotg`) e vuole che nessun
  container registrato abbia `-p`, `-P`, `--publish` o `--publish-all`.
- **MEDIA 2, `TestScenarioTelegramAccesoNelRepo` più stretto:** ogni GET «bot» su `metodo=sendMessage` vuole
  `json:in_attesa` 0 e i totali sono esattamente 1, 2, 3, 4; dopo `{"comando":"si"}` la prima GET `/r/` vuole
  «Notifiche Telegram attive» presente e «Vuoi essere avvisato…» assente; c'è un solo POST `…/notifica-telegram`
  con `notifica={campo:notifica}`, `azione=invia` e il 302.
- **BASSA 3:** il `callback_query.message.message_id` del pulsante è il `message_id` che `sendMessage` aveva dato al
  benvenuto (salvato nel mandato), non la posizione fra i mandati; `TestBotFintoPulsante` lo controlla, con un
  messaggio del cliente prima perché i due numeri siano diversi.
- **BASSA 4:** `TestBotFintoRifiuti` vuole 401 (e niente registrato) per un metodo con «/» (`sendMessage/x`,
  `getMe/`).
- **BASSA 5:** `TestSegnapostoTelegramRifiutati` ha il redirect `http://t.me/…` per `{start}`.
- **BASSA 6, test fragili:** in `TestPassoBotAttende` il `sendMessage` dalla goroutine non chiama più `t.Fatal`
  (esito su un canale, aspettato con un limite) e i limiti alti dei tempi sono larghi (15 s, 6 s, 3 s; quelli bassi
  restano); in `TestCmdBotFinto` l'attesa del bot pronto è un `select` con l'errore di avvio e 10 s.
- **BASSA 7:** `docs/SVILUPPO.md`: «`banco su` mai con `go run`», perché il bot finto gira dal binario di
  `os.Executable`.

Mutazioni sopravvissute alla prima versione, rifatte dopo le correzioni su una copia del repo
(`to-be-deleted-after-use/slice-074b/review/`: `muta.py`, `giro.log`, un log per mutazione; copia cancellata):
**7 prese su 7**, tutte da un'asserzione.

| # | Mutazione | Presa da |
|---|---|---|
| R04 | metodo del Bot API con «/» accettato | `TestBotFintoRifiuti` |
| R11 | `{start}` da un redirect `http://t.me` | `TestSegnapostoTelegramRifiutati` |
| R14 | Go con Telegram senza `--cap-drop ALL` | `TestComandoGoTelegram`, `TestSuConLaCatena` |
| R24 | scenario senza `json:totale` 3 dopo «Invia» | `TestScenarioTelegramAccesoNelRepo` |
| R25 | scenario senza le attese del riquadro B dopo «Sì» | `TestScenarioTelegramAccesoNelRepo` |
| R27 | Go con Telegram con `-p 47102:47102` | `TestComandoGoTelegram`, `TestSuConLaCatena` |
| R28 | Go con Telegram senza `--read-only` | `TestComandoGoTelegram`, `TestSuConLaCatena` |
