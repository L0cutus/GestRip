# 094 — Pianificatore dei lavori automatici (T4.9, nona)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/jobs.ex` (`due?/2`, `run/3`, `run_due/1`,
  `history/1`, `last_runs/0`, `destinations/1`, `perform/5`, `record/5`, `attempts_left?/2`, `@raised_detail`,
  `@scheduler_actor`, `@audience`), `jobs/scheduler.ex`, `jobs/job.ex`, `jobs/job_run.ex`, `jobs/config.ex` (`@defaults`,
  `get/0`, `enabled?/2`, `time/2`, `flag/1`, `on?/2`, `text/1`, `parse_hour/1`); inventario JOB-01…JOB-04 (e JOB-05 per
  le letture dei valori), D-16, D-17 con la risposta A di Stefano del 15/11/2026 (martedì–sabato).
- **Voci:** JOB-02, JOB-03, JOB-04 chiuse; JOB-01 (resto: giorno dell'esecuzione e ora raggiunta sull'orologio del
  negozio, cambio d'ora) chiusa. Nessuna migrazione: la tabella `esecuzioni_lavori` c'è dalla 0011. Deviazioni usate:
  D-16, D-17, D-102 (giorni di apertura, Stefano 18/11/2026), D-03 (un canale configurato ma spento non è un destinatario).

## Cosa c'è

- **`internal/jobs`** (nuovo):
  - `Pianificatore` (`Nuovo(d, orologio, canali, lavori)`): `Dovuto` (`due?/2`), `Destinazioni` (`destinations/1`),
    `Giro` (`run_due/1`), `EseguiAMano` (`run/3` con «A mano», per la 098), `Lavoro` (lo `Scheduler`), `Storico` e
    `Ultime` (`history/1`, `last_runs/0`, per la 098).
  - `Lavoro` è l'aggancio dei lavori veri (il behaviour `Job`): `Esegui(ctx, Config, adesso, destinazioni, contesto)
    (Risultato, error)`. I lavori si registrano per nome; un nome senza lavoro non è mai dovuto.
  - `Config` / `LeggiConfig`: attivo e ora dei tre lavori con le chiavi e le letture dello standalone, più la chiave
    solo Go `jobs.giorni_apertura` (D-17).
- **`cmd/gestrip/main.go`:** il pianificatore parte fra i lavori in sottofondo di `serve`, sulla coda in uscita (i
  canali attivi), con `lavoriAutomatici()` ancora vuoto (095–097 lo riempiono); opzione `-senza-lavori-automatici`
  (sviluppo e banco) con l'avviso «ATTENZIONE».
- **Import:** `jobs.giorni_apertura` entra in `importer.ChiaviSoloGo` (una riga così nella sorgente è una differenza
  bloccante, come le altre chiavi solo Go); `TestChiaviSoloGoComplete` conosce `internal/jobs/config.go`.
- **Banco:** `tools/banco/istanza.go` e `gotelegram.go` passano `-senza-lavori-automatici` ai due Go.
- **SVILUPPO:** sezione «Lavori automatici: il pianificatore» e l'opzione nuova fra quelle di `serve`.

## Come non gira due volte e come si ferma

- **Un lucchetto per processo** (`Pianificatore.mu`): `Giro`, `Dovuto` ed `EseguiAMano` lo prendono per tutto il
  tempo; il giro legge «già fatto oggi» dopo la riga dell'esecuzione precedente. Un'esecuzione a mano aspetta il giro
  in corso. Il giro è uno alla volta anche per costruzione (il ciclo di `Lavoro` è sequenziale).
- **Lo stato sta nel database** (righe `scheduled` del giorno del negozio): un riavvio non rifà il lavoro riuscito e
  non regala tentativi.
- **Stop:** `Lavoro(ctx, aspetta)` torna quando l'attesa dice di smettere (contesto finito); `serve` lo avvia con
  `lavori.Go` e lo aspetta (`lavori.Wait`) prima di chiudere il database. Uno stop mentre un lavoro gira: il lavoro
  riceve il contesto; se torna con l'errore dello stop (il contesto finito) non si scrive la riga (il giorno resta da
  fare, come lo standalone fermato a metà); se torna con un `Risultato`, riuscito **o fallito**, la riga si scrive con
  i suoi conteggi (`context.WithoutCancel`, una scrittura breve), perché il lavoro ha già fatto qualcosa.
- **Fra due processi** sullo stesso database non c'è blocco, come nello standalone (lì «un `Scheduler` per
  installazione»): sull'apparecchio `serve` è uno solo.

## Istanze del banco

I due Go del banco (`grgo-<nome>-go`, canali spenti, e `grgo-<nome>-gotg`, Telegram acceso con il bot finto) partono
con `-senza-lavori-automatici`: il pianificatore non parte, quindi nessuna riga di `esecuzioni_lavori`, nessun
`jobs.ran` e nessun messaggio al bot finto scritti da soli. Doppia rete sul Go del confronto: anche senza l'opzione
non scriverebbe niente (D-16, nessun canale). Sul Go con Telegram, invece, ritardi e solleciti (accesi di default)
sarebbero dovuti dalle 09:00/10:00 dei giorni aperti dalla 095 in poi: l'opzione è quella che li tiene fermi.
«Esegui adesso» della 098 resta possibile. Lo provano `TestSuConLaCatena` e `TestSuSenzaCatena`.

## Scelte

1. **Prima l'attesa, poi il giro** (lo standalone fa partire il primo tick dopo 60 s).
2. **Ora raggiunta sull'ora del muro di Roma** (ore, minuti, secondi locali contro l'ora impostata), non sulla durata
   dalla mezzanotte: il giorno del salto delle 02:00 un lavoro delle 02:30 parte alle 03:00, come `Time.compare`
   sull'ora locale dello standalone; il 25/10 le 02:30 tornano due volte e la seconda il giorno è già fatto.
3. **Ordine D-16 prima dello spento:** senza canali `EseguiAMano` dà `ErrSenzaCanali` anche per un lavoro spento;
   con un canale, lo spento a mano è «Saltato» con «Job disattivato dal titolare.» (JOB-06) e non conta per il giorno.
4. **Un errore di lettura o di scrittura della riga ferma il giro** (come l'eccezione fuori dal `perform` dello
   standalone): niente riga, il lavoro resta dovuto al giro dopo. Una riga senza il suo `jobs.ran` non si scrive.
5. **Registro:** per un panico il nome del lavoro, il tipo e lo stack (ACC-19), mai il valore; per un errore solo nome
   e tipo (vedi Dubbi, 5). La chiave è `nome`, perché `lavoro` è già quella del registro di `serve`.
6. **Destinatari letti da `CanaleAttivo` della coda** (stessa domanda di `Accoda`) più l'indirizzo dalla
   configurazione: un canale che la coda rifiuterebbe non è mai un destinatario.

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-094/prove/rosso.txt`): con gli stub delle firme (`internal/jobs` che non
  fa niente) 10 test su 15 rossi e `TestMaiDueEsecuzioniInsieme` appeso fino al limite di 2 minuti (poi i test sono
  stati resi a prova di attesa con `ricevi`/`manda`); `TestServePianificatore` rosso con il pianificatore non avviato
  in `serve`; `TestSuSenzaCatena` rosso prima dell'opzione nei comandi del banco.
