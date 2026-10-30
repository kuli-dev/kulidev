package scan

import (
	"path"
	"strings"

	"github.com/kulidev/kulidev/internal/parse"
)

type langInfo struct {
	name    string // display name
	grammar string // parse grammar id, "" when not parsed yet
}

var byExt = map[string]langInfo{
	".go":     {"Go", parse.Go},
	".py":     {"Python", parse.Python},
	".js":     {"JavaScript", parse.JavaScript},
	".mjs":    {"JavaScript", parse.JavaScript},
	".cjs":    {"JavaScript", parse.JavaScript},
	".jsx":    {"JavaScript", parse.JavaScript},
	".ts":     {"TypeScript", parse.TypeScript},
	".mts":    {"TypeScript", parse.TypeScript},
	".cts":    {"TypeScript", parse.TypeScript},
	".tsx":    {"TypeScript", parse.TSX},
	".java":   {"Java", ""},
	".kt":     {"Kotlin", ""},
	".php":    {"PHP", ""},
	".rb":     {"Ruby", ""},
	".rs":     {"Rust", ""},
	".cs":     {"C#", ""},
	".swift":  {"Swift", ""},
	".c":      {"C", ""},
	".h":      {"C", ""},
	".cc":     {"C++", ""},
	".cpp":    {"C++", ""},
	".hpp":    {"C++", ""},
	".sql":    {"SQL", ""},
	".sh":     {"Shell", ""},
	".bash":   {"Shell", ""},
	".tf":     {"Terraform", ""},
	".yaml":   {"YAML", ""},
	".yml":    {"YAML", ""},
	".json":   {"JSON", ""},
	".toml":   {"TOML", ""},
	".md":     {"Markdown", ""},
	".html":   {"HTML", ""},
	".css":    {"CSS", ""},
	".scss":   {"CSS", ""},
	".vue":    {"Vue", ""},
	".svelte": {"Svelte", ""},
	".proto":  {"Protobuf", ""},
}

func detectLang(p string) langInfo {
	base := path.Base(p)
	if base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile") {
		return langInfo{name: "Dockerfile"}
	}
	if base == "Makefile" {
		return langInfo{name: "Makefile"}
	}
	if strings.HasSuffix(base, ".d.ts") {
		return langInfo{"TypeScript", parse.TypeScript}
	}
	return byExt[strings.ToLower(path.Ext(base))]
}

// manifestKind classifies build, dependency, CI and infrastructure files.
// head is the start of the file, used to recognise Kubernetes YAML.
func manifestKind(p string, head []byte) string {
	base := path.Base(p)
	switch base {
	case "go.mod", "package.json", "requirements.txt", "pyproject.toml", "Pipfile", "setup.py",
		"pom.xml", "build.gradle", "build.gradle.kts", "composer.json", "Cargo.toml", "Gemfile",
		"Chart.yaml", "Jenkinsfile", ".gitlab-ci.yml", "kustomization.yaml", "Makefile":
		return base
	}
	switch {
	case base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") || strings.HasSuffix(base, ".Dockerfile"):
		return "Dockerfile"
	case strings.HasPrefix(base, "docker-compose") || strings.HasPrefix(base, "compose."):
		if ext := path.Ext(base); ext == ".yml" || ext == ".yaml" {
			return "Docker Compose"
		}
	case strings.HasSuffix(base, ".tf"):
		return "Terraform"
	case strings.Contains(p, ".github/workflows/"):
		return "GitHub Actions"
	case strings.HasSuffix(base, ".sql") && strings.Contains(strings.ToLower(p), "migration"):
		return "SQL migration"
	}
	if ext := path.Ext(base); (ext == ".yaml" || ext == ".yml") && isKubeYAML(head) {
		return "Kubernetes manifest"
	}
	return ""
}

func isKubeYAML(head []byte) bool {
	s := string(head)
	return strings.Contains(s, "apiVersion:") && strings.Contains(s, "\nkind:")
}
