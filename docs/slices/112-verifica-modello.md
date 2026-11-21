# 112 — Banco del modello, `gestrip verifica-modello` (T4.10, dodicesima)

- **Fonte:** piano della T4.10 riga 112, risposte A (il modello della T5.1 si sceglie con questo banco), B (formato corto
  D-103), C (una sola corsa vera, solo CPU) e F (un solo nuovo tentativo in 90 s); PIANO «T4.10, banco del modello e
  modello di estrazione (03/11/2026)». **Chi decide:** lo standalone a `go-baseline` (`mix assist.bench`,
  `GestripStandalone.Assist.Bench`, slice 179 dello standalone) per il punteggio, il Go (106, 104-105) per la richiesta e
  la lettura.
- **Voci:** RIP-H-54 e OPS-08 si chiudono (segno `// inventario: RIP-H-54, OPS-08` su `TestCasiDelRepo`,
  `TestGiraConIlModelloFinto`, `TestVerificaModelloNonStampaIlDettato`, `TestVerificaModelloProcesso`). Nessuna D nuova.

## Cosa c'è

- **`internal/verifica`** (nuovo): `casi.go` (`Carica`, `CaricaTutti`, `Campione`; i due formati di casi che c'erano già),
  `punteggio.go` (`Valuta`, tolleranze), `riassunto.go` (`Riassumi`, `Rapporto`, `RigaDelCaso`), `corsa.go` (`Corsa.Gira`,
  `DallaBusta`, cronometro sostituibile).
- **`cmd/gestrip/verifica.go`** e il ramo `verifica-modello` in `main.go` (commento del comando e `uso`).
- **`internal/assist/modello.go`:** `Estrazione.Tentativi` (le richieste mandate, anche sul rifiuto), per contare i nuovi
  tentativi; il resto del client è quello della 106.
- **`bench/modello/README.md`:** il comando, i parametri del server delle misure, e come rigenerare in locale gli 84
  casi dello standalone, che **restano fuori dal repo** (vedi Dati).
- **Test:** `internal/verifica/verifica_test.go`, `cmd/gestrip/verifica_test.go`, `cmd/gestrip/verifica_linux_test.go`;
  `internal/assist/modello_test.go` guarda anche `Tentativi`.
- **SVILUPPO:** sezione nuova «Banco del modello (slice 112, RIP-H-54, OPS-08)».

## Scelte

1. **Come la nuova busta, davvero:** ogni caso passa per `assist.Collegamento.Estrai` (stesso client, un solo messaggio
   utente «Oggi: …\nDettato: …», `temperature 0`, nuovo tentativo dopo illeggibile o errore, 90 s in tutto), senza
   registro dell'audit (`reg` nil), con un negozio senza clienti e con le oreficerie di `-oreficerie` (default
   «Bottari,Bianchi Gioielli», come `@default_oreficerie` dello standalone).
2. **Punteggio sulla proposta**, cioè sui campi che vedrebbe il negozio (come `read_answer/3` nello standalone), anche
   per i banchi B, C, D, D2: le chiavi corte `n o b d c p a x` vanno su nome, oreficeria, busta, descrizione,
   data_consegna, preventivo, acconto, altre_note; busta, acconto e altre note si rileggono dalle note «Busta N —
   Acconto N € — x» (D-20). Una chiave assente nel banco vuol dire «vuoto»: un acconto o una busta inventati sono errori.
   I casi nel formato dello standalone si contano sui loro campi (`@scored` più busta e parola quando ci sono); la
   **busta** come `right?("busta")`: `\bbusta\s+N\b` (Unicode, senza maiuscole) **in qualsiasi punto delle note** (M1 della
   revisione). Nei banchi B-D2 la busta resta il primo pezzo «Busta N» delle note, come le scrive la proposta: lì la
   chiave `b` è proprio quel pezzo, e una «busta N» dentro le altre note non è la busta della scheda.
   **Acconti e buste inventati** (M2): atteso vuoto e proposta con l'acconto (o la busta); il resoconto ha le righe
   «acconti inventati: N» (come la tabella del PIANO) e «buste inventate: N». Solo con una proposta.
