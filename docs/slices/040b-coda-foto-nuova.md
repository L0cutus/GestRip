# 040b — Coda delle foto della nuova busta, anteprima e barra della scheda (T4.3)

- **Fonte:** T4.3 del PIANO; piano delle slice nella ricevuta della 036 (040), divisa in 040a
  (livello browser del banco) e 040b.
- **Voci (con l'esito del banco sui dati importati):**
  - chiuse RIP-H-40 (foto scelte prima di generare la busta) e RIP-H-30;
  - parziale RIP-S-29 solo per la domanda 18 (messaggio delle 3 foto); anteprima e barra di
    avanzamento ora ci sono e il banco le confronta come attese normali;
  - parziale RIP-H-39: «📷 Scansiona» (`TestNuovaScansiona`, saltato).
- **Lavoro:** implementazione e un giro di correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco completo rilanciati dalla sessione principale.

## Cosa c'è

- **Pagina «Nuova riparazione»:** sezione `#intake-photos` con «Fotocamera» e «Galleria», nascosta
  senza JavaScript. Striscia `#photo-queue` nelle pagine interne.
- **Chiave del modulo:** `GET /nuova` crea una chiave casuale (16 byte, base64url), su
  `data-intake-key` e nel campo `intake_key`; resta uguale dopo un rifiuto o l'avviso del QR, è nuova
  per ogni busta; malformata → nuova. `POST /nuova` legge `intake_photos` come `Integer.parse`;
  se è positivo l'esito monouso della busta salvata (legato a utente e sessione, 5 minuti) porta id,
  numero e chiave, e la home lo disegna una volta in `#intake-photos-saved`. Il browser manda le
  foto solo se la chiave dell'avviso è quella del modulo da cui le ha scelte; altrimenti le toglie
  con «Le foto scelte prima sono state tolte…». La chiave non va in indirizzi, registro o audit.
  Nello standalone sta nel socket; nel Go torna dal browser: chi la manda è già dentro e potrebbe
  caricare foto comunque.
- **Coda in `app.js`:** `photo_queue.js` riga per riga (testi, 2 per fonte, 25 MiB, tipi, XHR una
  foto alla volta alla rotta della 039 con `x-csrf-token` e `x-file-name`, 413/415/422 definitivi e
  il resto con «Riprova», come `PERMANENT` dello standalone: 404 non è definitivo; barra, ✕,
  conferma prima di uscire solo con foto in coda).
- **Cambio pagina senza ricarica:** finché ci sono foto scelte o in coda, i link a `/` e `/nuova` e
  «Genera busta» passano da fetch e il body nuovo prende il posto del vecchio, con la cronologia
  gestita (un cambio del solo hash non fa niente). Una risposta di login o senza `#photo-queue` fa
  una navigazione normale. Fuori da questo giro le pagine si ricaricano (domanda 19).
- **Scheda (RIP-S-29):** il modulo parte con XHR: anteprima `blob:` e «nome · Caricamento foto: N%»
  con la barra; poi la pagina della risposta prende il posto di quella attuale, senza una seconda
  GET (il segno `mostra` è monouso). Durante l'invio i campi file sono spenti. Una risposta d'errore
  (403, 413, 500) riapre la scheda con una navigazione normale. Senza JavaScript tutto come la 038.

## Differenze, dichiarate

- Confine della navigazione (domanda 19).
- Senza JavaScript la sezione delle foto della nuova busta è nascosta.
- Salvataggio con fetch che non arriva al server: «Connessione assente» (il testo del client-error
  di LiveView nello standalone), pulsante riattivato, foto tenute.
- Scheda: le righe di avanzamento solo per le foto che partono (LiveView le mostra anche per le voci
  rifiutate finché non le annulla); una scelta durante un invio non si accoda (campi spenti); una
  risposta d'errore riapre la scheda invece di mostrare la pagina d'errore.
- Indirizzo dopo il salvataggio `/?esito=…` invece di `/`.

## Prove

- **TDD:** 9 test web nuovi rossi sugli stub; `TestScenariBrowserDelRepo` rosso con i JSON di prima;
  nel giro delle correzioni `TestCambioPaginaNelloScript` rosso sull'`app.js` di prima.
- **Mutazioni:** implementazione 26, 25 uccise e 1 equivalente. Review 32: 23 uccise; delle 9 vive in
  `app.js`, 3 coperte dallo scenario, 1 equivalente, le altre (chiave, seconda GET, popstate) coperte
  nel giro delle correzioni e uccise dal banco (prove negative).
- **Prove negative** (Go con `app.js` mutato): foto passate al modulo nuovo (16 differenze),
  `same = false` (108), chiave dell'avviso ignorata, `location.assign` dopo il caricamento (98):
  banco rosso in tutti i casi.
- **Scenario `coda`:** due buste di fila con le foto che salgono mentre si compila la seconda, 422
  definitivo, connessione interrotta e «Riprova», conferma prima di uscire, foto tolte cambiando
  modulo, salvataggio rifiutato (QR vuoto, nome vuoto) senza invii, avviso manomesso senza invii.
  `caricamento`: dopo un caricamento il visualizzatore non ricarica la pagina e nessuna GET in più
  della scheda.
- **Banco** (`fab-220`) completo, rilanciato dalla sessione principale: verde con
  `-dati sintetici-import` e su database vuoto, HTTP 24 scenari e 340 passi, browser 4 scenari e 8
  viste, «AVANZI: nessuno».
- **Copertura:** con l'esito dei dati importati chiuse 56, parziali 17; senza banco 26.

## Review indipendente

- **ALTA**, corretta: dopo un caricamento dalla scheda ogni salto d'ancora (miniatura, frecce,
  «Indietro») ricaricava la pagina con il segno consumato, quindi un'apertura vera in più nel
  registro (D-44) e il visualizzatore rotto.
- **MEDIA**, corrette: il banco non provava la chiave con il salvataggio rifiutato né con l'avviso
  manomesso; non provava che dopo il caricamento non c'è una seconda GET. Il confine della
  navigazione è la domanda 19.
- **BASSA**, corrette: risposte d'errore del caricamento XHR, sessione scaduta nel giro, scelta
  durante un invio, documenti del banco, fuoco dopo il cambio di pagina.

## Da sapere

- Chromium segue da solo i redirect di fetch e XHR senza ripassare dalle rotte di Playwright: le
  modifiche alle risposte nel banco (`ctx.trasforma`) seguono il redirect da sé. Vale anche per i
  controlli della 040a sulle richieste: il controllo su `Location` resta, la GET seguita no.
