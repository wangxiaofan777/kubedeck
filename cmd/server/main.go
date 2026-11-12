// Package main 是 KubeDeck 服务端入口。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kubedeck/kubedeck/internal/api"
	"github.com/kubedeck/kubedeck/internal/config"
	"github.com/kubedeck/kubedeck/internal/kube"
	"github.com/kubedeck/kubedeck/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	slog.SetDefault(logger)

	// 数据库：配置了连接串就连接并前置执行迁移；dev 模式可不配。
	if cfg.DatabaseURL != "" {
		st, err := initDatabase(ctx, cfg.DatabaseURL, logger)
		if err != nil {
			logger.Error("init database", "err", err)
			os.Exit(1)
		}
		defer st.Close()
		go auditMaintenance(ctx, st, logger)
	}

	// 开发模式：直接用本地 kubeconfig 注册一个集群，跳过 OIDC 与数据库。
	// 生产模式（后续任务）由数据库中的集群登记驱动。
	registry := kube.NewRegistry()
	if cfg.DevKubeconfig != "" {
		t, err := kube.NewDirectTransport(cfg.DevClusterName, cfg.DevKubeconfig, cfg.DevUser)
		if err != nil {
			logger.Error("init dev cluster", "err", err)
			os.Exit(1)
		}
		registry.Add(t)
		logger.Info("dev cluster registered", "name", cfg.DevClusterName, "kubeconfig", cfg.DevKubeconfig)
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewRouter(cfg, registry),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("server failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}

// initDatabase 连接数据库并应用迁移，同时补齐审计分区。
func initDatabase(ctx context.Context, url string, logger *slog.Logger) (*store.Store, error) {
	bootCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	st, err := store.Open(bootCtx, url)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(bootCtx, st.Pool()); err != nil {
		st.Close()
		return nil, err
	}
	versions, err := store.AppliedMigrations(bootCtx, st.Pool())
	if err != nil {
		st.Close()
		return nil, err
	}
	partitions, err := st.EnsureAuditPartitions(bootCtx, 3)
	if err != nil {
		st.Close()
		return nil, err
	}
	logger.Info("database ready", "migrations", versions, "audit_partitions", partitions)
	return st, nil
}

// auditMaintenance 每日检查审计分区：补齐未来月份、删除超出保留期的整月分区。
func auditMaintenance(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		days, err := st.RetentionDays(ctx)
		if err != nil {
			logger.Warn("read audit retention", "err", err)
			days = 180
		}
		if _, err := st.EnsureAuditPartitions(ctx, 3); err != nil {
			logger.Warn("ensure audit partitions", "err", err)
		}
		if dropped, err := st.DropExpiredAuditPartitions(ctx, days); err != nil {
			logger.Warn("drop expired audit partitions", "err", err)
		} else if len(dropped) > 0 {
			logger.Info("expired audit partitions dropped", "partitions", dropped, "retention_days", days)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
