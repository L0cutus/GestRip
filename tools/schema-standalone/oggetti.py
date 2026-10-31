# Scrive testdata/standalone-go-baseline-oggetti.json: i trigger e le viste di un database vero
# dello standalone (quello decifrato della catena del banco), come li tiene sqlite_schema.
# Solo lo schema, mai i dati. Il database si apre in sola lettura.
#
#   python3 -I tools/schema-standalone/oggetti.py <database in chiaro> <file json>
import json, sqlite3, sys

if len(sys.argv) != 3:
    sys.exit("uso: oggetti.py <database in chiaro> <file json>")
db, out = sys.argv[1], sys.argv[2]
c = sqlite3.connect("file:" + db + "?mode=ro", uri=True)
righe = c.execute(
    "SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE type IN ('trigger', 'view') ORDER BY type, name"
).fetchall()
c.close()
voci = [{"tipo": t, "nome": n, "tabella": tab, "sql": s} for t, n, tab, s in righe]
with open(out, "w", encoding="utf-8") as f:
    json.dump(voci, f, indent=1, ensure_ascii=False)
    f.write("\n")
print("%s: %d oggetti" % (out, len(voci)))
