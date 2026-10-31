# Piano: prova dell'import su una copia dei dati veri

Stato: **proposta, aspetta l'ok di Stefano** (decisione del 31/10/2026: la prova si fa prima della T4,
in due passi; questo è il primo, il piano). Niente parte senza il suo via.

Scopo: far girare `gestrip import` su una copia vera del database e dei media dello standalone per
vedere i casi che i dati sintetici non hanno. Il risultato è un report; il database Go che ne esce
si butta.

## Regole della prova

- La sorgente è **l'ultimo backup verificato** dello standalone, mai il database vivo. Della
  produzione si legge solo quel file, la sua prova (il `.json` accanto) e la riga d'esito del
  backup notturno, che serve a sceglierlo. Non si legge e non si cambia nient'altro.
- Tutto ciò che è in chiaro sta **in RAM**, in una cartella privata di `/dev/shm`, un tmpfs che
  c'è già e non chiede sudo. Lo swap di pc-fisso è solo zram, cioè anche lui in RAM: niente va
  sul disco, che non è cifrato. Sul disco va solo l'archivio come arriva, **cifrato con age**.
- Le due chiavi le scrive Stefano nel suo terminale, con `read -s`: non compaiono a video, non
  finiscono nella history e io non le leggo da nessuna parte. Restano in RAM solo per il tempo
  del comando che le usa.
  - L'identità age (`AGE-SECRET-KEY-1…`) passa ad `age` dallo standard input e non si scrive mai
    in un file.
  - La chiave SQLCipher (64 caratteri esadecimali) sta in un file nel tmpfs per la durata del
    container che decifra; poi si cancella.
- Io non apro mai il database: leggo solo il report dell'import, che per costruzione non ha
  valori dei dati (nomi, telefoni, testi, email, token). Ha solo tabelle, colonne, id, conteggi
  e codici. Se per capire un caso servisse guardare un valore, mi fermo e chiedo.
- Niente rete per chi tocca i dati in chiaro: il container che decifra gira con
  `--network none`, `gestrip import` non ha codice di rete. Niente log dei container
  (`--log-driver none`) e niente core dump (`ulimit -c 0`, `--ulimit core=0`).
- Durante la prova pc-fisso non va sospeso né ibernato, perché l'ibernazione scriverebbe la RAM
  su disco.
- Qualunque passo fallisca, e anche un Ctrl-C: si va diretti allo **smontaggio** (passo 7).

## Che cosa serve e quanto

Misurato su pc-fisso il 31/10/2026 alle 16:20:

| | |
| --- | --- |
| RAM disponibile (`MemAvailable`) | 14,6 GB su 33,4 (1,2 libera, il resto è cache che si libera) |
| swap | 18,7 GB, solo `/dev/zram0` (in RAM), 6,2 GB usati |
| `/dev/shm` | tmpfs, limite 16,7 GB, vuoto |
| disco della home | 563 GB liberi |

Dimensioni attese (dal cutover del 25/10: archivio 3,76 GB, database 6,4 MB, media 3,5 GB;
oggi un po' di più):

- Sul disco: l'archivio cifrato, circa 4 GB.
- In RAM, al massimo: il contenuto dell'archivio (circa 4 GB) più la copia che fa l'import
  (database Go e `media/`, circa 4 GB), cioè **circa 8 GB**.

**Soglie**: si parte solo con `MemAvailable` ≥ 2 × la dimensione dell'archivio + 4 GiB (circa
12 GiB). Prima dell'import si rimisura: almeno la dimensione dei media + 3 GiB. Sotto soglia ci si
ferma e si smonta. Il controllo dello spazio dell'import non basta da solo, perché in un tmpfs
misura il limite di `/dev/shm`, non la RAM vera.

Durata stimata: 10-20 minuti, quasi tutti per la copia dell'archivio dalla produzione. La copia è
limitata a 50 MB/s per non caricare la produzione.

## I passi

Chi fa cosa:
- io faccio i passi 0, 1, 2, 3b, 5, 6 e 7;
- Stefano fa i passi 3a e 4 nel suo terminale, perché sono quelli in cui scrive le chiavi.

