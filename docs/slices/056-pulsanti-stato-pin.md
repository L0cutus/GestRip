# 056 — Pulsanti rapidi, menu dello stato, 📌 e ritorno alla lista (T4.2, ottava)

- **Fonte:** coordinatore, 07-08/11/2026. Lo standalone a `go-baseline` = `302439d` (slice 225):
  `workshop_live.ex` (eventi `quick-status`, `transition`, `toggle_pin`, `@quick_steps`,
  `status_choices/1`, `#list-return` con `.ListReturnStore`, `list_path/1`), `core_components.ex`
  (`back_bar/1` con `.ListReturn`), `workshop.ex` (`toggle_repair_pin/3`, `transition_repair/4`),
  `repair_index.ex` (ordine con le fissate in testa), `assets/css/app.css` (`.pin-btn`), i test
  «the quick buttons of a card (slice 198)», «the status select of a card (slice 197)», «pin control
  toggles in place and crafted ids fail without mutation». Le decisioni di Stefano del 07/11/2026
  (filtro «Ricezione», risposte ai Dubbi della 053, 055 e 055d).
- **Voci:** RIP-H-14 (pulsanti rapidi), RIP-H-15 (menu dello stato), RIP-H-16 (📌), RIP-H-20 (ritorno
  alla lista filtrata). Non portate: RIP-H-17 e RIP-H-18 (vedi «Non portato»).
- **Deviazioni:** D-71 estesa al filtro e alla pillola della home; D-64 (numero visibile nella
  conferma e nel menu); **D-76…D-79 nuove**, solo registrate (si fanno nella 057 e nella 059).
- **Allineamento:** banco e documenti da `fab-224` a `fab-225`. Nessun codice dell'applicazione per
  la 225 (vedi «Allineamento alla 225»).
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

### A. Le azioni del riquadro (RIP-H-14, RIP-H-15, RIP-H-16)

Ogni riquadro della lista ha, a destra (sotto sul telefono), un blocco `rep-card-azioni` sopra il
link a tutta superficie, come il terzo blocco del riquadro dello standalone. Ogni azione è un modulo
POST con il CSRF e il campo nascosto `lista`: la query della lista di adesso, filtri e pagina
(`ritornoLista`). Il server **non segue `lista` com'è**: la rilegge con `buste.LeggiFiltri` e ne
riscrive l'indirizzo (`listaDelModulo`), quindi un valore falsificato porta al più a un'altra lista,
mai fuori (`TestListaDelModulo`, anche con `//altro.esempio` e un a capo).

- **«✓ Pronta» e «✓ Consegnata»** (`POST /repairs/{id}/rapido`, `status`):
  - «✓ Pronta» (`#quick-ready-<id>`, «Segna come pronta») solo su una busta in lavorazione;
    «✓ Consegnata» (`#quick-delivered-<id>`, «Segna come consegnata») solo su una pronta, con
    `data-confirm` «Segnare la busta 0045/26 come consegnata?» (il numero visibile, D-64);
    nessuno su preventivi, attesa materiali e compro oro (`buste.PassiRapidi`, `@quick_steps`);
  - `buste.PassoRapido`: lo stesso cambio di stato della scheda e del pannello QR (`passaStato`:
    grafo, date, conservazione, lotto, audit `repair.status_changed`). Lo stato di partenza si
    guarda **sulla riga riletta in transazione**;
  - «✓ Pronta» porta alla scheda sul prezzo (`/repairs/<id>?edit=prezzo`), senza messaggio, come lo
    standalone senza Telegram; «✓ Consegnata» torna alla lista con «Stato aggiornato.»;
  - partenza sbagliata, passo che non è rapido, busta che non c'è o nel cestino, id malformato:
    la lista con «Transizione non consentita.», niente scritto.
- **Il menu dello stato** (`#transition-<id>`, `POST /repairs/{id}/stato-lista`, `status`):
  - il `<select>` ha lo stato di adesso e i soli passi del grafo, nell'ordine della famiglia
    (`status_choices/1`), `aria-label` «Stato 0045/26»; spento (`disabled`) e senza «Cambia» su una
    busta senza passi (consegnata, annullata, chiusa);
  - `buste.CambiaStatoDallaLista`: il cambio della scheda; lo stato che la busta ha già (riletto in
    transazione) non scrive niente e torna alla lista in silenzio, senza audit (slice 185 dello
    standalone); riuscito «Stato aggiornato.», rifiutato «Transizione non consentita.»;
  - senza JavaScript il menu ha il suo pulsante «Cambia» (`data-solo-senza-js`).
