# 051b — I test che avviano processi figli non lasciano più orfani

- **Fonte:** coordinatore, 05/11/2026. Su pc-fisso giravano dal 30/10 sera due processi
  `gestrip.test serve -senza-volume-cifrato -db /tmp/TestServeProcessoNonScaricabile…/001/gestrip.db
  -interna 127.0.0.1:0 …`, con PPID 1, stato `SNl`, circa il 110% di CPU ciascuno per sei giorni.
  Ignoravano SIGTERM e sono stati chiusi con SIGKILL.
- **Voci:** nessuna nuova. I test toccati restano sulle loro voci (OPS-13, ADM-20, OPS-07, OPS-10,
  RIP-S-30).
- **Lavoro:** diagnosi, implementazione e mutazioni di un sottoagente. Commit e documenti privati
  li fa la sessione principale.

## La causa

**Trovata, ed era un mutante, non il prodotto.** Il journal dell'utente su pc-fisso ha i due scope
(`run-u83779.scope` e `run-u83871.scope`): li ha aperti `scripts/limiti.sh` il 30/10 alle 19:53:02 e
alle 19:54:25. Il comando era `go test -count=1 -timeout=60s ./internal/audit/ ./internal/clock/
./cmd/gestrip/`, dopo un `go vet` su `./internal/audit/` o `./internal/clock/`. Sono le mutazioni
della slice 016 (la conservazione dell'audit, commit delle 20:10). Il 05/11 alle 14:10 il journal
registra «Consumed 6d 12h 21min» e «6d 11h 48min» di CPU.

La catena, riprodotta (`prove/mutante-016-test-vecchio.log`):

1. Un mutante come `if !aspetta(ctx)` → `if aspetta(ctx)` in `Conservazione.Lavoro`, o
   `clock.Aspetta` che a contesto finito dà `true`, fa girare il lavoro a vuoto dopo lo stop: il
   giro torna subito con l'errore del contesto, l'attesa torna subito e si ricomincia.
   - Il ciclo che gira a vuoto spiega il 110% di CPU.
   - `serve` resta fermo in `lavori.Wait()`.
2. Lo stesso `serve` ha ancora `signal.NotifyContext` attivo (lo `stop()` è nel `defer` di `main`):
   ogni SIGTERM successivo viene preso e buttato. È il «SIGTERM ignorato» (`SigCgt` ha il bit 15).
3. Il test aspettava il figlio senza limite (`<-resto` dopo `fermato = true`). Il `-timeout=60s` di
   go test fa uscire il processo dei test con un panic, **senza i `t.Cleanup`**, e il figlio passa
   a init.

Il mutante copiato fuori dal modulo, con il test di prima e `-timeout=30s`, ha lasciato
esattamente un orfano così (`prove/mutante-016-test-vecchio-ps.log`, con `ps` e `SigCgt`): PPID 1,
`SNl`, 121% di CPU, ancora vivo e allo stesso 121% 3 s dopo un SIGTERM. È stato ucciso a mano con
SIGKILL.

**Il codice di produzione di oggi non ha il difetto.** I tre lavori di `serve` escono a contesto
finito:
- `Conservazione.Lavoro` e `Pulizia.Lavoro` escono quando `aspetta` dà `false`, e `clock.Aspetta` dà
  `false` su `ctx.Done()`;
- `Riscaldatore.Lavoro` esce su `ctx.Done()` e dopo ogni miniatura a contesto finito.

Prove a mano sul binario dei test che fa da `serve`, con `-comandi-stampa` su una cartella vuota
(`script/sigterm.sh`, `script/pipa.sh`):
- SIGTERM dopo «in ascolto», e a 0,01, 0,05, 0,1, 0,2, 0,3, 0,5 e 1 s dall'avvio;
- 18 lanci in parallelo dentro `scripts/limiti.sh`, e due SIGTERM a 10 ms;
- in tutti i casi `serve` esce in meno di 10 s. Esce con 0, oppure con 143 se il segnale arriva prima
  di `NotifyContext`, quando il database non è ancora aperto;
- stderr chiuso (`2>&-`): SIGTERM, uscita 0;
- stderr su una pipe il cui lettore muore dopo «in ascolto»: `serve` muore subito di SIGPIPE (141)
  alla prima scrittura del registro, anche con SIGPIPE ignorato dal padre. Non gira a vuoto e non
  si blocca.

Resta una fragilità di progetto, già vera ma non un difetto: vedi i «Dubbi».

## Cosa c'è

### `internal/figlitest` (nuovo, solo per i test)

Il binario di gestrip non lo importa. `Avvia(t, cmd)` e `AvviaEntro(t, cmd, limite)` danno un
`*figlitest.Figlio`, che incorpora `*exec.Cmd` e ha un suo `Wait`, una volta sola.

- **Gruppo e Pdeathsig (Linux):**
  - `Setpgid` mette il figlio in un gruppo di processi suo;
  - con `Pdeathsig: SIGKILL` il kernel uccide il figlio quando muore il processo dei test, anche per
    il panic del timeout, che non fa girare i Cleanup.
- **Il thread:** Pdeathsig scatta alla morte del *thread* che ha fatto fork, non del processo.
  - Il figlio parte da una goroutine con `runtime.LockOSThread` che resta lì fino al Cleanup: il
    runtime non può chiudere quel thread mentre il figlio serve.
  - Il runtime chiude un thread quando ci finisce sopra una goroutine bloccata che esce: raro, ma
    il figlio morirebbe a metà test.
  - Nel Cleanup, a figlio già ucciso e raccolto, la goroutine sblocca il thread e lo restituisce.
    Prima lo lasciavo chiudere, ma una prova (30 figli) ha mostrato che 1 volta su 30 il thread non
    si chiude (probabilmente era il thread principale, che il runtime non termina). Non c'era quindi
    da contarci.
- **Limite di vita:**
  - è il più piccolo fra 60 s (`LimiteMassimo`) e il tempo che resta prima del timeout di go test
    meno 10 s, ma almeno metà di quel tempo (`limiteDi`, da `t.Deadline()`);
  - passato il limite, SIGKILL al gruppo: chi legge stderr o aspetta `Wait` si sblocca, il test
    fallisce con «non è uscito entro il suo limite di … ed è stato ucciso con SIGKILL», e i Cleanup
    girano.
- **Cleanup, sempre** (test passato o fallito):
  - SIGKILL al gruppo e al figlio;
  - `Wait` entro 10 s;
  - poi `kill(pid, 0)` e `kill(-pgid, 0)` devono dare ESRCH entro 10 s, con i nipoti compresi (un
    nipote ucciso resta zombie finché init non lo raccoglie). Altrimenti il test fallisce.
- Fuori da Linux (solo sviluppo) non ci sono né gruppo né Pdeathsig: restano il limite e il Cleanup
  sul solo figlio.

### Test che avviano figli, rivisti

- `cmd/gestrip/figlio_linux_test.go`:
  - `figlio`/`figlioCon` avviano con `figlitest.Avvia`. Lo usano tutti i test del file, quelli di
    `import_linux_test.go`, `import_rami_linux_test.go` e `terminale_linux_test.go`;
  - `TestServeProcessoNonScaricabile` ora usa `figlio` (niente più `exec.Command` suo, `fermato` e
    `<-resto` senza limite);
  - nuovo `aspettaFine`: dopo il segnale legge il resto di stderr e aspetta l'uscita entro 30 s
    (`tempoDelloStop`: 10 s di Shutdown e 2 s di checkpoint in `serve`). Altrimenti dà SIGKILL al
    gruppo e un errore che lo dice. Lo usano i test che fermano `serve` o `import` con SIGTERM o
    SIGINT: un `serve` che non si ferma lo ferma questo tetto, e il limite di vita resta dietro
    quando il timeout di go test è più corto. Gli altri test sono coperti dal limite di vita.
- `cmd/gestrip/image_worker_linux_test.go` e `internal/imaging/lavoratore_test.go`: i tre
  `exec.Command(os.Args[0], ComandoFiglio, …)` con `Run`/`Output` passano da `figlitest.Avvia`.
- Restano come sono, perché sono già sicuri:
  - i figli avviati da `imaging.Lavoratore` (produzione, anche nei test di `cmd/gestrip` e
    `internal/imaging`): Pdeathsig, `LockOSThread` da Start a Wait, il tempo massimo e
    `WaitDelay`;
  - `tools/banco` (docker, git): fuori dai test.
- `internal/printing/cups_test.go` (righe 125, 214 e 311): i comandi finti partono da `esegui` di
  produzione, con un gruppo loro e il kill del gruppo al tempo scaduto, ma **senza Pdeathsig**; uno
  ha anche un nipote. Se il processo dei test morisse a metà, resterebbero vivi. `esegui` non l'ho
  toccato. I loro `sleep 30` ora sono `sleep 5`: più dei tempi massimi dei test (200-300 ms), della
  `WaitDelay` di 2 s e dei 3 s di attesa del nipote, quindi un orfano vive al più 5 s.
- Nessun altro `exec.Command`, `os.Args[0]`, `os.Executable` o `StartProcess` avvia figli nei test
  (cercati in tutto il repo).

## Prove

- `internal/figlitest/figlitest_linux_test.go`:
  - `TestCleanupTogliFiglioENipote`: un figlio che ignora SIGTERM avvia un nipote nel suo gruppo;
    dopo il Cleanup del sottotest tutti e due danno ESRCH, senza che il test chiami Wait;
  - `TestLimiteDiVita`: un figlio sordo a SIGTERM e un limite di 2 s. `Wait` torna con SIGKILL,
    `Scaduto()` è vero e il Cleanup dà l'errore del limite (con un `testing.TB` finto che raccoglie
    gli errori);
  - `TestFiglioCheEsce`: nessun errore e il limite non scatta;
  - `TestLimiteDi`: 0, 10 min, 60 s e 12 s di tempo rimasto, e nessuna `Deadline`;
  - `TestLimiteDopoWait` (dalla review): `/bin/true` con un limite di 300 ms, `Wait`, poi 700 ms di
    attesa. Il limite non scatta e il Cleanup non dà errori;
  - `TestAspettaMorti` (dalla review): su un figlio fermato con SIGSTOP `aspettaMorti` dà errore dopo
    300 ms; ucciso e raccolto, dà nil;
  - `TestPdeathsigSenzaCleanup`: è il caso del 30/10. Un processo intermedio (il binario dei test con
    `-test.timeout=10s`) avvia un figlio sordo con un limite di un'ora e si blocca. Il panic del
    timeout lo fa uscire senza Cleanup, e il figlio deve sparire entro 5 s.
