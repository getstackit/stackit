-- added_by records the GitHub login of the user who onboarded the repo at
-- runtime. Repos seeded directly (operator-managed) leave it empty, which the
-- server treats as "shared with all authenticated users".
ALTER TABLE repos ADD COLUMN added_by TEXT NOT NULL DEFAULT '';

CREATE INDEX repos_added_by_idx ON repos (added_by);
