# Slice 004 — Banco a scatola chiusa, scheletro (T0)

- **Livello:** completo (strumento che decide la parità; tocca Docker su pc-fisso).
- **Fonte:** PIANO §4; decisione 5 di Stefano (30/10/2026): istanza isolata e sintetica su
  pc-fisso, immagine di `go-baseline`, rete Docker interna, nessuna porta sull'host, prefisso
  proprio `grgo-`, spazio su disco controllato prima, mai la produzione. Riferimento di metodo:
  gli script della skill `gestrip-browser-gate` (pc-lab), letti e non modificati.
- **Confine:** `tools/banco` con questi comandi:
  - `su`, `esegui`, `giu` e `controlla`, sull'host;
  - `confronta` e `attendi`, dentro il container.

  In più `bench/scenari/healthz.json` e `bench/deviazioni.json` (vuoto), e gli scenari del banco
  riportati nella copertura. Solo HTTP. Il browser (Playwright) e i dati sintetici con la catena
  Flask → standalone → import arrivano in T3.

## Come si usa

Docker senza sudo, attraverso il gruppo `docker`:

    B=to-be-deleted-after-use/banco-bin
    CGO_ENABLED=0 go build -trimpath -tags nodynamic -o $B/gestrip ./cmd/gestrip
    CGO_ENABLED=0 go build -trimpath -tags nodynamic -o $B/banco ./tools/banco
    sg docker -c "$B/banco su -nome t0 -immagine gestrip-standalone:fab-213 -gestrip $B/gestrip"
    sg docker -c "$B/banco esegui -nome t0 -banco $B/banco"
    sg docker -c "$B/banco giu -nome t0"

Con `-dati sintetici-import` (slice 024) `banco su` prepara gli stessi dati dalle due parti con la
catena di PIANO §4.2 (cattura Flask sintetica → legacy import dello standalone → copia →
decifratura con il SQLCipher dell'immagine → `gestrip import -sintetico`) e fa partire il Go solo
con un import riconciliato. Senza l'opzione l'istanza nasce come prima, con i due account seminati.
Ricevuta in `docs/slices/024-catena-banco.md`.

Dalla slice 026, fra il legacy import e la copia c'è il passo **vendita** (`grgo-<nome>-vendita`,
`tools/banco/vendita.go`): nella stessa immagine dello standalone, senza rete, con le protezioni e
i dati dell'istanza come il legacy import, `bin/gestrip_standalone eval` avvia l'app come
`Release.quiet!/0` e chiama le funzioni della pagina delle vendite con il contesto d'audit della
pagina (`%{actor: <email del titolare sintetico>}`): `OnlineSales.create_draft/2`,
`SalePhotos.attach/3` su un piccolo JPEG inventato generato dal banco (montato in sola lettura) e
`OnlineSales.issue_sale/2`, che crea la revisione e copia la foto. Il profilo del negozio che
`issue_sale` vuole viene dal legacy import (che crea sempre il profilo «Negozio»); se mancasse,
`issue_sale` darebbe `{:error, :shop_profile_missing}` e il banco si fermerebbe con il motivo. Il
banco pretende una sola riga `GR_VENDITA:` riuscita, la foto nei media con la sua impronta e un
solo file in più. Dopo l'import Go controlla (non solo stampa): verdetto riconciliato, un avviso
`tipo/impronta_testo` su `sha256` di `historical_documents`, `sale_photos` e `sale_revision_photos`
per tutte le righe lette (D-42), le righe di `sales`, `sale_revisions`, `sale_photos`,
`sale_revision_photos` e `sale_revision_seals` lette e scritte come le ha contate lo standalone, e
le famiglie media `sale_photos` e `sale_revision_photos` complete (attesi = copiati = righe, nessun
file mancante né impronta diversa). Il riassunto le stampa insieme ai media in più della vendita.

Dalla slice 027 la catena arriva a un import **riconciliato**: il banco vuole anche esattamente un
avviso `traduzione/istante_senza_zona` su `sale_revision_photos.inserted_at`, per tutte le foto
delle revisioni scritte dalla vendita, senza `Id`; un avviso con lo stesso codice su una colonna
fuori dalla D-42 fa diventare rosso il banco.

