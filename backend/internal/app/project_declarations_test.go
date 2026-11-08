package app_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func declPath(id, code string) string { return "/api/admin/projects/" + id + "/declarations/" + code }

func (f *fundingProject) declaration(tok, code string) resp {
	f.t.Helper()
	return f.do("GET", declPath(f.id, code), nil, tok)
}

func TestPreviaDaDeclaracaoDeContrapartida(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	fp.save("proponent", proponentBody(0))

	// Sem a contrapartida informada o texto sai, mas não pode ser emitido.
	d := fp.declaration(p.maria, "declaracao_contrapartida")
	require.Equal(t, 200, d.Status, d.Body)
	assert.Equal(t, false, d.data()["ready"])
	assert.Equal(t, "Anexo IV", d.data()["annex"])

	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_500_000})
	d = fp.declaration(p.maria, "declaracao_contrapartida")
	require.Equal(t, 200, d.Status, d.Body)
	assert.Equal(t, true, d.data()["ready"], d.Body)
	assert.Contains(t, d.data()["paragraphs"].([]any)[0], "depositará o valor de R$ 15.000,00")
	assert.Equal(t, "Cuiabá/MT", d.data()["place"])
	assert.Equal(t, "Prefeita", d.data()["signerRole"])
	first := d.data()["paramsSha256"]

	// Mudar o valor muda o resumo: uma declaração já emitida deixa de valer.
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_600_000})
	assert.NotEqual(t, first, fp.declaration(p.maria, "declaracao_contrapartida").data()["paramsSha256"])
}

func TestPreviaDaDeclaracaoRespeitaAplicabilidadeEAlcance(t *testing.T) {
	p := newProjEnv(t)

	// Calamidade dispensa a contrapartida: a declaração não se aplica.
	cal := p.newFundingProject("convenio", "municipal_government", func(b map[string]any) { b["calamity"] = true })
	assert.Equal(t, 409, cal.declaration(p.maria, "declaracao_contrapartida").Status)

	// Termo de Fomento não tem declaração de contrapartida, mas tem a de não duplicidade (Anexo V).
	fom := p.newFundingProject("fomento", "association", nil)
	assert.Equal(t, 409, fom.declaration(p.maria, "declaracao_contrapartida").Status)
	d := fom.declaration(p.maria, "declaracao_nao_duplicidade")
	require.Equal(t, 200, d.Status, d.Body)
	assert.Equal(t, "Anexo V", d.data()["annex"])

	// Código sem modelo, sem sessão e projeto de outra pessoa.
	assert.Equal(t, 404, fom.declaration(p.maria, "termo_referencia").Status)
	assert.Equal(t, 401, fom.declaration("", "declaracao_nao_duplicidade").Status)
	assert.Equal(t, 404, fom.declaration(p.joao, "declaracao_nao_duplicidade").Status)
}

// declaredProject é um convênio municipal pronto para emitir a declaração de contrapartida: Maria é a representante legal.
func (p *projEnv) declaredProject(counterpartCents int) *fundingProject {
	p.t.Helper()
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	fp.save("proponent", proponentBody(0))
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": counterpartCents})
	return fp
}

const contrapartida = "declaracao_contrapartida"

func (f *fundingProject) accept(tok, code, hash string, confirm bool) resp {
	f.t.Helper()
	return f.do("POST", declPath(f.id, code)+"/accept", map[string]any{"paramsSha256": hash, "confirm": confirm}, tok)
}

// preview devolve a prévia (com a situação da última emissão) e o resumo do conteúdo.
func (f *fundingProject) preview(tok, code string) (map[string]any, string) {
	f.t.Helper()
	r := f.declaration(tok, code)
	require.Equal(f.t, 200, r.Status, r.Body)
	return r.data(), r.data()["paramsSha256"].(string)
}

func current(d map[string]any) map[string]any {
	c, _ := d["current"].(map[string]any)
	return c
}

func (f *fundingProject) issue(code string) string {
	f.t.Helper()
	d, hash := f.preview(f.maria, code)
	require.Equal(f.t, true, d["ready"], d["issues"])
	r := f.accept(f.maria, code, hash, true)
	require.Equal(f.t, 200, r.Status, r.Body)
	return hash
}

