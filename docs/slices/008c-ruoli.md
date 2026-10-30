# 008c — Ruoli titolare e dipendente, durata delle sessioni (T1)

- **Fonte:** prompt di Stefano del 30/10/2026 (`prompt-sessione-008c.md`): ruoli con i permessi
  dello standalone a `go-baseline` = `d3bab62`, **compresi** EME-13 e CLI-24/25 solo del titolare
  (slice 217); regola dell'ultimo titolare con password (ACC-10, ACC-11); disattivazione v1 con la
  sessione che cade al primo clic; controllo del ruolo lato server su ogni richiesta, con un test
  di richiesta forgiata dal dipendente per ogni azione «solo titolare». In più **D-31** (Stefano,
  30/10/2026): sessione al massimo 30 giorni dall'accesso, durata assoluta.
- Standalone letto a `d3bab62`: `router.ex` (pipeline `:browser` con `protect_from_forgery` prima
  di `:require_owner_user`, `live_session :owner`), `user_auth.ex` (`require_owner/2`,
  `on_mount(:ensure_owner)`), `accounts.ex` (`deactivate_user/2`, `set_role/3`,
  `other_active_owner/1`), gli eventi con `Accounts.owner?/1` nelle pagine comuni, i test di
  `accounts_users_test.exs`.
- **Confine:**
  - registrazione delle rotte della faccia interna con chi le può usare (aperta, personale,
    titolare) e la **tabella dei permessi** dello standalone come test: ogni rotta Go deve stare
    nella tabella con lo stesso permesso;
  - rifiuto lato server del dipendente su una rotta del titolare: 302 a `/` con «Operazione
    riservata al titolare.», pagine e POST, CSRF controllato prima (come lo standalone);
  - ruolo riletto a ogni richiesta (titolare declassato fuori al clic dopo);
  - regole dell'ultimo titolare in `internal/accounts` (`Disattiva`, `CambiaRuolo`), con la
    scrittura protetta in un'istruzione sola;
  - D-31 nel cookie di sessione.
- **Fuori:** pagina utenti (ACC-06), inviti, audit (ADM-16: le righe `user.deactivated` e
  `user.role_changed` vanno nella stessa transazione quando arriva), «chiudi tutte» (ACC-12). Le
  azioni «solo titolare» dentro pagine che il Go non ha ancora (EME-12/13/14, CLI-24/25, vendite,
  cestino) sono **nella tabella** con il loro messaggio; il test forgiato di ognuna arriva con la
  sua pagina. La tabella lo rende difficile da dimenticare: una rotta Go che non sta nella
  tabella, una voce del titolare con una rotta di tutti o un `mux.Handle` diretto fanno fallire
  i test.
- **Voci:** ACC-05, ACC-04 (ruoli), ACC-01/03/04 per D-31; ACC-10 e ACC-11 solo per la regola
  dell'ultimo titolare (restano parziali finché mancano pagina e audit).
- **Livello:** completo (ruoli, autenticazione).

## Misure sullo standalone (banco `grgo-s008c`, immagine `fab-218` = `d3bab62`)

Con un dipendente sintetico seminato con le funzioni dello standalone (`invite_user/2` e
`accept_invitation/2`), e una sonda HTTP usa e getta dentro la rete interna:

- dipendente su `/audit`, `/settings`, `/settings/utenti`, `/settings/shop`, `/audit/export.csv`,
  `/export.zip`: **302 `/`**, e la pagina dopo dice «Operazione riservata al titolare.»;
- dipendente in POST su `/settings/sessions/terminate-all`: senza token **403**, con token **302
  `/`** e il messaggio; lo stesso su `/handovers/history/1/delete` e `/repairs/1/firma/reset`;
- senza accesso: `GET /audit` e il POST con token → **302 `/login`**;
- dipendente su `/emergenza` e `/cestino`: 200 (pagine comuni);
- ruoli cambiati con `set_role/3` mentre le due sessioni sono aperte: il titolare declassato su
  `/audit` → 302 `/` con il messaggio, il dipendente promosso → 200, senza rifare l'accesso.

