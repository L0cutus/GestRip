# 037b — Risposte di Stefano alle domande delle slice 033–037

- **Fonte:** decisioni di Stefano del 01/11/2026 (`docs/domande-aperte.md`, «Risposte già
  registrate», domande 1, 5, 6, 8, 9, 13, 14); inventario D-44 estesa, D-45, D-46, D-47, RIP-S-30.
- **Voci:**
  - toccate e coperte da test nuovi: RIP-S-01 (D-44 estesa), RIP-S-13 (D-45), RIP-S-24 (D-46),
    RIP-S-30 (al più 2 processi delle immagini), RIP-S-33 (D-47);
  - toccate dai test cambiati: RIP-S-04, 06, 14, 15 (ritorno alla scheda con il segno);
  - nessuna voce cambia stato: RIP-S-13, 30 e 33 restano parziali per le parti già scritte
    (email di pronta, galleria della home).
- **Standalone:** non letto: le decisioni erano già prese e non serviva altro.
- **Lavoro:** implementazione e mutazioni di un sottoagente; review e mutazioni di un sottoagente
  diverso; `scripts/check` e banco rilanciati dalla sessione principale dopo le correzioni della
  review: check verde; banco (`fab-219`) verde con `-dati sintetici-import` e su database vuoto,
  21 scenari e 310 passi, «AVANZI: nessuno». `go-baseline` è stato spostato alla 220 dopo questi
  giri: la 037b non dipende dalla 220, l'allineamento è la 037d.

## Cosa c'è

- **Segno della scheda mostrata senza un'apertura** (`internal/web/segni.go`, D-44 estesa):
  - invariante (D-44 come applicata dalla sessione principale, 01/11/2026): ogni risposta che
    mostra i dati della scheda scrive la riga della propria azione o `repair.viewed` (con il segno
    «letto»), a meno che la richiesta porti un segno monouso valido del server;
  - nessuna riga per la scheda mostrata dopo il redirect di un'azione riuscita (modifica salvata,
    togli data, cambio stato, riapertura, attesa, revoca, nuovo link) e per i link «Modifica» e
    «Annulla»;
  - «rifiuti compresi» vale per i rifiuti mandati dalla pagina: ogni modulo della scheda porta il
    segno della pagina nel campo nascosto `mostra`. Un rifiuto, un'azione che non cambia niente o
    un guasto senza riga propria (azione non valida, stato uguale D-45, transizione non consentita,
    rifiuto al dipendente di `azioneDelTitolare`, nuovo link senza indirizzo D-46, modulo della
    modifica e della riapertura ridisegnati, togli data, revoca, nuovo link, attesa e riapertura
    non riusciti) torna o ridisegna senza riga solo con quel segno; senza segno, o con un segno
    falso, usato, di un'altra sessione o busta, scaduto, la scheda mostrata è un'apertura vera, e il
    redirect non ha il segno. Se quell'apertura non si scrive, home con «Impossibile aprire la
    riparazione.»;
  - il segno è un identificativo casuale di 22 caratteri nel parametro `mostra`, che vale solo con
    una voce in memoria legata a utente, nonce della sessione e busta, e si consuma alla prima GET
    di quella busta (la GET nel parametro, il POST nel campo). Lo mettono solo il server: nel
    redirect di un'azione riuscita, o di un rifiuto che portava il segno della pagina (2 minuti), e
    la pagina della scheda nei link e nei moduli (1 ora), mai oltre un'ora dall'ultima riga della
    catena (review); al più 64 per sessione, nessuno per una HEAD;
  - aperture vere, con la riga: ricaricare, riusare il segno, un segno inventato o vuoto, di
    un'altra sessione o di un altro utente, di un'altra busta, scaduto, una HEAD, `?modifica=1` o
    `?edit=…` scritti a mano. Un segno chiesto da un'altra sessione o per un'altra busta non si
    consuma. Pieno il deposito (10 000 segni), il link resta senza segno;
  - perché nell'indirizzo e non nel cookie: il cookie firmato si può rimandare uguale, e il segno
    varrebbe di nuovo; legato all'indirizzo vale solo per quella navigazione (un redirect non seguito
    non toglie la riga all'apertura dopo). Lo stesso schema di `?esito=` (031, 035);
  - regole in `docs/SVILUPPO.md` («Scheda della busta: aperture e mostre»): ogni azione torna con
    `f.allaScheda`, ogni disegno passa da `f.disegnaScheda`, la GET legge con `f.mostraOApri`.
