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

// errAny stands for "some error" where its identity does not matter.
var errAny = errors.New("any error")

// matches reports whether err is what a case expects.
func matches(err, want error) bool {
	if errors.Is(want, errAny) {
		return err != nil
	}

	return errors.Is(err, want)
}

func data(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

// rig is a gate over a scratch module with scripted tools.
type rig struct {
	app  *app
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

	return rig{fake: fake, out: out, app: &app{
		root: root, in: strings.NewReader("{}"), out: out,
		getenv: func(string) string { return "" },
		runner: func(io.Writer) proc.Runner { return fake },
	}}
}

// inModule turns arguments written in capitals, the message files of these
// tests, into paths inside the scratch module.
func (r rig) inModule(args []string) []string {
	placed := slices.Clone(args)
	for i, arg := range placed {
		if arg == strings.ToUpper(arg) && !strings.HasPrefix(arg, "-") {
			placed[i] = filepath.Join(r.app.root, arg)
		}
	}

	return placed
}

func TestDispatch(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"go.mod":                 data("module example.com/old\n"),
		".golangci-lint-version": data("v2.15.0\n"),
		"GOOD":                   data("# Please enter the commit message\nfeat: add export\n\nbody\n"),
		"BAD":                    data("oops\n"),
		// git stores a leading space as typed, so the hook must refuse it.
		"PAD": data(" feat: add export  \n"),
	}

	tests := []struct {
		name string
		// args naming one of the files are given as a path inside the module.
		args     []string
		wantDone bool
		// wantErr is matched with errors.Is; errAny accepts any error.
		wantErr  error
		wantOut  string
		wantCall string
	}{
		{name: "commit message from a file", args: []string{commitMsg, "GOOD"}, wantDone: true},
		{name: "unconventional message", args: []string{commitMsg, "BAD"}, wantErr: conv.ErrConvention},
		{name: "message with a leading space", args: []string{commitMsg, "PAD"}, wantErr: conv.ErrConvention},
		{name: "subject given directly", args: []string{commitMsg, "--subject", "fix:", "handle", "nil"}, wantDone: true},
		{name: "commit-msg without a file", args: []string{commitMsg}, wantErr: errUsage},
		{name: "commit-msg with a missing file", args: []string{commitMsg, "MISSING"}, wantErr: errAny},
		{name: "help", args: []string{"help"}, wantErr: errUsage},
		{name: "init without a module", args: []string{"init"}, wantErr: errUsage},
		{name: "agent with too many arguments", args: []string{agentStep, "a", "b"}, wantErr: errUsage},
		{name: "one named step", args: []string{"tools"}, wantDone: true, wantOut: "PASS: tools only"},
		{name: "an unknown step", args: []string{"nope"}, wantErr: check.ErrFailed},
		{
			name: "setup enables the hooks and checks the tools", args: []string{"setup"},
			wantDone: true, wantCall: "git config core.hooksPath .githooks",
		},
		{
			name: "vuln runs the pinned scanner", args: []string{"vuln"},
			wantDone: true, wantCall: "go run " + vulncheck + " ./...",
		},
		{name: "agent prompt hook", args: []string{agentStep, "claude-prompt"}, wantDone: true},
		{name: "agent step on a clean tree", args: []string{agentStep}, wantDone: true, wantOut: "nothing uncommitted"},
		{
			name: "init renames the module", args: []string{"init", "github.com/acme/shop"},
			wantDone: true, wantCall: "go mod edit -module github.com/acme/shop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, files)

			done, err := r.app.dispatch(t.Context(), r.inModule(tt.args))
			if done != tt.wantDone || !matches(err, tt.wantErr) || !strings.Contains(r.out.String(), tt.wantOut) {
				t.Fatalf("dispatch(%v) = %v, %v with output %q, want %+v", tt.args, done, err, r.out, tt)
			}

			if calls := r.fake.Calls(); tt.wantCall != "" && !slices.Contains(calls, tt.wantCall) {
				t.Fatalf("dispatch(%v) ran %v, want %q among them", tt.args, calls, tt.wantCall)
			}
		})
	}
}
