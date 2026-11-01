# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

Slice 033 (modifica della busta). Nessuna blocca il lavoro.

1. **Righe `repair.viewed` in più (RIP-S-01, RIP-S-04).** Nel Go ogni apertura della scheda è una
   richiesta, e scrive una riga: quindi anche aprire il modulo, «Annulla» e la scheda che si
   ritrova dopo il salvataggio (salvataggio, poi redirect, poi pagina). Lo standalone, per
   «Modifica» e dopo il salvataggio, non scrive niente, perché resta nella stessa pagina LiveView.
   **Raccomandazione:** accettarlo come deviazione sul modello della D-44: una riga
   `repair.viewed` per ogni pagina della scheda mostrata.
2. **«Salva modifiche» spento finché non cambia niente (RIP-S-04).** Nello standalone il pulsante
   è spento solo nell'aspetto finché non si scrive. Senza JavaScript resterebbe grigio anche dopo
   aver scritto, quindi nel Go per ora è sempre acceso. **Raccomandazione:** copiarlo con un
   piccolo pezzo di `app.js` quando arriva il livello browser del banco; fino ad allora la voce
   resta parziale.
3. **Annuncio dello stato riallineato prima di salvare (RIP-S-11).** Nello standalone, spuntando
   o togliendo «🪙 Scheda Compro Oro», il modulo dice subito «Lo stato «…» passerà a «…»». Nel Go
   lo si scopre dopo, dal messaggio «Riparazione aggiornata. Tipo busta cambiato: stato
   riallineato a «…».». I due rifiuti (busta fuori bottega, busta chiusa) non salvano niente e
   hanno il testo dello standalone. **Raccomandazione:** accettarlo fino al JavaScript, poi un
   aiuto in `app.js` con il testo preparato dal server.
4. **Telefono che si blocca appena si sceglie un'oreficeria (RIP-S-07).** Nello standalone il
   campo si spegne mentre si sceglie. Nel Go, senza JavaScript, un telefono scritto nello stesso
   salvataggio in cui la busta passa a un'oreficeria viene ignorato senza avviso: il server lo
   blocca come lo standalone. **Raccomandazione:** accettarlo fino al JavaScript.

Slice 034 (stati, riapertura e attesa). Nessuna blocca il lavoro.

5. **Stato uguale sulla scheda (RIP-S-13).** Sulla scheda lo standalone non confronta lo stato:
   «Cambia stato» verso lo stato che la busta ha già risponde «Transizione non consentita.»
   (`repair_live.ex:212-223`). Nella lista invece tace (`workshop_live.ex:222-229`, slice 185),
   perché un telefono che si riconnette rimanda i moduli. Il Go per ora copia lo standalone: sulla
   scheda l'errore, nella lista (T4.2) il silenzio. Capita solo con una pagina vecchia o un modulo
   rimandato. **Raccomandazione:** tacere anche sulla scheda (deviazione D-xx): nel Go ogni modulo
   è una richiesta HTTP, e un invio ripetuto o una pagina tornata indietro non devono mostrare un
   errore per una busta che è già dove deve stare.
6. **Righe `repair.viewed` dopo ogni azione della scheda.** Cambio stato, riapertura e attesa
   portano alla scheda con un redirect, che scrive una riga. Stessa questione della domanda 1.
   **Raccomandazione:** come la domanda 1.
7. **RIP-S-14, conferma della riapertura.** La finestra «Sei sicuro?» è JavaScript anche nello
   standalone (`data-confirm`); il Go la copia in `app.js`, ma nessun test la prova in un browser.
   **Raccomandazione:** lasciare la voce parziale fino al livello browser del banco.

Slice 035 (riquadro del link pubblico). Nessuna blocca il lavoro.

8. **«Genera nuovo link» senza `indirizzo_pubblico` (RIP-S-24).** Lo standalone non ha questo
   caso. Il Go oggi fa come la 031: il link si genera, e la scheda dice cosa scrivere nel file
   della macchina. Qui però costa di più che alla creazione della busta: il vecchio link smette
   di funzionare, e le iscrizioni Telegram e l'indirizzo email si tolgono, senza che si possa
   mostrare il link nuovo. **Raccomandazione:** con `indirizzo_pubblico` mancante rifiutare
   prima di toccare qualcosa, con un messaggio che dica cosa configurare (sul modello della
   D-32), e lasciare la creazione della busta com'è.
9. **Righe `repair.viewed` dopo revoca e nuovo link**, anche per il rifiuto al dipendente. Stessa
   questione delle domande 1 e 6. **Raccomandazione:** come la domanda 1.

## Note per lo standalone

