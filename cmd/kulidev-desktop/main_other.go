//go:build !windows && !darwin

// Command kulidev-desktop is the desktop app for Windows and macOS. On other systems
// use `kuli serve` and a browser.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "The KuliDev desktop app is built for Windows and macOS. Here, run `kuli serve` and open the URL it prints.")
	os.Exit(1)
}