func TestEmissaoEAssinaturaDaDeclaracao(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)

	d, hash := fp.preview(p.maria, contrapartida)
	assert.Nil(t, current(d), "ainda não foi emitida")

	// O aceite exige a confirmação e o resumo do texto que a pessoa leu.
	assert.Equal(t, 422, fp.accept(p.maria, contrapartida, hash, false).Status)
	wrong := fp.accept(p.maria, contrapartida, strings.Repeat("0", 64), true)
	assert.Equal(t, 409, wrong.Status)
	assert.Equal(t, "declaration_changed", wrong.errCode())
	assert.Equal(t, 0, p.mtid.Orders(), "nada foi pedido")

	// Emissão: PDF guardado, arquivo enviado ao MT-ID e pedido de assinatura para o CPF de quem assina.
	r := fp.accept(p.maria, contrapartida, hash, true)
	require.Equal(t, 200, r.Status, r.Body)
	c := current(r.data())
	assert.Equal(t, "awaiting_signature", c["status"])
	assert.Equal(t, true, c["requestedBySigner"])
	assert.Equal(t, false, c["stale"])
	require.Len(t, p.mtid.OrderIDs(), 1)
	order := p.mtid.OrderIDs()[0]
	req, _ := p.mtid.Request(order)
	assert.Equal(t, cpfFor("maria@exemplo.com"), req.SignerCPF)
	assert.False(t, req.Detached)
	pdf, ok := p.mtid.File(req.FileID)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
	assert.Contains(t, string(pdf), "R$ 15.000,00")

	// Pedir de novo com o mesmo conteúdo não cria outro pedido.
	again := fp.accept(p.maria, contrapartida, hash, true)
	require.Equal(t, 200, again.Status, again.Body)
	assert.Equal(t, c["id"], current(again.data())["id"])
	assert.Len(t, p.mtid.OrderIDs(), 1)

	// Enquanto o signatário não assina, o documento continua faltando.
	items, _ := fp.checklist(p.maria)
	assert.Equal(t, "missing", items[contrapartida]["status"])

	// O signatário assina no MTCidadão; a próxima consulta traz o arquivo para o checklist.
	p.mtid.Approve(order)
	d, _ = fp.preview(p.maria, contrapartida)
	c = current(d)
	assert.Equal(t, "signed", c["status"])
	assert.NotEmpty(t, c["signedAt"])
	fileID, _ := c["fileId"].(string)
	require.NotEmpty(t, fileID)
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "done", items[contrapartida]["status"])
	files := items[contrapartida]["files"].([]any)
	require.Len(t, files, 1)
	f := files[0].(map[string]any)
	assert.Equal(t, fileID, f["id"])
	assert.Equal(t, "declaracao-contrapartida-assinada.pdf", f["filename"])
	assert.Equal(t, "confirmed", f["status"])
	assert.Len(t, f["sha256"], 64)
	assert.Equal(t, 200, p.do("GET", docsPath(fp.id, "/files/", fileID, "/download"), nil, p.maria).Status)

	// Já assinada com o mesmo conteúdo: não emite outra.
	done := fp.accept(p.maria, contrapartida, hash, true)
	assert.Equal(t, 409, done.Status)
	assert.Equal(t, "declaration_already_signed", done.errCode())

	// A assinatura de cada declaração é separada: a de não duplicidade não foi tocada.
	other, _ := fp.preview(p.maria, "declaracao_nao_duplicidade")
	assert.Nil(t, current(other))
	assert.Equal(t, "missing", items["declaracao_nao_duplicidade"]["status"])
}

