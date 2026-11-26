-- +goose Up

-- Processo do projeto no SIGADOC e o acompanhamento da tramitação (docs/planejamento-integracao-sigadoc.md, seções 5 e 6.4).
-- Uma linha por projeto, criada quando o número do processo é gravado (a montagem do processo faz isso; fase F4).
CREATE TABLE project_sigadoc (
    project_id             uuid PRIMARY KEY REFERENCES projects (id) ON DELETE CASCADE,
    -- Matrícula do analista que abriu o processo: o acompanhamento consulta o SIGADOC com o token dela.
    matricula              text NOT NULL CHECK (matricula <> ''),
    -- Número e sigla do processo: únicos e imutáveis depois de gravados.
    process_number         text NOT NULL UNIQUE CHECK (process_number <> ''),
    process_sigla          text NOT NULL CHECK (process_sigla <> ''),
    opened_by_id           uuid REFERENCES admins (id) ON DELETE SET NULL,
    opened_at              timestamptz NOT NULL DEFAULT now(),
    -- Quando o acompanhamento viu o processo arquivado no SIGADOC; a partir daí não se consulta mais.
    sigadoc_archived_at    timestamptz,
    -- Última situação informada pelo SIGADOC (texto livre dele).
    situation              text NOT NULL DEFAULT '',
    -- Agendamento da próxima consulta. O intervalo cresce enquanto não há novidade e volta ao mínimo quando há.
    watch_next_at          timestamptz NOT NULL DEFAULT now(),
    watch_interval_seconds integer NOT NULL DEFAULT 900 CHECK (watch_interval_seconds > 0),
    watched_at             timestamptz,
    -- Mensagem curta do último erro de consulta (vazia quando deu certo). Nunca leva PIN nem corpo de resposta.
    watch_error            text NOT NULL DEFAULT ''
);

CREATE INDEX project_sigadoc_watch_idx ON project_sigadoc (watch_next_at) WHERE sigadoc_archived_at IS NULL;

-- O número e a sigla do processo não mudam depois de gravados.
-- +goose StatementBegin
CREATE FUNCTION project_sigadoc_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.process_number IS DISTINCT FROM OLD.process_number OR NEW.process_sigla IS DISTINCT FROM OLD.process_sigla
       OR NEW.project_id IS DISTINCT FROM OLD.project_id THEN
        RAISE EXCEPTION 'o número do processo do SIGADOC é imutável' USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER project_sigadoc_immutable BEFORE UPDATE ON project_sigadoc
    FOR EACH ROW EXECUTE FUNCTION project_sigadoc_immutable();

-- Movimentações do processo vistas no SIGADOC: o histórico do que aconteceu lá. Só leitura para o sistema.
CREATE TABLE sigadoc_movements (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id            uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    -- Id da movimentação no SIGADOC: a chave que impede gravar a mesma duas vezes.
    mov_id                bigint NOT NULL,
    type_id               integer NOT NULL DEFAULT 0,
    -- Evento (ex.: "Juntada") e a descrição do documento tramitado, como o SIGADOC informa.
    event                 text NOT NULL,
    document_description  text NOT NULL DEFAULT '',
    lotacao               text NOT NULL DEFAULT '',
    destination           text NOT NULL DEFAULT '',
    person                text NOT NULL DEFAULT '',
    -- Data da movimentação; nula se o SIGADOC mandou um formato que não se reconhece (então vale performed_at_raw).
    performed_at          timestamptz,
    performed_at_raw      text NOT NULL DEFAULT '',
    captured_at           timestamptz NOT NULL DEFAULT now(),
    -- Aconteceu depois de o projeto ser arquivado no sistema: aparece em destaque.
    after_project_archive boolean NOT NULL DEFAULT false,
    -- Quando a equipe marcou como vista (só importa para as posteriores ao arquivamento).
    seen_at               timestamptz,
    UNIQUE (project_id, mov_id)
);

CREATE INDEX sigadoc_movements_project_idx ON sigadoc_movements (project_id, mov_id DESC);

-- +goose Down
DROP TABLE sigadoc_movements;
DROP TRIGGER project_sigadoc_immutable ON project_sigadoc;
DROP FUNCTION project_sigadoc_immutable();
DROP TABLE project_sigadoc;
