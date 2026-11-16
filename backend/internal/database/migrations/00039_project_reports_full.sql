-- +goose Up

-- Relatório completo: monta-se em segundo plano (muitos arquivos). O registro ganha situação e andamento; o PDF pronto fica numa chave
-- temporária do armazenamento até `expires_at` (depois só o registro e os dados usados permanecem, planejamento D5).
ALTER TABLE project_reports
    ADD COLUMN status        text NOT NULL DEFAULT 'done' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    ADD COLUMN requested_at  timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN started_at    timestamptz,
    ADD COLUMN finished_at   timestamptz,
    ADD COLUMN heartbeat_at  timestamptz,
    ADD COLUMN progress_done  integer NOT NULL DEFAULT 0 CHECK (progress_done >= 0),
    ADD COLUMN progress_total integer NOT NULL DEFAULT 0 CHECK (progress_total >= 0),
    ADD COLUMN object_key    text,
    ADD COLUMN expires_at    timestamptz,
    -- Arquivos que não entraram (senha, corrompido, conversão recusada...): [{"file":"...","document":"...","reason":"..."}].
    ADD COLUMN warnings      jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN error         text NOT NULL DEFAULT '',
    -- Medições da montagem: pico de memória do processo, duração, arquivos, bytes de entrada.
    ADD COLUMN stats         jsonb NOT NULL DEFAULT '{}'::jsonb;

-- Enquanto está na fila ou montando, o PDF ainda não existe.
ALTER TABLE project_reports DROP CONSTRAINT project_reports_page_count_check;
ALTER TABLE project_reports DROP CONSTRAINT project_reports_size_bytes_check;
ALTER TABLE project_reports ADD CONSTRAINT project_reports_page_count_check CHECK (page_count >= 0);
ALTER TABLE project_reports ADD CONSTRAINT project_reports_size_bytes_check CHECK (size_bytes >= 0);
ALTER TABLE project_reports ALTER COLUMN page_count SET DEFAULT 0, ALTER COLUMN size_bytes SET DEFAULT 0, ALTER COLUMN sha256 SET DEFAULT '';

-- No máximo uma montagem em andamento por projeto.
CREATE UNIQUE INDEX project_reports_active_key ON project_reports (project_id) WHERE status IN ('queued', 'running');
CREATE INDEX project_reports_queue_idx ON project_reports (requested_at) WHERE status = 'queued';

-- +goose Down
DROP INDEX project_reports_queue_idx;
DROP INDEX project_reports_active_key;
DELETE FROM project_reports WHERE status <> 'done';
ALTER TABLE project_reports DROP CONSTRAINT project_reports_page_count_check;
ALTER TABLE project_reports DROP CONSTRAINT project_reports_size_bytes_check;
ALTER TABLE project_reports ADD CONSTRAINT project_reports_page_count_check CHECK (page_count > 0);
ALTER TABLE project_reports ADD CONSTRAINT project_reports_size_bytes_check CHECK (size_bytes > 0);
ALTER TABLE project_reports ALTER COLUMN page_count DROP DEFAULT, ALTER COLUMN size_bytes DROP DEFAULT, ALTER COLUMN sha256 DROP DEFAULT;
ALTER TABLE project_reports
    DROP COLUMN stats, DROP COLUMN error, DROP COLUMN warnings, DROP COLUMN expires_at, DROP COLUMN object_key,
    DROP COLUMN progress_total, DROP COLUMN progress_done, DROP COLUMN heartbeat_at, DROP COLUMN finished_at,
    DROP COLUMN started_at, DROP COLUMN requested_at, DROP COLUMN status;
