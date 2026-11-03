package app_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func membersPath(id string, rest ...string) string {
	return "/api/admin/projects/" + id + "/members" + strings.Join(rest, "")
}

// invite convida o CPF do e-mail dado (a pessoa pode ainda não ter conta).
func (p *projEnv) invite(tok, id, email, level string) resp {
	p.t.Helper()
	return p.do("POST", membersPath(id), map[string]any{"cpf": cpfFor(email), "level": level, "label": "Convidado " + email}, tok)
}

func (p *projEnv) mustInvite(id, email, level string) string {
	p.t.Helper()
	r := p.invite(p.maria, id, email, level)
	require.Equal(p.t, 201, r.Status, r.Body)
	return r.Body["data"].(map[string]any)["id"].(string)
}

func TestConvitePendenteValeNoPrimeiroLogin(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	memberID := p.mustInvite(id, "carla@exemplo.com", "edit")

	// A resposta mascara o CPF, não revela se há conta e mostra o convite como pendente.
	list := p.do("GET", membersPath(id), nil, p.maria).list()
	require.Len(t, list, 1)
	m := list[0].(map[string]any)
	assert.Equal(t, memberID, m["id"])
	assert.Regexp(t, `^\*\*\*\.\d{3}\.\d{3}-\*\*$`, m["cpf"])
	assert.NotContains(t, fmt.Sprint(m), cpfFor("carla@exemplo.com"))
	assert.Equal(t, "edit", m["level"])
	assert.Nil(t, m["acceptedAt"])

	// Primeiro login com aquele CPF: o projeto já aparece, com o papel de edição.
	carla := p.login("carla@exemplo.com")
	rows := p.do("GET", "/api/admin/projects", nil, carla).list()
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	assert.Equal(t, id, row["id"])
	assert.Equal(t, "edit", row["access"])
	assert.Equal(t, false, row["isOwner"])
	assert.Equal(t, "owner", p.do("GET", "/api/admin/projects", nil, p.maria).list()[0].(map[string]any)["access"])

	// Abrir o projeto marca o convite como aceito.
	got := p.project(carla, id)
	assert.Equal(t, "edit", got.data()["access"])
	assert.NotNil(t, p.do("GET", membersPath(id), nil, p.maria).list()[0].(map[string]any)["acceptedAt"])
}

func TestNiveisDeAcessoDoMembro(t *testing.T) {
	p := newProjEnv(t)
	fp := p.completeProject("5000/2026")
	p.mustInvite(fp.id, "leitor@exemplo.com", "read")
	p.mustInvite(fp.id, "editor@exemplo.com", "edit")
	reader, editor := p.login("leitor@exemplo.com"), p.login("editor@exemplo.com")
	base := "/api/admin/projects/" + fp.id

	// Leitura: vê tudo, inclusive o CPF completo do proponente e os arquivos, mas não altera nada.
	for _, tok := range []string{reader, editor} {
		got := p.project(tok, fp.id)
		assert.Equal(t, cpfFor("maria@exemplo.com"), got.data()["proponent"].(map[string]any)["cpf"])
		assert.Equal(t, 200, p.do("GET", base+"/documents", nil, tok).Status)
	}
	version := fp.current(p.maria)
	readerDenied := map[string]resp{
		"proposta":   p.do("PUT", base+"/proposal", proposalBody(version), reader),
		"documentos": p.do("POST", base+"/documents/requerimento_apoio/files", map[string]any{"filename": "a.pdf", "contentType": pdfType, "sizeBytes": 10}, reader),
		"enviar":     p.do("POST", base+"/submit", map[string]any{"version": version, "declaration": true}, reader),
	}
	for name, r := range readerDenied {
		assert.Equal(t, 403, r.Status, "leitor não pode: %s", name)
	}
	// Nenhum dos dois: excluir, desistir, duplicar, gerir acesso.
	for _, tok := range []string{reader, editor} {
		assert.Equal(t, 403, p.do("DELETE", base, nil, tok).Status)
		assert.Equal(t, 403, p.do("POST", base+"/withdraw", map[string]any{"note": "motivo qualquer"}, tok).Status)
		assert.Contains(t, []int{403, 404}, p.do("POST", base+"/duplicate", map[string]any{}, tok).Status)
		assert.Equal(t, 403, p.do("GET", membersPath(fp.id), nil, tok).Status)
		assert.Equal(t, 403, p.invite(tok, fp.id, "terceiro@exemplo.com", "read").Status)
	}

	// Edição: altera, anexa e envia.
	fp.upload(editor, "requerimento_apoio", "")
	pb := proposalBody(version)
	pb["sigconNumber"], pb["objectNature"], pb["title"] = "5000/2026", "services", "Título pelo editor"
	r := p.do("PUT", base+"/proposal", pb, editor)
	require.Equal(t, 200, r.Status, r.Body)
	assert.Equal(t, "Título pelo editor", p.project(p.maria, fp.id).data()["proposal"].(map[string]any)["title"], "o titular vê o que o editor gravou")
	require.Equal(t, 200, p.do("PUT", base+"/documents/requerimento_apoio/justification", map[string]any{"text": ""}, editor).Status)
	sent := fp.submit(editor, true)
	require.Equal(t, 200, sent.Status, sent.Body)
	assert.Equal(t, "submitted", sent.data()["status"])

	// Depois do envio o leitor continua lendo e baixando o comprovante.
	status, _, pdf := p.raw("GET", base+"/receipt", reader)
	assert.Equal(t, 200, status)
	assert.True(t, strings.HasPrefix(string(pdf), "%PDF-"))
}

