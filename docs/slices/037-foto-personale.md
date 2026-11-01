# 037 — Rotta delle foto del personale e cache delle miniature (T4.3)

- **Fonte:** T4.3 del PIANO; piano delle slice nella ricevuta della 036.
- **Voci:**
  - chiusa RIP-S-34 (cache delle miniature);
  - parziale RIP-S-33: rotta, audit e verifica del gettone ci sono, manca chi firma il gettone,
    cioè la galleria della home (T4.2, test saltato `TestGalleriaDellaHomeFirmaIlGettone`).
- **Correzione della 036:** allegare, cambiare visibilità e togliere foto su una busta nel
  cestino ora danno «non trovata», come lo standalone (`Repo.prepare_query` filtra il cestino
  anche in `Repo.get`; `Repair` è in `Trash.families`). Come lo standalone, il file normalizzato
  resta nell'archivio (lo toglie la pulizia della 041), senza righe né audit. La domanda aperta
  12 è tolta.
- **Inventario corretto:** il limite della cache è 20 000 file in produzione
  (`config/runtime.exs`), non 2000, che è il valore di sviluppo e test.
- **Lavoro:** implementazione e due giri di correzioni di un sottoagente, review e mutazioni di
  un sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **Rotta** `GET /repairs/{id}/photos/{foto}`, per il personale (`f.rotta`, riga in
  `tabellaPermessi`):
  - foto intera: tipo dai byte, `x-photo-variant: full`, `no-store`, `no-referrer`;
  - miniatura solo con esattamente `size=thumb` (vale l'ultima assegnazione, come Plug): WebP,
    `x-photo-variant: thumb`, `private, max-age=86400`; se non si fa, ripiega sulla foto intera;
  - 404 «Foto non disponibile» senza tipo né lunghezza per un'altra busta, una foto tolta o
    manomessa, una busta nel cestino, id malformati;
  - audit `repair.staff_photo_viewed` a ogni lettura, ok o denied, con ip e user agent; se l'audit
    non si scrive, 500 e niente byte;
  - gettone della galleria `g`: HMAC con sottochiave «galleria», 2 giorni, come `covered?/4`;
  - il service worker non la mette in cache, il registro non ha mai il percorso.
- **Cache delle miniature** (`internal/media/miniature.go`) in `<cartella del db>/miniature`:
  - cartella 0700, voce `<chiave>.webp` 0600 da un temporaneo rinominato, aperta senza seguire
    link;
  - voce guasta o non WebP rifatta, mai servita; mai più a lunga della sua foto, anche se la foto
    sparisce mentre si fa; la foto si legge con la verifica dell'impronta;
  - svuotata tutta oltre 20 000 file; cache non scrivibile = risposta senza tenerla;
  - `Pulisci` (sweep), `Prepara` dopo il caricamento, riscaldatore all'avvio di `serve` (pulizia,
    poi le foto più recenti prima), `Togli` per la 041;
  - la miniatura si fa sempre nel processo figlio (`imaging.Lavoratore`), e il figlio si ferma se
    il client se ne va.
- **Banco:**
  - passo `foto` della catena: `RepairPhotos.attach_many` di un JPEG inventato 1000×750 sulla
    1/26, esito `GR_FOTO` letto in modo stretto, 4 foto pretese nel report dell'import;
  - campi nuovi del confronto `impronta`, `lunghezza`, `dimensioni`;
  - scenari `foto` e `foto-dipendente`: byte uguali per le 3 foto della cattura e per la foto
    intera nuova, tipo, intestazioni e 400×300 per la miniatura (i byte no: tema H), 12 rifiuti,
    totale dell'audit.
- **Vet anche per Windows e macOS** (`GOOS=windows|darwin go vet -tags nodynamic`): i test con
  `syscall` di Linux sono in file `_linux_test.go`. Resta un solo errore su Windows, in
  `tools/banco/catena.go` (`O_NOFOLLOW`), che c'era già: il banco gira solo su Linux.

## Differenze, dichiarate

- La cartella si chiama `miniature` invece di `thumbnails`; le voci sono 0600 invece di 0644, e
  se ne legge al più 4 MiB.
- Il gettone della galleria è un HMAC invece di `Phoenix.Token`: lo firmerà solo il Go.
- Un user agent vuoto va nell'audit come NULL invece di `""`.
- Le foto piccole (fino a 2048 byte) hanno `content-length` nel Go; le foto vere partono a pezzi
  come nello standalone.
- Il 404 è mandato con le intestazioni prima del corpo, perché Caddy non aggiunga un tipo
  (misurato sul banco). Dietro `tailscale serve`, in produzione, non è misurato.
- Con più di 20 000 foto il riscaldatore all'avvio svuota la cache più volte e lascia in cache le
  foto più vecchie, come lo standalone.
- Una pagina con molte miniature non in cache fa partire un figlio per ognuna, come lo
  standalone (domanda aperta 13).
- Se i byte non sono un'immagine riconosciuta, il tipo viene dalla riga, come lo standalone
  (domanda aperta 14).

## Prove

- **TDD:** rosso misurato prima del codice: 12 test dei media, 13 delle pagine, 5 del dominio, 1
  del riscaldatore, 2 di `serve`, 1 dei campi del banco, e il cestino nel giro delle correzioni.
  Nel giro della review il codice era già giusto: il rosso è misurato sulle 11 mutazioni della
  review, tutte uccise dai test nuovi.
- **Mutazioni:** review 72, ne compilano 68, uccise 50. Delle 18 vive, 7 sono equivalenti
  (difese doppie, tempo di `hmac.Equal`, prefisso di dominio, id non positivi); le 11 per test
  mancante sono coperte e uccise.
- **Misurato:** una miniatura di una foto normalizzata 4096×3072 costa circa 62 MB di RSS e 0,4 s
  al figlio.
- **Banco** (`fab-219`), rilanciato dalla sessione principale: verde con `-dati sintetici-import`
  e su database vuoto, 20 scenari e 287 passi, «AVANZI: nessuno». Il giro delle correzioni
  della review tocca solo test e documenti: il binario ricompilato è identico byte per byte a
  quello del banco. Prova negativa (`size` confrontato senza maiuscole): 9 differenze, banco
  rosso.
- `scripts/check` verde; con l'esito del banco 49 voci chiuse, 21 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA.
- **MEDIA:** nessun test provava che la miniatura legge la foto con la verifica dell'impronta →
  test con una foto sostituita da un'altra immagine valida, nel dominio e sulla rotta.
- **BASSA:**
  - foto sparita durante la miniatura, tipo dai byte, sette difese della cache, report dell'import
    delle foto → test aggiunti;
  - frase sulla `content-length` → corretta;
  - vet per Windows → test spostati;
  - figli senza tetto → domanda aperta 13;
  - riscaldatore oltre 20 000 foto → dichiarato sopra;
  - ripiego sul tipo della riga → domanda aperta 14.

## Domande aperte nuove

In `docs/domande-aperte.md`: 13 (quanti processi delle immagini insieme) e 14 (tipo servito per
una foto che non si riconosce); la 12 è tolta.
