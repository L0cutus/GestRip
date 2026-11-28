# Da scrivere nel manuale

Elenco una tantum (03/11/2026) dei comportamenti visibili a chi usa GestRip, decisi prima della regola
«Per il manuale» nelle ricevute (D-49…D-64 e la T4.3). Da qui parte la prima slice del manuale (fine
della T4.4). Ogni riga dice dove è decisa; quelle segnate *(da fare)* descrivono come sarà il programma
quando la tappa indicata sarà fatta: nel manuale vanno solo quando il codice c'è.

Si toglie una riga quando è passata in un capitolo; il file sparisce quando è vuoto. Le sezioni «Foto
(T4.3)» e «Stampe (T4.4)» sono passate nei capitoli 03, 04 e 05 con la slice 049, «Ricerca e QR» nel
capitolo 06 con la slice 055, la lista e la galleria (slice 056 e 057) nel capitolo 06 con la slice 059.
Le oreficerie e i lotti (slice 086–093) sono passati nel capitolo 09 con la slice 099; quello che ne resta
per altri capitoli è nelle sezioni «Oreficerie, per i capitoli 01, 02 e 07», «Profilo del negozio (slice
087)» e «Promemoria automatici (slice 094–098)». Il backup e il ripristino (slice 119–131) sono nel capitolo 11 con
la slice 132, scritto dalle note di fine lavoro delle slice: qui non avevano righe; restano solo le figure
della sezione «Backup: figure mancanti».

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

## Pagina del cliente (slice 061, 062, 063, 064, 065, 074c) *(il riquadro Telegram è nella sezione della 072, quello email nella sezione della 070)*

- Il link che il cliente riceve apre la pagina «La tua riparazione» con il nome del negozio, il numero
  dell'etichetta, lo stato, le date, la lavorazione, la «Nota del laboratorio» (le note pubbliche, mai
  quelle interne), preventivo e prezzo finale. Non mostra mai nome e telefono del cliente.
- Il numero è senza «N.» (074c): «0045/26» con l'etichetta, «45/26» senza, come nei messaggi. Le pagine
  del negozio continuano a scrivere «N. 45/26».
- Le date («Accettata», «Consegna prevista», «Pronta dal», «Consegnata») sono scritte come sulla ricevuta,
  per esempio «05/11/2026» (074c); una consegna scritta a parole («fine mese») resta com'è.
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
- (135) Il vecchio link di una busta eliminata non apre mai un'altra busta con lo stesso numero senza
  gli zeri: il link di «0046/26» apre solo la busta che all'import era «0046/26»; se quella busta non
  c'è più, il cliente legge «Riparazione non disponibile». Vale anche per le foto e per i vecchi link
  degli artigiani. *(Per il capitolo 12 «Cestino e conservazione delle foto», 146.)*
- (136) «🗑 Sposta nel cestino», in fondo alla scheda, per titolare e dipendente: chiede «Spostare la busta N nel
  cestino? Resta recuperabile per X giorni.» e torna alla lista con «Busta spostata nel cestino.». La busta sparisce
  da liste, ricerca, contatori, campanella, briefing e promemoria, e il suo link del cliente non si apre più. Il suo
  dettato raccolto si cancella subito e **non torna** con il ripristino; quelli delle buste arrivate già nel cestino
  con il passaggio se ne vanno al primo giro della notte dopo il primo backup verificato. *(Per il capitolo 12
  «Cestino e conservazione delle foto», 146.)*
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
  riquadro aggiunge «Togli {link} dal testo per mandarlo senza link.»; dalla 071 dice «Togli {telegram_optin}…»
  o «Togli {link} e {telegram_optin}…» quando il testo contiene il link Telegram.
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

## Telegram: il bot del negozio (slice 071)

- Impostazioni › «Provider» (solo il titolare), riquadro «Telegram» fra la posta e la coda: «Token del bot» (quello
  che dà BotFather, nella forma «numero:testo») e «Chat di destinazione» (il numero della chat o del gruppo del
  negozio, o «@nome»), con «Salva provider Telegram»; «Configurato» o «Non configurato». Il token non si rivede mai:
  la casella vuota vuol dire «tieni quello salvato» («salvato — lascia vuoto per non cambiarlo»); per toglierlo c'è
  «Cancella il token salvato». Un token o una chat scritti male dicono «Formato non valido.».
