-- Impostazioni del negozio, chiave e valore: copia la regola di `settings` dello standalone
-- (20260909120000_create_settings.exs). Istanti in UTC, forma db.FormatoIstante.
CREATE TABLE impostazioni (
  chiave        TEXT PRIMARY KEY,
  valore        TEXT NOT NULL,
  aggiornata_il TEXT NOT NULL
) STRICT;
