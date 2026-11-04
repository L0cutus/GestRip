# Studio — foto HEIC/HEIF nel Go (03/11/2026)

Richiesta di Stefano (03/11/2026, tramite il coordinatore): il Go deve accettare anche le foto HEIC/HEIF,
il formato in cui le salvano gli iPhone e molti Android. In produzione lo standalone ne ha rifiutata
una («Foto rifiutata.» in 0,3 s): la causa era un JPEG Ultra HDR, non un HEIC (sezione 0). Sola documentazione: la D-xx e il codice
vengono dopo l'ok.

Vincoli (PIANO §2.2, D-48): `gestrip` senza CGO (`CGO_ENABLED=0`, `-tags nodynamic`); le immagini si
lavorano in `gestrip image-worker`, un processo figlio con limiti di memoria (avvio + 1440 MiB), di
tempo e di CPU, al più 2 insieme; errori di risorse distinti dai rifiuti (D-48).

## 0. Aggiornamento (03/11/2026): la causa vera era un'altra

Il coordinatore ha provato su pc-fisso con `fab-220` e la foto vera, poi cancellata. Era un JPEG
**«Ultra HDR»** di un Oppo Find X9 Ultra (Android), non un HEIC:
- un segmento APP2 «MPF» e una gain map `hdrgm`, quindi due immagini nello stesso file;
- Pillow 9.4 lo apre come MPO con `n_frames` 2;
- `normalize_image.py` rifiuta i file con più fotogrammi;
- risultato: 422 «Foto rifiutata.».

Un HEIC invece dà 415 «Formato non supportato.».

- **Il Go faceva lo stesso:** `immaginiMPO` in `internal/imaging/formati.go` copia la regola di
  Pillow (slice 036), con i test in `normalizza_test.go` («MPO con 2 immagini» rifiutata). Quindi
  anche il Go rifiutava ogni foto Ultra HDR dei telefoni recenti.
- **Decisa: D-65** (Stefano, 03/11/2026). Nata come deviazione dalla regola «un solo fotogramma»
  di RIP-S-30; dal 04/11/2026 è un **allineamento**: lo standalone ha corretto la stessa cosa nella
  sua slice 222 (`9a40f1f`, immagine `fab-222`, pacchetto 2026.11.03-1), `go-baseline` è a
  `f80f191`. Fatta nella mini-slice **045b** (`docs/slices/045b-ultra-hdr.md`), come la 222:
  - un JPEG che Pillow apre come MPO (Ultra HDR, MPO di fotocamere 3D e iPhone) si accetta;
  - si decodifica solo l'immagine principale, la prima: `image/jpeg` si ferma da sé alla sua fine
    (provato da un test); anche la riduzione dei 50 MP vale come per un JPEG;
  - la gain map si ignora, e si salva il JPEG pulito come per le altre foto;
  - restano rifiutati PNG e WebP animati, e l'indice MPF rotto che Pillow non apre (voci corte);
  - la miniatura, come `thumbnail_image.py`, continua a rifiutare un MPO: non le arriva mai, perché
    la foto salvata è già un JPEG semplice.
  - Prove: generatore sintetico `sintetiche.UltraHDR` (JPEG principale con APP1 XMP `hdrgm`, APP2
    MPF con due voci, gain map in coda); scenario del banco `foto-ultra-hdr`: **201 in tutti e
    due**, senza deviazione. Stessi esiti di Pillow 9.4 di `fab-222` su 16 file sintetici.
- **Le foto Ultra HDR, dopo:** le foto salvate perdono l'HDR, come tutte le altre. Il profilo
  colore si toglie come per le altre (RIP-S-30), quindi i colori di un'immagine Display P3 possono
  uscire un po' spenti, uguali allo standalone.

L'HEIC resta valido, ma è meno urgente: il resto dello studio vale com'è.

## 1. Decodifica sul server

Le strade possibili:

- **`github.com/gen2brain/h265`**, codec HEVC/HEIC in Go puro:
  - licenza MIT; è un porting dei decoder Rust `rust_h265` e `oxideav-h265`;
  - dichiara la decodifica byte per byte uguale sulla suite di conformità JCT-VC HEVC v1;
  - 8-16 bit; `heic.Decode` registra il formato in `image`;
  - SIMD su amd64 e arm64, con il tag `noasm` per il Go puro.
  - Contro: progetto giovane (circa 27 stelle, nessuna release); alcune estensioni di gamma rare sono
    rifiutate.
  - Va nel binario senza CGO, quindi dentro l'image-worker con gli stessi limiti, senza niente di
    nuovo sull'apparecchio.
