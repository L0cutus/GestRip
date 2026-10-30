# 011b — `gestrip reset-link` con l'indirizzo interno dell'apparecchio (T1)

- **Fonte:** Stefano, 30/10/2026, sui due punti aperti dalla review della 011: «gestrip reset-link
  deve prendere l'indirizzo interno dalla configurazione dell'apparecchio; -url solo per
  sovrascriverlo. Il comando stampa anche l'avviso che una nuova richiesta dalla pagina annulla
  questo link. L'annullamento da pagina resta come lo standalone.» Nello standalone il link si
  costruisce sull'host configurato della macchina (`PHX_HOST`, `PHX_URL_PORT` in
  `config/runtime.exs`, porta 443 omessa), mai sull'`Host` della richiesta (ACC-13).
- **Confine:**
  - l'indirizzo della faccia interna sta nel **file di configurazione della macchina**
    `/etc/gestrip/apparecchio.conf` (`-config` per un altro percorso), riga
    `indirizzo_interno = https://host[:porta]`, con le regole di forma della 011 (https, host
    valido, porta 1-65535, niente percorso né query); la porta 443 si omette come nello
    standalone. Lo scrive l'installer (T5); fuori dal database, quindi fuori dal backup;
  - `gestrip serve` legge il file prima di toccare il database: un file rovinato (chiave
    sconosciuta o ripetuta, riga senza `=`, indirizzo sbagliato) ferma l'avvio; uno che manca no;
  - `gestrip reset-link` lo legge; `-url` lo sostituisce solo per quel link. Senza indirizzo e
    senza `-url` è un errore che dice cosa scrivere e dove, senza creare il link;
  - il comando stampa che una nuova richiesta dalla pagina «Password dimenticata?» annulla il
    link. L'annullamento resta come lo standalone.
- **Fuori:** l'indirizzo pubblico (arriva con le rotte pubbliche che fanno link); la scrittura del
  file da parte dell'installer (T5).
- **Voci:** ACC-13 (resta parziale: canali).
- **Livello:** completo (link di autenticazione).

## Cosa c'è

- `internal/apparecchio`: `Leggi`, `IndirizzoInterno`, `ControllaIndirizzo` (spostata da
  `cmd/gestrip`, con la porta 443 omessa e la porta riscritta senza zeri davanti).
- `cmd/gestrip`: `-config` su `serve` e `reset-link`; `-url` facoltativo; avviso
  sull'annullamento dalla pagina.

## Prove

- TDD, primo giro: test dell'impostazione nel database e del comando rossi in compilazione, poi
  verdi. Dopo la review l'indirizzo è passato dal database al file: i test del file e di `serve`
  sono stati scritti **insieme** al codice nuovo, non prima; li controllano le mutazioni qui sotto.
- Mutazioni (sul codice finale, in una copia): **25 uccise su 27 che compilano**. Vive due,
  equivalenti: `u.RawQuery != ""` (una query ha sempre `?`, già rifiutato da `ContainsAny`);
  l'errore di `bufio.Scanner` (il file è al massimo 16 KB e si legge dalla memoria: non può
  fallire). Le prime vive (porta 65536, file al limite) hanno avuto il loro test.
- `scripts/check` verde. Banco non rifatto: la slice non tocca le rotte.

## Review indipendente

Nessun problema alto. Verificato dal revisore: `-url` controllato prima di aprire il database;
indirizzo mancante o rovinato scoperto prima di creare il link (test che contano le righe di
`reset_password`); il token solo sul terminale, non negli errori nuovi; l'avviso c'è e
l'annullamento dalla pagina è rimasto. Corretto:

- **MEDIA:** l'indirizzo nel database (scritto da `serve -indirizzo-interno`) viaggiava con il
  backup: dopo un ripristino su un'altra macchina, o in una prova isolata, il link avrebbe puntato
  all'host vecchio; due fonti (unit e configurazione guidata) avrebbero scritto la stessa chiave.
  Ora sta nel file della macchina, come `PHX_HOST` nello standalone.
- **MEDIA:** il messaggio di errore suggeriva di lanciare un secondo `serve` sul database del
  servizio acceso. Ora dice quale riga scrivere e in quale file.
- **BASSA:** `:443` e `:0443` restavano nel link; ora si omettono come nello standalone.

Dichiarato, come nella 011: il link si salva prima di stamparlo; se la scrittura sul terminale
fallisce resta un link valido mai stampato (e quelli aperti sono già annullati). Scade da solo in
30 minuti e il comando si rilancia.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `e7272c5` (ripresa della 011).
