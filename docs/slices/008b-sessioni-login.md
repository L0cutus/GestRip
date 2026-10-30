# 008b — Sessioni, accesso e uscita (T1)

- **Fonte:** richiesta di Stefano del 30/10/2026 (cookie per faccia firmato, rinnovo di sessione
  e CSRF a login e logout, `/login` solo sulla faccia interna D-28, calcolo finto se l'utente non
  esiste, Go dietro TLS sul banco); inventario ACC-01…04 e ACC-10 (disattivazione: la sessione
  cade al primo clic); standalone `go-baseline` (`session_controller.ex`, `user_auth.ex`,
  `accounts.ex`), **misurato** sul banco.
- **Confine:** accesso, uscita, ritorno alla pagina chiesta, controllo della sessione a ogni
  richiesta, home provvisoria protetta. Il limite ai tentativi (ACC-20) è della 009, i ruoli
  della 008c.
- **Voci:** ACC-01, ACC-02, ACC-03 (con banco), ACC-04 (test Go).
- **Livello:** completo (autenticazione).

## Misure sullo standalone (banco `grgo-s008`)

- `GET /` senza sessione: 302 `/login`. La pagina dopo mostra «Devi accedere per continuare.» una
  volta sola.
- `GET /login`: 200 con i campi `user[email]` (email, `autocomplete="username"`) e
  `user[password]`, «Accesso laboratorio», «Area riservata al personale autorizzato.»,
  «Accedi», «Password dimenticata?» verso `/reset-request`, token nel meta e nel modulo.
- Password sbagliata, email sconosciuta, modulo malformato: **422** con «Email o password non
  validi.». Senza token: 403. Email con maiuscole e spazi: entra.
- Login giusto: 302 alla pagina chiesta prima, o a `/`. Corpo `<html><body>You are being <a
  href="…">redirected</a>.</body></html>`.
- `/login` da dentro: 302 `/`.
- Uscita (`DELETE /logout` con `_method`): cookie cancellato (`max-age=0`), 302 `/login`.
  `POST /logout` senza `_method` nello standalone è 404, nel Go è l'uscita (D-30: il modulo
  dell'app non lo manda mai così).
- Elixir `String.trim` e `String.downcase`, sugli stessi casi del Go: coincidono, tranne «İ».
  Elixir dà «i» più il punto combinante, Go «i»: lo copia `NormalizzaEmail`.

## Cosa c'è

