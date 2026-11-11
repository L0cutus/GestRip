# 069 — Provider SMTP per la coda in uscita (T4.6, quarta slice, consegna parziale)

- **Fonte:** via dell'Orchestratore dell'11/11/2026 (via di Stefano del 09/11 per la T4.6), ripresa
  `2026-11-11-fine-068.md`. Lo standalone a `go-baseline` = `302439d` (`smtp.ex`, `tcp.ex`, `config.ex`,
  `outgoing.ex`, `providers_live.ex`, letti; non fatti girare).
- **Voci:** CLI-43 (invio email SMTP), CLI-51 (coda, cablaggio del provider). **Parziali:** CLI-52 (parte
  email) e ADM-09 (manca la pagina «Provider di invio»), D-03 e D-27 (manca l'accensione), D-16 (c'è solo
  `uscita.EmailAttiva`, i riquadri sono della 070).
- **Confine:** la pagina del titolare, l'accensione del canale dopo l'import e la revisione delle email
  importate in coda vanno insieme in una **069b**: oggi nessuno può accendere l'email, il provider è
  pronto ma fermo.
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna
  migrazione (la configurazione usa `impostazioni` e `segreti_provider`, già nello schema).

## Cosa c'è

- **Provider** (`internal/uscita/smtp.go`, `uscita.SMTP`): conversazione come `smtp.ex` (EHLO, STARTTLS o
  TLS implicito, AUTH solo su TLS, mai in chiaro; STARTTLS rifiutato = fine senza credenziali; certificato
  e nome host verificati, TLS ≥ 1.2), intestazioni come lo standalone (nessun `Message-ID`, oggetto e
  nome codificati se servono, testo in base64 a righe di 76), errori `smtp:<codice>` o un genere fisso
  (`:tls`, `:timeout`, `:protocol`), mai il testo del server, l'indirizzo o la password. 4xx e 5xx sono
  tutti un tentativo della coda, fino al quinto (come lo standalone). Tempi: 15 s per ogni lettura e
  scrittura, al più 2 minuti per tutta la conversazione, al più 512 righe di continuazione e 64 KiB per
  riga; il contesto annullato chiude la connessione.
- **Configurazione** (`internal/uscita/config_smtp.go`): `LeggiSMTP` da `impostazioni`
  `outgoing.smtp.*` e la password da `segreti_provider` (dove l'import già le mette e dove la pagina della
  069b le scriverà), riletta a ogni tentativo; tipo `Password` che non si stampa. `Usabile()` come
  `usable?/1`, più una **seconda serratura** solo Go (differenza senza D): falso con una `security` fuori da
  `none`/`tls`/`starttls` e con spazi, a capo, `<` o `>` in server, mittente o utente. Interruttore solo Go
  `canale_email_acceso` (D-03), in `importer.ChiaviSoloGo`: oggi nessuno lo scrive, quindi l'email è spenta.
- **Coda** (`coda.go`): `CanaleAttivo` (= `configured?/1`); con l'email spenta `Accoda` risponde
  `ErrNonConfigurato` e il giro non legge le righe email; il canale si ricontrolla riga per riga (spento a
  metà giro = riga saltata senza scrivere). `gestrip serve` registra sempre il provider SMTP, quindi la coda
  gira sempre (circa 8 SELECT ogni 30 s, nessuna scrittura con tutto spento), così una configurazione nuova
  vale senza riavviare, come lo standalone.

## Prove

`uscita`: `smtp_test.go` (18), `smtp_limiti_test.go` (9: scadenza complessiva, continuazioni, contesto
annullato, seconda serratura, mittente con a capo, canale spento durante il giro, riga lunga, tempo di
scrittura, tempo del TLS), `config_smtp_test.go` (6), con un server SMTP finto in processo
(`smtp_finto_test.go`: 127.0.0.1, porta casuale, certificato generato nel test). `cmd/gestrip`:
`TestServeEmailSMTP`. Nessun server vero, nessuna email vera. Rosso preso prima del codice (non compilava,
poi 20 test rossi con gli stub; per la review 6 test rossi).

## Mutazioni

Implementazione: 43 (27 più 16 per la review), tutte compilano; 42 uccise, 1 equivalente (TLS dopo
STARTTLS senza scadenza propria: resta quella di lettura). Review: 24 che compilano, 20 uccise, 4
sopravvissute (riga di 64 KiB, tempo di scrittura → test nuovi; stretta di mano e `MinVersion`
equivalenti).

## Banco

Nessuno scenario nuovo: senza un SMTP vero non c'è niente da confrontare (la pagina dei provider arriva con
la 069b, i riquadri con la 070). Giro di regressione con la coda sempre accesa (`s069`, `fab-225`,
`-dati sintetici-import`): **BANCO VERDE**, 61 scenari, 1097 passi, 0 inattese; **BANCO BROWSER VERDE**,
15 scenari, 30 viste, 0 inattese; nessuna riga della coda nel registro. `banco giu`. Le correzioni della
review toccano solo il ramo di una riga di un canale acceso, che nel banco non c'è: giro non rifatto.

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Dubbi

1. **Configurazione nel database** (`impostazioni` e `segreti_provider`) invece che nel file della macchina:
   come lo standalone e l'import, e l'unico modo perché il titolare la cambi dalla pagina.
   **Raccomandazione:** tenerla.
2. **Accensione (D-03) e email importate in coda (D-27):** nella 069b un «Accendi» nella pagina dei provider
   che prima mostra le email importate in sospeso da scartare o mandare una per una. Senza la revisione, al
   primo giro partirebbero tutte insieme. **Raccomandazione:** l'accensione non esce senza la D-27.
3. **Coda sempre accesa** (la 068 la teneva spenta senza provider). **Raccomandazione:** accettarlo.
4. **Difetto dello standalone copiato:** un nome mittente tutto ASCII ma con virgole o virgolette passa senza
   codifica (`Rossi, Mario` diventa due indirizzi; `"Banca" <x@y>` cambia il nome mostrato).
   **Raccomandazione (review):** codificarlo sempre quando non è semplice: è innocuo, ma è una differenza
   dallo standalone e va decisa.
5. **Oggetto senza limite di lunghezza** (oltre 998 byte un server può rifiutarlo), come lo standalone; gli
   oggetti vengono dai testi dell'editor. **Raccomandazione:** lasciarlo.

## Review

Revisore indipendente, 11/11/2026 (pc-fisso, slice non committata). Nessuna ALTA.

- **MEDIA, corretto:** i tempi massimi valevano per ogni lettura e il contesto non interrompeva la
  conversazione: un server «a goccia» bloccava la coda e la chiusura di `serve`.
- **BASSA, corretti:** mittente con a capo che apriva comandi (seconda serratura), sicurezza sconosciuta in
  chiaro, canale spento a metà giro che consumava un tentativo.
- **BASSA, Dubbi 4 e 5:** nome mittente non codificato, oggetto lungo.
- **Verificato senza rilievi:** AUTH mai in chiaro, certificato e nome host, iniezione dopo STARTTLS
  buttata, risposte enormi, intestazioni e corpo, errori senza dati, password mai stampata, allineamento a
  `smtp.ex`/`config.ex`, import della chiave e del segreto, chiusura di `serve`. `scripts/check` verde.
