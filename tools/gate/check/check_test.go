package check_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/gate/check"
	"github.com/latebit-io/go_project/tools/gate/git"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const (
	stepTools = "tools"
	stepTest  = "test"
	stepSmoke = "smoke"
)

var errTool = errors.New("exit status 1")

func file(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

// cleanTree is a module every step accepts.
func cleanTree() fstest.MapFS {
	return fstest.MapFS{
		"go.mod":                   file("module example.com/shop\n"),
		".golangci-lint-version":   file("v2.15.0\n"),
		"coverage.out":             file("mode: atomic\nexample.com/shop/internal/a/a.go:1.1,2.2 10 1\n"),
		"cmd/api/main.go":          file("// Command api serves.\npackage main\n"),
		"cmd/api/smoke_test.go":    file("package main\n\nfunc TestSmoke(t *testing.T) {}\n"),
		"cmd/README.md":            file("not a command"),
		"internal/platform/x/x.go": file("// Package x is clean.\npackage x\n"),
	}
}

// cleanAnswers scripts the tools for a branch with one good commit.
func cleanAnswers() map[string]proctest.Answer {
	return map[string]proctest.Answer{
		"golangci-lint version --short":   {Out: "2.15.0\n"},
		"git rev-parse --abbrev-ref HEAD": {Out: "feat/x\n"},
		"git merge-base":                  {Out: "abc\n"},
		"git log":                         {Out: "feat: add x\x00"},
		"git diff --name-only":            {Out: "internal/platform/x/x.go\x00"},
		"git ls-files -o":                 {Out: "internal/platform/x/x_test.go\x00"},
		"go env CGO_ENABLED":              {Out: "1\n"},
		"git config --get core.hooksPath": {Out: ".githooks\n"},
	}
}

// outcome is what one gate run printed, which tools it started and how it ended.
type outcome struct {
	out   string
	calls []string
	err   error
}

func run(t *testing.T, tree fstest.MapFS, answers map[string]proctest.Answer, steps ...string) outcome {
	t.Helper()

	var out bytes.Buffer

	// A key "env NAME" in the script sets an environment variable.
	fake := &proctest.Fake{Answers: answers}
	repo := git.Repo{Run: fake, Getenv: func(key string) string { return answers["env "+key].Out }}
	err := check.Run(t.Context(), &check.Env{Tree: tree, Repo: repo, Out: &out}, steps...)

	return outcome{out: out.String(), calls: fake.Calls(), err: err}
}

func TestRunPassesEveryStepInOrder(t *testing.T) {
	t.Parallel()

	got := run(t, cleanTree(), cleanAnswers())
	if got.err != nil {
		t.Fatalf("Run() = %v\n%s", got.err, got.out)
	}

	out, calls := got.out, got.calls

	if ran := strings.Count(out, "\n==> "); ran != len(check.Names()) {
		t.Errorf("Run() ran %d steps, want all %d", ran, len(check.Names()))
	}

	if !strings.HasSuffix(out, "PASS: all checks\n") {
		t.Errorf("Run() output ends %q, want the pass line", out[max(0, len(out)-40):])
	}

	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"go mod tidy -diff", "golangci-lint fmt --diff", "golangci-lint run",
		"GOOS=windows GOARCH=amd64 go vet ./...",
		"go test -shuffle=on -count=1 -coverprofile=coverage.out ./... -race",
		"go build -o bin/ ./cmd/...",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Run() never ran %q", want)
		}
	}
}

func TestRunSelectsSteps(t *testing.T) {
	t.Parallel()

	got := run(t, cleanTree(), cleanAnswers(), "arch", "clean")
	partial := strings.HasSuffix(got.out, "PASS: arch clean only\n") && !strings.Contains(got.out, "Lint")

	if got.err != nil || len(got.calls) != 0 || !partial {
		t.Fatalf("Run(arch, clean) = %+v, want only the two in-process steps and a partial pass line", got)
	}

	if got := run(t, cleanTree(), cleanAnswers(), "nope"); !errors.Is(got.err, check.ErrFailed) {
		t.Fatalf("Run(nope) = %v, want an unknown-step failure", got.err)
	}
}

