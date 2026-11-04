# 047 — Firma di consegna (T4.4, sesta e ultima)

- **Fonte:** riga 047 del piano della T4.4 nella ricevuta della 042; lo standalone a `go-baseline`:
  `DeliverySignature` e `DeliverySignature.Event` (`lib/gestrip_standalone/delivery_signature*.ex`),
  `DeliverySignatureController` e `delivery_signature_html/new.html.heex`, `assets/js/signature_pad.js`,
  la sezione `#repair-signature` di `repair_live.ex`, `Workshop.collection_changes/2`, la migrazione
  `20260913230000_create_delivery_signatures`, i test `delivery_signature_test.exs`,
  `delivery_signature_controller_test.exs` e `repair_live_test.exs` («the delivery signature on the
  detail»).
- **Voci:** RIP-S-36 (riquadro nella scheda), RIP-S-37 (pagina di firma), RIP-S-38 (salvataggio =
  consegna), RIP-S-39 (`firma.png`), RIP-S-40 (rifai firma).
- **Deviazioni:** nessuna. Porting fedele: ogni comportamento visibile è quello dello standalone,
  misurato sul banco (sotto).
- **Lavoro:** implementazione di un sottoagente, con un giro di mutazioni mirate sui punti critici.
  Review indipendente e giro finale alla sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/firma.go`, nuovo):
  - `Ritirabile(stato, compraOro)` è `collection_changes/2`: lo stato «ritirata» è `consegnata`
    (riparazioni) o `chiusa` (compro oro); ci si arriva solo con il grafo della RIP-S-13; chi c'è già
    può firmare senza cambi («Aggiungi firma»).
  - `DecodificaFirma` è `decode/1`: il prefisso `data:image/png;base64,` alla lettera, il base64 come
    `Base.decode64/1` (padding obbligatorio, niente spazi né a capo, i bit di riempimento non contano:
    misurato nell'immagine `fab-222`), poi i byte magici del PNG e al più 500 KiB. Come nello
    standalone, «PNG valido» vuol dire i byte magici: niente decodifica, niente che riscriva un byte.
  - `CatturaFirma` è `create/2` con `capture/4`: busta (non trovata → home), poi il campo (non valido →
    niente scritto, nemmeno il file), poi, con il lucchetto dell'archivio, il file salvato nella
    cartella media con le regole delle foto (`media.Archivio.SalvaConLucchetto`: nome = SHA-256, 0400)
    e nella transazione `UPDATE … WHERE firma_chiave IS NULL`: zero righe è «La firma è già stata
    registrata su questa busta.», che vince sullo stato; un grafo che non porta a «ritirata» annulla
    la transazione («Questa busta non può essere consegnata da dove si trova: firma non disponibile.»;
    il file resta nell'archivio, come nello standalone, e lo raccoglie la pulizia). Nella stessa
    transazione: stato, data di consegna solo se manca (oggi in negozio, come `CambiaStato`),
    `conservazione_iniziata_il` (l'orologio della conservazione delle foto), le quattro colonne della
    firma, la riga `captured` di `eventi_firma_consegna`, `repair.signature_captured` con
    `storage_key` e `named_collector` (mai il nome), e la chiusura del lotto d'avviso se la busta era
    l'ultima da ritirare. Nessuna `repair.status_changed`, nessuna notifica.
  - «Ritirato da»: spazi Unicode ai bordi tolti, i primi 120 grafemi, vuoto = nessuno (NULL).
  - `AzzeraFirma` è `reset/2`: `UPDATE … WHERE firma_chiave IS NOT NULL` toglie le quattro colonne e
    lascia stato e data di consegna; il file resta; il registro aggiunge una riga `reset` che lo nomina,
    con il nome di chi aveva ritirato; `repair.signature_reset`. Senza firma: «Su questa busta non c'è
    nessuna firma da azzerare.».
  - `LeggiFirma` è `read/3` con il pubblico `:staff`: i byte dall'archivio (impronta ricontrollata a
    ogni lettura, fuori dalla transazione) e `repair.signature_viewed` «ok», o nil e «denied» (busta
    senza firma, file mancante o cambiato, chiave non valida); una busta che non c'è nil senza riga. Se
    la riga non si scrive, niente byte (il controller dello standalone risponde lo stesso 404).
  - `ApriFirma` è il `with` di `new/2`: non trovata, già firmata (prima dello stato), non consegnabile,
    o i dati della pagina. Nessuna scrittura.
  - `Scheda.Firma` (`FirmaBusta`, con `Timbro()` = `signature_stamp/1`: «Firmata il …» nel fuso del
    negozio, «Firmata (data non registrata)» per una firma importata senza istante, « — ritirata da …»).
- **Pagine e rotte** (`internal/web/firma.go`, `pagine/firma.html`, sezione `firmaBusta` di
  `pagine/scheda.html`):
  - `GET /repairs/{id}/firma` (personale, guscio senza «🏠 Lista»: `fabs={false}`, nuovo campo
    `guscio.SenzaPulsanti`), `POST /repairs/{id}/firma` (personale), `GET /repairs/{id}/firma.png`
    (personale, senza guscio), `POST /repairs/{id}/firma/reset` (solo titolare, come la pipeline
    `:require_owner_user`: il dipendente va a / con «Operazione riservata al titolare.»). Tutte nella
    tabella dei permessi; nessuna rotta della faccia pubblica (`TestFirmaSoloInterna`).
  - Testi, id, attributi, stili in linea (45vh, 14rem, 44 e 48 px), codici, redirect e messaggi dello
    standalone. Il redirect tiene l'id com'era nel percorso, scritto come `~p` (`codificaURI`, estratto
    da `disposizione`): `/repairs/03/firma` → `/repairs/03`, `/repairs/+3/firma` → `/repairs/%2B3`
    (misurato).
  - `firma.png`: `image/png` senza charset, `no-store`, `no-referrer`; busta non firmata, inesistente
    o con un id che non è un numero: lo stesso 404 «Firma non disponibile», senza tipo, come le foto.
  - Sezione «Firma di consegna» della scheda fra le foto e il link pubblico: «Nessuna firma registrata
    su questa busta.» e «✍️ Consegna con firma» / «✍️ Aggiungi firma» solo se la busta può arrivare a
    «ritirata»; con la firma il timbro, l'immagine e, solo al titolare, «↺ Rifai firma» (modulo POST
    vero con `data-confirm` sul modulo, come nello standalone).
- **Riquadro in JavaScript** (`app.js`): `signature_pad.js` dello standalone riga per riga (delegato
  dal documento, legato una volta): canvas con i pixel del dispositivo, tratto 2,5 px `#102a6b`; la
  firma esiste dal primo tratto (`pointermove` durante il disegno), non dal primo tocco; riquadro vuoto
  → `alert("Firma mancante.")` e il modulo non parte; «Pulisci» svuota riquadro e campo; un riquadro
  già firmato non si svuota girando il telefono. CSS delle due pagine in `app.css`.
