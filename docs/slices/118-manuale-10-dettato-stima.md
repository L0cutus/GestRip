# 118 — Manuale d'uso della T4.10: capitolo 10 «Dettato AI e stima dei tempi» (T4.10, diciottesima)

- **Fonte:** le sezioni «Per il manuale» delle ricevute 101–117, ogni frase controllata sul codice Go attuale. Regole del
  manuale: PIANO, «Manuale d'uso (Stefano, 03/11/2026)». Decisione K del piano della T4.10 (Stefano, 19/11/2026): capitolo
  10 nuovo, Backup e Amministrazione scalano a 11 e 12.
- **Voci:** nessuna voce cambiata (solo documentazione e ricette del banco). Nessuna D nuova (ultima D-112).
- **Implementatore: il modello locale** (decisione di Stefano), dentro `box --loc claude`, non un sottoagente Claude.
  Schermate, revisione e controlli su Opus. **È l'ultima slice fatta così:** dal 24/11/2026 sera, per decisione di
  Stefano, il porting usa **solo Opus** per tutto (implementatori, revisione, mutazioni); niente Sonnet e niente modelli
  locali.

## Cosa c'è

- **`docs/manuale/10-dettato-ai-e-stima-dei-tempi.md`** (nuovo): stima dei tempi sulla scheda e nelle impostazioni (quando
  compare, i quattro casi in cui la pagina del cliente non la mostra, supplemento artigiano), «Dettato AI» nella nuova busta
  (dettato scritto e «🎤 Detta a voce», «✦ Estrai con AI», cosa viene compilato e in che ordine), configurazione
  dell'assistente (solo titolare), raccolta dei dettati (lotti da 500, esclusione di un cliente dalla scheda, scadenza a 12
  mesi), esportazione dei dettati in breve, messaggi d'errore e cosa fare, comportamento senza JavaScript.
- **`00-sommario.md`:** il capitolo 10 è un link; Backup 11, Amministrazione 12, Glossario 13; il video della posta rimanda
  al capitolo 12. **`99-glossario.md`:** voci nuove del capitolo. **`da-scrivere.md`:** sezione nuova «Esportazione dei
  dettati (slice 117) *(per il capitolo 12 Amministrazione)*» con comando, opzioni e messaggi (come escono davvero, con il
  prefisso «gestrip: »); il Profilo del negozio passa al capitolo 12.
- **Schermate:** otto nuove in `bench/manuale/schermate.json` con le ricette in `tools/banco/browser/manuale.mjs`
  (`preparaStima` crea 22 buste pronte perché la stima vuole almeno 20 buste, `configuraAssistente`, `bustaDalDettato`,
  `estrai`, `modelloFinto` che programma il modello finto del banco da Node, `wavProva` con una clip WAV inventata). La voce
  dell'assistente configurato è l'ultima: punta l'assistente su un indirizzo con una chiave e dopo il dettato non risponde
  più. Il giro ha rigenerato anche otto PNG dei capitoli 03, 04, 06 e 09 (interfaccia cambiata dopo la 099; ok del
  coordinatore).
- **`docs/domande-aperte.md`:** «Difetti in lista», nota 4: quattro difetti BASSI della 117 da una revisione Opus
  indipendente, solo in lista (coordinatore, ok di Stefano del 24/11/2026).

## Il modello locale: giri, tempi, interruzioni

| Giro | Modello | Orario | Chiamate | Esito |
|---|---|---|---|---|
| 0 | Inkling 2 Mini (`basalt-inkling-mx8-256k-vision`) | 14:42–14:59 (17 min) | 91 | fermato dal coordinatore con l'ok di Stefano («ferma e rilancia su Strata»): llm-switch era stato portato su Inkling alle 14:39 da Stefano, che pensava che le slice le facesse Claude. Capitolo a metà (459 righe), buttato; copia in `to-be-deleted-after-use/slice-118/inkling-giro1/` fino alla fine della slice |
| 1 | Strata Gyro S (`strata-gyro-s-224k-think6144`) | 15:00–15:21 (~20 min) | 192 | capitolo, sommario, glossario, da-scrivere scritti; **chiuso da Claude Code per memoria** (1-2 GB disponibili con Strata acceso) mentre lanciava `scripts/check` completo, prima del resoconto |
| 2 | Strata | 15:21–15:30 (8 min) | 89 | solo resoconto, con il divieto di test e check: rispettato |
| 3 | Strata | 15:46–15:56 (10 min) | 82 | correzioni della revisione (9 MEDIE e le piccole): tutte «fatto», nessun comando pesante |
| 4 | Strata | 15:58–16:03 (5 min) | 63 | due frasi sulla privacy dell'audio e del dettato ancora più larghe del codice (glossario «Trascrizione», capitolo 10 «Detta a voce») |

