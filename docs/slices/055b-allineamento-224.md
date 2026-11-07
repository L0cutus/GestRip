# 055b — go-baseline a `d3778df` e banco su `fab-224` (allineamento alle slice 223 e 224 dello standalone)

- **Fonte:** coordinatore, 07/11/2026. Stefano ha autorizzato lo spostamento del tag `go-baseline`
  dello standalone da `f80f191` (slice 222) a `d3778df` (slice 224) e il passaggio del banco
  dall'immagine `gestrip-standalone:fab-222` a `gestrip-standalone:fab-224`
  (`sha256:23333658b187…`, creata il 05/11/2026 alle 08:46). Il tag l'ha spostato la sessione
  principale; controllato: `go-baseline^{commit}` = `d3778df`. `git log f80f191..d3778df` ha solo:
  - **slice 223** (`ae37a8c`, `5302abc`): cookie di sessione di 30 giorni assoluti. Il Go l'ha già
    allineata nella slice 048 (`048-cookie-30-giorni.md`);
  - **slice 224** (`cb442c5`, `d3778df`): «a page of previews no longer answers 500 database is
    locked».
- **Letti:** `git show cb442c5` (`repair_photos.ex`, `delivery_signature.ex`,
  `repair_photo_concurrency_test.exs`) e la ricevuta
  `docs/plans/2026-10-27-gestrip-debiti-parita/slices/224-miniature-locked.md`; nel Go
  `internal/buste/letture.go`, `internal/buste/firma.go`, `internal/web/foto.go`,
  `internal/audit/audit.go`, `internal/db/db.go`.
- **Voci:** RIP-S-33 (foto del personale), la lettura della PNG della firma (slice 047). Nessuna
  voce cambia stato: è un **allineamento senza codice**.
- **Lavoro:** studio, documenti e banco di un sottoagente. Commit, inventario privato e registro di
  allineamento li fa la sessione principale.

## La 224 vista da fuori

- **Prima (fab-222):** ogni lettura di una foto di una busta (miniature e foto intera del negozio,
  link pubblici, lettura per nome del file), la riga della galleria e la PNG della firma scrivevano
  la loro riga d'audit dentro `BEGIN IMMEDIATE` + INSERT + COMMIT. Quando una pagina chiede quattro
  miniature nello stesso millisecondo, sul pool vero (5 connessioni, busy timeout 5 s, SQLCipher)
  qualcuna aspettava 5 s e rispondeva **500 «database is locked»**: è il 500 intermittente di
  `firma-riquadro` nel livello browser del banco dalla slice 051 (`/repairs/6/photos/28` e `/29`).
- **Dopo (fab-224):** la riga è **una sola INSERT in autocommit**, senza transazione. Per chi guarda
  da fuori cambia solo che il 500 non esce più:
  - le righe d'audit sono le stesse: `repair.staff_photo_viewed` (`photo_id`, e `variant` solo per
    una miniatura chiesta), le letture pubbliche, `repair.staff_gallery_viewed` (`photo_ids`),
    `repair.signature_viewed` (`audience`); stessi esiti `ok`/`denied`, una riga per lettura;
  - una riga rifiutata o un errore SQL danno ancora un errore e nessun byte;
  - risposte, intestazioni e corpi invariati; i tempi cambiano solo per le richieste che prima
    aspettavano i 5 s del busy timeout.

## Il Go

Fa già così, dalla slice 037 (foto) e 047 (firma):
- `buste.LeggiFotoPersonale` scrive la riga con `audit.Registro.Registra`, cioè una sola
  `INSERT INTO eventi_audit` in autocommit su `d.Scrittura`, senza transazione, e solo dopo dà i
  byte; un errore della riga è un 500 senza byte. Lo stesso per `LeggiFirma`
  (`repair.signature_viewed`, `audience` «staff»).
- La scrittura ha **una sola connessione** (`SetMaxOpenConns(1)`): le letture in parallelo aspettano
  la connessione in Go, una dopo l'altra, invece di contendersi il lock di SQLite. Il Go non dava
  il 500 neanche su fab-222 (ricevute 052–055a: «il Go nessun errore»).
- Galleria della home e link pubblici alle foto il Go non li ha ancora (T4.2 e seguenti): quando
  arriveranno, la regola da seguire è la stessa, una INSERT in autocommit.

Nessun codice dell'applicazione toccato.

## Cosa cambia

- **Banco:** `gestrip-standalone:fab-224` al posto di `fab-222` nell'uso di `tools/banco/main.go` e
  nell'esempio del flag `-immagine` (`tools/banco/istanza.go`).
- **Documenti:** `docs/PIANO.md` in testa (`go-baseline` = `d3778df`, banco su `fab-224`, con la
  catena di prima); `docs/SVILUPPO.md` (esempio delle schermate del manuale); `docs/prova-dati-veri.md`
  (immagine da controllare e da usare per la decifratura); `docs/slices/048-cookie-30-giorni.md` una
  riga datata sullo spostamento del tag. Ricevute e riprese vecchie lasciate come sono.
