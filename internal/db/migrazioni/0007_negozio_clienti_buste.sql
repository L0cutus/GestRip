-- Negozio, anagrafiche e buste: le tabelle shop_profiles, branding_assets, customers,
-- bag_sequences, oreficerie, artisans, handovers, ready_lots, repairs, repair_status_versions e
-- repair_photos dello standalone, nella forma finale a go-baseline (migrazioni da 20260905070000 a
-- 20261018180000), con i nomi del Go (importer.Mappa). Per ora come dati: la logica arriva con le
-- aree (T4).
--   - Le regole dello standalone come sono: NOT NULL, default, CHECK, chiavi esterne con il loro ON
--     DELETE, indici unici e indici.
--   - Istanti nella forma fissa db.FormatoIstante, date AAAA-MM-GG, booleani 0 o 1: l'import
--     traduce, il CHECK lo garantisce.
--   - Soldi in centesimi interi come nello standalone, con accanto il testo libero (OPS-03).
--   - Gli stati restano le parole dello standalone.
--   - L'indirizzo di contenuto delle foto (chiave = sha256 in esadecimale minuscolo) e i 32 byte
--     del codice Flask sono CHECK, dove lo standalone ha dei trigger: stesso effetto.
--   - La riga 1 di oreficerie («il negozio») la porta l'import; qui non si semina.

