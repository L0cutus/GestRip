# 045b — D-65, foto JPEG Ultra HDR (allineamento alla slice 222 dello standalone)

- **Fonte:** slice 222 dello standalone (`9a40f1f`, «a JPEG Ultra HDR from a gallery edit is
  accepted, main picture only», più `f80f191` con l'immagine `fab-222` e il pacchetto
  2026.11.03-1). Il tag `go-baseline` è stato spostato a `f80f191` il 04/11/2026 dalla sessione
  principale, autorizzato da Stefano (prima `12d6e7f`, slice 220). Letti `git show 9a40f1f`
  (`priv/normalize_image.py`, `media_store_normalize_test.exs`, `test/support/photo_fixture.ex`) e
  la ricevuta `docs/plans/2026-10-27-gestrip-debiti-parita/slices/222-foto-ultra-hdr.md`; ripresa
  `docs/handoff/2026-11-04-fine-045.md`, studio `docs/studio-heic.md` sezione 0.
- **Voci:** RIP-S-30 (normalizzazione delle foto). D-65 (Stefano, 03/11/2026) nasceva come
  deviazione; con la 222 è un **allineamento**: il Go fa quello che fa lo standalone.
- **Lavoro:** implementazione, misure su `fab-222`, banco e documenti di un sottoagente. Commit,
  inventario privato e registro di allineamento li fa la sessione principale.

## Cosa c'è

### La regola (come la 222)

`normalize_image.py` dalla 222: `if source.format != "MPO" and n_frames != 1: rifiuta`, e il draft
vale per `source.format in ("JPEG", "MPO")`. Nel Go (`internal/imaging`):

- `apri(in, maxPixel, conMPO)`: con `conMPO` un JPEG con più immagini MPF (quello che Pillow 9.4 apre
  come MPO, deciso da `immaginiMPO`, che resta la copia di `jpeg_factory`/`_getmp`) non si rifiuta
  più; dimensioni e limite dei 60 MP sono quelli della prima immagine (`jpeg.DecodeConfig` legge il
  primo SOF). PNG e WebP con più fotogrammi restano rifiutati sempre.
- `Normalizza` chiama `apri(…, true)`: si decodifica la sola immagine principale, perché
  `image/jpeg` si ferma alla prima EOI e la gain map in coda non si legge. Il draft (`dopoIlDraft`)
  vale già per l'MPO, che nel Go è `formatoJPEG`: un 50 MP Ultra HDR esce 4080×3060.
- `Miniatura` chiama `apri(…, false)`: `thumbnail_image.py` la 222 non l'ha toccato e rifiuta ancora
  ogni file con più fotogrammi, MPO compreso. Non cambia niente nell'uso: la miniatura si fa dalla
  foto salvata, che è un JPEG semplice.
- Restano rifiutati: le voci MPF più corte del numero delle immagini (Pillow non apre il file:
  `struct.error`), e tutto ciò che era rifiutato prima.
- Effetto collaterale, lo stesso della 222: passano anche gli MPO stereo (fotocamere 3D) e quelli
  dell'iPhone, con la prima immagine.

### Generatore sintetico

`sintetiche.UltraHDR(principale, gainMap, orientamento)` (`internal/imaging/sintetiche/ultrahdr.go`):
la foto principale con EXIF (produttore e orientamento), APP1 XMP con `hdrgm:Version` e il
contenitore GContainer (Primary + GainMap con la lunghezza), il profilo ICC e un APP2 MPF big endian
con due voci (principale `0x030000` a offset 0, gain map con dimensione e DataOffset veri
dall'intestazione TIFF dell'MPF); in coda la gain map, una JPEG in scala di grigi con il suo XMP
`hdrgm`, di dimensioni diverse dalla principale. Pillow 9.4 la apre come MPO con `n_frames` 2
(misurato). Nessuna foto vera.

## Misurato sullo standalone (`fab-222`, 04/11/2026)

Immagine `gestrip-standalone:fab-222` (`sha256:d83804a38c43…`, creata il 03/11/2026 alle 18:17,
Pillow 9.4.0). I 16 file sintetici scritti dal Go (`to-be-deleted-after-use/slice-045b/file/`) sono
stati normalizzati con lo script vero dell'immagine
(`/app/lib/gestrip_standalone-0.1.0/priv/normalize_image.py`, container senza rete), e Pillow ha
detto anche come apre ogni file. **Esito e dimensioni uguali al Go in 16 casi su 16:**

