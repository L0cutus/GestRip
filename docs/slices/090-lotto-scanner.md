# 090 — Scanner continuo con la fotocamera nel lotto d'avviso (T4.9, quinta)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/ready_lot_live.ex` (evento
  `scan_continuous`, `scan_add/2`, `scan_refusal/2`, `scan_reply/3`, `qr_outcome/2`, il pulsante `#ready-lot-scan-button` con
  `data-event="scan_continuous"` e `data-count-label="nel lotto"`), `ready_lots.ex` (`add_bag/3`), `assets/js/qr_scanner.js`
  (modo continuo, già portato dalla 079); test «the continuous scanner of the lot (slice 200)» di `ready_lot_live_test.exs`.
- **Voci:** ORE-07 (chiusa con il giro verde s090a). Nessuna migrazione. Deviazioni: D-98 (gemella della D-90, registrata dal
  coordinatore); nessuna gemella della D-91 (Dubbio 1).

## Cosa c'è

- **Pagina** (`internal/web/pagine/lotti.html`): «📷 Scansiona» (`#ready-lot-scan-button`, `type="button"`) nel modulo del QR,
  fra il campo del codice e «Aggiungi dal QR» (come nello standalone), con `data-scansione="/ready-lots/{id}/scansione"`,
  `data-ricarica="/ready-lots/{id}"` e `data-count-label="nel lotto"`; niente `data-target` né `data-submit`. Solo a lotto
  aperto (la sezione dell'aggiunta c'è solo allora). Senza JavaScript non fa niente.
