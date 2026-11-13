# 074 — Avvisi Telegram: popup del cambio di stato, nuovo messaggio del negozio, avvisi alla chat del negozio (T4.7, quarta slice)

- **Fonte:** brief del coordinatore del 13/11/2026, ripresa `2026-11-13-fine-073.md`. Lo standalone a `go-baseline` =
  `302439d`: `telegram_notices.ex` (`transition/3`, `prepare/3`, `authorize/2`, `compose_status/2`, `compose_message/2`,
  `notify_new_message/3`, `queue_text/4`, `queue_messages/4`, `escaped/1`, `first_name/1`, `first_line/1`, `cut/2`,
  `slug/1`), `telegram_notices/pending.ex`, `components/telegram_status_popup.ex`, `workshop.ex`
  (`transition_repair_freezing/5`, `status_version/1`, `transact_transition/5`), `telegram_subscriptions.ex`
  (`cut_off_chats/1`), `outgoing.ex` (`destination_for_shop/1`, `destination_for_subscriber/2`),
  `outgoing/telegram_inbound.ex` (`active?/0`), `quote_decision.ex:470-520` (`with_notice/3`, `notify_shop/3`,
  `notice_content/2`, `amount/1`), `public_message.ex` (`notify_shop/3`, `queue_notice/3`, `record_miss/3`,
  `miss_reason/1`, `notice_content/2`, `excerpt/1`), `repair_live.ex` (eventi `transition`, `telegram-status-send`,
  `telegram-status-cancel`, `reply`, `reply_notice/2`, `telegram_refusal/1`), `workshop_live.ex` (`transition`,
  `quick_status/3`, `after_telegram/1`); i test `telegram_notices_test.exs`, `telegram_status_notice_test.exs`,
  `status_telegram_popup_live_test.exs`, `repair_reply_telegram_live_test.exs`, `quote_decision_test.exs` e
  `public_repair_message_test.exs` (letti, non fatti girare). Inventario: CLI-47, CLI-48, RIP-H-18, CLI-14, CLI-08,
  CLI-13.
- **Voci:** CLI-47 (avviso del cambio di stato con il popup), RIP-H-18 (popup dalla lista, «✓ Pronta» che aspetta),
  CLI-48 (nuovo messaggio del negozio), CLI-14 (parte Telegram della risposta dalla scheda: con la 066 la voce ha
  ora tutte e due le sue parti, quindi la copertura che la segnava chiusa per sbaglio adesso dice il vero), CLI-08 e
  CLI-13 (avvisi alla chat del negozio). Tocca CLI-37 (le righe «Telegram cliente» nella lista degli invii, già
  pronta dalla 067).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** `versioni_stato_buste` e
  il suo trigger ci sono dalla 0007/0014, `iscrizioni_telegram` dalla 0008, le colonne `formato` e `tastiera` della
  coda dalla 0008 (usate dalla 071).

## Cosa c'è

- **Dominio** (`internal/buste/telegram_avvisi.go`):
  - `NotificaTelegram` = `Pending`: busta, numero visibile (D-64, per il popup), stato, versione, anteprima HTML,
    pulsante; le chat congelate in un campo non esportato, e `String`/`GoString` non le scrivono (come
    `@derive {Inspect, except: [:chats]}`).
  - Congelamento in `passaStato` (`stati.go`, `passaggio.congela`): solo `CambiaStato`, `CambiaStatoDallaLista` e
    `PassoRapido`; le chat iscritte con il consenso e non fermate si leggono nella transazione **prima**
    dell'UPDATE; dopo il commit `preparaNotifica` dà `Cambio.Telegram` o `Cambio.TelegramNonPreparata`.
  - `AutorizzaNotificaTelegram` = `authorize/2` (versione, busta, canale, controparte, righe in una transazione);
    `NotificaNuovoMessaggio` = `notify_new_message/3`; `accodaTelegram` = `queue_messages/4` (chat tagliate dal link
    fuori, link stabile, una riga per chat con `Posta.AccodaIn`, HTML e il pulsante).
  - Testi: `componiCambioStato`, `componiNuovoMessaggio` (valori protetti, `visivoStati`, tagli a 100 e 400 code
    point); `ModelloNonValido` ora è anche un `error`.
  - Negozio: `avvisaNegozio` (= `destination_for_shop(:telegram)` + `enqueue/3`, con `recover`), `avvisaMessaggio`
    (CLI-13, chiamata da `scriviMessaggioPubblico`), `testoAvvisoPreventivo` (CLI-08, chiamata da `rispondi` dopo il
    commit). `Buste.ConIndirizzoInterno` (in `serve` da `conf.IndirizzoInterno`).
  - Rifiuti nuovi: `ErrTelegramSuperata`, `ErrTelegramNonCliente`, `ErrTelegramLinkRevocato`; il canale spento è
    `ErrTelegramSpento` della 072.
