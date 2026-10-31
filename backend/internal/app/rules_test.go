package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParametrosExigemAutenticacaoEPermissao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/admin/rule-parameters"}, {"GET", "/api/admin/rule-parameters/keys"}, {"GET", "/api/admin/rule-parameters/effective"},
		{"POST", "/api/admin/rule-parameters"}, {"GET", "/api/admin/rule-parameters/" + id},
		{"PUT", "/api/admin/rule-parameters/" + id}, {"DELETE", "/api/admin/rule-parameters/" + id},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status, "sem token")
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status, "sem permissão")
		})
	}
}

func effectiveByKey(t *testing.T, r resp) map[string]map[string]any {
	t.Helper()
	require.Equal(t, 200, r.Status, r.Body)
	out := map[string]map[string]any{}
	for _, it := range r.list() {
		m := it.(map[string]any)
		out[m["key"].(string)] = m
	}
	return out
}

func TestParametrosPadraoDasNormas(t *testing.T) {
	e, boss := setup(t)

	// Em jun/2026 todos os seis estão vigentes.
	eff := effectiveByKey(t, e.do("GET", "/api/admin/rule-parameters/effective?date=2026-06-01", nil, boss))
	require.Len(t, eff, 6)
	assert.Equal(t, map[string]any{"awaiting_checklist": float64(2), "in_analysis": float64(7), "framing": float64(5), "awaiting_opinion": float64(15)},
		eff["stalled_after_days"]["value"], "limites de parado, por etapa")
	assert.EqualValues(t, 3, eff["complement_days"]["value"])
	assert.EqualValues(t, 60, eff["price_reference_max_age_days"]["value"])
	assert.Equal(t, "2026-05-15", eff["electoral_deadline"]["value"], "Portaria Conjunta 71/2026")
	assert.Equal(t, "2026-12-31", eff["electoral_deadline"]["validTo"])
	lei := eff["lei_12082"]["value"].(map[string]any)
	assert.EqualValues(t, 60000000, lei["ceiling_cents"], "R$ 600.000,00 em centavos")
	assert.EqualValues(t, 10, lei["min_municipal_counterpart_pct"])
	assert.EqualValues(t, 50, lei["max_fees_pct"])
	assert.EqualValues(t, 30, lei["min_regional_fees_pct"])
	tiers := eff["indirect_costs_tiers"]["value"].([]any)
	require.Len(t, tiers, 3)
	assert.EqualValues(t, 20, tiers[0].(map[string]any)["max_pct"])
	assert.Nil(t, tiers[2].(map[string]any)["up_to_cents"])

	// A data-limite eleitoral só vale no ano eleitoral; fora dele, sem valor vigente.
	eff = effectiveByKey(t, e.do("GET", "/api/admin/rule-parameters/effective?date=2027-03-01", nil, boss))
	assert.NotContains(t, eff, "electoral_deadline")
	assert.Contains(t, eff, "lei_12082")

	// Antes da IN 002/2026 não havia faixas de custos indiretos nem validade de 60 dias.
	eff = effectiveByKey(t, e.do("GET", "/api/admin/rule-parameters/effective?date=2025-01-10", nil, boss))
	assert.NotContains(t, eff, "indirect_costs_tiers")
	assert.NotContains(t, eff, "price_reference_max_age_days")

	assert.Equal(t, 422, e.do("GET", "/api/admin/rule-parameters/effective?date=ontem", nil, boss).Status)
	assert.Equal(t, 200, e.do("GET", "/api/admin/rule-parameters/effective", nil, boss).Status, "sem data usa hoje")

	keys := e.do("GET", "/api/admin/rule-parameters/keys", nil, boss).list()
	assert.Len(t, keys, 6)
	assert.NotEmpty(t, keys[0].(map[string]any)["description"])
}

