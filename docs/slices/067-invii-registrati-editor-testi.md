# 067 — Invii registrati, editor dei testi, D-81, D-82, D-46 estesa (T4.6, seconda slice)

- **Fonte:** via dell'Orchestratore dell'11/11/2026 (via di Stefano del 09/11 per la T4.6), ripresa
  `2026-11-10-fine-066.md`; risposte di Stefano del 10/11/2026 ai Dubbi delle slice 062, 064, 065 e 066
  (sezione «Decisioni di Stefano del 10/11/2026» di `docs/SVILUPPO.md`). Lo standalone a `go-baseline`
  = `302439d`.
- **Voci:** CLI-37 e RIP-S-21 («📨 Invii registrati» e «Togli dalla lista»), CLI-38 e ADM-13 (editor dei
  testi con il validatore dei segnaposto); CLI-09 (D-81), CLI-33 (D-82), CLI-32 (D-46 estesa,
  `{telegram_optin}`).
- **Deviazioni:** **D-81** e **D-82** nuove (decise da Stefano il 10/11), **D-46 estesa** ai testi con
  `{link}`. Tutte fatte.
- **Documenti senza codice:** inventario privato, CLI-17 e CLI-28 corrette (pagina storica di una busta
  conto terzi senza foto); PIANO: numero di prima della D-77 prerequisito della T4.11, informativa
  privacy da riscrivere prima del cutover (T5).
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna
  migrazione.

## Cosa c'è

- **Invii registrati** (`internal/buste/invii.go`, `internal/web/invii.go`, `pagine/scheda.html`): sotto
  «Rispondi al cliente» il riquadro `#repair-send-history` «📨 Invii registrati (N)» con le didascalie
  per canale, le righe dalla più recente (canale, «· all'artigiano», stato, ora, indirizzo o «contatto
  scelto sul telefono», testo con i link dell'email mascherati e l'HTML di Telegram ridotto a testo),
  come lo standalone. «Togli dalla lista» (`POST /repairs/{id}/togli-invio`, personale, CSRF) solo
  per le righe WhatsApp che non sono un avviso di ritiro, in una transazione con l'audit
  `outgoing.handoff_discarded` (mai il testo); il timbro «link inviato» resta.
- **Editor dei testi** (`buste/modelli.go`, `web/testi.go`, `pagine/testi.html`): `GET/POST
  /settings/templates`, solo titolare, prima scheda delle impostazioni («Messaggi WhatsApp»; `/settings`
  apre lì finché il Go non ha la scheda Negozio). Le 15 famiglie dello standalone (9 WhatsApp, 2
  Telegram, 4 email) con etichette, campi, predefiniti ed esempi parola per parola; validatore con i
  messaggi dello standalone (`format_error`, `rule_error`, oggetto su una riga, campi obbligatori), 4000
  grafemi, casella vuota = predefinito, righe `\r\n` → `\n`; audit `outgoing.template_saved` (famiglia,
  predefinito, valido; mai il testo) nella stessa transazione. I testi Telegram ed email si salvano già;
  li compongono la T4.7 e la 069.
- **D-81** (`buste/preventivo.go`): la revoca del rifiuto scrive `repair.status_changed
  {"status":"preventivo"}` nella stessa transazione, dopo `quote.rejection_revoked`, solo se lo stato
  cambia; senza motivo.
- **D-82** (`buste.NumeroNeiTesti` in `visibile.go`): `{numero}` nei testi senza «N.»; il titolo del popup,
  le pagine e le stampe restano con il numero visibile D-64.
- **D-46 estesa:** senza `indirizzo_pubblico` il popup mostra `#notice-link-missing` e un testo con `{link}`
  o `{telegram_optin}` non parte («Messaggio non inviato: manca «indirizzo_pubblico = …» in <file>, poi
  riavvia gestrip serve con «…».»), controllato prima di emettere qualunque link
  (`ErrSenzaIndirizzoPubblico`).
- **`{telegram_optin}`:** con un link valido porta sempre al «canale spento» dello standalone, mai a un 404
  (test nuovo).
- **Popup senza JavaScript:** il testo arriva con `\r\n` dal modulo nativo; ora si normalizza a `\n` prima
  di salvarlo e di costruire `wa.me` (difetto della 066 trovato dalla review).
- **Banco:** `bench/scenari/testi.json`, `testi-dipendente.json`, `bench/browser/testi-invii.json`, scenario
  `testiInvii` di `banco.mjs`; `leggiPopup` accetta il numero con e senza «N.» (la D-82 la provano i
  test Go, non il banco browser).

## Prove

- `buste`: `testi_test.go` (6), `invii_test.go` (7, con D-81, D-82 e `{telegram_optin}`), `avvisi_test.go` e
  `modelli_test.go` aggiornati.
