# 084 — Link e pagine pubbliche dell'artigiano (T4.8, nona e ultima)

- **Fonte:** standalone a `go-baseline` (`302439d`): `lib/gestrip_standalone/artisan_link.ex` (`sign/2`,
  `public_link/1`, `projection/2`, `legacy_projection/1`, `verify/2`, `@salt`, `@max_age`, `@projected`),
  `lib/gestrip_standalone_web/controllers/public_artisan_controller.ex` (`show/2`, `legacy/2`, `photo/2`,
  `legacy_photo/2`, `public_headers/1`), `public_artisan_html/show.html.heex`, `components/photo_viewer.ex`,
  `legacy_public_links.ex` (`verify_artisan/4`, vettore `:artisan` di `compatibility_evidence/0`),
  `repair_photos.ex` (`read_public_media/5`, `read_public_filename/5`), `outgoing.ex`
  (`resolve_artisan_link/2`, `destination_for_artisan/1`), `repair_live.ex` (`#repair-artisan-link`,
  `artisan_link/1`, `assign_artisan_send/2`), `config/runtime.exs` (`SECRET_KEY_BASE_FILE`), `router.ex`.
- **Voci:** ART-23, ART-24, ART-25, ART-26, RIP-S-25. **D-87** fatta (Phoenix.Token degli artigiani accettati).
  Nessuna D nuova.
- **Lavoro:** sottoagente d'implementazione, test rossi prima (compilazione) poi codice, mutazioni a mano. Il banco
  non è stato lanciato: lo fa la sessione principale.

## Cosa c'è

- **Phoenix.Token** (`internal/firmephoenix`, nuovo): `Segreto` (il `secret_key_base`, non si stampa), `Leggi`
  (regole di `SECRET_KEY_BASE_FILE`: file privato, al più 4096 byte, almeno 64 senza spazi ai bordi; assente =
  nessun gettone accettato), `VerificaIntero` (chiave PBKDF2-SHA256 1000 giri 32 byte con il sale, una per sale in
  memoria; `SFMyNTY.<payload>.<firma>`, HMAC a tempo costante sui due pezzi come scritti; payload
  `{dato, firmato_ms, max_age}` in External Term Format: versione 131, tupla piccola di tre interi
  SMALL_INTEGER/INTEGER/SMALL_BIG fino a 8 byte entro int64, niente dopo; tutto il resto è un rifiuto), `FirmaIntero`
  (solo test e banco).
- **Comportamento misurato** su Plug.Crypto 2.2.0 dell'immagine dello standalone (`fab-225`, `docker run --network
  none`, erts della release, nessun servizio): un gettone **firmato dopo adesso è `:invalid`** (anche di 1 s); è
  `:expired` quando `firmato + max_age < adesso` (in ms); il `max_age` scritto nel gettone non conta quando la verifica
  ne passa uno. Il Go fa lo stesso.
- **Vecchi gettoni Flask** (`firmeflask.VerificaArtigiano`, `FirmaArtigiano`): `<exp>.<16 esadecimali minuscoli>`
  su `artigiano:<numero>:<exp>`, il vettore dello standalone.
- **Dominio** (`internal/buste/artigiano_link.go`): `ConLinkArtigiano` (sottochiave del Go e segreto dello
  standalone), `GettoneArtigiano` («`g1.<firmato_ms>.<HMAC base64url>`» con `db.SottoChiave(chiave, "artigiano")` e un
  prefisso di dominio; 90 giorni compresi, non revocabile, rifiutato se firmato dopo adesso, istante solo cifre senza
  zeri davanti), `LinkArtigiano`, `RisolviLinkArtigiano` (`{link}` di `wa_artigiano`; senza indirizzo pubblico
  `ErrSenzaIndirizzoPubblico`), `PaginaArtigiano` (gettone del Go o Phoenix.Token; il dato deve essere l'id della
  rotta), `PaginaArtigianoStorica`, `LeggiFotoArtigiano` (per id), `LeggiFotoArtigianoStorica` (per nome file). La
  proiezione legge solo id, numero, QR, descrizione, stato, oreficeria. Audit: `repair.public_artisan_viewed|denied`,
  `repair.legacy_artisan_viewed|denied` (rifiuti con id «unknown»), `repair.public_photo_viewed` con `audience`
  «artisan» (`photo_id` ripulito o «filename»); una riga della pagina che non si scrive è il rifiuto.
- **Scheda** (RIP-S-25): `Scheda.LinkArtigiano` (nuovo a ogni lettura, solo con artigiano, non consegnata e
  indirizzo pubblico) e `ArtigianoSenzaTelefono` (busta non chiusa, artigiano senza numero WhatsApp); riquadro
  `#repair-artisan-link` sotto le azioni, con `#repair-artisan-link-url` e `#repair-artisan-send-missing`.
