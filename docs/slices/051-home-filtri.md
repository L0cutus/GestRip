# 051 — Filtri, pillole, ricerca testuale e report dai filtri della home (T4.2, seconda)

- **Fonte:** riga 051 del piano della T4.2 (`050-home-lista.md`); lo standalone a `go-baseline`:
  `lib/gestrip_standalone/workshop/repair_index.ex` (`normalize/1`, `apply_filters/3`, `search/2`,
  `escape_like/1`, `explicit_filter?/1`, `date_range/4`, `selection/2`), `Repair`
  (`normalize_bag_number/1`, `unread_notifications_query/1`, `with_messages_query/1`, `statuses/0`),
  `Workshop.list_external_oreficerie/0`, `list_artisans/0`, `record_repair_index_view/2`,
  `WorkshopLive` (pillole e «× Azzera filtro», «⧉ Schede PDF», `#repair-search-form`, il cassetto
  `#repair-filter-drawer` con i campi nascosti della slice 214, `filter_query/3`, `search_hidden/1`,
  `date_filter_label/1`, `oreficeria_filter_label/2`, `artisan_filter_label/2`, `status_label/1`),
  `RepairReportController` (`from_filters/1`, `too_many/2`) e i test `repair_index_test.exs`,
  `workshop_live_test.exs`, `workshop_filter_drawer_test.exs`, `repair_report_controller_test.exs`.
- **Voci:** RIP-H-03 (la ricerca com'è oggi nello standalone: il campo unico D-63 e la ricerca per
  etichetta D-64 sono della 052), RIP-H-04, RIP-H-05, RIP-H-06 (con `notifiche`), RIP-H-07, RIP-H-19,
  RIP-H-56 (pillola e casella del cassetto), RIP-S-46 (variante dai filtri).
- **Deviazioni:** D-01 (sezione foto del report, righe nuove per `report-filtri`), D-64 (numero
  visibile in lista, normalizzatore). Nessuna deviazione nuova.
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Cosa c'è

- **Dominio** (`internal/buste/indice.go`):
  - `Filtri` ha tutti i filtri di `normalize/1`: `Cerca`, `Stato`, `Oreficeria` (Id.positive/1 e
    oltre 1: il negozio non si filtra), `Artigiano` (Id.positive/1), le sette caselle compresa
    `Notifiche`, `DataDa`/`DataA` e `DataCampo` (accettazione, pronta, consegna, consegnata; ogni altro
    valore è l'accettazione). `Attivo` è `explicit_filter?/1` intero: la pagina e `data_campo` da soli
    non sono filtri.
  - `DataISO` è `Date.from_iso8601/1` + `Date.to_iso8601/1` misurati in fab-222: segno facoltativo,
    quattro cifre ASCII per l'anno, `-MM-GG`; anni negativi e zero, bisestili proleptici; «+2026-…»
    diventa «2026-…»; forma corta, settimane, giorni dell'anno e cifre non ASCII rifiutate. Prima
    `String.trim/1` (gli stessi spazi Unicode di `unicode.IsSpace`: misurati NEL, NBSP, U+2007,
    U+2028, U+3000 sì; U+200B e U+180E no).
  - La ricerca è `search/2`: `LIKE … ESCAPE '\'` su nome e telefono copiati sulla busta, numero,
    descrizione, note, note interne e codice QR; il modello è `ModelloRicerca`: prima
    `NormalizzaNumero` («012/26» trova «12/26»), poi l'escape di `\`, `%` e `_`.
  - Le notifiche sono `unread_notifications_query/1` (messaggio, accettazione o rifiuto non letti); il
    periodo è `date_range/4` con gli estremi inclusi sulla colonna scelta (una consegna a testo non ha
    data e non entra mai).
  - `Selezione` è `selection/2`: il conteggio intero della selezione e gli id delle prime N
    nell'ordine della lista, senza pagina, con il nascondimento di pronte e consegnate come la lista.
  - `TuttiGliStati` (`stati.go`) è `Repair.statuses/0` per il menu dello stato.
- **Report dai filtri** (`internal/buste/report.go`, `internal/web/report.go`): senza `ids` (assente,
  vuoto, di sole virgole o spazi, «ids» senza «=») il report è la selezione della home
  (`IdsDaiFiltri`, i parametri letti come la home con `testoPlug`); 422 «Nessuna riparazione da
  stampare: il filtro selezionato non trova nessuna busta.» e 422 «Troppe riparazioni per un solo
  report: N selezionate, il massimo è 50. Restringi la selezione.» con il conteggio intero (si
  caricano al più 51 id). `ids` vince sui filtri. Il 404 di prima è tolto.
- **Pagina** (`internal/web/home.go`, `pagine/home.html`, `app.css`):
  - pillole in `#active-repair-filters`, solo con un filtro esplicito, nell'ordine e con i testi dello
    standalone, poi `#clear-repair-filters` «× Azzera filtro» verso `/`;
  - `#repair-report-pdf` «⧉ Schede PDF» con `target="_blank"` e i filtri di `filter_query/3` senza la
    pagina (`/repairs/report.pdf` senza filtri);
  - `#repair-search-form`, modulo GET con gli altri filtri come campi nascosti (`search_hidden/1`);
  - il cassetto `#repair-filter-drawer` / `#repair-filter-form`, modulo GET: `attesa`, `scadenza` e
    `notifiche` nascosti se attivi (slice 214), poi cerca, stato (tutti i dieci stati), oreficeria e
    artigiano solo se ce ne sono, il campo del periodo, le due date, le quattro caselle (compro oro,
    fuori bottega, senza QR, messaggi), «Applica» e «Reset»;
  - paginazione, ricerca e report con tutti i filtri nell'ordine di `filter_query/3` (cerca, stato,
    oreficeria, artigiano, le caselle, notifiche, date, `data_campo` solo con una data, poi `p`),
    codificati come `Plug.Conn.Query.encode/1` (misurato);
  - tutto senza JavaScript.
