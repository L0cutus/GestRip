# 131 — Conferma «questa è ora la macchina vera» (T5.2, quindicesima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`), riga 131, risposta **I**; inventario
  OPS-19, D-12 e **D-118** (26/11/2026: (a), (b), (d) approvate da Stefano, (c), (e), (f) dal coordinatore); voce 14 di
  `docs/domande-aperte.md`; ricevuta 130.
- **Voci:** OPS-19 (**chiude**). Nessuna D nuova. **Dipendenze e migrazioni nuove:** nessuna.
- **Modelli:** implementatore Opus (~372k token in tre giri, ~43 minuti di orologio), revisore Opus indipendente
  (~147k token, ~10 minuti). **Banco:** non serve (vedi in fondo).

## Cosa c'è

- **`backup.ConfermaRipristino`** (`internal/backup/conferma.go`), l'unico punto della conferma, in una transazione
  `immediate`: prima di scrivere controlla che la copia sia ancora isolata (`ErrNonIsolata`), la parola `CONFERMO`
  (maiuscole/minuscole e spazi ai bordi indifferenti, `ErrParolaSbagliata`) e una scelta per **ogni** messaggio in coda
  adesso, né di più né di meno, id sconosciuti o non validi compresi (`ErrCodaCambiata`). Poi: cancella gli scartati
  (audit `outgoing.queued_discarded`, `reason: restore_confirmed`), lascia in coda i «manda», rimette
  `canale_email_acceso`, `canale_telegram_acceso`, `backup.remoto` dal campo `spento` **solo se mancano** (`DO NOTHING`:
  un interruttore toccato a mano durante l'isolamento resta com'è), toglie `ripristino.isolato`, scrive l'audit
  **`backup.restore_confirmed`** (archivio, SHA-256, ora della copia e del ripristino, cosa è stato riacceso, conteggi).
- **Pagina** `GET/POST /settings/backup/ripristino` (`internal/web/ripristino.go`, `pagine/ripristino.html`), solo
  titolare con CSRF: dice di spegnere o staccare dalla rete la macchina vecchia, elenca i messaggi in coda con
  «Scarta» (già scelto; senza scelta vale scarta) o «Manda» (nota: con il canale spento resta in coda finché non lo si
  riaccende), chiede la parola. Dopo una conferma già fatta (anche da console) dice «Ripristino già confermato:
  riavvia GestRip.».
- **Console** `gestrip conferma-ripristino [-db FILE]` (`cmd/gestrip/conferma_ripristino.go`): per ogni messaggio
  «manda o scarta? [Invio = scarta, m = manda]», poi la parola; coda presa all'inizio, se cambia la conferma rifiuta.
  Alias **`gestrip ripristina`** per `restore`.
- **Riavvio, non a caldo:** il `serve` partito isolato resta isolato fino al riavvio; pagina, banner e console dicono
  di riavviare GestRip. Così il primo giro della coda lo fa il serve nuovo, senza gli scartati. I comandi `backup`,
  `backup-remoto`, `prova-backup` funzionano subito dopo la conferma.
- **Sottochiavi** (`db.SottoChiaviDiServe`, `internal/db/chiave.go`): sessione, CSRF e galleria dalla chiave nuova;
  link email (conferma e disattivazione) e link artigiano `g1.` da `chiave_server_del_backup`, se c'è (già durante
  l'isolamento, così le email accodate e poi mandate hanno link validi); senza, tutto come prima.
- **Durante l'isolamento:** banner «Questa è una copia isolata» (`guscio.go`, `base.html`, `app.css`) per tutto il
  personale, con il link per il titolare e «chiama il titolare» per gli altri, non sulla firma (tablet al cliente) né
  sulla pagina della conferma; il banner legge solo `db.LeggiIsolamento`. «Esegui adesso» dei promemoria rifiuta
  (`internal/jobs/jobs.go`, `promemoria.go`). L'assistente usa solo un modello **di questa macchina**
  (`internal/assist`: URL verso 127.0.0.1, localhost o ::1, e `trasportoSoloLocale` che collega solo se ogni IP
  risolto è di loopback, contro un `localhost` dirottato da `/etc/hosts`); vale anche per la trascrizione.

## Prove

- **`TestConfermaRipristinoFinoAlServeNormale`** (segno `inventario: OPS-19`), da capo a fondo: macchina vera con bot
  acceso, 3 messaggi in coda, link artigiano ed email firmati prima del backup → backup → ripristino → serve isolato (0
  richieste al Bot API finto, «Esegui adesso» rifiuta, modello remoto spento, banner) → conferma da console con serve
  acceso (ancora 0 richieste, pagine pubbliche in manutenzione) → riavvio: arriva solo il messaggio «manda», ripartono
  lavori, notturno e avvisi; link vecchi 200, un gettone di un'altra chiave no; sessione, CSRF e galleria non dalla
  chiave vecchia.
