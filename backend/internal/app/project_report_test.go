package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawJSON é como raw, mas com corpo JSON (as rotas de relatório respondem PDF, não o envelope JSON).
func (e *env) rawJSON(method, path, token string, body any) (int, http.Header, []byte) {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(e.t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.1:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

// reportEnv é um projeto enviado com arquivos, assumido, conferido e com o departamento indicado (enquadramento pendente).
func newReportEnv(t *testing.T, sigcon string) (*framingEnv, string) {
	t.Helper()
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	f := &framingEnv{projEnv: p, staff: p.newStaff(), fin: p.deptID("Financeiro"), jur: p.deptID("Jurídico")}
	fp, extra := p.submittedWithExtra(sigcon)
	f.fp = fp
	require.Equal(t, 200, p.startAnalysis(f.staff.analyst, fp.id).Status)
	require.True(t, allApproved(p.approveAll(f.staff.analyst, fp.id)))
	require.Equal(t, 200, f.assign(f.staff.analyst, fp.id, f.fin).Status)
	return f, extra
}

func reportsPath(id string, rest ...string) string {
	return "/api/admin/projects/" + id + "/reports" + strings.Join(rest, "")
}

func TestRelatorioSoDepoisDoEnquadramentoConfirmado(t *testing.T) {
	f, _ := newReportEnv(t, "9101-2026")
	id := f.fp.id

	// Rascunho: sem relatório. Enviado e com enquadramento pendente: indisponível, com o motivo, para todos os perfis.
	draft := f.completeProject("9100-2026")
	code, _, _ := f.rawJSON("POST", reportsPath(draft.id), f.maria, map[string]any{"kind": "summary"})
	assert.Equal(t, 404, code, "rascunho não tem relatório")
	assert.NotContains(t, f.project(f.maria, draft.id).data(), "report")

	for name, tok := range map[string]string{"titular": f.maria, "analista": f.staff.analyst, "revisor": f.staff.reviewerSame} {
		got := f.project(tok, id).data()
		rep, _ := got["report"].(map[string]any)
		require.NotNil(t, rep, name)
		assert.Equal(t, false, rep["available"], name)
		assert.Contains(t, rep["reason"], "confirma o enquadramento", name)
		code, _, body := f.rawJSON("POST", reportsPath(id), tok, map[string]any{"kind": "summary"})
		assert.Equal(t, 409, code, name)
		assert.Contains(t, string(body), "report_unavailable", name)
	}

	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, id, 1).Status)
	for name, tok := range map[string]string{"titular": f.maria, "analista": f.staff.analyst} {
		rep := f.project(tok, id).data()["report"].(map[string]any)
		assert.Equal(t, true, rep["available"], name)
		assert.Empty(t, rep["reason"], name)
	}
}

func TestGerarRelatorioResumido(t *testing.T) {
	f, extra := newReportEnv(t, "9201-2026")
	id := f.fp.id
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, id, 1).Status)
	number := f.project(f.maria, id).data()["code"].(string)

	status, hdr, pdf := f.rawJSON("POST", reportsPath(id), f.maria, map[string]any{"kind": "summary"})
	require.Equal(t, 200, status, string(pdf))
	assert.Equal(t, "application/pdf", hdr.Get("Content-Type"))
	assert.Equal(t, "private, no-store", hdr.Get("Cache-Control"))
	assert.Contains(t, hdr.Get("Content-Disposition"), "relatorio-"+number+"-resumido-")
	rid := hdr.Get("X-Report-Id")
	require.NotEmpty(t, rid)
	require.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
	text := latin1(pdf) // o texto do PDF usa Latin-1
	for _, want := range []string{number, "Relatório resumido", "Pavimenta", "9201-2026", "11.222.333/0001-81", "Financeiro", "Foto da obra", "SHA-256", "Declara", "Página 1 de"} {
		assert.Contains(t, text, want)
	}
	assert.NotContains(t, text, cpfFor("maria@exemplo.com"), "o CPF vai mascarado")
	assert.Contains(t, text, "***.", "CPF mascarado")

	// O registro guarda quem gerou, quando, as opções e os dados usados; o PDF não é guardado.
	list := f.do("GET", reportsPath(id), nil, f.maria).list()
	require.Len(t, list, 1)
	rec := list[0].(map[string]any)
	assert.Equal(t, rid, rec["id"])
	assert.Equal(t, "summary", rec["kind"])
	assert.Equal(t, true, rec["pageNumbers"])
	assert.NotEmpty(t, rec["generatedBy"])
	assert.NotContains(t, rec, "data", "a lista não traz os dados")
	assert.NotContains(t, fmtJSON(rec), cpfFor("maria@exemplo.com"), "o CPF de quem gerou vai mascarado")
	sum := sha256.Sum256(pdf)
	assert.Equal(t, hex.EncodeToString(sum[:]), rec["sha256"])

	detail := f.do("GET", reportsPath(id, "/", rid), nil, f.maria)
	require.Equal(t, 200, detail.Status)
	data := detail.data()["data"].(map[string]any)
	assert.Equal(t, number, data["code"])
	assert.NotContains(t, fmtJSON(data), cpfFor("maria@exemplo.com"), "os dados guardados não têm o CPF completo")
	assert.NotContains(t, fmtJSON(data), "objectKey", "chaves de armazenamento não entram")
	var extraSeen bool
	for _, d := range data["documents"].([]any) {
		if d.(map[string]any)["extra"] == true {
			extraSeen = true
		}
	}
	assert.True(t, extraSeen, "o documento adicional está listado")
	_ = extra

	// Refeito do dado guardado, o PDF sai idêntico byte a byte.
	status, hdr2, again := f.rawJSON("GET", reportsPath(id, "/", rid, "/download"), f.maria, nil)
	require.Equal(t, 200, status)
	assert.Equal(t, pdf, again, "o PDF refeito é idêntico ao gerado")
	assert.Equal(t, hdr.Get("Content-Disposition"), hdr2.Get("Content-Disposition"))

	// Sem numeração: o rodapé fica só com a data.
	no := false
	_, _, plain := f.rawJSON("POST", reportsPath(id), f.maria, map[string]any{"kind": "summary", "pageNumbers": no})
	assert.NotContains(t, latin1(plain), "Página 1 de")
	assert.Contains(t, latin1(plain), "gerado em")
	assert.Len(t, f.do("GET", reportsPath(id), nil, f.maria).list(), 2)

	// Auditoria.
	var gen, down int
	require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.report-generate' AND entity_id = $1", id).Scan(&gen))
	require.NoError(t, f.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.report-download' AND entity_id = $1", id).Scan(&down))
	assert.Equal(t, 2, gen)
	assert.Equal(t, 1, down)
}

