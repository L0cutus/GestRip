# 060 — Ritocchi della T4.2: conferma di «✓ Consegnata» senza JavaScript, «Cambia stato», D-80, D-79 estesa

- **Fonte:** coordinatore, 09/11/2026, con le decisioni di Stefano del 09/11/2026 sui Dubbi delle slice
  056–059 (`docs/SVILUPPO.md`, «Decisioni di Stefano del 09/11/2026»). Lo standalone a `go-baseline`
  = `302439d`.
- **Voci:** RIP-H-14 (pulsanti rapidi), RIP-H-15 (menu dello stato), RIP-H-03 e RIP-H-12 (riquadro,
  filtro e segno «⚠ senza QR»), RIP-S-42 (ricevuta), RIP-S-43 (etichetta 76×127).
- **Deviazioni:** **D-80 nuova e fatta**; **D-79 estesa**; D-77 definitiva; estensioni della D-63 e
  della D-68 (solo registrate).
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

### A. «✓ Consegnata» senza JavaScript chiede conferma (RIP-H-14)

- Il modulo di «✓ Consegnata» del riquadro ha un campo `conferma` vuoto (`data-conferma-js`).
- **Senza JavaScript** `POST /repairs/{id}/rapido` con `status=consegnata` e senza `conferma=1`
  (`chiediConsegna` in `lista_azioni.go`) rilegge la busta (`RigaPerQR`) e, se è pronta e non Compro
  Oro, disegna la pagina `conferma_consegna` (in `home.html`): `#quick-delivered-confirm-text`
  «Segnare la busta 0045/26 come consegnata?» (numero visibile, D-64), un modulo con lo stesso
  `lista` (già riscritto da `listaDelModulo`), «Sì» (`#quick-delivered-yes`, `conferma=1`) e
  «Annulla» (`#quick-delivered-no`, link alla lista). Niente si scrive. Una busta che non è pronta,
  che non c'è o un id malformato: la lista con «Transizione non consentita.», come il passo rifiutato.
- **Con JavaScript** resta `data-confirm`: dopo «OK» `app.js` scrive `1` nel campo, il `fetch` lo
  manda e il server non chiede di nuovo.
- La rotta passa a `rottaConGuscio` (la pagina ha il guscio interno, titolo «Riparazioni»).

### B. «Cambia stato» (RIP-H-15)

- Il pulsante `data-solo-senza-js` del menu dello stato si chiama «Cambia stato» (era «Cambia»).

### C. D-80, date gg/mm/aaaa su ricevuta ed etichetta (RIP-S-42, RIP-S-43)

- `DatiRicevuta.Accettata()` (nuovo) e `DatiRicevuta.Prevista()` scrivono la data con
  `dataGiornoMeseAnno`; `stampe/ricevuta.go` usa `Accettata()`. `DatiEtichetta.Scadenza` idem. Un
  testo scritto al posto della data e «Da concordare» restano.

### D. D-79 estesa (RIP-H-03, RIP-H-12)

- `senzaQRSQL` (filtro `senza_qr` e contatore) è ora `stato aperto AND gestrip_senza_etichetta(codice_qr)`:
  una funzione SQL in Go (`internal/buste/senza_qr_sql.go`, `sqlite.MustRegisterDeterministicScalarFunction`
  in `init`) che dà 1 se il codice (anche NULL, vuoto o di soli spazi) non è un'etichetta per
  `EtichettaDi`. Così contatore, filtro e pillola seguono esattamente il segno della lista. La pillola
  non cambia testo.
- La scheda: `datiScheda.AltroFormato`; il segno accanto al titolo anche per un QR di altro formato,
  con il titolo «Busta senza etichetta RIP: il suo QR è di un altro formato».

### E. Documenti

- `docs/SVILUPPO.md`: D-77 definitiva, D-79 estesa, sezione «Decisioni di Stefano del 09/11/2026»
  (D-80, conferma, «Cambia stato», D-77, galleria come estensione della D-68, cursore come estensione
  della D-63, messaggi della fotocamera da tradurre, nomi dei file sul numero interno).
- Manuale: capitolo 04 (date della ricevuta e «Scad.:»), capitolo 06 (riquadro e filtro «⚠ Senza QR»,
  segno nella scheda, «Cambia stato», la conferma senza JavaScript).

## Prove

- `internal/buste`: `TestListaContatori` e `TestListaFiltri` con i codici di prova ora etichette
  (`qrEtichetta`) e buste nuove: un QR di altro formato aperto (conta), un'etichetta scritta « rip-2026-
  0046 » (non conta), un QR di altro formato annullato (non conta), «RIP-2026-45» (nel filtro);
  `TestRicevutaPrevista` (con `Accettata`), `TestDatiDellEtichetta` («20/11/2026»).
