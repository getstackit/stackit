-- name: GetRepo :one
SELECT id, display_name, owner, name, path, remote, created_at, updated_at
FROM repos
WHERE id = $1;
