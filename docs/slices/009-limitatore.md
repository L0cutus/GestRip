# 009 — Limite ai tentativi falliti e indirizzo del cliente (T1)

- **Fonte:** Stefano, 30/10/2026: limitatore prima di ogni uso reale; il limite si controlla
  **prima** della verifica della password; il 404 di blocco è il rifiuto della rotta (D-05).
  Inventario ACC-20 e CLI-21; standalone a `go-baseline` = `d3bab62`, slice 208 (`5680d37`):
  `attempt_limiter.ex`, `public_attempts.ex`, `client_ip.ex`, `session_controller.ex`, i test
  `attempt_limiter_test.exs` e `attempt_limits_test.exs`, la matrice di parità («Attempt limits»).
- **Confine:**
  - `internal/clientip`: l'indirizzo con cui si conta un cliente, regole di `ClientIp`;
  - `internal/ratelimit`: i quattro secchi dello standalone (`/stato` 10 in 15 min, token lunghi
    30 in 1 min, login 20 per indirizzo e 5 per indirizzo e account in 15 min; blocco sempre di
    15 min), solo i falliti, in memoria;
  - login: blocco controllato prima di cercare l'account e di verificare la password, `429`
    «Troppi tentativi, riprova fra N minuti.», azzeramento dell'account a login riuscito;
  - faccia pubblica: famiglie di percorsi dello standalone, ogni `404` della famiglia conta,
    l'indirizzo bloccato riceve il rifiuto della rotta **identico** al `404` normale (D-05),
    intestazioni comprese; le foto non contano.
- **Fuori:** le rotte pubbliche vere (T4.5 e seguenti: useranno il rifiuto di questa slice); il
  reset della password con i suoi limiti (ACC-13/14); la riga d'audit
  `security.attempts_blocked` (arriva con l'audit, ADM-16); la prova dal vivo dietro Funnel (T1).
  ACC-20 e CLI-21 restano parziali per queste parti.
- **Voci:** ACC-20 (login), CLI-21 (limitatore e indirizzo).
- **Livello:** completo (sicurezza, autenticazione, rotte pubbliche).

## Misure sullo standalone (banco `grgo-s009`, immagine `fab-218` = `d3bab62`)

Sonda HTTP dentro la rete interna, verso l'app con un indirizzo cliente inventato per prova:

- 404 normali: `/r`, `/stato/…` (pagina e poll), `/artigiano` → «Riparazione non disponibile»;
  `/lotto` → «Lotto non disponibile»; `/w`, `/w/…/current`, `/g` → «Documento non disponibile» con
  `X-Robots-Tag: noindex, nofollow`. Tutti: solo testo, **nessun Content-Type**, `no-store`,
  `no-referrer`, CSP, `nosniff`, `x-permitted-cross-domain-policies: none`.
- Bloccati (10 `/stato` sbagliati, 30 token sconosciuti): lo stesso testo con **solo** `no-store` e
  `no-referrer` (più HSTS e x-request-id). Il Go dà il 404 normale intero (D-05). Il blocco di
  `/stato` non tocca `/r`; un altro indirizzo non è bloccato; una foto bloccata risponde normale.
- Login: 4 sbagliati → 422, il **5°** → già 429 «Troppi tentativi, riprova fra 15 minuti.», poi 429
  anche con l'email scritta diversa; altro indirizzo o altro account → 422; 20 sbagliati da un
  indirizzo su account diversi → il 20° è 429, poi anche un account nuovo; il modulo malformato →
  422 anche da bloccato. Intestazioni del 429: quelle della pagina di login.

## Cosa c'è

- `internal/clientip`: le regole di `ClientIp`; in più la tailnet IPv6 (`fd7a:115c:a1e0::/48`) non
  è privata e conta per indirizzo intero (PIANO §1.6, review), come già 100.64/10.
- `internal/ratelimit`: i quattro secchi, solo i falliti, blocco di 15 minuti, pulizia al massimo
  una volta al minuto alla prima richiesta (niente timer), in memoria.
- Login (`internal/web/accesso.go`): blocco guardato **prima** di cercare l'account e di
  verificare la password; si contano account e indirizzo; il rifiuto che fa partire il blocco è già
  429; il login riuscito azzera l'account.
- Faccia pubblica (`internal/web/limiti.go`): `famigliaDi…` come `PublicAttempts.family/1`, sui
  segmenti letti come il mux; `RifiutoPubblico`, il solo modo in cui una rotta pubblica rifiuta,
  così il 404 di blocco è identico per costruzione; `limitePubblico` prima della scelta della rotta
  (`facciaConFiltro`), quindi conta anche i 404 di percorsi che nessuna rotta serve.
- Banco: Caddy con `trusted_proxies static private_ranges` come in produzione; segnaposto
  `{ip_cliente}` (IPv6 di documentazione casuale per scenario); scenario `limiti.json`.

## Prove

- TDD: 16 test rossi prima del codice (indirizzo, limitatore, login, famiglie, rifiuto, blocco),
  più 2 del banco; dopo la review 4 rossi nuovi (tailnet IPv6, orologio indietro, segmenti con
  `%2F`).
- Banco **verde**, 5 scenari, 84 passi, anche rilanciato subito (l'indirizzo per scenario non
  resta bloccato); spento, nessun avanzo.
- Mutazioni: **49 uccise su 51 che compilano**. Vive due, equivalenti: `Bloccato` con
  `!fino.Before` (a fine blocco la durata residua è zero comunque) e i falliti non azzerati quando
  parte il blocco (ogni blocco dura almeno la finestra, quindi alla fine sono già fuori).
- `scripts/check` verde con l'esito del banco. ACC-20 e CLI-21 **parziali** di proposito: test
  saltati per l'audit del blocco, i limiti del reset e la prova dietro Funnel.

## Review indipendente

Nessun problema alto. Corretto:

- **MEDIA:** i nodi della tailnet su IPv6 cadevano in fc00::/7 (privati) e tutti nello stesso /64:
  dietro `tailscale serve` 20 password sbagliate del personale avrebbero bloccato il login a tutti.
  Ora la tailnet IPv6 non è privata e conta per indirizzo intero. Da confermare dal vivo in T1.
- **BASSA:** un `%2F` dentro un token spezzava il segmento e la richiesta non contava; ora i
  segmenti si leggono come il mux e lo standalone.
- **BASSA:** con l'orologio tornato indietro la pulizia si fermava; ora riparte.

Dichiarato, non corretto:

- **MEDIA, uguale allo standalone:** richieste **in parallelo** passano tutte il controllo prima
  che il primo fallito venga contato (60 login insieme, 60 password verificate). Correggerlo è una
  deviazione: domanda per Stefano.
- **BASSA:** il Go legge i nomi delle famiglie decodificati, quindi conta anche `/st%61to/…` e
  `/%72/…`, che lo standalone non conta (e che nello standalone aggirano il limite: misurato sul
  banco dal revisore). Più severo dello standalone e non ancora una deviazione approvata: domanda
  per Stefano, insieme alla correzione nello standalone.
- **BASSA:** i percorsi con `.` o `..` ricevono il 404 prima del filtro (D-30: nessun browser li
  manda e nessuna rotta li serve).
- **BASSA, uguale allo standalone:** memoria senza tetto, cresce con gli indirizzi distinti per 15
  minuti.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `d3bab62`.
- **Commit:** questo (slice 009), dopo `cec0329` (008c).
