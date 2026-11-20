# 103 — Date di consegna calcolate dal codice (T4.10, terza; D-18)

- **Fonte:** `~/gestrip-e2b/consegna.py` (traduttore di produzione di pc-fisso, letto soltanto; le prove su una copia in
  `to-be-deleted-after-use/`). Lo standalone non lo usa: lì le date le calcola il modello, ed è la D-18.
- **Voci:** RIP-H-42 (parte date) e RIP-H-46 (D-18) **parziali**: il segno `// inventario:` si mette quando si chiudono
  (104-108), perché lo strumento di copertura conta chiusa una voce con il segno su un test verde. Nessuna D nuova.

## Cosa c'è

- **`internal/assist/consegna.go`** (pacchetto nuovo del PIANO §1.2): `DataConsegna(frase, oggi) (time.Time, bool)`,
  porting riga per riga di `consegna.py`: oggi, domani, dopodomani, giorni della settimana, «il 30», «il 5 novembre»,
  «fra N giorni/settimane/mesi», «e mezzo», «entro fine mese», «fine settimana», Natale, orari ignorati, minuscole come
  Python (anche la İ); nessuna frase riconosciuta → vuota. Di `oggi` conta solo il giorno nel suo fuso (il chiamante
  passa `clock.Roma`). Non usa `buste.Consegna` (tipo di salvataggio): il passaggio alla scheda è della 104.
- **SVILUPPO:** sezione «Date di consegna dal dettato (slice 103, D-18)».

## Scelte

1. **Uguale a `consegna.py` anche nelle stranezze** (Stefano, 19/11/2026, domanda D del piano; ogni ritocco va fra le
   idee). Copiate uguali, fra le altre: «il 22 ottobre» detto il 23/10 → 22/10 dell'anno dopo; «per la fine del mese
   prossimo» → fine di questo mese; «fine settimana» detto di domenica → il sabato di ieri; «martedì o lunedì» →
   lunedì (vince l'ordine lunedì…domenica); «fra 2 settimane e mezzogiorno» conta come «e mezzo»; «lunedí» con í e
   «İl 30» → vuote; «il 31 febbraio» → vuota.
2. **Dove `consegna.py` va in eccezione, il Go dà la data vuota** (coordinatore, 20/11/2026: niente D, basta la
   ricevuta; è il minimo del piano «nessuna frase riconosciuta → vuota»). In produzione l'eccezione fa fallire tutta la
   risposta del traduttore. Frasi che in Python danno ValueError o OverflowError, tutte nella tabella dei test:
   «lunedì 0», «il 00», «il 29 febbraio» detto dopo il 29/02 di un bisestile (e in un anno non bisestile), numeri
   enormi («fra 99999999999999999999 giorni, martedì», «fra 2635249153387078803 settimane», «fra
   9223372036854775807 giorni/mesi»), date oltre l'anno 9999 («domani» e «fra due giorni» il 31/12/9999, «natale» il
   25/12/9999, «il 5 gennaio» il 30/12/9999). Il Go non va mai in panic.
3. **Correzione del piano: «esclusione delle date da incidere» non sta in `consegna.py`.** Il traduttore riceve dal
   modello la sola frase della consegna (chiave «c»), quindi le date da incidere restano fuori a monte. **Passa
   esplicitamente alla 104** (richiesta al modello e lettura della risposta, formato corto D-103). Il piano lo diceva
   della 103: lo corregge il coordinatore.

## Prove

- Rosso: 97 righe del test cadevano sulla funzione vuota. `scripts/check` verde, rilanciato dalla sessione principale
  prima della revisione e dopo le correzioni. Copertura invariata (nessuna voce chiusa).
- **Tabella:** 135 frasi inventate (le 32 prove in fondo a `consegna.py`, fine mese, dicembre→gennaio, bisestili,
  «venerdì» detto di venerdì, orari, maiuscole e accenti, unicode, casi negativi) più un test sul fuso (sera tardi e
  notte a Roma, «fra un mese» attraverso il cambio dell'ora). **Confronto con una copia di `consegna.py`**: 122 uguali,
  13 eccezioni → vuota, 0 diverse; 50 000 frasi a caso dell'implementatore e 1,3 milioni del revisore (anni 1-9999,
  unicode, numeri fino a 20 cifre): 0 differenze, 0 panic.
- **Mutazioni dell'implementatore:** 32, tutte compilanti e uccise; dopo la revisione 14 nuove, 13 uccise, 1
  equivalente (guardia `n < 0` coperta dalla regola `n < 1`).
- **Revisione indipendente (Opus):** 0 ALTE; 1 MEDIA (date da incidere, scelta 3, decisa dal coordinatore); BASSE:
  eccezione → vuota (scelta 2, decisa), 12 casi di test mancanti (aggiunti, con 3 in più dell'implementatore), commenti
  sui limiti «oltre l'anno 9999» (aggiunti). Mutazioni del revisore: 52 compilanti, 33 uccise, 6 equivalenti, 13 vive
  poi coperte dai casi nuovi.
- Implementatore Opus: ~130k token, ~15 minuti con le correzioni.

## Banco

Nessuno scenario (come da piano): la tabella dei test e il confronto con `consegna.py` fanno da banco di frasi.

## Per il manuale

Niente di visibile: la funzione la userà il dettato della nuova busta (slice 108).
