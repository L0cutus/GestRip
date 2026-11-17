# 092 — Pagina pubblica del lotto `/lotto/{token}`, chiusura alla lettura e foto (T4.9, settima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/controllers/public_lot_controller.ex`
  (`show/2`, `photo/2`, `public_headers/1`, `context/1`), `public_lot_html.ex` (`shown_price/1`, `total/1`, `total?/2`,
  `price_label/1`, `to_collect/1`, `collected?/1`, `description/1`, `day/1`, `shop_line/1`) e
  `public_lot_html/show.html.heex`; `lib/gestrip_standalone/ready_lots.ex` (`get_public_lot/1`, `public_members/1`,
  `member/2`, `close_if_complete/2`); `repair_photos.ex` (`first_visible_by_repair/2`, `read_public_media/5`,
  `audit_public_read/6`); `public_attempts.ex` (`family(["lotto", _token])`); `online_sales.ex` (`get_shop_profile/0`,
  `shop_name/1`); `router.ex:136` e `:141`; test `public_lot_controller_test.exs`.
- **Voci:** ORE-14, ORE-15, ORE-16. Nessuna migrazione. Deviazioni usate: D-64 (numero visibile), D-05 (stesso 404
  anche da bloccati). Decisioni: domanda G (coordinatore e Stefano, 17/11/2026: uguaglianza esatta del gettone, nessun
  controllo oltre «non vuoto»). D-100 (foto pubbliche con l'audit non scritto, Dubbi 2), approvata da Stefano il 17/11/2026.
- **Cautela (domanda G):** il sorgente del Flask non era su pc-fisso. Che i gettoni del Flask siano
  `secrets.token_urlsafe(24)` (32 caratteri url-safe) e che arrivino intatti viene dalle citazioni nello standalone a
  `go-baseline` (`docs/parity-matrix.md:3046-3050`, `lib/gestrip_standalone/legacy_import/operational.ex`) e
  dall'import del Go (`internal/importer/mappa.go:151-158`). **Al cutover si apre con il Go un link `/lotto/…` vero già
  mandato a un'oreficeria.**

## Cosa c'è

- **Dominio** (`internal/buste/lotto_pubblico.go`, nuovo):
  - `lottoPubblico` = `get_public_lot/1`: `WHERE token = ? AND inviato_il IS NOT NULL AND accesso_pubblico_spento_il IS
    NULL`, uguaglianza esatta, gettone vuoto rifiutato prima della query. Ogni rifiuto è `ErrLottoNonDisponibile`;
  - `PaginaLottoPubblica(ctx, gettone)` = `show/2`: le buste (`public_members/1`: `oreficeria_id` uguale a quella del
    lotto, fuori dal cestino, `ORDER BY id DESC`, numero D-64), la chiusura alla lettura, `chiuso_il` riletto, la prima
    foto visibile all'oreficeria di ogni busta (`VisibiliPer(…, PubblicoOreficeria)`), il profilo del negozio;
  - `chiudiAllaLettura` = `close_if_complete/1`: la stessa `chiudiLottoCompleto` della consegna (slice 212: inviato,
    non chiuso, almeno una busta, nessuna busta non consegnata, **cestino compreso**), con l'istante di adesso, in una
    transazione di scrittura. Prima una lettura con la stessa condizione: se il lotto non si chiuderebbe, la
    connessione di scrittura non si prende (solo del Go, nessun effetto visibile; vedi Scelte);
  - `BustaLottoPubblico.EtichettaPrezzo` = `price_label/1`; `PaginaLottoPubblico.Totale`/`ConTotale`/`DaRitirare` =
    `total/1`, `total?/2`, `to_collect/1`;
  - `LeggiFotoLotto(ctx, gettone, busta, foto, v, c)` = `photo/2`: lotto del gettone, poi `member/2` (la busta del lotto,
    ancora della sua oreficeria, fuori dal cestino; nessuna riga d'audit se no), poi la foto con la colonna
    dell'oreficeria e la riga `repair.public_photo_viewed` (`audience` «orefice», `photo_id` ripulito, `variant` per
    la miniatura).
- **Riuso:** `fotoArtigianoLetta` (084) è diventata `fotoPubblicaLetta` con il pubblico come parametro, usata
  dall'artigiano e dal lotto; `ProfiloNegozio` legge anche `cap` (per `shop_line/1`); `nullaTesto`, `nullaIntero`,
  `NumeroVisibile`, `EtichettaPrezzo`, `EtichettaCentesimi`, `Grafemi`.
- **Web** (`internal/web/lotto_pubblico.go`, `pagine/lotto_pubblico.html`, `app.css` `.lotto-pubblico*`):
  `GET /lotto/{token}` e `GET /lotto/{token}/buste/{busta}/foto/{foto}` con `rottaPubblica` (registrate in
  `registraCliente`). La pagina: `intestazioniPubbliche` (no-store, no-referrer), niente X-Robots-Tag (come lo
  standalone), gli id dello standalone (`public-lot-greeting`, `-count`, `-total`, `-unpriced`, `-complete`,
  `-partial`, `-pending`, `-empty`, `-bag-<id>`, `-photo-<id>`). Il rifiuto è `RifiutoPubblico(w, r, rifiutoLotto,
  false)`: lo stesso 404 «Lotto non disponibile» senza Content-Type, contato dal limite (la famiglia `lotto` c'era già
  in `limiti.go`). La foto: `variante(r)` (solo `size=thumb` esatto), `rispostaFotoPubblica`, 404 «Foto non
  disponibile» non contato. L'indirizzo della miniatura è `/lotto/<segmentoPhoenix(gettone)>/buste/<id>/foto/<id>?size=thumb`
  con il gettone **della richiesta** (come `~p` nello standalone). Il telefono del piè con `attributoTel`.
- **Interfaccia `Buste`** (`nuova.go`) e `busteFinte` con i due metodi nuovi.
- **SVILUPPO:** punto «Pagina pubblica del lotto» nelle rotte; il Caddy del banco manda anche `/lotto/*` alla faccia
  pubblica.

## Scelte

1. **Uguaglianza esatta e niente altro** (domanda G): nessun controllo di lunghezza o alfabeto; i gettoni del Flask
   (32 caratteri) e del Go (32 caratteri, `gettoneLotto`) passano allo stesso modo; i `disabled-import-…` cadono sulla
   condizione dell'accesso spento, non sulla forma.
2. **Lettura prima della scrittura per la chiusura.** Lo standalone fa sempre un `UPDATE` condizionato (in modalità
   immediate prende il writer a ogni apertura). Il Go chiede prima con una lettura se il lotto si chiuderebbe: un lotto
   a metà o già chiuso non tocca mai la connessione di scrittura, così le anteprime dei link nelle app di messaggi e le
   ricariche non mettono in coda le scritture del negozio. L'esito è identico (la scrittura condizionata decide sempre,
   la gara di 12 letture scrive una data sola). Non è una deviazione: nessuna risposta cambia.
