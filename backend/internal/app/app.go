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
	"github.com/wagner/convenios-e-fomentos/backend/internal/audit"
	"github.com/wagner/convenios-e-fomentos/backend/internal/auth"
	"github.com/wagner/convenios-e-fomentos/backend/internal/authz"
	"github.com/wagner/convenios-e-fomentos/backend/internal/catalog"
	"github.com/wagner/convenios-e-fomentos/backend/internal/cep"
	"github.com/wagner/convenios-e-fomentos/backend/internal/config"
	"github.com/wagner/convenios-e-fomentos/backend/internal/database/db"
	"github.com/wagner/convenios-e-fomentos/backend/internal/department"
	"github.com/wagner/convenios-e-fomentos/backend/internal/httpapi"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mailer"
	"github.com/wagner/convenios-e-fomentos/backend/internal/mtid"
	"github.com/wagner/convenios-e-fomentos/backend/internal/outbox"
	"github.com/wagner/convenios-e-fomentos/backend/internal/preview"
	"github.com/wagner/convenios-e-fomentos/backend/internal/project"
	"github.com/wagner/convenios-e-fomentos/backend/internal/rules"
	"github.com/wagner/convenios-e-fomentos/backend/internal/scan"
	"github.com/wagner/convenios-e-fomentos/backend/internal/sigadoc"
	"github.com/wagner/convenios-e-fomentos/backend/internal/storage"
)

// AdminService cria o serviço de administradores sem motor de permissões
// (uso da CLI, que não avalia permissões).
func AdminService(pool *pgxpool.Pool) *admin.Service {
	return admin.NewService(pool, nil)
}

// App agrupa o roteador e as peças que precisam de ciclo de vida.
type App struct {
	Handler http.Handler
	// Router permite registrar rotas dos recursos (Fase 7) e injetar rotas nos testes.
	Router   *chi.Mux
	Authz    *authz.Engine
	Actions  *authz.Registry
	Catalog  *catalog.Service
	Rules    *rules.Service
	Projects *project.Service
	// Outbox envia a caixa de saída de e-mails em segundo plano (iniciado por Start).
	Outbox *outbox.Worker

	log            *slog.Logger
	pool           *pgxpool.Pool
	noOutboxWorker bool
	noReportWorker bool
	// noSigadocWatcher: os testes controlam quando o acompanhamento do SIGADOC roda.
	noSigadocWatcher bool
	// outboxDone fecha quando o worker de e-mail termina (ver Wait). Nulo se o worker não foi iniciado.
	outboxDone chan struct{}
}

// Option ajusta a montagem do grafo (usada principalmente pelos testes).
type Option func(*options)

type options struct {
	store          storage.Store
	mtid           mtid.Client
	converter      preview.Converter
	sigadoc        sigadoc.Client
	noOutboxWorker bool
	noReportWorker bool
	// noSigadocWatcher: veja WithoutSigadocWatcher.
	noSigadocWatcher bool
	extraActions     []func(*authz.Registry)
}

// WithStore usa o armazenamento dado em vez de criar o S3 a partir da configuração.
func WithStore(s storage.Store) Option { return func(o *options) { o.store = s } }

// WithActions registra ações a mais no motor de permissões. Os testes usam para ter um recurso de exemplo com campos e condições,
// que nenhum recurso real do sistema oferece.
func WithActions(register func(*authz.Registry)) Option {
	return func(o *options) { o.extraActions = append(o.extraActions, register) }
}

// WithMTID liga o MT-ID (assinatura das declarações). Sem ele, emitir declaração responde 503: o cliente HTTP real ainda não existe
// (docs/debito-tecnico-mt-id.md).
func WithMTID(c mtid.Client) Option { return func(o *options) { o.mtid = c } }

// WithSigadoc liga o SIGADOC (abertura e acompanhamento do processo do projeto). Sem ele, o acompanhamento não roda.
func WithSigadoc(c sigadoc.Client) Option { return func(o *options) { o.sigadoc = c } }

// WithoutSigadocWatcher não inicia o acompanhamento do SIGADOC em segundo plano. Os testes usam para controlar quando ele roda
// (App.Projects.ProcessSigadocWatches).
func WithoutSigadocWatcher() Option { return func(o *options) { o.noSigadocWatcher = true } }

// WithPreviewConverter usa o conversor de Word para PDF dado em vez do Gotenberg da configuração.
func WithPreviewConverter(c preview.Converter) Option { return func(o *options) { o.converter = c } }

// WithoutOutboxWorker não inicia o envio de e-mails em segundo plano. Os testes usam para controlar quando a caixa de saída é
// esvaziada (App.DrainOutbox) e assim ler os e-mails de forma determinística.
func WithoutOutboxWorker() Option { return func(o *options) { o.noOutboxWorker = true } }

