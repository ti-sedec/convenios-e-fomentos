package app_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/preview"
)

// fakeConverter faz as vezes do serviço de conversão de Word para PDF.
type fakeConverter struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (c *fakeConverter) ToPDF(_ context.Context, _ string, src io.Reader) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.fail {
		return nil, preview.ErrUnavailable
	}
	_, _ = io.Copy(io.Discard, src)
	return []byte("%PDF-1.7 convertido"), nil
}

func (c *fakeConverter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func summaryOf(r resp) map[string]any {
	rv, _ := r.data()["review"].(map[string]any)
	return rv["summary"].(map[string]any)
}

func TestReprovarItemComJustificativa(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp := p.submitted("9801/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	file := fileIDOf(reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"])

	t.Run("exige justificativa e decisão válida", func(t *testing.T) {
		require.Equal(t, 204, p.viewFile(staff.analyst, fp.id, file))
		assert.Equal(t, 422, p.reject(staff.analyst, fp.id, "document", "requerimento_apoio", "   ").Status)
		assert.Equal(t, 422, p.reject(staff.analyst, fp.id, "document", "requerimento_apoio", strings.Repeat("x", 501)).Status)
		bad := p.do("PUT", "/api/admin/projects/"+fp.id+"/review/items", map[string]any{"kind": "document", "target": "requerimento_apoio", "decision": "talvez"}, staff.analyst)
		assert.Equal(t, 422, bad.Status)
	})
	t.Run("reprovar exige ter visto o arquivo", func(t *testing.T) {
		other, _ := p.newAdmin(p.boss, "m.analista3@exemplo.com", "analyst")
		mine := p.submitted("9802/2026")
		require.Equal(t, 200, p.startAnalysis(other, mine.id).Status)
		r := p.reject(other, mine.id, "document", "requerimento_apoio", "Ilegível.")
		assert.Equal(t, "file_not_opened", r.errCode())
	})
	t.Run("só ver basta para reprovar, mas aprovar exige ler até o fim", func(t *testing.T) {
		other, _ := p.newAdmin(p.boss, "m.analista4@exemplo.com", "analyst")
		mine := p.submitted("9803/2026")
		require.Equal(t, 200, p.startAnalysis(other, mine.id).Status)
		otherFile := fileIDOf(reviewItems(p.project(other, mine.id))["document:requerimento_apoio"])
		require.Equal(t, 204, p.viewFile(other, mine.id, otherFile))
		r := p.review(other, mine.id, "document", "requerimento_apoio", true)
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, "file_not_read", r.errCode())
		assert.Equal(t, 200, p.reject(other, mine.id, "document", "requerimento_apoio", "Primeira página já mostra o erro.").Status)
		require.Equal(t, 200, p.review(other, mine.id, "document", "requerimento_apoio", false).Status)
		require.Equal(t, 204, p.do("POST", docsPath(mine.id, "/files/", otherFile, "/read"), nil, other).Status)
		assert.Equal(t, 200, p.review(other, mine.id, "document", "requerimento_apoio", true).Status)
		it := reviewItems(p.project(other, mine.id))["document:requerimento_apoio"]
		assert.Equal(t, true, it["files"].([]any)[0].(map[string]any)["read"])
		require.Equal(t, 200, p.review(other, mine.id, "document", "requerimento_apoio", false).Status)
	})
	t.Run("reprova, mostra a justificativa e conta à parte", func(t *testing.T) {
		r := p.reject(staff.analyst, fp.id, "document", "requerimento_apoio", "  Documento ilegível.  ")
		require.Equal(t, 200, r.Status, r.Body)
		it := reviewItems(r)["document:requerimento_apoio"]
		assert.Equal(t, "rejected", it["status"])
		assert.Equal(t, "Documento ilegível.", it["note"])
		assert.NotEmpty(t, it["reviewedBy"])
		assert.EqualValues(t, 1, summaryOf(r)["rejected"])
		assert.False(t, allApproved(r))
	})
	t.Run("reprovado impede indicar o departamento mesmo com o resto aprovado", func(t *testing.T) {
		r := p.approveAllExcept(staff.analyst, fp.id, "document:requerimento_apoio")
		assert.False(t, allApproved(r), "um item reprovado basta para travar")
		blocked := p.assign(staff.analyst, fp.id, p.deptID("Financeiro"))
		assert.Equal(t, "review_incomplete", blocked.errCode())
		assert.EqualValues(t, 1, blocked.Body["error"].(map[string]any)["details"].(map[string]any)["rejected"])
		assert.EqualValues(t, 1, summaryOf(r)["rejected"])
		assert.EqualValues(t, 0, summaryOf(r)["pending"])
	})
	t.Run("aprovar depois substitui a reprovação e apaga a justificativa", func(t *testing.T) {
		require.Equal(t, 204, p.openFile(staff.analyst, fp.id, file), "ler até o fim")
		r := p.review(staff.analyst, fp.id, "document", "requerimento_apoio", true)
		require.Equal(t, 200, r.Status, r.Body)
		it := reviewItems(r)["document:requerimento_apoio"]
		assert.Equal(t, "approved", it["status"])
		assert.Empty(t, it["note"])
		assert.True(t, allApproved(r))
	})
	t.Run("desfazer volta a pendente", func(t *testing.T) {
		require.Equal(t, 200, p.reject(staff.analyst, fp.id, "document", "requerimento_apoio", "De novo.").Status)
		r := p.review(staff.analyst, fp.id, "document", "requerimento_apoio", false)
		require.Equal(t, 200, r.Status, r.Body)
		it := reviewItems(r)["document:requerimento_apoio"]
		assert.Equal(t, "pending", it["status"])
		assert.Empty(t, it["note"])
	})
	t.Run("seção também pode ser reprovada, e quem não confere não reprova", func(t *testing.T) {
		assert.Equal(t, 200, p.reject(staff.analyst, fp.id, "section", "proposal", "O objeto não confere.").Status)
		for name, tok := range map[string]string{"supervisor": staff.supervisor, "revisor": staff.reviewerSame, "secretário": staff.secretary, "proponente": p.maria} {
			assert.Equal(t, 403, p.reject(tok, fp.id, "section", "proposal", "Não posso.").Status, name)
		}
	})
	t.Run("a devolução ao proponente limpa a reprovação do item devolvido", func(t *testing.T) {
		require.Equal(t, 200, p.complement(staff.analyst, fp.id, map[string]any{"items": []any{section("proposal", "Corrija o objeto.")}}).Status)
		it := reviewItems(p.project(staff.analyst, fp.id))["section:proposal"]
		assert.Equal(t, "pending", it["status"])
		assert.Empty(t, it["note"])
	})
}

