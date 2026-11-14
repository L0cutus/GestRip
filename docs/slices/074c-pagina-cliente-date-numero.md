# 074c — Pagina del cliente: date gg/mm/aaaa (D-84) e numero senza «N.» (D-82 estesa)

- **Fonte:** decisioni di Stefano del 13/11/2026 (`docs/SVILUPPO.md`, «Decisioni di Stefano del 13/11/2026»,
  risposte ai Dubbi ancora aperti di 055–063: Dubbi 1 e 2 della 061), via del coordinatore. Lo standalone a
  `go-baseline` = `302439d` (`public_repair_html/show.html.heex`: `{@repair.bag_number}` nel riquadro scuro,
  `{@repair.accepted_on}`, `delivery/1` con `Date.to_iso8601/1`, `{@repair.ready_on}`, `{@repair.delivered_on}`;
  `stamp/1` e `thread_stamp/1` già gg/mm/aaaa hh:mm).
- **Voci:** CLI-01 (pagina `/r/`), CLI-16 (pagina storica `/stato`), CLI-27 (anteprima del negozio). Nessuna voce
  nuova chiusa: sono differenze volute su voci già portate.
- **Lavoro:** sottoagente d'implementazione, mutazioni a mano. Nessuna migrazione.

## Cosa c'è

- **D-84, date gg/mm/aaaa** (`internal/buste/pagina_cliente.go`, `paginaDove`): «Accettata», «Consegna prevista»
  (solo quando è una data), «Pronta dal» e «Consegnata» passano da `dataGiornoMeseAnno` (la stessa funzione della
  D-80). Un testo scritto al posto della data di consegna («fine mese») resta com'è. Vale per `/r/<token>`,
  `/stato/<busta>/<anno>` e l'anteprima del negozio, che leggono tutte da `paginaDove`.
- **Altre date che il cliente vede:** le ore della conversazione (anche quelle che lo script ridisegna dal poll) e la
  data della risposta al preventivo sul link `/r/` sono già `clock.Timbro`, gg/mm/aaaa hh:mm come lo standalone. I
  riquadri email e Telegram e «Tempo indicativo» non hanno date.
- **Data della risposta al preventivo nell'anteprima (differenza dallo standalone che c'era già, trovata dalla
  review):** l'anteprima del negozio mostrava «il  · importo: 80,00», senza la data, perché solo `paginaCliente`
  impostava il campo `RispostaIl`; lo standalone mostra la data. Ora `RispostaIl` è un metodo di
  `datiPaginaCliente` (`internal/web/pagina_cliente.go`), con la stessa condizione (risposta approvata o rifiutata):
  vale per il link, la pagina storica (fase non applicabile: vuoto, come prima) e l'anteprima. La correzione
  proposta (spostare il calcolo in `mostraPaginaCliente`) non bastava: l'anteprima non passa da lì
  (`anteprima_cliente.go` chiama `mostraPubblica` direttamente).
- **D-82 estesa, numero senza «N.»:** `PaginaCliente.Numero` viene da `buste.NumeroNeiTesti` (al posto di
  `NumeroVisibile`): «0045/26» con l'etichetta RIP, «45/26» senza, anche nel riquadro scuro. Le pagine interne del
  negozio restano con «N. 45/26» (D-64): un test lo controlla sulla scheda.
- **Commenti:** campo `Numero` e date di `PaginaCliente`, `NumeroNeiTesti`, il commento di testa di
  `pagina_cliente.html`.
- **Banco:** normalizzatore `D-84` in `tools/banco/confronto.go` (`reD84`: una data AAAA-MM-GG da sola nel `<dd>`
  di quelle quattro righe), messo con `"normalizza": "D-84"` sui 15 passi che confrontano `.space-y-7@1/dl`:
  `pagina-cliente` 0-2, `anteprima-cliente` 3-8, `anteprima-cliente-dipendente` 3-8. In `bench/deviazioni.json`
  tolte le 11 righe D-64 di `.font-mono@1` per le buste senza etichetta (Go «N. 17/26», standalone «17/26»): ora le
  due app scrivono lo stesso numero. Le 10 righe D-64 delle buste con l'etichetta restano, con il motivo aggiornato.
