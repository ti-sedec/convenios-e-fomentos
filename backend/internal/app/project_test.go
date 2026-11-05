package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// projEnv prepara o ambiente com o seed (role Proponente) e dois proponentes que entraram pelo MT Login.
type projEnv struct {
	*env
	boss, maria, joao string
}

func newProjEnv(t *testing.T) *projEnv {
	t.Helper()
	e, boss := setup(t)
	e.runSeed(false)
	return &projEnv{env: e, boss: boss, maria: e.login("maria@exemplo.com"), joao: e.login("joao@exemplo.com")}
}

func (p *projEnv) typeID(code string) string {
	p.t.Helper()
	for _, it := range p.do("GET", "/api/admin/catalog/entity-types", nil, p.boss).list() {
		if m := it.(map[string]any); m["code"] == code {
			return m["id"].(string)
		}
	}
	p.t.Fatalf("tipo de entidade %q não encontrado", code)
	return ""
}

func (p *projEnv) create(tok string) (id string, version int) {
	p.t.Helper()
	r := p.do("POST", "/api/admin/projects", nil, tok)
	require.Equal(p.t, 201, r.Status, r.Body)
	return r.data()["id"].(string), version32(r)
}

func version32(r resp) int { return int(r.data()["version"].(float64)) }

func issueFields(r resp) map[string]string {
	out := map[string]string{}
	for _, it := range r.data()["issues"].([]any) {
		m := it.(map[string]any)
		out[m["section"].(string)+"."+m["field"].(string)] = m["code"].(string)
	}
	return out
}

func proponentBody(version int) map[string]any {
	return map[string]any{
		"version": version, "phones": []any{map[string]any{"number": "(65) 99999-8888", "type": "mobile"}},
		"emails": []any{map[string]any{"address": "Maria@Exemplo.com", "primary": true}},
		"uf":     "MT", "municipalityIbge": "5103403", "jobTitle": "Prefeita", "isLegalRepresentative": true,
	}
}

func institutionBody(version int, typeID string) map[string]any {
	return map[string]any{
		"version": version, "instrument": "convenio", "cnpj": "11.222.333/0001-81", "name": "Prefeitura Municipal de Cuiabá",
		"entityTypeId": typeID, "uf": "MT", "municipalityIbge": "5103403", "zip": "78000-000", "street": "Av. Principal", "number": "100",
		"district": "Centro", "email": "contato@cuiaba.mt.gov.br", "phone": "(65) 3333-4444",
		"habilitationStatus": "full", "habilitationNumber": "HAB-123", "habilitationValidUntil": time.Now().AddDate(1, 0, 0).Format(time.DateOnly),
	}
}

func proposalBody(version int) map[string]any {
	return map[string]any{
		"version": version, "sigconNumber": "3216/2026", "sigconRegisteredOn": "2026-02-10", "title": "Pavimentação da Rua A",
		"object": "Pavimentar 2 km da Rua A.", "justification": "A rua está intransitável.", "startDate": "2026-11-01", "endDate": "2027-05-01",
		"objectNature": "works", "municipalityCodes": []any{"5103403", "5108402"},
	}
}

