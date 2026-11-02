package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fundingProject é um projeto de Maria com a instituição e a proposta já preenchidas.
type fundingProject struct {
	*projEnv
	id      string
	version int
}

// newFundingProject cria o projeto com o instrumento e o tipo de entidade dados.
func (p *projEnv) newFundingProject(instrument, typeCode string, mutateProposal func(map[string]any)) *fundingProject {
	p.t.Helper()
	id, v := p.create(p.maria)
	fp := &fundingProject{projEnv: p, id: id, version: v}
	body := institutionBody(v, p.typeID(typeCode))
	body["instrument"] = instrument
	fp.save("institution", body)
	pb := proposalBody(fp.version)
	pb["objectNature"] = "services" // obra acrescenta documentos de engenharia; os testes que precisam dela pedem
	if mutateProposal != nil {
		mutateProposal(pb)
	}
	fp.save("proposal", pb)
	return fp
}

// save grava a seção e acompanha a versão; exige sucesso.
func (f *fundingProject) save(section string, body map[string]any) resp {
	f.t.Helper()
	body["version"] = f.version
	r := f.do("PUT", "/api/admin/projects/"+f.id+"/"+section, body, f.maria)
	require.Equal(f.t, 200, r.Status, r.Body)
	f.version = version32(r)
	return r
}

// try grava sem exigir sucesso (para testar recusas); só avança a versão se gravou.
func (f *fundingProject) try(section string, body map[string]any) resp {
	f.t.Helper()
	body["version"] = f.version
	r := f.do("PUT", "/api/admin/projects/"+f.id+"/"+section, body, f.maria)
	if r.Status == 200 {
		f.version = version32(r)
	}
	return r
}

func (p *projEnv) parliamentarian(name, chamber string) string {
	p.t.Helper()
	r := p.do("POST", "/api/admin/catalog/parliamentarians", map[string]any{"name": name, "chamber": chamber}, p.boss)
	require.Equal(p.t, 201, r.Status, r.Body)
	return r.data()["id"].(string)
}

func amendment(parliamentarianID string, number string, year int, cents int64) map[string]any {
	return map[string]any{"parliamentarianId": parliamentarianID, "number": number, "year": year, "amountCents": cents}
}

func fieldsOf(r resp) []string {
	var out []string
	for _, it := range r.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any) {
		out = append(out, it.(map[string]any)["field"].(string))
	}
	return out
}

func funding(m map[string]any) map[string]any { return m }

func TestRecursosEmendaParlamentar(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	dep := p.parliamentarian("Fulano de Tal", "state")
	year := time.Now().Year()

	r := fp.save("funding", funding(map[string]any{"type": "amendment", "amendments": []any{
		amendment(dep, "1234", year, 30_000_000), amendment("", "99", year, 20_000_000),
	}}))
	f := r.data()["funding"].(map[string]any)
	assert.Equal(t, "amendment", f["type"])
	assert.EqualValues(t, 50_000_000, f["amendmentTotalCents"])
	assert.EqualValues(t, 50_000_000, f["totalCents"], "total = recurso próprio (0) + emendas")
	ams := f["amendments"].([]any)
	require.Len(t, ams, 2)
	assert.Equal(t, "Fulano de Tal", ams[0].(map[string]any)["parliamentarianName"], "o nome vem do cadastro")
	assert.Equal(t, dep, ams[0].(map[string]any)["parliamentarianId"])
	assert.Equal(t, "", ams[1].(map[string]any)["parliamentarianId"])
	// A segunda linha não tem parlamentar: pendência.
	assert.Equal(t, "required", issueFields(r)["funding.amendments[1].parliamentarianId"])
	assert.NotContains(t, issueFields(r), "funding.amendments[0].number")

	// Com o nome por extenso, a pendência some e o nome fica guardado.
	r = fp.save("funding", funding(map[string]any{"type": "amendment", "amendments": []any{
		amendment(dep, "1234", year, 30_000_000),
		map[string]any{"parliamentarianName": "  Beltrana   da Silva ", "number": "99", "year": year, "amountCents": 20_000_000},
	}}))
	assert.Equal(t, "Beltrana da Silva", r.data()["funding"].(map[string]any)["amendments"].([]any)[1].(map[string]any)["parliamentarianName"])
	for k := range issueFields(r) {
		assert.NotContains(t, k, "funding.amendments", "emendas completas não deixam pendências")
	}

	// O valor total aparece na listagem.
	list := p.do("GET", "/api/admin/projects", nil, p.maria).list()
	assert.EqualValues(t, 50_000_000, list[0].(map[string]any)["totalCents"])

	// Trocar de tipo apaga as emendas só se o cliente as remover; senão, recusa.
	r = fp.try("funding", funding(map[string]any{"type": "own", "ownAmountCents": 1_000_000, "amendments": []any{amendment(dep, "1", year, 100)}}))
	assert.Equal(t, 422, r.Status)
	assert.Contains(t, fieldsOf(r), "amendments")
	r = fp.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 1_000_000}))
	assert.Empty(t, r.data()["funding"].(map[string]any)["amendments"])
	assert.EqualValues(t, 1_000_000, r.data()["funding"].(map[string]any)["totalCents"])
}

