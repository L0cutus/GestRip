#!/bin/sh
# Esegue un comando a priorità bassa e con limiti di CPU e RAM, così il dettato in
# negozio su pc-fisso non rallenta. Uso: scripts/limiti.sh <comando> [argomenti...]
# Dove manca systemd utente (CI, altre macchine) usa il solo nice.
set -e
if command -v systemd-run >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
  exec systemd-run --user --scope --quiet \
    -p CPUQuota="${GESTRIP_CPU_QUOTA:-800%}" -p MemoryMax="${GESTRIP_MEMORY_MAX:-16G}" \
    nice -n 19 "$@"
fi
exec nice -n 19 "$@"