I valori dell'host e della cartella della produzione stanno fuori dal repo, in
`~/.config/gestrip-go/prova-dati-veri.env` (0600, nessun segreto).

### 0. Preparazione, senza dati (io)

Prima, una **prova generale a secco** con il banco: lo stesso container di decifratura e lo stesso
import, sul database sintetico cifrato di un'istanza del banco, per non scoprire un errore di
comando a metà della prova vera.

Poi:

```sh
umask 077
R=~/sviluppo/gestrip-go
A=$R/to-be-deleted-after-use/prova-dati-veri    # solo l'archivio cifrato, script e binario
L=/dev/shm/gestrip-prova                        # tutto ciò che è in chiaro
mkdir -p "$A" && install -d -m 700 "$L" "$L/estratto" "$L/chiaro" "$L/segreti" "$L/go" "$L/esito"
touch "$A/inizio"
git -C "$R" status --short          # vuoto
git -C "$R" rev-parse HEAD          # il commit va nel riassunto
PATH=$HOME/sdk/go1.27.3/bin:$PATH CGO_ENABLED=0 go build -C "$R" -trimpath -o "$A/gestrip" ./cmd/gestrip
git -C ~/sviluppo/gestrip-standalone show go-baseline:deploy/chromium-seccomp.json > "$A/chromium-seccomp.json"
(cd ~/sviluppo/gestrip-cutover/package-2026.10.28-4 && sha256sum -c --ignore-missing SHA256SUMS | grep tools/age)
sg docker -c "docker image inspect --format '{{.Id}}' gestrip-standalone:fab-219"
grep MemAvailable /proc/meminfo; swapon --show; df -B1 /dev/shm "$A"
```

`age` è la 1.3.2 del pacchetto dello standalone: `tools/age: OK` contro gli SHA-256 del
pacchetto, che a loro volta fissano l'archivio ufficiale di age.

Scrivo anche, tutti in `$A` e senza segreti:
- `var.sh`, con le variabili qui sopra, `umask 077` e il nome dell'archivio;
- `decifra.exs`, lo stesso script Elixir del banco (`decifraStandalone` in `tools/banco/catena.go`);
- `decifra.sh`, il `docker run` del passo 4.

### 1. Scelta del backup (io, sola lettura sulla produzione)

```sh
. ~/.config/gestrip-go/prova-dati-veri.env
ssh "$PROD_HOST" "cat $PROD_STATO; ls -l --time-style=+%FT%T $PROD_RADICE/backups/"
ssh "$PROD_HOST" "cat $PROD_RADICE/backups/<id>.tar.age.json"
```

Criterio: l'esito del notturno è `OK` con la data dell'ultima notte, e si prende l'archivio con
la prova `.tar.age.json` più recente per `captured_at`, che abbia anche `verified_at`. Nella
cartella restano anche gli archivi di ritorno degli aggiornamenti: non si usano. Se l'esito non è
`OK`, o la prova manca, mi fermo e lo dico.

La firma HMAC della prova non la ricontrollo, perché serve una chiave della produzione. Controllo
invece lo SHA-256: quello dell'archivio copiato deve essere uguale a quello scritto nella prova,
che il notturno ha già verificato decifrando l'archivio.

### 2. Copia su pc-fisso (io)

```sh
scp -p -l 400000 "$PROD_HOST:$PROD_RADICE/backups/<id>.tar.age" "$PROD_HOST:$PROD_RADICE/backups/<id>.tar.age.json" "$A/"
sha256sum "$A/<id>.tar.age"         # uguale allo sha256 della prova, se no stop
grep MemAvailable /proc/meminfo     # soglia: 2 × archivio + 4 GiB
```

### 3a. Decifratura dell'archivio (Stefano, nel suo terminale)

```sh
cd ~/sviluppo/gestrip-go && . to-be-deleted-after-use/prova-dati-veri/var.sh
read -rsp 'Identità age: ' K; echo
printf '%s\n' "$K" | "$AGE" -d -i - "$A/$ID.tar.age" | tar -x -C "$L/estratto" --no-same-owner --no-same-permissions; echo "rc=${PIPESTATUS[*]}"
unset K
```

