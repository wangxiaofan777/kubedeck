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
	"github.com/kubedeck/kubedeck/internal/config"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/store"
)

const (
	shutdownTimeout   = 10 * time.Second
	bootstrapTimeout  = 60 * time.Second
	auditCheckPeriod  = 24 * time.Hour
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

	var pinger api.Pinger
	if st != nil {
		pinger = st
	}

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: api.NewRouter(api.Options{
			AllowedOrigins: cfg.AllowedOrigins,
			Registry:       registry,
			Pinger:         pinger,
		}),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerDone := make(chan struct{})
	if st != nil {
		go func() {
			defer close(workerDone)
			auditMaintenance(runCtx, st, logger)
		}()
	} else {
		close(workerDone)
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Addr)
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
	<-workerDone
	logger.Info("server stopped")
	return runErr
}

// openDatabase 连接数据库并应用迁移；未配置连接串时返回 nil（dev 模式）。
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

// buildRegistry 注册集群。当前只支持 dev 模式的本地 kubeconfig；
// 数据库驱动的集群登记在后续任务中接入。
func buildRegistry(cfg config.Config, logger *slog.Logger) (*kube.Registry, error) {
	registry := kube.NewRegistry()
	if cfg.DevKubeconfig == "" {
		return registry, nil
	}
	t, err := kube.NewDirectTransport(cfg.DevClusterName, cfg.DevKubeconfig, cfg.DevUser)
	if err != nil {
		return nil, fmt.Errorf("init dev cluster: %w", err)
	}
	registry.Add(t)
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