- Il mutante della 016 con il test nuovo (`prove/mutante-016-test-nuovo-*.log`):
  - con `-timeout=60s`, `chiusura con SIGTERM: il figlio non è uscito entro 30s (ucciso con
    SIGKILL…)`;
  - con `-timeout=30s`, l'errore del limite di vita (20 s);
  - nessun processo rimasto.
- `go test -count=3 ./cmd/gestrip/ ./internal/imaging/ ./internal/figlitest/` dentro
  `scripts/limiti.sh`: verde (80 s, 92 s, 11 s). Poi `pgrep -af 'gestrip.test|imaging.test|
  figlitest.test'` vuoto.
- `scripts/check` verde (`prove/check-2.log`, dopo il primo giro). Il primo giro era rosso per
  `TestNessunOrologioFuoriDaClock` (JOB-01): `figlitest` usava `time.Now`, `time.Until` e
  `time.After`. Ora passa da `clock.Sistema{}` e `clock.Aspetta`; `time.AfterFunc` è permesso.
- `-count=3` rifatto sul codice definitivo: verde (82 s, 92 s, 11 s), nessun processo rimasto.

## Mutazioni

Ogni mutazione gira su una copia pulita del repo fuori dal modulo, con i binari dei test in un
`GOTMPDIR` suo, così `pgrep` trova solo i processi di quella copia. Dopo ogni giro si controlla che
non resti niente (e lo si uccide se resta). 16 mutazioni, tutte con l'esito atteso. m01-m14 vengono
dal giro finale prima della review (`mutazioni/giro-3-finale.txt`); m15 e m16 sono rifatte dopo le
correzioni della review, tre e due volte (`mutazioni/giro-5-review.txt`):

