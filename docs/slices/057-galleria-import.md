# 057 — Galleria della home, D-76 e D-77 all'import, indice 0016 (T4.2, nona)

- **Fonte:** coordinatore, 08/11/2026. Lo standalone a `go-baseline` = `302439d` (slice 225):
  `workshop_live.ex` (linguette `#view-list` e `#view-gallery`, `view_path/2`, `filter_query/3` con
  `vista`, `refresh/1`, `assign_gallery/3`, `sign_gallery/2`, `gallery_src/3`, `gallery_grid/1`, il
  blocco `#repair-gallery`, `#list-return` con `list_path/1`, il cassetto con il campo nascosto
  `vista`, `search_hidden/1`, «⧉ Schede PDF»), `repair_photos.ex` (`gallery/1`, `audit_gallery/2`,
  `read_staff_media_covered/4`), `gallery_token.ex`, `repair_photo_controller.ex` (`covered?/4`),
  `settings/operator.ex` (`galleria_dim`, `gallery_size/1`), `assets/js/img_retry.js`, il test «the
  gallery view (slice 202)»; per la D-77 anche `legacy_public_links.ex` e
  `public_repair_controller.ex` (vedi i Dubbi). Le decisioni di Stefano del 07/11/2026 (D-76, D-77,
  indice 0016 lasciato a noi).
- **Voci:** RIP-H-13 (galleria), RIP-S-33 (il gettone ora lo firma la galleria), RIP-H-22 e RIP-H-30
  (D-76 e D-77 all'import), OPS-03 (regole sui dati dell'import).
- **Deviazioni:** **D-76** e **D-77** fatte; D-64 (numero visibile nella galleria, anche nel testo
  alternativo della foto); D-68 estesa a `repair.staff_gallery_viewed` (da confermare, Dubbio 1).
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

### A. La galleria della home (RIP-H-13)

- **Linguette** nella testa della lista: «☰ Lista» (`#view-list`, `title` «Vista lista») e «🖼
  Galleria» (`#view-gallery`, «Vista galleria»), dentro `<nav aria-label="Vista">`, con
  `aria-current="page"` su quella scelta. Portano gli stessi filtri **senza la pagina**
  (`indirizzoVista`, come `view_path/2`), «/» senza niente.
- **`vista=galleria`** è la vista e non un filtro: `buste.Filtri.Galleria`, letta solo col testo
  esatto «galleria» (`Map.get(params, "vista") == "galleria"`, niente spazi tolti, una lista non
  conta). Non cambia `Attivo()` (nessuna pillola, nessun «× Azzera filtro», pronte e consegnate
  nascoste come senza filtri), non entra nella riga `repair.index_viewed`, e la ricerca per numero
  d'etichetta (D-64, `Effettivi`) la tiene.
- **Dove resta** (come `filter_query/3`, dopo `data_campo` e prima di `p`): paginazione, «⧉ Schede PDF»
  (il report la ignora, come lo standalone), i campi nascosti del campo unico (`search_hidden/1`) e del
  cassetto (dopo `notifiche`, come lo standalone), `#list-return` `data-path` (in ordine di nome, come
  `Plug.Conn.Query.encode/1`), il campo `lista` delle azioni del riquadro (`listaDelModulo`), i
  redirect di `GET /cerca` e la lista disegnata dal POST del campo unico, «Chiudi» del pannello QR.
  **Non resta**, come nello standalone: nei riquadri dei contatori (indirizzi fissi) e in «× Azzera
  filtro» (`/`). Le pillole sono testo, non link; l'ordinamento della lista non si sceglie (lo
  standalone non ha un ordinamento da scegliere).