- Sotto la chat: «Usa un gruppo o una chat diversa da quella con cui provi il bot da cliente: i messaggi da questa
  chat sono ignorati.».
- «✈ Invia messaggio di prova»: chiede al bot il suo nome e, se risponde, manda subito un messaggio di prova alla
  chat del negozio (mai a un cliente), anche con l'invio Telegram spento. La pagina dice «Bot: @nome» e se il
  messaggio è stato accettato; se il bot non risponde, «Il bot non risponde a getMe (…): niente è stato accodato.»
  (per esempio «telegram:http:401»: token sbagliato).
- «Notifiche Telegram ai clienti» («Accese»/«Spente», «Accendi»/«Spegni»): spente di partenza. Spente, i messaggi
  Telegram per i clienti non partono (quelli già in coda falliscono). Se il bot ha un problema a leggere i messaggi
  (per esempio un webhook attivo sullo stesso token o un'altra installazione che legge), qui compare l'errore con
  l'ora. (La lettura dei messaggi e l'iscrizione dei clienti arrivano con le prossime slice.)
- «Invio Telegram» (solo GestRip Go, D-03): «Spento» dopo l'import; «Accendi l'invio Telegram» lo accende solo con
  il bot configurato e senza messaggi Telegram rimasti in coda dal programma di prima (D-27): quelli si guardano
  uno per uno con «Manda adesso» o «Scarta», come le email.
- Per il capitolo dell'amministrazione: se Telegram dice di rallentare (troppi messaggi), GestRip aspetta quanto
  chiesto prima di mandare gli altri; un messaggio che Telegram rifiuta (testo non valido, chat che non c'è) fallisce
  subito; un cliente che ha bloccato il bot non riceve più niente su quella chat. L'indirizzo del servizio Telegram
  si cambia solo dall'ambiente del servizio (TELEGRAM_API_ENDPOINT), mai dalle pagine.

## Telegram sulla pagina del cliente (slice 072)

- Sulla pagina della riparazione (il link `/r/…` e il vecchio link `/stato/…`), sopra il riquadro dell'email, il
  riquadro azzurro di Telegram: «Vuoi essere avvisato in tempo reale?» con «Attiva su Telegram →». Il pulsante apre
  Telegram sulla chat del bot del negozio con il pulsante «Avvia»: il token dentro il link vale 24 ore (quello dei
  messaggi WhatsApp con {telegram_optin} 60 minuti) e si usa una volta sola. Aprire il link non iscrive ancora
  nessuno: l'iscrizione la fa il bot (sezione sotto).
- Iscritto e con il «sì» dato nel bot, il riquadro dice «Notifiche Telegram attive» con «⚙️ Modifica preferenze»
  (un link nuovo, per esempio da un altro telefono) e «Disattiva» (chiede conferma; spegne le notifiche Telegram di
  quella riparazione per tutte le chat iscritte).
- Il riquadro c'è solo su una riparazione del cliente (non su quelle di un'oreficeria), non chiusa, e solo con
  «Invio Telegram» e «Notifiche Telegram ai clienti» accesi, il nome del bot noto (lo dà «✈ Invia messaggio di
  prova») e l'indirizzo pubblico configurato. Nell'«👁 Anteprima cliente» si vede, spento.
- Al più 6 link all'ora per riparazione dal pulsante e altri 6 dai messaggi WhatsApp; oltre, «⚠️ Troppe richieste
  nell'ultima ora: riprova più tardi o contatta il negozio.». Con Telegram spento il link dei messaggi WhatsApp
  risponde «Servizio notifiche temporaneamente non disponibile.».

## Il bot Telegram del negozio (slice 073)

