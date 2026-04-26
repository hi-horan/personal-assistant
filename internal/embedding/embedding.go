package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"personal-assistant/internal/config"

	"google.golang.org/genai"
)

const (
	TaskDocument = "RETRIEVAL_DOCUMENT"
	TaskQuery    = "RETRIEVAL_QUERY"
)

type Provider interface {
	Embed(ctx context.Context, text, taskType string) ([]float32, error)
	Dimension() int
	Name() string
}

func New(ctx context.Context, cfg config.Config) (Provider, error) {
	switch cfg.EmbeddingProvider {
	case "hash":
		return NewHashProvider(cfg.EmbeddingDimension), nil
	case "gemini":
		return NewGeminiProvider(ctx, cfg)
	case "bigmodel":
		return NewBigModelProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported embedding_provider %q", cfg.EmbeddingProvider)
	}
}

type HashProvider struct {
	dimension int
}

func NewHashProvider(dimension int) *HashProvider {
	if dimension <= 0 {
		dimension = config.DefaultEmbeddingDimension
	}
	return &HashProvider{dimension: dimension}
}

func (p *HashProvider) Name() string {
	return "hash"
}

func (p *HashProvider) Dimension() int {
	return p.dimension
}

func (p *HashProvider) Embed(_ context.Context, text, _ string) ([]float32, error) {
	vec := make([]float32, p.dimension)
	for _, token := range strings.Fields(strings.ToLower(text)) {
		sum := sha256.Sum256([]byte(token))
		idx := int(binary.BigEndian.Uint64(sum[:8]) % uint64(p.dimension))
		sign := float32(1)
		if sum[8]&1 == 1 {
			sign = -1
		}
		vec[idx] += sign
	}
	return vec, nil
}

type GeminiProvider struct {
	client    *genai.Client
	model     string
	dimension int
}

func NewGeminiProvider(ctx context.Context, cfg config.Config) (*GeminiProvider, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  cfg.GeminiAPIKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create gemini embedding client: %w", err)
	}
	return &GeminiProvider{
		client:    client,
		model:     cfg.EmbeddingModel,
		dimension: cfg.EmbeddingDimension,
	}, nil
}

func (p *GeminiProvider) Name() string {
	return "gemini"
}

func (p *GeminiProvider) Dimension() int {
	return p.dimension
}

func (p *GeminiProvider) Embed(ctx context.Context, text, taskType string) ([]float32, error) {
	dimension := int32(p.dimension)
	resp, err := p.client.Models.EmbedContent(ctx, p.model, genai.Text(text), &genai.EmbedContentConfig{
		TaskType:             taskType,
		OutputDimensionality: &dimension,
	})
	if err != nil {
		return nil, fmt.Errorf("embed content: %w", err)
	}
	if len(resp.Embeddings) == 0 || resp.Embeddings[0] == nil || len(resp.Embeddings[0].Values) == 0 {
		return nil, fmt.Errorf("embed content returned no values")
	}
	vec := append([]float32(nil), resp.Embeddings[0].Values...)
	if len(vec) != p.dimension {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(vec), p.dimension)
	}
	return vec, nil
}

type BigModelProvider struct {
	client    *http.Client
	model     string
	apiKey    string
	baseURL   string
	dimension int
}

func NewBigModelProvider(cfg config.Config) *BigModelProvider {
	return &BigModelProvider{
		client:    &http.Client{Timeout: 60 * time.Second},
		model:     cfg.EmbeddingModel,
		apiKey:    cfg.BigModelAPIKey,
		baseURL:   strings.TrimRight(cfg.BigModelBaseURL, "/"),
		dimension: cfg.EmbeddingDimension,
	}
}

func (p *BigModelProvider) Name() string {
	return "bigmodel"
}

func (p *BigModelProvider) Dimension() int {
	return p.dimension
}

func (p *BigModelProvider) Embed(ctx context.Context, text, _ string) ([]float32, error) {
	payload := bigModelEmbeddingRequest{
		Model:      p.model,
		Input:      text,
		Dimensions: p.dimension,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal bigmodel embedding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create bigmodel embedding request: %w", err)
	}
	req.Header.Set("authorization", "Bearer "+p.apiKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call bigmodel embeddings: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read bigmodel embedding response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bigmodel embeddings failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded bigModelEmbeddingResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode bigmodel embedding response: %w", err)
	}
	if len(decoded.Data) == 0 || len(decoded.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("bigmodel embedding response returned no values")
	}
	vec := append([]float32(nil), decoded.Data[0].Embedding...)
	if len(vec) != p.dimension {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(vec), p.dimension)
	}
	return vec, nil
}

type bigModelEmbeddingRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Dimensions int    `json:"dimensions"`
}

type bigModelEmbeddingResponse struct {
	Model string                  `json:"model"`
	Data  []bigModelEmbeddingData `json:"data"`
}

type bigModelEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}
