package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appPkg "github.com/wagner/convenios-e-fomentos/backend/internal/app"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/seed"
	"github.com/wagner/convenios-e-fomentos/backend/internal/testutil"
)

// runSeed executa o seed e devolve as senhas geradas no log (email → senha): só o usuário final de
// desenvolvimento tem senha, os administradores entram pelo MT Login.
func (e *env) runSeed(development bool, superAdminCPFs ...string) map[string]string {
	e.t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	require.NoError(e.t, seed.Run(context.Background(), seed.Deps{Pool: e.pool, Hasher: appPkg.Hasher(e.cfg), Engine: e.app.Authz,
		EndUsers: e.app.EndUsers, Log: log, Development: development, SuperAdminCPFs: superAdminCPFs}))
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(e.t, json.Unmarshal([]byte(line), &rec))
		if email, ok := rec["email"].(string); ok {
			if senha, _ := rec["senha"].(string); senha != "" {
				out[email] = senha
			}
		}
	}
	return out
}

func (e *env) deptID(name string) string {
	e.t.Helper()
	var id uuid.UUID
	require.NoError(e.t, e.pool.QueryRow(context.Background(), "SELECT id FROM departments WHERE name=$1", name).Scan(&id))
	return id.String()
}

// devToken é o access token do MT Login do administrador de desenvolvimento do seed (pelo CPF fictício).
func (e *env) devToken(email string) string {
	e.t.Helper()
	for _, u := range seed.DevUsers {
		if u.Email == email {
			tok := e.oidc.Token(testutil.Identity{CPF: u.CPF, Email: u.Email, Given: u.First, Family: u.Last})
			require.Equal(e.t, 200, e.do("GET", "/api/admin/auth/me", nil, tok).Status)
			return tok
		}
	}
	require.Fail(e.t, "usuário de desenvolvimento inexistente", email)
	return ""
}

// seeded devolve o ambiente já semeado, o token do Super Admin e uma função de login dos usuários de desenvolvimento.
func seeded(t *testing.T) (*env, string, func(email string) string) {
	e, boss := setup(t)
	e.runSeed(true)
	return e, boss, e.devToken
}

func (e *env) newDoc(token string, body map[string]any) resp {
	return e.do("POST", "/api/admin/content/documents", body, token)
}

func (e *env) docID(token string, body map[string]any) string {
	e.t.Helper()
	r := e.newDoc(token, body)
	require.Equal(e.t, 201, r.Status, r.Body)
	return r.data()["id"].(string)
}

func TestSeedIdempotenteEEstrutura(t *testing.T) {
	e, boss := setup(t)
	first := e.runSeed(true)
	assert.Len(t, first, 1, "só o usuário final de desenvolvimento tem senha; os administradores usam o MT Login")
	for email, pw := range first {
		assert.GreaterOrEqual(t, len(pw), 12, email)
	}
	count := func(q string, args ...any) int {
		var n int
		require.NoError(t, e.pool.QueryRow(context.Background(), q, args...).Scan(&n))
		return n
	}
	snapshot := func() []int {
		return []int{
			count("SELECT count(*) FROM admin_roles"), count("SELECT count(*) FROM admin_permissions"), count("SELECT count(*) FROM departments"),
			count("SELECT count(*) FROM admins"), count("SELECT count(*) FROM end_users"), count("SELECT count(*) FROM end_user_permissions"),
			count("SELECT count(*) FROM email_templates"), count("SELECT count(*) FROM end_user_roles"),
		}
	}
	before := snapshot()
	assert.Equal(t, []int{8, 63, 2, 9, 1, 11, 2, 2}, before, "8 roles de admin (inclui Proponente, Supervisor, Analista e Secretário), 63 permissões e 9 administradores (o chefe e 8 de desenvolvimento)")

	// Segunda execução: nada novo, e as senhas não são repetidas no log.
	assert.Empty(t, e.runSeed(true))
	assert.Equal(t, before, snapshot())

	// Permissões por role.
	perms := func(code string) map[string]string {
		rows, err := e.pool.Query(context.Background(), `SELECT p.action, coalesce(p.fields::text,'null') || ' ' || p.conditions::text
			FROM admin_permissions p JOIN admin_roles r ON r.id=p.role_id WHERE r.code=$1`, code)
		require.NoError(t, err)
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var a, c string
			require.NoError(t, rows.Scan(&a, &c))
			out[a] = c
		}
		return out
	}
	assert.Empty(t, perms("super-admin"), "Super Admin é curinga, sem linhas")
	assert.Len(t, perms("editor"), 8)
	author := perms("author")
	assert.Len(t, author, 7)
	assert.Contains(t, author["admin::content.document.update"], `["admin::is-creator"]`)
	assert.Contains(t, author["admin::content.document.delete"], `["admin::is-creator"]`)
	assert.NotContains(t, author, "admin::content.document.publish", "Author não publica")
	rev := perms("department-reviewer")
	assert.NotContains(t, rev, "admin::content.document.update", "o Revisor não tem mais permissões sobre documentos")
	assert.Equal(t, `null ["admin::project-submitted-same-department"]`, rev["admin::projects.read"], "o Revisor lê só projetos enviados do próprio departamento")

	// Roles de usuário final.
	endPerms := func(typ string) int {
		return count("SELECT count(*) FROM end_user_permissions p JOIN end_user_roles r ON r.id=p.role_id WHERE r.type=$1", typ)
	}
	assert.Equal(t, 4, endPerms("public"))
	assert.Equal(t, 7, endPerms("authenticated"))

	// Alterações feitas depois não são desfeitas por um novo seed.
	author2 := e.roleID("author")
	require.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+author2+"/permissions", map[string]any{"permissions": []map[string]any{{"action": "admin::content.document.read"}}}, boss).Status)
	pub := e.endRoleID("public")
	require.Equal(t, 200, e.do("PUT", "/api/admin/end-user-roles/"+pub, map[string]any{"name": "Public", "permissions": []string{}}, boss).Status)
	e.runSeed(true)
	assert.Len(t, perms("author"), 1)
	assert.Equal(t, 0, endPerms("public"))

	// Fora do desenvolvimento não há usuários de exemplo.
	e2, _ := setup(t)
	assert.Empty(t, e2.runSeed(false))
	var n int
	require.NoError(t, e2.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, e2.pool.QueryRow(context.Background(), "SELECT count(*) FROM admin_roles").Scan(&n))
	assert.Equal(t, 8, n)
}

