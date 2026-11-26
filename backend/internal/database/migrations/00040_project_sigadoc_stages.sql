-- +goose Up

-- Depois do enquadramento o projeto não vai direto para o parecer: volta à fila dos analistas ("Aguardando analista"), um analista o
-- assume ("Abertura do processo no SIGADOC") e só ao concluir a montagem do processo no SIGADOC ele fica "Aguardando parecer"
-- (docs/planejamento-integracao-sigadoc.md, seção 4). Duas etapas novas em projects e no histórico de etapas.
ALTER TABLE projects DROP CONSTRAINT projects_stage_check;
ALTER TABLE projects ADD CONSTRAINT projects_stage_check
    CHECK (stage IN ('awaiting_checklist', 'in_analysis', 'awaiting_proponent', 'awaiting_analyst', 'opening_sigadoc', 'awaiting_opinion'));

ALTER TABLE project_stage_history DROP CONSTRAINT project_stage_history_stage_check;
ALTER TABLE project_stage_history ADD CONSTRAINT project_stage_history_stage_check
    CHECK (stage IN ('awaiting_checklist', 'in_analysis', 'awaiting_proponent', 'awaiting_analyst', 'opening_sigadoc', 'awaiting_opinion'));

-- Com quem o projeto está (a mesma regra de project/history.go, holderOf): "Abertura do processo no SIGADOC" é do analista
-- responsável, como "Em análise"; "Aguardando analista" é da equipe (sem responsável).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_holder_type(p_status text, p_stage text, p_framing_status text, p_analyst_id uuid) RETURNS text AS $$
    SELECT CASE
        WHEN p_status = 'archived' THEN 'none'
        WHEN p_stage = 'awaiting_proponent' THEN 'proponent'
        WHEN p_stage = 'awaiting_opinion' OR p_framing_status = 'pending' THEN 'department'
        WHEN p_stage IN ('in_analysis', 'opening_sigadoc') AND p_analyst_id IS NOT NULL THEN 'analyst'
        ELSE 'team' END
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

-- +goose Down

-- Falha se houver projeto (ou trecho do histórico, que é imutável) nas etapas novas: não se desce com dado que não cabe.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_holder_type(p_status text, p_stage text, p_framing_status text, p_analyst_id uuid) RETURNS text AS $$
    SELECT CASE
        WHEN p_status = 'archived' THEN 'none'
        WHEN p_stage = 'awaiting_proponent' THEN 'proponent'
        WHEN p_stage = 'awaiting_opinion' OR p_framing_status = 'pending' THEN 'department'
        WHEN p_stage = 'in_analysis' AND p_analyst_id IS NOT NULL THEN 'analyst'
        ELSE 'team' END
$$ LANGUAGE sql IMMUTABLE;
-- +goose StatementEnd

ALTER TABLE project_stage_history DROP CONSTRAINT project_stage_history_stage_check;
ALTER TABLE project_stage_history ADD CONSTRAINT project_stage_history_stage_check
    CHECK (stage IN ('awaiting_checklist', 'in_analysis', 'awaiting_proponent', 'awaiting_opinion'));
ALTER TABLE projects DROP CONSTRAINT projects_stage_check;
ALTER TABLE projects ADD CONSTRAINT projects_stage_check
    CHECK (stage IN ('awaiting_checklist', 'in_analysis', 'awaiting_proponent', 'awaiting_opinion'));
