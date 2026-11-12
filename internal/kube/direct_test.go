package kube

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestListAllNamespacesFollowsContinue(t *testing.T) {
	pages := []*unstructured.UnstructuredList{
		{Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "default"}}},
			{Object: map[string]any{"metadata": map[string]any{"name": "kube-system"}}},
		}},
		{Items: []unstructured.Unstructured{
			{Object: map[string]any{"metadata": map[string]any{"name": "monitoring"}}},
		}},
	}
	pages[0].SetContinue("page-2")

	var calls int
	var seenContinue []string
	list := func(_ context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		seenContinue = append(seenContinue, opts.Continue)
		page := pages[calls]
		calls++
		return page, nil
	}

	names, err := listAllNamespaces(context.Background(), list)
	if err != nil {
		t.Fatalf("listAllNamespaces: %v", err)
	}
	if len(names) != 3 || names[0] != "default" || names[1] != "kube-system" || names[2] != "monitoring" {
		t.Fatalf("names = %v, want all three namespaces", names)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(seenContinue) != 2 || seenContinue[0] != "" || seenContinue[1] != "page-2" {
		t.Fatalf("continue cursors = %v, want ['', 'page-2']", seenContinue)
	}
}

func TestListAllNamespacesPropagatesError(t *testing.T) {
	want := errors.New("boom")
	list := func(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		return nil, want
	}
	if _, err := listAllNamespaces(context.Background(), list); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestListAllNamespacesStopsOnRepeatedCursor(t *testing.T) {
	// 服务端始终返回同一个 continue 游标时，防御性上限应阻止死循环。
	list := func(context.Context, metav1.ListOptions) (*unstructured.UnstructuredList, error) {
		page := &unstructured.UnstructuredList{}
		page.SetContinue("same")
		return page, nil
	}
	if _, err := listAllNamespaces(context.Background(), list); err == nil {
		t.Fatal("expected page limit error, got nil")
	}
}
