# 133 — Busta senza foto e lavori del giorno dopo il ripristino (D-117, D-119)

- **Fonte:** inventario D-117 (Stefano 25/11/2026, «ok come dici») e **D-119** (Stefano 27/11/2026, «15 ok»), voce 15
  di `docs/domande-aperte.md`, ricevuta 131.
- **Voci:** nessuna voce nuova chiusa; nuovi test con il segno su RIP-H-29, RIP-H-30, RIP-H-40, JOB-02, OPS-19.
  **Dipendenze e migrazioni nuove:** nessuna.
- **Modelli:** implementatore Opus (~391k token in due giri, ~40 minuti di orologio), revisore Opus indipendente (~177k token in
  due giri, ~30 minuti), giro del banco Opus (~123k token, ~90 minuti). **Banco:** verde al terzo giro (vedi in fondo).

## Cosa c'è

- **D-117, la domanda della busta senza foto** (solo Go, solo creazione). `POST /nuova` senza foto scelte nel browser
  (`intake_photos` assente o 0, quindi ogni modulo senza JavaScript) e senza `senza_foto=1` non crea niente: `buste.Crea`
  fa tutti i controlli e, per ultima, dopo l'avviso della forma e quello delle etichette già usate, dà
  `buste.ErrSenzaFoto` e annulla la transazione (nessuna busta, nessun numero consumato, nessun `repair.created`). La
  pagina torna con tutto quello che era scritto e, sotto «Genera busta», il riquadro «Questa riparazione non ha foto.
  Vuoi salvarla lo stesso?» con «Aggiungi foto» e «Salva senza foto». Prima della domanda vengono l'avviso del QR
  mancante e gli errori dei campi.
  - «Salva senza foto» rimanda il modulo con lo stesso invio: un doppio tocco resta una busta sola. Dopo la risposta il
    modulo la ricorda (campo nascosto) se un altro avviso o un errore lo ridisegnano.
  - «Aggiungi foto»: con JavaScript la domanda sparisce e la pagina va alle foto, senza richieste; senza JavaScript il
    modulo torna com'è, senza salvare.
  - I due pulsanti stanno dopo il modulo, legati a lui con l'attributo `form`: «Genera busta» resta il primo (Invio) e
    l'unico dentro `#new-repair-form`.
  - La conferma è lato server, quindi vale anche per il salvataggio che `app.js` manda con fetch (foto di una busta
    di prima ancora in memoria): la risposta è la stessa pagina e `app.js` la mostra. `app.js` non rimanda mai un POST
    da solo, quindi nessun invio automatico può creare una busta senza la risposta.
  - File: `internal/buste/crea.go`, `internal/web/nuova.go`, `internal/web/dettato.go` (un rifiuto del dettato tiene la
    risposta), `pagine/nuova.html`, `statici/app.js`, `statici/app.css`.
- **D-119, i lavori del giorno alla conferma del ripristino.** `backup.ConfermaRipristino`, nella stessa transazione,
  chiama `jobs.SegnaDovutiComeFatti` (`internal/jobs/ripristino.go`): per ogni lavoro già dovuto all'ora della conferma
  scrive una riga automatica «ok», 0 avvisi, 0 messaggi, «Segnato come già fatto alla conferma del ripristino: su questa
  macchina non è partito.». «Dovuto» è quello del giro, con le stesse funzioni (`nellOra`, estratta da `dovuto`, e
  `tentativiRimasti`, ora su un collegamento o una transazione): giorno d'apertura, lavoro attivo, ora del negozio con
  l'ora legale, nessuna esecuzione automatica riuscita oggi, meno di tre tentativi. **Senza** le domande sui canali:
  nella copia isolata sono spenti. Niente `jobs.ran`. Il conteggio va in `backup.restore_confirmed`
  (`jobs_marked_done`) e in `EsitoConferma.LavoriSegnati`. Un lavoro non ancora dovuto parte alla sua ora.
- **Per i test:** `opzioniServe.orologioLavori` (`cmd/gestrip/main.go`, nil = orologio vero) e `serveIsolatoCon`.
- **Banco:** `banco.mjs` e `manuale.mjs` aggiungono `senza_foto=1` a ogni `POST /nuova` che non ha già una risposta
  (`senzaFotoGiaDetto`). Documenti: `docs/SVILUPPO.md` (sezione nuova), voce 15 di `docs/domande-aperte.md` decisa.

## Prove

