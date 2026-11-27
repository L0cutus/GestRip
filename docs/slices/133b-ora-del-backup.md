# 133b — Ora del backup notturno dalla pagina Backup (D-115) e domanda delle foto senza JavaScript (voce 17c)

- **Fonte:** inventario **D-115** («alle 02:30 di default, con l'ora modificabile dal titolare»), piano T5.2 risposta C,
  voce 16 di `docs/domande-aperte.md` (Stefano 27/11/2026, «16 a»); voce 17 della revisione della 133 (Stefano
  27/11/2026: (a) e (b) tenere così, (c) corretta), D-117. Ricevute 124, 125, 126, 129c, 133.
- **Voci:** nessuna voce nuova chiusa; test nuovi con il segno su OPS-28 (`TestCambioOraBordi`).
  **Dipendenze e migrazioni nuove:** nessuna (una chiave nuova di `impostazioni`, `backup.ora_cambiata`).
- **Modelli:** implementatore Opus (~379k token in tre giri, ~50 minuti di orologio), revisore Opus indipendente
  (~197k token in due giri, ~35 minuti), giro del banco Opus (~80k token, ~60 minuti). **Banco:** `man` verde al primo
  giro, completo verde alla ripetizione (vedi in fondo).

## Cosa c'è

- **Pagina Backup** (`internal/web/backup.go`, `pagine/backup.html`): dentro «Stato», sotto «Ora del backup notturno» e
  «Prossimo backup», il campo `backup[ora]` (`type="time"`, `#backup-hour-input`) con «Salva» (`#backup-hour-save`) e la
  riga «Alle 02:30 se non la cambi. Scegli un'ora in cui il negozio è chiuso e l'apparecchio resta acceso. Vale da
  subito: …». `POST /settings/backup/ora`, solo titolare, CSRF dalla catena di `f.rotta`, ritorno alla pagina (PRG) con:
  - «Ora del backup notturno salvata: 23:00, tutti i giorni. Il primo a quest'ora: mercoledì 25/11/2026 23:00.»;
  - oppure, se aspettando l'ora nuova l'ultimo backup avrebbe più di 25 ore, «… Aspettando quest'ora l'ultimo backup
    buono avrebbe più di 25 ore: il prossimo backup parte entro pochi minuti, poi ogni giorno a quest'ora.»;
  - senza lavoro notturno (non configurato, lavori spenti, copia isolata) solo la prima frase: la riga «Prossimo
    backup» dice perché nessuno parte;
  - «L'ora del backup notturno era già questa: niente da cambiare.» / «Ora non valida: scrivi ore e minuti, per esempio
    02:30. L'ora del backup non è cambiata.» / «L'ora del backup non si è potuta salvare: riprova.».
- **`backup.Notturno.SalvaOra`** (`internal/backup/ora.go`): solo «HH:MM» con quattro cifre (00:00–23:59, spazi intorno
  tolti dalla faccia). La stessa ora non scrive niente. Altrimenti, in una transazione: `backup.ora` («HH:MM», la chiave
  della 124), `backup.ora_cambiata` (JSON `il`, `dal`, `previsto`) e l'audit **`backup.schedule_updated`** (risorsa
  `backup`, id `ora`, `from`, `to`, `first_turn`, `due_now`; attore il titolare), nello stile di `jobs.configured`. Non
  prende il lucchetto e non fa partire niente. `importer.ChiaviSoloGo` ha `backup.ora_cambiata`.
- **Calendario** (`statoTurno` in `notturno.go`, usato da backup, prova settimanale e prova mensile): con un cambio, i
  turni prima di `dal` valgono chiusi; per i turni da `dal` in poi contano solo le esecuzioni iniziate dopo il cambio
  (i tentativi contati solo in memoria li dimentica `SalvaOra`). Senza cambio è identico alla 124.
- **Allarme:** invariato, suona sempre oltre le 26 ore (dopo la revisione: la regola del cambio non lascia mai più di
  26 ore fra due backup, quindi non serve zittirlo).
