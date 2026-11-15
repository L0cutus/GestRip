# 082 — Storico delle consegne, «Elimina dallo storico», «Sgancia» e giri sulla scheda (T4.8, settima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/handover_history_live.ex`
  (`:index`, `:show`, `delete_form/1`, `page_query/2`, `bag_state/1`, `state_word/1`, `truncate/1`),
  `controllers/handover_history_controller.ex` (`delete/2`, `detach/2`), `workshop/handover_history.ex` (`build/1`,
  `normalize/1`, `apply_filters/2`, `page_rows/2`, `session/1`, `bags/1`, `reopened/1`, `detached_by_hand/1`),
  `workshop.ex` (`delete_handover_from_history/2`, `detach_bag_from_history/3`, `record_handover_history_view/2`,
  `record_handover_history_detail_view/2`, `audited_filter_keys/2`), `repair_live.ex` (`#repair-artisan-cycles`,
  `past_cycle?/1`, `cycle_session/1`), `router.ex` (ordine delle rotte, `:require_owner_user`), `user_auth.ex`
  (`require_owner/2`).
- **Voci:** ART-15, ART-16, ART-17, ART-18, ART-19 (riquadro sulla scheda), RIP-S-17 (parte giri); ART-22 (i
  solleciti sganciati restano). Nessuna D nuova.
- **Lavoro:** sottoagente d'implementazione, test rossi prima (dominio e pagine), mutazioni a mano. Il banco non è
  stato lanciato: lo fa la sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/storico.go`):
  - `LeggiFiltriStorico` = `normalize/1` (stato solo «aperte»/«chiuse» com'è scritto, artigiano e pagina con
    `Id.positive/1`, date con `Date.from_iso8601/1` dopo `String.trim/1`), `ChiaviRegistroStorico`;
  - `StoricoConsegne` = `build/1`: tutte le consegne dalla più recente (a pari apertura l'id più alto), con
    artigiano, buste e «ancora fuori» senza il cestino; date d'apertura sui giorni del negozio, estremi compresi
    (`start_of_local_day/1`; un anno sotto lo zero o oltre il 9999 non limita); 30 per pagina, una pagina oltre la fine
    è l'ultima;
  - `RegistraStorico` (`handover.history_viewed`, `handover_history`/`index`, `filter_keys`) e
    `RegistraDettaglioStorico` (`handover.history_detail_viewed` sulla consegna, senza dati);
  - `DettaglioStorico` = `session/1`: la consegna, le buste fuori dal cestino per numero come testo (poi id), e le
    **riaperte dalla tabella dei giri** (giri chiusi della consegna, non sganciati, di buste fuori dal cestino ora in
    un'altra consegna o in nessuna, dal rientro più recente, a pari istante nell'ordine dell'audit);
  - `EliminaDalloStorico` = `delete_handover_from_history/2`: solo una consegna chiusa; nella transazione conta le buste
    vive, sgancia tutte le buste (anche nel cestino), **poi i solleciti** (`ON DELETE RESTRICT` dalla 0008: restano con
    busta, artigiano e giorni), cancella la consegna e scrive `handover.deleted` con `bags_detached`,
    `discarded_bags_detached`, `reminders_unlinked`; i giri non cambiano;
  - `SganciaDalloStorico` = `detach_bag_from_history/3`: solo una consegna chiusa; la busta fuori dal cestino e di
    quella consegna perde il collegamento, `handover.bag_detached` con `repair_id`, e i giri chiusi della coppia
    diventano `sganciata = 1` (quello che la ricostruzione della 081 legge dalla stessa riga). Nessuna delle due
    scritture tocca `aggiornato_il`, come `update_all/3`.
- **Pagine** (`internal/web/storico.go`, `pagine/storico.html`): l'elenco (ritorno alle consegne, modulo GET dei
  filtri, righe con giorno, artigiano, stato «chiusa»/«in corso»/«in raccolta», buste, chiusura, «N ancora fuori»,
  «← Precedenti»/«Successive →» con i filtri nell'ordine di `page_query/2`); il dettaglio (titolo, apertura e chiusura,
  «🖨 Ristampa» e «📤 Condividi» solo con delle buste, le buste con stato, descrizione tagliata a 50 con tolleranza 5,
  uscita, rientro, nota; «Buste riaperte in un ciclo nuovo»); «✕ Elimina dallo storico» (elenco e dettaglio) e
  «✕ Sgancia» solo al titolare e solo su una consegna chiusa, con le domande dello standalone (`data-confirm`) e
  `data-invio-unico`; esiti e rifiuti con i messaggi del controller e i suoi redirect. Il link «📋 Storico consegne»
  della lista delle consegne e la voce del menu compaiono da soli (`haRotta`).
- **Rotte:** `GET /handovers/history`, `GET /handovers/{storico}/{id}` (vedi Scelte), `POST
  /handovers/history/{id}/delete` e `…/detach` `soloTitolare` (righe della tabella dei permessi, che le aveva già).
- **Scheda:** il riquadro «🔁 Cicli dall'artigiano» (`#repair-artisan-cycles`) dopo la nota dell'ultimo rientro, solo
  con un giro passato: «Consegna #N» o «Consegna senza sessione», l'artigiano («artigiano» senza), «ciclo corrente»,
  uscita e rientro nell'ora del negozio («—» se sconosciuti), «📝 nota registrata» solo su un giro passato
  (`disegnaScheda` legge `GiriDellArtigiano`).
