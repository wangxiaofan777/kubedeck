package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/domain"
)

type fakeStore struct {
	mu      sync.Mutex
	batches [][]domain.AuditEntry
	fail    bool
}

func (f *fakeStore) AppendAuditBatch(_ context.Context, entries []domain.AuditEntry) ([]domain.AuditRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("db down")
	}
	copied := append([]domain.AuditEntry(nil), entries...)
	f.batches = append(f.batches, copied)
	return nil, nil
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.batches {
		n += len(b)
	}
	return n
}

func testWriter(store Store) *Writer {
	w := NewWriter(store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.flush = 10 * time.Millisecond
	w.batch = 2
	return w
}

func TestWriterFlushesBatches(t *testing.T) {
	store := &fakeStore{}
	w := testWriter(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := 0; i < 3; i++ {
		if !w.Record(domain.AuditEntry{Verb: "delete", Resource: "pods"}) {
			t.Fatal("record rejected")
		}
	}
	deadline := time.Now().Add(time.Second)
	for store.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if store.count() != 3 {
		t.Fatalf("written = %d, want 3", store.count())
	}
}

func TestWriterRetriesAfterFailure(t *testing.T) {
	store := &fakeStore{fail: true}
	w := testWriter(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Record(domain.AuditEntry{Verb: "scale"})
	deadline := time.Now().Add(time.Second)
	for !w.Degraded() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !w.Degraded() {
		t.Fatal("writer should be degraded after failed write")
	}

	store.mu.Lock()
	store.fail = false
	store.mu.Unlock()
	deadline = time.Now().Add(time.Second)
	for store.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if store.count() != 1 {
		t.Fatalf("retried entries = %d, want 1", store.count())
	}
}

func TestWriterQueueOverflowSetsDegraded(t *testing.T) {
	w := &Writer{
		store:  &fakeStore{},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		queue:  make(chan domain.AuditEntry, 1),
		done:   make(chan struct{}),
	}
	w.Record(domain.AuditEntry{Verb: "a"})
	if w.Record(domain.AuditEntry{Verb: "b"}) {
		t.Fatal("second record should be rejected when queue is full")
	}
	if !w.Degraded() {
		t.Fatal("overflow should mark writer degraded")
	}
}
