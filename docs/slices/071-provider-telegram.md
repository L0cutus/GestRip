# 071 — Provider Telegram, configurazione, riquadro Telegram della pagina dei provider, D-03 e D-27 per Telegram (T4.7, prima slice)

- **Fonte:** via di Stefano del 12/11/2026 (T4.7 → T4.8), ripresa `2026-11-12-fine-070c.md`, brief del coordinatore
  del 13/11. Lo standalone a `go-baseline` = `302439d` (`outgoing.ex`: `record/2`, `rate_limit_wait/2`,
  `refused_for_good?/2`, `blocked?/2`, `attempt/1`, `dispatch_once/1`, `enqueue/3`; `outgoing/providers/telegram.ex`;
  `outgoing/config.ex`; `outgoing/message.ex`; `outgoing/telegram_inbound.ex`: `set_enabled/2`, `status/0`,
  `refresh_bot_username/0`, `token_keys/0`; `telegram_subscriptions.ex`: `block_chat/1`; `providers_live.ex`;
  `config/runtime.exs`; `telegram_queue_limits_test.exs`, `providers_live_test.exs`: letti, non fatti girare).
- **Voci:** ADM-10 (provider Telegram), CLI-51 parte Telegram (regole 429/400/403 e notifiche ai clienti spente),
  CLI-52 parte Telegram, D-03 e D-27 per Telegram, CLI-32 (frase del popup con `{telegram_optin}`). **Parziali:**
  ADM-10 (l'errore del lettore si legge e si mostra, il lettore è della 073), CLI-51 (il 403 spegne le iscrizioni
  già importate; iscriverle è della 072).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** `formato` e `tastiera`
  ci sono già in `messaggi_in_uscita` (0008) e l'import li riempie (`parse_mode`, `reply_markup`); token e chat
  stanno in `segreti_provider` e `impostazioni` come li porta l'import.

## Cosa c'è

