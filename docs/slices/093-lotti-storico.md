# 093 — Storico dei lotti inviati, dettaglio e reinvio (T4.9, ottava)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/ready_lot_history_live.ex`
  (`mount/3`, `load/2`, eventi `prepare-resend` e `open-resend`, `refusal/1`, `resendable?/1`, `collected?/1`,
  `truncate/1`, i due `render/1`), `lib/gestrip_standalone/ready_lots.ex` (`list_sent_lots/0`, `fetch_sent_lot/1`,
  `members/1`, `public_members/1`, `resend_message/1`, `lot_destination/1`, `record_resend/4`, `resend_members/1`,
  `resendable_lot/2`, `batch_message/2`, `public_link/1`, `refusal_message/1`), `outgoing.ex` (`handoff_multi/3`,
  `destination_for_lot/1`, `destination_for_chosen_contact/1`, `dial/1`), `ready_lots_live.ex` (il link allo storico),
  `router.ex:261-262`; test `ready_lots_test.exs` («the history of sent lots») e `ready_lot_live_test.exs` («the history
  of sent lots», «resending a sent lot», «the batch send and the bag links (slice 104)»).
- **Voci:** ORE-11, ORE-12, ORE-13. Nessuna migrazione. Deviazioni usate: D-64 (numero visibile delle buste), D-74 (il
  tocco su WhatsApp registra e porta la pagina alla lista), D-46 estesa (senza `indirizzo_pubblico` un testo con
  `{link}` non si prepara). **Una D-nuova da approvare** (Dubbi, 1).

## Cosa c'è

- **Dominio** (`internal/buste/lotti_storico.go`, nuovo):
  - `StoricoLotti` = `list_sent_lots/0`: lotti con `inviato_il`, chiusi compresi, `ORDER BY id DESC LIMIT 100`, con
    l'oreficeria, le buste fuori dal cestino e quelle non consegnate (due sottoquery: «3 e 2», mai «2 e 2»);
  - `LottoDelloStorico` = `fetch_sent_lot/1`: il lotto solo se inviato, altrimenti `ErrLottoNonNelloStorico` («Lotto non
    trovato nello storico.»);
  - `LottoAvviso.Reinviabile()` = `resendable?/1`: inviato, non chiuso, link acceso;
  - `PreparaReinvio(ctx, id, base)` = `resend_message/1`, **sola lettura**: `lottoReinviabile` (`resendable_lot/2`,
    anche id ≤ 0 → «Lotto non disponibile.»), `busteDaReinviare` (`resend_members/1`: nessuna busta fuori dal cestino
    da consegnare, comprese quelle passate a un'altra oreficeria, → `ErrLottoCompletato`; poi le buste di
    `public_members/1`, nessuna → `ErrNienteDaReinviare`), il testo `wa_oreficeria_lotto` con lo stesso link
    (`testoDelLotto`, che ora usa anche `InviaLotto`: stessa composizione, `ErrModelloLotto`,
    `ErrSenzaIndirizzoPubblico`), il numero `NumeroWhatsApp` dell'oreficeria ("" → contatto scelto);
  - `RegistraReinvio(ctx, id, testo, scelto, contesto)` = `record_resend/4`: in una transazione (`inTx`) rilegge il
    lotto e le buste (stessi rifiuti), scrive con `registraConsegna` la riga WhatsApp `accepted` all'oreficeria **senza
    busta** con il testo dato e il numero, o `contatto-scelto` con `scelto` o senza numero, `outgoing.handed_off`
    (`repair_id: null`) e `ready_lot.resent` `{"bags": N, "message_id": id}`. Non tocca lotto, gettone, invio, avvisi.
