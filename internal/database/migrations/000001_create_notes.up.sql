CREATE TABLE notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT      NOT NULL,
    body       TEXT      NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL
);
