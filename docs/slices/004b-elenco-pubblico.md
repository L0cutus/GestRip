# 004b — Elenco pubblico dell'inventario (risposta A2)

- **Fonte:** risposta di Stefano del 30/10/2026 alla domanda A2 (PIANO §8.3).
- **Confine:** nel repo va solo l'elenco ID, titolo e destino, generato da script con il controllo
  delle parole vietate; l'inventario completo resta privato. La CI fa girare la copertura
  sull'elenco.
- **Voci:** nessuna del prodotto (strumento di sviluppo).

## Cosa c'è

- `tools/copertura/elenco.go`: `scriviElenco` (ordine di ID, rifiuta titoli con tabulazioni o
  parole vietate, rifiuta senza elenco di parole vietate), `leggiElenco` (rifiuta righe con campi
  mancanti, ID non validi, destini ignoti, duplicati, elenco vuoto), `leggiParoleVietate`.
- `go run ./tools/copertura -scrivi-elenco` rigenera `docs/inventario-elenco.tsv` (353 voci).
- Senza `-scrivi-elenco`: se l'inventario privato c'è lo legge e controlla che l'elenco sia
  allineato (altrimenti errore); se manca, e non è stato chiesto con `-inventario`, legge l'elenco.
- `scripts/check` e la CI fanno sempre girare la copertura con `-verifica`.

## Prove

- TDD: 4 test nuovi, rossi (non compilavano) prima del codice.
- Dal vivo: inventario assente → legge l'elenco; `-inventario` esplicito assente → errore; elenco
  modificato a mano → «non è allineato»; elenco delle parole vietate assente → rifiuto.
- Mutazioni: 4 su 4 uccise (minuscole nel confronto, elenco vietato vuoto, titolo vuoto, elenco
  vuoto).
- Le 13 righe con «otto» erano tutte «lotto»: falsi allarmi, nessun dato vero.
- `scripts/check` verde.
