# 111 — Sidecar dell'IA (T4.10, undicesima)

- **Fonte:** PIANO §1.1 (lo schema con «sorveglia i sidecar su 127.0.0.1»), §1.2 (`internal/sidecar`), §1.8 (avvio,
  `/health`, riavvio con attese crescenti, GestRip su anche senza sidecar, prove vere solo CPU sotto `systemd-run` sopra la
  porta 47000) e §1.9 (sidecar in `/opt/gestrip/releases/<v>/`); piano della T4.10 riga 111, risposte A, C e G, nota 8
  (nella T4.10 avvio, salute e riavvio con il figlio finto; download, pacchetti e misure alla T5.1). **Lo standalone non
  avvia processi del modello** (cercato a `go-baseline` in `lib/` e `config/`: niente `Port.open`, `MuonTrap`,
  `System.cmd` per il modello; `LocalChat` parla con un server già acceso): qui non c'è un comportamento da copiare.
- **Voci:** nessuna voce dell'inventario si chiude (il sidecar è infrastruttura del PIANO, non una voce dello standalone);
  RIP-H-47 (riservatezza) resta parziale, senza segno. Nessuna D nuova.

## Cosa c'è

- **`internal/sidecar`** (nuovo): `sidecar.go` (`Comando`, `Tempi` e `TempiDiDefault`, `Supervisore` con `Nuovo`,
  `Lavoro`, `Stato`, `Indirizzo`, `ConAttesa`), `figlio.go` (avvio con Pdeathsig sul thread bloccato, arresto
  SIGTERM → SIGKILL, esito per il registro), `figlio_linux.go` / `figlio_altri.go` (gruppo di processi e segnali).
- **`internal/apparecchio/apparecchio.go`:** `Configurazione.Sidecar` e le righe `sidecar_estrazione`,
  `sidecar_estrazione_porta`, `sidecar_estrazione_salute` (e le stesse per `trascrizione`).
- **`cmd/gestrip/main.go`:** `serve` fa girare un supervisore per ogni sidecar del file fra i lavori in sottofondo (li
  aspetta all'uscita, prima di chiudere il database); `opzioniServe.conSidecar` per i test; il commento del comando.
- **Test:** `internal/sidecar/sidecar_test.go`, `sidecar_linux_test.go` (figlio finto e supervisore finto),
  `vero_linux_test.go` (la corsa vera, saltata senza le variabili); `internal/apparecchio/apparecchio_test.go`
  (`TestSidecar`); `cmd/gestrip/sidecar_linux_test.go` e il modo `sidecar-finto` in `figlio_linux_test.go`.
- **SVILUPPO:** sezione nuova «Sidecar dell'IA (slice 111, PIANO §1.8)».

## Scelte

1. **Supervisore generico** (risposta A): programma, argomenti con `{porta}`, porta, percorso di salute. Niente di
   specifico di `llama-server` nel codice; `/health` è solo il default del percorso.
2. **Solo il 200 è salute.** llama-server dà 503 mentre carica il modello: durante l'avvio si aspetta (fino a `Avvio`,
   3 min di default), a sidecar pronto 3 controlli falliti di fila (ogni 10 s) lo riavviano.
3. **Porta occupata → il figlio non parte** (e si riprova con le stesse attese): la salute risponderebbe per un altro
   server (un sidecar rimasto, un servizio di pc-fisso), e GestRip lo crederebbe suo. Resta una corsa fra il controllo e
   il `listen` del figlio, che allora esce e si riprova.
4. **Attese:** 1 s, poi il doppio, fino a 5 min; un figlio rimasto pronto 10 min riporta l'attesa a 1 s. Mai un
   arrendersi: un sidecar sbagliato costa al più un tentativo ogni 5 minuti e una riga nel registro.
5. **Ambiente del figlio: solo quello del `Comando`, di default vuoto** (come il figlio delle foto): le variabili di
   GestRip (anche eventuali segreti o proxy) non passano. Cartella `/`, quindi nella configurazione i percorsi del modello
   vanno scritti assoluti. Standard input, output ed errore su `/dev/null`: un server del modello può scriverci le
   richieste, cioè i dettati.
