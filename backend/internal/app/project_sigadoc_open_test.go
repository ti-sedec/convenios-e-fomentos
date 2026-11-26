package app_test

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
)

const (
	analystEmail = "m.analista@exemplo.com"
	goodPIN      = "24681357"
)

// bigPDF é um PDF de páginas pouco compressíveis, para passar do tamanho máximo de cada parte.
func bigPDF(t *testing.T, pages int) []byte {
	t.Helper()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetCompression(false)
	pdf.SetFont("Helvetica", "", 8)
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		for y := 10.0; y < 285; y += 3.5 {
			pdf.Text(8, y, fmt.Sprintf("p%d %0100d %0100d", i, i*7919+int(y), i*104729+int(y)))
		}
	}
	var b bytes.Buffer
	require.NoError(t, pdf.Output(&b))
	return b.Bytes()
}

// sigadocEnv é um projeto enviado, enquadrado e já assumido pelo analista (etapa "Abertura do processo no SIGADOC"), com o SIGADOC
// simulado configurado e a matrícula do analista conhecida.
type sigadocEnv struct {
	*framingEnv
	cpf string
}

type fileSpec struct {
	name, ctype string
	body        []byte
}

func newSigadocEnv(t *testing.T, sigcon string, files []fileSpec, maxPartMB int) *sigadocEnv {
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
	i := 0
	for _, code := range codes {
		if i >= len(files) {
			break
		}
		if fp.uploadAs(code, files[i].name, files[i].ctype, files[i].body) {
			i++
		}
	}
	require.Equal(t, len(files), i, "o checklist tem itens suficientes")
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	require.Equal(t, 200, p.startAnalysis(f.staff.analyst, fp.id).Status)
	require.True(t, allApproved(p.approveAll(f.staff.analyst, fp.id)))
	require.Equal(t, 200, f.assign(f.staff.analyst, fp.id, f.fin).Status)
	require.Equal(t, 200, f.confirm(f.staff.reviewerSame, fp.id, 1).Status)
	require.Equal(t, 200, p.startAnalysis(f.staff.analyst, fp.id).Status, "assume o projeto da fila")

	e := &sigadocEnv{framingEnv: f, cpf: cpfFor(analystEmail)}
	_, err := p.pool.Exec(context.Background(),
		`UPDATE rule_parameters SET value = jsonb_set(jsonb_set(value, '{termo_model_id}', '"42"'), '{max_part_mb}', to_jsonb($1::int)) WHERE key = 'sigadoc_settings'`, maxPartMB)
	require.NoError(t, err)
	p.sigadoc.SetMatriculas(e.cpf, "SEDEC111111")
	p.sigadoc.SetPIN(e.cpf, goodPIN)
	return e
}

func (e *sigadocEnv) state() resp { return e.sigadocState(e.staff.analyst, e.fp.id) }

func (e *sigadocEnv) open(tok, pin string, fields map[string]any) resp {
	return e.do("POST", "/api/admin/projects/"+e.fp.id+"/sigadoc/open", map[string]any{"pin": pin, "fields": fields}, tok)
}

func (e *sigadocEnv) resume(tok, pin string) resp {
	return e.do("POST", "/api/admin/projects/"+e.fp.id+"/sigadoc/resume", map[string]any{"pin": pin}, tok)
}

// wait espera a montagem sair de "em execução" e devolve a tarefa.
func (e *sigadocEnv) wait() map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		task, _ := e.state().data()["task"].(map[string]any)
		if task != nil && task["status"] != "running" {
			return task
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.t.Fatal("a montagem não terminou a tempo")
	return nil
}

func goodFiles(t *testing.T) []fileSpec {
	return []fileSpec{
		{"estatuto.pdf", "application/pdf", realPDF(t, 3, "")},
		{"foto.png", "image/png", pngBytes(t)},
		{"requerimento.docx", docxCT, append([]byte("PK\x03\x04"), make([]byte, 100)...)},
	}
}

