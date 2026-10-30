DROP INDEX IF EXISTS idx_job_runs_scheduled;
CREATE TABLE job_runs_rebuild (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  job           TEXT NOT NULL CONSTRAINT valid_job CHECK (job IN ('overdue', 'chases', 'briefing')),
  trigger       TEXT NOT NULL CONSTRAINT valid_trigger CHECK (trigger IN ('scheduled', 'manual')),
  outcome       TEXT NOT NULL CONSTRAINT valid_outcome CHECK (outcome IN ('ok', 'skipped', 'failed')),
  ran_at        TEXT NOT NULL,
  scheduled_for TEXT NOT NULL,
  notices       INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_notices CHECK (notices >= 0),
  messages      INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_messages CHECK (messages >= 0),
  detail        TEXT
);
DROP TABLE job_runs;
ALTER TABLE job_runs_rebuild RENAME TO job_runs;
CREATE INDEX idx_job_runs_scheduled ON job_runs(job, trigger, scheduled_for);