- **Ripristino:** `backup.ora_cambiata` è fra le `ImpostazioniTolteDalRipristino` (l'ora resta).
- **Documenti:** manuale cap. 11 (sezione nuova «Cambiare l'ora del backup», «alle 02:30 se non la cambi», prova
  settimanale «dopo il backup della domenica»), `docs/SVILUPPO.md` (sezione nuova «Backup: ora del backup dalla pagina»,
  124 e 126 corrette), `docs/PIANO.md` (riga del calendario), voce 16 di `docs/domande-aperte.md` decisa. Ricetta
  `backup-pagina` (`tools/banco/browser/manuale.mjs`) aspetta anche `#backup-hour-form`; descrizione in
  `bench/manuale/schermate.json` aggiornata. Il campo sta dentro `#backup-status`, già nel ritaglio.

## La regola dei bordi

Al salvataggio (adesso = C, ora nuova = N), dopo la revisione:

1. **Coperto** se la prima N dopo C (strettamente dopo: un cambio alle 03:00 in punto per le 03:00 guarda le 03:00 di
   domani) cade al più **25 ore** dopo la cattura dell'ultimo backup verificato sulla chiavetta (`LimiteDopoCambio`: le
   26 ore dell'allarme meno un'ora per fare e verificare il backup). Allora il primo backup è quella N (`previsto`) e
   fino ad allora niente.
2. **Non coperto** (più di 25 ore, o nessun backup verificato): il turno di adesso all'ora nuova è da fare, il backup
   parte al giro dopo (recupero, anche di giorno), con tre tentativi nuovi. Così un cambio non lascia mai più di 26 ore
   fra due backup.
3. Per i turni dall'ora nuova in poi contano solo le esecuzioni iniziate dopo C: il backup delle 02:30 di stanotte non
   chiude il turno delle 23:00 nuove di stasera.
4. L'allarme delle 26 ore non cambia.

Esempi (tutti in `TestCambioOraBordi`): 02:30 → 23:00 alle 10:00 dopo il backup della notte (20 ore e mezza): backup
stasera alle 23:00, non subito. 23:00 → 01:00 alle 10:00 (26 ore): **subito**, poi alle 01:00. 23:00 → 01:00 alle 23:30
dopo il backup delle 23:00 (2 ore): un backup in più alle 01:00. 02:30 → 01:00 alle 01:30 (46 ore e mezza): subito (è
notte; aspettare salterebbe la notte). 02:30 → 06:00 alle 10:00 (27 ore e mezza): subito. Lunedì 23:00, martedì alle
11:01 → 11:00 (36 ore): subito. Limite: ultimo backup alle 02:30, cambio alle 03:30 (25 ore esatte) coperto, alle 03:31
subito. Cambio alle 03:00 in punto per le 03:00: primo backup domani alle 03:00; alle 23:00 in punto per le 23:00 (44
ore): subito. Domenica alle 10:00 dalle 23:00 alle 02:30: backup subito e poi la prova settimanale (voce 18(c)). Backup
vecchio con i tre tentativi falliti: subito. Ora legale: sabato 28/03 02:30 → 02:15, il backup parte domenica alle 03:00
CEST e la prova dopo di lui. L'allarme suona a 26 ore e un minuto dall'ultimo backup anche dopo un cambio.

## Prove

- `internal/backup/ora_linux_test.go`: `TestSalvaOra` (13 valori non validi, nessuna scrittura; la stessa ora;
  00:00, 23:59, «02:+5», «+2:05»; audit con attore, from, to, due_now), **`TestCambioOraBordi`** (segno `inventario:
  OPS-28`; i bordi sopra, anche un tentativo in memoria di prima del cambio), `TestCambioOraLegale` (29/03 e prova della
  domenica), `TestSalvaOraCopiaIsolata` (nessuna esecuzione, nessun lucchetto, nessun allarme, niente dovuto),
  `TestCambioOraRovinato` (quattro righe rovinate: il calendario della 124), `TestSalvaOraDimenticaTentativi`.
