# Da scrivere nel manuale

Elenco una tantum (03/11/2026) dei comportamenti visibili a chi usa GestRip, decisi prima della regola
«Per il manuale» nelle ricevute (D-49…D-64 e la T4.3). Da qui parte la prima slice del manuale (fine
della T4.4). Ogni riga dice dove è decisa; quelle segnate *(da fare)* descrivono come sarà il programma
quando la tappa indicata sarà fatta: nel manuale vanno solo quando il codice c'è.

Si toglie una riga quando è passata in un capitolo; il file sparisce quando è vuoto. Le sezioni «Foto
(T4.3)» e «Stampe (T4.4)» sono passate nei capitoli 03, 04 e 05 con la slice 049, «Ricerca e QR» nel
capitolo 06 con la slice 055.

## Numero della busta ed etichette *(da fare: T4.2, ritocchi nelle stampe e nei messaggi)*

- Il numero che vedono tutti, personale e clienti, è quello dell'**etichetta QR**, nella forma
  «0045/26»: in lista, nella scheda, nei PDF, nelle etichette, nei messaggi WhatsApp, Telegram ed
  email e nella pagina di stato del cliente (D-64). Vero per lista e scheda (050, 053), PDF,
  etichetta della busta e pagina della firma (058: già scritto nei capitoli 04 e 05); *(da fare:
  messaggi e pagina di stato, T4.5–T4.7)*.
- In «Nuova riparazione» non c'è più il campo «N° Busta»: c'è «Etichetta (0045/26)» con il 📷.
  Scrivendo a mano «45» si intende l'etichetta 0045 dell'anno della data ricezione (D-64, D-71).
- Una busta senza etichetta mostra «N. 45/26» con il segno «⚠ senza QR» (D-64).
- Un'etichetta riusata dell'anno prima mostra il suo anno, per esempio «0045/25» (D-64).
- Busta di carta riusata dopo il ritiro: se l'etichetta è già su una busta **ancora aperta**, «Nuova
  riparazione» lo avvisa e si può salvare lo stesso. Nella scheda c'è «Un'altra busta ha la stessa
  etichetta» solo se l'altra è aperta; per quelle chiuse c'è il link «storico di questa etichetta»
  (D-64).

## Lista delle buste (slice 056 e 057, per un capitolo sulla lista)

Dalla ricevuta della slice 056 (`docs/slices/056-pulsanti-stato-pin.md`): il codice c'è, manca il
capitolo sulla lista.

- Su una busta in lavorazione c'è «✓ Pronta»: la segna pronta e apre la scheda sul prezzo finale.
- Su una busta pronta c'è «✓ Consegnata»: chiede «Segnare la busta 0045/26 come consegnata?» e, con
  OK, la segna consegnata; si resta sulla lista, con «Stato aggiornato.».
- Preventivi, buste in attesa materiali e Compro Oro non hanno pulsanti rapidi: si usa il menu.
- Il menu dello stato di ogni busta offre solo i passi consentiti; scelto un passo, lo stato cambia
  subito. Una busta consegnata o annullata ha il menu spento (si riapre dalla scheda).
- 📌 «Pinna in cima» tiene la busta in testa alla lista; lo stesso 📌, «Togli pin», la rimette al suo
  posto.
- Dopo un'azione la lista resta allo stesso punto e il messaggio compare in cima allo schermo
  (toccandolo si chiude).
- «← Indietro» della scheda e di «Nuova riparazione» torna alla lista da cui si è partiti, con i
  suoi filtri e la sua pagina.
- Nel cassetto dei filtri il periodo si sceglie per «Ricezione» (era «Accettazione»), e la pillola
  dice «📅 Ricezione: …» (D-71).

Dalla ricevuta della slice 057 (`docs/slices/057-galleria-import.md`), la galleria:

- Sopra la lista, «☰ Lista» e «🖼 Galleria» cambiano vista tenendo i filtri (la pagina riparte dalla
  prima).
- La galleria mostra, per le buste della pagina che hanno foto, la prima foto con «+2» se ce ne sono
  altre due, e sotto numero, data, cliente e stato. Toccando la foto si apre la scheda con quella foto
  grande; toccando i dati si apre la scheda.
- In cima dice quante schede hanno foto e quante senza foto sono nascoste; se nella pagina nessuna
  busta ha foto, «Nessuna scheda con foto trovata.».
- Pagine, filtri, ricerca, «⧉ Schede PDF» e «← Indietro» della scheda restano nella galleria; «×
  Azzera filtro» e i riquadri in alto tornano alla lista.
- La grandezza delle foto (piccola, media, grande) è un'impostazione del negozio (media se non c'è).
  *(da fare: la pagina delle impostazioni del Go non la ha ancora; oggi vale quella portata dallo
  standalone con l'import)*

Per chi fa l'import dallo standalone (capitolo dell'amministrazione, slice 057):

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
