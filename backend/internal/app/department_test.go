package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

// mkDept cria o departamento (se não existir) e devolve o ID.
func (e *env) mkDept(name string) string {
	e.t.Helper()
	d, err := db.New(e.pool).UpsertDepartment(context.Background(), db.UpsertDepartmentParams{Name: name})
	require.NoError(e.t, err)
	return d.ID.String()
}

func TestDepartamentosExigemAutenticacaoEPermissao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/admin/departments"}, {"POST", "/api/admin/departments"}, {"GET", "/api/admin/departments/" + id},
		{"PUT", "/api/admin/departments/" + id}, {"DELETE", "/api/admin/departments/" + id},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status, "sem token")
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status, "sem permissão")
		})
	}
}

func TestLeituraDeDepartamentosPorQuemAdministraAdministradores(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", "admin::settings.users.read")
	e.mkRole("chefe", "admin::settings.departments.read")
	gestor, _ := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	chefe, _ := e.newAdmin(boss, "chefe@exemplo.com", "chefe")

	assert.Equal(t, 200, e.do("GET", "/api/admin/departments", nil, gestor).Status, "alimenta o seletor de departamento")
	assert.Equal(t, 200, e.do("GET", "/api/admin/departments", nil, chefe).Status)
	assert.Equal(t, 403, e.do("POST", "/api/admin/departments", map[string]any{"name": "TI"}, gestor).Status, "ler não dá escrever")
	assert.Equal(t, 403, e.do("POST", "/api/admin/departments", map[string]any{"name": "TI"}, chefe).Status)
}

