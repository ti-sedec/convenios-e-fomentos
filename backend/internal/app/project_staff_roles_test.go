package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staffRoles cria um administrador para cada perfil de equipe SEMEADO (os mesmos que existem em produção, não perfis ad hoc).
type staffRoles struct {
	analyst, supervisor, secretary                        string
	reviewerSame, reviewerOther, reviewerNone, superAdmin string
}

// newStaff cria os perfis. O Revisor "do mesmo departamento" recebe Financeiro; o "de outro" recebe Jurídico; o "sem departamento"
// fica sem nenhum, e por isso não pode enxergar projeto algum.
func (p *projEnv) newStaff() staffRoles {
	p.t.Helper()
	fin, jur := p.deptID("Financeiro"), p.deptID("Jurídico")
	mk := func(email, role, dept string) string {
		tok, id := p.newAdmin(p.boss, email, role)
		if dept != "" {
			require.Equal(p.t, 200, p.do("PUT", "/api/admin/users/"+id, map[string]any{"departmentId": dept}, p.boss).Status)
		}
		return tok
	}
	return staffRoles{
		analyst:       mk("m.analista@exemplo.com", "analyst", ""),
		supervisor:    mk("m.supervisor@exemplo.com", "administrative-supervisor", ""),
		secretary:     mk("m.secretario@exemplo.com", "state-secretary", ""),
		reviewerSame:  mk("m.revisor.fin@exemplo.com", "department-reviewer", fin),
		reviewerOther: mk("m.revisor.jur@exemplo.com", "department-reviewer", jur),
		reviewerNone:  mk("m.revisor.sem@exemplo.com", "department-reviewer", ""),
		superAdmin:    p.boss,
	}
}

// setProjectDepartment define o departamento do projeto direto no banco: a rota que o grava (indicação pelo Analista) só existe
// numa fase posterior.
func (p *projEnv) setProjectDepartment(projectID, deptID string) {
	p.t.Helper()
	_, err := p.pool.Exec(context.Background(), "UPDATE projects SET department_id = $2 WHERE id = $1", projectID, deptID)
	require.NoError(p.t, err)
}

func (p *projEnv) status(tok, path string) int {
	p.t.Helper()
	code, _, _ := p.raw("GET", path, tok)
	return code
}

// Matriz de leitura: cada perfil × cada situação de projeto × cada rota de leitura. Rascunho é só do titular; a equipe lê enviado e
// arquivado; o Revisor só os do próprio departamento; quem não tem departamento não enxerga nada.
func TestMatrizDeLeituraDaEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fin := p.deptID("Financeiro")

	type project struct {
		id, file string
	}
	draftFP := p.completeProject("7101-2026")
	draft := project{draftFP.id, draftFP.upload(p.maria, "requerimento_apoio", "")}
	fileOf := func(fp *fundingProject) string {
		items, _ := fp.checklist(p.maria)
		return items["requerimento_apoio"]["files"].([]any)[0].(map[string]any)["id"].(string)
	}
	sentFP := p.submitted("7102-2026")
	sent := project{sentFP.id, fileOf(sentFP)}
	archivedFP := p.submitted("7103-2026")
	archived := project{archivedFP.id, fileOf(archivedFP)}
	require.Equal(t, 200, p.do("POST", "/api/admin/projects/"+archived.id+"/archive",
		map[string]any{"reason": "missing_documents", "note": "Faltaram documentos obrigatórios."}, staff.analyst).Status)
	p.setProjectDepartment(sent.id, fin)
	p.setProjectDepartment(archived.id, fin)

	paths := func(pr project) map[string]string {
		return map[string]string{
			"detalhe":   "/api/admin/projects/" + pr.id,
			"checklist": docsPath(pr.id),
			"download":  docsPath(pr.id, "/files/", pr.file, "/download"),
		}
	}
	ids := func(tok string) []string {
		r := p.do("GET", "/api/admin/projects", nil, tok)
		require.Equal(t, 200, r.Status, r.Body)
		var out []string
		for _, it := range r.list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}

	cases := []struct {
		name    string
		tok     string
		canSee  map[string]bool // por situação: draft, sent, archived
		comment string
		// forbidden: a condição nega tudo (Revisor sem departamento), então o guard da rota responde 403 em vez de uma lista vazia.
		forbidden bool
	}{
		{"titular", p.maria, map[string]bool{"draft": true, "sent": true, "archived": true}, "vê os próprios", false},
		{"proponente alheio", p.joao, map[string]bool{}, "não vê projeto de outro proponente", false},
		{"analista", staff.analyst, map[string]bool{"sent": true, "archived": true}, "só enviado e arquivado", false},
		{"supervisor", staff.supervisor, map[string]bool{"sent": true, "archived": true}, "só enviado e arquivado", false},
		{"secretário", staff.secretary, map[string]bool{"sent": true, "archived": true}, "só enviado e arquivado", false},
		{"revisor do mesmo departamento", staff.reviewerSame, map[string]bool{"sent": true, "archived": true}, "só os do seu departamento", false},
		{"revisor de outro departamento", staff.reviewerOther, map[string]bool{}, "nenhum", false},
		{"revisor sem departamento", staff.reviewerNone, map[string]bool{}, "nenhum", true},
		{"super admin", staff.superAdmin, map[string]bool{"draft": true, "sent": true, "archived": true}, "curinga", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.forbidden {
				assert.Equal(t, 403, p.do("GET", "/api/admin/projects", nil, c.tok).Status, "lista")
				for _, pr := range []project{draft, sent, archived} {
					for route, path := range paths(pr) {
						assert.Equal(t, 403, p.status(c.tok, path), route)
					}
				}
				return
			}
			list := ids(c.tok)
			for situation, pr := range map[string]project{"draft": draft, "sent": sent, "archived": archived} {
				want := c.canSee[situation]
				assert.Equal(t, want, contains(list, pr.id), "lista / %s (%s)", situation, c.comment)
				for route, path := range paths(pr) {
					got := p.status(c.tok, path)
					if want {
						assert.NotEqual(t, 404, got, "%s / %s: deveria alcançar", situation, route)
						assert.Less(t, got, 400, "%s / %s", situation, route)
					} else {
						assert.Equal(t, 404, got, "%s / %s: não deveria existir para este perfil", situation, route)
					}
				}
			}
		})
	}
}

