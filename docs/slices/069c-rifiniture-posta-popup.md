# 069c — Rifiniture della posta e del popup: D-83, prova subito, busta, «acceso ma non configurato», `\r\n` (T4.6, sesta slice)

- **Fonte:** decisioni di Stefano dell'11/11/2026 (`docs/SVILUPPO.md`, «Decisioni di Stefano dell'11/11/2026»:
  risposte ai Dubbi di 067, 068, 069 e 069b), via del coordinatore del 12/11. Lo standalone a `go-baseline` =
  `302439d` (`outgoing.ex`: `enqueue_test_message/2`, `deliver_now/2`; `providers_live.ex`; `providers/smtp.ex`).
- **Voci:** CLI-43 (D-83), ADM-09 e CLI-52 parte email (prova, numero di busta, riga «acceso ma non
  configurato»), CLI-32 (popup senza `indirizzo_pubblico`, D-46 estesa), RIP-S-04, RIP-H-30, CLI-14 (righe dei
  moduli). Nessuna voce nuova chiusa: sono rifiniture di voci già parziali.
- **Lavoro:** un sottoagente d'implementazione, mutazioni a mano. Nessuna migrazione.

## Cosa c'è

- **D-83** (`internal/uscita/smtp.go`): `intestazioneMittente` codifica sempre (`parolaSempreCodificata`,
  `=?utf-8?B?…?=`) un nome mittente che non è semplice (`nomeSemplice` = `@plain_name`), anche tutto ASCII:
  «Negozio, Otto» o `"Capo" <altro@example.com>` non cambiano più gli indirizzi del `From`. L'oggetto resta su
  `parolaCodificata` (ASCII stampabile in chiaro, come lo standalone). Differenza voluta, commentata come D-83.
- **Prova «subito»** (`internal/uscita/pagina_smtp.go`, `internal/web/provider.go`): `Coda.MandaProva` prende il
  posto di `AccodaProva` (e sparisce `ErrEmailSpenta`). Con la configurazione usabile manda **dentro la
  richiesta**, senza riga nella coda e **anche con l'invio delle email spento**, il messaggio di prova al mittente
  stesso: stesso oggetto e testo dello standalone, parte `shop`, nessuna busta. Usa `SMTP.ConsegnaAMano`
  (Consegna senza l'interruttore della D-03, i tempi massimi dell'SMTP: 15 s per passo, 2 minuti in tutto) passando
  da `provaCon` (panico recuperato, segreti puliti), con il contesto della richiesta. Senza configurazione resta
  il rifiuto dello standalone «Posta SMTP non è configurato: niente è stato accodato.» (il banco
  `provider-posta` lo confronta, sempre uguale). Esiti sulla pagina: «Messaggio di prova accettato da Posta SMTP:
  guarda se è arrivato nella casella del mittente.» o «Messaggio di prova non partito (smtp:535): controlla i
  dati del server e riprova.» (solo il genere dell'errore come lo salverebbe la coda: mai testo del server,
  indirizzo o password); un errore del database «Non è stato possibile mandare la prova.» con una riga senza
  dati nel registro. La frase «L'invio delle email è spento: accendilo qui sotto, poi manda la prova…» non c'è
  più; la frase sotto l'interruttore spento ora dice che la prova parte sempre.
