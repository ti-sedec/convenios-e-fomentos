-- Assume a análise: "Aguardando análise de checklist" -> "Em análise", com o responsável. Não devolve linha se o projeto não está
-- enviado e aguardando a conferência (quem chama já travou a linha e conferiu o estado).
-- name: AssumeProjectAnalysis :one
UPDATE projects SET stage = 'in_analysis', analyst_id = sqlc.arg(analyst_id), version = version + 1, updated_at = now(), updated_by_id = sqlc.arg(analyst_id)
WHERE id = sqlc.arg(id) AND status = 'submitted' AND stage = 'awaiting_checklist'
RETURNING *;

-- name: ListItemReviews :many
SELECT r.kind, r.target, r.status, r.note, r.reviewed_at, COALESCE(NULLIF(trim(a.first_name || ' ' || a.last_name), ''), a.email::text, '')::text AS reviewed_by_name
FROM project_item_reviews r
LEFT JOIN admins a ON a.id = r.reviewed_by_id
WHERE r.project_id = $1;

-- name: ApproveItemReview :exec
INSERT INTO project_item_reviews (project_id, kind, target, status, reviewed_by_id, reviewed_at)
VALUES (sqlc.arg(project_id), sqlc.arg(kind), sqlc.arg(target), 'approved', sqlc.arg(reviewed_by_id), now())
ON CONFLICT (project_id, kind, target)
DO UPDATE SET status = 'approved', note = NULL, reviewed_by_id = EXCLUDED.reviewed_by_id, reviewed_at = now();

-- Reprova o item, com a justificativa (obrigatória: a tabela confere).
-- name: RejectItemReview :exec
INSERT INTO project_item_reviews (project_id, kind, target, status, note, reviewed_by_id, reviewed_at)
VALUES (sqlc.arg(project_id), sqlc.arg(kind), sqlc.arg(target), 'rejected', sqlc.arg(note), sqlc.arg(reviewed_by_id), now())
ON CONFLICT (project_id, kind, target)
DO UPDATE SET status = 'rejected', note = EXCLUDED.note, reviewed_by_id = EXCLUDED.reviewed_by_id, reviewed_at = now();

-- Volta o item a pendente (desfazer a decisão, ou devolver ao proponente).
-- name: ResetItemReview :exec
INSERT INTO project_item_reviews (project_id, kind, target, status)
VALUES ($1, $2, $3, 'pending')
ON CONFLICT (project_id, kind, target)
DO UPDATE SET status = 'pending', note = NULL, reviewed_by_id = NULL, reviewed_at = NULL;

-- name: RecordFileView :exec
INSERT INTO project_file_views (file_id, admin_id) VALUES ($1, $2) ON CONFLICT (file_id, admin_id) DO NOTHING;

-- O administrador chegou ao fim do arquivo (e, por consequência, também o viu). Mantém a data da primeira leitura completa.
-- name: RecordFileRead :exec
INSERT INTO project_file_views (file_id, admin_id, read_at) VALUES ($1, $2, now())
ON CONFLICT (file_id, admin_id) DO UPDATE SET read_at = COALESCE(project_file_views.read_at, now());

-- Arquivos do projeto que este administrador já viu, e se leu cada um até o fim.
-- name: ListFileViewsOfAdmin :many
SELECT v.file_id, (v.read_at IS NOT NULL)::boolean AS read
FROM project_file_views v
JOIN project_files f ON f.id = v.file_id
JOIN project_document_items i ON i.id = f.item_id
WHERE i.project_id = $1 AND v.admin_id = $2;

-- E-mail do administrador, se ele pode receber avisos (ativo e não bloqueado).
-- name: GetNotifiableAdminEmailByID :one
SELECT email::text FROM admins WHERE id = $1 AND is_active AND NOT blocked;

-- Projeto já em análise mas sem responsável (voltou de uma complementação pedida antes de alguém assumir): o primeiro a assumir vira
-- o responsável. Não mexe na etapa nem na versão.
-- name: ClaimProjectAnalyst :one
UPDATE projects SET analyst_id = sqlc.arg(analyst_id), updated_at = now(), updated_by_id = sqlc.arg(analyst_id)
WHERE id = sqlc.arg(id) AND status = 'submitted' AND stage = 'in_analysis' AND analyst_id IS NULL
RETURNING *;

-- Grava o analista responsável de um projeto enviado e ativo (primeiro responsável ou troca de responsável), sem mexer em etapa nem
-- versão. Quem chama já travou a linha e conferiu a regra; precisa passar por Service.transition.
-- name: SetProjectAnalyst :one
UPDATE projects SET analyst_id = sqlc.arg(analyst_id), updated_at = now(), updated_by_id = sqlc.arg(analyst_id)
WHERE id = sqlc.arg(id) AND status = 'submitted'
RETURNING *;
