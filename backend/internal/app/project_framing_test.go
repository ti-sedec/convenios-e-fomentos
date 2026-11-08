package app_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
)

func (p *projEnv) assign(tok, id, dept string) resp {
	return p.do("PUT", "/api/admin/projects/"+id+"/department", map[string]any{"departmentId": dept}, tok)
}

func (p *projEnv) confirm(tok, id string, round int) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/framing/confirm", map[string]any{"round": round}, tok)
}

func (p *projEnv) refuse(tok, id string, round int, why string) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/framing/refuse", map[string]any{"round": round, "justification": why}, tok)
}

// reviewerOf cria um Revisor do Departamento no departamento dado.
func (p *projEnv) reviewerOf(email, dept string) string {
	p.t.Helper()
	tok, id := p.newAdmin(p.boss, email, "department-reviewer")
	require.Equal(p.t, 200, p.do("PUT", "/api/admin/users/"+id, map[string]any{"departmentId": dept}, p.boss).Status)
	return tok
}

// finalistic marca departamentos como área finalística (o seed de produção os cria sem a marca).
func (p *projEnv) finalistic(names ...string) {
	p.t.Helper()
	for _, n := range names {
		_, err := p.pool.Exec(context.Background(), "UPDATE departments SET is_finalistic = true WHERE name = $1", n)
		require.NoError(p.t, err)
	}
}

// framingEnv é um projeto enviado, assumido e com tudo aprovado, pronto para indicar o departamento.
type framingEnv struct {
	*projEnv
	staff    staffRoles
	fin, jur string
	fp       *fundingProject
}

func newFramingEnv(t *testing.T, sigcon string) *framingEnv {
	t.Helper()
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	f := &framingEnv{projEnv: p, staff: p.newStaff(), fin: p.deptID("Financeiro"), jur: p.deptID("Jurídico")}
	f.fp = p.submitted(sigcon)
	require.Equal(t, 200, p.startAnalysis(f.staff.analyst, f.fp.id).Status)
	require.True(t, allApproved(p.approveAll(f.staff.analyst, f.fp.id)))
	return f
}

func framingOf(r resp) map[string]any {
	f, _ := r.data()["framing"].(map[string]any)
	return f
}

func framingsOf(r resp) []any { l, _ := r.data()["framings"].([]any); return l }

func (p *projEnv) mailsBy(addr, subjectPart string) []mailer.Message {
	var out []mailer.Message
	for _, m := range p.mail.Msgs {
		if m.To == addr && strings.Contains(m.Subject, subjectPart) {
			out = append(out, m)
		}
	}
	return out
}

