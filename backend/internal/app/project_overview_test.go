package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overview lê o resumo de GET /admin/projects/overview.
func (p *projEnv) overview(tok, query string) map[string]any {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/overview"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r.data()
}

// attention devolve os itens de atenção por tipo e projeto.
func attention(o map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, it := range o["attention"].([]any) {
		m := it.(map[string]any)
		out[m["kind"].(string)+":"+m["projectId"].(string)] = m
	}
	return out
}

func TestVisaoGeralDoProponente(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()

	draft, _ := p.create(p.maria)
	sent := p.submitted("8001/2026")
	returned := p.submitted("8002/2026")
	require.Equal(t, 200, p.complement(analyst, returned.id, map[string]any{"message": "Corrija e reenvie.",
		"items": []any{section("institution", "O CEP não confere."), document("requerimento_apoio", "Faltou a assinatura.")}}).Status)

	// Titular: um rascunho, dois enviados (um deles devolvido).
	o := p.overview(p.maria, "")
	counts := o["counts"].(map[string]any)
	assert.EqualValues(t, 1, counts["draft"])
	assert.EqualValues(t, 1, counts["awaiting_checklist"])
	assert.EqualValues(t, 1, counts["awaiting_proponent"])
	for _, k := range []string{"in_analysis", "awaiting_opinion", "archived"} {
		assert.EqualValues(t, 0, counts[k], k)
	}
	assert.Equal(t, map[string]any{"owner": float64(3), "member": float64(0)}, o["byAccess"])

	// O valor em tramitação soma só os enviados.
	var want float64
	for _, it := range p.do("GET", "/api/admin/projects?status=submitted", nil, p.maria).list() {
		want += it.(map[string]any)["totalCents"].(float64)
	}
	cents := o["activeCents"].(map[string]any)
	require.Greater(t, want, float64(0))
	assert.Equal(t, want, cents["total"])
	assert.Equal(t, want, cents["convenio"].(float64)+cents["fomento"].(float64))

	att := attention(o)
	comp := att["complement_open:"+returned.id]
	require.NotNil(t, comp, o["attention"])
	assert.EqualValues(t, 2, comp["items"])
	assert.NotEmpty(t, comp["dueAt"])
	assert.NotEqual(t, true, comp["overdue"])
	d := att["draft:"+draft]
	require.NotNil(t, d)
	assert.Greater(t, d["blocking"], float64(0), "o rascunho vazio tem pendências que bloqueiam o envio")
	assert.Nil(t, att["complement_open:"+sent.id])
	// O complemento vem antes do rascunho (por urgência).
	assert.Equal(t, "complement_open", o["attention"].([]any)[0].(map[string]any)["kind"])

	// Prazo vencido: continua listado e marcado, e o resumo não arquiva.
	p.setDue(returned.id, time.Now().Add(-time.Hour))
	late := attention(p.overview(p.maria, ""))["complement_open:"+returned.id]
	require.NotNil(t, late)
	assert.Equal(t, true, late["overdue"])
	var stage string
	require.NoError(t, p.pool.QueryRow(t.Context(), "SELECT stage FROM projects WHERE id = $1", returned.id).Scan(&stage))
	assert.Equal(t, "awaiting_proponent", stage, "o resumo só lê")

	// Filtro de acesso: tudo é dela, então "member" fica vazio e "owner" igual a "all".
	assert.EqualValues(t, 0, p.overview(p.maria, "?access=member")["counts"].(map[string]any)["draft"])
	assert.Empty(t, p.overview(p.maria, "?access=member")["attention"])
	assert.Equal(t, p.overview(p.maria, "?access=all")["counts"], p.overview(p.maria, "?access=owner")["counts"])
}

