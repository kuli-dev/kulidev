package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kuli-dev/kulidev/internal/auth"
)

func TestSettingsSaveApplyTest(t *testing.T) {
	var gotKey string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "OK"}, "finish_reason": "stop"}}})
	}))
	defer model.Close()

	file := filepath.Join(t.TempDir(), "settings.json")
	h := NewHost(HostOptions{CanOpen: true, SettingsFile: file, Options: Options{Loopback: true}})
	defer h.Close()
	ts := httptest.NewServer(h.Handler())
	defer ts.Close()
	h.Open(mkRepo(t, "alpha"), "")

	body := `{"llm":{"baseURL":"` + model.URL + `/v1","model":"qwen","apiKey":"sk-test","reasoningEffort":"none"}}`
	if code, out := call(t, ts, "POST", "/api/settings/test", body); code != 200 || out["ok"] != true || out["reply"] != "OK" {
		t.Fatalf("test = %d %v", code, out)
	}
	if gotKey != "Bearer sk-test" {
		t.Errorf("key sent = %q", gotKey)
	}
	if code, out := call(t, ts, "PUT", "/api/settings", body); code != 200 || out["activeModel"] != "qwen" {
		t.Fatalf("save = %d %v", code, out)
	}
	// Applied at once to the open folder.
	if _, ov := call(t, ts, "GET", "/api/overview", ""); ov["llm"] != true || ov["model"] != "qwen" {
		t.Errorf("overview after save = %v", ov)
	}
	// The key is stored but never sent back.
	_, got := call(t, ts, "GET", "/api/settings", "")
	if strings.Contains(asJSON(got), "sk-test") || got["llm"].(map[string]any)["hasKey"] != true {
		t.Errorf("settings view = %v", got)
	}
	// Saving without a key keeps the saved one; clearKey removes it.
	call(t, ts, "PUT", "/api/settings", `{"llm":{"baseURL":"`+model.URL+`/v1","model":"qwen2"}}`)
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "sk-test") || !strings.Contains(string(b), "qwen2") {
		t.Errorf("file = %s", b)
	}
	call(t, ts, "PUT", "/api/settings", `{"llm":{"baseURL":"`+model.URL+`/v1","model":"qwen2"},"clearKey":true}`)
	call(t, ts, "PUT", "/api/settings", `{"llm":{"baseURL":"`+model.URL+`/v1","model":"qwen2","clearKey":true}}`)
	if b, _ := os.ReadFile(file); strings.Contains(string(b), "sk-test") {
		t.Errorf("key not cleared: %s", b)
	}
	if code, _ := call(t, ts, "PUT", "/api/settings", `{"llm":{"baseURL":"file:///etc","model":"x"}}`); code != 400 {
		t.Errorf("bad URL saved: %d", code)
	}
	// A new host picks the saved model up.
	h2 := NewHost(HostOptions{SettingsFile: file})
	if h2.llm == nil || h2.llm.Model() != "qwen2" {
		t.Errorf("reloaded model = %v", h2.llm)
	}
}

func TestSettingsNotForRemote(t *testing.T) {
	devices, _ := auth.Open(filepath.Join(t.TempDir(), "d.json"))
	h := NewHost(HostOptions{CanOpen: true, SettingsFile: filepath.Join(t.TempDir(), "s.json"), Options: Options{Auth: devices}})
	h.Open(mkRepo(t, "alpha"), "")
	defer h.Close()
	ts := httptest.NewServer(h.Handler())
	defer ts.Close()
	if code, _ := call(t, ts, "PUT", "/api/settings", `{"llm":{"baseURL":"http://evil.example/v1","model":"x"}}`); code == 200 {
		t.Error("a remote host accepted settings")
	}
}