- `internal/stampe`: le attese della ricevuta in gg/mm/aaaa; `TestRicevutaComeQuellaDiChromium` applica
  la D-80 al testo di Chromium.
- `internal/web`: `TestConsegnataConfermaSenzaJS` (nuovo: conferma vuota, «0», «sì» → pagina, niente
  scritto; busta non pronta, assente, id malformato → «Transizione non consentita.»; «Sì» consegna),
  `TestRiquadroAzioni` (campo `conferma`, «Cambia stato»), `TestPulsantiRapidi` (con `conferma=1`),
  `TestScriptAzioniDellaLista` (lo script mette `conferma=1`), `TestSchedaNumeroVisibile` (segno e
  titolo con QR di altro formato, aperta e chiusa), `TestSchedaApertura`, `TestHomeRiquadriPortanoAlFiltro`,
  `TestRotteConGuscio`.
- `tools/banco`: `TestNormalizzaD80`.

## Mutazioni

A mano, ognuna presa dai test: il controllo dello stato in `chiediConsegna` tolto (5 FAIL); la funzione
SQL che dà sempre 1 (5 FAIL); il campo `conferma` tolto dal modulo (4 FAIL). Già viste rosse prima di
aggiornare i test: `senzaQRSQL` com'era, la scheda senza `AltroFormato`, `Prevista` con la data grezza.

## Banco

- **Normalizzatore `D-80`** (`tools/banco/confronto.go`): solo una data AAAA-MM-GG subito dopo
  «ACCETTATA», «PREVISTA» o «Scad.:» diventa gg/mm/aaaa. Passi PDF di `ricevuta` e
  `ricevuta-dipendente` → `D-64+D-80` (attese del testo in gg/mm/aaaa), di `etichetta` e delle due
  etichette di `etichetta-scheda-dipendente` → `D-64+D-73+D-80`.
- **D-79:** 183 righe nuove in `bench/deviazioni.json` (totale D-79: 291), dalle inattese del primo
  giro, con uno script usa e getta che le accetta solo se: per `dashboard-senza-qr` cambia solo il
  numero (5 → 18); per le liste `senza_qr=1` (`home-contatori[11]`, `home-filtri[50]`) ogni riquadro
  del Go porta «⚠ senza QR». Il banco **non ammette righe sulle attese** (`atteso …`: «campo non valido
  per il livello HTTP», secondo giro fermato prima di partire): le 51 attese che la D-79 estesa cambia
  solo nel Go (`attributo:dashboard-senza-qr:aria-label` nei passi di `home`, `home-contatori`,
  `home-dipendente`; `testo:5 riparazioni` e `.rep-card@1:id` dei filtri `senza_qr`) sono tolte dagli
  scenari, anche quelle uguali del database vuoto; i campi restano in `confronta`, coperti dalle righe.
- `busta-etichetta[13]` (`solo_go`, la scheda della 36/26 con SINT-ALTRO-36): l'attesa
  `visibile:repair-noqr` diventa «⚠ senza QR».
