package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wagner/convenios-e-fomentos/backend/internal/admin"
	"github.com/wagner/convenios-e-fomentos/backend/internal/app"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
	"github.com/wagner/convenios-e-fomentos/backend/internal/testutil"
)

type env struct {
	t     *testing.T
	h     http.Handler
	pool  *pgxpool.Pool
	mail  *testutil.CaptureMailer
	cfg   *config.Config
	authz *authz.Engine
	app   *app.App
	oidc  *testutil.FakeOIDC
	store *storage.Memory
	mtid  *mtid.Memory
	conv  *fakeConverter
	// keepOutbox desliga o esvaziamento automático da caixa de saída depois de cada requisição (ver do).
	keepOutbox bool
}

type resp struct {
	Status int
	Body   map[string]any
}

func (r resp) data() map[string]any {
	d, _ := r.Body["data"].(map[string]any)
	return d
}

func (r resp) errCode() string {
	e, _ := r.Body["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *env {
	t.Helper()
	pool := testutil.NewPool(t)
	oidc := testutil.NewFakeOIDC(t)
	cfg := &config.Config{
		AppEnv: "test", HTTPPort: 8080,
		JWKSURI: oidc.JWKSURI(), OIDCIssuer: oidc.Issuer, OIDCClientID: oidc.ClientID, OIDCCPFClaim: oidc.CPFClaim,
		JWTAPISecret: "api-secret-de-teste-com-32-caracteres!!!", APITokenSalt: "salt-de-teste-com-32-caracteres-ok-123",
		AccessTokenTTL: 15 * time.Minute, RefreshTokenMaxTTL: time.Hour, RefreshTokenIdleTTL: 30 * time.Minute,
		ResetTokenTTL: 10 * time.Minute, EmailConfirmationTTL: time.Hour,
		RateLimitAuthPerMin: 1000, MaxBodyBytes: 1 << 20, PasswordMinLength: 8,
		Argon2MemoryKiB: 64, Argon2Iterations: 1, Argon2Parallel: 1,
		PublicURL: "http://app.test", CORSAllowedOrigins: []string{"http://localhost:3000"},
	}
	for _, m := range mutate {
		m(cfg)
	}
	mail := &testutil.CaptureMailer{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store := storage.NewMemory()
	signer := mtid.NewMemory()
	conv := &fakeConverter{}
	a, err := app.New(ctx, cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), pool, mail, app.WithStore(store), app.WithMTID(signer), app.WithPreviewConverter(conv), app.WithoutOutboxWorker(), app.WithoutReportWorker())
	require.NoError(t, err)
	require.NoError(t, a.Start(ctx))
	return &env{store: store, mtid: signer, conv: conv, t: t, h: a.Handler, pool: pool, mail: mail, cfg: cfg, authz: a.Authz, app: a, oidc: oidc}
}

func (e *env) do(method, path string, body any, token string) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, rd)
	req.RemoteAddr = "10.0.0.1:1234"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	e.drainOutbox()
	out := resp{Status: rec.Code}
	if rec.Body.Len() > 0 {
		require.NoError(e.t, json.Unmarshal(rec.Body.Bytes(), &out.Body), rec.Body.String())
	}
	return out
}

// drainOutbox envia agora os e-mails que a requisição deixou na caixa de saída. Em produção quem envia é um processo em segundo
// plano; nos testes ele não roda, e o esvaziamento depois de cada requisição mantém as asserções simples (ler p.mail logo em seguida).
// Os testes da própria caixa de saída ligam keepOutbox para controlar quando ela é esvaziada.
func (e *env) drainOutbox() {
	e.t.Helper()
	if e.keepOutbox {
		return
	}
	_, err := e.app.DrainOutbox(context.Background())
	require.NoError(e.t, err)
}

// cpfFor deriva, do e-mail, um CPF válido e estável: os testes identificam os administradores
// pelo e-mail, e o MT Login os identifica pelo CPF.
func cpfFor(email string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(email))
	base := fmt.Sprintf("%09d", h.Sum64()%1_000_000_000)
	if strings.Count(base, string(base[0])) == len(base) {
		base = "123456789"
	}
	for range 2 {
		sum := 0
		for i := range len(base) {
			sum += int(base[i]-'0') * (len(base) + 1 - i)
		}
		d := (sum * 10) % 11 % 10
		base += fmt.Sprint(d)
	}
	return base
}

