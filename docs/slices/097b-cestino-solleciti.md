# 097b — Le buste nel cestino fuori dai solleciti all'artigiano (correzione di 083 e 096)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/repo.ex:40-48` (`prepare_query/3`
  aggiunge `is_nil(deleted_at)` a ogni query su `Repair` e `Sale` salvo `with_discarded`),
  `workshop/artisan_reminders.ex:143-155` (`late_query/3` parte da `Repair`), chiamanti senza `with_discarded`
  (`pending_count` :94, `late_bags` :104-118, `jobs/artisan_chases.ex:79-92`); test `briefing_test.exs:220`. Trovato
  dal revisore della 097 (Dubbio 1 della 097). **Decisione di Stefano del 18/11/2026:** le buste nel cestino si
  escludono dai solleciti, come lo standalone; rovescia quella del 15/11 (Dubbio 2 della 083), presa sulla premessa
  sbagliata «`late_query/3` non guarda `deleted_at`».
- **Voci:** ART-20, ART-21, ART-07 (contatore), JOB-09, JOB-10 (fuori bottega): stesso stato di prima. Nessuna
  migrazione, nessuna D: è un allineamento allo standalone.

## Cosa cambia

- **`internal/buste/solleciti.go`:** `sqlRitardo` (late_query/3) ha `b.cancellata_il IS NULL`. Vale per tutti gli
  usi, che passano tutti da lì: la pagina «⏰ Solleciti pendenti» (`SollecitiPendenti`), l'ultimo sollecito di ogni
  busta (la sottoquery su `sqlRitardo`), il contatore della lista consegne (`ContaSollecitiPendenti`), il sollecito a
  mano (`RegistraSollecito`), il lavoro della 096 (`TimbraSollecitiDelGiro` via `busteInRitardoDove`) e il «fuori
  bottega» del briefing della 097.
- **`internal/buste/briefing.go`:** tolto il filtro del cestino aggiunto apposta per il briefing (ora ridondante);
  `fuoriBottegaBriefing` chiama `busteInRitardo`. Il briefing non cambia.
- Commenti di `internal/jobs/solleciti.go` e ricevute 083, 096, 097, `docs/SVILUPPO.md` allineati.

## Prove

- `TestSollecitiPendenti` (`internal/buste/solleciti_test.go`): la busta nel cestino (30 giorni fuori) è fuori
  dall'elenco, dal contatore e dalle soglie della pagina, e il sollecito all'artigiano della sua consegna nomina solo
  l'altra busta.
- `TestSollecitiLeBusteDellaPagina` (`internal/jobs/solleciti_test.go`): il lavoro non nomina né timbra la busta nel
  cestino; pagina 1, un avviso, riepilogo «Bulino: 32/26 (18gg)».
- `TestBriefingCinqueSezioni` (invariato) resta verde senza il filtro apposta.
- Rosso prima del codice (i due test sopra), poi verde. Mutazioni a mano (7) su `sqlRitardo` e sui suoi usi: 6 prese;
  viva solo quella che toglie il cestino dalla sottoquery dell'ultimo sollecito, equivalente (l'ultimo sollecito si
  legge solo per le buste dell'elenco, che il cestino non ce l'ha già più).
- `scripts/check` verde.
- **Revisione:** fatta dalla sessione principale (Opus) sul diff, tre righe di codice più commenti e test; ok del
  coordinatore (18/11/2026) a non chiamare un revisore separato per un cambio così piccolo. Nessun rilievo.

## Banco

- Gli scenari del banco sui solleciti (`bench/scenari/solleciti.json`, `bench/browser/sollecito-artigiano.json`,
  `bench/browser/consegne-solleciti.json`) non mettono buste nel cestino e non hanno attese scritte sul cestino:
  nessuna attesa cambia. Confrontano il Go con lo standalone, che il cestino lo esclude già: con una busta nel cestino
  ancora fuori nei dati, ora le due app tornano.

- **Giro s097b (18/11/2026): rosso solo per lo standalone**: browser 2 errori e 8 inattese della nota 8 (sesta e
  settima comparsa, «500 GET /repairs/81» e «500 GET /audit»); HTTP 1 inattesa, il `content-type` mancante dello
  standalone sulla foto inesistente (prima comparsa, accanto alla nota 8 in `docs/domande-aperte.md`).
- **Giro identico s097c (ok del coordinatore): VERDE.** HTTP 83 scenari, 1314 passi, 0 inattese, 0 errori; browser 29
  scenari, 58 viste, 0 inattese, 0 errori. Memoria disponibile minima 20,8 GB. Copertura con l'esito: 353 voci, chiuse
  211, rosse 0.

## Per il manuale

- Le buste nel cestino non compaiono più fra i solleciti all'artigiano (pagina, contatore, sollecito e riepilogo
  automatico).