- **Il 📌** (`#pin-repair-<id>`, `POST /repairs/{id}/pin`):
  - «Pinna in cima» o «Togli pin» (`aria-label` e `title`), `.pin-btn` e `.pinned` come lo
    standalone (trasparente, ruotato quando è fissato);
  - `buste.CambiaFissata`: legge e scrive `fissata_il` nella stessa transazione, audit
    `repair.pinned` o `repair.unpinned` senza dati, poi la lista in silenzio. La colonna c'era già
    (`0007_negozio_clienti_buste.sql`, `fissata_il`, indice `buste_fissata`) e l'ordine con le fissate
    in testa anche (`ordineLista`, dalla 050): nessuna migrazione;
  - busta che non c'è o nel cestino, id malformato: «Riparazione non trovata.», niente scritto.
- **Email a «pronta» e popup Telegram:** come nella scheda (`stati.go`), arrivano con le loro aree
  (T4.7 e T4.6): il Go non ha ancora né l'una né l'altro.

### B. Con JavaScript (D-70, `app.js`)

- Il menu parte appena si sceglie (`change` → `requestSubmit`, come `phx-change`), e «Cambia» si
  nasconde a ogni pagina (`[data-solo-senza-js]`).
- I moduli `data-azione-lista` partono con `fetch`: la lista di ritorno prende il posto di questa
  **allo stesso punto della pagina** (`mostraPagina(…, true)`, cronologia sostituita), come la lista
  di LiveView che si aggiorna sul posto; il messaggio («Stato aggiornato.», «Transizione non
  consentita.») diventa `flash-sospesa`, fisso in cima allo schermo finché non lo si tocca, come il
  toast dello standalone. «✓ Pronta» porta alla scheda con la cronologia (`aggiungi`).
- Un secondo invio dello stesso modulo, finché il primo non ha risposto, non parte (un doppio tocco
  sul 📌 lo toglierebbe di nuovo). Il login ricarica la pagina; una risposta che non è del
  personale (il 403 del CSRF) si mostra com'è; una rete assente dice «Connessione assente».
- La conferma di «✓ Consegnata» è quella di `data-confirm` (`window.confirm`), che ferma il clic
  prima dell'invio. Senza JavaScript il pulsante invia senza chiedere, come gli altri `data-confirm`
  del Go (vedi i Dubbi).

### C. Ritorno alla lista filtrata (RIP-H-20)

- La lista ha `<div id="list-return" data-path="…" hidden>` (prima della lista, come lo standalone):
  l'indirizzo della lista di adesso, «/» senza parametri; i parametri dei link (`parametriLink`: mai
  un codice d'etichetta, D-63), la pagina solo dalla 2, in ordine di nome come
  `Plug.Conn.Query.encode/1` su una mappa. Anche la lista disegnata dal POST del campo unico ha il
  suo.
- `app.js` (`.ListReturnStore` e `.ListReturn`): a ogni pagina scrive `data-path` in
  `sessionStorage` `gestrip:list-return` e mette quell'indirizzo nell'`href` dei link
  `a[data-list-return]` («← Indietro» della scheda, `#repair-back-list`, e di «Nuova riparazione»,
  `#new-repair-back`), solo se è l'indirizzo di una lista (la stessa espressione dello standalone:
  `/` da solo o con una query di caratteri da URL). Senza storage (finestra privata) o senza
  JavaScript resta «/».

### D. «Ricezione» nella home (D-71 estesa, Stefano 07/11/2026)

- Nel cassetto, «Filtra il periodo per data di» offre «Ricezione» al posto di «Accettazione» (il
  valore resta `accettazione`); la pillola dice «📅 Ricezione: 2026-01-01 → …». Le etichette «Data
  da» e «Data a» restano. La ricevuta del cliente resta «ACCETTATA».
- Riga D-71 di `docs/SVILUPPO.md` aggiornata («Date della busta»).

### E. Le decisioni di Stefano del 07/11/2026

Registrate in `docs/SVILUPPO.md`, «Decisioni di Stefano del 07/11/2026», dopo aver guardato lo
standalone a `302439d`:

