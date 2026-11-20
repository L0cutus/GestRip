# 107 — Pagina /settings/assistente, parte configurazione (T4.10, settima)

- **Fonte:** standalone a `go-baseline`: `lib/gestrip_standalone_web/live/assist_live.ex` (`save_ai`, `save_stt`, `test`,
  `save/3`, `load/1`, i due riquadri di `render/1`; la raccolta dei dettati è della 114), `lib/gestrip_standalone/assist/config.ex`
  (`get/1`, `usable?/1`, `put/3`, `keep_secret/2`, `changeset/3` con `validate_endpoint/1`, `validate_key_travel/1`,
  `normalize_glossary/2`, `validate_lengths/2`, `blank_secret/1`, `write/4`, `changed_keys/3`), `assist/glossary.ex`,
  `assist.ex` (`@self_test`), test `config_test.exs`, `glossary_test.exs`. `URI.parse/1` di Elixir 1.18.4 misurato
  nell'immagine `elixir:1.18.4-otp-27-slim` (senza rete) su 17023 indirizzi a caso: 0 differenze dal Go.
- **Voci:** ADM-11 parziale (manca l'accensione e lo spegnimento della trascrizione locale, T5.1: decisione del coordinatore
  del 20/11/2026, MEDIA 1 della revisione), senza segni `// inventario:` né voci negli scenari; RIP-H-46 resta parziale (parte
  trascrizione con la 109), senza segno.
  Nessuna D nuova.

## Cosa c'è

- **`internal/assist/config.go`** (nuovo): le chiavi dello standalone (`assist.ai.endpoint|model`, `assist.stt.endpoint|glossary`
  in `impostazioni`; `assist.ai.api_key`, `assist.stt.api_key` in `segreti_provider`, come la 069), `LeggiEstrazione` (il
  punto da cui il client della 106 prende la configurazione salvata: la prova qui, la busta nuova nella 108) e
  `LeggiTrascrizione` (per la 109), `ConfigTrascrizione`, il glossario (`GlossarioSalvato`, `GlossarioPerLaRete`,
  `EccessoGlossario`, 2000 sulla forma a una riga), l'indirizzo letto come `URI.parse/1` (`partiIndirizzo`) e
  `inChiaroAltrove` (`remote_plaintext?/1`). `Configurazione` e `ConfigTrascrizione` non stampano mai chiave e indirizzo
  (`Format` per ogni verbo di fmt, `LogValue` per slog: «***»).
- **`internal/assist/impostazioni.go`** (nuovo): `Impostazioni` (`Pagina`, `Salva`, `Prova`), `NuoveImpostazioni`. `Salva` è
  `Config.put/3` per le due famiglie; `Prova` è il «test»: `FraseDiProva` (quella dello standalone, senza clienti) con
  `Collegamento.Estrai` della 106 sulla configurazione salvata, negozio vuoto (la frase non ha clienti e la pagina mostra solo
  la descrizione).
