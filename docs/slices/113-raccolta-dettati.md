# 113 — Raccolta dei dettati al salvataggio (T4.10, tredicesima)

- **Fonte:** piano della T4.10 riga 113 (approvato da Stefano il 19/11/2026), risposta H (D-105) e risposta I (privacy
  dei dettati). **Chi decide:** lo standalone a `go-baseline` (`Training.capture/3`, `Training.record/3`,
  `Workshop.create_repair/4`; test `training_test.exs`, `training_collection_live_test.exs`), salvo la D-108.
- **Voci:** RIP-H-48 (segno `// inventario: RIP-H-48` sui test di `internal/buste`, `internal/web`, `internal/assist`,
  `internal/importer`). **D nuova:** D-108 (registrata dal coordinatore il 21/11/2026). **D applicata:** D-105.
- **Modelli:** implementatore Opus (dati personali), revisione e mutazioni Opus.

## Cosa c'è

- `internal/buste/raccolta.go`: dentro `Crea`, dopo l'audit della busta e prima del commit, la raccolta. Controlla in ordine
  l'interruttore (`assist.training.collect`, spenta di default), l'esclusione del cliente (`dettati_esclusioni`), il
  lotto (`assist.training.remaining`); consuma un'unità con `UPDATE … WHERE CAST(valore AS INTEGER) > 0` e scrive la riga
  in `dettati_addestramento` (testo, proposta, nome del modello). All'ultima unità, o con l'interruttore acceso su un lotto
  già finito, spegne la raccolta con l'audit `assist.training_collect` dell'attore `system` e soli numeri
  (`{"on":false,"remaining":0}`). `buste.Dettato` si nasconde in `%v`, `%+v`, `%#v` e slog.
- **D-108:** tutta la raccolta sta in un `SAVEPOINT` dentro la transazione della busta. Con un errore si annulla, l'unità
  non si consuma, la busta si salva; nel registro va solo una frase fissa senza dati. Lo standalone invece tiene l'unità
  consumata se l'INSERT fallisce, e un guasto su impostazioni o lotto gli fa fallire tutta la busta.
- `internal/web/raccolta.go`: deposito in memoria delle **catture**. L'estrazione («✦ Estrai con AI») mette testo,
  proposta e modello sul server, legati a utente, nonce della sessione e chiave del modulo (`intake_key`); il browser manda
  solo la chiave, quindi non può inventare né prendere la cattura di un altro. Una nuova estrazione sostituisce la
  cattura; «Genera busta» la prende (un solo salvataggio la usa) e la rimette se la busta non si salva o se `Crea` va in
  panic. Durata 2 ore; tetto 1000 catture in tutto e 16 per sessione (oltre, si toglie la più vecchia della sessione; a
  deposito pieno la cattura nuova non si tiene e la busta si salva senza riga). Un riavvio le perde. Funziona anche senza
  JavaScript (la pagina dell'estrazione porta la chiave nel modulo).
- `internal/assist/raccolta.go`: `PropostaPerLaRaccolta`, la proposta in JSON con le chiavi e i formati dello standalone
  (date GG/MM/AAAA, compro_oro 0/1); `Estrazione.Testo` è il dettato senza spazi ai bordi.
- **D-105** (`internal/importer/mappa.go`, `ValoriAllImport`; `importa.go`): all'import `assist.training.collect`
  diventa «0», `assist.training.remaining` si copia com'è.
- Banco: `tools/banco/raccolta.go` semina collect=1 e remaining=1 nelle due app dopo l'import (Go: direttamente nel
  database, come `seminaTelegramGo`; standalone: `eval` di `Settings.put` nel container del banco). Scenario browser
  `bench/browser/dettato-lotto.json` (funzione `dettatoLotto` in `banco.mjs`), in fondo, prima di `dettato-nuova`.
- `docs/SVILUPPO.md`: sezione «Raccolta dei dettati al salvataggio».

## Deviazione dal piano (approvata dal coordinatore il 21/11/2026)

Il piano chiedeva uno scenario **HTTP**. Nello standalone l'estrazione è un evento LiveView, come nella 108, quindi lo
scenario è **browser** (`dettato-lotto`): nelle due app si salvano due buste dal dettato (8113/25, 8114/25) con la
raccolta accesa e una sola unità rimasta; attese una riga d'audit `system` dopo la prima e ancora una sola dopo la
seconda. La semina di collect/remaining è approvata **solo nel comando `banco`** e nello standalone solo sulla copia del
banco (fab-225); il binario `gestrip` non ha nessun percorso per seminarla (verificato: fuori dai test le chiavi compaiono
solo in `internal/buste/raccolta.go`, che scrive collect solo a «0», in `internal/importer/mappa.go`, che forza «0», e in
`tools/banco`, che né `cmd/` né `internal/` importano). Il contatore delle buste rimaste nel Go non si vede finché non
arriva la pagina della 114: si misura attraverso le righe `system`.

