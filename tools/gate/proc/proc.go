// Package proc runs the external programs the gate depends on: git, go and
// golangci-lint.
package proc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner starts a program. env adds "KEY=value" entries to the environment.
type Runner interface {
	// Output returns what the program wrote to standard output.
	Output(ctx context.Context, env []string, name string, args ...string) (string, error)
	// Stream sends the program's output to the user as it runs.
	Stream(ctx context.Context, env []string, name string, args ...string) error
}

// Exec is the Runner that starts real processes in Dir. Out receives both
// output streams of a streamed program.
type Exec struct {
	Dir string
	Out io.Writer
}

// Output implements Runner. A failure carries the program's error output.
func (e Exec) Output(ctx context.Context, env []string, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := e.command(ctx, env, name, args)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

// Stream implements Runner.
func (e Exec) Stream(ctx context.Context, env []string, name string, args ...string) error {
	cmd := e.command(ctx, env, name, args)
	cmd.Stdout, cmd.Stderr = e.Out, e.Out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}

	return nil
}

func (e Exec) command(ctx context.Context, env []string, name string, args []string) *exec.Cmd {
	//nolint:gosec // the gate starts a fixed set of tools with arguments it builds itself
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.Dir
	cmd.Env = append(os.Environ(), env...)

	return cmd
}