- **Errori tipici del modello locale** (dalla revisione): nessun testo inventato (circa 180 testi tra caporali, tutti
  identici al codice), capitolo completo rispetto alle ricevute; gli errori sono di **interpretazione**: comportamento reale
  del browser (`min`/`max` dei campi numerici) letto come correzione del server, condizioni lette male («buste aperte»
  invece di tutte, «ordine del dettato» invece di un ordine fisso, modulo senza JavaScript), una **promessa sulla privacy
  dell'audio allargata** a un servizio esterno, troppi dettagli tecnici per chi usa il programma (comando, percorsi,
  codici).
- **Interruzioni:** una sola chiusura per memoria (giro 1, da Claude Code); il giro 0 l'ha fermato il coordinatore.
- **Memoria:** con Strata acceso restano ~2 GB: `scripts/check` completo non ci sta. Nei brief per il modello locale vanno
  vietati check e suite intera quando il modello è sulla stessa macchina; il check lo rilancia la sessione principale in una
  finestra di «memoria libera».

## Prove

- `scripts/check` rilanciato dalla sessione principale con Strata spento: verde sui testi del primo giro e di nuovo dopo
  le ricette delle schermate (voci 353: chiuse 53, rosse 0; segreti e dati veri OK). Dopo il terzo giro (solo `.md`):
  `scripts/githooks/pre-commit --tutto` e controllo dei link; la suite intera la rifà la CI.
- **Banco manuale:** giro m118a caduto su due ricette della stima (timeout dopo «Salva modifiche»), ricetta corretta; giro
  m118b verde (38 PNG). Guardia sotto 10 GB attiva, minimo visto 26,4 GB; `banco controlla` senza avanzi. Le otto
  schermate nuove guardate una per una dall'aiutante; `10-dettato-desktop` anche dalla sessione principale.

## Mutazioni

- Nessuna: solo documentazione e ricette del banco (le ricette le prova il giro del banco manuale).

## Review (Opus, indipendente)

- **Revisione sul testo del giro 1:** 0 ALTA, 9 MEDIA, una decina di BASSE. Le MEDIE: valori fuori campo (stima nelle
  impostazioni e «Giorni in più») bloccati dal browser e non corretti dal server; ordine fisso delle note («Busta», «Acconto»,
  altre); buste con lo stesso numero contate tutte, non solo le aperte; modulo aperto dopo l'estrazione anche senza
  JavaScript; promessa sull'audio allargata al servizio esterno; quarto caso in cui la stima non compare (meno di 20 buste);
  «stima spenta» vero solo se la stima è stata accesa; sezione «Esportare i dettati» troppo tecnica e messaggi senza il
  prefisso «gestrip: ». Giudizio: «molto fedele», circa 180 testi tra caporali tutti identici al codice.
- **Controllo delle correzioni (giro 3):** le 9 MEDIE corrette, nessun errore nuovo; restavano due frasi sulla privacy
  più larghe del codice (glossario «Trascrizione», «non va nei registri»).
- **Controllo del giro 4:** le due frasi giuste; grep di tutto `docs/manuale/` senza altre promesse sulla privacy più
  larghe del codice.
- Le correzioni del testo le ha fatte sempre il modello locale (regola di Stefano), mai la sessione principale.

## Per il manuale

- È la slice del manuale: il capitolo 10 è nuovo, il sommario cambia numerazione (Backup 11, Amministrazione 12, Glossario
  13).

## Differenze dallo standalone

- Nessuna nuova.

## Dubbi

1. Il Chromium del banco ha solo l'interfaccia inglese: nelle schermate `10-dettato-desktop` e
   `10-dettato-non-configurato-desktop` le date dei campi si vedono come «03/15/2026» (limite già scritto in SVILUPPO).
2. `10-assistente-desktop`: in fondo si vede un filo del bordo della sezione successiva; `10-scheda-escludi-cliente-telefono`
   ritaglia solo il riquadro del pulsante.
3. Dubbi del modello locale: in `da-scrivere.md` la riga sulla stima nella sezione «Pagina del cliente» resta finché quel
   capitolo non è scritto; regola «una parola per riga» contro la virgola e massimo di 13 cifre del telefono, dove il
   capitolo è più stretto del codice (non corretti).
