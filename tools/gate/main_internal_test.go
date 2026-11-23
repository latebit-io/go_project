package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/gate/check"
	"github.com/latebit-io/go_project/tools/gate/conv"
	"github.com/latebit-io/go_project/tools/gate/proc"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const (
	commitMsg = "commit-msg"
	agentStep = "agent"
)

// rig is a gate over a scratch module with scripted tools.
type rig struct {
	app  app
	fake *proctest.Fake
	out  *bytes.Buffer
}

func newRig(t *testing.T, files fstest.MapFS) rig {
	t.Helper()

	root := t.TempDir()
	if err := os.CopyFS(root, files); err != nil {
		t.Fatalf("write module: %v", err)
	}

	out := &bytes.Buffer{}
	fake := &proctest.Fake{Answers: map[string]proctest.Answer{
		"golangci-lint version --short":    {Out: "2.15.0\n"},
		"git config --get core.hooksPath":  {Out: ".githooks\n"},
		"git rev-parse --absolute-git-dir": {Out: root + "\n"},
	}}

	return rig{fake: fake, out: out, app: app{
		root: root, in: strings.NewReader("{}"), out: out,
		getenv: func(string) string { return "" },
		runner: func(io.Writer) proc.Runner { return fake },
	}}
}

func TestCommitMessage(t *testing.T) {
	t.Parallel()

	r := newRig(t, fstest.MapFS{
		"GOOD": {Data: []byte("# Please enter the commit message\nfeat: add export\n\nbody\n")},
		"BAD":  {Data: []byte("oops\n")},
		"PAD":  {Data: []byte(" feat: add export  \n")},
	})

	good, bad := filepath.Join(r.app.root, "GOOD"), filepath.Join(r.app.root, "BAD")

	if done, err := r.app.dispatch(t.Context(), []string{commitMsg, good}); !done || err != nil {
		t.Fatalf("commit-msg GOOD = %v, %v", done, err)
	}

	if _, err := r.app.dispatch(t.Context(), []string{commitMsg, bad}); !errors.Is(err, conv.ErrConvention) {
		t.Fatalf("commit-msg BAD = %v, want a convention error", err)
	}

	// The hook must refuse what the commits step would refuse once it is stored.
	padded := filepath.Join(r.app.root, "PAD")
	if _, err := r.app.dispatch(t.Context(), []string{commitMsg, padded}); !errors.Is(err, conv.ErrConvention) {
		t.Fatalf("commit-msg with a leading space = %v, want a convention error", err)
	}

	if _, err := r.app.dispatch(t.Context(), []string{commitMsg, "--subject", "fix:", "handle", "nil"}); err != nil {
		t.Fatalf("commit-msg --subject = %v", err)
	}

	missing := filepath.Join(r.app.root, "MISSING")

	for _, args := range [][]string{{commitMsg}, {commitMsg, missing}, {"help"}, {"init"}, {agentStep, "a", "b"}} {
		if done, err := r.app.dispatch(t.Context(), args); done || err == nil {
			t.Errorf("dispatch(%v) = %v, %v, want a failure", args, done, err)
		}
	}
}

func TestPreCommitLeavesExistingSubjectsToCI(t *testing.T) {
	t.Parallel()

	steps := preCommitSteps()
	if slices.Contains(steps, "commits") || len(steps) != len(check.Names())-1 {
		t.Fatalf("preCommitSteps() = %v, want every step except commits", steps)
	}
}

func TestStepsAndSetup(t *testing.T) {
	t.Parallel()

	r := newRig(t, fstest.MapFS{".golangci-lint-version": {Data: []byte("v2.15.0\n")}})

	if done, err := r.app.dispatch(t.Context(), []string{"tools"}); !done || err != nil {
		t.Fatalf("dispatch(tools) = %v, %v\n%s", done, err, r.out)
	}

	if _, err := r.app.dispatch(t.Context(), []string{"nope"}); !errors.Is(err, check.ErrFailed) {
		t.Fatalf("dispatch(nope) = %v, want an unknown step failure", err)
	}

	if done, err := r.app.dispatch(t.Context(), []string{"setup"}); !done || err != nil {
		t.Fatalf("dispatch(setup) = %v, %v", done, err)
	}

	if _, err := r.app.dispatch(t.Context(), []string{"vuln"}); err != nil {
		t.Fatalf("dispatch(vuln) = %v", err)
	}

	calls := r.fake.Calls()
	for _, want := range []string{"git config core.hooksPath .githooks", "go run " + vulncheck + " ./..."} {
		if !slices.Contains(calls, want) {
			t.Errorf("never ran %q; ran %v", want, calls)
		}
	}
}

func TestAgentAndInit(t *testing.T) {
	t.Parallel()

	r := newRig(t, fstest.MapFS{"go.mod": {Data: []byte("module example.com/old\n")}})

	if done, err := r.app.dispatch(t.Context(), []string{agentStep, "claude-prompt"}); !done || err != nil {
		t.Fatalf("dispatch(agent claude-prompt) = %v, %v", done, err)
	}

	// A clean tree: nothing to gate, so the step says so and never runs a check.
	if done, err := r.app.dispatch(t.Context(), []string{agentStep}); !done || err != nil {
		t.Fatalf("dispatch(agent) = %v, %v", done, err)
	}

	if !strings.Contains(r.out.String(), "nothing uncommitted") {
		t.Fatalf("agent output = %q, want the clean-tree line", r.out)
	}

	if done, err := r.app.dispatch(t.Context(), []string{"init", "github.com/acme/shop"}); !done || err != nil {
		t.Fatalf("dispatch(init) = %v, %v", done, err)
	}
}
