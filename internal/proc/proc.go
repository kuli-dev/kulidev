// Package proc starts child processes. Use it instead of os/exec directly: on Windows a
// GUI program (the desktop app) that starts a console program such as git gets a
// console window flashing up for every call unless it asks for none.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.Command without a console window on Windows.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hide(cmd)
	return cmd
}

// CommandContext is exec.CommandContext without a console window on Windows.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	return cmd
}