func TestSoOTitularGereOAcesso(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	mid := p.mustInvite(id, "editor@exemplo.com", "edit")
	editor := p.login("editor@exemplo.com")
	analyst := p.analyst() // lê tudo, mas sem a permissão de gerir acesso

	for name, tok := range map[string]string{"editor convidado": editor, "outro proponente": p.joao, "equipe sem permissão": analyst} {
		assert.Contains(t, []int{403, 404}, p.do("GET", membersPath(id), nil, tok).Status, name)
		assert.Contains(t, []int{403, 404}, p.invite(tok, id, "intruso@exemplo.com", "edit").Status, name)
		assert.Contains(t, []int{403, 404}, p.do("PUT", membersPath(id, "/", mid), map[string]any{"level": "read"}, tok).Status, name)
		assert.Contains(t, []int{403, 404}, p.do("DELETE", membersPath(id, "/", mid), nil, tok).Status, name)
	}
	assert.Len(t, p.do("GET", membersPath(id), nil, p.maria).list(), 1, "nada mudou")
	// O titular de outro projeto não gere o acesso deste.
	other, _ := p.create(p.joao)
	assert.Contains(t, []int{403, 404}, p.do("POST", membersPath(id), map[string]any{"cpf": cpfFor("x@exemplo.com"), "level": "read"}, p.joao).Status)
	assert.Equal(t, 201, p.do("POST", membersPath(other), map[string]any{"cpf": cpfFor("x@exemplo.com"), "level": "read"}, p.joao).Status)
}

func TestRevogacaoValeComASessaoAberta(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	mid := p.mustInvite(id, "carla@exemplo.com", "edit")
	carla := p.login("carla@exemplo.com")
	require.Equal(t, 200, p.do("GET", "/api/admin/projects/"+id, nil, carla).Status)
	require.Len(t, p.do("GET", "/api/admin/projects", nil, carla).list(), 1)

	require.Equal(t, 204, p.do("DELETE", membersPath(id, "/", mid), nil, p.maria).Status)
	// Mesmo token, próxima requisição: sem acesso nenhum.
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+id, nil, carla).Status)
	assert.Empty(t, p.do("GET", "/api/admin/projects", nil, carla).list())
	assert.Contains(t, []int{403, 404}, p.do("PUT", "/api/admin/projects/"+id+"/proposal", proposalBody(1), carla).Status)
	assert.Empty(t, p.do("GET", membersPath(id), nil, p.maria).list())
	// Revogar de novo ou um id que não é do projeto é 404.
	assert.Equal(t, 404, p.do("DELETE", membersPath(id, "/", mid), nil, p.maria).Status)
	assert.Equal(t, 404, p.do("DELETE", membersPath(id, "/00000000-0000-0000-0000-000000000000"), nil, p.maria).Status)

	// Convidar de novo reativa o acesso (e o convite volta a pendente).
	again := p.invite(p.maria, id, "carla@exemplo.com", "read")
	require.Equal(t, 201, again.Status, again.Body)
	assert.Equal(t, mid, again.data()["id"], "reaproveita o mesmo registro")
	assert.Nil(t, again.data()["acceptedAt"])
	assert.Equal(t, "read", p.project(carla, id).data()["access"])
}

