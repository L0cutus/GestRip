-- Vendite online e garanzie: courtesy_document_sequences, sales, sale_revisions, sale_photos,
-- sale_revision_photos, sale_revision_seals e sale_revision_revocations dello standalone, nella
-- forma finale a go-baseline (20260905193000, 20260914090000, 20260917150000, 20260918140000,
-- 20260918150000), con i nomi del Go (importer.Mappa). Come dati: la logica arriva con T4.12.
--   - Canali e stati restano le parole dello standalone; date AAAA-MM-GG nell'intervallo dello
--     standalone; soldi in centesimi con accanto il testo.
--   - Ciclo di vita di una vendita e identità del documento: gli stessi CHECK.
--   - Revisioni, foto delle revisioni, sigilli e revoche non si modificano e non si cancellano
--     (VEN-15), e una revisione sigillata non prende foto: gli stessi trigger, per ora senza
--     l'eccezione del permesso di eliminazione definitiva, che arriva con VEN-18 e RIP-S-53 (quella
--     slice rifà i trigger di cancellazione). Per l'import: foto della revisione prima del sigillo.
--   - Il controllo della catena delle revisioni (sale_revisions_validate_chain) e quello
--     dell'accesso ai vecchi link (sales_legacy_access_requires_seal_*) arrivano con VEN-11 e VEN-31
--     (importer.Trigger).

CREATE TABLE sequenze_documenti_cortesia (
  anno          INTEGER PRIMARY KEY CHECK (anno > 0),
  ultimo_numero INTEGER NOT NULL CHECK (ultimo_numero > 0)
) STRICT;

