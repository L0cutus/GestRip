# GestRip

> **⚠ In sviluppo, non ancora utilizzabile.** Questo è il rifacimento in Go di GestRip: molte
> funzioni mancano ancora e non c'è nessuna versione da installare. Il negozio per cui nasce usa
> ancora l'altra versione del programma.

Così funziona oggi la versione in uso in negozio; questa versione Go farà lo stesso.

https://github.com/user-attachments/assets/521d6c33-cf23-4cc7-bec5-40a24bcbc11a

Gestione delle riparazioni per oreficerie e orologerie: buste, clienti, preventivi, stati, foto,
stampe ed etichette, link per i clienti e per le oreficerie, registro d'emergenza.

## Com'è fatto

- **Un solo programma** (`gestrip`), scritto in Go senza CGO, che contiene sito, database
  SQLite, PDF e regole. Template, CSS e JavaScript stanno dentro il binario.
- **L'intelligenza artificiale gira in locale**, in programmi separati (estrazione dei campi dal
  dettato, trascrizione facoltativa) che `gestrip` avvia e sorveglia. Nessun servizio esterno è
  necessario.
- **Due facce:**
  - quella **interna**, per chi lavora in negozio, raggiungibile solo dalla rete Tailscale del
    negozio;
  - quella **pubblica**, per clienti, oreficerie e artigiani, esposta con Tailscale Funnel e
    limitata alle pagine dei link.
- **I dati restano in negozio**, su un mini PC con il disco cifrato. I backup sono cifrati con
  [age](https://age-encryption.org) prima di uscire dalla macchina e ogni backup viene
  verificato.

Il piano completo, con l'architettura, le scelte tecniche e le tappe, è in
[`docs/PIANO.md`](docs/PIANO.md).

## Per sviluppare

Requisiti:
- Linux amd64;
- Go 1.27.3;
- [gitleaks](https://github.com/gitleaks/gitleaks) 8.30.1 per i controlli dei commit;
- Docker, solo per il banco di confronto.

Le versioni esatte e le verifiche sono in [`docs/SVILUPPO.md`](docs/SVILUPPO.md).

    git config core.hooksPath scripts/githooks
    CGO_ENABLED=0 go build -trimpath -o gestrip ./cmd/gestrip
    scripts/check

`scripts/check` fa il controllo completo: formato, vet, staticcheck, errcheck, build, test,
copertura dell'inventario, segreti e parole vietate su tutta la history.

Per i commit servono anche alcuni file di configurazione locali, fuori dal repo, descritti in
[`docs/SVILUPPO.md`](docs/SVILUPPO.md) insieme al metodo di lavoro.

Il repository contiene solo codice e **dati inventati**: niente segreti e niente dati veri di
clienti, buste, foto, dettati, persone o negozi.

## Contributi

Per ora non si accettano contributi: issue e pull request non verranno prese in carico finché
il programma non arriva a una prima versione.

## Sicurezza

Le vulnerabilità si segnalano in privato, come spiegato in [`SECURITY.md`](SECURITY.md).

## Licenza

GNU Affero General Public License v3.0 ([`LICENSE`](LICENSE)). Chi offre GestRip modificato
ad altri, anche solo via rete, deve offrire anche il sorgente delle modifiche.
