package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// raw faz a requisição e devolve o corpo bruto (para respostas que não são JSON, como o PDF).
func (e *env) raw(method, path, token string) (int, http.Header, []byte) {
	e.t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	req.RemoteAddr = "10.0.0.1:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

// completeProject deixa o projeto de Maria sem nenhuma pendência que bloqueie o envio: seções preenchidas e todos os documentos
// do checklist justificados.
func (p *projEnv) completeProject(sigcon string) *fundingProject {
	p.t.Helper()
	id, v := p.create(p.maria)
	fp := &fundingProject{projEnv: p, id: id, version: v}
	fp.save("proponent", proponentBody(0))
	ib := institutionBody(0, p.typeID("municipal_government"))
	fp.save("institution", ib)
	pb := proposalBody(0)
	pb["sigconNumber"], pb["objectNature"] = sigcon, "services"
	fp.save("proposal", pb)
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_000_000})
	items, _ := fp.checklist(p.maria)
	for code := range items {
		r := p.do("PUT", docsPath(id, "/", code, "/justification"), map[string]any{"text": "Será entregue na assinatura."}, p.maria)
		require.Equal(p.t, 200, r.Status, r.Body)
	}
	return fp
}

func (f *fundingProject) submit(tok string, declaration bool) resp {
	f.t.Helper()
	return f.do("POST", "/api/admin/projects/"+f.id+"/submit", map[string]any{"version": f.current(tok), "declaration": declaration}, tok)
}

// current lê a versão atual do projeto.
func (f *fundingProject) current(tok string) int {
	f.t.Helper()
	r := f.do("GET", "/api/admin/projects/"+f.id, nil, tok)
	require.Equal(f.t, 200, r.Status, r.Body)
	return version32(r)
}

var codeRe = regexp.MustCompile(`^\d{4}-\d{6}$`)

func cuiabaYear() int {
	loc, err := time.LoadLocation("America/Cuiaba")
	if err != nil {
		return time.Now().Year()
	}
	return time.Now().In(loc).Year()
}

// setElectoralDeadline ajusta a data-limite eleitoral vigente (a padrão é de 2026, e os testes não dependem do dia em que rodam).
func (p *projEnv) setElectoralDeadline(day time.Time) {
	p.t.Helper()
	list := p.do("GET", "/api/admin/rule-parameters?key=electoral_deadline", nil, p.boss).list()
	require.NotEmpty(p.t, list)
	id := list[0].(map[string]any)["id"].(string)
	r := p.do("PUT", "/api/admin/rule-parameters/"+id, map[string]any{"value": day.Format(time.DateOnly), "validFrom": "2020-01-01", "validTo": nil}, p.boss)
	require.Equal(p.t, 200, r.Status, r.Body)
}

func TestEnvioBloqueadoPorPendencias(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	r := p.do("POST", "/api/admin/projects/"+id+"/submit", map[string]any{"version": v, "declaration": true}, p.maria)
	assert.Equal(t, 422, r.Status, r.Body)
	assert.Equal(t, "project_has_issues", r.errCode())
	issues := r.Body["error"].(map[string]any)["details"].(map[string]any)["issues"].([]any)
	assert.NotEmpty(t, issues)
	for _, it := range issues {
		assert.Equal(t, "block", it.(map[string]any)["severity"], "só as pendências que bloqueiam são listadas")
	}
	got := p.do("GET", "/api/admin/projects/"+id, nil, p.maria)
	assert.Equal(t, "draft", got.data()["status"])
	assert.Equal(t, "", got.data()["code"])
	assert.EqualValues(t, v, got.data()["version"], "nada mudou")

	// Falta só um documento: ainda bloqueia.
	fp := p.completeProject("100/2026")
	require.Equal(t, 200, p.do("PUT", docsPath(fp.id, "/cartao_cnpj/justification"), map[string]any{"text": ""}, p.maria).Status)
	r = fp.submit(p.maria, true)
	assert.Equal(t, "project_has_issues", r.errCode())
	assert.Contains(t, fmt.Sprint(r.Body["error"]), "documents_missing")
}

