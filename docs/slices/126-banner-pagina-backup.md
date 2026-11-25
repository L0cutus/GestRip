# 126 — Banner rosso del titolare e pagina Impostazioni → Backup (T5.2, ottava slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 126, risposta **H** (parte banner: «banner rosso solo per il titolare finché il problema resta»), «Rischi»
  (banner nel banco); D-116 (media rovinati nell'allarme); note **8(a)**, **10(a)**, **10(b)**, **10(c)** di
  `docs/domande-aperte.md`; ricevute 124 e 125; ripresa `docs/handoff/2026-11-25-fine-122.md`.
- **Voci:** OPS-28 (parte: allarme, banner e pagina). Nessuna D nuova.
- **Dipendenze nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** da rigirare (comandi e atteso in fondo); non lanciato dall'implementatore.

## Cosa c'è

- `internal/backup/stato.go`: `Stato`, `Notturno.Stato`, `Stato.Problemi`, `Stato.Allarme`, `Problema` con codici
  fissi (`ProblemaNonConfigurato` … `ProblemaMediaRovinati`, per gli avvisi della 127), `EtaMassimaBackup` (26 ore),
  `ProssimoGiro`, `ControllaChiavetta`, `Notturno.ConLavoroSpento` (campo `lavoroSpento` in `notturno.go`).
- `internal/db/stato_backup.go`: `RigaRegistroBackup`, `UltimeVerificheBackup`, `IstantiRegistroBackup`.
- `internal/web/backup.go`: interfaccia `BackupDelNegozio` (era `BackupAMano`: ora anche `Stato`), pagina
  `GET /settings/backup` (`paginaBackup`, `datiPaginaBackup`), banner (`allarmeBackup`, `vistaAllarme`,
  `cacheAllarme`, `durataAllarme` 1 minuto); `internal/web/pagine/backup.html`; template `allarmeBackup` in
  `base.html`; `guscio.go` (scheda «Backup» in `schedeSoloGo`, `guscio.AllarmeBackup`); `salute.go` (cache per
  faccia); `statici/app.css` (`.allarme-backup`, `.backup-stato-ok`, `.backup-problemi`).
- `cmd/gestrip/main.go`: con `-senza-lavori-automatici` `notturno.ConLavoroSpento()`; l'avviso all'avvio dice anche
  «il banner dei backup è spento».
- Documenti: SVILUPPO (sezione nuova «Backup: banner e pagina Impostazioni → Backup», rotte, 124), PIANO (§2.5
  calendario), `docs/domande-aperte.md` (8(a) in parte, 8(b) aperta, 10(a) aperta, 10(b) e 10(c) chiuse).

### Scelte

- **Condizioni del banner** (`Stato.Problemi`, nell'ordine): senza `backup_chiavetta` **solo** «non configurato»
  (nessun backup e nessuna prova ne sarebbero solo la conseguenza); poi chiavetta che non passa i controlli della 123
  (assente/non montata, sola lettura, altro disco: tre testi); ultima esecuzione (a mano o automatica) `fallito` —
  un `avviso` (backup riuscito, errore dopo: rotazione) no, il backup è valido; nessun backup verificato sulla
  chiavetta, o l'ultimo catturato **più di 26 ore** fa (26 esatte no); un istante del registro nel futuro
  (8(a)); ultima prova fallita (`db.ProvaSettimanaleFallita`); prova mancante (10(c)); media rovinati (D-116). L'età
  conta solo la chiavetta: è il backup di D-11 e della prova; la cartella di `gestrip backup -cartella` non conta.
- **10(c), prova mai fatta:** «prova mancante» se l'ultima prova riuscita è finita più di 8 giorni fa
  (`EtaMassimaProva`, la stessa regola della risposta G), oppure se non c'è nessuna prova e il primo backup sulla
  chiavetta ha più di 8 giorni. Così un apparecchio nuovo non suona subito (la prima prova parte dopo il primo backup,
  Dubbio 3 della 125), ma un lavoro della prova che non parte mai si vede entro 8 giorni. Una prova fallita e vecchia
  dà solo «prova fallita».
- **8(a), istante nel futuro:** il banner e la pagina lo dicono (problema `orologio`, stesso controllo di
  `BackupVerificatoDopo`: cattura o verifica di qualunque riga dopo adesso) e dicono cosa fare (controllare data e
  ora; le cancellazioni restano ferme finché l'orologio non ci arriva; se manca molto, assistenza). **Niente via
  d'uscita scritta** (segnare la riga non valida): è una scrittura sul registro che sblocca le cancellazioni (D-11),
  fuori da una pagina in sola lettura; va in una slice sua con revisione. Nota 8(a) resta aperta per quella parte.
- **Costo e cache** (riviste dopo la revisione, vedi sotto): lo stato è ~7 letture piccole del database (righe per id o su indici, tabelle di una riga al
  giorno) più i controlli della chiavetta **senza aprirla**: `EvalSymlinks`, tre stat, `/proc/self/mountinfo`,
  statfs, access — niente lucchetto, niente letture o scritture di file sulla chiavetta. A ogni pagina sarebbe
  lavoro inutile e una chiavetta lenta rallenterebbe tutto: cache **per faccia** di 1 minuto (la granularità del
  lavoro notturno), con l'orologio della faccia (finto nei test), riletta se l'orologio torna indietro. La lettura
  ha un tempo massimo di 5 s e non usa la cancellazione della richiesta (un browser che chiude non lascia in cache
  un «non si legge»). La pagina Backup legge sempre fresco e rinnova la cache: sistemata la chiavetta, aperta la
  pagina, il banner sparisce subito. Il dipendente non legge mai lo stato.
- **Guasto:** stato illeggibile → banner «Lo stato dei backup non si riesce a leggere…» (l'allarme non tace per un
  errore); la pagina lo dice e mostra solo il pulsante. Nel registro del server solo il tipo dell'errore.
- **Dove compare:** solo `guscioPer` con superficie interna e utente titolare; le pagine pubbliche e quelle
  d'ingresso tornano prima. Non sulla pagina Backup (gli stessi problemi sono nel suo riquadro) né sulla firma
  (revisione). Un `<aside>` etichettato, senza `role="alert"` (revisione). Classe `.allarme-backup`, **non** `.flash`/`.flash-errore`: `app.js` (`rileggiPagina`) toglie e rimette i
  `body > .flash`, e il banco legge `visibile:.flash-errore@1`.
- **Pagina:** sola lettura, senza JavaScript (il pulsante è un modulo con CSRF); accessibilità come le altre:
  sezioni con `aria-labelledby`, liste come lo storico dei promemoria, badge `badge-ok/attesa/errore`. Mai percorsi:
  della chiavetta lo stato, dell'archivio solo il nome del file; il motivo delle esecuzioni è quello dello storico
  (può contenere il punto di montaggio: è la pagina del titolare, sulla faccia interna). Nomi dei media rovinati in
  un `<details>` (servono all'assistenza).
- **10(a):** la pagina **mostra** l'ora (`backup.ora`) ma non la scrive: la modifica non è chiesta. 10(a) resta
  aperta per chi farà quella pagina.
- **10(b):** chiusa (la pagina c'è, il messaggio di «Esegui adesso» si vede lì).
- **Comportamento nel banco (Rischi del piano; regola rivista dopo la revisione, scelta del coordinatore):** il Go
  del banco parte senza `backup_chiavetta` e con `-senza-lavori-automatici`. Senza niente, il titolare vedrebbe «non
  configurato» su ogni pagina confrontata. `serve` con `-senza-lavori-automatici` chiama
  `Notturno.ConLavoroSpento()` e in `Stato.Allarme()` **tacciono solo** «non configurato», «backup vecchio» e
  «prova mancante»; chiavetta, esecuzione fallita (anche da «Esegui adesso»), prova fallita, media rovinati,
  orologio e «non si legge» suonano. Nel banco c'è solo «non configurato» (controllato: senza `backup_chiavetta`
  `Problemi` dà solo quello), quindi il banner tace.
  Motivo: il banner sorveglia il **backup automatico**; con l'opzione il lavoro notturno e la prova non girano per
  scelta, quindi nessun backup è atteso e «nessun backup da 26 ore» sarebbe sempre vero per costruzione. Non nasconde
  niente in produzione: l'apparecchio non usa mai l'opzione (l'avviso all'avvio lo ripete, ora anche per il banner),
  e la pagina Backup mostra comunque i problemi con la riga «In questo avvio i lavori automatici sono spenti…». È la
  strada che il piano indicava («`-senza-lavori-automatici` o un'opzione simile»). Nessun cambiamento in
  `tools/banco`.
- **Scheda «Backup»** nelle impostazioni, dopo «Stampanti» (le schede solo Go vanno dopo quelle dello standalone):
  compare su tutte le pagine delle impostazioni del Go, come «Stampanti».

## Prove (dati inventati, orologio finto, chiavetta finta della 123)

- `internal/backup/stato_linux_test.go`: `TestProblemi` (23 casi con i confini: 26 ore esatte / +1 s / 26 h 30
  scritte a numero, nessun backup e il suo testo, orologio esatto / +1 s, prova 8 giorni esatti / +1 s, mai fatta con
  il primo backup a 8 giorni / +1 s, fallito prima di un riuscito, avviso, chiavetta in tre modi, tutto insieme;
  testi senza percorsi; con il lavoro spento allarme vuoto e problemi uguali), `TestProssimoGiro` (prima, esatto,
  dopo, capodanno, ora legale, ora solare: la prima 02:30), `TestStatoVero` (database vuoto, backup vero, prova,
  esecuzione fallita, riga nel futuro, chiavetta tolta, lavoro spento, non configurato, database chiuso).
- `internal/db/stato_backup_test.go`: `TestLettureStatoBackup` (vuoto, ordine e campi, primo sulla chiavetta senza
  fallite né cartelle, istante più avanti anche da una verifica e da una riga fallita, database chiuso).
- `internal/web/backup_pagina_test.go`: `TestBannerBackupPerCondizioneERuolo` (ogni condizione: banner del
  titolare su `/`, `/settings/utenti`, `/audit` con il suo testo e il link; mai per il dipendente su `/` e
  `/nuova`; la pagina Backup ha i problemi e non il banner; stato sano: niente banner), `TestBannerBackupLavoroSpentoEGuasto`,
  `TestBannerBackupMaiFuoriDallePagineInterne` (faccia pubblica, `/login`, `/reset-request`),
  `TestBannerBackupCache` (una lettura al minuto, scadenza, orologio indietro, la pagina rinnova, il dipendente non
  legge), `TestPaginaBackupConDati` (tutte le sezioni, CSRF, scheda aperta, niente percorsi, motivo escapato),
  `TestPaginaBackupSenzaDati`, `TestPaginaBackupRuolo`.
- `internal/web/backup_test.go`: «Esegui adesso» ora controlla il messaggio **sulla pagina** (10(b)); senza backup
  nelle dipendenze anche la pagina risponde con l'errore e non c'è banner. `ruoli_test.go`: riga nuova
  `GET /settings/backup` (soloTitolare, OPS-28). `guscio_test.go`: schede e rotte con il guscio con «Backup».
- `cmd/gestrip/lavori_test.go` (`TestServeBackupNotturno`): con `-senza-lavori-automatici` lo stato della faccia ha
  `LavoroSpento` e l'allarme vuoto, senza l'opzione l'allarme è acceso (chiavetta non montata).
- Rossi visti: attese dei test del guscio (scheda e rotta nuove), l'ora solare in `ProssimoGiro` (Go sceglieva la
  seconda 02:30: corretto, prende la prima), un nome di aiuto doppio nei test, l'orologio indietro di un'ora nel test
  della cache (scadeva la sessione: portato a 10 s), gofmt.
- **Mutazioni a mano** (`to-be-deleted-after-use/_slice-126/muta.py`, `muta.log`, `muta-rifatte.log`), 19, tutte
  compilano. Primo giro **17 su 19**: sopravvissute M4 (26 → 27 ore: i test usavano la costante) e M5 (nessun backup
  senza allarme: equivalente, ricadeva su «più vecchio di 26 ore» con l'ora zero). Aggiunti il caso «26 ore e mezza»
  a numero e il controllo del testo «nessun backup»: **M4 e M5 uccise**, totale **19 su 19**. Elenco: M1 pagina
  `personale`; M2 banner per tutti i ruoli; M3 26 ore con `>=`; M4 27 ore; M5 nessun backup senza allarme; M6
  fallito guardando il più vecchio; M7 avviso come fallito; M8 chiavetta ignorata; M9 orologio ignorato (un'ora di
  tolleranza); M10 prova fallita ignorata; M11 prova mai fatta ignorata; M12 un media rovinato ignorato; M13 lavoro
  spento che non spegne; M14 non configurato senza allarme; M15 guasto senza banner; M16 cache mai scaduta; M17
  orologio indietro ignorato dalla cache; M18 pagina che non rinnova la cache; M19 `serve` che non passa il lavoro
  spento.

## Revisione (Opus indipendente, ~75k token, ~35 minuti, 14 mutazioni, 11 uccise; `-race` ok su backup e sui test toccati di web)

Nessun ALTA. Corretti tutti i MEDIA e i BASSA:

- **MEDIA 1, banner sulla firma:** il titolare passa il tablet al cliente su `/repairs/{id}/firma`. Ora
  `senzaAllarmeDelModello` (`backup`, `firma`) in `guscio.go`; controllate le altre pagine con il guscio: nessun'altra
  va al cliente (l'anteprima del cliente non ha guscio). Test `TestBannerBackupNonSullaFirma` (firma senza, scheda
  della stessa busta con).
- **MEDIA 2, chiavetta appesa:** `ControllaChiavetta` non ha contesto (syscall). Ora la lettura dello stato gira in
  una goroutine (`avviaLettura`), **una sola alla volta** per faccia, aspettata al più `tempoStatoBackup` (5 s):
  scaduto il tempo «non si legge» (fail-closed); le richieste che arrivano intanto non aspettano e mostrano l'ultimo
  banner o «non si legge»; a lettura finita il risultato va in cache. Test `TestBannerBackupLetturaAppesa` (lettura
  finta appesa: banner e pagina «non si legge», una sola lettura, poi la cache).
- **MEDIA 3, lavoro spento:** regola del coordinatore (sopra, «Comportamento nel banco»); testo dell'avviso all'avvio
  e della pagina aggiornati («il banner non segnala backup mancanti o vecchi»); stato illeggibile suona anche con i
  lavori spenti. In lista per la guida della T5.4: l'apparecchio non usa mai l'opzione (nota 11(a)).
- **BASSA, `</section>` in più in `backup.html`:** verificato, non è in più: chiude `<section id="settings-panel">`
  aperta dal template `schede`, come `stampanti.html`, `utenti.html`, `negozio.html`, `provider.html`, `testi.html`.
  Ora lo prova `TestBannerBackupForma` (aperte = chiuse nella pagina resa). È `promemoria.html` a non chiuderla
  (nota 11(c), non toccata).
- **BASSA, `role="alert"` e `<h2>` prima dell'`<h1>`:** il banner è un `<aside>` etichettato
  (`aria-labelledby`), senza ruolo, con il titolo in un paragrafo.
- **BASSA, testo della chiavetta:** «non c'è» solo per `ErrNonMontata`/`ErrDestinazione`; un errore della cartella dei
  dati o della tabella dei montaggi dice «problema dell'apparecchio, non della chiavetta: chiedi assistenza».
- **BASSA, «Motivo» grezzo:** in lista per la 127 (nota 11(b)): gli avvisi usano `Problema.Testo`, mai il motivo.
- **Mutazioni sopravvissute del revisore, rifatte contro i test nuovi** (`_slice-126/muta-revisione.log`): R2
  (lettura con la cancellazione della richiesta: `TestBannerBackupRichiestaCancellata`), R10 (pagina illeggibile
  senza la nota dei lavori spenti), R13 (banner con `.flash`/`.flash-errore`: `TestBannerBackupForma`): **uccise 3 su
  3**. Mutazioni delle correzioni (`muta-revisione-1.log`, `-2`, `-3`): N1 banner sulla firma, N2 niente
  single-flight, N3 nessun tempo massimo (rifatta compilabile, `muta-revisione-4.log`: la prova si blocca, uccisa dal `-timeout`
  del test), N4 con una lettura in
  corso si aspetta (sopravvissuta al primo giro: l'esito era lo stesso, cambiava solo l'attesa; aggiunto il controllo
  che la seconda pagina non aspetti, poi uccisa), N5 lavoro spento che zittisce tutto, N6 che non zittisce niente, N7
  guasto zittito con i lavori spenti, N8 errore dei dati detto come chiavetta assente: **uccise 8 su 8**. Totale
  della revisione 11 su 11.
- `-race` dell'implementatore dopo le correzioni: `internal/web -run Backup` e `internal/backup -run
  'Problemi|StatoVero'` puliti.

## Segno di copertura

**Nessun `// inventario:`.** OPS-28 (notturni fuori sede, conservazione, allarme, prova settimanale) chiude con 127
(avvisi Telegram/email dell'allarme, l'altra metà della risposta H) e 128 (copia fuori sede): la 126 fa solo il
banner. I test portano «Voce OPS-28, in parte».

## Per il manuale

- Se qualcosa non va con i backup, il titolare vede in cima a ogni pagina un riquadro rosso «⚠ Backup: c'è un
  problema» con cosa fare; il personale non lo vede. Il riquadro resta finché il problema non è sistemato.
- I casi: backup non configurato; chiavetta tolta o non usabile; ultimo backup non riuscito; nessun backup buono
  da più di 26 ore; orologio dell'apparecchio sbagliato; prova di ripristino non riuscita o non fatta da più di 8
  giorni; foto o file rovinati sul disco.
- Impostazioni → Backup: stato, ora del backup notturno e prossimo backup, «Esegui adesso», gli ultimi backup con
  l'esito, i backup verificati con la destinazione, l'ultima prova di ripristino.
- Dopo aver sistemato (per esempio rimessa la chiavetta) basta aprire la pagina Backup: il riquadro rosso si
  aggiorna subito (altrimenti entro un minuto).

## Differenze dallo standalone

- Lo standalone non ha né banner né pagina dei backup: l'allarme delle 26 ore lo fa otto-vps fuori dal pacchetto.
  Pagina, banner e scheda «Backup» sono solo del Go.

## Dubbi

1. **«Avviso» senza banner:** un backup `avviso` (riuscito, errore dopo, per esempio la rotazione) non accende il
   banner; se la rotazione fallisce ogni notte la chiavetta si riempie e allora il backup fallisce e il banner
   suona. Se Stefano lo vuole subito, è una riga in `Problemi`.
2. **Testo della prova fallita:** dice «la prova si ripete da sola la domenica notte»; il titolare non ha un
   pulsante per rifarla (c'è solo `gestrip prova-backup` da console). Un pulsante «Rifai la prova» sarebbe facile
   (stessa forma di «Esegui adesso») ma non è chiesto.
3. **Motivo nella pagina:** il motivo di un'esecuzione fallita è quello dello storico e può contenere il punto di
   montaggio della chiavetta. Sta solo nella pagina del titolare (faccia interna); il banner non ha mai percorsi.
4. **gopls:** `goToImplementation` su `BackupDelNegozio` (una sola implementazione vera, `Notturno`, più il finto
   dei test) è servito a controllare che il cambio d'interfaccia non lasciasse indietro nessuno; `findReferences` e
   `incomingCalls` su `allarmeBackup` hanno risposto con errori di posizione (indice vecchio dopo le modifiche):
   chiamanti controllati con grep (`guscioPer` soltanto). Il resto con grep e lettura.

## Difetti trovati per strada (solo in lista)

- Nota **8(b)** (righe doppie della pulizia dei media con un errore del controllo) era segnata «126, insieme
  all'allarme» ma non è nel brief: non toccata, resta aperta.
- Nessun altro nel codice toccato.

## Check

- Dopo la revisione: primo giro **rosso** su `TestNessunOrologioFuoriDaClock` (avevo usato `time.NewTimer` per
  l'attesa: ora `context.WithTimeout`, come il resto del pacchetto); poi **verde**, copertura invariata (voci 353: chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11; log
  `_slice-126/check2.log`).
- `scripts/check` senza banco (implementatore): primo giro **rosso** solo su gofmt (due file), dopo `gofmt -w`
  **verde**; copertura «voci 353: chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11» (invariata: nessun segno
  nuovo). Log in `to-be-deleted-after-use/_slice-126/check.log`.

## Banco

Rigiro di controllo su pc-fisso, con il «memoria libera» del coordinatore e la guardia sotto 10 GB (mai scattata,
minimo 19 GB), immagine `gestrip-standalone:fab-225`, dati `sintetici-import`.

- **s126a:** HTTP verde (91 scenari, 0 inattese); browser rosso sulla nota 8: 66 inattese e 1 errore, tutte in
  `raccolta-esclusioni[0]`, dove la standalone va in timeout e risulta «(assente)»; il Go dà i valori attesi.
- **s126b** (giro identico): **verde**. HTTP 91 scenari, 1571 differenze attese, 0 inattese, 0 errori; browser 38
  scenari, 49 attese, 0 inattese, 0 errori. `scripts/check` con l'esito: verde, «voci 353: chiuse 229, rosse 0,
  parziali 16, aperte 97, fuori v1 11».
- Il banner (`backup-alarm`) non compare in nessuna risposta salvata del Go.
- In lista: nel registro del Go, in tutti e due i giri, una riga `level=ERROR msg="foto non letta"` con 500 su
  `GET /repairs/{id}/photos/{foto}` (`internal/web/foto.go:57`, non toccato dalla 126); c'è anche nel giro verde,
  non è la D-100 (quella riguarda solo le facce pubbliche): in lista come nota 12 di `docs/domande-aperte.md`.
