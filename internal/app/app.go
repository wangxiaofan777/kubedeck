// Package app 负责进程级依赖组装与生命周期管理。
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kubedeck/kubedeck/internal/api"
	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/cluster"
	"github.com/kubedeck/kubedeck/internal/config"
	"github.com/kubedeck/kubedeck/internal/crypto"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/store"
)

const (
	shutdownTimeout   = 10 * time.Second
	bootstrapTimeout  = 60 * time.Second
	auditCheckPeriod  = 24 * time.Hour
	sessionCheckTick  = 60 * time.Second
	healthCheckPeriod = 60 * time.Second
	auditMonthsAhead  = 3
	defaultRetention  = 180
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 90 * time.Second
)

// Run 启动 HTTP 服务与后台任务，直到 ctx 结束或服务出错。
// 退出顺序：停止接收请求 → 停止后台任务 → 释放数据库连接（由 defer 完成）。
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if cfg.DevKubeconfig == "" && cfg.DatabaseURL == "" {
		return errors.New("database_url is required unless dev_kubeconfig is set")
	}

	st, err := openDatabase(ctx, cfg, logger)
	if err != nil {
		return err
	}
	if st != nil {
		defer st.Close()
	}

	registry, err := buildRegistry(cfg, logger)
	if err != nil {
		return err
	}

	authSvc, sessions, err := buildAuth(ctx, cfg, st, logger)
	if err != nil {
		return err
	}

	ring, err := buildKeyRing(cfg)
	if err != nil {
		return err
	}
	clusterMgr := cluster.NewManager(registry, st, ring, logger)
	if cfg.DevKubeconfig == "" {
		if err := clusterMgr.Load(ctx); err != nil {
			logger.Warn("load registered clusters", "err", err)
		}
	}

	var pinger api.Pinger
	if st != nil {
		pinger = st
	}

	opts := api.Options{
		AllowedOrigins: cfg.AllowedOrigins,
		Registry:       registry,
		Pinger:         pinger,
		Auth:           authSvc,
		ClusterAdmin:   clusterMgr,
		BootstrapToken: cfg.BootstrapToken,
		CookieSecure:   !cfg.CookieInsecure,
	}
	if st != nil {
		opts.AuthStore = st
		opts.AdminStore = st
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewRouter(opts),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var workers []chan struct{}
	if st != nil {
		workers = append(workers, startWorker(runCtx, func(ctx context.Context) {
			auditMaintenance(ctx, st, logger)
		}))
	}
	workers = append(workers, startWorker(runCtx, func(ctx context.Context) {
		sessionMaintenance(ctx, sessions, authSvc, logger)
	}))
	workers = append(workers, startWorker(runCtx, func(ctx context.Context) {
		clusterHealth(ctx, clusterMgr, logger)
	}))

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Addr, "auth", authMode(cfg))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var runErr error
	select {
	case runErr = <-errCh:
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("shutdown http server", "err", err)
		if runErr == nil {
			runErr = err
		}
	}

	cancel()
	for _, done := range workers {
		<-done
	}
	logger.Info("server stopped")
	return runErr
}

func startWorker(ctx context.Context, fn func(context.Context)) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(ctx)
	}()
	return done
}

func authMode(cfg config.Config) string {
	if cfg.DevKubeconfig != "" {
		return "dev"
	}
	return "oidc"
}