3. **Tolleranze solo quelle chieste:** maiuscole e spazi (ai bordi e ripetuti), «-» = «/» nel solo preventivo; il
   telefono atteso passa per `telefono.Normalizza` (come `Phone.normalize/1` nello standalone). «35 €», «35 euro»,
   «35.0» non sono «35».
4. **Tempi** per caso dalla domanda alla proposta, **nuovo tentativo compreso** (l'attesa del negozio; lo standalone
   misurava il solo scambio); mediana e p90 al rango più vicino come `rank/2`. Cronometro sulla lettura monotona
   (`clock.Parti`), sostituibile nei test.
5. **Riservatezza:** il resoconto ha solo numeri, nomi di campi e file, id dei casi e motivi fissi («ai:http:500»,
   «testo troppo corto»). Non escono il dettato, la risposta, il nome del modello che il server dice (potrebbe ripetere
   il dettato), l'indirizzo; un file sbagliato si rifiuta con il solo numero della riga. Niente `-json` con le risposte
   (lo standalone lo aveva: vedi Dubbi 4). Niente `-chiave`.
6. **File rifiutato intero** per una riga sbagliata, per un file vuoto o per due id uguali (come `load/1`); anche una
   riga dei banchi con il giorno della settimana che non torna con la data (la domanda lo ricalcola dalla data).
7. **Id dei casi dei banchi:** `<file senza .jsonl>-<riga>` (i banchi non hanno id).
8. **`-per-categoria N`** per la corsa su CPU: al più N casi per categoria di ogni file, presi uniformi lungo la categoria
   (indici i·len/N), nell'ordine. Con 10: 300 casi dei banchi (90 di B, 30 di C, 100 di D, 80 di D2), più 30 degli 84 se ci sono.
9. **Credenziali nell'indirizzo** (`http://utente:password@…`) rifiutate come errore d'uso (B6).
10. **Uscite:** 0 dopo la corsa (qualunque punteggio), 2 per un'opzione sbagliata, 1 per i casi sbagliati o per una corsa
   interrotta (Ctrl-C o `timeout`: stampa quello che ha fatto, il caso a metà non conta). Avanzamento su stderr ogni 25
   casi.

## Dati

- **Banchi B, C, D, D2:** quelli del repo (`bench/modello/`, inventati, README del 03/11/2026), letti così come sono.
- **84 casi dello standalone: fuori dal repo in attesa dell'ok di Stefano** (decisione del coordinatore, 21/11/2026: lo
  standalone è un repo privato, pubblicarli sarebbe un'esposizione nuova). Non sono nei default di `-casi`, né nei
  test, né in questa ricevuta. Si rigenerano in locale con `to-be-deleted-after-use/slice-112/rigenera-84.sh` (legge
  `go-baseline:test/fixtures/dettato/casi.jsonl` con `git show` e, con `converti84.py`, porta i telefoni a «300…» nel
  dettato e negli attesi, come i banchi B) in `to-be-deleted-after-use/slice-112/casi/`, e si passano con `-casi`. Il file
  rigenerato si cancella a fine slice. Dei loro esiti in ricevute e log del repo vanno solo i conteggi.

## Prove

- **Modello finto** (`tools/banco/modellofinto` con `httptest`) e **cronometro finto**: casi giusti, un campo sbagliato
  per volta (tutti i campi dei due formati), risposta in un blocco markdown, illeggibile e poi buona (2 tentativi), 500 e
  poi buona, due errori (`ai:http:503`), acconto letto come preventivo, oreficerie note e no; il corpo di ogni richiesta
  (solo `model`, `temperature 0`, `stream false`, un messaggio `user` con «Oggi: venerdì 23/10/2026\nDettato: …», nessuna
  `Authorization`); mediana/p90/max esatti; corsa interrotta.
