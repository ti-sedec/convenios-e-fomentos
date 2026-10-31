-- +goose Up

-- A equipe também devolve documentos adicionais do proponente (alvo: id do documento adicional).
ALTER TABLE project_complement_items DROP CONSTRAINT project_complement_items_kind_check;
ALTER TABLE project_complement_items ADD CONSTRAINT project_complement_items_kind_check CHECK (kind IN ('section', 'document', 'extra'));

-- +goose Down
DELETE FROM project_complement_items WHERE kind = 'extra';
ALTER TABLE project_complement_items DROP CONSTRAINT project_complement_items_kind_check;
ALTER TABLE project_complement_items ADD CONSTRAINT project_complement_items_kind_check CHECK (kind IN ('section', 'document'));