func TestRegrasDoConvite(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	mid := p.mustInvite(id, "carla@exemplo.com", "read")

	dup := p.invite(p.maria, id, "carla@exemplo.com", "edit")
	assert.Equal(t, 409, dup.Status)
	assert.Equal(t, "member_exists", dup.errCode())
	// Formatação do CPF não escapa da checagem de duplicidade.
	c := cpfFor("carla@exemplo.com")
	formatted := fmt.Sprintf("%s.%s.%s-%s", c[0:3], c[3:6], c[6:9], c[9:])
	assert.Equal(t, 409, p.do("POST", membersPath(id), map[string]any{"cpf": formatted, "level": "read"}, p.maria).Status)

	assert.Equal(t, 422, p.do("POST", membersPath(id), map[string]any{"cpf": cpfFor("maria@exemplo.com"), "level": "read"}, p.maria).Status, "o titular não se convida")
	for name, body := range map[string]map[string]any{
		"cpf inválido":   {"cpf": "11111111111", "level": "read"},
		"cpf curto":      {"cpf": "123", "level": "read"},
		"sem cpf":        {"level": "read"},
		"nível inválido": {"cpf": cpfFor("z@exemplo.com"), "level": "owner"},
		"sem nível":      {"cpf": cpfFor("z@exemplo.com")},
		"campo estranho": {"cpf": cpfFor("z@exemplo.com"), "level": "read", "status": "active", "invitedById": "x"},
		"rótulo enorme":  {"cpf": cpfFor("z@exemplo.com"), "level": "read", "label": strings.Repeat("a", 101)},
		"rótulo com NUL": {"cpf": cpfFor("z@exemplo.com"), "level": "read", "label": "a\u0000b"},
	} {
		r := p.do("POST", membersPath(id), body, p.maria)
		assert.Contains(t, []int{400, 422}, r.Status, name)
	}

	// Alterar o nível e a identificação.
	up := p.do("PUT", membersPath(id, "/", mid), map[string]any{"level": "edit", "label": "  Carla   contadora "}, p.maria)
	require.Equal(t, 200, up.Status, up.Body)
	assert.Equal(t, "edit", up.data()["level"])
	assert.Equal(t, "Carla contadora", up.data()["label"])
	assert.Equal(t, 422, p.do("PUT", membersPath(id, "/", mid), map[string]any{"level": "dono"}, p.maria).Status)

	// Limite de pessoas por projeto.
	// (as vagas são preenchidas direto no banco: o convite em si tem limite de 20 por minuto)
	_, err := p.pool.Exec(t.Context(), `INSERT INTO project_members (project_id, cpf, level, invited_by_id)
		SELECT $1, lpad(g::text, 11, '0'), 'read', created_by_id FROM projects, generate_series(1, 9) g WHERE id = $1`, id)
	require.NoError(t, err)
	full := p.invite(p.maria, id, "pessoa11@exemplo.com", "read")
	assert.Equal(t, 409, full.Status)
	assert.Equal(t, "members_limit", full.errCode())
	require.Equal(t, 204, p.do("DELETE", membersPath(id, "/", mid), nil, p.maria).Status)
	assert.Equal(t, 201, p.invite(p.maria, id, "pessoa11@exemplo.com", "read").Status, "revogar libera vaga")
}

func TestMembroSoVeOsProjetosEmQueFoiConvidado(t *testing.T) {
	p := newProjEnv(t)
	shared, _ := p.create(p.maria)
	private, _ := p.create(p.maria)
	p.mustInvite(shared, "carla@exemplo.com", "read")
	carla := p.login("carla@exemplo.com")

	rows := p.do("GET", "/api/admin/projects", nil, carla).list()
	require.Len(t, rows, 1)
	assert.Equal(t, shared, rows[0].(map[string]any)["id"])
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+private, nil, carla).Status)
	assert.Contains(t, []int{403, 404}, p.do("PUT", "/api/admin/projects/"+private+"/proposal", proposalBody(1), carla).Status)
	// O membro cria os próprios projetos normalmente, e Maria não os vê.
	mine, _ := p.create(carla)
	assert.Equal(t, 404, p.do("GET", "/api/admin/projects/"+mine, nil, p.maria).Status)
	assert.Len(t, p.do("GET", "/api/admin/projects", nil, carla).list(), 2)
}

