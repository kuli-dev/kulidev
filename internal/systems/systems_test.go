package systems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/store"
)

func put(t *testing.T, root, p, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(p))
	os.MkdirAll(filepath.Dir(full), 0o755)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A folder of three repositories plus a top-level compose file.
func TestFolderOfRepos(t *testing.T) {
	root := t.TempDir()
	for _, r := range []string{"shop-api", "shop-web", "worker"} {
		os.MkdirAll(filepath.Join(root, r, ".git"), 0o755)
	}
	put(t, root, "shop-api/pyproject.toml", "[project]\nname = \"shop-core\"\n")
	put(t, root, "shop-api/shop_core/__init__.py", "")
	put(t, root, "shop-api/shop_core/models.py", "class Order:\n    pass\n")
	put(t, root, "shop-api/app/main.py", "from shop_core.models import Order\n")
	put(t, root, "shop-api/app/orders.py", "router = APIRouter(prefix=\"/orders\")\n\n@router.get(\"/{order_id}/items\")\ndef items(order_id: int):\n    return []\n\n@app.get(\"/health\")\ndef health():\n    return 1\n")
	put(t, root, "shop-web/package.json", `{"name": "shop-web", "dependencies": {"react": "19"}}`+"\n")
	put(t, root, "shop-web/src/api.ts", "export const base = process.env.API_URL;\nexport const items = (id: string) => fetch(`${base}/api/v1/orders/${id}/items`);\nexport const up = () => fetch(`${base}/health`);\n")
	put(t, root, "shop-web/.env.example", "SHOP_API_URL=http://localhost:8000\nDISABLE_WORKER=1\n# worker: not used here\n")
	put(t, root, "worker/pyproject.toml", "[project]\nname = \"worker\"\ndependencies = [\n  # \"shop-core @ file:///old\",\n  \"shop-core>=1.0\",\n]\n")
	put(t, root, "worker/run.py", "import os\nfrom shop_core import models\n")
	put(t, root, "docker-compose.yml", "services:\n  shop-api:\n    build: ./shop-api\n  web:\n    build: ./shop-web\n")

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Refresh(root, true); err != nil {
		t.Fatal(err)
	}
	snap, err := st.LoadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	m := Build(root, snap)

	var names []string
	for _, p := range m.Projects {
		names = append(names, p.Path)
	}
	if strings.Join(names, ",") != ",shop-api,shop-web,worker" {
		t.Fatalf("projects = %q", names)
	}
	has := func(from, to, kind, evPath string) {
		t.Helper()
		for _, l := range m.Links {
			if l.From == from && l.To == to && l.Kind == kind {
				for _, e := range l.Evidence {
					if e.Path == evPath && e.Line > 0 && e.Text != "" {
						return
					}
				}
				t.Errorf("%s -%s-> %s: evidence %+v lacks %s", from, kind, to, l.Evidence, evPath)
				return
			}
		}
		t.Errorf("missing link %s -%s-> %s; links: %+v", from, kind, to, m.Links)
	}
	has("worker", "shop-api", Imports, "worker/run.py")
	has("shop-web", "shop-api", CallsAPI, "shop-web/src/api.ts")
	for _, l := range m.Links {
		if l.Kind == CallsAPI {
			if l.Count != 1 || !strings.Contains(l.Evidence[0].Target, "shop-api/app/orders.py:3") {
				t.Errorf("calls API = %+v (only the items call, not /health)", l)
			}
		}
	}
	has("worker", "shop-api", DependsOn, "worker/pyproject.toml")
	has("shop-web", "shop-api", Config, "shop-web/.env.example")
	has("", "shop-api", Config, "docker-compose.yml")
	has("", "shop-web", Config, "docker-compose.yml")
	for _, l := range m.Links {
		if l.From == "shop-api" || l.To == "worker" {
			t.Errorf("unexpected link %+v", l) // a flag (DISABLE_WORKER) and a comment are not links
		}
		if l.Kind == DependsOn && l.Count != 1 {
			t.Errorf("commented dependency counted: %+v", l)
		}
	}
}

func TestSingleRepoHasNoSystems(t *testing.T) {
	root := t.TempDir()
	put(t, root, "go.mod", "module example.com/one\n")
	put(t, root, "main.go", "package main\nfunc main() {}\n")
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	st.Refresh(root, true)
	snap, _ := st.LoadSnapshot()
	if m := Build(root, snap); len(m.Projects) > 1 || len(m.Links) != 0 {
		t.Errorf("single repo map = %+v", m)
	}
}
