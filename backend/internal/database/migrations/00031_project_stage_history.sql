-- +goose Up

-- Histórico de etapas: um trecho de tempo por linha, em que o projeto esteve numa combinação estável de situação, etapa,
-- enquadramento, departamento e analista. É a fonte de verdade para "quanto tempo em cada etapa" e "com quem estava"; o
-- audit_logs continua sendo a trilha de "quem fez o quê" (audit_log_id liga os dois).
-- Os ids de admin e de departamento não têm chave estrangeira de propósito: a tabela é imutável e uma exclusão (ON DELETE SET NULL)
-- seria uma alteração. O nome do departamento é copiado, como em project_framings.
CREATE TABLE project_stage_history (
    id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id             uuid NOT NULL REFERENCES projects (id),
    seq                    integer NOT NULL,
    status                 text NOT NULL CHECK (status IN ('submitted', 'archived')),
    stage                  text NOT NULL CHECK (stage IN ('awaiting_checklist', 'in_analysis', 'awaiting_proponent', 'awaiting_opinion')),
    framing_status         text CHECK (framing_status IN ('pending', 'confirmed')),
    holder_type            text NOT NULL CHECK (holder_type IN ('team', 'analyst', 'department', 'proponent', 'none')),
    holder_admin_id        uuid,
    holder_department_id   uuid,
    holder_department_name text,
    entered_at             timestamptz NOT NULL,
    left_at                timestamptz,
    entered_by_type        text NOT NULL CHECK (entered_by_type IN ('admin', 'end_user', 'api_token', 'system')),
    entered_by_id          uuid,
    transition             text NOT NULL,
    audit_log_id           uuid REFERENCES audit_logs (id),
    source                 text NOT NULL CHECK (source IN ('live', 'initial')),
    UNIQUE (project_id, seq),
    CONSTRAINT project_stage_history_period CHECK (left_at IS NULL OR left_at >= entered_at),
    CONSTRAINT project_stage_history_holder CHECK (
        (holder_type = 'analyst') = (holder_admin_id IS NOT NULL)
        AND (holder_type = 'department') = (holder_department_id IS NOT NULL AND holder_department_name IS NOT NULL)
        AND (holder_type = 'none') = (status = 'archived')
    )
);

-- No máximo um trecho aberto por projeto.
CREATE UNIQUE INDEX project_stage_history_open_key ON project_stage_history (project_id) WHERE left_at IS NULL;
CREATE INDEX project_stage_history_stage_idx ON project_stage_history (stage, entered_at);
CREATE INDEX project_stage_history_analyst_idx ON project_stage_history (holder_admin_id, entered_at) WHERE holder_admin_id IS NOT NULL;

-- Imutável, como project_versions: só se fecha um trecho aberto (left_at passa de nulo para um valor, uma única vez).
-- +goose StatementBegin
CREATE FUNCTION project_stage_history_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'project_stage_history é imutável' USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.left_at IS NOT NULL OR NEW.left_at IS NULL OR (to_jsonb(NEW) - 'left_at') <> (to_jsonb(OLD) - 'left_at') THEN
        RAISE EXCEPTION 'project_stage_history é imutável: só se fecha um trecho aberto' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER project_stage_history_guard BEFORE UPDATE OR DELETE ON project_stage_history
    FOR EACH ROW EXECUTE FUNCTION project_stage_history_guard();

-- Projetos que já existiam: um trecho estimado com o estado atual (não se reconstrói o passado). Mantém a regra de que todo projeto
-- enviado tem um trecho aberto igual ao estado atual. Rascunho não tem histórico.
INSERT INTO project_stage_history (project_id, seq, status, stage, framing_status, holder_type, holder_admin_id, holder_department_id,
                                   holder_department_name, entered_at, entered_by_type, transition, source)
SELECT p.id, 1, p.status, p.stage, p.framing_status, h.holder_type,
       CASE WHEN h.holder_type = 'analyst' THEN p.analyst_id END,
       CASE WHEN h.holder_type = 'department' THEN p.department_id END,
       CASE WHEN h.holder_type = 'department' THEN (SELECT d.name FROM departments d WHERE d.id = p.department_id) END,
       CASE WHEN p.status = 'archived' THEN COALESCE(p.archived_at, p.updated_at)
            WHEN p.stage = 'awaiting_checklist' THEN p.submitted_at
            ELSE p.updated_at END,
       'system', 'initial', 'initial'
FROM projects p
CROSS JOIN LATERAL (SELECT CASE
        WHEN p.status = 'archived' THEN 'none'
        WHEN p.stage = 'awaiting_proponent' THEN 'proponent'
        WHEN p.stage = 'awaiting_opinion' OR p.framing_status = 'pending' THEN 'department'
        WHEN p.stage = 'in_analysis' AND p.analyst_id IS NOT NULL THEN 'analyst'
        ELSE 'team' END AS holder_type) h
WHERE p.status <> 'draft';

-- +goose Down
DROP TRIGGER project_stage_history_guard ON project_stage_history;
DROP FUNCTION project_stage_history_guard();
DROP TABLE project_stage_history;
