package app_test

import (
	"time"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (f *fundingProject) extras(tok string) []any {
	f.t.Helper()
	r := f.do("GET", docsPath(f.id), nil, tok)
	require.Equal(f.t, 200, r.Status, r.Body)
	return r.data()["extraDocuments"].([]any)
}

// createExtra cria o documento adicional e devolve o id dele.
func (f *fundingProject) createExtra(tok, title, desc string) (string, resp) {
	f.t.Helper()
	r := f.do("POST", docsPath(f.id, "/extra"), map[string]any{"title": title, "description": desc}, tok)
	if r.Status != 201 {
		return "", r
	}
	list := r.data()["extraDocuments"].([]any)
	return list[len(list)-1].(map[string]any)["id"].(string), r
}

// uploadExtra anexa um PDF ao documento adicional (pedido, envio ao armazenamento e confirmação).
func (f *fundingProject) uploadExtra(tok, itemID string) resp {
	f.t.Helper()
	r := f.do("POST", docsPath(f.id, "/extra/", itemID, "/files"), map[string]any{"filename": "outro.pdf", "contentType": pdfType, "sizeBytes": len(pdfBytes)}, tok)
	if r.Status != 201 {
		return r
	}
	key := r.data()["upload"].(map[string]any)["fields"].(map[string]any)["key"].(string)
	f.store.Put(key, pdfType, pdfBytes)
	c := f.do("POST", docsPath(f.id, "/files/", r.data()["file"].(map[string]any)["id"].(string), "/confirm"), nil, tok)
	require.Equal(f.t, 200, c.Status, c.Body)
	return r
}

func TestDocumentoAdicionalFluxo(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("fomento", "association", nil)

	// Título é obrigatório; descrição não.
	_, r := fp.createExtra(p.maria, "  ", "sem título")
	assert.Equal(t, 422, r.Status)
	id, r := fp.createExtra(p.maria, "  Alvará   de   funcionamento ", "")
	require.Equal(t, 201, r.Status, r.Body)
	x := fp.extras(p.maria)[0].(map[string]any)
	assert.Equal(t, "Alvará de funcionamento", x["title"])
	assert.Equal(t, "", x["description"])
	assert.Nil(t, x["file"])

	// Um único arquivo por documento.
	up := fp.uploadExtra(p.maria, id)
	require.Equal(t, 201, up.Status, up.Body)
	assert.Equal(t, "confirmed", fp.extras(p.maria)[0].(map[string]any)["file"].(map[string]any)["status"])
	assert.Equal(t, "too_many_files", fp.uploadExtra(p.maria, id).errCode())

	// Edição de título e descrição.
	e := p.do("PUT", docsPath(fp.id, "/extra/", id), map[string]any{"title": "Alvará 2026", "description": "Renovado"}, p.maria)
	require.Equal(t, 200, e.Status, e.Body)
	assert.Equal(t, "Renovado", fp.extras(p.maria)[0].(map[string]any)["description"])

	// Não entra no resumo nem bloqueia.
	_, cl := fp.checklist(p.maria)
	assert.EqualValues(t, 24, cl.data()["summary"].(map[string]any)["total"])

	// Só o titular mexe.
	assert.Equal(t, 403, p.do("DELETE", docsPath(fp.id, "/extra/", id), nil, p.joao).Status)

	// Remover apaga o documento e o objeto.
	d := p.do("DELETE", docsPath(fp.id, "/extra/", id), nil, p.maria)
	require.Equal(t, 200, d.Status, d.Body)
	assert.Empty(t, fp.extras(p.maria))
}

func TestDocumentoAdicionalLimiteDeCinco(t *testing.T) {
	p := newProjEnv(t)
	fp := p.newFundingProject("convenio", "municipal_government", nil)
	var last string
	for i := 0; i < 5; i++ {
		id, r := fp.createExtra(p.maria, "Documento", "")
		require.Equal(t, 201, r.Status, r.Body)
		last = id
	}
	_, r := fp.createExtra(p.maria, "Sexto", "")
	assert.Equal(t, 409, r.Status)
	assert.Equal(t, "too_many_extra_documents", r.errCode())

	// Removendo um, abre vaga.
	require.Equal(t, 200, p.do("DELETE", docsPath(fp.id, "/extra/", last), nil, p.maria).Status)
	_, r = fp.createExtra(p.maria, "Sexto", "")
	assert.Equal(t, 201, r.Status, r.Body)
}

func TestDocumentoAdicionalAposOEnvio(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.submitted("2300/2026")

	// Enviado e sem complementação aberta: nada de adicionar.
	_, r := fp.createExtra(p.maria, "Tarde demais", "")
	assert.Equal(t, 409, r.Status)

	// Com complementação aberta, o proponente adiciona (e mexe no que adicionou), sem a equipe ter devolvido nada de documentos.
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("institution", "CEP")}}).Status)
	id, r := fp.createExtra(p.maria, "Comprovante extra", "pedido na análise")
	require.Equal(t, 201, r.Status, r.Body)
	require.Equal(t, 201, fp.uploadExtra(p.maria, id).Status)
	assert.Equal(t, 200, p.do("PUT", docsPath(fp.id, "/extra/", id), map[string]any{"title": "Comprovante", "description": ""}, p.maria).Status)
}

