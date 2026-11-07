# 055a — Contatore rimasto indietro, «Data ricezione» e «Registrata il» (T4.2)

- **Fonte:** le decisioni di Stefano del 06/11/2026 (ripresa `docs/handoff/2026-11-06-fine-054.md`,
  punto 1 del «Prossimo passo»; Dubbio 3 della ricevuta 053; D-71…D-74 nell'inventario). Lo
  standalone a `go-baseline`: `repair_live.ex` (la riga `<dt>Data</dt>`, «Registrata il» con
  `Clock.stamp(@repair.inserted_at)`, il campo «Data» della modifica), `workshop_live.ex` («Data
  accettazione»), `repair_sheet.ex` («Data accettazione» della scheda PDF), `repair_report.ex` («Data
  riparazione» del report). Il riallineamento del contatore è solo del Go.
- **Voci:** RIP-H-31 (numero dal contatore), RIP-H-29 e RIP-S-04 (il campo della data), RIP-S-01 e
  RIP-S-02 (scheda), RIP-S-45 e RIP-S-46 (scheda PDF e report).
- **Deviazioni:** D-71 e D-72 (fatte qui); D-73 e D-74 solo annotate (058 e T4.6).
- **Lavoro:** un sottoagente, con mutazioni a mano sui punti chiave.

## Cosa c'è

- **Contatore rimasto indietro** (`internal/buste/contatore.go`, nuovo; `crea.go`):
  - `Crea` fa l'INSERT con una funzione sola (`inserisci`). Se l'INSERT trova il numero già usato
    **e** il numero è automatico (`Accettazione.Numero` vuoto: la pagina non lo manda più dalla 053,
    `risolviNumero` dà il prossimo del contatore), `riallineaContatore` porta il contatore dell'anno al
    massimo dei numeri già usati e la creazione riprova **una volta** con il numero dopo, nella stessa
    transazione. Un numero scritto (solo dal dominio) già usato resta rifiutato come prima.
  - **Il massimo** (`massimoDellAnno`): tutte le buste con un numero che finisce con «/AA» (`LIKE
    '%/26%'`, **cestino compreso**: l'indice unico di `buste.numero` lo copre), lette in Go con
    `numeroDellAnno`: spazi ai bordi tolti, zeri davanti ignorati («0045/26» vale 45), al più cinque
    cifre che contano (il formato del contatore), la barra e le due cifre dell'anno. Fuori formato non
    conta e non rompe il calcolo: «45/2026», «45-26», «A45/26», «45 /26», «+45/26», «123456/26»,
    cifre non ASCII, vuoto. Gli zeri davanti contano per il valore anche se non sono in conflitto con
    la forma senza zeri: così il contatore non dà mai «45/26» accanto a un «0045/26» importato (per la
    ricerca, `NormalizzaNumero`, sono lo stesso numero).
  - **Il contatore non scende mai:** `UPDATE … SET ultimo_numero = MAX(ultimo_numero, ?)`, e il
    riallineamento si fa solo se il massimo arriva almeno al numero tentato (altrimenti il conflitto
    non viene dal contatore: «Numero busta già esistente.»).
  - **Audit:** una riga `bag_sequence.realigned`, risorsa `bag_sequence`, id l'anno, esito `ok`, dati
    `year`, `previous`, `new`, `bag_number_taken`. I nomi seguono quelli esistenti (`repair.created`,
    `label_sheet.counter_reset` con `previous` e `year`). `previous` è il valore del contatore prima
    della creazione (il numero tentato meno uno; 0 per un anno senza riga, come l'anteprima che dà
    «1»), `new` il massimo; il numero dato alla busta (`new`+1) è in `repair.created`.
  - **Secondo fallimento:** se anche il numero dopo il massimo è occupato (o supera le cinque cifre)
    resta «Numero busta già esistente.» e la transazione annulla tutto, riallineamento e audit
    compresi.
- **D-71, «Data ricezione»:** scheda (riga `<dt>` e modulo della modifica), «Nuova riparazione» (era
  «Data accettazione»), scheda PDF (era «Data accettazione»), report (era «Data riparazione»).
  Precompilata con oggi e modificabile come prima; dati e nomi dei campi uguali. La lista non ha
  un'etichetta della data. Non toccati: il filtro «Data da/Data a» della home con la scelta
  «Accettazione» e la sua pillola, il 📅 del pannello QR, la ricevuta («ACCETTATA»).
- **D-72, «Registrata il»:** nella scheda la riga c'è solo quando il giorno di `creato_il` **in
  negozio** (`clock.DataDi`, Roma) è diverso dalla data di ricezione (`internal/web/scheda.go`,
  `pagine/scheda.html`).
- **Banco** (`tools/banco/confronto.go`, `main.go`): normalizzatori `D-71` e `D-72`, la composizione
  con «+», il normalizzatore sul testo letto dei PDF e lo standalone normalizzato nelle differenze che
  restano (per la D-01 del report). Dettagli in `docs/SVILUPPO.md`.
- **Documenti:** `docs/SVILUPPO.md` (il rimedio del contatore al posto del «Rischio noto», «Date della
  busta», «Deviazioni annotate, da fare» con D-73 e D-74, i normalizzatori composti), `docs/PIANO.md`
  («Idee per l'interfaccia»: D-74 per la v1 e la data dal dettato dopo la v1), il glossario e il
  capitolo 4 del manuale, `docs/manuale/da-scrivere.md`.

## Deviazioni

- **D-71** (approvata, Stefano 06/11/2026): la data di accettazione si chiama «Data ricezione».
- **D-72** (approvata, Stefano 06/11/2026): «Registrata il» solo se di un altro giorno.
- **D-73** (approvata, da fare nella 058): sull'etichetta 76×127 «Consegna:» diventa «All'artigiano:»,
  la data di stampa non cambia e «Scad.:» resta. Non implementata.
- **D-74** (approvata, da fare con la T4.6): dopo ogni invio WhatsApp la pagina torna alla home da
  sola, per tutti i link e pulsanti wa.me (avviso di pronta, preventivo, link al cliente, pannello QR,
  oreficerie, artigiani); con JavaScript GestRip apre WhatsApp e porta la propria scheda alla home con
  i filtri di prima, con l'audit e «già avvisata» salvati prima di lasciare la pagina; senza
  JavaScript come oggi. Non implementata.

Nel banco D-71 e D-72 sono dichiarate con i normalizzatori: `scheda` passi 2, 3, 4, 5, 19
(`D-64+D-71+D-72`; l'attesa «05/01/2026 10:00» ora è assente), `nuova` 3 e 8, `scheda-pdf` 3–14,
`etichetta-scheda-dipendente` 4 e 5, `report` 3–14, `report-dipendente` 2, `report-filtri` 3–12 e 14
(`D-71`; le attese dei PDF dicono «DATA RICEZIONE»). Nessuna riga nuova in `bench/deviazioni.json`.

## Prove

- **Test Go** (database vero, orologio al 31/10/2026):
  - `internal/buste/contatore_test.go`: `TestContatoreIndietroRiallineato` (contatore a 2 con i numeri
    fino a 50/26: la busta è 51/26, il contatore 51, il 2025 non toccato, una riga d'audit con anno,
    `previous` 2, `new` 50, `bag_number_taken` «3/26», `repair.created` e il cliente nuovo salvati: il
    vincolo fallito non ha annullato la transazione; la busta dopo è 52/26 senza altre righe),
    `TestContatoreAPostoNonRiallinea` (nessuna riga, anche con un 90/26 scritto fuori dal contatore),
    `TestContatoreAnnoSenzaRiga` (riga nuova al massimo, `previous` 0), `TestContatoreMassimoNelCestino`,
    `TestContatoreNumeriStrani` (zeri davanti, spazi, «45/2026», «99-26», «A99/26», «123456/26», un
    altro anno, vuoto, «/26», «0/26»; e 13 letture di `numeroDellAnno`), `TestContatoreNonScende`,
    `TestContatoreSecondoTentativoFallito` (un trigger occupa il numero dopo il massimo: «Numero busta
    già esistente.» e il database com'era, senza la riga d'audit), `TestContatoreNumeroScrittoNonRiallinea`,
    `TestVincoloUnicoAnnullaSoloLIstruzione` (in SQLite il UNIQUE fallito annulla solo l'istruzione:
    la riga prima e quella dopo si salvano, quella dell'istruzione fallita no).
  - `internal/web/nuova_test.go`, `TestNuovaNumero`: il contatore dietro ora salva la busta (13/26)
    con una riga `bag_sequence.realigned`; con il trigger che occupa anche il numero dopo,
    «Numero busta già esistente.» in cima alla pagina e nessuna busta in più.
  - `internal/web/scheda_test.go`, `TestSchedaRegistrataSoloSeDiversa` (D-72): stesso giorno; mezzanotte
    del 5 a Roma (ancora il 4 in UTC); le 23:59 del 5; le 23:59 del 4 («04/09/2026 23:59»); la
    mezzanotte del 6 (ancora il 5 in UTC, «06/09/2026 00:00»); settimane dopo; la data cambiata.
  - Ritoccati per la D-71: `TestSchedaDatiRiparazione`, `TestModificaApreIlModulo`,
    `TestModuloNuovaCampi`, `TestSchedaPDFScaricata`, i test della scheda PDF e del report in
    `internal/stampe` (nei confronti con i PDF di Chromium dello standalone la D-71 si applica al
    testo di Chromium, dichiarata nel commento).
  - Banco, `tools/banco/date_test.go`: `TestNormalizzaD71`, `TestNormalizzaD72`,
    `TestNormalizzatoriComposti` (composizione, D-xx sconosciuta, le D-xx controllate nell'inventario),
    `TestNormalizzaSulTestoDelPDF` (con `chromium-scheda-2.pdf`), `TestNormalizzataEPoiTogli`;
    `TestScenarioSchedaSuiDueDati` con `D-64+D-71+D-72` e l'attesa assente.
- **Mutazioni a mano** (copia dei file nello scratchpad, test mirati): 5 su `contatore.go` (zeri
  davanti non ignorati, riallineamento mai fatto, condizione del massimo cambiata, `previous`
  sbagliato, spazi non tolti) e 2 su `scheda.go` (giorno in UTC invece che a Roma, «Registrata il»
  sempre): **7 uccise, 0 sopravvissute**.
- `scripts/check` verde (gofmt, vet, staticcheck, errcheck, build, test, copertura: 353 voci, 26
  chiuse, 0 rosse, 81 parziali, 235 aperte, 11 fuori v1; segreti e dati veri OK);
  `scripts/parole-vietate --tutto` e `gitleaks dir` puliti.

## Banco

Istanze nuove, `gestrip-standalone:fab-222`.

- **Primo giro, solo HTTP, dati importati** (prima delle dichiarazioni): 47 scenari, 860 passi, 1057
  attese, **67 inattese**, tutte della D-71 e D-72 (PDF della scheda e del report, scheda, «Nuova
  riparazione»). Il report ha già le righe D-01 con `togli` e la data del giorno nel testo: per
  questo un normalizzatore e non righe a valori esatti.
- **Secondo giro, solo HTTP, dati importati**: 47 scenari, 860 passi, **1105 attese, 0 inattese**, 0
  errori.
- **Giro completo, dati importati**: HTTP 47 scenari, 860 passi, 1105 attese, **0 inattese**, 0
  errori; browser 9 scenari, 18 viste, 26 attese, **2 inattese**: tutte e due il 500 noto dello
  **standalone** in `firma-riquadro[1]` (`/repairs/6/photos/28`, «database is locked» di fab-222,
  corretto in fab-224; il Go nessun errore). Non accettate in `deviazioni.json`: il Go è verde.
- **Giro completo, database vuoto**: HTTP 47 scenari, 860 passi, 33 attese, **0 inattese**, 0 errori
  (29 non provati con questi dati); browser 9 scenari, **0 inattese** (9 non provati): verde.
- `banco giu` dopo ogni giro e `banco controlla`: «AVANZI: nessuno».

## Per il manuale

- La data in cui il negozio riceve l'oggetto si chiama **«Data ricezione»**: in «Nuova riparazione»
  (già scritta con oggi, si può cambiare), nella scheda, nella modifica, sulla scheda PDF e nel
  report. Sulla ricevuta per il cliente resta «ACCETTATA».
- Nella scheda «Registrata il» (quando la busta è stata salvata in GestRip) compare solo se è un
  giorno diverso dalla data ricezione, per esempio per una busta ricevuta ieri e registrata oggi.
- Se il contatore dei numeri interni è rimasto indietro (dati vecchi), «Genera busta» non si blocca
  più: GestRip passa da solo al primo numero libero. «Numero busta già esistente.» resta solo se anche
  quel numero è occupato, un caso che con un programma solo che scrive non succede.
- Glossario: «Data ricezione», «Consegna entro» e «Registrata il» aggiunte; capitolo 4 (ricevuta e
  scheda PDF) e `da-scrivere.md` aggiornati. Le schermate del manuale non mostrano le date: nessuna
  da rifare.

## Dubbi

1. **Gli zeri davanti contano nel massimo.** Un «0045/26» importato porta il contatore almeno a 45,
   anche se per l'indice unico «45/26» sarebbe libero: così non nascono due buste che la ricerca
   (`NormalizzaNumero`) considera lo stesso numero. Scelta prudente; da confermare.
2. **Le forme fuori formato non contano** («45/2026», «A45/26», sei cifre…): non possono entrare in
   conflitto con un numero del contatore, quindi non servono per il massimo.
3. **Il secondo fallimento annulla anche il riallineamento:** il contatore resta indietro e la riga
   d'audit non c'è (come ogni rifiuto della creazione). Con un solo programma che scrive non si
   riesce a provocarlo se non con un trigger di prova (`TestContatoreSecondoTentativoFallito`).
4. **`previous` nell'audit** è il numero tentato meno uno, non letto prima: un anno senza riga e uno
   con la riga a 0 danno tutti e due 0. L'attore è chi ha premuto «Genera busta».
5. **Il riallineamento vale solo per il numero automatico.** `Accettazione.Numero` scritto (solo dal
   dominio e dai test della 037d) già usato resta «Numero busta già esistente.» senza toccare il
   contatore.
6. **Il cassetto dei filtri della home** ha ancora la scelta «Accettazione» per il campo della data
   e la pillola «📅 Accettazione: …». Non l'ho toccata perché fa parte del filtro «Data da/Data a»
   che non si tocca. **Domanda:** deve diventare «Ricezione»?
7. **La ricevuta per il cliente** dice «ACCETTATA» accanto alla data: non dice «Data» e non l'ho
   toccata. **Domanda:** va bene così, o diventa «RICEVUTA»?
8. **Il report** diceva «Data riparazione» (diverso già nello standalone dalla scheda): ora «Data
   ricezione» come le altre.
9. **Nessuno scenario del banco prova il riallineamento** (servirebbe un contatore indietro nei dati
   del banco): è coperto dai test Go del dominio e della pagina.
10. **Banco:** con un normalizzatore, una differenza che resta anche normalizzata ora mostra lo
    standalone **normalizzato** (serve alla D-01 del report sopra la D-71). Da guardare in review.

## Review

La review l'ha fatta un sottoagente diverso da quello dell'implementazione. Nessun problema ALTO o MEDIO, codice non toccato. `scripts/check` verde; banco HTTP su fab-222 con 0 differenze inattese. Il browser ha solo il 500 noto dello standalone in `firma-riquadro`.

Note BASSE, lasciate così e annotate qui:
- **Lo standalone mostrato è quello normalizzato.** Quando una differenza resta diversa anche dopo la normalizzazione, il rapporto del banco mostra come «Standalone» il testo normalizzato (per esempio «DATA RICEZIONE» al posto di «DATA RIPARAZIONE»). Questo non nasconde differenze vere: il Go si confronta sempre col suo testo grezzo, e le righe `togli` e le righe esatte devono spiegare tutto il resto. Chi scrive una riga nuova di `deviazioni.json` su questi passi deve saperlo.
- **I normalizzatori D-71 valgono su tutta la pagina.** Riscrivono ogni «Data» della pagina, non solo la data di ricezione. Diventa un rischio se arrivano pagine con altri `<dt>Data</dt>`.
- **«Registrata il» di un altro giorno non è provato sui due programmi.** I dati sintetici non hanno buste registrate in un giorno diverso dalla ricezione: il caso lo coprono solo i test unitari.
- **Difetto vecchio, fuori slice, da mettere in lista.** Un numero importato «0046/26» non blocca il «46/26» dato dal contatore, perché l'indice unico confronta il testo esatto. Così possono nascere due buste che per il negozio hanno lo stesso numero.