- **D-76** (slice 057): all'import, una volta, i codici salvati con spazi ai bordi o «rip-» minuscolo
  si riscrivono con `buste.NormalizzaCodice`, con avviso nel report. Lo standalone salva e importa il
  codice com'è.
- **D-77** (slice 057): all'import i numeri interni con zeri davanti («0046/26») si riscrivono senza
  zeri, con avviso. Lo standalone li tiene (il suo indice unico ha lo stesso difetto). Annotato anche
  in `docs/domande-aperte.md`, «Difetti in lista», punto 1.
- **D-78** (slice 059): la linea di mira nella finestra della fotocamera. Lo standalone non la ha; il
  Go la disegna già dalla 055 (Dubbio 2 della 055).
- **D-79** (slice 059): «⚠ senza QR» anche sulle buste con un QR di altro formato. Lo standalone marca
  solo le aperte senza codice.
- **Senza D** (slice 059, come lo standalone): due bip gravi per la pistola con un codice
  sconosciuto; il 📷 di «Etichetta» apre la fotocamera.
- **Nota senza D:** i segnaposto delle opzioni di stampa si rivedono alla T5.5 con la MUNBYN;
  l'indirizzo non cambia con una ricerca scritta, come oggi.

Il «Prossimo passo» e «In attesa di Stefano» di `docs/handoff/2026-11-07-fine-055d.md` hanno una riga
datata 08/11/2026 con la correzione.

## Allineamento alla 225

`git log d3778df..302439d` dello standalone ha solo la slice 225 (`6870cf0`, `302439d`): dopo ogni
apertura di WhatsApp la pagina torna alla lista (`assets/js/wa_home.js`, montato in `app.js`;
`notice_opener.js` manda `phx:wa-home`; `push_event("wa-home")` dopo il sollecito degli artigiani e
dopo il reinvio di un lotto; `data-wa-home="after-record"` su quei link). È la **D-74**, che il Go
farà con la T4.6: oggi il Go non ha né i link WhatsApp né le pagine dei solleciti e dello storico dei
lotti. Nessuna migrazione fra le due (`priv/repo/migrations` invariato), nessun cambio nelle pagine
che il banco confronta. **Nel Go non cambia codice per la 225.**

- `fab-225` al posto di `fab-224` in `tools/banco/main.go` (uso), `tools/banco/istanza.go` (esempio
  di `-immagine`), `docs/PIANO.md` (in testa, `go-baseline` = `302439d` con la catena di prima),
  `docs/SVILUPPO.md` (schermate del manuale), `docs/prova-dati-veri.md` (immagine da controllare e
  da usare per la decifratura, pacchetto 2026.11.06-1). Ricevute e riprese vecchie lasciate come sono.
- Immagine `gestrip-standalone:fab-225`: `sha256:b381754bd7e3…`, creata il 06/11/2026 alle 10:45.
- **Per la T4.6:** `wa_home.js` ricorda la lista in `sessionStorage` con la sua chiave
  `gs:list-search`; il Go ha già `gestrip:list-return` (questa slice), che la D-74 può usare.
- Registro di allineamento: nel repo non c'è (nessun file con `d3778df` oltre a ricevute, riprese e
  PIANO). La riga della 225 va nel registro del coordinatore.

## Deviazioni

- **D-71** (approvata, estesa il 07/11/2026): nel banco il normalizzatore `D-71` cambia anche la
  scelta `<option value="accettazione">Accettazione</option>` e «📅 Accettazione: » dello standalone;
  tutti i passi di `home-filtri` hanno `D-64+D-71`, le 10 attese con il periodo dicono «📅
  Ricezione:», e la riga D-69 del passo 53 in `bench/deviazioni.json` pure (con una parola nel motivo).
- **D-64** (approvata): la frase della conferma e l'`aria-label` del menu portano il numero
  visibile. Il normalizzatore `D-64` riconosce ora anche «Segnare la busta N/AA» e «Stato N/AA»
  (`reNumeroStandalone`); nel livello browser il numero della conferma è scritto «N».
- **D-70** (approvata): con JavaScript le azioni passano da `fetch` e la lista resta sul posto; senza,
  i POST. Nessuna riga nuova nel banco: nello standalone sono eventi LiveView, e il livello browser
  confronta quello che si vede.
- **D-76…D-79:** solo registrate; nessun codice.
- **Senza D-xx:** il pulsante «Cambia» senza JavaScript, `flash-sospesa`, il `data-path` scritto dai
  filtri letti (vedi i Dubbi).

