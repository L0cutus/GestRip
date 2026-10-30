# 020 — Il primo titolare su un database vuoto: `gestrip primo-titolare` e `/configura/<codice>`

- **Fonte:** decisione di Stefano del 30/10/2026, nella nota «Deciso» sotto ACC-16 nell'inventario.
  Standalone a `go-baseline` = `6eb93ed`: `bootstrap.ex` (`admin_from_files/2`, `run/0`),
  `bootstrap_test.exs`, `accounts/user.ex` (forma dell'email), `accounts.ex` (`create_user/1`,
  `@password_min_length 12`), `audit.ex`.
- **Voce:** ACC-16 («sostituita»).
- **Modello:** il link di reset (011) e gli inviti (012), con le stesse proprietà di sicurezza.

## Cosa c'è

- **Tabella** `codici_primo_titolare` (migrazione 0012), solo nel Go:
  - del codice si salva solo lo SHA-256, unico;
  - scadenza, uso e revoca sono istanti;
  - non è legata a un account.
  - L'import la lascia vuota (`importer.TabelleSoloGo`, `IndiciSoloGo`).
- **Dominio** (`internal/accounts/primo.go`):
  - `LinkDallaConsole`: in una transazione controlla che `utenti` sia vuota, annulla i codici
    aperti, scrive il nuovo codice e la riga `administrator.bootstrap_requested`.
  - `CodiceValido`: una sola lettura, non spende niente.
  - `Crea`: controlla prima il modulo, poi il codice, prima della derivazione PBKDF2. Nella
    transazione ci sono due scritture protette, tutte e due condizionate a `utenti` vuota: spesa
    del codice (non usato, non annullato, non scaduto), poi `INSERT … SELECT … WHERE NOT EXISTS`
    del titolare. Se una delle due non trova niente, non resta nulla. Poi annulla gli altri codici
    aperti e scrive la riga `administrator.bootstrapped`.
- **Pagina** `GET/POST /configura/{codice}` (`internal/web/configura.go`, `pagine/configura.html`):
  - solo sulla faccia interna (D-28), permesso `aperta`, CSRF per costruzione;
  - nessuna sessione richiesta; aprirla non spende il codice;
  - ogni rifiuto è la stessa pagina «Link non valido», 404, senza modulo;
  - email sbagliata, password diverse o corte: 422, l'email normalizzata resta nel modulo, il codice
    resta valido;
  - riuscito: redirect a `/login` con «Titolare creato. Ora puoi accedere.»;
  - `Cache-Control: no-store` sul modulo.
  - Superficie: come l'invito, senza manifest e senza worker. Prima di ogni account non c'è niente
    da installare, e la pagina non deve registrare un worker sul dispositivo di chi installa.
- **Comando** `gestrip primo-titolare [-url …] [-db …] [-config …]`, con le regole di `reset-link`:
  - `segreti.ControllaAmbiente`;
  - l'indirizzo viene da `indirizzoDelLink`; il database deve esistere (non lo crea);
  - nessun argomento in più (`argomentiFiniti`);
  - il link si stampa solo su un terminale.
  - Con un account nel database rifiuta: «il primo titolare si crea solo su un database senza
    account; per un altro account usa «gestrip invito», per la password del titolare «gestrip
    reset-link»».
- **Registro delle azioni:**
  - `console | administrator.bootstrap_requested | installation | administrator | ok |
    {"channel":"console"}`, per ogni link creato (come `password_reset.requested` di reset-link);
  - `installer | <ip> | administrator.bootstrapped | user | <id> | ok | {}`, nella transazione
    della creazione.

## Decisioni da confermare

- **Durata 60 minuti.** Reset vale 30 minuti, l'invito 15. All'installazione ci vuole il tempo di
  passare dal terminale a un browser nella tailnet. Il rischio di una durata più lunga è piccolo: il
  link sta solo sul terminale, vale solo con `utenti` vuota e solo sulla faccia interna.