func TestDocumentoAdicionalEnviadoFicaTravado(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.completeProject("2400/2026")
	id, _ := fp.createExtra(p.maria, "Anexo", "")
	require.Equal(t, 201, fp.uploadExtra(p.maria, id).Status)
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{section("institution", "CEP")}}).Status)

	// O que já fez parte da versão enviada não muda nem some.
	assert.Equal(t, true, fp.extras(p.maria)[0].(map[string]any)["locked"])
	e := p.do("PUT", docsPath(fp.id, "/extra/", id), map[string]any{"title": "Outro"}, p.maria)
	assert.Equal(t, 409, e.Status)
	assert.Equal(t, 409, p.do("DELETE", docsPath(fp.id, "/extra/", id), nil, p.maria).Status)
	assert.Len(t, fp.extras(p.maria), 1)
}

func TestDocumentoAdicionalDevolvidoPodeTrocarOArquivo(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	analyst := p.analyst()
	fp := p.completeProject("2500/2026")
	id, _ := fp.createExtra(p.maria, "Anexo", "")
	require.Equal(t, 201, fp.uploadExtra(p.maria, id).Status)
	require.Equal(t, 200, fp.submit(p.maria, true).Status)
	extra := func(note string) map[string]any { return map[string]any{"kind": "extra", "target": id, "note": note} }

	// A equipe devolve o documento adicional com uma observação; o alvo precisa existir.
	bad := p.complement(analyst, fp.id, map[string]any{"items": []any{map[string]any{"kind": "extra", "target": "00000000-0000-0000-0000-000000000000"}}})
	assert.Equal(t, 422, bad.Status, bad.Body)
	require.Equal(t, 200, p.complement(analyst, fp.id, map[string]any{"items": []any{extra("arquivo ilegível")}}).Status)
	item := p.project(p.maria, fp.id).data()["complement"].(map[string]any)["items"].([]any)[0].(map[string]any)
	assert.Equal(t, "Anexo", item["label"])
	assert.Equal(t, false, item["answered"])

	// Devolvido, o documento já enviado aceita nova descrição e troca de arquivo, mas não é excluído (a versão enviada o guarda).
	assert.Equal(t, 200, p.do("PUT", docsPath(fp.id, "/extra/", id), map[string]any{"title": "Anexo", "description": "legível"}, p.maria).Status)
	old := fp.extras(p.maria)[0].(map[string]any)["file"].(map[string]any)["id"].(string)
	require.Equal(t, 204, p.do("DELETE", docsPath(fp.id, "/files/", old), nil, p.maria).Status)
	require.Equal(t, 201, fp.uploadExtra(p.maria, id).Status)
	assert.NotEqual(t, old, fp.extras(p.maria)[0].(map[string]any)["file"].(map[string]any)["id"])
	assert.Equal(t, "extra_document_sent", p.do("DELETE", docsPath(fp.id, "/extra/", id), nil, p.maria).errCode())

	// A gravação marcou o item como respondido, e o reenvio é aceito.
	item = p.project(p.maria, fp.id).data()["complement"].(map[string]any)["items"].([]any)[0].(map[string]any)
	assert.Equal(t, true, item["answered"])
}
