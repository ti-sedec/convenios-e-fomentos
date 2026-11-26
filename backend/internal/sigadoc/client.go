// Pacote sigadoc isola o SIGADOC (sistema de protocolo do governo de MT, acessado pelo X-Via) do restante do sistema. O contrato é
// o do próprio SIGADOC (docs/sigadoc/swagger-sigaex.yaml e swagger-siga.yaml). O sistema autentica com um usuário de sistema
// (Basic) que REPRESENTA a matrícula de cada analista: POST /siga/autenticar?matricula=... devolve um JWT de cerca de 8 horas, e
// é com esse token que as chamadas seguintes (e as assinaturas) valem para o analista.
//
// Este arquivo traz o cliente HTTP e a autenticação. A sondagem do que o usuário de sistema pode chamar está em check.go.
package sigadoc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/wagner/convenios-e-fomentos/backend/internal/xvia"
)

// maxBody limita o que se lê de uma resposta (o stacktrace de erro do SIGADOC já passa de 8 KB).
const maxBody = 32 << 20

// Error é uma resposta de erro do SIGADOC.
type Error struct {
	// Status é o código HTTP (o SIGADOC responde 500 até para senha errada).
	Status int
	// Message é o errormsg (ou error) da resposta, sem o stacktrace.
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("sigadoc: HTTP %d: %s", e.Status, e.Message) }

// IsForbidden diz se o SIGADOC recusou o método para o usuário de sistema ("não possui acesso a este método"): é uma liberação
// que a MTI precisa dar, não um erro do pedido.
func IsForbidden(err error) bool {
	var e *Error
	return errors.As(err, &e) && isMethodForbidden(e.Status, e.Message)
}

// IsGatewayError diz se a recusa veio do X-Road (X-Via), que fica entre nós e o SIGADOC, e não do SIGADOC: o corpo é
// {"type":"Server.ClientProxy.…","message":…} (ou Server.ServerProxy). Exemplo: "Global configuration is expired", quando o servidor de
// segurança ficou sem atualizar a configuração global. É um problema de infraestrutura da MTI, que o analista não resolve; tentar de novo
// só adianta depois que ela for corrigida.
func IsGatewayError(err error) bool {
	var e *Error
	return errors.As(err, &e) && (strings.Contains(e.Message, `"Server.ClientProxy.`) || strings.Contains(e.Message, `"Server.ServerProxy.`))
}

// IsProhibited diz se o SIGADOC recusou a operação por uma regra de configuração do usuário ou da lotação ("Proibido porque não pode
// ... por configuração"). Outras recusas "Proibido porque…" (ex.: "não é via") são erro do pedido, não permissão. É uma liberação que a MTI precisa dar no SIGADOC, não um erro do pedido: repetir não adianta.
func IsProhibited(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.Contains(e.Message, "Proibido porque") && strings.Contains(e.Message, "por configuração")
}

// IsAuthFailed diz se o login do usuário de sistema foi recusado (senha errada ou usuário sem acesso).
func IsAuthFailed(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.Contains(e.Message, "Falha de autenticação")
}

// IsUnknownIdentity diz se a matrícula informada não existe no SIGADOC.
func IsUnknownIdentity(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.Contains(e.Message, "localizar a identidade")
}

func isMethodForbidden(status int, msg string) bool {
	return status == http.StatusForbidden && strings.Contains(msg, "não possui acesso a este método")
}

// HTTPConfig configura o cliente real.
type HTTPConfig struct {
	// BaseURL é a URL base da API do SIGADOC, já com o serviço no X-Via, ex.: https://host/r1/mt-dev/GOV/MTI/SIGADOC
	// (config.Config.SigadocURL monta esta URL).
	BaseURL string
	// XRoadClient vai no cabeçalho x-road-client (identifica o nosso subsistema no X-Via).
	XRoadClient string
	// Username e Password são o usuário de sistema do SIGADOC (Basic).
	Username, Password string
	// CAFile e TLSServerName: veja xvia.TLS.
	CAFile, TLSServerName string
	Timeout               time.Duration
	Log                   *slog.Logger
}

// HTTP é o cliente real do SIGADOC.
type HTTP struct {
	base     string
	xroad    string
	user     string
	password string
	client   *http.Client
	log      *slog.Logger
}

// NewHTTP cria o cliente. Não faz chamada de rede.
func NewHTTP(cfg HTTPConfig) (*HTTP, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("sigadoc: URL do X-Via inválida: %q", cfg.BaseURL)
	}
	if cfg.XRoadClient == "" {
		return nil, errors.New("sigadoc: x-road-client é obrigatório")
	}
	if cfg.Username == "" || cfg.Password == "" {
		return nil, errors.New("sigadoc: usuário e senha do usuário de sistema são obrigatórios")
	}
	hc, err := xvia.NewHTTPClient(xvia.TLS{CAFile: cfg.CAFile, ServerName: cfg.TLSServerName, Timeout: cfg.Timeout})
	if err != nil {
		return nil, fmt.Errorf("sigadoc: %w", err)
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &HTTP{base: strings.TrimRight(cfg.BaseURL, "/"), xroad: cfg.XRoadClient, user: cfg.Username, password: cfg.Password, client: hc, log: log}, nil
}