- **Banco:**
  - `bench/scenari/firma.json` (65 passi, `prova_con` sui dati importati): firma importata della 3/26
    (scheda, `firma.png` con la sua impronta, azzeramento, «Aggiungi firma» e nuova firma), la 15/26
    pronta (pagina, rifiuti, firma, seconda conferma, dipendente, azzeramento del titolare), la 11/26
    in attesa materiali (rifiuto senza scritture), la 1/26 in lavorazione, la 13/26 compro oro chiusa,
    buste e id che non ci sono. Attese fissate sui valori misurati, tranne data e ora del giorno del
    giro (confrontate fra le due app).
  - **`in_fondo`** (nuovo campo degli scenari HTTP, `TestScenariInFondo`): lo scenario della firma
    cambia buste lette da altri scenari, quindi gira dopo tutti gli altri.
  - `bench/browser/firma-riquadro.json` e la funzione `firma` di `banco.mjs` (livello browser):
    desktop sulla 2/26 (già consegnata: «Aggiungi firma») e telefono sulla 6/26 (pronta: «Consegna con
    firma»), col dito vero sul telefono (eventi touch di Chromium). Riquadro vuoto, un tocco solo, un
    tratto e «Pulisci»: «alert: Firma mancante.» e nessun POST; poi tratto, «Ritirato da» e «Conferma
    firma»; «↺ Rifai firma» con Annulla e con OK; «Annulla» della pagina di firma. Nessun errore
    JavaScript, nessuna risposta 4xx/5xx, niente fuori origine.

