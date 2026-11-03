# 041 — Pulizia dei media ogni 60 s (T4.3, ultima)

- **Fonte:** T4.3 del PIANO; piano delle slice nella ricevuta della 036.
- **Voce:** RIP-S-57 parziale. Restano le esportazioni vecchie, la conservazione delle foto (T4.11)
  e la scadenza dei dettati (T4.10): tre test saltati in `internal/media/pulizia_resto_test.go`.
- **Lavoro:** implementazione e due ritocchi di un sottoagente, review e mutazioni di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **`internal/media/pulizia.go`**, come `MediaCleanup` e `delete_pending` dello standalone:
  - `Riferimenti`: l'elenco unico delle colonne che nominano un oggetto dell'archivio, le 7 di
    `referenced?/1` (foto delle buste, firma della busta, eventi della firma, foto delle vendite e
    delle revisioni, documenti storici, immagini del negozio). Sono già nello schema Go e l'import le
    riempie, quindi sono protette tutte da subito. `TestRiferimentiCopronoLoSchema` fallisce se una
    colonna con «chiave» nel nome non sta nell'elenco o fra le escluse motivate, e confronta l'elenco
    con le colonne media dell'import;
  - `Accoda` (`queue_deletions/1`), da chiamare nella stessa transazione: oggi nessuno la chiama in
    produzione. Nello standalone la usano solo il cestino definitivo (dopo un backup verificato), il
    logo e la conservazione delle foto; `RepairPhotos.remove/3` no, quindi `RimuoviFoto` resta com'è
    (il file lo raccoglie la pulizia degli orfani dopo un backup verificato, RIP-S-55, T4.11);
  - il giro, subito all'avvio di `serve` e poi ogni 60 s, con il lucchetto dell'archivio (lo stesso
    di `AllegaFoto`): toglie i temporanei `.upload-` abbandonati; poi, se il database non è bloccato
    dopo un import, tratta la coda una chiave per volta, ciascuna in una transazione immediata (usata?
    → resta; altrimenti file, fsync della cartella, miniatura, riga, commit). Alla prima chiave con
    errore si ferma e il resto resta in coda.
- **Blocco dopo l'import (domanda 20):** finché un database importato non ha un backup verificato
  la coda non si tocca, con lo stesso controllo della conservazione del registro (ADM-17), ora in
  `db.BloccatoDopoImport`; una riga nel registro una volta per avvio. I temporanei si tolgono comunque.
- Registro: solo la categoria della causa, mai chiavi o percorsi; il giro non ferma mai il servizio;
  `serve` lo aspetta allo stop prima di chiudere il database.
- Regole per le slice future: `docs/SVILUPPO.md`, «Archivio media e pulizia».

## Differenze, dichiarate

- `Accoda` rifiuta una chiave malformata (lo standalone la mette in coda e poi il giro si blocca lì).
  Una chiave malformata arrivata con l'import blocca il giro come nello standalone (in pratica l'import
  le rifiuta già).
- Coda ferma dopo un import fino al backup verificato (domanda 20).
- Le cartelle `richiesta-*` dei caricamenti si puliscono solo all'avvio: il gestore le toglie sempre e
  restano solo dopo un crash, che porta un riavvio.
- `os.Remove` toglie anche una cartella vuota con il nome della chiave (`File.rm` no): irrilevante.

## Note per le slice future

- Una chiave il cui file non si riesce a togliere ferma la coda a ogni giro, con un avviso ogni 60 s,
  come lo standalone. Da rivedere quando il blocco dopo l'import cadrà (saltare la chiave o avvisare
  una volta sola).
- Il lucchetto dell'archivio vale solo dentro il processo: un comando da riga di comando che scrive
  nell'archivio mentre `serve` gira (esportazione, ripristino) avrà bisogno di un lucchetto fra
  processi.
- Finché manca il registro dei backup verificati, su un database importato la coda non si svuota mai.

## Prove

- **TDD:** rosso su uno stub per i 15 test della pulizia, la guardia sullo schema e i due di `serve`;
  per il blocco dopo l'import 2 test rossi prima del codice. Alcuni test sono nati per uccidere
  mutazioni: il loro rosso è la mutazione.
- **Mutazioni:** implementazione 30, tutte uccise. Review 32: 30 uccise, 2 sopravvissute (archivio
  diverso e pulizia senza miniature in `serve`), coperte poi da due test.
- **Banco** (`fab-220`) completo con `-dati sintetici-import`, rilanciato dalla sessione principale:
  verde, «AVANZI: nessuno». Un confronto diretto non c'è: è un meccanismo interno.

## Review indipendente

Nessuna ALTA né MEDIA. Nessuno scenario in cui la pulizia cancelli un file che serve: colonne uguali
a `referenced?/1`, un solo scrittore dell'archivio sotto lucchetto, transazione immediata per chiave,
import che non corre con `serve`. BASSA: due test mancanti (aggiunti), le note sopra.
