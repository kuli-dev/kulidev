package scan

import "testing"

func TestResolve(t *testing.T) {
	files := []string{
		"go.mod", "cmd/app/main.go", "internal/store/store.go",
		"web/src/lib/api.ts", "web/src/components/index.tsx", "web/src/app.tsx",
		"svc/app/__init__.py", "svc/app/db.py", "svc/app/models/user.py",
		"svc/ns/agent/nodes.py", "svc/ns/agent/prompts.py", "alembic/env.py",
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
		// a root "alembic/" folder must not capture the alembic library
		{"Python", "svc/app/db.py", "alembic", Target{TargetExternal, "py:alembic"}, false},
		{"Python", "svc/app/db.py", "alembic.env", Target{TargetFile, "alembic/env.py"}, false},
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

func TestIsStdlib(t *testing.T) {
	for name, want := range map[string]bool{
		"go:fmt": true, "go:net/http": true, "go:github.com/spf13/cobra": false,
		"py:os": true, "py:json": true, "py:typing": true, "py:sqlalchemy": false,
		"npm:fs": true, "npm:node:path": true, "npm:fs/promises": true, "npm:react": false,
		"npm:@tanstack/react-query": false,
	} {
		if got := IsStdlib(name); got != want {
			t.Errorf("IsStdlib(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolveJavaAndPHP(t *testing.T) {
	files := []string{
		"service/src/main/java/com/acme/billing/InvoiceService.java",
		"service/src/main/java/com/acme/model/Invoice.java",
		"service/src/main/java/com/acme/util/Money.java",
		"app/Models/User.php", "app/Http/Controllers/InvoiceController.php", "app/Http/Controllers/helpers.php",
		"legacy/old.php",
		"plugins/mail/composer.json", "plugins/mail/src/WP.php", "plugins/mail/src/Admin/Area.php",
		"wp/wp-admin/includes/upgrade.php",
	}
	r := newResolver(files, func(p string) []byte {
		if p == "plugins/mail/composer.json" {
			return []byte(`{"autoload":{"psr-4":{"WPMailSMTP\\":"src/"}}}`)
		}
		return nil
	})
	cases := []struct {
		lang, from, spec string
		want             Target
	}{
		{"Java", files[0], "com.acme.model.Invoice", Target{TargetFile, files[1]}},
		{"Java", files[0], "com.acme.util.Money.cents", Target{TargetFile, files[2]}}, // static import
		{"Java", files[0], "com.acme.model", Target{TargetPackage, "service/src/main/java/com/acme/model"}},
		{"Java", files[0], "java.util.List", Target{TargetExternal, "java:java.util"}},
		{"Java", files[0], "org.springframework.web.bind.annotation.GetMapping", Target{TargetExternal, "java:org.springframework"}},
		{"PHP", files[4], "App\\Models\\User", Target{TargetFile, "app/Models/User.php"}}, // PSR-4: App\ → app/
		{"PHP", files[4], "/helpers.php", Target{TargetFile, "app/Http/Controllers/helpers.php"}},
		{"PHP", "index.php", "legacy/old.php", Target{TargetFile, "legacy/old.php"}},
		{"PHP", files[4], "Illuminate\\Http\\Request", Target{TargetExternal, "php:Illuminate"}},
		{"PHP", files[4], "Exception", Target{TargetExternal, "php:Exception"}},
		// composer.json PSR-4: WPMailSMTP\ → plugins/mail/src/
		{"PHP", files[4], "WPMailSMTP\\WP", Target{TargetFile, "plugins/mail/src/WP.php"}},
		{"PHP", files[4], "WPMailSMTP\\Admin\\Area", Target{TargetFile, "plugins/mail/src/Admin/Area.php"}},
		// ABSPATH . 'wp-admin/includes/upgrade.php': unknown base, found by suffix
		{"PHP", "plugins/mail/src/WP.php", "@wp-admin/includes/upgrade.php", Target{TargetFile, "wp/wp-admin/includes/upgrade.php"}},
		{"PHP", "plugins/mail/src/WP.php", "@wp-admin/includes/missing.php", Target{TargetExternal, "php:include"}},
		{"PHP", "plugins/mail/src/WP.php", "/vendor/autoload.php", Target{TargetExternal, "composer:vendor"}},
	}
	for _, c := range cases {
		if got := r.resolve(c.lang, c.from, c.spec, false); got != c.want {
			t.Errorf("resolve(%s, %q) = %+v, want %+v", c.lang, c.spec, got, c.want)
		}
	}
	for name, want := range map[string]bool{"java:java.util": true, "java:org.springframework": false, "php:Exception": true, "php:Illuminate": false} {
		if IsStdlib(name) != want {
			t.Errorf("IsStdlib(%q) = %v", name, !want)
		}
	}
}

func TestGeneratedPSR4AndSkipped(t *testing.T) {
	gen := `<?php
$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);
return array(
    'WPMailSMTP\\Vendor\\' => array($baseDir . '/vendor_prefixed'),
    'WPMailSMTP\\' => array($baseDir . '/src'),
    'Psr\\Log\\' => array($vendorDir . '/psr/log/src'),
);`
	files := []string{"plugin/wp-mail-smtp.php", "plugin/src/WP.php", "plugin/vendor_prefixed/Monolog/Logger.php"}
	r := newResolver(files, func(p string) []byte {
		if p == "plugin/vendor/composer/autoload_psr4.php" {
			return []byte(gen)
		}
		return nil
	})
	for spec, want := range map[string]Target{
		"WPMailSMTP\\WP":                      {TargetFile, "plugin/src/WP.php"},
		"WPMailSMTP\\Vendor\\Monolog\\Logger": {TargetFile, "plugin/vendor_prefixed/Monolog/Logger.php"},
	} {
		if got := r.resolve("PHP", "plugin/src/WP.php", spec, false); got != want {
			t.Errorf("%s → %+v, want %+v", spec, got, want)
		}
	}
	if got := r.resolve("TypeScript", "web/app.ts", "./build/gen", false); got.Ref != "skipped:web/build/gen" {
		t.Errorf("import into a skipped folder = %+v", got)
	}
}
