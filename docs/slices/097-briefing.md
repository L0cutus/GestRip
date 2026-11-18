# 097 — Lavoro «Briefing del mattino» (T4.9, dodicesima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/jobs/briefing.ex` (`run/4`, `unwrap/1`,
  `enqueue!/4`, `detail/2`, `sections/3`, `section/3`, `ready/1`, `late/1`, `due_soon/1`, `open_with_date/0`,
  `out_of_shop/2`, `render/2`, `block/1`, `line/2`, `name/1`, `description/1`, `cut/1`, `pack/1`, `fit/1`, `split/3`,
  `take_fitting/3`, `clip/1`, `take_units/4`, `size/1`, `@cap`, `@max_description`, `@max_message`, `@not_late`),
  `jobs/config.ex` (`briefing_days`, `@briefing_days_key`, `days/1`), `workshop/repair.ex` (`ready_query/0`,
  `closed_statuses/0`), `workshop/artisan_reminders.ex` (`late_query/3`), `repo.ex` (`prepare_query/3`: il cestino);
  test `test/gestrip_standalone/jobs/briefing_test.exs`. Inventario JOB-10, JOB-11; D-16, D-17/D-102, D-64/D-82.
- **Voci:** JOB-10 e JOB-11 (parziali fino all'esito del banco, come sempre). Nessuna migrazione, nessuna D nuova.

## Cosa c'è

- **`internal/jobs/briefing.go`** (nuovo): `LavoroBriefing` (`NuovoBriefing(d, coda)`, `ConRegistro`), il `jobs.Lavoro`
  del briefing: testo (`componi`, `blocco`, `riga`), divisione (`dividi`, `adatta`, `accorcia`, `unitaUTF16`),
  `MassimoMessaggio` (4000), `VociPerSezione` (20), `MassimoDescrizioneBriefing` (60).
- **`internal/buste/briefing.go`** (nuovo): `LeggiBriefing(ctx, q, adesso, giorniFuori, quante)`, le cinque query nella
  transazione del lavoro. **Riusa** invece di copiare: `condizionePronta` (Repair.ready_query, estratta da
  `condizioneDaAvvisare` in `avvisi.go`, che ora la compone), `statiChiusiSQL` e `busteInRitardoDove` della pagina dei
  solleciti (late_query/3) per fuori bottega; `BustaInRitardo` ha in più `Cliente` (la copia del nome sulla busta).
- **`internal/jobs/config.go`:** `Config.GiorniBriefing()` dalla chiave `briefing_alert_fuori_bottega_giorni`
  (`ChiaveGiorniBriefing`; chiave dello standalone (in origine del Flask); l'import dallo standalone la porta se la sua pagina l'ha scritta; l'import Flask→standalone non la porta (`operational.ex` `@setting_targets`)): `Integer.parse` del valore senza spazi ai bordi, da 0 a 365, altrimenti
  14 (`GiorniBriefingPredefiniti`); `leggiSoglia` prende il minimo (1 per i ritardi, 0 per il briefing).
- **`cmd/gestrip/main.go`:** `lavoriAutomatici` registra anche il briefing (registro `lavoro=briefing`).
- **SVILUPPO:** sezione «Lavoro «Briefing del mattino»» e la riga del pianificatore aggiornata.

## Sezioni e testo

Le cinque domande dello standalone, nel suo ordine, ognuna con `LIMIT 21` (una più di quelle che mostra):

1. «✅ Pronte da avvisare»: riparazioni `pronta`, `avviso_pronta_il` vuoto, `data_consegna` vuota; dalla `data_pronta`
   più vecchia, quelle senza data in fondo, poi per id.
2. «🪙 Compro oro da avvisare»: compro oro `valutata`, stesse condizioni; dal più nuovo (`id DESC`).
3. «⚠ In ritardo»: `data_prevista` prima di oggi (negozio), stato non chiuso e non `pronta`/`valutata`, `data_consegna`
   vuota; per data promessa e id.
4. «📅 In scadenza 3gg»: le stesse con la data da oggi a oggi+3 compresi.
5. «🛠 Fuori bottega da ≥Ngg»: le buste della pagina dei solleciti con la soglia del briefing, dall'uscita più vecchia
   qualunque sia l'artigiano; N = 0 toglie la sezione. I giorni sono blocchi di 86 400 s (`DateTime.diff … :day`).

**Il cestino è escluso in tutte e cinque** (nello standalone le cinque query passano da `Repo.prepare_query/3`; il test
dello standalone lo prova per il ritardo). Vedi il Dubbio 1.

Testo semplice (nessun `parse_mode`), oggetto «Briefing gg/mm», intestazione «🌅 Briefing gg/mm» con il giorno del
negozio; un blocco per sezione non vuota: «<titolo> (N)» o «(21+)», righe «• #<numero> <cliente> — <descrizione>
(gg/mm/aaaa)» (data solo per ritardo e scadenza) o «• #<numero> <cliente> da <artigiano> · Ngg», e «…e altre ≥1».
Il numero è `buste.NumeroNeiTesti` (D-64/D-82: «#0045/26» con l'etichetta, «#45/26» senza; lo standalone scrive
`bag_number`). Cliente senza spazi ai bordi, vuoto «—»; descrizione vuota senza trattino, oltre 60 unità UTF-16 i
primi 57 grafemi e «…» (Dubbio 3). Niente da dire: «🌅 Briefing gg/mm — nulla all'orizzonte ☕», mandato lo stesso.
Frase: «N voci in M messaggi.» («1 voce», «1 messaggio»), «Nessuna voce: inviato il briefing vuoto in 1 messaggio.».
Le voci (`Avvisi`) sono le righe mostrate, al più 20 per sezione. **Nessun timbro** sulle buste.

## Divisione a 4000 unità UTF-16 (JOB-11)

- **Misura:** `unitaUTF16` = `size/1`: una runa fuori dal BMP vale 2 (le emoji come 😀 sono coppie surrogate),
  ogni altra 1 (anche «è»). Mai byte o rune.
- **`dividi` = `pack/1`:** i blocchi (intestazione, poi una sezione per blocco) si uniscono con «\n\n» finché la parte
  resta ≤ 4000 unità; altrimenti il blocco comincia una parte nuova. Un blocco che sta non si tocca mai.
- **`adatta` = `fit/1` + `split/3`:** un blocco oltre 4000 si divide fra le righe: almeno una riga per parte, poi
  tante quante stanno (≤ 4000 esatte); le parti dopo la prima hanno il titolo «<titolo> (segue)». I pezzi tornano a
  `dividi` come blocchi qualunque (l'intestazione sta col primo pezzo, la sezione dopo può stare con l'ultimo).
- **`accorcia` = `clip/1`:** solo dentro un blocco che non sta, una riga oltre 3900 unità (margine per il titolo)
  tiene i grafemi interi (uniseg) finché la somma resta ≤ 3899 e prende «…»: una coppia surrogata o un'emoji composta
  (👨‍👩‍👧) non si spezza mai.

## Tutto o niente

Una transazione per esecuzione: lettura delle sezioni (dentro la transazione), poi tutte le parti nell'ordine, per
ogni parte ogni destinatario (`AccodaIn`), poi il commit. Le righe della coda hanno id crescenti nell'ordine delle
parti.

- **Rifiuto della coda** (`ErrMessaggioNonValido`, `ErrSoloAMano` → «La coda ha rifiutato il briefing.»;
  `ErrNonConfigurato` → «Il bot Telegram non è più configurato.»): niente commit, quindi anche le parti già accodate si
  annullano; `Risultato{Fallito, Dettaglio}` con zero conteggi (`unwrap({:error, detail})`); nel registro una riga Warn
  «briefing: briefing non accodato» con il solo motivo.
- **Altro errore** (database): il lavoro torna con l'errore, tutto annullato, la riga è `DettaglioInterrotto`.
- **Stop:** con il contesto finito il lavoro torna con l'errore del contesto: niente riga, niente in coda, il giorno
  resta da fare. (Il briefing è l'ultimo lavoro del giro: `Giro` finisce senza riga e senza l'errore, come la 094.)
- **Senza destinatari:** `ErrSenzaCanali` subito. **Solo Telegram:** i destinatari li sceglie il pianificatore (094);
  con email e Telegram accesi va solo al bot, con la sola email non parte e non scrive righe (D-16).

## Scelte

1. **Query in `buste`, testo e divisione in `jobs`**, come 096: la regola «pronta» e «fuori bottega» resta una.
2. **Fuori bottega con la query della pagina** (con il filtro del cestino fino alla 097b, che l'ha portato in
   `sqlRitardo`), poi l'ordine per uscita e id in Go (la
   query della pagina ordina per artigiano): nessuna seconda copia di `late_query/3`.
3. **Lettura dentro la transazione di scrittura** (lo standalone legge prima): nessuna differenza visibile; il testo è
   quello delle buste lette nella stessa transazione.
4. **Data promessa rigirata senza controllarla** («2026-02-31» → «31/02/2026»): la colonna la accetta con il suo GLOB,
   il tipo `date` dello standalone no; il briefing la scrive invece di fermarsi (la 095 la tratta come rifiuto perché
   lì serve un conto sui giorni). Con dati validi nessuna differenza.
5. **`ErrSoloAMano` come rifiuto**, coerente con 095 e 096 (nello standalone sarebbe un'eccezione; non succede mai).

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-097/prove/rosso.txt`): con gli stub (`LeggiBriefing` vuota, `Esegui` che non
  fa niente, `unitaUTF16` = byte, `dividi` = identità, soglia fissa a 14) tutti i 24 `TestBriefing*` rossi, e
  `TestLavoriAutomatici` / `TestLavoriAutomaticiRegistroBriefing` rossi.
- **`internal/jobs/briefing_test.go`** (database vero, consegne vere, coda vera con i provider veri, orologio finto,
  dati inventati). JOB-10: `TestBriefingCinqueSezioni` (testo esatto dello standalone su un negozio sintetico; fuori: la
  già avvisata, 10 giorni, +4 giorni, consegnata, cinque buste nel cestino; riga Telegram «shop», nessun formato),
  `TestBriefingNumeroDeiTesti` (RIP-… in tutte e cinque), `TestBriefingNullaAllOrizzonte`,
  `TestBriefingGiornoDelNegozio` (23:30 UTC = il 18), `TestBriefingOrdine`, `TestBriefingFinestreEFiltri`,
  `TestBriefingRighe` (60 unità, 57 grafemi, emoji, «è», vuoti, «—»), `TestBriefingFuoriBottega` (14 sì 13 no, soglia
  3, 1, 0, rientrata, 22 buste fuori), `TestBriefingSoglia` (14 valori della chiave), `TestBriefingVentiEVentuno` (20/21 in pronte e fuori
  bottega), `TestBriefingGuasto`, `TestBriefingSoloTelegram` (email accesa non riceve; sola email: niente giro né riga,
  `ErrSenzaCanali` a mano; spento; non due volte al giorno), `TestBriefingSenzaDestinatari`, `TestBriefingNessunTimbro`
  (colonne e `aggiornato_il` uguali; il giorno dopo le stesse buste), `TestBriefingStop` (stop durante e prima;
  riavvio: un briefing), `TestBriefingRecuperoD17` (lunedì chiuso niente; martedì lo stato di martedì, «1 voce in 1
  messaggio.»). JOB-11: `TestBriefingUnitaUTF16` (contro `utf16.Encode`), `TestBriefingDivisioneAlLimite` (4000 esatte
  in un messaggio, 4001 in due; tre blocchi), `TestBriefingSezioneTroppoLunga` (prima parte a 4000 esatte, «(segue)»,
  cinque righe da 1800 in tre parti con intestazione e sezione dopo), `TestBriefingRigaTagliata` (emoji con e senza
  «x» davanti, «è», famiglia ZWJ; 3900 esatte intera, 3901 tagliata; blocco di 4000 esatte con una riga oltre 3900 non si
  tocca), `TestBriefingNomiLunghi` (5000 «x»; 20 nomi da 3000 emoji: ≥ 20 messaggi, tutti ≤ 4000 unità e UTF-8
  valido), `TestBriefingPiuMessaggiInOrdine` (60 buste: intestazione solo nel primo, ogni busta e ogni titolo una
  volta, parti accodate in ordine), `TestBriefingSegue`, `TestBriefingTuttoONiente` (rifiuto della seconda parte con i
  tre errori: la prima annullata; registro senza chat, nomi, numeri; il giro dopo manda tutto; chat «-100 200» rifiutata
  dalla coda vera).
- **`cmd/gestrip/lavori_test.go`:** `TestLavoriAutomatici` (tre lavori), `TestLavoriAutomaticiRegistroBriefing`
  (`lavoro=briefing` nel registro).
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-097/mutazioni/`, `muta.py`, `esiti*.txt`, `riassunto.txt`): 44
  compilanti su `jobs/briefing.go`, `buste/briefing.go`, `config.go`, `main.go`; **41 uccise, 3 equivalenti**. Al primo
  giro vive m08 (blocco di 4000 esatte trattato come troppo lungo) e m38 («1 voci»): uccise con i casi nuovi di
  `TestBriefingRigaTagliata` e `TestBriefingRecuperoD17`. Equivalenti: m02 (`>=` sul taglio a 20), m16 e m43 (la
  soglia 0 è guardata due volte, nella lettura e nella sezione; tolte tutte e due la mutazione muore).
- **Mutazioni della revisione:** 65, 60 uccise, 2 equivalenti (j03, c05), 3 sopravvissute poi uccise dai test aggiunti
  (`mutazioni/muta-revisione.sh`, `esiti-revisione.txt`): b15 (tolto il taglio a 21 in fuori bottega: 22 buste fuori,
  atteso «(21+)» e «…e altre ≥1»), b19 (`giorniFuori > 1`: soglia «1» e una busta fuori da un giorno, atteso «🛠 Fuori
  bottega da ≥1gg (1)»), c07 (chiave rinominata: i test usano la stringa letterale della chiave), tutti in
  `TestBriefingFuoriBottega` e `TestBriefingSoglia`.
- **`scripts/check`:** verde (`to-be-deleted-after-use/slice-097/prove/check.txt`; copertura: voci 353, rosse 0) e dopo
  le correzioni della revisione (`check-2.txt`).

**Misurato:** tutto quanto sopra nei test. **Solo ragionato:** l'ordine d'arrivo delle parti su Telegram dopo un errore
temporaneo (Dubbio 2); il comportamento sul banco.

## Banco

«Si vede con la 098»: nessuno scenario nuovo; i due Go del banco partono con `-senza-lavori-automatici`. Note per la
098: il briefing è spento di default (va acceso in tutte e due le app per confrontarlo); nello standalone del banco,
senza Telegram, è «Saltato» «Nessun bot Telegram configurato.», nel Go nessuna riga (D-16); il numero delle righe è
quello dei testi (D-64/D-82), quindi il testo differisce dallo standalone per le buste con l'etichetta.

**Giro s097a (18/11/2026): VERDE.** HTTP 83 scenari, 1314 passi, 0 inattese, 0 errori; browser 29 scenari, 58 viste,
0 inattese, 0 errori. Memoria disponibile minima 20,8 GB. Copertura con l'esito: 353 voci, chiuse 211 (JOB-10, JOB-11),
rosse 0, parziali 16, aperte 115, fuori v1 11.

## Per il manuale

- Se il titolare lo accende (pagina «Promemoria e job»), ogni mattina dei giorni di apertura il programma manda al bot
  Telegram del negozio un riepilogo: buste pronte da avvisare, compro oro da avvisare, in ritardo, in scadenza nei
  prossimi tre giorni e da troppi giorni dall'artigiano (di default 14; 0 toglie quella parte).
- Ogni parte elenca al più 20 buste; se ce ne sono di più lo dice («21+» e «…e altre»).
- Va solo su Telegram: con la sola email non parte.
- Non segna niente sulle buste: la stessa busta torna ogni mattina finché non si fa qualcosa.
- Se non c'è niente: «nulla all'orizzonte ☕». Se il testo è lungo arriva in più messaggi; quelli dopo il primo
  possono dire «(segue)».
- Se il messaggio non si può mandare non parte niente: lo storico mostra «Fallito» e il controllo riprova.

## Dubbi

1. **Il cestino nei solleciti (083/096) e nel briefing non torna.** Il briefing esclude le buste nel cestino in tutte e
   cinque le sezioni, come lo standalone. **Verifica del revisore: la premessa del Dubbio 2 della 083 («`late_query/3`
   non guarda `deleted_at`») era sbagliata.** Prove: `repo.ex:40-48`, `prepare_query/3` aggiunge `is_nil(deleted_at)`
   per `Repair` e `Sale` salvo `with_discarded`; `artisan_reminders.ex`, `late_query/3` parte da `Repair`; nessun
   chiamante passa `with_discarded`; `briefing_test.exs:220` (busta nel cestino esclusa). Quindi nello standalone pagina,
   contatore e lavoro dei solleciti escludono il cestino, mentre il Go (decisione di Stefano del 15/11, presa su quella
   premessa) lo include: oggi una busta nel cestino ancora dall'artigiano è nei solleciti ma non nel «fuori bottega» del
   briefing. **Raccomandazione:** escludere il cestino anche in `sqlRitardo` (`internal/buste/solleciti.go:143`), dopo
   una decisione di Stefano; la correzione toccherebbe 083 e 096, non questa slice. La 097 resta come lo standalone.
   **Deciso (Stefano, 18/11/2026):** si esclude il cestino anche nei solleciti, come lo standalone, nella 097b.
   **Fatto nella 097b:** il filtro sta in `sqlRitardo`; il filtro apposta del briefing è tolto (comportamento uguale).
2. **Parti fuori ordine.** Le parti sono righe separate della coda, accodate in ordine; il dispatcher le prende per id,
   ma dopo un errore temporaneo della prima la seconda può partire prima (lo standalone lo dice, JOB-11 lo annota).
   **Raccomandazione:** come lo standalone per la v1; fra le idee, eventualmente, il «(segue)» rende già leggibile un
   arrivo fuori ordine. **Deciso (coordinatore, 18/11/2026):** si tiene come lo standalone.
3. **Taglio della descrizione con due misure** (difetto dello standalone, copiato): `cut/1` misura in unità UTF-16 (>
   60) ma taglia a 57 grafemi, quindi una descrizione di 31 emoji (62 unità) prende «…» senza perdere niente, e una di
   57 emoji resta intera (114 unità) più «…». **Raccomandazione:** tenerlo per la v1 e metterlo fra i difetti non gravi dello
   standalone. **Deciso (coordinatore, 18/11/2026):** fra i difetti non gravi dello standalone.
4. **«(21+)» e «…e altre ≥1»** sono l'aritmetica del Flask (il totale è quello letto, 21): copiata, l'inventario la
   annota. Nessuna azione.
5. **La soglia del briefing e l'import** (difetto non grave dello standalone): `config.ex:147` e l'inventario JOB-10
   dicono che l'import la porta, ma l'import Flask→standalone non la porta (`operational.ex` `@setting_targets`); la
   porta solo l'import dallo standalone al Go, se la pagina dello standalone l'ha scritta. Senza riga vale 14.
   **Raccomandazione:** metterlo fra i difetti non gravi dello standalone; nel Go nessuna azione. **Deciso (coordinatore,
   18/11/2026):** fra i difetti non gravi; inventario corretto e promemoria del cutover aggiunto dal coordinatore.
