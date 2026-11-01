# 037e — CPU del figlio delle foto nella CI

- **Fonte:** CI rossa del commit `6930028` (run 41305328853, job 124844438404) dopo la 037c;
  decisione di Stefano del 01/11/2026: limiti di produzione invariati, limite di CPU più alto solo
  nei test della CI attraverso una variabile, misura vera sull'apparecchio alla T5 (già nel PIANO).
- **Voci:** toccata RIP-S-30 (limiti del figlio). Nessuna cambia stato.
- **Standalone:** non letto: i limiti di produzione (8 s di CPU, 10 s di orologio) non cambiano.
- **Lavoro:** implementazione, misure e mutazioni di un sottoagente; review stretta (6 mutazioni
  mirate) di un sottoagente diverso, per contenere l'uso; `scripts/check` rilanciato dalla
  sessione principale, verde. Decisione di Stefano del 01/11/2026: opzione 1 con le due aggiunte.

## Causa (dal log della CI)

Sul runner di GitHub (4 vCPU, i pacchetti dei test insieme) il figlio usa molta più CPU che su
pc-fisso (al più 2,58 s nella 037c), con l'orologio quasi uguale alla CPU:

| Foto | CPU del figlio sul runner |
| --- | --- |
| progressivo CMYK 8640×6480 | 7,92 s (nel binario intero ucciso al limite: causa `cpu`, dopo 8,65 s) |
| CMYK di base 8944×6708 | 6,13 s |
| progressivo 4:4:4 8944×6708 | 5,62 s |
| PNG RGB 16 bit 7000×8500 | 4,75 s |
| PNG RGBA 16 bit 7000×6000 | 2,83 s |

Cadevano il controllo «al più 5 s» (`cpuMassimaFotoGrande`) di
`TestLavoratoreFotoCheLoStandaloneAccetta` e `TestImageWorkerConIlBinarioIntero` (limite di 8 s).

## Cosa c'è

- **Limite di CPU dal padre:** `imaging.Lavoratore.CPU` (secondi; 0 = `LimitiDelFiglio.CPU`, 8 s).
  Il padre lo manda sempre come argomento: `gestrip image-worker <operazione> <secondi>`. Il figlio
  (`limitiDagliArgomenti`) accetta solo cifre decimali da `MinCPUFiglio` (1) a `MaxCPUFiglio` (60);
  senza argomento usa 8 s; altro, o più argomenti, è `UscitaOperazione` prima di darsi i limiti e
  di leggere la foto (per il padre `ErrRisorse`, causa `uscita`). La memoria resta sempre quella di
  `LimitiDelFiglio`. La causa `cpu` si decide con il limite mandato, non più con quello globale.
- **Perché la produzione non si può cambiare:** `serve` crea il lavoratore con `NuovoLavoratore`,
  che lascia `CPU` a 0; nessun file né variabile d'ambiente lo legge (il codice del pacchetto non
  legge l'ambiente: `TestLimitiSenzaAmbiente`); il figlio parte con l'ambiente vuoto; un valore
  fuori da 1…60 il figlio lo rifiuta. Cambiarlo vuol dire cambiare il codice.
- **Nei test:** `GESTRIP_TEST_CPU_FIGLIO` (secondi), letta solo dal codice dei test
  (`cpuFiglioDeiTest` in `internal/imaging` e `cmd/gestrip`), alza il limite dei test delle foto
  grandi e del binario intero; un valore fuori da 1…60 ferma il test. Il tempo d'orologio dei test
  resta `tempoFotoGrandi` (2 minuti, 037c).
- **Valore della CI: 30 s** (`.github/workflows/ci.yml`): il massimo misurato sul runner è 7,92 s,
  e una volta oltre 8 s; 30 s sono quasi 4 volte, restano sotto i 2 minuti d'orologio dei test e
  sotto i 60 s che il figlio accetta.
- **Soglia di 5 s (`cpuMassimaFotoGrande`):** dipende dalla macchina; vale solo dove è tarata
  (pc-fisso, `scripts/check`, che non mette la variabile). Con la variabile (la CI) non si
  controlla, ma la CPU di ogni foto si scrive sempre nel log (`controllaCPU`), così ogni giro della
  CI registra la CPU sul runner.
- **Regola** aggiornata in `docs/SVILUPPO.md` («Processo figlio delle foto»).

## Differenze dichiarate

Nessuna nuova: in produzione 8 s di CPU, memoria dell'avvio più 1440 MiB, 10 s di orologio, come
prima. Solo la CI fa girare i test delle foto grandi con 30 s di CPU.

## Prove

- **Rosso misurato prima del codice** (`slice-037e/prove/rosso-1.txt`, stub con il campo e le
  costanti, argomenti ignorati): rossi `TestFiglioConILimiti` (limite 8 con `CPU` 1, 30, 60),
  `TestFiglioVeroSiDaILimiti` (la foto passa con 1 s di CPU), `TestLimiteDiCPUDalPadre` (valori
  sbagliati accettati, `CPU` 61 accettato). Verdi sullo stub (il loro rosso è una mutazione):
  `TestFiglioOltreLaCPU`, `TestLimitiSenzaAmbiente`, il controllo di `serve` in
  `TestFotoEMiniatureDelServe`.
