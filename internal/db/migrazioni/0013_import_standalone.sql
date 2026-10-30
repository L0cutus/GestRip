-- Il segno di un import dallo standalone (gestrip import, PIANO T2): una riga sola, scritta
-- dall'import nella stessa transazione dei dati. Solo Go. Dice che il database viene da un import
-- e da quale sorgente (impronta e dimensione, mai un dato del negozio), così:
--   - la conservazione dell'audit non cancella niente finché non c'è un backup verificato dopo
--     l'import (Stefano, 30/10/2026: PIANO T2, ADM-17); la slice dei backup toglie il blocco
--     confrontando importato_il con il registro dei backup verificati (D-11);
--   - un database già importato si riconosce.
-- Il riferimento dell'autorizzazione (chi e quando) sta solo nel report, non qui.
CREATE TABLE import_standalone (
  id              INTEGER PRIMARY KEY CHECK (id = 1),
  modalita        TEXT    NOT NULL CHECK (modalita IN ('sintetico', 'autorizzato')),
  sha256_sorgente BLOB    NOT NULL CHECK (length(sha256_sorgente) = 32),
  byte_sorgente   INTEGER NOT NULL CHECK (byte_sorgente > 0),
  importato_il    TEXT    NOT NULL CHECK (importato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;
