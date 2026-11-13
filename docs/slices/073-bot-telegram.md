# 073 — Bot Telegram: lettore `getUpdates` e comandi del cliente (T4.7, terza slice)

- **Fonte:** brief del coordinatore del 13/11/2026, ripresa `2026-11-13-fine-072.md`. Lo standalone a `go-baseline` =
  `302439d`: `outgoing/telegram_inbound.ex` (`poll_once/0`, `handle_all/3`, `attempt/4`, `failed/2`, `handle/3`,
  `command/4`, `start/4`, `stop/3`, `privacy/3`, `press/4`, `reply/4`, `answer_unknown/3`, `send_reply/4`,
  `shop_chat?/2`, `record_error/1`, `clear_error/0`), `outgoing/telegram_poller.ex` (`next_delay/3`, `run/1`),
  `outgoing/telegram_inbound/reply_limit.ex`, `outgoing/providers/telegram.ex` (`get_updates/2`, `send_text/4`,
  `answer_callback/2`), `telegram_subscriptions.ex` (`start/2`, `consent/3`, `stop_chat/1`, `subscribe/3`,
  `switch_off/4`), `online_sales.ex` (`shop_name/0`); i test `telegram_inbound_test.exs`,
  `telegram_inbound_customer_test.exs`, `telegram_poller_test.exs` (letti, non fatti girare). Inventario: CLI-46.
- **Voci:** CLI-46 (lettore e comandi del bot). Tocca CLI-44 (il riquadro passa ad «attive» dopo il «sì» nel bot:
  provato nel giro completo) e ADM-10 (la pagina dei provider della 071 mostra l'ultimo errore e le chat sconosciute
  che il lettore ora scrive).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** le chiavi
  `telegram_inbound.*` sono in `config_telegram.go` dalla 071, `iscrizioni_telegram` e `token_telegram` dalla 0008.

## Cosa c'è

- **Lettore** (`internal/uscita/lettore_telegram.go`, `LettoreTelegram`): `Leggi` = `poll_once/0`, `Lavoro` =
  `TelegramPoller`, `ProssimaAttesa` = `next_delay/3`, `limiteRisposte` = `ReplyLimit`. Interfaccia
  `IscrizioniTelegram` (avvio, consenso, stop della chat, nome del negozio) con i due rifiuti
  `ErrLinkTelegramNonValido` e `ErrIscrizioneTelegramNonTrovata`.
- **Iscrizioni** (`internal/buste/telegram_bot.go`): `AvviaTelegram` (= `start/2`), `ConsensoTelegram` (= `consent/3`),
  `FermaChatTelegram` (= `stop_chat/1`), `NomeNegozio` (= `shop_name/0`).
- **Trasporto** (`internal/uscita/telegram.go`): `richiestaPer`, la porta della 071 con il tempo e il limite di lettura
  dati: la lettura lunga ha 35 s (`@poll_timeout`) e al più 8 MiB (solo Go; le altre richieste restano 15 s e 1 MiB).
  `scriviImpostazione` (`pagina_smtp.go`) prende ora una transazione o il collegamento di scrittura.
- **`gestrip serve`** (`cmd/gestrip/main.go`): avvia il lettore fra i lavori quando il provider Telegram è quello vero,
  con `aspettaPer` e il registro `lavoro=lettore_telegram`; all'arresto lo ferma e lo aspetta come gli altri.
- **Vincolo di Stefano (Dubbio 1 della 071):** una lettura parte solo con «Invio Telegram» (D-03) **e** «Notifiche
  Telegram ai clienti» accese e il bot `Usabile`; altrimenti è inattiva, senza richieste, e si riguarda dopo 30 s.
- **Lettura:** `getUpdates` con `allowed_updates` `["message","callback_query"]`, `timeout` 25 e l'offset se c'è;
  offset salvato dopo ogni aggiornamento; aggiornamento che fallisce (errore o panico) ritentato alla lettura dopo e
  saltato al terzo (`failing_update` `id:volte`); 409 come `409: <descrizione>` nell'ultimo errore (con il token pulito),
  mai `deleteWebhook`; altri errori `telegram:http:<stato>`, `telegram:risposta non riconosciuta`,
  `telegram:transport:<genere>`, `telegram:update <id> non gestito`; una lettura riuscita pulisce l'errore (scrive solo
  se c'era). Attese 0 / 1 s / 30 s / 5 s → 15 s → 1 min → 5 min.
