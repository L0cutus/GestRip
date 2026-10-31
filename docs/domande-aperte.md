# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

Dalla slice 030 (nucleo della busta). Per tutte e tre il Go oggi copia lo standalone e non dipende
dalla risposta; la regola del 30/10/2026 (PIANO §7.3) dice di correggere prima nello standalone.

1. **Messaggi d'errore in inglese nel modulo della busta.** Lo standalone ha le traduzioni degli
   errori solo in inglese (`priv/gettext/en`), quindi una data d'accettazione vuota o sbagliata
   mostra «can't be blank» o «is invalid», e così la lavorazione vuota, su una pagina tutta in
   italiano. I messaggi scritti apposta («Inserisci il nome del cliente.», «Numero busta già
   esistente.» …) sono già in italiano. *Raccomandazione:* è un difetto dello standalone;
   correggerlo lì con i testi italiani («Campo obbligatorio.», «Data non valida.») e poi copiarli
   nel Go. Fino ad allora il Go mostra gli stessi testi inglesi.
2. **Numero di busta con zeri davanti.** «012/26» passa il formato `N/AA`, si salva com'è e alza il
   contatore a 12: «12/26» e «012/26» possono convivere come due buste diverse, e la ricerca e i
   link `/stato/<busta>/<anno>` le distinguono. *Raccomandazione:* difetto dello standalone;
   correggerlo lì rifiutando lo zero davanti (o togliendolo prima del controllo d'unicità), poi il
   Go. Fino ad allora il Go fa come lo standalone.
3. **Data d'accettazione con l'ora.** Lo standalone (cast `:date` di Ecto) accetta anche
   «2026-10-31T10:00:00», lo taglia al giorno, ma per l'anno del numero automatico usa oggi. Dal
   selettore della data non arriva mai: succede solo con una richiesta costruita a mano. Il Go
   accetta solo `AAAA-MM-GG` e rifiuta il resto con «is invalid». *Raccomandazione:* tenere il
   rifiuto del Go come differenza voluta (D-43), perché nessun utente lo vede; in alternativa
   copiarlo.

4. **Quante righe `repair.viewed` per un'apertura della scheda (slice 032).** Nello standalone la
   scheda è un LiveView: il browser la carica due volte (pagina e collegamento), e
   `record_repair_event` non è sotto `connected?` (`repair_live.ex:48-50`), quindi un'apertura vera
   scrive **due** righe d'audit. L'inventario (RIP-S-01) dice «ogni apertura scrive l'audit», e il
   Go ne scrive **una**. *Raccomandazione:* tenere una riga per apertura, come dice l'inventario e
   come chiunque leggerebbe il registro; la doppia riga è un effetto di LiveView, non una regola.
   Da registrare come differenza voluta (D-44), oppure da correggere prima nello standalone.

## Note per lo standalone

1. **Impronte salvate come TEXT (slice 024, D-42).** Verificato dalla sessione dello standalone il
   31/10/2026: oltre a `historical_documents.sha256` anche `sale_photos.sha256` e
   `sale_revision_photos.sha256`. Domanda di allora: Lo standalone confronta o
   cerca mai `historical_documents.sha256` (controllo d'integrità, deduplica, pulizia dei file)? Se
   sì, con il valore salvato come TEXT quel confronto con un BLOB fallisce già oggi in produzione
   per ogni PDF storico arrivato da Flask. Da verificare dalla sessione dello standalone (Stefano,
   31/10/2026); il Go non ne dipende.

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
