-- name: DeleteRepo :exec
DELETE FROM repos
WHERE id = $1;
