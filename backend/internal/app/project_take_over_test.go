package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (p *projEnv) takeOver(tok, id string) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/take-over", nil, tok)
}

func analystOf(r resp) map[string]any {
	a, _ := r.data()["analyst"].(map[string]any)
	return a
}

func (p *projEnv) countAudit(action, id string) int {
	p.t.Helper()
	var n int
	require.NoError(p.t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2", action, id).Scan(&n))
	return n
}

// resubmitProposal faz o proponente corrigir a proposta devolvida e reenviar o projeto.
func (p *projEnv) resubmitProposal(fp *fundingProject, sigcon string) {
	p.t.Helper()
	pb := proposalBody(fp.current(p.maria))
	pb["objectNature"], pb["sigconNumber"] = "services", sigcon
	require.Equal(p.t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).Status)
	require.Equal(p.t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria).Status)
}

// Só o analista responsável continua a análise; os outros veem o projeto e precisam assumi-lo.
func TestSoOResponsavelContinuaAAnalise(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9501/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.Equal(t, 200, p.review(staff.analyst, fp.id, "section", "institution", true).Status)
	before := p.project(staff.analyst, fp.id).data()["version"]

	t.Run("quem não é o responsável não age", func(t *testing.T) {
		const code = "not_responsible_analyst"
		assert.Equal(t, code, p.review(other, fp.id, "section", "proponent", true).errCode())
		assert.Equal(t, code, p.complement(other, fp.id, map[string]any{"items": []any{section("proposal", "")}}).errCode())
		assert.Equal(t, code, p.do("POST", "/api/admin/projects/"+fp.id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, other).errCode())
		assert.Equal(t, code, p.assign(other, fp.id, p.deptID("Financeiro")).errCode())
		assert.Equal(t, code, p.startAnalysis(other, fp.id).errCode())
		v := p.project(staff.analyst, fp.id)
		assert.Equal(t, before, v.data()["version"], "nada mudou")
		assert.Equal(t, "in_analysis", v.data()["stage"])
		assert.Equal(t, "approved", reviewItems(v)["section:institution"]["status"])
	})
	t.Run("mas vê o projeto, a conferência e abre os arquivos", func(t *testing.T) {
		v := p.project(other, fp.id)
		require.Equal(t, 200, v.Status)
		assert.NotNil(t, v.data()["review"])
		a := analystOf(v)
		assert.Equal(t, p.adminID("m.analista@exemplo.com"), a["id"])
		assert.Equal(t, false, a["isMe"])
		file := fileIDOf(reviewItems(v)["document:requerimento_apoio"])
		assert.Equal(t, 204, p.viewFile(other, fp.id, file))
	})
	t.Run("na lista, \"meus projetos\" filtra pelo responsável", func(t *testing.T) {
		ids := func(tok string) []string {
			var out []string
			for _, x := range p.do("GET", "/api/admin/projects?analyst=me", nil, tok).Body["data"].([]any) {
				out = append(out, x.(map[string]any)["id"].(string))
			}
			return out
		}
		assert.Contains(t, ids(staff.analyst), fp.id)
		assert.NotContains(t, ids(other), fp.id)
		r := p.do("GET", "/api/admin/projects?analyst=me", nil, staff.analyst)
		assert.Equal(t, true, r.Body["data"].([]any)[0].(map[string]any)["analyst"].(map[string]any)["isMe"])
		assert.Equal(t, 422, p.do("GET", "/api/admin/projects?analyst=ninguem", nil, staff.analyst).Status)
	})
	t.Run("o Super Admin também precisa assumir", func(t *testing.T) {
		assert.Equal(t, "not_responsible_analyst", p.review(p.boss, fp.id, "section", "proponent", true).errCode())
	})
}