- **Provider** (`internal/uscita/telegram.go`, `uscita.Telegram`) = `Outgoing.Providers.Telegram`: `sendMessage`
  (`chat_id`, `text`, e `parse_mode`/`reply_markup` solo se la riga li porta) e `getMe` (`ChiediNome`). Il riferimento
  è il `message_id`. Rifiuti con le parole di Telegram dopo lo stato (`telegram:http:400:Bad Request: chat not
  found`), come lo standalone, perché la coda le legge. Il token sta nel percorso dell'indirizzo: mai l'errore di
  `net/http`, solo `telegram:transport:<genere>` (i generi dell'SMTP), e ogni testo che torna passa dalla pulizia
  del token. 15 s per richiesta, nessun redirect seguito, al più 1 MiB di risposta (solo Go).
- **Indirizzo del Bot API** solo da `TELEGRAM_API_ENDPOINT` (`EndpointDaAmbiente`, letto da `serve` prima del
  database): assente è Telegram; uno sbagliato ferma l'avvio (come `runtime.exs`); solo Go niente utente, query o
  frammento. `gestrip serve` registra il provider Telegram accanto all'SMTP.
- **Configurazione** (`config_telegram.go`): `LeggiTelegram`, `Usabile` (= `usable?/1` più una seconda serratura solo
  Go: token e chat di forma esatta, perché un token importato con «/» o «?» cambierebbe l'indirizzo),
  `TelegramAttivo`, `NotificheClientiAccese`. Il token è una `Password` (non si stampa).
- **Interruttore dell'invio Telegram (D-03, solo Go):** `canale_telegram_acceso`, in `importer.ChiaviSoloGo`. Spento:
  `Accoda` rifiuta, il giro non legge le righe Telegram (le importate restano com'erano). Le righe Telegram importate
  che prima «aspettavano la T4.7» ora partono con il provider vero appena il titolare accende l'invio (dopo la
  revisione D-27).
- **Regole della coda** (`coda.go`) = `record/2` e `dispatch_once/1`: **429** (stato intero) = tentativo, attesa
  `max(gradino, retry after N)` (al più un giorno, senza numero 10 s) e le **altre righe Telegram del giro trattenute**
  fino ad adesso + N senza tentativo (le email no; anche al quinto rifiuto); **400** = fallita subito; **403 sulla
  riga di un cliente** = fallita subito e, nella stessa transazione, spente le iscrizioni attive della chat con
  `repair.telegram_stop` (via `blocked`, attore `telegram`) = `block_chat/1`; un 403 sul negozio riprova; **notifiche
  ai clienti spente** = la riga del cliente fallisce senza richiesta (`telegram:notifiche ai clienti spente`).
- **Formato e tastiera:** `Messaggio.Formato`/`Tastiera` (letti dalla riga), `Contenuto.Formato` (`FormatoHTML`) e
  `Contenuto.Tastiera` (`*PulsanteURL`, scritto `{"inline_keyboard":[[{"text":…,"url":…}]]}` senza escape HTML, come
  Jason). Solo Telegram e solo HTML, altrimenti `ErrMessaggioNonValido` (`Message.changeset/2`).
- **Pagina** (`pagina_telegram.go`, `web/provider.go`, `pagine/provider.html`): riquadro `#telegram-card` fra la posta
  e la coda, come lo standalone: modulo token (mai rimostrato) e chat con l'aiuto `#telegram-chat-help`, «Cancella il
  token salvato», regole di `Telegram.changeset/2` (solo i campi cambiati, PCRE senza «u»), un token nuovo dimentica
  lo stato del bot (`forget_bot_state/2`); «✈ Invia messaggio di prova» (getMe, nome salvato e mostrato «Bot: @…»,
  poi il messaggio di prova alla chat del negozio **subito, senza coda, anche a invio spento**, come la posta della
  069c; audit `outgoing.test_sent`); «Notifiche Telegram ai clienti» (`set_enabled/2`, audit
  `outgoing.telegram_customer_notifications`) con «Il bot non è configurato…», le chat sconosciute e l'**ultimo errore
  del lettore** letto da `telegram_inbound.last_error`/`last_error_at` (per un 409 la frase sul webhook); solo Go
  «Invio Telegram» (`#telegram-switch`) con i messaggi Telegram rimasti in coda da mandare o scartare uno per uno
  (`#telegram-pending`), con le rotte `sospese/{id}` della 069b ora per i due canali.
- **Popup senza `indirizzo_pubblico`:** «Togli {telegram_optin} dal testo…» o «Togli {link} e {telegram_optin} dal
  testo…» quando il testo li contiene (`linkMancante(testo)`; Dubbio 7 della 069c).
- **Banco:** `bench/scenari/provider.json` confronta anche il riquadro Telegram (16 campi);
  `bench/browser/provider-telegram.json` (`providerTelegram` in `banco.mjs`): stato, notifiche accese e spente senza
  bot, rifiuto del modulo, salvataggio con un token finto, cancellazione, ripristino, prova senza configurazione,
  conteggi del registro. Mai una richiesta al Bot API (sotto).

## Scelte

- **Un interruttore del canale in più (D-03).** La D-03 dice «email e Telegram spenti finché il titolare non li
  accende». Lo standalone ha solo «Notifiche Telegram ai clienti», che non ferma le righe del **negozio**: con il bot
  importato e configurato partirebbero al primo giro (e con loro le righe del negozio rimaste in coda, contro la
  D-27). Quindi, come per l'email, un interruttore solo Go «Invio Telegram» con la revisione dei messaggi in coda;
  «Notifiche Telegram ai clienti» resta com'è nello standalone: spenta su un'installazione nuova, ma **l'import porta
  `outgoing.telegram.customer_notifications` così com'è** (se nello standalone era accesa, nel Go resta accesa); con
  «Invio Telegram» spento non fa danni (nessuna riga parte, nessun lettore oggi). Dubbio 1.
- **Prova subito, senza coda e anche a invio spento,** come la decisione di Stefano dell'11/11 per la posta (lo
  standalone la accoda). A invio spento la coda la rifiuterebbe, e la prova serve proprio prima di accendere. Le frasi
  dei rifiuti restano quelle dello standalone («… niente è stato accodato.»).
- **D-27 per Telegram come per l'email:** l'accensione si blocca con **un qualunque** messaggio Telegram in coda (stessa
  ragione della 069b: l'import non segna le righe).

## Prove

`uscita`: `telegram_test.go` (7: consegna con e senza formato, rifiuti e pulizia del token, errori di trasporto,
tempo massimo e risposta enorme, spento/notifiche/seconda serratura, getMe, endpoint, configurazione),
`coda_telegram_test.go` (9: 429 con retry after, attese del 429 in 5 casi, stato intero («4291», «4001») con un
provider finto, 429 solo Telegram e riga uscita dalla coda,
400, 403 con le iscrizioni, notifiche ai clienti spente, formato e tastiera con righe importate, D-03),
`pagina_telegram_test.go` (6: salva, rifiuti, pagina, accensione con revisione, notifiche, prova). `web`:
`provider_telegram_test.go` (6), `TestPopupSenzaIndirizzoPubblicoD46` (frase nuova), le rotte nuove in
`tabellaPermessi` e `TestRotteConGuscio`. `cmd/gestrip`: `TestServeTelegram`, `TestServeTelegramEndpointSbagliato`.
`importer`: `TestChiaviSoloGoComplete`. Tutto contro un Bot API finto in processo (`httptest`); mai Telegram, mai un
token vero.

**Rosso preso prima del codice** (con gli stub): 21 test rossi in `uscita`, 7 in `web` (compreso il popup), i 2 di
`cmd/gestrip` (uno non arriva al Bot API in 15 s, l'altro si blocca: `serve` partiva con l'endpoint sbagliato).
Test esistenti ritoccati: `TestPaginaProvider` (non pretende più l'assenza del Telegram), le tre chiamate a
`ScartaInSospeso` (ora dà anche il canale), `salva` (dà anche il momento del 429).

## Mutazioni

Script `to-be-deleted-after-use/slice-071/mutazioni/muta.py` su una copia del repo, test del pacchetto toccato.
42 mutazioni; nella forma finale compilano tutte (5 riscritte perché la prima forma non compilava: M10, M22, M33,
W1, e W2 rifatta con il filtro dei test giusto). **41 uccise al primo giro, 1 data per equivalente (M18) a torto:** la review ha mostrato che non lo era (sotto, R6). Due sopravvissute al primo
giro (M3, M8: «4291» e «4001» letti come 429 e 400) hanno portato `TestTelegramStatoIntero`: un server HTTP non
può mandare uno stato di quattro cifre (il server finto va in panico e il client vede un errore di trasporto), quindi
i casi dello standalone si provano con un provider finto che dichiara il motivo.

| # | Mutazione | Esito |
|---|---|---|
| M1 | 429 con più di 6 cifre: prima attesa invece di un giorno | uccisa (`TestTelegram429Attese`) |
| M2 | 429 senza numero: nessuna attesa | uccisa (idem) |
| M3 | «telegram:http:4291» letto come 429 | uccisa (`TestTelegramStatoIntero`) |
| M4 | il 429 trattiene anche le email | uccisa (`TestTelegram429SoloTelegram`) |
| M5 | il 429 non trattiene niente | uccisa (`…RetryAfter`, `…Attese`, `…SoloTelegram`) |
| M6 | la riga del 429 aspetta solo il gradino | uccisa (`…RetryAfter`, `…Attese`) |
| M7 | il 400 non fa fallire | uccisa (`TestTelegram400`) |
| M8 | «telegram:http:4001» letto come 400 | uccisa (`TestTelegramStatoIntero`) |
| M9 | 403 del negozio trattato come blocco | uccisa (`TestTelegram403`) |
| M10 | 403 del cliente senza spegnere le iscrizioni | uccisa (idem) |
| M11 | motivo dello spegnimento «stop» invece di «blocked» | uccisa (idem) |
| M12 | notifiche ai clienti spente: la riga riprova | uccisa (`TestTelegramClientiSpentiFallisce`) |
| M13 | il provider ignora le notifiche ai clienti | uccisa (idem, `TestTelegramSpentoNonApre`) |
| M14 | il provider ignora l'interruttore D-03 | uccisa (`TestTelegramSpentoNonApre`) |
| M15 | seconda serratura del token tolta | uccisa (idem) |
| M16 | token non pulito dai motivi | uccisa (`TestTelegramRifiuti`) |
| M17 | redirect seguiti | uccisa (idem) |
| M18 | risposta oltre 1 MiB non scartata | sopravvissuta: **non equivalente** (la review, R6: senza `LimitReader` il vecchio test passava lo stesso) → `TestTelegramRispostaSenzaFine`, ora la lettura senza limite è uccisa |
| M19 | endpoint con query accettato | uccisa (`TestEndpointDaAmbiente`) |
| M20 | endpoint vuoto = Telegram | uccisa (idem) |
| M21 | token nuovo senza dimenticare lo stato del bot | uccisa (`TestSalvaTelegram`) |
| M22 | formato del token non controllato | uccisa (`TestSalvaTelegramRifiuti`) |
| M23 | a capo finale della chat rifiutato (PCRE senza «u») | uccisa (idem) |
| M24 | «Cancella il token» non lo toglie | uccisa (`TestSalvaTelegram`) |
| M25 | D-27 Telegram conta le email | uccisa (`TestAccensioneTelegramConRevisione`) |
| M26 | getMe rifiutato non ferma la prova | uccisa (`TestProvaTelegram`) |
| M27 | audit della prova sempre «accepted» | uccisa (idem) |
| M28 | formato qualunque accettato | uccisa (`TestTelegramFormatoETastiera`) |
| M29 | formato e tastiera su un'email | uccisa (idem) |
| M30 | riga «in sospeso» anche senza il provider del suo canale | uccisa (tre test della revisione) |
| M31 | notifiche accese scritte «true» | uccisa (`TestNotificheClienti`) |
| M32 | notifiche accese con qualunque valore | uccisa (`TestConfigTelegram`) |
| M33 | tastiera non scritta sulla riga | uccisa (`TestTelegramFormatoETastiera`) |
| M34 | parse_mode non mandato | uccisa (idem, `TestTelegramConsegna`) |
| W1 | frase del popup senza «{link} e {telegram_optin}» | uccisa (`TestPopupSenzaIndirizzoPubblicoD46`) |
| W2 | «enabled=false» accende le notifiche | uccisa (`TestNotificheClientiEErroreLettore`) |
| W3 | getMe rifiutato detto come prova riuscita | uccisa (`TestProvaProviderTelegram`) |
| W4 | segnaposto del token anche senza token | uccisa (`TestPaginaProviderTelegram`, `TestSalvaProviderTelegram`) |
| W5 | scarto Telegram con la frase dell'email | uccisa (`TestAccensioneTelegram`) |
| W6 | rifiuto senza la chat mandata | uccisa (`TestSalvaProviderTelegram`) |
| C1 | `serve` senza il provider Telegram | uccisa (`TestServeTelegram`) |
| C2 | endpoint sbagliato non ferma l'avvio | uccisa (`TestServeTelegramEndpointSbagliato`, tempo scaduto) |

## Banco

**Il giro valido lo fa la sessione principale, su un'istanza sua (`s071c`):** livello HTTP verde (64 scenari, 1120 passi, 0 inattese); livello browser rosso (81 inattese, 2 errori) per lo scenario nuovo, corretto come scritto qui sotto. **Giro valido finale della sessione principale su un'istanza nuova (`s071f`), dopo le correzioni della review e dello scenario: BANCO VERDE, 64 scenari, 1120 passi, 0 inattese, 0 errori; BANCO BROWSER VERDE, 17 scenari, 34 viste, 0 inattese, 0 errori.** Il «database is locked» dello standalone visto su `s071e` (`GET /audit`, `salva-avverti` al telefono) non si è ripetuto: è il difetto saltuario dello standalone già visto (Dubbio 12 della 056), non legato alla 071 (la prova Telegram nello standalone si ferma a `getMe` e non accoda niente).

Correzioni dello scenario dopo `s071c` (browser rosso lì): nello standalone `provider-telegram` aspettava il messaggio
dell'interruttore delle notifiche, che resta sulla pagina, quindi leggeva lo stato vecchio e il clic dopo le
riaccendeva; lo standalone restava con le notifiche accese e gli scenari dopo cadevano. Ora lo scenario aspetta che
cambi lo stato del riquadro, porta le notifiche a «Spente» qualunque sia lo stato di partenza (l'import le porta com'erano), e alla
fine (anche dopo un errore) toglie il token finto e rimette chat e notifiche di partenza; lo stato del riquadro si
legge dallo span dopo il titolo «Telegram» (nel Go la pagina sta in un'altra `<section>`). Corretta anche una corsa
di `provider-posta` (069b): nello standalone il secondo rifiuto leggeva gli errori del primo, ancora sulla pagina.
Giri: `s071d` HTTP verde (64 scenari, 1120 passi, 0 inattese), browser 2 inattese, la corsa di `provider-posta`
(`provider-telegram` tutto uguale); `s071e` (con la corsa corretta) HTTP verde, browser 41 inattese e 1 errore, tutte di
`salva-avverti` sul telefono nello standalone: `500 GET /audit` per «database is locked» (il lucchetto SQLite dello
standalone tenuto 5 s mentre la pagina del registro si apriva due volte di fila), da capire.

Giri dell'implementatore, non validi: `s071` (HTTP verde, 64 scenari, 1120 passi, 0 inattese; browser rosso per
un errore dello scenario nuovo, `#telegram-card` che c'è solo nel Go, corretto, e per un «database is locked» dello
standalone in `testi-invii` con le mutazioni in parallelo) e `s071b` (HTTP di nuovo verde; il livello browser fermato
da Claude Code per la memoria di pc-fisso quasi finita, non rilanciato). `banco giu` fatto per tutte e due.

Nessuna richiesta al Bot API vero: il Go ha l'invio Telegram spento (D-03) e lo standalone non ha mai bot configurato
e notifiche accese insieme (lo scenario si ferma se trova Telegram già configurato, spegne le notifiche prima di
salvare il token finto e lo cancella subito).

**Seconda istanza Go con i canali accesi:** non fatta (Dubbio 5).

## Controlli

`scripts/check` verde a metà slice (dopo il codice e i test, prima delle mutazioni): copertura dell'inventario
«voci 353: chiuse 36, rosse 0, parziali 126, aperte 180, fuori v1 11», segreti e dati veri OK. Dopo, con pc-fisso
a corto di memoria, solo una verifica leggera dei pacchetti toccati (gofmt, vet, `go test -p 1` di `uscita`,
`importer`, `tools/banco`, i test Telegram/provider/popup/rotte di `web`, i `TestServe*` di `cmd/gestrip`): verde. Le
modifiche dopo il `check` sono il test `TestTelegramStatoIntero`, il `Location` del Bot API finto, un nome di campo
(`errAcceso`), la classe del riquadro dell'errore del lettore, lo scenario browser e le correzioni della review.
Dopo la review: `go test -p 2 ./internal/uscita ./internal/web ./cmd/gestrip` verde. **`scripts/check` va
rilanciato** dalla sessione principale: fatto due volte, l'ultima sul codice finale (dopo le correzioni della review e
dello scenario), **verde**, «voci 353: chiuse 36, rosse 0, parziali 126, aperte 180, fuori v1 11», segreti e dati veri OK.

## Per il manuale

- In «Impostazioni → Provider» c'è il riquadro «Telegram»: token del bot (non si rivede mai) e chat del negozio,
  «Configurato» o «Non configurato».
- «✈ Invia messaggio di prova» chiede al bot il suo nome e manda subito un messaggio alla chat del negozio, anche con
  l'invio Telegram spento; la pagina mostra «Bot: @nome» o perché il bot non risponde.
- «Notifiche Telegram ai clienti» si accendono e spengono lì; spente, i messaggi per i clienti non partono. Se il bot
  non riesce a leggere i messaggi (per esempio un 409), la pagina lo dice con l'ora.
- «Invio Telegram» è spento dopo l'import: si accende solo dopo aver mandato o scartato uno per uno i messaggi
  Telegram rimasti in coda.
- Nel riquadro dell'avviso senza indirizzo pubblico la frase dice di togliere anche {telegram_optin} quando il testo
  lo contiene.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Due interruttori Telegram** («Invio Telegram», solo Go per la D-03, e «Notifiche Telegram ai clienti» dello
   standalone). Alternativa: uno solo, ma cambierebbe il significato di quello dello standalone (fermerebbe anche il
   negozio) o lascerebbe partire le righe del negozio importate. **Raccomandazione:** tenerli tutti e due. **Vincolo per la 073:**
   dopo l'import le notifiche possono essere già accese (l'import porta la chiave com'è), quindi il lettore
   `getUpdates` deve leggere solo con tutti e due accesi, altrimenti partirebbe col bot importato prima che il titolare
   accenda «Invio Telegram» (lo standalone legge con le sole notifiche accese).
2. **Prova Telegram subito, senza coda, a invio spento** (lo standalone la accoda), come la posta della 069c. Audit
   `outgoing.test_sent` invece di `outgoing.queued`. **Raccomandazione:** tenerla così.
3. **Descrizione di Telegram nell'errore salvato** (`telegram:http:400:Bad Request: chat not found`), come lo
   standalone: la coda la legge (429, 403) e la pagina la mostra. A differenza dell'SMTP (solo il codice), qui il testo
   del server resta, ripulito dal token e tagliato a 500. **Raccomandazione:** tenerla (Telegram non ripete dati del
   cliente, e senza la descrizione il «retry after» non si legge).
4. **«Notifiche Telegram ai clienti» scrive anche a stato invariato** (un audit in più a ogni clic), come lo standalone.
   **Raccomandazione:** lasciarlo.
5. **Banco con i canali accesi** (ok di Stefano «se costa poco»): non fatto qui. Una seconda istanza Go nel banco vuol
   dire un secondo container, una seconda base dati sintetica e il confronto a tre (`istanza.go`, `esegui.go`,
   `confronto.go`): costa più di questa slice, e oggi non ci sarebbe niente da confrontare (lo standalone del banco non
   ha un Bot API finto). **Raccomandazione:** farlo con la 074 (avvisi ai clienti), con un Bot API finto in un
   container della rete interna per tutte e due le app (`TELEGRAM_API_ENDPOINT`), così si confrontano anche le
   richieste.
6. **Reset della password via Telegram (ACC-13):** il provider è pronto (`ConsegnaAMano` alla chat del negozio), ma
   `accounts/recupero.go` dice ancora «non configurato». **Raccomandazione:** una slice piccola dopo la T4.7, insieme
   all'email del reset.
7. **Testi solo del Go** (interruttore, revisione, esiti della prova): scritti dall'implementatore. **Raccomandazione:**
   rivederli con le schermate del manuale, come gli altri.

## Review

Revisore indipendente, 13/11/2026 (pc-fisso, slice non committata). Nessuna ALTA. Test nuovi con il rosso preso
prima (`slice-071/prove/rosso-review.log`): per i punti 5 e 6 sul codice di allora (con due stub), per i punti 1-4,
che provano un comportamento già giusto, contro la mutazione del revisore su una copia.

- **MEDIA, corretto (test):** il limite di 1 MiB sulla lettura non era provato (M18 data per equivalente a torto).
  `TestTelegramRispostaSenzaFine`: un Bot API finto che scrive senza fine; la richiesta torna in pochi secondi come
  «risposta non riconosciuta» e il server si ferma con un errore di scrittura. Senza `LimitReader`: tempo scaduto
  dopo 25 s e circa 11 GB scritti.
- **BASSA, corretto (test):** `bloccaChat` senza `fermata_il IS NULL` non faceva cadere niente. `TestTelegram403` ha
  un'iscrizione della stessa chat già fermata (`stop`, un'ora prima): resta com'era e senza audit.
- **BASSA, corretto (test):** il tetto di un giorno del 429 fra 2 e 6 cifre: caso «retry after 100000» = 24 h in
  `TestTelegram429Attese`.
- **BASSA, corretto (test):** `TestProviderOgniModuloConCSRF` legge ogni `<form method="post">` di
  `/settings/providers` in tre stati della pagina (vuota, con messaggi in coda da guardare, con i due invii e le
  notifiche accesi) e pretende il `_csrf_token`.
- **BASSA, corretto:** la riga «acceso ma non configurato» anche per Telegram (`#telegram-switch-unusable`: «L'invio
  Telegram è acceso ma Telegram non è configurato: nessun messaggio Telegram parte finché non completi token e chat qui
  sopra.»), come la posta della 069c; `TestTelegramAccesoMaNonConfigurato`.
- **BASSA, corretto:** il provider usava `http.DefaultTransport`, che segue `HTTP(S)_PROXY` (un proxy vedrebbe il token
  nel percorso): ora `trasportoTelegram`, clonato dal predefinito con `Proxy` nil. E `Telegram.Segreti()` dà il token
  salvato, così anche `pulisci` della coda lo toglie (seconda mano, come per l'SMTP). `TestTelegramSenzaProxyESegreti`
  (il test controlla il campo `Proxy`: con un server su 127.0.0.1 il proxy dell'ambiente non si vedrebbe, Go non lo usa
  per il loopback).
- **BASSA, corretto (ricevuta):** «Notifiche Telegram ai clienti» non è spenta dopo l'import: l'import porta la chiave
  com'è. Frase corretta in «Scelte» e vincolo per la 073 nel Dubbio 1.

**Mutazioni della review dopo le correzioni** (`slice-071/mutazioni/muta-review.py`, `esiti-review.log`, verificate una
per una sul codice finale):

| # | Mutazione | Esito |
|---|---|---|
| R6 | lettura della risposta senza `LimitReader` | uccisa (`TestTelegramRispostaSenzaFine`) |
| R2 | `bloccaChat` spegne anche le iscrizioni già fermate | uccisa (`TestTelegram403`) |
| R22 | 429 senza il tetto di un giorno fra 2 e 6 cifre | uccisa (`TestTelegram429Attese`, «un giorno da 6 cifre») |
| R15 | `#telegram-switch-form` senza `_csrf_token` | uccisa (`TestProviderOgniModuloConCSRF`) |
