# 062 — Foto e preventivo della pagina del cliente (T4.5, seconda slice)

- **Fonte:** coordinatore, 09/11/2026; T4.5 del PIANO. Lo standalone a `go-baseline` = `302439d`.
- **Voci:** CLI-04 (foto sulla pagina), CLI-05 (blocco del preventivo), CLI-06 (approvazione), CLI-07
  (rifiuto con conferma e motivo), CLI-09 e RIP-S-22 (risposta nella scheda e revoca). **Non fatta:**
  CLI-08 (avviso Telegram al negozio), perché il Go non ha ancora il canale Telegram (T4.6) né la
  campanella (ADM-05).
- **Deviazioni:** D-64 (numero), D-47 applicata anche alle foto pubbliche (vedi Dubbi). Nessuna nuova.
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'è

- **Dominio** (`internal/buste/preventivo.go`, `foto_pubbliche.go`), copia di `QuoteDecision` e di
  `read_public_media/5`:
  - `ApprovaPreventivo` e `RifiutaPreventivo` partono dal token (stessa ricerca della pagina:
    impronta, scadenza, revoca, cestino); l'unica autorità è l'UPDATE protetto (stato `preventivo`,
    nessuna risposta, fuori dal cestino). Il perché del no si chiede dopo, e una risposta data vince
    sullo stato (`claim_refusal/2`). L'approvazione porta a `in_lavorazione`, il rifiuto non tocca lo
    stato; ogni risposta azzera il suo segno «letto» (filtro «notifiche» della home); riga in
    `eventi_preventivo` (parte `customer` o `oreficeria`, attore `public`) e audit `quote.approved` /
    `quote.rejected` con `party`, `status`, `reason_given` (mai il testo del motivo). Motivo: spazi
    Unicode ai bordi tolti, primi 300 grafemi, vuoto = nessuno;
  - `RevocaRisposta(id, "approved"|"rejected")` è `revoke-quote`: revoca la risposta che la pagina
    mostrava, mai una rilettura; UPDATE protetto (niente da revocare = `ErrNonApprovato` /
    `ErrNonRifiutato`, nessuna scrittura); la revoca del rifiuto riporta `annullata` → `preventivo` e
    azzera la conservazione nello stesso UPDATE (CASE, come `release/4`); riga `shop` nella storia e
    audit con `status` e `answered_at`;
  - `PaginaCliente` ha ora `Risposta` (`public_state/1`: niente fuori da `preventivo`) e `Foto`
    (`visible_for/2` con il pubblico del link); `Scheda` ha `MotivoRifiuto` e `StoriaPreventivo`;
  - `LeggiFotoCliente`: link morto = nil senza audit; busta trovata = una riga
    `repair.public_photo_viewed` per lettura («ok»/«denied», `photo_id` ripulito, `audience`,
    `variant` per la miniatura), dopo aver letto i byte.
- **Pagine** (`internal/web/pagina_cliente.go`, `preventivo.go`, modelli):
  - `GET /r/{token}/foto/{foto}`: no-store e no-referrer per le due varianti, `x-photo-variant`, tipo
    dei byte; ogni rifiuto è il 404 «Foto non disponibile» senza tipo (come la rotta del personale);
    il limite non conta le foto (già in `limiti.go`);
  - `POST /r/{token}/preventivo/approva|rifiuta` con `rottaPubblica` (CSRF della faccia pubblica,
    intestazioni delle pagine), redirect alla pagina con il messaggio nel cookie di sessione della
    faccia pubblica (il flash di `back/4`), mostrato una volta (`public-flash-info` /
    `public-flash-error`); token che non apre niente = il 404 della pagina, contato dal limite;
  - pagina: blocco `#public-quote` nell'ordine dello standalone (approvata, rifiutata con motivo, «in
    corso di definizione», pannello di conferma con `?rifiuta=1`, richiesta), galleria
    `#public-repair-photos` con miniature e visualizzatore della scheda;
  - scheda: riquadro `#repair-quote` (c'è se c'è una storia), risposta con data, motivo, «Revoca» con
    il `data-confirm` dello standalone, storia; `POST /repairs/{id}/preventivo/revoca` per titolare e
    dipendente, con i quattro messaggi dello standalone.
