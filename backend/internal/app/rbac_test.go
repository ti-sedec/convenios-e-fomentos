package app_test

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
)

func (e *env) roleID(code string) string {
	e.t.Helper()
	var id uuid.UUID
	require.NoError(e.t, e.pool.QueryRow(context.Background(), "SELECT id FROM admin_roles WHERE code=$1", code).Scan(&id))
	return id.String()
}

func (e *env) adminID(email string) string {
	e.t.Helper()
	var id uuid.UUID
	require.NoError(e.t, e.pool.QueryRow(context.Background(), "SELECT id FROM admins WHERE email=$1", email).Scan(&id))
	return id.String()
}

// mkRole cria a role (se não existir) e concede as ações, sem condição.
func (e *env) mkRole(code string, actions ...string) {
	e.t.Helper()
	_, err := db.New(e.pool).UpsertAdminRole(context.Background(), db.UpsertAdminRoleParams{Code: code, Name: code})
	require.NoError(e.t, err)
	for _, a := range actions {
		e.grant(code, a, "", "[]")
	}
}

// newAdmin cadastra o administrador pelo CPF com a role, faz o primeiro acesso dele e devolve (token, id).
func (e *env) newAdmin(boss, email, roleCode string) (string, string) {
	e.t.Helper()
	tok := registerAndLogin(e.t, e, boss, email, roleCode)
	return tok, e.adminID(email)
}

func (r resp) list() []any {
	l, _ := r.Body["data"].([]any)
	return l
}

func (r resp) meta() map[string]any {
	m, _ := r.Body["meta"].(map[string]any)
	return m
}

var usersAll = []string{"admin::settings.users.read", "admin::settings.users.create", "admin::settings.users.update", "admin::settings.users.delete"}

func setup(t *testing.T) (*env, string) {
	e := newEnv(t)
	e.registerFirst()
	boss := e.login("ana@exemplo.com")
	return e, boss
}

func TestRotasExigemAutenticacaoEPermissao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := e.adminID("ana@exemplo.com")
	rid := e.roleID("vazio")
	routes := []struct{ method, path string }{
		{"GET", "/api/admin/users"}, {"GET", "/api/admin/users/" + id}, {"PUT", "/api/admin/users/" + id}, {"DELETE", "/api/admin/users/" + id},
		{"POST", "/api/admin/users/batch-delete"}, {"POST", "/api/admin/users/" + id + "/activate"}, {"POST", "/api/admin/users/" + id + "/deactivate"},
		{"POST", "/api/admin/users/" + id + "/roles"}, {"PUT", "/api/admin/users/" + id + "/roles"}, {"DELETE", "/api/admin/users/" + id + "/roles/" + rid},
		{"POST", "/api/admin/users/" + id + "/block"}, {"POST", "/api/admin/users/" + id + "/unblock"}, {"GET", "/api/admin/users/" + id + "/permissions"},
		{"GET", "/api/admin/roles"}, {"POST", "/api/admin/roles"}, {"GET", "/api/admin/roles/" + rid}, {"PUT", "/api/admin/roles/" + rid},
		{"DELETE", "/api/admin/roles/" + rid}, {"PUT", "/api/admin/roles/" + rid + "/permissions"}, {"POST", "/api/admin/roles/" + rid + "/duplicate"},
		{"GET", "/api/admin/catalog/actions"}, {"GET", "/api/admin/catalog/conditions"}, {"GET", "/api/admin/audit-logs"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status, "sem token")
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status, "sem permissão")
		})
	}
}

func TestListagemDeAdministradores(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("redator")
	e.mkRole("revisor")
	for _, n := range []string{"bia", "caio", "dora"} {
		e.newAdmin(boss, n+"@exemplo.com", "redator")
	}
	e.newAdmin(boss, "eva.revisora@exemplo.com", "revisor")
	// Pré-cadastro que ainda não fez o primeiro acesso.
	inv := e.do("POST", "/api/admin/users", map[string]any{"cpf": cpfFor("pend@exemplo.com"), "roleIds": []string{e.roleID("redator")}}, boss)
	require.Equal(t, 201, inv.Status)
	// Um desativado.
	e.do("POST", "/api/admin/users/"+e.adminID("dora@exemplo.com")+"/deactivate", nil, boss)

	all := e.do("GET", "/api/admin/users", nil, boss)
	require.Equal(t, 200, all.Status)
	assert.Len(t, all.list(), 6)
	assert.EqualValues(t, 6, all.meta()["total"])
	assert.EqualValues(t, 1, all.meta()["pageCount"])

	count := func(query string) int { return len(e.do("GET", "/api/admin/users?"+query, nil, boss).list()) }
	assert.Equal(t, 1, count("q=revisora"), "busca no e-mail")
	assert.Equal(t, 1, count("q="+url.QueryEscape("Bia Teste")), "busca no nome completo")
	assert.Equal(t, 1, count("q="+url.QueryEscape(cpf.Format(cpfFor("caio@exemplo.com")))), "busca por CPF com pontuação")
	assert.Equal(t, 1, count("q="+cpfFor("dora@exemplo.com")), "busca por CPF só com dígitos")
	assert.Equal(t, 0, count("q=00"), "menos de 3 dígitos não busca por CPF")
	assert.Equal(t, 0, count("q=%25"), "% é literal, não curinga")
	assert.Equal(t, 4, count("roleId="+e.roleID("redator")), "dora desativada e o convite pendente também têm a role")
	assert.Equal(t, 4, count("status=active"))
	assert.Equal(t, 1, count("status=inactive"))
	assert.Equal(t, 1, count("status=pending"))
	assert.Equal(t, 0, count("status=blocked"))
	assert.Equal(t, 1, count("status=active&roleId="+e.roleID("revisor")))

	// Ordenação e paginação.
	asc := e.do("GET", "/api/admin/users?sort=email:asc&pageSize=2&page=2", nil, boss)
	require.Equal(t, 200, asc.Status)
	assert.EqualValues(t, 3, asc.meta()["pageCount"])
	assert.Equal(t, "bia@exemplo.com", asc.list()[0].(map[string]any)["email"], "o pré-cadastro, sem e-mail ainda, vem primeiro")
	desc := e.do("GET", "/api/admin/users?sort=email:desc", nil, boss)
	assert.Equal(t, "eva.revisora@exemplo.com", desc.list()[0].(map[string]any)["email"])

	// Limite máximo de pageSize e entradas inválidas.
	assert.EqualValues(t, 100, e.do("GET", "/api/admin/users?pageSize=5000", nil, boss).meta()["pageSize"])
	for _, bad := range []string{"sort=password_hash", "sort=" + url.QueryEscape("email;DROP TABLE admins"), "sort=email:up", "page=0", "pageSize=x", "status=zumbi", "roleId=nao-uuid"} {
		assert.Equal(t, 422, e.do("GET", "/api/admin/users?"+bad, nil, boss).Status, bad)
	}
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 6, n)

	for _, raw := range all.list() {
		item := raw.(map[string]any)
		assert.Contains(t, item, "roles")
		assert.NotContains(t, item, "passwordHash")
		assert.Regexp(t, `^\*\*\*\.\d{3}\.\d{3}-\*\*$`, item["cpf"], "CPF sempre mascarado nas listagens")
	}
}

