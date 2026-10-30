package ask

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

type fake struct{ prompts []string }

func (f *fake) Model() string { return "fake" }
func (f *fake) Chat(_ context.Context, m []llm.Message) (string, llm.Usage, error) {
	last := m[len(m)-1].Content
	f.prompts = append(f.prompts, last)
	if strings.HasPrefix(last, "List 6-10 search keywords") {
		return "reconcile, Invoice, bank_statement", llm.Usage{}, nil
	}
	return "Invoices are matched to bank lines in reconcile [E1]. Made up [E99].", llm.Usage{}, nil
}

func TestAsk(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"billing/recon.py": "\"\"\"Bank reconciliation.\"\"\"\n\ndef reconcile(invoice, line):\n    \"\"\"Match an invoice to a bank statement line.\"\"\"\n    password = 'hunter2'\n    return invoice.total == line.amount\n",
		"api/routes.py":    "from billing.recon import reconcile\n\ndef post_line(x):\n    return reconcile(x, x)\n",
	}
	for p, b := range files {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(b), 0o644)
	}
	res, err := scan.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "kd"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Save(res)

	f := &fake{}
	a, err := Ask(context.Background(), st, "how do we match payments to the bank?", Options{Root: root, LLM: f})
	if err != nil {
		t.Fatal(err)
	}
	facts := f.prompts[1]
	for _, want := range []string{"function reconcile in billing/recon.py", "Match an invoice to a bank statement line.",
		"3| def reconcile", "billing/recon.py is imported by 1 files", "api/routes.py"} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts missing %q:\n%s", want, facts)
		}
	}
	if strings.Contains(facts, "hunter2") {
		t.Error("secret in code snippet reached the model")
	}
	if !strings.Contains(a.Text, "(billing/recon.py:3-6)") || strings.Contains(a.Text, "E99") {
		t.Errorf("answer = %q", a.Text)
	}
	if a.Stats.Valid != 1 || a.Stats.Removed != 1 {
		t.Errorf("stats = %+v", a.Stats)
	}

	// Without a model: retrieval only, still useful.
	a, err = Ask(context.Background(), st, "reconcile invoice", Options{Root: root})
	if err != nil || a.Text != "" || !strings.Contains(a.Facts, "def reconcile") {
		t.Errorf("retrieval-only: err=%v text=%q", err, a.Text)
	}
}

func TestExcerptKeepsKeywordLines(t *testing.T) {
	var lines []string
	for i := 1; i <= 120; i++ {
		lines = append(lines, "x := 1")
	}
	lines[99] = "if out == \"\" { return ErrEmptyReply }"
	got := excerpt(lines, 1, 120, []string{"empty"})
	has := map[int]bool{}
	for _, i := range got {
		has[i] = true
	}
	if !has[1] || !has[100] || !has[97] || !has[103] || !has[120] {
		t.Errorf("excerpt = %v", got)
	}
	if len(got) > snippetCap+8 {
		t.Errorf("excerpt too long: %d lines", len(got))
	}
}
