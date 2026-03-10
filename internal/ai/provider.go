package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

// NewProvider constructs a Provider from environment variables using the
// shared model setting. Equivalent to NewProviderFor("").
//
//	CURATOR_AI_PROVIDER   "ollama" (default) | "openai" | "disabled"
//	CURATOR_AI_HOST       base URL, e.g. http://localhost:11434
//	CURATOR_AI_MODEL      model name, e.g. llama3.2
//	CURATOR_AI_KEY        API key (openai / compatible endpoints only)
func NewProvider() Provider {
	return NewProviderFor("")
}

// NewProviderFor constructs a Provider for a named subsystem. It resolves the
// model name by checking CURATOR_AI_{SUBSYSTEM}_MODEL first (e.g.
// CURATOR_AI_SCORER_MODEL), then falling back to CURATOR_AI_MODEL, then the
// provider's built-in default. All other settings (provider type, host, key)
// are shared across subsystems.
//
// Recognised subsystem names: "enricher", "scorer", "suggester".
// Pass "" to use only the global CURATOR_AI_MODEL fallback.
func NewProviderFor(subsystem string) Provider {
	providerType := os.Getenv("CURATOR_AI_PROVIDER")
	if providerType == "" {
		providerType = "ollama"
	}

	host := os.Getenv("CURATOR_AI_HOST")
	key := os.Getenv("CURATOR_AI_KEY")

	// Resolve model: subsystem-specific > global > provider default.
	model := ""
	if subsystem != "" {
		model = os.Getenv("CURATOR_AI_" + strings.ToUpper(subsystem) + "_MODEL")
	}
	if model == "" {
		model = os.Getenv("CURATOR_AI_MODEL")
	}

	switch providerType {
	case "openai":
		if host == "" {
			host = "https://api.openai.com"
		}
		if model == "" {
			model = "gpt-4o-mini"
		}
		return &openAIProvider{
			host:   host,
			model:  model,
			key:    key,
			client: &http.Client{}, // no transport timeout — context deadline is the sole authority
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
			host:   host,
			model:  model,
			client: &http.Client{}, // no transport timeout — context deadline is the sole authority
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
	host   string
	model  string
	client *http.Client
}

type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
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
	host   string
	model  string
	key    string
	client *http.Client
}

type openAIRequest struct {
	Model    string          `json:"model"`
	Messages []openAIMessage `json:"messages"`
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
