package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"

	"github.com/wagner/convenios-e-fomentos/backend/internal/cpf"
)

// ErrInvalidToken indica JWT malformado, expirado, de outro emissor ou client, ou com assinatura inválida.
var ErrInvalidToken = errors.New("token inválido")

// ErrMissingCPF indica um token válido cujo claim de CPF está ausente ou inválido.
var ErrMissingCPF = errors.New("token sem CPF válido")

const (
	tokenTypeAccess = "Bearer"
	jwksRefresh     = time.Hour
	// Limita a releitura do JWKS provocada por tokens com `kid` desconhecido (rotação de chaves).
	unknownKIDEvery = time.Minute
	clockLeeway     = 30 * time.Second
	anonymousName   = "Usuário"
)

// OIDCConfig reúne os parâmetros do provedor OIDC (MT Login / Keycloak).
type OIDCConfig struct {
	JWKSURI  string
	Issuer   string
	ClientID string
	// CPFClaim é o claim do access token que traz o CPF do usuário.
	CPFClaim string
}

// Identity é a identidade extraída de um access token válido.
type Identity struct {
	Subject   string
	CPF       string // 11 dígitos, validado
	Email     string
	FirstName string
	LastName  string
}

// OIDCVerifier valida access tokens emitidos pelo provedor OIDC (assinatura via JWKS).
type OIDCVerifier struct {
	cfg  OIDCConfig
	keys keyfunc.Keyfunc
	now  func() time.Time
}

// NewOIDCVerifier cria o validador e passa a manter o JWKS atualizado até o contexto terminar.
// A primeira leitura do JWKS não bloqueia a subida: se o provedor estiver fora do ar, a API sobe
// e os tokens são rejeitados até a chave poder ser lida.
func NewOIDCVerifier(ctx context.Context, cfg OIDCConfig, log *slog.Logger) (*OIDCVerifier, error) {
	keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{cfg.JWKSURI}, keyfunc.Override{
		RefreshInterval:   jwksRefresh,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(unknownKIDEvery), 1),
		RefreshErrorHandlerFunc: func(url string) func(context.Context, error) {
			return func(ctx context.Context, err error) {
				log.ErrorContext(ctx, "falha ao atualizar o JWKS", "url", url, "error", err)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("criando leitor do JWKS: %w", err)
	}
	return &OIDCVerifier{cfg: cfg, keys: keys, now: time.Now}, nil
}

// Verify valida assinatura (RS256), emissor, expiração e que o token é um access token deste client,
// e devolve a identidade. Tokens de ID ou de refresh e tokens de outros clients são recusados.
func (v *OIDCVerifier) Verify(token string) (*Identity, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(token, claims, v.keys.Keyfunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockLeeway),
		jwt.WithTimeFunc(v.now),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if typ, ok := claims["typ"].(string); ok && !strings.EqualFold(typ, tokenTypeAccess) {
		return nil, fmt.Errorf("%w: tipo %q não é access token", ErrInvalidToken, typ)
	}
	if !v.forThisClient(claims) {
		return nil, fmt.Errorf("%w: token emitido para outro client", ErrInvalidToken)
	}

	digits, err := cpf.Normalize(claimString(claims, v.cfg.CPFClaim))
	if err != nil {
		return nil, ErrMissingCPF
	}
	id := &Identity{
		Subject: claimString(claims, "sub"), CPF: digits, Email: claimString(claims, "email"),
		FirstName: claimString(claims, "given_name"), LastName: claimString(claims, "family_name"),
	}
	if id.FirstName == "" && id.LastName == "" {
		id.FirstName, id.LastName, _ = strings.Cut(claimString(claims, "name"), " ")
		id.LastName = strings.TrimSpace(id.LastName)
	}
	if id.FirstName == "" && id.LastName == "" {
		id.FirstName = anonymousName
	}
	return id, nil
}

// forThisClient aceita tokens cujo `azp` (parte autorizada) ou `aud` seja o client desta aplicação.
func (v *OIDCVerifier) forThisClient(claims jwt.MapClaims) bool {
	if claimString(claims, "azp") == v.cfg.ClientID {
		return true
	}
	aud, err := claims.GetAudience()
	if err != nil {
		return false
	}
	for _, a := range aud {
		if a == v.cfg.ClientID {
			return true
		}
	}
	return false
}

func claimString(claims jwt.MapClaims, name string) string {
	s, _ := claims[name].(string)
	return strings.TrimSpace(s)
}
