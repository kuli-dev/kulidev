// Command kuli is the KuliDev command line.
package main

import (
	"fmt"
	"os"

	"github.com/kuli-dev/kulidev/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kuli:", err)
		os.Exit(1)
	}
}