func TestTitularEEditorGravandoAoMesmoTempo(t *testing.T) {
	p := newProjEnv(t)
	id, v := p.create(p.maria)
	p.mustInvite(id, "editor@exemplo.com", "edit")
	editor := p.login("editor@exemplo.com")

	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i, tok := range []string{p.maria, editor} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := proposalBody(v)
			b["title"] = fmt.Sprintf("Versão %d", i)
			statuses <- p.do("PUT", "/api/admin/projects/"+id+"/proposal", b, tok).Status
		}()
	}
	wg.Wait()
	close(statuses)
	got := map[int]int{}
	for s := range statuses {
		got[s]++
	}
	assert.Equal(t, map[int]int{200: 1, 409: 1}, got, "quem salva atrasado recebe conflito, não sobrescreve")
}

func TestAuditoriaDoCompartilhamento(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	mid := p.mustInvite(id, "carla@exemplo.com", "read")
	require.Equal(t, 200, p.do("PUT", membersPath(id, "/", mid), map[string]any{"level": "edit"}, p.maria).Status)
	require.Equal(t, 204, p.do("DELETE", membersPath(id, "/", mid), nil, p.maria).Status)

	for action, n := range map[string]int{"project.member-invited": 1, "project.member-updated": 1, "project.member-revoked": 1} {
		logs := p.do("GET", "/api/admin/audit-logs?action="+action+"&entityId="+id, nil, p.boss).list()
		require.Len(t, logs, n, action)
		entry := logs[0].(map[string]any)
		assert.Equal(t, "admin", entry["actorType"])
		assert.Equal(t, mid, entry["payload"].(map[string]any)["member"])
		assert.NotContains(t, fmt.Sprint(entry), cpfFor("carla@exemplo.com"), "a auditoria guarda o CPF mascarado")
		assert.Regexp(t, `^\*\*\*\.\d{3}\.\d{3}-\*\*$`, entry["payload"].(map[string]any)["cpf"])
	}
	// Recusa não gera registro.
	before := len(p.do("GET", "/api/admin/audit-logs?action=project.member-invited", nil, p.boss).list())
	p.invite(p.joao, id, "intruso@exemplo.com", "edit")
	assert.Len(t, p.do("GET", "/api/admin/audit-logs?action=project.member-invited", nil, p.boss).list(), before)
}

func TestMembroEditaDuranteAComplementacao(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	fp := p.submitted("5100/2026")
	p.mustInvite(fp.id, "editor@exemplo.com", "edit")
	p.mustInvite(fp.id, "leitor@exemplo.com", "read")
	editor, reader := p.login("editor@exemplo.com"), p.login("leitor@exemplo.com")
	require.Equal(t, 200, p.complement(p.analyst(), fp.id, map[string]any{"items": []any{section("proposal", "Ajustar o objeto")}}).Status)

	base := "/api/admin/projects/" + fp.id
	b := proposalBody(fp.current(editor))
	b["sigconNumber"], b["objectNature"], b["title"] = "5100/2026", "services", "Corrigido pelo editor"
	assert.Equal(t, 403, p.do("PUT", base+"/proposal", b, reader).Status)
	r := p.do("PUT", base+"/proposal", b, editor)
	require.Equal(t, 200, r.Status, r.Body)
	re := p.do("POST", base+"/resubmit", map[string]any{"version": fp.current(editor), "declaration": true}, editor)
	require.Equal(t, 200, re.Status, re.Body)
	assert.Equal(t, "in_analysis", re.data()["stage"])
	// Desistir continua sendo só do titular.
	assert.Equal(t, 403, p.do("POST", base+"/withdraw", map[string]any{"note": "motivo qualquer"}, editor).Status)
	assert.Equal(t, 200, p.do("POST", base+"/withdraw", map[string]any{"note": "desisti"}, p.maria).Status)
	// Arquivado: o membro ainda lê, mas não altera.
	assert.Equal(t, 200, p.do("GET", base, nil, reader).Status)
}

