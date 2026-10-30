# 016 — Conservazione dell'audit per 5 anni (ADM-17, D-14)

- **Fonte:** `prompt-via-area.md` (Stefano, 30/10/2026), fine T1. D-14 (Stefano, 29/10/2026): lo
  standalone conserva l'audit per sempre, il Go cancella gli eventi più vecchi di 5 anni con un
  lavoro mensile, senza toccare il registro di emergenza.
- **Voci:** ADM-17.
- **Lavoro:** implementazione e correzioni di un sottoagente; review e mutazioni di sottoagenti
  diversi; check, banco e verifica delle mutazioni rilanciati dalla sessione principale.

## Cosa c'è

- `internal/audit/conservazione.go`:
  - **limite:** «adesso − 5 anni» del calendario di Roma (`AddDate(-5,0,0)`); si cancella solo
    `creato_il <` limite, quindi l'evento di esattamente 5 anni resta;
  - **cancellazione:** a lotti da 1000 righe, ciascuno nella sua transazione (circa 3 ms, al
    massimo 27 ms misurati con 300.000 righe);
  - **un giro per mese di calendario a Roma:**
    - lo stato sta in `impostazioni` (`audit.conservazione`) e si controlla dentro la transazione
      di scrittura, quindi due processi non cancellano due volte;
    - un giro interrotto riprende con lo stesso limite;
    - un giro di un mese precedente rimasto a metà si chiude con la sua riga prima del nuovo;
  - **riga del giro**, scritta anche con 0: `audit.retention_applied`, attore `retention`,
    risorsa `audit`, id `events`, dati `{"deleted", "cutoff"}`. Non è mai cancellata dal suo giro;
  - **guardia contro l'orologio avanti:**
    - il limite non va oltre «evento più recente − 5 anni»;
    - se adesso supera l'evento più recente di oltre 31 giorni, il giro si rimanda con un WARN;
    - le righe del giro non contano come evento più recente;
  - **errori e panici:** vanno in log e si riprova al controllo dopo; uno spegnimento in mezzo a
    un giro non è un errore.
- `clock.Aspetta`: l'attesa vera, nel pacchetto dell'orologio come vuole la guardia.
- `serve`:
  - il lavoro parte dopo che le due facce sono su; controlla all'avvio e poi ogni ora;
  - si ferma e viene aspettato prima di `d.Chiudi()`, anche quando una faccia cade.
- Nessuna migrazione.

## Prove

- **TDD:**
  - test rossi con le sole firme;
  - dopo la review, rossi l'orologio avanti, la ripresa nel mese dopo, il panico nella transazione
    e l'attesa del lavoro in `serve`;
  - i test per le mutazioni sono provati con le mutazioni sul repo.
- **Misurato:** tempi dei lotti con 300.000 righe; nessuna instabilità con `-count=10` e
  `-count=20`, anche in parallelo ai test web e con `-race`.
- **Mutazioni:**
  - **primo giro**, su una copia: **60 uccise su 85 che compilano**. Le 19 lacune hanno avuto il
    loro test: la sessione principale ha rifatto le mutazioni su una copia nuova e ne ha uccise 18;
  - **vive**, 6 equivalenti:
    - `AddDate(0,-60,0)` normalizza allo stesso modo;
    - l'ordine dei lotti non cambia il risultato;
    - l'azzeramento dell'esito su errore è già zero;
    - `t.Stop` mancante non si vede;
    - il contesto dei lavori fermato comunque dal `defer`;
  - **dichiarata, 1:** la 83, il database chiuso prima di aspettare il lavoro. Da fuori non si
    vede, perché a contesto finito il lavoro non scrive più. L'ordine dei `defer` in `serve` è
    giusto.
- **Banco verde** con `fab-219`: 12 scenari, 137 passi. Spento, nessun avanzo.
- `scripts/check` verde con l'esito: chiusa **ADM-17** (22 voci chiuse).

## Review indipendente

Nessuna ALTA. Verificato dal revisore:
- confine, fuso e 29 febbraio;
- la riga del giro e le righe nuove non cancellate;
- solo `eventi_audit` toccata;
- la guardia del mese dentro una transazione `immediate`;
- il mese segnato come fatto solo nell'ultimo lotto;
- l'ordine di chiusura;
- nessun `time.Now` fuori da `internal/clock`.

Corretto:
- **MEDIA:** con l'orologio di sistema avanti per errore all'avvio (senza RTC, prima di NTP) il
  primo controllo cancellava eventi recenti: ora c'è la guardia descritta sopra.
- **BASSA:**
  - un giro interrotto e ripreso nel mese dopo perdeva il conteggio;
  - il test di `serve` non provava l'attesa del lavoro;
  - mancava il test del panico dentro la transazione.

Dichiarato: uno stato `audit.conservazione` rovinato ferma la conservazione, con un ERROR ogni ora
e senza recupero automatico (scelta prudente).

## Decisioni (in `docs/domande-aperte.md`, 9)

- Il primo giro dopo un import cancellerebbe in un colpo gli eventi importati più vecchi di 5 anni.
  Proposta: aspettare il primo backup verificato.
- Confine, nome della riga del giro, calendario, lotti, e la guardia dell'orologio con il margine
  di 31 giorni. Effetto da sapere: un negozio fermo più di 31 giorni rimanda il giro finché non
  torna un evento.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `52989d7` (015).