- **La galleria** (`internal/web/galleria.go`, modello `galleria` in `home.html`) prende il posto di
  `#repairs` (che non c'è): per le buste della pagina con almeno una foto (`buste.Galleria`, la prima
  per `posizione`, poi `id`, e quante sono; tutte le foto della busta, come il personale le vede)
  una cella `#gallery-<id>`:
  - la foto: `<a href="/repairs/<id>#repair-photo-viewer-<foto>" title="Apri foto">` (pagina intera,
    così `:target` apre il visualizzatore della scheda), `<img src="…?size=thumb&g=<gettone>"
    data-retry alt="Foto 0045/26" loading="lazy" decoding="async">`, «+N» (`.gallery-count`) se le foto
    sono più di una;
  - i dati: link alla scheda con numero visibile (D-64), data di ricezione, cliente, oreficeria, stato,
    🪙 («Compro oro»), 🔧 («Fuori bottega presso artigiano»), ⏰ consegna (rossa se urgente, e bordo
    rosso della cella), 💬 («Messaggio non letto»).
- **Messaggi:** sopra, `#gallery-summary` «N schede con foto · M senza foto nascoste» («1 scheda»; la
  seconda parte solo con M > 0). Senza foto nella pagina `#repair-gallery-empty` «Nessuna scheda con foto
  trovata.» con «M senza foto escluse dalla galleria.» se M > 0. La paginazione resta quella della
  lista (30 buste, con o senza foto), «N riparazioni» anche.
- **Dimensione:** `galleria_dim` dalle impostazioni (`buste.LeggiImpostazioniHome`): «S», «M», «L»
  esatti, ogni altro valore o nessuno «M» (`gallery_size/1`). Classi `gallery-S` (due colonne sul
  telefono, celle da 140 px), `gallery-M` (220 px), `gallery-L` (320 px) in `app.css`. Il Go **non
  aveva** l'impostazione: si legge la chiave importata dallo standalone (l'import porta tutte le
  chiavi di `settings`); la pagina per cambiarla non c'è ancora (vedi «Non portato»).
- **Con JavaScript:** una miniatura con `data-retry` che non arriva si chiede ancora una volta dopo
  1,5 s con `&retry=1` (`img_retry.js` dello standalone, slice 205, in fondo ad `app.js`); il pannello
  del QR della 055 va sopra la galleria come sopra la lista (`nellaLista` guarda `#repairs` o
  `#repair-gallery`).

### B. Audit e gettone (RIP-H-13, RIP-S-33)

- Ogni visita della galleria con almeno una foto scrive **una** riga `repair.staff_gallery_viewed`
  (`buste.RegistraGalleria`): attore, esito ok, risorsa `repair`, id `gallery`, dati
  `{"photo_ids":[…]}` nell'ordine della pagina. È una lettura in autocommit come le altre
  (`b.reg.Registra`, come `repair.staff_photo_viewed` della 045b/055b), fuori da ogni transazione.
