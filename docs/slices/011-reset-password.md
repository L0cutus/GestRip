# 011 — Password dimenticata del titolare: pagina, limiti e comando locale (T1, T4.15)

- **Fonte:** Stefano, 30/10/2026: «fai ora la pagina del reset con i suoi limiti (chiudi ACC-20) e
  il comando locale che genera il link (D-04). Il link solo nel terminale, mai nei log, scadenza
  breve, monouso, riga nel registro. Invio via Telegram/email quando arrivano i canali.»
  Standalone a `go-baseline` = `6eb93ed`: `password_recovery.ex`,
  `controllers/password_reset_controller.ex` e i suoi tre modelli, migrazione
  `20261027200100_create_password_resets.exs`.
- **Confine:**
  - tabella `reset_password` con le regole di `password_resets`, più l'origine (pagina o console);
  - `/reset-request` e `/reset/{token}` sulla faccia interna (D-28), con i testi, gli stati e le
    regole dello standalone: la pagina non chiede niente e risponde sempre uguale; il lavoro gira
    dopo la risposta; 3 richieste l'ora per indirizzo e 5 al giorno in tutto, contate sulle righe;
    un link nuovo annulla quelli aperti; 30 minuti, monouso; il reset chiude tutte le sessioni
    dell'account e annulla gli altri link;
  - senza canali (il Go non ha ancora Telegram né email) la richiesta dalla pagina trova «nessun
    canale», come lo standalone senza canali: il link si annulla e il registro lo dice;
  - `gestrip reset-link` (D-04): stampa il link solo sul terminale, mai nel registro del processo;
    stessa durata, monouso, riga `password_reset.requested` nel registro delle azioni; non conta
    nei limiti della pagina.
- **Fuori:** invio del link e avviso «password cambiata» via Telegram ed email (T4.6, T4.7: ACC-13
  e ACC-14 restano parziali).
- **Voci:** ACC-13, ACC-14 (senza i canali), ACC-20 (limiti del reset: si chiude).
- **Livello:** completo (autenticazione, password, audit).

## Cosa c'è

- `internal/db/migrazioni/0004_reset_password.sql`: `reset_password` con le regole di
  `password_resets` (solo lo SHA-256 del token, unico; scadenza, uso, revoca, ip, canale; righe
  cancellate con l'account) e l'origine (pagina o console).
- `internal/accounts/recupero.go`: `RichiestaDallaPagina` (limiti contati sulle righe della
  pagina, prima con letture semplici e poi di nuovo nella transazione di scrittura; titolare =
  il più vecchio attivo con password; un link nuovo annulla quelli aperti; senza canali il link
  si annulla e il registro lo dice; il rifiuto per limite si scrive una volta l'ora per
  indirizzo); `LinkDallaConsole` (D-04); `AccountDel`; `Reimposta` (password diverse e corte
  prima, link morto senza derivazione, poi in una transazione: link speso, password scritta solo
  su un titolare attivo, sessioni chiuse, altri link annullati, riga `password_reset.completed`).
- `internal/web/reset.go` e `pagine/reset.html`: `/reset-request` e `/reset/{token}` con i testi,
  i codici (200, 422, 400) e i messaggi dello standalone; la richiesta lavora dopo la risposta,
  con un contesto che sopravvive alla richiesta; il reset riuscito fa ripartire la sessione del
  browser e porta al login con «Password aggiornata. Accedi con la nuova password.».
- `gestrip reset-link -url https://host[:porta] [-db FILE]`: stampa il link (con l'email del
  titolare e la scadenza) **solo se l'uscita è un terminale**; non crea un database che manca;
  controlla l'indirizzo (https, nome di host valido, porta 1-65535, niente percorso né query).
- Banco: scenario `reset.json` (pagina, richiesta, link non valido in GET e POST).

## Prove

- TDD: i test del dominio, delle pagine e del comando rossi in compilazione (tipi e funzioni
  nuovi), poi verdi; dopo la review 5 test nuovi rossi prima (lavoro dopo la risposta, finestra
  dell'indirizzo, riga del limite ogni ora, titolare disattivato, database mancante e terminale).
- Banco **verde** con `fab-219`, 7 scenari, 97 passi; spento.
- Mutazioni: **35 uccise su 39 che compilano**. Vive quattro, equivalenti: `origine = 'pagina'`
  nel conteggio per indirizzo (le righe della console non hanno ip); il controllo dei limiti prima
  della transazione (la transazione ricontrolla: cambia solo l'attesa); il calcolo di un token
  oltre i 64 caratteri (non corrisponde comunque); la sessione del browser che fa il reset (cade
  comunque, perché il reset cambia la versione delle sessioni).
- `scripts/check` verde: chiusa **ACC-20**; parziali ACC-13 e ACC-14 (invio e avviso sui canali,
  test saltati), ADM-17, CLI-21.

## Review indipendente

Nessun problema alto. Verificato dal revisore: il token non compare in registro, errori, audit,
pagine o database in chiaro; 20 richieste simultanee dallo stesso indirizzo lasciano 3 righe; 8
reset simultanei con lo stesso link danno 1 riuscito. Corretto:

- **MEDIA:** nessun test proteggeva il lavoro dopo la risposta: con il contesto della richiesta
  la riga non si sarebbe scritta e i limiti non sarebbero mai scattati. Ora c'è.
- **MEDIA:** test mancanti sulla finestra di un'ora per indirizzo, sul titolare disattivato e
  sulla riga del limite ogni ora.
- **BASSA:** il comando creava un database vuoto se il percorso era sbagliato; stampava anche su
  una pipe o un file; accettava porte e nomi di host sbagliati; leggeva l'email dopo il commit.
- **BASSA:** aggiunto il controllo dei limiti con letture semplici prima del lucchetto di
  scrittura (`precheck/1` dello standalone).

Dichiarato: chiunque raggiunga la faccia interna può annullare con «Invia link reset» il link
appena stampato dal comando (un link nuovo annulla i vecchi, come nello standalone); senza
canali il link nuovo non arriva a nessuno, quindi il titolare rilancia il comando.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `3f7ca66` (010).
