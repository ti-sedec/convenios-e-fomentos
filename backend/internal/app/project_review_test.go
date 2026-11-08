package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (p *projEnv) startAnalysis(tok, id string) resp {
	return p.do("POST", "/api/admin/projects/"+id+"/start-analysis", nil, tok)
}

func (p *projEnv) review(tok, id, kind, target string, approved bool) resp {
	return p.do("PUT", "/api/admin/projects/"+id+"/review/items", map[string]any{"kind": kind, "target": target, "approved": approved}, tok)
}

// viewFile registra que o arquivo foi exibido no visualizador (a primeira página apareceu).
func (p *projEnv) viewFile(tok, id, fileID string) int {
	return p.do("POST", docsPath(id, "/files/", fileID, "/viewed"), nil, tok).Status
}

// openFile vê o arquivo e rola até o fim, como o Analista faz antes de aprovar.
func (p *projEnv) openFile(tok, id, fileID string) int {
	if st := p.viewFile(tok, id, fileID); st != 204 {
		return st
	}
	return p.do("POST", docsPath(id, "/files/", fileID, "/read"), nil, tok).Status
}

// reject reprova um item com a justificativa.
func (p *projEnv) reject(tok, id, kind, target, note string) resp {
	return p.do("PUT", "/api/admin/projects/"+id+"/review/items", map[string]any{"kind": kind, "target": target, "decision": "rejected", "note": note}, tok)
}

// reviewItems indexa os itens da conferência por "tipo:alvo".
func reviewItems(r resp) map[string]map[string]any {
	out := map[string]map[string]any{}
	rv, _ := r.data()["review"].(map[string]any)
	for _, it := range rv["items"].([]any) {
		m := it.(map[string]any)
		out[m["kind"].(string)+":"+m["target"].(string)] = m
	}
	return out
}

func allApproved(r resp) bool {
	rv, _ := r.data()["review"].(map[string]any)
	return rv["allApproved"] == true
}

// approveAll abre os arquivos e aprova todos os itens conferíveis.
func (p *projEnv) approveAll(tok, id string) resp {
	p.t.Helper()
	last := p.project(tok, id)
	for key, it := range reviewItems(last) {
		if it["blocking"] == true {
			continue
		}
		for _, f := range it["files"].([]any) {
			require.Equal(p.t, 204, p.openFile(tok, id, f.(map[string]any)["id"].(string)), key)
		}
		last = p.review(tok, id, it["kind"].(string), it["target"].(string), true)
		require.Equal(p.t, 200, last.Status, "%s: %v", key, last.Body)
	}
	return last
}

// submittedWithExtra é um projeto enviado que tem também um documento adicional com arquivo.
func (p *projEnv) submittedWithExtra(sigcon string) (*fundingProject, string) {
	p.t.Helper()
	fp := p.completeProject(sigcon)
	fp.upload(p.maria, "requerimento_apoio", "")
	require.Equal(p.t, 200, p.do("PUT", docsPath(fp.id, "/requerimento_apoio/justification"), map[string]any{"text": ""}, p.maria).Status)
	extra, r := fp.createExtra(p.maria, "Foto da obra", "")
	require.Equal(p.t, 201, r.Status, r.Body)
	require.Equal(p.t, 201, fp.uploadExtra(p.maria, extra).Status)
	require.Equal(p.t, 200, fp.submit(p.maria, true).Status)
	return fp, extra
}

func fileIDOf(it map[string]any) string {
	return it["files"].([]any)[0].(map[string]any)["id"].(string)
}

