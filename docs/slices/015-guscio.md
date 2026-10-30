# 015 — Guscio delle pagine interne: menu (ADM-22), Impostazioni con le schede (ADM-01), tavolozze (ADM-08), parte PWA (ADM-07)

- **Fonte:** `prompt-via-area.md` (Stefano, 30/10/2026), fine T1. Standalone a `go-baseline` =
  `6eb93ed`: `layouts.ex`, `layouts/root.html.heex`, `pwa.ex`, `pwa_controller.ex`, `router.ex`,
  `priv/sw.js`, `priv/static/settings-tabs.css`; test `navigation_consistency_test.exs`,
  `settings_navigation_test.exs`, `floating_buttons_test.exs`, `close_outside_live_test.exs`,
  `pwa_test.exs`, `pwa_surface_test.exs`.
- **Voci:** ADM-22, ADM-01, ADM-08 chiuse per quello che il Go ha oggi; ADM-07 parziale.
- **Lavoro:** implementazione di un sottoagente; review e mutazioni di sottoagenti diversi; check e
  banco rilanciati dalla sessione principale.

## Cosa c'è

- `internal/web/guscio.go`:
  - **Menu ☰**: un `<details>`, quindi va anche senza JavaScript. È guidato da una tabella con le
    13 voci dello standalone, con le stesse etichette, ordine, id e regole di ruolo.
    - Compare solo una voce la cui rotta GET esiste; «Statistiche» è rimandata.
    - Poi vengono «Cambia tema» ed «Esci» (un modulo POST).
  - «🏠 Lista» in basso.
  - **Schede delle Impostazioni**, con l'elenco dello standalone e quella aperta segnata.
    `/settings`, solo per il titolare, va alla prima scheda che esiste.
  - **8 tavolozze**: `app-tema` in `localStorage`, con lo script della testa identico a
    `pwa_head`.
  - **PWA**:
    - manifest in JSON come lo scrive Jason;
    - `sw.js` byte per byte uguale a quello dello standalone;
    - le icone di ripiego.
  - **Asset nel binario** con l'hash nel nome (`/assets/<nome>-<hash>.<est>`), serviti solo dal
    percorso esatto con cache di un anno, senza cookie, e solo sulla faccia interna.
- **Superficie per rotta, esplicita:** `f.rottaConGuscio` dà il guscio, `f.rotta` no. Le rotte
  aperte di login e reset sono «ingresso»: manifest, nessuna tavolozza, nessun menu. Tutto il resto
  è pubblico.
- Scenari del banco: `guscio-titolare.json`, `guscio-dipendente.json`, `pwa-guscio.json`.
- **Correzione di un test della 014:** `TestServeProcessoNonScaricabile` era instabile sotto
  carico. Mandava SIGTERM dopo la riga delle migrazioni, mentre `serve` salvava ancora la chiave
  del server. Ora `serve` scrive «in ascolto: …» quando le due facce sono su, e il test aspetta
  quella riga.

## Prove

- **TDD:**
  - 18 test rossi con le sole firme;
  - dopo la review, rossi l'elenco delle rotte con guscio, la rotta autenticata senza guscio e i
    percorsi non canonici degli asset;
  - i test aggiunti per le mutazioni sono verdi da subito e provati con le mutazioni;
  - il test instabile era rosso con l'attesa nuova, prima della correzione.
- **Misurato:**
  - in Chrome senza testa, su una copia statica: tavolozza salvata, selettore, tocco fuori ed Esc;
  - il test instabile, corretto: 20 volte e poi 15 sotto carico, verde.
- **Non provato:** installazione vera della PWA su HTTPS, telefoni veri, rimpicciolimento delle
  pillole allo scorrimento.
- **Mutazioni** (in una copia): **101 uccise su 112 che compilano**, poi le 9 lacune hanno avuto il
  loro test e sono state uccise rifacendole sul repo. Restano vive 2 equivalenti:
  - il ripiego di `/settings` senza schede non si raggiunge;
  - l'escape HTML del JSON oggi non cambia i byte; ora c'è un test con `&`, `<` e `>`.
- **Banco verde** con `fab-219`: 12 scenari, 137 passi. Spento, nessun avanzo.
  - In `registro.json` è tolta l'attesa «`audit.viewed` assente» alla prima apertura: ora
    `guscio-titolare` apre `/audit` prima. Il testo resta confrontato con lo standalone.
- `scripts/check` verde con l'esito: chiuse **ADM-22, ADM-01, ADM-08** (21 voci chiuse).

## Review indipendente

Nessuna ALTA. Verificato dal revisore:
- `sw.js`, icone e `settings-tabs.css` identici allo standalone;
- menu, schede, tavolozze e manifest come lo standalone;
- nessun `Set-Cookie` sugli asset;
- il worker non può mettere in cache pagine o dati;
- tutto questo solo sulla faccia interna;
- escape a posto.

Corretto:
- **MEDIA:** la superficie interna era dedotta dal permesso. Una pagina autenticata senza guscio
  (nello standalone l'anteprima-cliente) l'avrebbe ricevuto. Ora va chiesta in modo esplicito; c'è
  un test sull'elenco e uno su una rotta non marcata.
- **BASSA:** asset raggiungibili da percorsi non canonici (chiavi doppie nella cache del worker):
  ora solo dal percorso esatto.
- **BASSA:** test con la sessione aperta per asset e PWA; faccia pubblica anche per l'icona 512 e
  `/assets`; menu e schede legati ai permessi delle rotte; nessuna rotta nella regola di cache del
  worker; il commento sul `close_outside` portato solo per i menu.

## Differenze dallo standalone

- In `docs/domande-aperte.md` 7 e 8:
  - asset con l'hash nel nome, in cache del worker;
  - «Esci» come POST;
  - niente chiusura automatica dei messaggi verdi;
  - CSS a mano;
  - SIGTERM durante l'avvio.
- Temporanee, finché non arrivano le aree che le completano:
  - menu e schede solo per le pagine che esistono, `/settings` con il redirect;
  - niente logo, campanella, colori della barra, banner d'emergenza o pulsanti flottanti delle
    altre pagine;
  - manifest con il nome di ripiego.

## Ricevuta

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** questo, dopo `a593335` (014).
