# 091 — «📤 Chiudi e invia» del lotto d'avviso (T4.9, sesta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/ready_lots.ex` (`send_lot/2`, `mark_sent/2`,
  `send_refusal/2`, `open_lot_refusal/2`, `members/2`, `announce_members/3`, `send_error/1`, `batch_message/2`,
  `sent_batch/1`, `public_link/1`, `refusal_message/1`, `member_refusal_message/1`), `ready_notice.ex` (`announce/3` con
  `link:` e `chosen_contact: true`, `recipient/3`, `destination/3`, `link_into/4`, `notify/5`, `claim/3`,
  `claim_refusal/2`), `outgoing.ex` (`chosen_contact/0`, `destination_for_chosen_contact/1`),
  `ready_lot_live.ex` (evento `send`, `send_refusal/1`, `reload_lot/1`, i riquadri `#ready-lot-send` e
  `#ready-lot-sent`), `assets/js/wa_home.js` (slice 225); test `ready_lots_test.exs` («sending a lot») e
  `ready_lot_live_test.exs` («sending the lot», «the batch send and the bag links (slice 104)»).
- **Voci:** ORE-10 (chiusa con il giro verde s091b). Nessuna migrazione. Deviazioni: D-74 (link wa.me del lotto
  inviato), D-46 estesa (senza `indirizzo_pubblico`, decisa anche per il lotto da Stefano il 17/11/2026), D-94
  (transazione `BEGIN IMMEDIATE`), **D-99** (Stefano, 17/11/2026: il numero visibile nel rifiuto di una busta).

## Cosa c'è

- **Dominio** (`internal/buste/lotto_invio.go`, nuovo): `InviaLotto(ctx, id, base, contesto)` = `send_lot/2`, **in una
  transazione di scrittura**:
  1. il lotto con l'oreficeria (che non c'è → «Lotto non disponibile.»);
  2. **una scrittura che decide**: `UPDATE lotti_avviso SET inviato_il, accesso_pubblico_spento_il = NULL` solo se aperto,
     con almeno una busta fuori dal cestino e **nessuna** busta (cestino compreso) di un'altra oreficeria o di nessuna
     (`oreficeria_id IS NULL OR != …`, come il `NOT EXISTS` di `mark_sent/2`). Se non prende niente, il motivo dopo
     (`rifiutoInvio`): inviato o chiuso «Lotto già inviato.», poi la busta più recente di un'altra oreficeria («Busta
     #0045/26: non è più di questa oreficeria. Niente è partito.», D-99), altrimenti «Lotto vuoto: aggiungi almeno una busta.»;
  3. il testo del lotto `wa_oreficeria_lotto` (`{oref_nome}`, `{n_buste}`, `{link}`, `{negozio}`), **prima** delle
     buste: un testo salvato che non si compone è `ErrModelloLotto` e vince su quello delle buste;
  4. ogni busta fuori dal cestino, dalla più recente (`idMembri`), con `avvisaNelLotto` = `announce/3` con il link del
     lotto e il contatto scelto: la busta riletta nella transazione, il testo della sua famiglia d'avviso («Avviso
     oreficeria», `famigliaPer`), `{link}` → il link del lotto (nessun link della busta: niente token nuovo né
     `repair.public_link_issued`, slice 104 dello standalone), l'indirizzo `NumeroWhatsApp` dell'oreficeria o
     `contatto-scelto` (nessun telefono, nessuna cifra, cifre non ASCII: slice 196), poi **la stessa porta
     dell'avviso singolo**: `avvisaRitiroIn`, la parte in transazione di `avvisaRitiro` (estratta, senza copie: blocco
     `condizioneDaAvvisare`, riga WhatsApp `accepted`, `outgoing.handed_off`, evento «notified», `ready_notice.sent`).
     Il blocco che rifiuta diventa `*RifiutoMembro` con il numero visibile della busta (D-99, `rifiutoMembro`, letto anche
     dal cestino) («non è più pronta», «è già stata avvisata»; «è in un altro lotto
     ancora aperto» e «non esiste più» ci sono ma qui non succedono);
  5. `ready_lot.sent` con `{"bags": N}` e il commit. Ogni errore prima del commit annulla tutto (gancio di prova
     `dopoAvvisoNelLotto` dopo ogni busta).
  L'esito `LottoInviato`: il lotto, quante buste, il testo, il numero (`""` = contatto da scegliere), il link
  (`indirizzo_pubblico + /lotto/<gettone>`, mai quello della richiesta). Senza `indirizzo_pubblico` un testo (del lotto
  o delle buste) con `{link}` è `ErrSenzaIndirizzoPubblico` prima di ogni scrittura (D-46 estesa); senza `{link}` il
  lotto parte e il link è il solo percorso, come la pagina del lotto uscito della 089.
