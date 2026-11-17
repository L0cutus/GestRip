# 087 — Profilo del negozio in `/settings/shop` (T4.9, seconda)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone_web/live/shop_live.ex` (evento `save`, sezione
  «Profilo negozio» di `render/1`), `lib/gestrip_standalone/online_sales.ex` (`get_shop_profile/0`, `shop_name/0` e `/1`,
  `put_shop_profile/2`), `online_sales/shop_profile.ex` (`changeset/2`, `normalize_tax_code/1`), la migrazione
  `20261027200000_add_shop_tax_code`; test `shop_live_test.exs`, `shop_tax_code_test.exs`, `online_sales_test.exs`,
  `settings_navigation_test.exs`.
- **Voci:** ADM-02 (portata; si chiude con l'esito verde del banco). Decisione di Stefano del 15/11/2026 (risposta H):
  ADM-02 si chiude in questa slice; ADM-03, ADM-05 e ADM-07 restano alla T4.15. La riga T4.15 del PIANO è corretta (sotto,
  «Controlli»).
- **Lavoro:** sottoagente d'implementazione (Sonnet 5.5), test rossi prima, nessuna mutazione ancora (le fa la revisione). Il
  banco non è stato lanciato: lo fa la sessione principale quando c'è memoria (sotto, «Banco»).

## Cosa c'è

- **Dominio** (`internal/buste/profilo.go`, che già leggeva il profilo per `/m/` e `/privacy`): `ProfiloPerModulo` (i 17
  campi come testo, NULL e senza profilo vuoti) e `SalvaProfilo` (= `put_shop_profile/2`: legge la riga `default` nella
  transazione, applica il changeset, la crea o riscrive i soli campi cambiati, scrive `shop_profile.updated` nella stessa
  transazione). Il changeset di `ShopProfile` è portato com'è: `cast` (campo che non arriva = non cambia; vuoto o di soli spazi
  Unicode = NULL; è un cambio solo se diverso dal salvato), `trim_strings` solo sui cambi, `validate_required` di nome e
  garanzia sul valore che resterebbe, `validate_length` in grafemi solo sui cambi (nome, ragione sociale, indirizzo, email,
  email vendite, sottotitolo, Instagram, Facebook e sito 255; CAP e provincia 20; città 120; telefono 80; identificativo
  fiscale 40; orari 500; garanzia e codice fiscale senza limite), codice fiscale senza spazi e in maiuscole poi `\A(?:[A-Z0-9]{16}|[0-9]{11})\z`.
  Un rifiuto è `*ProfiloRifiutato` (errori per campo e valori da rimettere nel modulo, come `input_value/3`: il cambio, il testo
  scritto se non è un cambio, il salvato se il campo non è arrivato). Nessuna migrazione: la tabella `profilo_negozio` c'è dalla 0007.
- **Pagina** (`internal/web/negozio.go`, `pagine/negozio.html`, `statici/app.css`): `GET /settings/shop` e `POST /settings/shop`
  (`rottaConGuscio`, `soloTitolare`). Titolo «Impostazioni del negozio», link «Anagrafiche», «Profilo negozio» con la frase, il
  modulo `#shop-form` con i 17 campi nell'ordine e con le etichette dello standalone (la città con l'accento, «Città»: D-97), id
  `shop_profile_<campo>` e nomi `shop_profile[<campo>]` uguali ai suoi, nome e garanzia `required`, le due email `type="email"`,
  il codice fiscale `autocomplete="off"`, la garanzia in un `textarea`, «Salva impostazioni». Riuscito: 302 alla pagina con
  «Impostazioni salvate. I documenti emessi restano invariati.»; rifiutato: la pagina ridisegnata (200, nessun messaggio in alto)
  con `<p class="field-error">` sotto il campo («Campo obbligatorio.», «Troppo lungo (massimo N caratteri).», «codice fiscale non
  valido: 16 caratteri, o 11 cifre per una società»). La scheda «Negozio» (`schedaDelModello`) è la prima delle impostazioni, quindi
  `/settings` ci va (la 015 lo aspettava); il link «Profilo negozio» della pagina delle anagrafiche, che aspettava questa rotta,
  ora c'è per il titolare.
