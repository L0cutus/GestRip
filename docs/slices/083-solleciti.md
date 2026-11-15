# 083 — Solleciti all'artigiano: pagina, sollecito, WhatsApp, contatore e riquadro sulla scheda (T4.8, ottava)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/artisan_reminders_live.ex`
  (`handle_params/3`, `record-reminder`, `render/1`, `reminder_text/1`, `truncate/1`, `wa_home/2`),
  `workshop/artisan_reminders.ex` (`threshold_days/1`, `build/2`, `pending_count/1`, `late_bags/3`, `late_query/3`,
  `history/1`, `last_reminders/1`), `workshop.ex` (`record_artisan_reminder/4`, `write_artisan_reminder/3`,
  `record_artisan_reminders_view/3`), `workshop/artisan_reminder.ex`, `handover_live.ex` (`#artisan-reminders-link`,
  `#artisan-reminders-count`), `repair_live.ex` (`#repair-artisan-reminders`), `settings.ex` (`get/2`).
- **Voci:** ART-07 (contatore), ART-20, ART-21, ART-22, RIP-S-17 (parte solleciti). Nessuna D nuova; D-74 fatta per i
  due link WhatsApp dei solleciti.
- **Lavoro:** sottoagente d'implementazione, test del dominio e delle pagine, mutazioni a mano. Il banco non è stato
  lanciato: lo fa la sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/solleciti.go`):
  - `SogliaSolleciti` = `threshold_days/1`: il parametro `giorni` se non è vuoto dopo `String.trim/1`, altrimenti
    l'impostazione `solleciti_artigiano_giorni`; poi `Integer.parse/1` per intero (segno, sole cifre ASCII),
    `max(1, n)`; un testo che non è un numero, o nessuna impostazione, è **14** anche con l'impostazione (come il
    Flask). Nessun tetto: il 120 è solo del campo della pagina;
  - `SollecitiPendenti` = `build/2`: le buste con `consegnata_artigiano_il` e senza `rientrata_artigiano_il`
    (`out_of_shop_query/1`), con una consegna e il suo artigiano, uscite da almeno N giorni (limite compreso: il
    confine è adesso meno N·86 400 s); per artigiano **della consegna**, per nome e poi per uscita (poi id); i giorni
    fuori sono giorni interi di 86 400 s, troncati (`DateTime.diff(…, :day)`: non dipendono dall'ora del negozio);
    l'ultimo sollecito di ogni busta e del gruppo. Le buste nel cestino non si escludono, come `late_query/3`;
  - `ContaSollecitiPendenti` = `pending_count/1` (sempre la soglia dell'impostazione);
  - `RegistraVistaSolleciti` = `record_artisan_reminders_view/3` (`handover.reminders_viewed`,
    `artisan_reminders`/`index`, `days` e `count`);
  - `RegistraSollecito` = `record_artisan_reminder/4`: nella transazione rilegge le buste in ritardo dell'artigiano
    all'istante della scrittura, con il parametro della pagina; nessuna (o un artigiano illeggibile) è il rifiuto
    «Nessuna busta da sollecitare per questo artigiano.» senza scrivere; altrimenti per busta una riga
    `solleciti_artigiani` (busta, artigiano, consegna, attore, giorni fuori, istante) e `repair.artisan_reminded`
    (`artisan_id`, `handover_id`, `days_out`). Ritorna il gruppo riletto (per il testo WhatsApp);
  - `SollecitiDellaBusta` = `history/1`: dal più recente (a pari istante l'ultimo scritto).
- **Pagine** (`internal/web/solleciti.go`, `pagine/solleciti.html`):
  - `GET /handovers/reminders` (personale, come la fonte: nessuna regola di ruolo): ritorno alle consegne e link allo
    storico, il modulo GET della soglia (`giorni`, 1…120, valore la soglia usata), «Nessun sollecito pendente. Tutto
    in regola. ✓», per gruppo nome, «N busta/buste fuori bottega», telefono, «Ultimo sollecito», l'avviso del numero
    mancante, i tre comandi, le buste (numero D-64, descrizione troncata a 50/44, «Sollecitata il», «Ngg»);
    `handover.reminders_viewed` a ogni visita (se non si scrive, niente pagina);
  - `POST /handovers/reminders` (`artigiano`, `giorni` della pagina, `wa`): «⏰ Registra sollecito» (data-confirm
    «Registrare il sollecito per queste buste?», data-invio-unico) torna alla pagina con lo stesso `giorni` e
    «Sollecito registrato per N busta/buste.» o il rifiuto; «📱 WhatsApp sollecito» (`wa=numero`, solo con un numero
    componibile) e «👥 Scegli contatto» (`wa=scegli`, sempre) registrano lo stesso sollecito e vanno a
    `wa.me/<numero>?text=` con il testo del Flask («Ciao X, ti ricordo che hai ancora N buste fuori bottega. Buste:
    a, b», numero dei testi della D-64/D-82): senza JavaScript il redirect, con JavaScript (`data-avviso-invio`
    `data-channel="whatsapp"`, il gestore di «Invia» del popup della 066) il JSON `{"url": …}` `no-store`, la scheda
    aperta al tocco va a wa.me e la pagina alla lista (**D-74**). Nessuna riga nella coda in uscita;
  - il contatore `#artisan-reminders-count` accanto a «⏰ Solleciti pendenti» della lista consegne, solo sopra zero;
  - il riquadro `#repair-artisan-reminders` «⏰ Solleciti all'artigiano» della scheda (dopo quello dei giri): istante,
    «fuori da Ngg», attore se c'è; solo con un sollecito;
  - il link «⏰ Solleciti pendenti» della lista consegne e della barra di ritorno dello storico
    (`#handover-history-back`) e la voce del menu compaiono da soli con la rotta (`haRotta`): controllato da
    `TestElencoStorico` e `TestPaginaConsegne` (il test della 077 che registrava una rotta finta è tolto).