// latin1 decodifica o texto de um PDF gerado com as fontes padrão (um byte por caractere).
func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestRelatorioQuemPodeGerar(t *testing.T) {
	f, _ := newReportEnv(t, "9301-2026")
	id := f.fp.id
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, id, 1).Status)
	body := map[string]any{"kind": "summary"}

	// Equipe com alcance e o titular geram; membro do projeto também (D7).
	f.mustInvite(id, "leitor@exemplo.com", "read")
	member := f.login("leitor@exemplo.com")
	for name, tok := range map[string]string{"titular": f.maria, "membro (leitura)": member, "analista": f.staff.analyst,
		"supervisor": f.staff.supervisor, "secretário": f.staff.secretary, "revisor do departamento": f.staff.reviewerSame, "super admin": f.staff.superAdmin} {
		code, _, out := f.rawJSON("POST", reportsPath(id), tok, body)
		assert.Equal(t, 200, code, "%s: %s", name, out)
	}
	// Sem alcance: 404 (não revela que o projeto existe). Sem login: 401.
	for name, tok := range map[string]string{"outro proponente": f.joao, "revisor de outro departamento": f.staff.reviewerOther} {
		code, _, _ := f.rawJSON("POST", reportsPath(id), tok, body)
		assert.Equal(t, 404, code, name)
		code, _, _ = f.rawJSON("GET", reportsPath(id), tok, nil)
		assert.Equal(t, 404, code, name)
	}
	// Revisor sem departamento não alcança projeto algum (403, como na leitura do projeto).
	code, _, _ := f.rawJSON("POST", reportsPath(id), f.staff.reviewerNone, body)
	assert.Equal(t, 403, code)
	code, _, _ = f.rawJSON("POST", reportsPath(id), "", body)
	assert.Equal(t, 401, code)

	// Quem lê o projeto mas não tem a ação de relatório recebe 403.
	f.mkRole("leitor-sem-relatorio", "admin::projects.read")
	reader, _ := f.newAdmin(f.boss, "bia@exemplo.com", "leitor-sem-relatorio")
	code, _, _ = f.rawJSON("POST", reportsPath(id), reader, body)
	assert.Equal(t, 403, code)

	// O histórico mostra todas as gerações, de quem gerou cada uma.
	list := f.do("GET", reportsPath(id), nil, f.maria).list()
	assert.Len(t, list, 7)

	// Tipo de relatório inválido.
	for _, k := range []string{"", "xyz"} {
		r := f.do("POST", reportsPath(id), map[string]any{"kind": k}, f.maria)
		assert.Equal(t, 422, r.Status, "kind=%q", k)
	}
	// Relatório de outro projeto ou inexistente: 404.
	assert.Equal(t, 404, f.do("GET", reportsPath(id, "/00000000-0000-0000-0000-000000000000"), nil, f.maria).Status)
}

func TestRelatorioNaoSaiDeProjetoArquivado(t *testing.T) {
	f, _ := newReportEnv(t, "9401-2026")
	id := f.fp.id
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, id, 1).Status)
	_, _, pdf := f.rawJSON("POST", reportsPath(id), f.maria, map[string]any{"kind": "summary"})
	require.True(t, strings.HasPrefix(string(pdf), "%PDF-"))

	require.Equal(t, 200, f.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Negado por não atender ao interesse público."}, f.staff.secretary).Status)
	rep := f.project(f.maria, id).data()["report"].(map[string]any)
	assert.Equal(t, false, rep["available"])
	assert.Contains(t, rep["reason"], "arquivado")
	code, _, _ := f.rawJSON("POST", reportsPath(id), f.maria, map[string]any{"kind": "summary"})
	assert.Equal(t, 409, code)

	// O histórico continua legível e refaz o PDF mesmo depois do arquivamento.
	list := f.do("GET", reportsPath(id), nil, f.maria).list()
	require.Len(t, list, 1)
	code, _, again := f.rawJSON("GET", reportsPath(id, "/", list[0].(map[string]any)["id"].(string), "/download"), f.maria, nil)
	assert.Equal(t, 200, code)
	assert.Equal(t, pdf, again)
}