- **Pagine** (`internal/web/artigiano.go`, `pagine/artigiano.html`, solo faccia pubblica): `GET /artigiano/{id}`,
  `GET /artigiano/{busta}/{anno}`, `GET /artigiano/{id}/foto/{foto}`, `GET /artigiano/{busta}/{anno}/foto/{file}`;
  `no-store`, `no-referrer`, `X-Robots-Tag: noindex, nofollow` solo sulla pagina; ogni rifiuto della pagina è
  `RifiutoPubblico` «Riparazione non disponibile» (contato dal limite, famiglia `artigiano` già in `limiti.go`), delle
  foto «Foto non disponibile» (non contato). Id degli elementi come lo standalone (`artisan-bag-number`,
  `artisan-status`, `artisan-description`, `artisan-photos`, `artisan-photo-<id>`, `artisan-photo-viewer-<id>`,
  `artisan-photos-empty`). Il gettone `t` vale solo se è un testo.
- **Avvio** (`cmd/gestrip`): `secrets/segreto_standalone` accanto al database (`percorsoSegretoStandalone`), letto
  prima di aprire il database; sbagliato ferma l'avvio («segreto dello standalone: …», senza il contenuto).
- **Documenti:** SVILUPPO, sezione «Segreto dello standalone e link dell'artigiano» e banco; riga 083 (sotto).

## Scelte

1. **Il gettone del Go non è un Phoenix.Token.** Il piano chiede una sottochiave del Go: un formato suo con prefisso
   `g1.` e dominio nell'HMAC; i due tipi non si scambiano (test «gettone del go»/«phoenix come go»). Il Go non firma
   mai Phoenix.Token fuori da test e banco.
2. **Cestino:** una busta nel cestino è un rifiuto (lo standalone non lo guarda), come `/r/` e `/stato` del Go.
3. **Vecchio link Flask:** la busta si trova come `/stato` (numero com'è, poi senza zeri, D-77), ma la firma vale
   **solo sul numero scritto** nell'indirizzo, come lo standalone che cerca il numero esatto (a differenza di `/stato`,
   dove il Go accetta anche il numero salvato). Le foto del vecchio link usano il numero scritto (lo standalone quello
   salvato, uguale fuori dalla D-77).
4. **Numero sulla pagina:** quello visibile della D-64 (`NumeroVisibile`: «0045/26», senza etichetta «N. 45/26»);
   lo standalone scrive il numero interno. Vedi Dubbio 1.
5. **Intestazione del Phoenix.Token:** solo `HS256` (Plug accetta anche HS384/HS512, che lo standalone non scrive).
6. **`{link}` di `wa_artigiano`:** `RisolviLinkArtigiano` c'è ed è provata, ma nessuna pagina del Go la usa ancora: il
   popup «🔧 Sollecita artigiano» della scheda è dell'area WhatsApp (inventario, «Fuori area», CLI-29) e non è
   portato. Vedi Dubbio 2.

## Prove

- `internal/firmephoenix`: **vettori veri** di Plug.Crypto 2.2.0 (sei gettoni con dati SMALL_INTEGER, INTEGER e
  SMALL_BIG, millisecondi, `max_age` nel gettone; un altro sale; un dato testo; la chiave derivata), generati con
  `Plug.Crypto.sign/4` nell'immagine dello standalone con un `secret_key_base` inventato; la firma del Go dà gli stessi
  byte; rifiuti (sale, segreto, parti toccate, due o quattro parti, riempimento, HS512 anche firmato), termini cattivi
  (versione, arità, testo, virgola mobile, byte in più o in meno, big oltre int64), segreto che non si stampa, `Leggi`.
- `internal/firmeflask`: il vettore `:artisan` dello standalone e i rifiuti di forma (maiuscole, 15/17, punti, segno,
  spazi, a capo, numero vuoto firmato).
- `internal/buste`: gettone del Go (formato fissato da un vettore calcolato fuori, 90 giorni compresi, altra busta,
  toccato, zeri davanti, altra chiave, nessuna chiave), Phoenix.Token (90 giorni compresi, dopo adesso, altro sale,
  altro segreto, tipi scambiati), proiezione (numero D-64, stato, foto dell'artigiano, consegnata e senza artigiano
  rispondono, cestino no, `/r/` e pagina artigiano non si aprono a vicenda), foto per id e per nome, vecchio link Flask
  (D-77, rifiuti, audit), `{link}`.