- **Documenti:** `docs/SVILUPPO.md` (riga D-84 nella sezione del 13/11, «fatta con la 074c» sulle due decisioni e
  sull'ordine, D-82 estesa, D-64 «Numero visibile»), `docs/manuale/da-scrivere.md`, `docs/manuale/99-glossario.md`.

## Scelte

- **Numero della D nuova: D-84.** Il comando suggerito dà come più alta la D-99, ma la D-99 non è una deviazione:
  è l'ID inventato che i test del banco usano per una deviazione **non approvata** o **senza normalizzatore**
  (`tools/banco/date_test.go`, `esegui_test.go`, `browser_test.go`, `home_test.go`; `docs/slices/004-banco.md`).
  Usarla romperebbe quei test, e la successiva (D-100) lascerebbe un buco di 16 numeri. L'ultima D vera è la D-83
  (069c): la prima libera è la **D-84**.
- **Numero senza «N.» come estensione della D-82, non D nuova.** È la stessa regola («il numero che legge il
  cliente è il numero visibile senza "N."»), con la stessa funzione `NumeroNeiTesti`; Stefano l'ha chiesta «come nei
  messaggi della D-82». La D-82 diceva «Le pagine (la scheda, `/r/`, le stampe) non cambiano»: ora la riga dice che
  dal 13/11 la pagina del cliente cambia e la scheda e le stampe no. Una D nuova avrebbe due righe da tenere
  allineate sulla stessa regola. Nel banco non serve niente: senza etichetta il Go scrive ora come lo standalone.
- **Date con un normalizzatore, non con righe**, come la D-80: la differenza tocca ogni busta di ogni pagina del
  cliente. Un campo che il Go dà ancora AAAA-MM-GG (uguale al grezzo dello standalone) è inatteso («D-84 non
  applicata dal Go»).
- **Nessun nome di funzione nuovo:** `NumeroNeiTesti` ora serve anche alla pagina; il nome resta per non toccare
  i chiamanti della D-82, il commento dice il nuovo uso.

## Prove

- **Rosso prima del codice** (log `prove/rosso.log`): `TestPaginaClienteCosaMostra` (date attese gg/mm/aaaa, numero
  senza «N.»), `TestPaginaClienteDateGiornoMeseAnno` (nuovo: quattro date, consegna scritta che contiene una data,
  numero), `TestPaginaStoricaContenuto` («46/26») in `internal/buste`; `TestPaginaClienteDateENumero` (nuovo: `/r/`,
  anteprima, consegna scritta, etichetta, scheda interna con «N.»), `TestPaginaStoricaDateENumero` (nuovo),
  `TestPaginaClienteMostra`, `TestPaginaStoricaHTTP` in `internal/web`. Tutti rossi sull'asserzione (date AAAA-MM-GG,
  «N. 1/26»), poi verdi (`prove/verde.log`).
