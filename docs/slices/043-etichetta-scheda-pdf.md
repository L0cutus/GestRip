# 043 — Etichetta busta 76×127 e scheda PDF 100×230 (T4.4, seconda)

- **Fonte:** T4.4 del PIANO (piano nella ricevuta della 042); §2.1 e D-24 (cosa vuol dire «uguale»:
  formato, testo estratto normalizzato, contenuto dei QR, posizioni chiave ±0,5 mm nelle etichette).
- **Voci:** RIP-S-43 (etichetta busta, con la decisione del 29/10/2026: senza codice QR il QR
  contiene il numero busta) e RIP-S-45 (scheda PDF, con il blocco della firma).
- **Voci (con l'esito del banco sui dati importati):** chiuse RIP-S-43 e RIP-S-45 (la stampa diretta
  CUPS del 76×127, D-09, resta alla 046).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale con i binari ricompilati.
- **Domande nuove:** 23 (QR dello standalone a specchio; nota 6 per lo standalone) e 24 (font della
  scheda), in `docs/domande-aperte.md`.

## Cosa c'è

- **Etichetta** (`internal/stampe/etichetta.go`, `buste.Etichetta`, `GET /repairs/{id}/label.pdf`):
  contenuti, ordine, testi e misure del CSS di `BagLabel.document/1` e `page/3`: «→ ARTIGIANO» se la
  busta ne ha uno con il nome (9 pt maiuscolo, 0,5 pt fra le lettere, a capo nei 46 mm accanto al QR),
  «#numero» 22 pt, QR di 22 mm, «Consegna: <oggi nel fuso del negozio>» e «Scad.: <testo o data
  AAAA-MM-GG>» se c'è, lo stato con gli spazi al posto di «_» nel riquadro nero, la descrizione
  tagliata come `truncate(180, True, '…')` con la tolleranza di 5 (grafemi), «—» se vuota, la casella
  «FATTO», «Note:» e tre righe. QR = codice salvato così com'è (anche « »), il numero busta solo per
  NULL e "". In linea, allegato con `?download=` non vuoto (anche `0`, anche `download[]=`, come Plug);
  `etichetta_busta_<numero>.pdf`; `repair.label_generated` dopo il PDF; 503 «PDF non disponibile».
  `stampe.PaginaEtichetta(d, e)` disegna un'etichetta sulla pagina corrente: il foglio della consegna
  all'artigiano (T4.8) la chiamerà una volta per busta con l'artigiano della consegna.
