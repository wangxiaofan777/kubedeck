package kube

import (
	"context"
	"io"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

type fakeTransport struct{ name string }

func (f *fakeTransport) Name() string { return f.name }
func (f *fakeTransport) Info(context.Context) ClusterInfo {
	return ClusterInfo{Name: f.name, Status: StatusHealthy}
}
func (f *fakeTransport) Namespaces(context.Context) ([]string, error) { return nil, nil }
func (f *fakeTransport) List(context.Context, schema.GroupVersionResource, string, ListOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}
func (f *fakeTransport) Get(context.Context, schema.GroupVersionResource, string, string) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}
func (f *fakeTransport) ReviewAccess(context.Context, AccessRequest) (AccessResult, error) {
	return AccessResult{Allowed: true}, nil
}
func (f *fakeTransport) TransportFor(UserRef) (Transport, error) { return f, nil }
func (f *fakeTransport) Discovery(context.Context) (DiscoveryResult, error) {
	return DiscoveryResult{}, nil
}
func (f *fakeTransport) Create(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}
func (f *fakeTransport) Update(context.Context, schema.GroupVersionResource, string, *unstructured.Unstructured, []string) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}
func (f *fakeTransport) Delete(context.Context, schema.GroupVersionResource, string, string, DeleteOptions) error {
	return nil
}

func (f *fakeTransport) Scale(context.Context, schema.GroupVersionResource, string, string, int32) (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{}, nil
}
func (f *fakeTransport) Evict(context.Context, string, string, *int64) error {
	return nil
}

func (f *fakeTransport) Logs(context.Context, string, string, LogOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeTransport) Watch(context.Context, schema.GroupVersionResource, string, WatchOptions) (watch.Interface, error) {
	return watch.NewFake(), nil
}

func TestRegistryAddGetOrder(t *testing.T) {
	r := NewRegistry()
	r.Add(&fakeTransport{name: "b"})
	r.Add(&fakeTransport{name: "a"})
	r.Add(&fakeTransport{name: "c"})

	if got := r.Names(); len(got) != 3 || got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Fatalf("Names = %v, want registration order [b a c]", got)
	}
	if _, ok := r.Get("a"); !ok {
		t.Fatal("Get(a) should find the transport")
	}
	if r.Len() != 3 {
		t.Fatalf("Len = %d, want 3", r.Len())
	}
}

func TestRegistryOverwriteKeepsOrder(t *testing.T) {
	r := NewRegistry()
	r.Add(&fakeTransport{name: "a"})
	r.Add(&fakeTransport{name: "b"})
	r.Add(&fakeTransport{name: "a"})

	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	if got := r.Names(); got[0] != "a" || got[1] != "b" {
		t.Fatalf("Names = %v, want [a b]", got)
	}
}

func TestRegistryRemove(t *testing.T) {
	r := NewRegistry()
	r.Add(&fakeTransport{name: "a"})
	r.Add(&fakeTransport{name: "b"})

	if !r.Remove("a") {
		t.Fatal("Remove(a) = false, want true")
	}
	if r.Remove("a") {
		t.Fatal("Remove(a) twice = true, want false")
	}
	if got := r.Names(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Names after remove = %v, want [b]", got)
	}
	if _, ok := r.Get("a"); ok {
		t.Fatal("Get(a) after remove should be false")
	}
}

func TestRegistryAllSkipsRemoved(t *testing.T) {
	r := NewRegistry()
	r.Add(&fakeTransport{name: "a"})
	r.Add(&fakeTransport{name: "b"})
	r.Remove("a")

	all := r.All()
	if len(all) != 1 || all[0].Name() != "b" {
		t.Fatalf("All = %v, want only b", all)
	}
}
