# Banco del modello di estrazione (T4.10)

Dettati **inventati** con la risposta esatta per costruzione, per misurare il modello che estrae i
campi della busta da un dettato (PIANO, «T4.10, banco del modello e modello di estrazione»). Non ci
sono dati veri: nomi e cognomi sono combinati a caso da elenchi comuni, le oreficerie sono inventate,
i telefoni sono finti.

Ogni riga è un oggetto JSON:

- `cat`: il tipo di caso;
- `oggi`: la data del giorno del dettato;
- `dettato`: il testo dettato;
- `risposta`: i campi attesi, omessi se vuoti.

Le chiavi della risposta:

| Chiave | Campo |
|---|---|
| `n` | nome e cognome del cliente |
| `o` | oreficeria che porta il lavoro |
| `b` | numero della busta |
| `d` | oggetto e lavorazione |
| `c` | consegna, GG/MM/AAAA |
| `p` | preventivo |
| `a` | acconto |
| `x` | altre note per il cliente |

Telefono, compro oro e data di oggi li gestisce il codice di GestRip: non sono nella risposta.

| File | Dettati | Cosa prova |
|---|---|---|
| `B-difficili.jsonl` | 720 | forme difficili: correzioni a voce («busta 7, no scusa, 241»), oggetto prima e cliente in fondo, telefoni, oreficerie |
| `C-acconto-test.jsonl` | 600 | acconti detti a parole («ha versato 20 euro») |
| `D-prezzi-verbo.jsonl` | 200 | prezzi introdotti da un verbo («gli ho detto N euro») |
| `D2-prezzi-verbo-nuovi.jsonl` | 200 | prezzi con verbi e forme nuove, anche al futuro («spenderà») |

## Da dove vengono

I dettati sono stati generati per la prova del modello del 03/11/2026 (misure nel PIANO). Prima di
copiarli nel repo, il 03/11/2026, sono stati cambiati in modo coerente nel dettato e nella risposta,
con l'ok di Stefano:

- tre nomi delle liste del generatore che potevano somigliare a nomi veri sono stati sostituiti:
  - un cognome è diventato «Bortolin», un cognome già presente nelle liste;
  - due oreficerie sono diventate «Oreficeria Inventata Alfa» e «Oreficeria Inventata Beta»;
  - le righe cambiate sono 108;
- gli 80 telefoni, tutti in `B-difficili.jsonl`, hanno la seconda e la terza cifra a «00»
  (`300 …`), un prefisso mobile che non esiste. Formato e numero di cifre restano gli stessi.

I numeri della tabella del PIANO sono misurati sui file di prima. Le sostituzioni toccano pochi nomi
e nessuna forma della frase, quindi i risultati sui file del repo possono differire di poco.
