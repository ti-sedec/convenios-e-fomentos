-- +goose Up

-- Montagem do processo do projeto no SIGADOC (docs/planejamento-integracao-sigadoc.md, seções 5 e 6.2). Uma tarefa por tentativa
-- (a primeira e cada retomada) e os itens por projeto, para a retomada pular o que já foi feito. NENHUMA coluna guarda PIN: ele vive
-- só na memória da tarefa em andamento.
CREATE TABLE sigadoc_tasks (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    started_by_id  uuid REFERENCES admins (id) ON DELETE SET NULL,
    -- Matrícula representada (a do analista que iniciou).
    matricula      text NOT NULL CHECK (matricula <> ''),
    -- running: em execução. interrupted: parou (falha, reinício da API ou item com problema); retoma-se com o PIN de novo.
    -- completed: tudo inserido e assinado. cancelled: o projeto foi arquivado ou o analista cancelou.
    status         text NOT NULL CHECK (status IN ('running', 'interrupted', 'completed', 'cancelled')),
    attempt        integer NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    progress_done  integer NOT NULL DEFAULT 0 CHECK (progress_done >= 0),
    progress_total integer NOT NULL DEFAULT 0 CHECK (progress_total >= 0),
    -- Código estável do motivo da parada (pin_rejected, sigadoc_error, item_failed, restart, project_archived...) e texto curto
    -- para a tela. Nunca levam PIN, URL, token nem corpo de resposta.
    error_code     text NOT NULL DEFAULT '',
    error          text NOT NULL DEFAULT '',
    started_at     timestamptz NOT NULL DEFAULT now(),
    heartbeat_at   timestamptz,
    finished_at    timestamptz
);

-- No máximo uma tarefa em execução por projeto.
CREATE UNIQUE INDEX sigadoc_tasks_running_key ON sigadoc_tasks (project_id) WHERE status = 'running';
CREATE INDEX sigadoc_tasks_project_idx ON sigadoc_tasks (project_id, started_at DESC);

-- Itens a inserir no processo: o Termo, o relatório e cada arquivo (ou parte de arquivo) do proponente. A chave identifica o item de
-- forma estável entre tentativas. O status guarda até onde cada item foi (um passo por vez, gravado logo depois de feito):
-- pending → creating (marcado ANTES de chamar o SIGADOC, para reconhecer um documento criado antes de uma queda) → created →
-- finalized → signed → joined (juntado ao processo; o Termo é "autuado"). failed: o item não pôde ser preparado (arquivo corrompido...).
CREATE TABLE sigadoc_items (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    key         text NOT NULL CHECK (key <> ''),
    seq         integer NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('termo', 'report', 'document')),
    label       text NOT NULL,
    -- Descrição enviada ao SIGADOC: é também o que se procura para reconhecer um documento já criado.
    description text NOT NULL,
    file_id     uuid,
    part        integer NOT NULL DEFAULT 1 CHECK (part >= 1),
    parts       integer NOT NULL DEFAULT 1 CHECK (parts >= 1),
    sha256      text NOT NULL DEFAULT '',
    -- Dados que a retomada precisa e não se refazem (os campos do Termo que o analista conferiu). Nunca PIN.
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    status      text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'creating', 'created', 'finalized', 'signed', 'joined', 'failed')),
    sigla       text NOT NULL DEFAULT '',
    error       text NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, key)
);

CREATE INDEX sigadoc_items_project_idx ON sigadoc_items (project_id, seq);

-- +goose Down
DROP TABLE sigadoc_items;
DROP TABLE sigadoc_tasks;
