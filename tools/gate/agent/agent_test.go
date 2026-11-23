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

const firstTree = "tree1"

var errGate = errors.New("lint: check failed")

// secondTree is what git answers once the work tree has changed.
func secondTree() proctest.Answer { return proctest.Answer{Out: "tree2\n"} }

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
		"git ls-files -z":                  {Out: "a.go\x00gone.go\x00gen/model.gen.go\x00"},
		"git write-tree":                   {Out: firstTree + "\n"},
		"git diff --numstat":               {Out: "5\t5\ta.go\n"},
	}}
}

// result is what one run of the step printed and whether it reported done.
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
		Tree: fstest.MapFS{"a.go": {}, "gen/model.gen.go": {}},
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

func TestCleanTreeNeedsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.answers["git status"] = proctest.Answer{}

	got := r.run(t, "")
	if !got.done || !strings.Contains(got.out, "nothing uncommitted") || r.gates != 0 {
		t.Fatalf("run = %+v after %d gate runs, want a quiet success without the gate", got, r.gates)
	}

	if hook := r.run(t, "stop"); hook.out != "" {
		t.Fatalf("stop hook printed %q, want silence", hook.out)
	}
}

func TestPassingGateAsksAboutStagingOncePerTree(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	first := r.run(t, "")
	if first.done || !strings.HasPrefix(first.out, "QUESTION: The gate passes") {
		t.Fatalf("first run = %+v, want the staging question", first)
	}

	second := r.run(t, "")
	if !second.done || !strings.Contains(second.out, "nothing to ask") || r.gates != 1 {
		t.Fatalf("second run = %+v after %d gate runs, want the cached pass and no question", second, r.gates)
	}

	r.answers["git write-tree"] = secondTree()

	if third := r.run(t, ""); third.done || r.gates != 2 {
		t.Fatalf("run on a new tree = %+v after %d gate runs, want the gate and the question again", third, r.gates)
	}
}

// A tracked file that .gitignore matches must be in the snapshot, or an edit
// to it would reuse a remembered pass. Files deleted on disk are left out.
func TestSnapshotForcesTrackedFiles(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.run(t, "")

	listed := r.state(t, "tracked")
	if listed != "a.go\x00gen/model.gen.go" {
		t.Fatalf("tracked list = %q, want the two files that exist", listed)
	}

	forced := slices.ContainsFunc(r.calls, func(call string) bool {
		return strings.Contains(call, "git add -f --pathspec-from-file=") && strings.HasSuffix(call, "--pathspec-file-nul")
	})
	if !forced {
		t.Fatalf("no forced add of the tracked files in %v", r.calls)
	}
}

func TestFailingGate(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gateErr = errGate

	got := r.run(t, "")
	shown := strings.Contains(got.out, "finding 1") && strings.Contains(got.out, "never weaken")

	if got.done || !shown {
		t.Fatalf("run = %+v, want not done, with the gate output", got)
	}

	if hook := r.run(t, "claude-stop"); !hook.done || !strings.Contains(hook.out, `"systemMessage"`) {
		t.Fatalf("claude-stop on the same failing tree = %+v, want it let through with a note", hook)
	}

	if hook := r.run(t, "stop"); !hook.done || !strings.Contains(hook.out, "nothing changed since") {
		t.Fatalf("stop on the same failing tree = %+v, want it let through with a note", hook)
	}

	// The user's next message may follow a fix outside the tree, such as
	// installing a tool, so the failure is forgotten and the gate runs again.
	r.run(t, "prompt")

	if hook := r.run(t, "stop"); hook.done || r.gates != 2 {
		t.Fatalf("stop after a user prompt = %+v after %d gate runs, want the gate retried", hook, r.gates)
	}
}

func TestFailingGateBlocksAClaudeStop(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gateErr = errGate

	hook := r.run(t, "claude-stop")
	blocked := strings.Contains(hook.out, `"decision":"block"`) && strings.Contains(hook.out, "finding 1")

	if !hook.done || !blocked || r.gates != 1 {
		t.Fatalf("claude-stop on a failing tree = %+v after %d gate runs, want a JSON block", hook, r.gates)
	}
}