## Prove

- `internal/buste/raccolta_test.go`: riga nella stessa transazione, lotto che scende, raccolta spenta (4 casi), busta a
  mano, ultima unità che spegne con audit `system`, lotto finito/assente/illeggibile, concorrenza (due salvataggi
  sull'ultima unità, 5 giri: una riga, un audit), cliente escluso (anche a lotto finito), busta rifiutata, riservatezza;
  `TestRaccoltaErroreNonBloccaLaBusta` con 7 guasti iniettati (lettura delle impostazioni, esclusioni, consumo del lotto,
  INSERT in 3 modi, spegnimento all'ultima unità): busta e audit salvati, `remaining` resta 1, raccolta accesa.
- `internal/web/raccolta_test.go`: estrazione e salvataggio (anche senza JavaScript), cattura consumata, raccolta spenta,
  nuova estrazione che sostituisce, rifiuti che non la toccano, altra sessione o altra chiave, scadenza, ultima unità dalla
  pagina, errore con registro e audit puliti, `TestDepositoCattureTetto`, `TestRaccoltaPanicRimetteLaCattura`.
- `internal/importer/raccolta_test.go`: accesa con 300 rimaste → spenta e 300; spenta resta spenta; senza chiavi niente.
- `tools/banco`: `TestSuConLaCatena` controlla l'ordine (`importgo` prima di `raccolta`) e collect/remaining «1,1».
- `scripts/check` verde (rilanciato dal capo-slice tre volte; l'ultima con l'esito del banco).

## Mutazioni

- Implementatore: 24 scritte, 22 compilanti, 21 uccise, 1 equivalente (controllo di scadenza in `rimetti`, tolto dal
  codice); le 2 non compilanti riscritte e uccise. Sulle correzioni: 15 compilanti, 13 uccise al primo giro, 2 uccise dopo
  il rafforzamento dei test.
- Revisore: 34 compilanti, 32 uccise, 2 equivalenti: lettura del lotto e UPDATE senza guardia (scrittore unico,
  `SetMaxOpenConns(1)` e `_txlock=immediate`: la guardia nel WHERE resta una difesa in più); niente RELEASE del savepoint
  (il COMMIT li chiude).

## Review (Opus, indipendente)

Nessun rilievo ALTO. **MEDIA:** D-105 non implementata, decisiva da questa slice → corretta qui. **BASSE:** deposito delle
catture senza tetto → corretto; cattura persa con un panic in `Crea` → corretto; proposta nella forma del Go → in Dubbi.

## Banco s113 (21/11/2026, con il «memoria libera» del coordinatore)

Verde al primo giro. HTTP: 90 scenari, 1370 passi, differenze attese 1570, inattese 0, errori 0. Browser: 35 scenari, 70
viste, attese 45, inattese 0, da decidere 0, errori 0; `dettato-lotto` passato nelle due app. Guardia sotto 10 GB mai
scattata; nota 8 non comparsa (0 righe busy/locked nel log dello standalone). Copertura con l'esito: 353 voci, chiuse
222, rosse 0, parziali 16, aperte 104, fuori v1 11.

## Per il manuale

- La raccolta dei dettati è spenta finché il titolare non la accende (la pagina arriva con la 114); dopo un import dal
  vecchio programma è sempre spenta, con le buste rimaste del lotto conservate.
- Con la raccolta accesa, ogni busta compilata con «✦ Estrai con AI» e poi salvata tiene da parte, sull'apparecchio, il
  testo dettato e la proposta del modello. Le buste scritte a mano no.
- Se si rifà l'estrazione prima di salvare, vale l'ultima.
- I clienti che si sono opposti non vengono raccolti e non consumano il lotto.
- Dopo 500 buste raccolte la raccolta si spegne da sola.
- Se la raccolta ha un problema, la busta si salva lo stesso.

## Dubbi

1. **Forma della proposta** (BASSA della review): si salva quella del Go (date convertite, note D-20, telefono D-104/D-106
   anche dall'anagrafica, «data» sempre oggi), con le chiavi dello standalone. Conta per il conteggio delle correzioni
   (115) e gli pseudonimi (116).
2. **Cattura in memoria per 2 ore** e frase fissa nel registro in caso di errore (lo standalone non scrive niente): scelte
   dell'implementatore, accettate dal coordinatore. Un riavvio fra estrazione e salvataggio dà una busta senza riga.
3. Lo scenario `dettato-lotto` guarda le righe d'audit `system`, non la tabella delle righe raccolte: basta per uno
   spegnimento mancato, non per il contenuto della riga (coperto dai test Go).
