# 055d — Correzioni decise da Stefano il 06/11/2026 (T4.2, settima)

- **Fonte:** le risposte di Stefano del 06/11/2026 (tramite il coordinatore) alle domande b e c della
  ricevuta 052, ai Dubbi 5, 11 e 12 della 053, ai punti 1-5 del manuale della 049 e al dubbio 2 della
  051b; lo studio `docs/studio-D64-numero-etichetta.md`, punto (a). Lo standalone a `go-baseline`
  (`d3778df`) solo per i testi: `workshop_live.ex` (il suggerimento «Cerca in: …», «Massimo 2 per
  selezione»), `repair_live.ex` («Massimo 2 foto per volta.»), `delivery_signature_controller.ex`
  («Firma registrata: riparazione consegnata.»).
- **Voci:** RIP-H-03 (ricerca per numero, suggerimento), RIP-H-39 (campo «Etichetta»), RIP-H-22
  (scansione), RIP-S-04 (modifica), RIP-S-38 (firma), RIP-S-48 (stampanti), RIP-H-40 (foto in «Nuova
  riparazione» e coda), JOB-01 (orologio).
- **Deviazioni:** D-64 (A.b, B.1-B.4), D-63 (A.c), D-59 (C.1, solo il testo), **D-75 nuova** (C.4).
- **Lavoro:** un sottoagente, con mutazioni a mano. Nessuna review indipendente in questa slice.

## Cosa c'è

### A. Ricerca (052)

- **b. La busta senza etichetta nella ricerca per numero.** La regola dello studio D-64, punto (a),
  c'era già dalla 052 (`RicercaEtichetta.Trova`: il numero interno conta solo per le buste senza
  un'etichetta `RIP-…`). Nuovo: nella lista di una ricerca per numero una busta trovata **per il
  numero interno** porta sempre «⚠ senza QR» (`rigaDellaLista`, campo `NumeroInterno` di `rigaHome`),
  anche se è chiusa o ha un codice di altro formato (fuori da questa ricerca il segno resta quello di
  `Repair.missing_qr?/1`: aperta e senza codice). Il titolo del segno dice «Busta senza etichetta:
  trovata con il numero interno» invece di «Busta senza codice QR: aggiungilo dalla scheda».
  - **Anno, per «45» e «0045»:** come per le etichette (risposta 2 della T4.2), **tutti gli anni**:
    «45» trova le etichette 0045/AA di ogni anno e le buste senza etichetta 45/AA di ogni anno; con
    «45/26» solo quelle del 26. Non ho cambiato niente: era già così in `Trova`, l'ho solo provato.
- **c. Il suggerimento sotto il campo** (`#repair-search-hint`): con un numero d'etichetta (1-4 cifre,
  con o senza «/AA», da 1 in su: `LeggiRicercaEtichetta`) dice «Cerca in: cliente, n° busta,
  descrizione, note, note interne, QR», senza «telefono»; altrimenti il testo dello standalone. Lo
  sceglie il server dal testo cercato (`campoRicerca.Suggerimento`) e `app.js` mentre si scrive (i due
  testi negli attributi `data-cifre` e `data-testo`).

### B. Etichetta (053)

1. **«rip-» minuscolo.** `buste.NormalizzaCodice` è il codice come si salva e si confronta: lo usano
   `EtichettaDi` (e quindi `NumeroVisibile`, la ricerca per numero e il cassetto), `EtichettaScritta`
   (modulo e modifica), `CercaPerQR` (scansione e ricerca scritta di un codice) e `righeConCodice`
   (gemelle della creazione e della scheda, e scansione). «rip-2026-0045» e «Rip-2026-0045» si
   riconoscono e si salvano «RIP-2026-0045».