## Per il manuale

- Sulla scheda di una busta pronta c'è «✍️ Consegna con firma»: apre una pagina con un riquadro
  bianco dove il cliente firma col dito sul telefono del negozio (o col mouse).
- «Conferma firma» registra la firma e, insieme, consegna la busta (compro oro: la chiude). Un
  riquadro vuoto o toccato una volta sola non parte: compare «Firma mancante.».
- «Ritirato da» si riempie solo se a ritirare non è l'intestatario. «Pulisci» ricomincia, «Annulla»
  torna alla scheda.
- Su una busta già consegnata il pulsante dice «✍️ Aggiungi firma»: la firma si aggiunge e la consegna
  resta quella di prima. Una busta in lavorazione non si può firmare.
- Dopo la firma la scheda mostra quando è stata fatta, chi ha ritirato e l'immagine della firma. La
  firma la vede solo il personale, mai il cliente o l'artigiano dal loro link.
- Solo il titolare ha «↺ Rifai firma»: cancella la firma (dopo una conferma) e riapre la pagina per
  rifarla; la consegna resta registrata e la firma vecchia resta conservata nel registro.

## Misurato sullo standalone (banco, `gestrip-standalone:fab-222`, 04/11/2026)

- **HTTP** (esplorazione con tutte le attese «?», poi lo scenario finale): sui 62 passi e 540 valori
  le due app hanno dato gli stessi valori, nessuna differenza. Fra questi: i redirect (`/login`,
  `/repairs/N`, `/repairs/N/firma`, `/`), i messaggi, `/repairs/%2B3` per `+3`, le intestazioni di
  `firma.png` (200 `image/png`, `no-store`, `no-referrer`, `nosniff`; 404 «Firma non disponibile» senza
  tipo, 21 byte, uguale per non firmata, 999, `abc` e 0), il titolo «Firma busta #15/26 ·
  Laboratorio», l'impronta della firma importata (27 byte, `c6e0136e…`) e di quella mandata (70 byte,
  `cb507919…`), la firma importata rifirmabile dopo l'azzeramento («Aggiungi firma»), la data di
  consegna storica che resta.
- **Browser** (`firma-riquadro`, desktop e telefono): 54 valori per vista, uguali: «alert: Firma
  mancante.» con zero POST per il riquadro vuoto, il tocco solo e dopo «Pulisci» (riquadro a 0 pixel,
  campo vuoto); un solo POST alla conferma; i byte serviti da `firma.png` uguali a quelli mandati;
  il PNG ha il tratto, colore più frequente `16,42,107` (`#102a6b`), altezza 446 px (desktop) e 752
  px (telefono, 376 px CSS × 2), 1 e 2 pixel per px CSS; «confirm: Cancellare la firma e rifarla? La
  consegna resta registrata.» con Annulla (resta) e OK (pagina di firma con «Firma azzerata: puoi
  rifirmare.»).
- **Il PNG del browser non è lo stesso nelle due app** (dichiarato): la larghezza del riquadro dipende
  dall'impaginazione (contenuto largo 672 px nello standalone, 640 nel Go sul desktop), quindi
  cambiano larghezza e byte del PNG. Il banco confronta quello che conta (sopra), mai i byte.
- `Base.decode64/1` di Elixir 1.18.4 (nell'immagine, con `erl`): `QR==` e `YWJ=` accettati (bit di
  riempimento non a zero), `QQ`, `QQ=`, `Q Q==`, `QQ==\n`, `YWJj\nZA==`, `YW-_` rifiutati.

## Solo ragionato (dal codice e dai test dello standalone, non misurato sul banco)

