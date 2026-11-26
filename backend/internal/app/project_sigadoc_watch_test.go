package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
)

// sigadocProcess cria no SIGADOC simulado um processo autuado (com o PIN de teste) e devolve a sigla.
func (p *projEnv) sigadocProcess(matricula string) string {
	p.t.Helper()
	ctx := context.Background()
	tok, err := p.sigadoc.Authenticate(ctx, matricula)
	require.NoError(p.t, err)
	sigla, err := p.sigadoc.CreateDocument(ctx, tok, sigadoc.NewDocument{Model: "Termo", Description: "Termo de Abertura"})
	require.NoError(p.t, err)
	sigla, err = p.sigadoc.Finalize(ctx, tok, sigla)
	require.NoError(p.t, err)
	require.NoError(p.t, p.sigadoc.Sign(ctx, tok, sigla, sigadoc.NewPIN("24681357")))
	_, err = p.sigadoc.Autuar(ctx, tok, sigla)
	require.NoError(p.t, err)
	return sigla
}

// watch registra o processo no projeto e o deixa com a consulta vencida.
func (p *projEnv) watch(projectID, sigla string) {
	p.t.Helper()
	_, err := p.pool.Exec(context.Background(), `INSERT INTO project_sigadoc (project_id, matricula, process_number, process_sigla)
		VALUES ($1, 'SEDEC123456', $2, $3)`, projectID, "2026/"+sigla, sigla)
	require.NoError(p.t, err)
}

func (p *projEnv) makeDue(projectID string) {
	p.t.Helper()
	_, err := p.pool.Exec(context.Background(), "UPDATE project_sigadoc SET watch_next_at = now() - interval '1 second' WHERE project_id = $1", projectID)
	require.NoError(p.t, err)
}

func (p *projEnv) runWatch() int {
	p.t.Helper()
	n, err := p.app.Projects.ProcessSigadocWatches(context.Background())
	require.NoError(p.t, err)
	return n
}

func (p *projEnv) sigadocState(tok, id string) resp {
	return p.do("GET", "/api/admin/projects/"+id+"/sigadoc", nil, tok)
}

