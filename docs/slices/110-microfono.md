# 110 — Microfono nella nuova busta (T4.10, decima)

- **Fonte:** standalone a `go-baseline`: `assets/js/dictation.js` (il hook `Dictation`: `MIN_MS`, contesto non sicuro,
  `navigator.mediaDevices`, il doppio tocco, `getUserMedia({audio: true})`, «🔴 Sto ascoltando…», «⚠ Troppo breve, riprova.»,
  «⏳ Trascrivo…», la clip nel campo dell'upload con un DataTransfer), `lib/gestrip_standalone_web/live/workshop_live.ex`
  (`#assist-mic` con `aria-label`, il campo `live_file_input` di `:dictation` in `#assist-form`, `assist_stt?`,
  `handle_event("assist_said")` con i 200 grafemi, `handle_dictation/3`, `append_dictated/2`), `assist/providers/local_whisper.ex`
  (il tipo normalizzato verso il trascrittore). Piano T4.10 riga 110 e domanda G; D-25 nell'inventario (riga 170 e
  RIP-H-44). La parte server è quella della 109, senza cambiamenti.
- **Voci:** **RIP-H-44 chiusa** (segno su `TestMicrofonoPulsante` e `TestMicrofonoNelloScript`; scenari browser
  `dettato-clip` e `dettato-microfono`), con la **D-25** (WAV mono a 16 kHz invece di webm/ogg/mp4). RIP-H-45, RIP-H-46 e
  RIP-H-47 restano come le ha lasciate la 109 (parziali, senza segno). Nessuna D nuova. La risposta di `POST /nuova/trascrivi`
  **non cambia**.

## Cosa c'è

- **`internal/web/statici/app.js`:** il blocco «🎤 Detta a voce» (registrazione con un AudioWorklet, ricampionamento a 16 kHz,
  WAV, invio a `/nuova/trascrivi`, testo in fondo alla casella); vedi SVILUPPO, «Microfono nella nuova busta (slice 110…)».
  **`app.css`:** stile del pulsante (il `btn-ghost` dello standalone).
- **`internal/web/pagine/nuova.html`:** `#assist-mic` prima di «✦ Estrai con AI» e `#assist-clip` (campo file nascosto, senza
  nome) in `#assist-form`, solo con `Dati.Microfono`.
- **`internal/web/dettato.go`:** `microfonoConfigurato` (letto solo con il riquadro); **`nuova.go`:** `datiNuova.Microfono`,
  letto in `moduloNuova`; **`assistente.go`:** l'interfaccia `Assistente` ha `TrascrizioneConfigurata`.
- **`internal/assist/dettato.go`:** `Impostazioni.TrascrizioneConfigurata` (`LeggiTrascrizione(...).Usabile()`).
- **Banco:** `tools/banco/microfono.go` (il WAV del microfono finto: 3 s di un tono a 440 Hz, mono, 16 kHz), scritto da
  `scriviFotocamere` nella cartella delle fotocamere finte; `tools/banco/modellofinto/wav.go` (`DescriviWAV`: formato e tono di
  una clip WAV, `wav` e `tono_hz` nelle richieste a `/stt`, mai i byte); in `banco.mjs` `dettatoClip`, `dettatoMicrofono`,
  `IN_FONDO` e `ARGOMENTI_IN_FONDO` (le opzioni di Chromium per il microfono finto); `bench/browser/dettato-clip.json`,
  `bench/browser/dettato-microfono.json`; una riga D-25 in `bench/deviazioni.json`; l'elenco in `tools/banco/browser_test.go`.
- **SVILUPPO:** sezione nuova.

## Scelte

