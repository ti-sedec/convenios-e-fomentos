package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A equipe só trabalha com projetos enviados: rascunho é do proponente (e dos membros que ele convidou).
//
// Este teste usa os perfis SEMEADOS (os que as migrations 00022 e 00023 criam), não perfis ad hoc como p.analyst() em
// project_lifecycle_test.go. Os perfis ad hoc não têm condição nenhuma, então nunca revelariam o que o perfil real permite.
func TestEquipeNaoEnxergaRascunhos(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))

	draft := p.completeProject("7001-2026")
	draftFile := draft.upload(p.maria, "requerimento_apoio", "")
	sent := p.submitted("7002-2026")
	items, _ := sent.checklist(p.maria)
	sentFile := items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)

	// As rotas de leitura que a equipe usa. O recibo só existe depois do envio.
	routes := func(id, file string, withReceipt bool) map[string]string {
		r := map[string]string{
			"detalhe":    "/api/admin/projects/" + id,
			"checklist":  docsPath(id),
			"download":   docsPath(id, "/files/", file, "/download"),
			"declaração": declPath(id, contrapartida),
		}
		if withReceipt {
			r["recibo"] = "/api/admin/projects/" + id + "/receipt"
		}
		return r
	}

	// status lê só o código HTTP: o recibo e o download devolvem arquivo, não JSON.
	status := func(tok, path string) int {
		code, _, _ := p.raw("GET", path, tok)
		return code
	}

	// Controle: o titular alcança todas as rotas dos dois projetos. Sem isso, um 404 da equipe poderia ser só uma rota errada.
	ownerStatus := map[string]map[string]int{"rascunho": {}, "enviado": {}}
	for name, path := range routes(draft.id, draftFile, false) {
		ownerStatus["rascunho"][name] = status(p.maria, path)
		require.NotEqual(t, 404, ownerStatus["rascunho"][name], "titular em %s do rascunho", name)
	}
	for name, path := range routes(sent.id, sentFile, true) {
		ownerStatus["enviado"][name] = status(p.maria, path)
		require.NotEqual(t, 404, ownerStatus["enviado"][name], "titular em %s do enviado", name)
	}

	idsOf := func(tok string) []string {
		r := p.do("GET", "/api/admin/projects", nil, tok)
		require.Equal(t, 200, r.Status, r.Body)
		var ids []string
		for _, it := range r.list() {
			ids = append(ids, it.(map[string]any)["id"].(string))
		}
		return ids
	}

	for _, role := range []string{"analyst", "administrative-supervisor"} {
		t.Run(role, func(t *testing.T) {
			tok, _ := p.newAdmin(p.boss, role+"@exemplo.com", role)

			ids := idsOf(tok)
			assert.Contains(t, ids, sent.id, "a equipe lista o projeto enviado")
			assert.NotContains(t, ids, draft.id, "a equipe não lista rascunho")

			for name, path := range routes(draft.id, draftFile, false) {
				assert.Equal(t, 404, status(tok, path), "rascunho: %s", name)
			}
			for name, path := range routes(sent.id, sentFile, true) {
				assert.Equal(t, ownerStatus["enviado"][name], status(tok, path), "enviado: %s", name)
			}
		})
	}
}