- **Dopo la review** (log `prove/rosso-review.log`): `TestPaginaClienteDataRisposta` (nuovo: «il 05/11/2026 10:00»
  sul link e nell'anteprima, per la conferma e per il rifiuto; nessun attributo dell'`<article>` che cominci con
  una data AAAA-MM-GG, tranne `data-last-ts`) rosso sull'anteprima prima della correzione, poi verde. Le prove
  nuove di R7 (testo di consegna «2026-03-01» che resta com'è, in `TestPaginaClienteDateGiornoMeseAnno`), R10
  (`dd("Consegnata il", …)` che resta com'è, in `TestNormalizzaD84`) e R14 (l'attributo) passavano già sul codice:
  il loro rosso è sulla copia con la mutazione applicata (tabella della Review).
- **Banco, rosso di comportamento** (`prove/rosso-banco.log`): `TestNormalizzaD84` prima con un normalizzatore
  vuoto (le quattro date non cambiano, «D-84 senza normalizzatore»), poi verde. Il test vuole anche che restino
  com'erano un testo con una data dentro, «Tempo indicativo», una data con una cifra in più, una data fuori da un
  `<dd>` e `data-last-ts`.

## Mutazioni

13 mutazioni a mano, una alla volta su una copia del repo (`to-be-deleted-after-use/slice-074c/mutazioni/`, script
`muta.py`, log per mutazione, `giro.log`; copia cancellata a fine giro). **13 prese su 13**, tutte su un'asserzione
(nessuna per errore di compilazione):

| # | Mutazione | Presa da |
|---|---|---|
| M1 | numero con `NumeroVisibile` («N. 45/26») | `TestPaginaClienteCosaMostra` |
| M2 | «Accettata» lasciata AAAA-MM-GG | `TestPaginaClienteCosaMostra` |
| M3 | solo «Pronta dal» convertita | `TestPaginaClienteDateGiornoMeseAnno` |
| M4 | solo «Consegnata» convertita | `TestPaginaClienteCosaMostra` |
| M5 | «Consegna prevista» lasciata AAAA-MM-GG | `TestPaginaClienteCosaMostra` |
| M6 | la data vince sul testo scritto | `TestPaginaClienteCosaMostra` |
| M7 | mese e giorno scambiati in `dataGiornoMeseAnno` | `TestPaginaClienteCosaMostra` |
| M8 | «N.» davanti al numero dell'etichetta | `TestPaginaClienteCosaMostra` |
| M9 | normalizzatore che scrive AAAA/MM/GG | `TestNormalizzaD84` |
| M10 | normalizzatore senza «Consegnata» | `TestNormalizzaD84` |
| M11 | normalizzatore senza `</dd>` dopo la data | `TestNormalizzaD84` |
| M12 | `D-84` tolta da `normalizzatori` | `TestScenario…SuiDueDati` (scenari con `normalizza` sconosciuto) |
| M13 | normalizzatore su ogni `<dt>` | `TestNormalizzaD84` |

## Banco

**Fatto il 14/11** dalla sessione principale, su un'istanza nuova (`s074cp`), dopo il «memoria libera» del
coordinatore (21 GB disponibili all'inizio, 12 alla fine; mai sotto i 10). Prima, con 4 GB, il banco era stato rinviato
senza giri né attese sulla memoria.

- HTTP: scenari 65, passi 1137, differenze attese 1548, inattese 0, errori 0, non provati 0 — **BANCO VERDE**.
- Browser: scenari 17, viste 34, differenze attese 30, inattese 0, da decidere 0, errori 0 — **BANCO BROWSER VERDE**.
- Le 15 righe delle date sono «ATTESA (D-84, normalizzata)», per esempio `pagina-cliente[1]`: standalone
  «Accettata 2026-03-02 Pronta dal 2026-11-14 Consegnata 2026-11-14», Go «Accettata 02/03/2026 Pronta dal 14/11/2026
  Consegnata 14/11/2026». Istanza tolta, nessun avanzo.

`banco esegui` controlla che ogni D dei `normalizza` sia approvata nell'inventario privato
(`~/Condivisi/GestRip-Go/inventario-funzioni.md`): **le righe D-81…D-84 ci sono ora**, le ha aggiunte il coordinatore
dopo la review, e la D-82 porta l'estensione della 074c (CLI-01, CLI-16, CLI-27).

Atteso al giro (confermato): le differenze delle date sui 15 passi diventano «ATTESA (D-84, normalizzata)»; i `.font-mono@1`
delle buste senza etichetta (pagina-cliente 2; pagina-storica 0, 1, 9, 11; anteprima 4-6 nelle due varianti) uguali
alle due app; le righe D-64 delle buste con l'etichetta come prima.

## Controlli

`scripts/check` verde dopo la review (`prove/check3.log`, «uscita 0»: voci 353: chiuse 42, rosse 0, parziali 129, aperte 171, fuori v1 11). Il secondo giro
(`prove/check2.log`) era rosso solo per `gofmt` su `internal/web/pagina_cliente.go` (allineamento della struct dopo aver
tolto il campo), corretto. Primo giro, prima della review: verde (`prove/check1.log`, «uscita 0»; copertura: voci 353, chiuse 42, rosse 0, parziali 129, aperte
171, fuori v1 11).

## Per il manuale

- Pagina del cliente: numero senza «N.» («45/26», «0045/26») e date come sulla ricevuta («05/11/2026»); una consegna
  scritta a parole resta com'è. Messo in `docs/manuale/da-scrivere.md`; nel glossario, alla voce «Numero interno»,
  che il cliente lo legge senza «N.».

## Dubbi

1. **D-99 nel comando del coordinatore:** è il segnaposto dei test del banco, non una D usata; ho preso la D-84.
   Chiuso: la D-84 è nell'inventario.
2. **D-82 nell'inventario:** chiuso, il coordinatore ha aggiunto D-81…D-84 e l'estensione della D-82.
3. **Testo di consegna che è esattamente una data** (BASSA 4 della review): il Go lo lascia com'è (provato), ma il
   normalizzatore D-84 lo convertirebbe, con un falso allarme nel banco. Scritto come limite nel commento di `reD84`;
   i dati del banco non hanno un testo così. Raccomandazione: lasciarlo, e scrivere una riga di deviazione se mai
   capita.
4. **Pagina storica nel banco:** lo scenario `pagina-storica` non confronta il `<dl>` delle date (solo numero,
   preventivo e conversazione), quindi la D-84 lì la controllano solo i test Go. Raccomandazione: lasciarlo così
   (stesso modello e stessi dati di `/r/`, già confrontato); aggiungere il campo solo se un giro futuro tocca quello
   scenario.

## Review

Revisore: sottoagente indipendente, 14/11/2026, sul lavoro non committato. **Nessuna ALTA.** Corretto:

- **MEDIA 1:** l'anteprima del negozio senza la data della risposta al preventivo (sopra, «Cosa c'è»). `RispostaIl`
  metodo di `datiPaginaCliente`; test `TestPaginaClienteDataRisposta`.
- **BASSA 2 (R7):** prova che un testo di consegna «2026-03-01» resta com'è.
- **BASSA 3 (R10):** caso `dd("Consegnata il", "2026-11-04")` in `TestNormalizzaD84`; `reD84` non andava stretta
  (vuole già `\s*</dt>` subito dopo l'etichetta).
- **BASSA 4:** il limite del testo-data scritto nel commento di `reD84`.
- **R14 (facoltativo):** nessun attributo dell'`<article>` che cominci con `="AAAA-MM-GG`, escluso `data-last-ts`.

Mutazioni della review, esiti dopo le correzioni. R1–R6, R8, R9 e R11–R13 erano già prese dalla review; R7, R10, R14
e quella della MEDIA 1 rifatte su una copia del repo (`to-be-deleted-after-use/slice-074c/review/`, `muta.py`,
`giro.log`, un log per mutazione; copia cancellata), tutte prese su un'asserzione:

| # | Esito | Presa da |
|---|---|---|
| R1–R6 | presa (review) | test della slice |
| R7 testo di consegna che è una data convertito | **presa** | `TestPaginaClienteDateGiornoMeseAnno` («consegna scritta come una data: "01/03/2026"») |
| R8, R9 | presa (review) | test della slice |
| R10 normalizzatore con un `<dt>` più lungo | **presa** | `TestNormalizzaD84` («Consegnata il») |
| R11–R13 | presa (review) | test della slice |
| R14 attributo con una data AAAA-MM-GG nell'`<article>` | **presa** | `TestPaginaClienteDataRisposta` («data-ultimo="2026-10-31») |
| MEDIA 1 anteprima senza la data della risposta | **presa** | `TestPaginaClienteDataRisposta` («conferma anteprima: manca "il 05/11/2026 10:00"») |
