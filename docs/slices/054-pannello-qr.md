# 054 — Pannello delle azioni del QR (T4.2, quinta)

- **Fonte:** riga 054 del piano della T4.2 (`050-home-lista.md`); le risposte 9 e 10 del 05/11/2026
  (RIP-H-24 in questa slice sui dati importati; RIP-H-26 con lo stato che avanza adesso e l'avviso
  WhatsApp con la T4.6) e la D-70 (approvata il 05/11/2026: pannello e conferme senza passaggio dal
  server con JavaScript, la riserva senza JavaScript con un POST e una pagina di conferma). Lo
  standalone a `go-baseline`: `WorkshopLive` (`@qr_actions`, `qr_select/2`, `qr_confirm_text/2`,
  `@qr_auto_return_confirm`, `qr_execute/2`, `qr_error/1`, `source_word/1`, gli eventi `qr_advance`,
  `qr_confirm_yes`, `qr_confirm_no`, `qr_estimate`, il riquadro `#qr-action` con
  `#qr-action-notice`, `#qr-action-confirm`, `#qr-btn-avanza`, `#qr-btn-prev`, `#qr-btn-consulta`,
  `#qr-btn-si`, `#qr-btn-no`), `Workshop` (`qr_advance_repair/3`, `qr_set_estimate/3`,
  `qr_next_status/1`, `qr_auto_return?/1`, `qr_auto_return/4`, `return_write/4`,
  `close_handover_if_complete/3`, `audit_auto_return/4`, `run_transition/4`,
  `transact_transition/5`), `Repair.out_of_shop?/1` e `out_of_shop_query/1`; le ricevute dello
  standalone `006-azioni-qr-popup.md` e `003-rientro-automatico-qr.md`.
