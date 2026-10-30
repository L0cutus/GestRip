-- Inviti degli account (ACC-07, ACC-08, ACC-09): le regole di user_invitations dello standalone
-- (migrazione 20260915090000 a go-baseline), con i nomi del Go:
--   - nel database solo lo SHA-256 del token (32 byte), unico; il token in chiaro non si salva mai;
--   - scadenza, accettazione e revoca come istanti;
--   - righe dell'account cancellate con l'account.
-- Più rigido dello standalone, dal lato sicuro: istanti nella forma fissa.
CREATE TABLE inviti_utenti (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  utente_id     INTEGER NOT NULL REFERENCES utenti (id) ON DELETE CASCADE,
  hash_token    BLOB    NOT NULL UNIQUE CHECK (length(hash_token) = 32),
  scade_il      TEXT    NOT NULL CHECK (scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  accettato_il  TEXT    CHECK (accettato_il IS NULL OR accettato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  revocato_il   TEXT    CHECK (revocato_il IS NULL OR revocato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il     TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX inviti_utenti_utente ON inviti_utenti (utente_id);
