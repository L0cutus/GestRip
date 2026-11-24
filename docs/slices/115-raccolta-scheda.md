# 115 — Esclusione dalla raccolta dalla scheda, scadenza dei dettati a 12 mesi (T4.10, quindicesima)

- **Fonte:** piano della T4.10, riga 115 (approvato da Stefano il 19/11/2026). **Chi decide:** lo standalone a
  `go-baseline` (`Training.exclude_customer/2`, `expire/1`, `started?/0`, `excluded_customer?/2`;
  `MediaCleanup.run_training_expiry/3`; `repair_live.ex`), salvo le D approvate.
- **Voci:** RIP-S-26; RIP-H-51 solo per la durata di 12 mesi (la cancellazione quando la busta va nel cestino resta alla
  T4.11). Segno `// inventario:` sui test di `internal/buste`, `internal/web` e `cmd/gestrip`. **D nuove:** nessuna.
- **Modelli:** implementatore Opus (cancellazioni), revisione e mutazioni Opus.

## Cosa c'è

- `internal/buste/raccolta_scheda.go`: `EscludiClienteDellaBusta` (esclusione partendo dall'id della busta aperta),
  `LimiteDettati` e `ScadenzaDettati`.
  - Il cuore dell'esclusione della 114 è passato in `escludiCliente(tx, cliente, …)`, usato sia dalla pagina Assistente
    sia dalla scheda: la pagina si comporta come prima.
  - L'esclusione si può fare solo se la raccolta è stata accesa almeno una volta (`assist.training.remaining` presente,
    come `started?/0`). Il controllo «cliente già escluso» si rilegge dentro la transazione: il secondo tocco non
    scrive niente.
  - Cancella i dettati di tutte le buste del cliente, cestino compreso (come `with_discarded` nello standalone), e scrive
    la riga in `dettati_esclusioni` e l'audit `assist.training_customer_excluded` con il solo numero dei cancellati.
- **Scadenza:** cancella solo i dettati con `creato_il` prima di «adesso meno 12 mesi» (limite in UTC, il 29 febbraio
  diventa il 28, come `DateTime.shift`). Le buste restano e `dettati_esclusioni` non si tocca. Audit
  `assist.training_expired` con attore `retention` e `{"cancellati":N}` **solo se** N > 0. Tutto in una transazione:
  se l'audit non si scrive, non si cancella niente.
- `internal/media/pulizia.go`: `ConScadenzaDettati(orologio, funzione)`, iniettata da `cmd/gestrip/main.go` (`media`
  non può importare `buste`). Gira una volta per giorno del negozio (Europe/Rome), come `run_training_expiry/3`; un
  errore o un panico si riprovano al giro dopo e non fermano la coda delle cancellazioni dei media; nel registro una
  riga fissa con il solo tipo dell'errore.
- `internal/web/raccolta_scheda.go` e `pagine/scheda.html`: rotta `POST /repairs/{id}/escludi-raccolta` dietro
  `azioneDelTitolare` (ruolo riletto a ogni richiesta, CSRF), riquadro `#repair-training` subito prima del link
  pubblico come nello standalone, conferma con `data-confirm` come gli altri pulsanti della scheda.
- **Note nuove in `docs/domande-aperte.md`** (chieste dal coordinatore): 11 (nel Go la busta nel cestino tiene il
  dettato, nello standalone `Trash.discard/4` e `Training.expire/1` lo cancellano) e 12 (con «0045/26» e «N. 45/26»
  insieme, dalla pagina Assistente non si esclude nessuna delle due buste).
