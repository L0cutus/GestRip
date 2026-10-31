# 026 — Una vendita con foto e revisione nella catena del banco

- **Fonte:** richiesta di Stefano del 31/10/2026 (D-42 estesa): coprire nel banco sintetico le foto
  di vendita caricate dall'interfaccia e una revisione di vendita.
- **Voci:** nessuna (T3, banco).
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha rilanciato `scripts/check`, la catena e il banco.

## Cosa c'è

- **Passo `vendita`** della catena `-dati sintetici-import` (`tools/banco/vendita.go`), dopo il
  legacy import e prima della copia del database cifrato e dei media: un container dell'immagine
  dello standalone, senza rete, con le stesse protezioni dei passi vicini, avvia l'app come il
  legacy import (`LegacyImport.Release.quiet!`) e chiama le stesse funzioni della pagina delle
  vendite: `OnlineSales.create_draft/2`, `SalePhotos.attach/3` con un JPEG sintetico generato a ogni
  giro (nessun binario nel repo), `OnlineSales.issue_sale/2`. Il contesto d'audit ha la forma della
  pagina (`%{actor: <email del titolare sintetico>}`); il profilo del negozio è quello creato dal
  legacy import. Esito in una riga `GR_VENDITA:{…}` controllata (chiave della foto, conteggi di
  vendite, revisioni, sigilli, foto).
- **Controlli sul report dell'import Go:** righe lette e scritte di vendite, revisioni, sigilli e
  foto uguali ai conteggi dello standalone; esattamente tre avvisi `tipo/impronta_testo`
  (`historical_documents`, `sale_photos`, `sale_revision_photos`) per tutte le righe lette;
  famiglie media delle foto con `attesi == copiati == conteggi` e nessun mancante o hash diverso;
  la foto nei media con la sua impronta. `banco giu` toglie anche il container nuovo.

## Prove

- **TDD:** rosso misurato prima del codice (compilazione, poi 12 test sugli stub; per le correzioni
  della review 10 casi del report accettati, script con 4 contesti d'audit, ecc.). Dichiarati 5 casi
  aggiunti durante le mutazioni.
- **Mutazioni:** 33 + 16, tutte compilano: 48 uccise, 1 viva equivalente (conteggio dei file con
  la cartella radice in tutti e due i lati).
- **Dal vivo (`fab-219`):** il passo vendita riesce (vendita, foto, revisione, sigillo; media 6
  file, 703 byte) e l'import Go ha già i tre avvisi attesi, ma **si blocca** su
  `sale_revision_photos.inserted_at` salvato dallo standalone senza `Z`
  (`traduzione/istante`): bug dello standalone, domanda 2 in `docs/domande-aperte.md`, non corretto
  né copiato. Su una copia con la `Z` aggiunta l'import è riconciliato (54 righe, 6 file) e il
  banco accetta il report. Spento, nessun avanzo.
- `scripts/check` verde; con l'esito del banco 31 voci chiuse. **Banco senza opzione verde** (13
  scenari, 156 passi).

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** un ripiego che scriveva il profilo del negozio non girava mai (il legacy import lo crea
  sempre) e avrebbe nascosto una regressione: tolto, se manca `issue_sale` fa diventare rosso il
  banco; la 004 non diceva che la catena è rossa fino alla domanda 2: scritto.
- **BASSA:** test dello script più stretto (contesto d'audit, tre chiamate, ordine); sigilli e
  famiglie media controllati; un commento.
- **Lasciati:** montaggio di tutta la cartella dei segreti (come i passi vicini); determinismo del
  JPEG non provato da un test.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** vedi sotto.
