# 013 — Pagina del registro attività (ADM-16), e messaggio D-32 senza indirizzo

- **Fonte:** Stefano, 30/10/2026: «012 ok. Via con la 013 (pagina del registro, ADM-16).» e «D-32
  confermata anche nella seconda parte: senza indirizzo_interno la pagina non crea l'invito né il
  nuovo link e dice cosa manca (stesso messaggio del reset-link: quale riga e in quale file).»
  Standalone a `go-baseline` = `6eb93ed`: `audit_log.ex` (`load/3`), `live/audit_live.ex`,
  `audit.ex`, `workshop/audit_event.ex`; test `audit_log_test.exs`, `audit_live_test.exs`.
- **Confine:**
  - `/audit` sulla faccia interna, solo titolare, ricontrollato a ogni richiesta;
  - 100 eventi per pagina dal più recente (`creato_il`, poi `id`, decrescenti); filtri esatti su
    attore e azione (senza spazi ai lati), periodo in giorni (default 30, massimo 365, confine
    compreso); pagina e `fino_a` come `Integer.parse` con resto vuoto, limitati all'ultima pagina
    e all'ultima riga;
  - scelte dei filtri tra i valori presenti in tutto il registro;
  - apertura scritta come `audit.viewed` (risorsa `audit`, id `global`, dati `{"filters": […]}`)
    nella stessa transazione, dopo la lettura: la pagina non la mostra; se la riga non si scrive
    la lettura non dà niente;
  - «Audit non disponibile.» se la lettura non riesce;
  - D-32: il rifiuto della pagina utenti senza `indirizzo_interno` dice quale riga e in quale file,
    con lo stesso testo di `reset-link` (`apparecchio.ComeConfigurare`).
- **Fuori:**
  - i link «Esporta audit CSV» e «Esporta istanza completa», che arrivano con ADM-18 e ADM-19;
  - la voce di menu «Registro attività», che arriva con il layout;
  - la conservazione di 5 anni (D-14).
- **Voci:** ADM-16 (e D-32 per ACC-07, ACC-08).
- **Livello:** completo (lettura con scrittura d'audit nella stessa transazione).

## Cosa c'è

- `internal/audit/consulta.go`: `Registro.Consulta`, cioè `load/3` con filtri, fotografia, pagine
  e riga dell'apertura.
- `internal/web/registro.go` e `pagine/registro.html`: la pagina.
  - Il modulo dei filtri è un GET su `/audit` con `actor`, `action`, `giorni`. Nello standalone
    l'evento `filter` faceva `push_patch` allo stesso indirizzo; qui un valore vuoto può restare
    nell'URL.
  - «Precedente» e «Successiva» sono link con filtri, pagina e `fino_a`, come `query/2`.
  - La query si legge come `Plug.Conn.Query`: «+» è uno spazio, un «%» illeggibile resta com'è,
    il «;» fa parte del valore, un campo ripetuto vale per l'ultimo.
- `Dipendenze.Consultazione`; `serve` passa lo stesso registro di `Audit` (`dipendenzeWeb`).
- D-32: `Dipendenze.Configurazione` (il percorso del file dell'apparecchio) e `facciaWeb.senzaIndirizzo`.
- Banco: scenario `registro.json`:
  - pagina del titolare;
  - prima apertura senza `audit.viewed`, seconda con;
  - pagina enorme e `fino_a` non numerico;
  - casi della query (`%zz`, `action[]`, NBSP, spazio em, `+`, campo ripetuto);
  - rifiuto del dipendente.
- Inventario:
  - D-32 confermata con la data;
  - D-33 proposta;
  - nota Go su ADM-16.

## Prove

- TDD: test del dominio e della pagina scritti prima, rossi con la sola firma (`da fare`) e senza
  la rotta (404). Faceva eccezione `TestConsultaSenzaAttore`, verde anche con la sola firma perché
  la firma restituiva un errore: lo provano le mutazioni. Test D-32 rosso prima del messaggio
  nuovo. Dopo la review, test della query rosso prima del parser.
- Misurato sul banco (non solo ragionato): lo standalone tratta `action=%zz` come il testo «%zz» e
  non mostra righe. Il Go con `url.Query` scartava la coppia e mostrava tutto: corretto. Gli altri
  casi della query erano già uguali.
- Mutazioni (in una copia): **57 uccise su 59 che compilano**. Vive:
  - saltare le coppie vuote della query: è equivalente, perché una chiave vuota non è un campo;
  - in `serve`, passare a `dipendenzeWeb` opzioni vuote invece di `o`. È la colla dentro `serve`,
    provata da `TestDipendenzeWeb` solo fino alla funzione; servirebbe un test che entra da `serve`
    con login.

  Le vive del primo giro hanno avuto il loro test: filtro per sola azione, campi ripetuti, percorso
  del file passato alle pagine. Il controllo del «-» in `interoPositivo` era ridondante ed è tolto.
- Banco **verde** con `fab-219` su un'istanza nuova: 9 scenari, 121 passi; spento, nessun avanzo.
- `scripts/check` verde con l'esito del banco: chiusa **ADM-16** (16 voci chiuse).

## Review indipendente

Nessun difetto ALTA o MEDIA. Verificato dal revisore:
- parsing e limiti come `Integer.parse`, confine del periodo, ordine, valori dei filtri;
- fotografia letta nella transazione;
- riga `audit.viewed` (attore, niente IP né user agent, esito, dati);
- chiusura quando la riga non si scrive;
- permessi a ogni richiesta, escape, SQL con parametri;
- UTF-8 non valido → 400.

Corretto:

- **BASSA:** query con un «%» illeggibile, ora letta come Plug (misurato sul banco, vedi sopra).
- **BASSA:** il log dell'errore ora porta l'errore.
- **Test:**
  - nel test del titolare retrocesso c'era una condizione che non poteva mai essere vera: ora
    conta le righe esatte;
  - aggiunti `fino_a` non valido nel dominio, attore oltre 255 caratteri, link con «&», «+» e
    spazi, stesso registro per `Audit` e `Consultazione`.

Dichiarato:

- **D-33 (proposta):** lo standalone, aperto da browser, scrive due `audit.viewed`, perché
  LiveView chiama `handle_params` al render statico e alla connessione. La seconda lettura mostra
  la riga della prima. Il Go ne scrive una. Non è misurato: il banco HTTP fa solo il render
  statico.
- **Compromesso:** lettura e riga stanno in una transazione sulla connessione di scrittura
  (`_txlock=immediate`), quindi le altre scritture aspettano la lettura. Il revisore ha misurato
  circa 180 ms con 300.000 righe; per un negozio sono numeri lontani. In cambio non c'è il BUSY
  del passaggio da lettura a scrittura.
- **Spazi ai lati dei filtri:** `strings.TrimSpace` toglie anche NBSP. Sul banco NBSP e spazio em
  davanti all'azione danno lo stesso esito nelle due.
- **D-32:** il testo web non ha la parte «oppure dai -url», che sul web non ha senso. Dopo aver
  scritto la riga serve riavviare `gestrip serve`, perché il file si legge all'avvio; il messaggio
  non lo dice, come quello di `reset-link`.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `d6982e8` (012).
