package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestChatRetriesAndStripsThinking(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth = %q", got)
		}
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "m" || len(req.Messages) != 1 {
			t.Errorf("request = %+v", req)
		}
		if calls.Add(1) == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"<think>hmm</think>\nhello"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
	defer srv.Close()

	c, err := New(Config{BaseURL: srv.URL + "/v1/", APIKey: "k", Model: "m"}.FromEnv())
	if err != nil {
		t.Fatal(err)
	}
	out, u, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" || u.Prompt != 3 || calls.Load() != 2 {
		t.Errorf("out=%q usage=%+v calls=%d", out, u, calls.Load())
	}
}

func TestValidate(t *testing.T) {
	t.Setenv("KULI_LLM_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("KULI_LLM_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")
	if _, err := New(Config{}.FromEnv()); err == nil {
		t.Error("expected missing-config error")
	}
	t.Setenv("KULI_LLM_BASE_URL", "http://x/v1")
	t.Setenv("KULI_LLM_MODEL", "qwen")
	if _, err := New(Config{}.FromEnv()); err != nil {
		t.Error(err)
	}
}

func TestThinkingBudgetExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Reasoning != "none" {
			t.Errorf("reasoning_effort = %q, want none", req.Reasoning)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":""},"finish_reason":"length"}]}`))
	}))
	defer srv.Close()
	c, _ := New(Config{BaseURL: srv.URL, Model: "m", ReasoningEffort: "none"}.FromEnv())
	if _, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "x"}}); err != ErrThinkingBudget {
		t.Errorf("err = %v, want ErrThinkingBudget", err)
	}
}