## Scelte

1. **La pagina dopo un sollecito non è una visita.** In LiveView `record-reminder` ridisegna senza `handle_params`,
   quindi senza una riga `handover.reminders_viewed`; il Go torna alla pagina con un redirect e il segno
   `DopoSollecito` nella sessione (una volta, tolto anche da ogni altra pagina) salta quella riga. Lo stesso dopo il
   rifiuto.
2. **WhatsApp come modulo POST, non come link.** Lo standalone ha un `<a href="wa.me…" target=_blank phx-click>`;
   nel Go il sollecito si scrive prima di aprire WhatsApp (come «Invia» del popup, D-74) e il testo nomina le buste
   rilette nella scrittura. Senza JavaScript la pagina va a wa.me (lo standalone senza JavaScript non fa niente).
   Il banco confronta l'indirizzo wa.me aperto, non l'`href`.
3. **Il campo della soglia ha `id="giorni"`**, come `.input` dello standalone, così il banco HTTP confronta il valore.
4. **Numeri oltre i 64 bit** valgono il massimo (lo standalone non ha limiti); oltre 100 000 giorni nessuna busta.
   Un `giorni` che non è un testo (`giorni[]=…`) vale come assente (lo standalone darebbe un errore).

## Prove

- `internal/buste/solleciti_test.go`: `TestSogliaSolleciti` (tabella di `Integer.parse/1`, impostazione, «abc» → 14
  anche con l'impostazione, zero e negativi → 1), `TestSollecitiPendenti` (limite compreso al microsecondo, 20 giorni e
  14 ore → 20, artigiano della consegna e non della busta, rientrate, senza consegna, mai uscite fuori; cestino dentro;
  ordine; soglie della pagina; contatore con l'impostazione), `TestRegistraSollecito` (rifiuti senza scrivere, riga e
  audit esatti, soglia della pagina, due tocchi due solleciti, ultimo sollecito sull'elenco, rientrate non più
  sollecitabili, storia della busta dal più recente), `TestRegistraVistaSolleciti`.
