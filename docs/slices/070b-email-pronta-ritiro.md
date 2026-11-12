# 070b — Email «pronta per il ritiro», avviso email nella scheda, riga della pagina dei provider (T4.6, ottava e ultima slice)

- **Fonte:** lo standalone a `go-baseline` = `302439d`: `lib/gestrip_standalone/email_notice.ex` («The ready email»,
  `notify_ready/2`, `sheet/1`, `ready_bag/1`, `told?/1`, `queue_ready/2`, `ready_content/3`, `ready_miss/3`,
  `ready_state/1`, `ready_row/1`, `last_ready_event/2`), `outgoing.ex` (`destination_for_email_ready/1`,
  `state_label/1`), `workshop.ex` (`announce_ready_by_email/4` e i suoi tre chiamanti: `transact_transition/5`,
  `return_from_artisan/4`, `reopen_repair/3`), `live/repair_live.ex` (`#repair-email-notice`, `email_ready_text/1`,
  `email_miss/1`), e i test `email_ready_notice_test.exs` e `repair_email_notice_live_test.exs`. La voce CLI-42
  dell'inventario, la D-16, la ricevuta della 070 («Cosa resta alla 070b», Dubbio 1), la via del coordinatore.
- **Voci:** CLI-42 (email automatica «pronta per il ritiro» e lo stato nella scheda), con la D-16; ADM-09 (la riga
  della pagina dei provider); CLI-51 (`AccodaIn`).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** `email_pronta_inviata_il`
  c'è dalla 0007, la coda dalla 0008, l'indice `messaggi_in_uscita_busta` (lista degli invii e avviso) dalla 0018;
  le righe d'audit fanno da storia come nello standalone.
- **Con questa slice la T4.6 è finita.**

## Cosa c'è

- **Coda** (`internal/uscita/coda.go`): `Coda.AccodaIn(ctx, tx, …)`, `Accoda` nella transazione di chi chiama:
  stessi rifiuti prima di scrivere (`accodabile`: WhatsApp, messaggio non valido, canale senza provider o
  spento), poi la riga e `outgoing.queued` in `tx` (`scrivi`); `Accoda` è `accodabile` + una transazione sua +
  `scrivi`. L'interfaccia `buste.Posta` ha anche `AccodaIn`.
- **Dominio** (`internal/buste/email_pronta.go`): `avvisaPronta` (= `announce_ready_by_email/4`), `notificaPronta`
  (= `notify_ready/2` con `ready_bag/1` e `queue_ready/2`), `giaDetto` (= `told?/1`), `ultimaPronta`
  (= `last_ready_event/2`), `mancataPronta` (= `ready_miss/3`), `EmailDellaScheda`/`EmailPronta`/`statoPronta`
  (= `sheet/1`, `ready_state/1`, `ready_row/1`), `ConIndirizzoPubblico`, `PostaDisponibile`. In `avvisi.go`
  `garantisciLinkIn`, la parte di `garantisciLink` (= `ensure_public_link/2`) che lavora in una transazione data.
- **Dove parte:** `passaStato` (`stati.go`: «Cambia stato» della scheda, «✓ Pronta» e il menu della lista, «✅
  Pronta» del pannello del QR) e `Riapri` (`riapertura.go`), quando la busta arriva a «pronta» da un altro stato.
  Il rientro dall'artigiano dello standalone non c'è ancora nel Go (T4.8, RIP-S-16): `docs/SVILUPPO.md` dice che
  dovrà chiamare `avvisaPronta`. «✅ Valutata» del pannello del QR (compro oro) passa da `passaStato` ma non
  arriva a «pronta»: niente email, come lo standalone («never a compro oro card»). Gli altri cambi di stato del Go
  (cambio di tipo, firma di consegna, preventivo dal QR) non arrivano mai a «pronta».
