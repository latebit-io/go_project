package git_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/git"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const (
	commit    = "abc"
	gitDiff   = "git diff"
	index     = "/tmp/index"
	indexEnv  = "GIT_INDEX_FILE=/tmp/index "
	listIndex = "git ls-files -s"
)

var errGit = errors.New("fatal")

// query asks the repository one thing; a query without a result returns nil.
type query func(ctx context.Context, r git.Repo) (any, error)

func repo(env map[string]string, answers map[string]proctest.Answer) (git.Repo, *proctest.Fake) {
	fake := &proctest.Fake{Answers: answers}

	return git.Repo{Run: fake, Getenv: func(key string) string { return env[key] }}, fake
}

func TestQueries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		answers map[string]proctest.Answer
		ask     query
		want    any
		// wantCall, when set, must be among the commands that ran.
		wantCall string
	}{
		{
			name:    "branch is the checked-out one",
			answers: map[string]proctest.Answer{"git rev-parse --abbrev-ref HEAD": {Out: "feat/x\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Branch(ctx) },
			want:    "feat/x",
		},
		{
			name: "branch is the pull request's in CI",
			env:  map[string]string{"GITHUB_HEAD_REF": "fix/y"},
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.Branch(ctx) },
			want: "fix/y",
		},
		{
			name:    "fork point prefers the remote base",
			env:     map[string]string{"GITHUB_BASE_REF": "release"},
			answers: map[string]proctest.Answer{"git merge-base": {Out: commit + "\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.ForkPoint(ctx) },
			want:    commit, wantCall: "git merge-base origin/release HEAD",
		},
		{
			name: "fork point falls back to the local default branch",
			answers: map[string]proctest.Answer{
				"git rev-parse -q --verify": {Err: errGit},
				"git merge-base":            {Out: commit + "\n"},
			},
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.ForkPoint(ctx) },
			want: commit, wantCall: "git merge-base main HEAD",
		},
		{
			name:    "head",
			answers: map[string]proctest.Answer{"git rev-parse HEAD": {Out: commit + "\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Head(ctx) },
			want:    commit,
		},
		{
			name:    "dirty",
			answers: map[string]proctest.Answer{"git status": {Out: " M a.go\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Dirty(ctx) },
			want:    true,
		},
		{
			name: "changed lists tracked then untracked, names as they are on disk",
			answers: map[string]proctest.Answer{
				gitDiff:        {Out: "a.go\x00café b.go\x00"},
				"git ls-files": {Out: "c.go\x00"},
			},
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.Changed(ctx, commit, "AM", "*_test.go") },
			want: []string{"a.go", "café b.go", "c.go"}, wantCall: "git diff --name-only -z --diff-filter=AM abc -- *_test.go",
		},
		{
			name:    "added lines leave out deleted lines and binary rows",
			answers: map[string]proctest.Answer{"git diff --numstat": {Out: "10\t2\ta.go\n-\t-\tlogo.png\n3\t900\tb.go\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.AddedLines(ctx, "a", "b", "*.go") },
			want:    13,
		},
		{
			name:    "snapshot writes a tree from the scratch index",
			answers: map[string]proctest.Answer{"git write-tree": {Out: "tree1\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Snapshot(ctx, index, "*.go") },
			want:    "tree1", wantCall: indexEnv + "git add -A -- *.go",
		},
		{
			name:    "tracked lists the real index",
			answers: map[string]proctest.Answer{"git ls-files -z": {Out: "a.go\x00gen/model.gen.go\x00"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Tracked(ctx) },
			want:    []string{"a.go", "gen/model.gen.go"},
		},
		{
			name:     "force adds listed paths literally",
			ask:      func(ctx context.Context, r git.Repo) (any, error) { return nil, r.Force(ctx, index, "/tmp/tracked") },
			wantCall: indexEnv + "git --literal-pathspecs add -f --pathspec-from-file=/tmp/tracked --pathspec-file-nul",
		},
		{
			name:    "no symlink among regular files",
			answers: map[string]proctest.Answer{listIndex: {Out: "100644 abc 0\ta.go\n100755 def 0\trun\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.HasSymlink(ctx, index) },
			want:    false, wantCall: indexEnv + listIndex,
		},
		{
			name:    "a symlink is mode 120000",
			answers: map[string]proctest.Answer{listIndex: {Out: "100644 abc 0\ta.go\n120000 def 0\tdata\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.HasSymlink(ctx, index) },
			want:    true,
		},
		{
			name: "staged binaries are found by their first bytes",
			answers: map[string]proctest.Answer{
				"git mktree":                  {Out: "empty\n"},
				"git diff --cached":           {Out: "3\t0\ta.go\x00-\t-\tbin/app\x00-\t-\tlogo.png\x00"},
				"git cat-file blob :bin/app":  {Out: "\x7fELF\x02\x01"},
				"git cat-file blob :logo.png": {Out: "\x89PNG"},
			},
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.StagedBinaries(ctx) },
			want: []string{"bin/app"},
		},
		{
			name:    "git directory",
			answers: map[string]proctest.Answer{"git rev-parse --absolute-git-dir": {Out: "/repo/.git\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Dir(ctx) },
			want:    "/repo/.git",
		},
		{
			name:    "index listing",
			answers: map[string]proctest.Answer{listIndex: {Out: "100644 abc 0\ta.go\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.Index(ctx) },
			want:    "100644 abc 0\ta.go",
		},
		{
			name:    "hooks path",
			answers: map[string]proctest.Answer{"git config --get": {Out: ".githooks\n"}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.HooksPath(ctx), nil },
			want:    ".githooks",
		},
		{
			name:    "hooks path is empty when git has no setting",
			answers: map[string]proctest.Answer{"git config --get": {Err: errGit}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.HooksPath(ctx), nil },
			want:    "",
		},
		{
			name:     "set hooks path",
			ask:      func(ctx context.Context, r git.Repo) (any, error) { return nil, r.SetHooksPath(ctx, ".githooks") },
			wantCall: "git config core.hooksPath .githooks",
		},
		{
			name: "a kept tree exists",
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.HasTree(ctx, "kept"), nil },
			want: true,
		},
		{
			name:    "a pruned tree does not",
			answers: map[string]proctest.Answer{"git cat-file -e gone": {Err: errGit}},
			ask:     func(ctx context.Context, r git.Repo) (any, error) { return r.HasTree(ctx, "gone"), nil },
			want:    false,
		},
		{
			name: "an empty id names no tree",
			ask:  func(ctx context.Context, r git.Repo) (any, error) { return r.HasTree(ctx, ""), nil },
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, fake := repo(tt.env, tt.answers)

			got, err := tt.ask(t.Context(), r)
			if err != nil || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, %v, want %#v", got, err, tt.want)
			}

			if calls := fake.Calls(); tt.wantCall != "" && !slices.Contains(calls, tt.wantCall) {
				t.Fatalf("ran %q, want %q among them", calls, tt.wantCall)
			}
		})
	}
}

func TestFailuresAreReported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// failing is the command, by prefix, that git refuses.
		failing string
		ask     query
	}{
		{
			name: "branch", failing: "git rev-parse",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.Branch(ctx) },
		},
		{
			name: "dirty", failing: "git status",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.Dirty(ctx) },
		},
		{
			name: "changed, tracked part", failing: gitDiff,
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.Changed(ctx, commit, "AM") },
		},
		{
			name: "changed, untracked part", failing: "git ls-files",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.Changed(ctx, commit, "AM") },
		},
		{
			name: "added lines", failing: gitDiff,
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.AddedLines(ctx, "a", "b", "*.go") },
		},
		{
			name: "snapshot reports a failed add instead of an empty tree", failing: "git add",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.Snapshot(ctx, index, ".") },
		},
		{
			name: "force", failing: "git --literal-pathspecs add",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return nil, r.Force(ctx, index, "/tmp/tracked") },
		},
		{
			name: "symlinks", failing: listIndex,
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.HasSymlink(ctx, index) },
		},
		{
			name: "staged binaries, empty tree", failing: "git mktree",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.StagedBinaries(ctx) },
		},
		{
			name: "staged binaries, index diff", failing: "git diff --cached",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return r.StagedBinaries(ctx) },
		},
		{
			name: "set hooks path", failing: "git config",
			ask: func(ctx context.Context, r git.Repo) (any, error) { return nil, r.SetHooksPath(ctx, ".githooks") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, _ := repo(nil, map[string]proctest.Answer{tt.failing: {Err: errGit}})

			if _, err := tt.ask(t.Context(), r); !errors.Is(err, errGit) {
				t.Fatalf("error = %v, want git's failure", err)
			}
		})
	}
}