- **`internal/web/assistente.go`, `pagine/assistente.html`, `statici/app.css`** (nuovi): `GET /settings/assistente`,
  `POST /settings/assistente/estrazione` (`save_ai`), `…/trascrizione` (`save_stt`), `…/prova` (`test`), tutte
  `soloTitolare` e con il guscio (CSRF come le altre). Salvato: 302 alla pagina con «Impostazione salvata.»; rifiutato: la
  pagina (200) con gli errori sotto i campi, i valori mandati e l'altro riquadro com'è salvato; prova: la pagina (200) con
  l'esito in `#ai-probe` (classe `text-error` sugli errori), come lo standalone lo mostra sul posto. La scheda «Assistente»
  delle impostazioni ora compare (la rotta c'è); `page_title` «Assistente e dettatura».
- **`cmd/gestrip/main.go`:** `Dipendenze.Assistente = assist.NuoveImpostazioni(d, clock.Sistema{})`.
- **Banco:** `tools/banco` avvia il modello finto della 106 come container dell'istanza (vedi Banco); HTTP
  `bench/scenari/assistente.json` e `assistente-dipendente.json`; browser `bench/browser/assistente-salva.json` con la funzione
  in `tools/banco/browser/banco.mjs`.
- **SVILUPPO:** sezione «Assistente: configurazione (slice 107, ADM-11)»; `docs/domande-aperte.md` note 7 e 8.

## Scelte

1. **Regole copiate dal changeset, con le stranezze:** indirizzo e modello (estrazione) e indirizzo (trascrizione) obbligatori
   sui valori finali («non può essere vuoto»); schema `http`/`https` e host non vuoto come `URI.parse/1` (anche `HTTP://` vale,
   `http://:80` no); la chiave non va in chiaro verso un host che non sia `127.0.0.1`, `localhost`, `::1` o
   `0:0:0:0:0:0:0:1` (confronto esatto: `http://LOCALHOST` è «altrove»), anche la chiave tenuta dopo un cambio dell'indirizzo;
   lunghezze 255/120/255/4000 solo sui campi cambiati; glossario cambiato portato a una voce per riga, rifiutato se la forma a
   una riga supera 2000 («N carattere/caratteri di troppo sul limite di 2000»); `clear_secret` fuori da
   `true/1/false/0/vuoto` è «Valore non valido.» (il cast di `:boolean`), anche se `on` ha già tolto la chiave; un glossario di
   sole virgole è il testo vuoto e conta come cambiato (`put_change/3`). Audit `assist.provider_configured`
   (`assist_provider`, `ai`/`stt`, `ok`) con `family`, `changed` (i nomi nell'ordine di `keys/1`) e `configured`; mai un valore.
2. **Le chiavi in `segreti_provider`** (D-03, come la 069): `nil` cancella la riga. La chiave non torna mai: né nella pagina
   (sempre `value=""`, segnaposto «salvata — lascia vuoto per non cambiarla» se c'è), né nel rifiuto, né nel registro.
3. **Glossario a «\n» prima del dominio** (`aCapoUnix`, regola 069c delle caselle interne): la forma salvata è la stessa dello
   standalone (lì `\r` cade nello `String.split/1`), cambia solo il testo rimostrato dopo un rifiuto.
4. **Prova senza clienti:** il negozio passato a `Estrai` è vuoto; lo standalone legge clienti e oreficerie, ma l'esito mostrato
   («Il modello ha risposto: «…».», i primi 80 grafemi della descrizione) non ne dipende. Gli errori: «Il modello non è
   configurato: non è stato chiesto niente.», «Il modello ha rifiutato: ai:http:500.» (il motivo fisso della 106),
   «Il modello ha risposto qualcosa che non è un oggetto JSON.»; un guasto del database è la pagina d'errore. Con un 500 o una
   risposta illeggibile il Go chiede due volte (D-19), lo standalone una: l'esito mostrato è lo stesso.
5. **Il banco HTTP non salva:** nello standalone salvataggio e prova sono eventi LiveView (come per la 102), quindi gli scenari
   HTTP leggono la pagina (`assistente`) e il rifiuto del dipendente (`assistente-dipendente`); salvataggio, `http` con chiave
   verso un altro host e prova sono nel browser (`assistente-salva`), nelle due app. Nessuna rotta solo Go: niente `solo_go`,
   niente deviazioni.
6. **Il browser non può rimettere tutto com'era:** lo standalone rifiuta un indirizzo o un modello vuoti, quindi un'estrazione o
   una trascrizione salvata non si può più togliere dalla pagina. Lo scenario gira **per ultimo** (dopo tutti gli altri e dopo
   `lettore-qr`, `IN_FONDO` in `banco.mjs`) e alla fine (`finally`) rimette quello che si può (chiavi cancellate, glossario
   vuoto, i due indirizzi sul modello finto). **Scelta approvata dal coordinatore il 20/11/2026:** le due app restano
   configurate sul modello finto, chiavi e glossario vengono tolti. Conseguenza (MEDIA 3 della revisione): un secondo
   `banco esegui` sulla stessa istanza non è confrontabile, l'istanza va rifatta (`banco giu` + `banco su`) prima di ripetere.
7. **Container del modello finto nel banco** (approvato dal coordinatore il 20/11/2026). Letto nel codice: `banco giu` fa
   `docker rm -f` di ogni suffisso di `suffissiContainer()`, che comprende `-modello`, e ignora «No such container»; quindi lo
   toglie anche dopo un giro fermato a metà o un `su` caduto dopo averlo creato (il test `TestComandoModelloFinto` controlla il
   suffisso). La guardia della memoria sotto 10 GB guarda la memoria disponibile dell'host, quindi conta anche questo container
   (al più 256 MB, `--memory 256m`).

## Prove

- Test nuovi: `internal/assist/config_test.go` (21: niente di partenza, salva e leggi, 21 rifiuti, glossario al limite e forme,
  `partiIndirizzo` con i casi misurati in Elixir, chiave in chiaro solo verso questa macchina, casella vuota e cancella, valori
  del rifiuto, solo i campi cambiati, glossario importato non cambiato, audit, famiglia sconosciuta, spazi in lettura, niente stampa di chiave e indirizzo, prova
  con il modello finto, database chiuso); `internal/web/assistente_test.go` (7: pagina, solo titolare, salva, glossario con
  `\r\n`, rifiuto, prova con il modello finto, dipendenza assente o guasta); `cmd/gestrip/assistente_test.go` (serve dà la
  pagina sul suo database); `tools/banco` (`TestComandoModelloFinto`, catena con il modello finto, `MODELLO_URL`). Aggiornati
  `ruoli_test.go` (quattro rotte ADM-11), `guscio_test.go` (scheda e rotte con il guscio), `a_capo_test.go` (`stt[glossary]`).
- `scripts/check` verde (il primo giro aveva fermato `TestChiaviSoloGoComplete`: `assist/impostazioni.go` aggiunto ai file
  conosciuti che scrivono `impostazioni`, nessuna chiave solo Go).
- **Mutazioni a mano dell'implementatore:** 59 (57 compilanti), 47 uccise al primo giro, 4 equivalenti (`on` fra i veri di
  `keep_secret`: lo stesso valore è poi rifiutato dal cast; `presente` della chiave al posto di `!= nil`: una chiave finale non è
  mai di soli spazi; il glossario normalizzato uguale segnato come cambiato: solo una lunghezza già entro i limiti; `usabile`
  senza il modello nell'audit: un salvataggio valido ha sempre il modello), 6 vive poi uccise rafforzando i test (`Usabile`
  della trascrizione con spazi, cast di un indirizzo di soli spazi, `clear_secret` «0» e vuoto, glossario importato non
  cambiato, «Cancella la chiave salvata» dalla pagina, prova con il database guasto e la pagina buona).
- **Revisione indipendente (Opus): 0 ALTE, 3 MEDIE, 3 BASSE, tutte chiuse.** MEDIA 1: ADM-11 resta parziale (T5.1), tolti i
  segni e le voci degli scenari. MEDIA 2: mutazione 27 viva sulla chiave in chiaro: aggiunti fra gli indirizzi rifiutati
  « http://modelli.example.invalid» e «\thttp://127.0.0.2». MEDIA 3: un secondo `esegui` sulla stessa istanza cade: scritto
  qui (Banco) e in SVILUPPO che l'istanza va rifatta. BASSA 4: corretta in SVILUPPO la riga su `clear_secret` (`on` è «Valore
  non valido.»). BASSA 5: la seconda serratura va in lista (`docs/domande-aperte.md`, nota 8). BASSA 6: test nuovi per le
  vive del revisore (ordine di `changed` della trascrizione, valori esattamente al limite 255/120/255, `Pagina()` con
  l'indirizzo e senza modello non configurata, rifiuto della trascrizione con l'estrazione salvata, `_csrf_token` nei moduli
  `#ai-form`, `#ai-test-form`, `#stt-form`, `FraseDiProva` letterale, `http://localhost#x`, `ollama:11434` e
  `//modelli.example.invalid` con la chiave: solo l'errore dello schema).
- **Mutazioni del revisore:** 79 totali, 74 compilanti, 61 uccise, 3 equivalenti (40, 46, 57), 10 vive poi uccise dai test
  della BASSA 6 e della MEDIA 2 (dato del revisore; dopo le correzioni l'implementatore ha rifatto i suoi giri, sotto).
- **Secondo giro dell'implementatore dopo la revisione:** le 59 di prima più 8 sui pezzi dei rilievi (limite `>=`, ordine dei
  campi della trascrizione, `Configurata` senza modello, frase di prova, `#` nell'autorità, «in chiaro» senza schema http,
  `_csrf_token` della prova nel modello HTML, riquadro del rifiuto della trascrizione): 67, 65 compilanti, 61 uccise,
  4 equivalenti (le stesse quattro di sopra), 0 vive.
- Implementatore Opus: ~330k token, ~95 minuti con le correzioni.

## Banco

**Giro s107a (20/11/2026): HTTP verde, browser rosso.** HTTP 90 scenari, 1370 passi, 0 inattese, 0 errori. Browser 52
inattese e 1 errore:
- 48 dalla nota 8 di `docs/domande-aperte.md` (500 «Database busy» dello standalone su `GET /repairs/62` dopo `GET /audit`,
  vista PC di `sollecito-artigiano`, fase «scheda», con le inattese a catena in `sollecito-artigiano`, `anagrafica-artigiani`
  e `sessioni-consegna`);
- 2 dal selettore dello scenario: `section:has(#stt-form) h2` prendeva anche la sezione esterna `#settings-panel` e
  `pagina.stt.titolo` leggeva «Estrazione dal dettato» nelle due app; lo stesso difetto, nascosto, in `stato` (il badge
  della trascrizione). **Corretto:** ora si prende la sezione più interna (`section:has(#…-form):not(:has(section))`);
- 2 da `rifiuto-2.errori.api-key`, **differenza vera, decisa con la D-107 (Stefano, 20/11/2026):** lo standalone rifiuta la chiave in
  chiaro verso un altro host senza mostrare il messaggio (`config.ex:281-287`: `blank_secret/1` toglie `api_key` dai
  params, quindi `used_input?` è falso e `core_components` non mostra l'errore), il Go mostra «una chiave non viaggia in
  chiaro verso un host remoto». Si tiene il Go: il campo è fra i `solo_go` di `bench/browser/assistente-salva.json`
  (l'attesa resta sul Go, lo standalone esce come NOTA), scelta del coordinatore; `bench/deviazioni.json` non serve. Il
  silenzio dello standalone è la nota 10 di «Note per lo standalone» in `docs/domande-aperte.md`.

**Giro s107b (20/11/2026, dopo la correzione del selettore e il `solo_go`): HTTP verde, browser rosso.** HTTP 90 scenari,
1370 passi, 0 inattese, 0 errori. Browser 82 inattese e 1 errore; la nota 8 non è comparsa (`sollecito-artigiano` pulito,
nessun «busy» né «locked» nel log dello standalone):
- **1 errore + 76 inattese da `assistente-salva`, un solo difetto dello scenario.** «assistente-salva desktop standalone (fase
  rifiuto-2): errori del modulo ai non arrivati». `salva(…, 'errore')` aspetta errori del modulo **non vuoti** e diversi da
  quelli di prima; nel rifiuto-2 lo standalone non ne mostra nessuno (nota 10: endpoint e modello buoni, l'errore della
  chiave scartato da `used_input?`), quindi gli errori del rifiuto-1 spariscono e basta e dopo 30 s lo scenario si ferma.
  Le 76 inattese sono i 38 campi dopo quel punto (resto del rifiuto-2, rifiuti 3 e 4, prova-vuota, salva-ai, prove,
  salva-stt, chiave, cancella, ripristino, registro), ognuno contato due volte, tutti `standalone "(assente)"` con il Go
  uguale all'atteso. Il codice di `salva` era già lo stesso in s107a (confronto con la copia della revisione: fra i due giri
  cambia solo il selettore del riquadro, usato da `stato` e dal titolo, mai da `salva`; il `solo_go` lo legge solo il Go nel
  confronto) e la risposta dello standalone è deterministica: il passaggio di s107a non si ricostruisce dai log (lo
  standalone non registra gli eventi LiveView), va letto come una corsa di tempi favorevole, non come un comportamento su
  cui contare. **Corretto** in `tools/banco/browser/banco.mjs`: il rifiuto-2 usa `salva('ai', 'rifiuto-muto')`; per lo
  standalone basta che gli errori cambino, anche in nessuno, e che il modulo perda `phx-submit-loading` (LiveView ha chiuso
  il submit); senza errori di prima sulla pagina lo scenario si ferma. Il Go resta come `errore`: il messaggio deve
  arrivare, altrimenti è un ERRORE del giro. Nessun test Go esegue la logica dello scenario (il browser gira solo nel banco).
- **6 inattese da `lotti-invio` (4) e `lotti-reinvio` (2): instabilità del banco, non la 107 e non la nota 8.** Il gettone
  del lotto del Go è uscito `…UGR-`, con il trattino in fondo: la riduzione a «/lotto/<gettone>» (`/\/lotto\/[A-Za-z0-9_-]{32}\b/`,
  in due punti di `banco.mjs`) vuole un confine di parola dopo il 32° carattere, e fra «-» e la fine o il punto non c'è,
  quindi il gettone del Go resta in chiaro e lo standalone (gettone finito con una lettera) no. Capita con un gettone su 64;
  lo stesso lotto è letto in tutti e due gli scenari. I tre 500 del log dello standalone sono i `label.pdf`, `receipt.pdf`,
  `scheda.pdf` con un numero di busta enorme dello HTTP (meno di 1 ms), non questi. Correzione possibile, non fatta: al posto
  di `\b`, `(?![A-Za-z0-9_-])`.

Cosa cambia in `tools/banco`:

- con `-dati sintetici-import`, `banco su` avvia anche `grgo-<nome>-modello`: `banco modello-finto -ascolta :8082` dal binario del
  banco (come il bot finto della 074b, in sola lettura, `--read-only`, utente 1000, niente porte sull'host), alias
  `modello.invalid` sulla rete interna; `runtime.json` porta `"modello": "http://modello.invalid:8082"`; `banco giu` toglie
  `-modello`;
- il container del browser, con i dati, ha `MODELLO_URL=http://modello.invalid:8082`: lo scenario programma il modello finto
  (`POST /_prova/risposte`) e legge le richieste (`GET /_prova/richieste`) da Node, fuori dalla pagina;
- le due app raggiungono il modello finto solo quando lo scenario salva lì la loro estrazione: prima nessuna delle due è
  configurata, quindi `nuova` e gli altri scenari non cambiano;
- dopo `esegui` le due app restano configurate sul modello finto: **per ripetere il giro l'istanza va rifatta** (`banco giu` +
  `banco su`); un secondo `esegui` sulla stessa istanza lo rifiuta già il segno `browser_girato`.

Comando (dal capo-slice, dopo il «memoria libera»):

```
CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go build -tags nodynamic -o to-be-deleted-after-use/slice-107/banco-bin/banco ./tools/banco
CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go build -tags nodynamic -o to-be-deleted-after-use/slice-107/banco-bin/gestrip ./cmd/gestrip
to-be-deleted-after-use/slice-107/banco-bin/banco su -nome s107a -immagine gestrip-standalone:fab-225 \
  -gestrip $PWD/to-be-deleted-after-use/slice-107/banco-bin/gestrip -dati sintetici-import
to-be-deleted-after-use/slice-107/banco-bin/banco esegui -nome s107a -banco $PWD/to-be-deleted-after-use/slice-107/banco-bin/banco
```

(`esegui` con gli scenari, le deviazioni e lo script del browser predefiniti; poi l'esito a `scripts/check` con
`GESTRIP_BANCO_ESITO` prima di `banco giu -nome s107a`.) `banco su` va fatto con il binario compilato, mai con `go run`: il
modello finto, come il bot, gira da lì.


**Correzione fuori perimetro (ok del coordinatore, 20/11/2026):** in `tools/banco/browser/banco.mjs` (due punti, scenari
`lotti-invio` e `lotti-reinvio`) la riduzione del gettone a «/lotto/<gettone>» finiva con `\b`, che non vale dopo un «-»
finale: un gettone del Go che finiva con «-» (circa uno su 64) restava in chiaro e dava 6 inattese in s107b. Ora
`(?![A-Za-z0-9_-])`.


**Giro s107c (20/11/2026): VERDE al primo giro.** HTTP 90 scenari, 1370 passi, 0 inattese, 0 errori; browser 31 scenari,
62 viste, 0 inattese, 0 errori; la guardia sotto 10 GB non è mai scattata, nessun «busy» o «locked» nel log dello
standalone. Copertura con l'esito: 353 voci, chiuse 216, rosse 0, parziali 16, aperte 110, fuori v1 11.

## Per il manuale

- In «Impostazioni › Assistente» il titolare indica il modello per l'estrazione dal dettato (indirizzo del server, nome del
  modello, chiave se il server la chiede) e il servizio di trascrizione (indirizzo, chiave, parole del negozio). Il dipendente
  non vede la pagina.
- Senza niente scritto l'installazione non manda niente a nessuno: la pagina di accettazione non mostra il dettato.
- L'indirizzo comincia con `http://` o `https://`. Una chiave si può usare con `http://` solo se il modello sta sulla stessa
  macchina (`127.0.0.1`, `localhost`); verso un'altra macchina serve `https://`.
- La chiave non si rivede mai: la casella resta vuota con «salvata — lascia vuoto per non cambiarla»; lasciarla vuota la tiene,
  «Cancella la chiave salvata» la toglie.
- Le parole del negozio: una per riga (anche separate da virgole), al massimo 2000 caratteri una volta unite.
- «✦ Prova il modello» manda una frase fissa senza clienti e mostra l'inizio della descrizione che il modello ha capito, o il
  motivo per cui non ha risposto.
- Ogni salvataggio finisce nel registro con i soli nomi dei campi cambiati; ogni prova come «assist.extract» con il numero dei
  caratteri.

## Dubbi

1. **Banco dopo `assistente-salva`** (Scelte 6, approvata): nel giro non gira più niente dopo di lui (è in fondo al browser, e
   il browser è l'ultimo livello), ma l'istanza resta cambiata: un secondo `esegui` o una slice futura che aggiunge uno
   scenario «in fondo» lo deve sapere. La 108 configurerà comunque le due app sul modello finto già in `banco su` (serve per
   `nuova` in HTTP); allora questo scenario potrà partire da «configurato».
2. **Testi dell'estrazione dopo la D-103** e **seconda serratura nel client:** in lista in `docs/domande-aperte.md` (note 7 e
   8 della slice 107, coordinatore 20/11/2026). Verificato per la nota 8: l'import copia indirizzi, glossario e chiavi
   dell'assistente così come sono, senza la validazione del salvataggio. Non implementata.
3. Gli scenari nuovi non sono mai stati provati contro lo standalone vero: il primo giro potrebbe segnalare differenze di markup
   o di selettori (per esempio gli errori dello standalone sotto `div.fieldset`, il testo del pulsante della prova).
