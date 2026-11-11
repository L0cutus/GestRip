# 068 — «Salva e avverti», avviso dal pannello QR, righe `\r\n`, coda in uscita (T4.6, terza slice)

- **Fonte:** via dell'Orchestratore dell'11/11/2026 (via di Stefano del 09/11 per la T4.6), ripresa
  `2026-11-11-fine-067.md`. Lo standalone a `go-baseline` = `302439d`.
- **Voci:** la voce di «📲 Salva e avverti cliente» del modulo di modifica, RIP-H-26 (avviso di ritiro dal
  pannello QR dopo «✅ Valutata»), CLI-51 (coda durevole in uscita con tentativi).
- **Deviazioni:** D-74 (anche per l'avviso dal pannello), D-46 estesa (pannello senza
  `indirizzo_pubblico`). Nessuna D nuova; due differenze dichiarate senza D (sotto).
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Migrazione
  `0018_indice_invii_busta.sql` (solo un indice).

## Cosa c'è

- **«📲 Salva e avverti cliente»** (`internal/web/modifica.go`, `pagine/scheda.html`, `app.js`): salva come
  «Salva», poi il popup dell'avviso della 066 (`then_notify/2`); un rifiuto ridisegna il modulo senza popup
  e senza scrivere; `then` forgiato su una busta non pronta o senza numero = salvataggio semplice. Il
  pulsante c'è solo su una busta pronta con un numero. In `app.js` il pulsante premuto si copia in un
  campo nascosto prima che `data-invio-unico` spenga i pulsanti (tolto al ritorno con `pageshow`).
- **Avviso dal pannello QR** (`buste.AnnunciaRitiro` = `ReadyNotice.announce/3`, `web/pannello_qr.go`,
  `pagine/cercaqr.html`): solo «✅ Valutata» avvisa; ordine recapito, testo, link (D-46 prima di emettere),
  blocco con UPDATE condizionato (avviso unico); lotto aperto e «già avvisata» con le frasi dello
  standalone; audit `ready_notice.sent` e `outgoing.handed_off`. Il riquadro `#qr-opening` con «↗ Apri
  WhatsApp» (`target=_blank`); «Chiudi» ed Esc con l'avviso registrato ricaricano la lista (D-74).
  Registro: tipo dell'errore, `Warn` per i rifiuti attesi.
- **Righe `\r\n`** (`web/a_capo.go`): normalizzate a `\n` nelle caselle interne (risposta al cliente,
  descrizione e note di modifica e «Nuova riparazione», popup, testi), come lo standalone che riceve `\n`
  da LiveView. La pagina pubblica (`reason`, `testo`) resta com'è: lì lo standalone salva `\r\n` come il
  Go (modulo vero e `FormData`).
