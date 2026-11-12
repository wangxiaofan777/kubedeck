package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/kubedeck/kubedeck/internal/domain"
)

type fakeUsers struct {
	calls int
}

func (f *fakeUsers) UpsertUser(ctx context.Context, u domain.User) (domain.User, error) {
	f.calls++
	return domain.User{
		ID: 7, Subject: u.Subject, Email: u.Email, DisplayName: u.DisplayName,
		Source: "oidc", Status: "active",
	}, nil
}
func (f *fakeUsers) SetUserStatus(context.Context, int64, string) error { return nil }

type fakeIDP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	nonce  string
	revoke bool
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key}
	mux := http.NewServeMux()
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	issuer := idp.server.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"userinfo_endpoint":                     issuer + "/userinfo",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: "test-key", Algorithm: "RS256", Use: "sig",
		}}}
		_ = json.NewEncoder(w).Encode(set)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		signer, err := jose.NewSigner(
			jose.SigningKey{Algorithm: jose.RS256, Key: key},
			(&jose.SignerOptions{}).WithHeader("kid", "test-key"),
		)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		claims := map[string]any{
			"iss":    issuer,
			"aud":    "kubedeck",
			"sub":    "user-1",
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []string{"dev"},
			"nonce":  idp.nonce,
			"iat":    time.Now().Unix(),
			"exp":    time.Now().Add(time.Hour).Unix(),
		}
		raw, err := jwt.Signed(signer).Claims(claims).Serialize()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-1",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     raw,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if idp.revoke || r.Header.Get("Authorization") != "Bearer access-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub": "user-1", "email": "alice@example.com", "groups": []string{"dev"},
		})
	})
	return idp
}

func TestOIDCLoginCreatesSession(t *testing.T) {
	idp := newFakeIDP(t)
	provider, err := NewProvider(t.Context(), OIDCConfig{
		Issuer:       idp.server.URL,
		ClientID:     "kubedeck",
		ClientSecret: "secret",
		RedirectURL:  "https://kubedeck.example.com/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	users := &fakeUsers{}
	sessions := NewMemorySessions(5, time.Hour)
	svc := NewService(Config{Provider: provider, Users: users, Sessions: sessions})

	authURL, state, err := svc.StartLogin("/clusters/prod")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	idp.nonce = u.Query().Get("nonce")
	if idp.nonce == "" {
		t.Fatal("auth URL must carry nonce")
	}
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("auth URL must use PKCE S256")
	}

	sess, returnTo, err := svc.FinishLogin(t.Context(), state, state, "code-1")
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if returnTo != "/clusters/prod" {
		t.Fatalf("returnTo = %q", returnTo)
	}
	if sess.Identity.Subject != "user-1" || sess.Identity.Email != "alice@example.com" {
		t.Fatalf("identity = %+v", sess.Identity)
	}
	if sess.Identity.KubeUser != "alice@example.com" {
		t.Fatalf("kube user = %q, want email", sess.Identity.KubeUser)
	}
	if got, ok := sessions.Get(sess.ID); !ok || got.Identity.UserID != 7 {
		t.Fatalf("session not stored: %+v ok=%v", got, ok)
	}
}

func TestOIDCRejectsStateMismatch(t *testing.T) {
	idp := newFakeIDP(t)
	provider, err := NewProvider(t.Context(), OIDCConfig{
		Issuer: idp.server.URL, ClientID: "kubedeck", RedirectURL: "https://example.com/cb",
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{Provider: provider, Users: &fakeUsers{}, Sessions: NewMemorySessions(5, time.Hour)})
	_, state, err := svc.StartLogin("/")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.FinishLogin(t.Context(), state, "other", "code"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

func TestRevalidateRevokesSessions(t *testing.T) {
	idp := newFakeIDP(t)
	provider, err := NewProvider(t.Context(), OIDCConfig{
		Issuer: idp.server.URL, ClientID: "kubedeck", RedirectURL: "https://example.com/cb",
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions := NewMemorySessions(5, time.Hour)
	svc := NewService(Config{Provider: provider, Users: &fakeUsers{}, Sessions: sessions})
	authURL, state, err := svc.StartLogin("/")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	idp.nonce = u.Query().Get("nonce")
	sess, _, err := svc.FinishLogin(t.Context(), state, state, "code-1")
	if err != nil {
		t.Fatal(err)
	}

	// 未超过校验间隔：不请求 IdP，会话保留。
	svc.RevalidateIdP(t.Context())
	if _, ok := sessions.Get(sess.ID); !ok {
		t.Fatal("session should remain before validation interval")
	}

	idp.revoke = true
	svc.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	svc.RevalidateIdP(t.Context())
	if _, ok := sessions.Get(sess.ID); ok {
		t.Fatal("revoked session should be deleted")
	}
}