1. **La clip passa da un campo file, come nello standalone.** Il registratore mette il WAV in `#assist-clip` con un
   DataTransfer e un `change`; il gestore del campo lo manda a `/nuova/trascrivi`. Così lo scenario `dettato-clip` mette lo
   stesso file sintetico sul campo file di `#assist-form` nelle due app (nello standalone il campo dell'upload LiveView), e il
   caricamento di un file e la registrazione sono la stessa strada. Il campo non ha nome: `/nuova/estrai` non lo riceve.
2. **AudioWorklet caricato da un indirizzo `blob:`** (il codice del processore sta in app.js): niente asset nuovo, niente
   segnaposto come `__JSQR__`, niente cambiamento al precache; la CSP del Go non ha `script-src`.
3. **Ricampionamento mentre si registra** (filtro a scatola, la media dei campioni di ogni intervallo; con un ingresso più lento
   il campione si ripete), in PCM a 16 bit: in memoria restano 32 KB al secondo invece dei campioni a 32 bit a 44,1 o 48 kHz.
   Il nodo mescola i canali (`channelCount: 1`, `explicit`). Il contesto audio si crea con la frequenza del browser (non con
   `sampleRate: 16000`, che non tutti i browser accettano con un flusso del microfono).
4. **`getUserMedia({audio: true})` come lo standalone**, con i filtri del browser (eco, rumore, guadagno) accesi: un
   cambiamento qui cambierebbe la voce che arriva al trascrittore, e va deciso con la T5.1 se serve.
5. **Senza AudioWorklet** (o se non parte) il microfono si rilascia e il messaggio è quello che c'è già, «⚠ Microfono non
   disponibile su questo dispositivo o browser.»: nessun testo nuovo.
6. **A 25 MiB la registrazione si ferma da sola** (circa 13 minuti): la clip non supera mai `assist.ClipMassima`. Lo standalone
   non si ferma, e la sua clip oltre il limite si perde senza dire niente.
7. **Una risposta che non è il JSON della 109** (413, pagina d'errore, rete giù, CSRF scaduto) è «⚠ Trascrizione non
   disponibile, scrivi a mano e premi Estrai.», un messaggio che c'è già. Lo standalone, oltre 25 MiB, non dice niente e resta
   «⏳ Trascrivo…».
8. **La sessione revocata porta a `/login`** (`window.location.assign("/login")`, come `redirect(socket, to: ~p"/login")`): la
   fetch segue il redirect e il gestore guarda l'indirizzo finale; non ricarica la pagina, che dopo un'estrazione è un POST.
9. **«⏳ Trascrivo…» lo dice il gestore del campo**, non il registratore: vale anche per un file scelto. Nello standalone un file
   messo a mano nel campo non lo mostra (lo dice il hook); il messaggio finale è lo stesso.
10. **Il pulsante e il campo solo dentro il riquadro**, come lo standalone: la trascrizione si legge solo se l'estrazione è
    configurata, e un guasto della lettura è la pagina d'errore.
11. **Il contesto audio si chiama `ContestoAudio`:** `TestScriptDelSuonoQR` conta una sola `new Contesto()` in app.js (il suono
    del lettore QR); anche quella del microfono si crea solo in un tocco.
12. **Revisione indipendente (Opus): 0 ALTE, 3 MEDIE, 6 BASSE.** Corrette M1, M2, M3, B1, B3 (sotto); B2, B4, B5, B6 in
    lista (Dubbi 5).
    - **M1:** in `dettatoClip` la casella scritta a mano perde il fuoco prima del file (`blur()`): LiveView non riscrive una
      casella che ha il fuoco, e il caso «riuscita» sarebbe caduto nello standalone.
    - **M2:** `gs:pagina` (pagina cambiata senza ricaricare: giro delle foto, «✦ Estrai con AI», un link) butta la
      registrazione del pulsante che non c'è più (`scarta`: microfono spento, contesto chiuso, campioni lasciati, niente
      invio) e libera l'avvio di quel pulsante; anche un avvio finito dopo il cambio di pagina si butta. Come
      `destroyed()` → `release()` del hook.
    - **M3:** in `dettato-microfono` uno script messo prima di quelli della pagina tiene i flussi di `getUserMedia` e i
      contesti audio, e dopo ogni caso si guarda che tracce e contesti siano spenti (`*.rilascio`); in più una seconda
      ripresa sulla stessa pagina (`due.*`, il testo dopo il primo) e un tocco dopo un microfono negato sulla stessa pagina
      (`riprova.*`). Nessun testo nuovo.
    - **B1:** una traccia che finisce da sé (microfono staccato, permesso tolto) ferma la registrazione e manda la clip.
    - **B3:** mentre una clip è al server (`trascrivendo`) il pulsante e il campo della clip non fanno niente.

## Prove

- **Il Chromium del banco accetta il microfono finto da file:** con l'immagine `grgo-banco-browser:1.58.2` (già sul disco,
  `--pull never`, rete `none`, seccomp dello standalone) e `--use-fake-device-for-media-stream --use-fake-ui-for-media-stream
  --use-file-for-fake-audio-capture=<wav>`, una pagina su `127.0.0.1` riceve il tono del file (44,1 kHz nel contesto, circa
  440 Hz). Prova usa e getta in `to-be-deleted-after-use/slice-110/verifica-microfono/prova-flag.mjs`.
