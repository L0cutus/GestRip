# 101 — Stima dei tempi sulla scheda (T4.10, prima)

- **Fonte:** standalone a `go-baseline`: `RepairEstimate.for_shop`, `repair_live.ex` (righe ~718-736 e ~2370, riquadro
  della scheda). Inventario RIP-S-03, RIP-H-55 (parte negozio); CLI-03 già chiusa con la 061.
- **Voci:** RIP-S-03 (parziale fino all'esito del banco, come sempre). RIP-H-55 si chiude con la 102. Nessuna D nuova.

## Piano della T4.10

Tabella copiata da `~/Condivisi/GestRip-Go/piano-T4.10-bozza.md` (approvata da Stefano il 19/11/2026, «ok a tutto»:
valgono le raccomandazioni A–L; D-103, D-104 e D-105 registrate dal coordinatore; la colonna Modello dice chi scrive il
codice, revisione e mutazioni sempre Opus). Le domande A–L e le incongruenze sono nel file del piano; le correzioni al
PIANO e all'inventario le fa il coordinatore.

| Slice | Cosa | Voci | Dipende da | Banco | Rischi | Modello |
|---|---|---|---|---|---|---|
| **101** | Stima dei tempi sulla scheda (`RepairEstimate.for_shop`): per le buste in lavorazione, preventivo e in attesa materiali «Pronta di solito in N-M giorni dal ritiro · su X buste» (testo dello standalone; corretto il 19/11/2026), «Dati insufficienti: servono almeno 20 buste pronte (ora N)», «Giorni in più» e perché il cliente non la vede (stima spenta o data di consegna scritta); riusa il modello della 061, leggere non scrive | RIP-S-03; RIP-H-55 (parte negozio) | 061 | HTTP scheda di buste della catena nei tre stati e in uno stato chiuso, con stima spenta e accesa; browser no | basso/medio: la catena ha probabilmente meno di 20 buste pronte, quindi il banco vede solo «Dati insufficienti»; il caso pieno lo coprono i test Go | **Sonnet** (pagina, riuso) |
| **102** | Stima nelle impostazioni del negozio `/settings/shop` (`save_estimate`): interruttore, supplemento artigiano min/max riportato in 0–90, massimo mai sotto il minimo, default 7–8, spenta finché il titolare non la accende anche dopo un import, audit; solo titolare | ADM-04; chiude RIP-H-55 | 087, 101 | HTTP salvataggio nelle due app, rifiuto del dipendente, valori fuori campo; `in_fondo` (la leggono 061 e 101); browser salvataggio che alla fine rimette i valori | basso | **Sonnet** (impostazioni) |
| **103** | Date di consegna calcolate dal codice (D-18): porting di `consegna.py` in `internal/assist` (oggi, domani, giorni della settimana, «il 30», «il 5 novembre», «fra N giorni/settimane/mesi», «entro fine mese»; nessuna frase riconosciuta → vuota); esclusione delle date da incidere; banco di frasi con un «oggi» fisso, almeno 30 frasi più i casi negativi, atteso = quello che dà `consegna.py` | RIP-H-42 (parte date), RIP-H-46 (D-18) | — | nessuno scenario: tabella di test; confronto una tantum con `consegna.py` su una copia in `to-be-deleted-after-use/` | medio: date sbagliate finiscono nella consegna prevista; fine mese, anno nuovo, «venerdì» detto di venerdì (domanda D) | **Opus** (logica delicata) |
| **104** | Dalla risposta del modello alla scheda (il lavoro che oggi fa il traduttore di pc-fisso): lettura tollerante della risposta (JSON dentro markdown o con testo intorno, campo non semplice → illeggibile), D-19 (un solo nuovo tentativo se la risposta non è valida), «Busta N» e «Acconto N €» nelle note (D-20), compro oro solo da «oro vecchio» / «scheda compro oro», telefono normalizzato (e letto dal dettato se Stefano dice sì, domanda E), data di oggi | RIP-H-42, RIP-H-43 (parte) | 103 | nessuno: test con le risposte dei banchi `bench/modello/` usate come risposte finte | medio/alto: è il punto dove il Go diventa diverso dallo standalone; formato della risposta da decidere (domanda B) | **Opus** (logica delicata) |
| **105** | Regole su nome e oreficeria: nome dall'apertura («Cliente/Signora/Signor…»), maiuscole come `str.title()`, il nome deve essere una persona, oreficeria per contenimento (prima in ordine alfabetico), riconoscimento del cliente solo nei casi certi (telefono ≥ 7 cifre senza 0039/39, o nome uguale a parte accenti e spazi), «✓ Nome cliente corretto da «X» a «Y»…», coppia nome/telefono | RIP-H-43 (resto) | 104, 086 | nessuno scenario (si vede con la 108); tabelle dai test `assist_test.exs` | medio: un cliente riconosciuto per sbaglio finisce sulla busta di un altro | **Opus** (dati dei clienti) |
| **106** | Collegamento al modello e modello finto: richiesta compatibile OpenAI (`/v1/chat/completions`, istruzioni come messaggio di sistema, dettato come messaggio utente, `temperature 0`, attesa 90 s), nome del modello che ha risposto, rifiuti («Nessun modello è configurato…», testo 10–5000 caratteri, «AI temporaneamente non disponibile…»), audit `assist.extract` con soli conteggi, mai testo né chiave nei log; server finto per test e banco (`tools/banco`, come il bot finto della 074b) | RIP-H-46 (parte estrazione), RIP-H-47, RIP-H-45 (parte) | 104 | nessuno scenario nuovo; test con il server finto (errore 500, attesa scaduta, risposta illeggibile e poi buona) | alto: il dettato contiene nomi e telefoni; un errore non deve mai portarli nei log | **Opus** (privacy, rete) |
| **107** | Pagina `/settings/assistente`, parte configurazione: estrazione (indirizzo, modello, chiave) e trascrizione (indirizzo, chiave, glossario), indirizzo solo `http(s)://` con host, chiave mai mostrata e mai in chiaro verso un'altra macchina, limiti 255/120/255/4000, «casella vuota = tieni», chiavi in `segreti_provider`, prova con la frase fissa senza dati di clienti; solo titolare | ADM-11; RIP-H-46 (parte impostazioni) | 106, 069 | HTTP nelle due app puntate al modello finto: salvataggio, rifiuto del dipendente, `http` con chiave verso un altro host, prova; browser salvataggio che alla fine rimette | alto: segreti dei provider | **Opus** (segreti) |
| **108** | Nuova busta con «Dettato AI»: «Descrizione libera» e «✦ Estrai con AI» riempiono il modulo, sempre correggibile prima del salvataggio; «✓ Dati estratti. Verifica e correggi prima di salvare.»; messaggi d'errore; un errore non tocca il modulo; senza JavaScript la pagina torna con il modulo riempito | RIP-H-42, RIP-H-45 | 105–107 | HTTP `nuova` allargato nelle due app puntate al modello finto, con D-18 e D-20 fra le differenze attese (`bench/deviazioni.json`); browser dettato e salvataggio di una busta propria del 2025 | alto: riempie i dati di una busta; differenze volute da tenere separate dai bug | **Opus** (dati della busta) |
| **109** | Trascrizione, parte server: interfaccia `Transcriber`, motore finto, motore «server esterno» `{endpoint}/stt` come oggi (intestazione `X-Gestrip-Glossario` su una riga, max 2000 caratteri, sempre presente; attesa 35 s), mezzo secondo di silenzio prima e dopo per il motore locale, caricamento della clip con `csrf.ModuloConFile` (min 2000 byte, max 25 MiB, formati), audit `assist.transcribe` con soli conteggi, nessun audio conservato | RIP-H-44 (parte server), RIP-H-46 (parte trascrizione), RIP-H-47 | 107 | HTTP caricamento di una clip sintetica nelle due app con il trascrittore finto: troppo corta, formato sbagliato, non configurata, riuscita | alto: file in arrivo con la voce dei clienti | **Opus** (privacy, file) |
| **110** | Microfono nel browser: «🎤 Detta a voce», «🔴 Sto ascoltando… (tocca per fermare)», registrazione WAV mono 16 kHz con AudioWorklet (D-25), «⚠ Troppo breve, riprova.», contesto non sicuro, microfono negato, testo aggiunto in fondo alla casella | RIP-H-44 (D-25) | 109, 108 | browser con il microfono finto di Chromium (file WAV sintetico) nelle due app: lo standalone manda webm, il Go WAV; si confronta il testo che compare | medio/alto: audio nel browser senza testa, ricampionamento a 16 kHz | **Opus** (formato audio) |
| **111** | Sidecar: `internal/sidecar` avvia il modello (e, se c'è, la trascrizione) come processi figli su 127.0.0.1 a una porta alta, controlla `/health`, riavvia con attese crescenti, GestRip resta su anche se il sidecar non parte; prova unica con un modello vero solo su CPU (domanda F) | — (PIANO §1.8) | 106 | nessuno scenario; test con un figlio finto; una corsa vera sotto `systemd-run` con limiti, porta sopra 47000, solo con il «memoria libera» | alto: processi figli, RAM di pc-fisso, mai le porte di gestrip-switch | **Opus** (processi, risorse) |
| **112** | Banco del modello: `gestrip verifica-modello` (manda ogni caso come la nuova busta, legge con le stesse regole, punteggio per campo, mediana/p90/max dei tempi; tollera solo maiuscole, spazi e «400-500» = «400/500»; non stampa mai il dettato, non manda chiavi) sui 84 casi dello standalone e sui banchi B, C, D, D2 | RIP-H-54, OPS-08 | 104–106 | nessuno scenario; test con il modello finto; una corsa vera come nella 111 | medio: è lo strumento che deciderà il modello della T5.1 | **Opus** (misura) |
| **113** | Raccolta dei dettati al salvataggio: con la raccolta accesa e la busta compilata dal dettato della stessa pagina, testo, proposta e nome del modello nella stessa transazione della busta; lotto di 500 con scrittura protetta, l'ultima spegne la raccolta (audit `system`); cliente escluso → nessuna riga; un errore non blocca mai la busta | RIP-H-48 | 108 | HTTP nuova busta con estrazione e raccolta accesa nelle due app (`in_fondo`), contatore delle buste rimaste | alto: dati personali salvati, due salvataggi all'ultima unità | **Opus** (dati) |
| **114** | Pagina Assistente, parte raccolta: interruttore con il testo della D-07 (dati solo sull'apparecchio, spenta finché il titolare non la accende, modello cedibile), nuovo lotto solo a lotto finito e con conferma, esclusione di una busta per numero (`buste.NormalizzaNumero`, anche nel cestino) e di un cliente; solo numeri, mai un dettato; audit con numeri | RIP-H-49, RIP-H-50, ADM-12 | 107, 113 | HTTP nelle due app (`in_fondo`), con il testo D-07 fra le differenze attese; browser accensione e spegnimento che rimette | alto: cancellazioni irreversibili | **Opus** (cancellazioni) |
| **115** | Scheda: «🎙️ Escludi dalla raccolta dettati» (solo titolare, solo se la raccolta è stata accesa almeno una volta, conferma, secondo tocco senza scritture, «Solo il titolare può escludere un cliente.»); scadenza a 12 mesi nella pulizia giornaliera con audit `assist.training_expired` solo se cancella | RIP-S-26; RIP-H-51 (parte durata) | 114 | HTTP scheda titolare e dipendente; scadenza con orologio finto nei test | alto: cancellazioni | **Opus** (cancellazioni) |
| **116** | Pseudonimi per l'esportazione: nome e telefono sostituiti da valori inventati con una chiave usata una volta, righe dubbie scartate con il motivo (nome non trovato nel testo, telefoni sconosciuti, altri clienti, nomi propri comuni, cliente escluso) | RIP-H-52 (parte) | 113 | nessuno: tabelle dai test `pseudonym_test.exs` | alto: un nome vero che sfugge esce dall'apparecchio | **Opus** (privacy) |
| **117** | Comando `gestrip esporta-dettati FILE.jsonl`: file nuovo mai sovrascritto, solo del proprietario, dati attuali della busta, numero di busta mai scritto, stampa solo numeri; cancellazione delle righe solo dopo file scritto, sincronizzato e chiuso; in errore niente cancellato e file a metà tolto | RIP-H-52, OPS-09 | 116 | nessuno scenario; test del comando su un database di prova | alto: cancella dopo aver scritto | **Opus** (dati, cancellazioni) |
| **118** | Capitolo del manuale della tappa dalle sezioni «Per il manuale» 101–117, con le schermate (domanda K) | — | 101–117 | browser per le schermate | basso | **Sonnet** (testi) |

## Cosa c'è

- **`internal/buste/stima_scheda.go`** (nuovo): `stimaDellaScheda`, la stima per il negozio. Riusa il modello della 061:
  `pagina_cliente.go` è solo riordinato (stessi numeri, `perLaBusta` condiviso) e ora dà anche i conti del campione
  («ora N» conta le buste di bottega, «su X buste» anche quelle passate dall'artigiano).
- **`internal/buste/scheda.go`:** `leggiScheda` legge la stima (`Scheda.Stima`) solo per le buste in lavorazione,
  preventivo e in attesa materiali, mai per il compro oro. Il supplemento dell'artigiano vale anche dopo il rientro, come
  lo standalone. **`consegna.go`:** `Consegna.esiste()` come `date_source/2` dello standalone.
- **`internal/web/pagine/scheda.html`, `scheda.go`:** riquadro `#repair-time-estimate` dopo «Consegna entro»; «Giorni
  in più» solo sopra zero, in `.blocco-piccolo` (nuova in `app.css`, senza opacità come lo standalone); i due motivi
  per cui il cliente non la vede («stima spenta nelle impostazioni», «c'è una data di consegna») solo accanto a un
  intervallo, «spenta» per prima. Leggere la scheda non scrive.
- **Banco:** `bench/scenari/scheda-stima.json` (solo con `sintetici-import`): buste 1, 5, 10, 24 aperte, 2 chiusa, 34
  compro oro, da titolare; la 5 anche da dipendente. Confronta testo e presenza del riquadro, «Giorni in più», stato e
  consegna. Il numero N di «(ora N)» non è fissato: si confronta fra le due app (controlla anche l'import).
- **SVILUPPO:** sezione «Stima dei tempi sulla scheda (slice 101…)».

## Scelte

1. **Testo dello standalone, non del piano:** il piano scriveva «N–M giorni · su X buste»; lo standalone scrive «Pronta
   di solito in N-M giorni dal ritiro · su X buste». Si è seguito lo standalone (ok del coordinatore, che corregge il
   piano).
2. **Solo stima spenta nel banco:** l'interruttore del Go arriva con la 102 e con meno di 20 buste pronte la scheda è
   uguale accesa o spenta. **Obbligo della 102 (coordinatore, 19/11/2026):** aggiungere allo scenario `scheda-stima` il
   caso «accesa», dopo il salvataggio dell'impostazione.

## Prove

- Rosso: `buste` non compilava, i test web cadevano sul riquadro assente. Test nuovi: 7 in `buste` (stima piena con i
  tre perché, supplemento e giorni in più, busta già rientrata dall'artigiano, dati insufficienti, campione con
  cestino, compro oro e oltre 90 giorni, stati aperti, stessa stima della pagina del cliente, leggere non scrive), 5
  `TestSchedaStima*` in `web` (testi esatti, ordine, titolare e dipendente, buste chiuse, leggere non scrive).
- `scripts/check` verde, rilanciato dalla sessione principale prima e dopo le correzioni.
- **Mutazioni dell'implementatore:** 31, 27 uccise, 3 scartate (non compilano o rompono il template), 2 equivalenti
  (compro oro negli stati aperti: il CHECK del database non lo permette); poi 3 sull'artigiano rifatte, tutte uccise.
- **Revisione indipendente (Opus):** 0 ALTE; 1 MEDIA chiusa (test della busta rientrata dall'artigiano: la mutazione M11
  sopravviveva); BASSE: classe di «Giorni in più» senza opacità, commento su `max(GiorniInPiu,0)`; costo della lettura
  (scansione di `buste` senza indice su `data_pronta`, come lo standalone) lasciato, un indice solo se il banco lo
  mostra lento. Mutazioni del revisore: 24 compilanti, 20 uccise, 3 equivalenti, 1 viva poi uccisa.

## Banco

Scenario nuovo `scheda-stima` (vedi «Cosa c'è»). Il banco vede solo «Dati insufficienti (ora N)»: la catena ha meno
di 20 buste pronte.

**Giro s101a (19/11/2026): rosso per lo scenario della slice.** Una sola inattesa, `scheda-stima[4]`: lo scenario
attendeva «Preventivo» sulla busta 9, ma lo scenario `modifica` cambia lo stato della 9 durante il giro, e le due app
mostravano tutte e due «In lavorazione». Corretto: il passo usa la busta 10, già «Preventivo» stabile per lo scenario
`stati`, senza data di consegna né giorni in più come la 9 (ok del coordinatore per il giro dopo).
**Giro s101b: rosso per la nota 8** (decima comparsa, la prima con i lavori dello standalone spenti dalla 100): vista
telefono, `sessioni-rientro`, fase registro, «500 GET /audit» dopo 5019 ms, `database is locked` nel log dello
standalone; 1 errore e 10 inattese, tutte dello standalone. HTTP verde (87 scenari, 0 inattese).
**Giro identico s101c: VERDE.** HTTP 87 scenari, 1360 passi, 0 inattese, 0 errori; browser 29 scenari, 58 viste, 0
inattese, 0 errori. Memoria disponibile minima 19,7 GB. Copertura con l'esito: 353 voci, chiuse 214 (RIP-S-03), rosse
0, parziali 16, aperte 112, fuori v1 11.

## Per il manuale

- Nella scheda di una busta in lavorazione, in preventivo o in attesa materiali compare «Stima tempi».
- Con almeno 20 buste pronte negli ultimi 12 mesi dice «Pronta di solito in N-M giorni dal ritiro · su X buste»; con
  meno dice «Dati insufficienti: servono almeno 20 buste pronte (ora N)».
- Se la busta ha «Giorni in più» (più di zero), lo scrive sotto.
- La stima la vede sempre il negozio. Sotto la stima la scheda dice perché il cliente non la vede: «stima spenta nelle
  impostazioni» oppure «c'è una data di consegna».
- Una busta pronta, consegnata, annullata o compro oro non ha il riquadro. Guardare la scheda non cambia niente.

## Dubbi

1. **Scenario quando la catena avrà 20 o più buste pronte:** le attese «Dati insufficienti: presente» e «non mostrata ai
   clienti: assente» andranno cambiate; fallirebbero con un errore chiaro, non in silenzio.
2. **Giro a cavallo della mezzanotte:** N potrebbe cambiare fra le due app; finora mai visto.