Deve stampare `rc=0 0 0`. Con un errore di battitura `age` rifiuta l'identità: si svuota
`$L/estratto` (`rm -rf "$L/estratto"/*`) e si riprova.

### 3b. Controllo dell'archivio (io)

Gli SHA-256 di tutti i file estratti si confrontano con il suo `inventory.json`, come fa
`restore.sh` dello standalone. Lo script stampa solo il numero di file, «uguale» o «DIVERSO» e
l'immagine registrata nel backup:

```sh
python3 -I - "$L/estratto" <<'EOF'
import hashlib, json, os, sys
r = sys.argv[1]
inv = json.load(open(os.path.join(r, "inventory.json")))
fatti = {}
for d, _, fs in os.walk(r):
    for f in fs:
        p = os.path.join(d, f)
        n = os.path.relpath(p, r)
        if n not in ("inventory.json", "snapshot.json"):
            with open(p, "rb") as h:
                fatti[n] = hashlib.file_digest(h, "sha256").hexdigest()
print(len(inv), "file nell'inventario:", "uguale" if fatti == inv else "DIVERSO")
print("immagine del backup:", json.load(open(os.path.join(r, "snapshot.json")))["image"])
EOF
du -sb "$L/estratto/data" "$L/estratto/media"
rm "$A/$ID.tar.age"                 # non serve più; resta solo sullo standalone
```

Le condizioni per andare avanti:
- l'inventario dev'essere uguale;
- l'immagine del backup dev'essere quella di `gestrip-standalone:fab-219`. Se è un'altra e su
  pc-fisso c'è, si usa quella; se non c'è, mi fermo e chiedo.

### 4. Decifratura del database (Stefano, nel suo terminale)

```sh
read -rsp 'Chiave SQLCipher: ' K; echo
printf '%s\n' "$K" > "$L/segreti/sqlcipher.key"; unset K
sg docker -c "$A/decifra.sh"
rm -f "$L/segreti/sqlcipher.key"
```

