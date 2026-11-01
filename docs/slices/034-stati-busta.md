# 034 — Stati, riapertura e attesa della busta (T4.1)

- **Fonte:** T4.1 del PIANO; piano delle slice nella ricevuta della 030.
- **Voci:**
  - chiusa RIP-S-15;
  - parziali RIP-S-13 e RIP-S-14: l'email di pronta dopo il cambio di stato e la riapertura è
    dell'area email (T4.7, test saltato `TestEmailDiProntaDopoIlCambio`); per RIP-S-14 anche la
    finestra di conferma della riapertura nel browser (`TestPaginaRiaperturaConfermaNelBrowser`);
  - toccata ORE-15 (chiusura del lotto alla consegna), non segnata: la parte sulla pagina
    pubblica è di un'altra area.
- **Lavoro:** implementazione e due giri di correzioni di un sottoagente, review e mutazioni di
  un sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste`):
  - `CambiaStato` con i due grafi, e alla consegna la chiusura del lotto
    (`ReadyLots.close_after_delivery`): solo un lotto inviato, una volta sola, con le buste nel
    cestino contate, nella stessa transazione;
  - `Riapri` (`riapertura.go`): stati di riapertura, motivo obbligatorio, conservazione azzerata e
    riavviata alla chiusura successiva;
  - `Attesa` (`attesa.go`): metti e togli l'attesa della risposta del cliente, con il motivo
    facoltativo, indipendente dallo stato; scrive solo se qualcosa cambia.

  Audit, metadati e testi come lo standalone. I trigger della 0014 scattano: il contatore delle
  versioni conta anche la riapertura, e lo spegnimento Telegram non scatta su una riapertura.
- **Pagine** (`internal/web/stati.go`), tutte per il personale, POST con CSRF:
  - `POST /repairs/{id}/stato`: redirect alla scheda con «Stato aggiornato.» o «Transizione non
    consentita.». Lo stato che la busta ha già è un rifiuto, come la scheda dello standalone
    (`repair_live.ex:212-223`); il silenzio è solo della lista (T4.2, domanda aperta 5);
  - `POST /repairs/{id}/riapri`, con il guscio: «Busta riaperta.»; gli errori dei campi ridisegnano
    la scheda con i valori scritti, solo per i campi inviati (`used_input?`);
  - `POST /repairs/{id}/attesa`: «Busta messa in attesa risposta cliente.», «Attesa rimossa.» o
    «Azione non valida.».
- **Banco:**
  - passo `stati` della catena, dopo le modifiche: con le funzioni delle pagine dello standalone
    crea le buste 11–15, con stati in attesa materiali, annullata, compro oro chiusa,
    un'oreficeria con un lotto inviato e chiuso alla consegna, una riapertura e un'attesa messa e
    tolta. Apre anche 105 volte la scheda della 11/26, per avere più di una pagina di
    `repair.viewed` uguale dalle due parti. L'esito `GR_STATI` è letto in modo stretto;
  - scenario `stati`: schede di 9 buste, moduli del cambio stato e della riapertura, attesa, dal
    titolare e dal dipendente;
  - scenario `registro`: la paginazione si confronta sul filtro `action=repair.viewed`, con le
    attese che pretendono «Successiva» a pagina 1 e «Precedente» sull'ultima con i dati importati.
    Sul registro intero restano i controlli che valgono; il numero di pagine no, perché la home
    dello standalone scrive `repair.index_viewed` e quella del Go (T4.2) non c'è ancora. Scritto
    in `docs/slices/004-banco.md`.

  Del debito del banco della 032 restano: oreficeria riga 1, ora legale, nome del negozio,
  cestino, audit che fallisce, account disattivato o sessione revocata, classi.

## Differenze di forma, dichiarate

- POST con redirect invece degli eventi LiveView: cambio stato, riapertura e attesa nel Go non si
  confrontano via HTTP, li provano i test Go.
- Una busta che non c'è o nel cestino porta alla home con «Riparazione non trovata.»; lo
  standalone resta sulla scheda con «Riapertura non riuscita.», «Azione non valida.» o
  «Transizione non consentita.».
- «Riapertura non riuscita.» per un errore del database porta alla scheda con un redirect; lo
  standalone resta sulla pagina.
- Dopo «Azione non valida.» il motivo scritto si perde e il riquadro dell'attesa si chiude (nello
  standalone li tiene `validate-waiting`). `validate-waiting` non serve per il resto: il
  `<details>` resta aperto da solo mentre si scrive.
- La conferma della riapertura la dà `app.js` con `data-confirm`, come phoenix_html; senza
  JavaScript il pulsante riapre senza chiedere.
- Un `waiting[motivo]` annidato forgiato nello standalone dà «Azione non valida.», nel Go è
  ignorato: dettaglio dei parametri di Plug, della famiglia della D-30.

## Prove

- **TDD:** rosso misurato prima del codice (7 test del dominio, 8 delle pagine sugli stub). I
  test del banco del primo giro sono stati scritti dopo il codice e il loro rosso è misurato su una
  copia con gli stub; nei giri delle correzioni rosso misurato prima (stato uguale, guasto del
  lotto, campi mancanti, paginazione del registro).
- **Mutazioni:** review 67, ne compilano 64, uccise 55. Delle 9 vive, 4 sono equivalenti (id 0 o
  negativo, chiusura del lotto tentata a ogni cambio, `EXISTS` superfluo) e 5 erano buchi nei
  test (errore della chiusura del lotto scartato, ramo `used_input?`): coperti nel giro delle
  correzioni e uccisi.
- **Banco** (`fab-219`), rilanciato dalla sessione principale dopo le correzioni: verde con
  `-dati sintetici-import` e su database vuoto, 17 scenari e 231 passi, «AVANZI: nessuno». Import
  riconciliato con gli stessi 9 avvisi. Prova negativa (stati chiusi proposti fra quelli di
  riapertura): 14 differenze, banco rosso.
- `scripts/check` verde; con l'esito del banco 44 voci chiuse, 16 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA.
- **MEDIA:** la paginazione del registro, spostata su un filtro, confrontava a vuoto (una sola
  pagina) → righe create dalla catena e attese che non possono passare a vuoto.
- **BASSA:**
  - errore della chiusura del lotto senza test → guasto mirato, mutazione uccisa;
  - ramo `used_input?` della riapertura senza test → test aggiunto;
  - differenze da dichiarare → sopra;
  - nessun promemoria dell'email di pronta → test saltato con RIP-S-13 e 14.

## Domande aperte nuove

In `docs/domande-aperte.md`, domande 5–7: stato uguale sulla scheda, righe `repair.viewed` dopo le
azioni, conferma della riapertura nel browser. Nessuna blocca il lavoro.
