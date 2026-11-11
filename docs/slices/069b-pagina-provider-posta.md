# 069b — Pagina «Provider di invio» (posta), accensione D-03 e revisione D-27 (T4.6, quinta slice)

- **Fonte:** via dell'Orchestratore dell'11/11/2026 (via di Stefano del 09/11 per la T4.6), ripresa
  `2026-11-11-fine-069.md` (seconda parte della 069). Lo standalone a `go-baseline` = `302439d`
  (`providers_live.ex`, `Smtp.changeset/3`, `config.ex`, `outgoing.ex`).
- **Voci:** ADM-09 e CLI-52 (parte email), D-03 (accensione dopo l'import) e D-27 (email importate in coda)
  per l'email. **Parziali:** la parte Telegram di CLI-52 e della D-03 (T4.7).
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna migrazione.

## Cosa c'è

- **Dominio** (`internal/uscita/pagina_smtp.go`): `SalvaSMTP` con le regole di `Config.put/3` e
  `Smtp.changeset/3` (validazione solo dei campi cambiati, porta come `Integer.parse`, lunghezze in
  grafemi, credenziali sui valori finali, messaggi e ordine degli errori dello standalone; casella della
  password vuota = la tiene, la spunta toglie utente e password); audit `outgoing.provider_configured` con
  i soli nomi delle chiavi e `configured` dalla `Usabile` del Go (con la seconda serratura della 069).
  `PaginaSMTP` (stato «Configurato», coda: `list_recent(20)` e conteggi, ore sul fuso del negozio).
  `AccodaProva`: messaggio di prova al mittente, testo fisso, solo con l'email accesa.
- **D-03 e D-27** (`ImpostaEmail`, `ScartaInSospeso`, `MandaInSospeso`): l'email si accende solo con la
  posta configurata e **nessuna email in coda**; le email in coda si guardano una per una (destinatario,
  oggetto, testo con i link firmati nascosti) e si scartano (con conferma: la riga si cancella, audit
  `outgoing.queued_discarded`) o si mandano subito («Manda adesso»: un invio dentro la richiesta, conta come
  tentativo, mai due volte). C'è anche «Spegni», che lascia le righe in coda. Accensione, scarta, manda e il
  lavoro di ogni riga del giro stanno sotto lo stesso lucchetto (`aMano`); «Manda adesso» non dipende dalla
  richiesta (`context.WithoutCancel`), solo dai tempi massimi dell'SMTP; un audit non scritto dopo un invio
  riuscito va nel registro (solo id e tipo dell'errore) e la pagina mostra l'esito vero.
- **Pagine** (`internal/web/provider.go`, `pagine/provider.html`): scheda «Provider» nelle Impostazioni;
  `GET /settings/providers`, `POST /settings/providers/smtp|prova|accensione`,
  `POST /settings/providers/sospese/{id}/scarta|manda`, solo titolare, CSRF; la password mai nella pagina,
  neanche dopo un rifiuto. `gestrip serve` crea una coda sola per il lavoro in sottofondo e per la pagina.
- **Banco:** `bench/scenari/provider.json`, `provider-dipendente.json`, `bench/browser/provider-posta.json`
  (`providerPosta` in `banco.mjs`): solo server `.invalid`, nessuna prova con la posta configurata, quindi
  nessuna connessione verso fuori; la configurazione vuota rimessa com'era.

## Prove

`uscita`: `pagina_smtp_test.go` (12: regole con 20 rifiuti, solo i cambiati, password, audit, pagina,
accensione con la revisione, «Manda» contro il server SMTP finto accettata e rifiutata, due «Manda» insieme,
configurazione nuova senza riavvio), `correzioni_069b_test.go` (4: «Spegni» durante il giro, richiesta
chiusa, audit non scritto, `configured:false` e a capo finale). `web`: `provider_test.go` (10). Rosso preso
prima del codice (20 test con gli stub; per la review 3 test rossi).

## Mutazioni

Implementazione: 41 (34 più 7 per la review), tutte compilano, tutte uccise. Review: 22, 19 uccise, 1
equivalente (la password non arriva mai allo stampo), 2 sopravvissute (M20, M21) → test nuovo, ora uccise.

## Banco

`s069b`, `fab-225`, `-dati sintetici-import`, al primo giro: **BANCO VERDE**, 63 scenari, 1103 passi, 0
inattese; **BANCO BROWSER VERDE**, 16 scenari, 32 viste, 0 inattese; `provider-posta` con osservazioni
identiche (rifiuti del changeset, salvataggio, cancellazione delle credenziali, prova senza
configurazione, conteggi del registro). `banco giu`. Le correzioni della review toccano solo la concorrenza
e l'audit fallito, che il banco non usa: giro non rifatto. D-03 e D-27 sono solo del Go: le provano i test.

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- In «Impostazioni → Provider» il titolare scrive i dati del server di posta e vede «Configurato».
- L'invio delle email si accende con «Accendi»; se ci sono email rimaste in coda (per esempio arrivate
  dallo standalone), prima si guardano una per una e si mandano o si scartano.
- «Invia prova» manda un messaggio all'indirizzo del mittente.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Blocco dell'accensione con qualunque email in coda,** non solo le importate (PIANO): l'import non lascia
   un segno per riga e confrontare le date dipende dall'orologio della macchina dello standalone. Effetto:
   dopo uno «Spegni» con email in attesa di un nuovo tentativo bisogna guardarle prima di riaccendere.
   **Raccomandazione:** tenerla così; se dà fastidio, un segno sulle righe all'import (migrazione) in una
   slice futura.
2. **Prova con l'invio spento:** oggi non si può (testo del Go: «L'invio delle email è spento: accendilo qui
   sotto, poi manda la prova. Niente è stato accodato.»), quindi con email importate in coda il primo
   «Manda adesso» fa da prova delle credenziali. **Raccomandazione (review e implementatore):** una prova
   «subito», senza coda, al mittente stesso anche a email spenta.