func TestIndicarDepartamento(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	staff := p.newStaff()
	fin := p.deptID("Financeiro")
	fp := p.submitted("7001/2026")

	// Antes de assumir e antes de aprovar tudo.
	assert.Equal(t, "analysis_not_started", p.assign(staff.analyst, fp.id, fin).errCode())
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	r := p.assign(staff.analyst, fp.id, fin)
	assert.Equal(t, "review_incomplete", r.errCode())
	details := r.Body["error"].(map[string]any)["details"].(map[string]any)
	assert.Greater(t, details["pending"], float64(0))
	require.True(t, allApproved(p.approveAll(staff.analyst, fp.id)))

	t.Run("departamento inválido", func(t *testing.T) {
		notFinalistic := p.mkDept("Obras") // o seed de produção não marca
		noReviewer := p.mkDept("Cultura")
		_, err := p.pool.Exec(context.Background(), "UPDATE departments SET is_finalistic = true WHERE id = $1", noReviewer)
		require.NoError(t, err)
		assert.Equal(t, "department_not_finalistic", p.assign(staff.analyst, fp.id, notFinalistic).errCode())
		assert.Equal(t, "department_without_reviewer", p.assign(staff.analyst, fp.id, noReviewer).errCode())
		assert.Equal(t, 422, p.assign(staff.analyst, fp.id, "00000000-0000-0000-0000-000000000000").Status, "inexistente")

		// O único Revisor bloqueado também deixa o departamento sem quem decida.
		_, err = p.pool.Exec(context.Background(), "UPDATE admins SET blocked = true WHERE email = 'm.revisor.jur@exemplo.com'")
		require.NoError(t, err)
		assert.Equal(t, "department_without_reviewer", p.assign(staff.analyst, fp.id, p.deptID("Jurídico")).errCode())
		_, err = p.pool.Exec(context.Background(), "UPDATE admins SET blocked = false WHERE email = 'm.revisor.jur@exemplo.com'")
		require.NoError(t, err)
	})

	p.mail.Msgs = nil
	r = p.assign(staff.analyst, fp.id, fin)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "in_analysis", r.data()["stage"], "a etapa continua em análise")
	f := framingOf(r)
	require.NotNil(t, f)
	assert.EqualValues(t, 1, f["round"])
	assert.Equal(t, "pending", f["decision"])
	assert.Equal(t, "Financeiro", f["department"].(map[string]any)["name"])
	assert.Len(t, framingsOf(r), 1)

	t.Run("a indicação não se repete", func(t *testing.T) {
		assert.Equal(t, "framing_pending", p.assign(staff.analyst, fp.id, p.deptID("Jurídico")).errCode())
	})
	t.Run("e-mail aos Supervisores, sem dados pessoais", func(t *testing.T) {
		mails := p.mailsBy("m.supervisor@exemplo.com", "encaminhado ao departamento Financeiro")
		require.Len(t, mails, 1)
		body := mails[0].Body
		assert.Contains(t, body, "Departamento indicado: Financeiro (indicação nº 1)")
		assert.Contains(t, body, p.project(staff.analyst, fp.id).data()["code"])
		assert.Contains(t, body, "Pavimentação da Rua A")
		assert.Contains(t, body, "/dashboard/projetos/"+fp.id)
		// O CNPJ da instituição (14 dígitos) está na lista permitida; CPF (11 dígitos ou formatado) e telefone não.
		assert.NotRegexp(t, `\b\d{3}\.\d{3}\.\d{3}-\d{2}\b|\b\d{11}\b`, body, "nada que pareça CPF")
		assert.NotRegexp(t, `\(\d{2}\)\s?\d{4,5}-?\d{4}|\b\d{10,11}\b`, body, "nenhum telefone")
		assert.Contains(t, body, "CNPJ: ")
		assert.NotContains(t, body, "@", "nenhum e-mail")
		assert.Empty(t, p.mailsBy("m.analista@exemplo.com", "encaminhado"), "só os Supervisores")
		assert.Empty(t, p.mailsBy("m.revisor.fin@exemplo.com", "encaminhado"))
	})
	t.Run("só o Analista indica", func(t *testing.T) {
		other := p.submitted("7002/2026")
		for name, tok := range map[string]string{"supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame, "proponente": p.maria} {
			assert.Equal(t, 403, p.assign(tok, other.id, fin).Status, name)
		}
		draft := p.completeProject("7003/2026")
		assert.Equal(t, 403, p.assign(staff.analyst, draft.id, fin).Status)
	})
}

func TestRevisorSoEnxergaOQueFoiIndicadoAoDepartamento(t *testing.T) {
	f := newFramingEnv(t, "7101/2026")
	path := "/api/admin/projects/" + f.fp.id

	// Antes da indicação, nenhum Revisor enxerga o projeto.
	assert.Equal(t, 404, f.status(f.staff.reviewerSame, path))
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)

	assert.Equal(t, 200, f.status(f.staff.reviewerSame, path), "o Revisor do departamento indicado")
	assert.Equal(t, 404, f.status(f.staff.reviewerOther, path), "o de outro departamento")
	assert.Equal(t, 403, f.status(f.staff.reviewerNone, path), "o sem departamento")
	ids := func(tok string) []string {
		r := f.do("GET", "/api/admin/projects?framing=pending", nil, tok)
		require.Equal(t, 200, r.Status, r.Body)
		var out []string
		for _, it := range r.list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	assert.Equal(t, []string{f.fp.id}, ids(f.staff.reviewerSame), "a fila do Revisor")
	assert.Empty(t, ids(f.staff.reviewerOther))
	assert.Equal(t, []string{f.fp.id}, ids(f.staff.analyst))
}

