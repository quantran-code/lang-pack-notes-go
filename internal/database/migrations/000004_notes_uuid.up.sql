-- Note ids become canonical lowercase UUID strings. SQLite cannot mint a UUID, so the mapping of
-- every existing note to its new id is generated in Go at start-up and replaces the marker line
-- below with one INSERT INTO note_id_map statement; on a database without notes it is replaced by
-- nothing at all. The map is TEMP: it lives for this migration and is dropped at its end.

CREATE TEMP TABLE note_id_map (old_id INTEGER, new_id TEXT NOT NULL);

{{note_id_map}}

CREATE TABLE notes_new (
    id         TEXT PRIMARY KEY NOT NULL,
    title      TEXT      NOT NULL,
    body       TEXT      NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    status     INTEGER   NOT NULL DEFAULT 0
);

-- Rows are copied in id order, so the rebuilt table's rowids follow the old id order: that is what
-- the down migration falls back on to break a tie between two notes sharing a created_at.
INSERT INTO notes_new (id, title, body, created_at, status)
SELECT m.new_id, n.title, n.body, n.created_at, n.status
FROM notes n
         JOIN note_id_map m ON m.old_id = n.id
ORDER BY n.id;

-- The links are set aside before note_tags goes, because it has to be dropped before notes: with
-- foreign keys enforced, no row of note_tags may outlive the note it points at.
CREATE TEMP TABLE note_tags_backup AS SELECT note_id, tag_id FROM note_tags;

DROP TABLE note_tags;
DROP TABLE notes;

ALTER TABLE notes_new RENAME TO notes;

CREATE TABLE note_tags (
    note_id TEXT    NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX idx_note_tags_tag_id ON note_tags (tag_id);

-- Every link still points at the same note, now named by its UUID.
INSERT INTO note_tags (note_id, tag_id)
SELECT m.new_id, b.tag_id
FROM note_tags_backup b
         JOIN note_id_map m ON m.old_id = b.note_id;

DROP TABLE note_tags_backup;
DROP TABLE note_id_map;
