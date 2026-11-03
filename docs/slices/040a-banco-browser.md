# 040a — Livello browser del banco a scatola chiusa (T4.3)

- **Fonte:** PIANO §4.3 («Browser: Playwright con Chromium a versione fissata, come lo standalone,
  solo per ciò che ha bisogno di JavaScript»); via del 31/10/2026 (il livello browser con la prima
  funzione con JavaScript). La 040 è divisa: 040a il livello browser con le funzioni JavaScript che
  il Go ha già, 040b la coda delle foto della nuova busta (RIP-H-40) con anteprima e barra di
  avanzamento.
- **Voci (con l'esito del banco sui dati importati):**
  - chiuse RIP-S-28 (visualizzatore) e RIP-S-32 (conferma di «Elimina foto»);
  - parziale RIP-S-14: la conferma di «Riapri» è provata, resta l'email di pronta (T4.7);
  - parziale RIP-S-29: anteprima e barra di avanzamento mancano nel Go (040b) e la domanda 18.
- **Lavoro:** implementazione e due giri di correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco completo rilanciati dalla sessione principale.

## Cosa c'è

- **Immagine** `grgo-banco-browser:1.58.2` da `tools/banco/browser/` (Dockerfile, `package-lock.json`
  con le impronte sha512 copiate dallo standalone, `npm ci --ignore-scripts`), base
  `mcr.microsoft.com/playwright:v1.58.2-noble` fissata per digest, la stessa dello standalone;
  `libnss3-tools` fissato alla versione di `libnss3` della base. Si costruisce con
  `banco immagine-browser`, che mette l'impronta dei file nell'etichetta `grgo.banco.impronta`; un
  giro con un'immagine vecchia si rifiuta. Comandi e digest in `docs/SVILUPPO.md`.
- **Esecuzione:** `banco esegui` fa il livello HTTP e poi il container `grgo-<nome>-browser` sulla
  rete interna dell'istanza: nessuna porta, utente 1000, `cap-drop ALL`, sola lettura, seccomp e
  sandbox di Chromium, `--pull never`, niente `DEBUG`, solo le credenziali del titolare sintetico da
  file 0600, richieste fuori origine e service worker bloccati, redirect controllati come
  `activity-087.mjs` dello standalone. Lo script `banco.mjs` si monta in sola lettura e passa da
  `node --check`. Il confronto lo fa `banco` sull'host, con le regole del livello HTTP: ogni
  differenza è rossa se non è una D-xx approvata (`"livello":"browser"`). Viste desktop 1440×1000 e
  telefono 390×844.
- **Esito:** `esito.json` ha `dati`, `non_provati`, il livello browser e `verde` per i due livelli
  insieme; finché il browser non finisce vale «non eseguito», non verde. Un secondo giro sulla stessa
  istanza si rifiuta (il browser cambia i dati).
- **`prova_con` (correzione ALTA della review):** uno scenario dichiara con quali dati prova
  qualcosa; con altri dati gira e si confronta, ma esce «non provato» e la copertura non lo conta.
  Prima un giro su database vuoto chiudeva 14 voci senza provarle (CLI-23, RIP-S-01, 02, 05, 06, 08,
  09, 12, 15, 18, 24, 28, 32, 35). Con l'esito vuoto ora si chiudono 40 voci, con quello dei dati
  importati 54.
- **Scenari** (`bench/browser/`):
  - `visualizzatore`: apertura, frecce, tasti, Esc, dito sul telefono, contatore, cronologia, foto
    caricata davvero;
  - `conferme`: «Riapri busta» e «Elimina foto», con Annulla e con OK;
  - `caricamento`: invio automatico, «Foto caricata.» che sparisce dopo circa 3 s misurati nella
    pagina, un messaggio successivo che resta, 3 foto (2 salvate), foto oltre 25 MiB, finestre di
    dialogo registrate.
  In ogni scenario errori JavaScript, risposte 4xx/5xx e richieste fuori origine devono essere zero.
- **Da decidere nel banco** (coppia di valori fissata: un valore diverso è rosso): anteprima e barra
  di avanzamento (040b); «Massimo 2 foto per volta.» che nello standalone sparisce dopo circa 0,2 s
  (domanda 18).

## Difetto dello standalone trovato

500 «database is locked» sulle miniature mentre si caricano foto (nota 4 per lo standalone in
`docs/domande-aperte.md`, con la prova). Nel banco il campo `pagina.risposte-errore` di `caricamento`
è solo sul Go (`solo_go`): per lo standalone esce una riga `NOTA`, non un rosso. Gli errori di
console «Failed to load resource» non contano fra gli errori JavaScript, perché la stessa risposta la
conta già `pagina.risposte-errore`.

## Prove

- **TDD:** rosso prima del codice per `browser_test.go`, le deviazioni del browser, `prova_con`,
  l'orchestrazione e `solo_go` (non compilavano o fallivano); il test della copertura ha come rosso la
  mutazione uccisa.
- **Mutazioni:** implementazione 21 + 47, tutte uccise tranne una equivalente (`inA || inB`). Review
  44 che compilano: 29 uccise, 2 equivalenti, 13 coperte nel giro delle correzioni.
- **Prova negativa:** Go con `app.js` che ignora «Annulla»: HTTP verde, browser 80 differenze
  inattese, banco rosso.
- **Banco** (`fab-220`) completo, rilanciato dalla sessione principale con i binari finali: verde con
  `-dati sintetici-import` e su database vuoto, HTTP 24 scenari e 340 passi, browser 3 scenari e 6
  viste, «AVANZI: nessuno».
- **Copertura:** senza banco chiuse 26; con l'esito dei dati importati chiuse 54, parziali 18; con
  l'esito vuoto 40.

## Review indipendente

- **ALTA:** un giro su database vuoto chiudeva voci senza eseguire JavaScript → `prova_con` e `dati`
  nell'esito, test in `tools/copertura`.
- **MEDIA**, tutte corrette: esito verde se il browser cade prima di finire; «da decidere» con
  qualunque coppia; errori della pagina e risposte fallite non osservati; orchestrazione senza test;
  margine dei 3 s misurato fuori dalla pagina.
- **BASSA**, corrette: cartella di scrittura del container, errori generali, impronta dell'immagine e
  `--pull never`, `node --check`, secondo giro rifiutato, D-xx per livello, finestre di dialogo, test
  mancanti.

## Da sapere

- Ogni istanza del banco con il browser dura circa 2 minuti in più, e il browser va rifatto su
  un'istanza nuova.
- Le due immagini Docker (`grgo-banco-browser:1.58.2` e la base Playwright, circa 3,4 GB ciascuna)
  restano su pc-fisso: servono al banco.
- `scripts/check` è uscito due volte rosso nel primo giro dei test e tutto verde nel secondo, che
  stampava solo l'esito leggibile: il test caduto non si vedeva. Lanciati da soli, 5 giri su 5 verdi.
  Sospettato `TestAutenticaACostoPieno` (tempi sotto carico, visto cadere da solo nella 038).
  `scripts/check` ora stampa i test caduti nel primo giro e le loro righe d'errore prima di
  rilanciarli: alla prossima caduta la causa resta scritta.