func itemsOf(task map[string]any) []map[string]any {
	var out []map[string]any
	for _, it := range task["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func (e *sigadocEnv) descriptions() map[string]string {
	out := map[string]string{}
	for _, s := range e.sigadoc.Documents() {
		d, ty := e.sigadoc.Description(s)
		out[s] = d + "|" + ty
	}
	return out
}

func TestAbrirProcessoNoSigadocCaminhoFeliz(t *testing.T) {
	e := newSigadocEnv(t, "9601-2026", goodFiles(t), 24)
	id := e.fp.id

	t.Run("antes de abrir: botão disponível e nada no SIGADOC", func(t *testing.T) {
		d := e.state().data()
		op := d["opening"].(map[string]any)
		assert.Equal(t, true, op["available"], op["reason"])
		assert.Equal(t, true, op["termoReady"])
		assert.Nil(t, d["task"])
		assert.Empty(t, e.sigadoc.Documents())
	})
	t.Run("o Termo é pré-visualizado com os dados do projeto e os ajustes; nada é gravado", func(t *testing.T) {
		r := e.do("POST", "/api/admin/projects/"+id+"/sigadoc/termo", map[string]any{"fields": map[string]any{"docGovMT_resumoAssunto": "Título ajustado pelo analista"}}, e.staff.analyst)
		require.Equal(t, 200, r.Status, r.Body)
		html := r.data()["html"].(string)
		assert.Contains(t, html, "modelo 42")
		assert.Contains(t, html, "Título ajustado pelo analista")
		assert.Contains(t, html, "<dt>docGovMT_cpf-cnpj</dt>")
		assert.Equal(t, "Título ajustado pelo analista", r.data()["fields"].(map[string]any)["docGovMT_resumoAssunto"])
		assert.Empty(t, e.sigadoc.Documents(), "pré-visualizar não cria documento")
		assert.Zero(t, e.countRows("project_sigadoc"))
	})
	t.Run("PIN errado: recusado antes de começar, sem tarefa e sem documento", func(t *testing.T) {
		r := e.open(e.staff.analyst, "00000000", nil)
		assert.Equal(t, 422, r.Status)
		assert.Equal(t, "pin_rejected", r.errCode())
		assert.Zero(t, e.countRows("sigadoc_tasks"))
		assert.Empty(t, e.sigadoc.Documents())
		assert.NotContains(t, e.sigadoc.Calls(), "CreateDocument")
	})
	t.Run("quem não é o responsável não abre", func(t *testing.T) {
		other, _ := e.newAdmin(e.boss, "m.analista2@exemplo.com", "analyst")
		r := e.open(other, goodPIN, nil)
		assert.Equal(t, 409, r.Status)
		assert.Equal(t, "not_responsible_analyst", r.errCode())
		assert.Equal(t, 403, e.open(e.staff.reviewerSame, goodPIN, nil).Status, "o Revisor não tem a ação")
		assert.Equal(t, 403, e.open(e.maria, goodPIN, nil).Status)
		assert.Equal(t, 401, e.open("", goodPIN, nil).Status)
	})

	r := e.open(e.staff.analyst, goodPIN, map[string]any{"docGovMT_resumoAssunto": "Título ajustado pelo analista"})
	require.Equal(t, 202, r.Status, r.Body)
	task := e.wait()
	require.Equal(t, "completed", task["status"], task)

	t.Run("o processo foi aberto e o projeto aguarda o parecer", func(t *testing.T) {
		d := e.state().data()
		proc := d["process"].(map[string]any)
		assert.NotEmpty(t, proc["number"])
		assert.Regexp(t, `^SEDECMEM\d+A$`, proc["sigla"])
		v := e.project(e.staff.analyst, id)
		assert.Equal(t, "awaiting_opinion", v.data()["stage"])
		assert.Equal(t, "project.sigadoc-ready", e.history(id)[len(e.history(id))-1].Transition)
		assert.EqualValues(t, task["progressTotal"], task["progressDone"])
		assert.Equal(t, 1, e.countAudit("project.sigadoc-open", id))
		assert.Equal(t, 1, e.countAudit("project.sigadoc-ready", id))
	})
	t.Run("Termo, relatório e cada arquivo viraram documento, assinados e juntados ao processo", func(t *testing.T) {
		docs := e.descriptions()
		var termo, report, files []string
		for s, d := range docs {
			switch {
			case strings.HasPrefix(d, "Termo de Abertura"):
				termo = append(termo, s)
			case strings.HasSuffix(d, "|Interno Capturado"):
				report = append(report, s)
			case strings.HasSuffix(d, "|Externo Capturado"):
				files = append(files, s)
			}
		}
		require.Len(t, termo, 1)
		require.Len(t, report, 1)
		assert.Len(t, files, 3, "um documento por arquivo: PDF, imagem e Word, todos em PDF")
		proc := e.state().data()["process"].(map[string]any)["sigla"].(string)
		assert.Equal(t, termo[0], proc)
		assert.Equal(t, proc, e.sigadoc.Parent(report[0]), "o relatório foi juntado ao processo")
		for _, f := range files {
			assert.Equal(t, proc, e.sigadoc.Parent(f))
			d, _ := e.sigadoc.Description(f)
			assert.Contains(t, d, " — ", "«tipo — proponente ou emenda — código»")
			assert.Contains(t, d, str(e.project(e.staff.analyst, id).data()["code"]))
		}
		for _, it := range itemsOf(task) {
			assert.Equal(t, "joined", it["status"], it["label"])
		}
	})
	t.Run("o PIN não ficou em lugar nenhum", func(t *testing.T) {
		for _, q := range []string{
			"SELECT coalesce(string_agg(payload::text, ' '), '') FROM audit_logs",
			"SELECT coalesce(string_agg(error || error_code, ' '), '') FROM sigadoc_tasks",
			"SELECT coalesce(string_agg(error || payload::text || description || label, ' '), '') FROM sigadoc_items",
		} {
			var txt string
			require.NoError(t, e.pool.QueryRow(context.Background(), q).Scan(&txt))
			assert.NotContains(t, txt, goodPIN, q)
		}
		assert.NotContains(t, strings.Join(e.sigadoc.Calls(), " "), goodPIN)
	})
	t.Run("o acompanhamento passa a consultar o processo", func(t *testing.T) {
		require.Equal(t, 1, e.runWatch())
		assert.NotEmpty(t, movementsOf(e.state()), "as movimentações do processo entram no histórico")
	})
	t.Run("depois de pronto não se abre de novo", func(t *testing.T) {
		assert.Equal(t, "project_stage_invalid", e.open(e.staff.analyst, goodPIN, nil).errCode())
	})
}

func (e *sigadocEnv) countRows(table string) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

func str(v any) string { s, _ := v.(string); return s }

func TestFalhaNoMeioInterrompeEARetomadaContinua(t *testing.T) {
	e := newSigadocEnv(t, "9603-2026", goodFiles(t), 24)
	// Sign do Termo (1ª chamada) passa; a do relatório (2ª) falha.
	var n int
	e.sigadoc.FailNext("Finalize", &sigadoc.Error{Status: 500, Message: "SIGADOC fora do ar"}) // falha no Termo, antes de assinar
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "interrupted", task["status"], task)
	assert.Equal(t, "sigadoc_error", task["errorCode"])
	assert.Contains(t, task["error"], "SIGADOC fora do ar")
	assert.NotContains(t, fmt.Sprint(task), goodPIN)
	items := itemsOf(task)
	assert.Equal(t, "created", items[0]["status"], "o Termo foi criado e parou antes de finalizar")
	assert.Equal(t, "pending", items[1]["status"])

	// Estado: botão de abrir fechado, retomar aberto; o projeto continua na abertura.
	d := e.state().data()
	assert.Equal(t, false, d["opening"].(map[string]any)["available"])
	assert.Contains(t, d["opening"].(map[string]any)["reason"], "Retomar")
	assert.Equal(t, "opening_sigadoc", e.project(e.staff.analyst, e.fp.id).data()["stage"])
	assert.Equal(t, "sigadoc_task_interrupted", e.open(e.staff.analyst, goodPIN, nil).errCode(), "abrir de novo não: é retomar")
	assert.Equal(t, 1, len(e.sigadoc.Documents()), "só o Termo existe")

	// A retomada exige o PIN de novo e refaz só o que falta (não cria o Termo outra vez).
	assert.Equal(t, "pin_rejected", e.resume(e.staff.analyst, "99999999").errCode())
	require.Equal(t, 202, e.resume(e.staff.analyst, goodPIN).Status)
	task = e.wait()
	require.Equal(t, "completed", task["status"], task)
	assert.EqualValues(t, 2, task["attempt"])
	assert.Equal(t, 1+1+3, len(e.sigadoc.Documents()), "Termo + relatório + 3 arquivos, sem duplicar o Termo")
	n = 0
	for _, c := range e.sigadoc.Calls() {
		if c == "CreateDocument" {
			n++
		}
	}
	assert.Equal(t, 5, n, "cada documento foi criado uma vez só")
	assert.Equal(t, "awaiting_opinion", e.project(e.staff.analyst, e.fp.id).data()["stage"])
}

