# 013b — Il messaggio senza indirizzo dice come riavviare (D-32)

- **Fonte:** Stefano, 30/10/2026: «aggiungi al messaggio della pagina utenti che dopo aver scritto
  indirizzo_interno va riavviato gestrip serve, col comando esatto.» Approvati anche D-33 e il
  compromesso dei 180 ms della 013.
- **Cosa c'è:**
  - `apparecchio.ComandoRiavvio` = `sudo systemctl restart gestrip`;
  - il rifiuto della pagina utenti (invito e nuovo link) finisce con «, poi riavvia gestrip serve
    con «sudo systemctl restart gestrip».»;
  - `reset-link` non cambia, perché legge il file a ogni comando;
  - piano §1.9: `gestrip.service`;
  - nome del servizio in `docs/domande-aperte.md` (raccomandazione: tenerlo);
  - inventario: D-33 approvata, D-32 con il riavvio, nota del compromesso su ADM-16.
- **Prove:**
  - test della pagina rosso prima (manca il comando), poi verde;
  - `scripts/check` verde;
  - nessuna review a parte e nessuna mutazione: la modifica è una costante nel testo, e il test
    confronta il testo intero.
- **Commit:** questo, dopo `0cdebb1` (013).
