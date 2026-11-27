# 132 — Manuale cap. 11 «Backup e ripristino» (T5.2, ultima slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`), riga 132 e risposte A–K; PIANO, «Manuale
  d'uso»; le sezioni «Per il manuale» delle ricevute 119–131; voce 14 di `docs/domande-aperte.md`.
- **Voci:** nessuna voce cambiata (documentazione, ricette del banco e due testi). Nessuna D nuova (ultima D-118).
- **Modelli:** scrittore Opus (~213k token in due giri, ~15 minuti), revisore Opus indipendente (~188k token, ~5
  minuti, solo lettura mentre girava il banco).

## Cosa c'è

- **`docs/manuale/11-backup-e-ripristino.md`** (nuovo): cosa si salva e dove; il backup è una credenziale (anche la
  chiave con cui l'apparecchio entra nella Storage Box: chi ha backup + chiave dei backup può cancellare le copie fuori
  sede; in caso di furto chiedere subito di togliere la chiave dell'apparecchio dal pannello della Storage Box); la
  chiave dei backup e il foglio; backup notturno alle 02:30, rotazione 7 + 4, chiavetta; copia remota; prova
  settimanale (al più 3 tentativi, poi la domenica dopo) e mensile della copia remota (ogni notte finché non riesce);
  pagina Backup, configurazione della copia remota, riquadro rosso e avvisi (anche «di nuovo tutto a posto» dopo
  mezz'ora); tabella «allarme → cosa fare»; comandi dalla console; ripristino passo passo (`restore`/`ripristina`,
  chiavetta da indicare nel file dell'apparecchio della macchina nuova), copia isolata, conferma (macchina vecchia
  spenta, Scarta/Manda, CONFERMO, `conferma-ripristino`, le scelte fatte durante l'isolamento restano), riavvio, primo
  backup 10 minuti dopo il riavvio.
- `00-sommario.md` (voce 11 col link), `99-glossario.md` (15 voci nuove, fra cui Apparecchio, Console, File
  dell'apparecchio, Copia isolata, Chiave dei backup, Storage Box), `da-scrivere.md` (figure mancanti del backup).
- **Schermate:** `11-backup-pagina-desktop.png` (ricetta `backup-pagina`), `11-copia-remota-desktop.png`
  (`copia-remota`), `11-copia-remota-chiave-desktop.png` (`copia-remota-chiave`, crea la chiave e scrive fissa una
  riga `ssh-ed25519` inventata), in `bench/manuale/schermate.json` e `tools/banco/browser/manuale.mjs`, prima di
  `assistente-configurato`. **Solo a parole** (il banco non le prepara): configurazione con un server SFTP vero,
  riquadro rosso (il Go del banco gira senza lavori automatici), riquadro giallo e pagina della conferma.
- **Due testi corretti nel codice** (errore della 131 trovato dalla revisione): pagina della conferma
  (`internal/web/pagine/ripristino.html`) e console (`cmd/gestrip/conferma_ripristino.go`) dicevano «la prima notte
  dopo il riavvio si fa un backup nuovo»; il ripristino svuota `esecuzioni_backup`, quindi il backup parte 10 minuti
  dopo il riavvio (se la chiavetta è configurata), seguito dalla prova. Nessun test confrontava le frasi.

## Dove ricevute e codice non coincidevano (si è seguito il codice)

Ora del backup solo mostrata, non modificabile (risposta C: **voce 16** di `docs/domande-aperte.md` per Stefano);
pagina della conferma raggiunta dal riquadro giallo, non dalla pagina Backup; 3 tentativi in tutto per notte; esempio
del nome dell'archivio in UTC (`013000Z` a novembre); blocco delle cancellazioni solo per i database importati; primo
backup dopo la conferma (sopra).

## Prove

- `scripts/check` verde rilanciato dal capo-slice dopo le correzioni (voci 353: chiuse 57, rosse 0; segreti e dati veri
  OK); `node --check` sul `.mjs`, `TestSchermateDelRepo`.
- **Banco manuale** (giro `man`, «memoria libera» del coordinatore): verde al primo giro, 41 PNG; le tre nuove guardate
  dalla sessione principale. Cinque PNG vecchie ridisegnate con differenze del giorno rimesse com'erano. Guardia sotto 10
  GB attiva, minimo visto 19 GB; `banco giu` senza avanzi.

## Revisione (Opus indipendente, solo lettura: 1 ALTA, 4 MEDIA, 4 BASSA, tutte corrette)

ALTA: mancava l'avvertenza sulla chiave SFTP dentro il backup. MEDIA: primo backup dopo il riavvio; prova settimanale
descritta come ripetuta ogni notte; chiavetta da configurare sulla macchina nuova; scelte fatte durante l'isolamento.
BASSA: mezz'ora per «tutto a posto», parole da spiegare e voci del glossario, note sulle figure fuori dal capitolo,
«ricevute» in `da-scrivere.md`. Verificati: numeri e orari, testi citati, comandi e opzioni, ricette e selettori,
repository pubblico pulito.

## Mutazioni

Nessuna: documentazione, ricette del banco (provate dal giro) e due testi.

## Per il manuale

È il manuale: capitolo 11.

## LSP

Scrittore 3 usi (`ParolaDiConferma`, riferimenti di `Stato.Allarme`), revisore nessuno (grep e lettura).