3. **Nessun audit** né per la pagina né per la chiusura, come lo standalone.
4. **Numero D-64**: lo standalone scrive «N. <bag_number>»; il Go `NumeroVisibile` dà lo stesso «N. 14/26» senza
   etichetta e «0045/26» con l'etichetta. L'alt della miniatura è «Foto busta <numero senza N.>».
5. **Gli indirizzi delle foto** non portano il gettone in un `Referer` (no-referrer) e la pagina non resta in cache
   (no-store); le foto hanno le stesse due intestazioni.

## Prove

- **Rosso** (`to-be-deleted-after-use/slice-092/prove/rosso.txt`): scritto con `PaginaLottoPubblica`/`LeggiFotoLotto`
  in stub e le rotte non registrate (il codice era già scritto: gli stub sono stati messi e tolti per il giro rosso,
  copie in `slice-092/originali/`): 5 test del dominio e 6 della pagina rossi; in coda il test della lettura senza
  writer, rosso togliendo la lettura prima della scrittura.
- **Dominio** (`internal/buste/lotto_pubblico_test.go`): `TestLottoPubblicoSoloInviatoEAcceso` (gettone di 32 caratteri,
  vuoto, uno in più/meno, maiuscole, spazio davanti, lotto aperto, `disabled-import-7` spento, `%`; niente scritto),
  `TestLottoPubblicoBusteEPrezzi` (ordine, altra oreficeria e cestino fuori, prezzi finale/intervallo/nessuno/
  preventivo/soli centesimi, totale 129,50 con 2 senza importo, numero D-64, date, totale zero mostrato, solo testo
  senza totale), `TestLottoPubblicoChiudeAllaLettura` (a metà no, completo sì con l'istante, seconda lettura uguale,
  nessun audit, cestino non consegnato tiene aperto, lotto vuoto), `TestLottoPubblicoChiusuraInsieme` (12 letture, una
  data), `TestFotoLottoPubblico` (ok e miniatura con audit «orefice», prime foto, denied per altra busta/«zero»/«0»/
  nascosta, la pagina passa alla seconda foto, niente righe per gettone sbagliato, busta fuori, finta, zero, gettone
  con spazio, altra oreficeria, al negozio, cestino, lotto non inviato),
  `TestLottoPubblicoNonPrendeLaScritturaSenzaChiudere`.