func TestEdicaoDeAdministrador(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("redator")
	_, bia := e.newAdmin(boss, "bia@exemplo.com", "redator")
	_, caio := e.newAdmin(boss, "caio@exemplo.com", "redator")
	dept, err := db.New(e.pool).UpsertDepartment(context.Background(), db.UpsertDepartmentParams{Name: "Financeiro"})
	require.NoError(t, err)

	r := e.do("PUT", "/api/admin/users/"+bia, map[string]any{"departmentId": dept.ID.String()}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, dept.ID.String(), r.data()["departmentId"])
	assert.Equal(t, "bia@exemplo.com", r.data()["email"], "e-mail e nome continuam os do MT Login")

	// Nome, CPF e e-mail não são editáveis: vêm do MT Login.
	for _, field := range []map[string]any{{"firstName": "X"}, {"email": "x@y.com"}, {"cpf": cpfFor("x@y.com")}} {
		assert.Equal(t, 400, e.do("PUT", "/api/admin/users/"+bia, field, boss).Status, "campo desconhecido: %v", field)
	}
	assert.Equal(t, 422, e.do("PUT", "/api/admin/users/"+bia, map[string]any{"departmentId": uuid.NewString()}, boss).Status)
	assert.Equal(t, 404, e.do("PUT", "/api/admin/users/"+uuid.NewString(), map[string]any{}, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/users/nao-uuid", nil, boss).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/users/"+caio, nil, boss).Status)

	// Remover o departamento.
	r = e.do("PUT", "/api/admin/users/"+bia, map[string]any{"departmentId": nil}, boss)
	require.Equal(t, 200, r.Status)
	assert.Nil(t, r.data()["departmentId"])

	// Auditoria com estado anterior e novo.
	logs := e.do("GET", "/api/admin/audit-logs?action=admin.update&entityId="+bia, nil, boss)
	require.Len(t, logs.list(), 2)
	payload := logs.list()[1].(map[string]any)["payload"].(map[string]any)
	assert.Nil(t, payload["before"].(map[string]any)["departmentId"])
	assert.Equal(t, dept.ID.String(), payload["after"].(map[string]any)["departmentId"])
	assert.Equal(t, cpf.Mask(cpfFor("bia@exemplo.com")), payload["after"].(map[string]any)["cpf"])
	assert.Equal(t, e.adminID("ana@exemplo.com"), logs.list()[0].(map[string]any)["actorId"])
}

func TestAtivarDesativarEProtecaoDoUltimoSuperAdmin(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	gestor, gestorID := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	ana := e.adminID("ana@exemplo.com")

	// O gestor não age sobre quem tem mais poder do que ele, como o Super Admin.
	r := e.do("POST", "/api/admin/users/"+ana+"/deactivate", nil, gestor)
	assert.Equal(t, 403, r.Status)
	assert.Equal(t, "target_more_privileged", r.errCode())
	assert.Equal(t, 200, e.do("GET", "/api/admin/auth/me", nil, boss).Status, "nada mudou")

	// Quem não é Super Admin não concede a role Super Admin a ninguém, nem a si mesmo.
	e.mkRole("redator")
	_, bia := e.newAdmin(boss, "bia@exemplo.com", "redator")
	assert.Equal(t, 403, e.do("POST", "/api/admin/users/"+bia+"/roles", map[string]any{"roleIds": []string{e.roleID("super-admin")}}, gestor).Status)
	assert.Equal(t, "cannot_modify_self", e.do("POST", "/api/admin/users/"+gestorID+"/roles", map[string]any{"roleIds": []string{e.roleID("super-admin")}}, gestor).errCode())
	// O Super Admin concede; agora existem dois.
	assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+gestorID+"/roles", map[string]any{"roleIds": []string{e.roleID("super-admin")}}, boss).Status)

	// Auto-desativação é proibida, mesmo para Super Admin.
	assert.Equal(t, "cannot_modify_self", e.do("POST", "/api/admin/users/"+gestorID+"/deactivate", nil, gestor).errCode())

	// Agora Super Admin, o gestor pode agir sobre a Ana. Desativá-la tira o acesso dela na requisição seguinte, mesmo com o token ainda válido.
	r = e.do("POST", "/api/admin/users/"+ana+"/deactivate", nil, gestor)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "inactive", r.data()["status"])
	me := e.do("GET", "/api/admin/auth/me", nil, boss)
	assert.Equal(t, 403, me.Status)
	assert.Equal(t, "account_disabled", me.errCode())

	// Reativar devolve o acesso.
	r = e.do("POST", "/api/admin/users/"+ana+"/activate", nil, gestor)
	assert.Equal(t, "active", r.data()["status"])
	assert.Equal(t, 200, e.do("GET", "/api/admin/auth/me", nil, boss).Status)
}

