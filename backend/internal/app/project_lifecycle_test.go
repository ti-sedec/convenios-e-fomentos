package app_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staff cria uma pessoa da equipe com as ações dadas e devolve o token dela.
func (p *projEnv) staff(email string, actions ...string) string {
	p.t.Helper()
	role := "papel-" + email
	p.mkRole(role, actions...)
	tok, _ := p.newAdmin(p.boss, email, role)
	return tok
}

func (p *projEnv) analyst() string {
	return p.staff("analista@exemplo.com", "admin::projects.read", "admin::projects.request-complement", "admin::projects.archive")
}

// submitted devolve um projeto de Maria já enviado, com um arquivo real em "requerimento_apoio".
func (p *projEnv) submitted(sigcon string) *fundingProject {
	p.t.Helper()
	fp := p.completeProject(sigcon)
	fp.upload(p.maria, "requerimento_apoio", "")
	// O requerimento fica só com o arquivo (sem a justificativa de não envio que o completeProject deu a todos os documentos).
	require.Equal(p.t, 200, p.do("PUT", docsPath(fp.id, "/requerimento_apoio/justification"), map[string]any{"text": ""}, p.maria).Status)
	require.Equal(p.t, 200, fp.submit(p.maria, true).Status)
	return fp
}

func (p *projEnv) complement(tok, id string, body map[string]any) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/complement-requests", body, tok)
}

func section(target, note string) map[string]any {
	return map[string]any{"kind": "section", "target": target, "note": note}
}

func document(target, note string) map[string]any {
	return map[string]any{"kind": "document", "target": target, "note": note}
}

func (p *projEnv) project(tok, id string) resp {
	p.t.Helper()
	r := p.do("GET", "/api/admin/projects/"+id, nil, tok)
	require.Equal(p.t, 200, r.Status, r.Body)
	return r
}

func (p *projEnv) setDue(id string, due time.Time) {
	p.t.Helper()
	_, err := p.pool.Exec(context.Background(), "UPDATE project_complement_requests SET due_at = $2 WHERE project_id = $1 AND status = 'open'", id, due)
	require.NoError(p.t, err)
}

func TestPedidoDeComplementacao(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("2000/2026")

	body := map[string]any{"message": "Corrija o endereço e reenvie o requerimento.",
		"items": []any{section("institution", "O CEP não confere."), document("requerimento_apoio", "Faltou a assinatura.")}}
	r := p.complement(analyst, fp.id, body)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "awaiting_proponent", r.data()["stage"])
	c := r.data()["complement"].(map[string]any)
	assert.EqualValues(t, 1, c["number"])
	assert.Equal(t, "open", c["status"])
	assert.Equal(t, false, c["overdue"])
	assert.Equal(t, "Corrija o endereço e reenvie o requerimento.", c["message"])
	items := c["items"].([]any)
	require.Len(t, items, 2)
	labels := map[string]string{}
	for _, it := range items {
		m := it.(map[string]any)
		labels[m["target"].(string)] = m["label"].(string)
		assert.Equal(t, false, m["answered"])
	}
	assert.Equal(t, "Instituição", labels["institution"])
	assert.Equal(t, "Requerimento de apoio financeiro", labels["requerimento_apoio"])

	// Prazo: fim do 3º dia corrido, 23:59:59 em Cuiabá.
	loc, err := time.LoadLocation("America/Cuiaba")
	require.NoError(t, err)
	due, err := time.Parse(time.RFC3339, c["dueAt"].(string))
	require.NoError(t, err)
	now := time.Now().In(loc)
	want := time.Date(now.Year(), now.Month(), now.Day()+3, 23, 59, 59, 0, loc)
	assert.True(t, due.Equal(want), "prazo %s, esperado %s", due.In(loc), want)

	// O proponente vê o pedido e recebe o e-mail.
	got := p.project(p.maria, fp.id)
	assert.Equal(t, "awaiting_proponent", got.data()["stage"])
	assert.Equal(t, "open", got.data()["complement"].(map[string]any)["status"])
	msg := p.mail.Msgs[len(p.mail.Msgs)-1]
	assert.Equal(t, "maria@exemplo.com", msg.To)
	assert.Contains(t, msg.Subject, "correções solicitadas")
	for _, want := range []string{"O CEP não confere.", "Faltou a assinatura.", "Corrija o endereço", "arquivado"} {
		assert.Contains(t, msg.Body, want)
	}

	// Validações e estados.
	again := p.complement(analyst, fp.id, body)
	assert.Equal(t, 409, again.Status)
	assert.Equal(t, "project_stage_invalid", again.errCode(), "já aguarda o proponente")
	other := p.submitted("2001/2026")
	for name, b := range map[string]map[string]any{
		"sem itens":                   {"items": []any{}},
		"seção inexistente":           {"items": []any{section("nada", "")}},
		"documento fora do checklist": {"items": []any{document("nao_impedimento", "")}},
		"tipo inválido":               {"items": []any{map[string]any{"kind": "arquivo", "target": "x"}}},
		"item repetido":               {"items": []any{section("proposal", ""), section("proposal", "")}},
		"nota longa":                  {"items": []any{section("proposal", repeat("a", 501))}},
		"mensagem longa":              {"message": repeat("a", 2001), "items": []any{section("proposal", "")}},
	} {
		t.Run(name, func(t *testing.T) { assert.Equal(t, 422, p.complement(analyst, other.id, b).Status) })
	}
	draft := p.completeProject("2002/2026")
	assert.Equal(t, "project_not_submitted", p.complement(analyst, draft.id, body).errCode())
	assert.Equal(t, 403, p.complement(p.maria, other.id, body).Status, "o proponente não pede complementação")
	assert.Equal(t, 403, p.complement(p.joao, other.id, body).Status)
	assert.Equal(t, 404, p.complement(analyst, "00000000-0000-0000-0000-000000000000", body).Status)
}