func TestAssumirProjetoDeOutroAnalista(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9502/2026")

	// Antes de iniciar a análise não há de quem assumir.
	assert.Equal(t, "analysis_not_started", p.takeOver(other, fp.id).errCode())
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.Equal(t, 200, p.review(staff.analyst, fp.id, "section", "institution", true).Status)

	t.Run("só o Analista assume", func(t *testing.T) {
		for name, tok := range map[string]string{"supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame, "proponente": p.maria, "estranho": p.joao} {
			assert.Equal(t, 403, p.takeOver(tok, fp.id).Status, name)
		}
		assert.Equal(t, p.adminID("m.analista@exemplo.com"), analystOf(p.project(other, fp.id))["id"])
	})
	t.Run("rascunho não", func(t *testing.T) {
		assert.Equal(t, 403, p.takeOver(other, p.completeProject("9503/2026").id).Status)
	})

	p.mail.Msgs = nil
	r := p.takeOver(other, fp.id)
	require.Equal(t, 200, r.Status, r.Body)
	a := analystOf(r)
	assert.Equal(t, p.adminID("m.analista2@exemplo.com"), a["id"])
	assert.Equal(t, true, a["isMe"])
	assert.Equal(t, "in_analysis", r.data()["stage"])

	t.Run("a conferência feita continua", func(t *testing.T) {
		got := reviewItems(r)["section:institution"]
		assert.Equal(t, "approved", got["status"])
		assert.NotEmpty(t, got["reviewedBy"])
	})
	t.Run("o novo responsável age e o anterior passa a só ver", func(t *testing.T) {
		assert.Equal(t, 200, p.review(other, fp.id, "section", "proposal", true).Status)
		assert.Equal(t, "not_responsible_analyst", p.review(staff.analyst, fp.id, "section", "funding", true).errCode())
		assert.Equal(t, false, analystOf(p.project(staff.analyst, fp.id))["isMe"])
	})
	t.Run("a abertura de arquivos continua sendo de cada pessoa", func(t *testing.T) {
		item := reviewItems(p.project(other, fp.id))["document:requerimento_apoio"]
		assert.Equal(t, "file_not_opened", p.review(other, fp.id, "document", "requerimento_apoio", true).errCode())
		file := fileIDOf(item)
		require.Equal(t, 204, p.openFile(other, fp.id, file))
		assert.Equal(t, 200, p.review(other, fp.id, "document", "requerimento_apoio", true).Status)
	})
	t.Run("é idempotente", func(t *testing.T) {
		assert.Equal(t, 200, p.takeOver(other, fp.id).Status)
		assert.Equal(t, 1, p.countAudit("project.take-over", fp.id))
	})
	t.Run("avisa só o responsável anterior, sem dado pessoal", func(t *testing.T) {
		got := p.mailsBy("m.analista@exemplo.com", "sua análise foi assumida")
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Body, "assumiu a análise do projeto")
		assert.Contains(t, got[0].Body, "/dashboard/projetos/"+fp.id)
		for _, secret := range []string{"@exemplo.com", "CPF"} {
			assert.NotContains(t, got[0].Body, secret)
		}
		assert.Empty(t, p.mailsBy("m.analista2@exemplo.com", "assumida"))
		assert.Empty(t, p.mailsBy("maria@exemplo.com", "assumida"), "o proponente não é avisado")
	})
	t.Run("fica no histórico de etapas e na auditoria", func(t *testing.T) {
		h := p.assertHistory(fp.id)
		last, prev := h[len(h)-1], h[len(h)-2]
		assert.Equal(t, "analyst", last.Holder)
		assert.Equal(t, p.adminID("m.analista2@exemplo.com"), *last.Admin)
		assert.Equal(t, "project.take-over", last.Transition)
		assert.Equal(t, p.adminID("m.analista@exemplo.com"), *prev.Admin)
		assert.Equal(t, 1, p.countAudit("project.take-over", fp.id))
	})
	t.Run("entra no feed e nos números de quem assumiu", func(t *testing.T) {
		feed := p.do("GET", "/api/admin/projects/queue/activity?limit=30", nil, other).Body["data"].([]any)
		found := false
		for _, x := range feed {
			if it := x.(map[string]any); it["kind"] == "analysis_taken_over" && it["projectId"] == fp.id {
				found = true
				assert.Equal(t, true, it["by"].(map[string]any)["self"])
			}
		}
		assert.True(t, found, "o feed mostra a troca")
		me := p.do("GET", "/api/admin/projects/queue/me", nil, other).data()
		assert.EqualValues(t, 1, me["last7"].(map[string]any)["assumed"])
	})
	t.Run("o proponente não vê a troca", func(t *testing.T) {
		feed := p.do("GET", "/api/admin/projects/activity", nil, p.maria).Body["data"].([]any)
		for _, x := range feed {
			assert.NotContains(t, x.(map[string]any)["kind"], "taken")
		}
		_, has := p.project(p.maria, fp.id).data()["analyst"]
		assert.False(t, has)
	})
	t.Run("o Super Admin assume e o anterior perde as ações", func(t *testing.T) {
		require.Equal(t, 200, p.takeOver(p.boss, fp.id).Status)
		assert.Equal(t, "not_responsible_analyst", p.review(other, fp.id, "section", "funding", true).errCode())
		p.assertHistory(fp.id)
	})
}