- **Lettura senza audit stretta:** `buste.Scheda` (c'era già) è documentata come la sola lettura
  della scheda senza `repair.viewed`; `TestLetturaSenzaAuditSoloDaiChiamantiNoti` legge i sorgenti
  di tutto il repo e ammette solo `mostraOApri`, `leggiDopoRifiuto`, `cambiaStato` e
  `proprietarioDellaBusta`.
- **D-45:** «Cambia stato» verso lo stato che la busta ha già torna alla scheda in silenzio, senza
  audit né cambiamenti, anche su una busta chiusa; gli altri rifiuti restano.
- **D-46:** senza `indirizzo_pubblico` «Genera nuovo link» rifiuta prima di toccare qualunque cosa
  (vecchio link, iscrizioni Telegram, email ed email in coda, esito non ancora visto, audit), con
  «Nuovo link cliente|dell'oreficeria non generato: manca «indirizzo_pubblico = https://host[:porta]»
  in <file>, poi riavvia gestrip serve con «sudo systemctl restart gestrip».». Ordine: ruolo (la
  catena di `azioneDelTitolare`), id, indirizzo, busta (che non c'è: home), come «Nuovo link» degli
  inviti senza `indirizzo_interno`. Tolto dalla scheda il riquadro «Nuovo link … non mostrato»,
  diventato irraggiungibile; la home della 031 resta com'è.
- **Domanda 13:** al più `imaging.MaxFigli` = 2 processi `gestrip image-worker` insieme in tutto il
  processo (un semaforo del pacchetto, per ogni `Lavoratore`): chi aspetta smette se il contesto
  finisce, e il suo tempo di 10 s parte quando ha il posto. Il commento della memoria (avvio più
  1440 MiB) era giusto; aggiunto il caso peggiore con due figli.
- **D-47:** nella rotta delle foto del personale il tipo che non viene dai byte si serve solo se
  `mime.ParseMediaType` lo legge come `image/…` (minuscolo, senza parametri) e non è
  `image/svg+xml`; altrimenti `application/octet-stream`.
- **Banco:**
  - passi `solo_go` (`docs/slices/004-banco.md`): girano solo sul Go con una D-xx approvata,
    attese esatte, mai coperti da deviazioni; possono seguire `{location}` e `{href:<id>}` della
    risposta di prima;
  - passi `solo_standalone` e `{mostra}` nel corpo di un passo solo_go (il segno della pagina);
  - scenario `viste` (D-44): il titolare apre la 5/26; solo sul Go un rifiuto con il segno della
    pagina (nessuna riga) e un rifiuto forgiato con il token della home (una riga: lo standalone,
    per la stessa riga, apre la scheda in un passo solo_standalone); il dipendente con «Revoca link»
    e il segno (nessuna riga); il totale di `repair.viewed` si confronta prima e dopo.

## Differenze dichiarate

- Il segno è nell'indirizzo (`?mostra=`, e `?esito=…&mostra=…` per il nuovo link): l'indirizzo
  della scheda dopo un'azione non è più `/repairs/<id>`.
- Un rifiuto senza il segno della pagina (un POST forgiato o un modulo vecchio) mostra la scheda
  con una riga `repair.viewed`; nello standalone il rifiuto è un evento della pagina già aperta.
- D-47 esclude anche `image/svg+xml` (dubbio 2).
- Senza `indirizzo_pubblico` il pulsante «Genera nuovo link» resta; il rifiuto lo spiega.

## Prove

- **TDD:** rosso misurato prima del codice sugli stub (`to-be-deleted-after-use/slice-037b/prove/`):
  primo giro 25 test rossi in `internal/web` e `internal/imaging` (`rosso-1.txt`); giro della regola
  dei rifiuti, 13 casi rossi di `TestRifiutoSenzaSegnoDellaPaginaScriveLaRiga` (`rosso-2.txt`).
  I test del banco sono stati scritti dopo il codice: rosso misurato su copie con i rami nuovi
  spenti (`banco-rosso-stub.txt`, 3 test; `banco-rosso-stub-2.txt`, 1 test). Il caso «riapertura
  non riuscita» e `TestRifiutoSenzaSegnoConLaperturaCheNonSiScrive` sono venuti dalle mutazioni
  vive: il loro rosso è la mutazione uccisa.
- **Test nuovi:** `TestSchedaDopoUnAzioneSenzaRiga`, `TestSegnoDellaSchedaNonSiForgia`,
  `TestModificaEAnnullaSenzaRiga`, `TestRifiutoSenzaSegnoDellaPaginaScriveLaRiga` (14 rifiuti, con e
  senza segno, più segno di un'altra busta, usato, scaduto, inventato),
  `TestRifiutoSenzaSegnoConLaperturaCheNonSiScrive`, `TestPaginaStatoUgualeInSilenzio`,
  `TestNuovoLinkSenzaIndirizzoPubblico`, `TestLetturaSenzaAuditSoloDaiChiamantiNoti`,
  `TestDepositoSegni`, `TestTipoDellaRigaSoloSeImmagine`, `TestAlPiuDueFigliInsieme`,
  `TestAttesaDelPostoRispettaIlContesto`, `TestPassiSoloGo`, `TestPassiSoloGoRossi`,
  `TestLeggiScenarioSoloGo`, `TestPassiSoloStandaloneESegnoDelModulo`,
  `TestScenarioVisteSuiDueDati`. Cambiati: i ritorni alla scheda di `stati_test`, `modifica_test`,
  `link_test`, `ruoli_test` (il dipendente forzato dalla home torna senza segno), `scheda_test`; il
  browser dei test manda il segno della pagina con i POST a `/repairs/…` come un modulo
  (`senzaSegno` per un POST forgiato); `TestPaginaStatoUguale` diventa `TestPaginaStatiCompraOro`;
  `TestPaginaNuovoLinkOreficeriaESenzaIndirizzo` perde la parte senza indirizzo.
- **Mutazioni:** primo giro 63, ne compilano 59, uccise 52; giro della regola dei rifiuti 28, ne
  compilano 24, uccise 20. Delle 11 vive: 6 equivalenti (4 difese doppie tolte dal codice: nonce e
  segno vuoti, utente assente due volte; segnaposto in testa a un passo comune; segno vuoto in
  `mostraOApri`); 5 per test mancante (durata dei segni dei link, Location dopo un passo comune,
  riapertura non riuscita senza segno, apertura che non si scrive nei due ridisegni) coperte e
  uccise. Il posto non reso dei figli ora si vede in 10 s, non con il timeout di `go test`.
- **Banco** (`fab-219`, `to-be-deleted-after-use/slice-037b/banco.sh`): verde con
  `-dati sintetici-import` e su database vuoto, 21 scenari e 310 passi, «AVANZI: nessuno». Prove
  negative (`banco-negativo.sh`), sui dati importati:
  - `rifiuto-senza-riga` (un rifiuto senza segno torna con il segno): 2 differenze, la Location
    del forgiato con `mostra=` e `viste[22] visibile:audit-total: standalone "151 eventi", go "150
    eventi"`, banco rosso;
  - `riga-dopo-azione` (`mostraOApri` apre sempre): `viste[22]` 151 contro 153, banco rosso.
- `scripts/check` verde (senza l'esito del banco: 24 chiuse, 46 parziali, 0 rosse).
- **Solo ragionato:** che lo standalone non scriva righe per gli eventi LiveView (D-44); che un
  segno nel cookie si potrebbe rimandare.

## Review indipendente

Nessuna ALTA. Mutazioni della review: 56 scritte, 50 compilano, 47 uccise; vive M22 (`FormValue`
al posto del corpo), M42 (segno tolto da `repair-waiting-reply-form`), M44 (da
`repair-reopen-form`), coperte e uccise nel giro delle correzioni.
- **MEDIA:** catena dei segni senza tetto: seguendo «Modifica» e «Annulla» a turno si rileggeva la
  scheda per sempre con una riga sola → la voce del segno porta l'inizio della catena (l'ultima riga
  della sessione per la busta: apertura vera o azione riuscita); un segno di una pagina mostrata
  senza riga lo eredita e scade al più tardi un'ora dopo l'inizio; oltre, link, moduli e rifiuti sono
  aperture vere e fanno ripartire la catena (`TestCatenaDeiSegniDuraUnOra`, dallo scenario della
  review: 25 ore di clic ogni 50 minuti danno 16 righe; `TestAzioneRiuscitaFaRipartireLaCatena`,
  `TestDepositoCatena`).
- **BASSA:**
  - deposito riempito da una sola sessione → al più 64 segni per sessione, oltre si toglie il più
    vecchio di quella sessione; nessun segno per una HEAD (`TestDepositoTettoPerSessione`,
    `TestNessunSegnoPerUnaHEAD`);
  - ogni modulo della scheda con il segno → `TestOgniModuloDellaSchedaHaIlSegno` (gli 8 moduli letti
    dalle pagine), uccide M42 e M44;
  - `mostra` ripetuto nel corpo, query che non conta → `TestSegnoDelModuloUltimoDelCorpo`, uccide M22;
  - banco: dopo un errore di un passo solo_go la risposta di prima si azzera
    (`TestSoloGoDopoUnErrore`).
- **Prove del giro:** rosso misurato sugli stub prima del codice (`prove/rosso-3.txt`: 6 test web e
  1 del banco; i test di M22, M42 e M44 sono rossi sulle loro mutazioni). Mutazioni sulle
  correzioni: 18 (15 nuove più M22, M42, M44), tutte compilano, tutte uccise; un controllo doppio
  (inizio zero, già escluso dalla scadenza) è tolto dal codice.
- **Banco dopo le correzioni:** verde su database vuoto e con i dati importati, 21 scenari e 310
  passi, «AVANZI: nessuno»; prove negative rosse come prima (`rifiuto-senza-riga`, ora con un
  segno di catena nuova per il rifiuto forgiato: 2 differenze; `riga-dopo-azione`: 151 contro 153).
  `scripts/check` verde.

## Dubbi per Stefano

Il dubbio dei rifiuti senza riga è chiuso dalla regola della sessione principale (sopra). Restano,
registrati dalla sessione principale:
1. **SVG nella D-47:** escluso anche `image/svg+xml`, più stretto della decisione.
2. **Durate e tetto dei segni** (2 minuti, 1 ora, 10 000): scelte tecniche; oltre l'ora un modulo o
   «Annulla» della pagina vale come apertura, con la riga.
3. `?edit=prezzo` e `?edit=preventivo` dal popup del QR (T4.2) restano aperture con la riga.
