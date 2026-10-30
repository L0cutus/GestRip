# 006b — Moduli e CSRF

- **Fonte:** decisione di Stefano del 30/10/2026 (PIANO §8.4): «solo ciò che serve ai moduli Go
  (lettura dei form, CSRF, `_method` solo se lo usi; meglio solo POST)»; deviazione di categoria
  D-30 per i dettagli del framework non osservabili; inventario ACC-17; standalone misurato sul
  banco (`grgo-s006b`).
- **Confine:** una rotta POST avvolta in `web.Modulo` legge il modulo e rifiuta senza un token
  CSRF valido; le pagine prendono il token con `web.TokenCSRF`. Niente `_method` (il Go usa solo
  POST), niente JSON né multipart.
- **Voci:** ACC-17 resta **aperta** e senza segni nei test: si chiude con le sessioni (008), quando
  il cookie di sessione firmato e il login sono sul banco.

## Misure sullo standalone

- POST su una rotta vera (`/login`) senza token, con token falso, con `x-csrf-token` falso, con
  corpo JSON, multipart o vuoto: **403 «Forbidden»**, `text/html`, Cache-Control di base.
- Cookie di sessione: `path=/; secure; HttpOnly; SameSite=Lax`, senza scadenza.
- Limite del corpo di un modulo: 1 000 000 di byte passano, **1 000 001 danno 413 «Request Entity
  Too Large»**.
- Con `Accept: application/json` su una pagina lo standalone dà 406 (la pipeline accetta solo
  HTML): un browser non lo manda, D-30.

## Cosa c'è (`internal/web/modulo.go`)

- `NuovoCSRF(chiave, faccia)`: la protezione di **una** faccia, con la chiave del server (almeno
  32 byte, stabile tra i riavvii: la caricherà la 008 insieme alla prima rotta con un modulo).
- `csrf.Token(w, r)`:
  - il segreto del browser (32 byte casuali) sta nel cookie `__Host-gestrip_csrf_<faccia>`, con
    gli attributi del cookie dello standalone;
  - il valore è firmato con HMAC-SHA256, chiave del server e nome della faccia;
  - se il cookie manca, non ha la forma giusta o non ha la firma giusta, se ne crea uno nuovo;
  - più chiamate nella stessa risposta (il meta e il campo nascosto) usano lo stesso segreto;
  - il token è mascherato come in Phoenix: `base64url(maschera || segreto XOR maschera)`.
- `csrf.Modulo(h)`:
  - legge **solo il corpo**, e solo se è `application/x-www-form-urlencoded`, fino a 1 000 000
    di byte;
  - troppo grande → 413; illeggibile, interrotto o con UTF-8 non valido → 400;
  - la query dell'indirizzo non conta;
  - poi rifiuta con 403 «Forbidden», senza chiamare il gestore, una richiesta da un'altra origine
    (`http.CrossOriginProtection`) o senza token valido. Il token vale se è valido il campo
    `_csrf_token` **o** l'intestazione `x-csrf-token`, come Plug; il confronto è a tempo
    costante.
- `Proibita` (403), `TroppoGrande` (413, senza HSTS né `x-request-id`, come lo standalone) e
  `NonTrovata` passano da un'unica `paginaErrore`.
- Regole per chi scrive le rotte con moduli: `docs/SVILUPPO.md`, «Moduli» (ordine
  `csrf.Modulo(web.Pagina(h))`, `action` esplicito, multipart con il token nell'intestazione).

## Prove

- TDD: test del cookie, dei rifiuti, dei corpi (400, 413, limite esatto) e del token mascherato,
  rossi prima del codice. Dopo la review, altri test rossi prima delle correzioni:
  - due token nella stessa pagina;
  - cookie impostati da uno script, compresa la firma vera di un altro cookie con un segreto
    diverso;
  - token e cookie dell'altra faccia;
  - richieste da un altro sito e da un'altra porta;
  - query del POST con `;` e `%`;
  - corpo interrotto;
  - le intestazioni di 403, 400 e 413 attraverso la faccia.
- Mutazioni: 22 al primo giro e 24 dopo la review, tutte uccise, tranne:
  - il controllo del tipo di corpo, equivalente a quello che fa già la lettura;
  - il nome del cookie letto da un `Set-Cookie` della stessa risposta, equivalente grazie alla
    firma.

  Tolto il gestore dei rifiuti di `CrossOriginProtection`, mai chiamato (si usa `Check`).
- `scripts/check` verde; banco verde (56 passi). Il Go non ha ancora rotte con moduli, quindi il
  banco non li confronta: si farà con `/login` nella 008.

## Review indipendente (006b e 006c)

Nessun bypass diretto del CSRF trovato; nel registro nessun token né percorso. Corretti:

- **ALTA:** le due facce stanno sullo stesso host e i cookie non distinguono le porte. Il
  segreto era condiviso tra pubblica e interna, e uno script sulla faccia pubblica (XSS) poteva
  impostare il cookie e conoscere il segreto (fixation). Ora:
  - c'è un cookie per faccia;
  - il segreto è firmato dalla chiave del server e dal nome della faccia, quindi un cookie
    impostato da uno script non vale;
  - `CrossOriginProtection` rifiuta i POST da un'altra origine, 443 → 8443 compreso.
- **MEDIA:**
  - due `Token` nella stessa pagina alla prima visita creavano due segreti, e il primo token non
    valeva;
  - il 413 aveva HSTS e `x-request-id`, quello dello standalone no (misurato dal revisore; un
    browser lo produce con un testo oltre 1 MB);
  - `ParseForm` leggeva anche la query: un modulo senza `action` su un indirizzo con `;` o `%`
    dava 400. Ora si legge solo il corpo;
  - un panic nel gestore era registrato come `codice=200 rotta=nessuna`. Ora `codice=500
    panic=true` con rotta e id, e il panic prosegue.
- **BASSA:**
  - nel registro il metodo è solo uno di quelli noti o «altro»: prima un metodo di 200 KB dava
    una riga di 200 KB;
  - aggiunti `ReadTimeout` e `IdleTimeout` ai server;
  - le risposte 1xx non contano come codice finale;
  - `append` sui valori del modulo sostituito;
  - ordine `Modulo(Pagina(h))` e multipart documentati.
- **Dichiarate, D-30:**
  - `_csrf_token` doppio (vale il primo);
  - con `Accept: application/json` il Go dà 403 JSON dove lo standalone dà 406;
  - `%zz` e `;` nel **corpo** danno 400 nel Go e 403 nello standalone: un browser codifica sempre.

## Per la 008 (sessioni)

- Chiave del server per il CSRF: crearla alla prima partenza e conservarla (per esempio in
  `impostazioni`, quindi anche nel backup), e collegarla a `NuovoCSRF` per le due facce.
- Al login **e al logout** rinnovare sia il segreto CSRF sia la sessione.
- Il cookie di sessione ha lo stesso problema delle porte: un nome per faccia, firmato.
- Il cookie è `Secure`: per provare `/login` sul banco il Go deve stare dietro TLS, come lo
  standalone dietro Caddy.
- `CrossOriginProtection` confronta `Origin` con `Host` quando manca `Sec-Fetch-Site` (browser
  vecchi): va visto nella prova dal vivo quale `Host` arriva da `tailscale serve`.
- Mai CORS permissivo: la protezione con `x-csrf-token` si regge sul preflight che fallisce.