- `internal/web/senzafoto_test.go`: **`TestNuovaSenzaFotoChiedeConferma`** (senza `intake_photos`, con «0», «+0»,
  «000»: niente busta né audit, i valori scritti, la stessa chiave delle foto, la domanda dopo il modulo con un solo
  pulsante dentro; «Salva senza foto» crea con il numero 1/26, lo stesso invio ripetuto dà lo stesso esito),
  `TestNuovaConFotoNessunaDomanda`, `TestNuovaAggiungiFoto` (anche con `senza_foto` nello stesso invio e con il QR già
  avvisato; il gestore in `app.js`), `TestNuovaSenzaFotoPerUltima` (QR mancante, campo obbligatorio, forma, gemelle
  prima della domanda; la risposta ricordata), `TestModificaSenzaFotoNessunaDomanda`.
- `internal/jobs/ripristino_test.go`: `TestSegnaDovutiComeFatti` (briefing e ritardi sì, solleciti no; senza canali;
  niente `jobs.ran`; il giro dopo non li rifà, i solleciti partono alle 10:00 una volta),
  `TestSegnaDovutiComeFattiNonDovuti` (chiuso, spento, già riuscito, tre tentativi, ora legale 29/03, sera tardi UTC).
- `internal/backup/conferma_test.go`: conteggio e righe nella conferma giusta; nessuna riga dei lavori dopo un rifiuto
  o una conferma a metà (audit rotto da un trigger).
- **`TestConfermaRipristinoFinoAlServeNormale`** esteso (segno `inventario: OPS-19`): conferma dalla console venerdì
  alle 09:30 (briefing acceso) → serve riavviato con il pianificatore su un orologio di prova: alle 09:45 nessuna riga
  nuova e niente in coda per il negozio, alle 11:00 solo i solleciti; `jobs_marked_done: 2`. Il resto della 131
  (solo il messaggio «manda» al Bot API finto) resta verde.
- Test esistenti che creano buste senza foto: `bustaProva` e i moduli dei test del dettato e della raccolta portano
  `senza_foto=1`.
- **Mutazioni:** 17 su 17 uccise, vedi sotto.
- `scripts/check` verde: **chiuse 57, rosse 0** (parziali 192, aperte 93, fuori v1 11).

## Mutazioni

Fatte dall'implementatore, tutte **uccise** (17/17). Tre (M9, M13, M14) al primo giro non compilavano e sono state
rifatte in una forma che compila; M8 al primo giro faceva andare in panico il test, che ora fallisce con un messaggio.

| # | Mutazione | Uccisa da |
|---|---|---|
| M1 | `Crea` non dà mai `ErrSenzaFoto` | `TestNuovaSenzaFotoChiedeConferma` |
| M2 | domanda prima della forma e delle gemelle | `TestNuovaSenzaFotoPerUltima` |
| M3 | conteggio delle foto ignorato | `TestNuovaConFotoNessunaDomanda` |
| M4 | `senza_foto` ignorato | `TestNuovaSenzaFotoChiedeConferma` |
| M5 | `aggiungi_foto` ignorato | `TestNuovaAggiungiFoto` |
| M6 | risposta non ricordata dopo un errore | `TestNuovaSenzaFotoPerUltima` |
| M7 | campo nascosto messo con la domanda | `TestNuovaSenzaFotoChiedeConferma` |
| M8 | domanda dentro il modulo | `TestNuovaSenzaFotoChiedeConferma` |
| M9 | tentativi di oggi ignorati | `TestSegnaDovutiComeFatti` |
| M10 | riga «a mano» invece che automatica | `TestSegnaDovutiComeFatti` |
| M11 | riga «saltato» invece che «ok» | `TestSegnaDovutiComeFatti` |
| M12 | solo «attivo», senza giorno né ora | `TestSegnaDovutiComeFatti`, `…NonDovuti` |
| M13 | conferma senza segnare i lavori | `TestConfermaRipristino` |
| M14 | lo stesso, da capo a fondo | `TestConfermaRipristinoFinoAlServeNormale` |
| M15 | `jobs_marked_done` sempre 0 | `TestConfermaRipristino` |
| M16 | orologio dei lavori di serve ignorato | `TestConfermaRipristinoFinoAlServeNormale` |
| M17 | giorno del negozio spostato di un giorno | `TestSegnaDovutiComeFatti`, `…NonDovuti` |

`-race` (CGO=1, `-run` mirato) verde su cmd (da capo a fondo), jobs, backup, web.

## Revisione (Opus indipendente, 14 mutazioni, 9 uccise; nessuna ALTA, 1 MEDIA, 3 BASSA)

- **MEDIA, nessuna prova nel browser della domanda:** scenario browser nuovo **`domanda-foto`** (`banco.mjs`,
  `bench/browser/domanda-foto.json`), solo Go (lo standalone non fa niente; tutti i campi `solo_go`, D-117), senza
  `senzaFotoGiaDetto` (`ctx.domandaVera`), in `IN_FONDO` e per ultimo (lascia nel Go buste che lo standalone non ha).
  (a) Senza IndexedDB: una busta con una foto il cui invio si trattiene, «+ Nuova» nel giro, una busta senza foto →
  domanda (200), doppio tocco su «Salva senza foto» → un solo salvataggio (`a.salva.invii` = `200,302`), la pagina non
  si ricarica, la foto in memoria resta in coda e poi parte. (b) Domanda dopo un invio normale, in vista e con il fuoco;
  «Aggiungi foto» → 0 richieste, domanda tolta, campi intatti, fuoco sulle foto.
