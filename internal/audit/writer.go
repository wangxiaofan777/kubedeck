// Package audit 实现异步审计写入与补偿重试。
package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

const (
	defaultQueueSize   = 4096
	defaultBatchSize   = 200
	defaultFlushPeriod = time.Second
	maxRetryEntries    = 2000
	flushTimeout       = 10 * time.Second
)

// Store 是审计写入所需的最小仓储能力。
type Store interface {
	AppendAuditBatch(ctx context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error)
}

// Writer 异步批量写入审计。业务请求只做非阻塞入队，不等待数据库。
type Writer struct {
	store  Store
	logger *slog.Logger
	queue  chan domain.AuditEntry
	done   chan struct{}
	flush  time.Duration
	batch  int

	mu       sync.Mutex
	retry    []domain.AuditEntry
	degraded bool
	dropped  int
}

// NewWriter 创建审计写入器。store 为空时所有写入为 no-op。
func NewWriter(store Store, logger *slog.Logger) *Writer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Writer{
		store:  store,
		logger: logger,
		queue:  make(chan domain.AuditEntry, defaultQueueSize),
		done:   make(chan struct{}),
		flush:  defaultFlushPeriod,
		batch:  defaultBatchSize,
	}
}

// Record 非阻塞入队。队列满时记录错误并返回 false，绝不阻塞业务请求。
func (w *Writer) Record(entry domain.AuditEntry) bool {
	if w.store == nil {
		return true
	}
	select {
	case w.queue <- entry:
		return true
	default:
		w.mu.Lock()
		w.dropped++
		w.degraded = true
		dropped := w.dropped
		w.mu.Unlock()
		w.logger.Error("audit queue full; entry dropped",
			"requestId", entry.RequestID, "verb", entry.Verb, "dropped", dropped)
		return false
	}
}

// Degraded 表示是否发生过审计积压或写入失败。
func (w *Writer) Degraded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.degraded
}

// Run 启动后台循环，直到 ctx 结束；退出前尽力刷写剩余记录。
func (w *Writer) Run(ctx context.Context) {
	defer close(w.done)
	if w.store == nil {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(w.flush)
	defer ticker.Stop()

	batch := make([]domain.AuditEntry, 0, w.batch)
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case e := <-w.queue:
					batch = append(batch, e)
					if len(batch) >= w.batch {
						w.flushBatch(batch)
						batch = batch[:0]
					}
				default:
					if len(batch) > 0 {
						w.flushBatch(batch)
					}
					return
				}
			}
		case e := <-w.queue:
			batch = append(batch, e)
			if len(batch) >= w.batch {
				w.flushBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}
			w.retryPending()
		}
	}
}

// Wait 等待 Run 退出。
func (w *Writer) Wait() { <-w.done }

func (w *Writer) flushBatch(batch []domain.AuditEntry) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if _, err := w.store.AppendAuditBatch(ctx, batch); err != nil {
		w.logger.Error("audit write failed; queued for retry", "err", err, "entries", len(batch))
		w.enqueueRetry(batch)
		return
	}
	w.mu.Lock()
	w.degraded = false
	w.mu.Unlock()
}

func (w *Writer) retryPending() {
	w.mu.Lock()
	if len(w.retry) == 0 {
		w.mu.Unlock()
		return
	}
	pending := w.retry
	w.retry = nil
	w.mu.Unlock()
	w.flushBatch(pending)
}

func (w *Writer) enqueueRetry(entries []domain.AuditEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.degraded = true
	space := maxRetryEntries - len(w.retry)
	if space <= 0 {
		w.dropped += len(entries)
		w.logger.Error("audit retry queue full; entries dropped", "dropped", w.dropped)
		return
	}
	if len(entries) > space {
		w.dropped += len(entries) - space
		entries = entries[:space]
	}
	w.retry = append(w.retry, entries...)
}
