package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kubedeck/kubedeck/internal/auth"
)

func TestAuthMode_Dev(t *testing.T) {
	router := NewRouter(Options{Auth: fakeAuth{
		dev:      true,
		identity: auth.Identity{Subject: "dev", DisplayName: "Local Developer", Dev: true},
	}})
	rec := doRequest(t, router, http.MethodGet, "/auth/mode")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["mode"] != "dev" {
		t.Fatalf("mode = %v, want dev", body["mode"])
	}
	if body["devUser"] != "Local Developer" {
		t.Fatalf("devUser = %v", body["devUser"])
	}
}

func TestAuthMode_UnconfiguredWithoutService(t *testing.T) {
	rec := doRequest(t, NewRouter(Options{}), http.MethodGet, "/auth/mode")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["mode"] != "unconfigured" {
		t.Fatalf("mode = %v, want unconfigured", body["mode"])
	}
}

func TestDevLogin_OnlyInDevMode(t *testing.T) {
	devRouter := NewRouter(Options{Auth: fakeAuth{
		dev:      true,
		identity: auth.Identity{Subject: "dev", DisplayName: "Local Developer", Dev: true},
	}})
	rec := doRequest(t, devRouter, http.MethodPost, "/auth/dev-login")
	if rec.Code != http.StatusOK {
		t.Fatalf("dev login status = %d, want 200", rec.Code)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("dev login should set a session cookie")
	}

	oidcRouter := NewRouter(Options{Auth: fakeAuth{}})
	rec = doRequest(t, oidcRouter, http.MethodPost, "/auth/dev-login")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-dev dev-login status = %d, want 404", rec.Code)
	}
}

func TestDevModeDoesNotAutoLogin(t *testing.T) {
	// 开发模式也必须先显式登录；未带会话的 API 请求应返回 401。
	router := NewRouter(Options{Auth: fakeAuth{dev: true}})
	rec := doRequest(t, router, http.MethodGet, "/api/v1/auth/me")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