Dalla slice 028 c'è anche il passo `emergenza`, dopo la vendita e prima della copia: un container
dell'immagine dello standalone senza rete che, con le funzioni della pagina, dà un QR a una busta
importata (`Workshop.update_repair/4`), apre un evento, scrive una riga, la storna, scansiona la busta
alla consegna e chiude l'evento (`EmergencyRegister`). Esito in una riga `GR_EMERGENZA:{…}`
controllata, riassunto in `runtime.json` sotto `registro_emergenza`. Da qui il banco **pretende**
l'avviso `traduzione/istante_senza_zona` su tutte e sei le colonne della D-42, ciascuna con le righe
esatte (foto delle revisioni della vendita, 2 righe del registro, 1 evento chiuso, 1 busta
scansionata).

**Attese per i dati (slice 029, T3b).** `banco esegui` legge dal `runtime.json` dell'istanza con
quali dati è partita (`account-seminati` o `sintetici-import`; mancante o sconosciuto = errore) e lo
passa a `confronta -dati`. Un passo di scenario può avere `atteso_dati: {"sintetici-import": {…}}`:
quei campi sostituiscono o si aggiungono a `atteso` solo con quei dati. Il campo `conteggio:<frase>`
dice quante volte una frase compare nel corpo (intero canonico, maiuscole distinte). Le frasi di
`testo:` e `conteggio:` non possono contenere caratteri che l'HTML scappa (`'`, `&`, `<`, `>`;
`"` solo in un attributo intero come `href="/settings"`). Oggi lo usa solo `utenti[2]`: su
database vuoto «Invito da consegnare» assente, sui dati importati presente 2 volte (i due account
Flask senza password). **I due giri del banco sono verdi**, 13 scenari e 156 passi ciascuno. Sulla catena gli scenari danno ancora la differenza nota `utenti[2]` (T3b).

