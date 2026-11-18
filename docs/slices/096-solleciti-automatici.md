# 096 — Lavoro «Solleciti artigiano» (T4.9, undicesima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/jobs/artisan_chases.ex` (`run/4`,
  `candidates/2`, `claim/2`, `enqueue!/4`, `content/2`, `line/1`, `bags/1`, `unwrap/1`),
  `workshop/artisan_reminders.ex` (`late_query/3`, `threshold_days/1`), `jobs/config.ex` (`chases_days` letto da
  `ArtisanReminders.threshold_days(%{})`), `jobs.ex` (`perform/5`), `outgoing.ex` (`enqueue/3`); test
  `jobs_test.exs` («the artisan chase job»). Inventario JOB-09; D-16, D-17/D-102, D-64/D-82; decisione di Stefano del
  15/11/2026 sulle buste nel cestino (Dubbio 2 della 083).
- **Voci:** JOB-09 (parziale fino all'esito del banco, come sempre). Nessuna migrazione: `sollecito_avvisato_il` c'è
  dalla 0007 e l'import lo porta (`chase_notified_at`). Nessuna D nuova.

## Cosa c'è

- **`internal/jobs/solleciti.go`** (nuovo): `LavoroSolleciti` (`NuoviSolleciti(d, coda)`, `ConRegistro`), il
  `jobs.Lavoro` dei solleciti; usa l'`Accodatore` della 095.
- **`internal/buste/solleciti_giro.go`** (nuovo): `TimbraSollecitiDelGiro(ctx, tx, adesso)` = `candidates/2` +
  `claim/2` nella transazione del lavoro. **Riusa** la soglia e la query della 083, non le copia:
  `sogliaSolleciti` (il corpo di `Buste.soglia`, ora una funzione che legge con il `lettore` dato) e
  `busteInRitardoDove` (`busteInRitardo` con un `AND …` in più; la pagina passa `""`). `leggiImpostazione` è il corpo
  di `Buste.impostazione` per chi non ha un `Buste`.
- **`cmd/gestrip/main.go`:** `lavoriAutomatici` registra anche i solleciti; ogni lavoro ha il suo registro
  (`lavoro=ritardi`, `lavoro=solleciti`; prima il `With` era fuori, solo per i ritardi).
- **`internal/jobs/ritardi.go`** (correzione della 095): numero dei testi (Scelte, 4).
- **SVILUPPO:** sezione «Lavoro «Solleciti artigiano»» e la riga del pianificatore aggiornata.

## Per giro

`sollecito_avvisato_il` si confronta con `consegnata_artigiano_il`, l'uscita del giro in corso (come lo standalone,
`chase_notified_at < handed_over_at`): vuoto o **prima** dell'uscita → la busta si può nominare; uguale o dopo → già
nominata in questo giro. Nessun percorso azzera il timbro (né il rientro né la riapertura del giro): la nuova
consegna lo rende vecchio da sola. Quindi una busta nominata e ancora fuori non si nomina più (anche dopo 30 giorni);
rientrata e mandata di nuovo, si nomina di nuovo quando passa la soglia del giro nuovo.

## Tutto o niente

Una transazione per esecuzione (`Repo.transaction` dello standalone): elenco **dentro** la transazione, timbro di ogni
busta con la guardia del giro (`UPDATE … WHERE id AND <guardia>`, chi non timbra non entra nel riepilogo), poi un
riepilogo per destinatario con `AccodaIn` nella stessa transazione, poi il commit.

- **Rifiuto della coda** (`ErrMessaggioNonValido`, `ErrSoloAMano` → «La coda ha rifiutato il riepilogo dei
  solleciti.»; `ErrNonConfigurato` → «Il canale X non è più configurato.»): niente commit, quindi tutti i timbri e
  anche il riepilogo già accodato sull'altro canale si annullano. `Risultato{Fallito, Dettaglio: motivo}` con zero
  conteggi (`unwrap({:error, detail})` → `{:error, 0, 0, detail}`); nel registro una riga Warn con il solo motivo.