- **Pagina** (`internal/web/lotto_pubblico_test.go`): `TestPaginaLottoPubblico` (markup esatto di testa, totale,
  «(2 senza importo esatto)», stato, righe, descrizione di 101 grafemi tagliata, piè con indirizzo CAP città e
  `tel:`, intestazioni, niente dati del cliente, niente audit; una busta senza profilo: «1 riparazione pronta»,
  «(1 senza importo)», piè «Negozio»), `TestLottoPubblicoGiornoDelNegozio`, `TestLottoPubblicoStessoRifiuto` (otto
  rifiuti uguali fra loro: 404, corpo, no-store, no-referrer, nessun Content-Type, stesse intestazioni di sicurezza;
  il lotto spento dopo l'invio), `TestLottoPubblicoLimiteDeiFalliti` (40 pagine buone e 40 foto 404 non bloccano; 30
  gettoni sbagliati bloccano anche il gettone giusto; la foto non è bloccata; un altro indirizzo passa),
  `TestLottoPubblicoChiusuraAllaLettura` (a metà, HEAD chiude, «✓ Lotto completato», data invariata, cestino,
  vuoto), `TestFotoLottoPubblico` (src `?size=thumb`, foto e miniatura, solo `size=thumb` esatto, 8 righe «ok», sei
  rifiuti per foto e miniatura con lo stesso 404, foto nascosta, busta passata ad altra oreficeria).
- **Banco (unità):** `TestTokenBanco` (gettone del lotto di 32 caratteri, script, scenario), `TestScenariInFondo`
  (ultimi tre: `firma`, `lotto-pubblico`, `qr-pannello`), `TestProvaConDegliScenariDelRepo`,
  `TestScenarioUtentiSuiDueDati`, il Caddy in `istanza_test.go` e `gotelegram_test.go`.
- **`scripts/check`** verde (`to-be-deleted-after-use/slice-092/prove/check.txt`).

## Banco

**Giro s092a (17/11/2026, sessione principale): verde.** HTTP 82 scenari (in più `lotto-pubblico`), 1304 passi, 0
inattese, 0 errori; browser 28 scenari, 56 viste, 0 inattese, 0 errori. Memoria disponibile minima 19,5 GB. Copertura con
l'esito: 353 voci, chiuse 200 (ORE-14, ORE-15, ORE-16 chiuse), rosse 0, parziali 16, aperte 126, fuori v1 11.

Commit prima solo locale, in attesa della D delle foto pubbliche; D-100 approvata da Stefano il 17/11/2026, poi push.

- **Catena, passo `token`** (`tools/banco/token.go`): il lotto della 14/26 (l'unico, inviato e chiuso alla consegna
  dal passo degli stati) riceve il gettone `SINTETICO-banco-lotto-14-xxxxxxx` (32 caratteri, `lottoBanco`) e perde
  `closed_at`: un lotto completato prima della slice 212. Lo script controlla con `ReadyLots.get_public_lot/1` che il
  gettone apra il lotto e che sia aperto. Il Go lo riceve con l'import. Nessuna pagina interna mostra `closed_at` di un
  lotto inviato (`lotto-pagina` guarda `/ready-lots/1`, che dipende da `inviato_il`).
