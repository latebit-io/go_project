// Command gate is the project's one check, the same on a laptop, in a git
// hook, from an agent and in CI: go run ./tools/gate. See usage.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/latebit-io/go_project/tools/gate/agent"
	"github.com/latebit-io/go_project/tools/gate/check"
	"github.com/latebit-io/go_project/tools/gate/conv"
	"github.com/latebit-io/go_project/tools/gate/git"
	"github.com/latebit-io/go_project/tools/gate/initmod"
	"github.com/latebit-io/go_project/tools/gate/proc"
)

const usage = `usage: go run ./tools/gate [step...]         run the gate, or only the named steps
       go run ./tools/gate commit-msg <file>   check a commit message (git hook)
       go run ./tools/gate commit-msg --subject <text>
       go run ./tools/gate agent [stop|prompt|claude-stop|claude-prompt]
                                               the agent's task-complete step
       go run ./tools/gate setup               enable the git hooks, check the tools
       go run ./tools/gate init <module>       rename the module of a new project
       go run ./tools/gate vuln                scan dependencies (needs network)
steps: `

// vulncheck is pinned so a laptop and CI scan with the same tool.
const vulncheck = "golang.org/x/vuln/cmd/govulncheck@v1.10.0"

// Process exit codes. go run reports every failure as 1, so nothing finer
// would reach the caller.
const (
	exitOK = iota
	exitFailed
)

var errUsage = errors.New("bad arguments")

// app is the process the gate runs in, with its edges replaceable in tests.
type app struct {
	root   string
	in     io.Reader
	out    io.Writer
	getenv func(string) string
	// runner starts tools whose output goes to out.
	runner func(out io.Writer) proc.Runner
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
		os.Exit(exitFailed)
	}

	gate := &app{root: root, in: os.Stdin, out: os.Stdout, getenv: os.Getenv}
	gate.runner = func(out io.Writer) proc.Runner { return proc.Exec{Dir: root, Out: out} }

	done, err := gate.dispatch(context.Background(), os.Args[1:])
	if errors.Is(err, errUsage) {
		fmt.Fprintln(os.Stderr, usage+strings.Join(check.Names(), " "))
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "gate:", err)
	}

	if err != nil || !done {
		os.Exit(exitFailed)
	}
}

// dispatch runs one subcommand. It reports done unless the agent step left
// something for the agent: a failing gate or a question to relay.
func (a *app) dispatch(ctx context.Context, args []string) (bool, error) {
	command, rest := "", args
	if len(args) > 0 {
		command, rest = args[0], args[1:]
	}

	var err error

	switch command {
	case "agent":
		return a.agent(ctx, rest)
	case "commit-msg":
		err = commitMessage(rest)
	case "setup":
		err = a.setup(ctx)
	case "init":
		err = a.init(ctx, rest)
	case "vuln":
		err = a.runner(a.out).Stream(ctx, nil, "go", "run", vulncheck, "./...")
	case "help", "-h", "--help":
		err = errUsage
	default:
		err = a.check(ctx, a.out, args...)
	}

	return err == nil, err
}

func (a *app) repo(out io.Writer) git.Repo {
	return git.Repo{Run: a.runner(out), Getenv: a.getenv}
}

// check runs the named steps, or the whole gate, with all output going to out.
func (a *app) check(ctx context.Context, out io.Writer, steps ...string) error {
	env := &check.Env{Tree: os.DirFS(a.root), Repo: a.repo(out), Out: out}
	if err := check.Run(ctx, env, steps...); err != nil {
		return fmt.Errorf("failed at %w", err)
	}

	return nil
}

func (a *app) agent(ctx context.Context, args []string) (bool, error) {
	if len(args) > 1 {
		return false, errUsage
	}

	done, err := agent.Run(ctx, &agent.Config{
		Mode: strings.Join(args, ""),
		Repo: a.repo(a.out),
		In:   a.in,
		Out:  a.out,
		Gate: func(ctx context.Context, out io.Writer) error { return a.check(ctx, out) },
	})
	if err != nil {
		return false, fmt.Errorf("agent step: %w", err)
	}

	return done, nil
}

// commitMessage checks a subject given directly, or the first line of a
// message file that is not a git comment.
func commitMessage(args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	if args[0] == "--subject" {
		return subjectError(strings.Join(args[1:], " "))
	}

	//nolint:gosec // git names the message file; the gate reads it and nothing else
	message, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read commit message: %w", err)
	}

	for line := range strings.Lines(string(message)) {
		if !strings.HasPrefix(line, "#") {
			return subjectError(strings.TrimSpace(line))
		}
	}

	return subjectError("")
}

func subjectError(subject string) error {
	if err := conv.CheckSubject(subject); err != nil {
		return fmt.Errorf("commit subject: %w", err)
	}

	return nil
}

// setup enables the committed git hooks, then checks the tools are usable.
func (a *app) setup(ctx context.Context) error {
	if err := a.repo(a.out).SetHooksPath(ctx, ".githooks"); err != nil {
		return fmt.Errorf("enable git hooks: %w", err)
	}

	return a.check(ctx, a.out, "tools")
}

func (a *app) init(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errUsage
	}

	if err := initmod.Rename(ctx, a.root, a.runner(a.out), args[0]); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	return nil
}