func TestRecursosMistoSomaProprioEEmendas(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	dep := p.parliamentarian("Fulano", "federal")
	r := fp.save("funding", funding(map[string]any{"type": "mixed", "ownAmountCents": 15_000_000,
		"amendments": []any{amendment(dep, "7", time.Now().Year(), 35_000_000)}}))
	f := r.data()["funding"].(map[string]any)
	assert.EqualValues(t, 15_000_000, f["ownAmountCents"])
	assert.EqualValues(t, 35_000_000, f["amendmentTotalCents"])
	assert.EqualValues(t, 50_000_000, f["totalCents"])

	// Sem valor no recurso próprio, o tipo misto fica pendente.
	r = fp.save("funding", funding(map[string]any{"type": "mixed", "amendments": []any{amendment(dep, "7", time.Now().Year(), 35_000_000)}}))
	assert.Equal(t, "required", issueFields(r)["funding.ownAmountCents"])
	// Misto ou emenda sem nenhuma emenda também.
	r = fp.save("funding", funding(map[string]any{"type": "mixed", "ownAmountCents": 100}))
	assert.Equal(t, "required", issueFields(r)["funding.amendments"])
}

func TestRecursosValidamFormatoECoerencia(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	dep := p.parliamentarian("Fulano", "state")
	year := time.Now().Year()
	many := make([]any, 21)
	for i := range many {
		many[i] = amendment(dep, fmt.Sprint(i), year, 100)
	}
	for name, c := range map[string]struct {
		body  map[string]any
		field string
	}{
		"tipo inválido":                {funding(map[string]any{"type": "doacao"}), "type"},
		"valor sem tipo":               {funding(map[string]any{"ownAmountCents": 100}), "type"},
		"valor negativo":               {funding(map[string]any{"type": "own", "ownAmountCents": -1}), "ownAmountCents"},
		"valor absurdo":                {funding(map[string]any{"type": "own", "ownAmountCents": 2_000_000_000_000}), "ownAmountCents"},
		"emenda com recurso próprio":   {funding(map[string]any{"type": "amendment", "ownAmountCents": 100, "amendments": []any{amendment(dep, "1", year, 100)}}), "ownAmountCents"},
		"emendas demais":               {funding(map[string]any{"type": "amendment", "amendments": many}), "amendments"},
		"emenda sem tipo":              {funding(map[string]any{"amendments": []any{amendment(dep, "1", year, 100)}}), "amendments"},
		"ano antigo":                   {funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", 1999, 100)}}), "amendments[0].year"},
		"ano distante":                 {funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year+5, 100)}}), "amendments[0].year"},
		"valor da emenda negativo":     {funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, -5)}}), "amendments[0].amountCents"},
		"número longo":                 {funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, repeat("9", 31), year, 100)}}), "amendments[0].number"},
		"parlamentar inexistente":      {funding(map[string]any{"type": "amendment", "amendments": []any{amendment("00000000-0000-0000-0000-000000000000", "1", year, 100)}}), "amendments[0].parliamentarianId"},
		"parlamentar mal formado":      {funding(map[string]any{"type": "amendment", "amendments": []any{amendment("x", "1", year, 100)}}), "amendments[0].parliamentarianId"},
		"emenda repetida":              {funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 100), amendment(dep, "1", year, 200)}}), "amendments[1].number"},
		"custos indiretos no convênio": {funding(map[string]any{"type": "own", "ownAmountCents": 100, "indirectCostsCents": 10}), "indirectCostsCents"},
		"cachê sem marcar artistas":    {funding(map[string]any{"type": "own", "ownAmountCents": 100, "artistFeesCents": 10}), "artistFeesCents"},
		"chamamento inválido":          {funding(map[string]any{"tenderStatus": "talvez"}), "tenderStatus"},
		"data de coleta futura":        {funding(map[string]any{"priceOldestCollectedOn": time.Now().AddDate(0, 0, 3).Format(time.DateOnly)}), "priceOldestCollectedOn"},
		"data inválida":                {funding(map[string]any{"priceOldestCollectedOn": "ontem"}), "priceOldestCollectedOn"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fp.try("funding", c.body)
			assert.Equal(t, 422, r.Status, r.Body)
			assert.Contains(t, fieldsOf(r), c.field)
		})
	}
	// Um parlamentar desativado não pode ser escolhido de novo, mas segue valendo onde já foi usado.
	require.Equal(t, 200, fp.try("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 100)}})).Status)
	require.Equal(t, 200, p.do("PUT", "/api/admin/catalog/parliamentarians/"+dep, map[string]any{"name": "Fulano", "chamber": "state", "active": false}, p.boss).Status)
	assert.Equal(t, 200, fp.try("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 150)}})).Status, "já usado: continua valendo")
	other := fp.try("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "2", year, 100), amendment(dep, "3", year, 100)}}))
	assert.Equal(t, 200, other.Status, "ainda é o mesmo parlamentar já usado")
	fp2 := p.newFundingProject("convenio", "municipal_government", nil)
	assert.Equal(t, 422, fp2.try("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 100)}})).Status, "em outro projeto, o inativo não pode ser escolhido")
}

func TestParlamentarEmUsoNaoExcluiMasDesativa(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	used := p.parliamentarian("Fulano", "state")
	free := p.parliamentarian("Beltrano", "state")
	inUse := func(id string) bool {
		for _, it := range p.do("GET", "/api/admin/catalog/parliamentarians", nil, p.boss).list() {
			if m := it.(map[string]any); m["id"] == id {
				v, _ := m["inUse"].(bool)
				return v
			}
		}
		require.Fail(t, "parlamentar fora da listagem", id)
		return false
	}
	assert.False(t, inUse(used))

	fp.save("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(used, "1", time.Now().Year(), 100)}}))
	assert.True(t, inUse(used))
	assert.False(t, inUse(free))
	assert.Equal(t, true, p.do("GET", "/api/admin/catalog/parliamentarians/"+used, nil, p.boss).data()["inUse"])

	assert.Equal(t, "parliamentarian_in_use", p.do("DELETE", "/api/admin/catalog/parliamentarians/"+used, nil, p.boss).errCode())
	r := p.do("PUT", "/api/admin/catalog/parliamentarians/"+used, map[string]any{"name": "Fulano", "chamber": "state", "active": false}, p.boss)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, true, r.data()["inUse"], "desativar não tira o vínculo")
	assert.Equal(t, 204, p.do("DELETE", "/api/admin/catalog/parliamentarians/"+free, nil, p.boss).Status)
}

func TestContrapartida(t *testing.T) {
	p := newProjEnv(t)
	conv := p.newFundingProject("convenio", "municipal_government", nil)

	r := conv.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 10_000_000}))
	assert.Equal(t, "counterpart_missing", issueFields(r)["funding.counterpartKind"], "convênio sem contrapartida: aviso")
	for _, it := range r.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "counterpart_missing" {
			assert.Equal(t, "warn", m["severity"])
		}
	}
	r = conv.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_000_000}))
	assert.NotContains(t, issueFields(r), "funding.counterpartKind")
	// Calamidade dispensa a contrapartida (sem aviso).
	cal := p.newFundingProject("convenio", "municipal_government", func(b map[string]any) { b["calamity"] = true })
	r = cal.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 10_000_000}))
	assert.NotContains(t, issueFields(r), "funding.counterpartKind")

	// Tipo sem valor, ou valor sem tipo.
	assert.Equal(t, 422, conv.try("funding", funding(map[string]any{"type": "own", "ownAmountCents": 100, "counterpartCents": 100})).Status)
	r = conv.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 100, "counterpartKind": "financial"}))
	assert.Equal(t, "required", issueFields(r)["funding.counterpartCents"])
	r = conv.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 100, "counterpartKind": "in_kind", "counterpartCents": 50}))
	assert.Equal(t, "required", issueFields(r)["funding.counterpartDescription"])

	// Termo de Fomento: contrapartida só em bens e serviços.
	fom := p.newFundingProject("fomento", "association", nil)
	r = fom.try("funding", funding(map[string]any{"type": "amendment", "amendments": []any{amendment(p.parliamentarian("X", "state"), "1", time.Now().Year(), 100)}, "counterpartKind": "financial", "counterpartCents": 10}))
	assert.Equal(t, 422, r.Status)
	assert.Contains(t, fieldsOf(r), "counterpartKind")
	r = fom.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 5_000_000, "counterpartKind": "in_kind", "counterpartCents": 500_000, "counterpartDescription": "Cessão do espaço e equipe."}))
	assert.NotContains(t, issueFields(r), "funding.counterpartKind")
	assert.NotContains(t, issueFields(r), "funding.counterpartDescription")

	// Mudar para Fomento com contrapartida financeira já gravada vira pendência (a ordem de preenchimento é livre).
	late := p.newFundingProject("convenio", "association", nil)
	late.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": 100, "counterpartKind": "financial", "counterpartCents": 10}))
	ib := institutionBody(0, p.typeID("association"))
	ib["instrument"] = "fomento"
	r = late.save("institution", ib)
	assert.Equal(t, "counterpart_not_financial", issueFields(r)["funding.counterpartKind"])
}

