// Package llm talks to any OpenAI-compatible chat completions endpoint
// (vLLM, Ollama, LM Studio, llama.cpp, OpenRouter, OpenAI, ...).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Config selects the endpoint and model. Zero values fall back to the environment.
type Config struct {
	BaseURL     string // e.g. http://localhost:11434/v1
	APIKey      string
	Model       string
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
	// ReasoningEffort is sent as reasoning_effort when set (e.g. "none", "low").
	// Reasoning models can otherwise spend the whole token budget thinking.
	ReasoningEffort string
}

// FromEnv fills unset fields from KULI_LLM_* and then OPENAI_* variables.
func (c Config) FromEnv() Config {
	pick := func(cur string, keys ...string) string {
		if cur != "" {
			return cur
		}
		for _, k := range keys {
			if v := os.Getenv(k); v != "" {
				return v
			}
		}
		return ""
	}
	c.BaseURL = pick(c.BaseURL, "KULI_LLM_BASE_URL", "OPENAI_BASE_URL")
	c.APIKey = pick(c.APIKey, "KULI_LLM_API_KEY", "OPENAI_API_KEY")
	c.Model = pick(c.Model, "KULI_LLM_MODEL", "OPENAI_MODEL")
	c.ReasoningEffort = pick(c.ReasoningEffort, "KULI_LLM_REASONING_EFFORT")
	if c.MaxTokens == 0 {
		c.MaxTokens = 2048
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Minute
	}
	return c
}

// Validate reports what is missing to make a call.
func (c Config) Validate() error {
	var missing []string
	if c.BaseURL == "" {
		missing = append(missing, "base URL (--llm-url or KULI_LLM_BASE_URL)")
	}
	if c.Model == "" {
		missing = append(missing, "model (--llm-model or KULI_LLM_MODEL)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("LLM not configured: missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client is a minimal chat completions client.
type Client struct {
	cfg  Config
	http *http.Client
}

// New returns a client for a validated config.
func New(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}, nil
}

// Model returns the configured model name.
func (c *Client) Model() string { return c.cfg.Model }

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Reasoning   string    `json:"reasoning_effort,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ErrThinkingBudget means the model used its whole token budget before writing an answer.
var ErrThinkingBudget = errors.New("model ran out of tokens while reasoning: raise --max-tokens or set --reasoning-effort none")

// ErrEmptyReply means the model answered with no content.
var ErrEmptyReply = errors.New("model returned an empty reply")

// Usage counts tokens across calls.
type Usage struct{ Prompt, Completion int }

// Chat sends messages and returns the assistant's reply. It retries rate limits and
// server errors with backoff.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, Usage, error) {
	body, _ := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: msgs, Temperature: c.cfg.Temperature,
		MaxTokens: c.cfg.MaxTokens, Reasoning: c.cfg.ReasoningEffort})
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", Usage{}, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", Usage{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		if c.cfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", Usage{}, ctx.Err()
			}
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("LLM endpoint returned %s: %s", resp.Status, snippet(data))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return "", Usage{}, fmt.Errorf("LLM endpoint returned %s: %s", resp.Status, snippet(data))
		}
		var cr chatResponse
		if err := json.Unmarshal(data, &cr); err != nil {
			return "", Usage{}, fmt.Errorf("decode LLM response: %w", err)
		}
		if len(cr.Choices) == 0 {
			return "", Usage{}, errors.New("LLM response has no choices")
		}
		u := Usage{cr.Usage.PromptTokens, cr.Usage.CompletionTokens}
		out := stripThinking(cr.Choices[0].Message.Content)
		if out == "" {
			if cr.Choices[0].FinishReason == "length" {
				return "", u, ErrThinkingBudget
			}
			return "", u, ErrEmptyReply
		}
		return out, u, nil
	}
	return "", Usage{}, lastErr
}

// stripThinking removes <think>…</think> blocks some reasoning models emit inline.
func stripThinking(s string) string {
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "</think>")
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+j+len("</think>"):]
	}
	return strings.TrimSpace(s)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
