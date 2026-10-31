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

Con `-dati sintetici-import` (slice 024) `banco su` prepara gli stessi dati dalle due parti con la
catena di PIANO §4.2 (cattura Flask sintetica → legacy import dello standalone → copia →
decifratura con il SQLCipher dell'immagine → `gestrip import -sintetico`) e fa partire il Go solo
con un import riconciliato. Senza l'opzione l'istanza nasce come prima, con i due account seminati.
Ricevuta in `docs/slices/024-catena-banco.md`.

Dalla slice 026, fra il legacy import e la copia c'è il passo **vendita** (`grgo-<nome>-vendita`,
`tools/banco/vendita.go`): nella stessa immagine dello standalone, senza rete, con le protezioni e
i dati dell'istanza come il legacy import, `bin/gestrip_standalone eval` avvia l'app come
`Release.quiet!/0` e chiama le funzioni della pagina delle vendite con il contesto d'audit della
pagina (`%{actor: <email del titolare sintetico>}`): `OnlineSales.create_draft/2`,
`SalePhotos.attach/3` su un piccolo JPEG inventato generato dal banco (montato in sola lettura) e
`OnlineSales.issue_sale/2`, che crea la revisione e copia la foto. Il profilo del negozio che
`issue_sale` vuole viene dal legacy import (che crea sempre il profilo «Negozio»); se mancasse,
`issue_sale` darebbe `{:error, :shop_profile_missing}` e il banco si fermerebbe con il motivo. Il
banco pretende una sola riga `GR_VENDITA:` riuscita, la foto nei media con la sua impronta e un
solo file in più. Dopo l'import Go controlla (non solo stampa): verdetto riconciliato, un avviso
`tipo/impronta_testo` su `sha256` di `historical_documents`, `sale_photos` e `sale_revision_photos`
per tutte le righe lette (D-42), le righe di `sales`, `sale_revisions`, `sale_photos`,
`sale_revision_photos` e `sale_revision_seals` lette e scritte come le ha contate lo standalone, e
le famiglie media `sale_photos` e `sale_revision_photos` complete (attesi = copiati = righe, nessun
file mancante né impronta diversa). Il riassunto le stampa insieme ai media in più della vendita.

Dalla slice 027 la catena arriva a un import **riconciliato**: il banco vuole anche esattamente un
avviso `traduzione/istante_senza_zona` su `sale_revision_photos.inserted_at`, per tutte le foto
delle revisioni scritte dalla vendita, senza `Id`; un avviso con lo stesso codice su una colonna
fuori dalla D-42 fa diventare rosso il banco.

Dalla slice 028 c'è anche il passo `emergenza`, dopo la vendita e prima della copia: un container
dell'immagine dello standalone senza rete che, con le funzioni della pagina, dà un QR a una busta
importata (`Workshop.update_repair/4`), apre un evento, scrive una riga, la storna, scansiona la busta
alla consegna e chiude l'evento (`EmergencyRegister`). Esito in una riga `GR_EMERGENZA:{…}`
controllata, riassunto in `runtime.json` sotto `registro_emergenza`. Da qui il banco **pretende**
l'avviso `traduzione/istante_senza_zona` su tutte e sei le colonne della D-42, ciascuna con le righe
esatte (foto delle revisioni della vendita, 2 righe del registro, 1 evento chiuso, 1 busta
scansionata).

**Attese per i dati (slice 029, T3b).** `banco esegui` legge dal `runtime.json` dell'istanza con
quali dati è partita (`account-seminati` o `sintetici-import`; mancante o sconosciuto = errore) e lo
passa a `confronta -dati`. Un passo di scenario può avere `atteso_dati: {"sintetici-import": {…}}`:
quei campi sostituiscono o si aggiungono a `atteso` solo con quei dati. Il campo `conteggio:<frase>`
dice quante volte una frase compare nel corpo (intero canonico, maiuscole distinte). Le frasi di
`testo:` e `conteggio:` non possono contenere caratteri che l'HTML scappa (`'`, `&`, `<`, `>`;
`"` solo in un attributo intero come `href="/settings"`). Oggi lo usa solo `utenti[2]`: su
database vuoto «Invito da consegnare» assente, sui dati importati presente 2 volte (i due account
Flask senza password). **I due giri del banco sono verdi**, 13 scenari e 156 passi ciascuno. Sulla catena gli scenari danno ancora la differenza nota `utenti[2]` (T3b).

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
