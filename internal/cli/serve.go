package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
			if (tlsCert == "") != (tlsKey == "") {
				return errors.New("--tls-cert and --tls-key go together")
			}
			if !loopback && !useTLS && !insecure {
				return errors.New("refusing to serve on a network address without TLS: add --tls-self-signed, " +
					"--tls-cert/--tls-key, or --insecure-http if the network is already encrypted (Tailscale, WireGuard, SSH tunnel)")
			}

			st, err := o.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			root, err := filepath.Abs(o.repo)
			if err != nil {
				return err
			}
			dir, err := o.resolvedDataDir()
			if err != nil {
				return err
			}
			var model docs.Chatter
			if c, err := lf.client(); err == nil {
				model = c
				fmt.Fprintf(os.Stderr, "kuli: ask uses model %s\n", c.Model())
			} else {
				fmt.Fprintf(os.Stderr, "kuli: %v — the Ask page will be disabled\n", err)
			}

			// Off the loopback interface every device must pair; locally it's optional.
			var devices *auth.Store
			if !loopback || forceAuth {
				if devices, err = auth.Open(filepath.Join(dir, "devices.json")); err != nil {
					return err
				}
			}
			addrs := auth.LocalAddresses()
			if host != "" && host != "0.0.0.0" && host != "::" {
				addrs = []string{host}
			}
			if selfSigned {
				if tlsCert, tlsKey, err = auth.SelfSignedCert(filepath.Join(dir, "tls"), addrs); err != nil {
					return err
				}
			}
			// A terminal is a shell for whoever is allowed in: on a network address it
			// needs an explicit opt-in even though devices are paired.
			terminals := !noTerminal && !readOnly && (loopback || remoteShell)
			srv, err := server.New(root, dir, st, model, web.FS(), server.Options{
				ReadOnly: readOnly, Loopback: loopback, Terminals: terminals, Auth: devices, TLS: useTLS,
			})
			if err != nil {
				return err
			}
			defer srv.Close()

			network := "tcp"
			if host == "0.0.0.0" {
				network = "tcp4" // "all IPv4 interfaces", as asked (and what WSL forwards)
			}
			ln, err := net.Listen(network, addr)
			if err != nil {
				return err
			}
			scheme := "http"
			if useTLS {
				scheme = "https"
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "KuliDev is serving %s on %s://%s\n", filepath.Base(root), scheme, ln.Addr())
			if !loopback && !terminals && !noTerminal && !readOnly {
				fmt.Fprintln(os.Stderr, "kuli: terminals are off on a network address; add --allow-remote-terminal to give paired devices a shell")
			}
			if devices != nil {
				printPairing(out, devices, scheme, addrs, port, loopback)
			}

			hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
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
	lf.add(cmd, 2048)
	return cmd
}

// printPairing shows a one-time code, the URLs that carry it, and a QR code for phones.
func printPairing(out io.Writer, devices *auth.Store, scheme string, addrs []string, port string, loopback bool) {
	code := devices.NewCode()
	if loopback || len(addrs) == 0 {
		addrs = []string{"127.0.0.1"}
	}
	var urls []string
	for _, a := range addrs {
		h := a
		if strings.Contains(a, ":") {
			h = "[" + a + "]"
		}
		urls = append(urls, fmt.Sprintf("%s://%s:%s/#/pair?code=%s", scheme, h, port, code))
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