- `web`: `invii_test.go` (2), `testi_test.go` (4), `avvisi_test.go` (D-46, `\r\n` senza JavaScript), tabella
  dei permessi, guscio, interfaccia.
- Rosso preso prima del codice (il dominio non compilava, 7 test web caduti; per le correzioni della
  review il test `\r\n` e il caso `&amp;lt;3`).

## Mutazioni

Implementazione: 32 (30 più 2 per le correzioni della review: normalizzazione `\r\n` del popup tolta,
`&amp;` sciolto per primo), tutte compilano, tutte uccise. Review: 31, 29 uccise, 1 equivalente (`\S`
contro la classe esplicita, differisce solo su `\v`), 1 sopravvissuta (M21, `&amp;` sciolto per primo) →
caso nuovo nel test, ora uccisa.

## Banco

`fab-225`, `-dati sintetici-import`. Giro 1 (`s067a`): 2 inattese, spazi in più fra i `<code>` della pagina
dei testi, corretti nel Go. Giro 2 (`s067b`): **BANCO VERDE**, 61 scenari, 1097 passi, 0 inattese; **BANCO
BROWSER VERDE**, 14 scenari, 28 viste, 0 inattese. `banco giu`. Le correzioni della review non toccano
percorsi del banco: giro non rifatto. D-81 e D-46 non passano dal banco (le provano i test Go).

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- Nella scheda, sotto «Rispondi al cliente», «📨 Invii registrati» mostra i messaggi partiti per la busta;
  «Togli dalla lista» toglie un WhatsApp registrato per sbaglio (non l'avviso di ritiro).
- In «Impostazioni → Messaggi WhatsApp» il titolare corregge i testi dei messaggi, con l'anteprima e i
  segnaposto ammessi; casella vuota = testo predefinito.
- Senza l'indirizzo pubblico configurato i messaggi con il link della busta non partono, e il popup dice
  cosa configurare.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Anteprima dei testi** solo dopo «Salva» (o un rifiuto); nello standalone cambia a ogni tasto.
   **Raccomandazione:** tenerla così e farla dal vivo in una slice di rifinitura.
2. **Senza `indirizzo_pubblico` nessun testo predefinito parte**, perché tutti hanno `{link}`, e il
   dipendente legge un percorso da configurare su cui non può agire. Le parole prima della frase della
   D-46 le ha scelte l'implementatore. **Raccomandazione (review):** aggiungere «Togli {link} dal testo
   per mandarlo senza link.»; oppure Stefano detta il testo.
3. **`\r\n` dagli altri moduli senza JavaScript** (`reply[body]`, descrizione e note della modifica e di
   «Nuova riparazione», motivo del rifiuto e messaggio del cliente sulla pagina pubblica): il Go salva `\r\n`, lo standalone `\n`. Corretto solo nel popup. **Raccomandazione:** una
   correzione piccola e generale in una slice di rifinitura (o nella 068).
4. **«Ripristina predefinito» senza JavaScript** parte senza la conferma (`data-confirm` è di `app.js`);
   lo standalone richiede sempre JavaScript. **Raccomandazione:** accettarlo.
5. **Indici:** la lista degli invii legge `messaggi_in_uscita` per busta senza indice (come lo
   standalone). **Raccomandazione:** un indice con la migrazione della coda (068).

**Rimandati alla 068:** «📲 Salva e avverti cliente» del modulo di modifica e l'avviso di ritiro dal
pannello QR dopo «✅ Valutata» (RIP-H-26, test saltato in `pannello_qr_test.go`).

## Review

Revisore indipendente, 11/11/2026 (pc-fisso, slice non committata). Nessuna ALTA.

- **MEDIA, corretto:** popup senza JavaScript con `\r\n` nel testo salvato e in `wa.me` (difetto della
  066).
- **BASSA, test aggiunto:** `&amp;` sciolto per ultimo nel testo Telegram della lista (M21).
- **BASSA, dichiarato nel codice:** il banco browser non controlla la D-82.
- **BASSA, Dubbio 5:** indici. **BASSA, trascurabile:** un POST forgiato senza `template[text]` ripristina il
  predefinito (lo standalone non fa niente).
- **Verificato senza rilievi:** 15 famiglie e predefiniti uguali a `templates.ex`, validatore, ordine dei
  controlli, audit senza testo nella stessa transazione, escape nella casella, nell'anteprima e nella lista,
  lista degli invii e «Togli dalla lista» (id di un'altra busta, canale, avviso di ritiro, cestino), D-81,
  D-82, D-46 prima del link, `{telegram_optin}` mai 404, nessun testo nei registri, banco.
  `scripts/check` verde.
