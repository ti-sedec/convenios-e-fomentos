package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/scan"
)

// fakeScanner é um scanner programável para os testes.
type fakeScanner struct {
	mu     sync.Mutex
	infect bool
	err    error
	calls  int
	read   int
}

func (f *fakeScanner) Scan(_ context.Context, r io.Reader) (scan.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		f.read += n
		if err != nil {
			break
		}
	}
	if f.err != nil {
		return scan.Result{}, f.err
	}
	return scan.Result{Infected: f.infect, Signature: "Eicar-Test-Signature"}, nil
}

func (f *fakeScanner) set(infect bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infect, f.err = infect, err
}

func TestIDORDeTodasAsRotasDeProjeto(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	draft := p.newFundingProject("convenio", "municipal_government", nil)
	sent := p.submitted("4000-2026")
	items, _ := sent.checklist(p.maria)
	file := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, 200, p.complement(analyst, sent.id, map[string]any{"items": []any{section("proposal", "")}}).Status)

	type route struct {
		method, path string
		body         any
	}
	member := p.mustInvite(sent.id, "carla@exemplo.com", "read")
	var routes []route
	for _, id := range []string{draft.id, sent.id} {
		b := "/api/admin/projects/" + id
		routes = append(routes,
			route{"GET", b, nil}, route{"DELETE", b, nil},
			route{"PUT", b + "/proponent", proponentBody(1)}, route{"PUT", b + "/institution", institutionBody(1, p.typeID("association"))},
			route{"PUT", b + "/proposal", proposalBody(1)}, route{"PUT", b + "/funding", map[string]any{"version": 1, "type": "own", "ownAmountCents": 1}},
			route{"PUT", b + "/document-options", map[string]any{"version": 1, "reducedDeadline": true}},
			route{"GET", b + "/documents", nil}, route{"PUT", b + "/documents/cartao_cnpj/justification", map[string]any{"text": "x"}},
			route{"POST", b + "/documents/requerimento_apoio/files", map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}},
			route{"POST", b + "/documents/files/" + file + "/confirm", nil}, route{"GET", b + "/documents/files/" + file + "/download", nil},
			route{"DELETE", b + "/documents/files/" + file, nil}, route{"POST", b + "/submit", map[string]any{"version": 1, "declaration": true}},
			route{"POST", b + "/resubmit", map[string]any{"version": 1, "declaration": true}}, route{"POST", b + "/withdraw", map[string]any{"note": "invasor"}},
			route{"POST", b + "/duplicate", map[string]any{}}, route{"GET", b + "/receipt", nil},
			// Ações da equipe: o proponente alheio não as tem.
			route{"POST", b + "/archive", map[string]any{"reason": "other", "note": "invasor"}}, route{"POST", b + "/deny", map[string]any{"note": "invasor"}},
			route{"POST", b + "/complement-requests", map[string]any{"items": []any{section("proposal", "")}}},
			// Compartilhamento: quem não é titular não lista, convida, altera nem revoga.
			route{"GET", b + "/members", nil}, route{"POST", b + "/members", map[string]any{"cpf": cpfFor("x@exemplo.com"), "level": "edit"}},
			route{"PUT", b + "/members/" + member, map[string]any{"level": "edit"}}, route{"DELETE", b + "/members/" + member, nil},
		)
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+strings.TrimPrefix(rt.path, "/api/admin/projects/"), func(t *testing.T) {
			status, _, _ := p.rawBody(rt.method, rt.path, rt.body, p.joao)
			assert.Contains(t, []int{403, 404}, status, "outro proponente nunca alcança o projeto de Maria")
		})
	}
	// Nada mudou no projeto de Maria, nem no acesso dela.
	assert.Len(t, p.do("GET", "/api/admin/projects/"+sent.id+"/members", nil, p.maria).list(), 1)
	got := p.project(p.maria, sent.id)
	assert.Equal(t, "awaiting_proponent", got.data()["stage"])
	assert.Equal(t, "submitted", got.data()["status"])
	assert.Equal(t, "draft", p.project(p.maria, draft.id).data()["status"])
	// O proponente alheio tampouco o enxerga na lista.
	assert.Empty(t, p.do("GET", "/api/admin/projects", nil, p.joao).list())
}

