package agent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/gate/agent"
	"github.com/latebit-io/go_project/tools/gate/git"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const (
	firstTree   = "tree1"
	writeTree   = "git write-tree"
	numstat     = "git diff --numstat"
	stop        = "stop"
	claudeStop  = "claude-stop"
	stagingAsk  = "QUESTION: The gate passes"
	simplifyAsk = "simplify pass"
)

var errGate = errors.New("lint: check failed")

// rig is a repository with scripted git answers and a countable gate.
type rig struct {
	answers map[string]proctest.Answer
	gitDir  string
	gateErr error
	gates   int
	// duringGate, when set, runs while the gate does, to change the tree under it.
	duringGate func()
	// calls are the commands of the latest run.
	calls []string
}

func newRig(t *testing.T) *rig {
	t.Helper()

	dir := t.TempDir()

	return &rig{gitDir: dir, answers: map[string]proctest.Answer{
		"git rev-parse --absolute-git-dir": {Out: dir + "\n"},
		"git rev-parse --abbrev-ref HEAD":  {Out: "feat/x\n"},
		"git rev-parse HEAD":               {Out: "abc\n"},
		"git status":                       {Out: " M a.go\n"},
		"git ls-files -z":                  {Out: "a.go\x00gone.go\x00gen/model.gen.go\x00link\x00"},
		writeTree:                          {Out: firstTree + "\n"},
		numstat:                            {Out: "5\t5\ta.go\n"},
	}}
}

// newTree makes git report a changed work tree from now on.
func newTree(r *rig) { r.answers[writeTree] = proctest.Answer{Out: "tree2\n"} }

// result is whether one run of the step reported done, and what it printed.
type result struct {
	done bool
	out  string
}

func (r *rig) run(t *testing.T, mode string) result {
	t.Helper()

	var out bytes.Buffer

	fake := &proctest.Fake{Answers: r.answers}
	done, err := agent.Run(t.Context(), &agent.Config{
		Mode: mode,
		Repo: git.Repo{Run: fake, Getenv: func(string) string { return "" }},
		// link is a tracked symlink whose target does not exist.
		Tree: fstest.MapFS{"a.go": {}, "gen/model.gen.go": {}, "link": {Data: []byte("nowhere"), Mode: fs.ModeSymlink}},
		In:   strings.NewReader("{}"),
		Out:  &out,
		Gate: func(_ context.Context, w io.Writer) error {
			r.gates++

			if r.duringGate != nil {
				r.duringGate()
			}

			_, err := io.WriteString(w, "==> Lint\nfinding 1\n")

			return errors.Join(err, r.gateErr)
		},
	})
	if err != nil {
		t.Fatalf("Run(%q) error = %v", mode, err)
	}

	r.calls = fake.Calls()

	return result{done: done, out: out.String()}
}

