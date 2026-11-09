# 063 — Conversazione della pagina del cliente e della scheda (T4.5, terza slice)

- **Fonte:** coordinatore, 09/11/2026; T4.5 del PIANO. Lo standalone a `go-baseline` = `302439d`.
- **Voci:** CLI-10 (conversazione sulla pagina), CLI-11 (poll), CLI-12 (messaggio del cliente), CLI-15
  (campanella, ferma), RIP-S-19 (conversazione nella scheda), RIP-S-20 e CLI-14 solo per la riga nella
  conversazione. **Non fatte:** CLI-13 (Telegram al negozio) e gli avvisi dopo la risposta del negozio
  (popup WhatsApp, Telegram al cliente): vanno con i canali (T4.6, T4.7).
- **Deviazioni:** D-64 (numero nella campanella). Nessuna nuova.
- **Lavoro:** un sottoagente, con mutazioni a mano. Nessuna migrazione (tabelle già nella 0008).

## Cosa c'è

- **Dominio** (`internal/buste/conversazione.go`), copia di `RepairMessages`, `PublicMessage`,
  `RepairMessage` e `Notifications.summary/0`:
  - conversazione pubblica (`public_thread/2` con il link `:token`): dal più vecchio (istante, poi id),
    da `messaggi_visibili_dal` in poi; nascosta su consegnata, annullata, chiusa; il lato si legge alla
    lettura (`shop`, altrimenti `oreficeria` su una B2B, `customer`). `PaginaCliente` ha
    `ConConversazione` e `Conversazione`; `Scheda` ha `Messaggi` (tutta la conversazione);
  - `ConversazioneCliente(token)` per il poll: link morto, cestino e conversazione nascosta sono un
    solo `ErrBustaNonTrovata`; non scrive;
  - `ScriviMessaggioCliente`: trim Unicode, vuoto / oltre 1000 code point rifiutati; nella stessa
    transazione (immediata) la busta riletta, chiusa = `ErrConversazioneChiusa`, il limite di 10
    messaggi web della controparte in un'ora (dal cambio di controparte se è dopo), la riga, i due
    istanti (`messaggi_letti_il` a NULL: non letto per il negozio) e `repair.message_appended` con
    `bag_number`, `author`, `channel`, `message_id`, mai il testo. Dopo il commit
    `repair.message_notice_not_queued` con `reason: not_configured` (vedi Dubbi);
  - `RispondiAlCliente`: stesse regole del testo, riga del negozio già letta, anche su busta chiusa;
  - `LeggiCampanella`: i tre conteggi (messaggi non letti, approvazioni e rifiuti non letti, cestino
    escluso, nessuna esclusione per stato), le cinque più recenti per istante e id.
- **Pagine** (`internal/web/conversazione.go`, modelli, `app.js`, `app.css`):
  - `GET /r/{token}/poll`: JSON `{"messages":[{id,at,body,party,side}],"ts":…}` nell'ordine dello
    standalone, no-store e no-referrer; ogni rifiuto è il 404 della pagina (contato dal limite);
  - `POST /r/{token}/messaggio` con `rottaPubblica`: redirect a `#public-thread` con l'esito nel
    riquadro (nuovi flash `InfoConv` / `ErroreConv`, come `:thread_info` e `:thread_error`), busta
    chiusa con la frase in `public-flash-error`, link morto 404; i testi sono quelli dello standalone;
  - pagina: `#public-thread` sotto le foto con righe, «Ancora nessun messaggio…», modulo, riga
    `#public-thread-feedback` e i `<template>` per lo script; lo script fa poll ogni 30 s, bip 660 Hz,
    notifica, evidenzia, invio del modulo con `fetch`, stop al primo 404 (come `public_thread.js`);
  - scheda: `#repair-thread` («💬 Conversazione con il cliente / l'oreficeria», negozio a destra, segni
    🏪 📲), e `#repair-reply` («Rispondi al cliente», «Invia 🏪», nota sulla busta chiusa);
    `POST /repairs/{id}/risposta` per titolare e dipendente, «Risposta inviata.» e i tre rifiuti;
  - campanella in ogni pagina del personale (`#nav-bell-wrap`, `#nav-bell`, `#nav-bell-menu`, voci con
    «oggi / ieri / gg/mm», «Vedi tutte →» a `/?notifiche=1`), menu in un `<details>`, il numero davanti
    al titolo della scheda del browser.
- **Filtro «notifiche» della home:** usava già `ultimo_messaggio_il` / `messaggi_letti_il`; il segno
  «letto» all'apertura della scheda c'era già (`segnaMessaggiLetti`). Provato con un messaggio vero.