// rawBody é como raw, mas com corpo JSON (qualquer status, inclusive não JSON).
func (e *env) rawBody(method, path string, body any, token string) (int, http.Header, []byte) {
	e.t.Helper()
	r := e.do(method, path, body, token)
	return r.Status, nil, nil
}

func TestEntradasHostisNaoQuebramAListagem(t *testing.T) {
	p := newProjEnv(t)
	p.completeProject("4100-2026")
	for _, q := range []string{"' OR 1=1 --", "%", "_", `\`, "'; DROP TABLE projects; --", "日本語 😀", strings.Repeat("a", 5000)} {
		r := p.do("GET", "/api/admin/projects?q="+urlEscape(q), nil, p.maria)
		assert.Equal(t, 200, r.Status, "q=%q", q)
	}
	assert.Equal(t, 400, p.do("GET", "/api/admin/projects?q=a%00b", nil, p.maria).Status, "NUL na query é recusado, nunca 500")
	assert.Empty(t, p.do("GET", "/api/admin/projects?q=%27+OR+%27a%27%3D%27a%27+--", nil, p.maria).list(), "a injeção não vira 'tudo casa'")
	for _, sort := range []string{"title;DROP TABLE projects", "(select 1)", "updated_at desc, (select pg_sleep(5))", "../x", ""} {
		r := p.do("GET", "/api/admin/projects?sort="+urlEscape(sort), nil, p.maria)
		assert.Contains(t, []int{200, 422}, r.Status, "sort=%q", sort)
	}
	for _, page := range []string{"-1", "0", "abc", "99999999999999999999", "1e3"} {
		r := p.do("GET", "/api/admin/projects?page="+page, nil, p.maria)
		assert.Contains(t, []int{200, 422}, r.Status, "page=%s", page)
	}
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM projects").Scan(&n))
	assert.Equal(t, 1, n, "a tabela continua inteira")
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func TestEntradasHostisNosCamposDoProjeto(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	// E-mail com quebra de linha (injeção de cabeçalho) é recusado.
	b := proponentBody(v)
	b["emails"] = []any{map[string]any{"address": "a@b.co\nBcc: alvo@x.com", "primary": true}}
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria).Status)
	b["emails"] = []any{map[string]any{"address": "a@b.co\r\nBcc: alvo@x.com", "primary": true}}
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/proponent", b, p.maria).Status)

	// Texto estranho em campos livres é guardado como texto e devolvido como texto.
	pb := proposalBody(v)
	pb["title"] = `<script>alert(1)</script> "aspas" 日本語 😀`
	pb["object"] = "Linha 1\nLinha 2 \x00 com nulo"
	assert.Equal(t, 422, p.do("PUT", "/api/admin/projects/"+id+"/proposal", pb, p.maria).Status, "NUL é recusado de forma controlada, nunca 500")
	pb["object"] = "Linha 1\nLinha 2"
	r := p.do("PUT", "/api/admin/projects/"+id+"/proposal", pb, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, `<script>alert(1)</script> "aspas" 日本語 😀`, p.project(p.maria, id).data()["proposal"].(map[string]any)["title"])
	// O corpo excessivo é recusado antes de qualquer processamento.
	huge := proposalBody(v)
	huge["justification"] = strings.Repeat("a", 2<<20)
	assert.Equal(t, 413, p.do("PUT", "/api/admin/projects/"+id+"/proposal", huge, p.maria).Status)
	// Campos que não existem no contrato (mass assignment) são recusados.
	pb2 := proposalBody(v)
	pb2["status"], pb2["code"], pb2["createdById"], pb2["departmentId"] = "submitted", "2026-000001", "x", "y"
	assert.Equal(t, 400, p.do("PUT", "/api/admin/projects/"+id+"/proposal", pb2, p.maria).Status)
	assert.Equal(t, "draft", p.project(p.maria, id).data()["status"])
}

func TestPDFDoComprovanteComTextoHostil(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.completeProject("4200-2026")
	pb := proposalBody(0)
	pb["sigconNumber"], pb["objectNature"] = "4200-2026", "services"
	pb["title"] = "Título com 日本語, emoji 😀 e (parênteses) \\ barra"
	pb["object"] = strings.Repeat("Parágrafo longo com acentuação.\n", 150)
	fp.save("proposal", pb)
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	status, _, pdf := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	require.Equal(t, 200, status)
	assert.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
	assert.Greater(t, len(pdf), 2000)
}

func TestUploadsConcorrentesRespeitamOLimiteDoDocumento(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	var wg sync.WaitGroup
	statuses := make(chan int, 30)
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := p.do("POST", docsPath(fp.id, "/pesquisa_precos/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}, p.maria)
			statuses <- r.Status
		}()
	}
	wg.Wait()
	close(statuses)
	created, refused := 0, 0
	for s := range statuses {
		switch s {
		case 201:
			created++
		case 409:
			refused++
		default:
			t.Errorf("status inesperado %d", s)
		}
	}
	assert.Equal(t, 10, created, "exatamente o limite por documento, mesmo com 30 pedidos simultâneos")
	assert.Equal(t, 20, refused)
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_files").Scan(&n))
	assert.Equal(t, 10, n)
}

func TestCicloCompletoDeUploadsEmParalelo(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	items, _ := fp.checklist(p.maria)
	var codes []string
	for _, it := range items {
		codes = append(codes, it["code"].(string))
	}
	require.GreaterOrEqual(t, len(codes), 20)
	var wg sync.WaitGroup
	errs := make(chan string, len(codes)*2)
	for _, code := range codes[:20] {
		for k := range 2 { // dois arquivos por documento, todos ao mesmo tempo
			wg.Add(1)
			go func() {
				defer wg.Done()
				body := map[string]any{"filename": fmt.Sprintf("%s-%d.pdf", code, k), "contentType": pdfType, "sizeBytes": len(pdfBytes)}
				r := p.do("POST", docsPath(fp.id, "/", code, "/files"), body, p.maria)
				if r.Status != 201 {
					errs <- fmt.Sprintf("pedido %s: %d %v", code, r.Status, r.Body["error"])
					return
				}
				key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
				p.store.Put(key, pdfType, pdfBytes)
				if c := p.do("POST", docsPath(fp.id, "/files/", r.data()["file"].(map[string]any)["id"].(string), "/confirm"), nil, p.maria); c.Status != 200 {
					errs <- fmt.Sprintf("confirmação %s: %d %v", code, c.Status, c.Body["error"])
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	// O limite de envios em andamento por projeto (20) pode recusar parte dos pedidos simultâneos; o que for aceito precisa fechar sem erro.
	for e := range errs {
		assert.Contains(t, e, "pedido", "só pedidos podem ser recusados (limite de pendentes); nenhuma confirmação falha: %s", e)
		assert.Contains(t, e, "409")
	}
	var pending, confirmed int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FILTER (WHERE status='pending'), count(*) FILTER (WHERE status='confirmed') FROM project_files").Scan(&pending, &confirmed))
	assert.Zero(t, pending, "nada fica pela metade")
	assert.Positive(t, confirmed)
	assert.Equal(t, confirmed, p.store.Len(), "um objeto por arquivo confirmado")
}

func TestLimitesDeArmazenamentoDoProjeto(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	req := func(code string, size int64) resp {
		return p.do("POST", docsPath(fp.id, "/", code, "/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": size}, p.maria)
	}
	// Envios em andamento: 20 no máximo por projeto.
	codes := []string{"oficio_solicitacao", "requerimento_apoio", "plano_trabalho_sigcon", "comprovante_envio_sigcon", "certidao_habilitacao", "termo_referencia", "mapa_comparativo", "analise_critica", "pesquisa_precos", "declaracao_compatibilidade_precos"}
	n := 0
	for _, c := range codes {
		for range 2 {
			n++
			r := req(c, 10)
			if n <= 20 {
				require.Equal(t, 201, r.Status, r.Body)
			} else {
				assert.Equal(t, "too_many_pending", r.errCode())
			}
		}
	}
	assert.Equal(t, "too_many_pending", req("cartao_cnpj", 10).errCode())
	// Liberado o espaço (aqui, apagando os registros), novos pedidos voltam a ser aceitos.
	_, err := p.pool.Exec(context.Background(), "DELETE FROM project_files")
	require.NoError(t, err)

	// Armazenamento total: 2 GiB por projeto (tamanhos declarados).
	const mb = int64(1) << 20
	for i, c := range codes[:10] {
		r := req(c, 200*mb)
		if i < 10 {
			require.Equal(t, 201, r.Status, "%d: %v", i, r.Body)
		}
	}
	r := req("cartao_cnpj", 100*mb)
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "project_storage_full", r.errCode(), "2.000 MB já declarados; mais 100 MB passam de 2 GiB")
	assert.Equal(t, 201, req("cartao_cnpj", 40*mb).Status, "ainda cabem 48 MB")
}

func TestTaxaDeUploadPorUsuario(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RateLimitUploadPerMin = 6 })
	e.registerFirst()
	boss := e.login("ana@exemplo.com")
	e.runSeed(false)
	maria, joao := e.login("maria@exemplo.com"), e.login("joao@exemplo.com")
	pe := &projEnv{env: e, boss: boss, maria: maria, joao: joao}
	fp := pe.newFundingProject("convenio", "municipal_government", nil)

	statuses := map[int]int{}
	for range 10 {
		r := pe.do("POST", docsPath(fp.id, "/pesquisa_precos/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}, maria)
		statuses[r.Status]++
	}
	assert.Equal(t, 6, statuses[201], "seis pedidos por minuto")
	assert.Equal(t, 4, statuses[429])
	// O limite é por usuário, não por IP.
	jp := pe.do("POST", "/api/admin/projects", nil, joao)
	require.Equal(t, 201, jp.Status, "outra pessoa, mesmo IP, segue livre")
}

func TestHashEVerificacaoDeVirus(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	sum := sha256.Sum256(pdfBytes)
	want := hex.EncodeToString(sum[:])

	// Sem scanner: o hash é calculado mesmo assim.
	id := fp.upload(p.maria, "requerimento_apoio", "")
	items, _ := fp.checklist(p.maria)
	file := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)
	assert.Equal(t, id, file["id"])
	assert.Equal(t, want, file["sha256"])

	sc := &fakeScanner{}
	p.app.Projects.SetScanner(sc)
	start := func(code string) (fileID, key string) {
		r := p.do("POST", docsPath(fp.id, "/", code, "/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": len(pdfBytes)}, p.maria)
		require.Equal(t, 201, r.Status, r.Body)
		fileID = r.data()["file"].(map[string]any)["id"].(string)
		key = r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
		p.store.Put(key, pdfType, pdfBytes)
		return
	}
	confirm := func(fileID string) resp {
		return p.do("POST", docsPath(fp.id, "/files/", fileID, "/confirm"), nil, p.maria)
	}

	// Limpo: confirma, e o scanner leu o arquivo inteiro.
	f1, _ := start("termo_referencia")
	r := confirm(f1)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, want, r.data()["sha256"])
	assert.Equal(t, len(pdfBytes), sc.read)

	// Infectado: recusado (422), objeto e registro apagados, ocorrência na auditoria.
	sc.set(true, nil)
	f2, k2 := start("mapa_comparativo")
	r = confirm(f2)
	assert.Equal(t, 422, r.Status, r.Body)
	assert.Contains(t, fmt.Sprint(r.Body["error"]), "verificação de segurança")
	assert.False(t, p.store.Has(k2))
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), "SELECT count(*) FROM project_files WHERE id = $1", f2).Scan(&n))
	assert.Zero(t, n)
	logs := p.do("GET", "/api/admin/audit-logs?action=project.file-infected", nil, p.boss).list()
	require.Len(t, logs, 1)
	assert.Equal(t, "Eicar-Test-Signature", logs[0].(map[string]any)["payload"].(map[string]any)["signature"])

	// Scanner fora do ar: nada é liberado e nada é apagado; quando volta, o mesmo arquivo confirma.
	sc.set(false, fmt.Errorf("%w: conexão recusada", scan.ErrScan))
	f3, k3 := start("analise_critica")
	r = confirm(f3)
	assert.Equal(t, 503, r.Status)
	assert.Equal(t, "scan_unavailable", r.errCode())
	assert.True(t, p.store.Has(k3), "o objeto fica para nova tentativa")
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "missing", items["analise_critica"]["status"], "pendente não conta como entregue")
	sc.set(false, nil)
	assert.Equal(t, 200, confirm(f3).Status)
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "done", items["analise_critica"]["status"])

	// O hash vai no retrato da versão enviada.
	for _, code := range []string{"certidao_habilitacao"} {
		fp.upload(p.maria, code, "")
	}
	assert.Contains(t, fmt.Sprint(items["analise_critica"]["files"]), want)
}

func TestConfirmacaoUsaOTamanhoEOHashDoObjetoReal(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	r := p.do("POST", docsPath(fp.id, "/requerimento_apoio/files"), map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": len(pdfBytes)}, p.maria)
	require.Equal(t, 201, r.Status)
	key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
	// O que vale é o objeto guardado, não o tamanho declarado no pedido.
	real := append(append([]byte{}, pdfBytes...), []byte("trailer")...)
	p.store.Put(key, pdfType, real)
	c := p.do("POST", docsPath(fp.id, "/files/", r.data()["file"].(map[string]any)["id"].(string), "/confirm"), nil, p.maria)
	require.Equal(t, 200, c.Status, c.Body)
	sum := sha256.Sum256(real)
	assert.Equal(t, hex.EncodeToString(sum[:]), c.data()["sha256"])
	assert.EqualValues(t, len(real), c.data()["sizeBytes"])
}

func TestCPFSoApareceCompletoParaODono(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.completeProject("4300-2026")
	b := proponentBody(fp.current(p.maria))
	b["isLegalRepresentative"] = false
	b["legalRepName"], b["legalRepCpf"], b["legalRepJobTitle"] = "José da Silva", cpfFor("jose@exemplo.com"), "Prefeito"
	require.Equal(t, 200, p.do("PUT", "/api/admin/projects/"+fp.id+"/proponent", b, p.maria).Status)

	owner := p.project(p.maria, fp.id).data()["proponent"].(map[string]any)
	assert.Len(t, owner["cpf"], 11)
	assert.Equal(t, cpfFor("jose@exemplo.com"), owner["legalRepCpf"])

	p.mkRole("leitor", "admin::projects.read")
	staff, _ := p.newAdmin(p.boss, "bia@exemplo.com", "leitor")
	viewed := p.project(staff, fp.id).data()["proponent"].(map[string]any)
	assert.Regexp(t, `^\*\*\*\.\d{3}\.\d{3}-\*\*$`, viewed["cpf"], "a equipe vê o CPF mascarado")
	assert.Regexp(t, `^\*\*\*\.\d{3}\.\d{3}-\*\*$`, viewed["legalRepCpf"])
	assert.NotContains(t, fmt.Sprint(p.project(staff, fp.id).Body), owner["cpf"])
	// Listagens não trazem CPF nenhum.
	assert.NotContains(t, fmt.Sprint(p.do("GET", "/api/admin/projects", nil, staff).Body), owner["cpf"])
}

func TestDownloadsFicamRegistradosNaAuditoria(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.submitted("4400-2026")
	items, _ := fp.checklist(p.maria)
	fileID := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	require.Equal(t, 200, p.do("GET", docsPath(fp.id, "/files/", fileID, "/download"), nil, p.maria).Status)
	require.Equal(t, 200, p.do("GET", docsPath(fp.id, "/files/", fileID, "/download"), nil, p.maria).Status)
	status, _, _ := p.raw("GET", "/api/admin/projects/"+fp.id+"/receipt", p.maria)
	require.Equal(t, 200, status)

	dl := p.do("GET", "/api/admin/audit-logs?action=project.file-download&entityId="+fp.id, nil, p.boss).list()
	require.Len(t, dl, 2)
	entry := dl[0].(map[string]any)
	assert.Equal(t, "admin", entry["actorType"])
	assert.Equal(t, fileID, entry["payload"].(map[string]any)["file"])
	assert.Equal(t, "requerimento_apoio", entry["payload"].(map[string]any)["document"])
	assert.Len(t, p.do("GET", "/api/admin/audit-logs?action=project.receipt-download&entityId="+fp.id, nil, p.boss).list(), 1)
	// Tentativa recusada não gera registro de download.
	p.do("GET", docsPath(fp.id, "/files/", fileID, "/download"), nil, p.joao)
	assert.Len(t, p.do("GET", "/api/admin/audit-logs?action=project.file-download&entityId="+fp.id, nil, p.boss).list(), 2)
}

func TestRetencaoDeRascunhosAbandonados(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	old := p.newFundingProject("convenio", "municipal_government", nil)
	old.upload(p.maria, "requerimento_apoio", "")
	recent := p.newFundingProject("convenio", "municipal_government", nil)
	recent.upload(p.maria, "termo_referencia", "")
	sent := p.submitted("4500-2026")
	require.Equal(t, 3, p.store.Len())
	// Todos parecem antigos, mas só o rascunho antigo é apagado: o recente foi mexido agora e o enviado nunca expira.
	_, err := p.pool.Exec(context.Background(), "UPDATE projects SET updated_at = now() - interval '400 days' WHERE id = ANY($1)", []string{old.id, sent.id})
	require.NoError(t, err)

	ctx := context.Background()
	n, err := p.app.Projects.CleanupAbandonedDrafts(ctx, 0)
	require.NoError(t, err)
	assert.Zero(t, n, "0 desliga a retenção")
	n, err = p.app.Projects.CleanupAbandonedDrafts(ctx, 365)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+old.id, nil, p.maria).Status)
	assert.Equal(t, 200, p.do("GET", "/api/admin/projects/"+recent.id, nil, p.maria).Status)
	assert.Equal(t, "submitted", p.project(p.maria, sent.id).data()["status"])
	assert.Equal(t, 2, p.store.Len(), "os arquivos do rascunho apagado saem do armazenamento")
	logs := p.do("GET", "/api/admin/audit-logs?action=project.draft-expired&entityId="+old.id, nil, p.boss).list()
	assert.Len(t, logs, 1)
	n, err = p.app.Projects.CleanupAbandonedDrafts(ctx, 365)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestEdicaoSimultaneaDaMesmaVersao(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := proposalBody(v)
			b["title"] = fmt.Sprintf("Título %d", i)
			statuses <- p.do("PUT", "/api/admin/projects/"+id+"/proposal", b, p.maria).Status
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
	assert.Equal(t, 1, ok, "só uma gravação vence a corrida")
	assert.Equal(t, 7, conflict)
	assert.EqualValues(t, v+1, p.project(p.maria, id).data()["version"])
}
