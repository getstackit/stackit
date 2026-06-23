-- name: GetRepo :one
SELECT id, display_name, owner, name, path, remote, created_at, updated_at, added_by
FROM repos
WHERE id = $1;