func TestCustosIndiretosDoFomento(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("fomento", "association", nil)
	save := func(total, indirect int64) resp {
		return fp.save("funding", funding(map[string]any{"type": "own", "ownAmountCents": total, "indirectCostsCents": indirect,
			"tenderStatus": "waived", "tenderLegalBasis": "art19_i", "tenderJustification": "x"}))
	}
	over := func(r resp) bool { return issueFields(r)["funding.indirectCostsCents"] == "indirect_costs_over_limit" }

	// Até R$ 100 mil: 20%, limitado a R$ 15 mil.
	assert.False(t, over(save(10_000_000, 1_500_000)), "R$ 15.000,00 de R$ 100.000,00 está no teto")
	assert.True(t, over(save(10_000_000, 1_500_001)))
	assert.False(t, over(save(5_000_000, 1_000_000)), "20% de R$ 50.000,00")
	assert.True(t, over(save(5_000_000, 1_000_001)))
	// Até R$ 500 mil: 15%, teto de R$ 50 mil.
	assert.False(t, over(save(30_000_000, 4_500_000)), "15% de R$ 300.000,00")
	assert.True(t, over(save(30_000_000, 4_500_001)))
	assert.False(t, over(save(50_000_000, 5_000_000)), "teto de R$ 50.000,00 na faixa")
	assert.True(t, over(save(50_000_000, 5_000_001)))
	// Acima de R$ 500 mil: 10%, teto de R$ 200 mil.
	assert.False(t, over(save(60_000_000, 6_000_000)), "10% de R$ 600.000,00")
	assert.True(t, over(save(60_000_000, 6_000_001)))
	assert.False(t, over(save(5_000_000_000, 20_000_000)), "teto de R$ 200.000,00")
	r := save(5_000_000_000, 20_000_001)
	assert.True(t, over(r))
	for _, it := range r.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "indirect_costs_over_limit" {
			assert.Contains(t, m["message"], "R$ 200.000,00", "a mensagem diz o limite")
		}
	}

	// Ao passar para Convênio, os custos indiretos são zerados.
	save(10_000_000, 1_000_000)
	ib := institutionBody(0, p.typeID("association"))
	ib["instrument"] = "convenio"
	r = fp.save("institution", ib)
	assert.EqualValues(t, 0, r.data()["funding"].(map[string]any)["indirectCostsCents"])
}