- **Coda in uscita** (`internal/uscita/coda.go`): `Accoda`, `Giro`, `Lavoro`, interfaccia `Provider`,
  `Rifiuto`. Come `outgoing.ex` e `dispatcher.ex`: 5 tentativi, attese 10 s, 60 s, 5 min, 30 min, giro
  ogni 30 s, al più 50 righe per id, guardia `stato = 'queued'` su ogni UPDATE, ripresa dopo un riavvio,
  errore mai con dati (troncato a 500, segreti tolti anche dal riferimento), panico salvato come
  `raised`. **Si leggono solo le righe dei canali che hanno un provider:** le altre (per esempio le
  Telegram importate finché non c'è la T4.7) restano `queued`, senza tentativi. `gestrip serve` avvia la
  coda solo con un provider: oggi nessuno, la coda non gira (SMTP con la 069). Indice su
  `messaggi_in_uscita(busta_id)` (0018, `IndiciSoloGo` nella mappa dell'import).
- **Banco:** `banco.mjs` (scenario `salva-avverti`, `pannello-qr` esteso), `bench/browser/salva-avverti.json`,
  `pannello-qr.json`; le aperture di wa.me si contano tutte, senza togliere i doppioni.

## Differenze dichiarate senza D

- `AnnunciaRitiro` scarta subito un numero presente ma non componibile, prima del testo e del link; lo
  standalone se ne accorge dentro `notify/4`, dopo aver emesso il link. Il messaggio è lo stesso.
- Un panico del provider si salva come `raised` senza il messaggio dell'eccezione (mai dati).

## Prove

`web`: `salva_avverti_test.go` (5), `pannello_qr_avviso_test.go` (7, il test saltato della T4.2 ora attivo),
`a_capo_test.go` (4); `uscita/coda_test.go` (10: accoda e rifiuti, accetta, tentativi e attese, errore senza
dati e panico, riga chiusa nel frattempo, riferimento senza segreti, solo i canali con provider, ripresa,
indici); `cmd/gestrip/coda_uscita_test.go` (1). Rosso preso prima del codice (non compilava, poi 13 e 7 test
caduti con gli stub; per la review `TestGiroSoloCanaliConProvider`).

## Mutazioni

Implementazione: 43 (38 più 5 per la review), tutte compilano, tutte uccise. Review: 30, 27 uccise, 3
sopravvissute (guardia `queued` sul fallimento e sulla riprova, pulizia del riferimento) → test nuovi, ora
uccise.

## Banco

`fab-225`, `-dati sintetici-import`. Giro A: HTTP verde; browser 1 inattesa, il numero nel testo dell'avviso
dal pannello (D-64/D-82, artefatto dello scenario, normalizzato come `leggiPopup`). Giro finale dopo le
correzioni della review (`s068c`): **BANCO VERDE**, 61 scenari, 1097 passi, 0 inattese; **BANCO BROWSER
VERDE**, 15 scenari, 30 viste, 0 inattese; aperture di wa.me sempre 1. `banco giu`. Lo scenario nuovo usa una
busta che nessun altro scenario usa e la rimette in lavorazione; il testo salvato per lo scenario viene
ripristinato.

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- Nella modifica della scheda, su una busta pronta, «📲 Salva e avverti cliente» salva e apre subito il
  messaggio per il cliente.
- Nel pannello del QR, dopo «✅ Valutata», GestRip registra l'avviso di ritiro e mostra «↗ Apri WhatsApp»;
  l'avviso parte una volta sola.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Pagina pubblica con `\r\n`:** lasciata uguale allo standalone. **Raccomandazione:** tenerla così (per
   `\n` anche lì servirebbe una D).
2. **Popup di «Salva e avverti» a pagina intera** (nello standalone è sopra la scheda), come dopo
   «Rispondi» nella 066. **Raccomandazione:** accettarlo.
3. **Pannello QR senza `indirizzo_pubblico`:** lo stato avanza e la lista dice «✓ valutata — avviso non
   registrato: manca «indirizzo_pubblico = …» …»; le parole prima della frase della D-46 sono
   dell'implementatore. **Raccomandazione:** confermarle (insieme al Dubbio 2 della 067).
4. **Doppio «✅ Valutata» senza JavaScript:** il secondo non avvisa di nuovo e il link di WhatsApp del primo
   si recupera dalla scheda. **Raccomandazione:** accettarlo.
5. **Coda spenta senza provider** e canali senza provider lasciati in coda. **Raccomandazione:** tenerla
   così fino alla 069 (SMTP) e alla T4.7 (bot).

**Vincolo per la T4.7:** `Messaggio` e `Accoda` non portano ancora `formato` e `tastiera` (`parse_mode`,
`reply_markup`) delle righe Telegram, anche importate; e le regole Telegram della coda (429, 400, 403).

## Review

Revisore indipendente, 11/11/2026 (pc-fisso, slice non committata). Nessuna ALTA.

- **MEDIA, corretto:** con il primo provider (SMTP, 069) le righe Telegram importate sarebbero diventate
  `failed` in circa 36 minuti; ora si leggono solo i canali con un provider.
- **BASSA, test aggiunti:** guardie `queued` sul fallimento e sulla riprova, pulizia del riferimento.
- **BASSA, documentate:** vincolo `formato`/`tastiera` per la T4.7, differenza di `AnnunciaRitiro`.
- **BASSA, corretti:** registro del pannello (tipo, `Warn`), conteggio delle aperture di wa.me nel banco.
- **BASSA, non fatto:** `Accoda` controlla il messaggio prima del provider (lo standalone il contrario);
  oggi nessuno la chiama.
- **Verificato senza rilievi:** ordine salva → avviso, `then` forgiato, `app.js` (nessun altro modulo
  cambia), pannello (solo «Valutata», avviso unico, lotto, D-46 prima del link, audit, D-74), `\r\n` su tutte
  le caselle interne e la pagina pubblica uguale allo standalone, regole della coda, migrazione e mappa,
  nessuna rotta nuova, escape, testo mai nei registri. `scripts/check` verde.
