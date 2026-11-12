# 070 — Avviso email della pagina del cliente: riquadro, conferma, disattivazione, ripiego D-02 (T4.6, settima slice)

- **Fonte:** lo standalone a `go-baseline` = `302439d`: `lib/gestrip_standalone/email_notice.ex` (`box/2`,
  `request/5`, `resend/3`, `check_confirmation/1`, `confirm/2`, `revoke/3`, `check_stop/1`, `stop/2`, `footer/1`),
  `workshop/email_consent.ex`, `outgoing.ex` (`destination_for_email_confirmation/1`),
  `public_repair_controller.ex` (`email_optin/2`, `legacy_email_optin/2`, `email_optout/2`, `legacy_email_optout/2`,
  `email_confirm/2`, `email_confirm_post/2`, `email_stop/2`, `email_stop_post/2`, `emailed/3`, `revoked/3`),
  `public_repair_html/show.html.heex`, `email_action.html.heex`, `email_outcome.html.heex`, `router.ex`, e i test
  `public_repair_email_test.exs`, `public_repair_email_stop_test.exs`, `email_notice_test.exs`,
  `email_notice_stop_test.exs`. Le decisioni D-02 e D-16 dell'inventario, il PIANO (§2.4, D-02), la via del
  coordinatore per la 070.
- **Voci:** CLI-39, CLI-40, CLI-41 (con D-02 e D-16), CLI-19 (parte email della pagina storica), CLI-17 e CLI-28 (il
  riquadro solo sulla pagina storica di una busta del cliente). **Divisa: CLI-42 («pronta per il ritiro») va alla
  070b** (sotto, «Cosa resta alla 070b»).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** le tre colonne
  (`email_cliente`, `email_confermata_il`, `email_pronta_inviata_il`) ci sono dalla 0007, la coda dalla 0008; le righe
  d'audit fanno da storia delle richieste come nello standalone.

## Cosa c'è

- **Dominio** (`internal/buste/email_cliente.go`, = `EmailNotice`): `NormalizzaEmail`, `EmailDisponibile`,
  `RiquadroEmail`, `ChiediEmailCliente`/`ChiediEmailStorica` (richiesta e «Reinvia conferma»),
  `DisattivaEmailCliente`/`DisattivaEmailStorica` («Disattiva» della pagina), `ControllaConferma`/`ConfermaEmail`,
  `ControllaStop`/`DisattivaConLink` (link del piè), `MandaLinkDisattivazione` (ripiego D-02), `FormaPhoenix`. Le
  buste ricevono la coda e la sottochiave con `ConPosta` (`buste.Posta`: `Accoda`, `CanaleAttivo`); in `serve` è la
  stessa `uscita.Coda` del lavoro e della pagina dei provider, con `db.SottoChiave(chiave, "email")`.
- **Pagine** (`internal/web/email_cliente.go`): le nove rotte pubbliche (sezione nuova di `docs/SVILUPPO.md`), il
  riquadro `#public-email` nei tre stati in `pagina_cliente.html` (anche sulla pagina storica, con la credenziale nei
  campi nascosti, e nell'anteprima del negozio, spento: niente `action`, niente token, dentro il fieldset spento),
  `email_azione.html` (= `email_action`, e il modulo del ripiego) ed `esito_pubblico.html` con «Torna alla tua
  riparazione». Esiti nella sessione della faccia pubblica (`InfoEmail`, `ErroreEmail`), una volta.
- **Testi e coda:** l'email di conferma con i testi dell'editor (`email_conferma_oggetto`, `email_conferma_testo`,
  067), `{numero}` della D-82, `{nome}` di chi ha il link («cliente» senza), oggetto su una riga, il piè dello
  standalone con il link per disattivare; `Accoda` della 068. Una coda che rifiuta o un testo salvato rotto (mai il
  predefinito) lasciano l'indirizzo in attesa con `repair.email_optin_not_queued`.
- **Link firmati dal Go:** `base64url(dati).base64url(HMAC)` con la sottochiave «email», come i gettoni della
  galleria: scopo, busta, impronta con chiave dell'indirizzo (mai l'indirizzo in chiaro), id della riga della
  richiesta per la conferma, istante della firma; 48 ore la conferma, 10 anni la disattivazione.