- **BASSA, console e pagina della conferma senza i lavori segnati:** `jobs.FraseSegnati` («Lavori di oggi segnati come
  già fatti: Buste in ritardo, Briefing del mattino. Se la macchina vecchia non li aveva fatti, usa «Esegui adesso» in
  Promemoria e job dopo il riavvio.»), nella console e nel messaggio della pagina dopo la conferma; senza lavori la
  console dice «Lavori di oggi: nessuno segnato come già fatto.». `TestConfermaRipristinoFinoAlServeNormale` prova che
  dopo il riavvio «Esegui adesso» del briefing segnato lo fa ripartire (riga «A mano», messaggio al negozio in coda).
- **BASSA, rifiuto del dettato che ricorda la risposta senza test:** caso in `TestDettatoRifiuti` (ricordata con
  `senza_foto=1`, non con il campo assente o «0», mai la domanda).
- **BASSA, controlli in negativo:** sulla pagina della domanda e dopo «Aggiungi foto» senza JavaScript l'avviso del QR
  già risposto («Genera busta senza QR», `intake-qr-warning`) non compare più (il campo `qr_avvisato` resta);
  `TestConfermaRipristinoLavoriNonSegnati`: un trigger su `esecuzioni_lavori` fa fallire tutta la conferma senza
  cambiare niente.
- **UX, domanda in fondo e invisibile:** il riquadro sta in cima a «Accetta riparazione» (CSS `con-domanda-foto`, anche
  senza JavaScript); con JavaScript `app.js` ci porta scorrimento e fuoco (`tabindex="-1"`) dopo ogni pagina.
- **Mutazioni sopravvissute alla revisione (A, B, J, C, O):** l'implementatore non aveva l'elenco della revisione: le ha
  rifatte dalle descrizioni dei rilievi (N1–N10 qui sotto), tutte uccise; la L resta come motivata dalla revisione. Le
  17 dell'implementatore, rifatte sul codice nuovo, restano tutte uccise.

| # | Mutazione | Uccisa da |
|---|---|---|
| N1 | il rifiuto del dettato dimentica «Salva senza foto» | `TestDettatoRifiuti` |
| N2 | avviso del QR sulla pagina della domanda | `TestNuovaSenzaFotoChiedeConferma` |
| N3 | avviso del QR dopo «Aggiungi foto» | `TestNuovaAggiungiFoto` |
| N4 | errore di `SegnaDovutiComeFatti` ignorato | `TestConfermaRipristinoLavoriNonSegnati` |
| N5 | frase dei lavori sempre vuota | `TestFraseSegnati` |
| N6 | console senza i lavori | `TestConfermaRipristinoFinoAlServeNormale` |
| N7 | pagina senza i lavori | `TestConfermaRipristinoDallaPagina` |
| N8 | domanda non in cima | `TestNuovaSenzaFotoChiedeConferma` |
| N9 | niente fuoco sulla domanda | `TestNuovaAggiungiFoto` (solo il testo di `app.js`; nel browser `domanda-foto`) |
| N10 | frase con i nomi interni dei lavori | `TestFraseSegnati` |

## Scelte da confermare

1. **«Aggiungi foto» senza JavaScript** ridisegna il modulo com'è: senza JavaScript le foto prima del salvataggio non
   si scelgono (dalla 040b), quindi lì la risposta pratica è «Salva senza foto» e poi le foto dalla scheda.
2. **Ordine delle domande:** QR mancante, errori dei campi, forma ed etichette già usate, per ultima le foto. Una busta
   senza QR e senza foto chiede due volte (prima il QR, poi le foto).
3. **Conteggio delle foto:** la domanda vale quando il browser non dice di avere foto per il modulo. Il server non sa
   se le foto poi arrivano davvero alla busta (la coda le manda dopo).
4. **Lavori segnati senza guardare i canali** (spenti nella copia isolata): se il backup aveva tutti i canali spenti,
   il giro dopo il riavvio non avrebbe fatto niente comunque. La riga «ok» con la frase lo dice nella pagina
   «Promemoria e job».
5. **Nel banco** la domanda compare solo nello scenario browser `domanda-foto` (dopo la revisione); negli altri
   scenari e nelle schermate del manuale il `POST /nuova` porta già `senza_foto=1`.