- **Banco:** passo `token` con 27/26 (messaggio del cliente non letto) e 28/26 (messaggio del negozio);
  scenario `pagina-cliente-conversazione` (pagine, poll, poll su consegnata e revocato, 403 senza CSRF,
  vuoto, troppo lungo, inviato, limite, busta consegnata, link revocato). I messaggi vanno sulla 27/26,
  già non letta: i contatori della home non cambiano.

## Prove

- Test Go del dominio: ordine e lato, `messaggi_visibili_dal` compreso, B2B, vuota, stati chiusi e
  aperti, un'altra busta mai visibile; poll con token vuoto, inventato, revocato, scaduto all'istante,
  cestino, senza audit; messaggio (vuoto, spazi Unicode, 1000 esatti con bordi, 1001 e 1002, riga,
  segni, versione invariata, audit esatto, testo mai nel registro, campanella), busta chiusa, link
  morto; limite (fuori finestra, negozio, WhatsApp non contano; il bordo dell'ora conta; cambio di
  controparte; 12 insieme → 10); risposta del negozio; scheda con il segno letto; campanella.
- Test web: pagina vuota e con righe (escape, ordine, B2B, chiuse), poll (JSON esatto, intestazioni,
  404, nessun audit, token mai nel registro, limite), messaggio (403, esiti una volta, i testi, filtro
  notifiche, limite, chiusa, 404 anche con `a%2Fb`), scheda (riquadro, risposta, rifiuti, nota, B2B),
  campanella nel guscio; tabella dei permessi e segni dei moduli aggiornati.

## Mutazioni

21 a mano (`to-be-deleted-after-use/slice-063/mutazioni.py`): limite `>`, WhatsApp contato, limite
senza cambio di controparte, cliente scritto letto, negozio non letto, chiusa che accetta, pagina chiusa
con conversazione, messaggi di un'altra busta, `visibili_dal` escluso, 1000 escluso, senza trim, poll
sulla nascosta, testo nell'audit, avviso mancato assente, campanella con il cestino, ordine a pari
istante, flash che non si toglie, chiusa nel riquadro invece che in cima, `ts` della prima riga, parte
B2B persa. **Tutte uccise.**

## Banco

Istanza `s063`, `fab-225`, `-dati sintetici-import`. Primo giro: catena verde (`GR_TOKEN:ok` con i due
token nuovi), HTTP 2 inattese, tutte e due sul poll: lo standalone manda le chiavi nell'ordine `id, at,
body, party, side` (corretto nel Go) e il `ts` di un poll dopo una scrittura dipende dall'istante
(artefatto: tolto `forma-json` da quel passo, restano codice, tipo e testo).

Secondo giro, su un'istanza nuova (`banco giu`, `su`, `esegui`): **BANCO VERDE**, 53 scenari e 993
passi, 1511 differenze attese, **0 inattese**. Browser: 2 inattese, tutte sul titolo della pagina
della firma: il Go mette il numero della campanella («(1) ») davanti al titolo al caricamento, lo
standalone con l'hook `BellTitle` dopo la connessione di LiveView, quindi ce l'ha o no secondo il
momento (nello stesso giro una vista sì e una no). Aggiornate le due voci D-64 di `deviazioni.json`
(e l'elenco di `browser_test.go`) con le due forme dello standalone; il confronto rifatto con
`banco confronta-browser` sulle osservazioni di quel giro: **BANCO BROWSER VERDE** (12 scenari, 24
viste, 0 inattese). Istanza tolta con `banco giu`. Giro sul database vuoto non fatto (nessuna migrazione).

## Controlli

`scripts/check` verde; `scripts/parole-vietate --staged` verde (dopo `git add -A`, poi `git reset`).

## Per il manuale

- Capitolo del cliente: «💬 Lascia un messaggio», limiti (1000 caratteri, 10 all'ora), aggiornamento
  ogni 30 secondi con bip e notifica, niente conversazione su buste chiuse; nella scheda il riquadro
  della conversazione, «Rispondi al cliente» senza avvisi per ora; la campanella. In
  `docs/manuale/da-scrivere.md`.

## Dubbi

1. **CLI-13 e avviso mancato:** senza canale Telegram il Go scrive, come lo standalone con Telegram non
   configurato, `repair.message_notice_not_queued` con `reason: not_configured` a ogni messaggio del
   cliente. Quando arriva Telegram (T4.6) la riga diventa l'accodamento. Da confermare che va bene
   scriverla già adesso.
2. **Risposta del negozio senza avvisi (RIP-S-20, CLI-14):** lo standalone dopo «Invia 🏪» apre il popup
   WhatsApp o accoda il Telegram al cliente; qui solo «Risposta inviata.». Popup e Telegram vanno con
   T4.6 / T4.7. Su un rifiuto (vuota, troppo lunga) il Go non rimette il testo nel campo (lo standalone
   sì, LiveView); il `maxlength` del campo rende raro il secondo caso.
3. **Niente tempo reale nella scheda e nella campanella:** lo standalone aggiorna la scheda aperta e la
   campanella in diretta (PubSub) e mostra il toast di sei secondi; il Go le legge a ogni pagina (come
   `put_bell/2` delle sue pagine di controller). Un messaggio arrivato con la scheda aperta si vede (e si
   segna letto) ricaricando.
4. **Icona del negozio nella conversazione:** lo standalone usa `/branding/logo.jpg`; il Go non ha
   ancora il logo e usa l'icona `/branding/icon-192.png` (stesso alt, il nome del negozio).
5. **Campanella sulle pagine:** il Go la mette in ogni pagina interna con un utente; il numero è quello
   visibile (D-64, «#0045/26» o «N. 45/26») dove lo standalone scrive `#<numero interno>`.
