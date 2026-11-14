# 075 — Telegram: risposte del cliente, spegnimento e pulizia, offset vecchio, rilettura sopra 8 MiB

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/telegram_replies.ex` (`bag/2`,
  `post/3`), `public_message.ex` (`post/5` con `:telegram` e `also`), `outgoing/telegram_inbound.ex`
  (`answered_bag/2`, `command?/1`, `answer/5`), `outgoing/telegram_inbound/reply_limit.ex` (`:incoming`),
  `telegram_subscriptions.ex` (`purge/1`, `block_chat/1`, `cut_off/2`, `cut_off_chats/1`), `outgoing/telegram_poller.ex`
  (`purge_due?/2`), migrazioni `20261012150000` e `20261013160000`; test `telegram_inbound_reply_test.exs`,
  `telegram_subscriptions_test.exs` («switching off by itself», «purge/1»), `telegram_poller_test.exs`.
- **Decisioni:** Stefano, 13/11/2026 (`docs/SVILUPPO.md`, «Decisioni di Stefano del 13/11/2026»): con la 075 l'offset
  vecchio dopo una settimana (Dubbio 1 della 073) e la rilettura con `limit: 1` sopra gli 8 MiB (Dubbio 7 della 073);
  la riga d'audit `repair.quote_notice_not_queued` solo Go con una D nuova.
- **Voci:** CLI-49 (nuova, portata tutta), CLI-50 (la parte che mancava: pulizia oraria; il resto c'era già e ora
  ha i segni), CLI-46 (offset, rilettura), CLI-08 (D-86), CLI-13 e CLI-12 (la risposta Telegram è il messaggio della
  pagina). Chiude la T4.7 (Telegram) per la parte di questa tappa.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. **Nessuna migrazione**: l'indice
  della corrispondenza (`messaggi_in_uscita_risposte_telegram`, = `20261013160000`) e i trigger dello spegnimento
  (0014, = `20261012150000`) c'erano già.

## Cosa c'è

- **Risposte del cliente a una notifica (CLI-49).**
  - **Aggancio** in `LettoreTelegram.messaggio` (`internal/uscita/lettore_telegram.go`), come previsto dalla 073:
    dopo i bot e la chat del negozio, `bustaRisposta` (= `answered_bag/2`): solo un `reply_to_message` con
    `message_id` intero, solo se il testo non è un comando del bot (`reComandoBot` = `command?/1`,
    `^\s*/(start|stop|disattiva|privacy)(@\S+)?(\s|$)` con `iu`: spazi Unicode come la regex di Elixir con «u»,
    maiuscole Unicode); «/10 va bene» e «/stopper» sono messaggi. Senza busta: comando o testo libero come prima.
  - **Corrispondenza** (`Buste.BustaDellaRisposta`, `internal/buste/telegram_risposte.go` = `bag/2`): la riga della
    coda `telegram`/`customer`/`accepted` con `riferimento_provider` = il message_id e `indirizzo` = la chat,
    `accodato_il` da non più di 90 giorni (`>=`), con la busta, la più recente (`ORDER BY id DESC`); 0 se la chat è
    fra le tagliate con il link di quella busta (`chatTagliate` = `cut_off_chats/1`). Un `/stop` non conta.
  - **Il messaggio** (`LettoreTelegram.rispostaDelCliente` = `answer/5`): senza testo o di soli spazi (Unicode) «❌
    Solo risposte di testo per ora.» senza contarlo; oltre 8 al minuto per chat (nuovo genere `rispostaIngresso` di
    `limiteRisposte`, in memoria come `ReplyLimit :incoming`) «⏳ Hai inviato troppi messaggi: attendi qualche minuto
    e riprova.»; il resto si conta e va a `Buste.RispostaTelegram` (= `post/3`).
  - **`RispostaTelegram`:** la busta fuori dal cestino prima del testo (come `Repo.get/2` prima di `post/5`), poi
    `testoMessaggio` (vuoto, 1000 code point), poi `scriviMessaggio` con il link nuovo `linkTelegram`: nella
    transazione della pagina busta chiusa (`ErrRispostaChiusa`), poi di un'oreficeria (`ErrRispostaNonTrovata`, come
    il cestino e la busta sparita), il limite di 10 all'ora per busta contato con la pagina (`ErrRispostaTroppe`), la
    riga del cliente senza canale, la busta non letta, `repair.message_appended` con l'attore `telegram`, **poi `dopo`
    nella stessa transazione** (= `also`), commit, e l'avviso alla chat del negozio «💬 Messaggio dal cliente — busta
    N» (CLI-13) con l'attore `telegram`.
  - **Offset nella transazione:** il lettore passa come `dopo` la scrittura di `telegram_inbound.offset` = id + 1;
    un rifiuto non scrive niente, e l'offset lo salva il lettore dopo come per ogni aggiornamento.
  - **Le parole del bot** (dello standalone): «✅ Messaggio inviato al negozio.», «❌ Testo vuoto.», «❌ Risposta troppo
    lunga (N/1000), accorcia.» (N in code point del testo ripulito), «⏳ Hai già inviato molti messaggi nell'ultima
    ora: riprova più tardi.», «Questa riparazione non riceve più messaggi.», «❌ Riparazione non trovata.». La
    risposta cita il messaggio (`reply_parameters` con il `message_id` com'è e `allow_sending_without_reply: true`) e
    consuma i limiti delle risposte come le altre (`reply/4`): oltre 8 al minuto la riga si scrive e la conferma no.
  - **Interfaccia:** `uscita.IscrizioniTelegram` ha `BustaDellaRisposta` e `RispostaTelegram`; i rifiuti sono
    `uscita.ErrRispostaVuota`, `…TroppoLunga`, `…Troppe`, `…Chiusa`, `…NonTrovata`. Un altro errore (database) fa
    fallire l'aggiornamento: si riprova, al terzo si salta (073).
- **Spegnimento e pulizia (CLI-50).**
  - **Già fatto e ora segnato CLI-50:** i tre trigger della 0014 (consegnata/annullata/chiusa, cestino, oreficeria:
    `TestChiusuraSpegneLeIscrizioniTelegram`, `TestAltriSpegnimentiTelegram`), il 403 che spegne la chat su tutte le
    buste e il 403 del negozio che non spegne niente (`TestTelegram403`, 071), le notifiche in coda che falliscono con
    le notifiche ai clienti spente (`TestTelegramClientiSpentiFallisce`). Nuovo test di carattere:
    `TestTelegramNonSiRiaccende` (riapertura, cestino tolto con SQL, ritorno al cliente diretto: niente si riaccende).
  - **Nuovo: `LettoreTelegram.Pulisci`** (= `purge/1`, in una transazione): via le iscrizioni con `fermata_il` più
    vecchia di 90 giorni, i token con `scade_il <= adesso`; le righe Telegram dei clienti **non in coda** accodate da
    più di 90 giorni prendono l'indirizzo `cancellato` (non riconta quelle già cancellate). Dà i tre conti.
  - **Ogni ora dal lavoro del lettore** (`puliziaProtetta` = `purge_due?/2` e `purge/2`): prima di ogni lettura, se
    l'ultima è di almeno un'ora fa (la prima subito), **anche a canale spento** (il lavoro gira sempre in `serve` con
    il provider vero); un errore o un panico (anche dell'orologio) vanno nel registro col solo genere e la lettura
    continua.
- **Offset vecchio (D-85, solo Go).** `LettoreTelegram.offset` legge anche `aggiornata_il` (`leggiConIstante` in
  `config_telegram.go`, così il controllo dei file che toccano `impostazioni` non cambia) e dimentica un offset
  scritto **più** di sette giorni fa; a sette giorni esatti vale; un istante che non si legge non decide.
- **Rilettura sopra 8 MiB (Dubbio 7 della 073).** `richiestaPer` dà `errRispostaTroppoGrande` per una risposta oltre
  il massimo (`richiesta`, usata da tutto il resto, lo riporta al comportamento di prima: corpo nil, nessun errore);
  `chiediAggiornamenti` lo dice (`grande`, solo per un 200); `Leggi` rilegge subito con `limit: 1` e tiene il modo «un
  aggiornamento alla volta» (`unoAllaVolta`, al più 100 letture) finché una lettura torna vuota. Un solo aggiornamento
  oltre 8 MiB resta l'errore «telegram:risposta non riconosciuta». Corpo di `getUpdates` con `"limit":1` fra
  `offset` e `timeout`, assente senza.
- **Mancato avviso del preventivo (D-86, solo Go).** `rispondi` (`preventivo.go`) scrive
  `repair.quote_notice_not_queued` (`bag_number` salvato, `reason` `not_configured` o `error`, il contesto di chi ha
  risposto) quando `avvisaNegozio` non accoda; il motivo è quello di `repair.message_notice_not_queued`, ora in
  `registraAvvisoMancato` (`telegram_avvisi.go`) per tutti e due.
- **Banco** (solo codice e scenario, non lanciato): bot finto con `{"comando":"rispondi","testo":…}` (risposta con
  `reply_to_message` all'ultima notifica con un link mandata alla chat del cliente sintetico; 409 senza notifiche,
  400 per un testo vuoto, oltre 4096 caratteri o su un altro comando) e `GET /_prova/mandati?chat=…`; scenario
  `telegram-acceso` con 7 passi in più (sotto, «Banco»).
- **Documenti:** `docs/SVILUPPO.md` (sezione nuova «Risposte del cliente e pulizia Telegram», e i rimandi in 071, 073,
  074, 074b), `docs/manuale/da-scrivere.md` (sezione della 075, riga del registro del preventivo).

## Scelte

- **Busta prima del testo** in `RispostaTelegram`: `post/3` fa `Repo.get/2` e solo dopo `PublicMessage.post/5`
  guarda il testo: una busta nel cestino con un testo troppo lungo è «non trovata». Lo stesso nel Go (lettura fuori
  dalla transazione, poi di nuovo dentro).
- **Chiusa prima di oreficeria** (`open/2` con `:telegram`): una busta chiusa e di un'oreficeria dice «non riceve più
  messaggi». Nel Go `scriviMessaggio` già controlla la chiusura prima della classe; per la pagina `/r/` niente cambia
  (`linkPagina` non guarda la classe), per `/stato` neanche.
- **«Non è stato possibile inviare il messaggio.»** dello standalone (`{:error, :invalid}` dal changeset) non c'è: nel
  Go non c'è un changeset che possa rifiutare dopo i controlli; un errore del database fa fallire l'aggiornamento
  (come un `raise` nello standalone).
- **La pulizia in una transazione** (lo standalone fa tre istruzioni separate): stesso risultato, niente a metà.
- **La pulizia nel pacchetto `uscita`** (lo standalone la tiene in `TelegramSubscriptions`): è SQL sulle tre tabelle e
  la chiama solo il lavoro del lettore; `coda.go` tocca già `iscrizioni_telegram` per il 403. Nessun metodo in più
  per le buste.
- **Il modo «un aggiornamento alla volta» è in memoria** del lettore: dopo un riavvio si riparte senza limite e, se
  la risposta è ancora troppo grande, si rientra nel modo alla prima lettura.
- **`rispostaIngresso`** ha solo il limite per chat (8 al minuto), come `:incoming`; niente limite globale.
- **Audit della risposta:** `repair.message_appended` e l'avviso al negozio (`outgoing.queued`, o la riga del mancato
  avviso) con l'attore `telegram` (`@bot` dello standalone); mai la chat né il testo.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-075/prove/`): con gli scheletri (interfaccia, errori e
  `Pulisci` vuoti, metodi delle buste che non fanno niente):
  - `rosso-uscita.log`: rossi sull'asserzione `TestLettoreRispostaAUnaNotifica`, `…Rifiuti`, `…SoloTesto`,
    `…OttoAlMinuto`, `…ComandoOMessaggio`, `TestLettoreOffsetVecchio`, `TestLettoreRispostaTroppoGrande`,
    `TestPuliziaTelegram`, `TestLavoroPuliziaOraria`, `TestLettoreRispostaANotificaSenzaBusta` (la busta non si
    cercava). `TestLettoreRispostaFuoriDalFlusso` passava già (prova che niente succede).
  - `rosso-buste.log`: rossi `TestBustaDellaRisposta`, `TestRispostaTelegram`, `…Rifiuti`, `…DalBot`,
    `TestAvvisoPreventivoNonAccodato`; `TestTelegramNonSiRiaccende` è di carattere (comportamento dei trigger già
    presente): rosso solo per un errore nel test (consegna diretta da «in lavorazione»), corretto, poi verde.
  - `rosso-banco.log`: `TestBotFintoConIlLettoreVero` (parte nuova) e `TestBotFintoRispondi`.