- **app.js nel Chromium del banco** (`verifica-microfono/prova-pagina.mjs`: la pagina con il riquadro, l'app.js vero, un
  `/nuova/trascrivi` finto che legge il WAV): registrazione di 2 s → «✓ Trascritto…», la casella «  scritto a mano  » diventa
  «scritto a mano <testo>», la clip arriva come `_csrf_token` e poi `dictation` (`filename="dettato"`, `audio/wav`), **RIFF PCM
  1 canale 16000 Hz, 32000 byte/s, blocco 2, 16 bit, data coerente, 2,04 s, tono di circa 430 Hz** (il file è a 440; in altri giri 422-424 Hz); tocco
  breve → «⚠ Troppo breve, riprova.» in rosso; microfono negato → «⚠ Microfono negato: Permission denied — scrivi a mano.»;
  contesto non sicuro → il messaggio dello standalone; 413 → «⚠ Trascrizione non disponibile…»; redirect a `/login` → `/login`;
  due tocchi mentre si chiede il permesso → un solo `getUserMedia`; nessun errore JavaScript.
- Test nuovi: `internal/web/microfono_test.go` (pulsante e campo con la trascrizione, per titolare e dipendente, anche dopo
  un'estrazione rifiutata o riuscita; niente con la sola estrazione o la sola trascrizione; lettura guasta → 500; le righe di
  app.js: testi, 600 ms, 16 kHz, 25 MiB, AudioWorklet a un canale, intestazione WAV campo per campo, token prima della clip,
  `/login`, `append_dictated/2`, niente MediaRecorder, niente storage); `internal/assist/dettato_test.go`
  (`TestTrascrizioneConfigurata`); `tools/banco/modellofinto/wav_test.go` (mono 16 kHz, stereo 44,1 kHz con un LIST dispari,
  silenzio, data più lungo del file, non WAV, senza fmt, virgola mobile, zero canali, fruscio sopra il tono; il finto descrive
  la clip WAV e non il webm); `tools/banco/fotocamera_test.go` (`TestMicrofonoFinto`). Aggiornati `assistente_test.go`
  (`assistenteGuasto.TrascrizioneConfigurata`), `dettato_test.go` (`avvolgiAssistente`), `tools/banco/browser_test.go`.
- `scripts/check` verde (copertura senza banco: 353 voci, 0 rosse). Il primo giro dei test aveva trovato
  `TestScriptDelSuonoQR` (una sola `new Contesto()` in app.js): da qui la Scelte 11.
- **Mutazioni a mano** (`to-be-deleted-after-use/slice-110/mutazioni/muta.py`, su una copia del repo): 36 scritte (8 sulla
  pagina e su `assist`, 14 sulle righe di app.js, 14 sul trascrittore finto e sul microfono finto), 1 non compilante al primo
  giro (G01, riscritta); delle 36 compilanti 32 uccise al primo giro, 4 vive: 3 uccise rafforzando `TestDescriviWAV` (B02,
  isteresi tolta: fruscio di ±1500 sopra il tono; B04, tono anche per un formato che non è PCM: formato 3 a 16 bit; B07, fmt
  senza data), 1 equivalente (B10, la clip del finto non azzerata: solo riservatezza). 0 vive osservabili.
- **Mutazioni del revisore:** 42 scritte, 24 uccise, 7 equivalenti, 11 vive. Dopo le correzioni, rifatte J02, J03, J04,
  J05, J15, J16 (con la riga corrispondente del codice nuovo) nel Chromium del banco con
  `verifica-microfono/prova-revisione.mjs`, che fa i passi nuovi di `dettato-microfono` sull'app.js del Go: **uccise J03**
  (tracce accese dopo ogni caso), **J04** (contesti aperti), **J05** (dopo «negato» il tocco non riparte), **J16** (la
  seconda ripresa non parte) e **J02** (microfono acceso dopo un AudioWorklet che non parte: la vede solo la prova, il banco
  non ha un caso confrontabile perché lo standalone non usa AudioWorklet); **viva J15** (il ripiego di 1 s quando il
  processore non risponde: equivalente di fatto, il processore risponde sempre). Le stesse prove uccidono le mutazioni delle
  correzioni: M2 tolta (microfono e contesto accesi dopo `gs:pagina`, il pulsante nuovo non parte), B1 tolta (traccia finita
  e registrazione che continua), B3 tolta (due clip al server). Nei test Go le righe nuove sono in `TestMicrofonoNelloScript`.