- **Riuso e ritocchi:** `registraConsegna` scrive `busta_id` NULL e `repair_id: null` per `bustaID` 0 (`consegnaAMano.busta`);
  `scandisciLotto`/`colonneLotto` servono a `LottoAvviso` (089), allo storico e al reinvio; `BustaDelLotto.Ritirata`
  (stato `consegnata`) letto da `BusteDelLotto`; `InviaLotto` usa `testoDelLotto` (nessun cambio di comportamento: i test
  della 091 passano com'erano).
- **Web** (`internal/web/lotti_storico.go`, `pagine/lotti_storico.html`, `app.css` `.lotto-completato`,
  `.lotto-da-ritirare`, `.lotto-ritirata`, `.lotto-pulsanti form`): le quattro rotte (`rottaConGuscio`, `personale`),
  gli id dello standalone (`ready-lot-history-*`, `ready-lot-resend-*`). Il link «Storico dei lotti inviati» di
  `/ready-lots` compare ora (segue `haRotta`). Titolo della pagina «Storico lotti» per tutte e due.
  - **Il testo preparato senza LiveView:** `depositoReinvii` in memoria, chiave casuale (`casuale()`, 22 caratteri)
    legata a utente, nonce della sessione e lotto, 30 minuti, al più 500 voci (via quella che scade prima). `prendi`
    dice «da registrare» la prima volta e «già registrato» dopo (anche con due tocchi insieme, sotto lucchetto);
    `togli` dopo un rifiuto o un guasto al tocco.
  - **I due tocchi** sono moduli `data-avviso-invio data-channel="whatsapp"` (il gestore di app.js di «Invia» del popup
    e dei solleciti 083, nessun JavaScript nuovo): JSON `{"url"}` con `no-store`, scheda aperta al tocco a wa.me e
    pagina alla lista; senza JavaScript il redirect a wa.me.
- **Interfaccia `Buste`** (`nuova.go`) e `busteFinte` con i quattro metodi; `facciaWeb.reinvii` in `salute.go`.
- **SVILUPPO:** punto «Storico dei lotti e reinvio» nelle rotte, D-74 aggiornata (il reinvio è fatto).

## Scelte

1. **Il testo registrato è quello preparato** (come lo standalone, che registra `resend_pending`): il deposito tiene il
   testo, il tocco non ricompone. Il numero della riga è riletto al tocco (`lot_destination/1` nella transazione), il
   numero di wa.me è quello della preparazione (l'`href` dello standalone).
2. **«Una volta per preparazione» è della pagina**, come nello standalone è del socket: il dominio registra a ogni
   chiamata (test), il deposito decide. Preparare di nuovo dà una chiave nuova e un tocco nuovo registra di nuovo (come
   il test «either one records it» dello standalone).
3. **Rifiuto al tocco:** la chiave si consuma e il dettaglio si ridisegna con il messaggio (lo standalone lascia i link
   e un secondo tocco non registra: lo stesso effetto). Con JavaScript la scheda vuota si chiude (D-74, come il popup).
4. **D-46 estesa:** il messaggio è la frase del popup della 066, «Messaggio non inviato: manca … poi riavvia …».
5. **Ordine dello storico per id**, non per data di invio, come lo standalone (test con un lotto più recente inviato
   prima).

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-093/prove/rosso.txt`): preso **prima** del codice, con gli stub delle firme
  (dominio con `errDaFare`, deposito che non tiene niente, rotte non registrate): 6 test del dominio e 8 della pagina
  rossi; `TestDepositoReinviiScade` passava già con lo stub (prova solo dei «no»). Un ritocco successivo all'attesa
  della D-46 (la frase del popup) è stato rifatto rosso prima del codice web (in coda al file).
- **Dominio** (`internal/buste/lotti_storico_test.go`): `TestStoricoLotti` (vuoto, aperto escluso, ordine per id con un
  lotto inviato prima, chiuso, 3 buste e 2 da ritirare con cestino e consegnata, lotto senza buste),
  `TestStoricoLottiCento`, `TestLottoDelloStorico` (aperto, 9999, 0, −1; buste con `Ritirata` anche passate a un'altra
  oreficeria; spento e chiuso non reinviabili), `TestPreparaReinvio` (testo con lo stesso link e 1 busta pubblica su 3,
  numero, tre telefoni senza cifre, niente scritto), `TestPreparaReinvioRifiuti` (otto rifiuti di prepara **e** registra,
  niente scritto; testo non valido; senza indirizzo con e senza `{link}`), `TestRegistraReinvio` (riga, audit
  `ready_lot.resent` e `outgoing.handed_off` con `repair_id: null`, lotto e avvisi invariati, «Scegli», senza numero).
- **Pagina** (`internal/web/lotti_storico_test.go`): `TestPaginaStoricoLotti` (markup della lista vuota e piena, date
  sull'orologio del negozio d'estate e d'inverno, link da `/ready-lots`, titolare), `TestPaginaLottoStorico` (quattro
  rifiuti, markup del dettaglio, 61 grafemi tagliati a 60, D-64 con e senza etichetta, cestino escluso, stesso link
  della pagina del lotto, completato, spento senza link né reinvio, nessuna busta), `TestReinvioLotto` (riquadro, 403
  senza token, prepara non scrive, tocco JSON e riga, secondo tocco JSON / senza JavaScript / da «Scegli» senza riga,
  lotto e avvisi invariati, preparato di nuovo e «Scegli» senza JavaScript), `TestReinvioSenzaNumero`,
  `TestReinvioRifiuti`, `TestReinvioSenzaIndirizzoPubblico`, `TestReinvioToccoSenzaMessaggio` (chiave inventata, vuota,
  di un'altra sessione; rifiuto al tocco che consuma la chiave), `TestDepositoReinviiScade`,
  `TestDepositoReinviiUnaVolta` (20 tocchi insieme, uno solo registra). `TestPaginaLotti` (088) ora vuole il link.
- **Banco (unità):** `TestScenariBrowserDelRepo` (voci di `lotti-reinvio`), `TestScenarioUtentiSuiDueDati` (attese con
  i dati di `lotti-storico` solo su `/ready-lots/history` e `/ready-lots/history/1`).
- **`scripts/check`** verde (`to-be-deleted-after-use/slice-093/prove/check.txt`; dopo la revisione `check-2.txt`); il primo giro (`check-1-rosso.txt`) era
  rosso per `TestRotteConGuscio` e `TestOgniRottaHaIlSuoPermesso` (le quattro rotte nuove da aggiungere alle tabelle),
  sistemati.

## Banco

**Giro s093a (17/11/2026, sessione principale): ROSSO, da correggere prima del push.** HTTP 83 scenari, 6
inattese tutte in `lotti-storico`; browser 2 errori e 128 inattese tutte in `lotti-reinvio` (standalone, fase «storico»,
PC e telefono). Causa capita: nello standalone il link «Storico dei lotti inviati» di `/ready-lots`
(`ready_lots_live.ex:100`) non ha id; `ready-lot-history-link` è l'id del link pubblico nel dettaglio
(`ready_lot_history_live.ex:195`). Il Go l'ha messo sul link della lista e gli scenari lo cercano per id, quindi nello
standalone è «assente» e la prova browser non trova cosa cliccare. In più le attese `visibile:ready-lot-history-1` e
`visibile:ready-lot-history-bag-14` di `lotti-storico` dicono «(assente)» ma le due app mostrano lo stesso testo.
**Correzione:** togliere l'id dal link della lista nel Go (markup come lo standalone), cercare il link per href o testo
negli scenari, correggere le due attese, giro nuovo s093b. Esito rosso salvato in
`to-be-deleted-after-use/slice-093/prove/esito-s093a-rosso.json`.

Da fare dalla sessione principale.

- **HTTP `lotti-storico`** (nuovo, gira sui due database, non `in_fondo`): login con ritorno allo storico; il link
  «Storico dei lotti inviati» di `/ready-lots`; la lista (vuota: «Nessun lotto inviato.»; con i dati: il lotto 1 della
  catena, quella della 14/26, «0 da ritirare», niente «Completato», «Apri» verso `/ready-lots/history/1`); il dettaglio
  del lotto 1 con `normalizza: D-64` (titolo «📦 Lotto Oreficeria Sintetica», «Lotto #1 · inviato», il link per id, non
  per testo, il riquadro del reinvio senza testo, la busta 14 «Ritirata» e `/repairs/14`; sul database vuoto il
  redirect); `/ready-lots/history/999999` e `/abc` con «Lotto non trovato nello storico.» nella pagina dopo.
  **Interferenze:** legge e basta. Il lotto 1 è aperto (il passo `token` toglie `closed_at`) finché `lotto-pubblico`
  (`in_fondo`) non lo chiude: per questo lo storico lo vede «da ritirare» e con il reinvio, uguale nelle due app. Il
  commento del passo `token` (`tools/banco/token.go`) ora lo dice. Nessuno scenario HTTP invia lotti: la lista con i dati
  ha la sola riga 1.
- **Browser `lotti-reinvio`** (nuovo, `prova_con: sintetici-import`): **riusa i due lotti della 091** (eccezione ai dati
  propri, motivata nel commento in `banco.mjs`: un lotto inviato si ottiene solo con «📤 Chiudi e invia»; `lotti-invio`
  gira subito prima nella stessa vista e lascia due lotti inviati e non completati di «Zz Banco 091 <vista>» con il
  telefono tolto). Per vista: salva «Avviso lotto pronte oreficeria» senza `{link}`; dallo storico (righe filtrate per
  oreficeria, 2) apre la più vecchia (A e B: «2 buste», «2 da ritirare»; id e data normalizzati); dettaglio (titolo,
  ritorno, link ridotto a `/lotto/<gettone>`, 2 buste, «Apri busta», riquadro); «Prepara» senza numero (messaggio, testo,
  solo «Scegli», avviso, «Pronto, non ancora registrato…», nessuna riga); tocco su «Scegli contatto» (lista, 1 apertura,
  1 riga); telefono rimesso, «Prepara», **doppio tocco** (`dblclick`) su «↗ Apri WhatsApp» (lista); registro
  `ready_lot.resent` +2, `outgoing.handed_off` +2; alla fine telefono tolto e testo predefinito (`fine.ripristino`).
- **Fragilità da guardare per prime:** (a) `storico.riga` è il testo intero della riga: se differisce solo negli spazi
  fra i blocchi, è il markup (si può ridurre a `storico.da-ritirare`); (b) il `dblclick`: nello standalone due schede
  wa.me e due eventi (il secondo trova il messaggio già registrato), nel Go una sola richiesta (il modulo è già
  partito); si confronta solo il registro (+1) e il ritorno alla lista; (c) `visibile:ready-lot-history-1` dell'HTTP:
  stesso rischio degli spazi di (a); (d) `testo:Lotto #1 · inviato` è sul corpo grezzo: se HEEx spezza la riga, è il
  sorgente della pagina, non il Go.
- **Ordine:** `lotti-reinvio` dopo `lotti-invio` per nome; il telefono resta come la 091 l'ha lasciato (vuoto), il
  testo torna predefinito: `testi-invii`, `pannello-qr` e `anagrafica-oreficerie` (telefono) trovano quello di prima.

## Revisione

- **Mutazioni:** 52; uccise 46. Delle 6 sopravvissute, 2 equivalenti (D22, W03) e 4 coperte ora: W02 (il confine dei
  60 grafemi: una descrizione di esattamente 60 resta intera, `TestPaginaLottoStorico`), W13 (tetto 500→500000), W24
  (tolta la pulizia degli scaduti in `metti`), W25 (sfrattata la voce che scade per ultima), con
  `TestDepositoReinviiPieno` (orologio finto: 500 voci, la 501ª fa uscire quella che scade prima, le scadute si tolgono
  al `metti` dopo).
- **Rilievi:** 1 MEDIA e 5 BASSI, chiusi. MEDIA: il tetto del deposito non provato (sopra). BASSI: `via` non controllato
  (ora `scelto := via == "scegli"`, il numero solo se non scelto, in tutti e due i punti come `phx-value-via`;
  `TestReinvioViaNonScegli` con `via` vuoto e strano); il confine dei 60 grafemi (W02); `rispondiWaMe` è la terza copia
  dello stesso codice (popup 066, solleciti 083): **in lista per una slice di pulizia**, adesso non si tocca; il guasto
  che non è un rifiuto (Dubbi, 6); il testo della D proposta completato con il rifiuto al tocco e il nuovo accesso
  (Dubbi, 1).
- **Verifica a mano** (`to-be-deleted-after-use/slice-093/mutazioni/verifica.txt`): W02, W13, W24 e W25 riapplicate
  muoiono; il file rimesso com'era e verificato con `cmp`.

## Per il manuale

- In «Avviso oreficeria» il link «Storico dei lotti inviati» apre i lotti già partiti, dal più recente (gli ultimi
  100): per ognuno l'oreficeria, quante buste, quando è partito e quante buste mancano da ritirare, oppure «Completato
  il …» quando l'oreficeria le ha ritirate tutte. Un lotto inviato non si cancella: resta come memoria di chi è stato
  avvisato.
- «Apri» mostra il lotto: quando è partito e quando si è completato, il link della pagina dell'oreficeria (lo stesso
  mandato con il lotto) e le buste, con «Ritirata» e «Apri busta». I lotti arrivati dalla vecchia app con il link
  spento dicono «Link storico non riattivato durante l'importazione.».
- **Reinvia all'oreficeria:** per un lotto non ancora completato, «Prepara di nuovo il messaggio» rifà il testo con lo
  stesso link e il numero di buste che l'oreficeria vede ancora. Preparare non registra niente: il reinvio finisce fra
  gli invii solo quando tocchi «↗ Apri WhatsApp» (la chat dell'oreficeria) o «👥 Scegli contatto» (scegli tu a chi
  mandarlo). Un secondo tocco sullo stesso messaggio riapre WhatsApp senza registrare di nuovo. Gli avvisi delle buste
  non cambiano.
- Se l'oreficeria non ha un numero WhatsApp c'è solo «Scegli contatto», con l'invito a salvare il numero in
  Anagrafiche. Se tutte le buste sono state ritirate, o nessuna è più di quell'oreficeria, il messaggio lo dice e non
  si prepara niente.
- Se dopo «Prepara» passa troppo tempo (mezz'ora) o il programma si riavvia, il tocco dice «Prepara di nuovo il
  messaggio: niente è stato registrato.»: basta preparare di nuovo.

## Dubbi

1. **D-nuova (da approvare): messaggio preparato che il server non ha più.** Nello standalone il testo preparato vive
   nel socket finché la pagina è aperta; nel Go (senza LiveView) sta in memoria per 30 minuti, legato all'accesso. Un
   tocco con una chiave che il server non ha (riavvio, scaduta, un'altra sessione, inventata) non registra e ridisegna
   il dettaglio con un errore. Testo proposto: «D-nn (ORE-13, slice 093): il messaggio del reinvio preparato con
   «Prepara di nuovo il messaggio» vale 30 minuti e per l'accesso che l'ha preparato; dopo un riavvio, la scadenza o da
   un'altra sessione, o dopo un rifiuto al tocco, o dopo un nuovo accesso (esci ed entra), il tocco su «Apri WhatsApp» /
   «Scegli contatto» non registra e mostra «Prepara di nuovo il
   messaggio: niente è stato registrato.». Lo standalone tiene il testo nella pagina aperta.» Raccomandazione:
   approvare (è il solo modo di avere «un tocco, una riga» senza mettere il testo nel modulo, dove si potrebbe
   cambiare). Segnaposto nel codice: `msgReinvioDaPreparare` in `internal/web/lotti_storico.go`. Ragionato (nessun caso
   uguale nello standalone).
2. **Senza JavaScript il tocco registra** (il modulo va al server, poi il redirect a wa.me); nello standalone, senza
   LiveView, l'`<a href=wa.me>` apre WhatsApp e non registra niente. È la scelta già confermata per il popup («Senza
   JavaScript «Invia» va diretto a wa.me», Dubbio 1 della 066) e per i solleciti (083). Raccomandazione: tenerla senza
   D (coperta dalla D-74 così come è fatta). Ragionato.
3. **«1 buste» nello storico:** lo standalone scrive sempre «{n} buste» (anche «1 buste»), mentre la lista dei lotti
   aperti dice «1 busta». Copiato com'è (difetto estetico dello standalone, non corretto). Raccomandazione: lasciarlo in
   lista fra i difetti non gravi; se Stefano vuole «1 busta», è una D da una riga. Misurato nel Go, ragionato nello
   standalone (sorgente).
4. **Il lotto 1 dell'HTTP «0 da ritirare» e non «Completato»:** effetto del passo `token` della 092 (un lotto completato
   prima della slice 212, chiuso solo alla prossima apertura del link). Le due app lo mostrano uguale; chi guarda il
   banco lo sappia. Nessuna azione.
5. **Bug dello standalone:** nessuno trovato. Da notare, non da correggere: la riga dello storico conta anche le buste
   passate a un'altra oreficeria («N buste»), mentre il reinvio dice quante ne mostra il link (`n_buste` del testo):
   un lotto può dire «2 buste» nello storico e «1 riparazioni» nel messaggio. È lo standalone.
6. **Guasto che non è un rifiuto:** al «Prepara» o al tocco un errore del database dà la pagina d'errore 500 (con
   «reinvio non preparato» / «reinvio non registrato» nel registro), mentre lo standalone mostra «Reinvio non riuscito:
   niente è partito.». È la regola generale del Go (come la 089, scelta 7, e la 092). Raccomandazione: nessuna D.
   Ragionato.
7. **Differenze misurate e ragionate.** Misurato solo il Go (test). Il confronto con lo standalone è ragionato dal
   sorgente a `go-baseline` finché la sessione principale non fa il giro del banco: testi, id, ordine, conteggi,
   rifiuti, righe e audit del reinvio.