- **Giro con il lettore vero** (`TestRispostaTelegramDalBot`, `internal/buste`): Bot API finto in processo, il negozio
  scrive, la notifica è accettata con il message_id, il cliente risponde: messaggio nella conversazione, avviso alla
  chat del negozio con il link della scheda, conferma con `reply_parameters`, offset 41; dopo `/stop` risponde
  ancora; tagliata con il link riceve la risposta fissa.
- **Bot finto del banco con il lettore vero** (`TestBotFintoConIlLettoreVero`): dopo il messaggio della coda,
  `rispondi` → il lettore porta la risposta alla busta (iscrizioni finte), conferma citando, offset scritto nella
  transazione della risposta.
- **Limiti di tempo e spazi:** 7 giorni esatti/+1 µs (offset), 90 giorni esatti/+1 µs (corrispondenza, iscrizioni,
  righe), scadenza esatta dei token, 59:59/1 h/30 min (pulizia), 59 s/60 s (otto al minuto), NBSP ed em space
  (comandi e soli spazi), «/ſtop» (maiuscole Unicode di `i`).
- **Mai dati nel registro:** `TestLettoreRispostaAUnaNotifica` controlla che chat e testo non ci siano; l'audit
  senza chat (`TestRispostaTelegram`).

## Mutazioni