- **Stile:** classi nuove in `app.css` (filtri in griglia, stato colorato come i badge, correzioni in rosso, riaperte su
  fondo grigio, riquadro dei giri), limitate alle liste dello storico.

## Scelte

- **Rotte del titolare come lo standalone, non `f.azioneDelTitolare`.** Le due correzioni nello standalone sono POST
  di un controller dietro `:require_owner_user`: il dipendente va a `/` con «Operazione riservata al titolare.». La
  tabella dei permessi del Go le aveva già `soloTitolare` senza messaggio proprio; `azioneDelTitolare` è per gli
  eventi di una pagina comune con un rifiuto loro (Dubbio 1).
- **Dettaglio su `GET /handovers/{storico}/{id}`.** Il mux di Go va in panic con `GET /handovers/history/{id}` accanto
  a `GET /handovers/{id}/labels.pdf` (tutti e due prendono `/handovers/history/labels.pdf`, nessuno è più preciso). Con
  due segnaposto il modello è più generale di `labels.pdf`, quindi `/handovers/history/labels.pdf` va al PDF della
  consegna «history» (404, come lo standalone, dove `HandoverLabelController` viene prima nel router), e un primo
  segmento diverso da «history» è il 404 (Dubbio 3).
- **Senza «⏰ Solleciti pendenti» finché non c'è la 083** (revisione, BASSA 1): lo standalone mette il link nella barra
  di ritorno dello storico; qui compare da solo quando la rotta `GET /handovers/reminders` è registrata
  (`Solleciti: f.haRotta(...)` in `paginaStorico`, `storico.html`), come il link della lista delle consegne.
- **Le riaperte dalla tabella** (piano della 076): restano dopo la pulizia dell'audit (D-14), provato.
- **Prima la riga d'audit del dettaglio, poi la lettura**, come `handle_params :show`: un id leggibile che non c'è si
  registra; un id illeggibile no.

## Prove

