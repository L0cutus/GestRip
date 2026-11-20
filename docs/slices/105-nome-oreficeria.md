# 105 — Nome del cliente e oreficeria dal dettato (T4.10, quinta)

- **Fonte:** standalone a `go-baseline`, `assist.ex` (regole su nome, apertura, persona, oreficeria, riconoscimento del
  cliente, messaggio) e `assist_test.exs` (tabelle). Il traduttore di produzione (`~/gestrip-e2b/traduttore.py`) passa
  «n» e «o» invariati: tutte le regole della slice sono dello standalone.
- **Voci:** RIP-H-43 **parziale** (si chiude con la 108; nessun segno `// inventario:`). Decisione nuova di Stefano
  durante la slice: **D-106** (domanda 28, vedi Scelte), registrata dal coordinatore, da fare nella 108.

## Cosa c'è

- **`internal/assist/nome.go`** (vedi SVILUPPO, «Nome del cliente e oreficeria dal dettato (slice 105, RIP-H-43)»):
  `Negozio{Clienti, Oreficerie}` passato dal chiamante (la 108 lo leggerà dal database; `assist` non importa `buste`),
  ordinato per nome e id con l'ordine dei byte (collazione BINARY di SQLite); nome dall'apertura
  («Cliente/Signora/Signor…»), maiuscole come Elixir, il nome deve essere una persona, oreficeria per contenimento
  (prima in ordine alfabetico), riconoscimento del cliente solo nei casi certi (telefono ≥ 7 cifre senza 0039/39, o
  nome uguale a parte accenti e spazi), coppia nome/telefono. `Proposta` ha `OreficeriaID`, `NomeCorretto`,
  `NomeCorrettoDa`; `Proposta.Messaggio()` dà «✓ Nome cliente corretto da «X» a «Y». Verifica i dati prima di
  salvare.» oppure «✓ Dati estratti…».
- **`scheda.go`:** `PropostaDa` ed `Estrai` ricevono il `Negozio`; nome e oreficeria passano dalle regole (attesi della
  104 aggiornati).
- **Dipendenza nuova `golang.org/x/text` v0.43.0** (`cases`, `unicode/norm`) per NFKD e maiuscole complete come Elixir;
  è la versione che `golang.org/x/image` già richiedeva (ok del coordinatore, 20/11/2026).
- **SVILUPPO:** sezione «Nome del cliente e oreficeria dal dettato (slice 105, RIP-H-43)».

## Scelte

1. **Omonimi (domanda 28 → D-106).** Con due o più clienti con lo stesso nome e nessun telefono dettato lo standalone
   propone il telefono del primo per nome e id, e la busta può finire sul cliente sbagliato. Stefano (20/11/2026,
   tramite il coordinatore: «concordo»): con omonimi il telefono non si propone, campo vuoto → **D-106, da fare nella
   108**. La 105 resta uguale allo standalone; i due casi di test degli omonimi cambieranno nella 108.
2. Oreficeria con nome vuoto in anagrafica: per lo standalone corrisponde a qualsiasi nome detto (`contains("")`);
   copiato. Rischio basso se la creazione delle oreficerie rifiuta i nomi vuoti.
3. Differenze dallo standalone solo su caratteri che le tabelle Unicode di OTP 27 non conoscono (scritture aggiunte da
   U+0560 in su, caratteri non assegnati in Unicode 15; Go usa Unicode 17): nessuno compare in un nome italiano.
4. `NomeCorrettoDa` può essere vuoto ma presente: lo distingue `NomeCorretto`. Da ricordare quando la raccolta dei
   dettati (113-114) salverà la proposta.

## Prove

- Rosso: prima non compilava, poi con l'API vuota 9 test su 10 rossi. `scripts/check` verde, rilanciato dalla sessione
  principale prima della revisione e dopo le correzioni.
- **Test:** `nome_test.go`, 11 funzioni: tutte le tabelle di `assist_test.exs` su nome, apertura, persona, oreficeria e
  cliente, con il telefono nel dettato (D-104), più i casi di sicurezza (omonimi anche a parte le maiuscole, due persone
  sullo stesso telefono, telefono corto dopo «0039», «39»/«0039» davanti, nomi uguali a parte accenti e spazi, nome che
  non è una persona, cliente senza telefono o senza nome, «%s» nel nome).
- **Confronto con lo standalone:** funzioni copiate da `assist.ex` eseguite in Elixir 1.18/OTP 27 (docker, senza rete,
  immagine già presente): 200 000 casi dell'implementatore e 140 000 del revisore senza differenze; un carattere per
  volta solo le differenze della Scelta 3. Panic: nessuno, anche con UTF-8 non valido.
- **Mutazioni dell'implementatore:** 64 compilanti, 60 uccise, 4 equivalenti; dopo la revisione uccise anche quelle
  sull'ordine dei clienti.
- **Revisione indipendente (Opus):** 0 ALTE, 0 MEDIE; BASSE chiuse: messaggio composto senza `strings.Replace` su «%s»
  (un nome con «%s» finiva nel posto sbagliato), caso degli omonimi a parte le maiuscole (uccide M04), commento sulla
  sicurezza fra goroutine del `Caser` globale. Mutazioni del revisore: 48 compilanti, 47 uccise, 1 viva (M04) poi
  uccisa.
- Implementatore Opus: ~240k token, ~23 minuti con le correzioni.

## Banco

Nessuno scenario (come da piano): si vedrà con la 108.

## Per il manuale

Niente di visibile per ora. Con la 108: se il telefono dettato o il nome scritto diversamente trovano un cliente già
in anagrafica, compare «✓ Nome cliente corretto da «X» a «Y». Verifica i dati prima di salvare.»
