package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebIndexServed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	(&Server{}).webIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Personal Assistant") {
		t.Fatalf("body missing title: %s", rec.Body.String())
	}
}

func TestWebAssetServed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/web/app.js", nil)
	req.SetPathValue("file", "app.js")
	rec := httptest.NewRecorder()

	(&Server{}).webAsset(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "sendMessage") {
		t.Fatalf("body missing app script: %s", rec.Body.String())
	}
}