- **Voci:** RIP-H-23 (avanzamento di stato dal pannello), RIP-H-24 (rientro automatico
  dall'artigiano), RIP-H-25 («💰 Preventivo»), RIP-H-26 (valutazione del compro oro: **parziale**,
  l'avviso WhatsApp di ritiro è della T4.6, test saltato `TestPannelloQRAvvisoWhatsApp`).
- **Deviazioni:** D-70 (sotto); D-63 e D-64 come nella 052 e 053.
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Le azioni del pannello per stato

Dalla tabella `@qr_actions` di `workshop_live.ex` (go-baseline) e da `qr_select/2`:

| Busta | Stato | Pulsante | Conferma | Dopo |
|---|---|---|---|---|
| riparazione | in lavorazione, in attesa materiali | «✅ Pronta» (+ «💰 Preventivo») | nessuna | scheda con il modulo sul prezzo (`?edit=prezzo`) |
| riparazione | preventivo | «▶ In lavorazione» | nessuna | «✓ in lavorazione», lista con i filtri |
| riparazione | pronta | «✅ Consegnata» | «Marchio come consegnata? Verrà impostata data oggi.» | «✓ consegnata», lista con i filtri |
| compro oro | in valutazione | «✅ Valutata» | «Conferma valutazione completata? Verrà aperto WA "scheda pronta firma".» | «✓ valutata», lista con i filtri (l'avviso WhatsApp: T4.6) |
| compro oro | valutata | «✅ Perfezionata» | «Confermi firma scheda? Movimento di cassa registrato.» | «✓ perfezionata», lista |
| compro oro | perfezionata | «✅ Chiusa» | nessuna | «✓ chiusa», lista |
| tutte | consegnata, annullata, chiusa | nessuno | — | «ℹ Stato X — non avanzabile.» e solo «👁 Consultazione» |

Una busta ancora dall'artigiano il cui prossimo stato è pronta o consegnata chiede sempre la
conferma, con «La busta risulta fuori bottega: viene segnata anche rientrata dall'artigiano.» dopo la
frase della riga (o da sola). «💰 Preventivo» c'è solo per le riparazioni fuori da preventivo, pronta
e dagli stati chiusi; porta alla scheda con il modulo sul preventivo (`?edit=preventivo`).

## Cosa c'è

- **Dominio** (`internal/buste/qr.go`, nuovo; `stati.go`, `ricerca.go`, `etichetta_scritta.go`):
  - `passaStato` è l'unico cambio di stato (`run_transition/4` e `transact_transition/5`):
    `CambiaStato` della scheda ci passa senza cambiare comportamento (lo stato vuoto resta un
    rifiuto), e lo usano `AvanzaDaQR` e `PreventivoDaQR`. Grafo, date solo se vuote, conservazione,
    chiusura del lotto e audit `repair.status_changed` sono quelli della scheda, con `"via": "qr"`
    nei metadati come `qr_audit/1`.
  - `AvanzaDaQR(id, statoVisto)` è `qr_advance_repair/3` con `:expected_status`: la riga riletta in
    transazione deve avere ancora lo stato che il pannello mostrava, altrimenti `ErrStatoCambiato`
    e niente scritto; una busta chiusa nel frattempo dà prima `ErrNonAvanzabile`. Il prossimo stato
    è `qr_next_status/1`; uno stato del compro oro senza seguito è `ErrOroNonGestito`.
  - Il rientro automatico (RIP-H-24, `qr_auto_return/4`) sta nella stessa transazione: se il nuovo
    stato è pronta o consegnata, un `UPDATE` con la condizione di fuori bottega scrive solo
    `rientrata_artigiano_il` (né esito né note, come l'UPDATE automatico della sorgente); se ha
    segnato, la consegna all'artigiano si chiude quando non ha più buste fuori (una volta sola) e la
    riga `repair.returned_from_artisan_auto` dice `bag_number`, `handover_id`, `handover_closed` e
    `via`. La scheda non rientra mai (`CambiaStato` senza l'opzione). Le consegne all'artigiano e le
    buste fuori bottega oggi vengono solo dall'import: il rientro a mano e le consegne sono di
    un'altra area.
  - `PreventivoDaQR` è `qr_set_estimate/3`: in preventivo con l'audit, già in preventivo resta senza
    audit, un compro oro è `ErrPreventivoOro`, da pronta o chiusa il grafo dice «Transizione non
    consentita.».
  - `RigaQR` porta `FuoriBottega`; `Azione`, `RientroAutomatico`, `AvvisoDelPannello`,
    `ConPreventivo` e `ParolaDelloStato` sono la tabella e le parole della sorgente; `RigaPerQR`
    rilegge una busta per il pannello (fuori dal cestino).
- **Pagine** (`internal/web/pannello_qr.go`, nuovo; `cerca.go`, `pagine/cercaqr.html`,
  `statici/app.js`, `statici/app.css`, `salute.go`):
  - il pannello `#qr-action` della pagina della scansione (`POST /cerca-qr`) ha il modulo
    `#qr-action-form` (POST a `/repairs/{id}/qr-avanza`, con il token, `stato_visto` e i filtri della
    lista come campi nascosti, mai il codice letto) con «#qr-btn-avanza», «💰 Preventivo»
    (`formaction` a `/repairs/{id}/qr-preventivo`) e «👁 Consultazione»; con una conferma, il blocco
    `data-qr-conferma` (la frase `#qr-action-confirm`, «Sì» `#qr-btn-si` con `conferma=1`, «Annulla»
    `#qr-btn-no`) è nella pagina, `hidden`. Una busta chiusa ha solo l'avviso e «👁 Consultazione».
  - **D-70:** con JavaScript `app.js` scambia i due blocchi al clic di un pulsante con
    `data-qr-chiedi` e torna indietro con «Annulla», senza passare dal server, come la conferma in
    posto dello standalone; «Sì» manda il modulo. Senza JavaScript lo stesso pulsante manda il modulo
    senza `conferma=1` e il server risponde con il pannello in conferma (solo frase, «Sì» e
    «Annulla», `no-store`); il suo «Annulla» (`annulla=1`) ridisegna i pulsanti. Niente si scrive
    senza `conferma=1` esatto.
  - `POST /repairs/{id}/qr-avanza` e `POST /repairs/{id}/qr-preventivo` (personale, con il guscio,
    CSRF): dopo l'azione come nella tabella sopra; la lista è quella dei filtri del modulo, che
    restano (`qr_reset` e `refresh` dello standalone tengono `index_params`). La busta cambiata nel
    frattempo dà «Stato cambiato nel frattempo: ricontrolla.» (flash d'errore) con il pannello della
    busta com'è adesso; un rifiuto il pannello con l'avviso del rifiuto (`qr_error/1`: `Stato "x" non
    avanzabile`, `Stato CO "x" non gestito`, «QR preventivo non supportato per buste compro oro»,
    «Transizione non consentita.») e solo «👁 Consultazione»; una busta che non c'è o sta nel
    cestino «⚠️ Nessuna riparazione trovata». Nessuna di queste scrive `repair.qr_lookup`.
  - Nel modulo lo stato mostrato si chiama `stato_visto` perché `stato` è già il filtro della lista
    (un primo `stato` faceva leggere il filtro come stato visto: trovato dai test).
- **Banco:**
  - scenario HTTP `qr-pannello` (43 passi, con i due secondi invii della review, passi `solo_go` D-70, `in_fondo` dopo `firma`): la 29/26
    («✅ Pronta» con `?edit=prezzo` e la scheda in modifica, poi «✅ Consegnata» con la conferma
    nascosta, la conferma dal server con il filtro `stato=pronta`, «Annulla», «Sì» con il redirect a
    `/?stato=pronta` e «✓ consegnata», poi l'avviso della chiusa), la 30/26 con lo stato visto
    sbagliato («Stato cambiato nel frattempo…», niente scritto), la 33/26 («💰 Preventivo» con
    `?edit=preventivo`, poi «▶ In lavorazione» e «✓ in lavorazione»), la 34/26 (la conferma di
    «✅ Valutata», senza confermare), la 13/26 chiusa, due 403 senza token (e le schede rilette
    uguali), il registro senza codici. Sul database vuoto ogni azione dice «Nessuna riparazione
    trovata».
  - scenario browser `pannello-qr` (nelle due app, RIP-H-23…26): sul PC la 4/26 (le si dà
    l'etichetta RIP-2026-0058 dalla modifica della scheda; il pannello, la conferma con la frase del
    rientro, «Annulla», «Sì», la scheda con il modulo sul prezzo, il riquadro «📤 Fuori bottega» che
    sparisce) e la 32/26 dalla lista filtrata `?stato=pronta` («✅ Consegnata», la conferma, «✓
    consegnata» e la lista filtrata); sul telefono la 34/26 («✅ Valutata», la conferma, «✓ valutata»,
    la scheda «Valutata», poi «✅ Perfezionata» con la sua frase e «✓ perfezionata») e la 31/26
    («💰 Preventivo» con il modulo, poi «▶ In lavorazione»); dopo la review anche la 37/26
    consegnata (solo «ℹ Stato consegnata — non avanzabile.» e «👁 Consultazione»). Il «Sì» della
    32/26 e «▶ In lavorazione» della 31/26 sono un doppio tocco: l'esito è quello di un tocco e il
    Go manda un POST solo. Il confronto non guarda l'indirizzo della pagina della scansione né il
    numero del titolo (D-64). Dopo `pannello-qr` girano `schede`, `sfondo`, `tentativi` e
    `visualizzatore`, che non leggono queste buste.
  - `cerca-qr` aggiornato: il pannello della 052 ora ha le azioni (passi 3, 7, 11).
  - `TestScenariInFondo`, `TestProvaConDegliScenariDelRepo`, `TestScenarioUtentiSuiDueDati` e
    `TestScenariBrowserDelRepo` sanno dei due scenari; `docs/SVILUPPO.md` dice le buste che usano e la
    regola del pannello.

## Deviazioni

- **D-70** (approvata, dal registro: «con JavaScript il pannello azioni del QR e le conferme si
  aprono senza passaggio dal server»). **La 054 copre le conferme:** con JavaScript `app.js` le apre
  e le chiude senza passare dal server, senza JavaScript la riserva è un POST e una pagina di
  conferma. **Non copre il pannello:** nel Go il pannello è ancora la pagina della risposta a
  `POST /cerca-qr` (D-63), che arriva dal server anche con JavaScript (e la lista sparisce), mentre
  nello standalone il pannello si apre sopra la lista con un evento LiveView: vedi il Dubbio 9. Le
  azioni sono due POST con il redirect. Passi `solo_go` D-70 nel livello HTTP; nel livello browser i
  soli campi `solo_go` sono `consegna.invii` e `lavorazione.invii` (i POST del Go, che nello
  standalone non esistono).
- **RIP-H-26 parziale** (risposta 10): dopo «✅ Valutata» lo standalone registra l'avviso di ritiro e
  offre «↗ Apri WhatsApp» (`#qr-opening`); il Go dice «✓ valutata» e torna alla lista. Il browser
  non osserva `#qr-opening`.
- **L'email di «pronta»:** lo standalone, dopo «✅ Pronta» del pannello, accoda l'email di pronta
  come dopo il cambio di stato della scheda (`qr_advance_repair/3` passa da `transact_transition/5`,
  che chiama `announce_ready_by_email/4`). Nel Go manca per tutti i chiamanti fino alla T4.7: il test
  saltato `TestEmailDiProntaDopoIlCambio` e il commento di `passaStato` ora nominano anche il
  pannello (review B1).
- **Il secondo invio** (doppio tocco) nel Go è riuscito senza scrivere niente (review M1, sotto);
  nello standalone il secondo evento LiveView arriva a un pannello già chiuso e non fa niente.
- Altre differenze di forma, scritte nei Dubbi: la conferma si rilegge dalla busta di adesso (Dubbio
  2), la busta che non c'è (Dubbio 3).
- **Non confrontato con lo standalone** (solo nei test Go e nei passi `solo_go`): la riserva senza
  JavaScript (pagina di conferma e il suo «Annulla»); la busta cambiata nel frattempo; i rifiuti
  (`Stato "x" non avanzabile`, compro oro con «💰 Preventivo», grafo); la busta che non c'è o nel
  cestino; il CSRF; il registro (righe con `via`, `repair.returned_from_artisan_auto`, chiusura della
  consegna); «✅ Chiusa» del compro oro; «✅ Pronta» da in attesa materiali; il secondo invio sul
  server (il browser prova solo che il Go manda un POST e che l'esito è quello di un tocco).

## Prove

- **Test Go** (database vero, orologio al 31/10/2026):
  - `internal/buste/qr_test.go`: `TestAzioniDelPannello` (12 stati delle due famiglie, anche fuori
    bottega: pulsante, conferma, dopo, preventivo, avviso, prossimo stato e rientro),
    `TestAvanzaDaQR` (catene fino alla chiusura, date, conservazione, audit con `via`, cestino,
    assente), `TestAvanzaDaQRBustaCambiata`, `TestPreventivoDaQR`, `TestRientroAutomaticoDaQR`
    (istante solo, note tenute, consegna chiusa all'ultima, una volta sola, `handover_id` nullo,
    preventivo e scheda senza rientro, ordine delle righe d'audit), `TestRigaQRFuoriBottega`,
    `TestAvanzaDaQRSecondoInvio` (review M1).
  - `internal/web/pannello_qr_test.go`: `TestPannelloQRPerStato` (titolare e dipendente, 12 casi),
    `TestPannelloQRConferma` (la riserva senza JavaScript, «Annulla», un `conferma` diverso da «1», i
    filtri, «✓ consegnata», l'audit, il codice né nel registro né nelle righe del server),
    `TestPannelloQRAvanza` (`?edit=prezzo`, «✓ in lavorazione», «✓ chiusa», busta cambiata, chiusa,
    assente, nel cestino, nessuna `repair.qr_lookup`), `TestPannelloQRPreventivo`,
    `TestPannelloQRRientro`, `TestPannelloQRValutata`, `TestPannelloQRAvvisoWhatsApp` (saltato:
    T4.6), `TestPannelloQRCSRF`, `TestScriptDelPannelloQR`.
- `scripts/check` verde (gofmt, vet, staticcheck, errcheck, build, test, copertura: 353 voci, 26
  chiuse, 0 rosse, 81 parziali, 235 aperte, 11 fuori v1; segreti e dati veri OK).
  - Ritoccati: `TestCercaQRScansione` (il pannello con le azioni), la tabella dei permessi (due
    righe), `TestRotteConGuscio`, il finto `busteFinte`.

## Mutazioni

`to-be-deleted-after-use/slice-054/mutazioni/muta.py`, 40 punti, test mirati di `internal/buste` e
`internal/web` in una copia del repo nello scratchpad (cancellata prima di `scripts/check`).
Primo giro: 34 uccise, 5 non compilavano (riscritte), 1 sopravvissuta (la riga del pannello letta
anche dal cestino: aggiunto il caso a `TestPannelloQRAvanza` e `TestRigaQRFuoriBottega`). Secondo
giro sulle 6: 5 uccise, 1 sopravvissuta equivalente. Dopo la review 5 in più (M1: il secondo invio
come busta cambiata, anche da uno stato fuori famiglia, il dopo dall'azione di adesso, `app.js` che
manda due volte; B2: la conferma chiesta anche a una busta cambiata): 5 uccise. **Totale: 45 punti,
44 uccise, 1 equivalente.**

Uccise: consegnata e valutata senza conferma; pronta alla lista invece del prezzo; preventivo che
avanza a pronta; compro oro che salta valutata; preventivo anche per pronta; rientro anche in
bottega, per ogni stato, anche se già rientrata; nessuna frase del rientro; consegna chiusa con buste
ancora fuori e richiusa; audit senza `via`; nessun controllo dello stato visto; chiusa nel frattempo
come cambiata; preventivo al compro oro; preventivo già in preventivo riscritto; rientro anche dalla
scheda; stato vuoto della scheda accettato; riga del pannello dal cestino; conferma non richiesta o
con un valore qualsiasi; «Annulla» ignorato; lista senza filtri dopo l'azione; messaggio senza «✓» o
con lo stato di prima; pronta senza `?edit=prezzo`; preventivo senza `?edit=preventivo`; rifiuto con
i pulsanti; rifiuto sempre generico; codice nel modulo del pannello; stato visto col nome del filtro;
filtri persi nel modulo; conferma sempre visibile; pulsante senza `data-qr-chiedi`; token assente;
`app.js` che non ferma l'invio; azione senza CSRF.

Equivalente: dopo «Stato cambiato nel frattempo» il pannello disegnato dalla riga letta all'inizio
della stessa richiesta invece che da una seconda lettura: le due righe differiscono solo se un'altra
richiesta cambia la busta fra le due letture.

## Banco

Istanze nuove `s054a`…`s054e`, `gestrip-standalone:fab-222`.

- **Giro mirato** (`s054a`, dati importati: `qr-pannello` e `cerca-qr`, browser `pannello-qr`): HTTP
  65 passi, 13 inattese, tutte attese mie da correggere (le chiavi dell'attesa di base valgono anche
  sui dati; il pannello della 052 in `cerca-qr` ora ha le azioni); browser **verde al primo giro**.
  Rifatto il solo HTTP (`s054b`): 65 passi, **0 inattese**.
- **Giro completo, dati importati** (`s054c`): HTTP **47 scenari, 856 passi, 1081 differenze attese,
  0 inattese**, 0 errori; browser 9 scenari, 18 viste, 26 attese, **2 inattese**: il 500 noto dello
  **standalone** in `firma-riquadro[1]` (`/repairs/6/photos/29`, «database is locked» di fab-222; il
  Go nessun errore). Non accettato in `deviazioni.json`.
- **Giro completo, database vuoto** (`s054d`): HTTP 47 scenari, 856 passi, 31 attese, **0 inattese**
  (29 non provati con questi dati); browser 9 scenari, **0 inattese**: verde.
- **Secondo giro completo, dati importati** (`s054e`): HTTP uguale (47, 856, 1081, **0 inattese**);
  browser **4 inattese**, tutte in `firma-riquadro[1]` dello **standalone**: lo stesso 500 su
  `/repairs/6/photos/29` e, per lo stesso guasto, il suo messaggio «Firma registrata: riparazione
  consegnata.» che non arriva (il Go lo dà). `pannello-qr` verde in tutti i giri.
- **Dopo la review** (codice finale, istanze nuove):
  - **giro completo, dati importati, `s054f`**: HTTP **47 scenari, 860 passi, 1081 attese, 0
    inattese**, 0 errori; browser 9 scenari, 18 viste, 26 attese, **2 inattese**, tutte e due il 500
    noto dello **standalone** in `firma-riquadro[1]` (`/repairs/6/photos/29`; il Go nessun errore),
    non accettato in `deviazioni.json`. `pannello-qr` verde, con `consegna.invii` e
    `lavorazione.invii` a 1 nel Go (0 nello standalone, campo `solo_go`);
  - **giro completo, database vuoto, `s054g`**: HTTP 47 scenari, 860 passi, 31 attese, **0
    inattese** (29 non provati); browser 9 scenari, **0 inattese**: verde.
- `banco giu` dopo ogni giro e `banco controlla`: «AVANZI: nessuno».

## Review indipendente

Nessun difetto alto; uno medio e quattro bassi, tutti chiusi in questa slice.

1. **M1 (medio), doppio invio.** Un doppio tocco su «✅ Pronta» mostrava «Stato cambiato nel
   frattempo» con «✅ Consegnata» (e non si arrivava a `?edit=prezzo`); un doppio «Sì» su «✅
   Consegnata» mostrava «ℹ Stato "consegnata" non avanzabile». Ora: `app.js` ferma gli invii dopo il
   primo del modulo `#qr-action-form` (`preventDefault`, il pulsante resta acceso, `pageshow` lo
   azzera); sul server `AvanzaDaQR` tratta una busta già nello stato che l'azione dello stato mostrato
   avrebbe prodotto come riuscita senza scrivere (`giaFatto`), e la pagina segue l'azione dello stato
   mostrato (`AzioneDiStato`). Test `TestAvanzaDaQRSecondoInvio` (cinque azioni, rientro compreso),
   `TestPannelloQRAvanza` e `TestPannelloQRConferma` (i secondi invii), `TestScriptDelPannelloQR`;
   due passi in `qr-pannello`; nel browser il «Sì» della 32/26 e «▶ In lavorazione» della 31/26 con
   un doppio tocco e un POST solo nel Go. Mutazioni: quattro, uccise.
2. **B1, email di «pronta».** `TestEmailDiProntaDopoIlCambio` e il commento di `passaStato` nominano
   anche il pannello del QR fra i chiamanti che con la T4.7 manderanno l'email; la differenza è nelle
   Deviazioni.
3. **B2, test mancante.** `TestPannelloQRAvanza` ha la busta pronta con lo stato visto
   «preventivo» senza `conferma`: «Stato cambiato» e niente scritto. Il caso della review (stato
   visto «in_lavorazione») dopo M1 è proprio un secondo invio di «✅ Pronta», riuscito: per questo il
   test usa uno stato visto che non porta a pronta. La mutazione che toglie `x.Stato == visto` muore.
4. **B3, commento.** `TestScriptDelPannelloQR` nomina lo scenario browser `pannello-qr` e dice che il
   comportamento JavaScript lo prova solo il banco.
5. **B4, copertura del banco.** `pannello-qr` ha la 37/26 consegnata (l'avviso) e la 34/26 valutata
   («✅ Perfezionata» con la sua frase); quello che resta non confrontato è nelle Deviazioni.
6. **Ricevuta.** Dubbi 4 e 5 riscritti, Dubbio 9 (D-70 e il pannello), la differenza dell'email, cosa
   la 054 copre della D-70.

## Per il manuale

- Dopo la scansione il riquadro della busta propone il passo successivo: «✅ Pronta», «▶ In
  lavorazione», «✅ Consegnata»; per il compro oro «✅ Valutata», «✅ Perfezionata», «✅ Chiusa».
- «💰 Preventivo» mette la busta in preventivo e apre la scheda sul preventivo; «✅ Pronta» apre la
  scheda sul prezzo finale.
- Consegna, valutazione e perfezionamento chiedono conferma nel riquadro: «Sì» conferma, «Annulla»
  torna ai pulsanti.
- Dopo le altre azioni compare «✓ consegnata» (o lo stato nuovo) e si torna alla lista con i filtri
  di prima.
- Se la busta è ancora dall'artigiano, il riquadro lo dice nella conferma e la busta risulta anche
  rientrata.
- Una busta consegnata, annullata o chiusa ha solo «👁 Consultazione».
- Dopo «✅ Valutata» il messaggio WhatsApp al cliente non parte ancora: arriva con gli avvisi.

## Dubbi

1. **RIP-H-26 parziale.** Lo stato avanza, la conferma dice ancora «Verrà aperto WA "scheda pronta
   firma".» (il testo della sorgente) ma l'avviso non c'è fino alla T4.6. Ho tenuto il testo com'è;
   se nel frattempo il negozio usa il Go, la frase promette una cosa che non succede.
2. **La conferma si legge dalla busta di adesso.** Lo standalone decide se chiedere la conferma (e la
   frase del rientro) dalla riga della scansione; il Go la rilegge a ogni POST, perché il pannello è
   in un'altra richiesta. Se la busta è cambiata (stato diverso) il Go non chiede la conferma e
   risponde subito «Stato cambiato nel frattempo: ricontrolla.» con il pannello vero; lo standalone
   chiedeva prima la conferma e poi diceva la stessa cosa.
3. **La busta che non c'è o sta nel cestino** al momento dell'azione dà «⚠️ Nessuna riparazione
   trovata» al posto del pannello (lo standalone mostra il pannello vecchio con l'avviso «Nessuna
   riparazione trovata»): il Go non ha più i dati della scansione.
4. **Il doppio tocco** (review M1): `app.js` manda il modulo del pannello una volta sola, senza
   spegnere il pulsante premuto (si perderebbe `conferma=1`). Se il secondo invio arriva lo stesso
   (senza JavaScript, o da un'altra scheda), la busta già nello stato che quell'azione avrebbe
   prodotto vale come riuscita, senza scrivere niente: «✅ Pronta» va a `?edit=prezzo`, le altre
   «✓ …» e la lista; un rientro già avvenuto va bene. Solo uno stato mostrato della famiglia della
   busta vale: un modulo forgiato senza stato resta «busta cambiata».
5. **Il guasto del database** durante un'azione mostra il pannello con «Transizione non consentita.»
   (come `qr_error(_)` dello standalone) e va nelle righe del server, senza il codice; non è un 500.
   Un guasto nella lettura **prima** dell'azione (`RigaPerQR`, o le scelte del negozio) dà invece
   il 500 del Go.
6. **Il rientro e le consegne all'artigiano** oggi vengono solo dai dati importati: il Go non ha
   ancora il rientro a mano né le consegne (area artigiani). La scheda del Go non mostra «fuori
   bottega»: lo dice solo il riquadro «📤 Fuori bottega» della lista.
7. **Dopo «✅ Pronta»** la scheda si apre sul prezzo senza un messaggio, come lo standalone (solo il
   suono, che è della 055).
8. **Le voci RIP-H-23…26** le ho ricostruite dallo standalone e dai titoli dell'elenco pubblico
   (`docs/inventario-elenco.tsv`); il testo privato dell'inventario non l'ho letto.
9. **D-70, il pannello.** Nel Go, anche con JavaScript, la scansione carica una pagina nuova
   (`POST /cerca-qr`) e la lista sparisce; nello standalone il pannello si apre sopra la lista. La
   054 porta le conferme senza server, non il pannello. **Proposta:** aprire il pannello dentro la
   lista con la 055 (il lettore con la fotocamera lavora già sulla stessa pagina), tenendo
   `POST /cerca-qr` come riserva senza JavaScript.