47 mutazioni a mano sul codice nuovo (lettore, rilettura, pulizia, corrispondenza, risposta, D-86, bot finto), una
alla volta su una copia del repo (`to-be-deleted-after-use/slice-075/mutazioni/`: `muta.py`, `giro*.log`,
`esiti/<id>.log`; copia cancellata a fine giro). **47 prese su 47**, tutte da un'asserzione (nessun log con un panico
prima della caduta). Al primo giro (`giro1.log`) 42 prese; poi:
- **non compilavano** (variabile o import rimasti inutili) U02, B03, K02, K03: riscritte perché compilino e rifatte
  (`giro2.log`, `giro3.log`), prese;
- **U29 viva** nella prima forma («`fatta = adesso` tolto prima di `Pulisci`»): equivalente, perché la funzione dà
  comunque `adesso` alla fine (conta solo dopo un panico di `Pulisci`). Sostituita con «l'ora dell'ultima pulizia non
  tenuta» (`return ultima`), presa;
- **K03 viva** al secondo giro: il limite dei 4096 caratteri di `rispondi` non si raggiungeva, perché il corpo si
  leggeva al più per 4096 byte (il JSON non si decodificava prima). Corretto il bot finto (corpo fino a 64 KiB) e il
  test (4096 «è» passano, 4097 no); rifatta, presa (`giro3.log`);
