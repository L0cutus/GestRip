# Piano del porting di GestRip in Go

- **Stato:** proposta da approvare. Stefano approva il piano, poi si lavora a slice.
- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Fonti vincolanti:** `prompt-porting-go-v2.md` (prevale), `inventario-funzioni.md` (checklist
  di accettazione, 353 voci, decisioni A–G, deviazioni D-01…D-17), standalone Elixir al tag
  `go-baseline` = `5c80347`.
- **Ambiente già pronto (passo 0):** Go **1.27.3** (`~/sdk/go1.27.3`, sha256 verificato da
  go.dev), repo `~/sviluppo/gestrip-go` con remote `origin` = otto-vps `~/git/gestrip-go.git`,
  controllo dei segreti gitleaks 8.30.1 nel precommit. Dettagli in `docs/SVILUPPO.md`.

Le **domande per Stefano** sono raccolte nella sezione 8. Le prime dieci servono per
approvare il piano. Le altre si possono decidere più avanti.

---

## Indice

1. [Architettura](#1-architettura)
2. [Tema H: scelte tecniche](#2-tema-h-scelte-tecniche)
3. [Slice ordinate](#3-slice-ordinate)
4. [Banco a scatola chiusa](#4-banco-a-scatola-chiusa)
5. [Copertura dell'inventario](#5-copertura-dellinventario)
6. [Pubblicazione e dono ai colleghi](#6-pubblicazione-e-dono-ai-colleghi)
7. [Rischi, ambiguità e contraddizioni](#7-rischi-ambiguità-e-contraddizioni)
8. [Decisioni per Stefano](#8-decisioni-per-stefano)
9. [Metodo di lavoro](#9-metodo-di-lavoro)

---

## 1. Architettura

### 1.1 In una frase

Un solo programma (`gestrip`) che contiene il sito, il database, i PDF e le regole.
L'intelligenza artificiale gira in programmi separati (i *sidecar*) che `gestrip` avvia e
sorveglia. Tutto gira su un mini PC Ubuntu 26.04 con il disco cifrato.

```
 telefoni / PC del negozio ──Tailscale──▶ tailscale serve :8443 ─▶ gestrip faccia INTERNA (127.0.0.1:porta-A)
 clienti, artigiani, acquirenti ─Internet─▶ Tailscale Funnel :443 ─▶ gestrip faccia PUBBLICA (127.0.0.1:porta-B)
                                                                      │
                     gestrip (un binario Go, niente CGO) ─────────────┤
                       ├─ SQLite (modernc, file sul disco LUKS)       │
                       ├─ cartella media (foto, firme, PDF)           │
                       ├─ job interni (ritardi, solleciti, backup…)   │
                       └─ sorveglia i sidecar su 127.0.0.1:           │
                            llama-server (estrazione campi)           │
                            whisper.cpp / sherpa-onnx (facoltativo)   │
                          CUPS (`lp`) per la MUNBYN                    │
```

### 1.2 Pacchetti Go

La regola è un pacchetto per area di dominio. Le pagine web stanno separate dalle regole, così
le regole si provano senza browser.

| Pacchetto | Cosa fa |
|---|---|
| `cmd/gestrip` | il binario con i suoi comandi: `serve`, `import`, `backup`, `restore`, `rollback`, `reset-link` (D-04), `verifica-modello`, `image-worker` |
| `internal/clock` | l'unico orologio del negozio, Europe/Rome (vedi 1.7) |
| `internal/db` | apertura di SQLite, migrazioni, transazioni, filtro «vivo» del cestino |
| `internal/audit` | registro delle azioni, conservazione di 5 anni (D-14) |
| `internal/accounts` | utenti, ruoli titolare e dipendente, sessioni, inviti, reset, password PBKDF2 |
| `internal/repairs` | buste, numerazione N/AA, stati e transizioni, clienti, compro oro, stima dei tempi, cestino |
| `internal/media` | archivio a indirizzo di contenuto (SHA-256), foto, firme, miniature, conservazione |
| `internal/imaging` | normalizzazione delle foto e miniature WebP, in un processo figlio con limiti |
| `internal/pdf` | motore di impaginazione e i documenti: ricevuta, scheda, etichette, report, vendite, emergenza |
| `internal/printing` | interfaccia `Printer`, implementazione CUPS e una finta per i test |
| `internal/publiclinks` | token `/r/`, `/w/`, `/lotto`, password brevi `?pw=`, firme Flask (HMAC) |
| `internal/ratelimit`, `internal/clientip` | limite ai tentativi falliti e indirizzo vero del cliente |
| `internal/artisans`, `internal/lots` | artigiani, consegne, solleciti, oreficerie, lotti |
| `internal/sales` | vendite, garanzie, numerazione `V-AAAA-NNNN`, revisioni sigillate, revoche |
| `internal/emergency` | registro dei corrispettivi di emergenza, CSV, pacchetto PDF |
| `internal/outgoing` | coda in uscita, email SMTP, Telegram, i 9 testi WhatsApp |
| `internal/jobs` | pianificatore interno: orario di negozio, recupero, D-16 e D-17 |
| `internal/assist` | estrazione dei campi, date relative (nuovo, da `consegna.py`), trascrizione |
| `internal/sidecar` | avvio, controllo di salute e riavvio dei sidecar |
| `internal/backup` | backup a caldo, cifratura age, verifica, registro dei backup verificati, ripristino isolato |
| `internal/update` | manifest firmato, download, applicazione, ritorno indietro |
| `internal/importer` | import dallo standalone, con report di riconciliazione |
| `internal/web/internal`, `internal/web/public` | le due facce, ognuna con le sue rotte |
| `internal/web/ui` | template `html/template`, CSS e JS incorporati con `embed` |
| `tools/copertura`, `bench/` | strumenti di sviluppo: copertura dell'inventario e banco a scatola chiusa |

### 1.3 Binario unico

- `CGO_ENABLED=0`, compilazione riproducibile (`-trimpath`, versione e commit scritti nel
  binario). Il prototipo del tema H produce un ELF **statico** di 16 MB.
- Template, CSS, JS (htmx, jsQR), font dei PDF, icone e migrazioni sono incorporati con
  `embed`. Sul disco non serve nient'altro.
- Il binario si compila anche per Windows e macOS, ma solo per lo sviluppo (v2).

### 1.4 Database e migrazioni

- SQLite in puro Go `modernc.org/sqlite` (licenza BSD-3, niente CGO), in chiaro sul disco
  cifrato. Modalità WAL, `foreign_keys=ON`, `busy_timeout`. Un solo collegamento scrive, gli
  altri leggono, così le scritture non si pestano i piedi.
- **Migrazioni proprie**, piccole: file `.sql` numerati incorporati nel binario e una tabella
  `schema_migrations`. Si va solo avanti, mai indietro. Prima di ogni migrazione si fa un
  backup a caldo. Niente libreria esterna (goose e migrate fanno molto più del necessario).
- Lo schema copia le **regole** dello standalone: CHECK, indici unici, trigger. Per esempio le
  revisioni di garanzia immutabili e i contatori `repair_status_versions`. Non copia per forza
  i nomi Ecto. L'import traduce.
- Un **filtro «vivo» unico** per buste e vendite nel cestino (RIP-S-50), messo in `internal/db`
  dalla prima slice, così nessuna pagina dimentica il cestino.

### 1.5 Le due facce di rete

- Due server HTTP nello stesso processo, **ognuno con il suo elenco di rotte**, in ascolto
  solo su `127.0.0.1`. La faccia interna non può servire una rotta pubblica e viceversa. Un
  test confronta i due elenchi e fallisce se si toccano, a parte asset e `/healthz`.
- **Faccia pubblica:** `/r/`, `/stato`, `/foto` e `/thumb` legacy, `/artigiano`, `/lotto`,
  `/w/`, `/g/`, `/m/` (pagina «ritirato» e `sw.js`), `/privacy`. Pubblicata da
  `tailscale funnel` sulla **443**. Pagine che funzionano senza JavaScript, CSRF senza login,
  `no-store`, `no-referrer`, nessun log dei percorsi.
- **Faccia interna:** tutto il resto, compresi `/invito` (tema C) e, da decidere, `/login` e
  il reset (domanda 13). Pubblicata da `tailscale serve` solo nella tailnet.
- **Attenzione, conseguenza tecnica:** Funnel e Serve usano la stessa configurazione per porta.
  Se il pubblico sta sulla 443, l'interno **non** può stare sulla 443. Propongo l'interno sulla
  **8443** della tailnet (`https://negozio.tailnet.ts.net:8443`). Il personale lo vede una volta
  sola, perché l'app si installa come PWA. Va verificato con un prototipo sulla macchina di prova
  (slice 1).
- Se Funnel è spento, le funzioni pubbliche si spengono in modo pulito e l'app propone il testo
  WhatsApp al posto del link (v2).

### 1.6 Indirizzo vero del cliente dietro Funnel (CLI-21)

- Lo standalone legge `X-Forwarded-For` solo se la connessione arriva da un indirizzo privato,
  e prende la voce più a destra che non è privata.
- Con Funnel la connessione arriva da `tailscaled` su `127.0.0.1`. Secondo la documentazione
  e i casi pubblici, Funnel aggiunge `X-Forwarded-For` con l'indirizzo del visitatore e
  `Tailscale-Funnel-Request: ?1`. Le fonti sono in fondo al documento.
- **Regola proposta, la stessa dello standalone:**
  - la faccia pubblica ascolta **solo** su loopback, quindi l'unico vicino possibile è
    `tailscaled`;
  - si prende la voce più a destra di `X-Forwarded-For`;
  - IPv6 conta per `/64`, un IPv4-mapped si legge come IPv4;
  - gli indirizzi `100.64.0.0/10` della tailnet **non** contano come privati, come oggi
    (né 127/8, né 10/8, né 172.16/12, né 192.168/16): un membro della tailnet conta come un
    visitatore qualunque.
- **Da provare dal vivo nella slice 1**, sulla macchina di prova e non sulla produzione: si
  registra quale intestazione arriva davvero, da IPv4 e da IPv6. Il limitatore non si dichiara
  chiuso senza questa prova.

### 1.7 Orologio unico Europe/Rome (JOB-01)

- `internal/clock` dà `Now()` e `Today()` nel fuso Europe/Rome. Il fuso viene dai dati
  `time/tzdata` incorporati nel binario, quindi non dipende dal fuso del sistema.
- Nessun altro pacchetto chiama `time.Now()`: un test sul codice lo vieta. Nei test l'orologio
  è finto e si sposta a mano (giorni di chiusura, cambio dell'ora, fine anno).
- Le eccezioni dichiarate dello standalone si copiano come sono. Esempio: la scadenza dei token
  `/w/` alle 23:59:59.999999 **UTC** (VEN-25).

### 1.8 Sidecar dell'IA

- `internal/sidecar` avvia `llama-server` (e, se installata, la trascrizione) come processi
  figli su `127.0.0.1` con una porta alta. Controlla `/health` e lo riavvia con attese
  crescenti se cade. GestRip resta in piedi anche se il sidecar non parte.
- L'estrazione dei campi parla l'API compatibile OpenAI: `temperature 0`, timeout 90 s. Il
  titolare può indicare un server esterno (tema G). Il codice Go fa le regole deterministiche:
  date relative, telefono, nome, validazione JSON e un nuovo tentativo (vedi la domanda 7 sulle
  deviazioni).
- Interfaccia `Transcriber` con due motori (whisper.cpp su Vulkan e Parakeet su CPU, decisione
  aperta 3). **Lacuna trovata:** il browser registra in webm, ogg o mp4, mentre whisper.cpp e
  sherpa-onnx vogliono audio PCM. Proposta: il browser registra direttamente WAV mono a
  16 kHz (AudioWorklet), così non serve ffmpeg. Alternativa: ffmpeg come sidecar in più.
- Nei test i sidecar sono finti (server HTTP di prova). Le prove con i modelli veri girano con
  `-ngl 0` (solo CPU) e sotto `systemd-run` con limiti, su porte sopra 47000, senza mai usare i
  modelli o le porte di gestrip-switch, gestrip-e2b, llama-text e llama-embed.

### 1.9 Dove stanno i file sull'apparecchio

```
/var/lib/gestrip/           (dentro il volume LUKS; all'avvio si controlla che lo sia)
  gestrip.db                database
  media/                    foto, firme, PDF, logo (indirizzo SHA-256, permessi 0400)
  cache/thumbnails/         miniature, fuori dal backup
  secrets/                  segreto Flask, chiave pubblica age, segreti dell'apparecchio (0600)
/opt/gestrip/releases/<v>/  binari e sidecar per versione; `current` punta a quella attiva
/var/lib/gestrip-models/    modelli GGUF verificati per SHA-256 (fuori dal backup)
```

---

## 2. Tema H: scelte tecniche

Per ogni scelta indico 2 o 3 opzioni, i pro e i contro, la raccomandazione, la licenza, lo stato
di manutenzione al 30/10/2026 (ultimo aggiornamento su GitHub) e se serve CGO. Le scelte
segnate con ✅ sono state provate nel prototipo usa e getta, che è fuori dal repo.

### 2.1 PDF senza Chromium

**Documenti da fare** (misure dallo standalone):

| Documento | Formato |
|---|---|
| ricevuta | 80×297 mm |
| scheda | 100×230 mm |
| etichetta busta e foglio di consegna | 76×127 mm, margine 3 mm |
| report | A4, una pagina per busta, massimo 50 buste |
| etichette prenumerate | 16–200 mm, preset 57×32, QR di lato min(l,a)−4 mm |
| documento di garanzia | A4 con QR da 30 mm e fino a 6 foto |
| ricevuta di emergenza | A4 |
| riepilogo di emergenza | A4 |
| pacchetto di emergenza | il riepilogo con il CSV allegato in aggiornamento incrementale |

| Opzione | Pro | Contro | Licenza · manutenzione · CGO |
|---|---|---|---|
| **A. `signintech/gopdf` + un piccolo motore di impaginazione nostro** (righe, caselle con a capo, tabelle) + QR vettoriale `boombuler/barcode` | basso livello, controllo al millimetro; font TTF incorporati (€ e accenti); output con xref classica, utile per il pacchetto con CSV; ✅ etichetta 57×32 = 161,57×90,71 pt esatti, QR vettoriale | il motore delle caselle va scritto e testato (qualche centinaio di righe) | MIT · attivo (25/10/2026) · no CGO. QR: MIT · attivo (18/10/2026) |
| B. `johnfercher/maroto` v2 (griglia a righe e colonne sopra fpdf) | layout tabellare pronto | meno controllo sulle misure fini (etichette); dipende dal motore fpdf, il cui originale `go-pdf/fpdf` è archiviato | MIT · attivo · no CGO |
| C. `tdewolff/canvas` (grafica vettoriale con impaginazione del testo) | testo di ottima qualità, molte uscite | libreria grande, API meno stabile, più del necessario | MIT · attivo · no CGO |

**Raccomandazione: A.** Per i test si usa `pdfcpu` (Apache-2.0, attivo, no CGO) solo nei test:
estrae il testo e controlla dimensioni della pagina e allegati. Il pacchetto di emergenza si fa
come nello standalone: aggiornamento incrementale scritto a mano (`/Prev`, EmbeddedFile
`text/csv`), che rifiuta in modo chiuso ciò che non sa estendere.

Font proposto: **DejaVu Sans** (licenza Bitstream Vera, libera, già usata nell'immagine dello
standalone) o Noto Sans (OFL). Helvetica-Bold per il numero delle etichette è uno dei font
standard del PDF, quindi non va incorporato.

**Cosa conta come «uguale» nei PDF** (proposta da approvare, domanda 7):
- stesso formato della pagina;
- stesso testo estratto, normalizzato;
- stesso contenuto del QR;
- stesse posizioni chiave entro ±0,5 mm dove la misura è una regola (le etichette).

Non contano i byte né la resa grafica fine di Chromium.

### 2.2 Foto e miniature senza Python

Regole da rispettare, dallo standalone:
- **Ingresso:** JPEG, PNG o WebP a fotogramma singolo, al massimo 25 MiB e 60 MP.
- **Normalizzazione:** rotazione secondo l'EXIF, poi via EXIF, GPS e ICC; lato lungo massimo
  4096; JPEG qualità 90; uscita massima 16 MiB.
- **Miniatura:** WebP dentro un riquadro di 400 px, qualità 70.
- **Conservazione:** compressione a 1280 px nello stesso formato (JPEG q80, PNG, WebP q80).
- **Limiti di esecuzione:** 512 MiB di memoria e 8–10 s di tempo.

| Opzione | Pro | Contro | Licenza · manutenzione · CGO |
|---|---|---|---|
| **A. Libreria standard Go** (JPEG, PNG) + `golang.org/x/image` (decodifica WebP, ridimensionamento CatmullRom) + `gen2brain/webp` per **scrivere** WebP (libwebp trasformato in Go puro), compilato con `-tags nodynamic` | ✅ provato: binario statico, miniatura 400 px in circa 3 KB; legge anche l'orientamento EXIF; WebP con perdita come oggi | binario più grande; più lento di libwebp nativo (0,14 s per miniatura nel prototipo, accettabile) | MIT (libwebp BSD-3 dentro) · attivo (15/08/2026) · no CGO con `nodynamic` |
| B. Come A, ma WebP **senza perdita** con `HugoSmits86/nativewebp` | Go puro e semplice | miniature molto più pesanti; formato diverso da oggi | MIT · attivo · no CGO |
| C. Miniature JPEG invece di WebP | solo libreria standard | è una **deviazione** (cambia il tipo servito) e servirebbe una riga D | — |

**Raccomandazione: A.** Le misure di memoria contano: una foto da 60 MP decodificata occupa
circa 240 MB. La conversione gira quindi in un **processo figlio** dello stesso binario
(`gestrip image-worker`) con limite di memoria e di tempo, come gli script Python di oggi. Se
una foto malevola fa esplodere la memoria, cade il figlio, non il negozio.

La libreria standard non ha l'opzione `optimize` di Pillow né il trucco `draft()` per i JPEG
enormi. Cambiano un po' le dimensioni in byte, non il comportamento: va scritto nei test.

`chai2010/webp` è scartata perché richiede CGO.

### 2.3 Stampa CUPS dietro l'interfaccia `Printer`

```go
type Printer interface {
    List(ctx) ([]Queue, error)                  // code CUPS disponibili
    Print(ctx, queue string, pdf []byte, opt Options) (JobID, error)  // un solo lavoro
}
```

| Opzione | Pro | Contro | Licenza · CGO |
|---|---|---|---|
| **A. Comandi `lp`, `lpstat -e`, `lpoptions -p X -l` lanciati con `LC_ALL=C`** | come `stampa-etichette` oggi; l'uscita in inglese fisso risolve il problema italiano o inglese; «a buon fine» = `lp` esce con 0 e risponde `request id is …` | dipende dal pacchetto `cups-client` sull'apparecchio (va comunque installato) | nessuna dipendenza Go |
| B. Protocollo IPP diretto verso `localhost:631` (`phin1x/go-ipp`) | niente comandi esterni, stato del lavoro più ricco | più codice e più casi; driver ed eccezioni meno collaudati | Apache-2.0 · attivo · no CGO |

**Raccomandazione: A**, con la stampante finta per i test e una prova vera sulla MUNBYN
ITPP130 a fine slice etichette.
- `PageSize=Custom.<w>x<h>` si calcola dai millimetri, mai scritto fisso.
- Le altre opzioni (`MediaTracking=Gap`, `Darkness=8`) si configurano per stampante.
- Un solo `lp` per tutto il gruppo (D-08, D-09). Il contatore si salva nella stessa transazione
  dell'audit, solo se `lp` ha accettato il lavoro.

### 2.4 Firme Flask (HMAC) per i link vecchi

Questa scelta non ha alternative serie: si usa la libreria standard (`crypto/hmac`,
`crypto/sha256`, `crypto/subtle`).

**Algoritmo, letto dallo standalone:**
- HMAC-SHA256 con il segreto Flask, in esadecimale minuscolo;
- messaggio = parti unite con `:` più la scadenza;
- `stato:<busta>:<exp>`, troncato a 32 caratteri, nel parametro `t`;
- foto `<slug>:<file>:<exp>` nel parametro `sig`; il Go **firma** di nuovo con validità
  1 ora, perché la pagina `/stato` le serve ancora;
- `garanzia:<id>:<exp>` e `vendite/<id>:<file>:<exp>`;
- `artigiano:<busta>:<exp>`, troncato a 16, nella forma `t=<exp>.<16hex>`;
- `exp` fatto solo di cifre, normalizzato; valido se `exp ≥ adesso`.

**Regole sul file del segreto:**
- file regolare, non un link, permessi 0600, da 16 a 4096 byte;
- senza il segreto si rifiuta tutto;
- si confronta a tempo costante;
- il segreto non finisce mai nei log;
- va nel backup cifrato.

**Test:** i vettori sintetici dello standalone (`synthetic-legacy-secret`, exp 2000000000),
per esempio `garanzia:4711` → `be1ef8346aa9e1af8b075773dfe98b1a`.

I link `Phoenix.Token` non si accettano (D-02). Il Go riconosce però la loro forma e risponde
con la pagina di ripiego.

### 2.5 Backup a caldo, cifratura age, registro dei backup verificati

| Parte | Opzioni | Raccomandazione |
|---|---|---|
| Copia del database a caldo | A. `VACUUM INTO` (✅ provato: copia coerente con l'app accesa, integrità `ok`); B. API di backup online di SQLite tramite il driver; C. fermare l'app (lo fa oggi, 2-6 minuti) | **A**: un'istruzione SQL, copia già compattata. C è esclusa da D-10 |
| Cifratura | A. libreria `filippo.io/age` dentro il binario; B. comando `age` esterno | **A**: BSD-3, attiva (29/08/2026), no CGO, stesso formato del comando `age`, quindi si decifra anche a mano |
| Contenuto | tar con database, media, impostazioni, segreti (compreso il segreto Flask, D-10) e `inventory.json` con gli SHA-256 | come lo standalone, più i segreti |
| Verifica | decifra in una cartella temporanea, `PRAGMA integrity_check`, conteggi per tabella, SHA-256 dei media | uguale a oggi |
| Registro dei backup verificati (D-11) | tabella `backup_verifications`: ora, impronta dell'archivio, destinazione, conteggi, esito | sblocca il cestino definitivo e la pulizia delle foto solo con un backup verificato **dopo** il cestinamento |
| Destinazioni | chiavetta USB sempre inserita; remoto: A. SFTP (`golang.org/x/crypto/ssh`, Go puro); B. S3 compatibile; C. WebDAV | **USB + SFTP nella v1**, S3 più avanti se un collega lo chiede (domanda 15) |
| Calendario | ogni notte fuori dall'orario di negozio; 7 giornalieri e 4 settimanali; allarme oltre 26 ore; prova di ripristino settimanale (D-15) | come deciso nel tema E |

**Media grandi:** con molte foto, un tar completo ogni notte può pesare diversi GB.

Si parte con archivi completi, che sono più semplici e si ripristinano da soli. Le dimensioni
vere si misurano sulla copia dei dati nella slice 2. Se servono, si passa a «completo
settimanale più solo i file nuovi»: i media sono già a indirizzo SHA-256, quindi un incrementale
è semplice da verificare.

### 2.6 Aggiornamenti firmati e ritorno indietro

| Opzione | Pro | Contro |
|---|---|---|
| **A. Cartelle per versione** (`/opt/gestrip/releases/<v>`) + collegamento `current` + servizio systemd + comando `gestrip update` | ritorno indietro = rimettere il collegamento; facile da spiegare e da provare; lo stesso pacchetto vale da internet e da chiavetta USB | il controllo d'avvio e il giornale (OPS-22) vanno scritti |
| B. Pacchetti `.deb` da un repository apt firmato | strumenti standard di Ubuntu | apt non torna indietro da solo; serve gestire un repository |
| C. Sostituzione del binario sul posto (`minio/selfupdate`) | pochissimo codice | non copre sidecar e modelli; ritorno indietro debole |

**Raccomandazione: A.**
- **Manifest:** JSON con versione, canale (beta o stabile), file e SHA-256, firmato con
  minisign (ed25519). La chiave pubblica sta nel binario. Libreria `aead.dev/minisign` (MIT) o
  `jedisct1/go-minisign` (MIT, più aggiornata): si sceglie alla slice.
- **Applicazione:** solo fuori dall'orario di negozio.
  1. Backup a caldo.
  2. Si scrive il giornale `transaction.json` con fsync, che resiste a un blackout.
  3. Si sposta il collegamento e si riavvia.
  4. `/healthz` deve rispondere `ok` entro un tempo fissato, altrimenti si torna indietro da
     soli.
- **Migrazioni:** vanno solo avanti. Tornare a una versione precedente dopo una migrazione vuol
  dire anche ripristinare il backup fatto prima dell'aggiornamento. Per questo la procedura
  guidata (D-13) mostra cosa si perde, fa prima un backup dello stato attuale e chiede una
  parola di conferma. Resta anche il comando da console.

---

## 3. Slice ordinate

Le slice sono piccole e verticali, come nello standalone (vedi 9). Qui sotto ci sono le
**tappe**: ognuna si spezza in più slice da una sessione. Gli ID tra parentesi sono le voci
dell'inventario che la tappa chiude. Ho tenuto l'ordine proposto da Stefano. Ho cambiato una
cosa sola: l'import parte presto ma **cresce con le aree** (vedi T2).

Legenda del rischio: 🟢 basso · 🟡 medio · 🔴 alto.

### T0 — Banco e strumenti (prima di tutto, piccola)
- **Obiettivo:** CI locale, script di copertura dell'inventario (sezione 5), scheletro del banco
  a scatola chiusa con un primo scenario banale (`/healthz`).
- **Voci:** nessuna, è infrastruttura.
- **Test:** lo script di copertura legge le 353 voci dell'inventario e trova gli ID nei test.
- **Prova a scatola chiusa:** `/healthz` risponde sia sullo standalone isolato sia sul Go.
- **Rischio:** 🟢.

### T1 — Scheletro
- **Obiettivo:**
  - binario, configurazione, le due facce con elenchi di rotte separati;
  - SQLite e migrazioni, orologio unico;
  - login e ruoli, sessioni e «chiudi tutte», CSRF, intestazioni di sicurezza;
  - limitatore e indirizzo del cliente, audit, `/healthz`;
  - configurazione guidata del primo titolare su database vuoto.
- **Voci:** ACC-01…05, ACC-15, ACC-16 (sostituita dalla procedura guidata), ACC-17…20,
  CLI-21 (parte limitatore), ADM-01, ADM-08 (parte guscio PWA), ADM-16, ADM-17 (D-14),
  ADM-20, ADM-22, JOB-01, OPS-12 (controllo del volume cifrato), OPS-13.
- **Test:**
  - password PBKDF2 compatibili con gli hash dello standalone (vettore sintetico);
  - rifiuto degli hash sotto 600.000 iterazioni;
  - tabella dei permessi di ACC-05;
  - parametri del limitatore (10/15 min, 30/min, 5 e 20 sul login, blocco di 15 minuti);
  - 404 di blocco identico al 404 normale (D-05);
  - orologio finto;
  - divieto di `time.Now()` fuori da `internal/clock`.
- **Prova a scatola chiusa:** login giusto e sbagliato, ruoli, blocco dopo N tentativi,
  intestazioni.
- **Prova dal vivo:** indirizzo vero dietro Funnel (1.6) e porta interna 8443, su una macchina di
  prova con un account Tailscale di prova, **mai la produzione**.
- **Rischio:** 🟡 (Funnel e porte da verificare).

### T2 — Import, primo giro, e poi a ogni area
- **Obiettivo:** `gestrip import` dal SQLite in chiaro dello standalone. Lo schema Go di
  **tutte** le tabelle si scrive subito, perché l'import deve rifiutare una tabella sconosciuta
  (OPS-01…05). La logica delle aree arriva dopo.
  - Il primo giro porta utenti, impostazioni e audit, più le altre tabelle «come dati», con
    riconciliazione completa: conteggi, totali, media mancanti o in più, righe scartate con il
    motivo, verdetto. Se qualcosa non torna, non attiva nulla.
  - A ogni tappa successiva l'import di quell'area si arricchisce (token, timbri, impostazioni)
    e il banco lo controlla.
- **Voci:** OPS-01…05, OPS-07, OPS-10 (sostituita da `gestrip import`), OPS-29, OPS-02 (D-03,
  canali spenti), JOB-08 (`jobs.overdue.silenced_history`), CLI-22 (token in chiaro e hash).
  OPS-06 resta da decidere (domanda 19).
- **Test:**
  - import di un database sintetico con la struttura dello standalone, con riconciliazione a
    zero differenze;
  - secondo import identico al primo;
  - database corrotto o incompleto che fallisce senza attivare nulla;
  - tabella sconosciuta che blocca l'import.
- **Prova su una copia dei dati veri:** **solo con l'autorizzazione esplicita di Stefano e dove
  dice lui** (domanda 6). La decifratura con `sqlcipher` si fa su una copia, il file in chiaro sta
  solo su disco cifrato e si cancella alla fine. Nel repo vanno solo il report anonimo (conteggi)
  e i casi che fanno emergere, trasformati in dati inventati.
- **Rischio:** 🔴 (dati veri, casi inattesi). Proprio per questo va fatto presto.

### T3 — Banco a scatola chiusa, prima versione
- **Obiettivo:** il banco della sezione 4 gira su standalone isolato e Go con gli **stessi dati
  sintetici**, ottenuti con la catena «cattura Flask sintetica → standalone → import Go».
- **Voci:** nessuna voce di prodotto. Chiude invece le voci già fatte sul lato scatola chiusa.
- **Rischio:** 🟡 (serve un'istanza dello standalone isolata: domanda 5).

### T4 — Funzioni per aree

Ogni area è una serie di slice e alla fine di ogni area il banco deve essere verde. L'ordine
segue le dipendenze.

| Tappa | Obiettivo | Voci | Dipende da | Rischio |
|---|---|---|---|---|
| **T4.1 Nucleo della busta** | nuova busta da modulo, numerazione N/AA, clienti, oreficeria, artigiano, preventivo, consegna prevista, compro oro, QR incentivato, stati e transizioni, scheda e modifica, riapertura, cestino «vivo» | RIP-H-29…39, 41, 56; RIP-S-01, 02, 04…15, 18, 24 | T1 | 🟡 |
| **T4.2 Home e QR** | lista, ricerca, filtri, contatori, ordinamento, paginazione, galleria, pulsanti rapidi, 📌, ricerca per codice QR, pannello QR, lettore con la fotocamera | RIP-H-01…16, 20…28 | T4.1 | 🟢 |
| **T4.3 Foto e media** | archivio SHA-256, normalizzazione e miniature (tema H), visibilità per canale, caricamento in background, cache, pulizia | RIP-H-40; RIP-S-28…35, 57 | T4.1 | 🟡 |
| **T4.4 Stampe, etichette e firma** | motore PDF, ricevuta, scheda, etichetta 76×127, report (D-01), etichette 57×32 con contatore `max` e limite di 100, conferma «Etichette stampate correttamente?» (D-08), CUPS, firma di consegna | RIP-H-19; RIP-S-36…48 | T4.1, T4.3 | 🟡 |
| **T4.5 Pagina cliente e link** | `/r/`, `/stato` con firme Flask e password brevi, preventivo approva/rifiuta e revoca, conversazione e aggiornamento automatico, foto pubbliche, `/m/` ritirato e `sw.js`, `/privacy`, anteprima cliente | CLI-01…07, 09…12, 15…28, 53, 54; RIP-S-19…25, 27 | T4.1, T4.3 | 🔴 (link già mandati) |
| **T4.6 WhatsApp, coda in uscita, email** | 9 testi `wa.me` con l'editor e il validatore dei segnaposto, coda durevole con tentativi, SMTP, iscrizione, conferma e disiscrizione con la pagina di ripiego (D-02), riquadro email solo con SMTP (D-16) | CLI-29…43, 51, 52; RIP-H-17; RIP-S-20, 21, 23; ADM-09, ADM-13 | T4.5 | 🟡 |
| **T4.7 Telegram** | collegamento del bot, long polling, risposte, avvisi, popup dopo il cambio di stato, pulizia | CLI-08, 13, 14, 44…50; RIP-H-18; ADM-10 | T4.6 | 🟡 |
| **T4.8 Artigiani** | anagrafica, consegne con etichette e stampa CUPS (D-09), storico e giri (tema H, ART-19), rientri, solleciti, pagina artigiano pubblica e link Flask | ART-01…26; RIP-H-24; RIP-S-16, 17 | T4.4, T4.5 | 🟡 |
| **T4.9 Oreficerie, lotti e lavori automatici** | anagrafiche, lotti e `/lotto`, pianificatore con orario di negozio (D-17) e canali spenti (D-16), ritardi, solleciti, briefing | ORE-01…16; JOB-02…11; ADM-02 | T4.6, T4.7, T4.8 | 🟡 |
| **T4.10 Dettato AI e stima** | estrazione dei campi, regole del codice, date relative (nuovo, banco di `consegna.py`), sidecar finto e vero, trascrizione facoltativa, raccolta dei dettati spenta di default (D-07), banco modello, stima dei tempi | RIP-H-42…52, 54, 55; RIP-S-03, 26; CLI-03; ADM-04, ADM-11, ADM-12 | T4.1 | 🔴 (deviazioni non ancora scritte: domanda 7) |
| **T4.11 Cestino e conservazione** | cestino, ripristino, eliminazione definitiva legata al registro dei backup (D-11), conservazione delle foto | RIP-S-49…56; RIP-H-51; ADM-14, ADM-15 | T4.3, T5.2 (registro backup) | 🔴 (cancellazioni irreversibili) |
| **T4.12 Vendite con garanzia** | registro delle vendite, bozza ed emissione `V-AAAA-NNNN`, revisioni sigillate e revocabili, PDF col QR, cestino delle vendite, chiusura e riapertura | VEN-01…20, 32, 33 | T4.3, T4.4, ADM-02 | 🟡 |
| **T4.13 Garanzie pubbliche** | `/w/`, `/w/current`, foto, `/g/` Flask, rifiuto unico | VEN-21…31; OPS-27 | T4.12 | 🔴 (QR già nei pacchi; decisione aperta 10) |
| **T4.14 Registro di emergenza** | eventi, righe, storni, scorporo IVA, numerazione, ricevute, riepilogo, CSV e pacchetto PDF | EME-01…23 | T4.1, T4.4 | 🔴 (regole fiscali da copiare esatte) |
| **T4.15 Amministrazione** | utenti e inviti (sulla faccia interna), reset con il comando locale (D-04), profilo del negozio, logo e colori, PWA, testi, impostazioni | ACC-06…14; ADM-03, 05…07, 23 | T1 | 🟢 |

### T5 — Apparecchio
- **T5.1 Sidecar e modelli:**
  - supervisore;
  - download con SHA-256 e ripresa;
  - `THIRD_PARTY_NOTICES`;
  - verifica di un modello candidato sul banco dei dettati.
- **T5.2 Backup:**
  - backup a caldo cifrato e verificato, su USB e SFTP;
  - registro dei backup verificati, allarme, prova settimanale;
  - ripristino isolato fino alla conferma (D-10, D-11, D-12, D-15).
  - Voci: OPS-16…19, OPS-28.
  - Questa parte va anticipata prima di T4.11, perché il cestino definitivo ne dipende.
- **T5.3 Aggiornamenti e ritorno indietro:** manifest minisign, canali, pacchetto USB, ritorno
  automatico e procedura guidata (D-13). Voci: OPS-20…23.
- **T5.4 Installer:**
  - chiavetta o immagine, LUKS2 e TPM2, chiave di recupero e foglio da stampare;
  - ri-registrazione del TPM dopo gli aggiornamenti del kernel;
  - Tailscale serve e Funnel, servizio systemd, NUT e UPS.
  - Voci: OPS-14, OPS-15, OPS-25, OPS-26.
  - **Rischio 🔴.** Si prova solo su macchine di prova (pc-fiacco, N150, UDOO), mai su pc-fisso.
- **T5.5 Stampa vera:** collaudo sulla MUNBYN ITPP130, driver CUPS su Ubuntu 26.04 o raster
  generico.

### T6 — README e pagina di presentazione
README tecnico, guida d'installazione per un collega, pagina per orefici e orologiai (bozza in
6.3), `THIRD_PARTY_NOTICES` completo.

### Voci fuori dalle tappe v1
- **dopo-v1:** STA-01…05, ADM-18, ADM-19 (D-06), ACC-10 «Riattiva», ORE-03 eliminazione
  oreficeria, RIP-H-53. I loro dati si importano comunque.
- **mai:** ADM-21, OPS-11.
- Le voci «sostituita» si chiudono con il test della funzione che le sostituisce (per esempio
  OPS-16 → test del backup age).

### Ordine riassunto

```
T0 ─ T1 ─ T2(primo giro) ─ T3 ─┬─ T4.1 ─┬─ T4.2
                               │        ├─ T4.3 ─ T4.4 ─┬─ T4.5 ─ T4.6 ─ T4.7 ─ T4.8 ─ T4.9
                               │        │               └─ T4.12 ─ T4.13
                               │        ├─ T4.10            T4.14
                               │        └─ T4.15
                               └─ T5.2 (backup) prima di T4.11 ─ T5.1 ─ T5.3 ─ T5.4 ─ T5.5 ─ T6
         (T2 cresce a ogni tappa T4: l'import di ogni area si chiude con l'area)
```

---

## 4. Banco a scatola chiusa

### 4.1 Cosa è

Un programma separato dal prodotto (`bench/`) che parla con l'app solo via HTTP e browser, senza
guardare dentro il codice. Gli stessi scenari girano due volte, contro lo standalone e contro
il Go, e alla fine si confrontano i risultati.

### 4.2 Come gira

- **Stessi dati sintetici dalle due parti**, per costruzione:
  1. una cattura Flask **sintetica** generata da `test/fixtures/legacy_import/synthetic_capture.py`
     dello standalone (solo dati inventati);
  2. si importa nello standalone isolato con il suo `legacy_import`;
  3. si copia il suo database, lo si decifra (`sqlcipher`, chiave di prova) e lo si importa nel
     Go con `gestrip import`.

  Così il banco prova anche l'import a ogni giro.
- **Standalone isolato:** l'immagine al tag `go-baseline`, in una rete Docker `--internal`, CA
  propria, nessuna porta sull'host, come fa oggi la skill `gestrip-browser-gate`. La skill
  **non è su pc-fisso**: sta su **pc-lab** (`~/.claude/skills/gestrip-browser-gate`) e pilota
  pc-fisso via ssh. Non ho toccato nulla. Per usarla serve il permesso di Stefano (domanda 5),
  perché lavora con le immagini Docker dello standalone.
- **Go isolato:** il binario su porte sopra 47000, con una cartella dati temporanea.
- **Orologio:** lo standalone non si può fermare nel tempo, quindi gli scenari usano date
  relative («oggi meno 10 giorni») e il confronto normalizza le date.

### 4.3 Due livelli

1. **HTTP**, scritto in Go: client con cookie, CSRF letto dalle pagine, HTML analizzato. È
   veloce e deterministico, gira in CI, e copre la maggior parte dei casi: stati, testi,
   numeri, date, link, intestazioni, PDF scaricati.
2. **Browser** (Playwright con Chromium a versione fissata, come lo standalone): solo per ciò che
   ha bisogno di JavaScript, cioè lettore QR, coda delle foto, firma, aggiornamento automatico
   dei messaggi, PWA. Viewport desktop 1440×1000 e telefono 390×844.

### 4.4 Cosa confronta

| Cosa | Come |
|---|---|
| stato HTTP e redirect | uguali, con il percorso normalizzato rispetto all'host |
| intestazioni | `cache-control`, `referrer-policy`, `x-robots-tag`, `content-type`, `content-disposition` |
| testi | testo visibile delle zone principali, normalizzato (spazi); niente confronto dell'HTML intero |
| numeri e date | estratti e confrontati come valori |
| link | elenco degli `href` e dei `wa.me` con i testi decodificati, host normalizzato |
| PDF | formato della pagina, testo estratto, contenuto dei QR, allegati (pacchetto) |
| effetti | ciò che si vede dopo: audit consultabile, contatori, stato della busta |

### 4.5 Deviazioni D-xx

- Il file `bench/deviazioni.yaml` lega ogni D-xx agli scenari e alle asserzioni dove la
  differenza è attesa. Per esempio D-01: nel report Go manca «Nessuna foto allegata».
- Ogni differenza che non è scritta lì fa fallire il banco: è un bug.
- Una riga del file senza una D-xx approvata nell'inventario fa fallire il banco.

### 4.6 Dove vivono gli scenari

Nel repo, in `bench/scenarios/`, come file leggibili, con **solo dati inventati**. Nomi di
fantasia evidenti (es. «Oreficeria Esempio», «Mario Prova»), numeri di telefono di finzione,
nessuna foto vera: le immagini si generano nei test. Gitleaks e un controllo nel precommit
cercano email e telefoni che non stanno in un elenco di valori finti ammessi.

---

## 5. Copertura dell'inventario

- **Segno nel test:** un commento sopra ogni test, `// inventario: RIP-S-48`. Un test può
  coprire più voci. Gli scenari del banco portano lo stesso campo (`inventario: [RIP-S-48]`).
- **Script `tools/copertura`** (Go, nel repo):
  1. legge `inventario-funzioni.md` (una copia sincronizzata nel repo, `docs/inventario-funzioni.md`,
     solo testo, senza dati veri) e ricava ID e destino;
  2. trova i segni nei test e negli scenari;
  3. incrocia con l'esito di `go test -json` e del banco;
  4. stampa una tabella per voce, con questi stati:
     - **chiusa**: test Go verde, e banco verde dove si applica;
     - **parziale**: ha test, ma manca il banco o una parte;
     - **aperta**;
     - **fuori v1**.
- **Controlli automatici:**
  - un segno che punta a un ID inesistente blocca il precommit;
  - a ogni tappa il report va nella ricevuta della slice;
  - per l'uscita della v1 tutte le voci `v1` devono essere chiuse, altrimenti il rilascio non
    parte.
- **Allineamento:** quando il tag `go-baseline` si sposta, lo script mostra le voci toccate nel
  registro di allineamento. I loro test tornano «da ricontrollare» finché una slice non li
  conferma.

---

## 6. Pubblicazione e dono ai colleghi

### 6.1 Licenza

Lo scopo dichiarato da Stefano: «lo regalo ai colleghi e non voglio che qualcuno lo rivenda
chiuso».

| Licenza | Chi la riceve può… | Rivendita chiusa | Servizio online chiuso (SaaS) | Note |
|---|---|---|---|---|
| **MIT** | fare tutto, basta citare | **sì, possibile** | sì | la più semplice, non protegge dallo scopo |
| **Apache-2.0** (scritta oggi nel v2) | come MIT, più una clausola sui brevetti | **sì, possibile** | sì | non protegge dallo scopo |
| **GPL-3.0** | modificare e ridistribuire, ma chi distribuisce deve dare il sorgente con la stessa licenza | no | **sì, possibile**: chi lo offre online senza distribuirlo non deve dare il sorgente | protegge dalla rivendita di copie chiuse |
| **AGPL-3.0** | come GPL, e in più chi lo fa usare **via rete** deve offrire il sorgente | no | no | protegge anche dal «GestRip in abbonamento» chiuso |

**Raccomandazione: AGPL-3.0.** È l'unica che copre anche il caso di chi ne fa un servizio online
a pagamento senza dare le modifiche. Per i colleghi che lo usano in negozio non cambia nulla: non
devono pubblicare niente se non lo modificano. Se lo modificano, la pagina pubblica dell'app
deve offrire un link al sorgente, e il Go lo mette già di default (un piè di pagina «codice
sorgente»).

Compatibilità:
- Le librerie Go scelte sono MIT, BSD o Apache-2.0, tutte compatibili con (A)GPL-3.0.
- I sidecar (llama.cpp e whisper.cpp MIT, sherpa-onnx Apache-2.0) sono **programmi separati**,
  quindi la licenza di GestRip non li tocca e loro non toccano GestRip.
- I **pesi** hanno licenze proprie e si distribuiscono con il loro testo e la scheda modello:
  Qwen3.5 0.8B Apache-2.0, LoRA GestRip come opera derivata Apache-2.0 (v2), Parakeet CC-BY-4.0
  con attribuzione. Una licenza AGPL del codice non cambia quella dei pesi.

**Contraddizione da risolvere:** il v2 dice «licenza Apache 2.0». Se l'obiettivo è che nessuno lo
rivenda chiuso, Apache non basta. Decide Stefano (domanda 1).

Il nome «GestRip» si può proteggere a parte, con una regola d'uso del marchio nel README
(«le versioni modificate non si chiamano GestRip»). È facoltativo.

### 6.2 Quando renderlo pubblico

| Momento | Pro | Contro |
|---|---|---|
| Subito | trasparenza; la CI di GitHub è gratuita per i repo pubblici | si espone codice incompleto; un errore (un dato vero in un commit) resta pubblico per sempre |
| **Dopo l'import di prova sui dati veri (fine T2/T3)** | è il momento in cui è più facile che un dato vero scappi; dopo si è visto e ripulito | — |
| Alla v1 | prodotto completo da mostrare | mesi di storia mai controllata da occhi esterni |
| Alla prima installazione di un collega | — | troppo tardi per raccogliere aiuto |

**Raccomandazione:** repo GitHub **privato** appena Stefano lo crea, per avere la CI. Poi
**pubblico alla fine di T3**, dopo un controllo completo della history con gitleaks e una lettura
dei messaggi di commit. La pagina di presentazione va pubblicata alla v1 (domanda 2).

### 6.3 Pagina di presentazione (bozza dei contenuti)

Su `gestrip.app`, il dominio tenuto apposta, con GitHub Pages o una pagina statica. In italiano,
per orefici e orologiai, senza tecnicismi.

1. **Titolo:** «GestRip — le riparazioni del tuo negozio, in ordine. I dati restano in bottega.»
2. **Cosa fa** (con 3-4 immagini fatte su dati inventati):
   - buste con numero, foto e QR;
   - il cliente vede lo stato dal telefono e approva il preventivo;
   - consegne agli artigiani con etichette e solleciti;
   - avvisi su WhatsApp, email e Telegram;
   - firma alla consegna;
   - garanzie delle vendite online;
   - registro di emergenza dei corrispettivi.
3. **Il dettato:** scrivi o detta «anello oro rotto, signora Bianchi, pronto sabato» e GestRip
   compila la busta da solo. L'intelligenza artificiale gira nel negozio.
4. **L'apparecchio:** un mini PC silenzioso, sempre acceso (circa 10 W), da mettere sotto il
   banco, con la stampante di etichette. Si riaccende da solo dopo un blackout. Con un piccolo
   UPS si spegne pulito.
5. **I tuoi dati non escono dal negozio:**
   - disco cifrato;
   - backup cifrati sulla chiavetta e, se vuoi, altrove;
   - nessun abbonamento e nessuna nuvola di terzi;
   - una nota onesta sul microfono della tastiera del telefono e su quello locale.
6. **Requisiti e costi indicativi:**
   - mini PC (fascia N150 da circa 150-250 €, consigliato Ryzen 7 5825U da circa 300-400 €);
   - stampante di etichette termica (circa 100-150 €);
   - UPS piccolo (circa 60-80 €);
   - Tailscale gratuito per un negozio.

   I prezzi vanno verificati quando si scrive la pagina. GestRip è gratuito e libero.
7. **Come averlo:** installalo da solo seguendo la guida, oppure scrivi a Stefano per una mano
   (contatto da decidere: domanda 20).
8. **Chi lo fa:** un orefice che l'ha scritto per il suo negozio e lo regala ai colleghi.
9. Licenza, codice sorgente, privacy della pagina (nessun tracciamento).

### 6.4 Cosa serve a un collega per installarlo da solo, e cosa resta a Stefano

**Al collega:**
- il mini PC compatibile;
- la chiavetta d'installazione, da scaricare o ricevere;
- un account Tailscale;
- la stampante con driver CUPS;
- 30-60 minuti seguendo la procedura guidata dal browser;
- stampare e conservare il foglio con la chiave di recupero e la chiave dei backup.

Poi si aggiorna da solo.

**A Stefano (manutentore):**
- la chiave privata di firma degli aggiornamenti, offline e con una copia di sicurezza: se si
  perde, non si possono più spedire aggiornamenti;
- la promozione da beta a stabile;
- l'aiuto ai colleghi nei casi difficili (TPM dopo un cambio della scheda madre, ripristino);
- il modello addestrato e il banco dei dettati.

---

## 7. Rischi, ambiguità e contraddizioni

### 7.1 Decisioni aperte del v2 e impatto sul piano

| N. | Decisione | Impatto |
|---|---|---|
| 3 | motore di trascrizione locale | non blocca: T4.10 fa l'interfaccia `Transcriber` con il motore finto; il motore vero si sceglie dopo le misure su N150 e UDOO |
| 9 | modelli di mini PC e UPS | blocca solo T5.4 e T6 (README e pagina); servono le macchine di prova |
| 10 | prova dal vivo dei QR `/g/` sullo standalone di produzione | blocca la **chiusura** di T4.13 (VEN-28, 29). Il codice si fa coi vettori sintetici. La prova la fa Stefano, o io solo con un suo permesso esplicito, guardando solo l'esito |
| 12 | soglie (95%, 2 o 3 s, 5 s) | non blocca il codice; blocca il collaudo finale. **Incoerenza:** i criteri del v2 dicono «meno di ⟨2⟩ s», la decisione 12 dice «⟨3⟩ s» |

### 7.2 Contraddizioni trovate (da correggere nei documenti, non le decido io)

1. **Licenza:** il v2 dice Apache 2.0, ma lo scopo «non rivenduto chiuso» chiede (A)GPL (6.1).
2. **Commit di riferimento:** l'Obiettivo del v2 cita ancora `3415e90` su otto-vps. La testa
   del v2 e l'inventario usano `go-baseline` = `5c80347` su pc-fisso. Propongo `go-baseline`.
3. **Link `/artigiano` nel criterio d'import:** il v2 vuole che «un campione di link
   `/artigiano` già mandati» si apra uguale. Per D-02 i link artigiano nuovi (Phoenix.Token) non
   si accettano, quindi funzionano solo i vecchi Flask `/artigiano/<busta>/<anno>`. Va precisato.
4. **Utenti con la loro password:** l'import Flask→Elixir ha portato i dipendenti senza password.
   Nel Go entrerà con la sua password solo chi ha già un hash (di fatto il titolare). Gli altri
   vanno reinvitati.
5. **Deviazioni non registrate** che il v2 chiede ma lo standalone non fa (servono righe D-18 e
   seguenti, domanda 7):
   - date relative calcolate dal codice Go invece che dal modello (RIP-H-42 e 46);
   - un nuovo tentativo se la risposta del modello non è valida;
   - acconto e busta nelle note;
   - numero «sotto» il QR delle etichette 57×32 (lo standalone lo stampa a destra);
   - azzeramento manuale del contatore delle etichette con conferma (nello standalone non c'è);
   - l'«anno proposto e modificabile» (da verificare se coincide);
   - resa grafica diversa dei PDF senza Chromium (cosa conta come uguale, 2.1);
   - registrazione WAV dal browser per la trascrizione locale (1.8);
   - eventuale porta 8443 dell'interfaccia interna (1.5).
6. **Acconto:** la sezione «Fuori area» dice che compare in tre moduli, RIP-S-05 dice che non
   esiste un campo acconto, e il v2 vuole «totali di preventivi e acconti» nella riconciliazione.
   Lo verifico sullo schema in T2.
7. **Voci dell'inventario non riallineate** dopo le decisioni del 29/10: il corpo dice ancora
   «Da decidere» ma la testa le ha decise. Sono: RIP-H-13, 22, 38, 46, 49, 55; RIP-S-03, 11, 43,
   46, 47, 53, 55; CLI-03, 16, 31, 39, 40, 53; ART-05, 08, 12, 23, 26; ORE-03, 15; JOB-01, 02, 03,
   07, 08; STA-05; VEN-18, 30; ACC-09, 10, 13, 14; ADM-06, 17, 19, 23; OPS-02, 16, 17, 19, 21, 28.
   - Il riepilogo conta 352 voci, ma sono 353 (manca RIP-H-56).
   - Restano ancora «7 testi WhatsApp» in RIP-H-17 e ADM-13.
   - Restano ancora «nessun limite di frequenza» in ART-24, ORE-14 e ACC-01, superato dalla
     slice 208.

   Propongo di riallinearle io con un commit sull'inventario in `~/Condivisi`, dopo il suo ok.
8. **Glossario:** 4000 caratteri salvati (ADM-11) contro 2000 nella forma a una riga (tema G).
   Sembrano due limiti diversi, salvataggio e invio. Va confermato.

### 7.3 Probabili bug dello standalone: copiarli o correggerli prima?

Per la regola «nessun miglioramento non scritto» li copio **uguali** e li segno nei test come
«comportamento dello standalone», a meno che Stefano non chieda di correggerli prima nello
standalone, come ha fatto con le slice 208-213 (domanda 8).
- RIP-H-12: busta annullata o chiusa con data passata segnata «urgente».
- RIP-H-04: i filtri `scadenza` e `notifiche` si perdono premendo «Applica».
- RIP-H-03: ricerca `LIKE` senza escape di `%` e `_`.
- RIP-H-33: cliente e busta in due transazioni (cliente orfano possibile).
- RIP-S-42: preventivo «12.50 EUR» e prezzo «€ 12,50» sulla stessa ricevuta.
- EME-10 e 12: dopo lo storno di una riga legata a una busta, la busta resta bloccata per sempre.
- EME-13: un dipendente può eliminare una riga fiscale entro 5 minuti, senza controllo di ruolo.
- CLI-24: «Revoca link» e «Genera nuovo link» senza controllo di ruolo.
- VEN-20 e 21: email del venditore «se non nulla» nel PDF, «se non vuota» sulla pagina.
- CLI-17 e 28: `/stato` di una busta conto terzi mostra stato e foto «cliente». È voluto?

### 7.4 Punti tecnici aperti che chiudo io nelle slice
- Algoritmo e forma canonica del **sigillo** delle revisioni (`sale_revision_seals`): non sono
  nell'inventario, li leggo nel sorgente in T4.12.
- Gettoni della galleria dello staff (RIP-S-25, 33): oggi Phoenix.Token. Nel Go serve un gettone
  proprio, solo interno.
- Coda in uscita importata con i canali spenti (D-03): le righe `queued` restano ferme, non
  partono alla prima accensione senza conferma. Proposta da confermare (domanda 17).
- Orario di negozio e job (D-17): rispettano i **giorni** di apertura. Il briefing delle 08:00
  parte comunque prima dell'apertura nei giorni aperti. Il «recupero» vuol dire che il primo
  giorno aperto i job girano normalmente e trovano da soli ciò che è rimasto. Da confermare
  (domanda 18).
- Faccia di rete di `/login` e reset (domanda 13).
- `/stato/email-disattiva`: rientra nel limite di `/stato`? Lo leggo nel codice della slice 208.

### 7.5 Rischi principali

| Rischio | Effetto | Contromisura |
|---|---|---|
| un dato vero entra nel repo | non si cancella più da un repo pubblico | gitleaks e controllo dei valori finti nel precommit; repo pubblico solo dopo T3 e un controllo della history; dati veri mai in `~/sviluppo/gestrip-go` fuori da `to-be-deleted-after-use` |
| link già mandati che smettono di funzionare | clienti e acquirenti con QR morti | vettori sintetici, banco su ogni famiglia di link, campione reale in T2 con permesso, prova `/g/` (decisione 10) |
| Funnel non passa l'indirizzo vero o le porte non si combinano | limitatore inutile o interfaccia esposta | prototipo in T1 su una macchina di prova; test che la porta interna non risponde dal Funnel |
| PDF diversi da come li conosce il negozio | etichette che non stanno sulla MUNBYN | prova di stampa vera in T4.4 e T5.5; confronto al millimetro delle etichette |
| rallentare il dettato in negozio su pc-fisso | il negozio se ne accorge | ogni build e test sotto `systemd-run` con CPU e RAM limitate e `nice 19`; test solo CPU; mai le porte o i modelli in uso |
| TPM e LUKS su hardware vario | il PC non riparte da solo | solo su macchine di prova, ri-registrazione testata, chiave di recupero stampata |
| piano troppo lungo | stanchezza, slice che si allargano | tappe con banco verde; ogni slice una sessione; copertura misurata |

---

## 8. Decisioni per Stefano

**Per approvare il piano:**

1. **Licenza:** AGPL-3.0 (consigliata, protegge da rivendita e SaaS chiusi), GPL-3.0, oppure
   Apache-2.0 come nel v2 (non protegge). Se cambia, si corregge il v2.
2. **Quando pubblico:** repo GitHub privato subito per la CI, pubblico a fine T3 dopo il controllo
   della history, pagina alla v1 (consigliato)? Oppure un altro momento?
3. **Ordine delle tappe:** va bene T0 → T1 → T2 (import presto e poi per aree) → T3 → T4 per aree
   → T5 → T6, con il backup (T5.2) anticipato prima del cestino definitivo?
4. **Porta interna 8443:** visto che il pubblico deve stare sulla 443 di Funnel, va bene l'app
   interna su `…ts.net:8443`, da verificare in T1?
5. **Banco contro lo standalone:** posso far girare un'istanza **isolata e sintetica** dello
   standalone su pc-fisso, con l'immagine Docker di `go-baseline`, una rete interna e nessuna
   porta, come fa la skill `gestrip-browser-gate` di pc-lab? Oppure preferisci che giri su
   pc-lab?
6. **Import di prova sui dati veri (T2):** dove (pc-fisso, oppure una macchina di prova con disco
   cifrato), quando, e chi prepara la copia decifrata? Io non tocco i dati veri senza il tuo ok.
7. **Nuove deviazioni D-18 e seguenti** (elenco in 7.2 punto 5): le approvi così, una per una, o
   preferisci decidere alla tappa che le tocca? Includono il criterio «PDF uguali = formato,
   testo e QR, non resa grafica».
8. **Probabili bug dello standalone (7.3):** li copio uguali (regola di default) oppure ne vuoi
   correggere qualcuno prima nello standalone?
9. **Identità dei commit:** oggi l'autore usa l'email personale, come nello
   standalone. Per un repo pubblico conviene l'indirizzo «noreply» di GitHub. Lo cambio prima
   che la history cresca?
10. **Nome del modulo Go e del repo:** propongo `gestrip.app/gestrip` (indirizzo stabile sul tuo
    dominio, indipendente da GitHub) oppure `github.com/<utente>/gestrip`. Oggi il `go.mod` usa
    un nome provvisorio, `gestrip`.

**Si possono decidere più avanti, alla tappa indicata:**

11. Riferimento: confermi `go-baseline` e non `3415e90` del v2 (7.2 punto 2)? (T1)
12. Posso riallineare io il corpo dell'inventario alle decisioni del 29/10 (7.2 punto 7)? (subito)
13. `/login` e reset password: faccia interna (solo Tailscale, come `/invito`) o pubblica? (T1)
14. Criterio d'import per i link `/artigiano`: vale solo per i vecchi link Flask? (T2)
15. Copia remota dei backup: SFTP nella v1 va bene? Che destinazione useresti tu? (T5.2)
16. Pagina di ripiego per la disiscrizione (D-02): «disiscriviti lo stesso» (un flusso nuovo da
    progettare) o «contatta il negozio»? (T4.6)
17. Coda dei messaggi importata con i canali spenti: resta ferma e si scarta alla conferma, o si
    invia? (T2)
18. Job e orario di negozio: solo i giorni di apertura, oppure anche le ore? (T4.9)
19. OPS-06, archivio cifrato della sorgente d'import: serve nel Go? (T2)
20. Pagina di presentazione: quale contatto metti per chi vuole una mano? (T6)

---

## 9. Metodo di lavoro

Riuso il metodo dello standalone (`AGENTS.md`, «Work Guidance»), adattato al Go:

- **Una slice per sessione**, con fonte, confine di accettazione e voci dell'inventario dichiarati
  all'inizio, in `docs/slices/NNN-nome.md`.
- **TDD:** prima il test che fallisce, poi il codice. La ricevuta dice quanti test erano rossi.
- **Due livelli:**
  - *completo* per dati e schema, privacy, soldi, ruoli, autenticazione, audit, rotte pubbliche,
    link e cancellazioni: controllo delle mutazioni, review indipendente da un'altra sessione,
    ricevuta;
  - *leggero* per layout, CSS e testi: TDD e precommit.
- **Precommit:** `gofmt`, `go vet`, `staticcheck` a versione fissata, test, gitleaks, script di
  copertura. Tutto sotto
  `systemd-run --user --scope -p CPUQuota=800% -p MemoryMax=16G nice -n 19`.
- **Ricevuta di completamento** in fondo al file della slice, con commit, test, mutazioni,
  review, copertura e prove a scatola chiusa. Un commit per slice, poi push su `origin`.
- **Handoff** in `docs/handoff/` quando serve, committato con la slice.
- **Mai:** toccare lo standalone (solo `git show go-baseline:…`), la produzione, i servizi di
  pc-fisso o i dati veri senza permesso; usare `sudo` o installare pacchetti di sistema senza
  chiedere.

---

### Fonti esterne consultate

- Tailscale, «Tailscale Serve» (intestazioni d'identità solo per la tailnet, non per Funnel):
  https://tailscale.com/kb/1312/serve
- Caso pubblico su `X-Forwarded-For` e `Tailscale-Funnel-Request` dietro Funnel (da verificare
  dal vivo in T1): https://github.com/akashnaren/raspberry-pi-fun/pull/111
- Stato delle librerie: API di GitHub, letta il 30/10/2026.