- **Audit della prova:** lo standalone scrive `outgoing.queued` della riga (canale, parte, busta nil), perché la
  accoda. Il Go, senza riga, scrive `outgoing.test_sent` (risorsa `outgoing_provider`, id `email`), esito
  `ok`/`error`, dati `{"channel":"email","party":"shop","status":"accepted"|"failed"}`: mai indirizzo, testo,
  oggetto o errore (come `outgoing.queued_sent_by_hand` della 069b). Si scrive dopo l'invio con
  `context.WithoutCancel` (una richiesta chiusa a metà lascia l'audit); se non si scrive va nel registro del
  server solo il tipo dell'errore e la pagina mostra l'esito vero.
- **Numero di busta** nelle email da guardare (D-27): «Busta 0045/26» (con l'etichetta) o «Busta N. 7/26»
  (`buste.NumeroVisibile`, D-64), solo per una riga con una busta. `RigaCoda` porta `NumeroBusta` e
  `CodiceQRBusta` (due sottoquery su `buste` in `Coda.righe`).
- **«Acceso ma non configurato»:** con `canale_email_acceso` = 1 e la configurazione non `Usabile`, nel riquadro
  dell'interruttore `#smtp-switch-unusable`: «L'invio delle email è acceso ma Posta SMTP non è configurato:
  nessuna email parte finché non completi i dati del server qui sopra.».
- **Popup senza `indirizzo_pubblico`** (`internal/web/avvisi.go`, `linkMancante`): dopo la frase della D-46
  « Togli {link} dal testo per mandarlo senza link.». Il banco non legge `#notice-link-missing` (nessuno scenario
  senza indirizzo pubblico): niente da cambiare lì.
- **`\r\n` → `\n`:** guardate tutte le `<textarea>` delle pagine (`internal/web/pagine/*.html`; nessuna casella di
  più righe è scritta fuori dagli stampi) e i gestori che le leggono:
  - **già normalizzate** (067/068, `aCapoUnix`), lasciate: `reply[body]` (`conversazione.go`, risposta della
    scheda), `repair[description]`, `repair[public_notes]`, `repair[internal_notes]` della modifica
    (`modifica.go`) e di «Nuova riparazione» (`nuova.go`), `notice[body]` (`avvisi.go`, popup),
    `template[text]` (`testi.go`, editor dei testi);
  - **pubbliche, lasciate come lo standalone:** `reason` (`/r/…/preventivo/rifiuta`, `pagina_cliente.go`) e
    `testo` (`/r/…/messaggio` in `conversazione.go`, `/stato/…/messaggio` in `pagina_storica.go`);
  - **campi di una riga** (`repair[reopen_reason]`, `waiting[motivo]`, `ritirato_da`, …): `<input>`, il browser
    non ci mette a capo; lasciati.
  Nessun modulo interno mancava: la 069c aggiunge il controllo `TestCaselleDiPiuRigheNormalizzate`, che legge gli
  stampi e cade per una `<textarea>` nuova che non sia fra le sei normalizzate o le due pubbliche.

## Prove

`uscita`: `TestSMTPParoleCodificate` (3 casi nuovi D-83: virgola, virgolette e `<…>`, trattino basso; oggetto
ASCII in chiaro e non ASCII codificato), `TestMessaggioDiProvaEConfigurazioneNuova` (riscritto: senza
configurazione, spenta, accesa, configurazione nuova senza riavvio; nessuna riga, audit esatto),
`TestMessaggioDiProvaRifiutato` (535: solo «smtp:535», audit `failed` senza dati; provider senza invio a mano),
`TestMessaggioDiProvaRichiestaChiusa` (server muto, richiesta chiusa a 200 ms: errore senza dati, audit scritto).
`web`: `TestProvaProviderPosta` (riscritto), `TestProvaProviderPostaErrore`, `TestSospeseConNumeroBusta`,
`TestAccesoMaNonConfigurato`, `TestPopupSenzaIndirizzoPubblicoD46` (frase nuova),
`TestCaselleDiPiuRigheNormalizzate`.

**Rosso preso prima del codice:** D-83 3 casi rossi; prova subito 2 test rossi in `uscita` (con uno stub di
`MandaProva` che chiamava `AccodaProva`); pagina 4 test rossi in `web` (prova, busta, acceso ma non configurato,
più `TestProvaProviderPostaErrore`); popup 1 rosso. `TestMessaggioDiProvaRichiestaChiusa` scritto dopo, per la
mutazione del `WithoutCancel` (verde subito; ucciso M8). `TestCaselleDiPiuRigheNormalizzate` verde subito (nessuna
casella mancava): la sua forza è provata da M22.

## Mutazioni

Script `to-be-deleted-after-use/slice-069c/mutazioni/muta.py` su una copia del repo, test dei pacchetti toccati.
24 mutazioni, 23 compilano, **23 uccise, 0 sopravvissute**; 1 non compila (M9, non conta).

| # | Mutazione | Esito |
|---|---|---|
| M1 | nome non semplice di nuovo con `parolaCodificata` | uccisa (`TestSMTPParoleCodificate`) |
| M2 | ramo del nome semplice tolto (sempre codificato) | uccisa (idem) |
| M3 | oggetto con `parolaSempreCodificata` | uccisa (idem) |
| M4 | `MandaProva` senza il controllo `Usabile` | uccisa (`TestMessaggioDiProvaEConfigurazioneNuova`) |
| M5 | stato d'audit sempre `accepted` | uccisa (`…Rifiutato`, `…RichiestaChiusa`) |
| M6 | esito d'audit sempre `ok` | uccisa (idem) |
| M7 | indirizzo del mittente nell'audit | uccisa (tre test della prova) |
| M8 | audit col contesto della richiesta (senza `WithoutCancel`) | uccisa (`…RichiestaChiusa`) |
| M9 | oggetto tolto dal messaggio | non compila |
| M9b | prova a un altro indirizzo | uccisa (`…ConfigurazioneNuova`) |
| M9c | parte `customer` e testo diverso | uccisa (`TestProvaProviderPosta`) |
| M10 | rifiuto della prova senza il genere dell'errore | uccisa (`TestProvaProviderPosta`) |
| M11 | numero e codice QR della busta scambiati nella query | uccisa (`TestSospeseConNumeroBusta`) |
| M12 | prova rifiutata mostrata come riuscita | uccisa (`TestProvaProviderPosta`) |
| M13 | riga «acceso ma non configurato» con `||` | uccisa (`TestAccesoMaNonConfigurato`) |
| M14 | riga anche a email spenta | uccisa (idem) |
| M15 | codice QR ignorato (sempre «N. …») | uccisa (`TestSospeseConNumeroBusta`) |
| M16 | numero solo con il codice QR | uccisa (idem) |
| M17 | «non configurato» con il messaggio generico | uccisa (`TestProvaProviderPosta`) |
| M18 | errore del database non scritto nel registro | uccisa (`TestProvaProviderPostaErrore`) |
| M19 | riga «Busta …» tolta dallo stampo | uccisa (`TestSospeseConNumeroBusta`) |
| M20 | riga «acceso ma non configurato» tolta dallo stampo | uccisa (`TestAccesoMaNonConfigurato`) |
| M21 | frase «Togli {link}…» tolta | uccisa (`TestPopupSenzaIndirizzoPubblicoD46`) |
| M22 | `reply[body]` tolta dalle caselle normalizzate del controllo | uccisa (`TestCaselleDiPiuRigheNormalizzate`) |

## Banco

Giro di regressione `s069c`, `fab-225`, `-dati sintetici-import`, al primo giro: **BANCO VERDE**, 63 scenari,
1103 passi, 0 inattese, 0 errori; **BANCO BROWSER VERDE**, 16 scenari, 32 viste, 0 inattese, 0 errori. `provider-posta` confronta ancora la prova senza
configurazione («Posta SMTP non è configurato: niente è stato accodato.», `outgoing.queued` 0). Nessuno scenario
nuovo: la prova con la posta configurata aprirebbe una connessione (mai nel banco: solo `.invalid`); numero di
busta e riga «acceso ma non configurato» compaiono solo con delle email in coda o un
salvataggio a metà che il banco non fa, e D-83 non si vede senza un invio: li provano i test Go.
`banco giu`, `banco controlla`: nessun avanzo.

## Controlli

`scripts/check` verde (prima e dopo le mutazioni e la documentazione); `scripts/parole-vietate --staged` verde
(sui file cambiati con `--file`; con `--staged` lo rilancia la sessione principale al commit).

## Per il manuale

- «✉ Invia messaggio di prova» parte subito, anche con l'invio delle email spento, e dice se il server l'ha
  accettato o il codice dell'errore; non passa dalla coda.
- Nelle email da guardare c'è il numero della busta; con l'invio acceso e i dati del server incompleti la
  pagina lo dice.
- Il riquadro dell'avviso senza indirizzo pubblico dice anche «Togli {link} dal testo per mandarlo senza link.».
- In `docs/manuale/da-scrivere.md`.

## Dubbi

1. **Audit della prova `outgoing.test_sent`** (nome nuovo, solo Go) invece di `outgoing.queued`: non c'è una riga
   da accodare. **Raccomandazione:** tenerlo; se un giorno la pagina del registro traduce le azioni, aggiungere
   «Prova del provider di posta».
2. **La prova tiene la richiesta fino a 2 minuti** con un server che risponde piano (i tempi massimi dell'SMTP);
   la pagina non mostra niente nel frattempo. **Raccomandazione:** lasciarlo (è il titolare che la chiede, una
   volta); se dà fastidio, un tempo massimo più corto solo per la prova.
3. **Nome mittente lungo:** con D-83 un nome non semplice lungo (fino a 200 caratteri) diventa una sola parola
   codificata più lunga dei 75 caratteri dell'RFC 2047 (lo standalone fa lo stesso con i nomi non ASCII). I
   server la accettano quasi sempre. **Raccomandazione:** lasciarlo; spezzarla in più parole solo se un server
   la rifiuta.
4. **Testi solo del Go nuovi** (esito della prova, riga «acceso ma non configurato», frase sotto l'interruttore):
   scritti dall'implementatore. **Raccomandazione:** rivederli con le schermate del manuale, come gli altri.
5. **Caratteri «semplici» della D-83:** il testo del coordinatore diceva «lettere, cifre e spazi»; la sessione
   principale l'ha scritto come `@plain_name` dello standalone (anche punto, apostrofo e trattino in chiaro, per
   esempio «Negozio S.r.l.»), perché la raccomandazione accettata era «quando non è semplice» e quei segni non
   cambiano l'indirizzo. **Raccomandazione:** tenerlo così.
6. **Numero di busta D-64** (non D-82) nella lista delle email da guardare: è una pagina, e le pagine usano la
   D-64. Una busta nel cestino mostra ancora il suo numero. **Raccomandazione:** tenerlo così.
7. **Frase del popup:** dice «Togli {link}…», ma anche `{telegram_optin}` blocca l'invio allo stesso modo.
   **Raccomandazione:** con la T4.7 (Telegram) scrivere «Togli {link} e {telegram_optin} dal testo…».
8. **Due clic su «Invia prova»** mandano due email di prova al mittente (nessun lucchetto). È innocuo.
   **Raccomandazione:** lasciarlo.
9. **Riga `From:` oltre 998 byte** con un nome non ASCII molto lungo (come lo standalone; la D-83 non lo
   peggiora per i nomi ASCII). **Raccomandazione:** come il Dubbio 3.

## Review

Revisore indipendente, 12/11/2026 (pc-fisso, slice non committata). Nessuna ALTA. Mutazioni del revisore: 21
che compilano, 16 uccise, 5 sopravvissute (R8, R14, R15, R18b → test rafforzati, ora uccise, ripassate dalla
sessione principale; R20 → Dubbio 6).

- **MEDIA, corretto:** il test-guardia delle caselle riconosceva le interne solo per nome (una casella con lo
  stesso nome in un'altra pagina passava) e leggeva solo `name="…"`. Ora la chiave è «file nome», legge anche
  le virgolette semplici e i nomi senza virgolette, e conta tutte le `<textarea>` della pagina.
- **BASSA, corretto:** la prova con la richiesta chiusa ora pretende `smtp:transport::closed` in pochi secondi
  (non il tempo massimo); un caso con la configurazione a metà pretende il rifiuto dello standalone e nessun
  audit.
- **BASSA, Dubbi 5 e 6:** testo delle decisioni ritoccato dalla sessione principale (detto qui).
- **BASSA, Dubbio 9:** riga `From:` lunga, già nello standalone.
- **Domande di prodotto:** Dubbi 6, 7, 8.
- **Verificato senza rilievi:** D-83 senza iniezioni possibili, prova solo al mittente salvato e solo con la
  configurazione usabile, nessuna riga in coda, errori senza dati, CSRF e solo titolare, lucchetto `aMano` non
  toccato, sottoquery del numero di busta, riga «acceso ma non configurato», escape, frase del popup, elenco
  completo delle caselle, pagina pubblica non toccata, banco (rifiuto senza configurazione ancora confrontato).
  `scripts/check` verde.