- **U20** era presa dall'asserzione di `TestLettoreRispostaAUnaNotifica`, ma in `TestLettoreRispostaRifiuti` faceva
  anche un panico (`sotto` su un `reply_parameters` mancante): il test ora non va in panico; rifatta, presa senza
  panico (`giro4.log`).

| # | Mutazione | Presa da |
|---|---|---|
| U01 | offset a sette giorni esatti dimenticato | `TestLettoreOffsetVecchio` |
| U02 | offset mai dimenticato | `TestLettoreOffsetVecchio` |
| U03 | istante illeggibile dimentica l'offset | `TestLettoreOffsetVecchio` |
| U04 | nessuna rilettura con limit 1 | `TestLettoreRispostaTroppoGrande` |
| U05 | modo un-alla-volta non azzerato dalla lettura vuota | `TestLettoreRispostaTroppoGrande` |
| U06 | modo un-alla-volta non tenuto per le letture dopo | `TestLettoreRispostaTroppoGrande` |
| U07 | risposta troppo grande senza segnale | `TestLettoreRispostaTroppoGrande` |
| U08 | comando del bot senza maiuscole | `TestLettoreRispostaComandoOMessaggio` |
| U09 | comando del bot con spazi solo ASCII | `TestLettoreRispostaComandoOMessaggio` |
| U10 | comando senza la fine della parola | `TestLettoreRispostaComandoOMessaggio` |
| U11 | un comando che risponde va nella conversazione | `TestLettoreRispostaComandoOMessaggio`, `TestLettoreRispostaANotificaSenzaBusta` |
| U12 | soli spazi presi per testo | `TestLettoreRispostaSoloTesto` |
| U13 | otto al minuto non controllati | `TestLettoreRispostaOttoAlMinuto` |
| U14 | messaggi del cliente non contati | `TestLettoreRispostaOttoAlMinuto` |
| U15 | nove al minuto | `TestLettoreRispostaOttoAlMinuto` |
| U16 | limite per chat contato sulle altre risposte | `TestLettoreRispostaOttoAlMinuto` |
| U17 | offset nella transazione senza +1 | `TestLettoreRispostaAUnaNotifica` |
| U18 | conto della lunghezza senza ripulire | `TestLettoreRispostaRifiuti` |
| U19 | busta chiusa detta come non trovata | `TestLettoreRispostaRifiuti` |
| U20 | conferma senza citare il messaggio | `TestLettoreRispostaAUnaNotifica`, `TestLettoreRispostaRifiuti` |
| U21 | rifiuto sconosciuto preso per riuscito | `TestLettoreRispostaRifiuti` |
| U22 | iscrizioni fermate da 90 giorni esatti cancellate | `TestPuliziaTelegram` |
| U23 | token alla scadenza esatta tenuti | `TestPuliziaTelegram` |
| U24 | chat cancellata anche dalle righe in coda | `TestPuliziaTelegram` |
| U25 | chat cancellata anche dalle righe del negozio | `TestPuliziaTelegram` |
| U26 | pulizia anche delle email | `TestPuliziaTelegram` |
| U27 | pulizia ogni ora esatta saltata | `TestLavoroPuliziaOraria` |
| U28 | pulizia mai dal lavoro | `TestLavoroPuliziaOraria` |
| U29 | ora dell'ultima pulizia non tenuta | `TestLavoroPuliziaOraria` |
| B01 | notifica di 90 giorni esatti persa | `TestBustaDellaRisposta` |
| B02 | notifica non accettata presa | `TestBustaDellaRisposta` |
| B03 | chat tagliata con il link risponde ancora | `TestBustaDellaRisposta`, `TestRispostaTelegramDalBot` |
| B04 | notifica del negozio presa | `TestBustaDellaRisposta` |
| B05 | la notifica più vecchia invece della più recente | `TestBustaDellaRisposta` |
| B06 | busta cercata dopo il testo | `TestRispostaTelegramRifiuti` |
| B07 | risposta nella conversazione di un'oreficeria | `TestRispostaTelegramRifiuti` |
| B08 | dopo non chiamato | `TestRispostaTelegram` |
| B09 | errore di dopo ignorato | `TestRispostaTelegram` |
| B11 | busta chiusa detta come non trovata (dominio) | `TestRispostaTelegramRifiuti` |
| B12 | limite orario non tradotto | `TestRispostaTelegramRifiuti` |
| B13 | mancato avviso del preventivo senza riga | `TestAvvisoPreventivoNonAccodato` |
| B14 | mancato avviso del preventivo con l'azione del messaggio | `TestAvvisoPreventivoNonAccodato` |
| B15 | motivo sempre error | `TestAvvisoPreventivoNonAccodato`, `TestAvvisoNegozioMessaggioMancato` |
| B16 | riga del preventivo anche con l'avviso partito | `TestAvvisoPreventivoNonAccodato` |
| K01 | bot finto: notifica della chat sbagliata | `TestBotFintoRispondi` |
| K02 | bot finto: filtro per chat ignorato | `TestBotFintoRispondi` |
| K03 | bot finto: testo senza limite | `TestBotFintoRispondi` |

