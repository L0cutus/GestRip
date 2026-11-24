# 117 — Comando `gestrip esporta-dettati` (T4.10, diciassettesima)

- **Fonte:** piano della T4.10, riga 117 (approvato da Stefano il 19/11/2026). **Chi decide:** lo standalone a
  `go-baseline` (`TrainingExport.run/2`, `export/5`, `report/1`; `Training.current/1`, `corrected_fields/2`), salvo le
  D approvate.
- **Voci:** RIP-H-52 (il resto, dopo i pseudonimi della 116) e OPS-09. Segno `// inventario:` sui test.
- **D nuova: D-112** (coordinatore, 24/11/2026, registrata nell'inventario): se la transazione finale fallisce, il file
  si toglie solo dopo aver verificato che le righe sono ancora tutte nel database; se ne manca una o la verifica non
  riesce, il file resta e il comando esce con un errore fatto di soli numeri. Nella stessa D: una proposta che non è un
  oggetto JSON dà una riga «errore» (lo standalone fallirebbe tutta l'esportazione).
- **Modelli:** implementatore Opus (dati, cancellazioni), revisione e mutazioni Opus.

## Cosa c'è

- `internal/assist/esportazione.go`: la parte pura. `RigaPerAddestramento` (come `export_line/5`): prima il cliente
  escluso, poi la busta nel cestino, poi `Pseudonimizza` (116, regole D-111 non toccate). Porting di `Training.current/1`
  e `corrected_fields/2` (risposta = busta com'è adesso, campi corretti nell'ordine della pagina). La proposta si legge con
  `UseNumber`. Motivo nuovo `busta_nel_cestino`, come lo standalone.
- `internal/buste/esporta_dettati.go`: `EsportaDettati(ctx, percorso)` come `TrainingExport.run/2`:
  1. una chiave nuova per ogni esportazione;
  2. lettura dei dettati con la busta attuale (cestino compreso) e l'esclusione da `dettati_esclusioni`; nomi di tutti i
     clienti e oreficerie per `ParoleDiAltriNomi`;
  3. file con `O_CREATE|O_EXCL` e permessi 0600 (più `Chmod` sul file aperto): mai sovrascritto, nemmeno attraverso un
     collegamento simbolico;
  4. scrittura, flush, fsync del file, chiusura, **fsync della cartella** (in più rispetto allo standalone, come chiede il
     piano);
  5. solo allora una transazione: cancella gli id letti (esportati, scartati ed «errore», come lo standalone) a blocchi di
     500 e scrive l'audit `assist.training_exported` (attore `export`, solo numeri). Una riga arrivata durante
     l'esportazione resta.
  - In errore prima della transazione: file tolto, niente cancellato, nessun audit. Errore nella transazione: D-112.
  - Gli errori dei file perdono il percorso (`senzaPercorso`): resta solo la causa con un testo fisso, perché un nome di
    file può contenere dati (lo standalone stampa solo l'atomo, per esempio `:eacces`).
- `cmd/gestrip/esporta_dettati.go`: `gestrip esporta-dettati [-db FILE] FILE.jsonl`; uscita 2 per l'uso sbagliato, 1 per
  un errore. Il messaggio del database non trovato non dice il percorso (diverso dagli altri comandi, scritto nel codice).

## Prove

- Test prima rossi (contro versioni vuote) e poi verdi; quelli nati verdi dopo una correzione già fatta sono verificati
  con una mutazione a mano che li fa fallire.
- Misurato: file esistente (niente scritto, cancellato o registrato); 0600 anche con umask 0; numero di busta assente;
  uscita solo `totali=`, `esportati=`, `scartati=`, `cancellati=`, `scartati_<motivo>=`; errore di scrittura, di sync del
  file, di chiusura e di sync della cartella, e Ctrl-C a metà (501 righe): niente cancellato, file tolto; D-112 nei tre
  rami (righe presenti → file tolto; righe sparite → file resta; verifica fallita → file resta); 501 righe tutte
  esportate e cancellate; cliente escluso e cestino; dati attuali della busta (descrizione, preventivo «35.50»,
  consegna, note, oreficeria); `UseNumber` (id 2^53+1 esatto); altri nomi; nessun nome o telefono vero nel file;
  messaggi senza percorso.
- `scripts/check` verde, rilanciato dalla sessione principale (exit 0, 0 rosse). Nessun banco (il piano non lo prevede).

## Mutazioni

- Implementatore: 25 su 25 uccise (primo giro), 10 su 10 (giro delle correzioni).
- Revisore: 34 compilanti, 27 uccise, 3 equivalenti (date come testo, blocchi da 1, `Chmod` tolto); le altre 4
  sopravvissute (errore di Close, contesto ignorato, totale degli scartati, blocchi oltre 500) sono coperte dai test
  aggiunti dopo. Resta la riga con `creato_il` illeggibile (sotto).

## Review (Opus, indipendente)

- **ALTA:** la D-112 non era applicata (l'implementatore non la conosceva): fatta.
- **MEDIA:** il percorso del file finiva nei messaggi d'errore: tolto.
- **BASSE:** test mancanti (aggiunti), commento fuorviante (corretto), test dei permessi con umask 0 (innocuo).
- Le correzioni della review sono verificate dalla sessione principale con `scripts/check`, senza un secondo giro di
  revisione.

## Per il manuale

- Il comando `gestrip esporta-dettati FILE.jsonl` salva in un file i dettati raccolti, con nomi e telefoni dei clienti
  sostituiti da valori inventati.
- Il file deve essere nuovo: se esiste già, il comando si ferma e non tocca niente.
- Solo il proprietario può leggere il file.
- Nel file la risposta giusta è la busta com'è adesso, quindi entrano anche le correzioni fatte dopo il dettato. Il numero
  della busta non compare mai.
- A schermo escono solo numeri: quanti dettati c'erano, quanti esportati, quanti scartati e perché, quanti cancellati.
- Dopo un'esportazione riuscita i dettati letti si cancellano dall'apparecchio, anche quelli scartati; quelli arrivati
  nel frattempo restano.
- Se qualcosa va storto non si cancella niente e il file a metà viene tolto; nel caso raro in cui non si sa se i dettati
  sono stati cancellati, il file resta e il comando lo dice.

## Differenze dallo standalone (dichiarate)

- D-112 (sopra) e sync della cartella.
- Messaggi d'errore senza percorso.
- Casi limite che lo standalone non gestisce o gestisce diversamente, senza effetti sulla riservatezza: un `creato_il`
  illeggibile (impossibile per il CHECK dello schema) dà una riga «errore» cancellata invece di fermare tutto; una
  proposta con l'oreficeria come lista o mappa dà «errore»; un numero scritto «1e3» resta com'è (Jason scriverebbe
  «1.0e3»); U+2028/U+2029 scritti come escape (JSON equivalente).

## Dubbi

1. L'esportazione non controlla il blocco dopo un import (`db.BloccatoDopoImport`), come lo standalone: è un comando
   lanciato a mano dal titolare. Da tenere a mente al cutover.
2. Nessuna chiave fissa per i test: i test controllano proprietà e conteggi, non i nomi inventati esatti.