// identity é o usuário do MT Login de um e-mail.
func identity(email string) testutil.Identity {
	name, _, _ := strings.Cut(email, "@")
	return testutil.Identity{CPF: cpf.Format(cpfFor(email)), Email: email, Given: strings.ToUpper(name[:1]) + name[1:], Family: "Teste"}
}

// token é o access token do MT Login do e-mail. O primeiro uso cria o administrador.
func (e *env) token(email string) string { return e.oidc.Token(identity(email)) }

// login usa o token do e-mail numa requisição autenticada (que cria ou atualiza o administrador).
func (e *env) login(email string) string {
	e.t.Helper()
	tok := e.token(email)
	require.Equal(e.t, 200, e.do("GET", "/api/admin/auth/me", nil, tok).Status)
	return tok
}

// registerFirst cria o primeiro Super Admin (ana@exemplo.com) como a CLI faz.
func (e *env) registerFirst() {
	e.t.Helper()
	_, err := app.AdminService(e.pool).EnsureSuperAdmin(context.Background(), adminInput("ana@exemplo.com"), "")
	require.NoError(e.t, err)
}

func adminInput(email string) admin.EnsureSuperAdminInput {
	return admin.EnsureSuperAdminInput{CPF: cpfFor(email)}
}

func TestPrimeiroAcessoCriaAdministradorSemRoles(t *testing.T) {
	e := newEnv(t)
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	require.Zero(t, n)
	// A migração já cria o Proponente (que o primeiro acesso receberia); aqui vale o cenário sem ele.
	_, err := e.pool.Exec(context.Background(), "DELETE FROM admin_roles WHERE code = 'proponent'")
	require.NoError(t, err)

	tok := e.token("maria@exemplo.com")
	me := e.do("GET", "/api/admin/auth/me", nil, tok)
	require.Equal(t, 200, me.Status, me.Body)
	assert.Equal(t, "maria@exemplo.com", me.data()["email"])
	assert.Equal(t, cpf.Format(cpfFor("maria@exemplo.com")), me.data()["cpf"], "o próprio usuário vê o CPF completo")
	assert.Equal(t, false, me.data()["isSuperAdmin"])
	assert.Empty(t, me.data()["roles"])
	assert.Empty(t, me.data()["permissions"])

	var active bool
	var cpfDB, first string
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT is_active, cpf, first_name FROM admins").Scan(&active, &cpfDB, &first))
	assert.True(t, active)
	assert.Equal(t, cpfFor("maria@exemplo.com"), cpfDB, "CPF gravado só com dígitos")
	assert.Equal(t, "Maria", first)

	// Sem permissões, nada do painel abre.
	assert.Equal(t, 403, e.do("GET", "/api/admin/users", nil, tok).Status)
	assert.Equal(t, 403, e.do("GET", "/api/admin/roles", nil, tok).Status)

	// Novos acessos reaproveitam o registro.
	e.do("GET", "/api/admin/auth/me", nil, tok)
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 1, n)
}

func TestPrimeiroAcessoRegistraAuditoriaComCPFMascarado(t *testing.T) {
	e := newEnv(t)
	e.login("maria@exemplo.com")
	var payload []byte
	var actor string
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT payload, actor_type FROM audit_logs WHERE action='admin.auto-provisioned'").Scan(&payload, &actor))
	assert.Equal(t, "system", actor)
	assert.Contains(t, string(payload), cpf.Mask(cpfFor("maria@exemplo.com")))
	assert.NotContains(t, string(payload), cpfFor("maria@exemplo.com"), "o CPF completo não vai para a auditoria")
}

func TestPrimeiroAcessoConcorrenteCriaUmSoRegistro(t *testing.T) {
	e := newEnv(t)
	tok := e.token("maria@exemplo.com")
	var wg sync.WaitGroup
	statuses := make(chan int, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- e.do("GET", "/api/admin/auth/me", nil, tok).Status
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		assert.Equal(t, 200, s)
	}
	var n, logs int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM audit_logs WHERE action='admin.auto-provisioned'").Scan(&logs))
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, logs)
}

func TestLoginAtualizaNomeEEmailDoMTLogin(t *testing.T) {
	e := newEnv(t)
	id := identity("maria@exemplo.com")
	e.do("GET", "/api/admin/auth/me", nil, e.oidc.Token(id))

	id.Email, id.Given, id.Family = "maria.nova@exemplo.com", "Maria José", "Souza"
	me := e.do("GET", "/api/admin/auth/me", nil, e.oidc.Token(id))
	require.Equal(t, 200, me.Status)
	assert.Equal(t, "maria.nova@exemplo.com", me.data()["email"])
	assert.Equal(t, "Maria José", me.data()["firstName"])
	assert.Equal(t, "Souza", me.data()["lastName"])

	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 1, n, "o CPF é a identidade: mudar o e-mail não cria outra conta")

	// O e-mail pode se repetir entre contas (a identidade é o CPF).
	other := identity("outra@exemplo.com")
	other.Email = "maria.nova@exemplo.com"
	assert.Equal(t, 200, e.do("GET", "/api/admin/auth/me", nil, e.oidc.Token(other)).Status)
}

