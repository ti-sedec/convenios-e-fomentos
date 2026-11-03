-- +goose Up

-- Parlamentares de Mato Grosso eleitos em 2026 (mandato a partir de 2027): 8 deputados federais e 24 estaduais.
-- Fonte: ALMT, "Conheça os eleitos em Mato Grosso" (4 de outubro de 2026). Só o nome é gravado; quem já existe
-- no cadastro (mesmo nome e casa, sem diferenciar maiúsculas) é mantido como está.
INSERT INTO parliamentarians (name, chamber) VALUES
    ('Coronel Fernanda', 'federal'),
    ('Professora Rosa Neide', 'federal'),
    ('Fábio Garcia', 'federal'),
    ('Coronel Assis', 'federal'),
    ('Virgínia Mendes', 'federal'),
    ('Juarez Costa', 'federal'),
    ('Thiago Boava Hey Roy', 'federal'),
    ('Coronel Roveri', 'federal'),
    ('Max Russi', 'state'),
    ('Lúdio Cabral', 'state'),
    ('Gilberto Cattani', 'state'),
    ('Samantha do Abílio', 'state'),
    ('Dilmar Dal Bosco', 'state'),
    ('Ulysses Moraes', 'state'),
    ('Beto Dois a Um', 'state'),
    ('Dr. João', 'state'),
    ('Leo Bortolin', 'state'),
    ('Alessandra Ferreira', 'state'),
    ('Faissal', 'state'),
    ('Thiago Silva', 'state'),
    ('Diego Guimarães', 'state'),
    ('Valmir Moretto', 'state'),
    ('Dr. Eugênio', 'state'),
    ('Fabinho Tardin', 'state'),
    ('Nininho', 'state'),
    ('Valdir Barranco', 'state'),
    ('Sebastião Rezende', 'state'),
    ('Carlos Avallone', 'state'),
    ('Alcino Barcelos', 'state'),
    ('Adelcino Lopo', 'state'),
    ('Wilson Santos', 'state'),
    ('Chico Guarnieri', 'state')
ON CONFLICT (lower(name), chamber) DO NOTHING;

-- +goose Down
-- Parlamentares já usados em emendas ficam (a referência é ON DELETE RESTRICT).
DELETE FROM parliamentarians p
WHERE (lower(p.name), p.chamber) IN (
    ('coronel fernanda', 'federal'),
    ('professora rosa neide', 'federal'),
    ('fábio garcia', 'federal'),
    ('coronel assis', 'federal'),
    ('virgínia mendes', 'federal'),
    ('juarez costa', 'federal'),
    ('thiago boava hey roy', 'federal'),
    ('coronel roveri', 'federal'),
    ('max russi', 'state'),
    ('lúdio cabral', 'state'),
    ('gilberto cattani', 'state'),
    ('samantha do abílio', 'state'),
    ('dilmar dal bosco', 'state'),
    ('ulysses moraes', 'state'),
    ('beto dois a um', 'state'),
    ('dr. joão', 'state'),
    ('leo bortolin', 'state'),
    ('alessandra ferreira', 'state'),
    ('faissal', 'state'),
    ('thiago silva', 'state'),
    ('diego guimarães', 'state'),
    ('valmir moretto', 'state'),
    ('dr. eugênio', 'state'),
    ('fabinho tardin', 'state'),
    ('nininho', 'state'),
    ('valdir barranco', 'state'),
    ('sebastião rezende', 'state'),
    ('carlos avallone', 'state'),
    ('alcino barcelos', 'state'),
    ('adelcino lopo', 'state'),
    ('wilson santos', 'state'),
    ('chico guarnieri', 'state')
)
AND NOT EXISTS (SELECT 1 FROM project_amendments a WHERE a.parliamentarian_id = p.id);