- **Nessun dettato in uscita:** `TestVerificaModelloNonStampaIlDettato` (dettato con nome, telefono e parole inventate e
  uniche; il server li ripete nel nome del modello, in due risposte illeggibili e in due 500; stdout, stderr, `log` e
  `slog` catturati) e `TestVerificaModelloProcesso` (processo vero: uscite 0, 2, 2, 2, 1; file di casi sbagliato con il
  dettato dentro).
- **Casi del repo:** i quattro banchi, 1720 casi leggibili, id diversi, 300 nel campione da 10; il comando senza `-casi`
  li trova dalla radice del repo (30 domande con `-per-categoria 1`). I test usano solo casi scritti apposta.
- `scripts/check` verde (`to-be-deleted-after-use/slice-112/check-5.log`, dopo la revisione; copertura 353 voci, 49 chiuse, 0 rosse). In
  `check-1` solo gofmt; in `check-2` la guardia `TestNessunPercorsoNeiRegistriDelCodice` ha preso la variabile `r` (nome
  riservato alle richieste) in un `Fprintln` del comando: rinominata `riga`.
- **TDD:** i test sono stati scritti subito dopo il codice, non prima; il «rosso» l'hanno dato le mutazioni (sotto).

## Mutazioni

`to-be-deleted-after-use/slice-112/mutazioni/muta.py`, su una copia del repo: **47 scritte** (32 su `internal/verifica`,
2 su `Estrazione.Tentativi`, 10 sul comando e `main.go`, 3 sulla riservatezza). Primo giro: 32 uccise, 5 non compilanti
(riscritte), 7 vive. Di queste, 4 (giorno della settimana, id ripetuti, riga sbagliata saltata, busta vuota) vive solo
perché il filtro `-run` dello script non prendeva `TestCarica*`: con il filtro corretto uccise. Vive davvero: **V32** e
**C04** (le oreficerie non arrivano alla lettura) → test nuovi `TestGiraConLeOreficerieDelNegozio` e
`TestVerificaModelloOreficerie`, uccise; **V08** (parola cercata con le maiuscole) → controllo nuovo in
`TestValoriDellaProposta`, uccisa. **Finale: 46 uccise, 1 equivalente** (V28, `<=` → `<` in `Campione`: con un gruppo
lungo esattamente N il ramo del campione prende comunque tutti).

**Revisione indipendente:** 0 ALTE, 2 MEDIE, 9 BASSE. Mutazioni del revisore: **32 compilanti, 25 uccise, 4 vive, 3
equivalenti**. Corrette M1, M2 e B1-B8 (B9 nei Dubbi), con il test rosso prima (M1, M2, B3, B6: rossi osservati; B1 e
B2 sono test nuovi su comportamenti già giusti, che uccidono le mutazioni). Le 4 vive del revisore rifatte sul codice
corretto (`mutazioni/giro-6.log`): **R12** (`separaNote` che legge «busta …» minuscolo come la busta), **R20** (motivi dei
rifiuti non ordinati), **R26** (`verifica-modello` senza SIGTERM: test sul processo vero fermato mentre il server tiene la
domanda), **R31** (date a una cifra senza lo zero): **4 su 4 uccise**.

## Corsa vera (lanciata il 21/11/2026 con il «memoria libera» del coordinatore)

Cercati in sola lettura (`ls`, sorgente di `common/arg.cpp` per le opzioni; **`llama-server` mai avviato, nemmeno
`--help`**): `~/sviluppo/llama-cpp-b12503/build/bin/llama-server` e `~/ft-gestrip/gestrip-Qwen3.5-0.8B-v3-Q4_0.gguf`
(501 MB). Porta **61392**, sopra l'intervallo effimero 32768-60999 (B5 della revisione; lo script si ferma se è occupata; mai 43651, 43653, 43660-43662, 43671).