- `internal/backup/ripristino_linux_test.go`: il ripristino toglie anche `backup.ora_cambiata`.
- `internal/web/backup_ora_test.go` (backup vero senza chiavetta e con il lavoro spento): `TestOraBackupTitolare`
  (campo, token, salvataggio, conferma, audit, la stessa ora), `TestOraBackupRifiuti` (dipendente, senza token, token
  sbagliato, senza sessione, 8 valori non validi e il campo mancante, spazi intorno), `TestMsgOraSalvata`.
- `cmd/gestrip/ripristino_linux_test.go`, `TestServeCopiaIsolata` esteso: sulla copia isolata l'ora si salva
  (`Acceso` false), resta dopo il riavvio, niente dovuto, nessuna riga in `esecuzioni_backup`.
- `internal/importer`: `TestChiaviSoloGoComplete` con `backup.ora_cambiata` e `internal/backup/ora.go`.
- `TestOgniRottaHaIlSuoPermesso`: riga nuova `POST /settings/backup/ora` (solo titolare, OPS-28) nella tabella dei permessi.
- `-race` (CGO=1, `-run` mirato) verde su backup e web.
- `scripts/check` verde: **chiuse 57, rosse 0** (parziali 192, aperte 93, fuori v1 11).

## D-117, senza JavaScript (voce 17c)

- **Senza JavaScript** la domanda «Questa riparazione non ha foto. Vuoi salvarla lo stesso?» mostra solo «Salva senza
  foto» e la riga «Le foto si aggiungono dalla scheda dopo il salvataggio.» (`#intake-no-photos-later`, un `<div>`,
  sotto la domanda, prima del pulsante).
- **Con JavaScript** come nella 133: «Aggiungi foto» chiude la domanda e porta alle foto senza richieste.
- **Come:** «Aggiungi foto» nasce con `hidden` e `app.js` lo mostra a ogni `gs:pagina` (nel gestore della domanda che
  già porta scorrimento e fuoco); la riga ha `data-solo-senza-js`, la convenzione della 056 che `app.js` nasconde a ogni
  pagina. File: `pagine/nuova.html`, `statici/app.js`, `statici/app.css`. Dopo la revisione: `#intake-no-photos-add[hidden]
  { display: none; }` (`.pulsante-contorno` mette `display: inline-flex`, che batteva `[hidden]`); la riga è un `<div>` e
  non un `<p>`, perché lo scenario `domanda-foto` legge l'unico `<p>` della domanda (`banco.mjs` resta non toccato).
  Controllato con Chrome headless su una pagina con l'`app.css` vera, senza script:
  `to-be-deleted-after-use/_slice-133b/correzione/hidden.png` (solo la riga e «Salva senza foto»).
- **`aggiungi_foto=1` lato server: lasciata, innocua.** Il pulsante resta un `submit` con quel nome (il click con
  JavaScript fa `preventDefault`); se arriva lo stesso (pagina vecchia in cache) ridisegna il modulo senza salvare,
  come prima. `TestNuovaAggiungiFoto` resta.
- **Banco:** `tools/banco/browser/banco.mjs` **non toccato**; lo scenario `domanda-foto` aspetta la domanda visibile e
  poi clicca `#intake-no-photos-add`, che a quel punto `app.js` ha già mostrato.
- **Test:** `TestNuovaSenzaFotoSenzaJavaScript` (nuovo: due pulsanti di cui uno solo `hidden`, «Aggiungi foto»; la
  riga prima dei pulsanti; `app.js` lo mostra e nasconde `data-solo-senza-js`; la regola `#intake-no-photos-add[hidden]`
  che batte `.pulsante-contorno`; un solo `<p>` nella domanda);
  `TestNuovaSenzaFotoChiedeConferma` con il pulsante `hidden` e la riga.
- **Voce 17** di `docs/domande-aperte.md` segnata decisa e fra le «Risposte già registrate»; `docs/SVILUPPO.md`
  (sezione della 133) aggiornata.

## Mutazioni

Fatte dall'implementatore con uno script (file copiati e rimessi, test mirati): **21 su 21 uccise**. M1, M11, M13 e
M15 al primo giro non compilavano e sono state rifatte in una forma che compila.