func TestProjetosExigemAutenticacaoEPermissao(t *testing.T) {
	e, boss := setup(t)
	e.mkRole("vazio")
	nobody, _ := e.newAdmin(boss, "nobody@exemplo.com", "vazio")
	id := uuid.NewString()
	for _, rt := range []struct{ method, path string }{
		{"GET", "/api/admin/proponent-profile"}, {"PUT", "/api/admin/proponent-profile"},
		{"GET", "/api/admin/projects"}, {"POST", "/api/admin/projects"}, {"GET", "/api/admin/projects/" + id}, {"DELETE", "/api/admin/projects/" + id},
		{"PUT", "/api/admin/projects/" + id + "/proponent"}, {"PUT", "/api/admin/projects/" + id + "/institution"}, {"PUT", "/api/admin/projects/" + id + "/proposal"}, {"PUT", "/api/admin/projects/" + id + "/funding"},
		{"PUT", "/api/admin/projects/" + id + "/document-options"}, {"GET", "/api/admin/projects/" + id + "/documents"},
		{"PUT", "/api/admin/projects/" + id + "/documents/x/justification"}, {"POST", "/api/admin/projects/" + id + "/documents/x/files"},
		{"POST", "/api/admin/projects/" + id + "/documents/files/" + id + "/confirm"}, {"GET", "/api/admin/projects/" + id + "/documents/files/" + id + "/download"},
		{"DELETE", "/api/admin/projects/" + id + "/documents/files/" + id},
		{"POST", "/api/admin/projects/" + id + "/submit"}, {"GET", "/api/admin/projects/" + id + "/receipt"},
		{"POST", "/api/admin/projects/" + id + "/resubmit"}, {"POST", "/api/admin/projects/" + id + "/withdraw"}, {"POST", "/api/admin/projects/" + id + "/archive"},
		{"POST", "/api/admin/projects/" + id + "/deny"}, {"POST", "/api/admin/projects/" + id + "/complement-requests"}, {"POST", "/api/admin/projects/" + id + "/duplicate"},
	} {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			assert.Equal(t, 401, e.do(rt.method, rt.path, map[string]any{}, "").Status, "sem token")
			assert.Equal(t, 403, e.do(rt.method, rt.path, map[string]any{}, nobody).Status, "sem permissão")
		})
	}
}

