# 037c — CI instabile: errori del processo delle foto

- **Fonte:** richiesta di Stefano del 01/11/2026: la CI del commit `8f57fa1` (run 41269023793, job
  124737758933) è caduta in `TestImageWorkerConIlBinarioIntero` con «foto grande nel binario
  intero: imaging: immagine non valida» dopo 22,4 s; 16 giri su 17 verdi.
- **Voci:** toccate RIP-S-29, 30 (errori del figlio), 33 e 34 (miniatura non fatta per le
  risorse). Nessuna cambia stato.
- **Standalone:** non letto: la differenza (timeout come rifiuto) è già scritta nell'inventario
  (RIP-S-30, «limiti di memoria (512 MB) e tempo (10 s)») e nei commenti della 036.
- **Lavoro:** implementazione, misure, mutazioni e correzioni della review di un sottoagente;
  review e mutazioni di un sottoagente diverso; correzioni decise dalla sessione principale; `scripts/check` e 20 ripetizioni di
  `TestImageWorkerConIlBinarioIntero` rilanciati dalla sessione principale, verdi. Banco non
  rilanciato: nessun comportamento osservabile dal banco cambia. Deviazione dallo standalone
  proposta come D-48 (domanda aperta 5).

## Causa (misurata)

Misure in `to-be-deleted-after-use/ci-instabile/` su una copia del codice di `30a3dc2` con una
riga di diagnosi nel padre (stato d'uscita, segnale, orologio, CPU e RSS massimo del figlio).

- **Il tempo di orologio, non la memoria.** Con un quarto di CPU (`CPUQuota=25%`) il JPEG
  progressivo CMYK 8640×6480 viene fermato dal padre: `muro=10.12s cpu=2.54s maxrss=1345MiB
  uscita=-1 segnale="killed" tempo=context deadline exceeded` (`diagnosi-cpu-1.txt`). La foto
  vuole 2,5 s di CPU; il limite di CPU (8 s) è lontano, i 10 s di orologio di `TempoMassimo` no.
  Nella CI i pacchetti dei test girano insieme su 4 CPU (`internal/imaging` fa lo stesso tipo di
  foto per 67 s), quindi il figlio ha avuto meno di un terzo di CPU.
- **Il limite dei dati (RLIMIT_DATA) non c'entra.** Tetto minimo con cui ogni foto passa, per
  bisezione con il Figlio vero e il tetto abbassato (`prova-figlio-N`), ripetuto
  (`diagnosi-tetto-minimo.txt`):

  | Foto | 24 CPU (3 giri) | 4 CPU, GOMAXPROCS 4 come la CI (2 giri) | CPU 25% (2 giri) |
  | --- | --- | --- | --- |
  | progressivo CMYK 8640×6480 | 1312–1315 MiB | 1312 | 1312 |
  | progressivo 4:4:4 8944×6708 | 872–875 | 872–875 | 872–875 |
  | PNG RGB 16 bit 7000×8500 | 528–536 | 528–536 | 528–533 |
  | JPEG 8944×6708 | ≤ 202 | ≤ 202 | ≤ 202 |

  Il tetto è 1440 MiB: al peggio 125 MiB (9 %) di margine, uguale con poca CPU e con 4 processori.
  Ogni giro sotto il tetto è finito con l'uscita 2 del runtime («out of memory»), mai con un
  segnale (116 su 116). 50 giri del test di oggi a CPU piena: tutti verdi, al più 2,41 s di orologio,
  2,48 s di CPU e 1353 MiB di RSS per la foto più pesante (`diagnosi-ripetuto-50-riassunto.txt`).
- **Perché il messaggio diceva «immagine non valida»:** `esegui` trasformava ogni errore del
  figlio in `ErrImmagine`, anche il SIGKILL del tempo, l'uscita 2 della memoria e un figlio che non
  parte.

## Cosa c'è

- **Errore nuovo** `imaging.ErrRisorse` («imaging: elaborazione non riuscita adesso»): il figlio non
  ha elaborato la foto adesso, senza rifiutarla. L'errore è un `*imaging.ErroreRisorse` con la
  causa (`imaging.CausaDi`) e un dettaglio senza dati della foto né percorsi:
  - `tempo`: fermato dal padre dopo `Lavoratore.Tempo` (10 s): il contesto del figlio è scaduto
    (`context.DeadlineExceeded`) mentre quello del chiamante vale ancora, e il figlio non è uscito
    da sé con un codice (un rifiuto arrivato alla scadenza resta un rifiuto);
  - `cpu`: ucciso dal kernel al limite di CPU (SIGKILL dopo almeno 8 s di CPU, meno 100 ms per la
    differenza misurata fra il conto del kernel e la rusage);
  - `segnale`: ucciso da un altro segnale (per esempio il killer della memoria del kernel);
  - `memoria`: uscita 2 del runtime di Go, di solito la memoria negata («out of memory»); non
    sempre: anche un altro errore fatale del runtime (un thread che non si crea, un panic in
    un'altra goroutine) esce con 2;
  - `avvio`: il figlio non parte (binario che non c'è, percorso relativo) o non si dà i limiti;
  - `uscita`: un codice fuori dal protocollo.
  Il contesto annullato resta l'errore del contesto; un rifiuto vero resta `ErrImmagine` (anche un
  risultato vuoto o oltre il massimo).
- **Protocollo d'uscita del figlio non ambiguo:** 0 riuscita, 3 rifiuto, 4 limiti non messi, 5
  operazione sconosciuta (`imaging.Uscita…`). Mai 1 o 2: 2 è del runtime. Un panic della
  conversione si recupera ed è un rifiuto (come un'eccezione di Pillow), così l'uscita 2 è di solito
  la memoria. Lo standard error del figlio resta chiuso.
- **Cosa vede l'utente:** `buste.MsgFotoNonElaborata` = «Foto non elaborata adesso: riprova tra
  poco.», per le pagine di caricamento della 038 e della 039 al posto di «Foto rifiutata o non
  salvata.» / «Foto rifiutata.». `AllegaFoto` dà già l'errore del normalizzatore: ora è
  `ErrRisorse` e nessuna riga si scrive.
- **Miniature:** una miniatura non fatta per le risorse ripiega sulla foto intera come prima, ma
  non è `media.ErrMiniatura` (foto guasta): l'errore resta `ErrRisorse`, `Prepara` dà
  `PreparazioneRimandata`, la cache non scrive niente e la volta dopo la rifà. Nel registro di
  `serve` (`Miniature.Registro`): `level=WARN msg="miniatura non fatta adesso" causa=tempo`; una foto
  rifiutata solo in debug; mai la chiave né un percorso.
- **Test stabili:** i test che fanno girare le foto grandi nel figlio vero (`cmd/gestrip` e
  `internal/imaging`) danno al lavoratore un tempo di 2 minuti (`tempoFotoGrandi`): provano la
  memoria e la CPU, che non dipendono dal carico; i 10 s di orologio li prova
  `TestFiglioUccisoDopoIlTempo`. Un errore scrive la causa. Il margine verso i 10 s lo guarda la
  **CPU del figlio** (rusage, che non dipende dal carico): nei test delle foto grandi di
  `internal/imaging` ogni foto deve stare entro 5 s di CPU (`cpuMassimaFotoGrande`; misurato al più
  2,58 s, vedi sotto). La rusage si legge avvolgendo `eseguiFiglio` nei test: nessuna API nuova.
  Il test del binario intero decodifica tutto il risultato e ne controlla dimensioni e assenza di
  EXIF. Tetto di memoria, limite di CPU e `TempoMassimo` invariati.
- **Regola** in `docs/SVILUPPO.md` («Processo figlio delle foto»).

## Differenze dichiarate

- **Da registrare come D-xx (decisione di Stefano):** nello standalone un figlio fermato dal
  timeout di 10 s o caduto per i limiti di memoria e CPU dello script esce come rifiuto della foto
  (`:invalid_image`: «Foto rifiutata o non salvata.», 422 «Foto rifiutata.»; per la miniatura
  `:thumbnail_failed`). Nel Go è `ErrRisorse`: all'utente «Foto non elaborata adesso: riprova tra
  poco.», nel registro la causa; la miniatura ripiega sulla foto intera come prima.
- Anche la memoria negata e il limite di CPU danno «riprova», non «rifiutata»: con le foto che si
  accettano (fino a 60 MP, controllati prima di decodificare) i limiti bastano con margine
  (misurato sopra), quindi succede solo se la macchina è in difficoltà o con un difetto del
  decoder.

## Prove

- **TDD:** rosso misurato sugli stub prima del codice (`ci-instabile/prove/rosso-1.txt`,
  `rosso-2.txt`): 12 test rossi (imaging 9: memoria, CPU, tempo, limiti veri, codici d'uscita,
  panic, uscite, figlio che non parte, figlio finto che fallisce; media 1; buste 1; cmd 1). Il caso
  `prova-senza-limiti`, `prova-panico` nel figlio vero e il testo del tempo sono venuti dalle
  mutazioni vive: il loro rosso è la mutazione uccisa.
- **Test nuovi:** `TestCodiciDUscitaDelFiglio`, `TestPanicDellaConversioneERifiuto`,
  `TestUsciteDelFiglioComeErrori`, `TestFiglioCheNonParte` (imaging),
  `TestMiniaturaRimandataPerLeRisorse` (media), `TestAllegaFotoSenzaRisorseNonERifiuto`,
  `TestMiniaturaSenzaRisorseRipiegaSullaFoto` (buste). Cambiati: `TestFiglioOltreLaMemoria`,
  `TestFiglioOltreLaCPU`, `TestFiglioUccisoDopoIlTempo`, `TestFiglioVeroSiDaILimiti` (ora la causa,
  non `ErrImmagine`), `TestFiglioOperazioneSconosciuta`, `TestAttesaDelPostoRispettaIlContesto`,
  `TestLavoratoreFotoGrandiNeiLimiti`, `TestLavoratoreFotoCheLoStandaloneAccetta`,
  `TestImageWorkerConIlBinarioIntero` (tempo dei test e causa), `TestFotoEMiniatureDelServe`
  (registro della cache).
- **Ripetizioni con il codice nuovo** (`ci-instabile/stabile-*.txt`, script `stabile.sh`):
  - `TestImageWorkerConIlBinarioIntero` 50 volte (`-test.count=50`): 50 verdi;
  - lo stesso 10 volte con `CPUQuota=25%`: 10 verdi, 29,4–30,2 s l'uno (prima della 037c falliva
    alla stessa CPU);
  - `TestLavoratoreFoto…` e `TestFiglioVeroSiDaILimiti` 2 volte con `CPUQuota=25%`: verdi
    (`TestLavoratoreFotoCheLoStandaloneAccetta` 45 s per 5 foto, cioè oltre i 10 s per le più
    pesanti: col tempo di produzione sarebbe stato rosso);
  - `go test ./...` come la CI, tutti i pacchetti insieme su 4 CPU (`taskset -c 0-3`), 3 volte: 16
    pacchetti verdi ogni volta.
- **Mutazioni** (`ci-instabile/mutazioni/`): 34 sul codice nuovo, ne compilano 32, uccise 30
  al primo giro. Vive: M18 (il Figlio senza `converti`: nessun test faceva andare in panic la
  conversione nel figlio vero) e M34 (dettaglio del tempo vuoto), non equivalenti, coperte da
  `prova-panico` e dal testo in `TestFiglioUccisoDopoIlTempo` e uccise. Un ramo SIGXCPU è stato
  tolto dal codice prima delle mutazioni (Go ignora SIGXCPU e il kernel, con i limiti uguali,
  manda SIGKILL).
- **Banco:** non rilanciato: nessun comportamento che il banco osserva cambia (il banco non
  ferma il figlio e non lo fa cadere; le risposte di oggi sono le stesse).
- `scripts/check` verde (`ci-instabile/prove/check-2.txt`, senza l'esito del banco: 24 chiuse, 46 parziali, 0 rosse).
- **Banco**: comandi se la sessione principale vuole rilanciarlo comunque:
  `to-be-deleted-after-use/ci-instabile/banco-037b.sh 037c` (cartella da adattare: lo script
  scrive in `slice-037b/`).
- **Solo ragionato:** che nella CI il figlio abbia avuto meno di un terzo di CPU (dalla durata
  del test, 22,4 s contro 6,7 s, e dai pacchetti che girano insieme); che SIGXCPU non arrivi mai
  (con i limiti morbido e duro uguali il kernel manda SIGKILL; misurato solo che il figlio muore con
  SIGKILL).

## Review indipendente

Nessuna ALTA. Mutazioni della review: 42 scritte, 39 compilano, 32 uccise. Vive:
- R35 (tolto il controllo del contesto dopo il figlio: chi annulla riceveva `ErrRisorse`, causa
  tempo) e R37 (il figlio dà 1 byte per le foto grandi), non equivalenti → coperte (sotto) e uccise;
- R17 (scaduto prima di `err == nil`): equivalente, un figlio uscito senza errore non ha un tempo
  scaduto da riportare; dopo la correzione 4 l'ordine conta ancora meno;
- R29 (il Figlio usa `Normalizza` invece di `normalizzaNelFiglio`): equivalente in produzione, la
  variabile esiste solo perché un test faccia andare in panic la conversione nel figlio vero;
- R31 (scrittura del risultato fallita → uscita 0): il padre ha già smesso di leggere (risultato
  oltre il massimo) e dà `ErrImmagine` prima di guardare l'uscita;
- R33 (solo il tempo utente per la CPU): il figlio al limite usa quasi solo tempo utente
  (misurato: 996 ms utente e 3 ms di sistema su 1 s);
- R36 (lettura dello standard input fallita → limiti non messi invece di rifiuto): non si provoca
  senza far morire il padre; resta un rifiuto come prima.

Correzioni (decise dalla sessione principale):
- **MEDIA**, contesto annullato mentre il figlio lavora: `TestAnnullataDuranteIlFiglio` (annulla
  dopo 200 ms con `prova-dormi`, vuole `context.Canceled`). `scaduto` ora non dipende dall'ordine:
  `errors.Is(c.Err(), context.DeadlineExceeded)` e il contesto del chiamante non annullato.
- **MEDIA**, foto grandi del binario intero che controllavano solo `err == nil`: ora il risultato
  si decodifica tutto con `image/jpeg` e si controllano dimensioni (4096×3072, 4096×3072, 3373×4096)
  e assenza di EXIF.
- **BASSA/MEDIA**, margine verso i 10 s: controllo della CPU del figlio (sopra). Misure
  (`ci-instabile/review-cpu-*.txt`), massimo per foto: a CPU piena 10 giri, su 4 processori 3, con
  un quarto di CPU 3. Progressivo CMYK 8640×6480 2,36 / 2,28 / 2,58 s; CMYK di base 8944×6708 2,33 /
  2,32 / 2,58; PNG RGB 16 bit 1,80 / 1,76 / 2,02; progressivo 4:4:4 1,65 / 1,62 / 1,83; le altre
  sotto 1,7 s. Soglia 5 s, circa il doppio del massimo.
- **BASSA**, rifiuto alla scadenza: un figlio uscito da sé con un codice (`ps.Exited()`) ha la
  precedenza sul tempo; `TestUscitaPrimaDelTempo` con stati di figli veri e la scadenza data a
  `esitoDelFiglio` (deterministico).
- **BASSA**, uscita 2 non sempre memoria: scritto nel commento di `CausaMemoria` e qui.
- **BASSA**, `prova-senza-limiti` da root: saltata con un messaggio se `os.Geteuid() == 0`.
- **BASSA**, R29: accettata come equivalente in produzione (sopra).

Prove del giro:
- rosso misurato prima del codice (`ci-instabile/prove/rosso-review-1.txt`):
  `TestUscitaPrimaDelTempo` rosso sul codice di prima (rifiuto e uscita 2 alla scadenza davano
  tempo). `TestAnnullataDuranteIlFiglio` è verde sul codice di prima, rosso su R35; il controllo
  delle dimensioni è rosso su R37 con le foto oltre 400 KiB (R37 della review scattava oltre 10 MiB,
  dove nessuna foto del test arriva); il controllo della CPU è rosso con la soglia a 1 s
  (`rosso-review-2.txt`);
- mutazioni sulle correzioni (`mutazioni/esiti-review.txt`): 8, tutte compilano, 5 uccise. Vive
  C02 e C03 (scaduto senza guardare il contesto del chiamante, o `c.Err() != nil` come prima):
  equivalenti, un contesto del chiamante finito esce prima con il suo errore; C07 (ultima uscita
  dopo i segnali): su Linux un figlio aspettato è uscito o è morto per un segnale, il ramo serve
  solo fuori da Linux;
- ripetizioni: `TestImageWorkerConIlBinarioIntero` 50 volte, 50 verdi; 5 volte con
  `CPUQuota=25%`, 5 verdi, 30,5–31,5 s l'uno (`review-stabile-*.txt`);
- `scripts/check` verde (`ci-instabile/prove/check-review.txt`: 24 chiuse, 46 parziali, 0 rosse).

## Dubbi per Stefano
1. **D-xx da registrare** (sopra, «Differenze dichiarate»): timeout e limiti del figlio come
   «riprova» invece che come rifiuto. **Raccomandazione:** approvarla.
2. **Memoria negata e limite di CPU** danno anche loro «riprova», anche se per la stessa foto si
   ripeterebbero: con le foto accettate i limiti bastano con margine, quindi succede solo per una
   macchina in difficoltà o un difetto. **Raccomandazione:** tenerlo così (il registro dice la causa).
3. **Codice HTTP della 039** per `ErrRisorse` (lo standalone dà 422 «Foto rifiutata.» per tutto):
   **raccomandazione** 503 con `MsgFotoNonElaborata`; da decidere nella 039.
4. **10 s di orologio sull'apparecchio:** la foto più pesante vuole 2,5 s di CPU; con due figli e
   il negozio che lavora ci sta, ma va misurato alla T5 insieme alla RAM.
5. Il riscaldatore non rimette in coda una miniatura rimandata: la fa la prima pagina che la chiede.