func TestAssumirAAnalise(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9001/2026")
	assert.Equal(t, "awaiting_checklist", p.project(staff.analyst, fp.id).data()["stage"])
	assert.Nil(t, p.project(staff.analyst, fp.id).data()["analyst"], "ninguém assumiu ainda")

	r := p.startAnalysis(staff.analyst, fp.id)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "in_analysis", r.data()["stage"])
	a := r.data()["analyst"].(map[string]any)
	assert.Equal(t, p.adminID("m.analista@exemplo.com"), a["id"])
	assert.NotEmpty(t, a["name"])

	assert.Equal(t, true, a["isMe"], "o responsável vê que é ele")

	// Idempotente para o responsável; outro Analista não toma a análise por "iniciar": precisa assumir o projeto.
	assert.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	r = p.startAnalysis(other, fp.id)
	assert.Equal(t, 409, r.Status, r.Body)
	assert.Equal(t, "not_responsible_analyst", r.errCode())
	assert.Contains(t, r.Body["error"].(map[string]any)["message"], "Assuma o projeto")
	seen := p.project(other, fp.id).data()["analyst"].(map[string]any)
	assert.Equal(t, p.adminID("m.analista@exemplo.com"), seen["id"], "o responsável não troca")
	assert.Equal(t, false, seen["isMe"], "para o outro Analista o responsável não é ele")

	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.start-analysis' AND entity_id = $1", fp.id).Scan(&n))
	assert.Equal(t, 1, n, "só a primeira chamada muda algo e entra na auditoria")

	t.Run("só o Analista assume", func(t *testing.T) {
		fresh := p.submitted("9002/2026")
		for name, tok := range map[string]string{"supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame, "proponente": p.maria} {
			assert.Equal(t, 403, p.startAnalysis(tok, fresh.id).Status, name)
		}
	})
	t.Run("rascunho não", func(t *testing.T) {
		draft := p.completeProject("9003/2026")
		assert.Equal(t, 403, p.startAnalysis(staff.analyst, draft.id).Status)
	})
	t.Run("aguardando o proponente não", func(t *testing.T) {
		waiting := p.submitted("9004/2026")
		require.Equal(t, 200, p.complement(staff.analyst, waiting.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
		assert.Equal(t, "project_stage_invalid", p.startAnalysis(staff.analyst, waiting.id).errCode())
	})
	t.Run("duas pessoas ao mesmo tempo: uma assume, a outra é recusada", func(t *testing.T) {
		race := p.submitted("9005/2026")
		var wg sync.WaitGroup
		codes := make(chan int, 2)
		for _, tok := range []string{staff.analyst, other} {
			wg.Add(1)
			go func() { defer wg.Done(); codes <- p.startAnalysis(tok, race.id).Status }()
		}
		wg.Wait()
		close(codes)
		got := map[int]int{}
		for c := range codes {
			got[c]++
		}
		assert.Equal(t, map[int]int{200: 1, 409: 1}, got)
		var c int
		require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.start-analysis' AND entity_id = $1", race.id).Scan(&c))
		assert.Equal(t, 1, c)
	})
	t.Run("devolver sem responsável faz de quem devolveu o responsável", func(t *testing.T) {
		orphan := p.submitted("9006/2026")
		require.Equal(t, 200, p.complement(staff.analyst, orphan.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
		a := p.project(staff.analyst, orphan.id).data()["analyst"].(map[string]any)
		assert.Equal(t, p.adminID("m.analista@exemplo.com"), a["id"])
		assert.Equal(t, 409, p.complement(other, orphan.id, map[string]any{"items": []any{section("institution", "")}}).Status, "o outro Analista não age nele")
	})
	t.Run("em análise sem responsável (responsável excluído): o primeiro a iniciar vira o responsável", func(t *testing.T) {
		orphan := p.submitted("9007/2026")
		gone, _ := p.newAdmin(p.boss, "m.analista.saiu@exemplo.com", "analyst")
		require.Equal(t, 200, p.startAnalysis(gone, orphan.id).Status)
		_, err := p.pool.Exec(context.Background(), "DELETE FROM admins WHERE email = 'm.analista.saiu@exemplo.com'")
		require.NoError(t, err)
		assert.Equal(t, "in_analysis", p.project(staff.analyst, orphan.id).data()["stage"])
		assert.Nil(t, p.project(staff.analyst, orphan.id).data()["analyst"])
		r := p.startAnalysis(other, orphan.id)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Equal(t, p.adminID("m.analista2@exemplo.com"), r.data()["analyst"].(map[string]any)["id"])
	})
}

func TestConferenciaExigeAssumirAAnalise(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9101/2026")
	assert.Equal(t, "analysis_not_started", p.review(staff.analyst, fp.id, "section", "proponent", true).errCode())

	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	assert.Equal(t, 200, p.review(staff.analyst, fp.id, "section", "proponent", true).Status)

	require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "")}}).Status)
	assert.Equal(t, "project_stage_invalid", p.review(staff.analyst, fp.id, "section", "institution", true).errCode(), "aguardando o proponente")
}

