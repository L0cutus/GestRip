# 048 — Il cookie dell'accesso dura 30 giorni (D-31, allineamento alla slice 223 dello standalone)

- **Fonte:** coordinatore, 04/11/2026. La sessione dello standalone ha trovato, nella sua slice 223
  (`ae37a8c`, poi `5302abc` con l'immagine `fab-223`), che il cookie di sessione era senza
  `Max-Age` né `Expires`. Il telefono lo perdeva chiudendo il browser o la PWA, e Stefano rifaceva il
  login più volte al giorno. Il Go aveva lo stesso difetto: `internal/web/sessione.go` diceva «il
  cookie resta senza Max-Age, come quello dello standalone».
- **Voci:** ACC-01, ACC-03, ACC-04, ACC-17; D-31 (30 giorni assoluti dall'accesso, già controllati
  dal server). Con la 223 lo standalone fa lo stesso: è un **allineamento atteso**. `go-baseline`
  resta a `f80f191` finché Stefano non autorizza lo spostamento a `5302abc`.
  - 07/11/2026: con l'ok di Stefano `go-baseline` è passato a `d3778df` (slice 224, che contiene la
    223), banco su `fab-224`: vedi `055b-allineamento-224.md`.
- **Lavoro:** implementazione e mutazioni di un sottoagente, review di un altro. Commit, inventario
  privato e documenti li fa la sessione principale.

## Cosa c'è

- **Cookie di sessione** (`sessioni.scrivi`, chiamata solo da `salva` con l'orologio della faccia):
  - con un accesso (utente e istante), `Max-Age` è il tempo che resta fino ad accesso + 30 giorni,
    almeno 1 secondo. Il browser butta il cookie quando il server lo rifiuterebbe comunque, e una
    riscrittura (un messaggio dopo un redirect, un ritorno) non lo allunga;
  - senza accesso (solo la pagina di ritorno o un messaggio, dopo l'uscita o la scadenza) resta senza
    `Max-Age`, come prima;
  - lo standalone scrive invece sempre 30 giorni pieni a ogni riscrittura: il limite che conta resta
    quello del server, in tutti e due;
  - gli altri attributi restano gli stessi (Path=/, Secure, HttpOnly, SameSite=Lax, prefisso
    `__Host-`); l'uscita lo cancella con `Max-Age` −1, come prima.
- **Cookie CSRF** (`CSRF.impostaCookie`): `Max-Age` fisso di 30 giorni. Nello standalone il segreto
  CSRF sta dentro il cookie di sessione e ora dura 30 giorni; nel Go è un cookie a parte, e senza
  scadenza sarebbe sparito alla chiusura del browser mentre l'accesso restava: le pagine ripristinate
  dal telefono avrebbero dato 403 al primo invio. Il rinnovo all'accesso e all'uscita resta (D-67).
- **D-31 sul server**, invariata e già provata: `inTempo` rifiuta 30 giorni esatti, un istante mancante
  (0) e uno più di 5 minuti nel futuro (`TestSessioneDuraTrentaGiorni`,
  `TestSessioneSenzaAccessoValido`). È la stessa regola che la 223 ha portato nello standalone.
- **D-67** (Stefano, 04/11/2026), registrata con questo commit: un modulo caricato prima
  dell'accesso non vale più dopo il login, perché all'accesso il segreto CSRF si rinnova. Lo
  standalone lo accetta. Comportamento del Go invariato, solo dichiarato.

## Prove

- `TestMaxAgeDelCookieDiSessione` (orologio finto): senza accesso niente `Max-Age`; al login
  2592000; dopo 10 giorni e 30 s un messaggio che riscrive il cookie dà 20 giorni meno 30 s, anche
  alla pagina dopo; un secondo prima della scadenza 1; sessione scaduta riscritta senza `Max-Age`;
  `scrivi` con un accesso di 31 giorni fa dà 1, mai 0 o negativo.
- `TestLoginGiusto`, `TestCookieCSRF`, `TestCookieDellaFacciaComeLoStandalone`: 30 giorni sul cookie
  di sessione del login e sul CSRF, attributi invariati, niente `Expires`.
- Mutazioni a mano, 5 su 5 prese: sessione senza `Max-Age`, sessione a 30 giorni fissi, `Max-Age`
  anche senza accesso, minimo 0 invece di 1, CSRF senza `Max-Age`.
- `scripts/check` verde.
- Banco (`fab-222`, dati importati), giro mirato su `accesso`, `csrf-intestazioni`,
  `guscio-dipendente`, `guscio-titolare`, `pwa-guscio`: HTTP verde (5 scenari, 50 passi, 0 inattese).
  Il livello browser, che gira sempre per intero, ha dato rosso solo in `conferme` e `firma-riquadro`,
  uguale nelle due app («Foto (0)» in tutte e due): quegli scenari usano le foto caricate dallo
  scenario HTTP `foto`, lasciato fuori dal giro mirato. Il banco non confronta gli attributi del
  cookie di sessione (i nomi sono diversi nelle due app).

## Review indipendente

Nessun difetto alto o medio. Le note basse:
- corretto il commento in `sessione.go` che poteva far credere che anche lo standalone non allunghi
  il cookie riscritto;
- differenza che resta, voluta: senza accesso il cookie (pagina di ritorno, messaggio) non ha
  `Max-Age`, nello standalone sì. Se si chiude la PWA sulla pagina di login, dopo l'accesso si va
  alla home invece che alla pagina chiesta. Era già così;
- il CSRF senza login (faccia pubblica) scade 30 giorni dopo la sua creazione anche se usato: una
  pagina aperta proprio a cavallo della scadenza dà 403 all'invio. Caso raro, lasciato così;
- al login il `Max-Age` può valere un secondo in meno (due letture dell'orologio): innocuo.

## Per il manuale

- L'accesso dura 30 giorni dal login, anche se chiudi il browser, l'app o spegni il telefono.
- Usare GestRip non allunga i 30 giorni: dopo si rifà il login.
- «Esci» chiude subito l'accesso su quel telefono o computer.
- Se una pagina era aperta prima di entrare (per esempio in un'altra scheda), dopo il login va
  ricaricata prima di premere un pulsante (D-67).
