# 044b — Coda delle foto in background e 4 foto per volta (T4.3, D-58, D-59)

- **Fonte:** richiesta di Stefano del 03/11/2026 («premuto "Genera busta" devo poter continuare a
  lavorare dove voglio»), disegno approvato (messaggio al coordinatore del 03/11/2026), D-58 e D-59
  nell'inventario; ripresa `docs/handoff/2026-11-03-fine-044.md`.
- **Voci:** RIP-H-40 (coda in background, D-58; 4 per fonte, D-59), RIP-S-29 (4 foto per volta dalla
  scheda, D-59), RIP-S-35 (identificativo degli invii, nessun doppione).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; `scripts/check`, banco e copertura rilanciati dalla sessione principale con i binari
  ricompilati (sezione «Giro finale»).
- **Nello stesso commit:** le risposte di Stefano del 03/11/2026 alle domande 25-27 (D-60, D-61,
  D-62), D-63 (campo di ricerca unico) e D-64 (il numero visibile è quello dell'etichetta) con lo
  studio `docs/studio-D64-numero-etichetta.md`; righe del PIANO aggiornate (T4.2, T4.4…T4.7, «Idee
  per l'interfaccia», emoji fra le idee future); `docs/domande-aperte.md`.

## Cosa c'è

- **Coda in IndexedDB** (`internal/web/statici/app.js`): base `gestrip-coda-foto`, magazzini `voci`
  (stato, utente, scheda del browser, chiave del modulo, busta, posto) e `file` (le foto, a parte).
  Ogni foto scelta è subito una voce con un identificativo casuale (16 byte, base64url); un modulo
  ridisegnato con la stessa chiave la ritrova (avviso del QR, salvataggio rifiutato), uno con un'altra
  chiave della stessa scheda la toglie e lo dice («Le foto scelte prima sono state tolte…»), come lo
  standalone. «Genera busta» aspetta le scritture non finite e poi parte come un invio normale.
  L'avviso della busta salvata si annota subito in `sessionStorage` e poi le voci passano in attesa.
- **Invio da ogni pagina interna** (quelle con `#photo-queue`): una foto alla volta, nell'ordine delle
  buste, solo le voci dell'utente della pagina (`<meta name="gestrip-utente">`); una sola scheda
  manda (Web Locks, ripiego con un affitto in `localStorage`), le altre ridisegnano la striscia
  (BroadcastChannel). Errori 413, 415, 422 definitivi (la riga resta con il numero della busta
  finché la si chiude con ✕); gli altri, 404 compreso come nello standalone, si riprovano (D-60).
- **Nessuna conferma all'uscita.** Senza IndexedDB (o se non scrive) la voce resta in memoria: solo
  per quelle restano la conferma prima di uscire e il giro lista ↔ «Nuova riparazione» senza
  ricaricare della 040b. `mostraPagina` resta per il caricamento dalla scheda (segno monouso).
- **Striscia:** gli stessi testi di prima («📷 Busta 12/26: 1/2 foto, 50%»), ora sotto la barra con il
  menu (z-index 35 invece di 59): resta su ogni pagina e sul telefono copriva le ultime voci del menu.
- **Server:** `POST /repairs/{id}/photos` legge `x-upload-id` (`invioCoda`; un valore malformato si
  ignora) e chiama `buste.AllegaFotoConInvio`: sotto il lucchetto dell'archivio cerca la traccia in
  `invii_foto` (migrazione `0015_invii_foto.sql`) e, se c'è, risponde 201 con la foto già salvata,
  senza righe, audit né miniature; altrimenti allega e scrive la traccia nella stessa transazione.
  Traccia legata alla foto (`foto_id` annullabile, `ON DELETE SET NULL`: review B1), tabella in `TabelleSoloGo`, chiavi esterne in
  `EsterneSoloGo` (nuovo), indice in `IndiciSoloGo`.
- **D-59:** `buste.MaxFotoPerVolta = 4`, «Massimo 4 foto per volta.», `PER_SOURCE` e `MAX_FOTO` a 4,
  frasi d'aiuto di «Nuova riparazione» e della scheda; `limiteCaricamento = 4 × 25 MiB + 1 MiB`
  (Fotocamera e Galleria sono due moduli con un campo ciascuno: un invio della pagina ha un campo
  solo, e 4 foto da 25 MiB passano, `TestCaricamentoQuattroFotoDa25MiB`). `imaging.MaxFigli` invariato.

## Differenze, dichiarate

- D-58: la coda sopravvive a ricarica e navigazione, nessuna conferma all'uscita, nessun doppione;
  i tentativi ripartono da soli (D-60: 5 invii in tutto, attese 5, 15, 30 e 60 s, poi «Riprova»); le
  voci scadono da sole (D-62: in attesa o rifiutate dopo 14 giorni, scelte e mai salvate dopo 24 ore).
- D-59: 4 foto per fonte. Con 5 foto dalla scheda l'avviso resta dalle due parti: D-54 (l'avviso che
  nello standalone sparisce dopo 0,2 s con 3 foto) non si vede più nel banco e le sue righe sono uscite
  da `bench/deviazioni.json`.
- La striscia sta sotto la barra del menu (D-61: lo standalone la mette sopra e copre il menu).

## Prove

- **TDD:** rosso prima del codice (`to-be-deleted-after-use/slice-044b/prove/rosso-1.txt`): 10 test
  web e 1 di dominio.
- **Banco** (`fab-220`): verde con `-dati sintetici-import` (HTTP 32 scenari e 493 passi, 21 attese;
  browser 5 scenari e 10 viste, 22 attese) e su database vuoto (HTTP 6 attese, 17 non provati; browser 5 non provati); scenario nuovo
  `sfondo`. Container del browser con anche le credenziali sintetiche del dipendente.
- **Prove negative** (Go con `app.js` mutato, HTTP ridotto, browser completo): senza `x-upload-id`
  doppioni ovunque (sfondo: 7 foto invece di 4 navigando, 3 invece di 2 dopo la ricarica, 2 e 4
  invece di 1 e 2; coda: 4 invece di 2), banco rosso; coda solo in memoria (IndexedDB spento): foto
  perse (1, 1, 1, 0 invece di 4, 2, 2, 1) e quattro `beforeunload`, banco rosso.
- **`scripts/check`:** verde (il primo giro si è fermato su gofmt di `tools/banco/browser_test.go`).

## Da sapere

- D-60 elenca anche un'attesa di 2 min, ma con il fermo al 5° errore di fila non scatta mai: il codice
  fa 5 invii (attese 5, 15, 30, 60 s). Se Stefano vuole 5 ripetizioni oltre il primo invio basta
  allungare `RETRY_MS` e il fermo (segnalato al coordinatore).
- L'attesa dopo un invio interrotto dall'uscita, se la pagina non fa in tempo a scrivere, è ragionata
  e non misurata dal banco.
- Il «🏠 Lista» del Registro coperto sul telefono, trovato dal banco, è corretto nella review.

## Review indipendente

Nessuna ALTA. Correzioni, con test e scenari:

- **404:** torna da riprovare come lo standalone (`PERMANENT = [413, 415, 422]`): era un errore del
  brief.
- **M2, scheda duplicata:** una pagina non toglie le foto scelte di un modulo che un'altra pagina
  aperta tiene ancora (lo chiede sul canale: «who»/«mine»); «Genera busta» annota la chiave del
  modulo mandato e l'avviso cambia solo le voci di quel modulo, in una transazione: rifatto da una
  scheda duplicata non trova più niente. Scenario `schede` (duplicata con il `sessionStorage` copiato).
- **M3, tentativi:** numero, momento del prossimo ed errore stanno nella voce in IndexedDB (valgono per
  ogni pagina e scheda); attese 5, 15, 30, 60 s; dopo 5 errori di fila la coda si ferma fino a
  «Riprova». Una risposta del server arrivata mentre la pagina se ne va conta; un invio interrotto
  dall'uscita no. Scenario `tentativi` (503 fisso, 125 s di pagine che cambiano: 5 invii alle loro
  attese, nessuno dopo). D-60.