func TestAprovarDocumentoExigeAbrirOArquivo(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	other, _ := p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")
	fp := p.submitted("9201/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)

	doc := reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"]
	require.NotNil(t, doc)
	file := fileIDOf(doc)
	assert.Equal(t, false, doc["files"].([]any)[0].(map[string]any)["opened"])

	// Sem abrir: recusado, dizendo quais arquivos faltam.
	r := p.review(staff.analyst, fp.id, "document", "requerimento_apoio", true)
	assert.Equal(t, 422, r.Status)
	assert.Equal(t, "file_not_opened", r.errCode())
	details := r.Body["error"].(map[string]any)["details"].(map[string]any)
	assert.EqualValues(t, 1, details["total"])
	assert.Equal(t, file, details["files"].([]any)[0].(map[string]any)["id"])

	// Abriu: aprova.
	require.Equal(t, 204, p.openFile(staff.analyst, fp.id, file))
	r = p.review(staff.analyst, fp.id, "document", "requerimento_apoio", true)
	require.Equal(t, 200, r.Status, r.Body)
	got := reviewItems(r)["document:requerimento_apoio"]
	assert.Equal(t, "approved", got["status"])
	assert.NotEmpty(t, got["reviewedBy"])
	assert.Equal(t, true, got["files"].([]any)[0].(map[string]any)["opened"])

	// A abertura é por pessoa: o que o primeiro Analista abriu não vale para o segundo, e a visão de cada um mostra o seu.
	// Quem não é o responsável vê o projeto, mas não decide itens.
	seen := reviewItems(p.project(other, fp.id))["document:requerimento_apoio"]
	assert.Equal(t, false, seen["files"].([]any)[0].(map[string]any)["opened"])
	assert.Equal(t, "not_responsible_analyst", p.review(other, fp.id, "document", "requerimento_apoio", true).errCode())

	// Desfazer volta a pendente e some quem aprovou.
	r = p.review(staff.analyst, fp.id, "document", "requerimento_apoio", false)
	require.Equal(t, 200, r.Status)
	got = reviewItems(r)["document:requerimento_apoio"]
	assert.Equal(t, "pending", got["status"])
	assert.Empty(t, got["reviewedBy"])
	// Repetir é idempotente.
	assert.Equal(t, 200, p.review(staff.analyst, fp.id, "document", "requerimento_apoio", false).Status)

	// Seção e documento só com justificativa aprovam sem abertura.
	assert.Equal(t, 200, p.review(staff.analyst, fp.id, "section", "institution", true).Status)
	for key, it := range reviewItems(r) {
		if it["kind"] == "document" && len(it["files"].([]any)) == 0 && it["blocking"] != true {
			assert.Equal(t, 200, p.review(staff.analyst, fp.id, "document", it["target"].(string), true).Status, key)
			break
		}
	}
}

func TestConferenciaTudoAprovadoEImpedimento(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9301/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	assert.False(t, allApproved(p.project(staff.analyst, fp.id)))

	r := p.approveAll(staff.analyst, fp.id)
	assert.True(t, allApproved(r), "tudo aprovado")
	summary := r.data()["review"].(map[string]any)["summary"].(map[string]any)
	assert.EqualValues(t, 0, summary["pending"])
	assert.EqualValues(t, 0, summary["blockers"])

	// Desfazer um item tira o "tudo aprovado".
	assert.False(t, allApproved(p.review(staff.analyst, fp.id, "section", "funding", false)))

	t.Run("obrigatório sem arquivo nem justificativa é impedimento", func(t *testing.T) {
		blocked := p.submitted("9302/2026")
		require.Equal(t, 200, p.startAnalysis(staff.analyst, blocked.id).Status)
		_, err := p.pool.Exec(context.Background(),
			"UPDATE project_document_items SET justification = '' WHERE project_id = $1 AND requirement_code = (SELECT requirement_code FROM project_document_items WHERE project_id = $1 AND requirement_code IS NOT NULL AND justification <> '' LIMIT 1)", blocked.id)
		require.NoError(t, err)

		view := p.project(staff.analyst, blocked.id)
		var key string
		for k, it := range reviewItems(view) {
			if it["blocking"] == true {
				key = k
				assert.NotEmpty(t, it["reason"])
			}
		}
		require.NotEmpty(t, key, "o item sem arquivo nem justificativa aparece como impedimento")
		kind, target, _ := cut(key)
		assert.Equal(t, "item_not_reviewable", p.review(staff.analyst, blocked.id, kind, target, true).errCode())

		// Aprovando todo o resto, ainda não está tudo aprovado.
		r := p.project(staff.analyst, blocked.id)
		for _, it := range reviewItems(r) {
			if it["blocking"] == true {
				continue
			}
			for _, f := range it["files"].([]any) {
				p.openFile(staff.analyst, blocked.id, f.(map[string]any)["id"].(string))
			}
			r = p.review(staff.analyst, blocked.id, it["kind"].(string), it["target"].(string), true)
			require.Equal(t, 200, r.Status, r.Body)
		}
		assert.False(t, allApproved(r))
		assert.EqualValues(t, 1, r.data()["review"].(map[string]any)["summary"].(map[string]any)["blockers"])
	})

	t.Run("item que não existe", func(t *testing.T) {
		assert.Equal(t, "item_not_reviewable", p.review(staff.analyst, fp.id, "document", "nao-existe", true).errCode())
		assert.Equal(t, "item_not_reviewable", p.review(staff.analyst, fp.id, "section", "inventada", true).errCode())
		assert.Equal(t, 422, p.review(staff.analyst, fp.id, "xyz", "proponent", true).Status)
	})
}

func cut(key string) (string, string, bool) {
	for i := range key {
		if key[i] == ':' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

func TestDocumentoAdicionalNaConferencia(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp, extra := p.submittedWithExtra("9401/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)

	it := reviewItems(p.project(staff.analyst, fp.id))["extra:"+extra]
	require.NotNil(t, it, "o documento adicional entra na conferência")
	assert.Equal(t, "Foto da obra", it["label"])

	assert.Equal(t, "file_not_opened", p.review(staff.analyst, fp.id, "extra", extra, true).errCode())
	require.Equal(t, 204, p.openFile(staff.analyst, fp.id, fileIDOf(it)))
	assert.Equal(t, 200, p.review(staff.analyst, fp.id, "extra", extra, true).Status)

	r := p.approveAll(staff.analyst, fp.id)
	require.True(t, allApproved(r))

	// Um adicional criado depois do envio (só é possível durante uma complementação) aparece pendente e impede o "tudo aprovado".
	require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("institution", "CEP")}}).Status)
	late, resp := fp.createExtra(p.maria, "Outro", "")
	require.Equal(t, 201, resp.Status, resp.Body)
	require.Equal(t, 201, fp.uploadExtra(p.maria, late).Status)
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria).Status)
	r = p.project(staff.analyst, fp.id)
	got := reviewItems(r)
	assert.Equal(t, "pending", got["extra:"+late]["status"], "o novo adicional precisa ser conferido")
	assert.Equal(t, "pending", got["section:institution"]["status"], "a seção devolvida também")
	assert.Equal(t, "approved", got["extra:"+extra]["status"], "o adicional antigo continua aprovado")
	assert.False(t, allApproved(r))
}

