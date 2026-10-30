package scan

import "testing"

func TestResolve(t *testing.T) {
	files := []string{
		"go.mod", "cmd/app/main.go", "internal/store/store.go",
		"web/src/lib/api.ts", "web/src/components/index.tsx", "web/src/app.tsx",
		"svc/app/__init__.py", "svc/app/db.py", "svc/app/models/user.py",
		"svc/ns/agent/nodes.py", "svc/ns/agent/prompts.py",
		"web/src/lib/workspace/package.json", "web/src/lib/workspace/src/index.ts",
	}
	read := func(p string) []byte {
		if p == "web/src/lib/workspace/package.json" {
			return []byte(`{"name":"@x/workspace","main":"src/index.ts"}`)
		}
		if p == "go.mod" {
			return []byte("module example.com/demo\n\ngo 1.23\n")
		}
		return nil
	}
	r := newResolver(files, read)

	cases := []struct {
		lang, from, spec string
		want             Target
		maybeSymbol      bool
	}{
		{"Go", "cmd/app/main.go", "example.com/demo/internal/store", Target{TargetPackage, "internal/store"}, false},
		{"Go", "cmd/app/main.go", "fmt", Target{TargetExternal, "go:fmt"}, false},
		{"TypeScript", "web/src/app.tsx", "./lib/api", Target{TargetFile, "web/src/lib/api.ts"}, false},
		{"TypeScript", "web/src/app.tsx", "./lib/api.js", Target{TargetFile, "web/src/lib/api.ts"}, false},
		{"TypeScript", "web/src/app.tsx", "./components", Target{TargetFile, "web/src/components/index.tsx"}, false},
		{"TypeScript", "web/src/app.tsx", "@/lib/api", Target{TargetFile, "web/src/lib/api.ts"}, false},
		{"TypeScript", "web/src/app.tsx", "@tanstack/react-query/x", Target{TargetExternal, "npm:@tanstack/react-query"}, false},
		{"TypeScript", "web/src/app.tsx", "@/lib/workspace", Target{TargetFile, "web/src/lib/workspace/src/index.ts"}, false},
		{"Python", "svc/app/models/user.py", "..db", Target{TargetFile, "svc/app/db.py"}, false},
		{"Python", "svc/app/db.py", "app.models.user", Target{TargetFile, "svc/app/models/user.py"}, false},
		{"Python", "svc/app/db.py", "sqlalchemy.orm", Target{TargetExternal, "py:sqlalchemy"}, false},
		// namespace package (no __init__.py): "from . import prompts"
		{"Python", "svc/ns/agent/nodes.py", ".prompts", Target{TargetFile, "svc/ns/agent/prompts.py"}, true},
		{"Python", "svc/ns/agent/nodes.py", ".", Target{TargetPackage, "svc/ns/agent"}, false},
		// "from . import helper" where helper lives in __init__.py
		{"Python", "svc/app/db.py", ".helper", Target{TargetFile, "svc/app/__init__.py"}, true},
		// genuinely missing module must stay broken, even inside a package
		{"Python", "svc/app/db.py", ".models_gone", Target{TargetExternal, "unresolved:svc/app/models_gone"}, false},
		{"Python", "svc/ns/agent/nodes.py", ".models", Target{TargetExternal, "unresolved:svc/ns/agent/models"}, false},
	}
	for _, c := range cases {
		if got := r.resolve(c.lang, c.from, c.spec, c.maybeSymbol); got != c.want {
			t.Errorf("resolve(%s, %s, %q) = %+v, want %+v", c.lang, c.from, c.spec, got, c.want)
		}
	}
}