func TestAssumirAguardandoOProponente(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9504/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)

	r := p.takeOver(other, fp.id)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "awaiting_proponent", r.data()["stage"])
	assert.Equal(t, p.adminID("m.analista2@exemplo.com"), analystOf(r)["id"])
	p.assertHistory(fp.id)

	p.resubmitProposal(fp, "9504/2026")
	v := p.project(other, fp.id)
	assert.Equal(t, "in_analysis", v.data()["stage"])
	assert.Equal(t, p.adminID("m.analista2@exemplo.com"), analystOf(v)["id"], "o reenvio volta para quem assumiu")
	assert.Equal(t, "not_responsible_analyst", p.review(staff.analyst, fp.id, "section", "proposal", true).errCode())
	p.assertHistory(fp.id)
}

// O reenvio sem conferência continua pendente para o novo responsável.
func TestAssumirMantemOReenvioNaFila(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9505/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	p.resubmitProposal(fp, "9505/2026")

	resubmitted := func(tok string) bool {
		for _, x := range p.do("GET", "/api/admin/projects/queue/overview", nil, tok).data()["blocks"].(map[string]any)["resubmitted"].(map[string]any)["items"].([]any) {
			if x.(map[string]any)["projectId"] == fp.id {
				return true
			}
		}
		return false
	}
	assert.True(t, resubmitted(staff.analyst))
	require.Equal(t, 200, p.takeOver(other, fp.id).Status)
	assert.True(t, resubmitted(other), "continua na fila de reenviados depois da troca")
	require.Equal(t, 200, p.review(other, fp.id, "section", "proposal", true).Status)
	assert.False(t, resubmitted(other), "sai da fila quando algo é conferido")
}

func TestAssumirNaoVale(t *testing.T) {
	f := newFramingEnv(t, "9506/2026")
	other, _ := f.newAdmin(f.boss, "m.analista2@exemplo.com", "analyst")

	t.Run("com enquadramento pendente", func(t *testing.T) {
		require.Equal(t, 200, f.assign(f.staff.analyst, f.fp.id, f.fin).Status)
		assert.Equal(t, "framing_pending", f.takeOver(other, f.fp.id).errCode())
	})
	t.Run("aguardando o parecer", func(t *testing.T) {
		require.Equal(t, 200, f.confirm(f.staff.reviewerSame, f.fp.id, 1).Status)
		assert.Equal(t, "project_stage_invalid", f.takeOver(other, f.fp.id).errCode())
	})
	t.Run("arquivado", func(t *testing.T) {
		require.Equal(t, 200, f.do("POST", "/api/admin/projects/"+f.fp.id+"/deny", map[string]any{"note": "Negado."}, f.staff.secretary).Status)
		assert.Equal(t, 409, f.takeOver(other, f.fp.id).Status)
	})
}

// Dois Analistas assumindo ao mesmo tempo terminam com um único responsável e o histórico consistente.
func TestAssumirSimultaneo(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	b, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	c, _ := p.newAdmin(p.boss, "m.analista3@exemplo.com", "analyst")
	fp := p.submitted("9507/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)

	var wg sync.WaitGroup
	for _, tok := range []string{b, c} {
		wg.Add(1)
		go func() { defer wg.Done(); assert.Equal(t, 200, p.takeOver(tok, fp.id).Status) }()
	}
	wg.Wait()
	h := p.assertHistory(fp.id)
	winner := *h[len(h)-1].Admin
	assert.Contains(t, []string{p.adminID("m.analista2@exemplo.com"), p.adminID("m.analista3@exemplo.com")}, winner)
	assert.Equal(t, 2, p.countAudit("project.take-over", fp.id))
}

// Quem não pode mais receber e-mail (bloqueado) não derruba a troca.
func TestAssumirDeAnalistaBloqueado(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9508/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	_, err := p.pool.Exec(context.Background(), "UPDATE admins SET blocked = true WHERE email = 'm.analista@exemplo.com'")
	require.NoError(t, err)

	p.mail.Msgs = nil
	require.Equal(t, 200, p.takeOver(other, fp.id).Status)
	assert.Empty(t, p.mailsBy("m.analista@exemplo.com", "assumida"))
}