func TestChamamentoPublico(t *testing.T) {
	p := newProjEnv(t)
	tender := func(r resp) string { return issueFields(r)["funding.tenderStatus"] }
	dep := p.parliamentarian("Fulano", "state")
	year := time.Now().Year()
	byEmenda := map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 100)}}
	byOwn := map[string]any{"type": "own", "ownAmountCents": 100}
	mixed := map[string]any{"type": "mixed", "ownAmountCents": 100, "amendments": []any{amendment(dep, "1", year, 100)}}

	// Fomento: exige com recurso próprio (puro ou misto); com emenda só, não.
	fom := p.newFundingProject("fomento", "association", nil)
	assert.Empty(t, tender(fom.save("funding", copyMap(byEmenda))))
	assert.Equal(t, "tender_required", tender(fom.save("funding", copyMap(byOwn))))
	assert.Equal(t, "tender_required", tender(fom.save("funding", copyMap(mixed))))

	// Convênio: exige de entidade privada, não de pública.
	pub := p.newFundingProject("convenio", "municipal_government", nil)
	assert.Empty(t, tender(pub.save("funding", copyMap(byOwn))))
	priv := p.newFundingProject("convenio", "association", nil)
	assert.Equal(t, "tender_required", tender(priv.save("funding", copyMap(byOwn))))
	assert.Equal(t, "tender_required", tender(priv.save("funding", copyMap(byEmenda))), "convênio privado exige mesmo com emenda")

	// Chamamento realizado: edital e homologação.
	with := func(extra map[string]any) map[string]any {
		m := copyMap(byOwn)
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	r := fom.save("funding", with(map[string]any{"tenderStatus": "held"}))
	assert.Equal(t, "required", issueFields(r)["funding.tenderNoticeNumber"])
	assert.Equal(t, "required", issueFields(r)["funding.tenderApprovedOn"])
	r = fom.save("funding", with(map[string]any{"tenderStatus": "held", "tenderNoticeNumber": "001/2026", "tenderApprovedOn": "2026-03-01"}))
	for k := range issueFields(r) {
		assert.NotContains(t, k, "funding.tender")
	}
	// Dispensa e inexigibilidade: fundamento e justificativa.
	r = fom.save("funding", with(map[string]any{"tenderStatus": "waived"}))
	assert.Equal(t, "required", issueFields(r)["funding.tenderLegalBasis"])
	assert.Equal(t, "required", issueFields(r)["funding.tenderJustification"])
	r = fom.save("funding", with(map[string]any{"tenderStatus": "unfeasible", "tenderLegalBasis": "art20_ii", "tenderJustification": "Lei identifica a entidade."}))
	for k := range issueFields(r) {
		assert.NotContains(t, k, "funding.tender")
	}
	// Fundamentos: precisam combinar com a situação e com o instrumento.
	for name, extra := range map[string]map[string]any{
		"fundamento desconhecido":         {"tenderStatus": "waived", "tenderLegalBasis": "art99"},
		"dispensa com base de inexigível": {"tenderStatus": "waived", "tenderLegalBasis": "art20_i"},
		"base de convênio no fomento":     {"tenderStatus": "waived", "tenderLegalBasis": "conv14_i"},
		"base sem situação":               {"tenderLegalBasis": "art19_i"},
		"homologação futura":              {"tenderStatus": "held", "tenderApprovedOn": time.Now().AddDate(0, 0, 3).Format(time.DateOnly)},
		"justificativa longa":             {"tenderStatus": "waived", "tenderLegalBasis": "art19_i", "tenderJustification": repeat("a", 2001)},
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, 422, fom.try("funding", with(extra)).Status) })
	}
	r = priv.save("funding", with(map[string]any{"tenderStatus": "waived", "tenderLegalBasis": "conv14_iii", "tenderJustification": "Mesma entidade há 5 anos."}))
	assert.NotContains(t, issueFields(r), "funding.tenderLegalBasis")
	assert.Equal(t, 422, priv.try("funding", with(map[string]any{"tenderStatus": "waived", "tenderLegalBasis": "art19_i"})).Status, "base de fomento no convênio")
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestLei12082ShowsEEventosMusicais(t *testing.T) {
	p := newProjEnv(t)
	hire := func(b map[string]any) { b["hasArtisticHiring"] = true }
	fp := p.newFundingProject("convenio", "municipal_government", hire)
	code := func(r resp, field string) string { return issueFields(r)[field] }
	base := func(extra map[string]any) map[string]any {
		m := map[string]any{"type": "own", "ownAmountCents": 50_000_000, "counterpartKind": "financial", "counterpartCents": 5_000_000,
			"artistFeesCents": 20_000_000, "regionalArtistFeesCents": 6_000_000}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Dentro dos limites: R$ 500 mil de repasse, 10% de contrapartida, cachês de 40% e 30% deles regionais.
	r := fp.save("funding", base(nil))
	for k := range issueFields(r) {
		assert.NotContains(t, k, "lei12082")
	}
	// Teto de R$ 600 mil.
	r = fp.save("funding", base(map[string]any{"ownAmountCents": 60_000_001, "counterpartCents": 6_100_000, "artistFeesCents": 10_000_000, "regionalArtistFeesCents": 3_000_000}))
	assert.Equal(t, "lei12082_ceiling", code(r, "funding.totalCents"))
	r = fp.save("funding", base(map[string]any{"ownAmountCents": 60_000_000, "counterpartCents": 6_000_000, "artistFeesCents": 10_000_000, "regionalArtistFeesCents": 3_000_000}))
	assert.NotContains(t, issueFields(r), "funding.totalCents", "exatamente R$ 600.000,00 é permitido")
	// Contrapartida mínima de 10% para município.
	r = fp.save("funding", base(map[string]any{"counterpartCents": 4_999_999}))
	assert.Equal(t, "lei12082_counterpart", code(r, "funding.counterpartCents"))
	// Cachês até 50% do repasse.
	r = fp.save("funding", base(map[string]any{"artistFeesCents": 25_000_001, "regionalArtistFeesCents": 25_000_001}))
	assert.Equal(t, "lei12082_fees", code(r, "funding.artistFeesCents"))
	r = fp.save("funding", base(map[string]any{"artistFeesCents": 25_000_000, "regionalArtistFeesCents": 7_500_000}))
	assert.NotContains(t, issueFields(r), "funding.artistFeesCents", "exatamente 50% é permitido")
	// 30% dos cachês para artistas regionais.
	r = fp.save("funding", base(map[string]any{"regionalArtistFeesCents": 5_999_999}))
	assert.Equal(t, "lei12082_regional", code(r, "funding.regionalArtistFeesCents"))
	// Cachê obrigatório quando há contratação.
	r = fp.save("funding", base(map[string]any{"artistFeesCents": 0, "regionalArtistFeesCents": 0}))
	assert.Equal(t, "required", code(r, "funding.artistFeesCents"))
	// Autorização do Governador afasta os limites.
	r = fp.save("funding", base(map[string]any{"ownAmountCents": 90_000_000, "counterpartCents": 0, "artistFeesCents": 80_000_000, "regionalArtistFeesCents": 0, "governorWaiver": true}))
	for k := range issueFields(r) {
		assert.NotContains(t, k, "lei12082")
		assert.NotEqual(t, "funding.artistFeesCents", k)
	}
	// Regional maior que o total de cachês é erro de digitação.
	assert.Equal(t, 422, fp.try("funding", base(map[string]any{"artistFeesCents": 100, "regionalArtistFeesCents": 200})).Status)

	// Contrapartida mínima só vale para município; entidade privada não tem essa exigência.
	assoc := p.newFundingProject("fomento", "association", hire)
	r = assoc.save("funding", map[string]any{"type": "own", "ownAmountCents": 50_000_000, "artistFeesCents": 20_000_000, "regionalArtistFeesCents": 6_000_000,
		"tenderStatus": "waived", "tenderLegalBasis": "art19_i", "tenderJustification": "x"})
	assert.NotContains(t, issueFields(r), "funding.counterpartCents")

	// Sem contratação de artistas nada disso se aplica; desmarcar zera os valores.
	pb := proposalBody(0)
	pb["hasArtisticHiring"] = false
	fp.save("funding", base(nil))
	r = fp.save("proposal", pb)
	f := r.data()["funding"].(map[string]any)
	assert.EqualValues(t, 0, f["artistFeesCents"])
	assert.EqualValues(t, 0, f["regionalArtistFeesCents"])
	assert.Equal(t, false, f["governorWaiver"])
	for k := range issueFields(r) {
		assert.NotContains(t, k, "lei12082")
	}
}

func TestReferenciaDePrecoAntigaGeraAviso(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("fomento", "association", nil)
	save := func(daysAgo int) resp {
		return fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 5_000_000, "tenderStatus": "waived", "tenderLegalBasis": "art19_i",
			"tenderJustification": "x", "priceOldestCollectedOn": time.Now().AddDate(0, 0, -daysAgo).Format(time.DateOnly)})
	}
	assert.NotContains(t, issueFields(save(10)), "funding.priceOldestCollectedOn")
	r := save(90)
	assert.Equal(t, "price_reference_old", issueFields(r)["funding.priceOldestCollectedOn"])
	for _, it := range r.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "price_reference_old" {
			assert.Equal(t, "warn", m["severity"], "é aviso, não bloqueio")
		}
	}
}

func TestRecursosSoDoTitular(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	r := p.do("PUT", "/api/admin/projects/"+fp.id+"/funding", map[string]any{"version": fp.version, "type": "own", "ownAmountCents": 100}, p.joao)
	assert.Equal(t, 403, r.Status)
	r = p.do("PUT", "/api/admin/projects/"+fp.id+"/funding", map[string]any{"type": "own"}, p.maria)
	assert.Equal(t, 422, r.Status, "a versão é obrigatória")
	assert.Equal(t, 409, p.do("PUT", "/api/admin/projects/"+fp.id+"/funding", map[string]any{"version": 1, "type": "own"}, p.maria).Status)
}