func TestTokensRejeitados(t *testing.T) {
	e := newEnv(t)
	user := identity("maria@exemplo.com")
	tests := []struct {
		nome   string
		token  string
		status int
		code   string
	}{
		{"sem token", "", 401, "unauthorized"},
		{"lixo", "abc.def.ghi", 401, "unauthorized"},
		{"assinatura de outra chave", e.oidc.TokenSignedByOther(t, user), 401, "unauthorized"},
		{"expirado", e.oidc.TokenWith(user, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }), 401, "unauthorized"},
		{"emissor errado", e.oidc.TokenWith(user, func(c jwt.MapClaims) { c["iss"] = "https://outro/realms/x" }), 401, "unauthorized"},
		{"outro client", e.oidc.TokenWith(user, func(c jwt.MapClaims) { c["azp"] = "outro-app" }), 401, "unauthorized"},
		{"token de ID", e.oidc.TokenWith(user, func(c jwt.MapClaims) { c["typ"] = "ID" }), 401, "unauthorized"},
		{"sem CPF", e.oidc.TokenWith(user, func(c jwt.MapClaims) { delete(c, e.oidc.CPFClaim) }), 403, "invalid_identity"},
		{"CPF inválido", e.oidc.TokenWith(testutil.Identity{CPF: "111.111.111-11"}, nil), 403, "invalid_identity"},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			r := e.do("GET", "/api/admin/auth/me", nil, tt.token)
			assert.Equal(t, tt.status, r.Status)
			assert.Equal(t, tt.code, r.errCode())
		})
	}
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Zero(t, n, "token recusado nunca cria administrador")
}

func TestTokenDeUsuarioFinalNaoAbreOPainel(t *testing.T) {
	e := newEnv(t)
	access, _, _ := e.userToken("joao", "j@exemplo.com")
	assert.Equal(t, 401, e.do("GET", "/api/admin/auth/me", nil, access).Status)
}

func TestRotasDeSenhaESessaoLocaisNaoExistem(t *testing.T) {
	e := newEnv(t)
	for _, rt := range []struct{ method, path string }{
		{"POST", "/api/admin/auth/login"}, {"POST", "/api/admin/auth/refresh"}, {"POST", "/api/admin/auth/logout"},
		{"POST", "/api/admin/auth/forgot-password"}, {"POST", "/api/admin/auth/reset-password"}, {"POST", "/api/admin/auth/accept-invite"},
		{"POST", "/api/admin/auth/register-first"}, {"GET", "/api/admin/auth/sessions"}, {"POST", "/api/admin/users/invite"},
	} {
		assert.Contains(t, []int{404, 405}, e.do(rt.method, rt.path, map[string]any{}, e.token("ana@exemplo.com")).Status, rt.path)
	}
}

func TestAdminBloqueadoOuInativoPerdeAcessoImediatamente(t *testing.T) {
	e := newEnv(t)
	e.registerFirst()
	tok := e.login("ana@exemplo.com")

	_, err := e.pool.Exec(context.Background(), "UPDATE admins SET blocked = true")
	require.NoError(t, err)
	r := e.do("GET", "/api/admin/auth/me", nil, tok)
	assert.Equal(t, 403, r.Status)
	assert.Equal(t, "account_disabled", r.errCode())

	_, err = e.pool.Exec(context.Background(), "UPDATE admins SET blocked = false, is_active = false")
	require.NoError(t, err)
	assert.Equal(t, "account_disabled", e.do("GET", "/api/admin/auth/me", nil, tok).errCode())

	_, err = e.pool.Exec(context.Background(), "UPDATE admins SET is_active = true")
	require.NoError(t, err)
	assert.Equal(t, 200, e.do("GET", "/api/admin/auth/me", nil, tok).Status, "reativar devolve o acesso sem novo login")
}

