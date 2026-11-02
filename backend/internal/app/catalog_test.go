package app_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogosExigemAutenticacaoEPermissao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/admin/catalog/entity-types"}, {"POST", "/api/admin/catalog/entity-types"},
		{"GET", "/api/admin/catalog/entity-types/" + id}, {"PUT", "/api/admin/catalog/entity-types/" + id}, {"DELETE", "/api/admin/catalog/entity-types/" + id},
		{"GET", "/api/admin/catalog/municipalities"}, {"GET", "/api/admin/catalog/municipalities/5103403"},
		{"GET", "/api/admin/catalog/parliamentarians"}, {"POST", "/api/admin/catalog/parliamentarians"},
		{"GET", "/api/admin/catalog/parliamentarians/" + id}, {"PUT", "/api/admin/catalog/parliamentarians/" + id}, {"DELETE", "/api/admin/catalog/parliamentarians/" + id},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status, "sem token")
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status, "sem permissão")
		})
	}
}

func TestTiposDeEntidadePadrao(t *testing.T) {
	e, boss := setup(t)
	r := e.do("GET", "/api/admin/catalog/entity-types", nil, boss)
	require.Equal(t, 200, r.Status, r.Body)
	list := r.list()
	require.Len(t, list, 18)

	first, last := list[0].(map[string]any), list[len(list)-1].(map[string]any)
	assert.Equal(t, "Prefeitura Municipal", first["name"], "a prefeitura vem primeiro (padrão)")
	assert.Equal(t, "public", first["nature"])
	assert.Equal(t, false, first["isOther"])
	assert.Equal(t, "Outro", last["name"], "Outro sempre por último")
	assert.Equal(t, true, last["isOther"])
	assert.Equal(t, true, last["isSystem"])

	byCode := map[string]map[string]any{}
	for _, it := range list {
		m := it.(map[string]any)
		byCode[m["code"].(string)] = m
	}
	assert.Equal(t, true, byCode["association"]["nonProfit"])
	assert.Equal(t, "private", byCode["association"]["nature"])
	assert.Equal(t, false, byCode["public_service_concessionaire"]["nonProfit"])
	assert.Equal(t, "public", byCode["state_body"]["nature"])

	// Reiniciar a aplicação não duplica nem desfaz edições.
	rn := e.do("PUT", "/api/admin/catalog/entity-types/"+byCode["ngo"]["id"].(string),
		map[string]any{"name": "Organização Não Governamental", "nature": "private", "nonProfit": true, "sortOrder": 130}, boss)
	require.Equal(t, 200, rn.Status, rn.Body)
	require.NoError(t, e.app.Catalog.EnsureDefaults(context.Background()))
	assert.Len(t, e.do("GET", "/api/admin/catalog/entity-types", nil, boss).list(), 18)
	assert.Equal(t, "Organização Não Governamental", e.do("GET", "/api/admin/catalog/entity-types/"+byCode["ngo"]["id"].(string), nil, boss).data()["name"])
}

func TestCrudDeTiposDeEntidade(t *testing.T) {
	e, boss := setup(t)

	r := e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "coletivo", "name": "  Coletivo   Cultural ", "nature": "private", "nonProfit": true, "sortOrder": 150}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, "Coletivo Cultural", r.data()["name"])
	assert.Equal(t, true, r.data()["active"], "ativo por padrão")
	id := r.data()["id"].(string)

	assert.Equal(t, "entity_type_taken", e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "coletivo", "name": "Outro nome", "nature": "private"}, boss).errCode(), "código repetido")
	assert.Equal(t, "entity_type_taken", e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "outro_cod", "name": "coletivo cultural", "nature": "private"}, boss).errCode(), "nome repetido sem diferenciar maiúsculas")
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "Código Ruim", "name": "X", "nature": "private"}, boss).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "other", "name": "Outro 2", "nature": "private"}, boss).Status, "o código other é reservado")
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "x1", "name": "X", "nature": "mista"}, boss).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/entity-types", map[string]any{"code": "pub_sfl", "name": "Pública sem fins", "nature": "public", "nonProfit": true}, boss).Status, "só privada pode ser sem fins lucrativos")

	r = e.do("PUT", "/api/admin/catalog/entity-types/"+id, map[string]any{"name": "Coletivo", "nature": "private", "nonProfit": false, "active": false, "sortOrder": 160}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, false, r.data()["active"])
	assert.Equal(t, false, r.data()["nonProfit"])
	assert.Equal(t, 422, e.do("PUT", "/api/admin/catalog/entity-types/"+id, map[string]any{"code": "outro", "name": "Coletivo", "nature": "private"}, boss).Status, "o código não muda")

	active := e.do("GET", "/api/admin/catalog/entity-types?active=true", nil, boss).list()
	for _, it := range active {
		assert.NotEqual(t, id, it.(map[string]any)["id"], "inativo some da lista de ativos")
	}
	assert.Equal(t, 422, e.do("GET", "/api/admin/catalog/entity-types?active=talvez", nil, boss).Status)

	assert.Equal(t, 204, e.do("DELETE", "/api/admin/catalog/entity-types/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/catalog/entity-types/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/catalog/entity-types/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/catalog/entity-types/não-é-uuid", nil, boss).Status)
}