// O processo é registrado quando o Termo é autuado, antes do relatório. Se a montagem parar depois disso, a retomada precisa passar
// com o processo já registrado (antes era recusada com "sigadoc_process_exists") e juntar o que falta a esse mesmo processo.
func TestRetomadaDepoisDoTermoAutuadoContinuaNoMesmoProcesso(t *testing.T) {
	e := newSigadocEnv(t, "9613-2026", goodFiles(t), 24)
	e.sigadoc.FailNext("Sign", nil)                                                                  // o Termo assina
	e.sigadoc.FailNext("Sign", &sigadoc.Error{Status: 500, Message: "Erro ao registrar assinatura"}) // o relatório (Interno Capturado se assina)
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "interrupted", task["status"], task)
	items := itemsOf(task)
	assert.Equal(t, "joined", items[0]["status"], "o Termo foi autuado e juntado")
	assert.Equal(t, "finalized", items[1]["status"], "o relatório parou na assinatura")
	process := e.state().data()["process"]
	require.NotNil(t, process, "o processo já está registrado")

	assert.Equal(t, "sigadoc_process_exists", e.open(e.staff.analyst, goodPIN, nil).errCode()+"", "abrir de novo continua recusado")
	require.Equal(t, 202, e.resume(e.staff.analyst, goodPIN).Status, "retomar com o processo registrado precisa passar")
	task = e.wait()
	require.Equal(t, "completed", task["status"], task)
	assert.Equal(t, process, e.state().data()["process"], "o processo é o mesmo")
	assert.Equal(t, "awaiting_opinion", e.project(e.staff.analyst, e.fp.id).data()["stage"])
}