- Il bot legge i messaggi dei clienti solo con «Invio Telegram» e «Notifiche Telegram ai clienti» accesi tutti e due
  (pagina «Provider di invio») e il bot configurato. Spento uno dei due, il bot non legge e non risponde a nessuno;
  riacceso, riprende da solo entro mezzo minuto, senza riavviare.
- Il cliente preme «Attiva su Telegram» sulla pagina della riparazione, Telegram apre la chat del bot e lui preme
  «Avvia»: il bot risponde «👋 Sei stato collegato alla tua riparazione presso <nome del negozio>» e chiede con due
  pulsanti «✅ Sì, notifiche sulla riparazione» o «🔕 No grazie». Solo il «sì» accende le notifiche (e il riquadro
  della pagina passa a «Notifiche Telegram attive»).
- Un link già usato, scaduto o di una riparazione chiusa riceve «❌ Link di collegamento non valido o scaduto.
  Rigenera un nuovo link.». Un nuovo «Avvia» dalla pagina rimette la scelta a «no» finché il cliente non ripreme
  «sì».
- Il cliente scrive /stop (o /disattiva) e il bot spegne tutte le sue notifiche: «🔕 Notifiche disattivate per N
  iscrizione/i. Grazie!». /privacy manda il link dell'informativa del negozio (serve l'indirizzo pubblico).
- Qualunque altra cosa riceve una sola risposta ogni 10 minuti: «Questo è il bot di <negozio>. Per le notifiche apri
  il link della tua riparazione.»; la pagina dei provider conta queste chat («Risposte fisse date a chat
  sconosciute»). Il bot non risponde ai gruppi, ai canali, agli altri bot né alla chat del negozio.
- Se la pagina dei provider dice che il bot è in conflitto (errore 409), c'è un webhook attivo sul bot o un altro
  programma che lo legge (per esempio lo standalone ancora acceso): il bot riprova da solo, sempre più di rado (fino
  a ogni 5 minuti). GestRip non cancella mai il webhook: lo deve togliere il titolare.
- Le risposte del cliente a una notifica (con «Rispondi») entrano nella conversazione: sezione della 075, sotto.

## Avvisi Telegram al cliente e al negozio (slice 074)

- Gli avvisi al cliente partono solo con «Invio Telegram» e «Notifiche Telegram ai clienti» accesi, il bot
  configurato e l'indirizzo pubblico (il pulsante dell'avviso apre la pagina della riparazione). Solo il cliente
  diretto li riceve, mai una busta portata da un'oreficeria.
- Dopo un cambio di stato dalla scheda o dalla lista (menu dello stato, «✓ Pronta», «✓ Consegnata»), se il cliente
  è iscritto su Telegram con il «sì», compare il riquadro «📲 Notifica Telegram al cliente»: il testo come lo
  leggerà il cliente (non si modifica qui: si cambia in Impostazioni › Testi dei messaggi, «Cambio stato
  riparazione») e il pulsante «📂 Apri scheda riparazione». «Invia» mette l'avviso in coda («📲 Notifica Telegram
  accodata per il cliente»); «Annulla», Esc, un tocco fuori o ricaricare la pagina non mandano niente.
- L'avviso «Consegnata» arriva al cliente anche se la consegna spegne le sue notifiche. I cambi fatti dal QR, dai
  lotti o dall'import non chiedono niente e non avvisano.
- Se intanto la busta cambia ancora stato (anche dal QR), «Invia» dice «superata da un cambio di stato
  successivo» e non manda niente: vale il riquadro del cambio nuovo. Così anche con le notifiche spente nel
  frattempo, una busta passata a un'oreficeria o un link revocato o rigenerato.
- Dopo «✓ Pronta» con un cliente iscritto prima si risponde al riquadro, poi si va al prezzo finale.
- La risposta dalla scheda a un cliente iscritto parte da sola su Telegram («Risposta inviata. 📲 Notifica Telegram
  accodata per il cliente»), senza aprire WhatsApp; senza iscritti si apre il popup WhatsApp come prima.
- Un testo Telegram salvato che non va (segnaposto sconosciuto) non manda niente e lo dice in cima alla pagina:
  «Notifica Telegram non preparata/non accodata: il testo «…» non è valido (…).»; si corregge nei testi dei
  messaggi.