func TestEnvioExigeDeclaracaoEVersaoAtual(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("200/2026")
	v := fp.current(p.maria)

	r := p.do("POST", "/api/admin/projects/"+fp.id+"/submit", map[string]any{"version": v, "declaration": false}, p.maria)
	assert.Equal(t, 422, r.Status)
	assert.Contains(t, fieldsOf(r), "declaration")
	assert.Equal(t, 422, p.do("POST", "/api/admin/projects/"+fp.id+"/submit", map[string]any{"declaration": true}, p.maria).Status, "a versão é obrigatória")
	r = p.do("POST", "/api/admin/projects/"+fp.id+"/submit", map[string]any{"version": v - 1, "declaration": true}, p.maria)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "project_version_conflict", r.errCode(), "o projeto mudou desde o que o proponente viu")
	assert.Equal(t, "draft", p.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["status"])
}

func TestEnvioCompleto(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30)) // dentro do prazo, qualquer que seja o dia em que o teste rode
	fp := p.completeProject("300/2026")
	before := fp.current(p.maria)

	r := fp.submit(p.maria, true)
	require.Equal(t, 200, r.Status, r.Body)
	d := r.data()
	code := d["code"].(string)
	assert.Regexp(t, codeRe, code)
	assert.Equal(t, fmt.Sprintf("%d-000001", cuiabaYear()), code, "primeiro código do ano")
	assert.Equal(t, "submitted", d["status"])
	assert.Equal(t, "awaiting_checklist", d["stage"], "entra na etapa inicial da equipe")
	assert.NotNil(t, d["submittedAt"])
	assert.EqualValues(t, before+1, d["version"])
	assert.Equal(t, false, d["lateSubmission"])
	assert.NotContains(t, issueFields(r), "documents.documents")

	// O segundo projeto recebe o código seguinte.
	fp2 := p.completeProject("301/2026")
	assert.Equal(t, fmt.Sprintf("%d-000002", cuiabaYear()), fp2.submit(p.maria, true).data()["code"])

	// Depois do envio, nada do projeto muda mais.
	again := p.do("POST", "/api/admin/projects/"+fp.id+"/submit", map[string]any{"version": before + 1, "declaration": true}, p.maria)
	assert.Equal(t, 409, again.Status)
	assert.Equal(t, "project_already_submitted", again.errCode())
	for section, body := range map[string]map[string]any{
		"proponent": proponentBody(0), "institution": institutionBody(0, p.typeID("association")), "proposal": proposalBody(0),
		"funding": {"type": "own", "ownAmountCents": 1}, "document-options": {"publicSpaceUse": true},
	} {
		body["version"] = before + 1
		rr := p.do("PUT", "/api/admin/projects/"+fp.id+"/"+section, body, p.maria)
		assert.Equal(t, 409, rr.Status, section)
		assert.Equal(t, "project_not_editable", rr.errCode(), section)
	}
	assert.Equal(t, "project_not_editable", p.do("PUT", docsPath(fp.id, "/cartao_cnpj/justification"), map[string]any{"text": "x"}, p.maria).errCode())
	assert.Equal(t, "project_not_editable", p.do("POST", docsPath(fp.id, "/requerimento_apoio/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}, p.maria).errCode())
	assert.Equal(t, "project_not_editable", p.do("DELETE", "/api/admin/projects/"+fp.id, nil, p.maria).errCode(), "enviado não se exclui")

	// Auditoria do envio, sem dados pessoais.
	logs := p.do("GET", "/api/admin/audit-logs?action=project.submit&entityId="+fp.id, nil, p.boss).list()
	require.Len(t, logs, 1)
	payload := logs[0].(map[string]any)["payload"].(map[string]any)
	assert.Equal(t, code, payload["code"])

	// E-mail de confirmação ao e-mail principal do projeto.
	require.GreaterOrEqual(t, p.mail.Count(), 2)
	msg := p.mail.Msgs[0]
	assert.Equal(t, "maria@exemplo.com", msg.To)
	assert.Contains(t, msg.Subject, code)
	assert.Contains(t, msg.Body, code)
	assert.Contains(t, msg.Body, "/dashboard/projetos/"+fp.id)
	assert.Contains(t, msg.Body, "SIGADOC")
	assert.NotContains(t, msg.Body, "vedação eleitoral", "sem aviso quando enviado dentro do prazo")
}

func TestEnvioDescartaArquivosNaoConfirmados(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("400/2026")
	r := p.do("POST", docsPath(fp.id, "/pesquisa_precos/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}, p.maria)
	require.Equal(t, 201, r.Status, r.Body)
	pendingID := r.data()["file"].(map[string]any)["id"].(string)
	key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
	p.store.Put(key, pdfType, pdfBytes)
	kept := fp.upload(p.maria, "termo_referencia", "")

	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	assert.False(t, p.store.Has(key), "o envio pendente é apagado do armazenamento")
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_files WHERE id = $1", pendingID).Scan(&n))
	assert.Zero(t, n)
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_files WHERE id = $1 AND status = 'confirmed'", kept).Scan(&n))
	assert.Equal(t, 1, n, "o confirmado fica")
	// Confirmar um envio iniciado antes do envio do projeto não é mais possível.
	assert.Equal(t, 404, p.do("POST", docsPath(fp.id, "/files/", pendingID, "/confirm"), nil, p.maria).Status)
}

func TestVersaoDoEnvioEhImutavel(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("500/2026")
	code := fp.submit(p.maria, true).data()["code"].(string)
	ctx := context.Background()

	var number int
	var reason, snapshot string
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT number, reason, snapshot::text FROM project_versions WHERE project_id = $1", fp.id).Scan(&number, &reason, &snapshot))
	assert.Equal(t, 1, number)
	assert.Equal(t, "initial", reason)
	for _, want := range []string{code, "Pavimentação da Rua A", "Será entregue na assinatura.", "Declaro, sob as penas da lei", "documents_missing"} {
		if want == "documents_missing" {
			assert.NotContains(t, snapshot, want, "o retrato do envio não tem pendências de documentos")
			continue
		}
		assert.Contains(t, snapshot, want)
	}
	assert.NotContains(t, snapshot, "objectKey", "chaves de objetos não entram no retrato")

	_, err := p.pool.Exec(ctx, "UPDATE project_versions SET snapshot = '{}' WHERE project_id = $1", fp.id)
	require.Error(t, err, "o banco recusa alterar uma versão")
	assert.Contains(t, err.Error(), "imutável")
	var again string
	require.NoError(t, p.pool.QueryRow(ctx, "SELECT snapshot::text FROM project_versions WHERE project_id = $1", fp.id).Scan(&again))
	assert.Equal(t, snapshot, again)
}

func TestComprovanteEmPDF(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.completeProject("600/2026")

	// Rascunho não tem comprovante.
	code, _, _ := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	assert.Equal(t, 404, code)

	number := fp.submit(p.maria, true).data()["code"].(string)
	status, hdr, body := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	require.Equal(t, 200, status)
	assert.Equal(t, "application/pdf", hdr.Get("Content-Type"))
	assert.Contains(t, hdr.Get("Content-Disposition"), "comprovante-"+number+".pdf")
	assert.Equal(t, "private, no-store", hdr.Get("Cache-Control"))
	assert.True(t, strings.HasPrefix(string(body), "%PDF-"), "é um PDF")
	text := string(body)
	for _, want := range []string{number, "Comprovante de recebimento", "SEDEC", "600/2026", "Pavimenta", "11.222.333/0001-81", "R$ 100.000,00", "Declara"} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, "eleitoral")
	// Sempre igual: vem do retrato imutável.
	_, _, again := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	assert.Equal(t, len(body), len(again))

	// Quem não enxerga o projeto não baixa; quem lê tudo baixa.
	code, _, _ = p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.joao)
	assert.Equal(t, 404, code)
	p.mkRole("leitor", "admin::projects.read")
	staff, _ := p.newAdmin(p.boss, "bia@exemplo.com", "leitor")
	code, _, _ = p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", staff)
	assert.Equal(t, 200, code)
	code, _, _ = p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", "")
	assert.Equal(t, 401, code)
}

func TestNumeroDoSigconEhUnicoEntreEnviados(t *testing.T) {
	p := newProjEnv(t)
	a := p.completeProject("700/2026")
	b := p.completeProject("700/2026") // rascunho repetido só gera aviso
	assert.Equal(t, "sigcon_duplicated", issueFields(p.do("GET", "/api/admin/projects/"+b.id, nil, p.maria))["proposal.sigconNumber"])

	require.Equal(t, 200, a.submit(p.maria, true).Status)
	r := b.submit(p.maria, true)
	assert.Equal(t, 422, r.Status, r.Body)
	assert.Contains(t, fieldsOf(r), "sigconNumber")
	assert.Equal(t, "draft", p.do("GET", "/api/admin/projects/"+b.id, nil, p.maria).data()["status"])
	// Trocando o número, envia.
	pb := proposalBody(0)
	pb["sigconNumber"], pb["objectNature"] = "701/2026", "services"
	b.save("proposal", pb)
	assert.Equal(t, 200, b.submit(p.maria, true).Status)
}

func TestEnvioForaDoPrazoEleitoral(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, -1))

	fp := p.completeProject("800/2026")
	got := p.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria)
	assert.Equal(t, "electoral_deadline_passed", issueFields(got)["submission.electoralDeadline"])
	for _, it := range got.data()["issues"].([]any) {
		if m := it.(map[string]any); m["code"] == "electoral_deadline_passed" {
			assert.Equal(t, "warn", m["severity"], "é aviso, não bloqueia")
		}
	}
	r := fp.submit(p.maria, true)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, true, r.data()["lateSubmission"])
	assert.Contains(t, p.mail.Msgs[len(p.mail.Msgs)-1].Body, "vedação eleitoral")
	_, _, pdf := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	assert.Contains(t, string(pdf), "eleitoral", "o PDF usa Latin-1: só o trecho ASCII é comparável")

	// Dentro do prazo: sem aviso nem marca.
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 5))
	ok := p.completeProject("801/2026")
	assert.NotContains(t, issueFields(p.do("GET", "/api/admin/projects/"+ok.id, nil, p.maria)), "submission.electoralDeadline")
	assert.Equal(t, false, ok.submit(p.maria, true).data()["lateSubmission"])

	// A listagem traz a marca (para o selo "Fora do prazo" da fila da equipe).
	late := map[string]any{}
	for _, it := range p.do("GET", "/api/admin/projects", nil, p.maria).list() {
		m := it.(map[string]any)
		late[m["id"].(string)] = m["lateSubmission"]
	}
	assert.Equal(t, true, late[fp.id])
	assert.Equal(t, false, late[ok.id])
}

