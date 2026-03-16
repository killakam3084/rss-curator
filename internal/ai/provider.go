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

	// temperature: scorer needs deterministic output (temperature=0); all
	// other subsystems (enricher, suggester) use temperature=1 for variety.
	temperature := 1.0
	if subsystem == "scorer" {
		temperature = 0.0
	}

	// num_ctx: KV cache context window for Ollama. Curator prompts are small
	// (~400 tokens); 2048 is generous headroom without the cost of 128K default.
	numCtx := 2048
	if v := os.Getenv("CURATOR_AI_NUM_CTX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			numCtx = n
		}
	}

	// num_predict: max tokens to generate. Scorer output is ~80-100 tokens of
	// JSON; 400 is safe headroom. Enricher/suggester may need more.
	numPredict := 400
	if v := os.Getenv("CURATOR_AI_NUM_PREDICT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			numPredict = n
		}
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
