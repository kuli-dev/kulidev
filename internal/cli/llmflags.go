package cli

import (
	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/llm"
)

// llmFlags are the model options shared by commands that call an LLM.
// The API key is read from the environment only, so it never lands in shell history.
type llmFlags struct {
	cfg       llm.Config
	maxTokens int
}

func (l *llmFlags) add(cmd *cobra.Command, defaultMaxTokens int) {
	fs := cmd.Flags()
	fs.StringVar(&l.cfg.BaseURL, "llm-url", "", "OpenAI-compatible base URL (default $KULI_LLM_BASE_URL)")
	fs.StringVar(&l.cfg.Model, "llm-model", "", "model name (default $KULI_LLM_MODEL)")
	fs.Float64Var(&l.cfg.Temperature, "temperature", 0.2, "sampling temperature")
	fs.IntVar(&l.maxTokens, "max-tokens", defaultMaxTokens, "max tokens per model reply (reasoning models count thinking here)")
	fs.StringVar(&l.cfg.ReasoningEffort, "reasoning-effort", "", "send reasoning_effort (e.g. none, low) for reasoning models (default $KULI_LLM_REASONING_EFFORT)")
}

func (l *llmFlags) client() (*llm.Client, error) {
	cfg := l.cfg
	cfg.MaxTokens = l.maxTokens
	return llm.New(cfg.FromEnv())
}
