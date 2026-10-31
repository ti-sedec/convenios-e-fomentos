// Pacote config lê e valida as variáveis de ambiente da aplicação.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config reúne todas as configurações em tempo de execução.
type Config struct {
	AppEnv   string `env:"APP_ENV" envDefault:"development"`
	HTTPPort int    `env:"HTTP_PORT" envDefault:"8080"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	DatabaseURL string `env:"DATABASE_URL" envDefault:"postgres://app:app@localhost:5432/app?sslmode=disable"`

	// Autenticação dos administradores via MT Login (Keycloak/OIDC). O frontend conduz o login e a API
	// só valida os access tokens, com as chaves públicas do JWKS.
	JWKSURI      string `env:"JWKS_URI"`
	OIDCIssuer   string `env:"OIDC_ISSUER"`
	OIDCClientID string `env:"OIDC_CLIENT_ID"`
	// Claim do access token que traz o CPF do usuário.
	OIDCCPFClaim string `env:"OIDC_CPF_CLAIM" envDefault:"preferred_username"`
	// CPFs que o seed (e o cadastro inicial) torna Super Admin, separados por vírgula.
	SeedSuperAdminCPFs []string `env:"SEED_SUPER_ADMIN_CPF" envSeparator:","`

	// Segredo dos JWTs dos usuários finais (/api/*).
	JWTAPISecret string `env:"JWT_API_SECRET" envDefault:"dev-only-api-secret-change-me-000000000000"`
	APITokenSalt string `env:"API_TOKEN_SALT" envDefault:"dev-only-api-token-salt-change-me-0000000"`

	AccessTokenTTL      time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenMaxTTL  time.Duration `env:"REFRESH_TOKEN_MAX_TTL" envDefault:"720h"`
	RefreshTokenIdleTTL time.Duration `env:"REFRESH_TOKEN_IDLE_TTL" envDefault:"168h"`

	CORSAllowedOrigins  []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
	RateLimitAuthPerMin int      `env:"RATE_LIMIT_AUTH_PER_MINUTE" envDefault:"10"`
	// Limite geral por IP em todas as rotas (0 desliga). Protege o banco de requisições em massa.
	// RateLimitUploadPerMin limita, por usuário, os pedidos e confirmações de envio de arquivo dos projetos (0 desliga).
	RateLimitUploadPerMin int `env:"RATE_LIMIT_UPLOAD_PER_MINUTE" envDefault:"120"`
	// CEPAPIBaseURL é a raiz da consulta de CEP (ViaCEP, gratuito e sem chave); CEPTimeout limita cada consulta e
	// RateLimitCEPPerMin, por IP, as consultas por minuto (0 desliga).
	CEPAPIBaseURL      string        `env:"CEP_API_BASE_URL" envDefault:"https://viacep.com.br/ws"`
	CEPTimeout         time.Duration `env:"CEP_TIMEOUT" envDefault:"3s"`
	RateLimitCEPPerMin int           `env:"RATE_LIMIT_CEP_PER_MINUTE" envDefault:"60"`
	// ClamAVAddr (host:porta do clamd) liga a verificação de vírus dos arquivos dos projetos; vazio desliga.
	ClamAVAddr            string `env:"CLAMAV_ADDR"`
	RateLimitGlobalPerMin int    `env:"RATE_LIMIT_GLOBAL_PER_MINUTE" envDefault:"600"`
	// MTIDSimulate liga um MT-ID simulado (sem rede) para o desenvolvimento: o pedido de assinatura é aprovado sozinho depois de
	// MTIDSimulateDelay. NÃO assina nada de verdade; é recusado em produção. O cliente real do MT-ID ainda não existe
	// (docs/debito-tecnico-mt-id.md).
	MTIDSimulate      bool          `env:"MTID_SIMULATE"`
	MTIDSimulateDelay time.Duration `env:"MTID_SIMULATE_DELAY" envDefault:"10s"`
	MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"1048576"`
	PasswordMinLength int           `env:"PASSWORD_MIN_LENGTH" envDefault:"8"`

	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD"`
	SMTPFrom     string `env:"SMTP_FROM" envDefault:"no-reply@localhost"`

	// Validade do link de redefinição de senha dos usuários finais.
	ResetTokenTTL time.Duration `env:"RESET_TOKEN_TTL" envDefault:"30m"`
	// Validade do link de confirmação de e-mail dos usuários finais.
	EmailConfirmationTTL time.Duration `env:"EMAIL_CONFIRMATION_TTL" envDefault:"72h"`

	// CIDRs de proxies reversos confiáveis; só neles o X-Forwarded-For é aceito.
	TrustedProxies []string `env:"TRUSTED_PROXIES" envSeparator:","`

	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:8080"`

	// Armazenamento S3 (MinIO) dos arquivos dos documentos. Sem STORAGE_ENDPOINT o recurso fica
	// desligado: as rotas de arquivos respondem 503.
	StorageEndpoint  string `env:"STORAGE_ENDPOINT"`
	StorageUseSSL    bool   `env:"STORAGE_USE_SSL"`
	StorageAccessKey string `env:"STORAGE_ACCESS_KEY"`
	StorageSecretKey string `env:"STORAGE_SECRET_KEY"`
	StorageBucket    string `env:"STORAGE_BUCKET" envDefault:"documentos"`
	StorageRegion    string `env:"STORAGE_REGION" envDefault:"us-east-1"`
	// URL pública do armazenamento, vista pelo navegador (mesmo domínio da aplicação, com o prefixo
	// do proxy: ex. https://app.exemplo.gov.br/storage). As URLs pré-assinadas são calculadas sobre
	// o host dela; o proxy remove o prefixo antes de repassar ao MinIO.
	StoragePublicURL string `env:"STORAGE_PUBLIC_URL" envDefault:"http://localhost/storage"`
	// Validade das URLs pré-assinadas de envio e de download.
	StorageUploadTTL   time.Duration `env:"STORAGE_UPLOAD_TTL" envDefault:"15m"`
	StorageDownloadTTL time.Duration `env:"STORAGE_DOWNLOAD_TTL" envDefault:"2m"`
	// Validade da URL de exibição dos documentos (o leitor de PDF lê aos poucos, então é mais longa que a de download).
	StoragePreviewTTL time.Duration `env:"STORAGE_PREVIEW_TTL" envDefault:"15m"`

	// Conversão de documentos do Word em PDF para exibição (Gotenberg). Vazio: só PDF e imagem são exibidos.
	PreviewConverterURL string `env:"PREVIEW_CONVERTER_URL"`

	// Parâmetros do Argon2id (padrão recomendado pela OWASP: m=19 MiB, t=2, p=1).
	Argon2MemoryKiB  uint32 `env:"ARGON2_MEMORY_KIB" envDefault:"19456"`
	Argon2Iterations uint32 `env:"ARGON2_ITERATIONS" envDefault:"2"`
	Argon2Parallel   uint8  `env:"ARGON2_PARALLELISM" envDefault:"1"`
}

