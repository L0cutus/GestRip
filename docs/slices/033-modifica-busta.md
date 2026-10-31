# 033 — Modifica della busta (T4.1)

- **Fonte:** T4.1 del PIANO; piano delle slice nella ricevuta della 030.
- **Voci:**
  - chiuse: RIP-S-05, 06, 08, 09, 12;
  - parziali, per parti che nello standalone sono JavaScript (test saltato
    `TestModificaConJavaScript`): RIP-S-04 («Salva modifiche» spento nell'aspetto, errori
    mentre si scrive, ✏️ flottante), 07 (telefono che si blocca appena si sceglie l'oreficeria),
    10 («📷 Scansiona»), 11 (annuncio dello stato riallineato prima di salvare);
  - RIP-H-37 resta parziale per il segno nella lista (T4.2, `TestPagineConsegnaNellaLista`).
- **Lavoro:** implementazione e correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale.

## Cosa c'è

- **Dominio** (`internal/buste/modifica.go`): la modifica come `Workshop.update_repair` dello
  standalone, in una transazione:
  - solo i campi di `@edit_fields`: stato, numero, date di pronta e consegna e centesimi non si
    forzano;
  - validazioni e testi dello standalone;
  - prezzi a testo libero;
  - consegna come data o testo, e «Togli data»;
  - nome e telefono corretti solo sulla copia della busta, con il telefono bloccato sulle buste
    conto terzi anche lato server;
  - cambio di oreficeria con revoca del link e taglio della conversazione nella stessa
    transazione (audit `repair.public_link_revoked`, motivo `counterparty_changed`, mai il
    token); id 1 forzato e id inesistenti rifiutati;
  - artigiano, con l'id falso rifiutato;
  - QR senza unicità;
  - cambio di tipo compro oro con il riallineamento dello stato e i due rifiuti;
  - audit `repair.updated` e `repair.kind_changed` con i metadati dello standalone; se l'audit
    fallisce non cambia niente.
- **Nome e telefono come Ecto** (correzione della review): misurato `cast` più `update_change`
  con l'Ecto 3.14.2 dell'immagine:
  - lo stesso valore rimandato, spazi compresi, non è un cambio;
  - un valore diverso si ripulisce dagli spazi e cambia solo se è ancora diverso;
  - vuoto o di soli spazi vale nil: per il nome «can't be blank», per il telefono `""`.
- **Pagine** (`internal/web/modifica.go`):
  - `GET /repairs/{id}?modifica=1` apre il modulo;
  - `?edit=prezzo|preventivo` lo apre con il cursore (`autofocus`) nel campo giusto, ogni altro
    valore apre la scheda;
  - `POST /repairs/{id}/modifica` salva, con il guscio;
  - `POST /repairs/{id}/togli-data` toglie la data dalla scheda;
  - le rotte sono per il personale e stanno in `tabellaPermessi`;
  - «Annulla» è un link alla scheda;
  - un salvataggio riuscito porta alla scheda con «Riparazione aggiornata.»;
  - un rifiuto ridisegna il modulo con i valori scritti.
- **Banco:** passo `modifiche` della catena (`tools/banco/modifiche.go`), dopo l'emergenza. Con
  le funzioni dello standalone e i contesti delle pagine crea le buste 5–10 con:
  - prezzi a testo, numerici e in soli centesimi;
  - consegna a testo, a data e tolta;
  - note con a capo ed escaping;
  - giorni in più, artigiano, compro oro;
  - un'oreficeria senza telefono e un link revocato;
  - un cliente senza recapito;
  - QR tolto e QR doppio;
  - stati pronta, valutata e preventivo, con un preventivo accettato e uno rifiutato.

  L'esito `GR_MODIFICHE` è letto in modo stretto. Lo scenario `modifica` confronta 6 schede e il
  modulo su 8 buste. Il debito della 032 scende da 18 punti a 8: oreficeria riga 1 (la modifica
  la rifiuta), ora legale, nome del negozio, cestino, audit che fallisce, account disattivato o
  sessione revocata, righe d'audit, classi.

## Differenze di forma, dichiarate

- Il tipo mostrato dalla pagina viaggia nel campo nascosto `tipo_mostrato`: così il tipo cambia
  solo con un gesto, e una pagina vecchia non annulla un cambio fatto altrove.
- Il cursore è dato da `autofocus` invece di `phx-mounted JS.focus`; un modulo ridisegnato dopo
  un rifiuto non lo sposta.
- Una busta che sparisce (o va nel cestino) durante il salvataggio porta alla home con
  «Riparazione non trovata.»; lo standalone mostra il messaggio generico sul modulo. Capita solo
  con una corsa o una richiesta forgiata.
- Dopo un rifiuto con messaggio (artigiano, tipo, oreficeria, database) non si vedono gli errori
  dei campi, che lo standalone calcola mentre si scrive.
- Doppio invio: niente identificativo d'invio come nella 031, perché il secondo POST trova la
  busta già cambiata e non cambia niente (al più una riga `repair.updated` vuota); `app.js`
  spegne il pulsante al primo invio.
- Mancano «📷 Scansiona», il ✏️ flottante e «📲 Salva e avverti cliente» (area WhatsApp).
- Lo scenario del banco confronta solo GET: il salvataggio nello standalone è un evento
  LiveView sul websocket, come nella 031. Il salvataggio Go lo provano i test Go. La nota sta
  qui e in `docs/slices/004-banco.md`: lo schema degli scenari non ha un campo descrittivo.

## Prove

- **TDD:** rosso misurato prima del codice: 22 test del dominio e 17 delle pagine sugli stub. I
  test del banco sono stati scritti dopo il codice e il loro rosso misurato su una copia con le
  funzioni sostituite da stub (5 test e 4 sottocasi). Nel giro delle correzioni solo la M1 era
  rossa sul codice di prima; i test dei buchi di copertura sono stati provati con 8 mutazioni.
- **Mutazioni:** review 60, tutte compilano, 50 uccise; delle 10 vive, 7 erano buchi nei test e 1
  (errore dell'audit `kind_changed` ignorato) un guasto non provato: tutte e 8 coperte da test
  nuovi e uccise nel giro delle correzioni (8 mutazioni, tutte uccise). Restano 2 vive
  equivalenti: errori dei campi non inviati, e il motivo della revoca nello script Elixir, che
  vede solo il banco vero.
- **Banco** (`fab-219`), rilanciato dalla sessione principale dopo le correzioni: verde con
  `-dati sintetici-import` e su database vuoto, 16 scenari e 209 passi, «AVANZI: nessuno».
  L'import Go è riconciliato con gli stessi 9 avvisi di prima. Prova negativa (centesimi del
  modulo scritti con la virgola): 2 differenze, banco rosso.
- `scripts/check` verde; con l'esito del banco 43 voci chiuse, 15 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA.
- **MEDIA:** nome e telefono salvati con spazi venivano riscritti a ogni salvataggio, con
  `customer_fields` nell'audit → corretto come Ecto, misurato.
- **BASSA:**
  - «Togli data» su una consegna solo a testo, `aggiornato_il`, telefono bloccato nel modulo
    ridisegnato, oreficeria 1 forzata, prezzo vuoto senza `value`, «Togli data» su busta non
    trovata, casella compro oro con un valore strano → test aggiunti, mutazioni uccise;
  - lo scenario del banco fatto di sole GET → dichiarato sopra.

## Domande aperte nuove

In `docs/domande-aperte.md`, domande 1–4: righe `repair.viewed` in più, «Salva modifiche» spento,
annuncio del riallineamento, telefono che si blocca mentre si sceglie. Nessuna blocca il lavoro.
