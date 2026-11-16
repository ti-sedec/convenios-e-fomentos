package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/pdfbuild"
	"github.com/wagner/convenios-e-fomentos/backend/internal/project"
)

func realPDF(t *testing.T, pages int, protect string) []byte {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	if protect != "" {
		pdf.SetProtection(fpdf.CnProtectPrint, protect, "dono-secreto")
	}
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 12)
		pdf.Text(20, 30, fmt.Sprintf("conteudo da pagina %d", i))
	}
	var b bytes.Buffer
	require.NoError(t, pdf.Output(&b))
	return b.Bytes()
}

// hostilePDFBytes é um PDF com abertura em JavaScript e abertura de programa.
func hostilePDFBytes() []byte {
	var b bytes.Buffer
	offs := map[int]int{}
	obj := func(n int, body string) {
		offs[n] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R /OpenAction 5 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << >> /Annots [6 0 R] >>")
	obj(4, "<< /Length 0 >>\nstream\n\nendstream")
	obj(5, "<< /S /JavaScript /JS (app.alert\\('x'\\);) >>")
	obj(6, "<< /Type /Annot /Subtype /Link /Rect [50 700 300 730] /Border [0 0 1] /A << /S /Launch /F (cmd.exe) >> >>")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 7\n0000000000 65535 f \n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offs[i])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", x)
	return b.Bytes()
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for x := 0; x < 400; x++ {
		for y := 0; y < 300; y++ {
			img.Set(x, y, color.RGBA{240, 240, 200, 255})
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

// uploadAs anexa ao documento um arquivo com o conteúdo e o tipo dados. Devolve false se o item não aceita arquivo.
func (f *fundingProject) uploadAs(code, filename, ctype string, content []byte) bool {
	f.t.Helper()
	r := f.do("POST", docsPath(f.id, "/", code, "/files"), map[string]any{"filename": filename, "contentType": ctype, "sizeBytes": len(content)}, f.maria)
	if r.Status != 201 {
		return false
	}
	file := r.data()["file"].(map[string]any)["id"].(string)
	key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
	f.store.Put(key, ctype, content)
	require.Equal(f.t, 200, f.do("POST", docsPath(f.id, "/files/", file, "/confirm"), nil, f.maria).Status)
	require.Equal(f.t, 200, f.do("PUT", docsPath(f.id, "/", code, "/justification"), map[string]any{"text": ""}, f.maria).Status)
	return true
}

const docxCT = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// fullReportEnv é um projeto enviado, com arquivos de vários tipos (inclusive defeituosos) e com o enquadramento confirmado.
type fullReportEnv struct {
	*framingEnv
	good3, docx2 int // páginas dos PDFs de verdade
}

func newFullReportEnv(t *testing.T, sigcon string) *fullReportEnv {
	t.Helper()
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	p.finalistic("Financeiro", "Jurídico")
	f := &framingEnv{projEnv: p, staff: p.newStaff(), fin: p.deptID("Financeiro"), jur: p.deptID("Jurídico")}
	fp := p.completeProject(sigcon)
	f.fp = fp
	p.conv.pdf = realPDF(t, 2, "")

	items, _ := fp.checklist(p.maria)
	var codes []string
	for k := range items {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	type up struct {
		name, ctype string
		body        []byte
	}
	plan := []up{
		{"estatuto.pdf", "application/pdf", realPDF(t, 3, "")},
		{"foto.png", "image/png", pngBytes(t)},
		{"requerimento.docx", docxCT, append([]byte("PK\x03\x04"), make([]byte, 100)...)},
		{"quebrado.pdf", "application/pdf", append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("lixo "), 200)...)},
		{"protegido.pdf", "application/pdf", realPDF(t, 2, "senha")},
		{"hostil.pdf", "application/pdf", hostilePDFBytes()},
	}
	i := 0
	for _, code := range codes {
		if i >= len(plan) {
			break
		}
		if fp.uploadAs(code, plan[i].name, plan[i].ctype, plan[i].body) {
			i++
		}
	}
	require.Equal(t, len(plan), i, "o checklist tem itens suficientes para anexar os arquivos de teste")
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	require.Equal(t, 200, p.startAnalysis(f.staff.analyst, fp.id).Status)
	require.True(t, allApproved(p.approveAll(f.staff.analyst, fp.id)))
	require.Equal(t, 200, f.assign(f.staff.analyst, fp.id, f.fin).Status)
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, fp.id, 1).Status)
	return &fullReportEnv{framingEnv: f, good3: 3, docx2: 2}
}