- **Registro:** `repair.index_viewed` con le chiavi di `record_repair_index_view/2` (cerca, stato,
  oreficeria, artigiano, scadenza, data_da, data_a, data_campo, p): già così dalla 050, ora le chiavi
  corrispondono a filtri veri.
- **Banco** (generatore `to-be-deleted-after-use/slice-051/scenario/genera.py`, attese misurate sulle
  due app):
  - `home-filtri` (59 passi): ogni parametro, valori non validi (`oreficeria=1`, `+2`, ` 2`, `abc`,
    `artigiano=0`, `1x`, date non ISO, 30 febbraio, forma corta, segno, anno negativo e zero,
    `data_campo` sconosciuto o da solo), `10%`, `a_b`, `%`, `_`, `\`, «012/26», « 012/26 » con gli spazi, «00012/26»,
    `0045`, telefono, QR, testo da scappare, `cerca[]=x`, combinazioni, tutti i filtri insieme,
    paginazione coi filtri. Ogni passo confronta pillole, link del report, i due moduli campo per campo
    (tipo, nome, valore, `checked`, `selected`), la paginazione e 12 (o 30) riquadri, con il
    normalizzatore D-64. Gira prima di `home.json` (`TestOrdineScenariHome` aggiornato).
  - `report-filtri` (18 passi): accesso, PDF dai filtri (nessun filtro, stato, pagina ignorata, `ids=`
    vuoto, `ids` senza «=», «012/26», periodo, oreficeria, stato chiuso, `ids` che vince), tre 422
    «Nessuna riparazione…», `ids[]=` che resta «Selezione non valida». Righe D-01 nuove in
    `bench/deviazioni.json` (una per passo con PDF, con le copie della sezione foto).
  - `provacon_test.go` e `dati_test.go` sanno dei due scenari nuovi.
  - **Scenario toccato: `registro`.** `home-filtri` gira prima e fa 57 visite della home: lo standalone
    scrive più di 100 righe `repair.index_viewed` (due per visita, D-68 proposta); il Go ne scrive una
    per visita e supera le 100 con le righe degli scenari precedenti. Così anche sul database vuoto il
    registro intero ha «Successiva» a pagina 1 e «Precedente» sull'ultima, ma solo nel giro intero:
    `registro` lanciato da solo sul database vuoto non regge. Le due
    attese passano da `atteso_dati` ad `atteso`; `TestScenarioRegistroPaginazione` lo sa (i filtri per
    azione restano senza pagine sul database vuoto).
  - Tutte le attese dei due scenari nuovi stanno in `atteso_dati` dei dati importati: sul database vuoto
    gli scenari girano (non provati) e si confrontano solo le due app fra loro.

## Prove

- **Test Go** (orologio finto al 31/10/2026, database vero):
  - `internal/buste/indice_test.go`: `TestLeggiFiltriDelCassetto` (oreficeria, artigiano, data_campo,
    31 date), `TestModelloRicerca`, `TestListaFiltriDelCassetto` (ricerca nelle sette colonne, `%`,
    `_`, `\`, maiuscole, «012/26», oreficeria, artigiano, notifiche, periodo sulle quattro date,
    combinazioni, `Selezione`), `TestLeggiFiltri` aggiornato.
  - `internal/web/home_test.go`: `TestHomePillole`, `TestHomeRicercaECassetto`,
    `TestHomeFiltriDelCassetto`, `TestIndirizzoLista` con tutti i filtri.
  - `internal/web/report_test.go`: `TestReportDaiFiltriDellaHome` **senza `t.Skip`** (ordine, pagina
    ignorata, `ids` che vince, righe del registro nell'ordine, pronte nascoste, quattro 422 vuoti, 31
    buste, 50 stampate, 52 e 51 rifiutate col conteggio intero); `TestReportSenzaIdsESenzaAudit` ora
    vuole il PDF della selezione e il 503 anche dai filtri.
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-051/mutazioni/muta.py`, 20 punti, test mirati):
  **19 uccise, 1 sopravvissuta equivalente**. Uccise: escape del LIKE tolto; `ESCAPE` tolto su una
  colonna; `NormalizzaNumero` tolto; periodo «da» e «a» esclusivi; `cerca` e le date fuori da
  `Attivo` (le pronte nascoste non tornano); oreficeria 1 accettata; notifiche senza il rifiuto;
  pillola della scadenza senza «(7 giorni)»; artigiano senza nome; report con la pagina; campi
  nascosti della ricerca con `cerca`; `data_campo` sempre; 422 vuoto come 404; «Troppe» col numero
  delle caricate (uccisa dopo aver aggiunto il caso a 52 buste); selezione vuota non controllata;
  selezione senza l'ordine della lista; cassetto senza `scadenza` nascosta. Sopravvissuta:
  «pillole calcolate anche senza filtri», equivalente (il modello le mostra solo con `.Attivo`).
