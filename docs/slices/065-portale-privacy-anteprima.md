# 065 — Portale ritirato `/m/`, informativa `/privacy`, anteprima cliente (T4.5, quinta e ultima slice)

- **Fonte:** via di Stefano del 09/11/2026 (T4.5 → T4.6); parte della 064 della ripresa 063, divisa
  (ripresa `2026-11-10-fine-064.md`). Lo standalone a `go-baseline` = `302439d`.
- **Voci:** CLI-27 e RIP-S-27 (anteprima cliente), CLI-53 (`/m/` e `sw.js`), CLI-54 (`/privacy`, testo
  da decidere: Dubbio 1).
- **Deviazioni:** D-64 (numero visibile nell'anteprima, come `/r/`). Nessuna nuova.
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna migrazione.
- **Con questa slice la T4.5 è finita** (restano i pezzi rimandati ai canali: riquadri e iscrizioni
  email/Telegram, avvisi dopo la risposta del negozio, T4.6/T4.7).

## Cosa c'è

- **Anteprima cliente** (`internal/buste/anteprima.go`, `internal/web/anteprima_cliente.go`):
  `GET /repairs/{id}/anteprima-cliente` sulla faccia interna, per tutto il personale (come lo
  standalone, che chiede solo l'accesso). La pagina è quella della controparte (cliente, o oreficeria
  per una B2B, con le foto visibili a lei), letta con `paginaDove`, con la barra
  `#customer-preview-banner` («← Indietro» alla scheda) e tutti i controlli in un `fieldset disabled`:
  moduli senza `action` né `_csrf_token`, niente `data-poll-url`. Nessun token emesso o usato, nessun
  segno di lettura; l'unica scrittura è `repair.customer_preview_viewed` con `bag_number` = numero
  interno. Funziona con link revocato, scaduto o mai emesso, e anche su una consegnata (come lo
  standalone: si nasconde solo il pulsante). Cestino, inesistente, id malformato → 404 senza tipo e
  senza audit; audit che non si scrive → 503. Il messaggio del personale si toglie (`clear_flash`),
  anche sul 404. `private, no-store` e `no-referrer`. Le foto passano dalla rotta del personale.
  Pulsante «👁 Anteprima cliente» come ultima azione della scheda, non sulle consegnate.
- **`/m/`** (`internal/web/portale_privacy.go`): `GET /m/{codice}` e `/m/{codice}/{resto...}` → 410
  «Portale non più attivo» con nome e telefono del negozio («Negozio» senza profilo);
  `GET /m/{codice}/sw.js` fuori dal pipeline delle pagine, byte per byte uguale allo standalone
  (`text/javascript; charset=utf-8`, `no-cache`, nessun cookie). Il codice non si legge né si registra;
  non conta nel limitatore. Solo faccia pubblica.
- **`/privacy`**: testo uguale allo standalone, intestazione del titolare dal profilo (ragione sociale,
  nome, «Il negozio»; campi vuoti senza etichetta; «ai recapiti sopra indicati» solo con contatti,
  altrimenti «direttamente in negozio»), QR di `indirizzo_pubblico + "/privacy"` (mai l'`Host`; senza
  indirizzo niente QR), pulsante di stampa (`data-print-page` in `app.js`), nessun audit. Solo faccia
  pubblica.
- **Banco:** Caddy manda anche `/m/*` e `/privacy` al Go; scenari `portale-privacy`,
  `anteprima-cliente`, `anteprima-cliente-dipendente`; 12 righe D-64 in `deviazioni.json`.

## Prove

- `buste/anteprima_test.go` (4), `web/anteprima_cliente_test.go` (8, con la fotografia intera delle
  tabelle prima e dopo, la POST forgiata, il messaggio del personale tolto anche sul 404),
  `web/portale_privacy_test.go` (10, con telefono vuoto e sede senza contatti); tabella dei permessi,
  `Referrer-Policy`, barra delle azioni della scheda aggiornate. Il rosso c'era prima del codice (18
  test caduti).

## Mutazioni

Implementazione: 28, tutte compilano, tutte uccise (2 dopo un test aggiunto: CSRF nel modulo
dell'anteprima, telefono vuoto). Review: 28, 25 uccise, 1 equivalente (`ambito = 'default'`, già un
CHECK dello schema), 2 sopravvissute (M06 messaggio non tolto sul 404, M25 «recapiti» legato alla
sede) → due test nuovi, ora uccise.

