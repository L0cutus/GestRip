# Guida dell'import dallo standalone

`gestrip import` porta il database e i media dello standalone (Elixir) in un database Go **nuovo**.
Lo standalone non si tocca: resta intatto e riavviabile come ritorno indietro (OPS-06, mai).
Le regole stanno nell'inventario (OPS-01…OPS-10) e nelle deviazioni approvate (D-42).

## Prima di partire

- Lo standalone è fermo e il suo database è stato **decifrato** in un file SQLite in chiaro, su
  un volume cifrato. Il Go legge la sorgente solo in sola lettura e non la modifica.
- La destinazione, il report e la cartella `media/` accanto alla destinazione **non devono
  esistere**: l'import non sovrascrive mai niente.
- Lo SHA-256 della sorgente si calcola prima (`sha256sum`) e si passa con `-sha256`: se la
  sorgente cambia, l'import si rifiuta.

## Il comando

    gestrip import -autorizzato -riferimento "<chi e quando>" \
      -sorgente <db decifrato> -sha256 <impronta> \
      -destinazione <cartella del servizio>/gestrip.db -report <report.json> \
      -media <cartella media dello standalone> [-miniature <cartella miniature>]

- `-sintetico` al posto di `-autorizzato` si usa solo con dati inventati (test, banco).
- `-senza-media` è accettata **solo con `-sintetico`**: un database vero senza le foto e le firme
  che nomina non serve.
- Le miniature si contano e non si importano: il Go le rigenera.

## Che cosa succede

1. Controlli: modalità, percorsi, file accanto alla sorgente, volume cifrato, impronta, schema.
2. Copia privata della sorgente in una cartella di lavoro `.gestrip-import-*` accanto alla
   destinazione; tutte le tabelle passano nel database nuovo, ogni valore tradotto e controllato.
3. Media: ogni file nominato da una riga si copia con dimensione e impronta controllate.
4. Report `report.json` (0600): conteggi, avvisi, differenze, verdetto. Non contiene mai dati dei
   clienti.
5. Solo se il verdetto è **riconciliato** il database e `media/` prendono il loro nome. Altrimenti
   non si attiva niente e il report dice tabella, id, colonna e motivo.

Regole sui dati (decise da Stefano il 31/10/2026):
- le righe nel cestino si importano (il Go ha il cestino);
- una riga che non si traduce o che il Go rifiuta **blocca** l'import, non viene saltata;
- i file della cartella media che nessuna riga nomina sono un **avviso** e non si copiano;
- un file nella coda delle cancellazioni che c'è ancora si copia; se il contenuto non è quello
  della sua chiave l'import si blocca, da guardare a mano;
- l'impronta salvata come testo dallo standalone (esattamente 32 byte) nei PDF storici, nelle foto
  delle vendite e nelle foto delle revisioni di vendita si riscrive come BLOB e il report lo
  segnala come avviso per tabella, con il numero di righe; ogni altra forma blocca (D-42, dalla
  slice 025);
- prima di copiare si controlla lo spazio libero accanto alla destinazione: almeno due volte la
  sorgente più la cartella media più 1 GiB, altrimenti rifiuto (dalla slice 025).

## Se l'import si interrompe

Con Ctrl-C o SIGTERM l'import si ferma pulito: toglie la sua cartella di lavoro e non attiva
niente.

Dopo un'interruzione **di colpo** (SIGKILL, mancanza di corrente) possono restare accanto alla
destinazione:
- una cartella `.gestrip-import-*`, con dentro una **copia in chiaro della sorgente** (sul volume
  cifrato);
- se l'arresto è caduto fra i media e il database, una cartella `media/` senza `gestrip.db`.

Regola: **i resti si tolgono a mano**, il Go non cancella mai dati da solo.
- Un nuovo `gestrip import` rifiuta di partire finché ci sono i resti e dice quale ha trovato.
- `gestrip serve` non parte se il database manca e accanto ci sono i resti: non crea un database
  vuoto vicino alle foto dei clienti.

Cosa fare: guardare che cosa c'è, cancellare la cartella `.gestrip-import-*` e la `media/` senza
database (sono solo copie: lo standalone ha ancora tutto), poi rifare l'import da capo.
