# 027 — Istanti salvati senza zona dallo standalone (D-42)

- **Fonte:** risposta di Stefano del 31/10/2026 alla domanda 2 della slice 026 e, lo stesso giorno,
  l'elenco completo delle colonne dalla verifica della sessione dello standalone (sola lettura,
  database sintetico). Registrata nella D-42 dell'inventario (stessa causa: un `DateTime` passato a
  `Repo.query`/`SQL.query` con exqlite 0.40 si salva senza zona).
- **Voci:** nessuna nuova; rinforza OPS-01 (import) e la D-42.
- **Lavoro:** implementazione in due parti e correzioni di un sottoagente, review di un sottoagente
  diverso; la sessione principale ha rilanciato `scripts/check`, la catena, il banco e le mutazioni.

## Cosa c'è

- **`internal/importer/importa.go`:** nell'insieme `istantiSenzaZona` le sei colonne decise:
  `sale_revision_photos.inserted_at`, `emergency_entries.inserted_at`, `emergency_events.closed_at`
  e `updated_at`, `repairs.retention_started_at` e `updated_at`. Solo lì la forma
  `AAAA-MM-GGThh:mm:ss` con al più sei decimali e senza zona si legge come UTC e si scrive con la
  `Z`; ogni altra forma o colonna resta `traduzione/istante` e blocca. Il conteggio è per colonna
  della sorgente su tutto l'import (anche per una tabella divisa come `settings`), conta solo le righe
  che il Go ha preso, e gli avvisi `traduzione/istante_senza_zona` (tabella, colonna, righe, mai il
  valore né l'id) escono una volta, ordinati, prima del verdetto. `impronteTesto` (D-42) ha ora la
  stessa forma, un insieme di coppie tabella/colonna, senza cambiare comportamento: allargare la
  regola è aggiungere una riga.
- **`tools/banco`:** la catena vuole esattamente un avviso su `sale_revision_photos.inserted_at` con
  le righe della vendita, accetta e riporta le altre colonne decise, fallisce su ogni altra colonna,
  su un doppione, su righe mancanti o zero, su un `Id`. Il riassunto stampa gli avvisi per colonna.
- `docs/guida-import.md` e `docs/slices/004-banco.md` aggiornati.

## Prove

- **TDD:** rosso misurato prima del codice in tutte e due le parti (righe senza zona bloccate,
  due avvisi per `settings`, elenco incompleto, compilazione del banco). Già verdi prima, come
  guardie: i casi che devono bloccare (21) e la riga scartata da un vincolo che non si conta.
- **Mutazioni:** 22 provate, 18 compilano: 16 uccise (una sull'ordine degli avvisi solo a volte,
  perché con due avvisi alla pari l'ordine è quello della mappa), 2 vive equivalenti (l'ancora `^`
  della forma, che `time.Parse` rifiuta comunque; il filtro `n > 0`, sempre vero).
- **Dal vivo (`fab-219`):** la catena `-dati sintetici-import` arriva a un import **riconciliato**
  (54 righe, 6 file, 703 byte), tre avvisi `tipo/impronta_testo` e un avviso
  `traduzione/istante_senza_zona` su `sale_revision_photos.inserted_at` (1 riga); gli scenari sulla
  catena danno solo la differenza nota `utenti[2]` (T3b). Le altre colonne si misureranno con il
  registro d'emergenza nella catena (slice 028).
- `scripts/check` verde; con l'esito del banco 31 voci chiuse. **Banco senza opzione verde** (13
  scenari, 156 passi). Nessun avanzo.

## Review indipendente

Fatta sulla prima parte; nessuna ALTA. Corretti nella seconda parte:
- **MEDIA:** una colonna di una tabella divisa (`settings`) avrebbe dato due avvisi: conteggio per
  colonna della sorgente su tutto l'import; nessun test sulla riga scartata non contata: aggiunto
  (uccide la mutazione che toglie il `continue`).
- **BASSA:** test di coerenza delle colonne speciali con la mappa (traduzione `Istante`, BLOB nel
  Go); il banco vuole l'avviso senza `Id`.
- **Lasciati, solo su un report già bloccato:** una riga scritta e poi bloccata da una chiave
  esterna orfana si conta lo stesso; `tipo/impronta_testo` conta le righe lette e
  `traduzione/istante_senza_zona` quelle scritte. In un import riconciliato i numeri coincidono.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** `480b6b1`, dopo `6a1aa55`.
