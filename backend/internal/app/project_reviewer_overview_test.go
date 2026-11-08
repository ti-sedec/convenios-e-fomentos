package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reviewerOverview lê o resumo do departamento do Revisor.
func (p *projEnv) reviewerOverview(tok string) resp {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/department/overview", nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r
}

// pendingOf devolve o total, os ids (na ordem) e os itens do bloco de pendentes.
func pendingOf(r resp) (total int, ids []string, items []map[string]any) {
	b := r.data()["pending"].(map[string]any)
	for _, it := range b["items"].([]any) {
		m := it.(map[string]any)
		ids, items = append(ids, m["projectId"].(string)), append(items, m)
	}
	return int(b["total"].(float64)), ids, items
}

func TestVisaoGeralDoRevisor(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	staff := p.newStaff()
	fin, jur := p.deptID("Financeiro"), p.deptID("Jurídico")
	colleague := p.reviewerOf("colega.revisor.fin@exemplo.com", fin)
	an := staff.analyst
	const secret = "JUSTIFICATIVA-INTERNA-DA-RECUSA"

	// ready é um projeto assumido e com todos os itens aprovados, pronto para indicar o departamento.
	ready := func(sigcon string) *fundingProject {
		fp := p.submitted(sigcon)
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
		require.True(t, allApproved(p.approveAll(an, fp.id)))
		return fp
	}
	old := ready("9101/2026")       // com o Financeiro há 6 dias (limite 5)
	recent := ready("9102/2026")    // com o Financeiro agora
	other := ready("9103/2026")     // com o Jurídico
	confirmed := ready("9104/2026") // confirmado por Regina
	refused := ready("9105/2026")   // recusado: sai do alcance, mas conta como decisão
	archived := ready("9106/2026")  // negado pelo Secretário enquanto esperava
	noDept := ready("9107/2026")    // ainda sem departamento
	p.create(p.maria)               // rascunho: nunca entra

	for _, fp := range []*fundingProject{old, recent, confirmed, refused, archived} {
		require.Equal(t, 200, p.assign(an, fp.id, fin).Status)
	}
	require.Equal(t, 200, p.assign(an, other.id, jur).Status)
	require.Equal(t, 200, p.confirm(staff.reviewerSame, confirmed.id, 1).Status)
	require.Equal(t, 200, p.refuse(staff.reviewerSame, refused.id, 1, secret).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+archived.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)
	p.age(old.id, 10, 9, 6) // envio, análise e enquadramento: o trecho aberto começou há 6 dias

	r := p.reviewerOverview(staff.reviewerSame)
	assert.Equal(t, "Financeiro", r.data()["department"].(map[string]any)["name"])
	assert.Equal(t, fin, r.data()["department"].(map[string]any)["id"])
	assert.Equal(t, map[string]any{"framing": float64(5), "awaiting_opinion": float64(15)}, r.data()["limits"])
	assert.Equal(t, map[string]any{"framing_pending": float64(2), "awaiting_opinion": float64(1), "archived_30d": float64(1),
		"decided_30d": map[string]any{"confirmed": float64(1), "refused": float64(1)}}, r.data()["counts"])

	t.Run("pendentes: o que está há mais tempo com o departamento primeiro", func(t *testing.T) {
		total, ids, items := pendingOf(r)
		assert.Equal(t, 2, total)
		assert.Equal(t, []string{old.id, recent.id}, ids)
		assert.Equal(t, true, items[0]["overLimit"])
		assert.NotEqual(t, true, items[1]["overLimit"])
		assert.GreaterOrEqual(t, items[0]["sinceSeconds"].(float64), float64(5*86400))
		assert.EqualValues(t, 1, items[0]["round"])
		assert.NotEmpty(t, items[0]["indicatedAt"])
		assert.NotEmpty(t, items[0]["indicatedBy"], "o nome do Analista que indicou")
		assert.NotEmpty(t, items[0]["title"])
		assert.NotEmpty(t, items[0]["code"])
		assert.NotContains(t, ids, noDept.id, "ainda não foi indicado")
		assert.NotContains(t, ids, other.id, "é do Jurídico")
	})

	t.Run("outro departamento só enxerga o seu", func(t *testing.T) {
		theirs := p.reviewerOverview(staff.reviewerOther)
		assert.Equal(t, "Jurídico", theirs.data()["department"].(map[string]any)["name"])
		total, ids, _ := pendingOf(theirs)
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{other.id}, ids)
		assert.Equal(t, map[string]any{"framing_pending": float64(1), "awaiting_opinion": float64(0), "archived_30d": float64(0),
			"decided_30d": map[string]any{"confirmed": float64(0), "refused": float64(0)}}, theirs.data()["counts"])
	})

	t.Run("sem departamento no cadastro", func(t *testing.T) {
		// O Revisor sem departamento nem passa pela permissão (a condição do perfil nega tudo): a tela trata esse 403 como "sem departamento".
		assert.Equal(t, 403, p.do("GET", "/api/admin/projects/department/overview", nil, staff.reviewerNone).Status)
		// O Super Admin passa por qualquer permissão, mas não tem departamento: recebe tudo zerado, não os projetos de todos.
		none := p.reviewerOverview(staff.superAdmin)
		assert.Nil(t, none.data()["department"])
		total, ids, _ := pendingOf(none)
		assert.Equal(t, 0, total)
		assert.Empty(t, ids)
		assert.NotNil(t, none.data()["pending"].(map[string]any)["items"], "lista vazia, não nula")
		assert.Equal(t, map[string]any{"framing_pending": float64(0), "awaiting_opinion": float64(0), "archived_30d": float64(0),
			"decided_30d": map[string]any{"confirmed": float64(0), "refused": float64(0)}}, none.data()["counts"])
	})

	t.Run("a primeira decisão de um colega tira o projeto da lista", func(t *testing.T) {
		require.Equal(t, 200, p.confirm(colleague, recent.id, 1).Status)
		after := p.reviewerOverview(staff.reviewerSame)
		_, ids, _ := pendingOf(after)
		assert.Equal(t, []string{old.id}, ids)
		c := after.data()["counts"].(map[string]any)
		assert.EqualValues(t, 1, c["framing_pending"])
		assert.EqualValues(t, 2, c["awaiting_opinion"])
		assert.EqualValues(t, 2, c["decided_30d"].(map[string]any)["confirmed"], "as decisões são do departamento, não da pessoa")
	})

	t.Run("nova indicação depois da recusa volta a pendente, na rodada nova", func(t *testing.T) {
		require.Equal(t, 200, p.assign(an, refused.id, fin).Status)
		_, ids, items := pendingOf(p.reviewerOverview(staff.reviewerSame))
		assert.ElementsMatch(t, []string{old.id, refused.id}, ids)
		for _, it := range items {
			if it["projectId"] == refused.id {
				assert.EqualValues(t, 2, it["round"])
			}
		}
	})

	t.Run("nada pessoal do proponente nem a justificativa da recusa", func(t *testing.T) {
		body := strings.ToLower(fmt.Sprint(p.reviewerOverview(staff.reviewerSame).Body))
		for _, banned := range []string{cpfFor("maria@exemplo.com"), "maria@exemplo.com", "phone", "cpf", "email", strings.ToLower(secret)} {
			assert.NotContains(t, body, banned)
		}
	})

	t.Run("quem não é Revisor não vê, e o Revisor não vê a fila do Analista", func(t *testing.T) {
		for name, tok := range map[string]string{"analista": an, "supervisor": staff.supervisor, "secretário": staff.secretary, "proponente": p.maria} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/department/overview", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/department/overview", nil, "").Status)
		assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/overview", nil, staff.reviewerSame).Status)
	})

	t.Run("sem regra vigente, nada é marcado como acima do limite", func(t *testing.T) {
		list := p.do("GET", "/api/admin/rule-parameters?key=stalled_after_days", nil, p.boss).list()
		require.Len(t, list, 1)
		require.Equal(t, 204, p.do("DELETE", "/api/admin/rule-parameters/"+list[0].(map[string]any)["id"].(string), nil, p.boss).Status)
		none := p.reviewerOverview(staff.reviewerSame)
		assert.Equal(t, map[string]any{"framing": float64(0), "awaiting_opinion": float64(0)}, none.data()["limits"])
		total, _, items := pendingOf(none)
		assert.Equal(t, 2, total, "a lista segue")
		for _, it := range items {
			assert.NotEqual(t, true, it["overLimit"])
		}
	})
}