- **D-16 (solo Go):** riquadro e richiesta solo con l'email disponibile (canale acceso e configurato,
  `uscita.EmailAttiva` tramite `CanaleAttivo`, e `indirizzo_pubblico`, senza il quale i link dell'email non si
  scrivono). Senza, nessun riquadro e una richiesta (anche «Reinvia», anche con il modulo sbagliato) è «⚠️ L'invio
  dell'email non è al momento disponibile. Riprova più tardi.» in cima alla pagina, niente scritto. «Disattiva», la
  conferma e il link del piè funzionano anche con l'email spenta.
- **D-02 (solo Go):** un vecchio link di **conferma** dello standalone (Phoenix.Token) è il 410 «Link non valido o
  scaduto» (dura 48 ore; la frase dice di usare «Reinvia conferma»); un vecchio link di **disattivazione** (GET o il
  suo pulsante) apre la pagina di ripiego «Disattiva gli avvisi email» con il modulo «scrivi la tua email». Il POST
  `/stato/email-ripiego` risponde **sempre la stessa pagina** («Controlla la tua email») e il lavoro parte **dopo**
  la risposta (`Dipendenze.Sottofondo`, una goroutine in `serve` con al più 2 minuti), così né il testo né il tempo
  dicono se l'indirizzo è iscritto. Limite agli abusi: secchio nuovo `ratelimit.RipiegoEmail`, ogni invio conta, 5 in
  15 minuti per indirizzo IP, poi 15 minuti con la stessa risposta e niente lavoro; in più al più 3 link all'ora per
  busta (`repair.email_stop_link_sent`) e al più 10 buste per invio. L'email ha un testo del Go (non nell'editor).
