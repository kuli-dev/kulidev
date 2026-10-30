package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/auth"
	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/server"
	"github.com/kuli-dev/kulidev/internal/web"
)

func newServeCmd(o *options) *cobra.Command {
	var (
		addr                 string
		readOnly, noTerminal bool
		remoteShell          bool
		forceAuth            bool
		tlsCert, tlsKey      string
		selfSigned, insecure bool
		publicURL            string
		lf                   llmFlags
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Open the KuliDev workbench in a browser (local, or remote with device pairing)",
		Long: `Serves the workbench and its API for the repository's knowledge base.

Local (default): binds to 127.0.0.1 and needs no login.

Remote, e.g. from your phone or laptop:
  kuli serve --addr 0.0.0.0:7878 --tls-self-signed
Every device must be paired: the server prints a one-time code and a QR code; open
it on the device. TLS is required on a network address — use --tls-self-signed, your
own --tls-cert/--tls-key, or --insecure-http when the network is already encrypted
(Tailscale, WireGuard, an SSH tunnel). Run 'kuli scan' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return fmt.Errorf("bad --addr: %w", err)
			}
			loopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
			useTLS := tlsCert != "" || selfSigned
			// Behind a public tunnel (ngrok, Cloudflare Tunnel) we listen on loopback, but
			// the workspace is on the internet: treat it as remote in every way that matters.
			var pub *url.URL
			if publicURL != "" {
				if pub, err = url.Parse(publicURL); err != nil || pub.Host == "" || (pub.Scheme != "https" && pub.Scheme != "http") {
					return fmt.Errorf("--public-url must look like https://name.ngrok-free.app")
				}
				if pub.Scheme != "https" {
					return errors.New("--public-url must be https: the tunnel provides the TLS")
				}
				if !loopback {
					return errors.New("--public-url expects kuli to listen on loopback (the default --addr) with the tunnel in front")
				}
			}
			remote := !loopback || pub != nil
			if (tlsCert == "") != (tlsKey == "") {
				return errors.New("--tls-cert and --tls-key go together")
			}
			if !loopback && !useTLS && !insecure {
				return errors.New("refusing to serve on a network address without TLS: add --tls-self-signed, " +
					"--tls-cert/--tls-key, or --insecure-http if the network is already encrypted (Tailscale, WireGuard, SSH tunnel)")
			}

			// Which folder: -C, else the current folder when it was scanned before, else
			// none — the browser then shows "Open folder" (local use only).
			var root, dir string
			if f := cmd.Flag("repo"); f != nil && f.Changed {
				if root, err = filepath.Abs(o.repo); err != nil {
					return err
				}
			} else if cwd, _ := os.Getwd(); hasKnowledge(cwd) {
				root = cwd
			}
			if root == "" && remote {
				return errors.New("remote access serves one folder: add -C <repo>")
			}
			if root != "" {
				if o.dataDir != "" {
					dir, _ = filepath.Abs(o.dataDir)
				} else {
					dir = filepath.Join(root, ".kulidev")
				}
			}
			var model docs.Chatter
			if c, err := lf.client(); err == nil {
				model = c
				fmt.Fprintf(os.Stderr, "kuli: ask uses model %s\n", c.Model())
			} else {
				fmt.Fprintf(os.Stderr, "kuli: %v — using the model from Settings, if one is saved there\n", err)
			}

			// Off the loopback interface every device must pair; locally it's optional.
			stateDir := dir
			if stateDir == "" {
				stateDir = userStateDir()
			}
			var devices *auth.Store
			if remote || forceAuth {
				if devices, err = auth.Open(filepath.Join(stateDir, "devices.json")); err != nil {
					return err
				}
			}
			addrs := auth.LocalAddresses()
			if host != "" && host != "0.0.0.0" && host != "::" {
				addrs = []string{host}
			}
			if selfSigned {
				if tlsCert, tlsKey, err = auth.SelfSignedCert(filepath.Join(stateDir, "tls"), addrs); err != nil {
					return err
				}
			}
			// A terminal is a shell for whoever is allowed in: on a network address it
			// needs an explicit opt-in even though devices are paired.
			terminals := !noTerminal && !readOnly && (!remote || remoteShell)
			if terminals && runtime.GOOS == "windows" {
				terminals = false // no PTY support yet; WSL has the full workbench
				fmt.Fprintln(os.Stderr, "kuli: the terminal and agents are not supported on Windows yet; use kuli inside WSL for them")
			}
			opts := server.Options{ReadOnly: readOnly, Loopback: loopback, Terminals: terminals, Auth: devices, TLS: useTLS}
			if pub != nil {
				opts.AllowHosts = []string{pub.Hostname()}
				opts.TLS = true // the tunnel terminates TLS: cookies must be Secure
			}
			hub := server.NewHost(server.HostOptions{
				UI: web.FS(), LLM: model, Options: opts, RecentFile: filepath.Join(userStateDir(), "recent.json"),
				SettingsFile: filepath.Join(userStateDir(), "settings.json"), Version: Version,
				CanOpen: !remote && devices == nil, // browsing folders is for the local user only
			})
			defer hub.Close()
			if root != "" {
				sum, err := hub.Open(root, dir)
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "kuli: %s (%s)\n", filepath.Base(root), sum)
			}

			network := "tcp"
			if host == "0.0.0.0" {
				network = "tcp4" // "all IPv4 interfaces", as asked (and what WSL forwards)
			}
			ln, err := net.Listen(network, addr)
			if errors.Is(err, syscall.EADDRINUSE) {
				return fmt.Errorf("port %s is already in use (another kuli serve?) — stop it or pick another, e.g. --addr %s:%d", port, host, portPlus(port))
			} else if err != nil {
				return err
			}
			scheme := "http"
			if useTLS {
				scheme = "https"
			}
			out := cmd.OutOrStdout()
			if root == "" {
				fmt.Fprintf(out, "KuliDev is running on %s://%s — open it and choose a folder\n", scheme, ln.Addr())
			} else {
				fmt.Fprintf(out, "KuliDev is serving %s on %s://%s\n", filepath.Base(root), scheme, ln.Addr())
			}
			if remote && !terminals && !noTerminal && !readOnly {
				fmt.Fprintln(os.Stderr, "kuli: terminals are off for remote access; add --allow-remote-terminal to give paired devices a shell")
			}
			if pub != nil {
				fmt.Fprintf(out, "Public URL: %s (pairing required)\n", strings.TrimSuffix(pub.String(), "/"))
				printPairingURLs(out, devices, []string{strings.TrimSuffix(pub.String(), "/")})
			} else if devices != nil {
				printPairing(out, devices, scheme, addrs, port, loopback)
			}

			hs := &http.Server{Handler: hub.Handler(), ReadHeaderTimeout: 10 * time.Second}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM) // also systemd/docker stop
			defer stop()
			go func() {
				<-ctx.Done()
				shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				hs.Shutdown(shut)
			}()
			if useTLS {
				err = hs.ServeTLS(ln, tlsCert, tlsKey)
			} else {
				err = hs.Serve(ln)
			}
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1:7878", "listen address (0.0.0.0:7878 for other devices)")
	f.BoolVar(&readOnly, "read-only", false, "browse only: refuse to save files and open terminals")
	f.BoolVar(&noTerminal, "no-terminal", false, "disable the integrated terminal")
	f.BoolVar(&remoteShell, "allow-remote-terminal", false, "allow terminals on a network address (paired devices get a shell)")
	f.BoolVar(&forceAuth, "auth", false, "require device pairing even on localhost")
	f.StringVar(&tlsCert, "tls-cert", "", "TLS certificate file")
	f.StringVar(&tlsKey, "tls-key", "", "TLS private key file")
	f.BoolVar(&selfSigned, "tls-self-signed", false, "serve HTTPS with a generated certificate (browsers warn once)")
	f.BoolVar(&insecure, "insecure-http", false, "allow plain HTTP on a network address (only on an already-encrypted network)")
	f.StringVar(&publicURL, "public-url", "", "public https URL of a tunnel in front of kuli (ngrok, Cloudflare Tunnel); forces pairing")
	lf.add(cmd, 2048)
	return cmd
}

func portPlus(p string) int {
	n, _ := strconv.Atoi(p)
	return n + 1
}

// printPairing shows a one-time code, the URLs that carry it, and a QR code for phones.
func printPairing(out io.Writer, devices *auth.Store, scheme string, addrs []string, port string, loopback bool) {
	if loopback || len(addrs) == 0 {
		addrs = []string{"127.0.0.1"}
	}
	var bases []string
	for _, a := range addrs {
		h := a
		if strings.Contains(a, ":") {
			h = "[" + a + "]"
		}
		bases = append(bases, fmt.Sprintf("%s://%s:%s", scheme, h, port))
	}
	printPairingURLs(out, devices, bases)
}

func printPairingURLs(out io.Writer, devices *auth.Store, bases []string) {
	code := devices.NewCode()
	var urls []string
	for _, b := range bases {
		urls = append(urls, b+"/#/pair?code="+code)
	}
	fmt.Fprintf(out, "\nPair a device — code %s (valid %d minutes, works once):\n", code, int(auth.CodeTTL.Minutes()))
	for _, u := range urls {
		fmt.Fprintf(out, "  %s\n", u)
	}
	if q, err := auth.TerminalQR(urls[0]); err == nil {
		fmt.Fprintf(out, "\nScan with your phone:\n%s", q)
	}
	fmt.Fprintln(out, "Paired devices can invite more from the Devices view.")
}

// hasKnowledge reports whether dir was scanned before (it has a knowledge base).
func hasKnowledge(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".kulidev", "kulidev.db"))
	return err == nil
}

// UserStateDir is exported for the desktop app.
func UserStateDir() string { return userStateDir() }

// userStateDir holds per-user state that isn't tied to one repository (recent folders,
// pairing for a folder-less server): ~/.config/kulidev, %AppData%\kulidev, …
func userStateDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "kulidev")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kulidev-user")
}