func TestPerfilDoProponente(t *testing.T) {
	p := newProjEnv(t)

	r := p.do("GET", "/api/admin/proponent-profile", nil, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.NotEmpty(t, r.data()["name"], "o nome vem do MT Login")
	assert.Len(t, r.data()["cpf"], 11)
	assert.Equal(t, false, r.data()["complete"])
	assert.Empty(t, r.data()["phones"])

	body := map[string]any{
		"phones": []any{map[string]any{"number": "+55 (65) 99999-8888", "type": "mobile"}, map[string]any{"number": "6533334444", "type": "landline"}},
		"emails": []any{map[string]any{"address": " Maria@Exemplo.com ", "primary": false}},
		"uf":     "mt", "municipalityIbge": "5103403", "jobTitle": "  Secretária   de Obras ",
	}
	r = p.do("PUT", "/api/admin/proponent-profile", body, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, true, r.data()["complete"])
	assert.Equal(t, "MT", r.data()["uf"])
	assert.Equal(t, "Secretária de Obras", r.data()["jobTitle"])
	ph := r.data()["phones"].([]any)
	assert.Equal(t, "65999998888", ph[0].(map[string]any)["number"], "só dígitos, sem o +55")
	em := r.data()["emails"].([]any)[0].(map[string]any)
	assert.Equal(t, "maria@exemplo.com", em["address"], "minúsculas e sem espaços")
	assert.Equal(t, true, em["primary"], "sem principal marcado, o primeiro é o principal")

	// Cada proponente tem o seu.
	assert.Equal(t, false, p.do("GET", "/api/admin/proponent-profile", nil, p.joao).data()["complete"])
	assert.Equal(t, "65999998888", p.do("GET", "/api/admin/proponent-profile", nil, p.maria).data()["phones"].([]any)[0].(map[string]any)["number"])
}

func TestPerfilValidaOsContatos(t *testing.T) {
	p := newProjEnv(t)
	cases := map[string]map[string]any{
		"celular sem 9":          {"phones": []any{map[string]any{"number": "65899998888", "type": "mobile"}}},
		"celular com 10 dígitos": {"phones": []any{map[string]any{"number": "6599998888", "type": "mobile"}}},
		"fixo com 11 dígitos":    {"phones": []any{map[string]any{"number": "65999998888", "type": "landline"}}},
		"DDD inexistente":        {"phones": []any{map[string]any{"number": "10999998888", "type": "mobile"}}},
		"tipo desconhecido":      {"phones": []any{map[string]any{"number": "65999998888", "type": "radio"}}},
		"telefone repetido":      {"phones": []any{map[string]any{"number": "65999998888", "type": "mobile"}, map[string]any{"number": "(65) 99999-8888", "type": "mobile"}}},
		"e-mail inválido":        {"emails": []any{map[string]any{"address": "maria@", "primary": true}}},
		"e-mail com nome":        {"emails": []any{map[string]any{"address": "Maria <maria@exemplo.com>", "primary": true}}},
		"e-mail repetido":        {"emails": []any{map[string]any{"address": "a@b.co"}, map[string]any{"address": "A@B.CO"}}},
		"dois principais":        {"emails": []any{map[string]any{"address": "a@b.co", "primary": true}, map[string]any{"address": "c@d.co", "primary": true}}},
		"UF inválida":            {"uf": "XX"},
		"município de outra UF":  {"uf": "SP", "municipalityIbge": "5103403"},
		"município sem UF":       {"municipalityIbge": "5103403"},
		"município inexistente":  {"uf": "MT", "municipalityIbge": "0000000"},
		"cargo longo":            {"jobTitle": repeat("a", 101)},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r := p.do("PUT", "/api/admin/proponent-profile", body, p.maria)
			assert.Equal(t, 422, r.Status, r.Body)
		})
	}
	over := make([]any, 6)
	for i := range over {
		over[i] = map[string]any{"address": string(rune('a'+i)) + "@b.co"}
	}
	assert.Equal(t, 422, p.do("PUT", "/api/admin/proponent-profile", map[string]any{"emails": over}, p.maria).Status, "no máximo 5 e-mails")
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func TestCriarProjetoCopiaOPerfilEListaAsPendencias(t *testing.T) {
	p := newProjEnv(t)

	// Sem perfil: rascunho vazio, com nome e CPF do login.
	r := p.do("POST", "/api/admin/projects", nil, p.joao)
	require.Equal(t, 201, r.Status, r.Body)
	d := r.data()
	assert.Equal(t, "draft", d["status"])
	assert.EqualValues(t, 1, d["version"])
	assert.Equal(t, true, d["isOwner"])
	prop := d["proponent"].(map[string]any)
	assert.NotEmpty(t, prop["name"])
	assert.Len(t, prop["cpf"], 11)
	assert.Empty(t, prop["phones"])
	fields := issueFields(r)
	for _, f := range []string{"proponent.phones", "proponent.emails", "proponent.isLegalRepresentative", "institution.instrument", "institution.cnpj",
		"institution.entityTypeId", "institution.habilitationStatus", "proposal.sigconNumber", "proposal.title", "proposal.municipalityCodes"} {
		assert.Equal(t, "required", fields[f], f)
	}

	// Com perfil, o contato já vem preenchido (cópia: mudar o perfil depois não muda o projeto).
	require.Equal(t, 200, p.do("PUT", "/api/admin/proponent-profile", map[string]any{
		"phones": []any{map[string]any{"number": "65999998888", "type": "mobile"}}, "emails": []any{map[string]any{"address": "maria@exemplo.com", "primary": true}},
		"uf": "MT", "municipalityIbge": "5103403", "jobTitle": "Prefeita"}, p.maria).Status)
	id, _ := p.create(p.maria)
	got := p.do("GET", "/api/admin/projects/"+id, nil, p.maria)
	prop = got.data()["proponent"].(map[string]any)
	assert.Equal(t, "Prefeita", prop["jobTitle"])
	assert.Len(t, prop["phones"], 1)
	assert.NotContains(t, issueFields(got), "proponent.phones")
	require.Equal(t, 200, p.do("PUT", "/api/admin/proponent-profile", map[string]any{"jobTitle": "Outro cargo"}, p.maria).Status)
	assert.Equal(t, "Prefeita", p.do("GET", "/api/admin/projects/"+id, nil, p.maria).data()["proponent"].(map[string]any)["jobTitle"])
}