- **Altro:** `audit.ScriviConID` (l'id della riga della richiesta va nel link); `mandaConferma` con
  `context.WithoutCancel` (una connessione chiusa dopo la scrittura non lascia l'indirizzo in attesa senza la riga).

### Differenze dichiarate (senza D nuova)

- La richiesta con l'email disponibile segue l'ordine dello standalone (consenso, indirizzo, busta, limite); con
  l'email non disponibile la risposta è sempre «non disponibile» (D-16).
- Lo standalone, se il provider va in eccezione, scrive il messaggio dell'eccezione nel registro; qui un errore è
  `error` nella riga del mancato invio, senza testo.
- Il link «Torna alla tua riparazione» è `indirizzo_pubblico + /r/<token>` come `Workshop.public_url/1`.

## Cosa resta alla 070b

- **CLI-42:** l'email automatica «pronta per il ritiro» (`notify_ready/2`) dopo ogni passaggio a `pronta` (cambio di
  stato, pannello del QR, rientro dall'artigiano, riapertura), una sola volta (`email_pronta_inviata_il` e
  `repair.email_ready_queued` con l'id della riga nella **stessa transazione** dell'accodamento: serve una
  `Accoda` dentro la transazione del chiamante, che la coda della 068 non ha), `told?/1` con la riga fallita che
  riapre l'avviso, `repair.email_ready_not_queued`, solo all'indirizzo confermato della controparte (riusa
  `richiedente`, `valoriEmail`, `contenutoEmail`, `piedeEmail` di questa slice), `{link_stato}` con
  `ensure_public_link/2`.
- **Lo stato dell'avviso email nella scheda** (`EmailNotice.sheet/1`, `repair_email_notice_live_test.exs`).
- Confine: questa slice chiude tutto ciò che il cliente fa dalla pagina e dai link; la 070b tutto ciò che parte da
  solo dopo un cambio di stato del negozio.

## Prove

`buste` (`email_cliente_test.go`, 20): `TestNormalizzaEmail`, `TestRichiestaEmail` (testo esatto dell'email con
D-82, piè, link senza indirizzo, audit), `TestRichiestaEmailRifiuti` (consenso, indirizzo, link morto, D-16 anche
con il modulo sbagliato e «Reinvia», busta chiusa e nel cestino), `TestRichiestaEmailLimite`, `TestReinviaEmail`,
`TestRichiestaEmailNonAccodata` (`not_configured`, `invalid_template`), `TestRichiestaEmailOreficeria`,
`TestRiquadroEmail` (stati e consegna), `TestEmailDellAltraParte`, `TestConfermaEmail`, `TestConfermaEmailRifiuti`
(falso, rovinato, altra chiave, altro scopo, 48 ore al millesimo, cambiato, tolto, cestino, sparita, sostituita da
disattivazione e richiesta, A→B→A, altra busta, Phoenix), `TestDisattivaEmailPagina`, `TestDisattivaEmailLimite`,
`TestDisattivaConLink` (10 anni al millesimo, cestino, cambiato, sparita), `TestDisattivaConLinkSoloLaSua`,
`TestEmailDallaPaginaStorica`, `TestMandaLinkDisattivazione`, `TestMandaLinkDisattivazioneDieci`, `TestFormaPhoenix`,
`TestEmailNomeEOggetto`.
`web` (`email_cliente_test.go`, 8): `TestRiquadroEmailPagina`, `TestRichiestaEmailPagina`,
`TestRichiestaEmailPaginaRifiuti`, `TestEmailConfermaEDisattiva`, `TestEmailLinkRifiutati`, `TestEmailRipiegoD02`
(risposta identica, lavoro dopo la risposta, limite), `TestRiquadroEmailPaginaStorica`, `TestRiquadroEmailAnteprima`.
`tools/banco`: lo scenario nuovo negli elenchi di `dati_test.go` e `provacon_test.go`.

**Rosso:** detto com'è, i test sono stati scritti dopo una prima stesura del codice; il rosso si è preso su una copia
del repo con il dominio `email_cliente.go` a vuoto (ogni funzione esportata ridotta al `return` del valore zero):
**27 test rossi su 27** (19 in `buste`, uno con un panic che ferma il pacchetto e gli altri lanciati uno per uno; 8 in
`web`), log in `to-be-deleted-after-use/slice-070/prove/rosso*.log`. Dopo: il caso «spenta con il modulo sbagliato»
di `TestRichiestaEmailRifiuti` (riordino della D-16) e `TestEmailNomeEOggetto` e il caso «stop con tre campi» (per
le mutazioni sopravvissute M43, M45, M20) scritti prima della loro correzione o per uccidere la mutazione.

## Mutazioni

Script `to-be-deleted-after-use/slice-070/mutazioni/muta.py` su una copia del repo, test email di `buste` e `web`.
**49 mutazioni che compilano: 46 uccise, 3 equivalenti, 0 sopravvissute senza motivo.** Tre prime versioni non
compilavano (M24, M37, M41: riscritte perché compilassero, non contate) e una non trovava il testo (M47, riscritta).

| # | Mutazione | Esito |
|---|---|---|
| M1 | consenso senza «on» | uccisa (`TestRichiestaEmailRifiuti`, `TestRichiestaEmailPagina`) |
| M2 | indirizzo non in minuscolo | uccisa (`TestNormalizzaEmail` e altri) |
| M3 | 254 caratteri esclusi | uccisa (`TestNormalizzaEmail`) |
| M4 | spazi Unicode dentro l'indirizzo accettati | uccisa (`TestNormalizzaEmail`) |
| M5 | richiesta senza il controllo D-16 | uccisa (`TestRichiestaEmailRifiuti`, `…PaginaRifiuti`) |
| M6 | disponibile senza indirizzo pubblico | uccisa (`TestRichiestaEmailRifiuti`, `TestMandaLinkDisattivazione`) |
| M7 | link storico di una busta conto terzi con il riquadro | uccisa (`TestRiquadroEmail`, `TestEmailDallaPaginaStorica`, `TestRiquadroEmailPaginaStorica`) |
| M8 | busta chiusa con il riquadro | uccisa (`TestRichiestaEmailRifiuti`, `TestRiquadroEmail`, `…PaginaRifiuti`) |
| M9 | limite 7 invece di 6 | uccisa (`TestRichiestaEmailLimite`, `TestDisattivaEmailLimite`, `TestRichiestaEmailPagina`) |
| M10 | limite della pagina conta anche piè e link | uccisa (`TestDisattivaEmailLimite`) |
| M11 | indirizzo sostituito senza ritirare le email in coda | uccisa (`TestRichiestaEmailOreficeria`) |
| M12 | `resend` sempre falso | uccisa (`TestReinviaEmail`) |
| M13 | di chi è l'indirizzo: anche le righe «Reinvia» | **equivalente**: un «Reinvia» si scrive solo per l'indirizzo proprio, quindi la sua parte è già quella dell'indirizzo |
| M14 | ogni indirizzo è «proprio» | uccisa (`TestRiquadroEmail`, `TestEmailDellAltraParte`) |
| M15 | consegna anche da email di prima della richiesta | uccisa (`TestRiquadroEmail`, `TestRichiestaEmailPagina`) |
| M16 | sostituzione solo dalle disattivazioni | uccisa (`TestConfermaEmailRifiuti`, `TestEmailLinkRifiutati`) |
| M17 | `id >=` (la richiesta del link si sostituisce da sola) | uccisa (4 test) |
| M18 | conferma senza confrontare l'impronta dell'indirizzo | uccisa (`TestConfermaEmailRifiuti`, `TestEmailLinkRifiutati`) |
| M19 | scadenza esclusa all'istante esatto | uccisa (`TestConfermaEmailRifiuti`, `TestDisattivaConLink`) |
| M20 | scopo del link non controllato | sopravvissuta → caso «stop con tre campi» in `TestConfermaEmailRifiuti`: uccisa |
| M21 | conferma 49 ore | uccisa (`TestConfermaEmailRifiuti`) |
| M22 | disattivazione 11 anni | uccisa (`TestDisattivaConLink`) |
| M23 | conferma senza riga d'audit | uccisa (`TestConfermaEmail`) |
| M24 | link del piè senza confrontare l'impronta | uccisa (`TestDisattivaConLink`) |
| M25 | via «page» invece di «footer» | uccisa (`TestDisattivaConLink`) |
| M26 | disattivazione senza ritirare le email in coda | uccisa (`TestDisattivaEmailPagina`) |
| M27 | disattivazione dal link storico di una conto terzi | uccisa (`TestEmailDallaPaginaStorica`, `TestRiquadroEmailPaginaStorica`) |
| M28 | ripiego: 4 link all'ora per busta | uccisa (`TestMandaLinkDisattivazione`) |
| M29 | ripiego: 11 buste | uccisa (`TestMandaLinkDisattivazioneDieci`); dopo la review (tetto di 3 al giorno per indirizzo) diventa equivalente |
| M30 | motivo `not_configured` perso | uccisa (`TestRichiestaEmailNonAccodata`) |
| M31 | forma Phoenix senza il prefisso | uccisa (`TestConfermaEmailRifiuti`, `TestFormaPhoenix`) |
| M32 | link della busta anche revocato | uccisa (`TestConfermaEmail`) |
| M33 | D-16: «non disponibile» nel riquadro invece che in cima | uccisa (`TestRichiestaEmailPaginaRifiuti`) |
| M34 | ripiego senza limite | uccisa (`TestEmailRipiegoD02`) |
| M35 | ripiego fatto prima della risposta | uccisa (`TestEmailRipiegoD02`) |
| M36 | anteprima con l'`action` nel modulo | uccisa (`TestRiquadroEmailAnteprima`) |
| M37 | riquadro anche con l'email non disponibile | uccisa (`TestRiquadroEmailPagina`, `TestRiquadroEmailAnteprima`) |
| M38 | conferma senza «Torna alla tua riparazione» | uccisa (`TestEmailConfermaEDisattiva`) |
| M39 | vecchio link di disattivazione = 410 invece del ripiego | uccisa (`TestEmailRipiegoD02`) |
| M40 | frasi della consegna scambiate | uccisa (`TestRichiestaEmailPagina`) |
| M41 | «Reinvia» anche all'indirizzo dell'altra parte | uccisa (`TestEmailDellAltraParte`) |
| M42 | `{numero}` senza la D-82 | uccisa (`TestRichiestaEmail`) |
| M43 | oggetto senza `one_line` | sopravvissuta → `TestEmailNomeEOggetto`: uccisa |
| M44 | link del piè senza l'indirizzo pubblico | uccisa (`TestRichiestaEmail`) |
| M45 | «Cliente» invece di «cliente» | sopravvissuta → `TestEmailNomeEOggetto`: uccisa |
| M46 | busta chiusa: niente frase in cima | uccisa (`TestRichiestaEmailPaginaRifiuti`) |
| M47 | consenso senza il controllo «c'è» | **equivalente**: un consenso che manca è il testo vuoto, già rifiutato |
| M48 | indirizzo senza il controllo «c'è» | **equivalente**: un indirizzo che manca è il testo vuoto, già rifiutato da `NormalizzaEmail` |
| M49 | D-16: controlli del modulo anche con l'email spenta | uccisa (`TestRichiestaEmailRifiuti`) |

## Banco

`s070`, `fab-225`, `-dati sintetici-import`, al primo giro: **BANCO VERDE**, 64 scenari, 1120 passi, 0 inattese,
0 errori; **BANCO BROWSER VERDE**, 16 scenari, 32 viste, 0 inattese, 0 errori. Scenario nuovo HTTP
`bench/scenari/email-cliente.json` (17 passi, solo con i dati sintetici): pagina `/r/` e pagina storica `/stato/4/26`
con il riquadro, richiesta senza consenso, richiesta e «Disattiva» su un link morto (404 identici), i 410 di
`/stato/email-conferma` (falso, senza `t`, POST, Phoenix.Token) e di `/stato/email-disattiva` (GET e POST), il vecchio
link di disattivazione e il POST del ripiego (`solo_go` D-02), la richiesta storica con codice giusto e sbagliato.
Differenze attese nuove: D-16 sul redirect delle due richieste (lo standalone torna a `#public-email`, il Go in cima)
e sul riquadro della pagina storica; D-02 sulla pagina del vecchio link di disattivazione (410 contro 200 col
modulo). Sulla busta 21 nemmeno lo standalone mostra il riquadro (le tre righe D-16 scritte per quel passo non sono
servite e sono state tolte dopo il giro). Nessuno scenario browser nuovo: il Go del banco non ha `indirizzo_pubblico`
né l'email accesa, quindi non c'è un riquadro da confrontare (Dubbio 8); mai un SMTP configurato, nemmeno `.invalid`.
Il binario del banco è di prima di una riga (`context.WithoutCancel` in `mandaConferma`), che il banco non tocca.
`banco giu`, `banco controlla`: nessun avanzo, nessun container.

## Controlli

`scripts/check` verde prima delle mutazioni e alla fine, dopo test, documenti e deviazioni. Il calo da 38 a 35 voci chiuse della copertura è spiegato in «Correzioni dopo la review». `scripts/parole-vietate --file` sui file
cambiati: verde.

## Per il manuale

- Sezione nuova «Avviso email sulla pagina del cliente (slice 070)» in `docs/manuale/da-scrivere.md`: il riquadro solo
  con l'invio delle email acceso, la conferma con il pulsante, «Reinvia conferma», «Disattiva», il link nel piè, la
  busta di un'oreficeria, il limite, l'anteprima, i vecchi link (D-02) e la pagina di ripiego.

## Dubbi

1. **Senza `indirizzo_pubblico` niente riquadro, anche con l'email accesa** (i link dell'email non si possono
   scrivere, D-46). La pagina dei provider non lo dice. **Raccomandazione:** con la 070b aggiungere nella pagina dei
   provider la riga «il riquadro della pagina del cliente non c'è: manca indirizzo_pubblico», come il popup.