- **M4:** le voci in attesa o rifiutate scadono dopo 14 giorni (`QUEUE_MAX_MS`), le scelte dopo un
  giorno; anche una voce scaduta arrivata dopo l'inizio della pagina non si manda. Passo `scadenza`
  di `sfondo`.
- **B1:** `foto_id` annullabile con `ON DELETE SET NULL`: una foto tolta dalla scheda non torna con
  un nuovo invio dello stesso identificativo, che risponde 201 `{"ok":true,"photo_id":null}`.
- **B2-B6:** l'avviso in una transazione; «Rimuovi» fra le scritture che «Genera busta» aspetta; un
  errore di IndexedDB nel leggere il file si riprova (non è più «foto non più disponibile»);
  `navigator.storage.persist()` quando una foto entra in coda; «Genera busta» aspetta al più 5 s,
  poi le foto non scritte restano in memoria come nel ripiego.
- **B7, B8:** riga nel registro per un `x-upload-id` malformato (senza il valore); commento sul ramo
  «già salvata» (cestino e sessione).
- **B9:** test della prima intestazione, della traccia che non si scrive (niente foto, 500), della
  chiave primaria e del CHECK fuori dal lucchetto; testi della scheda letterali; `IndiciSoloGo`
  controllato anche al contrario; nomi vecchi.