func TestProponenteSoAlteraOQueFoiDevolvido(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("2100/2026")
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("institution", "CEP"), document("requerimento_apoio", "assinar")}}).Status)
	v := fp.current(p.maria)

	// Seção não devolvida: recusada. Devolvida: aceita e marcada como respondida.
	pb := proponentBody(v)
	r := p.do("PUT", "/api/admin/projects/"+fp.id+"/proponent", pb, p.maria)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "not_returned", r.errCode())
	ib := institutionBody(v, p.typeID("municipal_government"))
	ib["zip"] = "78000-111"
	r = p.do("PUT", "/api/admin/projects/"+fp.id+"/institution", ib, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "78000111", r.data()["institution"].(map[string]any)["zip"])
	answered := map[string]bool{}
	for _, it := range r.data()["complement"].(map[string]any)["items"].([]any) {
		m := it.(map[string]any)
		answered[m["target"].(string)] = m["answered"].(bool)
	}
	assert.True(t, answered["institution"])
	assert.False(t, answered["requerimento_apoio"], "o documento ainda não foi tocado")
	assert.Equal(t, "not_returned", p.do("PUT", "/api/admin/projects/"+fp.id+"/funding", map[string]any{"version": v + 1, "type": "own", "ownAmountCents": 5}, p.maria).errCode())

	// Documentos: só o devolvido (e o que ficar pendente).
	body := map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}
	assert.Equal(t, "not_returned", p.do("POST", docsPath(fp.id, "/termo_referencia/files"), body, p.maria).errCode())
	assert.Equal(t, "not_returned", p.do("PUT", docsPath(fp.id, "/termo_referencia/justification"), map[string]any{"text": "x"}, p.maria).errCode())
	assert.Equal(t, 409, p.do("PUT", "/api/admin/projects/"+fp.id+"/document-options", map[string]any{"version": v + 1, "reducedDeadline": true}, p.maria).Status)

	// Trocar o arquivo devolvido: o antigo (que fez parte do envio) só sai do checklist e continua guardado.
	items, _ := fp.checklist(p.maria)
	old := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	var key string
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT object_key FROM project_files WHERE id = $1", old).Scan(&key))
	assert.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", old), nil, p.maria).Status)
	assert.True(t, p.store.Has(key), "o objeto da versão enviada permanece")
	var locked bool
	var superseded *time.Time
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT locked, superseded_at FROM project_files WHERE id = $1", old).Scan(&locked, &superseded))
	assert.True(t, locked)
	assert.NotNil(t, superseded)
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "missing", items["requerimento_apoio"]["status"], "sem o arquivo, volta a pendente")
	assert.Empty(t, items["requerimento_apoio"]["files"])
	assert.Equal(t, 404, p.do("DELETE", docsPath(fp.id, "/files/", old), nil, p.maria).Status, "já substituído")
	// O arquivo antigo ainda pode ser baixado (histórico).
	assert.Equal(t, 200, p.do("GET", docsPath(fp.id, "/files/", old, "/download"), nil, p.maria).Status)

	// Anexar o novo: conta como resposta ao item.
	fp.upload(p.maria, "requerimento_apoio", "")
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "done", items["requerimento_apoio"]["status"])
	cur := p.project(p.maria, fp.id)
	for _, it := range cur.data()["complement"].(map[string]any)["items"].([]any) {
		assert.Equal(t, true, it.(map[string]any)["answered"], it.(map[string]any)["target"])
	}
}

