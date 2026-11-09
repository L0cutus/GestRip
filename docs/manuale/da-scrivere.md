# Da scrivere nel manuale

Elenco una tantum (03/11/2026) dei comportamenti visibili a chi usa GestRip, decisi prima della regola
«Per il manuale» nelle ricevute (D-49…D-64 e la T4.3). Da qui parte la prima slice del manuale (fine
della T4.4). Ogni riga dice dove è decisa; quelle segnate *(da fare)* descrivono come sarà il programma
quando la tappa indicata sarà fatta: nel manuale vanno solo quando il codice c'è.

Si toglie una riga quando è passata in un capitolo; il file sparisce quando è vuoto. Le sezioni «Foto
(T4.3)» e «Stampe (T4.4)» sono passate nei capitoli 03, 04 e 05 con la slice 049, «Ricerca e QR» nel
capitolo 06 con la slice 055, la lista e la galleria (slice 056 e 057) nel capitolo 06 con la slice 059.

## Numero della busta ed etichette *(da fare: messaggi e pagina di stato; capitoli 01 e 02)*

- Il numero che vedono tutti, personale e clienti, è quello dell'**etichetta QR**, nella forma
  «0045/26»: in lista, nella scheda, nei PDF, nelle etichette, nei messaggi WhatsApp, Telegram ed
  email e nella pagina di stato del cliente (D-64). Vero per lista e scheda (050, 053; la lista
  è nel capitolo 06), PDF, etichetta della busta e pagina della firma (058: già scritto nei capitoli 04
  e 05), pagina del cliente `/r/` (061); *(da fare:
  messaggi e pagina di stato `/stato`, T4.5–T4.7)*.
- In «Nuova riparazione» non c'è più il campo «N° Busta»: c'è «Etichetta (0045/26)» con il 📷.
  Scrivendo a mano «45» si intende l'etichetta 0045 dell'anno della data ricezione (D-64, D-71).
- Busta di carta riusata dopo il ritiro: se l'etichetta è già su una busta **ancora aperta**, «Nuova
  riparazione» lo avvisa e si può salvare lo stesso. Nella scheda c'è «Un'altra busta ha la stessa
  etichetta» solo se l'altra è aperta; per quelle chiuse c'è il link «storico di questa etichetta»
  (D-64).

## Pagina del cliente (slice 061, 062, 063) *(da fare: riquadri, pagine storiche; 064, T4.6)*

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
