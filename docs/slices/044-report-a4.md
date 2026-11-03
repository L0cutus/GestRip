# 044 — Report multi-scheda A4 (T4.4, terza)

- **Fonte:** T4.4 del PIANO (piano nella ricevuta della 042); §2.1 e D-24 (cosa vuol dire «uguale»);
  D-01 (la sezione delle foto del report nel Go non c'è, Stefano 29/10/2026); D-56 (un id di busta
  oltre i 64 bit nelle tre rotte dei PDF con `{id}` è una busta che non c'è, Stefano 03/11/2026,
  domanda 21: aggiunta alla slice dal coordinatore).
- **Voci:** RIP-S-46 (report A4 `GET /repairs/report.pdf`), **parziale**: la variante con `ids=` è
  fatta; quella senza `ids` (i filtri della home) va con la T4.2. RIP-H-19 (il link «⧉ Schede PDF»
  della home) non è in questa slice: arriva con la T4.2.
- **Copertura:** RIP-S-46 resta **parziale** per `TestReportDaiFiltriDellaHome`, che documenta la
  variante senza `ids` e si salta («variante con i filtri della home: T4.2 (RIP-H-19)»): un test
  saltato conta come parziale anche con l'esito del banco.
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale con i binari ricompilati.
- **Nello stesso commit:** le risposte di Stefano del 03/11/2026 alle domande 18-24 (D-52…D-59
  nell'inventario, `docs/domande-aperte.md` ridotta alla 6), la nota delle emoji fra le idee future
  e lo spostamento esplicito di RIP-H-19 e della variante dai filtri nella T4.2 del PIANO. Con D-54
  approvata RIP-S-29 è chiusa.

## Cosa c'è

- **Dominio** (`internal/buste/report.go`): `IdsDelReport` legge il testo di `ids` come `named_ids/1`
  (voci divise sulle virgole, `String.trim/1` con gli spazi Unicode, voci vuote tolte, `Id.positive/1`
  con segno e zeri davanti ammessi fino al massimo dei 64 bit, doppioni tolti tenendo il primo; la
  prima voce sbagliata ferma tutto) e il limite di 50 prima di cercare le buste; nessuna voce =
  `ErrSelezioneDaiFiltri`. `Buste.Report` legge le buste in una query (cestino filtrato, oreficeria,
  artigiano e profilo del negozio nella stessa lettura), dà `SchedeNonTrovate` con gli id mancanti
  nell'ordine chiesto, fa il PDF e poi una riga `repair.report_generated` per busta, nell'ordine, con
  `bag_number`. La prima riga che non si scrive ferma il documento (503) e le righe già scritte restano,
  come `audit/2` (`reduce_while`) dello standalone. I campi già pronti come `sheet/1`: «—» per cliente,
  telefono, data, consegna e lavorazione vuoti; stato con gli spazi e classe con i trattini; importi
  «€ testo» (il testo com'è, non `Money.label/1`) o «€ 12,50», uniti da « → »; note solo se non vuote;
  pronta e ritiro gg/mm/aaaa.
- **Documento** (`internal/stampe/report.go`): A4 con i margini 12/10/15/10 mm, l'intestazione solo
  sulla prima pagina (negozio, «Schede riparazioni», «Generato il …», «1 scheda»/«N schede»), una
  pagina per busta (`#numero`, cliente, badge dello stato con i colori della sua classe, griglia a due
  colonne con i campi pieni su una riga loro, date di pronta e ritiro), **senza la sezione delle foto**
  (D-01) e senza «Avvisata:» (come lo standalone); il piè di ogni pagina «Generato da <negozio> — uso
  interno» e «Pagina N / M» alla linea di base misurata in Chromium. Una scheda più lunga di una pagina
  continua sulla pagina dopo. Le note interne sono #555 ma non in corsivo (non c'è il corsivo fra i
  font incorporati: resa grafica, fuori da D-24).
- **Rotta** (`internal/web/report.go`, `GET /repairs/report.pdf`, `personale`, nella tabella dei
  permessi): `ids` letto come `Plug.Conn.Query.decode/1` (`idsDellaQuery`); un valore che non è un
  testo è 422 con `inspect/1` del valore (`valorePlug.inspect`); il PDF in linea
  `inline; filename="schede_<N>.pdf"`; i rifiuti come `refuse/3` (text/plain; charset=utf-8,
  private, no-store); senza `ids` il 404 della faccia (variante rimandata, sotto).
- **Banco:** scenari `report` (22 passi, dati importati), `report-rifiuti` (38, tutti i dati) e
  `report-dipendente` (5, dati importati); in `bench/deviazioni.json` 13 righe D-01 (più le 6 D-56 delle altre rotte) con il campo nuovo
  `togli` e `copie` (`tools/banco/confronto.go`): la differenza di `pdf:testo` è coperta solo se il
  testo dello standalone ha «📷 FOTO Nessuna foto allegata.» esattamente una volta per scheda del passo
  e, senza quelle copie, è esattamente quello del Go.
- **Fixture:** `internal/pdf/lettura/testdata/chromium-report-3-5-6-2.pdf` (PDF dello standalone sui
  dati sintetici del banco, 4 pagine); `TestReportComeQuelloDiChromium` confronta formato e testo
  normalizzato, tolta la sezione delle foto.

## D-56: id oltre i 64 bit nelle rotte dei PDF con `{id}`

- `/repairs/{id}/receipt.pdf`, `label.pdf` e `scheda.pdf`: oltre i 64 bit (anche negativo) il Go
  risponde il 404 «Not Found» di una busta che non c'è (stesso corpo, tipo e intestazioni), senza PDF
  né audit; lo standalone risponde 500. `idDellaRicevuta` dà `ErrBustaNonTrovata`; `ErrIdFuoriScala`
  è tolto. Test prima rossi (`prove/rosso-d56-pdf.log`).
- **Il report non c'entra:** con `ids=` lo standalone non dà 500 ma 422 «Selezione non valida:
  «99999999999999999999» non è un numero di riparazione.» (`Id.positive/1`, misurato), e lì nessuna
  deviazione è approvata: il Go copia il 422, anche per il negativo e per «+0099999999999999999999»
  (la voce com'è, senza spazi). Una prima versione che dava 404 anche nel report (dal brief) è stata
  tolta: gli id del report sono di nuovo interi (`[]int64`), non servono testi.
- Banco: nei passi `ricevuta[19]`, `etichetta[30]` e `scheda-pdf[22]` tolte le attese su codice e corpo
  (valgono per tutte e due le app) e aggiunte 6 righe D-56 in `bench/deviazioni.json` (codice e corpo,
  coppie esatte). I passi oltre i 64 bit di `report-rifiuti` hanno le attese del 422.
- **Nota per il futuro (limite del banco):** senza attesa, un Go che tornasse al 500 sarebbe uguale
  allo standalone e il banco resterebbe verde, perché una deviazione non usata non si segnala. Oggi lo
  fermano i test (`TestRicevutaIdComeEcto`, `TestDocumentiIdComeEcto`, `TestRicevutaIdComeLoStandalone`,
  `TestDocumentiPDFRifiuti`). Raccomandazione: far segnalare al banco le deviazioni non usate.

## Misurato sullo standalone (fab-220, 03/11/2026)

PDF e risposte scaricati con un programmino usa e getta in un container sulla rete del banco (dati
importati), letti con `internal/pdf/lettura`; `Plug.Conn.Query.decode/1` e `inspect/1` misurati con
`erlexec` della release nell'immagine, senza rete e senza la configurazione dell'app.

- PDF: `application/pdf`, `private, no-store`, `inline; filename="schede_3.pdf"`; pagine
  209,89×297,01 mm, una per busta; testo nell'ordine: intestazione, poi per ogni pagina la scheda, «📷
  FOTO Nessuna foto allegata.» e il piè «Generato da Negozio — uso interno Pagina N / M» (linea di base
  a 291,31 mm). Nessun «Avvisata». Le note con gli a capo collassano (HTML).
- Rifiuti: `text/plain; charset=utf-8`, `private, no-store`. 422 «Selezione non valida: «X» non è un
  numero di riparazione.» con la voce senza spazi (`abc`, `0`, `-1`, `1.0`, `1;2`, `1 2` da `1+2`,
  `"x\`, `è`, oltre i 64 bit); `9223372036854775807` passa ed è 404; 422 «Troppe riparazioni per un
  solo report: 51 selezionate, il massimo è 50. Restringi la selezione.» anche per buste che non ci
  sono; 404 «Riparazione non trovata: 21, 22, …, 50. Nessun report generato.» (id mancanti nell'ordine
  chiesto, separati da «, »).
- Query: vale l'ultima assegnazione (`ids=1&ids=2` → la 2; `ids[]=1&ids=2` → la 2; `ids=1&ids[]=2` →
  422 `["2"]`); `ids[]=1&ids[]=2` → `["1", "2"]`; `ids[a]=1` → `%{"a" => "1"}`; mappe con le chiavi in
  ordine; `ids[=1`, `ids]=1`, `ids[a]b=1` sono altre chiavi; `%FF` → 400 «Bad Request» (della faccia,
  come prima); `ids`, `ids=`, `ids=,,` e nessun `ids` → la selezione della home (200 con tutte le buste
  dei dati).
- `inspect/1`: quali caratteri sono stampabili e quali escono come `\uXXXX` (tabella su tutti i codici
  da 0 a U+10FFFF), `\#{`, `<<…>>` per un testo non stampabile, i limiti di 50 elementi (49 annidati;
  in una mappa con due liste lunghe la prima ne mostra 49, la seconda 48), 50 byte, 4096 caratteri,
  l'ordine delle mappe oltre 32 chiavi (hash, non copiato).
- `/repairs/report.pdf/` → il report; `/repairs/report.PDF` → la scheda della busta (302 a `/`);
  HEAD → 200 senza corpo; senza accesso → 302 a `/login` e, dopo l'accesso, ritorno al report con la
  query.

## Solo ragionato (dal codice, non misurato)

- Il 503 «PDF non disponibile» del report è `text/plain` con `private, no-store` (`refuse/3`), diverso
  dal 503 senza tipo degli altri PDF: dal codice del controller, non provocato sul banco.
- Le righe d'audit già scritte restano se una riga dopo non si scrive (`reduce_while`).
- Plug con forme che nessun modulo produce (`ids[][a]` seguito da `ids[][a][]`, liste di liste): il
  Go mette tutto nell'ultima mappa o lista; non misurato.
- Mappa con più di 32 chiavi in `inspect/1`: lo standalone usa l'ordine dell'hash, il Go le ordina.
- Una scheda più lunga della pagina: Chromium la continua sulla pagina dopo come il Go, ma dove va a
  capo dipende dal font (Liberation Sans in Chromium, DejaVu nel Go, come la scheda della 043).
- Il badge con uno stato vuoto («—»): lo schema del Go non ammette lo stato vuoto.

## Differenze e dubbi

1. **Variante senza `ids`** (i filtri della home): **rimandata alla T4.2**. Nel Go la home non ha né la
   lista né i filtri né l'ordine (`RepairIndex.selection/2`: fissate, messaggi non letti, anno e
   numero della busta, id; filtri nascosti per pronte e consegnate secondo le impostazioni): inventarli
   qui sarebbe una decisione di prodotto. Oggi la rotta senza `ids` (o con `ids` vuoto) risponde il 404
   «Not Found» della faccia, senza PDF né righe; il banco non prova quei casi (sarebbero una differenza
   non approvata). **Raccomandazione:** tenerla così fino alla T4.2, che porta lista, filtri, ordine,
   la variante dei filtri (con il 422 «Nessuna riparazione da stampare: il filtro selezionato non trova
   nessuna busta.») e il link «⧉ Schede PDF» (RIP-H-19) insieme.
2. **Copertura:** risolto con il test saltato `TestReportDaiFiltriDellaHome` (RIP-S-46 parziale).
3. **Corsivo delle note interne:** non c'è (solo DejaVu normale e grassetto). Resa grafica, D-24.
   **Raccomandazione:** lasciare.
4. **Righe d'audit a metà** se una riga non si scrive: il Go copia lo standalone (restano quelle già
   scritte, il documento non esce). **Raccomandazione:** tenere, è la stessa scelta dello standalone.

