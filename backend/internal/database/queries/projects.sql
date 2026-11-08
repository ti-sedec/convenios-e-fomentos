-- name: CreateProject :one
INSERT INTO projects (created_by_id, updated_by_id, proponent_name, proponent_cpf, proponent_phones, proponent_emails,
                      proponent_uf, proponent_municipality_ibge, proponent_job_title, proponent_personal)
VALUES ($1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: GetProjectForUpdate :one
SELECT * FROM projects WHERE id = $1 FOR UPDATE;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = $1;

-- name: UpdateProjectProponent :one
UPDATE projects SET
    proponent_phones = $2, proponent_emails = $3, proponent_uf = $4, proponent_municipality_ibge = $5,
    proponent_job_title = $6, is_legal_representative = $7, legal_rep_name = $8, legal_rep_cpf = $9,
    legal_rep_job_title = $10, proponent_personal = $12, legal_rep_personal = $13,
    version = version + 1, updated_at = now(), updated_by_id = $11
WHERE id = $1
RETURNING *;

-- name: UpdateProjectInstitution :one
UPDATE projects SET
    instrument = $2, institution_cnpj = $3, institution_name = $4, entity_type_id = $5, entity_type_other = $6,
    entity_nature = $7, entity_non_profit = $8, institution_uf = $9, institution_municipality_ibge = $10,
    institution_zip = $11, institution_street = $12, institution_number = $13, institution_complement = $14,
    institution_district = $15, institution_email = $16, institution_phone = $17, habilitation_status = $18,
    habilitation_number = $19, habilitation_valid_until = $20,
    version = version + 1, updated_at = now(), updated_by_id = $21
WHERE id = $1
RETURNING *;

-- name: UpdateProjectProposal :one
UPDATE projects SET
    sigcon_number = $2, sigcon_registered_on = $3, title = $4, object_description = $5, justification = $6,
    object_nature = $7, has_artistic_hiring = $8, calamity = $9,
    version = version + 1, updated_at = now(), updated_by_id = $10
WHERE id = $1
RETURNING *;

-- name: ListProjectExecutionPeriods :many
SELECT e.id, e.position, e.start_date, e.end_date, e.municipality_ibge, e.place_detail,
       m.name AS municipality_name, m.uf AS municipality_uf
FROM project_execution_periods e LEFT JOIN municipalities m ON m.ibge_code = e.municipality_ibge
WHERE e.project_id = $1
ORDER BY e.position;

-- name: ClearProjectExecutionPeriods :exec
DELETE FROM project_execution_periods WHERE project_id = $1;

-- name: AddProjectExecutionPeriod :exec
INSERT INTO project_execution_periods (project_id, position, start_date, end_date, municipality_ibge, place_detail)
VALUES ($1, $2, $3, $4, $5, $6);

-- Outros projetos (de qualquer usuário) que ainda não foram arquivados com o mesmo número do SIGCon.
-- name: CountOtherProjectsWithSigconNumber :one
SELECT count(*) FROM projects WHERE sigcon_number = $1 AND id <> $2 AND status <> 'archived';

-- name: ListExistingMunicipalityCodes :many
SELECT ibge_code FROM municipalities WHERE ibge_code = ANY(sqlc.arg('codes')::text[]) AND uf = sqlc.arg('uf')::text;

-- name: UpdateProjectFunding :one
UPDATE projects SET
    funding_type = $2, own_amount_cents = $3, amendment_amount_cents = $4, total_amount_cents = $5,
    counterpart_kind = $6, counterpart_cents = $7, counterpart_description = $8, indirect_costs_cents = $9,
    tender_status = $10, tender_notice_number = $11, tender_approved_on = $12, tender_legal_basis = $13,
    tender_justification = $14, artist_fees_cents = $15, regional_artist_fees_cents = $16, governor_waiver = $17,
    price_oldest_collected_on = $18, version = version + 1, updated_at = now(), updated_by_id = $19
WHERE id = $1
RETURNING *;

-- Normalização feita ao mudar a proposta/instituição: o que depende de outra escolha é zerado.
-- name: ResetProjectArtistFunding :exec
UPDATE projects SET artist_fees_cents = 0, regional_artist_fees_cents = 0, governor_waiver = false WHERE id = $1;

-- name: ResetProjectIndirectCosts :exec
UPDATE projects SET indirect_costs_cents = 0 WHERE id = $1;

-- name: ListProjectAmendments :many
SELECT a.id, a.parliamentarian_id, a.parliamentarian_free_name, COALESCE(p.name, '') AS parliamentarian_name,
       a.amendment_number, a.year, a.amount_cents
FROM project_amendments a LEFT JOIN parliamentarians p ON p.id = a.parliamentarian_id
WHERE a.project_id = $1
ORDER BY a.position;

-- name: ClearProjectAmendments :exec
DELETE FROM project_amendments WHERE project_id = $1;

-- name: AddProjectAmendment :exec
INSERT INTO project_amendments (project_id, position, parliamentarian_id, parliamentarian_free_name, amendment_number, year, amount_cents)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: NextProjectCodeNumber :one
INSERT INTO project_code_counters (year, last_number) VALUES ($1, 1)
ON CONFLICT (year) DO UPDATE SET last_number = project_code_counters.last_number + 1
RETURNING last_number;

-- name: SubmitProject :one
UPDATE projects SET status = 'submitted', stage = 'awaiting_checklist', code = $2, submitted_at = now(), submitted_by_id = $3,
    submission_ip = $4, late_submission = $5, version = version + 1, updated_at = now(), updated_by_id = $3
WHERE id = $1 AND status = 'draft'
RETURNING *;

-- name: CountOtherSubmittedProjectsWithSigconNumber :one
SELECT count(*) FROM projects WHERE sigcon_number = $1 AND id <> $2 AND status = 'submitted';

-- Envios de arquivo iniciados e nunca confirmados: descartados no envio do projeto, para o que foi enviado não mudar depois.
-- name: DeleteProjectPendingFiles :many
DELETE FROM project_files f USING project_document_items i
WHERE f.item_id = i.id AND i.project_id = $1 AND f.status = 'pending'
RETURNING f.object_key;

-- name: InsertProjectVersion :one
INSERT INTO project_versions (project_id, number, reason, submitted_at, submitted_by_id, submission_ip, snapshot)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProjectVersion :one
SELECT * FROM project_versions WHERE project_id = $1 AND number = $2;