func TestCrudDeDepartamentos(t *testing.T) {
	e, boss := setup(t)
	e.mkDept("Jurídico")
	e.mkDept("Financeiro")

	r := e.do("GET", "/api/admin/departments", nil, boss)
	require.Equal(t, 200, r.Status)
	assert.Len(t, r.list(), 2)
	assert.Equal(t, "Financeiro", r.list()[0].(map[string]any)["name"], "ordenado por nome")

	r = e.do("POST", "/api/admin/departments", map[string]any{"name": "  Tecnologia   da  Informação "}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, "Tecnologia da Informação", r.data()["name"], "espaços são normalizados")
	assert.EqualValues(t, 0, r.data()["userCount"])
	id := r.data()["id"].(string)

	assert.Equal(t, "department_name_taken", e.do("POST", "/api/admin/departments", map[string]any{"name": "Tecnologia da Informação"}, boss).errCode())
	assert.Equal(t, 422, e.do("POST", "/api/admin/departments", map[string]any{"name": "   "}, boss).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/departments", map[string]any{"name": string(make([]rune, 101))}, boss).Status)

	r = e.do("PUT", "/api/admin/departments/"+id, map[string]any{"name": "TI"}, boss)
	require.Equal(t, 200, r.Status)
	assert.Equal(t, "TI", r.data()["name"])
	assert.Equal(t, "department_name_taken", e.do("PUT", "/api/admin/departments/"+id, map[string]any{"name": "Financeiro"}, boss).errCode())
	assert.Equal(t, 200, e.do("PUT", "/api/admin/departments/"+id, map[string]any{"name": "TI"}, boss).Status, "manter o próprio nome é permitido")
	assert.Equal(t, "TI", e.do("GET", "/api/admin/departments/"+id, nil, boss).data()["name"])

	assert.Equal(t, 204, e.do("DELETE", "/api/admin/departments/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/departments/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/departments/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("PUT", "/api/admin/departments/"+id, map[string]any{"name": "X"}, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/departments/não-é-uuid", nil, boss).Status)
}

func TestExcluirDepartamentoEmUsoEhRecusado(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("redator")
	_, biaID := e.newAdmin(boss, "bia@exemplo.com", "redator")
	dept := e.mkDept("Financeiro")

	require.Equal(t, 200, e.do("PUT", "/api/admin/users/"+biaID, map[string]any{"departmentId": dept}, boss).Status)

	r := e.do("GET", "/api/admin/departments/"+dept, nil, boss)
	assert.EqualValues(t, 1, r.data()["userCount"])

	r = e.do("DELETE", "/api/admin/departments/"+dept, nil, boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "department_in_use", r.errCode())
	details := r.Body["error"].(map[string]any)["details"].(map[string]any)
	assert.EqualValues(t, 1, details["userCount"])

	// Sem vínculos, a exclusão passa.
	require.Equal(t, 200, e.do("PUT", "/api/admin/users/"+biaID, map[string]any{"departmentId": nil}, boss).Status)
	assert.Equal(t, 204, e.do("DELETE", "/api/admin/departments/"+dept, nil, boss).Status)
}

func TestDepartamentosSaoAuditadosEAuditoriaTrazONomeDoAtor(t *testing.T) {
	e, boss := setup(t)
	r := e.do("POST", "/api/admin/departments", map[string]any{"name": "TI"}, boss)
	id := r.data()["id"].(string)
	e.do("PUT", "/api/admin/departments/"+id, map[string]any{"name": "Tecnologia"}, boss)
	e.do("DELETE", "/api/admin/departments/"+id, nil, boss)

	for _, action := range []string{"department.create", "department.update", "department.delete"} {
		logs := e.do("GET", "/api/admin/audit-logs?action="+action+"&entityId="+id, nil, boss)
		require.Len(t, logs.list(), 1, action)
	}
	upd := e.do("GET", "/api/admin/audit-logs?action=department.update", nil, boss).list()[0].(map[string]any)
	payload := upd["payload"].(map[string]any)
	assert.Equal(t, map[string]any{"name": "TI", "isFinalistic": false}, payload["before"])
	assert.Equal(t, map[string]any{"name": "Tecnologia", "isFinalistic": false}, payload["after"])

	// A Ana foi criada sem nome (CLI) e só o primeiro acesso completa os dados: o nome vem do MT Login.
	assert.Equal(t, "Ana Teste", upd["actorName"])

	// Sem actorName quando o ator não é administrador ou o administrador foi excluído depois.
	bruno, brunoID := e.newAdmin(boss, "bruno@exemplo.com", "super-admin")
	require.Equal(t, 201, e.do("POST", "/api/admin/departments", map[string]any{"name": "Obras"}, bruno).Status)
	require.Equal(t, 204, e.do("DELETE", "/api/admin/users/"+brunoID, nil, boss).Status)
	logs := e.do("GET", "/api/admin/audit-logs?action=department.create&actorId="+brunoID, nil, boss).list()
	require.Len(t, logs, 1)
	assert.Nil(t, logs[0].(map[string]any)["actorName"])
	sys := e.do("GET", "/api/admin/audit-logs?actorType=system", nil, boss).list()
	require.NotEmpty(t, sys, "EnsureSuperAdmin grava como system")
	assert.Nil(t, sys[0].(map[string]any)["actorName"])
}

func TestDepartamentoAreaFinalistica(t *testing.T) {
	e, boss := setup(t)
	e.mkDept("Jurídico")

	// Criar sem o campo vale falso; com o campo grava.
	r := e.do("POST", "/api/admin/departments", map[string]any{"name": "Obras"}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, false, r.data()["isFinalistic"])
	obras := r.data()["id"].(string)
	r = e.do("POST", "/api/admin/departments", map[string]any{"name": "Cultura", "isFinalistic": true}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, true, r.data()["isFinalistic"])
	cultura := r.data()["id"].(string)

	// O filtro devolve só as áreas finalísticas.
	r = e.do("GET", "/api/admin/departments?finalistic=true", nil, boss)
	require.Equal(t, 200, r.Status)
	require.Len(t, r.list(), 1)
	assert.Equal(t, "Cultura", r.list()[0].(map[string]any)["name"])
	assert.Len(t, e.do("GET", "/api/admin/departments", nil, boss).list(), 3, "sem o filtro, todos")

	// PUT sem o campo mantém o valor (um cliente antigo não desmarca sem querer); com o campo, muda.
	r = e.do("PUT", "/api/admin/departments/"+cultura, map[string]any{"name": "Cultura e Turismo"}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, true, r.data()["isFinalistic"], "campo omitido mantém")
	r = e.do("PUT", "/api/admin/departments/"+obras, map[string]any{"name": "Obras", "isFinalistic": true}, boss)
	require.Equal(t, 200, r.Status)
	assert.Equal(t, true, r.data()["isFinalistic"])
	r = e.do("PUT", "/api/admin/departments/"+cultura, map[string]any{"name": "Cultura e Turismo", "isFinalistic": false}, boss)
	require.Equal(t, 200, r.Status)
	assert.Equal(t, false, r.data()["isFinalistic"])

	// A mudança do marcador fica na auditoria, com antes e depois.
	var before, after bool
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT (payload->'before'->>'isFinalistic')::boolean, (payload->'after'->>'isFinalistic')::boolean
		 FROM audit_logs WHERE entity = 'department' AND entity_id = $1 AND action = 'department.update' ORDER BY created_at DESC LIMIT 1`, cultura).Scan(&before, &after))
	assert.True(t, before)
	assert.False(t, after)
}