// reviewerActivity lê o feed do departamento do Revisor.
func (p *projEnv) reviewerActivity(tok, query string) resp {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/department/activity"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r
}

func TestAtividadeDoDepartamentoDoRevisor(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	staff := p.newStaff()
	fin, jur := p.deptID("Financeiro"), p.deptID("Jurídico")
	colleague := p.reviewerOf("colega.revisor.fin@exemplo.com", fin)
	an := staff.analyst
	const secret = "JUSTIFICATIVA-INTERNA-DA-RECUSA"

	ready := func(sigcon string) *fundingProject {
		fp := p.submitted(sigcon)
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
		require.True(t, allApproved(p.approveAll(an, fp.id)))
		return fp
	}
	confirmed, refused, other := ready("9201/2026"), ready("9202/2026"), ready("9203/2026")
	denied, withdrawn := ready("9204/2026"), ready("9205/2026")
	p.create(p.maria) // rascunho: nada dele entra
	for _, fp := range []*fundingProject{confirmed, refused, denied, withdrawn} {
		require.Equal(t, 200, p.assign(an, fp.id, fin).Status)
	}
	require.Equal(t, 200, p.assign(an, other.id, jur).Status)
	require.Equal(t, 200, p.confirm(staff.reviewerSame, confirmed.id, 1).Status)
	require.Equal(t, 200, p.refuse(colleague, refused.id, 1, secret).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+denied.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+withdrawn.id+"/withdraw", map[string]any{"note": "Vamos refazer o projeto."}, p.maria).Status)

	r := p.reviewerActivity(staff.reviewerSame, "?limit=30")
	by := func(e map[string]any) map[string]any { return e["by"].(map[string]any) }
	data := func(e map[string]any) map[string]any { return e["data"].(map[string]any) }

	t.Run("o percurso de cada projeto, do mais novo ao mais antigo", func(t *testing.T) {
		assert.Equal(t, []string{"framing_confirmed", "framing_indicated"}, kindsOfEvents(eventsOf(r, confirmed.id)))
		assert.Equal(t, []string{"framing_refused", "framing_indicated"}, kindsOfEvents(eventsOf(r, refused.id)))
		assert.Equal(t, []string{"archived", "framing_indicated"}, kindsOfEvents(eventsOf(r, denied.id)))
		assert.Equal(t, []string{"archived", "framing_indicated"}, kindsOfEvents(eventsOf(r, withdrawn.id)))
		assert.Empty(t, eventsOf(r, other.id), "é do Jurídico")
		for _, e := range eventsOf(r, confirmed.id) {
			assert.EqualValues(t, 1, data(e)["round"])
			assert.NotEmpty(t, e["title"])
			assert.NotEmpty(t, e["code"])
		}
		assert.Equal(t, map[string]any{"reason": "denied"}, data(eventsOf(r, denied.id)[0]))
		assert.Equal(t, map[string]any{"reason": "withdrawn"}, data(eventsOf(r, withdrawn.id)[0]))
		// Ordem geral: do mais novo ao mais antigo, e cada marco com id próprio.
		ids, last := map[string]bool{}, "9999"
		for _, it := range r.list() {
			m := it.(map[string]any)
			assert.False(t, ids[m["id"].(string)], "id repetido")
			ids[m["id"].(string)] = true
			at := m["at"].(string)
			assert.LessOrEqual(t, at, last)
			last = at
		}
	})

	t.Run("quem agiu: a equipe com nome (colegas inclusive), o proponente sem", func(t *testing.T) {
		ev := eventsOf(r, confirmed.id)
		assert.Equal(t, "admin", by(ev[0])["type"])
		assert.NotEmpty(t, by(ev[0])["name"])
		assert.Equal(t, true, by(ev[0])["self"], "Regina confirmou")
		assert.NotEqual(t, true, by(ev[1])["self"], "o Analista indicou")
		assert.NotEmpty(t, by(ev[1])["name"])
		ev = eventsOf(r, refused.id)
		assert.NotEmpty(t, by(ev[0])["name"], "o colega aparece com nome")
		assert.NotEqual(t, true, by(ev[0])["self"])
		assert.Equal(t, "proponent", by(eventsOf(r, withdrawn.id)[0])["type"])
		assert.Nil(t, by(eventsOf(r, withdrawn.id)[0])["name"])
		assert.Equal(t, "admin", by(eventsOf(r, denied.id)[0])["type"])
		// Para o colega, a recusa dele é "self".
		mine := eventsOf(p.reviewerActivity(colleague, "?limit=30"), refused.id)
		assert.Equal(t, true, by(mine[0])["self"])
	})

	t.Run("projeto recusado não abre; os demais abrem", func(t *testing.T) {
		for _, e := range eventsOf(r, refused.id) {
			assert.Equal(t, false, e["linkable"], e["kind"])
		}
		for _, id := range []string{confirmed.id, denied.id, withdrawn.id} {
			for _, e := range eventsOf(r, id) {
				assert.Equal(t, true, e["linkable"], e["kind"])
			}
		}
	})

	t.Run("nada interno demais vaza", func(t *testing.T) {
		for _, q := range []string{"?limit=30", ""} {
			body := strings.ToLower(fmt.Sprint(p.reviewerActivity(staff.reviewerSame, q).Body))
			for _, banned := range []string{strings.ToLower(secret), "justification", "10.0.0.1", cpfFor("maria@exemplo.com"), "maria@exemplo.com",
				"review-item", "payload", "cpf", "telefone", "email"} {
				assert.NotContains(t, body, banned, "feed %q contém %q", q, banned)
			}
		}
		for _, it := range r.list() {
			assert.Contains(t, []string{"framing_indicated", "framing_confirmed", "framing_refused", "archived"}, it.(map[string]any)["kind"])
		}
	})

	t.Run("cada departamento só vê o seu", func(t *testing.T) {
		theirs := p.reviewerActivity(staff.reviewerOther, "?limit=30")
		require.Len(t, theirs.list(), 1)
		assert.Equal(t, other.id, theirs.list()[0].(map[string]any)["projectId"])
		assert.Equal(t, "framing_indicated", theirs.list()[0].(map[string]any)["kind"])
	})

	t.Run("limite", func(t *testing.T) {
		assert.Len(t, p.reviewerActivity(staff.reviewerSame, "?limit=3").list(), 3)
		assert.LessOrEqual(t, len(p.reviewerActivity(staff.reviewerSame, "").list()), 15, "padrão de 15")
		for _, bad := range []string{"?limit=0", "?limit=31", "?limit=abc"} {
			assert.Equal(t, 422, p.do("GET", "/api/admin/projects/department/activity"+bad, nil, staff.reviewerSame).Status, bad)
		}
	})

	t.Run("acesso", func(t *testing.T) {
		for name, tok := range map[string]string{"analista": an, "proponente": p.maria, "supervisor": staff.supervisor, "secretário": staff.secretary, "revisor sem departamento": staff.reviewerNone} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/department/activity", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/department/activity", nil, "").Status)
		assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/activity", nil, staff.reviewerSame).Status)
		assert.Empty(t, p.reviewerActivity(staff.superAdmin, "").list(), "o Super Admin não tem departamento")
	})
}

