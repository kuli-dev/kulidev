package docs

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := map[string]string{
		"      POSTGRES_PASSWORD: hunter2":                            "      POSTGRES_PASSWORD: <redacted>",
		"  - DATABASE_URL=postgresql+asyncpg://user:s3cr3t@db:5432/x": "  - DATABASE_URL=postgresql+asyncpg://user:<redacted>@db:5432/x",
		`  "apiKey": "sk-abc123",`:                                    `  "apiKey": "<redacted>",`,
		"export AWS_SECRET_ACCESS_KEY=AKIAxyz":                        "export AWS_SECRET_ACCESS_KEY=<redacted>",
		"redis://:p4ss@cache:6379/0":                                  "redis://:<redacted>@cache:6379/0",
		// references and harmless lines stay readable
		"  - GITHUB_TOKEN=${GITHUB_TOKEN}":    "  - GITHUB_TOKEN=${GITHUB_TOKEN}",
		"ARG GITHUB_TOKEN":                    "ARG GITHUB_TOKEN",
		"url = https://${TOKEN}@github.com/x": "url = https://${TOKEN}@github.com/x",
		"image: postgres:16":                  "image: postgres:16",
		"https://github.com/kuli-dev/kulidev": "https://github.com/kuli-dev/kulidev",
	}
	for in, want := range cases {
		if got := redact(in); got != want {
			t.Errorf("redact(%q)\n got %q\nwant %q", in, got, want)
		}
		if strings.Contains(redact(in), "hunter2") || strings.Contains(redact(in), "s3cr3t") || strings.Contains(redact(in), "p4ss") {
			t.Errorf("secret leaked: %q", redact(in))
		}
	}
}