func TestSuperAdminPorCPFEIdempotente(t *testing.T) {
	e := newEnv(t)
	svc := app.AdminService(e.pool)
	ctx := context.Background()
	in := adminInput("ana@exemplo.com")
	first, err := svc.EnsureSuperAdmin(ctx, in, "")
	require.NoError(t, err)
	again, err := svc.EnsureSuperAdmin(ctx, in, "")
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)

	_, err = svc.EnsureSuperAdmin(ctx, admin.EnsureSuperAdminInput{CPF: "123"}, "")
	assert.Error(t, err)

	// Quem já entrou como usuário comum é promovido, sem duplicar.
	e.login("beto@exemplo.com")
	promoted, err := svc.EnsureSuperAdmin(ctx, adminInput("beto@exemplo.com"), "")
	require.NoError(t, err)
	assert.Equal(t, true, e.do("GET", "/api/admin/auth/me", nil, e.token("beto@exemplo.com")).data()["isSuperAdmin"])
	var n int
	require.NoError(t, e.pool.QueryRow(ctx, "SELECT count(*) FROM admins WHERE id=$1", promoted.ID).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestPreCadastroPorCPFECompletadoNoPrimeiroAcesso(t *testing.T) {
	e := newEnv(t)
	e.registerFirst()
	boss := e.login("ana@exemplo.com")
	e.mkRole("redator")

	body := map[string]any{"cpf": cpf.Format(cpfFor("bia@exemplo.com")), "roleIds": []string{e.roleID("redator")}}
	assert.Equal(t, 401, e.do("POST", "/api/admin/users", body, "").Status)

	r := e.do("POST", "/api/admin/users", body, boss)
	require.Equal(t, 201, r.Status, r.Body)
	assert.Equal(t, "pending", r.data()["status"], "ainda não fez o primeiro acesso")
	assert.Equal(t, cpf.Mask(cpfFor("bia@exemplo.com")), r.data()["cpf"], "a API devolve o CPF mascarado")
	assert.Len(t, r.data()["roles"], 1)

	// Mesmo CPF, em qualquer formatação, não cadastra de novo.
	body["cpf"] = cpfFor("bia@exemplo.com")
	rep := e.do("POST", "/api/admin/users", body, boss)
	assert.Equal(t, 409, rep.Status)
	assert.Equal(t, "cpf_taken", rep.errCode())

	// No primeiro acesso, o registro existente é completado e já vem com a role.
	me := e.do("GET", "/api/admin/auth/me", nil, e.token("bia@exemplo.com"))
	require.Equal(t, 200, me.Status)
	assert.Equal(t, "bia@exemplo.com", me.data()["email"])
	assert.Equal(t, "Bia", me.data()["firstName"])
	roles := me.data()["roles"].([]any)
	require.Len(t, roles, 1)
	assert.Equal(t, "redator", roles[0].(map[string]any)["code"])

	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 2, n)
	got := e.do("GET", "/api/admin/users/"+e.adminID("bia@exemplo.com"), nil, boss)
	assert.Equal(t, "active", got.data()["status"])

	logs := e.do("GET", "/api/admin/audit-logs?action=admin.register", nil, boss)
	require.Len(t, logs.list(), 1)
	assert.NotContains(t, fmt.Sprint(logs.list()[0]), cpfFor("bia@exemplo.com"), "auditoria sem o CPF completo")
}

func TestPreCadastroValidaEntrada(t *testing.T) {
	e := newEnv(t)
	e.registerFirst()
	boss := e.login("ana@exemplo.com")
	e.mkRole("redator")
	role := []string{e.roleID("redator")}
	valid := cpfFor("bia@exemplo.com")

	assert.Equal(t, 422, e.do("POST", "/api/admin/users", map[string]any{"cpf": "111.111.111-11", "roleIds": role}, boss).Status, "CPF inválido")
	assert.Equal(t, 422, e.do("POST", "/api/admin/users", map[string]any{"cpf": valid, "roleIds": []string{}}, boss).Status, "sem roles")
	assert.Equal(t, 422, e.do("POST", "/api/admin/users", map[string]any{"cpf": valid, "roleIds": []string{uuid.NewString()}}, boss).Status, "role inexistente")
	assert.Equal(t, 422, e.do("POST", "/api/admin/users", map[string]any{"cpf": valid, "roleIds": role, "departmentId": uuid.NewString()}, boss).Status, "departamento inexistente")
	assert.Equal(t, 400, e.do("POST", "/api/admin/users", map[string]any{"cpf": valid, "roleIds": role, "email": "x@y.com"}, boss).Status, "e-mail vem do MT Login")
	var n int
	require.NoError(t, e.pool.QueryRow(context.Background(), "SELECT count(*) FROM admins").Scan(&n))
	assert.Equal(t, 1, n, "pré-cadastro recusado não deixa rastro")
}

