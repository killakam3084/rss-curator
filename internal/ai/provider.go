package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Provider abstracts an LLM backend so the rest of the codebase is not
// coupled to any specific inference service.
type Provider interface {
	// Complete sends a system + user prompt and returns the model's response.
	Complete(ctx context.Context, system, user string) (string, error)
	// Available does a lightweight ping to check reachability.
	Available() bool
}

// FormatSetter is implemented by providers that support structured output
// schemas (Ollama structured outputs). Callers can type-assert a Provider to
// FormatSetter and call SetFormat with a JSON Schema object to constrain the
// model to a specific response shape.
type FormatSetter interface {
	SetFormat(schema json.RawMessage)
}

// NewProvider constructs a Provider from environment variables using the
// shared model setting. Equivalent to NewProviderFor("").
//
//	CURATOR_AI_PROVIDER   "ollama" (default) | "openai" | "anthropic" | "disabled"
//	CURATOR_AI_HOST       base URL, e.g. http://localhost:11434
//	CURATOR_AI_MODEL      model name, e.g. llama3.2
//	CURATOR_AI_KEY        API key (openai / anthropic / compatible endpoints only)
func NewProvider() Provider {
	return NewProviderFor("")
}

// NewProviderFor constructs a Provider for a named subsystem. Resolution order
// for each setting is subsystem-specific > global > provider default:
//
//	CURATOR_AI_{SUBSYSTEM}_PROVIDER  e.g. CURATOR_AI_SUGGESTER_PROVIDER=anthropic
//	CURATOR_AI_PROVIDER              global fallback (default: ollama)
//
//	CURATOR_AI_{SUBSYSTEM}_MODEL     e.g. CURATOR_AI_SCORER_MODEL=llama3.2
//	CURATOR_AI_MODEL                 global model fallback
//
//	CURATOR_AI_{SUBSYSTEM}_KEY       e.g. CURATOR_AI_SUGGESTER_KEY=sk-ant-...
//	CURATOR_AI_KEY                   global key fallback
//
// Recognised subsystem names: "enricher", "scorer", "suggester".
// Pass "" to use only the global settings.
func NewProviderFor(subsystem string) Provider {
	// Resolve provider type: subsystem-specific > global > "ollama".
	providerType := ""
	if subsystem != "" {
		providerType = os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_PROVIDER")
	}
	if providerType == "" {
		providerType = os.Getenv("CURATOR_AI_PROVIDER")
	}
	if providerType == "" {
		providerType = "ollama"
	}

	host := os.Getenv("CURATOR_AI_HOST")

	// Resolve API key: subsystem-specific > global.
	key := ""
	if subsystem != "" {
		key = os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_KEY")
	}
	if key == "" {
		key = os.Getenv("CURATOR_AI_KEY")
	}

	// Resolve model: subsystem-specific > global > provider default.
	model := ""
	if subsystem != "" {
		model = os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_MODEL")
	}
	if model == "" {
		model = os.Getenv("CURATOR_AI_MODEL")
	}

	// temperature: scorer needs deterministic output (temperature=0); all
	// other subsystems (enricher, suggester) use temperature=1 for variety.
	temperature := 1.0
	if subsystem == "scorer" {
		temperature = 0.0
	}

	// num_ctx: KV cache context window for Ollama. Scorer/enricher prompts are
	// small (~400 tokens); 2048 is generous headroom. Suggester prompts are
	// much larger (full watchlist + metadata + history), so
	// CURATOR_AI_{SUBSYSTEM}_NUM_CTX lets callers override per-subsystem
	// without inflating the KV cache for all other operations.
	numCtx := 2048
	if subsystem != "" {
		if v := os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_NUM_CTX"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				numCtx = n
			}
		}
	}
	if numCtx == 2048 {
		if v := os.Getenv("CURATOR_AI_NUM_CTX"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				numCtx = n
			}
		}
	}

	// num_predict: max tokens to generate. Same subsystem-specific > global
	// resolution as num_ctx.
	numPredict := 400
	if subsystem != "" {
		if v := os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_NUM_PREDICT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				numPredict = n
			}
		}
	}
	if numPredict == 400 {
		if v := os.Getenv("CURATOR_AI_NUM_PREDICT"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				numPredict = n
			}
		}
	}

	switch providerType {
	case "anthropic":
		if host == "" {
			host = "https://api.anthropic.com"
		}
		if model == "" {
			model = "claude-haiku-3-5"
		}
		return &anthropicProvider{
			host:        host,
			model:       model,
			key:         key,
			client:      &http.Client{},
			temperature: temperature,
		}
	case "openai":
		if host == "" {
			host = "https://api.openai.com"
		}
		if model == "" {
			model = "gpt-4o-mini"
		}
		return &openAIProvider{
			host:        host,
			model:       model,
			key:         key,
			client:      &http.Client{},
			temperature: temperature,
		}
	case "disabled":
		return &noopProvider{}
	default: // "ollama"
		if host == "" {
			host = "http://localhost:11434"
		}
		if model == "" {
			model = "llama3.2"
		}
		return &ollamaProvider{
			host:        host,
			model:       model,
			client:      &http.Client{},
			temperature: temperature,
			numCtx:      numCtx,
			numPredict:  numPredict,
		}
	}
}