func TestUsuariosDeDesenvolvimentoFuncionam(t *testing.T) {
	e, _ := setup(t)
	pw := e.runSeed(true)
	for _, email := range []string{"super@dev.local", "editor@dev.local", "author@dev.local", "revisor@dev.local", "revisor.juridico@dev.local", "analista@dev.local", "supervisor@dev.local", "secretario@dev.local"} {
		me := e.do("GET", "/api/admin/auth/me", nil, e.devToken(email))
		assert.Equal(t, 200, me.Status, email)
	}
	assert.Equal(t, true, e.do("GET", "/api/admin/auth/me", nil, e.devToken("super@dev.local")).data()["isSuperAdmin"])
	assert.Equal(t, 200, e.userLogin("usuario@dev.local", pw["usuario@dev.local"]).Status)
	assert.Equal(t, 200, e.userLogin("usuario", pw["usuario@dev.local"]).Status)
	me := e.do("GET", "/api/admin/auth/me", nil, e.devToken("author@dev.local"))
	assert.Equal(t, e.deptID("Financeiro"), me.data()["departmentId"])
}

func TestSeedGarantiSuperAdminsPorCPF(t *testing.T) {
	e := newEnv(t)
	cpfs := []string{cpfFor("lia@exemplo.com"), " ", cpf.Format(cpfFor("davi@exemplo.com"))}
	e.runSeed(false, cpfs...)
	e.runSeed(false, cpfs...) // idempotente

	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), `SELECT count(*) FROM admins a JOIN admin_users_roles ur ON ur.admin_id=a.id
		JOIN admin_roles r ON r.id=ur.role_id WHERE r.code='super-admin'`).Scan(&n))
	assert.Equal(t, 2, n)

	// No primeiro acesso a pessoa assume o registro e já é Super Admin.
	me := e.do("GET", "/api/admin/auth/me", nil, e.token("lia@exemplo.com"))
	require.Equal(t, 200, me.Status)
	assert.Equal(t, true, me.data()["isSuperAdmin"])
	assert.Equal(t, "lia@exemplo.com", me.data()["email"])

	// CPF inválido derruba o seed, sem expor o valor digitado.
	err := seed.Run(context.Background(), seed.Deps{Pool: e.pool, Hasher: appPkg.Hasher(e.cfg), Engine: e.app.Authz, EndUsers: e.app.EndUsers,
		Log: slog.New(slog.NewJSONHandler(io.Discard, nil)), SuperAdminCPFs: []string{"123.456.789-00"}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "123.456.789-00")
}

