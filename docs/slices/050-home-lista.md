# 050 — La home con lista, contatori, ordine e paginazione (T4.2, prima)

## Piano della T4.2

Copiato da `piano-T42.md` (05/11/2026, sottoagente di pianificazione).

Regola comune: nello standalone la home è LiveView, ma il primo disegno di una GET è HTML confrontabile via HTTP (contatori, riquadri, filtri, paginazione, galleria). Eventi (pulsanti rapidi, 📌, pannello QR, lettore) solo dal livello browser.

- 050 Lista, contatori, ordine, paginazione (home usabile): RIP-H-01 (parte), 02, 08, 09, 10, 11, 12, 21, 56 (segno e contatore). Porting di RepairIndex (repair_index.ex: build, selection, counters, urgent?, hide_defaults, explicit_filter?) in internal/buste/indice.go; filtri usati dai riquadri (stato, scadenza, compro_oro, attesa, fuori_bottega, senza_qr, messaggi) + p; conversazioni non lette da Repair.unread_messages_query; riquadri workshop_live.ex:1170-1282, buste :1653-1845, paginazione :1847, intestazione :1949; riquadro link a /repairs/{id} (regola dei segni D-44); D-64: buste.NumeroVisibile(codice_qr, numero) «0045/26» o «N. 45/26» + «⚠ senza QR»; audit repair.index_viewed coi soli nomi dei filtri. Banco: semina (>30 buste, RIP-2026-0045 chiusa+aperta, RIP-2026-0046 due aperte, RIP-2025-0045, fissata, messaggio non letto, scadenze relative a oggi, attesa, fuori bottega, compro oro); scenari home, home-dipendente, home-contatori; normalizzatore D-64 in confronto.go. Rischi: la semina cambia totali di altri scenari; stati.go commento index_viewed; numero buste nuove dipende dal giorno.
- 051 Filtri, pillole, ricerca testuale, report dai filtri: RIP-H-03 (pre D-63), 04, 05, 06, 07, 19, 56 (pillola), RIP-S-46 dai filtri; togliere t.Skip di TestReportDaiFiltriDellaHome.
- 052 Campo unico, ricerca per etichetta, QR, gemelle (D-63, D-64): GET /cerca smista (1 → 303 scheda; più → /?cerca=); POST /cerca-qr (codice mai nell'indirizzo); migrazione indice codice_qr; audit repair.qr_lookup.
- 053 D-64 su scheda e «Nuova riparazione».
- 054 Pannello azioni QR: RIP-H-23…26.
- 055 Lettore QR con fotocamera e suono: RIP-H-27, 28, D-52; jsQR nel repo (Apache-2.0).
- 056 Pulsanti rapidi, menu stato, 📌, ritorno alla lista: RIP-H-14, 15, 16, 20.
- 057 Galleria: RIP-H-13.
- 058 D-64 nei PDF già fatti (ricevuta, 76×127, scheda PDF, report).
- 059 Manuale capitolo 06.

Domande per Stefano mandate al coordinatore il 05/11 (1-11); la 050 non ne dipende (ordine per numero interno come lo standalone; audit index_viewed una volta sola, da dichiarare).

---

- **Fonte:** riga 050 del piano qui sopra; lo standalone a `go-baseline`:
  `lib/gestrip_standalone/workshop/repair_index.ex` (`build/2`, `counters/2`, `urgent?/3`,
  `hide_defaults/2`, `explicit_filter?/1`, `order_rows/1`), `Settings.Operator.read/0`,
  `Workshop.Repair` (`unread_messages_query/1`, `unread_messages_order/0`, `out_of_shop_query/1`,
  `missing_qr_query/1`, `missing_qr?/1`, `waiting_days/2`, `date_source/2`),
  `Workshop.record_repair_index_view/2`, `WorkshopLive` (`mount/3`, `audit_index!/2`, `render/1`
  con i riquadri dei contatori, lo stream delle buste e la paginazione, `shop_header/1`,
  `dashboard_card/1`, `filter_query/3`, `delivery/1`, `waiting_age/1`, `status_label/1`,
  `status_classes/1`), `assets/css/app.css` (le regole `dash-*`); i test `repair_index_test.exs`,
  `workshop_live_test.exs`, `workshop_test.exs`, `workshop_qr_nudge_live_test.exs`.
- **Voci:** RIP-H-01 (parte: intestazione; non «📷 Scan» né la campanella), RIP-H-02, RIP-H-08,
  RIP-H-09, RIP-H-10, RIP-H-11 (senza i pulsanti a destra del riquadro: 056), RIP-H-12, RIP-H-21,
  RIP-H-56 (segno e contatore; filtro dal riquadro sì, pillola e casella del cassetto con la 051),
  RIP-H-06 per i soli filtri dei riquadri.
- **Deviazioni:** D-64 (numero visibile, dichiarata nel banco con un normalizzatore); **D-68
  proposta, da approvare** (una riga `repair.index_viewed` per visita, sotto).
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Cosa c'è

- **Dominio** (`internal/buste/indice.go`, `internal/buste/visibile.go`, nuovi):
  - `Lista(ctx, Filtri)` è `build/2`: impostazioni lette una volta (`LeggiImpostazioniHome`: la
    soglia `scadenza_giorni` con `Integer.parse/1` per intero portata fra 1 e 365, altrimenti 7; gli
    interruttori `home_mostra_pronte` e `home_mostra_consegnate` spenti solo per «0»), il cestino
    escluso, i filtri dei riquadri, il nascondimento di pronte e consegnate solo senza filtri, il
    totale, 30 buste per pagina con la pagina portata dentro i limiti, l'ordine di `order_rows/1`
    (fissate, messaggio non letto, anno e progressivo letti come interi, id), le urgenti, i contatori
    e il nome del negozio. Non scrive niente.
  - `LeggiFiltri` è `normalize/1` per `stato`, `scadenza`, `compro_oro`, `attesa`, `fuori_bottega`,
    `senza_qr`, `messaggi` e `p` (testo di Plug, spazi Unicode ai bordi tolti, `Id.positive/1` per la
    pagina); `Filtri.Attivo` è `explicit_filter?/1` su questi.
  - I contatori sono una sola query con `count(*) FILTER (…)`, ognuno con la condizione del suo
    filtro (le stesse costanti SQL): pronte, in lavorazione, preventivi, in attesa materiali,
    consegnate oggi (`data_consegna` = oggi in negozio), in scadenza (non chiuse, da oggi a oggi più
    la soglia), compro oro (tutte), in attesa del cliente (non chiuse), fuori bottega, senza QR (aperte
    con QR nullo o di soli spazi, `trim` di SQLite come lo standalone), messaggi (conversazioni non
    lette, tolte solo consegnate e annullate).
  - `Urgente` è `urgent?/3`: né pronta né chiusa, data prevista entro oggi più la soglia, scadute
    comprese; senza data mai.
  - `GiorniAttesa` è `waiting_days/2`: «AAAA-MM-GG hh:mm:ss» ora del negozio (o il giorno a
    mezzanotte), giorni interi, almeno 0; le cifre sono fisse come in `NaiveDateTime.from_iso8601/1`.
  - `RegistraLista` scrive `repair.index_viewed` (risorsa `repair_index`, id `index`) con
    `filter_keys`.
  - **D-64:** `NumeroVisibile(codiceQR, numero)` dà «0045/26» con un'etichetta (`EtichettaDi`: spazi
    ai bordi tolti, esattamente la forma di `CodiceEtichetta`, numero da 1), con l'anno dell'etichetta
    («RIP-2025-0045» → «0045/25»); ogni altro codice dà «N. 45/26». `SenzaQR` e `QRMancante` sono
    `missing_qr?/1` e `blank_qr?/1`. È la funzione unica per le slice dopo (regola in
    `docs/SVILUPPO.md`, «Numero di busta»).
