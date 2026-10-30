-- Registro dei corrispettivi di emergenza: emergency_events ed emergency_entries dello standalone,
-- nella forma finale a go-baseline (20260914180000, 20260914210000, 20261030090000), con i nomi del
-- Go (importer.Mappa). Come dati: la logica arriva con la sua area.
--   - Soldi in centesimi interi come nello standalone; imponibile più IVA uguale al lordo.
--   - Un solo evento aperto alla volta (indice unico parziale su blocco_aperto, sempre 1).
--   - Numerazione annuale unica; una riga si storna una volta sola; una busta ha al massimo un
--     addebito vivo per giro (slice 218).
--   - Il metodo di pagamento e l'aliquota restano liberi come nel database dello standalone (li
--     controlla il codice).

CREATE TABLE eventi_emergenza (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  iniziato_il            TEXT    NOT NULL CHECK (iniziato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  chiuso_il              TEXT    CHECK (chiuso_il IS NULL OR chiuso_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  motivo                 TEXT    NOT NULL,
  matricola_registratore TEXT,
  note                   TEXT,
  aperto_da              TEXT,
  chiuso_da              TEXT,
  blocco_aperto          INTEGER NOT NULL DEFAULT 1,
  creato_il              TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il          TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE UNIQUE INDEX eventi_emergenza_uno_aperto ON eventi_emergenza (blocco_aperto) WHERE chiuso_il IS NULL;

CREATE TABLE righe_emergenza (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  evento_emergenza_id  INTEGER NOT NULL REFERENCES eventi_emergenza (id) ON DELETE RESTRICT,
  anno_documento       INTEGER NOT NULL,
  numero               INTEGER NOT NULL,
  descrizione          TEXT    NOT NULL CHECK (length(trim(descrizione)) > 0),
  lordo_centesimi      INTEGER NOT NULL,
  aliquota_iva         INTEGER NOT NULL,
  imponibile_centesimi INTEGER NOT NULL,
  iva_centesimi        INTEGER NOT NULL,
  pagamento            TEXT    NOT NULL,
  nome_cliente         TEXT,
  busta_id             INTEGER REFERENCES buste (id) ON DELETE RESTRICT,
  storno_di_id         INTEGER REFERENCES righe_emergenza (id) ON DELETE RESTRICT,
  giro_addebito        INTEGER NOT NULL DEFAULT 0,
  attore               TEXT,
  creato_il            TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (imponibile_centesimi + iva_centesimi = lordo_centesimi)
) STRICT;

CREATE UNIQUE INDEX righe_emergenza_documento ON righe_emergenza (anno_documento, numero);
CREATE INDEX righe_emergenza_evento ON righe_emergenza (evento_emergenza_id, numero);
CREATE UNIQUE INDEX righe_emergenza_uno_storno ON righe_emergenza (storno_di_id) WHERE storno_di_id IS NOT NULL;
CREATE UNIQUE INDEX righe_emergenza_un_addebito ON righe_emergenza (busta_id, giro_addebito)
  WHERE busta_id IS NOT NULL AND storno_di_id IS NULL;