func TestDevolverZeraSoOsItensDevolvidos(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9501/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.True(t, allApproved(p.approveAll(staff.analyst, fp.id)))

	r := p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "Ajuste o título."), document("requerimento_apoio", "Assinar.")}})
	require.Equal(t, 200, r.Status, r.Body)
	got := reviewItems(r)
	assert.Equal(t, "pending", got["section:proposal"]["status"], "devolvido volta a pendente")
	assert.Equal(t, "pending", got["document:requerimento_apoio"]["status"])
	assert.Equal(t, "approved", got["section:institution"]["status"], "o que não foi devolvido continua aprovado")
	assert.Equal(t, "approved", got["section:funding"]["status"])
	assert.False(t, allApproved(r))

	// O proponente corrige e reenvia; os não devolvidos seguem aprovados e só os devolvidos precisam de nova conferência.
	pb := proposalBody(fp.current(p.maria))
	pb["title"], pb["objectNature"], pb["sigconNumber"] = "Pavimentação da Rua B", "services", "9501/2026"
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proposal", pb, p.maria).Status)
	items, _ := fp.checklist(p.maria)
	old := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", old), nil, p.maria).Status)
	newFile := fp.upload(p.maria, "requerimento_apoio", "")
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+fp.id+"/resubmit", map[string]any{"version": fp.current(p.maria), "declaration": true}, p.maria).Status)

	v := p.project(staff.analyst, fp.id)
	assert.Equal(t, "in_analysis", v.data()["stage"])
	got = reviewItems(v)
	assert.Equal(t, "approved", got["section:institution"]["status"])
	assert.Equal(t, "pending", got["section:proposal"]["status"])
	assert.Equal(t, newFile, fileIDOf(got["document:requerimento_apoio"]), "o arquivo novo tem outro id")
	assert.Equal(t, false, got["document:requerimento_apoio"]["files"].([]any)[0].(map[string]any)["opened"], "arquivo novo exige nova abertura")
	assert.Equal(t, "file_not_opened", p.review(staff.analyst, fp.id, "document", "requerimento_apoio", true).errCode())

	assert.True(t, allApproved(p.approveAll(staff.analyst, fp.id)))
}

