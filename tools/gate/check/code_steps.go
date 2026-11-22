package check

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strings"

	"github.com/latebit-io/go_project/tools/arch"
	"github.com/latebit-io/go_project/tools/clean"
	"github.com/latebit-io/go_project/tools/gate/coverage"
	"github.com/latebit-io/go_project/tools/srcwalk"
)

const (
	// coverageFloor is the least share of statements, outside cmd/, tests must run.
	coverageFloor = 80
	coverProfile  = "coverage.out"
)

func modTidy(ctx context.Context, env *Env) error {
	return env.stream(ctx, nil, "go", "mod", "tidy", "-diff")
}

func formatting(ctx context.Context, env *Env) error {
	return env.stream(ctx, nil, "golangci-lint", "fmt", "--diff")
}

func lint(ctx context.Context, env *Env) error {
	return env.stream(ctx, nil, "golangci-lint", "run")
}

// stream runs a tool whose own output explains a failure.
func (e *Env) stream(ctx context.Context, vars []string, name string, args ...string) error {
	if err := e.Repo.Run.Stream(ctx, vars, name, args...); err != nil {
		return errors.Join(ErrFailed, err)
	}

	return nil
}

func architecture(_ context.Context, env *Env) error {
	module, files, err := arch.Load(env.Tree)
	if err != nil {
		return fmt.Errorf("load module: %w", err)
	}

	violations := arch.Check(module, files)
	for _, violation := range violations {
		env.sayf("%s\n", violation)
	}

	if len(violations) > 0 {
		return failf("%d architecture violation(s)", len(violations))
	}

	return nil
}

func cleanup(_ context.Context, env *Env) error {
	checker := clean.NewChecker()
	total := 0

	err := srcwalk.Files(env.Tree, func(name string, src []byte) error {
		findings, err := checker.Check(name, src)
		if err != nil {
			return fmt.Errorf("check: %w", err)
		}

		for _, finding := range findings {
			env.sayf("%s\n", finding)
		}

		total += len(findings)

		return nil
	})
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if total > 0 {
		return failf("%d cleanup finding(s)", total)
	}

	return nil
}

// crossBuild type-checks code and tests for every supported platform.
func crossBuild(ctx context.Context, env *Env) error {
	for _, platform := range arch.Platforms() {
		goos, goarch, _ := strings.Cut(platform, "/")

		if err := env.stream(ctx, []string{"GOOS=" + goos, "GOARCH=" + goarch}, "go", "vet", "./..."); err != nil {
			return fmt.Errorf("%s: %w", platform, err)
		}

		env.sayf("ok: %s\n", platform)
	}

	return nil
}

func testsAndCoverage(ctx context.Context, env *Env) error {
	args := []string{"test", "-shuffle=on", "-count=1", "-coverprofile=" + coverProfile, "./..."}

	// The race detector needs cgo, which a machine without a C compiler lacks.
	cgo, err := env.Repo.Run.Output(ctx, nil, "go", "env", "CGO_ENABLED")
	if err != nil {
		return fmt.Errorf("read go env: %w", err)
	}

	switch {
	case strings.TrimSpace(cgo) == "1":
		args = append(args, "-race")
	case env.inCI():
		return failf("cgo is off, so the race detector cannot run; CI must run it")
	default:
		env.sayf("note: no cgo here, so the race detector is off; CI runs it\n")
	}

	if err := env.stream(ctx, nil, "go", args...); err != nil {
		return err
	}

	return coverageFloorMet(env)
}

func coverageFloorMet(env *Env) error {
	module, err := arch.ModulePath(env.Tree)
	if err != nil {
		return fmt.Errorf("find module: %w", err)
	}

	profile, err := fs.ReadFile(env.Tree, coverProfile)
	if err != nil {
		return fmt.Errorf("read %s: %w", coverProfile, err)
	}

	// cmd/ is wiring, proven by each command's smoke test instead.
	covered, measured := coverage.Percent(string(profile), module+"/cmd/")
	if measured && covered < coverageFloor {
		return failf("coverage %.1f%% is below the %d%% floor", covered, coverageFloor)
	}

	env.sayf("ok: coverage %.1f%% (floor %d%%)\n", covered, coverageFloor)

	return nil
}

// commands requires a smoke test for every cmd/<name> and proves each links.
func commands(ctx context.Context, env *Env) error {
	entries, err := fs.ReadDir(env.Tree, "cmd")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read cmd: %w", err)
	}

	count := 0

	for _, entry := range entries {
		// The same directories the architecture check treats as packages.
		if !entry.IsDir() || srcwalk.Skips(entry.Name()) {
			continue
		}

		dir := path.Join("cmd", entry.Name())

		tested, err := hasSmokeTest(env.Tree, dir)
		if err != nil {
			return err
		}

		if !tested {
			return failf("%s has no TestSmoke; a command ships with a smoke test in a _test.go file", dir)
		}

		count++
	}

	if count == 0 {
		env.sayf("ok: no commands\n")

		return nil
	}

	if err := env.stream(ctx, nil, "go", "build", "-o", "bin/", "./cmd/..."); err != nil {
		return err
	}

	env.sayf("ok: %d command(s) have a smoke test and build\n", count)

	return nil
}

func hasSmokeTest(tree fs.FS, dir string) (bool, error) {
	tests, err := fs.Glob(tree, path.Join(dir, "*_test.go"))
	if err != nil {
		return false, fmt.Errorf("list tests in %s: %w", dir, err)
	}

	for _, name := range tests {
		src, err := fs.ReadFile(tree, name)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", name, err)
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		if err != nil {
			return false, fmt.Errorf("parse %s: %w", name, err)
		}

		for _, decl := range parsed.Decls {
			// A real test function, not a comment or a string that names one.
			if fn, isFunc := decl.(*ast.FuncDecl); isFunc && fn.Recv == nil && fn.Name.Name == "TestSmoke" {
				return true, nil
			}
		}
	}

	return false, nil
}
