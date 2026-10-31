# 030 — Nucleo della busta (T4.1, prima slice)

- **Fonte:** T4.1 del PIANO, via di Stefano del 31/10/2026 (T4.1 → T4.3 → T4.4 → T4.2).
- **Voci:** RIP-H-30, 31, 33, 34, 35, 36, 37, 38, 39; RIP-S-05, 06, 13 — **parte di dominio**,
  nessuna chiusa: le pagine arrivano nelle slice 031–034 (test di pagina saltati in
  `internal/buste/pagine_test.go`).
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso;
  mutazioni, `scripts/check` e banco rilanciati dalla sessione principale.

## Piano della T4.1

| Slice | Contenuto |
| --- | --- |
| 030 | nucleo di dominio (questa) |
| 031 | pagina «Nuova riparazione»: modulo, suggerimenti del cliente, oreficeria, artigiano, avviso QR, esito col link una volta (RIP-H-29…39, 41) |
| 032 | scheda della busta: apertura, riquadro dati, note (RIP-S-01, 02, 18) |
| 033 | modifica: campi, prezzi, consegna e «Togli data», cliente, oreficeria con revoca del link, artigiano, QR, tipo, `?edit=` (RIP-S-04…12) |
| 034 | stati, riapertura, attesa (RIP-S-13…15); qui anche la chiusura dei lotti alla consegna (`ReadyLots.close_after_delivery`) e lo stato uguale che non dice niente (`workshop_live.ex:225-229`) |
| 035 | riquadro del link pubblico: stato, revoca, nuovo link (RIP-S-24) |

Il «📷 Scansiona» e i suggerimenti mentre si scrive hanno bisogno di JavaScript: arrivano con la
prima funzione con JavaScript e con il livello browser del banco. Fino ad allora quelle voci
restano parziali. Il segno e il filtro «⚠ Senza QR» (RIP-H-56) stanno sulla lista (T4.2).

## Cosa c'è

- Pacchetto `internal/buste`: `Crea` (cliente scelto o creato, busta, gettone, audit in una
  transazione), `ProssimoNumero`, `CambiaStato` con i due grafi, le funzioni senza database
  (`Consentiti`, `StatoIniziale`, `Centesimi`, `LeggiPrezzo`, `DataDalTesto`, `LeggiConsegna`…) e
  i rifiuti con i testi esatti dello standalone.
- Il gettone pubblico come lo standalone: 32 byte casuali in base64url, salvati impronta SHA-256 e
  testo (CLI-22, per riusare il link nei messaggi), 90 giorni; mai in audit o registri, e
  `Creata` lo nasconde in `%v`, `%+v`, `%#v` e slog (lo standalone ha `redact: true`).
- Il confronto dei nomi come `String.downcase/1` di Elixir: «İ» diventa «i» + U+0307; la sigma
  finale no (solo con `:greek`).
- Migrazione `0014_trigger_buste.sql`: copiati i trigger dello standalone del contatore dei cambi
  di stato (`repair_status_versions_bump`) e dello spegnimento delle iscrizioni Telegram quando una
  busta si chiude, va nel cestino o passa a un'oreficeria (`telegram_subscriptions_off_when_*`),
  con l'audit `repair.telegram_switched_off`. Come nello standalone prendono l'ora dall'orologio
  di SQLite, non da quello del negozio. L'import scrive con INSERT e non li fa scattare.

## Prove

- **TDD:** rosso misurato prima del codice (34 test e un panic con gli stub; poi i test delle
  correzioni).
- **Mutazioni (sessione principale):** 16, tutte uccise: formato e contatore dei numeri, grafo,
  date storiche, stati chiusi, cestino, telefono vuoto, «İ», oreficeria 1, durata del gettone,
  decimali e lunghezza dei prezzi, tre condizioni dei trigger.
- **Banco** (`fab-219`): verde su database vuoto e con `-dati sintetici-import`.
- `scripts/check` verde; con l'esito del banco 32 voci chiuse, 14 parziali, 0 rosse.

## Review indipendente

Nessun problema ALTA o MEDIA. Sei BASSA:
1. minuscole diverse da Elixir per «İ» → corretto;
2. il gettone usciva stampando `Creata` → corretto;
3. date del modulo più strette di Ecto → domanda aperta 3;
4. nessun test vicino alla mezzanotte per le date di pronta e consegna → aggiunto, ucciso dalla
   mutazione del giorno UTC;
5. iscrizioni Telegram non spente alla chiusura → trigger copiati; la chiusura dei lotti va nella 034;
6. lo stesso stato è un rifiuto del dominio → la pagina deve confrontare prima, nella 034.

## Domande aperte nuove

In `docs/domande-aperte.md`: messaggi d'errore inglesi del modulo, numero con zeri davanti, data
d'accettazione con l'ora. Nessuna blocca il lavoro.
