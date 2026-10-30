# 008a — Tabella utenti e chiave del server (T1)

- **Fonte:** richiesta di Stefano del 30/10/2026 (slice 008 divisa in tre: 008a migrazione
  utenti e chiave del server persistente tra i riavvii); standalone `go-baseline`: migrazioni
  `20260905070000`, `20260907000000`, `20260915090000`, `20261002120000`, `accounts/user.ex`;
  ricevute 006b e 007 (a cosa serve la chiave).
- **Confine:** la tabella `utenti` con le regole di `users` dello standalone; la chiave del server
  creata alla prima partenza, stabile tra i riavvii, con sottochiavi per scopo. Login, sessioni e
  ruoli sono la 008b e la 008c; inviti e reset password le loro slice.
- **Voci:** nessuna si chiude qui (ACC-01…, ACC-05 con le slice successive).
- **Livello:** completo (schema e segreti).

## Cosa c'è

- `0002_utenti.sql`, regole di `users` con i nomi del Go (l'import traduce `owner` e `staff` in
  `titolare` e `dipendente`):
  - `AUTOINCREMENT`: un id non si riusa mai, perché l'audit nomina gli utenti per numero;
  - `hash_password` nullo finché l'invito non è accettato;
  - email unica solo tra gli account attivi (indice parziale, come `app_users_bp.py:41`);
  - `ruolo` titolare o dipendente, dipendente di default; `attivo` 0 o 1, 1 di default;
    `versione_sessione` da 0, mai negativa;
  - `nome_visibile`, `ultimo_accesso_il`, `disattivato_il`, `creato_il`, `aggiornato_il`;
    `STRICT`.

  Normalizzazione e forma dell'email e lunghezze restano nel codice, come nello standalone
  (changeset): arrivano con le slice che scrivono utenti.
- Più rigido dello standalone, dal lato sicuro (dopo la review):
  - email non vuota, già in minuscolo e senza spazi ai lati;
  - `hash_password` mai vuoto;
  - istanti nella forma fissa `db.FormatoIstante` (CHECK con GLOB), così l'ordine del testo è
    l'ordine del tempo.
- Tabella `segreti`, separata da `impostazioni` ed elencata in `db.TabelleSegrete`, così export,
  copie e pagine d'elenco la escludono per nome. Il valore è un BLOB di 32 byte, mai tutto a zero.
- `db.ChiaveServer`:
  - 32 byte casuali creati alla prima partenza dentro una transazione di scrittura immediata
    (otto connessioni insieme ottengono la stessa chiave, test);
  - stabile tra i riavvii; viaggia con il backup, che quindi **è una credenziale**;
  - una chiave rovinata è un errore e la riga resta com'è: rigenerarla in silenzio invaliderebbe
    sessioni e moduli.
- `db.RinnovaChiaveServer`: cambia la chiave, per un ripristino isolato, una macchina di prova o
  una chiave compromessa. La useranno la procedura di ripristino (OPS-19) e la console.
- `db.SottoChiave(chiave, scopo)`: HMAC-SHA256 per scopo. Alle facce vanno **solo** le
  sottochiavi `csrf` e `sessione`, mai la chiave del server.
- `gestrip serve` crea o legge la chiave dopo le migrazioni; se è rovinata si ferma prima di
  aprire le facce.

## Prove

- TDD: test della tabella (default, rifiuti, email unica tra gli attivi, id mai riusati), della
  chiave (persistente tra due aperture, diversa tra due database, rovinata → errore, sottochiavi)
  e dell'avvio (chiave creata, uguale al riavvio, rovinata → nessuna faccia aperta), rossi prima
  del codice.
- Mutazioni: 30 sul codice finale, sulla migrazione SQL e sul Go (rifatte dopo la review). Tutte uccise, tranne il
  controllo di `ErrNoRows` in `ChiaveServer`, che è equivalente: con qualunque altro errore
  fallisce anche l'inserimento che segue.
  - Due mutazioni risultavano «uccise» perché non compilavano. Ora la prova delle mutazioni
    controlla prima che il codice compili. Ricontrollate così le mutazioni sui `rand.Read`
    delle slice 006b, 006c e 007: una sola della 007 era un falso «uccisa» (non compilava), e
    nella forma corretta (sale vuoto) viene uccisa davvero.
  - La mutazione che toglieva il rollback è sopravvissuta ed era un difetto vero dei test: dopo
    una chiave rovinata l'unica connessione di scrittura restava bloccata. Aggiunto un test.
- `scripts/check` verde.

## Review indipendente

Nessun problema alto: le regole di `users` sono riportate tutte. Il revisore ha misurato che più
avvii insieme ottengono la stessa chiave. Corretti:

- **MEDIA:**
  - la chiave sarebbe finita in un export di `impostazioni`: ora è nella tabella `segreti`,
    esclusa per nome (`TabelleSegrete`, con un test che ci obbliga a elencare ogni tabella di
    segreti);
  - una copia del database porta con sé la chiave: scritto che il backup è una credenziale,
    aggiunto `RinnovaChiaveServer` per ripristini isolati e macchine di prova (PIANO §1.4).
- **BASSA:**
  - istanti, email e hash vuoti controllati dalla tabella;
  - chiave come BLOB (niente base64 tollerante) e mai tutta a zero;
  - solo sottochiavi alle facce;
  - test della riga rovinata che resta com'è, della concorrenza, dell'indice usato con
    `attivo = 1`;
  - tolto un file vuoto `internal/db/altro.db` lasciato da una mutazione della 005.

## Per le slice successive

- **Import:** `sqlite_sequence` a max(seq dello standalone, max id), come `rebuild_users`;
  istanti tradotti nella forma fissa; email normalizzate (attenzione alle collisioni tra
  maiuscole e minuscole tra account attivi).
- **008b:** cercare gli account con `attivo = 1`, non `AND attivo` (l'indice non si usa).
- **Debito (dalla 005):** due primi avvii insieme su un file nuovo: il secondo si ferma con
  «table … already exists» invece di aspettare. Senza danni, ma il messaggio inganna. Con
  systemd parte un processo solo.