2. **Testo dell'email di ripiego (D-02) e della sua pagina:** scritti dall'implementatore, non nell'editor dei testi.
   **Raccomandazione:** tenerli fissi (è una pagina di passaggio, per i soli vecchi link) e rivederli con le schermate
   del manuale.
3. **Vecchio link di conferma = «Link non valido o scaduto»**, non la pagina di ripiego: la decisione 16 parla della
   disiscrizione, e una conferma vecchia dura 48 ore e la frase dice già come averne una nuova. **Raccomandazione
   (anche della review):** tenere il 410. La frase del PIANO (§2.4, riga 330) che diceva «risponde con la pagina di
   ripiego» per tutti i Phoenix.Token è corretta in questo albero.
4. **Il ripiego cerca l'indirizzo anche nelle buste del cestino e in attesa di conferma**, e manda un'email per busta
   (con il tetto per indirizzo, al più 3 al giorno). **Raccomandazione:** tenerlo (il link di disattivazione vale
   anche nel cestino, e un indirizzo in attesa riceve già le nostre email); se Stefano preferisce, una sola email con
   tutti i link.
5. **Il lavoro del ripiego all'arresto di `serve`** (corretto con la review, M1): ora è legato ai lavori di `serve`,
   che lo aspettano (al più 2 minuti); un lavoro chiesto dopo l'inizio dell'arresto non parte. **Raccomandazione:**
   tenerlo così.
