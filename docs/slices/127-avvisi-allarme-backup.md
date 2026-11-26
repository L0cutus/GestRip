# 127 — Avvisi dell'allarme dei backup al negozio su email e Telegram (T5.2, nona slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 127, risposta **H** (parte avvisi: «un messaggio quando nasce il problema, uno al giorno finché resta, uno
  quando si risolve; con canali spenti solo il banner»), «Rischi»; D-16; nota **11(b)** di `docs/domande-aperte.md`;
  ricevuta 126 (codici fissi di `Problemi`, regola di `-senza-lavori-automatici`); ripresa
  `docs/handoff/2026-11-25-fine-126.md`.
- **Voci:** OPS-28 (parte: avvisi dell'allarme). Nessuna D nuova.
- **Dipendenze nuove:** nessuna. **Migrazioni nuove:** nessuna (lo stato sta in `impostazioni`).
- **Modelli:** implementatore Opus.
- **Banco:** non serve (vedi in fondo).

## Cosa c'è

- `internal/backup/avvisi.go`: `Avvisi`, `NuoviAvvisi`, `Avvisi.Giro`, `Avvisi.Lavoro`, `PostaDelNegozio`
  (l'interfaccia che `uscita.Coda` soddisfa: `CanaleAttivo`, `AccodaIn`), `decidiAvviso` (la regola), testi
  (`testoNascita`, `testoRisolti`, `testoPromemoria`, `titoliAvviso`), `ChiaveAvvisi` (`backup.avvisi`),
  `OraPromemoria` (09:00), `OgniQuantoAvvisi` (5 minuti), `AttesaAvvisiDopoAvvio` (30 minuti).
- `cmd/gestrip/main.go`: gli avvisi partono in `serve` accanto al backup notturno, sulla stessa coda in uscita, **solo
  senza** `-senza-lavori-automatici`; testo dell'opzione e avviso all'avvio aggiornati; `opzioniServe.aspettaAvvisi`
  per i test.
- `internal/importer/mappa.go`: `backup.avvisi` in `ChiaviSoloGo`; `correzioni_test.go`: `avvisi.go` fra i file che
  toccano `impostazioni`.
- Documenti: SVILUPPO (sezione nuova «Backup: avvisi dell'allarme al negozio»), PIANO (§2.5 calendario),
  `docs/domande-aperte.md` (11(b) chiusa, 11(a) completata), ricevuta 125 (Dubbio 1 confermato da Stefano il 25/11),
  ripresa `2026-11-25-fine-126.md` (domanda della domenica chiusa).

### Scelte

- **Un problema per codice, non un allarme unico.** Ogni `Problema.Codice` nasce, resta e si risolve per conto suo.
  Perché: i problemi cambiano davvero (chiavetta tolta → dopo 26 ore anche «backup vecchio»; chiavetta rimessa → il
  backup è ancora vecchio fino al prossimo giro) e con un allarme unico il negozio non saprebbe che cosa è cambiato
  né che cosa resta. Per non fare un messaggio per codice, **un giro manda al più un messaggio per canale**: «problema
  nuovo» (con «Ancora da sistemare» e «Risolto» se ci sono), altrimenti «risolto» («un problema è risolto, ma non
  tutti» con quelli aperti, o «di nuovo tutto a posto»), altrimenti il promemoria. Lo **stesso codice con un testo
  diverso** (chiavetta assente → in sola lettura) resta lo stesso problema: niente messaggio, il promemoria del
  giorno dopo ha il testo nuovo.
- **«Uno al giorno»:** il promemoria parte al **primo giro dalle 09:00 del negozio** (Europe/Rome, ora del muro: con
  l'ora legale le 07:00 UTC) di un giorno del negozio in cui nessun messaggio ha ancora elencato problemi aperti
  (nascita, risolto parziale o promemoria). Perché le 9: è l'ora dell'avviso dei ritardi (094), quando il negozio
  c'è; i problemi nascono quasi tutti di notte (backup alle 02:30) e un promemoria a ora fissa è prevedibile. Il
  messaggio di nascita delle 02:35 vale come messaggio del giorno: il primo promemoria è il mattino dopo. **Tutti i
  giorni**, anche quelli di chiusura, come il backup (D-115). Un giorno scritto «nel futuro» (orologio tornato
  indietro) non ferma il promemoria (confronto `!=`, non `<`). Nascita e risoluzione partono subito, a ogni ora.
- **Testi:** solo `Problema.Testo` (codici fissi, date, cosa fare; già senza percorsi nella 126) e un titolo fisso per
  codice per il «risolto» (`titoliAvviso`, uno generico per un codice sconosciuto). **Mai** il «Motivo» grezzo delle
  esecuzioni (nota 11(b), chiusa) né i nomi dei media. Il promemoria dice da quando il problema è aperto («(dal
  25/11/2026 02:35)»). Email con oggetto, Telegram in testo semplice (niente HTML, niente pulsante). Esempi resi in
  `to-be-deleted-after-use/_slice-127/esempi-messaggi.txt`.
- **Stato durevole e tutto-o-niente:** `backup.avvisi` in `impostazioni` (JSON: codici avvisati con l'istante della
  nascita, giorno dell'ultimo messaggio con problemi aperti), come `audit.conservazione` (016), scritto **nella stessa
  transazione** delle righe della coda (`AccodaIn`): o righe e stato, o niente. Un riavvio rilegge lo stato: non
  rimanda la nascita né il promemoria del giorno e non li perde. Una tabella nuova non serviva (una riga sola). Uno
  stato illeggibile (JSON rotto) vale «niente avvisato»: i problemi di adesso si ridicono (meglio due volte che
  mai); nel registro solo il tipo.
- **Canali spenti (D-16):** destinatari come i lavori della 094 (`CanaleAttivo` della coda e configurazione usabile:
  email al mittente dell'SMTP, Telegram alla chat del negozio). Nessun canale: il giro finisce **prima** di leggere lo
  stato dei backup e non tocca `backup.avvisi`. Così, riaccesi i canali, quello che è nato intanto si dice come nuovo
  e quello che si è risolto intanto come risolto (vero, anche se in ritardo). Un canale solo: solo lui.
- **`-senza-lavori-automatici`:** gli avvisi sono un **lavoro automatico** (come il backup notturno e la prova): con
  l'opzione **non partono proprio**, quindi nel banco e nello sviluppo non si accoda niente. In più, per difesa,
  `Giro` usa `Stato.Allarme()`, che con il lavoro spento fa tacere gli stessi problemi del banner («non configurato»,
  «backup vecchio», «prova mancante»): chi chiamasse `Giro` a mano avrebbe la stessa regola del banner (provato).
- **Stato dei backup illeggibile: nessun avviso.** Niente messaggio e **niente «risolto»** (un errore non è mai «tutto
  a posto»), `backup.avvisi` resta com'era, il giro dopo riprova; nel registro solo il tipo. Perché: `Stato` fallisce
  solo per il database (la chiavetta non dà errore, dà il problema `chiavetta`), e un database che non si legge di
  solito non scrive nemmeno la coda; un errore passeggero farebbe «problema» e poi «risolto» (falso allarme). Il
  banner del titolare lo mostra già (fail-closed lì, 126).
- **Attese:** primo giro **30 minuti** dopo l'avvio (dopo il recupero del backup, 10 minuti, e il tempo di farlo: un
  apparecchio rimasto spento la notte non manda «backup vecchio» seguito da «risolto» pochi minuti dopo), poi ogni
  **5 minuti**. Un panico o un errore del giro non fermano il lavoro.
- **Audit:** le righe della coda firmano `outgoing.queued` con attore `scheduler`, come i lavori automatici.

## Prove (dati inventati, orologio finto, coda in uscita vera senza rete)

`internal/backup/avvisi_linux_test.go` (coda vera con i provider veri SMTP e Telegram, mai chiamati: si accoda
soltanto; Stato scritto a mano):

- `TestAvvisiNascitaGiornoRisoluzione`: nascita alle 02:35, niente allo stesso giro, alle 9 e alle 23:59 dello stesso
  giorno, alle 08:59:59 del giorno dopo; promemoria alle 09:00 esatte e non alle 09:05; terzo giorno; risolto; niente
  dopo. Righe: email al mittente con l'oggetto, Telegram alla chat senza oggetto, destinatario `shop`, stesso testo;
  il promemoria con «(dal 25/11/2026 02:35)» anche il terzo giorno; audit `outgoing.queued` con `scheduler`.
- `TestAvvisiNoveDelNegozio`: le 9 con l'ora legale (08:59:59 e 09:00 CEST il 29/03), mezzanotte di Roma, giorno
  scritto nel futuro.
- `TestAvvisiProblemaCheCambia`: chiavetta → stesso codice con altro testo (niente) → più «backup vecchio» (nuovo con
  la chiavetta fra gli aperti) → chiavetta rimessa (risolto parziale) → niente promemoria lo stesso giorno → uno nasce
  e uno si risolve nello stesso giro.
- `TestAvvisiRiavvio`: Avvisi nuovi sullo stesso database: niente nascita rimandata, promemoria né perso né doppio,
  risolto non perso; stato con JSON rotto: il problema si ridice.
- `TestAvvisiCanaliSpenti`: tutti spenti (stato dei backup mai letto, niente scritto), coda senza provider, solo
  email, Telegram acceso con chat vuota, provider senza accensione con una chat non valida, spenti al momento del
  risolto e riaccesi.
- `TestAvvisiLavoriSpenti`: con il lavoro spento tacciono «backup vecchio» e «prova mancante», la chiavetta suona; con
  il `Notturno` vero non configurato: spento niente, acceso «non configurato».
- `TestAvvisiStatoIlleggibile`: errore dello stato: niente messaggio, niente risolto, stato com'era.
- `TestAvvisiTuttoONiente`: la riga Telegram rifiutata: nemmeno l'email né lo stato; il giro dopo riprova.
- `TestAvvisiTestiSenzaPercorsi`: tutti i problemi insieme, con il punto di montaggio negli errori della chiavetta,
  nel motivo dell'esecuzione e un nome di media: nessun percorso, nessun motivo, nessun nome, le sole barre nelle
  date, meno di 4096 caratteri; tutti i codici hanno un titolo.
- `TestAvvisiLavoro`: attese 30 minuti poi 5 minuti, un giro in panico non ferma il lavoro, contesto finito niente giri.
- `cmd/gestrip/lavori_test.go` (`TestServeBackupNotturno`): senza l'opzione gli avvisi partono con la prima attesa di 30
  minuti, con `-senza-lavori-automatici` non partono.
- `cmd/gestrip/main_test.go` (`TestServeAttesaPredefinita`): fra le attese predefinite di serve anche i 30 minuti
  degli avvisi; `coda_uscita_test.go` (`TestServeCodaInUscita`): gli avvisi hanno la loro attesa finta, così le attese
  contate sono solo quelle degli altri lavori.
- Rossi visti: gofmt del file di prova; nel primo `scripts/check` `TestServeAttesaPredefinita` e `TestServeCodaInUscita`
  (contano le attese diverse di `aspettaPer` e la quarta, i 30 minuti degli avvisi, ne spingeva fuori una: test
  adattati, non il codice); due mutazioni sopravvissute al primo giro (sotto).

### Mutazioni a mano (`to-be-deleted-after-use/_slice-127/muta.py`, `muta.log`, `muta-2.log`, `muta-rifatte.log`)

15 mutazioni. Primo giro **12 su 15**: M2 non compilava (rifatta), sopravvissute **M11** (Telegram senza la
seconda serratura `Usabile`) e **M12** (i problemi rimasti con l'istante di adesso). Aggiunti il caso del provider
senza accensione con una chat non valida e il controllo di «(dal …)» nel secondo promemoria; M11 al secondo
tentativo sopravviveva ancora (nessun problema nuovo da dire in quel passo: corretto il test). Totale **15 su 15**.
Elenco: M1 9:00 con `>`; M2 promemoria a ogni ora; M3 promemoria senza il giorno; M4 nascita che non segna il giorno;
M5 risolto parziale che non segna il giorno; M6 `Problemi` al posto di `Allarme` (lavori spenti); M7 canale spento
usato; M8 stato illeggibile letto come sano (darebbe «risolto»); M9 stato rotto come errore; M10 senza canali legge e
scrive; M11 chat senza `Usabile`; M12 istante di nascita perso; M13 nessuna attesa dopo l'avvio; M14 oggetto anche su
Telegram; M15 avvisi anche con `-senza-lavori-automatici`.

## Revisione (Opus indipendente, ~70k token, ~15 minuti, 14 mutazioni, 12 uccise; `-race` ok)

Nessun ALTA. Corretti i due MEDIA e due BASSA, scelte del coordinatore:

- **MEDIA 1, chiavetta ballerina senza isteresi** (fino a 12 messaggi l'ora per canale, anche di notte). Ora un codice
  aperto che manca dall'allarme è «risolto» solo dopo **30 minuti di fila** (`AttesaRisolto`); se torna prima non
  parte né «nuovo» né «risolto» e resta aperto con la sua data di nascita; l'inizio dell'assenza sta in
  `backup.avvisi` (campo `assenti`), scritto anche senza messaggio (`decidiAvviso` dice «scrivi»), quindi un riavvio
  durante i 30 minuti non lo perde né lo rimette a zero. Mentre aspetta la conferma il problema compare negli altri
  messaggi come «(dal …) <titolo>: sembra sistemato, si aspetta la conferma.». Perché 30 minuti: sei giri degli
  avvisi, abbastanza per un contatto lento, e il «risolto» arriva comunque entro mezz'ora dalla sistemazione vera.
  Test `TestAvvisiContattoBallerino` (due ore a 5 minuti alternati: un solo messaggio; assente 25 minuti e tornato:
  niente; risolto a 30 minuti esatti, non a 29) e il riavvio durante i 30 minuti in `TestAvvisiRiavvio`; gli altri
  test aggiornati (il risolto arriva al giro dei 30 minuti).
- **MEDIA 2, chiavetta appesa:** la lettura dello stato del giro gira in una goroutine (`leggiStatoBackup`) aspettata
  al più `TempoStatoAvvisi` (5 s): scaduto, `ErrStatoAvvisiScaduto` = illeggibile, nessun messaggio; **una lettura
  alla volta** (una lettura appesa non ne accumula altre); la goroutine non è fra i lavori di serve, quindi lo stop
  non la aspetta; un panico dentro diventa un errore. Lo schema è quello di `avviaLettura` della 126, ma il codice non
  si riusa: quello scrive nella cache della faccia web e dipende da `facciaWeb`; spostarlo in comune avrebbe toccato la
  pagina della 126 (non chiesto). Test `TestAvvisiChiavettaAppesa` (lettura finta che non torna, come
  `ControllaChiavetta`: due giri scaduti, una lettura sola, nessuna riga; il lavoro si ferma subito allo stop; sbloccata,
  il giro dopo avvisa).
- **BASSA, «non configurato» nasconde gli altri:** con `Configurato` falso i codici aperti che mancano non sono né
  assenti né risolti: restano aperti; tornata la configurazione si contano i 30 minuti. Test
  `TestAvvisiNonConfiguratoNasconde`.
- **BASSA, M13 del revisore sopravvissuta:** `TestChiaviSoloGoComplete` ora controlla `backup.ora` e `backup.avvisi`.
- **Mutazioni delle correzioni** (`_slice-127/muta-revisione.py`, `muta-revisione.log`): N1 risolto senza aspettare, N2
  assenza che non si accumula, N3 ritorno che non azzera l'assenza, N4 nascosti dati per assenti, N5 assenza non scritta
  senza messaggio, N6 nessun tempo massimo (uccisa dal `-timeout`), N7 nessuna lettura unica, R13 `backup.avvisi` tolta
  da `ChiaviSoloGo` (la M13 del revisore): **uccise 8 su 8**. `-race` dell'implementatore sugli avvisi: pulito.

## Segno di copertura

**Nessun `// inventario:`.** OPS-28 è «Backup notturni fuori sede e allarme»: con la 127 l'allarme è completo
(banner 126 + avvisi 127), ma «fuori sede» è la copia SFTP della **128**. La voce chiude con la 128 (insieme a
OPS-16). I test portano «Voce OPS-28, in parte».

## Per il manuale

- Se email o Telegram del negozio sono accesi, quando c'è un problema con i backup arriva un messaggio «⚠ Backup del
  negozio: c'è un problema nuovo» con cosa fare; finché resta, un promemoria ogni mattina verso le 9; quando è
  sistemato, «✅ … di nuovo tutto a posto». Con email e Telegram spenti c'è solo il riquadro rosso del titolare.
- Il messaggio non contiene mai percorsi né chiavi: per i dettagli c'è la pagina Impostazioni → Backup.

## Differenze dallo standalone

- Lo standalone non ha avvisi dei backup: l'allarme delle 26 ore lo fa otto-vps fuori dal pacchetto. Solo Go.

## Dubbi

1. **Messaggi di notte:** la nascita parte subito, quindi un backup fallito alle 02:30 fa arrivare un Telegram alle
   02:35. La risposta H dice «quando nasce»; se Stefano preferisce, la nascita fra le 22 e le 8 può aspettare le 9
   (una riga in `decidiAvviso`).
2. **Promemoria anche nei giorni di chiusura:** scelto come il backup (D-115). Se Stefano li vuole solo nei giorni di
   apertura (D-17), basta leggere `jobs.giorni_apertura`.
3. **Chiavetta che va e viene:** risolto nella revisione (MEDIA 1, sotto): 30 minuti di fila per il «risolto».
4. **Chiavetta appesa:** risolto per gli avvisi nella revisione (MEDIA 2, sotto); per il lavoro notturno resta in lista.
5. **gopls:** `goToImplementation` su `PostaDelNegozio` ha confermato che la implementano `uscita.Coda` e il finto
   dei test (`postaCheCade`) — è servito a scegliere l'interfaccia invece di `*uscita.Coda`. `findReferences` su
   `Stato.Allarme` ha risposto con un errore di posizione (indice vecchio: `workspaceSymbol` dava la riga 201 invece
   della 216); i chiamanti (`web/backup.go`, `avvisi.go`) controllati con grep. Il resto con grep e lettura.

## Difetti trovati per strada (solo in lista)

- `destinazioni` degli avvisi copia `jobs.Pianificatore.indirizzoDelNegozio` (15 righe): una funzione comune in
  `uscita` (per esempio `IndirizzoDelNegozio`) le unirebbe. Non fatto: non chiesto.
- **Per la 130 (ripristino):** oltre al registro dei backup, il ripristino dovrebbe azzerare `backup.avvisi` (lo stato
  della macchina vecchia): altrimenti la copia ripristinata manderebbe un «risolto» per problemi della macchina vecchia,
  una volta riaccesi i canali (131).
- **Lavoro notturno (124) con la chiavetta appesa:** `Notturno.Giro` e la prova aprono e controllano la chiavetta
  (`ApriChiavetta`, `controllaChiavetta`) senza contesto né tempo massimo, dentro un lavoro che `serve` aspetta allo
  stop (`lavori.Wait()`): una chiavetta appesa ferma il backup e lo stop di serve. Non corretto (indicazione del
  coordinatore): da fare in una slice sua.
- Dalla revisione (solo in lista, non corretti): stato JSON rotto → un problema già avvisato non avrà mai il suo
  «risolto» (si riparte da niente avvisato); controllo e scrittura dello stato fuori dalla stessa transazione, con due
  `serve` sullo stesso database i messaggi potrebbero doppiarsi; orologio tornato indietro → la data «(dal …)» può
  essere nel futuro; un canale acceso dopo la nascita non riceve il «nuovo» (solo promemoria e risolto); M6 del
  revisore (tolto il controllo dei codici doppi) è innocua: `Problemi` non dà mai due volte lo stesso codice.

## Check

- Dopo la revisione: vedi la riga in fondo a questa sezione.
- `scripts/check` senza banco: primo giro **rosso** su gofmt (il file di prova); secondo **rosso** su
  `TestServeAttesaPredefinita` e `TestServeCodaInUscita` (vedi Prove, test adattati; `check.log`); terzo **verde**,
  copertura invariata «voci 353: chiuse 53, rosse 0, parziali 192, aperte 97, fuori v1 11» (nessun segno nuovo). Log in
  `to-be-deleted-after-use/_slice-127/check2.log`.
- **Dopo la revisione:** `scripts/check` **verde** al primo giro, copertura invariata (stessa riga); log
  `_slice-127/check3.log`.

## Banco

**Non serve.** Nessuna pagina confrontata cambia (nessun template né rotta toccati). I due Go del banco (quello senza
canali e quello con Telegram acceso, `tools/banco/gotelegram.go`) partono **tutti e due** con
`-senza-lavori-automatici` (`tools/banco/istanza.go:357`, `gotelegram.go:161`), quindi il lavoro degli avvisi non
parte e non accoda niente; anche partisse, il primo giro sarebbe dopo 30 minuti e il banco non ha `backup_chiavetta`
(solo «non configurato», che tace con l'opzione). Cambia solo la riga dell'avviso all'avvio di `serve`, che il banco
non confronta.