## Prove

- **TDD:** rosso salvato per dominio, stampe, web, banco e D-56 delle tre rotte con `{id}`
  (`to-be-deleted-after-use/slice-044/prove/rosso-*.log`). Il ritorno al 422 del report oltre i 64 bit
  è il comportamento del primo giro: i test del 422 sono stati rimessi insieme al codice, senza un
  rosso a parte.
- **`scripts/check`:** verde (sottoagente), anche dopo le correzioni della review (`prove/check-review.log`).
- **Banco** (sottoagente, binari ricompilati dal codice finale, istanze nuove): dati importati verde
  (32 scenari, 493 passi, 19 differenze attese: 13 D-01 e 6 D-56, 0 inattese; browser verde con i 2
  «da decidere» che ora sono della D-54); database vuoto verde (6 attese D-56, 17 scenari non provati
  con quei dati); «AVANZI: nessuno». Il giro dopo le correzioni della review (`copie` nelle righe D-01,
  la D-54 come deviazione del browser) lo rifà la sessione principale. Prova negativa (giro precedente) sui dati importati con un Go che riordina
  gli id: 6 differenze inattese (testo dei PDF di `report[3]`, `[5]`, `[13]`, `report-dipendente[2]` e
  il corpo del 404 di `report[19]`), rosso; la D-01 non le copre.
