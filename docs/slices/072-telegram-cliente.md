# 072 — Iscrizione Telegram dalla pagina del cliente e dal link dei messaggi WhatsApp (T4.7, seconda slice)

- **Fonte:** brief del coordinatore del 13/11/2026, ripresa `2026-11-13-fine-071.md`. Lo standalone a `go-baseline` =
  `302439d`: `telegram_subscriptions.ex` (`available?/0`, `box/2`, `issue_link/4`, `revoke/3`, `under_limit/2`,
  `visible/1`, `link_alive/2`, `switch_off/4`), `telegram_subscriptions/link_token.ex`, `subscription.ex`,
  `outgoing/telegram_inbound.ex` (`active?/0`, `bot_username/0`), `public_repair_controller.ex` (`telegram_optin/2`,
  `telegram_link/2`, `legacy_telegram_optin/2`, `telegram_optout/2`, `legacy_telegram_optout/2`, `telegram_linked/3`,
  `telegram_followed/3`, `telegram_revoked/3`, `show/2`, `preview/2`, `legacy/2`), `public_repair_html/show.html.heex`
  (riquadro `#public-telegram`), `components/notice_popup.ex`; i test `public_repair_telegram_test.exs`,
  `public_repair_telegram_link_test.exs`, `telegram_subscriptions_test.exs`, `repair_telegram_optin_live_test.exs` (letti,
  non fatti girare). Inventario: CLI-44, CLI-45, CLI-19, CLI-22, D-02, D-16, D-46.
- **Voci:** CLI-44 (riquadro e pulsante), CLI-45 (link `/r/<token>/telegram`), CLI-19 (parte Telegram della pagina
  storica: telegram-optin e telegram-optout; con la 070 la voce ha tutte le sue azioni). Tocca CLI-32 (il popup
  avvisa del link Telegram spento secondo la regola nuova) e CLI-27 (il riquadro nell'anteprima, spento).
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. **Nessuna migrazione:** `token_telegram` e
  `iscrizioni_telegram` ci sono dalla 0008, i trigger della 0014 le spengono e tolgono i token (chiusura, cestino,
  passaggio a un'oreficeria), l'import le riempie da `telegram_link_tokens` (`token_hash` → `hash_token`, lo stesso
  SHA-256) e `telegram_subscriptions`.

## Cosa c'è

- **Dominio** (`internal/buste/telegram_cliente.go`): `TelegramDisponibile`, `RiquadroTelegram`,
  `IscriviTelegramCliente` (pulsante), `LinkTelegram` (link dei messaggi WhatsApp: sostituisce quello della 066 che
  rispondeva sempre «spento»), `IscriviTelegramStorica`, `DisattivaTelegramCliente`, `DisattivaTelegramStorica`,
  `ErrTroppeRichiesteTelegram`; `spegniIscrizioni` (= `switch_off/4`) è ora anche il ciclo di `tagliaTelegram` della
  061 (motivo e via «link», stesso comportamento).
- **Token:** 24 byte di `crypto/rand` in base64url senza «=» (32 caratteri), nel database solo lo SHA-256 con la busta
  e la scadenza, 24 ore dal pulsante e 60 minuti dal link WhatsApp; `https://t.me/<bot>?start=<token>`. Nella
  transazione, nell'ordine di `issue_link/4`: busta riletta fuori dal cestino, controparte (conto terzi nascosta anche
  chiusa), chiusa, storico revocato dopo la verifica della rotta, canale, limite, token, `repair.telegram_optin`
  (`bag_number`, `link` «token»/«legacy», `via` «page»/«whatsapp»; mai il token, la chat o il nome).
- **Limite:** 6 all'ora per busta **per via**, contati sulle righe d'audit (una riga senza via è della pagina, come
  lo standalone dopo la sua slice 136).
- **Pagine** (`internal/web/telegram_cliente.go`, `pagina_cliente.html`, `app.css`): le quattro rotte POST nuove e la
  GET della 066 completata; riquadro `#public-telegram` sopra `#public-email` nei due stati dello standalone, con i
  suoi testi, anche sulla pagina storica (credenziale nei campi nascosti) e nell'anteprima del negozio (spento: senza
  `action` né token, dentro il fieldset spento). Esiti nella riga `#public-telegram-feedback` (sessione
  `InfoTelegram`, `ErroreTelegram`), una volta. Colori di Telegram, il resto delle classi del riquadro email.
