# Domande aperte per Stefano

Raccolte durante il lavoro per tappa (`docs/SVILUPPO.md`, «Modo di lavorare: via per tappa»). Ogni domanda ha la raccomandazione
della sessione; il lavoro che non ne dipende va avanti. Quando Stefano risponde, la domanda passa
nell'inventario o nel piano e qui si toglie.

## Aperte

1. **Import: righe nel cestino e righe incoerenti (OPS-03, slice 022).** Il precedente Flask→Elixir
   non importava le righe cancellate e saltava una riga incoerente scrivendo il motivo. Il Go:
   - **importa le righe cancellate** (`deleted_at`), perché ha il cestino come lo standalone: non
     portarle vorrebbe dire perdere il cestino al passaggio;
   - **blocca** l'import su una riga che non si traduce o che il Go rifiuta, invece di saltarla:
     il report dice tabella, id, colonna e motivo, e il database nuovo non si attiva.

   **Raccomandazione:** confermare tutte e due. Poi OPS-03 si chiude.
2. **Import: spazio libero prima di partire (OPS-07).** Lo standalone chiedeva 10 GiB più le
   dimensioni calcolate. Il Go oggi non controlla. **Raccomandazione:** con i media (slice 023)
   controllare lo spazio libero nella cartella della destinazione: almeno due volte la sorgente
   (copia di lavoro e database nuovo) più la cartella media, più 1 GiB di margine; sotto, rifiuto
   prima di copiare.
3. **Import: resti di un import ucciso di colpo.** Dopo un SIGKILL o una mancanza di corrente resta
   `.gestrip-import-*` accanto alla destinazione, con una copia in chiaro della sorgente (sul volume
   cifrato), e, se l'arresto cade fra i media e il database, anche `media/` senza database. Oggi un
   import nuovo **rifiuta** e chiede di togliere i resti a mano, e `gestrip serve` **non parte** se il
   database manca e accanto ci sono quei resti (non crea un database vuoto vicino alle foto).
   **Raccomandazione:** tenere così (niente cancellazioni automatiche di dati) e scriverlo nella
   guida dell'import.
4. **Import: file nella cartella media che nessuna riga nomina (slice 023).** Lo standalone ne ha
   per costruzione (foto tolte che aspettano la pulizia dopo un backup verificato, vecchio logo,
   temporanei). Il precedente Flask→Elixir bloccava. Oggi il Go li **conta nel report come avviso e
   non li copia**. **Raccomandazione:** tenere così; lo standalone resta intatto per il ritorno
   indietro.
5. **Import senza i media (`-senza-media`).** Oggi è accettata **solo con `-sintetico`**: un import
   autorizzato senza media darebbe un database con foto e firme che nominano file assenti, e il Go
   non ha modo di aggiungerli dopo. **Raccomandazione:** tenerla solo per le prove sintetiche (o
   toglierla).
6. **Import: coda delle cancellazioni dei media.** Un file in coda che c'è ancora si copia nel Go
   (la cancellazione resta da fare, nella tabella `cancellazioni_media`); se c'è ma con un contenuto
   diverso dalla chiave, l'import **si blocca**, mentre lo standalone lo cancellerebbe e basta.
   **Raccomandazione:** tenere tutte e due le scelte (prudenti); un caso così sui dati veri va
   guardato a mano.

## Risposte già registrate

Le domande 1–15 (slice 013b–018) sono state approvate da Stefano il 30/10/2026 e
registrate: deviazioni D-34…D-41, note nell'inventario (ACC-16, ADM-17, ADM-20, ADM-22, OPS-12) e
nel piano (T2, T4, T5, §7.3).

La domanda 1 della slice 020 (dettagli di ACC-16) è stata approvata da Stefano il 31/10/2026,
con tutte e quattro le raccomandazioni: il link di `gestrip primo-titolare` vale 60 minuti; alla
creazione del link si scrive la riga d'audit `administrator.bootstrap_requested` con attore
`console`; `/` e `/login` su un database vuoto non dicono niente di nuovo; nessun limite ai
tentativi su `/configura` (codice da 256 bit). Registrata nell'inventario (ACC-16).