- **Scheda PDF** (`internal/stampe/scheda.go`, `buste.SchedaPDF`, `GET /repairs/{id}/scheda.pdf`):
  tutti i contenuti di `RepairSheet.render_html/2` nel suo ordine (negozio, badge dell'oreficeria,
  «N° BUSTA», date gg/mm/aaaa, cliente, telefono, artigiano, oggetti e lavorazioni con gli a capo,
  note e note interne con lo sfondo, «Firma artigiano», «Firma cliente», preventivo «testo €» o
  «€ 12,50» dai soli centesimi, ricevuta di deposito della compro oro con il grassetto in linea, blocco
  «Consegna — Firma del cliente» con dichiarazione, «Firmata il … — ritirata da …» o «(data non
  registrata)» e l'immagine). La firma si legge con `media.Archivio.Leggi` (impronta verificata);
  file mancante, guasto o chiave non valida = nessun blocco; senza `repair.signature_viewed`, come
  lo standalone. Funziona per le firme importate (stesse colonne; provato sul banco con la 3/26). Sempre
  in linea, `busta_<numero>.pdf`, `repair.sheet_generated`. Testo più lungo della pagina: pagine dopo,
  riquadri divisi.
- **Motore:** `Casella.Spaziatura` (letter-spacing), `Documento.Immagine` e `DimensioniPNG` (PNG con
  trasparenza, decodificato con la libreria standard, al più 16 Mpx), QR in modo byte come lo
  standalone. **Lettore:** `Immagini` della pagina, `CodiciQR` (decodificatore QR nostro, senza
  dipendenze), `PosizioniEtichetta`, `TestoPosizioni`, `PosizioniVicine`.
- **Pulsanti** nella barra della scheda, nell'ordine dello standalone: Modifica, «⧉ Anteprima PDF»
  (nuova scheda), «Ricevuta PDF», «🖨 Etichetta busta» (nuova scheda), «📤 Scarica etichetta»
  (`?download=1`); niente `data-share-pdf` né JavaScript (046). Il riquadro «fuori bottega» con
  «🖨 Ristampa» e «📤 Condividi» non c'è ancora nel Go: arriva con la sua slice (artigiani, T4.8).
- **Banco:** campi `pdf:qr` e `pdf:posizioni` (±0,5 mm), scenari `etichetta` (33 passi),
  `scheda-pdf` (23) ed `etichetta-scheda-dipendente` (6). Regole in `docs/SVILUPPO.md`.

## Misurato sullo standalone (fab-220, 03/11/2026)

PDF scaricati dalle due app del banco sui dati importati (programmino usa e getta nel container sulla
rete del banco) e letti con `internal/pdf/lettura`; matrice del QR letta con `erl` nell'immagine.

- Etichetta: pagina 75,861×127,000 mm (Formato «76x127»), DejaVu Sans; contenuto da 2,91 mm (Chromium
  arrotonda il margine di 3 mm a 11 px). Posizioni della 5/26: moduli del QR 53,87/5,94 16,09×15,90;
  «#5/26» 13,14/16,67; «Consegna:» 2,91/30,96; casella FATTO 3,04/53,58 3,70×3,70. Il Go cade entro
  0,3 mm su tutte le righe (fixture `chromium-etichetta-{1,4,5}.pdf`).
- **QR dello standalone:** un rettangolo `re … f` per modulo scuro (l'SVG di QRCode 3.2.0), versione
  1, livello M, maschera 0, **modo byte** anche per un testo alfanumerico, e **trasposto**: diritto
  non si legge, scambiando righe e colonne sì. Nella matrice di QRCode i moduli di sincronismo (6, 8) e
  (8, 6) sono chiari.
- Scheda: pagina 99,822×230,039 mm («100x230»), in **Liberation Sans** (il CSS chiede Helvetica
  Neue/Helvetica/Arial); il testo estratto è lo stesso del Go in DejaVu per le buste 1, 2, 3, 5, 6,
  7, 13 (fixture `chromium-scheda-{2,3,5,6,7}.pdf`).
- La firma sintetica della 3/26 (`\x89PNG…synthetic-signature`, non decodificabile): Chromium stampa
  il blocco con l'alt «Firma» dopo l'icona dell'immagine rotta.
- Risposte (scenari del banco, verdi): `inline; filename="etichetta_busta_1_26.pdf"`; `download=1`,
  `download=0`, `download[]=1` → `attachment`; `download=` e `download=1&download=` → `inline`; la scheda
  è `inline` anche con `download=1`; `+1`, `01`, la barra in fondo → la busta 1; 999, 0, -1 → 404;
  «abc», «1.0», « 1», «1%2F2» → 400; oltre i 64 bit → 500; `label.PDF` → 404; HEAD 200 senza corpo.

## Solo ragionato (dal codice e dai test dello standalone, non misurato)

- Taglio della descrizione a 185/179 grafemi; artigiano con il nome vuoto senza riga; QR « » com'è;
  la riga d'audit con `bag_number` (`record_repair_event/3`); firma senza istante «(data non
  registrata)», timbro nel fuso del negozio, «ritirata da» solo se non è NULL.
- Disposizione e misure di una firma PNG vera (nel banco la firma non si decodifica): un px del CSS
  per pixel, alta al più 2,5 cm.
- Il badge per qualunque oreficeria con il nome (anche la «Negozio» con id 1, se una busta la avesse).
- Etichette e schede più lunghe della pagina; la riga .meta con una scadenza lunghissima.
- Il fuso del container dello standalone per «Consegna:» (`:calendar.local_time()`): nel banco le due
  date coincidevano.

## Differenze e dubbi

1. **QR trasposto nello standalone** (misurato): il Go stampa il QR diritto, lo stesso contenuto. I
   telefoni leggono anche il trasposto (riprovano specchiato), ma uno scanner più semplice potrebbe no.
   Raccomandazione: tenere il QR diritto nel Go (è lo standard) e segnare la cosa come nota per lo
   standalone.
2. **Font della scheda:** Liberation Sans in Chromium, DejaVu nel Go (regola della 042: solo DejaVu
   incorporato). Testo uguale; a capo e altezze diverse, e una scheda molto lunga potrebbe passare alla
   seconda pagina in un punto diverso (formato diverso per D-24). Raccomandazione: restare con DejaVu;
   se serve, incorporare Liberation Sans (licenza SIL OFL 1.1) solo per la scheda in una slice futura.
3. **Firma che non si decodifica:** il Go copia Chromium e scrive «Firma» al posto dell'immagine (il
   blocco resta, perché il file si legge e l'impronta torna). Raccomandazione: tenere.
4. **Firma più larga di 90 mm:** il Go la riduce alla larghezza della pagina; Chromium la farebbe
   uscire dal bordo. Solo resa grafica.
5. **Maiuscolo:** `strings.ToUpper` del Go e `text-transform: uppercase` differiscono su pochi
   caratteri (ß → «SS» nel CSS). Raccomandazione: lasciare finché non capita un nome vero.
6. **Pagina troppo piena:** un'etichetta con un artigiano dal nome lunghissimo non va su una seconda
   pagina come in Chromium: il Go continua oltre il fondo. Raccomandazione: lasciare (nomi brevi).

## Review indipendente

Nessuna ALTA. Correzioni fatte (test prima rosso, log in `prove/rosso-review-*.log`):

- **MEDIA, firma con dati guasti:** una firma con l'IHDR giusto e l'IDAT che non è zlib (impronta
  giusta: lo standalone accetta una firma guardando solo i byte magici, quindi può arrivare
  dall'import) faceva fallire tutta la scheda (503 a ogni apertura): la scelta fra immagine e alt
  guardava solo l'intestazione e la decodifica avveniva dopo. Ora `pdf.LeggiPNG` decodifica davvero
  prima di scegliere; se fallisce, l'alt «Firma» come Chromium (`TestSchedaFirmaConDatiGuasti`).
- **BASSA, limite dei 16 Mpx senza test:** `TestLimitiDelMotore` con un PNG valido di 4001×4000 (il
  valore del limite non è cambiato).
- **BASSA, bit di formato corretti in silenzio:** il decodificatore accettava fino a 3 bit sbagliati;
  ora vuole una copia esatta (`TestQRFormatoSenzaCorrezione`: un bit cambiato in una copia si legge
  con l'altra, in tutte e due no). Le fixture di Chromium passano lo stesso.
- **BASSA, `PosizioniVicine`:** un numero con «;» e lo stesso senza erano vicini (separatore
  diverso); ora il separatore deve essere lo stesso, e voci con nomi o lunghezze diverse non sono
  vicine (`TestPosizioniVicineStruttura`); la riga «#numero» con l'artigiano sopra ha un valore
  atteso assoluto (`TestPosizioniConLArtigiano`).
- **Test mancanti o deboli:** il confronto riga per riga con Chromium ora fallisce se il numero delle
  righe è diverso (prima si saltava); `Specchiato == true` fissato sulle fixture; ritirata da «» →
  « — ritirata da »; 0 centesimi → «€ 0,00»; `?download=1&download=` in linea anche nei test web;
  6,8 mm fra le righe delle note; i pixel dell'immagine nel PDF (`/Width`, `/Height`); immagine con
  un lato nullo o negativo e spaziatura negativa sono errori (nessun documento dello standalone le
  usa). Questi casi erano già giusti nel codice: i test li fissano.
- **Documentato** in `docs/SVILUPPO.md`: `pdf:qr` confronta solo il testo, un QR del Go specchiato lo
  ferma il test di `internal/stampe` che controlla `!Specchiato`.

## Prove

- **TDD:** rosso salvato per decodificatore QR, motore (spaziatura, PNG, modo byte), dominio, stampe
  (scheda), pagine e banco (`to-be-deleted-after-use/slice-043/prove/rosso-*.log`); per
  l'impaginazione dell'etichetta i test sono nati con il codice, confrontati con le fixture di
  Chromium.
- **Banco** (sottoagente, binari ricompilati dal codice finale): dati importati verde (29 scenari, 428
  passi, 0 differenze inattese; browser verde, 2 «da decidere» della domanda 18); database vuoto verde
  (15 scenari non provati con quei dati). Prove negative sui dati importati: QR con «-X» in fondo al
  contenuto → 44 differenze (solo `pdf:qr`), QR spostato a destra di 1 mm → 22 differenze (solo
  `pdf:posizioni`), rosso tutte e due.
- **Sessione principale:** nel suo giro il primo banco su database vuoto ha avuto un 502 dello
  standalone su `csrf-intestazioni[2]` (un passo dell'avvio, scenario non della slice), passato al
  secondo giro: intoppo dello standalone, non del Go.
- **`scripts/check`:** verde (sottoagente), con la copertura dell'inventario.
- **Sessione principale, codice finale, binari ricompilati:** `scripts/check` verde; banco sui dati
  importati verde (29 scenari, 428 passi, 0 differenze inattese; browser con i 2 «da decidere» della
  domanda 18), database vuoto verde (15 passi non provati con quei dati), «AVANZI: nessuno».
- **Copertura:** con l'esito dei dati importati chiuse 60, parziali 18; senza banco chiuse 27.
- **Mutazioni della review:** 67 tentate, 65 compilate, 50 uccise; delle 15 vive 2 equivalenti di
  fatto, 13 buchi dei test coperti nelle correzioni.
