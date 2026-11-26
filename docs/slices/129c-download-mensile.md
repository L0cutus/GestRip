# 129c — Prova mensile della copia remota: download e verifica intera (T5.2, tredicesima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  risposta **E** («una volta al mese un download completo di notte con la verifica intera») e «Rischi»;
  `docs/domande-aperte.md` nota 13(c); ricevute 125 (prova settimanale: registro, calendario, recupero, tentativi),
  128 (copia remota, sorvegliante, codici), 129 (pagina); ripresa `docs/handoff/2026-11-25-fine-126.md`. Decisione
  tecnica del coordinatore: download dal server SFTP con verifica completa in uno spazio temporaneo locale.
- **Voci:** OPS-28 (**chiude**, vedi «Segno di copertura»). Nessuna D nuova.
- **Dipendenze nuove:** nessuna. **Migrazioni nuove:** **0025** `prove_remote`.
- **Modelli:** implementatore Opus.
- **Banco:** non serve (vedi in fondo).

## Cosa c'è

- `internal/backup/prova_remota.go` (nuovo): `ProvaRemota` (`Esegui`), `EsitoProvaRemota`, `MeseDi`,
  `EtaMassimaCopiaProvata` (8 giorni), `EtaMassimaProvaRemota` (36 giorni), i codici nuovi `CodiceRemotoNessuna`,
  `…Vecchia`, `…Orologio`, `…Assente`, `…Scaricamento`, `…Verifica`, gli errori `ErrProvaRemota…`,
  `DettaglioPanicoProvaRemota`, `Remoto.scarica` (download con lo SHA-256 e il tetto ai byte), e sul `Notturno`
  `ProvaRemotaDovuta`, `GiroProvaRemota`, `ProvaRemotaAMano`, `provaRemotaProtetta`.
- `internal/backup/notturno.go`: `Lavoro` fa backup → prova settimanale → prova remota nello stesso giro;
  `inserisciProvaRemota` iniettabile. `lucchetto.go`: `.scarica-backup-<cifre>` fra i resti che `PulisciResti` toglie.
- `internal/backup/stato.go`: `Stato.ProvaRemota`/`CeProvaRemota`, problemi **`prova_remota_fallita`** e
  **`prova_remota_mancante`** (`problemiProvaRemota`, `testoProvaRemota`; `remotoVecchio` estratto senza cambiare la
  regola). `avvisi.go`: i due titoli, `chiudiRemotoSpento` chiude anche questi, `ordineCodice` ha i codici remoti.
- `internal/db/prove_remote.go` e `migrazioni/0025_prove_remote.sql`: `ProvaRemota`, `RegistraProvaRemota`,
  `TentativiProvaRemota`, `UltimaProvaRemota`, `CodiciProvaRemota`.
- `cmd/gestrip/backup.go` e `main.go`: **`gestrip prova-backup -remota [-db FILE]`** (alternativo a `-chiavetta`).
- `internal/sftpfinto/server.go`: guasti delle letture `LetturaCadeDopo`, `FermoAllaLettura`, `AllaPrimaLettura`.
- `internal/importer/mappa.go`: `prove_remote` in `TabelleSoloGo`, il suo indice in `IndiciSoloGo`; test adattati
  (`schema_test.go` 25 migrazioni, `buste/giri_test.go` la 0025 nel ritorno indietro).
- Documenti: SVILUPPO (sezione nuova «Backup: prova mensile della copia remota»), PIANO (§8.2 n. 15 e T5.2),
  `docs/domande-aperte.md` nota 13(c) **chiusa**.

### Scelte

