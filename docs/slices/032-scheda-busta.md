# 032 — Scheda della busta in sola lettura (T4.1)

- **Fonte:** T4.1 del PIANO; piano delle slice nella ricevuta della 030.
- **Voci:** chiuse RIP-S-01, RIP-S-02, RIP-S-18. In RIP-S-01 «← Indietro» torna alla home: la
  lista con i filtri è la T4.2.
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso;
  `scripts/check` e banco rilanciati dalla sessione principale.
- **Inventario corretto:** RIP-S-01 diceva «id non numerico → Impossibile aprire la riparazione.».
  Lo standalone dice «Riparazione non trovata.», perché `get_repair/1` dà nil anche per un id
  malformato: misurato sul banco e letto nel codice. «Impossibile aprire» viene solo da un errore.

## Cosa c'è

- `GET /repairs/{id}` per titolare e dipendente, con il percorso dello standalone. Contiene:
  - titolo «Busta #N»;
  - riquadro «Dati riparazione» con righe, ordine e testi dello standalone;
  - destinatario diretto o B2B, con «Solo anagrafica» e «— Nessun recapito …»;
  - il testo che vince su centesimi e data;
  - «✅ ACCETTATO» o «❌ RIFIUTATO» sull'ora del negozio;
  - badge compro oro;
  - note visibili e interne distinte, con l'escaping.
- Apertura (`buste.ApriScheda`):
  - audit `repair.viewed`;
  - segni «letto» di conversazione e preventivo scritti solo se cambiano, nell'ordine dello
    standalone, con il ramo della risposta arrivata durante l'apertura;
  - busta nel cestino non trovata;
  - sessione anonima, disattivata o revocata rimandata al login.
- **Banco:** campi nuovi documentati in `docs/slices/004-banco.md`, con i loro limiti:
  - `visibile:` dà il testo visibile, con i tag in linea senza spazio;
  - `grezzo:` dà il testo con gli a capo;
  - `attributo:` dà il valore di un attributo;
  - il localizzatore accetta un id oppure `.classe@n`, con `/tag@n`.
- **Scenario `scheda`:** le quattro buste importate, i casi «non trovata» e i ruoli.
- **Differenze di forma:** classi e CSS sono del Go. Nella pagina mancano i pezzi delle altre
  slice e tappe: modifica (033), stati (034), link (035/T4.5), foto (T4.3), firme e PDF (T4.4),
  conversazione (T4.5), stima (T4.10), artigiano (T4.8), Telegram (T4.7), cestino (T4.11).

## Debito del banco

I dati sintetici non hanno prezzi, preventivi con risposta, compro oro, note, consegna prevista,
buste senza recapito né stati diversi da «In lavorazione» e «Consegnata». Quelle regole le provano
solo i test Go. Da allargare con la 033/034: un passo della catena con le funzioni dello
standalone, come per emergenza e vendita, che crei buste con quei dati. L'elenco completo, 18
punti, è nel resoconto dell'implementazione: prezzi, risposte, compro oro, note e a capo,
consegna, destinatari senza recapito, oreficeria riga 1, telefoni da scappare, altri stati, QR
vuoto, ora legale, nome del negozio, escaping, cestino, audit che fallisce, account disattivato o
sessione revocata, righe d'audit, classi.

## Prove

- **TDD:** rosso misurato prima del codice, nei due giri.
- **Mutazioni:** primo giro 51 che compilano, 43 uccise; giro delle correzioni 73 che compilano, 68
  uccise. Le 5 vive sono equivalenti:
  - id ≥ 0;
  - NULL su NULL;
  - la minuscola su stati già minuscoli;
  - due nel ritaglio degli attributi, che non ammettono «:».
- **Banco** (`fab-219`): verde su database vuoto e con `-dati sintetici-import`, 15 scenari e 189
  passi. La prova negativa, con una riga vuota e uno spazio tolto, dà 17 differenze.
- `scripts/check` verde; con l'esito del banco 38 voci chiuse, 13 parziali, 0 rosse.

## Review indipendente

Nessuna ALTA.
- **MEDIA:**
  - `visibile:` poteva dare verdi falsi → campi più stretti e nuovi;
  - scenario troppo generico → rafforzato, debito dichiarato sopra;
  - tre rami dell'apertura senza test → test con un aggancio, mutazioni uccise.
- **BASSA:**
  - `href` del telefono → ora `tel:333 111`, come Phoenix, con l'escaping;
  - peso del prezzo finale → 700;
  - conteggio del banco nella documentazione → aggiornato;
  - doppia riga `repair.viewed` di LiveView → domanda aperta 4;
  - log dei segni non scritti e dati rotti (oreficeria mancante, data impossibile) → come lo
    standalone, non cambiati.