// ─── Noop (disabled) ──────────────────────────────────────────────────────────

type noopProvider struct{}

func (n *noopProvider) Complete(_ context.Context, _, _ string) (string, error) {
	return "", fmt.Errorf("AI provider disabled")
}

func (n *noopProvider) Available() bool { return false }

// ─── Ollama ───────────────────────────────────────────────────────────────────

type ollamaProvider struct {
	host        string
	model       string
	client      *http.Client
	temperature float64
	numCtx      int
	numPredict  int
	format      json.RawMessage // nil = omit; set via SetFormat for structured output
}

// SetFormat implements FormatSetter. Pass a JSON Schema object to enable
// Ollama structured outputs, which pins the response to that exact shape.
func (o *ollamaProvider) SetFormat(schema json.RawMessage) { o.format = schema }

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  ollamaOptions   `json:"options"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature"`
	NumCtx      int     `json:"num_ctx"`
	NumPredict  int     `json:"num_predict"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
	Error   string        `json:"error,omitempty"`
}

func (o *ollamaProvider) Complete(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(ollamaRequest{
		Model: o.model,
		Messages: []ollamaMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: false,
		Format: o.format,
		Options: ollamaOptions{
			Temperature: o.temperature,
			NumCtx:      o.numCtx,
			NumPredict:  o.numPredict,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ollama: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ollama: decode response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama: %s", result.Error)
	}
	return result.Message.Content, nil
}

func (o *ollamaProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, o.host+"/api/tags", nil)
	resp, err := o.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ─── OpenAI-compatible ────────────────────────────────────────────────────────

type openAIProvider struct {
	host        string
	model       string
	key         string
	client      *http.Client
	temperature float64
}

type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (o *openAIProvider) Complete(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(openAIRequest{
		Model: o.model,
		Messages: []openAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: o.temperature,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.host+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("openai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.key != "" {
		req.Header.Set("Authorization", "Bearer "+o.key)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("openai: decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return result.Choices[0].Message.Content, nil
}

func (o *openAIProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, o.host+"/v1/models", nil)
	if o.key != "" {
		req.Header.Set("Authorization", "Bearer "+o.key)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ─── Anthropic ────────────────────────────────────────────────────────────────

type anthropicProvider struct {
	host        string
	model       string
	key         string
	client      *http.Client
	temperature float64
}

// anthropicRequest matches the Anthropic Messages API v1 request shape.
// https://docs.anthropic.com/en/api/messages
type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      string             `json:"system"`
	Messages    []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

const anthropicAPIVersion = "2023-06-01"

func (a *anthropicProvider) Complete(ctx context.Context, system, user string) (string, error) {
	body, _ := json.Marshal(anthropicRequest{
		Model:       a.model,
		MaxTokens:   2048,
		Temperature: a.temperature,
		System:      system,
		Messages: []anthropicMessage{
			{Role: "user", Content: user},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.host+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("anthropic: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("anthropic: decode response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("anthropic: %s: %s", result.Error.Type, result.Error.Message)
	}
	for _, block := range result.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("anthropic: empty response")
}

func (a *anthropicProvider) Available() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Anthropic has no unauthenticated ping endpoint; send a minimal
	// completion to verify the key and reachability in one round-trip.
	body, _ := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: 1,
		System:    "ping",
		Messages:  []anthropicMessage{{Role: "user", Content: "ping"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.host+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.key)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	resp, err := a.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// 200 = key valid and reachable; 400 = bad request but API is up (still available).
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest
}