- **Banco:** passo `token` con tre buste in più (1/26 con le foto, 9/26 approvata, 10/26
  dell'oreficeria rifiutata con motivo); scenari `pagina-cliente-foto` e `pagina-cliente-preventivo`
  (doppia risposta, risposta opposta, busta non in preventivo, token sbagliato e revocato, POST senza
  CSRF, flash una volta).

## Prove

- Test Go del dominio: stato pubblico (senza importo, con importo, centesimi, fuori da preventivo,
  approvata riportata in preventivo), approvazione (stato, segni, storia, audit, nessun
  `status_changed`, versione), doppia risposta e opposta (anche dopo un ritorno a preventivo e con la
  busta annullata), 8 risposte insieme (una sola passa), rifiuto (motivo vuoto, spazi, bordi, 300
  grafemi con emoji composta, parte oreficeria, audit senza testo), token non valido (inesistente,
  vuoto, revocato, cestino, scaduto alla scadenza esatta) senza scritture, revoca dell'approvazione
  (stato resta, doppia revoca, risposta su un preventivo revocato, nuova risposta dopo il ritorno a
  preventivo, answer non valido, busta inesistente), revoca del rifiuto (annullata → preventivo e
  conservazione azzerata; non annullata resta).
- Test web: blocco (tre forme, `?rifiuta=` con valori sbagliati), risposte via HTTP (403 senza CSRF,
  redirect, flash una volta, testi dello standalone, motivo scappato, 404 su token sbagliato e
  `a%2Fb` senza audit, token mai nel registro), limite dai POST, foto (galleria, intera, miniatura,
  audit esatto, b2b, cinque rifiuti con le loro righe `denied`, link revocato e inventato senza
  audit, foto fuori dal limite), riquadro della scheda (rifiutata, pagina vecchia, answer non valido,
  revoca, approvata, id malformato e busta inesistente); tabella dei permessi.

## Mutazioni

18 a mano (`to-be-deleted-after-use/slice-062/mutazioni.sh`): UPDATE protetto senza il rifiuto, stato
non scritto, 301 grafemi, `reason_given` sempre vero, conservazione non azzerata, revoca senza guardia,
stato pubblico senza filtro, ricerca senza revoca, pubblico sempre cliente, audit solo sugli «ok»,
flash informativo come errore, flash che non si toglie, miniatura in cache, riquadro sui segni invece
che sulla storia, attore assente, risposta data non nominata, `?rifiuta` senza il controllo del testo,
parte sempre cliente. 16 uccise subito; `?rifiuta` è equivalente (con le parentesi il valore è già
vuoto); la parte sempre cliente era sopravvissuta perché la mutazione cadeva su `party_label/1`:
aggiunto il controllo della storia e uccise tutte e due le varianti.

## Banco

Istanza `s062`, `fab-225`, `-dati sintetici-import`. Primo giro: catena verde (`GR_TOKEN:ok` con i
tre token nuovi), browser verde, HTTP 2 inattese, tutte e due su `pagina-cliente-foto[0]`: il
`photo_viewer` dello standalone mette il suo CSS in un `<style>` dentro la galleria, quindi il testo
visibile di `#public-repair-photos` e il conteggio di `gs-viewer-frame` contano anche le regole CSS
(il Go le ha in `app.css`). Artefatto del confronto: sostituiti con il numero delle miniature, il
contatore della seconda foto e il `src` della miniatura della foto 4.

Secondo giro, su un'istanza nuova (`banco giu`, `su`, `esegui`): **BANCO VERDE**, 52 scenari e 962
passi, 1511 differenze attese, **0 inattese**; **BANCO BROWSER VERDE** (12 scenari, 24 viste, 0
inattese). Istanza tolta con `banco giu`. Giro sul database vuoto non fatto (nessuna migrazione).

## Controlli

`scripts/check` verde; `scripts/parole-vietate --staged` verde (dopo `git add -A`, poi `git reset`).

## Per il manuale

- Capitolo del cliente: foto visibili secondo «Cliente ✓» / «Orefice ✓»; i due pulsanti del
  preventivo, la conferma del rifiuto con il motivo, «in corso di definizione» senza importo, una
  risposta sola; il riquadro «🤝 Preventivo» della scheda con «Revoca» e cosa succede allo stato. In
  `docs/manuale/da-scrivere.md`.

## Dubbi

1. **CLI-08 non fatta:** l'avviso Telegram al negozio dopo la risposta va con il canale Telegram
   (T4.6, o T4.7 se il canale arriva lì). La campanella che lo standalone avvisa non c'è (ADM-05): la
   risposta si vede dal filtro «notifiche» della home e nella scheda.
2. **Approvazione e riapertura fuori dal cambio di stato:** come lo standalone, l'approvazione
   (`preventivo` → `in_lavorazione`) e la revoca del rifiuto (`annullata` → `preventivo`) non scrivono
   `repair.status_changed` né `repair.reopened`: lo stato è nei dati di `quote.*`. Tutti e due i passi
   sono nel grafo del Go (il primo è un passaggio consentito, il secondo una riapertura), ma la
   riapertura non chiede il motivo che «Riapri busta» vuole. Da confermare.
3. **D-47 sulle foto pubbliche:** il tipo servito passa da `tipoServito` come la rotta del personale
   (solo `image/*`, mai SVG); lo standalone serve il tipo della riga com'è. Con le foto normalizzate
   non cambia niente; da dire se la D-47 va estesa per scritto anche a CLI-04.
4. **Banco senza il «sì» riuscito:** i dati sintetici non hanno una busta in preventivo con importo e
   senza risposta (aggiungerla sposta i contatori «Preventivi» di altri scenari); approvazione e
   rifiuto riusciti, la revoca e la risposta dopo una revoca li provano i test Go. Il banco confronta
   la pagina rifiutata, i doppi, gli opposti, il «non in preventivo», i 404 e il 403.
5. **Il flash della pagina** sta nel cookie di sessione della faccia pubblica (lo stesso formato
   firmato della faccia interna), come lo standalone lo tiene nella sua sessione.
6. **Riquadro della scheda «Confermato dal cliente»** anche quando ha risposto l'oreficeria (la storia
   dice «oreficeria»): testo dello standalone, lasciato così.

