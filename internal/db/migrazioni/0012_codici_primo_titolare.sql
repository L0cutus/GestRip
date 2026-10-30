-- Il primo titolare su un database vuoto (ACC-16, «sostituita»): il codice monouso e a scadenza
-- del link /configura/<codice>, stampato da «gestrip primo-titolare» al posto dei due file privati
-- dell'installer dello standalone. Solo Go, come reset_password e inviti_utenti:
--   - nel database solo lo SHA-256 del codice (32 byte), unico; il codice in chiaro non si salva mai;
--   - scadenza, uso e revoca come istanti (un codice nuovo annulla quelli aperti);
--   - nessun legame con un account: quando il codice serve, utenti è vuota.
CREATE TABLE codici_primo_titolare (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  hash_codice   BLOB    NOT NULL UNIQUE CHECK (length(hash_codice) = 32),
  scade_il      TEXT    NOT NULL CHECK (scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  usato_il      TEXT    CHECK (usato_il IS NULL OR usato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  revocato_il   TEXT    CHECK (revocato_il IS NULL OR revocato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il     TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;