- **Pagina** (`internal/web/home.go` nuovo, `pagine/home.html`, `pagine/base.html`, `app.css`):
  - `GET /` per titolare e dipendente: prima `repair.index_viewed` (se non si scrive, 500 e niente
    lista), poi la lista; titolo «Riparazioni · Laboratorio»; l'intestazione della bottega nella barra
    («GestRip», il nome del negozio in `<h1>`, «Banco riparazioni»; nuovo `Guscio.Bottega` per le pagine
    con `conBottega`); `?esito=` dopo una busta nuova funziona come prima.
  - I riquadri dei contatori con gli id, gli href, `title`, `aria-label` «Etichetta: N», il numero, e
    la cella del telefono (emoji, numero, parola breve) dello standalone; «⏳ In attesa», «📤 Fuori
    bottega» e «⚠ Senza QR» solo se contano qualcosa o il loro filtro è attivo, «💬 Messaggi» sempre.
  - La lista: «N riparazione/i», «+ Nuova», «Nessuna riparazione trovata.»; per ogni busta il
    riquadro `repair-<id>` con il link a tutta superficie `repair-open-<id>` e il numero, tutti e due a
    `/repairs/<id>` **senza segno D-44** (un'apertura vera, con la sua `repair.viewed`, come un
    indirizzo scritto a mano); cliente, data, descrizione; oreficeria (anche il negozio, come lo
    standalone), stato, 🪙 Compro Oro, ⏳ Ngg col motivo nel titolo, ⚠ senza QR, ⏰ consegna (testo o
    data), 💬/🗨, le due 🤝; bordo rosso (urgente o fissata), viola (messaggio non letto), attenuata se
    chiusa. Gli id dei segni sono quelli dello standalone.
  - Paginazione «← Precedente», «N / M», «Successiva →» con gli indirizzi di `filter_query/3` (i
    filtri dei riquadri nel loro ordine, poi `p`).
  - Tutto link e GET: senza JavaScript la home funziona (provato dai test: nessun `phx-`, nessun
    evento).