- **Nella stessa transazione (solo Go):** l'email si scrive nella transazione del cambio di stato, dentro
  `SAVEPOINT email_pronta`: link stabile, riga della coda, segno `email_pronta_inviata_il` (con `aggiornato_il`) e
  `repair.email_ready_queued` (`bag_number`, `message_id`, `party`). Il nuovo stato e l'email si scrivono insieme o
  niente. Un'email che non parte torna indietro fino al SAVEPOINT (anche il link nuovo) e lascia
  `repair.email_ready_not_queued` (`bag_number`, `reason`: `invalid_template`, `not_configured`, `error`), senza mai
  togliere il nuovo stato; solo un SAVEPOINT che non si può disfare annulla il cambio (database rotto).
- **Una volta sola** (come lo standalone): il segno; un «pronta» dopo non accoda niente (pronta → altro → pronta, la
  prima accettata o ancora in coda), a meno che l'ultima email di pronta del Go (dopo l'ultima richiesta non
  «Reinvia» o disattivazione) sia `failed`: allora il «pronta» dopo ne accoda una nuova. Un segno importato senza
  righe del Go, o una riga scartata dal titolare (D-27, la riga non c'è più), conta come detta. Un secondo tocco del
  pannello del QR o il menu della lista sullo stesso stato non cambiano lo stato e non scrivono niente. I nuovi
  tentativi della coda riprovano la stessa riga, mai una seconda.
- **A chi:** solo all'indirizzo confermato della controparte (`richiedente` = `address_party/1`): il cliente, o
  l'oreficeria che l'ha chiesto dal suo `/r/` (con il suo nome); mai un indirizzo importato su una busta conto
  terzi, il cliente di una busta passata a un'oreficeria, l'oreficeria di una busta tornata al cliente, un
  indirizzo in attesa o disattivato. L'indirizzo importato di una busta diretta è del cliente.
- **D-16 (solo Go):** solo con l'email disponibile (`PostaDisponibile`: canale acceso e configurato, chiave dei
  link, `indirizzo_pubblico` delle buste). Senza, niente si scrive, e il «pronta» dopo con l'email accesa riprova.
