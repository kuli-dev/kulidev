package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "kulidev", "settings.json")
	if s, err := Load(p); err != nil || s.LLM.BaseURL != "" {
		t.Fatalf("missing file = %+v %v", s, err)
	}
	in := Settings{LLM: LLM{BaseURL: "http://localhost:11434/v1", Model: "qwen", APIKey: "k", ReasoningEffort: "none"}}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(p)
	if err != nil || out != in {
		t.Fatalf("round trip = %+v %v", out, err)
	}
	if info, _ := os.Stat(p); runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestValidate(t *testing.T) {
	for _, c := range []struct {
		l  LLM
		ok bool
	}{
		{LLM{}, true},
		{LLM{BaseURL: "https://x.example/v1", Model: "m"}, true},
		{LLM{BaseURL: "ftp://x", Model: "m"}, false},
		{LLM{BaseURL: "https://x.example/v1"}, false},
		{LLM{BaseURL: "https://x.example/v1", Model: "m", ReasoningEffort: "max"}, false},
	} {
		if err := c.l.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err = %v", c.l, err)
		}
	}
}