func TestSecaoProponente(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)

	r := p.do("PUT", "/api/admin/projects/"+id+"/proponent", proponentBody(v), p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.EqualValues(t, v+1, r.data()["version"], "cada gravação incrementa a versão")
	prop := r.data()["proponent"].(map[string]any)
	assert.Equal(t, "maria@exemplo.com", prop["emails"].([]any)[0].(map[string]any)["address"])
	assert.NotContains(t, issueFields(r), "proponent.phones")
	assert.NotContains(t, issueFields(r), "proponent.legalRepName", "o proponente é o representante legal")

	// Versão antiga: conflito, com a versão atual nos detalhes.
	c := p.do("PUT", "/api/admin/projects/"+id+"/proponent", proponentBody(v), p.maria)
	assert.Equal(t, 409, c.Status)
	assert.Equal(t, "project_version_conflict", c.errCode())
	assert.EqualValues(t, v+1, c.Body["error"].(map[string]any)["details"].(map[string]any)["currentVersion"])
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/proponent", map[string]any{"phones": []any{}}, p.maria).Status, "versão é obrigatória")

	// Outra pessoa como representante legal.
	cur := v + 1
	b := proponentBody(cur)
	b["isLegalRepresentative"] = false
	r = p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	for _, f := range []string{"proponent.legalRepName", "proponent.legalRepCpf", "proponent.legalRepJobTitle"} {
		assert.Equal(t, "required", issueFields(r)[f], f)
	}
	cur++
	b = proponentBody(cur)
	b["isLegalRepresentative"] = false
	b["legalRepName"], b["legalRepCpf"], b["legalRepJobTitle"] = "José da Silva", cpfFor("jose@exemplo.com"), "Prefeito"
	r = p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, cpfFor("jose@exemplo.com"), r.data()["proponent"].(map[string]any)["legalRepCpf"])
	assert.NotContains(t, issueFields(r), "proponent.legalRepName")

	cur++
	b = proponentBody(cur)
	b["isLegalRepresentative"] = false
	b["legalRepCpf"] = "111.111.111-11"
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria).Status, "CPF inválido")

	// Voltando a ser o representante, os dados do outro são descartados.
	b = proponentBody(cur)
	r = p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "", r.data()["proponent"].(map[string]any)["legalRepName"])

	// Nome e CPF vêm do login e não mudam, mesmo que o cliente os envie.
	cur++
	b = proponentBody(cur)
	b["name"], b["cpf"] = "Fulano Falso", "00000000000"
	r = p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.NotEqual(t, "Fulano Falso", r.data()["proponent"].(map[string]any)["name"])
	assert.NotEqual(t, "00000000000", r.data()["proponent"].(map[string]any)["cpf"])
}

