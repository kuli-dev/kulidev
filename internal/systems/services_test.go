package systems

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/store"
)

func TestServices(t *testing.T) {
	root := t.TempDir()
	for _, r := range []string{"shop", "web", "core", "infra", "mailer"} {
		os.MkdirAll(filepath.Join(root, r, ".git"), 0o755)
	}
	files := map[string]string{
		"shop/pyproject.toml": "[project]\nname = \"shop\"\n",
		"shop/app/main.py":    "from core import money\n@app.get(\"/orders/{id}\")\ndef order(id):\n    return 1\n",
		"shop/Dockerfile":     "FROM python:3.12\nEXPOSE 8000\nCMD [\"uvicorn\", \"app.main:app\"]\n",
		"shop/docker-compose.yml": `services:
  api:
    build: .
    ports: ["8000:8000"]
    environment:
      - DATABASE_URL=postgresql://u:p@db:5432/shop
      - QUEUE=amqp://guest:guest@mq:5672/
    depends_on: [db, mq]
  order-consumer:
    build: .
    command: python -m app.consumer
    ports: ["9100"]
  db:
    image: postgres:16
  mq:
    image: rabbitmq:3-management
  web:
    build: ../web
    ports: ["3000:3000"]
    environment:
      API_URL: http://api:8000
`,
		"web/package.json":      `{"name": "web", "dependencies": {"next": "15"}}`,
		"web/src/page.tsx":      "export default function Page() { return null }\n",
		"core/pyproject.toml":   "[project]\nname = \"core\"\n",
		"core/core/__init__.py": "",
		"core/core/money.py":    "def add(a, b):\n    return a + b\n",
		"infra/shop/base/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: shop-api
spec:
  template:
    metadata:
      labels: {app: shop-api}
    spec:
      containers:
        - name: api
          image: registry.local/shop-api:1.0
          ports: [{containerPort: 8000}]
---
apiVersion: v1
kind: Service
metadata:
  name: shop-api-svc
spec:
  selector: {app: shop-api}
`,
		"infra/shop/prod/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: shop-api\nspec:\n  replicas: 3\n",
		"infra/shop/dev/deployment.yaml":  "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: shop-api\nspec:\n  replicas: 1\n",
		"infra/shop/prod/cron.yaml": `apiVersion: batch/v1
kind: CronJob
metadata:
  name: shop-nightly-report
spec:
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: report
              image: registry.local/shop-api:1.0
              command: [python, -m, app.report]
              env:
                - name: API
                  value: http://shop-api-svc.default.svc.cluster.local:8000
`,
		"infra/terraform/main.tf":        strings.Repeat("resource \"x\" \"y\" {}\n", 40),
		"infra/web/prod/deployment.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: storefront\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          image: registry.local/web:2\n          ports: [{containerPort: 3000}]\n",
		"mailer/go.mod":                  "module example.com/mailer\n",
		"mailer/main.go":                 "package main\nfunc main() {}\n",
		"mailer/Dockerfile":              "FROM golang\nEXPOSE 2525\n",
		"mailer/Dockerfile.worker":       "FROM golang\nCMD [\"/mailer\", \"worker\"]\n",
	}
	for p, b := range files {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte(b), 0o644)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Refresh(root, true)
	snap, _ := st.LoadSnapshot()
	m := Build(root, snap)

	got := map[string]Service{}
	for _, s := range m.Services {
		got[s.Name] = s
	}
	want := map[string]string{ // name → kind@code project
		"api": "api@shop", "order-consumer": "worker@shop", "db": "database@", "mq": "queue@", "web": "frontend@web",
		"shop-api": "api@shop", "shop-nightly-report": "cronjob@shop", "storefront": "frontend@web", "mailer": "api@mailer", "mailer-worker": "worker@mailer",
	}
	for n, w := range want {
		s, ok := got[n]
		if !ok {
			t.Errorf("service %s not found; have %v", n, keys(got))
			continue
		}
		if g := s.Kind + "@" + s.Project; g != w {
			t.Errorf("%s = %s, want %s", n, g, w)
		}
	}
	if s := got["shop-api"]; strings.Join(s.Envs, ",") != "dev,prod" || s.Deployed != "infra" || len(s.Ports) != 1 {
		t.Errorf("shop-api merged across overlays = %+v", s)
	}
	links := map[string]bool{}
	for _, l := range m.ServiceLinks {
		links[l.From+" "+l.Kind+" "+l.To] = true
		if len(l.Evidence) == 0 || l.Evidence[0].Line == 0 {
			t.Errorf("link without evidence: %+v", l)
		}
	}
	for _, w := range []string{"api depends on db", "api depends on mq", "api calls db", "api calls mq", "web calls api", "shop-nightly-report calls shop-api"} {
		if !links[w] {
			t.Errorf("missing service link %q; have %v", w, links)
		}
	}
	roles := map[string]string{}
	for _, p := range m.Projects {
		roles[p.Name] = strings.Join(p.Roles, ",")
	}
	for p, w := range map[string]string{"shop": "service", "web": "frontend", "core": "library", "infra": "infrastructure", "mailer": "service"} {
		if roles[p] != w {
			t.Errorf("role of %s = %q, want %q", p, roles[p], w)
		}
	}
}

// One repository running several services still gets them (no other projects).
func TestServicesInOneRepo(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("services:\n  api:\n    build: .\n    ports: [\"80\"]\n  worker:\n    build: .\n    command: celery worker\n  cache:\n    image: redis:7\n"), 0o644)
	os.WriteFile(filepath.Join(root, "app.py"), []byte("x = 1\n"), 0o644)
	st, _ := store.Open(t.TempDir())
	defer st.Close()
	st.Refresh(root, true)
	snap, _ := st.LoadSnapshot()
	m := Build(root, snap)
	if len(m.Services) != 3 {
		t.Errorf("services = %+v", m.Services)
	}
}

func keys(m map[string]Service) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
