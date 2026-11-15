-- name: InsertProjectReport :one
INSERT INTO project_reports (project_id, kind, version_number, page_numbers, cover_sheets, generated_by_id, generated_by_name,
                             generated_by_cpf, generated_at, page_count, size_bytes, sha256, data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- Lista sem o campo `data` (pesado): o detalhe é lido por GetProjectReport.
-- name: ListProjectReports :many
SELECT id, project_id, kind, version_number, page_numbers, cover_sheets, generated_by_id, generated_by_name, generated_by_cpf,
       generated_at, page_count, size_bytes, sha256, created_at, status, started_at, finished_at, progress_done, progress_total,
       (object_key IS NOT NULL AND expires_at > now()) AS downloadable, expires_at, warnings, error, stats
FROM project_reports WHERE project_id = $1 ORDER BY generated_at DESC, id DESC;

-- name: GetProjectReport :one
SELECT * FROM project_reports WHERE id = $1 AND project_id = $2;

-- Relatório completo: entra na fila. O índice único impede duas montagens ao mesmo tempo no projeto (erro 23505).
-- name: InsertQueuedProjectReport :one
INSERT INTO project_reports (project_id, kind, version_number, page_numbers, cover_sheets, generated_by_id, generated_by_name,
                             generated_by_cpf, generated_at, data, status, requested_at)
VALUES ($1, 'full', $2, $3, $4, $5, $6, $7, $8, $9, 'queued', $8)
RETURNING *;

-- Pega a próxima da fila. Várias instâncias da API podem rodar: SKIP LOCKED garante que cada montagem é de uma só.
-- name: ClaimNextProjectReport :one
UPDATE project_reports SET status = 'running', started_at = now(), heartbeat_at = now(), progress_done = 0
WHERE id = (SELECT id FROM project_reports WHERE status = 'queued' ORDER BY requested_at, id FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING *;

-- name: HeartbeatProjectReport :exec
UPDATE project_reports SET heartbeat_at = now(), progress_done = $2, progress_total = $3 WHERE id = $1 AND status = 'running';

-- name: FinishProjectReport :exec
UPDATE project_reports SET status = 'done', finished_at = now(), heartbeat_at = now(), page_count = $2, size_bytes = $3, sha256 = $4,
    object_key = $5, expires_at = $6, warnings = $7, stats = $8, progress_done = progress_total, error = ''
WHERE id = $1;

-- name: FailProjectReport :exec
UPDATE project_reports SET status = 'failed', finished_at = now(), error = $2, warnings = $3, stats = $4 WHERE id = $1;

-- Montagens "em andamento" cujo processo morreu (reinício, falta de memória): não são refeitas sozinhas, para um arquivo que derruba
-- o processo não ficar em laço. A pessoa pede de novo.
-- name: FailStaleProjectReports :many
UPDATE project_reports SET status = 'failed', finished_at = now(), error = 'A montagem foi interrompida (reinício ou falta de memória do servidor). Gere o relatório de novo.'
WHERE status = 'running' AND heartbeat_at < $1
RETURNING id, project_id;

-- name: ListExpiredProjectReports :many
SELECT id, object_key FROM project_reports WHERE object_key IS NOT NULL AND expires_at <= now() LIMIT 50;

-- name: ClearProjectReportObject :exec
UPDATE project_reports SET object_key = NULL WHERE id = $1;