- **Calendario: la prima domenica del mese, nello stesso giro, dopo il backup e dopo la prova settimanale.** Il mese
  di una prova è il mese della domenica della settimana del turno (`MeseDi` = mese di `SettimanaDi`): dal primo del
  mese al sabato prima si è ancora nel mese precedente. Perché: la domenica il negozio è chiuso e la notte c'è già il
  lavoro pesante della settimana; il download (~3,6 GB) e la seconda verifica stanno dopo la prova della chiavetta,
  con lo stesso lucchetto, senza sovrapporsi; e si riusa il calendario a settimane della 125 senza un'altra regola
  di date (un giorno fisso tipo «il 1°» cadrebbe in un giorno di negozio aperto). La prova è dovuta quando il turno
  del backup **e** la prova settimanale della settimana sono chiusi (riusciti o con i 3 tentativi finiti): una prova
  settimanale che fallisce non blocca il mese.
- **Recupero e tentativi:** spento la prima domenica, il mese resta da fare e la prova parte al primo giro utile
  (dopo il backup e la prova settimanale di recupero), fino alla prima domenica del mese dopo. Tentativi come 124 e
  125 (`daRifare`: al più 3 per mese, a 30 minuti, fino alla prima riuscita; le prove a mano non contano; righe non
  scritte contate in memoria).
- **Solo con la copia remota configurata** (e la chiavetta: senza, il lavoro notturno non parte e non ci sono copie).
  Con la configurazione scritta da meno di 26 ore e nessuna copia registrata la prova aspetta: altrimenti configurarla
  la domenica mattina, dopo il giro delle 02:30, farebbe fallire subito la prova con «nessuna».
- **Lucchetti:** solo quello dei backup, preso come la 124/125 (occupato: niente, riprova al minuto). La chiavetta non
  serve. Il download non scrive mai nel registro dei backup (D-11 invariato: `TestProvaRemotaDelMese` conta le righe)
  e non tocca né la rotazione locale né i file del server.
- **Mai fermare l'app:** `provaRemotaProtetta` (panico recuperato) e `eseguiProvaRemota` (panico = prova fallita
  `altro` con `DettaglioPanicoProvaRemota`); lo stop di serve chiude la connessione tramite il sorvegliante della 128
  (anche durante il download) e non lascia righe; la cartella temporanea si toglie con un `defer` che gira anche con
  il panico e lo stop; dopo un blackout la toglie `PulisciResti` (nome `.scarica-backup-<cifre>`).
- **Registro: tabella nuova `prove_remote` (0025)**, non `prove_ripristino` con un tipo. Motivo: `gopls references` su
  `db.UltimaProvaRipristino` dà i lettori della 126 (`stato.go:131`, `ProvaSettimanaleFallita`) e del calendario
  (`TentativiProva`): con le due prove in una tabella ognuno andrebbe filtrato, e un filtro dimenticato farebbe
  spegnere l'allarme della chiavetta da una prova remota riuscita; in più la prova remota vuole un mese, i byte e un
  **codice fisso** (gli avvisi fanno il testo dal codice: il motivo grezzo può avere la cartella del server), che
  `prove_ripristino` non ha, e il suo CHECK non si cambia senza ricostruire la tabella.
- **N giorni = 8** (`EtaMassimaCopiaProvata`): la copia scaricata fallisce se è catturata più di 8 giorni prima della
  prova (8 esatti passano), come la prova settimanale: la domanda è la stessa («da quanto vecchio ripartirei?»). Non
  26 ore: quelle le controlla già ogni giorno `remoto_vecchio`, e una sola notte senza rete farebbe fallire anche la
  prova del mese. Nessuna copia registrata (o l'ultima in un'altra cartella) fallisce con `nessuna`.
- **Prova mancante: 36 giorni** (`EtaMassimaProvaRemota`) dall'ultima prova (automatica o a mano), o dalla
  configurazione se non ce n'è mai stata una: 5 settimane (il massimo fra due prime domeniche) più un giorno, come
  8 = 7 + 1 della settimanale e 26 = 24 + 2 del backup (coerente con la 10(c): un apparecchio appena configurato non
  suona). Tace con `-senza-lavori-automatici` come `prova_mancante`; `prova_remota_fallita` invece suona (guasto
  vero, come `prova_fallita`).