CREATE TABLE vendite (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  data_vendita           TEXT    NOT NULL CHECK (data_vendita GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]' AND data_vendita BETWEEN '0001-01-01' AND '9987-12-31'),
  canale                 TEXT    NOT NULL CHECK (canale IN ('eBay', 'Vinted', 'Etsy', 'Subito', 'Sito', 'Altro')),
  descrizione            TEXT    NOT NULL,
  numero_serie           TEXT,
  prezzo_centesimi       INTEGER CHECK (prezzo_centesimi IS NULL OR prezzo_centesimi >= 0),
  prezzo_testo           TEXT,
  mesi_garanzia          INTEGER NOT NULL DEFAULT 24 CHECK (mesi_garanzia BETWEEN 0 AND 120),
  acquirente_nome        TEXT    NOT NULL,
  acquirente_indirizzo   TEXT,
  acquirente_cap         TEXT,
  acquirente_citta       TEXT,
  acquirente_provincia   TEXT,
  acquirente_email       TEXT,
  acquirente_telefono    TEXT,
  acquirente_id_fiscale  TEXT,
  note_interne           TEXT,
  stato                  TEXT    NOT NULL DEFAULT 'draft' CHECK (stato IN ('draft', 'issued', 'returned', 'refunded', 'annulled')),
  anno_documento         INTEGER,
  numero_documento       INTEGER,
  emessa_il              TEXT    CHECK (emessa_il IS NULL OR emessa_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  data_chiusura          TEXT    CHECK (data_chiusura IS NULL OR data_chiusura GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  motivo_chiusura        TEXT,
  id_flask               INTEGER,
  accesso_flask_spento_il TEXT   CHECK (accesso_flask_spento_il IS NULL OR accesso_flask_spento_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  stato_flask            TEXT,
  cancellata_il          TEXT    CHECK (cancellata_il IS NULL OR cancellata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il              TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il          TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK ((anno_documento IS NULL AND numero_documento IS NULL)
      OR (anno_documento IS NOT NULL AND numero_documento IS NOT NULL AND anno_documento > 0 AND numero_documento > 0)),
  CHECK ((stato = 'draft' AND anno_documento IS NULL AND numero_documento IS NULL
          AND emessa_il IS NULL AND data_chiusura IS NULL AND motivo_chiusura IS NULL)
      OR (stato = 'issued' AND anno_documento IS NOT NULL AND numero_documento IS NOT NULL
          AND emessa_il IS NOT NULL AND data_chiusura IS NULL AND motivo_chiusura IS NULL)
      OR (stato IN ('returned', 'refunded', 'annulled') AND anno_documento IS NOT NULL AND numero_documento IS NOT NULL
          AND emessa_il IS NOT NULL AND data_chiusura IS NOT NULL AND motivo_chiusura IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX vendite_documento ON vendite (anno_documento, numero_documento);
-- Una vendita per riga del Flask: i QR di garanzia già nei pacchi nominano quell'id.
CREATE UNIQUE INDEX vendite_id_flask ON vendite (id_flask) WHERE id_flask IS NOT NULL;
CREATE INDEX vendite_data ON vendite (data_vendita);
CREATE INDEX vendite_stato ON vendite (stato);
CREATE INDEX vendite_canale ON vendite (canale);
CREATE INDEX vendite_cancellate ON vendite (cancellata_il) WHERE cancellata_il IS NOT NULL;

CREATE TABLE revisioni_vendite (
  id                        INTEGER PRIMARY KEY AUTOINCREMENT,
  vendita_id                INTEGER NOT NULL REFERENCES vendite (id) ON DELETE RESTRICT,
  revisione_precedente_id   INTEGER REFERENCES revisioni_vendite (id) ON DELETE RESTRICT,
  numero_revisione          INTEGER NOT NULL CHECK (numero_revisione > 0),
  anno_documento            INTEGER NOT NULL,
  numero_documento          INTEGER NOT NULL CHECK (numero_documento > 0),
  data_vendita              TEXT    NOT NULL CHECK (data_vendita GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]' AND data_vendita BETWEEN '0001-01-01' AND '9987-12-31'),
  canale                    TEXT    NOT NULL CHECK (canale IN ('eBay', 'Vinted', 'Etsy', 'Subito', 'Sito', 'Altro')),
  descrizione               TEXT    NOT NULL,
  numero_serie              TEXT,
  prezzo_centesimi          INTEGER CHECK (prezzo_centesimi IS NULL OR prezzo_centesimi >= 0),
  prezzo_testo              TEXT,
  mesi_garanzia             INTEGER NOT NULL CHECK (mesi_garanzia BETWEEN 0 AND 120),
  garanzia_dal              TEXT    NOT NULL CHECK (garanzia_dal GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  garanzia_al               TEXT    NOT NULL CHECK (garanzia_al GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  acquirente_nome           TEXT    NOT NULL,
  acquirente_indirizzo      TEXT,
  acquirente_cap            TEXT,
  acquirente_citta          TEXT,
  acquirente_provincia      TEXT,
  acquirente_email          TEXT,
  acquirente_telefono       TEXT,
  acquirente_id_fiscale     TEXT,
  venditore_nome            TEXT    NOT NULL,
  venditore_ragione_sociale TEXT,
  venditore_indirizzo       TEXT,
  venditore_cap             TEXT,
  venditore_citta           TEXT,
  venditore_provincia       TEXT,
  venditore_email           TEXT,
  venditore_email_vendite   TEXT,
  venditore_telefono        TEXT,
  venditore_partita_iva     TEXT,
  condizioni_garanzia       TEXT    NOT NULL,
  hash_token_pubblico       BLOB    NOT NULL UNIQUE,
  token_pubblico_scade_il   TEXT    NOT NULL CHECK (token_pubblico_scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  emessa_il                 TEXT    NOT NULL CHECK (emessa_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il                 TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (garanzia_al >= garanzia_dal)
) STRICT;

CREATE UNIQUE INDEX revisioni_vendite_numero ON revisioni_vendite (vendita_id, numero_revisione);
CREATE INDEX revisioni_vendite_precedente ON revisioni_vendite (revisione_precedente_id);

-- Più di sei foto per vendita: sei è solo il limite dell'impaginazione del PDF (20260918140000).
CREATE TABLE foto_vendite (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  vendita_id INTEGER NOT NULL REFERENCES vendite (id) ON DELETE CASCADE,
  posizione  INTEGER NOT NULL CHECK (posizione > 0),
  chiave     TEXT    NOT NULL,
  nome_file  TEXT    NOT NULL,
  tipo_mime  TEXT    NOT NULL,
  byte       INTEGER NOT NULL CHECK (byte > 0),
  sha256     BLOB    NOT NULL,
  creato_il  TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiave = lower(hex(sha256)))
) STRICT;

CREATE UNIQUE INDEX foto_vendite_posizione ON foto_vendite (vendita_id, posizione);
CREATE INDEX foto_vendite_chiave ON foto_vendite (chiave);

CREATE TABLE foto_revisioni_vendite (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  revisione_vendita_id INTEGER NOT NULL REFERENCES revisioni_vendite (id) ON DELETE RESTRICT,
  posizione            INTEGER NOT NULL CHECK (posizione > 0),
  chiave               TEXT    NOT NULL,
  nome_file            TEXT    NOT NULL,
  tipo_mime            TEXT    NOT NULL,
  byte                 INTEGER NOT NULL CHECK (byte > 0),
  sha256               BLOB    NOT NULL,
  creato_il            TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiave = lower(hex(sha256)))
) STRICT;

CREATE UNIQUE INDEX foto_revisioni_vendite_posizione ON foto_revisioni_vendite (revisione_vendita_id, posizione);

CREATE TABLE sigilli_revisioni_vendite (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  revisione_vendita_id INTEGER NOT NULL UNIQUE REFERENCES revisioni_vendite (id) ON DELETE RESTRICT,
  sigillata_il         TEXT    NOT NULL CHECK (sigillata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TABLE revoche_revisioni_vendite (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  revisione_vendita_id INTEGER NOT NULL UNIQUE REFERENCES revisioni_vendite (id) ON DELETE RESTRICT,
  motivo               TEXT    NOT NULL,
  revocata_il          TEXT    NOT NULL CHECK (revocata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE TRIGGER revisioni_vendite_immutabili_modifica BEFORE UPDATE ON revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le revisioni delle vendite non si modificano');
END;

CREATE TRIGGER revisioni_vendite_immutabili_cancellazione BEFORE DELETE ON revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le revisioni delle vendite non si cancellano');
END;

CREATE TRIGGER foto_revisioni_vendite_immutabili_modifica BEFORE UPDATE ON foto_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le foto delle revisioni non si modificano');
END;

CREATE TRIGGER foto_revisioni_vendite_immutabili_cancellazione BEFORE DELETE ON foto_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le foto delle revisioni non si cancellano');
END;

CREATE TRIGGER foto_revisioni_vendite_solo_aperte BEFORE INSERT ON foto_revisioni_vendite
WHEN EXISTS (SELECT 1 FROM sigilli_revisioni_vendite s WHERE s.revisione_vendita_id = NEW.revisione_vendita_id)
BEGIN
  SELECT RAISE(ABORT, 'la revisione è sigillata: niente foto nuove');
END;

CREATE TRIGGER sigilli_revisioni_vendite_immutabili_modifica BEFORE UPDATE ON sigilli_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'i sigilli delle revisioni non si modificano');
END;

CREATE TRIGGER sigilli_revisioni_vendite_immutabili_cancellazione BEFORE DELETE ON sigilli_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'i sigilli delle revisioni non si cancellano');
END;

CREATE TRIGGER revoche_revisioni_vendite_immutabili_modifica BEFORE UPDATE ON revoche_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le revoche delle revisioni non si modificano');
END;

CREATE TRIGGER revoche_revisioni_vendite_immutabili_cancellazione BEFORE DELETE ON revoche_revisioni_vendite
BEGIN
  SELECT RAISE(ABORT, 'le revoche delle revisioni non si cancellano');
END;
