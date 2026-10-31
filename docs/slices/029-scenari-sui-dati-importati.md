# 029 — Gli scenari del banco anche sui dati importati (T3b)

- **Fonte:** T3 del PIANO: il banco gira su standalone e Go con gli stessi dati sintetici della
  catena. Sulla catena restava una sola differenza inattesa, `utenti[2]`.
- **Voci:** nessuna (T3, banco).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un sottoagente
  diverso; la sessione principale ha rilanciato `scripts/check` e i due giri del banco.

## Cosa c'era

`utenti[2]` si aspettava «Invito da consegnare» assente; coi dati importati lo mostrano **tutte e
due** le parti. Il motivo, letto nello standalone (`accounts.ex`: `pending: is_nil(password_hash)`)
e misurato sul database Go: i due account attivi della cattura Flask sintetica arrivano senza
password. Non è l'invito importato (quello del dipendente seminato, già accettato). Nessuna
differenza fra standalone e Go: era lo scenario scritto solo per il database vuoto.

## Cosa c'è

- `banco esegui` legge la modalità dei dati dal `runtime.json` dell'istanza (`preparaEsegui`) e la
  passa a `confronta -dati`; mancante o sconosciuta ferma il banco, anche dentro
  `eseguiScenariCon`.
- Scenari: `atteso_dati` per modalità (solo `sintetici-import`, mai vuoto, campi validati) e il campo
  `conteggio:<frase>`. Frasi con caratteri che l'HTML scappa rifiutate (le virgolette solo in un
  attributo intero, come negli scenari dei gusci).
- `utenti[2]`: su database vuoto `testo` assente e `conteggio` 0; sui dati importati presente e 2.
- Gli altri scenari non guardano i dati importati (guscio, codici, intestazioni, righe d'audit del
  banco stesso): si allargheranno con le funzioni della T4.

## Prove

- **TDD:** rosso misurato prima del codice (compilazione, poi 7 test con gli stub; per le correzioni
  3 test con gli stub e 10 mutazioni).
- **Mutazioni:** 33 nella review (24 uccise, 8 vive, 1 non compila) + 10 dopo le correzioni: tutte
  le vive con un test mancante ora uccise; restano 2 vive equivalenti (la chiave vuota rifiutata in
  lettura, un `+0*len`).
- **Dal vivo (`fab-219`):** con `-dati sintetici-import` import riconciliato (9 avvisi) e **banco
  verde**, 13 scenari, 156 passi, 0 differenze; senza opzione **verde**, 13 scenari, 156 passi.
- `scripts/check` verde; con l'esito del banco 31 voci chiuse. Nessun avanzo.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** il passaggio della modalità da `runtime.json` alle attese non era testato (estratto
  `preparaEsegui`, test di `cmdConfronta` contro due server TLS di prova); nessun test di un'attesa
  rispettata da una parte sola (aggiunto, nei due versi, con `atteso` e `atteso_dati`).
- **BASSA:** maiuscole nel conteggio, conteggi non canonici, frasi con caratteri scappati,
  validazione dei dati in `eseguiScenariCon`, documentazione in `docs/slices/004-banco.md`.
- **Lasciati:** l'esito del banco non dice con quali dati è stato fatto (la copertura usa l'esito
  del banco senza opzione); il 2 è scritto nello scenario, non ricavato a ogni giro.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** `5968142`, dopo `da37c78`.
