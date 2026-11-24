# 118b — Scadenza di lettura delle foto tolta a corpo finito (T4.10, mini-slice)

- **Fonte:** nota 10 di `docs/domande-aperte.md` (revisione della 109), approvata da Stefano come mini-slice; via del
  coordinatore il 24/11/2026 (quarta slice del giorno). **Chi decide:** la correzione della 109 per la clip
  (`SetReadDeadline(time.Time{})` a corpo finito). Difetto del Go: lo standalone non c'entra.
- **Voci:** nessuna voce nuova dell'inventario; nessuna D nuova (ultima usata D-112).
- **Modelli:** implementatore Opus (due giri: ~107k e ~122k token di contesto, ~19 minuti in tutto), revisione e mutazioni Opus (~98k token, ~18 minuti).
- **Stato: banco da fare.** Commit solo locale, senza push: il coordinatore non ha dato il «memoria libera» (11 GB
  disponibili con Inkling acceso e la prova in ombra della 117 in corso). Push, copertura col banco e riga di
  avanzamento dopo il giro verde.

## Cosa c'è

- `togliScadenzaLettura(w)` in `internal/web/modulo.go`: toglie la scadenza di lettura dell'ultimo pezzo quando il corpo
  è stato letto fino in fondo. La usano la scheda (`caricaFoto`), la coda (`caricaFotoCoda`) e la clip (`trascriviClip`,
  stesso comportamento della 109).
- **Scheda:** dopo `riceviFoto`, prima del ricontrollo della sessione. Se la lettura si è fermata al limite del corpo con
  una violazione (`arrivo.interrotto`), la scadenza resta: il corpo non è finito e lo scarto del resto prima della
  risposta non deve aspettare un client fermo (rilievo MEDIA della revisione).
- **Coda: stesso difetto.** net/http toglie da sé la scadenza a fine corpo e avvia la lettura di fondo, ma
  `lettoreLimitato` chiama `alPezzo` dopo quella lettura: se l'ultima lettura chiude anche un pezzo (1 MiB nella coda)
  la scadenza si rimette, scade durante l'attesa del figlio o la normalizzazione, e la richiesta viene cancellata.

## Prove

- `TestCaricamentoNormalizzazioneOltreIlPezzo` (scheda, normalizzatore finto da 900 ms, pezzo 300 ms): rosso «manca
  Foto caricata», 0 foto; verde.
- `TestCodaFotoNormalizzazioneOltreIlPezzo` (coda, foto di 1 MiB esatto): rosso `500 Foto non salvata.`; verde.
- `TestCaricamentoOltreIlLimiteClienteFermo` (oltre il limite con violazione, client fermo 5 s): rosso «risposta solo
  dopo che il client ha chiuso (5,13 s)»; verde in 1,24 s.
- `scripts/check` verde rilanciato dalla sessione principale (senza banco): voci 353, chiuse 53, rosse 0, parziali 192,
  aperte 97, fuori v1 11.

## Mutazioni

- Revisore: 6 su 6 compilano e sono uccise (chiamata tolta da scheda, coda, clip; spostata prima di `riceviFoto`;
  helper vuoto; scadenza breve al posto di toglierla).
- Sessione principale: 1 su 1 (segno `interrotto` mai messo → ucciso da `TestCaricamentoOltreIlLimiteClienteFermo`).

## Review (Opus, indipendente)

- **ALTA:** nessuna.
- **MEDIA:** la scadenza si toglieva anche con il corpo interrotto oltre il limite (client fermo: risposta a 6,1 s invece
  di 0,42 s, senza tetto in produzione): chiusa con `arrivo.interrotto` e il test sopra.
- **BASSE:** commento e documenti più ottimisti del codice (allineati); l'attesa di un posto del figlio ora non ha un
  tetto lato server, la limita solo il contesto (comportamento voluto, segnalato); margini dei test (300/900 ms) robusti,
  1 MiB in 300 ms da TLS in loopback passato anche con carico 6-17.

## Per il manuale

- Caricare più foto insieme dalla scheda di una busta, o una foto grande dalla coda della nuova busta, non finisce più con
  «Foto rifiutata o non salvata.» o «Foto non salvata.» quando il salvataggio dura più di qualche secondo dopo la fine
  dell'invio: le foto si salvano anche se l'elaborazione è lenta.
- Per il resto non cambia niente di visibile.

## Differenze dallo standalone

- Nessuna: è un difetto del solo Go.

## Dubbi

- Il punto debole resta in `lettoreLimitato`, che chiama `alPezzo` anche sulla lettura che ha già raggiunto la fine del
  corpo; chiuso nei gestori come nella 109, senza toccare il lettore.
- LSP: non usato né dall'implementatore né dal revisore (bastati grep e letture mirate).