- Le due conferme quasi contemporanee: nello standalone il lucchetto `MediaStore.with_lock` e
  l'`UPDATE` con `signature_storage_key IS NULL`; nel Go il lucchetto dell'archivio e lo stesso
  `UPDATE` (provato dai test Go: `TestCatturaFirmaConcorrente`, 10 giri, e
  `TestCatturaFirmaBloccoNellaScrittura` senza lucchetto). Il banco HTTP manda un passo alla volta.
- Le righe che il banco non vede: `eventi_firma_consegna` (`captured`, `reset` con la chiave e il
  nome), le righe d'audit (`repair.signature_captured`, `_reset`, `_viewed` con `ok`/`denied` e
  `{"audience":"staff"}`), `conservazione_iniziata_il`, la chiusura del lotto, il file che resta
  nell'archivio dopo un rifiuto per stato o dopo l'azzeramento. Tutto nei test Go.
- Il limite di 500 KiB (al banco un corpo così grande non sta in uno scenario): nei test Go, anche
  dalla pagina.
- La data di consegna «oggi»: lo standalone usa l'ora locale del container (`local_today/0`), il Go
  l'orologio del negozio come `CambiaStato`; sul banco coincidono.
- Il nome di «Ritirato da»: `String.trim/1` e `String.slice/3` sui grafemi, copiati con
  `unicode.IsSpace` e `uniseg` come altrove nel Go.

## Differenze e dubbi

1. **Scheda dopo le azioni della firma: apertura vera.** Dopo «Conferma firma», un rifiuto, «Rifai
   firma» o «Annulla» il Go va alla scheda senza il segno di D-44, quindi la scheda scrive la sua
   `repair.viewed`. È quello che fa lo standalone (la firma è un controller, non un evento della
   LiveView: dopo il redirect la scheda si monta di nuovo), e anche «Rifai firma» lì è un modulo POST
   vero. Scelto lo standalone; D-44 resta per gli eventi della scheda.
2. **Un parametro `firma_png` ripetuto con e senza parentesi** (`firma_png=…&firma_png[]=x`): Plug ne
   farebbe una lista (rifiuto), il Go legge il campo senza parentesi. Nessun browser lo manda; non
   corretto.
