# 102 — Stima dei tempi nelle impostazioni del negozio (T4.10, seconda)

- **Fonte:** standalone a `go-baseline`: `lib/gestrip_standalone/settings/estimate.ex` (`read` 40-46, `put` 53-91,
  `build`/`days`/`submitted_days`/`stored` 103-131), `lib/gestrip_standalone_web/live/shop_live.ex` (`save_estimate`
  93-105, catch-all 136-140, `assign_estimate` 148-157, sezione 284-338), `repair_estimate.ex` 59-99 (supplemento
  misurato o dalle impostazioni). Inventario ADM-04, RIP-H-55.
- **Voci:** ADM-04 e RIP-H-55 chiuse (la parte negozio di RIP-H-55 l'ha fatta la 101). Nessuna D nuova.

## Cosa c'è

- **`internal/buste/stima_impostazioni.go`** (nuovo): `PaginaStima` (valori attuali, riga «Oggi», supplemento
  «misurato sui dati» o «dalle impostazioni») e `SalvaStima`: interruttore acceso solo da `true`/`1`/`on`, salvato
  «1»/«0»; giorni interi senza resto riportati in 0–90, testo non intero → 7 o 8, campo assente → tiene il valore,
  massimo sotto il minimo → alzato al minimo (come `build/3`); chiavi e audit `settings.estimate_updated`
  (`{"changed":[chiavi]}`, mai i valori) nella stessa transazione, con la lettura dei valori attuali dentro la
  transazione. **`pagina_cliente.go`:** lettura delle chiavi condivisa con 061/101, costanti 7/8, campo `misurato`.
- **`internal/web/negozio.go`, `pagine/negozio.html`, `statici/app.css`:** sezione «Stima tempi» sotto il profilo, solo
  titolare, `POST /settings/shop/stima` (nello standalone è l'evento LiveView `save_estimate`: rotta normale del Go,
  nessuna D, ok del coordinatore 20/11/2026), 302 con «Stima tempi salvata.» / «Stima tempi non salvata: riprova.»,
  CSRF; un modulo forgiato (testo, `estimate[]`, chiavi con prefisso come `xestimate[enabled]`, campi del profilo) →
  «Richiesta non riconosciuta», come il catch-all dello standalone. `nuova.go`: solo riuso della lettura.
- **Import:** la stima resta spenta dopo un import (test in `internal/importer/stima_test.go`; l'importer già lo
  faceva).
- **Banco:** `bench/scenari/negozio-stima.json` (HTTP, pagina e valori di partenza nelle due app);
  `negozio-dipendente.json` porta anche ADM-04 (il dipendente viene rimandato alla home); `bench/browser/stima-negozio.json`
  con `banco.mjs`: salvataggio con valori fuori campo, poi stima **accesa** e lettura delle schede delle buste 10 e 5,
  infine rimette interruttore e giorni come li ha trovati (`finally`). Attese fisse con i numeri dipendenti dai dati
  normalizzati («(ora N)»).
- **SVILUPPO:** sezione «Stima dei tempi nelle impostazioni (slice 102…)».

## Scelte

1. **Caso «stima accesa» nel browser, non in `scheda-stima`** (obbligo del coordinatore del 19/11): nello standalone
   l'impostazione si salva con un evento LiveView, quindi uno scenario HTTP non la può accendere. Il banco dice solo
   che non si rompe niente: con meno di 20 buste pronte la scheda mostra «Dati insufficienti» sia accesa sia spenta;
   la differenza vera la provano i test Go (`TestStimaNegozioInterruttoreSullaScheda`,
   `TestStimaImpostazioniEPaginaDelCliente`). Ok del coordinatore, 20/11/2026.
2. **Rifiuto del POST al dipendente** coperto solo dai test Go (nel banco il dipendente è rimandato alla home nelle
   due app). Ok del coordinatore.
3. Dopo il giro del browser restano tre righe di `impostazioni` («0», «7», «8») dove prima mancavano: chi le legge
   vede lo stesso valore.

## Prove

- Rosso: `buste` e `web` non compilavano; i 2 test dell'import verdi da subito (fissano una proprietà già vera).
  Test nuovi: 9 in `buste`, 11 in `web`, 2 in `importer`, con i segni ADM-04 e RIP-H-55.
- `scripts/check` verde, rilanciato dalla sessione principale prima della revisione e dopo le correzioni.
- **Mutazioni dell'implementatore:** 53 (50 compilanti), 47 uccise, 3 equivalenti (lettura fuori transazione senza
  concorrenza, `!esiste` ridondante, `return` finale); 4 vive poi uccise rafforzando i test; dopo la revisione W16
  (ancora `^` tolta) uccisa.
- **Revisione indipendente (Opus):** 0 ALTE, 0 MEDIE sul codice; 1 MEDIA sul banco (il punto 1 delle Scelte, accettato
  dal coordinatore); 2 BASSE chiuse (test delle chiavi con prefisso; attese fisse per `pagina.oggi`, `accesa.oggi`,
  `accesa.scheda5.*`). Mutazioni del revisore: 29 compilanti, 27 uccise (una per stallo del test), 1 equivalente
  (M10, lettura fuori transazione), 1 viva (M23) poi uccisa dal test nuovo.
- Implementatore Sonnet 5.5 high: ~350k token, ~37 minuti con le correzioni.

## Banco

**Giro s102a (20/11/2026): non partito.** `banco su` si è fermato al controllo dello spazio: «spazio su disco
insufficiente: 63 GiB liberi» (`/home` di pc-fisso al 94%, il banco vuole al massimo il 90%). Nessun container
acceso. Il coordinatore ha fatto liberare spazio (disco al 70%).
**Giro s102b (20/11/2026): VERDE al primo giro.** HTTP 88 scenari, 1363 passi, 0 inattese, 0 errori; browser 30
scenari, 60 viste, 0 inattese, 0 errori; la guardia sotto 10 GB non è mai scattata. (Il primo `banco esegui` è caduto
per un errore dello script del giro, senza `-banco`; rilanciato sulla stessa istanza, la catena non era stata toccata.)
Copertura con l'esito: 353 voci, chiuse 216 (ADM-04, RIP-H-55), rosse 0, parziali 16, aperte 110, fuori v1 11.

## Per il manuale

- Nelle «Impostazioni del negozio», sotto il profilo, c'è la sezione «Stima tempi», solo per il titolare.
- La stima parte spenta: la accende il titolare con «Mostra la stima ai clienti». Un import non la accende.
- Il supplemento dell'artigiano (giorni minimi e massimi, 7-8 di partenza) vale finché non ci sono 10 buste misurate;
  i giorni vanno da 0 a 90.
- Un valore fuori campo non dà errore: viene riportato nel campo (−5 diventa 0, 120 diventa 90, un massimo sotto il
  minimo diventa uguale al minimo).
- La riga «Oggi» mostra la stima attuale («Pronta di solito in N-M giorni dal ritiro») o «Dati insufficienti» con i
  conti, e dice se il supplemento è «misurato sui dati» o «dalle impostazioni».
- Dopo «Salva stima tempi» compare «Stima tempi salvata.» e la scheda smette di dire «stima spenta nelle
  impostazioni». Il salvataggio finisce nel registro, senza i valori.

## Dubbi

1. Gli scenari nuovi non sono mai stati provati contro lo standalone vero: il primo giro potrebbe segnalare differenze
   di markup o di selettori da aggiustare.
2. Lo scenario browser suppone meno di 20 buste pronte nella catena e «Giorni in più» sulla busta 5; altrimenti cade
   con un errore chiaro.