- **Pillow:** uguale nelle due immagini (`python3-pil` 9.4.0-1.1+deb12u1, `libwebp7`
  1.2.4-0.2+deb12u1) e script `priv/*.py` con gli stessi md5 (`normalize_image.py`,
  `thumbnail_image.py`…). Le misure fatte con Pillow di fab-222 (`docs/studio-heic.md`,
  `internal/imaging/normalizza_test.go`, `formati.go`) valgono anche per fab-224: lasciate così.
- **`bench/deviazioni.json`:** nessuna riga esisteva solo per fab-222 (il 500 di `firma-riquadro`
  non era mai stato accettato): niente da togliere.

## Banco

Istanze nuove, `gestrip-standalone:fab-224`, binari compilati dal codice di adesso.

- **Giro 1, dati importati** (`s055b`): HTTP 47 scenari, 860 passi, 1105 attese, **0 inattese**, 0
  errori; browser 9 scenari, 18 viste, 26 attese, **0 inattese**, 0 da decidere, 0 errori. Verde.
- **Giro 2, dati importati** (`s055b2`): HTTP 47 / 860 / 1105 attese, **0 inattese**, 0 errori;
  browser 9 / 18 / 26 attese, **0 inattese**, 0 errori. Verde.
- **Giro 3, dati importati** (`s055b3`): HTTP 47 / 860 / 1105 attese, **0 inattese**, 0 errori;
  browser 9 / 18 / 26 attese, **0 inattese**, 0 errori. Verde.
- **Database vuoto** (`s055bv`): HTTP 47 scenari, 860 passi, 33 attese, **0 inattese**, 0 errori
  (29 non provati con questi dati); browser 9 scenari, 0 attese, **0 inattese**, 0 errori (9 non
  provati). Verde.
- In nessun giro `firma-riquadro` ha differenze, e nei log non c'è nessun «database is locked»: il
  500 dello standalone delle ricevute 051–055a non è uscito. Nessuna differenza nuova dovuta alla
  223 (il cookie di sessione il banco non lo confronta: i nomi sono diversi nelle due app) o alla
  224.
- `banco giu` dopo ogni giro («TOLTA grgo-s055b…», «AVANZI: nessuno») e `banco controlla` alla
  fine: «AVANZI: nessuno».

## Prove

- `scripts/check` verde (gofmt, vet, staticcheck, errcheck, build, test, copertura: 353 voci, 26
  chiuse, 0 rosse, 81 parziali, 235 aperte, 11 fuori v1; segreti e dati veri OK).

## Per il manuale

Niente da cambiare: per chi usa il Go non cambia nulla, né nelle pagine né nelle schermate.

## Dubbi

1. **`docs/prova-dati-veri.md` dice ora `fab-224`**, ma la produzione su otto-vps (dalla ricevuta
   224: «non deployato» al 05/11) potrebbe essere ancora `fab-222`, e il backup registra
   l'immagine che l'ha fatto. Fra le due non c'è nessuna migrazione: la regola già scritta («se è
   un'altra e su pc-fisso c'è, si usa quella») basta. Non l'ho verificato su otto-vps.
2. **Il 500 era intermittente** già su fab-222 (alcuni giri verdi, per esempio quello del 05/11):
   tre giri verdi su fab-224 lo rendono molto improbabile, non lo escludono. La prova deterministica
   è il test di concorrenza della 224 nello standalone.

## Review

La review l'ha fatta un sottoagente diverso. Non ha trovato problemi ALTI né MEDI e la tesi regge: per la 224 non serve codice. `scripts/check` è verde. Il banco non è stato rilanciato, perché i 4 giri su fab-224 erano già verdi. Le note BASSE:
- **Nessuna migrazione** fra `f80f191` e `d3778df`, e nemmeno fra 219, 220, 222 e 224: le istantanee `standalone-go-baseline*` non vanno rigenerate.
- **Esiti diversi quando la scrittura dell'audit fallisce** (difetto già presente prima, la 224 non l'ha cambiato). Lo standalone dà 404 se la riga viene rifiutata e 500 per un errore SQL. Il Go dà 500 per la foto e 404 per la firma con qualunque errore. In nessun caso escono byte e ci si arriva solo con un guasto del database. Il commento di `internal/web/firma.go` («lo standalone risponde lo stesso 404») vale solo per la riga rifiutata. Va in lista.
- **Proposta per `docs/prova-dati-veri.md`:** dire in modo esplicito «si usa l'immagine del backup solo se fra il suo commit e `go-baseline` non ci sono migrazioni, altrimenti ci si ferma». Oggi la regola regge perché di migrazioni non ce ne sono. È una decisione di procedura, quindi la lascio al coordinatore.
- Due righe di `docs/PIANO.md` e `docs/prova-dati-veri.md` erano più lunghe di 100 caratteri: riandate a capo, testo identico.
