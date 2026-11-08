-- +goose Up

-- Períodos e locais de execução. Antes o projeto tinha UM par início/término, UM detalhe de local e uma lista solta de municípios.
-- Agora cada linha é {início, término, município, detalhe do local}, e o projeto pode ter várias. Os campos são anuláveis porque o
-- rascunho aceita dados incompletos; a obrigatoriedade é conferida no envio (pendências), não aqui.
CREATE TABLE project_execution_periods (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    position          integer NOT NULL,
    start_date        date,
    end_date          date,
    municipality_ibge text REFERENCES municipalities (ibge_code),
    place_detail      text NOT NULL DEFAULT '',
    CONSTRAINT project_execution_periods_period CHECK (start_date IS NULL OR end_date IS NULL OR end_date >= start_date)
);
CREATE INDEX project_execution_periods_project_idx ON project_execution_periods (project_id, position);

-- Dados existentes: cada município de execução vira uma linha com as datas do projeto; o detalhe do local, que valia para o projeto
-- todo, é copiado para todas as linhas. Projeto com datas ou detalhe e sem município vira uma linha sem município.
INSERT INTO project_execution_periods (project_id, position, start_date, end_date, municipality_ibge, place_detail)
SELECT p.id, (row_number() OVER (PARTITION BY p.id ORDER BY m.search_name, m.ibge_code) - 1)::integer,
       p.start_date, p.end_date, pm.ibge_code, p.execution_place_detail
FROM projects p
JOIN project_municipalities pm ON pm.project_id = p.id
JOIN municipalities m ON m.ibge_code = pm.ibge_code;

INSERT INTO project_execution_periods (project_id, position, start_date, end_date, municipality_ibge, place_detail)
SELECT p.id, 0, p.start_date, p.end_date, NULL, p.execution_place_detail
FROM projects p
WHERE (p.start_date IS NOT NULL OR p.end_date IS NOT NULL OR p.execution_place_detail <> '')
  AND NOT EXISTS (SELECT 1 FROM project_municipalities pm WHERE pm.project_id = p.id);

DROP TABLE project_municipalities;
ALTER TABLE projects
    DROP CONSTRAINT projects_period,
    DROP COLUMN start_date,
    DROP COLUMN end_date,
    DROP COLUMN execution_place_detail;

-- +goose Down

-- Volta ao modelo antigo e perde informação: o projeto fica com o menor início, o maior término e o primeiro detalhe de local
-- preenchido; as linhas viram uma lista de municípios distintos.
ALTER TABLE projects
    ADD COLUMN start_date date,
    ADD COLUMN end_date date,
    ADD COLUMN execution_place_detail text NOT NULL DEFAULT '',
    ADD CONSTRAINT projects_period CHECK (start_date IS NULL OR end_date IS NULL OR end_date >= start_date);

CREATE TABLE project_municipalities (
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    ibge_code  text NOT NULL REFERENCES municipalities (ibge_code),
    PRIMARY KEY (project_id, ibge_code)
);

INSERT INTO project_municipalities (project_id, ibge_code)
SELECT DISTINCT project_id, municipality_ibge FROM project_execution_periods WHERE municipality_ibge IS NOT NULL;

UPDATE projects p SET
    start_date = a.start_date,
    end_date = a.end_date,
    execution_place_detail = a.place_detail
FROM (
    SELECT project_id, min(start_date) AS start_date, max(end_date) AS end_date,
           coalesce((array_agg(place_detail ORDER BY position) FILTER (WHERE place_detail <> ''))[1], '') AS place_detail
    FROM project_execution_periods GROUP BY project_id
) a
WHERE a.project_id = p.id;

DROP TABLE project_execution_periods;