func movementsOf(r resp) []map[string]any {
	var out []map[string]any
	for _, m := range r.data()["movements"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}

func (p *projEnv) watchRow(id string) (interval int, errText, situation string, archived bool) {
	p.t.Helper()
	require.NoError(p.t, p.pool.QueryRow(context.Background(),
		"SELECT watch_interval_seconds, watch_error, situation, sigadoc_archived_at IS NOT NULL FROM project_sigadoc WHERE project_id = $1", id).Scan(&interval, &errText, &situation, &archived))
	return
}

func TestAcompanhamentoRegistraAsMovimentacoesNoHistorico(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8801-2026")
	id := fp.id

	t.Run("sem processo, a equipe vê vazio e o proponente não vê nada", func(t *testing.T) {
		r := p.sigadocState(staff.analyst, id)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Nil(t, r.data()["process"])
		assert.Empty(t, movementsOf(r))
		assert.Equal(t, 404, p.sigadocState(p.maria, id).Status, "o proponente não enxerga o SIGADOC")
		assert.Equal(t, 401, p.sigadocState("", id).Status)
		assert.Equal(t, 0, p.runWatch(), "nada a consultar")
	})

	sigla := p.sigadocProcess("SEDEC123456")
	p.watch(id, sigla)
	// A criação, a finalização, a assinatura e a autuação já são movimentações do processo.
	p.sigadoc.AddMovement(sigla, "Juntada", "SEDECCONV", "SEDECCONV", "Maria Analista", time.Now())

	t.Run("a primeira consulta grava o que já existe, com a descrição do documento", func(t *testing.T) {
		require.Equal(t, 1, p.runWatch())
		r := p.sigadocState(staff.analyst, id)
		proc := r.data()["process"].(map[string]any)
		assert.Equal(t, sigla, proc["sigla"])
		assert.Equal(t, "", proc["error"])
		assert.NotNil(t, proc["lastCheckedAt"])
		ms := movementsOf(r)
		require.Len(t, ms, 5, "criação, finalização, assinatura, autuação e juntada")
		for _, m := range ms {
			assert.Equal(t, "Termo de Abertura", m["description"], "a descrição do documento tramitado")
			assert.Equal(t, false, m["afterArchive"])
		}
		assert.Equal(t, "Juntada", ms[0]["event"], "a mais recente primeiro")
		assert.Equal(t, "Maria Analista", ms[0]["person"])
		assert.Equal(t, false, r.data()["stillOpenAtSigadoc"], "projeto ativo: sem aviso de arquivamento")
	})

	t.Run("sem novidade o intervalo dobra; com novidade volta ao mínimo, e nada se grava duas vezes", func(t *testing.T) {
		iv, _, _, _ := p.watchRow(id)
		assert.Equal(t, 900, iv, "a primeira consulta trouxe novidade")
		p.makeDue(id)
		require.Equal(t, 1, p.runWatch())
		iv, _, _, _ = p.watchRow(id)
		assert.Equal(t, 1800, iv, "sem novidade, dobra")
		p.makeDue(id)
		p.runWatch()
		iv, _, _, _ = p.watchRow(id)
		assert.Equal(t, 3600, iv)
		assert.Len(t, movementsOf(p.sigadocState(staff.analyst, id)), 5, "a mesma movimentação não entra de novo")

		p.sigadoc.AddMovement(sigla, "Tramitação", "SEDECCONV", "SEDECJUR", "João", time.Now())
		p.makeDue(id)
		p.runWatch()
		iv, _, _, _ = p.watchRow(id)
		assert.Equal(t, 900, iv, "novidade: volta ao mínimo")
		assert.Len(t, movementsOf(p.sigadocState(staff.analyst, id)), 6)
	})

	t.Run("a consulta só acontece quando vence", func(t *testing.T) {
		assert.Equal(t, 0, p.runWatch(), "ainda não venceu")
	})

	t.Run("quem tem a permissão de ler vê; o Revisor de outro departamento não", func(t *testing.T) {
		assert.Equal(t, 200, p.sigadocState(staff.supervisor, id).Status)
		assert.Equal(t, 200, p.sigadocState(staff.secretary, id).Status)
		assert.Equal(t, 404, p.sigadocState(staff.reviewerOther, id).Status, "projeto sem departamento: o Revisor não o enxerga")
	})
}

func TestAcompanhamentoDoProjetoArquivadoNoSistema(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8802-2026")
	id := fp.id
	sigla := p.sigadocProcess("SEDEC123456")
	p.watch(id, sigla)
	p.sigadoc.AddMovement(sigla, "Juntada antes do arquivamento", "SEDECCONV", "SEDECCONV", "Maria", time.Now().Add(-2*time.Hour))
	require.Equal(t, 1, p.runWatch())

	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+id+"/withdraw", map[string]any{"note": "Desistimos."}, p.maria).Status)

	t.Run("projeto arquivado no sistema, processo segue aberto no SIGADOC: aviso", func(t *testing.T) {
		r := p.sigadocState(staff.analyst, id)
		assert.Equal(t, true, r.data()["projectArchived"])
		assert.Equal(t, true, r.data()["stillOpenAtSigadoc"])
		assert.EqualValues(t, 0, r.data()["newAfterArchive"])
	})

	p.sigadoc.AddMovement(sigla, "Parecer técnico anexado depois do arquivamento", "SEDECJUR", "SEDECJUR", "Ana", time.Now().Add(time.Hour))
	p.makeDue(id)
	require.Equal(t, 1, p.runWatch(), "o processo arquivado no sistema continua sendo consultado")

	t.Run("expõe o que foi acrescentado depois do arquivamento", func(t *testing.T) {
		r := p.sigadocState(staff.analyst, id)
		assert.EqualValues(t, 1, r.data()["newAfterArchive"])
		after := 0
		for _, m := range movementsOf(r) {
			if m["afterArchive"] == true {
				after++
				assert.Equal(t, "Parecer técnico anexado depois do arquivamento", m["event"])
				assert.Equal(t, false, m["seen"])
			}
		}
		assert.Equal(t, 1, after, "só a posterior ao arquivamento fica em destaque")
	})
	t.Run("marcar como vista zera o contador e fica na auditoria", func(t *testing.T) {
		r := p.do("POST", "/api/admin/projects/"+id+"/sigadoc/movements/seen", nil, staff.analyst)
		require.Equal(t, 200, r.Status, r.Body)
		assert.EqualValues(t, 0, r.data()["newAfterArchive"])
		assert.Equal(t, 1, p.countAudit("project.sigadoc-movements-seen", id))
		p.do("POST", "/api/admin/projects/"+id+"/sigadoc/movements/seen", nil, staff.analyst)
		assert.Equal(t, 1, p.countAudit("project.sigadoc-movements-seen", id), "sem novidade, não audita de novo")
		assert.Equal(t, 404, p.do("POST", "/api/admin/projects/"+id+"/sigadoc/movements/seen", nil, p.maria).Status, "o proponente não marca")
	})
	t.Run("quando o SIGADOC arquiva, o acompanhamento termina", func(t *testing.T) {
		p.sigadoc.Archive(sigla, time.Now().Add(2*time.Hour))
		p.makeDue(id)
		require.Equal(t, 1, p.runWatch())
		_, _, situation, archived := p.watchRow(id)
		assert.True(t, archived)
		assert.Equal(t, "Arquivado", situation)
		r := p.sigadocState(staff.analyst, id)
		assert.Equal(t, false, r.data()["stillOpenAtSigadoc"], "arquivado nos dois")
		assert.NotNil(t, r.data()["process"].(map[string]any)["archivedAtSigadoc"])
		p.makeDue(id)
		assert.Equal(t, 0, p.runWatch(), "arquivado no SIGADOC, não se consulta mais")
	})
}