## Prove

- **Test Go nuovi:**
  - `internal/buste/lista_azioni_test.go`: `TestPassoRapido` (pronta e consegnata con date e audit,
    doppio «✓ Pronta», partenze sbagliate compresi attesa materiali → pronta e compro oro, arrivi non
    rapidi, vuoto, cestino, id assenti: «Transizione non consentita.» e audit invariato),
    `TestCambiaFissata` (fissata adesso, audit `repair.pinned` e `repair.unpinned` con `{}`, la
    fissata in cima alla lista e poi no, cestino e id assenti: `ErrBustaNonTrovata`).
  - `internal/web/lista_azioni_test.go`: `TestRiquadroAzioni` (markup per stato e famiglia, opzioni
    esatte, menu spento, `lista` nel modulo), `TestPulsantiRapidi` (redirect al prezzo, lista con i
    filtri in ordine e «Stato aggiornato.», 11 rifiuti con «Transizione non consentita.», audit
    invariato, dipendente), `TestMenuStatoLista` (passo con date e audit, stato uguale in silenzio,
    9 rifiuti, compro oro), `TestPinLista` (ordine, `rep-card-pinned`, audit, «Riparazione non
    trovata.» per 5 id falsi compreso il cestino), `TestListaDelModulo` (11 valori di `lista`,
    falsificati compresi), `TestRitornoAllaLista` (`data-path` di 7 liste e della lista del POST,
    `data-list-return` nella scheda e in «Nuova riparazione», `app.js`), `TestScriptAzioniDellaLista`
    (`app.js` e `app.css`).
- **Test cambiati:** `TestHomePillole` e `TestHomeRicercaECassetto` («Ricezione»),
  `TestHomeRicercaEtichetta` (il «selected» si cerca nel solo menu del cassetto, perché ora ce l'hanno
  anche i menu dei riquadri), `TestPaginaNuova` e `TestSchedaApertura` (`data-list-return`); la tabella
  dei permessi (`tabellaPermessi`) con le tre rotte, per tutto il personale; `busteFinte` con i tre
  metodi nuovi.
- **Banco:** `TestNormalizzaD71` (scelta del periodo e pillola, e cosa resta), il test di
  `normalizzaD64` (conferma e menu), `TestScenariBrowserDelRepo` con `riquadro-azioni`.
- `scripts/check`: vedi «Controlli».

## Mutazioni

A mano, 16 punti (`python3` nella cartella temporanea della sessione, test mirati, file rimessi a
posto dopo ognuno). **16 uccise**, nessuna viva: partenza del passo rapido non guardata, stato uguale
rifiutato (dominio) o non taciuto (web), audit del 📌 scambiato, cestino non escluso dal 📌, ordine
dei parametri, codice d'etichetta in `lista`, pagina 1 nell'indirizzo, menu mai spento, pulsanti
rapidi anche al compro oro, «✓ Pronta» che non va al prezzo, tutte le opzioni nel menu, «Accettazione»
nella pillola, numero tolto dalla conferma, errore del 📌 taciuto, `sessionStorage` seguito senza
controllo.

## Banco

`gestrip-standalone:fab-225`, binari dell'albero di lavoro.

- **Primo giro, dati importati** (`s056`): HTTP 47 scenari, 862 passi, **47 inattese**, tutte
  dichiarabili, nessun guasto: `home-filtri` confronta anche il testo intero del menu del periodo
  (`visibile:repair-filter-form/select@4`), quindi tutti i passi di `home-filtri` (non solo quelli con
  un periodo) hanno avuto `D-64+D-71`; e 9 attese di `home[2]` sul menu della 22/26 e della 34/26
  scritte per il database vuoto e non per i dati importati (corrette). Il livello browser non è
  partito (HTTP rosso).
- **Secondo giro, dati importati** (`s056`, rifatta da capo): HTTP **0 inattese**; browser 28
  inattese e 4 errori, tutti di `riquadro-azioni`:
  - `#repair-status` della scheda aperta sul prezzo non si legge (il blocco è nascosto dal modulo):
    tolta l'osservazione `pronta.stato`, e lo scenario si fermava lì;
  - lo stato del riquadro si legge con `innerText`, che lo dà in maiuscolo dal CSS: ora `textContent`;
  - **un difetto vero del Go:** «Cambia» restava visibile con JavaScript, perché `.pulsante-contorno`
    (`display: inline-flex`) vinceva su `hidden`. Corretto con
    `[data-solo-senza-js][hidden] { display: none !important; }` in `app.css` (e nel test).
