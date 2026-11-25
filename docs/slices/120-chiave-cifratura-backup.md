# 120 — Chiave dei backup e cifratura age, comando `gestrip backup` (T5.2, seconda slice)

- **Fonte:** piano della T5.2 (`~/Condivisi/GestRip-Go/piano-T5.2-bozza.md`, approvato da Stefano il 25/11/2026),
  riga 120, risposte A e B, «Rischi» (media cancellati durante il backup); D-10, D-113, D-114 (niente rotazione
  qui); regole lasciate dalla 119 in SVILUPPO; difetto in lista 5a e 5b (review della 119).
- **Voci:** OPS-16 (parte: chiave, cifratura, scrittura atomica, comando a mano). Nessuna D nuova.
- **Dipendenza nuova:** `filippo.io/age` **v1.3.2**, licenza **BSD-3** (ok del coordinatore); porta
  `filippo.io/hpke` v0.4.0 (BSD-3) e `golang.org/x/crypto` v0.55.0 (BSD-3). Niente CGO: build statica verde.
- **Modelli:** implementatore Opus.
- **Banco:** nessuno (nessuna pagina cambia).

## Cosa c'è

- `internal/backup/cifra.go`: `CreaChiave` (identità X25519 in `secrets/chiave_backup`, 0600, formato di
  `age-keygen`, pubblicata con rinomina che non sovrascrive), `LeggiChiave` (file normale, dell'utente, senza
  permessi per altri, una sola identità), `Cifra` (`.pending` → age in flusso → fsync file e cartella → rinomina
  senza sovrascrivere → fsync cartella; `.pending` tolto con un errore), `NomeArchivio`, stima dello spazio della
  destinazione. Dettagli in SVILUPPO, «Backup: chiave e cifratura age».
- `internal/backup/sistema_*.go`: `rinominaSenzaSovrascrivere` (`renameat2` con `RENAME_NOREPLACE`), `dellUtente`.
- `internal/backup/archivio.go`: ogni scrittura del tar guarda il ctx (`scrittoreConContesto`).
- `internal/media`: lucchetto delle cancellazioni anche fra processi (`flock` su
  `<dati>/.lucchetto-cancellazioni-media`) e attese interrompibili con il ctx; `SenzaCancellazioni(ctx, f)`.
- `cmd/gestrip/backup.go` e `main.go`: comandi `chiave-backup` e `backup -cartella DIR`.

### Scelta: `gestrip backup` accanto a `serve` acceso

Nel repo nessun comando guarda se `serve` gira (niente pidfile né lucchetti su file; `esporta-dettati` e gli altri
si affidano a SQLite). Rifiutare il backup con `serve` acceso non va: il backup è a caldo (D-10) e il lavoro notturno
(124) girerà dentro `serve`. Quindi il lucchetto del 119 diventa anche **fra processi**: un `flock` su un file della
cartella dei dati, condiviso per il backup ed esclusivo per la coda delle cancellazioni (D-55), preso dopo il
RWMutex del processo. Il flock vale per descrizione di file aperto: due aperture si escludono come due processi, e
il kernel lo toglie se il processo muore. Il file sta fuori da `media/` (lì il backup accetta solo oggetti).

### Difetto 5a della review della 119: chiuso qui

Toccando il lucchetto, l'ho reso interrompibile: RWMutex con `TryLock`/`TryRLock` e flock con `LOCK_NB`, riprovati
ogni 50 ms finché il ctx non finisce. In più ogni scrittura del tar guarda il ctx, anche dentro `io.Copy` di un file
grande. Anche la 5b (nessun test per il lucchetto di `CancellaInCodaConLucchetto`) è chiusa da
`TestAttesaDellaPuliziaSiInterrompe`.

## Prove (tutte su dati inventati, in cartelle temporanee dei test)