func TestAcompanhamentoFalhaNaoEncerraEGuardaSoUmTextoCurto(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8803-2026")
	id := fp.id
	sigla := p.sigadocProcess("SEDEC123456")
	p.watch(id, sigla)

	p.sigadoc.FailNext("Movements", &sigadoc.Error{Status: 500, Message: "SIGADOC fora do ar"})
	require.Equal(t, 1, p.runWatch())
	iv, msg, _, archived := p.watchRow(id)
	assert.Equal(t, "SIGADOC fora do ar", msg)
	assert.Equal(t, 1800, iv, "falhou: tenta de novo mais tarde")
	assert.False(t, archived, "falha de rede não encerra o acompanhamento")
	assert.Equal(t, "SIGADOC fora do ar", p.sigadocState(staff.analyst, id).data()["process"].(map[string]any)["error"])

	t.Run("sem acesso ao método: texto próprio", func(t *testing.T) {
		p.makeDue(id)
		p.sigadoc.FailNext("Movements", &sigadoc.Error{Status: 403, Message: "O usuário de sistema não possui acesso a este método."})
		p.runWatch()
		_, msg, _, _ := p.watchRow(id)
		assert.Contains(t, msg, "não tem acesso")
	})
	t.Run("erro de rede não guarda URL nem credencial", func(t *testing.T) {
		p.makeDue(id)
		p.sigadoc.FailNext("Movements", context.DeadlineExceeded)
		p.runWatch()
		_, msg, _, _ := p.watchRow(id)
		assert.Equal(t, "O SIGADOC demorou demais para responder.", msg)
		p.makeDue(id)
		p.sigadoc.FailNext("Authenticate", assert.AnError)
		p.sigadoc.FailNext("Movements", assert.AnError)
		p.runWatch()
		_, msg, _, _ = p.watchRow(id)
		assert.Equal(t, "Não foi possível consultar o SIGADOC.", msg, "erro desconhecido vira um aviso genérico, sem o texto original")
	})
	t.Run("a consulta seguinte que dá certo limpa o erro", func(t *testing.T) {
		p.makeDue(id)
		p.runWatch()
		_, msg, _, _ := p.watchRow(id)
		assert.Empty(t, msg)
	})
}

func TestNumeroDoProcessoEImutavelEUnico(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	a, b := p.submitted("8804-2026"), p.submitted("8805-2026")
	p.watch(a.id, "SEDECMEM202600001A")
	ctx := context.Background()
	_, err := p.pool.Exec(ctx, "UPDATE project_sigadoc SET process_number = 'outro' WHERE project_id = $1", a.id)
	assert.Error(t, err, "imutável")
	_, err = p.pool.Exec(ctx, "UPDATE project_sigadoc SET process_sigla = 'outra' WHERE project_id = $1", a.id)
	assert.Error(t, err, "imutável")
	_, err = p.pool.Exec(ctx, `INSERT INTO project_sigadoc (project_id, matricula, process_number, process_sigla) VALUES ($1, 'SEDEC123456', $2, 'X')`,
		b.id, "2026/SEDECMEM202600001A")
	assert.Error(t, err, "o mesmo número não vale para dois projetos")
	_, err = p.pool.Exec(ctx, "UPDATE project_sigadoc SET situation = 'ok' WHERE project_id = $1", a.id)
	assert.NoError(t, err, "o resto se atualiza")
}
