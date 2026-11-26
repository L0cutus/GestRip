# 129 — Copia remota nella pagina Backup (T5.2, undicesima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 129, risposta **E** (sottoaccount, chiave creata dall'apparecchio, impronta controllata alla configurazione),
  «Rischi»; ricevute 126 (pagina Backup, solo titolare, CSRF, cache dello stato) e 128 (comandi `chiave-sftp` e
  `backup-remoto`, `backup.remoto`, nota 13); `docs/domande-aperte.md` nota 13; ripresa
  `docs/handoff/2026-11-25-fine-126.md`. Precisazione del coordinatore: la prova vera è la **129b** (con Stefano), il
  download mensile con verifica intera la **129c**.
- **Voci:** OPS-16 (destinazione remota), già **chiusa** dalla 128: nessun segno `inventario:` nuovo.
- **Dipendenze nuove:** nessuna. **Migrazioni nuove:** nessuna.
- **Modelli:** implementatore Opus.
- **Banco:** non serve (vedi in fondo); comandi del rigiro di controllo scritti lo stesso.
- **Prova vera sulla Storage Box:** **non fatta** (serve l'ok di Stefano, un sottoaccount e credenziali vere): passi in
  fondo, 129b.

## Cosa c'è

- `internal/backup/configura_remoto.go` (nuovo): le regole comuni a comando e pagina. `NuovaConfigRemota` (campi
  controllati prima di collegarsi, `*ErrCampoRemoto` con `CampoHost`/`CampoPorta`/`CampoUtente`/`CampoCartella`),
  `FissaRemoto` (impronta riscritta ben formata → chiave del server riletta e uguale → `Prova` con la chiave fissata →
  scrittura con la cartella vera), `ErrImprontaMalScritta`, `*ErrImprontaDiversa` (tipo e impronta viste),
  `ErrProvaInCorso`, `ErrRemotoNonConfigurato`; `ConfigurazioneRemota` per la pagina (`Leggi`, `ChiavePubblica`,
  `CreaChiave` con il lucchetto dei backup, `ChiaveDelServer`, `Fissa`, `Prova` della salvata, `Togli`; le operazioni
  di rete una alla volta con `TryLock`; `ConPreparazione` solo per i test).
- `internal/backup/remoto.go`: `TempoMassimoProvaRemota` (60 s) e `Remoto.TempoProva`: `Prova` e `ChiaveDelServer`
  hanno ora un tempo massimo intero (`tempoDellaProva`, `errTempoProva`); la scadenza dell'handshake di
  `ChiaveDelServer` è `ErrTempoRemoto`; `ErrNomeServer` (prima un `errors.New` senza nome, stesso testo);
  `Controlla` passa da `campoSbagliato` (stesse regole e messaggi).
- `cmd/gestrip/backup_remoto.go`: `backup-remoto` usa `NuovaConfigRemota` e `FissaRemoto` (stesse uscite di prima;
  l'impronta mal scritta ora si vede prima di collegarsi).
- `internal/web/backup_remoto.go` (nuovo): interfaccia `CopiaRemotaDelNegozio`, le cinque azioni, `testoErroreRemoto`,
  il modulo e i suoi campi. `backup.go`: `mostraBackup` (la pagina con errore/messaggio e la sezione della copia
  remota), `datiBackup.Remoto`, le rotte. `salute.go`: `Dipendenze.CopiaRemota`. `pagine/backup.html`: sezione
  `backup-remote`; `statici/app.css`: una regola `.backup-chiave-pubblica` (solo quella casella). `cmd/gestrip/main.go`: `dip.CopiaRemota = backup.NuovaConfigurazioneRemota(d, clock.Sistema{})`.
- `internal/web/ruoli_test.go`: cinque rotte nuove in `tabellaPermessi`, tutte `soloTitolare`, voce OPS-16.
  `guscio_test.go`: i due POST che ridisegnano la pagina (`…/remoto`, `…/remoto/impronta`) in `TestRotteConGuscio`.
- Documenti: SVILUPPO (sezione nuova «Backup: pagina della copia remota», riga della 128 corretta), PIANO §8.2 n. 15
  (129, 129b, 129c), `docs/domande-aperte.md` nota 13 (b) e (c).

### Scelte

- **Due passi, conferma riscrivendo l'impronta.** Primo passo («Mostra l'impronta del server»): con host, porta,
  utente e cartella il programma si collega solo fino alla chiave del server (si ferma prima dell'autenticazione) e
  mostra tipo e impronta; non scrive niente. Secondo passo: il titolare scrive o incolla nel campo vuoto l'impronta
  **pubblicata dal fornitore** e preme «Prova collegamento e salva». Perché un campo vuoto e non un pulsante «sì, è
  lei»: la conferma è il confronto con la fonte esterna, come `-impronta` del comando; un pulsante si preme senza
  guardare. Il server si rilegge al secondo passo e il confronto è con la chiave **vera** di quel momento (dopo la
  revisione nessun campo nascosto porta l'impronta: vedi «Revisione», B6).
- **Salvataggio solo con la prova riuscita:** `FissaRemoto` scrive solo dopo che il server si è presentato con la
  chiave scritta, ha accettato la chiave dell'apparecchio e ha la cartella (non la radice). «Prova collegamento» della
  configurazione salvata non scrive niente.
- **Una prova alla volta, senza fermare le altre pagine:** `TryLock` in `ConfigurazioneRemota`: la seconda richiesta
  risponde subito «Un'altra prova del collegamento è in corso», senza collegarsi e senza code. La prova gira nella
  richiesta del titolare (al più 60 s, il browser aspetta), non prende lucchetti che altre pagine usano. Il server non
  ha `WriteTimeout` (`cmd/gestrip/main.go`), quindi 60 s passano.
- **Tempo massimo nel pacchetto backup, non nella pagina:** `TempoMassimoProvaRemota` vale anche per il comando
  (nota 13, «senza sorvegliante»). Allo scadere il sorvegliante di `collega` chiude la connessione (il contesto della
  prova è il suo); `tempoDellaProva` dà `ErrTempoRemoto` codice `rete` qualunque operazione si sia interrotta. Lo stop
  di chi chiede (browser chiuso) resta l'errore del contesto, senza codice.
- **Spegnimento con una spunta** (`required`, controllata anche dal server): un clic per sbaglio non spegne la copia
  fuori sede. Gli archivi sul server restano (come `-togli`).
- **Chiave dell'apparecchio:** se manca, un pulsante la crea (con il lucchetto dei backup: durante un backup «crea la
  chiave quando ha finito»); se c'è, la riga pubblica in una casella `input` in sola lettura, larga quanto la pagina
  (si seleziona e copia senza JavaScript; una `textarea` senza nome la vieta `TestCaselleDiPiuRigheNormalizzate`). La privata non esce mai (test che cercano l'intestazione e i pezzi del corpo della privata nella
  pagina).
- **Errori in parole del titolare** (`testoErroreRemoto`), con il campo segnato (`aria-invalid`, `aria-describedby`
  sull'errore) quando è di un campo: impronta mal scritta o diversa (con l'impronta vista), impronta cambiata dopo il
  salvataggio, chiave non autorizzata (campo utente), cartella che non c'è, porta chiusa (`ECONNREFUSED`, campo porta),
  nome che non si trova (campo host), tempo scaduto, chiave mancante, prova in corso. Mai il testo grezzo (indirizzi,
  percorsi del server); nel registro del server solo codice e tipo.
- **Porta proposta 22** (`PortaRemotaPredefinita`), come il comando: la Storage Box risponde a SFTP sulla 22 e sulla
  23; si decide dopo la prova vera (129b). I testi d'aiuto dicono «22 o 23».
- **Host, utente e cartella** si vedono nella pagina del titolare (li ha scritti lui); banner e avvisi restano quelli
  della 128, senza.

## Prove (server SFTP finto, niente rete, niente credenziali vere)

Pacchetto `backup` (`configura_remoto_linux_test.go`):

- `TestNuovaConfigRemota`: 11 campi sbagliati con il campo giusto, spazi tolti.
- `TestFissaRemoto`: impronta mostrata senza scrivere; mal scritta (vuota, senza prefisso, corta); diversa (con tipo e
  impronta viste, codice `impronta`); chiave non autorizzata (`accesso`); cartella che non c'è (`cartella`); radice
  (`CampoCartella`); niente scritto in tutti questi casi; salvata con la cartella vera `/backup`; prova della salvata;
  un altro server con un'altra chiave allo stesso indirizzo (`impronta`); spenta, `ErrRemotoNonConfigurato`.
- `TestProvaRemotaTempoMassimo`: server fermo sulla cartella oltre il tempo della prova (`rete`, `ErrTempoRemoto`,
  niente scritto); connessione che non risponde all'handshake, scaduta per il tempo della prova e per quello
  dell'handshake; stop di chi chiede = contesto, senza codice.
- `TestChiaveDelServerRete`: porta chiusa (`ECONNREFUSED`) e nome che non si trova (`ErrNomeServer`), senza l'host nel
  testo.
- `TestConfigurazioneRemotaUnaAllaVolta`: mentre una conferma è ferma sul server, impronta, prova e conferma rispondono
  subito `ErrProvaInCorso`; dopo, funziona; la chiave non si crea durante un backup (`ErrBackupInCorso`).

Pacchetto `web` (`backup_remoto_linux_test.go`, faccia vera, `ConfigurazioneRemota` vera sul server finto):

- `TestPaginaCopiaRemota`: il giro intero con soli moduli (**senza JavaScript**): pagina nuova (pulsante della
  chiave, modulo, porta 22), chiave creata e mostrata (solo la pubblica, niente privata), di nuovo «c'era già»;
  primo passo che mostra l'impronta e non scrive; conferma con un'impronta diversa (resta il secondo passo, campo
  segnato, impronta vista), mal scritta, vuota (POST forgiato): niente salvato, primo passo di nuovo; conferma giusta (con spazi ai lati):
  302 e salvata con `/backup`; pagina con la configurazione e «Prova collegamento»; prova riuscita; spegnimento senza
  spunta rifiutato, con spunta fatto; prova da spenta.
- `TestPaginaCopiaRemotaErrori`: host con spazi, porta non numero, utente vuoto (campo segnato, **nessuna
  connessione**); chiave non autorizzata; chiave assente; cartella che non c'è; porta chiusa; nome che non si trova;
  tempo scaduto (server fermo oltre 300 ms di prova); impronta cambiata dopo il salvataggio (la prova della salvata
  rifiuta, configurazione invariata, l'impronta dell'altro server non si mostra). Mai testi grezzi del sistema.
- `TestPaginaCopiaRemotaRuoloECSRF`: il dipendente su tutte e cinque le rotte torna a / e niente parte (nessuna chiave,
  nessuna connessione, niente scritto né spento); il titolare senza token o con un token sbagliato: 403 su tutte;
  senza `CopiaRemota` nelle dipendenze niente sezione e azioni con l'errore.
- `TestPaginaCopiaRemotaProvaConcorrente`: due sessioni del titolare; mentre la prima conferma è ferma sul server, la
  seconda (primo passo e «Prova collegamento») risponde subito «in corso» senza collegarsi; home e pagina rispondono
  (< 3 s); sbloccata, la prima salva.
- `TestPaginaCopiaRemotaImprontaVistaForgiata` (rifatto con la revisione): campi forgiati con l'impronta vista non si
  mostrano mai e non fanno salvare; il secondo passo mostra solo l'impronta del server.
- Dopo la revisione anche `TestPaginaCopiaRemotaIlleggibileSiSpegne`, `TestPaginaCopiaRemotaTogliNonRiuscito`,
  `TestTestoCampoRemotoMaiMuto` (web) e `TestConfigurazioneRemotaStopDiServe`, `TestTogliDuranteLaConferma`,
  `TestFissaRemotoTempoUnico` (backup).
- Comando: `TestComandiCopiaRemota` della 128 passa invariato.
- `-race` (`CGO_ENABLED=1 -tags nodynamic`, `-run` mirato) su `internal/web` (`CopiaRemota|Backup|OgniRotta`, 85 s),
  `internal/backup` (test nuovi più `CopiaRemotaServerLento|CopiaRemotaStop`) e `cmd/gestrip` (`CopiaRemota|BackupRemoto`):
  pulito. Log `to-be-deleted-after-use/_slice-129/race.log`.

### Mutazioni a mano (`to-be-deleted-after-use/_slice-129/muta.py`, `muta.log`, `muta-2.log`, `muta-3.log`)

17 mutazioni sulle guardie, ognuna con il test mirato e il file rimesso identico (md5): **17 uccise** dopo una
correzione.

- Primo giro (`muta.log`): 15 su 17. Sopravvissute M7 (`ctx.Err() != nil` in `tempoDellaProva`) e M10 (caso
  `errTempoProva` nel sorvegliante): **guardie ridondanti**, il risultato era lo stesso senza (la causa del contesto
  della prova è `errTempoProva` solo se è scaduto il suo tempo, e `tempoDellaProva` traduce già l'errore del
  sorvegliante). Tolte tutte e due (codice più corto); M7 e M10 rifatte su altre guardie (`muta-2.log`): la causa del
  contesto della prova controllata (uccisa dal test sullo stop) e la scadenza dell'handshake di `ChiaveDelServer`
  tolta (uccisa: appeso oltre 10 s).
- Uccise: impronta mal scritta accettata (M1), confronto sempre uguale (M2), errore della prova ignorato (M3),
  controllo della cartella vera tolto (M4), una prova alla volta tolta (M5), tempo massimo della prova tolto (M6),
  tempo della prova senza codice (M8), scadenza dell'handshake senza `ErrTempoRemoto` (M9), spegnimento senza spunta
  (M11), impronta vista forgiata mostrata (M12), secondo passo perso dopo l'errore (M13), porta chiusa senza il suo
  testo (M14), «togli» aperto al personale (M15, anche `TestOgniRottaHaIlSuoPermesso`), campo sbagliato senza
  `aria-invalid` (M16), nome del server senza sentinella (M17).
- Giro finale sul codice definitivo (`muta-3.log`): **17 su 17**.

## Segno di copertura

Nessuno: OPS-16 è già chiusa dalla 128 (`TestGiroConCopiaRemota`) e la 129 non chiude un'altra voce intera. I test
nuovi dicono «Voce OPS-16, già chiusa dalla 128: niente segno».

## Per il manuale

- Impostazioni → Backup → «Copia remota (fuori dal negozio)». Se l'apparecchio non ha ancora la chiave: «Crea la
  chiave dell'apparecchio». Poi si copia la riga della chiave pubblica nel pannello della Storage Box, nelle chiavi
  SSH del sottoaccount dei backup (non è segreta).
- Si scrivono server, porta (22 o 23), utente del sottoaccount e cartella, e si preme «Mostra l'impronta del server».
  La pagina mostra l'impronta: si confronta con quella pubblicata da Hetzner e, se è uguale, si scrive o incolla
  quella **di Hetzner** nel campo e si preme «Prova collegamento e salva». Si salva solo se tutto funziona; altrimenti
  la pagina dice cosa non va (impronta diversa, chiave non ancora nel pannello, cartella che non c'è, porta chiusa,
  server che non risponde).
- «Prova collegamento» controlla in ogni momento la configurazione salvata senza caricare niente. «Spegni la copia
  remota» (con la spunta) la ferma; i backup già sul server restano.
- Schermate: della 132.

## Differenze dallo standalone

- Lo standalone non ha una pagina per la copia fuori sede (restic da otto-vps, fuori dal pacchetto). Solo Go.

## Dubbi

1. **Porta proposta 22 o 23:** resta 22 come il comando; si sceglie dopo la 129b.
2. **60 s di prova nella richiesta:** il browser aspetta fino a un minuto con la pagina ferma (il pulsante non ha
   JavaScript che dica «sto provando»). Accettabile per un'azione rara del titolare; un proxy davanti a serve con un
   tempo più corto (oggi non c'è: Tailscale) taglierebbe la risposta, non la prova.
3. **Conferma dell'impronta da scrivere:** se Stefano preferisce un pulsante «è uguale» dopo il confronto a occhio,
   si cambia solo il secondo passo; oggi è la forma più sicura (la stessa del comando).

## Difetti trovati per strada (solo in lista)

- Da sapere, non un difetto: la sezione della copia remota dipende da `CopiaRemota`, ma la pagina resta legata a
  `Backup` (126): senza `Backup` niente pagina, anche se `CopiaRemota` c'è. In serve ci sono sempre tutti e due.
- Nota 13(d) e i punti 3 e 4 della revisione 128 restano (archivio senza riga nel registro, rotazione remota che
  fallisce sempre solo «avviso»).
- Niente altro trovato.

## Revisione (Opus indipendente, ~90k token, ~25 minuti, 14 mutazioni, 13 uccise; `-race` ok)

Nessun ALTA; 2 MEDIA e 6 BASSA. Corretti, con le scelte del coordinatore:

- **MEDIA 1, lo stop di serve non fermava la prova.** Il contesto della richiesta non finisce allo stop: con un
  SIGTERM durante una conferma lenta (fino a 120 s prima della correzione B5) lo spegnimento dei server HTTP (10 s)
  scadeva e serve usciva con errore. Ora `ConfigurazioneRemota.ConStop(ctx)` (in serve il contesto dei segnali):
  impronta, conferma e prova girano su un contesto che finisce anche allo stop (`conStop`). Scelta: qui e non nel
  `BaseContext` del server HTTP, perché tocca solo le richieste che parlano con la rete per decine di secondi e non
  cambia come finiscono tutte le altre pagine. `TestConfigurazioneRemotaStopDiServe` (conferma ferma sul server,
  stop, ritorno in meno di 3 s con il contesto, niente scritto); N1: uccisa.
- **MEDIA 2, configurazione illeggibile non spegnibile:** il modulo «Spegni» compare anche con la configurazione che
  non si legge. `TestPaginaCopiaRemotaIlleggibileSiSpegne`; N4: uccisa.
- **BASSA 3, testi dei campi «tipo» e «impronta»:** aggiunti (`backup.CampoTipo`, `backup.CampoImpronta`, ora
  esportati) e un testo di ripiego `msgCampoRemotoSbagliato`: mai un messaggio muto. `TestTestoCampoRemotoMaiMuto`;
  N5: uccisa.
- **BASSA 4, spegnimento in corsa con la conferma:** `Togli` passa dallo stesso turno (`conRete`): durante una conferma
  è `ErrProvaInCorso` (la pagina dice «in corso»), così la conferma che finisce dopo non riscrive una configurazione
  appena spenta. `TestTogliDuranteLaConferma`; N2 e N6: uccise.
- **BASSA 5, «al più un minuto» ma la conferma arrivava a due:** un solo tempo massimo per tutta `FissaRemoto` (lettura
  della chiave e prova), `TempoMassimoProvaRemota`; il testo del secondo passo lo dice. `TestFissaRemotoTempoUnico`
  (due collegamenti lenti, ognuno sotto il tempo, insieme sopra); N3: uccisa.
- **BASSA 6, impronta del campo nascosto mostrata dopo un errore:** tolti i campi nascosti `tipo_visto` e
  `impronta_vista`. Dopo un errore il secondo passo torna solo con l'impronta riletta dal server (impronta diversa);
  ogni altro errore (anche l'impronta mal scritta) torna al primo passo con i campi scritti.
  `TestPaginaCopiaRemotaImprontaVistaForgiata` rifatto; M12 e M13 rifatte sul codice nuovo: uccise.
- **M14 del revisore sopravvissuta, spegnimento non riuscito:** `TestPaginaCopiaRemotaTogliNonRiuscito` (guasto: «non
  si è potuta spegnere», niente «spenta»; prova in corso: «in corso»).
- **Comando, impronta diversa scritta due volte** (su stdout e nell'errore su stderr): l'errore ora è
  `errImprontaNonUguale` («… niente è stato scritto»), il dettaglio solo su stdout.
- Mutazioni delle correzioni (`_slice-129/muta-revisione.log`): **8 su 8** (N1–N6, M12, M13 nuove).

Solo in lista (non corretti, indicazione del coordinatore):

- **B1:** dalla pagina si capisce se una porta di un host interno è aperta (porta chiusa / tempo scaduto / impronta
  mostrata). Il CSRF è chiuso e la pagina è solo del titolare: accettabile.
- **B2:** host e cartella ammettono caratteri strani (non controllo, non spazi nell'host): innocui, il server li
  rifiuta e la pagina li scrive con l'escape.

## gopls

`gopls check` sui cinque file nuovi o cambiati di più (`configura_remoto.go`, `remoto.go`, `web/backup_remoto.go`,
`web/backup.go`, `cmd/gestrip/backup_remoto.go`, con `GOFLAGS=-tags=nodynamic`): nessun avviso. `gopls references`
su `Remoto.ChiaveDelServer` (tre chiamanti: il comando senza `-impronta`, `FissaRemoto`, `ConfigurazioneRemota`): è
servito a decidere di mettere il tempo massimo **dentro** `ChiaveDelServer` e `Prova` (vale per tutti) invece che nella
pagina; su `senzaIndirizzo` (tre chiamanti) per controllare che `ErrNomeServer` non cambi altri testi. Il resto con
grep e lettura mirata.

## Check

- `scripts/check`: primo giro **rosso** su gofmt (tre file); secondo **rosso** su `TestCaselleDiPiuRigheNormalizzate`
  (la `textarea` della chiave senza nome: ora una casella `input` in sola lettura) e `TestRotteConGuscio` (i due POST
  che ridisegnano la pagina, aggiunti alla lista attesa); terzo **verde**, copertura «voci 353: chiuse 54, rosse 0,
  parziali 192, aperte 96, fuori v1 11» (invariata: nessun segno nuovo). Log `_slice-129/check-1.log`, `check-2.log`,
  `check-3.log`.
- `-race` rifatto dopo le correzioni sui test web toccati (`CopiaRemota|CaselleDiPiuRighe|RotteConGuscio`): pulito
  (`race-2.log`).
- **Dopo la revisione:** `scripts/check` intero **verde** al primo giro, copertura invariata («voci 353: chiuse 54,
  rosse 0, parziali 192, aperte 96, fuori v1 11»), `check-4.log`; `-race` mirato su `internal/web`, `internal/backup`
  e `cmd/gestrip` (test della copia remota, compresi i nuovi): pulito, `race-3.log`.

## Banco

**Non serve.** Cambia solo il template `backup.html` (sezione nuova), le rotte nuove sotto `/settings/backup` e una regola di
`app.css` per una classe che sta solo in quella sezione;
`base.html`, il banner e le altre pagine non cambiano. Nessuno scenario del banco apre `/settings/backup` né le sue
azioni (`grep` su `tools/banco/`: nessun riferimento; `browser/banco.mjs` visita solo `/settings/templates` e
`/settings/anagrafiche`), e il banco non ha `backup.remoto` né la chiave SFTP. Nessuna pagina confrontata cambia.

Se il coordinatore vuole lo stesso un rigiro di controllo (con il «memoria libera» e la guardia sotto 10 GB, binari
compilati come per il banco in `to-be-deleted-after-use/_slice-129/banco-bin/`):

```
banco su -nome s129a -immagine gestrip-standalone:fab-225 -gestrip <banco-bin>/gestrip -dati sintetici-import
banco esegui -nome s129a -banco <banco-bin>/banco
GESTRIP_BANCO_ESITO=to-be-deleted-after-use/banco/grgo-s129a/esito/esito.json scripts/check
docker logs grgo-s129a-app > …   # prima di banco giu
banco giu -nome s129a
```

Atteso: uguale all'ultimo giro verde (s126b): HTTP 91 scenari, 0 inattese, 0 errori; browser 38 scenari, 0 inattese;
`backup-remote` e `backup-alarm` in nessuna risposta salvata del Go. La nota 8 (standalone in timeout, «(assente)»)
vuole un rigiro.

## Prova vera: slice corta 129b (da fare con Stefano, quando avrà creato il sottoaccount)

**Prima (Stefano):** nel pannello Hetzner un **sottoaccount di prova** della Storage Box, separato da quello dei
backup dello standalone, con SSH/SFTP attivo e una cartella sua (per esempio `prova-gestrip`); host, porta e nome del
sottoaccount alla sessione. Nessuna password nel repo né nei log. Solo dati inventati.

**Dove:** pc-fisso, in una cartella dati nuova in `to-be-deleted-after-use/_slice-129b/` (mai i dati veri), con
`gestrip serve` locale (`127.0.0.1:47101`), database sintetico e una chiavetta finta (una cartella su un altro
dispositivo, o la macchina di prova pc-fiacco).

**Passi:**

1. `gestrip serve` sulla cartella nuova; titolare → Impostazioni → Backup → «Crea la chiave dell'apparecchio».
   Stefano copia la riga pubblica nel pannello del sottoaccount.
2. Primo passo con host `uXXXXXX.your-storagebox.de`, porta **23**, utente `uXXXXXX-subN`, cartella `prova-gestrip`:
   la pagina mostra l'impronta; Stefano la confronta con quella pubblicata da Hetzner (per la porta 23 e per la 22
   sono le stesse chiavi del server: da verificare) e la incolla; «Prova collegamento e salva».
3. Ripetere il primo passo sulla porta **22**: annotare se risponde, con la stessa impronta e lo stesso esito.
4. «Prova collegamento» sulla configurazione salvata; poi un errore voluto per ogni testo: cartella sbagliata, chiave
   tolta dal pannello (poi rimessa), impronta scritta male.
5. «Esegui adesso» con un archivio inventato di dimensione nota (dati sintetici, ~100 MB e uno di ~1 GB se la banda lo
   consente); a fine giro lo storico deve dire «copia remota riuscita» e la riga `sftp` nel registro.

**Cosa misurare (nota 13(b)):**

- **Durata e banda** del caricamento: byte / secondi per ogni archivio, banda in uscita in Mbit/s; proiezione per
  ~3,6 GB rispetto al tetto di **6 ore** (`TempoMassimoCopiaRemota`; serve almeno ~1,4 Mbit/s). Misura fatta dalla
  rete di pc-fisso: quella del negozio va rimisurata lì (T5.4).
- **Porta 22 o 23:** quale risponde a SFTP con la chiave, tempi del collegamento su tutte e due; quale proporre.
- **`statvfs@openssh.com`:** se la Storage Box lo dichiara (con l'estensione la copia controlla lo spazio prima;
  senza, si prova); valori dati (spazio libero coerente col pannello?).
- **`RealPath`** della cartella del sottoaccount: deve tornare assoluto (per esempio `/home/prova-gestrip` o
  `/prova-gestrip`) e non la radice; è quello che finisce in `backup.remoto`.
- Durata di «Prova collegamento» (deve stare ben sotto i 60 s di `TempoMassimoProvaRemota`).

**Esito atteso:** impronta mostrata e uguale a quella di Hetzner; salvataggio riuscito solo dopo la conferma; prova
riuscita; ogni errore voluto con il suo testo e niente salvato; caricamento riuscito con dimensione uguale, riga
`sftp` `ok`, nessun `.pending` rimasto; banda sufficiente per 3,6 GB in meno di 6 ore (se no: decisione a parte,
«completo settimanale + solo i file nuovi», piano «Rischi»); `statvfs` presente o no (annotato), `RealPath` assoluto.
**Alla fine:** spegnere la copia remota dalla pagina, cancellare la cartella dati di prova, Stefano toglie (o
tiene) il sottoaccount di prova e la chiave dal pannello. Il download mensile con verifica intera è della 129c.

## Token e tempi

Implementatore Opus: ~260k token, ~90 minuti; correzioni della revisione ~60k token, ~30 minuti (stime).
