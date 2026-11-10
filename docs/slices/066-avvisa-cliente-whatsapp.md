# 066 — Popup «📲 Avvisa cliente» e invio WhatsApp (T4.6, prima slice)

- **Fonte:** via di Stefano del 09/11/2026 (T4.5 → T4.6), ripresa `2026-11-10-fine-065.md`. Lo standalone a
  `go-baseline` = `302439d` (con il ritorno alla lista della slice 225).
- **Voci:** CLI-29…36 (popup, invio, modello automatico, segnaposto, numero e `wa.me`, timbro «link
  inviato», avviso di pronta unico e azzeramento), RIP-H-17 (📲 dalla lista), RIP-S-23 (riquadro
  dell'avviso nella scheda), RIP-S-20 (popup WhatsApp dopo «Rispondi al cliente»).
- **Deviazioni:** D-64 (`{numero}` = numero visibile), **D-74 fatta per il popup** (con JavaScript le
  scritture prima, poi WhatsApp e la lista ricordata; senza JavaScript come lo standalone).
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna
  migrazione.

## Cosa c'è

- **Dominio** (`internal/buste`):
  - `whatsapp.go`: `NumeroWhatsApp` (= `number/1`, tabella e ordine dei prefissi, 0039/00/39/+) e
    `IndirizzoWaMe` (codifica come `URI.encode` con `char_unreserved?`);
  - `modelli.go`: le 9 famiglie `wa_*` con etichette, campi e predefiniti parola per parola; testo
    in vigore da `impostazioni` `outgoing.template.<famiglia>`, altrimenti il predefinito; un testo
    salvato che non si compone è un errore (`ErroreModello` = `format_error/2`), mai il ripiego;
  - `avvisi.go`: `ApriAvviso` compone e non scrive (`{link}` resta letterale); `InviaAvviso` rilegge
    la busta e rifiuta se sono cambiati controparte, numero, canale o tipo (tipo sconosciuto, o
    `reply` fuori dalla risposta = `ErrTipoCambiato`), busta non pronta, già avvisata o in un lotto
    aperto; emette o riusa il link pubblico in una transazione sua (`garantisciLink`, motivi
    `expired`, `revoked`, `legacy_hash_only`, `missing`), poi la riga WhatsApp `accepted`,
    `outgoing.handed_off`, il timbro `link_inviato_il` (solo cliente diretto, una volta); per
    l'avviso di ritiro blocco `avviso_pronta_il`, evento `notified` e `ready_notice.sent` in una
    transazione con UPDATE condizionato (due invii insieme = una riga). `AzzeraAvviso`: evento
    `reset` e `ready_notice.reset`. Testo registrato senza spazi ai bordi (`Message.base_changeset`,
    trovato dal banco). Canale: oggi sempre WhatsApp, punto di scelta `sceglieTelegram` per la T4.7.
    Destinatario B2B: l'oreficeria (id > 1), «Collega» senza nome.
  - `LinkTelegram`: i controlli di `issue_link/4` (link, cliente diretto, busta non chiusa, canale).
- **Pagine** (`internal/web/avvisi.go`, `pagine/avviso.html`, `app.js`, `app.css`):
  - `GET /repairs/{id}/avviso`: pagina del popup, non scrive; dalla scheda, dalla lista
    (`da=lista&lista=…`) e dopo «Rispondi» (`scopo=risposta`);
  - `POST /repairs/{id}/avviso`: senza JavaScript redirect a `wa.me`; con `Accept: application/json`
    `{url}` e `no-store`; `app.js`: scheda vuota aperta al tocco, `fetch`, poi `wa.me` e ritorno alla
    lista ricordata (D-74); fetch fallito o rifiuto = la scheda vuota si chiude;
  - `POST /repairs/{id}/azzera-avviso`;
  - scheda: riquadro `#repair-notice`, pulsante `#repair-notify`; lista: 📲 `#notify-repair-N` sulle
    buste aperte; dopo «Rispondi» si va al popup (busta chiusa: di nuovo la scheda);
  - `GET /r/{token}/telegram` come lo standalone a canale spento: 503 text/plain «Servizio notifiche
    temporaneamente non disponibile.»; link morto o B2B 404 (contato dal limitatore); busta chiusa
    torna a `/r/<token>` con «Questa riparazione non riceve più avvisi.». Si completa con la T4.7.
- **Banco:** `bench/scenari/avvisi-whatsapp.json` (HTTP, `in_fondo`), `bench/browser/whatsapp.json` e lo
  scenario `whatsapp` di `banco.mjs` (wa.me catturato, mai aperto); normalizzatore D-64 esteso ad
  «Avvisa per la busta N/AA».

## Prove

- `buste`: `whatsapp_test.go` (3), `modelli_test.go` (4), `avvisi_test.go` (20: modello, segnaposto,
  stima, B2B e «Collega», link e motivi, controparte cambiata dentro `garantisciLink`, avviso unico,
  gara persa, lotto aperto nel blocco, azzeramento, tipo non valido, trim).
- `web`: `avvisi_test.go` (11: popup, invio con e senza JSON, CSRF, permessi, ordine del JS, ridisegno
  dopo un testo vuoto, link Telegram spento); tabella dei permessi, guscio, scheda e lista aggiornati.
- Rosso preso prima del codice (dominio non compilava, 8 test web caduti; per le correzioni della
  review `TestInviaTipoNonValido` e `TestLinkTelegramSpento`).

## Mutazioni

Implementazione: 32, tutte compilano, tutte uccise (3 dopo test aggiunti). Review: 37, 32 uccise, 5
sopravvissute (m07 lotto nel blocco, m13 controparte in `garantisciLink`, m23 «Collega», m29 campi
nascosti dopo un testo vuoto, m31 formato di `{stima}`) → test nuovi, ora uccise; più R32 (`reply` fuori
dalla risposta) e R33 (503 → 404), uccise.

## Banco

`fab-225`, `-dati sintetici-import`. Giro 1: 2 inattese HTTP (attese dello scenario); nel browser il Go
del banco non ha `indirizzo_pubblico`, quindi i testi con `{link}` sono rifiutati: lo scenario manda
testi senza link (il link lo provano i test Go); wa.me annotato anche con `request` e `framenavigated`.
Giro 2: una **differenza vera**, il trim del testo registrato, corretta. Giro finale (`s066c`): **BANCO
VERDE**, 59 scenari, 1091 passi, 0 inattese; **BANCO BROWSER VERDE**, 13 scenari, 26 viste, 0 inattese.
`banco giu`. Le correzioni della review non toccano percorsi del banco: giro non rifatto.

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- Nella lista e nella scheda «📲» apre il messaggio per il cliente (o per l'oreficeria) già scritto, che
  si può correggere; «Invia» lo registra e apre WhatsApp, poi GestRip torna alla lista di prima.
- Il testo lo sceglie GestRip: avviso di ritiro se la busta è pronta e non è ancora stata avvisata,
  altrimenti aggiornamento; dopo «Rispondi al cliente» sempre l'aggiornamento.
- L'avviso di ritiro parte una volta sola; nella scheda si vede quando è partito e si può azzerare.
- Senza numero di telefono valido il 📲 non c'è.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Senza JavaScript** «Invia» porta la pagina stessa a `wa.me` (come lo standalone quando il browser
   blocca la scheda nuova). Alternativa: una pagina con «↗ Apri WhatsApp». **Raccomandazione:** il
   redirect (un tocco solo).
2. **`{numero}` nei testi è il numero visibile D-64:** senza etichetta il cliente legge «n° N. 45/26».
   **Raccomandazione:** nei soli testi per il cliente «45/26» senza «N.» (sarebbe una D nuova; stesso
   dubbio della 061 per la pagina).
3. **Senza `indirizzo_pubblico`** un testo con `{link}` non parte («Il link della busta non si è potuto
   creare…»); nello standalone l'indirizzo c'è sempre. **Raccomandazione:** estendere la D-46 con la
   riga che dice cosa configurare.
4. **`{telegram_optin}` nei testi:** oggi il link porta al 503 «Servizio notifiche temporaneamente non
   disponibile.», come lo standalone a canale spento; si completa con la T4.7. **Domanda:** nei testi
   del negozio c'è `{telegram_optin}`?
5. **Differenze dichiarate in SVILUPPO.md:** nessun «Messaggio registrato in lista…» dopo l'invio con
   JavaScript (anche lo standalone dalla 225 lo mostra solo un attimo); dopo «Rispondi» il popup è una
   pagina intera (Esc e tocco fuori non chiudono, «Annulla» sì).
6. **Link «Apri il lotto»** del riquadro per le buste in un lotto aperto: arriva con la pagina dei lotti
   (T4.9).

**Rimandati:** «📲 Salva e avverti cliente» del modulo di modifica e l'avviso di ritiro dal pannello QR
dopo «✅ Valutata» (RIP-H-26, test saltato in `pannello_qr_test.go`): proposti per la 067. Testi
Telegram ed email ed editor (067, 069); ramo Telegram (T4.7).

## Review

Revisore indipendente, 10/11/2026 (pc-fisso, slice non committata). Nessuna ALTA.

- **MEDIA, corretto:** `{telegram_optin}` portava a `/r/<token>/telegram`, rotta assente (404 contato dal
  limitatore) mentre il popup prometteva «Servizio notifiche temporaneamente non disponibile.». Ora la
  rotta risponde come lo standalone a canale spento.
- **BASSA, corretto:** `tipo` forgiato nei campi nascosti (`tipo=xyz`, `reply` fuori dalla risposta).
- **BASSA, corretto:** `ridisegnaAvviso` trasformava ogni errore in «Riparazione non trovata.».
- **BASSA, dichiarato:** le due differenze del Dubbio 5.
- **Test mancanti, aggiunti:** m07, m13, m23, m29, m31.
- **Verificato senza rilievi:** predefiniti e scanner uguali a `templates.ex`, contesto dei segnaposto,
  `NumeroWhatsApp`, `IndirizzoWaMe` (niente a capo nel Location), B2B, avviso unico sotto gara, timbro,
  link pubblico, trim, nessuna scrittura su apri/annulla, tutto scritto prima del 302 e del JSON, CSRF,
  permessi, nessun open redirect, escape, testo mai nei log né nell'audit, ordine di `app.js`,
  normalizzatore del banco. `scripts/check` verde.
