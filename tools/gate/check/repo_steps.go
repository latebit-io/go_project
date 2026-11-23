package check

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/latebit-io/go_project/tools/gate/conv"
)

// lintVersionFile pins golangci-lint for laptops and CI alike; with
// different versions they disagree about the same code.
const lintVersionFile = ".golangci-lint-version"

// hooksDir holds the git hooks committed with the repository.
const hooksDir = ".githooks"

func toolsAndHooks(ctx context.Context, env *Env) error {
	pinned, err := fs.ReadFile(env.Tree, lintVersionFile)
	if err != nil {
		return fmt.Errorf("read %s: %w", lintVersionFile, err)
	}

	installed, err := env.Repo.Run.Output(ctx, nil, "golangci-lint", "version", "--short")
	if err != nil {
		return fmt.Errorf("golangci-lint is not usable: %w", err)
	}

	want := strings.TrimPrefix(strings.TrimSpace(string(pinned)), "v")
	if have := strings.TrimSpace(installed); have != want {
		return failf("golangci-lint %s installed, %s pins %s", have, lintVersionFile, want)
	}

	// A clone without the hooks commits unchecked; CI needs no hooks.
	if !env.inCI() && env.Repo.HooksPath(ctx) != hooksDir {
		return failf("git hooks are off; enable them with: go run ./tools/gate setup")
	}

	env.sayf("ok: golangci-lint %s, git hooks on\n", want)

	return nil
}

func branchName(ctx context.Context, env *Env) error {
	branch, err := env.Repo.Branch(ctx)
	if err != nil {
		return fmt.Errorf("find branch: %w", err)
	}

	dirty, err := env.Repo.Dirty(ctx)
	if err != nil {
		return fmt.Errorf("find changes: %w", err)
	}

	if branch == conv.DefaultBranch && dirty {
		return failf("uncommitted changes on %s; move them with: git checkout -b <type>/<slug>", branch)
	}

	if err := conv.CheckBranch(branch); err != nil {
		return errors.Join(ErrFailed, err)
	}

	env.sayf("ok: branch %s\n", branch)

	return nil
}

func stagedBinaries(ctx context.Context, env *Env) error {
	found, err := env.Repo.StagedBinaries(ctx)
	if err != nil {
		return fmt.Errorf("read index: %w", err)
	}

	if len(found) > 0 {
		return failf("compiled binaries are staged; build to bin/ and run git rm --cached on: %s", strings.Join(found, " "))
	}

	env.sayf("ok: no staged binaries\n")

	return nil
}

// testsChanged requires a test change with a Go change, unless the branch
// type promises unchanged behavior. Whether the test covers it is for review.
func testsChanged(ctx context.Context, env *Env) error {
	branch, err := env.Repo.Branch(ctx)
	if err != nil {
		return fmt.Errorf("find branch: %w", err)
	}

	if !conv.NeedsTest(branch) {
		env.sayf("ok: branch type needs no test change\n")

		return nil
	}

	fork, err := env.Repo.ForkPoint(ctx)
	if err != nil {
		return fmt.Errorf("find fork point: %w", err)
	}

	code, err := env.Repo.Changed(ctx, fork, "ACDMRT", "*.go", ":!*_test.go")
	if err != nil {
		return fmt.Errorf("list changed code: %w", err)
	}

	// A deleted test proves nothing, so only added or modified ones count.
	tests, err := env.Repo.Changed(ctx, fork, "AM", "*_test.go")
	if err != nil {
		return fmt.Errorf("list changed tests: %w", err)
	}

	if len(code) > 0 && len(tests) == 0 {
		return failf("branch %s changes Go code but no test; write the test first: %s", branch, strings.Join(code, " "))
	}

	env.sayf("ok: Go changes come with a test change, or there are none\n")

	return nil
}
