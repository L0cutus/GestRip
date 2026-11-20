# 106 — Collegamento al modello e modello finto (T4.10, sesta; D-19, D-103)

- **Fonte:** standalone a `go-baseline`: `assist/providers/local_chat.ex` (richiesta, intestazioni, `scrub`,
  `interpret`, 90 s), `assist.ex` (:150-195 `extract_with_model/3`, :485-495 limiti e configurazione, :516-545 `ask` e
  `with_model`, :598 `audit`), `assist/config.ex` (`usable?`), `assist/http.ex` (nessuna intestazione di ritorno),
  `workshop_live.ex` :2437-2463 (`assist_refusal/2`, testi dei rifiuti). Traduttore di produzione
  `~/gestrip-e2b/traduttore.py` (:86-94 richiesta al modello addestrato, :132 `.strip()` del dettato), letto soltanto.
- **Voci:** RIP-H-46 (parte estrazione), RIP-H-47, RIP-H-45 (parte) **parziali** (pagine, impostazioni e trascrizione
  nelle 107-108; nessun segno `// inventario:`). Nessuna D nuova.

## Cosa c'è

- **`internal/assist/modello.go`** (vedi SVILUPPO, «Collegamento al modello e modello finto (slice 106…)»):
  `Configurazione` (`Usabile`), `Collegamento{Config, Attesa}` con `Chiedi` (richiesta compatibile OpenAI) ed `Estrai`
  (rifiuti, domanda nel formato corto, nuovo tentativo, attesa unica di 90 s, audit `assist.extract`), i rifiuti
  `ErrNonConfigurato`, `ErrTestoCorto`, `ErrTestoLungo`, `ErrModello{Motivo}`, `MessaggioDiRifiuto` con i testi dello
  standalone, `Registratore` (l'interfaccia di `*audit.Registro`).
- **`internal/assist/scheda.go`:** `Domanda` toglie gli spazi ai bordi come `.strip()` di Python (correzione portata
  dalla 104/105); `Estrai(chiedi, …)` fa il nuovo tentativo anche dopo un errore di `chiedi` (domanda F), e vale l'esito
  del secondo.
- **`tools/banco/modellofinto`** (pacchetto nuovo) e **`banco modello-finto [-ascolta :8082]`**
  (`tools/banco/modellofinto.go`): server finto con risposte in coda (buona, errore HTTP, lenta, illeggibile, corpo
  grezzo, connessione chiusa), rotte `/_prova/risposte`, `/_prova/richieste`, `/_prova/salute`. Lo usano già i test di
  `assist`; il banco lo collegherà nelle 107-108.
- **SVILUPPO:** sezione nuova; la riga di `Estrai`/`Domanda` della 104 aggiornata.

## Scelte

1. **Un solo messaggio utente, senza messaggio di sistema — correzione del piano (coordinatore, 20/11/2026).** La
   riga 106 diceva «istruzioni come messaggio di sistema, dettato come messaggio utente» (com'è lo standalone), ma con
   il formato corto (D-103) il modello addestrato riceve dal traduttore un solo messaggio utente, «Oggi: …\nDettato: …».
   Il coordinatore ha tenuto la forma del traduttore e ha corretto la riga 106 di `piano-T4.10-bozza.md`: `Estrai`
   manda la sola `Domanda`, conforme al piano corretto. `Chiedi(ctx, istruzioni, testo)` sa mandare anche il messaggio
   di sistema (solo se le istruzioni non sono vuote), per la prova fissa della 107 e il banco della 112.
2. **Nuovo tentativo (domanda F, D-19):** al più due richieste in tutto, dopo **qualunque** fallimento (risposta
   illeggibile, errore di rete, stato HTTP diverso da 200, risposta non OpenAI), dentro **una scadenza unica** di 90 s
   (`context.WithTimeout` su tutta l'estrazione). A tempo finito il secondo tentativo non parte. Anche un 401/4xx si
   ritenta una volta: più semplice, costa una richiesta.
3. **Corpo della richiesta come lo standalone:** `model`, `temperature: 0`, `stream: false`, `messages`. Non copiati dal
   traduttore `max_tokens: 200` e `chat_template_kwargs: {enable_thinking: false}` (vedi Dubbi 2).
4. **Spazi del dettato:** i limiti 10–5000 si contano in grafemi (`String.length/1`) sul dettato senza spazi Unicode ai
   bordi (`String.trim/1`), come lo standalone; la `Domanda` usa poi `.strip()` di Python (in più U+001C-U+001F,
   misurato con Python 3.12 su tutti i caratteri). La proposta (telefono, compro oro) legge il dettato dopo
   `String.trim/1`.
5. **Motivi degli errori** fissi e senza testo: `ai:http:<stato>`, `ai:transport:<genere>` (forma di `inspect/1` di
   hackney, come `genereTrasporto` del canale Telegram: `::timeout`, `::econnrefused`, `::closed`…),
   `ai:risposta non riconosciuta`. Lo standalone scriveva `inspect(reason)` intero dopo aver tolto la chiave; qui il
   testo dell'errore non si usa mai (può portare l'indirizzo, anche con utente e password).
6. **In più rispetto allo standalone, per la riservatezza:** niente redirect (la chiave resta all'indirizzo
   configurato), niente proxy dall'ambiente (come Telegram), risposta letta al più per 1 MiB (oltre = «risposta non
   riconosciuta»), **niente keep-alive** (revisione, MEDIA M-1: con una connessione tenuta aperta, i byte che un server
   manda oltre il Content-Length net/http li scrive nel log standard, «Unsolicited response received on idle HTTP
   channel…», e possono ripetere il dettato). Un chiamante andato via è `ai:transport::closed`, la scadenza passata
   `ai:transport::timeout`.
7. **Lettura della risposta come Jason:** le chiavi contano con le loro maiuscole (`{"Choices": …}` non vale, mentre
   encoding/json in una struct lo accetterebbe): si legge per mappe di `json.RawMessage`; `content` e `model` valgono
   solo se sono testo.
8. **Audit come lo standalone:** `assist.extract`, risorsa `assist`, id `ai`, `{"characters"}` o
   `{"characters", "reason"}`; nessuna riga per non configurato e testo fuori misura; l'esito della scrittura non si
   guarda (come `audit/5`), e la scrittura non usa la scadenza del modello.
9. **Modello finto come pacchetto importabile** (`tools/banco/modellofinto`) invece che solo nel `main` del banco, così
   lo usano i test Go di `assist` e, dalle 107-108, quelli delle pagine.

## Prove

- `scripts/check` verde (due giri, l'ultimo dopo le mutazioni). Copertura invariata: 353 voci, 47 chiuse, 0 rosse.
- **Rosso:** il codice della prima stesura è stato scritto prima dei test, quindi il suo rosso non è misurato (lo
  sostituiscono le mutazioni). Il caso nuovo di M-1 è stato visto rosso senza la correzione (il dettato nel log).
- **Test** (`internal/assist/modello_test.go`, 15 funzioni): richiesta intera (percorso, corpo, Bearer senza spazi,
  indirizzo con «/» e spazi, dettato con spazi ai bordi), istruzioni come sistema in `Chiedi`, chiave assente o di soli
  spazi → nessuna `Authorization`, non configurato (5 forme) → nessuna richiesta né audit, limiti 9/10/5000/5001 anche in
  grafemi e con spazi ai bordi, nuovo tentativo in 14 sequenze (500, 503, 401, 201, 302, illeggibile, rete giù, busta non
  OpenAI, content non testo, e le loro combinazioni), attesa unica (modello lento, 500 e poi lento, illeggibile e poi
  lento, contesto già finito), server spento (`::econnrefused`), indirizzo che non si legge, nome del modello (10 casi),
  risposta al limite di 1 MiB e un byte oltre, niente redirect, genere degli errori di rete, e
  **`TestPrivacyDettatoEChiave`**: nome «Gianfilippo Inventatissimo», telefono «300 987 6543» e chiave
  «chiave-finta-di-prova» non compaiono in errore (`%v`, `%+v`, `%#v`, catena di `Unwrap`), messaggio, estrazione, righe
  di audit e log (`slog` e `log` catturati) con 500 che ripete dettato e chiave, attesa scaduta, illeggibile che li
  ripete, busta rotta che li ripete, rete giù, server spento; il collegamento non scrive log. Nessun test aspetta 90 s
  (attese di 0,2–3 s). In `scheda_test.go` i casi di `Estrai` con l'errore e quelli di `Domanda` con gli spazi.
- **Dopo la revisione:** in `TestPrivacyDettatoEChiave` il caso «byte oltre il Content-Length» (server TCP grezzo che
  ripete dettato e chiave dopo il corpo); `TestCorpoAMeta` (intestazioni e poi corpo a metà: il server si blocca →
  `::timeout`, chiude → `::closed`); `TestInterpretaModello` (18 corpi: maiuscole delle chiavi, `null`, tipi sbagliati);
  contesto annullato → `::closed` e scaduto → `::timeout`, con la riga di audit scritta su un contesto non finito;
  `characters` controllato anche con 5000 «é» precomposte, «e» + accento combinante e emoji con la pelle; Content-Type
  e Accept confrontati; il finto rifiuta con 400 uno «stato» fuori da 200-599 (prima `WriteHeader` andava in panic).
- **Modello finto:** `tools/banco/modellofinto/modellofinto_test.go` (coda, predefinita, richieste registrate, rotte di
  prova, lenta e chiusa, `Servi`) e `TestCmdModelloFinto`.
- **Mutazioni a mano** (script in `to-be-deleted-after-use/slice-106/mutazioni/`): 84 scritte, 1 non applicabile, 3 non
  compilano; **80 compilanti: 75 uccise, 4 equivalenti, 1 sparita con il codice** (il campo `Trasporto`, tolto perché
  inutile). Le 6 vive del primo giro uccise rafforzando i test (nome del modello mandato com'è, limite di 1 MiB in due
  versi, content non testo due volte, corpo del 500 del finto, ultima richiesta del finto). Equivalenti: `Domanda` sul
  dettato già ripulito o no (`.strip()` toglie più di `String.trim/1`), controllo della scadenza prima del secondo
  tentativo (senza, la richiesta fallisce subito con lo stesso motivo), `context.DeadlineExceeded` in `genereRete` (è
  già un `net.Error` con `Timeout()`), il finto che a chiamante andato via scrive lo stesso. Dopo la revisione, sui pezzi
  toccati: 24 compilanti, 21 uccise, 3 equivalenti (controlli su `nil` e `TrimSpace` già coperti da `Unmarshal`), poi
  tolti dal codice.
- **Revisione indipendente (Opus):** 0 ALTE, 1 MEDIA (M-1, keep-alive e log di net/http), 8 BASSE, **tutte chiuse**.
  Mutazioni del revisore: 72 totali, 69 compilanti, 60 uccise, 4 equivalenti (M8, M10, F5, F10), 5 vive (M15, M30, M31,
  M42, M49) poi uccise dai test nuovi.
- Nessuna rete esterna, nessun docker, nessun banco. Implementatore Opus: ~200k token, ~40 minuti con le correzioni della revisione.

## Banco

Nessuno scenario (come da piano): il modello finto è pronto per le 107-108 (`banco modello-finto`).

## Per il manuale

Niente di visibile per ora. Con la 108 le frasi dei rifiuti: «Nessun modello è configurato: non è stato chiesto niente a
nessuno.», «⚠ Testo troppo corto: servono almeno 10 caratteri.», «⚠ Testo troppo lungo: al massimo 5000 caratteri.»,
«⚠ Il modello non ha risposto con una scheda leggibile, riprova.», «⚠ AI temporaneamente non disponibile, riprova fra
qualche minuto.»; l'attesa massima del modello è di 90 secondi in tutto.

## Dubbi

1. ~~Messaggio di sistema~~ **chiuso:** correzione del piano (coordinatore, 20/11/2026), vedi Scelta 1.
2. **`max_tokens` e `enable_thinking`:** il traduttore li manda al modello addestrato; lo standalone no. Con un modello
   che «pensa» (Qwen) dietro `llama-server` senza `enable_thinking: false` le risposte potrebbero allungarsi; un server
   OpenAI vero rifiuterebbe `chat_template_kwargs`. Da decidere con la T5.1 / 111-112.
3. Il nome del modello che risponde viene dal server com'è (regole di `with_model/2`): un server che ripetesse il
   dettato nel campo `model` lo farebbe arrivare alla raccolta dei dettati (113-114). Non è un log né un audit; da
   ricordare lì.
