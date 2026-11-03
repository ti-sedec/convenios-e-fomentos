-- name: ListEntityTypes :many
SELECT * FROM entity_types
WHERE (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
ORDER BY sort_order, name;

-- name: GetEntityType :one
SELECT * FROM entity_types WHERE id = $1;

-- Trava a linha: impede novos vínculos enquanto o tipo é alterado ou excluído.
-- name: GetEntityTypeForUpdate :one
SELECT * FROM entity_types WHERE id = $1 FOR UPDATE;

-- name: CreateEntityType :one
INSERT INTO entity_types (code, name, nature, non_profit, active, sort_order)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateEntityType :one
UPDATE entity_types
SET name = $2, nature = $3, non_profit = $4, active = $5, sort_order = $6, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteEntityType :execrows
DELETE FROM entity_types WHERE id = $1 AND NOT is_system;

-- Carga dos padrões: não altera o que um administrador já editou.
-- name: InsertDefaultEntityType :execrows
INSERT INTO entity_types (code, name, nature, non_profit, is_system, sort_order)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (code) DO NOTHING;

-- name: CountMunicipalities :one
SELECT count(*) FROM municipalities;

-- name: InsertMunicipalities :exec
INSERT INTO municipalities (ibge_code, name, uf, search_name)
SELECT unnest(sqlc.arg('codes')::text[]), unnest(sqlc.arg('names')::text[]), unnest(sqlc.arg('ufs')::text[]), unnest(sqlc.arg('search_names')::text[])
ON CONFLICT (ibge_code) DO NOTHING;

-- name: ListMunicipalities :many
SELECT ibge_code, name, uf FROM municipalities
WHERE (sqlc.narg('uf')::text IS NULL OR uf = sqlc.narg('uf'))
  AND (sqlc.narg('search')::text IS NULL OR search_name LIKE sqlc.narg('search') || '%')
ORDER BY search_name, ibge_code
LIMIT $1 OFFSET $2;

-- name: CountMunicipalitiesFiltered :one
SELECT count(*) FROM municipalities
WHERE (sqlc.narg('uf')::text IS NULL OR uf = sqlc.narg('uf'))
  AND (sqlc.narg('search')::text IS NULL OR search_name LIKE sqlc.narg('search') || '%');

-- name: GetMunicipality :one
SELECT ibge_code, name, uf FROM municipalities WHERE ibge_code = $1;

-- name: ListParliamentarians :many
SELECT * FROM parliamentarians
WHERE (sqlc.narg('chamber')::text IS NULL OR chamber = sqlc.narg('chamber'))
  AND (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search') || '%')
ORDER BY lower(name), chamber;

-- name: GetParliamentarian :one
SELECT * FROM parliamentarians WHERE id = $1;

-- name: ListParliamentariansInUse :many
SELECT p.id FROM parliamentarians p
WHERE EXISTS (SELECT 1 FROM project_amendments a WHERE a.parliamentarian_id = p.id);

-- name: ParliamentarianInUse :one
SELECT EXISTS (SELECT 1 FROM project_amendments WHERE parliamentarian_id = $1);

-- name: GetParliamentarianForUpdate :one
SELECT * FROM parliamentarians WHERE id = $1 FOR UPDATE;

-- name: CreateParliamentarian :one
INSERT INTO parliamentarians (name, chamber, active) VALUES ($1, $2, $3) RETURNING *;

-- name: UpdateParliamentarian :one
UPDATE parliamentarians SET name = $2, chamber = $3, active = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteParliamentarian :execrows
DELETE FROM parliamentarians WHERE id = $1;