func TestStepsFail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		step   string
		change func(tree fstest.MapFS, answers map[string]proctest.Answer)
		want   string
	}{
		{
			name: "lint version drift", step: stepTools, want: "2.14.0 installed",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["golangci-lint version --short"] = proctest.Answer{Out: "2.14.0\n"}
			},
		},
		{
			name: "git hooks not enabled", step: stepTools, want: "git hooks are off",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git config --get core.hooksPath"] = proctest.Answer{Err: errTool}
			},
		},
		{
			name: "no race detector in CI", step: stepTest, want: "race detector cannot run",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["go env CGO_ENABLED"] = proctest.Answer{Out: "0\n"}
				a["env CI"] = proctest.Answer{Out: "true"}
			},
		},
		{
			name: "smoke test only in a comment", step: stepSmoke, want: "cmd/api has no TestSmoke",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) {
				tree["cmd/api/smoke_test.go"] = file("package main\n\n// func TestSmoke(t *testing.T) {}\n")
			},
		},
		{
			name: "work on the default branch", step: "branch", want: "uncommitted changes on main",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git rev-parse --abbrev-ref HEAD"] = proctest.Answer{Out: "main\n"}
				a["git status"] = proctest.Answer{Out: " M a.go\n"}
			},
		},
		{
			name: "branch name", step: "branch", want: "is not <type>/<slug>",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git rev-parse --abbrev-ref HEAD"] = proctest.Answer{Out: "wip\n"}
			},
		},
		{
			name: "commit subject", step: "commits", want: "6-word description",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git log"] = proctest.Answer{Out: "feat: add x\x00fix: one two three four five six\x00"}
			},
		},
		{
			name: "staged binary", step: "binaries", want: "bin/app",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git diff --cached"] = proctest.Answer{Out: "-\t-\tbin/app\x00"}
				a["git cat-file blob"] = proctest.Answer{Out: "MZ\x90"}
			},
		},
		{
			name: "code without a test", step: "tests-changed", want: "changes Go code but no test",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git ls-files -o"] = proctest.Answer{}
				a["git diff --name-only -z --diff-filter=AM"] = proctest.Answer{}
			},
		},
		{
			name: "layout", step: "arch", want: "1 architecture violation(s)",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) { tree["pkg/y/y.go"] = file("package y\n") },
		},
		{
			name: "narrating comment", step: "clean", want: "1 cleanup finding(s)",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) {
				tree["internal/platform/x/x.go"] = file("package x\n\n// Loop through the items\n")
			},
		},
		{
			name: "failing tool", step: "lint", want: "exit status 1",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["golangci-lint run"] = proctest.Answer{Err: errTool}
			},
		},
		{
			name: "platform that does not build", step: "cross", want: "linux/amd64",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) { a["go vet"] = proctest.Answer{Err: errTool} },
		},
		{
			name: "coverage under the floor", step: stepTest, want: "coverage 25.0% is below the 80% floor",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) {
				tree["coverage.out"] = file("mode: atomic\nexample.com/shop/a.go:1.1,2.2 1 1\nexample.com/shop/a.go:3.1,4.2 3 0\n")
			},
		},
		{
			name: "command without a smoke test", step: stepSmoke, want: "cmd/api has no TestSmoke",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) { delete(tree, "cmd/api/smoke_test.go") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tree, answers := cleanTree(), cleanAnswers()
			tt.change(tree, answers)

			got := run(t, tree, answers, tt.step)
			if !errors.Is(got.err, check.ErrFailed) || !strings.Contains(got.err.Error()+got.out, tt.want) {
				t.Fatalf("Run(%s) = %+v, want a failure mentioning %q", tt.step, got, tt.want)
			}
		})
	}
}

func TestStepsAdapt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		step   string
		change func(tree fstest.MapFS, answers map[string]proctest.Answer)
		want   string
	}{
		{
			name: "default branch has no commits to check", step: "commits", want: "no branch commits",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git rev-parse --abbrev-ref HEAD"] = proctest.Answer{Out: "main\n"}
			},
		},
		{
			name: "refactor branch needs no test", step: "tests-changed", want: "needs no test change",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git rev-parse --abbrev-ref HEAD"] = proctest.Answer{Out: "refactor/x\n"}
			},
		},
		{
			name: "no cgo turns the race detector off", step: stepTest, want: "race detector is off",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["go env CGO_ENABLED"] = proctest.Answer{Out: "0\n"}
			},
		},
		{
			name: "CI needs no git hooks", step: stepTools, want: "ok: golangci-lint",
			change: func(_ fstest.MapFS, a map[string]proctest.Answer) {
				a["git config --get core.hooksPath"] = proctest.Answer{Err: errTool}
				a["env CI"] = proctest.Answer{Out: "true"}
			},
		},
		{
			name: "module without commands", step: stepSmoke, want: "no commands",
			change: func(tree fstest.MapFS, _ map[string]proctest.Answer) {
				delete(tree, "cmd/api/main.go")
				delete(tree, "cmd/api/smoke_test.go")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tree, answers := cleanTree(), cleanAnswers()
			tt.change(tree, answers)

			got := run(t, tree, answers, tt.step)
			if got.err != nil || !strings.Contains(got.out, tt.want) {
				t.Fatalf("Run(%s) = %+v, want success mentioning %q", tt.step, got, tt.want)
			}
		})
	}
}