- **Copertura** con l'esito dei dati importati: chiuse 60, parziali 19 (RIP-S-46 parziale per il test
  saltato).

## Review indipendente

Nessuna ALTA.

- **MEDIA, variante senza `ids` e RIP-H-19:** spostate esplicitamente nella T4.2 del PIANO (dal
  coordinatore); RIP-S-46 resta parziale per `TestReportDaiFiltriDellaHome` (saltato).
- **BASSA, D-56 senza test sull'alias oltre i 64 bit:** togliendo `!n.IsInt64()` in `idDellaRicevuta`
  i test restavano verdi, e `/repairs/18446744073709551617/receipt.pdf` (2^64+1) avrebbe aperto la busta
  1. Aggiunto il caso «2^64 + id di una busta che c'è» (anche con `+00` davanti) a `TestRicevutaIdComeEcto`
  e `TestDocumentiIdComeEcto`: con il mutante cadono tutti e due (`prove/rosso-review-alias.log`).
- **BASSA, `togli` troppo largo:** togliendo tutte le copie della frase, una nota della busta uguale a
  «📷 FOTO Nessuna foto allegata.» che il Go non stampasse risultava coperta. Ora la deviazione vuole
  `copie` (una per scheda del passo) e la frase deve esserci esattamente tante volte; `copie` senza
  `togli` o minore di 1 è un errore (`TestDeviazioneConTogli`, rosso in `prove/rosso-review-togli.log`).
