# 109 — Trascrizione, parte server (T4.10, nona)

- **Fonte:** standalone a `go-baseline`: `lib/gestrip_standalone/assist.ex` (`transcribe/3`, `@min_audio_bytes`,
  `check_clip/1`, `check_container/1`, `ask/3`, `audit/5`), `assist/transcriber.ex`, `assist/audio.ex` (`@containers`,
  `normalise/1`, `container?/1`), `assist/providers/local_whisper.ex` (`@timeout`, intestazioni, `interpret/1`),
  `assist/glossary.ex` (`for_wire/1`), `assist/http.ex`, `lib/gestrip_standalone_web/live/workshop_live.ex` (`allow_upload(:dictation)`
  con `max_entries: 1` e `max_file_size: 26_214_400`, `handle_dictation/3`, `append_dictated/2`, `assist_refusal/2` per `:stt`),
  `test/gestrip_standalone/assist/providers_test.exs` (LocalWhisper). PIANO §1.8 (silenzio ai bordi), piano T4.10 riga 109 e
  domanda G.
- **Voci:** RIP-H-44 (parte server), RIP-H-46 (parte trascrizione), RIP-H-47: **restano parziali**, niente segno
  `// inventario:` (il microfono, la pagina e il testo aggiunto alla casella sono della 110). Nessuna D nuova.
  **Deviazione dal piano, approvata dal coordinatore il 20/11/2026:** nessuno scenario HTTP del caricamento della clip; il
  confronto nelle due app lo fa uno scenario browser della 110, dopo `assistente-salva` (Dubbio 1 della prima stesura,
  strada (b); la riga 110 del piano è già aggiornata). Nella 109 restano il giro di controllo s109a e i test Go.

## Cosa c'è

- **`internal/assist/trascrizione.go`** (nuovo): `Transcriber`, `ServerEsterno` (`{endpoint}/stt`), `TrascrittoreFinto`,
  `TipoAudio`/`ContenitoreAmmesso`, i rifiuti (`ErrTrascrizioneNonConfigurata`, `ErrClipCorta`, `ErrFormatoAudio`,
  `ErrTrascrittore`), `MessaggioDiTrascrizione`, `Trascrizione` e **`Impostazioni.Trascrivi`** (configurazione riletta,
  rifiuti che non costano, motore, audit `assist.transcribe` con soli conteggi). `registraAudit` è ora comune con l'audit
  `assist.extract` della 106 (`modello.go`).
- **`internal/assist/silenzio.go`** (nuovo): `ConSilenzio`, mezzo secondo di silenzio ai bordi di un WAV PCM a 16 bit, per il
  motore locale della T5.1.
- **`internal/assist/impostazioni.go`:** `Impostazioni` ha `AttesaTrascrizione`, `Trascrittore` e `Registro` (`cmd/gestrip`
  gli dà il registro della faccia interna).
- **`internal/web/trascrizione.go`** (nuovo): `POST /nuova/trascrivi` (personale, `rottaConFile`); **`assistente.go`:**
  l'interfaccia `Assistente` ha `Trascrivi`; **`salute.go`:** la rotta.
- **`tools/banco/modellofinto`:** il trascrittore finto nello stesso server (e container) del modello finto: `/stt`,
  `/_prova/risposte-stt`, `/_prova/richieste-stt`, `ProgrammaSTT`, `RichiesteSTT`.
- **SVILUPPO:** sezione nuova «Trascrizione, parte server (slice 109…)».

## Scelte

1. **Il motore è scelto da `Impostazioni.Trascrittore`** (nil = `ServerEsterno`). Il motore locale
   (sidecar, T5.1) sarà un altro `Transcriber`.
2. **Il silenzio non tocca il server esterno.** Lo standalone manda la clip com'è (webm, ogg…) e il trascrittore della bottega
   la decodifica da sé; il silenzio del PIANO §1.8 è stato misurato con Parakeet. `ConSilenzio` lavora sul WAV PCM (D-25, il
   formato che la 110 farà registrare) e la userà il motore locale; la parte che serve ora è solo la funzione, con i test
   (domanda G).
