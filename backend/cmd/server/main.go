// Command server runs the KeiPay HTTP API and (optionally) the asynq worker.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/mydisha/keipay/backend/internal/config"
	"github.com/mydisha/keipay/backend/internal/pkg/crypto"
	"github.com/mydisha/keipay/backend/internal/pkg/logger"
	"github.com/mydisha/keipay/backend/internal/provider"
	"github.com/mydisha/keipay/backend/internal/provider/xendit"
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	redisrepo "github.com/mydisha/keipay/backend/internal/repository/redis"
	"github.com/mydisha/keipay/backend/internal/service"
	transport "github.com/mydisha/keipay/backend/internal/transport/http"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/transport/http/middleware"
	"github.com/mydisha/keipay/backend/internal/worker"
)

// Build information, set with -ldflags at build time and logged at startup.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	startTime := time.Now()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	log, err := logger.New(logger.Options{
		Service:    "keipay",
		Env:        cfg.AppEnv,
		Level:      cfg.LogLevel,
		Format:     cfg.LogFormat,
		Production: cfg.IsProduction(),
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	if cfg.JWTSecretSet {
		log.Warn("JWT_SECRET is no longer used; admin sessions are server-side. Remove it from the environment.")
	}

	log.Info("starting keipay",
		zap.String("version", Version),
		zap.String("commit", Commit),
		zap.String("build_time", BuildTime),
		zap.String("env", cfg.AppEnv),
		zap.String("port", cfg.HTTPPort),
		zap.Bool("worker_enabled", cfg.RunWorker),
		zap.Bool("cookie_secure", cfg.CookieSecure()),
		zap.Strings("trusted_proxies", cfg.TrustedProxies),
		zap.Bool("setup_token_set", cfg.SetupToken != ""),
		zap.Duration("shutdown_timeout", cfg.GetShutdownTimeout()),
	)

	ctx := context.Background()

	log.Debug("connecting to postgres")
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()
	log.Info("postgres connected successfully")

	log.Debug("connecting to redis", zap.String("addr", cfg.RedisAddr), zap.Int("db", cfg.RedisDB))
	rdb, err := redisrepo.New(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatal("failed to connect to redis", zap.Error(err), zap.String("addr", cfg.RedisAddr))
	}
	defer func() { _ = rdb.Close() }()
	log.Info("redis connected successfully", zap.String("addr", cfg.RedisAddr))

	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatal("failed to initialize cipher", zap.Error(err))
	}
	log.Debug("encryption cipher initialized")

	log.Debug("initializing repositories")
	userRepo := postgres.NewUserRepo(pool)
	appRepo := postgres.NewAppRepo(pool)
	apiKeyRepo := postgres.NewAPIKeyRepo(pool)
	paymentRepo := postgres.NewPaymentRepo(pool)
	reportRepo := postgres.NewPaymentReportRepo(pool)
	reconRepo := postgres.NewReconciliationRepo(pool)
	endpointRepo := postgres.NewWebhookEndpointRepo(pool)
	eventRepo := postgres.NewEventRepo(pool)
	deliveryRepo := postgres.NewDeliveryRepo(pool)
	inboundRepo := postgres.NewInboundWebhookRepo(pool)
	auditRepo := postgres.NewAuditRepo(pool)
	connRepo := postgres.NewConnectionRepo(pool)
	merchantRepo := postgres.NewMerchantRepo(pool)
	txMgr := postgres.NewTxManager(pool)

	// The /v1 nonce store. API keys and apps are not cached, so a
	// revoked key or disabled app is refused on the next request. Gateway
	// callbacks are deduplicated in Postgres (inbound_webhooks), not here.
	log.Debug("initializing nonce store")
	nonces := redisrepo.NewIdempotency(rdb, 10*time.Minute)
	sessions := redisrepo.NewSessionStore(rdb)
	loginFailures := redisrepo.NewFailureCounter(rdb)

	log.Debug("initializing asynq enqueuer")
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}
	enqueuer := worker.NewEnqueuer(redisOpt)
	defer func() { _ = enqueuer.Close() }()
	log.Debug("asynq enqueuer initialized")

	// Gateway adapters built into this server.
	registry, err := provider.NewRegistry(xendit.New())
	if err != nil {
		log.Fatal("failed to register providers", zap.Error(err))
	}

	log.Debug("initializing services")
	auditSvc := service.NewAuditService(auditRepo)
	appSvc := service.NewAppService(appRepo, apiKeyRepo, connRepo, cipher, txMgr, auditSvc)
	connSvc := service.NewConnectionService(service.ConnectionDeps{
		Connections: connRepo,
		Merchants:   merchantRepo,
		Apps:        appRepo,
		Payments:    paymentRepo,
		Registry:    registry,
		Sealer:      cipher,
		Tx:          txMgr,
		Audit:       auditSvc,
		Log:         log,
		BaseURL:     cfg.HTTPBaseURL,
	})
	paymentSvc := service.NewPaymentService(paymentRepo, connRepo, registry, cipher, log)
	reportSvc := service.NewPaymentReportService(reportRepo)
	reconSvc := service.NewReconciliationService(service.ReconciliationDeps{
		Runs:        reconRepo,
		Connections: connRepo,
		Registry:    registry,
		Source:      service.NewProviderSource(registry, cipher),
		Tx:          txMgr,
		Enqueuer:    enqueuer,
		Audit:       auditSvc,
		Log:         log,
	})
	webhookSvc := service.NewWebhookService(service.WebhookDeps{
		Payments:   paymentRepo,
		Reports:    reportRepo,
		Inbound:    inboundRepo,
		Events:     eventRepo,
		Endpoints:  endpointRepo,
		Deliveries: deliveryRepo,
		Tx:         txMgr,
		Enqueuer:   enqueuer,
		Log:        log,
	})
	endpointSvc := service.NewWebhookEndpointService(service.WebhookEndpointDeps{
		Apps:       appRepo,
		Endpoints:  endpointRepo,
		Events:     eventRepo,
		Deliveries: deliveryRepo,
		Cipher:     cipher,
		Tx:         txMgr,
		Audit:      auditSvc,
		Enqueuer:   enqueuer,
		Log:        log,
	})
	deliverySvc := service.NewDeliveryService(service.DeliveryDeps{
		Deliveries: deliveryRepo,
		Endpoints:  endpointRepo,
		Tx:         txMgr,
		Enqueuer:   enqueuer,
		Audit:      auditSvc,
		Log:        log,
	})
	inboundSvc := service.NewInboundService(inboundRepo)
	hasher := service.NewBcryptHasher(service.DefaultBcryptCost)
	userSvc := service.NewUserService(userRepo, txMgr, auditSvc, hasher)
	authSvc, err := service.NewAuthService(service.AuthDeps{
		Users:    userRepo,
		Sessions: sessions,
		Failures: loginFailures,
		Tx:       txMgr,
		Audit:    auditSvc,
		Hasher:   hasher,
		Log:      log,
	}, service.AuthConfig{
		IdleTimeout:         cfg.SessionIdleTimeout,
		AbsoluteTimeout:     cfg.SessionAbsoluteTimeout,
		MaxFailuresPerIP:    cfg.LoginMaxFailuresPerIP,
		MaxFailuresPerEmail: cfg.LoginMaxFailuresPerEmail,
		FailureWindow:       cfg.LoginFailureWindow,
		SetupToken:          cfg.SetupToken,
	})
	if err != nil {
		log.Fatal("failed to initialize auth service", zap.Error(err))
	}
	log.Debug("services initialized")

	// One-time import of the deprecated XENDIT_* settings, before serving so
	// the first callback and payment already find the connection.
	imported, err := connSvc.ImportLegacyXendit(ctx, cfg.XenditSecretKey, cfg.XenditCallbackToken)
	logLegacyImport(log, imported, err)

	frontendDir := resolveFrontendDir(cfg.FrontendDir)
	switch {
	case frontendDir != "":
		log.Info("serving admin console", zap.String("dir", frontendDir))
	case cfg.FrontendDir != "":
		log.Warn("FRONTEND_DIR has no index.html; admin console disabled", zap.String("dir", cfg.FrontendDir))
	default:
		log.Info("no frontend build found; admin console disabled (run the Vite dev server or build frontend/)")
	}

	log.Debug("initializing HTTP router")
	cookies := middleware.CookieConfig{Secure: cfg.CookieSecure()}
	deps := transport.Deps{
		Log:             log,
		AppResolver:     appSvc,
		NonceStore:      nonces,
		SecretDecryptor: cipher,
		Sessions:        authSvc,
		Cookies:         cookies,
		NewToken:        service.NewToken,
		TrustedProxies:  cfg.TrustedProxies,
		Payment:         handler.NewPaymentHandler(paymentSvc),
		Webhook:         handler.NewWebhookHandler(webhookSvc, connSvc, log),
		Admin:           handler.NewAdminHandler(paymentSvc, auditSvc),
		Reports:         handler.NewReportHandler(reportSvc, log),
		Reconciliation:  handler.NewReconciliationHandler(reconSvc, log),
		Apps:            handler.NewAppHandler(appSvc),
		Endpoints:       handler.NewWebhookEndpointHandler(endpointSvc),
		Deliveries:      handler.NewDeliveryHandler(deliverySvc, inboundSvc),
		Connections:     handler.NewConnectionHandler(connSvc),
		Auth:            handler.NewAuthHandler(authSvc, cookies, service.NewToken),
		Users:           handler.NewUserHandler(userSvc),
		FrontendDir:     frontendDir,
	}
	router := transport.NewRouter(deps)

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Optional in-process asynq worker, plus the sweeper that re-enqueues
	// deliveries whose next attempt is overdue.
	var asynqSrv *asynq.Server
	sweepCtx, stopSweeper := context.WithCancel(ctx)
	defer stopSweeper()
	if cfg.RunWorker {
		// The sweeper deletes archived tasks through the inspector before
		// re-enqueuing their attempt.
		inspector := asynq.NewInspector(redisOpt)
		defer func() { _ = inspector.Close() }()
		log.Info("starting asynq worker", zap.Int("concurrency", 10),
			zap.Int("disable_after_failures", cfg.WebhookDisableAfterFailures))
		asynqSrv = startWorker(redisOpt, worker.NewForwarder(worker.ForwarderDeps{
			Deliveries:           deliveryRepo,
			Endpoints:            endpointRepo,
			Tx:                   txMgr,
			Cipher:               cipher,
			Audit:                auditSvc,
			Enqueuer:             enqueuer,
			Log:                  log,
			DisableAfterFailures: cfg.WebhookDisableAfterFailures,
		}), worker.NewReconciler(reconSvc), log)
		go worker.NewSweeper(deliveryRepo, enqueuer, inspector, log).Run(sweepCtx)
		log.Info("asynq worker started")
	}

	go func() {
		log.Info("http server listening", zap.String("addr", srv.Addr), zap.String("base_url", cfg.HTTPBaseURL))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("http server failed", zap.Error(err))
		}
	}()

	startupDuration := time.Since(startTime)
	log.Info("server started successfully",
		zap.Duration("startup_duration_ms", startupDuration),
		zap.String("startup_duration", startupDuration.String()),
	)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	sig := <-stop
	log.Info("shutdown signal received",
		zap.String("signal", sig.String()),
		zap.Duration("shutdown_timeout", cfg.GetShutdownTimeout()),
	)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.GetShutdownTimeout())
	defer cancel()

	shutdownStart := time.Now()

	log.Info("shutting down HTTP server")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Error("http server shutdown timed out", zap.Error(err))
		} else {
			log.Error("http server shutdown error", zap.Error(err))
		}
	} else {
		log.Info("http server shutdown complete", zap.Duration("duration_ms", time.Since(shutdownStart)))
	}

	stopSweeper()
	if asynqSrv != nil {
		log.Info("shutting down asynq worker")
		workerShutdownStart := time.Now()
		asynqSrv.Shutdown()
		log.Info("asynq worker shutdown complete", zap.Duration("duration_ms", time.Since(workerShutdownStart)))
	}

	log.Debug("closing asynq enqueuer")
	if err := enqueuer.Close(); err != nil {
		log.Warn("error closing enqueuer", zap.Error(err))
	}

	totalShutdownDuration := time.Since(shutdownStart)
	log.Info("shutdown complete",
		zap.Duration("total_shutdown_duration_ms", totalShutdownDuration),
		zap.String("total_shutdown_duration", totalShutdownDuration.String()),
	)
}

func startWorker(redisOpt asynq.RedisClientOpt, fwd *worker.Forwarder, rec *worker.Reconciler, log *zap.Logger) *asynq.Server {
	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 10,
		Queues:      map[string]int{worker.QueueWebhooks: 10, worker.QueueReconciliation: 2},
		Logger:      newAsynqLogger(log),
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeWebhookForward, fwd.HandleWebhookForward)
	mux.HandleFunc(worker.TypeReconciliationRun, rec.HandleReconciliationRun)

	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatal("asynq worker failed to run", zap.Error(err))
		}
	}()
	return srv
}
