# 039 — `POST /repairs/{id}/photos`, caricamento della coda delle foto (T4.3)

- **Fonte:** T4.3 del PIANO; piano delle slice nella ricevuta della 036.
- **Voci:** RIP-S-35 chiusa con l'esito del banco (scenario `coda-foto`); senza banco resta parziale.
  La coda che la usa (`photo_queue.js`) arriva nella 040.
- **Decisioni di Stefano (02/11/2026):** D-49 (nome del file ripulito anche sulla scheda, domanda 15
  della 038), D-50 e D-51 (domande 16 e 17): un guasto del server risponde 500 «Foto non salvata.»,
  «Foto rifiutata.» resta per le foto non valide. Conta per l'operatore: `photo_queue.js` tratta il
  422 come permanente (scarta la foto) e il 500 come da riprovare.
- **Lavoro:** implementazione e due giri di correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **Rotta** `POST /repairs/{id}/photos`, `personale`, con `f.rotta` e `csrf.Modulo`: il corpo è
  l'immagine e non si legge per il CSRF; il token arriva da `x-csrf-token`.
- **Ordine come il controller** (`RepairPhotoUploadController`), senza leggere il corpo prima: id e
  busta (404 «Busta non trovata.», anche nel cestino), tipo (415 «Formato non supportato.»),
  lunghezza dichiarata (413). Poi il corpo in `caricamenti/richiesta-*/` (0700, file 0600 con
  `O_EXCL`, tolti sempre), al più 25 MiB contati leggendo, con la scadenza di lettura spostata a ogni
  MiB, 30 s per pezzo (`read_body(length: 1 MiB, read_timeout: 30_000)`).
- **Esiti dopo `AllegaFoto`:**

  | Errore | Codice | Testo |
  |---|---|---|
  | nessuno | 201 | `{"ok":true,"photo_id":N}` |
  | sessione revocata | 401 | «Accesso scaduto.» |
  | risorse del processo figlio (D-48) | 503 | `MsgFotoNonElaborata` |
  | immagine non valida, caricamento non valido, troppe foto, busta cestinata nel frattempo | 422 | «Foto rifiutata.» |
  | ogni guasto: database, audit, archivio che non scrive, file del caricamento che non si rilegge, contesto annullato (D-50, D-51) | 500 | «Foto non salvata.», causa nel registro |

  Prima del salvataggio: 400 «Invio interrotto.» per un corpo che non arriva, 500 «Foto non salvata.»
  se il file temporaneo non si crea o non si scrive (D-50). JSON con le chiavi nell'ordine di Jason,
  `application/json; charset=utf-8`.
- **Nome del file:** `x-file-name` decodificato come `URI.decode` (il «+» resta), poi
  `buste.PulisciNomeFile`, «foto» se vuoto, estensione del tipo se manca.
- **Dominio:** `buste.EsisteBusta` (`get_repair`, senza audit); `buste.ErrFileCaricamento` per il file
  del caricamento che non si rilegge, distinto da `imaging.ErrImmagine`. Sulla scheda (038) il
  messaggio resta «Foto rifiutata o non salvata.» per tutti e due.
- **Scadenza per pezzo** condivisa con la 038 (`allungaLettura` in `internal/web/modulo.go`), con la
  prima scadenza prima del primo byte.
- **Banco:** `corpo_base64` (corpo binario), `{csrf}` nelle intestazioni, campo `forma-json` (JSON con
  i numeri al posto di `#`, per il 201). Scenario `coda-foto`: 403 senza cookie o token, 404, 415, 422,
  201 del titolare e del dipendente sulla 20/26, poi la galleria con le foto 5 e 6.

## Differenze, dichiarate

- Client che si scollega durante la normalizzazione: il Go non salva (500, niente su disco né
  righe); lo standalone completa il salvataggio, e al nuovo tentativo della coda può nascere un
  doppione. Se il client se ne va dopo la normalizzazione e prima della transazione, il file
  normalizzato resta come orfano senza righe e lo toglie la pulizia (041), come per la revoca.
- Corpo `multipart/form-data` o JSON: lo standalone lo farebbe leggere a `Plug.Parsers` prima del
  CSRF, il Go risponde 415 senza leggerlo. La coda non lo manda (ambito D-30).
- `Accept` senza HTML: lo standalone darebbe 406, il Go no (come nel resto del Go; la coda manda
  `*/*`).
- Il tempo per pezzo (1 MiB in 30 s) viene dalla lettura del controller; i sorgenti di Bandit non
  sono verificati.
- Il banco non vede il nome salvato (non compare in nessuna pagina): lo provano i test Go. Il 413 e
  il 400 di un invio troncato non sono nel banco, solo nei test Go.

## Prove

- **TDD:** 18 test rossi su 20 sullo stub (due provano la catena che c'era già); nei giri delle
  correzioni 8 casi su 11 rossi più il client scollegato, poi 2 rossi nei ritocchi. I test di cose già
  giuste hanno come rosso la mutazione uccisa.
- **Mutazioni:** implementazione 28 + 12 + 2, tutte uccise. Review 46 che compilano: 36 uccise, 5
  equivalenti, 1 non necessaria (errore di `Close`), 4 per test mancanti, coperte nel giro delle
  correzioni e uccise.
- **Stabilità:** i test dei pezzi lenti e della prima scadenza 10 su 10 sotto `nice 19`.
- **Banco** (`fab-220`), rilanciato dalla sessione principale: **verde** al primo giro e dopo le
  correzioni, con `-dati sintetici-import` e su database vuoto, 24 scenari e 340 passi,
  «AVANZI: nessuno».

## Review indipendente

Nessuna ALTA.
- **MEDIA:** D-51 non applicata (guasti come 422) → 500 per ogni guasto, con test e registro; anche
  il file del caricamento che non si rilegge (prima `ErrImmagine`).
- **BASSA:** valori di produzione dei tempi, prima scadenza prima del primo byte, `photo_id` della
  seconda foto, margini del test dei pezzi lenti → test aggiunti; client scollegato e nome non nel
  banco → dichiarati sopra.
