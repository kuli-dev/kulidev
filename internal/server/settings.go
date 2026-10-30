package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/llm"
	"github.com/kuli-dev/kulidev/internal/settings"
)

// Settings are changed only by the local user (the same rule as opening folders): a
// paired phone must not be able to point the model at a server of its choosing.

// LLMFromSettings builds the model client for saved settings (nil when none is set).
func LLMFromSettings(l settings.LLM) (docs.Chatter, error) {
	if l.BaseURL == "" || l.Model == "" {
		return nil, nil
	}
	maxTok := l.MaxTokens
	if maxTok == 0 {
		maxTok = 2048
	}
	c, err := llm.New(llm.Config{BaseURL: l.BaseURL, Model: l.Model, APIKey: l.APIKey,
		ReasoningEffort: l.ReasoningEffort, MaxTokens: maxTok, Temperature: 0.2, Timeout: 5 * time.Minute})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (h *Host) loadSettings() settings.Settings {
	s, _ := settings.Load(h.settingsFile)
	return s
}

func (h *Host) setLLM(c docs.Chatter) {
	h.mu.Lock()
	h.llm = c
	cur := h.cur
	h.mu.Unlock()
	if cur != nil {
		cur.SetLLM(c)
	}
}

type llmView struct {
	BaseURL         string `json:"baseURL"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort"`
	MaxTokens       int    `json:"maxTokens"`
	HasKey          bool   `json:"hasKey"` // the key itself is never sent back
}

func (h *Host) getSettings(w http.ResponseWriter, r *http.Request) {
	s := h.loadSettings()
	h.mu.RLock()
	active, readOnly, terms := h.llm, h.opts.ReadOnly, h.opts.Terminals
	h.mu.RUnlock()
	writeJSON(w, map[string]any{
		"llm":          llmView{s.LLM.BaseURL, s.LLM.Model, s.LLM.ReasoningEffort, s.LLM.MaxTokens, s.LLM.APIKey != ""},
		"activeModel":  modelName(active),
		"fromStartup":  h.llmFromStartup, // flags or KULI_LLM_* were given at start
		"settingsFile": h.settingsFile,
		"recentFile":   h.recent,
		"readOnly":     readOnly,
		"terminals":    terms,
		"version":      h.version,
	})
}

type llmInput struct {
	settings.LLM
	ClearKey bool `json:"clearKey"`
}

// merged applies an edit to the saved connection: an empty key keeps the saved one.
func (h *Host) merged(in llmInput) settings.LLM {
	cur := h.loadSettings().LLM
	l := in.LLM
	l.BaseURL, l.Model = strings.TrimSpace(l.BaseURL), strings.TrimSpace(l.Model)
	if l.APIKey == "" && !in.ClearKey {
		l.APIKey = cur.APIKey
	}
	return l
}

func readLLMInput(w http.ResponseWriter, r *http.Request) (llmInput, bool) {
	var req struct {
		LLM llmInput `json:"llm"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "bad request body")
		return req.LLM, false
	}
	return req.LLM, true
}

func (h *Host) putSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := readLLMInput(w, r)
	if !ok {
		return
	}
	l := h.merged(in)
	if err := l.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := LLMFromSettings(l)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	s := h.loadSettings()
	s.LLM = l
	if err := settings.Save(h.settingsFile, s); err != nil {
		fail(w, http.StatusInternalServerError, "could not save settings: "+err.Error())
		return
	}
	h.setLLM(c)
	writeJSON(w, map[string]any{"ok": true, "activeModel": modelName(c)})
}

// testSettings asks the model for a one-word reply, without saving anything.
func (h *Host) testSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := readLLMInput(w, r)
	if !ok {
		return
	}
	l := h.merged(in)
	if err := l.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if l.BaseURL == "" {
		fail(w, http.StatusBadRequest, "enter a base URL and a model first")
		return
	}
	l.MaxTokens = max(l.MaxTokens, 256)
	c, err := LLMFromSettings(l)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	cl, ok := c.(*llm.Client)
	if !ok {
		fail(w, http.StatusInternalServerError, "unexpected client")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	start := time.Now()
	reply, _, err := cl.Chat(ctx, []llm.Message{{Role: "user", Content: "Reply with the single word OK."}})
	if err != nil {
		msg := err.Error()
		if errors.Is(err, llm.ErrThinkingBudget) {
			msg += " (this is a reasoning model: set Reasoning effort to none)"
		}
		writeJSON(w, map[string]any{"ok": false, "error": msg, "ms": time.Since(start).Milliseconds()})
		return
	}
	if len(reply) > 200 {
		reply = reply[:200]
	}
	writeJSON(w, map[string]any{"ok": true, "reply": strings.TrimSpace(reply), "ms": time.Since(start).Milliseconds()})
}
