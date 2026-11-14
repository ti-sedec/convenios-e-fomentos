package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
)

// archivedMails devolve os e-mails de arquivamento enviados, por destinatário.
func (p *projEnv) archivedMails() map[string]mailer.Message {
	p.t.Helper()
	out := map[string]mailer.Message{}
	for _, m := range p.mail.Msgs {
		if strings.HasSuffix(m.Subject, " arquivado") {
			_, dup := out[m.To]
			require.False(p.t, dup, "%s recebeu dois e-mails de arquivamento", m.To)
			out[m.To] = m
		}
	}
	return out
}

// Todo arquivamento, de qualquer motivo, avisa o proponente, os Revisores do departamento do projeto, o Analista e o Supervisor.
func TestArquivamentoAvisaProponenteRevisorAnalistaESupervisor(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fin := p.deptID("Financeiro")
	fp := p.submitted("8001-2026")
	p.setProjectDepartment(fp.id, fin)
	p.mail.Msgs = nil

	note := "Fora das prioridades do exercício."
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": note}, staff.secretary).Status)

	got := p.archivedMails()
	assert.Len(t, got, 4, "proponente, revisor do departamento, analista e supervisor")
	for _, to := range []string{"maria@exemplo.com", "m.revisor.fin@exemplo.com", "m.analista@exemplo.com", "m.supervisor@exemplo.com"} {
		assert.Contains(t, got, to)
	}
	assert.NotContains(t, got, "m.revisor.jur@exemplo.com", "revisor de outro departamento não recebe")
	assert.NotContains(t, got, "m.revisor.sem@exemplo.com", "revisor sem departamento não recebe")
	assert.NotContains(t, got, "m.secretario@exemplo.com", "o Secretário foi quem decidiu e não está na lista")

	// O proponente recebe o motivo escrito e a orientação de duplicar.
	assert.Contains(t, got["maria@exemplo.com"].Body, note)
	assert.Contains(t, got["maria@exemplo.com"].Body, "Duplicar projeto")

	// A equipe recebe só a identificação, a categoria do motivo e o link: nada do texto livre nem de dados pessoais.
	for _, to := range []string{"m.revisor.fin@exemplo.com", "m.analista@exemplo.com", "m.supervisor@exemplo.com"} {
		body := got[to].Body
		assert.Contains(t, body, "Secretário de Estado (negação)", to)
		assert.Contains(t, body, "/dashboard/projetos/"+fp.id, to)
		assert.NotContains(t, body, note, "%s: o texto livre do motivo fica no sistema", to)
		assert.NotContains(t, body, "Maria", "%s: nome do proponente", to)
		assert.NotRegexp(t, `\d{3}\.?\d{3}\.?\d{3}-?\d{2}`, body, "%s: nada que pareça CPF", to)
		assert.NotContains(t, body, "@exemplo.com", "%s: nenhum e-mail pessoal", to)
		assert.NotRegexp(t, `\(?\d{2}\)?\s?\d{4,5}-?\d{4}`, body, "%s: nenhum telefone", to)
	}
}

// Sem departamento indicado ainda, não há Revisor a avisar.
func TestArquivamentoSemDepartamentoNaoAvisaRevisor(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8002-2026")
	p.mail.Msgs = nil

	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, staff.analyst).Status)

	got := p.archivedMails()
	assert.Len(t, got, 3, "proponente, analista e supervisor")
	for to := range got {
		assert.NotContains(t, to, "revisor")
	}
}

// Cada um dos quatro caminhos de arquivamento avisa.
func TestTodosOsMotivosDeArquivamentoAvisam(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()

	cases := map[string]func(id string) int{
		"falta de documentos": func(id string) int {
			return p.do("POST", "/api/admin/projects/"+id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, staff.analyst).Status
		},
		"desistência": func(id string) int {
			return p.do("POST", "/api/admin/projects/"+id+"/withdraw", map[string]any{"note": "Desistimos."}, p.maria).Status
		},
		"negação": func(id string) int {
			return p.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status
		},
		"prazo vencido": func(id string) int {
			require.Equal(t, 200, p.complement(staff.analyst, id, map[string]any{"items": []any{section("proposal", "")}}).Status)
			p.setDue(id, time.Now().Add(-time.Hour))
			n, err := p.app.Projects.ExpireComplements(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, n)
			p.drainOutbox()
			return 200
		},
	}
	i := 0
	for name, archive := range cases {
		i++
		t.Run(name, func(t *testing.T) {
			fp := p.submitted("81" + string(rune('0'+i)) + "0-2026")
			p.mail.Msgs = nil
			require.Equal(t, 200, archive(fp.id))
			got := p.archivedMails()
			assert.Len(t, got, 3, "proponente, analista e supervisor (sem departamento indicado)")
			assert.Contains(t, got, "maria@exemplo.com")
		})
	}
}