## D nuove da approvare

Testo per la tabella «Deviazioni volute» dell'inventario (le scrive il coordinatore):

- **D-85 (CLI-46; slice 075, Dubbio 1 della 073, approvato il 13/11/2026).** Il lettore del bot dimentica l'offset di
  `getUpdates` (`telegram_inbound.offset`) quando la sua riga non è stata scritta da più di sette giorni
  (`aggiornata_il`): la lettura parte senza offset. Motivo: dopo almeno una settimana senza aggiornamenti Telegram può
  ricominciare a numerarli più in basso, e con l'offset vecchio il bot resterebbe sordo. Lo standalone tiene l'offset
  per sempre. Senza offset Telegram ridà solo gli aggiornamenti non ancora confermati (di solito nessuno), quindi
  dimenticarlo non rilegge niente di già gestito. Il Go conta i sette giorni da quando ha scritto l'offset, Telegram
  dall'arrivo dell'ultimo aggiornamento: Dubbio 6. Nel Go: `LettoreTelegram.offset` (`internal/uscita/lettore_telegram.go`). Nel banco:
  niente (non si vede).
- **D-86 (CLI-08; slice 075, approvata il 13/11/2026).** Quando l'avviso della risposta al preventivo alla chat del
  negozio non parte (Telegram non configurato o spento, coda che rifiuta o va in panico), il Go scrive
  `repair.quote_notice_not_queued` con il numero della busta e il motivo (`not_configured` o `error`), come
  `repair.message_notice_not_queued` del messaggio del cliente; lo standalone non lascia traccia. Nel Go: `rispondi`
  (`internal/buste/preventivo.go`) e `registraAvvisoMancato` (`telegram_avvisi.go`). Nel banco: nessuno scenario
  confronta le righe d'audit dopo una risposta al preventivo, quindi nessun normalizzatore.

**Rilettura con `limit: 1` sopra gli 8 MiB: nessuna D.** Il limite degli 8 MiB è già solo del Go (073, senza D); la
rilettura fa sì che il Go, come lo standalone, legga tutti gli aggiornamenti. Se il coordinatore preferisce una D,
il testo: «Una risposta di `getUpdates` oltre gli 8 MiB si rilegge un aggiornamento alla volta (`limit: 1`) finché una
lettura torna vuota; lo standalone legge risposte di qualunque grandezza.»

## Banco

**Giro finale valido `s075q`** (sessione principale, binari dell'albero finale dopo la correzione qui sotto e
`scripts/check` verde, con il modello locale spento per la memoria): **BANCO VERDE**, HTTP 66 scenari, 1164 passi,
1548 differenze attese, 0 inattese, 0 errori, non provati 0; **BANCO BROWSER VERDE**, 17 scenari, 34 viste, 0 inattese,
0 errori; `telegram-acceso` verde nell'esito, compreso il passo 22 (avviso al negozio della risposta del cliente);
`banco giu`: «AVANZI: nessuno». Il primo giro `s075p` (qui sotto) ha trovato un bug del prodotto.

