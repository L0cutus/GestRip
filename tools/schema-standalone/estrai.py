# Dal database creato da ecto2sql.py scrive i riferimenti dei test di internal/importer:
#   - standalone-go-baseline.sql: il DDL finale dello standalone (sqlite_schema, solo schema);
#   - chiavi-segrete-standalone.txt: le chiavi di settings che contengono un segreto.
# Una chiave è segreta se, in un file di lib/, il campo che la legge (`campo: setting("chiave")`)
# è tra quelli che il modulo toglie da Inspect (`@derive {Inspect, except: [:campo]}`): è la regola
# con cui lo standalone stesso tiene i segreti fuori dai log (Outgoing.Config, Assist.Config).
# Uso: python3 -I estrai.py <database> <cartella lib> <cartella testdata> <commit>
import os, re, sqlite3, sys

db, lib, out, commit = sys.argv[1:5]

con = sqlite3.connect(db)
righe = con.execute("""SELECT type, name, sql FROM sqlite_schema
  WHERE sql IS NOT NULL AND name <> 'sqlite_sequence'
  ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name""").fetchall()
with open(os.path.join(out, 'standalone-go-baseline.sql'), 'w') as f:
    f.write('-- Schema finale dello standalone a go-baseline (%s), senza dati.\n' % commit)
    f.write('-- Generato da tools/schema-standalone/rigenera.sh: non modificare a mano.\n')
    f.write('-- schema_migrations la crea Ecto, non una migrazione: è aggiunta qui con la forma di ecto_sqlite3.\n\n')
    f.write('CREATE TABLE "schema_migrations" ("version" INTEGER PRIMARY KEY, "inserted_at" TEXT);\n\n')
    for tipo, nome, sql in righe:
        f.write(sql.strip() + ';\n\n')

chiavi = set()
for radice, _, file in os.walk(lib):
    for n in file:
        if not n.endswith('.ex'):
            continue
        s = open(os.path.join(radice, n)).read()
        campi = set()
        for m in re.finditer(r'@derive \{Inspect, except: \[([^\]]*)\]\}', s):
            campi |= set(re.findall(r':(\w+)', m.group(1)))
        for m in re.finditer(r'\b(\w+): setting\("([^"]+)"\)', s):
            if m.group(1) in campi:
                chiavi.add(m.group(2))
with open(os.path.join(out, 'chiavi-segrete-standalone.txt'), 'w') as f:
    f.write('# Chiavi segrete di settings dello standalone a go-baseline (%s).\n' % commit)
    f.write('# Generato da tools/schema-standalone/rigenera.sh: non modificare a mano.\n')
    for k in sorted(chiavi):
        f.write(k + '\n')
print('scritti %d oggetti dello schema e %d chiavi segrete' % (len(righe) + 1, len(chiavi)))