6. **Rinnovare la chiave del server fa scadere i link delle email** (conferma e disattivazione, come le sessioni). Un
   vecchio link di disattivazione Go diventa allora un 410 «Link non valido», non il ripiego. **Raccomandazione:**
   scriverlo nel manuale dell'amministrazione accanto a `RinnovaChiaveServer`; il ripiego per ogni link non valido si
   può aggiungere se capita.
7. **«Disattiva» non si vede con l'email spenta** (il riquadro sparisce per la D-16); resta il link nel piè di ogni
   email e la pagina di ripiego non serve. **Raccomandazione:** tenerlo così (con l'email spenta non parte niente).
8. **Banco:** il Go del banco non ha `indirizzo_pubblico` né l'email accesa, quindi il banco confronta solo i rifiuti,
   i 410 e le deviazioni D-16 e D-02; il giro riuscito (richiesta, conferma, disattivazione) lo provano i test Go.
   **Raccomandazione:** in una slice del banco, una seconda istanza Go con `indirizzo_pubblico` e un SMTP `.invalid`
   per confrontare anche il giro riuscito (lo standalone accoda senza SMTP solo fino al rifiuto della coda).

9. **Link di disattivazione non validi senza ripiego:** i vecchi link Flask (`?e=`) e i link del Go dopo il rinnovo
   della chiave del server danno il 410 «Link non valido», senza la pagina di ripiego (che c'è solo per la forma
   Phoenix.Token). **Raccomandazione della review:** il ripiego per ogni link di disattivazione non valido. Non fatto:
   lo decide Stefano.
10. **Frase del ripiego con l'email spenta:** la pagina dice «tra poco ti arriva un'email…» anche quando l'email è
    spenta e non parte niente. **Proposta neutra:** «Se l'indirizzo che hai scritto riceve gli avvisi di una
    riparazione, ti mandiamo un'email con il link per disattivarli. Se non arriva, chiedi al negozio.» Lasciata
    com'è finché Stefano non sceglie. **Raccomandazione:** usare la proposta neutra.
11. **Tetto per indirizzo del ripiego:** messo, al più 3 email di ripiego al giorno per lo stesso indirizzo, contate
    sull'impronta con chiave (`address_digest` nella riga `repair.email_stop_link_sent`, mai l'indirizzo).
    **Raccomandazione:** tenerlo a 3.