6. **Poll e `Accept`:** lo standalone passa dal plug `accepts ["json"]` (un `Accept` senza JSON né `*/*`
   avrebbe 406); il Go risponde comunque. Il browser manda `*/*`.
7. **Titolo con la campanella:** il «(1) » davanti al titolo è dello standalone (`bell_title.js`); sul
   banco browser la sua comparsa nello standalone dipende dal momento, e la deviazione lo accetta nelle
   due forme.
8. **Corsa trascurabile:** la rilettura nella transazione esclude anche una busta finita nel cestino un
   istante dopo la ricerca del token (lo standalone la rilegge con `Repo.get` e la accetterebbe).

## Review


Revisore, 10/11/2026 (pc-fisso, slice non committata).

- **MEDIA, corretto:** `ScriviMessaggioCliente` guardava il testo prima del link: un POST su un link
  morto (revocato, scaduto, inventato) con testo vuoto o oltre 1000 caratteri dava il redirect con
  l'esito nel riquadro invece del 404 (e il 404 non veniva contato dal limite). Lo standalone risolve
  prima il link (`with_public_repair/3`). Ora il link viene prima; `TestMessaggioDelCliente` prova
  vuoto e troppo lungo su tre link morti, `TestPaginaClienteMessaggio` il 404 con il testo vuoto.
  Rimettendo l'ordine vecchio tutti e due i test diventano rossi.
- **Copertura 35 → 32: non è un errore.** Senza `GESTRIP_BANCO_ESITO`, `HEAD` ha 28 voci chiuse, la
  slice 32 (+CLI-14, CLI-15, RIP-S-19, RIP-S-20). CLI-10, 11 e 12 hanno lo scenario
  `pagina-cliente-conversazione` e restano parziali finché non si passa l'esito del banco: 32 + 3 = 35
  è il conteggio con il banco verde.
- **BASSA, da decidere (non toccato):** CLI-14 e RIP-S-20 risultano chiuse, ma l'avviso dopo la
  risposta (popup WhatsApp, Telegram) manca (Dubbio 2). Anche CLI-15 risulta chiusa, ma solo nella
  versione ferma, senza aggiornamento in diretta (Dubbio 3). Lo strumento chiude una voce quando ha
  test verdi: serve un modo per segnarle parziali fino a T4.6 e T4.7.
- **BASSA:** la campanella aggiunge sei letture su `buste` (tre conteggi e tre «ultime 5») a ogni
  pagina del personale, senza indici dedicati: va bene con i volumi di un negozio. Ruoli giusti:
  qualunque utente del personale, come `live_session :authenticated` e `:owner`.
- **Verificato senza rilievi:** CSRF (`rottaPubblica` e `rotta`), limite di 10 messaggi all'ora e
  riga nella stessa transazione immediata (`_txlock=immediate`), 1000 code point, testo mai
  nell'audit né nei log (solo frasi fisse). Escaping: modelli con `html/template`; lo script ridisegna
  solo con `textContent` e `importNode` della pagina già escapata, `app.js` non usa mai `innerHTML`;
  il JSON del poll ha `nosniff`. Poll: stesso 404, nessuna scrittura, filtro per busta del token e
  `messaggi_visibili_dal`. «Rispondi»: titolare e dipendente, con CSRF.
- `scripts/check` e `scripts/parole-vietate --staged` verdi dopo la correzione.