6. **Gruppo di processi suo + Pdeathsig**, come `figlitest` e `imaging`: all'arresto SIGTERM al gruppo, SIGKILL dopo
   10 s, poi SIGKILL al gruppo anche a figlio già raccolto (i nipoti); se GestRip muore di colpo il figlio muore con lui.
7. **Configurazione nel file dell'apparecchio**, spenta di default: è roba della macchina (percorsi in
   `/opt/gestrip/releases/<v>/`, li scriverà l'installer della T5.1), non del database né del backup. Una riga sbagliata
   ferma l'avvio come ogni altra riga del file; un sidecar che a runtime non parte no.
8. **Nessun collegamento all'assistente** (vedi Dubbi 1): l'estrazione e la trascrizione continuano a usare solo gli
   indirizzi delle impostazioni della 107.
9. **I tempi di prova sono veri ma corti** (10-40 ms): l'attesa fra i riavvii si può sostituire (`ConAttesa`), ma i
   controlli di salute e l'avvio girano su `clock.Aspetta` e `clock.Parti`; l'orologio finto del repo (`clock.Finto`) dà
   solo l'ora, non le attese. I test aspettano condizioni, mai una durata fissa come prova (le due `Sleep` di
   `TestStabileAzzeraLAttesa` e `TestSingoliControlliFalliti` sono minimi: più lunghe non cambiano l'esito).

10. **Il 200 deve venire dal figlio (revisione, M2).** Dopo il primo 200, e a ogni controllo, il supervisore legge
    `/proc/net/tcp` e `/proc/net/tcp6`: sulla porta devono ascoltare solo socket su 127.0.0.1 (IPv4; anche `::1` o
    `0.0.0.0` bastano a dire no) e ognuno deve essere aperto da un processo del gruppo del figlio (inode del socket fra i
    `/proc/<pid>/fd` dei processi con quel gruppo, letto da `/proc/<pid>/stat`). Se no il sidecar non è pronto (all'avvio
    si aspetta fino a `Avvio`, a sidecar pronto conta come un controllo fallito) e il registro lo dice una volta per giro
    con il solo motivo («in ascolto anche fuori da 127.0.0.1», «socket di un altro processo», «nessun socket in ascolto»,
    «lettura di /proc/…»). `ascolto_linux.go`, circa 130 righe; fuori da Linux il controllo non si fa.
11. **Correzioni della revisione:** `pronto` misurato prima di `FasePronto` (M3); nessun avvio con il contesto già
    finito (B3); `{porta}` obbligatorio in almeno un argomento e percorso di salute controllato anche con `url.Parse`
    (B5); stessa porta per estrazione e trascrizione rifiutata nel file dell'apparecchio (M2).

## Prove