// Quem tem dois perfis, ou o mesmo e-mail do proponente, recebe uma só mensagem; quem está bloqueado não recebe.
func TestArquivamentoNaoRepeteDestinatarioENaoAvisaBloqueado(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	// O Analista acumula o perfil de Supervisor.
	_, err := p.pool.Exec(context.Background(), "INSERT INTO admin_users_roles (admin_id, role_id) VALUES ($1, $2)",
		p.adminID("m.analista@exemplo.com"), p.roleID("administrative-supervisor"))
	require.NoError(t, err)
	// O outro Supervisor está bloqueado.
	extra, _ := p.newAdmin(p.boss, "bloqueado@exemplo.com", "administrative-supervisor")
	_ = extra
	_, err = p.pool.Exec(context.Background(), "UPDATE admins SET blocked = true WHERE email = 'bloqueado@exemplo.com'")
	require.NoError(t, err)

	fp := p.submitted("8201-2026")
	p.mail.Msgs = nil
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)

	got := p.archivedMails() // archivedMails já falha se alguém recebe duas vezes
	assert.Contains(t, got, "m.analista@exemplo.com")
	assert.Contains(t, got, "m.supervisor@exemplo.com")
	assert.NotContains(t, got, "bloqueado@exemplo.com")
	assert.Len(t, got, 3)

	// Arquivar de novo é recusado e não manda nada.
	p.mail.Msgs = nil
	assert.Equal(t, "project_archived", p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": "De novo."}, staff.secretary).errCode())
	assert.Empty(t, p.mail.Msgs)
}

// A requisição só grava na caixa de saída: o envio é de outro processo. Se o SMTP falha, a ação vale e a mensagem é reenviada.
func TestEnvioEhAssincronoESobreviveAFalhaDoSMTP(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8301-2026")
	p.mail.Msgs = nil
	p.keepOutbox = true // este teste controla quando a caixa de saída é esvaziada

	p.mail.FailTimes(100) // SMTP fora do ar
	r := p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "archived", r.data()["status"], "a ação vale mesmo com o SMTP fora do ar")
	assert.Empty(t, p.mail.Msgs, "a requisição não enviou nada")
	var pending int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbound_emails WHERE status = 'pending' AND kind = 'project.archived'").Scan(&pending))
	assert.Equal(t, 3, pending, "proponente, analista e supervisor esperam na caixa de saída")

	// Primeira varredura: o SMTP falha, ninguém recebe e as mensagens ficam agendadas.
	_, err := p.app.DrainOutbox(context.Background())
	require.NoError(t, err)
	assert.Empty(t, p.mail.Msgs)
	var lastError string
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT last_error FROM outbound_emails WHERE kind = 'project.archived' LIMIT 1").Scan(&lastError))
	assert.Contains(t, lastError, "SMTP")

	// O SMTP volta e o prazo da nova tentativa passa: as três mensagens saem.
	p.mail.FailTimes(0)
	_, err = p.pool.Exec(context.Background(), "UPDATE outbound_emails SET next_attempt_at = now() - interval '1 second'")
	require.NoError(t, err)
	_, err = p.app.DrainOutbox(context.Background())
	require.NoError(t, err)
	assert.Len(t, p.archivedMails(), 3)
}

// O e-mail só existe se a ação foi confirmada: ação recusada não deixa mensagem na caixa de saída.
func TestAcaoRecusadaNaoDeixaEmailNaCaixaDeSaida(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("8401-2026")
	p.keepOutbox = true
	before := p.outboxCount()

	assert.Equal(t, 422, p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": ""}, staff.secretary).Status, "motivo obrigatório")
	assert.Equal(t, 403, p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": "Negado."}, staff.analyst).Status)
	assert.Equal(t, before, p.outboxCount())
}

func (p *projEnv) outboxCount() int {
	p.t.Helper()
	var n int
	require.NoError(p.t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM outbound_emails").Scan(&n))
	return n
}
