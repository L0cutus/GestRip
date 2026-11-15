# 085 — «🔧 Sollecita artigiano» e numero D-82 sulla pagina dell'artigiano (piccola, prima della T4.9)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/customer_notice.ex` (`open/2` e
  `send_to/4` del purpose `:artisan`), `lib/gestrip_standalone/outgoing.ex` (`destination_for_artisan/1`,
  `compose_artisan/1`, `resolve_artisan_link/2`), `lib/gestrip_standalone_web/components/notice_popup.ex` (`title/1`,
  `hint/1`, `problem_text/1`, `refusal/1`), `lib/gestrip_standalone_web/live/repair_live.ex` (`#repair-artisan-notify`,
  `assign_artisan_send/2`).
- **Voci:** ART-23 (il `{link}` del testo `wa_artigiano`), CLI-29 (parte artigiano del popup). **D-82 estesa** alla
  pagina pubblica dell'artigiano (Stefano, 15/11/2026: riga D-82 dell'inventario già estesa dal coordinatore). Nessuna D
  nuova.
- **Decisioni di Stefano del 15/11/2026** sui dubbi delle slice 082, 083 e 084 registrate sotto i dubbi delle tre
  ricevute («Deciso da Stefano il 15/11/2026: …»).
- **Lavoro:** slice piccola fatta dalla sessione principale (niente sottoagente d'implementazione, per il budget della
  settimana: d'accordo con il coordinatore, «sottoagenti solo quanto serve»); revisione indipendente con mutazioni da
  un sottoagente.

## Cosa c'è

- **Dominio** (`internal/buste/sollecito_artigiano.go`, nuovo): lo scopo `ScopoArtigiano` («artigiano»), il tipo
  `TipoSollecito` («artisan»), la parte `ParteArtigiano`, il problema `ProblemaChiusa`, i rifiuti
  `ErrArtigianoCambiato` e `ErrBustaChiusa`. `ApriAvviso(…, ScopoArtigiano)` compone la bozza senza scrivere niente:
  destinatario sempre l'artigiano della busta (anche su una busta B2B), numero per wa.me dal suo telefono; busta chiusa
  → problema «chiusa» (prima del numero, come `destination_for_artisan/1`); senza artigiano o senza un numero con delle
  cifre → «nessun recapito». Il testo `wa_artigiano`: nome dell'artigiano (o «Collega»), numero dei testi senza «N.»
  (D-82), prima riga della lavorazione, stato in maiuscolo, consegna e note pubbliche come righe, `{link}` letterale,
  il negozio; niente del cliente. Un testo salvato che non si compone: nessuna proposta.
- `InviaAvviso` con lo scopo dell'artigiano: testo vuoto, tipo diverso da «artisan» (e «artisan» con un altro scopo),
  artigiano cambiato (un altro, tolto o messo), numero cambiato o tolto, canale, busta chiusa → rifiuto senza scrivere.
  Poi `{link}` diventa il link dell'artigiano (`RisolviLinkArtigiano` della 084: firmato, non salvato; senza indirizzo
  pubblico `ErrSenzaIndirizzoPubblico`, D-46 estesa), il testo senza spazi ai bordi, la riga WhatsApp «accepted» verso
  `artisan` e `outgoing.handed_off` con `party: artisan`. Nessun timbro sulla busta (`link_inviato_il` resta com'è),
  nessun link della busta emesso.
- **Pagina** (`internal/web/avvisi.go`, `pagine/avviso.html`, `pagine/scheda.html`): sulla scheda «🔧 Sollecita
  artigiano» (`#repair-artisan-notify`, `/repairs/{id}/avviso?scopo=artigiano` con il segno della scheda) solo con un
  artigiano con il numero su una busta aperta (`Scheda.Sollecitabile`, `artisan_send_address`); la riga «Nessun
  telefono per l'artigiano…» della 084 resta com'era. Il popup: «🔧 Sollecita l'artigiano», il suggerimento «{link}
  diventa il link dell'artigiano all'invio…», i problemi «La busta è chiusa: l'artigiano non si sollecita.» e «Nessun
  telefono per l'artigiano: il sollecito su WhatsApp non può partire.», il campo nascosto `artigiano`, il rifiuto
  «L'artigiano della busta è cambiato mentre il messaggio era aperto: niente è stato inviato. Riaprilo.» e «La busta è
  chiusa: niente è stato inviato.». Senza indirizzo pubblico la riga `#notice-link-missing` nomina solo `{link}`.
- **Pagina dell'artigiano:** il numero è `NumeroNeiTesti` («0045/26», «45/26» senza «N.»), D-82 estesa.
- **Banco:** scenario browser nuovo `sollecito-artigiano` (ART-23, CLI-29) con dati propri: per vista un artigiano
  nuovo con il telefono e una busta del 2025 (8086/25 sul PC, 8087/25 sul telefono, QR `SINT-SOLLECITO-085-<vista>`)
  affidata a lui dalla modifica; pulsante, popup, «Annulla», «Invia» senza `{link}`, wa.me, riga «· all'artigiano»,
  `outgoing.handed_off`; alla fine la busta torna senza artigiano e l'artigiano si elimina (menu delle consegne e righe
  dell'anagrafica degli scenari dopo invariati).

## Scelte

1. **Il controllo dell'artigiano** confronta gli id così come sono (anche l'1): non `stessaControparte`, che per le
   oreficerie tratta la riga 1 come «nessuna».