**Testo e attributi di un elemento (slice 032).** Per confrontare due pagine con markup diverso
(Phoenix e `html/template`, che scrive `+` e `'` come `&#43;` e `&#39;`) ci sono tre campi, che
valgono anche in `atteso` e `atteso_dati`. Tutti prendono un **localizzatore**: un id
(`repair-status`) oppure `.classe@n` (l'n-esimo elemento, da 1, che ha quella classe), seguito
facoltativamente da `/tag` o `/tag@n` (il primo o l'n-esimo tag con quel nome dentro l'elemento,
per esempio `repair-detail/dd@2`). Se niente risponde il valore è `(assente)`.
- `visibile:<loc>`: il testo che si legge. Commenti tolti senza spazio; i tag in linea (`span`,
  `a`, `strong`, `em`, `b`, `i`, `small`, `code`, `label`, `button`, `input`…) tolti senza spazio,
  quelli di blocco e `<br>` valgono uno spazio; entità decodificate, spazi compattati.
  `333<span>111</span>` è «333111», `333<div>111</div>` è «333 111».
- `grezzo:<loc>`: il testo con a capo e spazi intatti (commenti e tag tolti senza spazio, entità
  decodificate, niente compattazione), per i riquadri pre-wrap, dove una riga vuota in più si vede.
- `attributo:<loc>:<nome>`: il valore dell'attributo nel tag di apertura, con le entità decodificate
  (`&quot;` e `&#34;` sono la stessa virgoletta); `""` per un attributo senza valore,
  `(senza attributo)` se l'elemento c'è ma l'attributo no.

**Limiti:** non è un parser HTML. Gli elementi si trovano con espressioni regolari sul corpo; la
chiusura si cerca contando solo i tag con lo stesso nome; id, classi e valori degli attributi si
leggono solo fra virgolette doppie (come li scrivono le due app); un `>` dentro un attributo
confonde la ricerca; il CSS non conta (un `span` reso a blocco dal foglio di stile resta in linea,
un elemento nascosto si legge lo stesso). Le classi delle due app sono diverse per costruzione
(Tailwind nello standalone), quindi `attributo:…:class` serve solo dove le classi sono le stesse.
Lo scenario `scheda` li usa sugli id del riquadro «Dati riparazione», su `repair-detail/dd@n`,
sull'intero `repair-detail/dl` delle buste chiuse (le aperte hanno in più la «Stima tempi»), su
`.whitespace-pre-wrap@1` (la descrizione) e sull'`href` del telefono.
Dalla 032 il banco ha **15 scenari e 189 passi**, verdi nei due giri.

**Passo `modifiche` e scenario `modifica` (slice 033).** Dopo il registro d'emergenza e prima della
copia c'è il passo `modifiche` (`grgo-<nome>-modifiche`, `tools/banco/modifiche.go`), con le
protezioni dei passi vicini (senza rete, `--rm`, i soli tre montaggi dell'istanza). Con le funzioni
delle pagine dello standalone crea sei buste nuove, 5/26–10/26, con i dati che la cattura non ha:
`Workshop.create_customer/2` e `create_repair/3` (la nuova busta), `Workshop.update_repair/4` e
`clear_expected_on/2` con il contesto della scheda (`actor` e `session_user`),
`Workshop.transition_repair/3` per gli stati e `QuoteDecision.approve/2` e `reject/3` con il
contesto della pagina pubblica (`actor: "public"`). Ne escono: preventivo e prezzo a testo,
numerici e in soli centesimi; consegna a testo, a data e tolta; note visibili e interne con a capo
e caratteri da scappare; nome e telefono da scappare; giorni in più; un artigiano nuovo; il cambio di
tipo in compro oro; una busta spostata a un'oreficeria senza telefono (link revocato); un cliente
senza telefono; il QR tolto e lo stesso QR su due buste; stati pronta, valutata e preventivo; un
preventivo accettato e uno rifiutato. L'esito è una riga `GR_MODIFICHE:{…}` letta in modo stretto
(`DisallowUnknownFields`, una sola riga): id 5–10, stati e tipi attesi, un solo link revocato, le
due risposte, 10 buste in tutto. Il banco pretende poi nel report dell'import Go tutte le buste
dello standalone lette (`repairs`) e scritte (`buste`). `banco giu` toglie anche questo container.
Le nuove righe non fanno nascere avvisi nuovi nell'import (sempre 9).

Lo scenario `modifica` confronta le sei schede (tutti gli id del riquadro, le tre note con
`grezzo:`, «Togli data», «Modifica», le risposte al preventivo con `testo:`) e il modulo
«Modifica riparazione» aperto con `?edit=prezzo` / `?edit=preventivo` su otto buste (anche la 2,
conto terzi, e la 4, fuori bottega): valori e attributi dei campi, testi nascosti, «Scritta a mano»,
il telefono spento con il suo avviso, le voci scelte dei due menu, la casella del compro oro, i
pulsanti; poi `?edit=altro` (la scheda) e il modulo senza accesso. Su database vuoto ogni busta è
«non trovata» dalle due parti. Le date delle risposte e di «Pronta dal» dipendono dal giorno del giro:
si confrontano fra le app ma non hanno un valore atteso. **Il salvataggio non si confronta:** nello
standalone è un evento LiveView sul websocket, come la nuova busta (031); la modifica la provano i
test Go. Non si confrontano nemmeno «📷 Scansiona» (il Go non lo ha ancora) e l'aspetto spento di
«Salva modifiche» (`aria-disabled`, LiveView). Una prova negativa (centesimi del modulo con la
virgola nel binario Go) dà 2 differenze.
Dalla 033 il banco ha **16 scenari e 209 passi**, verdi nei due giri.

**Passo `stati` e scenario `stati` (slice 034).** Dopo le modifiche e prima della copia c'è il passo
`stati` (`grgo-<nome>-stati`, `tools/banco/stati.go`), con le protezioni dei passi vicini (senza
rete, `--rm`, i soli tre montaggi dell'istanza). Con le funzioni delle pagine dello standalone crea
cinque buste, 11/26–15/26: `Workshop.create_customer/2` e `create_repair/3` (la nuova busta);
`TelegramNotices.transition/3` (l'evento transition della scheda), `Workshop.reopen_repair/3` e
`set_repair_waiting/3` con il contesto della scheda (`actor` e `session_user`); `ReadyLots.open_lot/2`,
`add_bag/3` e `send_lot/2` con il contesto delle pagine dei lotti (`actor`). Ne escono: in attesa
materiali con un'attesa del cliente e un motivo da scappare; annullata con un'attesa senza motivo
(azione « ON », motivo di spazi); una compro oro valutata, perfezionata e chiusa; una busta
dell'oreficeria 2 pronta, messa in un lotto inviato e consegnata, che chiude il lotto
(`ReadyLots.close_after_delivery/2`); una consegnata e riaperta in pronta con un motivo da scappare,
messa in attesa e tolta. L'esito è una riga `GR_STATI:{…}` letta in modo stretto
(`DisallowUnknownFields`, una sola riga): id 11–15, stati, tipi, attese e versioni
(`status_version/1`) attesi, una riapertura, il lotto chiuso, il numero dei lotti, le 105 aperture della scheda della 11/26 (paginazione del registro, sotto) e 15 buste in tutto.
Il banco pretende poi nel report dell'import Go tutte le 15 buste (`repairs` → `buste`) e tutti i
lotti (`ready_lots` → `lotti_avviso`). `banco giu` toglie anche questo container. Le nuove righe non
fanno nascere avvisi nuovi nell'import (sempre 9).

Lo scenario `stati` confronta la scheda delle cinque buste e di quattro di prima (la 2 consegnata
dalla scansione del registro, la 6 pronta, la 7 valutata, la 10 preventivo): stato, «Pronta dal» e
«Consegnata il», il modulo «Cambia stato» (testo, nome e `required` del menu, valore e testo di ogni
voce, il pulsante), il modulo «Riapri la busta» (titolo, testo, menu con le voci, il motivo con nome,
tipo, valore e `required`, il pulsante e il suo `data-confirm`), il riquadro dell'attesa («dal», il
motivo, «✓ Risposta arrivata») o il modulo per metterla (`summary`, il campo con nome, tipo, valore e
segnaposto, il pulsante); poi due schede dal dipendente e una senza accesso. Su database vuoto ogni
busta è «non trovata» dalle due parti. Le date di pronta, consegna e attesa dipendono dal giorno del
giro: si confrontano fra le app ma non hanno un valore atteso. **Il cambio di stato, la riapertura e
l'attesa non si confrontano:** nello standalone sono eventi LiveView sul websocket, come la modifica
(033); li provano i test Go. Non si confrontano nemmeno la finestra di conferma (`data-confirm`,
JavaScript) e i campi nascosti `waiting[azione]` (senza id).

**Paginazione del registro (slice 034).** Con le buste del passo `stati` il registro dello standalone
supera le 100 righe prima del Go: la home dello standalone scrive `repair.index_viewed` a ogni
apertura e quella del Go (T4.2) no (misurato: con l'elenco delle azioni dalle due parti l'unica in
più è `repair.index_viewed`, 19 righe in un giro). Per questo i totali del registro intero
differiscono e il numero delle pagine può essere diverso. Per provare la paginazione davvero, il
passo `stati` apre 105 volte la scheda della 11/26 con `Workshop.record_repair_event/3` (la riga
`repair.viewed` del mount della scheda; il conteggio esatto è nella riga `GR_STATI`): sono più di
una pagina, uguali dalle due parti dopo l'import. Lo scenario `registro` confronta sul filtro
`action=repair.viewed` il totale (`visibile:audit-total`), le righe della pagina, e pretende sui dati
importati 100 righe e «Successiva» a pagina 1, «Precedente» sull'ultima; sul database vuoto nessuna
delle due. Sul registro intero restano i controlli veri dalle due parti: sui dati importati
«Successiva» a pagina 1 e «Precedente» sull'ultima, sul database vuoto nessuna; «Precedente»
assente a pagina 1 e «Successiva» assente sull'ultima in tutti e due i giri. **Scoperto fino alla
T4.2:** il totale e il numero delle pagine del registro intero, e quindi quante pagine ci sono e
quali righe cadono su ognuna, perché lo standalone ha in più le righe `repair.index_viewed` della
home.
Dalla 034 il banco ha **17 scenari e 231 passi**, verdi nei due giri.

**Passo `link` e scenario `link` (slice 035).** Dopo gli stati e prima della copia c'è il passo
`link` (`grgo-<nome>-link`, `tools/banco/link.go`), con le protezioni dei passi vicini (senza rete,
`--rm`, i soli tre montaggi dell'istanza). Con le funzioni degli eventi `revoke-link` e
`regenerate-link` della scheda, `Workshop.revoke_public_access/2` e `regenerate_public_link/2`, e il
contesto della scheda (`actor` e `session_user`), più `Workshop.create_customer/2` e `create_repair/3`
per le buste nuove: un nuovo link sulla 1/26 della cattura (link scaduto con la sola impronta,
vecchio link con codice vivo, che si revoca per sempre), una revoca sulla 3/26 della cattura (anche
lei con il vecchio link), la 16/26 nata qui e revocata, la 17/26 dell'oreficeria 2 con un nuovo link,
la 18/26 revocata e poi rinnovata. I token non escono mai dallo script: l'esito è una riga
`GR_LINK:{…}` letta in modo stretto (`DisallowUnknownFields`, una sola riga) con lo stato del link
(`public_link_status/1`) e del vecchio link (`legacy_link_status/1`) delle buste rilette, tre nuovi
link `manual` (uno con `legacy: true`), tre revoche dei comandi (una con `legacy: true`; quella per
cambio di controparte delle modifiche ha il motivo e non conta) e 18 buste in tutto. Il banco
pretende poi nel report dell'import Go tutte le 18 buste. `banco giu` toglie anche questo container.
Le nuove righe non fanno nascere avvisi nuovi nell'import (sempre 9).

Lo scenario `link` confronta il riquadro `#repair-public-link` di dieci schede dal titolare (le cinque
del passo, la 2 e la 4 della cattura con il link scaduto, la 5 attiva, la 8 revocata dal cambio di
oreficeria) e di tre dal dipendente, più una senza accesso: la riga dello stato, il menu «⋯» con il suo
`aria-label` e `data-close-outside`, «Revoca link» e «Genera nuovo link» con le loro conferme, il
testo intero del riquadro, il nuovo link mostrato una volta (sempre assente: lo mostra solo la pagina
dopo un POST). Su database vuoto ogni busta è «non trovata» dalle due parti. Sui dati importati ogni
scheda ha un'attesa esplicita (`TestScenarioLinkSuiDueDati`): il testo intero dello stato dove non
dipende dal giorno del giro, altrimenti `conteggio:` di «attivo fino al» e della riga del vecchio
link; il menu c'è solo per il titolare. **Le revoche e i nuovi link non si confrontano:** nello
standalone sono eventi LiveView sul websocket, come la modifica (033); li provano i test Go, che
provano anche ciò che il passo non può fare con le funzioni delle pagine su questi dati: un link con
la sola impronta ancora valido (e quindi la conferma che nomina il vecchio link con codice), «·
inviato il», i tagli di Telegram ed email. `grezzo:` della riga dello stato non si confronta: HEEx ci
mette a capo e spazi che il Go non scrive (il testo visibile è lo stesso). Una prova negativa (menu
del link mostrato anche al dipendente nel binario Go) dà 41 differenze.
Dalla 035 il banco ha **18 scenari e 252 passi**, verdi nei due giri.

**Passo `foto`, campi delle foto e scenari `foto` (slice 037).** Dopo il link e prima della copia
c'è il passo `foto` (`grgo-<nome>-foto`, `tools/banco/fotobuste.go`), con le protezioni della
vendita (senza rete, `--rm`, i tre montaggi dell'istanza più la sola foto in sola lettura). Le tre
foto della cattura sintetica sono byte che non si decodificano (`synthetic_capture.py`), quindi
senza questo passo il banco non vedrebbe mai una miniatura vera. Il passo carica sulla 1/26 un JPEG
inventato di 1000×750 generato dal banco (`fotoBustaSintetica`, nessun file binario nel repo) con
la funzione del caricamento della scheda, `RepairPhotos.attach_many/3`, e il contesto della scheda
(`actor` e `session_user`). Lo standalone lo normalizza con Pillow. L'esito è una riga
`GR_FOTO:{…}` letta in modo stretto (`DisallowUnknownFields`, una sola riga): foto 4, busta 1,
posizione 3, `image/jpeg`, 4 foto delle buste in tutto. Il banco pretende poi nel report
dell'import Go le 4 foto lette (`repair_photos`) e scritte (`foto_buste`). `banco giu` toglie anche
questo container.

Tre campi nuovi nel confronto, validi anche in `atteso` e `atteso_dati`:
- `impronta`: lo SHA-256 esadecimale del corpo;
- `lunghezza`: i byte del corpo;
- `dimensioni`: `<larghezza>x<altezza>` dell'immagine nel corpo (JPEG, PNG o WebP, dalla sola
  intestazione), o `(non un'immagine)`.

Così una foto si confronta byte per byte senza mettere i byte nell'esito. Una miniatura fatta in Go
e una fatta da Pillow hanno byte diversi per scelta (tema H, ricevuta della 036) e si confrontano
per tipo, intestazioni e dimensioni.

Lo scenario `foto` confronta `GET /repairs/{id}/photos/{foto}`:
- senza accesso: `/login`, e dopo l'accesso tutte e due le app tornano all'indirizzo chiesto;
- le tre foto della cattura: intere, con l'impronta e la lunghezza calcolate dai byte di
  `synthetic_capture.py`, anche con `?size=thumb`, che ripiega sulla foto intera con
  `x-photo-variant: full` e `no-store`;
- la foto del passo: intera 1000×750 confrontata byte per byte fra le app; in miniatura WebP
  400×300, `private, max-age=86400`;
- `?size=` in sette forme (`THUMB`, `size[]`, `thumb%20`, l'ultima assegnazione che vince, un `g`
  falso con e senza miniatura);
- dodici rifiuti: altra busta, foto che non c'è, id malformati, ostili o troppo grandi, anche con
  `?size=thumb`. Ognuno è il 404 «Foto non disponibile», `no-store`, `no-referrer`, senza
  `content-type`;
- alla fine il totale delle righe `repair.staff_photo_viewed` del registro (28: una per lettura,
  anche per i rifiuti, con le 4 di `foto-dipendente`, che gira prima).

Lo scenario `foto-dipendente` fa le stesse letture principali dal dipendente. Su database vuoto ogni
foto dopo l'accesso è il rifiuto dalle due parti (`TestScenarioFotoSuiDueDati`).

**Misurato sul banco:** dietro Caddy lo standalone risponde al 404 senza `content-type` e senza
`content-length` (corpo a pezzi). Il Go manda le intestazioni prima del corpo, con lo stesso
risultato. Con la lunghezza, invece, Caddy aggiungeva `text/plain; charset=utf-8`.

**Non si confrontano:**
- il gettone buono della galleria: lo firma la galleria della home (T4.2), che il Go non ha
  ancora; lo provano i test Go;
- le regole della cache su disco (limite, voce guasta, pulizia): sono interne;
- la `content-length` delle foto: lo standalone non la manda. Il Go la manda solo per i corpi
  piccoli, che net/http scrive in una volta (fino a 2048 byte, come le foto sintetiche della
  cattura); una foto vera parte a pezzi come nello standalone. Non è osservabile da una pagina.

Una prova negativa (`?size=THUMB` accettato come miniatura nel binario Go, messo nel container del
Go al posto di quello buono) dà 9 differenze sul passo, banco rosso.
Dalla 037 il banco ha **20 scenari e 287 passi**, verdi nei due giri.

**Passo `numeri` e scenario `numeri` (slice 037d, `go-baseline` = slice 220, immagine
`gestrip-standalone:fab-220`).** Dopo le foto e prima della copia c'è il passo `numeri`
(`grgo-<nome>-numeri`, `tools/banco/numeri.go`), con le protezioni dei passi vicini (senza rete,
`--rm`, i soli tre montaggi dell'istanza). Con le funzioni della nuova busta e della modifica
(`Workshop.create_customer/2`, `create_repair/3`, `update_repair/4` con il contesto della scheda)
scrive «019/26» (salvato 19/26, busta 19) e «000020/26» (salvato 20/26, busta 20, contatore a 20) e
prova sei rifiuti della nuova busta (« 0019/26 » già esistente, «0/26» e «00000/26» fuori formato,
data e lavorazione vuote, data impossibile) e tre della modifica della 19/26 (campi obbligatori
vuoti, nome e telefono troppo lunghi, data e giorni in più non validi). Gli errori si leggono come
la pagina: `traverse_errors/2` con `CoreComponents.translate_error/1`, cioè il catalogo `it`.
L'esito è una riga `GR_NUMERI:{…}` letta in modo stretto (`DisallowUnknownFields`, una sola riga):
buste 19 e 20, numeri 19/26 e 20/26, prossimo numero 21/26, 20 buste in tutto. Poi il banco fa le
stesse operazioni con il **dominio Go** (`internal/buste`, linkato nel banco) su un database suo,
nella cartella dell'istanza, che poi cancella, e pretende lo stesso esito: numeri, prossimo numero,
e per ogni rifiuto gli stessi campi, gli stessi testi e lo stesso numero rimostrato
(`confrontaNumeri`). Così il confronto c'è anche se nello standalone la nuova busta e la modifica
sono eventi LiveView, che il banco HTTP non manda. Il banco pretende poi nel report dell'import Go
tutte le 20 buste. `banco giu` toglie anche questo container.

Lo scenario `numeri` confronta le schede 19 e 20 (titolo «Busta #19/26 · Laboratorio», mai con gli
zeri, data, cliente, stato, QR); su database vuoto sono «non trovate» dalle due parti
(`TestScenarioNumeriSuiDueDati`). «1 minuto» del login non c'è: il banco non arriva all'ultimo minuto
di un blocco (servono 14 minuti di attesa o verifiche simultanee); lo provano i test Go.

Prove negative: l'immagine `fab-219` (standalone prima della 220) ferma la catena al passo
(« 0019/26 » accettato come busta nuova); un banco con il dominio Go mutato («is invalid» al posto
di «Valore non valido.») la ferma con le due differenze, campo per campo.
Dalla 037d il banco ha **22 scenari e 314 passi**, verdi nei due giri.

**Com'è fatta un'istanza:**
- `grgo-<nome>-app` (standalone), `-caddy` (TLS interno, CA propria), `-go` (binario statico su
  `debian:bookworm-slim` fissata per digest) e `-banco` (il confronto), tutti sulla rete
  `grgo-<nome>-net` `--internal`;
- container con utente 1000, `cap-drop ALL`, sola lettura e limiti di CPU e RAM (4 CPU, 4 GB);
- segreti generati al momento, con permessi 0600, in
  `to-be-deleted-after-use/banco/grgo-<nome>/`;
- il profilo seccomp di Chromium letto con `git show go-baseline:` (mai dal worktree).

## Ricevuta di completamento

- **Data:** 30/10/2026, sessione Claude Code su pc-fisso.
- **Immagine:** `gestrip-standalone:fab-213`, cioè la slice 213 = `go-baseline` `5c80347`
  (creata il 29/10/2026 alle 23:02), id `sha256:abab45135c65…`.
- **TDD:** 6 test del confronto e 4 dell'esecuzione, scritti prima (`undefined: leggiScenario`) e
  poi verdi; 2 test della copertura per gli scenari.
- **Prove dal vivo:**
  - `banco su -nome t0`: bootstrap dello standalone e istanza pronta in 3 s;
  - `docker ps`: nessuna porta pubblicata; rete `internal=true`; `ss -ltn` sull'host senza le
    porte dell'istanza;
  - `banco esegui`: 1 scenario, 2 passi (GET con query inventata e HEAD), **0 differenze →
    BANCO VERDE**;
  - prova negativa con uno scenario temporaneo `POST /healthz`: «INATTESA codice: standalone
    404, go 405» → banco rosso. È il debito già annotato nella 003;
  - `banco giu -nome t0` → «AVANZI: nessuno».
- **Review indipendente (30/10/2026):** 13 osservazioni, 1 alta, tutte valutate.
  - **Alta, corretta: il controllo delle D-xx approvate non partiva mai.** Con
    `sh -c '… "$0" "$1" "$@"'` il primo argomento posizionale (`$1`) finiva anche in `"$@"`,
    `flag` si fermava lì e `-inventario` veniva ignorato. Ora non c'è più nessuna shell: due
    `docker run` (`attendi`, poi `confronta`) con argomenti diretti.
    - `TestComandiEseguiPassanoInventarioEdEsito` riesegue il parsing dei flag sugli argomenti
      veri;
    - `confronta` rifiuta argomenti in più;
    - con deviazioni presenti l'inventario è obbligatorio.
    - Prova dal vivo: una D-99 non approvata → «deviazione D-99 usata dal banco ma non approvata
      nell'inventario», banco rosso.
  - **Medie, corrette:**
    - `giu` non cancella niente se docker non risponde, e si ferma sugli errori diversi da «No
      such…»;
    - `docker rm -v`;
    - `su` crea la cartella delle istanze prima di misurare lo spazio;
    - percorsi controllati con `EvalSymlinks` sia in `su` sia in `giu`: assoluti, senza
      virgole, mai verso la produzione;
    - `-banco` vuoto rifiutato;
    - `confronta -esito` scrive l'esito per scenario.
  - **Allineato il PIANO:** `bench/scenari/`, `bench/deviazioni.json`, stati della copertura. Lo
    scenario `healthz` ora confronta anche `referrer-policy` e `x-robots-tag`.
  - **Debiti bassi:**
    - `-immagine` non viene verificata contro il tag (l'immagine non ha label di revisione);
    - deviazioni non più usate non segnalate;
    - corpi oltre 16 MiB troncati.
- **Mutazioni:** 10 sulle correzioni. 8 uccise. Una era scritta male e non si è applicata. La
  sopravvissuta era un ramo `Readlink` superfluo, che è stato tolto: `EvalSymlinks` già rifiuta i
  collegamenti verso la produzione e quelli che non si risolvono, come provano i test.
- **Secondo giro dal vivo, con il codice corretto:**
  - `su` → `esegui` → **BANCO VERDE**, con `inventario_controllato: true`;
  - `giu` → «AVANZI: nessuno».
- **Copertura:** con l'esito del banco ADM-20 è **chiusa** (4 test Go verdi più lo scenario
  `healthz` verde). Senza l'esito resta parziale. In tutto 353 voci: 1 chiusa, 339 aperte, 13
  fuori v1.
- **Debiti:**
  - le immagini vengono usate, ma niente pull né build: se manca un'immagine, `su` si ferma;
  - livello browser e dati sintetici in T3;
  - confronto del testo normalizzato e dei PDF quando arrivano le pagine.

**Passi solo del Go (slice 037b).** Un'azione che nello standalone è un evento LiveView (niente
HTTP) e nel Go è un POST con un redirect si prova con un passo `"solo_go": "D-xx"`: gira solo sul
Go, non confronta (`confronta` vuoto) e il Go deve dare esattamente le sue attese (`atteso`, e
`atteso_dati`); una differenza è sempre inattesa, nessuna deviazione la copre. La D-xx deve essere
approvata nell'inventario, come quelle di `deviazioni.json`. Solo un passo solo_go può avere nel
percorso `{location}` (la Location della risposta di prima del Go) o `{href:<id>}` (l'href del link
con quell'id), per seguire un indirizzo con un segno casuale; un segnaposto che non dà un percorso
relativo è un errore. Lo usa lo scenario `viste` (D-44): il conteggio di `repair.viewed` prima e
dopo le azioni si confronta sulle due parti. Nel corpo di un passo solo_go `{mostra}` è il segno
dei moduli della pagina di prima del Go (vuoto se non c'è). Un passo `"solo_standalone": "D-xx"`
gira solo sullo standalone, con le stesse regole e senza segnaposti: è quello che lì è una richiesta
e nel Go sta dentro passi solo_go (per esempio l'apertura della scheda che nel Go fa un rifiuto
forgiato senza il segno della pagina).