func (e *fullReportEnv) process(t *testing.T) {
	t.Helper()
	did, err := e.app.Projects.ProcessNextReport(context.Background())
	require.NoError(t, err)
	require.True(t, did, "havia uma montagem na fila")
}

func TestRelatorioCompletoMontaOPDFComTudo(t *testing.T) {
	e := newFullReportEnv(t, "9501-2026")
	id := e.fp.id
	number := e.project(e.maria, id).data()["code"].(string)

	r := e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.maria)
	require.Equal(t, 202, r.Status, r.Body)
	rec := r.data()
	assert.Equal(t, "queued", rec["status"])
	assert.Equal(t, "full", rec["kind"])
	assert.Equal(t, true, rec["coverSheets"], "folha de rosto marcada por padrão")
	assert.Equal(t, true, rec["pageNumbers"], "numeração marcada por padrão")
	rid := rec["id"].(string)

	// Uma montagem por vez no projeto.
	assert.Equal(t, "report_in_progress", e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.maria).errCode())
	// Ainda não há PDF.
	assert.Equal(t, "report_not_ready", e.do("GET", reportsPath(id, "/", rid, "/download"), nil, e.maria).errCode())

	e.process(t)

	got := e.do("GET", reportsPath(id, "/", rid), nil, e.maria)
	require.Equal(t, 200, got.Status, got.Body)
	d := got.data()
	require.Equal(t, "done", d["status"], got.Body)
	assert.Equal(t, true, d["downloadable"])
	warnings := d["warnings"].([]any)
	var reasons []string
	for _, w := range warnings {
		reasons = append(reasons, w.(map[string]any)["reason"].(string))
	}
	assert.Len(t, warnings, 2, "só o PDF quebrado e o protegido ficam de fora: %v", reasons)
	assert.Contains(t, reasons, "O arquivo está corrompido ou não é um PDF válido.")
	assert.Contains(t, reasons, "O arquivo está protegido por senha.")
	stats := d["stats"].(map[string]any)
	assert.EqualValues(t, 6, stats["files"])
	assert.Greater(t, stats["peakHeapBytes"], float64(0), "o pico de memória é medido")

	// Baixa o PDF montado, em fluxo, e confere por dentro.
	status, hdr, pdf := e.rawJSON("GET", reportsPath(id, "/", rid, "/download"), e.maria, nil)
	require.Equal(t, 200, status)
	assert.Equal(t, "application/pdf", hdr.Get("Content-Type"))
	assert.Equal(t, strconv.Itoa(len(pdf)), hdr.Get("Content-Length"))
	assert.Contains(t, hdr.Get("Content-Disposition"), "relatorio-"+number+"-completo-")
	sum := sha256.Sum256(pdf)
	assert.Equal(t, hex.EncodeToString(sum[:]), d["sha256"])
	assert.EqualValues(t, len(pdf), d["sizeBytes"])

	path := filepath.Join(t.TempDir(), "relatorio.pdf")
	require.NoError(t, os.WriteFile(path, pdf, 0o600))
	pages, err := pdfbuild.PageCount(context.Background(), path)
	require.NoError(t, err)
	assert.EqualValues(t, pages, d["pageCount"])
	// 6 arquivos: 3 + 1 (imagem) + 2 (word) + 1 (aviso) + 1 (aviso) + 1 (hostil) = 9 páginas de arquivos e 6 folhas de rosto, mais o relatório.
	assert.Greater(t, pages, 9+6)
	active, err := pdfbuild.ActiveContent(context.Background(), path)
	require.NoError(t, err)
	assert.Empty(t, active, "o PDF hostil enviado foi saneado")

	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	bms, err := api.ListBookmarks(context.Background(), f, model.NewDefaultConfiguration())
	require.NoError(t, err)
	flat := fmt.Sprint(bms)
	for _, want := range []string{"Relatório do projeto", "Identificação", "Sumário dos documentos", "Documentos", "Declaração de veracidade", "›"} {
		assert.Contains(t, flat, want)
	}

	// Auditoria e histórico.
	var gen, down int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.report-generate' AND entity_id = $1", id).Scan(&gen))
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action = 'project.report-download' AND entity_id = $1", id).Scan(&down))
	assert.Equal(t, 1, gen)
	assert.Equal(t, 1, down)
	list := e.do("GET", reportsPath(id), nil, e.maria).list()
	require.Len(t, list, 1)
	assert.Equal(t, "done", list[0].(map[string]any)["status"])

	// Depois do prazo o PDF some, mas o registro e os dados usados ficam.
	_, err = e.pool.Exec(context.Background(), "UPDATE project_reports SET expires_at = now() - interval '1 minute' WHERE id = $1", rid)
	require.NoError(t, err)
	assert.Equal(t, "report_expired", e.do("GET", reportsPath(id, "/", rid, "/download"), nil, e.maria).errCode())
	after := e.do("GET", reportsPath(id, "/", rid), nil, e.maria)
	assert.Equal(t, false, after.data()["downloadable"])
	assert.NotNil(t, after.data()["data"], "os dados usados permanecem")

	// Pode gerar outro depois que o anterior terminou.
	assert.Equal(t, 202, e.do("POST", reportsPath(id), map[string]any{"kind": "full", "coverSheets": false, "pageNumbers": false}, e.maria).Status)
}

