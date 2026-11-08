# 058 — D-64 nei PDF e nella pagina della firma, D-73, date nei PDF (T4.2, decima)

- **Fonte:** coordinatore, 08/11/2026. Lo standalone a `go-baseline` = `302439d` (slice 225):
  `receipt.ex` (`render_html/2`: «Busta #{bag_number}», «Accettata» con `escape(repair.accepted_on)`),
  `bag_label.ex` (`page/3`: «##{bag_number}», «Consegna: #{format_date(today)}», `expected_line/1` con
  la data com'è, `qr_value/1`), `repair_sheet.ex` (`<small>N° BUSTA</small>` con il numero,
  `format_date/1` gg/mm/aaaa), `repair_report.ex` (`sheet/1`: «##{bag_number}», `accepted/1`,
  `milestones/1`), `delivery_signature_html/new.html.heex` e il `page_title` del controller («Firma
  busta #N», «✍️ Consegna busta #N»). Le decisioni di Stefano: D-64 (03/11/2026, terza versione),
  risposta 3 alla 053 (06/11/2026: la pagina della firma come la scheda), D-73 (06/11/2026, punto 6
  della 049), «ACCETTATA» resta sulla ricevuta (07/11/2026).
- **Voci:** RIP-S-42 (ricevuta), RIP-S-43 (etichetta 76×127), RIP-S-45 (scheda PDF), RIP-S-46 (report,
  con `ids` e dai filtri), RIP-S-37 (pagina della firma); RIP-S-47/48 (etichette QR 57×32) controllate,
  nessun cambio.
- **Deviazioni:** D-64 estesa ai PDF e alla pagina della firma; **D-73 fatta**; D-71 e D-72
  verificate nei PDF (nessun cambio).
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

### A. Il numero visibile nei PDF (D-64)

- **Dominio** (`internal/buste/visibile.go`): `Mostrato` (`Testo` «0045/26» o «N. 45/26», `Etichetta`)
  e `NumeroMostrato(codiceQR, numero)`, cioè `NumeroVisibile` come valore; `Mostrato.Titolo()` mette
  il «#» solo davanti a un'etichetta («#0045/26», mai «#N. 45/26»).
- **Dati dei documenti:** `DatiRicevuta`, `DatiEtichetta`, `DatiSchedaPDF` e `SchedaReport` hanno il
  campo `Visibile`, calcolato dal `codice_qr` letto nella stessa query (`leggiRicevuta`,
  `leggiEtichetta`, `leggiSchedaPDF`, `leggiReport`). Vale per tutte le strade: «Ricevuta PDF»,
  «🖨 Etichetta busta» e «📤 Scarica etichetta», «🖨 Stampa etichetta» (stampa diretta:
  `EtichettaDaStampare` usa la stessa lettura), «⧉ Anteprima PDF», il report con `ids` e quello dai
  filtri della home (tutti e due da `buste.Report`).
- **Cosa resta sul numero interno:** `Numero` dei dati, quindi i nomi dei file (`ricevuta-45-26.pdf`,
  `etichetta_busta_45_26.pdf`, `busta_45_26.pdf`), il titolo del lavoro di stampa CUPS e il
  `bag_number` di tutte le righe d'audit, come lo standalone (vedi i Dubbi). Il QR dell'etichetta
  76×127 non cambia (`ValoreQR`: il codice salvato com'è, o il numero interno senza codice).
- **Come scrive ciascun PDF** (`internal/stampe/`), confrontato con lo standalone:

  | PDF | standalone | Go |
  |---|---|---|
  | ricevuta (`ricevuta.go`) | «Busta 45/26» | «Busta 0045/26», «Busta N. 45/26» (senza «#», come lo standalone) |
  | etichetta 76×127 (`etichetta.go`) | «#45/26» | «#0045/26», «N. 45/26» (`Titolo()`) |
  | scheda PDF (`scheda.go`) | «N° BUSTA» e «45/26» | «N° BUSTA» e «0045/26», «N. 45/26» (senza «#», a destra, senza a capo) |
  | report (`report.go`) | «#45/26» per scheda | «#0045/26», «N. 45/26» per scheda (`Titolo()`) |
  | etichette QR 57×32 (`etichette_qr.go`) | — (non è dello standalone) | invariato: «0045/26» è già il numero dell'etichetta, dalla 045 |

- **L'etichetta 76×127 con un numero lungo:** «#0045/26» (41,7 mm) e «N. 999/26» (41,9 mm) stanno nei
  46 mm accanto al QR. «N. 1000/26» e oltre (47,3 mm) vanno a capo dopo «N.», come farebbe Chromium
  con lo stesso testo (lo spazio è un punto di a capo); le due righe (15,5 mm) stanno nell'altezza del
  QR (22 mm), quindi il resto dell'etichetta non si sposta. «#12345/26» è una parola sola ed esce di
  poco dai 46 mm, come «#10000/25» dello standalone (Dubbio 2).

### B. La pagina della firma (D-64, risposta 3 alla 053)

- `PaginaFirma.Visibile` (`bustaDellaFirma` legge anche `codice_qr`); `datiFirma.titoloPagina()` dà
  «Firma busta #0045/26» o «Firma busta N. 45/26», e `firma.html` scrive «✍️ Consegna busta
  {{.Intestazione}}» con lo stesso numero. Vale anche quando la pagina si riapre dopo «Firma mancante o
  non valida.» e dopo «↺ Rifai firma» (la stessa rotta GET).
- Il numero interno in piccolo, che la scheda ha accanto al titolo, qui non c'è: la pagina la vede il
  cliente (Dubbio 3).

### C. D-73: «All'artigiano:» sull'etichetta 76×127

- `stampe.EtichettaDataArtigiano = "All'artigiano:"` al posto di «Consegna:»; la data resta quella di
  stampa (oggi in negozio, gg/mm/aaaa), «Scad.:» resta com'è. La voce è a sinistra nella riga `.meta`:
  la sua posizione non cambia, la riga si stringe come prima se insieme a «Scad.:» non ci sta.
- `docs/SVILUPPO.md`: la riga D-73 dice «fatta con la 058».

### D. Le date nei PDF (D-71, D-72)

Verificato PDF per PDF contro lo standalone; **nessun cambio di codice** serviva:

- **Ricevuta:** «ACCETTATA» resta (Stefano, 07/11/2026), con la data AAAA-MM-GG come lo standalone
  (`escape(repair.accepted_on)` stampa `Date.to_string/1`); anche «PREVISTA» con una data è
  AAAA-MM-GG. Test nuovo che lo fissa (`TestRicevutaNumeroVisibile`).
- **Scheda PDF:** «DATA RICEZIONE» (D-71, dalla 055a), gg/mm/aaaa come `Clock.format`.
- **Report:** «DATA RICEZIONE» (D-71, dalla 055a), gg/mm/aaaa; «Pronta:» e «Ritirata:» gg/mm/aaaa.
- **Etichetta 76×127:** non ha la data di ricezione; la data di stampa è gg/mm/aaaa e «Scad.:» con
  una data è AAAA-MM-GG, tutte e due come lo standalone (Dubbio 4).
- **D-72:** nessun PDF dello standalone stampa «Registrata il» (il campo `inserted_at` non compare in
  `receipt.ex`, `bag_label.ex`, `repair_sheet.ex`, `repair_report.ex`), quindi non c'è niente da
  togliere.

### E. Banco

- **Normalizzatori** (`tools/banco/confronto.go`): `D-64` riconosce anche «Busta N/AA» (ricevuta) e
  «N° BUSTA N/AA» (scheda PDF), senza «#» come lo standalone; «#N/AA» di etichetta, report e pagina
  della firma era già riconosciuto. Nuovo `D-73`: «Consegna: gg/mm/aaaa» del testo del PDF dello
  standalone diventa «All'artigiano: gg/mm/aaaa»; «Consegna — Firma del cliente», «Consegna entro» e
  «✍️ Consegna busta» restano.
- **Posizioni dell'etichetta** (`lettura.PosizioniEtichetta`): della riga del numero conta solo la y.
  La riga è centrata accanto al QR, quindi il suo inizio dipende dal testo, che con la D-64 nelle due
  app è diverso («#5/26» contro «N. 5/26»): confrontare la x vorrebbe dire una differenza su ogni
  etichetta. La riga si riconosce come «#…», «N. …» o «N.» (a capo); la data di stampa come
  «Consegna:» o «All'artigiano:». L'inizio della riga del numero, a testo uguale, lo confronta ancora
  `TestEtichettaComeQuellaDiChromium` (ogni riga entro ±0,5 mm da Chromium).
- **Scenari HTTP:** i passi dei PDF di `ricevuta` e `ricevuta-dipendente` hanno `D-64`, quelli di
  `scheda-pdf`, `report`, `report-dipendente`, `report-filtri` ed `etichetta-scheda-dipendente`
  `D-64+D-71`, quelli di `etichetta` (e le due etichette di `etichetta-scheda-dipendente`)
  `D-64+D-73`; le attese dicono «Busta N. 1/26» e «N° BUSTA N. 1/26». Passi nuovi in fondo a
  `ricevuta`, `etichetta` e `scheda-pdf` sulle buste della semina 22/26 (RIP-2026-0045), 25/26
  (RIP-2025-0045), 35/26 (senza QR) e 36/26 (SINT-ALTRO-36), e in fondo a `report` il report
  `ids=22,35,25,36` con la sua riga D-01 (4 copie della sezione delle foto). `report-filtri` provava
  già le buste con l'etichetta (`cerca=0045`).
- **Scenario `firma`:** i cinque passi della pagina hanno `D-64` e confrontano «Firma busta N. 15/26 ·
  Laboratorio», «✍️ Consegna busta N. 15/26» e «✍️ Consegna busta N. 3/26».
- **Browser `firma-riquadro`:** `pagina.titolo` non è più un'attesa comune: è una riga D-64 di
  `bench/deviazioni.json` per vista («Firma busta #2/26 · Laboratorio» contro «Firma busta N. 2/26 ·
  Laboratorio», e la 6/26 sul telefono); `TestScenariBrowserDelRepo` le conta.

### F. Documenti

- `docs/SVILUPPO.md`: «Numero visibile» (i PDF e la firma, dove va il «#»), il paragrafo nuovo «Numero
  visibile nei PDF e nella firma», le date nei PDF verificate, D-73 fatta, le posizioni dell'etichetta
  e il paragrafo del banco «PDF e firma con la D-64 e la D-73».
- Manuale: capitolo 04 (il numero su tutti i PDF, ricevuta, scheda PDF, etichetta con «#0045/26» e
  «All'artigiano:», report), capitolo 05 (il titolo della pagina di firma) con la schermata
  `05-firma-pagina-telefono.png` rifatta dal banco; `da-scrivere.md` (la prima riga del numero è vera
  anche per PDF, etichetta della busta e firma).

## Deviazioni

- **D-64** (approvata), estesa ai PDF e alla pagina della firma come sopra. Nel banco: normalizzatore
  sui passi dei PDF e della firma, due righe del livello browser (`firma-riquadro`, `pagina.titolo`).
- **D-73** (approvata, Stefano 06/11/2026): fatta. Nel banco: normalizzatore `D-73` sulle etichette.
- **D-71, D-72:** nessuna estensione; verificate nei PDF.
- **D-01:** una riga in più (report `ids=22,35,25,36`).

## Prove

- **Test Go nuovi:**
  - `internal/buste/documenti_numero_test.go`: `TestNumeroVisibileDeiDocumenti` (sette codici:
    RIP-2026-0045, RIP-2025-0045, « rip-2026- 0046 », NULL, "", SINT-ALTRO-36, RIP-2026-45; per
    ricevuta, etichetta, etichetta da stampare, scheda PDF, report e pagina della firma: il numero
    visibile, il numero interno in `Numero`, il QR com'è; il registro con il numero interno),
    `TestTitoloDelNumeroMostrato`.
  - `internal/stampe`: `TestRicevutaNumeroVisibile` (sette casi, niente «#», «ACCETTATA» con la data
    AAAA-MM-GG), `TestEtichettaNumeroVisibileEArtigiano` (sei casi con QR, centratura, posizioni;
    «N. 1000/26» e «N. 10000/25» su due righe senza spostare la data; «All'artigiano:»),
    `TestSchedaNumeroVisibile` (cinque casi, a destra senza a capo, «DATA RICEZIONE»),
    `TestReportNumeroVisibile` (quattro schede in ordine).
  - `internal/web/documenti_numero_test.go`: `TestNumeroVisibileNeiPDF` (ricevuta, etichetta, scheda
    PDF e report scaricati per tre codici: testo, QR, nomi dei file, righe d'audit col numero
    interno), `TestPaginaFirmaNumeroVisibile` (quattro codici, anche dopo «Firma mancante o non
    valida.»).
  - `internal/pdf/lettura`: `TestPosizioniDelleFormeDelGo`. `tools/banco`: casi nuovi di
    `normalizzaD64` (PDF e firma), `TestNormalizzaD73`.
- **Test cambiati:** i test dei PDF di `internal/stampe` (dati con `Visibile`; quelli «come quella di
  Chromium» applicano la D-64 e la D-73 al testo di Chromium, e l'etichetta misura l'impaginazione
  con lo stesso testo dello standalone), `TestDatiDellEtichetta`, `TestDatiDellaSchedaPDF`,
  `TestReportCampiDellaScheda`, i test web dei PDF scaricati e della pagina di firma
  (`numeriDelReport` legge «#…» e «N. …»), le posizioni di `lettura` e del banco (numero solo con
  la y), `TestScenariBrowserDelRepo`.

## Mutazioni

`to-be-deleted-after-use/slice-058/mutazioni/muta.py`, 25 punti, test mirati in una copia del repo
nello scratchpad (cancellata dopo): **25 uccise, 0 sopravvissute**. Ricevuta, etichetta, scheda PDF e
report col numero interno; ricevuta e scheda PDF col «#»; `Titolo()` sempre o mai col «#»; D-73 non
fatta; titolo e intestazione della firma col numero interno; le cinque letture senza il codice;
registro col numero visibile; QR dell'etichetta col numero visibile; posizioni senza «N. », senza
«All'artigiano:» e con la x del numero; normalizzatore D-64 senza «Busta » o senza «N° BUSTA »;
D-73 del banco spenta o su ogni «Consegna».

## Banco

`gestrip-standalone:fab-225`, binari dell'albero di lavoro.

- **Primo giro, dati importati** (`s058`): HTTP 48 scenari, 886 passi, 1237 attese, **8 inattese**,
  tutte dei passi nuovi di `ricevuta` e `scheda-pdf` (22/26, 25/26, 35/26, 36/26): l'attesa comune
  «(non un PDF)» valeva anche per i dati importati, perché quei passi non avevano ancora l'attesa del
  testo. Il testo era uguale nelle due app (standalone normalizzato = Go): scritto come attesa. Il
  livello browser non è partito (HTTP rosso).
- **Secondo giro, dati importati** (`s058`, da capo): HTTP **48 scenari, 886 passi, 1237 attese, 0
  inattese**, 0 errori; browser **12 scenari, 24 viste, 30 attese, 0 inattese**, 0 da decidere, 0
  errori: **verde**. Le etichette delle buste con l'etichetta: «#0045/26 All'artigiano: …» e
  «#0045/25 …» nel Go, «#22/26 Consegna: …» e «#25/26 …» nello standalone (D-64+D-73, normalizzate).
- **Database vuoto** (`s058v`): HTTP 48 scenari, 886 passi, 158 attese, **0 inattese** (30 non
  provati con questi dati); browser 12 scenari, **0 inattese** (11 non provati): **verde**.
- **Schermata del manuale** (`m058`, istanza nuova, elenco ridotto alla sola
  `05-firma-pagina-telefono.png`, uscita nella cartella temporanea e poi copiata): «✍️ Consegna busta
  N. 6/26».
- `banco giu` dopo ogni giro: «AVANZI: nessuno».

## Controlli

- `scripts/check` verde sul codice finale (gofmt, vet, staticcheck, errcheck, build, test,
  copertura: 353 voci, 26 chiuse, 0 rosse, 88 parziali, 228 aperte, 11 fuori v1; segreti e dati veri
  OK).
- `scripts/parole-vietate --staged` dopo `git add -A`: pulito; poi `git reset`.

## Per il manuale

- Su ricevuta, scheda PDF, etichetta della busta e report il numero della busta è quello
  dell'etichetta QR («0045/26», «0045/25» per un'etichetta dell'anno prima); senza etichetta «N.
  45/26». Il nome del file scaricato resta quello del numero interno.
- La ricevuta dice «Busta 0045/26»; la scheda PDF «N° BUSTA» e «0045/26»; l'etichetta della busta e il
  report «#0045/26».
- Sull'etichetta della busta la data di stampa si chiama «All'artigiano:» (era «Consegna:»).
- Un numero interno da 1000 in su, su un'etichetta della busta senza etichetta QR, va su due righe.
- La pagina di firma dice «✍️ Consegna busta #0045/26» (o «N. 45/26»).

Scritto nei capitoli 04 e 05 (e la schermata della pagina di firma rifatta); in `da-scrivere.md` la
prima riga del numero dice che PDF e firma sono fatti, restano messaggi e pagina di stato.

## Non portato

- **Messaggi e pagina di stato per il cliente** (WhatsApp, Telegram, email, `/stato`, `/r/`): sono
  delle loro tappe (T4.5–T4.7), come dice la D-64.
- **Il foglio delle etichette di una consegna all'artigiano** (T4.8): userà `PaginaEtichetta`, quindi
  avrà già il numero visibile e «All'artigiano:», ma i dati del foglio li farà quella tappa.
- **Capitolo 03 del manuale:** gli esempi della striscia delle foto («📷 Busta 21/26: 0/2 foto»)
  hanno ancora il numero interno, mentre dalla 053 la striscia mostra il numero visibile. Non è un PDF
  né la firma: lo sistema la 059 con il capitolo 06, o una slice del manuale.

## Dubbi

1. **Nomi dei file e titolo del lavoro di stampa** restano sul numero interno (`ricevuta-45-26.pdf`,
   `etichetta_busta_45_26.pdf`), come lo standalone: sono dati tecnici e il nome del file è unico per
   busta (due buste gemelle avrebbero lo stesso «0045/26»). Il cliente che riceve la ricevuta come
   file vede però «ricevuta-45-26.pdf» con dentro «Busta 0045/26». **Domanda:** va bene così?
2. **Etichetta 76×127 con un numero lungo:** «N. 1000/26» e oltre (buste senza etichetta da 1000 in
   su) vanno a capo dopo «N.», su due righe nell'altezza del QR; «#12345/26» esce di poco dal suo
   spazio. Ho fatto come farebbe Chromium con lo stesso testo, senza rimpicciolire il carattere.
3. **Pagina della firma senza il numero interno in piccolo:** la scheda mostra «N. 45/26» in piccolo
   accanto a «Busta #0045/26»; la pagina della firma no, perché la guarda il cliente e la risposta 3
   diceva «titolo e intestazione». Se lo vuole anche lì, è una riga.
4. **Formati delle date come lo standalone:** la ricevuta per il cliente ha «ACCETTATA 2026-03-02» e
   «PREVISTA 2026-04-15» (AAAA-MM-GG), e l'etichetta 76×127 «Scad.: 2026-04-15», mentre scheda PDF,
   report e data di stampa dell'etichetta sono gg/mm/aaaa. Non ho cambiato niente (niente formati
   nuovi senza decisione). **Domanda:** passare ricevuta e «Scad.:» a gg/mm/aaaa (sarebbe una D-xx
   nuova)?
5. **Posizioni dell'etichetta nel banco:** della riga del numero ora conta solo la y (sopra, «E»). La
   centratura del numero la controllano i test di `internal/stampe`, non più il confronto con lo
   standalone.

## Review

Review di un altro sottoagente, 08/11/2026, su pc-fisso (`~/sviluppo/gestrip-go`, albero non
committato), contro lo standalone a `go-baseline` (`receipt.ex`, `bag_label.ex`, `repair_sheet.ex`,
`repair_report.ex`, `delivery_signature_controller.ex` e `new.html.heex`).

**Guardato:**
- **Un solo `codice_qr` per documento:** le cinque letture (`leggiRicevuta`, `leggiEtichetta`, che
  serve anche `EtichettaDaStampare`, `leggiSchedaPDF`, `leggiReport`, `bustaDellaFirma`) leggono
  `codice_qr` nella stessa `SELECT` del numero e calcolano `Visibile` subito con `NumeroMostrato`;
  nell'etichetta `QR` e `Visibile` vengono dallo stesso valore letto. Nessuna lettura separata.
  Codice vuoto, NULL, di altro formato e «RIP-2026-45» danno «N. N/AA» (test della slice). La regola
  non guarda `compro_oro`: mancava una prova, aggiunta (sotto).
- **Cosa non cambia:** QR dell'etichetta (`ValoreQR` com'era), nomi dei file (`NomeFile*` su
  `Numero`), titolo del lavoro CUPS (`stampanti.go`, `NomeFileEtichetta(e.Numero)`) e `bag_number`
  di tutte le righe d'audit restano sul numero interno; i test web e di `buste` lo controllano.
- **Pagina della firma:** mostra solo titolo, intestazione, nome del cliente e descrizione (come lo
  standalone); il template non usa più `.Numero`. Con un'etichetta il numero interno non compare.
- **Confronto con lo standalone:** «Busta …» della ricevuta e «N° BUSTA» della scheda senza «#»,
  «#…» su etichetta, report e firma, come lo standalone con il numero visibile al posto di quello
  interno (D-64); «All'artigiano:» (D-73). Non ho trovato differenze non dichiarate.
- **Banco:** `normalizzaD64` mette il numero di `numeriVisibiliBanco` al posto di quello interno solo
  dopo i prefissi elencati, e solo nei passi con `normalizza`: non può far passare un Go sbagliato, al
  massimo rende rossa una pagina dove la D-64 non vale. `normalizzaD73` tocca solo «Consegna:
  gg/mm/aaaa». Le due righe browser di `deviazioni.json` sono esatte (valori interi, per vista).
- **`PosizioniEtichetta`:** la y del numero, il QR, «Consegna:»/«All'artigiano:» e FATTO restano
  confrontati entro ±0,5 mm; si perde solo la x del numero fra le due app, inevitabile con testi
  diversi. La centratura resta provata da `TestEtichettaNumeroVisibileEArtigiano` (ogni forma, entro
  0,05 mm) e, a testo uguale, da `TestEtichettaComeQuellaDiChromium` (x e y di ogni riga).
- **Test:** quelli nuovi provano quello che dicono (letture vere dal database, PDF letti di nuovo,
  QR decodificato, righe d'audit contate).

**Corretto (con un test ciascuno):**
- `internal/buste/documenti_numero_test.go`: `TestNumeroVisibileCompraOro`, una scheda Compro Oro
  («perfezionata», RIP-2026-0071) ha «0071/26» in pagina della firma, scheda PDF, ricevuta ed
  etichetta (QR invariato), «N. N/AA» senza codice. Il codice andava già bene: mancava la prova.
- `internal/web/documenti_numero_test.go`, `TestPaginaFirmaNumeroVisibile`: con un'etichetta il
  numero interno («1/26») non deve comparire da nessuna parte nella pagina del cliente.

**Resta:**
- **BASSA:** nel banco la centratura del numero dell'etichetta non si confronta più con lo
  standalone (solo la y); coperta dai test di `internal/stampe` (Dubbio 5). Se servisse, si
  potrebbe confrontare il centro della riga, ma `lettura.Riga` non ha la larghezza.
- **BASSA:** in `PosizioniEtichetta` una riga che comincia con «N. » prima del numero (per esempio
  un artigiano «N. Rossi», che però è scritto in maiuscolo dopo «→») verrebbe presa per il numero;
  vale lo stesso di prima per «#».
- **Decisioni di Stefano, non difetti:** Dubbi 1 (nomi dei file), 2 (numeri lunghi), 3 (niente
  numero interno sulla firma), 4 (formati delle date di ricevuta e «Scad.:»).

`scripts/check` verde dopo le correzioni; `scripts/parole-vietate --staged` pulito dopo
`git add -A`, poi `git reset`.
