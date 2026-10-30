# 009b — Limitatore come la slice 219 dello standalone (T1)

- **Fonte:** Stefano, 30/10/2026: i due buchi trovati dalla review della 009 si correggono prima
  nello standalone (slice 219, `6eb93ed`, niente D-32); poi si riallineano CLI-21 e ACC-20, si
  sposta `go-baseline` e il Go copia: prenotazione atomica prima della verifica, rotta
  riconosciuta da quella instradata. In più: nel Go anche il filtro che toglie i token dal log
  deve leggere il percorso decodificato.
- Standalone letto a `6eb93ed`: `attempt_limiter.ex` (`reserve/3`, `failed/4`, `release/3`),
  `session_controller.ex`, `public_attempts.ex`, i test nuovi di `attempt_limiter_test.exs` e
  `attempt_limits_bypass_test.exs`, la matrice di parità e la scheda della 219.
- **Voci:** CLI-21, ACC-20 (restano parziali per audit, reset e Funnel, come dopo la 009).
- **Livello:** completo (sicurezza).

## Cosa c'è

- `internal/ratelimit`: `Prenota` prende un posto prima della verifica, sotto lo stesso lucchetto
  che conta: ogni chiave ha tanti posti quanto il limite, presi dai falliti nella finestra e dalle
  verifiche in corso. Posti pieni di verifiche in corso: attesa di un secondo, non un blocco.
  `Fallito(…, biglietto)` trasforma il posto, `Rilascia` lo restituisce; per il blocco contano solo
  i falliti. Un posto mai sistemato esce con la finestra.
- Login: si prenotano account e indirizzo; se uno è rifiutato l'altro si restituisce e la risposta
  è il 429 con l'attesa più lunga («riprova fra 1 minuti» a posti pieni); sbagliato → due falliti;
  riuscito → posto dell'indirizzo restituito, account azzerato. Errore del database: i posti
  restano occupati fino a fine finestra, come lo standalone (eccezione nel controller).
- Faccia pubblica: prenotazione prima della rotta; 404 → fallito, altro → restituito, anche
  quando il gestore va in panic (come `before_send` sulla pagina d'errore). La famiglia si legge
  dai segmenti decodificati, cioè dalla rotta instradata (già dalla 009).
- Registro: nel Go non c'è un filtro per prefissi da aggirare, perché la riga di registro scrive
  solo il pattern della rotta, mai il percorso: test `TestRegistroSenzaPercorsoAncheInCodice` con
  percorsi in codice (`/%72/…`, `%2F`, query). La review ha controllato tutto il codice di
  produzione: nessun'altra scrittura contiene il percorso.
- Banco: immagine `fab-219` (verificata: il modulo del limitatore contiene `reserve`, quello di
  `fab-218` no).

## Prove

- TDD: 9 test rossi prima (5 del limitatore, 4 web: login simultanei per account e per indirizzo,
  posti pieni, `/stato` simultanei); dopo la review 2 test in più (`TestBigliettoGiaRestituito`,
  `TestFallitoTrasformaIlPosto`) e il caso di `/stato` in codice reso non vacuo.
- `-race` pulito. Banco **verde**, 5 scenari, 84 passi, con `fab-219`; spento.
- Mutazioni: **17 uccise su 17 che compilano** (prenotazione senza posto, posti in corso non
  contati, fallito che lascia il posto, blocco su tutti i posti, rilascio vuoto o di un posto
  qualunque, attesa lunga, biglietti uguali, vecchio flusso del login, rifiuto che non restituisce
  l'altro posto, login riuscito che tiene il posto, fallito senza biglietto, risposta buona o panic
  che tengono il posto, segmenti grezzi, `/st%61to` non decodificato, pubblico senza prenotazione).
- `scripts/check` verde con l'esito del banco.

## Review indipendente

Nessun problema alto. Misurato dal revisore: 200 login simultanei misti → 20 password sbagliate
verificate; 300 `/stato` simultanei misti → 8 codici sbagliati verificati. Corretto: il caso
`/st%61to` del test era vacuo; il rilascio sull'errore del database non era coperto da test ed era
diverso dallo standalone (ora copiato); `Fallito` che lascia il posto era preso solo dai test web.
Dichiarato: una rotta pubblica che scrive il 404 e poi va in panic restituisce il posto (nello
standalone conterebbe; oggi nessuna rotta lo fa); per le future rotte pubbliche POST il corpo si
legge dopo la prenotazione (nello standalone prima: `Plug.Parsers`), quindi un client lento tiene
un posto durante la lettura — da guardare quando arriveranno.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `8e0bec4` (009).
