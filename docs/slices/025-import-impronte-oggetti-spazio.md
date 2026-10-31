# 025 — Import: impronte salvate come testo (D-42), trigger e viste, spazio libero, OPS-03

- **Fonte:** risposte di Stefano del 31/10/2026 (domande 1–7 delle slice 022–024 e domanda su
  trigger e viste della 022c), `docs/guida-import.md`.
- **Voci:** chiusa **OPS-03**; rinforzate OPS-01 (trigger e viste) e OPS-07 (spazio libero, che
  nella 022 era stata chiusa senza questo controllo); D-42 fatta (OPS-05 resta parziale per i link
  storici, VEN-31).
- **Lavoro:** implementazione e mutazioni di un sottoagente, review di un sottoagente diverso;
  correzioni della review, catena e banco fatti dalla sessione principale.

## Cosa c'è

- **D-42** (`importa.go`, `media.go`): solo nella colonna `sha256` di `historical_documents`,
  `sale_photos` e `sale_revision_photos`, un TEXT di esattamente 32 byte si legge con
  `CAST(… AS BLOB)` e si scrive come BLOB con gli stessi byte; il report ha un avviso
  `tipo/impronta_testo` per tabella con il numero di righe. Il controllo del file usa gli stessi
  32 byte. Ogni altra forma blocca come prima (`tipo/blob_atteso`): 31 o 33 byte, 32 caratteri
  non ASCII (64 byte), 64 cifre esadecimali, numeri, un TEXT di 32 byte in un'altra colonna o
  tabella.
- **Trigger e viste** (`oggetti.go`): confrontati esattamente (tipo, nome, tabella, testo) con
  `internal/importer/testdata/standalone-go-baseline-oggetti.json`; diverso → `schema/oggetto_diverso`
  con il nome dell'oggetto (campo `oggetto` del report), mai il testo.
  - Il riferimento è il testo di **un database vero** dello standalone (`sqlite_schema` del
    database decifrato della catena del banco, immagine `fab-219` = `go-baseline`), rigenerato con
    `tools/schema-standalone/oggetti.py`. La fixture `.sql` ricavata dalle migrazioni ha gli stessi
    28 trigger con un'altra impaginazione (gli heredoc di Elixir non si tolgono il rientro):
    misurato con la catena, con la fixture come riferimento si bloccavano tutti e 28.
    `TestOggettiVeriComeLaFixture` controlla che i due file abbiano gli stessi oggetti e lo stesso
    testo a meno degli spazi, così se se ne rigenera uno solo il test se ne accorge.
  - I database sintetici dei test hanno i trigger con il testo vero.
- **Spazio libero** (`sorgente.go`, `sistema_linux.go`): prima di creare la cartella di lavoro,
  servono `2 × sorgente + file della cartella media (se -media) + 1 GiB` liberi per l'utente
  (`Bavail × Frsize`); sotto, rifiuto con quanto serve e quanto c'è. Fuori da Linux l'import
  rifiuta.
- **OPS-03**: test segnati per righe nel cestino importate, riga che non si traduce o che il Go
  rifiuta che blocca con tabella/id/colonna/motivo, nessuna riga sparita in silenzio, «vince il
  testo», consenso email mai attribuito. Le regole Flask→Elixir (date, orari, compro oro) non si
  applicano all'import verso Go.

## Prove

- **TDD:** rosso misurato prima del codice per D-42 (riproduce il blocco della 024), trigger e
  viste, spazio, riferimento vero (51 test rossi con il riferimento vecchio); i casi che
  provavano comportamenti già giusti erano verdi da subito e sono provati dalle mutazioni.
  Dichiarato: due casi aggiunti durante le mutazioni (nome di file di 32 byte che resta testo;
  cartella media con sottocartella e link).
- **Mutazioni:** 50 + 12, tutte compilano: 59 uccise, 3 vive (due equivalenti; `Bavail`→`Bfree`
  non si vede senza un file system vero). Correzione della review: la chiave per nome è uccisa da
  `TestVistaConIlNomeDiUnTriggerRicreato`.
- **Catena del banco** (`banco su -dati sintetici-import`): import Go **riconciliato**, 45 righe,
  5 file, 141 byte, avviso `tipo/impronta_testo` su `historical_documents` (1 riga). Gli scenari
  sui dati importati danno una sola differenza, quella nota di `utenti` passo 2 (standalone e Go
  uguali, è l'attesa dello scenario): T3b.
- `scripts/check` verde; con l'esito del banco **31 voci chiuse**, 3 parziali. **Banco verde** con
  `fab-219` (13 scenari, 156 passi), spento, nessun avanzo.

## Review indipendente

Nessuna ALTA. Corretti:
- **MEDIA:** una vista con il nome di un trigger, con il trigger ricreato, passava (trigger e viste
  hanno nomi separati in SQLite, la mappa era per nome). Ora la chiave è tipo e nome.
- **BASSA:** spazio con `Frsize` (come statvfs) invece di `Bsize`; casi D-42 con 32 caratteri non
  ASCII e controllo della lunghezza dei casi 31 e 33 byte.
- **Dichiarato:** i test che importano davvero vogliono almeno 1 GiB libero nella cartella
  temporanea (`TMPDIR`), per il controllo dello spazio vero.

Domanda aperta (non blocca): gli indici della sorgente non si confrontano; raccomandazione: tenerli
fuori (`docs/domande-aperte.md`).

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** vedi sotto.