| # | Mutazione | Uccisa da |
|---|---|---|
| M1 | cambio sempre coperto se c'è un backup | `TestCambioOraBordi` |
| M2 | copertura 0 ore invece di 12 | `TestCambioOraBordi` |
| M3 | coperto, ma primo turno = quello di adesso | `TestCambioOraBordi`, `TestCambioOraLegale` |
| M4 | turni prima di `dal` non chiusi | `TestCambioOraBordi`, `TestCambioOraLegale` |
| M5 | le esecuzioni di prima del cambio contano | `TestCambioOraBordi` |
| M6 | i tentativi in memoria di prima contano | `TestCambioOraBordi` |
| M7 | allarme senza attesa dopo il cambio | `TestCambioOraBordi`, `TestAttesaDopoCambioRemoto` |
| M8 | margine 24 ore invece di 2 | `TestCambioOraBordi`, `TestAttesaDopoCambioRemoto` |
| M9 | attesa anche con l'ultimo buono già vecchio al cambio | `TestAttesaDopoCambioRemoto` |
| M10 | copia remota senza attesa | `TestAttesaDopoCambioRemoto` |
| M11 | niente controllo delle quattro cifre («+2:30») | `TestSalvaOra` |
| M12 | la stessa ora riscrive | `TestSalvaOra` |
| M13 | niente audit | `TestSalvaOra` |
| M14 | conferma che promette un backup con il lavoro spento | `TestOraBackupTitolare`, `TestMsgOraSalvata` |
| M15 | rotta aperta a tutto il personale | `TestOraBackupRifiuti` |
| M16 | riga del cambio rovinata che ferma il giro | `TestCambioOraRovinato` |
| M17 | la copia isolata dice «acceso» | `TestServeCopiaIsolata` |
| M18 | «Aggiungi foto» visibile senza JavaScript | `TestNuovaSenzaFotoSenzaJavaScript`, `…ChiedeConferma` |
| M19 | niente riga senza JavaScript | `TestNuovaSenzaFotoSenzaJavaScript`, `…ChiedeConferma` |
| M20 | riga anche con JavaScript (senza `data-solo-senza-js`) | `TestNuovaSenzaFotoSenzaJavaScript`, `…ChiedeConferma` |
| M21 | `app.js` non mostra «Aggiungi foto» | `TestNuovaSenzaFotoSenzaJavaScript` |

### Dopo la revisione