- La chat del negozio riceve: «🤝 Preventivo APPROVATO — busta N» o «🚫 Preventivo RIFIUTATO — busta N» (con
  l'importo e il motivo), e «💬 Messaggio dal cliente — busta N» (o «dall'orefice») con l'inizio del messaggio e
  il link «Apri la scheda». Senza Telegram la risposta e il messaggio del cliente valgono lo stesso; nel registro
  delle azioni resta la riga del mancato avviso del messaggio (`repair.message_notice_not_queued`) e, dalla 075,
  anche quella del preventivo (`repair.quote_notice_not_queued`, solo GestRip Go, D-86).
- Gli avvisi accodati si vedono nella lista «📨 Invii registrati» della scheda come «Telegram cliente», mai con la
  chat.

## Risposte del cliente su Telegram e pulizia (slice 075)

- Il cliente che risponde con «Rispondi» a un avviso Telegram della riparazione (cambio di stato o nuovo messaggio)
  scrive nella conversazione della busta, come dalla pagina: il messaggio compare nella scheda, la busta diventa
  «non letta», la chat del negozio riceve «💬 Messaggio dal cliente — busta N» e il bot risponde al cliente
  «✅ Messaggio inviato al negozio.» citando il suo messaggio.
- Valgono le regole della pagina: al più 1000 caratteri («❌ Risposta troppo lunga (N/1000), accorcia.»), dieci
  messaggi all'ora per busta contati con quelli della pagina («⏳ Hai già inviato molti messaggi nell'ultima ora:
  riprova più tardi.»), e in più otto al minuto dalla stessa chat («⏳ Hai inviato troppi messaggi: attendi qualche
  minuto e riprova.»). Solo testo: una foto o un vocale ricevono «❌ Solo risposte di testo per ora.».
- Una riparazione consegnata, annullata o chiusa risponde «Questa riparazione non riceve più messaggi.»; una
  passata a un'oreficeria o nel cestino «❌ Riparazione non trovata.». Un avviso più vecchio di 90 giorni non porta
  più alla busta (il bot dà la risposta fissa).
- Il cliente che ha scritto /stop può ancora rispondere a un avviso; dopo «Revoca link» o «Genera nuovo link» no,
  finché non si iscrive di nuovo dalla pagina con il link nuovo.
- Ogni ora, anche con Telegram spento, GestRip cancella le iscrizioni spente da più di 90 giorni e i link t.me
  scaduti, e toglie la chat dagli avvisi Telegram più vecchi di 90 giorni (negli invii della scheda restano). Una
  busta riaperta o tolta dal cestino non riaccende le notifiche: il cliente
  si iscrive di nuovo.
- *(Solo GestRip Go, da rivedere con il manuale)* Se il bot è rimasto più di sei giorni senza messaggi, riparte
  da capo con la lettura (D-85): non serve fare niente.

## Oreficerie, per i capitoli 01, 02 e 07 (slice 086–093)

Il capitolo 09 spiega l'anagrafica, i lotti, la pagina `/lotto` e lo storico. Restano da dire nei capitoli
dove nasce la cosa:

- *(capitoli 01 e 02)* In «Nuova riparazione» e nella modifica della busta il menu «Oreficeria cliente» c'è solo
  se in anagrafica c'è almeno un'oreficeria; la prima voce è «— Diretta» e il nome del negozio. Nella scheda la
  voce «Oreficeria cliente» dice il nome, o «Diretta» e il nome del negozio; su una busta di un'oreficeria il
  «Telefono» è quello dell'oreficeria (con sotto «Oreficeria» e il nome) e il «Telefono cliente finale» è «Solo
  anagrafica: su busta B2B si scrive all'oreficeria.»; nella modifica il telefono del cliente finale è spento
  («Busta conto terzi: …»).