func (r *rig) state(t *testing.T, name string) string {
	t.Helper()

	data, err := fs.ReadFile(os.DirFS(r.gitDir), path.Join("agent-gate", name))
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

// act is one run of the step within a scenario, and what it must do.
type act struct {
	mode string
	// before changes the repository or the gate ahead of this run.
	before func(r *rig)
	done   bool
	// out must appear in the output; silent requires no output at all.
	out    string
	silent bool
	// gates is how often the gate has run so far in the scenario.
	gates int
}

// perform runs the act and fails the test unless it went as described.
func (a act) perform(t *testing.T, r *rig) {
	t.Helper()

	if a.before != nil {
		a.before(r)
	}

	got := r.run(t, a.mode)
	if got.done != a.done || !strings.Contains(got.out, a.out) || (a.silent && got.out != "") || r.gates != a.gates {
		t.Fatalf("run %q = %+v after %d gate runs, want %+v", a.mode, got, r.gates, a)
	}
}

// scenario is a repository and a series of acts on it.
type scenario struct {
	name string
	// given prepares the repository before the first act.
	given func(r *rig)
	// acts run in order on one repository, sharing its state files.
	acts []act
	// state lists state files and what they must hold afterwards.
	state map[string]string
	// call, when set, must start one of the commands of the last act.
	call string
}

// verify checks what the scenario left behind after its last act.
func (s scenario) verify(t *testing.T, r *rig) {
	t.Helper()

	for file, want := range s.state {
		if got := r.state(t, file); got != want {
			t.Fatalf("state %s = %q, want %q", file, got, want)
		}
	}

	started := slices.ContainsFunc(r.calls, func(call string) bool { return strings.HasPrefix(call, s.call) })
	if !started {
		t.Fatalf("no command starting with %q in %v", s.call, r.calls)
	}
}

func TestStep(t *testing.T) {
	t.Parallel()

	tests := []scenario{
		{
			name:  "a clean tree needs nothing, and a hook stays silent",
			given: func(r *rig) { r.answers["git status"] = proctest.Answer{} },
			acts: []act{
				{mode: "", done: true, out: "nothing uncommitted"},
				{mode: stop, done: true, silent: true},
			},
		},
		{
			name: "a passing gate asks about staging once per tree",
			acts: []act{
				{mode: "", out: stagingAsk, gates: 1},
				{mode: "", done: true, out: "nothing to ask", gates: 1},
				{mode: "", before: newTree, out: stagingAsk, gates: 2},
			},
		},
		{
			// A tracked file that .gitignore matches must be in the snapshot, or an
			// edit to it would reuse a remembered pass. Deleted files are left out.
			name:  "the snapshot adds every tracked file that exists, by name",
			acts:  []act{{mode: "", out: stagingAsk, gates: 1}},
			state: map[string]string{"tracked": "a.go\x00gen/model.gen.go\x00link"},
			call:  "GIT_INDEX_FILE=",
		},
		{
			// A link's target can change without the tree changing.
			name: "a symlink in the tree disables reuse of a pass",
			given: func(r *rig) {
				r.answers["git ls-files -s"] = proctest.Answer{Out: "100644 abc 0\ta.go\n120000 def 0\tlink\n"}
			},
			acts: []act{
				{mode: "", out: stagingAsk, gates: 1},
				{mode: "", done: true, gates: 2},
			},
		},
		{
			name:  "a failing gate is reported, then let go until the user has had a turn",
			given: func(r *rig) { r.gateErr = errGate },
			acts: []act{
				{mode: "", out: "finding 1", gates: 1},
				{mode: claudeStop, done: true, out: `"systemMessage"`, gates: 1},
				{mode: stop, done: true, out: "nothing changed since", gates: 1},
				{mode: "prompt", done: true, gates: 1},
				{mode: stop, out: "never weaken", gates: 2},
			},
		},
		{
			name:  "a failing gate blocks a Claude Code stop with its output",
			given: func(r *rig) { r.gateErr = errGate },
			acts:  []act{{mode: claudeStop, done: true, out: `"decision":"block"`, gates: 1}},
		},
		{
			name:  "a pass is not recorded for a tree that moved during the gate",
			given: func(r *rig) { r.duringGate = func() { newTree(r) } },
			acts: []act{
				{mode: "", out: "Files changed while the gate ran", gates: 1},
				{mode: "", before: func(r *rig) { r.duringGate = nil }, out: stagingAsk, gates: 2},
			},
		},
		{
			name:  "a major change asks about simplify first, once, and the prompt settles it",
			given: func(r *rig) { r.answers[numstat] = proctest.Answer{Out: "150\t60\ta.go\n60\t0\tb.go\n"} },
			acts: []act{
				{mode: claudeStop, done: true, out: "210 lines added"},
				{mode: claudeStop, done: true, out: "stage them", gates: 1},
				{mode: "claude-prompt", done: true, gates: 1},
			},
			state: map[string]string{"simplify-baseline": firstTree, "simplify-asked": ""},
		},
		{
			// What init does to a new project: thousands of lines gone, few added.
			name:  "deleting code is not a major change",
			given: func(r *rig) { r.answers[numstat] = proctest.Answer{Out: "19\t5199\ta.go\n"} },
			acts:  []act{{mode: "", out: stagingAsk, gates: 1}},
		},
		{
			name:  "without a prompt hook, continued work settles the simplify question",
			given: func(r *rig) { r.answers[numstat] = proctest.Answer{Out: "300\t0\ta.go\n"} },
			acts: []act{
				{mode: "", out: simplifyAsk},
				{
					mode: "", out: stagingAsk, gates: 1,
					before: func(r *rig) {
						newTree(r)
						r.answers[numstat] = proctest.Answer{Out: "3\t0\ta.go\n"}
					},
				},
			},
			state: map[string]string{"simplify-baseline": firstTree},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			if tt.given != nil {
				tt.given(r)
			}

			for _, each := range tt.acts {
				each.perform(t, r)
			}

			tt.verify(t, r)
		})
	}
}

func TestRunFails(t *testing.T) {
	t.Parallel()

	broken := map[string]proctest.Answer{"git": {Err: errGate}}

	tests := []struct {
		name    string
		mode    string
		answers map[string]proctest.Answer
		// wantErr is what Run returns; a Claude Code hook reports in JSON instead.
		wantErr  error
		wantDone bool
		wantOut  string
	}{
		{name: "git failure", answers: broken, wantErr: errGate},
		{
			name: "git failure in a Claude Code hook", mode: claudeStop, answers: broken,
			wantDone: true, wantOut: "could not run",
		},
		{name: "misspelled mode", mode: "claude-stp", wantErr: agent.ErrMode},
		{name: "claude without a hook", mode: "claude-", wantErr: agent.ErrMode},
		{name: "run is not a mode", mode: "run", wantErr: agent.ErrMode},
		{name: "two words", mode: "stop now", wantErr: agent.ErrMode},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			fake := &proctest.Fake{Answers: tt.answers}
			done, err := agent.Run(t.Context(), &agent.Config{
				Mode: tt.mode,
				Repo: git.Repo{Run: fake, Getenv: func(string) string { return "" }},
				In:   strings.NewReader(""),
				Out:  &out,
			})

			if done != tt.wantDone || !errors.Is(err, tt.wantErr) || !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("Run(%q) = %v, %v with %q, want %+v", tt.mode, done, err, out.String(), tt)
			}
		})
	}
}
