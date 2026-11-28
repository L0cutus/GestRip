# 135 — Link storici sicuri dopo un'eliminazione (`numero_import`)

- **Fonte:** piano T4.11 approvato da Stefano il 27/11/2026 (`piano-T4.11-bozza.md`, riga 135 della tabella; domanda
  **F**; rischio «Vecchi link «0046/26»»). Decisione **D-120** (risposta F (a), slice 134–135); **D-77**
  (`buste.NormalizzaNumero`, riscrittura all'import). Chiude il rischio residuo del Dubbio 2 della 064.
- **Voci:** CLI-16…20 (rifinitura), ART-24 (vecchio link dell'artigiano). **Dipende da:** 134 (`buste.numero_import`).
  **Dipendenze nuove:** nessuna. **Migrazioni nuove:** nessuna (nessun indice: la colonna si legge dalla riga già
  trovata per `numero`, mai cercata).
- **Modelli:** implementatore Opus (~180k token, ~16 minuti di orologio). Revisore Opus indipendente (~110k token, ~7 minuti):
  nessuna ALTA, nessuna MEDIA, 2 BASSE, corrette (vedi «Revisione»).

## Il buco

Prima della 135 (`internal/buste/storica.go`, `internal/buste/artigiano_link.go`) la busta del vecchio link si
cercava con il numero com'è scritto e poi senza zeri (`cercaStorica`, `cercaPerSlug`), e la credenziale valeva sul
numero **scritto** in ogni caso:

- `/stato` (pagina, poll, messaggio, email, Telegram: tutti da `credenzialeValida`): firma `?t=&exp=` e codice
  `?pw=` sul numero scritto **o** sul salvato;
- `/foto` e `/thumb` (`LeggiFotoStorica`): firma solo sullo slug scritto, busta trovata anche senza zeri;
- `/artigiano/<busta>/<anno>` e le sue foto (`apriArtigianoStorica`): gettone Flask solo sul numero scritto, busta
  trovata anche senza zeri.

Quindi, eliminata la busta importata «0046/26» (salvata «46/26»), una busta nuova «46/26» di un altro cliente si
apriva con il vecchio link firmato su «0046/26» (verificato con i test nuovi sul codice di prima: falliscono tutti i
casi del 404).

## Cosa c'è

- **Regola** (`bustaStorica.numeroDellaBusta`): il numero scritto è della busta se è il numero salvato o
  **esattamente** il suo `numero_import` (confronto di testo, niente normalizzazione; NULL non è mai uguale).
  `bustaStoricaDove` legge anche `numero_import`.
- **`/stato`** (`credenzialeValida`): codice e firma sul numero salvato sempre (come lo standalone, quindi
  «/stato/046/26» della «46/26» con la firma di «46/26» si apre ancora); sul numero scritto solo se è il
  `numero_import`. Copre pagina, poll, messaggio, email e Telegram storici.
- **`/foto` e `/thumb`** (`slugDellaBusta`): la firma sullo slug vale solo se lo slug è quello del numero salvato o
  del `numero_import` (`/` → `_`, come `replace/3`); altrimenti nil (404 «Foto non disponibile») **senza** la riga
  `repair.public_photo_viewed`, come una firma sbagliata.
- **Slug delle foto della pagina `/stato`** (`PaginaStorica`): quello del numero scritto se è della busta (com'era:
  «0046_26» per la busta importata «0046/26»), altrimenti quello del numero salvato, come `legacy_photo_src/1`
  dello standalone. Senza, le foto della pagina «/stato/046/26» aperta con la firma del salvato avrebbero avuto lo
  slug «046_26», ora rifiutato.
- **`/artigiano/<busta>/<anno>`** e `/artigiano/<busta>/<anno>/foto/<file>` (`apriArtigianoStorica`): gettone Flask
  sul numero scritto (com'era) e solo se il numero scritto è della busta.
- **Non cambia:** la ricerca (prima com'è, poi senza zeri), il cestino che ferma la ricerca (404 per tutte le rotte),
  la revoca (410 solo con la credenziale giusta, ora valutata con la regola nuova), i link nuovi per id
  (`/artigiano/<id>?t=g1.…` della chiave del server, D-118, e il Phoenix.Token dello standalone, D-87: non guardano
  il numero), `/r/<token>`.
- **Oreficerie:** il numero è unico su tutta la tabella (`buste.numero UNIQUE`), oreficeria o no: nessun caso a
  parte. Le buste B2B seguono la stessa regola prima delle loro restrizioni (niente conversazione né foto cliente).

## Prove

`CGO_ENABLED=0 $HOME/sdk/go1.27.3/bin/go test -tags nodynamic` su `internal/buste`, `internal/web`, `tools/banco`:
verdi.

- Nuovo `internal/buste/storica_eliminata_test.go`, con la busta «46/26» in quattro varianti di `numero_import`:
  NULL (nata nel Go: l'«altra» busta dopo l'eliminazione), «46/26» (importata già così), «046/26» (un altro numero
  con gli zeri) e «0046/26» (la busta giusta). L'eliminazione definitiva non c'è ancora (139): è simulata così.
  - `TestLinkStoricoStatoDopoEliminazione`: pagina, poll e messaggio con firma e codice di «0046/26» → 404 tranne
    per «0046/26»; firma del salvato su «46/26» e su «0046/26» → si apre sempre; firma di «046/26» solo per la
    «046/26»; firma di «0046/26» e codice scritti «46/26» → 404. Poi revocata: 410 solo con una credenziale che
    la apriva (il vecchio link solo per la «0046/26»), ogni altra 404 (dalla revisione).
  - `TestLinkStoricoFotoDopoEliminazione`: `/foto` e miniatura con slug «0046_26» → foto solo per «0046/26», e la
    riga d'audit solo allora; slug «46_26» sempre; la pagina «/stato/046/26» firma le foto sullo slug del salvato (o
    «046_26» per la busta importata così) e quelle foto si aprono; la pagina del vecchio link giusto firma ancora su
    «0046_26».
  - `TestLinkStoricoArtigianoDopoEliminazione`: pagina e foto (intera e miniatura) del vecchio gettone di «0046/26»
    → solo per «0046/26»; gettone di «46/26» sempre; gettone `g1.` per id sempre.
  - `TestLinkStoricoStatoNelCestino`, `TestLinkStoricoFotoNelCestino`, `TestLinkStoricoArtigianoNelCestino`: la
    busta giusta nel cestino resta un 404 con tutti e due i numeri, come prima.
- Nuovo `internal/web/storica_eliminata_test.go`: le rotte HTTP vere `/stato` (firma, codice, poll), `/foto`,
  `/thumb`, `/artigiano/<busta>/<anno>` e la sua foto: 404 con `numero_import` NULL, 200 con «0046/26»; il numero
  salvato 200 in tutti e due i casi.
- **Fixture ritoccate** (simulavano la D-77 senza la colonna della 134): `storica` (`internal/buste/storica_test.go`:
  `numero_import` = il numero su cui è impresso il codice), `TestPaginaStoricaFirma`, `TestPaginaStoricaContenuto`,
  `TestLeggiFotoStorica`, `TestPaginaArtigianoStorica` (`artigiano_link_test.go`) e `storicaWeb`
  (`internal/web/pagina_storica_test.go`): `numero_import` «0046/26». Nessuna attesa cambiata.

`PATH=$HOME/sdk/go1.27.3/bin:$PATH scripts/check`: **verde** (voci 353: chiuse 57, rosse 0, parziali 192, aperte 93,
fuori v1 11; segreti e dati veri OK; senza `GESTRIP_BANCO_ESITO`).

## Mutazioni

Fatte dall'implementatore con uno script (riga cambiata, test mirati di `internal/buste` e `internal/web`, file
rimesso e confrontato con la copia): **13 su 13 uccise**. Script e log in `to-be-deleted-after-use/_slice-135/`.

| # | Mutazione | Uccisa da (fra gli altri) |
|---|---|---|
| M1 | credenziale sul numero scritto sempre (com'era) | `TestLinkStoricoStatoDopoEliminazione`, `TestLinkStoriciDopoEliminazioneHTTP` |
| M2 | `numero_import` confrontato senza zeri | i tre `…DopoEliminazione` di `buste` |
| M3 | `numeroDellaBusta` ignora `numero_import` | `TestPaginaStoricaFirma`, `TestPaginaArtigianoStorica`, i nuovi |
| M4 | slug delle foto non controllato | `TestLinkStoricoFotoDopoEliminazione`, `TestLinkStoriciDopoEliminazioneHTTP` |
| M5 | `slugDellaBusta` ignora `numero_import` | `TestLeggiFotoStorica`, `TestPaginaStoricaHTTP`, i nuovi |
| M6 | slug delle foto della pagina sempre dal numero scritto (com'era) | `TestLinkStoricoFotoDopoEliminazione` |
| M7 | slug delle foto della pagina sempre dal numero salvato | `TestPaginaStoricaContenuto`, `TestPaginaStoricaHTTP` |
| M8 | artigiano senza il controllo del numero (com'era) | `TestLinkStoricoArtigianoDopoEliminazione`, `TestLinkArtigianoStoricoDopoEliminazioneHTTP` |
| M9 | credenziale senza il numero salvato | `TestPaginaStoricaNumeroSalvato`, `TestEmailDallaPaginaStorica` |
| M10 | `numero_import` letto dalla colonna `numero` | `TestPaginaStoricaCodiceSulNumeroScritto`, i nuovi |
| M11 | `\|\|` → `&&` in `numeroDellaBusta` | `TestPaginaStoricaFirma`, i nuovi |
| M12 | `\|\|` → `&&` in `slugDellaBusta` | `TestFotoStoricaSlugNelCestino`, `TestFotoStoricheELimite` |
| M13 | NULL trattato come «qualunque numero» | tutti e cinque i nuovi |

## Banco

**Rigiro da fare** (lo lancia il capo-slice). Scenari che toccano le rotte cambiate: `pagina-storica`,
`pagina-storica-rifiuti` (`/stato`, `/foto`, `/thumb`), `pagina-artigiano` (`/artigiano/<busta>/<anno>` e foto),
`email-cliente` e `telegram-cliente` (passano da `/stato/4/26?pw=`). Tutti usano numeri salvati o la busta «4/26»
importata già così (`numero_import` «4/26»): attese invariate, **tranne un passo**:

- `pagina-storica`, ultimo passo `GET /stato/04/26?t=<firma di «04/26»>`: era `solo_go` «D-77» con atteso 200 (la
  firma su un numero con gli zeri che la busta non ha mai avuto: proprio il buco). Ora è un passo confrontato
  (`confronta: codice`) con atteso **404** per tutte e due le app (lo standalone verifica solo sul numero salvato).

Esito: **tre giri, tutti e tre rossi solo nel browser per la nota 8**. Configurazione: `gestrip-standalone:fab-225`,
`sintetici-import`, «memoria libera» del coordinatore; guardia sotto 10 GB mai scattata, minimo 18,2 GB; `banco giu`
senza avanzi dopo ogni giro. In tutti e tre i giri l'HTTP è verde: 91 scenari, 1373 passi, 0 inattese, 0 errori,
«BANCO VERDE».

- **s135a:** browser 39 scenari, 78 viste, **2 inattese**, 0 errori, tutte e due in `sfondo[0]`. Lo standalone
  risponde 500 a `GET /repairs/1/photos/1` dopo 5123 ms con `Exqlite.Error Database busy` sull'INSERT in
  `audit_events` (nota 8).
- **s135b:** browser 39 scenari, 78 viste, **6 inattese, 1 errore**, tutte dalla nota 8:
  - `sessioni-rientro` desktop: lo standalone risponde 500 a `GET /repairs/56` dopo 5018 ms (`Database busy`) e poi
    va in timeout nella fase «fine»;
  - per conseguenza, sul telefono `anagrafica-artigiani[1]` legge «2 riparazioni associate» invece di 1: la busta
    di `sessioni-rientro` resta a «Incastonatore Sintetico» nello standalone, come prevede il commento dello scenario
    in `banco.mjs`.
- **s135c (terzo giro, ultimo):** browser 39 scenari, 78 viste, **4 inattese, 1 errore**, tutte in
  `storico-correzioni[1]` (telefono). Lo standalone risponde 500 a `GET /repairs/88` dopo 5018 ms (`Database busy`) e
  poi va in timeout nella fase «fine». È la stessa firma della s133b.
- **Link storici verdi in tutti e tre i giri:** `pagina-storica`, `pagina-storica-rifiuti`, `pagina-artigiano`,
  `email-cliente` e `telegram-cliente` sono `true` in tutti e tre gli esiti. Il passo cambiato (ultimo di
  `pagina-storica`, `GET /stato/04/26?t=…` confrontato con atteso 404 per le due app) è verde.
- **`scripts/check` con `GESTRIP_BANCO_ESITO` (esito s135c, il giro finale):** voci 353, chiuse 230, **rosse 0**,
  parziali 19 (aperte 93, fuori v1 11). Con l'esito s135b le chiuse erano 227 e le parziali 22. Rispetto alla s133c
  (233 chiuse) mancano 3 chiuse: con ogni probabilità sono le voci di `storico-correzioni`, rimasto rosso per la nota 8
  (non l'ho verificato voce per voce).
- Nessuna PNG del manuale toccata.

**Decisione:** il 28/11 Stefano ha deciso di fare il terzo giro e di pubblicare la slice anche con il browser rosso
per la nota 8 (decisione riferita dal coordinatore).

## Per il manuale

Una riga in `docs/manuale/da-scrivere.md` per il capitolo 12 (146): il vecchio link di una busta eliminata non apre
un'altra busta con lo stesso numero senza zeri; vale anche per le foto e per i vecchi link degli artigiani.

## Differenze dallo standalone

Lo standalone non riscrive i numeri e verifica solo sul numero salvato. Il Go accetta in più il numero scritto solo
quando è il `numero_import` (la busta riscritta dalla D-77). Lo slug delle foto della pagina `/stato` è ora quello
dello standalone (numero salvato) tranne che per la busta riscritta. Restano le differenze già dette nella 064
(ricerca senza zeri dello slug, riga `denied` con la firma giusta su una foto non visibile).

## LSP

3 chiamate, utili: `workspaceSymbol` «bustaStoricaDove» (riga 117) e `findReferences` su `bustaStoricaDove` (6: tutte
le ricerche dei link storici passano di lì, quindi basta leggere lì `numero_import`) e su `cercaStorica` (9: pagina,
poll, messaggio, email ×2, Telegram ×2, artigiano: conferma che email e Telegram storici passano da
`autorizzata`). Il resto (firme `firmeflask.Verifica*`, scenari del banco, fixture) con grep.

## Revisione (Opus indipendente, ~110k token, ~7 minuti; 15 mutazioni, 14 uccise; 0 ALTA, 0 MEDIA, 2 BASSE)

Mutazioni del revisore: 14 uccise, M14 equivalente. Le due BASSE, corrette dall'implementatore con test in
`TestLinkStoricoStatoDopoEliminazione`:

1. **Revoca con `numero_import` non provata in `buste`** (la M15 del revisore, credenziale della revoca controllata
   sul solo numero salvato, la prendeva solo `TestStoricaRevocataHTTP` in `web`). **Corretta:** per ogni variante,
   busta revocata: il vecchio link giusto («0046/26», firma e codice) dà il 410, un link estraneo il 404 e non il
   410. Rifatta la M15 sulla correzione: **uccisa** (tre casi del 410 diventano 404).
2. **Codice `?pw=` provato solo con «0046/26» scritto.** **Corretta:** il codice impresso su «0046/26» scritto
   «46/26» dà il 404 (vale solo sul salvato, e lì l'impronta non torna).

Nota (non un difetto): i database importati prima della 134 hanno `numero_import` NULL (vedi «Dubbi»); va nel
promemoria del cutover (lo segna il coordinatore).

**Passo del banco approvato:** il revisore ha verificato nello standalone che `legacy_credential_valid?` verifica
solo su `bag_number`, quindi `/stato/04/26` con la firma di «04/26» dà il 404 anche lì.

## Dubbi

- **Database importati prima della 134** (o backup di prima ripristinati): `numero_import` NULL anche sulle buste
  riscritte dalla D-77, quindi i loro vecchi link con gli zeri («0046/26») **non si aprono più** (404); quelli con il
  numero salvato sì. È la nota della 134 («NULL = nessun numero di prima»). Va bene se l'import vero si fa con un
  programma che ha la 134.
- **Numero cambiato nel Go dopo l'import:** se un giorno il numero di una busta importata si potesse modificare, il
  suo `numero_import` resterebbe quello vecchio; la ricerca però trova la busta solo per `numero`, quindi il vecchio
  link non la troverebbe comunque. Nessun caso oggi.
- **Passo del banco cambiato** (sopra): il piano diceva che gli scenari dei link storici non cambiano; questo passo
  `solo_go` provava esattamente il comportamento che la D-120 toglie.
