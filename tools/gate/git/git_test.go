package git_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/git"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

var errGit = errors.New("fatal")

func repo(env map[string]string, answers map[string]proctest.Answer) (git.Repo, *proctest.Fake) {
	fake := &proctest.Fake{Answers: answers}

	return git.Repo{Run: fake, Getenv: func(key string) string { return env[key] }}, fake
}

func TestBranch(t *testing.T) {
	t.Parallel()

	local, _ := repo(nil, map[string]proctest.Answer{"git rev-parse --abbrev-ref HEAD": {Out: "feat/x\n"}})
	if got, err := local.Branch(t.Context()); got != "feat/x" || err != nil {
		t.Fatalf("Branch() = %q, %v, want the checked-out branch", got, err)
	}

	ci, _ := repo(map[string]string{"GITHUB_HEAD_REF": "fix/y"}, nil)
	if got, err := ci.Branch(t.Context()); got != "fix/y" || err != nil {
		t.Fatalf("Branch() = %q, %v, want the pull request branch", got, err)
	}
}

func TestForkPointPrefersTheRemoteBase(t *testing.T) {
	t.Parallel()

	withRemote, fake := repo(map[string]string{"GITHUB_BASE_REF": "release"}, map[string]proctest.Answer{
		"git merge-base": {Out: "abc\n"},
	})
	if got, err := withRemote.ForkPoint(t.Context()); got != "abc" || err != nil {
		t.Fatalf("ForkPoint() = %q, %v", got, err)
	}

	if calls := fake.Calls(); calls[len(calls)-1] != "git merge-base origin/release HEAD" {
		t.Fatalf("ForkPoint() ran %v, want the remote base", calls)
	}

	localOnly, fake := repo(nil, map[string]proctest.Answer{"git rev-parse -q --verify": {Err: errGit}})
	if _, err := localOnly.ForkPoint(t.Context()); err != nil {
		t.Fatalf("ForkPoint() error = %v", err)
	}

	if calls := fake.Calls(); calls[len(calls)-1] != "git merge-base main HEAD" {
		t.Fatalf("ForkPoint() ran %v, want the local default branch", calls)
	}
}

func TestDirtyAndChanged(t *testing.T) {
	t.Parallel()

	r, fake := repo(nil, map[string]proctest.Answer{
		"git status":   {Out: " M a.go\n"},
		"git diff":     {Out: "a.go\x00caf\u00e9 b.go\x00"},
		"git ls-files": {Out: "c.go\x00"},
	})

	if dirty, err := r.Dirty(t.Context()); !dirty || err != nil {
		t.Fatalf("Dirty() = %v, %v, want true", dirty, err)
	}

	got, err := r.Changed(t.Context(), "abc", "AM", "*_test.go")
	if err != nil || !slices.Equal(got, []string{"a.go", "caf\u00e9 b.go", "c.go"}) {
		t.Fatalf("Changed() = %q, %v, want tracked then untracked, names as they are on disk", got, err)
	}

	if calls := fake.Calls(); calls[1] != "git diff --name-only -z --diff-filter=AM abc -- *_test.go" {
		t.Fatalf("Changed() ran %q", calls[1])
	}
}

func TestChangedLinesSkipsBinaryRows(t *testing.T) {
	t.Parallel()

	r, _ := repo(nil, map[string]proctest.Answer{"git diff --numstat": {Out: "10\t2\ta.go\n-\t-\tlogo.png\n3\t0\tb.go\n"}})

	if got, err := r.ChangedLines(t.Context(), "a", "b", "*.go"); got != 15 || err != nil {
		t.Fatalf("ChangedLines() = %d, %v, want 15", got, err)
	}
}

func TestSnapshotUsesTheScratchIndex(t *testing.T) {
	t.Parallel()

	failing, _ := repo(nil, map[string]proctest.Answer{"git add": {Err: errGit}})
	if _, err := failing.Snapshot(t.Context(), "/tmp/index", "."); !errors.Is(err, errGit) {
		t.Fatalf("Snapshot() error = %v, want the failed add reported, not an empty tree", err)
	}

	r, fake := repo(nil, map[string]proctest.Answer{"git write-tree": {Out: "tree1\n"}})

	got, err := r.Snapshot(t.Context(), "/tmp/index", "*.go")
	if got != "tree1" || err != nil {
		t.Fatalf("Snapshot() = %q, %v, want the tree id", got, err)
	}

	want := []string{"GIT_INDEX_FILE=/tmp/index git add -A -- *.go", "GIT_INDEX_FILE=/tmp/index git write-tree"}
	if calls := fake.Calls(); !slices.Equal(calls, want) {
		t.Fatalf("Snapshot() ran %v, want %v", calls, want)
	}
}

