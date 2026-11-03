# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

Slice 037c (CI instabile, errori di risorse del processo delle immagini). Nessuna blocca il lavoro.

6. **Memoria e CPU danno anch'esse «riprova»,** anche se per la stessa foto si ripeterebbero.
   Misurato: con le foto che si accettano il tetto ha almeno 125 MiB di margine e la CPU usata è
   circa 2,5 s su 8. **Raccomandazione:** tenerlo così.
   Va rivista alla T5 con la misura della CPU sull'apparecchio (Stefano, 01/11/2026).

Slice 040a (livello browser del banco). Non blocca il lavoro.

18. **«Massimo 2 foto per volta.» quando se ne scelgono 3 dalla scheda.** Misurato nel browser vero:
    in tutti e due i programmi si salvano le prime 2 e compare «Foto caricata.». Nello standalone
    il messaggio «Massimo 2 foto per volta.» compare e **sparisce dopo circa 0,2 s**, perché il
    browser annulla la terza foto e LiveView ricalcola gli errori (`drop_entry`): in pratica non si
    vede. Nel Go resta fino alla pagina successiva. **Raccomandazione:** tenere il Go (chi ha scelto
    3 foto capisce perché ne sono salite 2), come D-xx.

Slice 040b (coda delle foto della nuova busta). Non blocca il lavoro.

19. **Fin dove la coda delle foto sopravvive cambiando pagina.** Nello standalone (LiveView) le
    pagine del personale non si ricaricano mai, quindi le foto scelte per una busta continuano a
    salire su qualunque pagina. Nel Go le pagine si ricaricano: finché ci sono foto in coda,
    `app.js` cambia pagina senza ricaricare solo nel giro lista ↔ «Nuova riparazione» (quello del
    lavoro al banco). Se con foto ancora in coda si apre una scheda, il menu o «Esci», il browser
    chiede conferma e, se si esce, quelle foto si perdono. **Raccomandazione:** va bene per la v1;
    da rivedere con la T4.2 (la lista con i link alle schede), allargando il cambio pagina senza
    ricarica anche a quei link.

Slice 041 (pulizia dei file delle foto). Non blocca il lavoro.

20. **Cancellazioni di file ereditate dallo standalone, subito dopo l'import.** L'import porta nel
    Go la coda dei file che lo standalone aveva già deciso di cancellare (`media_deletion_jobs`).
    Lo standalone li cancellerebbe entro 60 s, e la sua cartella resta comunque intatta. Nel Go,
    per prudenza, la pulizia di quella coda **aspetta il primo backup verificato** dopo l'import,
    come già la conservazione del registro (ADM-17, decisione del 30/10/2026); i file temporanei
    abbandonati invece si tolgono subito. **Raccomandazione:** tenere l'attesa (un file cancellato
    non torna, e aspettare il backup costa solo qualche file in più per un giorno), come D-xx.

Slice 042 (motore dei PDF e ricevuta). Nessuna blocca il lavoro.

21. **Un numero di busta troppo grande nell'indirizzo del PDF.** Misurato sul banco: con un id
    oltre i 64 bit (`/repairs/9223372036854775808/receipt.pdf`) lo standalone risponde 500
    «Internal Server Error»; fino a 9223372036854775807 risponde 404 «Not Found». Il Go per ora
    copia il 500. **Raccomandazione:** nel Go 404 come per ogni busta che non c'è, come D-xx; per
    lo standalone è la nota 5. Vale anche per le altre rotte `/repairs/{id}/…` dei PDF.
22. **Caratteri che il font dei PDF non ha** (emoji, ideogrammi) scritti in un campo della busta.
    Lo standalone (Chromium) li disegna con un altro font del sistema; il Go usa solo DejaVu Sans
    incorporato e al loro posto stampa «�». Testo italiano, accenti ed «€» escono uguali.
    **Raccomandazione:** tenere «�» (i documenti restano leggibili e il binario non porta font in
    più), come D-xx.

Slice 043 (etichetta della busta e scheda PDF). Nessuna blocca il lavoro.

23. **QR dell'etichetta disegnato a specchio nello standalone.** Misurato due volte in modo
    indipendente sui PDF dello standalone del banco: il QR dell'etichetta 76×127 è **trasposto**
    (uno specchio sulla diagonale, che nessuna rotazione raddrizza); il contenuto è giusto. Non ho
    misurato quali lettori lo leggono: se le etichette sono in uso in negozio, il lettore che usate le
    legge (va confermato). Il Go stampa il QR diritto, con lo stesso contenuto. **Raccomandazione:** tenere il QR diritto nel Go
    (nota 6 per lo standalone) e, con la T4.2, far leggere al lettore QR del Go **anche** i codici
    specchiati, perché le etichette già attaccate alle buste sono così.