- *(capitolo 07)* In Impostazioni › «Messaggi WhatsApp» il testo «Avviso lotto pronte oreficeria» (parole
  `{oref_nome}`, `{n_buste}`, `{link}`, `{negozio}`) è il messaggio del lotto; il testo «Aggiornamento oreficeria»
  è quello che ogni busta del lotto registra al momento dell'invio, con il link del lotto al posto di
  `{link}`. Il capitolo 09 li nomina soltanto.
- *(capitolo 07)* Il pulsante «📲 Avvisa oreficeria» della scheda e «Azzera l'avviso»: il capitolo 09 li cita
  per le buste che non entrano in un lotto perché già avvisate.

## Profilo del negozio (slice 087 e D-97) *(per il capitolo 12 Amministrazione)*

- Impostazioni › «Negozio» (la prima scheda, solo il titolare) ha i dati del negozio che finiscono su
  ricevute, schede PDF, report, messaggi ai clienti, informativa privacy e piè della pagina del lotto delle
  oreficerie. Il dipendente non vede la pagina (chi ci va torna alla lista con «Operazione riservata al
  titolare.»).
- I campi, nell'ordine: «Nome negozio» (obbligatorio), «Sottotitolo», «Ragione sociale», «Identificativo
  fiscale», «Codice fiscale (vuoto = uguale alla P.IVA)», «Indirizzo», «CAP», «Città», «Provincia»,
  «Telefono», «Email», «Email vendite», «Orari (nei messaggi al cliente)», «Instagram», «Facebook», «Blog o
  sito» e, in fondo, «Condizioni di garanzia» (obbligatorie). Un campo facoltativo lasciato vuoto si toglie.
- Limiti: nome, ragione sociale, indirizzo, email, email vendite, sottotitolo, Instagram, Facebook e sito
  255 caratteri; CAP e provincia 20; città 120; telefono 80; identificativo fiscale 40; orari 500; garanzia e
  codice fiscale nessun limite di lunghezza (il codice fiscale ha una forma). Oltre il limite: «Troppo lungo
  (massimo N caratteri).» sotto il campo; campo obbligatorio vuoto: «Campo obbligatorio.».
- Il codice fiscale è di 16 lettere e cifre (persona) o di 11 cifre (società): gli spazi si tolgono e le
  lettere diventano maiuscole; altrimenti «codice fiscale non valido: 16 caratteri, o 11 cifre per una
  società».
- «Salva impostazioni» toglie gli spazi ai lati e dice «Impostazioni salvate. I documenti emessi restano
  invariati.»: i documenti già emessi non cambiano. Se c'è un errore non si salva niente e gli avvisi sono
  sotto i campi.