3. **Numero di busta** assente nelle email da guardare. **Raccomandazione:** mostrarlo (il dato c'è già).
4. **«Acceso» con una configurazione diventata inutilizzabile:** la pagina non dice perché non parte niente.
   **Raccomandazione:** una riga «acceso ma non configurato».
5. **«Scarta» cancella la riga** (resta l'audit), come «Togli dalla lista». **Raccomandazione:** tenerlo.
6. **Telegram** assente dalla pagina (nello standalone c'è il riquadro, ADM-10): arriva con la T4.7.
7. **Stranezze dello standalone copiate:** un a capo finale nel mittente o nel nome accettato, dominio di una
   lettera accentata; innocue grazie alla seconda serratura e alla codifica del nome. **Raccomandazione:**
   lasciarle.
8. I testi solo del Go (accensione, revisione, «Manda adesso», «Spegni») li ha scritti l'implementatore: da
   rivedere con il manuale.

## Review

Revisore indipendente, 11/11/2026 (pc-fisso, slice non committata). Nessuna ALTA.

- **MEDIA, corretto:** «Spegni» durante un invio del giro, poi «Manda adesso» = la stessa email due volte (il
  giro non prendeva il lucchetto).
- **MEDIA, corretto:** richiesta chiusa durante «Manda adesso» = riga rimasta in coda e rimandata.
- **BASSA, corretto:** audit fallito dopo un invio riuscito mostrava un errore d'invio.
- **BASSA, Dubbio 4:** «Acceso» senza configurazione usabile. **BASSA, test aggiunti:** M20, M21.
- **Domande di prodotto:** Dubbi 1, 2, 3, 5.
- **Verificato senza rilievi:** password mai nella pagina, nel registro, nell'audit o negli errori; CSRF e solo
  titolare sulle 6 rotte; id forgiati; escape; la prova va solo al mittente; nessun percorso accende l'email
  con righe in coda; due «Manda» insieme = un invio; fedeltà a `Config.put/3` e `Smtp.changeset/3`; banco
  senza connessioni verso fuori. `scripts/check` verde.
