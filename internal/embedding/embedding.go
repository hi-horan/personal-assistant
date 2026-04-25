package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

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
	normalize(vec)
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
	normalize(vec)
	return vec, nil
}

func normalize(vec []float32) {
	var sum float64
	for _, value := range vec {
		sum += float64(value * value)
	}
	if sum == 0 {
		return
	}
	scale := float32(1 / math.Sqrt(sum))
	for i := range vec {
		vec[i] *= scale
	}
}
