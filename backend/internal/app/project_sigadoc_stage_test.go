package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stageTrail devolve, na ordem, (etapa, com quem estava) de cada trecho do histórico de etapas do projeto.
func (p *projEnv) stageTrail(id string) [][2]string {
	p.t.Helper()
	rows, err := p.pool.Query(context.Background(), "SELECT stage, holder_type FROM project_stage_history WHERE project_id = $1 ORDER BY seq", id)
	require.NoError(p.t, err)
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var s, h string
		require.NoError(p.t, rows.Scan(&s, &h))
		out = append(out, [2]string{s, h})
	}
	require.NoError(p.t, rows.Err())
	return out
}

func (p *projEnv) projectIDs(tok, query string) []string {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects?"+query, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	var out []string
	for _, it := range r.list() {
		out = append(out, it.(map[string]any)["id"].(string))
	}
	return out
}

// Confirmar o enquadramento leva o projeto de volta à fila dos analistas (sem responsável) e avisa todos os Analistas.
func TestEnquadramentoConfirmadoVoltaParaAFilaDosAnalistas(t *testing.T) {
	f := newFramingEnv(t, "8101-2026")
	other, _ := f.newAdmin(f.boss, "m.analista2@exemplo.com", "analyst")
	id := f.fp.id
	require.Equal(t, 200, f.assign(f.staff.analyst, id, f.fin).Status)
	require.NotNil(t, analystOf(f.project(f.staff.analyst, id)), "em análise, com responsável")

	f.mail.Msgs = nil
	r := f.confirm(f.staff.reviewerSame, id, 1)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "awaiting_analyst", r.data()["stage"])
	assert.Equal(t, "confirmed", framingOf(r)["decision"])

	v := f.project(f.staff.analyst, id)
	assert.Equal(t, "awaiting_analyst", v.data()["stage"])
	assert.Nil(t, analystOf(v), "o responsável da análise é liberado")
	assert.Equal(t, "Financeiro", framingOf(v)["department"].(map[string]any)["name"], "o departamento permanece")
	assert.Equal(t, [][2]string{{"awaiting_checklist", "team"}, {"in_analysis", "analyst"}, {"in_analysis", "department"}, {"awaiting_analyst", "team"}}, f.stageTrail(id))

	t.Run("e-mail a todos os Analistas, sem dados pessoais", func(t *testing.T) {
		for _, to := range []string{"m.analista@exemplo.com", "m.analista2@exemplo.com"} {
			mails := f.mailsBy(to, "aguardando analista")
			require.Len(t, mails, 1, to)
			body := mails[0].Body
			assert.Contains(t, body, v.data()["code"])
			assert.Contains(t, body, "departamento Financeiro")
			assert.Contains(t, body, "/dashboard/projetos/"+id)
			assert.Contains(t, body, "Pavimentação da Rua A")
			assert.NotRegexp(t, `\b\d{3}\.\d{3}\.\d{3}-\d{2}\b|\b\d{11}\b`, body, "nada que pareça CPF")
		}
		assert.Empty(t, f.mailsBy("m.supervisor@exemplo.com", "aguardando analista"), "só os Analistas")
		assert.Empty(t, f.mailsBy("m.revisor.fin@exemplo.com", "aguardando analista"), "o Revisor não recebe")
	})
	t.Run("não duplica ao reprocessar a caixa de saída", func(t *testing.T) {
		f.drainOutbox()
		assert.Len(t, f.mailsBy("m.analista2@exemplo.com", "aguardando analista"), 1)
	})

	t.Run("aparece na fila e nos filtros", func(t *testing.T) {
		assert.Equal(t, []string{id}, f.projectIDs(other, "stage=awaiting_analyst"))
		assert.Empty(t, f.projectIDs(other, "stage=in_analysis"))
		assert.Empty(t, f.projectIDs(other, "stage=opening_sigadoc"))
		q := f.do("GET", "/api/admin/projects/queue/overview", nil, other).data()
		assert.EqualValues(t, 1, q["counts"].(map[string]any)["awaiting_analyst"])
		assert.EqualValues(t, 0, q["counts"].(map[string]any)["in_analysis"])
		blk := q["blocks"].(map[string]any)["awaiting_analyst"].(map[string]any)
		assert.EqualValues(t, 1, blk["total"])
		assert.Equal(t, id, blk["items"].([]any)[0].(map[string]any)["projectId"])
		own := f.do("GET", "/api/admin/projects/overview", nil, f.maria).data()
		assert.EqualValues(t, 1, own["counts"].(map[string]any)["awaiting_analyst"], "o proponente também conta a etapa")
	})
	t.Run("a conferência não cabe mais", func(t *testing.T) {
		for name, r := range map[string]resp{
			"devolver": f.complement(f.staff.analyst, id, map[string]any{"items": []any{section("proposal", "")}}),
			"desfazer": f.review(other, id, "section", "proponent", false),
			"indicar":  f.assign(other, id, f.jur),
			"arquivar": f.do("POST", "/api/admin/projects/"+id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, other),
		} {
			assert.Equal(t, 409, r.Status, name)
			assert.Equal(t, "project_stage_invalid", r.errCode(), name)
		}
		assert.Equal(t, "project_stage_invalid", f.takeOver(other, id).errCode(), "sem responsável, não há de quem assumir: assume-se pela fila")
	})
}

