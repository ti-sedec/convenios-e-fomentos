package app_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// segment é um trecho de project_stage_history.
type segment struct {
	Seq        int
	Status     string
	Stage      string
	Framing    *string
	Holder     string
	Admin      *string
	Dept       *string
	DeptName   *string
	Entered    time.Time
	Left       *time.Time
	ByType     string
	Transition string
	AuditID    *string
	Source     string
}

func (p *projEnv) history(id string) []segment {
	p.t.Helper()
	rows, err := p.pool.Query(context.Background(), `SELECT seq, status, stage, framing_status, holder_type, holder_admin_id::text,
		holder_department_id::text, holder_department_name, entered_at, left_at, entered_by_type, transition, audit_log_id::text, source
		FROM project_stage_history WHERE project_id = $1 ORDER BY seq`, id)
	require.NoError(p.t, err)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (s segment, err error) {
		err = r.Scan(&s.Seq, &s.Status, &s.Stage, &s.Framing, &s.Holder, &s.Admin, &s.Dept, &s.DeptName, &s.Entered, &s.Left, &s.ByType,
			&s.Transition, &s.AuditID, &s.Source)
		return
	})
	require.NoError(p.t, err)
	return out
}

// assertHistory confere a invariante do histórico: trechos em sequência sem lacuna nem sobreposição, só o último aberto, e o aberto
// igual ao estado atual do projeto. Devolve os trechos.
func (p *projEnv) assertHistory(id string) []segment {
	p.t.Helper()
	h := p.history(id)
	require.NotEmpty(p.t, h)
	for i, s := range h {
		assert.Equal(p.t, i+1, s.Seq)
		if i < len(h)-1 {
			require.NotNil(p.t, s.Left, "só o último trecho fica aberto")
			assert.True(p.t, s.Left.Equal(h[i+1].Entered), "o trecho seguinte começa quando o anterior termina")
		} else {
			assert.Nil(p.t, s.Left)
		}
	}
	last := h[len(h)-1]
	var status, stage string
	var framing, dept, analyst *string
	require.NoError(p.t, p.pool.QueryRow(context.Background(),
		"SELECT status, stage, framing_status, department_id::text, analyst_id::text FROM projects WHERE id = $1", id).
		Scan(&status, &stage, &framing, &dept, &analyst))
	assert.Equal(p.t, status, last.Status)
	assert.Equal(p.t, stage, last.Stage)
	assert.Equal(p.t, framing, last.Framing)
	switch last.Holder {
	case "analyst":
		assert.Equal(p.t, analyst, last.Admin)
	case "department":
		assert.Equal(p.t, dept, last.Dept)
	}
	return h
}

func holders(h []segment) []string {
	out := make([]string, len(h))
	for i, s := range h {
		out[i] = s.Holder
	}
	return out
}

func transitions(h []segment) []string {
	out := make([]string, len(h))
	for i, s := range h {
		out[i] = s.Transition
	}
	return out
}

