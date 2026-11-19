# 098 — Pagina «Promemoria e job» (T4.9, tredicesima)

- **Fonte:** standalone a `go-baseline`: il LiveView della pagina dei job (`jobs_live.ex`), `jobs/config.ex` (`put/get`,
  cast, `validate_hour`, soglie), `jobs.ex` (`history`, `last_runs`, esecuzione a mano); test `jobs_live_test.exs`.
  Inventario JOB-05, JOB-06; D-16, D-17/D-102. Piano della T4.9, domande A e B.
- **Voci:** JOB-05 e JOB-06 (parziali fino all'esito del banco, come sempre). Nessuna migrazione, nessuna D nuova.

## Cosa c'è

- **`internal/jobs/pagina.go`** (nuovo): `LeggiValori`, `Pianificatore.SalvaConfig` (solo le chiavi cambiate più
  `jobs.configured`, soglie validate solo se cambiate, ora validata anche quella salvata, come lo standalone),
  `Pianificatore.Pagina` (stato dei tre lavori, ultima esecuzione, ultime 20 righe), etichette e testi degli errori.
- **`internal/web/promemoria.go`** e **`pagine/promemoria.html`** (nuovi): `/settings/promemoria` solo titolare, con
  CSRF; modulo delle impostazioni (attivo, ora, soglie dei tre lavori, sette caselle dei giorni di apertura) e un
  modulo «▶ Esegui adesso» per lavoro, con `data-invio-unico` (un doppio tocco non fa due esecuzioni).
- **`cmd/gestrip/main.go`:** il pianificatore si crea sempre e va alla pagina (`Dipendenze.Lavori`);
  `-senza-lavori-automatici` ferma solo il giro al minuto, «Esegui adesso» resta possibile.
- **`internal/buste/solleciti.go`:** `SogliaSollecitiCon` (la soglia dei solleciti letta da una configurazione data).
- **Guscio:** scheda e titolo «Promemoria e job · Laboratorio».
- **Banco:** `bench/scenari/promemoria.json` (due app), `promemoria-dipendente.json` (rimando a / nelle due app),
  `telegram-promemoria.json` (solo Go con Telegram acceso, dopo `telegram-acceso`); tre righe in
  `bench/deviazioni.json` (due D-16, una D-102).
- **SVILUPPO:** sezione «Pagina Promemoria e job (slice 098, JOB-05, JOB-06)», una riga in «Rotte», la riga del
  pianificatore aggiornata.

## Scelte

1. **Resa D-16 (domanda B):** senza canali la pagina si apre e le impostazioni si salvano, con una riga che dice che i
   lavori partono solo con email o Telegram attivi; niente «Esegui adesso» né righe «Saltato»; le righe importate
   restano. Il briefing con la sola email risulta spento.
2. **Ordine dei controlli a mano** (Dubbio 6 della 094): prima i canali, poi l'interruttore. Senza canali un lavoro
   spento non lascia la riga «Saltato» (lo standalone sì); dichiarato nella resa D-16.
3. **Giorni di apertura (D-102):** chiave solo Go `jobs.giorni_apertura`, default martedì–sabato, modulo rifiutato
   senza nessun giorno («Scegli almeno un giorno di apertura.»). Il piano diceva «lun–sab»: vale la D-102.
4. **Ora più stretta dello standalone anche al salvataggio:** solo `HH:MM` e `HH:MM:SS`; per esempio `07:45:00.5` è
   rifiutato, lo standalone lo accetta (Dubbio 3 della 094, deciso: resta così).
5. **Intestazione come lo standalone**, seconda frase compresa («Se non hai configurato nessun canale di invio, un job
   non manda niente e lo dice qui sotto invece di provarci.»), aggiunta dopo la revisione.

## Prove

- Rosso con gli stub (`to-be-deleted-after-use/slice-098/prove/rosso.txt`): 8 test di `jobs` e 7 di `web`.
- Test nuovi in `internal/jobs/pagina_test.go` (valori, salvataggio, 19+ casi di rifiuto, «True», 365, `07:45:00.5`,
  ora salvata illeggibile, giorni, stato, guasto), `internal/web/promemoria_test.go` (pagina, dipendente, salva,
  rifiuti, «Esegui adesso» con `data-invio-unico` e token CSRF, senza canali, storico, `jobs[]`, «forse»),
  `cmd/gestrip/lavori_test.go` (`TestServePianificatore` con e senza `-senza-lavori-automatici`).
- `scripts/check` verde, rilanciato dalla sessione principale prima e dopo le correzioni.
- **Mutazioni dell'implementatore:** 51 scritte, 49 compilano, 43 uccise; poi 11 su 11 uccise dopo le correzioni
  (w11, prima viva, ora uccisa). Restano equivalenti: m12, w03, w05 (`WithoutCancel`), w16 (ordine della mappa).
- **Revisione indipendente (Opus):** 0 ALTE; 2 MEDIE chiuse (`data-invio-unico` su «Esegui adesso»; seconda frase
  dell'intestazione mancante); BASSE chiuse con test (CSRF di `#jobs-run`, pianificatore passato alla pagina con
  `-senza-lavori-automatici`, «True», 365, `jobs[]`, «forse»); BASSE lasciate: `WithoutCancel` e spegnimento in 10 s
  (rischio teorico, i lavori accodano soltanto), un solo errore sull'ora con cast fallito e ora salvata illeggibile.
  Mutazioni del revisore: 42 scritte, 38 compilano, 28 uccise, 2 equivalenti, 8 vive poi coperte dalle correzioni.

## Banco

Scenari nuovi: `promemoria` (due app: pagina, campi, D-16 e D-102 dichiarate; passi solo Go per «Esegui adesso»
senza canali, ora non valida, nessun giorno, salvataggio e ripristino), `promemoria-dipendente` (rimando a / nelle due
app), `telegram-promemoria` (solo Go con Telegram acceso: «Esegui adesso» dei tre lavori e testi in `/_prova/mandati`;
il briefing si accende e torna spento). Non confrontati perché instabili: lo storico e «Ultima esecuzione» di ritardi e
solleciti dello standalone (le righe «Saltato» dipendono dall'ora del giro), il numero delle buste in ritardo e il
testo esatto dei messaggi (dipendono dalla data), il briefing nelle due app (lo standalone del banco non ha il bot).

**Giro s098a (19/11/2026): rosso per la nota 8**, non per la 098: HTTP 86 scenari, 1347 passi, 0 inattese; browser
2 errori e 40 inattese, tutte dello standalone (vista telefono, `storico-correzioni` e `testi-invii`, `TimeoutError`;
nel suo log `Database busy` e `database is locked`; ottava e nona comparsa in `docs/domande-aperte.md`).
**Giro identico s098b (ok del coordinatore): VERDE.** HTTP 86 scenari, 1347 passi, 0 inattese, 0 errori; browser 29
scenari, 58 viste, 0 inattese, 0 errori. Memoria disponibile minima 23,9 GB. Copertura con l'esito: 353 voci, chiuse
213 (JOB-05, JOB-06), rosse 0, parziali 16, aperte 113, fuori v1 11.

## Per il manuale

- In Impostazioni → «Promemoria e job» il titolare sceglie per ogni promemoria (buste in ritardo, solleciti
  all'artigiano, briefing del mattino) se è attivo, a che ora parte (ora d'Italia) e dopo quanti giorni.
- Le sette caselle dicono in quali giorni i promemoria partono da soli: di default da martedì a sabato; almeno un
  giorno va spuntato.
- «▶ Esegui adesso» fa partire subito un promemoria, anche in un giorno chiuso; se il promemoria è spento risulta
  «Saltato» e non conta come esecuzione del giorno.
- Senza email o Telegram attivi la pagina lo dice e non mostra «Esegui adesso»; le impostazioni si salvano lo
  stesso. Il briefing vuole Telegram.
- In fondo alla pagina ci sono le ultime 20 esecuzioni, con l'esito e quante buste e messaggi.
- Il dipendente non vede la pagina.

## Dubbi

1. **Testi solo Go inventati** (raccomandazione: approvarli o correggerli e scriverli nella resa D-16 / D-102): la
   riga senza canali, «Il briefing parte solo con Telegram attivo.», «Niente è stato eseguito: …», «Scegli almeno un
   giorno di apertura.», «Non è stato possibile eseguire il job: riprova.», i nomi dei giorni da «Lunedì» a
   «Domenica». Nessuna D nuova.
2. **Ora non valida e salvataggio confrontati solo nel Go:** nello standalone sono eventi LiveView e via HTTP non si
   confrontano. Raccomandazione: un confronto vero con uno scenario browser quando servirà.
3. **`giorni_apertura` nel `changed` di `jobs.configured`** (campo solo Go). Raccomandazione: tenerlo.
4. **Modulo senza la mappa `jobs`:** il Go risponde «Richiesta non riconosciuta…» come nella 087; lo standalone non fa
   niente. Raccomandazione: tenerlo, coerente con le altre pagine.
5. **Un'ora salvata illeggibile blocca ogni salvataggio** finché non la si riscrive, come nello standalone.
   Raccomandazione: metterlo tra i difetti non gravi dello standalone, senza correggerlo nel Go.