// WithoutReportWorker não inicia a montagem do relatório completo em segundo plano. Os testes usam para controlar quando a fila é
// processada (App.Projects.ProcessNextReport).
func WithoutReportWorker() Option { return func(o *options) { o.noReportWorker = true } }

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
	catalog.Register(actions)
	rules.Register(actions)
	project.Register(actions)
	for _, register := range o.extraActions {
		register(actions)
	}
	conds := authz.NewConditionRegistry()
	store := authz.DBStore{Q: q}
	authz.RegisterStandardConditions(conds, store)
	project.RegisterConditions(conds, q)
	engine := authz.NewEngine(actions, conds, store, authz.PGBroadcaster{Pool: pool, Log: log}, log)

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

	departmentAdmin := department.NewHandler(department.NewService(pool), adminHandler.Guard, adminHandler.GuardAny)
	catalogSvc := catalog.NewService(pool)
	rulesSvc := rules.NewService(pool)
	projectSvc := project.NewService(pool, rulesSvc)
	projectSvc.SetNotifier(cfg.PublicURL, log)
	if cfg.ClamAVAddr != "" {
		projectSvc.SetScanner(scan.ClamAV{Addr: cfg.ClamAVAddr})
	}
	if o.sigadoc != nil {
		projectSvc.SetSigadoc(o.sigadoc)
		projectSvc.SetSigadocLookup(cfg.SigadocLookupMatricula)
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
		projectSvc.SetReports(project.ReportConfig{TmpDir: cfg.ReportTmpDir, MaxInputBytes: int64(cfg.ReportMaxInputMB) << 20, MaxPages: cfg.ReportMaxPages,
			Timeout: cfg.ReportTimeout, Retention: cfg.ReportRetention})
	}
	cepSvc := cep.NewService(cfg.CEPAPIBaseURL, cfg.CEPTimeout)
	cepLimit := func(next http.Handler) http.Handler { return next }
	if cfg.RateLimitCEPPerMin > 0 {
		cepLimit = httpapi.RateLimitByIP(cfg.RateLimitCEPPerMin)
	}

	r := httpapi.NewRouter(httpapi.RouterOptions{
		Log: log, DB: pool, AllowedOrigins: cfg.CORSAllowedOrigins, MaxBodyBytes: cfg.MaxBodyBytes, GlobalRatePerMinute: cfg.RateLimitGlobalPerMin,
		OpenAPISpec: api.OpenAPI, TrustedProxies: trusted,
		Routes: func(r chi.Router) {
			adminHandler.Routes(r)
			audit.NewHandler(q).Routes(r, adminHandler.Guard)
			departmentAdmin.Routes(r)
			catalog.NewHandler(catalogSvc, adminHandler.Guard).Routes(r)
			// Quem lê o catálogo de municípios (o proponente) também consulta o CEP, que serve para preencher município e UF.
			cep.NewHandler(cepSvc, adminHandler.Guard(catalog.ActionMunicipalitiesRead), cepLimit).Routes(r)
			rules.NewHandler(rulesSvc, adminHandler.Guard).Routes(r)
			project.NewHandler(projectSvc, adminHandler.Guard, project.Limits{Upload: cfg.RateLimitUploadPerMin, Heavy: project.DefaultHeavyLimit}).Routes(r)
		},
	})
	mux := r.(*chi.Mux)
	return &App{Handler: mux, Router: mux, Authz: engine, Actions: actions, Catalog: catalogSvc, Rules: rulesSvc, Projects: projectSvc, Outbox: outbox.NewWorker(pool, mail, log, outbox.DefaultConfig()), log: log, pool: pool, noOutboxWorker: o.noOutboxWorker, noReportWorker: o.noReportWorker, noSigadocWatcher: o.noSigadocWatcher}, nil
}

// Start sincroniza o registro de ações com o banco e passa a escutar as
// invalidações de cache vindas de outras instâncias, até o contexto terminar.
func (a *App) Start(ctx context.Context) error {
	if err := a.Catalog.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := a.Rules.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := a.Projects.EnsureDefaults(ctx); err != nil {
		return err
	}
	if err := authz.SyncActions(ctx, db.New(a.pool), a.Actions, a.Authz, a.log); err != nil {
		return err
	}
	go a.expireComplements(ctx)
	if !a.noReportWorker {
		go a.Projects.RunReportWorker(ctx)
	}
	// As montagens do processo no SIGADOC rodam em segundo plano com o contexto da aplicação; as que estavam em execução quando a API
	// caiu perderam o PIN e ficam interrompidas.
	a.Projects.SetSigadocContext(ctx)
	if n, err := a.Projects.InterruptStaleSigadocTasks(ctx); err != nil {
		return err
	} else if n > 0 {
		a.log.WarnContext(ctx, "montagens do processo no SIGADOC interrompidas pelo reinício", "quantidade", n)
	}
	if !a.noSigadocWatcher {
		go a.Projects.RunSigadocWatcher(ctx)
	}
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
	start := time.Now()
	ok := true
	if a.outboxDone != nil {
		select {
		case <-a.outboxDone:
		case <-time.After(timeout):
			ok = false
		}
	}
	// As montagens do processo no SIGADOC já receberam o cancelamento do contexto; esperam gravar o resultado (interrompida).
	if left := timeout - time.Since(start); left > 0 {
		ok = a.Projects.WaitSigadoc(left) && ok
	} else {
		ok = false
	}
	return ok
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