func (p *projEnv) inviteWith(id, email, level string, extra map[string]any) resp {
	p.t.Helper()
	body := map[string]any{"cpf": cpfFor(email), "level": level}
	for k, v := range extra {
		body[k] = v
	}
	return p.do("POST", membersPath(id), body, p.maria)
}

func (p *projEnv) mailsTo(addr string) []string {
	var out []string
	for _, m := range p.mail.Msgs {
		if m.To == addr {
			out = append(out, m.Body)
		}
	}
	return out
}

func TestAvisoDoConviteVaiParaOEmailInformado(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	r := p.inviteWith(id, "novo@exemplo.com", "edit", map[string]any{"email": "  Novo.Convidado@Exemplo.com "})
	require.Equal(t, 201, r.Status, r.Body)
	// O e-mail volta mascarado.
	assert.Equal(t, "n***@exemplo.com", r.data()["email"])
	assert.NotContains(t, fmt.Sprint(r.Body), "novo.convidado")

	bodies := p.mailsTo("novo.convidado@exemplo.com")
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "para consultar e editar")
	assert.Contains(t, bodies[0], "/dashboard/projetos/"+id)
	assert.Contains(t, bodies[0], "MT Login")
	assert.NotContains(t, bodies[0], cpfFor("novo@exemplo.com"), "o aviso só traz o CPF mascarado")
	assert.Regexp(t, `\*\*\*\.\d{3}\.\d{3}-\*\*`, bodies[0])
	assert.Contains(t, bodies[0], "Maria", "diz quem deu o acesso")

	// Reconvite depois da revogação avisa de novo, com o nível novo.
	mid := r.data()["id"].(string)
	require.Equal(t, 204, p.do("DELETE", membersPath(id, "/", mid), nil, p.maria).Status)
	require.Equal(t, 201, p.inviteWith(id, "novo@exemplo.com", "read", map[string]any{"email": "novo.convidado@exemplo.com"}).Status)
	bodies = p.mailsTo("novo.convidado@exemplo.com")
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[1], "somente leitura")
}

func TestAvisoDeQuemJaTemContaVaiParaOEmailDaConta(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	p.login("carla@exemplo.com") // carla já tem conta
	// Mesmo que o titular digite outro e-mail, o aviso vai para o da conta, e o digitado nem é usado.
	r := p.inviteWith(id, "carla@exemplo.com", "read", map[string]any{"email": "outro@exemplo.com"})
	require.Equal(t, 201, r.Status, r.Body)
	require.Len(t, p.mailsTo("carla@exemplo.com"), 1)
	assert.Empty(t, p.mailsTo("outro@exemplo.com"))
	// E sem informar e-mail nenhum, o aviso também chega. O titular não vê o e-mail da conta em lugar nenhum.
	id2, _ := p.create(p.maria)
	r = p.inviteWith(id2, "carla@exemplo.com", "read", nil)
	require.Equal(t, 201, r.Status)
	assert.Len(t, p.mailsTo("carla@exemplo.com"), 2)
	assert.Equal(t, "", r.data()["email"])
	assert.NotContains(t, fmt.Sprint(p.do("GET", membersPath(id2), nil, p.maria).Body), "carla@exemplo.com")
}

func TestConviteSemEmailNemContaNaoAvisaMasFunciona(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	before := len(p.mail.Msgs)
	r := p.inviteWith(id, "fantasma@exemplo.com", "read", nil)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Len(t, p.mail.Msgs, before, "ninguém para avisar")
	// O acesso vale quando a pessoa entrar.
	assert.Equal(t, id, p.do("GET", "/api/admin/projects", nil, p.login("fantasma@exemplo.com")).list()[0].(map[string]any)["id"])
}

func TestEmailDoConviteInvalido(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	for _, e := range []string{"sem-arroba", "a@b", "a b@exemplo.com", "x@exemplo.com\nBcc: alvo@exemplo.com", "Nome <a@exemplo.com>", strings.Repeat("a", 250) + "@exemplo.com"} {
		r := p.inviteWith(id, "novo@exemplo.com", "read", map[string]any{"email": e})
		assert.Contains(t, []int{400, 422}, r.Status, "%q", e)
	}
	assert.Empty(t, p.do("GET", membersPath(id), nil, p.maria).list(), "convite recusado não grava nada")
	assert.Empty(t, p.mail.Msgs)
}

