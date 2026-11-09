# 061 — Pagina del cliente `/r/<token>` e link stabile (T4.5, prima slice)

- **Fonte:** coordinatore, 09/11/2026; T4.5 del PIANO (🔴: link già mandati ai clienti). Lo standalone
  a `go-baseline` = `302439d`.
- **Voci:** CLI-01 (cosa mostra), CLI-02 (rifiuto uniforme), CLI-03 (stima dei tempi), CLI-21 (limite
  ai tentativi sulla pagina vera), CLI-22 (riuso: nessuna pagina pubblica emette un link), CLI-26 e
  CLI-28 per la pagina. Già fatte nella 035 e non toccate: RIP-S-24, CLI-23, CLI-24, CLI-25, la revoca
  al cambio di controparte (`modifica.go`, `revocaPerControparte`).
- **Deviazioni:** D-64 (numero visibile sulla pagina), D-05 (404 di blocco identico al normale, già
  nel limite). Nessuna nuova.
- **Lavoro:** un sottoagente, con mutazioni a mano. La review la fa un altro sottoagente.

## Cosa c'era già (T3, 030, 035)

- Colonne `token_pubblico` (testo), `hash_token_pubblico` (BLOB SHA-256, UNIQUE),
  `token_pubblico_scade_il`, `token_pubblico_revocato_il`; l'import le porta tutte e due dallo standalone.
- Emissione alla nascita della busta (90 giorni), «Revoca link» e «Genera nuovo link» con audit e tagli
  di Telegram ed email, stato nella scheda, revoca al cambio di controparte, il limitatore
  `internal/ratelimit` già davanti alla faccia pubblica (famiglia `/r/`: 30 falliti al minuto).
- Mancava la pagina: la faccia pubblica aveva solo `/healthz`.

## Cosa c'è

