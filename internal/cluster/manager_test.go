package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubedeck/kubedeck/internal/crypto"
	"github.com/kubedeck/kubedeck/internal/domain"
	"github.com/kubedeck/kubedeck/internal/kube"
)

type fakeStore struct {
	mu       sync.Mutex
	nextID   int64
	clusters map[string]domain.Cluster
	creds    map[int64]domain.Credential
}

func newFakeStore() *fakeStore {
	return &fakeStore{nextID: 1, clusters: map[string]domain.Cluster{}, creds: map[int64]domain.Credential{}}
}

func (f *fakeStore) Clusters(context.Context) ([]domain.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Cluster, 0, len(f.clusters))
	for _, c := range f.clusters {
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeStore) ClusterByName(_ context.Context, name string) (domain.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clusters[name]
	if !ok {
		return domain.Cluster{}, domain.ErrNotFound
	}
	return c, nil
}
func (f *fakeStore) ClusterByID(_ context.Context, id int64) (domain.Cluster, error) {
	for _, c := range f.clusters {
		if c.ID == id {
			return c, nil
		}
	}
	return domain.Cluster{}, domain.ErrNotFound
}
func (f *fakeStore) CredentialByClusterID(_ context.Context, id int64) (domain.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[id]
	if !ok {
		return domain.Credential{}, domain.ErrNotFound
	}
	return c, nil
}
func (f *fakeStore) CreateClusterWithCredential(_ context.Context, c domain.Cluster, cred domain.Credential) (domain.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.clusters[c.Name]; exists {
		return domain.Cluster{}, fmt.Errorf("%w: duplicate", domain.ErrConflict)
	}
	c.ID = f.nextID
	f.nextID++
	c.CreatedAt = time.Now()
	c.UpdatedAt = c.CreatedAt
	f.clusters[c.Name] = c
	cred.ClusterID = c.ID
	cred.RotatedAt = time.Now()
	f.creds[c.ID] = cred
	return c, nil
}
func (f *fakeStore) ReplaceCredential(_ context.Context, c domain.Credential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.RotatedAt = time.Now()
	f.creds[c.ClusterID] = c
	return nil
}
func (f *fakeStore) UpdateClusterConnection(_ context.Context, id int64, server, version, status, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, c := range f.clusters {
		if c.ID == id {
			c.Server, c.ServerVersion, c.Status, c.Message = server, version, domain.ClusterStatus(status), message
			f.clusters[name] = c
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *fakeStore) UpdateClusterStatus(_ context.Context, id int64, status domain.ClusterStatus, message, version string, _ time.Time) error {
	return f.UpdateClusterConnection(context.Background(), id, "", version, string(status), message)
}
func (f *fakeStore) RenameCluster(_ context.Context, id int64, displayName string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, c := range f.clusters {
		if c.ID == id {
			c.DisplayName, c.Labels = displayName, labels
			f.clusters[name] = c
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *fakeStore) DeleteCluster(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, c := range f.clusters {
		if c.ID == id {
			delete(f.clusters, name)
			delete(f.creds, id)
			return nil
		}
	}
	return domain.ErrNotFound
}
func (f *fakeStore) GetSetting(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`"1.34"`), nil
}

func fakeAPIServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"major": "1", "minor": "34", "gitVersion": version,
			})
		case "/readyz":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func kubeconfigFor(server string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
clusters:
- name: test
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: test
  context:
    cluster: test
    user: test
users:
- name: test
  user:
    token: test-token
`, server)
}

func testRing(t *testing.T) *crypto.Ring {
	t.Helper()
	key := make([]byte, 32)
	ring, err := crypto.NewRing(1, map[int][]byte{1: key})
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestManagerCreateEncryptsAndRegisters(t *testing.T) {
	srv := fakeAPIServer(t, "v1.37.2+k3s1")
	store := newFakeStore()
	registry := kube.NewRegistry()
	mgr := NewManager(registry, store, testRing(t), nil)

	c, err := mgr.Create(context.Background(), CreateRequest{
		Name:       "prod",
		Credential: kube.IngestInput{Kubeconfig: kubeconfigFor(srv.URL)},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.Status != domain.ClusterHealthy || c.ServerVersion != "v1.37.2+k3s1" {
		t.Fatalf("cluster = %+v", c)
	}
	if _, ok := registry.Get("prod"); !ok {
		t.Fatal("cluster should be registered")
	}
	cred := store.creds[c.ID]
	if strings.Contains(string(cred.Ciphertext), "test-token") {
		t.Fatal("credential must be encrypted at rest")
	}
	if cred.Fingerprint == "" || cred.AuthType != "kubeconfig" {
		t.Fatalf("credential meta = %+v", cred)
	}

	// 重复接入同名集群返回冲突。
	_, err = mgr.Create(context.Background(), CreateRequest{
		Name: "prod", Credential: kube.IngestInput{Kubeconfig: kubeconfigFor(srv.URL)},
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate create err = %v, want ErrConflict", err)
	}
}

func TestManagerRejectsLowVersion(t *testing.T) {
	srv := fakeAPIServer(t, "v1.33.9")
	mgr := NewManager(kube.NewRegistry(), newFakeStore(), testRing(t), nil)
	_, err := mgr.Create(context.Background(), CreateRequest{
		Name: "old", Credential: kube.IngestInput{Kubeconfig: kubeconfigFor(srv.URL)},
	})
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("err = %v, want ErrNotSupported", err)
	}
}

func TestManagerRotateAndDelete(t *testing.T) {
	srv := fakeAPIServer(t, "v1.37.2")
	store := newFakeStore()
	registry := kube.NewRegistry()
	mgr := NewManager(registry, store, testRing(t), nil)
	if _, err := mgr.Create(context.Background(), CreateRequest{
		Name: "prod", Credential: kube.IngestInput{Kubeconfig: kubeconfigFor(srv.URL)},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.Rotate(context.Background(), "prod", kube.IngestInput{Kubeconfig: kubeconfigFor(srv.URL)}); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if got := len(store.creds); got != 1 {
		t.Fatalf("credentials = %d, want 1 (rotated in place)", got)
	}
	if err := mgr.Delete(context.Background(), "prod"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := registry.Get("prod"); ok {
		t.Fatal("cluster should be removed from registry")
	}
	if len(store.clusters) != 0 {
		t.Fatal("cluster record should be deleted")
	}
}