func TestHistoricoDeEtapas(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	staff := p.newStaff()
	fin, jur := p.deptID("Financeiro"), p.deptID("Jurídico")
	fp := p.submitted("8001/2026")

	t.Run("o envio abre o primeiro trecho", func(t *testing.T) {
		h := p.assertHistory(fp.id)
		require.Len(t, h, 1)
		assert.Equal(t, "awaiting_checklist", h[0].Stage)
		assert.Equal(t, "team", h[0].Holder)
		assert.Equal(t, "project.submit", h[0].Transition)
		assert.Equal(t, "live", h[0].Source)
		assert.Equal(t, "admin", h[0].ByType)
		var submittedAt time.Time
		require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT submitted_at FROM projects WHERE id = $1", fp.id).Scan(&submittedAt))
		assert.True(t, h[0].Entered.Equal(submittedAt), "a entrada é o instante do envio")
		require.NotNil(t, h[0].AuditID)
		var action string
		require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT action FROM audit_logs WHERE id = $1", *h[0].AuditID).Scan(&action))
		assert.Equal(t, "project.submit", action, "o trecho aponta para o evento que o abriu")
	})

	t.Run("assumir a análise; repetir não abre trecho", func(t *testing.T) {
		require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
		require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
		h := p.assertHistory(fp.id)
		assert.Equal(t, []string{"team", "analyst"}, holders(h))
		assert.NotNil(t, h[1].Admin)
	})

	t.Run("devolver ao proponente e reenviar", func(t *testing.T) {
		require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "Corrija o título.")}}).Status)
		h := p.assertHistory(fp.id)
		assert.Equal(t, []string{"team", "analyst", "proponent"}, holders(h))
		assert.Equal(t, "awaiting_proponent", h[2].Stage)

		pb := proposalBody(fp.current(p.maria))
		pb["title"], pb["objectNature"], pb["sigconNumber"] = "Pavimentação da Rua B", "services", "8001/2026"
		require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).Status)
		r := p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria)
		require.Equal(t, 200, r.Status, r.Body)
		h = p.assertHistory(fp.id)
		assert.Equal(t, []string{"team", "analyst", "proponent", "analyst"}, holders(h))
		assert.Equal(t, "project.resubmit", h[3].Transition)
	})

	t.Run("enquadramento: indicar, recusar, indicar de novo e confirmar", func(t *testing.T) {
		require.True(t, allApproved(p.approveAll(staff.analyst, fp.id)))
		require.Equal(t, 200, p.assign(staff.analyst, fp.id, fin).Status)
		h := p.assertHistory(fp.id)
		require.Equal(t, "department", h[len(h)-1].Holder)
		require.NotNil(t, h[len(h)-1].DeptName)
		assert.Equal(t, "Financeiro", *h[len(h)-1].DeptName)
		assert.Equal(t, "in_analysis", h[len(h)-1].Stage, "o enquadramento pendente ainda é a etapa de análise")

		revFin := p.reviewerOf("hist.rev.fin2@exemplo.com", fin)
		require.Equal(t, 200, p.refuse(revFin, fp.id, 1, "Não é da nossa área.").Status)
		h = p.assertHistory(fp.id)
		assert.Equal(t, "analyst", h[len(h)-1].Holder, "a recusa devolve o projeto ao analista")
		assert.Equal(t, "project.framing-refuse", h[len(h)-1].Transition)

		require.Equal(t, 200, p.assign(staff.analyst, fp.id, jur).Status)
		revJur := p.reviewerOf("hist.rev.jur@exemplo.com", jur)
		require.Equal(t, 200, p.confirm(revJur, fp.id, 2).Status)
		h = p.assertHistory(fp.id)
		last := h[len(h)-1]
		assert.Equal(t, "awaiting_opinion", last.Stage)
		assert.Equal(t, "department", last.Holder)
		assert.Equal(t, "Jurídico", *last.DeptName)
		assert.Equal(t, "project.framing-confirm", last.Transition)
	})

	t.Run("arquivar fecha o último trecho em um trecho final", func(t *testing.T) {
		require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/withdraw", map[string]any{"note": "Desistimos do projeto."}, p.maria).Status)
		h := p.assertHistory(fp.id)
		last := h[len(h)-1]
		assert.Equal(t, "archived", last.Status)
		assert.Equal(t, "none", last.Holder)
		assert.Nil(t, last.Left)
	})

	t.Run("todo trecho tem evento de auditoria", func(t *testing.T) {
		for _, s := range p.history(fp.id) {
			require.NotNil(t, s.AuditID, s.Transition)
			var action, entityID string
			require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT action, entity_id FROM audit_logs WHERE id = $1", *s.AuditID).Scan(&action, &entityID))
			assert.Equal(t, s.Transition, action)
			assert.Equal(t, fp.id, entityID)
		}
	})
}