- **Comandi** (solo chat private, fuori la chat del negozio, gli altri bot, gruppi e canali): `/start <token>` con il
  benvenuto HTML e i due pulsanti dello standalone, `/stop` e `/disattiva`, `/privacy`, i pulsanti `cli:tx:<0|1>:<id>`
  (prima `answerCallbackQuery`), la risposta fissa con il contatore `unknown_chats`. Testi dello standalone.
- **Limiti in memoria:** fissa 1 ogni 10 minuti per chat e 20 al minuto in tutto; le altre 8 al minuto per chat e 30 al
  minuto in tutto; pulsante oltre il limite lasciato cadere tutto.
- **Audit:** `repair.telegram_linked` (busta, iscrizione), `repair.telegram_consent` (busta, iscrizione, consenso),
  `repair.telegram_stop` (busta, iscrizione, via `bot`), attore `telegram`. Mai chat id, token o testi nel registro,
  nelle impostazioni o nell'audit.

## Scelte

- **Le risposte del bot partono subito, non dalla coda**, come lo standalone (`send_text/4`: «Nothing about it is
  stored»): una risposta è dovuta adesso alla chat che ha scritto, e una riga in coda porterebbe la chat in
  `messaggi_in_uscita`. Una risposta rifiutata va nel registro (solo il motivo) e si lascia.
- **Il vincolo dei due interruttori** è nel lettore stesso (`attivo`), riletto a ogni giro: spegnere uno dei due ferma
  la lettura dopo, senza riavvio. L'invio Telegram si legge con `telegramAcceso` (lo stesso della coda).
- **Prima lettura subito all'avvio** (lo standalone aspetta prima i 30 s dell'intervallo da inattivo): dopo un riavvio
  il bot risponde senza ritardo. Con il canale spento non cambia niente (nessuna richiesta).
- **Arresto:** la lettura lunga usa il contesto di `serve`, quindi si chiude subito; un errore dovuto alla chiusura non
  si salva come ultimo errore. Un aggiornamento cominciato si finisce con un contesto senza annullamento (al più due
  richieste di 15 s), perché un `/start` gestito a metà e rigestito risponderebbe «link non valido»; fra un
  aggiornamento e l'altro si guarda l'arresto.
- **`/privacy` senza indirizzo pubblico** (solo Go): lo standalone ha sempre l'indirizzo del suo endpoint; qui senza
  `indirizzo_pubblico` non c'è un indirizzo da dare e la chat riceve la risposta fissa. Dubbio 2.
- **Limite di lettura di `getUpdates` 8 MiB** invece di 1 MiB: 100 aggiornamenti con testi lunghi possono passare il MiB,
  e una risposta tagliata sarebbe «non riconosciuta» per sempre (il lettore non andrebbe più avanti).
- **Iscrizioni dentro `buste`**, il lettore in `uscita`: `buste` importa già `uscita`, quindi il lettore riceve le
  iscrizioni come interfaccia (`IscrizioniTelegram`), e `serve` gli passa le `Buste`.
- **Risposte a una notifica (CLI-49, 075):** il punto d'aggancio è in `LettoreTelegram.messaggio`, con un commento. Fino
  alla 075 un messaggio con `reply_to_message` si comporta come nello standalone quando la notifica non è di nessuna
  busta: un comando resta il comando, il testo libero riceve la risposta fissa (`TestLettoreRispostaANotificaPrimaDella075`).
- **Un errore del database sul pulsante del consenso fa ritentare l'aggiornamento** (fino a tre volte, poi si salta);
  lo standalone in quel caso risponde «Iscrizione non trovata» (punto 8 della review: la scelta del Go è migliore,
  perché non dice al cliente una cosa falsa e il pulsante non ha effetti a metà).
