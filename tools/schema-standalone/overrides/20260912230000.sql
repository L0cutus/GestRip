DROP INDEX IF EXISTS idx_outgoing_messages_pending;
CREATE TABLE outgoing_messages_rebuild (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  repair_id          INTEGER REFERENCES repairs(id) ON DELETE SET NULL,
  channel            TEXT NOT NULL CONSTRAINT valid_channel CHECK (channel IN ('email', 'telegram', 'whatsapp')),
  party              TEXT NOT NULL CONSTRAINT valid_party CHECK (party IN ('shop', 'customer', 'oreficeria')),
  address            TEXT NOT NULL,
  subject            TEXT,
  body               TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'queued' CONSTRAINT valid_status CHECK (status IN ('queued', 'accepted', 'failed')),
  attempts           INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_attempts CHECK (attempts >= 0),
  next_attempt_at    TEXT NOT NULL,
  queued_at          TEXT NOT NULL,
  accepted_at        TEXT,
  failed_at          TEXT,
  provider_reference TEXT,
  last_error         TEXT,
  CONSTRAINT handoff_is_accepted CHECK (channel <> 'whatsapp' OR status = 'accepted')
);
DROP TABLE outgoing_messages;
ALTER TABLE outgoing_messages_rebuild RENAME TO outgoing_messages;
CREATE INDEX idx_outgoing_messages_pending ON outgoing_messages(status, next_attempt_at);
