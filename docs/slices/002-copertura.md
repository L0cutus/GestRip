# Slice 002 — Copertura dell'inventario (T0)

- **Livello:** completo nella forma (TDD e mutazioni), anche se è uno strumento: decide quando la v1
  si può rilasciare.
- **Fonte:** PIANO §5.
- **Confine:** `tools/copertura` legge l'inventario, trova i segni `// inventario: ID` nei test
  Go, incrocia con `go test -json`, stampa il riepilogo e blocca con `-verifica` o `-rilascio`.
  Gli scenari del banco arrivano con la slice 004.

## Ricevuta di completamento

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **TDD:** 7 test scritti prima del codice, rossi alla compilazione (`undefined: leggiInventario`).
  Al primo giro verde ne sono rimasti rossi 3, per due ragioni:
  - un segno appeso nell'esempio, che il codice ora rifiuta;
  - la riga del segno.

  Poi è stato aggiunto un test che controlla che i segni dentro le stringhe vengano ignorati e
  che quelli appesi altrove vengano rifiutati. Da qui la lettura con `go/parser` invece delle
  espressioni regolari.
- **Mutazioni:** 9 provate, 8 uccise.
  - Al primo giro è sopravvissuto anche «destino non riconosciuto»: il test ora controlla il
    messaggio, e la mutazione è stata uccisa al secondo giro.
  - Sopravvive il filtro dei sotto-test in `leggiRisultati`, ed è equivalente: se un sotto-test
    è rosso, è rosso anche il test che lo contiene, e lo stato della voce non cambia.
  - Il file buono è stato ripristinato copiandolo, non con `git checkout`.
- **Prova sull'inventario vero:** 353 voci, 340 da coprire (317 v1 e 23 sostituite), 13 fuori v1;
  coincide con il riepilogo ricalcolato il 30/10/2026.
- **Debiti:** gli scenari del banco (JSON con il campo `inventario`) si aggiungono alla slice 004.