func contains(list []string, id string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}

// Matriz de decisão: quem pode devolver, arquivar e negar. O Supervisor só acompanha; o Analista devolve e arquiva; o Secretário
// nega; o Revisor não decide nada nesta fase.
func TestMatrizDeDecisaoDaEquipe(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	staff := p.newStaff()
	fin := p.deptID("Financeiro")

	shared := p.submitted("7201-2026") // recebe só tentativas que devem ser recusadas, então não muda de estado
	p.setProjectDepartment(shared.id, fin)

	complement := func(tok, id string) int {
		return p.complement(tok, id, map[string]any{"items": []any{section("institution", "CEP")}}).Status
	}
	archive := func(tok, id string) int {
		return p.do("POST", "/api/admin/projects/"+id+"/archive", map[string]any{"reason": "missing_documents", "note": "Faltam documentos."}, tok).Status
	}
	deny := func(tok, id string) int {
		return p.do("POST", "/api/admin/projects/"+id+"/deny", map[string]any{"note": "Fora das prioridades."}, tok).Status
	}

	t.Run("quem não decide recebe 403 em tudo", func(t *testing.T) {
		for name, tok := range map[string]string{
			"supervisor": staff.supervisor, "revisor do mesmo departamento": staff.reviewerSame,
			"revisor de outro departamento": staff.reviewerOther, "revisor sem departamento": staff.reviewerNone,
		} {
			assert.Equal(t, 403, complement(tok, shared.id), "%s: devolver", name)
			assert.Equal(t, 403, archive(tok, shared.id), "%s: arquivar", name)
			assert.Equal(t, 403, deny(tok, shared.id), "%s: negar", name)
		}
	})
	t.Run("a equipe só lê o cadastro: nenhuma seção é gravada por ela", func(t *testing.T) {
		for name, tok := range map[string]string{
			"analista": staff.analyst, "secretário": staff.secretary, "supervisor": staff.supervisor,
			"revisor do mesmo departamento": staff.reviewerSame,
		} {
			for _, section := range []string{"proponent", "institution", "proposal", "funding", "document-options"} {
				assert.Equal(t, 403, p.do("PUT", "/api/admin/projects/"+shared.id+"/"+section, map[string]any{}, tok).Status,
					"%s: %s", name, section)
			}
		}
	})
	t.Run("o Analista não nega e o Secretário não devolve nem arquiva", func(t *testing.T) {
		assert.Equal(t, 403, deny(staff.analyst, shared.id))
		assert.Equal(t, 403, complement(staff.secretary, shared.id))
		assert.Equal(t, 403, archive(staff.secretary, shared.id))
	})
	t.Run("o Analista devolve", func(t *testing.T) {
		assert.Equal(t, 200, complement(staff.analyst, p.submitted("7202-2026").id))
	})
	t.Run("o Analista arquiva", func(t *testing.T) {
		assert.Equal(t, 200, archive(staff.analyst, p.submitted("7203-2026").id))
	})
	t.Run("o Secretário nega", func(t *testing.T) {
		assert.Equal(t, 200, deny(staff.secretary, p.submitted("7204-2026").id))
	})
	t.Run("ninguém da equipe age sobre rascunho", func(t *testing.T) {
		draft := p.completeProject("7205-2026")
		assert.Equal(t, 403, complement(staff.analyst, draft.id))
		assert.Equal(t, 403, archive(staff.analyst, draft.id))
		assert.Equal(t, 403, deny(staff.secretary, draft.id))
	})
}

// Quem acumula Proponente e Analista (as permissões dos perfis se somam) continua vendo o próprio rascunho, mas não o de outro
// proponente: a condição de visibilidade da equipe não tira o acesso que o perfil de Proponente já dá.
func TestPerfilDeEquipeSomadoAoDeProponente(t *testing.T) {
	p := newProjEnv(t)
	own, _ := p.create(p.maria)
	others, _ := p.create(p.joao)

	_, err := p.pool.Exec(context.Background(),
		"INSERT INTO admin_users_roles (admin_id, role_id) VALUES ($1, $2)", p.adminID("maria@exemplo.com"), p.roleID("analyst"))
	require.NoError(t, err)

	r := p.do("GET", "/api/admin/projects", nil, p.maria)
	require.Equal(t, 200, r.Status, r.Body)
	var ids []string
	for _, it := range r.list() {
		ids = append(ids, it.(map[string]any)["id"].(string))
	}
	assert.Contains(t, ids, own, "o próprio rascunho continua visível")
	assert.NotContains(t, ids, others, "rascunho de outro proponente não aparece, mesmo sendo Analista")
	assert.Equal(t, 200, p.status(p.maria, "/api/admin/projects/"+own))
	assert.Equal(t, 404, p.status(p.maria, "/api/admin/projects/"+others))
}