- **Solo se la riga c'è** ogni miniatura riceve il gettone di `foto.go` (slice 037: HMAC-SHA256 con la
  sottochiave «galleria», utente, busta, foto, firmato all'inizio del giorno UTC, valido due giorni):
  la rotta delle foto serve la miniatura coperta senza una riga sua (`LeggiMiniaturaCoperta`); se
  ripiega sulla foto intera la riga la scrive lo stesso. Se la riga non si scrive (errore dell'audit)
  la pagina si mostra senza gettoni e ogni miniatura scrive la sua riga (`sign_gallery/2`). Nessuna
  foto, nessuna riga.
- **Riscrittura:** lo standalone (LiveView) scrive la riga al primo disegno e alla connessione, e la
  riscrive dentro lo stesso socket solo se cambiano le foto o il giorno. Il Go non ha socket: una riga
  per visita (una GET), come `repair.index_viewed` con la D-68. L'indirizzo della miniatura resta lo
  stesso tutto il giorno (il gettone è del giorno), quindi il browser la trova nella cache. Vedi il
  Dubbio 1.
- Tolto il test saltato `TestGalleriaDellaHomeFirmaIlGettone` (`internal/buste/pagine_test.go`): la
  parte di RIP-S-33 che aspettava la galleria c'è.

### C. D-76 e D-77 all'import (`internal/importer/riscritture.go`)

Dopo la copia di tutte le tabelle, nella stessa transazione, prima dei controlli delle chiavi esterne
e della riconciliazione (i conteggi non cambiano), **prima i numeri e poi i codici** (così l'avviso
dei codici ha il numero finale). `aggiornato_il` non si tocca. Lo standalone importa e tiene numeri e
codici com'erano.

- **D-77, numeri con zeri davanti:** ogni `buste.numero` che `buste.NormalizzaNumero` cambia
  («0046/26» → «46/26», «007/25» → «7/25», «00/26» → «0/26») si riscrive. Avviso
  `buste/numero_senza_zeri` (tabella `repairs`, colonna `bag_number`, `righe` quante, `buste` con id,
  numero nuovo e `prima`).
  - **Caso non deciso (scelta provvisoria):** se il numero senza zeri è già di un'altra busta
    (cestino compreso: l'indice unico lo copre) **o** due buste importate lo darebbero tutte e due
    («05/26» e «005/26»), nessuna di quelle si riscrive; avviso a parte
    `buste/numero_senza_zeri_occupato` con id, numero tenuto e numero `occupato`. Domanda per Stefano
    nei Dubbi.
  - **Contatore:** il valore del numero non cambia, quindi `sequenze_buste` resta quello dello
    standalone e la lettura larga della 055a (`numeroDellAnno`) dava già 46 a «0046/26». Dopo la
    riscrittura il contatore che arriva a «46/26» trova il numero occupato, si riallinea e dà «47/26»
    (`TestContatoreDopoLaRiscrittura`): è proprio il difetto 1 di `docs/domande-aperte.md` che si
    chiude.
- **D-76, codici QR:** ogni `codice_qr` che `buste.NormalizzaCodice` cambia si riscrive: spazi ai bordi
  (anche Unicode e tab), «rip-» minuscolo, e — quando il risultato è esattamente un'etichetta — gli
  spazi accanto ai trattini («RIP - 2026 - 0045» → «RIP-2026-0045»). `NormalizzaCodice` non tocca la
  barra «/» (quella è di `EtichettaScritta`, per i numeri scritti nei moduli). Un codice che diventerebbe
  vuoto (solo spazi) resta com'è: vale già «senza QR». Avviso `buste/codice_qr_normalizzato` (colonna
  `qr_code`, `righe`, `buste` con id e numero).
- **Report:** `Voce` ha il campo nuovo `buste` (`BustaReport`: `id`, `numero`, `prima`, `occupato`).
  Mai il codice, mai un dato del cliente: il numero interno non lo è (commento di `Report` aggiornato).
  Il report resta «riconciliato»: sono avvisi, non differenze.
- **Banco:** i dati sintetici non hanno né numeri con zeri né codici da riscrivere (lo standalone li
  normalizza già quando li crea nei passi della catena, e la cattura Flask sintetica non ne ha): il
  report dell'import Go del banco non ha questi avvisi e nessun campo confrontato cambia. Nessun
  normalizzatore nuovo per D-76/D-77.

### D. Indice 0016 su `codice_qr`: tolto (migrazione 0017)

- **Verifica:** `EXPLAIN QUERY PLAN` (test temporaneo, tolto; esito in
  `to-be-deleted-after-use/slice-057/prove/piano-con-indice.txt`) su un database migrato con 300 buste e
  `ANALYZE`, sulle query vere che guardano `codice_qr`: `righeConCodice` nelle due forme (scansione e
  pannello QR, gemelle di «Nuova riparazione» e della scheda: `instr(b.codice_qr, ?)`),
  `busteDellEtichetta` (`LIKE`), la ricerca scritta della lista e del report (`cercaSQL`, `LIKE`), il
  filtro e il contatore «⚠ Senza QR» (`trim(b.codice_qr) = ''`). Tutte `SCAN b`: nessuna usa
  `buste_codice_qr`. Nessun'altra query del Go nomina `codice_qr` nel `WHERE` (le altre lo leggono
  per id).
- **Scelta:** tolto con `internal/db/migrazioni/0017_via_indice_codice_qr.sql` (`DROP INDEX
  buste_codice_qr;`, con il perché in testa, come le altre). Non serve a nessuna lettura e costa una
  scrittura in più a ogni busta salvata; lo standalone non l'ha mai avuto. La 0016 resta com'è (le
  migrazioni vanno solo avanti e la loro impronta è fissata). `IndiciSoloGo` non nomina più
  `buste(codice_qr)`; `TestMigrazioniSuDatabaseVuotoEDopoIlRiavvio` vuole 17 migrazioni e l'indice
  assente.
