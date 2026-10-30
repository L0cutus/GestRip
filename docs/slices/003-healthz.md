# Slice 003 — `/healthz` e `gestrip serve` (T0)

- **Livello:** completo (rotta pubblica, segnale del ritorno indietro automatico).
- **Fonte:** inventario ADM-20; standalone `go-baseline`:
  `lib/gestrip_standalone_web/controllers/health_controller.ex`, `lib/gestrip_standalone/health.ex`,
  `test/gestrip_standalone_web/controllers/health_controller_test.exs`.
- **Confine:**
  - `internal/db` con apertura, WAL, una sola connessione di scrittura, 4 di lettura e la sonda
    `Pronto` (solo `SELECT 1`, al massimo 2 s);
  - `internal/web` con `Salute` e le due facce, che per ora hanno solo `/healthz`;
  - `cmd/gestrip serve` su loopback, porte di sviluppo 47101 e 47102.

  Niente migrazioni, sessioni o altre rotte: arrivano in T1.

## Ricevuta di completamento

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Regole copiate dai test dello standalone:**
  - 200 con il corpo esatto «ok», oppure 503 «unavailable»;
  - `content-type: text/plain; charset=utf-8` e `cache-control: no-store`;
  - nessun `set-cookie` e nessun `location`;
  - la query string viene ignorata;
  - con le connessioni esaurite risponde 503 entro 3 s (budget di 2 s);
  - nessun dato del negozio e nessuna scrittura.
- **TDD:** test scritti prima (`undefined: Apri`, `TempoMassimoSonda`). `TestServeRispondeSuDueFacce`
  era rosso per un controllo sbagliato sugli indirizzi uguali con la porta 0; ora quel caso lo
  rifiuta `net.Listen`, come prova `TestServeRifiutaLeFacceSullaStessaPorta`.
- **Mutazioni:** 10 provate, 9 uccise, ripristino copiando il file buono. Sopravvive
  `return uno == 1` → `true`, che è equivalente perché `SELECT 1` dà sempre 1.
- **Copertura:** ADM-20 «con test» (i test portano il segno; il test di scatola chiusa arriva con
  la 004).
- **Debiti verso T1:**
  - metodi diversi da GET e HEAD: il mux Go risponde 405, lo standalone 404. Va allineato con
    la pagina 404 comune;
  - log di accesso e intestazioni di sicurezza comuni.
- **Fatto di passaggio:** `scripts/check` ora controlla con gofmt anche i file `.go` non ancora
  aggiunti a git.