- **Terzo giro, dati importati** (`s056`): HTTP 47 scenari, 862 passi, 1176 attese, **0 inattese**,
  0 errori; browser 11 scenari, 22 viste, 28 attese, **2 inattese**, 0 errori: `sfondo` sul telefono,
  `pagina.risposte-errore` dello **standalone** «500 GET /repairs/1/photos/4» (il Go niente). È lo
  scenario della coda delle foto, che questa slice non tocca, e la 500 è dello standalone: lo stesso
  tipo di 500 intermittente della lettura delle miniature (ricevute 051–055b). `riquadro-azioni`
  verde.
- **Giro mirato, dati importati** (`s056m`: HTTP `healthz`, browser `sfondo` e `riquadro-azioni`):
  HTTP 1 scenario, 7 passi, 0 inattese; browser 2 scenari, 4 viste, 10 attese, **0 inattese**, 0
  errori: **verde**. La 500 non si è ripetuta.
- **Database vuoto** (`s056v`): HTTP 47 scenari, 862 passi, 158 attese (125 in più di prima: il
  normalizzatore D-71 ora su tutti i passi di `home-filtri`), **0 inattese** (29 non provati con
  questi dati); browser 11 scenari, 0 inattese (10 non provati): **verde**.
- `banco giu` dopo ogni giro: «AVANZI: nessuno».

## Controlli

- `scripts/check` verde sul codice finale (gofmt, vet, staticcheck, errcheck, build, test,
  copertura: 353 voci, 26 chiuse, 0 rosse, 87 parziali, 229 aperte, 11 fuori v1; segreti e dati
  veri OK).
- `scripts/parole-vietate --staged` dopo `git add -A`: nessuna parola (poi `git reset`).

## Per il manuale

Aggiunte a `docs/manuale/da-scrivere.md`, sezione «Lista delle buste (slice 056)», perché un
capitolo sulla lista non c'è ancora:

- Su una busta in lavorazione c'è «✓ Pronta»: la segna pronta e apre la scheda sul prezzo finale.
- Su una busta pronta c'è «✓ Consegnata»: chiede «Segnare la busta 0045/26 come consegnata?» e, con
  OK, la segna consegnata; si resta sulla lista con «Stato aggiornato.».
- Il menu dello stato di ogni busta offre solo i passi consentiti e cambia lo stato appena si sceglie;
  una busta consegnata o annullata ha il menu spento.
- 📌 «Pinna in cima» tiene la busta in testa alla lista; «Togli pin» la rimette al suo posto.
- Dopo un'azione la lista resta allo stesso punto e il messaggio compare in cima allo schermo.
- «← Indietro» della scheda e di «Nuova riparazione» torna alla lista di prima, con filtri e pagina.
- Nel cassetto il periodo si filtra per «Ricezione», e la pillola dice «📅 Ricezione: …».

## Non portato

- **RIP-H-18, popup Telegram** (facoltativo per la v1): il Go non ha ancora Telegram (T4.6). Quando
  arriva: dopo «✓ Pronta» lo standalone apre il popup e va al prezzo solo dopo la risposta.
- **RIP-H-17, «📲 Avvisa cliente» dalla lista:** è dell'area WhatsApp (T4.6), non era nel brief.
- **La galleria e `vista=galleria` nei link e nel ritorno:** slice 057.

## Dubbi

1. **«✓ Consegnata» senza JavaScript non chiede conferma.** È la regola del Go per `data-confirm`
   (riapertura, foto, link, firma: senza JavaScript si invia). Lo standalone senza JavaScript non
   funziona proprio. Se Stefano vuole la riserva della D-70 anche qui, la conferma la disegna il
   server come nel pannello del QR (`conferma=1`): una pagina in più, piccola.
2. **«Cambia»** è un testo nuovo, solo senza JavaScript (lo standalone non ha il pulsante perché
   cambia alla scelta). Da confermare il testo.
3. **Il messaggio «sospeso»** (`flash-sospesa`) c'è solo dopo un'azione del riquadro con JavaScript:
   fisso in cima, si chiude toccandolo, come il toast dello standalone. Il resto del Go ha i messaggi
   in cima alla pagina (una differenza già vecchia). Se Stefano lo vuole dappertutto, è una slice di
   stile a parte.