func TestSecaoInstituicao(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	put := func(version int, mutate func(map[string]any)) resp {
		b := institutionBody(version, p.typeID("municipal_government"))
		if mutate != nil {
			mutate(b)
		}
		return p.do("PUT", "/api/admin/projects/"+id+"/institution", b, p.maria)
	}

	r := put(v, nil)
	require.Equal(t, 200, r.Status, r.Body)
	inst := r.data()["institution"].(map[string]any)
	assert.Equal(t, "11222333000181", inst["cnpj"], "CNPJ guardado só com dígitos")
	assert.Equal(t, "public", inst["nature"], "a natureza vem do tipo de entidade")
	assert.Equal(t, false, inst["nonProfit"])
	assert.Equal(t, "78000000", inst["zip"])
	assert.Equal(t, "6533334444", inst["phone"])
	assert.Equal(t, false, inst["entityTypeIsOther"])
	for k := range issueFields(r) {
		assert.NotContains(t, k, "institution.", "instituição completa não deixa pendências")
	}
	v++

	// Termo de Fomento não vale para entidade pública (nem para privada com fins lucrativos).
	r = put(v, func(b map[string]any) { b["instrument"] = "fomento" })
	assert.Equal(t, 422, r.Status)
	assert.Contains(t, r.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any)[0].(map[string]any)["field"], "instrument")
	r = put(v, func(b map[string]any) {
		b["instrument"] = "fomento"
		b["entityTypeId"] = p.typeID("public_service_concessionaire")
	})
	assert.Equal(t, 422, r.Status, "privada com fins lucrativos")
	r = put(v, func(b map[string]any) { b["instrument"] = "fomento"; b["entityTypeId"] = p.typeID("association") })
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "private", r.data()["institution"].(map[string]any)["nature"])
	assert.Equal(t, true, r.data()["institution"].(map[string]any)["nonProfit"])
	v++
	// Convênio vale para qualquer natureza.
	r = put(v, func(b map[string]any) { b["entityTypeId"] = p.typeID("association") })
	require.Equal(t, 200, r.Status, r.Body)
	v++

	// Tipo "Outro": texto livre e natureza informados pelo proponente.
	other := p.typeID("other")
	r = put(v, func(b map[string]any) { b["entityTypeId"] = other })
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, true, r.data()["institution"].(map[string]any)["entityTypeIsOther"])
	assert.Equal(t, "required", issueFields(r)["institution.entityTypeOther"])
	assert.Equal(t, "required", issueFields(r)["institution.nature"])
	v++
	r = put(v, func(b map[string]any) {
		b["entityTypeId"], b["entityTypeOther"], b["nature"], b["nonProfit"], b["instrument"] = other, "Coletivo cultural", "private", true, "fomento"
	})
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "Coletivo cultural", r.data()["institution"].(map[string]any)["entityTypeOther"])
	assert.NotContains(t, issueFields(r), "institution.entityTypeOther")
	v++
	assert.Equal(t, 422, put(v, func(b map[string]any) { b["entityTypeId"], b["nature"], b["nonProfit"] = other, "public", true }).Status, "pública não é sem fins lucrativos")
	// Mudar para um tipo comum descarta o texto livre.
	r = put(v, nil)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "", r.data()["institution"].(map[string]any)["entityTypeOther"])
	v++

	// Validações de formato.
	for name, mutate := range map[string]func(map[string]any){
		"CNPJ inválido":         func(b map[string]any) { b["cnpj"] = "11.222.333/0001-82" },
		"instrumento inválido":  func(b map[string]any) { b["instrument"] = "contrato" },
		"tipo inexistente":      func(b map[string]any) { b["entityTypeId"] = uuid.NewString() },
		"tipo mal formado":      func(b map[string]any) { b["entityTypeId"] = "x" },
		"CEP curto":             func(b map[string]any) { b["zip"] = "7800" },
		"e-mail inválido":       func(b map[string]any) { b["email"] = "contato" },
		"telefone inválido":     func(b map[string]any) { b["phone"] = "123" },
		"município de outra UF": func(b map[string]any) { b["uf"] = "SP" },
		"habilitação inválida":  func(b map[string]any) { b["habilitationStatus"] = "talvez" },
		"validade inválida":     func(b map[string]any) { b["habilitationValidUntil"] = "31/12/2030" },
		"nome longo":            func(b map[string]any) { b["name"] = repeat("a", 201) },
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, 422, put(v, mutate).Status) })
	}
}

func TestHabilitacaoGeraPendencias(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	save := func(mutate func(map[string]any)) resp {
		b := institutionBody(v, p.typeID("municipal_government"))
		mutate(b)
		r := p.do("PUT", "/api/admin/projects/"+id+"/institution", b, p.maria)
		require.Equal(t, 200, r.Status, r.Body)
		v++
		return r
	}
	assert.Equal(t, "habilitation_not_full", issueFields(save(func(b map[string]any) { b["habilitationStatus"] = "partial" }))["institution.habilitationStatus"])
	assert.Equal(t, "habilitation_expired", issueFields(save(func(b map[string]any) { b["habilitationValidUntil"] = "2020-01-01" }))["institution.habilitationValidUntil"])
	soon := save(func(b map[string]any) {
		b["habilitationValidUntil"] = time.Now().AddDate(0, 0, 10).Format(time.DateOnly)
	})
	assert.Equal(t, "habilitation_expiring", issueFields(soon)["institution.habilitationValidUntil"])
	for _, it := range soon.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "habilitation_expiring" {
			assert.Equal(t, "warn", m["severity"], "vencimento próximo é aviso, não bloqueio")
		}
	}
}

