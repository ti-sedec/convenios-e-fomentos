package sigadoc

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testService = "/r1/mt-dev/GOV/MTI/SIGADOC"
	testXRoad   = "mt-dev/GOV/SEDEC/CONVENIOS-FOMENTO"
	testUser    = "WSTESTE"
	testPass    = "senha-secreta-do-teste"
)

// jwt monta um JWT sem assinatura de verdade (o cliente só lê o exp).
func jwt(exp time.Time) string {
	enc := func(v any) string { b, _ := json.Marshal(v); return base64.RawURLEncoding.EncodeToString(b) }
	return enc(map[string]string{"typ": "JWT", "alg": "HS256"}) + "." + enc(map[string]any{"sub": "SEDEC123456", "exp": exp.Unix()}) + ".assinatura"
}

// newTestClient sobe um X-Via/SIGADOC falso em TLS e devolve um cliente que confia só nele. logs recebe o log do cliente.
func newTestClient(t *testing.T, logs *bytes.Buffer, h http.HandlerFunc) *HTTP {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	var lg *slog.Logger
	if logs != nil {
		lg = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	c, err := NewHTTP(HTTPConfig{BaseURL: srv.URL + testService, XRoadClient: testXRoad, Username: testUser, Password: testPass, CAFile: caFile, Log: lg})
	require.NoError(t, err)
	return c
}

func TestAuthenticate(t *testing.T) {
	exp := time.Now().Add(8 * time.Hour).Truncate(time.Second)
	var logs bytes.Buffer
	c := newTestClient(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, testService+"/siga/autenticar", r.URL.Path)
		assert.Equal(t, "SEDEC123456", r.URL.Query().Get("matricula"))
		assert.Equal(t, testXRoad, r.Header.Get("x-road-client"))
		u, p, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, testUser, u)
		assert.Equal(t, testPass, p)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": jwt(exp)})
	})
	tok, err := c.Authenticate(t.Context(), " SEDEC123456 ")
	require.NoError(t, err)
	assert.Equal(t, exp.Unix(), tok.ExpiresAt.Unix())
	assert.True(t, tok.Valid(time.Minute))
	assert.False(t, Token{Value: "x", ExpiresAt: time.Now().Add(30 * time.Second)}.Valid(time.Minute), "token perto de vencer não serve")
	assert.NotContains(t, logs.String(), testPass, "a senha não pode aparecer no log")
	assert.NotContains(t, logs.String(), tok.Value, "o token não pode aparecer no log")
}

func TestAuthenticateErros(t *testing.T) {
	respond := func(msg string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			// O SIGADOC responde 500 até para senha errada e manda o stacktrace junto.
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"errormsg": msg, "errordetails": []map[string]string{{"stacktrace": "com.crivano.Exception\n\tat ..."}}})
		}
	}
	t.Run("senha errada", func(t *testing.T) {
		_, err := newTestClient(t, nil, respond("Erro no login: Falha de autenticação!")).Authenticate(t.Context(), "SEDEC123456")
		require.Error(t, err)
		assert.True(t, IsAuthFailed(err))
		assert.False(t, IsUnknownIdentity(err))
		assert.NotContains(t, err.Error(), "stacktrace", "o stacktrace não entra na mensagem")
	})
	t.Run("matrícula inexistente", func(t *testing.T) {
		_, err := newTestClient(t, nil, respond("Erro no login: Ocorreu um erro tentando localizar a identidade do usuario '1'.")).Authenticate(t.Context(), "1")
		assert.True(t, IsUnknownIdentity(err))
		assert.False(t, IsAuthFailed(err))
	})
	t.Run("matrícula vazia não chama a rede", func(t *testing.T) {
		_, err := newTestClient(t, nil, func(http.ResponseWriter, *http.Request) { t.Error("não deveria chamar") }).Authenticate(t.Context(), "  ")
		require.Error(t, err)
	})
	t.Run("resposta sem token", func(t *testing.T) {
		_, err := newTestClient(t, nil, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }).Authenticate(t.Context(), "SEDEC123456")
		require.Error(t, err)
	})
}

func TestNewHTTPValida(t *testing.T) {
	ok := HTTPConfig{BaseURL: "https://x" + testService, XRoadClient: "c", Username: "u", Password: "p"}
	_, err := NewHTTP(ok)
	require.NoError(t, err)
	for name, mut := range map[string]func(*HTTPConfig){
		"url":     func(c *HTTPConfig) { c.BaseURL = "ftp://x" },
		"xroad":   func(c *HTTPConfig) { c.XRoadClient = "" },
		"usuario": func(c *HTTPConfig) { c.Username = "" },
		"senha":   func(c *HTTPConfig) { c.Password = "" },
		"ca":      func(c *HTTPConfig) { c.CAFile = "/nao/existe.pem" },
	} {
		cfg := ok
		mut(&cfg)
		_, err := NewHTTP(cfg)
		assert.Error(t, err, name)
	}
}

func TestMatriculaByCPF(t *testing.T) {
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, testService+"/siga/matricula", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		assert.Equal(t, "52998224725", r.URL.Query().Get("cpf"))
		assert.Equal(t, "SEDEC", r.URL.Query().Get("sigla"))
		_, _ = w.Write([]byte(`{"matricula":[{"matricula":"SEDEC123456","siglaOrgao":"SEDEC"}]}`))
	})
	got, err := c.MatriculaByCPF(t.Context(), Token{Value: "tok"}, "52998224725", "SEDEC")
	require.NoError(t, err)
	assert.Equal(t, []string{"SEDEC123456"}, got)
}