func TestReenvioComAsCorrecoes(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("2200/2026")
	code := p.project(p.maria, fp.id).data()["code"].(string)

	// Reenviar sem pedido aberto não é possível.
	r := p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria)
	assert.Equal(t, "project_not_awaiting", r.errCode())

	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("proposal", "novo título"), document("requerimento_apoio", "")}}).Status)
	resubmit := func(tok string, declaration bool) resp {
		return p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": declaration}, tok)
	}
	assert.Equal(t, 422, resubmit(p.maria, false).Status)
	assert.Equal(t, 403, resubmit(p.joao, true).Status)

	// Corrige a proposta; apagar o arquivo devolvido deixa o documento pendente, e o reenvio é bloqueado.
	pb := proposalBody(fp.current(p.maria))
	pb["title"], pb["objectNature"] = "Pavimentação da Rua B", "services"
	pb["sigconNumber"] = "2200/2026"
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).Status)
	items, _ := fp.checklist(p.maria)
	oldFile := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", oldFile), nil, p.maria).Status)
	blocked := resubmit(p.maria, true)
	assert.Equal(t, 422, blocked.Status)
	assert.Equal(t, "project_has_issues", blocked.errCode())

	fp.upload(p.maria, "requerimento_apoio", "")
	r = resubmit(p.maria, true)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "in_analysis", r.data()["stage"], "volta para a análise")
	assert.Equal(t, code, r.data()["code"], "o código não muda")
	assert.Equal(t, "answered", r.data()["complement"].(map[string]any)["status"])
	assert.Equal(t, "Pavimentação da Rua B", r.data()["proposal"].(map[string]any)["title"])

	// Duas versões, ambas imutáveis: a 1 com o título antigo, a 2 com o novo.
	var n1, n2 string
	var reason string
	ctx := context.Background()
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT snapshot::text FROM project_versions WHERE project_id = $1 AND number = 1", fp.id).Scan(&n1))
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT snapshot::text, reason FROM project_versions WHERE project_id = $1 AND number = 2", fp.id).Scan(&n2, &reason))
	assert.Equal(t, "complement", reason)
	assert.Contains(t, n1, "Pavimentação da Rua A")
	assert.NotContains(t, n1, "Rua B")
	assert.Contains(t, n2, "Pavimentação da Rua B")
	var ansVersion int
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT answered_version FROM project_complement_requests WHERE project_id = $1", fp.id).Scan(&ansVersion))
	assert.Equal(t, 2, ansVersion)

	// Fechou: nada mais é editável e o comprovante continua o do envio original.
	assert.Equal(t, "project_not_editable", p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).errCode())
	assert.Equal(t, "project_not_awaiting", resubmit(p.maria, true).errCode())
	status, _, pdf := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	assert.Equal(t, 200, status)
	assert.Contains(t, string(pdf), "Rua A")
	assert.Contains(t, p.mail.Msgs[len(p.mail.Msgs)-1].Subject, "correções recebidas")

	// Uma segunda rodada de complementação é possível depois da análise.
	r2 := p.complement(analyst, fp.id, map[string]any{"items": []any{section("funding", "")}})
	require.Equal(t, 200, r2.Status, r2.Body)
	assert.EqualValues(t, 2, r2.data()["complement"].(map[string]any)["number"])
}

