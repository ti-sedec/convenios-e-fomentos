-- name: ListProjectDeclarations :many
SELECT * FROM project_declarations WHERE project_id = $1 ORDER BY created_at, id;

-- A declaração ativa (aguardando ou assinada) da exigência, se houver.
-- name: GetActiveProjectDeclaration :one
SELECT * FROM project_declarations
WHERE project_id = $1 AND requirement_code = $2 AND status IN ('awaiting_signature', 'signed');

-- Trava a declaração ativa enquanto outra transação a troca ou a conclui.
-- name: GetActiveProjectDeclarationForUpdate :one
SELECT * FROM project_declarations
WHERE project_id = $1 AND requirement_code = $2 AND status IN ('awaiting_signature', 'signed')
FOR UPDATE;

-- name: GetProjectDeclaration :one
SELECT * FROM project_declarations WHERE id = $1;

-- name: InsertProjectDeclaration :one
INSERT INTO project_declarations (id, project_id, requirement_code, project_version, template_version, params_sha256, rendered_text,
    signer_name, signer_role, signer_cpf, accepted_by_id, accepted_by_cpf, accepted_ip, pdf_object_key, pdf_sha256, mtid_file_id, mtid_order_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
RETURNING *;

-- Só muda a declaração que ainda está no estado esperado: quem perde a corrida não sobrescreve o vencedor.
-- name: SetProjectDeclarationStatus :execrows
UPDATE project_declarations SET status = @status, updated_at = now()
WHERE id = @id AND status = @from_status;

-- name: SignProjectDeclaration :execrows
UPDATE project_declarations SET status = 'signed', signed_at = @signed_at, file_id = @file_id, updated_at = now()
WHERE id = @id AND status = 'awaiting_signature';

-- A última declaração emitida para a exigência, em qualquer situação.
-- name: GetLatestProjectDeclaration :one
SELECT * FROM project_declarations
WHERE project_id = $1 AND requirement_code = $2
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- Quem remove o arquivo assinado do checklist cancela a declaração, para ela poder ser emitida de novo.
-- name: CancelProjectDeclarationOfFile :execrows
UPDATE project_declarations SET status = 'cancelled', updated_at = now() WHERE file_id = $1 AND status = 'signed';

-- Declarações do projeto que aguardam a assinatura: a leitura do projeto ou do checklist consulta o MT-ID sobre elas.
-- name: ListAwaitingProjectDeclarations :many
SELECT * FROM project_declarations WHERE project_id = $1 AND status = 'awaiting_signature' ORDER BY created_at, id;
