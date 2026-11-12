package kube

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestValidateServer(t *testing.T) {
	tests := []struct {
		server string
		ok     bool
	}{
		{"https://10.0.0.1:6443", true},
		{"https://k8s.example.com", true},
		{"http://10.0.0.1:6443", false},
		{"https://169.254.169.254", false},
		{"https://169.254.1.1:6443", false},
		{"not-a-url", false},
	}
	for _, tc := range tests {
		err := ValidateServer(tc.server)
		if tc.ok && err != nil {
			t.Fatalf("ValidateServer(%q) = %v, want nil", tc.server, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("ValidateServer(%q) = nil, want error", tc.server)
		}
	}
}

func TestParseCredentialTokenRoundTrip(t *testing.T) {
	parsed, err := ParseCredential(IngestInput{
		Server: "https://10.0.0.1:6443",
		Token:  "secret-token",
		CAData: "not-a-cert",
	})
	if err == nil {
		t.Fatal("invalid CA should be rejected")
	}

	parsed, err = ParseCredential(IngestInput{Server: "https://10.0.0.1:6443", Token: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.AuthType != "token" || parsed.Config.BearerToken != "secret-token" {
		t.Fatalf("parsed = %+v", parsed)
	}
	var stored IngestInput
	if err := json.Unmarshal(parsed.Plaintext, &stored); err != nil {
		t.Fatalf("stored credential is not JSON: %v", err)
	}
	if stored.Token != "secret-token" {
		t.Fatalf("stored token = %q", stored.Token)
	}
	again, err := ParseStored("token", parsed.Plaintext)
	if err != nil || again.Config.BearerToken != "secret-token" {
		t.Fatalf("ParseStored: %+v err=%v", again, err)
	}
}

func TestParseKubeconfigRejectsExec(t *testing.T) {
	raw := `apiVersion: v1
kind: Config
current-context: c
clusters:
- name: c
  cluster:
    server: https://10.0.0.1:6443
contexts:
- name: c
  context: {cluster: c, user: u}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: /bin/sh
`
	_, err := ParseCredential(IngestInput{Kubeconfig: raw})
	if err == nil || !strings.Contains(err.Error(), "exec") {
		t.Fatalf("err = %v, want exec rejection", err)
	}
}

func TestCheckMinVersion(t *testing.T) {
	if err := CheckMinVersion("v1.37.2+k3s1", "1.34"); err != nil {
		t.Fatalf("valid version rejected: %v", err)
	}
	if err := CheckMinVersion("v1.33.9", "1.34"); err == nil {
		t.Fatal("old version should be rejected")
	}
	if err := CheckMinVersion("not-a-version", "1.34"); err == nil {
		t.Fatal("unparseable version should be rejected")
	}
}

func TestRecordProbeStateMachine(t *testing.T) {
	c := &DirectCluster{info: ClusterInfo{Status: StatusHealthy}}
	c.recordProbe("", "dial tcp: connection refused", false)
	c.recordProbe("", "dial tcp: connection refused", false)
	if c.Info(context.Background()).Status != StatusDegraded {
		t.Fatalf("status after 2 failures = %s, want degraded", c.Info(context.Background()).Status)
	}
	c.recordProbe("", "dial tcp: connection refused", false)
	if c.Info(context.Background()).Status != StatusUnreachable {
		t.Fatalf("status after 3 failures = %s, want unreachable", c.Info(context.Background()).Status)
	}
	c.recordProbe("v1.37.2", "", true)
	if info := c.Info(context.Background()); info.Status != StatusHealthy || info.Version != "v1.37.2" {
		t.Fatalf("recovered info = %+v", info)
	}
}

func TestClassifyProbeError(t *testing.T) {
	if status, _ := classifyProbeError(apierrors.NewUnauthorized("bad token")); status != StatusDegraded {
		t.Fatalf("401 status = %s, want degraded", status)
	}
	if status, _ := classifyProbeError(apierrors.NewForbidden(schema.GroupResource{Resource: "x"}, "x", errors.New("denied"))); status != StatusDegraded {
		t.Fatalf("403 status = %s, want degraded", status)
	}
	if status, _ := classifyProbeError(errors.New("dial tcp 10.0.0.1:6443: connect: connection refused")); status != StatusUnreachable {
		t.Fatalf("network status = %s, want unreachable", status)
	}
}
