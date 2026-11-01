# 037d — Allineamento alla slice 220 dello standalone

- **Fonte:** slice 220 dello standalone (`12d6e7f`, «errori italiano e numero di busta»), che
  corregge i due difetti trovati dalla slice 030 del Go (domande 1–2 delle slice 030 e 032). Il tag
  `go-baseline` è stato spostato a `12d6e7f` il 01/11/2026 (prima `6eb93ed`, slice 219),
  autorizzato da Stefano. Letti `git show 12d6e7f` e la ricevuta
  `docs/plans/2026-10-27-gestrip-debiti-parita/slices/220-errori-italiano-numero-busta.md`.
- **Voci:** RIP-H-30 (testi dei campi), RIP-H-31 (numero senza zeri, «0/26» rifiutato, contatore),
  RIP-S-04 e RIP-S-07 (testi della modifica; giorni in più vuoti), ACC-20 («1 minuto»). Nessuna
  cambia stato: RIP-H-30/31 restano dove erano, RIP-S-04/07 restano parziali (livello browser).
  Da ricontrollare quando arrivano: RIP-H-03 (T4.2), CLI-16…19 (T4.5), ART-08 (T4.8), ORE-06 (T4.9),
  RIP-H-50 (T4.10).
- **Lavoro:** implementazione, banco, mutazioni di un sottoagente; review e mutazioni di un
  sottoagente diverso; inventario, test dei «0 minuti», `scripts/check` e banco (`fab-220`, verde
  con `-dati sintetici-import` e su database vuoto, 22 scenari e 314 passi, «AVANZI: nessuno»)
  della sessione principale.

## Cosa c'è

### Testi degli errori in italiano

I messaggi di Ecto che il Go già mostra sono ora i testi del catalogo `it` della 220
(`priv/gettext/it/LC_MESSAGES/errors.po`), parola per parola:

| Costante | Prima (msgid) | Ora |
| --- | --- | --- |
| `buste.MsgObbligatorio` | can't be blank | Campo obbligatorio. |
| `buste.MsgNonValido` | is invalid | Valore non valido. |
| `buste.MsgNomeLungo` | should be at most 160 character(s) | Troppo lungo (massimo 160 caratteri). |
| `buste.MsgTelefonoLungo` | should be at most 40 character(s) | Troppo lungo (massimo 40 caratteri). |
| `buste.MsgCentesimi` | must be greater than or equal to 0 | Almeno 0. |