- **Codici:** problemi fissi `prova_remota_fallita` e `prova_remota_mancante`; nel registro il motivo ha uno dei 15
  codici di `db.CodiciProvaRemota` e `testoProvaRemota` ha un testo per ciascuno, senza host, utente né cartella
  (provato su tutti). Copia remota spenta: i due problemi si chiudono in silenzio come `remoto` e `remoto_vecchio`
  (scelta della revisione 128).
- **Controlli in ordine:** configurazione → copia del registro nella cartella configurata → età → chiave → spazio
  locale (byte + 10 GiB di `MargineSpazio`, come backup e verifica) → collegamento (impronta, chiave) → cartella →
  tipo e dimensione sul server (`controllo` prima di scaricare) → download con lo SHA-256 (`WriteTo`, letture in
  parallelo; tetto ai byte del registro) → byte e SHA-256 contro il registro → verifica completa (`verifica`; un
  `ErrSpazio` dell'estrazione resta `spazio`) → SHA-256, byte e ora di cattura della verifica contro il registro.
  Disco pieno durante il download: `spazio`. Tempo massimo: quello della copia (6 ore), più i 2 minuti senza
  risposte.
- **Comando:** `gestrip prova-backup -remota`, la stessa prova con la riga «a mano» (spegne `prova_remota_fallita`,
  non conta per il calendario), esce con 1 se non riesce; senza copia remota configurata non scrive niente e lo
  dice. `-remota` e `-chiavetta` insieme, o nessuno dei due, sono un errore d'uso.

## Prove (server SFTP finto, niente rete, niente credenziali vere)

`internal/backup/prova_remota_linux_test.go` (backup, copia, verifica, registro e lucchetto veri, orologio finto):

- `TestMeseDi`: primo del mese di martedì, prima domenica, fine anno, primo del mese di domenica.
- `TestProvaRemotaDovuta` (**calendario e recupero**): 02:29 della prima domenica (mese prima, fatto), prima del
  backup, prima della prova settimanale, prova settimanale a mano non basta, dovuta dopo; prova remota a mano non
  conta; 30 minuti e 3 tentativi; sabato 02/01 ancora dicembre; spento la prima domenica di gennaio e recupero di
  lunedì con la settimanale fallita tre volte; **non configurata** e senza chiavetta mai; configurata da 26 ore senza
  copie no, un minuto dopo sì.
- `TestProvaRemotaDelMese` (**`// inventario: OPS-28`**): il lavoro vero la prima domenica fa backup con copia, prova
  settimanale e prova remota nello stesso giro; riga `ok` con la copia del registro, niente temporanei, lucchetto
  libero, nessun problema, registro dei backup invariato, server non toccato, un'ora dopo non si rifà.
- `TestProvaRemotaFallisce`: nessuna copia; **non configurata** (niente riga); vecchia (8 giorni e 1 s; 8 esatti
  passano); nel futuro; tolta dal server (`assente`); **byte diversi** (`controllo`, visto dalla dimensione sul
  server); **archivio corrotto sul server** (stessi byte, SHA-256 diverso: `controllo`); **download interrotto**
  (connessione che cade dopo 200 byte: `scaricamento`); **server lento con tempo scaduto** (fermo alla lettura: 2
  minuti senza risposte a 300 ms → `rete`, `ErrTempoRemoto`; durata massima a 300 ms → `rete`); impronta diversa;
  chiave rifiutata; **spazio locale insufficiente** (`spazio`, prima di collegarsi); archivio corrotto registrato così
  (`verifica`); ultima copia in un'altra cartella (`nessuna`). Ogni volta: niente temporanei, lucchetto libero,
  motivo senza host né utente, meno di 10 s. Stato con `prova_remota_fallita` e testo senza host né cartella; una
  prova a mano riuscita lo spegne.
- `TestProvaRemotaStopPanicoLucchetto`: **stop durante il download** (server fermo alla prima lettura, stop: ritorna
  subito con il contesto, senza aspettare i 2 minuti), stop durante la verifica (il file scaricato c'era): nessuna
  riga, temporanei tolti, mese ancora da fare; lucchetto occupato: niente; panico nella verifica con un
  `.scarica-backup-12345` rimasto da un blackout: riga fallita `altro`, cartelle tolte; righe non scritte contate in
  memoria (3 tentativi in tutto).
- `TestProblemiProvaRemota`: confini dei 36 giorni (esatti tacciono), mai fatta contata dalla configurazione,
  fallita, non configurata, lavoro spento (mancante tace, fallita suona), testi di tutti i codici, titoli e ordine
  degli avvisi, copia spenta che chiude i problemi.
- `internal/db/prove_remote_test.go` `TestRegistraProvaRemota`: 19 righe rifiutate, rilettura, motivo lungo,
  tentativi per avvio e mese, CHECK da solo.
- `cmd/gestrip` `TestProvaBackupRemotaComando`: opzioni; non configurata (niente riga); backup vero messo sul server
  finto e registrato come copia: prova riuscita («a mano»), poi copia rovinata (`controllo`, «NON RIUSCITA», riga
  fallita, niente temporanei).
- `-race` (`CGO_ENABLED=1 -tags nodynamic`) su `internal/backup` e `internal/sftpfinto`: pulito (82 s).
  Log `to-be-deleted-after-use/_slice-129c/race.log`.

### Mutazioni a mano (`to-be-deleted-after-use/_slice-129c/muta.py`, `muta.log`, `muta-2.log`, `muta-3.log`)

19 mutazioni sulle guardie, ognuna con il test mirato e il file rimesso identico (md5). **19 su 19 uccise** dopo due
test rafforzati e una mutazione riscritta:

- Primo giro: sopravvissute **M4** (niente controllo della cartella della copia: nessun test aveva una copia in
  un'altra cartella → aggiunto il caso) e **M7** (niente controllo della dimensione sul server: serratura doppia, il
  download con il tetto e il confronto dei byte la prendevano dopo → il test ora vuole che la differenza si veda
  prima di scaricare, dal motivo «sul server»); **M6** non compilava (variabile inutile: riscritta); **M11** era
  scritta male (la sostituzione dava lo stesso risultato: riscritta sulla condizione).
- Elenco: M1 età non controllata; M2 età con `>=`; M3 cattura nel futuro accettata; M4 cartella della copia; M5
  spazio locale; M6 SHA-256 del download; M7 dimensione sul server; M8 cartella temporanea non tolta; M9 verifica non
  fatta; M10 stop registrato come prova fallita; M11 senza aspettare la prova settimanale; M12 calendario con le
  prove a mano; M13 niente attesa della prima copia; M14 mancante con `>=`; M15 mancante che suona con il lavoro
  spento; M16 copia spenta che non chiude i problemi della prova; M17 `.scarica-backup-` non pulita dopo un
  blackout; M18 registro che accetta un mese sbagliato; M19 comando con `-remota` e `-chiavetta` insieme.
- Non coperto da un test (guardia difensiva): il tetto ai byte del download (`ErrProvaRemotaTroppoGrande`) scatta solo
  se il server manda più byte di quelli dichiarati da `Lstat`, cosa che il server finto non fa.

## Revisione (Opus indipendente, ~130k token, ~40 minuti, 14 mutazioni, 8 uccise; `-race` ok)

Nessun ALTA; 1 MEDIA e 5 BASSA. Corretti con le scelte del coordinatore:

- **MEDIA 1, un mese fallito restava acceso fino a 5 settimane.** Con le prime copie fallite i 3 tentativi del mese
  finivano la prima domenica e `prova_remota_fallita` restava nel banner fino alla prima domenica del mese dopo,
  anche con le copie tornate buone, senza che il titolare potesse spegnerlo. Regola del coordinatore: un mese senza
  una prova riuscita **si riprova ogni notte**, nel giro dopo il backup, fino alla prima riuscita o alla prima
  domenica del mese dopo; dentro ogni notte restano i 3 tentativi a 30 minuti; vale anche per `nessuna` e `vecchia`.
  Perché va bene: la prova costa un download per notte solo finché il mese è rotto (quando è rotto conviene saperlo
  e chiuderlo presto), e una notte senza prova non cambia niente. Codice: colonna **`turno`** in `prove_remote`
  (0025 non ancora rilasciata, cambiata sul posto), `TentativiProvaRemota(…, mese, turno)` dà i tentativi della notte
  e le riuscite del mese, memoria per notte e per mese (`chiaveProvaRemota`, `chiaveMeseRemoto`). I testi non dicono
  più «al più tre volte»: tutti finiscono con «La prova si rifà da sola la notte dopo, dopo il backup, finché non
  riesce». Test: `TestProvaRemotaFallitaSiRifaLaNotteDopo` (3 fallite la domenica, riavvio che non rifà la notte,
  lunedì riuscita e problema chiuso, martedì niente), `TestProvaRemotaDovuta` (notte dopo, riavvio, fine mese:
  gennaio riprovato sabato 06/02, febbraio dalla domenica 07/02).
- **BASSA 2, tetto dei byte senza test:** guasto `OltreLaFine` nel server finto (letture oltre la fine del file);
  caso «oltre la fine» (`ErrProvaRemotaTroppoGrande`, `controllo`). R1 uccisa (N1).
- **BASSA 3:** `ErrSpazio` all'estrazione della verifica (lo spazio finisce dopo il download) → `spazio`; ENOSPC nel
  download (gancio `scrittoreScaricamentoPerTest`) → `spazio`, «disco pieno». R10 e R13 uccise (N3, N4).
- **BASSA 4, cartella della copia:** ora `path.Dir` uguale a `path.Clean(cartella)` **e** il nome che passa
  `reArchivio`, come la rotazione: `/backup2/…` e `/backup/altro.tar.age` sono `nessuna`. R3 uccisa (N2). Test dei
  permessi 0600 del file scaricato: R2 uccisa (N7).
- **BASSA 5, confronto dopo la verifica:** test con la verifica che dà un altro SHA-256 e un'altra ora di cattura
  (`controllo`). R11 uccisa.
- **BASSA 6:** testo di «mancante»: «la notte fra sabato e la prima domenica del mese».
- **Mutazioni delle correzioni** (`_slice-129c/muta.py`, `muta-revisione.log`): **7 su 7** (N1 tetto, N2 cartella con
  il prefisso, N3 `ErrSpazio` della verifica, N4 ENOSPC, N5 tentativi contati sul mese invece che sulla notte, N6
  riuscita del mese ignorata, N7 file 0644).

## Segno di copertura

**`// inventario: OPS-28`** su `TestProvaRemotaDelMese`. La voce («Backup notturni fuori sede e allarme», destino Go:
notturni, conservazione, allarme, prova settimanale, copia fuori sede controllata) ha ora tutte le parti nel codice:
124 (notturno), 123 (conservazione), 125 (prova settimanale), 126-127 (allarme), 128 (copia fuori sede) e qui la
verifica intera della copia fuori sede (risposta E), che la revisione della 128 indicava come l'unico pezzo mancante.
La prova vera della **129b** su una Storage Box non è codice e non cambia cosa fa il programma: misura i tempi e
sceglie la porta. Se il coordinatore preferisce chiudere OPS-28 solo dopo la 129b, basta togliere la riga del segno
(il test resta «in parte»). Corretto anche il commento di `TestGiroConCopiaRemota` (128), che diceva «manca il
download mensile».

## Per il manuale

- Una volta al mese, la notte della prima domenica, dopo il backup e la prova della chiavetta, il programma scarica
  l'ultima copia fuori sede dalla Storage Box e la riapre come in un ripristino vero (la decifra e controlla database
  e foto), senza toccare i dati del negozio né i file sul server.
- Se la prova non riesce (copia rovinata o diversa, copia troppo vecchia, server che non risponde, spazio finito
  sull'apparecchio) parte l'allarme: riquadro rosso e, se acceso, il messaggio su Telegram o email.
- Se l'apparecchio era spento quella notte, la prova si fa appena si riaccende. Dopo aver sistemato il problema si può
  rifare subito: `gestrip prova-backup -remota`.

## Differenze dallo standalone

- Lo standalone non ha questa prova nel pacchetto: su otto-vps restic copia fuori sede e la prova di ripristino
  controlla. Solo Go.

## Dubbi

1. **Spazio:** il download vuole i byte della copia più 10 GiB liberi nel volume dei dati (lo stesso margine di
   backup e verifica), e la verifica poi estrae il database accanto al file scaricato: con ~3,6 GB di archivio
   servono ~14 GB liberi più il database. Da guardare con la T5.4 (dimensione del volume cifrato).
2. **Durata:** download e seconda verifica la prima domenica allungano la notte di quanto dura scaricare ~3,6 GB (da
   misurare nella 129b insieme al caricamento). Con la banda in discesa di solito più alta, il tetto di 6 ore basta.
3. **Due prove in poche ore:** come la 125 (Dubbio 2), acceso la prima domenica alle 01:00 dopo un mese spento, alle
   01:10 si fa la prova del mese prima (se non fatta) e alle 02:30 quella del mese nuovo. Innocuo.
4. **Segno OPS-28:** vedi sopra; decide il coordinatore.

## Difetti trovati per strada (solo in lista)

- `ordineCodice` (avvisi, 127/128) non aveva `remoto` e `remoto_vecchio`: finivano in fondo («99…»). Aggiunti insieme ai
  due codici nuovi, perché servivano per metterli nell'ordine dei Problemi (una riga). Nient'altro corretto.
- `gopls check` segnala due concatenazioni in `WriteString` in `avvisi.go:461,463` (`elenco`, 127): solo stile, non
  toccate.
- Per la **130**: il ripristino deve svuotare anche `prove_remote` (scritto nella nota 13(c)).

## gopls

`gopls check` sui sei file nuovi o cambiati di più: nessun avviso nei file della 129c (solo i due suggerimenti di
stile di `avvisi.go` sopra). `gopls references` su `db.UltimaProvaRipristino` (lettori della 126 e della prova:
è servito a scegliere la tabella nuova), su `db.UltimoVerificatoRemoto` (un solo lettore oltre al mio, la rotazione
remota: riusarlo per la prova non cambia niente lì) e su `MeseDi` (solo calendario, righe e test). Il resto con grep e
lettura mirata. Circa 10 chiamate.

## Check

- `scripts/check` senza banco: primo giro **rosso** su gofmt (i due file di test nuovi, `check-1.log`); secondo
  **verde**, copertura «voci 353: chiuse 55, rosse 0, parziali 192, aperte 95, fuori v1 11» (prima 54 e 96: OPS-28
  chiusa). Log `to-be-deleted-after-use/_slice-129c/check-2.log`.
- **Dopo la revisione:** `-race` su `internal/backup` pulito (84 s, `race-2.log`); `scripts/check` rosso al primo giro
  su `TestIndiciComeLoStandalone` (l'indice ora è `prove_remote(avvio,mese,turno)`: chiave di `IndiciSoloGo`
  aggiornata, `check-3.log`), poi **verde**, stessa copertura (`check-4.log`).

## Banco

**Non serve.** Nessun template né rotta toccati; i problemi nuovi esistono solo con la copia remota configurata, e il
banco non ha `backup.remoto` né chiavetta e parte con `-senza-lavori-automatici` (nessun lavoro notturno, quindi
nessuna prova). La migrazione 0025 aggiunge una tabella solo Go che nessuna pagina confrontata legge.