- **Rotta** `POST /ready-lots/{id}/scansione` (`internal/web/lotto_scansione.go`, `f.rotta`, `personale`, CSRF del modulo),
  l'evento `scan_continuous`: il codice è `qr` senza spazi ai bordi (`codiceLetto`, quello della consegna), letto **prima come
  QR e poi come numero** (due chiamate di `AggiungiAlLotto`: solo «Riparazione non trovata.» passa alla seconda), quindi la
  logica di aggiunta della 089 senza copie: rifiuti di ORE-06 con «già avvisata» che vince, numero D-64/D-95, una scrittura
  che decide, riga `ready_lot.bag_added` con l'utente. Risponde in JSON `{ok, message, count, info}` (`rispondiEsito`, ora
  comune con la consegna):
  - busta nuova: `ok` e `info` veri, «✓ Aggiunta busta #0045/26» (numero visibile D-64);
  - «↻ Busta già in questo lotto»: `ok` e `info` falsi (nello standalone `scan_continuous` mette sempre un flash rosso, anche
    per «già in questo lotto», che nel campo è un esito: così alla chiusura la pagina lo mostra come ultimo errore);
  - codice di nessuna busta: «Riparazione non trovata. («X»)»; ogni altro rifiuto, il testo del campo senza il codice
    (compresi «Busta già nel lotto #N: …», «Più buste corrispondono…», «Lotto non disponibile.»);
  - `count`: le buste del lotto rilette (anche dopo un rifiuto; 0 per un lotto che non c'è).
  Un id che non è un numero positivo è 404; guasto: la pagina d'errore con la riga nel registro («busta scansionata non
  aggiunta al lotto», «buste del lotto non lette»). Niente messaggio nella sessione.
- **Lettore** (`app.js`): **nessun codice nuovo**. Il modo continuo della 079 è già generico (indirizzo, pagina da rileggere
  ed etichetta del conto vengono dal pulsante); cambiato un commento. La pagina riletta alla chiusura ha i due campi di testo
  del lotto (`#ready-lot-bag-number`, `#ready-lot-scan-code`), che il lettore rimette come erano.
- **Banco** (`tools/banco/fotocamera.go`, `browser/banco.mjs`, `bench/browser/lettore-qr.json`): sotto.
- **SVILUPPO:** punto «Scanner continuo del lotto» dopo la pagina del lotto, e la prova `scansione` nel punto della fotocamera
  finta.

## Scelte

1. **Una rotta sopra l'aggiunta che c'è.** Nessuna seconda strada: la scansione chiama `AggiungiAlLotto` (la stessa del
   campo), quindi ogni regola della 089 vale anche qui.
2. **`rispondiEsito` condiviso** con la consegna (marshal, `no-store`, `jsonCoda`); il resto delle due rotte è diverso
   (consegna: «già in lista» con info; lotto: errore).
3. **Il banco generalizza invece di copiare:** `fasiDelloScanner` (le tre fasi della finestra, estratte da `letturaConsegna`),
   `oreficeriaPropria` e `bustaPropria` (estratte da `lottiBuste`), `sequenzaScanner` (il video, due codici).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-090/prove/rosso.txt`): pulsante, rotta (404), tabella dei
  permessi («la tabella nomina "POST /ready-lots/{id}/scansione", che la faccia interna non registra»), il pulsante in
  `TestPaginaLotto`; la fotocamera del lotto (`codiceLotto` non definito).
- **Pagine** (`internal/web/lotto_scansione_test.go`): `TestPulsanteScansionaDelLotto` (markup esatto fra il campo e
  «Aggiungi dal QR», niente `data-target`/`data-submit`, assente a lotto uscito), `TestScansioneDelLotto` (per QR con gli
  spazi, già nel lotto, per numero, prima il QR, codice sconosciuto e `<b>`, cestinata, i sette rifiuti con i testi e il conto,
  rifiuto della prima lettura che non prova la seconda, più buste, `qr` mancante o mappa o lista, audit con l'utente e
  nessuna riga per i rifiuti, nessun flash nella sessione, lotto uscito e che non c'è, id non valido 404, senza token 403),
  `TestScansioneDelLottoDelTitolare`, `TestScansioneDelLottoGuasto`.
- **Ritoccati:** `TestPaginaLotto` (il pulsante c'era come assenza, segnaposto della 089), la tabella dei permessi
  (`ruoli_test.go`, ORE-07).
- **Banco** (`tools/banco/fotocamera_test.go`): `TestFotocameraDelLotto` (stesso controllo fotogramma per fotogramma di
  `TestFotocameraDellaConsegna`, ora in `controllaSequenzaScanner`, e il codice scritto anche in `banco.mjs`).
- **Mutazioni:** 16 a mano su `lotto_scansione.go` e `lotti.html` (`to-be-deleted-after-use/slice-090/mutazioni/`), tutte
  prese da un'asserzione (una non compilava, rifatta come «conto + 1»).

## Banco

**Giro s090a (17/11/2026, sessione principale): verde.** HTTP 81 scenari, 1293 passi, 0 inattese, 0 errori; browser 27
scenari, 54 viste, 40 differenze attese, 0 inattese, 0 errori; la prova `scansione` ha girato su PC e telefono nelle due
app con gli stessi valori. Memoria disponibile minima 17,0 GB. Copertura con l'esito: 353 voci, chiuse 196 (ORE-07 chiusa),
rosse 0, parziali 16, aperte 130, fuori v1 11. Un solo giro (un primo `banco esegui` si era fermato prima degli scenari per
un argomento sbagliato nello script del giro).

Nessuno scenario nuovo: la prova `scansione` è dentro `lettore-qr`
(dopo `scanner`, con i suoi Chromium), `lettore-qr` ha ORE-07 fra le voci. Ci si aspettano gli stessi 71 + 10 scenari
dell'ultimo giro e, nel browser, le righe nuove `lotto.*` di `lettore-qr` (PC e telefono).

- **Database vuoto:** solo `lotto.scansione.lista` «📦 Avviso oreficeria».
- **Dati**, per vista, nelle due app: la prova crea l'oreficeria «Zz Banco 090» e una sua busta pronta del 2025 con QR
  `SINT-LOTTO-090` (il PC; il telefono le trova, e se manca l'oreficeria o la busta le crea), apre il suo lotto da `/ready-lots` (sul telefono è
  lo stesso del PC, vuoto dopo «Rimuovi»). Nel campo «Numero busta» scrive «0099/26» senza mandarlo; il 📷; `lotto.y4m`
  (2,5 s di bianco, la busta, bianco, `SINT-NIENTE-079`, bianco, la busta, bianco): `aggiunta.messaggio` «✓ Aggiunta busta
  #N · 1 nel lotto» con `1760` e `60`; `sconosciuto.messaggio` «Riparazione non trovata. («SINT-NIENTE-079») · 1 nel lotto»
  con `330 330` e `[120,80,120]`; `ripetuta.messaggio` «↻ Busta già in questo lotto · 1 nel lotto» con `330 330`;
  `finestra` «aperta». «Chiudi»: `chiusa.percorso` «il lotto», `chiusa.conteggio` «1», `chiusa.righe` «#N — Anello lotto 090
  Rimuovi», `chiusa.messaggio` «(nessuno)», `chiusa.errore` «↻ Busta già in questo lotto» (il rosso resta, il verde si chiude
  al tocco), `chiusa.campo` `"0099/26"`, `registro.ready-lot-bag-added` «1»; sul PC «Rimuovi», `togli.conteggio` «0».
- **Se qualcosa è rosso, da guardare per primo:** (a) il numero «8100/25» dello standalone (`numeroDel2025`) e le liste di
  `lotti-buste` (numeri 8090-8099); (b) `lotto.scansione.aggiunta.messaggio` «(nessuno)» dopo 45 s: Chromium non legge il video o
  `SINT-LOTTO-090` non è la busta (creata nel PC: se il PC è caduto prima, il telefono la crea lui); (c) `chiusa.errore` nello
  standalone: il flash rosso di «già in questo lotto» è quello di `scan_continuous`; (d) un banco con `SCENARI` mirato su
  `lettore-qr` gira anche le altre prove (le fotocamere sono tutte di questo scenario).

## Per il manuale

- Nella pagina del lotto c'è «📷 Scansiona» accanto ad «Aggiungi dal QR»: la fotocamera resta accesa e ogni busta
  inquadrata entra subito nel lotto. Sotto il video si legge l'esito («✓ Aggiunta busta #0045/26 · 3 nel lotto») e si sente
  un bip acuto.
- Una busta già nel lotto, un codice sconosciuto o una busta che non può entrare (già avvisata, di un'altra oreficeria, non
  pronta, in un altro lotto) danno due bip gravi e il motivo.
- La stessa busta tenuta ferma davanti alla fotocamera non si ripete: per rileggerla la si toglie dall'inquadratura per un
  paio di secondi.
- «Chiudi» torna al lotto con la lista aggiornata e l'ultimo errore, se c'è stato; quello che si stava scrivendo nei campi resta.

## Dubbi

1. **Deviazioni gemelle.** D-98 (gemella della D-90: la lista sotto la finestra si rilegge solo alla chiusura) sì, gemella della D-91 **no**:
   nello standalone solo `scan_continuous` manda `qr-outcome` (il campo «Numero busta» e «Aggiungi dal QR» del lotto non
   suonano), quindi il Go non è diverso.
2. **«Già in questo lotto» è un errore nella scansione e un esito nel campo.** È lo standalone (flash rosso in
   `scan_continuous`, verde `refuse/2` nel campo): il Go lo copia (`info` falso). Nessuna D.
3. **Rilettura del conto che fallisce dopo un'aggiunta riuscita** (preesistente dalla 079, uguale nella consegna): se
   l'aggiunta riesce ma `BusteDelLotto` fallisce, il server dà la pagina 500 e `app.js` mostra «⚠ Errore di rete: la busta non è
   stata aggiunta, riprovo…»; alla ripresa la stessa busta risponde «↻ Busta già in questo lotto». **Raccomandazione:** solo in
   lista, nessun cambio (il guasto del database è un caso raro e la busta c'è).

## Revisione

Revisore indipendente: 31 mutazioni, 29 uccise, 1 sopravvissuta (L18, `codiceLetto(r)` al posto di `r.PostFormValue("qr")`:
poi coperta con un codice sconosciuto con spazi ai bordi e un `qr` ripetuto, dove vale l'ultimo come Plug; rifatta a mano,
ora muore), 1 non compilante. Quattro rilievi, tutti BASSI, nessuno ALTA o MEDIA; chiusi: deviazione registrata come D-98,
casi di L18, la prova del banco che crea anche la busta se manca, la nota del Dubbio 3.