## Review

Revisore: sottoagente, 09/11/2026, sul lavoro non committato; confronto con lo standalone a
`go-baseline` (`quote_decision.ex`) e con CLI-04…09 e RIP-S-22 dell'inventario. **Nessun difetto
ALTO o MEDIO; nessuna correzione fatta.**

- **POST pubblici:** `rottaPubblica` mette il CSRF della faccia pubblica (cookie a parte, non toccato
  dal flash); il link si cerca come la pagina (impronta, scadenza, revoca, cestino); un solo UPDATE
  protetto, provato da 8 risposte insieme (una passa, una riga di storia, un audit). Motivo: bordi
  Unicode tolti, 300 grafemi, mai nell'audit né nel registro (i due `Error` non hanno campi).
- **Foto:** `fotoDi` filtra per `busta_id` del link, visibilità prima della variante, `tipoServito`
  (D-47), `no-store` e `no-referrer` anche sul 404, `nosniff` da `Pagina`; il registro non scrive mai
  il percorso (test statico). Il token negli `src` non esce: la pagina ha `no-referrer`.
- **Revoca dalla scheda:** rotta `personale` con CSRF interno, risposta mostrata nel campo `answer`.
  **Dubbio 2:** la riapertura `annullata` → `preventivo` è identica allo standalone (CASE nello
  stesso UPDATE, conservazione azzerata, nessun `status_changed`): resta una scelta di prodotto.
- **Flash:** cookie di sessione della faccia pubblica, solo testi fissi, tolto alla prima pagina.
- **BASSA — link revocato fra lettura e scrittura:** l'UPDATE non ripete le condizioni del token,
  quindi un link revocato o scaduto nello stesso istante può ancora rispondere. Lo standalone fa lo
  stesso; finestra di millisecondi. Da lasciare.
- **BASSA — flash su un'altra busta:** il cookie ha `Path=/`, quindi il messaggio si vede sulla
  prossima pagina `/r/` aperta, anche di un'altra busta. Testi generici, come lo standalone.
- **BASSA — revoca nel cestino:** il Go rifiuta la revoca su una busta cestinata, lo standalone no.
  È più prudente e la scheda di una busta cestinata non la offre.
- **Test:** i test letti provano quello che dicono (corsa, flash una volta, foto di un'altra busta,
  audit senza testo). `scripts/check` verde; `scripts/parole-vietate --staged` verde.
