# 086 — Oreficerie clienti in `/settings/anagrafiche` (T4.9, prima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/oreficerie_live.ex` (eventi
  `create` e `update`, `render/1`), `lib/gestrip_standalone/workshop.ex` (`list_external_oreficerie/0`,
  `get_oreficeria/1`, `create_oreficeria/2`, `update_oreficeria/3`, `case_insensitive_duplicate?/4`,
  `external_oreficerie/0`), `workshop/oreficeria.ex`, `workshop/phone.ex`, `id.ex`, la migrazione
  `20260906213000_create_oreficerie_and_link_repairs.exs`; test `oreficerie_test.exs`, `oreficerie_live_test.exs`.
- **Voci:** ORE-01, ORE-02, ORE-03 (portate; si chiudono con l'esito verde del banco). La tabella `oreficerie` c'è dalla
  0007 (`nome` unico, `ON DELETE RESTRICT` dalle buste e dai lotti); la migrazione nuova semina solo la riga 1. Chiude il Dubbio 1 della 076.
- **Lavoro:** sottoagente d'implementazione (Sonnet), test rossi prima, nessuna mutazione ancora (le fa la revisione).
  Il banco non è stato lanciato: lo fa la sessione principale quando c'è memoria (sotto, «Banco»).

## Piano della T4.9

Tabella copiata da `~/Condivisi/GestRip-Go/piano-T4.9-bozza.md` (approvata da Stefano il 15/11/2026; la colonna Modello
dice chi scrive il codice, revisione e mutazioni sempre Opus).

| Slice | Cosa | Voci | Dipende da | Banco | Rischi | Modello |
|---|---|---|---|---|---|---|
| **086** | Oreficerie in `/settings/anagrafiche` accanto agli artigiani: elenco senza la riga 1 «Negozio», nuova, modifica in riga (doppione: creazione con maiuscole, modifica senza), «Oreficeria non trovata o non modificabile.», nessuna eliminazione; `NormalizzaTelefono`; audit `oreficeria.*` | ORE-01, 02, 03 | 076 | HTTP `anagrafiche`, `anagrafiche-dipendente` allargati; browser `anagrafica-oreficerie` con nomi propri per vista | basso; le oreficerie non si cancellano: quelle del banco restano nei menu (nome che cade in fondo o `in_fondo`). Chiude il Dubbio 1 della 076 | **Sonnet** (anagrafica) |
| **087** | Profilo del negozio `/settings/shop`, sola sezione «save» (ADM-03/05/07 alla T4.15): nome e garanzia obbligatori, lunghezze, «I documenti emessi restano invariati.», audit | ADM-02 | — | HTTP titolare e rifiuto del dipendente; browser con salvataggio che alla fine rimette il profilo | basso/medio: il profilo lo leggono PDF, `/privacy`, `/lotto`, manifest | **Sonnet** (impostazioni) |
| **088** | Dominio dei lotti e `/ready-lots`: pulizia dei lotti vuoti dei giorni prima all'apertura, lotti aperti oggi (orologio del negozio), «Nessun lotto aperto oggi.», apertura o ripresa del lotto di oggi con gettone di 24 byte, rifiuti, `ready_lot.opened` | ORE-04, 05 | 086 | HTTP lista e rifiuti; browser apertura su un'oreficeria creata dallo scenario | medio/alto: scrittura su GET, gettone pubblico, mezzanotte locale; due lotti nello stesso istante (domanda C) | **Opus** (link pubblico, cancellazioni) |
| **089** | `/ready-lots/{id}`: aggiunta per QR, numero (D-64), id; rifiuti di ORE-06 («già avvisata» vince); candidati per data di pronta; ✕ togli; sola lettura per un lotto inviato; audit; link «Apri il lotto» dal riquadro avvisi | ORE-06, 08, 09 | 088, 066 | HTTP rifiuti e candidati; browser buste proprie del 2025 di un'oreficeria propria | alto: gare fra aggiunte, esclusione con l'avviso singolo dentro l'UPDATE condizionato | **Opus** |
| **090** | Scanner continuo del lotto (riuso del lettore della 079): «↻ già in questo lotto» come errore, «Riparazione non trovata. («X»)» | ORE-07 | 089, 079 | browser con la fotocamera finta su una busta propria | medio: riuso senza duplicare; deviazioni gemelle di D-90/D-91 (domanda F) | **Sonnet** (interfaccia, riuso) |
| **091** | «📤 Chiudi e invia», tutto o niente: lotto inviato prima di avvisare, ogni busta con l'avviso nella stessa transazione, «Busta #id: <motivo>. Niente è partito.», `wa_oreficeria_lotto`, `{link}`, «Apri WhatsApp»/«👥 Scegli contatto» (D-74), oreficeria senza numero → contatto scelto, `ready_lot.sent` | ORE-10 | 089, 066–068 | browser invio di un lotto proprio (gettone normalizzato, D-64); HTTP rifiuti | alto: avvisi di pronta e link dei clienti in blocco, eccezione a metà annulla tutto | **Opus** |
| **092** | Pagina pubblica `/lotto/{token}` e foto (canale orefice, `?size=thumb`): stesso 404 per ogni rifiuto, `no-store`, `no-referrer`, limite dei falliti `lotto`; totale con «(N senza importo esatto)»; piè col profilo; chiusura alla lettura (ORE-15) | ORE-14, 15, 16 | 091, 087 | HTTP lotto 14/26 della cattura, gettoni sbagliati, foto, intestazioni; chiusura alla lettura `in_fondo` | alto: faccia pubblica (Funnel), scrittura su GET, link già mandati (domanda G) | **Opus** (link pubblico) |
| **093** | Storico `/ready-lots/history` (ultimi 100), dettaglio con «Link storico non riattivato…», reinvio: «Prepara» non scrive, solo il tocco registra la riga WhatsApp e `ready_lot.resent` (D-74) | ORE-11, 12, 13 | 091, 068 | HTTP elenco e dettaglio del 14/26, «Lotto non trovato nello storico.»; browser reinvio su un lotto della 091 | medio: doppio tocco, riga verso l'oreficeria | **Opus** (coda in uscita) |
| **094** | Pianificatore `internal/jobs`: giro al minuto in `main.go`; `due?` (attivo, ora raggiunta, ≤3 tentativi, nessun «ok» oggi, solo giorni di apertura D-17); destinatari (negozio su email e Telegram, briefing solo Telegram); canali spenti (D-16): nessuna esecuzione e nessuna riga; storico `esecuzioni_lavori`, «Il job si è interrotto…», `jobs.ran`; chiude JOB-01 | JOB-02, 03, 04; JOB-01 (resto) | 069, 071 | nessuno scenario nuovo; test con orologio finto (domenica, cambio d'ora, riavvio, terzo tentativo); le istanze Go del banco non scrivono righe | alto: parte da solo, mai due esecuzioni insieme, fermabile nei test | **Opus** (lavori automatici) |
| **095** | «Buste in ritardo»: un messaggio per busta e canale, timbro e messaggio nella stessa transazione, `claim` che ricontrolla il predicato, esclusi gli stati chiusi; silenziamento alla prima esecuzione vera (JOB-08) | JOB-07, 08 | 094 | si vede con la 098 | alto: messaggi automatici, silenziamento una volta sola, un errore non ferma le altre | **Opus** |
| **096** | «Solleciti artigiano»: stesse buste e soglia della 083, un riepilogo per canale, timbro per giro, tutto o niente; non registra un sollecito vero | JOB-09 | 094, 083 | si vede con la 098 | alto: «per giro» rispetto alla consegna, giorni con l'orologio del negozio | **Opus** |
| **097** | Briefing del mattino: cinque sezioni da 20 buste («21+»), «nulla all'orizzonte ☕», solo Telegram, tutto o niente, divisione a 4000 unità UTF-16 con «(segue)» e righe tagliate con «…» | JOB-10, 11 | 094, 071 | si vede con la 098 | medio/alto: UTF-16 delle emoji, parti fuori ordine | **Opus** |
| **098** | «Promemoria e job» `/settings/promemoria` (titolare): attivo, ora, soglie, letture «spento/acceso», `jobs.configured`; «▶ Esegui adesso» (spento → «Saltato», non conta come esecuzione del giorno); ultime 20 righe; giorni di apertura (D-17); resa della D-16 | JOB-05, 06 | 094–097 | HTTP nelle due app con deviazione D-16, dipendente rifiutato, ora non valida; solo Go con Telegram acceso (074b): «Esegui adesso» dei tre lavori e testi in `/_prova/mandati` | medio/alto: «Esegui adesso» manda davvero; buste in ritardo della cattura stabili | **Opus** (avvia invii) |
| **099** | Capitolo del manuale della tappa dalle sezioni «Per il manuale» 086–098, con le schermate | — | 086–098 | browser per le schermate | basso | **Sonnet** (testi) |

## Cosa c'è

- **Dominio** (`internal/buste/oreficerie.go`, nuovo): `Oreficerie` (= `list_external_oreficerie/0`: id > 1, per nome e
  id), `CreaOreficeria` (= `create_oreficeria/2`: nome ripulito e obbligatorio, telefono con `NormalizzaTelefono` o NULL,
  il doppione lo dice l'indice unico a maiuscole esatte, anche contro la riga 1), `ModificaOreficeria` (=
  `update_oreficeria/3`: riletta nella transazione; la riga 1 e un id che non c'è sono lo stesso «non trovata o non
  modificabile»; doppione senza maiuscole con `LOWER` solo ASCII fra le esterne, poi l'indice unico per il nome
  esatto, negozio compreso). Il tipo si chiama `OreficeriaCliente` perché `Oreficeria` è già quello della scheda. Riga
  d'audit `oreficeria.created` e `oreficeria.updated` (senza dati) nella stessa transazione: se non si scrive, niente
  cambia. Il controllo del nome e del telefono è quello degli artigiani (`campiArtigiano`, riusato).
- **Migrazione `0020_negozio_oreficeria.sql`** (Dubbio 1): `INSERT OR IGNORE INTO oreficerie (id, nome) VALUES (1, 'Negozio')`,
  come la migrazione dello standalone (`20260906213000`: `INSERT OR IGNORE INTO oreficerie(id, name) VALUES (1,
  'Negozio')`, nessun telefono). **L'import** (`internal/importer/importa.go`) toglie la riga seminata dentro la sua
  transazione prima di copiare le oreficerie: la riga 1 della sorgente arriva con nome e telefono suoi e il conteggio
  delle righe riconcilia.
- **Pagina** (`internal/web/anagrafiche.go`, `pagine/anagrafiche.html`, `statici/app.css`): il riquadro `#oreficerie`
  («Oreficerie clienti», la frase sui nomi, una riga per oreficeria con nome, telefono e «Salva» e **senza «Elimina»**,
  «Nessuna oreficeria registrata.» nascosto con delle righe, il modulo «Nuova oreficeria *» con «Telefono WhatsApp»)
  **sopra** quello degli artigiani, come nello standalone. Le rotte sono `POST /settings/anagrafiche/oreficerie` e
  `POST /settings/anagrafiche/oreficerie/{id}` (`rottaConGuscio`: dopo un rifiuto la pagina si ridisegna con i valori
  scritti; riuscite, redirect con il messaggio). Messaggi: «Oreficeria "X" aggiunta.», «Oreficeria aggiornata.»,
  «Il nome è obbligatorio.» (con «Campo obbligatorio.» sotto il nome nella creazione), «Esiste già un'oreficeria con il
  nome "X".», «Oreficeria non aggiunta.», «Oreficeria non trovata o non modificabile.». Per far posto al secondo
  riquadro i tipi della pagina sono diventati generici (`rigaAnagrafica`, `moduloNuovo`, `scrittiAnagrafiche`).
- **Tabelle aggiornate:** permessi (`ruoli_test.go`: ORE-02 e ORE-03, `personale`), rotte con il guscio (`guscio_test.go`),
  `busteFinte`, interfaccia delle buste di `nuova.go`.
- **Banco** (non lanciato): HTTP `anagrafiche` e `anagrafiche-dipendente` allargati (titolo del riquadro, vuoto, modulo
  nuovo con i suoi attributi, righe `oreficeria-name-2` e `-phone-2` dei dati importati, la riga 1 assente, «Elimina»
  assente); scenario browser nuovo `anagrafica-oreficerie` (ORE-01…03) con nomi propri per vista («Zz Banco 086
  desktop» e «… telefono»: cadono in fondo all'ordinamento). Le oreficerie non si cancellano: lo scenario ne lascia
  due in più per vista (`fine.righe-in-piu`), e nessuno scenario dopo legge i menu delle oreficerie (gli HTTP girano
  prima del livello browser).

## Scelte

1. **Stesso schema della 076:** il Go non ha LiveView, quindi due moduli POST e la stessa convenzione (rifiuto: pagina
   ridisegnata (200) con i valori scritti; esito: 302 con il messaggio). Il modulo «Nuova oreficeria» dopo un esito
   riuscito è vuoto (come gli artigiani, Dubbio 2).
2. **Riga 1 (il negozio stesso):** `id > 1` nell'elenco, nella modifica e nel confronto `LOWER`; non nell'indice unico,
   che la vede come nello standalone. Così «IL NEGOZIO» (altre maiuscole) si può usare in modifica e «Negozio» esatto
   no.
3. **Nessuna rotta di eliminazione:** `POST …/oreficerie/{id}/elimina` risponde 404, e il database tiene comunque
   le buste e i lotti (`ON DELETE RESTRICT`); l'eliminazione voluta dopo la v1 è un'altra funzione.
4. **ID e nomi dei campi** uguali a quelli dello standalone (`oreficerie-<id>`, `edit-oreficeria-<id>`,
   `oreficeria-name-<id>`, `new-oreficeria-form`, `oreficeria[name]`, `oreficeria[phone]`): il banco HTTP li legge
   per id.
5. **Un guasto del database.** Nello standalone un audit che non si scrive torna come changeset e dà «Il nome è
   obbligatorio.», e un guasto vero di SQLite fa cadere la pagina LiveView. Il Go mostra «Oreficeria non aggiunta.»
   (creazione) e «Oreficeria non trovata o non modificabile.» (modifica), con una riga nel registro («oreficeria non
   aggiunta», «oreficeria non modificata»): più sensato, e il banco non può osservarlo (nessun guasto si provoca
   dalle pagine). Vale lo stesso per gli artigiani della 076 («Artigiano non aggiunto.», «Artigiano non trovato.»).
6. **Lettura che non riesce.** Se l'elenco delle oreficerie (o degli artigiani) non si legge, la pagina è la pagina
   d'errore (500) con «anagrafica delle oreficerie non letta» nel registro.

## Per il manuale

- In Impostazioni → Anagrafiche, sopra gli artigiani, c'è l'elenco delle oreficerie clienti, in ordine di nome, con il
  nome e il telefono modificabili sulla riga e il pulsante «Salva».
- Per aggiungerne una si scrive il nome (obbligatorio) e, se serve, il telefono WhatsApp, poi «+ Aggiungi». Spazi e segni
  nel telefono vengono tolti: restano le cifre e l'eventuale «+» iniziale.
- Il nome non si può ripetere. Per una nuova oreficeria conta anche la maiuscola («Rossi» e «ROSSI» sono due nomi); quando
  si modifica una già in elenco no («rossi» è lo stesso nome di «Rossi»).
- Cambiare il nome aggiorna anche le riparazioni passate di quell'oreficeria, perché sono collegate per numero e non per
  nome.
- Le oreficerie non si possono eliminare. Può modificarle sia il titolare sia il dipendente.
- Il negozio stesso non compare in elenco e non si può cambiare.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-086/prove/`): `rosso-web.log` (6 test su 7 rossi
  sull'asserzione: le rotte e il riquadro non c'erano; il settimo, `TestOreficerieSenzaEliminazione`, era già verde
  perché una rotta che non c'è risponde 404: protegge dal futuro). Per il dominio il rosso era la compilazione (i tipi e
  le funzioni non c'erano ancora): non c'è un log a parte.
- **Dominio** (`internal/buste/oreficerie_test.go`): elenco senza la riga 1 e per nome (BINARY), elenco vuoto, riga 1
  non modificabile, creazione (nome vuoto, di soli spazi Unicode, che manca; telefono normalizzato, senza cifre, che
  manca; audit con attore), doppioni (esatti alla creazione anche contro la riga 1, `LOWER` solo ASCII alla modifica,
  la riga 1 fuori dal confronto ma dentro l'indice unico, «èrre» e «ÈRRE»), modifica (id che non c'è, nome vuoto, telefono
  che manca → NULL, le buste collegate per id, la riga 1 con il nome vuoto), nessuna eliminazione possibile con delle
  buste, audit nella transazione (trigger `RAISE(ABORT)`).
- **Pagina** (`internal/web/anagrafiche_oreficerie_test.go`): markup del riquadro vuoto e pieno (ordine, escape, `hidden`,
  nessun «Elimina», sopra gli artigiani, riga 1 assente), creazione (con doppione, negozio, nome vuoto, moduli
  forgiati, campo ripetuto), modifica con gli id forgiati dello standalone e la riga 1, 404 dell'eliminazione, POST senza
  token (403), guasto del database, CSS del `hidden`. `TestPaginaAnagrafiche` aggiornato (ora le oreficerie ci sono).
- **Migrazione 0020 e import** (correzione dopo la revisione, rosso in `prove/rosso-migrazione.log`):
  `internal/db/negozio_test.go` (database nuovo: la riga 1 «Negozio» senza telefono e la prima oreficeria nuova con id
  2; database con la riga 1 già presente, con l'id 1 di un'altra riga e con il nome «Negozio» di un'altra riga: `OR IGNORE`
  la lascia com'è, senza errore e senza una seconda riga); `internal/importer/importa_test.go`
  (`TestImportSostituisceIlNegozioSeminato`: nome e telefono della riga 1 della sorgente e non «Negozio», conteggio uguale; `TestImportSenzaRigaDelNegozioLaRimette`: sorgente senza la riga 1) e
  `TestImportCompletoRiconciliato` (la riga 1 del sintetico, che andava in conflitto con la seminata prima della
  correzione dell'import). Tabelle di test aggiornate: 20 migrazioni (`schema_test.go`), `TestMigrazioneGiriSuUnDatabaseInUso`
  (riporta il database alla 0018: rifà 19 e 20), `INSERT OR REPLACE` dove un test scrive la riga 1 con un nome proprio.
  `TestAnagraficheOreficerieNonLetta`: lettura delle oreficerie che non riesce (500, riga nel registro).
- **Banco (solo controlli senza istanza):** `go test ./tools/banco` (scenari letti, voci, attese, conteggio degli
  scenari browser) verde; `node --check` su `banco.mjs`.

## Mutazioni

Revisore Opus, su una copia del repo (cancellata a fine giro): 31 mutazioni (18 sul dominio `oreficerie.go`, 12 su
`anagrafiche.go`, 1 sul modello della pagina), 31 compilate, **31 uccise, 0 sopravvissute**. Fra le altre: riga 1
nell'elenco, nella modifica e nel confronto `LOWER`; `LOWER` tolto; il proprio nome contato come doppione; telefono
tenuto con `coalesce`; ordine dei controlli; audit tolto o rinominato; rotte solo del titolare; `hidden` sul vuoto; registro
del guasto. L'unico punto senza test (errore di lettura in `mostraAnagrafiche`) ora ha `TestAnagraficheOreficerieNonLetta`.

## Revisione

Revisore Opus indipendente, due giri.

- **Primo giro:** nessun difetto bloccante; dominio e pagina fedeli allo standalone. Rilievo MEDIO: riga 1 «Negozio»
  assente su un database non importato (Dubbio 1) → migrazione `0020_negozio_oreficeria.sql` e importer che toglie la
  riga seminata prima di copiare quella della sorgente. BASSO: frase della Scelta 5 sul guasto → corretta.
- **Secondo giro** (sola lettura, su migrazione e importer, con il banco in corso): nessun ALTO né MEDIO. Il `DELETE`
  dell'importer lavora sempre su un file nuovo, dopo `Migra`, e niente punta ancora all'id 1. Tre BASSI, chiusi: rete
  `INSERT OR IGNORE` della riga 1 dopo la copia (`TestImportSenzaRigaDelNegozioLaRimette`) e test che la riga 1 importata
  ha nome e telefono della sorgente; riga sui database di prova vecchi nel Dubbio 1; `INSERT OR REPLACE` nei test solo per
  l'id 1, `INSERT` semplice per le altre righe (una collisione deve fallire).

## Controlli

`scripts/check` verde, rilanciato dalla sessione principale dopo ogni giro di correzioni (ultimo: 16/11/2026 01:50 circa).
Copertura: voci 353, chiuse 42, rosse 0, parziali 163, aperte 137, fuori v1 11 (senza l'esito del banco).

## Banco (sessione principale)

Con il «memoria libera» del coordinatore e la guardia sotto 10 GB (mai scattata).

- **`s086a`** (16/11/2026, 01:10–01:35): HTTP 76 scenari, 1273 passi, 0 inattese, 0 errori; browser 24 scenari, 48 viste,
  **2 inattese**, 0 errori. Le due inattese erano un'attesa sbagliata dello scenario nuovo: `pagina.righe` atteso 0 con i
  dati importati, mentre standalone e Go mostrano entrambi 3 righe sul PC e 5 sul telefono (le 3 importate più le 2 create
  dalla vista del PC). Corretta in `atteso_dati`.
- **`s086b`** (finale, codice definitivo): 01:42–02:02, HTTP 76 scenari, 1273 passi, 0 inattese, 0 errori; browser 24
  scenari, 48 viste, 0 inattese, 0 errori. **Verde.** Esito passato a `scripts/check` (`GESTRIP_BANCO_ESITO`) prima di
  `banco giu`: voci 353, chiuse 189, rosse 0, parziali 16, aperte 137, fuori v1 11; ORE-01, ORE-02, ORE-03 chiuse, e
  ART-23 (085) si richiude.

## Misura

Implementatore Sonnet 5.5, ragionamento alto (prova di Stefano del 15/11): primo giro circa 12 minuti, poi due giri di
correzioni (circa 12 e 4 minuti); token del sottoagente in totale circa 340k. Revisione Opus: circa 10 minuti (mutazioni)
più uno (secondo giro), circa 175k token. Giri del banco: 2.

## Dubbi

1. **Riga 1 su un database non importato.** *(Era: sul database nuovo la prima oreficeria avrebbe avuto l'id 1 e sarebbe
   sparita da elenco e scelte, perché la 0007 non la semina.)* **Deciso dal coordinatore il 16/11/2026: migrazione come
   lo standalone (allineamento tecnico).** Fatta: `0020_negozio_oreficeria.sql` con `INSERT OR IGNORE`. I tre casi, con
   un test ciascuno: database nuovo (la riga 1 c'è, la prima oreficeria prende l'id 2 e compare nell'elenco); database
   importato (l'import toglie la riga seminata e copia quella della sorgente, nessun doppione né conflitto); id 1
   già occupato da un'altra riga (o nome «Negozio» di un'altra): `OR IGNORE` la lascia com'è e non ne aggiunge una. In
   quest'ultimo caso, su un database di prova, la riga 1 resta quella che è: è voluto, come nello standalone.
   I database di prova creati prima della 0020 e mai importati vanno ricreati (`OR IGNORE` non sistema un id 1 già preso
   da un'oreficeria cliente). L'import ha anche una rete: dopo la riconciliazione un `INSERT OR IGNORE` della stessa riga
   rimette la riga 1 se la sorgente non l'avesse (test `TestImportSenzaRigaDelNegozioLaRimette`).
2. **D-88 per le oreficerie.** Dopo un «+ Aggiungi» riuscito il Go svuota il modulo; nello standalone lo stesso evento di
   LiveView lascia nel campo il nome scritto (misurato per gli artigiani nel banco s076p, stesso codice). La D-88 dell'inventario
   è scritta per ART-01…04. **Si è proceduto come se fosse estesa a ORE-02:** lo scenario `anagrafica-oreficerie` osserva `crea.campo-nome` e `bench/deviazioni.json` ha le due righe D-88
   (desktop e telefono) con quel testo. L'inventario non è toccato.
   **Deciso da Stefano il 16/11/2026:** la D-88 vale anche per ORE-02 (anagrafica oreficerie); le due righe restano in
   `bench/deviazioni.json` come approvate e l'inventario privato la riporta estesa.
3. **Doppio tocco su «+ Aggiungi».** Come per gli artigiani i moduli hanno `data-invio-unico` (un invio solo); lo
   standalone ci arriva con `phx-submit`. Nessuna differenza attesa, ma non è misurata sull'istanza: lo scenario browser
   la prova (`crea.righe-in-piu` = 1 e nessun «Esiste già…» dopo il doppio tocco).
4. **Eliminazione dell'oreficeria (dopo la v1, tema A).** Non c'è e non c'è una rotta: 404. Quando si farà servirà una
   regola (per esempio solo senza buste né lotti) e una D nuova. Nessuna azione adesso.
