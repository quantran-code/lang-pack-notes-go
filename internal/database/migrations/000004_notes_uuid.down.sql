-- The migration back: note ids become the integers 1..N again, assigned in created_at order with
-- rowid breaking ties, so the old order is restored without stashing the ids the up migration
-- replaced. The map is TEMP: it lives for this migration and is dropped at its end.

CREATE TEMP TABLE note_uuid_map AS
SELECT id AS old_id, ROW_NUMBER() OVER (ORDER BY created_at, rowid) AS new_id
FROM notes;

CREATE TABLE notes_down (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT      NOT NULL,
    body       TEXT      NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    status     INTEGER   NOT NULL DEFAULT 0
);

INSERT INTO notes_down (id, title, body, created_at, status)
SELECT m.new_id, n.title, n.body, n.created_at, n.status
FROM notes n
         JOIN note_uuid_map m ON m.old_id = n.id;

-- The links are set aside before note_tags goes, because it has to be dropped before notes: with
-- foreign keys enforced, no row of note_tags may outlive the note it points at.
CREATE TEMP TABLE note_tags_backup AS SELECT note_id, tag_id FROM note_tags;

DROP TABLE note_tags;
DROP TABLE notes;

ALTER TABLE notes_down RENAME TO notes;

CREATE TABLE note_tags (
    note_id INTEGER NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX idx_note_tags_tag_id ON note_tags (tag_id);

-- Every link still points at the same note, now named by its integer id.
INSERT INTO note_tags (note_id, tag_id)
SELECT m.new_id, b.tag_id
FROM note_tags_backup b
         JOIN note_uuid_map m ON m.old_id = b.note_id;

DROP TABLE note_tags_backup;
DROP TABLE note_uuid_map;
