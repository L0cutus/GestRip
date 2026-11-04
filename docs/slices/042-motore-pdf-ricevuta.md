# 042 — Motore dei PDF e ricevuta della busta (T4.4, prima)

- **Fonte:** T4.4 del PIANO; §2.1 «PDF senza Chromium» (scelta A) e D-24 (cosa vuol dire «uguale»).
- **Voci:** RIP-S-41 (motore dei PDF, sostituita) e RIP-S-42 (ricevuta PDF).
- **Voci (con l'esito del banco sui dati importati):** chiuse RIP-S-41 e RIP-S-42.
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale con i binari ricompilati.

## Piano della T4.4

| Slice | Cosa | Voci |
|---|---|---|
| 042 | motore dei PDF (gopdf, font incorporato, misure in mm, QR vettoriale), lettore di prova; ricevuta 80×297 e pulsante | RIP-S-41 (sostituita), RIP-S-42 |
| 043 | etichetta busta 76×127 (QR = codice o numero busta) e scheda PDF 100×230 (blocco firma se firmata, anche importata) | RIP-S-43, 45 |
| 044 | report A4 con `ids=`, limite 50, senza sezione foto (D-01); filtri della home e link «⧉ Schede PDF» con la T4.2 | RIP-S-46 (parziale), RIP-H-19 → T4.2 |
| 045 | etichette QR prenumerate `/labels` e `labels.pdf`: limite 100, contatore `max` (D-08; la conferma della 045 tolta nella 046), numero sotto il QR (D-21), azzeramento con conferma (D-22) | RIP-S-47, 48 |
| 046 | stampa diretta: `internal/printing` con `Printer`, CUPS (`lp`, `lpstat`, `LC_ALL=C`) e stampante finta; 57×32 e 76×127 (D-08, D-09); «Condividi / Stampa» dal telefono | RIP-S-44; completa 43, 48 |
| 047 | firma di consegna (riquadro, pagina con JavaScript, salvataggio = consegna, `firma.png`, rifai firma) con il livello browser del banco | RIP-S-36…40 |

## Cosa c'è

- **Motore `internal/pdf`** sopra `github.com/signintech/gopdf` v0.38.3 (MIT), con
  `github.com/boombuler/barcode` v1.1.0 (MIT) per il QR; gopdf porta `phpdave11/gofpdi` (MIT) e
  `pkg/errors` v0.9.1 (BSD-2, alzata da 0.8.1 perché è quella nella cache dei moduli). Tutto dalla
  cache dei moduli di pc-fisso, niente rete. Misure in mm, pagine di formato qualsiasi, DejaVu Sans
  2.37 normale e grassetto incorporati (gli stessi file dell'immagine dello standalone, licenza
  accanto), caselle con a capo sulle parole, spazi come l'HTML o conservati, parole spezzabili,
  taglio a un numero di righe, allineamento, rettangoli, riquadri, linee e QR vettoriale (correzione
  M, margine di 4 moduli dentro il lato). Byte stabili: lo stesso documento dà gli stessi byte.
- **Lettore `internal/pdf/lettura`**, nostro, senza dipendenze: xref classica (anche con `/Prev`),
  FlateDecode, font Type0 Identity-H con ToUnicode e font semplici WinAnsi, matrici (`cm`, `Tm`, form
  XObject). Dà formato delle pagine, righe di testo con posizione e corpo, rettangoli pieni, testo
  normalizzato. Lo usano i test, `pdf.Controlla` (un PDF che non si rilegge non si consegna) e il
  banco. Provato anche su un PDF vero di Chromium dello standalone (fixture sui dati sintetici).
  pdfcpu non era nella cache; un lettore nostro piccolo basta per i due generatori da leggere e non
  porta dipendenze.
- **Ricevuta** (`internal/stampe`, `buste.Ricevuta`, `GET /repairs/{id}/receipt.pdf`): contenuti,
  ordine, testi e misure del CSS di `Receipt.render_html/2`; l'importo solo in centesimi «€ 12,50»,
  il prezzo finale con `Money.label/1` (testo misurato nello standalone), prevista testo/data/«Da
  concordare», nome di ripiego, sottotitolo vuoto = nessuna riga; allegato `ricevuta-<numero>.pdf`;
  `repair.receipt_generated` dopo il PDF; 503 «PDF non disponibile» se il PDF o la riga non riescono.
  Una ricevuta lunga continua su altre pagine 80×297.
- **Pulsante** «Ricevuta PDF» sulla scheda, subito dopo «Modifica», link semplice con il contorno.
- **Banco:** campi `pdf:formato` e `pdf:testo`; scenari `ricevuta` (22 passi) e `ricevuta-dipendente`.

## Misurato sullo standalone (fab-220, 03/11/2026)

- Risposta: 200, `application/pdf`, `private, no-store`, `attachment; filename="ricevuta-1-26.pdf"`;
  pagina 227,04×841,92 pt (80,09×297,01 mm), margini 0 (vince `@page { margin: 0 }`).
- `get_repair!`: 999, 0, -1 → 404 «Not Found»; «abc», « 1», «1.0», «1%2F2» → 400 «Bad Request»;
  «+1», «01» → la busta 1; oltre i 64 bit → 500; `/receipt.pdf/` → 200; `receipt.PDF` → 404.
- `send_download/3`: nome con lettere, cifre e `-._~`, il resto `%XX`, e allora anche `filename*`.
- `Money.label/1` su testo: `String.trim/1` toglie anche gli spazi Unicode non separabili.

## Differenze e dubbi

- Busta oltre i 64 bit: il Go copia il 500 dello standalone (probabile difetto suo; la raccomandazione
  è un 404 come le altre buste che non ci sono).
  Domanda aperta 21, nota 5 per lo standalone.
- Un carattere che DejaVu non ha: Chromium usa un altro font del sistema, il Go stampa «�» (domanda
  aperta 22).
- I margini della ricevuta sono 0 come lo standalone, non quelli scritti nell'inventario: la voce
  RIP-S-42 dell'inventario ha ora la nota della misura.
- Licenza dei font: `internal/pdf/caratteri/LICENSE` porta il testo originale di DejaVu 2.37 (con la
  nota sui glifi Arev), oltre all'origine e alle impronte dei file.

## Review indipendente

Nessuna ALTA. Correzioni fatte (test prima rosso, log in `prove/rosso-review-*.log`):

- **MEDIA, lettore: annidamento senza limite.** 20 milioni di «[» in uno stream di 82 KB facevano
  crollare il processo (stack overflow fatale, misurato anche nel rosso). Ora array e dizionari oltre
  64 livelli sono un errore.
- **MEDIA, lettore: albero delle pagine con nodi ripetuti** (262 144 pagine da un file piccolo, 16 s
  e 3,6 GB). Ora un oggetto che compare due volte nell'albero è un errore, al più 1000 pagine, al più
  128 MiB decompressi in tutto il documento (copre anche molte pagine sullo stesso stream).
- **MEDIA, lettore: immagini in linea in tempo quadratico** (200 000 immagini: 40,8 s nel rosso).
  Ora `bytes.Index` senza copia.
- **BASSA:** uno stream oltre 64 MiB decompressi è un errore, non un contenuto troncato; le CMap
  hanno un tetto di 100 000 voci in tutto il documento.
- **BASSA, motore:** con gli spazi conservati una riga di soli spazi seguita da una parola che da sola
  ci sta: la parola va intera nella riga dopo (prima si spezzava), come pre-wrap di Chromium.
- **Test mancanti** (mutazioni sopravvissute): sottotitolo di soli spazi (posizione di «Busta N»),
  prezzo finale non numerico con spazi ai bordi, glifo assente dal ToUnicode, DecodeParms, numero
  sbagliato all'offset della xref, voci «f» della xref, profondità di pagine e form, limite dello
  stream, `/Prev` circolare, tabulazione fra due parole, misure dei riquadri (il lettore ora dà anche
  i `Contorni`), righe che restano dopo il taglio, righe divise sulla linea di base dentro un BT
  (i due generatori non lo usano; il codice resta per T*, `'` e Td verticali, ed è provato). Ogni
  mutazione rifatta a mano è uccisa (`prove/rosso-review-mutazioni.log`); quella di `/Prev`
  all'inizio sopravviveva per un errore del test (offset dentro «startxref»), corretto.

## Prove

- **TDD:** rosso salvato per lettore, motore, dominio, pagina e banco; per l'impaginazione della
  ricevuta il rosso è stato preso dopo, su uno stub.
- **`scripts/check`:** verde (sottoagente), anche dopo le correzioni della review, su una copia del
  repo senza `to-be-deleted-after-use/`: lì i file di prova della review (`review-042/ostili/`, due
  pacchetti nella stessa cartella) fanno cadere `go vet ./...`.
- **Banco** (sottoagente): dati importati verde (26 scenari, 366 passi; browser verde), database
  vuoto verde, prova negativa con il piè della ricevuta cambiato: 22 differenze inattese, rosso.
- **Sessione principale, codice finale, binari ricompilati:** `scripts/check` verde; banco sui dati
  importati verde (26 scenari, 366 passi, 0 differenze inattese; browser 4 scenari, 2 «da decidere»
  della domanda 18), database vuoto verde (12 passi non provati con quei dati), «AVANZI: nessuno».
- **Copertura:** con l'esito dei dati importati chiuse 58, parziali 18; senza banco chiuse 27.
- **Mutazioni:** review 62 tentate, 60 compilate, 43 uccise; delle 17 vive 3 equivalenti, 2 solo resa
  grafica (grassetto, fuori da D-24), 12 coperte dai test aggiunti nelle correzioni (14 mutazioni
  rifatte, tutte uccise).
