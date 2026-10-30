# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`prompt-via-area.md`). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

1. **Nome del servizio systemd di `gestrip serve`** (slice 013b, 30/10/2026). Il messaggio della
   pagina utenti senza `indirizzo_interno` dice di riavviare con `sudo systemctl restart gestrip`:
   serviva un comando esatto e l'installer (T5) non esiste ancora. **Raccomandazione:** tenere
   `gestrip.service`. Se l'installer sceglie un altro nome, cambia una costante
   (`apparecchio.ComandoRiavvio`) e il test lo segue.

2. **Chi può fare la configurazione guidata del primo titolare (ACC-16)** (30/10/2026, prima della
   slice). Lo standalone crea il primo titolare dall'installer, leggendo email e password da due
   file privati, e solo se `users` è vuota. Il Go la sostituisce con una procedura nel browser.
   Il piano non dice chi può usarla: su un database vuoto, chiunque sulla tailnet apra la pagina
   per primo diventerebbe titolare. **Raccomandazione:** con il database vuoto, `gestrip serve`
   non mostra la procedura a chiunque. Il link `/configura/<codice>`, monouso e con scadenza, lo
   stampa solo un comando locale (`gestrip primo-titolare`, sullo stile di `reset-link` e
   `invito`: solo sul terminale, mai nel journal). La pagina chiede email e password (almeno 12
   caratteri), funziona solo se `utenti` è vuota e scrive `administrator.bootstrapped` con attore
   `installer`. Finché non rispondi, ACC-16 aspetta; le altre slice vanno avanti.

3. **`-senza-volume-cifrato` (OPS-12, slice 014).** `gestrip serve` parte solo se la cartella del
   database sta su un volume LUKS. Lo sviluppo e il banco non sono cifrati, quindi esiste
   un'opzione esplicita che spegne il controllo e stampa «ATTENZIONE … mai sull'apparecchio». Il
   messaggio di rifiuto nomina l'opzione. **Raccomandazione:** tenerla così, ma togliere il nome
   dell'opzione dal messaggio di rifiuto, per non suggerirla a chi è sull'apparecchio. Il banco e
   lo sviluppo la conoscono dal README. L'import di prova in tmpfs su pc-fisso (T2) userà
   l'opzione, senza eccezioni per tmpfs.

4. **btrfs e installer (OPS-12, slice 014).** Su btrfs il controllo risale al dispositivo dalla
   sorgente scritta in mountinfo; un btrfs su più dischi controlla solo quel dispositivo.
   **Raccomandazione:** l'installer (T5) usa ext4 su LUKS2, e il controllo resta com'è.

5. **Servizio systemd (T5).** **Raccomandazione:** nel servizio `gestrip` mettere anche
   `LimitCORE=0` e `UMask=0077`, come seconda difesa oltre a quello che fa già il binario
   (niente core dump, database a 0600).

6. **Database come link (OPS-12, slice 014).** Lo standalone accettava un `gestrip.db` che fosse un
   link. Il Go lo rifiuta, e rifiuta anche `-wal` e `-shm` che siano link o che stiano su un altro
   montaggio della cartella: altrimenti un link verso un disco in chiaro passerebbe il controllo
   del volume cifrato (trovato dalla review e misurato). Le cartelle intermedie possono ancora
   essere link: si controlla la cartella reale. **Raccomandazione:** approvarla come deviazione
   di OPS-12, che è già una voce «sostituita».

7. **Guscio delle pagine interne (slice 015): differenze che restano anche a porting finito.**
   - Gli asset hanno l'hash nel nome (`/assets/app-<hash>.css`), senza query. Il worker dello
     standalone mette in cache solo i file sotto `/assets/` senza query, e lì gli asset avevano
     `?vsn=d`: nello standalone non finivano mai in cache, nel Go sì. Sono solo file del guscio,
     senza dati di clienti.
   - «Esci» è un modulo POST con un bottone invece di un link con `data-method="delete"`, quindi
     funziona anche senza JavaScript.
   - I messaggi verdi non si chiudono da soli dopo 5 secondi (slice 162 dello standalone).
   - Il CSS è scritto a mano sulle stesse variabili, al posto di Tailwind e daisyUI: non è
     identico al pixel.

   **Raccomandazione:** approvare le prime due come deviazioni; rimandare la chiusura automatica
   alla T4 insieme alle pagine che la usano; il CSS è un dettaglio d'aspetto, che si controlla a
   occhio sul banco.

   Restano temporanee, finché non arrivano le aree che le completano:
   - menu con le sole voci delle pagine che esistono;
   - `/settings` che va alla prima scheda finché non c'è ADM-02;
   - niente logo, campanella, colori della barra né banner d'emergenza.

