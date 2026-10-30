-- La storia delle buste e i messaggi: repair_messages, artisan_reminders, outgoing_messages,
-- ready_notice_events, quote_decision_events, delivery_signature_events, telegram_subscriptions,
-- telegram_link_tokens, dettati_addestramento e dettati_esclusioni dello standalone, nella forma
-- finale a go-baseline, con i nomi del Go (importer.Mappa). Come dati: la logica arriva con le aree.
-- Le stesse regole della 0007; gli elenchi di parole (canali, destinatari, eventi, stati) restano
-- quelli dello standalone. La coda in uscita importata non parte da sola (D-27): lo decide il
-- codice dei canali, non lo schema.

CREATE TABLE messaggi_buste (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id      INTEGER NOT NULL REFERENCES buste (id) ON DELETE RESTRICT,
  autore        TEXT NOT NULL,
  canale        TEXT,
  testo         TEXT NOT NULL,
  inviato_il    TEXT NOT NULL CHECK (inviato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il     TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX messaggi_buste_busta ON messaggi_buste (busta_id, inviato_il);

CREATE TABLE solleciti_artigiani (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id              INTEGER NOT NULL REFERENCES buste (id) ON DELETE RESTRICT,
  artigiano_id          INTEGER NOT NULL REFERENCES artigiani (id) ON DELETE RESTRICT,
  consegna_artigiano_id INTEGER REFERENCES consegne_artigiani (id) ON DELETE RESTRICT,
  attore                TEXT,
  giorni_fuori          INTEGER NOT NULL,
  creato_il             TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX solleciti_artigiani_busta ON solleciti_artigiani (busta_id);
CREATE INDEX solleciti_artigiani_artigiano ON solleciti_artigiani (artigiano_id);

-- La coda in uscita. Una riga WhatsApp è già consegnata a mano, quindi solo 'accepted'.
CREATE TABLE messaggi_in_uscita (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id              INTEGER REFERENCES buste (id) ON DELETE SET NULL,
  canale                TEXT    NOT NULL CHECK (canale IN ('email', 'telegram', 'whatsapp')),
  destinatario          TEXT    NOT NULL CHECK (destinatario IN ('shop', 'customer', 'oreficeria', 'artisan')),
  indirizzo             TEXT    NOT NULL,
  oggetto               TEXT,
  testo                 TEXT    NOT NULL,
  stato                 TEXT    NOT NULL DEFAULT 'queued' CHECK (stato IN ('queued', 'accepted', 'failed')),
  tentativi             INTEGER NOT NULL DEFAULT 0 CHECK (tentativi >= 0),
  prossimo_tentativo_il TEXT    NOT NULL CHECK (prossimo_tentativo_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  accodato_il           TEXT    NOT NULL CHECK (accodato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  accettato_il          TEXT    CHECK (accettato_il IS NULL OR accettato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  fallito_il            TEXT    CHECK (fallito_il IS NULL OR fallito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  riferimento_provider  TEXT,
  ultimo_errore         TEXT,
  formato               TEXT,
  tastiera              TEXT    CHECK (tastiera IS NULL OR json_valid(tastiera)),
  CHECK (canale <> 'whatsapp' OR stato = 'accepted')
) STRICT;

CREATE INDEX messaggi_in_uscita_in_attesa ON messaggi_in_uscita (stato, prossimo_tentativo_il);
-- Dalla risposta di un cliente su Telegram alla sua busta (20261013160000).
CREATE INDEX messaggi_in_uscita_risposte_telegram ON messaggi_in_uscita (riferimento_provider, indirizzo)
  WHERE canale = 'telegram' AND destinatario = 'customer';

CREATE TABLE eventi_avviso_pronta (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id               INTEGER NOT NULL REFERENCES buste (id) ON DELETE RESTRICT,
  evento                 TEXT NOT NULL CHECK (evento IN ('notified', 'reset')),
  canale                 TEXT,
  messaggio_in_uscita_id INTEGER REFERENCES messaggi_in_uscita (id) ON DELETE SET NULL,
  attore                 TEXT,
  avvenuto_il            TEXT NOT NULL CHECK (avvenuto_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il              TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il          TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (evento = 'notified' OR messaggio_in_uscita_id IS NULL)
) STRICT;

CREATE INDEX eventi_avviso_pronta_busta ON eventi_avviso_pronta (busta_id, id);

CREATE TABLE eventi_preventivo (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id      INTEGER NOT NULL REFERENCES buste (id) ON DELETE RESTRICT,
  evento        TEXT NOT NULL CHECK (evento IN ('approved', 'rejected', 'approval_revoked', 'rejection_revoked')),
  parte         TEXT NOT NULL CHECK (parte IN ('customer', 'oreficeria', 'shop')),
  motivo        TEXT,
  attore        TEXT,
  avvenuto_il   TEXT NOT NULL CHECK (avvenuto_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il     TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  CHECK (evento = 'rejected' OR motivo IS NULL)
) STRICT;

CREATE INDEX eventi_preventivo_busta ON eventi_preventivo (busta_id, id);

CREATE TABLE eventi_firma_consegna (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id      INTEGER NOT NULL REFERENCES buste (id) ON DELETE RESTRICT,
  evento        TEXT NOT NULL CHECK (evento IN ('captured', 'reset')),
  chiave        TEXT NOT NULL CHECK (length(chiave) = 64),
  ritirata_da   TEXT,
  attore        TEXT,
  avvenuto_il   TEXT NOT NULL CHECK (avvenuto_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il     TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il TEXT NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX eventi_firma_consegna_busta ON eventi_firma_consegna (busta_id, id);

CREATE TABLE iscrizioni_telegram (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id       INTEGER NOT NULL REFERENCES buste (id) ON DELETE CASCADE,
  chat_id        INTEGER NOT NULL,
  consenso       INTEGER NOT NULL DEFAULT 0 CHECK (consenso IN (0, 1)),
  collegata_il   TEXT    NOT NULL CHECK (collegata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  consenso_il    TEXT    CHECK (consenso_il IS NULL OR consenso_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  fermata_il     TEXT    CHECK (fermata_il IS NULL OR fermata_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  motivo_fermata TEXT,
  creato_il      TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il  TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE UNIQUE INDEX iscrizioni_telegram_chat_busta ON iscrizioni_telegram (chat_id, busta_id);
CREATE INDEX iscrizioni_telegram_busta ON iscrizioni_telegram (busta_id);
CREATE INDEX iscrizioni_telegram_fermata ON iscrizioni_telegram (fermata_il);

-- Il token monouso del link t.me che crea un'iscrizione: nel database solo lo SHA-256.
CREATE TABLE token_telegram (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id   INTEGER NOT NULL REFERENCES buste (id) ON DELETE CASCADE,
  hash_token BLOB    NOT NULL UNIQUE,
  scade_il   TEXT    NOT NULL CHECK (scade_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il  TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX token_telegram_busta ON token_telegram (busta_id);

-- I dettati tenuti per l'addestramento: vivono quanto la loro busta (RIP-H-48).
CREATE TABLE dettati_addestramento (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id  INTEGER NOT NULL UNIQUE REFERENCES buste (id) ON DELETE CASCADE,
  testo     TEXT    NOT NULL,
  proposta  TEXT    NOT NULL CHECK (json_valid(proposta)),
  modello   TEXT,
  creato_il TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

-- I clienti che si sono opposti alla raccolta dei loro dettati.
CREATE TABLE dettati_esclusioni (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  cliente_id INTEGER NOT NULL UNIQUE REFERENCES clienti (id) ON DELETE CASCADE,
  creato_il  TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;