CREATE TABLE profilo_negozio (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  ambito              TEXT NOT NULL DEFAULT 'default' UNIQUE CHECK (ambito = 'default'),
  nome                TEXT NOT NULL,
  ragione_sociale     TEXT,
  indirizzo           TEXT,
  cap                 TEXT,
  citta               TEXT,
  provincia           TEXT,
  email               TEXT,
  email_vendite       TEXT,
  telefono            TEXT,
  partita_iva         TEXT,
  codice_fiscale      TEXT,
  condizioni_garanzia TEXT NOT NULL,
  sottotitolo         TEXT,
  orari               TEXT,
  instagram           TEXT,
  facebook            TEXT,
  sito                TEXT,
  creato_il           TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il       TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

-- Logo e icone (ADM-05), per indirizzo di contenuto.
CREATE TABLE immagini_negozio (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  tipo         TEXT    NOT NULL UNIQUE CHECK (tipo IN ('logo', 'icon_192', 'icon_512')),
  chiave       TEXT    NOT NULL UNIQUE CHECK (length(chiave) = 64 AND chiave NOT GLOB '*[^0-9a-f]*'),
  nome_file    TEXT    NOT NULL CHECK (length(trim(nome_file)) > 0),
  tipo_mime    TEXT    NOT NULL CHECK (tipo_mime IN ('image/jpeg', 'image/png')),
  byte         INTEGER NOT NULL CHECK (byte BETWEEN 1 AND 16777216),
  sha256       BLOB    NOT NULL CHECK (length(sha256) = 32),
  importata_il TEXT    NOT NULL CHECK (importata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiave = lower(hex(sha256)))
) STRICT;

CREATE TABLE clienti (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  nome          TEXT NOT NULL,
  telefono      TEXT NOT NULL,
  creato_il     TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX clienti_telefono ON clienti (telefono);

-- Il contatore dei numeri di busta N/AA, uno per anno.
CREATE TABLE sequenze_buste (
  anno          INTEGER PRIMARY KEY,
  ultimo_numero INTEGER NOT NULL
) STRICT;

CREATE TABLE oreficerie (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  nome     TEXT NOT NULL UNIQUE,
  telefono TEXT
) STRICT;

CREATE TABLE artigiani (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  nome     TEXT NOT NULL UNIQUE,
  telefono TEXT
) STRICT;

CREATE TABLE consegne_artigiani (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  artigiano_id  INTEGER NOT NULL REFERENCES artigiani (id) ON DELETE RESTRICT,
  chiusa_il     TEXT CHECK (chiusa_il IS NULL OR chiusa_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  note          TEXT,
  creato_il     TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX consegne_artigiani_artigiano ON consegne_artigiani (artigiano_id);

-- Lotti di avviso alle oreficerie: chiuso solo dopo l'invio, come nello standalone.
CREATE TABLE lotti_avviso (
  id                         INTEGER PRIMARY KEY AUTOINCREMENT,
  oreficeria_id              INTEGER NOT NULL REFERENCES oreficerie (id) ON DELETE RESTRICT,
  token                      TEXT NOT NULL UNIQUE,
  inviato_il                 TEXT CHECK (inviato_il IS NULL OR inviato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  chiuso_il                  TEXT CHECK (chiuso_il IS NULL OR chiuso_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  accesso_pubblico_spento_il TEXT CHECK (accesso_pubblico_spento_il IS NULL OR accesso_pubblico_spento_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il                  TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il              TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiuso_il IS NULL OR inviato_il IS NOT NULL)
) STRICT;

CREATE INDEX lotti_avviso_oreficeria ON lotti_avviso (oreficeria_id, id);

-- Le buste. Lo stato appartiene alla famiglia del tipo: riparazione o compro oro, con
-- «annullata» in tutte e due (20260908090000).
CREATE TABLE buste (
  id                            INTEGER PRIMARY KEY AUTOINCREMENT,
  cliente_id                    INTEGER NOT NULL REFERENCES clienti (id) ON DELETE RESTRICT,
  nome_cliente                  TEXT    NOT NULL,
  telefono_cliente              TEXT    NOT NULL,
  numero                        TEXT    NOT NULL UNIQUE,
  data_accettazione             TEXT    NOT NULL CHECK (data_accettazione GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  data_prevista                 TEXT    CHECK (data_prevista IS NULL OR data_prevista GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  data_prevista_testo           TEXT,
  data_pronta                   TEXT    CHECK (data_pronta IS NULL OR data_pronta GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  data_consegna                 TEXT    CHECK (data_consegna IS NULL OR data_consegna GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  descrizione                   TEXT    NOT NULL,
  note_pubbliche                TEXT,
  note_interne                  TEXT,
  stato                         TEXT    NOT NULL DEFAULT 'in_lavorazione',
  compro_oro                    INTEGER NOT NULL DEFAULT 0 CHECK (compro_oro IN (0, 1)),
  preventivo_centesimi          INTEGER CHECK (preventivo_centesimi IS NULL OR preventivo_centesimi >= 0),
  preventivo_testo              TEXT,
  prezzo_centesimi              INTEGER CHECK (prezzo_centesimi IS NULL OR prezzo_centesimi >= 0),
  prezzo_testo                  TEXT,
  giorni_in_piu                 INTEGER NOT NULL DEFAULT 0,
  oreficeria_id                 INTEGER REFERENCES oreficerie (id) ON DELETE RESTRICT,
  artigiano_id                  INTEGER REFERENCES artigiani (id) ON DELETE RESTRICT,
  consegna_artigiano_id         INTEGER REFERENCES consegne_artigiani (id) ON DELETE RESTRICT,
  lotto_avviso_id               INTEGER REFERENCES lotti_avviso (id) ON DELETE RESTRICT,
  hash_token_pubblico           BLOB    NOT NULL UNIQUE,
  token_pubblico                TEXT,
  token_pubblico_scade_il       TEXT    NOT NULL CHECK (token_pubblico_scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  token_pubblico_revocato_il    TEXT    CHECK (token_pubblico_revocato_il IS NULL OR token_pubblico_revocato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  hash_codice_flask             BLOB    CHECK (hash_codice_flask IS NULL OR length(hash_codice_flask) = 32),
  codice_flask_revocato_il      TEXT    CHECK (codice_flask_revocato_il IS NULL OR codice_flask_revocato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  fissata_il                    TEXT    CHECK (fissata_il IS NULL OR fissata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  fissata_il_testo_flask        TEXT,
  attesa_cliente                INTEGER NOT NULL DEFAULT 0 CHECK (attesa_cliente IN (0, 1)),
  attesa_dal                    TEXT,
  attesa_motivo                 TEXT,
  codice_qr                     TEXT,
  consegnata_artigiano_il       TEXT    CHECK (consegnata_artigiano_il IS NULL OR consegnata_artigiano_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  rientrata_artigiano_il        TEXT    CHECK (rientrata_artigiano_il IS NULL OR rientrata_artigiano_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  note_artigiano                TEXT,
  ultimo_messaggio_il           TEXT    CHECK (ultimo_messaggio_il IS NULL OR ultimo_messaggio_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  messaggi_letti_il             TEXT    CHECK (messaggi_letti_il IS NULL OR messaggi_letti_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  messaggi_testo_flask          TEXT,
  messaggi_visibili_dal         TEXT    CHECK (messaggi_visibili_dal IS NULL OR messaggi_visibili_dal GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  avviso_pronta_il              TEXT    CHECK (avviso_pronta_il IS NULL OR avviso_pronta_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  preventivo_accettato_il       TEXT    CHECK (preventivo_accettato_il IS NULL OR preventivo_accettato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  preventivo_accettato_letto_il TEXT    CHECK (preventivo_accettato_letto_il IS NULL OR preventivo_accettato_letto_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  preventivo_rifiutato_il       TEXT    CHECK (preventivo_rifiutato_il IS NULL OR preventivo_rifiutato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  preventivo_rifiutato_letto_il TEXT    CHECK (preventivo_rifiutato_letto_il IS NULL OR preventivo_rifiutato_letto_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  preventivo_rifiuto_motivo     TEXT,
  firma_chiave                  TEXT,
  firma_il                      TEXT    CHECK (firma_il IS NULL OR firma_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  firma_ritirata_da             TEXT,
  firma_byte                    INTEGER,
  ritardo_avvisato_il           TEXT    CHECK (ritardo_avvisato_il IS NULL OR ritardo_avvisato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  sollecito_avvisato_il         TEXT    CHECK (sollecito_avvisato_il IS NULL OR sollecito_avvisato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  link_inviato_il               TEXT    CHECK (link_inviato_il IS NULL OR link_inviato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  email_cliente                 TEXT,
  email_confermata_il           TEXT    CHECK (email_confermata_il IS NULL OR email_confermata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  email_pronta_inviata_il       TEXT    CHECK (email_pronta_inviata_il IS NULL OR email_pronta_inviata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  conservazione_iniziata_il     TEXT    CHECK (conservazione_iniziata_il IS NULL OR conservazione_iniziata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  cancellata_il                 TEXT    CHECK (cancellata_il IS NULL OR cancellata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il                     TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il                 TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK ((compro_oro = 0 AND stato IN ('in_lavorazione', 'preventivo', 'pronta', 'consegnata', 'in_attesa_materiali', 'annullata'))
      OR (compro_oro = 1 AND stato IN ('in_valutazione', 'valutata', 'perfezionata', 'chiusa', 'annullata')))
) STRICT;

CREATE INDEX buste_cliente ON buste (cliente_id);
CREATE INDEX buste_stato ON buste (stato);
CREATE INDEX buste_oreficeria ON buste (oreficeria_id);
CREATE INDEX buste_artigiano ON buste (artigiano_id);
CREATE INDEX buste_consegna_artigiano ON buste (consegna_artigiano_id);
CREATE INDEX buste_lotto_avviso ON buste (lotto_avviso_id);
CREATE INDEX buste_fissata ON buste (fissata_il);
-- Il cestino: parziale, come nello standalone (le liste chiedono IS NULL, il cestino il resto).
CREATE INDEX buste_cancellate ON buste (cancellata_il) WHERE cancellata_il IS NOT NULL;

-- Quante volte è cambiato lo stato di una busta (RIP-S-13); nessuna riga vale 0.
CREATE TABLE versioni_stato_buste (
  busta_id INTEGER PRIMARY KEY REFERENCES buste (id) ON DELETE CASCADE,
  versione INTEGER NOT NULL
) STRICT;

CREATE TABLE foto_buste (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id            INTEGER NOT NULL REFERENCES buste (id) ON DELETE CASCADE,
  posizione           INTEGER NOT NULL CHECK (posizione > 0),
  chiave              TEXT    NOT NULL,
  nome_file           TEXT    NOT NULL,
  tipo_mime           TEXT    NOT NULL DEFAULT 'image/png',
  byte                INTEGER NOT NULL CHECK (byte > 0),
  sha256              BLOB    NOT NULL,
  foto_busta          INTEGER NOT NULL DEFAULT 0 CHECK (foto_busta IN (0, 1)),
  visibile_cliente    INTEGER NOT NULL CHECK (visibile_cliente IN (0, 1)),
  visibile_artigiano  INTEGER NOT NULL CHECK (visibile_artigiano IN (0, 1)),
  visibile_oreficeria INTEGER NOT NULL CHECK (visibile_oreficeria IN (0, 1)),
  compressa_il        TEXT    CHECK (compressa_il IS NULL OR compressa_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il           TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il       TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (chiave = lower(hex(sha256)))
) STRICT;

CREATE UNIQUE INDEX foto_buste_posizione ON foto_buste (busta_id, posizione);
CREATE UNIQUE INDEX foto_buste_una_foto_busta ON foto_buste (busta_id) WHERE foto_busta = 1;
CREATE INDEX foto_buste_chiave ON foto_buste (chiave);