**Giro `s075p`** (sessione principale, binari dell'albero prima della correzione qui sotto): livello HTTP 66 scenari,
1164 passi, **2 inattese, tutte e due in `telegram-acceso[22]`**: `GET /_prova/mandati?metodo=sendMessage&chat=-4242000`
con `json:totale` 0 (atteso 1) e `json:ultimo.text` «(assente)» anche dopo 90 s. Il passo 21 era verde (conferma
«✅ Messaggio inviato al negozio.» con `reply_parameters.message_id` 6): la risposta entrava, l'avviso al negozio no.
L'istanza è stata tolta durante il livello browser (memoria sotto i 10 GB), senza log dei container.

**Causa (bug del prodotto, non dello scenario né del bot finto):** in `gestrip serve` (`cmd/gestrip/main.go`) il
lettore riceveva come iscrizioni un `buste.Nuove(d, clock.Sistema{})` nudo, **senza la coda** (`ConPosta`) e senza gli
indirizzi. `RispostaTelegram` scriveva il messaggio, poi `avvisaMessaggio` → `avvisaNegozio` trovava `b.posta == nil`
e dava `uscita.ErrNonConfigurato`: nessuna riga per la chat del negozio, solo `repair.message_notice_not_queued`
`not_configured`. I test di dominio non lo vedevano perché costruiscono le buste con la coda (`provaAvvisi`). Il resto
della strada era in regola: il contesto passato è senza annullamento (`context.WithoutCancel`), la configurazione è
usabile (la semina di `gotelegram.go` è quella della pagina), e la chat che il Go manda (`"-4242000"`, l'indirizzo
della riga com'è) corrisponde al filtro `chat=` del bot finto (`fmt.Sprint` dà lo stesso testo per stringa e numero).

**Correzione:** il lettore ha le buste di serve, `ms.buste(d).ConIndirizzoPubblico(…).ConIndirizzoInterno(…)`, le
stesse della faccia web (con la coda, la chiave delle email e gli indirizzi). **Prova:** `TestServeRispostaTelegramAvvisaIlNegozio`
(`cmd/gestrip/telegram_serve_test.go`): `serve` vero con un Bot API finto httptest, una notifica accettata, la risposta
del cliente in `getUpdates`; dopo la conferma del bot, nel database il messaggio del cliente, **una riga per la chat
del negozio** «💬 Messaggio dal cliente — busta 12/26…» e nessuna riga di mancato avviso. Rosso prima della correzione
(`prove/rosso-serve.log`: «messaggi 1, avvisi al negozio 0, mancati avvisi 1»), verde dopo. **Mutazione** S01 (il
lettore torna a `buste.Nuove` nudo, `review/muta-serve.py`, `giro-serve.log`, copia cancellata): presa
dall'asserzione. Il giro del banco va rifatto con i binari nuovi: ci si aspetta 66 scenari, 1164 passi, 0 inattese. `scripts/check` dopo la correzione verde (`prove/check4.log`, «uscita 0»): voci 353: chiuse 38, rosse 0,
parziali 135, aperte 169, fuori v1 11.

**Da fare dalla sessione principale** (qui non lanciato: Docker e memoria di pc-fisso li gestisce la sessione
principale). **Nessuna D nuova nel banco:** i 7 passi nuovi sono `solo_go` con la **D-03**, già approvata, come gli
altri dello scenario; nessun `normalizza` nuovo. Il banco non ha bisogno di D-85/D-86 approvate prima del giro.

Scenario `bench/scenari/telegram-acceso.json` (ora CLI-13, CLI-44, CLI-46, CLI-47, CLI-48, CLI-49, RIP-H-18), dopo il
`sendMessage` della notifica di «pronta» e prima di `/stop`:

1. `POST /repairs/23/risposta` con `reply[body]=Risposta sintetica del negozio` → 302;
2. la scheda: «Risposta inviata. 📲 Notifica Telegram accodata per il cliente»;
3. bot, `?metodo=sendMessage&chat=4242001`, attesa fino a 90 s (la coda): totale 4, `parse_mode` HTML, testo
   «Ciao <b>Cliente</b>! 👋\n\n💬 Hai un nuovo messaggio sulla busta <b>0046/26</b>:\n\n<blockquote>Risposta sintetica
   del negozio</blockquote>\n\n— Negozio», pulsante sul link della busta (CLI-48);
4. bot `{"comando":"rispondi","testo":"Risposta sintetica del cliente"}` → 200 (risponde a quella notifica);
5. bot, `?metodo=sendMessage&chat=4242001`, attesa fino a 30 s: `in_attesa` 0, totale 5, «✅ Messaggio inviato al
   negozio.», `reply_parameters.allow_sending_without_reply` true (CLI-49);
6. bot, `?metodo=sendMessage&chat=-4242000`, attesa fino a 90 s: totale 1, «💬 Messaggio dal cliente — busta
   0046/26\n\nRisposta sintetica del cliente\n\nApri la scheda: /repairs/23» (CLI-13; senza `indirizzo_interno` nel
   Go con Telegram il solo percorso);
7. la scheda `/repairs/23` con tutti e due i messaggi.

Poi `/stop` (ora `?chat=4242001`, totale 6) e il riquadro A come prima. **Cosa ci si aspetta:** HTTP **66 scenari**,
**1164 passi** (1157 + 7), 0 inattese, 0 errori, non provati 0; `telegram-acceso` dura un po' di più (fino a due
giri della coda, circa 30–90 s in più). Browser come prima (17 scenari, 34 viste). `giu` senza avanzi. Comandi come
nella ricevuta 074b (`banco su … -dati sintetici-import`, `banco esegui`, `banco giu`), con i binari nuovi: **anche
il binario `banco` va ricompilato** (il bot finto ha `rispondi` e il filtro `chat`).

Se qualcosa è rosso, da guardare:
- i testi esatti dei passi 3 e 6 sono scritti dalla lettura del codice (nome «Cliente» dalla prima parola del nome
  della busta 23, «0046/26» come nel popup, «Negozio» senza profilo, link senza `indirizzo_interno`);
- passo 5: il lettore deve trovare la notifica già `accepted`; la coda la segna subito dopo la risposta del bot
  finto, e il passo 4 parte solo dopo che il passo 3 l'ha vista arrivare: una gara è possibile solo se la lettura del
  Go cade fra la risposta del bot e la scrittura dell'esito (microsecondi). Se capitasse, il bot dà la risposta
  fissa e il passo 5 cade: rilanciare; se si ripete, è da guardare.

## Controlli

`scripts/check` verde al primo giro (`to-be-deleted-after-use/slice-075/prove/check1.log`, «uscita 0») e di nuovo
dopo le correzioni delle mutazioni (bot finto con il corpo fino a 64 KiB, due test più stretti: `check2.log`, «uscita
0»): gofmt, vet, staticcheck, errcheck, build senza CGO, test, copertura **voci 353: chiuse 38, rosse 0, parziali 135,
aperte 169, fuori v1 11**, segreti e dati veri OK.

Le chiuse passano da 39 (074b) a 38: CLI-50 si chiude (+1, nessuno scenario del banco); CLI-13 e CLI-48 ora sono
anche nello scenario `telegram-acceso` (−2) e, con CLI-49, si chiudono solo con l'esito verde del banco
(`GESTRIP_BANCO_ESITO`), che `scripts/check` qui non ha.

## Per il manuale

- Il cliente che risponde con «Rispondi» a un avviso Telegram scrive nella conversazione della riparazione: il
  messaggio compare nella scheda, la busta diventa «non letta», la chat del negozio riceve l'avviso e il bot risponde
  «✅ Messaggio inviato al negozio.».
- Valgono le regole della pagina (1000 caratteri, dieci messaggi all'ora per busta) più otto al minuto dalla stessa
  chat; solo testo. Riparazione chiusa: «Questa riparazione non riceve più messaggi.»; di un'oreficeria o nel
  cestino: «❌ Riparazione non trovata.».
- Chi ha scritto /stop può ancora rispondere; dopo «Revoca link» o «Genera nuovo link» no, finché non si iscrive di
  nuovo.
- Ogni ora, anche con Telegram spento, si cancellano le iscrizioni spente da più di 90 giorni e i link t.me scaduti;
  gli avvisi più vecchi di 90 giorni perdono la chat. Una busta riaperta o tolta dal cestino non riaccende le
  notifiche.
- Nel registro delle azioni c'è anche il mancato avviso del preventivo (`repair.quote_notice_not_queued`, D-86).
- Il bot fermo da più di una settimana riparte da solo (D-85): niente da fare per il titolare.

## Dubbi

1. **Offset importato al cutover (D-85).** L'offset importato porta l'`aggiornata_il` dello standalone: se fra
   l'ultima lettura dello standalone e la prima del Go passano più di sette giorni, il Go riparte senza offset. È
   proprio il caso per cui c'è la D-85 (Telegram può aver rinumerato); gli aggiornamenti che lo standalone aveva già
   confermato Telegram non li ridà. **Raccomandazione:** niente da fare; una riga nel promemoria del cutover per
   sapere perché il bot «rilegge».
2. **Rilettura con `limit: 1`: D o no?** Raccomandazione: nessuna D (sopra, «D nuove da approvare»).
3. **Un aggiornamento solo oltre 8 MiB** (impossibile con un testo di Telegram, al più 4096 caratteri; solo con
   campi enormi) lascia il bot sordo fino a che Telegram lo lascia cadere (24 ore), come prima della 075.
   **Raccomandazione:** lasciarlo; saltarlo vorrebbe leggere l'`update_id` da una risposta che non si legge.
4. **Il ripristino dal cestino non c'è ancora nel Go:** `TestTelegramNonSiRiaccende` lo simula con
   `cancellata_il = NULL`. Quando arriverà la slice del cestino, nessun trigger deve riaccendere: il test lo
   ricontrolla da solo solo se la slice userà la stessa colonna. **Raccomandazione:** nel brief della slice del
   cestino citare CLI-50.
5. **Errori del database nella risposta:** al terzo tentativo l'aggiornamento si salta e il cliente non riceve
   niente (come lo standalone, dove il `raise` fa lo stesso). Raccomandazione: lasciarlo.
6. **D-85: sette giorni contati da un momento diverso (review della 075, BASSA 1).** Telegram ricomincia a numerare
   dopo una settimana contando dall'**arrivo** dell'ultimo aggiornamento; il Go conta da `aggiornata_il`, cioè da
   quando l'ha **gestito**. Se il server era spento con aggiornamenti in attesa, la differenza arriva a circa 24 ore
   (Telegram tiene i non letti un giorno): in quella finestra il Go ha ancora l'offset vecchio e un aggiornamento
   rinumerato più in basso verrebbe confermato senza essere gestito, e perso. La soglia resta sette giorni
   (decisione di Stefano). **Raccomandazione:** dimenticare l'offset dopo **6 giorni**: è innocuo, perché senza offset
   Telegram ridà solo gli aggiornamenti non confermati, e copre la finestra. Il testo della D-85 nell'inventario lo
   sistema il coordinatore.
7. **Busta tornata dall'oreficeria al cliente diretto (review della 075).** Le risposte della vecchia chat alle
   notifiche di meno di 90 giorni tornano a entrare nella conversazione: `BustaDellaRisposta` taglia solo le chat
   fermate con `motivo_fermata = 'link'`, e il passaggio all'oreficeria le ferma con il motivo `oreficeria`. Lo
   standalone fa lo stesso (`cut_off_chats/1`). **Raccomandazione:** lasciarlo come lo standalone; serve una D solo
   se Stefano lo vuole diverso (per esempio tagliare anche le chat fermate per `oreficeria`).

## Review

Revisore indipendente, 14/11/2026, sul lavoro non committato: **approvabile, nessuna ALTA né MEDIA.** Correzioni nel
repo:

- **BASSA 1 (D-85):** soglia invariata (sette giorni, decisione di Stefano); la frase «al peggio si rileggono le
  ultime 24 ore» è corretta nella D-85 sopra; il punto è il Dubbio 6, con la raccomandazione dei sei giorni.
- **BASSA 2 (R05):** `TestLavoroPuliziaOraria` vuole «pulizia non riuscita» una volta sola in due giri senza che
  l'orologio si sposti (l'ora della pulizia si tiene anche dopo un errore).
- **BASSA 3 (R13):** `TestBustaDellaRisposta` con la tabella delle iscrizioni rinominata: `BustaDellaRisposta` dà
  l'errore e nessuna busta.
- **BASSA 4 (R15):** in `TestAvvisoPreventivoNonAccodato` le buste hanno l'etichetta `RIP-2026-0045`, così
  `bag_number` (il numero salvato) è diverso dal numero dei testi («0045/26») e la riga è controllata contro il primo.
- **BASSA 5 (R22):** `TestRispostaTelegramRifiuti` vuole che una risposta su una busta con il link storico revocato
  passi (come lo standalone: `open/2` con `:telegram` guarda solo chiusura e controparte).
- **BASSA 6:** `TestLettoreTroppoGrandeNon200` (R17: un 502 oltre gli 8 MiB è «telegram:http:502», niente rilettura,
  la lettura dopo è senza `limit`); in `TestLettoreOffsetVecchio` un offset «12a» (non intero, non vuoto) vale come
  nessun offset, come `integer/1` dello standalone (R24). **R06 della review è quasi equivalente**, come dice la review:
  annotata qui, non rifatta.
- **Banco:** al passo 5 di `telegram-acceso` anche `json:ultimo.reply_parameters.message_id` = `6` (il `message_id`
  del messaggio del cliente: il bot finto numera con un solo contatore `/start` (1), benvenuto (2), «✅ Notifiche
  attive» (3), avviso di «pronta» (4), avviso del nuovo messaggio (5), risposta del cliente (6)); al passo 7 la scheda
  ha una riga `data-party="shop"` e una `data-party="customer"`, e i testi delle due righe in ordine
  (`visibile:.conv-testo@1` del negozio, `@2` del cliente). Se il passo 5 cade solo sul `message_id`, la numerazione
  del bot finto è cambiata: da guardare prima di toccare l'attesa.
- **Dubbio 7** (oreficeria e ritorno al cliente diretto): nota, nessun codice cambiato.

Mutazioni della review rifatte sulle correzioni (`to-be-deleted-after-use/slice-075/review/`: `muta.py`, `giro.log`,
`esiti/<id>.log`; copia cancellata a fine giro), nella forma descritta dalla review:

| # | Mutazione | Esito |
|---|---|---|
| R05 | l'ora della pulizia non si tiene dopo un errore (riprova a ogni giro) | presa (`TestLavoroPuliziaOraria`) |
| R13 | l'errore delle chat tagliate ignorato (la busta passa) | presa (`TestBustaDellaRisposta`) |
| R15 | `bag_number` del preventivo con il numero dei testi | presa (`TestAvvisoPreventivoNonAccodato`) |
| R17 | un codice diverso da 200 troppo grande entra nel modo `limit: 1` | presa (`TestLettoreTroppoGrandeNon200`) |
| R22 | il link storico revocato rifiuta anche la risposta Telegram | presa (`TestRispostaTelegramRifiuti`) |
| R24 | un offset non intero preso per zero | presa (`TestLettoreOffsetVecchio`) |

**6 prese su 6**, tutte da un'asserzione. R17 al primo giro (`giro1.log`) era viva nella forma «`grande` vero anche
fuori dal 200»: la guardia era doppia (anche i rami dei codici diversi da 200 davano `grande` falso), quindi la
mutazione era equivalente. Il codice ora ha una guardia sola (i rami non 200 rimettono `grande` a falso, con un
commento) e R17 muta proprio quella: presa (`giro2.log`).

Dopo le correzioni `scripts/check` è verde (`prove/check3.log`, «uscita 0»): **voci 353: chiuse 38, rosse 0, parziali 135,
aperte 169, fuori v1 11**, segreti e dati veri OK.
