-- Lavori automatici, media e provenienza dal Flask: job_runs, media_deletion_jobs,
-- historical_documents, legacy_import_mappings e legacy_import_captures dello standalone, nella
-- forma finale a go-baseline, con i nomi del Go (importer.Mappa). Come dati.
--   - Lavori, avvii ed esiti restano le parole dello standalone; il giorno è quello del negozio.
--   - I PDF storici e la cattura del Flask non si modificano e non si cancellano, come nello
--     standalone; l'indirizzo di contenuto è un CHECK.
--   - Le corrispondenze Flask→standalone restano come sono: sono la storia di quell'import e gli
--     id dello standalone sono quelli del Go.

CREATE TABLE esecuzioni_lavori (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  lavoro      TEXT    NOT NULL CHECK (lavoro IN ('overdue', 'chases', 'briefing')),
  avvio       TEXT    NOT NULL CHECK (avvio IN ('scheduled', 'manual')),
  esito       TEXT    NOT NULL CHECK (esito IN ('ok', 'skipped', 'failed')),
  eseguito_il TEXT    NOT NULL CHECK (eseguito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  giorno      TEXT    NOT NULL CHECK (giorno GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  avvisi      INTEGER NOT NULL DEFAULT 0 CHECK (avvisi >= 0),
  messaggi    INTEGER NOT NULL DEFAULT 0 CHECK (messaggi >= 0),
  dettaglio   TEXT
) STRICT;

CREATE INDEX esecuzioni_lavori_giorno ON esecuzioni_lavori (lavoro, avvio, giorno);

-- Oggetti dell'archivio media da cancellare (RIP-S-53, RIP-S-57).
CREATE TABLE cancellazioni_media (
  chiave    TEXT NOT NULL PRIMARY KEY,
  creato_il TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE documenti_storici (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  nome_file_flask TEXT    NOT NULL UNIQUE,
  chiave          TEXT    NOT NULL,
  byte            INTEGER NOT NULL CHECK (byte > 0),
  sha256          BLOB    NOT NULL,
  creato_il       TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiave = lower(hex(sha256)))
) STRICT;

CREATE INDEX documenti_storici_chiave ON documenti_storici (chiave);

CREATE TRIGGER documenti_storici_immutabili_modifica BEFORE UPDATE ON documenti_storici
BEGIN
  SELECT RAISE(ABORT, 'i documenti storici non si modificano');
END;

CREATE TRIGGER documenti_storici_immutabili_cancellazione BEFORE DELETE ON documenti_storici
BEGIN
  SELECT RAISE(ABORT, 'i documenti storici non si cancellano');
END;

CREATE TABLE corrispondenze_import_flask (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  tabella_flask      TEXT    NOT NULL CHECK (length(trim(tabella_flask)) > 0),
  id_flask           TEXT    NOT NULL CHECK (length(trim(id_flask)) > 0),
  tabella_standalone TEXT    NOT NULL CHECK (length(trim(tabella_standalone)) > 0),
  id_standalone      INTEGER NOT NULL CHECK (id_standalone > 0),
  creato_il          TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE UNIQUE INDEX corrispondenze_import_flask_sorgente ON corrispondenze_import_flask (tabella_flask, id_flask);
CREATE UNIQUE INDEX corrispondenze_import_flask_destinazione ON corrispondenze_import_flask (tabella_standalone, id_standalone);

-- Una riga sola: la cattura del database Flask da cui viene lo standalone.
CREATE TABLE cattura_import_flask (
  id                INTEGER PRIMARY KEY CHECK (id = 1),
  byte_sorgente     INTEGER NOT NULL CHECK (byte_sorgente > 0),
  sha256_sorgente   BLOB    NOT NULL CHECK (length(sha256_sorgente) = 32),
  file_accanto_json TEXT    NOT NULL CHECK (length(file_accanto_json) > 0 AND json_valid(file_accanto_json)),
  sha256_cattura    BLOB    NOT NULL CHECK (length(sha256_cattura) = 32),
  creato_il         TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TRIGGER cattura_import_flask_immutabile_modifica BEFORE UPDATE ON cattura_import_flask
BEGIN
  SELECT RAISE(ABORT, 'la cattura del Flask non si modifica');
END;

CREATE TRIGGER cattura_import_flask_immutabile_cancellazione BEFORE DELETE ON cattura_import_flask
BEGIN
  SELECT RAISE(ABORT, 'la cattura del Flask non si cancella');
END;