- Il campo della città si chiama «Città» (D-97; il programma di prima aveva «Citta»).
- *(Da rivedere quando c'è ADM-07)* Il nome sotto l'icona dell'app installata e nel manifest dice ancora
  «Negozio», non il nome del profilo: lo porta la tappa T4.15. Nella scheda «Negozio» per ora c'è solo il
  profilo; le altre impostazioni del negozio arrivano con le prossime slice.

## Esportazione dei dettati (slice 117) *(per il capitolo 12 Amministrazione)*

- Il comando `gestrip esporta-dettati [-db FILE] FILE.jsonl` si lancia a mano sul computer dove gira GestRip; lo
  lancia chi gestisce l'installazione. Il database usato è `/var/lib/gestrip/gestrip.db`; con `-db FILE` se ne
  indica un altro.
- Il file deve essere nuovo: se esiste già il comando si ferma con «gestrip: Il file esiste già: l'esportazione
  non sovrascrive.» e non tocca niente. Il file è creato leggibile solo da chi lo ha creato (permessi 0600).
- Se il database non c'è: «gestrip: Esportazione non riuscita: database non trovato: lo crea «gestrip serve».»
- A schermo escono solo numeri, una riga per volta: «totali=», «esportati=», «scartati=», «cancellati=» e, per
  ogni motivo di scarto, una riga «scartati_<motivo>=» (per esempio «scartati_nome_non_trovato=»). Mai un
  dettato, mai un nome, mai un esempio.
- I motivi di scarto, che sono anche i nomi nelle righe «scartati_…»: `nome_non_trovato` (il nome non si trova
  nel testo in nessuna forma), `nome_ambiguo` (due grafie vicine dello stesso nome), `nome_residuo` (dopo la
  sostituzione resta un pezzo del nome vero), `telefono_dubbio` (un numero ha la forma di un telefono ma non è di
  nessuno dei noti), `telefono_residuo` (dopo la sostituzione resta un numero vero), `valore_illeggibile` (un
  valore non è né testo né un numero), `altro_nome` (nel dettato compare una parola del nome di un altro cliente o
  di un nome proprio comune), `cliente_escluso` (il cliente si è opposto), `busta_nel_cestino` (la busta è nel
  cestino) ed `errore` (un caso che non dovrebbe succedere).
- Se qualcosa va storto non si cancella niente e il file scritto a metà viene tolto, con il messaggio «gestrip:
  Esportazione non riuscita: …». Nel caso raro in cui la cancellazione ha dato errore ma le righe lette non sono
  più tutte nel database, il file resta: «gestrip: Esportazione non completata: la cancellazione dei dettati ha
  dato errore, ma N righe su M non ci sono più: il file resta.»; se la verifica di controllo non riesce:
  «gestrip: Esportazione non completata: la cancellazione dei dettati ha dato errore e la verifica non è riuscita:
  il file resta.» In nessuno dei messaggi compare un percorso di file, né del database né del file.
- L'esportazione non controlla il blocco che sta dopo un import (D-112): è un comando lanciato a mano da chi
  gestisce l'installazione, non una delle pagine.

## Promemoria automatici (slice 094–098) → capitolo 07 Messaggi

I promemoria automatici (nel codice «job», nella pagina «Promemoria e job») mandano al **negozio**, non ai
clienti, un messaggio per email e per Telegram, secondo i canali accesi. Sono tre.

- **«Buste in ritardo»** (slice 095): per ogni busta la cui data di consegna prevista è passata, un messaggio
  per canale con «Busta in ritardo» e il numero, il cliente, «Consegna prevista: gg/mm/aaaa (Ngg fa)» e la
  descrizione (al più 200 caratteri). Una busta si segnala una sola volta; se si cambia la data prevista,
  si segnala di nuovo quando passa la data nuova. Non si segnalano le buste consegnate, annullate, chiuse
  (Compro Oro) o nel cestino. «Giorni di ritardo» (di fabbrica 1, da 1 a 365) dice quanti giorni dopo la data
  prevista una busta è «in ritardo». Di fabbrica è acceso e parte alle 09:00.
  La prima volta che parte davvero, le buste in ritardo da **7 giorni o più** si mettono a tacere senza
  messaggi, per non ricevere tutto lo storico insieme; l'esecuzione dice «N buste storiche silenziate alla
  prima esecuzione.» (con una sola busta scrive «1 busta storiche»). Chi arriva dal programma di prima ha già
  fatto questo passo.
- **«Solleciti artigiano»** (slice 096): un solo messaggio per canale con le buste fuori bottega da troppo
  tempo, divise per artigiano, con «N buste fuori bottega da oltre N giorni.». «Giorni fuori bottega» (di
  fabbrica 14, minimo 1) è la stessa soglia della pagina «Solleciti pendenti». Ogni busta compare una volta
  per ogni volta che va dall'artigiano: se resta fuori non si ripete ogni giorno. Il messaggio non sollecita
  l'artigiano e non conta come sollecito: per sollecitarlo si usa sempre «Solleciti pendenti». Le buste nel
  cestino non si nominano (slice 097b). Di fabbrica è acceso e parte alle 10:00.
- **«Briefing del mattino»** (slice 097): solo Telegram (con la sola email non parte), una volta al giorno. Cinque
  parti: buste pronte da avvisare, Compro Oro da avvisare, in ritardo, in scadenza nei prossimi tre giorni, fuori
  bottega da almeno «Fuori bottega (0 = escludi)» giorni (di fabbrica 14; 0 toglie la parte). Ogni parte elenca
  al più 20 buste e, se ce ne sono di più, lo dice («21+» nel titolo, «…e altre» in fondo). Non segna niente
  sulle buste: la stessa busta torna ogni mattina finché non cambia. Senza niente da dire manda «nulla
  all'orizzonte ☕». Un testo lungo arriva in più messaggi (al più 4000 unità ciascuno) e quelli dopo il primo
  possono avere «(segue)» nel titolo. Di fabbrica è **spento** e parte alle 08:00.
- *(Solo del Go)* Giorni di apertura (D-17, D-102): i promemoria partono da soli solo nei giorni di apertura,
  di fabbrica da martedì a sabato; i festivi in settimana contano come giorni aperti. In un giorno chiuso non
  partono e non lasciano righe; il primo giorno aperto trovano da soli quello che era rimasto.
- **Quando partono:** una volta al giorno, dall'ora scelta dal titolare (ora d'Italia) in poi. Se l'apparecchio è
  spento a quell'ora, partono appena si riaccende nello stesso giorno; un giorno intero spento non si recupera.
  Solo se è acceso almeno un canale (email o Telegram); senza canali non partono e non lasciano righe. Se un
  promemoria non riesce, ci riprova al minuto dopo, al massimo 3 volte al giorno; se un messaggio non si può
  accodare non parte niente di quel promemoria («Fallito» con il motivo) e se ne riparla al giro dopo.
