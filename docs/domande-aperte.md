# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

1. **Import: indici della sorgente (OPS-01, slice 025).** Con la decisione su trigger e viste ora si
   confrontano tabelle, colonne, trigger e viste; gli indici della sorgente no. Un indice non
   contiene righe e non cambia i dati letti; il Go crea i suoi. **Raccomandazione:** non
   confrontarli (un indice in più o in meno nello standalone non cambia niente di quello che si
   importa); se si preferisce la stessa severità dei trigger, si aggiungono al riferimento vero
   con lo stesso blocco `schema/oggetto_diverso`. Il resto non dipende dalla risposta.

2. **Import: istante senza zona in `sale_revision_photos.inserted_at` (slice 026).** Misurato con
   la catena del banco (`fab-219` = `go-baseline`): una vendita emessa dalla pagina con una foto
   scrive in `sale_revision_photos.inserted_at` `2026-10-31T10:00:30.034145`, **senza la `Z`**.
   Causa, letta al tag: `issue_sale` (`copy_draft_photos`) e `revise_sale`
   (`copy_revision_photos`) passano `now`, un `DateTime`, a una query scritta a mano, ed exqlite
   0.40 lo scrive senza zona; la copia delle foto importate da Flask
   (`copy_imported_revision_photos!`) usa `DateTime.to_iso8601` e ha la `Z`. Il valore è UTC per
   costruzione (`DateTime.utc_now()`), e le altre colonne della stessa emissione (revisione,
   sigillo) hanno la `Z`. Il Go blocca (`traduzione/istante`, poi `riconciliazione/conteggio`), e
   bloccherebbe ogni database vero con una vendita con foto emessa o corretta dall'interfaccia.
   La catena si ferma qui: banco con `-dati sintetici-import` rosso. Prova su una copia: con la
   `Z` aggiunta a quel valore l'import è riconciliato (54 righe, 6 file, 703 byte) con i tre avvisi
   `tipo/impronta_testo` attesi. **Raccomandazione:** come D-42, solo in
   `sale_revision_photos.inserted_at`, accettare la forma `AAAA-MM-GGThh:mm:ss[.µs]` senza zona
   come UTC, scriverla con la `Z`, e un avviso per tabella con il numero di righe (per esempio
   `traduzione/istante_senza_zona`); ogni altra forma o colonna continua a bloccare. Lo standalone,
   se non lo si corregge, resta com'è. Da fare in una slice dell'import; poi la catena del banco
   controlla anche questo avviso.

## Note per lo standalone

1. **Impronte salvate come TEXT (slice 024, D-42).** Verificato dalla sessione dello standalone il
   31/10/2026: oltre a `historical_documents.sha256` anche `sale_photos.sha256` e
   `sale_revision_photos.sha256`. Domanda di allora: Lo standalone confronta o
   cerca mai `historical_documents.sha256` (controllo d'integrità, deduplica, pulizia dei file)? Se
   sì, con il valore salvato come TEXT quel confronto con un BLOB fallisce già oggi in produzione
   per ogni PDF storico arrivato da Flask. Da verificare dalla sessione dello standalone (Stefano,
   31/10/2026); il Go non ne dipende.

## Risposte già registrate

Le domande 1–15 (slice 013b–018) sono state approvate da Stefano il 30/10/2026 e
registrate: deviazioni D-34…D-41, note nell'inventario (ACC-16, ADM-17, ADM-20, ADM-22, OPS-12) e
nel piano (T2, T4, T5, §7.3).

La domanda 1 della slice 020 (dettagli di ACC-16) è stata approvata da Stefano il 31/10/2026,
con tutte e quattro le raccomandazioni: il link di `gestrip primo-titolare` vale 60 minuti; alla
creazione del link si scrive la riga d'audit `administrator.bootstrap_requested` con attore
`console`; `/` e `/login` su un database vuoto non dicono niente di nuovo; nessun limite ai
tentativi su `/configura` (codice da 256 bit). Registrata nell'inventario (ACC-16).

Le domande 1–7 delle slice 022–024 sono state approvate da Stefano il 31/10/2026, tutte come
raccomandate; per la 7 solo la (a), senza correggere lo standalone per ora:
- righe nel cestino importate, righe incoerenti che bloccano: nota in OPS-03, che si chiude;
- spazio libero prima di copiare (2 × sorgente + media + 1 GiB), resti di un import interrotto
  da togliere a mano (scritto in `docs/guida-import.md`), `-senza-media` solo con `-sintetico`:
  nota in OPS-07;
- file media non nominati come avviso, coda delle cancellazioni prudente: nota in OPS-02;
- impronta TEXT di 32 byte riscritta BLOB con avviso nel report: deviazione D-42. Estesa lo stesso
  giorno a `sale_photos` e `sale_revision_photos` dopo la verifica della sessione dello standalone
  (causa unica: le scritture con `Repo.query` e un binario semplice salvano TEXT); lo standalone
  non verrà corretto, il banco sintetico copre foto di vendita caricate dall'interfaccia e una
  revisione di vendita.

La domanda su trigger e viste della sorgente (giro di mutazioni della 022c) è stata approvata da
Stefano il 31/10/2026 come raccomandata: un elenco di trigger e viste diverso dal riferimento
blocca l'import con `schema/oggetto_diverso`. Nota in OPS-01; si fa nella slice 025.
