#!/bin/sh
# Avvio del container del livello browser del banco. Lo script degli scenari (banco.mjs) arriva
# montato in sola lettura in /gate, accanto a node_modules.
set -eu
umask 077
# L'uscita di debug di Playwright contiene indirizzi e valori dei moduli: mai accesa qui.
unset DEBUG PWDEBUG NODE_DEBUG NODE_OPTIONS
test -f "${TEST_CA_FILE:?serve la CA dell istanza}"
nssdb="$HOME/.pki/nssdb"
mkdir -p "$nssdb"
# certutil -N su un database che c'è già chiede la vecchia password e, senza terminale, riprova
# per sempre: si crea solo se manca, e ogni chiamata ha un tempo massimo.
if [ ! -f "$nssdb/cert9.db" ]; then
    timeout 30 certutil -N --empty-password -d "sql:$nssdb" </dev/null
fi
timeout 30 certutil -A -d "sql:$nssdb" -n grgo-banco-ca -t 'C,,' -i "$TEST_CA_FILE" </dev/null
export NODE_EXTRA_CA_CERTS="$TEST_CA_FILE"
# Lo script si controlla prima del giro: un errore di sintassi ferma tutto con un messaggio chiaro.
node --check /gate/banco.mjs
exec node /gate/banco.mjs "$@"