3. **Il glossario non si taglia all'invio**, come `for_wire/1`: il limite di 2000 caratteri lo fa rispettare il salvataggio
   della 107 (vedi Dubbi 3).
4. **Attesa di 35 s su tutta la richiesta** (in Go una scadenza unica; hackney la usa come `recv_timeout`, il tempo massimo fra
   due pezzi della risposta). Per una clip di qualche secondo è lo stesso.
5. **Rotta e risposta, che lo standalone non ha (LiveView):** `POST /nuova/trascrivi`, multipart con il campo `dictation` (il
   nome dell'upload dello standalone), risposta **JSON sempre 200** con `tipo` `ok`/`error` (i due tipi del messaggio sotto la
   casella), `messaggio` e `testo`, `Cache-Control: no-store` (porta il testo del cliente). Il testo **lo aggiunge la pagina**
   (110) in fondo alla casella: la casella può essere lunga 5000 caratteri e i campi di testa di `csrf.ModuloConFile` sono al
   più 4 KiB, quindi mandarla al server per l'unione non si può.
6. **Oltre 25 MiB è il 413** («Request Entity Too Large» della catena): lo standalone rifiuta la voce dell'upload senza
   dire niente sotto la casella (l'inventario lo segnala come non verificato); la pagina della 110 deciderà che cosa mostrare.
7. **Un modulo senza file è una clip vuota** («⚠ Registrazione troppo breve.», o «Nessuna trascrizione è configurata…» prima):
   così la risposta ha le intestazioni di una pagina (il 400 della catena è quello di Plug.Parsers, senza HSTS, e il test delle
   intestazioni di ogni rotta lo vieta). Un file di un altro campo o altre parti dopo la clip restano il 400.
8. **La sessione si ricontrolla quando la clip è arrivata** (`get_session_user` di `handle_dictation/3`): revocata → `/login`
   come le foto (`fuoriPerLeFoto`), la clip non parte.
9. **Panic del motore = `stt:provider non raggiungibile`** come il `rescue` di `ask/3`, con una riga di registro con la
   sola famiglia e il tipo del valore («assist: il motore della trascrizione è andato in panic» famiglia=stt tipo=…), come
   «assist provider for stt raised …» dello standalone (revisione, BASSA 7).
10. **Trascrittore finto nello stesso container del modello finto** (il brief lo consente «se è semplice»): stessa porta,
    rotte diverse, coda e richieste separate; della clip si tiene solo la lunghezza.

11. **Dopo la clip la scadenza di lettura si toglie** (revisione, ALTA 1): `allungaLettura` la tiene a ultimo pezzo + 10 s, e
    con il multipart letto dal gestore la fine del corpo non arriva alla lettura di fondo di net/http (che toglie la sua
    scadenza solo quando il corpo è finito): scaduta durante l'attesa del trascrittore, cancellava la richiesta dopo 10 s
    invece di 35. Ora, ricevuta la clip, `SetReadDeadline(time.Time{})`; l'attesa la tiene `assist` (35 s) e un browser che se
    ne va si vede dalla connessione chiusa.
12. **Al più 2 clip per istanza e 1 per account** (revisione, MEDIA 2), contate dall'inizio della ricezione alla fine della
    trascrizione; oltre, subito «⚠ Trascrizione non disponibile, scrivi a mano e premi Estrai.» (il rifiuto che c'è già),
    senza leggere la clip, senza audit.
13. **`ConSilenzio` alloca dalla dimensione vera** (revisione, MEDIA 3): frequenza al più 48 kHz e al più 8 canali (il
    silenzio è al più 384 KB), campioni al più `ClipMassima`; il risultato è al più `ClipMassima` più i due silenzi. Il
    controllo dei 4 GiB del RIFF non serve più ed è tolto.
14. **La clip si legge in un buffer che raddoppia** (`leggiClip`, revisione, BASSA 4) e ogni buffer lasciato indietro si
    azzera; restano solo i pochi KiB dei buffer di lettura di net/http e mime/multipart.
15. **Intestazioni che Go aggiunge da sé** verso il trascrittore (revisione, BASSA 8): `Accept-Encoding: gzip` (e la
    risposta compressa si decomprime da sola) e `User-Agent: Go-http-client/1.1`; lo standalone (hackney) non le manda.
    Nessun dato del negozio.

## Prove

- Test nuovi: `internal/assist/trascrizione_test.go` (richiesta di LocalWhisper con indirizzo sporco, tipo con parametri e
  maiuscole, chiave con spazi; glossario vuoto ma presente, niente Authorization senza chiave; risposte 200/201/302/500, senza
  `text`, `Text`, numero, null, non JSON, elenco, oltre 1 MiB, rete giù, lenta; niente redirect, server spento, indirizzo rotto,
  chiamante andato via; attese e limiti; formati ammessi e no; messaggi; ordine dei rifiuti senza audit; riuscita con testo e
  con silenzio e le righe di audit; motore che sbaglia, va in panic o dà un errore qualunque; attesa scaduta; database chiuso;
  riservatezza; `ConSilenzio` mono 16 kHz, stereo 44,1 kHz, con un pezzo LIST dispari, lunghezza ignota, mezzo campione, e i
  rifiuti). `internal/web/trascrizione_test.go` (non configurata; trascritta per titolare e dipendente, senza file temporanei e
  senza testo nel registro; i rifiuti con e senza audit; catena e forma del corpo: senza token, senza accesso, senza file, campo
  sbagliato, due clip, campo in coda, oltre 25 MiB e a 25 MiB esatti; sessione chiusa mentre la clip arriva; senza assistente e
  database guasto). `tools/banco/modellofinto` (`TestTrascrittoreFinto`). Aggiornati `ruoli_test.go`, `assistente_test.go`
  (`assistenteGuasto.Trascrivi`), `dettato_test.go` (l'indirizzo del finto nella faccia di prova).
- `scripts/check` verde (copertura senza banco: 353 voci, 0 rosse). Il primo giro aveva trovato il 400 senza HSTS di un
  modulo senza file (`TestIntestazioniDiSicurezzaSuOgniRotta`): da qui la Scelte 7.
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-109/mutazioni/muta.py`, su una copia del repo): 64 scritte, 2 non
  compilanti (S4, S5: variabili rimaste senza uso); delle **62 compilanti** 59 uccise al primo giro, 3 vive: 2 uccise rafforzando
  i test (A28, controllo del MiB della risposta tolto: una risposta JSON valida di 1 MiB e un byte; S10, controllo dei canali
  tolto: un WAV con zero canali e blocco zero, che senza il controllo divide per zero), 1 equivalente (W14: il tipo della
  clip vuota non conta, una clip vuota è sempre «troppo breve» prima del formato). 0 vive.
- **Revisione indipendente: 1 ALTA, 2 MEDIE, 5 BASSE, tutte chiuse.** ALTA 1: la scadenza dell'ultimo pezzo cancellava la
  trascrizione dopo 10 s (Scelte 11; `TestClipAttesaOltreIlPezzo` con pezzo 300 ms e attesa 1050 ms, in proporzione ai 10 e
  35 s, cade con il difetto vecchio). MEDIA 2: clip in parallelo senza limite (Scelte 12; `TestClipInParallelo`,
  `TestClipInCorso`). MEDIA 3: `ConSilenzio` allocava dall'intestazione (Scelte 13; `TestConSilenzioLimiti`). BASSA 4: copie
  intermedie di `io.ReadAll` (Scelte 14; `TestLeggiClip`). BASSA 5: test per le mutazioni vive di `ConSilenzio`
  (`TestConSilenzioForma`). BASSA 6: sessione non riletta (`TestClipSessioneNonRiletta`). BASSA 7: riga di registro del panic
  (Scelte 9; `TestTrascriviPanicNelRegistro` e `TestPrivacyTrascrizione`). BASSA 8: intestazioni aggiunte da Go (Scelte 15).
  Anche W12 (tempo del pezzo non allungato) ha il suo test, `TestClipCaricamentoLento`.
- **Altre rotte con lo stesso difetto (riportato, non corretto):** le **foto** (`POST /repairs/{id}/foto`) usano
  `allungaLettura` allo stesso modo: se dopo l'ultimo pezzo la normalizzazione nel processo figlio supera `tempoPezzo` (10 s),
  la richiesta può essere cancellata a metà; non provato. **`/nuova/estrai`** e la prova dell'assistente no: il corpo è un
  modulo letto per intero, e quando il corpo finisce net/http toglie la sua scadenza (la sonda del revisore con `ReadTimeout`
  di 500 ms e un modello che ne mette 1500 risponde bene).
- **Mutazioni del revisore:** 85 in tutto, 79 compilanti, 66 uccise, 4 equivalenti (A16, S10, W09, M04), 9 vive (S03, S04,
  S09, S11b, S13, S14, W11, W12, W15), poi tutte uccise o superate tranne W11: S03, S04, S11b, S13, S14 dai test di
  `TestConSilenzioForma`; S09 (controllo dei 4 GiB) è superata, il controllo è tolto perché con i limiti nuovi non si arriva;
  W12 da `TestClipCaricamentoLento`; W15 da `TestClipSessioneNonRiletta`. **W11** (la clip non azzerata) resta: è solo
  riservatezza, non si osserva da un test.
- **Secondo giro dell'implementatore sui pezzi corretti** (`muta2.py`): 22 scritte, tutte compilanti; 19 uccise al primo
  giro, 3 vive: R18 (il registro di `Impostazioni` ignorato) e S04 (il fmt di 14 byte, che leggeva i bit dal pezzo dopo)
  uccise con test nuovi; **R10** (i buffer lasciati indietro non azzerati) resta, come W11: solo riservatezza. **W11 e R10 contano come
  equivalenti di fatto** (ok del coordinatore, 20/11/2026). 0 vive osservabili.
- Implementatore Opus: token e minuti nel rapporto al coordinatore.

## Banco

**Nessuno scenario nuovo** (deviazione dal piano approvata dal coordinatore, vedi Dubbi 1): il confronto del caricamento
della clip si fa nella 110. Giro di controllo:

- **s109a (20/11/2026):** HTTP VERDE (90 scenari, 1370 passi, 0 inattese, 0 errori); browser 6 inattese e 1 errore, tutti
  in `sessioni-rientro` (vista telefono, standalone, fase «registro»): **nota 8**, con i tre segni (`GET /audit`, poi 500
  in 5017 ms, «database is locked», riga 6909 del log dello standalone). Dodicesima comparsa.
- **s109b (20/11/2026, unica ripetizione):** HTTP VERDE; browser 6 inattese e 1 errore: 4 in `storico-correzioni[0]`
  (vista PC, standalone, fase «fine») e 2 a catena in `anagrafica-artigiani[1]`: di nuovo **nota 8** (`GET /audit`, poi
  `GET /repairs/63` con 500 in 5020 ms, «Database busy», riga 4360). Tredicesima comparsa.
- Nessuna inattesa lato Go in tutti e due; la guardia sotto 10 GB non è mai scattata. **Stefano ha deciso di pubblicare
  la 109 con i due giri così documentati** (eccezione per questa slice, la regola della sola ripetizione non cambia).

Comandi del giro (per riferimento): I binari per il giro di controllo (nessuno scenario HTTP o browser
cambiato; il modello finto ha in più le rotte `/stt`) sono in `to-be-deleted-after-use/slice-109/banco-bin/`:

```
banco su -nome s109a -immagine gestrip-standalone:fab-225 -gestrip <banco-bin>/gestrip -dati sintetici-import
banco esegui -nome s109a -banco <banco-bin>/banco
GESTRIP_BANCO_ESITO=to-be-deleted-after-use/banco/grgo-s109a/esito/esito.json scripts/check
docker logs grgo-s109a-app > …   # prima di banco giu
banco giu -nome s109a
```

Atteso: uguale a s108a (HTTP 90 scenari, browser 32, 44 differenze attese, 0 inattese).

## Per il manuale

- Niente di visibile ancora: il pulsante «🎤 Detta a voce» arriva con la 110. Da scrivere allora: la clip va al servizio di
  trascrizione indicato in «Impostazioni › Assistente», insieme alle «Parole di questo negozio»; GestRip non conserva l'audio
  (né sul computer né sul server) e non scrive il testo trascritto nei registri; una registrazione troppo breve, un formato
  che il browser non sa mandare, il silenzio o il servizio spento hanno ognuno il suo messaggio sotto la casella.

## Dubbi

1. ~~Banco HTTP della riga 109~~ → **deciso dal coordinatore il 20/11/2026: strada (b)** (vedi Voci). Per memoria: (i) Nello standalone la clip arriva solo con l'upload LiveView
   (websocket): non c'è una richiesta HTTP da confrontare, quindi ogni passo sarebbe `solo_go`, che vuole una D approvata;
   (ii) il livello HTTP gira prima del browser, quando le app non hanno la trascrizione configurata (la configura
   `assistente-salva`, in fondo al browser): senza configurazione ogni clip dà «Nessuna trascrizione è configurata…», quindi
   «troppo corta», «formato sbagliato» e «riuscita» non si vedono; (iii) configurarla in un passo HTTP del Go non si può
   disfare (l'indirizzo è obbligatorio, come nello standalone) e `assistente-salva` troverebbe il Go «Configurato» e lo
   standalone no. **Strade possibili, da scegliere:** (a) uno scenario HTTP solo Go con una D nuova, che fa solo «non
   configurata» e i rifiuti della catena; (b) il confronto nella 110 con uno scenario browser in `IN_FONDO` dopo
   `assistente-salva` (che lascia le due app con la trascrizione sul finto, che ora risponde su `/stt`): nello standalone il
   file sintetico sull'input nascosto dell'upload, nel Go la pagina della 110; i quattro casi confrontati sul messaggio e sul
   testo aggiunto (come la strada (c) della 108); (c) lo stesso scenario browser già nella 109, con il Go raggiunto da una
   `fetch` a `/nuova/trascrivi` dalla pagina `/nuova` invece che dal pulsante; (d) la trascrizione configurata già da `banco
   su` nei dati sintetici delle due app (cambiano le attese di `assistente` e il punto di partenza di `assistente-salva`).
   Scelta la (b).
2. **Risposta JSON e 413:** la forma della risposta (Scelte 5) e il 413 oltre 25 MiB (Scelte 6) sono nuovi; la 110 li usa e
   può chiedere di cambiarli.
3. **Glossario oltre 2000 caratteri:** il salvataggio lo rifiuta, ma l'import copia le impostazioni dell'assistente senza la
   validazione (nota 8 delle domande aperte). Un glossario importato più lungo andrebbe intero nell'intestazione, come nello
   standalone. Un glossario con caratteri di controllo (non spazi) fa fallire la richiesta in Go (`stt:transport::error`),
   mentre hackney lo manderebbe: caso solo teorico.
4. **Sessione revocata con `fetch`:** il redirect a `/login` (come le foto) per una `fetch` della 110 arriva come pagina di
   login seguita; la 110 deve riconoscerlo (`response.redirected`) e andare a `/login`.