Il Go fa lo stesso (test). La durata massima della sessione non si misura: lo standalone non ne
ha (D-31).

## Cosa c'è

- `internal/accounts/utenti.go`: `Titolare` e `Dipendente`; `Disattiva` e `CambiaRuolo` con la
  regola dell'ultimo titolare (`restaUnTitolare`, come `other_active_owner/1`: un **altro**
  titolare attivo **con password**) dentro un'unica `UPDATE`; il motivo del rifiuto si legge dopo
  (`ErrUltimoTitolare`, `ErrNonTrovato`, `ErrRuoloNonValido`). Nessuna pagina li usa ancora.
- `internal/web/ruoli.go`: `f.rotta(mux, pattern, permesso, h)` con la catena dello standalone
  (pagina e sessione; CSRF per i POST; accesso; ruolo) e `f.speciale` per `/healthz` e
  `POST /login`. `richiedeTitolare` legge il ruolo riletto a ogni richiesta.
- `internal/web/sessione.go`: istante dell'accesso nel cookie; scaduto dopo 30 giorni di 24 ore
  (D-31), controllato dal server; il cookie resta senza Max-Age come quello dello standalone.
- `internal/web/ruoli_test.go`: `tabellaPermessi`, la tabella dello standalone a `d3bab62`
  (rotte, pagine del titolare, azioni del titolare dentro pagine comuni con il loro messaggio).
- Banco: titolare **e dipendente** sintetici sulle due istanze, segnaposto
  `{email_dipendente}` e `{password_dipendente}`, scenario `ruoli.json`.

## Prove

- TDD: 10 test rossi prima del codice (5 negli account, 5 nella faccia web: tabella, dipendente
  che forza, ruolo riletto, 30 giorni, cookie senza accesso valido) più 3 del banco (segnaposto,
  semina, credenziali montate); dopo la review altri 2 rossi (accesso nel futuro) e le due guardie
  nuove, provate a mano con una riga e una rotta sbagliate.
- Banco con l'immagine `fab-218` (= `go-baseline` `d3bab62`, contiene la migrazione della 218):
  **BANCO VERDE**, 4 scenari, 76 passi, 0 differenze; spento, nessun avanzo.
- Mutazioni: **33 uccise su 34 che compilano**. Viva una, equivalente: l'istante dell'accesso non
  azzerato quando la sessione non vale più (il cookie resta senza account; l'accesso dopo lo
  riscrive). Un'altra viva al primo giro (`accesso > 0`) era una clausola ridondante, tolta; una
  terza ha fatto aggiungere `TestDipendenteSenzaTitolariConPassword`.
- `scripts/check` verde con l'esito del banco: chiuse ACC-01…05, ACC-15, ADM-20, JOB-01;
  parziali ACC-10 e ACC-11 (manca l'audit, test saltato apposta).

## Review indipendente

Nessun problema alto. Corretto:

- **MEDIA:** nella tabella tre rifiuti delle vendite erano sbagliati: `trash`, `delete` e `reopen`
  rispondono al dipendente «Operazione riservata al titolare.» (`owner_denied/1`); i messaggi che
  avevo scritto sono quelli del titolare quando l'operazione fallisce.
- **MEDIA:** l'evento `delete` di SalesLive è l'eliminazione di una **bozza**, VEN-07, non VEN-18.
- **MEDIA:** la tabella non obbligava davvero la slice futura: una riga nuova `personale` per
  CLI-24, o un `mux.Handle` diretto, passavano. Ora `TestUnaVoceUnPermesso` e
  `TestNessunaRottaFuoriDallaTabella`.
- **BASSA:** D-31 con l'orologio avanti all'accesso: la sessione sarebbe durata 30 giorni più
  l'errore. Ora un accesso più di 5 minuti nel futuro non vale.
- **BASSA:** mancavano `/reset-request` e `/reset/:token` (aperte, ma solo per il titolare nel
  dominio): aggiunte alla tabella.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Riferimento:** `go-baseline` = `d3bab62` (slice 218), spostato in questa sessione.
- **Commit:** questo (slice 008c), dopo `48ac3db` (riallineamento 214-218 e D-31).