// openDatabase 连接数据库并应用迁移；未配置连接串时返回 nil（仅 dev 模式允许）。
func openDatabase(ctx context.Context, cfg config.Config, logger *slog.Logger) (*store.Store, error) {
	if cfg.DatabaseURL == "" {
		logger.Warn("database not configured; platform metadata and audit are disabled")
		return nil, nil
	}

	bootCtx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	defer cancel()

	st, err := store.Open(bootCtx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := store.Migrate(bootCtx, st.Pool()); err != nil {
		st.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	versions, err := store.AppliedMigrations(bootCtx, st.Pool())
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	partitions, err := st.EnsureAuditPartitions(bootCtx, auditMonthsAhead)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("ensure audit partitions: %w", err)
	}
	logger.Info("database ready", "migrations", versions, "audit_partitions", partitions)
	return st, nil
}

// buildAuth 构造认证服务。开发模式使用本地身份（显式配置才启用）；
// 生产模式从 OIDC discovery 开始，IdP 不可达时启动失败。
func buildAuth(ctx context.Context, cfg config.Config, st *store.Store, logger *slog.Logger) (*auth.Service, *auth.MemorySessions, error) {
	sessions := auth.NewMemorySessions(cfg.MaxSessionsPerUser, time.Duration(cfg.SessionTTLHours)*time.Hour)

	if cfg.DevKubeconfig != "" {
		subject := cfg.DevUser
		if subject == "" {
			subject = "dev"
		}
		id := auth.Identity{
			Subject:       subject,
			KubeUser:      cfg.DevUser, // 为空时不 Impersonation，直接用 kubeconfig 身份
			Email:         subject,
			DisplayName:   "Local Developer",
			PlatformAdmin: true,
			Dev:           true,
		}
		if st != nil {
			user, err := st.UpsertUser(ctx, domain.User{
				Subject:     subject,
				Email:       subject,
				DisplayName: "Local Developer",
				Source:      "local",
			})
			if err != nil {
				return nil, nil, fmt.Errorf("upsert dev user: %w", err)
			}
			id.UserID = user.ID
			id.Email = user.Email
		}
		logger.Warn("development auth enabled; no login or platform authorization is enforced",
			"user", subject)
		return auth.NewService(auth.Config{Sessions: sessions, Dev: &id, Users: st, Logger: logger}), sessions, nil
	}

	provider, err := auth.NewProvider(ctx, auth.OIDCConfig{
		Issuer:       cfg.OIDCIssuer,
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Scopes:       cfg.OIDCScopes,
	})
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, errors.New("database is required when OIDC auth is enabled")
	}
	return auth.NewService(auth.Config{
		Provider: provider,
		Users:    st,
		Sessions: sessions,
		Logger:   logger,
	}), sessions, nil
}

// buildRegistry 注册开发模式集群；生产集群由数据库登记在阶段 2 接入。
func buildRegistry(cfg config.Config, logger *slog.Logger) (*kube.Registry, error) {
	registry := kube.NewRegistry()
	if cfg.DevKubeconfig == "" {
		return registry, nil
	}
	cluster, err := kube.NewDirectClusterFromKubeconfig(cfg.DevClusterName, cfg.DevKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("init dev cluster: %w", err)
	}
	registry.Add(cluster)
	logger.Info("dev cluster registered", "name", cfg.DevClusterName, "kubeconfig", cfg.DevKubeconfig)
	return registry, nil
}

// auditMaintenance 补齐审计分区，并按保留期清理过期分区。
// 保留期配置读取失败时只告警并跳过删除，避免用默认值误删数据。
func auditMaintenance(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(auditCheckPeriod)
	defer ticker.Stop()

	for {
		if _, err := st.EnsureAuditPartitions(ctx, auditMonthsAhead); err != nil && ctx.Err() == nil {
			logger.Warn("ensure audit partitions", "err", err)
		}

		days, err := st.RetentionDays(ctx)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			days = defaultRetention
		case err != nil:
			logger.Warn("read audit retention; skip partition cleanup", "err", err)
			days = 0
		}
		if days > 0 {
			dropped, err := st.DropExpiredAuditPartitions(ctx, days)
			if err != nil && ctx.Err() == nil {
				logger.Warn("drop expired audit partitions", "err", err)
			} else if len(dropped) > 0 {
				logger.Info("expired audit partitions dropped", "partitions", dropped, "retention_days", days)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sessionMaintenance 清理过期会话并周期性向 IdP 校验撤销状态。
func sessionMaintenance(ctx context.Context, sessions *auth.MemorySessions, svc *auth.Service, logger *slog.Logger) {
	ticker := time.NewTicker(sessionCheckTick)
	defer ticker.Stop()
	for {
		if n := sessions.Cleanup(); n > 0 {
			logger.Debug("expired sessions cleaned", "count", n)
		}
		svc.RevalidateIdP(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// buildKeyRing 从配置构建凭据加密密钥环；未配置主密钥时返回 nil（仅开发模式可用）。
func buildKeyRing(cfg config.Config) (*crypto.Ring, error) {
	if cfg.MasterKey == "" {
		return nil, nil
	}
	key, err := crypto.ParseKey(cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("parse master key: %w", err)
	}
	keys := map[int][]byte{cfg.MasterKeyVersion: key}
	if cfg.PreviousMasterKey != "" {
		prev, err := crypto.ParseKey(cfg.PreviousMasterKey)
		if err != nil {
			return nil, fmt.Errorf("parse previous master key: %w", err)
		}
		keys[cfg.PreviousMasterKeyVersion] = prev
	}
	ring, err := crypto.NewRing(cfg.MasterKeyVersion, keys)
	if err != nil {
		return nil, err
	}
	return ring, nil
}

// clusterHealth 周期探测全部集群，更新三态与版本；启动后立即执行一次。
func clusterHealth(ctx context.Context, mgr *cluster.Manager, logger *slog.Logger) {
	mgr.ProbeAll(ctx)
	ticker := time.NewTicker(healthCheckPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mgr.ProbeAll(ctx)
		}
	}
}
