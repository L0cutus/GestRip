# 006c — x-request-id e registro delle richieste

- **Fonte:** decisione di Stefano del 30/10/2026 (PIANO §8.4): `x-request-id` generato dal Go,
  nei log e nell'intestazione; sul banco si controlla la presenza, non il valore. Misure sullo
  standalone (`grgo-s006b`): 20 caratteri, su ogni risposta compresi `/healthz` e 404, **non** sul
  400; Plug riusa quello del client, il Go no (deciso).
- **Confine:** ogni risposta ha un id nuovo; una riga di registro per richiesta, senza percorsi né
  query; `/healthz` non si registra.
- **Voci:** nessuna voce dell'inventario la nomina; tocca ACC-19 (nessun registro dei percorsi con
  token), che resta aperta finché non ci sono rotte con token.

## Cosa c'è

- `faccia`: `x-request-id` di 20 caratteri (15 byte casuali in base64 per URL), messo prima di
  instradare; `NonValida` (400) lo toglie, come lo standalone.
- Registro (`log/slog` su stderr, journald): `msg=richiesta faccia=… id=… metodo=… rotta=…
  codice=…`. La **rotta è il pattern del mux** (`GET /r/{token}`), quindi dice quale pagina senza
  scrivere il token; `nessuna` per i 404 e i 400. `/healthz` non si registra (`log: false`
  nello standalone).
- `Interna` e `Pubblica` ricevono il registro; `gestrip serve` li crea con `faccia=interna` e
  `faccia=pubblica`.
- Banco: campo `presenza:<intestazione>` (confronta «presente» o «assente»); aggiunto a tutti i
  passi.

## Prove

- TDD: test dell'id (nuovo, 20 caratteri, mai quello del client), del registro (niente token,
  niente query, niente id del client, `/healthz` assente, codice giusto, rotta come pattern) e del
  banco, rossi prima del codice.
- Banco: al primo giro **rosso** su 3 passi (i 400 dello standalone non hanno `x-request-id`):
  corretto, poi verde, 56 passi.
- Mutazioni: 13, tutte uccise dopo un test aggiunto (rotta «nessuna», primo WriteHeader). Tolto un
  `Write` del ResponseWriter avvolto, che le mutazioni hanno mostrato inutile.
- `scripts/check` verde.
- Review indipendente: insieme alla 006b, vedi la sua ricevuta (panic, metodo nel registro, 1xx corretti lì).
