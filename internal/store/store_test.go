package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kuli-dev/kulidev/internal/scan"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanSaveQuery(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":                  "module example.com/shop\n",
		"cmd/shop/main.go":        "package main\nimport \"example.com/shop/internal/payment\"\nfunc main() { payment.Charge() }\n",
		"internal/payment/pay.go": "package payment\ntype PaymentService struct{}\nfunc Charge() {}\n",
		"web/src/api.ts":          "export async function fetchOrders() {}\n",
		"web/src/app.tsx":         "import { fetchOrders } from './api';\nimport React from 'react';\nexport function App() { return null }\n",
		"Dockerfile":              "FROM golang\n",
		"deploy/app.yaml":         "apiVersion: apps/v1\nkind: Deployment\n",
		"node_modules/x/index.js": "function ignored() {}\n",
	})

	res, err := scan.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(filepath.Join(t.TempDir(), "kd"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Save(res); err != nil {
		t.Fatal(err)
	}
	// Saving twice must replace, not duplicate.
	if _, err := st.Save(res); err != nil {
		t.Fatal(err)
	}

	stats, err := st.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Parsed != 4 {
		t.Errorf("parsed = %d, want 4 (node_modules must be skipped)", stats.Parsed)
	}
	kinds := map[string]bool{}
	for _, m := range stats.Manifests {
		kinds[m.Label] = true
	}
	for _, k := range []string{"go.mod", "Dockerfile", "Kubernetes manifest"} {
		if !kinds[k] {
			t.Errorf("manifest %q not detected: %v", k, stats.Manifests)
		}
	}

	hits, err := st.Find("payment", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.Name == "PaymentService" && h.Path == "internal/payment/pay.go" && h.Line == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("Find(payment) missing PaymentService with evidence: %+v", hits)
	}

	_, users, err := st.Deps("internal/payment", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Path != "cmd/shop/main.go" || users[0].Line != 2 {
		t.Errorf("reverse deps of internal/payment = %+v", users)
	}
	// A Go file is reachable through its package.
	if _, users, _ := st.Deps("internal/payment/pay.go", true); len(users) != 1 {
		t.Errorf("reverse deps of pay.go = %+v", users)
	}

	_, imps, err := st.Deps("web/src/app.tsx", false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range imps {
		got[h.Name] = true
	}
	if !got["web/src/api.ts"] || !got["npm:react"] {
		t.Errorf("deps of app.tsx = %+v", imps)
	}
}
