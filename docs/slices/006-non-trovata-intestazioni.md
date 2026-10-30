# 006 — 404 comune e intestazioni di sicurezza (T1)

- **Fonte:** PIANO §1.5 e T1; decisione di Stefano del 30/10/2026 («`/healthz`: copia il 404
  dello standalone, niente deviazione»); inventario ADM-20, ACC-18; standalone `go-baseline`
  (Phoenix 1.8.13, Plug 1.20.3), **misurato** sul banco.
- **Confine:** ogni risposta che il Go non serve è il 404 dello standalone, byte per byte su
  codice, corpo e intestazioni che contano; percorsi letti come Phoenix; HSTS e Cache-Control di
  base come Plug; intestazioni di sicurezza delle pagine pronte per la 008.
- **Voci:** ADM-20 (resta chiusa, con più casi). ACC-18 **non** si chiude qui: mancano le pagine
  (le intestazioni di `Pagina` si verificano sul banco con `/login`, slice 008) e la parte HTTPS.

## Misure sullo standalone (banco `grgo-s006`, 30/10/2026)

Una sonda usa e getta dentro la rete interna del banco ha registrato codice, intestazioni e corpo
di 60 richieste allo standalone (dietro Caddy) e al Go. Risultati:

- **404:** corpo `Not Found`, `text/html; charset=utf-8`; con JSON richiesto
  `{"errors":{"detail":"Not Found"}}`, `application/json; charset=utf-8`. Sempre
  `cache-control: max-age=0, private, must-revalidate`, nessuna intestazione di sicurezza delle
  pagine, nessun `allow`.
- **Metodi:** POST, PUT, DELETE e OPTIONS su `/healthz` danno lo stesso 404, non 405. HEAD: 404
  senza corpo.
- **Percorsi:** `/healthz/`, `//healthz`, `/healthz//`, `/heal%74hz` rispondono come `/healthz`;
  `/HEALTHZ`, `/healthz/x`, `/healthz;x`, `/./healthz`, `/x/../healthz` sono 404 (nessun
  redirect).
- **Scelta HTML o JSON** (`Phoenix.Controller.accepts/2`): `_format` nella query vince; un `*/*`
  ovunque nell'Accept dà HTML; la prima voce con q=1 che corrisponde vince; le altre per q
  decrescente e, a pari q, per il **testo grezzo** della voce (lo spazio dopo la virgola conta:
  `application/json;q=0.5, text/html;q=0.5` → HTML, senza spazio → JSON); `text/*` → HTML,
  `application/*` → JSON; q illeggibile vale 1; `application/vnd.api+json` → HTML.
- **HSTS:** `strict-transport-security: max-age=31536000` su ogni risposta, `/healthz` compreso
  (force_ssl, esclusi gli host `localhost` e `127.0.0.1`).
- **Pagine** (`/login`, `/`): `content-security-policy: base-uri 'self'; frame-ancestors 'self';`,
  `referrer-policy: strict-origin-when-cross-origin`, `x-content-type-options: nosniff`,
  `x-permitted-cross-domain-policies: none`. Chiude il «Da verificare» dell'inventario
  sull'elenco esatto di `put_secure_browser_headers`.
- **Non copiate, perché del proxy o d'infrastruttura:** `alt-svc`, `via`, `vary` (Caddy), `date`, e
  `x-request-id` (Plug.RequestId): serve solo a collegare le righe del registro di Phoenix, che il
  Go non scrive per i percorsi; non è nell'inventario.

## Cosa c'è

- `internal/web/faccia.go`, comune alle due facce:
  - percorso riscritto nei segmenti di Phoenix prima del mux, `.` e `..` → 404, mai redirect;
  - quello che il mux non serve, metodo sbagliato compreso, è `NonTrovata`;
  - HSTS e Cache-Control di base;
  - `formatoErrore` (le regole sopra) e `Pagina` (intestazioni delle pagine).
- `NonValida`: il 400 dello standalone per una query con UTF-8 non valido («Bad Request», HTML o
  JSON dalla sola Accept, **senza** HSTS, misurato). `_format` letto come Plug (vedi Review).
- `gestrip serve`: `DisableGeneralOptionsHandler`, così `OPTIONS *` arriva alla faccia e dà il 404
  dello standalone invece del 200 vuoto di net/http.
- `NonTrovata` è esportata. **Attenzione per D-05:** nello standalone il 404 «normale» di una rotta
  pubblica è il suo rifiuto («Riparazione non disponibile», …), non «Not Found»
  (`public_attempts.ex`): il 404 di blocco del limitatore va reso identico a quello della rotta,
  da chiarire nella slice del limitatore.
- Banco: i passi possono mandare intestazioni (`intestazioni`) e percorsi grezzi che iniziano con
  `//` (si attaccano sempre alla base, test); uno scenario che mette `Host`, `Content-Length`,
  `Connection` o `Transfer-Encoding` è rifiutato, perché il client Go le ignora e il passo
  sarebbe verde senza provare niente. Scenari: `non-trovata.json` (49 passi) e `healthz.json`
  esteso (7 passi).
