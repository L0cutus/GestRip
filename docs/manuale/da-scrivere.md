# Da scrivere nel manuale

Elenco una tantum (03/11/2026) dei comportamenti visibili a chi usa GestRip, decisi prima della regola
«Per il manuale» nelle ricevute (D-49…D-64 e la T4.3). Da qui parte la prima slice del manuale (fine
della T4.4). Ogni riga dice dove è decisa; quelle segnate *(da fare)* descrivono come sarà il programma
quando la tappa indicata sarà fatta: nel manuale vanno solo quando il codice c'è.

Si toglie una riga quando è passata in un capitolo; il file sparisce quando è vuoto. Le sezioni «Foto
(T4.3)» e «Stampe (T4.4)» sono passate nei capitoli 03, 04 e 05 con la slice 049, «Ricerca e QR» nel
capitolo 06 con la slice 055, la lista e la galleria (slice 056 e 057) nel capitolo 06 con la slice 059.

## Numero della busta ed etichette *(da fare: messaggi; capitoli 01 e 02)*

- Il numero che vedono tutti, personale e clienti, è quello dell'**etichetta QR**, nella forma
  «0045/26»: in lista, nella scheda, nei PDF, nelle etichette, nei messaggi WhatsApp, Telegram ed
  email e nella pagina di stato del cliente (D-64). Vero per lista e scheda (050, 053; la lista
  è nel capitolo 06), PDF, etichetta della busta e pagina della firma (058: già scritto nei capitoli 04
  e 05), pagina del cliente `/r/` (061) e pagina dei vecchi link `/stato` (064); *(da fare: messaggi,
  T4.6–T4.7)*.
- In «Nuova riparazione» non c'è più il campo «N° Busta»: c'è «Etichetta (0045/26)» con il 📷.
  Scrivendo a mano «45» si intende l'etichetta 0045 dell'anno della data ricezione (D-64, D-71).
- Busta di carta riusata dopo il ritiro: se l'etichetta è già su una busta **ancora aperta**, «Nuova
  riparazione» lo avvisa e si può salvare lo stesso. Nella scheda c'è «Un'altra busta ha la stessa
  etichetta» solo se l'altra è aperta; per quelle chiuse c'è il link «storico di questa etichetta»
  (D-64).

## Pagina del cliente (slice 061, 062, 063, 064, 065) *(da fare: riquadro Telegram, T4.7; il riquadro email è nella sezione della 070)*

- Il link che il cliente riceve apre la pagina «La tua riparazione» con il nome del negozio, il numero
  dell'etichetta, lo stato, le date, la lavorazione, la «Nota del laboratorio» (le note pubbliche, mai
  quelle interne), preventivo e prezzo finale. Non mostra mai nome e telefono del cliente.
- «Tempo indicativo» («Pronta di solito in X-Y giorni dal ritiro») compare solo con la stima dei tempi
  accesa, per una busta aperta e senza una data di consegna scritta dal negozio.
- Un link revocato, scaduto o sostituito da «Genera nuovo link», o di una busta passata a un'altra
  oreficeria, risponde «Riparazione non disponibile», sempre uguale. Dopo 30 link sbagliati in un
  minuto dallo stesso indirizzo, quell'indirizzo è fermo 15 minuti (anche con un link giusto).
- I link già mandati dal vecchio programma continuano a funzionare finché non scadono.
- Foto (062): il cliente vede solo le foto con «Cliente ✓» nella scheda; su una busta di
  un'oreficeria, chi ha il link è l'oreficeria e vede quelle con «Orefice ✓». Si aprono a schermo
  intero con «‹ ›» e «← Indietro». Nascondere una foto la toglie subito anche dal link.
- Preventivo (062): con la busta in «Preventivo» e un importo scritto il cliente vede «✓ Confermo il
  preventivo» e «✗ Non procedo» (che chiede conferma e un motivo facoltativo). Senza importo vede
  «Preventivo in corso di definizione». Confermare porta la busta in «In lavorazione»; rifiutare non
  cambia lo stato: decide il negozio. Il cliente può rispondere una volta sola.
- Nella scheda (062) il riquadro «🤝 Preventivo» dice la risposta con data e ora, il motivo del rifiuto
  e la storia. «Revoca» annulla la risposta (resta nella storia): dopo la revoca di una conferma la
  busta resta in lavorazione, e per far rispondere di nuovo il cliente va rimessa in «Preventivo»;
  dopo la revoca di un rifiuto, una busta annullata torna in «Preventivo».
