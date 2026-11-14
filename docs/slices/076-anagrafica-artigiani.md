# 076 — Anagrafica degli artigiani e piano della T4.8 (T4.8, prima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/oreficerie_live.ex` (eventi
  `create_artisan`, `update_artisan`, `delete_artisan`, `render/1`), `lib/gestrip_standalone/workshop.ex`
  (`list_artisans/0`, `create_artisan/2`, `update_artisan/3`, `delete_artisan/2`, `case_insensitive_duplicate?/4`),
  `workshop/artisan.ex`, `workshop/phone.ex`, `id.ex`; test `artisans_test.exs`, `artisans_live_test.exs`; catalogo
  `priv/gettext/it/LC_MESSAGES/errors.po` («can't be blank» = «Campo obbligatorio.»).
- **Voci:** ART-01, ART-02, ART-03, ART-04 (tutte portate; si chiudono con l'esito verde del banco). Nessuna migrazione:
  `artigiani` e `consegne_artigiani` ci sono dalla 0007, con l'indice unico sul nome e `ON DELETE RESTRICT`.
- **Lavoro:** sottoagente d'implementazione, test rossi prima, mutazioni a mano. Il banco (Docker) non è stato lanciato:
  lo fa la sessione principale (sotto, «Banco»).

## Piano della T4.8

La T4.8 ha 26 voci ART più RIP-H-24 (già portata con la 054) e RIP-S-16, RIP-S-17. Otto slice dopo questa (rinumerate dopo la review: scanner a parte, giri divisi in dominio e pagine). Le slice hanno la dimensione delle
ultime (070–075: una pagina o un'area con il suo dominio, i test e uno scenario del banco). L'ordine segue le
dipendenze: prima l'anagrafica (serve a tutto), poi la sessione di consegna (crea i giri), poi le stampe della
consegna e lo scanner, poi il rientro (chiude i giri), poi la tabella dei giri e le pagine dello storico, poi i solleciti
(leggono le consegne aperte), infine i link pubblici.

| Slice | Cosa | Voci | Dipende da | Banco | Rischi |
|---|---|---|---|---|---|
| **076** | anagrafica: `/settings/anagrafiche` con la sola parte «Artigiani» (le oreficerie con la T4.9), nuovo, modifica, eliminazione; telefono normalizzato come `normalizza_telefono` | ART-01…04 | — | HTTP `anagrafiche`, `anagrafiche-dipendente`; browser `anagrafica-artigiani` | basso. La pagina è a metà finché la T4.9 non porta le oreficerie (Dubbio 1) |
| **077** | consegne: `/handovers` (pulizia delle sessioni vuote dei giorni prima all'apertura, sessioni aperte oggi, menu degli artigiani con «(sessione aperta)», «Nessun artigiano in anagrafica.»), apertura o ripresa della sessione di oggi, `/handovers/{id}`: aggiunta per numero o QR (prima numero poi QR; **con la D-64 il numero scritto è quello dell'etichetta**, «0045/26» o «45», e `buste.NormalizzaNumero` della 220 per gli zeri), «↻ già in lista», riapertura pulita di una busta già rientrata, ✕ per togliere, «✓ Conferma senza stampare» con `handover.confirmed` e gli **id** delle buste, ritorno a `/?fuori_bottega=1` | ART-05, 06, 07 (senza il contatore dei solleciti), 08, 10, 11 | 076 | HTTP le due pagine sui dati della cattura (consegna 1 dell'artigiano 1, busta 4/26); browser: apertura, aggiunta per etichetta e per QR, togli, conferma | medio: «oggi» con l'orologio del negozio (mezzanotte locale, JOB-01); la scrittura «nascosta» all'apertura della pagina (ART-05, deciso così); i numeri della D-64 nel campo e nei messaggi («✓ Aggiunta busta #0045/26») vanno nel normalizzatore D-64 del banco |
| **078** | stampe della consegna: `GET /handovers/{id}/labels.pdf` (un'etichetta 76×127 per busta, quella della 043/058 con «→ artigiano» della sessione e «All'artigiano:» della D-73, per numero, cestino escluso, 404 «Consegna vuota o non trovata», 503 «PDF non disponibile», `handover.labels_generated` prima dei byte, `?download=`), «👁 Anteprima», «🖨 Ristampa», «📤 Condividi» e **«🖨 Stampa e conferma» con la D-09**: con una stampante scelta il lavoro va a `lp` (`internal/printing`, coda della 046 «etichetta busta») e la consegna si conferma **solo se il lavoro è accettato**; senza stampante, come lo standalone (PDF e conferma). | ART-12 (sostituita) | 077, 046 | HTTP il PDF (`pdf:testo`, normalizzatori D-64 e D-73) e i rifiuti: la stampa con i comandi finti (come `stampa-diretta`) | medio: mai code CUPS vere (comandi finti, SVILUPPO «Stampa diretta CUPS»); il lavoro fuori dalla transazione, poi conferma e audit insieme; la coda scelta per il 76×127 è la stessa dell'etichetta busta (da confermare nel brief) |
| **079** | scanner continuo con la fotocamera nella sessione: «📷 Scansiona», ogni codice aggiunto subito (prima QR poi numero, ordine opposto al campo), finestra con esito e conteggio «in consegna», suoni (solo una busta nuova suona successo), lista riletta a ogni scansione, assente su sessione confermata; `jsQR` e lettore della 052 | ART-09 | 077 | browser con la fotocamera finta (come `lettore-qr`) | medio: il lettore della home va riusato senza duplicarlo; gli eventi dei suoni |
| **080** | rientro: dominio `return_from_artisan/4` (esiti `fatto` → pronta con data, `da_rifare` → in lavorazione, vuoto; grafo degli stati controllato prima, «Da «X» non si passa a «Y»…», «Esito lavoro non valido.», «La riparazione non risulta fuori bottega.», nota ripulita o cancellata, chiusura della sessione con l'ultima busta fuori, `repair.returned_from_artisan`) **con `avvisaPronta` nella stessa transazione quando l'esito porta a «pronta»** (SVILUPPO, sezione 070b). Lo standalone accoda l'avviso **dopo il commit** (`announce_ready_by_email(:returned, …)`, `workshop.ex:509-510`): metterlo nella transazione è una scelta del Go, la stessa dei cambi di stato della 070b. L'email parte solo se lo stato **prima** del rientro non era già «pronta»; il riquadro «🔧 Fuori bottega presso …» della scheda con il modulo di rientro, l'esito avvisato prima (`applicable_outcomes/1`), «Ristampa» e «Scarica» dell'etichetta; verifica di ART-14/RIP-H-24 (già della 054) contro le regole di ART-13 (chiusura, nessun doppio rientro) | ART-13, ART-14 (chiusura), RIP-S-16; RIP-H-24 (solo ricontrollo) | 077, 070b | HTTP la scheda della 4/26 (busta fuori della cattura) prima e dopo; browser il rientro con esito «Fatto» nelle due app (lo stato, la sessione chiusa, l'email di pronta **non** confrontata: D-16 come la 070b) | alto: transazione con l'email di pronta (SAVEPOINT della 070b, `primaDelCommit` nei test), gara fra due rientri, la busta importata «uscita» senza sessione; i giri passati devono leggere `repair.returned_from_artisan` |
| **081** | giri, solo dominio (tema H): **tabella dei giri** (migrazione nuova), costruita dall'import e dalla migrazione stessa dagli eventi `handover.confirmed` (con `repair_ids`), `repair.returned_from_artisan*`, `handover.bag_detached`, poi scritta da conferma (077), rientro (080) e rientro automatico (054); `Workshop.artisan_cycles/1` letto dalla tabella; **prova di equivalenza** con la ricostruzione dall'audit dello standalone (`HandoverHistory.cycles/1`) sugli eventi della cattura e su casi inventati (riaperture, uscita sconosciuta, sganciate) | ART-19 (dominio) | 077, 080 | nessuno scenario nuovo (si vede con la 082); test dell'import sulla cattura | alto: stesso storico della ricostruzione; conservazione dell'audit di 5 anni (D-14) è il motivo della tabella |
| **082** | pagine dello storico e giri sulla scheda: `/handovers/history` (filtri GET, 30 per pagina, `handover.history_viewed` con i soli nomi dei filtri), dettaglio con le «riaperte», «✕ Elimina dallo storico» (che **prima sgancia i solleciti** della consegna, `solleciti_artigiani.consegna_artigiano_id` è `ON DELETE RESTRICT` dalla 0008, e le buste, anche nel cestino; `reminders_unlinked` nella riga `handover.deleted`, come `delete_handover_from_history`) e «✕ Sgancia» del titolare (`f.azioneDelTitolare`), che scrivono anche la tabella dei giri, riquadro dei giri sulla scheda | ART-15, 16, 17, 18, 19 (riquadro); RIP-S-17 (parte giri) | 081 | HTTP elenco, filtri, dettaglio (consegna 1 della cattura), rifiuti del dipendente; browser «Elimina dallo storico» e «Sgancia» con le conferme | medio: «riaperte» dalla tabella e non dall'audit; i solleciti sganciati restano (ART-22) |
| **083** | solleciti: `/handovers/reminders` (soglia `?giorni=` o `solleciti_artigiano_giorni`, 14 di default, 1…120, raggruppati per artigiano della **sessione**, `handover.reminders_viewed`), «⏰ Registra sollecito» con la conferma, «📱 WhatsApp sollecito» e «👥 Scegli contatto» (wa.me con il testo del Flask, **D-74**: la scheda va alla lista), una riga `artisan_reminders` e `repair.artisan_reminded` per busta; contatore «⏰ Solleciti pendenti» della lista consegne; riquadro dei solleciti sulla scheda | ART-07 (contatore), ART-20, 21, 22; RIP-S-17 (parte solleciti) | 077, 082 | HTTP la pagina con soglie diverse (numero non valido → 14), il contatore; browser il sollecito registrato e il link wa.me | medio: «giorni fuori» troncati con l'orologio del negozio; la stessa soglia servirà a JOB-09 (T4.9) |
| **084** | link e pagine pubbliche dell'artigiano: `/artigiano/{id}?t=` firmato dal Go (gettone del Go su una sottochiave sua, 90 giorni, non revocabile, come `ArtisanLink`; **anche i Phoenix.Token già mandati dallo standalone, verificati con il suo segreto: D-87, Stefano 14/11/2026**), proiezione senza dati del cliente, 404 «Riparazione non disponibile» uguale per ogni rifiuto, foto del canale artigiano (`?size=thumb`), audit, limite dei falliti della famiglia `artigiano` (CLI-21, già in `limiti.go`); **i vecchi link del Flask `/artigiano/{busta}/{anno}?t=<exp>.<16hex>`** con il segreto Flask della 064 e le foto per nome file; il link sulla scheda e `{link}` nel testo `wa_artigiano` | ART-23, 24, 25, 26; RIP-S-25 (della T4.5, ancora aperta: dipende da ART-23) | 064, 076 | HTTP le due pagine sui dati della cattura (link Flask della 4/26 firmato dal banco, come `pagina-storica`), i rifiuti identici, le foto; i link nuovi solo Go (D-02) | alto: **i link Flask già mandati non devono rompersi** (stesso HMAC, numero interno, D-64 studio: «link firmati sul numero interno invariati»); un gettone di un tipo non apre l'altra pagina; la faccia pubblica (Funnel) |

Note al piano:
- **Link già mandati.** Con la D-87 (Stefano, 14/11/2026, eccezione alla D-02 solo per gli artigiani) dopo il
  passaggio funzionano i vecchi link Flask (ART-26), i link `/artigiano/<id>?t=` dello standalone (Phoenix.Token, al
  più 90 giorni, verificati con il suo segreto) e quelli nuovi del Go; e le foto per nome file. Nessun link già mandato
  agli artigiani si rompe.
- **Rientro e email di pronta.** Nella 080 il rientro chiama `avvisaPronta(ctx, tx, …)` dentro la sua transazione, con
  il SAVEPOINT `email_pronta` (sezione 070b di SVILUPPO): il nuovo stato e l'email si scrivono insieme; un'email che
  non parte non toglie il rientro. Lo standalone accoda dopo il commit: nel Go è una scelta, come nella 070b. Niente email se la busta era già «pronta» prima del rientro. Anche il rientro automatico dal QR (054) passa già da `passaStato`.
- **Stampa.** 078 usa solo `internal/printing` (`Printer`, `printing.Finta` nei test, comandi finti nel banco); un
  lavoro accettato scrive conferma e audit in una transazione, un rifiuto non conferma (D-09).
- **Cosa c'è già nel Go:** le tabelle `artigiani`, `consegne_artigiani`, `solleciti_artigiani` (0007, 0008) e le colonne
  delle buste (`artigiano_id`, `consegna_artigiano_id`, `consegnata_artigiano_il`, `rientrata_artigiano_il`,
  `note_artigiano`), importate dalla mappa dell'importer; l'artigiano nel modulo della busta e nella modifica (`buste.Scelte`,
  `crea.go`, `modifica.go`); la pillola «fuori bottega» e il filtro (`indice.go`); il rientro automatico dal QR
  (`qr.go`, 054); l'etichetta 76×127 con «All'artigiano:» (043, 058); la stampa diretta (046); il testo `wa_artigiano`
  nell'editor (067); la famiglia `artigiano` del limite dei falliti (`limiti.go`); il segreto Flask (064). Manca la
  tabella dei giri (081).
- **Dove sta il codice:** nel pacchetto `internal/buste` (non `internal/artisans` del PIANO §2): consegne, rientri e giri
  scrivono nelle transazioni delle buste e il rientro chiama `avvisaPronta`, che non è esportata. La 076 ci mette
  l'anagrafica (`artigiani.go`); le pagine stanno in `internal/web` come le altre.

## Perché l'anagrafica per prima

È la base di tutto il resto (la sessione di consegna sceglie un artigiano dell'anagrafica, i solleciti usano il suo
telefono), non dipende da niente di nuovo e ha la dimensione delle ultime slice. L'altro primo passo possibile, la lista
delle consegne, avrebbe dovuto leggere un'anagrafica che il Go non sa ancora modificare. Scelta come consigliato dal
brief.

## Cosa c'è

- **Dominio** (`internal/buste/artigiani.go`):
  - `Artigiani` = `list_artisans/0`: tutta l'anagrafica `ORDER BY nome, id` (BINARY come SQLite dello standalone:
    maiuscole prima), telefono "" per NULL; nessun artigiano riservato.
  - `CreaArtigiano` = `create_artisan/2`: nome ripulito dagli spazi ai bordi (`strings.TrimSpace`, spazi Unicode come
    `String.trim/1`), obbligatorio anche se il campo manca (`ErrNomeArtigiano`, «Il nome è obbligatorio.»); telefono
    normalizzato (`NormalizzaTelefono`) o NULL (anche se il campo manca, come `Map.update/4` di `normalize_attrs/1`); il
    doppione lo dice l'indice unico, **a maiuscole esatte** (`*ArtigianoDoppio`, «Esiste già un artigiano con il nome
    "X".» con il nome ripulito, senza escape: `~s()` dello standalone); riga `artisan.created` senza dati.
  - `ModificaArtigiano` = `update_artisan/3`: artigiano riletto nella transazione («Artigiano non trovato.»), nome
    obbligatorio, doppione **senza distinguere le maiuscole** fra gli altri (`id != ? AND LOWER(nome) = LOWER(?)`: LOWER di
    SQLite, solo ASCII, come lo standalone), poi nome e telefono riscritti (un telefono che manca diventa NULL); riga
    `artisan.updated`.
  - `EliminaArtigiano` = `delete_artisan/2`: «Artigiano non trovato.», poi le buste (**anche nel cestino**,
    `with_discarded: true`) con «Impossibile eliminare: questo artigiano ha 1 riparazione associata.» / «N riparazioni
    associate.», poi le consegne (anche vuote, anche chiuse) con «… 1 sessione di consegna.» / «N sessioni di
    consegna.»; riga `artisan.deleted`.
  - `NormalizzaTelefono` = `Workshop.Phone.normalize/1`: tolti gli spazi ai bordi, un «+» in testa resta, poi solo le
    cifre che `str.isdigit` di Python accetta: `\p{Nd}` più la tabella `@python_digit_ranges` dello standalone (apici,
    pedici, cifre cerchiate, ecc.); "" senza cifre.
  - Ogni scrittura e la sua riga d'audit in una transazione (Ecto.Multi): senza audit niente cambia.
- **Pagina** (`internal/web/anagrafiche.go`, `pagine/anagrafiche.html`, CSS in `app.css`): `GET /settings/anagrafiche`
  (`rottaConGuscio`, `personale`, titolo «Anagrafiche»; la voce «Anagrafiche» del menu ☰ ora compare), con
  «Impostazioni» / «Anagrafiche», il riquadro `#artigiani` («Artigiani», la frase sui nomi collegati per ID), una riga
  per artigiano (`#artisan-<id>`, modulo `#edit-artisan-<id>` con «Nome» e «Telefono», «Salva», «Elimina»
  `#delete-artisan-<id>` con `data-confirm` «Eliminare <nome salvato>?»), «Nessun artigiano registrato.»
  (`#artisans-empty`, sempre nella pagina, `hidden` con delle righe, come `hidden only:block`), il modulo
  `#new-artisan-form` («Nuovo artigiano *», «Nome e cognome», «Telefono WhatsApp», «Es.: 3391234567», «+ Aggiungi»).
  «Profilo negozio» solo al titolare **e** solo quando `GET /settings/shop` è registrata (ADM-02 non c'è ancora: come
  le voci del menu, niente link verso un 404).
- **Azioni** (moduli POST al posto degli eventi LiveView):
  - `POST /settings/anagrafiche/artigiani` (`create_artisan`, `rottaConGuscio`): riuscita → 302 alla pagina con
    «Artigiano "X" aggiunto.»; doppione → pagina ridisegnata con il messaggio e i valori **scritti** (LiveView non tocca i
    campi); nome vuoto → pagina con «Il nome è obbligatorio.», «Campo obbligatorio.» sotto il nome e i valori del
    changeset (nome vuoto, telefono normalizzato); un modulo che non è la mappa `artisan` (nessun `artisan[…]`, oppure un
    `artisan` o `artisan[]`, che Plug farebbe testo o lista) → «Artigiano non aggiunto.».
  - `POST /settings/anagrafiche/artigiani/{id}` (`update_artisan`, `rottaConGuscio`): id non positivo (`Id.positive/1`)
    o modulo senza la mappa → «Artigiano non trovato.»; doppione o nome vuoto → pagina ridisegnata con il messaggio e i
    valori scritti nella riga; riuscita → 302 con «Artigiano aggiornato.».
  - `POST /settings/anagrafiche/artigiani/{id}/elimina` (`delete_artisan`, `f.rotta`): sempre 302 alla pagina con
    «Artigiano eliminato.» o il rifiuto. «Elimina» sta nel modulo della riga (`formaction`, `formnovalidate`): nome e
    telefono che arrivano non contano.
  - Tutte e quattro per il personale (righe nuove nella tabella dei permessi: «live /settings/anagrafiche
    (OreficerieLive)», eventi `create_artisan`, `update_artisan`, `delete_artisan`), con CSRF e sessione rilette a ogni
    richiesta come il resto.
- **Banco:** `bench/scenari/anagrafiche.json`, `anagrafiche-dipendente.json`, `bench/browser/anagrafica-artigiani.json`,
  la funzione `anagraficaArtigiani` in `tools/banco/browser/banco.mjs`; i test del banco (`TestScenariBrowserDelRepo`,
  `TestScenarioUtentiSuiDueDati`) li conoscono.

## Scelte

1. **Rifiuti ridisegnati, riuscite con il redirect.** Come le altre pagine LiveView portate (utenti, testi): dopo un
   rifiuto il browser deve rivedere i valori scritti, come in LiveView, quindi la pagina si ridisegna (200); una
   riuscita torna alla pagina (302) e il modulo nuovo è vuoto. L'eliminazione torna sempre con il redirect (non ci
   sono valori da tenere).
2. **Valori dopo un rifiuto.** Doppione alla creazione: i valori scritti (il modulo non è riassegnato). Nome vuoto
   alla creazione: `to_form(changeset)`, quindi nome vuoto, telefono normalizzato e l'errore di Ecto «Campo
   obbligatorio.» sotto il nome (`erroriCampo`, classe `field-error` delle altre pagine). Modifica rifiutata: i valori
   scritti in quella riga, senza errore sotto il campo (lo standalone mette solo il messaggio). La domanda di
   «Elimina» nomina sempre il nome salvato.
3. **Pagina con le sole righe degli artigiani.** Le oreficerie clienti (ORE-01…03) sono della T4.9; la pagina del Go
   non le mostra ancora (Dubbio 1). Il banco confronta solo la parte degli artigiani.
4. **Pacchetto.** Il dominio sta in `internal/buste`, accanto alle `Scelte` che già leggono gli artigiani (sopra,
   «Dove sta il codice»).
5. **Unicode delle cifre.** `\p{Nd}` è la tabella della versione Unicode di Go 1.27 (`unicode.Nd`); lo standalone usa
   quella del PCRE di Erlang. Le cifre aggiunte da poco (per esempio alcune scritture di Unicode 16) potrebbero
   contare in una e non nell'altra: nessuno le scrive in un telefono. Nessun Dubbio aperto.
6. **Un artigiano con i soli solleciti.** Nessun conteggio li guarda (come lo standalone): l'eliminazione arriva al
   `DELETE` e il vincolo `RESTRICT` la ferma. Lo standalone lì solleva un'eccezione (la pagina LiveView cade e si
   ricarica senza messaggio); il Go risponde con la pagina d'errore, niente cambia e il registro dice «artigiano non
   eliminato» (Dubbio 2).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-076/prove/`): `rosso-buste.log` con uno scheletro che non
  fa niente (tutti e sette i test di dominio rossi sull'asserzione; due rossi perché la busta di prova non trova
  l'artigiano che lo scheletro non crea); `rosso-web.log` senza le rotte (404 e, per l'eliminazione, il 403 del modulo
  della busta senza token).
- **Dominio** (`internal/buste/artigiani_test.go`): elenco e ordine (BINARY), creazione (nome vuoto, di soli spazi
  Unicode, che manca; telefono normalizzato, senza cifre, che manca; audit con attore), `NormalizzaTelefono` (gli esempi
  dello standalone, il «+» non in testa, i confini delle tabelle: «⑨» sì, «⑩» no, «⑴» sì, «⑽» no, le cifre a 32 bit),
  doppioni (esatti alla creazione, LOWER solo ASCII alla modifica: «èrre» e «Èrre» sono due nomi, «ÈRRE» è un doppione
  di «Èrre»; il proprio nome con altre maiuscole va bene), modifica (id che non c'è, nome vuoto, telefono che manca →
  NULL, le buste collegate per id), eliminazione (buste anche nel cestino, consegne aperte e chiuse, singolare e
  plurale, ordine dei controlli, id che non c'è), audit nella transazione (trigger `RAISE(ABORT)`).
- **Pagina** (`internal/web/anagrafiche_test.go`): markup intero della pagina vuota e piena (ordine, escape dei nomi,
  `hidden`), «Profilo negozio» (titolare con la rotta sì, dipendente no, senza rotta no), creazione, doppione, vuoto,
  moduli forgiati, modifica con gli id forgiati dello standalone (`invalid`, `0`, `-1`, 29 cifre, `999`) e i moduli senza
  mappa, eliminazione (rifiuto con il conteggio, id forgiati, i campi della riga ignorati), POST senza token (403).
- **Tabelle aggiornate:** permessi (`ruoli_test.go`), rotte con il guscio e menu (`guscio_test.go`: ora c'è
  «Anagrafiche» per titolare e dipendente), `busteFinte`.

## Mutazioni

37 mutazioni a mano sul codice nuovo (dominio, pagina, modello), una alla volta su una copia del repo
(`to-be-deleted-after-use/slice-076/mutazioni/`: `muta.py`, `giro.log`, `esiti.json`, `esiti/<id>.log`; copia cancellata
a fine giro). **37 prese su 37**, tutte da un'asserzione. Tre (W02, W07, W14) al primo giro non compilavano (variabile
non usata): riscritte per compilare e rifatte nel secondo giro (`giro.log`, `esiti-giro1.json`), prese.

| Id | Mutazione | Presa da |
|---|---|---|
| B01 | solo `\p{Nd}`, senza le cifre di Python | `TestNormalizzaTelefono` |
| B02 | solo le cifre di Python, senza `\p{Nd}` | `TestNormalizzaTelefono`, `TestCreaArtigiano`, altri |
| B03 | il «+» in testa non resta | `TestNormalizzaTelefono`, `TestCreaArtigiano` |
| B04 | telefono senza `TrimSpace` (il «+» dopo uno spazio) | `TestNormalizzaTelefono`, `TestCreaArtigiano` |
| B05 | senza cifre resta il «+» | `TestNormalizzaTelefono` |
| B06 | tabella delle cerchiate fino a ⑩ | `TestNormalizzaTelefono` |
| B07 | nome non ripulito | `TestCreaArtigiano`, `TestNomiDoppiArtigiani`, `TestModificaAnagraficaArtigiano` |
| B08 | modifica senza telefono tiene quello di prima (`coalesce`) | `TestModificaAnagraficaArtigiano` |
| B09 | doppione della modifica a maiuscole esatte | `TestNomiDoppiArtigiani` |
| B10 | doppione della modifica contro sé stesso | `TestNomiDoppiArtigiani`, `TestModificaAnagraficaArtigiano` |
| B11 | modifica senza «Artigiano non trovato.» | `TestModificaAnagraficaArtigiano` |
| B12 | buste nel cestino non contate | `TestEliminaArtigiano` |
| B13 | solo le consegne aperte contate | `TestEliminaArtigiano` |
| B14 | eliminazione senza `artisan.deleted` | `TestEliminaArtigiano`, `TestArtigianiAuditNellaTransazione` |
| B15 | creazione con `artisan.updated` | `TestCreaArtigiano` |
| B16 | singolare di «riparazione associata» sbagliato | `TestEliminaArtigiano` |
| B17 | singolare di «sessione di consegna» sbagliato | `TestEliminaArtigiano` |
| B18 | doppione della creazione non riconosciuto | `TestNomiDoppiArtigiani` |
| B19 | elenco per id | `TestElencoArtigiani` |
| B20 | conteggio delle buste sbagliato | `TestEliminaArtigiano` |
| B21 | consegne non controllate | `TestEliminaArtigiano` |
| W01 | `artisan` testo insieme ad `artisan[name]` vale come mappa | `TestNuovoArtigianoDallaPagina` |
| W02 | ogni modulo vale come mappa | `TestNuovoArtigianoDallaPagina`, `TestModificaArtigianoDallaPagina` |
| W03 | doppione: nome ripulito nel modulo invece di quello scritto | `TestNuovoArtigianoDallaPagina` |
| W04 | nome vuoto senza l'errore sotto il campo | `TestNuovoArtigianoDallaPagina` |
| W05 | nome vuoto: telefono scritto invece di quello normalizzato | `TestNuovoArtigianoDallaPagina` |
| W06 | modifica rifiutata senza i valori scritti nella riga | `TestModificaArtigianoDallaPagina` |
| W07 | modifica senza la mappa `artisan` accettata | `TestModificaArtigianoDallaPagina` |
| W08 | rifiuto dell'eliminazione come esito verde | `TestEliminaArtigianoDallaPagina` |
| W09 | «Profilo negozio» anche al dipendente | `TestAnagraficheProfiloNegozio` |
| W10 | «Profilo negozio» senza la sua rotta | `TestPaginaAnagrafiche` |
| W11 | `hidden` di «Nessun artigiano registrato.» rovesciato | `TestPaginaAnagrafiche` |
| W12 | domanda di «Elimina» con il nome scritto | `TestModificaArtigianoDallaPagina` |
| W13 | creazione solo del titolare | `TestOgniRottaHaIlSuoPermesso`, `TestNuovoArtigianoDallaPagina` |
| W14 | messaggio con il nome non ripulito | `TestNuovoArtigianoDallaPagina` |
| W15 | valori scritti messi in ogni riga | `TestModificaArtigianoDallaPagina` |
| W16 | «Elimina» senza `formnovalidate` | `TestPaginaAnagrafiche`, `TestModificaArtigianoDallaPagina` |

## D nuove da approvare

- **D-88, approvata** (Stefano, 14/11/2026; riga già nell'inventario): dopo un «+ Aggiungi» riuscito il Go svuota il
  modulo, come vuole il codice dello standalone; nello standalone il nome scritto resta nel campo (effetto di LiveView),
  salvo subito dopo un rifiuto. Il Go la faceva già; `TestNuovoArtigianoDallaPagina` controlla nome e telefono vuoti
  dopo la creazione. **Il banco la usa:** due righe in `bench/deviazioni.json` (livello browser,
  `anagrafica-artigiani`, passi 0 e 1, `crea.campo-nome`: standalone `"  Artigiano Banco <vista>  "`, Go `""`), e
  `crea.campo-nome` non è più fra le attese dello scenario; `TestScenariBrowserDelRepo` conosce le due righe.
- La D-87 (link Phoenix.Token degli artigiani) è della 084, non di questa slice.

## Banco

**Giro finale valido `s076q`** (sessione principale, binari dell'albero finale dopo `scripts/check` verde): **BANCO
VERDE**, HTTP 68 scenari, 1170 passi, 1548 differenze attese, 0 inattese, 0 errori, non provati 0; **BANCO BROWSER
VERDE**, 18 scenari, 36 viste, 32 differenze attese (le 2 nuove «ATTESA (D-88)» su `crea.campo-nome`), 0 inattese,
0 errori; `banco giu`: «AVANZI: nessuno». Il primo giro `s076p` (qui sotto) aveva trovato la scritta del vuoto
visibile nel Go, un'attesa sbagliata e la differenza del campo nome, poi decisa da Stefano (D-88).

**Da fare dalla sessione principale** (qui non lanciato: Docker e la memoria di pc-fisso li gestisce la sessione
principale). Nessuna D nel banco: le parti che il Go non ha ancora (riquadro delle oreficerie, «Profilo negozio» del
titolare) non si confrontano.

- **HTTP**, due scenari nuovi:
  - `anagrafiche` (ART-01), 3 passi: login del titolare e `GET /settings/anagrafiche` con `visibile:artigiani/h2`,
    `visibile:artisans-empty`, i testi del riquadro e del modulo nuovo, gli attributi dei campi (`placeholder`, `name`,
    `type`, `required`, `inputmode`), la prima riga (`artisan-name-1`, `artisan-phone-1`, `delete-artisan-1` con
    `data-confirm`) e «Incastonatore Sintetico», le intestazioni. Sul database vuoto la riga 1 è assente; sui dati della
    cattura «Laboratorio Sintetico», «+390000000201», «Eliminare Laboratorio Sintetico?».
  - `anagrafiche-dipendente` (ART-01, ACC-05), 3 passi: login del dipendente e la pagina: 200, «Artigiani», nessun
    «Profilo negozio» nelle due app.
  - Ci si aspetta: **HTTP 68 scenari, 1170 passi** (66 e 1164 della 075, più 2 e 6), 0 inattese, 0 errori.
- **Browser**, uno scenario nuovo, `anagrafica-artigiani` (ART-01…04), sulle due viste: sul database vuoto solo la
  pagina (vuota); sui dati della cattura: nuovo artigiano «Artigiano Banco <vista>» con spazi e telefono
  «+390000760001», con un **doppio clic** su «+ Aggiungi» (un invio solo: `crea.errore` «(nessuno)», `artisan.created`
  +1), doppione alla creazione (valori scritti tenuti, anche il telefono « +39 333-1 »: `doppione.campo-telefono`), nome di soli spazi (errore sotto il campo),
  modifica nel nome del primo artigiano per nome («Incastonatore Sintetico») in maiuscolo (doppione, con il telefono
  « +39 333-1 » che resta scritto nella riga: `modifica-doppione.campo-telefono`), modifica valida
  (telefono «021234»), «Elimina» rifiutato su «Incastonatore Sintetico» («1 riparazione associata», la 6/26 della
  catena; «Laboratorio Sintetico» no: `busta-etichetta` crea delle buste con l'artigiano 1 solo nel Go), «Elimina»
  dell'artigiano nuovo, registro (`artisan.created`, `updated`, `deleted` +1 ciascuno). Alla fine l'anagrafica è com'era.
  Ci si aspetta: **browser 18 scenari, 36 viste**, 0 inattese.
- Comandi come nella ricevuta 074b (`banco su … -dati sintetici-import`, `banco esegui`, `banco giu`), con i binari
  nuovi (anche `banco`: i test del banco conoscono gli scenari nuovi).

**Giro `s076p`** (sessione principale, binari dell'albero dopo la review): HTTP verde (68 scenari, 1170 passi, 0
inattese); browser **10 inattese, tutte in `anagrafica-artigiani`**, uguali sulle due viste:
- `pagina.vuoto` (attesa `false`): standalone «false», Go «true». **Difetto vero del Go.** Il modello mette `hidden` su
  `<p id="artisans-empty" class="sotto">`, ma in `app.css` `.sotto { display: block }` viene dopo `[hidden] { display:
  none }` con la stessa specificità e vince: la scritta «Nessun artigiano registrato.» si vedeva anche con delle righe
  (lo standalone la nasconde con `hidden only:block`, e Playwright `isVisible()` lo misura). **Correzione:**
  `#artisans-empty[hidden] { display: none; }` dopo `.sotto`; `TestAnagraficheVuotoNascostoDalCSS` lo fissa
  (mutazioni S01 regola tolta, S02 `display: block`: **2 prese su 2**, `to-be-deleted-after-use/slice-076/mutazioni-s076p/`,
  copia cancellata). Nessun'altra pagina ha `class="sotto"` con `hidden`.
- `pagina.righe` (attesa `0`): le due app «2» (Incastonatore e Laboratorio Sintetico). **Attesa sbagliata**: l'«atteso»
  del database vuoto valeva anche per i dati; ora `atteso_dati` dice «2».
- `crea.campo-nome` (attesa `""`): standalone «"  Artigiano Banco desktop  "», Go «""». **Effetto di LiveView** nello
  standalone (Dubbio 6): Stefano ha scelto il modulo vuoto, **D-88**. Il Go non cambia; la differenza è attesa con
  due righe D-88 in `bench/deviazioni.json`. L'osservazione `doppione.campo-nome` resta: il passo dopo riscrive il
  campo con `fill` dalle due parti, quindi il valore lasciato dallo standalone non conta e le due app concordano.

Cosa ci si aspetta al prossimo giro: HTTP come sopra (68 scenari, 1170 passi, 0 inattese); browser 18 scenari, 36
viste, 0 inattese, **2 attese D-88** (`crea.campo-nome`, desktop e telefono).

Se qualcosa è rosso, da guardare per primo:
- `vuoto.campo-nome` e `vuoto.campo-telefono` (browser): dopo il nome vuoto lo standalone riassegna il modulo dal
  changeset; il Go ridisegna nome vuoto e telefono normalizzato («"3330"»). Se LiveView lasciasse nei campi il testo
  scritto (non misurato), la differenza sarebbe nel Go, da allineare (niente D: è un dettaglio di come la pagina
  ridisegna).
- `vuoto.sotto-il-campo`: lo standalone mette l'icona e «Campo obbligatorio.» in un `<p>` dentro il modulo; il Go un
  `<p class="field-error">`. Il testo letto è lo stesso se nel modulo non ci sono altri `<p>`.
- `elimina-rifiutato.errore` («… 1 riparazione associata.») dipende dalla semina: conta le buste di «Incastonatore
  Sintetico» (la sola 6/26 della catena). Uno scenario HTTP o browser futuro che gli assegni una busta in una sola app,
  o in tutte e due, lo cambia: è fragile per costruzione, non un errore del Go (review della 076, B6).
- Il banco HTTP sui dati della cattura dà per scontato che l'artigiano 1 sia «Laboratorio Sintetico» con
  «+390000000201» nelle due app (cattura sintetica, riga `artigiani` id 1).

## Controlli

`scripts/check` verde (`to-be-deleted-after-use/slice-076/prove/check2.log` e, con la ricevuta e SVILUPPO finiti,
`check3.log`; dopo le correzioni della review `check4.log`, dopo quelle del banco s076p `check5.log`, con la D-88 `check6.log`, «uscita 0»): gofmt, vet, staticcheck, errcheck, build senza CGO, test, copertura **voci 353: chiuse 38,
rosse 0, parziali 139, aperte 165, fuori v1 11**, segreti e dati veri OK. Il primo giro (`check1.log`) si era fermato su
gofmt (una riga vuota in fondo a un test). ART-01…04 passano da aperte a parziali; si chiudono con l'esito verde del
banco (`GESTRIP_BANCO_ESITO`), che `scripts/check` qui non ha.

## Review

Review indipendente: nessuna ALTA, 1 MEDIA, 6 BASSE; più quattro punti sul piano. Tutto corretto nella slice.

- **MEDIA M1 — doppio invio.** I moduli di riga e `#new-artisan-form` non avevano `data-invio-unico`: un doppio tocco su
  «+ Aggiungi» creava l'artigiano e poi mostrava «Esiste già…» (LiveView spegne i pulsanti finché il server risponde).
  Ora tutti e due i moduli lo hanno (`app.js` spegne i pulsanti al primo invio); `TestPaginaAnagrafiche` fissa
  l'attributo su tutti e due; nel banco browser il passo `crea` usa un doppio clic.
- **B1 (R01):** `TestModificaAnagraficaArtigiano` prova l'id 999 con il nome vuoto: «Artigiano non trovato.» prima del
  nome.
- **B2 (R11, R12):** doppione della creazione e modifica rifiutata con il telefono « +39 333-1 »: il valore ridisegnato è
  quello scritto (non normalizzato). Stesse osservazioni nel banco browser (`doppione.campo-telefono`,
  `modifica-doppione.campo-telefono`): nelle due app LiveView non tocca i campi dopo un rifiuto che dà solo il
  messaggio, il Go ridisegna i valori scritti, quindi devono concordare (non misurato: da guardare se rosso).
- **B3 (R13):** `artisan[name]=Primo Ripetuto&artisan[name]=Secondo Ripetuto` crea «Secondo Ripetuto» (l'ultimo, come
  Plug).
- **B4 (R14):** `TestPaginaAnagrafiche` controlla l'`_csrf_token` dentro `#new-artisan-form`.
- **B5 (R16, R17):** `TestArtigianiGuastoDelDatabase`: un artigiano con una sola riga in `solleciti_artigiani` (né buste
  né consegne): «Elimina» dà 500, niente cambia, «artigiano non eliminato» nel registro (Scelta 6, Dubbio 2); un guasto
  della modifica (trigger `RAISE(ABORT)` su `UPDATE artigiani`) dà «Artigiano non trovato.», niente cambia, «artigiano
  non modificato» nel registro.
- **B6:** nota sulla fragilità di `elimina-rifiutato.errore` nella sezione Banco.
- **Piano:** 080 diventata 082 con lo sgancio dei solleciti prima dell'eliminazione dallo storico (e Dubbio 2
  aggiornato); rientro (ora 080) con l'avviso dopo il commit nello standalone e l'email solo se la busta non era già
  «pronta»; scanner (ART-09) in una slice a parte (079); giri divisi in tabella con la prova di equivalenza (081) e
  pagine (082); slice rinumerate fino alla 084; promemoria del cutover nei Dubbi (Dubbio 5).

**Mutazioni dopo la review** (`to-be-deleted-after-use/slice-076/review/`: `muta.py`, `giro.log`, `esiti.json`,
`esiti/<id>.log`; copia cancellata a fine giro): **8 prese su 8**, tutte da un'asserzione.

| Id | Mutazione | Presa da |
|---|---|---|
| R01 | modifica: nome controllato prima dell'esistenza | `TestModificaAnagraficaArtigiano` |
| R11 | doppione della creazione: telefono normalizzato nel modulo | `TestNuovoArtigianoDallaPagina` |
| R12 | modifica rifiutata: telefono normalizzato nella riga | `TestModificaArtigianoDallaPagina` |
| R13 | campo ripetuto: vale il primo | `TestNuovoArtigianoDallaPagina` |
| R14 | `#new-artisan-form` senza `_csrf_token` | `TestPaginaAnagrafiche` |
| R16 | guasto dell'eliminazione: ritorno con «Artigiano non trovato.» invece del 500 | `TestArtigianiGuastoDelDatabase` |
| R17 | guasto della modifica non scritto nel registro | `TestArtigianiGuastoDelDatabase` |
| R18 | moduli senza `data-invio-unico` | `TestPaginaAnagrafiche` |

## Per il manuale

- In ☰ c'è «Anagrafiche»: per ora la pagina ha solo gli **artigiani** (le oreficerie clienti arrivano dopo).
- Si aggiunge un artigiano con nome (obbligatorio) e «Telefono WhatsApp» (facoltativo): il telefono si salva con le
  sole cifre e il «+» davanti, se c'era.
- Ogni riga si modifica sul posto («Salva»); cambiare il nome lo cambia anche sulle buste passate.
- Due artigiani non possono avere lo stesso nome; in modifica conta anche lo stesso nome scritto con altre maiuscole.
- «Elimina» chiede conferma e non elimina un artigiano che ha delle buste (anche nel cestino) o delle consegne: il
  messaggio dice quante.
- Titolare e dipendenti possono usare la pagina.
- Un doppio tocco su «+ Aggiungi» o «Salva» manda un invio solo.

## Dubbi

1. **Pagina a metà fino alla T4.9.** Lo standalone ha nella stessa pagina le oreficerie clienti (ORE-01…03) e il link
   «Profilo negozio» (ADM-02), entrambi della T4.9. Il Go mostra solo gli artigiani e non mostra il link finché
   `/settings/shop` non c'è. **Raccomandazione:** lasciarlo così (il Go non è ancora in uso); la prima slice della T4.9
   aggiunge il riquadro `#oreficerie` prima di `#artigiani` e lo scenario del banco lo confronta.
2. **Artigiano con i soli solleciti.** L'eliminazione cade sul vincolo del database (sopra, Scelta 6): pagina d'errore
   nel Go, eccezione nello standalone. Succede solo dopo che le sue consegne sono state eliminate dallo storico (ART-17)
   e le sue buste passate a un altro artigiano. Con la 082 l'eliminazione di una consegna dallo storico sgancia prima i suoi solleciti, come lo standalone: i solleciti restano, con l'artigiano, quindi il caso resta possibile. **Raccomandazione:** lasciarlo come lo standalone per ora; con la 083
   (solleciti) decidere se contare anche i solleciti con un messaggio («… ha N solleciti registrati»), che sarebbe una D.
3. **Link dello standalone agli artigiani.** **Deciso da Stefano il 14/11/2026:** eccezione alla D-02 solo per gli
   artigiani (D-87): il Go accetta anche i link `/artigiano/<id>?t=` dello standalone (Phoenix.Token), verificati con
   il `secret_key_base` dello standalone sull'apparecchio, oltre ai vecchi link Flask. Si fa con la 084.
4. **RIP-S-25 (link per l'artigiano sulla scheda)** è nella T4.5 del PIANO ma è ancora aperta e dipende da ART-23.
   **Raccomandazione:** spostarla nella 084 (scritto nel piano).
5. **Promemoria del cutover (D-02).** **Chiuso da Stefano il 14/11/2026:** con la D-87 i link dello standalone agli
   artigiani restano validi, quindi l'elenco delle buste da rimandare non serve.
6. **Il nome resta nel campo dopo «+ Aggiungi» riuscito (banco `s076p`, `crea.campo-nome`).** **Chiuso: Stefano,
   14/11/2026, ha scelto la (b), D-88** (il Go svuota il modulo; sopra, «D nuove»). Prove, dalla lettura del
   codice (non misurate di nuovo nel browser):
   - `create_artisan` riuscito fa `assign(:new_artisan_form, new_artisan_form())`: l'**intento** della pagina è il
     modulo vuoto, come nel Go;
   - `.input` di `core_components.ex` scrive `value={Phoenix.HTML.Form.normalize_value(@type, @value)}`: per un modulo
     nuovo il valore è `nil` prima (pagina appena aperta) e `nil` dopo. Il tracciamento dei cambi di HEEx (LiveView
     1.2.11 nel `mix.lock`) non manda nessuna differenza per l'attributo, l'`<input>` non si ritocca e il browser tiene
     quello che si era scritto. È il noto «il modulo LiveView non si svuota se il valore del server non cambia»;
   - quindi **non** è il focus (il focus è sul pulsante) né il doppio clic (con un clic solo vale lo stesso
     ragionamento): è un effetto di LiveView, ma deterministico, e lo vedono tutti;
   - dipende da cosa c'era prima: dopo un rifiuto per nome vuoto (modulo ridisegnato dal changeset: nome `""`,
     telefono normalizzato) il valore del server cambia, e un «+ Aggiungi» riuscito subito dopo **svuota** i campi.
   Opzioni: (a) copiare il caso comune: dopo una creazione riuscita il Go ridisegna nel modulo nuovo i valori scritti
   (nome e telefono come scritti), tranne subito dopo un rifiuto per nome vuoto (per esserlo del tutto servirebbe
   ricordare quel rifiuto); (b) tenere il modulo vuoto, che è quello che il codice dello standalone vuole, con una D
   nuova (è la D-88). **Raccomandazione:** (b): con il nome rimasto nel campo un secondo «+ Aggiungi» dà «Esiste
   già…», ed è proprio il fastidio che la M1 della review ha tolto; se si sceglie (a), è una modifica di poche righe in
   `creaArtigiano` (i valori passano dal redirect come il messaggio). In tutti e due i casi l'osservazione del banco
   cambia di conseguenza (con (b) va tolta, o diventa una differenza con la D approvata).

