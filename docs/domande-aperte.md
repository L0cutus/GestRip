# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

Slice 037b (risposte 1-14 nel codice). Nessuna blocca il lavoro.

1. **Rifiuti senza riga propria e D-44 (RIP-S-01).** La risposta diceva che anche dopo un rifiuto
   la scheda non scrive `repair.viewed`, «perché l'azione ha già la sua riga». I rifiuti però non
   scrivono audit («Azione non valida.», stato uguale D-45, «Transizione non consentita.», rifiuto
   al dipendente, nuovo link senza indirizzo D-46, modulo ridisegnato dopo un errore): un POST
   rifiutato apposta, con un token CSRF preso da un'altra pagina, mostrerebbe la scheda senza
   traccia. **Fatto nella 037b, da confermare:** ogni modulo della scheda porta il segno monouso
   della pagina (come «Modifica» e «Annulla»); un rifiuto da una pagina aperta con la sua riga non
   scrive niente, come deciso; un rifiuto senza quel segno conta come apertura e scrive la riga.
   **Raccomandazione:** tenerlo così.
2. **SVG nella D-47 (RIP-S-33).** Il ripiego sul tipo della riga esclude anche `image/svg+xml`, che
   il browser apre come documento con script: è più stretto di «solo `image/*`».
   **Raccomandazione:** tenerlo (nessuna scrittura mette mai un SVG).
3. **Durate dei segni della scheda.** Segno del redirect di un'azione 2 minuti, segno dei link e
   dei moduli della pagina 1 ora, al più 10 000 segni in memoria (scelte tecniche). Oltre l'ora, o
   dopo un riavvio, «Annulla» o un rifiuto scrivono la riga come un'apertura. **Raccomandazione:**
   accettarlo.
4. **`?edit=prezzo` e `?edit=preventivo`** (dal popup QR della home, T4.2) restano aperture con la
   riga: arrivano da un'altra pagina. **Raccomandazione:** accettarlo.

Slice 037c (CI instabile, errori di risorse del processo delle immagini). Nessuna blocca il lavoro.

5. **Errori di risorse non sono rifiuti (RIP-S-29, 30, 33, 34): deviazione da registrare.** Nello
   standalone un figlio fermato dal tempo di 10 s o caduto per i limiti di memoria o CPU esce come
   rifiuto (`:invalid_image`, «Foto rifiutata o non salvata.», 422; per la miniatura
   `:thumbnail_failed`). Nel Go, come chiesto, è un errore a parte: l'utente vede «Foto non
   elaborata adesso: riprova tra poco.», il registro la causa (tempo, cpu, segnale, memoria, avvio,
   uscita); la miniatura ripiega sulla foto intera come prima e non lascia niente in cache.
   **Raccomandazione:** approvarla come D-48.
6. **Memoria e CPU danno anch'esse «riprova»,** anche se per la stessa foto si ripeterebbero.
   Misurato: con le foto che si accettano il tetto ha almeno 125 MiB di margine e la CPU usata è
   circa 2,5 s su 8. **Raccomandazione:** tenerlo così.
7. **Codice HTTP di `POST /repairs/{id}/photos` (slice 039) per questo errore.** **Raccomandazione:**
   503 (riprovabile), invece del 422 dei rifiuti.
8. **I 10 s di orologio con due figli insieme sull'apparecchio** vanno misurati alla T5, accanto alla
   RAM (nota già nel piano). **Raccomandazione:** misurarli lì, senza cambiare ora.
9. **Miniatura rimandata dal riscaldatore all'avvio:** non si rimette in coda, la fa la prima pagina
   che la chiede. **Raccomandazione:** accettarlo.

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
