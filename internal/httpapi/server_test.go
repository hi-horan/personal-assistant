package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONAcceptsContentTypeParameters(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("content-type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()

	var body struct {
		Message string `json:"message"`
	}
	if !decodeJSON(rec, req, &body) {
		t.Fatalf("decodeJSON() returned false, status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body.Message != "hello" {
		t.Fatalf("Message = %q, want hello", body.Message)
	}
}

func TestDecodeJSONRejectsTrailingTokens(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(`{"message":"hello"} {}`))
	req.Header.Set("content-type", "application/json")
	rec := httptest.NewRecorder()

	var body struct {
		Message string `json:"message"`
	}
	if decodeJSON(rec, req, &body) {
		t.Fatal("decodeJSON() returned true, want false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
