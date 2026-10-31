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

-- Os nomes são resolvidos agora (como em audit.sql): vazios se o admin foi excluído.
-- name: ListStageHistory :many
SELECT h.id, h.seq, h.status, h.stage, h.framing_status, h.holder_type, h.holder_admin_id, h.holder_department_id,
       h.holder_department_name, h.entered_at, h.left_at, h.entered_by_type, h.entered_by_id, h.transition, h.audit_log_id, h.source,
       COALESCE((SELECT NULLIF(trim(a.first_name || ' ' || a.last_name), '') FROM admins a WHERE a.id = h.holder_admin_id), '')::text AS holder_admin_name,
       COALESCE((SELECT NULLIF(trim(a.first_name || ' ' || a.last_name), '') FROM admins a WHERE h.entered_by_type = 'admin' AND a.id = h.entered_by_id), '')::text AS entered_by_name
FROM project_stage_history h
WHERE h.project_id = $1
ORDER BY h.seq;