- **`github.com/gen2brain/heic`** (stesso autore della `gen2brain/webp` che usiamo già): con
  `nodynamic` usa un WASM ricavato dal crate Rust `heic`. Quel crate è **AGPL-3.0 o commerciale**.
  Scartata per la licenza, oltre al costo del WASM.
- **`jdeng/goheif`**: richiede CGO (libde265). Scartata.
- **Programma esterno `heif-dec`/`heif-convert` di libheif** (pacchetto `libheif-examples`; libheif
  e libde265 sono LGPL-3.0). L'image-worker lo chiamerebbe da figlio, con gli stessi limiti.
  - Contro: è codice C che legge file che arrivano da fuori, è un pacchetto in più da tenere
    aggiornato sull'apparecchio e c'è un passaggio in più su disco.
  - La versione in Ubuntu 26.04 è da controllare; su pc-fisso oggi è la 1.17.6.
  - Resta come ripiego.

Vale per tutte: HEVC può essere coperto da brevetti (lo dicono i README di h265 e di `heic`); per un
uso in negozio che decodifica soltanto, il rischio è basso, ma va scritto in `THIRD_PARTY_NOTICES`.

**Raccomandazione: `gen2brain/h265`, provata prima di adottarla.**
- Foto HEIC sintetiche, fatte da noi e non foto vere:
  - 8 e 10 bit;
  - a riquadri (grid), come quelle di iPhone e Samsung;
  - con rotazione e specchio del contenitore (`irot`/`imir`);
  - con EXIF e con immagine ausiliaria di profondità o alpha;
  - da 12 e da 48 megapixel.
- Fuzz sul decoder dentro l'image-worker.
- Tempo e memoria misurati anche sull'N150.
- Se non regge: ripiego su `heif-dec`.

## 2. Conversione nel browser

- **Safari su iPhone:** con `accept` che elenca solo JPG/PNG/WebP (come oggi in `nuova.html` e
  `scheda.html`) di solito converte già in JPEG prima dell'invio. Se togliamo il filtro o aggiungiamo
  `.heic`, manda l'HEIC com'è.
- **Chrome su Android:** in generale manda il file com'è. La foto rifiutata in produzione è
  probabilmente di questo tipo: ipotesi, non verificata.
- **Una libreria WASM nel browser** (libheif-js, heic2any) pesa 1-2 MB, è LGPL, è lenta sui telefoni
  economici e non copre l'«Invia» del caricamento JSON né altri client.
- **Non basta, e la raccomandazione è di non farla:** il server deve saper decodificare comunque.
  Lato browser basta aggiungere `.heic,.heif,image/heic,image/heif` ad `accept`.

## 3. Cosa si salva

- Si salva sempre in JPEG pulito come oggi (RIP-S-30): lato lungo al più 4096 px, qualità 90.
- I metadati si tolgono come per le altre foto: niente EXIF, niente GPS, niente ICC.
- L'orientamento nell'HEIC sta nel contenitore (`irot`/`imir`) e non solo nell'EXIF: va applicato
  prima di salvare. È un test esplicito.
- Il nome originale prende `.jpg`, come `IMG_0001.png` → `IMG_0001.jpg`.
- Restano i limiti di oggi: ingresso 25 MiB, 60 megapixel, tempo dell'image-worker. Una foto da
  48 megapixel di un iPhone recente sta sotto i 60.
- Le immagini con più fotogrammi (le «Live Photo» e le sequenze) danno solo l'immagine principale.

## 4. Tappa e costo

- **Dove:** una **slice piccola subito dopo la T4.4**, prima della T4.2. La T4.3 è chiusa e la T5.1
  (il sidecar dell'IA) non c'entra.
- **Cosa tocca:** `internal/imaging`, il riconoscimento del formato, gli `accept`, i testi
  («Formato non supportato.» resta per il resto) e il banco. Lo standalone rifiuta: la differenza si
  dichiara come deviazione, con uno scenario che manda un HEIC sintetico.
- **Costo:** una slice, più o meno come la 041; conta nel tetto delle 4 al giorno. Con il ripiego
  `heif-dec` serve anche il pacchetto nell'installer (T5.4).
- **Nel frattempo:** su iPhone Impostazioni → Fotocamera → Formati → «Più compatibile»; su Android
  togliere HEIF o «alta efficienza» nelle impostazioni della fotocamera.