- **Caddy:** `/lotto/*` alla faccia pubblica del Go (`percorsiPubblici`).
- **HTTP `lotto-pubblico`** (nuovo, `in_fondo`, `prova_con: sintetici-import`, ORE-14/15/16, `X-Forwarded-For:
  {ip_cliente}` su ogni passo): tre rifiuti della pagina (gettone inventato, `disabled-import-1`, il gettone giusto
  meno un carattere) con corpo e intestazioni attesi; due aperture del gettone giusto (con i dati: 200, `no-store`,
  `no-referrer`, niente X-Robots-Tag, «✓ Lotto completato…» già alla prima, niente parziale/in attesa/vuoto, «(1
  senza importo)», niente totale, una busta, nessuna miniatura; il saluto, il conto con la data, la riga
  `public-lot-bag-14` e `conteggio:tel:` si confrontano fra le app); sei rifiuti delle foto («Foto non disponibile»,
  no-store, no-referrer, niente X-Photo-Variant): la foto 4 (della 1/26) sotto la 14/26, intera e miniatura; la 1/26
  (fuori dal lotto) con la sua foto 4, intera e miniatura (un gettone di lotto non apre le foto di un'altra busta);
  gettone inventato; id «zero». Sul database vuoto ogni passo è il 404 (5 falliti della famiglia `lotto`: i tre
  rifiuti e le due aperture).
- **Cosa confronta di ORE-15:** la prima apertura chiude il lotto in tutte e due le app (prima il lotto era aperto
  per il passo della catena) e lo dice nella stessa risposta; la seconda dice lo stesso. La data non si vede: la
  provano i test Go.
- **Interferenze:** `in_fondo` perché la lettura chiude il lotto 1 e i rifiuti delle foto scrivono tre righe
  `repair.public_photo_viewed` «denied» (14/26 con la foto 4, intera e miniatura, e 14/26 con l'id «zero»: passano
  tutte da `member/2`) dopo lo scenario `registro`. Le righe
  sono le stesse nelle due app. Il limite: 5 falliti della famiglia `lotto` sul database vuoto, 3 con i dati,
  con un `{ip_cliente}` suo; le foto non contano. Gli scenari browser non leggono il lotto 1 né le sue righe.
- **Niente foto 200 nell'HTTP.** La 14/26 non ha foto e aggiungerne una nella catena sposta gli id delle foto che
  altri scenari scrivono nelle attese (`conferme` del browser: `remove-repair-photo-6`, `totaleFotoBuste`, il passo
  delle foto). La foto servita, la miniatura e l'audit «ok» li provano i test Go. Se la sessione principale vuole la
  foto 200 anche nel banco: una foto alla 14/26 nel passo `foto` (dopo quella della 1/26), poi `totaleFotoBuste` 5 e gli
  id dei passi browser da rileggere.
- **Se qualcosa è rosso, da guardare per primo:** (a) `visibile:public-lot-bag-14` dipende da come `testoVisibile`
  unisce i blocchi (Phoenix va a capo dentro i `div`, il Go no): se differisce solo negli spazi, è il markup; (b)
  `intestazione:content-type` dei 404: `RifiutoPubblico` manda le intestazioni prima del corpo per non far mettere
  `text/plain` al Caddy (084); (c) `GR_TOKEN:errore` con il lotto: il passo degli stati non ha chiuso il lotto alla
  consegna (`closed_at: %DateTime{}` nel `with`).

## Revisione

- **Mutazioni:** 66, di cui 64 compilano; 56 uccise. Delle 8 sopravvissute, 4 sono equivalenti (D1, D9, D28, W10) e
  4 erano buchi dei test: D23 (`l.id = ?` nella barriera `member/2` di `LeggiFotoLotto`), W17 (telefono del negozio
  vuoto nel piè), W13 (`X-Robots-Tag` sul 404 del lotto), ora coperte; W18 è il ramo della D da approvare (Dubbi, 2).
- **Rilievi:** 1 MEDIA e 3 BASSI, chiusi. MEDIA: una busta di un secondo lotto inviato, della stessa oreficeria e di
  un'altra, aperta con il gettone del lotto A dà nil/404 senza righe d'audit (test di dominio e `TestFotoLottoPubblico`
  della pagina, con il controllo che col gettone del suo lotto la foto si veda). BASSI: il telefono vuoto nel piè
  (`TestPaginaLottoPubblico`), `X-Robots-Tag` assente sul 404 (`TestLottoPubblicoStessoRifiuto`), i conteggi del banco
  in questa ricevuta (3 righe «denied», 5 falliti sul database vuoto).
- **Verifica a mano** (`to-be-deleted-after-use/slice-092/prove/mutazioni-revisione.txt`): D23 e W13 riapplicate
  muoiono. W17 applicata solo nel Go (`&& *Telefono != ""` tolto) sopravvive ancora ed è equivalente: il modello
  scrive il telefono solo con `{{if .Telefono}}`, e un telefono vuoto resta vuoto; le due varianti osservabili (il
  modello che lo scrive sempre, o il controllo tolto con il modello su `.HrefTel`) muoiono. Ogni file rimesso com'era
  e verificato con `cmp`.
- **Nota di metodo:** il rosso dei test (`prove/rosso.txt`) è stato preso **dopo** aver scritto il codice, mettendo
  degli stub al posto di `PaginaLottoPubblica`/`LeggiFotoLotto` e togliendo le rotte, non prima.
- La D-nuova (Dubbi, 2) è diventata la D-100, approvata da Stefano il 17/11/2026.

## Per il manuale

- Quando invii un lotto, l'oreficeria riceve un link che apre una pagina con le sue riparazioni pronte: il saluto con
  il suo nome, quante sono, il giorno del lotto, il totale da pagare e, per ogni busta, numero, data di pronta, la
  prima foto che lei può vedere, la descrizione, se è ancora da ritirare o già ritirata e il prezzo. In fondo ci sono
  il nome, l'indirizzo e il telefono del negozio, presi da «Impostazioni → Negozio».
- Il totale somma solo i prezzi scritti come importo («40», «40,50»). Le buste con un prezzo come «400/500» o senza
  prezzo sono contate a parte: «(1 senza importo)», «(2 senza importo esatto)».
- Quando l'oreficeria apre la pagina e tutte le buste del lotto sono state ritirate, il lotto risulta chiuso («✓ Lotto
  completato»). Di solito il lotto si chiude già alla consegna dell'ultima busta; la pagina chiude quelli rimasti
  aperti (per esempio i lotti della vecchia app).
