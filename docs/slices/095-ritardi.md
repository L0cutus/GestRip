# 095 — Lavoro «Buste in ritardo» (T4.9, decima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/jobs/overdue_repairs.ex` (`run/4`,
  `silence_history/2`, `candidates/1`, `unnamed_candidates/1`, `claim/6`, `enqueue!/4`, `content/2`, `cut/1`,
  `@backfill_days`, `@backfill_key`, `@max_description`), `jobs.ex` (`run/3`, `perform/5`: quando il lavoro è chiamato),
  `jobs/config.ex` (`overdue_days`, `days/1`, `@max_days`), `outgoing.ex` (`destination_for_shop/1`, `enqueue/3`),
  `repo.ex` (`prepare_query/3`: il cestino); test `jobs_test.exs` («the overdue bags job») e
  `jobs/overdue_reopen_test.exs` (slice 211 e 213). Inventario JOB-07, JOB-08 e le note collegate (riga dei «Decisi in
  sessione», slice 211 `9b0e144` e 213 `5c80347`), D-03, D-16, D-17, D-102.
- **Voci:** JOB-07 e JOB-08 (parziali fino all'esito del banco, come sempre). Nessuna migrazione: `ritardo_avvisato_il`
  c'è dalla 0007, la coda dalla 068. Deviazioni usate: D-16 e D-17/D-102 (attraverso il pianificatore della 094).
  Nessuna D nuova.

## Cosa c'è

- **`internal/jobs/ritardi.go`** (nuovo): `LavoroRitardi` (`NuoviRitardi(d, coda)`, `ConRegistro`), che è il
  `jobs.Lavoro` dei ritardi; `Accodatore` (la parte `AccodaIn` della coda); `GiorniStorico` (7) e `MassimoDescrizione`
  (200).
- **`internal/jobs/config.go`:** `Config.GiorniRitardi()` dalla chiave dello standalone `jobs.overdue_days`
  (`Integer.parse` del valore senza spazi ai bordi, da 1 a 365, altrimenti 1); `ChiaveStoricoSilenziato`
  (`jobs.overdue.silenced_history`) con la lettura e la scrittura nella transazione (stanno qui perché questo è già il
  file di `jobs` che tocca `impostazioni`, quello che `TestChiaviSoloGoComplete` conosce; aggiornato il commento).
- **`internal/buste/stati.go`:** `StatiChiusi()`, copia della lista degli stati chiusi, per non ripeterla nella query.
- **`cmd/gestrip/main.go`:** `lavoriAutomatici(d, coda, registro)` registra i ritardi (`registro` con `lavoro=ritardi`).
- **SVILUPPO:** sezione «Lavoro «Buste in ritardo»» e la riga del pianificatore aggiornata.

## Claim e transazione

- **Elenco** (`candidate`, `candidates/1`): gli id delle buste con `data_prevista` presente e `<=` oggi meno la soglia,
  stato non in `buste.StatiChiusi()`, `cancellata_il` NULL, `ritardo_avvisato_il` NULL, per id. «Oggi» è
  `clock.DataDi(adesso)` (Europe/Rome).
- **Timbro** (`timbra`, `claim/6`): **una transazione per busta**: `UPDATE buste SET ritardo_avvisato_il = adesso WHERE
  id = ? AND <lo stesso predicato> RETURNING numero, nome_cliente, descrizione, data_prevista`. Nessuna riga → niente
  (timbrata da un altro, chiusa, nel cestino, data tolta o spostata in avanti nel frattempo). Una riga → il testo si
  compone **dalla riga timbrata** (slice 213: data spostata su un altro giorno in ritardo → il messaggio dice il giorno
  nuovo) e si accoda un messaggio per destinatario con `AccodaIn` **nella stessa transazione** (riga e
  `outgoing.queued` firmato `scheduler` o da chi ha premuto «Esegui adesso»). Poi il commit.
- **Rifiuto della coda:** si esce senza commit, quindi timbro e messaggi già accodati della busta (anche quello
  dell'altro canale) si annullano; la busta resta da segnalare e il lavoro passa alla prossima. Motivi dello standalone:
  `ErrMessaggioNonValido`/`ErrSoloAMano` → «la coda ha rifiutato il messaggio della busta N»; `ErrNonConfigurato` →
  «il canale X non è più configurato». Esito `failed`, «<frase> N non riuscite: <primo motivo>», conteggi delle riuscite.
- **Guasto che non è un rifiuto** (database): il lavoro torna con l'errore → riga `failed` «Il job si è interrotto…»
  (094); come lo standalone, dove un'eccezione in `claim` esce da `run/4`. Le buste già timbrate restano timbrate con i
  loro messaggi, le altre al giro dopo (il giorno ha ancora tentativi).
- **Data promessa illeggibile** (correzione della revisione): la colonna accetta con il suo GLOB anche date che il
  calendario non ha (`2026-02-31`), mentre nello standalone una data così non può esistere, perché il tipo `date` di
  Ecto non la accetta. Nel Go è un rifiuto di quella busta: si annulla la sua transazione (niente timbro né
  messaggi), conta fra le non riuscite con «data promessa non valida della busta N» e si passa alla busta dopo; nel
  registro l'id della busta e il motivo fisso.
- **Senza destinatari** il lavoro torna subito con `ErrSenzaCanali`, senza silenziare né timbrare: il pianificatore
  non lo chiama mai così (D-16), ma un timbro senza messaggio sarebbe un avviso perso senza contarlo.
- Il timbro non tocca `aggiornato_il` (come `update_all`). I messaggi hanno `busta_id` NULL (`repair_id: nil` di
  `destination_for_shop`).

## Silenziamento (JOB-08)

Prima dell'elenco, in **una** transazione di scrittura: se la chiave `jobs.overdue.silenced_history` non c'è (qualunque
valore conta come presente, come `Settings.get` non nil), `UPDATE` del timbro su tutte le buste del predicato con il
confine **oggi − 7** (quindi in ritardo da 7 giorni o più), senza messaggi, e la chiave «1»; un errore a metà annulla
tutto e il giro dopo rifà tutto. La frase aggiunge « N buste storiche silenziate alla prima esecuzione.». Dopo il
commit il registro ha una riga Info («ritardi: storico silenziato alla prima esecuzione», `buste=N`, solo il numero):
se poi l'esecuzione si interrompe la riga dello storico dice solo «interrotto», e il numero non si perde.

«Prima esecuzione vera» = la prima volta che il pianificatore chiama il lavoro: con almeno un canale (D-16) e il lavoro
acceso. Verificato nello standalone: `run/3` salta il `perform` con il lavoro spento («Saltato») o senza destinatari, ma
**non distingue l'esecuzione a mano**: anche «Esegui adesso» silenzia. Il Go fa lo stesso; in più non silenzia nei
giorni chiusi (il lavoro non parte, D-17). L'import porta la chiave (deciso il 29/10): un negozio importato non
silenzia di nuovo e le buste vecchie non ancora segnalate si segnalano.

## Scelte

1. **Il lavoro sta in `internal/jobs`** (non in `buste`): il pacchetto dei lavori automatici importa `buste` solo per
   `StatiChiusi`; i test usano `buste.Crea`, `Modifica` e `TogliData` veri per la slice 211.
2. **La chiave si legge dentro la transazione di scrittura** (lo standalone la legge prima): due esecuzioni, anche di
   due processi, non silenziano due volte. Nessuna differenza visibile.
3. **Stop a metà = niente riga** (il lavoro torna con l'errore del contesto), come lo standalone fermato a metà: la
   094 permette anche un `Risultato` fallito con i conteggi, ma servirebbe una frase nuova; così il giorno resta da
   fare e dopo il riavvio il giro segnala solo le buste rimaste.
4. **Registro:** per una busta rifiutata `busta` (id) e `motivo` (le frasi fisse sopra); mai indirizzi, nomi o testi.

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-095/prove/rosso.txt`): con `ritardi.go` che non faceva niente tutti i 15
  `TestRitardi*` rossi sulle asserzioni (le buste si creano, nessun messaggio, nessun timbro, nessuna chiave).
- **`internal/jobs/ritardi_test.go`** (database vero, coda vera con i provider veri che non vanno in rete, orologio finto,
  dati inventati), marcati JOB-07 o JOB-08: `TestRitardiBustaAppenaInRitardo` (testo e oggetto esatti, timbro =
  istante, `aggiornato_il` uguale, `outgoing.queued` firmato `scheduler`, oggi/domani non in ritardo, seconda esecuzione
  vuota), `TestRitardiSoglia` (5 e 3 giorni; 15 valori della chiave), `TestRitardiStatiChiusiECestino`,
  `TestRitardiDueCanali`, `TestRitardiTesto` (200 grafemi con emoji composte, «…», descrizione vuota, 150 «è» in 300
  byte), `TestRitardiDataSpostataRiapre` (slice 211 con `Modifica` e `TogliData`), `TestRitardiTimbroRicontrolla`
  (slice 213: sei modifiche fra elenco e timbro con l'aggancio `primaDelTimbro`), `TestRitardiUnErroreNonFermaLeAltre`
  (tre rifiuti diversi; messaggio dell'altro canale annullato; registro senza indirizzi), `TestRitardiRifiutiEGuasti`
  (tutte rifiutate; guasto del database con un indirizzo inventato nell'errore → interrotto, la prima busta resta),
  `TestRitardiSilenziamento` (30, 7 e 6 giorni; chiusa e cestino esclusi; una volta sola), `TestRitardiSilenziamentoFrasi`,
  `TestRitardiSilenziamentoSoloAllaPrimaVera` (senza canali, spento, lunedì chiuso: niente; a mano sì; chiave importata:
  niente silenziamento e la busta vecchia si segnala), `TestRitardiSilenziamentoTuttoONiente` (chiave rifiutata da un
  trigger: nessun timbro), `TestRitardiStopERiavvio` (stop dopo la prima busta: niente riga; pianificatore nuovo: solo la
  seconda, nessun doppio), `TestRitardiRecuperoD17` (lunedì chiuso niente, martedì «15/11/2026 (2gg fa)»).
- **Correzioni della revisione** (stesso file): `TestRitardiTimbroRicontrollaLaSoglia` (soglia 3, data spostata a 2
  giorni fa fra elenco e timbro: niente timbro), `TestRitardiOggiDelNegozio` (23:30 UTC = già il giorno dopo in
  negozio), `TestRitardiStopConErroreNonDelContesto` (stop durante l'accodamento con «interrupted»: nessuna riga),
  portati dai test del revisore; `TestRitardiDataPromessaNonValida`, `TestRitardiSilenziamentoNelRegistro`,
  `TestRitardiSenzaDestinatari`. Uccidono le mutazioni corrispondenti (timbro senza soglia, «oggi» in UTC, niente
  controllo dello stop, data storta come guasto, destinatari vuoti accettati, niente riga Info:
  `mutazioni/esiti-3.txt`, 6 su 6).
- **`cmd/gestrip/lavori_test.go`:** `TestLavoriAutomaticiRitardi` (registrato il solo lavoro dei ritardi).
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-095/mutazioni/`): 35 su `ritardi.go` e `config.go`; 24 uccise, 3
  non compilano, 8 vive. Delle vive, 2 con un senso uccise dopo aver rinforzato i test (lettura della chiave nella
  transazione, tolta la prima lettura fuori; taglio in byte invece che in grafemi, test con 150 «è»); 6 equivalenti
  (`IS NOT NULL` su `data_prevista`, uno stato in più nel `NOT IN`, la prima lettura della chiave poi tolta, lo stop
  trattato come guasto e il controllo del contesto fra le buste — il pianificatore e `BeginTx` danno lo stesso esito —,
  la soglia 0 che il getter riporta a 1).
- **Mutazioni della revisione:** 50 compilanti, 36 uccise, 9 equivalenti, 5 vive; delle vive 3 uccise con i test sopra
  (m32, m37, m17) e 2 lasciate e annotate come non coperte: **m11 e m13**, l'errore del `Commit` ignorato (nel
  silenziamento e nel timbro), che i test non sanno provocare.
- **`scripts/check`:** verde al primo giro (`to-be-deleted-after-use/slice-095/prove/check.txt`) e dopo le correzioni
  della revisione (`check-2.txt`).

**Misurato:** tutto quanto sopra nei test. **Solo ragionato:** due processi sullo stesso database (il timbro
condizionato basta a non mandare due volte; non provato con due processi veri); il comportamento sul banco.

## Banco

«Si vede con la 098»: nessuno scenario nuovo; i due Go del banco partono con `-senza-lavori-automatici`.
Nota per la 098: i dati `sintetici-import` portano la chiave `jobs.overdue.silenced_history`, quindi sul banco il primo
«Esegui adesso» dei ritardi non silenzia e segnala tutte le buste in ritardo non ancora timbrate della cattura.

**Giro s095a (18/11/2026): VERDE.** HTTP 83 scenari, 1314 passi, 0 inattese, 0 errori; browser 29 scenari, 58 viste,
0 inattese, 0 errori. Memoria disponibile minima 20,6 GB. Copertura con l'esito: 353 voci, chiuse 208 (JOB-07, JOB-08),
rosse 0, parziali 16, aperte 118, fuori v1 11.

## Per il manuale

- Ogni mattina (dalle 9, nei giorni di apertura) il programma manda al negozio un messaggio per ogni busta che ha
  passato la data di consegna promessa, su email e su Telegram se sono attivi.
- Ogni busta si segnala una volta sola; se si cambia la data promessa con un giorno diverso, la busta si segnala di
  nuovo quando passa la data nuova.
- Le buste consegnate, annullate, chiuse (compro oro) o nel cestino non si segnalano.
- Il titolare può decidere dopo quanti giorni una busta è «in ritardo» (di default: il giorno dopo la data promessa;
  la pagina arriva con la 098).
- La prima volta che il controllo parte davvero, le buste in ritardo da una settimana o più vengono messe a tacere senza
  messaggi, per non ricevere tutto lo storico in una volta; chi arriva dal vecchio programma ha già fatto questo passo.
- Se il messaggio di una busta non si può accodare, le altre partono lo stesso; lo storico mostra «Fallito» con il
  motivo e il controllo riprova al minuto dopo (al massimo tre volte al giorno), altrimenti il giorno dopo.

## Dubbi

1. **Inventario JOB-08: «più di 7 giorni».** Lo standalone silenzia le buste con la data al più oggi − 7, cioè in ritardo
   da **7 giorni o più** (una busta di 7 giorni si silenzia; l'inventario dice «da 1 a 7 giorni vengono segnalate»). Il
   Go copia il codice e il test lo fissa. **Raccomandazione:** correggere la frase dell'inventario («da 7 giorni o più»;
   «da 1 a 6 segnalate»), nessuna D. **Fatto** dal coordinatore nell'inventario il 18/11/2026, senza D.
2. **«1 busta storiche silenziate alla prima esecuzione.»** Frase dello standalone, copiata com'è (anche il plurale
   sbagliato con una busta). **Raccomandazione:** tenerla così per la v1 (succede al massimo una volta per negozio, e un
   negozio importato non la vede mai). **Deciso (coordinatore, 18/11/2026, regola di Stefano):** si copia com'è e va
   fra i difetti non gravi dello standalone.
3. **«Un errore non ferma le altre» solo per i rifiuti della coda**, come lo standalone: un guasto del database su una
   busta ferma il lavoro («Il job si è interrotto…», poi il giro dopo). **Raccomandazione:** tenerlo; un guasto del
   database non è della busta, e i tre tentativi al giorno della 094 lo coprono.
4. **Stop a metà senza riga** (Scelte, 3). **Raccomandazione:** tenerlo; se si vorrà vedere nello storico anche
   l'esecuzione interrotta dallo spegnimento, serve una frase nuova e una «D-nuova (da approvare)».
5. **Testo semplice anche su Telegram** (niente `<b>` del Flask), come lo standalone (nessun `parse_mode`). Nessuna
   azione.

## Rettifica (slice 096, 18/11/2026)

Per la D-64/D-82 i messaggi dei ritardi e i motivi del dettaglio usano il numero dei testi (`buste.NumeroNeiTesti`:
«Busta in ritardo 0045/26» con l'etichetta, «45/26» senza), non il numero interno: corretto con la 096
(`TestRitardiNumeroDeiTesti`), nessuna D nuova.