func TestAberturaDeArquivosFicaRegistrada(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9601/2026")
	file := fileIDOf(reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"])
	views := func() int {
		var n int
		require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_file_views WHERE file_id = $1", file).Scan(&n))
		return n
	}

	assert.Equal(t, 204, p.openFile(p.maria, fp.id, file), "o titular vê o próprio arquivo")
	assert.Equal(t, 0, views(), "o titular não gera registro de abertura")

	assert.Equal(t, 204, p.openFile(staff.analyst, fp.id, file))
	assert.Equal(t, 1, views())
	assert.Equal(t, 204, p.openFile(staff.analyst, fp.id, file))
	assert.Equal(t, 1, views(), "abrir de novo não duplica")

	assert.Equal(t, 204, p.openFile(staff.supervisor, fp.id, file), "qualquer pessoa da equipe com leitura abre")
	assert.Equal(t, 2, views())
}

func TestVisaoDaConferenciaSoParaAEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9701/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)

	for name, tok := range map[string]string{"analista": staff.analyst, "supervisor": staff.supervisor, "secretário": staff.secretary} {
		v := p.project(tok, fp.id)
		assert.NotNil(t, v.data()["review"], name)
		assert.NotNil(t, v.data()["analyst"], name)
	}
	v := p.project(p.maria, fp.id)
	_, hasReview := v.data()["review"]
	_, hasAnalyst := v.data()["analyst"]
	assert.False(t, hasReview, "a conferência é interna")
	assert.False(t, hasAnalyst)

	// Só o Analista age; o Supervisor e o Secretário veem a conferência mas não mexem nela.
	for name, tok := range map[string]string{"supervisor": staff.supervisor, "secretário": staff.secretary, "revisor": staff.reviewerSame, "proponente": p.maria, "estranho": p.joao} {
		assert.Equal(t, 403, p.review(tok, fp.id, "section", "proponent", true).Status, name)
	}
	draft := p.completeProject("9702/2026")
	assert.Equal(t, 403, p.review(staff.analyst, draft.id, "section", "proponent", true).Status, "rascunho")
}

// O aviso de arquivamento vai ao Analista responsável; sem responsável, a todos.
func TestArquivamentoAvisaOAnalistaResponsavel(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	p.newAdmin(p.boss, "m.analista2@exemplo.com", "analyst")

	with := p.submitted("9801/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, with.id).Status)
	p.mail.Msgs = nil
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+with.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)
	got := p.archivedMails()
	assert.Contains(t, got, "m.analista@exemplo.com", "o responsável recebe")
	assert.NotContains(t, got, "m.analista2@exemplo.com", "os outros Analistas não")

	without := p.submitted("9802/2026")
	p.mail.Msgs = nil
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+without.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)
	got = p.archivedMails()
	assert.Contains(t, got, "m.analista@exemplo.com")
	assert.Contains(t, got, "m.analista2@exemplo.com", "sem responsável, todos os Analistas")

	bloqueado := p.submitted("9803/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, bloqueado.id).Status)
	_, err := p.pool.Exec(context.Background(), "UPDATE admins SET blocked = true WHERE email = 'm.analista@exemplo.com'")
	require.NoError(t, err)
	p.mail.Msgs = nil
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+bloqueado.id+"/deny", map[string]any{"note": "Negado."}, staff.secretary).Status)
	got = p.archivedMails()
	assert.NotContains(t, got, "m.analista@exemplo.com", "responsável bloqueado não recebe")
	assert.Contains(t, got, "m.analista2@exemplo.com", "cai para os Analistas ativos")
}

func TestConferenciaFicaNaAuditoriaSemTextoLivre(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9901/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	require.Equal(t, 200, p.review(staff.analyst, fp.id, "section", "proponent", true).Status)

	var payload string
	require.NoError(t, p.pool.QueryRow(context.Background(),
		"SELECT payload::text FROM audit_logs WHERE action = 'project.review-item' AND entity_id = $1", fp.id).Scan(&payload))
	assert.JSONEq(t, `{"kind":"section","target":"proponent","decision":"approved","approved":true}`, payload)

	// A justificativa de uma reprovação também não vai para a auditoria: fica só no item.
	require.Equal(t, 200, p.reject(staff.analyst, fp.id, "section", "proposal", "Texto livre da justificativa.").Status)
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM audit_logs WHERE entity_id = $1 AND payload::text LIKE '%Texto livre%'", fp.id).Scan(&n))
	assert.Equal(t, 0, n)
}