| File | Pillow apre come | `fab-222` | Go | `fab-220` |
| --- | --- | --- | --- | --- |
| Ultra HDR 64×32, orientamento 6, gain map 16×8 | MPO, 2 | 32×64 | 32×64 | rifiutata |
| la stessa con la coda della gain map rovinata (200 byte) | MPO, 2 | 32×64 | 32×64 | rifiutata |
| Ultra HDR 1600×1200, gain map 400×300 | MPO, 2 | 1600×1200 | 1600×1200 | rifiutata |
| Ultra HDR 640×480 del banco, gain map 160×120 | MPO, 2 | 640×480 | 640×480 | rifiutata |
| Ultra HDR 8160×6120 (50 MP), gain map 2040×1530 | MPO, 2 | 4080×3060 | 4080×3060 | rifiutata |
| Ultra HDR 6000×4500, orientamento 8 | MPO, 2 | 3072×4096 | 3072×4096 | rifiutata |
| JPEG 64×48 + MPF con 2 voci `0x030000` | MPO, 2 | 64×48 | 64×48 | rifiutata |
| MPF dell'iPhone (`0x20030000`, `0`) | MPO, 2 | 64×48 | 64×48 | rifiutata |
| MPF di disparità (`0x020002` ×2) | MPO, 2 | 64×48 | 64×48 | rifiutata |
| MPF con un tipo MP qualsiasi (`0x020000` ×2) | MPO, 2 | 64×48 | 64×48 | rifiutata |
| MPF con 1 immagine | JPEG | 64×48 | 64×48 | 64×48 |
| MPF senza voci | JPEG | 64×48 | 64×48 | 64×48 |
| MPF di formato 1 (`0x01030000` ×2) | JPEG | 64×48 | 64×48 | 64×48 |
| MPF con la seconda di formato 1 | JPEG | 64×48 | 64×48 | 64×48 |
| MPF con 3 immagini e 2 voci (voci corte) | non si apre | rifiutata | rifiutata | rifiutata |
| PNG animato (acTL 2) | PNG animato | rifiutata | rifiutata | rifiutata |

- In ogni uscita di `fab-222`: un solo SOI e nessun `MPF`. Nella Ultra HDR 64×32 con orientamento 6
  i colori della foto salvata sono quelli della principale raddrizzata (rosso (253,0,2) in alto a
  destra, verde (0,255,0) in basso a destra, nero in basso a sinistra), gli stessi punti che
  controlla il test Go.
- Pillow avvisa «malformed MPO file» per i tre MPF con una voce di formato non JPEG: li tratta come
  JPEG normali, come `immaginiMPO`.
