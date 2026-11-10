# 064 — Link storici della app Flask: `/stato`, poll e messaggio, foto storiche (T4.5, quarta slice)

- **Fonte:** via di Stefano del 09/11/2026 (T4.5 → T4.6), ripresa `2026-11-09-fine-063.md`. Lo standalone a
  `go-baseline` = `302439d`.
- **Voci:** CLI-16 (accesso con `?pw=` o `?t=&exp=`), CLI-17 (contenuto ridotto), CLI-18 (revoca, 410),
  CLI-19 (solo poll e messaggio), CLI-20 (foto `/foto` e `/thumb`).
  **Non fatte:** le rotte storiche email-optin/optout (T4.6) e telegram-optin/optout (T4.7), i riquadri
  email e Telegram della pagina storica di una busta diretta (T4.6/T4.7).
- **Divisione:** la 064 della ripresa 063 comprendeva anche `/m/`, `/privacy` e l'anteprima cliente;
  vanno nella **065** (scelta di coordinamento, per tenere la slice gestibile).
- **Deviazioni:** D-64 (numero visibile, come la `/r/`), D-77 (numeri senza zeri, vedi Dubbi 1-3).
  Nessuna nuova.
- **Lavoro:** un sottoagente d'implementazione, un revisore diverso, mutazioni a mano. Nessuna
  migrazione (colonne già importate: `hash_codice_flask`, `codice_flask_revocato_il`, `nome_file_flask`).

## Cosa c'è

- **Segreto Flask** (`internal/firmeflask`): `Leggi` usa `segreti.LeggiFile` (16-4096 byte) e toglie gli
  spazi ai bordi come `String.trim`; il file è `secrets/segreto_flask` nella cartella del database
  (PIANO §1.9). File assente = nessun segreto, tutte le firme rifiutate, `?pw=` funziona; file sbagliato
  = `serve` non parte, con un errore che non riporta il contenuto. `Segreto` non si stampa mai (fmt,
  slog, JSON danno «[segreto Flask]»). Firme HMAC-SHA256 esadecimali troncate a 32, `exp` solo cifre,
  valido se `exp ≥ adesso`, confronto a tempo costante; `stato` e foto delle buste usate ora, le altre
  famiglie (artigiano, garanzia, vendite) pronte e provate solo con i vettori dello standalone.
- **Dominio** (`internal/buste/storica.go`): `PaginaStorica`, `ConversazioneStorica`,
  `ScriviMessaggioStorico`, `LeggiFotoStorica`. La busta si cerca prima col numero com'è scritto
  nell'indirizzo, poi senza zeri; una busta nel cestino ferma la ricerca. La credenziale vale sul
  numero scritto o su quello salvato. Revocata con credenziale giusta → 410 con `reason: revoked`;
  ogni altro rifiuto → 404 unico e `repair.legacy_public_denied` senza busta. Contenuto ridotto:
  niente preventivo; B2B senza prezzi né conversazione (poll e messaggio B2B = 404 identico). La
  pagina e la `/r/` si leggono con la stessa `paginaDove`; il messaggio passa dalla stessa
  `scriviMessaggioPubblico` (revoca ricontrollata dentro la transazione). Foto: solo quelle visibili
  al cliente, firmate dalla pagina per un'ora sullo slug del numero scritto.
- **Rotte** (faccia pubblica, `internal/web/pagina_storica.go`): `GET /stato/{busta}/{anno}`, `…/poll`,
  `POST …/messaggio` (CSRF, credenziale solo dai campi del modulo e rimandata nel redirect),
  `GET /foto/{slug}/{file}`, `GET /thumb/{slug}/{file}`; pagina 410 `esito_pubblico.html`. Il
  limitatore CLI-21 conta i 404 di `/stato/*`, non le foto, come lo standalone.
- **Banco:** segreto **sintetico** dato alle due app (`LEGACY_WARRANTY_SECRET_FILE` e
  `go-data/secrets/segreto_flask`); Caddy manda `/stato/* /foto/* /thumb/*` alla faccia pubblica; il
  passo `token` dà i codici brevi sintetici alla 4/26 e alla 3/26; scenari `pagina-storica` e
  `pagina-storica-rifiuti`; 6 righe D-64 in `deviazioni.json`.

## Prove

- `firmeflask`: vettori dello standalone, scadenza enorme, foto, altre famiglie, codice, file del
  segreto, «non si stampa».
- `buste`: codice e firma sul numero scritto e su quello salvato, numero com'è scritto prima, cestino
  che ferma (pagina e slug delle foto), revoca (anche arrivata dopo la ricerca, dentro la
  transazione del messaggio), contenuto ridotto, poll, messaggio, foto, riga d'apertura che non si
  scrive (rifiuto).
- `web`: 8 test delle rotte; `cmd`: `TestServeSegretoFlask`; banco: firme degli scenari ricalcolate.

## Mutazioni

Implementazione: 26 a mano, tutte compilano, **tutte uccise** (numero scritto/salvato, 16 caratteri,
`exp >`, firma non troncata, revoca ignorata, niente 410, B2B, colonna della controparte, foto nascoste,
segreto assente come errore, minimo 16, firma senza segreto, audit, ricerca senza zeri prima, cestino,
credenziale dalla query, `/thumb` = `/foto`, slug del numero salvato, durata 60 s, segreto sbagliato
ignorato, `PathEscape`, testo prima della credenziale).
Review: 29, 25 uccise, 1 equivalente (firma maiuscola: il confronto la rifiuta comunque), 3 sopravvissute
(M10, M20, M25) → tre test nuovi, ora uccise.

