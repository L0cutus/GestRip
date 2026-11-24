# 116 — Pseudonimi per l'esportazione dei dettati; scadenza più prudente (T4.10, sedicesima)

- **Fonte:** piano della T4.10, riga 116 (approvato da Stefano il 19/11/2026). **Chi decide:** lo standalone a
  `go-baseline` (`Assist.Pseudonym`, `pseudonym_test.exs`; `TrainingExport.export/5` per il cliente escluso), salvo le
  D approvate.
- **Voci:** RIP-H-52 (parte pseudonimi; il comando di esportazione è la 117), RIP-H-51 (D-110). Segno `// inventario:`
  sui test.
- **D applicate, nuove (coordinatore, 24/11/2026, registrate nell'inventario):**
  - **D-110** (dubbi 2 e 3 della 115): la scadenza dei dettati salta il giro se l'orologio risulta andato avanti o dopo
    un import senza backup verificato più recente; il limite è il minimo fra «adesso − 12 mesi» ed «evento d'audit più
    recente − 12 mesi», senza contare le righe `assist.training_expired`.
  - **D-111** (rilievi della review): rinforzo della privacy dell'esportazione, più severo dello standalone (sotto).
- **Modelli:** implementatore Opus (privacy), revisione e mutazioni Opus (tre giri: completo, mirato, breve).

## Cosa c'è

- `internal/assist/pseudonimi.go`: porting di `Assist.Pseudonym`. `Pseudonimizza(RigaDaEsportare, Chiave)` restituisce
  la riga con nome e telefono sostituiti da valori inventati, oppure il motivo dello scarto: `nome_non_trovato`,
  `nome_ambiguo`, `nome_residuo`, `telefono_dubbio`, `telefono_residuo`, `valore_illeggibile`, `altro_nome`,
  `cliente_escluso` (più `errore`, dove lo standalone andrebbe in eccezione). `ParoleDeiNomi` e `ParoleDiAltriNomi`
  (`other_names` senza database: chi passa clienti e oreficerie è la 117).
  - **Chiave:** `NuovaChiave()` da `crypto/rand`, 32 byte; generatore ChaCha8 con seme HMAC-SHA256(chiave, id), quindi i
    valori inventati sono diversi da quelli dello standalone (regole uguali). `Chiave` e `RigaDaEsportare` non si
    stampano con nessun verbo di `fmt` né in slog (metodo `Format`).
  - **Jaro** come Elixir 1.18 (trasposizioni divise in virgola), con i valori misurati nell'immagine dello standalone.
  - **Valori:** stringhe, `json.Number`, interi e `float64` interi esatti si leggono come testo (sostituzione e rete
    finale); un decimale, NaN o un numero troppo grande danno `valore_illeggibile`.
- **D-111, nel dettaglio:**
  1. prima di tutto si tolgono i caratteri di formato invisibili `\p{Cf}` da testo, campi e candidati; U+02BC, U+02B9
     e U+2032 contano come apostrofi. Nella sola **copia di ricerca** (`copiaDiRicerca`) gli apostrofi diventano «'» e
     ogni segno non ASCII (`\p{P}`, `\p{S}`) fra due lettere si toglie: un nome che si vede solo lì fa scartare la riga
     (`nome_residuo`/`altro_nome`), e il testo esportato non cambia («l’anello» resta «l’anello»);
  2. le cifre si uniscono attraverso spazi Unicode e multipli, tab, a capo, «.», «,», «/», «-»; un telefono è una fila
     di 9-11 cifre che comincia con 0 o 3, oppure «+»/«00» seguiti da almeno 9 cifre; se non è noto, la riga si scarta
     come `telefono_dubbio`. La matricola resta;
  3. una fila che comincia con «00» si unisce solo attraverso gli spazi (numeri di busta e date scritti di seguito non
     diventano telefoni), ma «0041/79 123 45 67» e simili si rileggono insieme e restano telefoni;
  4. una data resta separata dalle altre cifre solo se è plausibile (giorno 1-31, mese 1-12, stesso separatore), ha
     l'anno a 4 cifre e non ha cifre attaccate; la si cerca da ogni posizione (niente dipendenza dalle sovrapposizioni
     della regex). Le date con l'anno a 2 cifre si uniscono come le altre cifre;
  5. con il telefono del cliente nel testo si riscrive solo un secondo numero che le forme dello standalone riconoscono
     da sole; uno visto solo dalle forme nuove fa scartare la riga. Nessun numero sconosciuto esce mai in chiaro.
- **D-110:** `audit.OrologioAvanti(ctx, q, adesso, escluse...)` estratta dalla conservazione dell'audit (che si comporta
  come prima, provato) e usata da `ScadenzaDettati` insieme a `db.BloccatoDopoImport`. Nei casi di salto: (0, nil),
  niente audit, niente messaggi; la pulizia segna il giorno e riprova il giorno dopo.

## Prove