func (p *projEnv) reviewerMe(tok, query string) map[string]any {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/department/me"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r.data()
}

func decisions(confirmed, refused int) map[string]any {
	return map[string]any{"confirmed": float64(confirmed), "refused": float64(refused)}
}

func TestMinhasDecisoesDoRevisor(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	fin := p.deptID("Financeiro")
	colleague, colleagueID := p.newAdmin(p.boss, "colega.revisor.fin@exemplo.com", "department-reviewer")
	require.Equal(t, 200, p.do("PUT", "/api/admin/users/"+colleagueID, map[string]any{"departmentId": fin}, p.boss).Status)
	an := staff.analyst

	ready := func(sigcon string) *fundingProject {
		fp := p.submitted(sigcon)
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
		require.True(t, allApproved(p.approveAll(an, fp.id)))
		require.Equal(t, 200, p.assign(an, fp.id, fin).Status)
		return fp
	}
	a, b, c, pending := ready("9301/2026"), ready("9302/2026"), ready("9303/2026"), ready("9304/2026")
	require.Equal(t, 200, p.confirm(staff.reviewerSame, a.id, 1).Status)
	require.Equal(t, 200, p.refuse(staff.reviewerSame, b.id, 1, "Não é da nossa área.").Status)
	require.Equal(t, 200, p.confirm(colleague, c.id, 1).Status)
	_ = pending // pendente: ainda não é decisão de ninguém

	t.Run("só o que a própria pessoa decidiu", func(t *testing.T) {
		assert.Equal(t, map[string]any{"last7": decisions(1, 1), "last30": decisions(1, 1)}, p.reviewerMe(staff.reviewerSame, ""))
		assert.Equal(t, map[string]any{"last7": decisions(1, 0), "last30": decisions(1, 0)}, p.reviewerMe(colleague, ""))
		assert.Equal(t, map[string]any{"last7": decisions(0, 0), "last30": decisions(0, 0)}, p.reviewerMe(staff.reviewerOther, ""))
	})

	t.Run("a rota não aceita pedir os números de outra pessoa", func(t *testing.T) {
		own := p.reviewerMe(staff.reviewerSame, "")
		for _, q := range []string{"?admin=" + colleagueID, "?actor=" + colleagueID, "?userId=" + colleagueID, "?adminId=" + colleagueID} {
			assert.Equal(t, own, p.reviewerMe(staff.reviewerSame, q), q)
		}
		assert.Len(t, own, 2, "só duas janelas")
	})

	t.Run("janelas de 7 e 30 dias", func(t *testing.T) {
		setDecidedAt := func(projectID string, days int) {
			_, err := p.pool.Exec(context.Background(), "UPDATE project_framings SET decided_at = now() - make_interval(days => $2) WHERE project_id = $1 AND round = 1", projectID, days)
			require.NoError(t, err)
		}
		setDecidedAt(b.id, 20) // a recusa sai da janela de 7 dias, não da de 30
		assert.Equal(t, map[string]any{"last7": decisions(1, 0), "last30": decisions(1, 1)}, p.reviewerMe(staff.reviewerSame, ""))
		setDecidedAt(a.id, 40) // a confirmação sai das duas
		assert.Equal(t, map[string]any{"last7": decisions(0, 0), "last30": decisions(0, 1)}, p.reviewerMe(staff.reviewerSame, ""))
	})

	t.Run("quem não é Revisor não vê", func(t *testing.T) {
		for name, tok := range map[string]string{"analista": an, "proponente": p.maria, "supervisor": staff.supervisor, "secretário": staff.secretary, "revisor sem departamento": staff.reviewerNone} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/department/me", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/department/me", nil, "").Status)
		assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/me", nil, staff.reviewerSame).Status)
	})
}