- `internal/backup/cifra_test.go`: `TestChiaveCreata` (0600, cartella 0700, formato, nessun temporaneo),
  `TestChiaveNonSovrascrive` (seconda creazione, file rovinato, chiave comparsa fra controllo e rinomina),
  `TestLeggiChiave` (permessi larghi, contenuti sbagliati, due identità, collegamento), `TestCifraGiroCompleto`
  (cifra, decifra con l'identità, ogni voce del tar uguale a `inventory.json`, SHA-256 e byte dell'archivio, niente
  chiave dentro, un'altra chiave non apre), `TestCifraErroreTogliePending` (file inatteso a metà, ctx finito a metà,
  scrittura che fallisce), `TestCifraNonSovrascriveUnArchivio` (anche comparso durante la scrittura),
  `TestCifraSpazioDestinazione` (al byte; spazio illeggibile), `TestCifraSenzaChiaveBuona`, `TestCifraDestinazione`
  (vuota, assente, file, cartella dei dati, `media/`, `secrets/`, collegamento ai dati), `TestNomeArchivio`,
  `TestArchivioContestoDentroLaCopia`.
- `internal/media/lucchetto_linux_test.go`: `TestCancellazioniAspettanoUnAltroProcesso` (secondo `Archivio` sulla
  stessa cartella = altro processo; file 0600 fuori da `media/`), `TestDueBackupInsieme`,
  `TestAttesaDellaPuliziaSiInterrompe` (stesso e altro processo, anche `CancellaInCodaConLucchetto`),
  `TestAttesaDelBackupSiInterrompe`.
- `cmd/gestrip/backup_linux_test.go`: rifiuto con stdout su `/dev/null` senza creare la chiave (processo vero),
  chiave stampata una volta e mai più, chiave tolta se non si riesce a mostrarla, opzioni, comando `backup` completo
  (saltato se la cartella temporanea ha meno di 11 GiB liberi: usa lo statfs vero).
- Test prima del codice per la cifratura (il pacchetto di prova non compilava); il lucchetto fra processi l'ho
  scritto prima del suo test: i test sono stati visti rossi con le mutazioni M7, M8b.
