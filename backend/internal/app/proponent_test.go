package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roleCodes(r resp) []string {
	var out []string
	for _, it := range r.data()["roles"].([]any) {
		out = append(out, it.(map[string]any)["code"].(string))
	}
	return out
}

func TestPrimeiroAcessoViraProponenteQuandoARoleExiste(t *testing.T) {
	e := newEnv(t)
	e.runSeed(false)

	tok := e.token("maria@exemplo.com")
	me := e.do("GET", "/api/admin/auth/me", nil, tok)
	require.Equal(t, 200, me.Status, me.Body)
	assert.Equal(t, []string{"proponent"}, roleCodes(me), "entra como Proponente, sem passar por 'Aguardando liberação'")
	assert.NotEmpty(t, me.data()["permissions"])
	assert.Equal(t, false, me.data()["isSuperAdmin"])

	var payload []byte
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT payload FROM audit_logs WHERE action='admin.auto-provisioned'").Scan(&payload))
	assert.Contains(t, string(payload), `"role": "proponent"`)

	// Segundo acesso não duplica a role.
	me = e.do("GET", "/api/admin/auth/me", nil, tok)
	assert.Equal(t, []string{"proponent"}, roleCodes(me))
}

func TestProponenteLeOsCatalogosMasNaoAdministra(t *testing.T) {
	e := newEnv(t)
	e.runSeed(false)
	tok := e.login("maria@exemplo.com")

	for _, path := range []string{
		"/api/admin/catalog/entity-types", "/api/admin/catalog/municipalities?uf=MT&q=cuiaba",
		"/api/admin/catalog/parliamentarians", "/api/admin/rule-parameters/effective?date=2026-06-01",
	} {
		assert.Equal(t, 200, e.do("GET", path, nil, tok).Status, path)
	}

	for _, rt := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/admin/catalog/entity-types", map[string]any{"code": "x1", "name": "X", "nature": "private"}},
		{"POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "X", "chamber": "state"}},
		{"POST", "/api/admin/rule-parameters", map[string]any{"key": "complement_days", "value": 5, "validFrom": "2020-01-01", "validTo": "2020-12-31"}},
		{"GET", "/api/admin/users", nil},
		{"GET", "/api/admin/roles", nil},
		{"POST", "/api/admin/departments", map[string]any{"name": "TI"}},
		{"GET", "/api/admin/audit-logs", nil},
	} {
		assert.Equal(t, 403, e.do(rt.method, rt.path, rt.body, tok).Status, rt.method+" "+rt.path)
	}
}

func TestSeedDaARoleDeProponenteAQuemFicouSemNenhuma(t *testing.T) {
	e := newEnv(t)
	// Primeiro acesso antes de a role existir: sem roles (a tela "Aguardando liberação"). A migração já a cria, então some com ela.
	_, err := e.pool.Exec(context.Background(), "DELETE FROM admin_roles WHERE code = 'proponent'")
	require.NoError(t, err)
	tok := e.token("joao@exemplo.com")
	me := e.do("GET", "/api/admin/auth/me", nil, tok)
	require.Equal(t, 200, me.Status)
	assert.Empty(t, roleCodes(me))
	assert.Empty(t, me.data()["permissions"])

	e.runSeed(false)
	assert.Equal(t, []string{"proponent"}, roleCodes(e.do("GET", "/api/admin/auth/me", nil, tok)))
}

func TestSeedNaoDaARoleDeProponenteAQuemJaTemOutra(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("analista", "admin::catalog.entity-types.read")
	staff, _ := e.newAdmin(boss, "bia@exemplo.com", "analista")

	e.runSeed(false)
	assert.Equal(t, []string{"analista"}, roleCodes(e.do("GET", "/api/admin/auth/me", nil, staff)))
}

func TestRoleDeProponenteEhDeSistema(t *testing.T) {
	e := newEnv(t)
	e.runSeed(false, cpfFor("ana@exemplo.com"))
	boss := e.login("ana@exemplo.com")

	var found map[string]any
	for _, it := range e.do("GET", "/api/admin/roles", nil, boss).list() {
		if m := it.(map[string]any); m["code"] == "proponent" {
			found = m
		}
	}
	require.NotNil(t, found, "a role Proponente existe depois do seed")
	assert.Equal(t, "Proponente", found["name"])
	assert.Equal(t, true, found["isSystem"])
}

func TestSeedAcrescentaAsPermissoesQueFaltamNaRoleDeProponente(t *testing.T) {
	e := newEnv(t)
	e.runSeed(false)
	ctx := context.Background()
	count := func() int {
		var n int
		require.NoError(t, e.pool.QueryRow(ctx, `SELECT count(*) FROM admin_permissions p JOIN admin_roles r ON r.id = p.role_id WHERE r.code = 'proponent'`).Scan(&n))
		return n
	}
	full := count()
	require.Equal(t, 14, full)

	// Instalação anterior às permissões de projeto: o seed completa o que falta, sem duplicar o que já existe.
	_, err := e.pool.Exec(ctx, `DELETE FROM admin_permissions WHERE action LIKE 'admin::projects.%' OR action LIKE 'admin::proponent-profile.%'`)
	require.NoError(t, err)
	require.Equal(t, 4, count(), "ficam só as permissões dos catálogos e dos parâmetros")
	e.runSeed(false)
	assert.Equal(t, full, count())
	e.runSeed(false)
	assert.Equal(t, full, count(), "rodar de novo não duplica")

	// A permissão recriada mantém a condição de criador: o proponente só enxerga os próprios projetos.
	var cond []byte
	require.NoError(t, e.pool.QueryRow(ctx, `SELECT conditions FROM admin_permissions WHERE action = 'admin::projects.read'`).Scan(&cond))
	assert.Contains(t, string(cond), "admin::is-creator")
}
