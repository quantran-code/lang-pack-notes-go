-- 0 = draft, 1 = published, 2 = archived. DEFAULT 0 makes every existing note a draft.
ALTER TABLE notes ADD COLUMN status INTEGER NOT NULL DEFAULT 0;