## Correzioni dopo la review (12/11/2026)

- **M1:** il lavoro del ripiego ha un `recover` (nel registro solo «lavoro del ripiego andato in panico» e il tipo del
  valore) e un semaforo di 4 posti (`postiLavoriRipiego`: posti pieni, lavoro scartato, stessa risposta); in `serve`
  `Dipendenze.Sottofondo` lo lega al `WaitGroup` dei lavori (`cmd/gestrip/main.go`), che all'arresto lo aspetta; dopo
  l'inizio dell'arresto un lavoro non parte (niente `Add` durante `Wait`).
- **M2:** `TestImprontaConChiave`: due chiavi danno due impronte; nei dati del link (e nel link) non ci sono
  l'indirizzo, l'indirizzo in base64, né lo SHA-256 nudo di «repair email address:»+indirizzo (esadecimale e base64).
- **B1:** `TestSottofondoPredefinito` (il sottofondo predefinito torna prima della fine del lavoro);
  `TestRipiegoLavoroProtetto` (la scadenza del contesto del lavoro è fra 2 minuti, letta da `ctx.Deadline()` con una
  `Buste` che la registra, senza aspettare; panico; semaforo); `TestMandaConfermaContestoAnnullato` (contesto già
  annullato, l'email si accoda lo stesso); `TestRiquadroEmailAnteprimaStati` (nessun `_csrf_token` né `action` nel
  riquadro dell'anteprima, stati richiesta e in attesa; lo stato attivo lo aveva già `TestRiquadroEmailAnteprima`).
- **B2:** conteggi e riga `repair.email_stop_link_sent` in una transazione IMMEDIATE (`_txlock=immediate` del
  collegamento di scrittura), la riga prima di `Accoda`; tetto di 3 al giorno per indirizzo (`TestRipiegoTetti`).
- **B3:** indirizzo non di chi ha il link: `{nome}` «cliente» e il destinatario dalla parte che l'ha chiesto
  (`TestRipiegoIndirizzoDellAltraParte`; aggiornata l'attesa di `TestMandaLinkDisattivazione` per la busta passata a
  un'oreficeria con l'indirizzo importato del cliente).
- **B4:** la consegna del riquadro per id (riga nuova del Go `repair.email_optin_queued` con `message_id`, scritta da
  `mandaConferma` dopo `Accoda`); senza quella riga (richiesta importata) solo fra le email con il link di conferma
  (`TestConsegnaSoloDellaConferma`).
- **B6:** ricerca con `lower(trim(email_cliente))`, ricontrollo sull'impronta dell'indirizzo salvato e normalizzato,
  link firmato per l'indirizzo com'è salvato (`TestRipiegoIndirizzoImportato`).
- `TestMandaLinkDisattivazioneDieci`: con il tetto di 3 al giorno per indirizzo vanno le 3 buste più recenti; il
  tetto di 10 buste resta sulla ricerca ma non si vede più da fuori (M29 diventa equivalente).
- **Rosso preso prima del codice:** in `buste` 4 rossi su 6 nuovi (`TestRipiegoTetti`, `TestRipiegoIndirizzoDellAltraParte`,
  `TestConsegnaSoloDellaConferma`, `TestRipiegoIndirizzoImportato`), in `web` `TestRipiegoLavoroProtetto` rosso con il
  panico che usciva dal lavoro (log `prove/rosso-review-*.log`). `TestImprontaConChiave`, `TestMandaConfermaContestoAnnullato`,
  `TestSottofondoPredefinito` e `TestRiquadroEmailAnteprimaStati` provano comportamenti già giusti: la loro forza è
  nelle mutazioni della review, rifatte.
- **Mutazioni della review rifatte** (`mutazioni/muta-review.py`, `esiti-review.log`): R1 (impronta senza chiave)
  uccisa da `TestImprontaConChiave`; R8 (sottofondo predefinito sincrono) uccisa da `TestSottofondoPredefinito`; R9
  (ripiego senza tetto di 2 minuti) uccisa da `TestRipiegoLavoroProtetto`; R18 (`mandaConferma` con il contesto della
  richiesta) uccisa da `TestMandaConfermaContestoAnnullato`; R24 (token CSRF nel modulo della richiesta
  nell'anteprima) uccisa da `TestRiquadroEmailAnteprimaStati`.
- **Banco non rifatto:** le correzioni non toccano niente di quello che `email-cliente` o gli altri scenari
  confrontano. Il POST del ripiego è un passo `solo_go` con le attese «200», «Controlla la tua email» e `no-store`, e
  la risposta non cambia (cambia solo il lavoro dopo la risposta, che il banco non guarda: l'email del Go è spenta);
  il riquadro, le richieste, i 410 e la pagina del vecchio link sono gli stessi.
- **Copertura, da 38 a 35 voci chiuse:** lo scenario nuovo `bench/scenari/email-cliente.json` è l'unico per CLI-39,
  CLI-40 e CLI-41, e senza l'esito del banco (`scripts/check` senza `GESTRIP_BANCO_ESITO`) una voce con uno scenario
  del banco resta parziale. Non è una regressione.

## Review

Revisore indipendente, 12/11/2026 (pc-fisso, slice non committata). Nessuna ALTA. Mutazioni del revisore: 29
che compilano, 22 uccise, 7 sopravvissute (R1, R8, R9, R18, R24 → test nuovi, ora uccise; R7 equivalente, R20
quasi equivalente). Il rosso con il dominio svuotato è debole (i test sono stati scritti dopo una prima stesura
del codice): la forza dei test la danno le mutazioni.

- **MEDIA, corretto (M1):** il lavoro del ripiego, che parte da una rotta pubblica, non aveva `recover` né un
  tetto: un panico avrebbe fatto cadere tutto `serve`. Ora `recover`, semaforo di 4 posti e lavori di `serve`.
- **MEDIA, corretto (M2):** nessun test pretendeva l'impronta con chiave dell'indirizzo nel link di
  disattivazione (10 anni di vita): ora `TestImprontaConChiave`.
- **BASSA, corrette:** B1 (test del sottofondo, del tetto di 2 minuti, di `WithoutCancel`, del CSRF
  nell'anteprima), B2 (limite per busta atomico e tetto per indirizzo), B3 (nome dell'oreficeria mai al cliente
  finale nell'email di ripiego), B4 (consegna del riquadro per id), B5 (arresto: lavoro aspettato), B6
  (indirizzi importati con maiuscole o spazi).
- **Domande di prodotto:** Dubbi 3, 9, 10, 11.
- **Copertura 38 → 35:** spiegata (lo scenario nuovo è l'unico per CLI-39, 40, 41: senza l'esito del banco le
  voci restano parziali), nessuna regressione.
- **Verificato senza rilievi:** gettoni (sottochiave propria, `hmac.Equal`, scopo firmato, nessun riuso su
  un'altra busta o un altro indirizzo, scadenze), nessun indirizzo in link, audit o registro, `no-referrer` e
  `no-store`; ripiego identico in testo, codice, intestazioni e tempo, con CSRF e limiti per IP; D-16 e
  anteprima; link già mandati mai 404 o 500; CLI-17 e CLI-28; deviazioni del banco strette; `{numero}` D-82.
  `scripts/check` verde.
