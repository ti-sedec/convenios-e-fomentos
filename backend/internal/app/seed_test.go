package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/seed"
	"github.com/wagner/convenios-e-fomentos/backend/internal/testutil"
)

const (
	pdfType  = "application/pdf"
	docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// runSeed executa o seed. Os administradores de desenvolvimento entram pelo MT Login, então o seed não gera senha nenhuma.
func (e *env) runSeed(development bool, superAdminCPFs ...string) {
	e.t.Helper()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	require.NoError(e.t, seed.Run(context.Background(), seed.Deps{Pool: e.pool, Engine: e.app.Authz, Log: log, Development: development, SuperAdminCPFs: superAdminCPFs}))
	assert.NotContains(e.t, buf.String(), "senha", "o seed não gera nem registra senhas")
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

func TestSeedIdempotenteEEstrutura(t *testing.T) {
	e, boss := setup(t)
	e.runSeed(true)
	count := func(q string, args ...any) int {
		var n int
		require.NoError(t, e.pool.QueryRow(context.Background(), q, args...).Scan(&n))
		return n
	}
	snapshot := func() []int {
		return []int{count("SELECT count(*) FROM admin_roles"), count("SELECT count(*) FROM admin_permissions"), count("SELECT count(*) FROM departments"), count("SELECT count(*) FROM admins")}
	}
	before := snapshot()
	assert.Equal(t, []int{6, 48, 2, 7}, before, "6 roles de admin (Super Admin, Proponente, Revisor, Supervisor, Analista e Secretário) e 7 administradores (o chefe e 6 de desenvolvimento)")

	// Segunda execução: nada novo.
	e.runSeed(true)
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
	rev := perms("department-reviewer")
	assert.Equal(t, `null ["admin::project-submitted-same-department"]`, rev["admin::projects.read"], "o Revisor lê só projetos enviados do próprio departamento")

	// Alterações feitas depois não são desfeitas por um novo seed.
	analyst := e.roleID("analyst")
	require.Equal(t, 200, e.do("PUT", "/api/admin/roles/"+analyst+"/permissions", map[string]any{"permissions": []map[string]any{{"action": "admin::projects.read"}}}, boss).Status)
	e.runSeed(true)
	assert.Len(t, perms("analyst"), 1)

	// Fora do desenvolvimento não há usuários de exemplo.
	e2, _ := setup(t)
	e2.runSeed(false)
	var n int
	require.NoError(t, e2.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 1, n)
	require.NoError(t, e2.pool.QueryRow(context.Background(), "SELECT count(*) FROM admin_roles").Scan(&n))
	assert.Equal(t, 6, n)
}

func TestUsuariosDeDesenvolvimentoFuncionam(t *testing.T) {
	e, _ := setup(t)
	e.runSeed(true)
	for _, email := range []string{"super@dev.local", "revisor@dev.local", "revisor.juridico@dev.local", "analista@dev.local", "supervisor@dev.local", "secretario@dev.local"} {
		me := e.do("GET", "/api/admin/auth/me", nil, e.devToken(email))
		assert.Equal(t, 200, me.Status, email)
	}
	assert.Equal(t, true, e.do("GET", "/api/admin/auth/me", nil, e.devToken("super@dev.local")).data()["isSuperAdmin"])
	me := e.do("GET", "/api/admin/auth/me", nil, e.devToken("revisor@dev.local"))
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
	err := seed.Run(context.Background(), seed.Deps{Pool: e.pool, Engine: e.app.Authz, Log: slog.New(slog.NewJSONHandler(io.Discard, nil)), SuperAdminCPFs: []string{"123.456.789-00"}})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "123.456.789-00")
}
