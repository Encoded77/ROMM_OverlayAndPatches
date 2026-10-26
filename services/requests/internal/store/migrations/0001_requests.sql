CREATE TABLE requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    username      TEXT    NOT NULL,
    igdb_id       INTEGER NOT NULL,
    title         TEXT    NOT NULL,
    year          INTEGER NOT NULL DEFAULT 0,
    cover_url     TEXT    NOT NULL DEFAULT '',
    platform_slug TEXT    NOT NULL,
    platform_name TEXT    NOT NULL,
    extras        TEXT    NOT NULL DEFAULT '[]',
    state         TEXT    NOT NULL,
    detail        TEXT    NOT NULL DEFAULT '',
    progress      REAL,
    rom_id        INTEGER,
    created_at    TEXT    NOT NULL,
    updated_at    TEXT    NOT NULL
);
CREATE INDEX requests_user ON requests (user_id, created_at);
CREATE INDEX requests_state ON requests (state);
