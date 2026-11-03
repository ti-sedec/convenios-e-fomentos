-- +goose Up

-- O registro de auditoria só recebe linhas novas: nada o altera nem o apaga (nem por TRUNCATE). O histórico de etapas aponta para
-- ele (project_stage_history.audit_log_id), então precisa ser confiável. Quem tem superusuário no banco ainda pode desativar o
-- trigger; a barreira vale para o código e para SQL comum.
-- +goose StatementBegin
CREATE FUNCTION audit_logs_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_logs é imutável' USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER audit_logs_no_change BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();
CREATE TRIGGER audit_logs_no_truncate BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION audit_logs_immutable();

-- +goose Down
DROP TRIGGER audit_logs_no_truncate ON audit_logs;
DROP TRIGGER audit_logs_no_change ON audit_logs;
DROP FUNCTION audit_logs_immutable();