- **Pagine** (`internal/web/telegram_avvisi.go`, `stati.go`, `lista_azioni.go`, `conversazione.go`,
  `pagine/base.html`, `statici/app.js`, `statici/app.css`):
  - `depositoNotifiche` (in memoria, gettone di 16 byte, un'ora, al più 256, legato a utente e nonce della
    sessione), `dopoCambio` (dopo ogni cambio di stato riuscito della scheda e della lista), `popupTelegram` (in
    `mostraConInfo`, una volta), la rotta `POST /repairs/{id}/notifica-telegram` (`personale`, nella tabella dei
    permessi come gli eventi `telegram-status-send`/`-cancel`).
  - `#telegram-status-popup` nel modello `popupTelegram` di `base.html`, con le classi del popup della 066 e due
    classi nuove (`avviso-anteprima`, `avviso-tasto`); `app.js`: fuoco su «Invia», Esc e tocco sullo sfondo =
    «Annulla».
  - «✓ Pronta» con il popup resta sulla lista e va al prezzo dopo la risposta; la risposta della scheda prova prima
    Telegram e poi, con zero righe o un testo rotto, il popup WhatsApp.

## Scelte

- **Il popup senza LiveView** (solo Go): lo standalone tiene `Pending` nel processo LiveView. Qui l'avviso sta nel
  deposito in memoria del server e la sessione porta solo un gettone casuale: le chat non vanno mai nel cookie (che
  è firmato ma leggibile) né nella pagina. La prima pagina del personale dopo il redirect disegna il popup e toglie
  il gettone: ricaricare è «Annulla», come lì. Un riavvio di `serve` perde i popup aperti, come una pagina chiusa;
  un popup vale un'ora (lo standalone finché la pagina vive). Il gettone è legato all'account e al nonce della
  sessione e alla busta della rotta: un altro account, un'altra sessione o un'altra busta non lo usano.
- **«Annulla» passa dal server** (un POST con `azione=annulla`): serve per «✓ Pronta», dove il prezzo finale aspetta
  la risposta qualunque sia (`after_telegram/1`), e senza JavaScript. Esc e il tocco sullo sfondo lo mandano.
- **Dopo la risposta del popup sulla scheda si torna con un segno nuovo** (`allaScheda`), anche dopo «Annulla»: il
  gettone, legato alla sessione e preso una volta, prova che il popup è il seguito di un cambio di stato appena fatto
  da quella scheda. Un gettone che non vale porta alla lista, mai alla scheda senza traccia.
- **Canale degli avvisi al cliente** (solo Go, come la D-16): oltre alle notifiche ai clienti e al bot dello
  standalone, «Invio Telegram» acceso (D-03) e `indirizzo_pubblico` (il pulsante porta il link `/r/`). Senza, nessun
  popup e nessun avviso. Il nome del bot non serve (lo chiede solo il riquadro della pagina del cliente, 072).
