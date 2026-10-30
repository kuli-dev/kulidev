// Package cli implements the `kuli` command line.
package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/kuli-dev/kulidev/internal/store"
)

// Version is set at build time via -ldflags.
var Version = "dev"

type options struct {
	repo    string
	dataDir string
}

func (o *options) resolvedDataDir() (string, error) {
	if o.dataDir != "" {
		return filepath.Abs(o.dataDir)
	}
	root, err := filepath.Abs(o.repo)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".kulidev"), nil
}

func (o *options) openStore() (*store.Store, error) {
	dir, err := o.resolvedDataDir()
	if err != nil {
		return nil, err
	}
	return store.OpenExisting(dir)
}

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	o := &options{}
	root := &cobra.Command{
		Use:   "kuli",
		Short: "KuliDev — your tireless dev kuli",
		Long: `KuliDev reads the codebase nobody documented, builds an evidence-backed
knowledge graph, and does the grunt work so you don't have to.

Docs-nya mana? Ini.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&o.repo, "repo", "C", ".", "repository root")
	root.PersistentFlags().StringVar(&o.dataDir, "data-dir", "", "where the knowledge base lives (default <repo>/.kulidev)")

	root.AddCommand(
		newScanCmd(o),
		newStatsCmd(o),
		newFindCmd(o),
		newDepsCmd(o),
		newMCPCmd(o),
		newDocsCmd(o),
		&cobra.Command{
			Use:   "version",
			Short: "Print the version",
			Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), "kuli", Version) },
		},
	)
	return root
}
