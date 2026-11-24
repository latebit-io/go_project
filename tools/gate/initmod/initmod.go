// Package initmod turns a fresh copy of the template into a project: it
// renames the module, replaces the gate's source with a pinned dependency
// and starts the project's own README.
package initmod

import (
	"bytes"
	"context"
	_ "embed" // for the starter README
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/latebit-io/go_project/tools/arch"
	"github.com/latebit-io/go_project/tools/gate/proc"
	"github.com/latebit-io/go_project/tools/srcwalk"
)

// ErrModule marks a module path that cannot be used.
var ErrModule = errors.New("module path")

const (
	// upstream is the module that publishes the gate: the template itself.
	upstream = "github.com/latebit-io/go_project"
	gateTool = upstream + "/tools/gate"
	// toolsDir holds the gate's source and tests, which a project does not keep.
	toolsDir = "tools"
	// gateDir is a directory only in a copy not yet converted; a project's own
	// tools/ has none.
	gateDir  = toolsDir + "/gate"
	fileMode = 0o644
)

// starter is the README a new project begins with, in place of the template's.
//
//go:embed project_readme.md
var starter string

// Project is a fresh copy of the template that is to become a project.
type Project struct {
	Root string
	Run  proc.Runner
	// Out is told each thing as it is done, and what to run next.
	Out io.Writer
}

// Convert makes the copy a project called next: it rewrites go.mod and every
// Go import, pins the gate as a tool from the template's module, and removes
// what only the template needs.
func (p Project) Convert(ctx context.Context, next string) error {
	if !regexp.MustCompile(`^[a-z0-9.-]+(/[A-Za-z0-9._~-]+)+$`).MatchString(next) {
		return fmt.Errorf("%w: %q does not look like github.com/acme/shop", ErrModule, next)
	}

	if next == upstream {
		return fmt.Errorf("%w: %s is the template itself", ErrModule, next)
	}

	files, err := os.OpenRoot(p.Root)
	if err != nil {
		return fmt.Errorf("open module: %w", err)
	}

	convertErr := p.convertFiles(ctx, files, next)

	return errors.Join(convertErr, files.Close())
}

func (p Project) convertFiles(ctx context.Context, files *os.Root, next string) error {
	current, err := arch.ModulePath(files.FS())
	if err != nil {
		return fmt.Errorf("read module: %w", err)
	}

	// Already renamed means an earlier run stopped part way: carry on from there.
	if current != next {
		err = renameModule(ctx, files, p.Run, modules{current: current, next: next})
		if err != nil {
			return err
		}
	}

	// The tool line in go.mod still names the template's package. With the
	// module renamed it is a dependency, fetched at a pinned version.
	_, err = p.Run.Output(ctx, nil, "go", "get", "-tool", gateTool+"@latest")
	if err != nil {
		return fmt.Errorf("pin the gate: %w", err)
	}

	err = p.sayf("ok: module %s, gate pinned in go.mod\n", next)
	if err != nil {
		return err
	}

	// After the pin, so that a failure above leaves a gate to run init with again.
	err = p.shedTemplate(files, next)
	if err != nil {
		return err
	}

	// The new path can sort differently among the imports.
	err = p.Run.Stream(ctx, nil, "golangci-lint", "fmt")
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}

	return p.sayf("next: go tool gate setup\n")
}

// shedTemplate replaces the template's README with a starter for the project
// and removes the gate's source. A copy without that source was converted
// before: its README, and any tools of its own, stay.
func (p Project) shedTemplate(files *os.Root, next string) error {
	gate, err := files.Stat(gateDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("find gate source: %w", err)
	}

	// A file of that name, such as a built binary, is the project's own.
	if !gate.IsDir() {
		return nil
	}

	names := strings.NewReplacer("{{name}}", path.Base(next), "{{upstream}}", upstream, "{{gate}}", gateTool)

	err = files.WriteFile("README.md", []byte(names.Replace(starter)), fileMode)
	if err != nil {
		return fmt.Errorf("start README: %w", err)
	}

	err = files.RemoveAll(toolsDir)
	if err != nil {
		return fmt.Errorf("remove gate source: %w", err)
	}

	return p.sayf("ok: README.md started, %s/ removed\n", toolsDir)
}

func (p Project) sayf(format string, args ...any) error {
	_, err := fmt.Fprintf(p.Out, format, args...)
	if err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}

// modules names the module path being replaced and its replacement.
type modules struct {
	current string
	next    string
}

// renameModule rewrites the imports first and go.mod last: if a write fails,
// go.mod still names the old module and running init again finishes the rest.
func renameModule(ctx context.Context, files *os.Root, run proc.Runner, m modules) error {
	err := srcwalk.Files(files.FS(), func(name string, src []byte) error {
		rewritten, importErr := rewriteImports(name, src, m.current, m.next)
		if importErr != nil || bytes.Equal(rewritten, src) {
			return importErr
		}

		return files.WriteFile(name, rewritten, fileMode)
	})
	if err != nil {
		return fmt.Errorf("rewrite imports: %w", err)
	}

	_, err = run.Output(ctx, nil, "go", "mod", "edit", "-module", m.next)
	if err != nil {
		return fmt.Errorf("edit go.mod: %w", err)
	}

	return nil
}

// rewriteImports replaces the module path in import declarations only. The
// same text in a string constant or a comment is program data and stays.
func rewriteImports(name string, src []byte, current, next string) ([]byte, error) {
	fset := token.NewFileSet()

	parsed, err := parser.ParseFile(fset, name, src, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	// Last import first, so earlier offsets stay valid as the text changes length.
	for _, spec := range slices.Backward(parsed.Imports) {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("import path in %s: %w", name, err)
		}

		rest, inModule := strings.CutPrefix(imported, current)
		if !inModule || (rest != "" && !strings.HasPrefix(rest, "/")) {
			continue
		}

		start, end := fset.Position(spec.Path.Pos()).Offset, fset.Position(spec.Path.End()).Offset
		src = slices.Concat(src[:start], []byte(strconv.Quote(next+rest)), src[end:])
	}

	return src, nil
}
