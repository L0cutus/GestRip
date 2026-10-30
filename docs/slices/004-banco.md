# Slice 004 — Banco a scatola chiusa, scheletro (T0)

- **Livello:** completo (strumento che decide la parità; tocca Docker su pc-fisso).
- **Fonte:** PIANO §4; decisione 5 di Stefano (30/10/2026): istanza isolata e sintetica su
  pc-fisso, immagine di `go-baseline`, rete Docker interna, nessuna porta sull'host, prefisso
  proprio `grgo-`, spazio su disco controllato prima, mai la produzione. Riferimento di metodo:
  gli script della skill `gestrip-browser-gate` (pc-lab), letti e non modificati.
- **Confine:** `tools/banco` con questi comandi:
  - `su`, `esegui`, `giu` e `controlla`, sull'host;
  - `confronta` e `attendi`, dentro il container.

  In più `bench/scenari/healthz.json` e `bench/deviazioni.json` (vuoto), e gli scenari del banco
  riportati nella copertura. Solo HTTP. Il browser (Playwright) e i dati sintetici con la catena
  Flask → standalone → import arrivano in T3.

## Come si usa

Docker senza sudo, attraverso il gruppo `docker`:

    B=to-be-deleted-after-use/banco-bin
    CGO_ENABLED=0 go build -trimpath -o $B/gestrip ./cmd/gestrip
    CGO_ENABLED=0 go build -trimpath -o $B/banco ./tools/banco
    sg docker -c "$B/banco su -nome t0 -immagine gestrip-standalone:fab-213 -gestrip $B/gestrip"
    sg docker -c "$B/banco esegui -nome t0 -banco $B/banco"
    sg docker -c "$B/banco giu -nome t0"

**Com'è fatta un'istanza:**
- `grgo-<nome>-app` (standalone), `-caddy` (TLS interno, CA propria), `-go` (binario statico su
  `debian:bookworm-slim` fissata per digest) e `-banco` (il confronto), tutti sulla rete
  `grgo-<nome>-net` `--internal`;
- container con utente 1000, `cap-drop ALL`, sola lettura e limiti di CPU e RAM (4 CPU, 4 GB);
- segreti generati al momento, con permessi 0600, in
  `to-be-deleted-after-use/banco/grgo-<nome>/`;
- il profilo seccomp di Chromium letto con `git show go-baseline:` (mai dal worktree).

## Ricevuta di completamento

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Immagine:** `gestrip-standalone:fab-213`, cioè la slice 213 = `go-baseline` `5c80347`
  (creata il 29/10/2026 alle 23:02), id `sha256:abab45135c65…`.
- **TDD:** 6 test del confronto e 4 dell'esecuzione, scritti prima (`undefined: leggiScenario`) e
  poi verdi; 2 test della copertura per gli scenari.
- **Prove dal vivo:**
  - `banco su -nome t0`: bootstrap dello standalone e istanza pronta in 3 s;
  - `docker ps`: nessuna porta pubblicata; rete `internal=true`; `ss -ltn` sull'host senza le
    porte dell'istanza;
  - `banco esegui`: 1 scenario, 2 passi (GET con query inventata e HEAD), **0 differenze →
    BANCO VERDE**;
  - prova negativa con uno scenario temporaneo `POST /healthz`: «INATTESA codice: standalone
    404, go 405» → banco rosso. È il debito già annotato nella 003;
  - `banco giu -nome t0` → «AVANZI: nessuno».
- **Review indipendente (30/10/2026):** 13 osservazioni, 1 alta, tutte valutate.
  - **Alta, corretta: il controllo delle D-xx approvate non partiva mai.** Con
    `sh -c '… "$0" "$1" "$@"'` il primo argomento posizionale (`$1`) finiva anche in `"$@"`,
    `flag` si fermava lì e `-inventario` veniva ignorato. Ora non c'è più nessuna shell: due
    `docker run` (`attendi`, poi `confronta`) con argomenti diretti.
    - `TestComandiEseguiPassanoInventarioEdEsito` riesegue il parsing dei flag sugli argomenti
      veri;
    - `confronta` rifiuta argomenti in più;
    - con deviazioni presenti l'inventario è obbligatorio.
    - Prova dal vivo: una D-99 non approvata → «deviazione D-99 usata dal banco ma non approvata
      nell'inventario», banco rosso.
  - **Medie, corrette:**
    - `giu` non cancella niente se docker non risponde, e si ferma sugli errori diversi da «No
      such…»;
    - `docker rm -v`;
    - `su` crea la cartella delle istanze prima di misurare lo spazio;
    - percorsi controllati con `EvalSymlinks` sia in `su` sia in `giu`: assoluti, senza
      virgole, mai verso la produzione;
    - `-banco` vuoto rifiutato;
    - `confronta -esito` scrive l'esito per scenario.
  - **Allineato il PIANO:** `bench/scenari/`, `bench/deviazioni.json`, stati della copertura. Lo
    scenario `healthz` ora confronta anche `referrer-policy` e `x-robots-tag`.
  - **Debiti bassi:**
    - `-immagine` non viene verificata contro il tag (l'immagine non ha label di revisione);
    - deviazioni non più usate non segnalate;
    - corpi oltre 16 MiB troncati.
- **Mutazioni:** 10 sulle correzioni. 8 uccise. Una era scritta male e non si è applicata. La
  sopravvissuta era un ramo `Readlink` superfluo, che è stato tolto: `EvalSymlinks` già rifiuta i
  collegamenti verso la produzione e quelli che non si risolvono, come provano i test.
- **Secondo giro dal vivo, con il codice corretto:**
  - `su` → `esegui` → **BANCO VERDE**, con `inventario_controllato: true`;
  - `giu` → «AVANZI: nessuno».
- **Copertura:** con l'esito del banco ADM-20 è **chiusa** (4 test Go verdi più lo scenario
  `healthz` verde). Senza l'esito resta parziale. In tutto 353 voci: 1 chiusa, 339 aperte, 13
  fuori v1.
- **Debiti:**
  - le immagini vengono usate, ma niente pull né build: se manca un'immagine, `su` si ferma;
  - livello browser e dati sintetici in T3;
  - confronto del testo normalizzato e dei PDF quando arrivano le pagine.
