# 129b — Prova vera della copia remota su una Storage Box (T5.2, slice corta)

- **Fonte:** ricevuta 129, sezione «Prova vera: slice corta 129b»; nota 13(b) di `docs/domande-aperte.md`; via di
  Stefano del 27/11/2026 («fai la 133 e dopo la 129b»), sottoaccount **di prova** creato da Stefano (separato da quello
  dei backup dello standalone), chiave dell'apparecchio installata da lui, cartella `backup` creata da lui.
- **Voci:** nessuna voce cambiata, nessun codice cambiato: è una misura. Nessuna D nuova.
- **Modelli:** esecutore Opus (~111k token, ~25 minuti di orologio, di cui ~10 di caricamento). Nessuna revisione di
  codice (non c'è codice); impronte pubblicate da Hetzner lette da un sottoagente web.
- **Dove:** pc-fisso, Go di prova in `to-be-deleted-after-use/_slice-129b/` (database con dati inventati, titolare
  inventato, `serve` solo su 127.0.0.1 con `-senza-lavori-automatici -senza-volume-cifrato`). Chiavetta finta: file
  ext4 da 4 GB montato senza sudo con `udisksctl` (una cartella semplice non passa il controllo del punto di
  montaggio). Archivi inventati: file di byte casuali in `media/`, nominati col proprio SHA-256. Nessuna password.

## Esito passo per passo (procedura della ricevuta 129)

1. Chiave dell'apparecchio: già creata con `gestrip chiave-sftp`, la pagina mostra la stessa riga pubblica.
2. **Porta 23:** il server è OpenSSH 9.6; la pagina mostra l'impronta ED25519 `SHA256:XqONwb1S0zuj5A1CDxpOSuD2hnAArV1A3wKY7Z3sdgM`,
   **uguale a quella pubblicata da Hetzner** (docs.hetzner.com/storage/storage-box/general, «SSH host keys», anche
   RSA ed ECDSA uguali). Confermata e salvata in 0,78 s. `RealPath` della cartella: **`/home/backup`**.
3. **Porta 22:** risponde, ma è ProFTPD (`SSH-2.0-mod_sftp`) con **solo la chiave RSA** (`SHA256:EMlfI8Gs…2ioI`,
   uguale alla RSA di Hetzner). Salvata in 1,19 s. `RealPath`: **`/backup`** (diverso dalla 23).
4. «Prova collegamento» riuscita. Errori voluti, ciascuno senza salvare niente:
   - cartella sbagliata: «La cartella non c'è sul server (o non è una cartella): creala nel sottoaccount dal
     pannello, oppure correggi il nome.»;
   - impronta scritta male: «L'impronta va scritta come la pubblica il fornitore: «SHA256:» seguito da 43 caratteri,
     senza spazi. Rifai il primo passo…»;
   - impronta ben scritta ma diversa: «IMPRONTA DIVERSA: il server si presenta con l'impronta SHA256:…, non con quella
     che hai scritto. Niente è stato salvato…»;
   - **chiave tolta dal pannello: non fatta** (serve Stefano nel pannello).
5. Tre «Esegui adesso» riusciti: riga `sftp` `ok`, byte uguali, nessun `.pending` rimasto; nella pagina la copia è in
   «Backup verificati» come «destinazione: copia remota». Download della prova mensile con `gestrip prova-backup
   -remota`: «scaricata, SHA-256 uguale al registro; verificata: 102 file, 51 tabelle», riga `ok` in `prove_remote`.

## Misure (rete di pc-fisso; quella del negozio va rimisurata nella T5.4)

| Archivio | Porta | Byte | Secondi | Mbit/s | Proiezione 3,6 GB |
|---|---|---|---|---|---|
| ~100 MB in salita | 22 | 105.534.808 | 62,6 | 13,5 | ~36 min |
| ~100 MB in salita | 23 | 105.543.016 | 85,6 | 9,9 | ~49 min |
| ~1 GB in salita | 23 | 1.049.550.184 | 611,5 | 13,7 | ~35 min |
| ~1 GB in discesa (prova mensile) | 23 | 1.049.550.184 | ~124 (127 con la verifica) | ~68 | ~7 min |

- Tetto di 6 ore (`TempoMassimoCopiaRemota`): rispettato con ampio margine.
- «Prova collegamento»: 0,49–0,82 s (molto sotto i 60 s di `TempoMassimoProvaRemota`); lettura dell'impronta 0,12–0,22 s.
- `statvfs@openssh.com`: presente su tutte e due le porte; dà circa 1 TiB totale e 0,75 TiB libero, cioè sembra
  l'intera Storage Box e non una quota del sottoaccount (da confrontare col pannello).
- `RealPath`: assoluto e non la radice su tutte e due, ma diverso secondo la porta (`/home/backup` contro `/backup`).

## Porta consigliata: la 23

OpenSSH con la chiave ED25519 che Hetzner pubblica per prima; la 22 è ProFTPD con sola RSA. La banda è la stessa (le
differenze sono la variazione fra giri). Decisione e difetto della porta: **voce 19** di `docs/domande-aperte.md`.

## Difetto trovato (non corretto, voce 19)

Cambiare porta lascia orfani gli archivi già caricati: la cartella salvata cambia con la porta e rotazione e prova
mensile confrontano il percorso (`internal/db/copie_remote.go` `ArchiviRemotiNellaCartella`,
`internal/backup/remoto.go`, `internal/backup/prova_remota.go`). Visto davvero: l'archivio caricato dalla porta 22 è
rimasto, quello della 23 è stato tolto dalla rotazione. Fallisce in modo sicuro (non cancella niente di sbagliato), ma
lo spazio si perde.

## Note minori

- Sulla porta 23 il Go accetta solo l'impronta che ha mostrato (la ED25519): copiando la RSA, pubblicata anche lei da
  Hetzner, si riceve «IMPRONTA DIVERSA». La guida (T5.4) e il manuale devono dire di confrontare la ED25519.
- Il registro di `serve` scrive «copia remota non configurata» anche per una conferma fallita (solo una riga INFO).

## Cosa resta

- Nel sottoaccount di prova: due archivi inventati (~100 MB dalla porta 22, l'orfano; ~1 GB dalla 23) e la chiave
  pubblica dell'apparecchio di prova. **Stefano:** la prova «chiave tolta dal pannello» (e rimessa) è da fare se la
  vuole; poi il sottoaccount di prova si può togliere, con la chiave e i due archivi.
- La copia remota del Go di prova è spenta dalla pagina; il Go di prova e la chiavetta finta si cancellano col commit.

## Per il manuale

- La porta da usare per la Storage Box è la 23; l'impronta da confrontare è la ED25519 pubblicata da Hetzner.
- Con la rete di prova 3,6 GB salgono in circa 35-50 minuti e scendono in circa 7.

## LSP

Non usato: nessun codice letto per modificarlo; i punti del difetto trovati con grep.