- **Banco:**
  - **Passo della home** (`tools/banco/home.go`, nuovo, dopo i numeri): venti buste nuove nello
    standalone con le funzioni delle pagine, più `Settings.Operator.put/2` (scadenza 12 giorni e le
    consegnate nascoste dalla home senza filtri): le gemelle RIP-2026-0045 (una consegnata) e
    RIP-2026-0046 (due aperte), RIP-2025-0045, una fissata, un messaggio del cliente non letto e uno
    del negozio, scadenze relative a oggi (+3, −2, +30, pronta +1), attesa, compro oro, senza QR, un
    codice non RIP, una consegnata e un'annullata scaduta, due buste del 2025 (9999/25 e 10000/25).
    Riga `GR_HOME:` controllata; le buste in tutto ora sono 40.
  - **Normalizzatore D-64** (`tools/banco/confronto.go`): nuovo campo del passo `"normalizza":
    "D-64"`; il corpo dello standalone passa da `normalizzaD64`, che mette al posto di «#21/26» (e
    «Apri busta 21/26» negli `aria-label`) il numero visibile scritto a mano in `numeriVisibiliBanco`
    (`home.go`, le 40 buste); una differenza che così torna uguale esce «ATTESA (D-64,
    normalizzata)». La D-64 si controlla approvata nell'inventario come quelle dei passi `solo_go`.
    Regole in `docs/SVILUPPO.md`.
  - **Scenari nuovi** (generati da `to-be-deleted-after-use/slice-050/scenario/genera.py`):
    - `home` (13 passi): la home, le pagine 2, 99, `abc`, 0 e `p[]=2`, i segni con i loro titoli
      (attesa, senza QR, messaggi, risposte al preventivo, consegna a testo), il registro
      `repair.index_viewed` (righe presenti, totale non confrontato: D-68), poi la scheda della 27/26
      aperta dal suo link (un'apertura vera che segna letto il messaggio) e la home dopo: la 27/26
      torna al suo posto, 🗨, «💬 Messaggi: 0». `home.json` gira dopo gli altri due scenari della
      home (ordine dei nomi), così la campanella dello standalone torna a zero prima del livello
      browser (con il messaggio non letto la scheda di firma dello standalone ha il titolo «(1) Firma
      busta…», misurato nel primo giro completo);
    - `home-contatori` (18 passi): ogni riquadro, `stato` con spazi, `scadenza=0` (è un filtro),
      `scadenza[]=1`, `compro_oro=` e `attesa=+` (non sono filtri: consegnate di nuovo nascoste),
      uno stato sconosciuto, due filtri insieme con la pagina;
    - `home-dipendente` (5 passi): la stessa home per il dipendente, un riquadro, il registro
      rifiutato.
    Ogni passo della lista confronta i contatori (aria-label, href, testo), la paginazione, i 30
    riquadri (id in ordine, numero, link, aria-label, testo del corpo) e i conteggi di
    `rep-card-urgente`, `consegna-tag-urgente`, `rep-card-pinned`, `rep-card-messaggio`; le attese
    hanno i valori misurati sui dati importati e sul database vuoto.
  - **Scenari toccati:** `report` e `report-rifiuti`, il passo con `ids=1…50`: le buste che non ci
    sono ora sono 41…50 (prima 21…50), perché la semina ne aggiunge venti. Nessun altro scenario
    cambia: nessuno guardava la lista della home, e i totali del registro si confrontano già solo sui
    filtri per azione.
  - `tools/banco/stati.go`: il commento sui totali del registro ora dice che la home scrive
    `repair.index_viewed` due volte nello standalone e una nel Go (D-68 proposta).
  - Test del banco: `TestLeggiEsitoHome`, `TestComandoHome`, `TestNumeriVisibiliBanco` (la tabella
    ricontrollata con `buste.NumeroVisibile`), `TestNormalizzaNelConfronto`; `su_test.go` con il passo
    nuovo e cinque casi di rottura; `provacon_test.go` e `dati_test.go` con i tre scenari.

## Deviazioni

- **D-64** (approvata): il numero visibile in lista è quello dell'etichetta: «#0045/26» (con il
  «#» della lista dello standalone), «N. 45/26» per una busta senza etichetta RIP; l'`aria-label`
  dei link è «Apri busta 0045/26» (lo standalone interpola il numero senza «#»). Nel banco: il
  normalizzatore, nessuna riga in `bench/deviazioni.json`.
- **D-68 (proposta, da approvare)** — RIP-H-21: lo standalone scrive `repair.index_viewed` due volte
  per visita (mount statico e connessione di LiveView, `audit_index_read/2` non è sotto
  `connected?`), il Go una sola (una GET). Stessa logica della D-44 per `repair.viewed`. **Non c'è
  la riga in `bench/deviazioni.json`**: il banco accetta solo id `D-NN` approvati nell'inventario
  (`reDev` e `controllaApprovate`), quindi «D-68 (proposta, da approvare)» lì farebbe rosso tutto il
  banco. Nessuno scenario confronta il totale delle righe `repair.index_viewed` (lo scenario `home`
  guarda solo che le righe ci siano); quando Stefano approva, si aggiunge la riga D-68
  nell'inventario e, se serve, un passo che confronta il totale con la sua riga in
  `bench/deviazioni.json`.

## Prove

- **Test Go** (orologio finto, database vero):
  - `internal/buste/indice_test.go`: `TestNumeroVisibile` (15 codici, `SenzaQR` su 8 casi),
    `TestUrgente` (17), `TestImpostazioniHome` (19 soglie, 6 interruttori), `TestLeggiFiltri`,
    `TestListaOrdine`, `TestListaOrdinePerAnno`, `TestListaContatori`, `TestListaFiltri`,
    `TestListaNascondePronteEConsegnate`, `TestListaPaginazione` (61 e 30 buste), `TestListaRiga`,
    `TestGiorniAttesa` (14), `TestRegistraLista`.
  - `internal/web/home_test.go`: `TestHomeVuota` (titolare e dipendente), `TestHomeRiquadriPortanoAlFiltro`,
    `TestHomeRiquadroBusta` (markup dei segni, ordine, link senza segno e la `repair.viewed` della
    scheda aperta), `TestHomeCompraOroEAttesaSenzaData`, `TestHomePaginazione`, `TestHomeRegistro`
    (chiavi, una riga per visita, `/nuova` non scrive, 500 senza riga), `TestHomeNascondePronteEConsegnate`,
    `TestHomeSenzaAccesso`.
  - Ritoccati: `TestSchedaNonTrovata` (non conta le righe della home), `TestSchedaNonAperta` (la
    busta ora è nella lista, la scheda no), `TestMarkupDelGuscio` (il contenitore della home).
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-050/mutazioni/muta.py`, 15 punti, test
  mirati di `internal/buste` e `internal/web`): **15 uccise, 0 sopravvissute**. Ordine delle fissate
  al contrario; messaggi non letti tolti dall'ordine; progressivo prima dell'anno; nascondimento
  anche con i filtri; scadenza senza oggi; urgente senza l'ultimo giorno; pronta urgente; messaggi
  contati anche delle consegnate; senza QR anche delle chiuse; pagina oltre l'ultima non riportata;
  chiavi del registro con un valore di soli spazi; etichetta senza la forma esatta; riquadro dei
  messaggi nascosto a zero; paginazione che perde `attesa`; attesa del cliente contata anche sulle
  chiuse.
- `scripts/check` verde (gofmt, vet, staticcheck, errcheck, build, test, copertura: 353 voci, 26
  chiuse, 0 rosse, 71 parziali, 245 aperte, 11 fuori v1; segreti e dati veri OK).

## Banco

Istanza `s050`, `gestrip-standalone:fab-222`.

- Esplorazione (scenario usa e getta con tutti i campi di 40 buste su 12 pagine): l'unica
  differenza era l'`aria-label` «Apri busta 21/26» senza «#», sistemata nel Go e nel normalizzatore.
- Scenari della home da soli, dati importati: 34 passi, 280 differenze attese (tutte D-64
  normalizzate), 0 inattese.
- Primo giro completo, dati importati: HTTP verde; browser rosso in `firma-riquadro` per il titolo
  «(1) …» della campanella dello standalone (sopra, corretto con gli ultimi due passi di `home`) e
  per due 500 di `/repairs/6/photos/…` dello standalone (il «database is locked» noto della 040a, solo
  standalone; nel giro dopo non c'è).
- **Esito finale, dati importati** (`-dati sintetici-import`, istanza nuova): HTTP 41 scenari, 676
  passi, 312 differenze attese (280 normalizzate D-64, le altre D-01 e D-56 di prima), **0 inattese**, 0 errori;
  browser 8 scenari, 16 viste, 22 attese, **0 inattese**, 0 da decidere. `banco giu` e `banco
  controlla`: nessun avanzo.
- **Esito finale, database vuoto** (istanza nuova): HTTP 41 scenari, 676 passi, 17 attese, **0
  inattese**, 0 errori (23 scenari non provati con questi dati, come previsto); browser 8 scenari,
  0 inattese. Nessun avanzo.

## Review indipendente

Una review di un altro sottoagente ha trovato questi punti, tutti chiusi in questa slice:

- **Medio.** Con `"normalizza"`, un campo che il Go dava **uguale al grezzo** dello standalone
  passava anche se il Go avrebbe dovuto mostrare il numero D-64. Ora è inatteso, con il campo
  «… (D-64 non applicata dal Go)» e il valore normalizzato come valore dello standalone
  (`confrontaCon`, test in `TestNormalizzaNelConfronto`). Gli scenari della home restano verdi: il Go
  applica la D-64 su ogni campo dei passi normalizzati.
- **Bassi.**
  1. I filtri della 051 (`cerca`, `oreficeria`, `artigiano`, `notifiche`, `data_da`, `data_a`,
     `data_campo`) oggi sono **ignorati** dal Go: non filtrano e **non contano** in `Filtri.Attivo`,
     quindi con `?cerca=x` la home del Go nasconde ancora pronte e consegnate (se l'impostazione lo
     dice) e mostra tutta la lista, mentre lo standalone filtra. Scelta: rimandati tutti insieme alla
     051, che porta lettura, filtro, `Attivo` e pillole in un colpo solo; contarli in `Attivo` senza
     filtrare mostrerebbe una lista che non è né quella dello standalone né quella di oggi. Nessuno
     scenario della 050 li usa (solo il registro, dove le chiavi si registrano già).
  2. «Nuova riparazione» ha ora la stessa intestazione della bottega della home (lo standalone usa
     `shop_header/1` in tutte e due): `datiNuova.bottega()`, test `TestNuovaConLaBottega`.
  3. I parametri della home e le chiavi del registro si leggono con `parametroPlug(...).testo()`
     (`testoPlug`), come `Plug.Conn.Query.decode/1`: `stato[=x` è un'altra chiave e non tocca lo stato,
     `stato[a]=x` dopo lo stato lo rende una mappa. Test `TestHomeParametriComePlug`.
  4. Test in più: QR di soli spazi scritto direttamente e consegnata con la data futura
     (`TestListaContatori`), «2026-10-29 10:00:01.5» dà 1 giorno (`TestGiorniAttesa`), `TestToniStato`
     sui 10 stati più uno sconosciuto, `TestIndirizzoLista` (tutti i filtri, ordine, codifica),
     `TestHomeRisposteAlPreventivo` (solo accettato, solo rifiutato, lette), la pagina 2 senza filtri
     in `TestListaNascondePronteEConsegnate`, l'aria-label vero «Apri busta N/AA» di ogni busta in
     `TestNumeriVisibiliBanco`. Mutazioni A–J della review più tre sulle correzioni (D-64 non
     applicata accettata, parametri letti con `parametroQuery`, «Nuova» senza bottega): **11 su 11
     uccise** (`to-be-deleted-after-use/slice-050/mutazioni/esiti-review.txt`).
  5. L'ordine degli scenari della home è documentato in `docs/SVILUPPO.md` e controllato da
     `TestOrdineScenariHome` (home-contatori e home-dipendente prima di home; il penultimo passo di
     home apre la 27/26).
  6. Nell'intestazione `<p>`/`<h1>` stanno in un `<div>` dentro il link (prima uno `<span>`); stesso
     aspetto (`display: block` c'era già).
- **Dopo le correzioni:** `scripts/check` verde; giro mirato (home*, report*, dati importati): 6
  scenari, 101 passi, 293 attese, 0 inattese; giro completo con i dati importati: HTTP 41 scenari,
  676 passi, 312 attese, 0 inattese, 0 errori; browser 8 scenari, 16 viste, 22 attese, 0 inattese.
  Banco spento, nessun avanzo.

## Per il manuale

- La pagina iniziale «Riparazioni» mostra in alto i riquadri con i numeri: Pronte, In lavorazione,
  Preventivi, In attesa materiali, Consegnate oggi, In scadenza, Compro oro, Messaggi; «In attesa»,
  «Fuori bottega» e «Senza QR» solo quando ce n'è almeno una.
- Toccando un riquadro la lista mostra solo quelle buste; «🏠 Lista» (o il logo GestRip) torna alla
  lista completa.
- In cima stanno le buste fissate con 📌, poi quelle con un messaggio del cliente da leggere, poi le
  altre dalla più recente.
- Il numero in grande è quello dell'etichetta QR («#0045/26»); una busta senza etichetta mostra il
  suo numero interno «N. 45/26» e, se è ancora aperta, «⚠ senza QR».
- Una busta rossa è da consegnare entro i giorni della soglia (di solito 7) o è già in ritardo; viola
  ha un messaggio non letto; sbiadita è chiusa.
- La lista ha 30 buste per pagina, con «← Precedente» e «Successiva →» in fondo.
- Se nelle impostazioni le pronte o le consegnate sono nascoste, si vedono comunque toccando il loro
  riquadro.
- Toccando una busta si apre la sua scheda.

## Dubbi e punti per Stefano

1. **D-68 da approvare** (sopra): una riga `repair.index_viewed` per visita nel Go, due nello
   standalone. Raccomandazione: approvarla, come la D-44.
2. **Il «⚠ senza QR» con un codice che non è un'etichetta.** Lo standalone mette il segno solo alle
   buste aperte con QR **vuoto** (RIP-H-56). Una busta con un codice di un altro formato
   («SINT-QR-0005», un codice del Flask) nel Go mostra «N. 5/26» (D-64: non è un'etichetta) ma **non**
   il segno, e non è contata in «Senza QR»: ho seguito lo standalone. Se Stefano vuole il segno anche
   lì, cambiano segno, contatore e filtro (una riga in `SenzaQR` e in `senzaQRSQL`).
3. **«#0045/26» e «N. 45/26».** Il «#» resta davanti al numero dell'etichetta come nella lista dello
   standalone e nel titolo «Busta #0045/26» dello studio D-64; davanti a «N. 45/26» no («#N. 45/26»
   sarebbe doppio). Da confermare.
4. **Il logo dell'intestazione** non c'è: nel Go non c'è ancora la rotta `/branding/logo.jpg`
   (ADM-05). L'intestazione ha «GestRip», il nome del negozio e «Banco riparazioni».
5. **Consegnate oggi** dipende dal giorno della semina: un giro del banco che passa la mezzanotte
   fra `banco su` ed `esegui` cambia contatori e attese (per tutte e due le app, ma le attese degli
   scenari della home sono fisse). Lo stesso per le scadenze relative a oggi.
6. **Oreficeria «il negozio» nel riquadro:** lo standalone mostra il nome dell'oreficeria anche per
   la riga 1 (il negozio stesso); copiato.
