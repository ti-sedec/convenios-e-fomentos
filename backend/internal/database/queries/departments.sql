-- name: ListDepartments :many
SELECT id, name, is_finalistic FROM departments ORDER BY name;

-- Cria o departamento com o marcador informado. Se já existe, só confirma o nome: o marcador NÃO é alterado, para um novo seed
-- não desfazer a escolha de quem administra os departamentos.
-- name: UpsertDepartment :one
INSERT INTO departments (name, is_finalistic) VALUES (sqlc.arg(name), sqlc.arg(is_finalistic))
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id, name, is_finalistic;

-- only_finalistic = true devolve só as áreas finalísticas.
-- name: ListDepartmentsWithCounts :many
SELECT d.id, d.name, d.is_finalistic,
       (SELECT count(*) FROM admins a WHERE a.department_id = d.id) AS user_count,
       (SELECT count(*) FROM admins a JOIN admin_users_roles ur ON ur.admin_id = a.id JOIN admin_roles r ON r.id = ur.role_id
         WHERE a.department_id = d.id AND r.code = 'department-reviewer' AND a.is_active AND NOT a.blocked) AS reviewer_count,
       (SELECT count(*) FROM projects pr WHERE pr.department_id = d.id) AS project_count
FROM departments d
WHERE (NOT sqlc.arg(only_finalistic)::boolean OR d.is_finalistic)
ORDER BY d.name;

-- name: GetDepartmentWithCounts :one
SELECT d.id, d.name, d.is_finalistic,
       (SELECT count(*) FROM admins a WHERE a.department_id = d.id) AS user_count,
       (SELECT count(*) FROM admins a JOIN admin_users_roles ur ON ur.admin_id = a.id JOIN admin_roles r ON r.id = ur.role_id
         WHERE a.department_id = d.id AND r.code = 'department-reviewer' AND a.is_active AND NOT a.blocked) AS reviewer_count,
       (SELECT count(*) FROM projects pr WHERE pr.department_id = d.id) AS project_count
FROM departments d
WHERE d.id = $1;

-- Trava a linha: inserções/atualizações de administradores e projetos que referenciam o
-- departamento esperam a transação terminar.
-- name: GetDepartmentForUpdate :one
SELECT id, name, is_finalistic FROM departments WHERE id = $1 FOR UPDATE;

-- name: CreateDepartment :one
INSERT INTO departments (name, is_finalistic) VALUES (sqlc.arg(name), sqlc.arg(is_finalistic)) RETURNING id, name, is_finalistic;

-- name: UpdateDepartment :one
UPDATE departments SET name = sqlc.arg(name), is_finalistic = sqlc.arg(is_finalistic) WHERE id = sqlc.arg(id) RETURNING id, name, is_finalistic;

-- name: DeleteDepartment :execrows
DELETE FROM departments WHERE id = $1;

-- name: ListDepartmentsByName :many
SELECT id, name FROM departments WHERE name = ANY($1::text[]);