func TestConfirmarEnquadramento(t *testing.T) {
	f := newFramingEnv(t, "7201/2026")
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	f.mail.Msgs = nil

	assert.Equal(t, "framing_round_stale", f.confirm(f.staff.reviewerSame, f.fp.id, 99).errCode(), "rodada que não é a atual")
	assert.Equal(t, 403, f.confirm(f.staff.reviewerOther, f.fp.id, 1).Status, "outro departamento")
	assert.Equal(t, 403, f.confirm(f.staff.analyst, f.fp.id, 1).Status, "o Analista não decide")
	assert.Equal(t, 403, f.confirm(f.staff.supervisor, f.fp.id, 1).Status)
	assert.Equal(t, 403, f.confirm(f.maria, f.fp.id, 1).Status)

	r := f.confirm(f.staff.reviewerSame, f.fp.id, 1)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "awaiting_opinion", r.data()["stage"])
	assert.Equal(t, "confirmed", framingOf(r)["decision"])
	assert.NotEmpty(t, framingOf(r)["decidedBy"])
	assert.Empty(t, f.mail.Msgs, "confirmar não envia e-mail")

	// Depois de confirmado o departamento continua e a decisão não se repete.
	assert.Equal(t, 200, f.status(f.staff.reviewerSame, "/api/admin/projects/"+f.fp.id), "o Revisor segue vendo")
	assert.Equal(t, "framing_already_decided", f.confirm(f.staff.reviewerSame, f.fp.id, 1).errCode())
	assert.Equal(t, "framing_already_decided", f.refuse(f.staff.reviewerSame, f.fp.id, 1, "tarde demais").errCode())

	// A lista mostra a etapa, o enquadramento e o departamento só para a equipe.
	var staffRow map[string]any
	for _, it := range f.do("GET", "/api/admin/projects?stage=awaiting_opinion", nil, f.staff.analyst).list() {
		if it.(map[string]any)["id"] == f.fp.id {
			staffRow = it.(map[string]any)
		}
	}
	require.NotNil(t, staffRow)
	assert.Equal(t, "confirmed", staffRow["framingStatus"])
	assert.Equal(t, "Financeiro", staffRow["department"].(map[string]any)["name"])
	for _, it := range f.do("GET", "/api/admin/projects", nil, f.maria).list() {
		row := it.(map[string]any)
		assert.NotContains(t, row, "department", "o titular não recebe campos internos")
		assert.NotContains(t, row, "framingStatus")
		assert.NotContains(t, row, "analyst")
	}
}