- `internal/web`: dal link della scheda alla pagina e alle foto (intestazioni, niente del cliente, foto della busta
  mai), quindici rifiuti **byte per byte uguali** (codice, corpo, intestazioni tranne `Date` e `X-Request-Id`), Phoenix e
  Flask che aprono, riquadro della scheda (senza numero, annullata, consegnata, senza artigiano, senza indirizzo).
- `cmd/gestrip`: `TestServeSegretoStandalone`. `tools/banco`: firme Flask dello scenario, `{phoenix_artigiano:…}`,
  `{href:…}` assoluto.

## Mutazioni

Ventotto mutazioni a mano in una copia del repo (poi cancellata), con i test mirati dei quattro pacchetti:
**22 uccise al primo giro, 5 vive poi uccise con test nuovi, 1 equivalente tolta dal codice**.
- Uccise al primo giro: numero di parti; HMAC ignorato; un secondo di tolleranza nel futuro; scadenza di 1 ms dopo;
  999 giri; versione 131; byte in più; big di 9 byte; big oltre int64; HMAC sul solo payload; parte «stato» al posto
  di «artigiano»; dato del Phoenix non confrontato con l'id; HMAC del Go ignorato; 1 ms in più ai 90 giorni; zeri
  davanti all'istante; cestino; pubblico «customer» al posto di «artisan»; firma Flask anche sul numero salvato; riga
  del rifiuto non scritta; `X-Robots-Tag` tolto; `t[a]=` letto come gettone; link anche sulla consegnata.
- Vive poi uccise: intestazione non controllata (test HS512 con l'HMAC giusto), arità maggiore di 3 (termine di arità 4
  con tre elementi), numero vuoto (firma vera di «artigiano::exp»), prefisso di dominio tolto (vettore fisso del
  gettone), «Nessun telefono» anche sulla busta annullata (test della annullata).
- Equivalente: il controllo di forma della firma in `VerificaArtigiano` ripeteva quello di `verifica`: tolto.

## Controlli

`scripts/check` verde (ultimo giro sul codice finale, vedi la ripresa).

## Banco (da lanciare: sessione principale)

