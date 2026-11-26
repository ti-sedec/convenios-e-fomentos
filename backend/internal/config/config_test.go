package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProducao(t *testing.T) {
	forte := func() *Config {
		return &Config{
			AppEnv: "production", HTTPPort: 8080, PasswordMinLength: 8,
			AccessTokenTTL: 1, RefreshTokenMaxTTL: 1, RefreshTokenIdleTTL: 1,
			ResetTokenTTL: 1, EmailConfirmationTTL: 1, RateLimitAuthPerMin: 10,
			Argon2MemoryKiB: 19456, Argon2Iterations: 2, Argon2Parallel: 1,
			JWTAPISecret: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			APITokenSalt: "cccccccccccccccccccccccccccccccccccc",
		}
	}
	tests := []struct {
		nome    string
		mutar   func(*Config)
		quebrou bool
	}{
		{"segredos fortes", func(*Config) {}, false},
		{"segredo curto", func(c *Config) { c.JWTAPISecret = "curto" }, true},
		{"segredo de desenvolvimento", func(c *Config) { c.APITokenSalt = "dev-only-api-token-salt-change-me-0000000" }, true},
		{"ambiente inválido", func(c *Config) { c.AppEnv = "x" }, true},
		{"argon2 fraco em produção", func(c *Config) { c.Argon2MemoryKiB = 1024 }, true},
		{"MT-ID simulado em produção", func(c *Config) { c.MTIDSimulate = true }, true},
		{"argon2 sem iterações", func(c *Config) { c.Argon2Iterations = 0 }, true},
		{"limite de login zerado", func(c *Config) { c.RateLimitAuthPerMin = 0 }, true},
		{"validade de token zerada", func(c *Config) { c.EmailConfirmationTTL = 0 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			c := forte()
			tt.mutar(c)
			if tt.quebrou {
				assert.Error(t, c.Validate())
			} else {
				assert.NoError(t, c.Validate())
			}
		})
	}
}

func TestSegredosDeDesenvolvimentoSoForaDeProducao(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	c, err := Load()
	assert.NoError(t, err)
	assert.Equal(t, 8080, c.HTTPPort)
}

func TestValidateOIDC(t *testing.T) {
	ok := func() *Config {
		return &Config{AppEnv: "production", JWKSURI: "https://login.exemplo/certs", OIDCIssuer: "https://login.exemplo/realms/mt", OIDCClientID: "app", OIDCCPFClaim: "cpf"}
	}
	tests := []struct {
		nome    string
		mutar   func(*Config)
		quebrou bool
	}{
		{"completo", func(*Config) {}, false},
		{"sem JWKS", func(c *Config) { c.JWKSURI = "" }, true},
		{"sem issuer", func(c *Config) { c.OIDCIssuer = "" }, true},
		{"sem client", func(c *Config) { c.OIDCClientID = " " }, true},
		{"sem claim do CPF", func(c *Config) { c.OIDCCPFClaim = "" }, true},
		{"http em produção", func(c *Config) { c.JWKSURI = "http://login.exemplo/certs" }, true},
		{"http em desenvolvimento", func(c *Config) { c.AppEnv = "development"; c.JWKSURI = "http://localhost:8180/certs" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			c := ok()
			tt.mutar(c)
			if tt.quebrou {
				assert.Error(t, c.ValidateOIDC())
			} else {
				assert.NoError(t, c.ValidateOIDC())
			}
		})
	}
}

// Toda variável lida pela aplicação precisa estar documentada no .env.example, e vice-versa.
func TestEnvExampleDocumentaTodasAsVariaveis(t *testing.T) {
	raw, err := os.ReadFile("../../.env.example")
	require.NoError(t, err)
	documented := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if name, _, ok := strings.Cut(line, "="); ok && name == strings.ToUpper(name) && !strings.ContainsAny(name, " ") && name != "" {
			documented[name] = true
		}
	}
	read := map[string]bool{"DB_PORT": true} // usada só pelo docker-compose.yml
	typ := reflect.TypeOf(Config{})
	for i := range typ.NumField() {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("env"), ",")
		if tag != "" {
			read[tag] = true
		}
	}
	for name := range read {
		assert.True(t, documented[name], "%s não está no .env.example", name)
	}
	for name := range documented {
		assert.True(t, read[name], "%s está no .env.example mas não é lida pela aplicação", name)
	}
}

func TestSigadocURL(t *testing.T) {
	tests := []struct {
		nome string
		c    Config
		want string
	}{
		{"sem nada", Config{}, ""},
		{"só a raiz do X-Via: acrescenta o serviço", Config{XViaBaseURL: "https://x/r1/mt-dev/", XViaSigadocService: "GOV/MTI/SIGADOC"}, "https://x/r1/mt-dev/GOV/MTI/SIGADOC"},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) { assert.Equal(t, tt.want, tt.c.SigadocURL()) })
	}
}