- **Banco e dati veri:** il banco crea il database Go con `gestrip import`, che applica tutte le
  migrazioni: la 0017 gira lì come sul database vuoto. La regola di `docs/prova-dati-veri.md` riguarda
  le migrazioni dello **standalone** fra l'immagine e `go-baseline`: non cambia (la 225 non ne ha), e
  il database Go della prova si crea nuovo con l'import. Nessuna modifica lì.

## Deviazioni

- **D-76, D-77** (Stefano, 07/11/2026): fatte come sopra; il caso del numero occupato è una scelta
  provvisoria.
- **D-64** (approvata): nella galleria il numero è quello visibile («#0045/26», «N. 45/26») e il testo
  alternativo della foto «Foto 0045/26» (lo standalone «Foto 21/26», il numero interno). Il
  normalizzatore D-64 del banco riconosce anche «Foto N/AA» (`reNumeroStandalone`).
- **D-68 estesa (da confermare):** una riga `repair.staff_gallery_viewed` per visita; lo standalone ne
  scrive due (disegno statico e connessione) e nessuna in più finché il socket vive.
- **D-63** (approvata): le linguette usano i parametri dei link (`parametriLink`): un codice
  d'etichetta letto non va nell'indirizzo; lo standalone mette sempre `cerca`.

## Prove

- **Test Go nuovi:**
  - `internal/buste/galleria_test.go`: `TestGalleria` (prima foto per posizione, quante, buste senza
    foto e non chieste assenti, nessun id), `TestRegistraGalleria` (riga e dati), `TestFiltriVista`
    (solo «galleria» esatto, non un filtro, tenuta dalla ricerca per etichetta),
    `TestImpostazioneGalleria` (S/M/L, ogni altro valore M, anche nella `Lista`).
  - `internal/web/galleria_test.go`: `TestGalleriaHome` (celle, link della foto e dei dati, `src` con il
    gettone, `alt` D-64, «+1», buste senza foto assenti, riassunto, linguette, niente `#repairs`;
    titolare e dipendente), `TestGalleriaRigaEGettone` (una riga per visita con le foto nell'ordine,
    gettone valido solo per utente, busta e foto, miniatura servita senza riga, stesso gettone nel
    giorno, nessuna riga senza foto, la lista non scrive righe della galleria),
    `TestGalleriaSenzaRigaSenzaGettone` (audit rifiutato da un trigger: pagina 200, niente gettone, la
    miniatura scrive la sua riga), `TestGalleriaVuota` (i due messaggi, «1 scheda con foto»),
    `TestGalleriaDimensione`, `TestGalleriaVistaConservata` (linguette senza pagina, paginazione,
    report, campi nascosti, `#list-return`, «× Azzera filtro» e riquadri senza vista, valori non
    esatti = lista, `lista` del 📌, `GET /cerca` anche con un numero d'etichetta, POST del campo unico),
    `TestSchedeDellaGalleria`, `TestScriptDellaGalleria` (`app.js`, `app.css`).
  - `internal/importer/regole_dati_test.go`: `TestRiscrittureDellImport` (numeri e codici riscritti e
    non, soli spazi, NULL, «00/26» occupato da «0/26», avvisi con righe e buste, nessun codice nel
    report), `TestNumeroSenzaZeriGiaOccupato` (occupato nel cestino, due candidate allo stesso numero),
    `TestContatoreDopoLaRiscrittura` (contatore importato invariato, la busta nuova prende «47/26»).
- **Test cambiati:** `TestHomeRicercaECassetto` (ora `vista=galleria` resta nei campi nascosti, nel
  cassetto e nel report), `TestImpostazioniHome` (il quarto campo), `TestScriptDelPannelloSopraLaLista`
  (`nellaLista`), `TestMigrazioniSuDatabaseVuotoEDopoIlRiavvio` (17 e indice assente),
  `TestNumeriVisibiliBanco` («Foto N/AA»), `TestOrdineScenariHome`, `TestProvaConDegliScenariDelRepo`,
  `TestScenarioUtentiSuiDueDati` (gli `atteso_dati` di `home-galleria`), `TestScenariBrowserDelRepo`
  (`galleria-home`); `busteFinte` con i due metodi nuovi.
- **Banco:** scenario HTTP nuovo `bench/scenari/home-galleria.json` (11 passi: sette pagine della
  galleria e della lista con circa 300 campi ciascuna, normalizzatore D-64, più due passi `solo_go`
  D-63 su `GET /cerca`), scenario browser nuovo `bench/browser/galleria-home.json` con
  `galleriaHome` in `tools/banco/browser/banco.mjs` (regole dentro ogni app, vedi «Banco»). Le attese di `home-galleria` sono i valori di un
  giro di sonda sugli scenari che girano prima (stesso stato dei dati), uguali nelle due app.

## Mutazioni

A mano, 20 punti (`to-be-deleted-after-use/slice-057/mutazioni/muta.py`, test mirati, file rimessi a
posto dopo ognuno). **20 uccise**, nessuna viva: `vista` tolta dai link, gettone anche senza riga,
gettone sulla foto sbagliata, gettone firmato all'ora e non all'inizio del giorno, prima foto per id,
nascoste non contate, riga anche senza foto, `vista` con gli spazi tolti, ricerca per etichetta che
toglie la vista, dimensione in minuscolo accettata, cassetto senza `vista`, linguetta con la pagina,
pannello QR solo sopra la lista, D-77 anche con il numero occupato, D-77 con due candidate, D-76 anche
dei codici di soli spazi, D-76 con i soli spazi ai bordi, riscritture spente, codice QR nel report,
normalizzatore D-64 senza «Foto».

## Banco

`gestrip-standalone:fab-225`, binari dell'albero di lavoro.

- **Primo giro, dati importati** (`s057`): HTTP 48 scenari, 873 passi, **11 inattese**, tutte dello
  scenario nuovo: `repairs-total` è un id del solo Go (tolto dal confronto) e `GET /cerca` c'è solo
  nel Go (D-63: i due passi sono diventati `solo_go`). Il livello browser non è partito (HTTP rosso).
- **Sonda** (stessa istanza rifatta da capo, solo gli scenari che girano prima di `home-galleria`,
  attese finte): i valori veri dei campi, uguali nelle due app, da cui sono scritte le attese.
- **Secondo giro, dati importati** (`s057`, da capo): HTTP **0 inattese**; browser **10 inattese**,
  tutte di `galleria-home`: numero di celle, «+N» e id delle foto diversi fra le due app perché gli
  scenari browser di prima (caricamento, coda, sfondo, schede, tentativi) caricano foto in modo
  diverso nelle due app (D-58, D-59), anche sulle buste della semina. Non è un difetto della galleria
  (il livello HTTP, sugli stessi dati, dava le stesse celle). Lo scenario ora osserva regole che
  valgono dentro ogni app: almeno una cella, gettone e `data-retry` su tutte, riassunto con il numero
  delle celle, «+N» nella forma giusta, visualizzatore aperto sulla foto del link, scheda della cella.
- **Terzo giro, dati importati** (`s057`, da capo): HTTP 48 scenari, 873 passi, 1182 attese, **0
  inattese**, 0 errori; browser 12 scenari, 24 viste, 28 attese, **0 inattese**, 0 errori: **verde**.
  Report dell'import Go: riconciliato, 9 avvisi, nessuno `buste/…` (i dati sintetici non hanno né
  numeri con zeri né codici da riscrivere).
- **Database vuoto** (`s057v`): HTTP 48 scenari, 873 passi, 158 attese, **0 inattese** (30 non
  provati con questi dati); browser 12 scenari, 0 inattese (11 non provati): **verde**.
- `banco giu` dopo ogni giro: «AVANZI: nessuno».

## Controlli

- `scripts/check` verde sul codice finale (gofmt, vet, staticcheck, errcheck, build, test,
  copertura: 353 voci, 26 chiuse, 0 rosse, 88 parziali, 228 aperte, 11 fuori v1; segreti e dati
  veri OK). RIP-H-13 è passata da aperta a parziale (test e banco `home-galleria`, `galleria-home`).
- `scripts/parole-vietate --staged` dopo `git add -A`: nessuna parola (poi `git reset`).

## Per il manuale

Aggiunte a `docs/manuale/da-scrivere.md`, sezione «Lista delle buste (slice 056 e 057)»:

- «☰ Lista» e «🖼 Galleria» cambiano vista tenendo i filtri; la pagina riparte dalla prima.
- La galleria mostra la prima foto di ogni busta della pagina che ne ha, «+2» se ce ne sono altre,
  e sotto numero, data, cliente e stato; la foto apre la scheda con quella foto grande, i dati la
  scheda.
- In cima quante schede hanno foto e quante senza foto sono nascoste; senza foto nella pagina
  «Nessuna scheda con foto trovata.».
- Pagine, filtri, ricerca, «⧉ Schede PDF» e «← Indietro» della scheda restano nella galleria; «×
  Azzera filtro» e i riquadri in alto tornano alla lista.
- La grandezza delle foto è un'impostazione del negozio *(da fare: la pagina delle impostazioni)*.
- Per l'import (amministrazione): numeri con gli zeri e codici QR scritti male si correggono da soli,
  il report dice quali buste; un numero già occupato resta com'è ed è segnalato a parte.

## Non portato

- **La scelta della dimensione della galleria** (S/M/L, `gallery_size` nella pagina delle impostazioni
  dell'operatore dello standalone): il Go non ha ancora quella pagina (area delle impostazioni, ADM).
  Il valore si legge già, quello importato.
- Niente altro di RIP-H-13.

## Dubbi

1. **Una riga `repair.staff_gallery_viewed` per visita.** Lo standalone ne scrive due per visita
   (disegno statico e connessione di LiveView) e poi, nello stesso socket, solo quando cambiano le foto
   o il giorno. Il Go non ha socket: una per GET, come la D-68 per `repair.index_viewed`. Da confermare
   come estensione della D-68 (o da dichiarare come D nuova).
2. **D-77, numero senza zeri già occupato — domanda per Stefano.** Scelta provvisoria: la busta non
   si riscrive, tiene «0046/26», e il report lo dice nell'avviso `buste/numero_senza_zeri_occupato`
   (con il numero occupato); lo stesso se due buste importate darebbero lo stesso numero («05/26» e
   «005/26»: nessuna delle due). Così il difetto 1 resta per quelle buste. Le alternative: rinumerare
   una delle due (con il contatore), o bloccare l'import finché non si corregge a mano nello
   standalone.
3. **D-77 e i link Flask già mandati (da sapere per la T4.5 e la T4.6).** Nello standalone la firma di
   `/stato/<busta>/<anno>?t=…` (`verify_status`), quella di `/artigiano/…` (`verify_artisan`) e
   l'impronta del codice del link (`repair_code_digest`, salvata in `hash_codice_flask`) sono calcolate
   sul numero **com'era salvato** («0046/26»). Dopo la D-77 la busta ha «46/26». Quando il Go porta
   quelle rotte deve trovare la busta con `NormalizzaNumero` e verificare firma e codice con il numero
   **scritto nell'indirizzo** (quello firmato da Flask), non con quello salvato; altrimenti i QR già
   stampati delle buste riscritte smettono di funzionare. Scritto in `docs/SVILUPPO.md`, «Numero di
   busta». Oggi nel Go quelle rotte non ci sono, quindi niente è rotto.
4. **D-76 tocca anche gli spazi accanto ai trattini** («RIP - 2026 - 0045», «rip-2026- 0045»), come
   `NormalizzaCodice` della 055d, solo quando il risultato è un'etichetta; la barra «/» no (non è nei
   codici, solo nei numeri scritti). Un codice di soli spazi non si riscrive (resta «senza QR»).
5. **`galleria_dim` senza pagina:** il Go legge la chiave importata ma non la fa cambiare. Va con
   l'area delle impostazioni.
6. **Ricerca per numero d'etichetta nella galleria:** toglie tutti i filtri ma non la vista (la vista
   non è un filtro). Lo standalone non ha la ricerca per etichetta (D-64), quindi non c'è un confronto.