func TestEditorTemControleTotalSobreDocumentos(t *testing.T) {
	e, _, login := seeded(t)
	editor := login("editor@dev.local")

	r := e.newDoc(editor, map[string]any{"titulo": "Primeiro", "conteudo": "texto", "observacao_interna": "nota"})
	require.Equal(t, 201, r.Status, r.Body)
	id := r.data()["id"].(string)
	assert.Equal(t, "rascunho", r.data()["status"])
	assert.Nil(t, r.data()["published_at"])
	assert.NotEmpty(t, r.data()["created_by_id"])
	assert.Equal(t, r.data()["created_by_id"], r.data()["updated_by_id"])

	u := e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"titulo": "Editado", "department_id": e.deptID("Jurídico")}, editor)
	require.Equal(t, 200, u.Status, u.Body)
	assert.Equal(t, "Editado", u.data()["titulo"])
	assert.Equal(t, "texto", u.data()["conteudo"], "campos ausentes não mudam")
	assert.Equal(t, e.deptID("Jurídico"), u.data()["department_id"])
	u = e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"department_id": nil}, editor)
	assert.Nil(t, u.data()["department_id"], "null limpa o campo")

	pub := e.do("POST", "/api/admin/content/documents/"+id+"/publish", nil, editor)
	require.Equal(t, 200, pub.Status)
	assert.Equal(t, "publicado", pub.data()["status"])
	assert.NotNil(t, pub.data()["published_at"])
	unpub := e.do("POST", "/api/admin/content/documents/"+id+"/unpublish", nil, editor)
	assert.Equal(t, "rascunho", unpub.data()["status"])
	assert.Nil(t, unpub.data()["published_at"])
	// Mudar o status pelo PUT equivale a publicar.
	up := e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"status": "publicado"}, editor)
	assert.Equal(t, "publicado", up.data()["status"])
	assert.NotNil(t, up.data()["published_at"])

	assert.Equal(t, 200, e.do("GET", "/api/admin/content/documents/"+id, nil, editor).Status)
	assert.Equal(t, 204, e.do("DELETE", "/api/admin/content/documents/"+id, nil, editor).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/content/documents/"+id, nil, editor).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/content/documents/"+id, nil, editor).Status)
	assert.Equal(t, 404, e.do("POST", "/api/admin/content/documents/"+uuid.NewString()+"/publish", nil, editor).Status)
	assert.Equal(t, 404, e.do("PUT", "/api/admin/content/documents/xyz", map[string]any{}, editor).Status)
}

func TestValidacaoECamposDeSistemaNuncaGraváveis(t *testing.T) {
	e, boss, login := seeded(t)
	editor := login("editor@dev.local")
	id := e.docID(editor, map[string]any{"titulo": "Doc"})
	fieldsOf := func(r resp) []string {
		var out []string
		for _, f := range r.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any) {
			out = append(out, f.(map[string]any)["field"].(string))
		}
		return out
	}

	// Campos de sistema: recusados até para o Super Admin, listando quais são.
	for _, field := range []string{"id", "created_at", "updated_at", "created_by_id", "updated_by_id", "published_at", "owner_user_id"} {
		for _, tok := range []string{editor, boss} {
			r := e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"titulo": "x", field: "valor"}, tok)
			if field == "id" || strings.HasSuffix(field, "_id") || strings.HasSuffix(field, "_at") {
				assert.Equal(t, 422, r.Status, field)
				assert.Equal(t, []string{field}, fieldsOf(r), field)
			}
		}
	}
	created := e.do("POST", "/api/admin/content/documents", map[string]any{"titulo": "T", "created_by_id": uuid.NewString(), "id": uuid.NewString()}, editor)
	assert.Equal(t, 422, created.Status)
	assert.Equal(t, []string{"created_by_id", "id"}, fieldsOf(created))

	cases := map[string]map[string]any{
		"campo desconhecido": {"titulo": "x", "inexistente": 1},
		"título vazio":       {"titulo": "  "},
		"título longo":       {"titulo": strings.Repeat("a", 201)},
		"status inválido":    {"status": "arquivado"},
		"tipo errado":        {"titulo": 123},
		"título nulo":        {"titulo": nil},
		"departamento":       {"department_id": uuid.NewString()},
		"uuid inválido":      {"department_id": "x"},
		"observação longa":   {"observacao_interna": strings.Repeat("a", 5001)},
	}
	for name, body := range cases {
		assert.Equal(t, 422, e.do("PUT", "/api/admin/content/documents/"+id, body, editor).Status, name)
	}
	assert.Equal(t, 422, e.newDoc(editor, map[string]any{"conteudo": "sem título"}).Status)
	assert.Equal(t, 422, e.newDoc(editor, map[string]any{"titulo": "T", "status": "publicado"}).Status, "nasce como rascunho")
	assert.Equal(t, 400, e.do("POST", "/api/admin/content/documents", []string{"x"}, editor).Status)
	d := e.do("GET", "/api/admin/content/documents/"+id, nil, editor)
	assert.Equal(t, "Doc", d.data()["titulo"], "nenhuma requisição recusada alterou o documento")
}