- **Risposte come lo standalone:** riuscito il 302 a t.me con `no-store`; link morto e conto terzi il 404 della
  pagina (che il limite ai tentativi conta; dalla pagina storica con `repair.legacy_public_denied`); busta chiusa la
  pagina con «Questa riparazione non riceve più avvisi.» in cima; canale spento (vecchio modulo) «⚠️ Servizio non
  disponibile. Riprova più tardi.» in cima; limite del pulsante «⚠️ Troppe richieste nell'ultima ora…» nel riquadro;
  dal link WhatsApp 503 «Servizio notifiche temporaneamente non disponibile.» e 429 «Troppe richieste nell'ultima
  ora: riprova più tardi o contatta il negozio.» in testo semplice. «Disattiva»: «Notifiche Telegram disattivate.» nel
  riquadro, anche senza niente da spegnere; conto terzi il 404.
- **Popup (066):** `ApriAvviso` mette `TelegramSpento` (l'avviso «⚠️ Notifiche Telegram ai clienti spente: il link
  Telegram porterà il cliente…» dello standalone) quando `TelegramDisponibile` è falso; era sempre vero.

## Scelte

- **Canale disponibile = D-16 come l'email (`EmailDisponibile`).** Servono tutti e due gli interruttori: «Invio
  Telegram» (D-03, solo Go, attraverso `CanaleAttivo("telegram")` della coda, che chiede anche il bot usabile) e
  «Notifiche Telegram ai clienti» (dello standalone, `active?/0`), più il nome del bot noto e l'**indirizzo pubblico**.
  Perché: (1) con la sola «Notifiche» accesa dall'import il cliente si iscriverebbe a un canale che il negozio non ha
  ancora acceso (la 073 leggerà solo con i due accesi, vincolo della 071); (2) gli avvisi Telegram della 074 hanno il
  pulsante con il link della pagina (`Workshop.public_url/1`), che senza indirizzo pubblico non si scrive (D-46): un
  cliente iscritto non riceverebbe niente. Lo standalone non ha questo caso (ha sempre l'host). Dubbio 1.
- **Seconda serratura del nome del bot (solo Go):** un nome che non è `[A-Za-z0-9_]+` (finirebbe nell'indirizzo t.me:
  un nome importato con «/» o «?» lo cambierebbe) è ignoto: niente riquadro, il link è 503. Il nome salvato da `getMe`
  ha sempre quella forma.
- **Busta nel cestino:** la busta si rilegge con `cancellata_il IS NULL` (come l'email della 070); lo standalone fa
  `Repo.get/2` e scriverebbe un token per una busta cestinata fra la rotta e la transazione (finestra di un istante,
  che il trigger del cestino aveva già svuotato). Differenza senza D, verso il più chiuso.
- **«Disattiva» senza limite**, come lo standalone (l'email ha i suoi 6 all'ora). Non scrive niente se non c'è niente
  da spegnere.
- **Il canale si chiede prima della transazione** (come `chiediEmail`), così dentro la transazione niente usa il
  collegamento di scrittura; il rifiuto «spento» arriva comunque dopo i controlli della busta, nell'ordine dello
  standalone.

## Prove

`buste` (`telegram_cliente_test.go`, 10): `TestTelegramDisponibile` (ogni pezzo della regola, nome del bot vuoto, di
spazi, con «/», «?», spazio, a capo, assente; senza coda), `TestTokenTelegramPagina` (32 caratteri base64url, SHA-256,
24 ore esatte, token nuovo a ogni pressione, audit esatto con l'IP e senza token, nessuna iscrizione),
`TestTokenTelegramWhatsApp` (60 minuti esatti, via «whatsapp», nessuna iscrizione), `TestTelegramLimiti` (6+6 per via,
il settimo rifiutato per via, dopo un'ora si riparte, righe senza via della pagina), `TestTelegramRifiuti` (link morto,
vuoto, cestino, conto terzi anche chiusa, chiusa, spento, senza indirizzo pubblico; niente scritto),
`TestRiquadroTelegram` (proposta/attive/nil: consenso, fermata, storico revocato, conto terzi, chiusa, cestino),
`TestDisattivaTelegram` (tutte le attive, le fermate e quelle di altre buste no, motivo e audit esatti, mai la chat,
seconda volta senza righe, chiusa, conto terzi, link morto), `TestTelegramDallaPaginaStorica`,
`TestTelegramStoricaRevocataNelFrattempo` (gancio `primaDelToken`, che gira dopo la verifica della credenziale e **prima** della transazione: prova che la busta si rilegge dopo la rotta, non che la rilettura stia nella transazione; vedi R10 sotto), `TestPopupTelegramSpentoSoloSenzaCanale`.
Adattato `TestTelegramOptinACanaleSpento` (firma nuova di `LinkTelegram`).
`web` (`telegram_cliente_test.go`, 6): `TestRiquadroTelegramPagina` (stati A e B con i testi e i moduli esatti, CSRF,
sopra l'email, niente riquadro con l'invio spento, con il nome del bot ignoto, conto terzi, chiusa),
`TestIscrizioneTelegramPagina` (senza CSRF 403, 302 a t.me con `no-store`, settima nel riquadro, spento in cima, chiusa,
conto terzi e link morto 404), `TestDisattivaTelegramPagina`, `TestLinkTelegramDaWhatsApp` (302 con `no-store` e
`no-referrer`, token diversi, 429 in testo semplice, il pulsante dopo i sei link, 503 per invio spento, notifiche
spente e nome ignoto senza token, 404 per conto terzi anche chiusa e link morto, chiusa), `TestRiquadroTelegramPaginaStorica`,
`TestRiquadroTelegramAnteprima`. `TestLinkTelegramSpento` della 066 resta verde com'è (senza coda il canale è spento).
`tools/banco`: lo scenario nuovo negli elenchi di `dati_test.go` e `provacon_test.go`.

**Rosso:** i test sono stati scritti prima del codice, con il dominio a vuoto (stub che compilano). Il primo giro
(`slice-072/prove/rosso-buste.log`, `rosso-web.log`: 10 + 6 rossi) **non vale**: i test fallivano tutti nella
preparazione (colonna `aggiornata_il` di `impostazioni` scritta male nell'INSERT). Corretti i test, il rosso valido
si è preso su una copia del repo con le sette funzioni esportate del dominio ridotte al `return` del valore zero:
**16 test nuovi rossi su 16** (10 in `buste`, 6 in `web`), ognuno su un'asserzione di comportamento, più i due test
vecchi del link Telegram (`TestTelegramOptinACanaleSpento`, `TestLinkTelegramSpento`), log
`slice-072/prove/rosso2-buste.log` e `rosso2-web.log`. Un secondo errore dei test (stato `accettata`, che il CHECK di
`buste` non ammette) si è visto al primo verde e corretto.

## Mutazioni

Script `to-be-deleted-after-use/slice-072/mutazioni/muta.py` su una copia del repo, test del pacchetto toccato
(`buste` con `-run 'Telegram|Popup|Link|Avvis'`, `web` con `-run Telegram`). **32 mutazioni che compilano: 32
uccise, 0 sopravvissute.** Due prime forme non compilavano (B4, B23: variabile non usata), riscritte e rifatte.

| # | Mutazione | Esito |
|---|---|---|
| B1 | token della pagina 25 ore invece di 24 | uccisa (`TestTokenTelegramPagina`) |
| B2 | token del link WhatsApp 2 ore invece di 60 minuti | uccisa (`TestTokenTelegramWhatsApp`) |
| B3 | link WhatsApp con la durata della pagina | uccisa (`TestTokenTelegramWhatsApp`) |
| B4 | token salvato in chiaro invece dello SHA-256 | uccisa (`TestTokenTelegramPagina`) |
| B5 | limite 7 invece di 6 | uccisa (`TestTelegramLimiti`) |
| B6 | limiti di pulsante e link WhatsApp contati insieme | uccisa (`TestTelegramLimiti`) |
| B7 | riga senza via contata come WhatsApp | uccisa (`TestTelegramLimiti`) |
| B8 | busta conto terzi non nascosta | uccisa (`TestRiquadroTelegram`, `TestTelegramDallaPaginaStorica`, `TestTelegramRifiuti`) |
| B9 | chiusa controllata prima della controparte | uccisa (`TestTelegramRifiuti`) |
| B10 | busta chiusa non controllata | uccisa (3 test) |
| B11 | token anche a canale spento | uccisa (`TestTelegramOptinACanaleSpento`, `TestTelegramRifiuti`) |
| B12 | notifiche ai clienti ignorate | uccisa (`TestTelegramDisponibile`) |
| B13 | indirizzo pubblico non richiesto | uccisa (`TestTelegramDisponibile`, `TestTelegramRifiuti`) |
| B14 | nome del bot senza seconda serratura | uccisa (`TestTelegramDisponibile`) |
| B15 | «Disattiva» spegne solo la prima iscrizione | uccisa (`TestDisattivaTelegram`) |
| B16 | «Disattiva» tocca anche le iscrizioni già fermate | uccisa (`TestDisattivaTelegram`, `TestLinkTagliaTelegram`) |
| B17 | motivo «stop» invece di «page» | uccisa (`TestDisattivaTelegram`, `TestTelegramDallaPaginaStorica`) |
| B18 | storico revocato non riletto dopo la verifica della rotta | uccisa (`TestRiquadroTelegram`, `TestTelegramStoricaRevocataNelFrattempo`) |
| B19 | audit con via sempre «page» | uccisa (`TestTelegramLimiti`, `TestTokenTelegramWhatsApp`) |
| B20 | audit con link sempre «token» | uccisa (`TestTelegramDallaPaginaStorica`) |
| B21 | riquadro «attive» anche senza consenso | uccisa (`TestRiquadroTelegram`) |
| B22 | token di 32 byte invece di 24 | uccisa (3 test) |
| B23 | popup: Telegram sempre spento | uccisa (`TestPopupTelegramSpentoSoloSenzaCanale`) |
| W1 | anteprima con l'`action` nel modulo | uccisa (`TestRiquadroTelegramAnteprima`) |
| W2 | riquadro Telegram sotto quello email | uccisa (`TestRiquadroTelegramPagina`) |
| W3 | link WhatsApp a canale spento: 404 invece di 503 | uccisa (`TestLinkTelegramDaWhatsApp`, `TestLinkTelegramSpento`) |
| W4 | link WhatsApp oltre il limite: redirect invece di 429 | uccisa (`TestLinkTelegramDaWhatsApp`) |
| W5 | busta chiusa: 404 invece della pagina | uccisa (3 test) |
| W6 | limite del pulsante senza l'ancora del riquadro | uccisa (`TestIscrizioneTelegramPagina`) |
| W7 | riquadro anche con il canale non disponibile | uccisa (`TestRiquadroTelegramAnteprima`, `TestRiquadroTelegramPagina`) |
| W8 | «Disattiva» storico senza la credenziale nel redirect | uccisa (`TestRiquadroTelegramPaginaStorica`) |
| W9 | canale spento: frase nel riquadro invece che in cima | uccisa (`TestIscrizioneTelegramPagina`) |

## Banco

**Giro valido: `s072d`, BANCO VERDE, 65 scenari, 1137 passi, 0 inattese, 0 errori; BANCO BROWSER VERDE, 17 scenari,
34 viste, 0 inattese, 0 errori.** `bench/scenari/telegram-cliente.json` (17 passi, nuovo): pagina della 27/26 senza
riquadro nelle due app, link WhatsApp 503 in testo semplice, pulsante a canale spento (302 alla pagina con «⚠️ Servizio
non disponibile…» in `#public-flash-error`), «Disattiva» (302 a `#public-telegram`), busta chiusa 21/26 (link e
pulsante alla pagina con la frase), link morto e conto terzi 17/26 404, pagina storica 4/26 con la credenziale
(pulsante, «Disattiva» con `?pw=SINT-4#public-telegram`, codice sbagliato 404). **Nessuna riga di deviazione:** nel
banco lo standalone non ha bot né notifiche accese e il Go ha l'invio Telegram spento, quindi le due app rispondono
uguale (il canale spento della D-16 e quello dello standalone danno le stesse risposte). Il riquadro, il token e lo
spegnimento riusciti li provano i test Go.

Giri prima di quello valido (tutti con `banco giu`): `s072` HTTP con 2 inattese, entrambe un errore dello scenario
(la 21/26 è già consegnata quando lo scenario gira: le due app rispondono 302 e lo scenario aspettava 503; corretto
usando la 27/26 per il canale spento e la 21/26 per la busta chiusa); `s072b` e `s072c` HTTP verde (65 scenari, 1137
passi, 0 inattese) e browser rosso solo per il `500 GET /audit` dello standalone («database is locked», il difetto
saltuario già noto), ogni volta in uno scenario diverso e non toccato dalla 072 (`provider-posta`, poi
`salva-avverti`; in `s072` era `whatsapp`, con `scripts/check` che girava insieme). Il Go non ha mai avuto
differenze sue. Nessuna richiesta al Bot API vero, mai.

## Controlli

`scripts/check` (`slice-072/prove/check2.log`): **verde**, «voci 353: chiuse 36, rosse 0, parziali 128, aperte
178, fuori v1 11», segreti e dati veri OK (prima della slice: parziali 126, aperte 180: CLI-44 e CLI-45 da aperte a
parziali, perché l'iscrizione vera arriva con il `/start` della 073). Il primo giro (`check1.log`) era rosso per
`TestChiaviSoloGoComplete` (importer): il dominio leggeva `impostazioni` da un file nuovo. La lettura del nome del bot
è passata in `uscita.NomeBot` (`config_telegram.go`, già fra i file noti), e le due mutazioni del nome (B12, B14)
rifatte dopo: uccise. Il banco valido gira sui binari costruiti dopo questa correzione.

## Per il manuale

- Sulla pagina della riparazione (link `/r/…` e vecchio link `/stato/…`), sopra il riquadro email, il riquadro azzurro
  «Vuoi essere avvisato in tempo reale?» con «Attiva su Telegram →»: apre il bot del negozio; aprire il link non
  iscrive ancora nessuno (lo fa il bot, prossima slice).
- Iscritto, «Notifiche Telegram attive» con «⚙️ Modifica preferenze» e «Disattiva».
- Il riquadro c'è solo con «Invio Telegram» e «Notifiche Telegram ai clienti» accesi, il nome del bot noto (dalla
  prova) e l'indirizzo pubblico; solo per le riparazioni del cliente non chiuse; nell'anteprima si vede spento.
- Limiti e frasi; il link dei messaggi WhatsApp con Telegram spento.
- In `docs/manuale/da-scrivere.md`, sezione «Telegram sulla pagina del cliente (slice 072)».

## Dubbi

1. **Canale disponibile con i due interruttori e l'indirizzo pubblico** (sopra, «Scelte»). Lo standalone chiede solo le
   notifiche ai clienti, il bot e il nome. **Raccomandazione:** tenerlo così (coerente con la D-16 dell'email e con il
   vincolo della 073); la pagina dei provider potrebbe dire, come per l'email (070b), che con Telegram acceso e senza
   indirizzo pubblico il riquadro non compare: piccola aggiunta da fare con la 073 o la 074.
2. **Seconda serratura del nome del bot** (solo Go). **Raccomandazione:** tenerla; il caso vero (nome salvato da
   `getMe`) passa sempre.
3. **Il popup dice «Notifiche Telegram ai clienti spente…»** (testo dello standalone) anche quando a mancare è un altro
   pezzo della regola (invio spento, nome del bot, indirizzo pubblico). **Raccomandazione:** lasciarlo per ora e
   rivederlo con le schermate del manuale insieme agli altri testi solo Go del popup.
4. **«Disattiva» senza limite**, come lo standalone (l'email ne ha uno). **Raccomandazione:** lasciarlo: non scrive
   niente se non c'è niente da spegnere e non rivela niente.
5. **Banco con il canale acceso:** come per l'email, il riquadro, il token e lo spegnimento riusciti si vedono solo nei
   test Go. **Raccomandazione:** il Bot API finto nella rete interna con la 074 (Dubbio 5 della 071) e, lì, uno
   scenario con il canale acceso nelle due app.
6. **Dopo l'import i link `/r/<token>/telegram` già mandati via WhatsApp rispondono 503 e il riquadro sparisce** finché
   il titolare non accende «Invio Telegram» (D-03: spento dopo l'import, anche con «Notifiche Telegram ai clienti»
   accese nello standalone). **Raccomandazione:** metterlo nel promemoria del cutover: accendere «Invio Telegram»
   (dopo la revisione D-27 dei messaggi in coda) appena fatto l'import, se il negozio usava Telegram.

## Review

Revisore indipendente, 13/11/2026 (pc-fisso, slice non committata). **Nessuna ALTA.** Correzioni nel repo, ognuna con
il rosso preso prima sulla copia con la mutazione corrispondente (`slice-072/mutazioni/review-*.log`):

- **MEDIA, corretto (test):** il limite non era provato per busta (R1). `TestTelegramLimiti`: dopo i 6+6 sulla busta,
  un'altra busta nella stessa ora ottiene subito il pulsante e il link WhatsApp.
- **MEDIA, corretto (test):** i campi della credenziale nei due moduli dello stato B della pagina storica non erano
  provati (R7, R8). `TestRiquadroTelegramPaginaStorica` legge i due moduli dalla pagina (`moduloDellaPagina`), controlla
  `pw` e li manda così come sono.
- **BASSA, corretto (ricevuta):** il gancio `primaDelToken` gira prima di `BeginTx`, quindi prova la rilettura dopo la
  rotta, non che stia nella transazione (R10 sopravvive). Spostarlo dentro la transazione non cambierebbe niente: una
  revoca scritta e confermata da un altro collegamento la vede anche la lettura fuori dalla transazione, e una non
  confermata non la vede nessuna delle due. Corretta la riga B18 e la descrizione del test.
- **BASSA, corretto (test):** `TestLimiteTelegramPaginaStorica`: la settima pressione dalla pagina storica torna a
  `?pw=…#public-telegram` con la frase del limite (R3).
- **BASSA, corretto (test):** `TestTelegramDallaPaginaStorica` controlla `scade_il` = adesso + 24 ore anche per il
  pulsante storico (R16).
- **BASSA, corretto (test):** `TestPopupTelegramSpentoSoloSenzaCanale` con `ConIndirizzoPubblico("")`: il popup dice
  Telegram spento (R6).
- **BASSA, corretto (codice e test):** un errore nel leggere lo stato di Telegram faceva fallire tutto il popup
  WhatsApp. Ora `ApriAvviso` lo tratta come canale spento e mette `TelegramNonLetto`; la pagina (`f.apriAvviso`, per le
  tre chiamate del popup) scrive «stato di Telegram non letto per il popup» nel registro, senza l'errore, come
  `emailAccesa` della 070c. Test `TestPopupTelegramStatoNonLetto` (buste) e `TestPopupConStatoTelegramNonLetto` (web).
- **Note, senza correzioni:**
  - R9: il confine della finestra del limite è `creato_il >= adesso − 1 ora`, come lo standalone (`inserted_at >= ^since`);
  - R11: mutazione equivalente secondo la review;
  - nota 10: `HEAD /r/{token}/telegram` crea un token come la GET (in Phoenix `Plug.Head` la fa diventare una GET):
    un'anteprima di link che fa HEAD e poi GET consuma 2 dei 6 posti del link WhatsApp, come nello standalone;
  - nota 9: dopo l'import «Invio Telegram» è spento, i link Telegram già mandati rispondono 503 (Dubbio 6, e in
    `docs/SVILUPPO.md`).

Dopo le correzioni `scripts/check` (`slice-072/prove/check3.log`) è **verde**: «voci 353: chiuse 36, rosse 0, parziali
128, aperte 178, fuori v1 11», segreti e dati veri OK.

**Verifica della sessione principale dopo le correzioni:** `scripts/check` verde (stessa riga dei conteggi); banco su
un'istanza nuova `s072p` con i binari dell'albero corretto: **BANCO VERDE**, 65 scenari, 1137 passi, 0 inattese, 0
errori; **BANCO BROWSER VERDE**, 17 scenari, 34 viste, 0 inattese, 0 errori; `banco giu`: nessun avanzo.

**Mutazioni della review dopo le correzioni** (`slice-072/mutazioni/muta-review.py`, `giro-review.log`,
`esiti-review.json`; la copia del repo è già cancellata):

| # | Mutazione | Esito |
|---|---|---|
| R1 | limite contato senza la busta (`id_risorsa`) | uccisa (`TestTelegramLimiti`) |
| R3 | limite dalla pagina storica: redirect senza la credenziale | uccisa (`TestLimiteTelegramPaginaStorica`) |
| R6 | popup che non guarda l'indirizzo pubblico | uccisa (`TestPopupTelegramSpentoSoloSenzaCanale`) |
| R7 | stato B storico: «Modifica preferenze» senza credenziale | uccisa (`TestRiquadroTelegramPaginaStorica`) |
| R8 | stato B storico: «Disattiva» senza credenziale | uccisa (`TestRiquadroTelegramPaginaStorica`) |
| R9b | limite con `>` invece di `>=` sul conteggio (la R9 della review, sul confine della finestra, è come lo standalone) | uccisa (`TestTelegramLimiti`) |
| R10 | busta riletta da `Lettura` invece che dalla transazione | sopravvissuta: non distinguibile con un test (sopra) |
| R16 | token del pulsante storico di 48 ore | uccisa (`TestTelegramDallaPaginaStorica`) |
| N1 | errore di Telegram che fa fallire il popup (il codice di prima) | uccisa (`TestPopupTelegramStatoNonLetto`) |
| N2 | errore di Telegram non scritto nel registro | uccisa (`TestPopupConStatoTelegramNonLetto`) |