7. **Testo alternativo con il numero visibile** («Foto 0045/26», «Foto N. 45/26»): è la regola D-64
   (ogni testo che mostra il numero). Si legge un po' strano con «N.».
8. **Inventario privato** non toccato: le note «Portato (slice 057)» di RIP-H-13, RIP-S-33 (gettone
   firmato), le righe D-76 e D-77 fatte e il caso occupato le scrive il coordinatore.

## Review

Sottoagente revisore, 08/11/2026, su pc-fisso (`~/sviluppo/gestrip-go`, albero di lavoro non
committato), confrontato con lo standalone a `go-baseline` (`workshop_live.ex`: linguette,
`refresh/1`, `assign_gallery/3`, `sign_gallery/2`, `gallery_src/3`, `filter_query/3`,
`view_path/2`, `list_path/1`, il cassetto; `repair_photos.ex`: `gallery/1`, `audit_gallery/2`,
`read_staff_media_covered/4`; `gallery_token.ex`; `img_retry.js`; il test «the gallery view (slice
202)»; `legacy_public_links.ex` e `public_repair_controller.ex` per la D-77) e con RIP-H-13
dell'inventario privato (letta, non toccata).

**Cosa ho guardato e verificato**

- **Gettone e audit:** il gettone si firma solo dopo che `RegistraGalleria` è riuscita, foto per
  foto con gli stessi id della riga (`foto[i]` e `Voci[i]` vanno insieme); senza riga nessun `g`.
  L'HMAC lega utente, busta, foto e inizio del giorno UTC, vale 48 ore come `@max_age` dello
  standalone: un gettone non apre un'altra foto, un'altra busta o un altro utente. Le buste nel
  cestino non sono nella lista, quindi né nella galleria né nella riga; la rotta delle foto rifiuta
  comunque una busta nel cestino anche con un gettone valido (`mediaAutorizzato`). Le foto non hanno
  un cestino loro.
