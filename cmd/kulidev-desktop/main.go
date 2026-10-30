//go:build windows || darwin

// Command kulidev-desktop is KuliDev as a desktop app: the workbench in a native window
// (WebView2 on Windows, WebKit on macOS), no browser and no commands. Start it with no
// arguments for the Open Folder screen, or pass a folder (drag it onto the app).
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/kuli-dev/kulidev/internal/cli"
	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/llm"
	"github.com/kuli-dev/kulidev/internal/proc"
	"github.com/kuli-dev/kulidev/internal/server"
	"github.com/kuli-dev/kulidev/internal/web"
)

func main() {
	// "KuliDev mcp …" (how coding agents started from the app reach the knowledge graph)
	// and the other commands work as in the kuli CLI.
	if len(os.Args) > 1 && isCommand(os.Args[1]) {
		if err := cli.NewRoot().Execute(); err != nil {
			fmt.Fprintln(os.Stderr, "kuli:", err)
			os.Exit(1)
		}
		return
	}
	state := cli.UserStateDir()
	os.MkdirAll(state, 0o700)
	// A GUI app has no console: keep a log next to the recent-folders file.
	if f, err := os.OpenFile(filepath.Join(state, "desktop.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	// The model comes from Settings unless KULI_LLM_* is set in the environment.
	var model docs.Chatter
	if c, err := llm.New(llm.Config{MaxTokens: 2048}.FromEnv()); err == nil {
		model = c
	}
	hub := server.NewHost(server.HostOptions{
		UI: web.FS(), LLM: model, CanOpen: true, RecentFile: filepath.Join(state, "recent.json"),
		SettingsFile: filepath.Join(state, "settings.json"), Version: cli.Version,
		Options: server.Options{Loopback: true, Terminals: true, Version: cli.Version},
	})
	defer hub.Close()
	if len(os.Args) > 1 {
		// In the background: the window shows at once, with the progress.
		if err := hub.StartOpen(os.Args[1]); err != nil {
			log.Printf("open %s: %v", os.Args[1], err) // the Open Folder screen shows instead
		}
	}

	// A private port on loopback only; the window is the only intended client.
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: hub.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	url := fmt.Sprintf("http://%s/", ln.Addr())
	log.Printf("KuliDev desktop on %s", url)

	w := webview.New(false)
	if w == nil {
		// No WebView2 runtime (very old Windows): fall back to the default browser.
		openBrowser(url)
		select {}
	}
	defer w.Destroy()
	w.SetTitle("KuliDev")
	w.SetSize(1440, 920, webview.HintNone)
	w.Navigate(url)
	w.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		proc.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		proc.Command("open", url).Start()
	}
}

// isCommand reports whether arg names a kuli CLI command rather than a folder to open.
func isCommand(arg string) bool {
	if _, err := os.Stat(arg); err == nil {
		return false // an existing folder (drag and drop) wins
	}
	for _, c := range cli.NewRoot().Commands() {
		if c.Name() == arg {
			return true
		}
	}
	return arg == "help" || arg == "--help" || arg == "-h" || arg == "--version"
}