- `scripts/githooks/pre-commit --tutto`: `gitleaks dir` ora guarda solo i file che git potrebbe
  committare: il contenuto dell'indice e quello su disco dei file tracciati e non tracciati,
  non quelli ignorati. Prima si fermava sulle chiavi della CA finta di un banco acceso, in
  `to-be-deleted-after-use/`. Ogni errore di copia ferma il controllo (niente pipe: in `sh`
  l'errore si perderebbe); pulizia anche su interruzione. Prove negative, tutte rosse: chiave in
  un file non tracciato, chiave solo nell'indice con il file su disco ripulito, file illeggibile.

## Prove

- TDD: test del 404, dei percorsi, di HSTS, della scelta del formato, di `Pagina` e del banco,
  rossi prima del codice (non compilavano; con il codice di prima davano 405 e il 404 di Go).
  Dopo la review, altri test rossi prima delle correzioni (parametri, sottoalbero, query, 400,
  `q` come Plug, HEAD, `OPTIONS *`, intestazioni rifiutate dal banco).
- **Banco verde:** 2 scenari, 56 passi, 0 differenze.
- **Sonda grezza** (richieste HTTP scritte byte per byte, standalone diretto senza Caddy contro
  Go): 100 richieste, 87 uguali. Le 13 diverse sono quelle dei debiti qui sotto.
- Mutazioni su `faccia.go`: 28 al primo giro e 21 dopo la review, tutte uccise tranne una
  equivalente: il controllo della seconda cifra di `%XX`, perché con una cifra non valida la
  conversione fallisce e scrive comunque un byte innocuo. Tolti, perché le mutazioni li hanno
  mostrati inutili:
  - la rimozione di `Content-Length`;
  - la validazione dei token dell'Accept;
  - il salto delle coppie vuote;
  - la scorciatoia e il `+` nella decodifica (non cambiano né la validità UTF-8 né il nome
    `_format`).
- `scripts/check` verde, errcheck compreso.

## Review indipendente

Fatta da un'altra sessione con una sonda propria, che scrive le richieste a mano (circa 90),
contro lo standalone **diretto** (senza Caddy) e contro il Go.

- **ALTA, corretta:** `faccia` chiamava il gestore trovato con `mux.Handler`, che non compila
  `r.PathValue` e `r.Pattern`: le rotte future con parametri (`/r/{token}`) avrebbero letto
  valori vuoti senza errori. Ora chiama `mux.ServeHTTP`; test con `/foto/{nome}`.
- **MEDIA, corrette:**
  - `_format` dalla query come Plug: «;» non separa, un `%` illeggibile resta, vale l'ultima
    assegnazione, `_format[]` o `_format[x]` lo rendono non testo (HTML);
  - query con UTF-8 non valido → 400 «Bad Request» senza HSTS, su ogni rotta, `/healthz`
    compreso;
  - `q` nell'Accept come Plug: virgolette tolte; uno spazio dopo la chiave o all'inizio del
    valore rende q illeggibile (vale 1);
  - un pattern a sottoalbero («/r/») portava a un redirect infinito del mux: ora è un 404 (e la
    regola è scritta in `docs/SVILUPPO.md`).
- **MEDIA, non corretta qui: il corpo delle richieste.** Lo standalone legge `_format` (e
  `_method`) anche dal corpo (form, JSON, multipart, anche su PUT e DELETE) e risponde 400 a un
  corpo illeggibile. Sono 12 delle 13 differenze della sonda grezza. Sono le regole di
  Plug.Parsers e Plug.MethodOverride, che servono comunque al primo modulo (login): vanno nella
  **slice 006b, dedicata, prima della 007**, con i limiti di dimensione (413) da misurare. Non è una
  deviazione approvata: è lavoro mancante.
- **BASSA:**
  - corretto `OPTIONS *`;
  - corretto `TestNonTrovataHEAD`, che non controllava il corpo: ora usa un server vero, e
    verifica corpo vuoto e `content-length: 9` come lo standalone;
  - corrette le intestazioni del banco ignorate in silenzio e il pre-commit (falsi verdi);
  - corretta la nota su D-05;
  - **dichiarato:** `GET /heal%zzthz` dà il 400 di net/http prima di arrivare alla faccia; lo
    standalone diretto dà 404, ma dietro Caddy anche lui dà quel 400. Dietro `tailscale serve`
    da verificare;
  - **dichiarato:** dopo la riscrittura `r.URL.Path` contiene i `%2F` decodificati: i gestori
    leggono i segmenti con `r.PathValue`, mai `r.URL.Path` (regola in `docs/SVILUPPO.md`);
  - **da provare dal vivo (T1, Funnel):** se `tailscaled` inoltra `Host: 127.0.0.1:…`, il Go non
    manda mai HSTS. Aggiunto alla prova dal vivo del piano.

## Debiti

- **Slice 006b (prossima):** corpo delle richieste come Plug.Parsers e Plug.MethodOverride
  (`_format` e `_method` dal corpo, 400 per un corpo illeggibile, limiti e 413).
- `%zz` nel percorso: 400 di net/http, da guardare dietro `tailscale serve`.
- Con due intestazioni Accept conta la prima, come Plug: provato nei test e dalla sonda grezza,
  non sul banco (lo strumento manda una sola intestazione per nome).
