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

// queueOverview lê o resumo da fila do Analista.
func (p *projEnv) queueOverview(tok, query string) resp {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/queue/overview"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r
}

// block devolve o total e os ids (na ordem) de um bloco da visão geral.
func block(r resp, name string) (total int, ids []string, items []map[string]any) {
	b := r.data()["blocks"].(map[string]any)[name].(map[string]any)
	for _, it := range b["items"].([]any) {
		m := it.(map[string]any)
		ids, items = append(ids, m["projectId"].(string)), append(items, m)
	}
	return int(b["total"].(float64)), ids, items
}

// returnAndResubmit devolve o projeto à correção e o proponente o reenvia.
func (p *projEnv) returnAndResubmit(fp *fundingProject, analyst, sigcon string) {
	p.t.Helper()
	require.Equal(p.t, 200, p.complement(analyst, fp.id, map[string]any{"message": "Corrija o título.", "items": []any{section("proposal", "Título.")}}).Status)
	pb := proposalBody(fp.current(p.maria))
	pb["title"], pb["objectNature"], pb["sigconNumber"] = "Pavimentação da Rua B", "services", sigcon
	require.Equal(p.t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).Status)
	require.Equal(p.t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria).Status)
}

func TestVisaoGeralDaFilaDoAnalista(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "outro.analista@exemplo.com", "analyst")
	fin := p.deptID("Financeiro")
	an := staff.analyst
	assume := func(fp *fundingProject) *fundingProject {
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
		return fp
	}
	p.create(p.maria) // rascunho: nunca entra

	entryOld := p.submitted("8501/2026")         // fila de entrada, há 3 dias (limite 2)
	entryNew := p.submitted("8502/2026")         // fila de entrada, agora
	stalled := assume(p.submitted("8503/2026"))  // em análise há 8 dias (limite 7)
	due40 := assume(p.submitted("8504/2026"))    // complementação que vence em 40 horas
	due60 := assume(p.submitted("8505/2026"))    // a que vence em 60 horas
	resub := assume(p.submitted("8506/2026"))    // reenviado, sem conferência depois
	reviewed := assume(p.submitted("8507/2026")) // reenviado e já conferido
	refused := assume(p.submitted("8508/2026"))  // enquadramento recusado, sem nova indicação
	again := assume(p.submitted("8509/2026"))    // recusado e indicado de novo (no Revisor há 6 dias)
	archived := assume(p.submitted("8510/2026")) // arquivado pelo Analista

	p.age(entryOld.id, 3)
	p.age(stalled.id, 10, 8)
	for _, fp := range []*fundingProject{due40, due60} {
		require.Equal(t, 200, p.complement(an, fp.id, map[string]any{"message": "Corrija.", "items": []any{section("proposal", "Título.")}}).Status)
	}
	p.setDue(due40.id, time.Now().Add(40*time.Hour))
	p.setDue(due60.id, time.Now().Add(60*time.Hour))
	p.returnAndResubmit(resub, an, "8506/2026")
	p.returnAndResubmit(reviewed, an, "8507/2026")
	require.Equal(t, 200, p.review(an, reviewed.id, "section", "institution", true).Status)
	for _, fp := range []*fundingProject{refused, again} {
		require.True(t, allApproved(p.approveAll(an, fp.id)))
		require.Equal(t, 200, p.assign(an, fp.id, fin).Status)
		require.Equal(t, 200, p.refuse(staff.reviewerSame, fp.id, 1, "Não é da nossa área.").Status)
	}
	require.Equal(t, 200, p.assign(an, again.id, fin).Status)
	p.age(again.id, 20, 19, 18, 17, 6)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+archived.id+"/archive", map[string]any{"reason": "other", "note": "Arquivado pela equipe."}, an).Status)

	r := p.queueOverview(an, "")
	counts := r.data()["counts"].(map[string]any)
	assert.Equal(t, map[string]any{"awaiting_checklist": float64(2), "in_analysis": float64(4), "framing_pending": float64(1),
		"awaiting_proponent": float64(2), "awaiting_opinion": float64(0), "archived_30d": float64(1)}, counts)
	assert.Equal(t, map[string]any{"awaiting_checklist": float64(2), "in_analysis": float64(7), "framing": float64(5), "awaiting_opinion": float64(15)}, r.data()["limits"])

	t.Run("aguardando assumir: a fila de entrada, a mais antiga primeiro", func(t *testing.T) {
		total, ids, items := block(r, "entry")
		assert.Equal(t, 2, total)
		assert.Equal(t, []string{entryOld.id, entryNew.id}, ids)
		assert.Equal(t, true, items[0]["overLimit"])
		assert.NotEqual(t, true, items[1]["overLimit"])
		assert.NotEmpty(t, items[0]["title"])
		assert.NotEmpty(t, items[0]["code"])
		assert.Nil(t, items[0]["analyst"])
	})
	t.Run("parados: o mais além do limite da própria etapa primeiro", func(t *testing.T) {
		total, ids, items := block(r, "stalled")
		assert.Equal(t, 3, total)
		// 3 dias para 2 (1,5), 6 dias para 5 (1,2), 8 dias para 7 (1,14).
		assert.Equal(t, []string{entryOld.id, again.id, stalled.id}, ids)
		assert.Equal(t, "framing", items[1]["stage"])
		assert.Equal(t, "department", items[1]["holder"])
		assert.EqualValues(t, 5, items[1]["limitDays"])
		assert.Equal(t, "team", items[0]["holder"])
		assert.Equal(t, "analyst", items[2]["holder"])
		assert.EqualValues(t, 7, items[2]["limitDays"])
		assert.NotNil(t, items[2]["analyst"])
		for _, it := range items {
			assert.NotEqual(t, "awaiting_proponent", it["stage"], "o prazo do proponente não conta como parado")
		}
	})
	t.Run("prazos a vencer em 48 horas", func(t *testing.T) {
		total, ids, items := block(r, "deadlines")
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{due40.id}, ids)
		assert.NotEmpty(t, items[0]["dueAt"])
		assert.NotEqual(t, true, items[0]["overdue"])
	})
	t.Run("reenviados sem conferência depois do reenvio", func(t *testing.T) {
		total, ids, _ := block(r, "resubmitted")
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{resub.id}, ids)
	})
	t.Run("recusados pelo Revisor, até indicar de novo", func(t *testing.T) {
		total, ids, items := block(r, "refused")
		assert.Equal(t, 1, total)
		assert.Equal(t, []string{refused.id}, ids)
		assert.Equal(t, "Financeiro", items[0]["department"])
		assert.NotEmpty(t, items[0]["refusedAt"])
		for _, name := range []string{"stalled", "entry", "resubmitted", "deadlines"} {
			_, _, others := block(r, name)
			for _, it := range others {
				assert.Nil(t, it["department"], "a rodada recusada de antes não aparece em %s", name)
			}
		}
	})

	t.Run("só os meus", func(t *testing.T) {
		mine := p.queueOverview(an, "?scope=mine")
		c := mine.data()["counts"].(map[string]any)
		assert.EqualValues(t, 0, c["awaiting_checklist"], "ninguém é dono antes de assumir")
		assert.EqualValues(t, 4, c["in_analysis"])
		total, ids, _ := block(mine, "entry")
		assert.Equal(t, 2, total, "a fila de entrada ignora o filtro")
		assert.Equal(t, []string{entryOld.id, entryNew.id}, ids)
		_, stalledIDs, _ := block(mine, "stalled")
		assert.Equal(t, []string{again.id, stalled.id}, stalledIDs, "o que está na entrada não é de ninguém")

		// Outro Analista não assumiu nada: só vê a fila de entrada.
		theirs := p.queueOverview(other, "?scope=mine")
		for k, v := range theirs.data()["counts"].(map[string]any) {
			assert.EqualValues(t, 0, v, k)
		}
		for _, name := range []string{"refused", "resubmitted", "stalled", "deadlines"} {
			total, _, _ := block(theirs, name)
			assert.Equal(t, 0, total, name)
		}
		total, _, _ = block(theirs, "entry")
		assert.Equal(t, 2, total)
		// Mas o escopo "equipe" mostra tudo para quem for.
		assert.Equal(t, counts, p.queueOverview(other, "").data()["counts"])
	})

	t.Run("prazo já vencido continua listado, marcado", func(t *testing.T) {
		p.setDue(due40.id, time.Now().Add(-time.Hour))
		_, ids, items := block(p.queueOverview(an, ""), "deadlines")
		assert.Equal(t, []string{due40.id}, ids)
		assert.Equal(t, true, items[0]["overdue"])
	})

	t.Run("nada pessoal do proponente", func(t *testing.T) {
		body := strings.ToLower(fmt.Sprint(r.Body))
		for _, banned := range []string{cpfFor("maria@exemplo.com"), "maria@exemplo.com", "phone", "cpf", "email"} {
			assert.NotContains(t, body, banned)
		}
	})

	t.Run("quem não é Analista não vê", func(t *testing.T) {
		for name, tok := range map[string]string{"proponente": p.maria, "supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/overview", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/queue/overview", nil, "").Status)
		assert.Equal(t, 422, p.do("GET", "/api/admin/projects/queue/overview?scope=minhas", nil, an).Status)
	})

	t.Run("sem regra vigente, nada é marcado como parado", func(t *testing.T) {
		list := p.do("GET", "/api/admin/rule-parameters?key=stalled_after_days", nil, p.boss).list()
		require.Len(t, list, 1)
		require.Equal(t, 204, p.do("DELETE", "/api/admin/rule-parameters/"+list[0].(map[string]any)["id"].(string), nil, p.boss).Status)
		none := p.queueOverview(an, "")
		assert.Equal(t, map[string]any{"awaiting_checklist": float64(0), "in_analysis": float64(0), "framing": float64(0), "awaiting_opinion": float64(0)}, none.data()["limits"])
		total, _, _ := block(none, "stalled")
		assert.Equal(t, 0, total)
		total, _, _ = block(none, "entry")
		assert.Equal(t, 2, total, "os demais blocos seguem")
	})
}