// A recusa por configuração do SIGADOC (permissão do usuário/lotação) interrompe a montagem com o motivo e o que fazer.
func TestRecusaPorConfiguracaoDoSigadocInterrompeComOMotivo(t *testing.T) {
	e := newSigadocEnv(t, "9614-2026", goodFiles(t), 24)
	e.sigadoc.FailNext("AutenticarDocumento", &sigadoc.Error{Status: 500, Message: "O documento X não pode ser autenticado com senha por A/B - Proibido porque não pode movimentar autenticação de documento com senha por configuração"}) // o 1º arquivo do proponente (Externo Capturado)
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "interrupted", task["status"], task)
	assert.Contains(t, task["error"], "Proibido porque")
	assert.Contains(t, task["error"], "Peça à MTI")
	assert.NotContains(t, fmt.Sprint(task), goodPIN)
}

func TestPinRecusadoNoMeioParaTudoENaoTentaDeNovo(t *testing.T) {
	e := newSigadocEnv(t, "9604-2026", goodFiles(t), 24)
	e.sigadoc.FailNext("Sign", &sigadoc.Error{Status: 500, Message: "PIN inválido"})
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "interrupted", task["status"], task)
	assert.Equal(t, "pin_rejected", task["errorCode"])
	assert.Contains(t, task["error"], "bloquear")
	signs := 0
	for _, c := range e.sigadoc.Calls() {
		if c == "Sign" {
			signs++
		}
	}
	assert.Equal(t, 1, signs, "um PIN recusado nunca é tentado de novo sozinho")
	assert.Equal(t, 1, len(e.sigadoc.Documents()), "nada além do Termo foi criado")
}