func TestAuthorEditaSoOQueCriou(t *testing.T) {
	e, _, login := seeded(t)
	author, editor := login("author@dev.local"), login("editor@dev.local")

	mine := e.docID(author, map[string]any{"titulo": "Meu", "conteudo": "c"})
	theirs := e.docID(editor, map[string]any{"titulo": "Do editor", "conteudo": "c"})
	var dept string
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT department_id::text FROM documents WHERE id=$1", mine).Scan(&dept))
	assert.Equal(t, e.deptID("Financeiro"), dept, "sem department_id, usa o do criador")

	assert.Equal(t, 200, e.do("PUT", "/api/admin/content/documents/"+mine, map[string]any{"titulo": "Meu 2"}, author).Status, "próprio: permitido")
	deny := e.do("PUT", "/api/admin/content/documents/"+theirs, map[string]any{"titulo": "invadido"}, author)
	assert.Equal(t, 403, deny.Status, "de outro: negado")
	assert.Equal(t, "forbidden", deny.errCode())
	assert.Equal(t, 403, e.do("DELETE", "/api/admin/content/documents/"+theirs, nil, author).Status)
	assert.Equal(t, "Do editor", e.do("GET", "/api/admin/content/documents/"+theirs, nil, author).data()["titulo"], "Author lê tudo")

	// Sem publicar: nem pela ação, nem mudando o status.
	pub := e.do("POST", "/api/admin/content/documents/"+mine+"/publish", nil, author)
	assert.Equal(t, 403, pub.Status)
	st := e.do("PUT", "/api/admin/content/documents/"+mine, map[string]any{"status": "publicado"}, author)
	assert.Equal(t, 422, st.Status, "o campo status não está liberado ao Author")

	assert.Equal(t, 204, e.do("DELETE", "/api/admin/content/documents/"+mine, nil, author).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/content/documents/"+theirs, nil, editor).Status, "o doc do editor segue intacto")

	// O Author lista tudo, mas só consegue editar os próprios (a UI usa as permissões efetivas do /me).
	me := e.do("GET", "/api/admin/auth/me", nil, author)
	var upd map[string]any
	for _, p := range me.data()["permissions"].([]any) {
		if p.(map[string]any)["action"] == "admin::content.document.update" {
			upd = p.(map[string]any)
		}
	}
	require.NotNil(t, upd)
	assert.Equal(t, []any{"admin::is-creator"}, upd["conditions"])
	assert.Equal(t, "document", upd["subject"])
}

// grantLegacyReviewerDocs devolve ao Revisor do Departamento as permissões sobre documentos que ele tinha antes da migração 00024.
// O perfil real não as tem mais, mas a condição same-department e a restrição por campo continuam valendo para qualquer perfil que
// as receba; estes testes seguem cobrindo isso com o Revisor, que já tem departamento no cadastro.
func grantLegacyReviewerDocs(e *env) {
	e.t.Helper()
	e.grant("department-reviewer", "admin::content.document.read", "", "[]")
	e.grant("department-reviewer", "admin::content.document.file.read", "", "[]")
	e.grant("department-reviewer", "admin::content.document.update", `["status","observacao_interna"]`, `["admin::same-department"]`)
	e.grant("department-reviewer", "admin::content.document.publish", "", `["admin::same-department"]`)
}

func TestRevisorDoDepartamento(t *testing.T) {
	e, _, login := seeded(t)
	grantLegacyReviewerDocs(e)
	editor, rev := login("editor@dev.local"), login("revisor@dev.local")
	fin, jur := e.deptID("Financeiro"), e.deptID("Jurídico")
	a := e.docID(editor, map[string]any{"titulo": "Financeiro 1", "department_id": fin})
	b := e.docID(editor, map[string]any{"titulo": "Jurídico 1", "department_id": jur})

	// Campo permitido (observacao_interna) no documento do próprio departamento.
	ok := e.do("PUT", "/api/admin/content/documents/"+a, map[string]any{"observacao_interna": "revisado"}, rev)
	require.Equal(t, 200, ok.Status, ok.Body)
	assert.Equal(t, "revisado", ok.data()["observacao_interna"])
	assert.Equal(t, "Financeiro 1", ok.data()["titulo"])

	// Campo proibido: 422 listando o campo; nada é gravado, mesmo junto de um campo permitido.
	bad := e.do("PUT", "/api/admin/content/documents/"+a, map[string]any{"titulo": "hackeado", "observacao_interna": "outra"}, rev)
	assert.Equal(t, 422, bad.Status)
	fs := bad.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any)
	require.Len(t, fs, 1)
	assert.Equal(t, "titulo", fs[0].(map[string]any)["field"])
	cur := e.do("GET", "/api/admin/content/documents/"+a, nil, editor)
	assert.Equal(t, "Financeiro 1", cur.data()["titulo"])
	assert.Equal(t, "revisado", cur.data()["observacao_interna"])
	assert.Equal(t, 422, e.do("PUT", "/api/admin/content/documents/"+a, map[string]any{"conteudo": "x"}, rev).Status)
	assert.Equal(t, 422, e.do("PUT", "/api/admin/content/documents/"+a, map[string]any{"department_id": jur}, rev).Status, "não move o documento de departamento")

	// Outro departamento: negado (403), mesmo com campo permitido.
	assert.Equal(t, 403, e.do("PUT", "/api/admin/content/documents/"+b, map[string]any{"observacao_interna": "x"}, rev).Status)
	assert.Equal(t, 403, e.do("POST", "/api/admin/content/documents/"+b+"/publish", nil, rev).Status)
	assert.Equal(t, 403, e.do("PUT", "/api/admin/content/documents/"+b, map[string]any{"status": "publicado"}, rev).Status)

	// Status do próprio departamento (campo liberado + ação publicar liberada).
	st := e.do("PUT", "/api/admin/content/documents/"+a, map[string]any{"status": "publicado"}, rev)
	require.Equal(t, 200, st.Status, st.Body)
	assert.Equal(t, "publicado", st.data()["status"])
	assert.Equal(t, 200, e.do("POST", "/api/admin/content/documents/"+a+"/unpublish", nil, rev).Status)

	// Sem criar nem excluir.
	assert.Equal(t, 403, e.newDoc(rev, map[string]any{"titulo": "x"}).Status)
	assert.Equal(t, 403, e.do("DELETE", "/api/admin/content/documents/"+a, nil, rev).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/content/documents/"+b, nil, rev).Status, "lê todos")
}

func TestMudarStatusExigeAcaoPublicarMesmoComCampoLiberado(t *testing.T) {
	e, boss, login := seeded(t)
	editor := login("editor@dev.local")
	id := e.docID(editor, map[string]any{"titulo": "Doc"})
	// Role com update liberando todos os campos (inclusive status), mas sem a ação publish.
	e.mkRole("atualizador")
	e.grant("atualizador", "admin::content.document.update", "", "[]")
	e.grant("atualizador", "admin::content.document.read", "", "[]")
	tok, _ := e.newAdmin(boss, "atu@exemplo.com", "atualizador")

	assert.Equal(t, 200, e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"titulo": "novo"}, tok).Status)
	r := e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"status": "publicado"}, tok)
	assert.Equal(t, 403, r.Status)
	assert.Equal(t, "admin::content.document.publish", r.Body["error"].(map[string]any)["details"].(map[string]any)["action"])
	assert.Equal(t, "rascunho", e.do("GET", "/api/admin/content/documents/"+id, nil, editor).data()["status"])
	// Repetir o mesmo status não é uma transição.
	assert.Equal(t, 200, e.do("PUT", "/api/admin/content/documents/"+id, map[string]any{"status": "rascunho"}, tok).Status)
}