```sh
# pc-fisso, ~/sviluppo/gestrip-go
CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go build -trimpath -tags nodynamic -o to-be-deleted-after-use/slice-112/gestrip ./cmd/gestrip
sh to-be-deleted-after-use/slice-112/rigenera-84.sh   # gli 84 casi, in locale e fuori da git
timeout -k 15 900 systemd-run --user --scope --quiet -p MemoryMax=3G -p MemorySwapMax=0 -p CPUQuota=400% \
  env -i HOME="$HOME" PATH=/usr/bin:/bin CUDA_VISIBLE_DEVICES= \
  nice -n 19 sh to-be-deleted-after-use/slice-112/corsa-vera.sh 10 \
  2>&1 | tee to-be-deleted-after-use/slice-112/corsa-vera.log
pgrep -af 'llama-serve[r].*61392' || echo "nessun processo rimasto"
```

`corsa-vera.sh` avvia `llama-server -m … --host 127.0.0.1 --port 61392 --device none -ngl 0 -t 4 -c 2048 -np 1 --jinja
--reasoning off -n 256 --cache-ram 0` con `CUDA_VISIBLE_DEVICES=` vuoto e `setpriv --pdeathsig KILL` (muore con lo script anche se il
`timeout` lo uccide), aspetta `/health` fino a 120 s, lancia `gestrip verifica-modello -indirizzo http://127.0.0.1:61392
-modello gestrip-qwen3.5-0.8b-v3 -per-categoria 10`, scrive durata e VmHWM/VmRSS di llama-server, e alla fine lo ferma
(trap). Con il file rigenerato (`rigenera-84.sh` prima della corsa) `-casi` è quello più i quattro banchi; senza, solo
i banchi. **Per fermarla:** `kill -TERM <PID di timeout>` (`pgrep -f 'timeout -k 15 900 systemd-run'`), non Ctrl-C sulla pipe
con `tee`, che fermerebbe anche `tee` prima del resoconto parziale; `verifica-modello` riceve il SIGTERM, stampa quello
che ha fatto ed esce con 1, e `corsa-vera.sh` ferma llama-server (B4). Il log di llama-server e quello della corsa restano nei temporanei (dati inventati; gli id degli 84 casi solo
lì): nella ricevuta vanno solo i conteggi.

**Parametri della corsa** (del server, non della richiesta, che resta quella della busta): `--reasoning off -n 256 --cache-ram 0`,
con `-c 2048 -np 1 --jinja`. **Scelta aperta per la T5.1:** il sidecar vero deve usare gli stessi parametri, altrimenti
le misure del banco non valgono in produzione. Lo dicono anche il README di `bench/modello/` e l'aiuto di `-indirizzo`.

- **Tempo atteso:** nella 111 una domanda breve su questa macchina ha preso circa 1 s; qui la risposta è una scheda di
  30-60 token, quindi **1-1,5 s per caso**. Con il campione di 10 per categoria (**330 casi**: 300 dei banchi più 30 degli 84; 300 senza)
  circa **6-8 minuti**, più i nuovi tentativi; il `timeout 900` lascia margine. **Tutti i 1804 casi** prenderebbero 30-45 minuti: per quelli
  `corsa-vera.sh 0` con `timeout -k 15 3600`, da decidere dopo aver visto il campione.
- **RAM attesa:** llama-server come nella 111 (VmHWM misurato 1,09 GB con `-c 2048`), `gestrip` qualche decina di MB:
  circa **1,1-1,2 GB** sotto il tetto di 3 GB, senza swap. CPU: 4 thread, tetto 400%, `nice 19`.

### Esito (21/11/2026)

