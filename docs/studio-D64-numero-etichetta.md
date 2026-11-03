# Studio D-64 — il numero dell'etichetta QR come numero visibile (03/11/2026)

Decisione di Stefano (D-64, terza versione del 03/11/2026): il numero interno N/AA e il suo contatore
restano come oggi ma diventano solo tecnici; il numero **visibile** a personale e clienti è quello
dell'etichetta QR prenumerata, nella forma «0045/26» (`RIP-2026-0045`). Qui i casi da decidere prima
del codice, ognuno con la raccomandazione. Sola documentazione: il codice si fa nelle tappe giuste.

Fatti dall'inventario: `qr_code` è testo libero, facoltativo e **non unico** (RIP-H-39, RIP-H-22,
deciso il 29/10/2026); il confronto è esatto, con gli spazi ai bordi tolti; le etichette 57×32 si
possono **ristampare** su qualsiasi intervallo (RIP-S-48, slice 210); una busta senza QR ha il segno
«⚠ senza QR» (RIP-H-56) e alla creazione si chiede conferma (RIP-H-39).

## (a) Buste senza etichetta o con un codice che non è `RIP-AAAA-NNNN`

Casi: busta salvata «senza QR», buste vecchie e importate, codici del Flask o di colleghi.

- **Numero visibile:** il numero interno, ma in una forma che non si confonda con un'etichetta,
  perché «45/26» interno e «0045/26» d'etichetta possono essere due buste diverse. Raccomandazione:
  «N. 45/26» (interno) accanto al segno «⚠ senza QR» che c'è già; nei testi per il cliente il numero
  interno come oggi.
- **Ricerca:** raccomandazione: un numero scritto («45», «0045», «45/26», «0045/26») cerca le
  etichette; il numero interno resta cercabile **solo per le buste senza etichetta `RIP-…`**, come
  ripiego (altrimenti quelle buste si ritroverebbero solo per nome). Più risultati → la lista (D-63).
- **Codice non `RIP`:** la scansione lo trova come oggi (confronto esatto); per il numero visibile
  conta come «senza etichetta».

## (b) Buste gemelle con lo stesso QR

Perché possono esistere: un intervallo di etichette ristampato (la ristampa è libera), una busta di
carta riusata con l'etichetta ancora attaccata, un errore di scansione, dati importati.

- **Cosa si mostra:** lo stesso «0045/26» su tutte; si distinguono per nome, data, stato e
  descrizione, come già nella scelta fra gemelle della ricerca QR.
- **Raccomandazione:** nessun blocco (decisione del 29/10), ma due avvisi che oggi non ci sono:
  - in «Nuova riparazione», scansionando un'etichetta già usata da una busta **aperta**: «Etichetta
    già usata dalla busta aperta di <cliente> (0045/26)», con la possibilità di salvare lo stesso;
  - nella scheda, una riga «Un'altra busta ha la stessa etichetta» con il link.
  Nei messaggi al cliente il numero resta «0045/26» (di solito le gemelle sono di clienti diversi).

## (c) Impatto su quello che è già fatto e sulle tappe dopo

Già fatto (da cambiare quando si tocca il numero, con una deviazione dichiarata nel banco):
- **042 ricevuta:** «Busta N» → numero visibile; nome del file `ricevuta-0045-26.pdf`.
- **043 etichetta 76×127:** «#5/26» → «#0045/26»; il QR resta il codice della busta o, senza
  codice, il numero interno (decisione del 29/10). **Scheda PDF:** «N° BUSTA» → numero visibile, il
  numero interno in piccolo.
- **044 report:** numero visibile in ogni scheda; `ids=` resta sugli id tecnici.
- **Scheda della busta (032) e «Nuova riparazione» dopo il salvataggio (031):** titolo «Busta
  #0045/26», il numero interno in piccolo.
- **Registro e audit:** `bag_number` resta il numero interno (dato tecnico, già scritto così).

Tappe dopo:
- **T4.2 lista e ricerca:** numero visibile in lista e galleria; ricerca come in (a), dentro D-63.
- **T4.5 pagina cliente e link:** `/stato/<busta>/<anno>` e i link già mandati restano sul numero
  interno (sono indirizzi, non numeri da leggere); la pagina mostra «0045/26».
- **T4.6 testi wa.me, email; T4.7 Telegram:** raccomandazione: **nessun segnaposto nuovo**; quello del
  numero della busta vuol dire «numero visibile» (etichetta o ripiego). I testi già scritti da Stefano
  continuano a funzionare e mostrano il numero che il cliente ha sulla busta.
- **T4.8 artigiani:** link firmati sul numero interno invariati; fogli di consegna e pagine con il
  numero visibile.
- **045 etichette 57×32:** sotto il QR «0045/26» al posto di «0045» (aggiorna D-21).

## (d) Il «numero busta» nei moduli

- **«Nuova riparazione»:** oggi c'è «N° Busta*» con il numero interno proposto. Raccomandazione:
  togliere il campo dal modulo (il numero interno si assegna da solo, come già succede con il campo
  vuoto) e fare di «Codice QR busta» il campo del numero: «Etichetta (0045/26)» con il 📷. Scritto a
  mano, «45» o «0045/26» diventa `RIP-<anno>-0045` (anno dall'etichetta scritta, se no l'anno della
  data di accettazione); un codice di altro formato resta com'è. L'avviso «⚠ Manca il codice QR»
  resta.
- **Modifica della busta:** il numero interno già non si modifica; l'etichetta si cambia come oggi
  il codice QR, mostrata come «0045/26».
- **Dopo il salvataggio:** «Busta 0045/26 salvata», e il numero interno solo nel registro.

## Risposte di Stefano (03/11/2026, tramite il coordinatore)

- **(a), (c), (d):** come raccomandato. In «Nuova riparazione» il campo «N° Busta» si toglie e il
  campo del QR diventa «Etichetta (0045/26)» con il 📷; «45» scritto senza anno prende l'anno della
  data di accettazione.
- **(b), il caso reale:** quando un cliente ritira, la stessa busta di carta con la sua etichetta si
  riusa per un'altra riparazione; di solito quindi c'è una busta chiusa (consegnata) e una aperta con
  la stessa etichetta. Regole:
  - «Nuova riparazione»: avviso solo se l'etichetta è usata da una busta ancora **aperta**; nessun
    avviso se le altre sono chiuse;
  - scansione dalla home (D-63): se fra le gemelle ce n'è **una sola aperta** si va dritti a quella
    (pannello azioni), senza scelta; la scelta compare solo con più buste aperte o nessuna aperta.
    **Cambia RIP-H-22**, dove oggi la scelta compare con ogni gemella;
  - scheda: «Un'altra busta ha la stessa etichetta» solo se l'altra è aperta; per quelle chiuse un
    link «storico di questa etichetta»;
  - un'etichetta di un anno precedente riusata (`RIP-2025-0045` su una busta del 2026) mostra
    «0045/25»: va bene così.
- **Aggiunta (03/11/2026):** un numero d'etichetta scritto a mano nella ricerca della home («0045/26»,
  «0045», «45»…) mostra **sempre tutte** le buste con quell'etichetta, anche chiuse e consegnate,
  qualunque filtro di stato sia attivo: la ricerca per etichetta ignora il filtro di stato, le altre
  ricerche (nome ecc.) tengono i filtri come oggi. Con 1 risultato si apre la scheda, con più la lista.
  La scansione 📷 resta come sopra (dritta all'unica aperta).