- **Indirizzo nella riga `administrator.bootstrapped`.** Lo standalone non lo scrive, perché lì non
  c'è un browser. Gli altri dati sono come nello standalone (attore `installer`, nessun dato).
- **`/` e `/login` su un database vuoto:** niente di nuovo. Il login non dice se il database è
  vuoto, e la faccia pubblica non ha né `/login` né `/configura`. L'installer stampa il comando.
- **Nessun limite ai tentativi** su `/configura`, come su `/reset/` e `/invito/` della faccia
  interna. Il limitatore è solo della faccia pubblica e del login, e il codice ha 256 bit.

## Differenze dallo standalone

- Il primo titolare si crea dal browser, con un codice, al posto dei due file privati (decisione di
  Stefano). Niente lettura di file, quindi niente controlli su permessi, link e dimensioni.
- La riga `administrator.bootstrap_requested` per il link è solo del Go.
- L'indirizzo compare nella riga `administrator.bootstrapped`.
- La pagina chiede la password due volte, come reset e invito.
- Nessuno scenario del banco: lo standalone non ha questa rotta.

## Prove

- **TDD:** test del dominio, della pagina e del comando rossi prima in compilazione, poi con le sole
  firme:
  - 9 rossi nel dominio («da fare»);
  - nella pagina, 4 rossi per la rotta mancante, più quelli della tabella dei permessi, del CSRF e
    del guscio;
  - nel comando, 3 rossi, più i test di `serve` rossi per la dipendenza non collegata.
  - Dopo il primo giro di mutazioni si sono aggiunti due test: un codice speso resta speso anche se
    `utenti` torna vuota, e i codici aperti si annullano dopo la creazione.
- **Mutazioni** (su una copia): **38 uccise su 41**. Le 3 vive sono equivalenti:
  - togliere una sola delle due guardie su `utenti` vuota nella scrittura: resta l'altra, e togliere
    tutte e due è uccisa;
  - togliere l'uscita anticipata prima della derivazione: cambia solo il costo di un codice morto.
- `scripts/check` verde; `-count=10` verde sui pacchetti toccati.
- Prova a mano: il binario stampa il link sotto un terminale vero (`script`); `GET` del codice dà
  200, uno inventato 404; il codice non compare nel registro del processo.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** da fare, dopo `8a5c63a` (019).

## Verifica della sessione principale e review indipendente

- La sessione principale ha rilanciato `scripts/check` (verde, ACC-16 chiusa: 26 voci chiuse con
  l'esito del banco) e il banco con `fab-219` (verde, spento, nessun avanzo). Ha anche rifatto le
  41 mutazioni su una copia: 38 uccise, 3 vive equivalenti, nessun errore.
- Review indipendente: nessuna ALTA né MEDIA. Verificati dal revisore:
  - nessun secondo titolare e nessun titolare con account già presenti: guardie dentro una
    transazione `immediate`, corse con 1 o 2 codici;
  - il codice: entropia, confronto per hash, scadenza e annullamento;
  - nessuna fuga: un solo 404, `no-store`, nessun codice nei registri;
  - PBKDF2 solo dopo un codice valido;
  - CSRF, permessi e superficie;
  - le regole di email e password uguali allo standalone;
  - `-count=20` e `-race` verdi.
- BASSA, dichiarate:
  - il controllo del terminale accetta anche `/dev/null`: il link si perde, senza fuga. C'è già
    in `reset-link` e `invito`, e si corregge per tutti con un vero isatty in una slice piccola;
  - le differenze dallo standalone (IP nella riga, riga `bootstrap_requested` in più) sono già
    scritte qui sopra;
  - **nota per l'import:** un import o un ripristino che lasciasse `utenti` vuota con un codice
    aperto lo renderebbe di nuovo usabile per al massimo 60 minuti. L'import annulla i codici
    aperti.
- Decisioni da confermare con Stefano: in `docs/domande-aperte.md`.