func TestHistoricoNaExpiracaoDoPrazo(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("8002/2026")
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	p.setDue(fp.id, time.Now().Add(-time.Hour))
	n, err := p.app.Projects.ExpireComplements(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	h := p.assertHistory(fp.id)
	assert.Equal(t, []string{"team", "proponent", "none"}, holders(h))
	last := h[len(h)-1]
	assert.Equal(t, "system", last.ByType, "o arquivamento por prazo é feito pelo sistema")
	assert.Equal(t, []string{"project.submit", "project.complement-request", "project.archive"}, transitions(h))
}

func TestHistoricoDeEtapasEImutavel(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.submitted("8003/2026")
	staff := p.newStaff()
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	ctx := context.Background()

	_, err := p.pool.Exec(ctx, "UPDATE project_stage_history SET stage = 'in_analysis' WHERE project_id = $1 AND left_at IS NULL", fp.id)
	assert.ErrorContains(t, err, "imutável", "só left_at pode mudar")
	_, err = p.pool.Exec(ctx, "UPDATE project_stage_history SET left_at = now() WHERE project_id = $1 AND seq = 1", fp.id)
	assert.ErrorContains(t, err, "imutável", "um trecho já fechado não muda")
	_, err = p.pool.Exec(ctx, "DELETE FROM project_stage_history WHERE project_id = $1", fp.id)
	assert.ErrorContains(t, err, "imutável")

	// No máximo um trecho aberto por projeto.
	_, err = p.pool.Exec(ctx, `INSERT INTO project_stage_history (project_id, seq, status, stage, holder_type, entered_at, entered_by_type, transition, source)
		VALUES ($1, 99, 'submitted', 'awaiting_checklist', 'team', now(), 'system', 'x', 'live')`, fp.id)
	assert.Error(t, err)

	// Coerência: analista exige o id do analista.
	_, err = p.pool.Exec(ctx, `INSERT INTO project_stage_history (project_id, seq, status, stage, holder_type, entered_at, left_at, entered_by_type, transition, source)
		VALUES ($1, 98, 'submitted', 'in_analysis', 'analyst', now(), now(), 'system', 'x', 'live')`, fp.id)
	assert.Error(t, err)
	p.assertHistory(fp.id)
}

// A migração 00031 cria um trecho estimado para os projetos que já existiam. Refaz esse passo sobre projetos em todas as situações e
// confere que o resultado é igual ao que o código grava em tempo real (a regra "com quem está" existe em Go e em SQL).
func TestMigracaoDoHistoricoInicial(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	fin := p.deptID("Financeiro")
	ctx := context.Background()

	team := p.submitted("8101/2026")
	analyst := p.submitted("8102/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, analyst.id).Status)
	proponent := p.submitted("8103/2026")
	require.Equal(t, 200, p.complement(staff.analyst, proponent.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	pending := p.submitted("8104/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, pending.id).Status)
	require.True(t, allApproved(p.approveAll(staff.analyst, pending.id)))
	require.Equal(t, 200, p.assign(staff.analyst, pending.id, fin).Status)
	opinion := p.submitted("8105/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, opinion.id).Status)
	require.True(t, allApproved(p.approveAll(staff.analyst, opinion.id)))
	require.Equal(t, 200, p.assign(staff.analyst, opinion.id, fin).Status)
	require.Equal(t, 200, p.confirm(p.reviewerOf("mig.rev@exemplo.com", fin), opinion.id, 1).Status)
	archived := p.submitted("8106/2026")
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+archived.id+"/withdraw", map[string]any{"note": "Desistimos do projeto."}, p.maria).Status)
	draft := p.completeProject("8107/2026")

	all := []*fundingProject{team, analyst, proponent, pending, opinion, archived}
	want := map[string]string{}
	for _, fp := range all {
		h := p.history(fp.id)
		want[fp.id] = h[len(h)-1].Holder
	}

	sqlText, err := os.ReadFile("../database/migrations/00031_project_stage_history.sql")
	require.NoError(t, err)
	stmt := string(sqlText)
	stmt = stmt[strings.Index(stmt, "INSERT INTO project_stage_history"):strings.Index(stmt, "-- +goose Down")]

	_, err = p.pool.Exec(ctx, "ALTER TABLE project_stage_history DISABLE TRIGGER project_stage_history_guard")
	require.NoError(t, err)
	_, err = p.pool.Exec(ctx, "DELETE FROM project_stage_history")
	require.NoError(t, err)
	_, err = p.pool.Exec(ctx, "ALTER TABLE project_stage_history ENABLE TRIGGER project_stage_history_guard")
	require.NoError(t, err)
	_, err = p.pool.Exec(ctx, stmt)
	require.NoError(t, err)

	for _, fp := range all {
		h := p.assertHistory(fp.id)
		require.Len(t, h, 1, fp.id)
		assert.Equal(t, want[fp.id], h[0].Holder, fp.id)
		assert.Equal(t, "initial", h[0].Source)
		assert.Equal(t, "initial", h[0].Transition)
		assert.Nil(t, h[0].AuditID)
	}
	assert.Empty(t, p.history(draft.id), "rascunho não tem histórico")
}

func TestAuditLogsEImutavel(t *testing.T) {
	p := newProjEnv(t)
	ctx := context.Background()
	var before int
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs").Scan(&before))
	require.Positive(t, before)

	_, err := p.pool.Exec(ctx, "UPDATE audit_logs SET action = 'x'")
	assert.ErrorContains(t, err, "imutável")
	_, err = p.pool.Exec(ctx, "DELETE FROM audit_logs")
	assert.ErrorContains(t, err, "imutável")
	_, err = p.pool.Exec(ctx, "TRUNCATE audit_logs CASCADE")
	assert.ErrorContains(t, err, "imutável")

	// Inserir continua funcionando, e a consulta da API também.
	_, err = p.pool.Exec(ctx, "INSERT INTO audit_logs (actor_type, action, entity) VALUES ('system', 'test.insert', 'test')")
	require.NoError(t, err)
	var after int
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs").Scan(&after))
	assert.Equal(t, before+1, after)
	assert.Equal(t, 200, p.do("GET", "/api/admin/audit-logs?action=test.insert", nil, p.boss).Status)
}

func TestHistoricoPelaAPI(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro")
	staff := p.newStaff()
	fin := p.deptID("Financeiro")
	fp := p.submitted("8201/2026")
	hist := func(tok string) resp { return p.do("GET", "/api/admin/projects/"+fp.id+"/history", nil, tok) }

	t.Run("só a equipe vê", func(t *testing.T) {
		assert.Equal(t, 404, hist(p.maria).Status, "o titular não vê o histórico interno")
		assert.Equal(t, 404, hist(p.joao).Status, "quem não tem acesso ao projeto também não")
		assert.Equal(t, 404, hist(staff.reviewerOther).Status, "revisor de outro departamento não vê projeto sem departamento")
		assert.Equal(t, 200, hist(staff.analyst).Status)
		assert.Equal(t, 200, hist(staff.supervisor).Status)
	})

	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.True(t, allApproved(p.approveAll(staff.analyst, fp.id)))
	require.Equal(t, 200, p.assign(staff.analyst, fp.id, fin).Status)

	r := hist(staff.analyst)
	require.Equal(t, 200, r.Status, r.Body)
	segs := r.data()["segments"].([]any)
	require.Len(t, segs, 3)
	first, last := segs[0].(map[string]any), segs[2].(map[string]any)
	assert.Equal(t, "team", first["holder"])
	assert.Equal(t, "project.submit", first["transition"])
	assert.NotNil(t, first["leftAt"])
	assert.Nil(t, last["leftAt"], "o trecho atual está aberto")
	assert.Equal(t, "department", last["holder"])
	assert.Equal(t, "Financeiro", last["department"].(map[string]any)["name"])
	assert.Equal(t, false, last["estimated"])
	assert.NotEmpty(t, last["auditLogId"])
	assert.NotEmpty(t, segs[1].(map[string]any)["analyst"].(map[string]any)["name"])

	sum := r.data()["summary"].(map[string]any)
	assert.GreaterOrEqual(t, sum["sinceSubmissionSeconds"], sum["currentStageSeconds"])
	assert.Contains(t, sum["byHolder"], "analyst")
	assert.Contains(t, sum["byStage"], "in_analysis")

	t.Run("rascunho não tem histórico", func(t *testing.T) {
		d := p.completeProject("8202/2026")
		r := p.do("GET", "/api/admin/projects/"+d.id+"/history", nil, p.maria)
		assert.Equal(t, 404, r.Status, "o titular não vê, nem do rascunho")
		assert.Equal(t, 404, p.do("GET", "/api/admin/projects/00000000-0000-0000-0000-000000000000/history", nil, staff.analyst).Status)
	})
}

func TestTravaDoHistoricoDeEtapas(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8301/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	ctx := context.Background()

	t.Run("mudar de etapa por fora do serviço é recusado ao confirmar", func(t *testing.T) {
		_, err := p.pool.Exec(ctx, "UPDATE projects SET stage = 'awaiting_proponent' WHERE id = $1", fp.id)
		assert.ErrorContains(t, err, "sem um trecho no histórico")
		_, err = p.pool.Exec(ctx, "UPDATE projects SET status = 'archived' WHERE id = $1", fp.id)
		assert.Error(t, err)
		var stage string
		require.NoError(t, p.pool.QueryRow(ctx, "SELECT stage FROM projects WHERE id = $1", fp.id).Scan(&stage))
		assert.Equal(t, "in_analysis", stage)
	})

	t.Run("mudar e registrar na mesma transação é aceito", func(t *testing.T) {
		tx, err := p.pool.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "UPDATE project_stage_history SET left_at = now() WHERE project_id = $1 AND left_at IS NULL", fp.id)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `INSERT INTO project_stage_history (project_id, seq, status, stage, holder_type, entered_at, entered_by_type, transition, source)
			VALUES ($1, 3, 'submitted', 'awaiting_proponent', 'proponent', now(), 'system', 'manual', 'live')`, fp.id)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, "UPDATE projects SET stage = 'awaiting_proponent' WHERE id = $1", fp.id)
		require.NoError(t, err)
		require.NoError(t, tx.Commit(ctx))
		p.assertHistory(fp.id)
	})

	t.Run("campos que não são estado e rascunhos não são afetados", func(t *testing.T) {
		_, err := p.pool.Exec(ctx, "UPDATE projects SET updated_at = now() WHERE id = $1", fp.id)
		assert.NoError(t, err)
		d := p.completeProject("8302/2026")
		_, err = p.pool.Exec(ctx, "UPDATE projects SET department_id = $2 WHERE id = $1", d.id, p.deptID("Financeiro"))
		assert.NoError(t, err)
	})

	t.Run("a exclusão do analista não é bloqueada", func(t *testing.T) {
		other := p.submitted("8303/2026")
		an, anID := p.newAdmin(p.boss, "hist.analista.excluido@exemplo.com", "analyst")
		require.Equal(t, 200, p.startAnalysis(an, other.id).Status)
		_, err := p.pool.Exec(ctx, "DELETE FROM admins WHERE id = $1", anID)
		require.NoError(t, err)
		var analyst *string
		require.NoError(t, p.pool.QueryRow(ctx, "SELECT analyst_id::text FROM projects WHERE id = $1", other.id).Scan(&analyst))
		assert.Nil(t, analyst)
		// Uma ação seguinte abre o trecho novo, já sem analista.
		require.Equal(t, 200, p.complement(staff.analyst, other.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
		p.assertHistory(other.id)
	})
}

// A regra "com quem está" existe em Go (holderOf) e em SQL (project_holder_type); as duas precisam concordar.
func TestRegraDeQuemEstaComOProjeto(t *testing.T) {
	p := newProjEnv(t)
	an := "11111111-1111-1111-1111-111111111111"
	cases := []struct{ status, stage, framing, analyst, want string }{
		{"archived", "in_analysis", "", an, "none"},
		{"archived", "awaiting_proponent", "", "", "none"},
		{"submitted", "awaiting_checklist", "", "", "team"},
		{"submitted", "in_analysis", "", "", "team"},
		{"submitted", "in_analysis", "", an, "analyst"},
		{"submitted", "in_analysis", "pending", an, "department"},
		{"submitted", "in_analysis", "pending", "", "department"},
		{"submitted", "awaiting_proponent", "", an, "proponent"},
		{"submitted", "awaiting_opinion", "confirmed", an, "department"},
	}
	for _, c := range cases {
		var got string
		require.NoError(t, p.pool.QueryRow(context.Background(),
			"SELECT project_holder_type($1, $2, NULLIF($3, ''), NULLIF($4, '')::uuid)", c.status, c.stage, c.framing, c.analyst).Scan(&got))
		assert.Equal(t, c.want, got, "%+v", c)
	}
}