// grant dá à role (por code) a permissão e invalida o cache do motor.
func (e *env) grant(roleCode, action string, fields, conds string) {
	e.t.Helper()
	ctx := context.Background()
	var roleID uuid.UUID
	require.NoError(e.t, e.pool.QueryRow(ctx, "SELECT id FROM admin_roles WHERE code=$1", roleCode).Scan(&roleID))
	var f []byte
	if fields != "" {
		f = []byte(fields)
	}
	_, err := db.New(e.pool).CreateAdminPermission(ctx, db.CreateAdminPermissionParams{RoleID: roleID, Action: action, Fields: f, Conditions: []byte(conds), Properties: []byte(`{}`)})
	require.NoError(e.t, err)
	require.NoError(e.t, e.authz.Invalidate(ctx, roleID))
}

// registerAndLogin cadastra o administrador pelo CPF com a role (por code), faz o primeiro acesso
// dele e devolve seu access token.
func registerAndLogin(t *testing.T, e *env, registrarToken, email, roleCode string) string {
	t.Helper()
	r := e.do("POST", "/api/admin/users", map[string]any{"cpf": cpfFor(email), "roleIds": []string{e.roleID(roleCode)}}, registrarToken)
	require.Equal(t, 201, r.Status, r.Body)
	return e.login(email)
}

func TestPermissaoParaCadastrarViaMotor(t *testing.T) {
	e := newEnv(t)
	e.registerFirst()
	boss := e.login("ana@exemplo.com")
	_, err := db.New(e.pool).UpsertAdminRole(context.Background(), db.UpsertAdminRoleParams{Code: "rh", Name: "RH"})
	require.NoError(t, err)
	rh := registerAndLogin(t, e, boss, "rh@exemplo.com", "rh")

	body := map[string]any{"cpf": cpfFor("novo@exemplo.com"), "roleIds": []string{e.roleID("rh")}}
	assert.Equal(t, 403, e.do("POST", "/api/admin/users", body, rh).Status)
	assert.Equal(t, 401, e.do("POST", "/api/admin/users", body, "").Status)

	e.grant("rh", "admin::settings.users.create", "", "[]")
	assert.Equal(t, 201, e.do("POST", "/api/admin/users", body, rh).Status, "a mudança vale logo após a invalidação")
}

func TestMeComPermissoesEfetivas(t *testing.T) {
	e := newEnv(t)
	e.registerFirst()
	boss := e.login("ana@exemplo.com")

	me := e.do("GET", "/api/admin/auth/me", nil, boss)
	assert.Equal(t, true, me.data()["isSuperAdmin"])
	assert.NotEmpty(t, me.data()["permissions"])

	_, err := db.New(e.pool).UpsertAdminRole(context.Background(), db.UpsertAdminRoleParams{Code: "leitor", Name: "Leitor"})
	require.NoError(t, err)
	e.grant("leitor", "admin::settings.users.read", `["email"]`, `["admin::is-creator"]`)
	e.grant("leitor", "admin::acao.fantasma.read", "", "[]") // ignorada: não registrada
	leitor := registerAndLogin(t, e, boss, "leitor@exemplo.com", "leitor")

	me = e.do("GET", "/api/admin/auth/me", nil, leitor)
	assert.Equal(t, false, me.data()["isSuperAdmin"])
	perms := me.data()["permissions"].([]any)
	require.Len(t, perms, 1)
	p := perms[0].(map[string]any)
	assert.Equal(t, "admin::settings.users.read", p["action"])
	assert.Equal(t, []any{"email"}, p["fields"])
	assert.Equal(t, []any{"admin::is-creator"}, p["conditions"])
}

func TestStartRemovePermissoesDeAcoesInexistentes(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q := db.New(e.pool)
	role, err := q.UpsertAdminRole(ctx, db.UpsertAdminRoleParams{Code: "x", Name: "X"})
	require.NoError(t, err)
	e.grant("x", "admin::settings.users.read", "", "[]")
	e.grant("x", "admin::acao.antiga.read", "", "[]")

	a, err := app.New(ctx, e.cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), e.pool, e.mail)
	require.NoError(t, err)
	require.NoError(t, a.Start(ctx))

	perms, err := q.ListAdminPermissionsByRole(ctx, role.ID)
	require.NoError(t, err)
	require.Len(t, perms, 1)
	assert.Equal(t, "admin::settings.users.read", perms[0].Action)
}
