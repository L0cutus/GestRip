# 036 — Nucleo dei media (T4.3, prima slice)

- **Fonte:** T4.3 del PIANO (tema H), via di Stefano del 31/10/2026 (T4.1 → T4.3 → T4.4 → T4.2).
- **Voci:** nessuna chiusa. Parte di dominio di RIP-S-29, 30, 31, 32, 33 (test di pagina saltati
  in `internal/buste/pagine_test.go`); le pagine arrivano nelle slice 037–041.
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Piano della T4.3

| Slice | Contenuto | Voci | Dipende da |
| --- | --- | --- | --- |
| 036 | nucleo senza pagine (questa): archivio SHA-256, normalizzazione e miniature in Go, processo figlio `gestrip image-worker` con i limiti, dominio delle foto | RIP-S-29…33, dominio | — |
| 037 | rotta delle foto del personale `GET /repairs/{id}/photos/{pid}` (`?size=thumb`, `x-photo-variant`, cache HTTP, 404 «Foto non disponibile» con audit), letture con audit, cache delle miniature (0700, oltre 2000 file svuotata, `sweep`), gettone della galleria | RIP-S-33, 34 | 036; galleria della home in T4.2 |
| 038 | galleria della scheda con visualizzatore senza JavaScript; caricamento dalla scheda (multipart, `x-csrf-token`, al più 2 foto); visibilità ed «Elimina foto» con conferma | RIP-S-28…32 | 036, 037; frecce, swipe e messaggio che sparisce: JavaScript |
| 039 | `POST /repairs/{id}/photos` in JSON (201/404/415/413/400/401/422, `x-file-name` ripulito) | RIP-S-35 | 036 |
| 040 | coda delle foto della nuova busta (`photo_queue.js`) con il **livello browser del banco** | RIP-H-40 | 039; prima funzione con JavaScript |
| 041 | pulizia ogni 60 s: `cancellazioni_media` dei file che nessuna riga usa, temporanei `.upload-` abbandonati, la miniatura con la sua foto | RIP-S-57 (parziale) | 036 |

Fuori dalla T4.3:
- conservazione, compressione a 1280 px, orfani dopo un backup verificato → T4.11;
- foto pubbliche → T4.5;
- galleria della home → T4.2;
- foto delle vendite → T4.12.

Da rispettare nella 038 e dopo: in produzione `imaging.Normalizza` e `Miniatura` si chiamano
solo attraverso `Lavoratore` (processo figlio); è scritto nei loro commenti.

## Cosa c'è

- **`internal/media`**: archivio per impronta SHA-256 come `store` e `read_object` dello
  standalone:
  - cartella 0700, che non può essere un link;
  - temporaneo `.upload-…` con O_EXCL a 0600, poi fsync, 0400, link (EEXIST va bene), fsync
    della cartella, rilettura con verifica dell'impronta;
  - lettura con chiave a 64 cifre esadecimali, file regolare senza seguire link, da 1 byte a
    16 MiB, impronta uguale al nome.

  L'import (slice 023) ora usa le stesse costanti e la stessa apertura, senza duplicati e con il
  comportamento invariato.
- **`internal/imaging`**: normalizzazione e miniature in Go puro, che copiano gli script Pillow
  9.4 dello standalone:
  - ingresso 25 MiB, oltre 60 MP rifiutato;
  - un solo fotogramma, con le regole di APNG, MPO (ImageDataFormat, come `_getmp`) e WebP
    animato;
  - draft dei JPEG oltre 4096 px, dimensioni con il `round_aspect` di `thumbnail`;
  - orientamento EXIF da JPEG, PNG e WebP, con i tipi del tag come `TiffImagePlugin`;
  - limiti del testo PNG (1 MiB per chunk, 64 MiB in tutto);
  - alfa tolto, uscita JPEG q90; miniatura WebP 400 px q70, metodo 4.

  Il ridimensionamento è un CatmullRom separabile scritto a mano: `x/image/draw` per una foto da
  60 MP alloca circa 1 GB.
- **`gestrip image-worker`**: processo figlio con ambiente vuoto, `Dir=/`, niente stderr,
  `Pdeathsig` SIGKILL (con `LockOSThread`), ucciso dopo 10 s. Si dà i limiti prima di leggere la
  foto: CPU 8 s, core 0, memoria dei dati quella dell'avvio più 1440 MiB (domanda aperta 10).
- **`internal/buste/foto.go`**:
  - `AllegaFoto` come `attach_many`;
  - visibilità iniziale e per canale, con un solo punto che decide (`Visibile`);
  - `ImpostaVisibilita`, `RimuoviFoto` (il file resta, lo toglie la pulizia della 041);
  - `NomeSalvato` con basename, extname e rootname di Elixir, misurati nell'immagine;
  - audit `repair.photo_uploaded`, `repair.photo_visibility_changed`, `repair.photo_removed`.

  Come `Repo.get` dello standalone, il dominio delle foto lavora anche su una busta nel cestino
  (domanda aperta 12).