- **Test nuovi:** `TestLimiteDiCPUDalPadre`, `TestLimitiSenzaAmbiente`. Cambiati:
  `TestFiglioConILimiti` (0 → 8, 1, 30, 60 nel figlio, con la variabile dei test nell'ambiente),
  `TestFiglioOltreLaCPU` (1 s mandato dal padre, senza toccare `LimitiDelFiglio`),
  `TestFiglioVeroSiDaILimiti` (Figlio vero con 1 s: causa `cpu`), `TestUscitaPrimaDelTempo`,
  `TestLavoratoreFotoGrandiNeiLimiti`, `TestLavoratoreFotoCheLoStandaloneAccetta` (soglia solo
  fuori dalla CI, CPU nel log), `TestImageWorkerConIlBinarioIntero` (limite dalla variabile),
  `TestFotoEMiniatureDelServe` (`serve`: `CPU` 0, cioè 8 s, e tempo 0, anche con la variabile).
- `scripts/check` verde (`prove/check-1.txt`, e `prove/check-2.txt` dopo i valori aggiunti per M19).
- Con `CPUQuota=25%` (`prove/cpu25.txt`): test delle foto grandi, del Figlio vero e del binario
  intero verdi; CPU del figlio al più 2,77 s (progressivo CMYK), soglia di 5 s controllata.
- **CI simulata** (`prove/ci-sim-*.txt`): `GESTRIP_TEST_CPU_FIGLIO=30`, `go test ./...` con tutti
  i pacchetti insieme su 4 CPU (`taskset -c 0-3`), 3 giri: 16 pacchetti verdi ogni volta. CPU del figlio (nel log, soglia non controllata) al più
  2,69 s (progressivo CMYK): su pc-fisso la contesa non la alza come sul runner.
- **Mutazioni** (`slice-037e/mutazioni/esiti.txt`, `esiti-2.txt`): 21 scritte, 20
  compilano, 18 uccise al primo giro. Vive: M19 (il figlio legge il numero in base 0: accetta
  `0x10`, `1_0`), non equivalente, coperta aggiungendo quei valori a `TestLimiteDiCPUDalPadre` e
  uccisa; M20 (`SetMemoryLimit` con la memoria di `LimitiDelFiglio` invece di quella dei limiti
  passati): equivalente, `limitiDagliArgomenti` copia sempre la memoria di `LimitiDelFiglio`. M10
  (il Figlio si dà `LimitiDelFiglio` invece dei limiti del padre) non compilava; riscritta in modo
  che compili (M10b) e uccisa da `TestFiglioVeroSiDaILimiti`.
- **Banco:** non rilanciato: nessun comportamento osservabile dal banco cambia.
- **Solo ragionato:** perché il runner usa più CPU (processori più lenti e condivisi; sul runner
  l'orologio è quasi uguale alla CPU, quindi non è attesa); che 30 s bastino su ogni runner (misurato
  un solo giro della CI); il ramo che salta la soglia con la variabile non si può far diventare
  rosso su pc-fisso (la CPU vi resta sotto 5 s).

## Review indipendente

Nessuna ALTA. Verificato: un solo costruttore in produzione (`NuovoLavoratore`, 8 s), argomento del
figlio controllato prima dei limiti e della foto (solo cifre, 1…60), variabile letta solo dai
`_test.go`, causa «cpu» dal limite mandato davvero.
- **MEDIA:** la variabile stava nell'`env` del job (arrivava anche a vet, build, copertura,
  gitleaks) → spostata nell'`env` del solo passo `test`, che vale anche per il secondo lancio di
  `go test` dopo un errore. Corretta dalla sessione principale.
- **BASSA:** `TestLimitiSenzaAmbiente` copre solo `internal/imaging`; `serve` è coperto da
  `TestFotoEMiniatureDelServe` con la variabile impostata (mutazione presa). Lasciata.
- **BASSA:** `gestrip image-worker normalizza 60` lanciato a mano accetta 60 s: non è un percorso
  di `serve`, che costruisce gli argomenti dal codice. Lasciata.
- **Mutazioni della review:** 6 su 6 prese (variabile in `NuovoLavoratore` e in `serve`, argomento
  ignorato, causa con 8 s fissi, tetto a 600, argomento sbagliato accettato).

## Dubbi per Stefano

1. **La CI non guarda più il margine di CPU:** su GitHub la regressione si vede solo leggendo il log
   (`CPU del figlio`). Su pc-fisso `scripts/check` la controlla a ogni slice. **Raccomandazione:**
   così, la misura vera è alla T5.
2. **Margine di produzione sul runner:** la foto più pesante ha usato 7,92 s di CPU sul runner,
   vicina agli 8 s. Se l'apparecchio è lento come il runner, la foto più pesante dello standalone
   cadrebbe (causa `cpu`, «riprova»). Già nel PIANO alla T5 (domanda 6 della 037c).
3. `TestFiglioOltreLaCPU` vuole il figlio caduto entro 8 s d'orologio: con `CPUQuota=25%` e i due
   pacchetti insieme ne ha messi 7,81 (037c, invariato). Se la CI ci inciampa, alzarlo.