- Ritocchi: `ruoli_test.go` (l'evento `training-exclude-customer` della scheda ha ora la rotta Go, voce RIP-S-26),
  finti dei test web con il metodo nuovo, `TestChiaviSoloGoComplete` con i due file nuovi che leggono `impostazioni`
  (nessuna chiave nuova).

## Prove

- Test prima rossi per la ragione giusta (funzioni vuote, rotta 404, collegamento in `main.go` tolto), poi verdi:
  - dominio: esclusione dalla scheda, secondo tocco, raccolta mai accesa (anche con il solo `assist.training.collect`),
    busta non trovata o nel cestino, gemelle, busta senza etichetta, tutto o niente;
  - scadenza: confine (−1 µs cancella, limite esatto e +1 µs no), 29 febbraio, niente da cancellare = niente audit,
    secondo giro senza audit, audit guasto = niente cancellato; `LimiteDettati` anche con l'ora data a Roma (1° marzo
    00:30 CET e 28 marzo 2027, fra due date diverse dell'ora legale);
  - pulizia: una volta per giorno del negozio (00:30 a Roma è già il giorno dopo), riprova dopo errore o panico;
  - `serve`: il giro d'avvio cancella un dettato del 2020 e tiene quello di un'ora fa;
  - HTTP della scheda: titolare, secondo tocco, raccolta mai accesa, dipendente (niente riquadro, POST forgiato
    rifiutato con «Solo il titolare può escludere un cliente.»), cestino/inesistente/id non valido («Riparazione non
    trovata.»), senza CSRF 403, guasto («Esclusione non riuscita.», niente cancellato).
- `scripts/check` verde, rilanciato dalla sessione principale (exit 0, 0 rosse).

## Mutazioni

- Implementatore: 4 a mano, tutte uccise.
- Revisore: 24 compilanti, 22 uccise. Sopravvissute: la lettura di `collect` invece di `remaining` nell'esclusione e
  `.UTC()` tolto in `LimiteDettati`. Dopo la review si sono aggiunti i due test, rossi con la mutazione e verdi senza:
  **24 su 24 uccise**.

## Review (Opus, indipendente)

Nessun rilievo ALTO né MEDIO. BASSI: i due test mancanti (fatti); il secondo POST con il segno della pagina già usato
torna alla scheda senza segno e scrive `repair.viewed` (convenzione D-44 già valida per tutta la scheda, nessuna
scrittura su dettati o esclusioni); le differenze dallo standalone da dichiarare (qui sotto); la scadenza gira anche su
un database appena importato senza backup verificato (come lo standalone; i dettati veri hanno meno di 12 mesi).

## Banco

Giri del 24/11/2026 con il «memoria libera» del coordinatore e la guardia sotto 10 GB (mai scattata).

- **s115a:** HTTP 91 scenari, 0 inattese. Browser rosso per la **nota 8**: `salva-avverti` vista PC, lo standalone
  risponde 500 a `GET /audit` dopo 5015 ms («database is locked»), 6 inattese e 1 errore, tutte dalla parte dello
  standalone.
- **s115b** (l'unica ripetizione concessa): HTTP 91 scenari, 0 inattese. Browser di nuovo rosso per la nota 8, in
  un altro scenario: `pannello-qr` vista telefono, 500 a `GET /audit` dopo 5016 ms, 93 inattese a catena «(assente)»
  dalla parte dello standalone e 1 errore.
- Nessuna differenza inattesa dalla parte del Go; nessuno dei due scenari è toccato dalla 115.
- **Pubblicata per decisione di Stefano (24/11/2026)**, come la 109, con i due rossi della nota 8 spiegati qui; solo
  per questa slice, la regola della sola ripetizione non cambia.
- Copertura con l'esito di s115b: **223 su 353, 0 rosse** (225 nella 114). Le due voci in meno restano parziali perché
  lo scenario del browser caduto per la nota 8 non conta come provato; non dipendono dalla 115.

## Per il manuale

- Nella scheda di una busta il titolare trova «🎙️ Escludi dalla raccolta dettati», ma solo se la raccolta dei dettati
  è stata accesa almeno una volta.
- Il pulsante chiede conferma, poi cancella i dettati di tutte le buste di quel cliente e non raccoglie più le sue
  buste future.
- Dopo l'esclusione, la scheda di ogni busta di quel cliente dice «Cliente escluso dalla raccolta dettati
  (opposizione).» e il pulsante non c'è più.
- Il dipendente non vede né il pulsante né la scritta.
- Dalla scheda non serve scrivere il numero: funziona anche quando la pagina Assistente risponde «Più buste
  corrispondono…».
- Ogni giorno il programma cancella da solo i dettati raccolti da più di 12 mesi; le buste restano.

## Differenze dallo standalone (dichiarate)

- **Gemelle:** lo standalone, dalla scheda, passa `bag_number` a `exclude_customer` e con due buste dallo stesso
  numero `Repo.one` va in errore; il Go esclude il cliente della busta aperta. Escludere una busta sola resta
  impossibile (nota 12).
- **Busta nel cestino:** la rotta risponde «Riparazione non trovata.», come le altre azioni della scheda.
- **Scadenza e cestino:** lo standalone in `expire/1` cancella anche i dettati delle buste nel cestino; il Go no fino
  alla T4.11 (nota 11).

## Dubbi

1. **Conferma senza JavaScript:** sulla scheda c'è solo `data-confirm`, come lo standalone e gli altri pulsanti della
   scheda; senza JavaScript il POST esegue subito. La pagina della 114 invece disegna la conferma dal server.
   Raccomandazione: lasciare così.
2. **Orologio avanti:** la scadenza non ha la guardia `MargineOrologio` della conservazione dell'audit; un apparecchio
   avviato prima della sincronizzazione dell'ora potrebbe cancellare dettati più giovani di 12 mesi. Lo standalone non
   ce l'ha. Raccomandazione: una D se la si vuole come l'audit.
3. **Dopo un import:** la scadenza non aspetta il backup verificato (domanda aperta 20). Danno minimo, da tenere a mente
   al cutover.
