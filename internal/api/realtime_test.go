package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/kubedeck/kubedeck/internal/auth"
	"github.com/kubedeck/kubedeck/internal/kube"
)

func streamTestServer(t *testing.T, watcher watch.Interface) (*httptest.Server, *fakeTransport) {
	t.Helper()
	transport := &fakeTransport{name: "prod", watcher: watcher, object: &unstructured.Unstructured{}}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{UserID: 7, Subject: "tester", PlatformAdmin: true}},
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv, transport
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame map[string]any
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read frame: %v", err)
	}
	return frame
}

func TestStreamSubscribeAndIncrementalEvent(t *testing.T) {
	watcher := watch.NewFake()
	srv, _ := streamTestServer(t, watcher)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(streamRequest{
		Type: "subscribe", ID: "s1", Version: "v1", Resource: "pods", Namespace: "default",
	}); err != nil {
		t.Fatal(err)
	}
	if frame := readFrame(t, conn); frame["type"] != "subscribed" || frame["id"] != "s1" {
		t.Fatalf("subscribed frame = %+v", frame)
	}

	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name": "web-1", "namespace": "default", "uid": "u1", "resourceVersion": "10",
		},
		"status": map[string]any{"phase": "Running"},
	}}
	watcher.Add(pod)

	start := time.Now()
	frame := readFrame(t, conn)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("event latency = %v, want <= 2s", elapsed)
	}
	if frame["type"] != "event" || frame["eventType"] != "ADDED" || frame["id"] != "s1" {
		t.Fatalf("event frame = %+v", frame)
	}
	obj, ok := frame["object"].(map[string]any)
	if !ok {
		t.Fatalf("object = %T", frame["object"])
	}
	meta := obj["metadata"].(map[string]any)
	if meta["name"] != "web-1" || meta["namespace"] != "default" {
		t.Fatalf("summary = %+v", obj)
	}
	if _, ok := obj["spec"]; ok {
		t.Fatal("summary must not include spec")
	}
}

func TestStreamSendsResyncWhenWatchCloses(t *testing.T) {
	watcher := watch.NewFake()
	srv, _ := streamTestServer(t, watcher)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(streamRequest{
		Type: "subscribe", ID: "s1", Version: "v1", Resource: "pods", Namespace: "default",
		ResourceVersion: "99",
	}); err != nil {
		t.Fatal(err)
	}
	_ = readFrame(t, conn) // subscribed
	watcher.Stop()

	frame := readFrame(t, conn)
	if frame["type"] != "resync" || frame["id"] != "s1" {
		t.Fatalf("resync frame = %+v", frame)
	}
}

func TestStreamRejectsCrossOrigin(t *testing.T) {
	watcher := watch.NewFake()
	srv, _ := streamTestServer(t, watcher)
	header := http.Header{"Origin": []string{"https://evil.example.com"}}
	if _, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), header); err == nil {
		t.Fatal("cross-origin websocket should be rejected")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want 403", resp)
	}
}

func TestStreamConnectionQuota(t *testing.T) {
	watcher := watch.NewFake()
	srv, _ := streamTestServer(t, watcher)
	var conns []*websocket.Conn
	for i := 0; i < maxConnsPerUser; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, conn)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
	if err == nil {
		t.Fatal("connection above quota should be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %v, want 429", resp)
	}
}

func TestStreamRejectsUnknownFrameType(t *testing.T) {
	watcher := watch.NewFake()
	srv, _ := streamTestServer(t, watcher)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	payload, _ := json.Marshal(map[string]string{"type": "bogus"})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatal(err)
	}
	frame := readFrame(t, conn)
	if frame["type"] != "error" || frame["code"] != "INVALID_ARGUMENT" {
		t.Fatalf("frame = %+v", frame)
	}
}

func TestStreamResyncOnExpiredResourceVersion(t *testing.T) {
	transport := &fakeTransport{name: "prod", watchErr: apierrors.NewResourceExpired("too old")}
	registry := &fakeRegistry{
		items: map[string]kube.ClusterAccess{"prod": &fakeCluster{t: transport}},
		order: []string{"prod"},
	}
	router := NewRouter(Options{
		Registry: registry,
		Auth:     fakeAuth{authenticated: true, identity: auth.Identity{UserID: 7, Subject: "tester", PlatformAdmin: true}},
	})
	srv := httptest.NewServer(router)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/api/v1/clusters/prod/stream"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(streamRequest{
		Type: "subscribe", ID: "s1", Version: "v1", Resource: "pods",
		Namespace: "default", ResourceVersion: "1",
	}); err != nil {
		t.Fatal(err)
	}
	frame := readFrame(t, conn)
	if frame["type"] != "resync" || frame["reason"] != "resource version expired" {
		t.Fatalf("frame = %+v", frame)
	}
}
