# 104 — Dalla risposta del modello alla scheda (T4.10, quarta; D-19, D-20, D-103, D-104)

- **Fonte:** traduttore di produzione di pc-fisso `~/gestrip-e2b/traduttore.py` (:21 compro oro, :30-60
  `trova_telefono`, :72-77 note, :78 consegna dalla sola «c», :89-90 messaggio «Oggi/Dettato», :136-139 JSON rotto),
  letto soltanto; standalone a `go-baseline`: `assist.ex` (:563-596 `decode`/`readable`, :64 compro oro, :384-388
  `to_string`, telefono e compro oro), `workshop_live.ex` :2172-2188 (campi nel modulo).
- **Voci:** RIP-H-42 e RIP-H-43 **parziali** (si chiudono con 105-108; nessun segno `// inventario:`). Nessuna D nuova.

## Cosa c'è

- **`internal/assist/scheda.go`, `telefono.go`** (vedi SVILUPPO, «Dalla risposta del modello alla scheda»):
  `LeggiRisposta` (valida / non valida, lettura tollerante), `PropostaDa` (struct `Proposta` con i campi del modulo
  della busta nuova, date AAAA-MM-GG), `TelefonoDalDettato` (D-104), `Domanda` (formato corto D-103), `Estrai` (D-19:
  un solo nuovo tentativo, poi `ErrRispostaIllegibile`; un errore del chiamante esce subito). Nessun log; gli errori
  non contengono testo del dettato. Nome e oreficeria restano grezzi (105); rete e attesa sono della 106.
- **`internal/telefono`** (nuovo, pacchetto foglia): `Normalizza`, spostata da `buste` (era `NormalizzaTelefono`,
  funzione pura) con il suo test (ART-02); la usano `buste/artigiani.go`, `web/anagrafiche.go` e `assist`, che così
  non importa più `buste` (niente cicli quando `buste` userà `assist`, 113).
- **SVILUPPO:** sezione «Dalla risposta del modello alla scheda (slice 104…)»; riga del telefono normalizzato
  aggiornata.

## Scelte

1. **Date da incidere** (obbligo passato dalla 103; coordinatore, 20/11/2026): l'esclusione è fatta così: la consegna
   si calcola **solo da «c»**, mai dal dettato o dalla descrizione «d», come il traduttore, con test espliciti
   (`TestConsegnaSoloDallaFraseC`). Il caso «il modello mette in «c» la data da incidere» resta com'è oggi in
   produzione (la prende per consegna). Né il traduttore né lo standalone hanno una regola nel codice: le tiene fuori
   il modello (istruzioni dello standalone `prompt.ex:52`, o il modello addestrato).
2. **Decimali nei campi copiati come lo standalone** (`Float.to_string` di Elixir: «1.5e3», «1.0e-5», da 2^53 sempre
   con esponente), misurati con Elixir 1.18/OTP 27 su 40 386 numeri; nelle note la forma di Python (lì scrive il
   traduttore). Rilievo MEDIO della revisione.
3. **Differenze volute dal traduttore** (tutte risposte rotte, coerenti con D-19 «campo non semplice → illeggibile»):
   risposta non JSON, busta/acconto annidati o infiniti, `[]`/`{}` vuoti in «b», «a», «c», «x», `NaN`, «c» o «x» non
   testo → non valida, nuovo tentativo e poi errore (il traduttore dava una scheda vuota o a metà, o un 502).
4. **Compro oro con lo spazio di PCRE** dello standalone (che in produzione ricalcola il compro oro dopo il
   traduttore): U+180E vale come spazio, U+001C-001F no; misurato con Elixir. 3 differenze su 30 000 dettati a caso
   rispetto al solo traduttore.
5. `{}` (BERT che non trova niente) è valida, come lo standalone. Un errore di rete senza nuovo tentativo: lo decide
   la 106. `Domanda` non toglie gli spazi ai bordi del dettato come fa `.strip()` del traduttore: da sistemare nella
   106.

## Idee (coordinatore, 20/11/2026; non fatte)

- Escludere «c» quando il dettato contiene «incid» e la data di «c» compare anche in «d».
- «incidere 12 12 2026» dettato senza barre dà il telefono «12122026», come il traduttore.

## Prove

- Rosso: 11 FAIL prima del codice. `scripts/check` verde, rilanciato dalla sessione principale prima della revisione e
  dopo le correzioni. Copertura invariata.
- **Test:** 10 funzioni, oltre 200 casi (markdown, testo intorno, campi annidati, JSON rotto, chiavi ripetute o
  sconosciute, righe dei banchi B, C, D, note D-20, 46+ telefoni spezzati, «Busta 12», «Acconto 50 €», Unicode, date da
  incidere, oggi a tarda sera, D-19 con chiamante finto, dettato lungo). Banchi `bench/modello/` verificati: dati
  inventati.
- **Confronto con una copia del traduttore** (più il passaggio dello standalone, decimali come Elixir): i 1720 casi
  dei banchi, 20 000 risposte a caso, 100 000 dettati per il telefono, 30 000 per il compro oro; generatore
  indipendente del revisore, 160 000 righe: uguali tranne le differenze delle Scelte 2-4.
- Fuzz del revisore, 40 s (1,47 milioni di esecuzioni): nessun panic. D-19: al massimo 2 chiamate.
- **Mutazioni dell'implementatore:** 48 compilanti, 46 uccise, 2 equivalenti; dopo la revisione 69, 65 uccise, 1 non
  compila, 3 equivalenti (controllo voluto, spazi ai bordi, normalizzazione del telefono di sole cifre).
- **Revisione indipendente (Opus):** 0 ALTE; 1 MEDIA chiusa (decimali, Scelta 2); BASSE chiuse: differenze volute
  scritte in SVILUPPO, test rinominato, `assist` non dipende più da `buste`, 10 mutazioni vive uccise con test nuovi;
  `TestTelefonoDettatoLungo` misurato a parte (15 ms su 5000 caratteri). Mutazioni del revisore: 57 compilanti, 44
  uccise, 3 equivalenti, 10 vive poi coperte.
- Implementatore Opus: ~250k token, ~28 minuti con le correzioni.
- Nota: per le verifiche con Elixir è stata scaricata l'immagine docker `elixir:1.18.4-otp-27-slim` (~465 MB, resta
  su pc-fisso; si può togliere con `docker rmi`). Le prove sono girate con `--network none`.

## Banco

Nessuno scenario (come da piano): tabelle di test e confronto con il traduttore.

## Per il manuale

Niente di visibile: la proposta di scheda la userà il dettato della nuova busta (slice 108).
