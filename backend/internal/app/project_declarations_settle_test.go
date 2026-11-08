package app_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declStatus lê a situação da declaração ativa direto do banco (sem passar por nenhuma rota que conclua a assinatura).
func (p *projEnv) declStatus(projectID, code string) string {
	p.t.Helper()
	var st string
	require.NoError(p.t, p.pool.QueryRow(context.Background(),
		"SELECT status FROM project_declarations WHERE project_id = $1 AND requirement_code = $2 ORDER BY created_at DESC LIMIT 1", projectID, code).Scan(&st))
	return st
}

// issued emite a declaração de contrapartida e devolve o pedido de assinatura.
func (p *projEnv) issuedContrapartida() (*fundingProject, string) {
	p.t.Helper()
	fp := p.declaredProject(1_500_000)
	fp.issue(contrapartida)
	return fp, p.mtid.OrderIDs()[len(p.mtid.OrderIDs())-1]
}

// Quem lê o checklist ou o projeto vê a assinatura concluída sem ter aberto a declaração.
func TestLeituraConcluiAssinaturaPendente(t *testing.T) {
	t.Run("checklist", func(t *testing.T) {
		p := newProjEnv(t)
		fp, order := p.issuedContrapartida()
		items, _ := fp.checklist(p.maria)
		assert.Equal(t, "missing", items[contrapartida]["status"], "ainda não assinou")
		assert.Equal(t, true, items[contrapartida]["declarationAwaiting"])

		p.mtid.Approve(order)
		items, _ = fp.checklist(p.maria)
		assert.Equal(t, "done", items[contrapartida]["status"])
		assert.Len(t, items[contrapartida]["files"], 1)
		assert.Equal(t, "signed", p.declStatus(fp.id, contrapartida))
	})
	t.Run("projeto", func(t *testing.T) {
		p := newProjEnv(t)
		fp, order := p.issuedContrapartida()
		before := fp.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["documents"].(map[string]any)

		p.mtid.Approve(order)
		after := fp.do("GET", "/api/admin/projects/"+fp.id, nil, p.maria).data()["documents"].(map[string]any)
		assert.Equal(t, before["done"].(float64)+1, after["done"])
		assert.Equal(t, "signed", p.declStatus(fp.id, contrapartida))
	})
	t.Run("recusada e vencida também são registradas", func(t *testing.T) {
		p := newProjEnv(t)
		fp, order := p.issuedContrapartida()
		p.mtid.Reject(order)
		items, _ := fp.checklist(p.maria)
		assert.Equal(t, "missing", items[contrapartida]["status"])
		assert.NotEqual(t, true, items[contrapartida]["declarationAwaiting"])
		assert.Equal(t, "rejected", p.declStatus(fp.id, contrapartida))
	})
	t.Run("quem não vê o projeto não dispara a consulta", func(t *testing.T) {
		p := newProjEnv(t)
		fp, order := p.issuedContrapartida()
		p.mtid.Approve(order)
		assert.Equal(t, 404, p.do("GET", docsPath(fp.id), nil, p.joao).Status)
		assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+fp.id, nil, p.joao).Status)
		assert.Equal(t, "awaiting_signature", p.declStatus(fp.id, contrapartida), "o MT-ID não foi consultado")
		assert.Equal(t, 200, p.do("GET", docsPath(fp.id), nil, p.maria).Status)
		assert.Equal(t, "signed", p.declStatus(fp.id, contrapartida))
	})
	t.Run("leituras simultâneas concluem uma vez", func(t *testing.T) {
		p := newProjEnv(t)
		fp, order := p.issuedContrapartida()
		p.mtid.Approve(order)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				assert.Equal(t, 200, p.do("GET", docsPath(fp.id), nil, p.maria).Status)
			}()
		}
		wg.Wait()
		items, _ := fp.checklist(p.maria)
		assert.Equal(t, "done", items[contrapartida]["status"])
		assert.Len(t, items[contrapartida]["files"], 1, "um único arquivo assinado")
	})
}

// O envio conclui a assinatura pendente antes de conferir os documentos.
func TestEnvioConcluiAssinaturaPendente(t *testing.T) {
	p := newProjEnv(t)
	fp, order := p.issuedContrapartida()
	p.mtid.Approve(order)
	fp.submit(p.maria, true) // pode ser recusado por outras pendências; a assinatura é concluída de qualquer forma
	assert.Equal(t, "signed", p.declStatus(fp.id, contrapartida))
}

// A declaração assinada que ficou desatualizada diz o que mudou, no pedido e no item do checklist.
func TestDeclaracaoDesatualizadaDizOQueMudou(t *testing.T) {
	p := newProjEnv(t)
	fp, order := p.issuedContrapartida()
	p.mtid.Approve(order)
	items, _ := fp.checklist(p.maria)
	require.Equal(t, "done", items[contrapartida]["status"])
	assert.Empty(t, items[contrapartida]["declarationStaleReasons"], "em dia: nada mudou")
	d, _ := fp.preview(p.maria, contrapartida)
	assert.Empty(t, current(d)["staleReasons"])

	reasons := func(v any) []string {
		var out []string
		for _, r := range v.([]any) {
			out = append(out, r.(string))
		}
		return out
	}

	// O cargo de quem assina vai impresso no PDF: mudá-lo invalida a assinatura.
	b := proponentBody(0)
	b["jobTitle"] = "Prefeito"
	fp.save("proponent", b)
	d, _ = fp.preview(p.maria, contrapartida)
	require.Equal(t, true, current(d)["stale"])
	assert.Equal(t, []string{"signer_role"}, reasons(current(d)["staleReasons"]))
	items, _ = fp.checklist(p.maria)
	assert.Equal(t, true, items[contrapartida]["declarationStale"])
	assert.Equal(t, []string{"signer_role"}, reasons(items[contrapartida]["declarationStaleReasons"]))

	// O valor da contrapartida está no texto.
	b["jobTitle"] = "Prefeita"
	fp.save("proponent", b)
	fp.save("funding", map[string]any{"type": "own", "ownAmountCents": 10_000_000, "counterpartKind": "financial", "counterpartCents": 1_600_000})
	d, _ = fp.preview(p.maria, contrapartida)
	assert.Equal(t, []string{"text"}, reasons(current(d)["staleReasons"]))
}
