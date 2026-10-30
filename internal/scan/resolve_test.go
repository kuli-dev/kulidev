package scan

import "testing"

func TestResolve(t *testing.T) {
	files := []string{
		"go.mod", "cmd/app/main.go", "internal/store/store.go",
		"web/src/lib/api.ts", "web/src/components/index.tsx", "web/src/app.tsx",
		"svc/app/__init__.py", "svc/app/db.py", "svc/app/models/user.py",
	}
	read := func(p string) []byte {
		if p == "go.mod" {
			return []byte("module example.com/demo\n\ngo 1.23\n")
		}
		return nil
	}
	r := newResolver(files, read)

	cases := []struct {
		lang, from, spec string
		want             Target
	}{
		{"Go", "cmd/app/main.go", "example.com/demo/internal/store", Target{TargetPackage, "internal/store"}},
		{"Go", "cmd/app/main.go", "fmt", Target{TargetExternal, "go:fmt"}},
		{"TypeScript", "web/src/app.tsx", "./lib/api", Target{TargetFile, "web/src/lib/api.ts"}},
		{"TypeScript", "web/src/app.tsx", "./lib/api.js", Target{TargetFile, "web/src/lib/api.ts"}},
		{"TypeScript", "web/src/app.tsx", "./components", Target{TargetFile, "web/src/components/index.tsx"}},
		{"TypeScript", "web/src/app.tsx", "@/lib/api", Target{TargetFile, "web/src/lib/api.ts"}},
		{"TypeScript", "web/src/app.tsx", "@tanstack/react-query/x", Target{TargetExternal, "npm:@tanstack/react-query"}},
		{"Python", "svc/app/models/user.py", "..db", Target{TargetFile, "svc/app/db.py"}},
		{"Python", "svc/app/db.py", "app.models.user", Target{TargetFile, "svc/app/models/user.py"}},
		{"Python", "svc/app/db.py", "sqlalchemy.orm", Target{TargetExternal, "py:sqlalchemy"}},
	}
	for _, c := range cases {
		if got := r.resolve(c.lang, c.from, c.spec); got != c.want {
			t.Errorf("resolve(%s, %s, %q) = %+v, want %+v", c.lang, c.from, c.spec, got, c.want)
		}
	}
}