- **Binario statico con `-tags nodynamic`**: `gen2brain/webp` porta `purego`, che senza il tag
  rende il binario dinamico e userebbe la libwebp di sistema. Senza il tag la compilazione si
  ferma (`internal/imaging/senza_nodynamic.go`); `TestBinarioStatico` controlla che il binario non
  abbia `PT_INTERP` né `PT_DYNAMIC`. Il tag è in `scripts/check`, nella CI, nel README, in
  `docs/SVILUPPO.md`, nei comandi del banco e della prova sui dati veri.
- **Dipendenze nuove**, senza rete (erano nella cache del prototipo del tema H):
  - `github.com/gen2brain/webp` v0.6.4 (MIT, con libwebp BSD-3);
  - `golang.org/x/image` v0.47.0 (BSD-3);
  - `github.com/ebitengine/purego` v0.10.1 (Apache-2.0), solo in `go.sum`: con il tag non si
    compila.

  **Da fare alla T5 (installer):** un file di avvisi di terze parti nel pacchetto, perché libwebp
  (BSD-3) lo chiede nelle distribuzioni binarie; manca già anche per sqlite e x/*.

## Differenze, dichiarate

- Pixel e byte delle foto normalizzate diversi da Pillow (CatmullRom invece di Lanczos o della
  riduzione del DCT, niente ottimizzazione di Huffman), come previsto dal tema H; le dimensioni
  sono uguali.
- Memoria del figlio: limite sui dati invece che sullo spazio d'indirizzi, con un valore più alto
  (domanda aperta 10). Il Go accetta qualche foto che Pillow rifiuta solo per la sua memoria.
- Casi rari diversi da Pillow (domanda aperta 11): WebP animato con un solo fotogramma, PNG a 16
  bit molto grandi, JPEG aritmetici o a 12 bit, BigTIFF nell'EXIF.
- Errori più fini nel dominio (`ErrBustaNonTrovata`, `ErrFotoNonTrovata`, `ErrSenzaArchivio`)
  dove lo standalone ha un solo `:not_found`; la pagina darà lo stesso messaggio.
- Una foto con un blocco EXIF guasto è rifiutata del tutto, come `getexif` dello standalone.
  Copiato.

## Prove

- **TDD:** rosso misurato prima del codice: 23 test di imaging, 6 dell'archivio, 11 del dominio
  delle foto. Alcuni test (8 forme di orientamento, EXIF guasti, foto grandi nei limiti, casi MPO
  allargati) sono stati scritti dopo il codice. Nel giro delle correzioni il rosso è misurato sul
  codice di prima (MPO, tipi dell'orientamento, testo PNG, ANMF, foto pesanti); i test che
  servivano solo a uccidere le mutazioni passavano già.
- Ogni caso delle correzioni è verificato su **Pillow 9.4** dell'immagine `fab-219`, sui
  sorgenti e con `normalize_image.py` in un container senza rete.
- **Misurato:** il figlio normalizza un JPEG 8944×6708 in 1,15 s; i casi più pesanti stanno sotto
  2,4 s di CPU. Le foto grandi dei test si generano dentro i test, niente file grandi nel repo.
- **Mutazioni:** review 75, ne compilano 72, uccise 41. Delle 31 vive, 15 sono equivalenti; le 16
  per test mancante sono coperte nel giro delle correzioni. Pdeathsig (il figlio che muore con
  il padre) è dichiarato e non provato: servirebbe un processo nonno, fragile.
- **Banco** (`fab-219`), rilanciato dalla sessione principale con `-tags nodynamic`: verde con
  `-dati sintetici-import` e su database vuoto, 18 scenari e 252 passi, «AVANZI: nessuno». Le
  correzioni della review toccano solo `internal/imaging` e i test, fuori da pagine e catena.
- `scripts/check` verde; con l'esito del banco 48 voci chiuse, 21 parziali, 0 rosse.

## Review indipendente

- **ALTA:** la CI di GitHub compilava senza `-tags nodynamic` e sarebbe diventata rossa al primo
  push → tag in ogni passo della CI e nei documenti.
- **MEDIA:**
  - MPO letto sul campo sbagliato (MPType invece di ImageDataFormat), con un test sbagliato →
    corretto come Pillow;
  - foto che lo standalone accetta rifiutate per memoria → limite misurato, domanda 10;
  - nessun test che il vero figlio si dia i limiti → test aggiunto.
- **BASSA:**
  - tipi del tag d'orientamento, limite totale del testo PNG, Pdeathsig e thread → corretti;
  - test vuoto della libreria WebP → reso significativo;
  - 16 mutazioni vive per test mancante → test aggiunti;
  - uso diretto di `Normalizza` → commento per la 038;
  - temporanei `.upload-` dopo un crash → nella 041;
  - avvisi delle licenze → alla T5.

## Domande aperte nuove

In `docs/domande-aperte.md`, domande 10–12: memoria del figlio, casi rari di immagini, foto su
una busta nel cestino.
