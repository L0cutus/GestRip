-- Password dimenticata del titolare (ACC-13, ACC-14): le regole di password_resets dello
-- standalone (migrazione 20261027200100 a go-baseline), con i nomi del Go:
--   - nel database solo lo SHA-256 del token (32 byte), unico; il token in chiaro non si salva mai;
--   - scadenza, uso e revoca come istanti; ip di chi l'ha chiesto; canale da cui è partito;
--   - righe dell'account cancellate con l'account.
-- In più: l'origine (la pagina o il comando locale, D-04), perché i limiti della pagina contano
-- solo le sue righe; canale «console» per il link stampato dal comando.
CREATE TABLE reset_password (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  utente_id     INTEGER NOT NULL REFERENCES utenti (id) ON DELETE CASCADE,
  hash_token    BLOB    NOT NULL UNIQUE CHECK (length(hash_token) = 32),
  scade_il      TEXT    NOT NULL CHECK (scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  usato_il      TEXT    CHECK (usato_il IS NULL OR usato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  revocato_il   TEXT    CHECK (revocato_il IS NULL OR revocato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  ip            TEXT    CHECK (ip IS NULL OR length(ip) <= 64),
  canale        TEXT    CHECK (canale IS NULL OR canale IN ('telegram', 'email', 'console')),
  origine       TEXT    NOT NULL CHECK (origine IN ('pagina', 'console')),
  creato_il     TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX reset_password_creato ON reset_password (creato_il);