- `internal/accounts/utenti.go`:
  - `Autentica`: email normalizzata, solo account attivi (`attivo = 1`, così si usa l'indice) e
    con password;
  - account inesistente, in attesa, con hash malformato o con password sbagliata costano la
    stessa derivazione (`hashFittizio`, come `@dummy_hash`);
  - `DiSessione`: account attivo con la stessa `versione_sessione`;
  - `RegistraAccesso`.
- `internal/web/sessione.go`:
  - cookie `__Host-gestrip_sessione_<faccia>`, firmato HMAC con la sottochiave «sessione» e il
    nome della faccia, attributi come lo standalone, senza scadenza (come lo standalone);
  - a ogni richiesta l'account si ricontrolla; se non vale più, il cookie perde l'account.
- `internal/web/accesso.go` e `pagine/*.html`:
  - `GET /login`, `POST /login`, `POST /logout` e `GET /`, solo sulla faccia interna;
  - messaggio una volta sola dopo un redirect;
  - ritorno alla pagina chiesta: solo GET navigati, regole di `safe_return_to` controllate quando
    si salva e quando si segue;
  - al login sessione **nuova** e segreto CSRF rinnovato, all'uscita cookie cancellato e CSRF
    rinnovato;
  - la home è provvisoria: la lista delle buste arriva con la T3.
- Facce: `web.Dipendenze` con sole sottochiavi; `Interna` e `Pubblica` restituiscono un errore se
  le chiavi non vanno.
- Banco:
  - il Go dietro lo stesso Caddy, in TLS, sulla faccia interna;
  - il titolare sintetico seminato con le stesse credenziali inventate dello standalone;
  - passi con corpo e segnaposto `{csrf}`, `{email}`, `{password}`, cookie per scenario;
  - campi `testo:` (frase nel corpo) e `atteso` (valori che **tutte e due** le istanze devono
    dare);
  - scenario `accesso.json`, 15 passi.

## Prove

- TDD: test di account, sessione, pagina di login, login giusto e sbagliato, ritorno, uscita,
  sessione ricontrollata, rinnovo, banco, rossi prima del codice.
- Banco verde: 3 scenari, 71 passi. Il primo giro verde era sospetto, perché due istanze che
  sbagliano allo stesso modo danno verde. Una sonda ha confermato il login vero su tutte e due,
  e da allora i passi chiave hanno `atteso`.
- Mutazioni: 38, con la prova che il codice compili. Hanno fatto aggiungere cinque test:
  - rinnovo della sessione al login;
  - messaggio del 422 sopra quello in sessione;
  - ritorno solo GET e ricontrollato anche in un cookie firmato;
  - uscita con sessione già scaduta;
  - account riattivato senza la sessione vecchia.

  Vive solo una, equivalente: il primo dei due controlli del ritorno, perché il secondo filtra
  comunque (doppio controllo voluto, come lo standalone).
- **Errore mio trovato dalle mutazioni:** una modifica ai test di `NormalizzaEmail` (i casi
  «İ», ZWSP e BOM) non era stata applicata, perché lo script non controllava la sostituzione.
  Lo stesso giro mi aveva fatto credere, sbagliando, che Go e Elixir coincidessero su «İ».
  Corretto e controllato.
- `scripts/check` verde con l'esito del banco: ACC-01, 02, 03, 04 chiuse.

## Review indipendente

Nessun problema alto. Il revisore ha misurato:
- tempi del login uguali tra email esistente e inesistente (mediana 64,30 e 64,37 ms);
- `POST /login` da un'altra origine (same-site o cross-site) → 403;
- `/login`, `/logout` e `/` sulla faccia pubblica → 404 (D-28);
- token di prima del login non più valido dopo;
- uscita che cancella il cookie;
- credenziali sintetiche assenti da esiti, repo e uscite.

Corretto:

- **MEDIA, differenza che un browser produce:** due schede su `/login`, si entra dalla prima e
  poi «Accedi» nella seconda. Il Go dava 403 (CSRF rinnovato), lo standalone 302 `/`. Ora chi ha
  già una sessione valida va a `/` prima del controllo CSRF, come lo standalone. È solo un
  redirect; chi non ha una sessione passa sempre dal controllo (test).
- **MEDIA, cookie di sessione sempre uguale:** lo stesso account dava lo stesso cookie a ogni
  accesso. Ora c'è un valore casuale per accesso.
  - **Resta una decisione di Stefano (PIANO §8.5):** come nello standalone, un cookie copiato vale
    finché l'account è attivo e la versione non cambia, anche dopo l'uscita da quel browser.
    Le alternative sono una durata massima o una tabella delle sessioni sul server.
- **BASSA:**
  - il cookie con un ritorno pieno di «&» superava i 4096 byte (json escapava le «&»);
  - lo scenario del banco non distingueva «torna alla pagina chiesta» dal default `/`: il passo
    dopo l'uscita ora non salva il ritorno;
  - due test stretti (tempi a metà del riferimento, cookie di sessione nuovo dopo il login).

Dichiarato:
- **Nessun limite ai tentativi** finché non arriva la 009 (ACC-20): il Go non va in uso prima.
- Il link «Password dimenticata?» porta a `/reset-request`, che nel Go è 404 fino alla slice del
  reset.
- Il banco conta i login sbagliati nel limitatore dello standalone (5 per account in 15
  minuti): più di quattro giri in 15 minuti lo fanno diventare rosso (429). Non è un falso verde,
  gli `atteso` lo vedono.
- `HEAD /` senza sessione non salva il ritorno, lo standalone sì: D-30, nessun browser naviga
  con HEAD.