func TestPermissaoDeCamposEFiltroNaLeitura(t *testing.T) {
	e, boss, login := seeded(t)
	editor := login("editor@dev.local")
	fin, jur := e.deptID("Financeiro"), e.deptID("Jurídico")

	// Role: lê só o título dos documentos que ela mesma criou.
	e.mkRole("leitor-proprio")
	e.grant("leitor-proprio", "admin::content.document.read", `["titulo"]`, `["admin::is-creator"]`)
	e.grant("leitor-proprio", "admin::content.document.create", "", "[]")
	tok, _ := e.newAdmin(boss, "lp@exemplo.com", "leitor-proprio")

	mine := e.docID(tok, map[string]any{"titulo": "Meu", "conteudo": "segredo", "observacao_interna": "interno"})
	theirs := e.docID(editor, map[string]any{"titulo": "Alheio", "conteudo": "c", "department_id": fin})

	list := e.do("GET", "/api/admin/content/documents", nil, tok)
	require.Equal(t, 200, list.Status)
	require.Len(t, list.list(), 1, "o filtro de permissão restringe a listagem")
	assert.EqualValues(t, 1, list.meta()["total"], "e a contagem da paginação")
	item := list.list()[0].(map[string]any)
	assert.Equal(t, mine, item["id"])
	assert.Equal(t, "Meu", item["titulo"])
	for _, hidden := range []string{"conteudo", "observacao_interna", "status", "department_id"} {
		assert.NotContains(t, item, hidden, hidden)
	}
	for _, system := range []string{"id", "created_at", "updated_at", "created_by_id", "updated_by_id", "published_at"} {
		assert.Contains(t, item, system, "campos de sistema são sempre legíveis")
	}
	assert.Equal(t, 403, e.do("GET", "/api/admin/content/documents/"+theirs, nil, tok).Status)
	one := e.do("GET", "/api/admin/content/documents/"+mine, nil, tok)
	assert.NotContains(t, one.data(), "conteudo")

	// A resposta do create também respeita os campos de leitura.
	cr := e.newDoc(tok, map[string]any{"titulo": "Outro", "conteudo": "x"})
	assert.NotContains(t, cr.data(), "conteudo")
	assert.Contains(t, cr.data(), "titulo")

	// Filtros do usuário se combinam (AND) com o de permissão: nunca o ampliam.
	assert.Len(t, e.do("GET", "/api/admin/content/documents?q=Alheio", nil, tok).list(), 0)
	assert.Len(t, e.do("GET", "/api/admin/content/documents?q=Meu", nil, tok).list(), 1)
	assert.Len(t, e.do("GET", "/api/admin/content/documents?department_id="+fin, nil, tok).list(), 0)

	// União entre roles, campo a campo e registro a registro: a 2ª role lê todos os campos
	// dos documentos do departamento do administrador.
	e.mkRole("leitor-depto")
	e.grant("leitor-depto", "admin::content.document.read", "", `["admin::same-department"]`)
	both, bothID := e.newAdmin(boss, "ld@exemplo.com", "leitor-proprio")
	require.Equal(t, 200, e.do("PUT", "/api/admin/users/"+bothID, map[string]any{"departmentId": fin}, boss).Status)
	require.Equal(t, 200, e.do("POST", "/api/admin/users/"+bothID+"/roles", map[string]any{"roleIds": []string{e.roleID("leitor-depto")}}, boss).Status)
	l := e.do("GET", "/api/admin/content/documents?sort=titulo:asc", nil, both)
	require.Equal(t, 200, l.Status, l.Body)
	byTitle := map[string]map[string]any{}
	for _, it := range l.list() {
		byTitle[it.(map[string]any)["titulo"].(string)] = it.(map[string]any)
	}
	require.Contains(t, byTitle, "Alheio", "documento do departamento dele, via 2ª role")
	assert.Equal(t, "c", byTitle["Alheio"]["conteudo"], "campos completos pela role same-department")
	_ = jur
}