- **Primo giro: fermato dal tetto.** Dopo circa 50 casi su 330 (48 s) l'OOM killer del memcg dello scope (3 GB) ha
  ucciso llama-server: anon-rss 3,1 GB, partito da 1,07 GB e cresciuto a ogni richiesta (gestrip 11 MB). Causa: la cache
  dei prompt in RAM di llama.cpp (`--cache-ram`, di default fino a 8 GiB), che cresce con dettati tutti diversi; nella 111
  (2 domande) non si vedeva. Niente fuori dallo scope: processo e porta spariti. Correzione: `--cache-ram 0`.
- **Secondo giro (identico più `--cache-ram 0`): PASS**, 330 casi su 330 in 205 s, VmHWM di llama-server 1,13 GB; dopo
  la corsa nessun processo e nessun ascolto sulla 61392. Qwen3.5 0.8B v3 Q4_0, solo CPU (4 thread), ragionamento spento.
  - schede tutte giuste 275/330: 84 casi dello standalone (campione) 30/30, B 56/90, C 21/30, D 98/100, D2 70/80;
  - per campo: data 30/30, nome 329/330, telefono 30/30, oreficeria 330/330, busta 299/309, descrizione 294/300, parola
    20/20, preventivo 304/330, acconto 279/300, altre note 278/300, data di consegna 330/330, compro oro 30/30 (dal testo);
  - acconti inventati 2, buste inventate 0, risposte illeggibili 0, rifiuti 0, nuovi tentativi 0;
  - tempo per scheda: mediana 608 ms, p90 753 ms, max 1011 ms.
- **Parametri della corsa e scelta aperta per la T5.1:** `--reasoning off -n 256 --cache-ram 0` (con `-c 2048 -np 1
  --jinja`). Il sidecar vero deve usare gli stessi (o almeno un tetto alla cache dei prompt), altrimenti le misure del
  banco non valgono in produzione e la memoria del mini PC cresce senza limite.

## Dubbi

1. **84 casi fuori dal repo in attesa dell'ok di Stefano.** Con l'ok si copiano in `bench/modello/` (con i telefoni
   già cambiati) e si rimettono nei default; fino ad allora si rigenerano in locale.
2. **Parametri del server della corsa** (`--reasoning off -n 256 --cache-ram 0`): scelta aperta per la T5.1, vedi «Corsa vera». Il
   traduttore di produzione ottiene lo stesso dalla richiesta (`max_tokens: 200`, `enable_thinking: false`), la busta no.
3. **Numeri non confrontabili con il PIANO del 03/11:** quelle misure contavano la risposta del modello (o di BERT);
   qui si conta la proposta, dopo le regole del nome della 105 (che possono correggere il nome, o svuotarlo). Se serve
   anche il punteggio della risposta nuda, è un'aggiunta piccola in `Valuta`.
4. **Niente `-json`:** lo standalone scriveva anche le risposte, per leggere gli errori; qui no, per non far uscire testo
   (le righe per caso dicono già quali campi sono sbagliati). Un `-json` con soli numeri si può aggiungere.
5. **Attesi pensati per il prompt lungo dello standalone:** due degli 84 casi hanno come nome atteso un segnaposto che
   il formato corto difficilmente dà; saranno sbagliati sul nome con quasi ogni modello. Lasciati come sono, da leggere
   sapendolo.
6. **Tempo per caso con il nuovo tentativo:** diverso dallo standalone (che misurava lo scambio); scelto perché è l'attesa
   del negozio.
7. **Tempi dei casi «non mandato»** (B9): un caso rifiutato prima della richiesta (troppo corto o troppo lungo) entra
   nei tempi con quasi 0 ms e abbassa mediana e p90. Nei banchi di oggi non ce ne sono; da decidere se toglierli dai tempi.
8. **Credenziali nell'indirizzo della busta (106):** `verifica-modello` le rifiuta (B6), ma l'indirizzo salvato
   nell'assistente le accetta e `Collegamento` le manda come Basic auth. Da decidere per la 106/107, qui solo annotato.