func TestSecaoProposta(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	put := func(version int, mutate func(map[string]any)) resp {
		b := proposalBody(version)
		if mutate != nil {
			mutate(b)
		}
		return p.do("PUT", "/api/admin/projects/"+id+"/proposal", b, p.maria)
	}

	r := put(v, nil)
	require.Equal(t, 200, r.Status, r.Body)
	prop := r.data()["proposal"].(map[string]any)
	assert.Equal(t, "3216/2026", prop["sigconNumber"])
	assert.Equal(t, "works", prop["objectNature"])
	muns := prop["municipalities"].([]any)
	require.Len(t, muns, 2)
	assert.Equal(t, "Cuiabá", muns[0].(map[string]any)["name"])
	assert.Equal(t, "Várzea Grande", muns[1].(map[string]any)["name"])
	assert.Equal(t, "MT", muns[0].(map[string]any)["uf"])
	for k := range issueFields(r) {
		assert.NotContains(t, k, "proposal.", "proposta completa não deixa pendências")
	}
	v++

	// Trocar os municípios substitui a lista; omitir apaga.
	r = put(v, func(b map[string]any) { b["municipalityCodes"] = []any{"5103403", "5103403"} })
	require.Equal(t, 200, r.Status, r.Body)
	assert.Len(t, r.data()["proposal"].(map[string]any)["municipalities"], 1, "repetidos viram um")
	v++
	r = put(v, func(b map[string]any) { delete(b, "municipalityCodes") })
	require.Equal(t, 200, r.Status)
	assert.Empty(t, r.data()["proposal"].(map[string]any)["municipalities"])
	assert.Equal(t, "required", issueFields(r)["proposal.municipalityCodes"])
	v++

	// O ano do número tem que bater com o da data de cadastro (pendência, não erro de gravação).
	r = put(v, func(b map[string]any) { b["sigconNumber"] = "3216/2025" })
	require.Equal(t, 200, r.Status)
	assert.Equal(t, "sigcon_year_mismatch", issueFields(r)["proposal.sigconNumber"])
	v++

	future := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	for name, mutate := range map[string]func(map[string]any){
		"número fora do formato":    func(b map[string]any) { b["sigconNumber"] = "3216-2026" },
		"número sem ano":            func(b map[string]any) { b["sigconNumber"] = "3216" },
		"cadastro no futuro":        func(b map[string]any) { b["sigconRegisteredOn"] = future },
		"data inválida":             func(b map[string]any) { b["startDate"] = "01/11/2026" },
		"término antes do início":   func(b map[string]any) { b["startDate"], b["endDate"] = "2027-05-01", "2026-11-01" },
		"natureza inválida":         func(b map[string]any) { b["objectNature"] = "magia" },
		"município de outro estado": func(b map[string]any) { b["municipalityCodes"] = []any{"3550308"} },
		"município inexistente":     func(b map[string]any) { b["municipalityCodes"] = []any{"0000000"} },
		"título longo":              func(b map[string]any) { b["title"] = repeat("a", 201) },
		"objeto longo":              func(b map[string]any) { b["object"] = repeat("a", 5001) },
		"mais de 50 municípios":     func(b map[string]any) { b["municipalityCodes"] = manyCodes(p, 51) },
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, 422, put(v, mutate).Status) })
	}
}

func manyCodes(p *projEnv, n int) []any {
	list := p.do("GET", "/api/admin/catalog/municipalities?uf=MT&pageSize=100", nil, p.boss).list()
	out := make([]any, 0, n)
	for _, it := range list[:n] {
		out = append(out, it.(map[string]any)["ibgeCode"])
	}
	return out
}

