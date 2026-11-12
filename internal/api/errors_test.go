package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestFromUpstream_Mapping(t *testing.T) {
	gr := schema.GroupResource{Group: "", Resource: "pods"}

	cases := []struct {
		name     string
		err      error
		wantCode ErrorCode
		wantHTTP int
	}{
		{"forbidden", apierrors.NewForbidden(gr, "web", errors.New("denied")), CodeForbidden, http.StatusForbidden},
		{"not found", apierrors.NewNotFound(gr, "web"), CodeNotFound, http.StatusNotFound},
		{"conflict", apierrors.NewConflict(gr, "web", errors.New("rv mismatch")), CodeConflict, http.StatusConflict},
		{"invalid", apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "web", nil), CodeUnprocessable, http.StatusUnprocessableEntity},
		{"bad request", apierrors.NewBadRequest("malformed"), CodeInvalidArgument, http.StatusBadRequest},
		{"too many requests", apierrors.NewTooManyRequests("slow down", 1), CodeRateLimited, http.StatusTooManyRequests},
		{"timeout", apierrors.NewTimeoutError("timed out", 1), CodeUpstreamTimeout, http.StatusGatewayTimeout},
		{"server timeout", apierrors.NewServerTimeout(gr, "list", 1), CodeUpstreamTimeout, http.StatusGatewayTimeout},
		{"service unavailable", apierrors.NewServiceUnavailable("down"), CodeClusterUnreachable, http.StatusServiceUnavailable},
		{"unauthorized", apierrors.NewUnauthorized("no token"), CodeUnauthenticated, http.StatusUnauthorized},
		{"network unreachable", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}, CodeClusterUnreachable, http.StatusServiceUnavailable},
		{"unknown error", errors.New("boom"), CodeInternal, http.StatusInternalServerError},
		{"nil", nil, CodeInternal, http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromUpstream(tc.err)
			if got.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s", got.Code, tc.wantCode)
			}
			if s := got.Code.HTTPStatus(); s != tc.wantHTTP {
				t.Fatalf("http = %d, want %d", s, tc.wantHTTP)
			}
			if tc.err != nil {
				if got.Details["upstream"] == "" {
					t.Fatal("details.upstream is empty, original message must be preserved")
				}
			}
		})
	}
}

func TestFromUpstream_PreservesReason(t *testing.T) {
	gr := schema.GroupResource{Resource: "pods"}
	e := FromUpstream(apierrors.NewForbidden(gr, "web", errors.New("denied")))
	if got := e.Details["upstreamReason"]; got != "Forbidden" {
		t.Fatalf("upstreamReason = %v, want Forbidden", got)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx := WithRequestID(t.Context(), "req-123")

	WriteError(ctx, rec, NewError(CodeForbidden, "nope"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}

	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != CodeForbidden || body.RequestID != "req-123" {
		t.Fatalf("body = %+v", body)
	}
}

func TestErrorCode_HTTPStatus_Unknown(t *testing.T) {
	if got := ErrorCode("NO_SUCH_CODE").HTTPStatus(); got != http.StatusInternalServerError {
		t.Fatalf("unknown code status = %d, want 500", got)
	}
	if got := ErrorCode("").HTTPStatus(); got != http.StatusInternalServerError {
		t.Fatalf("empty code status = %d, want 500", got)
	}
}

func TestHTTPStatus_AllCodesCovered(t *testing.T) {
	all := []ErrorCode{
		CodeUnauthenticated, CodeForbidden, CodeNotFound, CodeConflict,
		CodeInvalidArgument, CodeUnprocessable, CodeClusterUnreachable,
		CodeClusterDegraded, CodeUpstreamTimeout, CodeRateLimited,
		CodePayloadTooLarge, CodeUnsupported, CodeInternal,
	}
	seen := map[int]bool{}
	for _, c := range all {
		s := c.HTTPStatus()
		if s == http.StatusInternalServerError && c != CodeInternal {
			t.Fatalf("code %s fell through to 500", c)
		}
		seen[s] = true
	}
	if len(seen) < len(all)-2 { // 400 与 500 分别被两个码共享
		t.Fatalf("unexpected status collapse: %v", seen)
	}
}
