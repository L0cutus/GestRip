# 099 — Manuale d'uso della T4.9: capitolo 09 «Oreficerie» (T4.9, quattordicesima e ultima)

- **Fonte:** le sezioni «Per il manuale» delle ricevute 086–098, ogni frase controllata sul codice Go attuale
  (`internal/web`, `internal/web/pagine`, `internal/buste`, `internal/jobs`, `app.js`). Regole del manuale: PIANO,
  «Manuale d'uso (Stefano, 03/11/2026)».
- **Voci:** nessuna voce cambiata (solo documentazione e ricette del banco). Con questa slice la **T4.9 è chiusa**.

## Cosa c'è

- **`docs/manuale/09-oreficerie.md`** (nuovo): anagrafica delle oreficerie, busta di un'oreficeria, lotti (apertura,
  tre modi di aggiungere una busta, «📷 Scansiona», «Chiudi e invia», rifiuti), pagina pubblica `/lotto`, storico e
  reinvio, comportamento senza JavaScript. Dice quando un'azione è solo del titolare (dati del negozio, «Messaggi
  WhatsApp»).
- **`00-sommario.md`:** il capitolo 9 è un link. **`99-glossario.md`:** Avviso di ritiro, Link del lotto, Lotto,
  Oreficeria, Reinvio; ritoccata «Busta di un'oreficeria».
- **`da-scrivere.md`:** tre sezioni nuove: «Oreficerie, per i capitoli 01, 02 e 07»; «Profilo del negozio (slice 087 e
  D-97)» per il capitolo 11; «Promemoria automatici (slice 094–098) → capitolo 07 Messaggi» (i lavori automatici non
  hanno un capitolo nel sommario; ok del coordinatore del 19/11/2026).
- **Schermate:** sei nuove in `bench/manuale/schermate.json` con le ricette in `tools/banco/browser/manuale.mjs`
  (`oreficerie-anagrafica`, `lotti-lista`, `lotto-pagina`, `scheda-in-lotto`, `storico-lotto`, `lotto-pubblico`, più
  `assicuraOreficeria`, `preparaLotto`, `fissaOra`). `fissaOra` scrive fisse le date delle pagine dei lotti
  («15/03/2026 10:30»), così le figure sono coerenti e non cambiano a ogni giro. `storico-lotto` va prima di
  `lotto-pubblico`, perché leggere la pagina pubblica chiude il lotto.
- **Schermate rifatte:** il giro ha rigenerato anche dodici PNG dei capitoli 03–06, perché l'interfaccia è cambiata
  dopo la 059 (per esempio le schede del guscio).

## Prove

- `scripts/check` verde, rilanciato dalla sessione principale.
- **Revisione indipendente (Opus, sola lettura):** 0 ALTE; 3 MEDIE corrette (il numero del reinvio conta anche le
  buste ritirate; «sempre» tolto dai messaggi che finiscono con «Niente è partito.»; «Messaggi WhatsApp» solo del
  titolare); BASSE corrette (busta nel cestino → «Riparazione non trovata.», nome del negozio bloccato solo a maiuscole
  esatte, « · N nel lotto» anche sulle righe di errore del modo continuo, ordine dei campi del profilo, niente link al
  capitolo 7 non scritto, tolta la spiegazione del difetto «1 buste» come vuole la regola di `da-scrivere.md`).
- **Banco manuale:** giro m099a verde al primo colpo (30 schermate); dopo le correzioni giro m099b verde (30 schermate, memoria disponibile minima 24,9 GB), date coerenti.

## Per il manuale

È la slice del manuale: il capitolo 09 è il risultato. Nessun comportamento del programma cambiato.

## Dubbi

1. **Campo dello scanner del lotto schiacciato sul telefono** (difetto dell'interfaccia Go, non corretto qui perché
   la slice è solo di manuale): nella pagina del lotto, vista telefono, il campo `#ready-lot-scan-code` («Scansiona o
   inserisci il codice QR») si riduce a una fessura accanto a «📷 Scansiona» e «Aggiungi dal QR»: in
   `statici/app.css` `.lotto-qr input` ha `flex: 1; min-width: 0` dentro un contenitore con `flex-wrap`, quindi non
   va mai a capo. Si vede nella schermata `09-lotto-pagina-telefono.png`. **Raccomandazione:** una correzione piccola
   (un `min-width`, per esempio 12rem) in una slice prossima, poi rifare la schermata.
2. **Testo per busta dell'invio del lotto:** la ricevuta 091 lo chiama «Avviso oreficeria», il codice lo etichetta
   «Aggiornamento oreficeria» (famiglia `wa_oreficeria`): nel capitolo c'è il nome del codice.
3. **Niente schermata del riquadro «Lotto inviato»:** il Go del banco non ha `indirizzo_pubblico` e la conferma è
   una finestra del browser. Servirebbe una ricetta che cambia i testi; per ora il riquadro è descritto a parole.
4. **Lotti vecchi con buste** («dopo la v1» nel PIANO): il capitolo dice come ritrovarli da «Apri il lotto» nella
   scheda di una loro busta; ricavato dal codice, non provato nel browser.