func TestProjetoCompletoNaoTemPendencias(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	r := p.do("PUT", "/api/admin/projects/"+id+"/proponent", proponentBody(v), p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	r = p.do("PUT", "/api/admin/projects/"+id+"/institution", institutionBody(version32(r), p.typeID("municipal_government")), p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	r = p.do("PUT", "/api/admin/projects/"+id+"/proposal", proposalBody(version32(r)), p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	r = p.do("PUT", "/api/admin/projects/"+id+"/funding", map[string]any{"version": version32(r), "type": "own", "ownAmountCents": 10_000_000,
		"counterpartKind": "financial", "counterpartCents": 1_000_000}, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	// Só o checklist de documentos fica pendente: todas as seções preenchidas não deixam outra pendência.
	for k := range issueFields(r) {
		if strings.HasPrefix(k, "submission.") {
			continue // aviso de prazo eleitoral: depende da data em que o teste roda
		}
		assert.Equal(t, "documents.documents", k)
	}
	assert.EqualValues(t, 5, r.data()["version"])
}

func TestNumeroDoSigconRepetidoGeraAviso(t *testing.T) {
	p := newProjEnv(t)
	a, av := p.create(p.maria)
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+a+"/proposal", proposalBody(av), p.maria).Status)
	b, bv := p.create(p.joao)
	r := p.do("PUT", "/api/admin/projects/"+b+"/proposal", proposalBody(bv), p.joao)
	require.Equal(t, 200, r.Status, "o rascunho de outra pessoa não impede de gravar")
	assert.Equal(t, "sigcon_duplicated", issueFields(r)["proposal.sigconNumber"])
	for _, it := range r.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "sigcon_duplicated" {
			assert.Equal(t, "warn", m["severity"])
		}
	}
}

func TestProponenteSoEnxergaEEditaOsProprios(t *testing.T) {
	p := newProjEnv(t)
	mine, mv := p.create(p.maria)
	_, _ = p.create(p.maria)
	theirs, _ := p.create(p.joao)

	list := p.do("GET", "/api/admin/projects", nil, p.maria)
	require.Equal(t, 200, list.Status)
	assert.Len(t, list.list(), 2)
	assert.EqualValues(t, 2, list.meta()["total"])
	for _, it := range list.list() {
		assert.Equal(t, true, it.(map[string]any)["isOwner"])
	}
	assert.Len(t, p.do("GET", "/api/admin/projects", nil, p.joao).list(), 1)

	// O de outra pessoa não existe para Maria (404), nem para ler, editar ou excluir (403 na escrita do registro alheio).
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+theirs, nil, p.maria).Status)
	assert.Equal(t, 403, p.do("PUT", "/api/admin/projects/"+theirs+"/proposal", proposalBody(1), p.maria).Status)
	assert.Equal(t, 403, p.do("PUT", "/api/admin/projects/"+theirs+"/proponent", proponentBody(1), p.maria).Status)
	assert.Equal(t, 403, p.do("PUT", "/api/admin/projects/"+theirs+"/institution", institutionBody(1, p.typeID("association")), p.maria).Status)
	assert.Equal(t, 403, p.do("DELETE", "/api/admin/projects/"+theirs, nil, p.maria).Status)
	assert.Equal(t, 200, p.do("GET", "/api/admin/projects/"+theirs, nil, p.joao).Status, "o dono segue enxergando")
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+uuid.NewString(), nil, p.maria).Status)
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/não-é-uuid", nil, p.maria).Status)
	assert.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+mine+"/proposal", proposalBody(mv), p.maria).Status)

	// Quem lê todos os projetos (equipe) vê os de todos, sem poder de dono.
	p.mkRole("leitor", "admin::projects.read")
	staff, _ := p.newAdmin(p.boss, "bia@exemplo.com", "leitor")
	all := p.do("GET", "/api/admin/projects", nil, staff)
	assert.EqualValues(t, 3, all.meta()["total"])
	for _, it := range all.list() {
		assert.Equal(t, false, it.(map[string]any)["isOwner"])
	}
	assert.Equal(t, 200, p.do("GET", "/api/admin/projects/"+theirs, nil, staff).Status)
	assert.Equal(t, 403, p.do("PUT", "/api/admin/projects/"+theirs+"/proposal", proposalBody(1), staff).Status, "ler não dá editar")
}

