# 100 — Slice corta: lavori dello standalone spenti nel banco, campo dello scanner del lotto, testi della 098

- **Via:** ok diretto di Stefano del 19/11/2026 («ok slice corta poi T4.10, ok ai testi con promemoria»), passato dal
  coordinatore. Terza slice del 19/11.
- **Voci:** nessuna voce cambiata. Nessuna D nuova.

## Cosa c'è

1. **Banco: lavori automatici dello standalone spenti** (`tools/banco/lavori.go`, passo `lavori` di
   `catenaStandalone` dopo i token e prima della copia del DB; container `grgo-<nome>-lavori`, tolto da `banco giu`).
   Mette `jobs.overdue_enabled`, `jobs.chases_enabled`, `jobs.briefing_enabled` a «0» con `Settings.put/3`, controlla
   con `Config.get` e `Jobs.due?` e scrive `GR_LAVORI:ok`. Con il lavoro spento `Jobs.due?` risponde no e `run_due` non
   scrive né righe «Saltato» né audit (misurato dal revisore su `fab-225`: 0 righe; con le impostazioni cancellate 2 righe
   «skipped» e 2 di audit). Il Go del confronto eredita le impostazioni con l'import; il Go con Telegram
   (`seminaTelegramGo`) riaccende ritardi e solleciti nella sua copia per `telegram-promemoria`.
   **Perché:** togliere dal confronto le righe e l'audit che dipendono dall'ora del giro. **Non è il rimedio della
   nota 8**, che resta aperta (vedi sotto). C'era anche un'altra via, misurata dal revisore: una copia di `sys.config`
   con `tick_interval_ms` a nil montata in sola lettura e `RELEASE_SYS_CONFIG`. Si è scelto lo spegnimento nei dati
   perché non tocca file generati della release ed è lo stesso gesto della casella «Attivo» (ok del coordinatore).
2. **CSS:** `.lotto-qr input` con `min-width: 12rem`: sul telefono il campo `#ready-lot-scan-code` va a capo invece di
   schiacciarsi accanto ai pulsanti (Dubbio 1 della 099). Desktop e pagina delle consegne invariati (misurato dal
   revisore da 320 a 1280 px su una pagina con la stessa struttura). Schermata `09-lotto-pagina-telefono.png` rifatta.
3. **Testi della 098 approvati da Stefano il 19/11/2026**, con un cambio: «Non è stato possibile eseguire il job:
   riprova.» → «Non è stato possibile avviare il promemoria: riprova.». Scritto nella resa D-16/D-102 di SVILUPPO.

Scenari: `bench/scenari/promemoria.json` attende le caselle «Attivo» spente solo con `-dati sintetici-import`
(`atteso_dati`); `bench/deviazioni.json`: corretto il motivo delle due righe D-16.

## Prove

- Rosso: `tools/banco` senza il codice nuovo (simboli mancanti) e il testo della 098 (3 errori). Test nuovi
  `TestComandoLavori` (anche montaggi e ambiente), test della catena e di `banco giu`, `TestCampoQRDelLottoVaACapo`,
  `TestPromemoriaEseguiNonRiuscito` (guasto provocato con un trigger SQLite).
- `scripts/check` verde, rilanciato dalla sessione principale prima e dopo le correzioni.
- **Mutazioni dell'implementatore:** 16, tutte compilano e tutte uccise; poi R07 e R08 del revisore uccise.
- **Revisione indipendente (Opus):** 1 ALTA chiusa nella slice (l'ipotesi della nota 8 è smentita dalle ore: il testo
  non presenta più lo spegnimento come rimedio); 2 MEDIE chiuse (attese dello scenario anche senza `-dati`; frase falsa
  «senza un'altra immagine non si ferma»); BASSE: test dei montaggi chiuso, controllo `Jobs.due?` ridondante lasciato.
  Mutazioni del revisore: 16 compilanti, 10 uccise, 2 equivalenti, 4 vive (R03 e R04 dello script Elixir, le vede solo
  un giro vero; R07 e R08 poi uccise).

## Banco

**Giro s100a (19/11/2026): VERDE al primo giro.** Passo `lavori`: `GR_LAVORI:ok`. HTTP 86 scenari, 1347 passi, 0
inattese, 0 errori; browser 29 scenari, 58 viste, 0 inattese, 0 errori. Memoria disponibile minima 22,4 GB. Copertura
con l'esito: 353 voci, chiuse 213, rosse 0, parziali 16, aperte 113, fuori v1 11. Nessuna comparsa della nota 8 in
questo giro (un giro solo non dice niente sulla causa). Banco manuale m100a: rifatta `09-lotto-pagina-telefono.png`
(il campo ora sta a tutta larghezza sopra i pulsanti; rigenerata a caso anche `06-fotocamera-telefono.png`, scartata).

## Per il manuale

- Pagina del lotto sul telefono: il campo «Scansiona o inserisci il codice QR» non si schiaccia più accanto ai
  pulsanti; va a capo. Su schermo largo non cambia.
- Pagina «Promemoria e job»: se l'avvio a mano di un promemoria fallisce, la pagina dice «Non è stato possibile
  avviare il promemoria: riprova.».

## Dubbi

1. **Nota 8 aperta:** prova proposta dal coordinatore (non in questa slice): contare le comparse in un giro con lo
   standalone in modalità `deferred`, solo nel banco, mai come modifica dello standalone usato per il confronto.
2. **Carattere del campo dello scanner del lotto:** `#ready-lot-scan-code` usa un carattere a spaziatura fissa, diverso
   dal campo «Numero busta» accanto (si vede nella schermata nuova). C'era già prima; non toccato. Da mettere in lista.