func TestPrazoVencidoArquivaSozinho(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()

	// 1) Ao abrir o projeto depois do prazo.
	a := p.submitted("2300/2026")
	require.Equal(t, 200, p.complement(analyst, a.id, map[string]any{"items": []any{section("institution", "")}}).Status)
	p.setDue(a.id, time.Now().Add(-time.Minute))
	got := p.project(p.maria, a.id)
	assert.Equal(t, "archived", got.data()["status"])
	arc := got.data()["archive"].(map[string]any)
	assert.Equal(t, "complement_deadline", arc["reason"])
	assert.Contains(t, arc["note"], "prazo")
	assert.Equal(t, "expired", got.data()["complement"].(map[string]any)["status"])
	assert.Contains(t, p.mail.Msgs[len(p.mail.Msgs)-1].Subject, "arquivado")
	var by *string
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT archived_by_id::text FROM projects WHERE id = $1", a.id).Scan(&by))
	assert.Nil(t, by, "arquivado pelo sistema")
	logs := p.do("GET", "/api/admin/audit-logs?action=project.archive&actorType=system", nil, p.boss).list()
	assert.Len(t, logs, 1)

	// 2) Ao tentar gravar depois do prazo, mesmo que a rotina ainda não tenha rodado.
	b := p.submitted("2301/2026")
	require.Equal(t, 200, p.complement(analyst, b.id, map[string]any{"items": []any{section("institution", "")}}).Status)
	v := b.current(p.maria)
	p.setDue(b.id, time.Now().Add(-time.Second))
	ib := institutionBody(v, p.typeID("municipal_government"))
	r := p.do("PUT", "/api/admin/projects/"+b.id+"/institution", ib, p.maria)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "project_not_editable", r.errCode())
	assert.Equal(t, "archived", p.project(p.maria, b.id).data()["status"])

	// 3) A rotina arquiva em lote e não repete.
	c, d := p.submitted("2302/2026"), p.submitted("2303/2026")
	for _, x := range []*fundingProject{c, d} {
		require.Equal(t, 200, p.complement(analyst, x.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
		p.setDue(x.id, time.Now().Add(-time.Hour))
	}
	n, err := p.app.Projects.ExpireComplements(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	n, err = p.app.Projects.ExpireComplements(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)

	// 4) Prazo ainda no futuro: nada acontece.
	e := p.submitted("2304/2026")
	require.Equal(t, 200, p.complement(analyst, e.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	n, err = p.app.Projects.ExpireComplements(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, "submitted", p.project(p.maria, e.id).data()["status"])
}

func TestExpiracaoSimultaneaArquivaUmaVez(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	var ids []string
	for i := range 4 {
		x := p.submitted(fmt.Sprintf("24%02d/2026", i))
		require.Equal(t, 200, p.complement(analyst, x.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
		p.setDue(x.id, time.Now().Add(-time.Hour))
		ids = append(ids, x.id)
	}
	var wg sync.WaitGroup
	counts := make(chan int, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := p.app.Projects.ExpireComplements(context.Background())
			assert.NoError(t, err)
			counts <- n
		}()
	}
	wg.Wait()
	p.drainOutbox() // ExpireComplements é chamada direto, sem requisição que esvazie a caixa de saída
	close(counts)
	total := 0
	for n := range counts {
		total += n
	}
	assert.Equal(t, 4, total, "cada projeto é arquivado por uma única execução")
	var archived int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM projects WHERE status = 'archived'").Scan(&archived))
	assert.Equal(t, 4, archived)
	var mails int
	for _, m := range p.mail.Msgs {
		if m.Subject != "" && len(m.Subject) > 8 && m.Subject[len(m.Subject)-9:] == "arquivado" {
			mails++
		}
	}
	assert.Equal(t, 4, mails, "um e-mail por projeto")
	_ = ids
}

func TestDesistenciaDoProponente(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("2500/2026")
	withdraw := func(tok, note string) resp {
		return p.do("POST", "/api/admin/projects/"+fp.id+"/withdraw", map[string]any{"note": note}, tok)
	}
	assert.Equal(t, 422, withdraw(p.maria, "").Status, "o motivo é obrigatório")
	assert.Equal(t, 422, withdraw(p.maria, "ab").Status)
	assert.Equal(t, 403, withdraw(p.joao, "não é meu").Status)
	assert.Equal(t, 403, withdraw(analyst, "equipe não desiste").Status)

	// Desiste no meio de uma complementação: o pedido fica cancelado.
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	mailsBefore := p.mail.Count()
	r := withdraw(p.maria, "Não vamos mais executar o projeto.")
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "archived", r.data()["status"])
	assert.Equal(t, "withdrawn", r.data()["archive"].(map[string]any)["reason"])
	assert.Equal(t, "Não vamos mais executar o projeto.", r.data()["archive"].(map[string]any)["note"])
	assert.Equal(t, "cancelled", r.data()["complement"].(map[string]any)["status"])
	assert.Equal(t, mailsBefore+1, p.mail.Count(), "a desistência também avisa o proponente")
	msg := p.mail.Msgs[len(p.mail.Msgs)-1]
	assert.Contains(t, msg.Subject, "arquivado")
	assert.Contains(t, msg.Body, "a pedido do próprio proponente")
	assert.Contains(t, msg.Body, "Não vamos mais executar o projeto.")

	// Arquivado é definitivo.
	assert.Equal(t, "project_archived", withdraw(p.maria, "de novo").errCode())
	assert.Equal(t, "project_not_editable", p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", proposalBody(1), p.maria).errCode())
	assert.Equal(t, "project_not_editable", p.do("DELETE", "/api/admin/projects/"+fp.id, nil, p.maria).errCode())
	assert.Equal(t, "project_not_awaiting", p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": 1, "declaration": true}, p.maria).errCode())
	assert.Equal(t, "project_archived", p.do("POST", "/api/admin/projects/"+fp.id+"/archive", map[string]any{"reason": "other", "note": "x y z"}, analyst).errCode())
	assert.Equal(t, 200, func() int { s, _, _ := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria); return s }(), "o comprovante continua")
	assert.Len(t, p.do("GET", "/api/admin/projects?status=archived", nil, p.maria).list(), 1)

	// Rascunho não se arquiva (exclui-se); e o número do SIGCon fica livre.
	draft := p.completeProject("2501/2026")
	assert.Equal(t, "project_not_submitted", p.do("POST", "/api/admin/projects/"+draft.id+"/withdraw", map[string]any{"note": "desisti"}, p.maria).errCode())
	pb := proposalBody(0)
	pb["sigconNumber"], pb["objectNature"] = "2500/2026", "services"
	draft.save("proposal", pb)
	assert.Equal(t, 200, draft.submit(p.maria, true).Status, "o número do SIGCon do projeto arquivado está livre")
}

func TestNegativaEArquivamentoPelaEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	secretary := p.staff("secretario@exemplo.com", "admin::projects.read", "admin::projects.deny")
	fp := p.submitted("2600/2026")

	archive := func(tok, reason, note string) resp {
		return p.do("POST", "/api/admin/projects/"+fp.id+"/archive", map[string]any{"reason": reason, "note": note}, tok)
	}
	assert.Equal(t, 422, archive(analyst, "denied", "só o secretário nega").Status, "motivo fora da lista do analista")
	assert.Equal(t, 422, archive(analyst, "withdrawn", "não").Status)
	assert.Equal(t, 422, archive(analyst, "other", "").Status, "motivo obrigatório")
	assert.Equal(t, 403, archive(secretary, "other", "sem permissão de arquivar").Status)
	assert.Equal(t, 403, archive(p.maria, "other", "proponente não arquiva").Status)
	deny := func(tok, note string) resp {
		return p.do("POST", "/api/admin/projects/"+fp.id+"/deny", map[string]any{"note": note}, tok)
	}
	assert.Equal(t, 403, deny(analyst, "analista não nega").Status)
	assert.Equal(t, 403, deny(p.maria, "x y z").Status)

	// O Secretário nega a qualquer momento, sem parecer: o projeto é arquivado.
	r := deny(secretary, "Não há interesse público na proposta.")
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "archived", r.data()["status"])
	assert.Equal(t, "denied", r.data()["archive"].(map[string]any)["reason"])
	msg := p.mail.Msgs[len(p.mail.Msgs)-1]
	assert.Equal(t, "maria@exemplo.com", msg.To)
	assert.Contains(t, msg.Subject, "arquivado")
	assert.Contains(t, msg.Body, "Não há interesse público na proposta.")
	assert.Contains(t, msg.Body, "Duplicar projeto")
	logs := p.do("GET", "/api/admin/audit-logs?action=project.archive&entityId="+fp.id, nil, p.boss).list()
	require.Len(t, logs, 1)
	assert.Equal(t, "denied", logs[0].(map[string]any)["payload"].(map[string]any)["reason"])

	// O analista arquiva por falta de documentos.
	other := p.submitted("2601/2026")
	r = p.do("POST", "/api/admin/projects/"+other.id+"/archive", map[string]any{"reason": "missing_documents", "note": "Sem a certidão de habilitação."}, analyst)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "missing_documents", r.data()["archive"].(map[string]any)["reason"])
	assert.Contains(t, p.mail.Msgs[len(p.mail.Msgs)-1].Body, "documentos obrigatórios")
}

func (p *projEnv) duplicate(tok, id string, copyDocs bool) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/duplicate", map[string]any{"copyDocuments": copyDocs}, tok)
}

func TestDuplicarProjeto(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	dep := p.parliamentarian("Fulano", "state")

	// Origem: rascunho com emenda, situação de documentos, dois arquivos e uma justificativa.
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	r := fp.save("funding", map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "77", time.Now().Year(), 4_000_000)}})
	srcAmendment := r.data()["funding"].(map[string]any)["amendments"].([]any)[0].(map[string]any)["id"].(string)
	fp.save("document-options", map[string]any{"reducedDeadline": true})
	fileA := fp.upload(p.maria, "requerimento_apoio", "")
	fileB := fp.upload(p.maria, "oficio_deputado", srcAmendment)
	require.Equal(t, 200, p.do("PUT", docsPath(fp.id, "/cartao_cnpj/justification"), map[string]any{"text": "Será emitido."}, p.maria).Status)
	objects := p.store.Len()
	require.Equal(t, 2, objects)

	// Sem copiar os documentos.
	d := p.duplicate(p.maria, fp.id, false)
	require.Equal(t, 201, d.Status, d.Body)
	assert.Equal(t, "draft", d.data()["status"])
	assert.Equal(t, "", d.data()["code"])
	assert.NotEqual(t, fp.id, d.data()["id"])
	assert.Equal(t, fp.id, d.data()["originProjectId"])
	assert.EqualValues(t, 1, d.data()["version"])
	// Os períodos e locais de execução vêm junto, com ids novos.
	srcPeriods := p.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["proposal"].(map[string]any)["executionPeriods"].([]any)
	copied := d.data()["proposal"].(map[string]any)["executionPeriods"].([]any)
	require.Len(t, copied, len(srcPeriods))
	require.Len(t, copied, 2)
	for i := range copied {
		a, b := srcPeriods[i].(map[string]any), copied[i].(map[string]any)
		assert.NotEqual(t, a["id"], b["id"])
		for _, k := range []string{"startDate", "endDate", "municipalityIbge", "placeDetail"} {
			assert.Equal(t, a[k], b[k], k)
		}
	}
	assert.Equal(t, "Pavimentação da Rua A", d.data()["proposal"].(map[string]any)["title"])
	assert.Equal(t, "3216/2026", d.data()["proposal"].(map[string]any)["sigconNumber"])
	assert.Len(t, d.data()["proposal"].(map[string]any)["municipalities"], 2)
	assert.Equal(t, "11222333000181", d.data()["institution"].(map[string]any)["cnpj"])
	assert.NotEmpty(t, d.data()["proponent"].(map[string]any)["name"])
	assert.Equal(t, true, d.data()["documentOptions"].(map[string]any)["reducedDeadline"])
	f := d.data()["funding"].(map[string]any)
	assert.EqualValues(t, 4_000_000, f["totalCents"])
	newAm := f["amendments"].([]any)[0].(map[string]any)["id"].(string)
	assert.NotEqual(t, srcAmendment, newAm, "emenda com id novo")
	newID := d.data()["id"].(string)
	nfp := &fundingProject{projEnv: p, id: newID, version: version32(d)}
	items, _ := nfp.checklist(p.maria)
	assert.Equal(t, "missing", items["requerimento_apoio"]["status"], "documentos não foram copiados")
	assert.Equal(t, "missing", items["cartao_cnpj"]["status"])
	assert.Equal(t, objects, p.store.Len())

	// Copiando os documentos: arquivos (objetos novos), justificativas e o vínculo com a emenda nova.
	d2 := p.duplicate(p.maria, fp.id, true)
	require.Equal(t, 201, d2.Status, d2.Body)
	n2 := &fundingProject{projEnv: p, id: d2.data()["id"].(string), version: version32(d2)}
	am2 := d2.data()["funding"].(map[string]any)["amendments"].([]any)[0].(map[string]any)["id"].(string)
	items, _ = n2.checklist(p.maria)
	assert.Equal(t, "done", items["requerimento_apoio"]["status"])
	assert.Equal(t, "done", items["oficio_deputado@"+am2]["status"], "o ofício acompanha a emenda copiada")
	assert.Equal(t, "justified", items["cartao_cnpj"]["status"])
	assert.Equal(t, "Será emitido.", items["cartao_cnpj"]["justification"])
	copiedFile := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)
	assert.NotEqual(t, fileA, copiedFile["id"])
	assert.Equal(t, "requerimento_apoio.pdf", copiedFile["filename"])
	assert.Equal(t, "confirmed", copiedFile["status"])
	assert.Equal(t, objects+2, p.store.Len(), "dois objetos copiados")
	// A origem não mudou.
	src, _ := fp.checklist(p.maria)
	assert.Equal(t, "done", src["requerimento_apoio"]["status"])
	assert.Equal(t, fileB, src["oficio_deputado@"+srcAmendment]["files"].([]any)[0].(map[string]any)["id"])

	// Só o dono duplica.
	assert.Equal(t, 404, p.duplicate(p.joao, fp.id, false).Status)
	assert.Equal(t, 404, p.duplicate(p.maria, "00000000-0000-0000-0000-000000000000", false).Status)
	assert.Equal(t, 401, p.do("POST", "/api/admin/projects/"+fp.id+"/duplicate", nil, "").Status, "sem token")
}