func TestRelatorioCompletoSemFolhaDeRostoENumeracao(t *testing.T) {
	e := newFullReportEnv(t, "9601-2026")
	id := e.fp.id
	pagesOf := func(body map[string]any) int {
		r := e.do("POST", reportsPath(id), body, e.maria)
		require.Equal(t, 202, r.Status, r.Body)
		e.process(t)
		rid := r.data()["id"].(string)
		_, _, pdf := e.rawJSON("GET", reportsPath(id, "/", rid, "/download"), e.maria, nil)
		path := filepath.Join(t.TempDir(), "r.pdf")
		require.NoError(t, os.WriteFile(path, pdf, 0o600))
		n, err := pdfbuild.PageCount(context.Background(), path)
		require.NoError(t, err)
		return n
	}
	with := pagesOf(map[string]any{"kind": "full"})
	without := pagesOf(map[string]any{"kind": "full", "coverSheets": false, "pageNumbers": false})
	assert.Equal(t, 6, with-without, "uma folha de rosto por arquivo (6 arquivos)")
}

func TestRelatorioCompletoLimiteEFalhas(t *testing.T) {
	e := newFullReportEnv(t, "9701-2026")
	id := e.fp.id
	ctx := context.Background()

	// Acima do limite de entrada: recusa na hora, com a explicação.
	e.app.Projects.SetReports(project.ReportConfig{MaxInputBytes: 10, TmpDir: t.TempDir()})
	r := e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.maria)
	assert.Equal(t, 422, r.Status)
	assert.Equal(t, "report_too_large", r.errCode())

	// Limite de páginas: a montagem falha com mensagem clara e o registro fica como failed.
	e.app.Projects.SetReports(project.ReportConfig{MaxPages: 3, TmpDir: t.TempDir()})
	r = e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.maria)
	require.Equal(t, 202, r.Status, r.Body)
	e.process(t)
	got := e.do("GET", reportsPath(id, "/", r.data()["id"].(string)), nil, e.maria).data()
	assert.Equal(t, "failed", got["status"])
	assert.Contains(t, got["error"], "limite de 3 páginas")
	assert.Equal(t, false, got["downloadable"])

	// Uma montagem interrompida (o processo morreu) é encerrada como falha, não refeita sozinha.
	e.app.Projects.SetReports(project.ReportConfig{TmpDir: t.TempDir()})
	r = e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.maria)
	require.Equal(t, 202, r.Status, r.Body)
	rid := r.data()["id"].(string)
	_, err := e.pool.Exec(ctx, "UPDATE project_reports SET status = 'running', heartbeat_at = now() - interval '10 minutes' WHERE id = $1", rid)
	require.NoError(t, err)
	e.app.Projects.RunReportMaintenanceForTest(ctx)
	got = e.do("GET", reportsPath(id, "/", rid), nil, e.maria).data()
	assert.Equal(t, "failed", got["status"])
	assert.Contains(t, got["error"], "interrompida")
}

func TestRelatorioCompletoSoParaQuemPodeGerar(t *testing.T) {
	e := newFullReportEnv(t, "9801-2026")
	id := e.fp.id
	assert.Equal(t, 404, e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.joao).Status, "outro proponente")
	assert.Equal(t, 404, e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.staff.reviewerOther).Status)
	assert.Equal(t, 202, e.do("POST", reportsPath(id), map[string]any{"kind": "full"}, e.staff.analyst).Status)
	// Quem não alcança o projeto também não baixa nem lista.
	rid := e.do("GET", reportsPath(id), nil, e.maria).list()[0].(map[string]any)["id"].(string)
	assert.Equal(t, 404, e.do("GET", reportsPath(id, "/", rid, "/download"), nil, e.joao).Status)
}
