# 035 — Riquadro del link pubblico sulla scheda (T4.1, ultima slice)

- **Fonte:** T4.1 del PIANO; piano delle slice nella ricevuta della 030.
- **Voci:** chiuse RIP-S-24, CLI-23, CLI-24, CLI-25. Non segnate di proposito: CLI-22 (link
  nei messaggi, area messaggi), CLI-26 (pagina pubblica), CLI-18 (rifiuto 410 della pagina
  pubblica; qui c'è solo la colonna).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/link.go`):
  - stato del link come `public_link_state` e `legacy_link_status`: attivo, solo impronta,
    scaduto (anche alla scadenza esatta), revocato, mancante. La scheda legge lo stato, mai il
    token;
  - `RevocaLink` e `NuovoLink` come `revoke_public_access` e `regenerate_public_link`, in una
    transazione:
    - il vecchio link con codice si revoca per sempre, e il primo istante resta;
    - audit `{}` o `{"legacy":true}`, `{"reason":"manual"}` per il nuovo link;
    - le iscrizioni Telegram si spengono con `repair.telegram_stop` (un motivo NULL resta NULL),
      i token t.me si tolgono e i messaggi in coda falliscono;
    - l'indirizzo email si toglie con `repair.email_optout` e le email in coda falliscono;
    - la conversazione pubblica non cambia.

    Il nuovo gettone segue le regole della 030.
- **Pagine** (`internal/web/link.go`):
  - `POST /repairs/{id}/revoca-link` e `POST /repairs/{id}/nuovo-link`, solo per il titolare.
    Il dipendente torna alla scheda con «Solo il titolare può …», il messaggio dello standalone;
  - regola nuova `f.azioneDelTitolare` per le azioni del titolare dentro una pagina comune
    (`docs/SVILUPPO.md`, «Rotte»): solo POST, permesso `soloTitolare`, catena completa;
  - il nuovo link si mostra una volta, come nella 031: `/repairs/<id>?esito=<id casuale>`, voce in
    RAM legata a utente, sessione e busta, `no-store`. Lo standalone non rimostra mai il token
    salvato aprendo la scheda, quindi nemmeno il Go;
  - alla revoca e al nuovo link gli esiti non ancora aperti di quella busta e di quella sessione
    si tolgono, come `clear_one_time_link/3`.
- **Banco:**
  - passo `link` della catena: nuovo link sulla 1/26, revoca sulla 3/26 (con il vecchio link con
    codice), 16/26 revocata, 17/26 dell'oreficeria rinnovata, 18/26 revocata e poi rinnovata;
    esito `GR_LINK` letto in modo stretto;
  - scenario `link`: 10 schede dal titolare, 3 dal dipendente, 1 senza accesso, con attese
    esplicite. Nessun token nei log né negli esiti del banco, controllato con i token letti dal
    database.

## Differenze di forma, dichiarate

- POST con redirect invece degli eventi LiveView: revoca e nuovo link nel Go non si confrontano
  via HTTP, li provano i test Go.
- Una busta che non c'è o nel cestino porta alla home con «Riparazione non trovata.»; lo
  standalone dice «Link … non revocato.» sulla pagina.
- Nel messaggio di un guasto, «del cliente» o «dell'oreficeria» viene dalla busta riletta, non
  dalla pagina.
- Il dipendente rifiutato torna alla scheda con un redirect; lo standalone resta sulla pagina.
- Il link nuovo sta in `?esito=`, in RAM per 5 minuti: con un riavvio si perde (si rigenera).
- Nello standalone il link mostrato una volta lo toglie qualunque evento della scheda. Nel Go lo
  tolgono revoca e nuovo link; dopo una modifica, un cambio di stato o un'attesa si vedrebbe solo
  riaprendo lo stesso indirizzo `?esito=` entro 5 minuti.
- Senza `indirizzo_pubblico` il link si genera come nella 031, e la scheda dice cosa configurare
  (domanda aperta 8).
- `grezzo:` della riga dello stato differisce per gli spazi di HEEx; il testo visibile è uguale.
- Il banco non copre il link con la sola impronta ancora valido, «· inviato il», i tagli di
  Telegram ed email: nessuna funzione delle pagine li produce su questi dati. Li provano i test Go.

## Prove

- **TDD:** rosso misurato prima del codice: 8 test del dominio, 10 delle pagine; i 4 del banco su
  una copia con gli stub. Nel giro delle correzioni rosso misurato sul codice di prima per gli
  esiti non consumati; per gli altri tre punti, dove il codice era già giusto, sulle mutazioni.
- **Un test** controlla che il token non finisca in audit, registro slog a livello debug,
  Location, intestazioni, cookie, risposta della revoca, né negli errori (generatore del gettone
  iniettabile nei test, quattro guasti fino al Commit).
- **Mutazioni:** review 60, ne compilano 59, uccise 53. Delle 6 vive, 3 sono equivalenti (motivo
  riscritto dal secondo UPDATE, menu con l'esito, guardia del panic per le registrazioni di
  oggi); le altre e la guardia sono coperte nel giro delle correzioni, con 9 mutazioni tutte
  uccise.
- **Banco** (`fab-219`), rilanciato dalla sessione principale dopo le correzioni: verde con
  `-dati sintetici-import` e su database vuoto, 18 scenari e 252 passi, «AVANZI: nessuno».
  Prova negativa (menu mostrato anche al dipendente): 41 differenze, banco rosso.
- `scripts/check` verde; con l'esito del banco 48 voci chiuse, 16 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA; la regola `azioneDelTitolare` regge (7 mutazioni sulla catena, tutte uccise).
- **MEDIA:**
  - test «errori senza token» che controllava il token sbagliato → generatore iniettabile,
    mutazioni uccise;
  - differenze da dichiarare → sopra.
- **BASSA:** motivo Telegram NULL, panic di `azioneDelTitolare`, esito non consumato dopo una
  revoca → test e correzione; dipendente su una busta inesistente («Riparazione non trovata.»
  invece di «Solo il titolare…») → innocuo, lasciato.

## Domande aperte nuove

In `docs/domande-aperte.md`, domande 8 e 9: nuovo link senza `indirizzo_pubblico`, righe
`repair.viewed` dopo revoca e nuovo link.