- `TestConfermaRipristino*` (giusta, 8 rifiuti che non cambiano niente, a metà con l'audit rotto da un trigger,
  interruttore toccato a mano, macchina mai ripristinata), `TestConfermaRipristinoComando`, `…Stop`, `…GiaFatta`,
  `…DallaPagina` (banner per ruolo, assente su firma e conferma, CSRF, righe non valide), `TestSottoChiaviDiServe`,
  `TestEseguiAManoNellaCopiaIsolata`, `TestSoloLocaleNellaCopiaIsolata`, `TestSoloLocaleDialer`,
  `TestConnettiSoloLocale`. `TestServeCopiaIsolata` della 130 resta verde.
- `-race` (CGO=1, `-run` mirato) verde su cmd, backup, web, db, jobs, assist. `scripts/check` verde rilanciato dal
  capo-slice: **chiuse 57, rosse 0**.

## Revisione (Opus indipendente, 19 mutazioni, 16 uccise; nessuna ALTA, 2 MEDIA, 6 BASSA)

- Controllati e a posto: nessun invio prima della conferma né di righe scartate; doppia conferma (pagina + console)
  innocua (`immediate` + `ErrNonIsolata`); righe arrivate dopo la pagina rifiutate; conferma tutto o niente; la chiave
  vecchia non entra mai in sessione/CSRF/galleria; 28 indirizzi provati sul «locale» del modello senza falsi locali.
- **MEDIA 1** banner sulla firma senza test: corretto. **MEDIA 2** lavori del giorno rifatti dopo la conferma (solo al
  negozio): dubbio di prodotto, **voce 15** di `docs/domande-aperte.md`.
- **BASSA** corrette: banner che leggeva tutta la coda, messaggio dopo una conferma già fatta, nota su «Manda» con il
  canale spento, id non validi accettati, `localhost` risolto non verificato. Restano: controllo della lunghezza della
  chiave del backup irraggiungibile (CHECK della tabella), falsi «remoto» innocui (`LOCALHOST`, `127.0.0.2`).
- **Mutazioni delle correzioni:** 4 su 4 uccise (firma nel banner, riga non valida ignorata, dialer senza
  `IsLoopback` — sopravvissuta al primo giro, uccisa con `TestConnettiSoloLocale` —, «già confermato»).
  L'implementatore ne aveva fatte prima 15, tutte uccise.

## Scelte prudenti dell'implementatore (da confermare, voce 14)

Parola `CONFERMO`; interruttori rimessi solo se mancano; link email e artigiano dalla chiave del backup già durante
l'isolamento; riavvio a mano dopo la conferma (fra conferma e riavvio «Esegui adesso» rifiuta; le righe accodate in
quel tempo partono al riavvio, sono fatte dopo la conferma: da dire nel manuale e nella guida della T5.4); il dettato
senza modello locale dice «Nessun modello è configurato».

## Per il manuale

- Dopo un ripristino, in alto su ogni pagina compare un riquadro giallo «Questa è una copia isolata». Il titolare ha il
  link per confermare; gli altri vedono «chiama il titolare».
- La conferma è in Impostazioni → Backup → ripristino, oppure dalla console con `gestrip conferma-ripristino`.
- Prima di confermare: spegni la macchina vecchia, o staccala dalla rete.
- La pagina mostra i messaggi rimasti in coda. Per ognuno c'è «Scarta» (già scelto) o «Manda»; nel dubbio si lascia
  «Scarta». Un messaggio «Manda» di un canale spento aspetta finché il canale non si riaccende.
- Per confermare si scrive CONFERMO. Una parola sbagliata non cambia niente.
- Dopo la conferma bisogna riavviare GestRip: solo allora ripartono email, Telegram, promemoria e backup.
- I link già mandati ai clienti (conferma o disattivazione dell'email) e agli artigiani continuano a funzionare. Chi
  era collegato deve rientrare.
- Finché la copia è isolata, «Esegui adesso» dei promemoria non parte e l'assistente usa solo un modello di questa
  macchina.

## Differenze dallo standalone

- Lo standalone non aveva un comando per mettere online una copia isolata: per un disastro si ripristinava di nuovo
  con `--live`. Il Go conferma la copia isolata (D-12, D-118).

## Banco

Non serve: ripristino, conferma e isolamento sono solo Go e il banco (standalone contro Go sugli stessi dati) non li
esercita; le prove sono test da capo a fondo con `serve` di prova e un Bot API finto.

## LSP

Implementatore 3 `findReferences` (`ChiaveServer`, `EseguiAMano`, `LeggiEstrazione`), revisore 2: utili per i chiamanti,
ma con l'indice vecchio sui file appena toccati (mancavano usi): lì è più affidabile grep.