func TestRecusarEnquadramento(t *testing.T) {
	f := newFramingEnv(t, "7301/2026")
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	f.mail.Msgs = nil

	assert.Equal(t, 422, f.refuse(f.staff.reviewerSame, f.fp.id, 1, "").Status, "a justificativa é obrigatória")
	assert.Equal(t, 422, f.refuse(f.staff.reviewerSame, f.fp.id, 1, "ab").Status)
	assert.Empty(t, framingsOf(f.project(f.staff.analyst, f.fp.id))[0].(map[string]any)["decidedBy"], "nada foi decidido")

	why := "O objeto é de competência do Jurídico, não do Financeiro. " + strings.Repeat("Detalhe. ", 80)
	r := f.refuse(f.staff.reviewerSame, f.fp.id, 1, why)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "in_analysis", r.data()["stage"], "volta ao Analista")
	assert.Nil(t, framingOf(r), "não há enquadramento em curso")
	hist := framingsOf(r)
	require.Len(t, hist, 1)
	h := hist[0].(map[string]any)
	assert.Equal(t, "refused", h["decision"])
	assert.Equal(t, "Financeiro", h["department"].(map[string]any)["name"])
	assert.Contains(t, h["justification"], "competência do Jurídico", "a justificativa inteira fica no sistema")
	assert.True(t, allApproved(r), "a conferência não é tocada: continua toda aprovada")

	// O projeto sai da fila do departamento.
	assert.Equal(t, 404, f.status(f.staff.reviewerSame, "/api/admin/projects/"+f.fp.id))

	// Só os Supervisores são avisados, com a justificativa limitada.
	mails := f.mailsBy("m.supervisor@exemplo.com", "enquadramento recusado")
	require.Len(t, mails, 1)
	assert.Contains(t, mails[0].Body, "Departamento recusado: Financeiro (indicação nº 1)")
	assert.Contains(t, mails[0].Body, "competência do Jurídico")
	assert.Less(t, len([]rune(mails[0].Body)), 1000, "a justificativa vai truncada")
	assert.Contains(t, mails[0].Body, "…")
	assert.Empty(t, f.mailsBy("m.analista@exemplo.com", ""), "o Analista não é avisado por e-mail")
	assert.Len(t, f.mail.Msgs, 1)

	// O Analista indica outro departamento direto, sem reaprovar nada.
	r = f.assign(f.staff.analyst, f.fp.id, f.jur)
	require.Equal(t, 200, r.Status, r.Body)
	assert.EqualValues(t, 2, framingOf(r)["round"])
	assert.Len(t, framingsOf(r), 2)
}

func TestRecusasSemLimiteDeRodadas(t *testing.T) {
	f := newFramingEnv(t, "7401/2026")
	for round := 1; round <= 5; round++ {
		dept, rev := f.fin, f.staff.reviewerSame
		if round%2 == 0 {
			dept, rev = f.jur, f.staff.reviewerOther
		}
		require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, dept).Status, "rodada %d", round)
		require.Equal(t, 200, f.refuse(rev, f.fp.id, round, fmt.Sprintf("Recusa número %d.", round)).Status)
	}
	r := f.assign(f.staff.analyst, f.fp.id, f.fin)
	require.Equal(t, 200, r.Status, r.Body)
	require.EqualValues(t, 6, framingOf(r)["round"])
	hist := framingsOf(r)
	require.Len(t, hist, 6)
	for i, h := range hist {
		m := h.(map[string]any)
		assert.EqualValues(t, i+1, m["round"], "da mais antiga à mais nova")
		want := "refused"
		if i == 5 {
			want = "pending"
		}
		assert.Equal(t, want, m["decision"])
	}
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, f.fp.id, 6).Status)

	var pending int
	require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_framings WHERE project_id = $1 AND decision = 'pending'", f.fp.id).Scan(&pending))
	assert.Equal(t, 0, pending)
}