- **Come lo standalone (note della review):** se il titolare cambia il token del bot durante una lettura lunga, la
  lettura in corso finisce con il token di prima (l'offset che scrive dopo è del bot vecchio, e il salvataggio del
  token l'ha appena cancellato: al più un giro con un offset sbagliato); se il salvataggio dell'offset fallisce dopo
  un aggiornamento gestito, la lettura dopo lo rigestisce (un `/start` risponderebbe «link non valido»); un `/start`
  oltre il limite di 8 risposte al minuto per chat consuma il token e iscrive la chat, ma il benvenuto con i pulsanti
  non parte (il cliente rigenera il link dalla pagina).
- **Import:** `internal/importer` porta già `telegram_inbound.*` con le altre impostazioni (`settings` → `impostazioni`
  tranne le chiavi segrete e quelle solo Go); ora lo prova `TestImportCompletoRiconciliato` (offset e
  `failing_update` del database sintetico). È il comportamento giusto: il token del bot arriva con l'import, quindi
  l'offset è dello stesso bot e il Go riprende da dove lo standalone si era fermato, senza rigestire aggiornamenti già
  gestiti. Due avvertenze nei Dubbi (3 e 4).

## Prove

`uscita` (`lettore_telegram_test.go`, 20, con il Bot API finto della 071 e iscrizioni finte):
`TestLettoreSoloConIDueInterruttori` (notifiche spente o «true», invio spento o senza riga, bot senza token o con token
di forma sbagliata: inattivo e zero richieste; acceso: una lettura; rispento: di nuovo inattivo),
`TestLettoreRispostaFissa` (corpo esatto con `chat_id` numerico, offset 12, contatore 2, la pagina della 071 lo
mostra), `TestLettoreChiedeDallOffset` (corpo esatto con e senza offset), `TestLettoreOffsetDopoOgniAggiornamento`,
`TestLettoreAggiornamentoGiaLetto`, `TestLettoreFuoriDalFlusso` (gruppo, supergruppo, canale, `channel_post`, pulsante
in un gruppo, altro bot, pulsante di un bot, chat del negozio per numero e per `@nome`), `TestLettoreAggiornamentiStrani`
(risposta rifiutata 403, forme sbagliate contate, `update_id` non intero non contato), `TestLettore409` (conflitto,
token pulito, ora, offset fermo, una sola richiesta, la pagina lo mostra; 409 senza descrizione), `TestLettoreAltriErrori`
(502, `ok: false`, `result` non lista, 302; la lettura riuscita pulisce), `TestLettoreAggiornamentoCheFallisce` (3
tentativi, registro senza chat né messaggio d'errore; un panico con il token di `/start` nel messaggio),
`TestLettoreConteggioDegliErrori`, `TestLettoreStart` (corpo e tastiera esatti, `/START@bot` con spazi e tab, token con
spazio, nome del negozio protetto, link non valido, `/start` senza token, `/starttok`, foto senza testo),
`TestLettoreStopEPrivacy`, `TestLettorePrivacySenzaIndirizzo`, `TestLettorePulsanti` (risposta al pulsante poi conferma,
`+41`, iscrizione non trovata, sei pulsanti sconosciuti solo risposti), `TestLettoreLimiti` (tutti e quattro i limiti con
i loro confini: 9:59 no e 10:00 sì, 59 s no e 60 s sì; pulsanti oltre il limite; nessuna chat nel database),
`TestLettoreNienteDatiNeiRegistri`, `TestLettoreRispostaANotificaPrimaDella075`, `TestProssimaAttesa`,
`TestLavoroDelLettore` (inattivo, 409 crescente, con aggiornamenti, vuota, panico nelle iscrizioni).
`buste` (`telegram_bot_test.go`, 7): `TestAvviaTelegram` (riga esatta, token tolto, audit esatto senza chat né token,
secondo uso da due chat), `TestAvviaTelegramRifiuti` (vuoto, 65 caratteri anche se nel database, 64 accettato, ignoto, scaduto alla scadenza
esatta e valido un microsecondo prima, conto terzi, chiusa, cestino; token consumati; niente scritto), `TestAvviaTelegramDiNuovo`,
`TestConsensoTelegram`, `TestFermaChatTelegram`, `TestNomeNegozioDelBot`, `TestBotTelegramDallaPagina` (il giro
completo con il lettore vero: pulsante della pagina → `/start` → «Sì» → riquadro «attive» → `/stop` → proposta → token
già usato).
`cmd/gestrip` (`telegram_serve_test.go`, 3): `TestServeLettoreTelegram` (getUpdates all'avvio sul Bot API finto di
`TELEGRAM_API_ENDPOINT`, arresto in meno di 5 s con la lettura lunga aperta, nessun errore salvato),
`TestServeLettoreTelegramSpento` (notifiche spente o invio spento: nessuna getUpdates), `TestServeAspettaIlLettore`
(arresto mentre il lettore risponde: la risposta parte e l'offset si salva prima che `serve` finisca).
`importer`: due righe `telegram_inbound.*` nel database sintetico e i due controlli in
`TestImportCompletoRiconciliato`; `TestColonnaDeiRifiuti` aggiornato al nuovo rowid (9 invece di 7).

**Rosso** (`slice-073/prove/`): test scritti prima del codice, con scheletri che compilano e non fanno niente.
`rosso-uscita.log`: **20 rossi su 20**, ognuno su un'asserzione di comportamento (esito della lettura, richieste al
Bot API, offset, risposte). `rosso-buste.log`: **7 rossi su 7** (rifiuti non dati, iscrizione non creata: la riga
cercata non c'è, nome del negozio vuoto). `rosso-cmd.log`: `TestServeLettoreTelegram` rosso («il lettore non ha chiesto
getUpdates»); `TestServeLettoreTelegramSpento` verde anche sullo scheletro, com'è naturale per un test del «non fa
niente» (lo tiene vero la mutazione U1/U2 sul lettore). Al primo verde un solo test rosso per un errore del test
stesso (`<` scritto come «<» nel confronto), corretto.

## Mutazioni

Script `to-be-deleted-after-use/slice-073/mutazioni/muta.py` su una copia del repo (cancellata a fine lavoro), una
mutazione alla volta, test del pacchetto toccato (`uscita` con `-run 'Lettore|ProssimaAttesa|LavoroDel'`, `buste` con i
sette test del bot, `cmd/gestrip` con `-run 'ServeLettore|ServeAspettaIlLettore'`). **48 mutazioni che compilano: 48
uccise, 0 sopravvissute** (due solo dopo un test nuovo, sotto). Due prime forme non compilavano (U19, U31: variabile non
usata), riscritte come U19b e U31b. Esiti in `giro.log`, `giro2.log`, `esiti-giro1.json`, `esiti-giro2.json`.

Al primo giro sopravvivevano **B12** (nessun token di 65 caratteri era nel database, quindi il limite non si vedeva:
`TestAvviaTelegramRifiuti` ora scrive un token di 65 caratteri, rifiutato, e uno di 64, accettato) e **M2** (`serve` che
non aspetta il lettore: `TestServeAspettaIlLettore` ferma `serve` mentre il lettore sta rispondendo e controlla che la
risposta sia partita e l'offset salvato prima che `serve` finisca). Rifatte con i test nuovi: uccise.

| # | Mutazione | Esito |
|---|---|---|
| U1 | notifiche ai clienti ignorate | uccisa (`TestLettoreSoloConIDueInterruttori`, `TestLavoroDelLettore`) |
| U2 | «Invio Telegram» (D-03) ignorato | uccisa (`TestLettoreSoloConIDueInterruttori`) |
| U3 | offset salvato = id invece di id + 1 | uccisa (`TestLettoreRispostaFissa`, `TestLettoreOffsetDopoOgniAggiornamento`, `TestLettoreAggiornamentoGiaLetto`, `TestLettoreFuoriDalFlusso`, `TestLettoreAggiornamentiStrani`, `TestLettoreAggiornamentoCheFallisce`, `TestLettoreNienteDatiNeiRegistri`, `TestLavoroDelLettore`) |
| U4 | aggiornamento dietro l'offset gestito | uccisa (`TestLettoreAggiornamentoGiaLetto`, `TestLettoreNienteDatiNeiRegistri`, `TestLavoroDelLettore`) |
| U5 | 4 tentativi invece di 3 | uccisa (`TestLettoreAggiornamentoCheFallisce`, `TestLettoreNienteDatiNeiRegistri`) |
| U6 | aggiornamento che fallisce saltato subito | uccisa (`TestLettoreAggiornamentoCheFallisce`, `TestLettoreConteggioDegliErrori`, `TestLavoroDelLettore`) |
| U7 | 409 non riconosciuto come conflitto | uccisa (`TestLettore409`) |
| U8 | 409 senza pulizia del token | uccisa (`TestLettore409`) |
| U9 | lettura riuscita non pulisce l'errore | uccisa (`TestLettoreAltriErrori`) |
| U10 | arresto: lettura chiusa salvata come errore | uccisa (`TestServeLettoreTelegram`) |
| U11 | lettura vuota senza attesa | uccisa (`TestProssimaAttesa`, `TestLavoroDelLettore`) |
| U12 | inattivo aspetta 5 s | uccisa (`TestProssimaAttesa`, `TestLavoroDelLettore`) |
| U13 | attesa dopo errore non crescente | uccisa (`TestProssimaAttesa`, `TestLavoroDelLettore`) |
| U14 | chat del negozio nel flusso | uccisa (`TestLettoreFuoriDalFlusso`) |
| U15 | altri bot nel flusso | uccisa (`TestLettoreFuoriDalFlusso`) |
| U16 | gruppi nel flusso | uccisa (`TestLettoreFuoriDalFlusso`) |
| U17 | /start@bot non capito | uccisa (`TestLettoreStart`, `TestLettoreStopEPrivacy`) |
| U18 | /disattiva non capito | uccisa (`TestLettoreStopEPrivacy`) |
| U20 | nome del negozio non protetto | uccisa (`TestLettoreStart`) |
| U21 | tastiera: «Sì» con consenso 0 | uccisa (`TestLettoreStart`) |
| U22 | pulsante oltre il limite risposto | uccisa (`TestLettoreLimiti`) |
| U23 | risposta fissa ogni 5 minuti | uccisa (`TestLettoreLimiti`) |
| U24 | 21 risposte fisse al minuto | uccisa (`TestLettoreLimiti`) |
| U25 | 9 risposte al minuto per chat | uccisa (`TestLettoreLimiti`) |
| U26 | 31 risposte al minuto in tutto | uccisa (`TestLettoreLimiti`) |
| U27 | finestra dei limiti con >= | uccisa (`TestLettoreLimiti`) |
| U28 | contatore delle chat sconosciute fermo | uccisa (`TestLettoreRispostaFissa`, `TestLettoreAggiornamentoGiaLetto`, `TestLettoreStart`, `TestLettoreLimiti`) |
| U29 | /privacy senza indirizzo pubblico manda «/privacy» | uccisa (`TestLettorePrivacySenzaIndirizzo`) |
| U30 | nessun answerCallbackQuery | uccisa (`TestLettorePulsanti`, `TestLettoreLimiti`) |
| U32 | risposta fissa senza limite per chat | uccisa (`TestLettoreLimiti`) |
| B1 | token non consumato | uccisa (`TestAvviaTelegram`, `TestAvviaTelegramRifiuti`, `TestBotTelegramDallaPagina`) |
| B2 | token valido alla scadenza esatta | uccisa (`TestAvviaTelegramRifiuti`) |
| B3 | busta chiusa accettata | uccisa (`TestAvviaTelegramRifiuti`) |
| B4 | busta conto terzi accettata | uccisa (`TestAvviaTelegramRifiuti`) |
| B5 | nuovo /start tiene il consenso | uccisa (`TestAvviaTelegramDiNuovo`) |
| B6 | consenso da un'altra chat | uccisa (`TestConsensoTelegram`) |
| B7 | pulsante vecchio riaccende | uccisa (`TestConsensoTelegram`) |
| B8 | stessa risposta riscritta | uccisa (`TestConsensoTelegram`) |
| B9 | /stop tocca le iscrizioni già fermate | uccisa (`TestFermaChatTelegram`) |
| B10 | /stop con motivo «page» | uccisa (`TestFermaChatTelegram`) |
| B11 | chat con sole iscrizioni fermate sconosciuta | uccisa (`TestFermaChatTelegram`) |
| B12 | token di 65 caratteri accettato | sopravvissuta al primo giro; uccisa dopo il test nuovo (`TestAvviaTelegramRifiuti`) |
| B13 | audit di /start con la chat | uccisa (`TestAvviaTelegram`, `TestConsensoTelegram`) |
| B14 | nome del negozio vuoto non sostituito | uccisa (`TestNomeNegozioDelBot`) |
| M1 | serve non avvia il lettore | uccisa (`TestServeLettoreTelegram`, `TestServeAspettaIlLettore`) |
| M2 | serve non aspetta il lettore | sopravvissuta al primo giro; uccisa dopo il test nuovo (`TestServeAspettaIlLettore`) |
| U19b | /stop di una chat sconosciuta col conteggio | uccisa (`TestLettoreStopEPrivacy`) |
| U31b | conteggio dei fallimenti di un altro aggiornamento | uccisa (`TestLettoreConteggioDegliErrori`) |

## Banco

**Giro valido: `s073`, il primo e unico: BANCO VERDE, 65 scenari, 1137 passi, 0 inattese, 0 errori; BANCO BROWSER
VERDE, 17 scenari, 34 viste, 0 inattese, 0 errori**; `banco giu`: nessun avanzo. Lanciato dopo `scripts/check`, con i
binari costruiti dall'albero finale (il codice non è cambiato dopo; solo due test). Nessuno scenario nuovo: nel banco
«Invio Telegram» è spento (D-03) e lo standalone non ha il bot, quindi il lettore resta inattivo e non fa richieste
(nessuna rete esterna, comunque, nella rete interna del banco). Prima di `banco giu` il registro del container Go aveva
18 righe con «telegram», non guardate una per una: il lettore inattivo non scrive nel registro (scrive solo errori e
avvisi, con `lavoro=lettore_telegram`), quindi sono le righe delle rotte `/telegram…` dello scenario della 072. Il
lettore acceso lo provano i test Go (Dubbio 6).

## Controlli

`scripts/check` (`slice-073/prove/check2.log`): **verde**, «voci 353: chiuse 37, rosse 0, parziali 128, aperte 177,
fuori v1 11», segreti e dati veri OK (prima della slice: chiuse 36, parziali 128, aperte 178: CLI-46 da aperta a
chiusa). Il primo giro (`check1.log`, verde) dava CLI-46 parziale perché `TestServeAspettaIlLettore` era stato aggiunto
mentre il controllo girava: il segno c'era nel sorgente ma non negli esiti. Il giro valido è il secondo, sull'albero
finale; il codice non è cambiato fra i due (solo i due test delle mutazioni B12 e M2).

## Per il manuale

- Il bot del negozio legge e risponde solo con «Invio Telegram» e «Notifiche Telegram ai clienti» accesi tutti e due e
  il bot configurato; spento uno dei due tace; riacceso riparte da solo entro mezzo minuto.
- Dopo «Attiva su Telegram» e «Avvia» il bot dice «👋 Sei stato collegato alla tua riparazione presso …» e chiede «✅ Sì,
  notifiche sulla riparazione» / «🔕 No grazie»: solo il «sì» accende le notifiche.
- Link già usato, scaduto o di una riparazione chiusa: «❌ Link di collegamento non valido o scaduto. Rigenera un nuovo
  link.». /stop o /disattiva spengono tutto, /privacy manda il link dell'informativa.
- Ogni altra cosa riceve al più una risposta ogni 10 minuti («Questo è il bot di …»), contata nella pagina dei provider.
  Gruppi, canali, altri bot e la chat del negozio non ricevono risposta.
- Un 409 nella pagina dei provider vuol dire un webhook o un altro programma sullo stesso bot: GestRip riprova sempre
  più di rado e non tocca il webhook.
- In `docs/manuale/da-scrivere.md`, sezione «Il bot Telegram del negozio (slice 073)».

## Dubbi

1. **Offset vecchio dopo una settimana di silenzio** (anche dello standalone). Secondo la documentazione del Bot API,
   dopo almeno una settimana senza aggiornamenti il numero del prossimo aggiornamento si sceglie a caso: se esce più
   basso dell'offset salvato, `getUpdates` con quell'offset non lo dà (e lo conferma), e il bot resta sordo finché i
   numeri non superano l'offset. Una bottega con il bot poco usato può restare una settimana senza messaggi; lo stesso
   dopo l'import se «Invio Telegram» resta spento a lungo. Non misurato (servirebbe il Bot API vero). Il Go fa come lo
   standalone. **Raccomandazione:** con la 075 (che tocca la pulizia oraria) dimenticare l'offset quando la sua riga
   non è stata scritta da più di 7 giorni (`aggiornata_il`): costa poche righe e al peggio rilegge gli aggiornamenti
   delle ultime 24 ore, che i token scaduti e la regola dell'offset rendono innocui. Va deciso perché è una differenza
   dallo standalone.
2. **`/privacy` senza indirizzo pubblico** (solo Go): la risposta fissa, perché non c'è un indirizzo da dare.
   **Raccomandazione:** tenerlo; senza indirizzo pubblico il riquadro della pagina non compare (072) e nessun cliente
   arriva al bot da lì. In alternativa si può mandare «Privacy policy • <negozio>» senza link.
3. **Cutover: lo standalone acceso sullo stesso bot.** Con i due interruttori accesi nel Go e lo standalone ancora
   acceso, i due lettori si rubano gli aggiornamenti e uno dei due vede il 409 (la pagina dei provider lo dice).
   **Raccomandazione:** nel promemoria del cutover: spegnere lo standalone (o almeno le sue «Notifiche Telegram ai
   clienti») prima di accendere «Invio Telegram» nel Go.
4. **Cutover: «Invio Telegram» spento a lungo dopo l'import.** Telegram tiene gli aggiornamenti non letti per 24 ore:
   i `/start` e i messaggi dei clienti arrivati mentre il Go non legge si perdono dopo un giorno (i token della pagina
   durano comunque 24 ore). **Raccomandazione:** come il Dubbio 6 della 072, accendere «Invio Telegram» appena fatta la
   revisione D-27, se il negozio usava Telegram.
5. **I limiti delle risposte usano l'orologio del lettore** (l'ora del sistema; lo standalone l'orologio monotono).
   Un'ora del sistema che torna indietro può tenere il bot zitto con una chat al più per i minuti del salto (al più 10
   per la risposta fissa). **Raccomandazione:** lasciarlo (l'ora la tiene NTP; così i test spostano l'orologio finto);
   se servisse, passare a `clock.Partenza`.
6. **Banco con il canale acceso:** nel banco Telegram è spento (D-03), quindi il lettore non parte nelle due app e il
   bot si vede solo nei test Go con il Bot API finto. **Raccomandazione:** come per la 072, il Bot API finto nella rete
   interna del banco con la 074 e, lì, uno scenario `/start` → «Sì» → `/stop` nelle due app.
7. **Risposte di `getUpdates` oltre 8 MiB** (review della 073): una risposta più grande non si legge tutta, è «non
   riconosciuta», l'offset non si muove e il bot resta sordo finché Telegram non lascia cadere quegli aggiornamenti
   (fino a 24 ore). Con 100 aggiornamenti di testo è quasi impossibile, ma non escluso (allegati con didascalie
   lunghe). **Raccomandazione:** con la 075, dopo una risposta troppo grande rileggere con `limit: 1` (un
   aggiornamento alla volta) finché si passa; costo basso.

## Review

Revisore indipendente, 13/11/2026 (pc-fisso, slice non committata). **Nessuna ALTA né MEDIA.** Correzioni nel repo,
ognuna con il rosso preso prima (`slice-073/prove/rosso-review.log` per le due del codice, sul codice di prima; per i test
nuovi la mutazione corrispondente sulla copia, `slice-073/mutazioni/giro-review.log`):

- **Codice, `NomeNegozio` prima di `AvviaTelegram`** (`LettoreTelegram.avvia`): prima, se il nome del negozio non si
  leggeva dopo l'iscrizione, il nuovo tentativo diceva «link non valido» a un cliente già iscritto. Ora si legge prima,
  come `shop_name/0` dello standalone. Test `TestLettoreNomeNegozioPrimaDiAvviare` (rosso: `AvviaTelegram` chiamato).
- **Codice, `recover` in `manda`**: un panico nell'invio di una risposta si lascia cadere con il solo genere nel
  registro, come `send_reply/4` con `rescue`; prima faceva ritentare l'aggiornamento (e ripetere `/start`). Test
  `TestLettoreRispostaInPanico` con un `RoundTripper` che va in panico su `sendMessage` (rosso: tre `AvviaTelegram`).
  Per il test il provider ha un campo `trasporto`, nil fuori dai test.
- **Test nuovi:** `TestLavoroDopoUnPanicoDellaLettura` (R1: un orologio finto in panico dentro `salvaErrore` con un
  502; il lavoro aspetta 5 s e 15 s), `TestLettoreArrestoFraDueAggiornamenti` (R2), `{"ok":true,"result":null}` in
  `TestLettoreAltriErrori` (R5), `TestTempoDellaLetturaLunga` (R3), `TestLettoreLimitiDiLunghezza` (R4: 409 di 600
  caratteri tagliato a 500; R11: risposta di circa 2 MiB capita). Corretto il commento di `TestLavoroDelLettore` (le
  iscrizioni mancanti mandano in panico l'aggiornamento, non la lettura).

**Mutazioni della review dopo le correzioni** (`muta.py`, `giro-review.log`, `esiti-review.json`; la copia del repo è
già cancellata). Delle R1–R17 della review sono state rifatte quelle che toccano le correzioni e i test nuovi; l'elenco
completo della review non era nel brief.

| # | Mutazione | Esito |
|---|---|---|
| R1 | panico della lettura non recuperato | uccisa (`TestLavoroDopoUnPanicoDellaLettura`) |
| R2 | nessun arresto fra due aggiornamenti | uccisa (`TestLettoreArrestoFraDueAggiornamenti`) |
| R3 | lettura lunga di 25 s | uccisa (`TestTempoDellaLetturaLunga`) |
| R4 | ultimo errore tagliato a 600 | uccisa (`TestLettoreLimitiDiLunghezza`) |
| R5 | `result: null` preso per una lista vuota | uccisa (`TestLettoreAltriErrori`) |
| R11 | `getUpdates` letto al più 1 MiB | uccisa (`TestLettoreLimitiDiLunghezza`) |
| N1 | nome del negozio dopo l'iscrizione (il codice di prima) | uccisa (`TestLettoreNomeNegozioPrimaDiAvviare`) |
| N2 | panico dell'invio non recuperato (il codice di prima) | uccisa (`TestLettoreRispostaInPanico`) |

Dopo le correzioni anche le 48 mutazioni della slice sono state rifatte sulla stessa copia (`giro3.log`,
`esiti-giro3.json`): **48 uccise su 48**.

`scripts/check` dopo le correzioni (`slice-073/prove/check3.log`): **verde**, «voci 353: chiuse 37, rosse 0, parziali
128, aperte 177, fuori v1 11», segreti e dati veri OK. Il banco lo rilancia la sessione principale.

**Verifica della sessione principale dopo le correzioni della review:** `scripts/check` verde, «voci 353: chiuse 37,
rosse 0, parziali 128, aperte 177, fuori v1 11»; banco su un'istanza nuova `s073p` con i binari dell'albero corretto:
**BANCO VERDE**, 65 scenari, 1137 passi, 0 inattese, 0 errori; **BANCO BROWSER VERDE**, 17 scenari, 34 viste, 0
inattese, 0 errori; `banco giu`: nessun avanzo.