Le mutazioni sopravvissute alla revisione (R1, R2, R5, R8), rifatte sul codice nuovo, e quelle nuove: **10 su 10
uccise**. M1, M2 e M6–M10 non valgono più (codice tolto: finestra di 12 ore, `ultimoGiro`, attesa dell'allarme); M11 e
M19 sono diventate R5 e N5. Le altre della tabella sopra, rifatte, restano uccise.

| # | Mutazione | Uccisa da |
|---|---|---|
| R1 | limite di un minuto oltre le 25 ore | `TestCambioOraBordi` (al limite) |
| R2 | limite esclusivo (`<` invece di `<=`) | `TestCambioOraBordi` (al limite) |
| R5 | la cifra dopo i due punti non controllata («02:+5») | `TestSalvaOra` |
| R8 | la prima ora nuova può essere adesso | `TestCambioOraBordi` (cambio alle 03:00 in punto) |
| N1 | il ripristino non toglie `backup.ora_cambiata` | `TestRipristinoInCartellaNuova` |
| N2 | `SalvaOra` non dimentica i tentativi in memoria | `TestCambioOraBordi`, `TestSalvaOraDimenticaTentativi` |
| N3 | dimentica anche quelli delle prove | `TestSalvaOraDimenticaTentativi` |
| N4 | tolta la regola CSS di «Aggiungi foto» nascosto | `TestNuovaSenzaFotoSenzaJavaScript` |
| N5 | la riga nuova come `<p>` | `TestNuovaSenzaFotoSenzaJavaScript`, `…ChiedeConferma` |
| N6 | allarme zittito 3 ore in più | `TestCambioOraBordi` (26 ore e un minuto) |

## Revisione (Opus indipendente, 16 mutazioni, 12 uccise; 2 ALTA, 2 MEDIA, 3 BASSA)

- **ALTA, «Aggiungi foto» visibile senza JavaScript:** `.pulsante-contorno { display: inline-flex }` batteva `[hidden]`.
  Corretto con `#intake-no-photos-add[hidden] { display: none; }`; il test controlla la regola e la domanda.
- **ALTA, scenario `domanda-foto` rotto:** due `<p>` nella domanda (strict mode di `domanda.locator('p')`). La riga nuova
  è un `<div>`; `banco.mjs` non toccato; il test conta un solo `<p>`.
- **MEDIA, manuale sui 12 ore scritto male:** la regola delle 12 ore non c'è più (vedi sotto); il manuale descrive quella
  nuova delle 25 ore.
- **MEDIA-BASSA, ripristino:** `backup.ora_cambiata` in `ImpostazioniTolteDalRipristino`, con il test.
- **Difetto, fino a 36 ore senza backup:** la finestra di 12 ore prima dell'ultima ora nuova passata lasciava fino a 36 ore
  (lunedì 23:00 → martedì 11:01 alle 11:00 → mercoledì 11:00) e l'allarme zittito fino al primo backup + 2 ore. Regola
  nuova: coperto solo se la prima ora nuova cade al più 25 ore dopo l'ultimo backup verificato, altrimenti subito;
  allarme mai zittito (tolti `CoperturaPrimaDellOra`, `ultimoGiro`, `MargineDopoCambio`, `attesaDopoCambio`).
- **BASSA:** (a) «02:+5» rifiutato (cifre controllate in tutte e quattro le posizioni, caso nel test); (b) testo della
  pagina Backup: «Ogni giorno, all'ora del backup qui sotto, …» e «Dopo il backup della domenica prova anche un
  ripristino completo.»; (c) tentativi in memoria di prima del cambio: `SalvaOra` li dimentica (solo quelli dei backup),
  il filtro in `statoTurno` è tolto.
- **Test sui bordi:** limite esatto e un minuto oltre, cambio esattamente all'ora nuova (coperto e no).
- **Dubbi di prodotto (a) e (c):** voce 18 di `docs/domande-aperte.md`, con le raccomandazioni.

## Scelte da confermare

1. **Nessun limite sull'ora** (anche in orario di negozio): solo l'invito nella pagina a scegliere un'ora con il negozio
   chiuso. Il backup di giorno pesa come «Esegui adesso».
2. **Limite di 25 ore** (`LimiteDopoCambio` = 26 − 1): un'ora lasciata al primo backup all'ora nuova per partire e
   verificarsi prima dell'allarme. Con 26 ore esatte l'allarme si accenderebbe per i minuti del backup.
3. **Un backup in più è possibile** (per esempio 23:00 → 01:00 subito dopo il backup delle 23:00: un altro alle 01:00):
   innocuo, come il 25/10 della 124; si è preferito a un buco o a un allarme falso.
4. **Non coperto ⇒ backup entro pochi minuti, anche di giorno** e tre tentativi nuovi anche se il turno vecchio li
   aveva finiti (voce 18(a)); di domenica parte subito dopo anche la prova settimanale (voce 18(c)).
5. **Copia isolata:** l'ora si salva (come le impostazioni dei promemoria), non parte niente; vale dopo la conferma e il
   riavvio.
6. **Solo «HH:MM» dalla pagina**; la chiave continua ad accettare anche «HH:MM:SS» scritta a mano (124).
7. **17(c):** `aggiungi_foto=1` lasciato lato server (innocuo) invece di toglierlo.

## Per il manuale

- Nella pagina Backup il titolare cambia l'ora del backup notturno nel campo «Ora del backup notturno (ora italiana)» e
  preme «Salva». Senza cambi resta alle 02:30. La pagina conferma con il giorno e l'ora del primo backup all'ora nuova.
- Cambiare l'ora non salta il backup della notte. Se la prima ora nuova arriva al più 25 ore dopo l'ultimo backup buono
  si aspetta quella; altrimenti il backup parte entro pochi minuti, anche di giorno.
- Il riquadro rosso «più di 26 ore» non cambia.
- La prova settimanale resta legata al backup della domenica, alla nuova ora.
- Nuova riparazione senza foto e senza JavaScript: la domanda ha solo «Salva senza foto» e dice che le foto si
  aggiungono dalla scheda dopo il salvataggio. Con JavaScript (l'apparecchio del negozio) c'è anche «Aggiungi foto».
- Fatto in questa slice: cap. 11 aggiornato; la PNG `11-backup-pagina-desktop.png` la rifà il giro `man`.

## Differenze dallo standalone

- Lo standalone non ha l'ora del backup (timer di otto-vps alle 02:30, fuori dal pacchetto): tutto solo Go.

## Banco

Due giri (`gestrip-standalone:fab-225`, `sintetici-import`, «memoria libera» del coordinatore, guardia sotto 10 GB mai
scattata, minimo 18,9 GB; ogni `banco giu` «AVANZI: nessuno»):

- **`man` verde al primo giro:** 41 ricette, comprese quelle con `senzaFotoGiaDetto` della 133 (prima prova di
  `manuale.mjs` cambiato nella 133). `11-backup-pagina-desktop.png` rifatta: stato e, sotto, il campo «Ora del backup
  notturno (ora italiana)» con «Salva» (il campo mostra «02:30 AM» perché il Chromium del banco ha solo l'interfaccia
  inglese). `04-stampanti-desktop.png` rifatta e tenuta: ora mostra la scheda «Backup» accanto a «Stampanti» (la
  vecchia era della 118, prima della pagina Backup). Rimesse com'erano quattro PNG con sole differenze del giorno.
- **Completo:** `s133bb1` caduto sulla nota 8 (500 dello standalone su `GET /repairs/88` in `storico-correzioni[1]`,
  «Database busy»; `domanda-foto` pulito), unica ripetizione; **`s133bb2` verde**: HTTP 91 scenari, 1373 passi, 0
  inattese; browser 39 scenari, 78 viste, 0 inattese, 0 errori; `domanda-foto` verde con le 44 righe `NOTA standalone`
  attese. `scripts/check` con `GESTRIP_BANCO_ESITO`: voci 353, chiuse 233, rosse 0, parziali 16.

## Note restate dalla revisione (BASSA, non corrette)

- Cambio coperto al limite (prima ora nuova 25 ore dopo l'ultimo backup): il backup ha 1 ora, invece di 2, per finire
  la verifica prima delle 26 ore; un backup più lungo di un'ora dà l'allarme rosso per pochi minuti (succede già la
  notte del ritorno all'ora solare).
- Senza nessun backup verificato il messaggio «Aspettando quest'ora… avrebbe più di 25 ore» (`internal/web/backup.go`)
  è impreciso; il backup parte comunque subito.

## LSP

2 chiamate. `findReferences` su `statoTurno`: utile, conferma che i soli chiamanti sono `dovuto`, `provaDovuta` e
`provaRemotaDovuta` (quindi le prove seguono la regola del cambio senza altro codice). `goToImplementation` su
`BackupDelNegozio`: poco utile (ha dato solo `Notturno`, non il finto dei test, trovato con grep).

## Difetti trovati per strada (non corretti)

- `leggiOraBackup` (124) accetta segni: «+2:30» vale 02:30 e «-0:30» vale 00:30 (`strconv.Atoi` su due caratteri).
  `TestOraBackup` dice «+2:30 → predefinita» e passa solo perché +2:30 è proprio 02:30. Dalla pagina non arriva (la 133b
  controlla le cifre); resta per una riga scritta a mano.
- `ProssimoGiro` il giorno del salto dell'ora legale dà un'ora inesistente spostata in avanti (03:15 per le 02:15),
  mentre il backup parte alle 03:00: è solo il testo della pagina e del messaggio (già scritto nel commento della 126).
