-- +goose Up

-- Rede de segurança do histórico de etapas (00031): ao confirmar a transação, todo projeto enviado ou arquivado precisa ter um
-- trecho aberto igual ao seu estado. Quem muda situação, etapa, enquadramento, departamento ou analista sem passar por
-- Service.transition (script, SQL manual, rotina nova que esqueceu) tem a transação recusada. A regra de "com quem está" é a mesma
-- de project/history.go (holderOf) e da migração 00031.

-- +goose StatementBegin
CREATE FUNCTION project_holder_type(p_status text, p_stage text, p_framing_status text, p_analyst_id uuid) RETURNS text AS $$
    SELECT CASE
        WHEN p_status = 'archived' THEN 'none'
        WHEN p_stage = 'awaiting_proponent' THEN 'proponent'
        WHEN p_stage = 'awaiting_opinion' OR p_framing_status = 'pending' THEN 'department'
        WHEN p_stage = 'in_analysis' AND p_analyst_id IS NOT NULL THEN 'analyst'
        ELSE 'team' END
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION project_stage_history_check() RETURNS trigger AS $$
DECLARE
    p projects%ROWTYPE;
    holder text;
BEGIN
    -- O estado ao confirmar (um UPDATE anterior da mesma transação pode ter mudado a linha de novo).
    SELECT * INTO p FROM projects WHERE id = NEW.id;
    IF NOT FOUND OR p.status = 'draft' THEN
        RETURN NULL;
    END IF;
    -- Exclusão de um admin zera o analista (ON DELETE SET NULL): não é uma transição de etapa, e o trecho aberto continua
    -- apontando para quem era o responsável.
    IF OLD.analyst_id IS NOT NULL AND p.analyst_id IS NULL AND OLD.status = p.status AND OLD.stage IS NOT DISTINCT FROM p.stage
       AND OLD.framing_status IS NOT DISTINCT FROM p.framing_status AND OLD.department_id IS NOT DISTINCT FROM p.department_id THEN
        RETURN NULL;
    END IF;
    holder := project_holder_type(p.status, p.stage, p.framing_status, p.analyst_id);
    IF NOT EXISTS (
        SELECT 1 FROM project_stage_history h
        WHERE h.project_id = p.id AND h.left_at IS NULL
          AND h.status = p.status AND h.stage = p.stage AND h.framing_status IS NOT DISTINCT FROM p.framing_status
          AND h.holder_type = holder
          AND h.holder_department_id IS NOT DISTINCT FROM (CASE WHEN holder = 'department' THEN p.department_id END)
          AND h.holder_admin_id IS NOT DISTINCT FROM (CASE WHEN holder = 'analyst' THEN p.analyst_id END)
    ) THEN
        RAISE EXCEPTION 'o projeto % mudou de estado sem um trecho no histórico de etapas', p.id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER projects_stage_history_check AFTER UPDATE ON projects
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (OLD.status IS DISTINCT FROM NEW.status OR OLD.stage IS DISTINCT FROM NEW.stage
          OR OLD.framing_status IS DISTINCT FROM NEW.framing_status OR OLD.department_id IS DISTINCT FROM NEW.department_id
          OR OLD.analyst_id IS DISTINCT FROM NEW.analyst_id)
    EXECUTE FUNCTION project_stage_history_check();

-- +goose Down
DROP TRIGGER projects_stage_history_check ON projects;
DROP FUNCTION project_stage_history_check();
DROP FUNCTION project_holder_type(text, text, text, uuid);