func TestVisaoGeralFilaProntosParaIndicar(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "outro.analista@exemplo.com", "analyst")
	fin := p.deptID("Financeiro")
	an := staff.analyst
	assume := func(fp *fundingProject) *fundingProject {
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
		return fp
	}

	ready := assume(p.submitted("8601/2026"))    // tudo aprovado, sem departamento
	pending := assume(p.submitted("8602/2026"))  // ainda com itens por conferir
	rejected := assume(p.submitted("8603/2026")) // aprovado e depois reprovado um item
	refused := assume(p.submitted("8604/2026"))  // recusado pelo Revisor: volta pronto para indicar de novo
	framing := assume(p.submitted("8605/2026"))  // com o Revisor: já tem departamento
	for _, fp := range []*fundingProject{ready, rejected, refused, framing} {
		require.True(t, allApproved(p.approveAll(an, fp.id)))
	}
	require.Equal(t, 200, p.reject(an, rejected.id, "section", "proposal", "Corrija o título do projeto.").Status)
	for _, fp := range []*fundingProject{refused, framing} {
		require.Equal(t, 200, p.assign(an, fp.id, fin).Status)
	}
	require.Equal(t, 200, p.refuse(staff.reviewerSame, refused.id, 1, "Não é da nossa área.").Status)
	p.age(ready.id, 9, 8)

	r := p.queueOverview(an, "")
	total, ids, items := block(r, "ready")
	assert.Equal(t, 2, total)
	assert.ElementsMatch(t, []string{ready.id, refused.id}, ids)
	assert.Equal(t, ready.id, ids[0], "o mais antigo primeiro")
	assert.NotEmpty(t, items[0]["title"])
	assert.NotEqual(t, true, r.data()["blocks"].(map[string]any)["ready"].(map[string]any)["capped"])
	assert.NotContains(t, ids, pending.id)
	assert.NotContains(t, ids, rejected.id, "um item reprovado impede")
	assert.NotContains(t, ids, framing.id, "já tem departamento")

	// Reprovar um item tira o projeto; aprovar de novo o devolve.
	require.Equal(t, 200, p.reject(an, ready.id, "section", "institution", "Corrija os dados da instituição.").Status)
	_, ids, _ = block(p.queueOverview(an, ""), "ready")
	assert.Equal(t, []string{refused.id}, ids)
	require.Equal(t, 200, p.review(an, ready.id, "section", "institution", true).Status)
	_, ids, _ = block(p.queueOverview(an, ""), "ready")
	assert.ElementsMatch(t, []string{ready.id, refused.id}, ids)

	// Indicar o departamento tira o projeto do bloco.
	require.Equal(t, 200, p.assign(an, ready.id, fin).Status)
	_, ids, _ = block(p.queueOverview(an, ""), "ready")
	assert.Equal(t, []string{refused.id}, ids)

	// Só os meus: outro Analista não assumiu nenhum.
	total, _, _ = block(p.queueOverview(other, "?scope=mine"), "ready")
	assert.Equal(t, 0, total)
	total, _, _ = block(p.queueOverview(other, ""), "ready")
	assert.Equal(t, 1, total)

	t.Run("limite de candidatos", func(t *testing.T) {
		// Com o limite em 2, o terceiro candidato (o mais novo) nem é examinado.
		p.app.Projects.SetReadyCandidates(2)
		for i := 0; i < 2; i++ {
			assume(p.submitted(fmt.Sprintf("87%02d/2026", i)))
		}
		p.age(refused.id, 40, 39, 38, 37)
		blk := p.queueOverview(an, "").data()["blocks"].(map[string]any)["ready"].(map[string]any)
		assert.Equal(t, true, blk["capped"], "havia mais candidatos que o limite")
		assert.EqualValues(t, 1, blk["total"], "o mais antigo está entre os examinados")
	})
}