8. **SIGTERM durante l'avvio (slice 015).** Se `gestrip serve` riceve lo stop mentre si sta ancora
   avviando (per esempio un `systemctl stop` subito dopo lo start), esce con 1 e «context
   canceled» invece che pulito. **Raccomandazione:** se il contesto è già cancellato dal segnale,
   uscire con 0, così systemd non segna un guasto. È un piccolo cambio del prodotto.

9. **Conservazione di 5 anni e import (ADM-17, D-14, slice 016).** Il lavoro mensile gira subito
   al primo avvio. Dopo `gestrip import` (T2) cancellerebbe quindi in un colpo tutti gli eventi
   importati più vecchi di 5 anni, compresi quelli del Flask. **Raccomandazione:** il primo giro
   dopo un import aspetta il primo backup verificato (quando ci sarà il registro dei backup,
   D-11); fino ad allora, dopo un import il lavoro non cancella e scrive solo che ha rimandato.
   Altre scelte già applicate, da confermare:
   - si cancella `creato_il` < adesso − 5 anni del calendario di Roma, quindi l'evento di
     esattamente 5 anni resta;
   - la riga del giro (`audit.retention_applied`, attore `retention`) si scrive anche con 0
     cancellati;
   - un giro per mese di calendario, con un controllo all'avvio e poi ogni ora;
   - lotti da 1000 righe (circa 3 ms ciascuno).
   - guardia contro l'orologio avanti (aggiunta dopo la review): il limite non supera «evento
     più recente − 5 anni», e se adesso supera l'ultimo evento di più di 31 giorni il giro si
     rimanda. Un negozio fermo più di 31 giorni rimanda quindi il giro finché non torna un
     evento (per esempio un login).

10. **Differenze di ACC-17/18/19 senza un numero D (slice 017).** Sono decisioni già prese nelle
    slice 006b e 008b, ma senza deviazione scritta:
    - due cookie per faccia con il prefisso `__Host-` (sessione e CSRF) invece di un cookie solo;
    - `Secure` sempre, anche in sviluppo;
    - `CrossOriginProtection`, che rifiuta un POST da un'altra origine anche con il token giusto;
    - il registro delle richieste più stretto dello standalone: una riga con il solo pattern, mai
      percorso né query.

    Si aggiunge il POST /login con una sessione valida ma senza token: nel Go dà 302 a `/`, nello
    standalone 403 (ragionato dall'ordine del pipeline, non misurato). È innocuo.
    **Raccomandazione:** dare un numero D a tutte e cinque.

11. **Panic in un gestore (slice 017).** Il Go chiude la connessione senza risposta, come prima;
    lo standalone mostra la pagina 500 «Internal Server Error». In più ora il Go registra solo
    tipo e stack del panic, mai il valore, che può contenere un token. **Raccomandazione:** una
    slice piccola che scriva la pagina 500 dello standalone quando non è ancora uscito niente,
    dopo averne misurato sul banco le intestazioni.

12. **`serve` su un indirizzo che non è loopback.** Di default le facce ascoltano su
    127.0.0.1, e solo tailscaled fa il TLS. Con `-interna 0.0.0.0` (lo usa il banco) il Go serve
    in chiaro, senza il redirect a HTTPS di `force_ssl`. **Raccomandazione:** rifiutare un
    indirizzo non loopback senza un'opzione esplicita da sviluppo e banco.

13. **Possibile difetto dello standalone (ragionato, non misurato, non è un rischio).** Il
    `request_log_level` dello standalone non esclude `/m/:code`, che quindi finisce nel log col
    suo codice. Sono codici del portale ritirato, che oggi rispondono 410 e non aprono niente.
    Inoltre un prefisso scritto in codice (per esempio `/%72/<token>`) aggirerebbe l'esclusione,
    ma il token finisce nel log solo se chi lo ha già lo scrive così. Il Go non ha nessuno dei
    due problemi. **Raccomandazione:** annotarlo; correggerlo nello standalone solo se si fa
    un'altra slice lì.

14. **Moduli dei clienti e browser vecchi (da ricordare in T4, sospetto della review 017).** Senza
    `Sec-Fetch-Site` (Safari/iOS prima di 16.4), `CrossOriginProtection` confronta `Origin` con
    `Host`. Da una pagina con `Referrer-Policy: no-referrer`, come le pagine pubbliche dello
    standalone, il browser manda `Origin: null`, e un POST del cliente (per esempio «accetta
    preventivo») diventerebbe 403. **Raccomandazione:** quando arrivano i moduli pubblici, un test
    con `Origin: null` senza `Sec-Fetch-Site`. Se serve, la faccia pubblica si affida al solo
    token CSRF, come lo standalone.