func TestCrudDeParametros(t *testing.T) {
	e, boss := setup(t)

	// Nova versão do teto da Lei 12.082 (atualização por IPCA) a partir de 2027.
	var current map[string]any
	for _, it := range e.do("GET", "/api/admin/rule-parameters?key=lei_12082", nil, boss).list() {
		current = it.(map[string]any)
	}
	require.NotNil(t, current)
	body := map[string]any{"key": "lei_12082", "validFrom": "2027-01-01", "note": "IPCA 2026",
		"value": map[string]any{"ceiling_cents": 63000000, "min_municipal_counterpart_pct": 10, "max_fees_pct": 50, "min_regional_fees_pct": 30}}

	// Enquanto a versão atual não tem fim, a nova se sobrepõe a ela.
	r := e.do("POST", "/api/admin/rule-parameters", body, boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "rule_parameter_overlap", r.errCode())

	// Encerrar a atual em 31/12/2026 abre espaço para a nova.
	cur := current["value"]
	r = e.do("PUT", "/api/admin/rule-parameters/"+current["id"].(string), map[string]any{"value": cur, "validFrom": current["validFrom"], "validTo": "2026-12-31", "note": "encerrada"}, boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "2026-12-31", r.data()["validTo"])
	r = e.do("POST", "/api/admin/rule-parameters", body, boss)
	require.Equal(t, 201, r.Status, r.Body)
	newID := r.data()["id"].(string)
	assert.Nil(t, r.data()["validTo"])

	eff := effectiveByKey(t, e.do("GET", "/api/admin/rule-parameters/effective?date=2027-06-01", nil, boss))
	assert.EqualValues(t, 63000000, eff["lei_12082"]["value"].(map[string]any)["ceiling_cents"])
	eff = effectiveByKey(t, e.do("GET", "/api/admin/rule-parameters/effective?date=2026-06-01", nil, boss))
	assert.EqualValues(t, 60000000, eff["lei_12082"]["value"].(map[string]any)["ceiling_cents"], "a versão antiga segue valendo em 2026")

	// Mesma data de início da outra versão: conflito distinto.
	dup := map[string]any{"key": "lei_12082", "validFrom": "2027-01-01", "validTo": "2027-02-01", "value": body["value"]}
	assert.Equal(t, 409, e.do("POST", "/api/admin/rule-parameters", dup, boss).Status)

	// A versão nova não pode invadir a antiga ao ser editada.
	r = e.do("PUT", "/api/admin/rule-parameters/"+newID, map[string]any{"value": body["value"], "validFrom": "2026-06-01"}, boss)
	assert.Equal(t, "rule_parameter_overlap", r.errCode())
	assert.Equal(t, 200, e.do("PUT", "/api/admin/rule-parameters/"+newID, map[string]any{"value": body["value"], "validFrom": "2027-01-01", "note": "ajustada"}, boss).Status, "editar sem mudar o período não se sobrepõe a si mesma")
	assert.Equal(t, 422, e.do("PUT", "/api/admin/rule-parameters/"+newID, map[string]any{"key": "complement_days", "value": 3, "validFrom": "2027-01-01"}, boss).Status, "a chave não muda")

	assert.Equal(t, 200, e.do("GET", "/api/admin/rule-parameters/"+newID, nil, boss).Status)
	assert.Len(t, e.do("GET", "/api/admin/rule-parameters?key=lei_12082", nil, boss).list(), 2)
	assert.Equal(t, 422, e.do("GET", "/api/admin/rule-parameters?key=inexistente", nil, boss).Status)

	assert.Equal(t, 204, e.do("DELETE", "/api/admin/rule-parameters/"+newID, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/rule-parameters/"+newID, nil, boss).Status)
	assert.Equal(t, 404, e.do("DELETE", "/api/admin/rule-parameters/"+newID, nil, boss).Status)
	assert.Equal(t, 404, e.do("GET", "/api/admin/rule-parameters/não-é-uuid", nil, boss).Status)
}

func TestParametrosValidamOValorDeCadaChave(t *testing.T) {
	e, boss := setup(t)
	post := func(key string, value any) resp {
		return e.do("POST", "/api/admin/rule-parameters", map[string]any{"key": key, "value": value, "validFrom": "2020-01-01", "validTo": "2020-12-31"}, boss)
	}
	for name, c := range map[string]struct {
		key   string
		value any
	}{
		"chave desconhecida":         {"nao_existe", 1},
		"dias zero":                  {"complement_days", 0},
		"dias fracionados":           {"complement_days", 2.5},
		"dias como texto":            {"complement_days", "3"},
		"dias acima do limite":       {"price_reference_max_age_days", 400},
		"data inválida":              {"electoral_deadline", "15/05/2030"},
		"data como número":           {"electoral_deadline", 20300515},
		"lei sem teto":               {"lei_12082", map[string]any{"ceiling_cents": 0, "min_municipal_counterpart_pct": 10, "max_fees_pct": 50, "min_regional_fees_pct": 30}},
		"lei com percentual > 100":   {"lei_12082", map[string]any{"ceiling_cents": 1, "min_municipal_counterpart_pct": 10, "max_fees_pct": 150, "min_regional_fees_pct": 30}},
		"lei com campo desconhecido": {"lei_12082", map[string]any{"ceiling_cents": 1, "min_municipal_counterpart_pct": 10, "max_fees_pct": 50, "min_regional_fees_pct": 30, "extra": 1}},
		"parado sem uma etapa":       {"stalled_after_days", map[string]any{"awaiting_checklist": 2, "in_analysis": 7, "framing": 5}},
		"parado com dias zero":       {"stalled_after_days", map[string]any{"awaiting_checklist": 0, "in_analysis": 7, "framing": 5, "awaiting_opinion": 15}},
		"parado acima do limite":     {"stalled_after_days", map[string]any{"awaiting_checklist": 2, "in_analysis": 400, "framing": 5, "awaiting_opinion": 15}},
		"parado com etapa a mais":    {"stalled_after_days", map[string]any{"awaiting_checklist": 2, "in_analysis": 7, "framing": 5, "awaiting_opinion": 15, "awaiting_proponent": 3}},
		"parado como número":         {"stalled_after_days", 7},
		"faixas vazias":              {"indirect_costs_tiers", []any{}},
		"última faixa com limite": {"indirect_costs_tiers", []any{
			map[string]any{"up_to_cents": 100, "max_pct": 20, "cap_cents": 10}}},
		"faixa sem limite no meio": {"indirect_costs_tiers", []any{
			map[string]any{"up_to_cents": nil, "max_pct": 20, "cap_cents": 10}, map[string]any{"up_to_cents": nil, "max_pct": 10, "cap_cents": 10}}},
		"faixas fora de ordem": {"indirect_costs_tiers", []any{
			map[string]any{"up_to_cents": 500, "max_pct": 20, "cap_cents": 10}, map[string]any{"up_to_cents": 100, "max_pct": 15, "cap_cents": 10}, map[string]any{"up_to_cents": nil, "max_pct": 10, "cap_cents": 10}}},
	} {
		t.Run(name, func(t *testing.T) {
			r := post(c.key, c.value)
			assert.Equal(t, 422, r.Status, r.Body)
			assert.Equal(t, "validation_error", r.errCode())
		})
	}

	ok := post("indirect_costs_tiers", []any{
		map[string]any{"up_to_cents": 100, "max_pct": 20, "cap_cents": 10}, map[string]any{"up_to_cents": nil, "max_pct": 10, "cap_cents": 50}})
	assert.Equal(t, 201, ok.Status, ok.Body)

	badPeriod := e.do("POST", "/api/admin/rule-parameters", map[string]any{"key": "complement_days", "value": 5, "validFrom": "2031-05-01", "validTo": "2031-04-01"}, boss)
	assert.Equal(t, 422, badPeriod.Status, "fim antes do início")
	assert.Equal(t, 422, e.do("POST", "/api/admin/rule-parameters", map[string]any{"key": "complement_days", "value": 5, "validFrom": "amanhã"}, boss).Status)
}

func TestParametrosSaoAuditados(t *testing.T) {
	e, boss := setup(t)
	r := e.do("POST", "/api/admin/rule-parameters", map[string]any{"key": "complement_days", "value": 5, "validFrom": "2020-01-01", "validTo": "2020-12-31"}, boss)
	require.Equal(t, 201, r.Status, r.Body)
	id := r.data()["id"].(string)
	e.do("PUT", "/api/admin/rule-parameters/"+id, map[string]any{"value": 7, "validFrom": "2020-01-01", "validTo": "2020-12-31"}, boss)
	e.do("DELETE", "/api/admin/rule-parameters/"+id, nil, boss)
	for _, action := range []string{"rule-parameter.create", "rule-parameter.update", "rule-parameter.delete"} {
		logs := e.do("GET", "/api/admin/audit-logs?action="+action+"&entityId="+id, nil, boss)
		require.Len(t, logs.list(), 1, action)
	}
}
