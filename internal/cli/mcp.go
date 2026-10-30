package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/mcpserver"
	"github.com/kuli-dev/kulidev/internal/store"
)

func newMCPCmd(o *options) *cobra.Command {
	var rescan bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve the knowledge base to coding agents over MCP (stdio)",
		Long: `Runs an MCP server on stdin/stdout so agents such as Claude Code can query
the knowledge graph. Scans the repository first if it has no knowledge base yet.

  claude mcp add kulidev -- kuli mcp -C /path/to/repo`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Cursor passes ${workspaceFolder}; a client that leaves it unexpanded gets the
			// folder it started the server in.
			if strings.Contains(o.repo, "${") {
				o.repo = "."
			}
			root, err := filepath.Abs(o.repo)
			if err != nil {
				return err
			}
			dir, err := o.resolvedDataDir()
			if err != nil {
				return err
			}
			_, statErr := os.Stat(filepath.Join(dir, store.DBFile))
			st, err := store.Open(dir)
			if err != nil {
				return err
			}
			defer st.Close()

			srv := mcpserver.New(root, st)
			// stdout carries the protocol; progress goes to stderr.
			if rescan || statErr != nil {
				fmt.Fprintf(os.Stderr, "kuli: scanning %s…\n", root)
				if err := srv.Rescan(); err != nil {
					return fmt.Errorf("initial scan: %w", err)
				}
			}
			return srv.Run(cmd.Context(), Version)
		},
	}
	cmd.Flags().BoolVar(&rescan, "rescan", false, "rebuild the knowledge base before serving")
	return cmd
}
