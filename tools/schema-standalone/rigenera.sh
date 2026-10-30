#!/bin/sh
# Rigenera i riferimenti dei test di internal/importer dallo standalone vero, in sola lettura
# (git show del tag, mai checkout). Vedi README.md.
set -eu
STANDALONE="${GESTRIP_STANDALONE:-$HOME/sviluppo/gestrip-standalone}"
REF="${GESTRIP_RIFERIMENTO:-go-baseline}"
cd "$(dirname "$0")/../.."
mkdir -p to-be-deleted-after-use
T=$(mktemp -d to-be-deleted-after-use/schema-standalone.XXXXXX)
mkdir -p "$T/mig" "$T/lib"
commit=$(git -C "$STANDALONE" rev-parse --short "$REF^{commit}")
for f in $(git -C "$STANDALONE" ls-tree --name-only "$REF" priv/repo/migrations/ | grep '\.exs$' | grep -v formatter); do
  git -C "$STANDALONE" show "$REF:$f" > "$T/mig/$(basename "$f")"
done
for f in $(git -C "$STANDALONE" grep -l 'Inspect, except:' "$REF" -- lib | sed "s/^$REF://"); do
  git -C "$STANDALONE" show "$REF:$f" > "$T/lib/$(echo "$f" | tr / _)"
done
python3 -I tools/schema-standalone/ecto2sql.py "$T/mig" "$T/finale.db"
python3 -I tools/schema-standalone/estrai.py "$T/finale.db" "$T/lib" internal/importer/testdata "$commit"
echo "migrazioni: $(ls "$T/mig" | wc -l); file di lavoro in $T (da cancellare)"
