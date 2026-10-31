# 031 — Pagina «Nuova riparazione» (T4.1)

- **Fonte:** T4.1 del PIANO; piano delle slice nella ricevuta della 030.
- **Voci:**
  - chiuse: RIP-H-33, 34, 35, 36;
  - parziali: RIP-H-29 (ramo con l'AI, T4.10), 30 (foto, T4.3), 31 (numero proposto con l'AI,
    T4.10), 37 (scheda e modifica, 032–033), 38 (segni nella lista, T4.2), 39 («📷 Scansiona»,
    JavaScript), 41 (esito nella lista e dettato, T4.2 e T4.10);
  - aperta: RIP-H-32 (suggerimenti mentre si scrive, JavaScript).
- **Lavoro:** implementazione e due giri di correzioni di un sottoagente, review di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- `GET` e `POST /nuova` per titolare e dipendente, link «+ Nuova» dalla home, il ramo «AI non
  configurata» dello standalone con i testi esatti. Si leggono solo i campi di `@intake_fields`;
  gli altri si scartano. Errori sul modulo con i valori inseriti; dopo un errore il modulo
  rimostra il numero risolto, come il changeset dello standalone.
- Avviso del QR mancante lato server, con i casi dello standalone (slice 209).
- **Indirizzo pubblico:** nuova riga `indirizzo_pubblico = https://host[:porta]` nel file della
  macchina, con le regole di `indirizzo_interno` (011b). Il link è `<indirizzo_pubblico>/r/<token>`.
- **Esito e link mostrati una volta:** redirect a `/?esito=<id>`, con un identificativo casuale che
  non è il token; la voce sta in RAM, legata a utente e sessione, dura 5 minuti e si consuma alla
  prima GET. Il token non va mai in URL, cookie, intestazioni, registro o audit.
- **Doppio invio:** ogni modulo porta un identificativo d'invio; un secondo POST con lo stesso non
  crea niente e porta allo stesso esito, anche se arrivano insieme. `app.js` disabilita il
  pulsante, come fa LiveView con `phx-submit` (letto in `phoenix_live_view.js` 1.2.11
  dell'immagine `fab-219`).

## Differenze di forma, dichiarate

Nessuna cambia ciò che vede chi lavora; vengono tutte dal passaggio da LiveView a pagine HTTP:
- l'avviso del QR viaggia in un campo nascosto (`qr_avvisato`) invece che nello stato del socket:
  chi lo forza salta solo l'avviso;
- l'esito sta su `/?esito=` e in una memoria in RAM: con un riavvio entro 5 minuti si perde, e il
  link si potrà rigenerare dalla scheda (035);
- la pagina dell'esito ha `Cache-Control: no-store`;
- senza `indirizzo_pubblico` la home lo dice e spiega cosa scrivere: caso che lo standalone non ha;
- l'esito compare sulla home provvisoria; la lista vera è la T4.2;
- «Togli data» è copiato com'è nello standalone, cioè con JavaScript.

Annotati per dopo: il titolo della home («Riparazioni · Laboratorio») e il nome del negozio nel
guscio vanno con la T4.2.

## Prove

- **TDD:** rosso misurato prima del codice nel primo giro. Nel giro delle correzioni i test sono
  stati scritti prima ma eseguiti solo dopo, sulla copia del codice di prima: rossi (14 test web e
  la compilazione).
- **Mutazioni:** 10, poi 15, poi 24 sul codice nuovo, tutte uccise; le tre vive dei giri
  intermedi hanno avuto il loro test.
- **Banco** (`fab-219`): verde su database vuoto e con `-dati sintetici-import`, 14 scenari e 165
  passi. Lo scenario nuovo `nuova` confronta home e `/nuova` per titolare e dipendente. Il
  salvataggio nello standalone è un evento LiveView sul websocket: il livello HTTP non lo può
  confrontare, quindi avviso QR, errori, campi forzati ed esito li provano solo i test Go.
- `scripts/check` verde; con l'esito del banco 35 voci chiuse, 13 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA.
- **MEDIA:**
  - doppio invio, che con il numero vuoto creava due buste → corretto;
  - esito e link condivisi fra due schede → corretto.
- **BASSA:**
  - numero risolto non mostrato dopo un errore → corretto;
  - voce vuota dei menu non segnata → corretto;
  - legame del link provato solo dal test unitario → test d'integrazione aggiunti;
  - qualunque GET della home consumava il link → corretto con `?esito=`;
  - lo scenario del banco controlla solo GET → dichiarato sopra;
  - commenti superati → corretti;
  - differenze da dichiarare → sopra;
  - titolo della home → annotato per la T4.2.
