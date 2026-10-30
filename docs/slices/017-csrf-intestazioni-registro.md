# 017 — CSRF e cookie (ACC-17), intestazioni e HTTPS (ACC-18), nessun token nei registri (ACC-19)

- **Fonte:** `prompt-via-area.md` (Stefano, 30/10/2026), fine T1. Standalone a `go-baseline` =
  `6eb93ed`: `router.ex`, `endpoint.ex`, `config/prod.exs`, `config/runtime.exs`,
  `deploy/Caddyfile.*`; test `endpoint_logging_test.exs`, `crash_report_secrets_test.exs`,
  `session_revocation_test.exs`.
- **Voci:** ACC-17, ACC-18, ACC-19.
- **Lavoro:** implementazione e correzioni di un sottoagente; review di un sottoagente diverso; check,
  banco e le mutazioni rifatti dalla sessione principale.

## Cosa c'è

Quasi tutto c'era già dalle slice 006, 006b, 008b e 009. Questa slice lo verifica voce per voce
contro lo standalone e lo chiude con test che enumerano le rotte.

- **ACC-19, il buco vero:** un panic in un gestore faceva scrivere a net/http, sul suo ErrorLog (il
  journal), il valore del panic, che può contenere l'indirizzo con il token. Ora il registro ha
  rotta, id, tipo e stack, mai il valore, e verso net/http si rilancia `http.ErrAbortHandler`. La
  connessione si chiude come prima. La riga del panic scrive il codice già partito (0 se niente).
- **Faccia pubblica con il suo elenco di rotte** (`f.rottaPubblica`): una GET riceve le
  intestazioni di pagina, una POST anche il CSRF, per costruzione. Niente più `mux.Handle`
  diretto.
- **CSRF come Plug:** se nel corpo c'è `_csrf_token` vale solo quello; altrimenti l'intestazione
  `x-csrf-token`.
- **Controllo statico** (`TestNessunPercorsoNeiRegistriDelCodice`): nei sorgenti di `internal/web`
  e `cmd/gestrip` nessuna chiamata di registro, `fmt`, `log` o `panic` riceve l'indirizzo, la
  query, `PathValue`, `Referer` o la richiesta intera.
- **Test che enumerano le rotte** di tutte e due le facce: ogni POST senza token o con un token
  sbagliato dà 403 senza scrivere niente; le intestazioni di `put_secure_browser_headers` e HSTS
  dappertutto; una riga di registro per richiesta con il solo pattern; nessun token nei registri,
  nell'ErrorLog e nei corpi (11 prefissi, varianti in codice, errori 400/403/404/413/500, panic).
- `docs/SVILUPPO.md`: regole su rotte, registro e panic. Mai un segreto per valore come intero o
  array a una funzione che può andare in panic: nello stack comparirebbe in esadecimale.
- Banco: scenario `csrf-intestazioni.json` (19 passi).

## Prove

- **Rossi prima della correzione:** il valore del panic nell'ErrorLog; il codice del panic sempre
  500; tre casi del token nel corpo prima dell'intestazione. I test sul comportamento che c'era già
  sono provati con le mutazioni.
- **Mutazioni:**
  - **primo giro:** 22, tutte uccise;
  - **dopo la review:** 15, tutte uccise;
  - **verifica della sessione principale su una copia nuova:**
    - le 15 della review: tutte uccise;
    - delle prime 22, 18 si applicano ancora e sono uccise;
    - le altre 4 non trovano più il testo, cambiato dalle correzioni; i loro casi restano coperti
      dai test nuovi.
- **Banco verde** con `fab-219`: 13 scenari, 156 passi. I 403 su `/reset-request`, `/reset/:token`
  e `/invito/:token` senza token, che erano solo ragionati, ora sono misurati uguali. Spento,
  nessun avanzo.
- `scripts/check` verde con l'esito: chiuse **ACC-17, ACC-18, ACC-19** (25 voci chiuse).

## Review indipendente

Nessuna ALTA. Verificato dal revisore:
- nello stack stringhe e slice appaiono solo come puntatore e lunghezza;
- l'ErrorLog di net/http non scrive mai il percorso;
- il client vede la stessa cosa di prima;
- intestazioni ed esclusioni di HSTS uguali a Phoenix e `prod.exs`.

Corretto:
- **MEDIA:** la faccia pubblica era senza elenco di rotte, quindi le rotte future non erano coperte
  per costruzione. Inoltre un log del percorso in un ramo d'errore sfuggiva ai test. Ora ci sono il
  registratore pubblico e il controllo statico.
- **BASSA:** codice vero nella riga del panic; ordine corpo poi intestazione del CSRF; commento
  sullo stack.

Dichiarato:
- dietro tailscale serve, un panic si vedrà probabilmente come 502 del proxy (domanda 11);
- il controllo statico non prende un aiuto nuovo che restituisce il percorso, né una richiesta
  passata con un altro nome.

## Differenze dallo standalone (in `docs/domande-aperte.md`, 10–14)

- Due cookie `__Host-`, `Secure` sempre, `CrossOriginProtection`, registro più stretto, POST
  /login con sessione e senza token che dà 302: domanda 10.
- Panic senza la pagina 500: domanda 11. Nessun redirect a HTTPS, con l'ascolto solo su loopback:
  domanda 12.
- Possibile difetto dello standalone: `/m/:code` e i prefissi in codice nel suo registro (domanda
  13, nessun rischio).
- Moduli pubblici futuri con browser vecchi: domanda 14.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `e65ce29` (016).