6. **La domanda in cima al riquadro:** nel codice resta dopo il modulo (Invio = «Genera busta», selettori del banco), il
   CSS la mostra sotto «Accetta riparazione». Con JavaScript scorrimento e fuoco vanno sul riquadro, non su un pulsante
   (un Invio per sbaglio non salva). «Aggiungi foto» porta il fuoco sulla sezione delle foto: i suoi campi file sono
   nascosti.
7. I dubbi di prodotto della revisione (lavori mai fatti dalla macchina vecchia, lavori senza destinatari, «Aggiungi
   foto» senza JavaScript in tondo) sono la **voce 17** di `docs/domande-aperte.md`.

## Per il manuale

- Salvando una riparazione nuova **senza foto** compare la domanda «Questa riparazione non ha foto. Vuoi salvarla lo
  stesso?». «Aggiungi foto» torna alla scheda com'era scritta, per scattare o scegliere le foto; «Salva senza foto»
  salva.
- La domanda arriva dopo gli altri avvisi (etichetta mancante, etichetta già usata) e solo per una riparazione nuova:
  modificare una scheda non chiede niente.
- Dopo la conferma «questa è ora la macchina vera» e il riavvio, i promemoria del giorno già passati (ritardi,
  solleciti, briefing) non ripartono: nella pagina «Promemoria e job» risultano «Segnato come già fatto alla conferma
  del ripristino». Quelli con l'ora non ancora arrivata partono alla loro ora.
- La conferma (pagina o console) dice quali promemoria di oggi ha segnato. Se la macchina vecchia non li aveva fatti,
  dopo il riavvio si usa «Esegui adesso» in Promemoria e job.
- La domanda delle foto compare in cima al riquadro «Accetta riparazione».
- Da decidere (coordinatore): una schermata della domanda nel capitolo della nuova riparazione. Oggi le ricette del
  manuale la saltano.

## Differenze dallo standalone

- Lo standalone salva una riparazione nuova senza foto senza chiedere (D-117).
- Lo standalone, dopo un ripristino `--live`, rifà i lavori del giorno (D-119).

## Banco

Tre giri (`gestrip-standalone:fab-225`, `sintetici-import`, «memoria libera» del coordinatore, guardia sotto 10 GB mai
scattata, minimo 18,6 GB; `banco giu` senza avanzi):

- **s133a rosso per la ricetta nuova:** `domanda-foto` gira dopo `assistente-salva`, che lascia il Go con il modello
  finto configurato: su `/nuova` il riquadro «Inserimento manuale senza AI» è chiuso (`Aperto: !assistito`) e la
  ricetta non trovava il modulo. Il Go fa quello che deve. Corretta la ricetta: `apriManuale()` in `domandaFoto`
  (`tools/banco/browser/banco.mjs`) apre `#manual-intake` se è chiuso. HTTP già verde (91 scenari, 0 inattese).
- **s133b caduto sulla nota 8:** 500 dello standalone su `GET /repairs/88` in `storico-correzioni[1]` («Database
  busy»); `domanda-foto` pulito. Unica ripetizione.
- **s133c verde:** HTTP 91 scenari, 1373 passi, 0 inattese; browser 39 scenari, 78 viste, 0 inattese, 0 errori
  («BANCO BROWSER VERDE»), `domanda-foto` verde con le 44 righe `NOTA standalone domanda-foto…` attese (D-117).
  `scripts/check` con `GESTRIP_BANCO_ESITO`: voci 353, chiuse 233, rosse 0, parziali 16.

Gli scenari HTTP non cambiano (i loro `POST /nuova` finiscono prima della domanda). D-119 il banco non la esercita
(ripristino e conferma sono solo Go). `manuale.mjs` (con `senzaFotoGiaDetto`) non è girato qui: lo prova il giro
`man` della 133b; nessuna PNG del manuale toccata.

## LSP

3 chiamate. `findReferences` su `buste.Crea`: utile, conferma che in produzione la chiama solo `internal/web/nuova.go`
(il resto sono test e `tools/banco/numeri.go`, che semina senza la domanda). Due `findReferences` su
`ConfermaRipristino`: la prima senza identificatore, la seconda con l'indice vecchio ha risposto con i riferimenti di
`EsitoConferma`. Inutili: per i chiamanti è bastato grep.

## Difetti trovati per strada (non corretti)

- `go build ./...` senza `-tags nodynamic` non compila (`internal/imaging/senza_nodynamic.go`: `serveIlTagNodynamic`
  non definito). Probabilmente è voluto (obbliga il tag), ma l'errore non dice di aggiungerlo.
- Il gestore `data-invio-unico` (`app.js`) spegne solo i pulsanti **dentro** il modulo: un pulsante legato con
  l'attributo `form` (come i due della domanda) resta acceso. Il doppio invio lo fermano comunque `form.dataset.inviato`
  e l'invio monouso del server.
