-- +goose Up

-- CNPJ alfanumérico (IN RFB nº 2.229/2024): 12 posições de 0-9 e A-Z, mais 2 dígitos verificadores numéricos. O CNPJ numérico
-- antigo continua válido, então a regra só fica mais larga e nenhum dado existente é afetado.
ALTER TABLE projects DROP CONSTRAINT projects_institution_cnpj_check;
ALTER TABLE projects ADD CONSTRAINT projects_institution_cnpj_check
    CHECK (institution_cnpj IS NULL OR institution_cnpj ~ '^[0-9A-Z]{12}[0-9]{2}$');

-- +goose Down

ALTER TABLE projects DROP CONSTRAINT projects_institution_cnpj_check;
ALTER TABLE projects ADD CONSTRAINT projects_institution_cnpj_check
    CHECK (institution_cnpj IS NULL OR institution_cnpj ~ '^[0-9]{14}$');
