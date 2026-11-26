-- Processo do projeto no SIGADOC e acompanhamento da tramitação (migração 00041).

-- name: GetProjectSigadoc :one
SELECT * FROM project_sigadoc WHERE project_id = $1;

-- Grava o processo do projeto (uma vez: o número é imutável). O acompanhamento começa na hora.
-- name: CreateProjectSigadoc :one
INSERT INTO project_sigadoc (project_id, matricula, process_number, process_sigla, opened_by_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Reserva (por 5 minutos) os processos com consulta vencida, para só uma instância da API consultar cada um. A reserva é só o
-- adiamento de watch_next_at: se a API cair no meio, o processo volta a ser consultado quando a reserva vencer.
-- name: ClaimSigadocWatches :many
UPDATE project_sigadoc SET watch_next_at = now() + interval '5 minutes'
WHERE project_id IN (
    SELECT s.project_id FROM project_sigadoc s
    WHERE s.sigadoc_archived_at IS NULL AND s.watch_next_at <= now()
    ORDER BY s.watch_next_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- Resultado de uma consulta que deu certo.
-- name: FinishSigadocWatch :exec
UPDATE project_sigadoc
SET situation = sqlc.arg(situation), watch_interval_seconds = sqlc.arg(interval_seconds),
    watch_next_at = now() + make_interval(secs => sqlc.arg(interval_seconds)::int), watched_at = now(), watch_error = '',
    sigadoc_archived_at = CASE WHEN sqlc.arg(archived)::boolean THEN COALESCE(sigadoc_archived_at, now()) ELSE NULL END
WHERE project_id = sqlc.arg(project_id);

-- Resultado de uma consulta que falhou: tenta de novo mais tarde, com o intervalo maior.
-- name: FailSigadocWatch :exec
UPDATE project_sigadoc
SET watch_interval_seconds = sqlc.arg(interval_seconds), watch_next_at = now() + make_interval(secs => sqlc.arg(interval_seconds)::int),
    watched_at = now(), watch_error = sqlc.arg(error)
WHERE project_id = sqlc.arg(project_id);

-- Grava uma movimentação vista; devolve 1 se era nova.
-- name: InsertSigadocMovement :execrows
INSERT INTO sigadoc_movements (project_id, mov_id, type_id, event, document_description, lotacao, destination, person, performed_at,
                               performed_at_raw, after_project_archive)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (project_id, mov_id) DO NOTHING;

-- name: ListSigadocMovements :many
SELECT * FROM sigadoc_movements WHERE project_id = $1 ORDER BY mov_id DESC;

-- Marca como vistas as movimentações posteriores ao arquivamento; devolve quantas.
-- name: MarkSigadocMovementsSeen :execrows
UPDATE sigadoc_movements SET seen_at = now()
WHERE project_id = $1 AND after_project_archive AND seen_at IS NULL;