func TestDuplicarDeixaNadaPelaMetadeSeUmArquivoSumiu(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	fp.upload(p.maria, "requerimento_apoio", "")
	fp.upload(p.maria, "termo_referencia", "")
	var key string
	require.NoError(t, p.pool.QueryRow(context.Background(), `SELECT f.object_key FROM project_files f JOIN project_document_items i ON i.id = f.item_id
		WHERE i.requirement_code = 'termo_referencia'`).Scan(&key))
	require.NoError(t, p.store.Delete(context.Background(), key)) // o segundo arquivo sumiu do armazenamento
	before, projects := p.store.Len(), 0
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM projects").Scan(&projects))

	r := p.duplicate(p.maria, fp.id, true)
	assert.Equal(t, 409, r.Status, r.Body)
	assert.Equal(t, "file_missing", r.errCode())
	var after int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM projects").Scan(&after))
	assert.Equal(t, projects, after, "nenhum projeto pela metade")
	assert.Equal(t, before, p.store.Len(), "o objeto já copiado é apagado")
	// Sem copiar documentos, funciona.
	assert.Equal(t, 201, p.duplicate(p.maria, fp.id, false).Status)
}

func TestDuplicarProjetoArquivadoEOSigconDoOriginal(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))

	// Original ativo: a cópia avisa do número repetido e o envio dela só passa depois de trocá-lo.
	active := p.submitted("2800/2026")
	code := p.project(p.maria, active.id).data()["code"].(string)
	d := p.duplicate(p.maria, active.id, false)
	require.Equal(t, 201, d.Status, d.Body)
	assert.Equal(t, code, d.data()["originCode"], "a cópia mostra o código do projeto de origem")
	assert.Equal(t, "sigcon_duplicated", issueFields(d)["proposal.sigconNumber"])

	// Original arquivado: o número está livre e uma nova cópia não avisa nada (descartada a primeira cópia, também ativa).
	require.Equal(t, 204, p.do("DELETE", "/api/admin/projects/"+d.data()["id"].(string), nil, p.maria).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+active.id+"/withdraw", map[string]any{"note": "Vamos refazer o projeto."}, p.maria).Status)
	again := p.duplicate(p.maria, active.id, false)
	require.Equal(t, 201, again.Status, again.Body)
	assert.NotContains(t, issueFields(again), "proposal.sigconNumber")
	nid := again.data()["id"].(string)
	assert.Equal(t, "draft", p.project(p.maria, nid).data()["status"])
}

