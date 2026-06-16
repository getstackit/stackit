-- name: ListRepos :many
SELECT id, display_name, owner, name, path, remote, created_at, updated_at
FROM repos
ORDER BY id;