1. **Impronte salvate come TEXT (slice 024, D-42).** Verificato dalla sessione dello standalone il
   31/10/2026: oltre a `historical_documents.sha256` anche `sale_photos.sha256` e
   `sale_revision_photos.sha256`. Domanda di allora: Lo standalone confronta o
   cerca mai `historical_documents.sha256` (controllo d'integrità, deduplica, pulizia dei file)? Se
   sì, con il valore salvato come TEXT quel confronto con un BLOB fallisce già oggi in produzione
   per ogni PDF storico arrivato da Flask. Da verificare dalla sessione dello standalone (Stefano,
   31/10/2026); il Go non ne dipende.

2. **Messaggi d'errore inglesi e numero di busta con zeri davanti (slice 030).** Stefano,
   31/10/2026: sono difetti dello standalone e si correggono lì in una slice sola; avvisa lui quando
   arriva su `main`. Fino ad allora il Go copia lo standalone: «can't be blank» / «is invalid» nel
   modulo della busta, e «012/26» accettato accanto a «12/26». Quando arriva: spostare
   `go-baseline`, copiare i testi italiani e la regola del numero, togliere questa nota.

## Risposte già registrate

Le domande 1–15 (slice 013b–018) sono state approvate da Stefano il 30/10/2026 e
registrate: deviazioni D-34…D-41, note nell'inventario (ACC-16, ADM-17, ADM-20, ADM-22, OPS-12) e
nel piano (T2, T4, T5, §7.3).

La domanda 1 della slice 020 (dettagli di ACC-16) è stata approvata da Stefano il 31/10/2026,
con tutte e quattro le raccomandazioni: il link di `gestrip primo-titolare` vale 60 minuti; alla
creazione del link si scrive la riga d'audit `administrator.bootstrap_requested` con attore
`console`; `/` e `/login` su un database vuoto non dicono niente di nuovo; nessun limite ai
tentativi su `/configura` (codice da 256 bit). Registrata nell'inventario (ACC-16).

Le domande 1–7 delle slice 022–024 sono state approvate da Stefano il 31/10/2026, tutte come
raccomandate; per la 7 solo la (a), senza correggere lo standalone per ora:
- righe nel cestino importate, righe incoerenti che bloccano: nota in OPS-03, che si chiude;
- spazio libero prima di copiare (2 × sorgente + media + 1 GiB), resti di un import interrotto
  da togliere a mano (scritto in `docs/guida-import.md`), `-senza-media` solo con `-sintetico`:
  nota in OPS-07;
- file media non nominati come avviso, coda delle cancellazioni prudente: nota in OPS-02;
- impronta TEXT di 32 byte riscritta BLOB con avviso nel report: deviazione D-42. Estesa lo stesso
  giorno a `sale_photos` e `sale_revision_photos` dopo la verifica della sessione dello standalone
  (causa unica: le scritture con `Repo.query` e un binario semplice salvano TEXT); lo standalone
  non verrà corretto, il banco sintetico copre foto di vendita caricate dall'interfaccia e una
  revisione di vendita.

La domanda su trigger e viste della sorgente (giro di mutazioni della 022c) è stata approvata da
Stefano il 31/10/2026 come raccomandata: un elenco di trigger e viste diverso dal riferimento
blocca l'import con `schema/oggetto_diverso`. Nota in OPS-01; si fa nella slice 025.

Le domande 1–2 della slice 025–026 sono state approvate da Stefano il 31/10/2026 come raccomandate:
- gli indici della sorgente non si confrontano (nota in OPS-01);
- in `sale_revision_photos.inserted_at` un istante senza zona si accetta come UTC, si scrive con
  la `Z` e il report dà l'avviso `traduzione/istante_senza_zona` per tabella con il numero di
  righe; ogni altra forma o colonna blocca. Registrata nella D-42 (stessa causa: scritture a mano
  con exqlite 0.40); si fa nella slice 027. La sessione dello standalone sta cercando tutte le
  colonne scritte a mano con lo stesso problema: quando arriva l'elenco si estende in una volta.
- elenco arrivato lo stesso giorno dalla sessione dello standalone (sola lettura, database
  sintetico): la regola vale per tutte e sole `sale_revision_photos.inserted_at`,
  `emergency_entries.inserted_at`, `emergency_events.closed_at` e `updated_at`,
  `repairs.retention_started_at` e `updated_at`; una colonna può avere righe con e senza `Z`,
  l'avviso conta solo quelle senza. Registrato nella D-42. Nella catena del banco si aggiunge il
  registro d'emergenza fatto con le funzioni dello standalone (slice 028).

Domande 1–4 delle slice 030 e 032 (Stefano, 31/10/2026): 1–2 difetti dello standalone, nota 2 qui
sopra; 3 → D-43; 4 → D-44 (inventario).