func TestWorkTreeStartsFromTheCommit(t *testing.T) {
	t.Parallel()

	r, fake := repo(nil, map[string]proctest.Answer{"git write-tree": {Out: "tree1\n"}})

	got, err := r.WorkTree(t.Context(), "/tmp/index")
	if got != "tree1" || err != nil {
		t.Fatalf("WorkTree() = %q, %v", got, err)
	}

	want := []string{
		"GIT_INDEX_FILE=/tmp/index git read-tree HEAD",
		"GIT_INDEX_FILE=/tmp/index git add -A -- .",
		"GIT_INDEX_FILE=/tmp/index git write-tree",
	}
	if calls := fake.Calls(); !slices.Equal(calls, want) {
		t.Fatalf("WorkTree() ran %v, want %v", calls, want)
	}

	failing, _ := repo(nil, map[string]proctest.Answer{"git read-tree": {Err: errGit}})
	if _, err := failing.WorkTree(t.Context(), "/tmp/index"); !errors.Is(err, errGit) {
		t.Fatalf("WorkTree() error = %v, want the failed read-tree", err)
	}
}

func TestStagedBinaries(t *testing.T) {
	t.Parallel()

	r, _ := repo(nil, map[string]proctest.Answer{
		"git mktree":                  {Out: "empty\n"},
		"git diff --cached":           {Out: "3\t0\ta.go\x00-\t-\tbin/app\x00-\t-\tlogo.png\x00"},
		"git cat-file blob :bin/app":  {Out: "\x7fELF\x02\x01"},
		"git cat-file blob :logo.png": {Out: "\x89PNG"},
	})

	got, err := r.StagedBinaries(t.Context())
	if err != nil || !slices.Equal(got, []string{"bin/app"}) {
		t.Fatalf("StagedBinaries() = %v, %v, want only the ELF file", got, err)
	}
}

func TestSmallQueries(t *testing.T) {
	t.Parallel()

	r, _ := repo(nil, map[string]proctest.Answer{
		"git rev-parse --absolute-git-dir": {Out: "/repo/.git\n"},
		"git ls-files -s":                  {Out: "100644 abc 0\ta.go\n"},
		"git log":                          {Out: "feat: one\nfix: two\n"},
	})

	if dir, err := r.Dir(t.Context()); dir != "/repo/.git" || err != nil {
		t.Fatalf("Dir() = %q, %v", dir, err)
	}

	if index, err := r.Index(t.Context()); index != "100644 abc 0\ta.go" || err != nil {
		t.Fatalf("Index() = %q, %v", index, err)
	}

	subjects, err := r.Subjects(t.Context(), "abc")
	if err != nil || !slices.Equal(subjects, []string{"feat: one", "fix: two"}) {
		t.Fatalf("Subjects() = %v, %v", subjects, err)
	}
}

func TestHasTreeAndHooksPath(t *testing.T) {
	t.Parallel()

	r, fake := repo(nil, map[string]proctest.Answer{
		"git cat-file -e gone": {Err: errGit},
		"git rev-parse HEAD":   {Out: "abc\n"},
		"git config --get":     {Out: ".githooks\n"},
	})

	if r.HasTree(t.Context(), "gone") || r.HasTree(t.Context(), "") || !r.HasTree(t.Context(), "kept") {
		t.Fatal("HasTree() must be false for a pruned or empty id and true otherwise")
	}

	if head, err := r.Head(t.Context()); head != "abc" || err != nil {
		t.Fatalf("Head() = %q, %v", head, err)
	}

	if r.HooksPath(t.Context()) != ".githooks" {
		t.Fatalf("HooksPath() = %q", r.HooksPath(t.Context()))
	}

	unset, _ := repo(nil, map[string]proctest.Answer{"git config --get": {Err: errGit}})
	if unset.HooksPath(t.Context()) != "" {
		t.Fatal("HooksPath() must be empty when git has no setting")
	}

	err := r.SetHooksPath(t.Context(), ".githooks")
	if err != nil || !slices.Contains(fake.Calls(), "git config core.hooksPath .githooks") {
		t.Fatalf("SetHooksPath() = %v after %v", err, fake.Calls())
	}
}

func TestFailuresAreReported(t *testing.T) {
	t.Parallel()

	r, _ := repo(nil, map[string]proctest.Answer{"git": {Err: errGit}})

	if _, err := r.Changed(t.Context(), "abc", "AM"); !errors.Is(err, errGit) {
		t.Fatalf("Changed() error = %v", err)
	}

	if _, err := r.ChangedLines(t.Context(), "a", "b", "*.go"); !errors.Is(err, errGit) {
		t.Fatalf("ChangedLines() error = %v", err)
	}

	if _, err := r.StagedBinaries(t.Context()); !errors.Is(err, errGit) {
		t.Fatalf("StagedBinaries() error = %v", err)
	}
}