func TestArquivoComProblemaNaoImpedeOsOutros(t *testing.T) {
	files := []fileSpec{
		{"estatuto.pdf", "application/pdf", realPDF(t, 2, "")},
		{"quebrado.pdf", "application/pdf", append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("lixo "), 200)...)},
		{"foto.png", "image/png", pngBytes(t)},
	}
	e := newSigadocEnv(t, "9605-2026", files, 24)
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "interrupted", task["status"], task)
	assert.Equal(t, "item_failed", task["errorCode"])
	assert.Contains(t, task["error"], "1 arquivo")
	var failed, joined int
	for _, it := range itemsOf(task) {
		switch it["status"] {
		case "failed":
			failed++
			assert.Contains(t, it["error"], "corrompido")
		case "joined":
			joined++
		}
	}
	assert.Equal(t, 1, failed)
	assert.Equal(t, 4, joined, "Termo, relatório e os dois arquivos bons")
	assert.Equal(t, "opening_sigadoc", e.project(e.staff.analyst, e.fp.id).data()["stage"], "com item falho o projeto não avança")
}

func TestArquivoGrandeEDividoEmPartes(t *testing.T) {
	files := []fileSpec{{"grande.pdf", "application/pdf", bigPDF(t, 160)}}
	e := newSigadocEnv(t, "9606-2026", files, 1) // cada parte de até 1 MB
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "completed", task["status"], task)
	var parts []map[string]any
	for _, it := range itemsOf(task) {
		if it["kind"] == "document" {
			parts = append(parts, it)
		}
	}
	require.Greater(t, len(parts), 1, "o arquivo passou de 1 MB e foi dividido")
	for i, p := range parts {
		assert.EqualValues(t, i+1, p["part"])
		assert.EqualValues(t, len(parts), p["parts"])
		assert.Equal(t, "joined", p["status"])
	}
	var seen int
	for _, d := range e.descriptions() {
		if strings.Contains(d, fmt.Sprintf("(parte 1 de %d)", len(parts))) {
			seen++
		}
	}
	assert.Equal(t, 1, seen, "a descrição diz 'parte n de N'")
}

func TestArquivarOProjetoCancelaAMontagem(t *testing.T) {
	e := newSigadocEnv(t, "9607-2026", goodFiles(t), 24)
	// Na criação do relatório (depois do Termo) o proponente desiste do projeto.
	e.sigadoc.SetHook("CreateDocument", func() {})
	created := 0
	e.sigadoc.SetHook("Finalize", func() {
		created++
		if created == 1 { // depois de criar o Termo, antes do resto
			e.do("POST", "/api/admin/projects/"+e.fp.id+"/withdraw", map[string]any{"note": "Desistimos."}, e.maria)
		}
	})
	require.Equal(t, 202, e.open(e.staff.analyst, goodPIN, nil).Status)
	task := e.wait()
	require.Equal(t, "cancelled", task["status"], task)
	assert.Equal(t, "project_archived", task["errorCode"])
	assert.Equal(t, "archived", e.project(e.staff.analyst, e.fp.id).data()["status"])
	assert.LessOrEqual(t, len(e.sigadoc.Documents()), 1, "parou: no máximo o Termo foi criado")
	assert.Equal(t, "project_not_submitted", e.open(e.staff.analyst, goodPIN, nil).errCode())
}

