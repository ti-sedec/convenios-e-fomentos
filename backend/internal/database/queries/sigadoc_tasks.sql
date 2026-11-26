-- Montagem do processo no SIGADOC (migração 00042).

-- name: CreateSigadocTask :one
INSERT INTO sigadoc_tasks (project_id, started_by_id, matricula, status, attempt)
VALUES ($1, $2, $3, 'running', COALESCE((SELECT max(attempt) FROM sigadoc_tasks WHERE project_id = $1), 0) + 1)
RETURNING *;

-- name: GetSigadocTask :one
SELECT * FROM sigadoc_tasks WHERE id = $1;

-- name: GetLatestSigadocTask :one
SELECT * FROM sigadoc_tasks WHERE project_id = $1 ORDER BY started_at DESC, attempt DESC LIMIT 1;

-- Marca a execução ainda viva e atualiza o andamento.
-- name: TouchSigadocTask :exec
UPDATE sigadoc_tasks SET heartbeat_at = now(), progress_done = $2, progress_total = $3 WHERE id = $1 AND status = 'running';

-- name: FinishSigadocTask :exec
UPDATE sigadoc_tasks
SET status = sqlc.arg(status), error_code = sqlc.arg(error_code), error = sqlc.arg(error), progress_done = sqlc.arg(progress_done),
    progress_total = sqlc.arg(progress_total), finished_at = now()
WHERE id = sqlc.arg(id) AND status = 'running';

-- Reinício da API: as tarefas "em execução" perderam o PIN (que vivia só na memória) e ficam interrompidas.
-- name: InterruptRunningSigadocTasks :execrows
UPDATE sigadoc_tasks
SET status = 'interrupted', error_code = 'restart', error = 'A API foi reiniciada durante a montagem. Informe o PIN de novo para retomar de onde parou.', finished_at = now()
WHERE status = 'running';

-- name: UpsertSigadocItem :exec
INSERT INTO sigadoc_items (project_id, key, seq, kind, label, description, file_id, part, parts, sha256, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (project_id, key) DO NOTHING;

-- name: ListSigadocItems :many
SELECT * FROM sigadoc_items WHERE project_id = $1 ORDER BY seq, part;

-- name: SetSigadocItemState :exec
UPDATE sigadoc_items SET status = $3, sigla = $4, error = $5, updated_at = now() WHERE project_id = $1 AND key = $2;

-- name: SetSigadocItemParts :exec
UPDATE sigadoc_items SET parts = $3, sha256 = $4, description = $5, updated_at = now() WHERE project_id = $1 AND key = $2;

-- Conclui a abertura: o processo está pronto no SIGADOC e o projeto passa a aguardar o parecer. Só vale na etapa de abertura.
-- name: CompleteProjectOpening :one
UPDATE projects SET stage = 'awaiting_opinion', updated_at = now(), updated_by_id = sqlc.arg(updated_by_id)
WHERE id = sqlc.arg(id) AND status = 'submitted' AND stage = 'opening_sigadoc'
RETURNING *;
