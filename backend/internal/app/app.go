// Pacote app faz a injeção manual de dependências, compartilhada pelo servidor e pelos testes.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wagner/convenios-e-fomentos/backend/api"
	"github.com/wagner/convenios-e-fomentos/backend/internal/admin"
	"github.com/wagner/convenios-e-fomentos/backend/internal/apitoken"
	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/auth"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/catalog"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cep"
	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/department"
	"github.com/wagner/convenios-e-fomentos/backend/internal/document"
	"github.com/wagner/convenios-e-fomentos/backend/internal/enduser"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
	"github.com/wagner/convenios-e-fomentos/backend/internal/outbox"
	"github.com/wagner/convenios-e-fomentos/backend/internal/preview"
	"github.com/wagner/convenios-e-fomentos/backend/internal/project"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
	"github.com/wagner/convenios-e-fomentos/backend/internal/scan"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// Hasher cria o hasher de senhas a partir da configuração.
func Hasher(cfg *config.Config) auth.Hasher {
	return auth.Hasher{Memory: cfg.Argon2MemoryKiB, Iterations: cfg.Argon2Iterations, Parallelism: cfg.Argon2Parallel, MinLength: cfg.PasswordMinLength}
}

// AdminService cria o serviço de administradores sem motor de permissões
// (uso da CLI, que não avalia permissões).
func AdminService(pool *pgxpool.Pool) *admin.Service {
	return admin.NewService(pool, nil)
}

// App agrupa o roteador e as peças que precisam de ciclo de vida.
type App struct {
	Handler http.Handler
	// Router permite registrar rotas dos recursos (Fase 7) e injetar rotas nos testes.
	Router  *chi.Mux
	Authz   *authz.Engine
	Actions *authz.Registry
	// Identifier e EndUsers servem para montar as rotas públicas dos recursos (Fase 7).
	Identifier *enduser.Identifier
	EndUsers   *enduser.Service
	APITokens  *apitoken.Service
	Documents  *document.Service
	Catalog    *catalog.Service
	Rules      *rules.Service
	Projects   *project.Service
	// Outbox envia a caixa de saída de e-mails em segundo plano (iniciado por Start).
	Outbox *outbox.Worker

	log            *slog.Logger
	pool           *pgxpool.Pool
	noOutboxWorker bool
	// outboxDone fecha quando o worker de e-mail termina (ver Wait). Nulo se o worker não foi iniciado.
	outboxDone chan struct{}
}

// Option ajusta a montagem do grafo (usada principalmente pelos testes).
type Option func(*options)

type options struct {
	store          storage.Store
	mtid           mtid.Client
	converter      preview.Converter
	noOutboxWorker bool
}

// WithStore usa o armazenamento dado em vez de criar o S3 a partir da configuração.
func WithStore(s storage.Store) Option { return func(o *options) { o.store = s } }

// WithMTID liga o MT-ID (assinatura das declarações). Sem ele, emitir declaração responde 503: o cliente HTTP real ainda não existe
// (docs/debito-tecnico-mt-id.md).
func WithMTID(c mtid.Client) Option { return func(o *options) { o.mtid = c } }

// WithPreviewConverter usa o conversor de Word para PDF dado em vez do Gotenberg da configuração.
func WithPreviewConverter(c preview.Converter) Option { return func(o *options) { o.converter = c } }

// WithoutOutboxWorker não inicia o envio de e-mails em segundo plano. Os testes usam para controlar quando a caixa de saída é
// esvaziada (App.DrainOutbox) e assim ler os e-mails de forma determinística.
func WithoutOutboxWorker() Option { return func(o *options) { o.noOutboxWorker = true } }

// NewStore cria o armazenamento S3 da configuração; devolve nil quando STORAGE_ENDPOINT não está definido.
func NewStore(cfg *config.Config) (storage.Store, error) {
	if cfg.StorageEndpoint == "" {
		return nil, nil
	}
	return storage.NewS3(storage.S3Config{Endpoint: cfg.StorageEndpoint, UseSSL: cfg.StorageUseSSL, PublicURL: cfg.StoragePublicURL,
		AccessKey: cfg.StorageAccessKey, SecretKey: cfg.StorageSecretKey, Bucket: cfg.StorageBucket, Region: cfg.StorageRegion})
}

