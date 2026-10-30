package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/llm"
)

func newDocsCmd(o *options) *cobra.Command {
	var (
		noLLM     bool
		only      []string
		cfg       llm.Config
		maxTokens int
	)
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "DocsMana: generate evidence-backed handover docs (docs-nya mana? ini.)",
		Long: `Generates project overview, architecture, dependency map, infrastructure and
getting-started documents into <data-dir>/knowledge. Every claim links to file:line.

Works with any OpenAI-compatible endpoint (vLLM, Ollama, LM Studio, llama.cpp,
OpenRouter, OpenAI, ...):

  export KULI_LLM_BASE_URL=http://localhost:11434/v1   # Ollama
  export KULI_LLM_MODEL=qwen3:8b
  export KULI_LLM_API_KEY=...                          # if the endpoint needs one
  export KULI_LLM_REASONING_EFFORT=none                # reasoning models: skip long thinking

Without an LLM it writes facts-only documents. Secrets in config files are redacted
before anything is sent to the model.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, id := range only {
				if !contains(docs.DocIDs, id) {
					return fmt.Errorf("unknown document %q (choose from %s)", id, strings.Join(docs.DocIDs, ", "))
				}
			}
			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			snap, err := st.LoadSnapshot()
			if err != nil {
				return err
			}
			dir, err := o.resolvedDataDir()
			if err != nil {
				return err
			}

			opts := docs.Options{OutDir: filepath.Join(dir, "knowledge"), Only: only, Version: Version, Log: os.Stderr}
			if !noLLM {
				cfg.MaxTokens = maxTokens
				c, err := llm.New(cfg.FromEnv())
				if err != nil {
					fmt.Fprintf(os.Stderr, "kuli: %v\nkuli: writing facts-only docs (use --no-llm to silence this)\n", err)
				} else {
					opts.LLM = c
					fmt.Fprintf(os.Stderr, "kuli: using model %s\n", c.Model())
				}
			}
			m, err := docs.Generate(cmd.Context(), snap, opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "\nDocs-nya mana? Ini → %s\n\n", relTo(snap.Run.Root, opts.OutDir))
			for _, d := range m.Docs {
				grounding := ""
				if strings.HasPrefix(d.Mode, "llm") {
					grounding = fmt.Sprintf("  %3.0f%% of claims cited", d.Coverage*100)
					if d.Removed > 0 {
						grounding += fmt.Sprintf(", %d invalid citations removed", d.Removed)
					}
				}
				fmt.Fprintf(out, "  %-22s %-13s %3d citations%s\n", d.File, d.Mode, d.Valid, grounding)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&noLLM, "no-llm", false, "write facts-only documents without calling a model")
	cmd.Flags().StringSliceVar(&only, "only", nil, "generate only these documents ("+strings.Join(docs.DocIDs, ",")+")")
	cmd.Flags().StringVar(&cfg.BaseURL, "llm-url", "", "OpenAI-compatible base URL (default $KULI_LLM_BASE_URL)")
	cmd.Flags().StringVar(&cfg.Model, "llm-model", "", "model name (default $KULI_LLM_MODEL)")
	cmd.Flags().Float64Var(&cfg.Temperature, "temperature", 0.2, "sampling temperature")
	cmd.Flags().IntVar(&maxTokens, "max-tokens", 4096, "max tokens per model reply (reasoning models count thinking here)")
	cmd.Flags().StringVar(&cfg.ReasoningEffort, "reasoning-effort", "", "send reasoning_effort (e.g. none, low) for reasoning models (default $KULI_LLM_REASONING_EFFORT)")
	cmd.AddCommand(newDocsStatusCmd(o))
	return cmd
}

func newDocsStatusCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show which generated docs are stale because their evidence changed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := o.resolvedDataDir()
			if err != nil {
				return err
			}
			root, err := filepath.Abs(o.repo)
			if err != nil {
				return err
			}
			res, err := docs.Status(filepath.Join(dir, "knowledge"), root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			stale := 0
			for _, r := range res {
				fmt.Fprintf(out, "%-8s %s\n", r.Status, r.Doc.File)
				for i, c := range r.Changed {
					if i == 5 {
						fmt.Fprintf(out, "           … %d more\n", len(r.Changed)-5)
						break
					}
					fmt.Fprintf(out, "           changed: %s\n", c)
				}
				if r.Status != "CURRENT" {
					stale++
				}
			}
			if stale > 0 {
				fmt.Fprintf(out, "\n%d stale — run `kuli scan && kuli docs --only <id>` to refresh\n", stale)
			}
			return nil
		},
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func relTo(root, p string) string {
	if rel, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return p
}
