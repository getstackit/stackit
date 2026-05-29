-- name: InsertRepo :one
INSERT INTO repos (id, display_name, owner, name, path, remote)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, display_name, owner, name, path, remote, created_at, updated_at;