- **Altro errore** (database): il lavoro torna con l'errore, la transazione si annulla, la riga è
  `DettaglioInterrotto` (l'eccezione dello standalone esce da `Repo.transaction` e annulla lo stesso).
- **Stop:** con il contesto finito (anche se il driver lo dice con un altro errore) il lavoro torna con l'errore del
  contesto: niente riga, tutto annullato, il giorno resta da fare.
- **Senza destinatari:** `ErrSenzaCanali` subito, niente timbri (il pianificatore non lo chiama mai così, D-16).
- **Nulla da sollecitare:** «Nessuna busta oltre soglia.», esito `ok`, nessun messaggio né timbro.
- **Riuscito:** «N buste oltre soglia.» («1 busta»), avvisi = buste nominate, messaggi = destinatari.

Testo (semplice, nessun `parse_mode`): oggetto «Solleciti artigiano»; righe «🧰 Solleciti artigiano», «N buste fuori
bottega da oltre <soglia> giorni.», poi una per artigiano (per nome) «<nome>: <numero> (Ngg), …» con le buste dalla
più vecchia uscita. I giorni sono quelli della pagina: blocchi interi di 86 400 s dall'uscita. Il numero è
`buste.NumeroNeiTesti` (D-64/D-82: «0020/26» con l'etichetta, «21/26» senza); lo standalone scrive `bag_number`.

**Non registra un sollecito:** nessuna riga `solleciti_artigiani` né `repair.artisan_reminded`; la pagina continua a
dire «mai sollecitata». Il timbro non tocca `aggiornato_il` (come `update_all`).

## Scelte

1. **Lato del dominio in `buste`**, il lavoro in `jobs`: la regola «busta da sollecitare» resta una sola (pagina,
   contatore, lavoro), come `ArtisanReminders.late_query/3` che lo standalone riusa.
2. **Soglia letta dentro la transazione** (lo standalone la legge prima, in `Config.get`): nessuna differenza
   visibile, e la soglia del testo è quella usata per l'elenco.
3. **Filtro dell'elenco e guardia del timbro tutti e due**, come lo standalone, anche se nella stessa transazione di
   scrittura uno basta (vedi le mutazioni equivalenti).
4. **Numero dei testi anche nei ritardi (correzione della 095 per la D-64/D-82, nessuna D nuova).** Il riepilogo usa
   `buste.NumeroNeiTesti` («0045/26» con l'etichetta, «45/26» senza; precedente: `internal/web/telegram_avvisi.go`).
   La 095 scriveva il numero interno: ora `ritardi.go` legge anche `codice_qr` nel `RETURNING` del timbro e usa il
   numero dei testi nell'oggetto, nel testo e nei motivi del dettaglio dello storico («la coda ha rifiutato il
   messaggio della busta 0045/26», «data promessa non valida della busta 0045/26»). Test
   `TestRitardiNumeroDeiTesti` (JOB-07), rosso senza la correzione (`prove/rosso-2.txt`).

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-096/prove/rosso.txt`): con gli stub (`TimbraSollecitiDelGiro` vuota,
  `Esegui` che non fa niente) tutti i 13 `TestSolleciti*` rossi sulle asserzioni (consegne vere create, nessun
  messaggio, nessun timbro); `TestLavoriAutomatici` rosso (un solo lavoro registrato).
- **`internal/jobs/solleciti_test.go`** (database vero, consegne vere con `ApriConsegna`/`AggiungiAllaConsegna`/
  `ConfermaConsegna`/`RientraDallArtigiano`, coda vera con i provider veri, orologio finto, dati inventati), tutti
  JOB-09: `TestSollecitiBustaOltreSoglia` (testo e oggetto esatti, timbro = istante, `outgoing.queued` firmato
  `scheduler`, la busta da 3 giorni no, seconda esecuzione e giorno dopo vuoti), `TestSollecitiUnRiepilogoPerCanale`
  (due artigiani in un riepilogo, ordine, D-64, email e Telegram con lo stesso testo), `TestSollecitiSoglia` (14,
  «abc», « 7 », limite compreso al secondo, «0», soglia enorme), `TestSollecitiTimbroPerGiro` (stesso giro mai
  due volte, rientrata, giro nuovo sotto e oltre soglia, timbro uguale all'uscita), `TestSollecitiLeBusteDellaPagina`
  (cestino dentro, senza consegna fuori, artigiano della consegna), `TestSollecitiNessunSollecitoVero`,
  `TestSollecitiTuttoONiente` (tre rifiuti; riepilogo dell'email annullato; registro senza indirizzi, artigiani,
  numeri; il giro dopo nomina tutte), `TestSollecitiGuasto`, `TestSollecitiGiorniEOrologio` (uscita alle 23:30 e
  mezzanotte del negozio; cambio d'ora del 25/10: il confine è a 14·86 400 s, alle 09:00 e non alle 10:00 del muro;
  lavoro e pagina d'accordo in ogni istante), `TestSollecitiNullaDaSollecitare`, `TestSollecitiSenzaDestinatari`,
  `TestSollecitiStop` (stop durante l'accodamento e prima; riavvio: un riepilogo solo), `TestSollecitiRecuperoD17`
  (lunedì chiuso niente, martedì «(14gg)»).
- **`cmd/gestrip/lavori_test.go`:** `TestLavoriAutomatici` (ritardi e solleciti registrati, il briefing no).
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-096/mutazioni/`, `muta.py`, `esiti.txt`): 27 compilanti su
  `solleciti.go`, `solleciti_giro.go`, `busteInRitardoDove` e `main.go`; **22 uccise, 5 vive equivalenti**: guardia
  del timbro tolta (m04), filtro dell'elenco tolto (m08, m27), busta o gruppo tenuti senza timbro (m05, m25). Elenco
  e timbro stanno nella stessa transazione di scrittura, quindi filtro e guardia si coprono a vicenda.
- **Mutazioni della revisione:** 45 compilanti, 36 uccise, 5 equivalenti (le stesse m04/m05/m08/m25/m27) più j19;
  sopravvissute da chiudere e01–e03 (`leggiImpostazione`) e m02–m03 (registro dei solleciti in `lavoriAutomatici`).
- **Correzioni della revisione:** `TestLavoriAutomaticiRegistro` (`cmd/gestrip`, JOB-09: `lavoriAutomatici` con un
  registro su buffer, un riepilogo dei solleciti rifiutato da una coda senza provider, nel registro
  `lavoro=solleciti` e non `lavoro=ritardi`), `TestImpostazioneVuotaEMancante` (`internal/buste`, ART-20: salvata
  vuota → ok vero, mancante → ok falso), `TestRitardiNumeroDeiTesti` (sopra). Ricontrollo
  (`mutazioni/muta2.py`, `esiti-2.txt`, mutazioni ricostruite dalla descrizione del revisore): e01 (vuota come
  mancante), e02 (mancante come presente), e03 (ok sempre falso), m02 (`con("ritardi")`), m03 (senza `With`) e la
  correzione della 095 tolta: **6 su 6 uccise**.
- **`scripts/check`:** verde (`to-be-deleted-after-use/slice-096/prove/check.txt`; copertura: voci 353, rosse 0) e
  dopo le correzioni (`check-2.txt`).

**Misurato:** tutto quanto sopra nei test. **Solo ragionato:** due processi sullo stesso database (la transazione
non può salire a scrittura su una lettura vecchia: errore e riga «interrotto», non un doppio riepilogo); il
comportamento sul banco.

## Banco

«Si vede con la 098»: nessuno scenario nuovo; i due Go del banco partono con `-senza-lavori-automatici`.
Nota per la 098: nella cattura la 4/26 è fuori dal 06/01/2026 (consegna 1, «Laboratorio Sintetico») e il suo
`chase_notified_at` arriva dall'import: se è vuoto o di prima dell'uscita, il primo «Esegui adesso» dei solleciti la
nomina e la timbra. Il timbro non cambia la pagina dei solleciti né lo scenario `consegne-solleciti` (non lo legge).

**Giro s096a (18/11/2026): rosso per un'oscillazione del banco**, non per la 096 né per la nota 8: 8 inattese nello
scenario HTTP `firma` (ora della firma 10:38 nello standalone, 10:39 nel Go, testo uguale; nota 9 di
`docs/domande-aperte.md`); browser verde. **Giro identico s096b (ok del coordinatore): VERDE.** HTTP 83 scenari, 1314
passi, 0 inattese, 0 errori; browser 29 scenari, 58 viste, 0 inattese, 0 errori. Memoria disponibile minima 20,5 GB.
Copertura con l'esito: 353 voci, chiuse 209 (JOB-09), rosse 0, parziali 16, aperte 117, fuori v1 11.

## Per il manuale

- Ogni mattina (dalle 10, nei giorni di apertura) il programma manda al negozio, su email e su Telegram se sono attivi,
  un solo messaggio con le buste che sono dall'artigiano da troppi giorni, divise per artigiano.
- Il numero di giorni è lo stesso della pagina «Solleciti pendenti» (di default 14).
- Ogni busta compare una volta per ogni volta che va dall'artigiano: se resta fuori non la si riceve tutti i giorni;
  se torna e poi riparte, comparirà di nuovo quando avrà superato i giorni.
- Il messaggio non sollecita l'artigiano e non conta come sollecito: per sollecitarlo si usa sempre la pagina
  «Solleciti pendenti».
- Se il messaggio non si può mandare, non si segna nessuna busta: lo storico mostra «Fallito» e il controllo riprova
  (al massimo tre volte al giorno), altrimenti il giorno dopo.

## Dubbi

1. **«Giorni» della soglia = blocchi di 86 400 s, non giorni del calendario del negozio** (il rischio del piano). Lo
   standalone, la pagina 083 e il lavoro contano così: una busta uscita alle 23:30 è «oltre 14 giorni» alle 23:30 del
   quattordicesimo giorno, non alla mezzanotte; col cambio d'ora il confine si sposta di un'ora sull'orologio del muro.
   Il «quando gira» è invece sull'orologio del negozio (094). **Raccomandazione:** tenerlo così (lavoro e pagina
   devono dire le stesse buste); nessuna D. **Deciso (coordinatore, 18/11/2026):** si tiene così.
2. **«da oltre N giorni» con il limite compreso** (una busta fuori da esattamente N giorni è nominata): frase dello
   standalone, copiata. **Raccomandazione:** tenerla per la v1 e metterla fra i difetti non gravi dello standalone. **Deciso
   (coordinatore, 18/11/2026):** fra i difetti non gravi dello standalone.
3. **`ErrSoloAMano` come rifiuto** (come la 095): nello standalone `{:not_a_queue_channel, …}` non ha una clausola nel
   `case` e diventerebbe un'eccezione («interrotto»). Non succede mai (i destinatari sono solo email e Telegram).
   **Raccomandazione:** nessuna azione; resta coerente con la 095.
4. **Buste nel cestino nominate** (Dubbio 2 della 083, deciso da Stefano il 15/11/2026: come lo standalone). Il lavoro
   le nomina come la pagina. Nessuna azione.
