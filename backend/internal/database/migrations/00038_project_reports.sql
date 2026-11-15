-- +goose Up

-- Relatórios do projeto (PDF). Só o REGISTRO fica guardado: quem gerou, quando, o tipo, as opções e os dados usados naquele momento
-- (sem os documentos anexados). O PDF não é guardado: o relatório resumido é refeito a partir de `data`, então sai sempre igual.
-- Sem prazo de retenção (planejamento-relatorio-projeto.md, D5 e D6).
CREATE TABLE project_reports (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    kind              text NOT NULL CHECK (kind IN ('summary', 'full')),
    -- Número da versão enviada (project_versions.number) de onde vieram os dados.
    version_number    integer NOT NULL,
    page_numbers      boolean NOT NULL DEFAULT true,
    cover_sheets      boolean NOT NULL DEFAULT false,
    generated_by_id   uuid REFERENCES admins (id) ON DELETE SET NULL,
    -- Nome e CPF (mascarado) copiados: o histórico sobrevive a mudanças e exclusões da conta.
    generated_by_name text NOT NULL,
    generated_by_cpf  text NOT NULL DEFAULT '',
    generated_at      timestamptz NOT NULL,
    page_count        integer NOT NULL CHECK (page_count > 0),
    size_bytes        bigint NOT NULL CHECK (size_bytes > 0),
    -- Hash do PDF gerado na ocasião.
    sha256            text NOT NULL,
    -- Os dados que alimentaram o relatório (ReportData), com CPFs mascarados.
    data              jsonb NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX project_reports_project_idx ON project_reports (project_id, generated_at DESC);

-- Quem pode gerar: o mesmo alcance da leitura do projeto (o titular e os membros, no caso do proponente; a equipe, só em projeto
-- enviado ou arquivado; o Revisor, só do próprio departamento). A regra do enquadramento confirmado é conferida pelo serviço.
INSERT INTO admin_permissions (role_id, action, subject, conditions)
SELECT r.id, d.action, 'project', d.conditions::jsonb
FROM (VALUES
    ('proponent', 'admin::projects.report', '["admin::is-creator","admin::project-member"]'),
    ('analyst', 'admin::projects.report', '["admin::project-submitted"]'),
    ('administrative-supervisor', 'admin::projects.report', '["admin::project-submitted"]'),
    ('state-secretary', 'admin::projects.report', '["admin::project-submitted"]'),
    ('department-reviewer', 'admin::projects.report', '["admin::project-submitted-same-department"]')
) AS d (role_code, action, conditions)
JOIN admin_roles r ON r.code = d.role_code
WHERE NOT EXISTS (SELECT 1 FROM admin_permissions p WHERE p.role_id = r.id AND p.action = d.action)
ON CONFLICT DO NOTHING;

-- O cache de permissões não tem prazo de validade (ver a 00024): avisa as instâncias em execução.
SELECT pg_notify('authz_invalidate', '[]');

-- +goose Down
DELETE FROM admin_permissions p USING admin_roles r
WHERE p.role_id = r.id AND p.action = 'admin::projects.report';
DROP TABLE project_reports;
SELECT pg_notify('authz_invalidate', '[]');