// A sondagem só pode pedir o que não tem efeito: nada de criar documento nem de validar PIN.
func TestCheckClassificaESoSondaOQueNaoTemEfeito(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.Method+" "+strings.TrimPrefix(r.URL.Path, testService)] = r.Header.Get("Content-Type")
		mu.Unlock()
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		path := strings.TrimPrefix(r.URL.Path, testService)
		switch {
		case strings.HasSuffix(path, "/movimentacoes") || path == "/sigaex/documentos/"+probeSigla:
			// liberado: o documento não existe (404 do SIGADOC)
			respondJSON(w, http.StatusNotFound, `{"errormsg":"Número do Documento não existe"}`)
		case path == "/siga/matricula":
			respondJSON(w, http.StatusBadRequest, `{"errormsg":"O cpf é obrigatório."}`)
		case path == "/sigaex/documentos":
			respondJSON(w, http.StatusOK, `{"list":[]}`)
		case strings.HasSuffix(path, "/pdf-completo"):
			respondJSON(w, http.StatusInternalServerError, `{"errormsg":"br.gov.jfrj.siga.ex.api.v1.DocumentosSiglaPdfCompletoGet from [Module \"deployment.sigaex.war\"]"}`)
		default:
			respondJSON(w, http.StatusForbidden, `{"errormsg":"O usuário de sistema não possui acesso a este método."}`)
		}
	})
	out := c.Check(t.Context(), Token{Value: "tok"}, time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))

	byPath := map[string]Outcome{}
	for _, o := range out {
		byPath[o.Probe.Method+" "+o.Probe.Path] = o
	}
	assert.Equal(t, Allowed, byPath["GET /sigaex/documentos/"+probeSigla+"/movimentacoes"].Result)
	assert.Equal(t, Allowed, byPath["GET /siga/matricula"].Result, "400 de validação conta como liberado")
	assert.Equal(t, Allowed, byPath["GET /sigaex/documentos"].Result)
	assert.Equal(t, NotDeployed, byPath["GET /sigaex/documentos/"+probeSigla+"/pdf-completo"].Result)
	assert.Equal(t, Blocked, byPath["POST /sigaex/documentos/"+probeSigla+"/autuar"].Result)
	assert.Equal(t, Blocked, byPath["GET /sigaex/modelos"].Result)

	// Não sondados, e portanto nunca chamados.
	assert.Equal(t, Skipped, byPath["POST /siga/pin/validar"].Result)
	assert.Equal(t, Skipped, byPath["POST /sigaex/documentos"].Result)
	mu.Lock()
	defer mu.Unlock()
	assert.NotContains(t, seen, "POST /siga/pin/validar")
	assert.NotContains(t, seen, "POST /sigaex/documentos")

	// Formato dos corpos.
	assert.Equal(t, "application/x-www-form-urlencoded", seen["POST /sigaex/documentos/"+probeSigla+"/assinar-com-pin"])
	assert.Equal(t, "application/x-www-form-urlencoded", seen["POST /sigaex/documentos/"+probeSigla+"/juntar"])
}

func respondJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}

func TestProbesTodosOsPassosObrigatoriosEstaoNaLista(t *testing.T) {
	need := map[string]bool{
		"/siga/matricula": false, "/sigaex/modelos": false, "/siga/pin/validar": false, "/sigaex/documentos": false,
		"/finalizar": false, "/assinar-com-pin": false, "/autuar": false, "/juntar": false, "/movimentacoes": false,
	}
	for _, p := range Probes(time.Now()) {
		for k := range need {
			if strings.HasSuffix(p.Path, k) {
				need[k] = true
			}
		}
	}
	for k, found := range need {
		assert.True(t, found, "faltou sondar %s", k)
	}
	// Nenhuma sondagem leva PIN: o PIN nunca aparece em Probes.
	for _, p := range Probes(time.Now()) {
		assert.NotContains(t, strings.ToLower(p.Form.Encode()+p.JSON+p.Query.Encode()), "pin")
	}
}

func TestAsciiEntities(t *testing.T) {
	assert.Equal(t, "Solicita&#231;&#227;o de apoio - R$ 10,00", asciiEntities("Solicitação de apoio - R$ 10,00"))
	assert.Equal(t, "sem acento", asciiEntities("sem acento"))
	assert.Equal(t, "n&#186; 1 &#8211; ok", asciiEntities("nº 1 – ok"))
}

// O SIGADOC de desenvolvimento corrompe acentos na entrevista; a pré-visualização manda referências numéricas.
func TestPreviewTermoEnviaAcentosComoEntidades(t *testing.T) {
	var got string
	c := newTestClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		got = r.PostForm.Get("entrevista")
		assert.Equal(t, testService+"/sigaex/modelos/23323/processar-entrevista", r.URL.Path)
		_, _ = w.Write([]byte("<form></form>"))
	})
	_, err := c.PreviewTermo(t.Context(), Token{Value: "t"}, "23323", map[string]string{"docGovMT_interessado_nome": "Associação"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"docGovMT_interessado_nome":"Associa&#231;&#227;o"}`, got)
}