func TestCancelarEReiniciarDaApi(t *testing.T) {
	e := newSigadocEnv(t, "9608-2026", goodFiles(t), 24)
	ctx := context.Background()

	t.Run("reinício da API: montagem em execução vira interrompida", func(t *testing.T) {
		_, err := e.pool.Exec(ctx, `INSERT INTO sigadoc_tasks (project_id, matricula, status) VALUES ($1, 'SEDEC111111', 'running')`, e.fp.id)
		require.NoError(t, err)
		n, err := e.app.Projects.InterruptStaleSigadocTasks(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 1, n)
		task := e.state().data()["task"].(map[string]any)
		assert.Equal(t, "interrupted", task["status"])
		assert.Equal(t, "restart", task["errorCode"])
	})
	t.Run("cancelar sem montagem em andamento", func(t *testing.T) {
		r := e.do("POST", "/api/admin/projects/"+e.fp.id+"/sigadoc/cancel", nil, e.staff.analyst)
		assert.Equal(t, "sigadoc_nothing_to_cancel", r.errCode())
	})
	t.Run("retomar sem nada interrompido", func(t *testing.T) {
		e2 := newSigadocEnv(t, "9609-2026", goodFiles(t), 24)
		assert.Equal(t, "sigadoc_nothing_to_resume", e2.resume(e2.staff.analyst, goodPIN).errCode())
	})
	t.Run("take-over fica bloqueado com a montagem em andamento", func(t *testing.T) {
		_, err := e.pool.Exec(ctx, `UPDATE sigadoc_tasks SET status = 'running' WHERE project_id = $1`, e.fp.id)
		require.NoError(t, err)
		other, _ := e.newAdmin(e.boss, "m.analista3@exemplo.com", "analyst")
		assert.Equal(t, "sigadoc_task_running", e.takeOver(other, e.fp.id).errCode())
		r := e.do("POST", "/api/admin/projects/"+e.fp.id+"/sigadoc/cancel", nil, e.staff.analyst)
		require.Equal(t, 200, r.Status, r.Body)
		assert.Equal(t, "cancelled", r.data()["task"].(map[string]any)["status"], "sem execução viva, a tarefa é fechada aqui")
		assert.Equal(t, 200, e.takeOver(other, e.fp.id).Status, "terminada a montagem, o take-over volta a valer")
	})
}

func TestPinTemLimiteDeTentativas(t *testing.T) {
	e := newSigadocEnv(t, "9610-2026", goodFiles(t), 24)
	for i := 0; i < 5; i++ {
		assert.Equal(t, 422, e.open(e.staff.analyst, "00000000", nil).Status, "tentativa %d", i+1)
	}
	r := e.open(e.staff.analyst, goodPIN, nil)
	assert.Equal(t, 429, r.Status, "depois de 5 tentativas na janela, espera")
	assert.Zero(t, e.countRows("sigadoc_tasks"))
	assert.Equal(t, 422, e.open(e.staff.analyst, "12", nil).Status, "PIN curto demais nem conta")
}

func TestAberturaSemConfiguracaoOuMatricula(t *testing.T) {
	e := newSigadocEnv(t, "9611-2026", goodFiles(t), 24)
	ctx := context.Background()
	_, err := e.pool.Exec(ctx, `UPDATE rule_parameters SET value = jsonb_set(value, '{termo_model_id}', '""') WHERE key = 'sigadoc_settings'`)
	require.NoError(t, err)
	assert.Equal(t, "sigadoc_not_configured", e.open(e.staff.analyst, goodPIN, nil).errCode(), "sem o id do modelo do Termo")
	assert.Equal(t, false, e.state().data()["opening"].(map[string]any)["available"])
	_, err = e.pool.Exec(ctx, `UPDATE rule_parameters SET value = jsonb_set(value, '{termo_model_id}', '"42"') WHERE key = 'sigadoc_settings'`)
	require.NoError(t, err)

	e.sigadoc.SetMatriculas(e.cpf) // o SIGADOC não tem matrícula para este CPF
	assert.Equal(t, "sigadoc_no_matricula", e.open(e.staff.analyst, goodPIN, nil).errCode())
}
