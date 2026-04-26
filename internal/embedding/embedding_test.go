package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"personal-assistant/internal/config"
)

func TestBigModelProviderEmbed(t *testing.T) {
	var gotReq bigModelEmbeddingRequest
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path = %q, want /embeddings", r.URL.Path)
		}
		gotAuth = r.Header.Get("authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeJSON(t, w, bigModelEmbeddingResponse{
			Model: gotReq.Model,
			Data: []bigModelEmbeddingData{{
				Index:     0,
				Embedding: []float32{3, 4, 0},
			}},
		})
	}))
	defer server.Close()

	provider := NewBigModelProvider(config.Config{
		EmbeddingModel:     "embedding-3",
		EmbeddingDimension: 3,
		BigModelAPIKey:     "test-key",
		BigModelBaseURL:    server.URL,
	})
	vec, err := provider.Embed(context.Background(), "hello", TaskQuery)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("authorization = %q, want bearer key", gotAuth)
	}
	if gotReq.Model != "embedding-3" || gotReq.Input != "hello" || gotReq.Dimensions != 3 {
		t.Fatalf("request = %#v, want embedding-3 hello dimensions 3", gotReq)
	}
	want := []float32{3, 4, 0}
	for i := range want {
		if vec[i] != want[i] {
			t.Fatalf("vec[%d] = %v, want %v; full=%v", i, vec[i], want[i], vec)
		}
	}
}

func TestBigModelProviderEmbedDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, bigModelEmbeddingResponse{
			Model: "embedding-3",
			Data:  []bigModelEmbeddingData{{Embedding: []float32{1, 2}}},
		})
	}))
	defer server.Close()

	provider := NewBigModelProvider(config.Config{
		EmbeddingModel:     "embedding-3",
		EmbeddingDimension: 3,
		BigModelAPIKey:     "test-key",
		BigModelBaseURL:    server.URL,
	})
	if _, err := provider.Embed(context.Background(), "hello", TaskDocument); err == nil {
		t.Fatal("Embed() error = nil, want dimension mismatch")
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