func TestDocumentoAdicionalSoPodeSerAprovado(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fp, extra := p.submittedWithExtra("9802/2026")
	require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
	assert.Equal(t, 422, p.reject(staff.analyst, fp.id, "extra", extra, "Não serve.").Status)
}

// approveAllExcept abre e aprova todos os itens conferíveis, menos o de chave "tipo:alvo" dada.
func (p *projEnv) approveAllExcept(tok, id, skip string) resp {
	p.t.Helper()
	last := p.project(tok, id)
	for key, it := range reviewItems(last) {
		if it["blocking"] == true || key == skip {
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

// submittedWithDocx é um projeto enviado cujo requerimento é um arquivo do Word.
func (p *projEnv) submittedWithDocx(sigcon string) (*fundingProject, string) {
	p.t.Helper()
	const docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	docx := append([]byte("PK\x03\x04"), make([]byte, 200)...)
	fp := p.completeProject(sigcon)
	r := fp.do("POST", docsPath(fp.id, "/requerimento_apoio/files"), map[string]any{"filename": "requerimento.docx", "contentType": docxType, "sizeBytes": len(docx)}, p.maria)
	require.Equal(p.t, 201, r.Status, r.Body)
	file := r.data()["file"].(map[string]any)["id"].(string)
	key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
	fp.store.Put(key, docxType, docx)
	require.Equal(p.t, 200, fp.do("POST", docsPath(fp.id, "/files/", file, "/confirm"), nil, p.maria).Status)
	require.Equal(p.t, 200, p.do("PUT", docsPath(fp.id, "/requerimento_apoio/justification"), map[string]any{"text": ""}, p.maria).Status)
	require.Equal(p.t, 200, fp.submit(p.maria, true).Status)
	return fp, file
}

func TestExibicaoDosDocumentos(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	previewOf := func(tok, id, file string) resp {
		return p.do("GET", docsPath(id, "/files/", file, "/preview"), nil, tok)
	}

	t.Run("PDF é exibido como está, sem baixar e sem conversão", func(t *testing.T) {
		fp := p.submitted("9811/2026")
		require.Equal(t, 200, p.startAnalysis(staff.analyst, fp.id).Status)
		file := fileIDOf(reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"])
		r := previewOf(staff.analyst, fp.id, file)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Equal(t, "pdf", r.data()["kind"])
		assert.Equal(t, "application/pdf", r.data()["contentType"])
		assert.Equal(t, false, r.data()["converted"])
		assert.Contains(t, r.data()["url"], "inline=1")
		assert.Equal(t, 0, p.conv.count())
		// Pedir a URL não conta como abrir: só o registro de exibição conta.
		assert.Equal(t, "file_not_opened", p.review(staff.analyst, fp.id, "document", "requerimento_apoio", true).errCode())
	})
	t.Run("o titular também vê, e o acesso fica na auditoria", func(t *testing.T) {
		fp := p.submitted("9812/2026")
		file := fileIDOf(reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"])
		assert.Equal(t, 200, previewOf(p.maria, fp.id, file).Status)
		var n int
		require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.file-preview' AND entity_id = $1", fp.id).Scan(&n))
		assert.Equal(t, 1, n)
	})
	t.Run("quem não lê o projeto não vê", func(t *testing.T) {
		fp := p.submitted("9813/2026")
		file := fileIDOf(reviewItems(p.project(staff.analyst, fp.id))["document:requerimento_apoio"])
		assert.Equal(t, 404, previewOf(p.joao, fp.id, file).Status)
		assert.Equal(t, 404, p.do("POST", docsPath(fp.id, "/files/", file, "/viewed"), nil, p.joao).Status)
	})
	t.Run("Word é convertido uma vez e o original continua baixável", func(t *testing.T) {
		before := p.conv.count()
		fp, file := p.submittedWithDocx("9814/2026")
		r := previewOf(staff.analyst, fp.id, file)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Equal(t, "pdf", r.data()["kind"])
		assert.Equal(t, true, r.data()["converted"])
		assert.Contains(t, r.data()["url"], "previews/"+file+".pdf")
		assert.Equal(t, before+1, p.conv.count())
		assert.True(t, p.store.Has("previews/"+file+".pdf"))
		assert.Equal(t, 200, previewOf(staff.analyst, fp.id, file).Status)
		assert.Equal(t, before+1, p.conv.count(), "a segunda exibição usa o PDF guardado")

		d := p.do("GET", docsPath(fp.id, "/files/", file, "/download"), nil, staff.analyst)
		require.Equal(t, 200, d.Status)
		assert.Contains(t, d.data()["url"], "requerimento.docx", "o download é do arquivo original")
	})
	t.Run("conversão que falha responde 422 e não deixa PDF pela metade", func(t *testing.T) {
		fp, file := p.submittedWithDocx("9815/2026")
		p.conv.mu.Lock()
		p.conv.fail = true
		p.conv.mu.Unlock()
		defer func() { p.conv.mu.Lock(); p.conv.fail = false; p.conv.mu.Unlock() }()
		r := previewOf(staff.analyst, fp.id, file)
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, "preview_unavailable", r.errCode())
		assert.False(t, p.store.Has("previews/"+file+".pdf"))
	})
	t.Run("excluir o arquivo apaga o PDF gerado", func(t *testing.T) {
		// Arquivo do Word ainda em rascunho: a exclusão remove o objeto e o PDF de exibição.
		fp := p.completeProject("9816/2026")
		const docxType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		docx := append([]byte("PK\x03\x04"), make([]byte, 50)...)
		r := fp.do("POST", docsPath(fp.id, "/requerimento_apoio/files"), map[string]any{"filename": "r.docx", "contentType": docxType, "sizeBytes": len(docx)}, p.maria)
		require.Equal(t, 201, r.Status, r.Body)
		file := r.data()["file"].(map[string]any)["id"].(string)
		fp.store.Put(r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string), docxType, docx)
		require.Equal(t, 200, fp.do("POST", docsPath(fp.id, "/files/", file, "/confirm"), nil, p.maria).Status)
		require.Equal(t, 200, previewOf(p.maria, fp.id, file).Status)
		require.True(t, p.store.Has("previews/"+file+".pdf"))
		require.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", file), nil, p.maria).Status)
		assert.False(t, p.store.Has("previews/"+file+".pdf"))
	})
}
