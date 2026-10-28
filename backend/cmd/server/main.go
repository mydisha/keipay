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
	"github.com/mydisha/keipay/backend/internal/repository/postgres"
	redisrepo "github.com/mydisha/keipay/backend/internal/repository/redis"
	"github.com/mydisha/keipay/backend/internal/service"
	transport "github.com/mydisha/keipay/backend/internal/transport/http"
	"github.com/mydisha/keipay/backend/internal/transport/http/handler"
	"github.com/mydisha/keipay/backend/internal/worker"
)

// Build information, set via ldflags during build.
// These are used for startup logging and can be injected via Docker build args.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func main() {
	startTime := time.Now()

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	// Initialize logger.
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

	// Log build and startup information.
	log.Info("starting keipay",
		zap.String("version", Version),
		zap.String("commit", Commit),
		zap.String("build_time", BuildTime),
		zap.String("env", cfg.AppEnv),
		zap.String("port", cfg.HTTPPort),
		zap.Bool("worker_enabled", cfg.RunWorker),
		zap.Duration("shutdown_timeout", cfg.GetShutdownTimeout()),
	)

	ctx := context.Background()

	// Initialize infrastructure: PostgreSQL.
	log.Debug("connecting to postgres")
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal("failed to connect to postgres", zap.Error(err))
	}
	defer pool.Close()
	log.Info("postgres connected successfully")

	// Initialize infrastructure: Redis.
	log.Debug("connecting to redis", zap.String("addr", cfg.RedisAddr), zap.Int("db", cfg.RedisDB))
	rdb, err := redisrepo.New(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatal("failed to connect to redis", zap.Error(err), zap.String("addr", cfg.RedisAddr))
	}
	defer func() { _ = rdb.Close() }()
	log.Info("redis connected successfully", zap.String("addr", cfg.RedisAddr))

	// Initialize encryption cipher.
	cipher, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatal("failed to initialize cipher", zap.Error(err))
	}
	log.Debug("encryption cipher initialized")

	// Initialize repositories.
	log.Debug("initializing repositories")
	adminRepo := postgres.NewAdminRepo(pool)
	tenantRepo := postgres.NewTenantRepo(pool)
	paymentRepo := postgres.NewPaymentRepo(pool)
	ledgerRepo := postgres.NewLedgerRepo(pool)
	webhookRepo := postgres.NewWebhookRepo(pool)
	auditRepo := postgres.NewAuditRepo(pool)
	txMgr := postgres.NewTxManager(pool)

	// Initialize caches and idempotency stores.
	log.Debug("initializing caches and idempotency stores")
	tenantCache := redisrepo.NewTenantCache(rdb, tenantRepo)
	idemp := redisrepo.NewIdempotency(rdb, 24*time.Hour)
	nonces := redisrepo.NewIdempotency(rdb, 10*time.Minute)

	// Initialize asynq enqueuer.
	log.Debug("initializing asynq enqueuer")
	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}
	enqueuer := worker.NewEnqueuer(redisOpt, cfg.WebhookMaxRetry)
	defer func() { _ = enqueuer.Close() }()
	log.Debug("asynq enqueuer initialized", zap.Int("max_retry", cfg.WebhookMaxRetry))

	// Initialize external clients.
	log.Debug("initializing xendit client")
	xnd := xenditClient(cfg)
	log.Debug("xendit client initialized")

	// Initialize services.
	log.Debug("initializing services")
	auditSvc := service.NewAuditService(auditRepo)
	tenantSvc := service.NewTenantService(tenantRepo, tenantCache, cipher, txMgr, auditSvc)
	paymentSvc := service.NewPaymentService(paymentRepo, xnd)
	ledgerSvc := service.NewLedgerService(ledgerRepo)
	webhookSvc := service.NewWebhookService(paymentRepo, ledgerRepo, webhookRepo, txMgr, idemp, enqueuer)
	deliverySvc := service.NewDeliveryService(webhookRepo, enqueuer, auditSvc)
	authSvc := service.NewAuthService(adminRepo, cfg.JWTSecret, txMgr, auditSvc)
	log.Debug("services initialized")

	frontendDir := resolveFrontendDir(cfg.FrontendDir)
	switch {
	case frontendDir != "":
		log.Info("serving admin console", zap.String("dir", frontendDir))
	case cfg.FrontendDir != "":
		log.Warn("FRONTEND_DIR has no index.html; admin console disabled", zap.String("dir", cfg.FrontendDir))
	default:
		log.Info("no frontend build found; admin console disabled (run the Vite dev server or build frontend/)")
	}

	// Initialize handlers and router.
	log.Debug("initializing HTTP router")
	deps := transport.Deps{
		Log:             log,
		TenantResolver:  tenantCache,
		NonceStore:      nonces,
		SecretDecryptor: cipher,
		JWTVerifier:     authSvc,
		CallbackToken:   cfg.XenditCallbackToken,
		Payment:         handler.NewPaymentHandler(paymentSvc),
		Webhook:         handler.NewWebhookHandler(webhookSvc, log),
		Admin:           handler.NewAdminHandler(authSvc, tenantSvc, paymentSvc, ledgerSvc, deliverySvc, auditSvc),
		FrontendDir:     frontendDir,
	}
	router := transport.NewRouter(deps)

	// Create HTTP server.
	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Optional in-process asynq worker, plus the sweeper that re-enqueues
	// deliveries whose post-commit enqueue failed.
	var asynqSrv *asynq.Server
	sweepCtx, stopSweeper := context.WithCancel(ctx)
	defer stopSweeper()
	if cfg.RunWorker {
		log.Info("starting asynq worker", zap.Int("concurrency", 10))
		asynqSrv = startWorker(redisOpt, tenantRepo, webhookRepo, cipher, log, cfg.WebhookMaxRetry)
		go worker.NewSweeper(webhookRepo, enqueuer, log).Run(sweepCtx)
		log.Info("asynq worker started")
	}

	// Run HTTP server in background.
	go func() {
		log.Info("http server listening", zap.String("addr", srv.Addr), zap.String("base_url", cfg.HTTPBaseURL))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("http server failed", zap.Error(err))
		}
	}()

	// Log successful startup with duration.
	startupDuration := time.Since(startTime)
	log.Info("server started successfully",
		zap.Duration("startup_duration_ms", startupDuration),
		zap.String("startup_duration", startupDuration.String()),
	)

	// =============================
	// Graceful Shutdown
	// =============================
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	sig := <-stop
	log.Info("shutdown signal received",
		zap.String("signal", sig.String()),
		zap.Duration("shutdown_timeout", cfg.GetShutdownTimeout()),
	)

	// Create shutdown context with configurable timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.GetShutdownTimeout())
	defer cancel()

	// Track shutdown progress.
	shutdownStart := time.Now()

	// Shutdown HTTP server.
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

	// Shutdown asynq worker if running.
	stopSweeper()
	if asynqSrv != nil {
		log.Info("shutting down asynq worker")
		workerShutdownStart := time.Now()
		asynqSrv.Shutdown()
		log.Info("asynq worker shutdown complete", zap.Duration("duration_ms", time.Since(workerShutdownStart)))
	}

	// Close enqueuer.
	log.Debug("closing asynq enqueuer")
	if err := enqueuer.Close(); err != nil {
		log.Warn("error closing enqueuer", zap.Error(err))
	}

	// Log final shutdown summary.
	totalShutdownDuration := time.Since(shutdownStart)
	log.Info("shutdown complete",
		zap.Duration("total_shutdown_duration_ms", totalShutdownDuration),
		zap.String("total_shutdown_duration", totalShutdownDuration.String()),
	)
}

func startWorker(
	redisOpt asynq.RedisClientOpt,
	tenantRepo *postgres.TenantRepo,
	webhookRepo *postgres.WebhookRepo,
	cipher *crypto.Cipher,
	log *zap.Logger,
	maxRetry int,
) *asynq.Server {
	fwd := worker.NewForwarder(tenantRepo, webhookRepo, cipher, log, maxRetry)

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 10,
		Queues:      map[string]int{"webhooks": 10},
		Logger:      newAsynqLogger(log),
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeWebhookForward, fwd.HandleWebhookForward)

	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatal("asynq worker failed to run", zap.Error(err))
		}
	}()
	return srv
}
