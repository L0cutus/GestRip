# 012 — Inviti: aggiungi un account, nuovo link, scelta della password (T4.15)

- **Fonte:** Stefano, 30/10/2026: «Poi gli inviti (ACC-07/08/09): finché non ci sono i canali il
  titolare vede/copia il link d'invito dalla pagina utenti o lo genera dal comando locale.» e
  «Per la 012 torna ai test prima rossi, poi il codice: gli inviti toccano la sicurezza.»
  Standalone a `go-baseline` = `6eb93ed`: `accounts.ex` (`invite_user/2`, `reissue_invitation/2`,
  `accept_invitation/2`, `invitation_recipient/1`), `accounts/invitation.ex`, `accounts/user.ex`
  (`invitation_changeset/2`), `live/users_live.ex`, `controllers/invitation_controller.ex` e i
  due modelli, migrazione `20260915090000_create_user_management.exs`; test
  `accounts_users_test.exs`, `users_live_test.exs`, `invitation_controller_test.exs`.
- **Confine:**
  - tabella `inviti_utenti` con le regole di `user_invitations` (solo lo SHA-256 del token,
    unico; scadenza, accettazione, revoca; righe cancellate con l'account);
  - `Invita`: account senza password e link monouso di 15 minuti, nella stessa transazione della
    riga `user.invited` con il ruolo; email normalizzata, forma `^[^@\s]+@[^@\s]+\.[^@\s]+$`, al
    massimo 254 caratteri, unica solo tra gli attivi; nome al massimo 120, vuoto → nullo; ruolo
    titolare o dipendente (dipendente se manca);
  - `NuovoLink`: solo per un account attivo senza password, annulla i link aperti di quell'account,
    riga `user.invitation_reissued`; il controllo sta dentro la scrittura;
  - `/invito/{token}` sulla faccia interna (D-28): aprire non spende; ogni rifiuto è la stessa
    pagina «Invito non valido» con 404; password diverse o corte → 422 e il link resta; riuscito
    → `/login` con «Password impostata. Ora puoi accedere.»; riga `user.invitation_accepted` con
    l'email come attore; spesa del link e password nella stessa transazione;
  - pagina utenti: modulo «Aggiungi un account», riquadro del link mostrato una volta, «Nuovo
    link» per gli account in attesa, i messaggi dello standalone. Il link si costruisce
    sull'indirizzo interno del file dell'apparecchio (011b), mai sull'`Host`;
  - `gestrip invito` (D-32): invita un account nuovo o rigenera il link di uno in attesa; link solo
    sul terminale, riga nel registro con attore «console»; stesse regole di `reset-link`.
- **Fuori:** invio del link per email o Telegram (lo standalone non lo fa: si consegna a mano).
- **Voci:** ACC-07, ACC-08, ACC-09.
- **Livello:** completo (account, password, link, audit).

## Cosa c'è

- `internal/db/migrazioni/0005_inviti_utenti.sql`: `inviti_utenti` con le regole di
  `user_invitations`.
- `internal/accounts/inviti.go`: `Invita`, `NuovoLink`, `Destinatario`, `Accetta`,
  `AccountAttivo`. Le condizioni di monouso, scadenza, revoca e account attivo stanno dentro le
  scritture; `Accetta` rifiuta un link morto prima della derivazione PBKDF2 (come `Reimposta`).
- `internal/web/inviti.go`, `pagine/invito.html`, `pagine/utenti.html`: modulo «Aggiungi un
  account», riquadro del link, «Nuovo link», `/invito/{token}`. Il link appena creato sta solo
  nella risposta al modulo, con `Cache-Control: no-store`; mai nel cookie di sessione. Dopo un
  rifiuto il modulo ripropone l'email normalizzata e il nome senza spazi, come il changeset.
- `gestrip invito -email … [-ruolo] [-nome] [-url] [-db] [-config]` (D-32); `serve` passa alle
  pagine l'indirizzo interno del file dell'apparecchio. `invito` e `reset-link` rifiutano gli
  argomenti in più.
- Banco: scenario `invito.json` (rifiuti di `/invito` in GET e POST). Il resto della pagina utenti
  nello standalone è LiveView e il banco HTTP non lo raggiunge.
- Inventario: D-32 e le note su ACC-07 e ACC-08.

## Prove

- TDD: test del dominio, delle pagine e del comando rossi in compilazione; poi, con le sole firme,
  **21 test rossi** per il motivo giusto (funzioni e rotte mancanti); poi verdi. Dopo la review:
  rossi prima i test di `no-store`, dei valori normalizzati, di `user[]` e degli argomenti in più.
  Il test delle guardie dentro la scrittura di `Accetta` era verde da subito (il codice era giusto):
  lo provano le mutazioni.
- Mutazioni (in una copia): **56 uccise su 57 che compilano**. Viva una, equivalente: togliere il
  controllo dell'id positivo nel «Nuovo link» (l'id 0 o non numerico finisce comunque in «Nessun
  nuovo link»). Le vive del primo giro hanno avuto il loro test: `\v` nel dominio, destinatario di
  un link revocato, modulo con una sola casella, invitato disattivato senza «Nuovo link», guardie
  di `Accetta` tra pre-controllo e scrittura.
- Banco **verde** con `fab-219`: 8 scenari, 103 passi; spento, nessun avanzo. Il testo del rifiuto
  dello standalone va a capo nell'HTML: lo scenario lo controlla in due pezzi.
- `scripts/check` verde con l'esito del banco: chiuse **ACC-07, ACC-08, ACC-09**.

## Review indipendente

Nessun difetto di sicurezza nel codice. Verificato dal revisore: token solo per SHA-256, mai in
log, errori, audit, cookie; link mai sull'`Host`; 30 corse tra `Accetta` e `NuovoLink` senza due
esiti positivi; 8 `Invita` simultanei con la stessa email danno un account; audit nella
transazione; permessi e richieste forgiate. Corretto:

- **MEDIA:** le condizioni dentro la scrittura di `Accetta` non avevano un test (il pre-controllo le
  copriva): ora un orologio finto fa scadere, revocare o disattivare tra i due momenti.
- **MEDIA:** nessun test sulla GET di un link revocato: ora `Destinatario` lo rifiuta nel test.
- **MEDIA:** la risposta con il link poteva restare nella cache del browser: ora `no-store`.
- **BASSA:** valori grezzi nel modulo dopo un rifiuto; `user[]` dava «Email non valida.» invece di
  «Account non aggiunto.»; mancavano i casi `\v`, una sola casella, invitato disattivato;
  argomenti in più ignorati dal comando.

Dichiarato: ricaricare (F5) la risposta di «Nuovo link» e confermare il nuovo invio del modulo crea
un altro link e annulla quello appena consegnato; il browser chiede conferma prima di rinviare. Nello
standalone (LiveView) ricaricare non rifà l'evento. Il link resta fuori dal cookie apposta.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `26ecb7d` (011b).
