-- name: LockProjectFiles :exec
UPDATE project_files f SET locked = true
FROM project_document_items i
WHERE f.item_id = i.id AND i.project_id = $1 AND f.status = 'confirmed' AND f.superseded_at IS NULL;

-- name: SupersedeProjectFile :execrows
UPDATE project_files SET superseded_at = now() WHERE id = $1 AND superseded_at IS NULL;

-- name: NextProjectVersionNumber :one
SELECT COALESCE(max(number), 0) + 1 FROM project_versions WHERE project_id = $1;

-- name: SetProjectStage :one
UPDATE projects SET stage = $2, version = version + 1, updated_at = now(), updated_by_id = $3
WHERE id = $1 AND status = 'submitted'
RETURNING *;

-- name: ArchiveProject :one
UPDATE projects SET status = 'archived', archived_at = now(), archived_by_id = $2, archive_reason = $3, archive_note = $4,
    version = version + 1, updated_at = now(), updated_by_id = $2
WHERE id = $1 AND status = 'submitted'
RETURNING *;

-- name: NextComplementNumber :one
SELECT COALESCE(max(number), 0) + 1 FROM project_complement_requests WHERE project_id = $1;

-- name: CreateComplementRequest :one
INSERT INTO project_complement_requests (project_id, number, requested_by_id, due_at, message)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: AddComplementItem :exec
INSERT INTO project_complement_items (request_id, kind, target, note) VALUES ($1, $2, $3, $4);

-- name: ListComplementItems :many
SELECT * FROM project_complement_items WHERE request_id = $1 ORDER BY kind DESC, target;

-- O pedido aberto do projeto, se houver.
-- name: GetOpenComplementRequest :one
SELECT * FROM project_complement_requests WHERE project_id = $1 AND status = 'open';

-- O pedido aberto ou, não havendo, o mais recente.
-- name: GetLatestComplementRequest :one
SELECT * FROM project_complement_requests WHERE project_id = $1 ORDER BY number DESC LIMIT 1;

-- name: MarkComplementItemAnswered :exec
UPDATE project_complement_items i SET answered_at = COALESCE(i.answered_at, now())
FROM project_complement_requests r
WHERE i.request_id = r.id AND r.project_id = $1 AND r.status = 'open' AND i.kind = $2 AND i.target = $3;

-- name: AnswerComplementRequest :exec
UPDATE project_complement_requests SET status = 'answered', answered_at = now(), answered_version = $2 WHERE id = $1 AND status = 'open';

-- name: CloseOpenComplementRequests :exec
UPDATE project_complement_requests SET status = $2 WHERE project_id = $1 AND status = 'open';

-- Pedidos abertos com o prazo vencido. SKIP LOCKED: várias instâncias da API podem rodar a rotina ao mesmo tempo.
-- name: LockDueComplementRequests :many
SELECT id, project_id FROM project_complement_requests
WHERE status = 'open' AND due_at < now()
ORDER BY due_at
LIMIT 100
FOR UPDATE SKIP LOCKED;

-- name: GetComplementRequestForUpdate :one
SELECT * FROM project_complement_requests WHERE id = $1 FOR UPDATE;

-- Cópia de um projeto para um novo rascunho do mesmo proponente. Não leva identificação, envio, arquivamento nem a área.
-- name: DuplicateProject :one
INSERT INTO projects (
    created_by_id, updated_by_id, origin_project_id, proponent_name, proponent_cpf,
    proponent_phones, proponent_emails, proponent_uf, proponent_municipality_ibge, proponent_job_title,
    is_legal_representative, legal_rep_name, legal_rep_cpf, legal_rep_job_title, proponent_personal, legal_rep_personal,
    instrument, institution_cnpj, institution_name, entity_type_id, entity_type_other, entity_nature, entity_non_profit,
    institution_uf, institution_municipality_ibge, institution_zip, institution_street, institution_number, institution_complement,
    institution_district, institution_email, institution_phone, habilitation_status, habilitation_number, habilitation_valid_until,
    sigcon_number, sigcon_registered_on, title, object_description, justification, object_nature,
    has_artistic_hiring, calamity,
    funding_type, own_amount_cents, amendment_amount_cents, total_amount_cents, counterpart_kind, counterpart_cents, counterpart_description,
    indirect_costs_cents, tender_status, tender_notice_number, tender_approved_on, tender_legal_basis, tender_justification,
    artist_fees_cents, regional_artist_fees_cents, governor_waiver, price_oldest_collected_on,
    exclusive_supplier, public_space_use, reduced_deadline)
SELECT
    $2, $2, p.id, $3, $4,
    p.proponent_phones, p.proponent_emails, p.proponent_uf, p.proponent_municipality_ibge, p.proponent_job_title,
    p.is_legal_representative, p.legal_rep_name, p.legal_rep_cpf, p.legal_rep_job_title, p.proponent_personal, p.legal_rep_personal,
    p.instrument, p.institution_cnpj, p.institution_name, p.entity_type_id, p.entity_type_other, p.entity_nature, p.entity_non_profit,
    p.institution_uf, p.institution_municipality_ibge, p.institution_zip, p.institution_street, p.institution_number, p.institution_complement,
    p.institution_district, p.institution_email, p.institution_phone, p.habilitation_status, p.habilitation_number, p.habilitation_valid_until,
    p.sigcon_number, p.sigcon_registered_on, p.title, p.object_description, p.justification, p.object_nature,
    p.has_artistic_hiring, p.calamity,
    p.funding_type, p.own_amount_cents, p.amendment_amount_cents, p.total_amount_cents, p.counterpart_kind, p.counterpart_cents, p.counterpart_description,
    p.indirect_costs_cents, p.tender_status, p.tender_notice_number, p.tender_approved_on, p.tender_legal_basis, p.tender_justification,
    p.artist_fees_cents, p.regional_artist_fees_cents, p.governor_waiver, p.price_oldest_collected_on,
    p.exclusive_supplier, p.public_space_use, p.reduced_deadline
FROM projects p WHERE p.id = $1
RETURNING *;

-- name: CopyProjectExecutionPeriods :exec
INSERT INTO project_execution_periods (project_id, position, start_date, end_date, municipality_ibge, place_detail)
SELECT sqlc.arg('new_project_id')::uuid, e.position, e.start_date, e.end_date, e.municipality_ibge, e.place_detail
FROM project_execution_periods e WHERE e.project_id = sqlc.arg('source_project_id')::uuid;

-- name: CopyProjectAmendment :exec
INSERT INTO project_amendments (id, project_id, position, parliamentarian_id, parliamentarian_free_name, amendment_number, year, amount_cents)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: CreateProjectDocumentItemCopy :one
INSERT INTO project_document_items (project_id, requirement_code, amendment_id, justification, title, description)
VALUES ($1, sqlc.narg('requirement_code')::text, sqlc.narg('amendment_id')::uuid, $2, $3, $4)
RETURNING *;

-- Rascunhos sem nenhuma alteração há mais de N dias (retenção). Só rascunhos: projeto enviado nunca é apagado por tempo.
-- name: ListAbandonedDrafts :many
SELECT id FROM projects
WHERE status = 'draft' AND updated_at < now() - make_interval(days => @older_than_days::int)
ORDER BY updated_at
LIMIT 200;
