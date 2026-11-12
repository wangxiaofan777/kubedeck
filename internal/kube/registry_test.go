package kube

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