- **Chi legge il profilo** lo legge dalla tabella e vede il salvato (provato, sotto): `/m/` e `/privacy` (`LeggiProfiloNegozio`),
  il nome della lista e delle schede (`nomeNegozio`, `Scelte`), ricevuta (nome e sottotitolo), scheda PDF, report, i messaggi
  (`leggiProfiloAvviso`: `{negozio}`, `{citta}`, `{orari}`, `{social}`). **Non** lo legge il manifest (nome) e il titolo sotto
  l'icona dell'app (`apple-mobile-web-app-title`): dicono ancora «Negozio» (Dubbio 2).
- **Tabelle aggiornate:** permessi (`ruoli_test.go`: `GET` e `POST /settings/shop`, `soloTitolare`), rotte con il guscio e schede
  (`guscio_test.go`), `busteFinte` e interfaccia `Buste` (`nuova.go`), caselle di più righe (`a_capo_test.go`: la garanzia).
- **Banco** (non lanciato): HTTP `negozio` (la pagina del titolare: etichette, nomi, tipi, `required`, `autocomplete`, il nome
  «Negozio» e la garanzia predefinita dell'import) e `negozio-dipendente` (la pagina rifiutata, 302 a `/`); browser
  `profilo-negozio` (sotto, «Banco»).

## Scelte

1. **Stessa convenzione delle anagrafiche (076, 086):** il Go non ha LiveView, quindi la POST va alla pagina stessa
   (`POST /settings/shop`, come `/settings/templates`): riuscito 302 con il messaggio, rifiuto 200 con i valori. Nello
   standalone l'esito resta nella stessa pagina (flash e modulo ricostruito dal salvato): dopo il ritorno è la stessa cosa.
2. **Il changeset di Ecto com'è, non una validazione «simile».** I casi limite si vedono nei dati: un valore uguale al salvato
   non si ripulisce (un import può lasciare spazi ai bordi: salvare il modulo senza toccarlo non li cambia); un campo che non
   arriva (invio forgiato) non cambia; senza cambi non c'è UPDATE e `aggiornato_il` resta (la riga d'audit si scrive lo stesso);
   `String.upcase/1` scioglie «ß» in «SS» e le legature, quindi anche qui (`strings.ToUpper` non lo fa) per non rifiutare un
   codice che lo standalone accetta.
3. **Nome e garanzia vuoti non si «ripuliscono» dal modulo:** dopo un rifiuto un campo che non cambia (per esempio nome di soli
   spazi senza un profilo, dove NULL = NULL) mostra il testo scritto, uno che cambia (nome svuotato su un profilo salvato) mostra
   il cambio ripulito, cioè vuoto: è `input_value/3` del changeset, provato in `TestProfiloNegozioValoriDopoUnRifiuto`.
