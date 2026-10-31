-- Histórico de etapas (ver a migração 00031). Quem chama já travou a linha do projeto.

-- name: GetOpenStageHistory :one
SELECT * FROM project_stage_history WHERE project_id = $1 AND left_at IS NULL;

-- name: CloseStageHistory :exec
UPDATE project_stage_history SET left_at = now() WHERE id = $1 AND left_at IS NULL;

-- O trecho começa no instante da transação (o mesmo em que o anterior foi fechado). O nome do departamento é copiado.
-- name: InsertStageHistory :one
INSERT INTO project_stage_history (project_id, seq, status, stage, framing_status, holder_type, holder_admin_id, holder_department_id,
                                   holder_department_name, entered_at, entered_by_type, entered_by_id, transition, audit_log_id, source)
VALUES (sqlc.arg(project_id),
        (SELECT COALESCE(max(h.seq), 0) + 1 FROM project_stage_history h WHERE h.project_id = sqlc.arg(project_id)),
        sqlc.arg(status), sqlc.arg(stage), sqlc.narg(framing_status), sqlc.arg(holder_type), sqlc.narg(holder_admin_id),
        sqlc.narg(holder_department_id), (SELECT d.name FROM departments d WHERE d.id = sqlc.narg(holder_department_id)),
        now(), sqlc.arg(entered_by_type), sqlc.narg(entered_by_id), sqlc.arg(transition), sqlc.narg(audit_log_id), 'live')
RETURNING *;