| # | Mutazione | Test | Esito |
|---|---|---|---|
| m01 | mutante 016 (Lavoro a vuoto dopo lo stop) | `TestServeProcessoNonScaricabile`, `-timeout=60s` | rosso con messaggio, nessun orfano |
| m02 | m01 + limite di vita oltre il timeout | idem, `-timeout=20s` | panic del timeout, nessun orfano (Pdeathsig) |
| m03 | m02 senza Pdeathsig | idem | panic, **1 orfano** (atteso: prova che serve Pdeathsig) |
| m04 | m02 senza `LockOSThread` | idem | panic, nessun orfano (vedi Dubbi) |
| m05 | test bloccato (`time.Sleep(time.Hour)`) dopo SIGTERM, limite oltre il timeout | idem | panic, nessun orfano |
| m06 | Cleanup senza SIGKILL | `./internal/figlitest/` | rosso |
| m07 | senza `Setpgid` | idem | rosso (nipote vivo) |
| m08 | senza Pdeathsig | idem | rosso (`TestPdeathsigSenzaCleanup`) |
| m09 | il limite non uccide | idem | rosso (timeout, nessun orfano) |
| m10 | limite scaduto taciuto | idem | rosso |
| m11 | limite senza il margine di 10 s | idem | rosso |
| m12 | limite senza «almeno metà» | idem | rosso |
| m13 | limite senza il tetto di 60 s | idem | rosso |
| m14 | SIGKILL al solo figlio, non al gruppo | idem | rosso (nipote vivo) |
| m15 | controllo ESRCH sempre buono | idem | rosso 3 volte su 3 (`TestAspettaMorti`) |
| m16 | `Wait` non ferma il limite (né `finito` né `timer.Stop`) | idem | rosso 2 volte su 2 (`TestLimiteDopoWait`) |