- Mutazioni a mano (tutte compilano), 13: **uccise 12** — M1 permessi della chiave controllati solo per «altri»;
  M2 chiave pubblicata con `os.Rename`; M3 archivio pubblicato con `os.Rename`; M4 `.pending` non tolto; M5 spazio
  con un byte di tolleranza; M6 controllo «chiave esistente» prima di generare tolto; M7 niente flock; M8b attesa
  senza alcun controllo del ctx (M8 più il controllo all'inizio del giro tolto) (test appeso fino al timeout); M9 scrittura del tar che non guarda il ctx; M10
  destinazione dentro i dati ammessa; M11 chiave non mostrata che resta; M12 controllo del terminale tolto.
  **Sopravvissuta 1:** M8, attesa fra due tentativi che non guarda il ctx (`clock.Aspetta` su `context.Background()`): il controllo all'inizio del giro ferma
  comunque l'attesa entro 50 ms (equivalente a meno della latenza).
- `scripts/check`: vedi resoconto del coordinatore (implementatore: sotto).

## Per il manuale

- La prima volta si crea la **chiave dei backup** con `gestrip chiave-backup`: il programma la mostra **una volta
  sola** sullo schermo. Va scritta subito sul foglio di carta: senza, i backup non si aprono più.
- Il comando funziona solo davanti a un terminale; se la chiave c'è già rifiuta e non la mostra di nuovo.
- Un backup a mano si fa con `gestrip backup -cartella /percorso/della/chiavetta`, anche con il programma acceso.
- L'archivio si chiama con data e ora (in UTC), per esempio `gestrip-backup-20261125T023000Z.tar.age`, ed è
  cifrato: chi trova la chiavetta senza la chiave non legge niente.
- Se nella cartella non c'è spazio per l'archivio, il backup non parte. Un backup interrotto non lascia file a
  metà. Il comando non cancella mai archivi vecchi.
- Dopo il backup il comando dice «non ancora verificato»: la verifica arriva con il prossimo passo.
- I due comandi si lanciano con lo stesso utente del programma (gestrip), mai come root: altrimenti la pulizia
  delle foto si blocca.

## Differenze dallo standalone

- Cifratura con la libreria `age` dentro il binario invece del comando `age` (PIANO §2.5); stesso formato.
- Nome con data e ora invece di un uuid; rinomina che non sovrascrive mai (lo standalone usa `os.replace`).
- La chiave la crea il programma (`chiave-backup`), lo standalone la riceve già fatta (`--identity-file`).
- Archivio scritto con l'app accesa (D-10): serve il lucchetto fra processi verso la pulizia dei media.

## Dubbi

1. **Margine della destinazione 64 MiB** sopra una stima per eccesso: numero mio. Lo standalone vuole 10 GiB + 3×
   il tar, ma sul disco dei dati, dove decifra per verificare. Da rivedere con la 123 (chiavetta da 128 GB).
2. **Nome in UTC** (`...Z`): ordinamento stabile anche all'ora legale; Stefano leggerà un'ora in meno d'inverno.
3. **Nessun controllo del volume cifrato** in `chiave-backup` e `backup` (come gli altri comandi da console; lo
   fa `serve`). La chiave sta in `secrets/` dentro i dati: se i dati sono nel volume LUKS lo è anche lei.
4. **Chiave dell'utente sbagliato:** il controllo del proprietario c'è ma non ha un test (servirebbe root).
5. **Il comando `backup` legge la chiave privata** per ricavarne la pubblica (regola della 119: ammesso). Una
   chiave pubblica salvata a parte eviterebbe di leggerla; non serve finché la 121 la usa comunque per verificare.
6. **Segno di copertura:** come nella 119, niente `// inventario: OPS-16` (chiuderebbe la voce); lo mette la slice
   che la chiude (121 o 130), d'accordo con il coordinatore.
7. **File del lucchetto** `.lucchetto-cancellazioni-media` resta nella cartella dei dati per sempre (vuoto). Non
   entra nel backup; il ripristino (130) non deve portarlo.
8. LSP (gopls): usato per i riferimenti di `CancellaInCoda` e `SenzaCancellazioni` (tutti i chiamanti da
   aggiornare); il resto con grep e letture mirate.

## Difetti trovati per strada (solo in lista)

- Nessuno nuovo. Note 5a e 5b di `docs/domande-aperte.md` chiuse qui; resta la 5c (guardia `AGE-SECRET-KEY-` senza
  test in minuscolo).

## Revisione (Opus, ~95k token, ~15 minuti)

Nessun difetto ALTA né MEDIA. Mutazioni del revisore: 11 provate. **Uccise 5:** C (più identità ammesse), K
(SHA-256 senza i byte cifrati), G (niente controllo del ctx all'inizio dell'attesa), B (5b, lucchetto tolto da
`CancellaInCodaConLucchetto`), S (destinazione uguale alla cartella dei dati). **Sopravvissute 5, ora uccise** dai test
aggiunti dopo la revisione (rifatte sul codice di oggi, `to-be-deleted-after-use/slice-120/muta2.log`):
E (media tolti dalla stima) e F (aggiunta di age e byte fissi tolti) → `TestStimaDellaDestinazione`; L (`O_EXCL` →
`O_TRUNC` sul `.pending`) → `TestCifraNonToccaUnPendingEsistente`; N (niente `EvalSymlinks` sulla cartella dei
dati) → `TestCifraDatiDietroUnCollegamento`; H (limite `maxChiave` tolto) → `TestChiaveTroppoGrande`. **Quasi
equivalente:** U (contenuto della chiave nel messaggio d'errore del parse: age non cita la riga nei casi provati).

Corretti dopo la revisione:
- Il registro della pulizia diceva «causa=database» per un lucchetto che non si apre (per esempio creato da root):
  ora `media.ErrLucchetto` e «causa=lucchetto» (`TestLucchettoCheNonSiApre`). Regola «stesso utente di `serve`, mai
  root» in SVILUPPO e in «Per il manuale».
- 6d: temporaneo `.chiave_backup-*` rimasto → messaggio che dice di toglierlo (`TestTemporaneoDellaChiaveRimasto`).
- 6e: fsync della cartella dopo aver tolto la chiave non mostrata (non osservabile in un test; la rimozione sì).

In lista (`docs/domande-aperte.md`, nota 6): (a) ordine dei lucchetti invertito fra `cancellaInCoda` e
`CancellaInCodaConLucchetto`, latente (T4.11); (b) due backup insieme passano senza sommare lo spazio (124);
(c) `RENAME_NOREPLACE` con EINVAL su CIFS, 9p e FUSE senza rename2 (123).