- **Il numero nei testi** come la D-82 (`NumeroNeiTesti`: «0045/26» con l'etichetta, «45/26» senza), anche negli
  avvisi alla chat del negozio; il popup dice «Busta N. 45/26» (D-64), come quello della 066. Lo standalone scrive
  `bag_number`. Dubbio 3.
- **«Apri la scheda»** (CLI-13): `indirizzo_interno + /repairs/<id>`. Il percorso della scheda nel Go è lo stesso
  dello standalone (`/repairs/<id>`), sulla faccia interna (lo standalone ha una faccia sola,
  `Endpoint.url()`). Senza `indirizzo_interno` resta il solo percorso (in `serve` c'è sempre). 
- **Mancato avviso del preventivo** (CLI-08): come lo standalone, nessuna riga (`with_notice/3` dà `notice: nil`);
  quello del messaggio (CLI-13) è `repair.message_notice_not_queued` con `not_configured` o `error`, come prima con
  `not_configured`. Dubbio 4.
- **Testo del popup:** «Il testo si cambia in Impostazioni › Testi dei messaggi.» (lo standalone: «… › Template.»,
  il nome della sua pagina).
- **Busta nel cestino fra il cambio e «Invia»:** `ErrBustaNonTrovata`, «non è stato possibile accodarla» (lo
  standalone la rilegge anche nel cestino e prova ad accodare).
- **L'anteprima si prepara dopo il commit** (`preparaNotifica` rilegge la busta, come `prepare/3` lavora sulla busta
  che il cambio ha lasciato): se un'altra scrittura cambia lo stato fra il commit e la lettura, l'anteprima può
  mostrare lo stato nuovo, ma la versione è quella del cambio, quindi «Invia» la rifiuta come superata. Niente parte
  con un testo che non corrisponde al cambio autorizzato.
- **Un errore del database nel preparare l'avviso** non tocca il cambio di stato: niente popup e una riga nel
  registro («notifica Telegram non preparata»), senza dati.
- **Convivenza con la 066 e la 070b:** come lo standalone, un cambio di stato apre solo il popup Telegram (il
  popup WhatsApp resta di «Rispondi», «Salva e avverti» e del QR); l'email di pronta parte nella transazione del
  cambio, indipendente dal popup. Il popup «📲 Avvisa cliente» resta solo WhatsApp (Dubbio 2).

## Prove

`buste` (`telegram_avvisi_test.go`, 18): `TestNotificaDelCambioDiStato`, `TestNotificaCongelaPrimaDellaConsegna`,
`TestNotificaNienteSenzaIscrittiOCanale`, `TestNotificaTestoCheNonSiCompone`, `TestNotificaSoloDaiCambiConIlPopup`
(lista, «✓ Pronta», QR, cambio rifiutato), `TestAutorizzaSuperata` (cambio dopo, QR, 📌), `TestAutorizzaIlTestoDelPopup`,
`TestAutorizzaRifiuti` (canale, notifiche, conto terzi, una chat tagliata e una iscritta di nuovo, tutte tagliate),
`TestNotificaNonScriveLeChat` (`%v`, `%+v`, `%#v`, `%s`), `TestComponiCambioStato` (tutti gli stati, sconosciuto,
tagli a 100), `TestCampiTelegramDichiarati`, `TestNuovoMessaggioTelegram` (due righe, un link, 400 e 401, testo del
titolare), `TestNuovoMessaggioNiente`, `TestNuovoMessaggioTuttoONiente`, `TestAvvisoNegozioPreventivo`,
`TestAvvisoNegozioPreventivoSenzaTelegram`, `TestAvvisoNegozioMessaggio` (grafemi: 401 tagliati, 300 di due code
point interi; oreficeria; senza indirizzo interno), `TestAvvisoNegozioMessaggioMancato` (not_configured ed error,
anche un panico).
`web` (`telegram_avvisi_test.go`, 4, più 3 della review, sotto): `TestPopupTelegramScheda`, `TestPopupTelegramAnnullaESuperato`,
`TestPopupTelegramLista`, `TestRispostaConTelegram`. `ruoli_test.go`: la rotta nuova nella tabella dei permessi.

**Rosso** (`slice-074/prove/`): test scritti prima di provare il codice, sul codice con le funzioni nuove ridotte a
scheletri che compilano e non fanno niente (`preparaNotifica`, `AutorizzaNotificaTelegram`,
`NotificaNuovoMessaggio`, `avvisaNegozio`, i due `componi…`, `avvisaMessaggio` che scrive `not_configured` come
prima, e nelle pagine `dopoCambio`, `popupTelegram`, `notificaTelegram`). `rosso-buste.log`: **16 rossi su 18**,
ognuno su un'asserzione di comportamento (avviso non preparato, righe che mancano, testi vuoti, motivo sbagliato);
`TestNotificaNienteSenzaIscrittiOCanale` e `TestAvvisoNegozioPreventivoSenzaTelegram` verdi anche sugli scheletri, com'è
naturale per due test del «non fa niente» (li tengono veri M1, M2 e M16). `rosso-web.log`: **4 rossi su 4** (nessun
popup, «Annulla» che va alla lista, risposta che va al popup WhatsApp). Al primo verde nessun test rosso.

## Mutazioni

Script `slice-074/mutazioni/muta.py` su una copia del repo (cancellata a fine lavoro), una mutazione alla volta,
`go vet` e i test del pacchetto toccato (`buste` con `-run 'Notifica|Autorizza|Componi|CampiTelegram|NuovoMessaggio|AvvisoNegozio'`,
`web` con `-run 'PopupTelegram|RispostaConTelegram'`). **48 mutazioni che compilano: 48 uccise, 0 sopravvissute** (due
solo dopo un test nuovo, sotto). Una prima forma non compilava (M30, `fmt` non importato), riscritta come M30b; M33b
è la forma più fine di M33. Esiti in `giro.log`, `giro-extra.log`, `esiti-giro1.json`, `esiti-parziali.json`.

Al primo giro sopravvivevano **M26** (estratto in code point invece che in grafemi: il messaggio di prova era sopra i
400 in tutti e due i conti; `TestAvvisoNegozioMessaggio` ora manda anche 300 grafemi di due code point, che non si
tagliano) e **W1** (il cambio dopo che non toglie il popup di prima: il test guardava la pagina fra i due cambi, e
così consumava il popup; ora i due cambi sono di fila). Rifatte con i test nuovi: uccise.

| # | Mutazione | Esito |
|---|---|---|
| M1 | notifiche ai clienti ignorate | uccisa (`TestNotificaNienteSenzaIscrittiOCanale`, `TestAutorizzaRifiuti`, `TestNuovoMessaggioNiente`) |
| M2 | indirizzo pubblico non chiesto | uccisa (`TestNotificaNienteSenzaIscrittiOCanale`) |
| M3 | chat senza consenso comprese | uccisa (`TestNotificaDelCambioDiStato`, `TestNuovoMessaggioTelegram`, `TestNuovoMessaggioNiente`) |
| M4 | chat fermate comprese | uccisa (`TestNotificaDelCambioDiStato`) |
| M5 | chat tagliate non riconosciute | uccisa (`TestAutorizzaRifiuti`) |
| M6 | & non protetta | uccisa (`TestNotificaDelCambioDiStato`, `TestAutorizzaIlTestoDelPopup`, `TestComponiCambioStato`, `TestCampiTelegramDichiarati`, `TestNuovoMessaggioTelegram`) |
| M7 | < non protetto | uccisa (`TestNotificaDelCambioDiStato`, `TestAutorizzaIlTestoDelPopup`, `TestComponiCambioStato`, `TestCampiTelegramDichiarati`, `TestNuovoMessaggioTelegram`) |
| M8 | nome intero invece del primo | uccisa (`TestNotificaDelCambioDiStato`, `TestAutorizzaIlTestoDelPopup`, `TestComponiCambioStato`, `TestCampiTelegramDichiarati`, `TestNuovoMessaggioTelegram`) |
| M9 | taglio a max-1 | uccisa (`TestComponiCambioStato`, `TestNuovoMessaggioTelegram`) |
| M10 | taglio senza «…» | uccisa (`TestComponiCambioStato`, `TestNuovoMessaggioTelegram`) |
| M11 | descrizione a 101 | uccisa (`TestComponiCambioStato`) |
| M12 | testo a 401 | uccisa (`TestNuovoMessaggioTelegram`) |
| M13 | titolo di «consegnata» sbagliato | uccisa (`TestNotificaCongelaPrimaDellaConsegna`, `TestComponiCambioStato`) |
| M14 | stato sconosciuto senza maiuscola | uccisa (`TestComponiCambioStato`) |
| M15 | descrizione non ripulita ai bordi | uccisa (`TestComponiCambioStato`) |
| M16 | prepara anche sulla busta conto terzi | uccisa (`TestNotificaNienteSenzaIscrittiOCanale`) |
| M17 | «Invia» senza controllo della versione | uccisa (`TestAutorizzaSuperata`) |
| M18 | «Invia» senza il canale | uccisa (`TestAutorizzaRifiuti`) |
| M19 | «Invia» senza la controparte | uccisa (`TestAutorizzaRifiuti`) |
| M20 | chat tagliate non escluse | uccisa (`TestAutorizzaRifiuti`) |
| M21 | nessuna chat rimasta non è un rifiuto | uccisa (`TestAutorizzaRifiuti`) |
| M22 | righe senza formato HTML | uccisa (`TestNotificaCongelaPrimaDellaConsegna`, `TestNuovoMessaggioTelegram`) |
| M23 | nuovo messaggio anche su conto terzi | uccisa (`TestNuovoMessaggioNiente`) |
| M24 | avviso al negozio senza recover | uccisa (`TestAvvisoNegozioMessaggioMancato`) |
| M25 | mancato avviso sempre «error» | uccisa (`TestAvvisoNegozioMessaggioMancato`) |
| M26 | estratto del messaggio in code point | sopravvissuta al primo giro; uccisa dopo il test nuovo (`TestAvvisoNegozioMessaggio`) |
| M27 | chi ha scritto scambiato | uccisa (`TestAvvisoNegozioMessaggio`) |
| M28 | importo in centesimi dimenticato | uccisa (`TestAvvisoNegozioPreventivo`) |
| M29 | rifiuto senza motivo dice «Motivo: » | uccisa (`TestAvvisoNegozioPreventivo`) |
| M31 | numero col «N.» nei testi (D-82) | uccisa (`TestNotificaDelCambioDiStato`, `TestAutorizzaIlTestoDelPopup`, `TestComponiCambioStato`, `TestCampiTelegramDichiarati`, `TestNuovoMessaggioTelegram`) |
| M32 | scheda senza congelare | uccisa (`TestNotificaDelCambioDiStato`, `TestNotificaCongelaPrimaDellaConsegna`, `TestNotificaTestoCheNonSiCompone`, `TestAutorizzaSuperata`, `TestAutorizzaIlTestoDelPopup`, `TestAutorizzaRifiuti`, `TestNotificaNonScriveLeChat`) |
| M33 | chat lette dopo la consegna | uccisa (`TestNotificaDelCambioDiStato`, `TestNotificaCongelaPrimaDellaConsegna`, `TestNotificaTestoCheNonSiCompone`, `TestNotificaSoloDaiCambiConIlPopup`, `TestAutorizzaSuperata`, `TestAutorizzaIlTestoDelPopup`, `TestAutorizzaRifiuti`, `TestNotificaNonScriveLeChat`) |
| M34 | risposta al preventivo senza avviso | uccisa (`TestAvvisoNegozioPreventivo`) |
| M35 | messaggio del cliente senza avviso | uccisa (`TestAvvisoNegozioMessaggio`, `TestAvvisoNegozioMessaggioMancato`) |
| W1 | il cambio dopo non toglie il popup di prima | sopravvissuta al primo giro; uccisa dopo il test nuovo (`TestPopupTelegramAnnullaESuperato`) |
| W2 | avviso non tolto dal deposito | uccisa (`TestPopupTelegramScheda`) |
| W3 | deposito senza controllo dell'account | uccisa (`TestPopupTelegramAnnullaESuperato`) |
| W4 | deposito senza controllo della busta | uccisa (`TestPopupTelegramAnnullaESuperato`) |
| W5 | popup non tolto dalla sessione | uccisa (`TestPopupTelegramScheda`) |
| W6 | «Annulla» manda | uccisa (`TestPopupTelegramAnnullaESuperato`, `TestPopupTelegramLista`) |
| W7 | anteprima con i tag | uccisa (`TestPopupTelegramScheda`) |
| W8 | dopo la scheda si va a dopo | uccisa (`TestPopupTelegramScheda`, `TestPopupTelegramAnnullaESuperato`) |
| W9 | «✓ Pronta» va al prezzo anche con il popup | uccisa (`TestPopupTelegramLista`) |
| W10 | risposta con Telegram apre anche il popup WhatsApp | uccisa (`TestRispostaConTelegram`) |
| W11 | testo Telegram rotto taciuto | uccisa (`TestRispostaConTelegram`) |
| W12 | testo rotto del cambio taciuto | uccisa (`TestPopupTelegramAnnullaESuperato`) |
| M30b | String con la chat | uccisa (`TestNotificaNonScriveLeChat`) |
| M33b | chat lette dopo l'UPDATE | uccisa (`TestNotificaCongelaPrimaDellaConsegna`) |

## Banco

**Giro valido: `s074`, il primo e unico: BANCO VERDE, 65 scenari, 1137 passi, 0 inattese, 0 errori; BANCO BROWSER
VERDE, 17 scenari, 34 viste, 0 inattese, 0 errori**; `banco giu`: nessun avanzo. Lanciato dopo `scripts/check`, con i
binari dell'albero finale. Nessuno scenario nuovo e nessuna riga in `bench/deviazioni.json`: nel banco «Invio
Telegram» è spento (D-03) e lo standalone non ha il bot, quindi nelle due app nessun popup e nessun avviso; il
mancato avviso del messaggio del cliente resta `not_configured` nelle due, come prima.

**Banco con il canale acceso: non fatto, piano per una 074b** (il costo supera la metà della slice; l'immagine dello
standalone non serve cambiarla):

1. **Bot API finto** come sottocomando del binario del banco (`banco bot-finto -ascolta :8081`), in un container in
   più della rete interna dell'istanza, con l'alias `bot.invalid`: `getMe`, `sendMessage`, `answerCallbackQuery`,
   `getUpdates` (da una coda di aggiornamenti), più due rotte di prova, `POST /_prova/aggiornamenti` (mette in coda un
   `/start <token>`, un pulsante `cli:tx:1:<id>`, un `/stop`) e `GET /_prova/mandati` (quello che il Go ha mandato,
   senza token). Nessuna rete esterna: la rete del banco è già `internal`.
2. **Seconda istanza Go** (`<nome>-gotg`) dalla stessa copia dei dati dopo l'import, con
   `TELEGRAM_API_ENDPOINT=http://bot.invalid:8081`, `indirizzo_pubblico`, e le impostazioni seminate da `banco su`
   (token e chat sintetici, `canale_telegram_acceso=1`, notifiche ai clienti, nome del bot), dietro Caddy con un
   alias suo.
3. **Scenario solo Go** `bench/scenari/telegram-acceso.json` con i passi `solo_go` che il banco ha già (slice 037b),
   più un passo nuovo «bot» che parla con le rotte `/_prova/…`: pulsante «Attiva su Telegram» della pagina `/r/` →
   `/start` nel bot finto → «Sì» → riquadro «Notifiche Telegram attive» → cambio di stato dalla scheda → popup →
   «Invia» → giro della coda (attesa di 30 s o `GET` fino a quando arriva) → `sendMessage` nel bot finto con il
   testo, l'HTML e il pulsante → `/stop` → riquadro di nuovo «Vuoi essere avvisato…».
4. Stima: circa 200 righe per il bot finto con i suoi test, 150 per `su`/`giu`/`esegui` e i loro test, 100 per il
   passo «bot» e lo scenario. Una slice intera.

## Controlli

`scripts/check` (`slice-074/prove/check2.log`): **verde**, «voci 353: chiuse 42, rosse 0, parziali 128, aperte 172,
fuori v1 11», segreti e dati veri OK (prima della slice: chiuse 37, aperte 177: CLI-47, CLI-48, RIP-H-18, CLI-08 e
CLI-13 da aperte a chiuse; CLI-14 era già chiusa per la copertura, ora anche nei fatti). Il primo giro (`check1.log`)
si era fermato a gofmt su `telegram_avvisi_test.go` (allineamento di due struct); corretto, nessun cambio di codice.

## Per il manuale

- Il riquadro «📲 Notifica Telegram al cliente» dopo un cambio di stato della scheda o della lista, con il testo
  in sola lettura e il pulsante; «Invia» lo mette in coda, «Annulla» (anche Esc, un tocco fuori, ricaricare) no.
- L'avviso «Consegnata» arriva anche se la consegna spegne le notifiche; QR, lotti e import non avvisano.
- «superata da un cambio di stato successivo» e gli altri rifiuti; «✓ Pronta» con un iscritto: prima il riquadro,
  poi il prezzo.
- La risposta dalla scheda a un iscritto parte da sola su Telegram; senza iscritti il popup WhatsApp.
- I testi rotti lo dicono in cima alla pagina; la chat del negozio riceve preventivi approvati e rifiutati e i
  messaggi dei clienti con «Apri la scheda».
- In `docs/manuale/da-scrivere.md`, sezione «Avvisi Telegram al cliente e al negozio (slice 074)».

## Dubbi

1. **Banco con il canale acceso: rimandato a una 074b** (sezione Banco). Costa più di metà di una slice: un Bot API
   finto dentro la rete del banco, una seconda istanza Go con un'altra configurazione e un modo di confrontare passi
   che lo standalone del banco non può fare (non ha il bot). **Raccomandazione:** farla come 074b, prima della 075,
   con il piano qui sopra; l'immagine dello standalone non cambia.
2. **Il popup «📲 Avvisa cliente» non manda ancora su Telegram.** Nello standalone (slice 145, `queue_text/4`,
   `telegram_reachable?/1`, `ReadyNotice.notify_telegram/4`) un cliente diretto iscritto ma senza telefono vede lo
   stesso il pulsante, e il popup e l'avviso di ritiro possono partire su Telegram con il testo scritto a mano. Nel
   Go il popup della 066 è solo WhatsApp (lo dice anche il suo commento), e il brief della 074 non lo nominava.
   **Raccomandazione:** una slice piccola dopo la 075 (le funzioni di coda e di taglio delle chat della 074 ci sono
   già: `accodaTelegram`), da segnare sulle voci CLI-29…CLI-36 dell'inventario.
3. **Il numero della busta nei testi Telegram, anche per il negozio, è quello della D-82** («0045/26» con
   l'etichetta, «45/26» senza), non `bag_number` dello standalone; il popup dice «Busta N. 45/26» come quello della
   066 (D-64). **Raccomandazione:** tenerlo: il negozio cerca la busta con il numero dell'etichetta, e la D-82 dice
   già «WhatsApp, e poi email e Telegram».
4. **Il mancato avviso della risposta al preventivo non lascia righe** (come lo standalone), mentre quello del
   messaggio del cliente sì (`repair.message_notice_not_queued`). Il brief parlava di audit per tutti e due.
   **Raccomandazione:** aggiungere, solo nel Go, una riga `repair.quote_notice_not_queued` con lo stesso motivo
   (costa poche righe, aiuta il titolare a capire perché la chat del negozio tace); deciderlo prima di farlo perché
   è una differenza dallo standalone.
5. **Il popup vive un'ora nella memoria del server** e un riavvio di `gestrip serve` lo perde (lo standalone lo
   tiene finché la pagina è aperta, e un riavvio chiude la pagina). **Raccomandazione:** tenerlo così; un'ora basta
   per rispondere, e un avviso perso si rimanda con un nuovo cambio di stato o dalla risposta.
6. **Gli avvisi al cliente chiedono anche «Invio Telegram» acceso e l'indirizzo pubblico** (solo Go, come la D-16
   per l'email): senza, nessun popup. Il nome del bot invece non serve. **Raccomandazione:** tenerlo; dopo il
   cutover il riquadro della pagina del cliente (072) chiede le stesse cose più il nome del bot, quindi un cliente
   può iscriversi solo quando gli avvisi possono partire.

## Review

Revisore indipendente, 13/11/2026 (pc-fisso, slice non committata). **Nessuna ALTA.** Correzioni nel repo, ognuna con
il rosso preso prima su una copia del repo con la mutazione corrispondente applicata (`slice-074/mutazioni/`,
`muta-review.py`, `giro-review.log`, `giro-review-2.log`, un log per mutazione; la copia è già cancellata):

- **MEDIA 1, doppio tocco su «Invia» (difetto vero).** Prima il secondo invio non trovava più il gettone e andava
  alla lista con «…il popup non è più valido.»: il browser mostrava proprio quella risposta, quindi «non inviata» con
  l'avviso già in coda, e dopo «✓ Pronta» si perdeva il prezzo; Esc dopo «Invia» mandava anche «Annulla». Ora: il
  modulo del popup è `data-invio-unico` (il pulsante premuto va in un campo nascosto, quindi `azione` arriva; dopo
  l'invio il modulo è segnato e Esc non manda niente); il deposito non toglie l'avviso alla risposta ma lo segna
  `risposto` per 5 minuti (mai oltre la sua scadenza): un secondo invio dello stesso popup torna dove torna il primo
  (la scheda, la lista o il prezzo), senza errore e senza accodare di nuovo, come lo standalone. Un gettone che non
  c'è (scaduto, di un'altra sessione, inventato) va ancora alla lista con la frase. Il popup già risposto non si
  disegna più. Test: `TestPopupTelegramScheda` (secondo invio alla scheda, una riga, nessun errore; gettone
  sconosciuto alla lista con la frase; `data-invio-unico` nel modulo), `TestPopupTelegramDoppioInvio` (due «Invia»
  dopo «✓ Pronta», tutti e due al prezzo, una riga).
- **MEDIA 2, campo CSRF del popup (R18):** il `contiene` del popup ha `<input name="_csrf_token" …>` con il token della
  pagina e il gettone.
- **MEDIA 3, «✓ Consegnata» dalla lista (R6):** `TestPopupTelegramLista` porta una busta pronta e iscritta a
  consegnata con `conferma=1`: il popup sulla lista, «Invia» accoda una riga e torna alla lista.
- **BASSE:** `TestPopupTelegramScadenzaESessioni` (R1: il gettone di una sessione usato da un'altra sessione dello
  stesso account va a `/` e non accoda; R2, R3: dopo 61 minuti «Invia» dice «non più valido» e il popup non si
  disegna; R4: 257 popup, il primo sparisce); `TestComponiCambioStato` (R12: 60 «è» non tagliate, 101 tagliate a 100
  e «…»); `TestAutorizzaRifiuti` (R8: «Invia» su una busta nel cestino è `ErrBustaNonTrovata`, niente in coda);
  `TestAvvisoNegozioPreventivo` (R9: testo dell'importo vuoto e centesimi presenti, «Importo: € 99,00»);
  `TestRispostaTelegramCodaCheRifiuta` (R16: la coda che rifiuta dà «Notifica Telegram non accodata: non è stato
  possibile accodarla.» e il popup WhatsApp).

**Mutazioni della review dopo le correzioni.** Il brief della correzione nominava R1, R2, R3, R4, R6, R8, R9, R12, R16,
R18 e diceva R14, R15 e R17 equivalenti o quasi e R7 uccisa per stallo; il testo delle altre (R5, R7, R10, R11, R13,
R14, R15, R17, R19) non era nel brief, quindi non sono state rifatte qui. D1–D3 sono le mutazioni del difetto MEDIA 1.

| # | Mutazione | Esito |
|---|---|---|
| D1 | secondo invio senza gettone (il codice di prima) | uccisa (`TestPopupTelegramScheda`, `TestPopupTelegramDoppioInvio`) |
| D2b | secondo invio che accoda di nuovo | uccisa (`TestPopupTelegramScheda`, `TestPopupTelegramDoppioInvio`) |
| D3 | modulo del popup senza `data-invio-unico` | uccisa (`TestPopupTelegramScheda`) |
| R1 | deposito senza la sessione (nonce) | uccisa (`TestPopupTelegramScadenzaESessioni`) |
| R2 | risposta senza scadenza | uccisa (`TestPopupTelegramScadenzaESessioni`) |
| R3 | popup scaduto disegnato | uccisa (`TestPopupTelegramScadenzaESessioni`) |
| R4 | deposito senza limite a 256 | uccisa (`TestPopupTelegramScadenzaESessioni`) |
| R6 | «✓ Consegnata» senza popup | uccisa (`TestPopupTelegramLista`) |
| R8 | busta nel cestino non esclusa dalla lettura di «Invia» | **equivalente**: sopravvive perché anche `garantisciLinkIn` rifiuta la busta nel cestino con `ErrBustaNonTrovata`, prima di ogni riga; il test nuovo prova il comportamento (rifiuto e niente in coda) |
| R9 | «Importo: » vuoto con il testo vuoto | uccisa (`TestAvvisoNegozioPreventivo`) |
| R12 | taglio contato in byte | uccisa (`TestComponiCambioStato`) |
| R16 | errore Telegram generico taciuto | uccisa (`TestRispostaTelegramCodaCheRifiuta`) |
| R18 | popup senza campo CSRF | uccisa (`TestPopupTelegramScheda`) |

D2 nella prima forma non compilava (variabile non usata), riscritta come D2b. Dopo le correzioni le 48 mutazioni della
slice sono state rifatte sulla stessa copia (`muta-giro3.py`, `giro3.log`, `esiti-giro3.json`): 44 uccise sulle 44 che hanno ancora il loro testo
(M30 non compila come al primo giro; W2, W3, W4 e W6 non trovavano più il loro testo, cambiato dalla correzione: W2 è
diventata D1/D2b, le altre sono state rifatte sul codice nuovo in `muta-giro3b.py`, `giro3b.log`). W4b e W6b uccise
(`TestPopupTelegramAnnullaESuperato`, `TestPopupTelegramLista`); **W3b** (il deposito senza il controllo
dell'account) sopravvive ed è **equivalente**: resta il controllo del nonce della sessione, 16 byte casuali a ogni
accesso, quindi un altro account non ha mai lo stesso nonce (prima della correzione W3 toglieva tutti e due ed era
uccisa).

`scripts/check` dopo le correzioni (`slice-074/prove/check3.log`): **verde**, «voci 353: chiuse 42, rosse 0, parziali 128, aperte
172, fuori v1 11», segreti e dati veri OK. Il banco lo rilancia la sessione principale.


**Verifica della sessione principale dopo le correzioni della review:** `scripts/check` verde, «voci 353: chiuse 42,
rosse 0, parziali 128, aperte 172, fuori v1 11», segreti e dati veri OK. Un primo giro del banco (`s074p`) ha dato
HTTP verde (65 scenari, 1137 passi, 0 inattese) ma il livello browser è stato interrotto per memoria quasi finita su
pc-fisso (un processo estraneo al progetto), istanza smontata senza avanzi; non conta. Giro valido su un'istanza nuova
`s074q`: **BANCO VERDE**, 65 scenari, 1137 passi, 0 inattese, 0 errori; **BANCO BROWSER VERDE**, 17 scenari, 34 viste,
0 inattese, 0 errori; `banco giu`: nessun avanzo.