- `internal/buste/storico_test.go`: `riaperteDallAudit` è `reopened/1` riscritta in Go senza SQL in comune con
  `storico.go` (comprese le due stranezze di SQLite e `Integer.parse/1`, Dubbio 2). `TestStoricoConsegne` (ordine,
  conteggi senza cestino, filtri con i confini di mezzanotte del negozio, anni fuori dalle quattro cifre, 30 per pagina,
  pagina oltre la fine), `TestLeggiFiltriStorico`, `TestRegistriDelloStorico`, `TestDettaglioERiaperteDalVivo`
  (conferma, rientri, riapertura in un'altra consegna, ritorno dal cestino, rifiuti dello sgancio senza riga d'audit,
  sgancio con `stessiGiri` della 081 — tabella dal vivo uguale alla ricostruzione — riaperte uguali a `reopened/1` a
  ogni passo, dal rientro più recente, una riaperta nel cestino che sparisce, e tolto l'audit le stesse riaperte),
  `TestRiaperteInventate` (la storia inventata della 081: uguali a `reopened/1` per ogni consegna tranne la 7, Dubbio
  2), `TestEliminaDalloStorico` (rifiuti, buste anche dal cestino, solleciti sganciati che restano, riga d'audit,
  giri invariati, `aggiornato_il` invariato).
- `internal/web/storico_test.go`: `TestElencoStorico` (vuoto, righe, stati, pulsante solo al titolare sulle chiuse,
  filtri scelti, chiavi d'audit senza valori né liste), `TestPagineStorico`, `TestDettaglioStorico` (titolo, istanti del
  negozio, buste, nota, riaperte, correzioni solo al titolare su una chiusa, «in raccolta», consegna che non c'è con la
  riga d'audit, id illeggibili senza, `/handovers/history/labels.pdf` 404), `TestCorrezioniDelloStorico` (dipendente a
  `/` con «Operazione riservata al titolare.» e niente cambia, 403 senza token, tutti i rifiuti con i loro redirect,
  esiti), `TestRiquadroGiriSullaScheda`.
- Test aggiornati: `TestPaginaConsegne` (il link allo storico c'è), i tre test del menu, `TestRotteConGuscio`, la
  tabella dei permessi; `tools/banco`: `TestScenariBrowserDelRepo` (scenario nuovo e attese), `TestScenarioUtentiSuiDueDati`
  (gli scenari dello storico guardano la consegna 1 della cattura).

## Mutazioni

25 mutazioni a mano, una alla volta su una copia del repo (`to-be-deleted-after-use/082-storico/mutazioni/`:
`muta.py`, `giro1.log`, `giro2.log`, `esiti/<id>.log`; copia cancellata a fine giro). Primo giro: **22 prese su 25**;
M03 e M04 sopravvissute (la busta nel cestino del test era ancora nella consegna, e le due riaperte rientravano nello
stesso istante): aggiunti al test una riaperta che va nel cestino e un minuto fra i due rientri; M22 non compilava
(variabile inutilizzata: riscritta). Secondo giro su M03, M04, M22: **prese tutte e tre**. Totale **25 su 25**.

| Id | Mutazione | Presa da |
|---|---|---|
| M01 | riaperte anche sganciate | TestDettaglioERiaperteDalVivo, TestRiaperteInventate |
| M02 | riaperte anche ancora nella consegna | TestDettaglioERiaperteDalVivo, TestRiaperteInventate |
| M03 | riaperte anche nel cestino | TestDettaglioERiaperteDalVivo (2° giro) |
| M04 | riaperte dalla meno recente | TestDettaglioERiaperteDalVivo (2° giro) |
| M05 | elenco a pari apertura per id crescente | TestStoricoConsegne |
| M06 | `data_a` esclusa | TestStoricoConsegne |
| M07 | `data_da` esclusa | TestStoricoConsegne |
| M08 | buste contate con il cestino | TestElencoStorico, TestStoricoConsegne |
| M09 | pagina oltre la fine non riportata | TestPagineStorico, TestStoricoConsegne |
| M10 | stato qualunque | TestLeggiFiltriStorico |
| M11 | «Elimina» non sgancia il cestino | TestCorrezioniDelloStorico, TestEliminaDalloStorico |
| M12 | «Elimina» non sgancia i solleciti | TestEliminaDalloStorico |
| M13 | buste del cestino contate male | TestEliminaDalloStorico |
| M14 | «Elimina» di una consegna aperta | TestCorrezioniDelloStorico, TestEliminaDalloStorico |
| M15 | «Sgancia» segna i giri aperti | TestDettaglioERiaperteDalVivo |
| M16 | «Sgancia» anche dal cestino | TestDettaglioERiaperteDalVivo |
| M17 | sgancio con l'id della consegna | TestDettaglioERiaperteDalVivo |
| M18 | correzioni al dipendente nel dettaglio | TestDettaglioStorico |
| M19 | «Elimina» al dipendente nell'elenco | TestElencoStorico |
| M20 | id di busta illeggibile all'elenco | TestCorrezioniDelloStorico |
| M21 | «Elimina» del personale | TestCorrezioniDelloStorico, TestOgniRottaHaIlSuoPermesso |
| M22 | riquadro dei giri senza un giro passato | TestRiquadroGiriSullaScheda (2° giro) |
| M23 | «nota registrata» anche sul giro attuale | TestRiquadroGiriSullaScheda |
| M24 | chiavi d'audit della lista invece dello storico | TestElencoStorico |
| M25 | riga d'audit anche per un id illeggibile | TestDettaglioStorico |

## Controlli

`scripts/check` (gofmt, vet, staticcheck, errcheck, build senza CGO, tutti i test, copertura dell'inventario,
segreti): il primo giro si è fermato a gofmt (quattro file), corretti; il secondo verde dopo le mutazioni e l'ultima
modifica del codice (`to-be-deleted-after-use/082-storico/prove/check2.log`, «uscita 0»): copertura «voci 353:
chiuse 43, rosse 0, parziali 152, aperte 147, fuori v1 11».

## Banco

**Da fare dalla sessione principale** (qui non lanciato). Scenari nuovi:

- HTTP `storico-consegne` (titolare): elenco vuoto e con la consegna 1 della cattura (aperta, «in corso», «1 ancora
  fuori»), filtri (artigiano, stato, date, valori illeggibili, pagina oltre la fine), dettaglio della consegna 1
  (`normalizza: D-64`), consegna che non c'è e id illeggibile, `/handovers/history/labels.pdf`, le correzioni rifiutate
  sulla consegna 1 aperta (nessun dato cambia), «ID busta non valido.».
- HTTP `storico-consegne-dipendente`: elenco e dettaglio senza pulsanti, i due POST forgiati a `/` con «Operazione
  riservata al titolare.», la consegna 1 com'era.
- Browser `storico-correzioni` su dati propri (due buste del 2025 per vista con QR `SINT-STORICO-082-<vista>-A|B`, una
  consegna di «Incastonatore Sintetico» chiusa da due rientri «Nessun cambio di stato», «✕ Sgancia» sulla B e «✕ Elimina
  dallo storico» con le domande; alla fine le due buste senza artigiano). Sul PC gira dopo `sessioni-rientro` e
  `sfondo`, prima di `tentativi`.

Da guardare per primi: le righe dell'elenco (`visibile:handover-history-1`: spazi e «·» del testo delle due app),
il dettaglio della consegna 1 con la D-64, e nel browser `dettaglio.righe` (Dubbio 4). Se la LiveView scrivesse le
letture dello storico due volte, non tocca il browser (non le conta) e l'HTTP ne vede una per richiesta.

## Dubbi

1. **`f.azioneDelTitolare` o la rotta del titolare.** Il brief diceva `azioneDelTitolare` per «Sgancia»; lo
   standalone e la tabella dei permessi dicono rotta `soloTitolare` (`:require_owner_user`, il dipendente a `/` con
   «Operazione riservata al titolare.», ART-17: «dipendente … non cambiano nulla»). Ho seguito lo standalone, che è
   anche quello che il banco confronta. **Raccomandazione:** tenere così.
2. **Riaperte con dati strani.** `reopened/1` prende anche un rientro con `handover_id` decimale (7.0: SQLite lo trova
   uguale a 7) e una busta scritta con lo zero o il «+» davanti (`Integer.parse/1`), che `cycles/1` e la tabella della
   081 ignorano. Sono righe che né lo standalone né il Go scrivono (Jason scrive interi, l'id è sempre `to_string`); la
   storia inventata della 081 ha il caso 7.0 e il test lo fissa come unica differenza. **Raccomandazione:** tenere la
   tabella (coerente con i giri della scheda).
3. ~~**Il dettaglio con due segnaposto.**~~ **Chiuso dopo la revisione** (BASSA 2): `rottaConGuscioSe` dà il 404 prima
   della catena quando il primo segmento non è «history», anche a chi non è entrato, come lo standalone; il gestore
   ricontrolla. Prova in `TestDettaglioStorico` (`/handovers/abc/1` 404 per il dipendente e per chi non è entrato).
4. **Ordine delle due buste del browser.** Il dettaglio le ordina per numero come testo: nello standalone sono 8082/25 e
   8083/25 (scritti), nel Go i numeri del contatore del 2025: se nel Go passassero da 9 a 10 («9/25», «10/25») l'ordine
   cambierebbe e `dettaglio.righe` sarebbe diverso senza un errore. **Raccomandazione:** se capita, confrontare le
   righe ordinate.
5. **Doppia lettura della LiveView (non misurato).** `handle_params` dello standalone gira al primo disegno e al
   collegamento: nel browser lo standalone scriverebbe due righe `handover.history_viewed` per visita, il Go una. Lo
   scenario browser non le conta; l'HTTP vede solo il primo disegno. **Raccomandazione:** nessuna azione; da annotare
   se il banco lo mostra.
6. **Anno del numero di una busta nuova con una data di un altro anno.** Lo standalone precompila «N° Busta» con il
   numero dell'anno in corso (`peek_next_bag_number/0`) e lo salva com'è; il Go, che per la D-64 non ha il campo, usa
   l'anno della data di accettazione (`accepted_on_year/1`, quello che lo standalone fa a campo vuoto): una busta
   accettata il 03/06/2025 è «…/26» nello standalone e «…/25» nel Go. La D-64 toglie il campo ma non dice niente
   dell'anno; non ho scritto una D. **Raccomandazione:** tenere il Go (l'anno della busta segue la sua data) e
   confermarlo come conseguenza della D-64.
   **Deciso da Stefano il 15/11/2026: si tiene il Go → D-92.**

## Dopo la revisione e il banco s082p (sessione principale)

Revisione: nessuna ALTA né MEDIA. Banco s082p: HTTP 74/1238/0; browser 21/42 con 64 inattese e un errore, tutti dalla
stessa causa: lo scenario nuovo `storico-correzioni` creava nello standalone buste del **2026** (52/26, 53/26 sul PC,
68/26, 69/26 sul telefono) perché lo standalone precompila «N° Busta» con l'anno in corso (Dubbio 6); sul telefono
`riquadro-azioni` trovava la prima pagina spostata (`TimeoutError` su `selectOption` e 60 campi «(assente)» dello
standalone), e `storico-correzioni` aveva i numeri non normalizzati. Lo stesso valeva per `sessioni-rientro` della 080.
Correzioni:

- `banco.mjs`: `numeroDel2025` scrive nello standalone un numero del 2025 in «N° Busta» (8080/25, 8081/25 per
  `sessioni-rientro`; 8082/25…8085/25 per `storico-correzioni`); commenti d'ordine e `SVILUPPO.md` («Banco: scenari che
  cambiano i dati») dicono che la data del 2025 da sola vale solo per il Go. Le attese restano (`<numero>` copre «#8082/25»
  e «N. 12/25»).
- BASSA 1: riga in «Scelte» (il link ai solleciti arriva con la 083) e promemoria nella ripresa.
- BASSA 2: `rottaConGuscioSe` (sopra, Dubbio 3), test rosso prima (`/handovers/abc/1`: 302 a `/login`), poi verde;
  `percorsoDi` dei test sostituisce `{storico}` con «history».
- BASSA 3: `TestCorrezioniDelloStorico` legge il messaggio seguendo `r.location` (`seguiRedirect`): per la consegna 999
  con l'id di busta illeggibile il messaggio finale è «Consegna non trovata.» (il dettaglio che non c'è rifà il
  redirect), come nello standalone.
- `scripts/check` di nuovo verde (`to-be-deleted-after-use/082-storico/prove/check3.log`). Il banco va rilanciato
  dalla sessione principale.

## Banco finale (sessione principale)

`s082q`, 15/11/2026, istanza nuova dopo le correzioni di `s082p` e della revisione: HTTP 74 scenari, 1238 passi,
0 inattese, 0 errori; browser 21 scenari, 42 viste, 0 inattese, 0 errori. `scripts/check` rifatto dalla sessione
principale: verde.
