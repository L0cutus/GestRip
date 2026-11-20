# 108 — Nuova busta con il «Dettato AI» (T4.10, ottava)

- **Fonte:** standalone a `go-baseline`: `lib/gestrip_standalone_web/live/workshop_live.ex` (`#assist-card`, `#assist-disabled`,
  `#manual-intake` con `intake_open?` e la voce «Inserimento manuale senza AI», `handle_event("assist_extract")`,
  `repair_form/1`, `assist_repair_attrs/1`, `assign_customer_pair/3`, `same_customer_name?/2`, `assign_assist/1`,
  `assist_summary/1`, `assist_refusal/2`), `lib/gestrip_standalone/assist.ex` (`extract_with_model/3`, `shape/3`,
  `read_answer/3`), `assist/prompt.ex` (`@fields`, per la risposta programmata del banco). Il collegamento al modello, le
  regole del nome e le date sono quelli delle 103-107.
- **Voci:** **RIP-H-42 chiusa** (segno su `TestDettatoRiempieIlModulo`, scenario browser `dettato-nuova`); **RIP-H-43 chiusa**
  (segno su `TestDettatoOmonimiECorrezione`; le tabelle sono quelle della 104-105, il «N° busta» lo dà il contatore per la
  D-64). **RIP-H-45 resta parziale:** i messaggi dell'estrazione ci sono tutti, ma la voce comprende quelli della trascrizione
  e gli eventi del registratore (109-110), quindi niente segno. RIP-H-46 e RIP-H-47 restano parziali (trascrizione, 109).
  **D-106** fatta, nella forma aggiornata da Stefano il 20/11/2026 (omonimi con lo stesso telefono: si propone). Nessuna D
  nuova. **Deviazione dal piano, approvata dal coordinatore il 20/11/2026:** lo scenario HTTP `nuova` non si allarga (resta
  com'è); il confronto «nelle due app puntate al modello finto» lo fa lo scenario browser `dettato-nuova`, con D-18 e D-20 fra
  le differenze attese (Dubbio 1 della prima stesura, strada (c)).

## Cosa c'è

- **`internal/web/dettato.go`** (nuovo): il riquadro e `POST /nuova/estrai` (personale, con il guscio, CSRF come gli altri
  moduli); vedi SVILUPPO, «Nuova busta con il «Dettato AI» (slice 108…)». `negozioDelDettato` costruisce `assist.Negozio`
  da `buste.Scelte` (oreficerie esterne) e `buste.ClientiNoti` (tutti i clienti); `valoriDellaProposta` è `repair_form/1`.
- **`internal/web/nuova.go`:** `leggiModuloMandato` (la lettura del modulo, ora comune a «Genera busta» e all'estrazione);
  `datiNuova` con `Assistito`, `Dettato`, `Aperto`; `moduloNuova` legge se l'estrazione è configurata; `Buste` ha
  `ClientiNoti` e `GemelleAperteDi`; «Genera busta» rifiutata rimostra la casella del dettato. **`pagine/nuova.html`:** `#assist-card` (o `#assist-disabled`), `#manual-intake` aperto o chiuso, voce del
  sommario. **`statici/app.js`:** il modulo del dettato si porta dietro il modulo della busta e `intake_aperto`, «Genera busta» la
  casella del dettato; l'estrazione aspetta le foto che si stanno salvando come «Genera busta» e nel giro va con fetch;
  **`app.css`:** stili del riquadro.
- **`internal/web/assistente.go`:** l'interfaccia `Assistente` ha `EstrazioneConfigurata` ed `EstraiDettato`.
- **`internal/assist/dettato.go`** (nuovo): `Impostazioni.EstrazioneConfigurata` (`LeggiEstrazione(...).Usabile()`) e
  `EstraiDettato` (configurazione riletta, rifiuti che non costano, poi il negozio dal chiamante con una funzione, poi
  `Collegamento.Estrai` della 106 con «oggi» nell'ora del negozio); `modello.go`: i rifiuti che non costano in
  `controllaPrima`, usati anche da `Collegamento.Estrai`.
- **`internal/assist/nome.go`:** D-106 (`telefonoDellUnico`).
- **`internal/buste/scelte.go`:** `ClientiNoti` (list_customers/0, per nome e id, nessun audit);
  **`etichetta_scritta.go`:** `GemelleAperteDi` (gli avvisi delle etichette rimessi dopo un rifiuto del dettato).
- **`docs/domande-aperte.md`:** nota 9 (due test instabili sotto carico, BASSA 7 della revisione).
- **Banco:** `bench/browser/dettato-nuova.json`, la funzione `dettatoNuova` in `tools/banco/browser/banco.mjs` (in `IN_FONDO`,
  dopo `assistente-salva`), due righe D-18 e D-20 in `bench/deviazioni.json`, l'elenco in `tools/banco/browser_test.go`.
- **SVILUPPO:** sezione nuova; la regola (6) e i test della sezione della 105 aggiornati per la D-106.

## Scelte

1. **Un modulo a parte per il dettato, con il modulo della busta copiato da app.js.** Lo standalone ha due moduli LiveView;
   senza LiveView il dettato deve arrivare al server insieme al modulo della busta, perché un rifiuto non lo tocchi. L'altra
   strada (la casella e il pulsante associati al modulo della busta con `form=`/`formaction`) avrebbe fatto di «✦ Estrai con
   AI» il pulsante predefinito del modulo (è prima nel documento): l'Invio in un campo, o la pistola del QR, avrebbe estratto
   invece di salvare. Con JavaScript un rifiuto lascia il modulo com'è e com'era aperto; **senza JavaScript** arriva solo il
   dettato: la riuscita riempie il modulo (come chiede il piano), un rifiuto ridisegna il modulo nuovo. Le foto: l'estrazione
   aspetta quelle che si stanno salvando nel browser come «Genera busta» e, nel giro con le foto solo in memoria, va con
   fetch, così non si perdono (MEDIA 2 della revisione).
2. **La riuscita rifà il modulo come `repair_form/1`:** i campi della proposta, e artigiano, note interne e codice QR vuoti
   come in un modulo nuovo (lo standalone rimpiazza il changeset); l'avviso del QR mancante si azzera (`qr_warned` false), come
   gli avvisi delle etichette (il codice torna vuoto). Restano l'identificativo dell'invio e la chiave delle foto: le foto
   scelte prima restano di questo modulo. Il numero di busta non si propone (D-64).
3. **Modulo aperto o chiuso:** chiuso con l'estrazione configurata, aperto dopo una riuscita (`intake-reveal`) e dopo «Genera
   busta»; dopo un rifiuto come era (con JavaScript, `intake_aperto`), senza JavaScript chiuso come una pagina nuova. Gli
   avvisi delle etichette dopo un rifiuto con la regola di «Genera busta» (BASSA 1): il codice simile a un'etichetta se è
   ancora nel campo, le buste aperte con il codice già avvisato (rilette), l'avviso del QR mancante solo senza di loro.
4. **Estrazione tolta dopo l'apertura della pagina:** la pagina resta com'era all'apertura, come lo standalone che decide al
   mount (riquadro, «Inserimento manuale senza AI», chiuso se non era aperto), con «Nessun modello è configurato: …» (BASSA 6).
   Senza `Dipendenze.Assistente` è lo stesso rifiuto, senza chiedere niente.
5. **D-106 aggiornata (Stefano, 20/11/2026):** contano i clienti con lo stesso nome confrontabile (la chiave di `riconosci`:
   «Nicolò Rossi» e «Nicolo Rossi» sono omonimi, MEDIA 3); se hanno tutti lo stesso telefono (`telefono.Normalizza` e poi
   `telefonoConfrontabile`: «333 1234567» e «+393331234567» sono lo stesso numero) si propone quello del primo per nome e id
   com'è salvato, e il salvataggio trova la scheda con quel telefono; con numeri diversi il campo resta vuoto.
6. **Il negozio lo legge la pagina** (`buste.ClientiNoti` e le oreficerie di `Scelte`), come dice il piano: `assist` non
   importa `buste` e `buste` non importa `assist`. `EstraiDettato` riceve una funzione e la chiama solo dopo i rifiuti che non
   costano (non configurata, testo corto o lungo), come lo standalone legge oreficerie e clienti dopo i controlli (BASSA 5).
7. **Banco HTTP non allargato:** deviazione dal piano approvata dal coordinatore il 20/11/2026 (vedi Voci).
8. **Dopo «Genera busta» rifiutata la casella del dettato resta piena** (decisione del coordinatore sul Dubbio 5): app.js la
   copia nel modulo della busta e il server la rimostra; il messaggio sotto no.

## Prove

- Test nuovi: `internal/web/dettato_test.go` (riquadro con e senza estrazione, anche per il dipendente; riempimento con
  clienti e oreficerie inventati, D-18, D-20, D-104, compro oro, audit con i soli caratteri, nessuna busta salvata, invio e
  chiave tenuti, avvisi azzerati, salvataggio della proposta; D-106 e correzione del nome sulla pagina; rifiuti corto, lungo,
  illeggibile, 500 con il modulo intatto, aperto con `intake_aperto`, senza JavaScript; non configurato e senza dipendenza;
  senza CSRF e CSRF nel modulo del dettato; righe `\r\n`; riservatezza del registro e dell'audit; guasti del database e dei clienti;
  «Genera busta» rifiutata con il modulo aperto; il negozio dal database, per nome e id;
  «oggi» del negozio: il giorno dei test è il 13/03/2026, diverso da quello in cui girano, e un caso alle 23:30 UTC, quando a
  Roma è già il giorno dopo; il browser che se ne va ferma la domanda al modello, «closed» nell'audit).
  `internal/assist/dettato_test.go` (`EstrazioneConfigurata`: indirizzo senza modello o con modello di spazi non basta). `internal/assist/nome_test.go`:
  i casi degli omonimi secondo la D-106 aggiornata (numeri diversi → vuoto, stesso numero → proposto, «Nicolò»/«Nicolo» con
  «333 1234567» e «+393331234567» → proposto, con numeri diversi → vuoto, uno senza telefono → vuoto, telefono dettato che
  vince, nome unico, spazi ai bordi). `internal/buste/etichetta_scritta_test.go` (`GemelleAperteDi`). Sulla pagina anche gli
  omonimi con lo stesso numero, gli avvisi delle etichette dopo un rifiuto, l'oreficeria scelta che resta segnata, la
  casella del dettato dopo «Genera busta» rifiutata, l'estrazione tolta dal database dopo l'apertura, i clienti letti solo
  dopo i rifiuti che non costano. Aggiornati `ruoli_test.go`, `guscio_test.go`, `a_capo_test.go`, `tools/banco/browser_test.go`.
- `scripts/check` verde (copertura senza banco: 353 voci, 0 rosse).
- **Mutazioni a mano** (script `to-be-deleted-after-use/slice-108/mutazioni/muta.py`): 54 scritte, 2 non applicabili, 7 non
  compilanti; delle **45 compilanti** 39 uccise al primo giro, 6 vive: 4 uccise rafforzando i test (`intake_aperto` diverso da
  «1», `qr_avvisato` diverso da «1», `EstrazioneConfigurata` con il solo indirizzo, CSRF tolto dal modulo del dettato: il
  controllo di prima trovava il CSRF di un altro modulo), 2 equivalenti (D-106 con `trovati > 1`: con nessun cliente il
  telefono è comunque vuoto; `rows.Err()` di `ClientiNoti` ignorato: non provocabile con SQLite in un test). 0 vive.
- **Revisione indipendente (Opus): 0 ALTE, 4 MEDIE, 7 BASSE, tutte chiuse.** MEDIA 1: `pagina.manuale` del browser leggeva
  anche la freccia «⌄»: ora il primo span del sommario (le attese non cambiano, le ATTESE restano 2). MEDIA 2: l'estrazione
  aspetta le foto come «Genera busta» e nel giro va con fetch. MEDIA 3: omonimi con il nome confrontabile, unita alla D-106
  aggiornata (Scelte 5). MEDIA 4: coperta dalla decisione sul banco HTTP (strada (c)). BASSA 1: avvisi delle etichette dopo
  un rifiuto con la regola di «Genera busta» (`buste.GemelleAperteDi`). BASSA 2: oreficeria segnata dopo un rifiuto. BASSA 3:
  caso alle 23:30 UTC («oggi» del negozio). BASSA 4: il browser che se ne va ferma la domanda al modello. BASSA 5: clienti
  letti dopo i rifiuti che non costano. BASSA 6: estrazione tolta dopo l'apertura, pagina com'era al mount. BASSA 7: i due
  test instabili sotto carico in `docs/domande-aperte.md` (nota 9), non corretti.
- **Mutazioni del revisore:** 56 scritte, 53 compilanti, 46 uccise, 4 equivalenti (7, 49, 52, 53), 3 vive (23, 54, 55) poi
  uccise dai test delle BASSE 2, 4 e 3.
- **Secondo giro dell'implementatore sui pezzi corretti** (`muta2.py`): 30 scritte, 5 non compilanti; delle **25 compilanti**
  21 uccise al primo giro, 4 vive poi uccise o tolte: `!d.Assistito` nel modulo aperto dopo un rifiuto era codice morto (un
  rifiuto arriva sempre dal riquadro: tolto, e le due mutazioni della riga nuova sono uccise); tre su `GemelleAperteDi`
  (codice vuoto, solo aperte, errore del database) uccise dal test nuovo. 0 vive.
- Implementatore Opus: ~420k token, ~75 minuti in tutto (correzioni della revisione comprese).

## Banco

Non lanciato (lo lancia il capo-slice). Comando, con il «memoria libera» e la guardia sotto 10 GB, binari in
`to-be-deleted-after-use/slice-108/banco-bin/`:

```
banco su -nome s108a -immagine gestrip-standalone:fab-225 -gestrip <banco-bin>/gestrip -dati sintetici-import
banco esegui -nome s108a -banco <banco-bin>/banco
GESTRIP_BANCO_ESITO=to-be-deleted-after-use/banco/grgo-s108a/esito/esito.json scripts/check
docker logs grgo-s108a-app > …   # prima di banco giu
banco giu -nome s108a
```

Atteso: HTTP uguale a s107c (nessuno scenario HTTP cambiato); browser con `dettato-nuova` in fondo, 2 ATTESE (D-18
`estratto.consegna`, D-20 `estratto.note`, PC). Lo scenario non è mai stato provato contro lo standalone vero (Dubbi 3).

**Giro s108a (20/11/2026): VERDE al primo giro.** HTTP 90 scenari, 1370 passi, 0 inattese, 0 errori; browser 32 scenari,
64 viste, 44 differenze attese (comprese D-18 e D-20 di `dettato-nuova`), 0 inattese, 0 errori; la guardia sotto 10 GB
non è mai scattata, nessun «busy» o «locked» nel log dello standalone. Copertura con l'esito: 353 voci, chiuse 218
(RIP-H-42, RIP-H-43), rosse 0, parziali 16, aperte 108, fuori v1 11.

## Per il manuale

- Con il modello configurato («Impostazioni › Assistente»), «Nuova riparazione» si apre con il riquadro «1 · Incolla il testo
  dettato»: si scrive (o si incolla) la descrizione libera della busta e si preme «✦ Estrai con AI». Il modulo sotto
  («Inserimento manuale senza AI») resta chiuso finché non serve: si apre da solo con i dati estratti.
- Il modulo si riempie con nome e telefono del cliente, oreficeria, data di oggi, consegna prevista (calcolata dalla frase
  detta: «fra due settimane», «venerdì», «entro fine mese»…), lavorazione, note per il cliente («Busta 31 — Acconto 20 € — …»),
  preventivo e la spunta compro oro se si è detto «oro vecchio». Artigiano, note interne ed etichetta si scrivono a mano.
- Sotto la casella: «✓ Dati estratti. Verifica e correggi prima di salvare.», o «✓ Nome cliente corretto da «X» a «Y».» se il
  negozio ha riconosciuto il cliente dal telefono o dal nome. Niente si salva finché non si preme «Genera busta».
- Se il telefono non è stato detto e in anagrafica ci sono più clienti con lo stesso nome (anche scritto con o senza
  accenti) e numeri diversi, il telefono resta vuoto: lo sceglie chi accetta la busta. Se hanno tutti lo stesso numero, si
  propone quello.
- I messaggi d'errore: testo troppo corto (meno di 10 caratteri) o lungo (oltre 5000), «Il modello non ha risposto con una
  scheda leggibile, riprova.», «AI temporaneamente non disponibile, riprova fra qualche minuto.». Un errore non cambia quello
  che è già scritto nel modulo.
- Senza modello configurato la pagina mostra «🔒 Dettato AI … non è attualmente disponibile» e il modulo, come prima.

## Dubbi

1. ~~Banco HTTP non allargato~~ → **deciso dal coordinatore il 20/11/2026: strada (c)**, `nuova.json` resta com'è e il
   confronto lo fa il browser `dettato-nuova` (deviazione dal piano, vedi Voci). Le altre strade, per memoria: (a)
   configurare l'estrazione già in `banco su` (cambia le attese di `assistente` e `assistente-salva`); (b) un livello HTTP
   dopo il browser.
2. ~~D-106 con due schede uguali~~ → **D-106 aggiornata da Stefano il 20/11/2026:** stesso telefono per tutti gli omonimi →
   si propone; numeri diversi → vuoto (Scelte 5).
3. Lo scenario `dettato-nuova` non è mai stato provato contro lo standalone vero: il primo giro potrebbe trovare differenze
   di selettori o di tempi (per esempio il clic su `#manual-intake > summary` nello standalone, il messaggio scritto sul posto).
   Il microfono dello standalone (la trascrizione resta configurata da `assistente-salva`) non lo guardo: nel Go arriva con
   le 109-110.
4. Con JavaScript l'estrazione ricarica la pagina (non riempie i campi sul posto come LiveView), dopo aver aspettato le foto
   che si stanno salvando; nel giro va con fetch e la cronologia resta su `/nuova`. Senza giro, l'indirizzo dopo
   un'estrazione è `/nuova/estrai`: ricaricarla ripete il POST (il browser chiede), come per gli errori di `/nuova`.
5. ~~Casella del dettato dopo «Genera busta» rifiutata~~ → **deciso: resta piena** (Scelte 8). Il messaggio sotto la casella
   invece non si rimostra (lo standalone lo tiene): non incide sui dati.