// Load lê o ambiente (carregando o .env apenas fora de produção) e valida o resultado.
func Load() (*Config, error) {
	if os := getAppEnv(); os != "production" {
		// Ausência do arquivo .env é normal.
		_ = godotenv.Load()
	}
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("lendo variáveis de ambiente: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func getAppEnv() string {
	var c struct {
		AppEnv string `env:"APP_ENV" envDefault:"development"`
	}
	_ = env.Parse(&c)
	return c.AppEnv
}

// IsProduction indica se a aplicação roda em produção.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

const (
	minSecretLen         = 32
	owaspArgon2MemoryKiB = 19456
)

// Validate verifica a consistência das configurações. Em produção, segredos
// ausentes, curtos ou iguais aos valores de desenvolvimento impedem a subida.
func (c *Config) Validate() error {
	var errs []error
	switch c.AppEnv {
	case "development", "test", "production":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV inválido %q (use development, test ou production)", c.AppEnv))
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		errs = append(errs, errors.New("HTTP_PORT fora do intervalo 1-65535"))
	}
	if c.AccessTokenTTL <= 0 || c.RefreshTokenMaxTTL <= 0 || c.RefreshTokenIdleTTL <= 0 {
		errs = append(errs, errors.New("as durações de token devem ser positivas"))
	}
	if c.ResetTokenTTL <= 0 || c.EmailConfirmationTTL <= 0 {
		errs = append(errs, errors.New("RESET_TOKEN_TTL e EMAIL_CONFIRMATION_TTL devem ser positivos"))
	}
	if c.PasswordMinLength < 1 {
		errs = append(errs, errors.New("PASSWORD_MIN_LENGTH deve ser >= 1"))
	}
	if c.RateLimitAuthPerMin < 1 || c.RateLimitGlobalPerMin < 0 || c.RateLimitUploadPerMin < 0 || c.RateLimitCEPPerMin < 0 {
		errs = append(errs, errors.New("RATE_LIMIT_AUTH_PER_MINUTE deve ser >= 1 e RATE_LIMIT_GLOBAL_PER_MINUTE >= 0"))
	}
	if c.Argon2Iterations < 1 || c.Argon2Parallel < 1 || c.Argon2MemoryKiB < 8*uint32(c.Argon2Parallel) {
		errs = append(errs, errors.New("parâmetros do Argon2 inválidos (iterações >= 1, paralelismo >= 1, memória >= 8 KiB por thread)"))
	}
	if c.StorageEndpoint != "" {
		if c.StorageAccessKey == "" || c.StorageSecretKey == "" || c.StorageBucket == "" {
			errs = append(errs, errors.New("STORAGE_ACCESS_KEY, STORAGE_SECRET_KEY e STORAGE_BUCKET são obrigatórios com STORAGE_ENDPOINT"))
		}
		if u, err := url.Parse(c.StoragePublicURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, errors.New("STORAGE_PUBLIC_URL deve ser uma URL http(s) completa"))
		} else if c.IsProduction() && u.Scheme != "https" {
			errs = append(errs, errors.New("STORAGE_PUBLIC_URL deve ser https em produção"))
		}
		if c.StorageUploadTTL <= 0 || c.StorageDownloadTTL <= 0 {
			errs = append(errs, errors.New("STORAGE_UPLOAD_TTL e STORAGE_DOWNLOAD_TTL devem ser positivos"))
		}
	}
	if c.MTIDSimulate && c.IsProduction() {
		errs = append(errs, errors.New("MTID_SIMULATE não pode ficar ligado em produção: o MT-ID simulado não assina de verdade"))
	}
	if c.MTIDSimulate && c.MTIDSimulateDelay < 0 {
		errs = append(errs, errors.New("MTID_SIMULATE_DELAY não pode ser negativo"))
	}
	if c.IsProduction() && c.Argon2MemoryKiB < owaspArgon2MemoryKiB {
		errs = append(errs, fmt.Errorf("ARGON2_MEMORY_KIB abaixo do mínimo recomendado pela OWASP (%d) em produção", owaspArgon2MemoryKiB))
	}
	if c.IsProduction() {
		secrets := map[string]string{
			"JWT_API_SECRET": c.JWTAPISecret,
			"API_TOKEN_SALT": c.APITokenSalt,
		}
		for name, v := range secrets {
			if len(v) < minSecretLen || strings.HasPrefix(v, "dev-only-") {
				errs = append(errs, fmt.Errorf("%s ausente ou fraco: em produção use um valor aleatório com ao menos %d caracteres", name, minSecretLen))
			}
		}
	}
	return errors.Join(errs...)
}

// ValidateOIDC exige o provedor de identidade dos administradores configurado. Só o servidor da
// API precisa dele; a CLI (migrações, seed, administradores) funciona sem.
func (c *Config) ValidateOIDC() error {
	var errs []error
	for name, v := range map[string]string{"JWKS_URI": c.JWKSURI, "OIDC_ISSUER": c.OIDCIssuer, "OIDC_CLIENT_ID": c.OIDCClientID, "OIDC_CPF_CLAIM": c.OIDCCPFClaim} {
		if strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Errorf("%s é obrigatório (login dos administradores pelo MT Login)", name))
		}
	}
	for name, v := range map[string]string{"JWKS_URI": c.JWKSURI, "OIDC_ISSUER": c.OIDCIssuer} {
		httpAllowed := !c.IsProduction() && strings.HasPrefix(v, "http://")
		if v != "" && !strings.HasPrefix(v, "https://") && !httpAllowed {
			errs = append(errs, fmt.Errorf("%s deve ser uma URL https", name))
		}
	}
	return errors.Join(errs...)
}