- **`internal/jobs/jobs_test.go`** (orologio finto, database vero, coda con i provider veri che non vanno in rete):
  `TestDovutoAttivoEOra`, `TestDovutoGiorniDiApertura` (domenica e lunedì chiusi, a mano sì, valori illeggibili),
  `TestDovutoTentativi` (terzo tentativo, «ok» che consuma il giorno, a mano che non conta, giorno nuovo),
  `TestDovutoDopoUnRiavvio`, `TestDovutoCambioDOra` (29/03 e 25/10, sera tardi UTC), `TestDestinazioni`,
  `TestCanaliSpentiNienteRighe`, `TestStoricoEAudit`, `TestSpentoASconosciuto`, `TestLavoroInterrotto` (panico ed
  errore con un indirizzo inventato che non finisce né in riga, né in audit, né nel registro), `TestMaiDueEsecuzioniInsieme`
  (un giro fermo dentro il lavoro, altri due giri e un'esecuzione a mano: picco 1, una sola riga automatica),
  `TestLavoroSiFerma` (panico nel giro, contesto finito, attesa che smette subito), `TestArrestoDuranteUnLavoro`,
  `TestStoricoEUltime` (con una riga importata «Saltato»), `TestRigaEAuditInsieme` (audit rifiutato: niente riga).
  Correzioni della revisione: nel caso «sera tardi UTC» un minuto dopo il giorno è già fatto (tentativi contati sul
  giorno del negozio); `TestArrestoDuranteUnLavoro` ha anche lo stop con `Risultato{Fallito: true, Avvisi: 1}` → una
  riga `failed` con i conteggi; `TestStoricoEAudit` ha conteggi negativi → 0 in riga e in audit; `TestLavoroInterrotto`
  vuole lo stack nella riga del panico e non in quella dell'errore; `TestServePianificatore` chiude l'uscita 200 ms
  dopo lo stop, così passa solo se `serve` aspetta davvero il pianificatore.
- **`cmd/gestrip/lavori_test.go`:** `TestServePianificatore` (parte, si ferma, `serve` lo aspetta; con l'opzione non
  parte e c'è l'avviso), `TestOpzioneSenzaLavoriAutomatici`.
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-094/mutazioni/`): 31 su `jobs.go` e `config.go`, 26 uccise
  al primo giro; le 3 vive con un senso (ordine attesa/giro, giorno «0» letto come domenica, ora predefinita del
  briefing) uccise dopo aver rinforzato i test; restano 2 equivalenti (il `continue` su `ErrSenzaCanali` nel giro,
  irraggiungibile sotto il lucchetto, e il dettaglio dell'interrotto, già a zero) e 1 che non compila. Dopo la
  revisione altre 6, tutte uccise: tentativi contati sul giorno UTC, niente riga per uno stop con un risultato fallito,
  panico senza stack, errore con lo stack, conteggi negativi non portati a 0, `go` semplice al posto di `lavori.Go` in
  `serve`.
- **`scripts/check`:** verde (`to-be-deleted-after-use/slice-094/prove/check.txt`, dopo le correzioni della revisione
  `check-2.txt`); i giri prima erano rossi per
  gofmt, un campo inutilizzato in un test (staticcheck) e `TestChiaviSoloGoComplete` (chiave nuova da dichiarare).

**Misurato:** tutto quanto sopra nei test. **Solo ragionato:** che sull'apparecchio giri un solo `serve` per database;
che i lavori veri (095–097) rispettino il contesto e tornino presto allo stop; il comportamento con l'orologio di
sistema che salta (il giro legge `Adesso()` a ogni passata, un salto indietro fa solo ripetere la domanda).

## Banco

Nessuno scenario nuovo (il pianificatore non ha superficie HTTP fino alla 098). Il giro serve a
provare che i due Go del banco partono con l'opzione nuova e che gli scenari di sempre restano verdi; il Go del banco
è costruito dall'albero, quindi accetta l'opzione.

**Giro s094a (18/11/2026): rosso solo per la nota 8** (quinta comparsa, `docs/domande-aperte.md`): HTTP 83 scenari, 0
inattese; browser 1 errore e 52 inattese, tutte dello standalone in `whatsapp` telefono («500 GET /», `Database busy`).
**Giro identico s094b: VERDE.** HTTP 83 scenari, 1314 passi, 0 inattese, 0 errori; browser 29 scenari, 58 viste, 0
inattese, 0 errori; i due Go del banco hanno scritto all'avvio «lavori automatici spenti (-senza-lavori-automatici)».
Memoria disponibile minima 21,7 GB. Copertura con l'esito: 353 voci, chiuse 206 (JOB-01, JOB-02, JOB-03, JOB-04),
rosse 0, parziali 16, aperte 120, fuori v1 11.

## Per il manuale

- I lavori automatici (avviso delle buste in ritardo, solleciti all'artigiano, briefing del mattino) partono da soli,
  una volta al giorno, all'ora scelta dal titolare (ora d'Italia).
- Partono solo nei giorni di apertura del negozio: di default da martedì a sabato. Un giorno chiuso non lascia righe;
  il primo giorno aperto trova da solo quello che era rimasto.
- Partono solo se è attivo almeno un canale (email o Telegram); il briefing solo con Telegram. Senza canali sono
  spenti e lo storico resta vuoto.
- Se un lavoro non riesce, ci riprova al minuto dopo, al massimo tre volte al giorno; lo storico mostra ogni tentativo.
- Con l'apparecchio spento all'ora del lavoro, il lavoro parte appena riacceso quel giorno; un giorno intero spento
  non si recupera.
- Per ora niente di visibile: i tre lavori arrivano con le slice 095–097 e la pagina «Promemoria e job» con la 098.

## Dubbi

1. **Giorni di apertura: forma della chiave e «nessun giorno».** `jobs.giorni_apertura` = giorni ISO separati da
   virgole («2,3,4,5,6»); una riga vuota o illeggibile vale martedì–sabato. **Deciso (Stefano, 18/11/2026):** la chiave
   resta così e la 098 rifiuta il modulo senza nessun giorno. Riga **D-102** (precisa la D-17): sette caselle nella
   pagina Promemoria, default martedì–sabato, lavori automatici solo nei giorni spuntati, «Esegui adesso» sempre.
2. **Festivi.** Contano solo i giorni della settimana: un festivo infrasettimanale è un giorno aperto. **Deciso
   (Stefano, 18/11/2026, D-102):** così per la v1; i giorni di chiusura sono un'idea per dopo.
3. **Lettura dell'ora salvata più stretta dello standalone.** Il Go legge `HH:MM` e `HH:MM:SS`; lo standalone, con
   `Time.from_iso8601`, prende anche forme come `09:00:00.5` o `T09:00:00`. Nel Go una forma così fa usare l'ora
   predefinita. **Deciso (Stefano, 18/11/2026):** resta così, senza una D (la pagina manda solo `HH:MM`).
4. **Lo standalone del banco scrive «Saltato» ogni giorno** (scheduler acceso, nessun canale), il Go no (D-16 e
   opzione). Non tocca la 094; **raccomandazione:** negli scenari della 098 confrontare lo storico solo dove la D-16 è
   dichiarata (le righe «Saltato» dello standalone come deviazione D-16).
5. **Registro dell'errore di un lavoro senza testo né stack** (deciso col coordinatore il 18/11/2026: così; nella lista
   delle differenze dallo standalone). Lo standalone scrive nel log l'eccezione intera
   (`Exception.format`, testo e stack). Il Go scrive per un panico tipo e stack (ACC-19), per un errore restituito solo
   il tipo: il testo di un errore può contenere indirizzi, nomi o il testo di un messaggio, e lo stack di un errore
   restituito direbbe solo dove lo si registra. **Raccomandazione:** tenerlo così; se servirà più diagnosi, i lavori
   095–097 possono restituire errori con un genere fisso e sicuro (come `uscita.Rifiuto`) da scrivere al posto del tipo.
6. **Nota per la 098: ordine dei controlli a mano.** `EseguiAMano` guarda prima i canali (nessuno: `ErrSenzaCanali`,
   niente riga, D-16) e poi l'interruttore (spento: «Saltato»); lo standalone guarda prima l'interruttore. Cambia
   solo con un lavoro spento e nessun canale (standalone «Saltato», Go niente), coerente con la risposta B (niente
   «Esegui adesso» né righe «Saltato» senza canali). **Raccomandazione:** tenerlo, dichiarandolo nella resa D-16 della
   098.
7. **Nota per la 095–097: il recupero della D-17.** Il pianificatore non fa niente di speciale il primo giorno aperto:
   il «recupero» è dei lavori, che devono trovare da soli quello che è rimasto (ritardi: buste non ancora segnalate;
   solleciti e briefing: lo stato di oggi). **Raccomandazione:** un test per lavoro nella 095–097 con un lunedì
   chiuso e il martedì che trova la busta diventata in ritardo di lunedì.