- **Mutazioni di app.js nel Chromium del banco** (`prova-pagina.mjs`, a mano): campioni scritti big-endian → tono di 3155 Hz;
  ricampionamento con il passo sbagliato → 0,69 s e 1276 Hz: le vedrebbe la riga D-25 di `dettato-microfono`. L'ultimo pezzo
  del processore ignorato → 2,07 s e 424 Hz: viva, quasi equivalente (si perde al più un pezzo di 4096 campioni, poi il
  ripiego di 1 s chiude la clip).

## Banco

**Giro s110a (21/11/2026): VERDE al primo giro.** HTTP 90 scenari, 1370 passi, 0 inattese, 0 errori; browser 34
scenari, 68 viste, 45 differenze attese, 0 inattese, 0 errori; una ATTESA nuova come previsto (D-25
`dettato-microfono[0] detta.clip`: `audio/webm` nello standalone, `audio/wav, PCM 16000 Hz, 1 canale, 16 bit, tono di 440
Hz circa` nel Go). La guardia sotto 10 GB non è mai scattata. Copertura con l'esito: 353 voci, chiuse 219, rosse 0,
parziali 16, aperte 107, fuori v1 11.

## Per il manuale

- Con la trascrizione configurata («Impostazioni › Assistente», «Trascrizione del parlato») il riquadro del dettato ha
  «🎤 Detta a voce» accanto a «✦ Estrai con AI». Un tocco comincia la registrazione («🔴 Sto ascoltando… (tocca per
  fermare)»), un secondo tocco la ferma e la manda al servizio di trascrizione («⏳ Trascrivo…»); il testo si aggiunge in
  fondo alla casella, così si può dettare in più riprese, e poi si preme «✦ Estrai con AI».
- La prima volta il browser chiede il permesso del microfono. Serve l'indirizzo sicuro (https): altrimenti «⚠ Microfono non
  disponibile: contesto non sicuro…». Permesso negato: «⚠ Microfono negato: … — scrivi a mano.».
- Un tocco troppo breve (meno di 0,6 secondi) non manda niente: «⚠ Troppo breve, riprova.». Gli altri messaggi
  (registrazione troppo breve, non ho sentito nulla, trascrizione non disponibile) sono quelli della 109.
- GestRip non conserva l'audio: né nel browser né sul server; la clip va al servizio indicato nelle impostazioni, insieme
  alle «Parole di questo negozio». Una registrazione lunga si ferma da sola dopo circa 13 minuti.

## Dubbi

1. **I due scenari non sono mai stati provati contro le due app vere** (non ho lanciato il banco): app.js è provato nel
   Chromium del banco con una pagina finta, ma selettori e tempi dello standalone (LiveView che riceve un file messo con
   `setInputFiles` sul campo dell'upload, il hook con il microfono finto) li vedrà il primo giro. Il tono stimato del Go ha
   40 Hz di tolleranza (nella prova 430 Hz per 440): se i filtri del browser lo spostassero di più, la riga D-25 va ritoccata.
2. **Estrazione tolta dopo l'apertura della pagina:** il rifiuto ridisegna il riquadro com'era (108, Scelte 4) ma il microfono
   no (la trascrizione si legge solo con l'estrazione configurata); caso di bordo, da decidere se serve.
3. **Contesto audio e filtri del browser** (Scelte 3 e 4): per il trascrittore locale della T5.1 andrà misurato se il
   ricampionamento a scatola e i filtri di `getUserMedia` peggiorano la trascrizione rispetto al webm di oggi.
4. **La risposta di `POST /nuova/trascrivi` resta quella della 109.** Il 413 non ha un messaggio suo (Scelte 7): con il fermo a
   25 MiB del registratore lo vede solo un file scelto a mano più grande.
5. **Rilievi della revisione lasciati in lista** (decisione del coordinatore): **B2**, nessuna scadenza su `addModule` e
   `resume` (un browser che non li fa finire lascia il pulsante fermo); **B4**, il controllo dei nomi di `IN_FONDO` (era già
   così prima della 110); **B5** e **B6**.
