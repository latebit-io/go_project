// Package check holds the steps of the gate and runs them in order.
package check

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/latebit-io/go_project/tools/gate/git"
)

// ErrFailed marks a check that ran and found a problem, as opposed to one
// that could not run.
var ErrFailed = errors.New("check failed")

// Env is what the steps work on: the module's files and its git repository,
// whose Runner also starts the tools.
type Env struct {
	Tree fs.FS
	Repo git.Repo
	Out  io.Writer

	writeErr error
}

// step is one named check.
type step struct {
	name  string
	title string
	run   func(ctx context.Context, env *Env) error
}

// steps lists the gate in order. Cheap checks come first, so a trivial
// failure does not wait for lint and tests.
func steps() []step {
	return []step{
		{name: "tools", title: "Tools and hooks", run: toolsAndHooks},
		{name: "branch", title: "Branch name", run: branchName},
		{name: "commits", title: "Commit subjects", run: commitSubjects},
		{name: "tidy", title: "Module tidy", run: modTidy},
		{name: "binaries", title: "Staged binaries", run: stagedBinaries},
		{name: "tests-changed", title: "Tests changed", run: testsChanged},
		{name: "arch", title: "Architecture", run: architecture},
		{name: "clean", title: "Cleanup", run: cleanup},
		{name: "format", title: "Format", run: formatting},
		{name: "lint", title: "Lint", run: lint},
		{name: "cross", title: "Cross-platform build", run: crossBuild},
		{name: "test", title: "Tests and coverage", run: testsAndCoverage},
		{name: "smoke", title: "Commands", run: commands},
	}
}

// Names lists the steps in gate order.
func Names() []string {
	all := steps()

	names := make([]string, 0, len(all))
	for _, each := range all {
		names = append(names, each.name)
	}

	return names
}

// Run runs the named steps in gate order, or every step when none is named,
// and stops at the first failure.
func Run(ctx context.Context, env *Env, names ...string) error {
	for _, name := range names {
		if !slices.Contains(Names(), name) {
			return fmt.Errorf("%w: no step %q; steps: %s", ErrFailed, name, strings.Join(Names(), " "))
		}
	}

	for _, each := range steps() {
		if len(names) > 0 && !slices.Contains(names, each.name) {
			continue
		}

		env.sayf("\n==> %s\n", each.title)

		if err := each.run(ctx, env); err != nil {
			return fmt.Errorf("%s: %w", each.name, err)
		}
	}

	// A partial run must not read as the whole gate passing.
	passed := "all checks"
	if len(names) > 0 {
		passed = strings.Join(names, " ") + " only"
	}

	env.sayf("\nPASS: %s\n", passed)

	return env.writeErr
}

// sayf writes progress for the user. The first write error is kept and
// returned by Run, so the steps need not handle it one by one.
func (e *Env) sayf(format string, args ...any) {
	if _, err := fmt.Fprintf(e.Out, format, args...); err != nil && e.writeErr == nil {
		e.writeErr = fmt.Errorf("write output: %w", err)
	}
}

// inCI reports a run on a CI server, where nothing may be skipped quietly.
func (e *Env) inCI() bool {
	return e.Repo.Getenv("CI") != ""
}

// failf reports a finding: what is wrong, as an ErrFailed.
func failf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFailed, fmt.Sprintf(format, args...))
}