## Banco

Istanza `s065`, `fab-225`, `-dati sintetici-import`. Primo giro: 8 inattese, artefatti degli scenari
(il ritorno alla pagina chiesta dopo il login, un conteggio del riquadro email che è T4.6). Giro finale
su istanza nuova: **BANCO VERDE**, 58 scenari, 1084 passi, 1529 attese, **0 inattese**; **BANCO
BROWSER VERDE**, 12 scenari, 24 viste, 0 inattese. `banco giu`. Nessuna correzione al Go per il banco.
I due test della review non cambiano il comportamento: banco non rifatto. Nei dati del banco nessuna
busta con preventivo in attesa: quel ramo dell'anteprima lo provano i test Go.

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo i test della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- Nella scheda «👁 Anteprima cliente» mostra la pagina come la vede il cliente (o l'oreficeria), con
  tutto spento: non si manda niente e non si segna niente come letto. Non c'è sulle buste consegnate.
- Chi apre un vecchio indirizzo del portale delle oreficerie (`/m/…`) legge «Portale non più attivo»
  con nome e telefono del negozio; il telefono smette di mostrare i dati vecchi.
- L'informativa privacy è su `/privacy`, con il QR per aprirla dal telefono e il pulsante per stamparla.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Testo dell'informativa (MEDIA, review).** È uguale allo standalone, ma descrive cose che il Go
   oggi non fa: avvisi email (T4.6) e Telegram (T4.7), dettatura e raccolta dei dettati (T4.10), foto
   delle pratiche chiuse ridotte e poi eliminate (RIP-S-55, non ancora portata), vendite e garanzie
   (T4.12/T4.13, dati già importati). I paragrafi della dettatura dicono «server in Germania (UE)»:
   col Go sull'apparecchio in negozio è **falso**. **Raccomandazione:** tenere il testo e la data
   (`2026-10-27`) finché il Go non va in produzione; prima del cutover rivederlo tutto con Stefano,
   e le frasi sulla Germania riscriverle comunque con un testo deciso da lui. Il Go non si usa con
   clienti veri prima del cutover, quindi oggi l'informativa non è pubblicata da nessuna parte.
2. **Link «Informativa privacy» nell'anteprima:** l'anteprima sta sulla faccia interna, dove `/privacy`
   non c'è; il link porta a `indirizzo_pubblico + "/privacy"` (a `/privacy` se l'indirizzo manca). Lo
   standalone scrive `/privacy`. **Raccomandazione:** tenerlo.
3. **QR della privacy senza `indirizzo_pubblico`:** non c'è (lo standalone ha sempre un indirizzo).
4. **Titolo della finestra** delle pagine pubbliche senza titolo: «GestRip · Laboratorio» nello
   standalone, «GestRip» nel Go (differenza di prima, non della 065). Per una slice di rifinitura.
5. Come lo standalone: un telefono scritto vuoto mostra «Telefono:» con il link vuoto; `/privacy` non
   vede l'utente loggato (la faccia pubblica non ha sessioni del personale).

## Review

Revisore indipendente, 10/11/2026 (pc-fisso, slice non committata). Nessuna ALTA né MEDIA sul codice.

- **BASSA, test aggiunto:** il messaggio del personale tolto anche sul 404 non era provato (M06).
- **BASSA, test aggiunto:** «ai recapiti sopra indicati» con la sede e senza contatti (M25).
- **BASSA, facoltativa, non fatta:** il banco non apre l'anteprima di una consegnata né di una nel
  cestino (provati con una sonda e dai test Go).
- **MEDIA di prodotto:** testo dell'informativa (Dubbio 1).
- **Verificato senza rilievi:** nessuna scrittura oltre l'audit, nessuna POST possibile, permessi,
  B2B con foto dell'oreficeria, nessun dato interno nella pagina, intestazioni; `/m/` (codice mai letto,
  `sw.js` identico, instradamento come Phoenix, solo faccia pubblica); `/privacy` (titolare, QR non
  dall'`Host`, escape, testo uguale parola per parola); deviazioni del banco solo D-64.
  `scripts/check` verde.