- **Dominio** (`internal/buste/pagina_cliente.go`): `PaginaCliente(token)` è `get_public_repair/1` +
  `token_page/1`:
  - cerca per `ImprontaToken` = SHA-256 del testo arrivato (`hash_token/1`), scadenza **dopo** adesso,
    senza revoca, fuori dal cestino; ogni rifiuto è `ErrBustaNonTrovata`. Un token emesso dallo
    standalone e importato (solo impronta, o impronta e testo) funziona uguale;
  - restituisce solo quello che la pagina mostra (struttura `PaginaCliente`): mai nome o telefono del
    cliente, recapiti del negozio, note interne, token, numero interno;
  - numero visibile D-64 (`NumeroVisibile`), etichette dello stato di `status_label/1`, consegna di
    `delivery/1` (AAAA-MM-GG o il testo), preventivo com'è scritto, prezzo finale con `Money.label/1`;
  - **stima dei tempi (CLI-03) completa**: `RepairEstimate.for_customer/2` con interruttore
    `stima_tempi_attiva`, supplemento artigiano dalle impostazioni o misurato, giorni in più, campione
    di 12 mesi (0–90 giorni, almeno 20 buste di bottega, 10 dall'artigiano), percentili 25/75;
  - aprire la pagina **non scrive niente**, come `show/2` (nessuna riga d'audit).
- **Pagina** (`internal/web/pagina_cliente.go`, `pagine/pagina_cliente.html`, CSS in `app.css`):
  `GET /r/{token}` con `rottaPubblica` (intestazioni di sicurezza delle pagine), più `no-store` e
  `no-referrer`; rifiuto con `RifiutoPubblico` («Riparazione non disponibile», lo stesso per ogni
  motivo, contato dal limite). Gli id dello standalone (`public-shop-masthead`, `public-time-estimate`,
  `public-shop-contact`, `public-privacy-link`). La faccia pubblica serve anche `/assets/…` e l'icona
  da 192 (PIANO §1.5 li ammette sulle due facce).
- **Punti d'attacco segnati nel modello:** esiti (062–063), riquadri Telegram ed email (T4.6),
  preventivo e foto (062), conversazione (063). `PaginaCliente.ID` e `.Oreficeria` sono già lì per le
  foto «cliente»/«orefice» e i riquadri.
- **Banco:**
  - il Caddy del banco manda `/r/*` del Go alla faccia pubblica (47102), il resto all'interna;
  - passo nuovo della catena `token` (`tools/banco/token.go`), ultimo prima della copia: cinque buste
    ricevono un token **inventato** salvato come lo salva lo standalone (21/26, 23/26, 17/26 vivi; 3/26
    revocata e 2/26 scaduta con la sola impronta), controllato con `get_public_repair/1`. Il Go li
    riceve con l'import: è la prova che un link emesso dallo standalone continua a funzionare;
  - scenari `pagina-cliente` (3 pagine, revocato, scaduto, inesistente) e `pagina-cliente-limite` (30
    falliti, poi il token buono bloccato, un altro indirizzo libero), con `X-Forwarded-For`;
  - 3 righe D-64 per il numero (`visibile:.font-mono@1`). Le classi `font-mono`, `space-y-7` e
    `whitespace-pre-wrap` nel modello sono solo appigli per il banco.

## Prove

- Test Go: token importato (solo impronta e con testo), rifiuto uniforme (inesistente, revocato,
  scaduto alla scadenza esatta, cestino, sostituito da un nuovo link, vuoto, maiuscole, spazio),
  cambio di controparte (il vecchio link muore, quello nuovo è dell'oreficeria; id 1 = cliente),
  cosa mostra e cosa no (anche sul corpo HTTP e sul registro), intestazioni, stesse risposte per ogni
  rifiuto, limite (40 buoni non contano, 30 falliti bloccano anche il buono, altro indirizzo libero,
  sblocco dopo 15 minuti), stima (interruttore, consegna, stati, artigiano, campione, minimo).
- Test aggiornati: asset e icona ora anche sulla faccia pubblica (`guscio_test`, `sicurezza_test`),
  i test con rotte `/r/` di prova le usano al posto della pagina vera, `ChiaviSoloGo` conosce il file
  che legge le chiavi `stima_tempi_*`; banco: Caddyfile, passo `token`, `prova_con`, `atteso_dati`.

## Mutazioni

15 a mano sulle regole principali (revoca, scadenza `>`, cestino, numero visibile, stima con consegna,
stati della stima, percentile, minimo del campione, supplemento, compro oro, preventivo senza «€»,
oreficeria id 1, `no-store`, 404, giorni negativi): 14 uccise subito; la viva (oreficeria id 1) uccisa
con un test in più.

## Banco

Istanza `s061`, `fab-225`, `-dati sintetici-import`. Primo giro: catena verde (`GR_TOKEN:ok`),
browser verde (0 inattese), HTTP 10 inattese, tutte degli scenari nuovi e corrette negli scenari:
- 4 sul conteggio del token nella pagina (vedi Dubbio 4): tolto dal confronto;
- 6 sul `content-type` dei tre 404: il Go non lo manda (misurato dentro la rete del banco, come lo
  standalone), ma dietro il Caddy del banco il 404 del Go arriva al client con `text/plain`, quello dello
  standalone no (probabilmente perché lo standalone comprime la risposta e Caddy non la «annusa»).
  Artefatto del banco: tolto dal confronto dei 404, resta nel test Go.

Secondo giro, su un'istanza nuova (il browser cambia i dati: `banco giu`, `su`, `esegui`): **BANCO
VERDE**, 50 scenari e 925 passi, 1511 differenze attese, **0 inattese**; **BANCO BROWSER VERDE** (12
scenari, 24 viste, 0 inattese). Istanza tolta con `banco giu`. Giro sul database vuoto non fatto
(nessuna migrazione).

## Controlli

`scripts/check` verde; `scripts/parole-vietate --staged` verde (dopo `git add -A`, poi `git reset`).

## Per il manuale

- Capitolo nuovo o del cliente: cosa vede il cliente aprendo il link (stato, date, lavorazione, nota
  del laboratorio, prezzi, «Tempo indicativo» solo con la stima accesa e senza data di consegna); un
  link revocato, scaduto o sostituito dice «Riparazione non disponibile»; dopo 30 tentativi sbagliati
  in un minuto lo stesso indirizzo è fermo 15 minuti. Messo in `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Date in AAAA-MM-GG** sulla pagina («Accettata», «Pronta dal», «Consegnata», «Consegna
   prevista»), come lo standalone. Con la D-80 i PDF sono passati a gg/mm/aaaa: la pagina del cliente
   resta così finché Stefano non decide.
2. **Il numero visibile «N. 45/26»** per una busta senza etichetta RIP compare anche al cliente (D-64);
   lo standalone mostra «45/26». Fatto come la D-64 dice («ovunque»), ma il «N.» davanti nel riquadro
   scuro va confermato.
3. **Link «Informativa privacy»** porta a `/privacy`, che arriva con la 064: fino ad allora dà 404.
4. **Riquadri email/Telegram:** sui dati del banco lo standalone li mostra già sulle buste aperte (il
   token compare 3 volte nei loro moduli, sulla 23/26 e sulla 17/26); nel Go arrivano con la T4.6. Il
   banco non confronta né i riquadri né quel conteggio; il test Go controlla che il token non compaia.
5. **Banner d'emergenza** del layout radice dello standalone (`emergency_banner`) non c'è sulla pagina
   del cliente del Go: da decidere se serve sulle pagine pubbliche.
6. **CLI-22 «emissione quando un messaggio lo chiede»** (`ensure_public_link/2`) va con i messaggi
   (T4.6); qui solo la regola che nessuna pagina pubblica emette un link.

## Review

Revisore: sottoagente, 09/11/2026, sul lavoro non committato; standalone a `go-baseline`. Nessun
giro del banco (si tiene quello della ricevuta). Nessuna correzione al codice.

- **Esito: nessun difetto ALTO né MEDIO.**
- **Token:** la ricerca è per impronta SHA-256 del testo arrivato (`hash_token/1`), confronto
  nell'indice di SQLite, nessun confronto in chiaro. Inesistente, revocato, scaduto (`>` come
  `get_public_repair/1`), cestino e sostituito fanno la stessa unica query e danno lo stesso
  `RifiutoPubblico`: stesso corpo, stesse intestazioni (anche `no-store`/`no-referrer`, come
  `public_headers/1` che lo standalone mette anche sul 404), uguale al 404 di blocco (`Pagina` e
  `RifiutoPubblico` mettono le stesse quattro intestazioni). Il test web lo controlla davvero.
- **Token dello standalone:** stesso formato (testo libero, impronta binaria), l'import copia
  `public_token_hash` com'è; il passo `token` del banco li crea con `:crypto.hash(:sha256, …)` e il Go
  li apre (scenario `pagina-cliente` verde).
- **Dati esposti:** il modello stampa solo i campi di `PaginaCliente`; niente token, id, nome o
  telefono; nel registro va il pattern della rotta, mai il percorso; l'errore 500 non ha dettagli.
- **Limitatore:** invariato (010/035); i buoni restituiscono il posto, i 404 contano, il test prova
  40 buoni, 30 falliti, blocco del buono e altro indirizzo libero.
- **Asset:** sulla faccia pubblica solo `/assets/{nome}` (mappa chiusa, percorso esatto) e l'icona
  192; niente manifest né worker. Come lo standalone, che serve gli asset ovunque.
- **Banco:** `tools/banco/token.go` scrive con `eval` nel container senza rete dell'istanza, solo
  sulle cinque buste sintetiche, con token `SINTETICO-…`, e ricontrolla stato e ricerca.
- BASSA: il test del token importato calcola l'impronta con lo stesso SHA-256 del codice; la prova
  incrociata vera è il banco. Un test a risposta nota (impronta esadecimale fissa) la renderebbe
  indipendente.
- BASSA: un token buono fa più query (stima) di uno sbagliato, quindi i tempi si distinguono solo fra
  buono e rifiutato, come nello standalone; fra i motivi di rifiuto no.
- `scripts/check` verde; `scripts/parole-vietate --staged` verde (dopo `git add -A`, poi `git reset`).

Dopo la review (09/11/2026): la BASSA 1 è stata chiusa nel commit con `TestPaginaClienteImprontaFissa`, che usa un'impronta esadecimale fissa calcolata con `sha256sum` e non lo SHA-256 del codice.