4. **Le righe della garanzia** arrivano con «\r\n» da un modulo senza JavaScript e si salvano con «\n» (`aCapoUnix`): nello
   standalone la casella è di LiveView e manda «\n» (decisione dell'11/11, 069c); la casella ha il nome scritto per esteso nel
   modello, come vuole `TestCaselleDiPiuRigheNormalizzate`.
5. **Moduli forgiati e guasti** (non osservabili dalle pagine): un modulo senza `shop_profile[…]` è il catch-all di
   `handle_event`: «Richiesta non riconosciuta: nessuna impostazione salvata.»; `shop_profile` come testo o lista (nello
   standalone il processo LiveView cade e si ricollega) dà la stessa frase; un campo scritto come lista o mappa è l'errore di
   cast «Valore non valido.» sul campo (come in Ecto). Un guasto del database mostra «Impostazioni non salvate: riprova.» (la frase
   delle sezioni gemelle dello standalone) con i valori scritti e una riga nel registro («profilo del negozio non salvato», senza il testo); se il profilo non si
   legge la pagina è quella d'errore (500) con «profilo del negozio non letto». Nello standalone invece un guasto del database
   fa cadere la pagina LiveView (e un audit che non si scrive rende il changeset dell'audit come modulo, nessuna schermata
   sensata): il Go mostra il messaggio, come la Scelta 5 della 086; il banco non può osservarlo.
6. **Solo la sezione del profilo:** la pagina dello standalone ha altre cinque sezioni (operatività ADM-03, stima dei tempi
   ADM-04, logo e icone ADM-05, colori della barra ADM-06, accessi all'installazione); nessuna è in questa slice, e la pagina
   non le nomina. La pagina HTTP del banco non le confronta.
7. **`value` dei campi NULL:** lo standalone omette l'attributo `value` per un campo NULL (`normalize_value/2` dà nil); il Go
   scrive `value=""`. Per chi usa la pagina è identico; lo scenario HTTP confronta il `value` solo del nome.

## Per il manuale

- In Impostazioni la prima scheda è «Negozio» (la vede solo il titolare): ci sono i dati del negozio che finiscono su ricevute,
  schede, report, messaggi ai clienti e informativa privacy.
- Il nome del negozio e le condizioni di garanzia sono obbligatori; tutto il resto (sottotitolo, ragione sociale, identificativo
  fiscale, codice fiscale, indirizzo, CAP, città, provincia, telefono, email, email vendite, orari, Instagram, Facebook, sito) è
  facoltativo: un campo lasciato vuoto si toglie.
- Si preme «Salva impostazioni»: gli spazi ai lati si tolgono e compare «Impostazioni salvate. I documenti emessi restano
  invariati.» (i documenti già emessi non cambiano).
- Se manca il nome o la garanzia, o un campo è troppo lungo (nome 255 caratteri, orari 500, CAP 20…), l'avviso è sotto il
  campo e non si salva niente.
- Il codice fiscale è di 16 lettere e cifre (persona) o 11 cifre (società): spazi tolti e maiuscole; se si lascia vuoto vale la
  partita IVA.
- Per ora il nome sotto l'icona dell'app installata resta «Negozio»; e in questa scheda c'è solo il profilo (le altre
  impostazioni arrivano con le prossime slice).

## Prove

- **Rosso prima del codice** (`to-be-deleted-after-use/slice-087/prove/`): `rosso-dominio.log` (la compilazione: i tipi e le
  funzioni non c'erano), poi con le sole firme come segnaposto `rosso-dominio-con-stub.log` (12 test del dominio rossi
  sull'asserzione) e `rosso-web.log` (10 test della pagina rossi: rotte e markup non c'erano).
- **Dominio** (`internal/buste/profilo_negozio_test.go`): creazione (campi ripuliti, il resto NULL, istanti, audit con attore),
  secondo salvataggio (stessa riga, `creato_il` fermo, `aggiornato_il` nuovo, due righe d'audit), obbligatori (vuoti, spazi
  Unicode, assenti; due errori insieme; nessuna scrittura né audit; su profilo salvato resta com'era; campo non arrivato =
  non cambia), lunghezze di tutti i campi (il massimo passa con gli spazi ai bordi, uno in più no; grafemi con accento
  combinato; garanzia di 20.000 caratteri; due errori insieme), facoltativi (ripuliti, NULL, svuotamento, campo non arrivato),
  solo i cambi passano dal controllo (valori salvati con spazi o oltre il limite, riscritti uguali), codice fiscale (normalizzato,
  società, sette valori rifiutati con il messaggio, «ß», svuotato), valore non testo, valori dopo un rifiuto, `ProfiloPerModulo`,
  audit nella transazione (trigger `RAISE(ABORT)` alla creazione e alla modifica), e che chi già legge il profilo
  (`LeggiProfiloNegozio`, `nomeNegozio`, `leggiProfiloAvviso`, ricevuta, scheda PDF, report) vede i valori salvati.
- **Pagina** (`internal/web/negozio_test.go`): markup della pagina vuota (scheda aperta, titolo, link, frase, modulo con token,
  17 campi nell'ordine con etichette, attributi, `required`, `autocomplete`, bottone, nessun errore, nessuna delle altre sezioni),
  del profilo salvato (escape di virgolette, `<` e `&`, a capo della garanzia, NULL), salvataggio (302, messaggio una volta sola,
  valori nei campi, righe del database, una riga d'audit con l'attore), rifiuti (obbligatori, valori scritti, profilo salvato
  intatto, nessun audit), lunghezze e codice fiscale (messaggi sotto il campo, svuotarlo lo toglie), garanzia con «\r\n»,
  moduli forgiati (cinque forme, lista, campo ripetuto = l'ultimo, senza token 403), dipendente (GET e POST forzata → `/` con
  «Operazione riservata al titolare.», senza accesso → `/login`), scheda «Negozio» prima delle altre e `/settings` → `/settings/shop`,
  link «Profilo negozio» (titolare sì, dipendente no), il salvato letto da home, ricevuta e informativa pubblica, guasto di lettura e
  di scrittura con la riga nel registro e nessun testo del profilo. Aggiornati: `TestIngressoImpostazioni`,
  `TestSchedeCresconoConLeRotte`, `TestAnagraficheProfiloNegozio` e `TestPaginaAnagrafiche` (la pagina ora c'è davvero).
- **Banco (solo controlli senza istanza):** `go test ./tools/banco` (scenari letti, voci, attese, `atteso_dati` ammessi solo sul
  passo `/settings/shop`, conteggio e voci degli scenari browser, sul vuoto solo la pagina) verde; `node --check` su `banco.mjs`.

## Mutazioni

Revisore Opus, su una copia del repo: 34 mutazioni (dominio, pagina, modello, rotte), 33 compilate, **29 uccise, 4
sopravvissute**. Tre si sono chiuse con un test nuovo del dominio (`TestProfiloNegozioSenzaCambiNienteUpdate`: valore uguale
al salvato dopo gli spazi tolti, codice fiscale uguale dopo la normalizzazione, secondo salvataggio identico: `aggiornato_il`
non cambia e c'è una riga d'audit in più); riapplicate a mano una alla volta, tutte e tre lo fanno cadere
(`trim-uguale-resta-cambio`, `cf-uguale-resta-cambio`, `case len(cambi) > 0` → `>= 0`). La quarta (`shop_profile[]`
accettato come mappa) è equivalente: dà lo stesso «Richiesta non riconosciuta», niente da fare.

## Revisione

Revisore Opus indipendente, due fasi. **Fase 1:** nessun ALTO; 1 MEDIO (il nome nel manifest e sotto l'icona, deciso: va con
ADM-07 nella T4.15, Dubbio 2) e 4 BASSI, chiusi (la ricevuta del test di dominio controlla anche il sottotitolo; la ricevuta
tolta dal test della pagina, che non può leggere il PDF; lista o mappa dello stesso campo che vincono sul testo in qualunque
ordine, con test; l'errore del ripristino del banco in `ripristino.errore`). **Fase 2:** nessun rilievo sulle correzioni.

## Controlli

`scripts/check` verde (16/11/2026, log in `to-be-deleted-after-use/slice-087/prove/check.log`, rilanciato dopo ogni giro di
correzioni; il primo giro era caduto per `staticcheck` e per un `vet` su una variabile di un test, corrette). Copertura: voci 353, chiuse 42, rosse 0, parziali 164,
aperte 136, fuori v1 11 (senza l'esito del banco: ADM-02 passa da aperta a parziale).

PIANO condiviso (`~/Condivisi/GestRip-Go/PIANO.md`): nella riga **T4.15 Amministrazione** il titolo diceva ancora «profilo del
negozio» fra le cose da fare, mentre le voci (`ACC-06…14; ADM-03, 05…07, 23`) già non avevano ADM-02. Tolto «profilo del negozio, » dal
titolo e aggiunta, dopo «impostazioni», la nota «(il profilo del negozio, ADM-02, è chiuso nella slice 087 della T4.9)»;
nient'altro è cambiato (copia di prima in `to-be-deleted-after-use/slice-087/PIANO.prima.md`). La riga T4.9 aveva già ADM-02 e
la T4.12 la dipendenza da ADM-02.

## Banco (sessione principale)

Con il «memoria libera» del coordinatore e la guardia sotto 10 GB.

- **`s087a`** (16/11/2026): HTTP 78 scenari, 1279 passi, 2 inattese, 0 errori; browser 25 scenari, 50 viste, 10 inattese e 1
  errore. Le 2 inattese dell'HTTP erano la frase a capo nello standalone dello scenario `negozio` (il paragrafo del profilo va a
  capo nel sorgente: corretta, ora si cerca la seconda riga). Le 10 del browser: 4 sono la D-93 (campi nome e garanzia dopo il
  rifiuto, due viste: sotto, Dubbio 4), le altre e l'errore sono il 500 «Database busy» dello standalone in
  `sollecito-artigiano` sul telefono (nota 8 di `docs/domande-aperte.md`), che non dipende dal profilo (ripristino riuscito in
  tutte e due le viste: `ripristino.uguale`, `ripristino.nome` e `registro.shop-profile-updated` = 2 senza differenze).
- **Giro finale: la D-93 è approvata** (Stefano, 16/11/2026). Fino ad allora la slice era stata committata solo in locale, senza
  push (regola «Slice senza banco» in `docs/SVILUPPO.md`); ora il giro finale unico può fare tutto con le quattro righe della D-93.

## Misura

Implementatore Sonnet 5.5, ragionamento alto: circa 27 minuti il primo giro, poi tre giri di correzioni (circa 5 e 10 minuti, e
l'ultimo con le mutazioni chiuse e la ricevuta); circa 480k token del sottoagente. Revisione Opus: circa 155k token. Giri del
banco: 1 (il finale è nel giro unico, con la D-93 approvata).

## Dubbi

1. **`/settings` e `/settings/shop`: deciso dal coordinatore.** Nello standalone sono la stessa pagina; nel Go `/settings` resta il
   302 alla prima scheda (ora «Negozio»). Il 302 resta; nell'inventario ADM-01 perde la parola «temporaneo».
2. **Il nome nel manifest e sotto l'icona dell'app: deciso dal coordinatore.** Il manifest e `apple-mobile-web-app-title` dicono
   ancora «Negozio»; il nome dal profilo lo porta ADM-07 nella T4.15. ADM-02 si chiude con questa eccezione, scritta qui e nel
   manuale.
3. **«Citta» senza accento** (anche «Operativita» nelle altre sezioni): tenuta uguale allo standalone, il banco la confronta
   lettera per lettera. Il coordinatore lo chiede a Stefano; se la vuole corretta è una D nuova su un testo.
   **Deciso da Stefano il 16/11/2026:** «Città» con l'accento nel Go (**D-97**); lo standalone resta «Citta». Correzione fatta nel
   commit successivo (sezione «Correzione del 17/11/2026 (D-97)», in fondo). «Operativita» e le altre sezioni: la D-97 riguarda
   solo l'etichetta della città del profilo; le altre si decidono quando le sezioni arrivano.
4. **Dopo un rifiuto per nome o garanzia di soli spazi** lo standalone lascia «   » nei campi (LiveView non tocca un campo che
   l'utente ha toccato), il Go ridisegna la pagina dal changeset e li svuota (banco s087a, `rifiuto.campo-nome` e
   `rifiuto.campo-garanzia`, tutte e due le viste). È la **D-93**: quattro righe in
   `bench/deviazioni.json` (scenario `profilo-negozio`, passi 0 e 1). `banco esegui -inventario` controlla che la D-93 sia
   approvata nella tabella «Deviazioni volute» dell'inventario privato (il controllo legge solo l'ID).
   **Deciso da Stefano il 16/11/2026:** D-93 approvata; le quattro righe restano.

## Correzione del 17/11/2026 (D-97)

Stefano ha approvato il 16/11/2026 la **D-97**: l'etichetta del campo della città nel profilo del negozio è «Città» con l'accento
(lo standalone ha «Citta»). Cambiati `internal/web/negozio.go` (etichetta) e il suo test (rosso, poi verde); nel banco l'attesa
dell'etichetta esce dagli scenari `negozio` (HTTP, passo 2, `visibile:shop-form/label@8`) e `profilo-negozio` (browser,
`pagina.etichette`, due viste) e diventa tre righe D-97 in `bench/deviazioni.json`. Il nome del campo (`city`), i valori scritti
e il testo dell'informativa (`portale_privacy.go`, che mostra il valore, non l'etichetta) non cambiano.

### Per il manuale

- Nelle Impostazioni del negozio il campo della città si chiama «Città» (con l'accento).
