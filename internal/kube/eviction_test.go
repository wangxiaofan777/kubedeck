package kube

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakediscovery "k8s.io/client-go/discovery/fake"
	clienttesting "k8s.io/client-go/testing"
)

func newFakeTransport(groupVersions ...string) (*DirectTransport, *fakediscovery.FakeDiscovery) {
	disc := &fakediscovery.FakeDiscovery{Fake: &clienttesting.Fake{}}
	for _, gv := range groupVersions {
		disc.Resources = append(disc.Resources, &metav1.APIResourceList{GroupVersion: gv})
	}
	return &DirectTransport{discovery: disc}, disc
}

func TestEvictionAPIVersionSelectsModern(t *testing.T) {
	tr, _ := newFakeTransport("policy/v1", "policy/v1beta1")
	got, err := tr.evictionAPIVersion(context.Background())
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if got != "policy/v1" {
		t.Fatalf("api version = %q, want policy/v1", got)
	}
}

func TestEvictionAPIVersionFallsBackToLegacy(t *testing.T) {
	tr, disc := newFakeTransport("policy/v1beta1")
	got, err := tr.evictionAPIVersion(context.Background())
	if err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if got != "policy/v1beta1" {
		t.Fatalf("api version = %q, want policy/v1beta1", got)
	}

	// 结果在 TTL 内复用，不会因发现结果变化立刻切换。
	disc.Resources = append(disc.Resources, &metav1.APIResourceList{GroupVersion: "policy/v1"})
	got, err = tr.evictionAPIVersion(context.Background())
	if err != nil {
		t.Fatalf("negotiate cached: %v", err)
	}
	if got != "policy/v1beta1" {
		t.Fatalf("cached api version = %q, want policy/v1beta1", got)
	}

	// 缓存过期后按集群当前能力重新协商。
	tr.evictCheckedAt = tr.evictCheckedAt.Add(-2 * evictionVersionTTL)
	got, err = tr.evictionAPIVersion(context.Background())
	if err != nil {
		t.Fatalf("negotiate refreshed: %v", err)
	}
	if got != "policy/v1" {
		t.Fatalf("refreshed api version = %q, want policy/v1", got)
	}
}

func TestEvictionAPIVersionDiscoveryError(t *testing.T) {
	tr, disc := newFakeTransport()
	disc.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	if _, err := tr.evictionAPIVersion(context.Background()); err == nil {
		t.Fatal("discovery failure should surface, not guess an api version")
	}
	if tr.evictAPIVersion != "" {
		t.Fatalf("failed negotiation must not be cached, got %q", tr.evictAPIVersion)
	}
}
