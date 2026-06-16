CREATE TABLE repos (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL DEFAULT '',
    path         TEXT NOT NULL DEFAULT '',
    remote       TEXT NOT NULL DEFAULT 'origin',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX repos_owner_name_idx ON repos (owner, name);