- **HTTP `pagina-artigiano`** (nuovo, `prova_con` `sintetici-import`, 23 passi): la 4/26 e la 1/26 della cattura col
  vecchio link Flask firmato dal banco (`4102444800.<16hex>`, ricontrollato da `TestFirmeScenarioArtigiano`) e col
  Phoenix.Token `{phoenix_artigiano:<id>:60}` firmato a ogni richiesta con il `secret_key_base` **sintetico e fisso**
  del banco (`segretoStandaloneBanco`): lo standalone lo verifica con Plug.Crypto vero, il Go con `firmephoenix`, quindi
  il banco è anche la prova dei vettori contro l'originale. Foto 4 della 1/26 intera e miniatura, foto di un'altra busta
  e id non valido; quindici rifiuti con codice, corpo e intestazioni (senza gettone, zeri, scaduto, maiuscole,
  `04/26`, `4/25`, Phoenix sulla rotta Flask e viceversa, Phoenix scaduto oltre 90 giorni, altra busta, `abc`, busta che
  non c'è, `t[a]=`).
- **Modifiche al banco:** `secret_key_base` dello standalone fisso e sintetico (prima casuale), lo stesso al Go in
  `go-data/secrets/segreto_standalone`; Caddy manda anche `/artigiano/*` alla faccia pubblica; `{href:…}` di un link
  assoluto dà percorso e query.
- **I link nuovi del Go sono solo Go** (D-02): nel banco il Go non ha `indirizzo_pubblico`, quindi la scheda non mostra
  il riquadro e il gettone `g1.` non si prova via HTTP; lo coprono i test Go.
- **Attese da guardare per prime:** il passo 0/2 (`/artigiano/4/26` e `/artigiano/1/26`) confronta stato e
  lavorazione e `conteggio:?size=thumb`; il numero della busta **non** si confronta (D-64 nel Go, numero interno nello
  standalone). Se lo standalone desse `:invalid` al Phoenix firmato 60 s prima (orologi diversi tra host e container),
  i passi Phoenix darebbero 404 solo lì.
- **Rischi:** il `secret_key_base` fisso cambia le chiavi di sessione dello standalone del banco (sintetiche, nessun
  effetto atteso); gli altri scenari non lo leggono.

## Dubbi

1. **Numero sulla pagina dell'artigiano.** Il Go mostra il numero visibile della D-64 con «N.» senza etichetta
   («N. 45/26»); la pagina del cliente lo mostra senza «N.» (D-82 estesa con la 074c). **Raccomandazione:** estendere
   la D-82 anche alla pagina dell'artigiano (una riga: `NumeroNeiTesti` al posto di `NumeroVisibile`).
   **Deciso da Stefano il 15/11/2026: D-82 estesa alla pagina dell'artigiano, nessuna D nuova; fatto nella slice 085.**
2. **Popup «🔧 Sollecita artigiano» della scheda** (`wa_artigiano` con `{link}`, una riga «accettata» verso
   l'artigiano): non è nel Go e nessuna slice lo nomina (inventario, area WhatsApp, CLI-29). `RisolviLinkArtigiano`
   è pronta. **Raccomandazione:** una slice piccola dopo la T4.8, insieme alla riga «Nessun telefono…» che oggi è solo
   informativa.
   **Deciso da Stefano il 15/11/2026: slice piccola dedicata, la prima al via dopo la T4.8: fatto nella slice 085.**
3. **Riquadro senza indirizzo pubblico:** senza `indirizzo_pubblico` il Go non mostra il link (non saprebbe che
   indirizzo dare); lo standalone ha sempre l'host. Coerente con la D-46; da confermare.
   **Deciso da Stefano il 15/11/2026: senza `indirizzo_pubblico` il riquadro non compare (D-46), resta così.**
4. **Cutover:** il `secret_key_base` dello standalone va copiato in `secrets/segreto_standalone` sull'apparecchio
   (ce l'ha il coordinatore nel promemoria del cutover); senza, i link dello standalone ancora in giro (al più 90
   giorni) danno il 404.

## Dopo il banco s084p e la revisione di sicurezza (15/11/2026)

- **Banco `s084p`** (sessione principale): browser verde, `consegne-solleciti` della 083 verde, passi Phoenix uguali
  nelle due app. HTTP: 15 inattese, tutte `pagina-artigiano[8..22] intestazione:content-type` (standalone «», Go
  «text/plain; charset=utf-8»): `RifiutoPubblico` toglieva il tipo ma mandava intestazioni e corpo insieme, e il Caddy
  davanti metteva `text/plain` (lo stesso caso della 078, `rispostaSenzaTipo`). **Corretto:** `RifiutoPubblico` fa
  `Flush` dopo `WriteHeader` e prima del corpo (`TestRifiutoPubblicoFlush`, rosso prima). Vale per **tutti** i 404
  pubblici (`/r/`, `/stato`, `/lotto`, `/w`, `/g`, il blocco del limite): lo standalone li manda tutti con
  `send_resp/3`, senza tipo, quindi cambiano nella direzione giusta; nessuno scenario oggi confronta il
  `content-type` di quei 404 (né attende `text/plain`), quindi restano verdi. Le foto rifiutate facevano già il `Flush`
  (`rispostaFotoPubblica`). Le intestazioni dei rifiuti restano identiche fra loro (test byte per byte).
- **Revisione (BASSE):** avviso all'avvio se `secrets/segreto_standalone` manca («segreto dello standalone assente
  (<percorso>): i link Phoenix degli artigiani saranno rifiutati», senza contenuto; test, rosso provato); commento di
  `leggiIntero` corretto (accetta forme non canoniche, sicuro perché si legge solo dopo l'HMAC; logica invariata);
  quattro passi nuovi in `pagina-artigiano` (27 passi) con nomi ostili sulla foto del vecchio link Flask
  (`..%2F..%2Fsecrets%2Fsegreto_flask`, `%2Fetc%2Fpasswd`, `..%2Fgestrip.db`, `%2E%2E`) → «Foto non disponibile».
  Il `../` letterale non è nel banco (il client e il Caddy puliscono il percorso, il mux del Go risponderebbe 301): è
  nel test del dominio insieme agli altri nomi ostili.
- **Attesa del prossimo giro:** `pagina-artigiano` tutto verde; i passi dei rifiuti sono ora `[12..26]`.

## Banco finale (sessione principale)

`s084q`, 15/11/2026, istanza nuova sul codice definitivo dopo le correzioni di `s084p` e della revisione di sicurezza:
HTTP 76 scenari, 1273 passi, 0 inattese, 0 errori; browser 22 scenari, 44 viste, 0 inattese, 0 errori. Nei due giri i
passi Phoenix.Token hanno dato lo stesso esito nelle due app (prova incrociata con Plug.Crypto vero). `scripts/check`
rifatto dalla sessione principale: verde.