func TestDeclaracaoAssinadaFicaDesatualizadaQuandoOCadastroMuda(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	fp.issue(contrapartida)
	p.mtid.Approve(p.mtid.OrderIDs()[0])
	_, _ = fp.preview(p.maria, contrapartida) // conclui a assinatura
	items, _ := fp.checklist(p.maria)
	require.Equal(t, "done", items[contrapartida]["status"])
	before := fp.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["documents"].(map[string]any)

	// O valor da contrapartida muda: a declaração assinada não vale mais.
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_600_000})
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "missing", items[contrapartida]["status"])
	assert.Equal(t, true, items[contrapartida]["declarationStale"])
	after := fp.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["documents"].(map[string]any)
	assert.Equal(t, before["done"].(float64)-1, after["done"])
	assert.Equal(t, before["missing"].(float64)+1, after["missing"])
	d, newHash := fp.preview(p.maria, contrapartida)
	assert.Equal(t, true, current(d)["stale"])
	assert.Equal(t, "signed", current(d)["status"])

	// Emitir a nova substitui a antiga; a antiga sai do checklist.
	fp.accept(p.maria, contrapartida, newHash, true)
	d, _ = fp.preview(p.maria, contrapartida)
	assert.Equal(t, "awaiting_signature", current(d)["status"])
	items, _ = fp.checklist(p.maria)
	assert.Empty(t, items[contrapartida]["files"], "a assinada antiga saiu do checklist")
	p.mtid.Approve(p.mtid.OrderIDs()[len(p.mtid.OrderIDs())-1])
	d, _ = fp.preview(p.maria, contrapartida)
	assert.Equal(t, "signed", current(d)["status"])
	assert.Equal(t, false, current(d)["stale"])
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, "done", items[contrapartida]["status"])
	assert.Equal(t, false, items[contrapartida]["declarationStale"])
	assert.Len(t, items[contrapartida]["files"], 1)
}

func TestMudarOCadastroComPedidoEmAndamentoSubstituiOPedido(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	fp.issue(contrapartida)
	first := p.mtid.OrderIDs()[0]

	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_700_000})
	_, hash := fp.preview(p.maria, contrapartida)
	require.Equal(t, 200, fp.accept(p.maria, contrapartida, hash, true).Status)

	ids := p.mtid.OrderIDs()
	require.Len(t, ids, 1, "o pedido com o valor antigo foi cancelado no MT-ID")
	assert.NotEqual(t, first, ids[0])
}

func TestAssinaturaRecusadaOuVencidaPermiteEmitirDeNovo(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	hash := fp.issue(contrapartida)

	p.mtid.Reject(p.mtid.OrderIDs()[0])
	d, _ := fp.preview(p.maria, contrapartida)
	assert.Equal(t, "rejected", current(d)["status"])
	items, _ := fp.checklist(p.maria)
	assert.Equal(t, "missing", items[contrapartida]["status"])

	require.Equal(t, 200, fp.accept(p.maria, contrapartida, hash, true).Status)
	last := p.mtid.OrderIDs()[len(p.mtid.OrderIDs())-1]
	p.mtid.Expire(last)
	d, _ = fp.preview(p.maria, contrapartida)
	assert.Equal(t, "expired", current(d)["status"])

	require.Equal(t, 200, fp.accept(p.maria, contrapartida, hash, true).Status)
}

func TestCancelarPedidoDeAssinatura(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)

	del := func(tok string) resp { return p.do("DELETE", declPath(fp.id, contrapartida), nil, tok) }
	assert.Equal(t, 409, del(p.maria).Status, "não há pedido")

	fp.issue(contrapartida)
	require.Equal(t, 1, p.mtid.Orders())
	assert.Equal(t, 204, del(p.maria).Status)
	assert.Equal(t, 0, p.mtid.Orders(), "o pedido foi cancelado no MT-ID")
	d, _ := fp.preview(p.maria, contrapartida)
	assert.Equal(t, "cancelled", current(d)["status"])
	assert.Equal(t, 409, del(p.maria).Status)
}

func TestFalhaDoMTIDNaoDeixaRastro(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	_, hash := fp.preview(p.maria, contrapartida)
	objects := p.store.Len()

	p.mtid.Err = errors.New("x-via fora do ar")
	r := fp.accept(p.maria, contrapartida, hash, true)
	assert.Equal(t, 502, r.Status)
	assert.Equal(t, "signature_failed", r.errCode())
	assert.Equal(t, objects, p.store.Len(), "o PDF gerado foi apagado")
	d, _ := fp.preview(p.maria, contrapartida)
	assert.Nil(t, current(d))

	p.mtid.Err = nil
	assert.Equal(t, 200, fp.accept(p.maria, contrapartida, hash, true).Status, "volta a funcionar")
}