- **Impostazioni › «Promemoria»** (solo il titolare; la pagina si chiama «Promemoria e job», con il link
  «Solleciti» alla pagina dei solleciti): per ogni promemoria la frase che lo spiega, «Ultima esecuzione»,
  «Attivo», «Ora (Europe/Rome)» e la soglia; poi «Giorni di apertura» con sette caselle (da «Lunedì» a
  «Domenica»; almeno un giorno: «Scegli almeno un giorno di apertura.»); «Salva cadenze e soglie» dice
  «Impostazioni salvate.» (se un campo non è valido, l'avviso è sotto il campo e non si salva niente).
- **«▶ Esegui adesso»** fa partire subito un promemoria, anche in un giorno chiuso: «Job eseguito.»; l'esito si
  legge nello storico. Il briefing ha il pulsante solo con Telegram attivo. Senza email né Telegram la pagina
  dice «I promemoria partono solo con email o Telegram attivi: finché non ne accendi uno in Provider non
  partono e non lasciano righe. Le impostazioni si salvano lo stesso.» e non mostra il pulsante. Se il
  promemoria è spento, risulta «Saltato» e non conta come esecuzione del giorno (dalla ricevuta della 098).
- **Storico delle esecuzioni:** le ultime 20, ognuna con il nome del promemoria, l'esito («Eseguito»,
  «Saltato», «Fallito»), quando, come è partito («Automatico» o «A mano»), quante buste e quanti messaggi e il
  dettaglio. Se un promemoria si interrompe a metà (un guasto del database) la riga lo dice («Il job si è
  interrotto…») e il giro dopo riprova.
- Difetti non gravi dello standalone copiati (tenerli in lista, non spiegarli nel manuale): «1 busta storiche
  silenziate»; «da oltre N giorni» con il limite compreso; la descrizione del briefing tagliata con due misure
  diverse (emoji); «(21+)» e «…e altre ≥1»; un'ora salvata illeggibile che blocca ogni salvataggio finché non
  la si riscrive.

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

## Backup: figure mancanti (slice 132) *(capitolo 11)*

Tre figure del capitolo 11 non ci sono, perché l'apparecchio di prova delle schermate (il banco) non ha quello
che servirebbe. Si aggiungono quando il banco lo avrà.

- **I due passi della configurazione della copia remota** (impronta mostrata e conferma «Prova collegamento e
  salva»): sul banco non c'è un server SFTP.
- **Il riquadro rosso «⚠ Backup: c'è un problema»:** sul banco il backup automatico è spento e non c'è una
  chiavetta configurata, quindi il riquadro non compare.
- **Il riquadro giallo «⚠ Questa è una copia isolata» e la pagina «Conferma del ripristino»:** servirebbe un
  ripristino vero sul banco.

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
