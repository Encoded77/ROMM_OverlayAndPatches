-- What the engine needs to drive a request through search, download and import.
ALTER TABLE requests ADD COLUMN provider       TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN job_id         TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN candidates     TEXT    NOT NULL DEFAULT '[]';
ALTER TABLE requests ADD COLUMN chosen         TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN searches       INTEGER NOT NULL DEFAULT 0;
ALTER TABLE requests ADD COLUMN next_search_at TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN searched_at    TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN placed         TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN parent_id      INTEGER REFERENCES requests(id) ON DELETE CASCADE;
ALTER TABLE requests ADD COLUMN extra          TEXT    NOT NULL DEFAULT '';
ALTER TABLE requests ADD COLUMN alt_names      TEXT    NOT NULL DEFAULT '[]';

-- Admin settings (rules per platform, provider, schedule, notifications), one JSON value per key.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