3. **Token CSRF della pagina di login dopo l'accesso** (trovato sul banco a database vuoto, non di
   questa slice): un POST con il token della pagina `/login` di prima dell'accesso è accettato dallo
   standalone (302) e rifiutato dal Go (403, il Go cambia il token all'accesso). Nell'uso vero ogni
   modulo viene da una pagina caricata dopo l'accesso; lo scenario ora apre `/` dopo ogni accesso.
   Da guardare con le voci dell'accesso (ACC), se serve una decisione.
4. **Tocco del telefono nel banco:** un tratto che finisce senza il dito fermo fa perdere a Chromium il
   tocco dopo (lo legge come la fine di una spinta), uguale nelle due app; lo scenario fa fermare il
   dito prima di alzarlo (scritto in `docs/SVILUPPO.md`). Non è un difetto delle app: con eventi
   sintetici il tratto ha una velocità che un dito vero non ha alla fine della firma.

## Cosa resta della T4.4

Con la 047 il codice della T4.4 è finito. Restano:

- la **prima slice del manuale d'uso** (T4.3 e T4.4), dalle sezioni «Per il manuale» delle ricevute;
- la **T5.5, stampa vera** sulla MUNBYN ITPP130 (opzioni CUPS, `PageSize` su una coda «driverless»,
  CUPS che accetta il lavoro a stampante spenta: ricevuta della 046);
- RIP-S-46 parziale fino alla T4.2 (report dai filtri della home) e RIP-S-44 finché una pagina non usa
  «Condividi / Stampa».

## Prove

- **Test Go** nuovi: `internal/buste/firma_test.go` (decodifica, grafo, consegna, «Ritirato da», busta
  già ritirata, stato non consegnabile, seconda firma, concorrenza, blocco nella scrittura, rifiuti
  senza scritture, transazione con l'audit o il registro che non si scrivono, senza archivio, lotto,
  data di consegna storica, azzeramento, lettura con SHA-256 alterato, file mancante e chiave non
  valida, firma importata, pagina, timbro); `internal/web/firma_test.go` (pagina, rifiuti, salvataggio,
  rifiuti del salvataggio con PNG non valido e oltre 500 KiB, `firma.png`, file alterato,
  azzeramento del titolare e del dipendente, sezione della scheda, nessuna rotta pubblica);
  `tools/banco/infondo_test.go`. Aggiornati: tabella dei permessi, rotte con il guscio, intestazioni
  di sicurezza (`no-referrer` di `firma.png`), scenari del repo.
- **TDD:** il codice è nato prima dei test in questa slice; al posto del rosso iniziale, mutazioni
  mirate (`to-be-deleted-after-use/slice-047/mutazioni/`): 16 tentate, 14 uccise; una viva era un buco
  (data di consegna storica su una busta pronta) ed è coperta da `TestCatturaFirmaTieneLaDataDiConsegna`
  (poi uccisa); una è equivalente (il `Content-Type` tolto prima del `Flush`: net/http non lo indovina
  con il corpo vuoto, come per le foto).
- **`scripts/check`:** verde (sottoagente, codice finale), anche con l'esito del banco sui dati
  importati (`GESTRIP_BANCO_ESITO`): voci 353, chiuse 68, rosse 0, parziali 19 (senza esito: chiuse
  26, parziali 61).
- **Banco** (sottoagente, binari ricompilati dal codice finale, immagine `fab-222`):
  - giro completo sui dati importati: HTTP 38 scenari, 637 passi, 32 differenze attese (D-xx già
    approvate), **0 inattese**; browser 8 scenari, 16 viste, 22 attese, **0 inattese**, 0 da decidere;
    «BANCO VERDE» e «BANCO BROWSER VERDE»;
  - giro completo sul database vuoto: browser verde; HTTP con 12 inattese, tutte in `firma` (il token
    di login del dubbio 3); scenario corretto con `GET /` dopo ogni accesso e rifatto con giri mirati
    del solo `firma` sui due dati: 65 passi, 0 inattese tutti e due;
  - prima, giri d'esplorazione (attese «?») per leggere i valori delle due app: HTTP 540 valori
    uguali, browser 2×54 uguali dopo due correzioni dello scenario (colore del tratto per frequenza,
    dito fermo sul telefono);
  - «AVANZI: nessuno» dopo ogni giro.

## Review indipendente (04/11/2026)

Sottoagente diverso, codice confrontato con lo standalone al tag `go-baseline`. Nessun difetto grave o
medio; la ricevuta corrisponde al codice sui punti verificati (byte della firma, SHA-256 a ogni
lettura, file mai cancellato al reset, transazione e ordine file → commit come lo standalone,
concorrenza con `firma_chiave IS NULL`, limite di 500 KiB dopo la decodifica, accessi e 404
identico, firme importate, riquadro uguale a `signature_pad.js`, `in_fondo` stabile).

- **Corretto:** «Aggiungi firma» su una busta già consegnata non deve chiudere il lotto (mutazione
  M13 viva): nuovo `TestCatturaFirmaAggiuntaNonChiudeIlLotto`.
- **Mutazioni:** 26, più M13 rifatta; 23 uccise, M23 equivalente. Restano vive M17 (lucchetto
  dell'archivio contro la pulizia fra salvataggio e commit: serve un test che intrecci la pulizia) e
  M18 (busta nel cestino fra la lettura e la transazione: messaggio «già firmata» invece di «non
  trovata»), buchi noti.
- **Da decidere (coordinatore e Stefano):** il token CSRF di un modulo caricato prima dell'accesso
  non vale più dopo (rinnovo del segreto al login, scelta della 006b e della 008b). Lo standalone lo
  accetta; non è una deviazione dichiarata. Proposta: dichiararla (nuova D-xx o estensione di D-30)
  senza cambiare il comportamento.
- **Uguali allo standalone, in lista:** un salvataggio del file che fallisce e un file di firma
  alterato non lasciano una riga nel log interno; un rifiuto lascia il PNG nell'archivio fino alla
  pulizia.