- **B10:** `salva-2.striscia` letta nella pagina nello stesso istante in cui compare «Riprova», con i
  tentativi da soli trattenuti; `naviga` manda la prima foto al server e cambia pagina prima della
  risposta, come `ricarica`.
- **Registro sul telefono:** la tabella scorre nel suo riquadro (`tabella-scorrevole`, come
  `overflow-x-auto` dello standalone) e non allarga più la pagina sotto «🏠 Lista»; `sfondo` torna
  alla lista da lì.
- **Mutazioni vive della review:** J6 (senza lucchetto) uccisa da `schede` (`due-schede.invii` e
  `doppi`); J7 (tentativi senza attesa) da `tentativi` (`errore.attese`); J8 (`innerHTML`) da
  `TestCodaNelloScript`; J10 (scelte che non scadono) da `sfondo` (`scadenza.*`).

### Prove della review

- **Rosso lato server:** `TestCodaFotoTracciaSoloDegliInviiRiusciti` (500 invece di 201 con
  `photo_id` null) e `TestCodaFotoIdentificativoMalformatoIgnorato` (nessuna riga nel registro), log in
  `to-be-deleted-after-use/slice-044b/prove/rosso-review-server.txt`.
- **Rosso lato browser:** il Go di prima della review (`app.js` e Registro della prima bozza) con gli
  scenari `schede`, `tentativi`, `sfondo` e `coda`: 80 differenze inattese e 3 errori (la scheda
  duplicata toglie le foto della prima; i tentativi ripartono a ogni pagina; le voci vecchie partono;
  sul telefono «🏠 Lista» del Registro coperto).
- **Mutazioni rilanciate sul codice corretto:** J6 rossa per `schede` (3 invii invece di 2, 1 doppio),
  J7 per `tentativi` (`errore.attese`: «0 s, 0 s, 0 s, 0 s»), J10 per `sfondo` (`scadenza.inviate`
  1, la voce vecchia nella striscia), J8 per `TestCodaNelloScript`.
- **Banco** (`fab-220`, istanze nuove): dati importati HTTP 32 scenari e 493 passi (21 attese, 0
  inattese), browser 7 scenari e 14 viste (22 attese, 0 inattese); database vuoto HTTP 6 attese e 17
  non provati, browser 0 differenze e 7 non provati. Verde.
- **`scripts/check`:** verde.

## Giro finale (sessione principale)

Binari ricompilati, istanze nuove, `fab-220`:
- **`scripts/check`:** verde al primo giro, nessun test caduto.
- **Dati importati:** HTTP 32 scenari e 493 passi, 21 attese, 0 inattese. Browser 7 scenari e 14
  viste, 22 attese e **2 inattese**, entrambe in `conferme[0]`: è lo **standalone** che risponde una
  volta `500 GET /repairs/20/photos/6`, il Go no. Lo scenario `conferme` rifatto da solo su
  un'istanza nuova è verde (HTTP 32/493, 21 attese; browser 1 scenario, 2 viste, 0 differenze).
  Passeggero dello standalone, come il 502 della 043.
- **Database vuoto:** HTTP 6 attese, 0 inattese, 17 non provati; browser 0 differenze, 7 non provati.
  Verde.
- **Copertura** (353 voci): senza il banco chiuse 27, rosse 0, parziali 52, aperte 263, fuori v1 11;
  con l'esito del giro completo su dati importati chiuse 60, rosse 0, parziali 19, aperte 263, fuori
  v1 11. Una voce in meno rispetto alla 044 (61) per lo scenario `conferme` segnato rosso dal 500
  dello standalone.
- `banco controlla`: nessun avanzo.
