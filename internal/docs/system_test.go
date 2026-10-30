package docs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/store"
	"github.com/kuli-dev/kulidev/internal/systems"
)

func TestSystemOverview(t *testing.T) {
	root := t.TempDir()
	for _, r := range []string{"shop-api", "worker"} {
		os.MkdirAll(filepath.Join(root, r, ".git"), 0o755)
	}
	for p, body := range map[string]string{
		"shop-api/pyproject.toml":        "[project]\nname = \"shop-core\"\n",
		"shop-api/shop_core/__init__.py": "",
		"shop-api/shop_core/models.py":   "class Order:\n    pass\n",
		"worker/pyproject.toml":          "[project]\nname = \"worker\"\ndependencies = [\"shop-core\"]\n",
		"worker/run.py":                  "from shop_core import models\n",
	} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(body), 0o644)
	}
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	st.Refresh(root, true)
	snap, _ := st.LoadSnapshot()
	m := systems.Build(root, snap)
	out := filepath.Join(root, ".kulidev", "knowledge")

	res, err := SystemOverview(&m, root, out, "test")
	if err != nil || res == nil {
		t.Fatalf("overview = %v %v", res, err)
	}
	b, _ := os.ReadFile(filepath.Join(out, res.File))
	doc := string(b)
	for _, want := range []string{"## Projects", "**shop-api**", "### worker → shop-api", "worker imports code from shop-api",
		"](../../worker/run.py#L1)", "worker declares a dependency on shop-api", "**shop-api** is used by 1"} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in:\n%s", want, doc)
		}
	}
	if res.Evidence["worker/run.py"] == "" {
		t.Errorf("evidence = %v", res.Evidence)
	}
	// Regenerating the other docs keeps the System overview in the list, first.
	if _, err := Generate(context.Background(), snap, Options{OutDir: out, Version: "test"}); err != nil {
		t.Fatal(err)
	}
	man, _ := ReadManifest(out)
	if len(man.Docs) == 0 || man.Docs[0].ID != SystemID {
		t.Errorf("manifest after Generate = %+v", man.Docs)
	}
	fr, _ := Status(out, root)
	if fr[0].Status != "CURRENT" {
		t.Errorf("fresh overview reported %s", fr[0].Status)
	}
	os.WriteFile(filepath.Join(root, "worker/run.py"), []byte("from shop_core import models  # edited\n"), 0o644)
	if fr, _ := Status(out, root); fr[0].Status != "STALE" {
		t.Errorf("edited evidence not stale: %s", fr[0].Status)
	}
}