func TestSignatarioEORepresentanteLegalInformado(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	rep := cpfFor("prefeito@exemplo.com")
	body := proponentBody(0)
	body["isLegalRepresentative"], body["legalRepName"], body["legalRepCpf"], body["legalRepJobTitle"] = false, "João Souza", rep, "Prefeito"
	fp.save("proponent", body)
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_500_000})

	d, hash := fp.preview(p.maria, contrapartida)
	assert.Equal(t, "João Souza", d["signerName"])
	r := fp.accept(p.maria, contrapartida, hash, true)
	require.Equal(t, 200, r.Status, r.Body)
	// Quem pediu (Maria) não é quem assina: o pedido vai para o representante, que confirma no MTCidadão.
	assert.Equal(t, false, current(r.data())["requestedBySigner"])
	req, _ := p.mtid.Request(p.mtid.OrderIDs()[0])
	assert.Equal(t, rep, req.SignerCPF)
	assert.NotContains(t, string(mustJSON(t, r.Body)), rep, "o CPF do signatário não sai da API")
}

func TestEmitirDeclaracaoExigeCadastroCompletoEPermissao(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	fp.save("proponent", proponentBody(0))
	d, hash := fp.preview(p.maria, contrapartida)
	require.Equal(t, false, d["ready"], "sem contrapartida")
	r := fp.accept(p.maria, contrapartida, hash, true)
	assert.Equal(t, 422, r.Status)
	assert.Equal(t, "declaration_not_ready", r.errCode())

	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_500_000})
	_, hash = fp.preview(p.maria, contrapartida)
	assert.Equal(t, 403, fp.accept(p.joao, contrapartida, hash, true).Status, "projeto de outra pessoa")
	assert.Equal(t, 401, fp.accept("", contrapartida, hash, true).Status)
	assert.Equal(t, 404, fp.accept(p.maria, "termo_referencia", hash, true).Status, "sem modelo")
	assert.Equal(t, 0, p.mtid.Orders())
}

