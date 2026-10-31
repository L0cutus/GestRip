# 028 — Il registro d'emergenza nella catena del banco

- **Fonte:** decisione di Stefano del 31/10/2026 sull'elenco delle colonne con istanti senza zona
  (D-42): misurare l'avviso su tutte le colonne con il registro d'emergenza fatto con le funzioni
  dello standalone.
- **Voci:** nessuna (T3, banco).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; la sessione principale ha rilanciato `scripts/check`, la catena e il banco.

## Cosa c'è

- **Passo `emergenza`** (`tools/banco/emergenza.go`), dopo la vendita e prima della copia, con le
  stesse protezioni dei passi vicini (senza rete, `--rm`). Con le funzioni della pagina:
  - `Workshop.update_repair/4` dà un QR alla busta 2/26, che è «pronta». Il contesto è quello della scheda:
    `actor` e `session_user`. Le buste della cattura sintetica non hanno QR;
  - `EmergencyRegister.open_event/3` apre l'evento, con `notify_owner: false` come per il titolare;
  - `create_entry/5` scrive una riga libera, e `reverse_entry/5` la storna;
  - `scan/4` scansiona la busta;
  - `close_event/2` chiude l'evento.

  L'esito sta in una riga `GR_EMERGENZA:{…}`, letta in modo stretto (`DisallowUnknownFields`, una sola riga).
  Le righe del registro si rileggono con `entries/1`. Stato e conservazione si leggono dalla busta riletta
  dopo la scansione. `banco giu` toglie anche il container nuovo.
- **Controlli:** il banco pretende l'avviso `traduzione/istante_senza_zona` su tutte e sei le colonne
  della D-42, ciascuna con le righe esatte, senza `Id`. Un avviso su un'altra colonna, un doppione o
  zero righe fanno diventare rosso il banco.
- La scansione accetta una busta in qualunque stato non cestinato e non addebitato: è scritto così
  nella EME-08, quindi non è una domanda nuova.

## Prove

- **TDD:** rosso misurato prima del codice: prima la compilazione, poi 11 test con gli stub. Le correzioni
  della review sono solo nei test, e il loro rosso è misurato con 6 mutazioni.
- **Mutazioni:** 38 + 6. Delle 43 che compilano, 30 sono uccise. Le 13 vive sono equivalenti:
  - i controlli doppi in `scritto()`, che `leggiEsitoEmergenza` garantisce già;
  - gli id passati come interi, che `Id.positive` accetta;
  - i conteggi che in ogni catena possibile valgono 1.
- **Dal vivo (`fab-219`):** il passo riesce. L'esito è: busta 2 consegnata con la conservazione avviata,
  2 righe, evento chiuso. L'import Go è **riconciliato**, con 9 avvisi:
  - 3 `tipo/impronta_testo`;
  - `traduzione/istante_senza_zona` su `emergency_entries.inserted_at`=2,
    `emergency_events.closed_at`=1, `updated_at`=1, `repairs.retention_started_at`=1,
    `updated_at`=1, `sale_revision_photos.inserted_at`=1.

  Gli scenari sulla catena danno solo la differenza nota `utenti[2]` (T3b).
- `scripts/check` verde; con l'esito del banco le voci chiuse sono 31. **Banco senza opzione verde**: 13
  scenari, 156 passi. Nessun avanzo.

## Review indipendente

Nessuna ALTA e nessuna MEDIA. Le BASSE corrette nei test:
- il QR della scheda è lo stesso della scansione;
- l'esito si legge dalla busta riletta;
- le righe vengono da `entries/1`;
- `session_user` è nel contesto della scheda;
- c'è un caso con la riga 0.

Lasciati:
- i controlli doppi;
- la busta scansionata senza una riga che la addebiti. Nella pagina si può fare lo stesso (scansiono e non
  salvo), e le colonne coperte non cambiano.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** `9051f28`, dopo `e5ff775`.