- **Inventario:** `banco esegui` controlla le D-xx sull'inventario privato, che non ha ancora la D-80.
  Per i giri ho passato `-inventario` con un file dei soli ID approvati (le righe `| D-xx |`
  dell'inventario più `| D-80 |`, nessun altro contenuto), in `to-be-deleted-after-use/slice-060/`.
  **Il coordinatore deve scrivere la D-80 nell'inventario** prima del prossimo giro normale.
- **Conferma della 056:** il campo `conferma` nel modulo non è un campo confrontato: nessuna
  differenza nuova.
- Giri (dati importati, `gestrip-standalone:fab-225`):
  - primo (`s060`): HTTP 48 scenari, 886 passi, 1325 attese, **211 inattese** (D-79 e il passo
    `solo_go`), 0 errori; nessuna inattesa D-80 né della conferma; browser non partito;
  - secondo: fermato dal controllo delle righe sulle attese (sopra);
  - terzo (`s060`, da capo): HTTP **48 scenari, 886 passi, 1508 attese, 0 inattese**, 0 errori;
    browser **12 scenari, 24 viste, 30 attese, 0 inattese**, 0 da decidere, 0 errori: **verde**.
    `banco giu`: «AVANZI: nessuno».
- Il giro sul database vuoto non è stato fatto (come chiesto).

## Controlli

`scripts/check` verde; `scripts/parole-vietate --staged` verde (dopo `git add -A`, poi `git reset`).

## Per il manuale

- Capitolo 04: le date della ricevuta e «Scad.:» dell'etichetta in giorno/mese/anno (fatto).
- Capitolo 06: «⚠ Senza QR» conta anche i QR di altro formato, il segno nella scheda, «Cambia stato»,
  la pagina della conferma senza JavaScript (fatto). Nessuna schermata nuova: la figura 6.1 resta
  giusta; quelle con il riquadro «⚠ Senza QR» (se ce ne sono) mostrano il numero di prima finché non si
  rifanno.

## Dubbi

1. **Funzione SQL nel driver.** `gestrip_senza_etichetta` è registrata nel driver `sqlite` all'avvio
   del pacchetto `buste` (globale al processo). È la via più semplice per avere la stessa regola di
   `EtichettaDi`; l'alternativa era riscrivere la forma dell'etichetta in SQL (fragile). Da sapere: un
   altro strumento che apre il database senza `buste` non ha la funzione.
2. **La pagina della conferma** ha solo la frase e i due pulsanti (con la barra e i pulsanti della
   pagina interna), senza la lista sotto. Se Stefano la vuole sopra la lista come il pannello del QR,
   è un lavoro in più.
3. **Attese tolte per la D-79:** il banco non ammette righe sulle attese, quindi 51 attese del
   contatore «⚠ Senza QR» e della lista `senza_qr` sono uscite dagli scenari (il confronto resta, con
   le righe D-79). Sul database vuoto quei campi restano confrontati fra le due app, ma senza attesa.

## Review


Revisore: sottoagente, 09/11/2026. Nessun difetto ALTO o MEDIO.

- **Conferma senza JavaScript:** giusta. Senza `conferma=1` `chiediConsegna` legge e basta (nessuna
  scrittura, audit invariato); CSRF e ruolo `personale` vengono dalla catena di `rottaConGuscio` come
  prima. La busta nel cestino è esclusa da `RigaPerQR` (`cancellata_il IS NULL`), ma il test non la
  provava: **aggiunta** a `TestConsegnataConfermaSenzaJS` (mutazione: tolto il filtro del cestino in
  `RigaPerQR` → FAIL). BASSA.
- **`gestrip_senza_etichetta`:** robusta. È registrata in `init` del pacchetto `buste`, che è l'unico
  posto da cui parte la query: ogni processo che la usa (serve, comandi, import, test) ha già fatto
  l'`init` prima di aprire il database. Equivale a `SenzaQR || QRAltroFormato` su una busta aperta
  (gli stati chiusi coincidono con `statiChiusi`). **Aggiunto** `TestSenzaEtichettaSQLComeIlSegno`
  (`senza_qr_sql_test.go`): connessione nuova fuori da `db`, NULL, vuoto, spazi Unicode, etichette
  scritte male, BLOB e intero, confrontati col Go (mutazione: BLOB ignorato → FAIL). BASSA.
- **Prestazioni:** 33 000 buste aperte in memoria, contatore 42 ms contro 7 ms del vecchio `trim`
  (regex per riga, nessun indice usabile in entrambi i casi). Con i numeri di un negozio (qualche
  migliaio) resta sotto i 5 ms: accettabile. BASSA, solo da sapere.
- **D-80:** giusta. `Accettata`, `Prevista` e `Scadenza` usano `dataGiornoMeseAnno`; il testo scritto e
  «Da concordare» restano. Il normalizzatore tocca solo la data dopo le tre etichette. Da sapere: un
  testo scritto che sia esattamente una data AAAA-MM-GG resterebbe così nel Go ma verrebbe girato nello
  standalone dal normalizzatore: il banco lo mostrerebbe come inattesa, non lo nasconde. BASSA.
- **Banco:** le 183 righe nuove sono tutte D-79, su `home`, `home-contatori`, `home-dipendente`,
  `home-filtri`: campi solo `dashboard-senza-qr` (numero 5 → 18) e `.rep-card@N` dei due passi
  `senza_qr=1`, e ogni riquadro del Go in quei passi porta «⚠ senza QR». Le attese tolte dagli scenari
  sono solo `dashboard-senza-qr`, `testo:5 riparazioni` e `.rep-card@1:id` dei filtri `senza_qr` (gli altri `-`/`+` sono solo virgole spostate e i testi PDF della D-80): nulla di
  estraneo alla D-79 resta scoperto. Per il database vuoto restano `repairs-empty` e il confronto.
- **Test:** provano quello che dicono (letti `TestConsegnataConfermaSenzaJS`, `TestNormalizzaD80`; i
  due buchi sopra sono gli unici trovati).
- Controlli: `scripts/check` e `scripts/parole-vietate --staged` verdi dopo i due test aggiunti.