Cercati gli altri testi inglesi di Ecto o del codice nel Go (`internal/`, `cmd/`, pagine): nessuno.
Gli altri messaggi dei campi erano già scritti in italiano dallo standalone («formato richiesto
N/AA (es. 12/26)», «Numero busta già esistente.», «Giorni in più: da 0 a 90.», «Motivo
obbligatorio.» …) e non cambiano. Il dominio `default` della 220 («Something went wrong!»,
«Attempting to reconnect», «close», «Actions», «We can't find the internet») è del guscio LiveView
dello standalone: il Go non mostra nessuno di quei testi. Le pagine non ancora portate (vendita,
registro d'emergenza, impostazioni del negozio) useranno la regola nuova di `docs/SVILUPPO.md`
(«Testi degli errori dei campi»).

### Numero di busta senza zeri davanti

- `buste.NormalizzaNumero` è `Repair.normalize_bag_number/1`: toglie gli zeri davanti e tiene una
  cifra («012/26» → «12/26», «00/26» → «0/26»); tutto ciò che non è N/AA torna com'è. RE2 non ha lo
  sguardo avanti: la regex è `^0+(\d+/\d{2}\n?)$`, con `\n?` per il `$` di PCRE che vale anche prima
  di un a capo finale (caso provato, non raggiungibile: chi chiama toglie gli spazi).
- La creazione normalizza il numero scritto (dopo gli spazi) prima del contatore, del formato e
  dell'unicità; il contatore legge il numero normalizzato, quindi «000012/26» lo porta a 12; il
  modulo dopo un rifiuto rimostra il numero normalizzato («012/26» già esistente → «12/26»;
  «00/26» → «0/26»).
- Formato della busta nuova `^([1-9]\d{0,4})/(\d{2})$` (`formatoNumeroScritto`): «0/26», «00/26»,
  «00000/26» rifiutati con il messaggio del formato. Il contatore tiene il formato di prima.
- La modifica non tocca il numero: una busta importata con zeri si modifica e tiene il suo numero
  (test, come nella 220).
- L'import storico non cambia: conserva il numero della sorgente.
- **Parti non ancora portate** (scritte come regola in `docs/SVILUPPO.md`, «Numero di busta»):
  - T4.2, RIP-H-03 (ricerca della home): `RepairIndex.search/2` normalizza **tutto il testo
    cercato** prima dell'escape del `LIKE` («012/26» trova «12/26»; un testo che non è N/AA non
    cambia);
  - T4.5, CLI-16…19 (`/stato/<busta>/<anno>`, anche poll e messaggio): la busta si cerca con il
    numero normalizzato (`get_legacy_public_repair/1`); il rifiuto resta unico (stesso 404 con e
    senza zeri, con codice sbagliato o busta che non c'è);
  - T4.8, ART-08 (numero scritto alla consegna all'artigiano, `Workshop.find_bag/3`) e T4.9, ORE-06
    (numero scritto nel lotto, `ReadyLots.find_bag/4`): numero normalizzato; il QR no;
  - T4.10, RIP-H-50 (esclusione dell'assistente, `Training.exclude/2` e `exclude_customer/2`):
    `String.trim` e poi normalizzato;
  - **non** si normalizzano `/artigiano/<busta>/<anno>` e le foto storiche `/foto`, `/thumb`: link
    firmati dal server sul numero com'è scritto.
  - Limite dichiarato dalla 220: un numero con zeri davanti scritto da un import futuro non si trova
    più col numero digitato né col link `/stato`.

### «1 minuto»

`msgTroppi` (ACC-20): l'ultimo minuto di un blocco è «Troppi tentativi, riprova fra 1 minuto.», gli
altri restano «N minuti» (anche 0, come lo standalone).

### Giorni in più vuoti (trovato dal banco nuovo)

Il confronto del banco sulla modifica ha trovato una differenza **del Go**, che c'era già dalla 033
e non dipende dalla 220: con «Giorni in più» vuoto il Go diceva «Campo obbligatorio.», lo
standalone salva 0. Misurato sull'immagine `fab-220` (sonda in `to-be-deleted-after-use/slice-037d/`):
`Repair.update_changeset(%Repair{extra_days: 3}, %{"extra_days" => ""})` dà `changes: %{extra_days:
0}` e nessun errore, e così con soli spazi; con 0 salvato, nessun cambio. È il cast di Ecto: un valore
vuoto prende il default dello schema (`field(:extra_days, :integer, default: 0)`). Il Go ora fa lo
stesso (vuoto vale «0»). Nessun altro campo della modifica ha un default diverso da nil, salvo
`customer_phone_snapshot` (default ""), che il Go già tratta come stringa vuota. **Da confermare
dalla sessione principale** (non è una decisione di prodotto: è il Go che torna a copiare lo
standalone; l'inventario RIP-S-04 dice «Obbligatori: … giorni in più», da correggere).

## Banco

- **Immagine:** `gestrip-standalone:fab-220`, id `sha256:427a38b1adc6…`, creata il 01/11/2026 alle
  00:26 dalla sessione dello standalone (4 minuti dopo il commit `12d6e7f`, prima della 221). Non
  rifatta: verificato che contiene la 220 (nel rilascio `priv/gettext/it/LC_MESSAGES/errors.po`
  uguale byte per byte a `git show 12d6e7f:…` — md5 `1f3960…`; `normalize_bag_number` in
  `Repair`; `minutes` in `SessionController`; niente di questo in `fab-219`). Riferimenti aggiornati
  in `tools/banco` (uso ed esempio di `-immagine`), `docs/PIANO.md` e `docs/prova-dati-veri.md`;
  le ricevute storiche no.
- **Passo `numeri` e scenario `numeri`:** descritti in `docs/slices/004-banco.md`. Il passo fa nello
  standalone, con le funzioni della nuova busta e della modifica e gli errori letti con
  `translate_error/1`, nove casi (due buste salvate e otto rifiuti); poi gli stessi nel dominio Go
  (linkato nel banco, database suo cancellato dopo) e pretende lo stesso esito campo per campo. Lo
  scenario confronta via HTTP le schede delle buste 19 e 20 (numero senza zeri). «1 minuto» non si
  raggiunge dal banco (serve l'ultimo minuto di un blocco): lo provano i test Go.
- **Esiti** (`to-be-deleted-after-use/slice-037d/banco.sh`, binari dal codice finale):
  - `-dati sintetici-import`: **BANCO VERDE**, 22 scenari, 314 passi, 0 differenze; catena con la
    220: import dello standalone e del Go riconciliati, 20 buste;
  - database vuoto: **BANCO VERDE**, 22 scenari, 314 passi;
  - `banco giu` e `banco controlla`: «AVANZI: nessuno».
- **Prove negative:**
  - immagine `fab-219` con `-dati sintetici-import`: la catena si ferma al passo `numeri`
    (« 0019/26 » salvato come busta nuova), `su` esce con 1;
  - banco con il dominio Go mutato (`MsgNonValido = "is invalid"`) e `fab-220`: `su` esce con 1,
    «rifiuti diversi: data: standalone [Valore non valido.], Go [is invalid]; modifica_non_validi: …».

## Prove

- **Rosso misurato** (`to-be-deleted-after-use/slice-037d/prove/`): con il codice di prima e una
  `NormalizzaNumero` finta, 11 test rossi in `internal/buste` (TestNormalizzaNumero, i quattro dei
  numeri con zeri, la modifica di una busta con zeri, i testi del catalogo, e i quattro esistenti
  aggiornati ai testi italiani) e 6 in `internal/web` (due di «1 minuto», tre degli errori sui moduli
  e TestPaginaAvvisoQR, che legge il testo); `TestNuovaNumeroConZeri` rosso su 9 righe;
  `tools/banco`: `TestNumeriNelGoComeLoStandalone`, `TestSuConLaCatena` e 17 casi di
  `TestSuLaCatenaSiFerma` rossi. Giorni in più vuoti: `TestModificaGiorniInPiu` e
  `TestNumeriNelGoComeLoStandalone` rossi prima della correzione.
- **Test nuovi:** `internal/buste/zeri_test.go` (7 test), `TestNuovaNumeroConZeri`
  (`internal/web/nuova_test.go`), `tools/banco/numeri_test.go` (5 test),
  `TestScenarioNumeriSuiDueDati`. Aggiornati ai testi italiani i test che leggevano quelli inglesi.
- **Mutazioni:** 24 sul codice nuovo, 24 compilano, **24 uccise** (`mutazioni/esiti.txt`):
  normalizzazione (4), formato della busta nuova (3), contatore (1), «1 minuto» (2), giorni vuoti
  (2), testi (3), confronto e lettura del banco (6), catena (3). Nessuna sopravvissuta.
- `scripts/check` verde (copertura: 353 voci, chiuse 24, rosse 0, parziali 46, aperte 272, fuori
  v1 11).

## Review indipendente

Nessuna ALTA né MEDIA. Confrontate riga per riga con la 220: regola del numero (regex Elixir contro
RE2: `\d` solo ASCII, `$` prima di un a capo finale, «00/26», numeri lunghi, anno), ordine dei
passi della creazione e messaggio che vince, numero rimostrato, contatore, testi identici al
catalogo, giorni in più vuoti (schema `default: 0`, Ecto 3.14.2 in `mix.lock`), banco che non può
essere verde a vuoto.
- **BASSA:** mancava il test dei «0 minuti» (mutazione `minuti <= 1` viva) → `TestMsgTroppiMinuti`,
  verde sul codice e rosso sulla mutazione, aggiunto dalla sessione principale.
- **BASSA:** inventario da aggiornare (D-43, RIP-H-31, RIP-S-04) → fatto dalla sessione principale,
  con le note della 220 e il registro di allineamento.
- **BASSA:** il banco prova «Giorni in più» vuoto ma non di soli spazi: coperto dal test Go.
- **Mutazioni della review:** 40 scritte, 39 compilano, 37 uccise; vive F5 (equivalente: cambia solo
  «0/26», che la creazione rifiuta e la transazione annulla) e L1 (ora uccisa).

## Dubbi

- Giorni in più vuoti: correzione del Go verso lo standalone, fuori dalla 220 (sopra). Tenuta dalla
  sessione principale: copiare lo standalone misurato; inventario RIP-S-04 corretto.
- `fab-220` non ha label di revisione (debito della 004): la verifica è sul contenuto.
