# 010 — Registro delle azioni e pagina utenti (T1, T4.15)

- **Fonte:** Stefano, 30/10/2026: «audit + pagina utenti (chiudi ACC-10, ACC-11 e la riga del
  blocco di ACC-20), poi reset password». Standalone a `go-baseline` = `6eb93ed`: `audit.ex`,
  `workshop/audit_event.ex`, migrazione `20260905070000` (`audit_events`), `accounts.ex`
  (`list_users/0`, `deactivate_user/2`, `set_role/3`), `live/users_live.ex`,
  `attempt_limiter.ex` (`record_block/2`).
- **Confine:**
  - tabella `eventi_audit` e scrittura di un evento (ADM-17), dentro la transazione della modifica
    quando c'è;
  - `Disattiva` e `CambiaRuolo` con la riga `user.deactivated` e `user.role_changed` nella stessa
    transazione (ACC-10, ACC-11);
  - la riga `security.attempts_blocked` all'inizio di ogni blocco del limitatore (ACC-20, CLI-21);
  - pagina `/settings/utenti` del titolare: elenco (ACC-06), «Mostra disattivati», «Disattiva» e
    cambio di ruolo.
- **Fuori:** inviti, «Nuovo link» e `/invito` (ACC-07, 08, 09: slice dopo, con il modulo «Aggiungi
  un account»); pagina del registro (ADM-16); conservazione di 5 anni (D-14, con i lavori
  automatici); reset della password (slice dopo).
- **Senza LiveView:** le azioni della pagina sono moduli POST con CSRF che tornano alla pagina con
  il messaggio dello standalone; il cambio di ruolo parte alla scelta nel menu, come
  `phx-change`.
- **Voci:** ADM-17, ACC-06, ACC-10, ACC-11; la riga del blocco di ACC-20 e CLI-21.
- **Livello:** completo (ruoli, audit, account).

## Cosa c'è

- `internal/db/migrazioni/0003_eventi_audit.sql`: `eventi_audit` con le regole di `audit_events`
  (campi obbligatori non vuoti e lunghezze di `AuditEvent.changeset`, dati oggetto JSON `{}` di
  default), istante nella forma fissa, id mai riusati.
- `internal/audit`: `Scrivi` dentro la transazione di chi chiama, `Registra` da sola; attore, ip e
  user agent dal contesto (vuoti → nulli), id della risorsa come testo.
- `internal/accounts`: `Disattiva` e `CambiaRuolo` in una transazione con la scrittura protetta e
  la riga `user.deactivated` / `user.role_changed` (`{"role": …}` con il nome Go del ruolo); un
  rifiuto non scrive righe; una riga che non si scrive annulla la modifica. `ElencoUtenti` come
  `list_users/0`. La sessione porta l'email dell'account (l'attore delle sue azioni).
- `internal/web/utenti.go` e `pagine/utenti.html`: `/settings/utenti` del titolare con i testi
  di UsersLive (dipendente scritto «Staff»), «Mostra/Nascondi disattivati (N)», «Disattiva» con la
  conferma, il menu del ruolo che parte alla scelta; i messaggi di esito e di errore dello
  standalone; messaggio di esito (`:info`) una volta sola.
- Limitatore: `registraBlocco`, la riga `security.attempts_blocked` all'inizio di ogni blocco
  (login: un secchio per riga; pagine pubbliche); se non si scrive, la risposta non cambia e resta
  un avviso senza dettagli nel registro del processo.
- Tabella dei permessi: `GET /settings/utenti`, `POST …/{id}/disattiva` (ACC-10) e
  `POST …/{id}/ruolo` (ACC-11), solo del titolare.
- Banco: scenario `utenti.json` (pagina del titolare con i testi, dipendente rimandato a `/` con
  «Operazione riservata al titolare.»).

## Prove

- TDD: i test di tabella e audit rossi senza la migrazione (provato togliendola), quelli degli
  account e della pagina rossi in compilazione (firme e metodi nuovi), poi verdi. Due test sbagliati
  da me e corretti: l'ultimo accesso mostrato è quello del login appena fatto; un ruolo
  **mancante** dà «Ruolo non aggiornato.», uno **vuoto** «Ruolo non valido.», come lo standalone.
- Banco **verde** con `fab-219`, 6 scenari, 92 passi.
- Mutazioni: **28 uccise su 28 che compilano** (dopo la review); una viva al primo giro (il
  messaggio di esito che restava alla pagina dopo) ha fatto aggiungere il controllo.
- `scripts/check` verde: chiuse ACC-06, ACC-10, ACC-11 (in più ACC-01…05, ACC-15, ADM-20,
  JOB-01); parziali ADM-17 (conservazione D-14), ACC-20 (limiti del reset), CLI-21 (prova dietro
  Funnel).

## Review indipendente

Nessun problema alto. Controllati dal revisore: permessi delle tre rotte nuove, audit dentro la
transazione con rollback, regola dell'ultimo titolare senza corse, `Id.positive`, testi e
ordine, riga del blocco senza dati del cliente, HTML senza punti iniettabili. Corretto:

- **MEDIA:** la riga `security.attempts_blocked` si perdeva se il cliente chiudeva la connessione
  (contesto della richiesta annullato): un attaccante decideva se il registro vedeva i suoi
  blocchi. Ora la scrittura non dipende dalla richiesta (`context.WithoutCancel`, al massimo 5 s).
- **BASSA:** «Mostra disattivati» si richiudeva dopo un'azione riuscita; nello standalone resta
  aperto (la pagina non si ricarica) e si richiude solo sui rifiuti del ruolo (`push_navigate`).
  Ora i moduli portano lo stato.
- **BASSA:** un campo `ruolo` ripetuto dava «Ruolo non aggiornato.»; ora vale l'ultimo, come Plug.
- **BASSA:** test più stretti sulle righe dei disattivati (né menu del ruolo né «Disattiva»).

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `9e6acdf` (009b).
