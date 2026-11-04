# 045 — Etichette QR prenumerate 57×32 (T4.4, quarta)

- **Fonte:** T4.4 del PIANO (piano nella ricevuta della 042, riga 045); `LabelSheet`, `LabelSheets`,
  `LabelSheetController` e i loro test nello standalone (slice 210: limite di 100, contatore `max`).
- **Voci:** RIP-S-47 (modulo `GET /labels`) e RIP-S-48 (PDF `GET /labels.pdf`). La stampa diretta
  CUPS del 57×32 (D-08, seconda metà) resta alla 046.
- **Deviazioni:** D-08 (il contatore si salva solo con la conferma), D-21 e D-64 (numero «0045/26»
  sotto il QR), D-22 (azzeramento del titolare), D-66 (proposta del nuovo anno). D-08 e D-22 nella
  forma proposta in questa slice e **approvata da Stefano il 04/11/2026** (tramite il coordinatore),
  con il testo «Sì, stampate» per il pulsante; D-66 nuova, approvata lo stesso giorno e fatta qui.
- **Lavoro:** implementazione e correzioni della review di un sottoagente; review e mutazioni di un
  sottoagente diverso; giro finale alla sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/etichette_qr.go`):
  - `LeggiFoglio` è `LabelSheet.parse/2`: «da» alzato a 1, «a» mancante = da+49, anno di oggi e
    57×32 mm per i mancanti; `String.trim/1`, `Integer.parse/1` e `Float.parse/1` per intero (con le
    due correzioni dello standalone «.5» → «0.5» e «57.» → «57.0»), al più 18 cifre e 26 caratteri;
    un valore che non è un testo (lista o mappa di Plug), da > a o mm fuori da 16…200 →
    `ErrRichiestaEtichette`; oltre 100 etichette `TroppeEtichette` con la richiesta com'era.
  - `ModuloEtichette` è `form_defaults/1` (ultimo+1, ultimo+50, anno di oggi, formato salvato o
    57×32; valori illeggibili → default) più `NuovoAnno` (D-66).
  - `GeneraEtichette`: prima il PDF, poi in una transazione formato (`format_g`) e
    `label_sheet.generated` con i dati dello standalone; **non** il contatore (D-08).
  - `ConfermaEtichette` («Sì, stampate», D-08): in una transazione, con l'ultimo letto dentro,
    `etichette_qr_ultimo = max(ultimo, a)`, `etichette_qr_anno = max(anno salvato, anno del foglio)` e
    `label_sheet.confirmed` (dati del foglio più `previous` e `last`).
  - `AzzeraEtichette` (D-22, D-66), per un anno: in una transazione `etichette_qr_ultimo = 0`,
    `etichette_qr_anno` = quell'anno e `label_sheet.counter_reset` (`previous` come numero, `new_year`,
    `year`).
  - `NuovoAnnoPer(anno)`: la condizione del D-66 per un anno qualunque (quello proposto dal modulo o
    quello del foglio); `AnnoDelModulo`: 2024…2099.
  - `FormatoG` è `format_g/1`: arrotondamento a metà per eccesso sul valore esatto come
    `Float.round/2` e `Float.to_string/1` (misurati, sotto).
  - Codice del QR `RIP-<anno>-NNNN`, numero stampato «NNNN/AA», nome del file
    `etichette-qr-<anno>-<da>-<a>.pdf`; il pad a 4 cifre non taglia (10000 resta 10000).
- **Documento** (`internal/stampe/etichette_qr.go`): una pagina L×H per etichetta, QR vettoriale del
  motore e sotto il numero (D-21, D-64). Disposizione scelta (documentata nel file e fissata da
  `TestDisposizioneEtichettaQR`):
  - 2 mm di margine su ogni lato;
  - numero in grassetto centrato, corpo che parte da `max(8, int(h × 0,55))` pt come lo standalone e
    scende di mezzo punto finché sta nella larghezza;
  - QR di lato `min(L − 4, H − 4 − riga del numero)`, con il margine chiaro di 4 moduli dentro, che
    lo separa dal numero; il gruppo è centrato.
  - A 57×32 mm il numero è 17 pt e il QR 22 mm (nello standalone 28 mm, con il numero a destra); a
    16×16 mm il numero è 7,5 pt e il QR 9,35 mm (moduli di 0,32 mm, circa 2,5 punti di una termica a
    203 dpi: al limite, l'avviso «Formato piccolo» resta).
- **Pagine e rotte** (`internal/web/etichette_qr.go`, `pagine/etichette.html`, tutte con il guscio):
  - `GET /labels` (personale): il modulo dello standalone con gli stessi testi, valori e attributi;
    il pulsante dice «Genera PDF» e va a `GET /labels/genera` (D-08). Per il titolare «Azzera
    contatore» (se l'ultimo numero non è 0) oppure, al suo posto, la proposta «Nuovo anno: ripartire
    da 1?» (D-66): mai due pulsanti. Il dipendente vede il contatore, non i pulsanti, e al nuovo anno
    l'avviso «Nuovo anno: chiedi al titolare di azzerare il contatore, così il prossimo foglio
    riparte da 1.». Con `?anno=N` (2024…2099) il modulo propone quell'anno: è il ritorno dopo un
    azzeramento dalla pagina del PDF pronto; un valore fuori si ignora.
  - `GET /labels.pdf` (personale): allegato, `private, no-store`; 400 «Richiesta non valida» senza
    tipo; 422 con il modulo ripieno e «Massimo 100 etichette per volta: hai chiesto N.»; 503 «PDF
    non disponibile».
  - `GET /labels/genera` (solo Go, D-08): stesse regole; la pagina «PDF pronto» con «⬇ Scarica PDF»,
    la proposta del nuovo anno se l'anno **del foglio** è più nuovo dell'ultimo confermato e il
    contatore non è 0 (D-66: il 28/12 si prepara il foglio dell'anno dopo), prima della domanda «Etichette stampate correttamente?», «Sì, stampate» (modulo POST con CSRF e i valori
    del foglio nascosti) e «No» (la stessa pagina con `stampate=no`: «Contatore non aggiornato: puoi
    scaricare di nuovo il PDF e ristampare.»). Funziona senza JavaScript.
  - `POST /labels/conferma` (solo Go, personale): CSRF, le stesse regole (limite di 100 compreso:
    una richiesta forgiata oltre il limite è un 422, un valore non valido un 400) e in più l'anno
    nell'intervallo del modulo, 2024…2099, altrimenti 400. **Scelta tecnica:** un anno forgiato
    (`anno=20267`) confermato bloccherebbe la D-66 per sempre; la regola vale solo per la conferma,
    che c'è solo nel Go, mentre `/labels.pdf` accetta ogni anno come lo standalone. Poi ritorno a
    `/labels` con «Contatore aggiornato: ultimo numero stampato N.» o «Contatore non aggiornato:
    riprova.».
  - `POST /labels/azzera` (solo Go, D-22): azione del titolare con il guscio
    (`azioneDelTitolareConGuscio`); il dipendente torna a `/labels` con «Solo il titolare può azzerare
    il contatore.» e niente cambia. Senza `conferma=1` è la pagina della domanda «Azzerare il
    contatore? Il prossimo foglio ripartirà da 1.» con «Sì, azzera» (POST con CSRF, `conferma=1`,
    `nuovo_anno` e `anno` se c'erano) e «Annulla» (link a `/labels`): la conferma esplicita vale anche
    senza JavaScript. Solo il POST con `conferma=1` azzera, per l'anno mandato (2024…2099) o l'anno di
    oggi, e torna a `/labels?anno=N` (o `/labels` senza anno): dalla pagina del PDF pronto il modulo
    propone così da = 1 per l'anno del foglio, perché il PDF già fatto ha numeri vecchi. Il
    `data-confirm` dei pulsanti è tolto: con la pagina di conferma si sarebbe chiesto due volte.
  - La voce «🏷️ Etichette QR» del menu compare da sola, ora che la rotta c'è (per tutti e due i ruoli).
- **D-66, il modo tecnico:** la chiave nuova `etichette_qr_anno` (solo Go, in
  `importer.ChiaviSoloGo`: una riga così nei dati dello standalone si scarta) si scrive con la
  conferma (il più nuovo fra quello salvato e quello del foglio) e con l'azzeramento (l'anno per cui si
  azzera). La proposta c'è quando l'anno è noto, il contatore non è 0 e l'anno scelto è più nuovo: in
  `/labels` l'anno proposto dal modulo, in `/labels/genera` quello del foglio. Dopo l'azzeramento
  l'anno salvato è quello nuovo, quindi la proposta non torna per quell'anno, nemmeno dopo la ristampa
  confermata di un foglio dell'anno prima. Con i dati importati l'anno non c'è e non si propone niente
  finché non si conferma un foglio.
- **Banco:** scenari `etichette-qr-dipendente` (13 passi) ed `etichette-qr` (49 passi): modulo, PDF
  (formato, testo, QR, intestazioni), ristampa, formato con i decimali, 16×16, oltre 9999, 422 e 15
  rifiuti 400, dipendente e titolare; i passi solo Go (D-08, D-22) fanno la conferma dopo ogni PDF
  dello standalone, così i contatori delle due app tornano uguali e il resto si confronta. Dopo la
  review i passi solo Go provano anche la pagina di conferma dell'azzeramento (D-22) e la proposta del
  nuovo anno nella pagina del PDF pronto per un foglio del 2027 (D-66).
  In `bench/deviazioni.json` 63 righe: 49 D-08 (azione e pulsante del modulo, contatore prima della
  conferma), 6 D-64 e 1 D-21 (testo del PDF), 7 D-66 (proposta e avviso del nuovo anno). D-22 sta nei
  passi `solo_go`.

## Per il manuale

- In «🏷️ Etichette QR» si scelgono i numeri (da, a), l'anno e il formato; al massimo 100 etichette
  per volta. Il programma propone i numeri dopo l'ultimo stampato.
- «Genera PDF» apre una pagina con «⬇ Scarica PDF» e la domanda «Etichette stampate correttamente?».
  Il contatore si aggiorna solo con «Sì, stampate»; con «No» non cambia niente e si può riscaricare
  il PDF e ristampare.
- Ristampare numeri vecchi non riporta indietro il contatore.
- Su ogni etichetta il numero è stampato sotto il QR, nella forma «0045/26»; il QR contiene
  «RIP-2026-0045». Anche le etichette piccole (minimo 16 mm) hanno QR e numero interi.
- Il titolare può azzerare il contatore con «Azzera contatore»: si apre una pagina con la domanda
  «Azzerare il contatore? Il prossimo foglio ripartirà da 1.», «Sì, azzera» e «Annulla». Il
  contatore si azzera solo con «Sì, azzera». Il dipendente vede il contatore ma non il pulsante.
- Se si sceglie un anno più nuovo di quello dell'ultimo foglio confermato (a inizio anno, o a fine
  dicembre preparando il foglio dell'anno dopo), il programma propone al titolare «Nuovo anno:
  ripartire da 1?», nel modulo e nella pagina del PDF pronto; il dipendente vede un avviso che dice di
  chiedere al titolare. Dopo l'azzeramento il modulo riparte da 1 per quell'anno: il PDF già fatto va
  rigenerato.

## Review indipendente

Un sottoagente diverso ha letto il codice e le voci; non ha cambiato codice. Tre difetti rispetto a
quanto approvato da Stefano e alcuni ritocchi, tutti corretti in questa slice:

1. **D-66 sull'anno scelto:** la condizione usava solo l'anno di oggi; il 28/12, preparando il foglio
   2027 con anno = 2027, la proposta non usciva e il 2027 partiva da 351. Ora vale anche l'anno del
   foglio (`NuovoAnnoPer`) e la proposta compare nella pagina del PDF pronto, prima della domanda; dopo
   l'azzeramento si torna a `/labels?anno=2027` con da = 1 (`TestNuovoAnnoSulFoglio`,
   `TestNuovoAnnoNelPDFPronto`).
2. **D-22 senza JavaScript:** la conferma era solo `data-confirm`. Ora `POST /labels/azzera` senza
   `conferma=1` mostra la pagina della domanda e solo «Sì, azzera» azzera (`TestAzzeraEtichetteQR`,
   tabella dei permessi e rotte con il guscio aggiornate, passi del banco).
3. **D-66 dopo l'azzeramento:** `AzzeraEtichette` non scriveva l'anno; un azzeramento nel 2027 seguito
   dalla ristampa confermata di un foglio del 2026 faceva tornare la proposta. Ora scrive
   `etichette_qr_anno` = l'anno per cui si azzera, nella stessa transazione (test con lo scenario della
   review in `TestNuovoAnnoSulFoglio`).
4. **Anno forgiato:** `/labels/genera?anno=20267` seguito da «Sì, stampate» bloccava la D-66 per
   sempre. Ora la sola conferma vuole un anno fra 2024 e 2099, altrimenti 400
   (`TestConfermaAnnoForgiato`); `/labels.pdf` resta come lo standalone.
5. **Ritocchi:** con la proposta del nuovo anno il pulsante generale «Azzera contatore» non c'è (mai due
   pulsanti); nell'audit `label_sheet.counter_reset` `previous` è un numero, come in
   `label_sheet.confirmed`; test del `Flush` del 400 (`TestRifiutoEtichetteFlush`, con un
   ResponseWriter che registra se le intestazioni sono partite prima del corpo).

**Mutazioni della review:** 22, tutte compilate; 21 uccise. È sopravvissuta solo m21 (togliere il
`Flush` del 400), ora uccisa da `TestRifiutoEtichetteFlush` (rifatta dal sottoagente:
`prove/mutante-flush.log`, FAIL).

## Misurato sullo standalone (fab-220, 04/11/2026)

- Con `erl` della release nell'immagine (senza rete): `Float.parse/1` legge «1E5», «5.7E1»,
  «+1.5», «1.5e+1» e non «1.e5», « 57», «57,5» per intero; `Integer.parse/1` legge «+5», «05» e
  non «٥»; `Float.round/2` arrotonda a metà per eccesso sul valore esatto (57.03125 → 57.0313,
  1234.125 → 1234.13, 123456.5 → 123457); `Float.to_string/1` dà «0.0001» ma «1.0e-5»;
  `String.trim/1` toglie U+00A0 e U+2003 ma non U+200B.
- Sul banco (dati importati): intestazioni del PDF, nome del file, formato delle pagine (anche
  57,03125×20,5 → «57x21» e 16×16), contenuto dei QR (lo standalone li stampa trasposti, il lettore li
  legge), 422 e 400 come il Go. A 16×16 mm Chromium taglia il numero a destra del QR: il testo del
  PDF è «00» (D-21, coperto). Il 400 dello standalone arriva senza `Content-Type` e senza
  `Content-Length` (risposta in streaming): il Go ora manda le intestazioni prima del corpo
  (`Flush`), altrimenti il Caddy del banco ci metteva da sé `text/plain` (vedi «Da sapere»).

## Solo ragionato (dal codice, non misurato)

- `stored_integer/2` dello standalone ha interi senza limite: un contatore salvato oltre i 64 bit nel
  Go vale 0 (il default). Un formato salvato «1e400» nello standalone fa cadere la pagina (eccezione
  non presa), nel Go vale il default. Solo per dati guasti.
- `Float.to_string/1` per valori non interi oltre 10^15: non succede (sei cifre significative).
- Il 503 «PDF non disponibile» quando il motore o la transazione falliscono (provato nei test Go,
  non sul banco).

## Da sapere

- **Il 400 senza tipo e il proxy:** anche il 503 «PDF non disponibile» delle altre rotte dei PDF
  (`inviaPDF`) manda le intestazioni insieme al corpo. Dietro un proxy che, come il Caddy del banco,
  sceglie il tipo da sé, uscirebbe `text/plain`. Il banco non provoca mai quel 503, quindi non si vede;
  si sistema con la stessa riga se un giorno lo si prova.
- «No» non scrive niente nel registro: è solo un link.
- I passi del banco non provano il formato piccolo (`label-sheet-small`) oltre il 16×16 del PDF: lo
  provano i test Go.

## Prove

- **Test Go nuovi:** `internal/buste/etichette_qr_test.go` (lettura come `parse/2`, rifiuti, 100,
  nomi e numeri anche oltre 9999, `format_g`, modulo e valori illeggibili, D-08 con la transazione e la
  riga d'audit che manca, conferma con `max` e anno, azzeramento e D-66), `internal/stampe/
  etichette_qr_test.go` (PDF riletto con `internal/pdf/lettura`: pagine, formato, QR diritto, numero
  sotto il QR, dentro i margini e centrato, per 8 formati fra 16×16 e 200×200 e per i numeri oltre
  9999; la disposizione documentata), `internal/web/etichette_qr_test.go` (modulo, PDF, rifiuti 400 e
  422 sulle tre rotte, conferma forgiata oltre 100, CSRF, «No», conferma, riga che non si scrive,
  ruoli, D-66). Tabella dei permessi, menu e rotte con il guscio aggiornati; `ChiaviSoloGo` con
  `etichette_qr_anno`.
- **TDD:** i test sono stati scritti insieme al codice, non prima: nessun rosso salvato.
- **`scripts/check`:** verde (`prove/check-2.log`, sottoagente); il primo giro si era fermato su
  staticcheck (caratteri invisibili scritti nudi in un test) e su `TestChiaviSoloGoComplete` (la
  chiave nuova `etichette_qr_anno`), corretti.
- **Banco** (sottoagente, `fab-220`, `-dati sintetici-import`, istanza nuova e binari ricompilati dal
  codice finale): HTTP verde, 34 scenari e 553 passi, 84 differenze attese (le 21 di prima più le 63
  nuove), 0 inattese; browser verde, 7 scenari e 14 viste, 22 attese, nessun «da decidere». Prima del
  giro completo due giri mirati sui soli scenari nuovi: il primo ha trovato il `Content-Type` del 400
  (corretto) e mostrato che D-66 non si vedeva (l'anno del dipendente era già il 2026: lo scenario
  del dipendente ora usa il 2025); dal secondo vengono le righe delle deviazioni, controllate una per
  una. Il giro su database vuoto non è stato fatto.
- **Copertura** con l'esito del banco: RIP-S-47 e RIP-S-48 chiuse; in tutto chiuse 63, parziali 18,
  rosse 0 (senza banco chiuse 27). Nota: per lo strumento RIP-S-48 è chiusa, ma la stampa diretta
  CUPS della D-08 è della 046 (il piano della T4.4 dice «completa 43, 48»).
- **Dopo le correzioni della review** (sottoagente): `scripts/check` verde (`prove/check-review.log`);
  i test del banco (`tools/banco`) rilanciati dopo l'ultima modifica di `bench/deviazioni.json`, verdi.
  Banco mirato sui soli due scenari delle etichette (fab-220, `-dati sintetici-import`, istanza
  nuova): 62 passi, 63 differenze attese, 0 inattese. Un primo giro mirato aveva 2 inattese: i due
  passi nuovi spostavano l'ultimo modulo da 46 a 48, e le sue due righe D-08 sono state rinumerate.
  Il giro completo resta quello di prima della review (34 scenari, 553 passi, verde).