func TestContaBloqueadaNaoRecebeAviso(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	p.login("bloqueada@exemplo.com")
	_, err := p.pool.Exec(t.Context(), "UPDATE admins SET blocked = true WHERE cpf = $1", cpfFor("bloqueada@exemplo.com"))
	require.NoError(t, err)
	require.Equal(t, 201, p.inviteWith(id, "bloqueada@exemplo.com", "read", nil).Status)
	assert.Empty(t, p.mailsTo("bloqueada@exemplo.com"))
}

func TestConvitesTemLimiteDeTaxaPorUsuario(t *testing.T) {
	p := newProjEnv(t)
	id, _ := p.create(p.maria)
	statuses := map[int]int{}
	for range 25 {
		statuses[p.do("POST", membersPath(id), map[string]any{"cpf": "123", "level": "read"}, p.maria).Status]++
	}
	assert.Equal(t, 20, statuses[422], "20 por minuto")
	assert.Equal(t, 5, statuses[429])
	// Outra pessoa não é afetada.
	other, _ := p.create(p.joao)
	assert.Equal(t, 201, p.do("POST", membersPath(other), map[string]any{"cpf": cpfFor("x@exemplo.com"), "level": "read"}, p.joao).Status)
}

// access restringe a listagem ao que é da pessoa: titular, membro, ou um dos dois. Soma-se à permissão, nunca a amplia.
func TestFiltroDeAcessoDaListagem(t *testing.T) {
	p := newProjEnv(t)
	p.setElectoralDeadline(time.Now().AddDate(0, 0, 30))
	mariaOwn, _ := p.create(p.maria)
	shared, _ := p.create(p.maria)
	mid := p.mustInvite(shared, "carla@exemplo.com", "read")
	carla := p.login("carla@exemplo.com")
	carlaOwn, _ := p.create(carla)

	ids := func(tok, query string) []string {
		r := p.do("GET", "/api/admin/projects"+query, nil, tok)
		require.Equal(t, 200, r.Status, r.Body)
		out := []string{}
		for _, it := range r.list() {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		return out
	}

	assert.ElementsMatch(t, []string{shared, carlaOwn}, ids(carla, ""))
	assert.ElementsMatch(t, []string{shared, carlaOwn}, ids(carla, "?access=all"))
	assert.Equal(t, []string{carlaOwn}, ids(carla, "?access=owner"))
	assert.Equal(t, []string{shared}, ids(carla, "?access=member"))
	assert.ElementsMatch(t, []string{mariaOwn, shared}, ids(p.maria, "?access=owner"))
	assert.Empty(t, ids(p.maria, "?access=member"), "o titular não é membro dos próprios projetos")
	// Combina com os outros filtros e com o total da paginação.
	assert.Empty(t, ids(carla, "?access=member&status=submitted"))
	assert.EqualValues(t, 1, p.do("GET", "/api/admin/projects?access=member", nil, carla).meta()["total"])

	// Revogar o acesso tira o projeto de "membro" na hora.
	require.Equal(t, 204, p.do("DELETE", membersPath(shared, "/", mid), nil, p.maria).Status)
	assert.Empty(t, ids(carla, "?access=member"))
	assert.Equal(t, []string{carlaOwn}, ids(carla, "?access=all"))

	for _, bad := range []string{"mine", "OWNER", "owner,member"} {
		r := p.do("GET", "/api/admin/projects?access="+bad, nil, carla)
		assert.Equal(t, 422, r.Status, bad)
	}

	// A equipe enxerga os projetos enviados de todo mundo, mas com access só o que é dela (nada).
	sent := p.submitted("7101/2026")
	tok, _ := p.newAdmin(p.boss, "analista-acesso@exemplo.com", "analyst")
	assert.Contains(t, ids(tok, ""), sent.id)
	for _, a := range []string{"owner", "member", "all"} {
		assert.Empty(t, ids(tok, "?access="+a), "analista com access=%s", a)
	}
}