func TestRemoverADeclaracaoAssinadaCancelaAEmissao(t *testing.T) {
	p := newProjEnv(t)
	fp := p.declaredProject(1_500_000)
	hash := fp.issue(contrapartida)
	p.mtid.Approve(p.mtid.OrderIDs()[0])
	d, _ := fp.preview(p.maria, contrapartida)
	fileID := current(d)["fileId"].(string)

	require.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", fileID), nil, p.maria).Status)
	d, _ = fp.preview(p.maria, contrapartida)
	assert.Equal(t, "cancelled", current(d)["status"])
	require.Equal(t, 200, fp.accept(p.maria, contrapartida, hash, true).Status, "pode emitir de novo")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// personalBody são os dados pessoais de quem assina, com o endereço em Cuiabá.
func personalBody() map[string]any {
	return map[string]any{
		"nationality": "brasileira", "maritalStatus": "married", "zip": "78000-000", "street": "Rua das Flores", "number": "100",
		"complement": "", "district": "Centro", "uf": "MT", "municipalityIbge": "5103403",
	}
}

// signerProject é um convênio de município em que Maria é a representante legal e tem os dados pessoais preenchidos.
func (p *projEnv) signerProject() *fundingProject {
	p.t.Helper()
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	body := proponentBody(0)
	body["personal"] = personalBody()
	fp.save("proponent", body)
	return fp
}

func TestGratuidadeDaPrevisaoAAssinatura(t *testing.T) {
	p := newProjEnv(t)
	fp := p.signerProject()
	cpfMaria := cpfFor("maria@exemplo.com")

	d, hash := fp.preview(p.maria, "declaracao_gratuidade")
	assert.Equal(t, true, d["ready"], d["issues"])
	assert.Equal(t, "Anexo VII", d["annex"])
	text := d["paragraphs"].([]any)[0].(string)
	assert.Contains(t, text, "Eu, ")
	assert.Contains(t, text, "brasileira, casado(a), portador(a) do CPF nº "+formatCPF(cpfMaria))
	assert.Contains(t, text, "residente e domiciliado em Rua das Flores, nº 100, Bairro Centro – Cuiabá/MT,")
	assert.Contains(t, text, "CNPJ nº 11.222.333/0001-81, DECLARO que a Programação Pavimentação da Rua A a ser realizada nos seguintes locais e períodos: "+
		"no município de Cuiabá (Av. Principal), no período de 01/11/2026 a 15/12/2026; no município de Várzea Grande, no período de 10/01/2027 a 01/05/2027, terá entrada gratuita")
	assert.NotContains(t, text, "Identidade")

	require.Equal(t, 200, fp.accept(p.maria, "declaracao_gratuidade", hash, true).Status)
	p.mtid.Approve(p.mtid.OrderIDs()[0])
	d, _ = fp.preview(p.maria, "declaracao_gratuidade")
	assert.Equal(t, "signed", current(d)["status"])
	items, _ := fp.checklist(p.maria)
	assert.Equal(t, "done", items["declaracao_gratuidade"]["status"])
	assert.Equal(t, "declaracao-gratuidade-assinada.pdf", items["declaracao_gratuidade"]["files"].([]any)[0].(map[string]any)["filename"])
	// Cada declaração tem o seu arquivo: as outras não foram tocadas.
	assert.Equal(t, "missing", items["declaracao_publicidade"]["status"])
	assert.Equal(t, "missing", items["declaracao_compatibilidade_precos"]["status"])
}

func TestPublicidadeEmitidaPelaPrefeituraEAssinadaPorOutroRepresentante(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	rep := cpfFor("prefeito@exemplo.com")
	body := proponentBody(0)
	body["personal"] = personalBody() // os dados da Maria não valem para o representante
	body["isLegalRepresentative"], body["legalRepName"], body["legalRepCpf"], body["legalRepJobTitle"] = false, "João Souza", rep, "Prefeito"
	fp.save("proponent", body)

	d, _ := fp.preview(p.maria, "declaracao_publicidade")
	assert.Equal(t, false, d["ready"])
	assert.Contains(t, d["issues"].([]any)[0].(map[string]any)["message"], "do representante legal")

	body["legalRepPersonal"] = map[string]any{"nationality": "brasileiro", "maritalStatus": "single", "street": "Av. Brasil", "number": "S/N",
		"district": "Jardim", "uf": "MT", "municipalityIbge": "5103403"}
	fp.save("proponent", body)
	d, hash := fp.preview(p.maria, "declaracao_publicidade")
	require.Equal(t, true, d["ready"], d["issues"])
	text := d["paragraphs"].([]any)[0].(string)
	assert.Contains(t, text, "Eu, João Souza, brasileiro, solteiro(a), portador(a) do CPF nº "+formatCPF(rep))
	assert.Contains(t, text, "Av. Brasil, nº S/N, Bairro Jardim – Cuiabá/MT")
	assert.Contains(t, text, "COMPROMETO-ME a manter um perfil na Plataforma Mapas")

	require.Equal(t, 200, fp.accept(p.maria, "declaracao_publicidade", hash, true).Status)
	req, _ := p.mtid.Request(p.mtid.OrderIDs()[0])
	assert.Equal(t, rep, req.SignerCPF)

	// Mudar o estado civil de quem assina invalida a declaração.
	body["legalRepPersonal"].(map[string]any)["maritalStatus"] = "married"
	fp.save("proponent", body)
	d, _ = fp.preview(p.maria, "declaracao_publicidade")
	assert.Equal(t, true, current(d)["stale"])
}

func TestCompatibilidadeDePrecosNaoPedeDadosPessoais(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	fp.save("proponent", proponentBody(0))

	d, hash := fp.preview(p.maria, "declaracao_compatibilidade_precos")
	require.Equal(t, true, d["ready"], d["issues"])
	assert.Equal(t, "Anexo V", d["annex"])
	assert.Contains(t, d["paragraphs"].([]any)[0], "apresentadas por Prefeitura Municipal de Cuiabá, referentes à proposta do convênio n.º 3216/2026, contemplam preços compatíveis")
	require.Equal(t, 200, fp.accept(p.maria, "declaracao_compatibilidade_precos", hash, true).Status)

	osc := p.newFundingProject("fomento", "association", nil)
	osc.save("proponent", proponentBody(0))
	d, _ = osc.preview(p.maria, "declaracao_compatibilidade_precos")
	assert.Equal(t, "Anexo IV", d["annex"])
	assert.Contains(t, d["paragraphs"].([]any)[0], "proposta do termo de fomento n.º 3216/2026")
}

func TestEquipeVeOCPFMascaradoNaDeclaracao(t *testing.T) {
	p := newProjEnv(t)
	fp := p.signerProject()
	cpfMaria := cpfFor("maria@exemplo.com")
	_, ownerHash := fp.preview(p.maria, "declaracao_publicidade")

	staff := p.analyst()
	r := fp.declaration(staff, "declaracao_publicidade")
	require.Equal(t, 200, r.Status, r.Body)
	text := r.data()["paragraphs"].([]any)[0].(string)
	assert.NotContains(t, text, formatCPF(cpfMaria))
	assert.NotContains(t, text, cpfMaria)
	assert.Contains(t, text, "***.")
	assert.Equal(t, ownerHash, r.data()["paramsSha256"], "o resumo é o mesmo para todos")
}

func TestDadosPessoaisSaoValidadosEHerdadosDoPerfil(t *testing.T) {
	p := newProjEnv(t)

	// Validação, na seção do projeto e no perfil.
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	bad := proponentBody(0)
	bad["personal"] = map[string]any{"maritalStatus": "talvez", "zip": "123", "uf": "MT", "municipalityIbge": "9999999"}
	r := fp.try("proponent", bad)
	require.Equal(t, 422, r.Status, r.Body)
	fields := map[string]bool{}
	for _, f := range r.Body["error"].(map[string]any)["details"].(map[string]any)["fields"].([]any) {
		fields[f.(map[string]any)["field"].(string)] = true
	}
	assert.True(t, fields["personal.maritalStatus"] && fields["personal.zip"] && fields["personal.municipalityIbge"], fields)

	// O perfil guarda os dados pessoais e cada projeto novo começa com eles.
	prof := map[string]any{"phones": []any{map[string]any{"number": "(65) 99999-8888", "type": "mobile"}},
		"emails": []any{map[string]any{"address": "maria@exemplo.com", "primary": true}},
		"uf":     "MT", "municipalityIbge": "5103403", "jobTitle": "Prefeita", "personal": personalBody()}
	require.Equal(t, 200, p.do("PUT", "/api/admin/proponent-profile", prof, p.maria).Status)
	got := p.do("GET", "/api/admin/proponent-profile", nil, p.maria).data()["personal"].(map[string]any)
	assert.Equal(t, "Rua das Flores", got["street"])
	assert.Equal(t, "78000000", got["zip"], "o CEP é guardado só com dígitos")

	id, _ := p.create(p.maria)
	proj := p.do("GET", "/api/admin/projects/"+id, nil, p.maria).data()["proponent"].(map[string]any)
	assert.Equal(t, "brasileira", proj["personal"].(map[string]any)["nationality"])
	assert.Equal(t, "married", proj["personal"].(map[string]any)["maritalStatus"])

	// A duplicação leva os dados pessoais do proponente e do representante.
	src := p.newFundingProject("convenio", "municipal_government", nil)
	b := proponentBody(0)
	b["personal"] = personalBody()
	b["isLegalRepresentative"], b["legalRepName"], b["legalRepJobTitle"] = false, "João Souza", "Prefeito"
	b["legalRepPersonal"] = map[string]any{"nationality": "brasileiro", "maritalStatus": "single", "street": "Av. Brasil", "number": "1", "district": "Centro", "uf": "MT", "municipalityIbge": "5103403"}
	src.save("proponent", b)
	dup := p.duplicate(p.maria, src.id, false)
	require.Equal(t, 201, dup.Status, dup.Body)
	pr := dup.data()["proponent"].(map[string]any)
	assert.Equal(t, "Rua das Flores", pr["personal"].(map[string]any)["street"])
	assert.Equal(t, "Av. Brasil", pr["legalRepPersonal"].(map[string]any)["street"])

	// O representante legal deixa de ser outra pessoa: os dados dele somem.
	b["isLegalRepresentative"] = true
	saved := src.save("proponent", b)
	assert.Equal(t, "", saved.data()["proponent"].(map[string]any)["legalRepPersonal"].(map[string]any)["street"])
}

func formatCPF(d string) string { return d[:3] + "." + d[3:6] + "." + d[6:9] + "-" + d[9:] }