Prima della review m15 era presa solo 4 volte su 6 (3 su 5 sul codice di allora): dipendeva da
quando init raccoglieva il nipote zombie. `TestAspettaMorti` la prende sempre. In m16 ognuna delle due
metà da sola basta: `timer.Stop` ferma il limite, e `finito` copre un callback già partito che
`Stop` non può più fermare. Tolta una sola delle due, i test restano verdi.

m06 e m08 sono rimasti verdi al primo giro e hanno portato due correzioni ai test. La rete di
sicurezza del test (`uccidiAMano`) girava prima del Cleanup di `Avvia` e lo copriva: ora sta sul
test di fuori. Il processo intermedio di `TestPdeathsigSenzaCleanup` usava il limite di vita
normale, che uccideva il figlio prima del timeout: ora ha un'ora.

## Per il manuale

Niente per il manuale: solo test. Il comportamento di `serve` non cambia.

## Dubbi

- **Lo stop di `serve` non ha un tetto, e dopo il primo SIGTERM gli altri si perdono.** Oggi non è
  un difetto: i tre lavori escono a contesto finito. Un lavoro futuro che non lo facesse, però,
  terrebbe `serve` fermo in `lavori.Wait()` per sempre. Con systemd arriva comunque SIGKILL dopo
  `TimeoutStopSec` (90 s di default; l'unità vera è in T5). Due correzioni possibili, da decidere
  nella sessione principale perché cambiano il comportamento di produzione:
  - `stop()` appena il contesto finisce (`context.AfterFunc(ctx, stop)`): il secondo SIGTERM o
    Ctrl-C uccide subito, come di solito nei programmi Go;
  - un tempo massimo anche per `lavori.Wait()`, con una riga nel registro.

  Non le ho applicate.
- **`LockOSThread` non si può provare con un test.** m04 resta senza orfani perché quando il
  processo muore muoiono tutti i thread. Il blocco protegge da un'altra cosa: un figlio ucciso a
  metà test, perché il runtime ha chiuso il thread da cui era partito. Succede solo se sullo stesso
  thread finisce un'altra goroutine bloccata. È la stessa scelta di `imaging.Lavoratore`.
- **Lavoratore delle foto (produzione):** già a posto (Pdeathsig SIGKILL, `LockOSThread` da Start a
  Wait, tempo massimo con Kill, `WaitDelay`). Il figlio `image-worker` non avvia nipoti, quindi il
  kill del gruppo non serve. Nessuna modifica.
- **Stampa (`internal/printing`, produzione):** `lp`, `lpstat` e `lpoptions` hanno un gruppo loro e
  il kill del gruppo al tempo scaduto, ma non Pdeathsig. Se `serve` morisse di SIGKILL durante una
  stampa, il comando finirebbe da solo (dura poco), e con systemd `KillMode=control-group` lo
  ucciderebbe comunque. Lo segnalo solo.
- In `/tmp` restano 7 cartelle `TestServe…` del 30/10 e del 03/11 (`TestServeMigraPrimaDiAscoltare`
  ×4, `TestServeRifiutaLAscoltoNonLocale`, `TestServeControllaLAscoltoVero`,
  `TestServeAttesaPredefinita`). Sono resti di test interrotti prima di questa slice; non le ho
  toccate perché non sono mie. Si possono togliere.
- **Il limite usa l'ora del muro.** `clock.Sistema.Adesso()` fa `UTC()`, e così perde la lettura
  monotona: un salto dell'orologio (timesyncd all'avvio dell'N150) può accorciare o allungare il
  limite di vita e le attese di `aspettaMorti`. Proposta: una lettura monotona in `internal/clock`,
  oppure un'eccezione per `figlitest` nella guardia JOB-01. `time.AfterFunc`, che fa scattare il
  limite, è già monotono. Non applicata.
- **`scripts/limiti.sh` usa `systemd-run --scope`**, e lo scope resta vivo finché ci resta dentro un
  processo: così i due orfani sono durati sei giorni nei loro scope. `systemd-run --user --wait
  --pipe --collect`, come servizio, ucciderebbe tutto il gruppo all'uscita, ma va provato con stdin
  e TTY. Non applicato.
- `LimiteMassimo` di 60 s: nessun figlio dei test avviato con `figlitest` dura tanto oggi (le foto
  grandi passano dal Lavoratore, con i loro 2 minuti). Un test futuro più lungo deve usare
  `AvviaEntro`.

## Review indipendente

Un difetto medio e alcuni bassi, tutti corretti o scritti qui:
- **MEDIA, il limite di vita scattava anche su un figlio già uscito e raccolto.** Ne veniva un falso
  errore nel Cleanup e un `kill(-pid)` su un pid già raccolto. Corretto: `Wait`, dopo `Cmd.Wait`,
  mette `finito` e ferma il timer sotto il lucchetto, e il callback uccide solo se `!chiuso &&
  !finito`. Il test è `TestLimiteDopoWait`, la mutazione m16.
- **BASSA, `aspettaMorti` senza un test affidabile** (m15 instabile): aggiunto `TestAspettaMorti`
  (SIGSTOP, poi SIGKILL e raccolta). m15 ora è presa sempre.
- **BASSA, tempi corti:** `TestLimiteDiVita` ora ha 2 s, `TestPdeathsigSenzaCleanup` usa
  `-test.timeout=10s`.
- **BASSA, ricevuta e commenti:**
  - i test della stampa sono ora nella ricevuta, con gli `sleep` a 5 s;
  - la documentazione di `figlitest` ora dice che un nipote con un gruppo suo e i nipoti in
    generale sfuggono a Pdeathsig;
  - il numero di m15 è corretto;
  - la prova del mutante della 016 è rifatta con `ps` nel log;
  - il commento prima di `aspettaFine` dice che arriva prima il tetto di 30 s.
- **Nei Dubbi, senza applicarli:** l'ora del muro nel limite, lo scope di `limiti.sh`, lo stop di
  `serve`.

## Ricevuta

- **Data:** 06/11/2026, sottoagente su pc-fisso. Commit: lo fa la sessione principale.