- La colonna `fab-220` (stesso script prima della 222) mostra il rosso di prima.
- **Solo ragionato, non misurato:** che lo standalone, con un MPO, prenda l'EXIF della prima
  immagine (`_after_jpeg_open` di Pillow 9.4 non chiama `seek(0)`, e l'orientamento misurato torna);
  che un MPF con DataOffset sbagliati non dia fastidio (Pillow legge gli offset solo per i
  fotogrammi dopo il primo, che la normalizzazione non apre).

## Banco

- **Immagine:** `gestrip-standalone:fab-222` al posto di `fab-220` in `tools/banco` (uso ed esempio
  di `-immagine`, commento di `esitoNumeriBuono`), `docs/PIANO.md` (riga di `go-baseline`) e
  `docs/prova-dati-veri.md`; le ricevute e i commenti dei test che citano misure fatte su `fab-220`
  restano com'erano (sono la storia di quelle misure).
- **Scenario nuovo `foto-ultra-hdr`** (`bench/scenari/foto-ultra-hdr.json`, RIP-S-30,
  `sintetici-import`): il titolare carica sulla busta 19 la Ultra HDR sintetica 640×480 (15 KB) e un
  JPEG con le voci MPF corte, poi apre la scheda. Atteso dalle due parti: 201 `{"ok":true,
  "photo_id":#}`, 422 «Foto rifiutata.», scheda con «Foto (1)» e la miniatura della foto 7. Nessuna
  riga in `bench/deviazioni.json`. La 19/26 (passo `numeri`) non ha foto e nessun altro scenario ne
  conta le foto; lo scenario non apre la foto, per non cambiare il conteggio dell'audit
  `repair.staff_photo_viewed` di `foto`. Test del repo: `TestScenarioUltraHDRSuiDueDati` (il corpo
  mandato è un MPO con due SOI, `MPF` e `hdrgm`), e lo scenario aggiunto agli elenchi di
  `TestScenarioUtentiSuiDueDati` e `TestProvaConDegliScenariDelRepo`.
- **Esiti** (`fab-222`, `-dati sintetici-import`, istanze nuove, binari dal codice di adesso):
  - primo giro completo, con lo scenario sulla busta 20: HTTP verde (35 scenari, 561 passi, 84
    attese, 0 inattese); browser rosso, 7 inattese tutte nello scenario `conferme`, uguali dalle due
    parti («Foto (3)» invece di «Foto (2)»…): `conferme` conta le foto della 20/26, e la foto in più
    era dello scenario nuovo. Corretto spostandolo sulla 19/26;
  - secondo giro completo: **HTTP verde** (35 scenari, 561 passi, 84 attese, 0 inattese, 0 errori),
    **browser verde** (7 scenari, 14 viste, 22 attese, 0 inattese, nessun «da decidere»); import Go
    riconciliato; `banco giu` e `banco controlla`: «AVANZI: nessuno».
  - **Passaggio a `fab-222`:** nessuna differenza nuova negli scenari vecchi. Le attese sono le
    stesse 84 + 22 della 045, e dei 561 passi 6 sono dello scenario nuovo. Il giro su
    database vuoto non è stato fatto.

## Prove

- **Test** (`internal/imaging/normalizza_test.go`):
  - `TestNormalizzaPiuFotogrammi` (era `TestNormalizzaRifiutaPiuFotogrammi`): i casi MPO che lo
    standalone ora accetta sono accettati, con le dimensioni (JPEG 64×48, non quadrato, così le
    dimensioni dicono qualcosa); restano rifiutati «MPO con le voci corte» e l'APNG con 2
    fotogrammi.
  - `TestJPEGSiFermaAllaPrimaEOI`: `image/jpeg` decodifica una Ultra HDR, e una con la gain map
    rovinata, pixel per pixel come la sola foto principale; `DecodeConfig` dà le sue dimensioni.
  - `TestNormalizzaUltraHDR`: la Ultra HDR 64×32 con orientamento 6 esce 32×64, di base, senza EXIF
    né ICC, un SOI, niente `MPF` né `hdrgm`, con i colori della principale e non il grigio della
    gain map; la miniatura rifiuta l'MPO e fa quella della foto salvata; fuori da `-short` 50 MP →
    4080×3060 e 6000×4500 con orientamento 8 → 3072×4096.
- **Rosso controllato con due mutazioni** (non salvate come file): `Normalizza` con
  `apri(…, false)` (la regola di prima) fa cadere i tre test; il draft limitato a `fotogrammi == 1`
  fa cadere «Ultra HDR da 50 MP: 4096×3072, attese 4080×3060».
- **`scripts/check`:** verde al secondo giro (`prove/check-2.txt`; copertura senza l'esito del
  banco: 353 voci, chiuse 26, rosse 0, parziali 55; dopo la 045 erano chiuse 27: RIP-S-30 ha
  ora lo scenario `foto-ultra-hdr` e senza l'esito del banco resta parziale, come ogni voce con
  scenari; con l'esito del giro verde si chiude). Il primo giro (`prove/check-1.txt`) era caduto
  su due test del repo del banco, `TestScenarioUtentiSuiDueDati` e `TestProvaConDegliScenariDelRepo`,
  che tengono l'elenco degli scenari con attese per i dati: lo scenario nuovo è stato aggiunto.
- **TDD:** il codice è stato scritto prima dei test nuovi; il rosso è quello delle due mutazioni
  sopra, non salvato.

## Correzione della review

La review indipendente ha misurato con gli script veri di `fab-222` (`normalize_image.py` e
`thumbnail_image.py`, container senza rete) 30 forme limite dell'indice MPF e dell'EXIF scritte dal
Go: prima della correzione 6 esiti della normalizzazione erano diversi da Pillow (3 file che Pillow
non apre accettati dal Go, 3 che Pillow apre rifiutati dal Go), più qualche esito della miniatura.
Erano differenze già della slice 036, non portate dalla 045b; nessuna foto vera ha quelle forme.
Corretti in `internal/imaging/formati.go`, ora **30 su 30 uguali**:

- `immaginiMPO` scorre le voci anche con 0 o 1 immagini (una sola immagine con la voce corta:
  Pillow non apre il file), prende le voci solo se UNDEFINED o BYTE (gli altri tipi danno
  TypeError: JPEG normale) e le legge big endian solo con la testa «MM\0*», come `_getmp`;
- `intero` (NumberOfImages) prende il primo valore di ogni tipo intero, anche segnato o ripetuto;
- `leggiIFD`: un tag con i dati fuori dal blocco **ferma** la lettura dell'IFD (OSError di
  `_safe_read`, `load` si ferma), non si salta. Tocca anche l'EXIF: un tag rotto prima
  dell'orientamento lo nasconde e la foto non si raddrizza, come nello standalone.

Test nuovo `TestMPFComePillow` (19 forme MPF per normalizzazione e miniatura, 3 EXIF).

## Per il manuale

- Le foto «Ultra HDR» dei telefoni recenti si caricano come tutte le altre. Sono, per esempio, le
  foto ritagliate o ingrandite nella Galleria di alcuni Android.
- Si salva la foto normale: l'effetto HDR (la luminosità in più sugli schermi che lo mostrano) si
  perde, come per ogni foto, che viene sempre ripulita e salvata in JPEG.
- PNG e WebP animati restano rifiutati con «Foto rifiutata.».

## Dubbi

- Nessuna decisione di prodotto presa qui: dove c'era da scegliere si è copiata la 222 (la miniatura
  rifiuta ancora l'MPO, come `thumbnail_image.py`).
- Da fare dalla sessione principale: la riga D-65 dell'inventario (allineata alla 222) e il registro
  di allineamento.

## Banco dopo la review (sessione principale, 04/11/2026)

Giro mirato sugli scenari delle foto (`coda-foto`, `foto`, `foto-dipendente`, `foto-ultra-hdr`) con i
binari ricompilati dal codice corretto dalla review, `fab-222`, `-dati sintetici-import`: HTTP 4
scenari, 63 passi, 0 differenze attese, 0 inattese; browser 7 scenari, 14 viste, 22 attese, 0
inattese; «AVANZI: nessuno». `scripts/check` verde rilanciato dalla sessione principale.