- Il link funziona solo dopo «📤 Chiudi e invia». Un link sbagliato, di un lotto non inviato o disattivato
  nell'importazione mostra «Lotto non disponibile». Troppi link sbagliati di fila dallo stesso indirizzo bloccano
  quell'indirizzo per un quarto d'ora.
- Le foto visibili nella pagina sono quelle con «Oreficeria» acceso nella visibilità della foto: spegnendolo, la foto
  sparisce dal link.
- Una busta passata a un'altra oreficeria, tornata al negozio o messa nel cestino sparisce dalla pagina e dalle sue
  foto.

## Dubbi

1. **Niente foto 200 nell'HTTP del banco** (sopra, Banco). Raccomandazione: tenere così (i test Go provano foto,
   miniatura e audit); se si vuole la foto anche nel banco, è un ritocco della catena con gli id dei passi browser da
   rileggere.
2. **D-100 (approvata da Stefano il 17/11/2026): riga d'audit della foto che non si scrive.** Lo standalone, se `audit_public_read/6`
   riceve `{:error, changeset}`, cade nell'`else` di `photo/2` e risponde il 404 «Foto non disponibile»; il Go risponde
   la pagina d'errore 500 (come le foto del cliente, 062, e dell'artigiano, 084, che fanno già così). Un errore SQL vero
   è un 500 in tutte e due. Differenza solo ragionata (non si provoca da HTTP). Raccomandazione: approvare per tutte e
   tre le rotte foto pubbliche. Testo proposto: «D-nn (foto pubbliche, audit non scritto): se la riga
   `repair.public_photo_viewed` non si scrive, le foto di `/r/`, `/artigiano/` e `/lotto/` rispondono la pagina
   d'errore 500 senza byte, invece del 404 «Foto non disponibile» dello standalone; nessuna foto esce senza la sua
   riga.» Nel codice il segnaposto è in `fotoLottoPubblica`.
3. **`/lotto/%2F` (un gettone che decodificato è «/» o finisce con «/»):** la `faccia` del Go risponde il suo «Not
   Found» generico (il mux pulisce il percorso `/lotto//`), lo standalone la pagina del controller con «Lotto non
   disponibile». È del lettore dei percorsi, uguale per `/r/%2F`, e il limite lo conta lo stesso (famiglia `lotto`).
   Misurato nel Go (test), ragionato nello standalone. Raccomandazione: lasciarlo alla tappa che tocca `faccia`; nessun
   gettone vero ha «/».
4. **Bug dello standalone:** nessuno trovato. Da notare, non da correggere: una busta del lotto passata a un'altra
   oreficeria e non consegnata tiene aperto il lotto per sempre (la chiusura guarda tutte le buste del lotto, la pagina
   solo quelle dell'oreficeria), e la pagina dice «Il lotto non è ancora completato.» anche con tutte le visibili
   ritirate. È la regola della slice 212, copiata com'è.
5. **Differenze misurate e ragionate.** Misurato solo il Go (test). Tutto il confronto con lo standalone è ragionato
   dal sorgente a `go-baseline` finché la sessione principale non fa il giro del banco: testi, id, intestazioni, ordine,
   totale, chiusura, foto, rifiuti.
