package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testWebFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>spa</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

func TestSPAHandler_ServesExistingFile(t *testing.T) {
	rec := httptest.NewRecorder()
	SPAHandler(testWebFS()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "console.log(1)" {
		t.Fatalf("body = %q", body)
	}
}

func TestSPAHandler_FallsBackToIndex(t *testing.T) {
	for _, p := range []string{"/", "/clusters/prod", "/workloads/deployments/web"} {
		rec := httptest.NewRecorder()
		SPAHandler(testWebFS()).ServeHTTP(rec, htmlRequest(http.MethodGet, p))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", p, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "spa") {
			t.Fatalf("%s body = %q, want index.html", p, rec.Body.String())
		}
	}
}

func TestSPAHandler_NonHTMLRequestDoesNotGetIndex(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/clusters/prod", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	SPAHandler(testWebFS()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", body.Code)
	}
}

func TestSPAHandler_NonGETReturns405(t *testing.T) {
	rec := httptest.NewRecorder()
	SPAHandler(testWebFS()).ServeHTTP(rec, htmlRequest(http.MethodPost, "/clusters/prod"))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", allow)
	}
}

func htmlRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	return req
}

func TestSPAHandler_APIUnknownReturnsJSON404(t *testing.T) {
	rec := httptest.NewRecorder()
	SPAHandler(testWebFS()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body Error
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", body.Code)
	}
}
