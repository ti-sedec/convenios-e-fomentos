-- +goose Up

-- Remove o "portal público": contas locais de usuário final (cadastro, login com senha, confirmação de e-mail e redefinição de
-- senha), os tokens de API, o CRUD genérico de documentos (/api/documents) e o que só eles usavam. O login é exclusivamente pelo
-- MT Login e os convênios vivem em /api/admin/*; hml e produção tinham essas tabelas vazias quando isto foi feito.
DROP TABLE document_files;
DROP TABLE documents;
DROP TABLE api_tokens;
DROP TABLE email_templates;
DROP TABLE end_user_sessions;
DROP TABLE end_user_permissions;
DROP TABLE end_users;
DROP TABLE end_user_roles;
DROP TABLE one_time_tokens;
DROP TABLE settings;

-- As roles de administrador perdem as permissões das ações que deixaram de existir.
DELETE FROM admin_permissions
WHERE action LIKE 'admin::content.document.%'
   OR action LIKE 'admin::settings.api-tokens.%'
   OR action LIKE 'admin::settings.email-templates.%'
   OR action LIKE 'admin::plugin.enduser-%';

-- Editor e Author existiam só para o CRUD de documentos. Saem se ninguém as usa; se alguém usa, ficam para o administrador decidir.
DELETE FROM admin_roles r
WHERE r.code IN ('editor', 'author')
  AND NOT EXISTS (SELECT 1 FROM admin_users_roles ur WHERE ur.role_id = r.id);

-- +goose Down
-- Recria só a ESTRUTURA das tabelas (as de hml e produção estavam vazias); dados apagados não voltam.
CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE one_time_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_type text NOT NULL,
    subject_id   uuid NOT NULL,
    purpose      text NOT NULL CHECK (purpose IN ('confirmation', 'reset', 'invite')),
    token_hash   text NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX one_time_tokens_subject_idx ON one_time_tokens (subject_type, subject_id, purpose);

CREATE TABLE end_user_roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- public, authenticated ou custom-<slug>
    type        text NOT NULL UNIQUE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE end_user_permissions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id    uuid NOT NULL REFERENCES end_user_roles (id) ON DELETE CASCADE,
    action     text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (role_id, action)
);
CREATE INDEX end_user_permissions_action_idx ON end_user_permissions (action);

-- O e-mail não é único no banco: "uma conta por e-mail" é uma configuração,
-- aplicada pela aplicação. password_hash é nulo para provedores externos (futuro).
CREATE TABLE end_users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username      citext NOT NULL UNIQUE,
    email         citext NOT NULL,
    password_hash text,
    confirmed     boolean NOT NULL DEFAULT false,
    blocked       boolean NOT NULL DEFAULT false,
    role_id       uuid NOT NULL REFERENCES end_user_roles (id) ON DELETE RESTRICT,
    provider      text NOT NULL DEFAULT 'local',
    -- campos extras aceitos no cadastro (register.allowedFields)
    profile       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX end_users_email_idx ON end_users (email);
CREATE INDEX end_users_role_idx ON end_users (role_id);

CREATE TABLE end_user_sessions (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    end_user_id        uuid NOT NULL REFERENCES end_users (id) ON DELETE CASCADE,
    family_id          uuid NOT NULL,
    family_started_at  timestamptz NOT NULL DEFAULT now(),
    refresh_token_hash text NOT NULL UNIQUE,
    user_agent         text NOT NULL DEFAULT '',
    ip                 text NOT NULL DEFAULT '',
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_used_at       timestamptz NOT NULL DEFAULT now(),
    expires_at         timestamptz NOT NULL,
    revoked_at         timestamptz
);
CREATE INDEX end_user_sessions_user_idx ON end_user_sessions (end_user_id);
CREATE INDEX end_user_sessions_family_idx ON end_user_sessions (family_id);

CREATE TABLE email_templates (
    key          text PRIMARY KEY,
    sender_name  text NOT NULL DEFAULT '',
    sender_email text NOT NULL,
    reply_to     text NOT NULL DEFAULT '',
    subject      text NOT NULL,
    body_html    text NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    type          text NOT NULL CHECK (type IN ('read-only', 'full-access', 'custom')),
    -- HMAC-SHA256(API_TOKEN_SALT, token) em hexadecimal; o token em claro nunca é guardado
    token_hash    text NOT NULL UNIQUE,
    -- últimos caracteres do token, só para o administrador reconhecê-lo na lista
    hint          text NOT NULL,
    -- ações api:: liberadas (somente para type = custom)
    permissions   jsonb NOT NULL DEFAULT '[]'::jsonb,
    -- 7, 30 ou 90; nulo = ilimitado. Usado também ao regenerar.
    lifespan_days integer CHECK (lifespan_days IN (7, 30, 90)),
    expires_at    timestamptz,
    last_used_at  timestamptz,
    created_by    uuid REFERENCES admins (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    revoked_at    timestamptz,
    CONSTRAINT api_tokens_permissions_array CHECK (jsonb_typeof(permissions) = 'array')
);
-- O nome é único entre os tokens não revogados.
CREATE UNIQUE INDEX api_tokens_name_active_idx ON api_tokens (lower(name)) WHERE revoked_at IS NULL;
CREATE INDEX api_tokens_created_by_idx ON api_tokens (created_by);

CREATE TABLE documents (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    titulo             text NOT NULL,
    conteudo           text NOT NULL DEFAULT '',
    status             text NOT NULL DEFAULT 'rascunho' CHECK (status IN ('rascunho', 'publicado')),
    -- nunca exposta na API pública
    observacao_interna text NOT NULL DEFAULT '',
    department_id      uuid REFERENCES departments (id) ON DELETE SET NULL,
    published_at       timestamptz,
    -- criador administrador (usado pelas condições is-creator e has-same-role-as-creator)
    created_by_id      uuid REFERENCES admins (id) ON DELETE SET NULL,
    updated_by_id      uuid REFERENCES admins (id) ON DELETE SET NULL,
    -- dono usuário final (usado pela policy is-owner na API pública)
    owner_user_id      uuid REFERENCES end_users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT documents_published_at_matches_status CHECK ((status = 'publicado') = (published_at IS NOT NULL))
);
CREATE INDEX documents_status_published_idx ON documents (status, published_at DESC);
CREATE INDEX documents_department_idx ON documents (department_id);
CREATE INDEX documents_created_by_idx ON documents (created_by_id);
CREATE INDEX documents_owner_idx ON documents (owner_user_id);
CREATE INDEX documents_created_at_idx ON documents (created_at DESC);

-- Arquivos anexados aos documentos. O conteúdo fica no armazenamento de objetos (MinIO); aqui ficam
-- só os metadados. A linha nasce "pendente" (envio autorizado) e vira "confirmado" quando a API
-- confere o objeto no armazenamento.
CREATE TABLE document_files (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id   uuid NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    -- gerada pelo servidor (nunca derivada do nome enviado pelo cliente)
    object_key    text NOT NULL UNIQUE,
    filename      text NOT NULL,
    content_type  text NOT NULL,
    size_bytes    bigint NOT NULL CHECK (size_bytes > 0),
    status        text NOT NULL DEFAULT 'pendente' CHECK (status IN ('pendente', 'confirmado')),
    created_by_id uuid REFERENCES admins (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    confirmed_at  timestamptz,
    CONSTRAINT document_files_confirmed_at_matches_status CHECK ((status = 'confirmado') = (confirmed_at IS NOT NULL))
);
CREATE INDEX document_files_document_idx ON document_files (document_id, created_at);
CREATE INDEX document_files_pending_idx ON document_files (created_at) WHERE status = 'pendente';
