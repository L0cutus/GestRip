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

## Pagina del cliente (slice 061, 062, 063, 064, 065) *(da fare: riquadri; T4.6)*

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
  finché non si aggiunge «indirizzo_pubblico» nel file di configurazione e si riavvia (D-46).
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

## Avvisi email (slice 069) *(da fare: la pagina «Provider di invio» e i riquadri, slice dopo e 070)*

- Slice 069: niente di visibile ancora. GestRip sa mandare le email con il server di posta del negozio
  (STARTTLS, TLS diretto o un relay locale senza password), ma il canale email resta spento finché il
  titolare non lo configura e lo accende dalla pagina «Provider di invio» delle impostazioni, che arriva con la
  slice dopo. Anche la configurazione portata dallo standalone con l'import resta spenta finché non la si
  accende (D-03), e le email rimaste in coda nello standalone non partono da sole (D-27).
- Per il capitolo dell'amministrazione: la password del server di posta non si vede mai sulle pagine né nei
  registri; un errore dice solo il codice del server (per esempio «smtp:535», credenziali rifiutate) e GestRip
  fa cinque tentativi (il secondo dopo 10 secondi, poi dopo 1 minuto, 5 minuti e 30 minuti) prima di dare il
  messaggio per fallito.

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