func TestEmendaComDocumentoEnviadoNaoPodeSerRemovida(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	dep := p.parliamentarian("Fulano", "state")
	year := time.Now().Year()

	fp := p.completeProject("2900/2026")
	r := fp.save("funding", map[string]any{"type": "amendment", "amendments": []any{amendment(dep, "1", year, 100_000)}, "counterpartKind": "financial", "counterpartCents": 10_000})
	am := r.data()["funding"].(map[string]any)["amendments"].([]any)[0].(map[string]any)["id"].(string)
	items, _ := fp.checklist(p.maria)
	for _, it := range items {
		if it["status"] == "missing" {
			fp.upload(p.maria, it["code"].(string), it["amendmentId"].(string))
		}
	}
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("funding", "Corrigir o valor.")}}).Status)

	e := amendment(dep, "1", year, 120_000)
	e["id"] = am
	save := func(rows ...any) resp {
		return p.do("PUT", "/api/admin/projects/"+fp.id+"/funding", map[string]any{"version": fp.current(p.maria), "type": "amendment", "amendments": rows,
			"counterpartKind": "financial", "counterpartCents": 10_000}, p.maria)
	}
	// Remover a emenda (que já tem documentos enviados) é recusado; corrigir o valor, não.
	r = save(amendment(dep, "2", year, 120_000))
	assert.Equal(t, 422, r.Status, r.Body)
	assert.Contains(t, fieldsOf(r), "amendments")
	r = save(e)
	require.Equal(t, 200, r.Status, r.Body)
	assert.EqualValues(t, 120_000, r.data()["funding"].(map[string]any)["totalCents"])
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "done", items["oficio_deputado@"+am]["status"])
}

func TestListagemMostraEtapaEPrazoDaComplementacao(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("3000/2026")
	row := func() map[string]any {
		return p.do("GET", "/api/admin/projects", nil, p.maria).list()[0].(map[string]any)
	}
	assert.Equal(t, "awaiting_checklist", row()["stage"])
	assert.Nil(t, row()["complementDueAt"])
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	assert.Equal(t, "awaiting_proponent", row()["stage"])
	assert.NotNil(t, row()["complementDueAt"])
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/withdraw", map[string]any{"note": "desisti de vez"}, p.maria).Status)
	assert.Equal(t, "withdrawn", row()["archiveReason"])
	assert.Nil(t, row()["complementDueAt"])
}