4. **Doppio tocco sul 📌 senza JavaScript:** due invii, due cambi (fissa e toglie). Con JavaScript il
   secondo non parte. Lo standalone, con LiveView, fa due cambi anche lui.
5. **Partenza di «✓ Pronta» guardata in transazione:** lo standalone guarda lo stato prima, fuori
   dalla transazione, e poi applica il grafo: una busta passata nel frattempo da «in lavorazione» a
   «in attesa materiali» diventerebbe pronta. Il Go la rifiuta, come dice l'inventario («stato
   cambiato nel frattempo → Transizione non consentita.»).
6. ~~**Busta nel cestino:** il 📌 dello standalone rilegge con `Repo.get`, senza guardare il cestino.~~
   Chiuso con la review: anche lo standalone toglie le righe cestinate (`Repo.prepare_query`) e dice
   «Riparazione non trovata.», come il Go. Non è una differenza.
7. **`data-path` di `#list-return`:** lo standalone scrive i parametri dell'indirizzo come sono
   (anche «p=99», «stato=boh», un parametro sconosciuto); il Go quelli dei filtri letti (pagina
   riportata all'ultima, valori non validi tolti), in ordine di nome. Per una lista normale sono
   uguali (il banco lo confronta su cinque liste). Con un codice d'etichetta nella ricerca il Go non
   lo scrive (D-63), lo standalone sì.
8. **`aria-label` del menu e conferma con «N.»:** per una busta senza etichetta il numero visibile è
   «N. 35/26», quindi «Stato N. 35/26» e «Segnare la busta N. 35/26 come consegnata?». È la regola
   della D-64 (come «Apri busta N. 35/26»); si legge un po' strano.
9. **D-78, la linea di mira c'è già** (055): la 059 deve solo confermarla. Se Stefano pensava a
   un'altra linea (più evidente, un riquadro), va detto all'inizio della 059.
10. **D-79, quanto si estende:** il segno nella lista e il suo titolo, e se anche il contatore e il
    filtro «⚠ Senza QR» contano le buste con un codice di altro formato. Va deciso prima della 059.
11. **D-76:** «spazi ai bordi o rip- minuscolo» come detto; `NormalizzaCodice` toglie anche gli
    spazi accanto a «-» («RIP - 2026 - 0045»): da decidere se la riscrittura dell'import li tocca.
    **D-77:** cosa fare se il numero senza zeri è già di un'altra busta.
12. **La 500 dello standalone in `sfondo`** (terzo giro) non si è ripetuta nel giro mirato: se torna,
    va alla sessione dello standalone come la diagnosi «database is locked» della 224.
13. **Il banco browser lascia la 22/26 e la 23/26 consegnate:** nessuno scenario dopo le legge oggi; uno
    scenario browser nuovo che le usi deve saperlo (scritto in `docs/SVILUPPO.md`).
14. **Inventario privato** non toccato: le note «Portato (slice 056)» di RIP-H-14, 15, 16, 20, la D-71
    estesa e le righe D-76…D-79 le scrive il coordinatore.

## Review


Un sottoagente revisore, 08/11/2026, sul worktree non committato; standalone a `go-baseline`
(`workshop_live.ex`, `workshop.ex`, `repo.ex`, `trash.ex`, `telegram_notices.ex`).

**Guardato:**

- `buste.PassoRapido`, `CambiaStatoDallaLista`, `CambiaFissata`: lo stato di partenza, lo stato
  uguale e la fissata si leggono sulla riga riletta **dentro** la transazione di scrittura
  (`passaStato`, `fissa.go`); una riga d'audit per cambio, nessuna per lo stato uguale e per i rifiuti;
  il cestino escluso in tutte e tre (`cancellata_il IS NULL`).
- Rotte: le tre passano da `f.rotta` → `catena` → `csrf.Modulo` e `richiedeAccesso`, `personale` come
  lo standalone (titolare e dipendente), nella `tabellaPermessi`.
- `lista`: `listaDelModulo` rilegge con `buste.LeggiFiltri` e riscrive con `ritornoLista`, che dà
  sempre «/» o «/?…» codificato: nessun reindirizzamento aperto (anche `//host`, URL assoluti, a capo:
  `TestListaDelModulo`). `sessionStorage`: l'`href` si scrive solo se combacia con
  `^\/(\?[…]*)?$`, quindi niente `//host`, `\`, schemi o percorsi diversi da «/».
- Standalone: `quick-status` (partenza da `@quick_steps`, «✓ Pronta» senza messaggio al prezzo,
  «✓ Consegnata» con «Stato aggiornato.»), `transition` (stato uguale in silenzio), `toggle_pin`
  (nessun messaggio, «Riparazione non trovata.»), `status_choices/1`, il `<select>` spento senza
  passi, `list_path/1`, `.ListReturnStore`: allineati, con le differenze dichiarate (D-64, D-70,
  Dubbi 1-8).
- `app.js`: `data-confirm` sta prima e ferma il clic (`stopImmediatePropagation`); `preventDefault`
  prima del controllo del doppio invio; `fetch` con la rete assente («Connessione assente», il modulo
  si sblocca), login → ricarica, 403/500 → pagina com'è (lo stesso schema del pannello QR); fuoco
  rimesso sull'elemento con lo stesso id dopo il cambio della lista; senza JavaScript tutto funziona
  con i POST e «Cambia». Il banco browser (`riquadro-azioni`) prova davvero menu, 📌, rapidi,
  conferma Annulla/OK e «← Indietro».
- Test: guardano stato, date, audit (contato prima e dopo i rifiuti) e indirizzo del redirect, non
  solo il codice; i test di `app.js` sono di testo, ma il comportamento lo prova il banco browser.

**Corretto:**

- Test che mancava: il menu dello stato su una busta **nel cestino** (sia un passo del grafo sia lo
  stato che ha già) dà «Transizione non consentita.» e non scrive niente (`TestMenuStatoLista`).
  Mutazione a mano (busta non trovata trattata come stato uguale, in silenzio): uccisa; file rimesso.
- Commento di `internal/buste/fissa.go`: diceva che lo standalone non guarda il cestino nel 📌. Non è
  così: `Workshop.get_repair` e `repo.get` passano da `Repo.prepare_query`, che toglie le righe con
  `deleted_at` (la famiglia `Repair` è in `Trash.families/0`). Corretto il commento.

**Resta (per il coordinatore):**

- **BASSA — Dubbio 6 sbagliato nei fatti.** Lo standalone dice «Riparazione non trovata.» anche lui
  per una busta nel cestino (vedi sopra): non è una differenza, il Dubbio si può chiudere. Il codice
  Go è giusto.
- **MEDIA (decisione di prodotto, già nel Dubbio 1) — «✓ Consegnata» senza JavaScript consegna senza
  chiedere.** È l'unico pulsante rapido irreversibile dalla lista (consegnata non ha passi; serve la
  riapertura). La regola del Go per `data-confirm` è questa, ma qui un tocco sbagliato su un telefono
  senza JavaScript consegna davvero. Raccomandazione: la conferma del server (`conferma=1`) come nel
  pannello QR, se Stefano è d'accordo.
- **BASSA — risposte fuori ordine con due azioni di fila su riquadri diversi.** Il blocco del doppio
  invio è per modulo: un 📌 su una busta e subito un cambio di menu su un'altra partono insieme, e se
  la prima risposta arriva dopo la seconda la lista mostrata è quella vecchia (senza il secondo
  cambio, che però è scritto). LiveView li mette in fila. Basta ricaricare; si potrebbe tenere un solo
  invio in corso per tutta la lista.
- **BASSA — accessibilità:** il menu parte al `change`, quindi con le frecce su un `<select>` chiuso
  (Chrome e Firefox su PC) ogni freccia è un cambio di stato. È lo stesso comportamento di
  `phx-change` dello standalone, quindi parità; da sapere. Dopo «✓ Consegnata» su una lista filtrata
  la busta sparisce e il fuoco torna in cima alla pagina. Il messaggio sospeso si chiude solo con un
  tocco o un clic, non con Esc; sul telefono copre la barra in alto finché non lo si tocca (come il
  toast dello standalone).
- **BASSA — un errore del database** nei tre POST si registra e si mostra come «Transizione non
  consentita.» / «Riparazione non trovata.», non come errore; lo standalone andrebbe in errore. Poco
  probabile, nessun dato toccato.

`scripts/check` verde dopo le correzioni; `scripts/parole-vietate --staged` dopo `git add -A`:
nessuna parola, poi `git reset`. Niente commit.
