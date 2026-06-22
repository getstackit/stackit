DROP INDEX IF EXISTS repos_added_by_idx;
ALTER TABLE repos DROP COLUMN IF EXISTS added_by;
