package docs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/llm"
	"github.com/kuli-dev/kulidev/internal/scan"
	"github.com/kuli-dev/kulidev/internal/store"
)

// fakeLLM answers with one valid cite, one unknown evidence id, one out-of-range
// line cite, and an uncited paragraph — and records what it was sent.
type fakeLLM struct{ prompts []string }

func (f *fakeLLM) Model() string { return "fake" }
func (f *fakeLLM) Chat(_ context.Context, msgs []llm.Message) (string, llm.Usage, error) {
	f.prompts = append(f.prompts, msgs[len(msgs)-1].Content)
	return "## Section\n\nThe service charges payments [E1].\n\nThis is made up [E999].\n\n" +
		"- Bad line range [E1 L5000-5001]\n- No citation at all\n", llm.Usage{Prompt: 10, Completion: 5}, nil
}

func fixture(t *testing.T) (*store.Snapshot, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"README.md":               "# Shop\n\nA tiny shop.\n\n## Getting started\n\nRun `make dev`.\n",
		"Makefile":                "dev:\n\tgo run ./cmd/shop\ntest:\n\tgo test ./...\n",
		"go.mod":                  "module example.com/shop\n\ngo 1.23\n",
		"cmd/shop/main.go":        "package main\nimport \"example.com/shop/internal/payment\"\nfunc main() { payment.Charge() }\n",
		"internal/payment/pay.go": "package payment\ntype PaymentService struct{}\nfunc Charge() {}\n",
		"docker-compose.yml":      "services:\n  db:\n    image: postgres:16\n    environment:\n      POSTGRES_PASSWORD: hunter2\n      DATABASE_URL: postgres://u:s3cr3t@db/x\n",
		".env.example":            "API_KEY=changeme\nDB_HOST=localhost\n",
	}
	for p, body := range files {
		full := filepath.Join(root, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(body), 0o644)
	}
	res, err := scan.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "kd"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Save(res); err != nil {
		t.Fatal(err)
	}
	snap, err := st.LoadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snap, root
}

func TestGenerateWithLLM(t *testing.T) {
	snap, root := fixture(t)
	out := filepath.Join(root, ".kulidev", "knowledge")
	fake := &fakeLLM{}
	m, err := Generate(context.Background(), snap, Options{OutDir: out, LLM: fake, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Docs) != 5 {
		t.Fatalf("got %d docs, want 5", len(m.Docs))
	}
	for _, p := range fake.prompts {
		for _, secret := range []string{"hunter2", "s3cr3t", "changeme"} {
			if strings.Contains(p, secret) {
				t.Errorf("secret %q was sent to the LLM", secret)
			}
		}
	}
	if !strings.Contains(strings.Join(fake.prompts, "\n"), "API_KEY") {
		t.Error("env example variable names should reach the LLM")
	}

	ov := m.Docs[0]
	if ov.ID != "overview" || ov.Valid != 1 || ov.Removed != 2 {
		t.Errorf("overview citations: %+v", ov)
	}
	// 4 claims (2 paragraphs + 2 bullets), only the first validly cited.
	if ov.Coverage != 0.25 {
		t.Errorf("coverage = %v, want 0.25", ov.Coverage)
	}
	b, _ := os.ReadFile(filepath.Join(out, "project-overview.md"))
	doc := string(b)
	for _, want := range []string{"citation_coverage: 0.25", "](../../", "# Project overview"} {
		if !strings.Contains(doc, want) {
			t.Errorf("overview missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "E999") {
		t.Error("invalid citation left in document")
	}

	arch, _ := os.ReadFile(filepath.Join(out, "architecture.md"))
	if !strings.Contains(string(arch), "```mermaid") || !strings.Contains(string(arch), "-->|1|") {
		t.Errorf("architecture lacks dependency graph:\n%s", arch)
	}
}

func TestStatusDetectsStaleDocs(t *testing.T) {
	snap, root := fixture(t)
	out := filepath.Join(root, ".kulidev", "knowledge")
	if _, err := Generate(context.Background(), snap, Options{OutDir: out, Version: "test"}); err != nil {
		t.Fatal(err)
	}
	res, err := Status(out, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Status != "CURRENT" {
			t.Errorf("%s should be CURRENT right after generation: %v", r.Doc.ID, r.Changed)
		}
	}

	os.WriteFile(filepath.Join(root, "internal/payment/pay.go"), []byte("package payment\nfunc Refund() {}\n"), 0o644)
	res, _ = Status(out, root)
	stale := map[string]bool{}
	for _, r := range res {
		if r.Status == "STALE" {
			stale[r.Doc.ID] = true
		}
	}
	if !stale["architecture"] {
		t.Errorf("architecture cites pay.go and should be STALE: %+v", res)
	}
	if stale["getting-started"] {
		t.Errorf("getting-started does not cite pay.go and should stay CURRENT")
	}
}

func TestFactsOnlyNeedsNoLLM(t *testing.T) {
	snap, root := fixture(t)
	out := filepath.Join(root, "docs-out")
	m, err := Generate(context.Background(), snap, Options{OutDir: out, Only: []string{"getting-started"}, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Docs) != 1 || m.Docs[0].Mode != "facts-only" {
		t.Fatalf("docs = %+v", m.Docs)
	}
	b, _ := os.ReadFile(filepath.Join(out, "getting-started.md"))
	for _, want := range []string{"make targets in Makefile: dev", "API_KEY", "Facts-only mode"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("getting-started missing %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "changeme") {
		t.Error("env example value leaked into docs")
	}
}