// New monta todo o grafo de dependências.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, pool *pgxpool.Pool, mail mailer.Mailer, opts ...Option) (*App, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.store == nil {
		s, err := NewStore(cfg)
		if err != nil {
			return nil, err
		}
		o.store = s
	}
	trusted, err := httpapi.ParseCIDRs(cfg.TrustedProxies)
	if err != nil {
		return nil, err
	}
	q := db.New(pool)

	// Motor de permissões: ações e condições são registradas em código.
	actions := authz.NewRegistry()
	authz.RegisterCoreActions(actions)
	document.Register(actions) // recurso de exemplo
	catalog.Register(actions)
	rules.Register(actions)
	project.Register(actions)
	conds := authz.NewConditionRegistry()
	store := authz.DBStore{Q: q}
	authz.RegisterStandardConditions(conds, store)
	project.RegisterConditions(conds, q)
	engine := authz.NewEngine(actions, conds, store, authz.PGBroadcaster{Pool: pool, Log: log}, log)
	engine.SetEndUserStore(authz.EndUserStore{Q: q})

	// Sem JWKS_URI (CLI) não há como validar tokens de administrador; o servidor da API exige a
	// configuração antes de subir (Config.ValidateOIDC).
	var oidc *auth.OIDCVerifier
	if cfg.JWKSURI != "" {
		oidc, err = auth.NewOIDCVerifier(ctx, auth.OIDCConfig{JWKSURI: cfg.JWKSURI, Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, CPFClaim: cfg.OIDCCPFClaim}, log)
		if err != nil {
			return nil, err
		}
	}
	adminAuth := auth.NewAdminAuth(pool, oidc, log)
	adminSvc := admin.NewService(pool, engine)
	adminHandler := admin.NewHandler(adminSvc, adminAuth, engine)

	apiJWT := auth.NewJWTIssuer(cfg.JWTAPISecret, auth.AudienceAPI, cfg.AccessTokenTTL)
	endUserSvc, err := enduser.NewService(pool, Hasher(cfg), apiJWT, mail, engine, enduser.Config{
		RefreshMaxTTL: cfg.RefreshTokenMaxTTL, RefreshIdleTTL: cfg.RefreshTokenIdleTTL, ResetTTL: cfg.ResetTokenTTL,
		ConfirmTTL: cfg.EmailConfirmationTTL, PublicURL: cfg.PublicURL, DefaultSender: cfg.SMTPFrom,
	}, log)
	if err != nil {
		return nil, err
	}
	tokenSvc := apitoken.NewService(pool, engine, cfg.APITokenSalt, log)
	identifier := &enduser.Identifier{Svc: endUserSvc, Engine: engine, Tokens: tokenSvc}
	endUserHandler := enduser.NewHandler(endUserSvc, identifier, cfg.RateLimitAuthPerMin)
	endUserAdmin := enduser.NewAdminHandler(endUserSvc, adminHandler.Guard, adminHandler.GuardAny)
	tokenAdmin := apitoken.NewHandler(tokenSvc, adminHandler.Guard)
	departmentAdmin := department.NewHandler(department.NewService(pool), adminHandler.Guard, adminHandler.GuardAny)
	documentSvc := document.NewService(pool, engine)
	if o.store != nil {
		documentSvc.SetStorage(o.store, cfg.StorageUploadTTL, cfg.StorageDownloadTTL, log)
	}
	catalogSvc := catalog.NewService(pool)
	rulesSvc := rules.NewService(pool)
	projectSvc := project.NewService(pool, rulesSvc)
	projectSvc.SetNotifier(cfg.PublicURL, log)
	if cfg.ClamAVAddr != "" {
		projectSvc.SetScanner(scan.ClamAV{Addr: cfg.ClamAVAddr})
	}
	if o.mtid != nil {
		projectSvc.SetMTID(o.mtid)
	}
	if o.store != nil {
		projectSvc.SetStorage(o.store, cfg.StorageUploadTTL, cfg.StorageDownloadTTL, log)
		conv := o.converter
		if conv == nil && cfg.PreviewConverterURL != "" {
			conv = preview.NewGotenberg(cfg.PreviewConverterURL, 90*time.Second)
		}
		projectSvc.SetPreview(conv, cfg.StoragePreviewTTL, log)
	}
	cepSvc := cep.NewService(cfg.CEPAPIBaseURL, cfg.CEPTimeout)
	cepLimit := func(next http.Handler) http.Handler { return next }
	if cfg.RateLimitCEPPerMin > 0 {
		cepLimit = httpapi.RateLimitByIP(cfg.RateLimitCEPPerMin)
	}
	documentAdmin := document.NewAdminHandler(documentSvc, engine, adminHandler.Guard)
	documentPublic := document.NewPublicHandler(documentSvc, identifier)

	r := httpapi.NewRouter(httpapi.RouterOptions{
		Log: log, DB: pool, AllowedOrigins: cfg.CORSAllowedOrigins, MaxBodyBytes: cfg.MaxBodyBytes, GlobalRatePerMinute: cfg.RateLimitGlobalPerMin,
		OpenAPISpec: api.OpenAPI, TrustedProxies: trusted,
		Routes: func(r chi.Router) {
			adminHandler.Routes(r)
			audit.NewHandler(q).Routes(r, adminHandler.Guard)
			endUserAdmin.Routes(r)
			tokenAdmin.Routes(r)
			departmentAdmin.Routes(r)
			documentAdmin.Routes(r)
			catalog.NewHandler(catalogSvc, adminHandler.Guard).Routes(r)
			// Quem lê o catálogo de municípios (o proponente) também consulta o CEP, que serve para preencher município e UF.
			cep.NewHandler(cepSvc, adminHandler.Guard(catalog.ActionMunicipalitiesRead), cepLimit).Routes(r)
			rules.NewHandler(rulesSvc, adminHandler.Guard).Routes(r)
			project.NewHandler(projectSvc, adminHandler.Guard, project.Limits{Upload: cfg.RateLimitUploadPerMin, Heavy: project.DefaultHeavyLimit}).Routes(r)
			documentPublic.Routes(r)
			endUserHandler.Routes(r)
		},
	})
	mux := r.(*chi.Mux)
	return &App{Handler: mux, Router: mux, Authz: engine, Actions: actions, Identifier: identifier, EndUsers: endUserSvc, APITokens: tokenSvc, Documents: documentSvc, Catalog: catalogSvc, Rules: rulesSvc, Projects: projectSvc, Outbox: outbox.NewWorker(pool, mail, log, outbox.DefaultConfig()), log: log, pool: pool, noOutboxWorker: o.noOutboxWorker}, nil
}