- `internal/web/solleciti_test.go`: `TestPaginaSolleciti` (pagina vuota e piena con il markup esatto, numero D-64,
  troncatura, telefono, avviso del numero, soglie «abc», 5, 0, impostazione, «+» e vuoto; righe del registro; il
  titolare), `TestContatoreSolleciti`, `TestRegistraSollecito` (403 senza token, redirect con il parametro, messaggio,
  ultimo sollecito, nessuna visita in più dopo sollecito e rifiuto, rifiuti, riquadro della scheda dopo il rientro,
  attore NULL), `TestSollecitoWhatsApp` (redirect e JSON a wa.me con il testo del Flask, «Scegli contatto», niente
  coda in uscita, rifiuto con il JSON).
- Aggiornati: `TestElencoStorico` e `TestPaginaConsegne` (il link compare da solo), `TestMenu*`, `TestRotteConGuscio`,
  la tabella dei permessi (`ruoli_test.go`), `TestScenariBrowserDelRepo`.

## Mutazioni

Ventitré mutazioni a mano, ognuna in una copia del repo (poi cancellata), con i test `Sollecit|Consegne|Contatore|Storico`
di `internal/buste` e `internal/web`: **22 uccise al primo giro, 1 viva poi uccisa**.
- Uccise: limite escluso (`<`); zero e negativi non portati a 1; segno meno ignorato; testo illeggibile che non dà 14;
  parametro di spazi non vuoto; rientrate in ritardo; ordine dei gruppi; artigiano della busta al posto di quello della
  consegna; giorni fuori su 23 ore; nessuna busta non rifiutata; attore non scritto; `days_out` dell'audit; contatore
  con un'altra soglia; solleciti della busta dal più vecchio; pagina dopo il sollecito contata come visita; parametro
  `giorni` perso dopo il sollecito; «buste» del testo del Flask cambiato; «📱» anche senza numero; numero tolto da
  wa.me; JSON senza `no-store`; contatore anche a zero; riquadro della scheda tolto.
- Viva al primo giro: l'ultimo sollecito del **gruppo** con `Before` al posto di `After` (i test avevano solleciti allo
  stesso istante). Aggiunto in `TestRegistraSollecito` del dominio il caso con le buste sollecitate in momenti
  diversi: ora uccisa.

## Controlli

`scripts/check` verde (`uscita 0`, copertura: voci 353, chiuse 41, rosse 0). I test di `tools/banco` passano
(`TestScenariBrowserDelRepo` con lo scenario nuovo). `node --check` di `banco.mjs` pulito.

## Banco

- **HTTP `solleciti`** (nuovo, legge e basta, dipendente): `/handovers/reminders` senza parametro, `?giorni=abc` (14),
  `?giorni=0` (1), `?giorni=%20120%20` (120), `?giorni=2000` (vuota anche con i dati), con `D-64`; confronta il testo
  della soglia, `attributo:giorni:value`, `#artisan-reminders-empty`, il gruppo `#artisan-reminder-group-1` e la riga
  `#artisan-reminder-bag-4` (la 4/26 della cattura, consegna 1, fuori dal 06/01/2026); poi `/handovers` con il link e
  il contatore. I giorni fuori sono blocchi di 86 400 s dall'uscita: il confine cade all'ora d'uscita della 4/26, non a
  mezzanotte. Nessuna attesa li fissa (l'HTTP ha solo `visibile:` confrontati fra le due app, il browser li scrive
  `<n>gg`); il testo visibile della riga li contiene, quindi l'HTTP può differire solo se quel confine cade fra la
  richiesta allo standalone e quella al Go.
- **Browser `consegne-solleciti`** (nuovo): sul PC con i dati la pagina, «⏰ Registra sollecito» con la conferma, «👥
  Scegli contatto» (wa.me senza numero, testo con il numero «N», la pagina torna a «/»), il riquadro della scheda
  della 4/26 (due righe) e `repair.artisan_reminded` (2); sul telefono e sul database vuoto la pagina vuota.
  **Non usa dati propri** (Dubbio 1): gira prima di `pannello-qr`, che sul PC fa rientrare la 4/26.

## Revisione (coordinatore, 15/11/2026: nessun rilievo ALTO né MEDIO, quattro BASSI corretti)