- **BASSA, test mancanti** (mutanti della review rifatti, tutti uccisi: `prove/mutanti-review.log`):
  «Go senza l'ultima pagina» in `TestDeviazioneConTogli` (un `HasPrefix` al posto dell'uguaglianza);
  preventivo e prezzo a testo vuoto con i centesimi («€ 5,00 → € 7,00»); `TestReportPosizioni` (un campo
  pieno comincia sempre una riga sua dalla prima colonna, la seconda colonna a 107 mm, il piè con la
  linea di base a 291,31 mm, a 10 mm e fino a 200 mm); la regola «limite − 1 − i» con due chiavi
  (`ids[a][]=1…60&ids[b][]=1…60`: 49 e 48 elementi, misurato nell'immagine dello standalone).
- **BASSA, riferimento vecchio:** il «da decidere» di `bench/browser/caricamento.json` (domanda 18) è
  approvato come D-54: tolto da `da_decidere` e messo in `bench/deviazioni.json` come deviazione del
  livello browser (desktop e telefono, la stessa coppia di valori); `TestScenariBrowserDelRepo` lo
  fissa. Aggiornata la guida del banco (`docs/slices/004-banco.md`).
- **Sessione principale, codice finale, binari ricompilati:** `scripts/check` verde; banco sui dati
  importati verde (32 scenari, 493 passi, 19 differenze attese: 13 D-01 e 6 D-56, 0 inattese;
  browser con 2 differenze attese D-54 e nessun «da decidere»), database vuoto verde (6 attese D-56,
  17 passi non provati con quei dati), «AVANZI: nessuno».
- **Copertura:** con l'esito dei dati importati chiuse 61 (RIP-S-29 per D-54), parziali 18 (RIP-S-46
  compresa); senza banco chiuse 27.
- **Mutazioni della review:** 60 compilate, 51 uccise; delle 9 vive 3 equivalenti, 6 coperte dai
  test aggiunti nelle correzioni.