func TestDecisoesSimultaneas(t *testing.T) {
	t.Run("dois Revisores do mesmo departamento: um vence", func(t *testing.T) {
		for name, second := range map[string]func(f *framingEnv, tok string) resp{
			"confirmam os dois": func(f *framingEnv, tok string) resp { return f.confirm(tok, f.fp.id, 1) },
			"um confirma, o outro recusa": func(f *framingEnv, tok string) resp {
				return f.refuse(tok, f.fp.id, 1, "Não é daqui.")
			},
		} {
			t.Run(name, func(t *testing.T) {
				f := newFramingEnv(t, "7501/2026")
				colleague := f.reviewerOf("m.revisor.fin2@exemplo.com", f.fin)
				require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)

				results := make([]resp, 2)
				var wg sync.WaitGroup
				for i, run := range []func() resp{
					func() resp { return f.confirm(f.staff.reviewerSame, f.fp.id, 1) },
					func() resp { return second(f, colleague) },
				} {
					wg.Add(1)
					go func() { defer wg.Done(); results[i] = run() }()
				}
				wg.Wait()
				var ok, conflict int
				for _, r := range results {
					switch {
					case r.Status == 200:
						ok++
					case r.Status == 409 && r.errCode() == "framing_already_decided":
						conflict++
					}
				}
				assert.Equal(t, 1, ok, "exatamente uma decisão vale: %v / %v", results[0].Body, results[1].Body)
				assert.Equal(t, 1, conflict)
			})
		}
	})
	t.Run("recusou e o colega confirma logo depois: 409, não 403", func(t *testing.T) {
		f := newFramingEnv(t, "7502/2026")
		colleague := f.reviewerOf("m.revisor.fin2@exemplo.com", f.fin)
		require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
		require.Equal(t, 200, f.refuse(f.staff.reviewerSame, f.fp.id, 1, "Não é daqui.").Status)
		r := f.confirm(colleague, f.fp.id, 1)
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, "framing_already_decided", r.errCode())
		// Quem é de outro departamento continua sem acesso.
		assert.Equal(t, 403, f.confirm(f.staff.reviewerOther, f.fp.id, 1).Status)
	})
	t.Run("o Analista indica duas vezes ao mesmo tempo: uma indicação", func(t *testing.T) {
		f := newFramingEnv(t, "7503/2026")
		var wg sync.WaitGroup
		codes := make(chan string, 2)
		for _, tok := range []string{f.staff.analyst, f.staff.analyst} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r := f.assign(tok, f.fp.id, f.fin)
				if r.Status == 200 {
					codes <- "ok"
				} else {
					codes <- r.errCode()
				}
			}()
		}
		wg.Wait()
		close(codes)
		got := map[string]int{}
		for c := range codes {
			got[c]++
		}
		assert.Equal(t, map[string]int{"ok": 1, "framing_pending": 1}, got)
	})
}

func TestEnquadramentoPendenteBloqueiaAConferencia(t *testing.T) {
	f := newFramingEnv(t, "7601/2026")
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	id := f.fp.id

	assert.Equal(t, "framing_pending", f.complement(f.staff.analyst, id, map[string]any{"items": []any{section("proposal", "")}}).errCode())
	assert.Equal(t, "framing_pending", f.do("POST", "/api/admin/projects/"+id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, f.staff.analyst).errCode())
	assert.Equal(t, "framing_pending", f.review(f.staff.analyst, id, "section", "proponent", false).errCode())
	assert.Equal(t, "framing_pending", f.review(f.staff.analyst, id, "section", "proponent", true).errCode())

	// Desistência e negação continuam valendo, e o Revisor do departamento é avisado do arquivamento.
	t.Run("negação pelo Secretário", func(t *testing.T) {
		f.mail.Msgs = nil
		require.Equal(t, 200, f.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Negado."}, f.staff.secretary).Status)
		assert.Contains(t, f.archivedMails(), "m.revisor.fin@exemplo.com")
		assert.Equal(t, "project_not_submitted", f.confirm(f.staff.reviewerSame, id, 1).errCode(), "arquivado não se decide")
	})
	t.Run("desistência do proponente", func(t *testing.T) {
		g := newFramingEnv(t, "7602/2026")
		require.Equal(t, 200, g.assign(g.staff.analyst, g.fp.id, g.fin).Status)
		assert.Equal(t, 200, g.do("POST", "/api/admin/projects/"+g.fp.id+"/withdraw", map[string]any{"note": "Desistimos."}, g.maria).Status)
	})
}

func TestDepoisDeConfirmadoSoRestaDesistirOuNegar(t *testing.T) {
	f := newFramingEnv(t, "7701/2026")
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, f.fp.id, 1).Status)
	id := f.fp.id

	for name, r := range map[string]resp{
		"devolver": f.complement(f.staff.analyst, id, map[string]any{"items": []any{section("proposal", "")}}),
		"desfazer": f.review(f.staff.analyst, id, "section", "proponent", false),
		"indicar":  f.assign(f.staff.analyst, id, f.jur),
		"assumir":  f.startAnalysis(f.staff.analyst, id),
	} {
		assert.Equal(t, 409, r.Status, name)
		assert.Equal(t, "project_stage_invalid", r.errCode(), name)
		assert.Contains(t, r.Body["error"].(map[string]any)["message"], "enquadramento já foi confirmado", name)
	}
	assert.Equal(t, 200, f.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Negado."}, f.staff.secretary).Status)
}