`decifra.sh` è il passo «decifra» del banco, con le sue protezioni, sull'immagine dello
standalone, che ha SQLCipher (l'host no):

```sh
docker run --rm --name grgo-prova-decifra --network none --log-driver none --ulimit core=0 \
  --init --user 1000:1000 --cap-drop ALL --security-opt no-new-privileges:true --read-only \
  --security-opt seccomp="$A/chromium-seccomp.json" \
  --tmpfs /tmp:uid=1000,gid=1000,mode=1777,size=256m --tmpfs /home/app:uid=1000,gid=1000,mode=0700,size=32m \
  --cpus 4 --memory 4g --pids-limit 512 \
  --mount type=bind,src="$L/estratto/data",dst=/catena/cifrato \
  --mount type=bind,src="$L/chiaro",dst=/catena/chiaro \
  --mount type=bind,src="$L/segreti/sqlcipher.key",dst=/run/secrets/sqlcipher.key,readonly \
  --mount type=bind,src="$L/segreti/secret_key_base",dst=/run/secrets/secret_key_base,readonly \
  --env DATABASE_PATH=/catena/cifrato/gestrip.db --env SECRET_KEY_BASE_FILE=/run/secrets/secret_key_base \
  --env SQLCIPHER_KEY_FILE=/run/secrets/sqlcipher.key --env RELEASE_TMP=/tmp --env RELEASE_DISTRIBUTION=none \
  --env ERL_CRASH_DUMP=/dev/null \
  gestrip-standalone:fab-219 bin/gestrip_standalone eval "$(cat "$A/decifra.exs")"
```

- `secret_key_base` è un valore casuale di prova, scritto da `decifra.sh` prima del container:
  l'applicazione non parte, serve solo perché la configurazione lo pretende.
- Deve stampare `GR_DECIFRATO`. Il database in chiaro finisce in `$L/chiaro/standalone.db`, in
  RAM.

### 5. Import (io)

```sh
grep MemAvailable /proc/meminfo     # soglia: media + 3 GiB
ulimit -c 0
SHA=$(sha256sum "$L/chiaro/standalone.db" | cut -d' ' -f1); DIM=$(stat -c %s "$L/chiaro/standalone.db")
MIN=; [ -d "$L/estratto/data/thumbnails" ] && MIN="-miniature $L/estratto/data/thumbnails"
"$A/gestrip" import -autorizzato -riferimento "Stefano, 31/10/2026: prova su una copia di un backup verificato" \
  -sorgente "$L/chiaro/standalone.db" -sha256 "$SHA" -dimensione "$DIM" \
  -destinazione "$L/go/gestrip.db" -report "$L/esito/report.json" \
  -media "$L/estratto/media" $MIN -senza-volume-cifrato
```

- L'import gira sull'host e non in un container. In un container con il limite di memoria, le
  foto scritte nel tmpfs si conterebbero nella sua memoria e lo farebbero chiudere.
- `-senza-volume-cifrato` serve perché il tmpfs non è LUKS. È il caso per cui l'opzione esiste
  (`docs/SVILUPPO.md`).

### 6. Report (io)

1. Leggo `$L/esito/report.json`, che non ha valori dei dati, e a Stefano mando in chat i conteggi
   veri:
   - verdetto;
   - righe lette e scritte per tabella;
   - avvisi e differenze per codice;
   - media copiati, mancanti e in più.
2. Nel repo va solo un **riassunto anonimo**, `docs/prova-dati-veri-esito.md`, generato dal report.
   Contiene:
   - il verdetto, il commit del Go e la data;
   - per ogni tabella, se le righe lette tornano con quelle scritte: sì o no, **senza i numeri**;
   - per ogni codice d'avviso o di differenza, la tabella, la colonna e quante voci;
   - per ogni colonna di soldi, se i totali tornano: sì o no, senza le somme;
   - per ogni famiglia di media: copiati tutti, mancanti, impronte diverse, file in più. Solo sì o
     no.
3. Nel repo **non** vanno:
   - i numeri di righe;
   - le somme;
   - gli id;
   - lo SHA-256 della sorgente;
   - il campo `oggetto`;
   - il riferimento.

   Il volume del lavoro e gli incassi non sono dati personali, ma il repo diventerà pubblico.
4. Il riassunto passa `scripts/parole-vietate --file` e gitleaks. Lo legge Stefano e solo con il
   suo ok si committa.
5. Ogni caso che emerge diventa:
   - una domanda in `docs/domande-aperte.md`, con codice, tabella e colonna, senza valori;
   - quando è deciso, un caso **inventato** nei test e nel database sintetico.

   Un verdetto «bloccato» non è un fallimento della prova: è quello che la prova deve trovare.
6. Il report completo non si conserva: sparisce con il tmpfs. Resta nella trascrizione della
   sessione. Lì ci sono solo conteggi, id e codici.

### 7. Smontaggio e verifica (io)

```sh
rm -rf "$L"
rm -f "$A/$ID.tar.age" "$A/$ID.tar.age.json"
ls -A /dev/shm                                   # niente gestrip-prova
grep -E 'MemAvailable|Shmem:' /proc/meminfo      # la memoria condivisa torna come prima
sg docker -c "docker ps -a --filter name=grgo-prova -q"    # vuoto
coredumpctl list --since "@$(stat -c %Y "$A/inizio")" 2>&1 # nessun core dump
find ~ /tmp /var/tmp -xdev -type f -newer "$A/inizio" -size +1M 2>/dev/null   # controllo a mano
rm -rf "$A"
```

`find` elenca i file grandi nati durante la prova. Li guardo uno per uno: non deve esserci niente
che venga dalla prova. Le cache e i log normali di pc-fisso li riconosco e li lascio.

Cosa resta alla fine:
- sul disco solo il riassunto anonimo e, se Stefano lo vuole, le domande nuove;
- l'archivio cifrato è cancellato, e comunque era cifrato;
- Stefano chiude il suo terminale.

## Dopo la prova

- Rifaccio il controllo completo della history (gitleaks e parole vietate su tutti i commit) e
  chiedo a Stefano per la pubblicazione (sua decisione 3 del 31/10/2026).
- Poi la T4, nell'ordine deciso: T4.1 busta → T4.3 foto e media → T4.4 stampe, etichette e firma →
  T4.2 home e QR.
