-- name: InsertAuditLog :one
INSERT INTO audit_logs (actor_type, actor_id, action, entity, entity_id, payload, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- O ator administrador é resolvido para o nome atual; vazio se foi excluído ou ainda sem nome.
-- name: ListAuditLogs :many
SELECT l.id, l.actor_type, l.actor_id, l.action, l.entity, l.entity_id, l.payload, l.ip, l.created_at,
       COALESCE(btrim(a.first_name || ' ' || a.last_name), '')::text AS actor_name
FROM audit_logs l
LEFT JOIN admins a ON a.id = CASE WHEN l.actor_type = 'admin' AND l.actor_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN l.actor_id::uuid END
WHERE (sqlc.narg('actor_type')::text IS NULL OR l.actor_type = sqlc.narg('actor_type'))
  AND (sqlc.narg('actor_id')::text IS NULL OR l.actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('action')::text IS NULL OR l.action = sqlc.narg('action'))
  AND (sqlc.narg('entity')::text IS NULL OR l.entity = sqlc.narg('entity'))
  AND (sqlc.narg('entity_id')::text IS NULL OR l.entity_id = sqlc.narg('entity_id'))
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR l.created_at >= sqlc.narg('from_time'))
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR l.created_at <= sqlc.narg('to_time'))
ORDER BY l.created_at DESC, l.id DESC
LIMIT sqlc.arg('page_limit') OFFSET sqlc.arg('page_offset');

-- name: CountAuditLogs :one
SELECT count(*) FROM audit_logs
WHERE (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type'))
  AND (sqlc.narg('actor_id')::text IS NULL OR actor_id = sqlc.narg('actor_id'))
  AND (sqlc.narg('action')::text IS NULL OR action = sqlc.narg('action'))
  AND (sqlc.narg('entity')::text IS NULL OR entity = sqlc.narg('entity'))
  AND (sqlc.narg('entity_id')::text IS NULL OR entity_id = sqlc.narg('entity_id'))
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR created_at >= sqlc.narg('from_time'))
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR created_at <= sqlc.narg('to_time'));