// Um analista assume o projeto da fila e vira o responsável pela abertura do processo; só ele age, os outros assumem (take-over).
func TestAssumirProjetoDaFilaPosEnquadramento(t *testing.T) {
	f := newFramingEnv(t, "8201-2026")
	other, otherID := f.newAdmin(f.boss, "m.analista2@exemplo.com", "analyst")
	id := f.fp.id
	require.Equal(t, 200, f.assign(f.staff.analyst, id, f.fin).Status)
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, id, 1).Status)

	r := f.startAnalysis(other, id)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "opening_sigadoc", r.data()["stage"])
	assert.Equal(t, otherID, analystOf(r)["id"])
	assert.Equal(t, 1, f.countAudit("project.assume-opening", id))
	assert.Equal(t, [2]string{"opening_sigadoc", "analyst"}, f.stageTrail(id)[len(f.stageTrail(id))-1])

	t.Run("quem assumiu é o responsável e repetir não muda nada", func(t *testing.T) {
		before := f.project(other, id).data()["version"]
		assert.Equal(t, 200, f.startAnalysis(other, id).Status)
		assert.Equal(t, 1, f.countAudit("project.assume-opening", id))
		assert.Equal(t, before, f.project(other, id).data()["version"])
	})
	t.Run("outro analista não age; o antigo responsável também não", func(t *testing.T) {
		assert.Equal(t, "not_responsible_analyst", f.startAnalysis(f.staff.analyst, id).errCode())
		assert.Equal(t, [][2]string{{"opening_sigadoc", "analyst"}}, f.stageTrail(id)[len(f.stageTrail(id))-1:])
	})
	t.Run("take-over troca o responsável e avisa o anterior", func(t *testing.T) {
		f.mail.Msgs = nil
		r := f.takeOver(f.staff.analyst, id)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Equal(t, "opening_sigadoc", r.data()["stage"], "a etapa não muda")
		assert.Equal(t, f.adminID("m.analista@exemplo.com"), analystOf(r)["id"])
		assert.Len(t, f.mailsBy("m.analista2@exemplo.com", "assumida por outro analista"), 1)
	})
	t.Run("na abertura do processo a conferência continua fechada", func(t *testing.T) {
		for name, r := range map[string]resp{
			"devolver": f.complement(f.staff.analyst, id, map[string]any{"items": []any{section("proposal", "")}}),
			"desfazer": f.review(f.staff.analyst, id, "section", "proponent", false),
			"indicar":  f.assign(f.staff.analyst, id, f.jur),
		} {
			assert.Equal(t, 409, r.Status, name)
			assert.Contains(t, r.Body["error"].(map[string]any)["message"], "abertura do processo no SIGADOC", name)
		}
	})
	t.Run("filtros e contadores", func(t *testing.T) {
		assert.Equal(t, []string{id}, f.projectIDs(f.staff.analyst, "stage=opening_sigadoc"))
		assert.Empty(t, f.projectIDs(f.staff.analyst, "stage=awaiting_analyst"))
		q := f.do("GET", "/api/admin/projects/queue/overview?scope=mine", nil, f.staff.analyst).data()
		assert.EqualValues(t, 1, q["counts"].(map[string]any)["opening_sigadoc"])
		assert.EqualValues(t, 0, q["counts"].(map[string]any)["awaiting_analyst"])
	})
	t.Run("desistência e negação continuam valendo", func(t *testing.T) {
		assert.Equal(t, 200, f.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Negado."}, f.staff.secretary).Status)
		assert.Equal(t, "archived", f.project(f.staff.analyst, id).data()["status"])
	})
}

// Quem recusa continua devolvendo o projeto ao analista de sempre: só a confirmação libera o responsável.
func TestRecusaMantemOAnalistaEAConfirmacaoOLibera(t *testing.T) {
	f := newFramingEnv(t, "8301-2026")
	id := f.fp.id
	require.Equal(t, 200, f.assign(f.staff.analyst, id, f.fin).Status)
	require.Equal(t, 200, f.refuse(f.staff.reviewerSame, id, 1, "Não é do nosso departamento.").Status)
	v := f.project(f.staff.analyst, id)
	assert.Equal(t, "in_analysis", v.data()["stage"])
	assert.NotNil(t, analystOf(v), "o analista continua responsável após a recusa")
	f.mail.Msgs = nil
	require.Equal(t, 200, f.assign(f.staff.analyst, id, f.jur).Status)
	require.Equal(t, 200, f.confirm(f.staff.reviewerOther, id, 2).Status)
	assert.Equal(t, "awaiting_analyst", f.project(f.staff.analyst, id).data()["stage"])
	assert.Len(t, f.mailsBy("m.analista@exemplo.com", "aguardando analista"), 1, "um aviso, da rodada que confirmou")
}
