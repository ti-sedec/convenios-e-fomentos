-- +goose Up

-- O número da proposta no SIGCon tem o formato NNNN-AAAA (e não NNNN/AAAA). Converte os números já gravados e troca a restrição.
ALTER TABLE projects DROP CONSTRAINT projects_sigcon_number_check;
UPDATE projects SET sigcon_number = replace(sigcon_number, '/', '-') WHERE sigcon_number LIKE '%/%';
ALTER TABLE projects ADD CONSTRAINT projects_sigcon_number_check
    CHECK (sigcon_number IS NULL OR sigcon_number ~ '^[0-9]{1,6}-[0-9]{4}$');

-- +goose Down

ALTER TABLE projects DROP CONSTRAINT projects_sigcon_number_check;
UPDATE projects SET sigcon_number = replace(sigcon_number, '-', '/') WHERE sigcon_number LIKE '%-%';
ALTER TABLE projects ADD CONSTRAINT projects_sigcon_number_check
    CHECK (sigcon_number IS NULL OR sigcon_number ~ '^[0-9]{1,6}/[0-9]{4}$');