func TestFiltrosDaListagemDaEquipe(t *testing.T) {
	f := newFramingEnv(t, "7801/2026")
	waiting := f.submitted("7802/2026") // aguardando análise de checklist
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)

	ids := func(query string) []string {
		r := f.do("GET", "/api/admin/projects?"+query, nil, f.staff.analyst)
		require.Equal(t, 200, r.Status, r.Body)
		var out []string
		for _, it := range r.list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}
	assert.ElementsMatch(t, []string{waiting.id}, ids("stage=awaiting_checklist"))
	assert.ElementsMatch(t, []string{f.fp.id}, ids("stage=in_analysis"))
	assert.ElementsMatch(t, []string{waiting.id, f.fp.id}, ids("stage=awaiting_checklist,in_analysis"), "várias etapas")
	assert.ElementsMatch(t, []string{f.fp.id}, ids("framing=pending"))
	assert.ElementsMatch(t, []string{waiting.id}, ids("framing=none&stage=awaiting_checklist,in_analysis"))
	assert.ElementsMatch(t, []string{f.fp.id}, ids("department="+f.fin))
	assert.Empty(t, ids("department="+f.jur))
	assert.ElementsMatch(t, []string{f.fp.id}, ids("analyst="+f.adminID("m.analista@exemplo.com")))

	for _, bad := range []string{"stage=xyz", "framing=talvez", "department=nao-e-uuid", "analyst=123"} {
		assert.Equal(t, 422, f.do("GET", "/api/admin/projects?"+bad, nil, f.staff.analyst).Status, bad)
	}
}

func TestDepartamentoComProjetoEContadores(t *testing.T) {
	f := newFramingEnv(t, "7901/2026")
	dept := func(id string) map[string]any {
		return f.do("GET", "/api/admin/departments/"+id, nil, f.boss).data()
	}
	assert.EqualValues(t, 1, dept(f.fin)["reviewerCount"])
	assert.EqualValues(t, 0, dept(f.fin)["projectCount"])
	f.reviewerOf("m.revisor.fin2@exemplo.com", f.fin)
	assert.EqualValues(t, 2, dept(f.fin)["reviewerCount"])

	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	assert.EqualValues(t, 1, dept(f.fin)["projectCount"])

	// Com projeto vinculado não se exclui.
	r := f.do("DELETE", "/api/admin/departments/"+f.fin, nil, f.boss)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "department_in_use", r.errCode())
	assert.EqualValues(t, 1, r.Body["error"].(map[string]any)["details"].(map[string]any)["projectCount"])

	// Renomear e desmarcar não mexem na rodada aberta nem no histórico.
	require.Equal(t, 200, f.do("PUT", "/api/admin/departments/"+f.fin, map[string]any{"name": "Finanças", "isFinalistic": false}, f.boss).Status)
	v := f.project(f.staff.analyst, f.fp.id)
	assert.Equal(t, "Financeiro", framingOf(v)["department"].(map[string]any)["name"], "o nome da rodada é o da data da indicação")
	assert.Equal(t, 200, f.confirm(f.staff.reviewerSame, f.fp.id, 1).Status, "desmarcar não afeta rodada aberta")
}

func TestEnquadramentoFicaNaAuditoriaSemTextoLivre(t *testing.T) {
	f := newFramingEnv(t, "7951/2026")
	require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
	require.Equal(t, 200, f.refuse(f.staff.reviewerSame, f.fp.id, 1, "Justificativa que não deve ir para a auditoria.").Status)
	for _, action := range []string{"project.assign-department", "project.framing-refuse"} {
		var payload string
		require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT payload::text FROM audit_logs WHERE action = $1 AND entity_id = $2", action, f.fp.id).Scan(&payload), action)
		assert.NotContains(t, payload, "Justificativa", action)
	}
}