2. **Busta senza artigiano e modulo senza artigiano** (solo un modulo forzato: il popup lì non ha il modulo): come lo
   standalone, la cui bozza ha il problema `:no_address`, «Nessun recapito per questo messaggio: niente è stato
   inviato.» (`ErrNessunRecapito`), senza scrivere (correzione della revisione).
3. **Senza indirizzo pubblico** la riga del popup dice «{link} non si può usare: <cosa configurare>. Togli {link} dal
   testo per mandarlo senza link.» (la frase della D-46 estesa, senza `{telegram_optin}`, che il testo dell'artigiano
   non ha). Lo standalone ha sempre l'host.
4. **Dopo «Invia»** come gli altri avvisi: senza JavaScript il redirect a wa.me, con JavaScript la lista (D-74).

## Per il manuale

- Sulla scheda di una busta affidata a un artigiano che ha il telefono c'è «🔧 Sollecita artigiano».
- Il pulsante apre un messaggio WhatsApp già scritto per l'artigiano: numero della busta, lavorazione, stato, consegna
  prevista e note per lui. Il nome e il telefono del cliente non ci sono mai.
- Nel testo, `{link}` diventa al momento dell'invio il link della pagina riservata all'artigiano (busta, lavorazione,
  stato e le sole foto autorizzate per lui). Il testo si può correggere prima di premere «Invia».
- «Invia» registra il messaggio in «📨 Invii registrati» con « · all'artigiano» e apre WhatsApp.
- Su una busta consegnata, annullata o chiusa il pulsante non c'è. Se l'artigiano non ha il telefono, sotto il link
  per l'artigiano c'è «Nessun telefono per l'artigiano: il sollecito su WhatsApp non può partire.».
- Sulla pagina dell'artigiano il numero della busta si scrive senza «N.» davanti, come sulla pagina del cliente.

## Prove

- `internal/buste/sollecito_artigiano_test.go`: bozza completa (testo esatto, niente del cliente, aprire non scrive),
  consegna e note come righe, «Collega», numero dell'etichetta, busta B2B; senza artigiano, telefono nullo, vuoto, di
  soli spazi o senza cifre; le tre chiusure (anche senza numero); testo salvato non valido; invio con `{link}` (il link
  apre la pagina dell'artigiano, riga, wa.me, audit, nessun timbro, link della busta invariato), invio senza `{link}`
  senza indirizzo pubblico; tutti i rifiuti senza scrivere.
- `internal/web/sollecito_artigiano_test.go`: pulsante con il segno, popup (titolo, numero, piattaforma, campi
  nascosti, testo, suggerimento, niente del cliente), invio con il link, riga « · all'artigiano»; senza pulsante con
  l'artigiano senza numero, sulla busta chiusa e senza artigiano; popup forzati con i due problemi; rifiuti
  «artigiano cambiato» e «chiusa» senza scrivere; senza indirizzo pubblico la riga, il ridisegno con i campi nascosti e
  l'invio senza `{link}`.
- `internal/buste/artigiano_link_test.go`: il numero della pagina senza etichetta è quello dei testi, senza «N.».
- **Rosso provato:** tolto l'aggancio in `ApriAvviso`/`InviaAvviso`, cadono 5 test del dominio; i test web sono stati
  scritti dopo il codice della pagina (la revisione li prova con le mutazioni).

## Mutazioni

Revisione indipendente (sottoagente, senza le conclusioni della slice): 29 mutazioni a mano, tutte compilano, **27
uccise**. Sopravvissute: una equivalente (la riga registrata con l'indirizzo del modulo, già uguale a quello del database
per il controllo appena prima); una vera, l'ordine «chiusa» prima di «senza artigiano» su una busta chiusa e senza
artigiano: **coperta** con un caso nuovo in `TestSollecitoSenzaRecapito` («nessun recapito», come `:no_artisan` dello
standalone che viene prima della chiusura).

## Revisione

Nessuna ALTA né MEDIA. Tre BASSE, corrette:

1. Modulo forzato senza artigiano su una busta senza artigiano: ora `ErrNessunRecapito` con «Nessun recapito per questo
   messaggio: niente è stato inviato.» (`refusal(:no_address)` dello standalone, a cui `deliver/3` arriva per il
   problema della bozza), non «Il recapito è cambiato…». Test nel dominio e nella pagina.
2. `LinkArtigiano` vuoto anche senza la sottochiave: in `gestrip serve` c'è sempre (`cmd/gestrip` la deriva dalla chiave
   del database); scritto nel commento.