func TestCnpjAlfanumerico(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	ib := institutionBody(v, p.typeID("municipal_government"))
	ib["cnpj"] = "12.abc.345/01de-35" // exemplo da Receita; minúsculas e pontuação são normalizadas
	r := p.do("PUT", "/api/admin/projects/"+id+"/institution", ib, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "12ABC34501DE35", r.data()["institution"].(map[string]any)["cnpj"])

	ids := func(q string) []string {
		var out []string
		for _, it := range p.do("GET", "/api/admin/projects?q="+q, nil, p.maria).list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	assert.Equal(t, []string{id}, ids("12.ABC.345"), "busca com pontuação")
	assert.Equal(t, []string{id}, ids("12abc345"), "busca sem pontuação e em minúsculas")
	assert.Empty(t, ids("12ABD"))

	ib["cnpj"] = "12.ABC.345/01DE-36" // dígito verificador errado
	ib["version"] = r.data()["version"]
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/institution", ib, p.maria).Status)
}

func TestListagemDeProjetos(t *testing.T) {
	p := newProjEnv(t)
	a, av := p.create(p.maria)
	b, bv := p.create(p.maria)
	ib := institutionBody(av, p.typeID("municipal_government"))
	ib["name"] = "Prefeitura de Sinop"
	ib["cnpj"] = "00.000.000/0001-91"
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+a+"/institution", ib, p.maria).Status)
	pb := proposalBody(bv)
	pb["title"] = "Reforma da escola"
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+b+"/proposal", pb, p.maria).Status)

	ids := func(path string) []string {
		var out []string
		for _, it := range p.do("GET", path, nil, p.maria).list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	assert.Equal(t, []string{b, a}, ids("/api/admin/projects"), "mais recente primeiro")
	assert.Equal(t, []string{a}, ids("/api/admin/projects?q=sinop"))
	assert.Equal(t, []string{a}, ids("/api/admin/projects?q=00.000.000"), "busca por CNPJ com ou sem pontuação")
	assert.Equal(t, []string{b}, ids("/api/admin/projects?q=ESCOLA"))
	assert.Equal(t, []string{b}, ids("/api/admin/projects?q=3216%2F2026"))
	assert.Empty(t, ids("/api/admin/projects?q=%25"), "% é literal, não coringa")
}

func TestExcluirRascunho(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	assert.Equal(t, 204, p.do("DELETE", "/api/admin/projects/"+id, nil, p.maria).Status)
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+id, nil, p.maria).Status)
	assert.Equal(t, 404, p.do("DELETE", "/api/admin/projects/"+id, nil, p.maria).Status)
	assert.Equal(t, 404, p.do("PUT", "/api/admin/projects/"+id+"/proposal", proposalBody(1), p.maria).Status)
}

func TestProjetosSaoAuditadosSemDadosPessoais(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+id+"/proponent", proponentBody(v), p.maria).Status)
	p.do("DELETE", "/api/admin/projects/"+id, nil, p.maria)

	for _, action := range []string{"project.create", "project.update", "project.delete"} {
		logs := p.do("GET", "/api/admin/audit-logs?action="+action+"&entityId="+id, nil, p.boss)
		require.Len(t, logs.list(), 1, action)
	}
	upd := p.do("GET", "/api/admin/audit-logs?action=project.update", nil, p.boss).list()[0].(map[string]any)["payload"].(map[string]any)
	assert.Equal(t, "proponent", upd["section"])
	var payloads []byte
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT string_agg(payload::text, ' ') FROM audit_logs WHERE entity = 'project'").Scan(&payloads))
	assert.NotContains(t, string(payloads), "99999", "telefones não vão para a auditoria")
	assert.NotContains(t, string(payloads), "maria@exemplo.com")
}