- La risposta del cliente compare nella lista con il filtro delle notifiche; l'avviso Telegram al
  negozio arriva con Telegram (T4.6).
- Conversazione (063): sotto le foto, «💬 Lascia un messaggio» con i messaggi dal più vecchio (🏪 il
  negozio, 👤 il cliente o l'oreficeria, 👤 con il segno di WhatsApp per quelli trascritti) e il campo
  per scriverne uno (massimo 1000 caratteri, 10 all'ora per busta). Con la pagina aperta i messaggi
  nuovi arrivano da soli ogni 30 secondi, con un bip e (se il telefono lo permette) una notifica;
  senza, basta ricaricare. Una busta consegnata, annullata o chiusa non mostra la conversazione e non
  riceve messaggi.
- Nella scheda (063) il riquadro «💬 Conversazione con il cliente» (o «con l'oreficeria») mostra tutti i
  messaggi; aprire la scheda li segna letti. «Rispondi al cliente» → «Invia 🏪» scrive la risposta, che
  il cliente vede sulla sua pagina. Per ora non parte nessun avviso al cliente (WhatsApp, Telegram):
  arriva con i canali (T4.6, T4.7). Un messaggio arrivato mentre la scheda è aperta si vede ricaricando.
- La campanella 🔔 (063) in alto su ogni pagina conta i messaggi dei clienti non letti e le risposte ai
  preventivi non lette, con le cinque più recenti e «Vedi tutte →» (la lista con le notifiche). Si
  aggiorna cambiando pagina; il numero compare anche nel titolo della scheda del browser.
- Vecchi link del programma Flask (064): `/stato/<busta>/<anno>` con la password breve (`?pw=`) o con il
  link firmato continuano ad aprire la pagina del cliente, ridotta: niente preventivo da approvare, solo
  le foto con «Cliente ✓». Su una busta di un'oreficeria il vecchio link (che può avere in mano il
  cliente finale) non mostra prezzi, conversazione né foto. La password si può scrivere con spazi o
  minuscole; il numero anche con gli zeri davanti («0046/26»).
- Dopo «Revoca link» o «Genera nuovo link» il vecchio link non apre più niente: chi ha la password
  giusta legge «Link non più attivo. Chiedi a <negozio> il link nuovo della tua riparazione.», chi
  l'ha sbagliata «Riparazione non disponibile». Dopo 10 tentativi sbagliati in 15 minuti dallo stesso
  indirizzo, quell'indirizzo è fermo 15 minuti.
- I vecchi link firmati funzionano solo se all'installazione è stato copiato il segreto della vecchia
  app (capitolo dell'amministrazione, *da fare con l'installer, T5*); senza, funzionano solo quelli con
  la password breve.
- «👁 Anteprima cliente» (065), l'ultimo pulsante delle azioni della scheda: apre la pagina come la vede
  chi ha il link (il cliente, o l'oreficeria per le sue buste, con le foto che vede lei), sotto la barra
  «👁 Anteprima: così la vede il cliente. Nulla qui viene inviato.» con «← Indietro» verso la scheda. I
  pulsanti e i campi sono spenti: dall'anteprima non si manda niente e non si segna letto niente.
  Funziona anche con il link revocato o scaduto. Non c'è sulle buste consegnate. L'apertura resta nel
  registro delle attività (titolare e dipendente).
- Vecchio portale delle oreficerie (065): i vecchi indirizzi `/m/…` mostrano «Il portale non è più
  attivo.» con il nome e il telefono del negozio; un telefono che aveva installato il portale come app
  ne cancella le pagine salvate.
- Informativa privacy (065): il link «Informativa privacy» in fondo alla pagina del cliente apre
  l'informativa con i dati del negozio (ragione sociale, P.IVA, sede e contatti presi dalle impostazioni
  del negozio, solo quelli scritti). «🖨 Stampa per esporre in negozio» la stampa su un foglio A4 con il
  QR che porta alla pagina *(il testo da riallineare a cosa fa il Go: vedi i Dubbi della 065)*.

## Avvisare il cliente su WhatsApp (slice 066, 067, 068) *(da fare: Telegram, T4.7)*

- «📲 Avvisa cliente» nella scheda (o «📲 Avvisa oreficeria» per una busta di un'oreficeria) e il 📲 di
  ogni busta aperta nella lista aprono lo stesso riquadro: in alto «WhatsApp» con il numero, sotto il
  messaggio già scritto, che si può correggere, e «Invia» e «Annulla». Aprire e annullare non registrano
  niente.
- Il testo lo sceglie GestRip: preventivo, pronta per il ritiro, compro oro pronta per la firma, primo
  messaggio con il link per seguire la riparazione, o aggiornamento. Su una busta di un'oreficeria il
  messaggio va sempre all'oreficeria, mai al cliente finale.
- «{link}» nel testo diventa il link della busta solo quando si preme «Invia».
- «Invia» registra il messaggio e apre WhatsApp con il testo già pronto; GestRip intanto torna alla
  lista con i filtri di prima. Il messaggio va comunque mandato a mano da WhatsApp.
- L'avviso di pronta per il ritiro si manda una volta sola: la scheda mostra «📲 Avvisato il …». Per
  mandarlo di nuovo c'è «Azzera l'avviso» (chiede conferma; la storia resta).
- Dopo «Rispondi al cliente» si apre lo stesso riquadro, con il messaggio di aggiornamento.
- Se il cliente (o l'oreficeria) non ha un numero, il riquadro lo dice e non c'è niente da mandare; nella
  scheda il pulsante non c'è.
- Nei messaggi il numero della busta è scritto senza «N.»: «45/26», o «0045/26» con l'etichetta (D-82).
- Nella scheda, sotto «Rispondi al cliente», «📨 Invii registrati (N)» si apre con un tocco e mostra tutto
  quello che è stato registrato per la busta, dal più recente: canale, ora, numero e testo. «Registrato» vuol
  dire che WhatsApp è stato aperto con quel messaggio, non che il cliente l'abbia ricevuto o letto.
- «Togli dalla lista» (chiede conferma) toglie un messaggio preparato e mai mandato; non cambia niente su
  WhatsApp. L'avviso di ritiro non si toglie da qui: si azzera con «Azzera l'avviso».
- Se GestRip non ha l'indirizzo pubblico configurato, il riquadro lo dice: un messaggio con {link} non parte
  finché non si aggiunge «indirizzo_pubblico» nel file di configurazione e si riavvia (D-46). Dalla 069c il
  riquadro aggiunge «Togli {link} dal testo per mandarlo senza link.».
- Impostazioni › «Messaggi WhatsApp» (solo il titolare): i testi dei messaggi, WhatsApp, Telegram ed email,
  con i segnaposto che ogni testo può usare e un'anteprima con dati inventati. «Salva il testo» salva; un
  segnaposto che il testo non può usare (per esempio «{password}») non si salva e la pagina dice quale.
  «Ripristina predefinito» (chiede conferma), o una casella vuota, rimette il testo di fabbrica.
- Slice 068: nel modulo di modifica di una busta pronta (con il numero del cliente o dell'oreficeria) c'è
  «📲 Salva e avverti cliente»: salva come «Salva modifiche» e subito apre il riquadro dell'avviso di ritiro,
  con il prezzo appena scritto. Se il salvataggio non va (per esempio la descrizione vuota) il modulo resta lì
  con l'errore e il riquadro non si apre.
- Slice 068: dal pannello del QR, «✅ Valutata» di una scheda compro oro registra subito l'avviso di ritiro e
  mostra «↗ Apri WhatsApp» (con il messaggio già pronto) e «👁 Consultazione». WhatsApp si apre solo toccando il
  link; GestRip intanto torna alla lista. Se l'avviso non può partire lo stato passa lo stesso a «Valutata» e
  la lista dice perché: «(no WA, telefono mancante)», già avvisata, in un lotto aperto, un testo da
  correggere, o l'indirizzo pubblico mancante.
- Slice 068: le righe scritte senza JavaScript nella descrizione, nelle note e nella risposta al cliente si
  salvano come quelle scritte con JavaScript (niente da sapere per chi usa GestRip).

## Avvisi email (slice 069, 069b, 069c) *(i riquadri della pagina del cliente: sezione della 070, sotto)*

- Slice 069: GestRip sa mandare le email con il server di posta del negozio (STARTTLS, TLS diretto o un relay
  locale senza password), ma il canale email resta spento finché il titolare non lo configura e lo accende.
- Slice 069b, Impostazioni › «Provider» (solo il titolare): il riquadro «Posta SMTP» con server, porta,
  sicurezza, utente, password, indirizzo e nome del mittente, e «Configurato» o «Non configurato». La password
  non si rivede mai: la casella vuota vuol dire «tieni quella salvata» (lo dice il segnaposto «salvata — lascia
  vuoto per non cambiarla»); per toglierla c'è la spunta «Cancella le credenziali salvate (utente e password)».
  Gli errori compaiono sotto i campi (per esempio «Al massimo 65535.» per la porta, «serve una password per
  questo utente», «le credenziali non viaggiano su una connessione in chiaro»).
- «Invio delle email» (solo GestRip Go, D-03): sotto il modulo, «Spento» o «Acceso». Dopo l'import è spento:
  «Accendi l'invio delle email» lo accende solo con la posta configurata e senza email rimaste in coda. Le
  email rimaste in coda dal programma di prima (D-27) si vedono una per una (destinatario, oggetto, testo con i
  link di conferma nascosti, tentativi) con «Manda adesso» (un solo invio, subito: dice se il server l'ha
  presa o il codice d'errore) e «Scarta» (chiede conferma; l'email non partirà più). Mai tutte insieme.
  «Spegni l'invio delle email» lo rispegne: le email restano in coda e, per riaccendere, vanno di nuovo
  guardate una per una.
- «✉ Invia messaggio di prova» (slice 069c): manda subito una email di prova all'indirizzo del mittente stesso
  (mai a un cliente), «GestRip — prova del provider di posta», **anche con l'invio delle email spento**: serve a
  controllare i dati del server prima di accendere. La pagina dice «Messaggio di prova accettato da Posta SMTP:
  guarda se è arrivato nella casella del mittente.» o, se non è partito, il codice dell'errore (per esempio
  «smtp:535», credenziali rifiutate). La prova non passa dalla «Coda uscente».
- La «Coda uscente» (con «↻ Aggiorna») mostra le email e il loro stato: «In coda», «Accettato dal provider» (il
  server l'ha presa, non vuol dire che sia stata letta) o «Fallito», con i tentativi e l'ultimo errore.
- Slice 069c: nelle email rimaste in coda da guardare c'è anche il numero della busta («Busta 0045/26» o «Busta
  N. 45/26»). Se l'invio è acceso ma i dati del server non bastano più (per esempio il server cancellato), la
  pagina lo dice: «L'invio delle email è acceso ma Posta SMTP non è configurato: nessuna email parte…».
- Anche la configurazione portata dallo standalone con l'import resta spenta finché non la si accende (D-03).
- Per il capitolo dell'amministrazione: la password del server di posta non si vede mai sulle pagine né nei
  registri; un errore dice solo il codice del server (per esempio «smtp:535», credenziali rifiutate) e GestRip
  fa cinque tentativi (il secondo dopo 10 secondi, poi dopo 1 minuto, 5 minuti e 30 minuti) prima di dare il
  messaggio per fallito.

## Avviso email sulla pagina del cliente (slice 070, 070b)

- Con l'invio delle email acceso (Impostazioni › «Provider») e l'indirizzo pubblico configurato, la pagina del
  cliente (il link `/r/…` e il vecchio link `/stato/…` di una busta del cliente) ha il riquadro verde «📧
  Preferisci un avviso via email?»: il cliente scrive la sua email, spunta il consenso (con il link
  all'informativa) e preme «Attiva avviso email →». Con l'invio spento il riquadro non c'è (D-16: il vecchio
  programma lo mostrava anche senza posta).
- Gli arriva un'email «Conferma avviso email — busta 0045/26» con il link di conferma (vale 48 ore). Il link apre
  una pagina con il pulsante «Conferma»: solo il pulsante conferma, così un programma che apre i link delle
  email da solo non conferma niente. Dopo, «Email confermata ✅» e «Torna alla tua riparazione».
- Finché non conferma, il riquadro dice «📬 Conferma la tua email» e cosa è successo all'email (in invio,
  scritta, non riuscita, non partita) con «Reinvia conferma». Dopo la conferma: «✅ Avviso email attivo» con
  «Disattiva» (chiede conferma).
- Ogni email al cliente finisce con «Per disattivare gli avvisi: <link>»: il link apre una pagina con il
  pulsante «Disattiva» (vale 10 anni).
- Su una busta di un'oreficeria il riquadro è dell'oreficeria (dal suo link `/r/…`); il vecchio link `/stato/…`
  di una busta di un'oreficeria non ha il riquadro. Una busta consegnata o annullata non ha il riquadro.
- Al più 6 richieste e 6 «Disattiva» all'ora per busta: oltre, «Troppe richieste nell'ultima ora…».
- I testi dell'email di conferma sono quelli di Impostazioni › «Messaggi WhatsApp» › email (slice 067).
- Nell'anteprima del cliente il riquadro si vede come lo vede il cliente, ma i pulsanti sono spenti.
- **Vecchi link del programma di prima (D-02):** i link di conferma e di disattivazione mandati dallo standalone
  non valgono più. Un vecchio link di conferma dice «Link non valido o scaduto» (si chiede un link nuovo con
  «Reinvia conferma»); un vecchio link di disattivazione apre una pagina «Disattiva gli avvisi email» dove il
  cliente scrive la sua email: se è iscritta, gli arriva un'email con un link nuovo per disattivare. La pagina
  risponde sempre allo stesso modo, così nessuno può scoprire quali email sono iscritte.
- **Email «pronta per il ritiro» (slice 070b):** quando la busta passa a «Pronta» (dalla scheda, dalla lista con
  «✓ Pronta» o dal menu, dal pannello del QR con «✅ Pronta», o riaprendo una busta consegnata in «Pronta»), al
  cliente con l'email confermata parte da sola un'email «La tua riparazione è pronta — busta 0045/26» con il link
  della sua pagina. Parte **una volta sola**: se la busta torna in lavorazione e poi di nuovo pronta, non ne parte
  un'altra; ne parte un'altra solo se la prima non è riuscita (la coda l'ha data per fallita). Va a chi l'ha
  chiesta: su una busta di un'oreficeria all'oreficeria; se la busta nel frattempo è passata a un'altra
  controparte, l'email non parte. Non parte nemmeno con l'invio delle email spento: al prossimo «Pronta» con
  l'invio acceso sì. I testi sono quelli di Impostazioni › «Messaggi WhatsApp» › email (slice 067).
- **Nella scheda (slice 070b):** il riquadro «📧 Avviso email» (sotto «🔔 Avviso di ritiro») mostra l'indirizzo
  che il cliente ha chiesto, «In attesa di conferma» o «Confermato», e l'email di pronta: «partirà da sola al
  passaggio a «pronta», una volta», poi «In coda dal…», «Accettato dal provider il…» (il server di posta l'ha
  presa: non vuol dire che sia arrivata), «Fallito il… Riparte al prossimo passaggio a «pronta»», «non accodata:
  …» con il motivo (per esempio un testo dell'email non valido nelle Impostazioni), o «inviata dal gestionale
  precedente il…» per quelle mandate dallo standalone, «tolta dalla coda dal titolare» per un'email scartata
  dalla pagina dei provider (non ne parte un'altra). Con l'email non disponibile (invio spento o indirizzo pubblico
  mancante) la scheda dice che l'email di pronta non partirà. Il riquadro lo vedono titolare e dipendenti.
- **Pagina dei provider (slice 070b):** con l'invio delle email acceso ma senza l'indirizzo pubblico, la pagina
  dice che il riquadro «avvisami via email» della pagina del cliente non compare, che l'email di pronta non
  parte, e cosa scrivere nel file di configurazione.

## Galleria (slice 057) *(da fare: impostazioni)*

- La grandezza delle foto della galleria (piccola, media, grande) si sceglie nelle impostazioni del
  negozio *(da fare: la pagina delle impostazioni del Go non la ha ancora; oggi vale quella portata
  dallo standalone con l'import, media se non c'è)*. Il capitolo 06 dice solo che è un'impostazione
  del negozio.

## Import dallo standalone (slice 057, per il capitolo dell'amministrazione)

- Dopo l'import le buste con il numero scritto con gli zeri davanti («0046/26») hanno il numero senza
  zeri («46/26»), e i codici QR scritti con spazi o con «rip-» minuscolo sono scritti nella forma
  giusta («RIP-2026-0045»). Il report dell'import dice quante e quali buste (D-76, D-77).
- Se il numero senza zeri era già di un'altra busta, quella busta tiene il numero con gli zeri e il
  report lo segnala a parte: va guardata a mano.

## Accesso (T1, da recuperare)

Dalla ricevuta della slice 048 (`docs/slices/048-cookie-30-giorni.md`), da mettere nel capitolo
«Amministrazione» o in uno sull'accesso quando si recuperano le tappe T1-T4.1.

- L'accesso dura 30 giorni dal login, anche se chiudi il browser, l'app o spegni il telefono.
- Usare GestRip non allunga i 30 giorni: dopo si rifà il login.
- «Esci» chiude subito l'accesso su quel telefono o computer.
- Se una pagina era aperta prima di entrare (per esempio in un'altra scheda), dopo il login va
  ricaricata prima di premere un pulsante (D-67).

## Non per il manuale

D-50, D-51 (messaggi d'errore del server per le foto), D-55 (pulizia dopo un import), D-56 (indirizzi
non validi): dettagli tecnici, non cambiano l'uso di tutti i giorni.