- **Figlio finto** (il binario dei test con `sidecar-finto <modo> <porta>`): partenza e salute (gruppo suo),
  avvio lento (503 poi 200: nessuna caduta), figlio ucciso 4 volte → attese 10, 20, 40, 40 ms e 5 avvii, figlio stabile →
  attesa sempre al minimo, figlio che esce subito con 3 → riprova all'infinito con le attese che crescono e `Lavoro`
  che non torna, programma che non c'è (registro «non trovato», senza percorso), figlio muto oltre il tempo d'avvio,
  figlio malato → riavvio, singoli controlli falliti non di fila → nessun riavvio, porta occupata → nessun avvio finché
  si libera, figlio sordo a SIGTERM → SIGKILL dopo l'arresto, **nessun orfano** (nipote nel gruppo morto quando il figlio
  cade e alla chiusura), ambiente e cartella del figlio, fd 0-2 su `/dev/null`, registro senza dettato, chiave, ambiente,
  argomenti o percorso, contesto già finito, chiusura durante l'attesa, **figlio morto con GestRip ucciso con SIGKILL**
  (supervisore finto in un processo a parte). Dopo la revisione: figlio **appeso** (accetta e non risponde: riparte
  dopo il tempo d'avvio; la chiusura non aspetta la GET appesa), **302** verso un 200 che non vale come salute, figlio
  che ascolta su **0.0.0.0**, **200 da un processo fuori dal gruppo** (il figlio cede la porta a un processo in una
  sessione sua: mai pronto, poi «porta occupata»), `ascoltoDelGruppo` sul socket del processo dei test (nostro, di un
  altro gruppo, anche su `::1`), lettura di un `/proc/net/tcp` inventato, `controlli_falliti=2` nel registro del figlio
  malato. Le attese esatte contano solo le cadute volute (`cadutaVoluta`); le porte dei test sono a caso fra 20000 e
  32000, sotto l'intervallo effimero (M1); `aspettaMorto` conta uno zombie come morto (B8).
- **serve:** con le righe nel file avvia il sidecar finto, lo porta a pronto e alla chiusura lo ferma prima di tornare
  (pid e gruppo spariti); un sidecar che non parte non ferma serve (`/healthz` 200); senza righe nessun supervisore;
  una faccia che cade fa tornare serve con l'errore e il sidecar già fermato (B7).
- `go test -count=10` e `-race -count=3` di `internal/sidecar` verdi; `scripts/check` verde (`check-1.log`, `check-2.log` dopo i test rafforzati, `check-3.log` dopo la revisione; copertura
  senza banco 353 voci, 0 rosse). Nessun processo rimasto (`pgrep`).
- **Mutazioni a mano:** vedi sotto (giro su una copia, `to-be-deleted-after-use/slice-111/mutazioni/`).
- **Corsa vera: non lanciata** (brief). Comando qui sotto.

## Corsa vera (lanciata il 21/11/2026 con il «memoria libera» del coordinatore)

Cercati in sola lettura: `~/sviluppo/llama-cpp-b12503/build/bin/llama-server` (build con il backend CUDA: si tiene
fuori la GPU con `--device none -ngl 0` e con `CUDA_VISIBLE_DEVICES=` nell'ambiente del figlio, che il test gli dà)
e `~/ft-gestrip/gestrip-Qwen3.5-0.8B-v3-Q4_0.gguf` (0,5 GB; **non** è il file dei profili di gestrip-switch, che usano
`~/gestrip-e2b/…`). Porta 47391 (libera al momento della prova; mai 43651, 43653, 43660-43662, 43671).

```sh
# pc-fisso, ~/sviluppo/gestrip-go
CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -c -trimpath -tags nodynamic \
  -o to-be-deleted-after-use/slice-111/sidecar.test ./internal/sidecar
timeout 120 systemd-run --user --scope --quiet -p MemoryMax=3G -p MemorySwapMax=0 -p CPUQuota=400% \
  env -i HOME="$HOME" CUDA_VISIBLE_DEVICES= GESTRIP_SIDECAR_VERO_PORTA=47391 \
  GESTRIP_SIDECAR_VERO="$HOME/sviluppo/llama-cpp-b12503/build/bin/llama-server -m $HOME/ft-gestrip/gestrip-Qwen3.5-0.8B-v3-Q4_0.gguf --host 127.0.0.1 --port {porta} --device none -ngl 0 -t 4 -c 2048 -np 1 --jinja" \
  nice -n 19 to-be-deleted-after-use/slice-111/sidecar.test -test.run '^TestSidecarVero$' -test.v -test.timeout 100s \
  2>&1 | tee to-be-deleted-after-use/slice-111/corsa-vera.log
pgrep -af 'llama-server.*47391|sidecar.test' || echo "nessun processo rimasto"
```

Il `timeout 120` esterno manda SIGTERM al binario dei test; llama-server muore con lui (Pdeathsig). Cosa fa: avvio fino al 200 di `/health` (tempo scritto), due domande brevi inventate a `/v1/chat/completions`
(`max_tokens` 64; si scrivono stato, byte e tempo, mai la risposta), VmRSS/VmHWM del figlio, figlio ucciso con SIGKILL e
ripartito, chiusura senza orfani. **RAM attesa:** circa 0,6-0,9 GB per llama-server (0,5 GB di pesi in mmap più il
contesto di 2048 e i buffer di calcolo su CPU) più qualche decina di MB del test; il tetto di 3 GB dello scope copre
tutto, senza swap. CPU: 4 thread, tetto 400%. Durata attesa: meno di un minuto.

**Esito del 21/11/2026, 03:24: PASS in 4,7 s.** Pronto in 0,8 s; due domande HTTP 200 in circa 1 s ciascuna; memoria
del figlio VmHWM 1,05 GB dopo l'avvio e 1,09 GB dopo le domande (sopra la stima di 0,6-0,9 GB, ben sotto il tetto di
3 GB); figlio ucciso con SIGKILL e ripartito dopo l'attesa di 1 s (pronto in 1,8 s); fermato in 57 ms. Dopo la corsa
nessun processo `llama-server`/`sidecar.test` e nessun ascolto sulla porta 47391. GPU esclusa (`CUDA_VISIBLE_DEVICES`
vuoto, `--device none -ngl 0`).

## Mutazioni

`to-be-deleted-after-use/slice-111/mutazioni/muta.py`, su una copia del repo: **38 scritte** (20 sul supervisore, 7
sull'avvio e l'arresto del figlio, 3 sui segnali, 5 sul file dell'apparecchio, 3 su `serve`). Al primo giro 2 non
compilanti (S03, A04: riscritte), 30 uccise, 6 vive. Rafforzati i test e rifatte: **S04** (un riavvio per salute mancata
contato come «stabile»: `TestFiglioMalatoRiparte` ora guarda che le attese raddoppino), **A01** (una sola delle due righe
obbligatorie: ora il messaggio dice quale manca), **C01** (`serve` che non aspetta il supervisore: il sidecar finto di
`cmd/gestrip` ora chiude 300 ms dopo SIGTERM, e il test guarda che sia morto quando `serve` torna), S03 riscritta:
**uccise**. Restano **4 vive, equivalenti**: F01 (`Env` senza la copia: `Nuovo` lo rende già non nil), A04 (programma
relativo nel file: lo rifiuta anche `Comando.Controlla`), L03 (SIGTERM al solo figlio e non al gruppo: i nipoti muoiono
comunque con il SIGKILL al gruppo dopo la raccolta), F06 (thread sbloccato subito dopo `Start`: Pdeathsig scatterebbe solo
se il runtime chiudesse quel thread, cosa che un test non provoca in modo affidabile). **Finale: 34 uccise, 4
equivalenti, 0 vive osservabili.**

## Revisione indipendente

**0 ALTE, 3 MEDIE, 7 BASSE.** Mutazioni del revisore: **35 scritte, 32 compilanti, 23 uccise, 4 equivalenti, 5 vive**.
Corrette M1 (porte dei test nell'intervallo effimero: 6 binari falliti su 36 sotto stress), M2 (un 200 di un altro server
valeva come «pronto»), M3, B1, B2, B3, B5, B6, B7, B8 (vedi Scelte 10-11 e Prove). B4 resta nei Dubbi.
Le mutazioni vive del revisore rifatte sul codice corretto (`mutazioni/giro-3.log`): **uccise M05** (`controlli_falliti=2`
nel registro), **M14** (il 302), **M15b** (il figlio appeso), **M20** (la faccia caduta con il sidecar acceso), **M25** (la
chiusura durante una GET appesa); **M29 resta viva ed è ora equivalente**: con M3 `pronto` si misura prima di
`FasePronto`, quindi una pausa dopo non cambia la misura. Mutazioni nuove sulle correzioni (rosso dei test nuovi): il
controllo della porta sempre vero, senza il «fuori da 127.0.0.1», senza il proprietario, con un gruppo qualunque, stessa
porta permessa nel file, `{porta}` non obbligatorio, percorso di salute con host: **7 su 7 uccise** (N05 riscritta dopo un
primo giro che non compilava). **Stress** (`stress/gira.sh`): 5 giri di 12 processi `-race` in parallelo con `nice 10`
(10 di `internal/sidecar` interi, 1 dei test `Sidecar` di `cmd/gestrip`, 1 di `internal/apparecchio`): **60 su 60 verdi**.

## Dubbi

**Scelte aperte, alla T5.1 (decisione del coordinatore, 21/11/2026):** l'uso automatico dei sidecar da parte
dell'assistente (Dubbio 1), i nomi delle chiavi `sidecar_*` (Dubbio 2) e i limiti cgroup (Dubbio 4). Nella 111 i sidecar
restano spenti di default e l'assistente usa solo gli indirizzi della 107.

1. **Collegamento all'assistente: scelta di prodotto non scritta, non fatta.** Con un sidecar d'estrazione acceso,
   l'estrazione deve usarlo da sola (indirizzo `http://127.0.0.1:<porta>` e nome del modello) quando le impostazioni sono
   vuote? Deve vincere sulle impostazioni? La pagina «Assistente» deve mostrarlo, o dire che è giù? E il modello da
   mandare nel campo `model` (llama-server lo ignora, un altro server no)? Lo stesso per la trascrizione (`/stt` del server
   esterno di oggi contro l'API del motore locale della T5.1). Proposta: decidere con la T5.1, quando c'è il modello locale;
   fino ad allora il sidecar si avvia solo se il file lo chiede e l'assistente resta come nella 107.
2. **Chiavi del file dell'apparecchio** (`sidecar_<famiglia>`, `_porta`, `_salute`): forma mia, da confermare con
   l'installer della T5.1 (che le scriverà). Gli argomenti si separano con gli spazi, senza virgolette: un percorso con uno
   spazio non si scrive (sull'apparecchio i percorsi sono di GestRip).
3. **PIANO §1.8 da correggere (risposta A), testo proposto** al posto del primo punto:
   «`internal/sidecar` avvia i sidecar dell'IA come processi figli su `127.0.0.1` con una porta alta: il server
   dell'estrazione dei campi (un server compatibile OpenAI: `llama-server` con un GGUF o un altro, scelto alla T5.1 con il
   banco della 112) e, se installata, la trascrizione. Il supervisore è generico (programma, argomenti, porta, percorso di
   salute, nel file dell'apparecchio); controlla la salute e riavvia con attese crescenti se cade. GestRip resta in piedi
   anche se il sidecar non parte.» E nello schema di §1.1 «llama-server (estrazione campi)» → «server del modello
   (estrazione campi)».
4. **Limiti di risorse del figlio** (memoria, CPU): il supervisore non li mette (il figlio delle foto ha `setrlimit`). Per
   un server del modello `RLIMIT_DATA` è fragile (mmap dei pesi); meglio un cgroup dal servizio systemd dell'installer
   (`MemoryMax` sull'unità di gestrip, o un'unità per il sidecar). Da decidere alla T5.1 con le misure sull'apparecchio.
5. **Nipoti con un gruppo loro** (Setsid) sfuggono all'arresto, come in `figlitest`; llama-server non ne fa. Sull'apparecchio
   li prende il cgroup dell'unità systemd (`KillMode=control-group`, il default).
6. **`llama-server --help` lanciato una volta** (sola lettura delle opzioni, `env -i`, niente modello né porta) per
   controllare `--device`: il binario carica il backend CUDA anche solo per l'aiuto, quindi può aver interrogato il driver
   della GPU per un istante. Nessun modello caricato.
7. **B4 della revisione:** dopo la raccolta del figlio il SIGKILL al gruppo colpisce i nipoti rimasti; se il gruppo è già
   vuoto e nel frattempo il kernel ha dato quel numero a un nuovo processo che si è fatto capo di un gruppo, il SIGKILL
   andrebbe a lui. Probabilità quasi nulla (il pid deve essere riusato fra `Wait` e la riga dopo); lasciato così.
8. **Porte dei sidecar per la T5.1:** sull'apparecchio scegliere porte fuori dall'intervallo effimero (32768-60999 su
   Linux) oppure riservarle con `net.ipv4.ip_local_reserved_ports`, così una connessione in uscita non può prenderle
   prima del sidecar (la stessa causa di M1 nei test). La 47301 dell'esempio sta dentro l'intervallo.