// queueActivity lê o feed da equipe.
func (p *projEnv) queueActivity(tok, query string) resp {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/queue/activity"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r
}

// eventsOf devolve os marcos de um projeto, do mais novo ao mais antigo.
func eventsOf(r resp, projectID string) (events []map[string]any) {
	for _, it := range r.list() {
		if m := it.(map[string]any); m["projectId"] == projectID {
			events = append(events, m)
		}
	}
	return events
}

func kindsOfEvents(events []map[string]any) (kinds []string) {
	for _, e := range events {
		kinds = append(kinds, e["kind"].(string))
	}
	return kinds
}

func TestAtividadeDaFilaDaEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "outro.analista@exemplo.com", "analyst")
	fin := p.deptID("Financeiro")
	an := staff.analyst
	const secret = "JUSTIFICATIVA-INTERNA-DA-RECUSA"

	main := p.submitted("8701/2026")
	require.Equal(t, 200, p.startAnalysis(an, main.id).Status)
	p.returnAndResubmit(main, an, "8701/2026")
	require.True(t, allApproved(p.approveAll(an, main.id)))
	require.Equal(t, 200, p.assign(an, main.id, fin).Status)
	require.Equal(t, 200, p.refuse(staff.reviewerSame, main.id, 1, secret).Status)
	require.Equal(t, 200, p.assign(an, main.id, fin).Status)
	require.Equal(t, 200, p.confirm(staff.reviewerSame, main.id, 2).Status)

	archivedByTeam := p.submitted("8702/2026")
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+archivedByTeam.id+"/archive", map[string]any{"reason": "other", "note": "Arquivado pela equipe."}, an).Status)
	withdrawn := p.submitted("8703/2026")
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+withdrawn.id+"/withdraw", map[string]any{"note": "Vamos refazer o projeto."}, p.maria).Status)
	expired := p.submitted("8704/2026")
	require.Equal(t, 200, p.complement(an, expired.id, map[string]any{"message": "Corrija.", "items": []any{section("proposal", "Título.")}}).Status)
	p.setDue(expired.id, time.Now().Add(-time.Hour))
	n, err := p.app.Projects.ExpireComplements(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	p.create(p.maria) // rascunho: nada dele entra

	r := p.queueActivity(an, "?limit=30")

	t.Run("o percurso do projeto, do mais novo ao mais antigo, com o interno", func(t *testing.T) {
		ev := eventsOf(r, main.id)
		assert.Equal(t, []string{"framing_confirmed", "department_assigned", "framing_refused", "department_assigned", "resubmitted",
			"complement_requested", "analysis_started", "submitted"}, kindsOfEvents(ev))
		for _, i := range []int{0, 1, 2, 3} {
			assert.Equal(t, "Financeiro", ev[i]["data"].(map[string]any)["department"], ev[i]["kind"])
		}
		assert.EqualValues(t, 1, ev[5]["data"].(map[string]any)["items"])
		assert.EqualValues(t, 2, ev[4]["data"].(map[string]any)["version"])
		assert.NotEmpty(t, ev[5]["data"].(map[string]any)["dueAt"])
		assert.NotEmpty(t, ev[0]["title"])
		assert.NotEmpty(t, ev[0]["code"])
	})

	t.Run("quem agiu: a equipe com nome, o proponente sem", func(t *testing.T) {
		ev := eventsOf(r, main.id)
		by := func(i int) map[string]any { return ev[i]["by"].(map[string]any) }
		assert.Equal(t, "admin", by(0)["type"], "o Revisor confirmou")
		assert.NotEmpty(t, by(0)["name"])
		assert.NotEqual(t, true, by(0)["self"], "não foi o Analista que consulta")
		assert.Equal(t, "admin", by(1)["type"])
		assert.Equal(t, true, by(1)["self"], "o Analista que consulta indicou")
		assert.Equal(t, "proponent", by(4)["type"], "o reenvio é do proponente")
		assert.Nil(t, by(4)["name"])
		assert.Equal(t, "proponent", by(7)["type"], "o envio")
		assert.Nil(t, by(7)["name"])
		assert.NotEqual(t, true, by(7)["self"])
	})

	t.Run("arquivamentos: pela equipe, pelo proponente e pelo sistema", func(t *testing.T) {
		ev := eventsOf(r, archivedByTeam.id)
		require.Equal(t, []string{"archived", "submitted"}, kindsOfEvents(ev))
		assert.Equal(t, map[string]any{"reason": "other"}, ev[0]["data"])
		assert.Equal(t, "admin", ev[0]["by"].(map[string]any)["type"])
		ev = eventsOf(r, withdrawn.id)
		assert.Equal(t, map[string]any{"reason": "withdrawn"}, ev[0]["data"])
		assert.Equal(t, "proponent", ev[0]["by"].(map[string]any)["type"])
		ev = eventsOf(r, expired.id)
		assert.Equal(t, map[string]any{"reason": "complement_deadline"}, ev[0]["data"])
		assert.Equal(t, "system", ev[0]["by"].(map[string]any)["type"])
		assert.Nil(t, ev[0]["by"].(map[string]any)["name"])
	})

	t.Run("nada interno demais vaza", func(t *testing.T) {
		for _, q := range []string{"?limit=30", "?scope=mine&limit=30", ""} {
			body := strings.ToLower(fmt.Sprint(p.queueActivity(an, q).Body))
			for _, banned := range []string{strings.ToLower(secret), "justification", "10.0.0.1", cpfFor("maria@exemplo.com"), "maria@exemplo.com",
				"review-item", "payload", "cpf", "telefone"} {
				assert.NotContains(t, body, banned, "feed %q contém %q", q, banned)
			}
		}
		for _, it := range r.list() {
			assert.NotContains(t, []string{"review_item", ""}, it.(map[string]any)["kind"])
		}
	})

	t.Run("só os meus e o limite", func(t *testing.T) {
		// O Analista que assumiu vê os do projeto principal; outro Analista, que não assumiu nada, não vê nenhum com "só os meus".
		assert.NotEmpty(t, eventsOf(p.queueActivity(an, "?scope=mine&limit=30"), main.id))
		assert.Empty(t, p.queueActivity(other, "?scope=mine&limit=30").list())
		assert.NotEmpty(t, p.queueActivity(other, "?limit=30").list(), "a equipe inteira vê tudo")
		assert.Len(t, p.queueActivity(an, "?limit=3").list(), 3)
		assert.LessOrEqual(t, len(p.queueActivity(an, "").list()), 15, "padrão de 15")
	})

	t.Run("quem não é Analista não vê", func(t *testing.T) {
		for name, tok := range map[string]string{"proponente": p.maria, "supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/activity", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/queue/activity", nil, "").Status)
		for _, bad := range []string{"?limit=0", "?limit=31", "?limit=abc", "?scope=minhas"} {
			assert.Equal(t, 422, p.do("GET", "/api/admin/projects/queue/activity"+bad, nil, an).Status, bad)
		}
	})
}

// backdateAudit recua um evento de auditoria (o log é imutável: só o teste desliga a trava, e a religa).
func (p *projEnv) backdateAudit(projectID, action string, days int) {
	p.t.Helper()
	ctx := context.Background()
	_, err := p.pool.Exec(ctx, "ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_no_change")
	require.NoError(p.t, err)
	_, err = p.pool.Exec(ctx, "UPDATE audit_logs SET created_at = now() - make_interval(days => $3) WHERE entity_id = $1 AND action = $2", projectID, action, days)
	require.NoError(p.t, err)
	_, err = p.pool.Exec(ctx, "ALTER TABLE audit_logs ENABLE TRIGGER audit_logs_no_change")
	require.NoError(p.t, err)
}

func (p *projEnv) queueMe(tok, query string) map[string]any {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/queue/me"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r.data()
}

func counts(assumed, complements, indicated, archived int) map[string]any {
	return map[string]any{"assumed": float64(assumed), "complements": float64(complements), "indicated": float64(indicated), "archived": float64(archived)}
}

func TestMeuResumoDoAnalista(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	other, otherID := p.newAdmin(p.boss, "outro.analista@exemplo.com", "analyst")
	fin := p.deptID("Financeiro")
	an := staff.analyst

	a1, a2, a3 := p.submitted("8801/2026"), p.submitted("8802/2026"), p.submitted("8803/2026")
	for _, fp := range []*fundingProject{a1, a2, a3} {
		require.Equal(t, 200, p.startAnalysis(an, fp.id).Status)
	}
	require.Equal(t, 200, p.startAnalysis(an, a1.id).Status, "assumir de novo não conta outra vez")
	require.Equal(t, 200, p.complement(an, a1.id, map[string]any{"message": "Corrija.", "items": []any{section("proposal", "Título.")}}).Status)
	require.True(t, allApproved(p.approveAll(an, a2.id)))
	require.Equal(t, 200, p.assign(an, a2.id, fin).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+a3.id+"/archive", map[string]any{"reason": "other", "note": "Arquivado pela equipe."}, an).Status)

	// Outro Analista: assumiu um e arquivou outro. A desistência de um terceiro, feita pelo proponente, não é de nenhum dos dois.
	b1, b2, w := p.submitted("8804/2026"), p.submitted("8805/2026"), p.submitted("8806/2026")
	require.Equal(t, 200, p.startAnalysis(other, b1.id).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+b2.id+"/archive", map[string]any{"reason": "other", "note": "Arquivado pela equipe."}, other).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+w.id+"/withdraw", map[string]any{"note": "Vamos refazer o projeto."}, p.maria).Status)

	t.Run("só o que a própria pessoa fez", func(t *testing.T) {
		mine := p.queueMe(an, "")
		assert.Equal(t, map[string]any{"last7": counts(3, 1, 1, 1), "last30": counts(3, 1, 1, 1)}, mine)
		assert.Equal(t, map[string]any{"last7": counts(1, 0, 0, 1), "last30": counts(1, 0, 0, 1)}, p.queueMe(other, ""))
	})

	t.Run("a rota não aceita pedir os números de outra pessoa", func(t *testing.T) {
		own := p.queueMe(an, "")
		for _, q := range []string{"?admin=" + otherID, "?actor=" + otherID, "?userId=" + otherID, "?scope=mine&adminId=" + otherID} {
			assert.Equal(t, own, p.queueMe(an, q), q)
		}
		// Só duas janelas e quatro contagens: nada de nome, projeto nem lista.
		for key, v := range own {
			assert.Contains(t, []string{"last7", "last30"}, key)
			assert.Len(t, v, 4)
		}
	})

	t.Run("janelas de 7 e de 30 dias", func(t *testing.T) {
		p.backdateAudit(a1.id, "project.start-analysis", 20) // fora dos 7, dentro dos 30
		p.backdateAudit(a2.id, "project.start-analysis", 40) // fora dos dois
		p.backdateAudit(a1.id, "project.complement-request", 10)
		assert.Equal(t, map[string]any{"last7": counts(1, 0, 1, 1), "last30": counts(2, 1, 1, 1)}, p.queueMe(an, ""))
	})

	t.Run("quem não é Analista não vê", func(t *testing.T) {
		for name, tok := range map[string]string{"proponente": p.maria, "supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame} {
			assert.Equal(t, 403, p.do("GET", "/api/admin/projects/queue/me", nil, tok).Status, name)
		}
		assert.Equal(t, 401, p.do("GET", "/api/admin/projects/queue/me", nil, "").Status)
	})

	t.Run("arquivamento pelo sistema e desistência não contam", func(t *testing.T) {
		exp := p.submitted("8807/2026")
		require.Equal(t, 200, p.complement(an, exp.id, map[string]any{"message": "Corrija.", "items": []any{section("proposal", "Título.")}}).Status)
		p.setDue(exp.id, time.Now().Add(-time.Hour))
		n, err := p.app.Projects.ExpireComplements(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, n)
		got := p.queueMe(an, "")["last7"].(map[string]any)
		assert.EqualValues(t, 1, got["archived"], "continua só o arquivamento feito pela equipe")
		assert.EqualValues(t, 1, got["complements"], "mais o pedido novo")
	})
}