// Token é o JWT que representa uma matrícula.
type Token struct {
	// Value é o JWT. É segredo: nunca vai para log nem para o banco.
	Value string
	// ExpiresAt vem do claim exp; zero se o token não o trouxer.
	ExpiresAt time.Time
}

// Valid diz se o token ainda serve, com uma folga antes do vencimento.
func (t Token) Valid(margin time.Duration) bool {
	return t.Value != "" && (t.ExpiresAt.IsZero() || time.Now().Add(margin).Before(t.ExpiresAt))
}

// Authenticate faz POST /siga/autenticar para a matrícula (formato SEDEC123456, em maiúsculas) e devolve o token.
func (c *HTTP) Authenticate(ctx context.Context, matricula string) (Token, error) {
	matricula = strings.TrimSpace(matricula)
	if matricula == "" {
		return Token{}, errors.New("sigadoc: matrícula vazia")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/siga/autenticar?"+url.Values{"matricula": {matricula}}.Encode(), nil)
	if err != nil {
		return Token{}, err
	}
	req.SetBasicAuth(c.user, c.password)
	status, body, err := c.send(ctx, req)
	if err != nil {
		return Token{}, err
	}
	if status != http.StatusOK {
		return Token{}, parseError(status, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" {
		return Token{}, errors.New("sigadoc: resposta de autenticação sem token")
	}
	return Token{Value: out.Token, ExpiresAt: expiry(out.Token)}, nil
}

// MatriculaByCPF devolve as matrículas do CPF no órgão (GET /siga/matricula).
func (c *HTTP) MatriculaByCPF(ctx context.Context, token Token, cpf, orgao string) ([]string, error) {
	status, body, err := c.call(ctx, token, http.MethodGet, "/siga/matricula", url.Values{"cpf": {cpf}, "sigla": {orgao}}, "", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, parseError(status, body)
	}
	var out struct {
		Matricula []struct {
			Matricula string `json:"matricula"`
		} `json:"matricula"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("sigadoc: resposta de matrícula inválida: %w", err)
	}
	res := make([]string, 0, len(out.Matricula))
	for _, m := range out.Matricula {
		res = append(res, m.Matricula)
	}
	return res, nil
}

// call monta e envia uma chamada autenticada pelo token. query vai na URL; body (se houver) leva o Content-Type informado.
func (c *HTTP) call(ctx context.Context, token Token, method, path string, query url.Values, contentType string, body []byte) (int, []byte, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.Value)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.send(ctx, req)
}

func (c *HTTP) send(ctx context.Context, req *http.Request) (int, []byte, error) {
	req.Header.Set("x-road-client", c.xroad)
	start := time.Now()
	res, err := c.client.Do(req)
	if err != nil {
		// O erro de rede pode trazer a URL, mas nunca credenciais (Basic e Bearer vão em cabeçalho).
		// O erro do net/http carrega a URL inteira, e a validação do PIN leva o PIN na query: só a causa segue adiante.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		c.log.WarnContext(ctx, "SIGADOC: falha de rede", "method", req.Method, "path", req.URL.Path, "error", err)
		return 0, nil, fmt.Errorf("sigadoc %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return 0, nil, fmt.Errorf("sigadoc %s %s: lendo a resposta: %w", req.Method, req.URL.Path, err)
	}
	attrs := []any{"method", req.Method, "path", req.URL.Path, "status", res.StatusCode, "ms", time.Since(start).Milliseconds(), "bytes", len(data)}
	if res.StatusCode >= 400 && !strings.Contains(req.URL.Path, "/pin") {
		// O começo da resposta de erro (com o stacktrace do SIGADOC, que a tela não mostra) para diagnosticar. As rotas de PIN ficam de fora:
		// a validação leva o par CPF:PIN na query, e não há garantia de que o erro não a repita.
		attrs = append(attrs, "error_body", truncateBody(data, 1500))
	}
	c.log.DebugContext(ctx, "SIGADOC", attrs...)
	return res.StatusCode, data, nil
}

func truncateBody(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// parseError transforma uma resposta de erro do SIGADOC em *Error.
func parseError(status int, body []byte) error {
	return &Error{Status: status, Message: messageOf(body)}
}

// messageOf extrai a mensagem da resposta de erro do SIGADOC ({"errormsg": ...} ou {"error": ...}), sem o stacktrace.
func messageOf(body []byte) string {
	var e struct {
		ErrorMsg string `json:"errormsg"`
		Error    string `json:"error"`
	}
	msg := ""
	if json.Unmarshal(body, &e) == nil {
		msg = e.ErrorMsg
		if msg == "" {
			msg = e.Error
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
	}
	return msg
}

// expiry lê o claim exp de um JWT sem validar a assinatura (a chave é do SIGADOC; só queremos saber quando o token vence).
func expiry(jwt string) time.Time {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}
	var c struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(raw, &c) != nil || c.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(c.Exp, 0)
}