// Start sincroniza o registro de ações com o banco e passa a escutar as
// invalidações de cache vindas de outras instâncias, até o contexto terminar.
func (a *App) Start(ctx context.Context) error {
	if err := a.EndUsers.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := a.Catalog.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := a.Rules.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := a.Projects.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := authz.SyncEndUserActions(ctx, db.New(a.pool), a.Actions, a.Authz, a.log); err != nil {
		return err
	}
	if err := authz.SyncActions(ctx, db.New(a.pool), a.Actions, a.Authz, a.log); err != nil {
		return err
	}
	go a.expireComplements(ctx)
	if !a.noOutboxWorker {
		a.outboxDone = make(chan struct{})
		go func() {
			defer close(a.outboxDone)
			a.Outbox.Run(ctx)
		}()
	}
	go func() {
		if err := a.Authz.Listen(ctx); err != nil {
			a.log.ErrorContext(ctx, "escuta de invalidações encerrada", "error", err)
		}
	}()
	return nil
}

// Wait espera o worker de e-mail terminar o envio que tem em andamento depois que o contexto de Start foi cancelado, por no máximo
// timeout. Sem isso o processo pode sair no meio de um envio e a mensagem, já entregue ao servidor SMTP, voltar à fila e sair de novo.
// Devolve false se o prazo acabou antes. Sem worker iniciado, devolve true na hora.
func (a *App) Wait(timeout time.Duration) bool {
	if a.outboxDone == nil {
		return true
	}
	select {
	case <-a.outboxDone:
		return true
	case <-time.After(timeout):
		return false
	}
}

// DrainOutbox envia agora todos os e-mails vencidos da caixa de saída e devolve quantos reservou.
func (a *App) DrainOutbox(ctx context.Context) (int, error) { return a.Outbox.Drain(ctx) }

// expireInterval é a frequência com que a rotina arquiva projetos com o prazo de complementação vencido.
const expireInterval = 5 * time.Minute

// expireComplements roda até o contexto terminar. Várias instâncias da API podem rodá-la ao mesmo tempo: cada pedido é
// travado por quem o processa. Além dela, o prazo também é conferido na hora em que o proponente abre ou altera o projeto.
func (a *App) expireComplements(ctx context.Context) {
	run := func() {
		if n, err := a.Projects.ExpireComplements(ctx); err != nil {
			a.log.ErrorContext(ctx, "arquivando projetos com prazo de complementação vencido", "error", err)
		} else if n > 0 {
			a.log.InfoContext(ctx, "projetos arquivados por prazo de complementação vencido", "quantidade", n)
		}
	}
	run()
	t := time.NewTicker(expireInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