func TestVisaoGeralDoMembro(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()

	returned := p.submitted("8101/2026")
	require.Equal(t, 200, p.complement(analyst, returned.id, map[string]any{"message": "Corrija e reenvie.", "items": []any{section("institution", "CEP.")}}).Status)
	sharedDraft, _ := p.create(p.maria)
	readOnlyDraft, _ := p.create(p.maria)
	p.mustInvite(returned.id, "carla@exemplo.com", "read")
	editMember := p.mustInvite(sharedDraft, "carla@exemplo.com", "edit")
	p.mustInvite(readOnlyDraft, "carla@exemplo.com", "read")
	carla := p.login("carla@exemplo.com")
	own, _ := p.create(carla)

	o := p.overview(carla, "")
	assert.Equal(t, map[string]any{"owner": float64(1), "member": float64(3)}, o["byAccess"])
	att := attention(o)

	// Convites ainda não abertos: aviso para qualquer nível, com o nome de quem convidou.
	for id, level := range map[string]string{returned.id: "read", sharedDraft: "edit", readOnlyDraft: "read"} {
		n := att["shared_new:"+id]
		require.NotNil(t, n, id)
		assert.Equal(t, level, n["level"])
		assert.NotEmpty(t, n["invitedBy"])
	}
	// Só age quem pode: o leitor não recebe a complementação nem o rascunho; o editor recebe o rascunho compartilhado.
	assert.Nil(t, att["complement_open:"+returned.id])
	assert.Nil(t, att["draft:"+readOnlyDraft])
	assert.NotNil(t, att["draft:"+sharedDraft])
	assert.NotNil(t, att["draft:"+own])
	// O leitor enxerga a contagem do projeto devolvido.
	assert.EqualValues(t, 1, o["counts"].(map[string]any)["awaiting_proponent"])

	// Filtros: "owner" só o dela; "member" não traz o rascunho próprio.
	own1 := p.overview(carla, "?access=owner")
	assert.EqualValues(t, 1, own1["counts"].(map[string]any)["draft"])
	assert.Equal(t, map[string]any{"owner": float64(1), "member": float64(3)}, own1["byAccess"], "os totais por acesso ignoram o filtro")
	assert.Nil(t, attention(own1)["shared_new:"+returned.id])
	mem := p.overview(carla, "?access=member")
	assert.EqualValues(t, 2, mem["counts"].(map[string]any)["draft"])
	assert.Nil(t, attention(mem)["draft:"+own])
	assert.NotNil(t, attention(mem)["draft:"+sharedDraft])

	// Abrir o projeto aceita o convite: o aviso some.
	p.project(carla, sharedDraft)
	assert.Nil(t, attention(p.overview(carla, ""))["shared_new:"+sharedDraft])
	assert.NotNil(t, attention(p.overview(carla, ""))["shared_new:"+returned.id])

	// Revogar o acesso tira o projeto de tudo na hora.
	require.Equal(t, 204, p.do("DELETE", membersPath(sharedDraft, "/", editMember), nil, p.maria).Status)
	o = p.overview(carla, "")
	assert.Equal(t, map[string]any{"owner": float64(1), "member": float64(2)}, o["byAccess"])
	assert.Nil(t, attention(o)["draft:"+sharedDraft])
}

// A visão geral é sempre do que é da pessoa, mesmo para quem lê todos os projetos.
func TestVisaoGeralNaoViraPainelDaEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.create(p.maria)
	p.submitted("8201/2026")

	tok, _ := p.newAdmin(p.boss, "analista-resumo@exemplo.com", "analyst")
	require.NotEmpty(t, p.do("GET", "/api/admin/projects", nil, tok).list(), "o analista vê o projeto enviado")
	o := p.overview(tok, "")
	assert.Equal(t, map[string]any{"owner": float64(0), "member": float64(0)}, o["byAccess"])
	for k, v := range o["counts"].(map[string]any) {
		assert.EqualValues(t, 0, v, k)
	}
	assert.Empty(t, o["attention"])
	assert.EqualValues(t, 0, o["activeCents"].(map[string]any)["total"])

	// O Super Admin lê tudo, e também só recebe o que é dele.
	assert.Equal(t, map[string]any{"owner": float64(0), "member": float64(0)}, p.overview(p.boss, "")["byAccess"])

	for _, bad := range []string{"?access=mine", "?access=OWNER"} {
		assert.Equal(t, 422, p.do("GET", "/api/admin/projects/overview"+bad, nil, p.maria).Status, bad)
	}
	assert.Equal(t, 401, p.do("GET", "/api/admin/projects/overview", nil, "").Status)
}

func TestVisaoGeralAvisaAssinaturaPendente(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	assert.Nil(t, attention(p.overview(p.maria, ""))["signature_pending:"+fp.id], "ainda não foi emitida")

	fp.issue(contrapartida)
	it := attention(p.overview(p.maria, ""))["signature_pending:"+fp.id]
	require.NotNil(t, it)
	assert.NotEmpty(t, it["declaration"], "traz o nome da declaração")
	// O rascunho continua pedindo atenção ao mesmo tempo.
	assert.NotNil(t, attention(p.overview(p.maria, ""))["draft:"+fp.id])
}