1. **Segno `DopoSollecito` tolto all'inizio della pagina** (`paginaSolleciti`): letto in una variabile e tolto prima di
   `SollecitiPendenti`; se la lettura non riesce, la sessione si salva prima della pagina d'errore (che da sola non
   scrive il cookie), così la visita dopo scrive `handover.reminders_viewed`. Test rosso prima:
   `TestSegnoDopoSollecitoConLetturaGuasta` (con una `Buste` che fa fallire `SollecitiPendenti`); rosso anche con il
   solo spostamento, senza il salvataggio della sessione.
2. **Niente N+1 per l'ultimo sollecito:** una sola `SELECT busta_id, max(creato_il) … WHERE busta_id IN (SELECT b.id
   <sqlRitardo>) GROUP BY busta_id` con lo stesso confine (`last_reminders/1`); i test restano verdi (compreso quello
   dell'ultimo del gruppo con istanti diversi).
3. `TestSollecitoWhatsApp`: il JSON si confronta dopo `json.Unmarshal`, non con un `ReplaceAll`.
4. Sezione «Banco» corretta sui giorni fuori (sopra). **Non più provato:** il ramo `{{if .Solleciti}}` falso (lista
   consegne e storico senza la rotta dei solleciti): ora la rotta c'è sempre e il test della 077 che la faceva mancare
   è stato cambiato; il ramo resta nel template finché `haRotta` lo governa.

## Dubbi

1. **Il browser non può usare dati propri.** Una busta creata dallo scenario esce adesso e la soglia è di almeno un
   giorno: non è mai «in ritardo». Lo scenario usa quindi la sola busta in ritardo della cattura, la 4/26 (consegna 1,
   «Laboratorio Sintetico»), solo sul PC e prima di `pannello-qr` (ordine dei nomi), e non la cambia: scrive solo due
   solleciti (nessun contatore li conta; il contatore della lista consegne conta le buste in ritardo). Dopo, la 4/26
   la guarda solo `pannello-qr` (modifica, rientro dal QR, «📤 Fuori»), che non legge i solleciti. Sul telefono la
   4/26 è già rientrata e la pagina è vuota nelle due app. **Raccomandazione:** tenerlo così; l'alternativa (una
   busta «vecchia» seminata nella cattura) cambierebbe i contatori della home e dello storico di molti scenari.
2. **Le buste nel cestino non si escludono.** `late_query/3` non guarda `deleted_at`: una busta nel cestino ancora dall'artigiano è nei
   solleciti e nel contatore, e un sollecito la nomina. Il Go copia. **Raccomandazione:** lasciarlo come lo
   standalone (la 082 conta «ancora fuori» senza il cestino: le due pagine possono non tornare); se si preferisce
   escluderla è una riga e una D.
3. **Artigiano con i soli solleciti** (Dubbio 2 della 076): con la 083 i solleciti si scrivono davvero, quindi il
   caso «elimina un artigiano che ha solo solleciti» (errore del vincolo, pagina d'errore nel Go, eccezione nello
   standalone) diventa possibile appena le sue consegne sono eliminate dallo storico. **Raccomandazione:** come la
   076, lasciarlo come lo standalone; se capita in negozio, un messaggio «… ha N solleciti registrati» (una D).

## Banco finale (sessione principale)

`s083p`, 15/11/2026: HTTP 75 scenari, 1246 passi, 0 inattese; browser 22 scenari, 44 viste, 2 inattese e 0 errori, tutte
e due un'attesa sbagliata con le due app uguali: in `consegne-solleciti` le attese `vuoto.elenco`/`vuoto.gruppi` del
database vuoto valevano anche con i dati sul PC (dove la pagina ha il gruppo e la scritta del vuoto è «(assente)» in
tutte e due le app). Corrette in `atteso_dati` sul valore misurato nel giro; il giro non è stato rifatto (limite
settimanale dei token in preavviso, d'accordo con il coordinatore). `scripts/check` rifatto dalla sessione principale:
verde; test di `tools/banco` verdi dopo la correzione.

Esito: banco verde salvo 2 attese corrette dopo il giro, da confermare col giro della 084. **Confermato:** `consegne-solleciti` verde nei giri `s084p` e `s084q` della 084.