func TestListagemAdminPaginacaoOrdenacaoEFiltros(t *testing.T) {
	e, _, login := seeded(t)
	editor, author := login("editor@dev.local"), login("author@dev.local")
	fin, jur := e.deptID("Financeiro"), e.deptID("Jurídico")
	for i, t0 := range []string{"Beta", "alfa", "Gama", "Delta", "épsilon", "Zeta", "Eta"} {
		dept := fin
		if i%2 == 1 {
			dept = jur
		}
		id := e.docID(editor, map[string]any{"titulo": t0, "conteudo": "texto " + t0, "department_id": dept})
		if i < 3 {
			e.do("POST", "/api/admin/content/documents/"+id+"/publish", nil, editor)
		}
	}
	e.docID(author, map[string]any{"titulo": "Do autor"})

	all := e.do("GET", "/api/admin/content/documents", nil, editor)
	assert.EqualValues(t, 8, all.meta()["total"])
	p2 := e.do("GET", "/api/admin/content/documents?pageSize=3&page=2&sort=titulo:asc", nil, editor)
	assert.EqualValues(t, 3, p2.meta()["pageCount"])
	assert.Len(t, p2.list(), 3)
	last := e.do("GET", "/api/admin/content/documents?pageSize=3&page=3&sort=titulo:asc", nil, editor)
	assert.Len(t, last.list(), 2)
	assert.Len(t, e.do("GET", "/api/admin/content/documents?pageSize=3&page=9", nil, editor).list(), 0)
	assert.EqualValues(t, 100, e.do("GET", "/api/admin/content/documents?pageSize=10000", nil, editor).meta()["pageSize"], "limite máximo")

	// Ordenação estável e sem repetir registros entre páginas.
	seen := map[string]bool{}
	for page := 1; page <= 3; page++ {
		for _, it := range e.do("GET", fmt.Sprintf("/api/admin/content/documents?pageSize=3&page=%d&sort=status:asc", page), nil, editor).list() {
			id := it.(map[string]any)["id"].(string)
			assert.False(t, seen[id], "repetido entre páginas")
			seen[id] = true
		}
	}
	assert.Len(t, seen, 8)

	count := func(q string) int { return len(e.do("GET", "/api/admin/content/documents?"+q, nil, editor).list()) }
	assert.Equal(t, 3, count("status=publicado"))
	assert.Equal(t, 5, count("status=rascunho"))
	assert.Equal(t, 1, count("q=alfa"))
	assert.Equal(t, 1, count("q="+url.QueryEscape("Do autor")))
	assert.Equal(t, 0, count("q=%25"), "% é literal")
	assert.Equal(t, 3, count("department_id="+jur))
	assert.Equal(t, 5, count("department_id="+fin), "4 do editor + 1 do autor, que herda o departamento dele")
	assert.Equal(t, 1, count("status=publicado&department_id="+jur+"&q=alfa"))
	first := e.do("GET", "/api/admin/content/documents?sort=titulo:desc", nil, editor).list()[0].(map[string]any)["titulo"]
	assert.Equal(t, "épsilon", first, "ordenação de texto pelo banco")
	for _, bad := range []string{"sort=observacao_interna", "sort=titulo%3BDROP+TABLE+documents", "sort=titulo:up", "status=x", "department_id=x", "page=0", "pageSize=-1"} {
		assert.Equal(t, 422, e.do("GET", "/api/admin/content/documents?"+bad, nil, editor).Status, bad)
	}
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM documents").Scan(&n))
	assert.Equal(t, 8, n)
}

func TestRotasAdminDeDocumentosExigemPermissao(t *testing.T) {
	e, boss, _ := seeded(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/admin/content/documents"}, {"POST", "/api/admin/content/documents"}, {"GET", "/api/admin/content/documents/" + id},
		{"PUT", "/api/admin/content/documents/" + id}, {"DELETE", "/api/admin/content/documents/" + id},
		{"POST", "/api/admin/content/documents/" + id + "/publish"}, {"POST", "/api/admin/content/documents/" + id + "/unpublish"},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status)
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status)
		})
	}
}