## Banco

Istanza `s064`, `fab-225`, `-dati sintetici-import`. Primo giro: 22 inattese, tutte artefatti degli
scenari nuovi (Content-Type dei 404 dietro il Caddy del banco, attese del 410 e del poll, numero D-64,
un conteggio sfasato da un passo `solo_go`), corretti negli scenari. Giro finale su istanza nuova:
**BANCO VERDE**, 55 scenari, 1037 passi, 1517 differenze attese, **0 inattese**; **BANCO BROWSER
VERDE**, 12 scenari, 24 viste, 0 inattese. `banco giu`. Le correzioni della review (ricerca dello slug
con l'indice, tre test) non cambiano il comportamento: banco non rifatto.

Limiti: sui dati sintetici l'unica busta diretta con foto (1/26) ha il link storico revocato, quindi la
foto servita con firma giusta la provano solo i test Go; il messaggio storico riuscito idem (la 27/26
ha già raggiunto il limite di 10 all'ora nello scenario della conversazione).

## Controlli

`scripts/check` verde (rilanciato dalla sessione principale prima e dopo le correzioni della review);
`scripts/parole-vietate --staged` verde.

## Per il manuale

- I vecchi link `/stato/…` mandati dalla vecchia app funzionano ancora, con la password breve o con il
  link firmato; mostrano stato, date e foto, senza preventivo.
- Per una busta conto terzi il vecchio link non mostra prezzi, conversazione né foto.
- «Revoca link» spegne anche il vecchio link: chi lo apre legge «Link non più attivo. Chiedi a <negozio>
  il link nuovo della tua riparazione.».
- Dal vecchio link si può scrivere al negozio (come dal link nuovo), salvo per le buste conto terzi.
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Firma e codice valgono sul numero scritto e su quello salvato.** Lo standalone verifica sul
   salvato; dopo la D-77 il salvato è «46/26» mentre il link Flask dice «0046/26», quindi senza il
   numero scritto i link vecchi si romperebbero. **Raccomandazione:** tenerlo.
2. **La busta si cerca prima col numero com'è scritto, poi senza zeri.** Con il numero occupato della
   D-77 (le due buste «0046/26» e «46/26» restano distinte) il link di «0046/26» apre la sua busta, non
   l'altra. **Raccomandazione:** tenerlo. Rischio residuo (review, BASSA): se un giorno «0046/26» non
   esiste più nel Go (cancellazione definitiva per conservazione, che oggi non c'è) la firma di
   «0046/26» aprirebbe «46/26». **Raccomandazione:** prima di qualunque cancellazione definitiva,
   salvare all'import il numero di prima della D-77 e verificare su quello invece che sul numero
   scritto (cambio di schema, da decidere).
3. **Slug delle foto senza zeri:** il Go trova «0046_26» anche per la busta «46/26»; lo standalone cerca
   solo lo slug esatto. Effetto: con una firma giusta su una foto rifiutata il Go scrive la riga
   `denied` dove lo standalone non scrive niente (fuori dal confronto del banco).
   **Raccomandazione:** tenerlo come parte della D-77.
4. **CLI-17 nell'inventario:** dice che su una busta B2B il link storico mostra le foto «cliente»; il
   codice dello standalone non ne mostra nessuna (`visible?/3` con pubblico cliente su busta esterna).
   Il Go copia il codice. **Raccomandazione:** correggere il testo dell'inventario.
5. **Segreto vero di almeno 16 byte** (lo standalone ne accetta da 1): da controllare al cutover.
   Con il segreto assente `serve` parte senza avviso.
6. **Due foto con lo stesso nome file Flask nella stessa busta:** il Go prende quella con l'id più
   basso, lo standalone va in 500. **Raccomandazione:** tenere il Go.
7. **Credenziale del POST solo dai campi del modulo** (regola del repo); Phoenix accetterebbe anche la
   query. La pagina la manda sempre nel corpo.
8. **Audit della foto che non si scrive:** 500 invece del 404 dello standalone, come già per le foto di
   `/r/` (difetto 2 in lista in `docs/domande-aperte.md`).
9. Piccole: `strings.ToUpper` contro `String.upcase` su lettere come «ß»; la pagina 410 è un modello
   del Go che copia `email_outcome`.

## Review

Revisore indipendente, 10/11/2026 (pc-fisso, slice non committata). Nessuna ALTA né MEDIA.

- **BASSA, da decidere:** rischio residuo del numero scritto (Dubbio 2).
- **BASSA, corretto:** `cercaPerSlug` scorreva tutta la tabella a ogni `/foto` e `/thumb`
  (`replace(numero,'/','_')`); ora cerca prima `numero = ?` (indice), il `replace` solo come ripiego.
- **BASSA, dichiarato:** audit della foto che non si scrive (Dubbio 8).
- **Test mancanti, aggiunti:** revoca dentro la transazione del messaggio storico (M10), riga
  d'apertura che non si scrive (M20), «0046_26» nel cestino che ferma lo slug (M25).
- **Domande di prodotto:** Dubbi 4 e 7.
- **Verificato senza rilievi:** firme e scadenza, password breve, scelta della busta con la D-77,
  410 solo con credenziale giusta e 404 identico, contenuto ridotto, foto (visibilità, revoca, audit),
  ordine della query misurato su `fab-225` (`sig` poi `exp`), segreto mai stampato, CSRF, escape,
  limitatore, lettura del segreto all'avvio, deviazioni del banco. `scripts/check` verde.
