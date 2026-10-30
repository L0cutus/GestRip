# Prova dal vivo di T1: cosa serve e cosa si controlla

La prova va fatta insieme a Stefano (`prompt-via-area.md`): mai sulla produzione, su una macchina di
prova con un account Tailscale di prova (PIANO, T1 «Prova dal vivo»). Qui c'è la lista, preparata
alla fine della slice 017.

## Cosa serve

- Una macchina di prova Linux con Tailscale, su un account di prova, con il nome
  `*.ts.net` attivo e Funnel abilitato per quel nodo.
- Il binario `gestrip`, compilato senza CGO dal commit del giorno.
- Una cartella dati su un volume LUKS. Se non c'è, `-senza-volume-cifrato`, e la prova del volume
  si fa a parte.
- `/etc/gestrip/apparecchio.conf` con `indirizzo_interno = https://<nodo>.ts.net:8443`.
- `tailscale serve --https=8443 http://127.0.0.1:47101` (faccia interna) e
  `tailscale funnel --https=443 http://127.0.0.1:47102` (faccia pubblica).
- Un telefono nella tailnet, per la PWA e il login sulla 8443, e un telefono fuori dalla tailnet,
  per Funnel.

## Cosa si controlla

1. **Host:**
   - quale `Host` arriva al Go da `tailscale serve` (8443) e da Funnel (443): deve essere il nome
     `*.ts.net` e non `127.0.0.1`, altrimenti sparisce HSTS e `CrossOriginProtection` sbaglia;
   - quale `X-Forwarded-For` arriva da Funnel (CLI-21, PIANO §1.6).
2. **Intestazioni:** con `curl -sI` su tutte e due le facce:
   - HSTS, CSP, Referrer-Policy, nosniff e X-Permitted-Cross-Domain-Policies arrivano intatte;
   - Tailscale non ne aggiunge e non ne toglie.
3. **Cookie:**
   - i cookie `__Host-` della 443 e della 8443 convivono sullo stesso host;
   - il login sulla 8443 funziona anche nella PWA installata;
   - un modulo dalla 443 verso la 8443 riceve 403.
4. **HTTP in chiaro:**
   - `http://<nodo>.ts.net` e la 8443 in http non servono niente;
   - `ss -ltnp` mostra solo 127.0.0.1:47101 e 47102, più tailscaled.
5. **Registri:**
   - `journalctl -u tailscaled` durante una richiesta con un token: tailscaled non deve
     registrare il percorso (l'equivalente di «Caddy senza access log»);
   - il journal di gestrip con una richiesta con token, un 404, un 400 con `%FF` e un 403 CSRF: il
     token non deve comparire mai.
6. **Percorsi strani:** un percorso con `%zz` e uno con `%2F`, per vedere cosa ne fa Tailscale
   prima del Go.
7. **PWA:**
   - installazione dalla 8443;
   - il worker registrato e il manifest letto;
   - dopo un aggiornamento del binario gli asset nuovi arrivano (nomi con l'hash).
8. **Panic:** cosa vede il browser dietro tailscale serve quando un gestore va in panic (probabile
   502 del proxy, domanda aperta 11).
9. **Volume cifrato:** sulla macchina con LUKS vero, `serve` parte senza l'opzione e rifiuta una
   cartella dati spostata su un disco in chiaro.

Ogni esito va nella ricevuta della prova, con data e versione. Niente dati veri.