- Parte A: tutti i casi di `pseudonym_test.exs` con dati inventati (telefoni `333 000 …`, oreficeria «Esempio»), più i
  casi limite (accenti nei due sensi e forma decomposta, maiuscole, nome in parte, più occorrenze, apostrofi, spazi
  Unicode, 5 forme di telefono, +39/0039) e un test per ogni regola della D-111, con e senza il telefono del cliente, con
  gli esempi chiesti dal coordinatore («busta 0045/26 24/11/2026», «buste 0045/26, 0046/26», «0045/26 0046/26
  0047/26», «30/11/2026 3 anelli», «03/12/2026 40 euro», «300 - 350 - 400 euro», «0041/79 123 45 67», «Ros⠀si»,
  «347 123⏎4567»).
- Confronto con lo standalone vero (implementatore, una tantum, immagine `elixir:1.18.4-otp-27-slim` senza rete, 1 GB):
  14.000 casi inventati, stesso motivo in tutti, 0 differenze di struttura nelle righe esportate (prima della D-111).
- Parte B: orologio avanti (sopra il margine salta; al margine cancella; sotto il margine il tetto protegge), import
  senza backup, raccolta spenta, righe `training_expired` che non spostano il limite; conservazione dell'audit che
  ignora le proprie righe.
- **Metodo:** nella parte A il primo codice è stato scritto prima dei test (poi resi rossi con versioni finte, per la
  ragione giusta); tutte le correzioni successive sono partite da un test rosso.
- `scripts/check` verde, rilanciato dalla sessione principale (exit 0, 0 rosse). Nessun banco: il piano non lo prevede
  per la 116 (ok del coordinatore).

## Mutazioni

- Implementatore: 33 + 11 (giri 1-2), 24 (giro 3), 16 (giro 4); le sopravvissute finali sono equivalenti o uccise da
  test aggiunti.
- Revisore: giro 1 30 compilanti, 25 uccise; giro 2 20 compilanti, 17 uccise; le sopravvissute non equivalenti sono
  state coperte da test nuovi.
- Revisore, giro 3: 13 compilanti, 13 uccise. Implementatore, giro 5: 15 compilanti, 11 uccise, 4 equivalenti o quasi
  (una è la pulizia dei segni ASCII, dubbio 4).

## Review (Opus, indipendente)

- **Giro 1:** ALTA A1 (telefoni sconosciuti con separatori non ammessi, ereditato) e MEDIA M2 (nomi spezzati da
  caratteri invisibili, ereditato) → D-111; MEDIA M1 (Jaro diverso da Elixir) e M3 (`json.Number` non letto) corrette;
  BASSE: chiave visibile con `%d` (corretta), messaggio di `OrologioAvanti` (corretto), test della rete finale (aggiunto).
- **Giro 2:** chiusi tutti; nuove ALTE «0041/79…» e `float64` (corrette); MEDIA falsi positivi su date e importi
  (D-111 punti 4 e 5), a capo (unito, decisione del coordinatore), test della conservazione (aggiunto); BASSA U+2800 e
  U+00B7 (tolti).
- **Giro 3 (breve):** ALTE chiuse; MEDIA nuova dalla regola delle date («0423 52/12/34», «347 12.12.26» uscivano) →
  date solo plausibili con l'anno a 4 cifre, cercate da ogni posizione; BASSA U+2027 e U+30FB → pulizia dei segni non
  ASCII fra lettere nella copia di ricerca. Corretti nell'ultimo giro dell'implementatore; verifica finale della sessione
  principale: `scripts/check` e test dei pseudonimi verdi.

## Per il manuale

Nessun cambiamento visibile: i pseudonimi si vedono solo con il comando di esportazione della 117. Per la scadenza dei
dettati: «Se l'ora del sistema sembra andata avanti, o il programma è appena stato importato e non c'è ancora un backup
verificato, la cancellazione dei dettati vecchi di 12 mesi aspetta il giorno dopo, senza avvisi.»

## Differenze dallo standalone (dichiarate)

- D-111 (sopra): più righe scartate, nessun nome o telefono in chiaro; con il telefono del cliente nel testo, i numeri
  visti solo dalle forme nuove fanno scartare invece di essere riscritti.
- Valori inventati diversi (generatore diverso), regole uguali.
- `cliente_escluso` è un campo della riga (`RigaDaEsportare.ClienteEscluso`, controllato per primo); nello standalone sta
  in `TrainingExport.export/5`. La lettura di `dettati_esclusioni` è della 117.
- D-110: la scadenza dei dettati è più prudente dello standalone.

## Dubbi e note

1. **Per la 117:** leggere il JSON con `UseNumber` (nota 14); decidere se serve una chiave fissa costruibile solo nei
   test (lo standalone ha l'opzione `key:`); lo scarto «busta nel cestino» va con la 117 o la T4.11.
2. **Prezzi in fila** con il telefono del cliente nel testo riscritti come telefono, come nello standalone (nota 13).
3. I due difetti dello standalone chiusi dalla D-111 sono in `domande-aperte.md`, «Note per lo standalone», nota 11.
4. **Segni ASCII dentro un nome** («Ros-si», «Ros.si») passano come nello standalone: la pulizia vale solo per i segni non
   ASCII (il trattino serve in «porta-anelli»). Nota 15.
5. **Nei campi** (note, proposta), «0423 52.12.34», «0423.52.12.34» e «347 12.12.26» sono riconosciuti dalle forme dello
   standalone e quindi **sostituiti** (D-111 punto 5), non scartati; non escono mai in chiaro (test). Nota 15.
