# 007 — Password PBKDF2 compatibili con lo standalone (T1)

- **Fonte:** inventario ACC-15; `go-baseline:lib/gestrip_standalone/accounts/password.ex` e
  `accounts.ex` (lunghezza minima); PIANO T1 («password PBKDF2 compatibili con gli hash dello
  standalone, vettore sintetico; rifiuto degli hash sotto 600.000 iterazioni»).
- **Confine:** `internal/accounts` sa fare l'hash di una password nuova, verificare un hash
  importato dallo standalone e controllare la lunghezza minima. Utenti, sessioni e login sono la
  008.
- **Voci:** ACC-15 (chiusa).
- **Livello:** completo (autenticazione).

## Vettori dallo standalone

Generati il 30/10/2026 dal codice vero dello standalone, `Accounts.Password.hash/1`, su
un'istanza **sintetica** del banco (`grgo-s007`, rete interna, nessun dato vero), con tre password
inventate (ASCII, Unicode, vuota). Il Go li verifica tutti.

Lo stesso giro ha misurato:
- varianti dell'hash:
  - `+600000` e `0600000` valgono;
  - non valgono: base64 senza padding, a capo in fondo, `$` in più, prefisso maiuscolo, spazio
    iniziale, meno di 600.000 iterazioni;
- `String.length`: 11 «é» scomposte → 11, 12 emoji famiglia → 12, 12 «\r\n» → 12.

Un secondo giro, dopo la review, ha misurato i casi che il revisore aveva solo ragionato:
- `\r\n` e `\r` in fondo, a capo nel sale, `599999`, `-600000`, `600000x`, derivata di 31 o 64
  byte → no;
- base64 con bit di coda non nulli → **sì** (Elixir non è rigido: parità, commento corretto);
- sale vuoto → sì;
- iterazioni enormi → eccezione;
- «क्क»×6 → 12 (Elixir non applica GB9c, come uniseg);
- un byte non UTF-8 ×12 → 12;
- Elixir dichiara Unicode 16.

## Cosa c'è (`internal/accounts/password.go`)

- `Hash`: `pbkdf2_sha256$600000$<sale 16 byte>$<derivata 32 byte>`, base64 standard con
  padding, `crypto/pbkdf2` della libreria standard.
- `Verifica`:
  - forma esatta in quattro parti, iterazioni intere (anche con `+` o zeri iniziali) non sotto
    600.000, base64 con padding;
  - confronto a tempo costante;
  - un hash con un a capo è rifiutato: il decoder base64 di Go ignora gli a capo, quello di
    Elixir no;
  - un hash corretto ma con meno iterazioni è **rifiutato**, non accettato e aggiornato.
- `IterazioniMassime` (6.000.000): oltre, l'hash è rifiutato. Lo standalone non ha tetto: con un
  numero enorme solleva un'eccezione (misurato), con uno grande ma valido lavora per ore a ogni
  login. È una divergenza dal lato sicuro, contro un hash importato manipolato.
- In modalità FIPS (`GODEBUG=fips140=only`) un sale importato corto fa fallire la derivazione: la
  verifica dà no invece di un panic. Questo ramo non è provato da un test, perché la modalità si
  sceglie all'avvio del processo.
- `PasswordAbbastanzaLunga`: almeno 12 grafemi, contati con `github.com/rivo/uniseg` v0.4.7
  (licenza MIT, compatibile con AGPL-3.0; Go puro), come `String.length` di Elixir. Elixir usa
  Unicode 16 e uniseg Unicode 15: possono differire solo i caratteri nuovi di Unicode 16.

## Prove

- TDD: test con i vettori, le varianti misurate, la forma dell'hash nuovo, le iterazioni, la
  lunghezza in grafemi, rossi prima del codice (non compilavano).
- Mutazioni: 18. Le prime quattro sopravvissute hanno portato a:
  - due test nuovi: `\r` da solo; un hash corretto con 1000 iterazioni, che deve essere
    rifiutato;
  - un test che costruiva l'hash di prova con la funzione sotto prova: ora usa `crypto/pbkdf2`
    direttamente;
  - un controllo di lunghezza ridondante tolto.

  Ora tutte uccise.
- Dopo la review: tetto delle iterazioni, niente panic in FIPS, vettori del secondo giro nei
  test; mutazioni rifatte, tutte uccise tranne il ramo FIPS (dichiarato sopra).
- `scripts/check` verde.

## Review indipendente

Nessun problema alto: nessun modo di aggirare la verifica, e i vettori dello standalone si
verificano.

- MEDIA, «uniseg non applica GB9c»: **smentita dalla misura** (Elixir dà 12 come il Go).
- MEDIA, nessun tetto alle iterazioni: corretta (`IterazioniMassime`).
- BASSA, base64 non canonico: misurato, Elixir lo accetta; commento corretto.
- BASSA, panic in FIPS: corretto.
- BASSA, test che dicevano «misurato» per casi solo ragionati: ora sono misurati tutti.
- Per la 008:
  - il login deve fare una derivazione fittizia quando l'utente non esiste o l'hash è
    malformato (come `@dummy_hash` dello standalone), così i tempi non rivelano nulla;
  - le password arrivano dai moduli, che rifiutano già l'UTF-8 non valido.