- **Import:** le riscritture girano su `c.tx`, dopo la copia e prima di `chiaviEsterne`,
  `sequenze`, `riconcilia` e del commit: un blocco successivo le toglie con il rollback. Il caso
  occupato (anche da una busta nel cestino, l'indice `UNIQUE` di `numero` copre tutte) e le due
  candidate allo stesso numero non riscrivono niente e non perdono dati. Un `dopo` non può essere il
  `prima` di un'altra candidata (il `dopo` non ha zeri davanti), quindi non c'è un ordine che liberi
  un numero. Nessun trigger della 0014 scatta su `UPDATE … SET numero`/`codice_qr`. Il report ha
  solo id e numeri interni (mai il codice: provato dal test). Il contatore resta quello importato e
  la busta nuova salta il numero occupato (`TestContatoreDopoLaRiscrittura`).
- **Migrazione 0017:** stile delle altre (commento in testa, nessun `IF EXISTS` come nelle altre;
  l'idempotenza la dà `schema_migrations` con l'impronta). Provato a parte un database fermo alla 16
  con l'indice: `Migra` applica solo la 17, l'indice sparisce, una seconda `Migra` non applica
  niente, `integrity_check` ok.
- **EXPLAIN QUERY PLAN rifatto da me** (test temporaneo, tolto; esito in
  `to-be-deleted-after-use/review-057/piano-con-indice.txt`): database migrato, 300 buste, **indice
  ricreato a mano** e `ANALYZE`, con un controllo positivo (`codice_qr = ?` usa `buste_codice_qr`,
  quindi il pianificatore lo vedeva). Le query vere: `righeConCodice` nelle due forme (catturata la
  stringa SQL della funzione), `busteDellEtichetta`, `Lista` (conteggio e righe) e `Selezione` con
  `dove` per ricerca scritta, «⚠ Senza QR», numero d'etichetta e nessun filtro, il contatore «Senza
  QR». Tutte `SCAN b` o per `rowid`: nessuna usa l'indice. `grep` di `codice_qr` nel codice non di
  test: nessun'altra query lo mette nel `WHERE`.
- **`vista=galleria` punto per punto con lo standalone:** resta nella paginazione (dopo
  `data_campo`, prima di `p`), in «⧉ Schede PDF», nei campi nascosti della ricerca
  (`search_hidden/1`) e del cassetto (dopo `notifiche`, come la riga 1386), in `#list-return`, nelle
  linguette (senza `p`); non resta in «× Azzera filtro», «Reset» del cassetto e nei riquadri, fissi
  anche lì. Non entra in `repair.index_viewed` (la lista di `record_repair_index_view/2` non ha
  `vista`). Nelle azioni del riquadro, in `GET /cerca` e nel «Chiudi» del pannello QR il Go la porta
  perché lì lo standalone non cambia pagina (LiveView): stesso effetto.
- **Celle:** stessi elementi, link, `alt`, «+N», 🪙/🔧/⏰/💬, bordo rosso, riassunto e vuoto;
  `img_retry.js` uguale.

**Corretto (con test)**

- Test nuovo `TestGalleriaCestinoEGettoneAltrove` (`internal/web/galleria_test.go`): una busta
  messa nel cestino esce dalla galleria e dalla riga successiva; il gettone che la sua miniatura
  aveva prima dà il 404 «Foto non disponibile» con la riga `denied`; lo stesso gettone sulla foto
  di un'altra busta non copre (la miniatura scrive la sua riga). Prima nessun test della galleria
  provava il cestino.
- Test nuovo `TestRiscrittureNellImportBloccato` (`internal/importer/regole_dati_test.go`): un
  import con numero e codice da riscrivere e una chiave esterna orfana si blocca e non lascia la
  destinazione né file accanto, dopo che le riscritture sono state fatte (gli avvisi ci sono).
- Commento di `NormalizzaNumero` (`internal/buste/numeri.go`): diceva ancora «L'import storico no:
  conserva il numero della sorgente».
- `docs/SVILUPPO.md`, «Numero di busta»: la nota per chi porta i link Flask nominava `/stato` e
  `/artigiano` ma non le foto storiche `/foto/<slug>/…` e `/thumb/<slug>/…`, che cercano la busta
  con lo slug del numero salvato («0046_26», `get_legacy_public_repair_by_slug/1`) e ne verificano
  la firma sullo slug: aggiunte.

`scripts/check` verde dopo le correzioni; `scripts/parole-vietate --staged` pulito dopo `git add -A`
(poi `git reset`).

**Cosa resta**

- **MEDIA — Dubbio 2 (D-77 col numero occupato):** è una decisione di prodotto, da portare a
  Stefano. Finché non si decide, quelle buste tengono gli zeri e il difetto 1 resta per loro.
- **BASSA — report di un import bloccato:** gli avvisi `buste/…` compaiono anche quando l'import si
  blocca e niente resta (come gli altri avvisi, per esempio quelli senza zona): chi legge il report
  bloccato potrebbe credere che le buste siano state riscritte. Il verdetto «bloccato» lo dice; al
  più una riga nel manuale dell'import.
- **BASSA — `#list-return` dai filtri, non dai parametri grezzi:** lo standalone scrive in
  `data-path` tutti i parametri della richiesta (`list_path(@index_params)`), quindi anche
  `vista=Galleria` o un parametro sconosciuto; il Go solo i filtri letti (scelta della 056,
  scritta nel commento di `ritornoLista`, non come D-xx). Non cambia niente per i link che GestRip
  stesso disegna.
- **BASSA — «0046/26\n»:** `zeriDavanti` accetta un a capo finale (come il `$` di PCRE), quindi
  un numero importato con l'a capo diventa «46/26\n», a capo compreso. Un caso solo teorico, non
  provato contro i dati veri.
- **BASSA — Dubbio 1 (una riga per visita):** coerente con la D-68, va solo confermato.