func TestTipoOutroEhDoSistema(t *testing.T) {
	e, boss := setup(t)
	var other map[string]any
	for _, it := range e.do("GET", "/api/admin/catalog/entity-types", nil, boss).list() {
		if m := it.(map[string]any); m["isOther"] == true {
			other = m
		}
	}
	require.NotNil(t, other)
	id := other["id"].(string)

	r := e.do("PUT", "/api/admin/catalog/entity-types/"+id, map[string]any{"name": "Outro tipo", "nature": "private", "active": false}, boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "entity_type_system", r.errCode(), "não pode ser desativado")
	r = e.do("DELETE", "/api/admin/catalog/entity-types/"+id, nil, boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "entity_type_system", r.errCode(), "não pode ser excluído")
	assert.Equal(t, 200, e.do("PUT", "/api/admin/catalog/entity-types/"+id, map[string]any{"name": "Outro (especifique)", "nature": "private", "sortOrder": 9999}, boss).Status, "pode ser renomeado")
}

func TestMunicipiosDoIBGE(t *testing.T) {
	e, boss := setup(t)

	r := e.do("GET", "/api/admin/catalog/municipalities?pageSize=1", nil, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.EqualValues(t, 5571, r.meta()["total"], "todos os municípios do IBGE")

	r = e.do("GET", "/api/admin/catalog/municipalities?uf=mt&pageSize=500", nil, boss)
	require.Equal(t, 200, r.Status)
	assert.EqualValues(t, 142, r.meta()["total"], "municípios de Mato Grosso")
	assert.Len(t, r.list(), 142)

	// A busca ignora acentos e maiúsculas e vale pelo começo do nome.
	r = e.do("GET", "/api/admin/catalog/municipalities?uf=MT&q=CUIABA", nil, boss)
	require.Equal(t, 200, r.Status)
	require.Len(t, r.list(), 1)
	cuiaba := r.list()[0].(map[string]any)
	assert.Equal(t, "Cuiabá", cuiaba["name"])
	assert.Equal(t, "5103403", cuiaba["ibgeCode"])
	assert.Equal(t, "MT", cuiaba["uf"])
	assert.NotEmpty(t, e.do("GET", "/api/admin/catalog/municipalities?q=são+paulo", nil, boss).list())
	assert.Empty(t, e.do("GET", "/api/admin/catalog/municipalities?uf=MT&q=ZZZ", nil, boss).list())

	// Paginação.
	r = e.do("GET", "/api/admin/catalog/municipalities?uf=MT&pageSize=10&page=15", nil, boss)
	assert.Len(t, r.list(), 2, "última página: 142 = 14×10 + 2")
	assert.EqualValues(t, 15, r.meta()["pageCount"])

	assert.Equal(t, 422, e.do("GET", "/api/admin/catalog/municipalities?uf=XX", nil, boss).Status)
	assert.Equal(t, 422, e.do("GET", "/api/admin/catalog/municipalities?page=0", nil, boss).Status)

	g := e.do("GET", "/api/admin/catalog/municipalities/5103403", nil, boss)
	require.Equal(t, 200, g.Status)
	assert.Equal(t, "Cuiabá", g.data()["name"])
	assert.Equal(t, 404, e.do("GET", "/api/admin/catalog/municipalities/0000000", nil, boss).Status)

	// A lista é de referência: não há como alterá-la pela API.
	assert.Equal(t, 405, e.do("POST", "/api/admin/catalog/municipalities", map[string]any{}, boss).Status)
	assert.Equal(t, 405, e.do("DELETE", "/api/admin/catalog/municipalities/5103403", nil, boss).Status)

	// A carga é idempotente.
	require.NoError(t, e.app.Catalog.EnsureDefaults(context.Background()))
	assert.EqualValues(t, 5571, e.do("GET", "/api/admin/catalog/municipalities?pageSize=1", nil, boss).meta()["total"])
}

func TestCrudDeParlamentares(t *testing.T) {
	e, boss := setup(t)

	// A migração 00034 já carrega os parlamentares eleitos em 2026; as contagens abaixo partem deles.
	count := func(query string) int {
		return len(e.do("GET", "/api/admin/catalog/parliamentarians"+query, nil, boss).list())
	}
	all, federal, active := count(""), count("?chamber=federal"), count("?active=true")

	r := e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "  Fulano   de Tal ", "chamber": "state"}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, "Fulano de Tal", r.data()["name"])
	assert.Equal(t, true, r.data()["active"])
	id := r.data()["id"].(string)

	// Mesmo nome em outra casa é outra pessoa; na mesma casa é duplicado.
	assert.Equal(t, 201, e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "Fulano de Tal", "chamber": "federal"}, boss).Status)
	assert.Equal(t, "parliamentarian_taken", e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "FULANO DE TAL", "chamber": "state"}, boss).errCode())
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "   ", "chamber": "state"}, boss).Status)
	assert.Equal(t, 422, e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "Beltrano", "chamber": "municipal"}, boss).Status)

	assert.Equal(t, 201, e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "Beltrana Souza", "chamber": "state", "active": false}, boss).Status)

	assert.Equal(t, all+3, count(""))
	assert.Equal(t, federal+1, count("?chamber=federal"))
	assert.Equal(t, active+2, count("?active=true"))
	assert.Len(t, e.do("GET", "/api/admin/catalog/parliamentarians?q=beltr", nil, boss).list(), 1)
	assert.Equal(t, 422, e.do("GET", "/api/admin/catalog/parliamentarians?chamber=x", nil, boss).Status)

	r = e.do("PUT", "/api/admin/catalog/parliamentarians/"+id, map[string]any{"name": "Fulano de Tal Júnior", "chamber": "state", "active": false}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, false, r.data()["active"])
	r = e.do("PUT", "/api/admin/catalog/parliamentarians/"+id, map[string]any{"name": "Fulano de Tal Júnior", "chamber": "state"}, boss)
	assert.Equal(t, false, r.data()["active"], "omitir active mantém o valor atual")
	assert.Equal(t, "parliamentarian_taken", e.do("PUT", "/api/admin/catalog/parliamentarians/"+id, map[string]any{"name": "Fulano de Tal", "chamber": "federal"}, boss).errCode())

	assert.Equal(t, 204, e.do("DELETE", "/api/admin/catalog/parliamentarians/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/catalog/parliamentarians/"+id, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/catalog/parliamentarians/"+id, nil, boss).Status)
}

func TestCatalogosSaoAuditados(t *testing.T) {
	e, boss := setup(t)
	r := e.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": "Fulano", "chamber": "state"}, boss)
	id := r.data()["id"].(string)
	e.do("PUT", "/api/admin/catalog/parliamentarians/"+id, map[string]any{"name": "Fulano Silva", "chamber": "state"}, boss)
	e.do("DELETE", "/api/admin/catalog/parliamentarians/"+id, nil, boss)
	for _, action := range []string{"parliamentarian.create", "parliamentarian.update", "parliamentarian.delete"} {
		logs := e.do("GET", "/api/admin/audit-logs?action="+action+"&entityId="+id, nil, boss)
		require.Len(t, logs.list(), 1, action)
	}
	upd := e.do("GET", "/api/admin/audit-logs?action=parliamentarian.update", nil, boss).list()[0].(map[string]any)["payload"].(map[string]any)
	assert.Equal(t, "Fulano", upd["before"].(map[string]any)["name"])
	assert.Equal(t, "Fulano Silva", upd["after"].(map[string]any)["name"])
}