3. Commenti della pagina dell'artigiano e del test della proiezione allineati alla D-82 estesa.

Differenza notata dalla revisione e non nuova: dopo «Invia» lo standalone mostra il flash «Sollecito registrato in
lista: WhatsApp si apre con il testo.»; il Go, come per tutti gli altri avvisi dalla 066, porta alla lista senza flash
(D-74). Vedi Dubbio 3.

## Controlli

`scripts/check` verde (sessione principale, sul codice finale): copertura 353 voci, chiuse 42, rosse 0, parziali 160,
aperte 140, fuori v1 11. ART-23 passa da chiusa a parziale perché ora ha uno scenario del banco
(`sollecito-artigiano`): senza l'esito del banco (`-banco`) le voci con scenari restano parziali; l'esito di s085q è
verde ma è andato via con `banco giu` (la cartella dell'istanza). Il test di `tools/banco` conta il ventitreesimo
scenario browser.

## Banco (sessione principale)

- **`s085p`** (15/11/2026, con il «memoria libera» del coordinatore e la guardia sotto 10 GB, mai scattata): HTTP 76
  scenari, 1273 passi, 0 inattese; browser 23 scenari, 46 viste: **`sollecito-artigiano` verde nelle due app**, 58
  inattese e 2 errori tutti da una causa sola, una corsa nello script: `attendi` di `anagrafica-artigiani`
  (`waitForFunction(x => document.body.innerText.includes(x))`) ha letto `document.body` nullo mentre il Go caricava
  la pagina dopo l'invio («Cannot read properties of null (reading 'innerText')»); lo scenario si è fermato sul PC
  prima di eliminare il suo artigiano, che sul telefono ha spostato le righe dell'anagrafica (e quindi «elimina
  rifiutato» su un artigiano senza buste) e il menu di `sessioni-consegna`. Non dipende dalla slice (l'anagrafica non è
  cambiata) né dal Go. **Corretto:** le 11 attese sul testo della pagina in `banco.mjs` (3 `includes`, 8 `.test`) ora
  guardano prima che `document.body` ci sia. `TestScenariBrowserDelRepo` conta il ventitreesimo scenario.
  **Perché proprio adesso:** il codice della 085 non tocca l'anagrafica né il ricaricamento delle pagine del Go (nessuna
  rotta o template di `/settings/anagrafiche` cambiato). Durante s085p (15:38–15:58) il revisore faceva girare i 29
  `go test` delle mutazioni (fino alle 15:51): CPU occupata e tempi diversi. È un'ipotesi coerente con gli orari, non
  misurata; da qui la regola nuova in SVILUPPO («Banco e revisione»).
- **`s085q`** (finale, codice definitivo, «memoria libera» del coordinatore alle 16:02, guardia mai scattata, nessun
  altro carico): HTTP 76 scenari, 1273 passi, 0 inattese, 0 errori; browser 23 scenari, 46 viste, 0 inattese, 0
  errori. **Verde.** Giri serviti: 2 (il primo rosso per la corsa dello script, il secondo sul codice definitivo dopo
  le correzioni della revisione).

## Misura

Implementatore: sessione principale (Opus), niente sottoagente; revisione Opus (circa 143k token, 18 minuti); revisione
0 ALTE, 0 MEDIE, 3 BASSE; mutazioni 27 uccise su 29 (1 equivalente, 1 coperta dopo); banco 2 giri (s085p rosso per la
corsa dello script del banco, s085q verde).

## Dubbi

1. ~~**Scelta 2**~~ **Chiuso dopo la revisione:** il modulo forzato senza artigiano ha il messaggio dello standalone.
2. **Scelta 3** (riga senza indirizzo pubblico che nomina solo `{link}`): conseguenza della D-46 estesa.
   **Raccomandazione:** tenere così.
3. **Flash dopo «Invia».** Lo standalone dice «Sollecito registrato in lista: WhatsApp si apre con il testo.» (e
   «Messaggio registrato in lista…» per gli altri avvisi); il Go non dice niente e torna alla lista (D-74), dalla 066.
   **Raccomandazione:** considerarlo parte della D-74; se Stefano vuole il messaggio, è una riga per tutti gli avvisi.