24. **Font della scheda PDF.** Lo standalone stampa la scheda 100×230 in Liberation Sans (il font che
    Chromium trova nell'immagine), il Go in DejaVu Sans come gli altri PDF. Il testo è identico; cambia
    dove vanno a capo le righe, e una scheda molto lunga potrebbe passare alla seconda pagina in un
    punto diverso. **Raccomandazione:** tenere DejaVu (un solo font per tutti i documenti), come
    D-xx; Liberation Sans (licenza OFL) si può incorporare più avanti se la stampa vera lo chiede.

## Note per lo standalone

1. **Impronte salvate come TEXT (slice 024, D-42).** Verificato dalla sessione dello standalone il
   31/10/2026: oltre a `historical_documents.sha256` anche `sale_photos.sha256` e
   `sale_revision_photos.sha256`. Domanda di allora: Lo standalone confronta o
   cerca mai `historical_documents.sha256` (controllo d'integrità, deduplica, pulizia dei file)? Se
   sì, con il valore salvato come TEXT quel confronto con un BLOB fallisce già oggi in produzione
   per ogni PDF storico arrivato da Flask. Da verificare dalla sessione dello standalone (Stefano,
   31/10/2026); il Go non ne dipende.

2. **Tipo servito per una foto che non si riconosce (slice 037, D-47).** Senza urgenza (Stefano,
   01/11/2026). La rotta delle foto del personale, se i byte non sono un'immagine riconosciuta,
   ripiega sul `tipo_mime` della riga qualunque sia: una riga con `text/html` (database manomesso,
   import strano) verrebbe servita come HTML sull'origine interna. Il Go ripiega solo su `image/*`,
   altrimenti `application/octet-stream`. Oggi non succede: nessuna scrittura mette un tipo così.

3. **Scrittura del file temporaneo senza controllo (slice 039, D-50).** Senza urgenza (Stefano,
   02/11/2026). In `RepairPhotoUploadController` l'esito di `IO.binwrite` non si guarda: con il disco
   pieno il file resta troncato e il caricamento finisce come «Foto rifiutata.» (422), come se la
   foto fosse sbagliata. Il Go risponde 500 «Foto non salvata.».

4. **500 «database is locked» sulle miniature mentre si caricano foto (slice 040a).** Misurato sul
   banco con i dati sintetici, immagine `fab-220`. `RepairPhotoController.show/2`
   (`repair_photo_controller.ex:15`) scrive la riga d'audit della lettura in un `BEGIN IMMEDIATE`:
   se la scheda chiede insieme diverse miniature mentre nella stessa busta si caricano foto, le
   richieste aspettano i 5 s del busy timeout e rispondono 500 (`Exqlite.Error database is locked`),
   e la miniatura resta rotta. Su 5 giri completi del banco è successo in 1; ripetendo lo scenario
   sulla stessa istanza, su quasi tutte le miniature delle buste coinvolte. Il Go, con le stesse
   azioni, non ha mai risposto 4xx o 5xx. Non tocca la sicurezza; nel banco il campo è solo sul Go
   (`solo_go`). Da passare alla sessione dello standalone.

5. **500 su un id di busta oltre i 64 bit (slice 042).** Misurato sul banco, immagine `fab-220`:
   `GET /repairs/9223372036854775808/receipt.pdf` (e `99999999999999999999`) risponde 500
   «Internal Server Error», mentre `9223372036854775807` risponde 404. Causa probabile (letta, non
   misurata): l'id diventa un intero che SQLite non sa confrontare. Non tocca la sicurezza: è solo un
   errore del server al posto del 404. Da passare alla sessione dello standalone (domanda 21).

6. **QR dell'etichetta busta trasposto (slice 043).** Misurato sui PDF di `fab-220`
   (`/repairs/:id/label.pdf`): il QR è lo specchio sulla diagonale della matrice giusta (versione 1,
   livello M, maschera 0): differisce in 2 moduli dalla trasposta e in 104-168 da ogni rotazione
   diritta. Causa probabile (ragionata, non
   misurata): righe e colonne scambiate nel disegno dell'SVG. Non tocca la sicurezza. Da passare
   alla sessione dello standalone (domanda 23).

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

Le domande 1–2 della slice 025–026 sono state approvate da Stefano il 31/10/2026 come raccomandate:
- gli indici della sorgente non si confrontano (nota in OPS-01);
- in `sale_revision_photos.inserted_at` un istante senza zona si accetta come UTC, si scrive con
  la `Z` e il report dà l'avviso `traduzione/istante_senza_zona` per tabella con il numero di
  righe; ogni altra forma o colonna blocca. Registrata nella D-42 (stessa causa: scritture a mano
  con exqlite 0.40); si fa nella slice 027. La sessione dello standalone sta cercando tutte le
  colonne scritte a mano con lo stesso problema: quando arriva l'elenco si estende in una volta.
- elenco arrivato lo stesso giorno dalla sessione dello standalone (sola lettura, database
  sintetico): la regola vale per tutte e sole `sale_revision_photos.inserted_at`,
  `emergency_entries.inserted_at`, `emergency_events.closed_at` e `updated_at`,
  `repairs.retention_started_at` e `updated_at`; una colonna può avere righe con e senza `Z`,
  l'avviso conta solo quelle senza. Registrato nella D-42. Nella catena del banco si aggiunge il
  registro d'emergenza fatto con le funzioni dello standalone (slice 028).

Domande 1–4 delle slice 030 e 032 (Stefano, 31/10/2026): 1–2 difetti dello standalone, corretti
dalla slice 220 dello standalone (`12d6e7f`, `go-baseline` dal 01/11/2026) e allineati nel Go con la
slice 037d (testi del catalogo `it`, `buste.NormalizzaNumero`; la nota per lo standalone è tolta);
3 → D-43; 4 → D-44 (inventario).

Domande 1–14 delle slice 033–037 (Stefano, 01/11/2026):
- 1, 6, 9 (righe `repair.viewed`): non come raccomandato; una riga solo per un'apertura vera della
  scheda, nessuna dopo il redirect di un'azione (rifiuti compresi), per «Modifica» e per «Annulla».
  Il segno del redirect non si può forgiare. D-44 estesa; nota in RIP-S-01.
- 2, 3, 4, 7: accettate; RIP-S-04, 11, 07, 14 restano parziali fino al livello browser del banco.
- 5: tacere anche sulla scheda (D-45, RIP-S-13).
- 8: rifiutare «Genera nuovo link» senza `indirizzo_pubblico` prima di toccare qualunque cosa, sul
  modello D-32; la creazione della busta resta com'è (D-46, RIP-S-24).
- 10 e 13: 1440 MiB e al più 2 processi delle immagini insieme; alla T5 la misura della RAM
  sull'apparecchio, e se non ci sta si abbassa con una D-xx (nota in RIP-S-30 e nel piano, T5).
- 11: accettate come scritte nei test, senza D-xx (nota in RIP-S-30).
- 14: ripiego sul tipo della riga solo se `image/*`, altrimenti `application/octet-stream` (D-47,
  RIP-S-33); nota 2 per lo standalone (era la 3 fino alla slice 037d).
Le parti di codice (1, 5, 6, 8, 9, 13, 14) le porta la slice 037b.

Domande 1–4 della slice 037b e 5, 7, 8, 9 della 037c (Stefano, 01/11/2026, tramite il revisore
della 037b), tutte come raccomandate:
- 1: un rifiuto senza il segno della pagina conta come apertura (nota in RIP-S-01, D-44);
- 2: il ripiego sul tipo della riga esclude anche `image/svg+xml` (D-47);
- 3: durate dei segni 2 minuti e 1 ora, catena al più un'ora dall'ultima riga, 10 000 in memoria;
- 4: `?edit=prezzo` e `?edit=preventivo` restano aperture con la riga;
- 5: errori di risorse del processo delle foto distinti dai rifiuti → D-48;
- 7: `POST /repairs/{id}/photos` (039) risponde 503 per un errore di risorse;
- 8: i 10 s di orologio con due figli si misurano alla T5;
- 9: la miniatura rimandata la fa la prima pagina che la chiede.
La 6 resta aperta fino alla misura della CPU alla T5. CPU sul runner della CI: slice 037e.

Domanda 15 della slice 038 (Stefano, 02/11/2026): il nome del file delle foto caricate dalla
scheda si ripulisce come nel caricamento JSON (`buste.PulisciNomeFile`, `clean_name/1`) → D-49
(RIP-S-29).

Domande 16 e 17 della slice 039 (Stefano, 02/11/2026): un guasto del server nel caricamento della
coda (disco che non scrive, errore del database) risponde 500 «Foto non salvata.»; «Foto rifiutata.»
resta per le foto non valide. La 16 come raccomandata (D-50, nota per lo standalone), la 17 diversa
dalla raccomandazione (era 422) → D-51 (RIP-S-35).