func TestPassIsNotRecordedForATreeThatMoved(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.duringGate = func() { r.answers["git write-tree"] = secondTree() }

	got := r.run(t, "")
	if got.done || !strings.Contains(got.out, "Files changed while the gate ran") {
		t.Fatalf("run = %+v, want the pass refused because the tree moved", got)
	}

	if r.state(t, "gate-result") != "" {
		t.Fatalf("gate-result = %q, want nothing recorded", r.state(t, "gate-result"))
	}

	r.duringGate = nil

	if again := r.run(t, ""); again.done || !strings.Contains(again.out, "stage them") || r.gates != 2 {
		t.Fatalf("second run = %+v after %d gate runs, want the gate rerun on the settled tree", again, r.gates)
	}
}

func TestMajorChangeAsksAboutSimplify(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.answers["git diff --numstat"] = proctest.Answer{Out: "150\t60\ta.go\n"}

	hook := r.run(t, "claude-stop")
	blocked := strings.Contains(hook.out, `"decision":"block"`) && strings.Contains(hook.out, "210 lines")

	if !hook.done || !blocked {
		t.Fatalf("claude-stop = %+v, want a JSON block with the simplify question", hook)
	}

	if r.gates != 0 || r.state(t, "simplify-asked") != firstTree {
		t.Fatalf("gate ran %d times, asked=%q; the question comes before the gate", r.gates, r.state(t, "simplify-asked"))
	}

	if again := r.run(t, "claude-stop"); !strings.Contains(again.out, "stage them") {
		t.Fatalf("second stop = %+v, want no repeat of the simplify question", again)
	}

	if prompt := r.run(t, "claude-prompt"); !prompt.done || r.state(t, "simplify-baseline") != firstTree {
		t.Fatalf("prompt = %+v, baseline=%q, want the question settled", prompt, r.state(t, "simplify-baseline"))
	}
}

func TestDirectRunSettlesWhenWorkContinues(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.answers["git diff --numstat"] = proctest.Answer{Out: "300\t0\ta.go\n"}

	if got := r.run(t, ""); got.done || !strings.Contains(got.out, "simplify pass") {
		t.Fatalf("run = %+v, want the simplify question", got)
	}

	r.answers["git write-tree"] = secondTree()
	r.answers["git diff --numstat"] = proctest.Answer{Out: "3\t0\ta.go\n"}

	got := r.run(t, "")
	if r.state(t, "simplify-baseline") != firstTree || strings.Contains(got.out, "simplify pass") {
		t.Fatalf("run after more work = %+v, want the old question settled and no new one", got)
	}
}

func TestRunReportsGitFailure(t *testing.T) {
	t.Parallel()

	fake := &proctest.Fake{Answers: map[string]proctest.Answer{"git": {Err: errGate}}}

	repo := git.Repo{Run: fake, Getenv: func(string) string { return "" }}

	done, err := agent.Run(t.Context(), &agent.Config{Repo: repo})
	if done || !errors.Is(err, errGate) {
		t.Fatalf("Run() = %v, %v, want the git failure", done, err)
	}

	// Claude Code ignores a failing exit, so its hook must say so in JSON.
	var out bytes.Buffer

	hook := &agent.Config{Mode: "claude-stop", Repo: repo, In: strings.NewReader(""), Out: &out}

	done, err = agent.Run(t.Context(), hook)
	if !done || err != nil || !strings.Contains(out.String(), "could not run") {
		t.Fatalf("Run(claude-stop) = %v, %v with %q, want the failure as a system message", done, err, out.String())
	}
}

func TestRunRejectsUnknownModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"claude-stp", "claude-", "run", "stop now"} {
		if _, err := agent.Run(t.Context(), &agent.Config{Mode: mode}); !errors.Is(err, agent.ErrMode) {
			t.Errorf("Run(%q) = %v, want an unknown mode error", mode, err)
		}
	}
}