func TestEnviosSimultaneosGeramUmSoEnvio(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("900/2026")
	v := fp.current(p.maria)
	var wg sync.WaitGroup
	statuses := make(chan int, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- p.do("POST", "/api/admin/projects/"+fp.id+"/submit", map[string]any{"version": v, "declaration": true}, p.maria).Status
		}()
	}
	wg.Wait()
	close(statuses)
	ok, conflict := 0, 0
	for s := range statuses {
		switch s {
		case 200:
			ok++
		case 409:
			conflict++
		}
	}
	assert.Equal(t, 1, ok)
	assert.Equal(t, 5, conflict)
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_versions WHERE project_id = $1", fp.id).Scan(&n))
	assert.Equal(t, 1, n)
	var last int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT last_number FROM project_code_counters WHERE year = $1", cuiabaYear()).Scan(&last))
	assert.Equal(t, 1, last, "um único código foi gerado")
}

func TestListagemMostraCodigoEFiltraEnviados(t *testing.T) {
	p := newProjEnv(t)
	sent := p.completeProject("1000/2026")
	draft := p.completeProject("1001/2026")
	code := sent.submit(p.maria, true).data()["code"].(string)

	all := p.do("GET", "/api/admin/projects", nil, p.maria).list()
	assert.Len(t, all, 2)
	only := p.do("GET", "/api/admin/projects?status=submitted", nil, p.maria).list()
	require.Len(t, only, 1)
	row := only[0].(map[string]any)
	assert.Equal(t, sent.id, row["id"])
	assert.Equal(t, code, row["code"])
	assert.NotNil(t, row["submittedAt"])
	d := p.do("GET", "/api/admin/projects?status=draft", nil, p.maria).list()
	require.Len(t, d, 1)
	assert.Equal(t, draft.id, d[0].(map[string]any)["id"])
	assert.Empty(t, p.do("GET", "/api/admin/projects?status=archived", nil, p.maria).list())
	assert.Equal(t, 422, p.do("GET", "/api/admin/projects?status=cancelled", nil, p.maria).Status)
}

func TestEnvioSoDoTitular(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("1100/2026")
	v := fp.current(p.maria)
	body := map[string]any{"version": v, "declaration": true}
	assert.Equal(t, 403, p.do("POST", "/api/admin/projects/"+fp.id+"/submit", body, p.joao).Status)
	p.mkRole("editor-projetos", "admin::projects.read", "admin::projects.update")
	staff, _ := p.newAdmin(p.boss, "bia@exemplo.com", "editor-projetos")
	assert.Equal(t, 403, p.do("POST", "/api/admin/projects/"+fp.id+"/submit", body, staff).Status, "quem edita não envia sem a permissão de enviar")
	assert.Equal(t, 404, p.do("POST", "/api/admin/projects/00000000-0000-0000-0000-000000000000/submit", body, p.maria).Status)
}