func TestAPIPublicaAnonimoXAutenticado(t *testing.T) {
	e, _, login := seeded(t)
	editor := login("editor@dev.local")
	pub := e.docID(editor, map[string]any{"titulo": "Publicado", "conteudo": "aberto", "observacao_interna": "SEGREDO-INTERNO-123"})
	draft := e.docID(editor, map[string]any{"titulo": "Rascunho", "conteudo": "fechado", "observacao_interna": "SEGREDO-INTERNO-123"})
	require.Equal(t, 200, e.do("POST", "/api/admin/content/documents/"+pub+"/publish", nil, editor).Status)
	var bodies []string
	collect := func(r resp) resp { bodies = append(bodies, toJSON(r.Body)); return r }

	// Anônimo: lê só publicados.
	list := collect(e.do("GET", "/api/documents", nil, ""))
	require.Equal(t, 200, list.Status, list.Body)
	require.Len(t, list.list(), 1)
	assert.Equal(t, pub, list.list()[0].(map[string]any)["id"])
	assert.EqualValues(t, 1, list.meta()["total"])
	assert.Equal(t, 200, collect(e.do("GET", "/api/documents/"+pub, nil, "")).Status)
	assert.Equal(t, 404, e.do("GET", "/api/documents/"+draft, nil, "").Status, "rascunho não aparece")
	assert.Equal(t, 404, e.do("GET", "/api/documents/"+uuid.NewString(), nil, "").Status)
	assert.Equal(t, 404, e.do("GET", "/api/documents/xyz", nil, "").Status)

	// Anônimo não escreve (401); token inválido nunca vira anônimo.
	assert.Equal(t, 401, e.do("POST", "/api/documents", map[string]any{"titulo": "x"}, "").Status)
	assert.Equal(t, 401, e.do("PUT", "/api/documents/"+pub, map[string]any{"titulo": "x"}, "").Status)
	assert.Equal(t, 401, e.do("DELETE", "/api/documents/"+pub, nil, "").Status)
	assert.Equal(t, 401, e.do("GET", "/api/documents", nil, "lixo").Status)

	// Autenticado: cria (como rascunho dele), edita e exclui só o próprio.
	maria, _, mariaID := e.userToken("maria", "maria@exemplo.com")
	joao, _, _ := e.userToken("joao", "joao@exemplo.com")
	c := collect(e.do("POST", "/api/documents", map[string]any{"titulo": "Da Maria", "conteudo": "meu"}, maria))
	require.Equal(t, 201, c.Status, c.Body)
	mine := c.data()["id"].(string)
	var owner, status string
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT owner_user_id::text, status FROM documents WHERE id=$1", mine).Scan(&owner, &status))
	assert.Equal(t, mariaID, owner)
	assert.Equal(t, "rascunho", status)

	assert.Len(t, e.do("GET", "/api/documents", nil, "").list(), 1, "rascunho dela não vira público")
	assert.Equal(t, 404, e.do("GET", "/api/documents/"+mine, nil, maria).Status, "a API pública só serve publicados")

	up := collect(e.do("PUT", "/api/documents/"+mine, map[string]any{"titulo": "Da Maria 2"}, maria))
	require.Equal(t, 200, up.Status, up.Body)
	assert.Equal(t, "Da Maria 2", up.data()["titulo"])
	other := e.do("PUT", "/api/documents/"+mine, map[string]any{"titulo": "roubado"}, joao)
	assert.Equal(t, 404, other.Status, "rascunho alheio nem revela que existe")
	// Documento publicado de outra pessoa: existe, mas não é dele.
	denied := e.do("PUT", "/api/documents/"+pub, map[string]any{"titulo": "roubado"}, joao)
	assert.Equal(t, 403, denied.Status)
	assert.Equal(t, "is-owner", denied.Body["error"].(map[string]any)["details"].(map[string]any)["policy"])
	assert.Equal(t, 403, e.do("DELETE", "/api/documents/"+pub, nil, joao).Status)
	assert.Equal(t, "Publicado", e.do("GET", "/api/admin/content/documents/"+pub, nil, editor).data()["titulo"])

	// Campos que a API pública não aceita.
	for name, body := range map[string]map[string]any{
		"observação interna": {"titulo": "x", "observacao_interna": "vazou"},
		"status":             {"titulo": "x", "status": "publicado"},
		"departamento":       {"titulo": "x", "department_id": e.deptID("Financeiro")},
		"dono":               {"titulo": "x", "owner_user_id": mariaID},
		"campo de sistema":   {"titulo": "x", "created_by_id": uuid.NewString()},
	} {
		assert.Equal(t, 422, e.do("POST", "/api/documents", body, maria).Status, name)
		assert.Equal(t, 422, e.do("PUT", "/api/documents/"+mine, body, maria).Status, name)
	}
	assert.Equal(t, 422, e.do("POST", "/api/documents", map[string]any{"conteudo": "sem título"}, maria).Status)
	assert.Equal(t, 422, e.do("POST", "/api/documents", map[string]any{"titulo": ""}, maria).Status)

	assert.Equal(t, 204, e.do("DELETE", "/api/documents/"+mine, nil, maria).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/documents/"+mine, nil, maria).Status)

	// A observação interna nunca aparece em nenhuma resposta pública.
	for _, b := range bodies {
		assert.NotContains(t, b, "SEGREDO-INTERNO-123")
		assert.NotContains(t, b, "observacao_interna")
		assert.NotContains(t, b, "department_id")
		assert.NotContains(t, b, "owner_user_id")
		assert.NotContains(t, b, "created_by_id")
	}
}

func TestAPIPublicaListagem(t *testing.T) {
	e, _, login := seeded(t)
	editor := login("editor@dev.local")
	for _, title := range []string{"Beta", "Alfa", "Gama", "Delta", "Épsilon"} {
		id := e.docID(editor, map[string]any{"titulo": title, "conteudo": "conteúdo " + title})
		e.do("POST", "/api/admin/content/documents/"+id+"/publish", nil, editor)
	}
	e.docID(editor, map[string]any{"titulo": "Rascunho oculto"})

	all := e.do("GET", "/api/documents", nil, "")
	assert.EqualValues(t, 5, all.meta()["total"])
	assert.Equal(t, "Épsilon", all.list()[0].(map[string]any)["titulo"], "padrão: publicados mais recentes primeiro")
	asc := e.do("GET", "/api/documents?sort=titulo:asc&pageSize=2&page=2", nil, "")
	assert.Equal(t, "Delta", asc.list()[0].(map[string]any)["titulo"])
	assert.EqualValues(t, 3, asc.meta()["pageCount"])
	assert.Len(t, e.do("GET", "/api/documents?q=gama", nil, "").list(), 1)
	assert.EqualValues(t, 100, e.do("GET", "/api/documents?pageSize=999", nil, "").meta()["pageSize"])
	for _, bad := range []string{"sort=status", "sort=observacao_interna", "sort=titulo:x", "page=0", "pageSize=z"} {
		assert.Equal(t, 422, e.do("GET", "/api/documents?"+bad, nil, "").Status, bad)
	}
	// Filtros administrativos não existem na API pública: são ignorados, nunca ampliam o resultado.
	assert.EqualValues(t, 5, e.do("GET", "/api/documents?status=rascunho", nil, "").meta()["total"])
}

func TestAPIPublicaComTokensDeAPI(t *testing.T) {
	e, boss, login := seeded(t)
	editor := login("editor@dev.local")
	pub := e.docID(editor, map[string]any{"titulo": "Publicado"})
	e.do("POST", "/api/admin/content/documents/"+pub+"/publish", nil, editor)

	ro := e.createToken(boss, "leitura", "read-only", "7d").data()["token"].(string)
	full := e.createToken(boss, "total", "full-access", "7d").data()["token"].(string)
	custom := e.createToken(boss, "só cria", "custom", "7d", "api::document.create").data()["token"].(string)

	// Token somente leitura tentando escrever.
	assert.Equal(t, 200, e.do("GET", "/api/documents", nil, ro).Status)
	assert.Equal(t, 200, e.do("GET", "/api/documents/"+pub, nil, ro).Status)
	assert.Equal(t, 403, e.do("POST", "/api/documents", map[string]any{"titulo": "x"}, ro).Status)
	assert.Equal(t, 403, e.do("DELETE", "/api/documents/"+pub, nil, ro).Status)

	c := e.do("POST", "/api/documents", map[string]any{"titulo": "Da integração"}, full)
	require.Equal(t, 201, c.Status, c.Body)
	var owner *string
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT owner_user_id::text FROM documents WHERE id=$1", c.data()["id"]).Scan(&owner))
	assert.Nil(t, owner, "token de API não tem usuário dono")
	assert.Equal(t, 403, e.do("PUT", "/api/documents/"+pub, map[string]any{"titulo": "x"}, full).Status, "is-owner nega tokens")
	assert.Equal(t, 201, e.do("POST", "/api/documents", map[string]any{"titulo": "Custom"}, custom).Status)
	assert.Equal(t, 403, e.do("GET", "/api/documents", nil, custom).Status)
}

func TestMudarPermissoesPublicasValeNaHora(t *testing.T) {
	e, boss, login := seeded(t)
	editor := login("editor@dev.local")
	pub := e.docID(editor, map[string]any{"titulo": "Publicado"})
	e.do("POST", "/api/admin/content/documents/"+pub+"/publish", nil, editor)
	assert.Equal(t, 200, e.do("GET", "/api/documents/"+pub, nil, "").Status)

	// Tirar a leitura da role Public: o anônimo passa a receber 401.
	publicRole := e.endRoleID("public")
	require.Equal(t, 200, e.do("PUT", "/api/admin/end-user-roles/"+publicRole, map[string]any{"name": "Public", "permissions": []string{"api::document.find"}}, boss).Status)
	assert.Equal(t, 200, e.do("GET", "/api/documents", nil, "").Status)
	assert.Equal(t, 401, e.do("GET", "/api/documents/"+pub, nil, "").Status)

	// A role Authenticated detalha as ações com a rota vinculada.
	r := e.do("GET", "/api/admin/end-user-roles/"+e.endRoleID("authenticated"), nil, boss)
	routes := map[string]string{}
	for _, p := range r.data()["permissions"].([]any) {
		m := p.(map[string]any)
		routes[m["action"].(string)] = m["method"].(string) + " " + m["route"].(string)
	}
	assert.Equal(t, "PUT /api/documents/{id}", routes["api::document.update"])
	assert.Equal(t, "DELETE /api/documents/{id}", routes["api::document.delete"])
}