- **Testi:** `email_pronta_oggetto`/`email_pronta_testo` dell'editor (067), `{nome}` («cliente» senza),
  `{numero}` della D-82 («0045/26» per un'etichetta), `{descrizione}`, `{negozio}`, `{link_stato}` =
  `indirizzo_pubblico/r/<token>` del link stabile (rinnovato con `repair.public_link_issued` se scaduto o
  revocato), oggetto su una riga, il piè con il link per disattivare (`piedeEmail` della 070). Un testo salvato
  rotto è un mancato invio, mai il predefinito. Nessun indirizzo nell'audit, nessun testo d'errore nel registro.
- **Disattivazione e indirizzo nuovo:** già della 070 (`togliEmail`, `ritiraInCoda`): una disattivazione (pagina,
  piè di ogni email compresa quella di pronta, «Revoca link») o un indirizzo nuovo fanno `failed` anche l'email di
  pronta ancora in coda per l'indirizzo di prima («Avvisi email disattivati dal destinatario»); lo stesso
  indirizzo chiesto di nuovo lascia stare la coda.
- **Scheda** (`internal/web/email_pronta.go`, riquadro `#repair-email-notice` in `scheda.html`, dopo l'avviso di
  ritiro, classe `riquadro-avviso` già esistente): «📧 Avviso email», l'indirizzo e «In attesa di conferma» o
  «Confermato»; `#repair-email-ready-pending` «Email di pronta: partirà da sola al passaggio a «pronta», una
  volta.» per un indirizzo confermato della controparte prima di ogni email di pronta e fuori da «pronta»;
  `#repair-email-ready` con le parole della coda («In coda dal», «Accettato dal provider il», «Fallito il …
  Riparte al prossimo passaggio a «pronta».»), «non accodata: posta SMTP non configurata / testo dell'email non
  valido nelle Impostazioni / errore. Riparte…», «inviata dal gestionale precedente il» (segno importato). Ore del
  negozio. Solo Go (D-16): con l'email non disponibile, al posto della promessa, `#repair-email-ready-off`
  «Email di pronta: l'email non è disponibile (Impostazioni › Provider): al passaggio a «pronta» non partirà.».
- **Pagina dei provider** (Dubbio 1 della 070): con l'invio acceso e senza `indirizzo_pubblico`,
  `#smtp-switch-no-public` «Senza indirizzo pubblico il riquadro «avvisami via email» della pagina del cliente non
  compare e l'email di pronta non parte: manca «indirizzo_pubblico = https://host[:porta]» in <file>, poi riavvia
  gestrip serve con «…».» (la frase della D-46 di `comeConfigurare`). Spento, o con l'indirizzo, niente.
- **`serve`:** `dipendenzeWeb` dà alle buste `conf.IndirizzoPubblico` (`ConIndirizzoPubblico`), lo stesso della
  pagina del cliente.

### Differenze dichiarate (senza D nuova)

- **La transazione:** lo standalone accoda dopo il commit del cambio di stato, in una transazione sua (un arresto
  fra i due commit perde l'email; il «pronta» dopo riprova). Qui nella stessa transazione con un SAVEPOINT, come
  ha chiesto il coordinatore: insieme o niente, e un'email mancata non toglie lo stato, come lì.
- **D-16:** con l'email spenta o senza indirizzo pubblico lo standalone scrive `repair.email_ready_not_queued`
  `not_configured` e la scheda lo dice; il Go non scrive niente e la scheda, prima del «pronta», dice che non
  partirà.
- **Errore:** lo standalone scrive nel registro il messaggio dell'eccezione («ready email not queued: …»); qui il
  registro non ha niente e la riga del mancato invio ha `reason` `error`, senza testo.

## Prove

`uscita` (`accoda_in_test.go`, 1): `TestAccodaIn` (invisibile prima del commit, il rollback toglie riga e
`outgoing.queued`, il commit le scrive come `Accoda`, i rifiuti in una transazione che si conferma non scrivono
niente, `Accoda` com'era).
`buste` (`email_pronta_test.go`, 11): `TestEmailPronta` (testo esatto con D-82, link stabile, piè, segno con
l'istante del cambio, audit senza indirizzo), `TestEmailProntaUnaVolta` (accettata, ancora in coda, scartata),
`TestEmailProntaFallitaRiapre`, `TestEmailProntaNiente` (in attesa, senza indirizzo, disattivata, segno
importato, altri stati, D-16 spenta e senza indirizzo pubblico, la porta su una busta non pronta e nel cestino),
`TestEmailProntaContoTerzi` (oreficeria con il suo nome, importato conto terzi, passata, tornata, importato
diretto, l'ultima richiesta), `TestEmailProntaTutteLeVie` (pannello del QR, «✓ Pronta», menu della lista,
riapertura; secondo tocco e stesso stato senza scrivere), `TestEmailProntaNonAccodata` (testo rotto, coda che
rifiuta, errore del database con un trigger: stato «pronta», link e segno tornati indietro, nessun dato nel
registro), `TestEmailProntaStessaTransazione` (gancio di prova prima del commit: nella transazione stato, riga,
segno e righe d'audit; senza commit niente), `TestEmailProntaTesti` (link scaduto rinnovato, «cliente», testi
salvati con tutti i campi), `TestEmailProntaDopoDisattivazione` (pagina e piè tolgono l'email in coda, indirizzo
nuovo avvisato, stesso indirizzo lascia la coda), `TestEmailDellaScheda` (nil, in attesa, confermato, spenta, in
coda, accettata, fallita, la scheda la porta, riga tolta, non accodata, importato, altra parte).
`web` (`email_pronta_test.go`, 3): `TestAvvisoEmailNellaScheda` (riquadro esatto, il «Cambia stato» della scheda
accoda, in coda/accettata/fallita/non accodata/in attesa/importato/altra parte/D-16, posto dopo l'avviso di
ritiro), `TestTestoEmailProntaNonAccodata`, `TestProviderSenzaIndirizzoPubblico`.
`cmd/gestrip`: `TestDipendenzeWeb` (le buste di `serve` hanno l'indirizzo pubblico: posta disponibile con, non
senza).

**Rosso, preso prima del codice:** prima le firme vuote (stub: `AccodaIn` che rifiuta, `notificaPronta` ed
`emailDellaScheda` che non fanno niente, `PostaDisponibile` falso, nessun riquadro, nessuna riga dei provider,
nessun `ConIndirizzoPubblico` in `serve`), poi i test: **16 rossi su 16** (1 in `uscita`, 11 in `buste` lanciati
uno per uno, 3 in `web`, 1 in `cmd/gestrip`), tutti sulle attese, nessuno per la compilazione; log in
`to-be-deleted-after-use/slice-070b/prove/rosso*.log`. Un primo giro di `TestEmailProntaUnaVolta` andava in panico
su un indice (corretto il test con un `Fatalf` prima dell'indice, poi rosso sulle attese). Dopo il codice tutti
verdi al primo giro, tranne l'attesa di `TestAvvisoEmailNellaScheda` sulla riga D-16 (il testo fisso del modello
non scappa l'apostrofo: corretta l'attesa). Per le mutazioni sopravvissute (sotto) tre casi in più dopo il
codice: l'orologio spostato prima del giro in `TestEmailDellaScheda` (M26), il mancato invio in mezzo in
`TestEmailProntaFallitaRiapre` (M37), la busta già pronta in `TestAvvisoEmailNellaScheda` (M28).

## Mutazioni

Script `to-be-deleted-after-use/slice-070b/mutazioni/muta.py` su una copia del repo (go build, poi i test di `buste`, `uscita`, `web` e `cmd/gestrip` della slice con un tempo massimo; il file torna com'era dopo ogni prova). **42 mutazioni che compilano: 39 uccise, 3 equivalenti, 0 sopravvissute senza motivo.** Una prima versione non compilava (M27, riscritta come M27b). Al primo giro 6 sopravvissute: 3 equivalenti (M2, M36, M38) e 3 (M26, M28, M37) uccise dopo con test nuovi (`giro2.log`). M21 (accodare in una transazione sua dentro quella del cambio) blocca il collegamento di scrittura: la uccide il tempo massimo dei test.

| # | Mutazione | Esito |
|---|---|---|
| M1 | passaStato senza email di pronta | uccisa (`TestEmailPronta`, `TestEmailProntaUnaVolta`, `TestEmailProntaFallitaRiapre` e altri 4) |
| M2 | passaStato senza il controllo «da un altro stato» | **equivalente**: il grafo non permette «pronta» → «pronta» (rifiuto prima), e lo stesso stato esce prima di scrivere |
| M3 | Riapri senza email di pronta | uccisa (`TestEmailProntaTutteLeVie`) |
| M4 | porta senza il controllo dello stato | uccisa (`TestEmailProntaNiente`) |
| M5 | anche un indirizzo in attesa | uccisa (`TestEmailProntaNiente`) |
| M6 | anche l'indirizzo dell'altra parte | uccisa (`TestEmailProntaContoTerzi`) |
| M7 | told? sempre falso | uccisa (`TestEmailProntaUnaVolta`, `TestEmailProntaNiente`) |
| M8 | una riga fallita non riapre | uccisa (`TestEmailProntaFallitaRiapre`) |
| M9 | ultima riga anche prima della disattivazione o della richiesta | uccisa (`TestEmailProntaDopoDisattivazione`) |
| M10 | senza la D-16 | uccisa (`TestEmailProntaNiente`) |
| M11 | link_stato senza indirizzo pubblico | uccisa (`TestEmailPronta`, `TestEmailProntaTesti`) |
| M12 | link stabile non garantito | uccisa (`TestEmailPronta`, `TestEmailProntaTesti`) |
| M13 | testo rotto con motivo «error» | uccisa (`TestEmailProntaNonAccodata`, `TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M14 | coda che rifiuta con motivo «error» | uccisa (`TestEmailProntaNonAccodata`) |
| M15 | niente segno | uccisa (`TestEmailPronta`, `TestEmailProntaUnaVolta`, `TestEmailProntaStessaTransazione`) |
| M16 | niente riga email_ready_queued | uccisa (`TestEmailPronta`, `TestEmailProntaUnaVolta`, `TestEmailProntaFallitaRiapre` e altri 4) |
| M17 | parte sempre cliente nell'audit | uccisa (`TestEmailProntaContoTerzi`) |
| M18 | nessun ritorno al SAVEPOINT | uccisa (`TestEmailProntaNonAccodata`, `TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M19 | un'email mancata toglie il nuovo stato | uccisa (`TestEmailProntaNonAccodata`, `TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M20 | niente riga del mancato invio | uccisa (`TestEmailProntaNonAccodata`, `TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M21 | Accoda in una transazione sua (fuori dal cambio) | uccisa ((tempo scaduto)) |
| M22 | AccodaIn senza outgoing.queued | uccisa (`TestEmailPronta`, `TestEmailProntaStessaTransazione`, `TestAccodaIn` e altri 1) |
| M23 | AccodaIn senza i rifiuti | uccisa (`TestAccodaIn`) |
| M24 | scheda: controparte sempre | uccisa (`TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M25 | scheda: email sempre disponibile | uccisa (`TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M26 | scheda: accettata con l'ora dell'accodamento | sopravvissuta → `TestEmailDellaScheda` con l'orologio spostato di un'ora prima del giro: uccisa |
| M27 | scheda: niente segno importato | non compila (`il` non usato): riscritta come M27b, non contata |
| M27b | scheda: niente segno importato (riscritta perché compili) | uccisa (`TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M28 | scheda: promessa anche su una busta pronta | sopravvissuta → caso «già pronta» in `TestAvvisoEmailNellaScheda`: uccisa |
| M29 | scheda: «il» per «dal» | uccisa (`TestAvvisoEmailNellaScheda`) |
| M30 | motivo del testo rotto perso | uccisa (`TestAvvisoEmailNellaScheda`, `TestTestoEmailProntaNonAccodata`) |
| M31 | provider: riga anche con l'email spenta | uccisa (`TestProviderSenzaIndirizzoPubblico`) |
| M32 | provider: riga anche con l'indirizzo | uccisa (`TestProviderSenzaIndirizzoPubblico`) |
| M33 | serve senza indirizzo pubblico nelle buste | uccisa (`TestDipendenzeWeb`) |
| M34 | scheda: riquadro anche senza indirizzo | uccisa (`TestEmailDellaScheda`, `TestAvvisoEmailNellaScheda`) |
| M35 | porta: anche nel cestino | uccisa (`TestEmailProntaNiente`) |
| M36 | Riapri chiama la porta per ogni stato | **equivalente**: la porta rilegge lo stato in transazione e una busta non pronta è `not_ready`, senza scrivere |
| M37 | told? legge anche i mancati invii | sopravvissuta → mancato invio in mezzo in `TestEmailProntaFallitaRiapre`: uccisa |
| M38 | segno senza aggiornato_il | **equivalente con l'orologio finto**: nella stessa transazione `aggiornato_il` è già l'istante del cambio di stato (scritto da `passaStato`/`Riapri`); con l'orologio vero i due istanti differiscono di qualche microsecondo, senza effetti |
| M39 | Accoda e AccodaIn senza il canale acceso | uccisa (`TestAccodaIn`, `TestAccoda`) |
| M40 | scheda: riga D-16 al posto sbagliato | uccisa (`TestAvvisoEmailNellaScheda`) |
| M41 | niente SAVEPOINT | uccisa (`TestAttesaIndipendenteDalloStato`, `TestEmailPronta`, `TestEmailProntaUnaVolta` e altri 7) |
| M42 | commit anche con il gancio di prova che rifiuta | uccisa (`TestEmailProntaStessaTransazione`) |

## Banco

`s070b`, `fab-225`, `-dati sintetici-import`, al primo giro: **BANCO VERDE**, 64 scenari, 1120 passi, 0 inattese,
0 errori; **BANCO BROWSER VERDE**, 16 scenari, 32 viste, 0 inattese, 0 errori. Nessuno scenario nuovo: il Go del
banco non ha l'email accesa né `indirizzo_pubblico` e nessuna busta ha un indirizzo confermato (D-16; mai un SMTP
configurato, nemmeno `.invalid`), e il banco HTTP non può cambiare lo stato nello standalone (LiveView). Quello che
si può confrontare sta nel giro browser `whatsapp`, che porta già la 39/26 a «pronta» dalla scheda nelle due app:
conta anche `repair.email_ready_queued` e `repair.email_ready_not_queued` prima e dopo (0 nelle due app, nelle due
viste) e guarda che la scheda non abbia `#repair-email-notice` (`pronta.email` «0» nelle due). Così il binario vero
passa dal SAVEPOINT a ogni cambio di stato del banco (scheda, lista, pannello del QR), senza differenze. Nessuna
riga nuova in `bench/deviazioni.json`. `banco giu`, `banco controlla`: nessun avanzo, nessun container.

## Controlli

`scripts/check` verde prima delle mutazioni e alla fine, dopo test, documenti e banco: copertura 36 voci chiuse
(35 dopo la 070: in più CLI-42, che non ha scenari del banco nell'elenco). `scripts/parole-vietate --file` sui file
cambiati: verde.

## Per il manuale

- Sezione «Avviso email sulla pagina del cliente (slice 070, 070b)» di `docs/manuale/da-scrivere.md`: l'email
  «pronta per il ritiro» (quando parte, una volta sola, a chi, con l'invio spento), il riquadro «📧 Avviso email»
  della scheda con le sue frasi, la riga della pagina dei provider senza indirizzo pubblico.

## Dubbi

1. **Email spenta (D-16) al passaggio a «pronta»:** il Go non scrive niente (lo standalone scrive
   `repair.email_ready_not_queued` `not_configured` e la scheda dice «non accodata: posta SMTP non configurata»).
   Prima del «pronta» la scheda dice «Email di pronta: l'email non è disponibile (Impostazioni › Provider): al passaggio a
   «pronta» non partirà.» (testo neutro dopo la review, B2); dopo, su una busta già pronta, non dice niente dell'email. Il «pronta» dopo con l'email accesa
   riprova. **Raccomandazione:** tenerlo (D-16: nessuna riga d'errore per chi non usa l'email).
2. **Stessa transazione invece di due:** lo stato e l'email si scrivono insieme (SAVEPOINT); l'unico caso in cui
   l'email toglie il cambio di stato è un database che non riesce a disfare il SAVEPOINT (allora non si
   scriverebbe nemmeno il cambio). **Raccomandazione:** tenerlo così.
3. **Email scartata dal titolare (D-27, «Scarta»):** la riga non c'è più e il «pronta» dopo non ne manda un'altra
   (come lo standalone: solo una riga `failed` riapre l'avviso). **Raccomandazione:** tenerlo; chi scarta lo fa
   apposta.
4. **Compro oro con un indirizzo confermato:** come lo standalone, la scheda promette «partirà da sola al passaggio
   a «pronta»» anche su una busta compro oro, che a «pronta» non arriva mai (e l'email non parte). Caso raro.
   **Raccomandazione:** togliere la promessa sulle buste compro oro (una condizione), con l'ok di Stefano.
5. **Testi solo del Go** (la riga D-16 della scheda e la riga della pagina dei provider senza indirizzo pubblico):
   scritti dall'implementatore. **Raccomandazione:** tenerli e rivederli con le schermate del manuale.
6. **Banco:** l'email di pronta riuscita la provano solo i test Go, come per la 070 (Dubbio 8 lì).
   **Raccomandazione:** la stessa della 070, una seconda istanza Go con l'email accesa e un SMTP `.invalid` in una
   slice del banco.
7. **Per il coordinatore (non una scelta):** il rientro dall'artigiano (T4.8, RIP-S-16) dovrà chiamare
   `avvisaPronta` nella sua transazione quando l'esito porta la busta a «pronta»; scritto in `docs/SVILUPPO.md`.

## Correzioni dopo la review (12/11/2026)

- **MEDIA M1, corretto:** una riga `repair.email_ready_queued` che punta a una riga della coda che non c'è più
  (scartata dal titolare dalla pagina dei provider, D-27) dava `nil`, e la scheda prometteva «partirà da sola…»
  mentre `giaDetto` la conta come detta e al «pronta» dopo non parte niente. Ora `statoPronta` dà lo stato
  `discarded` (solo Go: lo standalone non ha «Scarta») e la scheda scrive «Email di pronta: tolta dalla coda dal
  titolare.», senza promesse. Lo scarto continua a contare come detto (Dubbio 3, decide Stefano). Test:
  `TestEmailDellaScheda` (riga tolta → `discarded`) e il caso «tolta» di `TestAvvisoEmailNellaScheda`.
- **BASSA B1, corretto:** in `TestAvvisoEmailNellaScheda`, dopo «accettata», la busta torna in lavorazione e la
  scheda non ha `repair-email-ready-pending` (uccide R15).
- **BASSA B2, corretto:** la riga D-16 della scheda ora è neutra, perché vale anche senza `indirizzo_pubblico`:
  «Email di pronta: l'email non è disponibile (Impostazioni › Provider): al passaggio a «pronta» non partirà.».
- **BASSA B3, corretto:** `TestEmailProntaContoTerzi` controlla anche che `outgoing.queued` della riga
  dell'oreficeria abbia `"party":"oreficeria"` (uccide R24b).
- **BASSA B4, corretto:** M38 è scritta «equivalente con l'orologio finto» nella tabella.
- **Rosso preso prima del codice** (`prove/rosso-review.log`): `TestEmailDellaScheda` (M1) e
  `TestAvvisoEmailNellaScheda` (M1 e B2) rossi; i casi di B1 e B3 provano comportamenti già giusti, e la loro forza
  è nelle mutazioni. Dopo il codice tutti verdi.
- **Mutazioni rifatte** (`mutazioni/giro-review.log`, copia nuova del repo, poi cancellata): R15 (promessa anche
  dopo un'email di pronta) uccisa da `TestAvvisoEmailNellaScheda`; R24b (`outgoing.queued` sempre del cliente)
  uccisa da `TestEmailProntaContoTerzi`; R-M1 (riga scartata come nessuna email) uccisa da `TestEmailDellaScheda` e
  `TestAvvisoEmailNellaScheda`.
- **Banco non rifatto:** le correzioni toccano solo frasi della scheda che compaiono con un indirizzo email sulla
  busta, e nel banco nessuna busta ne ha uno (il giro browser `whatsapp` guarda proprio che il riquadro non ci
  sia); i test e gli scenari del banco non cambiano.
- `scripts/check` verde dopo le correzioni.

## Review
Revisore indipendente, 12/11/2026 (pc-fisso, slice non committata). Nessuna ALTA. Mutazioni del revisore: 25
che compilano e valide, 20 uccise, 5 sopravvissute (R15 e R24b → test nuovi, ora uccise; R03, R17, R25
equivalenti). Il rosso della ricevuta (16 test caduti sulle attese) ricontato dai log: torna.

- **MEDIA, corretto (M1):** dopo uno scarto dalla pagina dei provider (D-27, solo Go) la scheda prometteva
  «partirà da sola…» ma al «pronta» successivo non partiva niente. Ora «Email di pronta: tolta dalla coda dal
  titolare.», senza promessa.
- **BASSA, corrette:** B1 (test della promessa dopo un'email di pronta), B2 (riga D-16 della scheda neutra: la
  causa può essere l'indirizzo pubblico, non solo l'invio spento), B3 (`outgoing.queued` con la parte
  dell'oreficeria), B4 (M38 equivalente solo con l'orologio finto).
- **Domande di prodotto:** lo scarto conta come «già detta» o riapre l'avviso (Dubbio 3); promessa sulle buste
  compro oro (Dubbio 4, il revisore è d'accordo a toglierla con l'ok di Stefano).
- **Verificato senza rilievi:** `AccodaIn` e SAVEPOINT (stato ed email insieme, link nuovo disfatto, mai il
  collegamento di scrittura dentro la transazione), righe non committate mai visibili al giro, `aMano` fuori dal
  percorso, 4 cambi insieme = 1 email, tutti i percorsi verso «pronta» (manca solo il rientro dall'artigiano,
  T4.8, scritto in SVILUPPO.md), doppi invii come `email_notice.ex`, destinatario e CLI-28, D-16, D-82, nessun
  indirizzo in audit o registro, escape, banco senza mascherature. `scripts/check` verde.