- `scripts/check` verde (copertura: 353 voci, 26 chiuse, 0 rosse, 76 parziali, 240 aperte, 11 fuori
  v1; segreti e dati veri OK).

## Banco

Istanza `s051`, `gestrip-standalone:fab-222`.

- Esplorazione (i due scenari da soli, dati importati): `home-filtri` 0 differenze inattese, 720
  attese (tutte D-64 normalizzate); `report-filtri` solo la sezione foto (D-01) e un passo che avevo
  pensato vuoto (`stato=chiusa`: c'è una busta chiusa nella cattura).
- **Giro completo, dati importati** (istanza nuova, tre volte): HTTP **43 scenari, 753 passi, 1043
  differenze attese** (D-64 normalizzate, D-01, D-56), **0 inattese**, 0 errori. Browser: 8 scenari,
  16 viste, 22 attese, **2 inattese**, sempre le stesse: in `firma-riquadro[1]` lo **standalone**
  risponde 500 a due foto della 6/26 (`/repairs/6/photos/…`), con «Exqlite.Error database is locked»
  nel suo log; il Go nessun errore. È il guasto noto della 040a e della 050 (lì una volta su due),
  e **non dipende da questa slice**: un giro con il solo scenario `healthz` prima del browser (istanza
  nuova) dà lo stesso 500 dello standalone.
- **Giro completo, database vuoto** (istanza nuova): HTTP 43 scenari, 753 passi, 17 attese, **0
  inattese**, 0 errori (25 non provati con questi dati, come previsto); browser 8 scenari, **0
  inattese**: verde.
- `banco giu` e `banco controlla` dopo ogni giro: nessun avanzo.

## Review indipendente

Nessun difetto alto o medio: filtri, ricerca, pillole, link, moduli e report riscontrati punto per
punto con lo standalone. Le note basse:
- corretto: in `home-filtri` la paginazione era controllata con `a@2`, sempre assente (con due pagine
  c'è un solo link); ora i passi 2, 54, 55 e 56 fissano `repair-pagination/a:href`;
- corretti il commento di `TestScenarioRegistroPaginazione` e il paragrafo sul `registro` qui sopra
  (il lato Go), e la frase sugli spazi di «012/26»;
- lasciato così: il passo con tutti i filtri dà una lista vuota (l'AND con risultati è provato a
  coppie); nel report dai filtri gli id e le buste si leggono in due volte, quindi una busta cestinata
  proprio in mezzo dà 404 invece di sparire dal PDF (caso rarissimo);
- da mettere in conto: nella 052 «0012» senza l'anno dovrà trovare la 12/26 (oggi la normalizzazione
  vale solo sul numero intero); nella 057 `vista` va aggiunta ai link, al cassetto e al report;
  «⧉ Schede PDF» senza filtri, in negozio, supera quasi sempre le 50 buste e dà il messaggio.

## Deviazioni

- Nessuna nuova. D-01 copre la sezione foto anche nel report dai filtri; D-64 il numero visibile dei
  riquadri (normalizzatore). Nel report il numero resta quello interno «#12/26» come nello
  standalone (D-64 nei PDF è la 058).

## Per il manuale

- In alto nella lista, «🔍 Filtri e ricerca» apre il cassetto: stato, oreficeria, artigiano, periodo
  su una data a scelta (accettazione, pronta, consegna prevista, consegnata) e quattro caselle.
- «Applica» mostra la lista filtrata; «Reset» o «× Azzera filtro» tornano alla lista completa.
- Sopra la lista ogni filtro attivo ha la sua pillola colorata (per esempio «🔍 anello» o
  «📅 Accettazione: 2026-01-01 → …»).
- La casella «Cerca per nome o n° busta…» cerca in cliente, numero, descrizione, note, telefono e QR;
  «012/26» trova la 12/26; i filtri già scelti restano.
- «⧉ Schede PDF» apre il PDF di tutte le buste della lista filtrata (non solo della pagina), al più
  50: oltre, il PDF non si fa e il messaggio chiede di restringere i filtri.
- Cambiando pagina i filtri restano.

## Dubbi

1. **«⏰ In scadenza (7 giorni)»:** lo standalone scrive sempre «7 giorni» nella pillola, anche con
   un'altra soglia (nel banco la soglia è 12). Copiato com'è. È una scelta di prodotto (probabilmente
   un difetto dello standalone): se Stefano vuole la soglia vera nella pillola, è una riga in
   `pilloleDi` e una D-xx.
2. **«Troppe riparazioni…» dai filtri non è nel banco:** i dati sintetici hanno 40 buste e lo
   standalone crea buste solo da LiveView, quindi nessun passo HTTP arriva a 51. Il testo è già
   provato dal banco con `ids` (scenario `report-rifiuti`) e la variante dai filtri dai test Go (51 e
   52 buste). Per averlo nel banco servirebbero altre 11 buste nella semina, che cambiano i totali
   degli scenari della home.
3. **`vista=galleria`:** lo standalone porta `vista` nei link e come campo nascosto del cassetto; nel
   Go la galleria è della 057, quindi `vista` è ignorata e nessuno scenario la usa.
4. **Il cassetto si apre sempre chiuso**, anche con dei filtri attivi, come lo standalone.
5. **Il livello browser con i dati importati non è verde** per il 500 «database is locked» dello
   standalone sulle foto della 6/26 (sopra): ora 4 giri su 4, anche senza gli scenari di questa
   slice. È un guasto dello standalone (o del banco che lo carica), non del Go; va deciso se
   accettarlo in `firma-riquadro` o indagarlo a parte.
6. Un `data_a` con l'anno negativo («-2026-01-01») si accetta come nello standalone (pillola
   «Accettazione: … → -2026-01-01», lista vuota): copiato, è un caso che nessuno scrive a mano.