func TestExclusaoIndividualEmLoteEUltimoSuperAdmin(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	e.mkRole("redator")
	gestor, gestorID := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	_, bia := e.newAdmin(boss, "bia@exemplo.com", "redator")
	_, caio := e.newAdmin(boss, "caio@exemplo.com", "redator")
	ana := e.adminID("ana@exemplo.com")

	assert.Equal(t, "target_more_privileged", e.do("DELETE", "/api/admin/users/"+ana, nil, gestor).errCode())
	assert.Equal(t, "cannot_modify_self", e.do("DELETE", "/api/admin/users/"+gestorID, nil, gestor).errCode())

	// Lote é atômico: se um item viola a regra, nenhum é excluído.
	r := e.do("POST", "/api/admin/users/batch-delete", map[string]any{"ids": []string{bia, ana}}, gestor)
	assert.Equal(t, 403, r.Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/users/"+bia, nil, boss).Status)

	// Item inexistente derruba o lote inteiro.
	assert.Equal(t, 404, e.do("POST", "/api/admin/users/batch-delete", map[string]any{"ids": []string{bia, uuid.NewString()}}, gestor).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/users/"+bia, nil, boss).Status)

	assert.Equal(t, 204, e.do("POST", "/api/admin/users/batch-delete", map[string]any{"ids": []string{bia, caio, bia}}, gestor).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/users/"+bia, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/users/"+caio, nil, boss).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/users/batch-delete", map[string]any{"ids": []string{}}, gestor).Status)

	_, dora := e.newAdmin(boss, "dora@exemplo.com", "redator")
	assert.Equal(t, 204, e.do("DELETE", "/api/admin/users/"+dora, nil, gestor).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/users/"+dora, nil, gestor).Status)

	logs := e.do("GET", "/api/admin/audit-logs?action=admin.delete", nil, boss)
	assert.EqualValues(t, 3, logs.meta()["total"])
}

func TestRemoverSuperAdminDoUltimoESerUnicoAdmin(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	gtok, gestorID := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	ana := e.adminID("ana@exemplo.com")
	super := e.roleID("super-admin")

	// Ana tem só uma role: não pode ficar sem nenhuma.
	r := e.do("DELETE", "/api/admin/users/"+ana+"/roles/"+super, nil, boss)
	assert.Equal(t, 422, r.Status)

	// Com outra role, ainda é o último Super Admin ativo.
	assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+ana+"/roles", map[string]any{"roleIds": []string{e.roleID("gestor")}}, boss).Status)
	r = e.do("DELETE", "/api/admin/users/"+ana+"/roles/"+super, nil, boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "last_super_admin", r.errCode())

	// Com um segundo Super Admin, a remoção passa.
	assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+gestorID+"/roles", map[string]any{"roleIds": []string{super}}, boss).Status)
	r = e.do("DELETE", "/api/admin/users/"+ana+"/roles/"+super, nil, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Len(t, r.data()["roles"], 1)

	// Role não atribuída, inexistente e roles inválidas (agora quem age é o novo Super Admin;
	// a Ana perdeu a role e, com ela, o direito de mexer na role Super Admin).
	assert.Equal(t, 403, e.do("DELETE", "/api/admin/users/"+gestorID+"/roles/"+super, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/users/"+ana+"/roles/"+super, nil, gtok).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/users/"+ana+"/roles/"+uuid.NewString(), nil, gtok).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/users/"+ana+"/roles", map[string]any{"roleIds": []string{uuid.NewString()}}, gtok).Status)
}

func TestCadastroComSuperAdminSoPorSuperAdmin(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	gestor, _ := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	body := map[string]any{"cpf": cpfFor("x@exemplo.com"), "roleIds": []string{e.roleID("super-admin")}}
	assert.Equal(t, 403, e.do("POST", "/api/admin/users", body, gestor).Status)
	assert.Equal(t, 201, e.do("POST", "/api/admin/users", body, boss).Status)
}

func TestDesativacoesConcorrentesNaoRemovemTodosOsSuperAdmins(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	bruno, brunoID := e.newAdmin(boss, "bruno@exemplo.com", "gestor")
	ana := e.adminID("ana@exemplo.com")
	require.Equal(t, 200, e.do("POST", "/api/admin/users/"+brunoID+"/roles", map[string]any{"roleIds": []string{e.roleID("super-admin")}}, boss).Status)

	// Os dois Super Admins desativam um ao outro ao mesmo tempo.
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, c := range []struct{ token, target string }{{boss, brunoID}, {bruno, ana}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- e.do("POST", "/api/admin/users/"+c.target+"/deactivate", nil, c.token).Status
		}()
	}
	wg.Wait()
	close(results)
	var ok int
	for s := range results {
		switch s {
		case 200:
			ok++
		case 403, 409: // conta já desativada (403) ou proteção do último Super Admin (409)
		default:
			t.Errorf("status inesperado %d", s)
		}
	}
	assert.Equal(t, 1, ok)
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM admins a JOIN admin_users_roles ur ON ur.admin_id=a.id JOIN admin_roles r ON r.id=ur.role_id WHERE r.code='super-admin' AND a.is_active`).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestCrudDeRoles(t *testing.T) {
	e, boss := setup(t)

	r := e.do("POST", "/api/admin/roles", map[string]any{"name": "Gestão de Conteúdo", "description": "Equipe editorial"}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	id := r.data()["id"].(string)
	assert.Equal(t, "gestao-de-conteudo", r.data()["code"])
	assert.Equal(t, false, r.data()["isSystem"])
	assert.EqualValues(t, 0, r.data()["userCount"])
	assert.Equal(t, []any{}, r.data()["permissions"])

	second := e.do("POST", "/api/admin/roles", map[string]any{"name": "Gestão de Conteúdo"}, boss)
	assert.Equal(t, "gestao-de-conteudo-2", second.data()["code"], "code único com sufixo")
	assert.Equal(t, 422, e.do("POST", "/api/admin/roles", map[string]any{"name": ""}, boss).Status)

	r = e.do("PUT", "/api/admin/roles/"+id, map[string]any{"name": "Gestão Editorial", "description": "nova"}, boss)
	require.Equal(t, 200, r.Status)
	assert.Equal(t, "Gestão Editorial", r.data()["name"])
	assert.Equal(t, "gestao-de-conteudo", r.data()["code"], "o code é imutável")

	// Contagem de administradores na listagem e no detalhe.
	e.newAdmin(boss, "bia@exemplo.com", "gestao-de-conteudo")
	list := e.do("GET", "/api/admin/roles", nil, boss)
	counts := map[string]float64{}
	for _, it := range list.list() {
		m := it.(map[string]any)
		counts[m["code"].(string)] = m["userCount"].(float64)
	}
	assert.EqualValues(t, 1, counts["gestao-de-conteudo"])
	assert.EqualValues(t, 1, counts["super-admin"])
	assert.EqualValues(t, 1, e.do("GET", "/api/admin/roles/"+id, nil, boss).data()["userCount"])

	// Excluir role em uso: 409 explicando o motivo.
	del := e.do("DELETE", "/api/admin/roles/"+id, nil, boss)
	assert.Equal(t, 409, del.Status)
	assert.Equal(t, "role_in_use", del.errCode())
	assert.Contains(t, del.Body["error"].(map[string]any)["message"], "1 administrador")

	// Role livre pode ser excluída.
	free := second.data()["id"].(string)
	assert.Equal(t, 204, e.do("DELETE", "/api/admin/roles/"+free, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/roles/"+free, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/roles/"+free, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/roles/xyz", nil, boss).Status)

	// Super Admin: não exclui, não duplica, não altera permissões.
	super := e.roleID("super-admin")
	assert.Equal(t, "super_admin_immutable", e.do("DELETE", "/api/admin/roles/"+super, nil, boss).errCode())
	assert.Equal(t, "super_admin_immutable", e.do("POST", "/api/admin/roles/"+super+"/duplicate", nil, boss).errCode())
	assert.Equal(t, "super_admin_immutable", e.do("PUT", "/api/admin/roles/"+super+"/permissions", map[string]any{"permissions": []any{}}, boss).errCode())
	sd := e.do("GET", "/api/admin/roles/"+super, nil, boss)
	assert.Equal(t, true, sd.data()["isSuperAdmin"])
}

func TestSubstituirPermissoesEValidacao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("editor")
	rid := e.roleID("editor")
	put := func(perms ...map[string]any) resp {
		if perms == nil {
			perms = []map[string]any{}
		}
		return e.do("PUT", "/api/admin/roles/"+rid+"/permissions", map[string]any{"permissions": perms}, boss)
	}
	fieldPaths := func(r resp) []string {
		var out []string
		for _, f := range r.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any) {
			out = append(out, f.(map[string]any)["field"].(string))
		}
		return out
	}

	cases := []struct {
		nome string
		perm map[string]any
		path string
	}{
		{"ação inexistente", map[string]any{"action": "admin::content.fantasma.read"}, "permissions[0].action"},
		{"ação do domínio api, que não existe mais", map[string]any{"action": "api::x.find"}, "permissions[0].action"},
		{"recurso divergente", map[string]any{"action": "admin::content.document.read", "subject": "outro"}, "permissions[0].subject"},
		{"campos inexistentes", map[string]any{"action": "admin::content.document.read", "fields": []string{"titulo", "nope"}}, "permissions[0].fields"},
		{"campos em ação sem suporte", map[string]any{"action": "admin::content.document.publish", "fields": []string{"titulo"}}, "permissions[0].fields"},
		{"condição em ação sem suporte", map[string]any{"action": "admin::content.document.create", "conditions": []string{"admin::is-creator"}}, "permissions[0].conditions"},
		{"condição inexistente", map[string]any{"action": "admin::content.document.read", "conditions": []string{"admin::nao-existe"}}, "permissions[0].conditions"},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			r := put(c.perm)
			assert.Equal(t, 422, r.Status, r.Body)
			assert.Equal(t, []string{c.path}, fieldPaths(r))
		})
	}
	dup := put(map[string]any{"action": "admin::content.document.read"}, map[string]any{"action": "admin::content.document.read", "subject": "document"})
	assert.Equal(t, 422, dup.Status, "mesma ação e recurso repetidos")
	assert.Equal(t, 422, e.do("PUT", "/api/admin/roles/"+rid+"/permissions", map[string]any{}, boss).Status)
	assert.Equal(t, 404, e.do("PUT", "/api/admin/roles/"+uuid.NewString()+"/permissions", map[string]any{"permissions": []any{}}, boss).Status)

	// Sucesso: fields nulo (todos) x lista; condições; subject padrão = recurso.
	r := put(
		map[string]any{"action": "admin::content.document.read", "fields": []string{"conteudo", "titulo"}, "conditions": []string{"admin::same-department", "admin::is-creator"}},
		map[string]any{"action": "admin::content.document.publish"},
		map[string]any{"action": "admin::settings.users.read"},
	)
	require.Equal(t, 200, r.Status, r.Body)
	perms := r.data()["permissions"].([]any)
	require.Len(t, perms, 3)
	byAction := map[string]map[string]any{}
	for _, p := range perms {
		m := p.(map[string]any)
		byAction[m["action"].(string)] = m
	}
	read := byAction["admin::content.document.read"]
	assert.Equal(t, "document", read["subject"])
	assert.Equal(t, []any{"conteudo", "titulo"}, read["fields"])
	assert.Equal(t, []any{"admin::is-creator", "admin::same-department"}, read["conditions"])
	assert.Nil(t, byAction["admin::content.document.publish"]["fields"], "null = todos os campos")

	// "fields": [] (nenhum campo) é diferente de null (todos) e deve ser preservado.
	r = put(map[string]any{"action": "admin::content.document.read", "fields": []string{}})
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, []any{}, r.data()["permissions"].([]any)[0].(map[string]any)["fields"])

	// Troca em lote substitui tudo (e a mudança vale imediatamente).
	require.Equal(t, 200, put(map[string]any{"action": "admin::settings.users.read"}).Status)
	editor, _ := e.newAdmin(boss, "ed@exemplo.com", "editor")
	assert.Equal(t, 200, e.do("GET", "/api/admin/users", nil, editor).Status)
	r = put(map[string]any{"action": "admin::settings.roles.read"})
	require.Equal(t, 200, r.Status)
	assert.Len(t, r.data()["permissions"], 1)
	assert.Equal(t, 403, e.do("GET", "/api/admin/users", nil, editor).Status, "permissão removida vale na hora")
	assert.Equal(t, 200, e.do("GET", "/api/admin/roles", nil, editor).Status)
	assert.Equal(t, 200, put().Status, "lista vazia remove tudo")
	assert.Equal(t, 403, e.do("GET", "/api/admin/roles", nil, editor).Status)

	// Permissão já gravada não se perde se a validação falhar.
	put(map[string]any{"action": "admin::settings.roles.read"})
	assert.Equal(t, 422, put(map[string]any{"action": "admin::settings.roles.read"}, map[string]any{"action": "nada"}).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/roles", nil, editor).Status)

	// Auditoria do before/after.
	logs := e.do("GET", "/api/admin/audit-logs?action=admin-role.permissions-replace&entityId="+rid, nil, boss)
	require.NotEmpty(t, logs.list())
	last := logs.list()[0].(map[string]any)["payload"].(map[string]any)
	assert.Contains(t, last, "before")
	assert.Contains(t, last, "after")
}

func TestDuplicarRole(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("editor", "admin::settings.users.read")
	e.grant("editor", "admin::settings.roles.read", `[]`, `[]`)
	src := e.roleID("editor")
	require.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+src, map[string]any{"name": "Editor", "description": "Edita tudo"}, boss).Status)

	r := e.do("POST", "/api/admin/roles/"+src+"/duplicate", nil, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, "Editor (cópia)", r.data()["name"])
	assert.Equal(t, "Edita tudo", r.data()["description"])
	assert.NotEqual(t, src, r.data()["id"])
	assert.Equal(t, "editor-copia", r.data()["code"])
	assert.Equal(t, false, r.data()["isSystem"])
	perms := r.data()["permissions"].([]any)
	require.Len(t, perms, 2)
	var fieldsOfRolesRead any = "x"
	for _, p := range perms {
		if m := p.(map[string]any); m["action"] == "admin::settings.roles.read" {
			fieldsOfRolesRead = m["fields"]
		}
	}
	assert.Equal(t, []any{}, fieldsOfRolesRead, "campos vazios (não nulos) são preservados")

	// A cópia é independente da original.
	e.do("PUT", "/api/admin/roles/"+r.data()["id"].(string)+"/permissions", map[string]any{"permissions": []any{}}, boss)
	assert.Len(t, e.do("GET", "/api/admin/roles/"+src, nil, boss).data()["permissions"], 2)
	assert.Equal(t, 404, e.do("POST", "/api/admin/roles/"+uuid.NewString()+"/duplicate", nil, boss).Status)
	// Duplicar de novo gera outro code.
	assert.Equal(t, "editor-copia-2", e.do("POST", "/api/admin/roles/"+src+"/duplicate", nil, boss).data()["code"])
}

func TestCatalogo(t *testing.T) {
	e, boss := setup(t)

	r := e.do("GET", "/api/admin/catalog/actions", nil, boss)
	require.Equal(t, 200, r.Status)
	groups := r.list()
	cats := []string{}
	var docAction map[string]any
	for _, g := range groups {
		gm := g.(map[string]any)
		cats = append(cats, gm["category"].(string))
		for _, a := range gm["actions"].([]any) {
			am := a.(map[string]any)
			if am["id"] == "admin::content.document.read" {
				docAction = am
			}
		}
	}
	assert.Equal(t, []string{"collection-types", "settings"}, cats)
	require.NotNil(t, docAction)
	assert.Equal(t, true, docAction["supportsFields"])
	assert.Equal(t, []any{"titulo", "conteudo", "status", "observacao_interna", "department_id"}, docAction["availableFields"], "campos configuráveis (sem os de sistema)")

	conds := e.do("GET", "/api/admin/catalog/conditions", nil, boss).list()
	ids := []string{}
	for _, c := range conds {
		ids = append(ids, c.(map[string]any)["id"].(string))
	}
	assert.Equal(t, []string{"admin::is-creator", "admin::has-same-role-as-creator", "admin::same-department", "admin::project-member", "admin::project-editor", "admin::project-submitted", "admin::project-submitted-same-department"}, ids)
}

func TestAuditoriaFiltrosEPaginacao(t *testing.T) {
	e, boss := setup(t)
	for i := range 5 {
		require.Equal(t, 201, e.do("POST", "/api/admin/roles", map[string]any{"name": fmt.Sprintf("Role %d", i)}, boss).Status)
	}
	ana := e.adminID("ana@exemplo.com")

	r := e.do("GET", "/api/admin/audit-logs?action=admin-role.create&pageSize=2", nil, boss)
	require.Equal(t, 200, r.Status)
	assert.Len(t, r.list(), 2)
	assert.EqualValues(t, 5, r.meta()["total"])
	assert.EqualValues(t, 3, r.meta()["pageCount"])
	first := r.list()[0].(map[string]any)
	assert.Equal(t, ana, first["actorId"])
	assert.Equal(t, "admin", first["actorType"])
	assert.Equal(t, "admin_role", first["entity"])
	assert.Contains(t, first["payload"], "after")

	count := func(q string) float64 {
		return e.do("GET", "/api/admin/audit-logs?"+q, nil, boss).meta()["total"].(float64)
	}
	assert.EqualValues(t, 5, count("entity=admin_role&actorId="+ana))
	assert.EqualValues(t, 0, count("actorId="+uuid.NewString()))
	assert.EqualValues(t, 0, count("actorType=api_token"))
	assert.EqualValues(t, 5, count("action=admin-role.create&from=2000-01-01T00:00:00Z&to=2999-01-01T00:00:00Z"))
	assert.EqualValues(t, 0, count("action=admin-role.create&to=2000-01-01T00:00:00Z"))
	assert.EqualValues(t, 0, count("action=admin-role.create&from=2999-01-01T00:00:00Z"))
	assert.Equal(t, 422, e.do("GET", "/api/admin/audit-logs?from=ontem", nil, boss).Status)
	assert.Equal(t, 422, e.do("GET", "/api/admin/audit-logs?to=2026-13-45", nil, boss).Status)
	// Filtro com SQL no valor é só um texto: não casa com nada.
	assert.EqualValues(t, 0, count("action="+url.QueryEscape("x' OR '1'='1")))
}

func TestApagarRoleInvalidaCache(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("tmp", "admin::settings.roles.read")
	rid := e.roleID("tmp")
	// Aquece o cache dessa role.
	tok, id := e.newAdmin(boss, "t@exemplo.com", "tmp")
	assert.Equal(t, 200, e.do("GET", "/api/admin/roles", nil, tok).Status)
	// Remove a role do admin e exclui: o cache da role some e uma recriação com o mesmo ID é impossível;
	// o relevante é que a exclusão funciona após liberar a role.
	require.Equal(t, 204, e.do("DELETE", "/api/admin/users/"+id, nil, boss).Status)
	assert.Equal(t, 204, e.do("DELETE", "/api/admin/roles/"+rid, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/roles/"+rid, nil, boss).Status)
}

func TestEscalonamentoDePrivilegioEBloqueado(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", append([]string{"admin::settings.roles.read", "admin::settings.roles.create", "admin::settings.roles.update", "admin::settings.roles.delete"}, usersAll...)...)
	e.mkRole("poderoso", "admin::settings.audit-logs.read")
	e.mkRole("leve", "admin::settings.users.read")
	gestor, gestorID := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	_, podID := e.newAdmin(boss, "pod@exemplo.com", "poderoso")
	_, leveID := e.newAdmin(boss, "leve@exemplo.com", "leve")
	perms := func(p ...map[string]any) map[string]any {
		return map[string]any{"permissions": append([]map[string]any{}, p...)}
	}
	auditRead := map[string]any{"action": "admin::settings.audit-logs.read"}
	usersRead := map[string]any{"action": "admin::settings.users.read"}

	t.Run("não altera as permissões de uma role que possui", func(t *testing.T) {
		r := e.do("PUT", "/api/admin/roles/"+e.roleID("gestor")+"/permissions", perms(usersRead), gestor)
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, "cannot_modify_own_role", r.errCode())
	})

	t.Run("só concede permissões que possui", func(t *testing.T) {
		created := e.do("POST", "/api/admin/roles", map[string]any{"name": "Nova", "description": ""}, gestor)
		require.Equal(t, 201, created.Status, created.Body)
		nova := created.data()["id"].(string)

		r := e.do("PUT", "/api/admin/roles/"+nova+"/permissions", perms(usersRead, auditRead), gestor)
		assert.Equal(t, 403, r.Status)
		assert.Equal(t, "privilege_escalation", r.errCode())
		details := r.Body["error"].(map[string]any)["details"].(map[string]any)["permissions"].([]any)
		require.Len(t, details, 1)
		assert.Equal(t, "admin::settings.audit-logs.read", details[0].(map[string]any)["action"])
		assert.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+nova+"/permissions", perms(usersRead), gestor).Status)

		// Reduzir ou manter o que já havia é sempre permitido; ampliar, não.
		assert.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+nova+"/permissions", perms(), gestor).Status)
	})

	t.Run("não duplica role com permissões que não possui", func(t *testing.T) {
		r := e.do("POST", "/api/admin/roles/"+e.roleID("poderoso")+"/duplicate", nil, gestor)
		assert.Equal(t, 403, r.Status)
		assert.Equal(t, "privilege_escalation", r.errCode())
		assert.Equal(t, 201, e.do("POST", "/api/admin/roles/"+e.roleID("leve")+"/duplicate", nil, gestor).Status)
	})

	t.Run("só atribui roles que cobre", func(t *testing.T) {
		r := e.do("POST", "/api/admin/users/"+leveID+"/roles", map[string]any{"roleIds": []string{e.roleID("poderoso")}}, gestor)
		assert.Equal(t, 403, r.Status)
		assert.Equal(t, "privilege_escalation", r.errCode())

		body := map[string]any{"cpf": cpfFor("novo@exemplo.com"), "roleIds": []string{e.roleID("poderoso")}}
		assert.Equal(t, "privilege_escalation", e.do("POST", "/api/admin/users", body, gestor).errCode())
		body["roleIds"] = []string{e.roleID("leve")}
		assert.Equal(t, 201, e.do("POST", "/api/admin/users", body, gestor).Status)
	})

	t.Run("não age sobre quem tem mais poder", func(t *testing.T) {
		roles := map[string]any{"roleIds": []string{e.roleID("leve")}}
		for _, c := range []struct {
			method, path string
			body         any
		}{
			{"PUT", "/api/admin/users/" + podID, map[string]any{"departmentId": nil}}, {"DELETE", "/api/admin/users/" + podID, nil},
			{"POST", "/api/admin/users/" + podID + "/deactivate", nil}, {"POST", "/api/admin/users/" + podID + "/roles", roles},
			{"DELETE", "/api/admin/users/" + podID + "/roles/" + e.roleID("poderoso"), nil},
		} {
			r := e.do(c.method, c.path, c.body, gestor)
			assert.Equal(t, 403, r.Status, c.method+" "+c.path)
			assert.Equal(t, "target_more_privileged", r.errCode(), c.method+" "+c.path)
		}
		// Sobre quem tem menos poder, pode.
		assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+leveID+"/deactivate", nil, gestor).Status)
	})

	t.Run("não altera a própria conta", func(t *testing.T) {
		assert.Equal(t, "cannot_modify_self", e.do("PUT", "/api/admin/users/"+gestorID, map[string]any{"departmentId": nil}, gestor).errCode())
		assert.Equal(t, "cannot_modify_self", e.do("POST", "/api/admin/users/"+gestorID+"/roles", map[string]any{"roleIds": []string{e.roleID("leve")}}, gestor).errCode())
		assert.Equal(t, "cannot_modify_self", e.do("DELETE", "/api/admin/users/"+gestorID+"/roles/"+e.roleID("gestor"), nil, gestor).errCode())
	})

	t.Run("Super Admin não tem essas restrições", func(t *testing.T) {
		assert.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+e.roleID("gestor")+"/permissions", perms(usersRead, auditRead), boss).Status)
		assert.Equal(t, 201, e.do("POST", "/api/admin/roles/"+e.roleID("poderoso")+"/duplicate", nil, boss).Status)
		assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+podID+"/deactivate", nil, boss).Status)
	})
}

func TestAguardandoLiberacaoELiberacaoPorSubstituicaoDeRoles(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("leitor", "admin::settings.users.read")
	e.mkRole("redator", "admin::settings.audit-logs.read")
	// A migração já cria o Proponente (que o primeiro acesso receberia); aqui vale o cenário sem ele.
	_, err := e.pool.Exec(context.Background(), "DELETE FROM admin_roles WHERE code = 'proponent'")
	require.NoError(t, err)

	// Quem entra pelo MT Login sem pré-cadastro fica ativo, sem roles e aguardando liberação.
	maria := e.login("maria@exemplo.com")
	mariaID := e.adminID("maria@exemplo.com")
	assert.Equal(t, "awaiting", e.do("GET", "/api/admin/users/"+mariaID, nil, boss).data()["status"])

	// Pré-cadastrado que ainda não entrou continua pending; ana segue active.
	require.Equal(t, 201, e.do("POST", "/api/admin/users", map[string]any{"cpf": cpfFor("pre@exemplo.com"), "roleIds": []string{e.roleID("leitor")}}, boss).Status)
	statusOf := func(status string) []any { return e.do("GET", "/api/admin/users?status="+status, nil, boss).list() }
	require.Len(t, statusOf("awaiting"), 1)
	assert.Equal(t, mariaID, statusOf("awaiting")[0].(map[string]any)["id"])
	assert.Len(t, statusOf("pending"), 1)
	assert.Len(t, statusOf("active"), 1, "só a Ana; quem não tem role não é active")

	// Liberar: define o conjunto de roles. Vale na requisição seguinte.
	r := e.do("PUT", "/api/admin/users/"+mariaID+"/roles", map[string]any{"roleIds": []string{e.roleID("leitor"), e.roleID("redator")}}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "active", r.data()["status"])
	assert.Len(t, r.data()["roles"], 2)
	assert.Empty(t, statusOf("awaiting"))
	assert.Equal(t, 200, e.do("GET", "/api/admin/users", nil, maria).Status)

	// O conjunto é substituído: roles ausentes saem.
	r = e.do("PUT", "/api/admin/users/"+mariaID+"/roles", map[string]any{"roleIds": []string{e.roleID("redator")}}, boss)
	require.Equal(t, 200, r.Status)
	assert.Len(t, r.data()["roles"], 1)
	assert.Equal(t, 403, e.do("GET", "/api/admin/users", nil, maria).Status)

	// Validações.
	assert.Equal(t, 422, e.do("PUT", "/api/admin/users/"+mariaID+"/roles", map[string]any{"roleIds": []string{}}, boss).Status)
	assert.Equal(t, 422, e.do("PUT", "/api/admin/users/"+mariaID+"/roles", map[string]any{"roleIds": []string{uuid.NewString()}}, boss).Status)
	assert.Equal(t, 404, e.do("PUT", "/api/admin/users/"+uuid.NewString()+"/roles", map[string]any{"roleIds": []string{e.roleID("leitor")}}, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/users/"+uuid.NewString()+"/permissions", nil, boss).Status)
}

func TestSubstituirRolesRespeitaSuperAdminEEscalonamento(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	e.mkRole("poderoso", "admin::settings.audit-logs.read")
	gestor, _ := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	// Maria entraria como Proponente (criado pela migração); o teste parte de quem não tem role nenhuma.
	_, err := e.pool.Exec(context.Background(), "DELETE FROM admin_roles WHERE code = 'proponent'")
	require.NoError(t, err)
	e.login("maria@exemplo.com")
	maria := e.adminID("maria@exemplo.com")
	ana := e.adminID("ana@exemplo.com")
	put := func(token, id string, roles ...string) resp {
		return e.do("PUT", "/api/admin/users/"+id+"/roles", map[string]any{"roleIds": roles}, token)
	}

	// Só Super Admin concede a role Super Admin; quem não é, também não cobre o que não tem.
	assert.Equal(t, 403, put(gestor, maria, e.roleID("super-admin")).Status)
	assert.Equal(t, "privilege_escalation", put(gestor, maria, e.roleID("poderoso")).errCode())
	assert.Equal(t, 200, put(gestor, maria, e.roleID("gestor")).Status, "o gestor tem tudo o que a role gestor concede")

	// O último Super Admin ativo não perde a role.
	r := put(boss, maria, e.roleID("super-admin"))
	require.Equal(t, 200, r.Status, r.Body)
	r = e.do("PUT", "/api/admin/users/"+ana+"/roles", map[string]any{"roleIds": []string{e.roleID("gestor")}}, e.token("maria@exemplo.com"))
	require.Equal(t, 200, r.Status, "agora há dois Super Admins")
	// Resta só a Maria como Super Admin: ela não pode se rebaixar (conta própria é livre para Super Admin, mas protegida pelo último).
	r = put(e.token("maria@exemplo.com"), maria, e.roleID("gestor"))
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "last_super_admin", r.errCode())
}

func TestBloquearEDesbloquearAdministrador(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("gestor", usersAll...)
	e.mkRole("redator")
	gestor, gestorID := e.newAdmin(boss, "gestor@exemplo.com", "gestor")
	bia, biaID := e.newAdmin(boss, "bia@exemplo.com", "redator")
	ana := e.adminID("ana@exemplo.com")

	r := e.do("POST", "/api/admin/users/"+biaID+"/block", nil, gestor)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "blocked", r.data()["status"])
	assert.Len(t, r.data()["roles"], 1, "bloquear mantém as roles")
	me := e.do("GET", "/api/admin/auth/me", nil, bia)
	assert.Equal(t, 403, me.Status)
	assert.Equal(t, "account_disabled", me.errCode())
	assert.Len(t, e.do("GET", "/api/admin/users?status=blocked", nil, boss).list(), 1)

	r = e.do("POST", "/api/admin/users/"+biaID+"/unblock", nil, gestor)
	require.Equal(t, 200, r.Status)
	assert.Equal(t, "active", r.data()["status"])
	assert.Equal(t, 200, e.do("GET", "/api/admin/auth/me", nil, bia).Status)

	assert.Equal(t, "cannot_modify_self", e.do("POST", "/api/admin/users/"+gestorID+"/block", nil, gestor).errCode())
	assert.Equal(t, "target_more_privileged", e.do("POST", "/api/admin/users/"+ana+"/block", nil, gestor).errCode())
	assert.Equal(t, 404, e.do("POST", "/api/admin/users/"+uuid.NewString()+"/block", nil, boss).Status)

	// Com dois Super Admins, um pode bloquear o outro; o bloqueio é auditado.
	assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+gestorID+"/roles", map[string]any{"roleIds": []string{e.roleID("super-admin")}}, boss).Status)
	assert.Equal(t, 200, e.do("POST", "/api/admin/users/"+ana+"/block", nil, gestor).Status)
	logs := e.do("GET", "/api/admin/audit-logs?action=admin.block", nil, gestor)
	assert.EqualValues(t, 2, logs.meta()["total"])
}

func TestPermissoesEfetivasDeOutroAdministrador(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("leitor", "admin::settings.users.read")
	e.grant("leitor", "admin::content.document.read", `["title"]`, `["admin::is-creator"]`)
	_, leitorID := e.newAdmin(boss, "leitor@exemplo.com", "leitor")
	ana := e.adminID("ana@exemplo.com")

	r := e.do("GET", "/api/admin/users/"+leitorID+"/permissions", nil, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, false, r.data()["isSuperAdmin"])
	perms := r.data()["permissions"].([]any)
	require.Len(t, perms, 2)
	byAction := map[string]map[string]any{}
	for _, p := range perms {
		byAction[p.(map[string]any)["action"].(string)] = p.(map[string]any)
	}
	doc := byAction["admin::content.document.read"]
	assert.Equal(t, []any{"title"}, doc["fields"])
	assert.Equal(t, []any{"admin::is-creator"}, doc["conditions"])

	su := e.do("GET", "/api/admin/users/"+ana+"/permissions", nil, boss)
	assert.Equal(t, true, su.data()["isSuperAdmin"])
	assert.NotEmpty(t, su.data()["permissions"])
}

func TestListagemDeRolesAbertaAQuemAdministraAdministradores(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("leitor", "admin::settings.users.read")
	e.mkRole("vazio")
	leitor, _ := e.newAdmin(boss, "leitor@exemplo.com", "leitor")
	vazio, _ := e.newAdmin(boss, "vazio@exemplo.com", "vazio")

	assert.Equal(t, 200, e.do("GET", "/api/admin/roles", nil, leitor).Status, "alimenta o seletor de roles")
	assert.Equal(t, 403, e.do("GET", "/api/admin/roles", nil, vazio).Status)
	assert.Equal(t, 401, e.do("GET", "/api/admin/roles", nil, "").Status)
	assert.Equal(t, 403, e.do("GET", "/api/admin/roles/"+e.roleID("vazio"), nil, leitor).Status, "as permissões continuam exigindo roles.read")
}

func TestCatalogoDeAcoesExigeLeituraDeRoles(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("leitor", "admin::settings.roles.read")
	e.mkRole("vazio")
	leitor, _ := e.newAdmin(boss, "leitor@exemplo.com", "leitor")
	vazio, _ := e.newAdmin(boss, "vazio@exemplo.com", "vazio")

	for _, tok := range []string{leitor, boss} {
		assert.Equal(t, 200, e.do("GET", "/api/admin/catalog/actions", nil, tok).Status)
	}
	assert.Equal(t, 403, e.do("GET", "/api/admin/catalog/actions", nil, vazio).Status)
	assert.Equal(t, 401, e.do("GET", "/api/admin/catalog/actions", nil, "").Status)
}
