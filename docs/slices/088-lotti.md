# 088 — Dominio dei lotti e `/ready-lots` (T4.9, terza)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/ready_lots.ex` (`open_lot/2`,
  `list_open_lots/0`, `purge_stale_empty_lots/0`, `b2b_goldsmith/2`, `reusable_lot/2`, `insert_lot/3`, `mint_token/0`,
  `open_today/0`, `start_of_today/0`, `lots_holding_a_bag/0`, `refusal_message/1`), `workshop/ready_lot.ex`
  (`open_query/1`), `workshop/oreficeria.ex` (`is_b2b/1`), `id.ex`, `ready_lots_live.ex` (mount, evento `open`,
  `goldsmith_param/1`, `render/1`); test `ready_lots_test.exs` («opening a lot», «the stale purge») e
  `ready_lot_live_test.exs` («the list page»).
- **Voci:** ORE-04, ORE-05 (portate; si chiudono con l'esito verde del banco). Deviazione nuova **D-94** (approvata da
  Stefano il 15/11/2026, riga dell'inventario del coordinatore). La tabella `lotti_avviso` c'è dalla 0007 (token unico,
  `ON DELETE RESTRICT` dalle buste): nessuna migrazione.
- **Lavoro:** sottoagente d'implementazione (Opus), test rossi prima; mutazioni e revisione di un revisore Opus; banco
  della sessione principale (s088a, s088b). Commit solo locale (decisione del coordinatore del 16/11/2026).

## Cosa c'è

- **Dominio** (`internal/buste/lotti.go`, nuovo):
  - `PulisciLottiVuoti` (= `purge_stale_empty_lots/0`): `DELETE` dei lotti aperti (né inviati né chiusi) creati prima
    della mezzanotte del negozio (`inizioDiOggi`, la stessa delle consegne) senza nessuna busta, anche nel cestino
    (`NOT EXISTS`: la trappola del `NOT IN` con i NULL non c'è). Dà quanti; nessuna riga d'audit.
  - `LottiApertiOggi` (= `list_open_lots/0`): lotti aperti con `creato_il >= mezzanotte`, dal più recente, con il nome
    dell'oreficeria, le buste fuori dal cestino e l'istante d'apertura.
  - `ApriLotto` (= `open_lot/2`): id ≤ 1 → `ErrLottoOreficeriaPrincipale` («Non si può creare un avviso per
    l'oreficeria principale.»), oreficeria che non c'è → `ErrLottoOreficeriaNonTrovata` («Oreficeria non trovata.»);
    poi, **nella stessa transazione di scrittura** (D-94), il lotto aperto di oggi più recente dell'oreficeria o uno
    nuovo con il gettone (24 byte di `crypto/rand`, base64 url-safe senza «=», 32 caratteri) e la riga
    `ready_lot.opened` (`ready_lot`, id del lotto, `{"oreficeria_id": N}`). Se l'audit non si scrive il lotto non c'è.
- **Pagina** (`internal/web/lotti.go`, `pagine/lotti.html`, `statici/app.css` `.lotti-riga`): `GET /ready-lots` (pulizia,
  poi la lista) e `POST /ready-lots` (`rottaConGuscio`, `personale`). Titolo «Avviso oreficeria», `📦 Avviso oreficeria`,
  la frase, il modulo `#ready-lot-open-form` con «Oreficeria» / «Scegli un'oreficeria» / le oreficerie clienti per nome
  (`Oreficerie`, senza la riga 1) e «Apri lotto», «Nessuna oreficeria in anagrafica. Aggiungine una.», «Lotti aperti
  oggi» con «Nessun lotto aperto oggi.» o le righe `#ready-lot-<id>` («<oreficeria> — lotto #N · 1 busta / N buste ·
  aperto gg/mm/aaaa hh:mm» sull'orologio del negozio, «Apri» verso `/ready-lots/<id>`). Rifiuti della pagina:
  «Seleziona un'oreficeria.» (campo vuoto, assente, `lot` non mappa, valore non testo) e «Oreficeria non valida.»
  (`idPositivo`). La voce di menu «📦 Avvisi oreficeria» compare da sola ora che la rotta c'è.
- **Tabelle aggiornate:** permessi (`ruoli_test.go`: ORE-04, ORE-05, `personale`), menu e rotte con il guscio
  (`guscio_test.go`), interfaccia delle buste (`nuova.go`) e `busteFinte`, titolo del modello (`guscio.go`), rotte
  (`salute.go`).
- **Documenti:** sezione in `docs/SVILUPPO.md` («Rotte», dopo il rientro); idea D in `docs/PIANO.md` («Idee per
  l'interfaccia», dopo la v1).
- **Banco** (non lanciato): HTTP `lotti` e `lotti-dipendente` (GET senza accesso → `/login`; la lista per titolare e
  dipendente); browser `lotti-apertura` (sotto, «Banco»). Per l'ordine dei nomi `anagrafica-oreficerie` sul telefono vede
  anche l'oreficeria di `lotti-apertura` del PC: `pagina.righe` 6 (revisione, M2).

## Scelte

1. **Stesso schema della 077 (consegne):** senza LiveView l'evento `open` è un POST; riuscita → 302 a `/ready-lots/<id>`
   (push_navigate), rifiuto → la lista ridisegnata (200) con il messaggio e il menu su «Scegli un'oreficeria»: ogni
   rifiuto viene da un valore che il menu non offre, quindi non c'è una scelta da tenere (revisione, B1). L'etichetta
   «Oreficeria» avvolge il menu, come `.input type="select"` dello standalone (banco s088a).
2. **Pulizia sulla GET.** Lo standalone pulisce solo a LiveView collegata (`connected?`), non nel rendering statico; il Go
   ha solo la GET e pulisce lì, una volta per caricamento, mai dopo un rifiuto. Nella pagina non cambia niente (mostra
   solo i lotti di oggi); nel banco HTTP lo standalone non pulisce e il Go sì, ma nessuno scenario legge i lotti vecchi.
3. **Un'occhiata sola all'orologio** in `ApriLotto` (revisione, B2): la mezzanotte di «oggi» e l'istante del lotto nuovo
   vengono dalla stessa lettura. Pulizia e lista leggono l'orologio una volta ciascuna.
4. **D-94 senza codice in più:** la scrittura del Go ha una sola connessione e `BEGIN IMMEDIATE`, quindi leggere il
   lotto di oggi e inserirlo nella stessa transazione basta (come `ApriConsegna`). Il test lancia 12 aperture insieme:
   un lotto, una riga d'audit.
5. **Guasto del database** (pulizia, lettura, apertura): pagina d'errore (500) con una riga nel registro («lotti vuoti non
   puliti», «lotti aperti non letti», «oreficerie dei lotti non lette», «lotto non aperto»). Nello standalone la LiveView
   cade senza un messaggio suo: nessun testo inventato. Il banco non lo può osservare.
6. **Id oltre 2⁶³−1:** «Oreficeria non valida.» come `Id.positive/1` (stesso tetto).
7. **«Storico dei lotti inviati»** compare quando c'è la rotta `GET /ready-lots/history` (093), come lo storico delle
   consegne nella 077: niente link verso un 404. Il banco HTTP per ora non lo confronta.
8. **Decisione I** (ritardi in giorni di calendario): non tocca la 088; vale per le slice dei lavori (094–096).

## Per il manuale

- Nel menu ☰ c'è «📦 Avvisi oreficeria», per titolare e dipendente: la pagina «Avviso oreficeria» prepara un lotto di buste
  pronte per un'oreficeria cliente.
- Si sceglie l'oreficeria dal menu e si tocca «Apri lotto»: se per quell'oreficeria c'è già un lotto aperto oggi, si
  riapre quello; altrimenti se ne crea uno nuovo.
- Sotto, «Lotti aperti oggi» elenca i lotti di oggi non ancora inviati, con il numero del lotto, quante buste ha e l'ora
  di apertura, e il pulsante «Apri».
- Un lotto vuoto dei giorni prima sparisce da solo quando si apre la pagina. Un lotto dei giorni prima che ha delle buste
  non compare più nell'elenco di oggi.
- Senza oreficerie in anagrafica la pagina lo dice e porta ad Anagrafiche per aggiungerne una.

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-088/prove/`), con il dominio e l'interfaccia in stub:
  `rosso-dominio.log` (8 test su 8 rossi sull'asserzione), `rosso-web.log` (3 su 4 rossi; `TestRifiutoLottoNonPulisce` era
  già verde perché la rotta non c'era e niente cancellava: protegge dal futuro).
- **Dominio** (`internal/buste/lotti_aperti_test.go`): apertura nuova (gettone di 32 caratteri url-safe, istanti, campi
  vuoti, audit con attore e `oreficeria_id`) e ripresa senza un'altra riga, un'altra oreficeria con il suo gettone;
  rifiuti (1, 0, negativo, che non c'è) senza scritture; inviato o chiuso non si riusa, nemmeno un lotto di ieri con
  buste (23:59:59,999999 di Roma), fra due di oggi il più recente; **mezzanotte del negozio con l'orologio finto alle
  00:30 di Roma** (23:59 di ieri no, 00:00 esatte sì); audit nella transazione (trigger `RAISE(ABORT)`); D-94 con 12
  aperture insieme; pulizia (vuoti dei giorni prima, anche un microsecondo prima di mezzanotte; restano: con busta, con
  busta nel cestino, inviato vuoto, mezzanotte, oggi; busta in nessun lotto; nessun audit; seconda pulizia 0); lista
  (ordine, nomi, conto senza cestino, esclusi inviati e quelli di ieri anche pieni, mezzanotte inclusa, istante).
- **Pagina** (`internal/web/lotti_test.go`): markup vuoto e pieno (menu per nome con escape, riga 1 assente, righe dal più
  recente con «1 busta»/«0 buste» e l'ora di Roma 09:05 e 00:00, nessun link allo storico) e pulizia alla GET; i 14
  rifiuti (moduli forgiati compresi) con la lista ridisegnata, il menu senza scelta, apertura con spazi, ripresa, audit con
  l'utente, 403 senza token; nessuna pulizia dopo un rifiuto; i quattro guasti (500 e registro).
- **Banco (solo controlli senza istanza):** `go test ./tools/banco` verde; `node --check` su `banco.mjs`.

## Mutazioni

Revisore Opus: 30 mutazioni, 29 compilate (la 18 non compila), **28 uccise**. Sopravvissuta la 04 («aperto» ridotto a
`inviato_il IS NULL`): equivalente, perché il CHECK della 0007 (riga 102) vuole `inviato_il` per ogni lotto chiuso. Non
tentate le due mutazioni «fuori dalla transazione» della D-94: con una sola connessione di scrittura il test andrebbe in
timeout. La D-94 è coperta dal test con 12 aperture insieme e da quello del trigger sull'audit.

## Revisione

Revisore Opus indipendente, due fasi. Nessun rilievo ALTO. MEDI negli scenari: M1 (sul telefono il lotto del PC è già in
lista: `apri.righe-prima` e `pagina.vuoto`) e M2 (`anagrafica-oreficerie` telefono `pagina.righe` 6). BASSI: B1 (la
«scelta tenuta» non si raggiungeva: tolta, test sul menu senza `selected`) e B2 (orologio letto una volta in
`ApriLotto`). Tutti chiusi; seconda fase senza rilievi sulle correzioni.

## Controlli

`scripts/check` verde (implementatore: `prove/check.log` e, dopo le correzioni, `prove/check-2.log`; rilanciato dalla
sessione principale). Copertura senza l'esito del banco: voci 353, chiuse 42, rosse 0, parziali 166, aperte 134, fuori v1 11.

## Banco

Sessione principale, con il «memoria libera» del coordinatore.

- **`s088a`** (16/11/2026): HTTP 80 scenari, 1287 passi, 4 inattese; browser 26 scenari, 52 viste, 15 inattese. Inattese sui lotti tutte nelle attese o nello scenario, nessuna differenza fra le app; le 4 di
  `profilo-negozio` sono il rosso noto della D-93. Correzioni:
  - HTTP `lotti`, `lotti-dipendente` [2]: dopo l'accesso tutte e due le app tornano a `/ready-lots` (la pagina chiesta
    prima del login), non a «/»: attesa corretta.
  - HTTP [3]: il menu ha anche «Oreficeria Senza Recapito» nelle due app (il Go elenca tutte le oreficerie esterne, con o
    senza telefono): attesa corretta.
  - Browser `pagina.etichetta`: nello standalone la `<label>` avvolge la `<select>`; il Go ora fa lo stesso e lo scenario
    misura il testo dell'etichetta senza le opzioni.
  - Browser `404 GET /ready-lots/N` nel Go: Chromium segue da sé il redirect di una risposta data da una rotta, senza
    ripassare dalle rotte, quindi la pagina finta non prendeva la GET. Ora lo scenario fa il POST del Go senza seguire il
    redirect e risponde con una pagina che va a `/ready-lots/L` con un meta refresh (una navigazione nuova, presa dalla
    pagina finta); i cookie del 302 passano. Da togliere con la 089.
  - M1: sul telefono il lotto del PC è già in lista: `pagina.vuoto` «(nessuno)» con i dati e, al posto di
    `apri.vuoto-prima`, `apri.righe-prima` (righe della propria oreficeria prima di aprire, 0).
  - M2: `anagrafica-oreficerie` telefono `pagina.righe` 6 (commento in `banco.mjs`).
- **`s088b`** (16/11/2026, 04:46–05:07, codice definitivo): HTTP 80 scenari, 1287 passi, 0 inattese; browser 26 scenari,
  52 viste, 4 inattese, tutte il rosso noto di `profilo-negozio` (la D-93 non è ancora nell'inventario: giro fatto con una
  copia di `bench/deviazioni.json` senza le righe D-93, passata con `-deviazioni`). **Verde per la 088.** Commit solo
  locale: giro finale unico e push di 087, 088 e 089 dopo la risposta di Stefano sulla D-93.

Scenari:

- **HTTP `lotti` e `lotti-dipendente`** (ORE-04): la GET senza accesso (302 a `/login`), l'accesso (302 a `/ready-lots`),
  poi la lista: testi, nome del campo, menu, «Nessuna oreficeria…» (solo sul vuoto), titolo e testo del riquadro dei lotti,
  intestazioni. `dati_test.go` ammette i loro `atteso_dati`.
- **Browser `lotti-apertura`** (ORE-04, ORE-05): sul vuoto solo la pagina. Con i dati, per vista, un'oreficeria propria
  «Zz Banco 088 desktop» / «… telefono» creata da `/settings/anagrafiche` (resta, come quelle della 086); «Apri lotto»
  senza scelta, con tre valori forgiati (`abc`, `1`, `987654`, un'opzione aggiunta al menu), apertura (indirizzo
  `/ready-lots/L`), la riga nella lista («… — lotto #L · 0 buste · aperto <data> Apri»), ripresa dello stesso lotto, una
  riga `ready_lot.opened`. Il lotto resta aperto e vuoto per il resto del giro; nessun altro scenario legge i lotti.
- **D-94 nel banco:** nessuna differenza attesa: nessuno scenario apre due lotti nello stesso istante. Nessuna riga in
  `bench/deviazioni.json`.

## Misura

Implementatore Opus: circa 16 minuti più due giri di correzioni, circa 300k token del sottoagente. Revisione Opus: circa
150k token. Giri del banco: 2.

## Dubbi

1. **Redirect e «Apri» verso `/ready-lots/<id>` prima della 089.** Fedele allo standalone, ma fino alla 089 nel Go è un
   404 (la voce di menu ora compare). **Deciso dal coordinatore il 16/11/2026:** 088 e 089 committate in locale senza
   push fino alla risposta di Stefano sulla D-93, poi un giro finale unico del banco e il push di tutto.
2. **Pulizia anche dalla GET senza JavaScript** (Scelta 2): nel Go anche un rendering «statico» pulisce, nello standalone
   no. Nessun effetto visibile. **Raccomandazione:** tenerlo (è il solo rendering del Go); non serve una D. **Per Stefano.**
