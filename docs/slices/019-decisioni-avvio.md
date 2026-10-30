# 019 — Decisioni di Stefano sull'avvio: rifiuto senza l'opzione, stop pulito, ascolto solo locale

- **Fonte:** risposte di Stefano alle domande aperte, 30/10/2026 (note «Deciso» sotto OPS-12 e
  ADM-20 nell'inventario).
- **Voci:** nessuna nuova chiusa (OPS-12 e ADM-20 restano chiuse e allineate alle decisioni).
- **Lavoro:** implementazione e correzioni di un sottoagente; review di un sottoagente diverso; check,
  banco e il primo giro di mutazioni rifatti dalla sessione principale.

## Cosa c'è

- **Volume cifrato:** il rifiuto non nomina più `-senza-volume-cifrato`, e la riga d'uso nemmeno.
  L'opzione resta, con il suo avviso; è descritta in `docs/SVILUPPO.md`.
- **Stop durante l'avvio:** se SIGTERM o SIGINT arrivano mentre `serve` si avvia, l'uscita è 0 con
  «gestrip: fermato durante l'avvio».
  - Conta come stop solo se l'unica causa dell'errore è il contesto, seguendo tutti i rami degli
    errori avvolti (`soloDa`).
  - Ogni errore vero, anche insieme allo stop, esce con 1.
  - In `internal/db/migra.go` gli errori che possono venire dal contesto ora sono avvolti con `%w`.
- **Ascolto solo locale:** `serve` rifiuta un indirizzo d'ascolto non loopback su tutte e due le
  facce senza `-ascolto-non-locale`.
  - Il controllo si fa prima di aprire qualsiasi cosa e accetta solo IP scritti in 127.0.0.0/8 o
    `::1`, oppure `localhost` esatto, sempre con la porta.
  - Dopo l'apertura si controlla anche dove ascolta davvero la faccia.
  - Il banco passa l'opzione.

## Prove

- **TDD:** rossi con le sole firme; dopo la review, rossi lo stop durante la verifica del backup e
  lo stop accanto a un errore vero.
- **Mutazioni:**
  - primo giro: 15 su 15 uccise, rifatte dalla sessione principale su una copia nuova;
  - dopo la review, giro del sottoagente: 6 su 7 uccise. Viva il `%v` sul conteggio della copia:
    non c'è un modo deterministico di far arrivare lo stop proprio lì, e la riga usa comunque
    `%w`.
- **Review:** il revisore ha provato sul binario vero oltre 200 SIGTERM a ritardi diversi, senza
  nessuna uscita 1. Ha provato anche le forme d'IP strane (`127.1`, decimale, esadecimale, ottale,
  zone), tutte rifiutate.
- **Banco verde** con `fab-219`. Il log del container mostra le due ATTENZIONE e «in ascolto».
  Spento, nessun avanzo.
- `scripts/check` verde, `-count=10` su `./cmd/gestrip/` e `./internal/db/`.

## Review indipendente

Nessuna ALTA né MEDIA. Corrette le BASSA:
- uno stop durante la verifica del backup dava uscita 1 (`%v`);
- uno stop accanto alla copia «da togliere a mano» dava uscita 0;
- mancava il test del controllo dopo l'apertura sulla faccia interna.

Dichiarato in `docs/SVILUPPO.md`:
- un SIGTERM nei primi 70 ms circa, prima di `signal.NotifyContext`, fa morire il processo con 143,
  che per systemd è una fine pulita;
- le due opzioni compaiono in `gestrip serve -h` e negli errori delle opzioni (è voluto, il repo è
  pubblico);
- se `localhost` risolve fuori da loopback, il rifiuto arriva dopo che il database è stato
  creato. È un caso remoto.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `39f1487` (018).
