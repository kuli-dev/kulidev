package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/docs"
	"github.com/kuli-dev/kulidev/internal/server"
	"github.com/kuli-dev/kulidev/internal/web"
)

func newServeCmd(o *options) *cobra.Command {
	var (
		addr        string
		readOnly    bool
		noTerminal  bool
		remoteShell bool
		lf          llmFlags
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Open the KuliDev web UI (architecture graph, search, docs, ask)",
		Long: `Serves the web UI and JSON API for the repository's knowledge base.

Binds to 127.0.0.1 by default: the UI shows your source code. Use --addr 0.0.0.0:7878
only on a network you trust. Run 'kuli scan' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			host, _, _ := net.SplitHostPort(addr)
			loopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
			// A terminal is remote code execution for anyone who can reach the port:
			// on a network address it needs an explicit opt-in.
			terminals := !noTerminal && !readOnly && (loopback || remoteShell)
			srv, err := server.New(root, dir, st, model, web.FS(), server.Options{ReadOnly: readOnly, Loopback: loopback, Terminals: terminals})
			if err != nil {
				return err
			}
			defer srv.Close()
			if !loopback && !terminals && !noTerminal && !readOnly {
				fmt.Fprintln(os.Stderr, "kuli: terminals are off on a network address; add --allow-remote-terminal only on a network you trust")
			}
			if !loopback {
				what := "read and edit"
				if readOnly {
					what = "read"
				}
				fmt.Fprintf(os.Stderr, "kuli: ⚠ listening on %s — anyone who can reach it can %s this repository's code\n", addr, what)
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
			fmt.Fprintf(cmd.OutOrStdout(), "KuliDev is serving %s on http://%s\n", filepath.Base(root), ln.Addr())

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			go func() {
				<-ctx.Done()
				shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				hs.Shutdown(shut)
			}()
			if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:7878", "listen address")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "browse only: refuse to save files and open terminals")
	cmd.Flags().BoolVar(&noTerminal, "no-terminal", false, "disable the integrated terminal")
	cmd.Flags().BoolVar(&remoteShell, "allow-remote-terminal", false, "allow terminals when listening on a non-loopback address (anyone who can reach it gets a shell)")
	lf.add(cmd, 2048)
	return cmd
}