2. **L'avviso della forma non riconosciuta.** `SembraEtichetta` riconosce i codici che somigliano a
   un'etichetta senza esserlo: quelli che cominciano con «RIP» (spazi esclusi) e i numeri di al più 6
   cifre, con o senza «#» davanti e con o senza un anno di 2 o 4 cifre dopo «-», «/», «.» o «_»
   («45/2026», «0045-26», «45.26», «#45», «00045», «0», «4 5», «123456»). Un'etichetta vera, un numero
   scritto come la ricerca, un codice di altro formato («ABC-12») e un codice a barre lungo (13 cifre)
   no. In «Nuova riparazione» `Crea` lo dice una volta (`*FormaNonRiconosciuta`, dopo gli errori dei
   campi e prima delle gemelle); il modulo lo mostra e porta il codice nel campo nascosto
   `forma_avvisata` (come `gemelle_avvisate`); rimandato con lo stesso codice salva com'è. Nella
   modifica lo stesso (`Modifica.FormaConfermata`, avviso sotto il campo «Etichetta»), ma solo per un
   testo **cambiato**: un codice importato rimandato com'era non avvisa. **Testo proposto:**
   - «Nuova riparazione»: «⚠ «45/2026» non è un'etichetta riconosciuta: scrivila come 0045/26 o 45,
     oppure premi di nuovo «Genera busta» per salvarla così com'è.»
   - modifica: lo stesso, con «Salva modifiche» al posto di «Genera busta».
3. **Gli spazi.** Regola scelta, prudente:
   - ai **bordi** si tolgono sempre, da ogni codice (anche gli spazi Unicode), alla creazione e alla
     modifica; prima un codice di altro formato si salvava con gli spazi;
   - **dentro** si tolgono solo quelli **accanto a un separatore**: «-» per il codice intero
     («RIP - 2026 - 0045», «rip-2026- 0045» → «RIP-2026-0045»), «-» e «/» per il numero scritto
     («0045 / 26» → «RIP-2026-0045»), e solo se il risultato è un'etichetta riconosciuta;
   - mai fra due cifre o fra due lettere: «4 5» non diventa «45» (avvisa), «RIP 2026 0045» e
     «RIP-2026-00 45» restano come sono (avvisano), «ABC 12» di un collega resta «ABC 12».
   - La modifica normalizza anche un codice rimandato **uguale** al salvato (spazi ai bordi, «rip-»),
     ma non lo legge come un numero scritto: un «45» importato resta «45».
   - I dati già salvati non si riscrivono: la normalizzazione vale in ogni lettura (gemelle,
     scansione, numero visibile, ricerca) e alla prossima modifica della busta.
4. **Lo storico di questa etichetta** è il link `GET /cerca?cerca=0045/26`, cioè la stessa lista di
   A.b: la busta senza etichetta con il numero interno 45/26 ci compare marcata «⚠ senza QR».

### C. Manuale della 049

1. **«Massimo 4 foto per pulsante».** In «Nuova riparazione» la frase sopra i pulsanti era «Massimo 4
   per selezione» (`nuova.html`) e l'errore era «Massimo 4 foto per volta.» (`app.js`): ora tutti e
   due «Massimo 4 foto per pulsante». Nella scheda resta «Massimo 4 foto per volta.» (lì si sceglie
   una volta per pulsante e il limite è per invio). Manuale, capitolo 3, aggiornato.
2. **La riga foto a 390 px.** La riga sbordava perché `.stato-foto` è una griglia: la sua colonna si
   allargava fino al nome intero del file (`white-space: nowrap`), e «Rimuovi» finiva oltre il bordo.
   Ora le colonne di `.stato-foto`, `.avanzamento-foto` e `.foto-accettazione` sono `minmax(0, 1fr)`,
   il nome si accorcia con «…» e «Rimuovi» non si stringe. Verifica: vedi «Banco».
3. **«Riprova» con il conto alla rovescia.** Nella riga rossa della coda, finché i tentativi
   automatici continuano (non dopo il quinto errore di fila), accanto a «Riprova» c'è «nuovo tentativo
   tra N s…», su una riga sua sotto il messaggio e aggiornato ogni secondo (`countdown`, `tick` in
   `app.js`; `.attesa-coda` in `app.css`);
   a zero «nuovo tentativo…». Dopo il quinto errore resta solo «Riprova». Risponde anche alla domanda
   3 della 051.
4. **D-75, firma Compro Oro.** Dopo la firma di una scheda Compro Oro la scheda dice «Firma
   registrata: pratica chiusa.» invece di «Firma registrata: riparazione consegnata.» (che resta per
   le riparazioni). `messaggioFirma` rilegge la busta appena firmata con `buste.Scheda` (lettura senza
   audit: aggiunta ai chiamanti ammessi di `TestLetturaSenzaAuditSoloDaiChiamantiNoti`, perché legge
   solo il tipo dopo un'azione già registrata); se la rilettura non riesce, il testo dello standalone.
   Registrata in `docs/SVILUPPO.md`. **Il banco non la vede**: `firma` e `firma-riquadro` firmano solo
   riparazioni; nessuna riga in `bench/deviazioni.json`.
5. **«stampante» al posto di «CUPS».** Nei messaggi per l'utente:
   - «CUPS non risponde.» → «la stampante non risponde.»; «CUPS ha rifiutato il lavoro (…).» → «la
     stampante ha rifiutato il lavoro (…).»; «CUPS non ha risposto in tempo. …» → «la stampante non ha
     risposto in tempo. …» (minuscole perché seguono «Stampa non riuscita:» ed «Etichetta non
     stampata:»);
   - la pagina «Stampanti» senza elenco: «Le stampanti non rispondono. L'elenco delle stampanti non
     c'è. …» (prima l'errore di CUPS, «CUPS non risponde»); «… senza i comandi di CUPS.» → «… senza i
     comandi delle stampanti.»; «Questa coda non c'è in CUPS: …» → «Questa stampante non c'è più nel
     computer: …»; «CUPS non risponde: puoi tenere la coda salvata …» → «Le stampanti non rispondono:
     puoi tenere la stampante salvata …»; «(non trovata in CUPS)» → «(non trovata nel computer)»;
     «Code di CUPS con un nome che GestRip non usa» → «Stampanti con un nome che GestRip non usa».
   - CUPS resta nei registri, nei commenti e nel glossario (spiegato come termine tecnico).
   - **Segnaposto delle opzioni** (`segnapostoOpzioni`): con una stampante scelta che dice le sue
     opzioni (lpoptions), le prime due che si possono scrivere (`LeggiOpzioni` le accetta: mai
     PageSize e simili), ognuna con il valore di adesso (o la prima scelta), così l'esempio copiato
     com'è non cambia niente; senza, un esempio per tipo di documento. **Proposti:**
     - etichette QR 57×32 (termica a rotolo con etichette staccate): «MediaTracking=Gap Darkness=8»
       (quello di prima);
     - etichetta busta 76×127 (termica per etichette grandi): «Darkness=12 PrintSpeed=3».

### D. Lettura monotona del tempo

`internal/clock` ha `Partenza` (`Parti`, `Trascorso`: `time.Now` e `time.Since`, con la lettura
monotona) e `Resta(t)` (`time.Until`). `figlitest` li usa per l'attesa di `aspettaMorti` e per il
limite di vita dal `Deadline` di go test (che ha la lettura monotona). Tolta la variabile `orologio`
di `figlitest`. `clock.Sistema.Adesso` non cambia: fa `UTC()` e quindi resta senza lettura monotona,
che è giusto per gli istanti da salvare.

## Deviazioni

- **D-64** (approvata), dichiarata nel banco con due passi `solo_go` nuovi in `busta-etichetta` (18:
  l'avviso della forma per «45/2026»; 19: «rip-2026- 0045» riconosciuto dalle gemelle come
  RIP-2026-0045). Il segno «⚠ senza QR» nella ricerca per numero sta nelle liste di ricerca per
  numero, che il banco prova già solo nel Go (passi `solo_go` D-64 della 052). La scansione che trova
  anche i salvati minuscoli o con gli spazi: i dati sintetici non ne hanno, il banco non la vede.
- **D-64**, nel banco anche due righe nuove in `bench/deviazioni.json` (`home-filtri` passi 9 e 10: la
  busta 12/26 annullata con «⚠ senza QR» nella ricerca «012/26»).
- **D-58** (approvata): il conto alla rovescia; due righe browser in `bench/deviazioni.json` (`coda`,
  `salva-2.striscia`, desktop e telefono).
- **D-63** (approvata): il suggerimento senza «telefono» con un numero. Il banco confronta il testo
  del modulo solo senza testo cercato (`home-cerca`, `cerca-qr`), dove è uguale allo standalone.
- **D-59** (approvata): in «Nuova riparazione» il testo «per pulsante». Cambiate 4 righe di
  `bench/deviazioni.json` (`coda` e `sfondo`, `cinque.errori`, desktop e telefono: «Massimo 4 foto per
  pulsante.»), il motivo dice la 055d; `TestScenariBrowserDelRepo` aggiornato.
- **D-75** (nuova, decisa da Stefano il 06/11/2026): sopra, C.4. Da aggiungere alla tabella
  dell'inventario privato (vedi i Dubbi).
- **Senza D-xx**, perché sono pagine solo del Go: i testi «stampante» (pagina «Stampanti», stampa
  diretta; aggiornata l'attesa di `stampa-diretta`, passo con il rifiuto di lp) e il segnaposto.

## Prove

- **Test Go nuovi:**
  - `internal/buste/forma_test.go`: `TestNormalizzaCodice` (17 casi), `TestSembraEtichetta` (25),
    `TestCreaFormaNonRiconosciuta` (tre conferme sbagliate e il database uguale, prima i campi,
    confermata, «rip-» senza avviso, prima la forma poi le gemelle, tutte e due confermate, la gemella
    importata minuscola e con gli spazi), `TestModificaFormaNonRiconosciuta` (avviso e busta uguale,
    prima i campi, confermata, rimandata uguale senza avviso, «rip-» nella modifica).
  - `internal/web/correzioni_test.go`: `TestHomeRicercaNumeroInterno` («0012», «12», «12/26»; chiusa;
    codice di altro formato; il titolo; fuori dalla ricerca per numero niente segno),
    `TestSuggerimentoRicerca` (8 testi, la pagina, `app.js`), `TestNuovaFormaNonRiconosciuta` (avviso,
    niente salvataggio, errore dei campi dopo l'avviso, salvata com'è, «rip-» in maiuscolo),
    `TestModificaFormaNonRiconosciutaWeb` (avviso sotto il campo, modulo rimandato com'è che salva),
    `TestSalvaFirmaCompraOro` (messaggio, stato «chiusa», ripiego), `TestSegnapostoOpzioni` (quattro
    casi e la pagina senza «CUPS»), `TestCodaContoAllaRovescia` (`app.js` e `app.css`).
  - `internal/clock/clock_test.go`: `TestPartenzaMonotona`.
- **Test cambiati** (il comportamento nuovo): `TestEtichettaScritta` (minuscolo, spazi ai separatori,
  spazi ai bordi tolti), `TestCreaEtichettaScritta`, `TestCodiceQR`, `TestModificaQR`,
  `TestModificaEtichettaScritta` (uguale al salvato normalizzato, «45» importato che resta),
  `TestNumeroVisibile`, `TestRicercaEtichettaTrova`, `TestCercaPerQR` e `TestSchedaGemelle` (la
  scansione trova anche i salvati minuscoli o con gli spazi), `TestHomeRicercaECassetto`,
  `TestNuovaSezioneFoto`, `TestCodaNelloScript`, i test delle stampanti, `TestLetturaSenzaAudit…`,
  `TestScenariBrowserDelRepo`.
- `scripts/check` verde (ultimo giro sul codice finale): gofmt, vet, staticcheck, errcheck, build, test,
  copertura (353 voci: 26 chiuse, 0 rosse, 83 parziali, 233 aperte, 11 fuori v1), segreti e dati veri OK.

## Mutazioni

`to-be-deleted-after-use/slice-055d/mutazioni/muta.py`, 39 punti, test mirati in una copia del repo.
Primo giro: 37 uccise, 2 sopravvissute:

- m15 (la modifica avvisa anche per un codice rimandato uguale al salvato): viva perché nessun test
  rimandava un codice importato con gli spazi che, normalizzato, cambia. Aggiunto il caso a
  `TestModificaFormaNonRiconosciuta` («␠45/2026␠» importato e rimandato: niente avviso, salvato
  «45/2026»); rifatta a mano: uccisa.
- m14 (`CercaPerQR` con `CodiceLetto` invece di `NormalizzaCodice`): **equivalente**, perché
  `righeConCodice` normalizza di nuovo il codice; la normalizzazione in `CercaPerQR` serve solo a
  dire «vuoto» prima della lettura, e gli spazi sono già tolti da tutte e due.

**Totale: 39 punti, 38 uccise, 1 equivalente.** Uccise, fra le altre: «rip-» non riconosciuto, spazi
ai separatori non tolti o tolti tutti, numero 0 accettato, bordi non tolti, la barra del numero
scritto, la somiglianza senza «RIP» o con 4 cifre o con gli spazi, la conferma ignorata, l'avviso mai
dato, le gemelle e la scansione senza normalizzare il salvato, le candidate SQL senza `upper`, la
modifica che non normalizza o legge «45» come numero, il segno «⚠ senza QR» assente o su ogni
ricerca, il suggerimento sempre con «telefono», `forma_avvisata` non letto o non rimandato (nuova e
modifica), l'avviso della modifica non mostrato, il messaggio della firma sempre uguale o sempre
«pratica chiusa», il segnaposto con PageSize, con tre opzioni o sempre predefinito, «CUPS» rimesso nei
messaggi e nell'elenco, il conto alla rovescia anche a coda ferma, la regola CSS tolta, «per volta» in
«Nuova riparazione», `Parti` senza lettura monotona, `Resta` rovesciato, l'attesa e il limite di
`figlitest` rovesciati.

## Banco

`gestrip-standalone:fab-224`, binari dell'albero di lavoro.

- **Primo giro, dati importati** (`s055d`): HTTP 4 inattese, browser 4 inattese. Tutte dichiarabili,
  nessun guasto:
  - `busta-etichetta[18]` e `[19]` (passi nuovi): le attese per i dati importati non coprivano le
    chiavi delle attese generali (si fondono per chiave). Corrette.
  - `home-filtri[9]` e `[10]` («012/26» e « 012/26 »): la busta 12/26 annullata senza etichetta ora ha
    «⚠ senza QR» (A.b). Due righe **D-64** in `bench/deviazioni.json`.
  - `coda[0]` e `[1]`, `salva-2.striscia`: il conto alla rovescia accanto a «Riprova» (C.3). Il
    numero dei secondi dipende da quando si legge, quindi `banco.mjs` scrive «N» al posto del numero
    (solo il numero: il testo resta); due righe **D-58** in `bench/deviazioni.json`, tolta l'attesa
    uguale per le due app da `bench/browser/coda.json` e da `TestScenariBrowserDelRepo`.
- **Giro completo, dati importati** (`s055d`, dopo le correzioni): HTTP **47 scenari, 862 passi, 1107
  differenze attese, 0 inattese**, 0 errori; browser **10 scenari, 20 viste, 28 attese, 0 inattese**,
  0 errori: **verde**.
- **Giro completo, database vuoto** (`s055v`): HTTP 47 scenari, 862 passi, 33 attese, **0 inattese**
  (29 non provati con questi dati); browser 10 scenari, **0 inattese** (9 non provati): **verde**.
- **Schermate del manuale** (`banco manuale`, istanza `m055d`, 390 px per il telefono): la ricetta
  `coda-errore` non andava già con HEAD (provato con `git archive HEAD`): le due ricette della coda
  creavano due buste aperte con la stessa etichetta `RIP-2026-0900` e, dalla 053, la seconda riceveva
  l'avviso delle gemelle. Corretta in `manuale.mjs` (un'etichetta per busta, dalla 0900). Rifatte le
  schermate: cambiano `03-foto-nuova-telefono.png` («Massimo 4 foto per pulsante», i nomi accorciati
  con «…» e «Rimuovi» dentro il riquadro: **C.2 verificato**), `03-foto-coda-errore-telefono.png` (il
  conto alla rovescia, su una riga sua sotto il messaggio), `03-foto-coda-telefono.png` e
  `04-stampanti-desktop.png` (testi e segnaposto), e le due della scheda (busta 0901/26).
- Il conto alla rovescia su una riga sua (`flex-basis: 100%; order: 1`, solo CSS) è arrivato dopo i
  giri completi: giro mirato `s055m` (HTTP `healthz`, browser `coda`, `tentativi`, `sfondo`):
  HTTP 1 scenario, 7 passi, 0 inattese; browser 3 scenari, 6 viste, 22 attese, **0 inattese**: **verde**.
- `banco giu` dopo ogni giro: «AVANZI: nessuno».

## Per il manuale

Aggiornati i capitoli:

- `03-foto.md`: «Massimo 4 foto per pulsante.» in «Nuova riparazione» (la frase e l'errore), il nome
  lungo accorciato con «…», il conto alla rovescia «nuovo tentativo tra N s…» accanto a «Riprova».
- `04-stampe-ed-etichette.md`: i messaggi con «stampante» al posto di «CUPS», il segnaposto delle
  opzioni.
- `05-firma-di-consegna.md`: «Firma registrata: pratica chiusa.» per il Compro Oro.
- `06-ricerca-e-qr.md`: le buste senza etichetta nella ricerca per numero, marcate «⚠ senza QR», con
  la regola dell'anno; il suggerimento senza «telefono»; il campo «Etichetta» scritto a mano
  (minuscolo, spazi) e l'avviso della forma non riconosciuta; «Salva modifiche».
- `99-glossario.md`: CUPS come termine tecnico che i messaggi non usano più.
- Schermate rifatte con `banco manuale` (vedi «Banco»); didascalia della figura 3.3 e descrizione in
  `bench/manuale/schermate.json` con il conto alla rovescia.

## Dubbi

1. **D-75 nell'inventario privato:** la riga va aggiunta alla tabella delle deviazioni (testo di prima
   «Firma registrata: riparazione consegnata.», testo nuovo «Firma registrata: pratica chiusa.», solo
   Compro Oro). Il repo pubblico non ha l'inventario: la scrive il coordinatore.
2. **Il suggerimento con 1-4 cifre** toglie solo «telefono», come deciso; ma con un numero la ricerca
   non guarda nemmeno cliente, descrizione e note (guarda etichette e numeri interni). Un testo più
   esatto sarebbe «Cerca il numero dell'etichetta (0045/26) o il numero interno delle buste senza
   etichetta». Da decidere con Stefano.
3. **«⚠ senza QR» su una busta con un codice di altro formato** (per esempio «SINT-QR-0045»): nella
   ricerca per numero la marca, perché senza etichetta `RIP-…` conta come «senza etichetta» (studio
   D-64). Il segno dice «senza QR» anche se un QR c'è: il titolo spiega «senza etichetta». Se Stefano
   preferisce, un testo diverso («⚠ senza etichetta») solo in quel caso.
4. **Dati già salvati con gli spazi o in minuscolo:** non li ho riscritti (nessuna migrazione dei dati,
   e l'import resta fedele allo standalone). Si riconoscono in ogni lettura e si sistemano alla prima
   modifica della busta. Se Stefano vuole, una riscrittura una tantum al primo avvio (con una riga
   d'audit) è piccola.
5. **La scansione ora confronta anche il salvato normalizzato** (lo standalone confronta il salvato
   com'è): la query non usa più l'indice 0016 su `codice_qr` (cerca dentro il codice senza spazi, in
   maiuscolo). Con qualche migliaio di buste è una lettura di pochi millisecondi; se servisse,
   un'espressione indicizzata.
6. **Il segnaposto con le opzioni della stampante** usa il valore **di adesso** della stampante:
   copiato com'è non cambia niente, ma mostra le chiavi vere. In alternativa una scelta diversa
   dall'attuale (più «esempio», ma copiarla cambia la stampa). Ho tenuto la prudente.
7. **I segnaposto per tipo** («Darkness=12 PrintSpeed=3» per l'etichetta busta) sono esempi
   generici delle stampanti termiche: con la stampante vera (T5.5) conviene metterci le sue opzioni.
8. **Il conto alla rovescia** il banco lo vede una volta (`coda`, «tra N s…»), ma nessuno scenario lo
   guarda scorrere fino a zero né sparire dopo il quinto errore: lo provano solo i test dei testi di
   `app.js`. Se serve, uno scenario browser in una slice dopo.
9. **`TestScenariBrowserDelRepo`** ha nel codice la lista delle deviazioni del browser: ogni riga
   nuova di `bench/deviazioni.json` va scritta anche lì (qui le D-58 e le D-59 «per pulsante»).
10. **Nessuna review indipendente** in questa slice: se il coordinatore la vuole, i punti più delicati
   sono la regola degli spazi (`NormalizzaCodice`, `SembraEtichetta`) e la scansione che ora confronta
   il salvato normalizzato.

## Review

La review l'ha fatta un sottoagente diverso. Dopo le sue correzioni `scripts/check` è verde e il banco completo `rev055d` su fab-224 con dati importati dà 0 differenze inattese: HTTP 47 scenari e 862 passi, browser 10 scenari.

- **MEDIA, scelta di prodotto, da confermare: la scansione confronta il codice salvato normalizzato.**
  - Lo standalone (`lookup_repairs_by_qr/2`) confronta `qr_code` così com'è e toglie gli spazi solo al codice letto. Il Go ora trova anche un salvato `" RIP-2026-0046"` e i `rip-2026-0045` minuscoli, quindi può mostrare delle gemelle dove lo standalone dava una busta sola. Succede solo con dati importati. I codici di altro formato restano sensibili alle maiuscole, come nello standalone.
  - È dichiarato in `docs/SVILUPPO.md` (D-64, «Codice normalizzato»). Il banco non lo vede, perché i dati sintetici non hanno codici del genere.
  - Il commit tiene la versione (a), quella raccomandata, coerente con il numero visibile, le gemelle e la ricerca. Se Stefano non la conferma si passa alla (b): normalizzare il codice salvato solo per le etichette `RIP-…` e tornare al confronto esatto per gli altri codici.
  - La query non usa più l'indice della migrazione 0016. Con 30.000 buste su pc-fisso costa circa 6 ms per scansione e circa 8 ms per la ricerca «45».
- **BASSA, corretta: le query non trovavano codici con spazi Unicode.** Il filtro SQL che sceglie le buste da controllare toglieva solo gli spazi ASCII, mentre `NormalizzaCodice` toglie anche spazi Unicode e tab. Una busta importata come «RIP-2026- 0045» mostrava «0045/26», ma scansione, gemelle e ricerca non la trovavano. Ora quel filtro cerca anno e numero con `instr`. C'è un test nuovo, `TestCandidateConSpaziUnicode`.
- **BASSA, corretta: due commenti diventati falsi,** in `ricerca.go` e nel motivo dell'indice `buste(codice_qr)` in `internal/importer/mappa.go`. Togliere quell'indice con una migrazione è una decisione per dopo.
- **BASSA, segnalata: il conto alla rovescia nel banco browser può sfarfallare.** Se il browser tarda più di 5 s, il testo passa da «tra N s…» a «nuovo tentativo…». Ne esce al massimo una differenza inattesa ogni tanto; una differenza vera non può restare nascosta.