- **`LottoAvviso.Telefono`**: il telefono dell'oreficeria, per l'avviso «senza numero» prima dell'invio.
- **Pagina** (`internal/web/lotto_invio.go`, `pagine/lotti.html`, `app.css` `.lotto-avviso`, `.lotto-inviato`,
  `.lotto-testo`, `.lotto-pulsanti`): dopo «Buste nel lotto», solo a lotto aperto con una busta visibile,
  `#ready-lot-send` («Chiudi e invia», la frase con l'oreficeria, modulo `POST /ready-lots/{id}/invia` con
  `data-invio-unico` e `#ready-lot-send-button` «📤 Chiudi e invia» con la domanda dello standalone in `data-confirm`,
  `#ready-lot-send-no-number` senza un numero). La rotta (`rottaConGuscio`, `personale`, l'evento `send`):
  - riuscita: la pagina (**200, non un redirect**: il testo pronto si vede una volta, come `@sent` della LiveView) con
    «Lotto inviato: apri WhatsApp per mandarlo all'oreficeria.», il badge, niente aggiunte/candidate/«Rimuovi»/invio, e
    `#ready-lot-sent`: «Lotto inviato», la frase, il testo in `<pre id="ready-lot-sent-message">`, «Apri WhatsApp»
    (`#ready-lot-sent-whatsapp`, solo con un numero), «👥 Scegli contatto» (`#ready-lot-sent-choose`, sempre,
    `wa.me/?text=`), `#ready-lot-sent-no-number` senza numero, «Link pubblico: …». Al posto di «Questo lotto è partito
    il …», che torna riaprendo la pagina;
  - rifiuto: la pagina ridisegnata con il lotto riletto (`ridisegnaLotto`) e l'errore: i `Rifiuto`, `*RifiutoMembro`,
    «Il testo «<etichetta>» non è valido: <avviso>. Niente è partito.», «Lotto non inviato: manca «indirizzo_pubblico =
    …» in <file>, poi riavvia … Niente è partito.» (D-46 estesa, testo solo del Go);
  - un lotto che non c'è o un id non valido: la lista con «Lotto non trovato.»; guasto: la pagina d'errore con «lotto
    non inviato» nel registro.
- **D-74** (`app.js`, nell'IIFE del popup, accanto a `lista()`): un clic su `a[data-wa-lista]` («Apri WhatsApp», «👥
  Scegli contatto») lascia aprire la scheda nuova e porta la pagina alla lista ricordata (`gestrip:list-return`), come
  `wa_home.js` dello standalone; le scritture sono già fatte quando i link si vedono (come il pannello QR della 068).
- **Tabelle:** permessi (`ruoli_test.go`, ORE-10), rotte con il guscio (`guscio_test.go`), interfaccia `Buste`
  (`nuova.go`) e `busteFinte`.
- **SVILUPPO:** punto «Chiudi e invia del lotto» nelle rotte dopo lo scanner, D-74 (punti fatti), popup (link wa.me).

## Scelte

1. **Una transazione per tutto**, con la porta dell'avviso singolo riusata (`avvisaRitiroIn`) e non copiata: lo
   standalone annida la transazione di `notify/5` dentro quella del lotto, il Go passa la stessa `tx`.
2. **Il testo delle buste ricomposto nella transazione**, leggendo la busta con `tx`; modelli e profilo dalla connessione
   di lettura (come `AnnunciaRitiro`): in WAL leggono l'ultimo stato confermato e non bloccano.
3. **Testi registrati senza spazi ai bordi** (`TrimFunc`, come `AnnunciaRitiro`); il testo del lotto mostrato com'è.
4. **Risposta 200 e non PRG** dopo l'invio: con un redirect il testo pronto si perderebbe o andrebbe tenuto in sessione;
   lo standalone lo mostra una volta sola (un ricaricamento della LiveView lo perde). Un «ricarica» del browser rimanda
   il modulo: «Lotto già inviato.» con la pagina in sola lettura, niente di doppio.
5. **Guasto = pagina d'errore** (lo standalone mostra «Invio non riuscito: niente è partito.»), come la scelta 7 della
   089.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-091/prove/rosso.txt`): 9 test del dominio con
  `InviaLotto` in stub, 8 della pagina, `TestRotteConGuscio`, `TestOgniRottaHaIlSuoPermesso` (rotta non registrata).
- **Dominio** (`internal/buste/lotto_invio_test.go`): `TestInviaLotto` (testi esatti del lotto e delle righe, ordine
  dalla più recente, numero, link, lotto `inviato_il`/link riacceso, busta nel cestino non avvisata, nessun link della
  busta, audit con attore e `{"bags":2}`, l'ultima riga è `ready_lot.sent`, membri ancora nel lotto),
  `TestInviaLottoRifiutiDelLotto` (che non c'è, vuoto, solo cestino, inviato, chiuso, due volte; niente scritto),
  `TestInviaLottoRifiutiDelleBuste` (8 casi: non pronta, consegnata, già avvisata, avvisata e non pronta, altra
  oreficeria, nessuna, tutte e due altrove → la più recente, altrove nel cestino; testo, niente scritto, poi parte),
  `TestInviaLottoModelloNonValido` (lotto, tutti e due → lotto, buste), `TestInviaLottoSenzaNumero` (NULL, vuoto, «da
  chiedere», «³⁴⁷»), `TestInviaLottoSenzaIndirizzoPubblico` (tre combinazioni con `{link}` rifiutate, senza `{link}` il
  percorso), `TestInviaLottoGuastoAMeta` (errore dopo la seconda busta: tutto annullato, ordine visto),
  `TestInviaLottoGara` (12 insieme: 1 inviato, 11 «già inviato»), `TestInviaLottoPoiAvvisoSingolo` (prima «in lotto
  aperto», dopo «già avvisata», azzerato riparte, resta del lotto).
- **Pagina** (`internal/web/lotto_invio_test.go`): `TestRiquadroChiudiEInvia` (markup esatto, ordine, avviso senza
  numero per tre telefoni, assente vuoto/cestino/inviato), `TestInviaLottoDallaPagina` (200, markup esatto del
  riquadro con i due wa.me, sola lettura, riga e audit, riaperta, scheda, senza token 403),
  `TestInviaLottoSenzaNumeroDallaPagina`, `TestRifiutiDellInvioDelLotto` (vuoto, busta non pronta con l'id, testo,
  già inviato, lotto che non c'è), `TestInviaLottoSenzaIndirizzoPubblicoDallaPagina`, `TestInviaLottoDelTitolare`,
  `TestInviaLottoGuasto`, `TestScriptAperturaWhatsAppDelLotto`.
- **Ritoccati:** `TestRotteConGuscio`, la tabella dei permessi.

## Banco

**Giri della sessione principale (17/11/2026).** s091a rosso solo per il «database is locked» dello standalone (nota 8,
quarta comparsa: `testi-invii` PC, 1 errore e 57 inattese dello standalone, HTTP verde, Go senza 5xx). s091b, giro
identico, **verde**: HTTP 81 scenari, 0 inattese, 0 errori; browser 28 scenari, 56 viste, 42 differenze attese (fra cui
le due D-99), 0 inattese, 0 errori; `lotti-invio` su PC e telefono nelle due app con `fine.ripristino` «ok». Memoria
disponibile minima 18,8 GB. Copertura con l'esito: 353 voci, chiuse 197 (ORE-10 chiusa), rosse 0, parziali 16, aperte
129, fuori v1 11.

- **Browser `lotti-invio`** (nuovo, `bench/browser/lotti-invio.json`, ORE-10), dopo `lotti-buste` e prima di
  `pannello-qr` (ordine dei nomi). Sul database vuoto niente. Con i dati, per vista: oreficeria propria «Zz Banco 091
  <vista>» con telefono, tre buste pronte del 2025 (8101-8103/25 PC, 8104-8106/25 telefono, QR
  `SINT-LOTTO-091-<vista>-A…C`); salva «Avviso lotto pronte oreficeria» e «Avviso oreficeria» **senza `{link}`** (il Go
  del banco non ha `indirizzo_pubblico`: D-46 estesa) e li rimette predefiniti alla fine anche dopo un errore. Poi: il
  lotto con A e B, il riquadro; una seconda scheda sul lotto, A «In lavorazione» dalla prima, invio dalla seconda →
  «Busta #ID: non è più pronta. Niente è partito.» nello standalone e «Busta #N: …» nel Go (D-99, due righe in
  `bench/deviazioni.json`; `rifiuto.busta` controlla che il testo nomini A); A di nuovo pronta; invio
  dalla prima (domanda, messaggio, riquadro, testo, i due wa.me decodificati, link ridotto a `/lotto/<gettone>`, sola
  lettura); invio dalla seconda → «Lotto già inviato.» con «Questo lotto è partito il»; «Apri WhatsApp» → lista e una
  apertura wa.me; riaperto il lotto, la frase «partito il <data>»; telefono tolto da «Anagrafiche», lotto nuovo con C,
  avviso prima/dopo, niente «Apri WhatsApp», «👥 Scegli contatto» → lista; la scheda di C con «contatto scelto sul
  telefono»; registro `ready_lot.sent` 2, `ready_notice.sent` 3, `outgoing.handed_off` 3.
- **Ritoccati:** `anagrafica-oreficerie` telefono `pagina.righe` 7 → **8** (la «Zz Banco 091 desktop»);
  `lotto-pagina` (HTTP) `visibile:ready-lot-send-button` e `visibile:ready-lot-sent` assenti sul lotto 1 inviato, con e
  senza dati, e ORE-10 fra le voci.
- **Niente HTTP dei rifiuti dell'invio:** nello standalone l'invio è un evento LiveView, e un passo `solo_go` chiede una
  D approvata che qui non c'è; i rifiuti li coprono i test Go e, nelle due app, il browser (busta non pronta, già
  inviato).
- **Se qualcosa è rosso, da guardare per primo:** (a) `invia.testo`, `invia.whatsapp`, `invia.scegli` non hanno
  un'attesa (contengono il nome del negozio del profilo, che `profilo-negozio` del PC cambia prima del telefono): si
  confrontano solo fra le app; (b) `dopo.partito` normalizza lo spazio prima del punto (nello standalone il link è
  dentro un `<a>` su più righe); (c) un errore a metà lascia i due testi predefiniti solo se il ripristino riesce
  (`.catch`): controllare `/settings/templates` prima del giro dopo; (d) `rifiuto.*`: la seconda scheda dello
  standalone deve essere collegata (LiveView) quando A cambia stato; (e) gli scenari dopo questo vedono tre buste del
  2025 avvisate e due lotti inviati in più: nessuno li legge (liste in fondo, lotti aperti di oggi per oreficeria).

## Per il manuale

- Nella pagina del lotto, sotto le buste, c'è «Chiudi e invia» con il pulsante «📤 Chiudi e invia». Prima di inviare
  GestRip chiede conferma: dopo l'invio ogni busta del lotto risulta avvisata e il lotto non si può più cambiare.
- Dopo l'invio la pagina mostra il testo pronto per l'oreficeria, «Apri WhatsApp» (apre la chat dell'oreficeria con il
  testo già scritto) e «👥 Scegli contatto» (apre WhatsApp per scegliere a chi mandarlo, per esempio un collega). GestRip
  non manda niente da solo: il messaggio parte quando lo mandi da WhatsApp. Toccato uno dei due, GestRip torna alla
  lista delle riparazioni.
- Se l'oreficeria non ha un numero WhatsApp in «Anagrafiche», la pagina lo dice prima e dopo: il lotto parte lo stesso e
  il contatto si sceglie con «👥 Scegli contatto».
- Se una busta del lotto non è più pronta, è già stata avvisata o è passata a un'altra oreficeria, non parte niente e la
  pagina dice quale busta e perché: si sistema (o si toglie dal lotto) e si riprova.
- Il link nel messaggio porta alla pagina pubblica del lotto (con le foto). Riaprendo il lotto si legge quando è partito
  e il link.

## Dubbi

1. **Senza `indirizzo_pubblico` il lotto non parte (D-46 estesa).** Lo standalone ha sempre l'host; il Go, come per il
   popup (067) e il pannello QR (068), rifiuta un testo con `{link}` prima di scrivere, con «Lotto non inviato: manca
   «indirizzo_pubblico = …» … Niente è partito.» (testo solo del Go). Senza `{link}` nei due testi il lotto parte con il
   solo percorso. **Raccomandazione:** tenere (è la D-46 estesa già approvata, nessuna D nuova); in produzione
   `indirizzo_pubblico` c'è. Nel banco obbliga lo scenario a salvare i due testi senza `{link}`. **Deciso da Stefano il
   17/11/2026: la D-46 estesa vale anche per l'invio del lotto.**
2. **«Busta #<id>» nel rifiuto di una busta (misurato nel sorgente, ragionato sull'effetto).** Lo standalone scrive
   l'**id** della busta (`ready_lot_live.ex:137-139`, `"Busta ##{repair_id}: …"`, e il test lo vuole), mentre «Rimuovi»
   scrive il numero (`"Busta ##{repair.bag_number} rimossa dal lotto."`) e la pagina del lotto mostra il numero
   dell'etichetta (D-64). L'operatore legge «Busta #137: non è più pronta» e cerca una busta 137 che non c'è. Il Go per
   ora **copia** (l'id), con il segnaposto nel codice. **Raccomandazione:** D nuova (D-99), testo proposto: «Il rifiuto
   dell'invio del lotto per una busta nomina il numero visibile della busta (D-64, «Busta #0045/26: non è più pronta.
   Niente è partito.») al posto dell'id interno dello standalone.» Nel banco la riga `rifiuto.errore` diventerebbe una
   differenza attesa (o un normalizzatore D-64). **Deciso da Stefano il 17/11/2026: D-99 approvata e fatta** (il Go
   scrive il numero visibile; due righe D-99 in `bench/deviazioni.json`).
3. **Senza JavaScript nessuna conferma prima dell'invio** (irreversibile). `data-confirm` è solo JavaScript, come per gli
   altri pulsanti del Go; lo standalone senza JavaScript non funziona affatto. **Raccomandazione:** lasciare così (la
   PWA ha JavaScript); se si vuole, la conferma dal server come «✓ Consegnata» della 060, in lista idee. **Deciso da
   Stefano il 17/11/2026: niente per la v1, fra le idee per dopo (PIANO, «Idee per l'interfaccia»).**
4. **Motivi che nell'invio non succedono** («è in un altro lotto ancora aperto», «non esiste più», «il link del cliente
   non si è potuto creare»): l'inventario li elenca, ma nel lotto segnato inviato le buste non sono in un lotto aperto,
   le buste nel cestino non si avvisano e nessun link della busta si emette (slice 104). Il Go ha i primi due mappati
   per sicurezza, il terzo no. **Raccomandazione:** solo nota.
5. **Ricarica dopo l'invio.** La risposta è la pagina del POST: un «ricarica» rimanda il modulo e dà «Lotto già
   inviato.» (il testo pronto non si rivede: lo ridarà il reinvio della 093). Nello standalone una ricarica perde allo
   stesso modo il riquadro. **Raccomandazione:** nessun cambio.

## Revisione

Revisore indipendente: nessun rilievo ALTA; uno MEDIA e tre BASSI, chiusi. MEDIA: il ripristino dei due testi in fondo a
`lotti-invio` non si nasconde più (osservazione `fine.ripristino`, attesa «ok»). BASSI: la risorsa `ready_lot` della riga
`ready_lot.sent` controllata in `TestInviaLotto`; `TestInviaLottoBloccoDecide` (gancio `primaDelBloccoNelLotto`: busta
cestinata, non più pronta o avvisata fra la lettura e il blocco → «non esiste più», «non è più pronta», «è già stata
avvisata», niente scritto); `TestRifiutoDellInvioRileggeIlLotto` (un invio di un collega sotto la pagina: il rifiuto
ridisegna il lotto riletto). I tre mutanti della revisione, rifatti a mano, ora muoiono. SVILUPPO: la porta del lotto
nel punto della D-46 estesa. Il testo «Busta #id» è poi diventato il numero visibile con la D-99 (17/11/2026).
