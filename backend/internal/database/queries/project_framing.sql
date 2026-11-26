-- name: NextFramingRound :one
SELECT (COALESCE(max(round), 0) + 1)::int FROM project_framings WHERE project_id = $1;

-- name: InsertFraming :one
INSERT INTO project_framings (project_id, round, department_id, department_name, indicated_by_id)
VALUES (sqlc.arg(project_id), sqlc.arg(round), sqlc.arg(department_id), sqlc.arg(department_name), sqlc.arg(indicated_by_id))
RETURNING *;

-- name: GetPendingFraming :one
SELECT * FROM project_framings WHERE project_id = $1 AND decision = 'pending';

-- name: GetLatestFraming :one
SELECT * FROM project_framings WHERE project_id = $1 ORDER BY round DESC LIMIT 1;

-- Só decide uma rodada ainda pendente (a trava da linha do projeto já serializa quem decide).
-- name: DecideFraming :one
UPDATE project_framings
SET decision = sqlc.arg(decision), decided_by_id = sqlc.arg(decided_by_id), decided_at = now(), justification = sqlc.arg(justification)
WHERE id = sqlc.arg(id) AND decision = 'pending'
RETURNING *;

-- name: ListFramings :many
SELECT f.id, f.round, f.department_id, f.department_name, f.indicated_at, f.decision, f.decided_at, f.justification,
       COALESCE(NULLIF(trim(ia.first_name || ' ' || ia.last_name), ''), ia.email::text, '')::text AS indicated_by_name,
       COALESCE(NULLIF(trim(da.first_name || ' ' || da.last_name), ''), da.email::text, '')::text AS decided_by_name
FROM project_framings f
LEFT JOIN admins ia ON ia.id = f.indicated_by_id
LEFT JOIN admins da ON da.id = f.decided_by_id
WHERE f.project_id = $1
ORDER BY f.round;

-- Define o departamento e o enquadramento do projeto, e a etapa. framing_status e department_id nulos limpam (recusa).
-- release_analyst libera o analista responsável (confirmação do enquadramento: o projeto volta à fila dos analistas).
-- name: SetProjectFraming :one
UPDATE projects
SET department_id = sqlc.narg(department_id), framing_status = sqlc.narg(framing_status), stage = sqlc.arg(stage),
    analyst_id = CASE WHEN sqlc.arg(release_analyst)::boolean THEN NULL ELSE analyst_id END,
    version = version + 1, updated_at = now(), updated_by_id = sqlc.arg(updated_by_id)
WHERE id = sqlc.arg(id) AND status = 'submitted'
RETURNING *;

-- Lê o departamento com trava compartilhada: uma exclusão ou mudança simultânea espera esta transação.
-- name: GetDepartmentForShare :one
SELECT id, name, is_finalistic FROM departments WHERE id = $1 FOR SHARE;

-- Revisores que podem decidir um enquadramento naquele departamento (ativos e não bloqueados).
-- name: CountActiveReviewers :one
SELECT count(*) FROM admins a
JOIN admin_users_roles ur ON ur.admin_id = a.id
JOIN admin_roles r ON r.id = ur.role_id
WHERE r.code = 'department-reviewer' AND a.department_id = $1 AND a.is_active AND NOT a.blocked;
