# 070c — Rifiniture dell'email lato cliente: ripiego per ogni link non valido, frasi a email spenta, compro oro (dopo la T4.6)

- **Fonte:** decisioni di Stefano del 12/11/2026 (`docs/SVILUPPO.md`, «Decisioni di Stefano del 12/11/2026»:
  risposte ai Dubbi di 069c, 070 e 070b), via del coordinatore del 12/11. Lo standalone a `go-baseline` =
  `302439d` (`public_repair_controller.ex`: `email_stop/2`, `email_stop_post/2`, `stop_refused/1`;
  `email_notice.ex`: `sheet/1`).
- **Voci:** CLI-41 (disattivazione e ripiego D-02), CLI-42 (riquadro della scheda). Nessuna voce nuova chiusa:
  sono rifiniture di voci già portate.
- **Lavoro:** la sessione principale, review indipendente di un altro agente, mutazioni a mano. Nessuna migrazione.

## Cosa c'è

- **D-02 estesa** (`internal/web/email_cliente.go`): `GET` e `POST /stato/email-disattiva` con un link che non si
  verifica aprono la pagina di ripiego «scrivi la tua email» (200), non più il 410 «Link non valido» dello
  standalone (`stop_refused/1`). Vale per un link inventato, senza `t`, nella forma della vecchia app Flask
  (`?e=` con l'indirizzo in chiaro: la pagina non lo ripete), firmato con una chiave di prima del rinnovo, e per il
  vecchio `Phoenix.Token` come prima. Il ramo `default` di `disattivaEmail` e `disattivaEmailPost` è
  `mostraRipiego`; `stopRifiutato` non c'è più. Una scrittura che non riesce con un link valido resta il 503
  «Disattivazione non riuscita». Il link di conferma non valido resta il 410 (Dubbio 3 della 070).
- **Frasi a email spenta** (testo neutro provvisorio): con `EmailDisponibile` falso (o non leggibile) la pagina di
  ripiego dice «Questo link non è valido o non vale più. Scrivi l'indirizzo a cui ricevi gli avvisi per chiedere di
  disattivarli.» con il pulsante «Invia richiesta», e la risposta del modulo è «Avvisi email» / «Al momento il
  negozio non manda email. Per disattivare gli avvisi o per qualunque dubbio chiedi al negozio.». Con l'email
  accesa le frasi restano quelle della 070, con «Questo link non è valido o non vale più.» al posto di «Questo link
  è di una versione precedente del sistema…» (ora la pagina la apre ogni link non valido). La scelta dipende solo
  dallo stato di tutto il negozio (`emailAccesa`), quindi la risposta resta identica per ogni indirizzo.
- **Compro oro** (`internal/web/email_pronta.go`): la scheda di una busta compro oro non promette «partirà da sola»
  e non dice «non partirà» (`!s.CompraOro` nella condizione della promessa). Lo standalone la promette.
- **Template:** il commento di `email_azione.html` dice i link che aprono il ripiego.

## Prove

- **Rosso prima del codice** (log `prove/rosso.log`, `prove/rosso-compro-oro.log`): `TestEmailLinkRifiutati`
  (410 al posto del ripiego per i quattro link non validi, GET e POST), `TestEmailRipiegoD02` (firma alterata come
  dopo il rinnovo della chiave; frasi a email spenta), `TestAvvisoEmailNellaScheda` (promessa sulla busta compro
  oro, con l'email accesa e spenta). Il primo rosso della scheda era il vincolo `CHECK` dello stato compro oro
  (busta di prova messa in `in_valutazione`); il rosso giusto è stato ripreso togliendo la condizione.
- **Dopo la review:** il 503 con un trigger che fa fallire l'`UPDATE` delle buste; un errore nel leggere se l'email
  è accesa vale come spenta; il pulsante «Mandami il link» solo con l'email accesa; la risposta identica a email
  spenta provata da un altro indirizzo IP, con il lavoro in sottofondo che parte davvero.

## Mutazioni

Della review, su una copia (17): 15 uccise subito. Le due che sopravvivevano, uccise dopo i test nuovi (rifatte
dalla sessione principale):

| # | Mutazione | Esito |
|---|---|---|
| M1 | tolta `!s.CompraOro` | uccisa (`TestAvvisoEmailNellaScheda`) |
| M15 | compro oro: tolta la promessa ma lasciato «non partirà» | uccisa |
| M2, M3 | 410 nel `default` del POST / della GET solo per `ErrLinkEmailVecchio` | uccise |
| M11, M12, M14 | ripiego con 410; condizione della GET invertita; senza `intestazioniPubbliche` | uccise |
| M4, M5, M16 | `emailAccesa` invertita, sempre vera, sempre falsa | uccise |
| M7, M17, M8, M13 | frasi e titoli sempre «acceso» o sempre «spento» | uccise |
| M9 | risposta a email spenta diversa per un indirizzo | uccisa |
| M6 | errore di `EmailDisponibile` letto come «accesa» | **sopravviveva**, uccisa dopo la review |
| M10 | `ErrDisattivazioneNonRiuscita` finito nel ripiego | **sopravviveva**, uccisa dopo la review |

## Banco

`s070c2`, `fab-225`, `-dati sintetici-import`. Righe nuove in `bench/deviazioni.json`: D-02 sui passi 10 e 11 di
`email-cliente` (link inventato, GET e POST: codice 410 → 200, frase dell'esito, «Link non valido»,
`#email-fallback-address`); i due passi confrontano anche `#email-fallback-address` e non hanno più l'attesa del
410. L'attesa del passo 13 (`solo_go`, il POST del ripiego) è ora «Al momento il negozio non manda email.»: il Go del
banco ha l'email spenta. Un primo secondo giro sulla stessa istanza già usata è stato fermato e non conta (i dati
erano già cambiati dal primo giro). Giro valido su un'istanza nuova: **BANCO VERDE**, 64 scenari, 1120 passi, 0
inattese, 0 errori; **BANCO BROWSER VERDE**, 16 scenari, 32 viste, 0 inattese, 0 errori. `banco giu`: nessun avanzo.

## Controlli

`scripts/check` verde, rilanciato dalla sessione principale dopo le correzioni della review; copertura dell'inventario invariata: 353 voci, chiuse 36,
rosse 0.

## Dubbi

1. **Testo provvisorio a email spenta:** «Al momento il negozio non manda email…» è vero (a email spenta il lavoro
   del ripiego non manda niente e non registra niente). Il testo definitivo, come gli altri solo Go, lo prepara il
   coordinatore con il manuale.
2. **Una lettura in più per ogni link non valido** (`CanaleAttivo`), e con il database in errore una riga di errore
   nel registro per richiesta. Trascurabile; da guardare solo se un bot apre link in serie.

## Review

Agente indipendente: nessuna ALTA. MEDIA 1 (pulsante «Mandami il link» a email spenta) e MEDIA 2 (503 senza test)
corrette; BASSE 3 e 4 (errore dello stato, risposta identica solo a limite finito) corrette; BASSA 5 (testo che
diceva più di quello che succede) corretta con il testo nuovo; BASSA 6 (documentazione: `docs/SVILUPPO.md`, commento
di `email_azione.html`) corretta; BASSA 7 è il Dubbio 2. I Dubbi 6 e 9 della ricevuta 070 sono superati da questa
slice.
